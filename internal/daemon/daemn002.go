package daemon

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/storage"
)

func (s *Server) handleDAEMN002(request protocol.CommandRequest) (protocol.CommandResponse, bool) {
	switch request.Command {
	case protocol.FeatureVersionRead, "version.features":
		return protocol.SuccessResponse(request, structToMap(protocol.NewVersionFeatures())), true
	case protocol.FeatureStreamReplay:
		return s.handleStreamReplay(request), true
	case protocol.FeatureStreamAck:
		return s.handleStreamAck(request), true
	case protocol.FeatureStreamStatus:
		return s.handleStreamStatus(request), true
	case "delegate.escalation_delivered":
		return s.handleDeliveryEvidence(request, "delivered"), true
	case "delegate.escalation_delivery_failed":
		return s.handleDeliveryEvidence(request, "delivery_failed"), true
	case "delegate.new":
		s.sessionMu.Lock()
		defer s.sessionMu.Unlock()
		return s.handleDelegateNew(request), true
	case "council.new":
		s.sessionMu.Lock()
		defer s.sessionMu.Unlock()
		return s.handleCouncilNew(request), true
	case "delegate.ack", "delegate.message", "delegate.clarify", "delegate.answer_clarification",
		"delegate.update", "delegate.request_update", "delegate.submit", "delegate.review",
		"delegate.review_question", "delegate.review_answer", "delegate.review_submit",
		"delegate.revise", "delegate.accept", "delegate.escalate", "delegate.escalation_flush",
		"delegate.resolve_escalation":
		return s.handleDelegationEvent(request), true
	case "delegate.escalation_batches":
		return s.handleEscalationBatches(request), true
	case "council.request_attendance", "council.attend", "council.lock_agenda", "council.prepare",
		"council.ready", "council.prepared_partial", "council.poll", "council.hand_raise",
		"council.grant", "council.speak", "council.intervene", "council.propose",
		"council.revise", "council.request_vote", "council.vote", "council.finalize",
		"council.unresolved":
		return s.handleCouncilEvent(request), true
	case "cancel":
		return s.handleCancel(request), true
	case "block", "delegate.block":
		return s.handleBlock(request), true
	case "resume":
		return s.handleResume(request), true
	case "limits.show":
		return s.handleLimitsShow(request), true
	case "limits.extend":
		return s.handleLimitsExtend(request), true
	case "status.session":
		return s.handleSessionStatus(request), true
	case "transcript.render":
		return s.handleTranscriptRender(request), true
	case "export.bundle":
		return s.handleExportBundle(request), true
	case "tail.session":
		return s.handleTailSession(request), true
	default:
		return protocol.CommandResponse{}, false
	}
}

func (s *Server) handleStreamReplay(request protocol.CommandRequest) protocol.CommandResponse {
	sessionID := stringParam(request, "session_id")
	member := stringParam(request, "member")
	metadata, sessionDir, err := s.loadSession(sessionID)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	if !storage.Participant(metadata, member) {
		return protocol.ErrorResponse(request, daemonProtocolError(storage.NewValidationError(storage.CategoryPrincipalInvalid, "member", "member is not a session participant")))
	}
	follow := boolParam(request, "follow")
	if follow {
		if err := s.recordStreamSubscriberHeartbeat(sessionDir, metadata, member); err != nil {
			return protocol.ErrorResponse(request, daemonProtocolError(err))
		}
	}
	frames, err := storage.ReplayStreamWithAfterReplay(sessionDir, metadata, storage.ReplayOptions{
		FromStart:          boolParam(request, "from_start"),
		Since:              stringParam(request, "since"),
		Follow:             follow,
		FollowTimeout:      durationParam(request, "follow_timeout_ms", time.Millisecond),
		FollowPollInterval: durationParam(request, "follow_poll_ms", time.Millisecond),
	}, s.streamFollowAfterReplay(follow))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, map[string]any{"frames": frames, "follow_bounded": follow})
}

func (s *Server) streamFollowAfterReplay(follow bool) func() error {
	if !follow || s.StreamFollowAfterReplay == nil {
		return nil
	}
	return s.StreamFollowAfterReplay
}

func (s *Server) recordStreamSubscriberHeartbeat(sessionDir string, metadata *storage.SessionMetadata, member string) error {
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return err
	}
	now := s.now()
	phase := storage.PhaseCreated
	lastCursor := ""
	if len(index.Events) > 0 {
		last := index.Events[len(index.Events)-1]
		phase = last.Phase
		lastCursor = storage.CursorFor(int64(len(index.Events)-1), last.EventID)
	}
	suffix := fmt.Sprintf("%d", now.UnixNano())
	_, err = storage.AppendEvent(sessionDir, metadata, storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_stream_subscriber_heartbeat_" + member + "_" + suffix,
		CommandID:     "cmd_stream_subscriber_heartbeat_" + member + "_" + suffix,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         phase,
		Type:          "stream_subscriber_heartbeat",
		From:          member,
		To:            []string{metadata.Moderator},
		CreatedAt:     now,
		Payload: map[string]any{
			"member":        member,
			"subscriber_id": "sub_" + member,
			"status":        "heartbeat",
			"last_cursor":   lastCursor,
		},
	})
	return err
}

