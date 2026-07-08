package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SeventeenthEarth/agent-turn-network-control/internal/protocol"
	"github.com/SeventeenthEarth/agent-turn-network-control/internal/registry"
)

func TestUnitTranscriptMarkdownAndJSONLAreDeterministic(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.State.Phase = "accepted"
	metadata.Status = StatusTerminal
	metadata.Cost = CostSummary{USDEstimateTotal: 1.25, RunnerCallsTotal: 2, MissingCostRunnerCallsTotal: 1}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_review_001",
		CommandID:     "cmd_review_001",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "under_review",
		Type:          "review_requested",
		From:          "agent-mod",
		To:            []string{"agent-1"},
		CreatedAt:     fixedTranscriptTime(),
		Payload:       map[string]any{"review": map[string]any{"focus": []string{"tests"}}, "recipients": []string{"agent-1"}},
	})
	appendTranscriptEvent(t, sessionDir, metadata, EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_runner_001",
		CommandID:     "cmd_runner_001",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "accepted",
		Type:          "runner_invocation_failed",
		From:          "atn-controld",
		To:            []string{"agent-mod"},
		CreatedAt:     fixedTranscriptTime().Add(time.Minute),
		Runner:        &RunnerInfo{InvocationID: "run_001", AdapterKind: "fake", Member: "agent-1", Attempt: 1, Status: "failed"},
		Cost:          json.RawMessage(`{"usd_estimate":1.25}`),
		Payload:       map[string]any{"summary": "runner failed closed"},
	})

	first, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	second, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md again: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("markdown transcript is not deterministic")
	}
	for _, want := range []string{"Runner And Cost Summary", "runner_calls_total", "usd_estimate_total", "review_requested", "runner_invocation_failed", "cost:"} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("markdown transcript missing %q:\n%s", want, string(first))
		}
	}

	jsonl, err := RenderTranscript(sessionDir, metadata, TranscriptJSONLFormat)
	if err != nil {
		t.Fatalf("RenderTranscript jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(jsonl)), "\n")
	if len(lines) != 2 {
		t.Fatalf("jsonl event count = %d, want 2: %s", len(lines), string(jsonl))
	}
	if !strings.Contains(lines[0], `"event_id":"evt_review_001"`) || !strings.Contains(lines[1], `"event_id":"evt_runner_001"`) {
		t.Fatalf("jsonl order not stable: %s", string(jsonl))
	}
}

func TestUnitTranscriptCouncilLinkedAuthorityEvidence(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "Council transcript"
	metadata.State.Phase = "finalized"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	metadata.LinkedAuthority = &LinkedAuthority{KanbanCardID: "t_trans_001"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	for _, event := range []EventEnvelope{
		councilTranscriptEvent(metadata, "evt_attend_001", "preparation", "member_attended", map[string]any{"attendance": map[string]any{"agent-1": "present"}}),
		councilTranscriptEvent(metadata, "evt_agenda_001", "discussion", "agenda_locked", map[string]any{"agenda": []string{"decide export shape"}}),
		councilTranscriptEvent(metadata, "evt_final_001", "finalized", "council_finalized", map[string]any{"linked_authority_result": map[string]any{"status": "posted", "evidence": "fixture-only"}}),
	} {
		appendTranscriptEvent(t, sessionDir, metadata, event)
	}
	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	for _, want := range []string{"linked_authority", "attendance", "agenda", "linked_authority_result", "fixture-only"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("council transcript missing %q:\n%s", want, string(out))
		}
	}
}

func TestUnitSURFD002TranscriptProjectsVisibleSurfaceDeliveryStates(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "SURFD-002 surface projection"
	metadata.State.Phase = "finalized"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	metadata.Surface = &Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "thread-surfd-002", DeliveryOwner: "moderator_runtime"}
	metadata.LinkedAuthority = &LinkedAuthority{KanbanCardID: "t_surfd_002"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_select_001", "discussion", "speaker_selected", map[string]any{"turn": float64(1), "selection_mode": "role_order"}))
	appendTranscriptEvent(t, sessionDir, metadata, EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_speech_001",
		CommandID:     "cmd_evt_speech_001",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "speech",
		From:          "agent-1",
		To:            []string{"agent-mod", "agent-2"},
		CreatedAt:     fixedTranscriptTime().Add(time.Minute),
		Payload:       map[string]any{"turn": float64(1), "speech": "Visible speech from selected participant."},
	})
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_final_posted", "finalized", "council_finalized", map[string]any{
		"final_summary":           "accepted",
		"surface_evidence":        map[string]any{"status": "posted", "kind": "discord_thread", "thread_id": "thread-surfd-002", "final_message_id": "msg-final-001"},
		"linked_authority_result": map[string]any{"status": "posted", "kanban_card_id": "t_surfd_002", "kanban_comment_id": "comment-001"},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_final_string_any_evidence", "finalized", "council_finalized", map[string]any{
		"final_summary":    "accepted with string evidence array",
		"surface_evidence": map[string]any{"status": "posted", "evidence": []any{"discord://channels/thread-surfd-002/msg-any-001"}},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_unresolved_failed", "unresolved", "council_unresolved", map[string]any{
		"reason":                  "blocked follow-up",
		"surface_evidence":        map[string]any{"status": "failed", "failure_reason": "discord write failed"},
		"linked_authority_result": map[string]any{"status": "pending_followup", "followup_card_id": "t_followup"},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_cancel_missing", "cancelled", "session_cancelled", map[string]any{"reason": "user cancelled before visible post"}))

	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	for _, want := range []string{
		"Visible Surface Projection Summary",
		"evt_speech_001", "speech", "renderable", "turn=1 selected=agent-1",
		"evt_final_posted", "visible_surface", "posted", "msg-final-001", "linked_authority", "comment-001",
		"evt_final_string_any_evidence", "discord://channels/thread-surfd-002/msg-any-001",
		"evt_unresolved_failed", "failed", "pending_followup", "discord write failed", "t_followup",
		"evt_cancel_missing", "missing/unproven",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("SURFD-002 transcript missing %q:\n%s", want, string(out))
		}
	}
	if strings.Contains(string(out), "| `evt_final_string_any_evidence` | `council_finalized` | `visible_surface` | `posted` |") {
		t.Fatalf("generic evidence[] must not prove posted visible closeout delivery:\n%s", string(out))
	}
}

