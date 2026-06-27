package storage

import (
	"fmt"
	"sort"
	"strings"
)

type SelectedRunnerAccounting struct {
	SelectedRunnerPass          bool                            `json:"selected_runner_pass"`
	SelectedSpeakerCount        int                             `json:"selected_speaker_count"`
	RunnerStartedCount          int                             `json:"runner_started_count"`
	RunnerSucceededCount        int                             `json:"runner_succeeded_count"`
	RunnerFailedCount           int                             `json:"runner_failed_count"`
	TerminalDiscardCount        int                             `json:"terminal_discard_count"`
	DispatchFailureCount        int                             `json:"dispatch_failure_count"`
	LinkedRunnerSpeechCount     int                             `json:"linked_runner_speech_count"`
	RunnerlessSpeechCount       int                             `json:"runnerless_speech_count"`
	ManualOrFallbackSpeechCount int                             `json:"manual_or_fallback_speech_count"`
	Diagnostics                 []SelectedRunnerDiagnostic      `json:"diagnostics,omitempty"`
	SelectedRunners             []SelectedRunnerGrantAccounting `json:"selected_runners,omitempty"`
}

type SelectedRunnerGrantAccounting struct {
	SelectedEventID              string   `json:"selected_event_id"`
	Member                       string   `json:"member,omitempty"`
	Turn                         int      `json:"turn,omitempty"`
	RunnerStarted                bool     `json:"runner_started"`
	RunnerStartEventIDs          []string `json:"runner_start_event_ids,omitempty"`
	RunnerSucceededEventIDs      []string `json:"runner_succeeded_event_ids,omitempty"`
	RunnerInvocationIDs          []string `json:"runner_invocation_ids,omitempty"`
	LinkedRunnerSpeechEventIDs   []string `json:"linked_runner_speech_event_ids,omitempty"`
	LinkedRunnerDeliveryEventIDs []string `json:"linked_runner_delivery_event_ids,omitempty"`
	TerminalFailureEventIDs      []string `json:"terminal_failure_event_ids,omitempty"`
	TerminalDiscardEventIDs      []string `json:"terminal_discard_event_ids,omitempty"`
	DispatchFailureEventIDs      []string `json:"dispatch_failure_event_ids,omitempty"`
	Pass                         bool     `json:"pass"`
	Status                       string   `json:"status"`
}

type SelectedRunnerDiagnostic struct {
	Code               string `json:"code"`
	EventID            string `json:"event_id,omitempty"`
	SelectedEventID    string `json:"selected_event_id,omitempty"`
	Member             string `json:"member,omitempty"`
	RunnerInvocationID string `json:"runner_invocation_id,omitempty"`
	Message            string `json:"message,omitempty"`
}

