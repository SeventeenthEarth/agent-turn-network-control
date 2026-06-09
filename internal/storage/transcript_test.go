package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
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
		From:          "kkachi-agent-networkd",
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