func TestUnitRUNFIX3004TranscriptProjectionFailsClosedOnCloseoutDiagnostics(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "RUNFIX3-004 closeout diagnostics"
	metadata.State.Phase = "unresolved"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	metadata.Surface = &Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "thread-runfix3004-transcript"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_unresolved_diag", "unresolved", "council_unresolved", map[string]any{
		"reason":           "thread mismatch",
		"surface_evidence": map[string]any{"status": "posted", "kind": "discord_thread", "thread_id": "thread-other", "final_message_id": "msg-thread-other"},
		"closeout_diagnostics": []any{
			map[string]any{"code": "exact_thread_mismatch", "stage": "unresolved", "expected_thread_id": "thread-runfix3004-transcript", "observed_thread_id": "thread-other", "reason": "visible closeout thread does not match the configured council thread"},
		},
	}))
	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	if strings.Contains(string(out), "| `evt_unresolved_diag` | `council_unresolved` | `visible_surface` | `posted` |") {
		t.Fatalf("closeout diagnostics must override posted visible-surface status:\n%s", string(out))
	}
	for _, want := range []string{"missing/unproven", "closeout_diagnostics", "exact_thread_mismatch", "thread-runfix3004-transcript", "thread-other"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("RUNFIX3-004 transcript missing %q:\n%s", want, string(out))
		}
	}
}

func TestUnitRUNFIX3004ExportManifestIncludesCloseoutDiagnostics(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "RUNFIX3-004 export diagnostics"
	metadata.State.Phase = "unresolved"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	metadata.Surface = &Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "thread-runfix3004-export"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_unresolved_export", "unresolved", "council_unresolved", map[string]any{
		"reason": "proof missing",
		"closeout_diagnostics": []any{
			map[string]any{"code": "missing_visible_closeout_proof", "stage": "unresolved", "expected_thread_id": "thread-runfix3004-export", "reason": "surface_evidence is missing"},
		},
	}))
	if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	result, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(result.BundleDir, "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	for _, want := range []string{"closeout_diagnostics", "missing_visible_closeout_proof", "thread-runfix3004-export"} {
		if !strings.Contains(string(manifestBytes), want) {
			t.Fatalf("bundle manifest missing %q:\n%s", want, string(manifestBytes))
		}
	}
}

