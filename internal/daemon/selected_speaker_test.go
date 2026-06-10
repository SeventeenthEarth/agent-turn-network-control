package daemon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/daemon"
	"kkachi-agent-network-control/internal/memberruntime"
	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/runner"
	"kkachi-agent-network-control/internal/storage"
)

func TestSelectedSpeakerDispatchInvokesSelectedMemberThroughRunnerAndRecordsSpeech(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_selected_speaker", SessionType: storage.SessionTypeCouncil, Title: "MEMBR-002", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_selected_speaker", CommandID: "cmd_create_selected_speaker"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	speaker := appendSpeakerSelected(t, sessionDir, metadata, "evt_speaker_selected_agent_1", "cmd_council_grant", "agent-1")
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "isolated wrapper speech evidence"}, Cost: &runner.Cost{TokensIn: 2, TokensOut: 3, USDEstimate: 0.04, Source: runner.HermesAgentCostSource}}}}}
	handler := daemon.SelectedSpeakerDispatchHandler{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}}
	stream := &selectedSpeakerStream{frames: framesThroughSpeaker(t, sessionDir, metadata, speaker.EventID)}
	rt := memberruntime.Runtime{SessionID: metadata.ID, Member: "agent-1", Role: "assignee", Stream: stream, Cursors: &selectedSpeakerCursorStore{}, Policy: memberruntime.Policy{ActionTypes: memberruntime.NewPolicy("speaker_selected").ActionTypes, RecipientHints: map[string]struct{}{"self": {}}}, Handler: handler}

	if err := rt.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce selected speaker dispatch failed: %v", err)
	}
	if adapter.calls != 1 || len(adapter.reqs) != 1 {
		t.Fatalf("selected speaker should invoke runner once, calls=%d reqs=%#v", adapter.calls, adapter.reqs)
	}
	if got := adapter.reqs[0].Member.ID; got != "agent-1" {
		t.Fatalf("runner must use selected registry member, got %q", got)
	}
	if adapter.reqs[0].ResolvedWrapper != wrapper {
		t.Fatalf("runner must use resolved wrapper path, got %q want %q", adapter.reqs[0].ResolvedWrapper, wrapper)
	}

	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	started := findEvent(t, index.Events, "runner_invocation_started")
	speech := findEvent(t, index.Events, "speech")
	if started.CausationEventID != speaker.EventID || speech.CausationEventID != speaker.EventID {
		t.Fatalf("started/speech must point back to speaker_selected: started=%q speech=%q want=%q", started.CausationEventID, speech.CausationEventID, speaker.EventID)
	}
	if started.Runner.InvocationID == "" || speech.Runner.InvocationID != started.Runner.InvocationID {
		t.Fatalf("speech must preserve runner invocation id: started=%#v speech=%#v", started.Runner, speech.Runner)
	}
	if started.Runner.Member != "agent-1" || speech.From != "agent-1" {
		t.Fatalf("terminal speech must originate from selected member with runner evidence: started=%#v speech=%#v", started, speech)
	}
	if started.Payload["wrapper_path_sha256"] == "" || started.Payload["adapter_kind"] != runner.HermesAgentKind {
		t.Fatalf("started payload missing redacted wrapper/backend evidence: %#v", started.Payload)
	}
	if len(stream.acks) != 2 || stream.acks[len(stream.acks)-1].EventID != speaker.EventID {
		t.Fatalf("runtime should ack speaker_selected after durable success evidence, acks=%#v", stream.acks)
	}
}

