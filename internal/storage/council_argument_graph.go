package storage

import (
	"fmt"
	"sort"
	"strings"
)

const (
	discussionQualityCompatibility = "compatibility"
	discussionQualityWarn          = "quality_warn"
	discussionQualityRequired      = "quality_required"
)

var argumentGraphStances = map[string]struct{}{
	"support":        {},
	"challenge":      {},
	"refine":         {},
	"extend":         {},
	"synthesize":     {},
	"question":       {},
	"risk_addition":  {},
	"decision_frame": {},
}

var argumentGraphContributionTypes = map[string]struct{}{
	"support":        {},
	"challenge":      {},
	"refine":         {},
	"extend":         {},
	"synthesize":     {},
	"question":       {},
	"risk_addition":  {},
	"decision_frame": {},
	"new_axis":       {},
}

var argumentGraphClaimKinds = map[string]struct{}{
	"observation":    {},
	"requirement":    {},
	"risk":           {},
	"decision_frame": {},
	"evidence":       {},
	"open_question":  {},
	"proposal":       {},
}

type discussionQualityPolicy struct {
	mode                           string
	modeReason                     string
	openingUnlinkedTurns           int
	requireClaims                  bool
	requireStanceLinksAfterOpening bool
	allowNewAxisWithReason         bool
	maxConsecutiveNewAxis          int
}

type argumentTargetLink struct {
	targetEventID string
	targetClaimID string
	stance        string
}

type autoSpeakerSelection struct {
	member string
	score  int
	reason string
	need   map[string]any
}

func validateDiscussionQualityLimits(limits Limits) error {
	if limits.Council.DiscussionQuality == nil {
		return nil
	}
	_, err := resolveDiscussionQualityPolicy(limits)
	return err
}

func resolveDiscussionQualityPolicy(limits Limits) (discussionQualityPolicy, error) {
	policy := discussionQualityPolicy{
		mode:                           discussionQualityCompatibility,
		modeReason:                     "discussion_quality absent; defaulting to compatibility",
		openingUnlinkedTurns:           1,
		requireClaims:                  false,
		requireStanceLinksAfterOpening: false,
		allowNewAxisWithReason:         true,
		maxConsecutiveNewAxis:          1,
	}
	config := limits.Council.DiscussionQuality
	if config == nil {
		return policy, nil
	}
	mode := strings.TrimSpace(config.Mode)
	if mode == "" {
		return policy, NewValidationError(CategoryInvalidEnvelope, "limits.council.discussion_quality.mode", "discussion_quality mode is required when discussion_quality is present")
	}
	if !allowedString(mode, discussionQualityCompatibility, discussionQualityWarn, discussionQualityRequired) {
		return policy, NewValidationError(CategoryInvalidEnvelope, "limits.council.discussion_quality.mode", "unsupported discussion_quality mode")
	}
	if config.OpeningUnlinkedTurns < 0 {
		return policy, NewValidationError(CategoryInvalidEnvelope, "limits.council.discussion_quality.opening_unlinked_turns", "opening_unlinked_turns must be non-negative")
	}
	if config.MaxConsecutiveNewAxis < 0 {
		return policy, NewValidationError(CategoryInvalidEnvelope, "limits.council.discussion_quality.max_consecutive_new_axis", "max_consecutive_new_axis must be non-negative")
	}
	policy.mode = mode
	policy.modeReason = "discussion_quality mode resolved from typed limits.council.discussion_quality"
	policy.openingUnlinkedTurns = config.OpeningUnlinkedTurns
	if policy.openingUnlinkedTurns == 0 {
		policy.openingUnlinkedTurns = 1
	}
	policy.requireClaims = config.RequireClaims
	policy.requireStanceLinksAfterOpening = config.RequireStanceLinksAfterOpening
	policy.allowNewAxisWithReason = config.AllowNewAxisWithReason
	policy.maxConsecutiveNewAxis = config.MaxConsecutiveNewAxis
	if policy.maxConsecutiveNewAxis == 0 {
		policy.maxConsecutiveNewAxis = 1
	}
	return policy, nil
}

