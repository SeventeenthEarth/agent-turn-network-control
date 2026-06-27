package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atn-control/internal/registry"
)

func TestUnitRegistryWhitelistsOnlyHermesAgent(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Get(HermesAgentKind); err != nil {
		t.Fatalf("hermes-agent should be registered: %v", err)
	}
	if _, err := reg.Get("codex-cli"); err == nil {
		t.Fatalf("unknown adapter kind should fail before dispatch")
	}
	_, err = NewRegistry(fakeAdapter{kind: "codex-cli", costSource: HermesAgentCostSource})
	if err == nil {
		t.Fatalf("non hermes adapter registration should fail")
	}
}

func TestUnitParseHermesCostScansLast32KBAndMissingIsNil(t *testing.T) {
	prefix := strings.Repeat("x", 40*1024)
	stderr := []byte(prefix + "\n{\"hermes_cost\":{\"tokens_in\":7,\"tokens_out\":11,\"usd_estimate\":0.25}}\n")
	cost := ParseHermesCost(stderr)
	if cost == nil || cost.TokensIn != 7 || cost.TokensOut != 11 || cost.USDEstimate != 0.25 || cost.Source != HermesAgentCostSource {
		t.Fatalf("unexpected cost: %#v", cost)
	}
	if got := ParseHermesCost([]byte("not json\n{\"hermes_cost\":{\"tokens_in\":\"bad\"}}\n")); got != nil {
		t.Fatalf("malformed cost should be nil, got %#v", got)
	}
	if got := ParseHermesCost([]byte("ordinary stderr")); got != nil {
		t.Fatalf("missing cost should be nil, got %#v", got)
	}
}

func TestUnitEnvForMemberUsesOnlyAllowlist(t *testing.T) {
	member := registry.Member{EnvAllowlist: []string{"KEEP_ME", "MISSING"}}
	env := EnvForMember(member, registry.Runtime{LookupEnv: func(name string) (string, bool) {
		switch name {
		case "KEEP_ME":
			return "value", true
		case "SECRET_TOKEN":
			return "leak", true
		default:
			return "", false
		}
	}})
	if len(env) != 1 || env[0] != "KEEP_ME=value" {
		t.Fatalf("unexpected env propagation: %#v", env)
	}
}

func TestIntegrationHermesAdapterUsesResolvedWrapperArgvAndParsesCost(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "fake-hermes")
	log := filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$0|$1|$2|$3|$4|$KEEP_ME|${SECRET_TOKEN-unset}\" > " + shellQuote(log) + "\nprintf '%s\\n' 'session_handle=abc123'\nprintf '%s\\n' '{\"type\":\"speech\",\"payload\":{\"turn\":1,\"speech\":\"hello world\"}}'\nprintf '%s\\n' '{\"hermes_cost\":{\"tokens_in\":3,\"tokens_out\":4,\"usd_estimate\":0.05}}' >&2\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	adapter := NewHermesAgentAdapter()
	result, err := adapter.Send(context.Background(), Request{
		ResolvedWrapper: wrapper,
		Member:          registry.Member{Workspace: dir},
		Prompt:          "hello world",
		Env:             []string{"KEEP_ME=yes"},
	})
	if err != nil || !result.OK {
		t.Fatalf("Send failed: result=%#v err=%v", result, err)
	}
	argv, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	if got := strings.TrimSpace(string(argv)); got != wrapper+"|chat|-Q|-q|hello world|yes|unset" {
		t.Fatalf("wrapper was not invoked by resolved argv/minimal env: %q", got)
	}
	if result.Cost == nil || result.Cost.TokensIn != 3 || result.Cost.Source != HermesAgentCostSource {
		t.Fatalf("cost not parsed: %#v stderr=%q", result.Cost, result.Stderr)
	}
	if result.SessionHandle == nil || *result.SessionHandle != "abc123" {
		t.Fatalf("session handle not parsed: %#v", result.SessionHandle)
	}
	if result.SemanticEventType != "speech" || result.Payload["speech"] != "hello world" {
		t.Fatalf("typed response payload not parsed: event=%q payload=%#v", result.SemanticEventType, result.Payload)
	}
}

