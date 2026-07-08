package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SeventeenthEarth/agent-turn-network-control/internal/memberruntime"
	"github.com/SeventeenthEarth/agent-turn-network-control/internal/protocol"
	"github.com/SeventeenthEarth/agent-turn-network-control/internal/registry"
	"github.com/SeventeenthEarth/agent-turn-network-control/internal/runner"
	"github.com/SeventeenthEarth/agent-turn-network-control/internal/storage"
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
	prompt, err := h.promptFor(event)
	if err != nil {
		if appendErr := h.appendPromptDiagnostic(event, err); appendErr != nil {
			return appendErr
		}
		return memberruntime.ErrDurableFailureRecorded
	}
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
		TerminalValidator:        validator.validateCanonicalSpeechTerminal(ctx),
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

func (h SelectedSpeakerDispatchHandler) validateCanonicalSpeechTerminal(ctx context.Context) RunnerTerminalValidator {
	return func(event storage.EventEnvelope) (storage.EventEnvelope, error) {
		if event.Payload == nil {
			return storage.EventEnvelope{}, fmt.Errorf("selected runner speech payload is required")
		}
		payload := normalizeSelectedRunnerSpeechPayload(event.Payload)
		canonical, err := storage.BuildCouncilEvent(h.SessionDir, h.Metadata, storage.CouncilEventSpec{
			Action:           "speak",
			Actor:            h.Member.ID,
			CommandID:        event.CommandID,
			CausationEventID: event.CausationEventID,
			Payload:          payload,
			Now:              event.CreatedAt,
		})
		if err != nil {
			return storage.EventEnvelope{}, err
		}
		if err := h.bindSelectedMemberVisibleDelivery(ctx, canonical.Payload, event.EventID); err != nil {
			return storage.EventEnvelope{}, err
		}
		bindSelectedRunnerSurfaceEvidence(canonical.Payload, event.EventID)
		event.Phase = canonical.Phase
		event.From = canonical.From
		event.To = canonical.To
		event.Turn = canonical.Turn
		event.Payload = canonical.Payload
		return event, nil
	}
}

func (h SelectedSpeakerDispatchHandler) bindSelectedMemberVisibleDelivery(ctx context.Context, payload map[string]any, speechEventID string) error {
	if !selectedRunnerRequiresVisibleDiscordDelivery(h.Metadata) {
		return nil
	}
	if payload == nil {
		return fmt.Errorf("selected runner visible delivery payload is required")
	}
	speech := strings.TrimSpace(payloadString(payload, "speech"))
	if speech == "" {
		return fmt.Errorf("selected runner visible delivery speech is required")
	}
	sender, ok := h.Adapter.(runner.VisibleSender)
	if !ok {
		return fmt.Errorf("selected runner adapter cannot perform participant profile visible delivery")
	}
	surface := h.Metadata.Surface
	target, err := selectedRunnerVisibleDeliveryTarget(surface)
	if err != nil {
		return err
	}
	result, err := sender.SendVisible(ctx, runner.VisibleDeliveryRequest{
		SessionID:       h.Metadata.ID,
		Member:          h.Member,
		ResolvedWrapper: resolvedWrapper(h.Member),
		Target:          target,
		Content:         speech,
		Kind:            surface.Kind,
		Platform:        surface.Platform,
		ChannelID:       surface.ChannelID,
		ThreadID:        surface.ThreadID,
		Timeout:         h.Timeout,
		Env:             runner.EnvForMember(h.Member, h.Runtime),
	})
	if err != nil {
		return fmt.Errorf("selected member visible delivery failed: %w", err)
	}
	evidence, err := selectedRunnerVisibleDeliveryEvidence(h.Member.ID, surface, speechEventID, result)
	if err != nil {
		return err
	}
	payload["surface_evidence"] = evidence
	return nil
}

func selectedRunnerRequiresVisibleDiscordDelivery(metadata *storage.SessionMetadata) bool {
	if metadata == nil || metadata.Surface == nil {
		return false
	}
	return strings.TrimSpace(metadata.Surface.Kind) == "discord_thread" || strings.TrimSpace(metadata.Surface.Platform) == "discord"
}

func selectedRunnerVisibleDeliveryTarget(surface *storage.Surface) (string, error) {
	if surface == nil {
		return "", fmt.Errorf("selected runner visible delivery surface is required")
	}
	if strings.TrimSpace(surface.Kind) != "discord_thread" {
		return "", fmt.Errorf("selected runner visible delivery requires discord_thread surface")
	}
	if strings.TrimSpace(surface.Platform) != "discord" {
		return "", fmt.Errorf("selected runner visible delivery requires discord platform")
	}
	channelID := strings.TrimSpace(surface.ChannelID)
	threadID := strings.TrimSpace(surface.ThreadID)
	if channelID == "" || threadID == "" {
		return "", fmt.Errorf("selected runner visible delivery requires channel_id and thread_id")
	}
	return "discord:" + channelID + ":" + threadID, nil
}

