package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"atn-control/internal/protocol"
	"atn-control/internal/registry"
)

var cursorPattern = regexp.MustCompile(`^cur_([0-9]{12})_(.+)$`)

type StreamFrame struct {
	Cursor   string        `json:"cursor"`
	IsReplay bool          `json:"is_replay"`
	Event    EventEnvelope `json:"event"`
}

type ReplayOptions struct {
	FromStart          bool
	Since              string
	Follow             bool
	FollowTimeout      time.Duration
	FollowPollInterval time.Duration
}

type StreamStatus struct {
	SessionID                   string                             `json:"session_id"`
	LatestCursor                string                             `json:"latest_cursor,omitempty"`
	LatestEventID               string                             `json:"latest_event_id,omitempty"`
	Cursors                     map[string]StreamCursorState       `json:"cursors"`
	Subscribers                 []StreamSubscriberState            `json:"subscribers"`
	ParticipantRuntimeReadiness *ParticipantRuntimeReadinessReport `json:"participant_runtime_readiness,omitempty"`
	SelectedRunnerAccounting    SelectedRunnerAccounting           `json:"selected_runner_accounting,omitempty"`
}

type StreamCursorState struct {
	Cursor         string `json:"cursor"`
	EventID        string `json:"event_id"`
	AcknowledgedAt string `json:"acknowledged_at"`
}

type StreamSubscriberState struct {
	Member          string `json:"member"`
	SubscriberID    string `json:"subscriber_id"`
	LastCursor      string `json:"last_cursor,omitempty"`
	Status          string `json:"status"`
	LastHeartbeatAt string `json:"last_heartbeat_at"`
}

type ActiveSession struct {
	SessionID string `json:"session_id"`
	Phase     Phase  `json:"phase"`
	Status    Status `json:"status"`
}

func ParseCursor(cursor string) (int64, string, error) {
	matches := cursorPattern.FindStringSubmatch(cursor)
	if matches == nil {
		return 0, "", NewValidationError(CategoryInvalidEnvelope, "cursor", "malformed cursor")
	}
	var offset int64
	if _, err := fmt.Sscanf(matches[1], "%d", &offset); err != nil {
		return 0, "", NewValidationError(CategoryInvalidEnvelope, "cursor", "malformed cursor offset")
	}
	if matches[2] == "" {
		return 0, "", NewValidationError(CategoryInvalidEnvelope, "cursor", "cursor event id is required")
	}
	return offset, matches[2], nil
}

func CursorFor(offset int64, eventID string) string {
	return cursorFor(offset, eventID)
}

func ReplayStream(sessionDir string, metadata *SessionMetadata, opts ReplayOptions) ([]StreamFrame, error) {
	return ReplayStreamWithAfterReplay(sessionDir, metadata, opts, nil)
}

func ReplayStreamWithAfterReplay(sessionDir string, metadata *SessionMetadata, opts ReplayOptions, afterReplay func() error) ([]StreamFrame, error) {
	if err := ensureStreamMode(opts); err != nil {
		return nil, err
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, err
	}
	start, err := replayStartOffset(index, opts)
	if err != nil {
		return nil, err
	}
	frames := framesFrom(index.Events, start, true)
	if opts.Follow {
		if afterReplay != nil {
			if err := afterReplay(); err != nil {
				return nil, err
			}
		}
		liveFrames, err := followFrames(sessionDir, metadata, int64(len(index.Events)), opts)
		if err != nil {
			return nil, err
		}
		frames = append(frames, liveFrames...)
	}
	return frames, nil
}

