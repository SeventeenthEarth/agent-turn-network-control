package storage

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type sessionProjection struct {
	id, sessionType, title, moderator, status, phase                         string
	priorPhase, resumePhase, blockedByEventID                                string
	currentTurn, consensusRound                                              int
	lastSpeaker                                                              string
	tokensInTotal, tokensOutTotal                                            int
	usdEstimateTotal                                                         float64
	runnerCallsTotal, missingCostRunnerCallsTotal                            int
	userEscalationsTotal, pendingEscalationBatches, pendingBatchedCandidates int
	waitingUserEscalationEventID, waitingUserBatchID                         string
	surfaceJSON, linkedAuthorityJSON, linkedAuthorityResultJSON              string
	createdAt, updatedAt, closedAt                                           string
}

type participantProjection struct {
	sessionID, member, displayName, role, wrapper, sessionRef, streamCursor, runtimeStatus, lastHeartbeatAt, participantStatus string
	speakingCount, lastSpokeTurn                                                                                               int
	hasLastSpokeTurn                                                                                                           bool
}

type eventProjection struct {
	eventID, commandID, causationEventID, correlationID, sessionID, sessionType, phase, eventType, sender string
	schemaVersion, turn                                                                                   int
	hasTurn                                                                                               bool
	recipientJSON, runnerJSON                                                                             string
	costTokensIn, costTokensOut                                                                           int
	hasCostTokensIn, hasCostTokensOut                                                                     bool
	costUSD                                                                                               float64
	hasCostUSD                                                                                            bool
	costSource, createdAt, payloadJSON                                                                    string
}

type recipientProjection struct {
	eventID, sessionID, recipient string
	ordinal                       int
}

type runnerProjection struct {
	invocationID, sessionID, member, adapterKind, sourceCommandID, startedEventID, terminalEventID string
	attempt                                                                                        int
	isRetry                                                                                        bool
	status                                                                                         string
	costTokensIn, costTokensOut                                                                    int
	hasCostTokensIn, hasCostTokensOut                                                              bool
	costUSD                                                                                        float64
	hasCostUSD                                                                                     bool
	costSource                                                                                     string
	costMissing                                                                                    bool
	durationSec                                                                                    float64
	hasDurationSec                                                                                 bool
	startedAt, completedAt                                                                         string
}

type batchProjection struct {
	batchID, sessionID, status, firstEventID, latestEventID, userEscalationEventID         string
	batchWindowSec, pendingCount                                                           int
	batchDeadlineAt, priorPhase, resumePhase, createdAt, updatedAt, flushedAt, cancelledAt string
}

type batchItemProjection struct {
	batchID, sessionID, sourceEventID, addedEventID, sourceMember, questionHash, urgency, createdAt string
}

type streamCursorProjection struct {
	sessionID, member, cursor, eventID, acknowledgedAt string
}

type streamSubscriberProjection struct {
	sessionID, member, subscriberID, connectedAt, lastHeartbeatAt, lastCursor, status string
}

type delegationReviewProjection struct {
	sessionID                                  string
	reviewRound                                int
	reviewer, verdict, findingsJSON, createdAt string
}

type handRaiseProjection struct {
	sessionID                                            string
	turn                                                 int
	member                                               string
	wantsToSpeak, eligible                               bool
	intent, reason, evidenceSummary, ineligibilityReason string
	relevance, urgency                                   int
	hasRelevance, hasUrgency                             bool
	createdAt                                            string
}

type attendanceProjection struct {
	sessionID, member, attendanceRequestedEventID, memberAttendedEventID, attendanceStatus, attendanceSummary, surfaceEvidenceJSON, requestedAt, attendedAt string
	required                                                                                                                                                bool
}

type agendaLockProjection struct {
	sessionID, agendaLockedEventID, lockedBy, decisionQuestion, constraintsJSON, surfaceEvidenceJSON, lockedAt string
}

type voteProjection struct {
	sessionID                                       string
	consensusRound, draftVersion                    int
	member, vote, reason, requiredChange, createdAt string
}

type linkedAuthorityProjection struct {
	sessionID, terminalEventID, status, kanbanCardID, kanbanCommentID, vaultDecisionNote, followupCardID, failureReason, evidenceJSON, recordedAt string
}

type commandProjection struct {
	commandID, sessionID, firstEventID, resultSummary, createdAt string
}

type artifactProjection struct {
	sessionID, artifactID, sourcePath, storedPath string
	sizeBytes                                     int
	sha256, mime, ingestedAt                      string
}

