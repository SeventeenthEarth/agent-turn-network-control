package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"atn-control/internal/memberruntime"
	"atn-control/internal/protocol"
	"atn-control/internal/registry"
	"atn-control/internal/runner"
	"atn-control/internal/storage"
)

const (
	defaultSelectedSpeakerDispatchTimeout = 30 * time.Second
	defaultLiveVisibleMaxDiscussionTurns  = 15
	requiredLiveVisibleDispatchTimeoutSec = 120
)

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
	surface := surfaceParam(request)
	if err := validateCouncilNewVisibleSurface(request, surface); err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	requestContext := councilRequestContextParam(request)
	standardLiveVisibleDefaults := selectedRunnerTimeoutPolicyRequired(surface, requestContext)
	if standardLiveVisibleDefaults {
		if limits.MaxDiscussionTurns <= 0 {
			limits.MaxDiscussionTurns = defaultLiveVisibleMaxDiscussionTurns
		}
		if limits.DispatchTimeoutSec <= 0 {
			limits.DispatchTimeoutSec = requiredLiveVisibleDispatchTimeoutSec
		}
	}
	requestContext, timeoutEvidence, err := s.normalizeSelectedRunnerTimeoutRequestContext(surface, limits, requestContext)
	if err != nil {
		return protocol.ErrorResponse(request, daemonProtocolError(err))
	}
	turnMode := stringParam(request, "turn_mode")
	if standardLiveVisibleDefaults && turnMode == "" {
		turnMode = "relevance"
	}
	startSpec := storage.CouncilStartSpec{
		Session: storage.SessionSpec{
			ID:                            sessionID,
			SessionType:                   storage.SessionTypeCouncil,
			Title:                         stringParam(request, "title"),
			Moderator:                     moderator,
			Surface:                       surface,
			RequestContext:                requestContext,
			LinkedAuthority:               linkedAuthorityParam(request),
			SelectedRunnerTimeoutEvidence: timeoutEvidence,
			TurnMode:                      turnMode,
			Limits:                        limits,
			EventID:                       eventID,
			CommandID:                     commandID,
			CorrelationID:                 stringParam(request, "correlation_id"),
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
	if action == "grant" {
		return s.councilGrantAppendResponse(request, sessionDir, metadata, result, dedup)
	}
	return eventAppendResponse(request, result, dedup)
}

func (s *Server) councilGrantAppendResponse(request protocol.CommandRequest, sessionDir string, metadata *storage.SessionMetadata, result storage.AppendResult, dedup bool) protocol.CommandResponse {
	response := map[string]any{
		"cursor":         result.Cursor,
		"event_id":       result.EventID,
		"offset":         result.Offset,
		"deduplicated":   dedup,
		"append_status":  "accepted",
		"grant_event_id": result.EventID,
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		response["dispatch_status"] = "status_unavailable"
		response["followup_required"] = true
		response["status_error"] = err.Error()
		return protocol.SuccessResponse(request, response)
	}
	accounting := storage.SelectedRunnerAccountingFromIndex(metadata, index)
	for _, grant := range accounting.SelectedRunners {
		if grant.SelectedEventID != result.EventID {
			continue
		}
		response["selected_event_id"] = grant.SelectedEventID
		response["selected_member"] = grant.Member
		if grant.Turn > 0 {
			response["turn"] = grant.Turn
		}
		response["dispatch_status"] = grant.Status
		response["runner_status"] = grant.RunnerStatus
		response["speech_link_status"] = grant.SpeechLinkStatus
		response["followup_required"] = grant.FollowupRequired
		if len(grant.RunnerStartEventIDs) > 0 {
			response["runner_started_event_id"] = grant.RunnerStartEventIDs[0]
		}
		if len(grant.RunnerSucceededEventIDs) > 0 {
			response["runner_succeeded_event_id"] = grant.RunnerSucceededEventIDs[0]
		}
		if len(grant.TerminalFailureEventIDs) > 0 {
			response["runner_failure_event_id"] = grant.TerminalFailureEventIDs[0]
		}
		if len(grant.TerminalDiscardEventIDs) > 0 {
			response["runner_discard_event_id"] = grant.TerminalDiscardEventIDs[0]
		}
		if len(grant.DispatchFailureEventIDs) > 0 {
			response["dispatch_failure_event_id"] = grant.DispatchFailureEventIDs[0]
		}
		if len(grant.LinkedRunnerSpeechEventIDs) > 0 {
			response["linked_runner_speech_event_id"] = grant.LinkedRunnerSpeechEventIDs[0]
		}
		return protocol.SuccessResponse(request, response)
	}
	response["dispatch_status"] = "pending"
	response["runner_status"] = "pending"
	response["speech_link_status"] = "pending"
	response["followup_required"] = true
	return protocol.SuccessResponse(request, response)
}

func councilRequestContextParam(request protocol.CommandRequest) map[string]any {
	context := mapParam(request, "request_context")
	if len(context) == 0 {
		context = map[string]any{}
	}
	if value := stringParam(request, "source"); value != "" {
		context["source"] = value
	}
	for key, param := range map[string]string{
		"source":          "request_source",
		"override_reason": "override_reason",
	} {
		if value := stringParam(request, param); value != "" {
			context[key] = value
		}
	}
	if value := firstNonEmpty(
		stringParam(request, "requested_output_mode"),
		stringParam(request, "output_mode"),
		stringParam(request, "requested_output"),
		mapString(context, "requested_output_mode"),
		mapString(context, "output_mode"),
		mapString(context, "requested_output"),
	); value != "" {
		if normalized, supported := normalizeCouncilOutputMode(value); supported {
			context["requested_output_mode"] = normalized
		} else {
			context["requested_output_mode"] = value
		}
		delete(context, "output_mode")
		delete(context, "requested_output")
	}
	for key, param := range map[string]string{
		"visible_output_required":       "visible_output_required",
		"explicit_non_visible_override": "explicit_non_visible_override",
	} {
		if value, ok := request.Params[param].(bool); ok {
			context[key] = value
		}
	}
	if len(context) == 0 {
		return nil
	}
	return context
}

func councilActionFromCommand(command string) string {
	action := strings.TrimPrefix(command, "council.")
	return strings.ReplaceAll(action, "_", "-")
}

func validateCouncilNewVisibleSurface(request protocol.CommandRequest, surface *storage.Surface) error {
	context := mapParam(request, "request_context")
	if err := validateCouncilNewIntentFieldConsistency(request, context); err != nil {
		return err
	}
	requestSource := firstNonEmpty(
		stringParam(request, "request_source"),
		stringParam(request, "source"),
		mapString(context, "source"),
	)
	requestedOutputModeRaw := firstNonEmpty(
		stringParam(request, "requested_output_mode"),
		stringParam(request, "output_mode"),
		stringParam(request, "requested_output"),
		mapString(context, "requested_output_mode"),
		mapString(context, "output_mode"),
		mapString(context, "requested_output"),
	)
	requestedOutputMode, outputModeSupported := normalizeCouncilOutputMode(requestedOutputModeRaw)
	explicitNonVisibleOverride := boolParam(request, "explicit_non_visible_override") || mapBool(context, "explicit_non_visible_override")
	overrideReason := firstNonEmpty(stringParam(request, "override_reason"), mapString(context, "override_reason"))
	discordOrigin := strings.HasPrefix(requestSource, "discord")
	nonVisibleRequested := requestedOutputMode == "artifact_only" || requestedOutputMode == "daemon_cli_actor_speech" || requestedOutputMode == "activation_planning_only"
	overrideComplete := explicitNonVisibleOverride && strings.TrimSpace(overrideReason) != ""
	if strings.TrimSpace(requestedOutputModeRaw) != "" && !outputModeSupported {
		return storage.NewValidationError(
			storage.CategoryInvalidEnvelope,
			"request_context.requested_output_mode",
			"ATN council requested_output_mode must be live_visible_thread, artifact_only, daemon_cli_actor_speech, transcript/export-only, transcript_export_only, local-daemon-only, local_daemon_only, or activation_planning_only",
		)
	}
	if strings.TrimSpace(requestedOutputModeRaw) == "" {
		return storage.NewValidationError(
			storage.CategoryInvalidEnvelope,
			"request_context.requested_output_mode",
			"ATN council.new must declare live-visible requested_output_mode=live_visible_thread with a Discord surface, or a supported non-visible/local-daemon-only mode with explicit_non_visible_override and override_reason before council.new",
		)
	}
	if nonVisibleRequested && !overrideComplete {
		return storage.NewValidationError(
			storage.CategoryInvalidEnvelope,
			"request_context.explicit_non_visible_override",
			"ATN councils require live-visible output unless the user explicitly requested a supported non-visible/local-daemon-only mode with override_reason before council.new",
		)
	}
	visibleFlag := boolParam(request, "visible_output_required") || mapBool(context, "visible_output_required")
	visibleRequired := visibleFlag || requestedOutputMode == "live_visible_thread" || surface != nil || (discordOrigin && (!nonVisibleRequested || !overrideComplete))
	if !visibleRequired {
		return nil
	}
	if surface == nil {
		return storage.NewValidationError(
			storage.CategoryInvalidEnvelope,
			"surface",
			"Discord-origin or live-visible ATN council requires a Discord visible surface before council.new",
		)
	}
	if surface.Platform != "discord" {
		return storage.NewValidationError(
			storage.CategoryInvalidEnvelope,
			"surface.platform",
			"live-visible ATN council surface.platform must be discord",
		)
	}
	switch surface.Kind {
	case "discord_thread":
		if strings.TrimSpace(surface.ThreadID) == "" {
			return storage.NewValidationError(storage.CategoryInvalidEnvelope, "surface.thread_id", "discord_thread live-visible surface requires thread_id")
		}
	case "discord_channel", "discord_parent_channel":
		if strings.TrimSpace(surface.ChannelID) == "" {
			return storage.NewValidationError(storage.CategoryInvalidEnvelope, "surface.channel_id", "Discord parent-channel fallback surface requires channel_id")
		}
	default:
		return storage.NewValidationError(
			storage.CategoryInvalidEnvelope,
			"surface.kind",
			"live-visible ATN council surface.kind must be discord_thread or approved Discord channel fallback",
		)
	}
	return nil
}

type selectedRunnerTimeoutOverride struct {
	TimeoutSec    int
	ApprovalBasis string
}

type selectedSpeakerTimeoutResolution struct {
	Duration time.Duration
	Source   string
}

func (s *Server) normalizeSelectedRunnerTimeoutRequestContext(surface *storage.Surface, limits storage.Limits, context map[string]any) (map[string]any, *storage.SelectedRunnerTimeoutEvidence, error) {
	if !selectedRunnerTimeoutPolicyRequired(surface, context) {
		return context, nil, nil
	}
	configured := limits.DispatchTimeoutSec
	if configured <= 0 {
		return nil, nil, storage.NewValidationError(storage.CategoryInvalidEnvelope, "limits.dispatch_timeout_sec", "Discord live-visible selected-runner councils require configured limits.dispatch_timeout_sec")
	}
	override, present, err := selectedRunnerTimeoutOverrideFromContext(context)
	if err != nil {
		return nil, nil, err
	}
	approvedAlternative := false
	approvalBasis := ""
	if configured == requiredLiveVisibleDispatchTimeoutSec {
		if present {
			delete(context, "selected_runner_timeout_override")
		}
	} else {
		if !present {
			return nil, nil, storage.NewValidationError(storage.CategoryInvalidEnvelope, "request_context.selected_runner_timeout_override", "non-120 dispatch_timeout_sec for Discord live-visible selected-runner councils requires request_context.selected_runner_timeout_override")
		}
		if override.TimeoutSec != configured {
			return nil, nil, storage.NewValidationError(storage.CategoryInvalidEnvelope, "request_context.selected_runner_timeout_override.timeout_sec", "selected_runner_timeout_override.timeout_sec must match limits.dispatch_timeout_sec")
		}
		approvedAlternative = true
		approvalBasis = override.ApprovalBasis
		context["selected_runner_timeout_override"] = map[string]any{
			"timeout_sec":    override.TimeoutSec,
			"approval_basis": override.ApprovalBasis,
		}
	}
	resolution := s.selectedSpeakerTimeoutResolutionForLimits(limits)
	if resolution.Duration != time.Duration(configured)*time.Second {
		return nil, nil, storage.NewValidationError(storage.CategoryCommandConflict, "limits.dispatch_timeout_sec", "guarded live-visible selected-runner timeout must match the current daemon SelectedSpeakerTimeout override before council.new")
	}
	return context, &storage.SelectedRunnerTimeoutEvidence{
		PolicyRequired:       true,
		ConfiguredTimeoutSec: configured,
		EffectiveTimeoutSec:  configured,
		EffectiveSource:      resolution.Source,
		ApprovedAlternative:  approvedAlternative,
		ApprovalBasis:        approvalBasis,
		Compliant:            true,
	}, nil
}

func selectedRunnerTimeoutPolicyRequired(surface *storage.Surface, context map[string]any) bool {
	if surface == nil || strings.TrimSpace(surface.Platform) != "discord" {
		return false
	}
	requestedOutputMode := strings.TrimSpace(mapString(context, "requested_output_mode"))
	if requestedOutputMode == "" {
		return false
	}
	overrideComplete := mapBool(context, "explicit_non_visible_override") && strings.TrimSpace(mapString(context, "override_reason")) != ""
	if selectedRunnerNonVisibleRequested(requestedOutputMode) && overrideComplete {
		return false
	}
	return true
}

func selectedRunnerNonVisibleRequested(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "artifact_only", "daemon_cli_actor_speech", "activation_planning_only":
		return true
	default:
		return false
	}
}

