package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atn-control/internal/protocol"
	"atn-control/internal/registry"
)

func TestUnitRUNFIX014RunnerFailureThenFallbackSpeechDoesNotPassSelectedRunner(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "failure_fallback")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_failure", "agent-1", 1, 0))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_started_failure", "runner_invocation_started", "evt_selected_failure", "run_failure", "agent-1", "started", 1*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_failed_failure", "runner_invocation_failed", "evt_selected_failure", "run_failure", "agent-1", "failed", 2*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeech(metadata, "evt_fallback_speech", "evt_selected_failure", "agent-1", 1, nil, map[string]any{
		"speech":           "Manual fallback speech after runner failure.",
		"fallback_profile": true,
		"source":           "manual_fallback_profile",
	}, 3*time.Second))

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	accounting := SelectedRunnerAccountingFromIndex(metadata, index)
	assertRUNFIX014FailureAccounting(t, accounting)

	status, err := CouncilStatusFromLogAt(sessionDir, metadata, fixedTranscriptTime())
	if err != nil {
		t.Fatalf("CouncilStatusFromLogAt: %v", err)
	}
	assertRUNFIX014FailureAccounting(t, status["selected_runner_accounting"].(SelectedRunnerAccounting))

	stream, err := StreamStatusFromLogAt(sessionDir, metadata, fixedTranscriptTime())
	if err != nil {
		t.Fatalf("StreamStatusFromLogAt: %v", err)
	}
	assertRUNFIX014FailureAccounting(t, stream.SelectedRunnerAccounting)
	member := stream.ParticipantRuntimeReadiness.Members[0]
	if member.SelectedRunnerPrerequisite == nil || member.SelectedRunnerPrerequisite.Ready || member.SelectedRunnerPrerequisite.Status != "runner_terminal_failure" {
		t.Fatalf("selected runner readiness should fail closed on terminal failure: %#v", member.SelectedRunnerPrerequisite)
	}

	transcript, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript: %v", err)
	}
	for _, want := range []string{"Selected Runner Accounting", "selected_runner_pass: `false`", "runner_failed_count: `1`", "manual_or_fallback_speech_count: `1`", "selected_runner_terminal_failure"} {
		if !strings.Contains(string(transcript), want) {
			t.Fatalf("RUNFIX-014 transcript missing %q:\n%s", want, string(transcript))
		}
	}

	result, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(result.BundleDir, "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v\n%s", err, string(manifestBytes))
	}
	manifestAccounting, ok := manifest["selected_runner_accounting"].(map[string]any)
	if !ok || manifestAccounting["selected_runner_pass"] != false || manifestAccounting["runner_failed_count"] != float64(1) || manifestAccounting["manual_or_fallback_speech_count"] != float64(1) {
		t.Fatalf("manifest selected_runner_accounting mismatch: %#v", manifest["selected_runner_accounting"])
	}
}

func TestUnitRUNFIX014RunnerDiscardThenFallbackSpeechDoesNotPassSelectedRunner(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "discard_fallback")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_discard", "agent-1", 1, 0))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_started_discard", "runner_invocation_started", "evt_selected_discard", "run_discard", "agent-1", "started", 1*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_discarded", "runner_result_discarded", "evt_selected_discard", "run_discard", "agent-1", "discarded_after_cancel", 2*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeech(metadata, "evt_fallback_speech_discard", "evt_selected_discard", "agent-1", 1, nil, map[string]any{
		"speech":           "Manual fallback speech after discarded runner result.",
		"fallback_profile": true,
		"source":           "manual_fallback_profile",
	}, 3*time.Second))

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	assertRUNFIX014TerminalAccounting(t, SelectedRunnerAccountingFromIndex(metadata, index), terminalAccountingWant{
		terminalDiscardCount: 1,
		diagnosticCode:       "selected_runner_terminal_discard",
		runnerStartedCount:   1,
	})
}