func (s *projectionState) initSession(metadata *SessionMetadata, snapshot map[string]snapshotMember) {
	surfaceJSON, _ := canonicalJSON(metadata.Surface)
	linkedJSON, _ := canonicalJSON(metadata.LinkedAuthority)
	sp := &sessionProjection{
		id:                  metadata.ID,
		sessionType:         string(metadata.SessionType),
		title:               metadata.Title,
		moderator:           metadata.Moderator,
		status:              string(statusFromPhase(metadata.State.Phase)),
		phase:               string(metadata.State.Phase),
		currentTurn:         metadata.State.CurrentTurn,
		consensusRound:      metadata.State.ConsensusRound,
		lastSpeaker:         metadata.State.LastSpeaker,
		surfaceJSON:         surfaceJSON,
		linkedAuthorityJSON: linkedJSON,
		createdAt:           timeText(metadata.CreatedAt),
		updatedAt:           timeText(metadata.CreatedAt),
	}
	s.sessions[metadata.ID] = sp
	for _, member := range metadata.Participants {
		snap := snapshot[member]
		s.participants[metadata.ID+"\x00"+member] = &participantProjection{
			sessionID:     metadata.ID,
			member:        member,
			displayName:   snap.DisplayName,
			role:          snap.Role,
			wrapper:       snap.Wrapper,
			sessionRef:    metadata.ID,
			runtimeStatus: "unknown",
		}
	}
}

func (s *projectionState) applyEvent(metadata *SessionMetadata, offset int, event EventEnvelope) error {
	recipientJSON, err := canonicalJSONRequired(event.To)
	if err != nil {
		return wrapProjectionError(ProjectionErrorReplay, "canonicalize recipients", err)
	}
	runnerJSON, err := canonicalJSON(event.Runner)
	if err != nil {
		return wrapProjectionError(ProjectionErrorReplay, "canonicalize runner", err)
	}
	payloadJSON, err := canonicalJSONRequired(event.Payload)
	if err != nil {
		return wrapProjectionError(ProjectionErrorReplay, "canonicalize payload", err)
	}
	cost := parseCost(event.Cost)
	turn := 0
	hasTurn := false
	if event.Turn != nil {
		turn = *event.Turn
		hasTurn = true
	}
	ep := &eventProjection{
		eventID:          event.EventID,
		schemaVersion:    event.SchemaVersion,
		commandID:        event.CommandID,
		causationEventID: event.CausationEventID,
		correlationID:    event.CorrelationID,
		sessionID:        event.SessionID,
		sessionType:      string(event.SessionType),
		turn:             turn,
		hasTurn:          hasTurn,
		phase:            string(event.Phase),
		eventType:        event.Type,
		sender:           event.From,
		recipientJSON:    recipientJSON,
		runnerJSON:       runnerJSON,
		costTokensIn:     cost.tokensIn,
		costTokensOut:    cost.tokensOut,
		costUSD:          cost.usd,
		hasCostTokensIn:  cost.hasTokensIn,
		hasCostTokensOut: cost.hasTokensOut,
		hasCostUSD:       cost.hasUSD,
		costSource:       cost.source,
		createdAt:        timeText(event.CreatedAt),
		payloadJSON:      payloadJSON,
	}
	s.events = append(s.events, ep)
	s.eventCount++
	s.hasher.write(event.SessionID, fmt.Sprintf("%012d", offset), ep.eventID, ep.eventType, payloadJSON)
	for i, recipient := range event.To {
		s.recipients = append(s.recipients, &recipientProjection{eventID: event.EventID, sessionID: event.SessionID, recipient: recipient, ordinal: i})
	}
	session := s.sessions[event.SessionID]
	if session != nil {
		session.phase = string(event.Phase)
		session.status = string(statusFromPhase(event.Phase))
		session.updatedAt = timeText(event.CreatedAt)
		if event.Turn != nil {
			session.currentTurn = *event.Turn
		}
		if cr := anyInt(event.Payload, "consensus_round"); cr != 0 {
			session.consensusRound = cr
		}
		if event.Phase == "blocked" {
			session.priorPhase = anyString(event.Payload, "prior_phase")
			session.resumePhase = anyString(event.Payload, "resume_phase")
			session.blockedByEventID = event.EventID
		}
		if event.Type == "session_resumed" {
			session.priorPhase, session.resumePhase, session.blockedByEventID = "", "", ""
		}
		if statusFromPhase(event.Phase) == StatusTerminal {
			session.closedAt = timeText(event.CreatedAt)
		}
		if event.Type == "user_escalation_requested" {
			session.userEscalationsTotal++
			session.waitingUserEscalationEventID = event.EventID
			session.waitingUserBatchID = anyString(event.Payload, "batch_id")
		}
	}
	if event.CommandID != "" {
		s.commandEvents[event.CommandID] = append(s.commandEvents[event.CommandID], ep)
	}
	s.applyRunnerEvent(session, event, cost)
	s.applyEscalationEvent(session, event)
	s.applyCouncilEvent(session, event)
	s.applyStreamEvent(event)
	s.applyReviewEvent(event)
	s.applyArtifacts(event)
	return nil
}

