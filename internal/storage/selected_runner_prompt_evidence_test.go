package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"atn-control/internal/protocol"
	"atn-control/internal/registry"
)

func TestUnitNEWFIX001BuildSelectedRunnerPromptEnvelopeBlocksMissingNonAgendaRequiredField(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prompt_missing_role")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prompt_missing_role", fixedTranscriptTime())
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prompt_missing_role", "agent-1", 1, time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" {
		t.Fatalf("missing role_assignment must block prompt evidence: %#v", envelope.Evidence)
	}
	if !containsPromptKey(envelope.Evidence.MissingRequiredContext, "role_assignment") {
		t.Fatalf("missing role_assignment should be explicit: %#v", envelope.Evidence)
	}
}

func TestUnitNEWFIX004SelectedRunnerPromptEvidenceDefaultsOwnHistoryFieldsForOlderPayload(t *testing.T) {
	index := &LogIndex{Events: []EventEnvelope{{
		Type:             selectedRunnerPromptEvidenceEventType,
		CausationEventID: "evt_selected_legacy",
		Payload: map[string]any{
			"session_id":                     "sess_legacy",
			"selected_member":                "agent-1",
			"result":                         "pass",
			"included_context":               []any{"session_id", "prior_claim_context"},
			"prior_context_source_event_ids": []any{"evt_prior_1"},
		},
	}}}

	evidence := LatestSelectedRunnerPromptEvidenceFromIndex(index)
	if evidence == nil {
		t.Fatal("LatestSelectedRunnerPromptEvidenceFromIndex should return legacy evidence")
	}
	if evidence.SpeakerSelectedEventID != "evt_selected_legacy" {
		t.Fatalf("legacy causation fallback should populate speaker_selected_event_id: %#v", evidence)
	}
	if len(evidence.OwnHistorySourceEventIDs) != 0 || len(evidence.OwnLatestClaimSourceEventIDs) != 0 || len(evidence.OwnClaimIndexSourceEventIDs) != 0 {
		t.Fatalf("legacy payload should default new own-history fields to empty slices: %#v", evidence)
	}
}

func TestUnitNEWFIX004BuildSelectedRunnerPromptEnvelopeIncludesOwnHistoryWithoutClaimsBlocking(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "own_history_speech_only")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_own_history_speech_only", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_own_history_1", "agent-1", map[string]any{"turn": 1, "speech": "Earlier selected-member speech."}, time.Second)
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_own_history_2", "agent-1", map[string]any{"turn": 3, "speech": "Latest selected-member speech without claims."}, 3*time.Second)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_own_history_speech_only", "agent-1", 4, 4*time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "pass" {
		t.Fatalf("speech-only own history should not block: %#v", envelope.Evidence)
	}
	if !containsPromptKey(envelope.Evidence.IncludedContext, "selected_member_prior_speeches") {
		t.Fatalf("selected_member_prior_speeches should be included: %#v", envelope.Evidence)
	}
	if containsPromptKey(envelope.Evidence.IncludedContext, "selected_member_latest_claims") || containsPromptKey(envelope.Evidence.IncludedContext, "selected_member_claim_index") {
		t.Fatalf("claim-specific own-history context should stay absent when no prior claims exist: %#v", envelope.Evidence)
	}
	if len(envelope.Evidence.OwnHistorySourceEventIDs) != 2 {
		t.Fatalf("own history should record both selected-member speech sources: %#v", envelope.Evidence)
	}
	if len(envelope.Evidence.OwnLatestClaimSourceEventIDs) != 0 || len(envelope.Evidence.OwnClaimIndexSourceEventIDs) != 0 {
		t.Fatalf("speech-only own history should not emit claim provenance fields: %#v", envelope.Evidence)
	}
	first := "event_id=evt_prior_own_history_1 member=agent-1 turn=1 speech=Earlier selected-member speech."
	second := "event_id=evt_prior_own_history_2 member=agent-1 turn=3 speech=Latest selected-member speech without claims."
	firstAt := strings.Index(envelope.Prompt, first)
	secondAt := strings.Index(envelope.Prompt, second)
	if firstAt == -1 || secondAt == -1 {
		t.Fatalf("own-history prompt should carry source-backed prior speeches:\n%s", envelope.Prompt)
	}
	if firstAt > secondAt {
		t.Fatalf("own-history prompt should preserve oldest-to-newest order:\n%s", envelope.Prompt)
	}
}