func TestUnitSURFD002ExportManifestDeclaresVisibleEvidenceProjection(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "SURFD-002 export projection"
	metadata.State.Phase = "finalized"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	metadata.Surface = &Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "thread-surfd-export"}
	metadata.LinkedAuthority = &LinkedAuthority{KanbanCardID: "t_surfd_export"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_final_export", "finalized", "council_finalized", map[string]any{
		"surface_evidence":        map[string]any{"status": "posted", "kind": "discord_thread", "final_message_id": "msg-export-final"},
		"linked_authority_result": map[string]any{"status": "posted", "kanban_comment_id": "comment-export"},
	}))
	if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	result, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(result.BundleDir, "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	for _, want := range []string{"surface_delivery_projection", "visible_surface", "linked_authority", "msg-export-final", "comment-export"} {
		if !strings.Contains(string(manifestBytes), want) {
			t.Fatalf("bundle manifest missing %q:\n%s", want, string(manifestBytes))
		}
	}
}

func TestUnitRUNFIX016ExportManifestDeclaresSummaryTurnAccounting(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "RUNFIX-016 summary accounting"
	metadata.State.Phase = "finalized"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_select_plugin", "discussion", "speaker_selected", map[string]any{"turn": float64(1), "member": "agent-1", "selection_mode": "role_order"}))
	appendTranscriptEvent(t, sessionDir, metadata, runfix016SpeechEvent(metadata, "evt_speech_plugin", "agent-1", 1, &RunnerInfo{InvocationID: "run_plugin_001", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, Status: "succeeded"}, map[string]any{
		"speech":          "Plugin evidence object speech.",
		"plugin_evidence": map[string]any{"visible_message_id": "msg-plugin-001"},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_select_object", "discussion", "speaker_selected", map[string]any{"turn": float64(2), "member": "agent-2", "selection_mode": "role_order"}))
	appendTranscriptEvent(t, sessionDir, metadata, runfix016SpeechEvent(metadata, "evt_speech_object", "agent-2", 2, nil, map[string]any{
		"speech":   "Evidence object speech.",
		"evidence": map[string]any{"kind": "discord_message", "message_id": "msg-object-002"},
	}))
	// Summary accounting may expose an explicit Discord message id for turn-level audit,
	// but that id must not by itself prove visible delivery success.
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_select_list", "discussion", "speaker_selected", map[string]any{"turn": float64(3), "member": "agent-1", "selection_mode": "role_order"}))
	appendTranscriptEvent(t, sessionDir, metadata, runfix016SpeechEvent(metadata, "evt_speech_list", "agent-1", 3, nil, map[string]any{
		"speech": "Evidence list speech.",
		"evidence": []any{
			map[string]any{"kind": "doc", "uri": "doc://not-visible"},
			map[string]any{"kind": "discord_message", "discord_message_id": "msg-list-003"},
		},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_final_dict", "finalized", "council_finalized", map[string]any{
		"final_summary": "dict evidence does not block export",
		"evidence":      map[string]any{"note": "summary-only"},
	}))
	if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	manifest := runfix016BuildManifest(t, sessionDir, metadata)
	rows := runfix016SummaryRows(t, manifest)
	if len(rows) != 3 {
		t.Fatalf("summary_turn_accounting row count = %d, want 3: %#v", len(rows), rows)
	}
	assertRUNFIX016SummaryRow(t, rows[0], map[string]any{
		"turn": float64(1), "member": "agent-1", "speaker_selected_event_id": "evt_select_plugin", "speech_event_id": "evt_speech_plugin", "runner_invocation_id": "run_plugin_001", "visible_message_id": "msg-plugin-001",
	})
	assertRUNFIX016SummaryRow(t, rows[1], map[string]any{
		"turn": float64(2), "member": "agent-2", "speaker_selected_event_id": "evt_select_object", "speech_event_id": "evt_speech_object", "runner_invocation_id": "", "visible_message_id": "msg-object-002",
	})
	assertRUNFIX016SummaryRow(t, rows[2], map[string]any{
		"turn": float64(3), "member": "agent-1", "speaker_selected_event_id": "evt_select_list", "speech_event_id": "evt_speech_list", "runner_invocation_id": "", "visible_message_id": "msg-list-003",
	})
}

func TestUnitRUNFIX016UnsupportedEvidenceDoesNotBecomeVisibleMessageOrDeliveryProof(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "RUNFIX-016 unsupported evidence"
	metadata.State.Phase = "finalized"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_select_unsupported", "discussion", "speaker_selected", map[string]any{"turn": float64(1), "member": "agent-1", "selection_mode": "role_order"}))
	appendTranscriptEvent(t, sessionDir, metadata, runfix016SpeechEvent(metadata, "evt_speech_unsupported", "agent-1", 1, nil, map[string]any{
		"speech": "Unsupported evidence must not become a visible message.",
		"evidence": []any{
			map[string]any{"kind": "doc", "uri": "doc://not-visible"},
			map[string]any{"nested": map[string]any{"message_id": "msg-nested-must-not-count"}},
		},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_select_generic", "discussion", "speaker_selected", map[string]any{"turn": float64(2), "member": "agent-2", "selection_mode": "role_order"}))
	appendTranscriptEvent(t, sessionDir, metadata, runfix016SpeechEvent(metadata, "evt_speech_generic", "agent-2", 2, nil, map[string]any{
		"speech":   "Generic evidence message_id must not become a visible message.",
		"evidence": map[string]any{"kind": "doc", "message_id": "msg-generic-must-not-count"},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_final_unsupported", "finalized", "council_finalized", map[string]any{
		"surface_evidence": map[string]any{
			"status":   "posted",
			"evidence": []any{map[string]any{"uri": "doc://not-delivery-proof"}},
		},
		"linked_authority_result": map[string]any{
			"status":   "posted",
			"evidence": map[string]any{"comment_id": "comment-nested-must-not-count"},
		},
	}))
	if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	manifest := runfix016BuildManifest(t, sessionDir, metadata)
	rows := runfix016SummaryRows(t, manifest)
	if len(rows) != 2 {
		t.Fatalf("summary_turn_accounting row count = %d, want 2: %#v", len(rows), rows)
	}
	assertRUNFIX016SummaryRow(t, rows[0], map[string]any{
		"turn": float64(1), "member": "agent-1", "speaker_selected_event_id": "evt_select_unsupported", "speech_event_id": "evt_speech_unsupported", "runner_invocation_id": "", "visible_message_id": "",
	})
	assertRUNFIX016SummaryRow(t, rows[1], map[string]any{
		"turn": float64(2), "member": "agent-2", "speaker_selected_event_id": "evt_select_generic", "speech_event_id": "evt_speech_generic", "runner_invocation_id": "", "visible_message_id": "",
	})

	projection, ok := manifest["surface_delivery_projection"].([]any)
	if !ok {
		t.Fatalf("surface_delivery_projection missing: %#v", manifest["surface_delivery_projection"])
	}
	seenTargets := map[string]string{}
	for _, raw := range projection {
		row, ok := raw.(map[string]any)
		if !ok || row["event_id"] != "evt_final_unsupported" {
			continue
		}
		target, _ := row["target"].(string)
		status, _ := row["status"].(string)
		seenTargets[target] = status
	}
	for _, target := range []string{"visible_surface", "linked_authority"} {
		status, ok := seenTargets[target]
		if !ok {
			t.Fatalf("unsupported final evidence must produce fail-closed projection row for %s: %#v", target, projection)
		}
		if status != "missing/unproven" {
			t.Fatalf("unsupported list/map evidence for %s must stay missing/unproven, got %q", target, status)
		}
	}
	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript: %v", err)
	}
	for _, forbidden := range []string{
		"| `evt_final_unsupported` | `council_finalized` | `visible_surface` | `posted` |",
		"| `evt_final_unsupported` | `council_finalized` | `linked_authority` | `posted` |",
		"selected_runner_pass: `true`",
	} {
		if strings.Contains(string(out), forbidden) {
			t.Fatalf("unsupported evidence promoted forbidden claim %q:\n%s", forbidden, string(out))
		}
	}
	if strings.Count(string(out), "missing/unproven") < 2 {
		t.Fatalf("unsupported delivery evidence should fail closed as missing/unproven:\n%s", string(out))
	}
}

func TestUnitRUNFIX016ExportBundleToleratesFinalizedDictListAndMissingEvidence(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{name: "dict", payload: map[string]any{"final_summary": "dict optional evidence", "evidence": map[string]any{"note": "summary-only"}}},
		{name: "list", payload: map[string]any{"final_summary": "list optional evidence", "evidence": []any{map[string]any{"uri": "doc://summary"}}}},
		{name: "missing", payload: map[string]any{"final_summary": "missing optional evidence"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir := createBareSessionDir(t)
			metadata := testMetadata()
			metadata.SessionType = SessionTypeCouncil
			metadata.Title = "RUNFIX-016 finalized " + tc.name
			metadata.State.Phase = "finalized"
			metadata.Status = StatusTerminal
			metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
			if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
				t.Fatalf("WriteSessionYAMLAtomic: %v", err)
			}
			appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_select_"+tc.name, "discussion", "speaker_selected", map[string]any{"turn": float64(1), "member": "agent-1", "selection_mode": "role_order"}))
			appendTranscriptEvent(t, sessionDir, metadata, runfix016SpeechEvent(metadata, "evt_speech_"+tc.name, "agent-1", 1, nil, map[string]any{"speech": "speech for " + tc.name}))
			appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_final_"+tc.name, "finalized", "council_finalized", tc.payload))
			if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
				t.Fatalf("write snapshot: %v", err)
			}
			manifest := runfix016BuildManifest(t, sessionDir, metadata)
			rows := runfix016SummaryRows(t, manifest)
			if len(rows) != 1 {
				t.Fatalf("summary_turn_accounting rows missing for %s: %#v", tc.name, rows)
			}
			assertRUNFIX016SummaryRow(t, rows[0], map[string]any{
				"turn": float64(1), "member": "agent-1", "speaker_selected_event_id": "evt_select_" + tc.name, "speech_event_id": "evt_speech_" + tc.name, "runner_invocation_id": "", "visible_message_id": "",
			})
		})
	}
}

func TestUnitARGUE004TranscriptRendersArgumentGraphSummaryWithoutRewritingSpeech(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "ARGUE-004 transcript projection"
	metadata.State.Phase = "discussion"
	metadata.Status = StatusOpen
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendArgumentGraphTranscriptEvents(t, sessionDir, metadata)

	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	text := string(out)
	for _, want := range []string{
		"Argument Graph Projection Summary",
		"evt_argue_open", "new_axis", "opening decision axis",
		"claim `T1.C1`: Traceability gates pilot acceptance.",
		"evt_argue_support", "support", "link `support` -> `evt_argue_open/T1.C1`",
		"evt_argue_challenge", "challenge", "link `challenge` -> `evt_argue_open/T1.C1`", "link `refine` -> `evt_argue_support/T2.C1`",
		"evt_argue_synthesize", "synthesize", "doc://evidence/synthesis",
		"quality_diagnostics", "omitted_graph_need_targets",
		"relation_audit", "checked_targets",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ARGUE-004 transcript missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, `"speech": "Original support speech text."`) {
		t.Fatalf("raw payload speech must remain unchanged in event JSON block:\n%s", text)
	}
	if strings.Contains(text, "Original support speech text. [") {
		t.Fatalf("transcript must not enrich payload.speech:\n%s", text)
	}
}

func TestUnitARGUE004ExportManifestDeclaresArgumentGraphProjection(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "ARGUE-004 export projection"
	metadata.State.Phase = "discussion"
	metadata.Status = StatusOpen
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendArgumentGraphTranscriptEvents(t, sessionDir, metadata)
	if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
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
		t.Fatalf("unmarshal bundle_manifest.json: %v\n%s", err, string(manifestBytes))
	}
	rows, ok := manifest["argument_graph_projection"].([]any)
	if !ok || len(rows) != 4 {
		t.Fatalf("argument_graph_projection rows missing: %#v\n%s", manifest["argument_graph_projection"], string(manifestBytes))
	}
	for _, want := range []string{"claims_json", "stance_links_json", "contribution_type", "new_axis_reason", "evidence_json", "quality_diagnostics_json", "relation_audit_json"} {
		if !strings.Contains(string(manifestBytes), want) {
			t.Fatalf("bundle manifest missing %q:\n%s", want, string(manifestBytes))
		}
	}
}

func TestUnitSURFD002TranscriptRequiresPriorFloorGrantForSpeech(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "SURFD-002 prior floor grant"
	metadata.State.Phase = "discussion"
	metadata.Status = StatusOpen
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_speech_before_grant",
		CommandID:     "cmd_evt_speech_before_grant",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "speech",
		From:          "agent-1",
		To:            []string{"agent-mod", "agent-2"},
		CreatedAt:     fixedTranscriptTime(),
		Payload:       map[string]any{"turn": float64(7), "speech": "This must not become renderable from a later floor grant."},
	})
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_select_late", "discussion", "speaker_selected", map[string]any{"turn": float64(7), "selection_mode": "role_order"}))

	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	if strings.Contains(string(out), "evt_speech_before_grant` | `speech` | `speech` | `renderable`") {
		t.Fatalf("speech before cursor-ordered floor grant must not render as accepted:\n%s", string(out))
	}
	if !strings.Contains(string(out), "evt_speech_before_grant` | `speech` | `speech` | `floor_grant_missing_or_mismatched`") {
		t.Fatalf("speech before floor grant must fail closed:\n%s", string(out))
	}
}

