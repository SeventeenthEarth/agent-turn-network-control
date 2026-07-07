package storage

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"atn-control/internal/protocol"
	"atn-control/internal/registry"
)

const daemonPrincipal = "atn-controld"

const defaultCouncilResponseWindowSec = 120

type CouncilStartSpec struct {
	Session SessionSpec
	Members []string
	Now     time.Time
}

type CouncilEventSpec struct {
	Action           string
	Actor            string
	CommandID        string
	CausationEventID string
	Payload          map[string]any
	Now              time.Time
}

func CreateCouncil(dataHome string, loaded *registry.LoadedRegistry, spec CouncilStartSpec, runtime registry.Runtime) (*SessionMetadata, []AppendResult, bool, error) {
	runtime = runtimeWithDefaults(runtime)
	session := spec.Session
	if session.SessionType == "" {
		session.SessionType = SessionTypeCouncil
	}
	if session.SessionType != SessionTypeCouncil {
		return nil, nil, false, NewValidationError(CategoryMetadataInvalid, "session_type", "council new requires council session_type")
	}
	moderator := strings.TrimSpace(session.Moderator)
	if moderator == "" {
		return nil, nil, false, NewValidationError(CategoryPrincipalInvalid, "moderator", "moderator is required")
	}
	members, err := canonicalCouncilMembers(moderator, spec.Members)
	if err != nil {
		return nil, nil, false, err
	}
	session.Moderator = moderator
	session.Participants = append([]string{moderator}, members...)
	if session.EventID == "" {
		session.EventID = eventIDFromCommand("evt_session_created", session.CommandID)
	}
	if session.CommandID == "" {
		session.CommandID = "cmd_council_new_" + session.ID
	}
	now := spec.Now.UTC()
	if now.IsZero() {
		now = runtime.Now().UTC()
	}

	if existingMetadata, existingDir, ok, err := existingCouncil(dataHome, session.ID); err != nil {
		return nil, nil, false, err
	} else if ok {
		index, err := ReadLogIndex(existingDir, existingMetadata)
		if err != nil {
			return nil, nil, false, err
		}
		created := sessionCreatedEvent(metadataFromSpec(session, loaded, existingMetadata.CreatedAt), session, existingMetadata.CreatedAt)
		if len(index.Events) >= 1 && commandEquivalent(index.Events[0], created) {
			return existingMetadata, []AppendResult{{Cursor: cursorFor(0, index.Events[0].EventID), Offset: 0, EventID: index.Events[0].EventID}}, true, nil
		}
		return nil, nil, false, NewValidationError(CategorySessionExists, existingDir, "session already exists with different council payload")
	}

	metadata, first, err := CreateSession(dataHome, loaded, session, runtimeWithNow(runtime, now))
	if err != nil {
		return nil, nil, false, err
	}
	return metadata, []AppendResult{first}, false, nil
}

func BuildCouncilEvent(sessionDir string, metadata *SessionMetadata, spec CouncilEventSpec) (EventEnvelope, error) {
	event, _, err := buildCouncilEvent(sessionDir, metadata, spec)
	return event, err
}

func RecordCouncilEvent(sessionDir string, metadata *SessionMetadata, spec CouncilEventSpec) (AppendResult, bool, error) {
	event, index, err := buildCouncilEvent(sessionDir, metadata, spec)
	if err != nil {
		return AppendResult{}, false, err
	}
	return appendIdempotentEvent(sessionDir, metadata, index, event)
}

func RecordCouncilResponseWindowTimeout(sessionDir string, metadata *SessionMetadata, turn int, now time.Time) ([]AppendResult, bool, error) {
	if metadata == nil {
		return nil, false, NewValidationError(CategoryMetadataInvalid, "session", "metadata is required")
	}
	if metadata.SessionType != SessionTypeCouncil {
		return nil, false, NewValidationError(CategoryMetadataInvalid, "session_type", "council timeout requires council session")
	}
	if turn <= 0 {
		return nil, false, NewValidationError(CategoryInvalidEnvelope, "turn", "positive turn is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, false, err
	}
	window, ok := latestResponseWindowForTurn(index, turn)
	if !ok {
		return nil, false, NewValidationError(CategoryCommandConflict, "response_window", "response window is not open for turn")
	}
	if !window.deadline.IsZero() && now.Before(window.deadline) {
		return nil, false, NewValidationError(CategoryCommandConflict, "response_window", "response window deadline has not elapsed")
	}
	accounting := responseWindowAccountingFromIndex(metadata, index, now, turn)
	missing, _ := accounting["missing_members"].([]string)
	if len(missing) == 0 {
		return nil, true, nil
	}
	results := make([]AppendResult, 0, len(missing))
	allDedup := true
	for _, member := range missing {
		commandID := fmt.Sprintf("cmd_response_window_timeout_%s_%d_%s", metadata.ID, turn, member)
		result, dedup, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{
			Action:    "drop",
			Actor:     daemonPrincipal,
			CommandID: commandID,
			Payload: map[string]any{
				"turn":               turn,
				"member":             member,
				"reason":             "response window timeout",
				"request_event_id":   window.eventID,
				"response_window_id": window.id,
				"auto":               true,
				"auto_reason":        "timeout",
			},
			Now: now,
		})
		if err != nil {
			return results, false, err
		}
		results = append(results, result)
		if !dedup {
			allDedup = false
		}
	}
	return results, allDedup, nil
}

func buildCouncilEvent(sessionDir string, metadata *SessionMetadata, spec CouncilEventSpec) (EventEnvelope, *LogIndex, error) {
	if metadata == nil {
		return EventEnvelope{}, nil, NewValidationError(CategoryMetadataInvalid, "session", "metadata is required")
	}
	if metadata.SessionType != SessionTypeCouncil {
		return EventEnvelope{}, nil, NewValidationError(CategoryMetadataInvalid, "session_type", "council command requires council session")
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return EventEnvelope{}, nil, err
	}
	current := latestPhase(metadata, index)
	action := strings.TrimSpace(spec.Action)
	if action == "" {
		return EventEnvelope{}, nil, NewValidationError(CategoryInvalidEnvelope, "action", "council action is required")
	}
	now := spec.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if strings.TrimSpace(spec.CommandID) == "" {
		spec.CommandID = fmt.Sprintf("cmd_council_%s_%d", strings.ReplaceAll(action, "-", "_"), now.UnixNano())
	}
	payload := clonePayload(spec.Payload)
	if payload == nil {
		payload = map[string]any{}
	}
	delete(payload, "closeout_diagnostics")
	if action == "drop" && strings.TrimSpace(spec.CausationEventID) == "" {
		spec.CausationEventID = strings.TrimSpace(payloadStringDefault(payload, "request_event_id", ""))
	}
	payload["session_type"] = string(metadata.SessionType)
	payload["title"] = metadata.Title
	payload["moderator"] = metadata.Moderator
	payload["participants"] = append([]string(nil), metadata.Participants...)
	if metadata.Surface != nil {
		payload["surface"] = metadata.Surface
	}
	if metadata.LinkedAuthority != nil {
		payload["linked_authority"] = metadata.LinkedAuthority
	}
	eventType, phase, actor, to, turn, err := councilTransition(metadata, index, current, action, spec, payload)
	if err != nil {
		return EventEnvelope{}, nil, err
	}
	event := EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventIDFromCommand("evt_"+eventType, spec.CommandID),
		CommandID:        spec.CommandID,
		CausationEventID: strings.TrimSpace(spec.CausationEventID),
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Turn:             turn,
		Phase:            phase,
		Type:             eventType,
		From:             actor,
		To:               to,
		CreatedAt:        now,
		Payload:          payload,
	}
	if event.CausationEventID != "" {
		if _, ok := eventByID(index, event.CausationEventID); !ok {
			return EventEnvelope{}, nil, NewValidationError(CategoryInvalidEnvelope, "causation_event_id", "causation event must reference this session")
		}
	}
	attachPersistentParticipantRuntimeEvidence(metadata, index, &event)
	return event, index, nil
}