func TestUnitRUNFIX014DispatchFailureThenFallbackSpeechDoesNotPassSelectedRunner(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "dispatch_failure_fallback")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_dispatch", "agent-1", 1, 0))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingDispatchFailure(metadata, "evt_dispatch_failed", "evt_selected_dispatch", "agent-1", 1*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeech(metadata, "evt_fallback_speech_dispatch", "evt_selected_dispatch", "agent-1", 1, nil, map[string]any{
		"speech":           "Manual fallback speech after dispatch failure.",
		"fallback_profile": true,
		"source":           "manual_fallback_profile",
	}, 2*time.Second))

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	assertRUNFIX014TerminalAccounting(t, SelectedRunnerAccountingFromIndex(metadata, index), terminalAccountingWant{
		dispatchFailureCount: 1,
		diagnosticCode:       "selected_runner_dispatch_failure",
		runnerStartedCount:   0,
	})
}

func TestUnitRUNFIX014LinkedRunnerSpeechPassesSelectedRunner(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "linked_success")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_success", "agent-1", 1, 0))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_started_success", "runner_invocation_started", "evt_selected_success", "run_success", "agent-1", "started", 1*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_succeeded_success", "runner_invocation_succeeded", "evt_selected_success", "run_success", "agent-1", "succeeded", 2*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeech(metadata, "evt_runner_speech_success", "evt_selected_success", "agent-1", 1, &RunnerInfo{
		InvocationID:    "run_success",
		AdapterKind:     "hermes-agent",
		Member:          "agent-1",
		Attempt:         1,
		SourceCommandID: "cmd_runner_success",
		Status:          "succeeded",
	}, map[string]any{"speech": "Linked runner speech.", "surface_evidence": map[string]any{"status": "posted", "kind": "discord_thread", "thread_id": metadata.Surface.ThreadID, "message_id": "msg-runner-success", "references_event_id": "evt_runner_speech_success"}}, 3*time.Second))

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	accounting := SelectedRunnerAccountingFromIndex(metadata, index)
	if !accounting.SelectedRunnerPass || accounting.SelectedSpeakerCount != 1 || accounting.RunnerStartedCount != 1 || accounting.RunnerSucceededCount != 1 || accounting.LinkedRunnerSpeechCount != 1 || accounting.RunnerFailedCount != 0 {
		t.Fatalf("linked runner speech should pass selected-runner accounting: %#v", accounting)
	}
	if len(accounting.Diagnostics) != 0 {
		t.Fatalf("linked runner success should not emit diagnostics: %#v", accounting.Diagnostics)
	}

	stream, err := StreamStatusFromLogAt(sessionDir, metadata, fixedTranscriptTime())
	if err != nil {
		t.Fatalf("StreamStatusFromLogAt: %v", err)
	}
	member := stream.ParticipantRuntimeReadiness.Members[0]
	if member.SelectedRunnerPrerequisite == nil || !member.SelectedRunnerPrerequisite.Ready || member.SelectedRunnerPrerequisite.Status != "selected_runner_pass" {
		t.Fatalf("selected runner readiness should pass with linked runner speech: %#v", member.SelectedRunnerPrerequisite)
	}
}
func TestUnitRUNFIX3004LinkedRunnerSpeechWithoutVisibleDeliveryFailsSelectedRunner(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "missing_delivery")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_missing_delivery", "agent-1", 1, 0))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_started_missing_delivery", "runner_invocation_started", "evt_selected_missing_delivery", "run_missing_delivery", "agent-1", "started", 1*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_succeeded_missing_delivery", "runner_invocation_succeeded", "evt_selected_missing_delivery", "run_missing_delivery", "agent-1", "succeeded", 2*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeech(metadata, "evt_runner_speech_missing_delivery", "evt_selected_missing_delivery", "agent-1", 1, &RunnerInfo{
		InvocationID:    "run_missing_delivery",
		AdapterKind:     "hermes-agent",
		Member:          "agent-1",
		Attempt:         1,
		SourceCommandID: "cmd_runner_missing_delivery",
		Status:          "succeeded",
	}, map[string]any{"speech": "Linked runner speech without visible delivery evidence."}, 3*time.Second))

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	accounting := SelectedRunnerAccountingFromIndex(metadata, index)
	if accounting.SelectedRunnerPass {
		t.Fatalf("linked runner speech without visible delivery evidence must fail selected-runner accounting: %#v", accounting)
	}
	if !selectedRunnerDiagnosticsContain(accounting.Diagnostics, "missing_selected_runner_delivery_evidence") {
		t.Fatalf("missing selected-runner delivery diagnostic missing: %#v", accounting.Diagnostics)
	}
	if len(accounting.SelectedRunners) != 1 || accounting.SelectedRunners[0].Status != "missing_selected_runner_delivery_evidence" {
		t.Fatalf("grant should expose missing selected-runner delivery evidence: %#v", accounting.SelectedRunners)
	}

	stream, err := StreamStatusFromLogAt(sessionDir, metadata, fixedTranscriptTime())
	if err != nil {
		t.Fatalf("StreamStatusFromLogAt: %v", err)
	}
	member := stream.ParticipantRuntimeReadiness.Members[0]
	if member.SelectedRunnerPrerequisite == nil || member.SelectedRunnerPrerequisite.Ready || member.SelectedRunnerPrerequisite.Status != "missing_selected_runner_delivery_evidence" {
		t.Fatalf("selected runner readiness should fail closed without visible delivery evidence: %#v", member.SelectedRunnerPrerequisite)
	}
}

