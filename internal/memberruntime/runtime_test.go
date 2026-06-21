package memberruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hun-control/internal/protocol"
	"hun-control/internal/storage"
)

func TestRuntimeReplayFirstThenLiveAckAndPersist(t *testing.T) {
	frames := []storage.StreamFrame{
		frame(0, true, "evt_created", "session_created", "kkachi-agent-networkd", []string{"agent-1"}),
		frame(1, true, "evt_private_to_other", "runner_dispatch_requested", "agent-mod", []string{"agent-2"}),
		frame(2, false, "evt_live", "assignee_update", "agent-2", []string{}),
	}
	stream := &fakeStream{frames: frames}
	store := &memoryCursorStore{}
	var handled []string
	rt := Runtime{SessionID: "sess_test", Member: "agent-1", Role: "assignee", Stream: stream, Cursors: store, Policy: NewPolicy("runner_dispatch_requested"), Handler: HandlerFunc(func(ctx context.Context, frame storage.StreamFrame) error {
		handled = append(handled, frame.Event.EventID)
		return nil
	})}
	if err := rt.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if len(handled) != 1 || handled[0] != "evt_private_to_other" {
		t.Fatalf("runtime must not use to as visibility control; handled=%#v", handled)
	}
	if len(stream.acks) != 3 {
		t.Fatalf("all processed frames should be acked, got %#v", stream.acks)
	}
	if got := store.cursor; got != "cur_000000000002_evt_live" {
		t.Fatalf("cursor persisted after ack expected latest, got %q", got)
	}
	if !stream.replayReq.Follow || stream.replayReq.Since != "" {
		t.Fatalf("unexpected replay request: %#v", stream.replayReq)
	}
}

func TestRuntimeActionAckAfterDurableFailButNotPlainError(t *testing.T) {
	for _, tc := range []struct {
		name       string
		handlerErr error
		wantAck    bool
	}{
		{name: "durable fail recorded", handlerErr: ErrDurableFailureRecorded, wantAck: true},
		{name: "plain failure", handlerErr: errors.New("local failure"), wantAck: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &fakeStream{frames: []storage.StreamFrame{frame(0, true, "evt_action", "runner_dispatch_requested", "agent-mod", []string{"agent-1"})}}
			store := &memoryCursorStore{}
			rt := Runtime{SessionID: "sess_test", Member: "agent-1", Stream: stream, Cursors: store, Policy: NewPolicy("runner_dispatch_requested"), Handler: HandlerFunc(func(context.Context, storage.StreamFrame) error { return tc.handlerErr })}
			err := rt.RunOnce(context.Background())
			if tc.wantAck {
				if err != nil || len(stream.acks) != 1 || store.cursor == "" {
					t.Fatalf("durable failure should ack/persist, err=%v acks=%#v cursor=%q", err, stream.acks, store.cursor)
				}
			} else if err == nil || len(stream.acks) != 0 || store.cursor != "" {
				t.Fatalf("plain failure should stop before ack/persist, err=%v acks=%#v cursor=%q", err, stream.acks, store.cursor)
			}
		})
	}
}

