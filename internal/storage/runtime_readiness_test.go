package storage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/protocol"
)

func TestUnitParticipantRuntimeReadinessRequiresSubscriberCursorHeartbeatAttendanceAndPreparation(t *testing.T) {
	sessionDir, metadata := readinessCouncilForTest(t, "sess_runtime_ready", true)
	now := fixedRuntime().Now().Add(20 * time.Second)

	report, err := ParticipantRuntimeReadinessFromLog(sessionDir, metadata, ParticipantRuntimeReadinessOptions{RequireAttendance: true, RequirePreparation: true, Now: now})
	if err != nil {
		t.Fatalf("ParticipantRuntimeReadinessFromLog: %v", err)
	}
	if !report.Ready || !report.LiveReady || report.Status != "ready" {
		t.Fatalf("expected ready report: %#v", report)
	}
	if len(report.Members) != 1 || report.Members[0].ReadinessClass != "success" {
		t.Fatalf("expected one successful member row: %#v", report.Members)
	}
}

func TestUnitParticipantRuntimeReadinessFailsClosedForMissingAndStaleEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		buildReady bool
		nowDelta   time.Duration
		want       string
	}{
		{name: "missing subscriber and cursor", want: "missing_subscriber"},
		{name: "stale heartbeat", buildReady: true, nowDelta: 4 * time.Minute, want: "stale_heartbeat"},
		{name: "stale cursor ack", buildReady: true, nowDelta: 6 * time.Minute, want: "stale_cursor_ack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir, metadata := readinessCouncilForTest(t, "sess_runtime_"+strings.ReplaceAll(tc.name, " ", "_"), tc.buildReady)
			now := fixedRuntime().Now().Add(20 * time.Second)
			if tc.nowDelta > 0 {
				now = fixedRuntime().Now().Add(tc.nowDelta)
			}
			report, err := ParticipantRuntimeReadinessFromLog(sessionDir, metadata, ParticipantRuntimeReadinessOptions{RequireAttendance: true, RequirePreparation: true, Now: now})
			if err != nil {
				t.Fatalf("ParticipantRuntimeReadinessFromLog: %v", err)
			}
			if report.Ready || report.LiveReady {
				t.Fatalf("expected readiness failure: %#v", report)
			}
			if !strings.Contains(strings.Join(report.BlockingReasons, ","), tc.want) {
				t.Fatalf("blocking reasons %v do not contain %q", report.BlockingReasons, tc.want)
			}
		})
	}
}

func TestUnitParticipantRuntimeReadinessDistinguishesTimeoutFailureFromPartialSuccess(t *testing.T) {
	sessionDir, metadata := readinessCouncilForTest(t, "sess_runtime_timeout_distinction", true)
	appendRawEventForTest(t, sessionDir, metadata, "evt_preparation_requested_timeout_distinction", "cmd_prepare_timeout_distinction", "preparation_requested", "preparation", "agent-mod", []string{"agent-1"}, map[string]any{"timeout_sec": 1}, 30*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_member_prepared_partial_timeout_distinction", "cmd_partial_timeout_distinction", "member_prepared_partial", "preparation", "kkachi-agent-networkd", []string{"agent-mod"}, map[string]any{"member": "agent-1", "reason": "timeout", "summary": "timed out"}, 32*time.Second)

	report, err := ParticipantRuntimeReadinessFromLog(sessionDir, metadata, ParticipantRuntimeReadinessOptions{RequireAttendance: true, RequirePreparation: true, Now: fixedRuntime().Now().Add(33 * time.Second)})
	if err != nil {
		t.Fatalf("ParticipantRuntimeReadinessFromLog: %v", err)
	}
	if report.Ready {
		t.Fatalf("timeout preparation must not be ready: %#v", report)
	}
	member := report.Members[0]
	if member.Preparation.Status != "failure" || member.Preparation.Reason != "preparation_timeout" || member.ReadinessClass != "failure" {
		t.Fatalf("timeout preparation should remain failure, got member=%#v", member)
	}
}

func TestUnitStreamStatusTerminalReadinessUsesEventTimeNotStalePostFinalHeartbeat(t *testing.T) {
	sessionDir, metadata := readinessCouncilForTest(t, "sess_runtime_terminal_reference", true)
	metadata.Status = StatusTerminal
	metadata.State.Phase = "finalized"
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}

	status, err := StreamStatusFromLogAt(sessionDir, metadata, fixedRuntime().Now().Add(20*time.Minute))
	if err != nil {
		t.Fatalf("StreamStatusFromLogAt: %v", err)
	}
	report := status.ParticipantRuntimeReadiness
	if report == nil {
		t.Fatalf("expected participant runtime readiness report")
	}
	if !report.Ready || !report.LiveReady || report.Status != "ready" {
		t.Fatalf("terminal report should evaluate readiness at event time, got %#v", report)
	}
	if report.EvaluationMode != "terminal_event_time" || report.FreshnessReferenceEventType != "stream_cursor_acknowledged" {
		t.Fatalf("expected terminal event-time reference, got mode=%q ref_type=%q report=%#v", report.EvaluationMode, report.FreshnessReferenceEventType, report)
	}
	if strings.Contains(strings.Join(report.BlockingReasons, ","), "stale_heartbeat") || strings.Contains(strings.Join(report.BlockingReasons, ","), "stale_cursor_ack") {
		t.Fatalf("terminal readiness must not be blocked by stale post-final freshness: %#v", report.BlockingReasons)
	}
}

