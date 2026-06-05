package memberruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/storage"
)

var ErrDurableFailureRecorded = errors.New("durable fail event recorded")

type StreamClient interface {
	Replay(ctx context.Context, req ReplayRequest) ([]storage.StreamFrame, error)
	Ack(ctx context.Context, req AckRequest) error
}

type ReplayRequest struct {
	SessionID string
	Member    string
	Since     string
	Follow    bool
}

type AckRequest struct {
	SessionID string
	Member    string
	Cursor    string
	EventID   string
}

type CursorStore interface {
	Load(sessionID, member string) (string, error)
	Save(sessionID, member, cursor string) error
}

type ActionHandler interface {
	Handle(ctx context.Context, frame storage.StreamFrame) error
}

type HandlerFunc func(context.Context, storage.StreamFrame) error

func (f HandlerFunc) Handle(ctx context.Context, frame storage.StreamFrame) error {
	return f(ctx, frame)
}

type Policy struct {
	ActionTypes    map[string]struct{}
	Phases         map[storage.Phase]struct{}
	Roles          map[string]struct{}
	Senders        map[string]struct{}
	RecipientHints map[string]struct{}
}

func NewPolicy(types ...string) Policy {
	out := Policy{ActionTypes: map[string]struct{}{}}
	for _, typ := range types {
		if strings.TrimSpace(typ) != "" {
			out.ActionTypes[typ] = struct{}{}
		}
	}
	return out
}

type Runtime struct {
	SessionID string
	Member    string
	Role      string
	Stream    StreamClient
	Cursors   CursorStore
	Handler   ActionHandler
	Policy    Policy
}

func (r *Runtime) RunOnce(ctx context.Context) error {
	if strings.TrimSpace(r.SessionID) == "" || strings.TrimSpace(r.Member) == "" {
		return fmt.Errorf("session_id and member are required")
	}
	if r.Stream == nil || r.Cursors == nil || r.Handler == nil {
		return fmt.Errorf("stream, cursor store, and handler are required")
	}
	since, err := r.Cursors.Load(r.SessionID, r.Member)
	if err != nil {
		return err
	}
	frames, err := r.Stream.Replay(ctx, ReplayRequest{SessionID: r.SessionID, Member: r.Member, Since: since, Follow: true})
	if err != nil {
		return err
	}
	var previousOffset int64 = -1
	if since != "" {
		offset, _, err := storage.ParseCursor(since)
		if err != nil {
			return err
		}
		previousOffset = offset
	}
	seenLive := false
	for _, frame := range frames {
		offset, eventID, err := storage.ParseCursor(frame.Cursor)
		if err != nil {
			return err
		}
		if frame.Event.SchemaVersion != protocol.SchemaVersion {
			return fmt.Errorf("unknown event schema_version %d", frame.Event.SchemaVersion)
		}
		if frame.Event.EventID != eventID {
			return fmt.Errorf("corrupt stream frame: cursor event id does not match event")
		}
		if offset != previousOffset+1 {
			return fmt.Errorf("cursor gap: got offset %d after %d", offset, previousOffset)
		}
		if seenLive && frame.IsReplay {
			return fmt.Errorf("replay frame appeared after live frame")
		}
		if !frame.IsReplay {
			seenLive = true
		}
		if r.shouldAct(frame.Event) {
			if err := r.Handler.Handle(ctx, frame); err != nil && !errors.Is(err, ErrDurableFailureRecorded) {
				return err
			}
		}
		if err := r.Stream.Ack(ctx, AckRequest{SessionID: r.SessionID, Member: r.Member, Cursor: frame.Cursor, EventID: frame.Event.EventID}); err != nil {
			return err
		}
		if err := r.Cursors.Save(r.SessionID, r.Member, frame.Cursor); err != nil {
			return err
		}
		previousOffset = offset
	}
	return nil
}

func (r *Runtime) shouldAct(event storage.EventEnvelope) bool {
	if event.From == r.Member {
		return false
	}
	if len(r.Policy.ActionTypes) == 0 {
		return false
	}
	if _, ok := r.Policy.ActionTypes[event.Type]; !ok {
		return false
	}
	if len(r.Policy.Senders) > 0 {
		if _, ok := r.Policy.Senders[event.From]; !ok {
			return false
		}
	}
	if len(r.Policy.Phases) > 0 {
		if _, ok := r.Policy.Phases[event.Phase]; !ok {
			return false
		}
	}
	if len(r.Policy.Roles) > 0 {
		if _, ok := r.Policy.Roles[r.Role]; !ok {
			return false
		}
	}
	if len(r.Policy.RecipientHints) > 0 && !r.recipientHintMatches(event.To) {
		return false
	}
	return true
}

func (r *Runtime) recipientHintMatches(recipients []string) bool {
	for _, recipient := range recipients {
		if _, ok := r.Policy.RecipientHints[recipient]; ok {
			return true
		}
		if recipient == r.Member {
			if _, ok := r.Policy.RecipientHints["self"]; ok {
				return true
			}
			if _, ok := r.Policy.RecipientHints["$member"]; ok {
				return true
			}
		}
	}
	return false
}

type FileCursorStore struct {
	Root string
}

func (s FileCursorStore) Load(sessionID, member string) (string, error) {
	path, err := s.path(sessionID, member)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	cursor := strings.TrimSpace(string(content))
	if cursor == "" {
		return "", nil
	}
	if _, _, err := storage.ParseCursor(cursor); err != nil {
		return "", err
	}
	return cursor, nil
}

func (s FileCursorStore) Save(sessionID, member, cursor string) error {
	if _, _, err := storage.ParseCursor(cursor); err != nil {
		return err
	}
	path, err := s.path(sessionID, member)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(cursor+"\n"), 0o600)
}

func (s FileCursorStore) path(sessionID, member string) (string, error) {
	if strings.Contains(sessionID, "/") || strings.Contains(member, "/") || strings.Contains(sessionID, "\x00") || strings.Contains(member, "\x00") {
		return "", fmt.Errorf("unsafe cursor key")
	}
	return filepath.Join(filepath.Clean(s.Root), sessionID, member+".cursor"), nil
}

type DispatchLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *DispatchLocks) Lock(sessionID, member string) func() {
	key := sessionID + "\x00" + member
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	lock := l.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		l.locks[key] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func ActionTypes(types map[string]struct{}) []string {
	out := make([]string, 0, len(types))
	for typ := range types {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}
