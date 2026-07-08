package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SeventeenthEarth/agent-turn-network-control/internal/protocol"
	"github.com/SeventeenthEarth/agent-turn-network-control/internal/registry"
)

type DelegationStartSpec struct {
	Session           SessionSpec
	Assignee          string
	Task              string
	Context           string
	Acceptance        []string
	ExpectedOutputs   []string
	AssignmentEventID string
	Now               time.Time
}

type DelegationEventSpec struct {
	Action              string
	Actor               string
	Recipients          []string
	CommandID           string
	CausationEventID    string
	Payload             map[string]any
	ArtifactSourcePaths []string
	Now                 time.Time
}

type SessionCancelSpec struct {
	Actor     string
	Reason    string
	Cause     string
	CommandID string
	Now       time.Time
}

type SessionBlockSpec struct {
	Actor     string
	Category  string
	Reason    string
	CommandID string
	Now       time.Time
}

type SessionResumeSpec struct {
	Actor          string
	BlockedEventID string
	Reason         string
	CommandID      string
	Now            time.Time
}

type LimitsExtendSpec struct {
	Actor          string
	AuthorizedBy   string
	BlockedEventID string
	Changes        map[string]any
	Reason         string
	CommandID      string
	Now            time.Time
}

type EscalationBatchSummary struct {
	BatchID       string `json:"batch_id"`
	PendingCount  int    `json:"pending_count"`
	FirstEventID  string `json:"first_event_id"`
	DeadlineAt    string `json:"deadline_at,omitempty"`
	PhaseAtBatch  string `json:"phase_at_batch,omitempty"`
	CanFlush      bool   `json:"can_flush"`
	LatestEventID string `json:"latest_event_id,omitempty"`
}

func CreateDelegation(dataHome string, loaded *registry.LoadedRegistry, spec DelegationStartSpec, runtime registry.Runtime) (*SessionMetadata, []AppendResult, bool, error) {
	if spec.Session.SessionType == "" {
		spec.Session.SessionType = SessionTypeDelegation
	}
	if spec.Session.SessionType != SessionTypeDelegation {
		return nil, nil, false, NewValidationError(CategoryMetadataInvalid, "session_type", "delegate new requires delegation session_type")
	}
	if strings.TrimSpace(spec.Assignee) == "" {
		return nil, nil, false, NewValidationError(CategoryPrincipalInvalid, "assignee", "assignee is required")
	}
	if strings.TrimSpace(spec.Task) == "" {
		return nil, nil, false, NewValidationError(CategoryInvalidEnvelope, "task", "task is required")
	}
	if !containsString(spec.Session.Participants, spec.Assignee) {
		spec.Session.Participants = append(spec.Session.Participants, spec.Assignee)
	}
	if spec.AssignmentEventID == "" {
		spec.AssignmentEventID = eventIDFromCommand("evt_task_assigned", spec.Session.CommandID)
	}
	now := spec.Now.UTC()
	if now.IsZero() {
		now = runtimeWithDefaults(runtime).Now().UTC()
	}

	if existingMetadata, existingDir, ok, err := existingDelegation(dataHome, spec.Session.ID); err != nil {
		return nil, nil, false, err
	} else if ok {
		index, err := ReadLogIndex(existingDir, existingMetadata)
		if err != nil {
			return nil, nil, false, err
		}
		assignment := taskAssignedEvent(existingMetadata, spec, now)
		if len(index.Events) >= 2 && commandEquivalent(index.Events[1], assignment) {
			return existingMetadata, []AppendResult{
				{Cursor: cursorFor(0, index.Events[0].EventID), Offset: 0, EventID: index.Events[0].EventID},
				{Cursor: cursorFor(1, index.Events[1].EventID), Offset: 1, EventID: index.Events[1].EventID},
			}, true, nil
		}
		return nil, nil, false, NewValidationError(CategorySessionExists, existingDir, "session already exists with different delegation payload")
	}

	metadata, first, err := CreateSession(dataHome, loaded, spec.Session, runtime)
	if err != nil {
		return nil, nil, false, err
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		return nil, nil, false, err
	}
	second, err := AppendEvent(sessionDir, metadata, taskAssignedEvent(metadata, spec, now))
	if err != nil {
		return nil, nil, false, err
	}
	return metadata, []AppendResult{first, second}, false, nil
}