func validateArgumentGraphHandRaise(metadata *SessionMetadata, index *LogIndex, payload map[string]any) error {
	if _, ok := payload["target_links"]; !ok {
		return nil
	}
	_, err := parseArgumentTargetLinks(index, payload, "target_links", false)
	if err != nil {
		return err
	}
	return nil
}

func validateArgumentGraphSpeech(metadata *SessionMetadata, index *LogIndex, actor string, turn int, payload map[string]any) error {
	policy, err := resolveDiscussionQualityPolicy(metadata.Limits)
	if err != nil {
		return err
	}
	claims, claimsPresent, err := parseArgumentClaims(payload)
	if err != nil {
		return err
	}
	_, linksPresent := payload["stance_links"]
	links, err := parseArgumentTargetLinks(index, payload, "stance_links", policy.mode == discussionQualityRequired)
	if err != nil {
		return err
	}
	contribution, contributionPresent, err := parseContributionType(payload)
	if err != nil {
		return err
	}
	newAxisReason := strings.TrimSpace(payloadStringDefault(payload, "new_axis_reason", ""))
	if contribution == "synthesize" && len(links) < 2 {
		return NewValidationError(CategoryInvalidEnvelope, "contribution_type", "synthesize requires at least two valid prior targets")
	}
	if contribution == "new_axis" && newAxisReason == "" {
		return NewValidationError(CategoryInvalidEnvelope, "new_axis_reason", "new_axis requires new_axis_reason")
	}

	if policy.mode == discussionQualityRequired {
		afterOpening := argumentGraphAfterOpening(index, policy)
		if afterOpening && policy.requireClaims && (!claimsPresent || len(claims) == 0) {
			return NewValidationError(CategoryInvalidEnvelope, "claims", "quality_required requires claims after opening window")
		}
		newAxisAllowed := contribution == "new_axis" && newAxisReason != "" && policy.allowNewAxisWithReason
		if afterOpening && policy.requireStanceLinksAfterOpening && (!linksPresent || len(links) == 0) && !newAxisAllowed {
			return NewValidationError(CategoryInvalidEnvelope, "stance_links", "quality_required requires stance_links after opening window")
		}
		if afterOpening && !newAxisAllowed && len(links) == 0 {
			return NewValidationError(CategoryInvalidEnvelope, "stance_links", "quality_required rejects orphan speech after opening window")
		}
		if contribution == "new_axis" && consecutiveNewAxisCount(index)+1 > policy.maxConsecutiveNewAxis && !selectedSpeakerHasTargetedNewAxisReason(index, turn, actor) {
			return NewValidationError(CategoryInvalidEnvelope, "contribution_type", "quality_required rejects repeated new_axis without targeted moderator reason")
		}
		if deterministicRuntimeNoise(payloadStringDefault(payload, "speech", "")) {
			return NewValidationError(CategoryInvalidEnvelope, "speech", "quality_required rejects runtime/system-noise speech")
		}
		if missing := missingSelectedGraphNeedTargets(index, turn, actor, links); len(missing) > 0 {
			return NewValidationError(CategoryInvalidEnvelope, "stance_links", "speech omits moderator-selected graph_need targets: "+strings.Join(missing, ","))
		}
		payload["prior_speaker_argue_quality_evidence"] = priorSpeakerQualityEvidenceForSpeech(index, policy, actor, turn, payload, links, nil)
	}

	if policy.mode == discussionQualityWarn {
		diagnostics := argumentGraphDiagnostics(index, turn, actor, payload, contribution, contributionPresent, links)
		if len(diagnostics) > 0 {
			payload["quality_diagnostics"] = diagnostics
		}
		payload["prior_speaker_argue_quality_evidence"] = priorSpeakerQualityEvidenceForSpeech(index, policy, actor, turn, payload, links, diagnostics)
	}
	return nil
}

