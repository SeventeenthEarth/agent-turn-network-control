package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kkachi-agent-network-control/internal/memberruntime"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/runner"
	"kkachi-agent-network-control/internal/storage"
)

// SelectedSpeakerDispatchHandler is the MEMBR-002 bounded pilot seam: a real
// speaker_selected stream event for the configured member is turned into one
// bounded runner invocation through that member's resolved registry wrapper.
type SelectedSpeakerDispatchHandler struct {
	SessionDir    string
	Metadata      *storage.SessionMetadata
	Member        registry.Member
	Adapter       runner.Adapter
	Runtime       registry.Runtime
	Locks         *DispatchLocks
	Now           func() time.Time
	MaxRetries    int
	Timeout       time.Duration
	PromptBuilder func(storage.EventEnvelope, registry.Member) string
}

func (h SelectedSpeakerDispatchHandler) Handle(ctx context.Context, frame storage.StreamFrame) error {
	event := frame.Event
	if event.Type != "speaker_selected" {
		return nil
	}
	selected, err := selectedSpeakerMember(event)
	if err != nil {
		return err
	}
	if selected == "" {
		return fmt.Errorf("speaker_selected missing selected member")
	}
	if selected != h.Member.ID {
		return nil
	}
	commandID := strings.TrimSpace(event.CommandID)
	if commandID == "" {
		return fmt.Errorf("speaker_selected missing command_id")
	}
	prompt := h.promptFor(event)
	if h.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.Timeout)
		defer cancel()
	}
	dispatchMetadata := *h.Metadata
	dispatchMetadata.State.Phase = event.Phase
	if event.Turn != nil {
		dispatchMetadata.State.CurrentTurn = *event.Turn
	}
	validator := h
	validator.Metadata = &dispatchMetadata
	service := RunnerDispatchService{SessionDir: h.SessionDir, Metadata: &dispatchMetadata, Member: h.Member, Adapter: h.Adapter, Runtime: h.Runtime, Locks: h.Locks, Now: h.Now}
	result, err := service.Dispatch(ctx, RunnerDispatchRequest{
		CommandID:                commandID,
		SourceCommandID:          commandID,
		Prompt:                   prompt,
		MaxRetries:               h.MaxRetries,
		Timeout:                  h.Timeout,
		CausationEventID:         event.EventID,
		AllowedTerminalTypes:     []string{"speech"},
		DisallowedTerminalReason: "selected_runner_requires_canonical_speech",
		TerminalValidator:        validator.validateCanonicalSpeechTerminal(),
		PayloadMissingReason:     "selected_runner_speech_payload_missing",
		PayloadInvalidReason:     "selected_runner_speech_payload_invalid",
	})
	if err != nil {
		return err
	}
	switch result.TerminalEventType {
	case "runner_invocation_failed", "runner_result_discarded":
		return memberruntime.ErrDurableFailureRecorded
	default:
		return nil
	}
}

func (h SelectedSpeakerDispatchHandler) validateCanonicalSpeechTerminal() RunnerTerminalValidator {
	return func(event storage.EventEnvelope) (storage.EventEnvelope, error) {
		if event.Payload == nil {
			return storage.EventEnvelope{}, fmt.Errorf("selected runner speech payload is required")
		}
		canonical, err := storage.BuildCouncilEvent(h.SessionDir, h.Metadata, storage.CouncilEventSpec{
			Action:           "speak",
			Actor:            h.Member.ID,
			CommandID:        event.CommandID,
			CausationEventID: event.CausationEventID,
			Payload:          event.Payload,
			Now:              event.CreatedAt,
		})
		if err != nil {
			return storage.EventEnvelope{}, err
		}
		event.Phase = canonical.Phase
		event.From = canonical.From
		event.To = canonical.To
		event.Turn = canonical.Turn
		event.Payload = canonical.Payload
		return event, nil
	}
}

func (h SelectedSpeakerDispatchHandler) promptFor(event storage.EventEnvelope) string {
	if h.PromptBuilder != nil {
		return h.PromptBuilder(event, h.Member)
	}
	turn := event.Payload["turn"]
	return fmt.Sprintf("KAN council selected registered member %s to speak for turn %v. Use the configured participant wrapper boundary and return a typed speech event. causation_event_id=%s", h.Member.ID, turn, event.EventID)
}

func selectedSpeakerMember(event storage.EventEnvelope) (string, error) {
	payloadMember := ""
	if member, ok := event.Payload["member"].(string); ok {
		payloadMember = strings.TrimSpace(member)
	}
	toMember := ""
	if len(event.To) == 1 {
		toMember = strings.TrimSpace(event.To[0])
	}
	if payloadMember != "" && toMember != "" && payloadMember != toMember {
		return "", fmt.Errorf("speaker_selected member/to mismatch: payload.member=%q to[0]=%q", payloadMember, toMember)
	}
	if payloadMember != "" {
		return payloadMember, nil
	}
	return toMember, nil
}
