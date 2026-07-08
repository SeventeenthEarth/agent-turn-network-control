package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SeventeenthEarth/agent-turn-network-control/internal/registry"
)

const selectedRunnerPromptEvidenceEventType = "selected_runner_prompt_evidence"

const selectedRunnerRequiredResponseSchema = `{"type":"speech","payload":{"speech":"string","claims":"optional[]","stance_links":"optional[]","contribution_type":"optional string","new_axis_reason":"optional string|null","evidence":"optional[]"}}; claim_kind_allowed_values=[observation, requirement, risk, decision_frame, evidence, open_question, proposal]`

const selectedRunnerMissingContextInstruction = "If any required agenda or context field is missing, do not generate a substantive council turn. Treat the turn as blocked by missing control-owned prompt context."

const selectedRunnerArgueRule = "When prior claims exist, respond using the current council speech contract. Link through stance_links to the relevant prior claims, or use contribution_type=new_axis with new_axis_reason only when introducing a justified new axis."

const selectedRunnerPromptContextWindow = 6

type SelectedRunnerPromptEvidence struct {
	SessionID                    string   `json:"session_id"`
	SpeakerSelectedEventID       string   `json:"speaker_selected_event_id"`
	SelectedMember               string   `json:"selected_member"`
	Turn                         int      `json:"turn,omitempty"`
	CausationEventID             string   `json:"causation_event_id,omitempty"`
	Result                       string   `json:"result"`
	IncludedContext              []string `json:"included_context,omitempty"`
	MissingRequiredContext       []string `json:"missing_required_context,omitempty"`
	AgendaSourceEventIDs         []string `json:"agenda_source_event_ids,omitempty"`
	PriorContextSourceEventIDs   []string `json:"prior_context_source_event_ids,omitempty"`
	OwnHistorySourceEventIDs     []string `json:"own_history_source_event_ids,omitempty"`
	OwnLatestClaimSourceEventIDs []string `json:"own_latest_claim_source_event_ids,omitempty"`
	OwnClaimIndexSourceEventIDs  []string `json:"own_claim_index_source_event_ids,omitempty"`
	ParticipantRuntimeMode       string   `json:"participant_runtime_mode,omitempty"`
	HotPromptStrategy            string   `json:"hot_prompt_strategy,omitempty"`
	DeltaSourceEventIDs          []string `json:"delta_source_event_ids,omitempty"`
	RehydrateSourceEventIDs      []string `json:"rehydrate_source_event_ids,omitempty"`
	RehydrateValidationStatus    string   `json:"rehydrate_validation_status,omitempty"`
	RehydrateValidationFailures  []string `json:"rehydrate_validation_failures,omitempty"`
	StatelessFallback            bool     `json:"stateless_fallback"`
	StatelessFallbackStatus      string   `json:"stateless_fallback_status,omitempty"`
	FullHistoryHotPromptStatus   string   `json:"full_history_hot_prompt_status,omitempty"`
	OwnHistoryHotPromptStatus    string   `json:"own_history_hot_prompt_status,omitempty"`
	RuntimeBlockStatus           string   `json:"runtime_block_status,omitempty"`
	RuntimeBlockReason           string   `json:"runtime_block_reason,omitempty"`
	PromptContextSHA256          string   `json:"prompt_context_sha256,omitempty"`
	RedactedPromptExcerpt        string   `json:"redacted_prompt_excerpt,omitempty"`
}

type SelectedRunnerPromptEnvelope struct {
	Prompt   string
	Evidence SelectedRunnerPromptEvidence
}

type selectedRunnerPromptSource struct {
	EventID string
	Turn    int
	Member  string
	Speech  string
	Claims  []map[string]any
}

type selectedRunnerPromptClaim struct {
	ID      string
	Summary string
}

type selectedRunnerOwnHistoryContext struct {
	PriorSpeeches             string
	LatestClaims              string
	ClaimIndex                string
	HistorySourceEventIDs     []string
	LatestClaimSourceEventIDs []string
	ClaimIndexSourceEventIDs  []string
	HasPriorSpeechHistory     bool
	HasPriorClaimHistory      bool
	PriorSpeechesMissing      bool
	LatestClaimsMissing       bool
	ClaimIndexMissing         bool
}