func argumentGraphAfterOpening(index *LogIndex, policy discussionQualityPolicy) bool {
	if policy.openingUnlinkedTurns <= 0 {
		return true
	}
	return priorSpeechCount(index) >= policy.openingUnlinkedTurns
}

func priorSpeechCount(index *LogIndex) int {
	if index == nil {
		return 0
	}
	count := 0
	for _, event := range index.Events {
		if event.Type == "speech" {
			count++
		}
	}
	return count
}

func priorSpeakerQualityEvidenceForSpeech(index *LogIndex, policy discussionQualityPolicy, actor string, turn int, payload map[string]any, links []argumentTargetLink, diagnostics []map[string]any) map[string]any {
	afterOpening := argumentGraphAfterOpening(index, policy)
	contribution := strings.TrimSpace(payloadStringDefault(payload, "contribution_type", ""))
	newAxisReason := strings.TrimSpace(payloadStringDefault(payload, "new_axis_reason", ""))
	newAxisAllowed := contribution == "new_axis" && newAxisReason != "" && (policy.allowNewAxisWithReason || policy.mode != discussionQualityRequired)
	orphan := afterOpening && len(links) == 0 && !newAxisAllowed
	validTargets := make([]map[string]any, 0, len(links))
	for _, link := range links {
		validTargets = append(validTargets, map[string]any{
			"target_event_id": link.targetEventID,
			"target_claim_id": link.targetClaimID,
			"stance":          link.stance,
		})
	}
	evidence := map[string]any{
		"pass":                        !orphan && len(diagnostics) == 0,
		"mode":                        policy.mode,
		"opening_unlinked_turns":      policy.openingUnlinkedTurns,
		"prior_speech_count":          priorSpeechCount(index),
		"opening_exempt":              !afterOpening,
		"display_hint_authority":      "rejected",
		"responds_to_event_id_status": "display_only",
		"valid_prior_target_count":    len(validTargets),
		"valid_prior_targets":         validTargets,
		"new_axis_exception":          newAxisAllowed,
	}
	if orphan {
		evidence["orphan"] = true
		evidence["diagnostic_code"] = "orphan_speech"
	}
	if newAxisAllowed {
		evidence["new_axis_reason"] = newAxisReason
	}
	if len(diagnostics) > 0 {
		evidence["diagnostics"] = diagnostics
	}
	return evidence
}

func parseArgumentClaims(payload map[string]any) (map[string]struct{}, bool, error) {
	raw, ok := payload["claims"]
	if !ok {
		return nil, false, nil
	}
	items, ok := rawObjectSlice(raw)
	if !ok {
		return nil, true, NewValidationError(CategoryInvalidEnvelope, "claims", "claims must be an array of objects")
	}
	seen := map[string]struct{}{}
	for i, item := range items {
		claimID := strings.TrimSpace(anyString(item, "claim_id"))
		if claimID == "" {
			return nil, true, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("claims[%d].claim_id", i), "claim_id is required")
		}
		if _, exists := seen[claimID]; exists {
			return nil, true, NewValidationError(CategoryInvalidEnvelope, "claims", "duplicate claim_id")
		}
		if strings.TrimSpace(anyString(item, "summary")) == "" {
			return nil, true, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("claims[%d].summary", i), "claim summary is required")
		}
		if kind := strings.TrimSpace(anyString(item, "kind")); kind != "" {
			if _, ok := argumentGraphClaimKinds[kind]; !ok {
				return nil, true, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("claims[%d].kind", i), "unsupported claim kind")
			}
		}
		seen[claimID] = struct{}{}
	}
	return seen, true, nil
}