func TestUnitSURFD002DeliveryStatusFailsClosedForUnsupportedExplicitStatus(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "SURFD-002 unsupported status"
	metadata.State.Phase = "finalized"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_final_bad_status", "finalized", "council_finalized", map[string]any{
		"surface_evidence":        map[string]any{"status": "succeeded", "final_message_id": "msg-unsupported"},
		"linked_authority_result": map[string]any{"status": "complete", "kanban_comment_id": "comment-unsupported"},
	}))

	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	if strings.Contains(string(out), "| `evt_final_bad_status` | `council_finalized` | `visible_surface` | `succeeded` |") || strings.Contains(string(out), "| `evt_final_bad_status` | `council_finalized` | `linked_authority` | `complete` |") {
		t.Fatalf("unsupported explicit statuses must not project as success:\n%s", string(out))
	}
	if strings.Count(string(out), "missing/unproven") < 2 {
		t.Fatalf("unsupported explicit statuses must fail closed as missing/unproven:\n%s", string(out))
	}
}

func TestUnitSURFD002DeliveryStatusFailsClosedForProoflessExplicitStatus(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "SURFD-002 proofless status"
	metadata.State.Phase = "finalized"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_posted_no_proof", "finalized", "council_finalized", map[string]any{
		"surface_evidence":        map[string]any{"status": "posted"},
		"linked_authority_result": map[string]any{"status": "posted"},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_failed_no_proof", "unresolved", "council_unresolved", map[string]any{
		"surface_evidence":        map[string]any{"status": "failed"},
		"linked_authority_result": map[string]any{"status": "pending_followup"},
	}))

	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	for _, forbidden := range []string{
		"| `evt_posted_no_proof` | `council_finalized` | `visible_surface` | `posted` |",
		"| `evt_posted_no_proof` | `council_finalized` | `linked_authority` | `posted` |",
		"| `evt_failed_no_proof` | `council_unresolved` | `visible_surface` | `failed` |",
		"| `evt_failed_no_proof` | `council_unresolved` | `linked_authority` | `pending_followup` |",
	} {
		if strings.Contains(string(out), forbidden) {
			t.Fatalf("proofless explicit delivery status must fail closed, found %q:\n%s", forbidden, string(out))
		}
	}
	if strings.Count(string(out), "missing/unproven") < 4 {
		t.Fatalf("proofless explicit statuses must all fail closed as missing/unproven:\n%s", string(out))
	}
}

