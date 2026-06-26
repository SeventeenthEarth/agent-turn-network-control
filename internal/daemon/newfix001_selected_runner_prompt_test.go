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
	if !containsString(anyStringSliceTest(evidenceEvent.Payload["included_context"]), "prior_claim_context") {
		t.Fatalf("prompt evidence should record prior_claim_context inclusion: %#v", evidenceEvent.Payload)
	}
	if !containsString(anyStringSliceTest(evidenceEvent.Payload["prior_context_source_event_ids"]), "evt_speech_cmd_prior_speak_turn_1") {
		t.Fatalf("prompt evidence should record prior context source ids: %#v", evidenceEvent.Payload)
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

	transcript, err := storage.RenderTranscript(sessionDir, metadata, storage.TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript: %v", err)
	}
	for _, want := range []string{"Selected Runner Prompt Evidence", "prompt_context_sha256", "prior_context_source_event_ids"} {
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
}

func TestNEWFIX001CouncilGrantBlocksWhenAgendaContextMissing(t *testing.T) {
	dataHome, metadata, sessionDir := createCouncilForPromptContext(t, map[string]any{"decision_question": "What should ship?"})
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
	for _, want := range []string{"success_criteria", "out_of_scope_policy"} {
		if !containsString(missing, want) {
			t.Fatalf("blocked prompt evidence missing %q in %#v", want, evidenceEvent.Payload)
		}
	}
	diagnostic := latestEventOfType(t, index.Events, "selected_runner_dispatch_failed")
	if diagnostic.Payload["reason"] != "selected_runner_context_missing" {
		t.Fatalf("agenda-context block should leave durable selected-runner diagnostic: %#v", diagnostic.Payload)
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
	if !containsString(missing, "prior_speech_context") {
		t.Fatalf("prior-context block should record prior_speech_context: %#v", evidenceEvent.Payload)
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
