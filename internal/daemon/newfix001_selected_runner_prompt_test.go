package daemon_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atn-control/internal/daemon"
	"atn-control/internal/protocol"
	"atn-control/internal/runner"
	"atn-control/internal/storage"
)

func TestNEWFIX001CouncilGrantBuildsProjectionBackedPromptEvidenceWithPriorClaims(t *testing.T) {
	dataHome, metadata, sessionDir := createDiscussionCouncilForDispatch(t)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "grant", "agent-mod", "cmd_prior_grant_turn_1", map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"}, 7*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "speak", "agent-1", "cmd_prior_speak_turn_1", map[string]any{
		"turn":              1,
		"speech":            "Prior claimed speech for context.",
		"claims":            []any{map[string]any{"claim_id": "T1.C1", "summary": "Keep the agenda body explicit.", "kind": "requirement"}},
		"contribution_type": "support",
	}, 8*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_turn_2", map[string]any{"turn": 2}, 9*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_turn_2", map[string]any{"turn": 2, "intent": "answer", "reason": "follow up on prior claim"}, 10*time.Second)

	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 2, "speech": "Projection-backed selected runner response."}, Cost: &runner.Cost{TokensIn: 3, TokensOut: 5, Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}

	response := server.Handle(protocol.NewRequest("cmd_council_grant_projection_prompt", "council.grant", map[string]any{
		"session_id": metadata.ID,
		"actor":      "agent-mod",
		"command_id": "cmd_council_grant_projection_prompt",
		"payload": map[string]any{
			"turn":           2,
			"member":         "agent-1",
			"selection_mode": "moderator_direct",
		},
	}))
	if !response.OK {
		t.Fatalf("projection-backed grant should succeed: %+v", response)
	}
	if adapter.calls != 1 || len(adapter.reqs) != 1 {
		t.Fatalf("projection-backed grant should invoke runner once, calls=%d reqs=%#v", adapter.calls, adapter.reqs)
	}
	prompt := adapter.reqs[0].Prompt
	for _, want := range []string{
		"decision_question: What should ship?",
		"success_criteria: Produce a canonical typed speech with agenda and prior context.",
		"out_of_scope_policy: Do not invent agenda text or repair missing control context from plugin hints.",
		"role_assignment: assignee",
		"stance_assignment: answer",
		"required_response_schema:",
		"missing_context_instruction:",
		"prior_claim_context:",
		"selected_member_prior_speeches:",
		"selected_member_latest_claims:",
		"selected_member_claim_index:",
		"T1.C1:Keep the agenda body explicit.",
		"argue_stance_rule:",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("selected-runner prompt missing %q:\n%s", want, prompt)
		}
	}

	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	evidenceEvent := latestEventOfType(t, index.Events, "selected_runner_prompt_evidence")
	if evidenceEvent.Payload["result"] != "pass" {
		t.Fatalf("prompt evidence should pass: %#v", evidenceEvent.Payload)
	}
	included := anyStringSliceTest(evidenceEvent.Payload["included_context"])
	for _, want := range []string{"decision_question", "success_criteria", "out_of_scope_policy"} {
		if !containsString(included, want) {
			t.Fatalf("NEWFIX-009 prompt evidence should include agenda key %q: %#v", want, evidenceEvent.Payload)
		}
	}
	for _, want := range []string{"prior_claim_context", "selected_member_prior_speeches", "selected_member_latest_claims", "selected_member_claim_index"} {
		if !containsString(included, want) {
			t.Fatalf("prompt evidence should include %q: %#v", want, evidenceEvent.Payload)
		}
	}
	agendaSourceIDs := anyStringSliceTest(evidenceEvent.Payload["agenda_source_event_ids"])
	if len(agendaSourceIDs) != 1 {
		t.Fatalf("NEWFIX-009 prompt evidence should record one agenda source event: %#v", evidenceEvent.Payload)
	}
	agendaEvent, ok := eventByID(index.Events, agendaSourceIDs[0])
	if !ok || agendaEvent.Type != "agenda_locked" {
		t.Fatalf("NEWFIX-009 agenda source id should resolve to agenda_locked event: id=%q events=%#v", agendaSourceIDs[0], index.Events)
	}
	for key, want := range fullAgendaPayload() {
		if got := stringValueTest(agendaEvent.Payload[key]); got != want {
			t.Fatalf("NEWFIX-009 agenda_locked payload %s mismatch: got %q want %q payload=%#v", key, got, want, agendaEvent.Payload)
		}
	}
	for _, field := range []string{"prior_context_source_event_ids", "own_history_source_event_ids", "own_latest_claim_source_event_ids", "own_claim_index_source_event_ids"} {
		if !containsString(anyStringSliceTest(evidenceEvent.Payload[field]), "evt_speech_cmd_prior_speak_turn_1") {
			t.Fatalf("prompt evidence should record %s: %#v", field, evidenceEvent.Payload)
		}
	}
	if strings.TrimSpace(stringValueTest(evidenceEvent.Payload["prompt_context_sha256"])) == "" {
		t.Fatalf("prompt evidence should record prompt_context_sha256: %#v", evidenceEvent.Payload)
	}

	status, err := storage.CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	statusEvidence, ok := status["selected_runner_prompt_evidence"].(storage.SelectedRunnerPromptEvidence)
	if !ok || statusEvidence.Result != "pass" || statusEvidence.SpeakerSelectedEventID == "" {
		t.Fatalf("status selected_runner_prompt_evidence mismatch: %#v", status["selected_runner_prompt_evidence"])
	}
	if len(statusEvidence.OwnHistorySourceEventIDs) == 0 || len(statusEvidence.OwnLatestClaimSourceEventIDs) == 0 || len(statusEvidence.OwnClaimIndexSourceEventIDs) == 0 {
		t.Fatalf("status prompt evidence should expose own-history provenance: %#v", statusEvidence)
	}

	transcript, err := storage.RenderTranscript(sessionDir, metadata, storage.TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript: %v", err)
	}
	for _, want := range []string{"Selected Runner Prompt Evidence", "prompt_context_sha256", "prior_context_source_event_ids", "own_history_source_event_ids", "own_claim_index_source_event_ids"} {
		if !strings.Contains(string(transcript), want) {
			t.Fatalf("transcript missing %q:\n%s", want, string(transcript))
		}
	}

	bundle, err := storage.BuildExportBundle(sessionDir, metadata, storage.ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(bundle.BundleDir, "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal bundle manifest: %v\n%s", err, string(manifestBytes))
	}
	manifestEvidence, ok := manifest["selected_runner_prompt_evidence"].(map[string]any)
	if !ok || manifestEvidence["result"] != "pass" || manifestEvidence["speaker_selected_event_id"] == "" {
		t.Fatalf("manifest selected_runner_prompt_evidence mismatch: %#v", manifest["selected_runner_prompt_evidence"])
	}
	if !containsString(anyStringSliceTest(manifestEvidence["own_history_source_event_ids"]), "evt_speech_cmd_prior_speak_turn_1") {
		t.Fatalf("manifest prompt evidence should expose own-history provenance: %#v", manifestEvidence)
	}
}