func TestUnitStreamStatusTerminalReadinessPrefersLatestSpeakerSelectedReference(t *testing.T) {
	sessionDir, metadata := readinessCouncilForTest(t, "sess_runtime_terminal_speaker_reference", true)
	selected := appendRawEventForTest(t, sessionDir, metadata, "evt_terminal_reference_selected", "cmd_terminal_reference_selected", "speaker_selected", "discussion", "agent-mod", []string{"agent-1"}, map[string]any{"turn": float64(1), "member": "agent-1", "selection_mode": "moderator_direct"}, 20*time.Second)
	appendRunnerEventForRuntimeReadinessTest(t, sessionDir, metadata, "evt_terminal_reference_runner_started", "runner_invocation_started", selected.EventID, "run_terminal_reference", "agent-1", "started", 21*time.Second)
	appendRunnerEventForRuntimeReadinessTest(t, sessionDir, metadata, "evt_terminal_reference_runner_succeeded", "runner_invocation_succeeded", selected.EventID, "run_terminal_reference", "agent-1", "succeeded", 22*time.Second)
	appendRunnerSpeechForRuntimeReadinessTest(t, sessionDir, metadata, "evt_terminal_reference_speech", selected.EventID, "run_terminal_reference", "agent-1", 1, 23*time.Second)
	metadata.Status = StatusTerminal
	metadata.State.Phase = "finalized"
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}

	status, err := StreamStatusFromLogAt(sessionDir, metadata, fixedRuntime().Now().Add(20*time.Minute))
	if err != nil {
		t.Fatalf("StreamStatusFromLogAt: %v", err)
	}
	report := status.ParticipantRuntimeReadiness
	if report == nil {
		t.Fatalf("expected participant runtime readiness report")
	}
	if !report.Ready || !report.LiveReady || report.Status != "ready" {
		t.Fatalf("terminal report should remain ready at selected-speaker event time, got %#v", report)
	}
	if report.EvaluationMode != "terminal_event_time" || report.FreshnessReferenceEventID != selected.EventID || report.FreshnessReferenceEventType != "speaker_selected" {
		t.Fatalf("expected latest speaker_selected reference, got mode=%q ref_id=%q ref_type=%q report=%#v", report.EvaluationMode, report.FreshnessReferenceEventID, report.FreshnessReferenceEventType, report)
	}
	if report.EvaluatedAt == report.GeneratedAt {
		t.Fatalf("terminal report should separate evaluated_at from generated_at: %#v", report)
	}
	if strings.Contains(strings.Join(report.BlockingReasons, ","), "stale_heartbeat") || strings.Contains(strings.Join(report.BlockingReasons, ","), "stale_cursor_ack") {
		t.Fatalf("terminal readiness must not be blocked by stale post-final freshness: %#v", report.BlockingReasons)
	}
}