func TestRuntimeFailsClosedOnMalformedCursorGapCorruptFrameAndUnknownSchema(t *testing.T) {
	cases := []struct {
		name   string
		store  *memoryCursorStore
		frames []storage.StreamFrame
	}{
		{name: "malformed cursor store", store: &memoryCursorStore{cursor: "bad"}, frames: nil},
		{name: "cursor gap", store: &memoryCursorStore{}, frames: []storage.StreamFrame{frame(1, true, "evt_gap", "session_created", "kkachi-agent-networkd", []string{"agent-1"})}},
		{name: "corrupt frame", store: &memoryCursorStore{}, frames: []storage.StreamFrame{{Cursor: "cur_000000000000_evt_a", IsReplay: true, Event: event("evt_b", "session_created", "kkachi-agent-networkd", []string{"agent-1"})}}},
		{name: "missing schema", store: &memoryCursorStore{}, frames: []storage.StreamFrame{{Cursor: "cur_000000000000_evt_a", IsReplay: true, Event: func() storage.EventEnvelope {
			e := event("evt_a", "session_created", "kkachi-agent-networkd", []string{"agent-1"})
			e.SchemaVersion = 0
			return e
		}()}}},
		{name: "unknown schema", store: &memoryCursorStore{}, frames: []storage.StreamFrame{{Cursor: "cur_000000000000_evt_a", IsReplay: true, Event: func() storage.EventEnvelope {
			e := event("evt_a", "session_created", "kkachi-agent-networkd", []string{"agent-1"})
			e.SchemaVersion = 99
			return e
		}()}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := &fakeStream{frames: tc.frames}
			rt := Runtime{SessionID: "sess_test", Member: "agent-1", Stream: stream, Cursors: tc.store, Policy: NewPolicy("runner_dispatch_requested"), Handler: HandlerFunc(func(context.Context, storage.StreamFrame) error { return nil })}
			if err := rt.RunOnce(context.Background()); err == nil {
				t.Fatalf("expected fail-closed error")
			}
			if len(stream.acks) != 0 {
				t.Fatalf("fail-closed path should not ack: %#v", stream.acks)
			}
		})
	}
}

func TestRuntimePolicyUsesRecipientHintsAsActionFilterNotVisibility(t *testing.T) {
	frames := []storage.StreamFrame{
		frame(0, true, "evt_to_other", "runner_dispatch_requested", "agent-mod", []string{"agent-2"}),
		frame(1, true, "evt_to_self", "runner_dispatch_requested", "agent-mod", []string{"agent-1"}),
	}
	stream := &fakeStream{frames: frames}
	store := &memoryCursorStore{}
	var handled []string
	rt := Runtime{
		SessionID: "sess_test",
		Member:    "agent-1",
		Role:      "assignee",
		Stream:    stream,
		Cursors:   store,
		Policy:    Policy{ActionTypes: NewPolicy("runner_dispatch_requested").ActionTypes, RecipientHints: map[string]struct{}{"self": {}}},
		Handler: HandlerFunc(func(ctx context.Context, frame storage.StreamFrame) error {
			handled = append(handled, frame.Event.EventID)
			return nil
		}),
	}
	if err := rt.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if len(handled) != 1 || handled[0] != "evt_to_self" {
		t.Fatalf("recipient hint should narrow action to self-addressed event, handled=%#v", handled)
	}
	if len(stream.acks) != 2 || store.cursor != "cur_000000000001_evt_to_self" {
		t.Fatalf("recipient hint must not become stream visibility control: acks=%#v cursor=%q", stream.acks, store.cursor)
	}
}

