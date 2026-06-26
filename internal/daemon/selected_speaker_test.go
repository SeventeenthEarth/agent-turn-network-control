package daemon_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"atn-control/internal/daemon"
	"atn-control/internal/memberruntime"
	"atn-control/internal/protocol"
	"atn-control/internal/registry"
	"atn-control/internal/runner"
	"atn-control/internal/storage"
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
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "message": "isolated wrapper speech evidence"}, Cost: &runner.Cost{TokensIn: 2, TokensOut: 3, USDEstimate: 0.04, Source: runner.HermesAgentCostSource}}}}}
	handler := daemon.SelectedSpeakerDispatchHandler{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, PromptBuilder: selectedSpeakerPromptBuilder()}
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
	succeeded := findEvent(t, index.Events, "runner_invocation_succeeded")
	speech := findEvent(t, index.Events, "speech")
	if started.CausationEventID != speaker.EventID || succeeded.CausationEventID != speaker.EventID || speech.CausationEventID != speaker.EventID {
		t.Fatalf("started/succeeded/speech must point back to speaker_selected: started=%q succeeded=%q speech=%q want=%q", started.CausationEventID, succeeded.CausationEventID, speech.CausationEventID, speaker.EventID)
	}
	if started.Runner.InvocationID == "" || succeeded.Runner.InvocationID != started.Runner.InvocationID || speech.Runner.InvocationID != started.Runner.InvocationID {
		t.Fatalf("success and speech must preserve runner invocation id: started=%#v succeeded=%#v speech=%#v", started.Runner, succeeded.Runner, speech.Runner)
	}
	if started.Runner.Member != "agent-1" || succeeded.Runner.Status != "succeeded" || speech.From != "agent-1" || speech.Payload["speech"] != "isolated wrapper speech evidence" {
		t.Fatalf("terminal speech must originate from selected member with normalized speech payload and runner success evidence: started=%#v succeeded=%#v speech=%#v", started, succeeded, speech)
	}
	if started.Payload["wrapper_path_sha256"] == "" || started.Payload["adapter_kind"] != runner.HermesAgentKind {
		t.Fatalf("started payload missing redacted wrapper/backend evidence: %#v", started.Payload)
	}
	accounting := storage.SelectedRunnerAccountingFromIndex(metadata, index)
	if !accounting.SelectedRunnerPass || accounting.RunnerSucceededCount != 1 || accounting.LinkedRunnerSpeechCount != 1 {
		t.Fatalf("selected speaker runner success should account for succeeded invocation and linked speech: %#v", accounting)
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
	handler := daemon.SelectedSpeakerDispatchHandler{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, PromptBuilder: selectedSpeakerPromptBuilder()}
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

func TestSelectedSpeakerDispatchDeliveryOutputMismatchCannotBeRepairedByFallbackSpeech(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_selected_speaker_delivery_mismatch", SessionType: storage.SessionTypeCouncil, Title: "RUNFIX2-002 delivery mismatch", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_selected_speaker_delivery_mismatch", CommandID: "cmd_create_selected_speaker_delivery_mismatch"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	speaker := appendSpeakerSelected(t, sessionDir, metadata, "evt_speaker_selected_delivery_mismatch", "cmd_council_grant_delivery_mismatch", "agent-1")
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: false, ErrorClass: runner.ErrorClassAdapterCommandMismatch, Payload: map[string]any{"diagnostic_excerpt": "platform_delivery posted [redacted]"}}, err: runner.ErrSemantic}}}
	handler := daemon.SelectedSpeakerDispatchHandler{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, PromptBuilder: selectedSpeakerPromptBuilder()}
	stream := &selectedSpeakerStream{frames: framesThroughSpeaker(t, sessionDir, metadata, speaker.EventID)}
	rt := memberruntime.Runtime{SessionID: metadata.ID, Member: "agent-1", Stream: stream, Cursors: &selectedSpeakerCursorStore{}, Policy: memberruntime.Policy{ActionTypes: memberruntime.NewPolicy("speaker_selected").ActionTypes, RecipientHints: map[string]struct{}{"self": {}}}, Handler: handler}

	if err := rt.RunOnce(context.Background()); err != nil {
		t.Fatalf("durable adapter mismatch should let runtime ack, got: %v", err)
	}
	if _, _, err := storage.RecordCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{
		Action:           "speak",
		Actor:            "agent-1",
		CommandID:        "cmd_manual_fallback_after_delivery_mismatch",
		CausationEventID: speaker.EventID,
		Now:              daemonFixedRuntime().Now().Add(3 * time.Second),
		Payload: map[string]any{
			"speech":           "Manual fallback speech after delivery-only adapter output.",
			"fallback_profile": true,
			"source":           "manual_fallback_profile",
		},
	}); err != nil {
		t.Fatalf("RecordCouncilEvent fallback speech: %v", err)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	failure := findEvent(t, index.Events, "runner_invocation_failed")
	if failure.Payload["error_class"] != runner.ErrorClassAdapterCommandMismatch || failure.Payload["reason"] != runner.ErrorClassAdapterCommandMismatch {
		t.Fatalf("delivery mismatch failure must preserve adapter mismatch diagnostics: %#v", failure.Payload)
	}
	accounting := storage.SelectedRunnerAccountingFromIndex(metadata, index)
	if accounting.SelectedRunnerPass || accounting.RunnerFailedCount != 1 || accounting.RunnerSucceededCount != 0 || accounting.LinkedRunnerSpeechCount != 0 || accounting.ManualOrFallbackSpeechCount != 1 {
		t.Fatalf("fallback speech must not repair delivery-only selected runner failure: %#v", accounting)
	}
}