func readinessCouncilForTest(t *testing.T, sessionID string, includeRuntimeEvidence bool) (string, *SessionMetadata) {
	t.Helper()
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        sessionID,
			Title:     "readiness",
			Moderator: "agent-mod",
			Surface:   &Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "thread_runtime_readiness"},
			Limits:    Limits{StreamStaleThresholdSec: 90, StreamRepollThresholdSec: 300},
			EventID:   "evt_" + sessionID + "_created",
			CommandID: "cmd_" + sessionID + "_new",
		},
		Members: []string{"agent-1"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendRawEventForTest(t, sessionDir, metadata, "evt_"+sessionID+"_attendance", "cmd_"+sessionID+"_attendance", "attendance_requested", "created", "agent-mod", []string{"agent-1"}, map[string]any{"required_members": []any{"agent-1"}, "timeout_sec": 30}, time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_"+sessionID+"_attend", "cmd_"+sessionID+"_attend", "member_attended", "created", "agent-1", []string{"agent-mod"}, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_"+sessionID+"_prepare", "cmd_"+sessionID+"_prepare", "preparation_requested", "preparation", "agent-mod", []string{"agent-1"}, map[string]any{"timeout_sec": 30}, 3*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_"+sessionID+"_ready", "cmd_"+sessionID+"_ready", "member_ready", "preparation", "agent-1", []string{"agent-mod"}, map[string]any{"summary": "ready"}, 4*time.Second)
	if includeRuntimeEvidence {
		appendRawEventForTest(t, sessionDir, metadata, "evt_"+sessionID+"_subscriber", "cmd_"+sessionID+"_subscriber", "stream_subscriber_heartbeat", "preparation", "agent-1", []string{"agent-mod"}, map[string]any{"member": "agent-1", "subscriber_id": "sub_agent_1", "status": "heartbeat", "last_cursor": "cur_000000000004_evt_" + sessionID + "_ready"}, 5*time.Second)
		if _, _, err := AcknowledgeCursor(sessionDir, metadata, "agent-1", "cur_000000000004_evt_"+sessionID+"_ready", "cmd_"+sessionID+"_ack", fixedRuntime().Now().Add(6*time.Second)); err != nil {
			t.Fatalf("AcknowledgeCursor: %v", err)
		}
	}
	return sessionDir, metadata
}

func appendRunnerEventForRuntimeReadinessTest(t *testing.T, sessionDir string, metadata *SessionMetadata, eventID, typ, selectedEventID, invocationID, member, status string, delta time.Duration) {
	t.Helper()
	event := EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventID,
		CommandID:        "cmd_" + eventID,
		CausationEventID: selectedEventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            "discussion",
		Type:             typ,
		From:             "kkachi-agent-networkd",
		To:               []string{member},
		CreatedAt:        fixedRuntime().Now().Add(delta),
		Runner: &RunnerInfo{
			InvocationID:    invocationID,
			AdapterKind:     "hermes-agent",
			Member:          member,
			Attempt:         1,
			SourceCommandID: "cmd_" + eventID,
			Status:          status,
		},
		Payload: map[string]any{"selected_event_id": selectedEventID},
	}
	if typ != "runner_invocation_started" {
		event.Cost = json.RawMessage(`{"tokens_in":1,"tokens_out":1,"usd_estimate":0.01,"source":"fixture"}`)
	}
	if _, err := AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent(%s): %v", typ, err)
	}
}

func appendRunnerSpeechForRuntimeReadinessTest(t *testing.T, sessionDir string, metadata *SessionMetadata, eventID, selectedEventID, invocationID, member string, turn int, delta time.Duration) {
	t.Helper()
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
		CreatedAt:        fixedRuntime().Now().Add(delta),
		Runner: &RunnerInfo{
			InvocationID:    invocationID,
			AdapterKind:     "hermes-agent",
			Member:          member,
			Attempt:         1,
			SourceCommandID: "cmd_" + eventID,
			Status:          "succeeded",
		},
		Cost:    json.RawMessage(`{"tokens_in":1,"tokens_out":1,"usd_estimate":0.01,"source":"fixture"}`),
		Payload: map[string]any{"turn": float64(turn), "member": member, "speech": "selected runner response", "selected_event_id": selectedEventID},
	}
	if _, err := AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("AppendEvent(speech): %v", err)
	}
}