func SelectedRunnerAccountingFromIndex(metadata *SessionMetadata, index *LogIndex) SelectedRunnerAccounting {
	accounting := SelectedRunnerAccounting{SelectedRunnerPass: false}
	if index == nil {
		accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{Code: "missing_log_index", Message: "selected-runner accounting could not read channel index"})
		return accounting
	}
	grantIndex := map[string]int{}
	invocationToGrant := map[string]string{}
	succeededInvocations := map[string]struct{}{}
	linkedSpeechEvents := map[string]EventEnvelope{}
	for _, event := range index.Events {
		switch event.Type {
		case "speaker_selected":
			member := selectedRunnerMember(event)
			grant := SelectedRunnerGrantAccounting{
				SelectedEventID: event.EventID,
				Member:          member,
				Status:          "pending",
			}
			if turn, ok := payloadInt(event.Payload, "turn"); ok {
				grant.Turn = turn
			}
			accounting.SelectedRunners = append(accounting.SelectedRunners, grant)
			grantIndex[event.EventID] = len(accounting.SelectedRunners) - 1
			accounting.SelectedSpeakerCount++
		case "runner_invocation_started":
			accounting.RunnerStartedCount++
			grantID := matchingSelectedGrant(event, grantIndex, invocationToGrant)
			if grantID == "" || event.Runner == nil {
				continue
			}
			grant := &accounting.SelectedRunners[grantIndex[grantID]]
			if grant.Member != "" && event.Runner.Member != grant.Member {
				accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
					Code:               "runner_start_member_mismatch",
					EventID:            event.EventID,
					SelectedEventID:    grant.SelectedEventID,
					Member:             event.Runner.Member,
					RunnerInvocationID: event.Runner.InvocationID,
					Message:            "runner start did not match selected speaker member",
				})
				continue
			}
			grant.RunnerStarted = true
			grant.RunnerStartEventIDs = appendUniqueString(grant.RunnerStartEventIDs, event.EventID)
			if event.Runner.InvocationID != "" {
				grant.RunnerInvocationIDs = appendUniqueString(grant.RunnerInvocationIDs, event.Runner.InvocationID)
				invocationToGrant[event.Runner.InvocationID] = grant.SelectedEventID
			}
		case "runner_invocation_succeeded":
			grantID := matchingSelectedGrant(event, grantIndex, invocationToGrant)
			if grantID == "" || event.Runner == nil {
				continue
			}
			grant := &accounting.SelectedRunners[grantIndex[grantID]]
			if event.Runner.Status != "succeeded" {
				accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
					Code:               "runner_invocation_succeeded_status_not_succeeded",
					EventID:            event.EventID,
					SelectedEventID:    grant.SelectedEventID,
					Member:             event.Runner.Member,
					RunnerInvocationID: event.Runner.InvocationID,
					Message:            "runner_invocation_succeeded must have runner.status succeeded to count as selected-runner pass",
				})
				continue
			}
			if grant.Member != "" && event.Runner.Member != grant.Member {
				accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
					Code:               "runner_success_member_mismatch",
					EventID:            event.EventID,
					SelectedEventID:    grant.SelectedEventID,
					Member:             event.Runner.Member,
					RunnerInvocationID: event.Runner.InvocationID,
					Message:            "runner success did not match selected speaker member",
				})
				continue
			}
			if event.Runner.InvocationID != "" {
				grant.RunnerInvocationIDs = appendUniqueString(grant.RunnerInvocationIDs, event.Runner.InvocationID)
				grant.RunnerSucceededEventIDs = appendUniqueString(grant.RunnerSucceededEventIDs, event.EventID)
				if _, ok := succeededInvocations[event.Runner.InvocationID]; !ok {
					succeededInvocations[event.Runner.InvocationID] = struct{}{}
					accounting.RunnerSucceededCount++
				}
			}
		case "speech":
			if event.Runner == nil {
				accounting.RunnerlessSpeechCount++
				grantID := selectedGrantForSpeech(event, grantIndex)
				if grantID != "" {
					accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
						Code:            "runnerless_speech_not_selected_runner_evidence",
						EventID:         event.EventID,
						SelectedEventID: grantID,
						Member:          event.From,
						Message:         "runnerless speech is lifecycle/fallback evidence only",
					})
				}
				if manualOrFallbackSpeech(event) {
					accounting.ManualOrFallbackSpeechCount++
					if grantID != "" {
						accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
							Code:            "manual_or_fallback_speech_not_selected_runner_evidence",
							EventID:         event.EventID,
							SelectedEventID: grantID,
							Member:          event.From,
							Message:         "manual/fallback speech must not repair selected_runner_pass",
						})
					}
				}
				continue
			}
			grantID := matchingSelectedGrant(event, grantIndex, invocationToGrant)
			if grantID == "" {
				continue
			}
			grant := &accounting.SelectedRunners[grantIndex[grantID]]
			if event.Runner.Status != "succeeded" {
				accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
					Code:               "linked_runner_speech_status_not_succeeded",
					EventID:            event.EventID,
					SelectedEventID:    grant.SelectedEventID,
					Member:             event.Runner.Member,
					RunnerInvocationID: event.Runner.InvocationID,
					Message:            "linked runner speech must have runner.status succeeded to count as selected-runner pass",
				})
				continue
			}
			if linkedRunnerSpeechMatchesGrant(event, *grant) {
				accounting.LinkedRunnerSpeechCount++
				grant.LinkedRunnerSpeechEventIDs = appendUniqueString(grant.LinkedRunnerSpeechEventIDs, event.EventID)
				linkedSpeechEvents[event.EventID] = event
			} else {
				accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
					Code:               "linked_runner_speech_mismatch",
					EventID:            event.EventID,
					SelectedEventID:    grant.SelectedEventID,
					Member:             event.Runner.Member,
					RunnerInvocationID: event.Runner.InvocationID,
					Message:            "runner speech did not match selected member, invocation, and selected-event causation",
				})
			}
		case "runner_invocation_failed":
			accounting.RunnerFailedCount++
			accounting.recordSelectedRunnerTerminal(event, grantIndex, invocationToGrant, "terminal_failure")
		case "runner_result_discarded":
			accounting.TerminalDiscardCount++
			accounting.recordSelectedRunnerTerminal(event, grantIndex, invocationToGrant, "terminal_discard")
		case "selected_runner_dispatch_failed":
			accounting.DispatchFailureCount++
			accounting.recordSelectedRunnerTerminal(event, grantIndex, invocationToGrant, "dispatch_failure")
		}
	}
	if len(accounting.SelectedRunners) > 0 {
		accounting.SelectedRunnerPass = true
	}
	for i := range accounting.SelectedRunners {
		grant := &accounting.SelectedRunners[i]
		switch {
		case len(grant.TerminalFailureEventIDs) > 0 || len(grant.TerminalDiscardEventIDs) > 0 || len(grant.DispatchFailureEventIDs) > 0:
			grant.Pass = false
			grant.Status = "runner_terminal_failure"
		case !grant.RunnerStarted:
			grant.Pass = false
			grant.Status = "missing_runner_invocation_started"
			accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
				Code:            "missing_runner_invocation_started",
				SelectedEventID: grant.SelectedEventID,
				Member:          grant.Member,
				Message:         "selected speaker has no selected-runner start evidence",
			})
		case len(grant.RunnerSucceededEventIDs) == 0:
			grant.Pass = false
			grant.Status = "missing_runner_invocation_succeeded"
			accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
				Code:            "missing_runner_invocation_succeeded",
				SelectedEventID: grant.SelectedEventID,
				Member:          grant.Member,
				Message:         "selected-runner start has no runner_invocation_succeeded evidence",
			})
		case len(grant.LinkedRunnerSpeechEventIDs) == 0:
			grant.Pass = false
			grant.Status = "missing_linked_runner_speech"
			accounting.Diagnostics = append(accounting.Diagnostics, SelectedRunnerDiagnostic{
				Code:            "missing_linked_runner_speech",
				SelectedEventID: grant.SelectedEventID,
				Member:          grant.Member,
				Message:         "selected-runner start has no linked canonical speech",
			})
		case requiresSelectedRunnerDeliveryProof(metadata):
			if deliveryDiagnostic, ok := selectedRunnerDeliveryDiagnostic(metadata, *grant, linkedSpeechEvents); !ok {
				grant.Pass = false
				grant.Status = deliveryDiagnostic.Code
				accounting.Diagnostics = append(accounting.Diagnostics, deliveryDiagnostic)
			} else {
				grant.LinkedRunnerDeliveryEventIDs = appendUniqueString(grant.LinkedRunnerDeliveryEventIDs, deliveryDiagnostic.EventID)
				grant.Pass = true
				grant.Status = "selected_runner_pass"
			}
		default:
			grant.Pass = true
			grant.Status = "selected_runner_pass"
		}
		accounting.SelectedRunnerPass = accounting.SelectedRunnerPass && grant.Pass
	}
	accounting.Diagnostics = uniqueSelectedRunnerDiagnostics(accounting.Diagnostics)
	return accounting
}