func TestUnitNEWFIX004BuildSelectedRunnerPromptEnvelopeBlocksMalformedOwnHistorySpeech(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "own_history_bad_speech")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_own_history_bad_speech", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_bad_own_history_speech", "agent-1", map[string]any{"turn": 1}, time.Second)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_own_history_bad_speech", "agent-1", 2, 2*time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" {
		t.Fatalf("malformed own-history speech must block: %#v", envelope.Evidence)
	}
	if !containsPromptKey(envelope.Evidence.MissingRequiredContext, "selected_member_prior_speeches") {
		t.Fatalf("malformed own-history speech should record selected_member_prior_speeches: %#v", envelope.Evidence)
	}
}

func TestUnitNEWFIX004BuildSelectedRunnerPromptEnvelopeBlocksOwnHistorySpeechWithInvalidTurnProvenance(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "own_history_bad_turn")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_own_history_bad_turn", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_bad_own_history_turn", "agent-1", map[string]any{"turn": "bad", "speech": "Selected-member speech with invalid turn provenance."}, time.Second)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_own_history_bad_turn", "agent-1", 2, 2*time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" {
		t.Fatalf("own-history speech with invalid turn provenance must block: %#v", envelope.Evidence)
	}
	if !containsPromptKey(envelope.Evidence.MissingRequiredContext, "selected_member_prior_speeches") {
		t.Fatalf("invalid turn provenance should record selected_member_prior_speeches: %#v", envelope.Evidence)
	}
	if strings.Contains(envelope.Prompt, "selected_member_prior_speeches:") {
		t.Fatalf("corrupt own-history speech must not survive into selected_member_prior_speeches:\n%s", envelope.Prompt)
	}
}

func TestUnitNEWFIX004BuildSelectedRunnerPromptEnvelopeUsesTopLevelTurnWhenPayloadTurnAbsent(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "own_history_top_level_turn")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_own_history_top_level_turn", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEventWithTopLevelTurn(t, sessionDir, metadata, "evt_prior_top_level_turn", "agent-1", 1, map[string]any{"speech": "Selected-member speech using top-level turn fallback."}, time.Second)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_own_history_top_level_turn", "agent-1", 2, 2*time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "pass" {
		t.Fatalf("top-level turn fallback should preserve valid own-history provenance: %#v", envelope.Evidence)
	}
	want := "event_id=evt_prior_top_level_turn member=agent-1 turn=1 speech=Selected-member speech using top-level turn fallback."
	if !strings.Contains(envelope.Prompt, want) {
		t.Fatalf("top-level turn fallback should appear in the prompt:\n%s", envelope.Prompt)
	}
}

func TestUnitNEWFIX004BuildSelectedRunnerPromptEnvelopeBlocksMalformedOwnHistoryClaims(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "own_history_bad_claims")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_own_history_bad_claims", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_bad_own_history_claims", "agent-1", map[string]any{
		"turn":   1,
		"speech": "Selected-member claim-bearing speech.",
		"claims": []any{map[string]any{"claim_id": "T1.C1"}},
	}, time.Second)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_own_history_bad_claims", "agent-1", 2, 2*time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" {
		t.Fatalf("malformed own-history claims must block: %#v", envelope.Evidence)
	}
	for _, want := range []string{"selected_member_latest_claims", "selected_member_claim_index"} {
		if !containsPromptKey(envelope.Evidence.MissingRequiredContext, want) {
			t.Fatalf("malformed own-history claims should record %q: %#v", want, envelope.Evidence)
		}
	}
}

func TestUnitNEWFIX004BuildSelectedRunnerPromptEnvelopeUsesLatestValidOwnHistoryClaimsWhenOlderClaimsMalformed(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "own_history_mixed_claim_validity")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_own_history_mixed_claim_validity", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_bad_own_history_claims_old", "agent-1", map[string]any{
		"turn":   1,
		"speech": "Older selected-member claim-bearing speech.",
		"claims": []any{map[string]any{"claim_id": "T1.C1"}},
	}, time.Second)
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_valid_own_history_claims_new", "agent-1", map[string]any{
		"turn":   2,
		"speech": "Newer selected-member claim-bearing speech.",
		"claims": []any{map[string]any{"claim_id": "T2.C1", "summary": "Use the latest valid claim-bearing speech for latest-claims continuity."}},
	}, 2*time.Second)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_own_history_mixed_claim_validity", "agent-1", 3, 3*time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" {
		t.Fatalf("mixed-validity own-history claims should still block on claim-index incompleteness: %#v", envelope.Evidence)
	}
	if !containsPromptKey(envelope.Evidence.IncludedContext, "selected_member_latest_claims") {
		t.Fatalf("latest valid own-history claims should remain included: %#v", envelope.Evidence)
	}
	if containsPromptKey(envelope.Evidence.MissingRequiredContext, "selected_member_latest_claims") {
		t.Fatalf("older malformed claims must not poison latest valid own-history claims: %#v", envelope.Evidence)
	}
	if !containsPromptKey(envelope.Evidence.MissingRequiredContext, "selected_member_claim_index") {
		t.Fatalf("mixed-validity own-history claims should still block on claim-index completeness: %#v", envelope.Evidence)
	}
	if len(envelope.Evidence.OwnLatestClaimSourceEventIDs) != 1 || envelope.Evidence.OwnLatestClaimSourceEventIDs[0] != "evt_prior_valid_own_history_claims_new" {
		t.Fatalf("latest own-history claim provenance should point at the latest valid claim-bearing speech: %#v", envelope.Evidence)
	}
	if !strings.Contains(envelope.Prompt, "selected_member_latest_claims: event=evt_prior_valid_own_history_claims_new claims=T2.C1:Use the latest valid claim-bearing speech for latest-claims continuity.") {
		t.Fatalf("latest valid own-history claims should stay source-backed in the prompt:\n%s", envelope.Prompt)
	}
	if strings.Contains(envelope.Prompt, "selected_member_claim_index:") {
		t.Fatalf("claim-index prompt content should stay absent while malformed history keeps the index blocked:\n%s", envelope.Prompt)
	}
}

