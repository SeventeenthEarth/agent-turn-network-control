package daemon_test

import (
	"strings"
	"testing"
	"time"

	"hun-control/internal/daemon"
	"hun-control/internal/protocol"
	"hun-control/internal/storage"
)

func TestRUNFIX011PrepareRecordsAttendanceTimeoutAndFailsClosedWithoutRuntimeReadiness(t *testing.T) {
	dataHome, metadata, sessionDir := createRUNFIX011Council(t, "prepare_timeout")
	server := newRUNFIX011ServerAt(dataHome, 3*time.Second)

	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_runfix011_attendance_timeout", map[string]any{"timeout_sec": 1}, time.Second)
	response := server.Handle(councilEventRequest(metadata.ID, "council.prepare", "cmd_runfix011_prepare_timeout", map[string]any{"timeout_sec": 30}))
	if response.OK {
		t.Fatalf("prepare must fail closed when attendance timed out and runtime readiness is absent: %+v", response)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	timeout := findEvent(t, index.Events, "member_attended")
	if timeout.From != "kkachi-agent-networkd" || timeout.Payload["status"] != "no_response_timeout" || timeout.Payload["member"] != "agent-1" {
		t.Fatalf("attendance timeout evidence has wrong shape: %#v", timeout)
	}
	if eventTypeCount(index.Events, "preparation_requested") != 0 {
		t.Fatalf("failed prepare must not append preparation_requested: %#v", index.Events)
	}
}

func TestRUNFIX011PollRecordsPreparationTimeoutAndFailsClosed(t *testing.T) {
	dataHome, metadata, sessionDir := createPreparedRUNFIX011Council(t, "poll_preparation_timeout", true, false)
	server := newRUNFIX011ServerAt(dataHome, 8*time.Second)

	response := server.Handle(councilEventRequest(metadata.ID, "council.poll", "cmd_runfix011_poll_timeout", map[string]any{"turn": 1}))
	if response.OK {
		t.Fatalf("poll must fail closed when preparation timeout is recorded: %+v", response)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	partial := findEvent(t, index.Events, "member_prepared_partial")
	if partial.From != "kkachi-agent-networkd" || partial.Payload["reason"] != "timeout" || partial.Payload["member"] != "agent-1" {
		t.Fatalf("preparation timeout evidence has wrong shape: %#v", partial)
	}
	if eventTypeCount(index.Events, "hand_raise_requested") != 0 {
		t.Fatalf("failed poll must not append hand_raise_requested: %#v", index.Events)
	}
}

func TestRUNFIX011GrantFailsClosedWithDurableDiagnosticWhenRuntimeReadinessMissing(t *testing.T) {
	dataHome, metadata, sessionDir := createPreparedRUNFIX011Council(t, "grant_runtime_missing", false, true)
	server := newRUNFIX011ServerAt(dataHome, 9*time.Second)
	server.RunnerAdapter = &fakeRunRTAdapter{}

	response := server.Handle(councilEventRequest(metadata.ID, "council.grant", "cmd_runfix011_grant_missing_runtime", map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"}))
	if response.OK {
		t.Fatalf("grant must fail closed when participant runtime readiness is missing: %+v", response)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if eventTypeCount(index.Events, "speaker_selected") != 0 {
		t.Fatalf("failed grant must not append speaker_selected: %#v", index.Events)
	}
	diagnostic := findEvent(t, index.Events, "selected_runner_dispatch_failed")
	if diagnostic.Payload["reason"] != "selected_runner_preflight_failed" || diagnostic.Payload["participant_runtime_ready"] != false {
		t.Fatalf("grant failure must leave durable selected-runner diagnostic: %#v", diagnostic.Payload)
	}
	if got := strings.Join(anyStringSlice(diagnostic.Payload["blocking_reasons"]), ","); !strings.Contains(got, "missing_subscriber") {
		t.Fatalf("diagnostic should include missing runtime evidence, got %q payload=%#v", got, diagnostic.Payload)
	}
}

func TestRUNFIX011AutoGrantFailsClosedBeforeSpeakerSelectedWhenRuntimeReadinessMissing(t *testing.T) {
	dataHome, metadata, sessionDir := createPreparedRUNFIX011Council(t, "auto_grant_runtime_missing", false, true)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_runfix011_auto_grant_poll", map[string]any{"turn": 1}, 7*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_runfix011_auto_grant_raise", map[string]any{"turn": 1, "intent": "answer", "reason": "auto eligible"}, 8*time.Second)
	server := newRUNFIX011ServerAt(dataHome, 9*time.Second)
	server.RunnerAdapter = &fakeRunRTAdapter{}

	response := server.Handle(councilEventRequest(metadata.ID, "council.grant", "cmd_runfix011_auto_grant_missing_runtime", map[string]any{"turn": 1, "auto": true}))
	if response.OK {
		t.Fatalf("auto grant must fail closed when participant runtime readiness is missing: %+v", response)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if eventTypeCount(index.Events, "speaker_selected") != 0 {
		t.Fatalf("failed auto grant must not append speaker_selected: %#v", index.Events)
	}
	diagnostic := findEvent(t, index.Events, "selected_runner_dispatch_failed")
	if diagnostic.Payload["reason"] != "selected_runner_preflight_failed" || diagnostic.Payload["selected_member"] != "agent-1" || diagnostic.Payload["participant_runtime_ready"] != false {
		t.Fatalf("auto grant failure must leave durable selected-runner diagnostic: %#v", diagnostic.Payload)
	}
	if got := strings.Join(anyStringSlice(diagnostic.Payload["blocking_reasons"]), ","); !strings.Contains(got, "missing_subscriber") {
		t.Fatalf("diagnostic should include missing runtime evidence, got %q payload=%#v", got, diagnostic.Payload)
	}
}

func createRUNFIX011Council(t *testing.T, suffix string) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHome(t)
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{
			ID:        "sess_runfix011_" + suffix,
			Title:     "RUNFIX-011",
			Moderator: "agent-mod",
			Surface:   &storage.Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "thread_runfix011_" + suffix},
			Limits:    storage.Limits{PreparationTimeoutSec: 1, StreamStaleThresholdSec: 90, StreamRepollThresholdSec: 300},
			EventID:   "evt_created_runfix011_" + suffix,
			CommandID: "cmd_created_runfix011_" + suffix,
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

func createPreparedRUNFIX011Council(t *testing.T, suffix string, includeRuntimeEvidence, includeReady bool) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, metadata, sessionDir := createRUNFIX011Council(t, suffix)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_"+suffix+"_attendance", map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_"+suffix+"_attend", map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_"+suffix+"_agenda", map[string]any{"decision_question": "What should ship?"}, 3*time.Second)
	if includeRuntimeEvidence {
		appendRuntimeEvidenceForRUNFIX011(t, sessionDir, metadata, "agent-1", 4*time.Second)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_"+suffix+"_prepare", map[string]any{"timeout_sec": 1}, 5*time.Second)
	if includeReady {
		appendCouncilEventForDispatch(t, sessionDir, metadata, "ready", "agent-1", "cmd_"+suffix+"_ready", map[string]any{"summary": "ready"}, 6*time.Second)
	}
	return dataHome, metadata, sessionDir
}

func appendRuntimeEvidenceForRUNFIX011(t *testing.T, sessionDir string, metadata *storage.SessionMetadata, member string, delta time.Duration) {
	t.Helper()
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex before runtime evidence: %v", err)
	}
	last := index.Events[len(index.Events)-1]
	cursor := storage.CursorFor(int64(len(index.Events)-1), last.EventID)
	event := storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_runtime_heartbeat_" + member + "_" + last.EventID,
		CommandID:     "cmd_runtime_heartbeat_" + member + "_" + last.EventID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         last.Phase,
		Type:          "stream_subscriber_heartbeat",
		From:          member,
		To:            []string{metadata.Moderator},
		CreatedAt:     daemonFixedRuntime().Now().Add(delta),
		Payload:       map[string]any{"member": member, "subscriber_id": "sub_" + member, "status": "heartbeat", "last_cursor": cursor},
	}
	if _, err := storage.AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent runtime heartbeat: %v", err)
	}
	if _, _, err := storage.AcknowledgeCursor(sessionDir, metadata, member, cursor, "cmd_runtime_ack_"+member+"_"+last.EventID, daemonFixedRuntime().Now().Add(delta+time.Second)); err != nil {
		t.Fatalf("AcknowledgeCursor runtime evidence: %v", err)
	}
}

func newRUNFIX011ServerAt(dataHome string, delta time.Duration) *daemon.Server {
	runtime := daemonFixedRuntime()
	now := runtime.Now().Add(delta)
	runtime.Now = func() time.Time { return now }
	return daemon.NewServer(dataHome, runtime)
}

func councilEventRequest(sessionID, command, commandID string, payload map[string]any) protocol.CommandRequest {
	return protocol.NewRequest(commandID, command, map[string]any{
		"session_id": sessionID,
		"actor":      "agent-mod",
		"command_id": commandID,
		"payload":    payload,
	})
}

func anyStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