func TestUnitRUNFIX3004LinkedRunnerSpeechWithMismatchedThreadFailsSelectedRunner(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "wrong_thread")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_wrong_thread", "agent-1", 1, 0))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_started_wrong_thread", "runner_invocation_started", "evt_selected_wrong_thread", "run_wrong_thread", "agent-1", "started", 1*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_succeeded_wrong_thread", "runner_invocation_succeeded", "evt_selected_wrong_thread", "run_wrong_thread", "agent-1", "succeeded", 2*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeech(metadata, "evt_runner_speech_wrong_thread", "evt_selected_wrong_thread", "agent-1", 1, &RunnerInfo{
		InvocationID:    "run_wrong_thread",
		AdapterKind:     "hermes-agent",
		Member:          "agent-1",
		Attempt:         1,
		SourceCommandID: "cmd_runner_wrong_thread",
		Status:          "succeeded",
	}, map[string]any{"speech": "Linked runner speech with mismatched thread evidence.", "surface_evidence": map[string]any{"status": "posted", "kind": "discord_thread", "thread_id": "thread-other", "message_id": "msg-runner-wrong-thread", "references_event_id": "evt_runner_speech_wrong_thread"}}, 3*time.Second))

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	accounting := SelectedRunnerAccountingFromIndex(metadata, index)
	if accounting.SelectedRunnerPass {
		t.Fatalf("linked runner speech with mismatched thread must fail selected-runner accounting: %#v", accounting)
	}
	if !selectedRunnerDiagnosticsContain(accounting.Diagnostics, "selected_runner_delivery_thread_mismatch") {
		t.Fatalf("selected-runner delivery thread mismatch diagnostic missing: %#v", accounting.Diagnostics)
	}
	if len(accounting.SelectedRunners) != 1 || accounting.SelectedRunners[0].Status != "selected_runner_delivery_thread_mismatch" {
		t.Fatalf("grant should expose selected-runner delivery thread mismatch: %#v", accounting.SelectedRunners)
	}
}

func TestUnitRUNFIX014LinkedRunnerSpeechRequiresSucceededStatus(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "linked_failed_status")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_failed_status", "agent-1", 1, 0))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_started_failed_status", "runner_invocation_started", "evt_selected_failed_status", "run_failed_status", "agent-1", "started", 1*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeech(metadata, "evt_runner_speech_failed_status", "evt_selected_failed_status", "agent-1", 1, &RunnerInfo{
		InvocationID:    "run_failed_status",
		AdapterKind:     "hermes-agent",
		Member:          "agent-1",
		Attempt:         1,
		SourceCommandID: "cmd_runner_failed_status",
		Status:          "failed",
	}, map[string]any{"speech": "Linked runner speech with non-succeeded runner status."}, 2*time.Second))

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	accounting := SelectedRunnerAccountingFromIndex(metadata, index)
	if accounting.SelectedRunnerPass || accounting.LinkedRunnerSpeechCount != 0 || accounting.RunnerSucceededCount != 0 {
		t.Fatalf("linked runner speech with failed status must not pass or count as linked success: %#v", accounting)
	}
	if !selectedRunnerDiagnosticsContain(accounting.Diagnostics, "linked_runner_speech_status_not_succeeded") {
		t.Fatalf("non-succeeded linked runner speech diagnostic missing: %#v", accounting.Diagnostics)
	}
	if len(accounting.SelectedRunners) != 1 || accounting.SelectedRunners[0].Status != "missing_runner_invocation_succeeded" {
		t.Fatalf("grant should remain blocked until runner_invocation_succeeded is present: %#v", accounting.SelectedRunners)
	}
}

