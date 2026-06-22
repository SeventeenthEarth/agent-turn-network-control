package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hun-control/internal/memberruntime"
	"hun-control/internal/protocol"
	"hun-control/internal/registry"
	"hun-control/internal/runner"
	"hun-control/internal/storage"
)

const defaultSelectedSpeakerDispatchTimeout = 30 * time.Second

func (s *Server) handleCouncilNew(request protocol.CommandRequest) protocol.CommandResponse {
	limits, err := limitsParam(request)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	moderator := stringParam(request, "moderator")
	members := stringSliceParam(request, "members")
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
	startSpec := storage.CouncilStartSpec{
		Session: storage.SessionSpec{
			ID:              sessionID,
			SessionType:     storage.SessionTypeCouncil,
			Title:           stringParam(request, "title"),
			Moderator:       moderator,
			Surface:         surfaceParam(request),
			LinkedAuthority: linkedAuthorityParam(request),
			TurnMode:        stringParam(request, "turn_mode"),
			Limits:          limits,
			EventID:         eventID,
			CommandID:       commandID,
			CorrelationID:   stringParam(request, "correlation_id"),
		},
		Members: members,
		Now:     s.now(),
	}
	loaded, err := registry.Load(s.DataHome, s.Runtime)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	principals, err := storage.CouncilReconcilePrincipals(s.DataHome, loaded, startSpec, s.Runtime)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	reconcileReport := registry.ReconcileReport{BeforeSHA256: loaded.SourceSHA256, AfterSHA256: loaded.SourceSHA256}
	if len(principals) > 0 {
		loaded, reconcileReport, err = registry.ReconcileCouncilMembers(s.DataHome, principals, s.Runtime)
		if err != nil {
			return protocol.ErrorResponse(request, daemonProtocolError(err))
		}
	}
	metadata, results, dedup, err := storage.CreateCouncil(s.DataHome, loaded, startSpec, s.Runtime)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	response := map[string]any{"session_id": metadata.ID, "results": results, "deduplicated": dedup}
	if len(reconcileReport.Added) > 0 {
		response["registry_reconcile"] = map[string]any{
			"added":         append([]string(nil), reconcileReport.Added...),
			"source_sha256": reconcileReport.AfterSHA256,
		}
	}
	return protocol.SuccessResponse(request, response)
}

