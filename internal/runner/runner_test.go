package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/registry"
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
	script := "#!/bin/sh\nprintf '%s\\n' \"$0|$1|$2|$KEEP_ME|${SECRET_TOKEN-unset}\" > " + shellQuote(log) + "\nprintf '%s\\n' 'session_handle=abc123'\nprintf '%s\\n' '{\"hermes_cost\":{\"tokens_in\":3,\"tokens_out\":4,\"usd_estimate\":0.05}}' >&2\n"
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
	if got := strings.TrimSpace(string(argv)); got != wrapper+"|send|hello world|yes|unset" {
		t.Fatalf("wrapper was not invoked by resolved argv/minimal env: %q", got)
	}
	if result.Cost == nil || result.Cost.TokensIn != 3 || result.Cost.Source != HermesAgentCostSource {
		t.Fatalf("cost not parsed: %#v stderr=%q", result.Cost, result.Stderr)
	}
	if result.SessionHandle == nil || *result.SessionHandle != "abc123" {
		t.Fatalf("session handle not parsed: %#v", result.SessionHandle)
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

type fakeAdapter struct{ kind, costSource string }

func (f fakeAdapter) Kind() string                                      { return f.kind }
func (f fakeAdapter) CostSource() string                                { return f.costSource }
func (f fakeAdapter) Send(context.Context, Request) (Result, error)     { return Result{}, nil }
func (f fakeAdapter) Resume(context.Context, Request) (Result, error)   { return Result{}, nil }
func (f fakeAdapter) Cancel(context.Context, SessionHandle) error       { return nil }
func (f fakeAdapter) ParseSessionHandle([]byte) (*SessionHandle, error) { return nil, nil }

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
