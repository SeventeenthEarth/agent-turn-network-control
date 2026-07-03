package daemon_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"atn-control/internal/daemon"
	"atn-control/internal/protocol"
	"atn-control/internal/runner"
	"atn-control/internal/storage"
)

func TestCOUNCILSTAB001LiveVisibleSelectedRunnerFifteenTurnGoldenPath(t *testing.T) {
	dataHome, metadata, sessionDir := createGuardedVisibleCouncilForDispatch(t, map[string]any{
		"kind":       "discord_thread",
		"platform":   "discord",
		"channel_id": "chan-council-stab",
		"thread_id":  "thread-council-stab",
	}, 120, nil)
	adapter := &fakeRunRTAdapter{}
	for turn := 1; turn <= 15; turn++ {
		adapter.results = append(adapter.results, fakeRunRTResult{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": turn, "speech": fmt.Sprintf("canonical selected-runner turn %02d", turn)}, Cost: &runner.Cost{TokensIn: 2, TokensOut: 3, Source: runner.HermesAgentCostSource}}})
		adapter.visibleResults = append(adapter.visibleResults, fakeVisibleDeliveryResult{result: runner.VisibleDeliveryResult{OK: true, Status: "posted", Kind: "discord_thread", Platform: "discord", ChannelID: "chan-council-stab", ThreadID: "thread-council-stab", MessageID: fmt.Sprintf("msg_council_stab_%02d", turn), PostingPath: "selected_member_profile_send", SenderMember: "agent-1"}})
	}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}

	for turn := 1; turn <= 15; turn++ {
		if turn > 1 {
			appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", fmt.Sprintf("cmd_council_stab_poll_%02d", turn), map[string]any{"turn": turn}, time.Duration(turn*10)*time.Second)
			appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", fmt.Sprintf("cmd_council_stab_raise_%02d", turn), map[string]any{"turn": turn, "intent": "continue", "reason": fmt.Sprintf("selected-runner turn %02d", turn)}, time.Duration(turn*10+1)*time.Second)
		}
		response := server.Handle(councilGrantRequestForTurn(metadata.ID, fmt.Sprintf("cmd_council_stab_grant_%02d", turn), "agent-1", turn))
		if !response.OK {
			t.Fatalf("council.grant turn %d failed: %+v", turn, response)
		}
	}
	if _, _, err := storage.RecordCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{Action: "unresolved", Actor: "agent-mod", CommandID: "cmd_council_stab_unresolved", Payload: map[string]any{"reason": "15-turn selected-runner golden path reached terminal closeout"}, Now: daemonFixedRuntime().Now().Add(200 * time.Second)}); err != nil {
		t.Fatalf("golden path terminal closeout failed: %v", err)
	}
	reloaded, err := storage.LoadSessionYAML(sessionDir)
	if err != nil {
		t.Fatalf("LoadSessionYAML: %v", err)
	}
	if reloaded.Status != storage.StatusTerminal || reloaded.State.Phase != "unresolved" {
		t.Fatalf("golden path terminal status mismatch: %s/%s", reloaded.Status, reloaded.State.Phase)
	}

	if adapter.calls != 15 || adapter.visibleCalls != 15 {
		t.Fatalf("golden path must call selected runner and visible sender once per turn, runner=%d visible=%d", adapter.calls, adapter.visibleCalls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	accounting := storage.SelectedRunnerAccountingFromIndex(metadata, index)
	if !accounting.SelectedRunnerPass || accounting.SelectedSpeakerCount != 15 || accounting.RunnerStartedCount != 15 || accounting.RunnerSucceededCount != 15 || accounting.LinkedRunnerSpeechCount != 15 || accounting.RunnerlessSpeechCount != 0 || accounting.DispatchFailureCount != 0 {
		t.Fatalf("15-turn selected-runner accounting mismatch: %#v", accounting)
	}
	unresolved := findEvent(t, index.Events, "council_unresolved")
	if unresolved.EventID == "" || unresolved.Payload["reason"] == "" {
		t.Fatalf("golden path terminal closeout missing: %#v", unresolved)
	}
	for _, grant := range accounting.SelectedRunners {
		if !grant.Pass || len(grant.LinkedRunnerSpeechEventIDs) != 1 || len(grant.LinkedRunnerDeliveryEventIDs) != 1 {
			t.Fatalf("each selected turn must have one linked speech and one visible delivery: %#v", grant)
		}
	}
}

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
	succeeded := findEvent(t, index.Events, "runner_invocation_succeeded")
	speech := findEvent(t, index.Events, "speech")
	if started.CausationEventID != selected.EventID || succeeded.CausationEventID != selected.EventID || speech.CausationEventID != selected.EventID {
		t.Fatalf("runner events must point to speaker_selected causation: selected=%s started=%s succeeded=%s speech=%s", selected.EventID, started.CausationEventID, succeeded.CausationEventID, speech.CausationEventID)
	}
	if speech.Runner.InvocationID == "" || succeeded.Runner.InvocationID != started.Runner.InvocationID || speech.Runner.InvocationID != started.Runner.InvocationID || speech.Runner.Member != "agent-1" {
		t.Fatalf("success and speech must preserve invocation/member evidence: started=%#v succeeded=%#v speech=%#v", started.Runner, succeeded.Runner, speech.Runner)
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
	if eventTypeCount(index.Events, "runner_invocation_started") != 1 || eventTypeCount(index.Events, "runner_invocation_succeeded") != 1 || eventTypeCount(index.Events, "speech") != 1 {
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
			"selection_mode": "relevance",
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
		From:             "atn-controld",
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
	if diagnostic.Payload["reason"] != "selected_runner_dispatch_incomplete_stale" || diagnostic.Payload["error_class"] != runner.ErrorClassStalePhaseEvidence || diagnostic.CausationEventID != selected.EventID {
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
	for key, want := range map[string]any{
		"append_status":      "accepted",
		"dispatch_status":    "runner_terminal_failure",
		"runner_status":      "timeout",
		"speech_link_status": "missing_linked_runner_speech",
		"followup_required":  true,
	} {
		if got := response.Result[key]; got != want {
			t.Fatalf("timeout response %s=%#v want %#v result=%#v", key, got, want, response.Result)
		}
	}
	if response.Result["grant_event_id"] == "" || response.Result["selected_event_id"] == "" || response.Result["runner_failure_event_id"] == "" {
		t.Fatalf("timeout response missing event pointers: %#v", response.Result)
	}
	accounting := storage.SelectedRunnerAccountingFromIndex(metadata, index)
	if len(accounting.SelectedRunners) != 1 {
		t.Fatalf("selected-runner accounting missing grant row: %#v", accounting)
	}
	grant := accounting.SelectedRunners[0]
	if grant.RunnerStatus != "timeout" || grant.SpeechLinkStatus != "missing_linked_runner_speech" || !grant.FollowupRequired {
		t.Fatalf("timeout accounting must expose runner/speech/followup axes: %#v", grant)
	}
}

func TestCouncilGrantUnsupportedAdapterFailsClosedBeforeSelectedRunnerLaunch(t *testing.T) {
	dataHome, metadata, sessionDir := createSurfacedDiscussionCouncilForDispatch(t)
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = &unsupportedSelectedSpeakerAdapter{}

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_unsupported_adapter", "agent-1"))
	if response.OK {
		t.Fatalf("council.grant should fail closed for unsupported selected-runner adapter: %+v", response)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	diagnostic := findEvent(t, index.Events, "selected_runner_dispatch_failed")
	if diagnostic.Payload["reason"] != "selected_runner_preflight_failed" {
		t.Fatalf("unsupported adapter should record selected-runner preflight diagnostic: %#v", diagnostic.Payload)
	}
	if !strings.Contains(strings.Join(anyStringSlice(diagnostic.Payload["blocking_reasons"]), ","), "runner_adapter_unavailable") {
		t.Fatalf("diagnostic should expose unsupported adapter blocker: %#v", diagnostic.Payload)
	}
	if eventTypeCount(index.Events, "speaker_selected") != 0 || eventTypeCount(index.Events, "runner_invocation_started") != 0 || eventTypeCount(index.Events, "speech") != 0 {
		t.Fatalf("unsupported adapter preflight must not append grant, runner, or speech events: %#v", index.Events)
	}
}
func TestCouncilGrantBlocksGuardedLiveVisibleTimeoutPolicyDrift(t *testing.T) {
	dataHome, metadata, sessionDir := createGuardedDiscussionCouncilForDispatch(t, 120, nil)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}
	server.SelectedSpeakerTimeout = 45 * time.Second

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_timeout_policy_block", "agent-1"))
	if !response.OK {
		t.Fatalf("council.grant should record timeout-policy diagnostic without launching runner: %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("timeout-policy drift must block selected-runner launch, calls=%d", adapter.calls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	diagnostic := findEvent(t, index.Events, "selected_runner_dispatch_failed")
	if diagnostic.Payload["reason"] != "selected_runner_timeout_policy_blocked" {
		t.Fatalf("timeout-policy drift should record selected_runner_timeout_policy_blocked, payload=%#v", diagnostic.Payload)
	}
	if diagnostic.Payload["diagnostic_owner"] != "control/NEWFIX-005" {
		t.Fatalf("timeout-policy drift should cite NEWFIX-005 ownership, payload=%#v", diagnostic.Payload)
	}
}
func TestCouncilGrantBlocksGuardedVisibleChannelFallbackTimeoutPolicyDrift(t *testing.T) {
	dataHome, metadata, sessionDir := createGuardedVisibleCouncilForDispatch(t, map[string]any{
		"kind":       "discord_channel",
		"platform":   "discord",
		"channel_id": "chan-guarded-fallback",
	}, 120, nil)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}
	server.SelectedSpeakerTimeout = 45 * time.Second

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_timeout_policy_channel_fallback", "agent-1"))
	if !response.OK {
		t.Fatalf("channel fallback council.grant should record timeout-policy diagnostic without launching runner: %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("timeout-policy drift on channel fallback must block selected-runner launch, calls=%d", adapter.calls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	diagnostic := findEvent(t, index.Events, "selected_runner_dispatch_failed")
	if diagnostic.Payload["reason"] != "selected_runner_timeout_policy_blocked" {
		t.Fatalf("channel fallback timeout-policy drift should record selected_runner_timeout_policy_blocked, payload=%#v", diagnostic.Payload)
	}
}

func TestCouncilGrantAllowsGuardedLiveVisibleTimeoutWhenDaemonOverrideMatches(t *testing.T) {
	dataHome, metadata, sessionDir := createGuardedDiscussionCouncilForDispatch(t, 120, nil)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "canonical selected runner speech"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}
	server.SelectedSpeakerTimeout = 120 * time.Second

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_timeout_policy_match", "agent-1"))
	if !response.OK {
		t.Fatalf("matching daemon override should preserve selected-runner launch: %+v", response)
	}
	if adapter.calls != 1 {
		t.Fatalf("matching timeout policy should launch selected runner once, calls=%d", adapter.calls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if eventTypeCount(index.Events, "selected_runner_dispatch_failed") != 0 {
		t.Fatalf("matching timeout policy must not append selected_runner_dispatch_failed: %#v", index.Events)
	}
	if eventTypeCount(index.Events, "speech") != 1 {
		t.Fatalf("matching timeout policy should still append canonical speech: %#v", index.Events)
	}
}

type unsupportedSelectedSpeakerAdapter struct {
	fakeRunRTAdapter
}

func (*unsupportedSelectedSpeakerAdapter) Kind() string {
	return "codex-cli"
}

func createDiscussionCouncilForDispatch(t *testing.T) (string, *storage.SessionMetadata, string) {
	t.Helper()
	return createCouncilForDispatch(t, nil)
}

func createSurfacedDiscussionCouncilForDispatch(t *testing.T) (string, *storage.SessionMetadata, string) {
	t.Helper()
	return createCouncilForDispatch(t, &storage.Surface{Kind: "discord_thread", ThreadID: "thread_hun007"})
}

func createGuardedDiscussionCouncilForDispatch(t *testing.T, dispatchTimeoutSec int, override map[string]any) (string, *storage.SessionMetadata, string) {
	t.Helper()
	return createGuardedVisibleCouncilForDispatch(t, map[string]any{
		"kind":       "discord_thread",
		"platform":   "discord",
		"channel_id": "chan-guarded",
		"thread_id":  "thread-guarded",
	}, dispatchTimeoutSec, override)
}

func createGuardedVisibleCouncilForDispatch(t *testing.T, surface map[string]any, dispatchTimeoutSec int, override map[string]any) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, _, _ := dispatchDataHome(t)
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	requestContext := map[string]any{
		"source":                "discord_thread",
		"requested_output_mode": "live_visible_thread",
	}
	if override != nil {
		requestContext["selected_runner_timeout_override"] = override
	}
	response := server.Handle(protocol.NewRequest("cmd_guarded_new_"+name, "council.new", map[string]any{
		"session_id":      "sess_guarded_council_dispatch_" + name,
		"moderator":       "agent-mod",
		"members":         []any{"agent-1"},
		"title":           "NEWFIX-005 guarded dispatch",
		"request_context": requestContext,
		"surface":         surface,
		"limits":          map[string]any{"dispatch_timeout_sec": dispatchTimeoutSec},
	}))
	if !response.OK {
		t.Fatalf("guarded council.new should pass: %+v", response)
	}
	sessionID := response.Result["session_id"].(string)
	sessionDir, err := storage.SessionDir(dataHome, sessionID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	metadata, err := storage.LoadSessionYAML(sessionDir)
	if err != nil {
		t.Fatalf("LoadSessionYAML: %v", err)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_attendance_"+name, map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_attend_"+name, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_"+name, map[string]any{"decision_question": "What should ship?", "success_criteria": "Produce a canonical typed speech with agenda and prior context.", "out_of_scope_policy": "Do not invent agenda text or repair missing control context from plugin hints."}, 3*time.Second)
	if strings.TrimSpace(fmt.Sprint(surface["kind"])) == "discord_thread" {
		appendRUNFIX009RuntimeEvidence(t, sessionDir, metadata, "agent-1", 3500*time.Millisecond)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_"+name, map[string]any{"timeout_sec": 30}, 4*time.Second)
	if strings.TrimSpace(fmt.Sprint(surface["kind"])) == "discord_thread" {
		appendCouncilEventForDispatch(t, sessionDir, metadata, "ready", "agent-1", "cmd_ready_"+name, map[string]any{"summary": "ready for guarded NEWFIX-005 dispatch"}, 4500*time.Millisecond)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_"+name, map[string]any{"turn": 1}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_"+name, map[string]any{"turn": 1, "intent": "answer", "reason": "selected"}, 6*time.Second)
	return dataHome, metadata, sessionDir
}

func createCouncilForDispatch(t *testing.T, surface *storage.Surface) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHome(t)
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_council_dispatch_" + name, Title: "RUNFIX-003", Moderator: "agent-mod", Surface: surface, EventID: "evt_created_" + name, CommandID: "cmd_created_" + name},
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
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_"+name, map[string]any{"decision_question": "What should ship?", "success_criteria": "Produce a canonical typed speech with agenda and prior context.", "out_of_scope_policy": "Do not invent agenda text or repair missing control context from plugin hints."}, 3*time.Second)
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
	return councilGrantRequestForTurn(sessionID, commandID, member, 1)
}

func councilGrantRequestForTurn(sessionID, commandID, member string, turn int) protocol.CommandRequest {
	return protocol.NewRequest(commandID, "council.grant", map[string]any{
		"session_id": sessionID,
		"actor":      "agent-mod",
		"command_id": commandID,
		"payload": map[string]any{
			"turn":           turn,
			"member":         member,
			"selection_mode": "relevance",
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