func TestNEWFIX001CouncilGrantBlocksWhenAgendaContextMissing(t *testing.T) {
	dataHome, metadata, sessionDir := createCouncilForPromptContextWithoutAgenda(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_missing_agenda_context", "agent-1"))
	if !response.OK {
		t.Fatalf("grant should fail closed after durable blocked evidence, got %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("agenda-context block must prevent runner launch, calls=%d", adapter.calls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if eventTypeCount(index.Events, "runner_invocation_started") != 0 {
		t.Fatalf("agenda-context block must not append runner_invocation_started: %#v", index.Events)
	}
	evidenceEvent := latestEventOfType(t, index.Events, "selected_runner_prompt_evidence")
	if evidenceEvent.Payload["result"] != "blocked" {
		t.Fatalf("prompt evidence should be blocked: %#v", evidenceEvent.Payload)
	}
	missing := anyStringSliceTest(evidenceEvent.Payload["missing_required_context"])
	for _, want := range []string{"decision_question", "success_criteria", "out_of_scope_policy"} {
		if !containsString(missing, want) {
			t.Fatalf("blocked prompt evidence missing %q in %#v", want, evidenceEvent.Payload)
		}
	}
	diagnostic := latestEventOfType(t, index.Events, "selected_runner_dispatch_failed")
	if diagnostic.Payload["reason"] != "selected_runner_context_missing" {
		t.Fatalf("agenda-context block should leave durable selected-runner diagnostic: %#v", diagnostic.Payload)
	}
}

func TestNEWFIX009CouncilLockAgendaRejectsMissingRequiredAgendaFields(t *testing.T) {
	dataHome, loaded, _ := dispatchDataHome(t)
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_newfix009_agenda_validation_" + name, Title: "NEWFIX-009", Moderator: "agent-mod", EventID: "evt_created_newfix009_validation_" + name, CommandID: "cmd_created_newfix009_validation_" + name},
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

	cases := []struct {
		name    string
		field   string
		payload map[string]any
	}{
		{
			name:  "missing_decision_question",
			field: "decision_question",
			payload: map[string]any{
				"success_criteria":    "Produce a canonical typed speech with agenda and prior context.",
				"out_of_scope_policy": "Do not invent agenda text or repair missing control context from plugin hints.",
			},
		},
		{
			name:  "missing_success_criteria",
			field: "success_criteria",
			payload: map[string]any{
				"decision_question":   "What should ship?",
				"out_of_scope_policy": "Do not invent agenda text or repair missing control context from plugin hints.",
			},
		},
		{
			name:  "missing_out_of_scope_policy",
			field: "out_of_scope_policy",
			payload: map[string]any{
				"decision_question": "What should ship?",
				"success_criteria":  "Produce a canonical typed speech with agenda and prior context.",
			},
		},
	}
	for i, tc := range cases {
		_, _, err := storage.RecordCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{
			Action:    "lock-agenda",
			Actor:     "agent-mod",
			CommandID: "cmd_newfix009_lock_agenda_" + tc.name,
			Payload:   tc.payload,
			Now:       daemonFixedRuntime().Now().Add(time.Duration(i+1) * time.Second),
		})
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Fatalf("missing %s should fail closed at agenda storage boundary, err=%v", tc.field, err)
		}
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if eventTypeCount(index.Events, "agenda_locked") != 0 {
		t.Fatalf("invalid agenda submissions must not append agenda_locked: %#v", index.Events)
	}
}

func TestNEWFIX001CouncilGrantBlocksWhenPriorContextCannotBeReconstructed(t *testing.T) {
	dataHome, metadata, sessionDir := createCouncilForPromptContext(t, fullAgendaPayload())
	appendRawPromptContextEvent(t, sessionDir, metadata, storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_prior_bad_speech",
		CommandID:     "cmd_prior_bad_speech",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "speech",
		From:          "agent-1",
		To:            []string{"agent-mod"},
		CreatedAt:     daemonFixedRuntime().Now().Add(7 * time.Second),
		Payload:       map[string]any{"turn": 1},
	})
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_turn_2_prior_missing", map[string]any{"turn": 2}, 8*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_turn_2_prior_missing", map[string]any{"turn": 2, "intent": "answer", "reason": "prior context follow-up"}, 9*time.Second)

	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 2, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	response := server.Handle(protocol.NewRequest("cmd_council_grant_prior_context_missing", "council.grant", map[string]any{
		"session_id": metadata.ID,
		"actor":      "agent-mod",
		"command_id": "cmd_council_grant_prior_context_missing",
		"payload": map[string]any{
			"turn":           2,
			"member":         "agent-1",
			"selection_mode": "moderator_direct",
		},
	}))
	if !response.OK {
		t.Fatalf("grant should return after durable prior-context block, got %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("prior-context block must prevent runner launch, calls=%d", adapter.calls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	evidenceEvent := latestEventOfType(t, index.Events, "selected_runner_prompt_evidence")
	if evidenceEvent.Payload["result"] != "blocked" {
		t.Fatalf("prior-context prompt evidence should be blocked: %#v", evidenceEvent.Payload)
	}
	missing := anyStringSliceTest(evidenceEvent.Payload["missing_required_context"])
	for _, want := range []string{"prior_speech_context", "selected_member_prior_speeches"} {
		if !containsString(missing, want) {
			t.Fatalf("prior-context block should record %q: %#v", want, evidenceEvent.Payload)
		}
	}
	if eventTypeCount(index.Events, "runner_invocation_started") != 0 {
		t.Fatalf("prior-context block must not append runner_invocation_started: %#v", index.Events)
	}
}

func TestNEWFIX001CouncilGrantBlocksWhenControlOwnedStanceAssignmentMissing(t *testing.T) {
	dataHome, metadata, sessionDir := createCouncilForPromptContextWithoutRaise(t, fullAgendaPayload())
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 1, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	response := server.Handle(councilGrantRequest(metadata.ID, "cmd_council_grant_missing_stance_assignment", "agent-1"))
	if !response.OK {
		t.Fatalf("grant should return after durable stance block, got %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("stance-assignment block must prevent runner launch, calls=%d", adapter.calls)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	evidenceEvent := latestEventOfType(t, index.Events, "selected_runner_prompt_evidence")
	if evidenceEvent.Payload["result"] != "blocked" {
		t.Fatalf("stance-assignment prompt evidence should be blocked: %#v", evidenceEvent.Payload)
	}
	missing := anyStringSliceTest(evidenceEvent.Payload["missing_required_context"])
	if !containsString(missing, "stance_assignment") {
		t.Fatalf("stance-assignment block should record stance_assignment: %#v", evidenceEvent.Payload)
	}
	if eventTypeCount(index.Events, "runner_invocation_started") != 0 {
		t.Fatalf("stance-assignment block must not append runner_invocation_started: %#v", index.Events)
	}
}

func TestNEWFIX004CouncilGrantPreservesOwnHistoryBeyondGlobalRecentWindow(t *testing.T) {
	dataHome, metadata, sessionDir := createCouncilForOwnHistoryRoundRobin(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 9, "speech": "Own-history aware selected runner response."}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}

	response := server.Handle(protocol.NewRequest("cmd_council_grant_own_history_round_robin", "council.grant", map[string]any{
		"session_id": metadata.ID,
		"actor":      "agent-mod",
		"command_id": "cmd_council_grant_own_history_round_robin",
		"payload": map[string]any{
			"turn":           9,
			"member":         "agent-1",
			"selection_mode": "moderator_direct",
		},
	}))
	if !response.OK {
		t.Fatalf("round-robin own-history grant should succeed: %+v", response)
	}
	if adapter.calls != 1 || len(adapter.reqs) != 1 {
		t.Fatalf("round-robin own-history grant should invoke runner once, calls=%d reqs=%#v", adapter.calls, adapter.reqs)
	}
	prompt := adapter.reqs[0].Prompt
	for _, want := range []string{
		"selected_member_prior_speeches:",
		"event_id=evt_speech_cmd_round_robin_speech_1 member=agent-1 turn=1 speech=Agent 1 opening position.",
		"event_id=evt_speech_cmd_round_robin_speech_5 member=agent-1 turn=5 speech=Agent 1 follow-up refinement.",
		"selected_member_latest_claims: event=evt_speech_cmd_round_robin_speech_5 claims=T5.C1:Preserve continuity beyond the recent window.",
		"selected_member_claim_index:",
		"event=evt_speech_cmd_round_robin_speech_1 claim=T1.C1 summary=State the baseline rationale.",
		"event=evt_speech_cmd_round_robin_speech_5 claim=T5.C1 summary=Preserve continuity beyond the recent window.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("round-robin own-history prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, "event_id=evt_speech_cmd_round_robin_speech_1 member=agent-1 turn=1 speech=Agent 1 opening position.") > strings.Index(prompt, "event_id=evt_speech_cmd_round_robin_speech_5 member=agent-1 turn=5 speech=Agent 1 follow-up refinement.") {
		t.Fatalf("own-history prompt should preserve oldest-to-newest prior speeches:\n%s", prompt)
	}

	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	evidenceEvent := latestEventOfType(t, index.Events, "selected_runner_prompt_evidence")
	if evidenceEvent.Payload["result"] != "pass" {
		t.Fatalf("round-robin prompt evidence should pass: %#v", evidenceEvent.Payload)
	}
	priorIDs := anyStringSliceTest(evidenceEvent.Payload["prior_context_source_event_ids"])
	wantPriorIDs := []string{
		"evt_speech_cmd_round_robin_speech_3",
		"evt_speech_cmd_round_robin_speech_4",
		"evt_speech_cmd_round_robin_speech_5",
		"evt_speech_cmd_round_robin_speech_6",
		"evt_speech_cmd_round_robin_speech_7",
		"evt_speech_cmd_round_robin_speech_8",
	}
	if len(priorIDs) != len(wantPriorIDs) {
		t.Fatalf("global recent lane should stay bounded at 6 events: %#v", evidenceEvent.Payload)
	}
	for i, want := range wantPriorIDs {
		if priorIDs[i] != want {
			t.Fatalf("global recent lane should preserve exact source ids/order, got=%#v want=%#v", priorIDs, wantPriorIDs)
		}
	}
	ownHistoryIDs := anyStringSliceTest(evidenceEvent.Payload["own_history_source_event_ids"])
	for _, want := range []string{"evt_speech_cmd_round_robin_speech_1", "evt_speech_cmd_round_robin_speech_5"} {
		if !containsString(ownHistoryIDs, want) {
			t.Fatalf("own-history provenance should include %q: %#v", want, evidenceEvent.Payload)
		}
	}
	if !containsString(anyStringSliceTest(evidenceEvent.Payload["own_latest_claim_source_event_ids"]), "evt_speech_cmd_round_robin_speech_5") {
		t.Fatalf("latest-claim provenance should point at the latest selected-member claim-bearing speech: %#v", evidenceEvent.Payload)
	}
	for _, want := range []string{"evt_speech_cmd_round_robin_speech_1", "evt_speech_cmd_round_robin_speech_5"} {
		if !containsString(anyStringSliceTest(evidenceEvent.Payload["own_claim_index_source_event_ids"]), want) {
			t.Fatalf("claim-index provenance should include %q: %#v", want, evidenceEvent.Payload)
		}
	}

	status, err := storage.CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	statusEvidence, ok := status["selected_runner_prompt_evidence"].(storage.SelectedRunnerPromptEvidence)
	if !ok {
		t.Fatalf("status selected_runner_prompt_evidence missing: %#v", status["selected_runner_prompt_evidence"])
	}
	if len(statusEvidence.PriorContextSourceEventIDs) != 6 {
		t.Fatalf("status evidence should preserve the bounded global lane: %#v", statusEvidence)
	}
	if len(statusEvidence.OwnHistorySourceEventIDs) < 2 || len(statusEvidence.OwnClaimIndexSourceEventIDs) < 2 {
		t.Fatalf("status evidence should expose distinct own-history provenance: %#v", statusEvidence)
	}

	transcript, err := storage.RenderTranscript(sessionDir, metadata, storage.TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript: %v", err)
	}
	for _, want := range []string{"own_history_source_event_ids", "own_latest_claim_source_event_ids", "own_claim_index_source_event_ids"} {
		if !strings.Contains(string(transcript), want) {
			t.Fatalf("transcript missing %q:\n%s", want, string(transcript))
		}
	}

	bundle, err := storage.BuildExportBundle(sessionDir, metadata, storage.ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(bundle.BundleDir, "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal bundle manifest: %v\n%s", err, string(manifestBytes))
	}
	manifestEvidence, ok := manifest["selected_runner_prompt_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("manifest selected_runner_prompt_evidence missing: %#v", manifest["selected_runner_prompt_evidence"])
	}
	if !containsString(anyStringSliceTest(manifestEvidence["own_history_source_event_ids"]), "evt_speech_cmd_round_robin_speech_1") {
		t.Fatalf("manifest should expose earliest own-history provenance outside the bounded global lane: %#v", manifestEvidence)
	}
}

func TestNEWFIX004CouncilGrantUsesLatestValidOwnHistoryClaimsWhenOlderClaimsMalformed(t *testing.T) {
	dataHome, metadata, sessionDir := createCouncilForMixedValidityOwnHistoryRoundRobin(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 9, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	response := server.Handle(protocol.NewRequest("cmd_council_grant_mixed_validity_own_history", "council.grant", map[string]any{
		"session_id": metadata.ID,
		"actor":      "agent-mod",
		"command_id": "cmd_council_grant_mixed_validity_own_history",
		"payload": map[string]any{
			"turn":           9,
			"member":         "agent-1",
			"selection_mode": "moderator_direct",
		},
	}))
	if !response.OK {
		t.Fatalf("mixed-validity own-history grant should return after durable block, got %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("mixed-validity own-history claim-index block must prevent runner launch, calls=%d", adapter.calls)
	}

	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	evidenceEvent := latestEventOfType(t, index.Events, "selected_runner_prompt_evidence")
	if evidenceEvent.Payload["result"] != "blocked" {
		t.Fatalf("mixed-validity own-history prompt evidence should be blocked: %#v", evidenceEvent.Payload)
	}
	included := anyStringSliceTest(evidenceEvent.Payload["included_context"])
	if !containsString(included, "selected_member_latest_claims") {
		t.Fatalf("mixed-validity own-history should still include latest valid claims: %#v", evidenceEvent.Payload)
	}
	missing := anyStringSliceTest(evidenceEvent.Payload["missing_required_context"])
	if containsString(missing, "selected_member_latest_claims") {
		t.Fatalf("older malformed claims must not poison latest valid own-history claims: %#v", evidenceEvent.Payload)
	}
	if !containsString(missing, "selected_member_claim_index") {
		t.Fatalf("mixed-validity own-history should still block on claim-index completeness: %#v", evidenceEvent.Payload)
	}
	if !containsString(anyStringSliceTest(evidenceEvent.Payload["own_latest_claim_source_event_ids"]), "evt_speech_cmd_mixed_validity_round_robin_speech_5") {
		t.Fatalf("latest valid own-history claim provenance should point at the newer valid claim-bearing speech: %#v", evidenceEvent.Payload)
	}
	if eventTypeCount(index.Events, "runner_invocation_started") != 0 {
		t.Fatalf("mixed-validity own-history block must not append runner_invocation_started: %#v", index.Events)
	}
}

func TestNEWFIX004CouncilGrantBlocksStaleLatestClaimsWhenNewerOwnClaimsMalformed(t *testing.T) {
	dataHome, metadata, sessionDir := createCouncilForTrailingMalformedOwnHistoryRoundRobin(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 9, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	response := server.Handle(protocol.NewRequest("cmd_council_grant_trailing_malformed_own_history", "council.grant", map[string]any{
		"session_id": metadata.ID,
		"actor":      "agent-mod",
		"command_id": "cmd_council_grant_trailing_malformed_own_history",
		"payload": map[string]any{
			"turn":           9,
			"member":         "agent-1",
			"selection_mode": "moderator_direct",
		},
	}))
	if !response.OK {
		t.Fatalf("trailing-malformed own-history grant should return after durable block, got %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("trailing-malformed own-history block must prevent runner launch, calls=%d", adapter.calls)
	}

	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	evidenceEvent := latestEventOfType(t, index.Events, "selected_runner_prompt_evidence")
	if evidenceEvent.Payload["result"] != "blocked" {
		t.Fatalf("trailing-malformed own-history prompt evidence should be blocked: %#v", evidenceEvent.Payload)
	}
	included := anyStringSliceTest(evidenceEvent.Payload["included_context"])
	if containsString(included, "selected_member_latest_claims") {
		t.Fatalf("newer malformed claim-bearing speech must clear stale latest claims: %#v", evidenceEvent.Payload)
	}
	missing := anyStringSliceTest(evidenceEvent.Payload["missing_required_context"])
	for _, want := range []string{"selected_member_latest_claims", "selected_member_claim_index"} {
		if !containsString(missing, want) {
			t.Fatalf("trailing-malformed own-history should record %q: %#v", want, evidenceEvent.Payload)
		}
	}
	if len(anyStringSliceTest(evidenceEvent.Payload["own_latest_claim_source_event_ids"])) != 0 {
		t.Fatalf("stale latest-claim provenance must be cleared when the newest claim-bearing speech is malformed: %#v", evidenceEvent.Payload)
	}
	if eventTypeCount(index.Events, "runner_invocation_started") != 0 {
		t.Fatalf("trailing-malformed own-history block must not append runner_invocation_started: %#v", index.Events)
	}
}

func TestNEWFIX004CouncilGrantBlocksOwnHistorySpeechWithInvalidTurnProvenance(t *testing.T) {
	dataHome, metadata, sessionDir := createCouncilForInvalidOwnHistoryTurnRoundRobin(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "speech", SemanticStatus: "succeeded", Payload: map[string]any{"turn": 9, "speech": "should not run"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter

	response := server.Handle(protocol.NewRequest("cmd_council_grant_invalid_turn_own_history", "council.grant", map[string]any{
		"session_id": metadata.ID,
		"actor":      "agent-mod",
		"command_id": "cmd_council_grant_invalid_turn_own_history",
		"payload": map[string]any{
			"turn":           9,
			"member":         "agent-1",
			"selection_mode": "moderator_direct",
		},
	}))
	if !response.OK {
		t.Fatalf("invalid-turn own-history grant should return after durable block, got %+v", response)
	}
	if adapter.calls != 0 {
		t.Fatalf("invalid-turn own-history block must prevent runner launch, calls=%d", adapter.calls)
	}

	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	evidenceEvent := latestEventOfType(t, index.Events, "selected_runner_prompt_evidence")
	if evidenceEvent.Payload["result"] != "blocked" {
		t.Fatalf("invalid-turn own-history prompt evidence should be blocked: %#v", evidenceEvent.Payload)
	}
	missing := anyStringSliceTest(evidenceEvent.Payload["missing_required_context"])
	if !containsString(missing, "selected_member_prior_speeches") {
		t.Fatalf("invalid turn provenance should record selected_member_prior_speeches: %#v", evidenceEvent.Payload)
	}
	if eventTypeCount(index.Events, "runner_invocation_started") != 0 {
		t.Fatalf("invalid-turn own-history block must not append runner_invocation_started: %#v", index.Events)
	}
}

func createCouncilForMixedValidityOwnHistoryRoundRobin(t *testing.T) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHomeWithMembers(t, "agent-1", "agent-2", "agent-3", "agent-4")
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_prompt_context_mixed_validity_round_robin_" + name, Title: "NEWFIX-004", Moderator: "agent-mod", EventID: "evt_created_mixed_validity_round_robin_" + name, CommandID: "cmd_created_mixed_validity_round_robin_" + name},
		Members: []string{"agent-1", "agent-2", "agent-3", "agent-4"},
		Now:     daemonFixedRuntime().Now(),
	}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_attendance_mixed_validity_round_robin_"+name, map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_attend_mixed_validity_round_robin_agent_1_"+name, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-2", "cmd_attend_mixed_validity_round_robin_agent_2_"+name, map[string]any{"status": "present", "summary": "ready"}, 3*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-3", "cmd_attend_mixed_validity_round_robin_agent_3_"+name, map[string]any{"status": "present", "summary": "ready"}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-4", "cmd_attend_mixed_validity_round_robin_agent_4_"+name, map[string]any{"status": "present", "summary": "ready"}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_mixed_validity_round_robin_"+name, fullAgendaPayload(), 6*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_mixed_validity_round_robin_"+name, map[string]any{"timeout_sec": 30}, 7*time.Second)
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_mixed_validity_round_robin_speech_1", "cmd_mixed_validity_round_robin_speech_1", "agent-1", 1, map[string]any{"speech": "Agent 1 opening position.", "claims": []any{map[string]any{"claim_id": "T1.C1"}}}, 8*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_mixed_validity_round_robin_speech_2", "cmd_mixed_validity_round_robin_speech_2", "agent-2", 2, map[string]any{"speech": "Agent 2 counterpoint."}, 9*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_mixed_validity_round_robin_speech_3", "cmd_mixed_validity_round_robin_speech_3", "agent-3", 3, map[string]any{"speech": "Agent 3 supporting note."}, 10*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_mixed_validity_round_robin_speech_4", "cmd_mixed_validity_round_robin_speech_4", "agent-4", 4, map[string]any{"speech": "Agent 4 implementation concern."}, 11*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_mixed_validity_round_robin_speech_5", "cmd_mixed_validity_round_robin_speech_5", "agent-1", 5, map[string]any{"speech": "Agent 1 follow-up refinement.", "claims": []any{map[string]any{"claim_id": "T5.C1", "summary": "Use the latest valid claim-bearing speech for continuity.", "kind": "requirement"}}}, 12*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_mixed_validity_round_robin_speech_6", "cmd_mixed_validity_round_robin_speech_6", "agent-2", 6, map[string]any{"speech": "Agent 2 latest objection."}, 13*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_mixed_validity_round_robin_speech_7", "cmd_mixed_validity_round_robin_speech_7", "agent-3", 7, map[string]any{"speech": "Agent 3 latest evidence."}, 14*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_mixed_validity_round_robin_speech_8", "cmd_mixed_validity_round_robin_speech_8", "agent-4", 8, map[string]any{"speech": "Agent 4 latest concern."}, 15*time.Second))
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_mixed_validity_round_robin_"+name, map[string]any{"turn": 9}, 16*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_mixed_validity_round_robin_"+name, map[string]any{"turn": 9, "intent": "answer", "reason": "mixed-validity round-robin follow-up"}, 17*time.Second)
	return dataHome, metadata, sessionDir
}

func createCouncilForTrailingMalformedOwnHistoryRoundRobin(t *testing.T) (string, *storage.SessionMetadata, string) {
	dataHome, loaded, _ := dispatchDataHomeWithMembers(t, "agent-1", "agent-2", "agent-3", "agent-4")
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_prompt_context_trailing_malformed_round_robin_" + name, Title: "NEWFIX-004", Moderator: "agent-mod", EventID: "evt_created_trailing_malformed_round_robin_" + name, CommandID: "cmd_created_trailing_malformed_round_robin_" + name},
		Members: []string{"agent-1", "agent-2", "agent-3", "agent-4"},
		Now:     daemonFixedRuntime().Now(),
	}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_attendance_trailing_malformed_round_robin_"+name, map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_attend_trailing_malformed_round_robin_agent_1_"+name, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-2", "cmd_attend_trailing_malformed_round_robin_agent_2_"+name, map[string]any{"status": "present", "summary": "ready"}, 3*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-3", "cmd_attend_trailing_malformed_round_robin_agent_3_"+name, map[string]any{"status": "present", "summary": "ready"}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-4", "cmd_attend_trailing_malformed_round_robin_agent_4_"+name, map[string]any{"status": "present", "summary": "ready"}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_trailing_malformed_round_robin_"+name, fullAgendaPayload(), 6*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_trailing_malformed_round_robin_"+name, map[string]any{"timeout_sec": 30}, 7*time.Second)
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_trailing_malformed_round_robin_speech_1", "cmd_trailing_malformed_round_robin_speech_1", "agent-1", 1, map[string]any{"speech": "Agent 1 opening position.", "claims": []any{map[string]any{"claim_id": "T1.C1", "summary": "Do not surface stale latest claims after malformed newer own history.", "kind": "requirement"}}}, 8*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_trailing_malformed_round_robin_speech_2", "cmd_trailing_malformed_round_robin_speech_2", "agent-2", 2, map[string]any{"speech": "Agent 2 counterpoint."}, 9*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_trailing_malformed_round_robin_speech_3", "cmd_trailing_malformed_round_robin_speech_3", "agent-3", 3, map[string]any{"speech": "Agent 3 supporting note."}, 10*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_trailing_malformed_round_robin_speech_4", "cmd_trailing_malformed_round_robin_speech_4", "agent-4", 4, map[string]any{"speech": "Agent 4 implementation concern."}, 11*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_trailing_malformed_round_robin_speech_5", "cmd_trailing_malformed_round_robin_speech_5", "agent-1", 5, map[string]any{"speech": "Agent 1 malformed newer claim-bearing follow-up.", "claims": []any{map[string]any{"claim_id": "T5.C1"}}}, 12*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_trailing_malformed_round_robin_speech_6", "cmd_trailing_malformed_round_robin_speech_6", "agent-2", 6, map[string]any{"speech": "Agent 2 latest objection."}, 13*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_trailing_malformed_round_robin_speech_7", "cmd_trailing_malformed_round_robin_speech_7", "agent-3", 7, map[string]any{"speech": "Agent 3 latest evidence."}, 14*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_trailing_malformed_round_robin_speech_8", "cmd_trailing_malformed_round_robin_speech_8", "agent-4", 8, map[string]any{"speech": "Agent 4 latest concern."}, 15*time.Second))
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_trailing_malformed_round_robin_"+name, map[string]any{"turn": 9}, 16*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_trailing_malformed_round_robin_"+name, map[string]any{"turn": 9, "intent": "answer", "reason": "trailing-malformed round-robin follow-up"}, 17*time.Second)
	return dataHome, metadata, sessionDir
}