func (s *Server) handleCouncilEvent(request protocol.CommandRequest) protocol.CommandResponse {
	metadata, sessionDir, err := s.loadSession(stringParam(request, "session_id"))
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	action := councilActionFromCommand(request.Command)
	if err := s.applyCouncilRuntimePreflight(request, sessionDir, metadata, action); err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	result, dedup, err := storage.RecordCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{
		Action:           action,
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

func (s *Server) applyCouncilRuntimePreflight(request protocol.CommandRequest, sessionDir string, metadata *storage.SessionMetadata, action string) error {
	if metadata == nil || metadata.SessionType != storage.SessionTypeCouncil || metadata.Surface == nil || metadata.Surface.Kind != "discord_thread" {
		return nil
	}
	now := s.now()
	switch action {
	case "prepare":
		if _, err := storage.ApplyAttendanceTimeouts(sessionDir, metadata, now); err != nil {
			return err
		}
		report, err := storage.ParticipantRuntimeReadinessFromLog(sessionDir, metadata, storage.ParticipantRuntimeReadinessOptions{RequireAttendance: true, Now: now})
		if err != nil {
			return err
		}
		return storage.ParticipantReadinessError(report, "participant_runtime_readiness")
	case "poll":
		if _, err := storage.ApplyPreparationTimeouts(sessionDir, metadata, now); err != nil {
			return err
		}
		report, err := storage.ParticipantRuntimeReadinessFromLog(sessionDir, metadata, storage.ParticipantRuntimeReadinessOptions{RequireAttendance: true, RequirePreparation: true, Now: now})
		if err != nil {
			return err
		}
		return storage.ParticipantReadinessError(report, "participant_runtime_readiness")
	case "grant":
		selected, err := s.selectedMemberForGrantPreflight(sessionDir, metadata, request)
		if err != nil {
			return err
		}
		prereq := s.selectedRunnerPrerequisite(sessionDir, selected)
		report, err := storage.ParticipantRuntimeReadinessFromLog(sessionDir, metadata, storage.ParticipantRuntimeReadinessOptions{
			RequireAttendance:      true,
			RequirePreparation:     true,
			RequireSelectedRunner:  true,
			SelectedMember:         selected,
			SelectedRunnerEvidence: map[string]storage.SelectedRunnerPrerequisite{selected: prereq},
			Now:                    now,
		})
		if err != nil {
			return err
		}
		if report.Ready {
			return nil
		}
		if err := s.appendSelectedRunnerPreflightDiagnostic(sessionDir, metadata, request, selected, report); err != nil {
			return err
		}
		return storage.ParticipantReadinessError(report, "selected_runner_prerequisite")
	default:
		return nil
	}
}

func (s *Server) selectedMemberForGrantPreflight(sessionDir string, metadata *storage.SessionMetadata, request protocol.CommandRequest) (string, error) {
	payload := mapParam(request, "payload")
	if selected := strings.TrimSpace(firstNonEmpty(payloadString(payload, "member"), payloadString(payload, "to"))); selected != "" {
		return selected, nil
	}
	event, err := storage.BuildCouncilEvent(sessionDir, metadata, storage.CouncilEventSpec{
		Action:           "grant",
		Actor:            stringParam(request, "actor"),
		CommandID:        stringParam(request, "command_id"),
		CausationEventID: stringParam(request, "causation_event_id"),
		Payload:          payload,
		Now:              s.now(),
	})
	if err != nil {
		return "", err
	}
	selected := strings.TrimSpace(firstNonEmpty(payloadString(event.Payload, "member"), payloadString(event.Payload, "to")))
	if selected == "" && len(event.To) == 1 {
		selected = strings.TrimSpace(event.To[0])
	}
	if selected == "" {
		return "", storage.NewValidationError(storage.CategoryPrincipalInvalid, "member", "selected speaker must be resolved before readiness preflight")
	}
	return selected, nil
}

func (s *Server) selectedRunnerPrerequisite(sessionDir, memberID string) storage.SelectedRunnerPrerequisite {
	var blockers []string
	var evidence []string
	loaded, err := registry.LoadSnapshot(sessionDir, s.Runtime)
	if err != nil {
		blockers = append(blockers, "registry_snapshot_unavailable")
	} else {
		member, ok := loaded.Registry.Members[memberID]
		if !ok {
			blockers = append(blockers, "selected_member_missing_in_snapshot")
		} else {
			evidence = append(evidence, "registry_snapshot_member:"+member.ID)
			if !member.Enabled {
				blockers = append(blockers, "selected_member_not_enabled_in_snapshot")
			}
			if member.AdapterKind != runner.HermesAgentKind {
				blockers = append(blockers, "unsupported_runner_adapter_kind")
			}
			if resolvedWrapper(member) == "" {
				blockers = append(blockers, "resolved_wrapper_missing")
			}
		}
	}
	if _, err := s.selectedSpeakerAdapter(); err != nil {
		blockers = append(blockers, "runner_adapter_unavailable")
	} else {
		evidence = append(evidence, "runner_adapter:"+runner.HermesAgentKind)
	}
	if len(blockers) > 0 {
		return storage.SelectedRunnerPrerequisite{Ready: false, Status: "blocking", BlockingReasons: blockers, Evidence: evidence}
	}
	return storage.SelectedRunnerPrerequisite{Ready: true, Status: "ready", Evidence: evidence}
}

func (s *Server) appendSelectedRunnerPreflightDiagnostic(sessionDir string, metadata *storage.SessionMetadata, request protocol.CommandRequest, selected string, report *storage.ParticipantRuntimeReadinessReport) error {
	commandID := strings.TrimSpace(stringParam(request, "command_id"))
	if commandID == "" {
		commandID = fmt.Sprintf("cmd_selected_runner_preflight_%d_%s", s.now().UnixNano(), selected)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return err
	}
	phase := metadata.State.Phase
	if len(index.Events) > 0 {
		phase = index.Events[len(index.Events)-1].Phase
	}
	for _, event := range index.Events {
		if event.Type == "selected_runner_dispatch_failed" && event.CommandID == commandID && payloadString(event.Payload, "reason") == "selected_runner_preflight_failed" {
			return nil
		}
	}
	payload := map[string]any{
		"reason":                    "selected_runner_preflight_failed",
		"selected_member":           selected,
		"diagnostic_owner":          "control/RUNFIX-011",
		"diagnostic_path":           "internal/daemon/council_handlers.go",
		"participant_runtime_ready": false,
	}
	if report != nil {
		payload["blocking_reasons"] = append([]string(nil), report.BlockingReasons...)
		payload["participant_runtime_readiness"] = report
	}
	event := storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventIDFor(commandID, "selected_runner_dispatch_failed", 1, s.now()),
		CommandID:     commandID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         phase,
		Type:          "selected_runner_dispatch_failed",
		From:          "kkachi-agent-networkd",
		To:            []string{metadata.Moderator},
		CreatedAt:     s.now(),
		Payload:       payload,
	}
	_, err = storage.AppendEvent(sessionDir, metadata, event)
	return err
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
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
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, runner.ErrorClassStalePhaseEvidence, "selected_runner_dispatch_incomplete_stale", "internal/daemon/selected_speaker.go")
	}
	selected, err := selectedSpeakerMember(event)
	if err != nil {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "selected_member_mismatch", "internal/daemon/selected_speaker.go")
	}
	if selected == "" {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "selected_member_missing", "internal/daemon/selected_speaker.go")
	}
	if metadata.Surface != nil && metadata.Surface.Kind == "discord_thread" {
		prereq := s.selectedRunnerPrerequisite(sessionDir, selected)
		report, err := storage.ParticipantRuntimeReadinessFromLog(sessionDir, metadata, storage.ParticipantRuntimeReadinessOptions{
			RequireAttendance:      true,
			RequirePreparation:     true,
			RequireSelectedRunner:  true,
			SelectedMember:         selected,
			SelectedRunnerEvidence: map[string]storage.SelectedRunnerPrerequisite{selected: prereq},
			Now:                    s.now(),
		})
		if err != nil {
			return err
		}
		if !report.Ready {
			return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "participant_runtime_readiness_blocked", "internal/daemon/council_handlers.go", map[string]any{
				"diagnostic_owner":              "control/RUNFIX-011",
				"participant_runtime_ready":     false,
				"participant_runtime_readiness": report,
				"blocking_reasons":              append([]string(nil), report.BlockingReasons...),
			})
		}
	}
	loaded, err := registry.LoadSnapshot(sessionDir, s.Runtime)
	if err != nil {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "registry_snapshot_unavailable", registry.SnapshotFileName)
	}
	member, ok := loaded.Registry.Members[selected]
	if !ok || !member.Enabled {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "selected_member_not_enabled_in_snapshot", registry.SnapshotFileName)
	}
	adapter, err := s.selectedSpeakerAdapter()
	if err != nil {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "runner_adapter_unavailable", "internal/runner")
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
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "selected_runner_dispatch_failed", "internal/daemon/selected_speaker.go")
	}
	return nil
}

func (s *Server) selectedSpeakerAdapter() (runner.Adapter, error) {
	if s.RunnerAdapter != nil {
		if s.RunnerAdapter.Kind() != runner.HermesAgentKind {
			return nil, fmt.Errorf("unsupported runner adapter kind %q", s.RunnerAdapter.Kind())
		}
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

func (s *Server) appendSelectedSpeakerDispatchDiagnostic(sessionDir string, metadata *storage.SessionMetadata, speaker storage.EventEnvelope, errorClass, reason, path string, extra ...map[string]any) error {
	payload := map[string]any{
		"reason":           reason,
		"selected_member":  selectedMemberForDiagnostic(speaker),
		"diagnostic_owner": "control/RUNFIX-003",
		"diagnostic_path":  path,
	}
	for _, values := range extra {
		for key, value := range values {
			payload[key] = value
		}
	}
	if errorClass != "" {
		payload["error_class"] = errorClass
	}
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
		Payload:          payload,
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
