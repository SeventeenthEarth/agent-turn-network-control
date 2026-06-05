package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegrationStreamReplaySinceAndBoundedFollow(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	metadata, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	second, err := AppendEvent(sessionDir, metadata, testEvent(metadata, "evt_000002"))
	if err != nil {
		t.Fatalf("AppendEvent second: %v", err)
	}

	fromStart, err := ReplayStream(sessionDir, metadata, ReplayOptions{FromStart: true})
	if err != nil {
		t.Fatalf("ReplayStream from start: %v", err)
	}
	if len(fromStart) != 2 || fromStart[0].Cursor != "cur_000000000000_evt_created_001" || !fromStart[0].IsReplay {
		t.Fatalf("unexpected from-start frames: %#v", fromStart)
	}

	since, err := ReplayStream(sessionDir, metadata, ReplayOptions{Since: "cur_000000000000_evt_created_001"})
	if err != nil {
		t.Fatalf("ReplayStream since: %v", err)
	}
	if len(since) != 1 || since[0].Cursor != second.Cursor || since[0].Event.EventID != "evt_000002" {
		t.Fatalf("since cursor must be exclusive, got %#v", since)
	}

	frames, err := ReplayStreamWithAfterReplay(sessionDir, metadata, ReplayOptions{FromStart: true, Follow: true}, func() error {
		_, appendErr := AppendEvent(sessionDir, metadata, testEvent(metadata, "evt_live_003"))
		return appendErr
	})
	if err != nil {
		t.Fatalf("ReplayStreamWithAfterReplay: %v", err)
	}
	if len(frames) != 3 || !frames[0].IsReplay || !frames[1].IsReplay || frames[2].IsReplay || frames[2].Event.EventID != "evt_live_003" {
		t.Fatalf("bounded follow must replay first then live appended events, got %#v", frames)
	}
}

func TestIntegrationStreamAckDeduplicatesAndStatusIsReadOnly(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	metadata, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	second, err := AppendEvent(sessionDir, metadata, testEvent(metadata, "evt_000002"))
	if err != nil {
		t.Fatalf("AppendEvent second: %v", err)
	}
	now := time.Date(2026, 6, 4, 12, 2, 0, 0, time.UTC)
	ack, dedup, err := AcknowledgeCursor(sessionDir, metadata, "agent-1", second.Cursor, "cmd_ack_same", now)
	if err != nil || dedup {
		t.Fatalf("first ack got result=%#v dedup=%v err=%v", ack, dedup, err)
	}
	again, dedup, err := AcknowledgeCursor(sessionDir, metadata, "agent-1", second.Cursor, "cmd_ack_same", now.Add(time.Minute))
	if err != nil || !dedup || again.EventID != ack.EventID {
		t.Fatalf("duplicate ack got result=%#v dedup=%v err=%v", again, dedup, err)
	}
	_, _, err = AcknowledgeCursor(sessionDir, metadata, "agent-1", "cur_000000000000_evt_created_001", "cmd_ack_same", now)
	assertStorageIssue(t, err, CategoryCommandConflict)
	_, _, err = AcknowledgeCursor(sessionDir, metadata, "unknown", second.Cursor, "cmd_ack_unknown", now)
	assertStorageIssue(t, err, CategoryPrincipalInvalid)

	channelPath := filepath.Join(sessionDir, ChannelJSONLName)
	before, err := os.ReadFile(channelPath)
	if err != nil {
		t.Fatalf("read channel before status: %v", err)
	}
	status, err := StreamStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("StreamStatusFromLog: %v", err)
	}
	after, err := os.ReadFile(channelPath)
	if err != nil {
		t.Fatalf("read channel after status: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("stream status mutated channel.jsonl")
	}
	if status.Cursors["agent-1"].Cursor != second.Cursor || status.LatestEventID != ack.EventID {
		t.Fatalf("unexpected stream status: %#v", status)
	}
}