func RecordDelegationEvent(sessionDir string, metadata *SessionMetadata, spec DelegationEventSpec) (AppendResult, bool, error) {
	if metadata == nil {
		return AppendResult{}, false, NewValidationError(CategoryMetadataInvalid, "session", "metadata is required")
	}
	if metadata.SessionType != SessionTypeDelegation {
		return AppendResult{}, false, NewValidationError(CategoryMetadataInvalid, "session_type", "delegation command requires delegation session")
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return AppendResult{}, false, err
	}
	current := latestPhase(metadata, index)
	action := strings.TrimSpace(spec.Action)
	if action == "" {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "action", "delegation action is required")
	}
	if strings.TrimSpace(spec.CommandID) == "" {
		spec.CommandID = fmt.Sprintf("cmd_delegate_%s_%d", action, spec.Now.UTC().UnixNano())
	}
	now := spec.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	payload := clonePayload(spec.Payload)
	if payload == nil {
		payload = map[string]any{}
	}
	if result, deduplicated, handled, err := replayExistingDelegationCommand(metadata, index, action, spec, payload); handled {
		return result, deduplicated, err
	}
	if statusFromPhase(current) == StatusTerminal {
		return AppendResult{}, false, NewValidationError(CategoryCommandConflict, "phase", "delegation session is terminal")
	}
	eventType, phase, from, to, err := delegationTransition(metadata, index, current, action, spec, payload)
	if err != nil {
		return AppendResult{}, false, err
	}
	if len(spec.ArtifactSourcePaths) > 0 {
		artifacts, err := ingestArtifacts(sessionDir, metadata, spec.Actor, spec.ArtifactSourcePaths, now)
		if err != nil {
			return AppendResult{}, false, err
		}
		payload["artifacts"] = artifacts
	}
	if err := validateDelegationPayload(action, payload); err != nil {
		return AppendResult{}, false, err
	}
	event := EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventIDFromCommand("evt_"+eventType, spec.CommandID),
		CommandID:        spec.CommandID,
		CausationEventID: strings.TrimSpace(spec.CausationEventID),
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            phase,
		Type:             eventType,
		From:             from,
		To:               to,
		CreatedAt:        now,
		Payload:          payload,
	}
	if event.CausationEventID != "" {
		if _, ok := eventByID(index, event.CausationEventID); !ok {
			return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "causation_event_id", "causation event must reference this session")
		}
	}
	sort.Strings(event.To)
	return appendIdempotentEvent(sessionDir, metadata, index, event)
}

// replayExistingDelegationCommand handles exact command-id retries before the
// current phase gate rejects them. Artifact-bearing writes still go through the
// normal append path so file-copy side effects are not replayed implicitly.
func replayExistingDelegationCommand(metadata *SessionMetadata, index *LogIndex, action string, spec DelegationEventSpec, payload map[string]any) (AppendResult, bool, bool, error) {
	if strings.TrimSpace(spec.CommandID) == "" || len(spec.ArtifactSourcePaths) > 0 {
		return AppendResult{}, false, false, nil
	}
	for offset, existing := range index.Events {
		if existing.CommandID != spec.CommandID {
			continue
		}
		priorPhase := metadata.State.Phase
		if offset > 0 {
			priorPhase = index.Events[offset-1].Phase
		}
		replayPayload := clonePayload(payload)
		if replayPayload == nil {
			replayPayload = map[string]any{}
		}
		eventType, phase, from, to, err := delegationTransition(metadata, index, priorPhase, action, spec, replayPayload)
		if err != nil {
			return AppendResult{}, false, true, NewValidationError(CategoryCommandConflict, "command_id", "command_id already used with different payload")
		}
		if err := validateDelegationPayload(action, replayPayload); err != nil {
			return AppendResult{}, false, true, NewValidationError(CategoryCommandConflict, "command_id", "command_id already used with different payload")
		}
		event := EventEnvelope{
			SchemaVersion:    protocol.SchemaVersion,
			EventID:          eventIDFromCommand("evt_"+eventType, spec.CommandID),
			CommandID:        spec.CommandID,
			CausationEventID: strings.TrimSpace(spec.CausationEventID),
			CorrelationID:    metadata.ID,
			SessionID:        metadata.ID,
			SessionType:      metadata.SessionType,
			Phase:            phase,
			Type:             eventType,
			From:             from,
			To:               to,
			Payload:          replayPayload,
		}
		sort.Strings(event.To)
		if commandEquivalent(existing, event) {
			return AppendResult{Cursor: cursorFor(int64(offset), existing.EventID), Offset: int64(offset), EventID: existing.EventID}, true, true, nil
		}
		return AppendResult{}, false, true, NewValidationError(CategoryCommandConflict, "command_id", "command_id already used with different payload")
	}
	return AppendResult{}, false, false, nil
}