func selectedRunnerTimeoutOverrideFromContext(context map[string]any) (selectedRunnerTimeoutOverride, bool, error) {
	if context == nil {
		return selectedRunnerTimeoutOverride{}, false, nil
	}
	raw, ok := context["selected_runner_timeout_override"]
	if !ok || raw == nil {
		return selectedRunnerTimeoutOverride{}, false, nil
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return selectedRunnerTimeoutOverride{}, true, storage.NewValidationError(storage.CategoryInvalidEnvelope, "request_context.selected_runner_timeout_override", "selected_runner_timeout_override must be an object")
	}
	timeoutSec, ok := mapPositiveInt(value, "timeout_sec")
	if !ok {
		return selectedRunnerTimeoutOverride{}, true, storage.NewValidationError(storage.CategoryInvalidEnvelope, "request_context.selected_runner_timeout_override.timeout_sec", "selected_runner_timeout_override.timeout_sec must be a positive integer")
	}
	approvalBasis := strings.TrimSpace(mapString(value, "approval_basis"))
	if approvalBasis == "" {
		return selectedRunnerTimeoutOverride{}, true, storage.NewValidationError(storage.CategoryInvalidEnvelope, "request_context.selected_runner_timeout_override.approval_basis", "selected_runner_timeout_override.approval_basis is required")
	}
	return selectedRunnerTimeoutOverride{TimeoutSec: timeoutSec, ApprovalBasis: approvalBasis}, true, nil
}