func TestUnitNEWFIX004BuildSelectedRunnerPromptEnvelopeBlocksStaleLatestClaimsWhenNewerOwnClaimsMalformed(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "own_history_newer_malformed_claims")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_own_history_newer_malformed_claims", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_valid_own_history_claims_old", "agent-1", map[string]any{
		"turn":   1,
		"speech": "Older valid selected-member claim-bearing speech.",
		"claims": []any{map[string]any{"claim_id": "T1.C1", "summary": "Do not present stale latest claims when a newer claim-bearing event is malformed."}},
	}, time.Second)
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_bad_own_history_claims_new", "agent-1", map[string]any{
		"turn":   2,
		"speech": "Newer malformed selected-member claim-bearing speech.",
		"claims": []any{map[string]any{"claim_id": "T2.C1"}},
	}, 2*time.Second)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_own_history_newer_malformed_claims", "agent-1", 3, 3*time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" {
		t.Fatalf("newer malformed own-history claims must block: %#v", envelope.Evidence)
	}
	if containsPromptKey(envelope.Evidence.IncludedContext, "selected_member_latest_claims") {
		t.Fatalf("newer malformed claim-bearing speech must clear stale latest claims: %#v", envelope.Evidence)
	}
	for _, want := range []string{"selected_member_latest_claims", "selected_member_claim_index"} {
		if !containsPromptKey(envelope.Evidence.MissingRequiredContext, want) {
			t.Fatalf("newer malformed claim-bearing speech should record %q: %#v", want, envelope.Evidence)
		}
	}
	if len(envelope.Evidence.OwnLatestClaimSourceEventIDs) != 0 {
		t.Fatalf("stale latest-claim provenance must be cleared when the newest claim-bearing speech is malformed: %#v", envelope.Evidence)
	}
	if strings.Contains(envelope.Prompt, "selected_member_latest_claims:") {
		t.Fatalf("stale latest-claim prompt content must not survive a newer malformed claim-bearing speech:\n%s", envelope.Prompt)
	}
}