func BuildSelectedRunnerPromptEnvelope(metadata *SessionMetadata, index *LogIndex, speaker EventEnvelope, member registry.Member) (SelectedRunnerPromptEnvelope, error) {
	if metadata == nil {
		return SelectedRunnerPromptEnvelope{}, NewValidationError(CategoryMetadataInvalid, "session", "session metadata is required")
	}
	if index == nil {
		return SelectedRunnerPromptEnvelope{}, NewValidationError(CategoryLogCorrupt, ChannelJSONLName, "log index is required")
	}
	if speaker.Type != "speaker_selected" {
		return SelectedRunnerPromptEnvelope{}, NewValidationError(CategoryInvalidEnvelope, "type", "selected-runner prompt evidence requires speaker_selected event")
	}

	turn := 0
	if speaker.Turn != nil {
		turn = *speaker.Turn
	}
	if turn == 0 {
		turn = anyInt(speaker.Payload, "turn")
	}

	evidence := SelectedRunnerPromptEvidence{
		SessionID:                  metadata.ID,
		SpeakerSelectedEventID:     strings.TrimSpace(speaker.EventID),
		SelectedMember:             strings.TrimSpace(member.ID),
		Turn:                       turn,
		CausationEventID:           strings.TrimSpace(speaker.EventID),
		Result:                     "pass",
		ParticipantRuntimeMode:     "persistent_delta",
		HotPromptStrategy:          "delta_only",
		RehydrateValidationStatus:  "not_required_hot_delta",
		StatelessFallback:          false,
		StatelessFallbackStatus:    "disabled",
		FullHistoryHotPromptStatus: "rejected",
		OwnHistoryHotPromptStatus:  "rejected",
		RuntimeBlockStatus:         "not_blocked",
	}

	contextPayload := map[string]any{}
	addIncluded := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, key)
			return
		}
		evidence.IncludedContext = appendUniqueString(evidence.IncludedContext, key)
		contextPayload[key] = value
	}

	addIncluded("session_id", metadata.ID)
	addIncluded("selected_member", strings.TrimSpace(member.ID))
	if turn > 0 {
		evidence.IncludedContext = appendUniqueString(evidence.IncludedContext, "turn")
		contextPayload["turn"] = turn
	} else {
		evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "turn")
	}
	addIncluded("causation_event_id", strings.TrimSpace(speaker.EventID))
	addIncluded("participant_runtime_mode", evidence.ParticipantRuntimeMode)
	addIncluded("hot_prompt_strategy", evidence.HotPromptStrategy)
	contextPayload["stateless_fallback"] = evidence.StatelessFallback
	contextPayload["stateless_fallback_status"] = evidence.StatelessFallbackStatus
	contextPayload["full_history_hot_prompt_status"] = evidence.FullHistoryHotPromptStatus
	contextPayload["own_history_hot_prompt_status"] = evidence.OwnHistoryHotPromptStatus
	contextPayload["rehydrate_validation_status"] = evidence.RehydrateValidationStatus
	contextPayload["runtime_block_status"] = evidence.RuntimeBlockStatus
	addIncluded("role_assignment", strings.TrimSpace(member.Role))
	addIncluded("stance_assignment", selectedRunnerStanceAssignment(index.Events, speaker, member.ID))
	addIncluded("required_response_schema", selectedRunnerRequiredResponseSchema)
	addIncluded("missing_context_instruction", selectedRunnerMissingContextInstruction)

	agenda, ok := latestAgendaLockedBeforeSelected(index.Events, speaker.EventID)
	if !ok {
		evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "decision_question")
		evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "success_criteria")
		evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "out_of_scope_policy")
	} else {
		evidence.AgendaSourceEventIDs = appendUniqueString(evidence.AgendaSourceEventIDs, agenda.EventID)
		addIncluded("decision_question", selectedRunnerContextText(agenda.Payload, "decision_question"))
		addIncluded("success_criteria", selectedRunnerContextText(agenda.Payload, "success_criteria"))
		addIncluded("out_of_scope_policy", selectedRunnerContextText(agenda.Payload, "out_of_scope_policy"))
	}

	priorSpeech, priorClaims, priorSourceIDs, hasPriorSpeechHistory, hasPriorClaimHistory, priorSpeechMissing, priorClaimMissing := boundedPriorPromptContext(index.Events, speaker.EventID)
	if hasPriorSpeechHistory {
		if strings.TrimSpace(priorSpeech) == "" || priorSpeechMissing {
			evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "prior_speech_context")
		} else {
			evidence.IncludedContext = appendUniqueString(evidence.IncludedContext, "prior_speech_context")
			contextPayload["prior_speech_context"] = priorSpeech
		}
	}
	if hasPriorClaimHistory {
		if strings.TrimSpace(priorClaims) == "" || priorClaimMissing {
			evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "prior_claim_context")
		} else {
			evidence.IncludedContext = appendUniqueString(evidence.IncludedContext, "prior_claim_context")
			contextPayload["prior_claim_context"] = priorClaims
		}
		addIncluded("argue_stance_rule", selectedRunnerArgueRule)
	}
	for _, eventID := range priorSourceIDs {
		evidence.PriorContextSourceEventIDs = appendUniqueString(evidence.PriorContextSourceEventIDs, eventID)
	}

	ownHistory := selectedMemberOwnHistoryContext(index.Events, speaker.EventID, member.ID)
	if ownHistory.HasPriorSpeechHistory {
		if strings.TrimSpace(ownHistory.PriorSpeeches) == "" || ownHistory.PriorSpeechesMissing {
			evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "selected_member_prior_speeches")
		} else {
			evidence.IncludedContext = appendUniqueString(evidence.IncludedContext, "selected_member_prior_speeches")
			contextPayload["selected_member_prior_speeches"] = ownHistory.PriorSpeeches
		}
	}
	if ownHistory.HasPriorClaimHistory {
		if strings.TrimSpace(ownHistory.LatestClaims) == "" || ownHistory.LatestClaimsMissing {
			evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "selected_member_latest_claims")
		} else {
			evidence.IncludedContext = appendUniqueString(evidence.IncludedContext, "selected_member_latest_claims")
			contextPayload["selected_member_latest_claims"] = ownHistory.LatestClaims
		}
		if strings.TrimSpace(ownHistory.ClaimIndex) == "" || ownHistory.ClaimIndexMissing {
			evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "selected_member_claim_index")
		} else {
			evidence.IncludedContext = appendUniqueString(evidence.IncludedContext, "selected_member_claim_index")
			contextPayload["selected_member_claim_index"] = ownHistory.ClaimIndex
		}
	}
	for _, eventID := range ownHistory.HistorySourceEventIDs {
		evidence.OwnHistorySourceEventIDs = appendUniqueString(evidence.OwnHistorySourceEventIDs, eventID)
	}
	for _, eventID := range ownHistory.LatestClaimSourceEventIDs {
		evidence.OwnLatestClaimSourceEventIDs = appendUniqueString(evidence.OwnLatestClaimSourceEventIDs, eventID)
	}
	for _, eventID := range ownHistory.ClaimIndexSourceEventIDs {
		evidence.OwnClaimIndexSourceEventIDs = appendUniqueString(evidence.OwnClaimIndexSourceEventIDs, eventID)
	}

	evidence.DeltaSourceEventIDs = selectedRunnerDeltaSourceEventIDs(evidence)
	rehydrate, rehydrateRequired := selectedRunnerRehydratePayload(metadata, index, speaker, member)
	evidence.RehydrateSourceEventIDs = selectedRunnerRehydrateSourceEventIDs(evidence, rehydrate)
	if rehydrateRequired {
		evidence.RehydrateValidationStatus = "validated"
		failures := selectedRunnerRehydrateValidationFailures(metadata, index, rehydrate, member)
		if len(failures) > 0 {
			evidence.RehydrateValidationStatus = "failed"
			evidence.RehydrateValidationFailures = failures
			evidence.RuntimeBlockStatus = "blocked_rehydrate_failed"
			evidence.RuntimeBlockReason = "rehydrate_identity_validation_failed"
			evidence.MissingRequiredContext = appendUniqueString(evidence.MissingRequiredContext, "rehydrate_identity_validation")
		}
	}
	contextPayload["delta_source_event_ids"] = append([]string(nil), evidence.DeltaSourceEventIDs...)
	contextPayload["rehydrate_source_event_ids"] = append([]string(nil), evidence.RehydrateSourceEventIDs...)
	contextPayload["rehydrate_validation_status"] = evidence.RehydrateValidationStatus
	contextPayload["rehydrate_validation_failures"] = append([]string(nil), evidence.RehydrateValidationFailures...)
	contextPayload["runtime_block_status"] = evidence.RuntimeBlockStatus
	contextPayload["runtime_block_reason"] = evidence.RuntimeBlockReason

	if len(evidence.MissingRequiredContext) > 0 {
		evidence.Result = "blocked"
	} else {
		evidence.Result = "pass"
	}
	contextPayload["included_context"] = append([]string(nil), evidence.IncludedContext...)
	contextPayload["missing_required_context"] = append([]string(nil), evidence.MissingRequiredContext...)
	contextPayload["agenda_source_event_ids"] = append([]string(nil), evidence.AgendaSourceEventIDs...)
	contextPayload["prior_context_source_event_ids"] = append([]string(nil), evidence.PriorContextSourceEventIDs...)
	contextPayload["own_history_source_event_ids"] = append([]string(nil), evidence.OwnHistorySourceEventIDs...)
	contextPayload["own_latest_claim_source_event_ids"] = append([]string(nil), evidence.OwnLatestClaimSourceEventIDs...)
	contextPayload["own_claim_index_source_event_ids"] = append([]string(nil), evidence.OwnClaimIndexSourceEventIDs...)
	contextPayload["delta_source_event_ids"] = append([]string(nil), evidence.DeltaSourceEventIDs...)
	contextPayload["rehydrate_source_event_ids"] = append([]string(nil), evidence.RehydrateSourceEventIDs...)
	contextPayload["rehydrate_validation_status"] = evidence.RehydrateValidationStatus
	contextPayload["rehydrate_validation_failures"] = append([]string(nil), evidence.RehydrateValidationFailures...)
	contextPayload["runtime_block_status"] = evidence.RuntimeBlockStatus
	contextPayload["runtime_block_reason"] = evidence.RuntimeBlockReason
	contextPayload["selected_member"] = evidence.SelectedMember
	contextPayload["speaker_selected_event_id"] = evidence.SpeakerSelectedEventID
	contextPayload["result"] = evidence.Result

	contextJSON, err := json.Marshal(contextPayload)
	if err != nil {
		return SelectedRunnerPromptEnvelope{}, NewValidationError(CategoryInvalidEnvelope, "selected_runner_prompt_context", err.Error())
	}
	digest := sha256.Sum256(contextJSON)
	evidence.PromptContextSHA256 = hex.EncodeToString(digest[:])
	evidence.RedactedPromptExcerpt = selectedRunnerRedactedPromptExcerpt(evidence)

	prompt := selectedRunnerPromptFromContext(contextPayload)
	return SelectedRunnerPromptEnvelope{Prompt: prompt, Evidence: evidence}, nil
}

