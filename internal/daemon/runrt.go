package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/runner"
	"kkachi-agent-network-control/internal/storage"
)

type RunnerDispatchService struct {
	SessionDir string
	Metadata   *storage.SessionMetadata
	Member     registry.Member
	Adapter    runner.Adapter
	Runtime    registry.Runtime
	Locks      *DispatchLocks
	Now        func() time.Time
}

type RunnerDispatchRequest struct {
	CommandID       string
	SourceCommandID string
	Prompt          string
	MaxRetries      int
	Timeout         time.Duration
	Cancelled       func() bool
}

type RunnerDispatchResult struct {
	InvocationIDs []string
	TerminalEvent string
	Attempts      int
}

type DispatchLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *DispatchLocks) Lock(sessionID, member string) func() {
	key := sessionID + "\x00" + member
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	lock := l.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		l.locks[key] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (s *RunnerDispatchService) Dispatch(ctx context.Context, req RunnerDispatchRequest) (RunnerDispatchResult, error) {
	if err := s.preflight(); err != nil {
		return RunnerDispatchResult{}, err
	}
	locks := s.Locks
	if locks == nil {
		locks = &DispatchLocks{}
	}
	unlock := locks.Lock(s.Metadata.ID, s.Member.ID)
	defer unlock()
	maxRetries := req.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	sourceCommandID := strings.TrimSpace(req.SourceCommandID)
	if sourceCommandID == "" {
		sourceCommandID = req.CommandID
	}
	var out RunnerDispatchResult
	var priorErr error
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		if attempt > 1 {
			if err := s.appendRetry(req, attempt, priorErr); err != nil {
				return out, err
			}
		}
		invocationID := invocationIDFor(sourceCommandID, attempt, s.now())
		out.InvocationIDs = append(out.InvocationIDs, invocationID)
		if err := s.appendStarted(req, invocationID, sourceCommandID, attempt); err != nil {
			return out, err
		}
		result, err := s.Adapter.Send(ctx, runner.Request{
			SessionID:       s.Metadata.ID,
			Member:          s.Member,
			ResolvedWrapper: resolvedWrapper(s.Member),
			Prompt:          req.Prompt,
			Timeout:         req.Timeout,
			InvocationID:    invocationID,
			SourceCommandID: sourceCommandID,
			Attempt:         attempt,
			Env:             runner.EnvForMember(s.Member, s.Runtime),
		})
		out.Attempts = attempt
		discarded := result.Discarded || (req.Cancelled != nil && req.Cancelled())
		if discarded {
			terminal, appendErr := s.appendTerminal(req, "runner_result_discarded", invocationID, sourceCommandID, attempt, result, map[string]any{"reason": "cancelled_or_late_result"})
			out.TerminalEvent = terminal.EventID
			return out, appendErr
		}
		if err == nil && result.OK {
			terminalType := result.SemanticEventType
			if terminalType == "" {
				terminalType = "assignee_update"
			}
			payload := result.Payload
			if payload == nil {
				payload = map[string]any{"stdout": string(result.Stdout)}
			}
			terminal, appendErr := s.appendTerminal(req, terminalType, invocationID, sourceCommandID, attempt, result, payload)
			out.TerminalEvent = terminal.EventID
			return out, appendErr
		}
		priorErr = err
		if !retryable(result, err) || attempt > maxRetries {
			payload := map[string]any{"error_class": result.ErrorClass, "reason": runnerFailureReason(result)}
			if payload["error_class"] == "" {
				payload["error_class"] = "runner_error"
			}
			terminal, appendErr := s.appendTerminal(req, "runner_invocation_failed", invocationID, sourceCommandID, attempt, result, payload)
			out.TerminalEvent = terminal.EventID
			return out, appendErr
		}
	}
	return out, priorErr
}

