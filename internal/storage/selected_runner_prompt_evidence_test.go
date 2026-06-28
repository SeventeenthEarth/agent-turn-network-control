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
		ConfiguredTimeoutSec: 120,
		EffectiveTimeoutSec:  120,
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