func TestUnitRUNFIX014LinkedRunnerSpeechWithoutRunnerSuccessDoesNotInflateSuccessCount(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "linked_speech_without_success")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_no_success", "agent-1", 1, 0))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingRunnerEvent(metadata, "evt_runner_started_no_success", "runner_invocation_started", "evt_selected_no_success", "run_no_success", "agent-1", "started", 1*time.Second))
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, selectedRunnerAccountingSpeech(metadata, "evt_runner_speech_no_success", "evt_selected_no_success", "agent-1", 1, &RunnerInfo{
		InvocationID:    "run_no_success",
		AdapterKind:     "hermes-agent",
		Member:          "agent-1",
		Attempt:         1,
		SourceCommandID: "cmd_runner_no_success",
		Status:          "succeeded",
	}, map[string]any{"speech": "Runner-linked speech without durable success event."}, 2*time.Second))

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	accounting := SelectedRunnerAccountingFromIndex(metadata, index)
	if accounting.SelectedRunnerPass || accounting.RunnerSucceededCount != 0 || accounting.LinkedRunnerSpeechCount != 1 {
		t.Fatalf("linked speech without runner_invocation_succeeded must remain non-passing and not count runner success: %#v", accounting)
	}
	if len(accounting.SelectedRunners) != 1 || accounting.SelectedRunners[0].Status != "missing_runner_invocation_succeeded" {
		t.Fatalf("grant should expose missing durable runner success status: %#v", accounting.SelectedRunners)
	}
	stream, err := StreamStatusFromLogAt(sessionDir, metadata, fixedTranscriptTime())
	if err != nil {
		t.Fatalf("StreamStatusFromLogAt: %v", err)
	}
	member := stream.ParticipantRuntimeReadiness.Members[0]
	if member.SelectedRunnerPrerequisite == nil || member.SelectedRunnerPrerequisite.Ready || member.SelectedRunnerPrerequisite.Status != "missing_runner_invocation_succeeded" {
		t.Fatalf("selected runner readiness should fail closed without runner_invocation_succeeded: %#v", member.SelectedRunnerPrerequisite)
	}
}

func assertRUNFIX014FailureAccounting(t *testing.T, accounting SelectedRunnerAccounting) {
	t.Helper()
	if accounting.SelectedRunnerPass {
		t.Fatalf("selected_runner_pass must be false after terminal runner failure: %#v", accounting)
	}
	if accounting.SelectedSpeakerCount != 1 || accounting.RunnerStartedCount != 1 || accounting.RunnerFailedCount != 1 || accounting.RunnerlessSpeechCount != 1 || accounting.ManualOrFallbackSpeechCount != 1 || accounting.LinkedRunnerSpeechCount != 0 {
		t.Fatalf("selected runner failure/fallback counts mismatch: %#v", accounting)
	}
	if !selectedRunnerDiagnosticsContain(accounting.Diagnostics, "selected_runner_terminal_failure") || !selectedRunnerDiagnosticsContain(accounting.Diagnostics, "manual_or_fallback_speech_not_selected_runner_evidence") {
		t.Fatalf("selected runner failure/fallback diagnostics missing: %#v", accounting.Diagnostics)
	}
}

type terminalAccountingWant struct {
	terminalDiscardCount int
	dispatchFailureCount int
	diagnosticCode       string
	runnerStartedCount   int
}

func assertRUNFIX014TerminalAccounting(t *testing.T, accounting SelectedRunnerAccounting, want terminalAccountingWant) {
	t.Helper()
	if accounting.SelectedRunnerPass {
		t.Fatalf("selected_runner_pass must be false after terminal selected-runner diagnostic: %#v", accounting)
	}
	if accounting.SelectedSpeakerCount != 1 ||
		accounting.RunnerStartedCount != want.runnerStartedCount ||
		accounting.RunnerFailedCount != 0 ||
		accounting.TerminalDiscardCount != want.terminalDiscardCount ||
		accounting.DispatchFailureCount != want.dispatchFailureCount ||
		accounting.RunnerlessSpeechCount != 1 ||
		accounting.ManualOrFallbackSpeechCount != 1 ||
		accounting.LinkedRunnerSpeechCount != 0 {
		t.Fatalf("selected runner terminal/fallback counts mismatch: %#v", accounting)
	}
	if !selectedRunnerDiagnosticsContain(accounting.Diagnostics, want.diagnosticCode) || !selectedRunnerDiagnosticsContain(accounting.Diagnostics, "manual_or_fallback_speech_not_selected_runner_evidence") {
		t.Fatalf("selected runner terminal/fallback diagnostics missing: %#v", accounting.Diagnostics)
	}
}