func (s *RunnerDispatchService) preflight() error {
	if s.Metadata == nil {
		return storage.NewValidationError(storage.CategoryMetadataInvalid, "session", "metadata is required")
	}
	if s.Metadata.Status == storage.StatusTerminal || statusFromPhaseForRunRT(s.Metadata.State.Phase) == storage.StatusTerminal || statusFromPhaseForRunRT(s.Metadata.State.Phase) == storage.StatusBlocked {
		return storage.NewValidationError(storage.CategoryCommandConflict, "session", "session is terminal or blocked")
	}
	if !storage.Participant(s.Metadata, s.Member.ID) {
		return storage.NewValidationError(storage.CategoryPrincipalInvalid, "member", "member is not a session participant")
	}
	if s.Member.AdapterKind != runner.HermesAgentKind {
		return fmt.Errorf("unsupported runner adapter kind %q", s.Member.AdapterKind)
	}
	if s.Adapter == nil || s.Adapter.Kind() != runner.HermesAgentKind {
		return fmt.Errorf("unsupported runner adapter kind")
	}
	if resolvedWrapper(s.Member) == "" {
		return registry.NewValidationError(registry.CategoryWrapperUnresolvable, "wrapper", "resolved wrapper path is required")
	}
	if s.Metadata.Limits.MaxRunnerCalls > 0 && s.Metadata.Cost.RunnerCallsTotal >= s.Metadata.Limits.MaxRunnerCalls {
		return storage.NewValidationError(storage.CategoryCommandConflict, "max_runner_calls", "runner call budget exceeded")
	}
	return nil
}

func (s *RunnerDispatchService) appendStarted(req RunnerDispatchRequest, invocationID, sourceCommandID string, attempt int) error {
	_, err := storage.AppendEvent(s.SessionDir, s.Metadata, s.baseEvent(req, "runner_invocation_started", invocationID, sourceCommandID, attempt, nil, map[string]any{"prompt_sha256": sha256Text(req.Prompt)}))
	return err
}

func (s *RunnerDispatchService) appendRetry(req RunnerDispatchRequest, attempt int, priorErr error) error {
	sourceCommandID := originalCommandID(req)
	payload := map[string]any{
		"attempt":             attempt,
		"prior_error_class":   "runner_error",
		"original_command_id": sourceCommandID,
		"target_member":       s.Member.ID,
	}
	if priorErr != nil {
		payload["prior_error"] = priorErr.Error()
	}
	event := storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventIDFor(sourceCommandID, "runner_retry_attempted", attempt, s.now()),
		CommandID:     sourceCommandID,
		CorrelationID: s.Metadata.ID,
		SessionID:     s.Metadata.ID,
		SessionType:   s.Metadata.SessionType,
		Phase:         s.Metadata.State.Phase,
		Type:          "runner_retry_attempted",
		From:          "kkachi-agent-networkd",
		To:            []string{s.Metadata.Moderator},
		CreatedAt:     s.now(),
		Payload:       payload,
	}
	_, err := storage.AppendEvent(s.SessionDir, s.Metadata, event)
	return err
}

func (s *RunnerDispatchService) appendTerminal(req RunnerDispatchRequest, typ, invocationID, sourceCommandID string, attempt int, result runner.Result, payload map[string]any) (storage.EventEnvelope, error) {
	event := s.baseEvent(req, typ, invocationID, sourceCommandID, attempt, &result, payload)
	event.Cost = runner.CostRaw(result.Cost)
	res, err := storage.AppendEvent(s.SessionDir, s.Metadata, event)
	if err == nil {
		event.EventID = res.EventID
	}
	return event, err
}