func TestUnitSURFD002DeliveryStatusFailsClosedForEmptyNonStringEvidencePointers(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "SURFD-002 empty evidence pointers"
	metadata.State.Phase = "finalized"
	metadata.Status = StatusTerminal
	metadata.Participants = []string{"agent-mod", "agent-1", "agent-2"}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_empty_non_string_proof", "finalized", "council_finalized", map[string]any{
		"surface_evidence": map[string]any{
			"status":           "posted",
			"final_message_id": []any{},
			"evidence":         map[string]any{},
		},
		"linked_authority_result": map[string]any{
			"status":              "pending_followup",
			"followup_card_id":    false,
			"vault_decision_note": []any{},
		},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, councilTranscriptEvent(metadata, "evt_empty_failure_proof", "unresolved", "council_unresolved", map[string]any{
		"surface_evidence": map[string]any{
			"status":         "failed",
			"failure_reason": map[string]any{},
		},
	}))

	out, err := RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript md: %v", err)
	}
	for _, forbidden := range []string{
		"| `evt_empty_non_string_proof` | `council_finalized` | `visible_surface` | `posted` |",
		"| `evt_empty_non_string_proof` | `council_finalized` | `linked_authority` | `pending_followup` |",
		"| `evt_empty_failure_proof` | `council_unresolved` | `visible_surface` | `failed` |",
	} {
		if strings.Contains(string(out), forbidden) {
			t.Fatalf("empty non-string evidence pointer must fail closed, found %q:\n%s", forbidden, string(out))
		}
	}
	if strings.Count(string(out), "missing/unproven") < 3 {
		t.Fatalf("empty non-string evidence pointers must fail closed as missing/unproven:\n%s", string(out))
	}
}