func (a *SelectedRunnerAccounting) recordSelectedRunnerTerminal(event EventEnvelope, grantIndex map[string]int, invocationToGrant map[string]string, kind string) {
	grantID := matchingSelectedGrant(event, grantIndex, invocationToGrant)
	if grantID == "" {
		return
	}
	grant := &a.SelectedRunners[grantIndex[grantID]]
	switch kind {
	case "terminal_failure":
		grant.TerminalFailureEventIDs = appendUniqueString(grant.TerminalFailureEventIDs, event.EventID)
	case "terminal_discard":
		grant.TerminalDiscardEventIDs = appendUniqueString(grant.TerminalDiscardEventIDs, event.EventID)
	case "dispatch_failure":
		grant.DispatchFailureEventIDs = appendUniqueString(grant.DispatchFailureEventIDs, event.EventID)
	}
	invocationID := ""
	if event.Runner != nil {
		invocationID = event.Runner.InvocationID
	}
	a.Diagnostics = append(a.Diagnostics, SelectedRunnerDiagnostic{
		Code:               "selected_runner_" + kind,
		EventID:            event.EventID,
		SelectedEventID:    grant.SelectedEventID,
		Member:             firstNonEmptyString(runnerMember(event), payloadStringDefault(event.Payload, "selected_member", "")),
		RunnerInvocationID: invocationID,
		Message:            fmt.Sprintf("%s blocks selected_runner_pass", event.Type),
	})
}