func mapPositiveInt(value map[string]any, key string) (int, bool) {
	if value == nil {
		return 0, false
	}
	raw, ok := value[key]
	if !ok {
		return 0, false
	}
	switch typed := raw.(type) {
	case int:
		return typed, typed > 0
	case int64:
		return int(typed), typed > 0
	case float64:
		if typed == float64(int(typed)) {
			return int(typed), typed > 0
		}
	}
	return 0, false
}

func normalizeCouncilOutputMode(mode string) (string, bool) {
	switch strings.TrimSpace(mode) {
	case "":
		return "", true
	case "live_visible_thread", "artifact_only", "daemon_cli_actor_speech", "activation_planning_only":
		return strings.TrimSpace(mode), true
	case "transcript/export-only", "transcript_export_only":
		return "artifact_only", true
	case "local-daemon-only", "local_daemon_only":
		return "activation_planning_only", true
	default:
		return strings.TrimSpace(mode), false
	}
}

func validateCouncilNewIntentFieldConsistency(request protocol.CommandRequest, context map[string]any) error {
	if err := validateCouncilNewOutputModeAliasConsistency(request, context); err != nil {
		return err
	}
	for key, param := range map[string]string{
		"override_reason": "override_reason",
	} {
		top := stringParam(request, param)
		nested := mapString(context, key)
		if top != "" && nested != "" && top != nested {
			return storage.NewValidationError(
				storage.CategoryInvalidEnvelope,
				"request_context."+key,
				"ATN council.new output-intent fields must not conflict between top-level params and request_context",
			)
		}
	}
	for key, param := range map[string]string{
		"visible_output_required":       "visible_output_required",
		"explicit_non_visible_override": "explicit_non_visible_override",
	} {
		if nestedRaw, ok := context[key]; ok {
			if _, nestedOK := nestedRaw.(bool); !nestedOK {
				return storage.NewValidationError(storage.CategoryInvalidEnvelope, "request_context."+key, "ATN council.new boolean output-intent fields must be true or false")
			}
		}
		topRaw, topExists := request.Params[param]
		if !topExists {
			continue
		}
		topBool, topOK := topRaw.(bool)
		if !topOK {
			return storage.NewValidationError(storage.CategoryInvalidEnvelope, param, "ATN council.new boolean output-intent fields must be true or false")
		}
		if nestedRaw, nestedExists := context[key]; nestedExists {
			nestedBool := nestedRaw.(bool)
			if topBool != nestedBool {
				return storage.NewValidationError(
					storage.CategoryInvalidEnvelope,
					"request_context."+key,
					"ATN council.new output-intent fields must not conflict between top-level params and request_context",
				)
			}
		}
	}
	return nil
}

