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
	selected := selectedSpeakerMember(event)
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
	service := RunnerDispatchService{SessionDir: h.SessionDir, Metadata: h.Metadata, Member: h.Member, Adapter: h.Adapter, Runtime: h.Runtime, Locks: h.Locks, Now: h.Now}
	result, err := service.Dispatch(ctx, RunnerDispatchRequest{CommandID: commandID, SourceCommandID: commandID, Prompt: prompt, MaxRetries: h.MaxRetries, Timeout: h.Timeout, CausationEventID: event.EventID})
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

func (h SelectedSpeakerDispatchHandler) promptFor(event storage.EventEnvelope) string {
	if h.PromptBuilder != nil {
		return h.PromptBuilder(event, h.Member)
	}
	turn := event.Payload["turn"]
	return fmt.Sprintf("KAN council selected registered member %s to speak for turn %v. Use the configured participant wrapper boundary and return a typed speech event. causation_event_id=%s", h.Member.ID, turn, event.EventID)
}

func selectedSpeakerMember(event storage.EventEnvelope) string {
	if member, ok := event.Payload["member"].(string); ok && strings.TrimSpace(member) != "" {
		return strings.TrimSpace(member)
	}
	if len(event.To) == 1 {
		return strings.TrimSpace(event.To[0])
	}
	return ""
}