type parsedCost struct {
	tokensIn, tokensOut       int
	usd                       float64
	hasTokensIn, hasTokensOut bool
	hasUSD                    bool
	source                    string
	nullOrMissing             bool
}

func parseCost(raw json.RawMessage) parsedCost {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return parsedCost{nullOrMissing: true}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return parsedCost{}
	}
	out := parsedCost{source: anyString(payload, "source")}
	if value, ok := anyFloat(payload, "tokens_in", "input_tokens", "tokens_input"); ok {
		out.tokensIn = int(value)
		out.hasTokensIn = true
	}
	if value, ok := anyFloat(payload, "tokens_out", "output_tokens", "tokens_output"); ok {
		out.tokensOut = int(value)
		out.hasTokensOut = true
	}
	if value, ok := anyFloat(payload, "usd_estimate", "usd", "cost_usd"); ok {
		out.usd = value
		out.hasUSD = true
	}
	return out
}

func (s *projectionState) applyRunnerEvent(session *sessionProjection, event EventEnvelope, cost parsedCost) {
	if event.Runner == nil {
		return
	}
	id := event.Runner.InvocationID
	if id == "" {
		id = anyString(event.Payload, "invocation_id")
	}
	if id == "" {
		return
	}
	switch event.Type {
	case "runner_invocation_started":
		s.runners[id] = &runnerProjection{
			invocationID:    id,
			sessionID:       event.SessionID,
			member:          event.Runner.Member,
			adapterKind:     event.Runner.AdapterKind,
			sourceCommandID: event.Runner.SourceCommandID,
			startedEventID:  event.EventID,
			attempt:         event.Runner.Attempt,
			isRetry:         event.Runner.IsRetry,
			status:          "started",
			startedAt:       timeText(event.CreatedAt),
		}
		if session != nil {
			session.runnerCallsTotal++
		}
	default:
		runner := s.runners[id]
		if runner == nil {
			runner = &runnerProjection{
				invocationID:    id,
				sessionID:       event.SessionID,
				member:          event.Runner.Member,
				adapterKind:     event.Runner.AdapterKind,
				sourceCommandID: event.Runner.SourceCommandID,
				startedEventID:  event.EventID,
				attempt:         event.Runner.Attempt,
				isRetry:         event.Runner.IsRetry,
				status:          "started",
				startedAt:       timeText(event.CreatedAt),
			}
			s.runners[id] = runner
		}
		status := "succeeded"
		if event.Type == "runner_invocation_failed" {
			status = anyString(event.Payload, "status", "reason")
			if status == "" {
				status = "failed"
			}
		}
		if event.Type == "runner_result_discarded" {
			status = "discarded_after_cancel"
		}
		if event.Runner.Status != "" && event.Type == "runner_invocation_failed" {
			status = event.Runner.Status
		}
		runner.status = status
		runner.terminalEventID = event.EventID
		runner.completedAt = timeText(event.CreatedAt)
		runner.costTokensIn, runner.costTokensOut, runner.costUSD = cost.tokensIn, cost.tokensOut, cost.usd
		runner.hasCostTokensIn, runner.hasCostTokensOut, runner.hasCostUSD = cost.hasTokensIn, cost.hasTokensOut, cost.hasUSD
		runner.costSource = cost.source
		if event.Runner.DurationSec != nil {
			runner.durationSec = *event.Runner.DurationSec
			runner.hasDurationSec = true
		}
		if cost.nullOrMissing {
			runner.costMissing = true
			if session != nil {
				session.missingCostRunnerCallsTotal++
			}
		} else if session != nil {
			if cost.hasTokensIn {
				session.tokensInTotal += cost.tokensIn
			}
			if cost.hasTokensOut {
				session.tokensOutTotal += cost.tokensOut
			}
			if cost.hasUSD {
				session.usdEstimateTotal += cost.usd
			}
		}
	}
}

