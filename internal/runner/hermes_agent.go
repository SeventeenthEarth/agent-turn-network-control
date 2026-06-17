package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type HermesAgentAdapter struct{}

func NewHermesAgentAdapter() *HermesAgentAdapter { return &HermesAgentAdapter{} }

func (a *HermesAgentAdapter) Kind() string       { return HermesAgentKind }
func (a *HermesAgentAdapter) CostSource() string { return HermesAgentCostSource }

func (a *HermesAgentAdapter) Send(ctx context.Context, req Request) (Result, error) {
	return a.invoke(ctx, "send", req)
}

func (a *HermesAgentAdapter) Resume(ctx context.Context, req Request) (Result, error) {
	return a.invoke(ctx, "resume", req)
}

func (a *HermesAgentAdapter) Cancel(ctx context.Context, handle SessionHandle) error {
	return nil
}

func (a *HermesAgentAdapter) ParseSessionHandle(stdout []byte) (*SessionHandle, error) {
	for _, line := range strings.Split(string(stdout), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "session_handle=") {
			handle := SessionHandle(strings.TrimPrefix(line, "session_handle="))
			return &handle, nil
		}
	}
	return nil, nil
}

func (a *HermesAgentAdapter) invoke(ctx context.Context, mode string, req Request) (Result, error) {
	wrapper := strings.TrimSpace(req.ResolvedWrapper)
	if wrapper == "" && req.Member.ResolvedWrapper != nil {
		wrapper = req.Member.ResolvedWrapper.ResolvedPath
	}
	if wrapper == "" || !filepath.IsAbs(wrapper) {
		return Result{ErrorClass: "wrapper_unresolved"}, fmt.Errorf("resolved wrapper path is required")
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	args := append([]string(nil), req.Args...)
	if len(args) == 0 {
		args = []string{mode}
		if req.SessionHandle != nil {
			args = append(args, "--session", string(*req.SessionHandle))
		}
		if req.Prompt != "" {
			args = append(args, req.Prompt)
		}
	}
	cmd := exec.CommandContext(ctx, wrapper, args...)
	cmd.Dir = req.Member.Workspace
	cmd.Env = append([]string(nil), req.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	err := cmd.Run()
	duration := time.Since(started)
	result := Result{
		OK:       err == nil,
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode(err),
		Duration: duration,
		Cost:     ParseHermesCost(stderr.Bytes()),
	}
	if handle, parseErr := a.ParseSessionHandle(stdout.Bytes()); parseErr == nil {
		result.SessionHandle = handle
	}
	if ctx.Err() != nil {
		result.OK = false
		result.ErrorClass = ErrorClassTimeout
		return result, ctx.Err()
	}
	semantic, class, reason := parseHermesResponseOutput(stdout.Bytes(), stderr.Bytes())
	if class != "" {
		result.OK = false
		result.ErrorClass = class
		result.Payload = diagnosticPayload(class, reason, stdout.Bytes(), stderr.Bytes())
		return result, fmt.Errorf("%w: %s", ErrSemantic, class)
	}
	if err != nil {
		result.ErrorClass = classifyHermesCommandFailure(stdout.Bytes(), stderr.Bytes())
		if result.ErrorClass == ErrorClassModelProviderFailure {
			result.Payload = diagnosticPayload(result.ErrorClass, result.ErrorClass, stdout.Bytes(), stderr.Bytes())
		}
		return result, err
	}
	result.OK = true
	result.SemanticStatus = "succeeded"
	result.SemanticEventType = semantic.EventType
	result.Payload = semantic.Payload
	return result, nil
}

type hermesSemanticOutput struct {
	EventType string
	Payload   map[string]any
}

func parseHermesResponseOutput(stdout, stderr []byte) (hermesSemanticOutput, string, string) {
	lines := nonEmptyOutputLines(stdout)
	var responseLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "session_handle=") {
			continue
		}
		if looksLikeJSON(line) {
			semantic, class, reason := parseHermesJSONLine(line)
			if class != "" || semantic.EventType != "" || len(semantic.Payload) > 0 {
				return semantic, class, reason
			}
			continue
		}
		if isDeliveryOrFallbackOnly(line) {
			return hermesSemanticOutput{}, ErrorClassAdapterCommandMismatch, ErrorClassAdapterCommandMismatch
		}
		responseLines = append(responseLines, line)
	}
	if len(responseLines) > 0 {
		return hermesSemanticOutput{EventType: "assignee_update", Payload: map[string]any{"message": strings.Join(responseLines, "\n")}}, "", ""
	}
	if outputHasProviderFailure(stderr) {
		return hermesSemanticOutput{}, ErrorClassModelProviderFailure, ErrorClassModelProviderFailure
	}
	return hermesSemanticOutput{}, ErrorClassMalformedOrMissingResponse, ErrorClassMalformedOrMissingResponse
}

