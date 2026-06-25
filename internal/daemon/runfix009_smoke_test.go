package daemon_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atn-control/internal/daemon"
	"atn-control/internal/protocol"
	"atn-control/internal/runner"
	"atn-control/internal/storage"

	_ "modernc.org/sqlite"
)

func TestRUNFIX009IntegratedControlSmokeFixture(t *testing.T) {
	dataHome, metadata, sessionDir := createRUNFIX009Council(t)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_runfix009_attendance", map[string]any{"timeout_sec": 30}, time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "attend", "agent-1", "cmd_runfix009_attend_agent_1", map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_runfix009_agenda", map[string]any{"decision_question": "What proves RUNFIX-009 locally?"}, 3*time.Second)
	appendRUNFIX009RuntimeEvidence(t, sessionDir, metadata, "agent-1", 3500*time.Millisecond)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_runfix009_prepare", map[string]any{"timeout_sec": 30}, 4*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "ready", "agent-1", "cmd_runfix009_ready_agent_1", map[string]any{"summary": "ready for RUNFIX-009 smoke"}, 4500*time.Millisecond)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "poll", "agent-mod", "cmd_runfix009_poll_1", map[string]any{"turn": 1}, 5*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_runfix009_raise_1", map[string]any{"turn": 1, "intent": "open", "reason": "seed local evidence"}, 6*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "grant", "agent-mod", "cmd_runfix009_grant_1", map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"}, 7*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "speak", "agent-1", "cmd_runfix009_speak_1", map[string]any{
		"turn":              1,
		"speech":            "Opening local evidence axis for RUNFIX-009.",
		"claims":            []any{map[string]any{"claim_id": "T1.C1", "summary": "Integrated smoke evidence must remain control local."}},
		"contribution_type": "new_axis",
		"new_axis_reason":   "opening decision axis",
	}, 8*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "poll", "agent-mod", "cmd_runfix009_poll_2", map[string]any{"turn": 2}, 9*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_runfix009_raise_2", map[string]any{"turn": 2, "intent": "answer", "reason": "selected runner smoke"}, 10*time.Second)

	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{
		OK:                true,
		SemanticEventType: "speech",
		SemanticStatus:    "succeeded",
		Payload: map[string]any{
			"turn":              2,
			"speech":            "Runner-produced speech with diagnostic-only missing relation evidence.",
			"claims":            []any{map[string]any{"claim_id": "T2.C1", "summary": "The fake runner produced durable terminal speech evidence."}},
			"contribution_type": "support",
		},
		Cost: &runner.Cost{TokensIn: 3, TokensOut: 5, USDEstimate: 0.08, Source: runner.HermesAgentCostSource},
	}}}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}

	grant := server.Handle(protocol.NewRequest("cmd_runfix009_grant_2", "council.grant", map[string]any{
		"session_id": metadata.ID,
		"actor":      "agent-mod",
		"command_id": "cmd_runfix009_grant_2",
		"payload": map[string]any{
			"turn":           2,
			"member":         "agent-1",
			"selection_mode": "moderator_direct",
		},
	}))
	if !grant.OK {
		t.Fatalf("RUNFIX-009 selected-speaker grant failed: %+v", grant)
	}
	if adapter.calls != 1 || len(adapter.reqs) != 1 {
		t.Fatalf("RUNFIX-009 smoke must launch exactly one fake local runner call, calls=%d reqs=%#v", adapter.calls, adapter.reqs)
	}
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "propose", "agent-mod", "cmd_runfix009_propose", map[string]any{"draft": "Accept local control smoke evidence."}, 12*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "request-vote", "agent-mod", "cmd_runfix009_vote_request", map[string]any{"timeout_sec": 30}, 13*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "vote", "agent-1", "cmd_runfix009_vote_agent_1", map[string]any{"vote": "approve", "reason": "Local evidence is durable."}, 14*time.Second)
	appendRUNFIX009CouncilEvent(t, sessionDir, metadata, "finalize", "agent-mod", "cmd_runfix009_finalize", map[string]any{
		"final_summary": "RUNFIX-009 control-local smoke fixture finalized.",
		"surface_evidence": map[string]any{
			"status":           "posted",
			"kind":             "discord_thread",
			"final_message_id": "msg-runfix009-final-local",
		},
		"linked_authority_result": map[string]any{
			"status":            "posted",
			"kanban_card_id":    "t_runfix009",
			"kanban_comment_id": "comment-runfix009-local",
			"evidence":          map[string]any{"source": "control-local-smoke"},
		},
	}, 15*time.Second)

	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	selected := lastEventOfType(t, index.Events, "speaker_selected")
	started := findEvent(t, index.Events, "runner_invocation_started")
	speech := lastEventOfType(t, index.Events, "speech")
	finalized := findEvent(t, index.Events, "council_finalized")
	if started.CausationEventID != selected.EventID || speech.CausationEventID != selected.EventID {
		t.Fatalf("RUNFIX-009 requires canonical speaker_selected causation: selected=%s started=%s speech=%s", selected.EventID, started.CausationEventID, speech.CausationEventID)
	}
	if speech.Runner == nil || started.Runner == nil || speech.Runner.InvocationID == "" || speech.Runner.InvocationID != started.Runner.InvocationID || speech.Runner.Member != "agent-1" {
		t.Fatalf("RUNFIX-009 terminal speech must preserve runner invocation/member evidence: started=%#v speech=%#v", started.Runner, speech.Runner)
	}
	if speech.Payload["speech"] != "Runner-produced speech with diagnostic-only missing relation evidence." {
		t.Fatalf("RUNFIX-009 quality_warn must not mutate runner speech text: %#v", speech.Payload)
	}
	diagnostics, ok := speech.Payload["quality_diagnostics"].([]any)
	if !ok || !diagnosticsContain(diagnostics, "orphan_speech") {
		t.Fatalf("RUNFIX-009 speech must expose orphan_speech diagnostics: %#v", speech.Payload)
	}
	if _, ok := speech.Payload["stance_links"]; ok {
		t.Fatalf("RUNFIX-009 quality_warn must not invent fallback stance_links: %#v", speech.Payload)
	}
	status, err := storage.CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	quality := status["discussion_quality"].(map[string]any)
	if quality["mode"] != "quality_warn" || quality["discussion_quality_pass"] != false {
		t.Fatalf("RUNFIX-009 must expose warning-mode quality status: %#v", quality)
	}
	hardWarnings := quality["hard_warning_counts"].(map[string]int)
	if hardWarnings["orphan_speech"] == 0 {
		t.Fatalf("RUNFIX-009 hard warning exposure missing orphan_speech: %#v", quality)
	}
	if finalized.Payload["surface_evidence"] == nil || finalized.Payload["linked_authority_result"] == nil {
		t.Fatalf("RUNFIX-009 closeout must preserve visible surface and linked authority evidence: %#v", finalized.Payload)
	}

	reloadedMetadata, err := storage.LoadSessionYAML(sessionDir)
	if err != nil {
		t.Fatalf("LoadSessionYAML: %v", err)
	}
	transcript, err := storage.RenderTranscript(sessionDir, reloadedMetadata, storage.TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript: %v", err)
	}
	for _, want := range []string{
		"Runner And Cost Summary", "runner_calls_total: `1`",
		"Visible Surface Projection Summary", "msg-runfix009-final-local", "comment-runfix009-local",
		"Argument Graph Projection Summary", "orphan_speech",
		started.Runner.InvocationID,
	} {
		if !strings.Contains(string(transcript), want) {
			t.Fatalf("RUNFIX-009 transcript missing %q:\n%s", want, string(transcript))
		}
	}

	export := server.Handle(protocol.NewRequest("cmd_runfix009_export", "export.bundle", map[string]any{"session_id": metadata.ID}))
	if !export.OK {
		t.Fatalf("export.bundle failed: %+v", export)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(fmt.Sprint(export.Result["bundle_dir"]), "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	for _, want := range []string{"surface_delivery_projection", "argument_graph_projection", "msg-runfix009-final-local", "comment-runfix009-local", "orphan_speech"} {
		if !strings.Contains(string(manifestBytes), want) {
			t.Fatalf("RUNFIX-009 export manifest missing %q:\n%s", want, string(manifestBytes))
		}
	}

	report, err := storage.RebuildProjection(dataHome, storage.ProjectionOptions{Runtime: daemonFixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection: %v", err)
	}
	db := openRUNFIX009ProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	if got := runfix009ScalarText(t, db, `SELECT status FROM runner_invocations WHERE invocation_id = '`+started.Runner.InvocationID+`'`); got != "succeeded" {
		t.Fatalf("RUNFIX-009 runner projection status=%q want succeeded", got)
	}
	if got := runfix009ScalarText(t, db, `SELECT quality_diagnostics_json FROM council_argument_graph_projection WHERE event_id = '`+speech.EventID+`'`); !strings.Contains(got, "orphan_speech") {
		t.Fatalf("RUNFIX-009 ARGUE projection missing orphan_speech diagnostics: %s", got)
	}
	if got := runfix009ScalarText(t, db, `SELECT status FROM linked_authority_results WHERE session_id = '`+metadata.ID+`'`); got != "posted" {
		t.Fatalf("RUNFIX-009 linked authority projection status=%q want posted", got)
	}
}

func createRUNFIX009Council(t *testing.T) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHome(t)
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{
			ID:        "sess_runfix009_smoke",
			Title:     "RUNFIX-009 integrated smoke",
			Moderator: "agent-mod",
			Surface: &storage.Surface{
				Kind:     "discord_thread",
				Platform: "discord",
				ThreadID: "thread-runfix009-local",
			},
			LinkedAuthority: &storage.LinkedAuthority{KanbanCardID: "t_runfix009"},
			Limits: storage.Limits{Council: storage.CouncilLimits{DiscussionQuality: &storage.DiscussionQualityLimits{
				Mode:                 "quality_warn",
				OpeningUnlinkedTurns: 1,
			}}},
			EventID:   "evt_runfix009_created",
			CommandID: "cmd_runfix009_created",
		},
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
	return dataHome, metadata, sessionDir
}