func (s *Server) handleStreamAck(request protocol.CommandRequest) protocol.CommandResponse {
	sessionID := stringParam(request, "session_id")
	metadata, sessionDir, err := s.loadSession(sessionID)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result, dedup, err := storage.AcknowledgeCursor(sessionDir, metadata, stringParam(request, "member"), stringParam(request, "cursor"), stringParam(request, "command_id"), s.now())
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, map[string]any{"cursor": result.Cursor, "event_id": result.EventID, "offset": result.Offset, "deduplicated": dedup})
}

func (s *Server) handleStreamStatus(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	status, err := storage.StreamStatusFromLogAt(sessionDir, metadata, s.now())
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, structToMap(status))
}

func (s *Server) handleTranscriptRender(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	format := stringParam(request, "format")
	if format == "" {
		format = storage.TranscriptMarkdownFormat
	}
	content, err := storage.RenderTranscript(sessionDir, metadata, format)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, map[string]any{
		"session_id": metadata.ID,
		"format":     format,
		"content":    string(content),
	})
}

func (s *Server) handleExportBundle(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result, err := storage.BuildExportBundle(sessionDir, metadata, storage.ExportBundleOptions{OutputPath: stringParam(request, "output_path")})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, structToMap(result))
}

func (s *Server) handleTailSession(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	frames, err := storage.ReplayStream(sessionDir, metadata, storage.ReplayOptions{FromStart: true})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	limit := intParam(request, "limit")
	if limit <= 0 {
		limit = 20
	}
	if len(frames) > limit {
		frames = frames[len(frames)-limit:]
	}
	return protocol.SuccessResponse(request, map[string]any{"frames": frames})
}

func (s *Server) handleDeliveryEvidence(request protocol.CommandRequest, kind string) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	reporter := stringParam(request, "reporter")
	if reporter == "" {
		reporter = metadata.Moderator
	}
	result, dedup, err := storage.RecordDeliveryEvidence(sessionDir, metadata, storage.DeliveryEvidence{
		Kind:             kind,
		Reporter:         reporter,
		EscalationEvent:  stringParam(request, "escalation"),
		DeliveryTarget:   stringParam(request, "delivery_target"),
		Platform:         stringParam(request, "platform"),
		MessageRef:       stringParam(request, "message_ref"),
		FailureTarget:    stringParam(request, "target"),
		FailureReason:    stringParam(request, "reason"),
		WillRetryTargets: stringSliceParam(request, "will_retry_targets"),
		CommandID:        stringParam(request, "command_id"),
		Now:              s.now(),
	})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, map[string]any{"cursor": result.Cursor, "event_id": result.EventID, "offset": result.Offset, "deduplicated": dedup})
}

func (s *Server) loadSession(sessionID string) (*storage.SessionMetadata, string, error) {
	if sessionID == "" {
		return nil, "", storage.NewValidationError(storage.CategoryInvalidSessionID, "session_id", "session id is required")
	}
	sessionDir, err := storage.SessionDir(s.DataHome, sessionID)
	if err != nil {
		return nil, "", err
	}
	metadata, err := storage.LoadSessionYAML(sessionDir)
	if err != nil {
		return nil, "", err
	}
	return metadata, filepath.Clean(sessionDir), nil
}

func (s *Server) now() time.Time {
	runtime := s.Runtime
	if runtime.Now != nil {
		return runtime.Now().UTC()
	}
	return time.Now().UTC()
}

func stringParam(request protocol.CommandRequest, key string) string {
	if request.Params == nil {
		return ""
	}
	if value, ok := request.Params[key].(string); ok {
		return value
	}
	return ""
}

func boolParam(request protocol.CommandRequest, key string) bool {
	if request.Params == nil {
		return false
	}
	if value, ok := request.Params[key].(bool); ok {
		return value
	}
	return false
}

func durationParam(request protocol.CommandRequest, key string, unit time.Duration) time.Duration {
	if request.Params == nil {
		return 0
	}
	switch value := request.Params[key].(type) {
	case int:
		if value > 0 {
			return time.Duration(value) * unit
		}
	case int64:
		if value > 0 {
			return time.Duration(value) * unit
		}
	case float64:
		if value > 0 {
			return time.Duration(value) * unit
		}
	}
	return 0
}

func intParam(request protocol.CommandRequest, key string) int {
	if request.Params == nil {
		return 0
	}
	switch value := request.Params[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func stringSliceParam(request protocol.CommandRequest, key string) []string {
	if request.Params == nil {
		return nil
	}
	value, ok := request.Params[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func structToMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}
	return out
}

func daemonProtocolError(err error) error {
	if storage.IsValidationError(err) {
		issues := storage.Issues(err)
		for _, issue := range issues {
			if issue.Category == storage.CategoryCommandConflict && issue.Path == "active_session" {
				return protocol.NewCategorizedError("active_session_locked", "concurrency", issue.Message, protocol.ExitActiveSession, map[string]any{"path": issue.Path}, nil)
			}
			if issue.Category == storage.CategoryCommandConflict {
				return protocol.NewError(protocol.ErrorValidation, issue.Message, protocol.ExitUsage, map[string]any{"category": issue.Category, "path": issue.Path})
			}
		}
		return protocol.NewError(protocol.ErrorValidation, err.Error(), protocol.ExitUsage, map[string]any{"issues": fmt.Sprint(issues)})
	}
	return err
}