func selectedRunnerEvidenceFromAccounting(accounting SelectedRunnerAccounting, speakerEventID string) SelectedRunnerPrerequisite {
	for _, grant := range accounting.SelectedRunners {
		if grant.SelectedEventID != speakerEventID {
			continue
		}
		if grant.Pass {
			evidence := append([]string{}, grant.RunnerSucceededEventIDs...)
			evidence = append(evidence, grant.LinkedRunnerSpeechEventIDs...)
			evidence = append(evidence, grant.LinkedRunnerDeliveryEventIDs...)
			return SelectedRunnerPrerequisite{Ready: true, Status: "selected_runner_pass", Evidence: uniqueSorted(evidence)}
		}
		evidence := append([]string{}, grant.TerminalFailureEventIDs...)
		evidence = append(evidence, grant.TerminalDiscardEventIDs...)
		evidence = append(evidence, grant.DispatchFailureEventIDs...)
		evidence = append(evidence, grant.RunnerStartEventIDs...)
		evidence = append(evidence, grant.RunnerSucceededEventIDs...)
		evidence = append(evidence, grant.LinkedRunnerSpeechEventIDs...)
		return SelectedRunnerPrerequisite{Ready: false, Status: grant.Status, BlockingReasons: []string{grant.Status}, Evidence: uniqueSorted(evidence)}
	}
	return SelectedRunnerPrerequisite{Ready: false, Status: "missing_selected_runner_prerequisite", BlockingReasons: []string{"missing_selected_runner_prerequisite"}}
}

func selectedRunnerMember(event EventEnvelope) string {
	member := payloadStringDefault(event.Payload, "member", "")
	if member == "" && len(event.To) == 1 {
		member = event.To[0]
	}
	return strings.TrimSpace(member)
}

func matchingSelectedGrant(event EventEnvelope, grantIndex map[string]int, invocationToGrant map[string]string) string {
	if event.Runner != nil && event.Runner.InvocationID != "" {
		if grantID := invocationToGrant[event.Runner.InvocationID]; grantID != "" {
			return grantID
		}
	}
	for _, key := range []string{"selected_event_id", "speaker_selected_event_id", "selected_runner_event_id"} {
		if grantID := payloadStringDefault(event.Payload, key, ""); grantID != "" {
			if _, ok := grantIndex[grantID]; ok {
				return grantID
			}
		}
	}
	if event.CausationEventID != "" {
		if _, ok := grantIndex[event.CausationEventID]; ok {
			return event.CausationEventID
		}
	}
	return ""
}

func selectedGrantForSpeech(event EventEnvelope, grantIndex map[string]int) string {
	for _, key := range []string{"selected_event_id", "speaker_selected_event_id", "selected_runner_event_id"} {
		if grantID := payloadStringDefault(event.Payload, key, ""); grantID != "" {
			if _, ok := grantIndex[grantID]; ok {
				return grantID
			}
		}
	}
	if _, ok := grantIndex[event.CausationEventID]; ok {
		return event.CausationEventID
	}
	return ""
}

func linkedRunnerSpeechMatchesGrant(event EventEnvelope, grant SelectedRunnerGrantAccounting) bool {
	if event.Runner == nil {
		return false
	}
	if event.Runner.Status != "succeeded" {
		return false
	}
	if grant.Member == "" || event.From != grant.Member || event.Runner.Member != grant.Member {
		return false
	}
	if event.Runner.InvocationID == "" || !stringInSlice(event.Runner.InvocationID, grant.RunnerInvocationIDs) {
		return false
	}
	return selectedGrantForSpeech(event, map[string]int{grant.SelectedEventID: 0}) == grant.SelectedEventID
}