func CouncilStatusFromLog(sessionDir string, metadata *SessionMetadata) (map[string]any, error) {
	return CouncilStatusFromLogAt(sessionDir, metadata, time.Now().UTC())
}

func CouncilStatusFromLogAt(sessionDir string, metadata *SessionMetadata, now time.Time) (map[string]any, error) {
	if metadata == nil {
		return nil, NewValidationError(CategoryMetadataInvalid, "session", "metadata is required")
	}
	if metadata.SessionType != SessionTypeCouncil {
		return nil, NewValidationError(CategoryMetadataInvalid, "session_type", "council status requires council session")
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, err
	}
	phase := latestPhase(metadata, index)
	status := map[string]any{
		"phase":           phase,
		"status":          statusFromPhase(phase),
		"current_turn":    currentCouncilTurn(index),
		"consensus_round": latestConsensusRound(index),
		"moderator":       metadata.Moderator,
		"members":         councilMembers(metadata),
		"attendance":      councilAttendanceStatus(metadata, index),
		"agenda":          councilAgendaStatus(index),
		"draft":           councilDraftStatus(index),
		"hand_raises":     councilHandRaiseStatus(index),
		"vote":            councilVoteStatus(metadata, index),
	}
	status["discussion_quality"] = councilDiscussionQualityStatus(metadata, index, phase)
	status["discussion_lifecycle"] = councilDiscussionLifecycle(metadata, index)
	status["response_window_accounting"] = responseWindowAccountingFromIndex(metadata, index, now.UTC(), currentCouncilTurn(index))
	selectedRunnerAccounting := SelectedRunnerAccountingFromIndex(metadata, index)
	status["selected_runner_accounting"] = selectedRunnerAccounting
	if evidence := LatestSelectedRunnerPromptEvidenceFromIndex(index); evidence != nil {
		status["selected_runner_prompt_evidence"] = *evidence
	}
	if evidence := selectedRunnerTimeoutEvidenceProjection(metadata); evidence != nil {
		status["selected_runner_timeout_evidence"] = *evidence
	}
	status["participant_runtime_readiness"] = ParticipantRuntimeReadinessFromIndex(metadata, index, readinessOptionsForStatus(metadata, index, now, selectedRunnerAccounting))
	if evidence := latestPersistentParticipantRuntimeEvidenceFromIndex(index); evidence != nil {
		status["persistent_participant_runtime_evidence"] = evidence
	}
	if diagnostics := closeoutDiagnosticsForStatus(metadata, index); len(diagnostics) > 0 {
		status["closeout_diagnostics"] = diagnostics
	}
	if len(index.Events) > 0 {
		last := index.Events[len(index.Events)-1]
		status["latest_event_id"] = last.EventID
		status["latest_cursor"] = cursorFor(int64(len(index.Events)-1), last.EventID)
	}
	if metadata.LinkedAuthority != nil {
		status["linked_authority"] = metadata.LinkedAuthority
	}
	if result := latestLinkedAuthorityResult(index); result != nil {
		status["linked_authority_result"] = result
	}
	return status, nil
}

// CouncilReconcilePrincipals validates council.new request shape before any persistent
// registry reconcile. It returns the exact moderator+member principals that may be
// reconciled. A matching existing council returns nil principals so idempotent
// duplicate requests do not mutate the registry.
func CouncilReconcilePrincipals(dataHome string, loaded *registry.LoadedRegistry, spec CouncilStartSpec, runtime registry.Runtime) ([]string, error) {
	if loaded == nil {
		return nil, NewValidationError(CategorySnapshotRequired, "registry", "loaded registry is required")
	}
	runtime = runtimeWithDefaults(runtime)
	session := spec.Session
	if session.SessionType == "" {
		session.SessionType = SessionTypeCouncil
	}
	if session.SessionType != SessionTypeCouncil {
		return nil, NewValidationError(CategoryMetadataInvalid, "session_type", "council new requires council session_type")
	}
	moderator := strings.TrimSpace(session.Moderator)
	if moderator == "" {
		return nil, NewValidationError(CategoryPrincipalInvalid, "moderator", "moderator is required")
	}
	members, err := canonicalCouncilMembers(moderator, spec.Members)
	if err != nil {
		return nil, err
	}
	session.Moderator = moderator
	session.Participants = append([]string{moderator}, members...)
	if session.EventID == "" {
		session.EventID = eventIDFromCommand("evt_session_created", session.CommandID)
	}
	if session.CommandID == "" {
		session.CommandID = "cmd_council_new_" + session.ID
	}

	preflightLoaded := cloneLoadedRegistryWithPrincipals(loaded, session.Participants)
	if err := validateSessionSpec(preflightLoaded, session); err != nil {
		return nil, err
	}
	if existingMetadata, existingDir, ok, err := existingCouncil(dataHome, session.ID); err != nil {
		return nil, err
	} else if ok {
		index, err := ReadLogIndex(existingDir, existingMetadata)
		if err != nil {
			return nil, err
		}
		created := sessionCreatedEvent(metadataFromSpec(session, preflightLoaded, existingMetadata.CreatedAt), session, existingMetadata.CreatedAt)
		if len(index.Events) >= 1 && commandEquivalent(index.Events[0], created) {
			return nil, nil
		}
		return nil, NewValidationError(CategorySessionExists, existingDir, "session already exists with different council payload")
	}
	if active, err := FindActiveSession(dataHome, runtime); err != nil {
		return nil, err
	} else if active != nil {
		return nil, NewValidationError(CategoryCommandConflict, "active_session", fmt.Sprintf("active session %s is %s", active.SessionID, active.Status))
	}
	return append([]string(nil), session.Participants...), nil
}

func cloneLoadedRegistryWithPrincipals(loaded *registry.LoadedRegistry, principals []string) *registry.LoadedRegistry {
	clone := *loaded
	clone.Registry.Members = make(map[string]registry.Member, len(loaded.Registry.Members)+len(principals))
	for id, member := range loaded.Registry.Members {
		clone.Registry.Members[id] = member
	}
	for _, principal := range principals {
		if _, ok := clone.Registry.Members[principal]; !ok {
			clone.Registry.Members[principal] = registry.Member{ID: principal}
		}
	}
	return &clone
}

func canonicalCouncilMembers(moderator string, raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, NewValidationError(CategoryPrincipalInvalid, "members", "at least one council member is required")
	}
	seen := map[string]struct{}{}
	members := make([]string, 0, len(raw))
	for _, value := range raw {
		member := strings.TrimSpace(value)
		if member == "" {
			return nil, NewValidationError(CategoryPrincipalInvalid, "members", "member id is required")
		}
		if isReservedNormalPrincipal(member) {
			return nil, NewValidationError(CategoryPrincipalInvalid, "members", "reserved principal cannot be a council member")
		}
		if member == moderator {
			return nil, NewValidationError(CategoryPrincipalInvalid, "members", "moderator must not be included in council members")
		}
		if _, ok := seen[member]; ok {
			return nil, NewValidationError(CategoryPrincipalInvalid, "members", "duplicate council member")
		}
		seen[member] = struct{}{}
		members = append(members, member)
	}
	if isReservedNormalPrincipal(moderator) {
		return nil, NewValidationError(CategoryPrincipalInvalid, "moderator", "reserved principal cannot be moderator")
	}
	return members, nil
}