func followFrames(sessionDir string, metadata *SessionMetadata, start int64, opts ReplayOptions) ([]StreamFrame, error) {
	timeout := opts.FollowTimeout
	if timeout <= 0 {
		timeout = 250 * time.Millisecond
	}
	poll := opts.FollowPollInterval
	if poll <= 0 {
		poll = 25 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		index, err := ReadLogIndex(sessionDir, metadata)
		if err != nil {
			return nil, err
		}
		if int64(len(index.Events)) > start {
			return framesFrom(index.Events, start, false), nil
		}
		if !time.Now().Before(deadline) {
			return nil, nil
		}
		sleep := poll
		if remaining := time.Until(deadline); remaining < sleep {
			sleep = remaining
		}
		if sleep > 0 {
			time.Sleep(sleep)
		}
	}
}

func StreamStatusFromLog(sessionDir string, metadata *SessionMetadata) (*StreamStatus, error) {
	return StreamStatusFromLogAt(sessionDir, metadata, time.Now().UTC())
}

func StreamStatusFromLogAt(sessionDir string, metadata *SessionMetadata, now time.Time) (*StreamStatus, error) {
	if metadata == nil {
		return nil, NewValidationError(CategoryMetadataInvalid, "session", "metadata is required")
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, err
	}
	status := &StreamStatus{
		SessionID:   metadata.ID,
		Cursors:     map[string]StreamCursorState{},
		Subscribers: []StreamSubscriberState{},
	}
	if len(index.Events) > 0 {
		last := index.Events[len(index.Events)-1]
		status.LatestCursor = cursorFor(int64(len(index.Events)-1), last.EventID)
		status.LatestEventID = last.EventID
	}
	subscribers := map[string]StreamSubscriberState{}
	for _, event := range index.Events {
		switch event.Type {
		case "stream_cursor_acknowledged":
			member := payloadStringDefault(event.Payload, "member", event.From)
			status.Cursors[member] = StreamCursorState{
				Cursor:         payloadStringDefault(event.Payload, "cursor", ""),
				EventID:        payloadStringDefault(event.Payload, "event_id", ""),
				AcknowledgedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
			}
		case "stream_subscriber_connected", "stream_subscriber_heartbeat", "stream_subscriber_disconnected", "stream_subscriber_stale":
			member := payloadStringDefault(event.Payload, "member", event.From)
			subscriberID := payloadStringDefault(event.Payload, "subscriber_id", "")
			if subscriberID == "" {
				continue
			}
			state := payloadStringDefault(event.Payload, "status", strings.TrimPrefix(event.Type, "stream_subscriber_"))
			subscribers[member+"\x00"+subscriberID] = StreamSubscriberState{
				Member:          member,
				SubscriberID:    subscriberID,
				LastCursor:      payloadStringDefault(event.Payload, "last_cursor", payloadStringDefault(event.Payload, "cursor", "")),
				Status:          state,
				LastHeartbeatAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
			}
		}
	}
	keys := make([]string, 0, len(subscribers))
	for key := range subscribers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		status.Subscribers = append(status.Subscribers, subscribers[key])
	}
	selectedRunnerAccounting := SelectedRunnerAccountingFromIndex(metadata, index)
	status.SelectedRunnerAccounting = selectedRunnerAccounting
	status.ParticipantRuntimeReadiness = ParticipantRuntimeReadinessFromIndex(metadata, index, readinessOptionsForStatus(metadata, index, now, selectedRunnerAccounting))
	return status, nil
}

func readinessOptionsForStatus(metadata *SessionMetadata, index *LogIndex, now time.Time, selectedRunnerAccounting SelectedRunnerAccounting) ParticipantRuntimeReadinessOptions {
	opts := ParticipantRuntimeReadinessOptions{Now: now}
	if metadata == nil || metadata.SessionType != SessionTypeCouncil {
		return opts
	}
	if ref := terminalRuntimeReadinessReference(metadata, index); ref != nil {
		opts.FreshnessNow = ref.CreatedAt.UTC()
		opts.EvaluationMode = "terminal_event_time"
		opts.FreshnessReferenceEventID = ref.EventID
		opts.FreshnessReferenceEventType = ref.Type
	}
	opts.RequireAttendance = latestEventOfType(index, "attendance_requested") != nil
	opts.RequirePreparation = latestEventOfType(index, "preparation_requested") != nil
	if selected := latestEventOfType(index, "speaker_selected"); selected != nil {
		opts.RequireSelectedRunner = true
		opts.SelectedMember = payloadStringDefault(selected.Payload, "member", "")
		if opts.SelectedMember == "" && len(selected.To) == 1 {
			opts.SelectedMember = selected.To[0]
		}
		opts.SelectedRunnerEvidence = map[string]SelectedRunnerPrerequisite{opts.SelectedMember: selectedRunnerEvidenceFromAccounting(selectedRunnerAccounting, selected.EventID)}
	}
	return opts
}

