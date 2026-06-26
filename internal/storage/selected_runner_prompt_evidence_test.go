package storage

import (
	"testing"
	"time"

	"atn-control/internal/protocol"
	"atn-control/internal/registry"
)

func TestUnitNEWFIX001BuildSelectedRunnerPromptEnvelopeBlocksMissingNonAgendaRequiredField(t *testing.T) {
	sessionDir, metadata := createSelectedRunnerAccountingSession(t, "prompt_missing_role")
	appendSelectedRunnerAccountingEvent(t, sessionDir, metadata, EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_agenda_prompt_missing_role",
		CommandID:     "cmd_agenda_prompt_missing_role",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "discussion",
		Type:          "agenda_locked",
		From:          metadata.Moderator,
		To:            []string{"agent-1"},
		CreatedAt:     fixedTranscriptTime(),
		Payload: map[string]any{
			"decision_question":   "What should ship?",
			"success_criteria":    "Produce a canonical typed speech with agenda and prior context.",
			"out_of_scope_policy": "Do not invent agenda text or repair missing control context from plugin hints.",
		},
	})
	speaker := selectedRunnerAccountingSpeakerSelected(metadata, "evt_selected_prompt_missing_role", "agent-1", 1, time.Second)
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

func containsPromptKey(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