func LatestSelectedRunnerPromptEvidenceFromIndex(index *LogIndex) *SelectedRunnerPromptEvidence {
	if index == nil {
		return nil
	}
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != selectedRunnerPromptEvidenceEventType {
			continue
		}
		evidence := selectedRunnerPromptEvidenceFromPayload(event.Payload)
		if evidence == nil {
			continue
		}
		if strings.TrimSpace(evidence.SpeakerSelectedEventID) == "" {
			evidence.SpeakerSelectedEventID = strings.TrimSpace(event.CausationEventID)
		}
		return evidence
	}
	return nil
}

func selectedRunnerPromptEvidenceFromPayload(payload map[string]any) *SelectedRunnerPromptEvidence {
	if payload == nil {
		return nil
	}
	evidence := &SelectedRunnerPromptEvidence{
		SessionID:                    anyString(payload, "session_id"),
		SpeakerSelectedEventID:       anyString(payload, "speaker_selected_event_id"),
		SelectedMember:               anyString(payload, "selected_member"),
		Turn:                         anyInt(payload, "turn"),
		CausationEventID:             anyString(payload, "causation_event_id"),
		Result:                       anyString(payload, "result"),
		IncludedContext:              selectedRunnerStringSlice(payload["included_context"]),
		MissingRequiredContext:       selectedRunnerStringSlice(payload["missing_required_context"]),
		AgendaSourceEventIDs:         selectedRunnerStringSlice(payload["agenda_source_event_ids"]),
		PriorContextSourceEventIDs:   selectedRunnerStringSlice(payload["prior_context_source_event_ids"]),
		OwnHistorySourceEventIDs:     selectedRunnerStringSlice(payload["own_history_source_event_ids"]),
		OwnLatestClaimSourceEventIDs: selectedRunnerStringSlice(payload["own_latest_claim_source_event_ids"]),
		OwnClaimIndexSourceEventIDs:  selectedRunnerStringSlice(payload["own_claim_index_source_event_ids"]),
		ParticipantRuntimeMode:       anyString(payload, "participant_runtime_mode"),
		HotPromptStrategy:            anyString(payload, "hot_prompt_strategy"),
		DeltaSourceEventIDs:          selectedRunnerStringSlice(payload["delta_source_event_ids"]),
		RehydrateSourceEventIDs:      selectedRunnerStringSlice(payload["rehydrate_source_event_ids"]),
		RehydrateValidationStatus:    anyString(payload, "rehydrate_validation_status"),
		RehydrateValidationFailures:  selectedRunnerStringSlice(payload["rehydrate_validation_failures"]),
		StatelessFallback:            anyBool(payload, "stateless_fallback"),
		StatelessFallbackStatus:      anyString(payload, "stateless_fallback_status"),
		FullHistoryHotPromptStatus:   anyString(payload, "full_history_hot_prompt_status"),
		OwnHistoryHotPromptStatus:    anyString(payload, "own_history_hot_prompt_status"),
		RuntimeBlockStatus:           anyString(payload, "runtime_block_status"),
		RuntimeBlockReason:           anyString(payload, "runtime_block_reason"),
		PromptContextSHA256:          anyString(payload, "prompt_context_sha256"),
		RedactedPromptExcerpt:        anyString(payload, "redacted_prompt_excerpt"),
	}
	if evidence.SessionID == "" && evidence.SpeakerSelectedEventID == "" && evidence.SelectedMember == "" && evidence.Result == "" {
		return nil
	}
	return evidence
}