func CancelSession(sessionDir string, metadata *SessionMetadata, spec SessionCancelSpec) (AppendResult, bool, error) {
	index, current, err := mutableSessionIndex(sessionDir, metadata)
	if err != nil {
		return AppendResult{}, false, err
	}
	if !Participant(metadata, spec.Actor) || spec.Actor != metadata.Moderator {
		return AppendResult{}, false, NewValidationError(CategoryPrincipalInvalid, "actor", "only the moderator may cancel the session")
	}
	if strings.TrimSpace(spec.Reason) == "" {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "reason", "cancel reason is required")
	}
	now := spec.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if strings.TrimSpace(spec.CommandID) == "" {
		spec.CommandID = fmt.Sprintf("cmd_cancel_%d", now.UnixNano())
	}
	payload := map[string]any{"reason": strings.TrimSpace(spec.Reason)}
	if strings.TrimSpace(spec.Cause) != "" {
		payload["cause"] = strings.TrimSpace(spec.Cause)
	}
	event := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventIDFromCommand("evt_session_cancelled", spec.CommandID),
		CommandID:     spec.CommandID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "cancelled",
		Type:          "session_cancelled",
		From:          spec.Actor,
		To:            allParticipants(metadata),
		CreatedAt:     now,
		Payload:       payload,
	}
	for offset, existing := range index.Events {
		if existing.CommandID != event.CommandID {
			continue
		}
		if commandEquivalent(existing, event) {
			return AppendResult{Cursor: cursorFor(int64(offset), existing.EventID), Offset: int64(offset), EventID: existing.EventID}, true, nil
		}
		return AppendResult{}, false, NewValidationError(CategoryCommandConflict, "command_id", "command_id already used with different payload")
	}
	if statusFromPhase(current) == StatusTerminal {
		return AppendResult{}, false, NewValidationError(CategoryCommandConflict, "phase", "cannot cancel terminal session")
	}
	return appendIdempotentEvent(sessionDir, metadata, index, event)
}

func BlockSession(sessionDir string, metadata *SessionMetadata, spec SessionBlockSpec) (AppendResult, bool, error) {
	index, current, err := mutableSessionIndex(sessionDir, metadata)
	if err != nil {
		return AppendResult{}, false, err
	}
	if statusFromPhase(current) == StatusTerminal || current == "blocked" {
		return AppendResult{}, false, NewValidationError(CategoryCommandConflict, "phase", "cannot block terminal or already blocked session")
	}
	if spec.Actor != metadata.Moderator {
		return AppendResult{}, false, NewValidationError(CategoryPrincipalInvalid, "actor", "only the moderator may block the session")
	}
	if !allowedBlockCategory(spec.Category) {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "category", "invalid block category")
	}
	if strings.TrimSpace(spec.Reason) == "" {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "reason", "block reason is required")
	}
	now := spec.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if strings.TrimSpace(spec.CommandID) == "" {
		spec.CommandID = fmt.Sprintf("cmd_block_%d", now.UnixNano())
	}
	payload := map[string]any{"category": spec.Category, "reason": spec.Reason, "prior_phase": string(current), "resume_phase": string(current)}
	event := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventIDFromCommand("evt_session_blocked", spec.CommandID),
		CommandID:     spec.CommandID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "blocked",
		Type:          "session_blocked",
		From:          spec.Actor,
		To:            allParticipants(metadata),
		CreatedAt:     now,
		Payload:       payload,
	}
	return appendIdempotentEvent(sessionDir, metadata, index, event)
}