func (s *projectionState) applyEscalationEvent(session *sessionProjection, event EventEnvelope) {
	batchID := anyString(event.Payload, "batch_id")
	switch event.Type {
	case "escalation_batched":
		if batchID == "" {
			return
		}
		batch := s.batches[batchID]
		if batch == nil {
			batch = &batchProjection{
				batchID:         batchID,
				sessionID:       event.SessionID,
				status:          "pending",
				firstEventID:    event.EventID,
				batchWindowSec:  anyInt(event.Payload, "batch_window_sec"),
				batchDeadlineAt: anyString(event.Payload, "batch_deadline_at"),
				priorPhase:      anyString(event.Payload, "prior_phase"),
				resumePhase:     anyString(event.Payload, "resume_phase"),
				createdAt:       timeText(event.CreatedAt),
			}
			s.batches[batchID] = batch
		}
		batch.latestEventID = event.EventID
		batch.updatedAt = timeText(event.CreatedAt)
		batch.pendingCount++
		item := &batchItemProjection{
			batchID:       batchID,
			sessionID:     event.SessionID,
			sourceEventID: anyString(event.Payload, "source_event_id"),
			addedEventID:  event.EventID,
			sourceMember:  anyString(event.Payload, "source_member", "member"),
			questionHash:  anyString(event.Payload, "question_hash"),
			urgency:       anyString(event.Payload, "urgency"),
			createdAt:     timeText(event.CreatedAt),
		}
		if item.sourceEventID == "" {
			item.sourceEventID = event.EventID
		}
		s.batchItems[batchID+"\x00"+item.sourceEventID] = item
		if session != nil {
			session.pendingEscalationBatches = countPendingBatches(s.batches, event.SessionID)
			session.pendingBatchedCandidates++
		}
	case "user_escalation_requested":
		if batchID != "" && anyBool(event.Payload, "batch", "batched") {
			if batch := s.batches[batchID]; batch != nil {
				batch.status = "flushed"
				batch.userEscalationEventID = event.EventID
				batch.latestEventID = event.EventID
				batch.pendingCount = 0
				batch.flushedAt = timeText(event.CreatedAt)
				batch.updatedAt = timeText(event.CreatedAt)
			}
		}
	case "escalation_rate_limited":
		if batchID != "" {
			if batch := s.batches[batchID]; batch != nil {
				batch.status = "rate_limited"
				batch.latestEventID = event.EventID
				batch.updatedAt = timeText(event.CreatedAt)
			}
		}
	}
	if session != nil {
		session.pendingEscalationBatches = countPendingBatches(s.batches, event.SessionID)
		session.pendingBatchedCandidates = countPendingBatchItems(s.batches, event.SessionID)
	}
}

func countPendingBatches(batches map[string]*batchProjection, sessionID string) int {
	count := 0
	for _, batch := range batches {
		if batch.sessionID == sessionID && batch.status == "pending" {
			count++
		}
	}
	return count
}

func countPendingBatchItems(batches map[string]*batchProjection, sessionID string) int {
	count := 0
	for _, batch := range batches {
		if batch.sessionID == sessionID && batch.status == "pending" {
			count += batch.pendingCount
		}
	}
	return count
}