func TestSelectedSpeakerDispatchRejectsMemberToMismatchBeforeAdapterLaunch(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_selected_speaker_mismatch", SessionType: storage.SessionTypeCouncil, Title: "MEMBR-002 mismatch", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_selected_speaker_mismatch", CommandID: "cmd_create_selected_speaker_mismatch"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true}}}}
	handler := daemon.SelectedSpeakerDispatchHandler{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, PromptBuilder: selectedSpeakerPromptBuilder()}
	event := storage.EventEnvelope{SchemaVersion: protocol.SchemaVersion, EventID: "evt_speaker_selected_mismatch", CommandID: "cmd_council_grant_mismatch", CorrelationID: metadata.ID, SessionID: metadata.ID, SessionType: metadata.SessionType, Phase: storage.Phase("discussion"), Type: "speaker_selected", From: metadata.Moderator, To: []string{"agent-2"}, CreatedAt: time.Date(2026, 6, 10, 21, 30, 0, 0, time.UTC), Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "manual"}}

	err = handler.Handle(context.Background(), storage.StreamFrame{Event: event})
	if err == nil {
		t.Fatalf("member/to mismatch should fail closed")
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter launched despite selected member mismatch")
	}
}

func TestSelectedSpeakerDispatchFailsClosedWithoutUsablePromptBuilder(t *testing.T) {
	tests := []struct {
		name            string
		promptBuilder   func(storage.EventEnvelope, registry.Member) string
		wantConfigured  bool
		wantErrorSubstr string
	}{
		{name: "missing builder", wantConfigured: false, wantErrorSubstr: "required"},
		{name: "empty builder output", promptBuilder: func(storage.EventEnvelope, registry.Member) string { return "   " }, wantConfigured: true, wantErrorSubstr: "empty prompt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataHome, loaded, wrapper := dispatchDataHome(t)
			metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_selected_speaker_prompt_" + strings.NewReplacer("/", "_", " ", "_").Replace(tt.name), SessionType: storage.SessionTypeCouncil, Title: "NEWFIX-001 prompt builder", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_selected_speaker_prompt_" + strings.NewReplacer("/", "_", " ", "_").Replace(tt.name), CommandID: "cmd_create_selected_speaker_prompt_" + strings.NewReplacer("/", "_", " ", "_").Replace(tt.name)}, daemonFixedRuntime())
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
			speaker := appendSpeakerSelected(t, sessionDir, metadata, "evt_speaker_selected_prompt_"+strings.NewReplacer("/", "_", " ", "_").Replace(tt.name), "cmd_council_grant_prompt_"+strings.NewReplacer("/", "_", " ", "_").Replace(tt.name), "agent-1")
			member := loaded.Registry.Members["agent-1"]
			member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
			adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
			handler := daemon.SelectedSpeakerDispatchHandler{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, PromptBuilder: tt.promptBuilder}
			stream := &selectedSpeakerStream{frames: framesThroughSpeaker(t, sessionDir, metadata, speaker.EventID)}
			rt := memberruntime.Runtime{SessionID: metadata.ID, Member: "agent-1", Stream: stream, Cursors: &selectedSpeakerCursorStore{}, Policy: memberruntime.Policy{ActionTypes: memberruntime.NewPolicy("speaker_selected").ActionTypes, RecipientHints: map[string]struct{}{"self": {}}}, Handler: handler}

			if err := rt.RunOnce(context.Background()); err != nil {
				t.Fatalf("prompt-builder failure should let runtime ack after durable diagnostic, got: %v", err)
			}
			if adapter.calls != 0 {
				t.Fatalf("prompt-builder failure must prevent runner launch, calls=%d", adapter.calls)
			}
			index, err := storage.ReadLogIndex(sessionDir, metadata)
			if err != nil {
				t.Fatalf("ReadLogIndex: %v", err)
			}
			if eventTypeCount(index.Events, "runner_invocation_started") != 0 {
				t.Fatalf("prompt-builder failure must not append runner_invocation_started: %#v", index.Events)
			}
			diagnostic := findEvent(t, index.Events, "selected_runner_dispatch_failed")
			if diagnostic.Payload["reason"] != "selected_runner_prompt_missing" || diagnostic.Payload["prompt_builder_configured"] != tt.wantConfigured {
				t.Fatalf("unexpected prompt-builder diagnostic: %#v", diagnostic.Payload)
			}
			if validationError, _ := diagnostic.Payload["validation_error"].(string); !strings.Contains(validationError, tt.wantErrorSubstr) {
				t.Fatalf("prompt-builder diagnostic missing %q: %#v", tt.wantErrorSubstr, diagnostic.Payload)
			}
			if diagnostic.CausationEventID != speaker.EventID {
				t.Fatalf("prompt-builder diagnostic must preserve causation: %#v", diagnostic)
			}
			if len(stream.acks) != 2 || stream.acks[len(stream.acks)-1].EventID != speaker.EventID {
				t.Fatalf("runtime should ack speaker_selected after durable prompt-builder diagnostic, acks=%#v", stream.acks)
			}
		})
	}
}

func selectedSpeakerPromptBuilder() func(storage.EventEnvelope, registry.Member) string {
	return func(storage.EventEnvelope, registry.Member) string {
		return "projection-backed selected-runner prompt"
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