func TestIntegrationHermesAdapterSendVisibleUsesProfileSendAndParsesMessageID(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "fake-hermes")
	log := filepath.Join(dir, "visible-argv.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$0|$1|$2|$3|$4|$5|$KEEP_ME|${SECRET_TOKEN-unset}\" > " + shellQuote(log) + "\nprintf '%s\\n' '{\"ok\":true,\"status\":\"posted\",\"kind\":\"discord_thread\",\"platform\":\"discord\",\"channel_id\":\"chan-live\",\"thread_id\":\"thread-live\",\"message_id\":\"msg-live\"}'\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	result, err := NewHermesAgentAdapter().SendVisible(context.Background(), VisibleDeliveryRequest{
		ResolvedWrapper: wrapper,
		Member:          registry.Member{ID: "jangbi", Workspace: dir},
		Target:          "discord:chan-live:thread-live",
		Content:         "visible speech",
		Kind:            "discord_thread",
		Platform:        "discord",
		ChannelID:       "chan-live",
		ThreadID:        "thread-live",
		Env:             []string{"KEEP_ME=yes"},
	})
	if err != nil || !result.OK {
		t.Fatalf("SendVisible failed: result=%#v err=%v", result, err)
	}
	argv, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	if got := strings.TrimSpace(string(argv)); got != wrapper+"|send|--to|discord:chan-live:thread-live|--json|visible speech|yes|unset" {
		t.Fatalf("visible delivery wrapper argv/env mismatch: %q", got)
	}
	if result.MessageID != "msg-live" || result.Status != "posted" || result.PostingPath != "selected_member_profile_send" || result.SenderMember != "jangbi" {
		t.Fatalf("visible delivery result did not parse/bind message evidence: %#v", result)
	}
}

func TestIntegrationHermesAdapterSendVisibleFailsClosedWithoutMessageID(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "fake-hermes")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"status\":\"posted\"}'\n"), 0o700); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	result, err := NewHermesAgentAdapter().SendVisible(context.Background(), VisibleDeliveryRequest{ResolvedWrapper: wrapper, Member: registry.Member{ID: "jangbi", Workspace: dir}, Target: "discord:chan-live:thread-live", Content: "visible speech"})
	if err == nil || result.OK || result.ErrorClass != ErrorClassVisibleDeliveryMalformed {
		t.Fatalf("SendVisible without message_id should fail closed, result=%#v err=%v", result, err)
	}
}

func TestIntegrationHermesAdapterTimeoutReportsMissingCost(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "fake-hermes")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nsleep 1\n"), 0o700); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	adapter := NewHermesAgentAdapter()
	result, err := adapter.Send(context.Background(), Request{ResolvedWrapper: wrapper, Member: registry.Member{Workspace: dir}, Timeout: 10 * time.Millisecond})
	if err == nil || result.OK || result.ErrorClass != "timeout" {
		t.Fatalf("expected timeout result, got %#v err=%v", result, err)
	}
	if result.Cost != nil {
		t.Fatalf("timeout cost should be nil: %#v", result.Cost)
	}
}

func TestIntegrationHermesAdapterMissingWorkspaceIsLaunchFailureNotMalformedResponse(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "fake-hermes")
	marker := filepath.Join(dir, "invoked")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\nprintf '%s\\n' 'should not run'\n"), 0o700); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	missingWorkspace := filepath.Join(dir, "missing-workspace")

	result, err := NewHermesAgentAdapter().Send(context.Background(), Request{ResolvedWrapper: wrapper, Member: registry.Member{Workspace: missingWorkspace}})
	if err == nil || result.OK {
		t.Fatalf("missing workspace should fail before wrapper launch, result=%#v err=%v", result, err)
	}
	if result.ErrorClass != "workspace_missing" {
		t.Fatalf("missing workspace should be classified explicitly, not as malformed output: result=%#v err=%v", result, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("wrapper should not be invoked when workspace is missing, stat=%v", statErr)
	}
}