func runnerMember(event EventEnvelope) string {
	if event.Runner == nil {
		return ""
	}
	return event.Runner.Member
}

func requiresSelectedRunnerDeliveryProof(metadata *SessionMetadata) bool {
	return metadata != nil && metadata.Surface != nil && metadata.Surface.Kind == "discord_thread"
}

func selectedRunnerDeliveryDiagnostic(metadata *SessionMetadata, grant SelectedRunnerGrantAccounting, linkedSpeechEvents map[string]EventEnvelope) (SelectedRunnerDiagnostic, bool) {
	if !requiresSelectedRunnerDeliveryProof(metadata) {
		return SelectedRunnerDiagnostic{}, true
	}
	for _, eventID := range grant.LinkedRunnerSpeechEventIDs {
		event, ok := linkedSpeechEvents[eventID]
		if !ok {
			continue
		}
		if diagnostic, ok := selectedRunnerSpeechDeliveryDiagnostic(metadata, grant, event); ok {
			return SelectedRunnerDiagnostic{EventID: event.EventID}, true
		} else if diagnostic.Code != "" {
			return diagnostic, false
		}
	}
	return SelectedRunnerDiagnostic{
		Code:            "missing_selected_runner_delivery_evidence",
		SelectedEventID: grant.SelectedEventID,
		Member:          grant.Member,
		Message:         "selected-runner speech has no bound-thread visible delivery evidence",
	}, false
}

func selectedRunnerSpeechDeliveryDiagnostic(metadata *SessionMetadata, grant SelectedRunnerGrantAccounting, event EventEnvelope) (SelectedRunnerDiagnostic, bool) {
	diagnostic := SelectedRunnerDiagnostic{
		SelectedEventID:    grant.SelectedEventID,
		EventID:            event.EventID,
		Member:             grant.Member,
		RunnerInvocationID: firstNonEmptyString(payloadStringDefault(event.Payload, "runner_invocation_id", ""), payloadStringDefault(event.Payload, "invocation_id", "")),
	}
	if event.Runner != nil && strings.TrimSpace(event.Runner.InvocationID) != "" {
		diagnostic.RunnerInvocationID = event.Runner.InvocationID
	}
	surfaceEvidence := anyMap(event.Payload, "surface_evidence", "plugin_evidence", "delivery_evidence", "evidence")
	if surfaceEvidence == nil {
		diagnostic.Code = "missing_selected_runner_delivery_evidence"
		diagnostic.Message = "selected-runner speech has no bound-thread visible delivery evidence"
		return diagnostic, false
	}
	status, _ := deliveryEvidenceStatus(surfaceEvidence)
	if status != "posted" {
		diagnostic.Code = "selected_runner_delivery_status_not_posted"
		diagnostic.Message = fmt.Sprintf("selected-runner speech visible delivery is %s, not posted", status)
		return diagnostic, false
	}
	if strings.TrimSpace(anyString(surfaceEvidence, "kind")) != "discord_thread" {
		diagnostic.Code = "selected_runner_delivery_kind_invalid"
		diagnostic.Message = "selected-runner delivery evidence must identify kind=discord_thread"
		return diagnostic, false
	}
	if expectedPlatform := strings.TrimSpace(metadata.Surface.Platform); expectedPlatform != "" {
		observedPlatform := strings.TrimSpace(anyString(surfaceEvidence, "platform"))
		if observedPlatform == "" {
			diagnostic.Code = "missing_selected_runner_delivery_platform"
			diagnostic.Message = "selected-runner delivery evidence must include platform"
			return diagnostic, false
		}
		if observedPlatform != expectedPlatform {
			diagnostic.Code = "selected_runner_delivery_platform_mismatch"
			diagnostic.Message = "selected-runner delivery evidence platform does not match the configured council surface"
			return diagnostic, false
		}
	}
	if expectedChannelID := strings.TrimSpace(metadata.Surface.ChannelID); expectedChannelID != "" {
		observedChannelID := strings.TrimSpace(anyString(surfaceEvidence, "channel_id"))
		if observedChannelID == "" {
			diagnostic.Code = "missing_selected_runner_delivery_channel_id"
			diagnostic.Message = "selected-runner delivery evidence must include the configured channel_id"
			return diagnostic, false
		}
		if observedChannelID != expectedChannelID {
			diagnostic.Code = "selected_runner_delivery_channel_mismatch"
			diagnostic.Message = "selected-runner delivery evidence channel does not match the configured council channel"
			return diagnostic, false
		}
	}
	if strings.TrimSpace(anyString(surfaceEvidence, "message_id")) == "" {
		diagnostic.Code = "missing_selected_runner_delivery_message_id"
		diagnostic.Message = "selected-runner delivery evidence must include a real message_id"
		return diagnostic, false
	}
	if postingPath := strings.TrimSpace(anyString(surfaceEvidence, "posting_path")); postingPath != "selected_member_profile_send" {
		diagnostic.Code = "selected_runner_delivery_posting_path_invalid"
		diagnostic.Message = "selected-runner delivery evidence must use selected_member_profile_send posting path"
		return diagnostic, false
	}
	if senderMember := strings.TrimSpace(anyString(surfaceEvidence, "sender_member")); senderMember != grant.Member {
		diagnostic.Code = "selected_runner_delivery_sender_mismatch"
		diagnostic.Message = "selected-runner delivery evidence sender_member must match the selected member"
		return diagnostic, false
	}
	if expectedThreadID := strings.TrimSpace(metadata.Surface.ThreadID); expectedThreadID != "" {
		observedThreadID := strings.TrimSpace(anyString(surfaceEvidence, "thread_id"))
		if observedThreadID == "" {
			diagnostic.Code = "missing_selected_runner_delivery_thread_id"
			diagnostic.Message = "selected-runner delivery evidence must include the configured thread_id"
			return diagnostic, false
		}
		if observedThreadID != expectedThreadID {
			diagnostic.Code = "selected_runner_delivery_thread_mismatch"
			diagnostic.Message = "selected-runner delivery evidence thread does not match the configured council thread"
			return diagnostic, false
		}
	}
	if !selectedRunnerDeliveryLinksSpeech(surfaceEvidence, event.EventID) {
		diagnostic.Code = "selected_runner_delivery_unlinked"
		diagnostic.Message = "selected-runner delivery evidence must link to the canonical speech event"
		return diagnostic, false
	}
	return SelectedRunnerDiagnostic{}, true
}