func selectedRunnerDiagnosticsContain(diagnostics []SelectedRunnerDiagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func createSelectedRunnerAccountingSession(t *testing.T, suffix string) (string, *SessionMetadata) {
	t.Helper()
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.ID = "sess_runfix014_" + suffix
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "RUNFIX-014"
	metadata.Moderator = "agent-mod"
	metadata.Participants = []string{"agent-mod", "agent-1"}
	metadata.State.Phase = "discussion"
	metadata.TurnMode = "moderator_direct"
	metadata.Surface = &Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "thread-runfix014-" + suffix}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write registry snapshot: %v", err)
	}
	return sessionDir, metadata
}

func selectedRunnerAccountingSpeakerSelected(metadata *SessionMetadata, eventID, member string, turn int, offset time.Duration) EventEnvelope {
	return EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventID,
		CommandID:     "cmd_" + eventID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "speaker_selected",
		From:          metadata.Moderator,
		To:            []string{member},
		CreatedAt:     fixedTranscriptTime().Add(offset),
		Payload:       map[string]any{"turn": float64(turn), "member": member, "selection_mode": "moderator_direct"},
	}
}

func selectedRunnerAccountingRunnerEvent(metadata *SessionMetadata, eventID, typ, selectedEventID, invocationID, member, status string, offset time.Duration) EventEnvelope {
	event := EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventID,
		CommandID:        "cmd_runner_" + invocationID,
		CausationEventID: selectedEventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            "discussion",
		Type:             typ,
		From:             "atn-controld",
		To:               []string{member},
		CreatedAt:        fixedTranscriptTime().Add(offset),
		Runner: &RunnerInfo{
			InvocationID:    invocationID,
			AdapterKind:     "hermes-agent",
			Member:          member,
			Attempt:         1,
			SourceCommandID: "cmd_runner_" + invocationID,
			Status:          status,
		},
		Payload: map[string]any{"selected_event_id": selectedEventID},
	}
	if typ != "runner_invocation_started" {
		event.Cost = json.RawMessage("null")
	}
	return event
}

func selectedRunnerAccountingDispatchFailure(metadata *SessionMetadata, eventID, selectedEventID, member string, offset time.Duration) EventEnvelope {
	return EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventID,
		CommandID:        "cmd_" + eventID,
		CausationEventID: selectedEventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            "discussion",
		Type:             "selected_runner_dispatch_failed",
		From:             "atn-controld",
		To:               []string{member},
		CreatedAt:        fixedTranscriptTime().Add(offset),
		Payload: map[string]any{
			"selected_event_id": selectedEventID,
			"selected_member":   member,
			"reason":            "selected_runner_preflight_failed",
		},
	}
}

func selectedRunnerAccountingSpeech(metadata *SessionMetadata, eventID, selectedEventID, member string, turn int, runner *RunnerInfo, payload map[string]any, offset time.Duration) EventEnvelope {
	event := EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventID,
		CommandID:        "cmd_" + eventID,
		CausationEventID: selectedEventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            "discussion",
		Type:             "speech",
		From:             member,
		To:               []string{metadata.Moderator},
		CreatedAt:        fixedTranscriptTime().Add(offset),
		Runner:           runner,
		Payload:          payload,
	}
	event.Payload["turn"] = float64(turn)
	if runner != nil {
		event.Cost = json.RawMessage(`{"tokens_in":1,"tokens_out":1,"usd_estimate":0.01,"source":"fixture"}`)
	}
	return event
}

func appendSelectedRunnerAccountingEvent(t *testing.T, sessionDir string, metadata *SessionMetadata, event EventEnvelope) {
	t.Helper()
	if _, err := AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent %s: %v", event.EventID, err)
	}
}