func selectedRunnerStringSlice(value any) []string {
	out := make([]string, 0)
	for _, item := range anySlice(map[string]any{"items": value}, "items") {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}

func latestAgendaLockedBeforeSelected(events []EventEnvelope, speakerEventID string) (EventEnvelope, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventID == speakerEventID {
			for j := i - 1; j >= 0; j-- {
				if events[j].Type == "agenda_locked" {
					return events[j], true
				}
			}
			break
		}
	}
	return EventEnvelope{}, false
}

func selectedRunnerContextText(payload map[string]any, key string) string {
	if text := selectedRunnerContextValue(payload[key]); text != "" {
		return text
	}
	constraints := anyMap(payload, "constraints")
	if constraints != nil {
		return selectedRunnerContextValue(constraints[key])
	}
	return ""
}

func selectedRunnerContextValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		if len(typed) == 0 {
			return ""
		}
		return mustCompactJSON(typed)
	case []any:
		if len(typed) == 0 {
			return ""
		}
		return mustCompactJSON(typed)
	case map[string]any:
		if len(typed) == 0 {
			return ""
		}
		return mustCompactJSON(typed)
	default:
		if value == nil {
			return ""
		}
		return mustCompactJSON(value)
	}
}

func selectedRunnerStanceAssignment(events []EventEnvelope, speaker EventEnvelope, memberID string) string {
	_ = events
	_ = memberID
	return strings.TrimSpace(anyString(speaker.Payload, "stance_assignment"))
}