func TestUnitTranscriptRejectsUnsupportedFormatAndCorruptLog(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	if _, err := RenderTranscript(sessionDir, metadata, "html"); err == nil {
		t.Fatalf("unsupported transcript format must fail")
	}
	if err := os.WriteFile(filepath.Join(sessionDir, ChannelJSONLName), []byte("{bad}\n"), 0o600); err != nil {
		t.Fatalf("write corrupt log: %v", err)
	}
	if _, err := RenderTranscript(sessionDir, metadata, TranscriptJSONLFormat); err == nil {
		t.Fatalf("corrupt log must fail closed")
	}
}

func TestUnitExportBundleWritesDeterministicLocalFilesAndRejectsUnsafePath(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, testEvent(metadata, "evt_export_001"))
	if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	result, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle: %v", err)
	}
	for _, name := range []string{"brief.md", "bundle_manifest.json", "channel.jsonl", "registry_snapshot.yaml", "session.json", "transcript.jsonl", "transcript.md"} {
		if _, err := os.Stat(filepath.Join(result.BundleDir, name)); err != nil {
			t.Fatalf("missing bundle file %s: %v", name, err)
		}
	}
	resultAgain, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle again: %v", err)
	}
	if result.BundleDir != resultAgain.BundleDir || strings.Join(result.Files, ",") != strings.Join(resultAgain.Files, ",") {
		t.Fatalf("bundle output not deterministic: %#v vs %#v", result, resultAgain)
	}
	if _, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{OutputPath: "../escape"}); err == nil {
		t.Fatalf("unsafe output path must fail")
	}
}