func ResumeSession(sessionDir string, metadata *SessionMetadata, spec SessionResumeSpec) (AppendResult, bool, error) {
	index, current, err := mutableSessionIndex(sessionDir, metadata)
	if err != nil {
		return AppendResult{}, false, err
	}
	if current != "blocked" {
		return AppendResult{}, false, NewValidationError(CategoryCommandConflict, "phase", "session is not blocked")
	}
	if spec.Actor != metadata.Moderator {
		return AppendResult{}, false, NewValidationError(CategoryPrincipalInvalid, "actor", "only the moderator may resume the session")
	}
	blocking, ok := eventByID(index, spec.BlockedEventID)
	if !ok || blocking.Phase != "blocked" {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "blocked_event", "blocked-event must reference the blocking event for this session")
	}
	if blocking.Type == "session_budget_exceeded" {
		return AppendResult{}, false, NewValidationError(CategoryCommandConflict, "blocked_event", "budget/limit blocks require limits extend")
	}
	if blocking.Type != "session_blocked" && blocking.Type != "security_violation" && blocking.Type != "escalation_timeout" {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "blocked_event", "blocked-event is not manually resumable")
	}
	resumePhase := Phase(payloadStringDefault(blocking.Payload, "resume_phase", ""))
	if resumePhase == "" || !validPhase(metadata.SessionType, resumePhase) || statusFromPhase(resumePhase) == StatusTerminal || resumePhase == "blocked" {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "resume_phase", "blocking event has invalid resume_phase")
	}
	if strings.TrimSpace(spec.Reason) == "" {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "reason", "resume reason is required")
	}
	now := spec.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if strings.TrimSpace(spec.CommandID) == "" {
		spec.CommandID = fmt.Sprintf("cmd_resume_%d", now.UnixNano())
	}
	payload := map[string]any{"blocked_event_id": spec.BlockedEventID, "reason": spec.Reason, "resume_phase": string(resumePhase), "resolved_by": spec.Actor}
	event := EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventIDFromCommand("evt_session_resumed", spec.CommandID),
		CommandID:        spec.CommandID,
		CausationEventID: spec.BlockedEventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            resumePhase,
		Type:             "session_resumed",
		From:             spec.Actor,
		To:               allParticipants(metadata),
		CreatedAt:        now,
		Payload:          payload,
	}
	return appendIdempotentEvent(sessionDir, metadata, index, event)
}

func ExtendLimits(sessionDir string, metadata *SessionMetadata, spec LimitsExtendSpec) (AppendResult, bool, error) {
	index, current, err := mutableSessionIndex(sessionDir, metadata)
	if err != nil {
		return AppendResult{}, false, err
	}
	if current != "blocked" {
		return AppendResult{}, false, NewValidationError(CategoryCommandConflict, "phase", "session is not blocked")
	}
	if spec.Actor != metadata.Moderator {
		return AppendResult{}, false, NewValidationError(CategoryPrincipalInvalid, "actor", "only the moderator may extend limits")
	}
	if spec.AuthorizedBy != "user" {
		return AppendResult{}, false, NewValidationError(CategoryPrincipalInvalid, "authorized_by", "limits extend requires user authorization")
	}
	blocking, ok := eventByID(index, spec.BlockedEventID)
	if !ok || blocking.Type != "session_budget_exceeded" {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "blocked_event", "limits extend requires a session_budget_exceeded blocked event")
	}
	if len(spec.Changes) == 0 {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "changes", "limits changes are required")
	}
	resumePhase := Phase(payloadStringDefault(blocking.Payload, "resume_phase", ""))
	if resumePhase == "" {
		resumePhase = Phase(payloadStringDefault(blocking.Payload, "prior_phase", "working"))
	}
	if !validPhase(metadata.SessionType, resumePhase) || statusFromPhase(resumePhase) != StatusOpen {
		return AppendResult{}, false, NewValidationError(CategoryInvalidEnvelope, "resume_phase", "blocking event has invalid resume_phase")
	}
	now := spec.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if strings.TrimSpace(spec.CommandID) == "" {
		spec.CommandID = fmt.Sprintf("cmd_limits_extend_%d", now.UnixNano())
	}
	payload := map[string]any{"authorized_by": spec.AuthorizedBy, "blocked_event_id": spec.BlockedEventID, "changes": spec.Changes, "resume_phase": string(resumePhase)}
	if strings.TrimSpace(spec.Reason) != "" {
		payload["reason"] = strings.TrimSpace(spec.Reason)
	}
	event := EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventIDFromCommand("evt_limits_extended", spec.CommandID),
		CommandID:        spec.CommandID,
		CausationEventID: spec.BlockedEventID,
		CorrelationID:    metadata.ID,
		SessionID:        metadata.ID,
		SessionType:      metadata.SessionType,
		Phase:            resumePhase,
		Type:             "limits_extended",
		From:             spec.Actor,
		To:               allParticipants(metadata),
		CreatedAt:        now,
		Payload:          payload,
	}
	return appendIdempotentEvent(sessionDir, metadata, index, event)
}