func terminalRuntimeReadinessReference(metadata *SessionMetadata, index *LogIndex) *EventEnvelope {
	if metadata == nil || index == nil || (metadata.Status != StatusTerminal && statusFromPhase(metadata.State.Phase) != StatusTerminal) {
		return nil
	}
	if event := latestEventOfType(index, "speaker_selected"); event != nil {
		return event
	}
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		switch event.Type {
		case "stream_cursor_acknowledged", "stream_subscriber_heartbeat", "stream_subscriber_connected", "member_ready", "member_prepared_partial", "member_attended", "preparation_requested", "attendance_requested":
			return &event
		}
	}
	return nil
}

func AcknowledgeCursor(sessionDir string, metadata *SessionMetadata, member, cursor, commandID string, now time.Time) (AppendResult, bool, error) {
	if !Participant(metadata, member) {
		return AppendResult{}, false, NewValidationError(CategoryPrincipalInvalid, "member", "member is not a session participant")
	}
	offset, eventID, err := ParseCursor(cursor)
	if err != nil {
		return AppendResult{}, false, err
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return AppendResult{}, false, err
	}
	if offset < 0 || offset >= int64(len(index.Events)) || index.Events[offset].EventID != eventID {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "cursor", "cursor does not match channel.jsonl")
	}
	if strings.TrimSpace(commandID) == "" {
		commandID = fmt.Sprintf("cmd_stream_ack_%d_%s", now.UTC().UnixNano(), member)
	}
	event := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventIDFromCommand("evt_stream_ack_"+metadata.ID, commandID),
		CommandID:     commandID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         latestPhase(metadata, index),
		Type:          "stream_cursor_acknowledged",
		From:          "atn-controld",
		To:            []string{member},
		CreatedAt:     now.UTC(),
		Payload:       map[string]any{"member": member, "cursor": cursor, "event_id": eventID},
	}
	return appendIdempotentEvent(sessionDir, metadata, index, event)
}

type DeliveryEvidence struct {
	Kind             string
	Reporter         string
	EscalationEvent  string
	DeliveryTarget   string
	Platform         string
	MessageRef       string
	DeliveredBatchID string
	FailureTarget    string
	FailureReason    string
	WillRetryTargets []string
	CommandID        string
	Now              time.Time
}