func validateCouncilNewOutputModeAliasConsistency(request protocol.CommandRequest, context map[string]any) error {
	seen := map[string]string{}
	for _, value := range []string{
		stringParam(request, "requested_output_mode"),
		stringParam(request, "output_mode"),
		stringParam(request, "requested_output"),
		mapString(context, "requested_output_mode"),
		mapString(context, "output_mode"),
		mapString(context, "requested_output"),
	} {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized, supported := normalizeCouncilOutputMode(trimmed)
		if !supported {
			normalized = trimmed
		}
		seen[normalized] = trimmed
	}
	if len(seen) > 1 {
		return storage.NewValidationError(
			storage.CategoryInvalidEnvelope,
			"request_context.requested_output_mode",
			"ATN council.new output-mode aliases must not declare conflicting requested output modes",
		)
	}
	return nil
}

func mapString(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	if text, ok := value[key].(string); ok {
		return text
	}
	return ""
}

func mapBool(value map[string]any, key string) bool {
	if value == nil {
		return false
	}
	if flag, ok := value[key].(bool); ok {
		return flag
	}
	return false
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
		From:          "atn-controld",
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
	if timeoutEvidence, blocked := s.selectedRunnerTimeoutRuntimeEvidence(metadata); blocked {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "selected_runner_timeout_policy_blocked", "internal/daemon/council_handlers.go", map[string]any{
			"diagnostic_owner":                 "control/NEWFIX-005",
			"selected_runner_timeout_evidence": selectedRunnerTimeoutEvidencePayload(timeoutEvidence),
			"blocking_reasons":                 []string{"selected_runner_timeout_policy_mismatch"},
		})
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
	promptEnvelope, err := storage.BuildSelectedRunnerPromptEnvelope(&dispatchMetadata, index, event, member)
	if err != nil {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "selected_runner_prompt_context_build_failed", "internal/storage/selected_runner_prompt_evidence.go", map[string]any{
			"diagnostic_owner": "control/NEWFIX-001",
			"validation_error": err.Error(),
		})
	}
	if err := s.appendSelectedRunnerPromptEvidence(sessionDir, metadata, event, promptEnvelope.Evidence, index.Events); err != nil {
		return err
	}
	if promptEnvelope.Evidence.Result == "blocked" {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "selected_runner_context_missing", "internal/storage/selected_runner_prompt_evidence.go", map[string]any{
			"diagnostic_owner":                "control/NEWFIX-001",
			"selected_runner_prompt_evidence": selectedRunnerPromptEvidencePayload(promptEnvelope.Evidence),
			"blocking_reasons":                append([]string(nil), promptEnvelope.Evidence.MissingRequiredContext...),
		})
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
		PromptBuilder: func(storage.EventEnvelope, registry.Member) string {
			return promptEnvelope.Prompt
		},
	}
	frame := storage.StreamFrame{Cursor: result.Cursor, IsReplay: false, Event: event}
	if err := handler.Handle(ctx, frame); err != nil && !errors.Is(err, memberruntime.ErrDurableFailureRecorded) {
		return s.appendSelectedSpeakerDispatchDiagnostic(sessionDir, metadata, event, "", "selected_runner_dispatch_failed", "internal/daemon/selected_speaker.go")
	}
	return nil
}

