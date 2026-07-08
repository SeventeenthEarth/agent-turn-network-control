package memberruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SeventeenthEarth/agent-turn-network-control/internal/runner"
	"github.com/SeventeenthEarth/agent-turn-network-control/internal/storage"
)

// ParticipantSession is the control-owned PRSLR-004 local participant runtime
// registry row. It is council-scoped: a handle may be reused for the same
// session/member pair only, never across councils.
type ParticipantSession struct {
	SessionID   string `json:"session_id"`
	Member      string `json:"member"`
	Handle      string `json:"participant_session_handle"`
	Generation  int    `json:"generation"`
	LastCursor  string `json:"last_cursor,omitempty"`
	LastEventID string `json:"last_event_id,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// ParticipantSessionRegistry is a deterministic in-process registry used by
// bounded local-control runtime seams. It is intentionally fail-closed on stale
// cursor movement and unsafe keys; durable evidence is written by storage event
// payloads, while this registry gives daemon dispatch a stable handle primitive.
type ParticipantSessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]ParticipantSession
}

func NewParticipantSessionRegistry() *ParticipantSessionRegistry {
	return &ParticipantSessionRegistry{sessions: map[string]ParticipantSession{}}
}

func DeterministicParticipantSessionHandle(sessionID, member string) runner.SessionHandle {
	sessionID = strings.TrimSpace(sessionID)
	member = strings.TrimSpace(member)
	sum := sha256.Sum256([]byte(sessionID + "\x00" + member))
	return runner.SessionHandle("psh_" + hex.EncodeToString(sum[:])[:24])
}

func (r *ParticipantSessionRegistry) OpenCouncilSessions(sessionID string, members []string, now time.Time) (map[string]ParticipantSession, error) {
	if r == nil {
		return nil, fmt.Errorf("participant session registry is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("at least one member is required")
	}
	ordered := append([]string(nil), members...)
	sort.Strings(ordered)
	out := map[string]ParticipantSession{}
	for _, member := range ordered {
		session, err := r.OpenCouncilSession(sessionID, member, now)
		if err != nil {
			return nil, err
		}
		out[session.Member] = session
	}
	return out, nil
}

func (r *ParticipantSessionRegistry) OpenCouncilSession(sessionID, member string, now time.Time) (ParticipantSession, error) {
	if r == nil {
		return ParticipantSession{}, fmt.Errorf("participant session registry is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	member = strings.TrimSpace(member)
	if unsafeParticipantSessionKey(sessionID) || unsafeParticipantSessionKey(member) {
		return ParticipantSession{}, fmt.Errorf("unsafe participant session key")
	}
	key := sessionID + "\x00" + member
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions == nil {
		r.sessions = map[string]ParticipantSession{}
	}
	if existing, ok := r.sessions[key]; ok {
		return existing, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	session := ParticipantSession{
		SessionID:  sessionID,
		Member:     member,
		Handle:     string(DeterministicParticipantSessionHandle(sessionID, member)),
		Generation: 1,
		UpdatedAt:  now.UTC().Format(time.RFC3339Nano),
	}
	r.sessions[key] = session
	return session, nil
}

func (r *ParticipantSessionRegistry) ObserveCursor(sessionID, member, cursor, eventID, coverageKind string, now time.Time) error {
	if r == nil {
		return fmt.Errorf("participant session registry is required")
	}
	offset, cursorEventID, err := storage.ParseCursor(cursor)
	if err != nil {
		return err
	}
	if strings.TrimSpace(eventID) == "" || cursorEventID != strings.TrimSpace(eventID) {
		return fmt.Errorf("cursor event id does not match observed event")
	}
	session, err := r.OpenCouncilSession(sessionID, member, now)
	if err != nil {
		return err
	}
	if session.LastCursor != "" {
		previousOffset, _, err := storage.ParseCursor(session.LastCursor)
		if err != nil {
			return err
		}
		if offset <= previousOffset {
			return fmt.Errorf("stale participant cursor")
		}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	session.LastCursor = cursor
	session.LastEventID = eventID
	session.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	key := strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(member)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions == nil {
		r.sessions = map[string]ParticipantSession{}
	}
	r.sessions[key] = session
	_ = coverageKind
	return nil
}

func (r *ParticipantSessionRegistry) Snapshot(sessionID string) []ParticipantSession {
	if r == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []ParticipantSession{}
	for _, session := range r.sessions {
		if session.SessionID == sessionID {
			out = append(out, session)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Member < out[j].Member })
	return out
}

func unsafeParticipantSessionKey(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.Contains(value, "/") || strings.Contains(value, "\x00")
}
