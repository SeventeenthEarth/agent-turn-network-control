package daemon

import (
	"encoding/json"
	"strings"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/storage"
)

func (s *Server) handleDelegateNew(request protocol.CommandRequest) protocol.CommandResponse {
	loaded, err := registry.Load(s.DataHome, s.Runtime)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	sessionID := stringParam(request, "session_id")
	commandID := stringParam(request, "command_id")
	if commandID == "" {
		commandID = "cmd_delegate_new_" + sessionID
	}
	eventID := stringParam(request, "event_id")
	if eventID == "" {
		eventID = "evt_session_created_" + sessionID
	}
	assignmentEventID := stringParam(request, "assignment_event_id")
	if assignmentEventID == "" {
		assignmentEventID = "evt_task_assigned_" + sessionID
	}
	moderator := stringParam(request, "moderator")
	assignee := stringParam(request, "assignee")
	participants := stringSliceParam(request, "participants")
	if len(participants) == 0 {
		participants = compactStrings(moderator, assignee)
	}
	metadata, results, dedup, err := storage.CreateDelegation(s.DataHome, loaded, storage.DelegationStartSpec{
		Session: storage.SessionSpec{
			ID:              sessionID,
			SessionType:     storage.SessionTypeDelegation,
			Title:           stringParam(request, "title"),
			Moderator:       moderator,
			Participants:    participants,
			Surface:         surfaceParam(request),
			LinkedAuthority: linkedAuthorityParam(request),
			TurnMode:        stringParam(request, "turn_mode"),
			Limits:          limitsParam(request),
			EventID:         eventID,
			CommandID:       commandID,
			CorrelationID:   stringParam(request, "correlation_id"),
		},
		Assignee:          assignee,
		Task:              stringParam(request, "task"),
		Context:           stringParam(request, "context"),
		Acceptance:        stringSliceParam(request, "acceptance"),
		ExpectedOutputs:   stringSliceParam(request, "expected_outputs"),
		AssignmentEventID: assignmentEventID,
		Now:               s.now(),
	}, s.Runtime)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, map[string]any{"session_id": metadata.ID, "results": results, "deduplicated": dedup})
}

func (s *Server) handleDelegationEvent(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	action := delegationActionFromCommand(request.Command)
	result, dedup, err := storage.RecordDelegationEvent(sessionDir, metadata, storage.DelegationEventSpec{
		Action:              action,
		Actor:               stringParam(request, "actor"),
		Recipients:          stringSliceParam(request, "recipients"),
		CommandID:           stringParam(request, "command_id"),
		CausationEventID:    firstNonEmpty(stringParam(request, "causation_event_id"), stringParam(request, "in_reply_to"), stringParam(request, "escalation")),
		Payload:             mapParam(request, "payload"),
		ArtifactSourcePaths: stringSliceParam(request, "artifact_source_paths"),
		Now:                 s.now(),
	})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return eventAppendResponse(request, result, dedup)
}

func (s *Server) handleEscalationBatches(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	batches, err := storage.ListEscalationBatches(sessionDir, metadata)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, map[string]any{"session_id": metadata.ID, "batches": batches})
}

func (s *Server) handleCancel(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result, dedup, err := storage.CancelSession(sessionDir, metadata, storage.SessionCancelSpec{
		Actor:     defaultStringParam(request, "actor", metadata.Moderator),
		Reason:    stringParam(request, "reason"),
		Cause:     stringParam(request, "cause"),
		CommandID: stringParam(request, "command_id"),
		Now:       s.now(),
	})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return eventAppendResponse(request, result, dedup)
}

func (s *Server) handleBlock(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result, dedup, err := storage.BlockSession(sessionDir, metadata, storage.SessionBlockSpec{
		Actor:     defaultStringParam(request, "actor", metadata.Moderator),
		Category:  stringParam(request, "category"),
		Reason:    stringParam(request, "reason"),
		CommandID: stringParam(request, "command_id"),
		Now:       s.now(),
	})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return eventAppendResponse(request, result, dedup)
}

func (s *Server) handleResume(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result, dedup, err := storage.ResumeSession(sessionDir, metadata, storage.SessionResumeSpec{
		Actor:          defaultStringParam(request, "actor", metadata.Moderator),
		BlockedEventID: stringParam(request, "blocked_event_id"),
		Reason:         stringParam(request, "reason"),
		CommandID:      stringParam(request, "command_id"),
		Now:            s.now(),
	})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return eventAppendResponse(request, result, dedup)
}