func ListEscalationBatches(sessionDir string, metadata *SessionMetadata) ([]EscalationBatchSummary, error) {
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, err
	}
	batches := map[string]*EscalationBatchSummary{}
	for _, event := range index.Events {
		batchID := payloadStringDefault(event.Payload, "batch_id", "")
		if batchID == "" {
			continue
		}
		switch event.Type {
		case "escalation_batched":
			batch := batches[batchID]
			if batch == nil {
				batch = &EscalationBatchSummary{BatchID: batchID, FirstEventID: event.EventID, CanFlush: true}
				batches[batchID] = batch
			}
			batch.PendingCount++
			batch.LatestEventID = event.EventID
			batch.DeadlineAt = payloadStringDefault(event.Payload, "batch_deadline_at", batch.DeadlineAt)
			batch.PhaseAtBatch = payloadStringDefault(event.Payload, "prior_phase", string(event.Phase))
		case "user_escalation_requested", "escalation_rate_limited":
			if batch := batches[batchID]; batch != nil {
				batch.PendingCount = 0
				batch.CanFlush = false
				batch.LatestEventID = event.EventID
			}
		}
	}
	keys := make([]string, 0, len(batches))
	for key, batch := range batches {
		if batch.PendingCount > 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]EscalationBatchSummary, 0, len(keys))
	for _, key := range keys {
		out = append(out, *batches[key])
	}
	return out, nil
}

func existingDelegation(dataHome, sessionID string) (*SessionMetadata, string, bool, error) {
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

func taskAssignedEvent(metadata *SessionMetadata, spec DelegationStartSpec, now time.Time) EventEnvelope {
	payload := map[string]any{
		"task": spec.Task,
	}
	if spec.Context != "" {
		payload["context"] = spec.Context
	}
	if len(spec.Acceptance) > 0 {
		payload["acceptance_criteria"] = append([]string(nil), spec.Acceptance...)
	}
	if len(spec.ExpectedOutputs) > 0 {
		payload["expected_outputs"] = append([]string(nil), spec.ExpectedOutputs...)
	}
	return EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       spec.AssignmentEventID,
		CommandID:     spec.Session.CommandID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "assigned",
		Type:          "task_assigned",
		From:          metadata.Moderator,
		To:            []string{spec.Assignee},
		CreatedAt:     now,
		Payload:       payload,
	}
}