func (s *Server) appendSelectedRunnerPromptEvidence(sessionDir string, metadata *storage.SessionMetadata, speaker storage.EventEnvelope, evidence storage.SelectedRunnerPromptEvidence, events []storage.EventEnvelope) error {
	if selectedRunnerPromptEvidenceRecorded(events, speaker.EventID) {
		return nil
	}
	event := storage.EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventIDFor("prompt_"+sha256Text(speaker.EventID)[:12], "selected_runner_prompt_evidence", 1, s.now()),
		CommandID:        speaker.CommandID,
		CausationEventID: speaker.EventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            speaker.Phase,
		Type:             "selected_runner_prompt_evidence",
		From:             "atn-controld",
		To:               []string{metadata.Moderator},
		CreatedAt:        s.now(),
		Payload:          selectedRunnerPromptEvidencePayload(evidence),
	}
	_, err := storage.AppendEvent(sessionDir, metadata, event)
	return err
}

func selectedRunnerPromptEvidenceRecorded(events []storage.EventEnvelope, speakerEventID string) bool {
	for _, event := range events {
		if event.Type == "selected_runner_prompt_evidence" && event.CausationEventID == speakerEventID {
			return true
		}
	}
	return false
}

func selectedRunnerPromptEvidencePayload(evidence storage.SelectedRunnerPromptEvidence) map[string]any {
	payload := map[string]any{
		"session_id":                evidence.SessionID,
		"speaker_selected_event_id": evidence.SpeakerSelectedEventID,
		"selected_member":           evidence.SelectedMember,
		"turn":                      evidence.Turn,
		"causation_event_id":        evidence.CausationEventID,
		"result":                    evidence.Result,
		"included_context":          append([]string(nil), evidence.IncludedContext...),
		"missing_required_context":  append([]string(nil), evidence.MissingRequiredContext...),
		"prompt_context_sha256":     evidence.PromptContextSHA256,
	}
	if len(evidence.AgendaSourceEventIDs) > 0 {
		payload["agenda_source_event_ids"] = append([]string(nil), evidence.AgendaSourceEventIDs...)
	}
	if len(evidence.PriorContextSourceEventIDs) > 0 {
		payload["prior_context_source_event_ids"] = append([]string(nil), evidence.PriorContextSourceEventIDs...)
	}
	if len(evidence.OwnHistorySourceEventIDs) > 0 {
		payload["own_history_source_event_ids"] = append([]string(nil), evidence.OwnHistorySourceEventIDs...)
	}
	if len(evidence.OwnLatestClaimSourceEventIDs) > 0 {
		payload["own_latest_claim_source_event_ids"] = append([]string(nil), evidence.OwnLatestClaimSourceEventIDs...)
	}
	if len(evidence.OwnClaimIndexSourceEventIDs) > 0 {
		payload["own_claim_index_source_event_ids"] = append([]string(nil), evidence.OwnClaimIndexSourceEventIDs...)
	}
	if evidence.RedactedPromptExcerpt != "" {
		payload["redacted_prompt_excerpt"] = evidence.RedactedPromptExcerpt
	}
	return payload
}