func createCouncilForInvalidOwnHistoryTurnRoundRobin(t *testing.T) (string, *storage.SessionMetadata, string) {
	dataHome, loaded, _ := dispatchDataHomeWithMembers(t, "agent-1", "agent-2", "agent-3", "agent-4")
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_prompt_context_invalid_turn_round_robin_" + name, Title: "NEWFIX-004", Moderator: "agent-mod", EventID: "evt_created_invalid_turn_round_robin_" + name, CommandID: "cmd_created_invalid_turn_round_robin_" + name},
		Members: []string{"agent-1", "agent-2", "agent-3", "agent-4"},
		Now:     daemonFixedRuntime().Now(),
	}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_attendance_invalid_turn_round_robin_"+name, map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_attend_invalid_turn_round_robin_agent_1_"+name, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-2", "cmd_attend_invalid_turn_round_robin_agent_2_"+name, map[string]any{"status": "present", "summary": "ready"}, 3*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-3", "cmd_attend_invalid_turn_round_robin_agent_3_"+name, map[string]any{"status": "present", "summary": "ready"}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-4", "cmd_attend_invalid_turn_round_robin_agent_4_"+name, map[string]any{"status": "present", "summary": "ready"}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_invalid_turn_round_robin_"+name, fullAgendaPayload(), 6*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_invalid_turn_round_robin_"+name, map[string]any{"timeout_sec": 30}, 7*time.Second)
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_invalid_turn_round_robin_speech_1", "cmd_invalid_turn_round_robin_speech_1", "agent-1", 1, map[string]any{"turn": "bad", "speech": "Agent 1 invalid-turn follow-up."}, 8*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_invalid_turn_round_robin_speech_2", "cmd_invalid_turn_round_robin_speech_2", "agent-2", 2, map[string]any{"speech": "Agent 2 counterpoint."}, 9*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_invalid_turn_round_robin_speech_3", "cmd_invalid_turn_round_robin_speech_3", "agent-3", 3, map[string]any{"speech": "Agent 3 supporting note."}, 10*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_invalid_turn_round_robin_speech_4", "cmd_invalid_turn_round_robin_speech_4", "agent-4", 4, map[string]any{"speech": "Agent 4 implementation concern."}, 11*time.Second))
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_invalid_turn_round_robin_"+name, map[string]any{"turn": 9}, 16*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_invalid_turn_round_robin_"+name, map[string]any{"turn": 9, "intent": "answer", "reason": "invalid-turn round-robin follow-up"}, 17*time.Second)
	return dataHome, metadata, sessionDir
}

