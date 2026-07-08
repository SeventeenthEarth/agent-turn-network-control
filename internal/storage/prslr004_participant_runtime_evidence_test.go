package storage

import (
	"testing"
	"time"

	"github.com/SeventeenthEarth/agent-turn-network-control/internal/registry"
)

func TestPRSLR004CouncilSpeechAnnotatesAllMemberParticipantRuntimeCoverage(t *testing.T) {
	dataHome, loaded := lvcor001LoadedCouncilRegistry(t, []string{"agent-1", "agent-2"})
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{ID: "sess_prslr004_coverage", Title: "PRSLR-004 coverage", Moderator: "agent-mod", EventID: "evt_prslr004_coverage_created", CommandID: "cmd_prslr004_coverage_new"},
		Members: []string{"agent-1", "agent-2"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	openCouncilForSpeechPRSLR004(t, sessionDir, metadata)

	result, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{
		Action:           "speak",
		Actor:            "agent-1",
		CommandID:        "cmd_prslr004_speech",
		CausationEventID: "evt_prslr004_select_" + metadata.ID,
		Payload: map[string]any{
			"turn":   1,
			"speech": "canonical public speech",
			"claims": []any{map[string]any{"claim_id": "c1", "summary": "opening", "kind": "observation"}},
		},
		Now: fixedRuntime().Now().Add(20 * time.Second),
	})
	if err != nil {
		t.Fatalf("RecordCouncilEvent(speak): %v", err)
	}
	speech := prslr004EventByID(t, sessionDir, metadata, result.EventID)

	evidence := payloadMap(t, speech.Payload, "persistent_participant_runtime_evidence")
	if evidence["status"] != "coverage_complete" || evidence["speech_event_id"] != speech.EventID || evidence["coverage_cursor"] != result.Cursor {
		t.Fatalf("speech coverage evidence should bind status/event/cursor: %#v result=%#v", evidence, result)
	}
	rows := payloadRows(t, evidence, "members")
	if len(rows) != 2 {
		t.Fatalf("expected all council members in coverage rows, got %#v", rows)
	}
	seen := map[string]map[string]any{}
	for _, row := range rows {
		seen[row["member"].(string)] = row
	}
	if seen["agent-1"]["coverage_kind"] != "self_ack" || seen["agent-2"]["coverage_kind"] != "observe_delta" {
		t.Fatalf("speaker must self-ack and non-speaker must observe delta: %#v", seen)
	}
	if seen["agent-1"]["participant_session_handle"] == "" || seen["agent-2"]["participant_session_handle"] == "" {
		t.Fatalf("coverage rows require participant session handles: %#v", seen)
	}
	if seen["agent-1"]["last_cursor"] != result.Cursor || seen["agent-2"]["last_cursor"] != result.Cursor {
		t.Fatalf("all member cursors should advance to public speech cursor: %#v cursor=%s", seen, result.Cursor)
	}

	status, err := CouncilStatusFromLogAt(sessionDir, metadata, fixedRuntime().Now().Add(21*time.Second))
	if err != nil {
		t.Fatalf("CouncilStatusFromLogAt: %v", err)
	}
	if status["persistent_participant_runtime_evidence"] == nil {
		t.Fatalf("council status must export persistent_participant_runtime_evidence: %#v", status)
	}
}

func TestPRSLR004CouncilParticipantHandlesDoNotCollideAcrossCouncils(t *testing.T) {
	dataHomeA, loadedA := lvcor001LoadedCouncilRegistry(t, []string{"agent-1", "agent-2"})
	dataHomeB, loadedB := lvcor001LoadedCouncilRegistry(t, []string{"agent-1", "agent-2"})
	first := prslr004CouncilWithSpeech(t, dataHomeA, loadedA, "sess_prslr004_first", "cmd_prslr004_first_speech")
	second := prslr004CouncilWithSpeech(t, dataHomeB, loadedB, "sess_prslr004_second", "cmd_prslr004_second_speech")
	firstEvidence := payloadMap(t, first.Payload, "persistent_participant_runtime_evidence")
	secondEvidence := payloadMap(t, second.Payload, "persistent_participant_runtime_evidence")
	firstRows := payloadRows(t, firstEvidence, "members")
	secondRows := payloadRows(t, secondEvidence, "members")
	if firstRows[0]["participant_session_handle"] == secondRows[0]["participant_session_handle"] || firstRows[1]["participant_session_handle"] == secondRows[1]["participant_session_handle"] {
		t.Fatalf("same member handles must not collide across councils: first=%#v second=%#v", firstRows, secondRows)
	}
}