func boundedPriorPromptContext(events []EventEnvelope, speakerEventID string) (string, string, []string, bool, bool, bool, bool) {
	prior := selectedRunnerPromptSourcesBeforeSelected(events, speakerEventID, selectedRunnerPromptContextWindow, "")
	if len(prior) == 0 {
		return "", "", nil, false, false, false, false
	}

	speechParts := make([]string, 0, len(prior))
	claimParts := make([]string, 0, len(prior))
	sourceIDs := make([]string, 0, len(prior))
	hasPriorSpeechHistory := false
	hasPriorClaimHistory := false
	priorSpeechMissing := false
	priorClaimMissing := false
	for _, source := range prior {
		sourceIDs = append(sourceIDs, source.EventID)
		hasPriorSpeechHistory = true
		if strings.TrimSpace(source.Speech) == "" {
			priorSpeechMissing = true
		} else {
			speechParts = append(speechParts, fmt.Sprintf("turn=%d member=%s speech=%s", source.Turn, source.Member, source.Speech))
		}
		if len(source.Claims) == 0 {
			continue
		}
		hasPriorClaimHistory = true
		claimsForEvent, malformed := selectedRunnerClaimsForSource(source)
		if malformed || len(claimsForEvent) == 0 {
			priorClaimMissing = true
			continue
		}
		claimPartsForEvent := make([]string, 0, len(claimsForEvent))
		for _, claim := range claimsForEvent {
			claimPartsForEvent = append(claimPartsForEvent, claim.ID+":"+claim.Summary)
		}
		claimParts = append(claimParts, fmt.Sprintf("event=%s claims=%s", source.EventID, strings.Join(claimPartsForEvent, "; ")))
	}
	return strings.Join(speechParts, "\n"), strings.Join(claimParts, "\n"), sourceIDs, hasPriorSpeechHistory, hasPriorClaimHistory, priorSpeechMissing, priorClaimMissing
}

func selectedRunnerPromptSourcesBeforeSelected(events []EventEnvelope, speakerEventID string, limit int, memberID string) []selectedRunnerPromptSource {
	position := -1
	for i, event := range events {
		if event.EventID == speakerEventID {
			position = i
			break
		}
	}
	if position <= 0 {
		return nil
	}
	prior := make([]selectedRunnerPromptSource, 0)
	for i := position - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != "speech" {
			continue
		}
		if memberID != "" && strings.TrimSpace(event.From) != strings.TrimSpace(memberID) {
			continue
		}
		prior = append(prior, selectedRunnerPromptSourceFromEvent(event))
		if limit > 0 && len(prior) >= limit {
			break
		}
	}
	if len(prior) == 0 {
		return nil
	}
	for left, right := 0, len(prior)-1; left < right; left, right = left+1, right-1 {
		prior[left], prior[right] = prior[right], prior[left]
	}
	return prior
}

func selectedRunnerPromptSourceFromEvent(event EventEnvelope) selectedRunnerPromptSource {
	turn := anyInt(event.Payload, "turn")
	if turn <= 0 && event.Turn != nil && *event.Turn > 0 {
		turn = *event.Turn
	}
	source := selectedRunnerPromptSource{
		EventID: strings.TrimSpace(event.EventID),
		Turn:    turn,
		Member:  strings.TrimSpace(event.From),
		Speech:  strings.TrimSpace(payloadStringDefault(event.Payload, "speech", "")),
	}
	claims := anySlice(event.Payload, "claims")
	if len(claims) == 0 {
		return source
	}
	source.Claims = make([]map[string]any, 0, len(claims))
	for _, claim := range claims {
		typed, ok := claim.(map[string]any)
		if !ok {
			source.Claims = append(source.Claims, map[string]any{"malformed": true})
			continue
		}
		source.Claims = append(source.Claims, typed)
	}
	return source
}

func selectedRunnerSourceHasRequiredProvenance(source selectedRunnerPromptSource) bool {
	return strings.TrimSpace(source.EventID) != "" && strings.TrimSpace(source.Member) != "" && source.Turn > 0 && strings.TrimSpace(source.Speech) != ""
}

func selectedRunnerClaimsForSource(source selectedRunnerPromptSource) ([]selectedRunnerPromptClaim, bool) {
	if len(source.Claims) == 0 {
		return nil, false
	}
	claims := make([]selectedRunnerPromptClaim, 0, len(source.Claims))
	malformed := false
	for _, claim := range source.Claims {
		if claim["malformed"] == true {
			malformed = true
			continue
		}
		claimID := strings.TrimSpace(anyString(claim, "claim_id"))
		summary := strings.TrimSpace(anyString(claim, "summary"))
		if claimID == "" || summary == "" {
			malformed = true
			continue
		}
		claims = append(claims, selectedRunnerPromptClaim{ID: claimID, Summary: summary})
	}
	if len(claims) == 0 {
		malformed = true
	}
	return claims, malformed
}

func selectedRunnerDeltaSourceEventIDs(evidence SelectedRunnerPromptEvidence) []string {
	ids := make([]string, 0)
	ids = appendUniqueString(ids, evidence.SpeakerSelectedEventID)
	for _, eventID := range evidence.AgendaSourceEventIDs {
		ids = appendUniqueString(ids, eventID)
	}
	for _, eventID := range evidence.PriorContextSourceEventIDs {
		ids = appendUniqueString(ids, eventID)
	}
	return ids
}

