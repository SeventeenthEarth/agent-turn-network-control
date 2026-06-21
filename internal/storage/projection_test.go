package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hun-control/internal/protocol"

	_ "modernc.org/sqlite"
)

func TestIntegrationProjectionCreatesFullSchemaV1Tables(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	if _, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime()); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	report, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection failed: %v", err)
	}
	if report.SchemaVersion != ProjectionSchemaV1 || report.SessionCount != 1 || report.EventCount != 1 {
		t.Fatalf("unexpected projection report: %#v", report)
	}

	db := openProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	for _, table := range projectionTableOrder {
		if count := tableExists(t, db, table); count != 1 {
			t.Fatalf("expected table %s to exist, got count %d", table, count)
		}
	}
	if got := metadataText(t, db, "schema_version"); got != "1" {
		t.Fatalf("expected schema_version metadata 1, got %q", got)
	}
	if got := metadataText(t, db, "source_event_count"); got != "1" {
		t.Fatalf("expected source_event_count metadata 1, got %q", got)
	}
	if got := rowCount(t, db, "event_recipients"); got != 2 {
		t.Fatalf("expected event recipient rows, got %d", got)
	}
}

func TestIntegrationProjectionReplaysEventSpecificTables(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	spec := testSessionSpec()
	spec.ID = "sess_projection"
	spec.SessionType = SessionTypeCouncil
	spec.EventID = "evt_projection_created"
	spec.CommandID = "cmd_projection_create"
	metadata, _, err := CreateSession(dataHome, loaded, spec, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendProjectionEvents(t, sessionDir, metadata)

	report, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection failed: %v", err)
	}
	if report.EventCount != 15 {
		t.Fatalf("expected 15 source events, got %#v", report)
	}

	db := openProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	for table, want := range map[string]int{
		"runner_invocations":            1,
		"escalation_batches":            1,
		"escalation_batch_items":        1,
		"council_attendance_projection": 1,
		"council_agenda_locks":          1,
		"council_hand_raises":           1,
		"council_votes":                 1,
		"linked_authority_results":      1,
		"stream_cursors":                1,
		"stream_subscribers":            1,
		"delegation_reviews":            1,
		"artifacts":                     1,
		"commands_seen":                 2,
	} {
		if got := rowCount(t, db, table); got != want {
			t.Fatalf("expected %s rows %d, got %d", table, want, got)
		}
	}
	if status := scalarText(t, db, `SELECT status FROM runner_invocations WHERE invocation_id = 'run_001'`); status != "succeeded" {
		t.Fatalf("expected runner succeeded, got %q", status)
	}
	if got := scalarText(t, db, `SELECT status FROM escalation_batches WHERE batch_id = 'batch_001'`); got != "flushed" {
		t.Fatalf("expected flushed batch, got %q", got)
	}
	if got := scalarText(t, db, `SELECT status FROM linked_authority_results WHERE session_id = 'sess_projection'`); got != "posted" {
		t.Fatalf("expected linked authority status posted, got %q", got)
	}
}