func delegationTransition(metadata *SessionMetadata, index *LogIndex, current Phase, action string, spec DelegationEventSpec, payload map[string]any) (string, Phase, string, []string, error) {
	actor := strings.TrimSpace(spec.Actor)
	if actor == "" {
		actor = metadata.Moderator
	}
	if !principalAllowed(metadata, actor, false) {
		return "", "", "", nil, NewValidationError(CategoryPrincipalInvalid, "actor", "actor is not allowed for this session")
	}
	to := append([]string(nil), spec.Recipients...)
	phase := current
	eventType := ""
	requirePhase := func(allowed ...Phase) error {
		for _, allowedPhase := range allowed {
			if current == allowedPhase {
				return nil
			}
		}
		return NewValidationError(CategoryCommandConflict, "phase", fmt.Sprintf("%s is not allowed from phase %s", action, current))
	}
	switch action {
	case "ack":
		if err := requirePhase("assigned"); err != nil {
			return "", "", "", nil, err
		}
		eventType, phase = "assignee_acknowledged", "acknowledged"
		if actor == metadata.Moderator {
			return "", "", "", nil, NewValidationError(CategoryPrincipalInvalid, "actor", "assignee acknowledgement must come from assignee")
		}
		to = defaultRecipients(to, metadata.Moderator)
	case "message":
		eventType = "delegation_message"
		to = defaultRecipients(to, metadata.Moderator)
	case "clarify":
		if err := requirePhase("acknowledged", "working", "revision_requested"); err != nil {
			return "", "", "", nil, err
		}
		eventType, phase = "clarification_requested", "needs_clarification"
		to = defaultRecipients(to, metadata.Moderator)
	case "answer-clarification":
		if err := requirePhase("needs_clarification", "waiting_user"); err != nil {
			return "", "", "", nil, err
		}
		if _, ok := eventByIDAndType(index, spec.CausationEventID, "clarification_requested"); !ok {
			return "", "", "", nil, NewValidationError(CategoryInvalidEnvelope, "in_reply_to", "must reference clarification_requested")
		}
		eventType, phase = "clarification_answered", "working"
		to = defaultRecipients(to, payloadStringDefault(payload, "assignee", ""))
		if len(to) == 0 || to[0] == "" {
			to = []string{firstNonModerator(metadata)}
		}
	case "update":
		if err := requirePhase("acknowledged", "working", "revision_requested"); err != nil {
			return "", "", "", nil, err
		}
		eventType, phase = "assignee_update", "working"
		to = defaultRecipients(to, metadata.Moderator)
	case "request-update":
		eventType = "assignee_update_requested"
		to = defaultRecipients(to, firstNonModerator(metadata))
	case "submit":
		if err := requirePhase("acknowledged", "working", "revision_requested"); err != nil {
			return "", "", "", nil, err
		}
		eventType, phase = "work_submitted", "submitted"
		to = defaultRecipients(to, metadata.Moderator)
	case "review":
		if err := requirePhase("submitted"); err != nil {
			return "", "", "", nil, err
		}
		eventType, phase = "review_requested", "under_review"
		to = defaultRecipients(to, firstNonModerator(metadata))
	case "review-question":
		if err := requirePhase("under_review"); err != nil {
			return "", "", "", nil, err
		}
		eventType = "review_clarification_requested"
		to = defaultRecipients(to, firstNonModerator(metadata))
	case "review-answer":
		if err := requirePhase("under_review"); err != nil {
			return "", "", "", nil, err
		}
		eventType = "review_clarification_answered"
		to = defaultRecipients(to, metadata.Moderator)
	case "review-submit":
		if err := requirePhase("under_review"); err != nil {
			return "", "", "", nil, err
		}
		eventType = "review_submitted"
		to = defaultRecipients(to, metadata.Moderator)
	case "revise":
		if err := requirePhase("under_review"); err != nil {
			return "", "", "", nil, err
		}
		eventType, phase = "revision_requested", "revision_requested"
		to = defaultRecipients(to, firstNonModerator(metadata))
	case "accept":
		if err := requirePhase("submitted", "under_review"); err != nil {
			return "", "", "", nil, err
		}
		eventType, phase = "work_accepted", "accepted"
		to = allParticipants(metadata)
	case "escalate":
		eventType = "user_escalation_requested"
		phase = "waiting_user"
		to = []string{"user"}
		payload["batch"] = false
		payload["prior_phase"] = string(current)
		if payload["resume_phase"] == nil || payload["resume_phase"] == "" {
			payload["resume_phase"] = resumePhaseForEscalation(current)
		}
		if payload["included_event_ids"] == nil && spec.CausationEventID != "" {
			payload["included_event_ids"] = []string{spec.CausationEventID}
		}
		if urgency := payloadStringDefault(payload, "urgency", "normal"); urgency == "low" && payloadStringDefault(payload, "batch_policy", "auto") != "never" {
			eventType, phase = "escalation_batched", current
			to = []string{metadata.Moderator}
			payload["batch_id"] = defaultString(payloadStringDefault(payload, "batch_id", ""), eventIDFromCommand("escbatch", spec.CommandID))
			payload["action"] = defaultString(payloadStringDefault(payload, "action", ""), "created")
			payload["source_event_id"] = defaultString(payloadStringDefault(payload, "source_event_id", ""), spec.CausationEventID)
		}
	case "escalation-flush":
		eventType, phase = "user_escalation_requested", "waiting_user"
		actor = "atn-controld"
		to = []string{"user"}
		payload["batch"] = true
		payload["prior_phase"] = string(current)
		payload["resume_phase"] = resumePhaseForEscalation(current)
	case "resolve-escalation":
		if err := requirePhase("waiting_user"); err != nil {
			return "", "", "", nil, err
		}
		escalation, ok := eventByIDAndType(index, spec.CausationEventID, "user_escalation_requested")
		if !ok {
			return "", "", "", nil, NewValidationError(CategoryInvalidEnvelope, "escalation", "must reference user_escalation_requested")
		}
		eventType = "user_escalation_resolved"
		actor = "user"
		to = []string{metadata.Moderator}
		phase = Phase(payloadStringDefault(escalation.Payload, "resume_phase", "working"))
		payload["escalation_event_id"] = spec.CausationEventID
		payload["resume_phase"] = string(phase)
	default:
		return "", "", "", nil, NewValidationError(CategoryInvalidEnvelope, "action", "unsupported delegation action")
	}
	return eventType, phase, actor, to, nil
}