func selectedRunnerRehydrateSourceEventIDs(evidence SelectedRunnerPromptEvidence, rehydrate map[string]any) []string {
	ids := make([]string, 0)
	for _, group := range [][]string{
		evidence.PriorContextSourceEventIDs,
		evidence.OwnHistorySourceEventIDs,
		evidence.OwnLatestClaimSourceEventIDs,
		evidence.OwnClaimIndexSourceEventIDs,
	} {
		for _, eventID := range group {
			ids = appendUniqueString(ids, eventID)
		}
	}
	for _, key := range []string{"source_event_ids", "argue_source_event_ids"} {
		for _, eventID := range selectedRunnerStringSlice(rehydrate[key]) {
			ids = appendUniqueString(ids, eventID)
		}
	}
	return ids
}

func selectedRunnerRehydratePayload(metadata *SessionMetadata, index *LogIndex, speaker EventEnvelope, member registry.Member) (map[string]any, bool) {
	if anyBool(speaker.Payload, "participant_runtime_rehydrate_required", "rehydrate_required") {
		return anyMap(speaker.Payload, "participant_runtime_rehydrate"), true
	}
	if metadata == nil || index == nil {
		return nil, false
	}
	payload, ok := selectedRunnerPersistentRuntimeRehydratePayload(metadata.ID, index, speaker.EventID, member.ID)
	return payload, ok
}

func selectedRunnerPersistentRuntimeRehydratePayload(sessionID string, index *LogIndex, speakerEventID, memberID string) (map[string]any, bool) {
	memberID = strings.TrimSpace(memberID)
	if memberID == "" || index == nil {
		return nil, false
	}
	seenRuntimeEvidence := false
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if strings.TrimSpace(event.EventID) == strings.TrimSpace(speakerEventID) {
			continue
		}
		evidence := anyMap(event.Payload, "persistent_participant_runtime_evidence")
		if evidence == nil {
			continue
		}
		seenRuntimeEvidence = true
		rows := selectedRunnerRuntimeEvidenceRows(evidence["members"])
		for _, row := range rows {
			if strings.TrimSpace(anyString(row, "member")) != memberID {
				continue
			}
			cursor := strings.TrimSpace(anyString(row, "last_cursor", "coverage_cursor"))
			eventID := strings.TrimSpace(anyString(row, "last_event_id"))
			if eventID == "" {
				eventID = strings.TrimSpace(anyString(evidence, "speech_event_id"))
			}
			if eventID == "" {
				eventID = event.EventID
			}
			handle := strings.TrimSpace(anyString(row, "participant_session_handle", "handle"))
			generation := anyInt(row, "participant_session_generation", "generation")
			payload := map[string]any{
				"session_id":          strings.TrimSpace(sessionID),
				"member":              memberID,
				"cursor":              cursor,
				"ack_cursor":          cursor,
				"participant_handle":  handle,
				"expected_handle":     handle,
				"generation":          generation,
				"expected_generation": generation,
				"source_event_ids":    []any{eventID},
				"rehydrate_source":    "persistent_participant_runtime_evidence",
				"producer_event_id":   event.EventID,
			}
			if cursor == "" || handle == "" || generation <= 0 {
				return payload, true
			}
			_, cursorEventID, err := ParseCursor(cursor)
			if err != nil || !selectedRunnerCursorResolves(index, cursor) || !selectedRunnerEventIDResolves(index, eventID) || strings.TrimSpace(eventID) != strings.TrimSpace(cursorEventID) {
				return payload, true
			}
			return nil, false
		}
	}
	if !seenRuntimeEvidence {
		return nil, false
	}
	return map[string]any{
		"session_id":       strings.TrimSpace(sessionID),
		"member":           memberID,
		"rehydrate_source": "persistent_participant_runtime_evidence_missing_member_row",
		"source_event_ids": []any{},
	}, true
}