func TestUnitExportBundleRejectsUnsafeOutputDirectories(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, testEvent(metadata, "evt_export_001"))

	if _, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{OutputPath: string(os.PathSeparator)}); err == nil {
		t.Fatalf("root output directory must fail")
	}

	regularFile := filepath.Join(t.TempDir(), "bundle-file")
	if err := os.WriteFile(regularFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write regular file output target: %v", err)
	}
	if _, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{OutputPath: regularFile}); err == nil {
		t.Fatalf("regular file output directory must fail")
	}

	symlinkTarget := t.TempDir()
	symlinkPath := filepath.Join(t.TempDir(), "bundle-link")
	if err := os.Symlink(symlinkTarget, symlinkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{OutputPath: symlinkPath}); err == nil {
		t.Fatalf("symlink output directory must fail")
	}
}

func TestUnitExportBundleRequiresRegistrySnapshot(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	appendTranscriptEvent(t, sessionDir, metadata, testEvent(metadata, "evt_export_001"))

	if _, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{}); err == nil {
		t.Fatalf("missing registry snapshot must fail")
	}
}

func appendArgumentGraphTranscriptEvents(t *testing.T, sessionDir string, metadata *SessionMetadata) {
	t.Helper()
	for _, event := range []EventEnvelope{
		councilTranscriptEvent(metadata, "evt_select_1", "discussion", "speaker_selected", map[string]any{"turn": float64(1), "selection_mode": "role_order"}),
		argumentGraphSpeechEvent(metadata, "evt_argue_open", "agent-1", 1, map[string]any{
			"speech":            "Original opening speech text.",
			"claims":            []any{map[string]any{"claim_id": "T1.C1", "summary": "Traceability gates pilot acceptance.", "kind": "requirement"}},
			"contribution_type": "new_axis",
			"new_axis_reason":   "opening decision axis",
			"evidence":          []any{"doc://evidence/opening"},
		}),
		councilTranscriptEvent(metadata, "evt_select_2", "discussion", "speaker_selected", map[string]any{"turn": float64(2), "selection_mode": "role_order"}),
		argumentGraphSpeechEvent(metadata, "evt_argue_support", "agent-2", 2, map[string]any{
			"speech":            "Original support speech text.",
			"claims":            []any{map[string]any{"claim_id": "T2.C1", "summary": "Export must keep relation fields.", "kind": "requirement"}},
			"stance_links":      []any{map[string]any{"target_event_id": "evt_argue_open", "target_claim_id": "T1.C1", "stance": "support", "rationale": "The target sets the same acceptance axis."}},
			"contribution_type": "support",
			"evidence":          []any{"doc://evidence/support"},
		}),
		councilTranscriptEvent(metadata, "evt_select_3", "discussion", "speaker_selected", map[string]any{"turn": float64(3), "selection_mode": "role_order"}),
		argumentGraphSpeechEvent(metadata, "evt_argue_challenge", "agent-1", 3, map[string]any{
			"speech": "Original challenge and refine speech text.",
			"claims": []any{map[string]any{"claim_id": "T3.C1", "summary": "Diagnostics must not become acceptance.", "kind": "risk"}},
			"stance_links": []any{
				map[string]any{"target_event_id": "evt_argue_open", "target_claim_id": "T1.C1", "stance": "challenge", "rationale": "Traceability alone is not enough without diagnostics."},
				map[string]any{"target_event_id": "evt_argue_support", "target_claim_id": "T2.C1", "stance": "refine", "rationale": "The export requirement needs malformed-row behavior."},
			},
			"contribution_type":   "challenge",
			"quality_diagnostics": []any{map[string]any{"code": "omitted_graph_need_targets", "severity": "warning", "missing_targets": []any{"T0.C1"}}},
			"relation_audit":      map[string]any{"checked_targets": []any{"evt_argue_open/T1.C1", "evt_argue_support/T2.C1"}},
		}),
		councilTranscriptEvent(metadata, "evt_select_4", "discussion", "speaker_selected", map[string]any{"turn": float64(4), "selection_mode": "role_order"}),
		argumentGraphSpeechEvent(metadata, "evt_argue_synthesize", "agent-2", 4, map[string]any{
			"speech": "Original synthesize speech text.",
			"claims": []any{map[string]any{"claim_id": "T4.C1", "summary": "Keep raw payload and additive projection.", "kind": "decision_frame"}},
			"stance_links": []any{
				map[string]any{"target_event_id": "evt_argue_open", "target_claim_id": "T1.C1", "stance": "synthesize", "rationale": "Combines traceability."},
				map[string]any{"target_event_id": "evt_argue_support", "target_claim_id": "T2.C1", "stance": "synthesize", "rationale": "Combines export preservation."},
			},
			"contribution_type": "synthesize",
			"evidence":          []any{"doc://evidence/synthesis"},
		}),
	} {
		appendTranscriptEvent(t, sessionDir, metadata, event)
	}
}