func prslr004CouncilWithSpeech(t *testing.T, dataHome string, loaded *registry.LoadedRegistry, sessionID, commandID string) EventEnvelope {
	t.Helper()
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{ID: sessionID, Title: "PRSLR-004 coverage", Moderator: "agent-mod", EventID: "evt_" + sessionID + "_created", CommandID: "cmd_" + sessionID + "_new"},
		Members: []string{"agent-1", "agent-2"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil(%s): %v", sessionID, err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	openCouncilForSpeechPRSLR004(t, sessionDir, metadata)
	result, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-1", CommandID: commandID, CausationEventID: "evt_prslr004_select_" + metadata.ID, Payload: map[string]any{"turn": 1, "speech": "hello", "claims": []any{map[string]any{"claim_id": "c1", "summary": "opening", "kind": "observation"}}}, Now: fixedRuntime().Now().Add(20 * time.Second)})
	if err != nil {
		t.Fatalf("RecordCouncilEvent(speak %s): %v", sessionID, err)
	}
	return prslr004EventByID(t, sessionDir, metadata, result.EventID)
}

func openCouncilForSpeechPRSLR004(t *testing.T, sessionDir string, metadata *SessionMetadata) {
	t.Helper()
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_attendance_"+metadata.ID, "cmd_prslr004_attendance_"+metadata.ID, "attendance_requested", "created", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"required_members": []any{"agent-1", "agent-2"}}, time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_attend1_"+metadata.ID, "cmd_prslr004_attend1_"+metadata.ID, "member_attended", "created", "agent-1", []string{"agent-mod"}, map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_attend2_"+metadata.ID, "cmd_prslr004_attend2_"+metadata.ID, "member_attended", "created", "agent-2", []string{"agent-mod"}, map[string]any{"status": "present", "summary": "ready"}, 3*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_prepare_"+metadata.ID, "cmd_prslr004_prepare_"+metadata.ID, "preparation_requested", "preparation", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{}, 4*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_ready1_"+metadata.ID, "cmd_prslr004_ready1_"+metadata.ID, "member_ready", "preparation", "agent-1", []string{"agent-mod"}, map[string]any{"summary": "ready"}, 5*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_ready2_"+metadata.ID, "cmd_prslr004_ready2_"+metadata.ID, "member_ready", "preparation", "agent-2", []string{"agent-mod"}, map[string]any{"summary": "ready"}, 6*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_poll_"+metadata.ID, "cmd_prslr004_poll_"+metadata.ID, "hand_raise_requested", "discussion", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"turn": float64(1), "required_members": []any{"agent-1", "agent-2"}}, 7*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_raise1_"+metadata.ID, "cmd_prslr004_raise1_"+metadata.ID, "hand_raise", "discussion", "agent-1", []string{"agent-mod"}, map[string]any{"turn": float64(1), "intent": "speak", "reason": "ready"}, 8*time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr004_select_"+metadata.ID, "cmd_prslr004_select_"+metadata.ID, "speaker_selected", "discussion", "agent-mod", []string{"agent-1"}, map[string]any{"turn": float64(1), "member": "agent-1", "selection_mode": "moderator_direct", "stance_assignment": "ready"}, 9*time.Second)
}

func prslr004EventByID(t *testing.T, sessionDir string, metadata *SessionMetadata, eventID string) EventEnvelope {
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

func payloadMap(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := payload[key].(map[string]any)
	if !ok || value == nil {
		t.Fatalf("payload[%s] not map: %#v", key, payload[key])
	}
	return value
}

func payloadRows(t *testing.T, payload map[string]any, key string) []map[string]any {
	t.Helper()
	values, ok := payload[key].([]map[string]any)
	if ok {
		return values
	}
	anyValues, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("payload[%s] not rows: %#v", key, payload[key])
	}
	out := make([]map[string]any, 0, len(anyValues))
	for _, value := range anyValues {
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("payload[%s] row not map: %#v", key, value)
		}
		out = append(out, row)
	}
	return out
}