func createCouncilForOwnHistoryRoundRobin(t *testing.T) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHomeWithMembers(t, "agent-1", "agent-2", "agent-3", "agent-4")
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_prompt_context_round_robin_" + name, Title: "NEWFIX-004", Moderator: "agent-mod", EventID: "evt_created_round_robin_" + name, CommandID: "cmd_created_round_robin_" + name},
		Members: []string{"agent-1", "agent-2", "agent-3", "agent-4"},
		Now:     daemonFixedRuntime().Now(),
	}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_attendance_round_robin_"+name, map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_attend_round_robin_agent_1_"+name, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-2", "cmd_attend_round_robin_agent_2_"+name, map[string]any{"status": "present", "summary": "ready"}, 3*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-3", "cmd_attend_round_robin_agent_3_"+name, map[string]any{"status": "present", "summary": "ready"}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-4", "cmd_attend_round_robin_agent_4_"+name, map[string]any{"status": "present", "summary": "ready"}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_round_robin_"+name, fullAgendaPayload(), 6*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_round_robin_"+name, map[string]any{"timeout_sec": 30}, 7*time.Second)
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_round_robin_speech_1", "cmd_round_robin_speech_1", "agent-1", 1, map[string]any{"speech": "Agent 1 opening position.", "claims": []any{map[string]any{"claim_id": "T1.C1", "summary": "State the baseline rationale.", "kind": "requirement"}}}, 8*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_round_robin_speech_2", "cmd_round_robin_speech_2", "agent-2", 2, map[string]any{"speech": "Agent 2 counterpoint."}, 9*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_round_robin_speech_3", "cmd_round_robin_speech_3", "agent-3", 3, map[string]any{"speech": "Agent 3 supporting note."}, 10*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_round_robin_speech_4", "cmd_round_robin_speech_4", "agent-4", 4, map[string]any{"speech": "Agent 4 implementation concern."}, 11*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_round_robin_speech_5", "cmd_round_robin_speech_5", "agent-1", 5, map[string]any{"speech": "Agent 1 follow-up refinement.", "claims": []any{map[string]any{"claim_id": "T5.C1", "summary": "Preserve continuity beyond the recent window.", "kind": "requirement"}}}, 12*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_round_robin_speech_6", "cmd_round_robin_speech_6", "agent-2", 6, map[string]any{"speech": "Agent 2 latest objection."}, 13*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_round_robin_speech_7", "cmd_round_robin_speech_7", "agent-3", 7, map[string]any{"speech": "Agent 3 latest evidence."}, 14*time.Second))
	appendRawPromptContextEvent(t, sessionDir, metadata, roundRobinSpeechEvent(metadata, "evt_speech_cmd_round_robin_speech_8", "cmd_round_robin_speech_8", "agent-4", 8, map[string]any{"speech": "Agent 4 latest concern."}, 15*time.Second))

	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_round_robin_"+name, map[string]any{"turn": 9}, 16*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_round_robin_"+name, map[string]any{"turn": 9, "intent": "answer", "reason": "round-robin follow-up"}, 17*time.Second)
	return dataHome, metadata, sessionDir
}
func roundRobinSpeechEvent(metadata *storage.SessionMetadata, eventID, commandID, member string, turn int, payload map[string]any, offset time.Duration) storage.EventEnvelope {
	typedPayload := map[string]any{"turn": turn}
	for key, value := range payload {
		typedPayload[key] = value
	}
	return storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventID,
		CommandID:     commandID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "speech",
		From:          member,
		To:            []string{metadata.Moderator},
		CreatedAt:     daemonFixedRuntime().Now().Add(offset),
		Payload:       typedPayload,
	}
}