func RecordDeliveryEvidence(sessionDir string, metadata *SessionMetadata, evidence DeliveryEvidence) (AppendResult, bool, error) {
	if !Participant(metadata, evidence.Reporter) {
		return AppendResult{}, false, NewValidationError(CategoryPrincipalInvalid, "reporter", "reporter is not a session participant")
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return AppendResult{}, false, err
	}
	escalationEvent, ok := eventByIDAndType(index, evidence.EscalationEvent, "user_escalation_requested")
	if !ok {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "escalation", "escalation must reference user_escalation_requested")
	}
	if strings.TrimSpace(evidence.CommandID) == "" {
		evidence.CommandID = fmt.Sprintf("cmd_delivery_%d_%s", evidence.Now.UTC().UnixNano(), evidence.Reporter)
	}
	payload := map[string]any{"escalation_event_id": evidence.EscalationEvent}
	eventType := ""
	suffix := ""
	to := []string{metadata.Moderator}
	switch evidence.Kind {
	case "delivered":
		if evidence.DeliveryTarget == "" || evidence.Platform == "" {
			return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "delivery", "delivery_target and platform are required")
		}
		eventType = "user_escalation_delivered"
		suffix = "evt_user_escalation_delivered"
		to = []string{"user"}
		payload["delivery_target"] = evidence.DeliveryTarget
		payload["platform"] = evidence.Platform
		payload["delivered_batch_id"] = nil
		if evidence.DeliveredBatchID != "" {
			payload["delivered_batch_id"] = evidence.DeliveredBatchID
		} else if batchID := payloadStringDefault(escalationEvent.Payload, "batch_id", ""); batchID != "" {
			payload["delivered_batch_id"] = batchID
		}
		if evidence.MessageRef != "" {
			payload["message_ref"] = evidence.MessageRef
		}
	case "delivery_failed":
		if evidence.FailureTarget == "" || evidence.FailureReason == "" {
			return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "delivery", "target and reason are required")
		}
		eventType = "user_escalation_delivery_failed"
		suffix = "evt_user_escalation_delivery_failed"
		payload["target"] = evidence.FailureTarget
		payload["reason"] = evidence.FailureReason
		if len(evidence.WillRetryTargets) > 0 {
			payload["will_retry_targets"] = append([]string(nil), evidence.WillRetryTargets...)
		}
	default:
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "delivery.kind", "unknown delivery evidence kind")
	}
	event := EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventIDFromCommand(suffix, evidence.CommandID),
		CommandID:        evidence.CommandID,
		CausationEventID: evidence.EscalationEvent,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            "waiting_user",
		Type:             eventType,
		From:             evidence.Reporter,
		To:               to,
		CreatedAt:        evidence.Now.UTC(),
		Payload:          payload,
	}
	return appendIdempotentEvent(sessionDir, metadata, index, event)
}

func Participant(metadata *SessionMetadata, member string) bool {
	if metadata == nil || strings.TrimSpace(member) == "" {
		return false
	}
	for _, participant := range metadata.Participants {
		if participant == member {
			return true
		}
	}
	return false
}

func FindActiveSession(dataHome string, runtime registry.Runtime) (*ActiveSession, error) {
	runtime = runtimeWithDefaults(runtime)
	cleanDataHome := filepath.Clean(dataHome)
	if err := registry.ValidateDataHome(cleanDataHome, runtime); err != nil {
		return nil, err
	}
	sessionsRoot := filepath.Join(cleanDataHome, SessionsDirName)
	info, err := os.Lstat(sessionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, NewValidationError(CategorySessionUnsafe, sessionsRoot, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, NewValidationError(CategorySessionUnsafe, sessionsRoot, "sessions path is unsafe")
	}
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return nil, NewValidationError(CategorySessionUnsafe, sessionsRoot, err.Error())
	}
	var active *ActiveSession
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		sessionDir, err := SessionDir(cleanDataHome, entry.Name())
		if err != nil {
			return nil, err
		}
		metadata, err := LoadSessionYAML(sessionDir)
		if err != nil {
			return nil, err
		}
		index, err := ReadLogIndex(sessionDir, metadata)
		if err != nil {
			return nil, err
		}
		phase := metadata.State.Phase
		if len(index.Events) > 0 {
			phase = index.Events[len(index.Events)-1].Phase
		}
		status := statusFromPhase(phase)
		if status == StatusTerminal {
			continue
		}
		current := &ActiveSession{SessionID: metadata.ID, Phase: phase, Status: status}
		if active != nil {
			return nil, NewValidationError(CategoryCommandConflict, "active_session", "multiple active sessions found")
		}
		active = current
	}
	return active, nil
}

func ensureStreamMode(opts ReplayOptions) error {
	if opts.FromStart && opts.Since != "" {
		return NewValidationError(CategoryInvalidEnvelope, "stream", "from_start and since are mutually exclusive")
	}
	return nil
}