func (s *RunnerDispatchService) baseEvent(req RunnerDispatchRequest, typ, invocationID, sourceCommandID string, attempt int, result *runner.Result, payload map[string]any) storage.EventEnvelope {
	status := "started"
	var duration *float64
	if result != nil {
		status = runnerStatus(typ, *result)
		if result.Discarded || typ == "runner_result_discarded" {
			status = "discarded_after_cancel"
		}
		seconds := result.Duration.Seconds()
		duration = &seconds
	}
	from := s.Member.ID
	to := []string{s.Metadata.Moderator}
	if strings.HasPrefix(typ, "runner_") {
		from = "kkachi-agent-networkd"
		to = []string{s.Metadata.Moderator}
	}
	commandID := strings.TrimSpace(sourceCommandID)
	if commandID == "" {
		commandID = strings.TrimSpace(req.CommandID)
	}
	return storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventIDFor(commandID, typ, attempt, s.now()),
		CommandID:     commandID,
		CorrelationID: s.Metadata.ID,
		SessionID:     s.Metadata.ID,
		SessionType:   s.Metadata.SessionType,
		Phase:         s.Metadata.State.Phase,
		Type:          typ,
		From:          from,
		To:            to,
		CreatedAt:     s.now(),
		Runner:        &storage.RunnerInfo{InvocationID: invocationID, AdapterKind: runner.HermesAgentKind, Member: s.Member.ID, Attempt: attempt, IsRetry: attempt > 1, SourceCommandID: sourceCommandID, Status: status, DurationSec: duration},
		Payload:       payload,
	}
}

func originalCommandID(req RunnerDispatchRequest) string {
	sourceCommandID := strings.TrimSpace(req.SourceCommandID)
	if sourceCommandID != "" {
		return sourceCommandID
	}
	return strings.TrimSpace(req.CommandID)
}

func runnerStatus(typ string, result runner.Result) string {
	if result.Discarded || typ == "runner_result_discarded" {
		return "discarded_after_cancel"
	}
	if result.OK {
		if result.SemanticStatus != "" {
			return normalizeRunnerStatus(result.SemanticStatus)
		}
		return "succeeded"
	}
	return normalizeRunnerStatus(result.ErrorClass)
}

func normalizeRunnerStatus(value string) string {
	switch value {
	case "started", "succeeded", "failed", "timeout", "semantic_error", "discarded_after_cancel", "cancelled", "interrupted":
		return value
	case "dispatch_timeout":
		return "timeout"
	default:
		return "failed"
	}
}

func runnerFailureReason(result runner.Result) string {
	switch result.ErrorClass {
	case "timeout", "dispatch_timeout":
		return "dispatch_timeout"
	case "transport", "transport_error":
		return "transport_error"
	case "nonzero_exit", "nonzero_exit_empty_output":
		return "nonzero_exit_empty_output"
	case "semantic_error", "semantic_parse_error":
		return "semantic_parse_error"
	case "cancelled":
		return "cancelled"
	case "interrupted":
		return "interrupted"
	default:
		if result.ErrorClass != "" {
			return "other"
		}
		return "other"
	}
}

func (s *RunnerDispatchService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	if s.Runtime.Now != nil {
		return s.Runtime.Now().UTC()
	}
	return time.Now().UTC()
}

func retryable(result runner.Result, err error) bool {
	if err == nil {
		return false
	}
	return result.ErrorClass == "timeout" || result.ErrorClass == "transport" || result.ErrorClass == "nonzero_exit" || result.ErrorClass == ""
}

func resolvedWrapper(member registry.Member) string {
	if member.ResolvedWrapper != nil {
		return member.ResolvedWrapper.ResolvedPath
	}
	return ""
}

func invocationIDFor(commandID string, attempt int, now time.Time) string {
	return "run_" + sha256Text(fmt.Sprintf("%s:%d:%d", commandID, attempt, now.UnixNano()))[:24]
}

func eventIDFor(commandID, typ string, attempt int, now time.Time) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '_'
	}, commandID+"_"+typ)
	if len(clean) > 60 {
		clean = clean[:60]
	}
	return fmt.Sprintf("evt_%s_%d_%d", clean, attempt, now.UnixNano())
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func statusFromPhaseForRunRT(phase storage.Phase) storage.Status {
	switch phase {
	case "blocked":
		return storage.StatusBlocked
	case "finalized", "accepted", "cancelled":
		return storage.StatusTerminal
	default:
		return storage.StatusOpen
	}
}