func parseArgumentTargetLinks(index *LogIndex, payload map[string]any, key string, requireRationale bool) ([]argumentTargetLink, error) {
	raw, ok := payload[key]
	if !ok {
		return nil, nil
	}
	items, ok := rawObjectSlice(raw)
	if !ok {
		return nil, NewValidationError(CategoryInvalidEnvelope, key, key+" must be an array of objects")
	}
	out := make([]argumentTargetLink, 0, len(items))
	for i, item := range items {
		targetEventID := strings.TrimSpace(anyString(item, "target_event_id"))
		if targetEventID == "" {
			return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s[%d].target_event_id", key, i), "target_event_id is required")
		}
		stance := strings.TrimSpace(anyString(item, "stance"))
		if _, ok := argumentGraphStances[stance]; !ok {
			return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s[%d].stance", key, i), "unsupported stance")
		}
		target, exists := eventByID(index, targetEventID)
		if !exists {
			return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s[%d].target_event_id", key, i), "target event does not exist")
		}
		if target.Type != "speech" {
			return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s[%d].target_event_id", key, i), "target event must be prior speech")
		}
		targetClaims, targetHasClaims, err := parseArgumentClaims(target.Payload)
		if err != nil {
			return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s[%d].target_event_id", key, i), "target speech has malformed claims")
		}
		targetClaimID := strings.TrimSpace(anyString(item, "target_claim_id"))
		if targetHasClaims && targetClaimID == "" {
			return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s[%d].target_claim_id", key, i), "target_claim_id is required when target speech has claims")
		}
		if targetClaimID != "" && targetHasClaims {
			if _, ok := targetClaims[targetClaimID]; !ok {
				return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s[%d].target_claim_id", key, i), "target_claim_id is absent from target speech claims")
			}
		}
		if requireRationale && key == "stance_links" && strings.TrimSpace(anyString(item, "rationale")) == "" {
			return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s[%d].rationale", key, i), "rationale is required in quality_required mode")
		}
		out = append(out, argumentTargetLink{targetEventID: targetEventID, targetClaimID: targetClaimID, stance: stance})
	}
	return out, nil
}

func parseContributionType(payload map[string]any) (string, bool, error) {
	raw, ok := payload["contribution_type"]
	if !ok {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", true, NewValidationError(CategoryInvalidEnvelope, "contribution_type", "contribution_type must be a string")
	}
	value = strings.TrimSpace(value)
	if _, ok := argumentGraphContributionTypes[value]; !ok {
		return "", true, NewValidationError(CategoryInvalidEnvelope, "contribution_type", "unsupported contribution_type")
	}
	return value, true, nil
}

func rawObjectSlice(raw any) ([]map[string]any, bool) {
	switch typed := raw.(type) {
	case []map[string]any:
		return append([]map[string]any(nil), typed...), true
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			object, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			out = append(out, object)
		}
		return out, true
	default:
		return nil, false
	}
}

func consecutiveNewAxisCount(index *LogIndex) int {
	count := 0
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != "speech" {
			continue
		}
		if payloadStringDefault(event.Payload, "contribution_type", "") != "new_axis" {
			break
		}
		count++
	}
	return count
}

func selectedSpeakerHasTargetedNewAxisReason(index *LogIndex, turn int, actor string) bool {
	selected := selectedSpeakerEvent(index, turn, actor)
	if selected == nil {
		return false
	}
	if anyMap(selected.Payload, "graph_need") != nil {
		return true
	}
	reason := strings.ToLower(payloadStringDefault(selected.Payload, "reason", ""))
	return strings.Contains(reason, "new_axis") || strings.Contains(reason, "new axis") || strings.Contains(reason, "missing decision dimension")
}