func councilTransition(metadata *SessionMetadata, index *LogIndex, current Phase, action string, spec CouncilEventSpec, payload map[string]any) (string, Phase, string, []string, *int, error) {
	if statusFromPhase(current) == StatusTerminal {
		return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "phase", "council session is terminal")
	}
	actor := strings.TrimSpace(spec.Actor)
	if actor == "" {
		actor = metadata.Moderator
	}
	requirePhase := func(allowed ...Phase) error {
		for _, allowedPhase := range allowed {
			if current == allowedPhase {
				return nil
			}
		}
		return NewValidationError(CategoryCommandConflict, "phase", fmt.Sprintf("%s is not allowed from phase %s", action, current))
	}
	requireModerator := func() error {
		if actor != metadata.Moderator {
			return NewValidationError(CategoryPrincipalInvalid, "actor", "only the council moderator may perform this action")
		}
		return nil
	}
	requireMember := func() error {
		if !councilMember(metadata, actor) {
			return NewValidationError(CategoryPrincipalInvalid, "actor", "actor is not a council member")
		}
		return nil
	}

	var turnPtr *int
	switch action {
	case "request-attendance":
		if err := requirePhase("created"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		payload["required_members"] = councilMembers(metadata)
		fillSurfacePayload(metadata, payload)
		return "attendance_requested", "created", actor, councilMembers(metadata), nil, requirePayloadOptionalTimeout(payload)
	case "attend":
		if err := requirePhase("created"); err != nil {
			return "", "", "", nil, nil, err
		}
		if actor == daemonPrincipal {
			member := payloadStringDefault(payload, "member", "")
			if !councilMember(metadata, member) {
				return "", "", "", nil, nil, NewValidationError(CategoryPrincipalInvalid, "member", "timeout attendance member must be a council member")
			}
		} else if err := requireMember(); err != nil {
			return "", "", "", nil, nil, err
		}
		if !allowedString(payloadStringDefault(payload, "status", ""), "present", "partial", "unavailable", "no_response_timeout") {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "status", "invalid attendance status")
		}
		return "member_attended", "created", actor, []string{metadata.Moderator}, nil, nil
	case "lock-agenda":
		if err := requirePhase("created"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		for _, required := range []string{"decision_question", "success_criteria", "out_of_scope_policy"} {
			if strings.TrimSpace(payloadStringDefault(payload, required, "")) == "" {
				return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, required, required+" is required")
			}
		}
		return "agenda_locked", "created", actor, councilMembers(metadata), nil, nil
	case "prepare":
		if err := requirePhase("created"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := validateAttendanceAgendaGuard(metadata, index); err != nil {
			return "", "", "", nil, nil, err
		}
		if _, ok := payload["topic"]; !ok {
			payload["topic"] = metadata.Title
		}
		return "preparation_requested", "preparation", actor, councilMembers(metadata), nil, requirePayloadOptionalTimeout(payload)
	case "ready":
		if err := requirePhase("preparation"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireMember(); err != nil {
			return "", "", "", nil, nil, err
		}
		if strings.TrimSpace(payloadStringDefault(payload, "summary", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "summary", "summary is required")
		}
		return "member_ready", "preparation", actor, []string{metadata.Moderator}, nil, nil
	case "prepared-partial":
		if err := requirePhase("preparation"); err != nil {
			return "", "", "", nil, nil, err
		}
		if actor == daemonPrincipal {
			member := payloadStringDefault(payload, "member", "")
			if !councilMember(metadata, member) {
				return "", "", "", nil, nil, NewValidationError(CategoryPrincipalInvalid, "member", "partial preparation member must be a council member")
			}
		} else if err := requireMember(); err != nil {
			return "", "", "", nil, nil, err
		}
		if strings.TrimSpace(payloadStringDefault(payload, "reason", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "reason", "reason is required")
		}
		return "member_prepared_partial", "preparation", actor, []string{metadata.Moderator}, nil, nil
	case "poll":
		if err := requirePhase("preparation", "discussion"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		turn := positivePayloadInt(payload, "turn")
		if turn == 0 {
			turn = nextCouncilTurn(index)
		}
		payload["turn"] = turn
		openedAt := spec.Now.UTC()
		if openedAt.IsZero() {
			openedAt = time.Now().UTC()
		}
		deadlineAt := openedAt.Add(defaultCouncilResponseWindowSec * time.Second)
		payload["response_window_id"] = responseWindowID(metadata.ID, turn, eventIDFromCommand("evt_hand_raise_requested", spec.CommandID))
		payload["response_window_duration_sec"] = defaultCouncilResponseWindowSec
		payload["response_window_opened_at"] = openedAt.Format(time.RFC3339Nano)
		payload["response_window_deadline_at"] = deadlineAt.Format(time.RFC3339Nano)
		payload["required_members"] = councilMembers(metadata)
		turnPtr = intPtr(turn)
		return "hand_raise_requested", "discussion", actor, councilMembers(metadata), turnPtr, requirePayloadOptionalTimeout(payload)
	case "hand-raise":
		if err := requirePhase("discussion"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireMember(); err != nil {
			return "", "", "", nil, nil, err
		}
		turn, err := requireTurn(payload, currentCouncilTurn(index))
		if err != nil {
			return "", "", "", nil, nil, err
		}
		payload["turn"] = turn
		responseAt := spec.Now.UTC()
		if responseAt.IsZero() {
			responseAt = time.Now().UTC()
		}
		if !sameCommandEventType(index, spec.CommandID, "hand_raise") {
			if err := validateResponseWindowAccepts(index, actor, turn, responseAt); err != nil {
				return "", "", "", nil, nil, err
			}
		}
		if value, ok := payload["wants_to_speak"]; ok && value == false {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "wants_to_speak", "wants_to_speak=false is not a council drop; use council.drop")
		}
		if _, ok := payload["wants_to_speak"]; !ok {
			payload["wants_to_speak"] = true
		}
		if err := validateCouncilRaiseOrDropUnique(index, spec.CommandID, actor, turn, "hand_raise"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := validateArgumentGraphHandRaise(metadata, index, payload); err != nil {
			return "", "", "", nil, nil, err
		}
		turnPtr = intPtr(turn)
		return "hand_raise", "discussion", actor, []string{metadata.Moderator}, turnPtr, nil
	case "drop":
		if err := requirePhase("discussion"); err != nil {
			return "", "", "", nil, nil, err
		}
		member := actor
		autoDrop := anyBool(payload, "auto")
		if actor == daemonPrincipal && autoDrop {
			member = strings.TrimSpace(payloadStringDefault(payload, "member", ""))
			if !councilMember(metadata, member) {
				return "", "", "", nil, nil, NewValidationError(CategoryPrincipalInvalid, "member", "timeout drop member must be a council member")
			}
		} else {
			if err := requireMember(); err != nil {
				return "", "", "", nil, nil, err
			}
			if autoDrop {
				return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "auto", "auto=true council.drop is reserved for PRSLR-003 daemon timeout handling")
			}
		}
		turn, err := requireTurn(payload, currentCouncilTurn(index))
		if err != nil {
			return "", "", "", nil, nil, err
		}
		payload["turn"] = turn
		if strings.TrimSpace(payloadStringDefault(payload, "reason", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "reason", "drop reason is required")
		}
		requestEventID := strings.TrimSpace(payloadStringDefault(payload, "request_event_id", ""))
		if requestEventID == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "request_event_id", "drop requires matching hand_raise_requested event")
		}
		requestEvent, ok := eventByIDAndType(index, requestEventID, "hand_raise_requested")
		if !ok {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "request_event_id", "drop request_event_id must reference hand_raise_requested in this session")
		}
		if anyInt(requestEvent.Payload, "turn") != turn {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "request_event_id", "drop request_event_id must match turn")
		}
		if !autoDrop && !sameCommandEventType(index, spec.CommandID, "hand_raise_dropped") {
			responseAt := spec.Now.UTC()
			if responseAt.IsZero() {
				responseAt = time.Now().UTC()
			}
			if err := validateResponseWindowAccepts(index, member, turn, responseAt); err != nil {
				return "", "", "", nil, nil, err
			}
		}
		if err := validateCouncilRaiseOrDropUnique(index, spec.CommandID, member, turn, "hand_raise_dropped"); err != nil {
			return "", "", "", nil, nil, err
		}
		payload["member"] = member
		payload["wants_to_speak"] = false
		turnPtr = intPtr(turn)
		return "hand_raise_dropped", "discussion", actor, []string{metadata.Moderator}, turnPtr, nil
	case "grant":
		if err := requirePhase("discussion"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		payloadMember := strings.TrimSpace(payloadStringDefault(payload, "member", ""))
		payloadTo := strings.TrimSpace(payloadStringDefault(payload, "to", ""))
		if payloadMember != "" && payloadTo != "" && payloadMember != payloadTo {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "member", "payload.member must match payload.to for speaker grant")
		}
		member := firstNonEmptyString(payloadMember, payloadTo)
		if member == "" && anyBool(payload, "auto") {
			selection, err := autoCouncilSpeaker(metadata, index)
			if err != nil {
				return "", "", "", nil, nil, err
			}
			member = selection.member
			if strings.TrimSpace(payloadStringDefault(payload, "selection_mode", "")) == "" {
				payload["selection_mode"] = "relevance"
			}
			if strings.TrimSpace(payloadStringDefault(payload, "reason", "")) == "" && selection.reason != "" {
				payload["reason"] = selection.reason
			}
			payload["selection_score"] = selection.score
			if len(selection.need) > 0 {
				payload["graph_need"] = selection.need
			}
		}
		if !councilMember(metadata, member) {
			return "", "", "", nil, nil, NewValidationError(CategoryPrincipalInvalid, "member", "selected speaker must be a council member")
		}
		payload["member"] = member
		turn, err := requireTurn(payload, currentCouncilTurn(index))
		if err != nil {
			return "", "", "", nil, nil, err
		}
		payload["turn"] = turn
		mode := payloadStringDefault(payload, "selection_mode", "")
		if mode == "" {
			mode = defaultString(metadata.TurnMode, "moderator_direct")
			payload["selection_mode"] = mode
		}
		if !validTurnMode(mode) {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "selection_mode", "unsupported selection_mode")
		}
		if metadata.TurnMode != "" && mode != metadata.TurnMode && strings.TrimSpace(payloadStringDefault(payload, "reason", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "reason", "reason is required when selection_mode deviates from turn_mode")
		}
		delete(payload, "stance_assignment")
		stanceAssignment := strings.TrimSpace(selectedGrantStanceAssignment(index, member, turn))
		if stanceAssignment == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "stance_assignment", "speaker grant requires matching hand_raise intent or reason")
		}
		payload["stance_assignment"] = stanceAssignment
		turnPtr = intPtr(turn)
		return "speaker_selected", "discussion", actor, []string{member}, turnPtr, nil
	case "speak":
		if err := requirePhase("discussion"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireMember(); err != nil {
			return "", "", "", nil, nil, err
		}
		turn, err := requireTurn(payload, currentCouncilTurn(index))
		if err != nil {
			return "", "", "", nil, nil, err
		}
		if !speakerGranted(index, actor, turn) {
			return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "speaker_selected", "speaker must be selected before speech")
		}
		if strings.TrimSpace(payloadStringDefault(payload, "speech", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "speech", "speech is required")
		}
		if err := validateNoPostedSelectedRunnerVisibleEcho(index, actor, turn); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := validateArgumentGraphSpeech(metadata, index, actor, turn, payload); err != nil {
			return "", "", "", nil, nil, err
		}
		payload["turn"] = turn
		turnPtr = intPtr(turn)
		return "speech", "discussion", actor, participantsExcept(metadata, actor), turnPtr, nil
	case "intervene":
		if err := requirePhase("discussion"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		member := firstNonEmptyString(payloadStringDefault(payload, "member", ""), payloadStringDefault(payload, "to", ""))
		if !councilMember(metadata, member) {
			return "", "", "", nil, nil, NewValidationError(CategoryPrincipalInvalid, "member", "intervention target must be a council member")
		}
		if strings.TrimSpace(payloadStringDefault(payload, "reason", "")) == "" || strings.TrimSpace(payloadStringDefault(payload, "message", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "intervention", "reason and message are required")
		}
		payload["member"] = member
		return "moderator_intervention", "discussion", actor, []string{member}, nil, nil
	case "propose":
		if err := requirePhase("discussion"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		if strings.TrimSpace(payloadStringDefault(payload, "draft", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "draft", "draft is required")
		}
		if latestDraftVersion(index) != 0 {
			return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "draft_version", "initial proposal already exists")
		}
		if err := validateCouncilProposeLifecycle(metadata, index); err != nil {
			return "", "", "", nil, nil, err
		}
		payload["draft_version"] = 1
		return "draft_conclusion", "draft_conclusion", actor, councilMembers(metadata), nil, nil
	case "revise":
		if err := requirePhase("draft_conclusion", "consensus_vote"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		prior := latestDraftVersion(index)
		if prior == 0 {
			return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "draft_version", "revision requires an existing draft")
		}
		if strings.TrimSpace(payloadStringDefault(payload, "draft", "")) == "" || strings.TrimSpace(payloadStringDefault(payload, "revision_reason", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "revision", "draft and revision_reason are required")
		}
		payload["supersedes_draft_version"] = prior
		payload["draft_version"] = prior + 1
		return "draft_conclusion", "draft_conclusion", actor, councilMembers(metadata), nil, nil
	case "request-vote":
		if err := requirePhase("draft_conclusion"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		draft := positivePayloadInt(payload, "draft_version")
		if draft == 0 {
			draft = latestDraftVersion(index)
			payload["draft_version"] = draft
		}
		if draft == 0 || draft != latestDraftVersion(index) {
			return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "draft_version", "vote request requires current draft")
		}
		round := latestConsensusRound(index) + 1
		payload["consensus_round"] = round
		return "consensus_vote_requested", "consensus_vote", actor, councilMembers(metadata), nil, requirePayloadOptionalTimeout(payload)
	case "vote":
		if err := requirePhase("consensus_vote"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireMember(); err != nil {
			return "", "", "", nil, nil, err
		}
		draft, round, ok := currentVoteRequest(index)
		if !ok {
			return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "consensus_vote_requested", "vote requires an open vote request")
		}
		if requested := positivePayloadInt(payload, "draft_version"); requested != 0 && requested != draft {
			return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "draft_version", "vote draft_version does not match open vote")
		}
		if duplicateVote(index, actor, round, draft) {
			return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "vote", "member already voted in this consensus round")
		}
		if !allowedString(payloadStringDefault(payload, "vote", ""), "approve", "approve_with_conditions", "block", "abstain") {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "vote", "invalid vote")
		}
		if strings.TrimSpace(payloadStringDefault(payload, "reason", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "reason", "vote reason is required")
		}
		payload["draft_version"] = draft
		payload["consensus_round"] = round
		return "consensus_vote", "consensus_vote", actor, []string{metadata.Moderator}, nil, nil
	case "finalize":
		if err := requirePhase("consensus_vote"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := validateFinalizeReady(metadata, index, payload); err != nil {
			return "", "", "", nil, nil, err
		}
		return "council_finalized", "finalized", actor, councilMembers(metadata), nil, nil
	case "unresolved":
		if err := requirePhase("discussion", "draft_conclusion", "consensus_vote"); err != nil {
			return "", "", "", nil, nil, err
		}
		if err := requireModerator(); err != nil {
			return "", "", "", nil, nil, err
		}
		if strings.TrimSpace(payloadStringDefault(payload, "reason", "")) == "" {
			return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "reason", "unresolved reason is required")
		}
		if current == "consensus_vote" && !hasBlockVote(index) && !hasEvidence(payload) {
			return "", "", "", nil, nil, NewValidationError(CategoryCommandConflict, "evidence", "unresolved requires block vote or timeout evidence")
		}
		if diagnostics := unresolvedCloseoutDiagnostics(metadata, index, payload); len(diagnostics) > 0 {
			payload["closeout_diagnostics"] = diagnostics
		}
		return "council_unresolved", "unresolved", actor, councilMembers(metadata), nil, nil
	default:
		return "", "", "", nil, nil, NewValidationError(CategoryInvalidEnvelope, "action", "unsupported council action")
	}
}