func appendRUNFIX009CouncilEvent(t *testing.T, sessionDir string, metadata *storage.SessionMetadata, action, actor, commandID string, payload map[string]any, delta time.Duration) storage.AppendResult {
	t.Helper()
	result, _, err := storage.RecordCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{
		Action:    action,
		Actor:     actor,
		CommandID: commandID,
		Payload:   payload,
		Now:       daemonFixedRuntime().Now().Add(delta),
	})
	if err != nil {
		content, _ := json.MarshalIndent(payload, "", "  ")
		t.Fatalf("RecordCouncilEvent(%s/%s): %v payload=%s", action, commandID, err, content)
	}
	return result
}

func appendRUNFIX009RuntimeEvidence(t *testing.T, sessionDir string, metadata *storage.SessionMetadata, member string, delta time.Duration) {
	t.Helper()
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex before runtime evidence: %v", err)
	}
	last := index.Events[len(index.Events)-1]
	cursor := storage.CursorFor(int64(len(index.Events)-1), last.EventID)
	event := storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_runfix009_runtime_heartbeat_" + member,
		CommandID:     "cmd_runfix009_runtime_heartbeat_" + member,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         last.Phase,
		Type:          "stream_subscriber_heartbeat",
		From:          member,
		To:            []string{metadata.Moderator},
		CreatedAt:     daemonFixedRuntime().Now().Add(delta),
		Payload: map[string]any{
			"member":        member,
			"subscriber_id": "sub_runfix009_" + member,
			"status":        "heartbeat",
			"last_cursor":   cursor,
		},
	}
	if _, err := storage.AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent runtime heartbeat: %v", err)
	}
	if _, _, err := storage.AcknowledgeCursor(sessionDir, metadata, member, cursor, "cmd_runfix009_runtime_ack_"+member, daemonFixedRuntime().Now().Add(delta+100*time.Millisecond)); err != nil {
		t.Fatalf("AcknowledgeCursor runtime evidence: %v", err)
	}
}

func lastEventOfType(t *testing.T, events []storage.EventEnvelope, typ string) storage.EventEnvelope {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == typ {
			return events[i]
		}
	}
	t.Fatalf("missing event type %s in %#v", typ, events)
	return storage.EventEnvelope{}
}

func diagnosticsContain(diagnostics []any, code string) bool {
	for _, item := range diagnostics {
		row, ok := item.(map[string]any)
		if ok && row["code"] == code {
			return true
		}
	}
	return false
}

func openRUNFIX009ProjectionDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open projection: %v", err)
	}
	return db
}

func runfix009ScalarText(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	var value string
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("query scalar %q: %v", query, err)
	}
	return value
}