func replayStartOffset(index *LogIndex, opts ReplayOptions) (int64, error) {
	if opts.Since == "" {
		return 0, nil
	}
	offset, eventID, err := ParseCursor(opts.Since)
	if err != nil {
		return 0, err
	}
	if offset < 0 || offset >= int64(len(index.Events)) || index.Events[offset].EventID != eventID {
		return 0, NewValidationError(CategoryInvalidEnvelope, "cursor", "cursor does not match channel.jsonl")
	}
	return offset + 1, nil
}

func framesFrom(events []EventEnvelope, start int64, replay bool) []StreamFrame {
	if start < 0 {
		start = 0
	}
	if start > int64(len(events)) {
		start = int64(len(events))
	}
	frames := make([]StreamFrame, 0, int64(len(events))-start)
	for i := start; i < int64(len(events)); i++ {
		frames = append(frames, StreamFrame{Cursor: cursorFor(i, events[i].EventID), IsReplay: replay, Event: events[i]})
	}
	return frames
}

func latestPhase(metadata *SessionMetadata, index *LogIndex) Phase {
	if index != nil && len(index.Events) > 0 {
		return index.Events[len(index.Events)-1].Phase
	}
	return metadata.State.Phase
}

func appendIdempotentEvent(sessionDir string, metadata *SessionMetadata, index *LogIndex, event EventEnvelope) (AppendResult, bool, error) {
	prepareEventForAppend(metadata, index, &event)
	if event.CommandID != "" {
		for offset, existing := range index.Events {
			if existing.CommandID != event.CommandID {
				continue
			}
			if commandEquivalent(existing, event) {
				return AppendResult{Cursor: cursorFor(int64(offset), existing.EventID), Offset: int64(offset), EventID: existing.EventID}, true, nil
			}
			return AppendResult{}, false, NewValidationError(CategoryCommandConflict, "command_id", "command_id already used with different payload")
		}
	}
	result, err := AppendEvent(sessionDir, metadata, event)
	return result, false, err
}

// commandEquivalent defines retry equivalence for idempotent command ids. It
// intentionally compares semantic envelope fields and payload only; CreatedAt
// is excluded because a retried command reconstructs the expected envelope at a
// later wall-clock time but must still map back to the original event.
func commandEquivalent(a, b EventEnvelope) bool {
	if a.Type != b.Type || a.CommandID != b.CommandID || a.CausationEventID != b.CausationEventID || a.SessionID != b.SessionID || a.SessionType != b.SessionType || a.Phase != b.Phase || a.From != b.From {
		return false
	}
	leftTo, _ := json.Marshal(a.To)
	rightTo, _ := json.Marshal(b.To)
	if string(leftTo) != string(rightTo) {
		return false
	}
	leftPayload, _ := json.Marshal(normalizedCommandPayload(a))
	rightPayload, _ := json.Marshal(normalizedCommandPayload(b))
	return string(leftPayload) == string(rightPayload)
}

func normalizedCommandPayload(event EventEnvelope) map[string]any {
	payload := clonePayload(event.Payload)
	if payload == nil {
		return nil
	}
	if event.SessionType == SessionTypeCouncil {
		for _, key := range []string{"session_type", "title", "moderator", "participants", "surface", "linked_authority"} {
			delete(payload, key)
		}
	}
	return payload
}

func eventByIDAndType(index *LogIndex, eventID, eventType string) (EventEnvelope, bool) {
	for _, event := range index.Events {
		if event.EventID == eventID && event.Type == eventType {
			return event, true
		}
	}
	return EventEnvelope{}, false
}

func eventIDFromCommand(prefix, commandID string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '_'
	}, commandID)
	if len(clean) > 80 {
		clean = clean[:80]
	}
	return prefix + "_" + clean
}

func payloadStringDefault(payload map[string]any, key, fallback string) string {
	if payload == nil {
		return fallback
	}
	if value, ok := payload[key].(string); ok {
		return value
	}
	return fallback
}