func TestUnitPRSLR005BuildSelectedRunnerPromptEnvelopeMarksPersistentDeltaAndRejectsHotFallbackLanes(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prslr005_persistent_delta")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prslr005_delta", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_prior_prslr005_delta", "agent-1", map[string]any{
		"turn":   1,
		"speech": "Prior selected-member speech retained only as rehydrate/audit provenance.",
		"claims": []any{map[string]any{"claim_id": "T1.C1", "summary": "Prior claim is ARGUE provenance, not a full-history hot lane."}},
	}, time.Second)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prslr005_delta", "agent-1", 2, 2*time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "pass" {
		t.Fatalf("persistent_delta hot prompt should pass when delta authority is valid: %#v", envelope.Evidence)
	}
	if envelope.Evidence.ParticipantRuntimeMode != "persistent_delta" || envelope.Evidence.HotPromptStrategy != "delta_only" {
		t.Fatalf("prompt evidence should mark persistent delta hot path: %#v", envelope.Evidence)
	}
	if envelope.Evidence.StatelessFallback || envelope.Evidence.StatelessFallbackStatus != "disabled" {
		t.Fatalf("stateless fallback must be explicitly disabled: %#v", envelope.Evidence)
	}
	if envelope.Evidence.FullHistoryHotPromptStatus != "rejected" || envelope.Evidence.OwnHistoryHotPromptStatus != "rejected" {
		t.Fatalf("full-history/own-history hot lanes must be rejected: %#v", envelope.Evidence)
	}
	if envelope.Evidence.RehydrateValidationStatus != "not_required_hot_delta" || envelope.Evidence.RuntimeBlockStatus != "not_blocked" {
		t.Fatalf("hot delta should not require rehydrate or block runtime: %#v", envelope.Evidence)
	}
	for _, want := range []string{"evt_agenda_prslr005_delta", "evt_prior_prslr005_delta", "evt_selected_prslr005_delta"} {
		if !containsPromptKey(envelope.Evidence.DeltaSourceEventIDs, want) {
			t.Fatalf("delta source refs should include %q: %#v", want, envelope.Evidence)
		}
	}
	if !containsPromptKey(envelope.Evidence.RehydrateSourceEventIDs, "evt_prior_prslr005_delta") {
		t.Fatalf("prior own-history should remain rehydrate/audit provenance, not hot fallback: %#v", envelope.Evidence)
	}
	for _, want := range []string{
		"participant_runtime_mode: persistent_delta",
		"hot_prompt_strategy: delta_only",
		"stateless_fallback: false",
		"full_history_hot_prompt_status: rejected",
		"own_history_hot_prompt_status: rejected",
	} {
		if !strings.Contains(envelope.Prompt, want) {
			t.Fatalf("prompt missing PRSLR-005 marker %q:\n%s", want, envelope.Prompt)
		}
	}
}

func TestUnitPRSLR005BuildSelectedRunnerPromptEnvelopeValidatesMatchingRehydrateIdentity(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prslr005_rehydrate_valid")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prslr005_rehydrate_valid", fixedTranscriptTime())
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prslr005_rehydrate_valid", "agent-1", 1, time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	speaker.Payload["participant_runtime_rehydrate_required"] = true
	speaker.Payload["participant_runtime_rehydrate"] = map[string]any{
		"session_id":             metadata.ID,
		"member":                 "agent-1",
		"cursor":                 CursorFor(0, "evt_agenda_prslr005_rehydrate_valid"),
		"ack_cursor":             CursorFor(0, "evt_agenda_prslr005_rehydrate_valid"),
		"participant_handle":     "handle-valid",
		"expected_handle":        "handle-valid",
		"generation":             3,
		"expected_generation":    3,
		"source_event_ids":       []any{"evt_agenda_prslr005_rehydrate_valid"},
		"argue_source_event_ids": []any{},
	}
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "pass" {
		t.Fatalf("matching rehydrate identity should pass: %#v", envelope.Evidence)
	}
	if envelope.Evidence.RehydrateValidationStatus != "validated" || envelope.Evidence.RuntimeBlockStatus != "not_blocked" || len(envelope.Evidence.RehydrateValidationFailures) != 0 {
		t.Fatalf("matching rehydrate identity should validate without failures: %#v", envelope.Evidence)
	}
	if !containsPromptKey(envelope.Evidence.RehydrateSourceEventIDs, "evt_agenda_prslr005_rehydrate_valid") {
		t.Fatalf("validated rehydrate should preserve source refs: %#v", envelope.Evidence)
	}
}

func TestUnitPRSLR005BuildSelectedRunnerPromptEnvelopeBlocksRehydrateIdentityMismatch(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prslr005_rehydrate_mismatch")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prslr005_rehydrate", fixedTranscriptTime())
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prslr005_rehydrate", "agent-1", 1, time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	speaker.Payload["participant_runtime_rehydrate_required"] = true
	speaker.Payload["participant_runtime_rehydrate"] = map[string]any{
		"session_id":             "wrong-session",
		"member":                 "agent-other",
		"cursor":                 "cur_000001_evt_prior",
		"ack_cursor":             "cur_000999_evt_other",
		"participant_handle":     "handle-a",
		"expected_handle":        "handle-b",
		"generation":             2,
		"expected_generation":    3,
		"source_event_ids":       []any{"evt_agenda_prslr005_rehydrate"},
		"argue_source_event_ids": []any{"evt_missing_argue"},
	}
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" {
		t.Fatalf("rehydrate identity mismatch must block prompt evidence: %#v", envelope.Evidence)
	}
	if envelope.Evidence.RehydrateValidationStatus != "failed" || envelope.Evidence.RuntimeBlockStatus != "blocked_rehydrate_failed" {
		t.Fatalf("rehydrate failure should block whole runtime/council: %#v", envelope.Evidence)
	}
	if !containsPromptKey(envelope.Evidence.MissingRequiredContext, "rehydrate_identity_validation") {
		t.Fatalf("missing context should record rehydrate identity failure: %#v", envelope.Evidence)
	}
	for _, want := range []string{"session_id_mismatch", "member_mismatch", "cursor_malformed", "ack_cursor_mismatch", "participant_handle_mismatch", "generation_mismatch", "argue_source_event_id_unresolved"} {
		if !containsPromptKey(envelope.Evidence.RehydrateValidationFailures, want) {
			t.Fatalf("rehydrate failure missing %q: %#v", want, envelope.Evidence)
		}
	}
	if !strings.Contains(envelope.Prompt, "runtime_block_status: blocked_rehydrate_failed") {
		t.Fatalf("blocked prompt should disclose runtime block status:\n%s", envelope.Prompt)
	}
}