func parseHermesJSONLine(line string) (hermesSemanticOutput, string, string) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return hermesSemanticOutput{}, ErrorClassMalformedOrMissingResponse, ErrorClassMalformedOrMissingResponse
	}
	if jsonHasProviderFailure(raw) {
		return hermesSemanticOutput{}, ErrorClassModelProviderFailure, ErrorClassModelProviderFailure
	}
	if jsonHasDeliveryOrFallbackOnly(raw) {
		return hermesSemanticOutput{}, ErrorClassAdapterCommandMismatch, ErrorClassAdapterCommandMismatch
	}
	eventType := firstString(raw, "semantic_event_type", "event_type", "type")
	if payload, ok := raw["payload"].(map[string]any); ok && len(payload) > 0 {
		if eventType == "" {
			eventType = "assignee_update"
		}
		return hermesSemanticOutput{EventType: eventType, Payload: payload}, "", ""
	}
	if eventType != "" {
		payload := payloadFromTypedFields(raw)
		if len(payload) > 0 {
			return hermesSemanticOutput{EventType: eventType, Payload: payload}, "", ""
		}
		return hermesSemanticOutput{}, ErrorClassMalformedOrMissingResponse, ErrorClassMalformedOrMissingResponse
	}
	if text := firstString(raw, "response", "text", "message", "content"); text != "" {
		return hermesSemanticOutput{EventType: "assignee_update", Payload: map[string]any{"message": text}}, "", ""
	}
	return hermesSemanticOutput{}, ErrorClassMalformedOrMissingResponse, ErrorClassMalformedOrMissingResponse
}

func classifyHermesCommandFailure(stdout, stderr []byte) string {
	if outputHasProviderFailure(stdout) || outputHasProviderFailure(stderr) {
		return ErrorClassModelProviderFailure
	}
	return "nonzero_exit"
}

func nonEmptyOutputLines(stdout []byte) []string {
	rawLines := strings.Split(string(stdout), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func looksLikeJSON(line string) bool {
	return strings.HasPrefix(line, "{") || strings.HasPrefix(line, "[")
}

func payloadFromTypedFields(raw map[string]any) map[string]any {
	payload := map[string]any{}
	for key, value := range raw {
		switch key {
		case "semantic_event_type", "event_type", "type", "status", "session_handle":
			continue
		}
		payload[key] = value
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key].(string); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func outputHasProviderFailure(output []byte) bool {
	lower := strings.ToLower(string(output))
	for _, marker := range []string{
		"model_provider_failure",
		"provider error",
		"provider_error",
		"model error",
		"model_error",
		"model not found",
		"rate limit",
		"quota exceeded",
		"invalid api key",
		"authentication failed",
		"permission denied",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func jsonHasProviderFailure(raw map[string]any) bool {
	for _, key := range []string{"error_class", "code", "reason", "error"} {
		if value, ok := raw[key].(string); ok && outputHasProviderFailure([]byte(value)) {
			return true
		}
	}
	return false
}

func isDeliveryOrFallbackOnly(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"platform_delivery",
		"delivery_evidence",
		"discord_message_id",
		"message_id=",
		"delivered=",
		"fallback",
		"manual output",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func jsonHasDeliveryOrFallbackOnly(raw map[string]any) bool {
	switch strings.ToLower(firstString(raw, "semantic_event_type", "event_type", "type")) {
	case "platform_delivery", "delivery_evidence", "fallback":
		return true
	}
	for _, key := range []string{"platform_delivery", "delivery_evidence", "discord_message_id", "message_id", "fallback", "manual_output"} {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	if status := firstString(raw, "status"); status == "posted" || status == "delivered" || status == "fallback" {
		return true
	}
	return false
}

func diagnosticPayload(class, reason string, stdout, stderr []byte) map[string]any {
	payload := map[string]any{
		"error_class": class,
		"reason":      reason,
	}
	if excerpt := redactedDiagnosticExcerpt(stdout, stderr); excerpt != "" {
		payload["diagnostic_excerpt"] = excerpt
	}
	return payload
}

var pathLikePattern = regexp.MustCompile(`/[^\s'"]+`)

func redactedDiagnosticExcerpt(stdout, stderr []byte) string {
	combined := strings.TrimSpace(string(stdout) + "\n" + string(stderr))
	if combined == "" {
		return ""
	}
	fields := strings.Fields(combined)
	for i, field := range fields {
		lower := strings.ToLower(field)
		if strings.HasPrefix(field, "/") || strings.Contains(lower, "message_id") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "authorization") || strings.Contains(lower, "auth=") || strings.Contains(lower, "profile=") || strings.Contains(lower, "provider=") || strings.Contains(lower, "gateway=") {
			fields[i] = "[redacted]"
		}
	}
	excerpt := strings.Join(fields, " ")
	excerpt = pathLikePattern.ReplaceAllString(excerpt, "[redacted_path]")
	if len(excerpt) > 240 {
		excerpt = excerpt[:240]
	}
	return excerpt
}
