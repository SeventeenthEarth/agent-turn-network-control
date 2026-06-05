package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/protocol"

	_ "modernc.org/sqlite"
)

func TestIntegrationDelegationLifecycleReviewCancelAndProjection(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "result.md"), []byte("done\n"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	agentOne := loaded.Registry.Members["agent-1"]
	agentOne.Workspace = workspace
	loaded.Registry.Members["agent-1"] = agentOne

	metadata, results, dedup, err := CreateDelegation(dataHome, loaded, DelegationStartSpec{
		Session: SessionSpec{
			ID:           "sess_deleg_lifecycle",
			SessionType:  SessionTypeDelegation,
			Title:        "DELEG lifecycle",
			Moderator:    "agent-mod",
			Participants: []string{"agent-mod", "agent-1"},
			EventID:      "evt_deleg_created",
			CommandID:    "cmd_deleg_new",
		},
		Assignee:          "agent-1",
		Task:              "Implement DELEG",
		AssignmentEventID: "evt_deleg_assigned",
		Now:               fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	if dedup || len(results) != 2 || results[0].Offset != 0 || results[1].Offset != 1 {
		t.Fatalf("expected two fresh ordered results, dedup=%v results=%#v", dedup, results)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if got := []string{index.Events[0].Type, index.Events[1].Type}; got[0] != "session_created" || got[1] != "task_assigned" {
		t.Fatalf("delegate new must append session_created then task_assigned, got %#v", got)
	}
	if index.Events[1].Phase != "assigned" {
		t.Fatalf("task_assigned must project assigned phase, got %q", index.Events[1].Phase)
	}

	appendDelegationForTest(t, sessionDir, metadata, DelegationEventSpec{Action: "ack", Actor: "agent-1", CommandID: "cmd_ack", Payload: map[string]any{"understanding": "ok"}, Now: fixedRuntime().Now().Add(time.Second)})
	appendDelegationForTest(t, sessionDir, metadata, DelegationEventSpec{Action: "update", Actor: "agent-1", CommandID: "cmd_update", Payload: map[string]any{"progress_status": "working", "summary": "working"}, Now: fixedRuntime().Now().Add(2 * time.Second)})
	submit := appendDelegationForTest(t, sessionDir, metadata, DelegationEventSpec{Action: "submit", Actor: "agent-1", CommandID: "cmd_submit", Payload: map[string]any{"summary": "done"}, ArtifactSourcePaths: []string{filepath.Join(workspace, "result.md")}, Now: fixedRuntime().Now().Add(3 * time.Second)})
	submitted := eventByIDForTest(t, sessionDir, metadata, submit.EventID)
	artifactPayload := anySlice(submitted.Payload, "artifacts")
	if len(artifactPayload) != 1 {
		t.Fatalf("expected one ingested artifact ref, got %#v", submitted.Payload)
	}
	artifact := artifactPayload[0].(map[string]any)
	if artifact["source_path"] != nil || strings.Contains(mustJSON(t, submitted.Payload), workspace) {
		t.Fatalf("work_submitted persisted raw source path: %s", mustJSON(t, submitted.Payload))
	}
	for _, key := range []string{"artifact_id", "stored_path", "sha256", "size_bytes", "mime"} {
		if artifact[key] == nil || artifact[key] == "" {
			t.Fatalf("artifact missing %s: %#v", key, artifact)
		}
	}

	appendDelegationForTest(t, sessionDir, metadata, DelegationEventSpec{Action: "review", Actor: "agent-mod", Recipients: []string{"agent-1"}, CommandID: "cmd_review", Payload: map[string]any{"review_focus": []string{"risk"}}, Now: fixedRuntime().Now().Add(4 * time.Second)})
	appendDelegationForTest(t, sessionDir, metadata, DelegationEventSpec{Action: "review-submit", Actor: "agent-1", CommandID: "cmd_review_submit", Payload: map[string]any{"verdict": "changes_requested", "findings": []map[string]any{{"severity": "high", "issue": "gap", "required_change": "fix"}}}, Now: fixedRuntime().Now().Add(5 * time.Second)})
	appendDelegationForTest(t, sessionDir, metadata, DelegationEventSpec{Action: "revise", Actor: "agent-mod", Recipients: []string{"agent-1"}, CommandID: "cmd_revise", Payload: map[string]any{"required_changes": []string{"fix"}, "source_review_event_id": "evt_cmd_review_submit_review_submitted"}, Now: fixedRuntime().Now().Add(6 * time.Second)})

	cancel, cancelDedup, err := CancelSession(sessionDir, metadata, SessionCancelSpec{Actor: "agent-mod", Reason: "user cancelled", CommandID: "cmd_cancel", Now: fixedRuntime().Now().Add(7 * time.Second)})
	if err != nil {
		t.Fatalf("CancelSession: %v", err)
	}
	if cancelDedup || cancel.EventID == "" {
		t.Fatalf("unexpected cancel result dedup=%v result=%#v", cancelDedup, cancel)
	}
	if _, _, err := RecordDelegationEvent(sessionDir, metadata, DelegationEventSpec{Action: "accept", Actor: "agent-mod", CommandID: "cmd_accept_after_cancel", Payload: map[string]any{"final_summary": "done"}, Now: fixedRuntime().Now().Add(8 * time.Second)}); err == nil {
		t.Fatalf("accept after cancel must fail closed")
	}
	if _, _, err := CancelSession(sessionDir, metadata, SessionCancelSpec{Actor: "agent-mod", Reason: "again", CommandID: "cmd_cancel_again", Now: fixedRuntime().Now().Add(9 * time.Second)}); err == nil {
		t.Fatalf("double cancel with new command id must fail closed")
	}
	replayed, replayDedup, err := CancelSession(sessionDir, metadata, SessionCancelSpec{Actor: "agent-mod", Reason: "user cancelled", CommandID: "cmd_cancel", Now: fixedRuntime().Now().Add(10 * time.Second)})
	if err != nil || !replayDedup || replayed.EventID != cancel.EventID {
		t.Fatalf("same cancel command replay should deduplicate, result=%#v dedup=%v err=%v", replayed, replayDedup, err)
	}
	if _, _, err := CancelSession(sessionDir, metadata, SessionCancelSpec{Actor: "agent-mod", Reason: "different", CommandID: "cmd_cancel", Now: fixedRuntime().Now().Add(11 * time.Second)}); err == nil {
		t.Fatalf("same command id with different cancel payload must conflict")
	}

	report, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection: %v", err)
	}
	db := openProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	if got := scalarText(t, db, `SELECT phase FROM sessions WHERE id = 'sess_deleg_lifecycle'`); got != "cancelled" {
		t.Fatalf("expected projected phase cancelled, got %q", got)
	}
	if got := scalarText(t, db, `SELECT status FROM sessions WHERE id = 'sess_deleg_lifecycle'`); got != "terminal" {
		t.Fatalf("expected projected status terminal, got %q", got)
	}
	if got := scalarText(t, db, `SELECT closed_at FROM sessions WHERE id = 'sess_deleg_lifecycle'`); got == "" {
		t.Fatalf("expected closed_at after cancellation")
	}
	if got := rowCount(t, db, "delegation_reviews"); got != 1 {
		t.Fatalf("expected one review projection row, got %d", got)
	}
	if active, err := FindActiveSession(dataHome, fixedRuntime()); err != nil || active != nil {
		t.Fatalf("cancelled delegation must release active-session lock, active=%#v err=%v", active, err)
	}
}

func TestIntegrationDelegationBlockResumeLimitsAndCancelFailClosed(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	metadata, _, _, err := CreateDelegation(dataHome, loaded, DelegationStartSpec{
		Session:  SessionSpec{ID: "sess_deleg_block", SessionType: SessionTypeDelegation, Title: "DELEG block", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_block_created", CommandID: "cmd_block_new"},
		Assignee: "agent-1", Task: "block test", AssignmentEventID: "evt_block_assigned", Now: fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendDelegationForTest(t, sessionDir, metadata, DelegationEventSpec{Action: "ack", Actor: "agent-1", CommandID: "cmd_block_ack", Payload: map[string]any{"understanding": "ok"}, Now: fixedRuntime().Now().Add(time.Second)})

	blocked, _, err := BlockSession(sessionDir, metadata, SessionBlockSpec{Actor: "agent-mod", Category: "external_dependency", Reason: "dependency down", CommandID: "cmd_block", Now: fixedRuntime().Now().Add(2 * time.Second)})
	if err != nil {
		t.Fatalf("BlockSession: %v", err)
	}
	if _, _, err := ResumeSession(sessionDir, metadata, SessionResumeSpec{Actor: "agent-mod", BlockedEventID: blocked.EventID, Reason: "fixed", CommandID: "cmd_resume", Now: fixedRuntime().Now().Add(3 * time.Second)}); err != nil {
		t.Fatalf("ResumeSession manual block: %v", err)
	}

	budget := appendRawEventForTest(t, sessionDir, metadata, "evt_budget_block", "cmd_budget", "session_budget_exceeded", "blocked", "kkachi-agent-networkd", []string{"agent-mod"}, map[string]any{"limit_kind": "max_runner_calls", "observed": 1, "limit": 1, "prior_phase": "working", "resume_phase": "working", "action": "session_blocked"}, 4*time.Second)
	if _, _, err := ResumeSession(sessionDir, metadata, SessionResumeSpec{Actor: "agent-mod", BlockedEventID: budget.EventID, Reason: "try manual", CommandID: "cmd_bad_resume", Now: fixedRuntime().Now().Add(5 * time.Second)}); err == nil {
		t.Fatalf("manual resume of budget block must fail closed")
	}
	if _, _, err := ExtendLimits(sessionDir, metadata, LimitsExtendSpec{Actor: "agent-mod", AuthorizedBy: "user", BlockedEventID: budget.EventID, Changes: map[string]any{"max_runner_calls": 2}, CommandID: "cmd_limits_extend", Now: fixedRuntime().Now().Add(6 * time.Second)}); err != nil {
		t.Fatalf("ExtendLimits budget block: %v", err)
	}
	report, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection after limits extend: %v", err)
	}
	db := openProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	if got := scalarText(t, db, `SELECT phase FROM sessions WHERE id = 'sess_deleg_block'`); got != "working" {
		t.Fatalf("expected limits-extended phase working, got %q", got)
	}
	if got := scalarText(t, db, `SELECT status FROM sessions WHERE id = 'sess_deleg_block'`); got != "open" {
		t.Fatalf("expected limits-extended status open, got %q", got)
	}
	assertProjectionNull(t, db, "prior_phase", "sess_deleg_block")
	assertProjectionNull(t, db, "resume_phase", "sess_deleg_block")
	assertProjectionNull(t, db, "blocked_by_event_id", "sess_deleg_block")
}

func TestIntegrationDelegationRejectsUnsafeArtifactAndMalformedReview(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	metadata, _, _, err := CreateDelegation(dataHome, loaded, DelegationStartSpec{
		Session:  SessionSpec{ID: "sess_deleg_failclosed", SessionType: SessionTypeDelegation, Title: "DELEG fail closed", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_fail_created", CommandID: "cmd_fail_new"},
		Assignee: "agent-1", Task: "fail closed", AssignmentEventID: "evt_fail_assigned", Now: fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendDelegationForTest(t, sessionDir, metadata, DelegationEventSpec{Action: "ack", Actor: "agent-1", CommandID: "cmd_fail_ack", Payload: map[string]any{"understanding": "ok"}, Now: fixedRuntime().Now().Add(time.Second)})
	before := eventCountForTest(t, sessionDir, metadata)
	if _, _, err := RecordDelegationEvent(sessionDir, metadata, DelegationEventSpec{Action: "submit", Actor: "agent-1", CommandID: "cmd_bad_artifact", Payload: map[string]any{"summary": "bad"}, ArtifactSourcePaths: []string{filepath.Join(t.TempDir(), "missing.md")}, Now: fixedRuntime().Now().Add(2 * time.Second)}); err == nil {
		t.Fatalf("unsafe/missing artifact must fail closed")
	}
	if after := eventCountForTest(t, sessionDir, metadata); after != before {
		t.Fatalf("failed artifact submit appended event: before=%d after=%d", before, after)
	}
	appendRawEventForTest(t, sessionDir, metadata, "evt_fail_submitted", "cmd_fail_submit_ok", "work_submitted", "submitted", "agent-1", []string{"agent-mod"}, map[string]any{"summary": "ok", "artifacts": []map[string]any{}}, 3*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_fail_review", "cmd_fail_review", "review_requested", "under_review", "agent-mod", []string{"agent-1"}, map[string]any{"review_focus": []string{"risk"}}, 4*time.Second)
	before = eventCountForTest(t, sessionDir, metadata)
	if _, _, err := RecordDelegationEvent(sessionDir, metadata, DelegationEventSpec{Action: "review-submit", Actor: "agent-1", CommandID: "cmd_bad_findings", Payload: map[string]any{"verdict": "changes_requested", "findings": []map[string]any{{"severity": "extreme", "issue": "bad"}}}, Now: fixedRuntime().Now().Add(5 * time.Second)}); err == nil {
		t.Fatalf("malformed review findings must fail closed")
	}
	if after := eventCountForTest(t, sessionDir, metadata); after != before {
		t.Fatalf("failed review submit appended event: before=%d after=%d", before, after)
	}
}

func appendDelegationForTest(t *testing.T, sessionDir string, metadata *SessionMetadata, spec DelegationEventSpec) AppendResult {
	t.Helper()
	result, _, err := RecordDelegationEvent(sessionDir, metadata, spec)
	if err != nil {
		t.Fatalf("RecordDelegationEvent(%s): %v", spec.Action, err)
	}
	return result
}

func appendRawEventForTest(t *testing.T, sessionDir string, metadata *SessionMetadata, eventID, commandID, typ string, phase Phase, from string, to []string, payload map[string]any, delta time.Duration) EventEnvelope {
	t.Helper()
	event := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventID,
		CommandID:     commandID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         phase,
		Type:          typ,
		From:          from,
		To:            to,
		CreatedAt:     fixedRuntime().Now().Add(delta),
		Payload:       payload,
	}
	if _, err := AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent(%s): %v", typ, err)
	}
	return event
}

func eventByIDForTest(t *testing.T, sessionDir string, metadata *SessionMetadata, eventID string) EventEnvelope {
	t.Helper()
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	for _, event := range index.Events {
		if event.EventID == eventID {
			return event
		}
	}
	t.Fatalf("missing event %s", eventID)
	return EventEnvelope{}
}

func eventCountForTest(t *testing.T, sessionDir string, metadata *SessionMetadata) int {
	t.Helper()
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	return len(index.Events)
}

func assertProjectionNull(t *testing.T, db *sql.DB, column, sessionID string) {
	t.Helper()
	var value any
	if err := db.QueryRow("SELECT "+column+" FROM sessions WHERE id = ?", sessionID).Scan(&value); err != nil {
		t.Fatalf("query %s for %s: %v", column, sessionID, err)
	}
	if value != nil {
		t.Fatalf("expected %s for %s to be NULL, got %#v", column, sessionID, value)
	}
}