func validateDelegationPayload(action string, payload map[string]any) error {
	required := map[string][]string{
		"ack":                  {"understanding"},
		"clarify":              {"question"},
		"answer-clarification": {"answer", "source"},
		"message":              {"kind", "message"},
		"update":               {"progress_status", "summary"},
		"request-update":       {"reason"},
		"submit":               {"summary"},
		"review-submit":        {"verdict"},
		"revise":               {"required_changes"},
		"accept":               {"final_summary"},
		"escalate":             {"question", "urgency"},
		"resolve-escalation":   {"answer"},
	}
	for _, key := range required[action] {
		if strings.TrimSpace(payloadStringDefault(payload, key, "")) == "" {
			if len(delegationSlice(payload, key)) == 0 {
				return NewValidationError(CategoryInvalidEnvelope, key, key+" is required")
			}
		}
	}
	if action == "message" && !allowedString(payloadStringDefault(payload, "kind", ""), "note", "instruction", "scope_correction", "priority_update", "process_guidance") {
		return NewValidationError(CategoryInvalidEnvelope, "kind", "invalid delegation message kind")
	}
	if action == "update" && !allowedString(payloadStringDefault(payload, "progress_status", ""), "working", "blocked", "partial", "testing", "ready_to_submit") {
		return NewValidationError(CategoryInvalidEnvelope, "progress_status", "invalid progress status")
	}
	if action == "review-submit" {
		if !allowedString(payloadStringDefault(payload, "verdict", ""), "accepted", "changes_requested", "rejected", "needs_clarification") {
			return NewValidationError(CategoryInvalidEnvelope, "verdict", "invalid review verdict")
		}
		for _, item := range delegationSlice(payload, "findings") {
			finding, ok := item.(map[string]any)
			if !ok {
				return NewValidationError(CategoryInvalidEnvelope, "findings", "finding must be an object")
			}
			if !allowedString(payloadStringDefault(finding, "severity", ""), "low", "medium", "high", "critical") {
				return NewValidationError(CategoryInvalidEnvelope, "findings.severity", "invalid finding severity")
			}
			if payloadStringDefault(finding, "issue", "") == "" {
				return NewValidationError(CategoryInvalidEnvelope, "findings.issue", "finding issue is required")
			}
		}
	}
	return nil
}

