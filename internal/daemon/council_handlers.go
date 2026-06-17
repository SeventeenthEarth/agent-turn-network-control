package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"kkachi-agent-network-control/internal/memberruntime"
	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/runner"
	"kkachi-agent-network-control/internal/storage"
)

const defaultSelectedSpeakerDispatchTimeout = 30 * time.Second

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
	limits, err := limitsParam(request)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
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
			Limits:          limits,
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
	if dispatchErr := s.dispatchSelectedSpeakerAfterGrant(context.Background(), sessionDir, metadata, result); dispatchErr != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(dispatchErr))
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

func (s *Server) dispatchSelectedSpeakerAfterGrant(ctx context.Context, sessionDir string, metadata *storage.SessionMetadata, result storage.AppendResult) error {
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return err
	}
	event, ok := eventByIDForDispatch(index.Events, result.EventID)
	if !ok || event.Type != "speaker_selected" {
		return nil
	}
	if selectedSpeakerDispatchRecorded(index.Events, event.EventID) {
		return nil
	}
	if selectedSpeakerDispatchStartedWithoutTerminal(index.Events, event.EventID) {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "selected_runner_dispatch_incomplete_stale", "internal/daemon/selected_speaker.go")
	}
	selected, err := selectedSpeakerMember(event)
	if err != nil {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "selected_member_mismatch", "internal/daemon/selected_speaker.go")
	}
	if selected == "" {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "selected_member_missing", "internal/daemon/selected_speaker.go")
	}
	loaded, err := registry.LoadSnapshot(sessionDir, s.Runtime)
	if err != nil {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "registry_snapshot_unavailable", registry.SnapshotFileName)
	}
	member, ok := loaded.Registry.Members[selected]
	if !ok || !member.Enabled {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "selected_member_not_enabled_in_snapshot", registry.SnapshotFileName)
	}
	adapter, err := s.selectedSpeakerAdapter()
	if err != nil {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "runner_adapter_unavailable", "internal/runner")
	}
	dispatchMetadata := *metadata
	dispatchMetadata.State.Phase = event.Phase
	if event.Turn != nil {
		dispatchMetadata.State.CurrentTurn = *event.Turn
	}
	handler := SelectedSpeakerDispatchHandler{
		SessionDir: sessionDir,
		Metadata:   &dispatchMetadata,
		Member:     member,
		Adapter:    adapter,
		Runtime:    s.Runtime,
		Locks:      s.DispatchLocks,
		Now:        s.now,
		MaxRetries: s.selectedSpeakerMaxRetries(metadata),
		Timeout:    s.selectedSpeakerTimeout(metadata),
	}
	frame := storage.StreamFrame{Cursor: result.Cursor, IsReplay: false, Event: event}
	if err := handler.Handle(ctx, frame); err != nil && !errors.Is(err, memberruntime.ErrDurableFailureRecorded) {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "selected_runner_dispatch_failed", "internal/daemon/selected_speaker.go")
	}
	return nil
}

func (s *Server) selectedSpeakerAdapter() (runner.Adapter, error) {
	if s.RunnerAdapter != nil {
		return s.RunnerAdapter, nil
	}
	registry, err := runner.NewRegistry()
	if err != nil {
		return nil, err
	}
	return registry.Get(runner.HermesAgentKind)
}

func (s *Server) selectedSpeakerTimeout(metadata *storage.SessionMetadata) time.Duration {
	if s.SelectedSpeakerTimeout > 0 {
		return s.SelectedSpeakerTimeout
	}
	if metadata != nil && metadata.Limits.DispatchTimeoutSec > 0 {
		return time.Duration(metadata.Limits.DispatchTimeoutSec) * time.Second
	}
	return defaultSelectedSpeakerDispatchTimeout
}

func (s *Server) selectedSpeakerMaxRetries(metadata *storage.SessionMetadata) int {
	if s.SelectedSpeakerMaxRetries > 0 {
		return s.SelectedSpeakerMaxRetries
	}
	if metadata != nil && metadata.Limits.RunnerMaxRetries > 0 {
		return metadata.Limits.RunnerMaxRetries
	}
	return 0
}

func eventByIDForDispatch(events []storage.EventEnvelope, eventID string) (storage.EventEnvelope, bool) {
	for _, event := range events {
		if event.EventID == eventID {
			return event, true
		}
	}
	return storage.EventEnvelope{}, false
}

func selectedSpeakerDispatchRecorded(events []storage.EventEnvelope, speakerEventID string) bool {
	for _, event := range events {
		if event.CausationEventID != speakerEventID {
			continue
		}
		switch event.Type {
		case "runner_invocation_failed", "runner_result_discarded", "speech", "selected_runner_dispatch_failed":
			return true
		}
	}
	return false
}

func selectedSpeakerDispatchStartedWithoutTerminal(events []storage.EventEnvelope, speakerEventID string) bool {
	for _, event := range events {
		if event.CausationEventID == speakerEventID && event.Type == "runner_invocation_started" {
			return true
		}
	}
	return false
}

func (s *Server) appendSelectedSpeakerDispatchDiagnostic(sessionDir string, metadata *storage.SessionMetadata, speaker storage.EventEnvelope, reason, path string) error {
	event := storage.EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventIDFor(speaker.EventID, "selected_runner_dispatch_failed", 1, s.now()),
		CommandID:        speaker.CommandID,
		CausationEventID: speaker.EventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            speaker.Phase,
		Type:             "selected_runner_dispatch_failed",
		From:             "kkachi-agent-networkd",
		To:               []string{metadata.Moderator},
		CreatedAt:        s.now(),
		Payload: map[string]any{
			"reason":           reason,
			"selected_member":  selectedMemberForDiagnostic(speaker),
			"diagnostic_owner": "control/RUNFIX-003",
			"diagnostic_path":  path,
		},
	}
	_, err := storage.AppendEvent(sessionDir, metadata, event)
	return err
}

func selectedMemberForDiagnostic(event storage.EventEnvelope) string {
	if member, err := selectedSpeakerMember(event); err == nil {
		return member
	}
	if member, ok := event.Payload["member"].(string); ok {
		return strings.TrimSpace(member)
	}
	return ""
}