func selectedRunnerDeliveryLinksSpeech(surfaceEvidence map[string]any, speechEventID string) bool {
	for _, key := range []string{"references_event_id", "speech_event_id", "event_id", "source_event_id"} {
		if strings.TrimSpace(anyString(surfaceEvidence, key)) == speechEventID {
			return true
		}
	}
	return false
}
func manualOrFallbackSpeech(event EventEnvelope) bool {
	if event.Type != "speech" || event.Runner != nil {
		return false
	}
	for _, key := range []string{"manual", "manual_speech", "manual_profile", "fallback", "fallback_profile", "fallback_profile_pass", "manual_or_fallback", "runnerless"} {
		if value, ok := event.Payload[key]; ok && truthyOrNonEmpty(value) {
			return true
		}
	}
	for _, key := range []string{"source", "source_kind", "speech_source", "mode", "profile_mode"} {
		value := strings.ToLower(payloadStringDefault(event.Payload, key, ""))
		if strings.Contains(value, "manual") || strings.Contains(value, "fallback") || strings.Contains(value, "runnerless") {
			return true
		}
	}
	return false
}

func truthyOrNonEmpty(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != "" && strings.TrimSpace(typed) != "false"
	default:
		return value != nil
	}
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || stringInSlice(value, values) {
		return values
	}
	return append(values, value)
}

func stringInSlice(value string, values []string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func uniqueSelectedRunnerDiagnostics(in []SelectedRunnerDiagnostic) []SelectedRunnerDiagnostic {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]SelectedRunnerDiagnostic, 0, len(in))
	for _, item := range in {
		key := item.Code + "\x00" + item.EventID + "\x00" + item.SelectedEventID + "\x00" + item.RunnerInvocationID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SelectedEventID != out[j].SelectedEventID {
			return out[i].SelectedEventID < out[j].SelectedEventID
		}
		if out[i].EventID != out[j].EventID {
			return out[i].EventID < out[j].EventID
		}
		return out[i].Code < out[j].Code
	})
	return out
}