func (s *projectionState) applyCouncilEvent(session *sessionProjection, event EventEnvelope) {
	switch event.Type {
	case "attendance_requested":
		required := stringSetFromPayload(event.Payload, "required_members")
		for _, member := range event.To {
			if member == "user" {
				continue
			}
			isRequired := len(required) == 0 || required[member]
			s.attendance[event.SessionID+"\x00"+member] = &attendanceProjection{
				sessionID:                  event.SessionID,
				member:                     member,
				required:                   isRequired,
				attendanceRequestedEventID: event.EventID,
				attendanceStatus:           "requested",
				requestedAt:                timeText(event.CreatedAt),
			}
		}
	case "member_attended":
		member := anyString(event.Payload, "member")
		if member == "" {
			member = event.From
		}
		key := event.SessionID + "\x00" + member
		row := s.attendance[key]
		if row == nil {
			row = &attendanceProjection{sessionID: event.SessionID, member: member, required: true, requestedAt: timeText(event.CreatedAt)}
			s.attendance[key] = row
		}
		row.memberAttendedEventID = event.EventID
		row.attendanceStatus = anyString(event.Payload, "attendance_status", "status")
		row.attendanceSummary = anyString(event.Payload, "attendance_summary", "summary")
		row.surfaceEvidenceJSON, _ = canonicalJSON(anyMap(event.Payload, "surface_evidence"))
		row.attendedAt = timeText(event.CreatedAt)
	case "agenda_locked":
		constraintsJSON, _ := canonicalJSON(anyMap(event.Payload, "constraints"))
		surfaceEvidenceJSON, _ := canonicalJSON(anyMap(event.Payload, "surface_evidence"))
		s.agendaLocks[event.SessionID] = &agendaLockProjection{
			sessionID:           event.SessionID,
			agendaLockedEventID: event.EventID,
			lockedBy:            event.From,
			decisionQuestion:    anyString(event.Payload, "decision_question"),
			constraintsJSON:     constraintsJSON,
			surfaceEvidenceJSON: surfaceEvidenceJSON,
			lockedAt:            timeText(event.CreatedAt),
		}
	case "hand_raise":
		turn := anyInt(event.Payload, "turn")
		if event.Turn != nil {
			turn = *event.Turn
		}
		row := &handRaiseProjection{
			sessionID:       event.SessionID,
			turn:            turn,
			member:          event.From,
			wantsToSpeak:    anyBool(event.Payload, "wants_to_speak"),
			intent:          anyString(event.Payload, "intent"),
			reason:          anyString(event.Payload, "reason"),
			evidenceSummary: anyString(event.Payload, "evidence_summary"),
			eligible:        true,
			createdAt:       timeText(event.CreatedAt),
		}
		if value, ok := anyFloat(event.Payload, "relevance"); ok {
			row.relevance, row.hasRelevance = int(value), true
		}
		if value, ok := anyFloat(event.Payload, "urgency"); ok {
			row.urgency, row.hasUrgency = int(value), true
		}
		if session != nil && session.lastSpeaker == event.From {
			row.eligible = false
			row.ineligibilityReason = "recent_speaker"
		}
		s.handRaises[event.SessionID+"\x00"+fmt.Sprint(turn)+"\x00"+event.From] = row
	case "consensus_vote":
		round := anyInt(event.Payload, "consensus_round")
		if round == 0 && session != nil {
			round = session.consensusRound
		}
		draft := anyInt(event.Payload, "draft_version")
		row := &voteProjection{
			sessionID:      event.SessionID,
			consensusRound: round,
			draftVersion:   draft,
			member:         event.From,
			vote:           anyString(event.Payload, "vote"),
			reason:         anyString(event.Payload, "reason"),
			requiredChange: anyString(event.Payload, "required_change"),
			createdAt:      timeText(event.CreatedAt),
		}
		s.votes[event.SessionID+"\x00"+fmt.Sprint(round)+"\x00"+fmt.Sprint(draft)+"\x00"+event.From] = row
	case "council_finalized", "council_unresolved":
		result := anyMap(event.Payload, "linked_authority_result")
		if result != nil {
			evidenceJSON, _ := canonicalJSONRequired(anyMap(result, "evidence"))
			resultJSON, _ := canonicalJSONRequired(result)
			row := &linkedAuthorityProjection{
				sessionID:         event.SessionID,
				terminalEventID:   event.EventID,
				status:            anyString(result, "status"),
				kanbanCardID:      anyString(result, "kanban_card_id"),
				kanbanCommentID:   anyString(result, "kanban_comment_id"),
				vaultDecisionNote: anyString(result, "vault_decision_note"),
				followupCardID:    anyString(result, "followup_card_id"),
				failureReason:     anyString(result, "failure_reason"),
				evidenceJSON:      evidenceJSON,
				recordedAt:        timeText(event.CreatedAt),
			}
			s.linkedAuthorityResults[event.SessionID] = row
			if session != nil {
				session.linkedAuthorityResultJSON = resultJSON
			}
		}
	}
}

func stringSetFromPayload(payload map[string]any, key string) map[string]bool {
	set := map[string]bool{}
	for _, item := range anySlice(payload, key) {
		if text, ok := item.(string); ok {
			set[text] = true
		}
	}
	return set
}

