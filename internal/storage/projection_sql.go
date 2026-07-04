package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var projectionSchema = []string{
	`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		session_type TEXT NOT NULL,
		title TEXT NOT NULL,
		moderator TEXT NOT NULL,
		status TEXT NOT NULL,
		phase TEXT NOT NULL,
		prior_phase TEXT,
		resume_phase TEXT,
		blocked_by_event_id TEXT,
		current_turn INTEGER NOT NULL DEFAULT 0,
		consensus_round INTEGER NOT NULL DEFAULT 0,
		last_speaker TEXT,
		tokens_in_total INTEGER NOT NULL DEFAULT 0,
		tokens_out_total INTEGER NOT NULL DEFAULT 0,
		usd_estimate_total REAL NOT NULL DEFAULT 0.0,
		runner_calls_total INTEGER NOT NULL DEFAULT 0,
		missing_cost_runner_calls_total INTEGER NOT NULL DEFAULT 0,
		user_escalations_total INTEGER NOT NULL DEFAULT 0,
		pending_escalation_batches_total INTEGER NOT NULL DEFAULT 0,
		pending_batched_candidates_total INTEGER NOT NULL DEFAULT 0,
		waiting_user_escalation_event_id TEXT,
		waiting_user_batch_id TEXT,
		surface_json TEXT,
		linked_authority_json TEXT,
		linked_authority_result_json TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		closed_at TEXT
	)`,
	`CREATE TABLE session_participants (
		session_id TEXT NOT NULL,
		member TEXT NOT NULL,
		display_name TEXT,
		role TEXT,
		wrapper TEXT,
		session_ref TEXT,
		stream_cursor TEXT,
		runtime_status TEXT,
		last_heartbeat_at TEXT,
		participant_status TEXT,
		speaking_count INTEGER NOT NULL DEFAULT 0,
		last_spoke_turn INTEGER,
		PRIMARY KEY (session_id, member)
	)`,
	`CREATE TABLE events (
		event_id TEXT PRIMARY KEY,
		schema_version INTEGER NOT NULL,
		command_id TEXT,
		causation_event_id TEXT,
		correlation_id TEXT,
		session_id TEXT NOT NULL,
		session_type TEXT NOT NULL,
		turn INTEGER,
		phase TEXT NOT NULL,
		type TEXT NOT NULL,
		sender TEXT NOT NULL,
		recipient_json TEXT NOT NULL,
		runner_json TEXT,
		cost_tokens_in INTEGER,
		cost_tokens_out INTEGER,
		cost_usd REAL,
		cost_source TEXT,
		created_at TEXT NOT NULL,
		payload_json TEXT NOT NULL
	)`,
	`CREATE INDEX events_by_correlation ON events(correlation_id)`,
	`CREATE INDEX events_by_command ON events(command_id)`,
	`CREATE TABLE runner_invocations (
		invocation_id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		member TEXT NOT NULL,
		adapter_kind TEXT NOT NULL,
		source_command_id TEXT,
		started_event_id TEXT NOT NULL,
		terminal_event_id TEXT,
		attempt INTEGER NOT NULL,
		is_retry INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL,
		cost_tokens_in INTEGER,
		cost_tokens_out INTEGER,
		cost_usd REAL,
		cost_source TEXT,
		cost_missing INTEGER NOT NULL DEFAULT 0,
		duration_sec REAL,
		started_at TEXT NOT NULL,
		completed_at TEXT
	)`,
	`CREATE INDEX runner_invocations_by_session ON runner_invocations(session_id, started_at)`,
	`CREATE INDEX runner_invocations_by_member ON runner_invocations(session_id, member)`,
	`CREATE INDEX runner_invocations_by_source_command ON runner_invocations(session_id, source_command_id)`,
	`CREATE TABLE escalation_batches (
		batch_id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		status TEXT NOT NULL,
		first_event_id TEXT NOT NULL,
		latest_event_id TEXT NOT NULL,
		user_escalation_event_id TEXT,
		batch_window_sec INTEGER NOT NULL,
		batch_deadline_at TEXT NOT NULL,
		prior_phase TEXT NOT NULL,
		resume_phase TEXT,
		pending_count INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		flushed_at TEXT,
		cancelled_at TEXT
	)`,
	`CREATE INDEX escalation_batches_by_session ON escalation_batches(session_id, status, batch_deadline_at)`,
	`CREATE TABLE escalation_batch_items (
		batch_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		source_event_id TEXT NOT NULL,
		added_event_id TEXT NOT NULL,
		source_member TEXT,
		question_hash TEXT NOT NULL,
		urgency TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (batch_id, source_event_id)
	)`,
	`CREATE INDEX escalation_batch_items_by_session ON escalation_batch_items(session_id, batch_id)`,
	`CREATE TABLE event_recipients (
		event_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		recipient TEXT NOT NULL,
		ordinal INTEGER NOT NULL,
		PRIMARY KEY (event_id, recipient),
		UNIQUE (event_id, ordinal)
	)`,
	`CREATE INDEX event_recipients_by_session_recipient ON event_recipients(session_id, recipient, event_id)`,
	`CREATE INDEX event_recipients_by_event ON event_recipients(event_id)`,
	`CREATE TABLE stream_cursors (
		session_id TEXT NOT NULL,
		member TEXT NOT NULL,
		cursor TEXT NOT NULL,
		event_id TEXT NOT NULL,
		acknowledged_at TEXT NOT NULL,
		PRIMARY KEY (session_id, member)
	)`,
	`CREATE TABLE stream_subscribers (
		session_id TEXT NOT NULL,
		member TEXT NOT NULL,
		subscriber_id TEXT NOT NULL,
		connected_at TEXT NOT NULL,
		last_heartbeat_at TEXT NOT NULL,
		last_cursor TEXT,
		status TEXT NOT NULL,
		PRIMARY KEY (session_id, member, subscriber_id)
	)`,
	`CREATE TABLE delegation_reviews (
		session_id TEXT NOT NULL,
		review_round INTEGER NOT NULL,
		reviewer TEXT NOT NULL,
		verdict TEXT NOT NULL,
		findings_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (session_id, review_round, reviewer)
	)`,
	`CREATE TABLE council_hand_raises (
		session_id TEXT NOT NULL,
		turn INTEGER NOT NULL,
		member TEXT NOT NULL,
		wants_to_speak INTEGER NOT NULL,
		intent TEXT,
		relevance INTEGER,
		urgency INTEGER,
		reason TEXT,
		evidence_summary TEXT,
		eligible INTEGER NOT NULL DEFAULT 1,
		ineligibility_reason TEXT,
		drop_status TEXT NOT NULL DEFAULT 'raised',
		drop_event_id TEXT,
		request_event_id TEXT,
		observed_cursor TEXT,
		stance_continuity TEXT,
		current_stance_summary TEXT,
		accepted_claims_json TEXT,
		created_at TEXT NOT NULL,
		PRIMARY KEY (session_id, turn, member)
	)`,
	`CREATE TABLE council_attendance_projection (
		session_id TEXT NOT NULL,
		member TEXT NOT NULL,
		required INTEGER NOT NULL DEFAULT 1,
		attendance_requested_event_id TEXT NOT NULL,
		member_attended_event_id TEXT,
		attendance_status TEXT,
		attendance_summary TEXT,
		surface_evidence_json TEXT,
		requested_at TEXT NOT NULL,
		attended_at TEXT,
		PRIMARY KEY (session_id, member)
	)`,
	`CREATE INDEX council_attendance_by_session_status ON council_attendance_projection(session_id, attendance_status)`,
	`CREATE TABLE council_agenda_locks (
		session_id TEXT PRIMARY KEY,
		agenda_locked_event_id TEXT NOT NULL,
		locked_by TEXT NOT NULL,
		decision_question TEXT NOT NULL,
		constraints_json TEXT,
		surface_evidence_json TEXT,
		locked_at TEXT NOT NULL
	)`,
	`CREATE TABLE council_votes (
		session_id TEXT NOT NULL,
		consensus_round INTEGER NOT NULL,
		draft_version INTEGER NOT NULL,
		member TEXT NOT NULL,
		vote TEXT NOT NULL,
		reason TEXT,
		required_change TEXT,
		created_at TEXT NOT NULL,
		PRIMARY KEY (session_id, consensus_round, draft_version, member)
	)`,
	`CREATE TABLE council_argument_graph_projection (
		session_id TEXT NOT NULL,
		event_id TEXT NOT NULL,
		event_ordinal INTEGER NOT NULL,
		turn INTEGER NOT NULL,
		speaker TEXT NOT NULL,
		speech TEXT NOT NULL,
		contribution_type TEXT,
		new_axis_reason TEXT,
		status TEXT NOT NULL,
		diagnostic TEXT,
		claims_json TEXT NOT NULL,
		stance_links_json TEXT NOT NULL,
		evidence_json TEXT NOT NULL,
		quality_diagnostics_json TEXT NOT NULL,
		relation_audit_json TEXT NOT NULL,
		PRIMARY KEY (session_id, event_id)
	)`,
	`CREATE INDEX council_argument_graph_projection_by_session_turn ON council_argument_graph_projection(session_id, turn, event_ordinal)`,
	`CREATE TABLE linked_authority_results (
		session_id TEXT PRIMARY KEY,
		terminal_event_id TEXT NOT NULL,
		status TEXT NOT NULL,
		kanban_card_id TEXT,
		kanban_comment_id TEXT,
		vault_decision_note TEXT,
		followup_card_id TEXT,
		failure_reason TEXT,
		evidence_json TEXT NOT NULL,
		recorded_at TEXT NOT NULL
	)`,
	`CREATE TABLE commands_seen (
		command_id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		first_event_id TEXT NOT NULL,
		result_summary TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE artifacts (
		session_id TEXT NOT NULL,
		artifact_id TEXT NOT NULL,
		source_path TEXT NOT NULL,
		stored_path TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		sha256 TEXT NOT NULL,
		mime TEXT,
		ingested_at TEXT NOT NULL,
		PRIMARY KEY (session_id, artifact_id)
	)`,
	`CREATE TABLE projection_metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
}

var projectionColumns = map[string][]string{
	"sessions":                          {"id", "session_type", "title", "moderator", "status", "phase", "prior_phase", "resume_phase", "blocked_by_event_id", "current_turn", "consensus_round", "last_speaker", "tokens_in_total", "tokens_out_total", "usd_estimate_total", "runner_calls_total", "missing_cost_runner_calls_total", "user_escalations_total", "pending_escalation_batches_total", "pending_batched_candidates_total", "waiting_user_escalation_event_id", "waiting_user_batch_id", "surface_json", "linked_authority_json", "linked_authority_result_json", "created_at", "updated_at", "closed_at"},
	"session_participants":              {"session_id", "member", "display_name", "role", "wrapper", "session_ref", "stream_cursor", "runtime_status", "last_heartbeat_at", "participant_status", "speaking_count", "last_spoke_turn"},
	"events":                            {"event_id", "schema_version", "command_id", "causation_event_id", "correlation_id", "session_id", "session_type", "turn", "phase", "type", "sender", "recipient_json", "runner_json", "cost_tokens_in", "cost_tokens_out", "cost_usd", "cost_source", "created_at", "payload_json"},
	"runner_invocations":                {"invocation_id", "session_id", "member", "adapter_kind", "source_command_id", "started_event_id", "terminal_event_id", "attempt", "is_retry", "status", "cost_tokens_in", "cost_tokens_out", "cost_usd", "cost_source", "cost_missing", "duration_sec", "started_at", "completed_at"},
	"escalation_batches":                {"batch_id", "session_id", "status", "first_event_id", "latest_event_id", "user_escalation_event_id", "batch_window_sec", "batch_deadline_at", "prior_phase", "resume_phase", "pending_count", "created_at", "updated_at", "flushed_at", "cancelled_at"},
	"escalation_batch_items":            {"batch_id", "session_id", "source_event_id", "added_event_id", "source_member", "question_hash", "urgency", "created_at"},
	"event_recipients":                  {"event_id", "session_id", "recipient", "ordinal"},
	"stream_cursors":                    {"session_id", "member", "cursor", "event_id", "acknowledged_at"},
	"stream_subscribers":                {"session_id", "member", "subscriber_id", "connected_at", "last_heartbeat_at", "last_cursor", "status"},
	"delegation_reviews":                {"session_id", "review_round", "reviewer", "verdict", "findings_json", "created_at"},
	"council_hand_raises":               {"session_id", "turn", "member", "wants_to_speak", "intent", "relevance", "urgency", "reason", "evidence_summary", "eligible", "ineligibility_reason", "drop_status", "drop_event_id", "request_event_id", "observed_cursor", "stance_continuity", "current_stance_summary", "accepted_claims_json", "created_at"},
	"council_attendance_projection":     {"session_id", "member", "required", "attendance_requested_event_id", "member_attended_event_id", "attendance_status", "attendance_summary", "surface_evidence_json", "requested_at", "attended_at"},
	"council_agenda_locks":              {"session_id", "agenda_locked_event_id", "locked_by", "decision_question", "constraints_json", "surface_evidence_json", "locked_at"},
	"council_votes":                     {"session_id", "consensus_round", "draft_version", "member", "vote", "reason", "required_change", "created_at"},
	"council_argument_graph_projection": {"session_id", "event_id", "event_ordinal", "turn", "speaker", "speech", "contribution_type", "new_axis_reason", "status", "diagnostic", "claims_json", "stance_links_json", "evidence_json", "quality_diagnostics_json", "relation_audit_json"},
	"linked_authority_results":          {"session_id", "terminal_event_id", "status", "kanban_card_id", "kanban_comment_id", "vault_decision_note", "followup_card_id", "failure_reason", "evidence_json", "recorded_at"},
	"commands_seen":                     {"command_id", "session_id", "first_event_id", "result_summary", "created_at"},
	"artifacts":                         {"session_id", "artifact_id", "source_path", "stored_path", "size_bytes", "sha256", "mime", "ingested_at"},
	"projection_metadata":               {"key", "value"},
}

var projectionTableOrder = []string{
	"sessions", "session_participants", "events", "runner_invocations", "escalation_batches", "escalation_batch_items",
	"event_recipients", "stream_cursors", "stream_subscribers", "delegation_reviews", "council_hand_raises",
	"council_attendance_projection", "council_agenda_locks", "council_votes", "linked_authority_results",
	"council_argument_graph_projection", "commands_seen", "artifacts", "projection_metadata",
}

func configureRebuildDB(db *sql.DB) error {
	for _, stmt := range []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA temp_store=MEMORY`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA foreign_keys=OFF`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "configure sqlite", err)
		}
	}
	return nil
}

func populateProjectionDB(db *sql.DB, dataHome string, state *projectionState) (*ProjectionReport, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "begin projection transaction", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = tx.Rollback()
		}
	}()
	for _, stmt := range projectionSchema {
		if _, err := tx.Exec(stmt); err != nil {
			return nil, wrapProjectionError(ProjectionErrorStorage, "create projection schema", err)
		}
	}
	if err := insertProjectionRows(tx, state); err != nil {
		return nil, err
	}
	metadata := map[string]string{
		"schema_version":       fmt.Sprint(ProjectionSchemaV1),
		"source_session_count": fmt.Sprint(state.sessionCount),
		"source_event_count":   fmt.Sprint(state.eventCount),
		"source_hash":          state.sourceHash,
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := tx.Exec(`INSERT INTO projection_metadata(key, value) VALUES (?, ?)`, key, metadata[key]); err != nil {
			return nil, wrapProjectionError(ProjectionErrorStorage, "insert projection metadata", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "commit projection", err)
	}
	ok = true
	if err := integrityCheck(db); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "integrity check", err)
	}
	return &ProjectionReport{
		DataHome:      dataHome,
		DBPath:        filepath.Join(dataHome, ProjectionDBName),
		SchemaVersion: ProjectionSchemaV1,
		SessionCount:  state.sessionCount,
		EventCount:    state.eventCount,
		SourceHash:    state.sourceHash,
	}, nil
}

func insertProjectionRows(tx *sql.Tx, state *projectionState) error {
	for _, row := range sortedValues(state.sessions) {
		_, err := tx.Exec(`INSERT INTO sessions VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, row.sessionType, row.title, row.moderator, row.status, row.phase, nullString(row.priorPhase), nullString(row.resumePhase),
			nullString(row.blockedByEventID), row.currentTurn, row.consensusRound, nullString(row.lastSpeaker), row.tokensInTotal, row.tokensOutTotal,
			row.usdEstimateTotal, row.runnerCallsTotal, row.missingCostRunnerCallsTotal, row.userEscalationsTotal, row.pendingEscalationBatches,
			row.pendingBatchedCandidates, nullString(row.waitingUserEscalationEventID), nullString(row.waitingUserBatchID), nullString(row.surfaceJSON),
			nullString(row.linkedAuthorityJSON), nullString(row.linkedAuthorityResultJSON), row.createdAt, row.updatedAt, nullString(row.closedAt))
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert sessions", err)
		}
	}
	for _, row := range sortedValues(state.participants) {
		_, err := tx.Exec(`INSERT INTO session_participants VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, row.sessionID, row.member, nullString(row.displayName), nullString(row.role), nullString(row.wrapper), nullString(row.sessionRef), nullString(row.streamCursor), nullString(row.runtimeStatus), nullString(row.lastHeartbeatAt), nullString(row.participantStatus), row.speakingCount, nullInt(row.lastSpokeTurn, row.hasLastSpokeTurn))
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert session_participants", err)
		}
	}
	for _, row := range state.events {
		_, err := tx.Exec(`INSERT INTO events VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.eventID, row.schemaVersion, nullString(row.commandID), nullString(row.causationEventID), nullString(row.correlationID), row.sessionID, row.sessionType, nullInt(row.turn, row.hasTurn), row.phase, row.eventType, row.sender, row.recipientJSON, nullString(row.runnerJSON), nullInt(row.costTokensIn, row.hasCostTokensIn), nullInt(row.costTokensOut, row.hasCostTokensOut), nullFloat(row.costUSD, row.hasCostUSD), nullString(row.costSource), row.createdAt, row.payloadJSON)
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert events", err)
		}
	}
	for _, row := range state.recipients {
		if _, err := tx.Exec(`INSERT INTO event_recipients VALUES (?,?,?,?)`, row.eventID, row.sessionID, row.recipient, row.ordinal); err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert event_recipients", err)
		}
	}
	for _, row := range sortedValues(state.runners) {
		_, err := tx.Exec(`INSERT INTO runner_invocations VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.invocationID, row.sessionID, row.member, row.adapterKind, nullString(row.sourceCommandID), row.startedEventID, nullString(row.terminalEventID), row.attempt, boolInt(row.isRetry), row.status, nullInt(row.costTokensIn, row.hasCostTokensIn), nullInt(row.costTokensOut, row.hasCostTokensOut), nullFloat(row.costUSD, row.hasCostUSD), nullString(row.costSource), boolInt(row.costMissing), nullFloat(row.durationSec, row.hasDurationSec), row.startedAt, nullString(row.completedAt))
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert runner_invocations", err)
		}
	}
	for _, row := range sortedValues(state.batches) {
		_, err := tx.Exec(`INSERT INTO escalation_batches VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.batchID, row.sessionID, row.status, row.firstEventID, row.latestEventID, nullString(row.userEscalationEventID), row.batchWindowSec, row.batchDeadlineAt, row.priorPhase, nullString(row.resumePhase), row.pendingCount, row.createdAt, row.updatedAt, nullString(row.flushedAt), nullString(row.cancelledAt))
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert escalation_batches", err)
		}
	}
	for _, row := range sortedValues(state.batchItems) {
		_, err := tx.Exec(`INSERT INTO escalation_batch_items VALUES (?,?,?,?,?,?,?,?)`, row.batchID, row.sessionID, row.sourceEventID, row.addedEventID, nullString(row.sourceMember), row.questionHash, row.urgency, row.createdAt)
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert escalation_batch_items", err)
		}
	}
	for _, row := range sortedValues(state.streamCursors) {
		if _, err := tx.Exec(`INSERT INTO stream_cursors VALUES (?,?,?,?,?)`, row.sessionID, row.member, row.cursor, row.eventID, row.acknowledgedAt); err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert stream_cursors", err)
		}
	}
	for _, row := range sortedValues(state.streamSubscribers) {
		if _, err := tx.Exec(`INSERT INTO stream_subscribers VALUES (?,?,?,?,?,?,?)`, row.sessionID, row.member, row.subscriberID, row.connectedAt, row.lastHeartbeatAt, nullString(row.lastCursor), row.status); err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert stream_subscribers", err)
		}
	}
	for _, row := range sortedValues(state.delegationReviews) {
		if _, err := tx.Exec(`INSERT INTO delegation_reviews VALUES (?,?,?,?,?,?)`, row.sessionID, row.reviewRound, row.reviewer, row.verdict, row.findingsJSON, row.createdAt); err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert delegation_reviews", err)
		}
	}
	for _, row := range sortedValues(state.handRaises) {
		_, err := tx.Exec(`INSERT INTO council_hand_raises VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.sessionID, row.turn, row.member, boolInt(row.wantsToSpeak), nullString(row.intent), nullInt(row.relevance, row.hasRelevance), nullInt(row.urgency, row.hasUrgency), nullString(row.reason), nullString(row.evidenceSummary), boolInt(row.eligible), nullString(row.ineligibilityReason), row.dropStatus, nullString(row.dropEventID), nullString(row.requestEventID), nullString(row.observedCursor), nullString(row.stanceContinuity), nullString(row.currentStanceSummary), nullString(row.acceptedClaimsJSON), row.createdAt)
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert council_hand_raises", err)
		}
	}
	for _, row := range sortedValues(state.attendance) {
		_, err := tx.Exec(`INSERT INTO council_attendance_projection VALUES (?,?,?,?,?,?,?,?,?,?)`, row.sessionID, row.member, boolInt(row.required), row.attendanceRequestedEventID, nullString(row.memberAttendedEventID), nullString(row.attendanceStatus), nullString(row.attendanceSummary), nullString(row.surfaceEvidenceJSON), row.requestedAt, nullString(row.attendedAt))
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert council_attendance_projection", err)
		}
	}
	for _, row := range sortedValues(state.agendaLocks) {
		_, err := tx.Exec(`INSERT INTO council_agenda_locks VALUES (?,?,?,?,?,?,?)`, row.sessionID, row.agendaLockedEventID, row.lockedBy, row.decisionQuestion, nullString(row.constraintsJSON), nullString(row.surfaceEvidenceJSON), row.lockedAt)
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert council_agenda_locks", err)
		}
	}
	for _, row := range sortedValues(state.votes) {
		if _, err := tx.Exec(`INSERT INTO council_votes VALUES (?,?,?,?,?,?,?,?)`, row.sessionID, row.consensusRound, row.draftVersion, row.member, row.vote, nullString(row.reason), nullString(row.requiredChange), row.createdAt); err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert council_votes", err)
		}
	}
	for _, row := range sortedValues(state.argumentGraphs) {
		_, err := tx.Exec(`INSERT INTO council_argument_graph_projection VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			row.SessionID, row.EventID, row.EventOrdinal, row.Turn, row.Speaker, row.Speech, nullString(row.ContributionType),
			nullString(row.NewAxisReason), row.Status, nullString(row.Diagnostic), row.ClaimsJSON, row.StanceLinksJSON,
			row.EvidenceJSON, row.QualityDiagnosticsJSON, row.RelationAuditJSON)
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert council_argument_graph_projection", err)
		}
	}
	for _, row := range sortedValues(state.linkedAuthorityResults) {
		_, err := tx.Exec(`INSERT INTO linked_authority_results VALUES (?,?,?,?,?,?,?,?,?,?)`, row.sessionID, row.terminalEventID, row.status, nullString(row.kanbanCardID), nullString(row.kanbanCommentID), nullString(row.vaultDecisionNote), nullString(row.followupCardID), nullString(row.failureReason), row.evidenceJSON, row.recordedAt)
		if err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert linked_authority_results", err)
		}
	}
	for _, row := range sortedValues(state.commands) {
		if _, err := tx.Exec(`INSERT INTO commands_seen VALUES (?,?,?,?,?)`, row.commandID, row.sessionID, row.firstEventID, row.resultSummary, row.createdAt); err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert commands_seen", err)
		}
	}
	for _, row := range sortedValues(state.artifacts) {
		if _, err := tx.Exec(`INSERT INTO artifacts VALUES (?,?,?,?,?,?,?,?)`, row.sessionID, row.artifactID, row.sourcePath, row.storedPath, row.sizeBytes, row.sha256, nullString(row.mime), row.ingestedAt); err != nil {
			return wrapProjectionError(ProjectionErrorStorage, "insert artifacts", err)
		}
	}
	return nil
}

func integrityCheck(db *sql.DB) error {
	var result string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("sqlite integrity_check failed: %s", result)
	}
	return nil
}

func projectionContentHash(db *sql.DB) (string, error) {
	hash := sha256.New()
	for _, table := range projectionTableOrder {
		rows, err := rowsForHash(db, table, projectionColumns[table])
		if err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte(table))
		_, _ = hash.Write([]byte{0})
		for _, row := range rows {
			encoded, _ := json.Marshal(row)
			_, _ = hash.Write(encoded)
			_, _ = hash.Write([]byte{0})
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func rowsForHash(db *sql.DB, table string, cols []string) ([][]any, error) {
	order := strings.Join(cols, ", ")
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s", strings.Join(cols, ", "), table, order)
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := [][]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		scan := make([]any, len(cols))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		row := make([]any, len(values))
		for i, value := range values {
			switch typed := value.(type) {
			case []byte:
				row[i] = string(typed)
			default:
				row[i] = typed
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func metadataValue(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM projection_metadata WHERE key = ?`, key).Scan(&value)
	return value, err
}

func fsyncFileBestEffort(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func cleanupProjectionTemp(tmpPath string) error {
	var firstErr error
	matches, err := filepath.Glob(tmpPath + "*")
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