func deterministicRuntimeNoise(speech string) bool {
	text := strings.TrimSpace(strings.ToLower(speech))
	if text == "" {
		return false
	}
	prefixes := []string{"warning:", "runtime warning:", "system warning:", "traceback", "panic:", "error:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	needles := []string{"max iteration", "maximum iteration", "context length exceeded", "tool call failed", "rate limit exceeded"}
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func missingSelectedGraphNeedTargets(index *LogIndex, turn int, actor string, links []argumentTargetLink) []string {
	selected := selectedSpeakerEvent(index, turn, actor)
	if selected == nil {
		return nil
	}
	required := selectedGraphNeedTargets(selected.Payload)
	if len(required) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, link := range links {
		if link.targetEventID != "" && link.targetClaimID != "" {
			seen[link.targetEventID+"/"+link.targetClaimID] = struct{}{}
		}
		if link.targetClaimID != "" {
			seen[link.targetClaimID] = struct{}{}
		}
	}
	missing := []string{}
	for _, target := range required {
		if _, ok := seen[target]; !ok {
			missing = append(missing, target)
		}
	}
	sort.Strings(missing)
	return missing
}

func selectedGraphNeedTargets(payload map[string]any) []string {
	need := anyMap(payload, "graph_need")
	if need == nil {
		return nil
	}
	needType := strings.TrimSpace(anyString(need, "type", "need"))
	if needType != "" && needType != "synthesis" && needType != "synthesize" {
		return nil
	}
	required := map[string]struct{}{}
	for _, item := range anySlice(need, "target_claim_ids") {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			required[strings.TrimSpace(text)] = struct{}{}
		}
	}
	if links, ok := rawObjectSlice(need["target_links"]); ok {
		for _, link := range links {
			eventID := strings.TrimSpace(anyString(link, "target_event_id"))
			claimID := strings.TrimSpace(anyString(link, "target_claim_id"))
			if eventID != "" && claimID != "" {
				required[eventID+"/"+claimID] = struct{}{}
			} else if claimID != "" {
				required[claimID] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(required))
	for value := range required {
		out = append(out, value)
	}
	return out
}

func selectedSpeakerEvent(index *LogIndex, turn int, actor string) *EventEnvelope {
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != "speaker_selected" || anyInt(event.Payload, "turn") != turn {
			continue
		}
		member := payloadStringDefault(event.Payload, "member", "")
		if member == "" && len(event.To) == 1 {
			member = event.To[0]
		}
		if member == actor {
			selected := event
			return &selected
		}
	}
	return nil
}

func argumentGraphDiagnostics(index *LogIndex, turn int, actor string, payload map[string]any, contribution string, contributionPresent bool, links []argumentTargetLink) []map[string]any {
	diagnostics := []map[string]any{}
	newAxisReason := strings.TrimSpace(payloadStringDefault(payload, "new_axis_reason", ""))
	policy := discussionQualityPolicy{openingUnlinkedTurns: 1, allowNewAxisWithReason: true}
	if argumentGraphAfterOpening(index, policy) && len(links) == 0 && (contribution != "new_axis" || newAxisReason == "") {
		diagnostics = append(diagnostics, qualityDiagnostic("orphan_speech", "speech has no stance_links and is not a justified new_axis"))
	}
	if contribution == "new_axis" && consecutiveNewAxisCount(index) > 0 {
		diagnostics = append(diagnostics, qualityDiagnostic("repeated_new_axis", "new_axis follows another new_axis"))
	}
	if deterministicRuntimeNoise(payloadStringDefault(payload, "speech", "")) {
		diagnostics = append(diagnostics, qualityDiagnostic("runtime_noise", "speech resembles deterministic runtime/system noise"))
	}
	if missing := missingSelectedGraphNeedTargets(index, turn, actor, links); len(missing) > 0 {
		diagnostics = append(diagnostics, map[string]any{
			"code":            "omitted_graph_need_targets",
			"severity":        "warning",
			"message":         "speech omits moderator-selected graph_need targets",
			"missing_targets": missing,
		})
	}
	return diagnostics
}

func councilDiscussionQualityStatus(metadata *SessionMetadata, index *LogIndex, phase Phase) map[string]any {
	policy, err := resolveDiscussionQualityPolicy(metadata.Limits)
	if err != nil {
		return map[string]any{
			"mode":                    "invalid",
			"lifecycle_pass":          phase == "finalized",
			"discussion_quality_pass": false,
			"hard_warning_counts":     map[string]int{"invalid_discussion_quality_policy": 1},
			"hard_warning_codes":      []string{"invalid_discussion_quality_policy"},
		}
	}

	relationCounts := map[string]int{}
	handRaiseRelationCounts := map[string]int{}
	diagnosticCounts := map[string]int{}
	hardWarningCounts := map[string]int{}
	speechCount := 0
	openingSpeechCount := 0
	linkedSpeechCount := 0
	orphanSpeechCount := 0
	justifiedNewAxisCount := 0
	repeatedNewAxisCount := 0
	targetLinkCount := 0
	handRaiseTargetLinkCount := 0
	omittedGraphNeedTargetCount := 0
	priorSpeechWasNewAxis := false
	validPriorTargetRows := []map[string]any{}
	orphanRows := []map[string]any{}
	newAxisRows := []map[string]any{}
	invalidRelationRows := []map[string]any{}
	firstOrphanEventID := ""
	priorIndex := &LogIndex{Events: []EventEnvelope{}}

	addHardWarning := func(code string) {
		if code == "" {
			return
		}
		hardWarningCounts[code]++
	}
	addDiagnostic := func(code string) {
		if code == "" {
			return
		}
		diagnosticCounts[code]++
		switch code {
		case "orphan_speech", "repeated_new_axis", "omitted_graph_need_targets", "runtime_noise", "missing_required_argue_linkage", "missing_argument_graph_fields":
			addHardWarning(code)
		}
	}

	for _, event := range index.Events {
		switch event.Type {
		case "hand_raise":
			links, err := parseArgumentTargetLinks(priorIndex, event.Payload, "target_links", false)
			if err != nil {
				addDiagnostic("missing_required_argue_linkage")
				invalidRelationRows = append(invalidRelationRows, map[string]any{"event_id": event.EventID, "field": "target_links", "reason": err.Error()})
				break
			}
			for _, link := range links {
				handRaiseTargetLinkCount++
				handRaiseRelationCounts[link.stance]++
			}
		case "speech":
			speechHardWarnings := map[string]bool{}
			addSpeechHardWarning := func(code string) {
				if code != "" {
					speechHardWarnings[code] = true
				}
			}
			turn := anyInt(event.Payload, "turn")
			if turn == 0 && event.Turn != nil {
				turn = *event.Turn
			}
			afterOpening := speechCount >= policy.openingUnlinkedTurns
			speechCount++
			if !afterOpening {
				openingSpeechCount++
			}
			links, err := parseArgumentTargetLinks(priorIndex, event.Payload, "stance_links", false)
			if err != nil {
				addDiagnostic("missing_required_argue_linkage")
				invalidRelationRows = append(invalidRelationRows, map[string]any{"event_id": event.EventID, "field": "stance_links", "reason": err.Error()})
				links = nil
			}
			if len(links) > 0 {
				linkedSpeechCount++
			}
			for _, link := range links {
				targetLinkCount++
				relationCounts[link.stance]++
				validPriorTargetRows = append(validPriorTargetRows, map[string]any{"event_id": event.EventID, "target_event_id": link.targetEventID, "target_claim_id": link.targetClaimID, "stance": link.stance})
			}
			contribution := payloadStringDefault(event.Payload, "contribution_type", "")
			newAxisReason := strings.TrimSpace(payloadStringDefault(event.Payload, "new_axis_reason", ""))
			justifiedNewAxis := contribution == "new_axis" && newAxisReason != ""
			if justifiedNewAxis {
				justifiedNewAxisCount++
				if afterOpening {
					newAxisRows = append(newAxisRows, map[string]any{"event_id": event.EventID, "reason": newAxisReason})
				}
				if priorSpeechWasNewAxis {
					repeatedNewAxisCount++
					addSpeechHardWarning("repeated_new_axis")
				}
			}
			if afterOpening && len(links) == 0 && !justifiedNewAxis {
				orphanSpeechCount++
				if firstOrphanEventID == "" {
					firstOrphanEventID = event.EventID
				}
				orphanRows = append(orphanRows, map[string]any{"event_id": event.EventID, "turn": turn, "speaker": event.From, "responds_to_event_id_status": "display_only"})
				addSpeechHardWarning("orphan_speech")
			}
			if deterministicRuntimeNoise(payloadStringDefault(event.Payload, "speech", "")) {
				addSpeechHardWarning("runtime_noise")
			}
			for _, diagnostic := range payloadDiagnostics(event.Payload, "quality_diagnostics") {
				code := strings.TrimSpace(anyString(diagnostic, "code"))
				if code != "" {
					diagnosticCounts[code]++
					switch code {
					case "orphan_speech", "repeated_new_axis", "omitted_graph_need_targets", "runtime_noise", "missing_required_argue_linkage", "missing_argument_graph_fields":
						addSpeechHardWarning(code)
					}
				}
				omittedGraphNeedTargetCount += len(anySlice(diagnostic, "missing_targets"))
			}
			for code := range speechHardWarnings {
				addHardWarning(code)
			}
			priorSpeechWasNewAxis = contribution == "new_axis"
		}
		priorIndex.Events = append(priorIndex.Events, event)
	}

	hardWarningCodes := sortedMapKeys(hardWarningCounts)
	discussionPass := len(hardWarningCounts) == 0
	if policy.mode == discussionQualityCompatibility && linkedSpeechCount == 0 && justifiedNewAxisCount == 0 {
		discussionPass = false
	}
	priorSpeakerEvidence := map[string]any{
		"pass":                         len(orphanRows) == 0 && len(invalidRelationRows) == 0,
		"mode":                         policy.mode,
		"opening_unlinked_turns":       policy.openingUnlinkedTurns,
		"display_hint_authority":       "rejected",
		"responds_to_event_id_status":  "display_only",
		"first_orphan_event_id":        firstOrphanEventID,
		"orphan_count":                 len(orphanRows),
		"orphan_events":                orphanRows,
		"valid_prior_target_count":     len(validPriorTargetRows),
		"valid_prior_targets":          validPriorTargetRows,
		"invalid_relation_count":       len(invalidRelationRows),
		"invalid_relation_diagnostics": invalidRelationRows,
		"new_axis_exception_count":     len(newAxisRows),
		"new_axis_exceptions":          newAxisRows,
	}
	return map[string]any{
		"mode":                                 policy.mode,
		"mode_reason":                          policy.modeReason,
		"lifecycle_pass":                       phase == "finalized",
		"discussion_quality_pass":              discussionPass,
		"speech_count":                         speechCount,
		"opening_speech_count":                 openingSpeechCount,
		"linked_speech_count":                  linkedSpeechCount,
		"orphan_speech_count":                  orphanSpeechCount,
		"justified_new_axis_count":             justifiedNewAxisCount,
		"repeated_new_axis_count":              repeatedNewAxisCount,
		"target_link_count":                    targetLinkCount,
		"relation_counts":                      relationCounts,
		"quality_diagnostic_counts":            diagnosticCounts,
		"hard_warning_counts":                  hardWarningCounts,
		"hard_warning_codes":                   hardWarningCodes,
		"omitted_graph_need_targets":           omittedGraphNeedTargetCount,
		"hand_raise_target_link_count":         handRaiseTargetLinkCount,
		"hand_raise_relation_counts":           handRaiseRelationCounts,
		"prior_speaker_argue_quality_evidence": priorSpeakerEvidence,
	}
}

func payloadDiagnostics(payload map[string]any, key string) []map[string]any {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	items, ok := rawObjectSlice(raw)
	if !ok {
		return nil
	}
	return items
}

func sortedMapKeys(values map[string]int) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func qualityDiagnostic(code, message string) map[string]any {
	return map[string]any{"code": code, "severity": "warning", "message": message}
}

func autoCouncilSpeaker(metadata *SessionMetadata, index *LogIndex) (autoSpeakerSelection, error) {
	policy, err := resolveDiscussionQualityPolicy(metadata.Limits)
	if err != nil {
		return autoSpeakerSelection{}, err
	}
	qualityAware := policy.mode == discussionQualityWarn || policy.mode == discussionQualityRequired
	turn := currentCouncilTurn(index)
	if !qualityAware {
		return compatibilityAutoSpeaker(metadata, index), nil
	}
	raises := currentEligibleHandRaises(metadata, index, turn)
	if len(raises) == 0 {
		return autoSpeakerSelection{}, NewValidationError(CategoryCommandConflict, "hand_raise", "quality-aware auto-selection requires an eligible hand_raise")
	}
	last := latestCouncilSpeaker(index)
	nonConsecutive := make([]EventEnvelope, 0, len(raises))
	for _, raise := range raises {
		if raise.From != last {
			nonConsecutive = append(nonConsecutive, raise)
		}
	}
	if len(nonConsecutive) > 0 {
		raises = nonConsecutive
	} else if qualityAware {
		return autoSpeakerSelection{}, NewValidationError(CategoryCommandConflict, "speaker_selected", "quality-aware auto-selection has no non-consecutive eligible speaker")
	}

	best := autoSpeakerSelection{score: -1 << 30}
	for _, raise := range raises {
		score, reason, need := scoreCouncilHandRaise(index, raise)
		if score > best.score || (score == best.score && raise.From < best.member) {
			best = autoSpeakerSelection{member: raise.From, score: score, reason: reason, need: need}
		}
	}
	return best, nil
}

func currentEligibleHandRaises(metadata *SessionMetadata, index *LogIndex, turn int) []EventEnvelope {
	out := []EventEnvelope{}
	for _, event := range index.Events {
		if event.Type != "hand_raise" || anyInt(event.Payload, "turn") != turn || !councilMember(metadata, event.From) {
			continue
		}
		if value, ok := event.Payload["wants_to_speak"].(bool); ok && !value {
			continue
		}
		if value, ok := event.Payload["eligible"].(bool); ok && !value {
			continue
		}
		out = append(out, event)
	}
	return out
}

func compatibilityAutoSpeaker(metadata *SessionMetadata, index *LogIndex) autoSpeakerSelection {
	turn := currentCouncilTurn(index)
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type == "hand_raise" && anyInt(event.Payload, "turn") == turn && councilMember(metadata, event.From) {
			return autoSpeakerSelection{member: event.From, reason: "compatibility fallback selected latest hand_raise"}
		}
	}
	members := councilMembers(metadata)
	if len(members) > 0 {
		return autoSpeakerSelection{member: members[0], reason: "compatibility fallback selected first council member"}
	}
	return autoSpeakerSelection{}
}

func latestCouncilSpeaker(index *LogIndex) string {
	for i := len(index.Events) - 1; i >= 0; i-- {
		if index.Events[i].Type == "speech" {
			return index.Events[i].From
		}
	}
	return ""
}

func scoreCouncilHandRaise(index *LogIndex, raise EventEnvelope) (int, string, map[string]any) {
	score := anyInt(raise.Payload, "relevance") + anyInt(raise.Payload, "urgency")
	intent := strings.TrimSpace(payloadStringDefault(raise.Payload, "intent", ""))
	switch intent {
	case "challenge", "risk_addition", "risk", "block", "rebuttal":
		score += 8
	case "synthesize", "decision_frame":
		score += 7
	case "refine", "extend", "question":
		score += 5
	case "support":
		score += 3
	}
	links, _ := parseArgumentTargetLinks(index, raise.Payload, "target_links", false)
	if len(links) > 0 {
		score += len(links) * 4
	}
	reason := "auto ARGUE score"
	if intent != "" {
		reason += " intent=" + intent
	}
	need := map[string]any{}
	if len(links) > 0 {
		targets := make([]map[string]any, 0, len(links))
		for _, link := range links {
			targets = append(targets, map[string]any{"target_event_id": link.targetEventID, "target_claim_id": link.targetClaimID, "stance": link.stance})
		}
		need["target_links"] = targets
		need["type"] = intent
		need["target_link_count"] = len(links)
		need["relation_count"] = len(links)
	}
	return score, reason, need
}