func (s *projectionState) applyStreamEvent(event EventEnvelope) {
	switch event.Type {
	case "stream_cursor_acknowledged":
		member := anyString(event.Payload, "member")
		if member == "" {
			member = event.From
		}
		s.streamCursors[event.SessionID+"\x00"+member] = &streamCursorProjection{
			sessionID:      event.SessionID,
			member:         member,
			cursor:         anyString(event.Payload, "cursor"),
			eventID:        anyString(event.Payload, "event_id"),
			acknowledgedAt: timeText(event.CreatedAt),
		}
	case "stream_subscriber_connected", "stream_subscriber_heartbeat", "stream_subscriber_disconnected":
		member := anyString(event.Payload, "member")
		if member == "" {
			member = event.From
		}
		subscriberID := anyString(event.Payload, "subscriber_id")
		if subscriberID == "" {
			return
		}
		key := event.SessionID + "\x00" + member + "\x00" + subscriberID
		row := s.streamSubscribers[key]
		if row == nil {
			row = &streamSubscriberProjection{sessionID: event.SessionID, member: member, subscriberID: subscriberID, connectedAt: timeText(event.CreatedAt)}
			s.streamSubscribers[key] = row
		}
		row.lastHeartbeatAt = timeText(event.CreatedAt)
		row.lastCursor = anyString(event.Payload, "last_cursor", "cursor")
		row.status = anyString(event.Payload, "status")
		if row.status == "" {
			row.status = strings.TrimPrefix(event.Type, "stream_subscriber_")
		}
	}
}

func (s *projectionState) applyReviewEvent(event EventEnvelope) {
	if event.Type != "review_submitted" && event.Type != "review_verdict" {
		return
	}
	findingsJSON, _ := canonicalJSONRequired(anySlice(event.Payload, "findings"))
	round := anyInt(event.Payload, "review_round")
	if round == 0 {
		round = 1
	}
	reviewer := anyString(event.Payload, "reviewer")
	if reviewer == "" {
		reviewer = event.From
	}
	s.delegationReviews[event.SessionID+"\x00"+fmt.Sprint(round)+"\x00"+reviewer] = &delegationReviewProjection{
		sessionID:    event.SessionID,
		reviewRound:  round,
		reviewer:     reviewer,
		verdict:      anyString(event.Payload, "verdict"),
		findingsJSON: findingsJSON,
		createdAt:    timeText(event.CreatedAt),
	}
}

func (s *projectionState) applyArtifacts(event EventEnvelope) {
	var arrays [][]any
	switch event.Type {
	case "work_submitted":
		arrays = append(arrays, anySlice(event.Payload, "artifacts"))
	case "review_requested":
		arrays = append(arrays, anySlice(event.Payload, "target_artifacts"))
	case "work_accepted":
		arrays = append(arrays, anySlice(event.Payload, "accepted_artifacts"))
	default:
		return
	}
	for _, items := range arrays {
		for _, item := range items {
			artifact, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := anyString(artifact, "artifact_id")
			if id == "" {
				continue
			}
			s.artifacts[event.SessionID+"\x00"+id] = &artifactProjection{
				sessionID:  event.SessionID,
				artifactID: id,
				sourcePath: anyString(artifact, "source_path"),
				storedPath: anyString(artifact, "stored_path"),
				sizeBytes:  anyInt(artifact, "size_bytes"),
				sha256:     anyString(artifact, "sha256"),
				mime:       anyString(artifact, "mime"),
				ingestedAt: timeText(event.CreatedAt),
			}
		}
	}
}

func (s *projectionState) finalizeCommands() error {
	ids := make([]string, 0, len(s.commandEvents))
	for commandID := range s.commandEvents {
		ids = append(ids, commandID)
	}
	sort.Strings(ids)
	for _, commandID := range ids {
		events := s.commandEvents[commandID]
		if len(events) == 0 {
			continue
		}
		sessionID := events[0].sessionID
		for _, event := range events[1:] {
			if event.sessionID != sessionID {
				return wrapProjectionError(ProjectionErrorReplay, "command id crosses sessions", NewValidationError(CategoryCommandConflict, commandID, "command_id maps to multiple sessions"))
			}
		}
		summaryParts := make([]map[string]string, 0, len(events))
		for _, event := range events {
			summaryParts = append(summaryParts, map[string]string{
				"event_id": event.eventID,
				"type":     event.eventType,
				"phase":    event.phase,
			})
		}
		summary, _ := canonicalJSONRequired(summaryParts)
		s.commands[commandID] = &commandProjection{
			commandID:     commandID,
			sessionID:     sessionID,
			firstEventID:  events[0].eventID,
			resultSummary: summary,
			createdAt:     events[0].createdAt,
		}
	}
	return nil
}

func sortedValues[T any](m map[string]*T) []*T {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*T, 0, len(keys))
	for _, key := range keys {
		out = append(out, m[key])
	}
	return out
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullInt(value int, ok bool) any {
	if !ok {
		return nil
	}
	return value
}

func nullFloat(value float64, ok bool) any {
	if !ok {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