func argumentGraphSpeechEvent(metadata *SessionMetadata, id, speaker string, turn int, payload map[string]any) EventEnvelope {
	payload["turn"] = float64(turn)
	return EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       id,
		CommandID:     "cmd_" + id,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "speech",
		From:          speaker,
		To:            []string{"agent-mod", "agent-1", "agent-2"},
		CreatedAt:     fixedTranscriptTime().Add(time.Duration(turn) * time.Minute),
		Payload:       payload,
	}
}

func runfix016SpeechEvent(metadata *SessionMetadata, id, speaker string, turn int, runner *RunnerInfo, payload map[string]any) EventEnvelope {
	payload["turn"] = float64(turn)
	event := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       id,
		CommandID:     "cmd_" + id,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "speech",
		From:          speaker,
		To:            []string{"agent-mod", "agent-1", "agent-2"},
		CreatedAt:     fixedTranscriptTime().Add(time.Duration(turn) * time.Minute),
		Runner:        runner,
		Payload:       payload,
	}
	if runner != nil {
		event.Cost = json.RawMessage(`null`)
	}
	return event
}

func runfix016BuildManifest(t *testing.T, sessionDir string, metadata *SessionMetadata) map[string]any {
	t.Helper()
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
		t.Fatalf("unmarshal bundle_manifest.json: %v\n%s", err, string(manifestBytes))
	}
	return manifest
}

func runfix016SummaryRows(t *testing.T, manifest map[string]any) []map[string]any {
	t.Helper()
	rawRows, ok := manifest["summary_turn_accounting"].([]any)
	if !ok {
		t.Fatalf("summary_turn_accounting missing: %#v", manifest["summary_turn_accounting"])
	}
	rows := make([]map[string]any, 0, len(rawRows))
	for _, raw := range rawRows {
		row, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("summary_turn_accounting row has unexpected shape: %#v", raw)
		}
		rows = append(rows, row)
	}
	return rows
}

func assertRUNFIX016SummaryRow(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("summary_turn_accounting[%s] = %#v, want %#v; row=%#v", key, got[key], wantValue, got)
		}
	}
}

func appendTranscriptEvent(t *testing.T, sessionDir string, metadata *SessionMetadata, event EventEnvelope) {
	t.Helper()
	if _, err := AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent %s: %v", event.EventID, err)
	}
}

func councilTranscriptEvent(metadata *SessionMetadata, id, phase, typ string, payload map[string]any) EventEnvelope {
	return EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       id,
		CommandID:     "cmd_" + id,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         Phase(phase),
		Type:          typ,
		From:          "agent-mod",
		To:            []string{"agent-1", "agent-2"},
		CreatedAt:     fixedTranscriptTime(),
		Payload:       payload,
	}
}

func fixedTranscriptTime() time.Time {
	return time.Date(2026, 6, 8, 19, 2, 10, 0, time.UTC)
}
