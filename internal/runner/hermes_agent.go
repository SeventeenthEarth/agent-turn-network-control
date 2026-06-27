package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	return a.invoke(ctx, "chat", req)
}

func (a *HermesAgentAdapter) Resume(ctx context.Context, req Request) (Result, error) {
	return a.invoke(ctx, "resume", req)
}

func (a *HermesAgentAdapter) Cancel(ctx context.Context, handle SessionHandle) error {
	return nil
}

func (a *HermesAgentAdapter) SendVisible(ctx context.Context, req VisibleDeliveryRequest) (VisibleDeliveryResult, error) {
	wrapper := strings.TrimSpace(req.ResolvedWrapper)
	if wrapper == "" && req.Member.ResolvedWrapper != nil {
		wrapper = req.Member.ResolvedWrapper.ResolvedPath
	}
	if wrapper == "" || !filepath.IsAbs(wrapper) {
		return VisibleDeliveryResult{ErrorClass: "wrapper_unresolved"}, fmt.Errorf("resolved wrapper path is required")
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	if workspaceResult, workspaceErr := validateHermesWorkspace(req.Member.Workspace); workspaceErr != nil {
		return VisibleDeliveryResult{OK: false, ExitCode: -1, ErrorClass: workspaceResult.ErrorClass, Payload: workspaceResult.Payload, DiagnosticMsg: workspaceErr.Error()}, workspaceErr
	}
	args := append([]string(nil), req.Args...)
	if len(args) == 0 {
		target := strings.TrimSpace(req.Target)
		content := strings.TrimSpace(req.Content)
		if target == "" {
			return VisibleDeliveryResult{ErrorClass: ErrorClassVisibleDeliveryMalformed}, fmt.Errorf("visible delivery target is required")
		}
		if content == "" {
			return VisibleDeliveryResult{ErrorClass: ErrorClassVisibleDeliveryMalformed}, fmt.Errorf("visible delivery content is required")
		}
		args = []string{"send", "--to", target, "--json", content}
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
	result := VisibleDeliveryResult{
		OK:           err == nil,
		Stdout:       stdout.Bytes(),
		Stderr:       stderr.Bytes(),
		ExitCode:     exitCode(err),
		Duration:     duration,
		Kind:         strings.TrimSpace(req.Kind),
		Platform:     strings.TrimSpace(req.Platform),
		ChannelID:    strings.TrimSpace(req.ChannelID),
		ThreadID:     strings.TrimSpace(req.ThreadID),
		PostingPath:  "selected_member_profile_send",
		SenderMember: strings.TrimSpace(req.Member.ID),
	}
	if ctx.Err() != nil {
		result.OK = false
		result.ErrorClass = ErrorClassTimeout
		return result, ctx.Err()
	}
	if err != nil {
		result.OK = false
		result.ErrorClass = ErrorClassVisibleDeliveryFailed
		result.Payload = diagnosticPayload(result.ErrorClass, result.ErrorClass, stdout.Bytes(), stderr.Bytes())
		return result, err
	}
	parsed, class, reason := parseHermesVisibleDeliveryOutput(stdout.Bytes())
	if class != "" {
		result.OK = false
		result.ErrorClass = class
		result.Payload = diagnosticPayload(class, reason, stdout.Bytes(), stderr.Bytes())
		return result, fmt.Errorf("%s: %s", class, reason)
	}
	result.Status = firstNonEmpty(parsed.Status, result.Status)
	result.Kind = firstNonEmpty(parsed.Kind, result.Kind)
	result.Platform = firstNonEmpty(parsed.Platform, result.Platform)
	result.ChannelID = firstNonEmpty(parsed.ChannelID, result.ChannelID)
	result.ThreadID = firstNonEmpty(parsed.ThreadID, result.ThreadID)
	result.MessageID = parsed.MessageID
	result.Payload = parsed.Payload
	result.OK = true
	return result, nil
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
	if workspaceResult, workspaceErr := validateHermesWorkspace(req.Member.Workspace); workspaceErr != nil {
		return workspaceResult, workspaceErr
	}
	args := append([]string(nil), req.Args...)
	if len(args) == 0 {
		switch mode {
		case "chat":
			args = []string{"chat", "-Q"}
			if req.SessionHandle != nil {
				args = append(args, "--session", string(*req.SessionHandle))
			}
			if req.Prompt != "" {
				args = append(args, "-q", req.Prompt)
			}
		default:
			args = []string{mode}
			if req.SessionHandle != nil {
				args = append(args, "--session", string(*req.SessionHandle))
			}
			if req.Prompt != "" {
				args = append(args, req.Prompt)
			}
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
	if err != nil && stdout.Len() == 0 && stderr.Len() == 0 {
		if class := classifyHermesLaunchFailure(err); class != "" {
			result.ErrorClass = class
			result.Payload = diagnosticPayload(class, class, nil, []byte(err.Error()))
			return result, err
		}
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

func validateHermesWorkspace(workspace string) (Result, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return Result{}, nil
	}
	info, err := os.Stat(workspace)
	if err != nil {
		class := ErrorClassWorkspaceInvalid
		if os.IsNotExist(err) {
			class = ErrorClassWorkspaceMissing
		}
		return Result{OK: false, ExitCode: -1, ErrorClass: class, Payload: diagnosticPayload(class, class, nil, []byte(err.Error()))}, fmt.Errorf("%s: %w", class, err)
	}
	if !info.IsDir() {
		class := ErrorClassWorkspaceInvalid
		return Result{OK: false, ExitCode: -1, ErrorClass: class, Payload: diagnosticPayload(class, class, nil, []byte("workspace is not a directory"))}, fmt.Errorf("%s: workspace is not a directory", class)
	}
	return Result{}, nil
}

func classifyHermesLaunchFailure(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "chdir") || strings.Contains(lower, "current directory") {
		if strings.Contains(lower, "no such file") || strings.Contains(lower, "not found") {
			return ErrorClassWorkspaceMissing
		}
		return ErrorClassWorkspaceInvalid
	}
	return ""
}

type hermesSemanticOutput struct {
	EventType string
	Payload   map[string]any
}

func parseHermesResponseOutput(stdout, stderr []byte) (hermesSemanticOutput, string, string) {
	lines := nonEmptyOutputLines(stdout)
	var responseLines []string
	for index, line := range lines {
		if isHermesCLIControlLine(line) {
			continue
		}
		if looksLikeJSON(line) {
			if len(responseLines) > 0 {
				return hermesSemanticOutput{}, ErrorClassMalformedOrMissingResponse, ErrorClassMalformedOrMissingResponse
			}
			semantic, class, reason := parseHermesJSONText(strings.Join(lines[index:], "\n"))
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

func parseHermesJSONText(text string) (hermesSemanticOutput, string, string) {
	var raw map[string]any
	decoder := json.NewDecoder(strings.NewReader(text))
	if err := decoder.Decode(&raw); err != nil {
		return hermesSemanticOutput{}, ErrorClassMalformedOrMissingResponse, ErrorClassMalformedOrMissingResponse
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return hermesSemanticOutput{}, ErrorClassMalformedOrMissingResponse, ErrorClassMalformedOrMissingResponse
	}
	return parseHermesJSON(raw)
}

func parseHermesJSON(raw map[string]any) (hermesSemanticOutput, string, string) {
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

func isHermesCLIControlLine(line string) bool {
	return strings.HasPrefix(line, "session_handle=") || strings.HasPrefix(line, "session_id:") || strings.HasPrefix(line, "session_id=")
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

type hermesVisibleDeliveryOutput struct {
	Status    string
	Kind      string
	Platform  string
	ChannelID string
	ThreadID  string
	MessageID string
	Payload   map[string]any
}

func parseHermesVisibleDeliveryOutput(stdout []byte) (hermesVisibleDeliveryOutput, string, string) {
	text := strings.TrimSpace(string(stdout))
	if text == "" {
		return hermesVisibleDeliveryOutput{}, ErrorClassVisibleDeliveryMalformed, "missing_visible_delivery_output"
	}
	var raw map[string]any
	decoder := json.NewDecoder(strings.NewReader(text))
	if err := decoder.Decode(&raw); err == nil {
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return hermesVisibleDeliveryOutput{}, ErrorClassVisibleDeliveryMalformed, ErrorClassVisibleDeliveryMalformed
		}
		return parseHermesVisibleDeliveryJSON(raw)
	}
	parsed := hermesVisibleDeliveryOutput{Payload: map[string]any{}}
	for _, line := range nonEmptyOutputLines(stdout) {
		if isHermesCLIControlLine(line) {
			continue
		}
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			switch strings.TrimSpace(key) {
			case "status", "delivery_status":
				parsed.Status = value
			case "kind":
				parsed.Kind = value
			case "platform":
				parsed.Platform = value
			case "channel_id", "chat_id":
				parsed.ChannelID = value
			case "thread_id":
				parsed.ThreadID = value
			case "message_id", "discord_message_id":
				parsed.MessageID = value
			}
			parsed.Payload[key] = value
		}
	}
	if parsed.MessageID == "" {
		return hermesVisibleDeliveryOutput{}, ErrorClassVisibleDeliveryMalformed, "visible_delivery_message_id_missing"
	}
	if parsed.Status == "" {
		parsed.Status = "posted"
	}
	return parsed, "", ""
}

func parseHermesVisibleDeliveryJSON(raw map[string]any) (hermesVisibleDeliveryOutput, string, string) {
	if raw == nil {
		return hermesVisibleDeliveryOutput{}, ErrorClassVisibleDeliveryMalformed, ErrorClassVisibleDeliveryMalformed
	}
	if errText := firstString(raw, "error", "error_message"); errText != "" {
		return hermesVisibleDeliveryOutput{}, ErrorClassVisibleDeliveryFailed, "visible_delivery_error_result"
	}
	parsed := hermesVisibleDeliveryOutput{
		Status:    firstStringDeep(raw, "status", "delivery_status"),
		Kind:      firstStringDeep(raw, "kind", "surface_kind"),
		Platform:  firstStringDeep(raw, "platform"),
		ChannelID: firstStringDeep(raw, "channel_id", "chat_id"),
		ThreadID:  firstStringDeep(raw, "thread_id"),
		MessageID: firstStringDeep(raw, "message_id", "discord_message_id", "messageId"),
		Payload:   raw,
	}
	if parsed.MessageID == "" {
		if message, ok := raw["message"].(map[string]any); ok {
			parsed.MessageID = firstString(message, "id", "message_id")
		}
	}
	if parsed.MessageID == "" {
		return hermesVisibleDeliveryOutput{}, ErrorClassVisibleDeliveryMalformed, "visible_delivery_message_id_missing"
	}
	if parsed.Status == "" {
		parsed.Status = "posted"
	}
	return parsed, "", ""
}

func firstStringDeep(raw map[string]any, keys ...string) string {
	if value := firstString(raw, keys...); value != "" {
		return value
	}
	for _, candidate := range raw {
		switch typed := candidate.(type) {
		case map[string]any:
			if value := firstStringDeep(typed, keys...); value != "" {
				return value
			}
		case []any:
			for _, item := range typed {
				if nested, ok := item.(map[string]any); ok {
					if value := firstStringDeep(nested, keys...); value != "" {
						return value
					}
				}
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