func existingCouncil(dataHome, sessionID string) (*SessionMetadata, string, bool, error) {
	sessionDir, err := SessionDir(dataHome, sessionID)
	if err != nil {
		return nil, "", false, err
	}
	if info, err := os.Lstat(sessionDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, "", false, NewValidationError(CategorySessionUnsafe, sessionDir, "session path is unsafe")
		}
		metadata, err := LoadSessionYAML(sessionDir)
		return metadata, sessionDir, err == nil, err
	} else if !os.IsNotExist(err) {
		return nil, "", false, NewValidationError(CategorySessionUnsafe, sessionDir, err.Error())
	}
	return nil, "", false, nil
}

func runtimeWithNow(runtime registry.Runtime, now time.Time) registry.Runtime {
	runtime.Now = func() time.Time { return now }
	return runtime
}
func isReservedNormalPrincipal(principal string) bool {
	return principal == "user" || principal == daemonPrincipal
}
func councilMembers(metadata *SessionMetadata) []string {
	out := []string{}
	for _, p := range metadata.Participants {
		if p != metadata.Moderator {
			out = append(out, p)
		}
	}
	return out
}
func councilMember(metadata *SessionMetadata, member string) bool {
	for _, p := range councilMembers(metadata) {
		if p == member {
			return true
		}
	}
	return false
}
func validateNoPostedSelectedRunnerVisibleEcho(index *LogIndex, actor string, turn int) error {
	if index == nil || turn <= 0 || strings.TrimSpace(actor) == "" {
		return nil
	}
	for _, event := range index.Events {
		if event.Type != "speech" || strings.TrimSpace(event.From) != strings.TrimSpace(actor) || eventTurn(event) != turn {
			continue
		}
		if event.Runner == nil || strings.TrimSpace(event.Runner.InvocationID) == "" {
			continue
		}
		surfaceEvidence := anyMap(event.Payload, "surface_evidence")
		if strings.TrimSpace(anyString(surfaceEvidence, "status")) != "posted" {
			continue
		}
		if strings.TrimSpace(anyString(surfaceEvidence, "posting_path")) != "selected_member_profile_send" {
			continue
		}
		return NewValidationError(CategoryCommandConflict, "visible_delivery_echo", "posted selected-runner speech already exists for this member and turn; do not re-record the Discord-visible echo as council.speak")
	}
	return nil
}
func fillSurfacePayload(metadata *SessionMetadata, payload map[string]any) {
	if metadata.Surface != nil {
		payload["surface_kind"] = metadata.Surface.Kind
		if metadata.Surface.ThreadID != "" {
			payload["thread_id"] = metadata.Surface.ThreadID
		}
	}
}
func requirePayloadOptionalTimeout(payload map[string]any) error {
	if v := positivePayloadInt(payload, "timeout_sec"); v < 0 {
		return NewValidationError(CategoryInvalidEnvelope, "timeout_sec", "timeout_sec must be positive")
	}
	return nil
}
func intPtr(v int) *int { return &v }