func TestIntegrationARGUE004ProjectionPersistsArgumentGraphRowsAndDiagnostics(t *testing.T) {
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        "sess_argue004_projection",
			Title:     "ARGUE-004 projection",
			Moderator: "agent-mod",
			TurnMode:  "role_order",
			EventID:   "evt_argue004_created",
			CommandID: "cmd_argue004_created",
		},
		Members: []string{"agent-1", "agent-2"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendArgumentGraphTranscriptEvents(t, sessionDir, metadata)
	appendTranscriptEvent(t, sessionDir, metadata, argumentGraphSpeechEvent(metadata, "evt_argue_malformed", "agent-1", 5, map[string]any{
		"speech":              "Malformed legacy payload preserved for projection diagnostics.",
		"claims":              "not-an-array",
		"stance_links":        map[string]any{"target_event_id": "evt_argue_open"},
		"contribution_type":   17,
		"evidence":            "not-an-array",
		"quality_diagnostics": "not-an-array",
	}))
	appendTranscriptEvent(t, sessionDir, metadata, argumentGraphSpeechEvent(metadata, "evt_argue_malformed_elements", "agent-2", 6, map[string]any{
		"speech":              "Malformed array elements must not project relation JSON.",
		"claims":              []any{"bad"},
		"stance_links":        []any{"bad"},
		"evidence":            []any{"doc://evidence/scalar-safe", map[string]any{"uri": "doc://evidence/object-safe", "label": "object evidence"}},
		"quality_diagnostics": []any{"bad"},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, argumentGraphSpeechEvent(metadata, "evt_argue_partial_malformed_objects", "agent-1", 7, map[string]any{
		"speech": "Partially malformed object arrays must fail closed.",
		"claims": []any{
			map[string]any{"claim_id": "T7.C1", "summary": "Valid claim entry."},
			map[string]any{"kind": "risk"},
		},
		"stance_links": []any{
			map[string]any{"target_event_id": "evt_argue_open", "stance": "support"},
			map[string]any{"rationale": "Missing projection-compatible target or stance key."},
		},
		"quality_diagnostics": []any{
			map[string]any{"code": "valid_diagnostic"},
			map[string]any{"missing_targets": []any{"T0.C1"}},
		},
	}))
	appendTranscriptEvent(t, sessionDir, metadata, argumentGraphSpeechEvent(metadata, "evt_argue_missing", "agent-2", 8, map[string]any{
		"speech": "Legacy speech without ARGUE fields.",
	}))

	report, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection failed: %v", err)
	}
	db := openProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	if got := rowCount(t, db, "council_argument_graph_projection"); got != 8 {
		t.Fatalf("expected 8 argument graph rows, got %d", got)
	}
	if got := scalarText(t, db, `SELECT contribution_type FROM council_argument_graph_projection WHERE event_id = 'evt_argue_synthesize'`); got != "synthesize" {
		t.Fatalf("expected synthesize contribution, got %q", got)
	}
	if got := scalarText(t, db, `SELECT status FROM council_argument_graph_projection WHERE event_id = 'evt_argue_synthesize'`); got != "projected" {
		t.Fatalf("expected projected synthesize row, got %q", got)
	}
	if got := scalarText(t, db, `SELECT stance_links_json FROM council_argument_graph_projection WHERE event_id = 'evt_argue_challenge'`); !strings.Contains(got, `"stance":"challenge"`) || !strings.Contains(got, `"stance":"refine"`) {
		t.Fatalf("expected challenge/refine stance links, got %s", got)
	}
	if got := scalarText(t, db, `SELECT quality_diagnostics_json FROM council_argument_graph_projection WHERE event_id = 'evt_argue_challenge'`); !strings.Contains(got, "omitted_graph_need_targets") {
		t.Fatalf("expected quality diagnostics, got %s", got)
	}
	if got := scalarText(t, db, `SELECT relation_audit_json FROM council_argument_graph_projection WHERE event_id = 'evt_argue_challenge'`); !strings.Contains(got, "checked_targets") {
		t.Fatalf("expected relation audit, got %s", got)
	}
	if got := scalarText(t, db, `SELECT diagnostic FROM council_argument_graph_projection WHERE event_id = 'evt_argue_malformed'`); !strings.Contains(got, "malformed_claims") || !strings.Contains(got, "malformed_stance_links") || !strings.Contains(got, "malformed_quality_diagnostics") {
		t.Fatalf("expected malformed diagnostics, got %q", got)
	}
	if got := scalarText(t, db, `SELECT status FROM council_argument_graph_projection WHERE event_id = 'evt_argue_malformed_elements'`); got != "diagnostic" {
		t.Fatalf("expected malformed element row diagnostic status, got %q", got)
	}
	if got := scalarText(t, db, `SELECT diagnostic FROM council_argument_graph_projection WHERE event_id = 'evt_argue_malformed_elements'`); !strings.Contains(got, "malformed_claims") || !strings.Contains(got, "malformed_stance_links") || !strings.Contains(got, "malformed_quality_diagnostics") || strings.Contains(got, "malformed_evidence") {
		t.Fatalf("expected malformed element relation diagnostics with safe evidence accepted, got %q", got)
	}
	if got := scalarText(t, db, `SELECT claims_json FROM council_argument_graph_projection WHERE event_id = 'evt_argue_malformed_elements'`); got != "null" {
		t.Fatalf("malformed scalar claims must not be preserved in projection JSON, got %s", got)
	}
	if got := scalarText(t, db, `SELECT stance_links_json FROM council_argument_graph_projection WHERE event_id = 'evt_argue_malformed_elements'`); got != "null" {
		t.Fatalf("malformed scalar stance links must not be preserved in projection JSON, got %s", got)
	}
	if got := scalarText(t, db, `SELECT quality_diagnostics_json FROM council_argument_graph_projection WHERE event_id = 'evt_argue_malformed_elements'`); got != "null" {
		t.Fatalf("malformed scalar quality diagnostics must not be preserved in projection JSON, got %s", got)
	}
	if got := scalarText(t, db, `SELECT evidence_json FROM council_argument_graph_projection WHERE event_id = 'evt_argue_malformed_elements'`); !strings.Contains(got, `"doc://evidence/scalar-safe"`) || !strings.Contains(got, `"uri":"doc://evidence/object-safe"`) {
		t.Fatalf("safe string/object evidence entries should be preserved, got %s", got)
	}
	if got := scalarText(t, db, `SELECT status FROM council_argument_graph_projection WHERE event_id = 'evt_argue_partial_malformed_objects'`); got != "diagnostic" {
		t.Fatalf("expected partial malformed object row diagnostic status, got %q", got)
	}
	if got := scalarText(t, db, `SELECT diagnostic FROM council_argument_graph_projection WHERE event_id = 'evt_argue_partial_malformed_objects'`); !strings.Contains(got, "malformed_claims") || !strings.Contains(got, "malformed_stance_links") || !strings.Contains(got, "malformed_quality_diagnostics") {
		t.Fatalf("expected partial malformed object diagnostics, got %q", got)
	}
	if got := scalarText(t, db, `SELECT diagnostic FROM council_argument_graph_projection WHERE event_id = 'evt_argue_missing'`); got != "missing_argument_graph_fields" {
		t.Fatalf("expected missing diagnostic, got %q", got)
	}
}

func TestIntegrationProjectionRunnerAccountingUsesStartedNotCostSource(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	spec := testSessionSpec()
	spec.ID = "sess_runner_accounting"
	spec.EventID = "evt_accounting_created"
	metadata, _, err := CreateSession(dataHome, loaded, spec, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	duration := 1.25
	events := []EventEnvelope{
		{
			SchemaVersion: protocol.SchemaVersion, EventID: "evt_run_a_started", CommandID: "cmd_run_a_started", CorrelationID: metadata.ID,
			SessionID: metadata.ID, SessionType: metadata.SessionType, Phase: "working", Type: "runner_invocation_started", From: "agent-1", To: []string{"agent-mod"}, CreatedAt: fixedRuntime().Now(),
			Runner: &RunnerInfo{InvocationID: "run_a", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, SourceCommandID: "cmd_source_a", Status: "started"}, Payload: map[string]any{},
		},
		{
			SchemaVersion: protocol.SchemaVersion, EventID: "evt_run_a_done", CommandID: "cmd_run_a_done", CorrelationID: metadata.ID,
			SessionID: metadata.ID, SessionType: metadata.SessionType, Phase: "working", Type: "assignee_update", From: "agent-1", To: []string{"agent-mod"}, CreatedAt: fixedRuntime().Now().Add(time.Second),
			Runner: &RunnerInfo{InvocationID: "run_a", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, SourceCommandID: "cmd_source_a", Status: "succeeded", DurationSec: &duration},
			Cost:   rawJSON(t, map[string]any{"tokens_in": 5, "tokens_out": 8, "usd_estimate": 0.02, "source": "hermes-agent-stderr-parse"}), Payload: map[string]any{"message": "done"},
		},
		{
			SchemaVersion: protocol.SchemaVersion, EventID: "evt_run_b_started", CommandID: "cmd_run_b_started", CorrelationID: metadata.ID,
			SessionID: metadata.ID, SessionType: metadata.SessionType, Phase: "working", Type: "runner_invocation_started", From: "agent-1", To: []string{"agent-mod"}, CreatedAt: fixedRuntime().Now().Add(2 * time.Second),
			Runner: &RunnerInfo{InvocationID: "run_b", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, SourceCommandID: "cmd_source_b", Status: "started"}, Payload: map[string]any{},
		},
		{
			SchemaVersion: protocol.SchemaVersion, EventID: "evt_run_b_failed", CommandID: "cmd_run_b_failed", CorrelationID: metadata.ID,
			SessionID: metadata.ID, SessionType: metadata.SessionType, Phase: "working", Type: "runner_invocation_failed", From: "agent-1", To: []string{"agent-mod"}, CreatedAt: fixedRuntime().Now().Add(3 * time.Second),
			Runner: &RunnerInfo{InvocationID: "run_b", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, SourceCommandID: "cmd_source_b", Status: "failed", DurationSec: &duration},
			Cost:   rawJSON(t, nil), Payload: map[string]any{"error_class": "nonzero_exit"},
		},
	}
	for _, event := range events {
		if _, err := AppendEvent(sessionDir, metadata, event); err != nil {
			t.Fatalf("append %s: %v", event.EventID, err)
		}
	}
	report, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection failed: %v", err)
	}
	db := openProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	if got := scalarInt(t, db, `SELECT runner_calls_total FROM sessions WHERE id = 'sess_runner_accounting'`); got != 2 {
		t.Fatalf("runner_calls_total should count started events, got %d", got)
	}
	if got := scalarInt(t, db, `SELECT missing_cost_runner_calls_total FROM sessions WHERE id = 'sess_runner_accounting'`); got != 1 {
		t.Fatalf("missing cost count should count terminal null cost, got %d", got)
	}
	if got := scalarInt(t, db, `SELECT tokens_in_total FROM sessions WHERE id = 'sess_runner_accounting'`); got != 5 {
		t.Fatalf("tokens total should come from cost object only, got %d", got)
	}
}

func TestIntegrationVerifyStorageDetectsMissingAndMismatchProjection(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	if _, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime()); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	report, err := VerifyStorage(dataHome, VerifyOptions{Runtime: fixedRuntime()})
	if err == nil {
		t.Fatalf("expected missing projection verification failure")
	}
	if report == nil || report.Status != VerifyStatusMissing || !report.RecoverableProjectionOnly {
		t.Fatalf("unexpected missing projection report: %#v err=%v", report, err)
	}

	rebuild, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection failed: %v", err)
	}
	db := openProjectionDB(t, rebuild.DBPath)
	if _, err := db.Exec(`UPDATE projection_metadata SET value = 'sha256:bad' WHERE key = 'source_hash'`); err != nil {
		_ = db.Close()
		t.Fatalf("mutate metadata: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close projection: %v", err)
	}

	report, err = VerifyStorage(dataHome, VerifyOptions{Runtime: fixedRuntime()})
	if err == nil {
		t.Fatalf("expected mismatch verification failure")
	}
	if report == nil || report.Status != VerifyStatusMismatch || !report.RecoverableProjectionOnly {
		t.Fatalf("unexpected mismatch report: %#v err=%v", report, err)
	}
}