func selectedRunnerRuntimeEvidenceRows(value any) []map[string]any {
	rows := make([]map[string]any, 0)
	switch typed := value.(type) {
	case []map[string]any:
		for _, row := range typed {
			if row != nil {
				rows = append(rows, row)
			}
		}
	case []any:
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok && row != nil {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func selectedRunnerRehydrateValidationFailures(metadata *SessionMetadata, index *LogIndex, rehydrate map[string]any, member registry.Member) []string {
	if rehydrate == nil {
		return []string{"rehydrate_ref_missing"}
	}
	failures := make([]string, 0)
	if got := strings.TrimSpace(anyString(rehydrate, "session_id")); got == "" || got != strings.TrimSpace(metadata.ID) {
		failures = appendUniqueString(failures, "session_id_mismatch")
	}
	if got := strings.TrimSpace(anyString(rehydrate, "member")); got == "" || got != strings.TrimSpace(member.ID) {
		failures = appendUniqueString(failures, "member_mismatch")
	}
	cursor := strings.TrimSpace(anyString(rehydrate, "cursor"))
	ackCursor := strings.TrimSpace(anyString(rehydrate, "ack_cursor"))
	cursorEventID := ""
	if cursor == "" {
		failures = appendUniqueString(failures, "cursor_missing")
	} else {
		_, parsedEventID, err := ParseCursor(cursor)
		if err != nil {
			failures = appendUniqueString(failures, "cursor_malformed")
		} else {
			cursorEventID = parsedEventID
			if !selectedRunnerCursorResolves(index, cursor) {
				failures = appendUniqueString(failures, "cursor_authority_mismatch")
			}
		}
	}
	if ackCursor == "" || cursor == "" || ackCursor != cursor {
		failures = appendUniqueString(failures, "ack_cursor_mismatch")
	}
	handle := strings.TrimSpace(anyString(rehydrate, "participant_handle"))
	expectedHandle := strings.TrimSpace(anyString(rehydrate, "expected_handle"))
	if handle == "" || expectedHandle == "" || handle != expectedHandle {
		failures = appendUniqueString(failures, "participant_handle_mismatch")
	}
	generation := anyInt(rehydrate, "generation")
	expectedGeneration := anyInt(rehydrate, "expected_generation")
	if generation <= 0 || expectedGeneration <= 0 || generation != expectedGeneration {
		failures = appendUniqueString(failures, "generation_mismatch")
	}
	knownEvents := map[string]struct{}{}
	if index != nil {
		for _, event := range index.Events {
			knownEvents[strings.TrimSpace(event.EventID)] = struct{}{}
		}
	}
	sourceIDs := selectedRunnerStringSlice(rehydrate["source_event_ids"])
	argueSourceIDs := selectedRunnerStringSlice(rehydrate["argue_source_event_ids"])
	if len(sourceIDs) == 0 {
		failures = appendUniqueString(failures, "source_event_ids_missing")
	}
	sourceRefsCursor := false
	for _, eventID := range sourceIDs {
		if strings.TrimSpace(eventID) == cursorEventID && cursorEventID != "" {
			sourceRefsCursor = true
		}
		if _, ok := knownEvents[eventID]; !ok {
			failures = appendUniqueString(failures, "source_event_id_unresolved")
		}
	}
	for _, eventID := range argueSourceIDs {
		if strings.TrimSpace(eventID) == cursorEventID && cursorEventID != "" {
			sourceRefsCursor = true
		}
		if _, ok := knownEvents[eventID]; !ok {
			failures = appendUniqueString(failures, "argue_source_event_id_unresolved")
		}
	}
	if cursorEventID != "" && !sourceRefsCursor {
		failures = appendUniqueString(failures, "cursor_source_ref_mismatch")
	}
	return failures
}

func selectedRunnerCursorResolves(index *LogIndex, cursor string) bool {
	offset, eventID, err := ParseCursor(cursor)
	if err != nil || index == nil || offset < 0 || int(offset) >= len(index.Events) {
		return false
	}
	return strings.TrimSpace(index.Events[offset].EventID) == strings.TrimSpace(eventID)
}

func selectedRunnerEventIDResolves(index *LogIndex, eventID string) bool {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || index == nil {
		return false
	}
	for _, event := range index.Events {
		if strings.TrimSpace(event.EventID) == eventID {
			return true
		}
	}
	return false
}

func selectedMemberOwnHistoryContext(events []EventEnvelope, speakerEventID, memberID string) selectedRunnerOwnHistoryContext {
	context := selectedRunnerOwnHistoryContext{}
	sources := selectedRunnerPromptSourcesBeforeSelected(events, speakerEventID, 0, memberID)
	if len(sources) == 0 {
		return context
	}

	context.HasPriorSpeechHistory = true
	speechParts := make([]string, 0, len(sources))
	claimIndexParts := make([]string, 0)
	latestClaimsParts := make([]string, 0)
	latestClaimsEventID := ""
	for _, source := range sources {
		context.HistorySourceEventIDs = appendUniqueString(context.HistorySourceEventIDs, source.EventID)
		if !selectedRunnerSourceHasRequiredProvenance(source) {
			context.PriorSpeechesMissing = true
			if len(source.Claims) > 0 {
				context.HasPriorClaimHistory = true
				context.ClaimIndexMissing = true
				latestClaimsEventID = ""
				latestClaimsParts = latestClaimsParts[:0]
				context.LatestClaimSourceEventIDs = nil
			}
			continue
		}
		speechParts = append(speechParts, fmt.Sprintf("event_id=%s member=%s turn=%d speech=%s", source.EventID, source.Member, source.Turn, source.Speech))
		if len(source.Claims) == 0 {
			continue
		}
		context.HasPriorClaimHistory = true
		claimsForEvent, malformed := selectedRunnerClaimsForSource(source)
		if malformed {
			context.ClaimIndexMissing = true
			latestClaimsEventID = ""
			latestClaimsParts = latestClaimsParts[:0]
			context.LatestClaimSourceEventIDs = nil
			continue
		}
		if len(claimsForEvent) == 0 {
			continue
		}
		latestClaimsEventID = source.EventID
		latestClaimsParts = latestClaimsParts[:0]
		context.LatestClaimSourceEventIDs = []string{source.EventID}
		context.ClaimIndexSourceEventIDs = appendUniqueString(context.ClaimIndexSourceEventIDs, source.EventID)
		for _, claim := range claimsForEvent {
			latestClaimsParts = append(latestClaimsParts, claim.ID+":"+claim.Summary)
			claimIndexParts = append(claimIndexParts, fmt.Sprintf("event=%s claim=%s summary=%s", source.EventID, claim.ID, claim.Summary))
		}
	}

	context.PriorSpeeches = strings.Join(speechParts, "\n")
	if context.HasPriorClaimHistory {
		if latestClaimsEventID == "" {
			context.LatestClaimsMissing = true
			context.LatestClaimSourceEventIDs = nil
		} else {
			context.LatestClaims = fmt.Sprintf("event=%s claims=%s", latestClaimsEventID, strings.Join(latestClaimsParts, "; "))
		}
		if len(claimIndexParts) == 0 {
			context.ClaimIndexMissing = true
		}
		if context.ClaimIndexMissing {
			context.ClaimIndexSourceEventIDs = nil
		} else {
			context.ClaimIndex = strings.Join(claimIndexParts, "\n")
		}
	}
	return context
}

func selectedRunnerRedactedPromptExcerpt(evidence SelectedRunnerPromptEvidence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "speaker_selected_event_id=%s\n", evidence.SpeakerSelectedEventID)
	fmt.Fprintf(&b, "selected_member=%s\n", evidence.SelectedMember)
	fmt.Fprintf(&b, "turn=%d\n", evidence.Turn)
	fmt.Fprintf(&b, "result=%s\n", evidence.Result)
	fmt.Fprintf(&b, "participant_runtime_mode=%s\n", evidence.ParticipantRuntimeMode)
	fmt.Fprintf(&b, "hot_prompt_strategy=%s\n", evidence.HotPromptStrategy)
	fmt.Fprintf(&b, "stateless_fallback=%t\n", evidence.StatelessFallback)
	fmt.Fprintf(&b, "full_history_hot_prompt_status=%s\n", evidence.FullHistoryHotPromptStatus)
	fmt.Fprintf(&b, "own_history_hot_prompt_status=%s\n", evidence.OwnHistoryHotPromptStatus)
	fmt.Fprintf(&b, "rehydrate_validation_status=%s\n", evidence.RehydrateValidationStatus)
	fmt.Fprintf(&b, "runtime_block_status=%s\n", evidence.RuntimeBlockStatus)
	fmt.Fprintf(&b, "included_context=%s\n", mustCompactJSON(evidence.IncludedContext))
	fmt.Fprintf(&b, "missing_required_context=%s\n", mustCompactJSON(evidence.MissingRequiredContext))
	if len(evidence.AgendaSourceEventIDs) > 0 {
		fmt.Fprintf(&b, "agenda_source_event_ids=%s\n", mustCompactJSON(evidence.AgendaSourceEventIDs))
	}
	if len(evidence.PriorContextSourceEventIDs) > 0 {
		fmt.Fprintf(&b, "prior_context_source_event_ids=%s\n", mustCompactJSON(evidence.PriorContextSourceEventIDs))
	}
	if len(evidence.OwnHistorySourceEventIDs) > 0 {
		fmt.Fprintf(&b, "own_history_source_event_ids=%s\n", mustCompactJSON(evidence.OwnHistorySourceEventIDs))
	}
	if len(evidence.OwnLatestClaimSourceEventIDs) > 0 {
		fmt.Fprintf(&b, "own_latest_claim_source_event_ids=%s\n", mustCompactJSON(evidence.OwnLatestClaimSourceEventIDs))
	}
	if len(evidence.OwnClaimIndexSourceEventIDs) > 0 {
		fmt.Fprintf(&b, "own_claim_index_source_event_ids=%s\n", mustCompactJSON(evidence.OwnClaimIndexSourceEventIDs))
	}
	return strings.TrimSpace(b.String())
}

func selectedRunnerPromptFromContext(contextPayload map[string]any) string {
	var b strings.Builder
	fmt.Fprintln(&b, "ATN council selected-runner prompt envelope")
	for _, key := range []string{
		"session_id",
		"speaker_selected_event_id",
		"selected_member",
		"role_assignment",
		"stance_assignment",
		"turn",
		"causation_event_id",
		"participant_runtime_mode",
		"hot_prompt_strategy",
		"stateless_fallback",
		"stateless_fallback_status",
		"full_history_hot_prompt_status",
		"own_history_hot_prompt_status",
		"delta_source_event_ids",
		"rehydrate_source_event_ids",
		"rehydrate_validation_status",
		"rehydrate_validation_failures",
		"runtime_block_status",
		"runtime_block_reason",
		"decision_question",
		"success_criteria",
		"out_of_scope_policy",
		"required_response_schema",
		"prior_speech_context",
		"prior_claim_context",
		"selected_member_prior_speeches",
		"selected_member_latest_claims",
		"selected_member_claim_index",
		"argue_stance_rule",
		"missing_context_instruction",
	} {
		value, ok := contextPayload[key]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s: %v\n", key, value)
	}
	fmt.Fprintln(&b, "Return only a typed speech JSON object and do not emit wrapper logs or prose outside the JSON object.")
	return strings.TrimSpace(b.String())
}
