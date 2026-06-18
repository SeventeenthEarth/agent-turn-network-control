package storage

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	defaultStreamHeartbeatIntervalSec = 30
	defaultStreamStaleThresholdSec    = 90
	defaultStreamRepollThresholdSec   = 300
	defaultPreparationTimeoutSec      = 600
)

type ParticipantRuntimeReadinessOptions struct {
	RequireAttendance      bool
	RequirePreparation     bool
	RequireSelectedRunner  bool
	SelectedMember         string
	SelectedRunnerEvidence map[string]SelectedRunnerPrerequisite
	Now                    time.Time
}

type ParticipantRuntimeReadinessReport struct {
	SessionID                     string                              `json:"session_id"`
	Ready                         bool                                `json:"ready"`
	LiveReady                     bool                                `json:"live_ready"`
	Status                        string                              `json:"status"`
	GeneratedAt                   string                              `json:"generated_at"`
	RequiredMembers               []string                            `json:"required_members"`
	BlockingReasons               []string                            `json:"blocking_reasons"`
	StreamHeartbeatIntervalSec    int                                 `json:"stream_heartbeat_interval_sec"`
	StreamStaleThresholdSec       int                                 `json:"stream_stale_threshold_sec"`
	StreamRepollThresholdSec      int                                 `json:"stream_repoll_threshold_sec"`
	AttendanceRequired            bool                                `json:"attendance_required"`
	PreparationRequired           bool                                `json:"preparation_required"`
	SelectedRunnerRequired        bool                                `json:"selected_runner_required"`
	Members                       []ParticipantMemberRuntimeReadiness `json:"members"`
	LatestAttendanceRequestEvent  string                              `json:"latest_attendance_request_event_id,omitempty"`
	LatestPreparationRequestEvent string                              `json:"latest_preparation_request_event_id,omitempty"`
}

type ParticipantMemberRuntimeReadiness struct {
	Member                     string                      `json:"member"`
	Ready                      bool                        `json:"ready"`
	LiveReady                  bool                        `json:"live_ready"`
	ReadinessClass             string                      `json:"readiness_class"`
	BlockingReasons            []string                    `json:"blocking_reasons"`
	SubscriberPresence         EvidenceStatus              `json:"subscriber_presence"`
	CursorAck                  EvidenceStatus              `json:"cursor_ack"`
	CursorAckFreshness         EvidenceStatus              `json:"cursor_ack_freshness"`
	HeartbeatFreshness         EvidenceStatus              `json:"heartbeat_freshness"`
	Attendance                 EvidenceStatus              `json:"attendance"`
	Preparation                EvidenceStatus              `json:"preparation"`
	SelectedRunnerPrerequisite *SelectedRunnerPrerequisite `json:"selected_runner_prerequisite,omitempty"`
}

