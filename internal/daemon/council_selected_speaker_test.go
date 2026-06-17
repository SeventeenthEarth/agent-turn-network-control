package daemon_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/daemon"
	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/runner"
	"kkachi-agent-network-control/internal/storage"
)

func TestCouncilGrantDispatchesSelectedMemberRunnerAndRecordsSpeech(t *testing.T) {
	dataHome, metadata, sessionDir := createDiscussionCouncilForDispatch(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "canonical selected runner speech"}, Cost: &runner.Cost{TokensIn: 2, TokensOut: 3, USDEstimate: 0.04, Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_dispatch", "agent-1"))
	if !response.OK {
		t.Fatalf("council.grant should succeed: %+v", response)
	}
	if adapter.calls != 1 || len(adapter.reqs) != 1 {
		t.Fatalf("grant should invoke selected runner once, calls=%d reqs=%#v", adapter.calls, adapter.reqs)
	}
	if adapter.reqs[0].Member.ID != "agent-1" || adapter.reqs[0].ResolvedWrapper == "" {
		t.Fatalf("runner must use selected snapshot member and resolved wrapper: %#v", adapter.reqs[0])
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	selected := findEvent(t, index.Events, "speaker_selected")
	started := findEvent(t, index.Events, "runner_invocation_started")
	speech := findEvent(t, index.Events, "speech")
	if started.CausationEventID != selected.EventID || speech.CausationEventID != selected.EventID {
		t.Fatalf("runner events must point to speaker_selected causation: selected=%s started=%s speech=%s", selected.EventID, started.CausationEventID, speech.CausationEventID)
	}
	if speech.Runner.InvocationID == "" || speech.Runner.InvocationID != started.Runner.InvocationID || speech.Runner.Member != "agent-1" {
		t.Fatalf("speech must preserve invocation/member evidence: started=%#v speech=%#v", started.Runner, speech.Runner)
	}
}

func TestCouncilGrantRejectsGenericRunnerSuccessAsSelectedRunnerPass(t *testing.T) {
	dataHome, metadata, sessionDir := createDiscussionCouncilForDispatch(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "assignee_update", SemanticStatus: "succeeded", Payload: map[string]any{"message": "generic success"}, Cost: &runner.Cost{TokensIn: 1, TokensOut: 1, Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_generic", "agent-1"))
	if !response.OK {
		t.Fatalf("council.grant should return after durable discard: %+v", response)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if eventTypeCount(index.Events, "assignee_update") != 0 {
		t.Fatalf("generic assignee_update must not be recorded as selected-runner success: %#v", index.Events)
	}
	discarded := findEvent(t, index.Events, "runner_result_discarded")
	if discarded.Payload["reason"] != "selected_runner_requires_canonical_speech" || discarded.Payload["discarded_event_type"] != "assignee_update" {
		t.Fatalf("discarded event should explain selected-runner rejection: %#v", discarded.Payload)
	}
}

func TestCouncilGrantRejectsMissingOrMalformedSelectedRunnerSpeechPayload(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]any
		wantReason  string
		wantMessage string
	}{
		{
			name:        "missing",
			payload:     nil,
			wantReason:  "selected_runner_speech_payload_missing",
			wantMessage: "selected runner speech payload is required",
		},
		{
			name:        "malformed",
			payload:     map[string]any{"turn": 1},
			wantReason:  "selected_runner_speech_payload_invalid",
			wantMessage: "speech is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataHome, metadata, sessionDir := createDiscussionCouncilForDispatch(t)
			adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: tt.payload, Cost: &runner.Cost{TokensIn: 1, TokensOut: 1, Source: runner.HermesAgentCostSource}}}}}
			server := daemon.NewServer(dataHome, daemonFixedRuntime())
			server.RunnerAdapter = adapter

			response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_payload_"+tt.name, "agent-1"))
			if !response.OK {
				t.Fatalf("council.grant should return after durable discard: %+v", response)
			}
			index, err := storage.ReadLogIndex(sessionDir, metadata)
			if err != nil {
				t.Fatalf("ReadLogIndex: %v", err)
			}
			if eventTypeCount(index.Events, "speech") != 0 {
				t.Fatalf("invalid selected-runner speech payload must not append speech: %#v", index.Events)
			}
			discarded := findEvent(t, index.Events, "runner_result_discarded")
			if discarded.Payload["reason"] != tt.wantReason {
				t.Fatalf("discarded event reason=%#v want %q payload=%#v", discarded.Payload["reason"], tt.wantReason, discarded.Payload)
			}
			if got := discarded.Payload["validation_error"]; got == nil || !strings.Contains(got.(string), tt.wantMessage) {
				t.Fatalf("discarded event should expose validation error containing %q: %#v", tt.wantMessage, discarded.Payload)
			}
		})
	}
}