func validateAttendanceAgendaGuard(metadata *SessionMetadata, index *LogIndex) error {
	if metadata.Surface == nil || metadata.Surface.Kind != "discord_thread" {
		return nil
	}
	seenRequest := false
	attended := map[string]bool{}
	agendaAfterAttendance := false
	for _, event := range index.Events {
		switch event.Type {
		case "attendance_requested":
			seenRequest = true
		case "member_attended":
			if !seenRequest {
				continue
			}
			member := payloadStringDefault(event.Payload, "member", "")
			if member == "" {
				member = event.From
			}
			if councilMember(metadata, member) && allowedString(payloadStringDefault(event.Payload, "status", ""), "present", "partial", "unavailable", "no_response_timeout") {
				attended[member] = true
			}
		case "agenda_locked":
			if seenRequest && allMembersMarked(metadata, attended) {
				agendaAfterAttendance = true
			}
		}
	}
	if !seenRequest {
		return NewValidationError(CategoryCommandConflict, "attendance_requested", "discord_thread council requires attendance_requested before prepare")
	}
	if !allMembersMarked(metadata, attended) {
		return NewValidationError(CategoryCommandConflict, "member_attended", "discord_thread council requires terminal attendance for all members: "+strings.Join(missingMembers(metadata, attended), ","))
	}
	if !agendaAfterAttendance {
		return NewValidationError(CategoryCommandConflict, "agenda_locked", "discord_thread council requires agenda_locked after attendance")
	}
	return nil
}

func allMembersMarked(metadata *SessionMetadata, marked map[string]bool) bool {
	return len(missingMembers(metadata, marked)) == 0
}
func missingMembers(metadata *SessionMetadata, marked map[string]bool) []string {
	missing := []string{}
	for _, m := range councilMembers(metadata) {
		if !marked[m] {
			missing = append(missing, m)
		}
	}
	return missing
}
func positivePayloadInt(payload map[string]any, key string) int { return anyInt(payload, key) }
func requireTurn(payload map[string]any, fallback int) (int, error) {
	turn := positivePayloadInt(payload, "turn")
	if turn == 0 {
		turn = fallback
	}
	if turn <= 0 {
		return 0, NewValidationError(CategoryInvalidEnvelope, "turn", "positive turn is required")
	}
	return turn, nil
}
func currentCouncilTurn(index *LogIndex) int {
	max := 0
	for _, e := range index.Events {
		if e.Turn != nil && *e.Turn > max {
			max = *e.Turn
		}
		if t := anyInt(e.Payload, "turn"); t > max {
			max = t
		}
	}
	return max
}
func nextCouncilTurn(index *LogIndex) int { return currentCouncilTurn(index) + 1 }

type responseWindowProjection struct {
	eventID         string
	id              string
	turn            int
	openedAt        time.Time
	deadline        time.Time
	durationSec     int
	requiredMembers []string
}

func responseWindowID(sessionID string, turn int, requestEventID string) string {
	return fmt.Sprintf("rw_%s_%d_%s", sessionID, turn, requestEventID)
}

func latestResponseWindowForTurn(index *LogIndex, turn int) (responseWindowProjection, bool) {
	if index == nil || turn <= 0 {
		return responseWindowProjection{}, false
	}
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != "hand_raise_requested" || anyInt(event.Payload, "turn") != turn {
			continue
		}
		openedAt := event.CreatedAt.UTC()
		if text := payloadStringDefault(event.Payload, "response_window_opened_at", ""); text != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
				openedAt = parsed.UTC()
			}
		}
		duration := anyInt(event.Payload, "response_window_duration_sec")
		if duration <= 0 {
			duration = defaultCouncilResponseWindowSec
		}
		deadline := openedAt.Add(time.Duration(duration) * time.Second)
		if text := payloadStringDefault(event.Payload, "response_window_deadline_at", ""); text != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
				deadline = parsed.UTC()
			}
		}
		required, ok := payloadStringSlice(event.Payload, "required_members")
		if !ok || len(required) == 0 {
			required = append([]string(nil), event.To...)
		}
		id := payloadStringDefault(event.Payload, "response_window_id", "")
		if id == "" {
			id = responseWindowID(event.SessionID, turn, event.EventID)
		}
		return responseWindowProjection{eventID: event.EventID, id: id, turn: turn, openedAt: openedAt, deadline: deadline, durationSec: duration, requiredMembers: required}, true
	}
	return responseWindowProjection{}, false
}

func validateResponseWindowAccepts(index *LogIndex, member string, turn int, responseAt time.Time) error {
	window, ok := latestResponseWindowForTurn(index, turn)
	if !ok || window.deadline.IsZero() {
		return nil
	}
	if !responseAt.Before(window.deadline) && !responseAt.Equal(window.deadline) {
		return NewValidationError(CategoryCommandConflict, "response_window", "response window closed; late response is not eligible")
	}
	accounting := responseWindowAccountingFromIndex(nil, index, responseAt, turn)
	if accounting["state"] == "closed" {
		return NewValidationError(CategoryCommandConflict, "response_window", "response window closed; late response is not eligible")
	}
	return nil
}