func TestIntegrationHermesAdapterClassifiesResponseGenerationOutput(t *testing.T) {
	tests := []struct {
		name            string
		stdout          string
		stderr          string
		exit            int
		wantOK          bool
		wantEvent       string
		wantClass       string
		forbidExcerpt   []string
		wantErr         bool
		wantPayloadKey  string
		wantSemanticErr bool
	}{
		{
			name:      "explicit response text succeeds",
			stdout:    "session_handle=abc\nfresh response text\n",
			wantOK:    true,
			wantEvent: "assignee_update",
		},
		{
			name:      "typed compact jsonl speech succeeds",
			stdout:    "session_handle=abc\n{\"type\":\"speech\",\"payload\":{\"turn\":1,\"speech\":\"hello from compact jsonl\"}}\n",
			wantOK:    true,
			wantEvent: "speech",
		},
		{
			name:          "delivery shaped output is adapter mismatch",
			stdout:        "session_handle=abc\nplatform_delivery=posted message_id=123\n",
			wantClass:     ErrorClassAdapterCommandMismatch,
			wantErr:       true,
			forbidExcerpt: []string{"message_id=123"},
		},
		{
			name:            "typed platform delivery is adapter mismatch",
			stdout:          "session_handle=abc\n{\"type\":\"platform_delivery\",\"message_id\":\"123\"}\n",
			wantClass:       ErrorClassAdapterCommandMismatch,
			wantErr:         true,
			wantPayloadKey:  "diagnostic_excerpt",
			forbidExcerpt:   []string{"message_id", "message_id\":\"123"},
			wantSemanticErr: true,
		},
		{
			name:            "typed fallback delivery is adapter mismatch",
			stdout:          "session_handle=abc\n{\"type\":\"fallback\",\"message\":\"posted\"}\n",
			wantClass:       ErrorClassAdapterCommandMismatch,
			wantErr:         true,
			wantPayloadKey:  "diagnostic_excerpt",
			wantSemanticErr: true,
		},
		{
			name:           "provider failure is classified and redacted",
			stdout:         "session_handle=abc\n",
			stderr:         "provider error: model not found wrapper=/tmp/secret-wrapper SECRET_TOKEN=supersecret\n",
			exit:           1,
			wantClass:      ErrorClassModelProviderFailure,
			wantErr:        true,
			wantPayloadKey: "diagnostic_excerpt",
			forbidExcerpt:  []string{"supersecret", "SECRET_TOKEN", "/tmp/secret-wrapper"},
		},
		{
			name:      "pretty typed json speech succeeds",
			stdout:    "session_handle=abc\n{\n  \"type\": \"speech\",\n  \"payload\": {\n    \"turn\": 1,\n    \"speech\": \"hello from pretty json\"\n  }\n}\n",
			wantOK:    true,
			wantEvent: "speech",
		},
		{
			name:      "hermes cli session id before pretty typed json succeeds",
			stdout:    "session_id: 20260621_151539_a3a94e\n{\n  \"type\": \"speech\",\n  \"payload\": {\n    \"turn\": 1,\n    \"speech\": \"hello from hermes cli stdout\"\n  }\n}\n",
			wantOK:    true,
			wantEvent: "speech",
		},
		{
			name:      "prose before typed json is malformed missing response",
			stdout:    "session_handle=abc\nI will answer as JSON:\n{\"type\":\"speech\",\"payload\":{\"speech\":\"hello\"}}\n",
			wantClass: ErrorClassMalformedOrMissingResponse,
			wantErr:   true,
		},
		{
			name:      "prose after typed json is malformed missing response",
			stdout:    "session_handle=abc\n{\"type\":\"speech\",\"payload\":{\"speech\":\"hello\"}}\nThat is my answer.\n",
			wantClass: ErrorClassMalformedOrMissingResponse,
			wantErr:   true,
		},
		{
			name:      "malformed json is malformed missing response",
			stdout:    "{not json\n",
			wantClass: ErrorClassMalformedOrMissingResponse,
			wantErr:   true,
		},
		{
			name:      "session handle alone is missing response",
			stdout:    "session_handle=abc\n",
			wantClass: ErrorClassMalformedOrMissingResponse,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			wrapper := filepath.Join(dir, "fake-hermes")
			script := "#!/bin/sh\n"
			if tt.stdout != "" {
				script += "printf '%s' " + shellQuote(tt.stdout) + "\n"
			}
			if tt.stderr != "" {
				script += "printf '%s' " + shellQuote(tt.stderr) + " >&2\n"
			}
			if tt.exit != 0 {
				script += "exit 1\n"
			}
			if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
				t.Fatalf("write wrapper: %v", err)
			}
			result, err := NewHermesAgentAdapter().Send(context.Background(), Request{ResolvedWrapper: wrapper, Member: registry.Member{Workspace: dir}})
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got result=%#v", result)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: result=%#v err=%v", result, err)
			}
			if tt.wantSemanticErr && !errors.Is(err, ErrSemantic) {
				t.Fatalf("error should wrap ErrSemantic, got result=%#v err=%v", result, err)
			}
			if result.OK != tt.wantOK {
				t.Fatalf("OK=%v want %v result=%#v", result.OK, tt.wantOK, result)
			}
			if tt.wantEvent != "" && result.SemanticEventType != tt.wantEvent {
				t.Fatalf("event=%q want %q payload=%#v", result.SemanticEventType, tt.wantEvent, result.Payload)
			}
			if tt.wantClass != "" && result.ErrorClass != tt.wantClass {
				t.Fatalf("error_class=%q want %q result=%#v err=%v", result.ErrorClass, tt.wantClass, result, err)
			}
			if tt.wantPayloadKey != "" && result.Payload[tt.wantPayloadKey] == nil {
				t.Fatalf("payload missing %q: %#v", tt.wantPayloadKey, result.Payload)
			}
			excerpt, _ := result.Payload["diagnostic_excerpt"].(string)
			for _, forbidden := range tt.forbidExcerpt {
				if strings.Contains(excerpt, forbidden) {
					t.Fatalf("diagnostic excerpt leaked %q: %q", forbidden, excerpt)
				}
			}
		})
	}
}

type fakeAdapter struct{ kind, costSource string }

func (f fakeAdapter) Kind() string                                      { return f.kind }
func (f fakeAdapter) CostSource() string                                { return f.costSource }
func (f fakeAdapter) Send(context.Context, Request) (Result, error)     { return Result{}, nil }
func (f fakeAdapter) Resume(context.Context, Request) (Result, error)   { return Result{}, nil }
func (f fakeAdapter) Cancel(context.Context, SessionHandle) error       { return nil }
func (f fakeAdapter) ParseSessionHandle([]byte) (*SessionHandle, error) { return nil, nil }

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