func TestCouncilGrantDispatchIsDurablyIdempotentAcrossReplayAndRestart(t *testing.T) {
	dataHome, metadata, sessionDir := createDiscussionCouncilForDispatch(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "once"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	first := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_idempotent", "agent-1"))
	if !first.OK {
		t.Fatalf("first grant failed: %+v", first)
	}
	second := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_idempotent", "agent-1"))
	if !second.OK {
		t.Fatalf("replayed grant failed: %+v", second)
	}
	restartedAdapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "twice"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	restarted := daemon.NewServer(dataHome, daemonFixedRuntime())
	restarted.RunnerAdapter = restartedAdapter
	third := restarted.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_idempotent", "agent-1"))
	if !third.OK {
		t.Fatalf("restart replay failed: %+v", third)
	}
	if adapter.calls != 1 || restartedAdapter.calls != 0 {
		t.Fatalf("durable replay/restart idempotency failed, first calls=%d restarted calls=%d", adapter.calls, restartedAdapter.calls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if eventTypeCount(index.Events, "runner_invocation_started") != 1 || eventTypeCount(index.Events, "speech") != 1 {
		t.Fatalf("duplicate replay should not append second runner result: %#v", index.Events)
	}
}

func TestCouncilGrantStartedOnlyReplayRecordsStaleIncompleteDispatch(t *testing.T) {
	dataHome, metadata, sessionDir := createDiscussionCouncilForDispatch(t)
	grant, _, err := storage.RecordCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{
		Action:    "grant",
		Actor:     "agent-mod",
		CommandID: "cmd_council_grant_started_only",
		Payload: map[string]any{
			"turn":           1,
			"member":         "agent-1",
			"selection_mode": "moderator_direct",
		},
		Now: daemonFixedRuntime().Now().Add(7 * time.Second),
	})
	if err != nil {
		t.Fatalf("RecordCouncilEvent grant: %v", err)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex before started: %v", err)
	}
	selected, ok := eventByID(index.Events, grant.EventID)
	if !ok {
		t.Fatalf("speaker_selected %q not found", grant.EventID)
	}
	started := storage.EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          "evt_started_only_selected_runner",
		CommandID:        "cmd_council_grant_started_only",
		CausationEventID: selected.EventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            selected.Phase,
		Type:             "runner_invocation_started",
		From:             "kkachi-agent-networkd",
		To:               []string{metadata.Moderator},
		CreatedAt:        daemonFixedRuntime().Now().Add(8 * time.Second),
		Runner:           &storage.RunnerInfo{InvocationID: "run_started_only_selected_runner", AdapterKind: runner.HermesAgentKind, Member: "agent-1", Attempt: 1, SourceCommandID: "cmd_council_grant_started_only", Status: "started"},
		Payload:          map[string]any{"member_id": "agent-1", "adapter_kind": runner.HermesAgentKind},
	}
	if _, err := storage.AppendEvent(sessionDir, metadata, started); err != nil {
		t.Fatalf("AppendEvent started-only evidence: %v", err)
	}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_started_only", "agent-1"))
	if !response.OK {
		t.Fatalf("replayed grant should record stale diagnostic: %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("started-only replay policy should record stale diagnostic instead of duplicate launch, calls=%d", adapter.calls)
	}
	index, err = storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex after replay: %v", err)
	}
	diagnostic := findEvent(t, index.Events, "selected_runner_dispatch_failed")
	if diagnostic.Payload["reason"] != "selected_runner_dispatch_incomplete_stale" || diagnostic.CausationEventID != selected.EventID {
		t.Fatalf("started-only replay should record stale incomplete diagnostic: %#v", diagnostic)
	}
	if eventTypeCount(index.Events, "speech") != 0 || eventTypeCount(index.Events, "runner_invocation_started") != 1 {
		t.Fatalf("started-only replay should not append terminal speech or duplicate started event: %#v", index.Events)
	}
}

func TestCouncilGrantBoundedDispatchTimeoutRecordsFailure(t *testing.T) {
	dataHome, metadata, sessionDir := createDiscussionCouncilForDispatch(t)
	adapter := blockingSelectedSpeakerAdapter{}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.SelectedSpeakerTimeout = 5 * time.Millisecond

	start := time.Now()
	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_timeout", "agent-1"))
	elapsed := time.Since(start)
	if !response.OK {
		t.Fatalf("council.grant should return after durable timeout failure: %+v", response)
	}
	if elapsed > time.Second {
		t.Fatalf("bounded dispatch wedged too long: %s", elapsed)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	failure := findEvent(t, index.Events, "runner_invocation_failed")
	if failure.Payload["reason"] != "dispatch_timeout" || failure.CausationEventID == "" {
		t.Fatalf("timeout failure must be durable and causally linked: %#v", failure)
	}
}

func createDiscussionCouncilForDispatch(t *testing.T) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHome(t)
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_council_dispatch_" + name, Title: "RUNFIX-003", Moderator: "agent-mod", EventID: "evt_created_" + name, CommandID: "cmd_created_" + name},
		Members: []string{"agent-1"},
		Now:     daemonFixedRuntime().Now(),
	}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_attendance_"+name, map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_attend_"+name, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_"+name, map[string]any{"decision_question": "What should ship?"}, 3*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_"+name, map[string]any{"timeout_sec": 30}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_"+name, map[string]any{"turn": 1}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_"+name, map[string]any{"turn": 1, "intent": "answer", "reason": "selected"}, 6*time.Second)
	return dataHome, metadata, sessionDir
}