func (s *Server) handleLimitsShow(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	phase, status, err := sessionPhaseStatus(sessionDir, metadata)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	limits, err := storage.EffectiveLimits(sessionDir, metadata)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, map[string]any{
		"session_id":   metadata.ID,
		"session_type": metadata.SessionType,
		"phase":        phase,
		"status":       status,
		"limits":       limits,
		"observed":     metadata.Cost,
		"escalations":  metadata.Escalations,
	})
}

func (s *Server) handleLimitsExtend(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result, dedup, err := storage.ExtendLimits(sessionDir, metadata, storage.LimitsExtendSpec{
		Actor:          defaultStringParam(request, "actor", metadata.Moderator),
		AuthorizedBy:   stringParam(request, "authorized_by"),
		BlockedEventID: stringParam(request, "blocked_event_id"),
		Changes:        mapParam(request, "changes"),
		Reason:         stringParam(request, "reason"),
		CommandID:      stringParam(request, "command_id"),
		Now:            s.now(),
	})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return eventAppendResponse(request, result, dedup)
}

func (s *Server) handleSessionStatus(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	phase, status, err := sessionPhaseStatus(sessionDir, metadata)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	limits, err := storage.EffectiveLimits(sessionDir, metadata)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result := map[string]any{
		"session_id":    metadata.ID,
		"session_type":  metadata.SessionType,
		"title":         metadata.Title,
		"moderator":     metadata.Moderator,
		"participants":  metadata.Participants,
		"phase":         phase,
		"status":        status,
		"created_at":    metadata.CreatedAt,
		"surface":       metadata.Surface,
		"turn_mode":     metadata.TurnMode,
		"limits":        limits,
		"cost":          metadata.Cost,
		"escalations":   metadata.Escalations,
		"registry_hash": metadata.RegistrySnapshot.SourceSHA256,
	}
	if boolParam(request, "verbose") && metadata.SessionType == storage.SessionTypeCouncil {
		council, err := councilVerboseStatus(sessionDir, metadata)
		if err != nil {
			return protocol.ErrorResponse(request, daemonProtocolError(err))
		}
		result["council"] = council
	}
	return protocol.SuccessResponse(request, result)
}

func eventAppendResponse(request protocol.CommandRequest, result storage.AppendResult, dedup bool) protocol.CommandResponse {
	return protocol.SuccessResponse(request, map[string]any{"cursor": result.Cursor, "event_id": result.EventID, "offset": result.Offset, "deduplicated": dedup})
}

func delegationActionFromCommand(command string) string {
	action := strings.TrimPrefix(command, "delegate.")
	return strings.ReplaceAll(action, "_", "-")
}

func sessionPhaseStatus(sessionDir string, metadata *storage.SessionMetadata) (storage.Phase, storage.Status, error) {
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return "", "", err
	}
	phase := metadata.State.Phase
	if len(index.Events) > 0 {
		phase = index.Events[len(index.Events)-1].Phase
	}
	return phase, statusForPhase(phase), nil
}

func statusForPhase(phase storage.Phase) storage.Status {
	switch phase {
	case "accepted", "cancelled", "finalized", "unresolved":
		return storage.StatusTerminal
	case "blocked":
		return storage.StatusBlocked
	default:
		return storage.StatusOpen
	}
}

func mapParam(request protocol.CommandRequest, key string) map[string]any {
	if request.Params == nil {
		return nil
	}
	value, ok := request.Params[key]
	if !ok || value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	data, err := json.Marshal(value)
	if err != nil || json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}

func limitsParam(request protocol.CommandRequest) storage.Limits {
	var limits storage.Limits
	value := mapParam(request, "limits")
	if len(value) == 0 {
		return limits
	}
	data, err := json.Marshal(value)
	if err == nil {
		_ = json.Unmarshal(data, &limits)
	}
	return limits
}

func surfaceParam(request protocol.CommandRequest) *storage.Surface {
	value := mapParam(request, "surface")
	if len(value) == 0 {
		return nil
	}
	var surface storage.Surface
	data, err := json.Marshal(value)
	if err != nil || json.Unmarshal(data, &surface) != nil {
		return nil
	}
	return &surface
}

func linkedAuthorityParam(request protocol.CommandRequest) *storage.LinkedAuthority {
	value := mapParam(request, "linked_authority")
	if len(value) == 0 {
		return nil
	}
	var linked storage.LinkedAuthority
	data, err := json.Marshal(value)
	if err != nil || json.Unmarshal(data, &linked) != nil {
		return nil
	}
	return &linked
}

func defaultStringParam(request protocol.CommandRequest, key, fallback string) string {
	if value := stringParam(request, key); value != "" {
		return value
	}
	return fallback
}

func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
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
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