func createCouncilForPromptContext(t *testing.T, agendaPayload map[string]any) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHome(t)
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_prompt_context_" + name, Title: "NEWFIX-001", Moderator: "agent-mod", EventID: "evt_created_" + name, CommandID: "cmd_created_" + name},
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
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_"+name, agendaPayload, 3*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_"+name, map[string]any{"timeout_sec": 30}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_"+name, map[string]any{"turn": 1}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_"+name, map[string]any{"turn": 1, "intent": "answer", "reason": "selected"}, 6*time.Second)
	return dataHome, metadata, sessionDir
}

func createCouncilForPromptContextWithoutAgenda(t *testing.T) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHome(t)
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_prompt_context_no_agenda_" + name, Title: "NEWFIX-009", Moderator: "agent-mod", EventID: "evt_created_no_agenda_" + name, CommandID: "cmd_created_no_agenda_" + name},
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
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_attendance_no_agenda_"+name, map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_attend_no_agenda_"+name, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_no_agenda_"+name, map[string]any{"timeout_sec": 30}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_no_agenda_"+name, map[string]any{"turn": 1}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_raise_no_agenda_"+name, map[string]any{"turn": 1, "intent": "answer", "reason": "selected"}, 6*time.Second)
	return dataHome, metadata, sessionDir
}