func selectedRunnerVisibleDeliveryEvidence(memberID string, surface *storage.Surface, speechEventID string, result runner.VisibleDeliveryResult) (map[string]any, error) {
	if !result.OK {
		return nil, fmt.Errorf("selected member visible delivery did not return ok status: %s", result.ErrorClass)
	}
	if strings.TrimSpace(result.Status) != "posted" {
		return nil, fmt.Errorf("selected member visible delivery status %q is not posted", result.Status)
	}
	if strings.TrimSpace(result.MessageID) == "" {
		return nil, fmt.Errorf("selected member visible delivery message_id missing")
	}
	if strings.TrimSpace(result.PostingPath) != "selected_member_profile_send" {
		return nil, fmt.Errorf("selected member visible delivery used disallowed posting_path %q", result.PostingPath)
	}
	if strings.TrimSpace(result.SenderMember) != strings.TrimSpace(memberID) {
		return nil, fmt.Errorf("selected member visible delivery sender_member %q does not match selected member %q", result.SenderMember, memberID)
	}
	if observed := strings.TrimSpace(result.Kind); observed != "" && observed != strings.TrimSpace(surface.Kind) {
		return nil, fmt.Errorf("selected member visible delivery kind %q does not match configured surface", observed)
	}
	if observed := strings.TrimSpace(result.Platform); observed != "" && observed != strings.TrimSpace(surface.Platform) {
		return nil, fmt.Errorf("selected member visible delivery platform %q does not match configured surface", observed)
	}
	if observed := strings.TrimSpace(result.ChannelID); observed != "" && observed != strings.TrimSpace(surface.ChannelID) {
		return nil, fmt.Errorf("selected member visible delivery channel_id mismatch")
	}
	if observed := strings.TrimSpace(result.ThreadID); observed != "" && observed != strings.TrimSpace(surface.ThreadID) {
		return nil, fmt.Errorf("selected member visible delivery thread_id mismatch")
	}
	return map[string]any{
		"status":              "posted",
		"kind":                "discord_thread",
		"platform":            "discord",
		"channel_id":          strings.TrimSpace(surface.ChannelID),
		"thread_id":           strings.TrimSpace(surface.ThreadID),
		"message_id":          strings.TrimSpace(result.MessageID),
		"posting_path":        "selected_member_profile_send",
		"sender_member":       strings.TrimSpace(memberID),
		"references_event_id": strings.TrimSpace(speechEventID),
		"speech_event_id":     strings.TrimSpace(speechEventID),
	}, nil
}

func bindSelectedRunnerSurfaceEvidence(payload map[string]any, speechEventID string) {
	if payload == nil || strings.TrimSpace(speechEventID) == "" {
		return
	}
	for _, key := range []string{"surface_evidence", "plugin_evidence", "delivery_evidence", "evidence"} {
		value, ok := payload[key]
		if !ok {
			continue
		}
		surfaceEvidence, ok := value.(map[string]any)
		if !ok || surfaceEvidence == nil {
			continue
		}
		if _, exists := surfaceEvidence["references_event_id"]; !exists {
			surfaceEvidence["references_event_id"] = speechEventID
		}
		if _, exists := surfaceEvidence["speech_event_id"]; !exists {
			surfaceEvidence["speech_event_id"] = speechEventID
		}
		return
	}
}

func (h SelectedSpeakerDispatchHandler) promptFor(event storage.EventEnvelope) (string, error) {
	if h.PromptBuilder == nil {
		return "", fmt.Errorf("selected speaker prompt builder is required")
	}
	prompt := strings.TrimSpace(h.PromptBuilder(event, h.Member))
	if prompt == "" {
		return "", fmt.Errorf("selected speaker prompt builder returned empty prompt")
	}
	return prompt, nil
}

func (h SelectedSpeakerDispatchHandler) appendPromptDiagnostic(event storage.EventEnvelope, validationErr error) error {
	if h.Metadata == nil {
		return fmt.Errorf("selected speaker metadata is required")
	}
	payload := map[string]any{
		"reason":                    "selected_runner_prompt_missing",
		"selected_member":           h.Member.ID,
		"turn":                      event.Payload["turn"],
		"diagnostic_owner":          "control/NEWFIX-001",
		"diagnostic_path":           "internal/daemon/selected_speaker.go",
		"validation_error":          validationErr.Error(),
		"prompt_builder_configured": h.PromptBuilder != nil,
	}
	diagnostic := storage.EventEnvelope{
		SchemaVersion:    protocol.SchemaVersion,
		EventID:          eventIDFor(event.EventID, "selected_runner_dispatch_failed", 1, h.now()),
		CommandID:        event.CommandID,
		CausationEventID: event.EventID,
		CorrelationID:    h.Metadata.ID,
		SessionID:        h.Metadata.ID,
		SessionType:      h.Metadata.SessionType,
		Phase:            event.Phase,
		Type:             "selected_runner_dispatch_failed",
		From:             "atn-controld",
		To:               []string{h.Metadata.Moderator},
		CreatedAt:        h.now(),
		Payload:          payload,
	}
	_, err := storage.AppendEvent(h.SessionDir, h.Metadata, diagnostic)
	return err
}

func (h SelectedSpeakerDispatchHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

func normalizeSelectedRunnerSpeechPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	normalized := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		normalized[key] = value
	}
	if speech, ok := normalized["speech"].(string); ok && strings.TrimSpace(speech) != "" {
		return normalized
	}
	for _, key := range []string{"message", "content", "text"} {
		if value, ok := normalized[key].(string); ok && strings.TrimSpace(value) != "" {
			normalized["speech"] = value
			return normalized
		}
	}
	return normalized
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