func TestIntegrationVerifyStorageDetectsCorruptProjectionBytes(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	if _, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime()); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataHome, ProjectionDBName), []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt projection: %v", err)
	}

	report, err := VerifyStorage(dataHome, VerifyOptions{Runtime: fixedRuntime()})
	if err == nil {
		t.Fatalf("expected corrupt projection verification failure")
	}
	if report == nil || report.Status != VerifyStatusCorrupt || !report.RecoverableProjectionOnly {
		t.Fatalf("unexpected corrupt projection report: %#v err=%v", report, err)
	}
}

func TestIntegrationProjectionRebuildIsIdempotent(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	if _, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime()); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	first, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("first RebuildProjection failed: %v", err)
	}
	second, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("second RebuildProjection failed: %v", err)
	}

	if first.SourceHash == "" {
		t.Fatalf("expected non-empty source hash")
	}
	if first.SourceHash != second.SourceHash {
		t.Fatalf("source hash changed across rebuilds: first=%q second=%q", first.SourceHash, second.SourceHash)
	}
	if first.SessionCount != second.SessionCount || first.EventCount != second.EventCount {
		t.Fatalf("source counts changed across rebuilds: first=%#v second=%#v", first, second)
	}
}

func TestIntegrationProjectionRejectsUnsafeExistingSQLite(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	if _, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime()); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	target := filepath.Join(t.TempDir(), "target.sqlite")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dataHome, ProjectionDBName)); err != nil {
		t.Fatalf("symlink projection: %v", err)
	}

	_, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	assertStorageIssue(t, err, CategoryProjectionUnsafe)
}