func ingestArtifacts(sessionDir string, metadata *SessionMetadata, actor string, sourcePaths []string, now time.Time) ([]map[string]any, error) {
	workspace := ""
	members, err := readSnapshotMembers(sessionDir)
	if err != nil {
		return nil, err
	}
	member, ok := members[actor]
	if !ok {
		return nil, NewValidationError(CategoryPrincipalInvalid, "actor", "actor is missing from registry snapshot")
	}
	workspace = strings.TrimSpace(member.Workspace)
	if workspace == "" {
		return nil, NewValidationError(CategoryPrincipalInvalid, "workspace", "actor workspace is required in registry snapshot")
	}
	artifactDir := filepath.Join(filepath.Clean(sessionDir), "artifacts")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return nil, NewValidationError(CategoryPathUnsafe, "artifacts", err.Error())
	}
	limit := metadata.Limits.MaxArtifactBytes
	if limit <= 0 {
		limit = 25 * 1024 * 1024
	}
	out := make([]map[string]any, 0, len(sourcePaths))
	for i, source := range sourcePaths {
		cleanSource := filepath.Clean(source)
		info, err := os.Lstat(cleanSource)
		if err != nil {
			return nil, NewValidationError(CategoryPathUnsafe, "artifact.source", err.Error())
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, NewValidationError(CategoryPathUnsafe, "artifact.source", "artifact source must be a regular non-symlink file")
		}
		if info.Size() > int64(limit) {
			return nil, NewValidationError(CategoryPathUnsafe, "artifact.source", "artifact exceeds max_artifact_bytes")
		}
		if workspace != "" && !pathContains(filepath.Clean(workspace), cleanSource) && !pathContains(filepath.Clean(sessionDir), cleanSource) {
			return nil, NewValidationError(CategoryPathUnsafe, "artifact.source", "artifact source escapes member workspace and session directory")
		}
		in, err := os.Open(cleanSource)
		if err != nil {
			return nil, NewValidationError(CategoryPathUnsafe, "artifact.source", err.Error())
		}
		hasher := sha256.New()
		artifactID := fmt.Sprintf("art_%s_%02d", eventIDFromCommand("", fmt.Sprintf("%s_%d", actor, now.UnixNano()))[1:], i+1)
		storedName := artifactID + "_" + filepath.Base(cleanSource)
		storedPathAbs := filepath.Join(artifactDir, storedName)
		outFile, err := os.OpenFile(storedPathAbs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return nil, NewValidationError(CategoryPathUnsafe, "artifact.stored_path", err.Error())
		}
		if _, err := io.Copy(io.MultiWriter(outFile, hasher), in); err != nil {
			_ = in.Close()
			_ = outFile.Close()
			return nil, NewValidationError(CategoryAppendFailed, "artifact", err.Error())
		}
		if err := in.Close(); err != nil {
			_ = outFile.Close()
			return nil, NewValidationError(CategoryAppendFailed, "artifact", err.Error())
		}
		if err := outFile.Close(); err != nil {
			return nil, NewValidationError(CategoryAppendFailed, "artifact", err.Error())
		}
		mime := sniffMIME(storedPathAbs)
		out = append(out, map[string]any{
			"artifact_id": artifactID,
			"stored_path": filepath.ToSlash(filepath.Join(SessionsDirName, metadata.ID, "artifacts", storedName)),
			"sha256":      "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
			"size_bytes":  info.Size(),
			"mime":        mime,
			"ingested_at": now.UTC().Format(time.RFC3339Nano),
		})
	}
	return out, nil
}

func mutableSessionIndex(sessionDir string, metadata *SessionMetadata) (*LogIndex, Phase, error) {
	if metadata == nil {
		return nil, "", NewValidationError(CategoryMetadataInvalid, "session", "metadata is required")
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, "", err
	}
	return index, latestPhase(metadata, index), nil
}

func eventByID(index *LogIndex, eventID string) (EventEnvelope, bool) {
	for _, event := range index.Events {
		if event.EventID == eventID {
			return event, true
		}
	}
	return EventEnvelope{}, false
}

func allParticipants(metadata *SessionMetadata) []string {
	to := append([]string(nil), metadata.Participants...)
	sort.Strings(to)
	return to
}

func defaultRecipients(current []string, fallback string) []string {
	if len(current) > 0 {
		return current
	}
	if fallback == "" {
		return nil
	}
	return []string{fallback}
}

func firstNonModerator(metadata *SessionMetadata) string {
	for _, participant := range metadata.Participants {
		if participant != metadata.Moderator {
			return participant
		}
	}
	return metadata.Moderator
}

func clonePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	return out
}

func allowedString(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func allowedBlockCategory(value string) bool {
	return allowedString(value, "external_dependency", "user_decision_required", "scope_conflict", "policy_or_security", "budget_or_limit", "other")
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func resumePhaseForEscalation(current Phase) string {
	if current == "needs_clarification" {
		return "working"
	}
	return string(current)
}

func sniffMIME(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer func() { _ = file.Close() }()
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	detected := http.DetectContentType(buf[:n])
	if strings.HasPrefix(detected, "text/plain") {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".markdown":
			return "text/markdown"
		case ".json":
			return "application/json"
		case ".yaml", ".yml":
			return "application/yaml"
		}
	}
	return detected
}

func delegationSlice(payload map[string]any, key string) []any {
	if payload == nil {
		return nil
	}
	switch value := payload[key].(type) {
	case []any:
		return value
	case []string:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, item)
		}
		return out
	case []map[string]any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}