type EvidenceStatus struct {
	Status     string `json:"status"`
	EventID    string `json:"event_id,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	Reason     string `json:"reason,omitempty"`
	RecordedAt string `json:"recorded_at,omitempty"`
}

type SelectedRunnerPrerequisite struct {
	Ready           bool     `json:"ready"`
	Status          string   `json:"status"`
	BlockingReasons []string `json:"blocking_reasons,omitempty"`
	Evidence        []string `json:"evidence,omitempty"`
}

func ParticipantRuntimeReadinessFromLog(sessionDir string, metadata *SessionMetadata, opts ParticipantRuntimeReadinessOptions) (*ParticipantRuntimeReadinessReport, error) {
	if metadata == nil {
		return nil, NewValidationError(CategoryMetadataInvalid, "session", "metadata is required")
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, err
	}
	return ParticipantRuntimeReadinessFromIndex(metadata, index, opts), nil
}

func ParticipantRuntimeReadinessFromIndex(metadata *SessionMetadata, index *LogIndex, opts ParticipantRuntimeReadinessOptions) *ParticipantRuntimeReadinessReport {
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	limits := effectiveRuntimeLimits(metadata)
	required := readinessRequiredMembers(metadata)
	streams := streamEvidenceByMember(index)
	attendanceEvent, attendance := attendanceEvidenceByMember(metadata, index)
	preparationEvent, preparation := preparationEvidenceByMember(metadata, index)

	report := &ParticipantRuntimeReadinessReport{
		SessionID:                  metadata.ID,
		Ready:                      true,
		LiveReady:                  true,
		Status:                     "ready",
		GeneratedAt:                now.Format(time.RFC3339Nano),
		RequiredMembers:            append([]string(nil), required...),
		StreamHeartbeatIntervalSec: limits.StreamHeartbeatIntervalSec,
		StreamStaleThresholdSec:    limits.StreamStaleThresholdSec,
		StreamRepollThresholdSec:   limits.StreamRepollThresholdSec,
		AttendanceRequired:         opts.RequireAttendance,
		PreparationRequired:        opts.RequirePreparation,
		SelectedRunnerRequired:     opts.RequireSelectedRunner,
	}
	if attendanceEvent != nil {
		report.LatestAttendanceRequestEvent = attendanceEvent.EventID
	}
	if preparationEvent != nil {
		report.LatestPreparationRequestEvent = preparationEvent.EventID
	}

	for _, member := range required {
		stream := streams[member]
		row := ParticipantMemberRuntimeReadiness{
			Member:             member,
			Ready:              true,
			LiveReady:          true,
			ReadinessClass:     "success",
			SubscriberPresence: subscriberPresenceStatus(stream),
			CursorAck:          cursorAckStatus(stream, index),
			CursorAckFreshness: cursorAckFreshnessStatus(stream, now, limits.StreamRepollThresholdSec),
			HeartbeatFreshness: heartbeatFreshnessStatus(stream, now, limits.StreamStaleThresholdSec),
			Attendance:         optionalEvidence("not_required"),
			Preparation:        optionalEvidence("not_required"),
		}
		addEvidenceBlockers(&row, "subscriber_presence", row.SubscriberPresence)
		addEvidenceBlockers(&row, "cursor_ack", row.CursorAck)
		addEvidenceBlockers(&row, "cursor_ack_freshness", row.CursorAckFreshness)
		addEvidenceBlockers(&row, "heartbeat_freshness", row.HeartbeatFreshness)
		if opts.RequireAttendance {
			row.Attendance = attendanceReadinessStatus(attendance[member])
			addEvidenceBlockers(&row, "attendance", row.Attendance)
		}
		if opts.RequirePreparation {
			row.Preparation = preparationReadinessStatus(preparation[member])
			addEvidenceBlockers(&row, "preparation", row.Preparation)
		}
		if opts.RequireSelectedRunner && member == strings.TrimSpace(opts.SelectedMember) {
			prereq := opts.SelectedRunnerEvidence[member]
			if prereq.Status == "" {
				prereq = SelectedRunnerPrerequisite{Ready: false, Status: "missing_selected_runner_prerequisite", BlockingReasons: []string{"missing_selected_runner_prerequisite"}}
			}
			row.SelectedRunnerPrerequisite = &prereq
			if !prereq.Ready {
				for _, reason := range prereq.BlockingReasons {
					if strings.TrimSpace(reason) != "" {
						row.BlockingReasons = append(row.BlockingReasons, "selected_runner_prerequisite:"+reason)
					}
				}
				if len(prereq.BlockingReasons) == 0 {
					row.BlockingReasons = append(row.BlockingReasons, "selected_runner_prerequisite:"+prereq.Status)
				}
			}
		}
		row.BlockingReasons = uniqueSorted(row.BlockingReasons)
		if len(row.BlockingReasons) > 0 {
			row.Ready = false
			row.LiveReady = false
			row.ReadinessClass = readinessClass(row.BlockingReasons)
			report.Ready = false
			report.LiveReady = false
			for _, reason := range row.BlockingReasons {
				report.BlockingReasons = append(report.BlockingReasons, member+":"+reason)
			}
		}
		report.Members = append(report.Members, row)
	}
	report.BlockingReasons = uniqueSorted(report.BlockingReasons)
	if len(report.BlockingReasons) > 0 {
		report.Status = "blocked"
	}
	return report
}

func ApplyAttendanceTimeouts(sessionDir string, metadata *SessionMetadata, now time.Time) ([]AppendResult, error) {
	if metadata == nil || metadata.SessionType != SessionTypeCouncil || metadata.Surface == nil || metadata.Surface.Kind != "discord_thread" {
		return nil, nil
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, err
	}
	request := latestEventOfType(index, "attendance_requested")
	if request == nil {
		return nil, nil
	}
	deadline := request.CreatedAt.UTC().Add(time.Duration(timeoutSec(request.Payload, effectiveRuntimeLimits(metadata).PreparationTimeoutSec)) * time.Second)
	if now.UTC().Before(deadline) {
		return nil, nil
	}
	marked := terminalAttendanceAfter(metadata, index, request.EventID)
	var results []AppendResult
	for _, member := range councilMembers(metadata) {
		if marked[member] {
			continue
		}
		commandID := "cmd_attendance_timeout_" + request.EventID + "_" + member
		result, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{
			Action:           "attend",
			Actor:            daemonPrincipal,
			CommandID:        commandID,
			CausationEventID: request.EventID,
			Payload: map[string]any{
				"member":                  member,
				"status":                  "no_response_timeout",
				"summary":                 "attendance timeout expired without participant response",
				"reason":                  "timeout",
				"timeout_source_event_id": request.EventID,
			},
			Now: now,
		})
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func ApplyPreparationTimeouts(sessionDir string, metadata *SessionMetadata, now time.Time) ([]AppendResult, error) {
	if metadata == nil || metadata.SessionType != SessionTypeCouncil || metadata.Surface == nil || metadata.Surface.Kind != "discord_thread" {
		return nil, nil
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, err
	}
	request := latestEventOfType(index, "preparation_requested")
	if request == nil {
		return nil, nil
	}
	deadline := request.CreatedAt.UTC().Add(time.Duration(timeoutSec(request.Payload, effectiveRuntimeLimits(metadata).PreparationTimeoutSec)) * time.Second)
	if now.UTC().Before(deadline) {
		return nil, nil
	}
	marked := terminalPreparationAfter(metadata, index, request.EventID)
	var results []AppendResult
	for _, member := range councilMembers(metadata) {
		if marked[member] {
			continue
		}
		commandID := "cmd_preparation_timeout_" + request.EventID + "_" + member
		result, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{
			Action:           "prepared-partial",
			Actor:            daemonPrincipal,
			CommandID:        commandID,
			CausationEventID: request.EventID,
			Payload: map[string]any{
				"member":                  member,
				"reason":                  "timeout",
				"summary":                 "preparation timeout expired without participant response",
				"timeout_source_event_id": request.EventID,
			},
			Now: now,
		})
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func latestEventOfType(index *LogIndex, eventType string) *EventEnvelope {
	if index == nil {
		return nil
	}
	for i := len(index.Events) - 1; i >= 0; i-- {
		if index.Events[i].Type == eventType {
			event := index.Events[i]
			return &event
		}
	}
	return nil
}

func effectiveRuntimeLimits(metadata *SessionMetadata) Limits {
	var limits Limits
	if metadata != nil {
		limits = metadata.Limits
	}
	if limits.StreamHeartbeatIntervalSec <= 0 {
		limits.StreamHeartbeatIntervalSec = defaultStreamHeartbeatIntervalSec
	}
	if limits.StreamStaleThresholdSec <= 0 {
		limits.StreamStaleThresholdSec = defaultStreamStaleThresholdSec
	}
	if limits.StreamRepollThresholdSec <= 0 {
		limits.StreamRepollThresholdSec = defaultStreamRepollThresholdSec
	}
	if limits.PreparationTimeoutSec <= 0 {
		limits.PreparationTimeoutSec = defaultPreparationTimeoutSec
	}
	return limits
}

func readinessRequiredMembers(metadata *SessionMetadata) []string {
	if metadata != nil && metadata.SessionType == SessionTypeCouncil {
		return councilMembers(metadata)
	}
	out := []string{}
	if metadata != nil {
		for _, member := range metadata.Participants {
			if member != metadata.Moderator {
				out = append(out, member)
			}
		}
	}
	sort.Strings(out)
	return out
}

type memberStreamEvidence struct {
	subscriber *EventEnvelope
	ack        *EventEnvelope
}

func streamEvidenceByMember(index *LogIndex) map[string]memberStreamEvidence {
	out := map[string]memberStreamEvidence{}
	if index == nil {
		return out
	}
	for i := range index.Events {
		event := index.Events[i]
		member := ""
		switch event.Type {
		case "stream_cursor_acknowledged":
			member = payloadStringDefault(event.Payload, "member", event.From)
			evidence := out[member]
			evidence.ack = &event
			out[member] = evidence
		case "stream_subscriber_connected", "stream_subscriber_heartbeat", "stream_subscriber_disconnected", "stream_subscriber_stale":
			member = payloadStringDefault(event.Payload, "member", event.From)
			if strings.TrimSpace(member) == "" || strings.TrimSpace(payloadStringDefault(event.Payload, "subscriber_id", "")) == "" {
				continue
			}
			evidence := out[member]
			evidence.subscriber = &event
			out[member] = evidence
		}
	}
	return out
}

func subscriberPresenceStatus(stream memberStreamEvidence) EvidenceStatus {
	if stream.subscriber == nil {
		return EvidenceStatus{Status: "blocking", Reason: "missing_subscriber"}
	}
	status := payloadStringDefault(stream.subscriber.Payload, "status", strings.TrimPrefix(stream.subscriber.Type, "stream_subscriber_"))
	if status == "disconnected" || status == "stale" || stream.subscriber.Type == "stream_subscriber_disconnected" || stream.subscriber.Type == "stream_subscriber_stale" {
		return EvidenceStatus{Status: "blocking", EventID: stream.subscriber.EventID, Reason: "subscriber_" + status, RecordedAt: stream.subscriber.CreatedAt.UTC().Format(time.RFC3339Nano)}
	}
	return EvidenceStatus{Status: "ready", EventID: stream.subscriber.EventID, Reason: status, RecordedAt: stream.subscriber.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

func cursorAckStatus(stream memberStreamEvidence, index *LogIndex) EvidenceStatus {
	if stream.ack == nil {
		return EvidenceStatus{Status: "blocking", Reason: "missing_cursor_ack"}
	}
	cursor := payloadStringDefault(stream.ack.Payload, "cursor", "")
	offset, eventID, err := ParseCursor(cursor)
	if err != nil || offset < 0 || index == nil || offset >= int64(len(index.Events)) || index.Events[offset].EventID != eventID {
		return EvidenceStatus{Status: "blocking", EventID: stream.ack.EventID, Cursor: cursor, Reason: "invalid_cursor", RecordedAt: stream.ack.CreatedAt.UTC().Format(time.RFC3339Nano)}
	}
	return EvidenceStatus{Status: "ready", EventID: stream.ack.EventID, Cursor: cursor, Reason: "cursor_acknowledged", RecordedAt: stream.ack.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

func cursorAckFreshnessStatus(stream memberStreamEvidence, now time.Time, thresholdSec int) EvidenceStatus {
	if stream.ack == nil {
		return EvidenceStatus{Status: "blocking", Reason: "missing_cursor_ack"}
	}
	if now.Sub(stream.ack.CreatedAt.UTC()) > time.Duration(thresholdSec)*time.Second {
		return EvidenceStatus{Status: "blocking", EventID: stream.ack.EventID, Reason: "stale_cursor_ack", RecordedAt: stream.ack.CreatedAt.UTC().Format(time.RFC3339Nano)}
	}
	return EvidenceStatus{Status: "ready", EventID: stream.ack.EventID, Reason: "fresh_cursor_ack", RecordedAt: stream.ack.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

func heartbeatFreshnessStatus(stream memberStreamEvidence, now time.Time, thresholdSec int) EvidenceStatus {
	if stream.subscriber == nil {
		return EvidenceStatus{Status: "blocking", Reason: "missing_subscriber_heartbeat"}
	}
	if now.Sub(stream.subscriber.CreatedAt.UTC()) > time.Duration(thresholdSec)*time.Second {
		return EvidenceStatus{Status: "blocking", EventID: stream.subscriber.EventID, Reason: "stale_heartbeat", RecordedAt: stream.subscriber.CreatedAt.UTC().Format(time.RFC3339Nano)}
	}
	return EvidenceStatus{Status: "ready", EventID: stream.subscriber.EventID, Reason: "fresh_heartbeat", RecordedAt: stream.subscriber.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

func attendanceEvidenceByMember(metadata *SessionMetadata, index *LogIndex) (*EventEnvelope, map[string]EvidenceStatus) {
	out := map[string]EvidenceStatus{}
	request := latestEventOfType(index, "attendance_requested")
	if request == nil {
		return nil, out
	}
	for _, event := range eventsAfter(index, request.EventID) {
		if event.Type != "member_attended" {
			continue
		}
		member := payloadStringDefault(event.Payload, "member", "")
		if member == "" {
			member = event.From
		}
		if !councilMember(metadata, member) {
			continue
		}
		status := payloadStringDefault(event.Payload, "status", payloadStringDefault(event.Payload, "attendance_status", ""))
		out[member] = EvidenceStatus{Status: status, EventID: event.EventID, Reason: payloadStringDefault(event.Payload, "reason", status), RecordedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano)}
	}
	return request, out
}

func preparationEvidenceByMember(metadata *SessionMetadata, index *LogIndex) (*EventEnvelope, map[string]EvidenceStatus) {
	out := map[string]EvidenceStatus{}
	request := latestEventOfType(index, "preparation_requested")
	if request == nil {
		return nil, out
	}
	for _, event := range eventsAfter(index, request.EventID) {
		member := ""
		status := ""
		switch event.Type {
		case "member_ready":
			member = event.From
			status = "ready"
		case "member_prepared_partial":
			member = payloadStringDefault(event.Payload, "member", "")
			if member == "" {
				member = event.From
			}
			status = "partial"
		default:
			continue
		}
		if !councilMember(metadata, member) {
			continue
		}
		out[member] = EvidenceStatus{Status: status, EventID: event.EventID, Reason: payloadStringDefault(event.Payload, "reason", status), RecordedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano)}
	}
	return request, out
}

func attendanceReadinessStatus(status EvidenceStatus) EvidenceStatus {
	switch status.Status {
	case "":
		return EvidenceStatus{Status: "blocking", Reason: "missing_attendance"}
	case "present":
		status.Status = "ready"
		status.Reason = firstNonEmptyStatusReason(status.Reason, "present")
		return status
	case "partial":
		status.Status = "partial_success"
		status.Reason = firstNonEmptyStatusReason(status.Reason, "partial_attendance")
		return status
	case "unavailable":
		status.Status = "blocking"
		status.Reason = firstNonEmptyStatusReason(status.Reason, "attendance_unavailable")
		return status
	case "no_response_timeout":
		status.Status = "failure"
		status.Reason = "attendance_no_response_timeout"
		return status
	default:
		return EvidenceStatus{Status: "blocking", EventID: status.EventID, Reason: "invalid_attendance_status", RecordedAt: status.RecordedAt}
	}
}

func preparationReadinessStatus(status EvidenceStatus) EvidenceStatus {
	switch status.Status {
	case "":
		return EvidenceStatus{Status: "blocking", Reason: "missing_preparation"}
	case "ready":
		status.Reason = firstNonEmptyStatusReason(status.Reason, "ready")
		return status
	case "partial":
		if status.Reason == "timeout" {
			status.Status = "failure"
			status.Reason = "preparation_timeout"
			return status
		}
		status.Status = "partial_success"
		status.Reason = firstNonEmptyStatusReason(status.Reason, "partial_preparation")
		return status
	default:
		return EvidenceStatus{Status: "blocking", EventID: status.EventID, Reason: "invalid_preparation_status", RecordedAt: status.RecordedAt}
	}
}

func optionalEvidence(status string) EvidenceStatus {
	return EvidenceStatus{Status: status}
}

func addEvidenceBlockers(row *ParticipantMemberRuntimeReadiness, category string, status EvidenceStatus) {
	switch status.Status {
	case "ready", "not_required":
		return
	case "partial_success":
		row.BlockingReasons = append(row.BlockingReasons, category+":"+status.Reason)
	case "failure", "blocking":
		row.BlockingReasons = append(row.BlockingReasons, category+":"+status.Reason)
	default:
		if status.Status != "" {
			row.BlockingReasons = append(row.BlockingReasons, category+":"+status.Status)
		} else {
			row.BlockingReasons = append(row.BlockingReasons, category+":missing")
		}
	}
}

func readinessClass(reasons []string) string {
	for _, reason := range reasons {
		if strings.Contains(reason, "timeout") || strings.Contains(reason, "failure") || strings.Contains(reason, "unavailable") {
			return "failure"
		}
		if strings.Contains(reason, "partial") {
			return "partial_success"
		}
	}
	return "blocking"
}

func terminalAttendanceAfter(metadata *SessionMetadata, index *LogIndex, eventID string) map[string]bool {
	out := map[string]bool{}
	for _, event := range eventsAfter(index, eventID) {
		if event.Type != "member_attended" {
			continue
		}
		member := payloadStringDefault(event.Payload, "member", "")
		if member == "" {
			member = event.From
		}
		if councilMember(metadata, member) && allowedString(payloadStringDefault(event.Payload, "status", ""), "present", "partial", "unavailable", "no_response_timeout") {
			out[member] = true
		}
	}
	return out
}

func terminalPreparationAfter(metadata *SessionMetadata, index *LogIndex, eventID string) map[string]bool {
	out := map[string]bool{}
	for _, event := range eventsAfter(index, eventID) {
		member := ""
		switch event.Type {
		case "member_ready":
			member = event.From
		case "member_prepared_partial":
			member = payloadStringDefault(event.Payload, "member", "")
			if member == "" {
				member = event.From
			}
		default:
			continue
		}
		if councilMember(metadata, member) {
			out[member] = true
		}
	}
	return out
}

func eventsAfter(index *LogIndex, eventID string) []EventEnvelope {
	if index == nil {
		return nil
	}
	start := 0
	for i, event := range index.Events {
		if event.EventID == eventID {
			start = i + 1
			break
		}
	}
	if start >= len(index.Events) {
		return nil
	}
	return index.Events[start:]
}

func timeoutSec(payload map[string]any, fallback int) int {
	if value := positivePayloadInt(payload, "timeout_sec"); value > 0 {
		return value
	}
	if fallback > 0 {
		return fallback
	}
	return defaultPreparationTimeoutSec
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func firstNonEmptyStatusReason(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ParticipantReadinessError(report *ParticipantRuntimeReadinessReport, path string) error {
	if report == nil || report.Ready {
		return nil
	}
	return NewValidationError(CategoryCommandConflict, path, fmt.Sprintf("participant runtime readiness blocked: %s", strings.Join(report.BlockingReasons, ",")))
}