func TestSelectedSpeakerDispatchRecordsDurableFailureBeforeAck(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_selected_speaker_fail", SessionType: storage.SessionTypeCouncil, Title: "MEMBR-002 failure", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_selected_speaker_fail", CommandID: "cmd_create_selected_speaker_fail"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	speaker := appendSpeakerSelected(t, sessionDir, metadata, "evt_speaker_selected_agent_1_fail", "cmd_council_grant_fail", "agent-1")
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: false, ErrorClass: "timeout"}, err: errors.New("timeout")}}}
	handler := daemon.SelectedSpeakerDispatchHandler{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}}
	stream := &selectedSpeakerStream{frames: framesThroughSpeaker(t, sessionDir, metadata, speaker.EventID)}
	rt := memberruntime.Runtime{SessionID: metadata.ID, Member: "agent-1", Stream: stream, Cursors: &selectedSpeakerCursorStore{}, Policy: memberruntime.Policy{ActionTypes: memberruntime.NewPolicy("speaker_selected").ActionTypes, RecipientHints: map[string]struct{}{"self": {}}}, Handler: handler}

	if err := rt.RunOnce(context.Background()); err != nil {
		t.Fatalf("durable runner failure should let runtime ack, got: %v", err)
	}
	if adapter.calls != 1 {
		t.Fatalf("selected speaker should attempt runner once, calls=%d", adapter.calls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	started := findEvent(t, index.Events, "runner_invocation_started")
	failure := findEvent(t, index.Events, "runner_invocation_failed")
	if failure.CausationEventID != speaker.EventID || failure.Runner.InvocationID != started.Runner.InvocationID {
		t.Fatalf("failure must preserve causation and invocation id: started=%#v failure=%#v", started, failure)
	}
	if string(failure.Cost) != "null" || failure.Payload["reason"] != "dispatch_timeout" {
		t.Fatalf("failure must be durable null-cost timeout evidence, cost=%s payload=%#v", failure.Cost, failure.Payload)
	}
	if len(stream.acks) != 2 || stream.acks[len(stream.acks)-1].EventID != speaker.EventID {
		t.Fatalf("runtime should ack only after durable failure evidence, acks=%#v", stream.acks)
	}
}

func appendSpeakerSelected(t *testing.T, sessionDir string, metadata *storage.SessionMetadata, eventID, commandID, member string) storage.EventEnvelope {
	t.Helper()
	event := storage.EventEnvelope{SchemaVersion: protocol.SchemaVersion, EventID: eventID, CommandID: commandID, CorrelationID: metadata.ID, SessionID: metadata.ID, SessionType: metadata.SessionType, Phase: storage.Phase("discussion"), Type: "speaker_selected", From: metadata.Moderator, To: []string{member}, CreatedAt: time.Date(2026, 6, 10, 21, 30, 0, 0, time.UTC), Payload: map[string]any{"turn": 1, "member": member, "selection_mode": "manual"}}
	if _, err := storage.AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent speaker_selected: %v", err)
	}
	return event
}

func framesThroughSpeaker(t *testing.T, sessionDir string, metadata *storage.SessionMetadata, speakerEventID string) []storage.StreamFrame {
	t.Helper()
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex frames: %v", err)
	}
	frames := make([]storage.StreamFrame, 0, len(index.Events))
	for offset, event := range index.Events {
		frames = append(frames, storage.StreamFrame{Cursor: storage.CursorFor(int64(offset), event.EventID), IsReplay: true, Event: event})
		if event.EventID == speakerEventID {
			return frames
		}
	}
	t.Fatalf("speaker event %q not found in %#v", speakerEventID, index.Events)
	return nil
}

func findEvent(t *testing.T, events []storage.EventEnvelope, typ string) storage.EventEnvelope {
	t.Helper()
	for _, event := range events {
		if event.Type == typ {
			return event
		}
	}
	t.Fatalf("missing event type %q in %#v", typ, events)
	return storage.EventEnvelope{}
}

type selectedSpeakerStream struct {
	frames []storage.StreamFrame
	acks   []memberruntime.AckRequest
}

func (s *selectedSpeakerStream) Replay(ctx context.Context, req memberruntime.ReplayRequest) ([]storage.StreamFrame, error) {
	return append([]storage.StreamFrame(nil), s.frames...), nil
}

func (s *selectedSpeakerStream) Ack(ctx context.Context, req memberruntime.AckRequest) error {
	s.acks = append(s.acks, req)
	return nil
}

type selectedSpeakerCursorStore struct{ cursor string }

func (s *selectedSpeakerCursorStore) Load(string, string) (string, error) { return s.cursor, nil }
func (s *selectedSpeakerCursorStore) Save(_, _, cursor string) error {
	s.cursor = cursor
	return nil
}