func TestUnitPRSLR005BuildSelectedRunnerPromptEnvelopeBlocksStaleEqualAckCursorAndMissingRefs(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prslr005_rehydrate_stale_cursor")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prslr005_stale_cursor", fixedTranscriptTime())
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prslr005_stale_cursor", "agent-1", 1, time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	speaker.Payload["participant_runtime_rehydrate_required"] = true
	speaker.Payload["participant_runtime_rehydrate"] = map[string]any{
		"session_id":             metadata.ID,
		"member":                 "agent-1",
		"cursor":                 CursorFor(99, "evt_missing_cursor"),
		"ack_cursor":             CursorFor(99, "evt_missing_cursor"),
		"participant_handle":     "handle-stale",
		"expected_handle":        "handle-stale",
		"generation":             1,
		"expected_generation":    1,
		"source_event_ids":       []any{},
		"argue_source_event_ids": []any{},
	}
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" || envelope.Evidence.RuntimeBlockStatus != "blocked_rehydrate_failed" {
		t.Fatalf("stale matching cursor and missing refs must block: %#v", envelope.Evidence)
	}
	for _, want := range []string{"cursor_authority_mismatch", "source_event_ids_missing", "cursor_source_ref_mismatch"} {
		if !containsPromptKey(envelope.Evidence.RehydrateValidationFailures, want) {
			t.Fatalf("rehydrate stale cursor failure missing %q: %#v", want, envelope.Evidence)
		}
	}
}

func TestUnitPRSLR005BuildSelectedRunnerPromptEnvelopeBlocksCurrentAuthorityMismatch(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prslr005_rehydrate_source_mismatch")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prslr005_source_mismatch", fixedTranscriptTime())
	appendSelectedRunnerPromptSpeechEvent(t, sessionDir, metadata, "evt_other_prslr005_source_mismatch", "agent-1", map[string]any{"speech": "other source"}, 500*time.Millisecond)
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prslr005_source_mismatch", "agent-1", 1, time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	speaker.Payload["participant_runtime_rehydrate_required"] = true
	speaker.Payload["participant_runtime_rehydrate"] = map[string]any{
		"session_id":             metadata.ID,
		"member":                 "agent-1",
		"cursor":                 CursorFor(0, "evt_agenda_prslr005_source_mismatch"),
		"ack_cursor":             CursorFor(0, "evt_agenda_prslr005_source_mismatch"),
		"participant_handle":     "handle-valid",
		"expected_handle":        "handle-valid",
		"generation":             1,
		"expected_generation":    1,
		"source_event_ids":       []any{"evt_other_prslr005_source_mismatch"},
		"argue_source_event_ids": []any{},
	}
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" || !containsPromptKey(envelope.Evidence.RehydrateValidationFailures, "cursor_source_ref_mismatch") {
		t.Fatalf("cursor authority source mismatch must block: %#v", envelope.Evidence)
	}
}