func selectedRunnerTimeoutEvidencePayload(evidence *storage.SelectedRunnerTimeoutEvidence) map[string]any {
	if evidence == nil {
		return nil
	}
	return map[string]any{
		"policy_required":        evidence.PolicyRequired,
		"configured_timeout_sec": evidence.ConfiguredTimeoutSec,
		"effective_timeout_sec":  evidence.EffectiveTimeoutSec,
		"effective_source":       evidence.EffectiveSource,
		"approved_alternative":   evidence.ApprovedAlternative,
		"approval_basis":         evidence.ApprovalBasis,
		"compliant":              evidence.Compliant,
	}
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

func (s *Server) selectedSpeakerTimeoutResolution(metadata *storage.SessionMetadata) selectedSpeakerTimeoutResolution {
	if metadata == nil {
		return s.selectedSpeakerTimeoutResolutionForLimits(storage.Limits{})
	}
	return s.selectedSpeakerTimeoutResolutionForLimits(metadata.Limits)
}

func (s *Server) selectedSpeakerTimeoutResolutionForLimits(limits storage.Limits) selectedSpeakerTimeoutResolution {
	if s.SelectedSpeakerTimeout > 0 {
		return selectedSpeakerTimeoutResolution{Duration: s.SelectedSpeakerTimeout, Source: "daemon_override"}
	}
	if limits.DispatchTimeoutSec > 0 {
		return selectedSpeakerTimeoutResolution{Duration: time.Duration(limits.DispatchTimeoutSec) * time.Second, Source: "session_limit"}
	}
	return selectedSpeakerTimeoutResolution{Duration: defaultSelectedSpeakerDispatchTimeout, Source: "default_fallback_30s"}
}

func (s *Server) selectedRunnerTimeoutRuntimeEvidence(metadata *storage.SessionMetadata) (*storage.SelectedRunnerTimeoutEvidence, bool) {
	if metadata == nil || metadata.SelectedRunnerTimeoutEvidence == nil || !metadata.SelectedRunnerTimeoutEvidence.PolicyRequired {
		return nil, false
	}
	resolution := s.selectedSpeakerTimeoutResolution(metadata)
	allowed := time.Duration(metadata.SelectedRunnerTimeoutEvidence.ConfiguredTimeoutSec) * time.Second
	copy := *metadata.SelectedRunnerTimeoutEvidence
	copy.EffectiveTimeoutSec = int(resolution.Duration / time.Second)
	copy.EffectiveSource = resolution.Source
	copy.Compliant = resolution.Duration == allowed
	return &copy, !copy.Compliant
}

func (s *Server) selectedSpeakerTimeout(metadata *storage.SessionMetadata) time.Duration {
	return s.selectedSpeakerTimeoutResolution(metadata).Duration
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
		From:             "atn-controld",
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