func responseWindowAccountingFromIndex(metadata *SessionMetadata, index *LogIndex, now time.Time, turn int) map[string]any {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	window, ok := latestResponseWindowForTurn(index, turn)
	if !ok {
		return map[string]any{"configured": false}
	}
	required := append([]string(nil), window.requiredMembers...)
	if metadata != nil && len(required) == 0 {
		required = councilMembers(metadata)
	}
	responded := map[string]map[string]any{}
	autoDropCount := 0
	for _, event := range index.Events {
		if (event.Type != "hand_raise" && event.Type != "hand_raise_dropped") || anyInt(event.Payload, "turn") != turn {
			continue
		}
		member := responseWindowResponseMember(event)
		if member == "" {
			continue
		}
		row := map[string]any{"member": member, "event_id": event.EventID, "type": event.Type, "created_at": event.CreatedAt.UTC().Format(time.RFC3339Nano)}
		if event.Type == "hand_raise_dropped" && anyBool(event.Payload, "auto") {
			row["auto"] = true
			row["auto_reason"] = payloadStringDefault(event.Payload, "auto_reason", "")
			autoDropCount++
		}
		responded[member] = row
	}
	missing := []string{}
	for _, member := range required {
		if _, ok := responded[member]; !ok {
			missing = append(missing, member)
		}
	}
	respondedRows := make([]map[string]any, 0, len(responded))
	for _, row := range responded {
		respondedRows = append(respondedRows, row)
	}
	sort.Slice(respondedRows, func(i, j int) bool { return respondedRows[i]["member"].(string) < respondedRows[j]["member"].(string) })
	state := "open"
	closedReason := ""
	if len(required) > 0 && len(missing) == 0 {
		state = "closed"
		if autoDropCount > 0 {
			closedReason = "timeout"
		} else {
			closedReason = "all_members_responded"
		}
	} else if !window.deadline.IsZero() && now.After(window.deadline) {
		state = "closed"
		closedReason = "timeout"
	}
	out := map[string]any{
		"configured":                    true,
		"turn":                          turn,
		"request_event_id":              window.eventID,
		"response_window_id":            window.id,
		"duration_sec":                  window.durationSec,
		"opened_at":                     window.openedAt.Format(time.RFC3339Nano),
		"deadline_at":                   window.deadline.Format(time.RFC3339Nano),
		"state":                         state,
		"closed_reason":                 closedReason,
		"required_members":              required,
		"required_count":                len(required),
		"responded_members":             respondedRows,
		"responded_count":               len(respondedRows),
		"missing_members":               missing,
		"missing_count":                 len(missing),
		"auto_drop_count":               autoDropCount,
		"participant_runtime_readiness": "not_claimed_prslr004_pending",
	}
	return out
}

func responseWindowResponseMember(event EventEnvelope) string {
	if event.Type == "hand_raise_dropped" {
		if member := strings.TrimSpace(payloadStringDefault(event.Payload, "member", "")); member != "" {
			return member
		}
	}
	return strings.TrimSpace(event.From)
}

func sameCommandEventType(index *LogIndex, commandID string, eventType string) bool {
	commandID = strings.TrimSpace(commandID)
	if index == nil || commandID == "" || strings.TrimSpace(eventType) == "" {
		return false
	}
	for _, event := range index.Events {
		if event.CommandID == commandID && event.Type == eventType {
			return true
		}
	}
	return false
}

func speakerGranted(index *LogIndex, member string, turn int) bool {
	for _, e := range index.Events {
		if e.Type == "speaker_selected" && anyInt(e.Payload, "turn") == turn && (payloadStringDefault(e.Payload, "member", "") == member || (len(e.To) == 1 && e.To[0] == member)) {
			return true
		}
	}
	return false
}
func selectedGrantStanceAssignment(index *LogIndex, member string, turn int) string {
	if index == nil || turn <= 0 || strings.TrimSpace(member) == "" {
		return ""
	}
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != "hand_raise" {
			continue
		}
		if strings.TrimSpace(event.From) != strings.TrimSpace(member) || anyInt(event.Payload, "turn") != turn {
			continue
		}
		if intent := strings.TrimSpace(payloadStringDefault(event.Payload, "intent", "")); intent != "" {
			return intent
		}
		return strings.TrimSpace(payloadStringDefault(event.Payload, "reason", ""))
	}
	return ""
}

func validateCouncilRaiseOrDropUnique(index *LogIndex, commandID, member string, turn int, eventType string) error {
	if index == nil || turn <= 0 || strings.TrimSpace(member) == "" {
		return nil
	}
	for _, event := range index.Events {
		if event.Type != "hand_raise" && event.Type != "hand_raise_dropped" {
			continue
		}
		if event.CommandID != "" && commandID != "" && event.CommandID == commandID && event.Type == eventType {
			continue
		}
		if strings.TrimSpace(event.From) != strings.TrimSpace(member) || anyInt(event.Payload, "turn") != turn {
			continue
		}
		if eventType == "hand_raise" && event.Type != "hand_raise_dropped" {
			continue
		}
		return NewValidationError(CategoryCommandConflict, "hand_raise_response", "duplicate_conflicting_response for member and turn")
	}
	return nil
}