func TestUnitPRSLR005BuildSelectedRunnerPromptEnvelopeBlocksMissingPersistentRuntimeMemberRow(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prslr005_rehydrate_producer")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prslr005_producer", fixedTranscriptTime())
	producerTurn := 1
	producerRuntimeEvent := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_speech_prslr005_producer",
		CommandID:     "cmd_evt_speech_prslr005_producer",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Turn:          &producerTurn,
		Phase:         "discussion",
		Type:          "speech",
		From:          "agent-1",
		To:            []string{metadata.Moderator},
		CreatedAt:     fixedTranscriptTime().Add(500 * time.Millisecond),
		Payload: map[string]any{
			"speech": "prior speech with runtime coverage",
			"persistent_participant_runtime_evidence": map[string]any{
				"status":          "coverage_complete",
				"speech_event_id": "evt_speech_prslr005_producer",
				"coverage_cursor": CursorFor(1, "evt_speech_prslr005_producer"),
				"members": []any{map[string]any{
					"member":                     "agent-other",
					"participant_session_handle": "handle-derived",
					"generation":                 1,
					"last_cursor":                CursorFor(1, "evt_speech_prslr005_producer"),
					"last_event_id":              "evt_speech_prslr005_producer",
				}},
			},
		},
	}
	content, err := json.Marshal(producerRuntimeEvent)
	if err != nil {
		t.Fatalf("marshal producer runtime event: %v", err)
	}
	channel, err := os.OpenFile(filepath.Join(sessionDir, ChannelJSONLName), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open channel for producer runtime event: %v", err)
	}
	if _, err := channel.Write(append(content, '\n')); err != nil {
		_ = channel.Close()
		t.Fatalf("write producer runtime event: %v", err)
	}
	if err := channel.Close(); err != nil {
		t.Fatalf("close producer runtime event channel: %v", err)
	}
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prslr005_producer", "agent-1", 2, time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" || envelope.Evidence.RehydrateValidationStatus != "failed" || envelope.Evidence.RuntimeBlockStatus != "blocked_rehydrate_failed" {
		t.Fatalf("missing persistent runtime member row should produce rehydrate block before runner: %#v", envelope.Evidence)
	}
	for _, want := range []string{"cursor_missing", "source_event_ids_missing"} {
		if !containsPromptKey(envelope.Evidence.RehydrateValidationFailures, want) {
			t.Fatalf("derived rehydrate block missing %q: %#v", want, envelope.Evidence)
		}
	}
}

func TestUnitPRSLR005BuildSelectedRunnerPromptEnvelopeBlocksStalePersistentRuntimeMemberCursor(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prslr005_rehydrate_stale_runtime_member_cursor")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prslr005_runtime_stale", fixedTranscriptTime())
	staleTurn := 1
	staleRuntimeEvent := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_speech_prslr005_runtime_stale",
		CommandID:     "cmd_evt_speech_prslr005_runtime_stale",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Turn:          &staleTurn,
		Phase:         "discussion",
		Type:          "speech",
		From:          "agent-1",
		To:            []string{metadata.Moderator},
		CreatedAt:     fixedTranscriptTime().Add(500 * time.Millisecond),
		Payload: map[string]any{
			"speech": "prior speech with stale persistent runtime coverage",
			"persistent_participant_runtime_evidence": map[string]any{
				"status":          "coverage_complete",
				"speech_event_id": "evt_speech_prslr005_runtime_stale",
				"coverage_cursor": CursorFor(1, "evt_speech_prslr005_runtime_stale"),
				"members": []any{map[string]any{
					"member":                     "agent-1",
					"participant_session_handle": "handle-derived",
					"generation":                 1,
					"last_cursor":                CursorFor(99, "evt_missing_runtime_cursor"),
					"last_event_id":              "evt_missing_runtime_cursor",
				}},
			},
		},
	}
	content, err := json.Marshal(staleRuntimeEvent)
	if err != nil {
		t.Fatalf("marshal stale runtime event: %v", err)
	}
	channel, err := os.OpenFile(filepath.Join(sessionDir, ChannelJSONLName), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open channel for stale runtime event: %v", err)
	}
	if _, err := channel.Write(append(content, '\n')); err != nil {
		_ = channel.Close()
		t.Fatalf("write stale runtime event: %v", err)
	}
	if err := channel.Close(); err != nil {
		t.Fatalf("close stale runtime event channel: %v", err)
	}
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prslr005_runtime_stale", "agent-1", 2, time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)

	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "assignee"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" || envelope.Evidence.RehydrateValidationStatus != "failed" || envelope.Evidence.RuntimeBlockStatus != "blocked_rehydrate_failed" {
		t.Fatalf("stale selected-member persistent runtime cursor should produce rehydrate block before runner: %#v", envelope.Evidence)
	}
	for _, want := range []string{"cursor_authority_mismatch", "source_event_id_unresolved"} {
		if !containsPromptKey(envelope.Evidence.RehydrateValidationFailures, want) {
			t.Fatalf("derived stale-runtime rehydrate block missing %q: %#v", want, envelope.Evidence)
		}
	}
}

