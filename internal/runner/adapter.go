package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/SeventeenthEarth/agent-turn-network-control/internal/registry"
)

const HermesAgentKind = "hermes-agent"
const HermesAgentCostSource = "hermes-agent-stderr-parse"

var ErrSemantic = errors.New("semantic runner failure")

const (
	ErrorClassAdapterCommandMismatch     = "adapter_command_mismatch"
	ErrorClassModelProviderFailure       = "model_provider_failure"
	ErrorClassTimeout                    = "timeout"
	ErrorClassMalformedOrMissingResponse = "malformed_or_missing_response"
	ErrorClassStalePhaseEvidence         = "stale_phase_evidence"
	ErrorClassWorkspaceMissing           = "workspace_missing"
	ErrorClassWorkspaceInvalid           = "workspace_invalid"
	ErrorClassVisibleDeliveryFailed      = "visible_delivery_failed"
	ErrorClassVisibleDeliveryMalformed   = "visible_delivery_malformed"
)

type SessionHandle string

type Cost struct {
	TokensIn    int     `json:"tokens_in"`
	TokensOut   int     `json:"tokens_out"`
	USDEstimate float64 `json:"usd_estimate"`
	Source      string  `json:"source"`
}

type Request struct {
	SessionID       string
	Member          registry.Member
	ResolvedWrapper string
	Prompt          string
	SessionHandle   *SessionHandle
	Timeout         time.Duration
	InvocationID    string
	SourceCommandID string
	Attempt         int
	Env             []string
	Args            []string
}

type Result struct {
	OK                bool
	Stdout            []byte
	Stderr            []byte
	ExitCode          int
	Duration          time.Duration
	Cost              *Cost
	SessionHandle     *SessionHandle
	SemanticStatus    string
	SemanticEventType string
	Payload           map[string]any
	ErrorClass        string
	Discarded         bool
}

type VisibleDeliveryRequest struct {
	SessionID       string
	Member          registry.Member
	ResolvedWrapper string
	Target          string
	Content         string
	Kind            string
	Platform        string
	ChannelID       string
	ThreadID        string
	Timeout         time.Duration
	Env             []string
	Args            []string
}

type VisibleDeliveryResult struct {
	OK            bool
	Stdout        []byte
	Stderr        []byte
	ExitCode      int
	Duration      time.Duration
	Status        string
	Kind          string
	Platform      string
	ChannelID     string
	ThreadID      string
	MessageID     string
	PostingPath   string
	SenderMember  string
	Payload       map[string]any
	ErrorClass    string
	DiagnosticMsg string
}

type VisibleSender interface {
	SendVisible(ctx context.Context, req VisibleDeliveryRequest) (VisibleDeliveryResult, error)
}

type Adapter interface {
	Kind() string
	CostSource() string
	Send(ctx context.Context, req Request) (Result, error)
	Resume(ctx context.Context, req Request) (Result, error)
	Cancel(ctx context.Context, handle SessionHandle) error
	ParseSessionHandle(stdout []byte) (*SessionHandle, error)
}

type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry(adapters ...Adapter) (*Registry, error) {
	reg := &Registry{adapters: map[string]Adapter{}}
	if len(adapters) == 0 {
		adapters = []Adapter{NewHermesAgentAdapter()}
	}
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, fmt.Errorf("runner adapter is nil")
		}
		if adapter.Kind() != HermesAgentKind {
			return nil, fmt.Errorf("unsupported runner adapter kind %q", adapter.Kind())
		}
		if adapter.CostSource() != HermesAgentCostSource {
			return nil, fmt.Errorf("adapter %q declares unsupported cost source %q", adapter.Kind(), adapter.CostSource())
		}
		reg.adapters[adapter.Kind()] = adapter
	}
	if _, ok := reg.adapters[HermesAgentKind]; !ok {
		return nil, fmt.Errorf("required runner adapter %q is not registered", HermesAgentKind)
	}
	return reg, nil
}

func (r *Registry) Get(kind string) (Adapter, error) {
	if r == nil {
		return nil, fmt.Errorf("runner registry is nil")
	}
	adapter, ok := r.adapters[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported runner adapter kind %q", kind)
	}
	return adapter, nil
}

func (r *Registry) Kinds() []string {
	if r == nil || len(r.adapters) == 0 {
		return nil
	}
	return []string{HermesAgentKind}
}

func CostRaw(cost *Cost) json.RawMessage {
	if cost == nil {
		return json.RawMessage("null")
	}
	payload := map[string]any{
		"tokens_in":    cost.TokensIn,
		"tokens_out":   cost.TokensOut,
		"usd_estimate": cost.USDEstimate,
		"source":       cost.Source,
	}
	content, _ := json.Marshal(payload)
	return content
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