func appendProjectionEvents(t *testing.T, sessionDir string, metadata *SessionMetadata) {
	t.Helper()
	at := time.Date(2026, 6, 4, 12, 1, 0, 0, time.UTC)
	commandID := "cmd_projection_flow"
	duration := 2.5
	events := []EventEnvelope{
		{
			EventID: "evt_runner_started", Phase: "discussion", Type: "runner_invocation_started", From: "agent-1", To: []string{"agent-mod"}, Runner: &RunnerInfo{
				InvocationID: "run_001", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, SourceCommandID: commandID, Status: "started",
			}, Payload: map[string]any{"invocation_id": "run_001"},
		},
		{
			EventID: "evt_runner_done", Phase: "discussion", Type: "runner_result_submitted", From: "agent-1", To: []string{"agent-mod"}, Runner: &RunnerInfo{
				InvocationID: "run_001", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, SourceCommandID: commandID, Status: "succeeded", DurationSec: &duration,
			}, Cost: rawJSON(t, map[string]any{"tokens_in": 10, "tokens_out": 20, "usd_estimate": 0.03, "source": "fake"}), Payload: map[string]any{"summary": "done"},
		},
		{EventID: "evt_batch", Phase: "blocked", Type: "escalation_batched", From: "agent-1", To: []string{"agent-mod"}, Payload: map[string]any{"batch_id": "batch_001", "source_event_id": "evt_runner_done", "source_member": "agent-1", "question_hash": "sha256:q", "urgency": "normal", "batch_window_sec": 30, "batch_deadline_at": "2026-06-04T12:02:00Z", "prior_phase": "discussion", "resume_phase": "discussion"}},
		{EventID: "evt_user", Phase: "blocked", Type: "user_escalation_requested", From: "agent-mod", To: []string{"user"}, Payload: map[string]any{"batch_id": "batch_001", "batched": true}},
		{EventID: "evt_attendance_requested", Phase: "preparation", Type: "attendance_requested", From: "agent-mod", To: []string{"agent-1"}, Payload: map[string]any{"required_members": []string{"agent-1"}}},
		{EventID: "evt_attended", Phase: "preparation", Type: "member_attended", From: "agent-1", To: []string{"agent-mod"}, Payload: map[string]any{"attendance_status": "attended", "attendance_summary": "present", "surface_evidence": map[string]any{"message_id": "m1"}}},
		{EventID: "evt_agenda", Phase: "discussion", Type: "agenda_locked", From: "agent-mod", To: []string{"agent-1"}, Payload: map[string]any{"decision_question": "Ship?", "constraints": map[string]any{"scope": "local"}, "surface_evidence": map[string]any{"message_id": "m2"}}},
		{EventID: "evt_hand", Phase: "discussion", Type: "hand_raise", From: "agent-1", To: []string{"agent-mod"}, Payload: map[string]any{"turn": 1, "wants_to_speak": true, "intent": "support", "relevance": 5, "urgency": 3}},
		{EventID: "evt_vote", Phase: "consensus_vote", Type: "consensus_vote", From: "agent-1", To: []string{"agent-mod"}, Payload: map[string]any{"consensus_round": 1, "draft_version": 1, "vote": "approve", "reason": "ready"}},
		{EventID: "evt_cursor", Phase: "discussion", Type: "stream_cursor_acknowledged", From: "agent-1", To: []string{"agent-mod"}, Payload: map[string]any{"cursor": "cur_1", "event_id": "evt_vote"}},
		{EventID: "evt_subscriber", Phase: "discussion", Type: "stream_subscriber_connected", From: "agent-1", To: []string{"agent-mod"}, Payload: map[string]any{"subscriber_id": "sub_1", "cursor": "cur_1"}},
		{EventID: "evt_review", Phase: "draft_conclusion", Type: "review_submitted", From: "agent-1", To: []string{"agent-mod"}, Payload: map[string]any{"review_round": 1, "verdict": "approve", "findings": []any{map[string]any{"summary": "ok"}}}},
		{EventID: "evt_artifact", Phase: "draft_conclusion", Type: "work_accepted", From: "agent-mod", To: []string{"agent-1"}, Payload: map[string]any{"accepted_artifacts": []any{map[string]any{"artifact_id": "art_1", "source_path": "/tmp/source", "stored_path": "artifacts/art_1", "size_bytes": 123, "sha256": "sha256:art", "mime": "text/plain"}}}},
		{EventID: "evt_final", Phase: "finalized", Type: "council_finalized", From: "agent-mod", To: []string{"agent-1"}, Payload: map[string]any{"linked_authority_result": map[string]any{"status": "posted", "kanban_card_id": "t_xxxxx", "evidence": map[string]any{"comment_id": "c1"}}}},
	}
	for i, event := range events {
		event.SchemaVersion = protocol.SchemaVersion
		event.CommandID = commandID
		event.CorrelationID = metadata.ID
		event.SessionID = metadata.ID
		event.SessionType = metadata.SessionType
		event.CreatedAt = at.Add(time.Duration(i) * time.Second)
		if _, err := AppendEvent(sessionDir, metadata, event); err != nil {
			t.Fatalf("append %s: %v", event.EventID, err)
		}
	}
}

func openProjectionDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open projection: %v", err)
	}
	return db
}

func tableExists(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("table exists %s: %v", table, err)
	}
	return count
}

func rowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func metadataText(t *testing.T, db *sql.DB, key string) string {
	t.Helper()
	var value string
	if err := db.QueryRow(`SELECT value FROM projection_metadata WHERE key = ?`, key).Scan(&value); err != nil {
		t.Fatalf("metadata %s: %v", key, err)
	}
	return value
}

func scalarInt(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var value int
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("query scalar %q: %v", query, err)
	}
	return value
}

func scalarText(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	var value string
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("query scalar %q: %v", query, err)
	}
	return value
}

func rawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw json: %v", err)
	}
	return content
}