func TestUnitPRSLR005BuildSelectedRunnerPromptEnvelopeBlocksMismatchedPersistentRuntimeCursorSource(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prslr005_rehydrate_mismatch_runtime_member_cursor_source")
	appendSelectedRunnerPromptAgendaEvent(t, sessionDir, metadata, "evt_agenda_prslr005_runtime_mismatch", fixedTranscriptTime())
	mismatchTurn := 1
	mismatchRuntimeEvent := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_speech_prslr005_runtime_mismatch",
		CommandID:     "cmd_evt_speech_prslr005_runtime_mismatch",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Turn:          &mismatchTurn,
		Phase:         "discussion",
		Type:          "speech",
		From:          "agent-1",
		To:            []string{metadata.Moderator},
		CreatedAt:     fixedTranscriptTime().Add(500 * time.Millisecond),
		Payload: map[string]any{
			"speech": "prior speech with mismatched persistent runtime cursor/source",
			"persistent_participant_runtime_evidence": map[string]any{
				"status":          "coverage_complete",
				"speech_event_id": "evt_speech_prslr005_runtime_mismatch",
				"coverage_cursor": CursorFor(1, "evt_speech_prslr005_runtime_mismatch"),
				"members": []any{map[string]any{
					"member":                     "agent-1",
					"participant_session_handle": "handle-derived",
					"generation":                 1,
					"last_cursor":                CursorFor(1, "evt_speech_prslr005_runtime_mismatch"),
					"last_event_id":              "evt_agenda_prslr005_runtime_mismatch",
				}},
			},
		},
	}
	content, err := json.Marshal(mismatchRuntimeEvent)
	if err != nil {
		t.Fatalf("marshal mismatch runtime event: %v", err)
	}
	channel, err := os.OpenFile(filepath.Join(sessionDir, ChannelJSONLName), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open channel for mismatch runtime event: %v", err)
	}
	if _, err := channel.Write(append(content, '\n')); err != nil {
		_ = channel.Close()
		t.Fatalf("write mismatch runtime event: %v", err)
	}
	if err := channel.Close(); err != nil {
		t.Fatalf("close mismatch runtime event channel: %v", err)
	}
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prslr005_runtime_mismatch", "agent-1", 2, time.Second)
	speaker.Payload["stance_assignment"] = "answer"
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, speaker)
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	envelope, err := BuildSelectedRunnerPromptEnvelope(metadata, index, speaker, registry.Member{ID: "agent-1", Role: "participant"})
	if err != nil {
		t.Fatalf("BuildSelectedRunnerPromptEnvelope: %v", err)
	}
	if envelope.Evidence.Result != "blocked" || envelope.Evidence.RehydrateValidationStatus != "failed" || envelope.Evidence.RuntimeBlockStatus != "blocked_rehydrate_failed" || !containsString(envelope.Evidence.RehydrateValidationFailures, "cursor_source_ref_mismatch") {
		t.Fatalf("mismatched selected-member persistent runtime cursor/source should block before runner: %#v", envelope.Evidence)
	}
}

func TestUnitPRSLR005SelectedRunnerPromptEvidenceSummaryRendersRehydrateFields(t *testing.T) {
	evidence := &SelectedRunnerPromptEvidence{
		SessionID:                   "sess_transcript_prslr005",
		SpeakerSelectedEventID:      "evt_selected_transcript_prslr005",
		SelectedMember:              "agent-1",
		Turn:                        3,
		Result:                      "blocked",
		ParticipantRuntimeMode:      "persistent_delta",
		HotPromptStrategy:           "delta_only",
		DeltaSourceEventIDs:         []string{"evt_delta"},
		RehydrateSourceEventIDs:     []string{"evt_rehydrate"},
		RehydrateValidationStatus:   "failed",
		RehydrateValidationFailures: []string{"cursor_authority_mismatch", "source_event_ids_missing"},
		StatelessFallback:           false,
		StatelessFallbackStatus:     "disabled",
		FullHistoryHotPromptStatus:  "rejected",
		OwnHistoryHotPromptStatus:   "rejected",
		RuntimeBlockStatus:          "blocked_rehydrate_failed",
		RuntimeBlockReason:          "rehydrate_identity_validation_failed",
	}
	var b strings.Builder
	renderSelectedRunnerPromptEvidenceSummary(&b, evidence)
	transcript := b.String()
	for _, want := range []string{"participant_runtime_mode", "hot_prompt_strategy", "delta_source_event_ids", "rehydrate_source_event_ids", "rehydrate_validation_status", "rehydrate_validation_failures", "stateless_fallback", "stateless_fallback_status", "full_history_hot_prompt_status", "own_history_hot_prompt_status", "runtime_block_status", "runtime_block_reason"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("transcript prompt evidence summary missing %q:\n%s", want, transcript)
		}
	}
}