func TestIntegrationDeliveryEvidenceEventShapesFollowProtocolSOT(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	metadata, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	escalation := testEvent(metadata, "evt_user_escalation_requested_01")
	escalation.Type = "user_escalation_requested"
	escalation.Phase = "waiting_user"
	escalation.From = "agent-1"
	escalation.To = []string{"agent-mod"}
	escalation.Payload = map[string]any{"question": "Need user input?", "urgency": "blocked"}
	if _, err := AppendEvent(sessionDir, metadata, escalation); err != nil {
		t.Fatalf("append escalation: %v", err)
	}

	delivered, dedup, err := RecordDeliveryEvidence(sessionDir, metadata, DeliveryEvidence{
		Kind:            "delivered",
		Reporter:        "agent-mod",
		EscalationEvent: "evt_user_escalation_requested_01",
		DeliveryTarget:  "origin",
		Platform:        "hermes",
		MessageRef:      "msg_1",
		CommandID:       "cmd_delivery_delivered_shape",
		Now:             time.Date(2026, 6, 5, 1, 3, 0, 0, time.UTC),
	})
	if err != nil || dedup {
		t.Fatalf("RecordDeliveryEvidence delivered result=%#v dedup=%v err=%v", delivered, dedup, err)
	}
	failed, dedup, err := RecordDeliveryEvidence(sessionDir, metadata, DeliveryEvidence{
		Kind:             "delivery_failed",
		Reporter:         "agent-mod",
		EscalationEvent:  "evt_user_escalation_requested_01",
		FailureTarget:    "telegram",
		FailureReason:    "unreachable",
		WillRetryTargets: []string{"origin"},
		CommandID:        "cmd_delivery_failed_shape",
		Now:              time.Date(2026, 6, 5, 1, 4, 0, 0, time.UTC),
	})
	if err != nil || dedup {
		t.Fatalf("RecordDeliveryEvidence failed result=%#v dedup=%v err=%v", failed, dedup, err)
	}

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	deliveredEvent := mustEventByID(t, index, delivered.EventID)
	if deliveredEvent.Type != "user_escalation_delivered" || deliveredEvent.From != "agent-mod" || deliveredEvent.Phase != "waiting_user" {
		t.Fatalf("delivered event envelope drifted: %+v", deliveredEvent)
	}
	assertStringSlice(t, "delivered to", deliveredEvent.To, []string{"user"})
	for _, key := range []string{"escalation_event_id", "delivery_target", "platform", "message_ref", "delivered_batch_id"} {
		if _, ok := deliveredEvent.Payload[key]; !ok {
			t.Fatalf("delivered payload missing %q: %+v", key, deliveredEvent.Payload)
		}
	}
	if deliveredEvent.Payload["delivered_batch_id"] != nil {
		t.Fatalf("non-batched delivery should record null delivered_batch_id, got %+v", deliveredEvent.Payload)
	}

	failedEvent := mustEventByID(t, index, failed.EventID)
	if failedEvent.Type != "user_escalation_delivery_failed" || failedEvent.From != "agent-mod" || failedEvent.Phase != "waiting_user" {
		t.Fatalf("failure event envelope drifted: %+v", failedEvent)
	}
	assertStringSlice(t, "failure to", failedEvent.To, []string{"agent-mod"})
	for _, key := range []string{"escalation_event_id", "target", "reason", "will_retry_targets"} {
		if _, ok := failedEvent.Payload[key]; !ok {
			t.Fatalf("failure payload missing %q: %+v", key, failedEvent.Payload)
		}
	}
}

func TestIntegrationActiveSessionLockUsesDurableTerminalLog(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	metadata, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	secondSpec := testSessionSpec()
	secondSpec.ID = "sess_second"
	secondSpec.EventID = "evt_created_second"
	secondSpec.CommandID = "cmd_create_second"
	_, _, err = CreateSession(dataHome, loaded, secondSpec, fixedRuntime())
	assertStorageIssue(t, err, CategoryCommandConflict)

	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	terminal := testEvent(metadata, "evt_accepted_terminal")
	terminal.Type = "work_accepted"
	terminal.Phase = "accepted"
	terminal.From = "agent-mod"
	terminal.To = []string{"agent-1"}
	terminal.Payload = map[string]any{"accepted_artifacts": []any{}}
	if _, err := AppendEvent(sessionDir, metadata, terminal); err != nil {
		t.Fatalf("append terminal: %v", err)
	}
	if _, _, err := CreateSession(dataHome, loaded, secondSpec, fixedRuntime()); err != nil {
		t.Fatalf("active lock should release after terminal durable event: %v", err)
	}
}

func mustEventByID(t *testing.T, index *LogIndex, eventID string) EventEnvelope {
	t.Helper()
	for _, event := range index.Events {
		if event.EventID == eventID {
			return event
		}
	}
	t.Fatalf("missing event %q in %+v", eventID, index.Events)
	return EventEnvelope{}
}

func assertStringSlice(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d: %#v", label, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q; all=%#v", label, i, got[i], want[i], got)
		}
	}
}
