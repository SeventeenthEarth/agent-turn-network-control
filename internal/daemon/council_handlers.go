package daemon

import (
	"fmt"
	"strings"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/storage"
)

func (s *Server) handleCouncilNew(request protocol.CommandRequest) protocol.CommandResponse {
	loaded, err := registry.Load(s.DataHome, s.Runtime)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	sessionID := stringParam(request, "session_id")
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_council_%d", s.now().UnixNano())
	}
	commandID := stringParam(request, "command_id")
	if commandID == "" {
		commandID = "cmd_council_new_" + sessionID
	}
	eventID := stringParam(request, "event_id")
	if eventID == "" {
		eventID = "evt_session_created_" + sessionID
	}
	metadata, results, dedup, err := storage.CreateCouncil(s.DataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{
			ID:              sessionID,
			SessionType:     storage.SessionTypeCouncil,
			Title:           stringParam(request, "title"),
			Moderator:       stringParam(request, "moderator"),
			Surface:         surfaceParam(request),
			LinkedAuthority: linkedAuthorityParam(request),
			TurnMode:        stringParam(request, "turn_mode"),
			Limits:          limitsParam(request),
			EventID:         eventID,
			CommandID:       commandID,
			CorrelationID:   stringParam(request, "correlation_id"),
		},
		Members: stringSliceParam(request, "members"),
		Now:     s.now(),
	}, s.Runtime)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return protocol.SuccessResponse(request, map[string]any{"session_id": metadata.ID, "results": results, "deduplicated": dedup})
}

func (s *Server) handleCouncilEvent(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result, dedup, err := storage.RecordCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{
		Action:           councilActionFromCommand(request.Command),
		Actor:            stringParam(request, "actor"),
		CommandID:        stringParam(request, "command_id"),
		CausationEventID: stringParam(request, "causation_event_id"),
		Payload:          mapParam(request, "payload"),
		Now:              s.now(),
	})
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	return eventAppendResponse(request, result, dedup)
}

func councilActionFromCommand(command string) string {
	action := strings.TrimPrefix(command, "council.")
	return strings.ReplaceAll(action, "_", "-")
}

func councilVerboseStatus(sessionDir string, metadata *storage.SessionMetadata) (map[string]any, error) {
	return storage.CouncilStatusFromLog(sessionDir, metadata)
}