func TestUnitNEWFIX005CouncilStatusAndExportBundleProjectSelectedRunnerTimeoutEvidence(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Title = "NEWFIX-005 timeout evidence projection"
	metadata.Surface = &Surface{Kind: "discord_thread", Platform: "discord", ChannelID: "chan-timeout", ThreadID: "thread-timeout"}
	metadata.SelectedRunnerTimeoutEvidence = &SelectedRunnerTimeoutEvidence{
		PolicyRequired:       true,
		ConfiguredTimeoutSec: 150,
		EffectiveTimeoutSec:  150,
		EffectiveSource:      "session_limit",
		ApprovedAlternative:  true,
		ApprovalBasis:        "Approved live-visible exception.",
		Compliant:            true,
	}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, registry.SnapshotFileName), []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, ChannelJSONLName), nil, 0o600); err != nil {
		t.Fatalf("write empty channel log: %v", err)
	}

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	statusEvidence, ok := status["selected_runner_timeout_evidence"].(SelectedRunnerTimeoutEvidence)
	if !ok {
		t.Fatalf("council status selected_runner_timeout_evidence missing or wrong type: %#v", status["selected_runner_timeout_evidence"])
	}
	if !reflect.DeepEqual(statusEvidence, *metadata.SelectedRunnerTimeoutEvidence) {
		t.Fatalf("council status selected_runner_timeout_evidence mismatch: got %#v want %#v", statusEvidence, *metadata.SelectedRunnerTimeoutEvidence)
	}

	result, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(result.BundleDir, "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal bundle_manifest.json: %v\n%s", err, string(manifestBytes))
	}
	manifestEvidence, ok := manifest["selected_runner_timeout_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("bundle manifest selected_runner_timeout_evidence missing: %#v", manifest["selected_runner_timeout_evidence"])
	}
	wantManifestEvidence := map[string]any{
		"policy_required":        true,
		"configured_timeout_sec": float64(150),
		"effective_timeout_sec":  float64(150),
		"effective_source":       "session_limit",
		"approved_alternative":   true,
		"approval_basis":         "Approved live-visible exception.",
		"compliant":              true,
	}
	if !reflect.DeepEqual(manifestEvidence, wantManifestEvidence) {
		t.Fatalf("bundle manifest selected_runner_timeout_evidence mismatch: got %#v want %#v", manifestEvidence, wantManifestEvidence)
	}
}

func TestUnitNEWFIX005StreamStatusLeavesTimeoutEvidenceOutOfScope(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.SessionType = SessionTypeCouncil
	metadata.Surface = &Surface{Kind: "discord_thread", Platform: "discord", ChannelID: "chan-timeout", ThreadID: "thread-timeout"}
	metadata.SelectedRunnerTimeoutEvidence = &SelectedRunnerTimeoutEvidence{
		PolicyRequired:       true,
		ConfiguredTimeoutSec: 150,
		EffectiveTimeoutSec:  150,
		EffectiveSource:      "session_limit",
		ApprovedAlternative:  false,
		Compliant:            true,
	}
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, ChannelJSONLName), nil, 0o600); err != nil {
		t.Fatalf("write empty channel log: %v", err)
	}

	status, err := StreamStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("StreamStatusFromLog: %v", err)
	}
	if strings.Contains(mustJSON(t, status), "selected_runner_timeout_evidence") {
		t.Fatalf("stream.status must not expose selected_runner_timeout_evidence: %s", mustJSON(t, status))
	}
}
func appendSelectedRunnerPromptAgendaEvent(t *testing.T, sessionDir string, metadata *SessionMetadata, eventID string, createdAt time.Time) {
	t.Helper()
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventID,
		CommandID:     "cmd_" + eventID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "agenda_locked",
		From:          metadata.Moderator,
		To:            []string{"agent-1"},
		CreatedAt:     createdAt,
		Payload: map[string]any{
			"decision_question":   "What should ship?",
			"success_criteria":    "Produce a canonical typed speech with agenda and prior context.",
			"out_of_scope_policy": "Do not invent agenda text or repair missing control context from plugin hints.",
		},
	})
}

func appendSelectedRunnerPromptSpeechEvent(t *testing.T, sessionDir string, metadata *SessionMetadata, eventID, member string, payload map[string]any, offset time.Duration) {
	t.Helper()
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventID,
		CommandID:     "cmd_" + eventID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "speech",
		From:          member,
		To:            []string{metadata.Moderator},
		CreatedAt:     fixedTranscriptTime().Add(offset),
		Payload:       payload,
	})
}

func appendSelectedRunnerPromptSpeechEventWithTopLevelTurn(t *testing.T, sessionDir string, metadata *SessionMetadata, eventID, member string, turn int, payload map[string]any, offset time.Duration) {
	t.Helper()
	eventTurn := turn
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventID,
		CommandID:     "cmd_" + eventID,
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Turn:          &eventTurn,
		Phase:         "discussion",
		Type:          "speech",
		From:          member,
		To:            []string{metadata.Moderator},
		CreatedAt:     fixedTranscriptTime().Add(offset),
		Payload:       payload,
	})
}

func containsPromptKey(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