func createCouncilForPromptContextWithoutRaise(t *testing.T, agendaPayload map[string]any) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHome(t)
	name := strings.NewReplacer("/", "_", "\x00", "_").Replace(t.Name())
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{ID: "sess_prompt_context_no_raise_" + name, Title: "NEWFIX-001", Moderator: "agent-mod", EventID: "evt_created_no_raise_" + name, CommandID: "cmd_created_no_raise_" + name},
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
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_attendance_no_raise_"+name, map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_attend_no_raise_"+name, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_agenda_no_raise_"+name, agendaPayload, 3*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prepare_no_raise_"+name, map[string]any{"timeout_sec": 30}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_poll_no_raise_"+name, map[string]any{"turn": 1}, 5*time.Second)
	return dataHome, metadata, sessionDir
}

func appendRawPromptContextEvent(t *testing.T, sessionDir string, metadata *storage.SessionMetadata, event storage.EventEnvelope) {
	t.Helper()
	if _, err := storage.AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent %s: %v", event.EventID, err)
	}
}

func latestEventOfType(t *testing.T, events []storage.EventEnvelope, typ string) storage.EventEnvelope {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == typ {
			return events[i]
		}
	}
	t.Fatalf("missing latest event type %q in %#v", typ, events)
	return storage.EventEnvelope{}
}

func anyStringSliceTest(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if ok && strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringValueTest(value any) string {
	text, _ := value.(string)
	return text
}

func fullAgendaPayload() map[string]any {
	return map[string]any{
		"decision_question":   "What should ship?",
		"success_criteria":    "Produce a canonical typed speech with agenda and prior context.",
		"out_of_scope_policy": "Do not invent agenda text or repair missing control context from plugin hints.",
	}
}