func TestRuntimePolicyUsesSenderPhaseRoleAndIgnoresSelfOrigin(t *testing.T) {
	frames := []storage.StreamFrame{
		frame(0, true, "evt_wrong_sender", "runner_dispatch_requested", "agent-2", []string{"agent-1"}),
		frame(1, true, "evt_wrong_phase", "runner_dispatch_requested", "agent-mod", []string{"agent-1"}),
		frame(2, true, "evt_self_origin", "runner_dispatch_requested", "agent-1", []string{"agent-1"}),
		frame(3, true, "evt_action", "runner_dispatch_requested", "agent-mod", []string{"agent-1"}),
	}
	frames[0].Event.Phase = storage.Phase("working")
	frames[1].Event.Phase = storage.PhaseCreated
	frames[2].Event.Phase = storage.Phase("working")
	frames[3].Event.Phase = storage.Phase("working")
	stream := &fakeStream{frames: frames}
	store := &memoryCursorStore{}
	var handled []string
	rt := Runtime{
		SessionID: "sess_test",
		Member:    "agent-1",
		Role:      "assignee",
		Stream:    stream,
		Cursors:   store,
		Policy: Policy{
			ActionTypes: NewPolicy("runner_dispatch_requested").ActionTypes,
			Senders:     map[string]struct{}{"agent-mod": {}},
			Phases:      map[storage.Phase]struct{}{storage.Phase("working"): {}},
			Roles:       map[string]struct{}{"assignee": {}},
		},
		Handler: HandlerFunc(func(ctx context.Context, frame storage.StreamFrame) error {
			handled = append(handled, frame.Event.EventID)
			return nil
		}),
	}
	if err := rt.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if len(handled) != 1 || handled[0] != "evt_action" {
		t.Fatalf("sender/phase/role policy should handle only matching non-self event, handled=%#v", handled)
	}
	if len(stream.acks) != len(frames) {
		t.Fatalf("ignored policy events should still be acknowledged as processed: %#v", stream.acks)
	}

	stream = &fakeStream{frames: []storage.StreamFrame{frame(0, true, "evt_role_blocked", "runner_dispatch_requested", "agent-mod", []string{"agent-1"})}}
	stream.frames[0].Event.Phase = storage.Phase("working")
	rt.Stream = stream
	rt.Cursors = &memoryCursorStore{}
	rt.Role = "reviewer"
	handled = nil
	if err := rt.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce role-filter path failed: %v", err)
	}
	if len(handled) != 0 || len(stream.acks) != 1 {
		t.Fatalf("role mismatch should ignore action but still ack: handled=%#v acks=%#v", handled, stream.acks)
	}
}

func TestFileCursorStoreRejectsCorruptCursor(t *testing.T) {
	store := FileCursorStore{Root: t.TempDir()}
	path := filepath.Join(store.Root, "sess_test", "agent-1.cursor")
	if err := osMkdirAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(path, []byte("not-a-cursor\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("sess_test", "agent-1"); err == nil {
		t.Fatalf("corrupt cursor store should fail closed")
	}
}

func TestDispatchLocksSerializeSameMemberOnly(t *testing.T) {
	var locks DispatchLocks
	unlock := locks.Lock("sess", "agent-1")
	entered := make(chan struct{}, 1)
	go func() {
		unlock2 := locks.Lock("sess", "agent-1")
		defer unlock2()
		entered <- struct{}{}
	}()
	select {
	case <-entered:
		t.Fatalf("same member lock should block")
	case <-time.After(20 * time.Millisecond):
	}
	unlockOther := locks.Lock("sess", "agent-2")
	unlockOther()
	unlock()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatalf("same member lock did not release")
	}
}

type fakeStream struct {
	frames    []storage.StreamFrame
	replayReq ReplayRequest
	acks      []AckRequest
}

func (f *fakeStream) Replay(ctx context.Context, req ReplayRequest) ([]storage.StreamFrame, error) {
	f.replayReq = req
	return append([]storage.StreamFrame(nil), f.frames...), nil
}
func (f *fakeStream) Ack(ctx context.Context, req AckRequest) error {
	f.acks = append(f.acks, req)
	return nil
}

type memoryCursorStore struct{ cursor string }

func (s *memoryCursorStore) Load(string, string) (string, error) {
	if s.cursor != "" {
		if _, _, err := storage.ParseCursor(s.cursor); err != nil {
			return "", err
		}
	}
	return s.cursor, nil
}
func (s *memoryCursorStore) Save(_, _, cursor string) error { s.cursor = cursor; return nil }

func frame(offset int64, replay bool, eventID, typ, from string, to []string) storage.StreamFrame {
	return storage.StreamFrame{Cursor: storage.CursorFor(offset, eventID), IsReplay: replay, Event: event(eventID, typ, from, to)}
}
func event(eventID, typ, from string, to []string) storage.EventEnvelope {
	return storage.EventEnvelope{SchemaVersion: protocol.SchemaVersion, EventID: eventID, SessionID: "sess_test", SessionType: storage.SessionTypeDelegation, Phase: storage.PhaseCreated, Type: typ, From: from, To: to, CreatedAt: time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC), Payload: map[string]any{}}
}

var osMkdirAll = func(path string) error { return os.MkdirAll(path, 0o700) }
var osWriteFile = func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) }