func participantsExcept(metadata *SessionMetadata, except string) []string {
	out := []string{}
	for _, p := range metadata.Participants {
		if p != except {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
func latestDraftVersion(index *LogIndex) int {
	max := 0
	for _, e := range index.Events {
		if e.Type == "draft_conclusion" {
			if v := anyInt(e.Payload, "draft_version"); v > max {
				max = v
			}
		}
	}
	return max
}
func latestConsensusRound(index *LogIndex) int {
	max := 0
	for _, e := range index.Events {
		if v := anyInt(e.Payload, "consensus_round"); v > max {
			max = v
		}
	}
	return max
}
func currentVoteRequest(index *LogIndex) (int, int, bool) {
	for i := len(index.Events) - 1; i >= 0; i-- {
		e := index.Events[i]
		if e.Type == "consensus_vote_requested" {
			return anyInt(e.Payload, "draft_version"), anyInt(e.Payload, "consensus_round"), true
		}
		if e.Type == "draft_conclusion" {
			return 0, 0, false
		}
	}
	return 0, 0, false
}
func duplicateVote(index *LogIndex, member string, round, draft int) bool {
	for _, e := range index.Events {
		if e.Type == "consensus_vote" && e.From == member && anyInt(e.Payload, "consensus_round") == round && anyInt(e.Payload, "draft_version") == draft {
			return true
		}
	}
	return false
}
func validateFinalizeReady(metadata *SessionMetadata, index *LogIndex, payload map[string]any) error {
	diagnostics := lifecycleCloseoutDiagnostics(metadata, index, "finalization")
	draft, round, ok := currentVoteRequest(index)
	if !ok {
		return NewValidationError(CategoryCommandConflict, "consensus_vote_requested", "finalize requires an open vote request")
	}
	votes := map[string]string{}
	for _, e := range index.Events {
		if e.Type == "consensus_vote" && anyInt(e.Payload, "consensus_round") == round && anyInt(e.Payload, "draft_version") == draft {
			votes[e.From] = payloadStringDefault(e.Payload, "vote", "")
		}
	}
	for _, m := range councilMembers(metadata) {
		if votes[m] == "" {
			return NewValidationError(CategoryCommandConflict, "vote", "finalize requires a vote from every council member")
		}
		if votes[m] == "block" {
			return NewValidationError(CategoryCommandConflict, "vote", "blocking vote prevents finalization")
		}
	}
	if strings.TrimSpace(payloadStringDefault(payload, "final_summary", "")) == "" {
		return NewValidationError(CategoryInvalidEnvelope, "final_summary", "final_summary is required")
	}
	if err := validateVisibleCloseoutEvidenceEnvelope(metadata, payload); err != nil {
		return err
	}
	diagnostics = append(diagnostics, visibleCloseoutDiagnostics(metadata, payload, "finalization")...)
	if len(diagnostics) > 0 {
		return closeoutDiagnosticsError(diagnostics)
	}
	return validateLinkedAuthorityResult(metadata, payload)
}
func validateLinkedAuthorityResult(metadata *SessionMetadata, payload map[string]any) error {
	if metadata.LinkedAuthority == nil {
		return nil
	}
	result := anyMap(payload, "linked_authority_result")
	if result == nil {
		return NewValidationError(CategoryInvalidEnvelope, "linked_authority_result", "linked authority result is required")
	}
	status := anyString(result, "status")
	switch status {
	case "posted":
		if anyString(result, "kanban_comment_id") == "" && anyString(result, "vault_decision_note") == "" && len(anySlice(result, "evidence")) == 0 {
			return NewValidationError(CategoryInvalidEnvelope, "linked_authority_result", "posted result requires return evidence")
		}
		return nil
	case "failed":
		if anyString(result, "failure_reason") == "" {
			return NewValidationError(CategoryInvalidEnvelope, "linked_authority_result.failure_reason", "failed result requires failure_reason")
		}
		return nil
	case "pending_followup":
		if anyString(result, "followup_card_id") == "" && len(anySlice(result, "evidence")) == 0 {
			return NewValidationError(CategoryInvalidEnvelope, "linked_authority_result", "pending_followup requires followup evidence")
		}
		return nil
	default:
		return NewValidationError(CategoryInvalidEnvelope, "linked_authority_result.status", "invalid linked authority result status")
	}
}
func hasBlockVote(index *LogIndex) bool {
	for _, e := range index.Events {
		if e.Type == "consensus_vote" && payloadStringDefault(e.Payload, "vote", "") == "block" {
			return true
		}
	}
	return false
}
func hasEvidence(payload map[string]any) bool {
	return len(anySlice(payload, "evidence")) > 0 || strings.TrimSpace(payloadStringDefault(payload, "timeout_evidence", "")) != ""
}

func councilAttendanceStatus(metadata *SessionMetadata, index *LogIndex) map[string]any {
	rows := map[string]map[string]any{}
	requested := false
	required := map[string]bool{}
	for _, member := range councilMembers(metadata) {
		required[member] = true
		rows[member] = map[string]any{"member": member, "required": true, "status": "missing"}
	}
	for _, event := range index.Events {
		switch event.Type {
		case "attendance_requested":
			requested = true
			requestedMembers, ok := payloadStringSlice(event.Payload, "required_members")
			if !ok || len(requestedMembers) == 0 {
				requestedMembers = event.To
			}
			for _, member := range requestedMembers {
				if !councilMember(metadata, member) {
					continue
				}
				required[member] = true
				row := rows[member]
				if row == nil {
					row = map[string]any{"member": member}
					rows[member] = row
				}
				row["required"] = true
				row["status"] = "requested"
				row["attendance_requested_event_id"] = event.EventID
				row["requested_at"] = event.CreatedAt.UTC().Format(time.RFC3339Nano)
			}
		case "member_attended":
			member := payloadStringDefault(event.Payload, "member", "")
			if member == "" {
				member = event.From
			}
			if !councilMember(metadata, member) {
				continue
			}
			row := rows[member]
			if row == nil {
				row = map[string]any{"member": member, "required": true}
				rows[member] = row
			}
			row["status"] = payloadStringDefault(event.Payload, "attendance_status", payloadStringDefault(event.Payload, "status", ""))
			row["summary"] = payloadStringDefault(event.Payload, "attendance_summary", payloadStringDefault(event.Payload, "summary", ""))
			row["member_attended_event_id"] = event.EventID
			row["attended_at"] = event.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	members := councilMembers(metadata)
	attendanceRows := make([]map[string]any, 0, len(members))
	missing := []string{}
	for _, member := range members {
		row := rows[member]
		if row == nil {
			row = map[string]any{"member": member, "required": true, "status": "missing"}
		}
		if required[member] && !allowedString(payloadStringDefault(row, "status", ""), "present", "partial", "unavailable", "no_response_timeout") {
			missing = append(missing, member)
		}
		attendanceRows = append(attendanceRows, row)
	}
	return map[string]any{
		"requested":       requested,
		"complete":        requested && len(missing) == 0,
		"missing_members": missing,
		"members":         attendanceRows,
	}
}

func councilAgendaStatus(index *LogIndex) map[string]any {
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != "agenda_locked" {
			continue
		}
		return map[string]any{
			"event_id":          event.EventID,
			"locked_by":         event.From,
			"decision_question": payloadStringDefault(event.Payload, "decision_question", ""),
			"locked_at":         event.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	return map[string]any{"locked": false}
}

func councilDraftStatus(index *LogIndex) map[string]any {
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != "draft_conclusion" {
			continue
		}
		return map[string]any{
			"event_id":                     event.EventID,
			"draft_version":                anyInt(event.Payload, "draft_version"),
			"supersedes_draft_version":     anyInt(event.Payload, "supersedes_draft_version"),
			"revision_reason":              payloadStringDefault(event.Payload, "revision_reason", ""),
			"latest_draft_conclusion_at":   event.CreatedAt.UTC().Format(time.RFC3339Nano),
			"latest_draft_conclusion_from": event.From,
		}
	}
	return map[string]any{"draft_version": 0}
}

func councilHandRaiseStatus(index *LogIndex) []map[string]any {
	turn := currentCouncilTurn(index)
	rows := []map[string]any{}
	for _, event := range index.Events {
		if (event.Type != "hand_raise" && event.Type != "hand_raise_dropped") || anyInt(event.Payload, "turn") != turn {
			continue
		}
		status := "raised"
		if event.Type == "hand_raise_dropped" {
			status = "dropped"
		}
		member := responseWindowResponseMember(event)
		row := map[string]any{
			"event_id":       event.EventID,
			"turn":           turn,
			"member":         member,
			"status":         status,
			"wants_to_speak": anyBool(event.Payload, "wants_to_speak"),
			"intent":         payloadStringDefault(event.Payload, "intent", ""),
			"reason":         payloadStringDefault(event.Payload, "reason", ""),
			"created_at":     event.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		if event.Type == "hand_raise_dropped" {
			row["drop_event_id"] = event.EventID
			row["request_event_id"] = payloadStringDefault(event.Payload, "request_event_id", "")
			row["observed_cursor"] = payloadStringDefault(event.Payload, "observed_cursor", "")
			row["stance_continuity"] = payloadStringDefault(event.Payload, "stance_continuity", "")
			row["current_stance_summary"] = payloadStringDefault(event.Payload, "current_stance_summary", "")
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i]["member"].(string) < rows[j]["member"].(string)
	})
	return rows
}

func councilVoteStatus(metadata *SessionMetadata, index *LogIndex) map[string]any {
	draft, round, open := currentVoteRequest(index)
	votes := make([]map[string]any, 0, len(councilMembers(metadata)))
	seen := map[string]bool{}
	for _, event := range index.Events {
		if event.Type != "consensus_vote" || anyInt(event.Payload, "consensus_round") != round || anyInt(event.Payload, "draft_version") != draft {
			continue
		}
		seen[event.From] = true
		votes = append(votes, map[string]any{
			"event_id":        event.EventID,
			"member":          event.From,
			"vote":            payloadStringDefault(event.Payload, "vote", ""),
			"reason":          payloadStringDefault(event.Payload, "reason", ""),
			"required_change": payloadStringDefault(event.Payload, "required_change", ""),
			"consensus_round": round,
			"draft_version":   draft,
			"created_at":      event.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	sort.Slice(votes, func(i, j int) bool {
		return votes[i]["member"].(string) < votes[j]["member"].(string)
	})
	missing := []string{}
	if open {
		for _, member := range councilMembers(metadata) {
			if !seen[member] {
				missing = append(missing, member)
			}
		}
	}
	return map[string]any{
		"open":            open,
		"consensus_round": round,
		"draft_version":   draft,
		"votes":           votes,
		"missing_members": missing,
		"complete":        open && len(missing) == 0,
	}
}

func latestLinkedAuthorityResult(index *LogIndex) map[string]any {
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != "council_finalized" && event.Type != "council_unresolved" {
			continue
		}
		return anyMap(event.Payload, "linked_authority_result")
	}
	return nil
}

func latestTerminalCouncilEvent(index *LogIndex) *EventEnvelope {
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type == "council_finalized" || event.Type == "council_unresolved" {
			return &index.Events[i]
		}
	}
	return nil
}

func closeoutDiagnosticsForStatus(metadata *SessionMetadata, index *LogIndex) []map[string]any {
	if event := latestTerminalCouncilEvent(index); event != nil {
		if diagnostics := closeoutDiagnosticsFromPayload(event.Payload); len(diagnostics) > 0 {
			return diagnostics
		}
	}
	return lifecycleCloseoutDiagnostics(metadata, index, "pre_finalization")
}

func closeoutDiagnosticsFromPayload(payload map[string]any) []map[string]any {
	if payload == nil {
		return nil
	}
	value, ok := payload["closeout_diagnostics"]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []map[string]any:
		if len(typed) == 0 {
			return nil
		}
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			row, ok := item.(map[string]any)
			if ok {
				out = append(out, row)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func lifecycleCloseoutDiagnostics(metadata *SessionMetadata, index *LogIndex, stage string) []map[string]any {
	lifecycle := councilDiscussionLifecycle(metadata, index)
	if !lifecycle.Configured {
		return nil
	}
	diagnostics := make([]map[string]any, 0, 2+len(lifecycle.MissingDiscussionTurns))
	if lifecycle.OpeningEventID == "" {
		diagnostics = append(diagnostics, closeoutDiagnostic("missing_moderator_opening", stage, "moderator opening event is missing", nil, "", ""))
	}
	for _, turn := range lifecycle.MissingDiscussionTurns {
		diagnostics = append(diagnostics, closeoutDiagnostic("missing_discussion_turn", stage, fmt.Sprintf("turn=%d", turn), nil, "", ""))
	}
	if len(lifecycle.MissingCloseoutMembers) > 0 {
		diagnostics = append(diagnostics, closeoutDiagnostic("missing_participant_closeout", stage, "selected participant closeout speech is missing", append([]string(nil), lifecycle.MissingCloseoutMembers...), "", ""))
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return diagnostics
}

func unresolvedCloseoutDiagnostics(metadata *SessionMetadata, index *LogIndex, payload map[string]any) []map[string]any {
	diagnostics := lifecycleCloseoutDiagnostics(metadata, index, "unresolved")
	diagnostics = append(diagnostics, closeoutDiagnostic("missing_moderator_synthesis", "unresolved", "final_summary was not recorded before unresolved closeout", nil, "", ""))
	diagnostics = append(diagnostics, visibleCloseoutDiagnostics(metadata, payload, "unresolved")...)
	return diagnostics
}

func validateVisibleCloseoutEvidenceEnvelope(metadata *SessionMetadata, payload map[string]any) error {
	if metadata == nil || metadata.Surface == nil || metadata.Surface.Kind != "discord_thread" {
		return nil
	}
	raw, ok := payload["surface_evidence"]
	if !ok {
		return nil
	}
	surfaceEvidence, ok := asStringMap(raw)
	if !ok || len(surfaceEvidence) == 0 {
		return NewValidationError(CategoryInvalidEnvelope, "surface_evidence", "surface_evidence must be an object")
	}
	status := strings.TrimSpace(anyString(surfaceEvidence, "status"))
	if status == "" {
		return NewValidationError(CategoryInvalidEnvelope, "surface_evidence.status", "surface_evidence.status is required and must be one of posted, failed, pending_followup")
	}
	switch status {
	case "posted", "failed", "pending_followup":
		return nil
	default:
		return NewValidationError(CategoryInvalidEnvelope, "surface_evidence.status", "surface_evidence.status must be one of posted, failed, pending_followup")
	}
}

func hasVisibleCloseoutMessagePointer(payload map[string]any) bool {
	return hasAnyEvidencePointer(payload, "final_message_id", "message_id", "message_ref")
}

func visibleCloseoutDiagnostics(metadata *SessionMetadata, payload map[string]any, stage string) []map[string]any {
	if metadata == nil || metadata.Surface == nil || metadata.Surface.Kind != "discord_thread" {
		return nil
	}
	expectedThreadID := strings.TrimSpace(metadata.Surface.ThreadID)
	surfaceEvidence := anyMap(payload, "surface_evidence")
	if surfaceEvidence == nil {
		return []map[string]any{closeoutDiagnostic("missing_visible_closeout_proof", stage, "surface_evidence is missing", nil, expectedThreadID, "")}
	}
	diagnostics := []map[string]any{}
	rawStatus := strings.TrimSpace(anyString(surfaceEvidence, "status"))
	status, _ := visibleCloseoutSurfaceStatus(surfaceEvidence)
	if rawStatus == "posted" && !hasVisibleCloseoutMessagePointer(surfaceEvidence) {
		diagnostics = append(diagnostics, closeoutDiagnostic("missing_final_message_id", stage, "surface_evidence.final_message_id or accepted visible message equivalent is required for posted visible closeout proof", nil, expectedThreadID, strings.TrimSpace(anyString(surfaceEvidence, "thread_id"))))
	} else {
		switch status {
		case "posted":
			// Exact-thread binding still needs to be checked below.
		case "failed":
			diagnostics = append(diagnostics, closeoutDiagnostic("visible_closeout_failed", stage, firstNonEmptyString(strings.TrimSpace(anyString(surfaceEvidence, "failure_reason", "reason")), "visible closeout delivery failed"), nil, expectedThreadID, strings.TrimSpace(anyString(surfaceEvidence, "thread_id"))))
		case "pending_followup":
			diagnostics = append(diagnostics, closeoutDiagnostic("visible_closeout_pending_followup", stage, firstNonEmptyString(strings.TrimSpace(anyString(surfaceEvidence, "reason")), strings.TrimSpace(anyString(surfaceEvidence, "followup_card_id")), "visible closeout delivery is pending follow-up"), nil, expectedThreadID, strings.TrimSpace(anyString(surfaceEvidence, "thread_id"))))
		default:
			diagnostics = append(diagnostics, closeoutDiagnostic("missing_visible_closeout_proof", stage, "visible closeout evidence is missing or unproven", nil, expectedThreadID, strings.TrimSpace(anyString(surfaceEvidence, "thread_id"))))
		}
	}
	if expectedThreadID != "" {
		observedThreadID := strings.TrimSpace(anyString(surfaceEvidence, "thread_id"))
		if observedThreadID == "" {
			diagnostics = append(diagnostics, closeoutDiagnostic("missing_thread_binding", stage, "surface_evidence.thread_id is required for exact-thread proof", nil, expectedThreadID, observedThreadID))
		} else if observedThreadID != expectedThreadID {
			diagnostics = append(diagnostics, closeoutDiagnostic("exact_thread_mismatch", stage, "visible closeout thread does not match the configured council thread", nil, expectedThreadID, observedThreadID))
		}
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return diagnostics
}

func closeoutDiagnosticsError(diagnostics []map[string]any) error {
	return NewValidationError(CategoryCommandConflict, "closeout_diagnostics", "closeout requirements not satisfied: "+mustCompactJSON(diagnostics))
}

func closeoutDiagnostic(code, stage, reason string, memberIDs []string, expectedThreadID, observedThreadID string) map[string]any {
	row := map[string]any{"code": code}
	if strings.TrimSpace(stage) != "" {
		row["stage"] = strings.TrimSpace(stage)
	}
	if strings.TrimSpace(reason) != "" {
		row["reason"] = strings.TrimSpace(reason)
	}
	if len(memberIDs) > 0 {
		copied := append([]string(nil), memberIDs...)
		sort.Strings(copied)
		row["member_ids"] = copied
	}
	if strings.TrimSpace(expectedThreadID) != "" {
		row["expected_thread_id"] = strings.TrimSpace(expectedThreadID)
	}
	if strings.TrimSpace(observedThreadID) != "" {
		row["observed_thread_id"] = strings.TrimSpace(observedThreadID)
	}
	return row
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