func appendCouncilEventForDispatch(t *testing.T, sessionDir string, metadata *storage.SessionMetadata, action, actor, commandID string, payload map[string]any, delta time.Duration) {
	t.Helper()
	if _, _, err := storage.RecordCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{Action: action, Actor: actor, CommandID: commandID, Payload: payload, Now: daemonFixedRuntime().Now().Add(delta)}); err != nil {
		t.Fatalf("RecordCouncilEvent(%s): %v", action, err)
	}
}

func councilGrantRequest(sessionID, commandID, member string) protocol.CommandRequest {
	return protocol.NewRequest(commandID, "council.grant", map[string]any{
		"session_id": sessionID,
		"actor":      "agent-mod",
		"command_id": commandID,
		"payload": map[string]any{
			"turn":           1,
			"member":         member,
			"selection_mode": "moderator_direct",
		},
	})
}

func eventTypeCount(events []storage.EventEnvelope, typ string) int {
	count := 0
	for _, event := range events {
		if event.Type == typ {
			count++
		}
	}
	return count
}

func eventByID(events []storage.EventEnvelope, eventID string) (storage.EventEnvelope, bool) {
	for _, event := range events {
		if event.EventID == eventID {
			return event, true
		}
	}
	return storage.EventEnvelope{}, false
}

type blockingSelectedSpeakerAdapter struct{}

func (blockingSelectedSpeakerAdapter) Kind() string       { return runner.HermesAgentKind }
func (blockingSelectedSpeakerAdapter) CostSource() string { return runner.HermesAgentCostSource }
func (blockingSelectedSpeakerAdapter) Send(ctx context.Context, req runner.Request) (runner.Result, error) {
	<-ctx.Done()
	return runner.Result{OK: false, ErrorClass: "timeout", Cost: nil}, ctx.Err()
}
func (blockingSelectedSpeakerAdapter) Resume(ctx context.Context, req runner.Request) (runner.Result, error) {
	return blockingSelectedSpeakerAdapter{}.Send(ctx, req)
}
func (blockingSelectedSpeakerAdapter) Cancel(context.Context, runner.SessionHandle) error { return nil }
func (blockingSelectedSpeakerAdapter) ParseSessionHandle([]byte) (*runner.SessionHandle, error) {
	return nil, nil
}
