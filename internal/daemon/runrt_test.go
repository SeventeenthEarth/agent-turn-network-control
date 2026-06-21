package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hun-control/internal/daemon"
	"hun-control/internal/registry"
	"hun-control/internal/runner"
	"hun-control/internal/storage"
)

func TestRUNRTDispatcherAppendsStartedBeforeAdapterAndTerminalCost(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_runrt", SessionType: storage.SessionTypeDelegation, Title: "RUNRT", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_runrt", CommandID: "cmd_create_runrt"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "assignee_update", SemanticStatus: "succeeded", Payload: map[string]any{"message": "done"}, Cost: &runner.Cost{TokensIn: 2, TokensOut: 3, USDEstimate: 0.04, Source: runner.HermesAgentCostSource}}}}}
	service := daemon.RunnerDispatchService{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, Now: daemonFixedRuntime().Now}
	result, err := service.Dispatch(context.Background(), daemon.RunnerDispatchRequest{CommandID: "cmd_runrt_dispatch", Prompt: "work"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if adapter.calls != 1 || len(result.InvocationIDs) != 1 {
		t.Fatalf("expected one adapter call/invocation: calls=%d result=%#v", adapter.calls, result)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if len(index.Events) != 4 {
		t.Fatalf("expected created+started+runner success+terminal, got %d", len(index.Events))
	}
	started := index.Events[1]
	succeeded := index.Events[2]
	terminal := index.Events[3]
	if started.Type != "runner_invocation_started" || len(started.Cost) != 0 {
		t.Fatalf("bad started envelope: %#v cost=%s", started, started.Cost)
	}
	if started.From != "kkachi-agent-networkd" || len(started.To) != 1 || started.To[0] != "agent-mod" || started.CommandID != "cmd_runrt_dispatch" {
		t.Fatalf("runner operational event should reuse original command id and address moderator: %#v", started)
	}
	if succeeded.Type != "runner_invocation_succeeded" || string(succeeded.Cost) == "" || succeeded.Runner.InvocationID != started.Runner.InvocationID {
		t.Fatalf("bad runner success envelope: type=%s cost=%s runner=%#v", succeeded.Type, succeeded.Cost, succeeded.Runner)
	}
	if terminal.Type != "assignee_update" || string(terminal.Cost) == "" || terminal.Runner.InvocationID != started.Runner.InvocationID {
		t.Fatalf("bad terminal envelope: type=%s cost=%s runner=%#v", terminal.Type, terminal.Cost, terminal.Runner)
	}
	if terminal.From != "agent-1" || len(terminal.To) != 1 || terminal.To[0] != "agent-mod" || terminal.CommandID != "cmd_runrt_dispatch" {
		t.Fatalf("terminal semantic event should keep member semantic origin and original command id: %#v", terminal)
	}
}

func TestRUNRTDispatcherRetriesWithNewInvocationIDAndNullCostFailure(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_retry", SessionType: storage.SessionTypeDelegation, Title: "RUNRT retry", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_retry", CommandID: "cmd_create_retry"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{
		{result: runner.Result{OK: false, ErrorClass: "nonzero_exit"}, err: errors.New("exit 1")},
		{result: runner.Result{OK: false, ErrorClass: "semantic_error", SemanticStatus: "semantic_error"}, err: errors.New("semantic")},
	}}
	service := daemon.RunnerDispatchService{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, Now: daemonFixedRuntime().Now}
	result, err := service.Dispatch(context.Background(), daemon.RunnerDispatchRequest{CommandID: "cmd_retry", MaxRetries: 1})
	if err != nil {
		t.Fatalf("Dispatch appending failure should succeed: %v", err)
	}
	if adapter.calls != 2 || len(result.InvocationIDs) != 2 || result.InvocationIDs[0] == result.InvocationIDs[1] {
		t.Fatalf("retry should launch twice with unique invocation ids: calls=%d result=%#v", adapter.calls, result)
	}
	index, _ := storage.ReadLogIndex(sessionDir, metadata)
	seenRetry, seenFailure := false, false
	seenStartedAttempts := map[int]string{}
	for _, event := range index.Events {
		switch event.Type {
		case "runner_invocation_started":
			seenStartedAttempts[event.Runner.Attempt] = event.Runner.InvocationID
			if event.CommandID != "cmd_retry" || event.Runner.SourceCommandID != "cmd_retry" {
				t.Fatalf("started retry attempts must reuse original command id/source command id: %#v", event)
			}
			if event.From != "kkachi-agent-networkd" || len(event.To) != 1 || event.To[0] != "agent-mod" {
				t.Fatalf("runner started should be daemon-origin addressed to moderator: %#v", event)
			}
		case "runner_retry_attempted":
			seenRetry = true
			if event.CommandID != "cmd_retry" || event.Runner != nil || len(event.Cost) != 0 {
				t.Fatalf("retry policy event must reuse command id and omit runner/cost: %#v cost=%s", event, event.Cost)
			}
			if event.From != "kkachi-agent-networkd" || len(event.To) != 1 || event.To[0] != "agent-mod" {
				t.Fatalf("retry policy event should be daemon-origin addressed to moderator: %#v", event)
			}
			if event.Payload["original_command_id"] != "cmd_retry" || event.Payload["target_member"] != "agent-1" {
				t.Fatalf("retry payload missing original command/target member evidence: %#v", event.Payload)
			}
		case "runner_invocation_failed":
			seenFailure = true
			if event.CommandID != "cmd_retry" || event.Runner.SourceCommandID != "cmd_retry" {
				t.Fatalf("failure must reuse original command id/source command id: %#v", event)
			}
			if event.From != "kkachi-agent-networkd" || len(event.To) != 1 || event.To[0] != "agent-mod" {
				t.Fatalf("runner failure should be daemon-origin addressed to moderator: %#v", event)
			}
			if string(event.Cost) != "null" {
				t.Fatalf("failure cost should be explicit null, got %s", event.Cost)
			}
		}
	}
	if len(seenStartedAttempts) != 2 || seenStartedAttempts[1] == "" || seenStartedAttempts[2] == "" || seenStartedAttempts[1] == seenStartedAttempts[2] {
		t.Fatalf("retry attempts should have two unique runner invocation ids: %#v", seenStartedAttempts)
	}
	if !seenRetry || !seenFailure {
		t.Fatalf("missing retry/failure events: %#v", index.Events)
	}
}

func TestRUNRTDispatcherNormalizesNonzeroExitStatus(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_nonzero_status", SessionType: storage.SessionTypeDelegation, Title: "RUNRT nonzero", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_nonzero", CommandID: "cmd_create_nonzero"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: false, ErrorClass: "nonzero_exit"}, err: errors.New("exit 1")}}}
	service := daemon.RunnerDispatchService{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, Now: daemonFixedRuntime().Now}
	if _, err := service.Dispatch(context.Background(), daemon.RunnerDispatchRequest{CommandID: "cmd_nonzero", MaxRetries: 0}); err != nil {
		t.Fatalf("Dispatch appending normalized failure should succeed: %v", err)
	}
	index, _ := storage.ReadLogIndex(sessionDir, metadata)
	var failure *storage.EventEnvelope
	for i := range index.Events {
		if index.Events[i].Type == "runner_invocation_failed" {
			failure = &index.Events[i]
			break
		}
	}
	if failure == nil {
		t.Fatalf("missing runner_invocation_failed in %#v", index.Events)
	}
	if failure.Runner.Status != "failed" {
		t.Fatalf("nonzero_exit must be normalized to allowed runner.status failed, got %q", failure.Runner.Status)
	}
	if failure.Payload["error_class"] != "nonzero_exit" || failure.Payload["reason"] != "nonzero_exit_empty_output" {
		t.Fatalf("failure payload should preserve detailed error evidence: %#v", failure.Payload)
	}
}

func TestRUNRTDispatcherPreservesRUNFIX004FailureDiagnostics(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_runfix004_failure", SessionType: storage.SessionTypeDelegation, Title: "RUNFIX-004 failure", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_runfix004_failure", CommandID: "cmd_create_runfix004_failure"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: false, ErrorClass: runner.ErrorClassAdapterCommandMismatch, Payload: map[string]any{"diagnostic_excerpt": "platform_delivery posted [redacted]"}}, err: runner.ErrSemantic}}}
	service := daemon.RunnerDispatchService{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, Now: daemonFixedRuntime().Now}
	if _, err := service.Dispatch(context.Background(), daemon.RunnerDispatchRequest{CommandID: "cmd_runfix004_failure"}); err != nil {
		t.Fatalf("Dispatch appending failure should succeed: %v", err)
	}
	index, _ := storage.ReadLogIndex(sessionDir, metadata)
	failure := findEvent(t, index.Events, "runner_invocation_failed")
	if failure.Payload["error_class"] != runner.ErrorClassAdapterCommandMismatch || failure.Payload["reason"] != runner.ErrorClassAdapterCommandMismatch {
		t.Fatalf("failure payload should preserve RUNFIX-004 class/reason: %#v", failure.Payload)
	}
	if failure.Payload["diagnostic_excerpt"] != "platform_delivery posted [redacted]" {
		t.Fatalf("failure payload should preserve redacted diagnostic excerpt only: %#v", failure.Payload)
	}
}

func TestRUNRTDispatcherClassifiesCancelledLateResultAsStalePhaseEvidence(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_runfix004_stale", SessionType: storage.SessionTypeDelegation, Title: "RUNFIX-004 stale", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_runfix004_stale", CommandID: "cmd_create_runfix004_stale"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true, SemanticEventType: "assignee_update", SemanticStatus: "succeeded", Payload: map[string]any{"message": "late"}, Cost: &runner.Cost{Source: runner.HermesAgentCostSource}}}}}
	service := daemon.RunnerDispatchService{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, Now: daemonFixedRuntime().Now}
	if _, err := service.Dispatch(context.Background(), daemon.RunnerDispatchRequest{CommandID: "cmd_runfix004_stale", Cancelled: func() bool { return true }}); err != nil {
		t.Fatalf("Dispatch appending stale discard should succeed: %v", err)
	}
	index, _ := storage.ReadLogIndex(sessionDir, metadata)
	discarded := findEvent(t, index.Events, "runner_result_discarded")
	if discarded.Payload["error_class"] != runner.ErrorClassStalePhaseEvidence || discarded.Payload["reason"] != runner.ErrorClassStalePhaseEvidence || discarded.Payload["discard_reason"] != "cancelled_or_late_result" {
		t.Fatalf("discard payload should preserve stale phase evidence: %#v", discarded.Payload)
	}
	if discarded.Runner.Status != "discarded_after_cancel" {
		t.Fatalf("discarded runner status=%q want discarded_after_cancel", discarded.Runner.Status)
	}
	if eventTypeCount(index.Events, "assignee_update") != 0 {
		t.Fatalf("late success must not be appended as semantic success: %#v", index.Events)
	}
}

func TestRUNRTDispatcherDoesNotLaunchWhenStartedAppendFails(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_append_fail", SessionType: storage.SessionTypeDelegation, Title: "RUNRT append fail", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_append_fail", CommandID: "cmd_create_append_fail"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	if err := os.Chmod(filepath.Join(sessionDir, storage.ChannelJSONLName), 0o400); err != nil {
		t.Fatalf("chmod channel: %v", err)
	}
	member := loaded.Registry.Members["agent-1"]
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{{result: runner.Result{OK: true}}}}
	service := daemon.RunnerDispatchService{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, Now: daemonFixedRuntime().Now}
	_, err = service.Dispatch(context.Background(), daemon.RunnerDispatchRequest{CommandID: "cmd_append_fail"})
	if err == nil {
		t.Fatalf("expected append failure")
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter launched despite started append failure")
	}
}

func TestRUNRTDispatcherPreDispatchRejectsUnknownAdapter(t *testing.T) {
	dataHome, loaded, wrapper := dispatchDataHome(t)
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{ID: "sess_unknown_adapter", SessionType: storage.SessionTypeDelegation, Title: "RUNRT unknown", Moderator: "agent-mod", Participants: []string{"agent-mod", "agent-1"}, EventID: "evt_created_unknown", CommandID: "cmd_create_unknown"}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, _ := storage.SessionDir(dataHome, metadata.ID)
	member := loaded.Registry.Members["agent-1"]
	member.AdapterKind = "codex-cli"
	member.ResolvedWrapper = &registry.WrapperResolution{ResolvedPath: wrapper}
	adapter := &fakeRunRTAdapter{}
	service := daemon.RunnerDispatchService{SessionDir: sessionDir, Metadata: metadata, Member: member, Adapter: adapter, Runtime: daemonFixedRuntime(), Locks: &daemon.DispatchLocks{}, Now: daemonFixedRuntime().Now}
	if _, err := service.Dispatch(context.Background(), daemon.RunnerDispatchRequest{CommandID: "cmd_unknown"}); err == nil {
		t.Fatalf("unknown adapter should fail before dispatch")
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter launched despite unknown kind")
	}
}

type fakeRunRTResult struct {
	result runner.Result
	err    error
}
type fakeRunRTAdapter struct {
	results []fakeRunRTResult
	calls   int
	reqs    []runner.Request
}

func (f *fakeRunRTAdapter) Kind() string       { return runner.HermesAgentKind }
func (f *fakeRunRTAdapter) CostSource() string { return runner.HermesAgentCostSource }
func (f *fakeRunRTAdapter) Send(ctx context.Context, req runner.Request) (runner.Result, error) {
	idx := f.calls
	f.calls++
	f.reqs = append(f.reqs, req)
	if idx >= len(f.results) {
		return runner.Result{OK: true}, nil
	}
	r := f.results[idx]
	return r.result, r.err
}
func (f *fakeRunRTAdapter) Resume(context.Context, runner.Request) (runner.Result, error) {
	return f.Send(context.Background(), runner.Request{})
}
func (f *fakeRunRTAdapter) Cancel(context.Context, runner.SessionHandle) error       { return nil }
func (f *fakeRunRTAdapter) ParseSessionHandle([]byte) (*runner.SessionHandle, error) { return nil, nil }

func dispatchDataHome(t *testing.T) (string, *registry.LoadedRegistry, string) {
	t.Helper()
	dataHome := t.TempDir()
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	bin := filepath.Join(dataHome, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	wrapper := filepath.Join(bin, "fake-hermes")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	workspace := filepath.Join(dataHome, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "schema_version: 1\nwrapper_path_allowlist:\n  - " + bin + "\nmembers:\n  agent-mod:\n    display_name: Moderator\n    wrapper: fake-hermes\n    workspace: " + workspace + "\n    role: moderator\n    enabled: true\n    adapter_kind: hermes-agent\n    runtime_kind: hermes-cli-stream\n  agent-1:\n    display_name: Agent One\n    wrapper: fake-hermes\n    workspace: " + workspace + "\n    role: assignee\n    enabled: true\n    adapter_kind: hermes-agent\n    runtime_kind: hermes-cli-stream\n    env_allowlist:\n      - KEEP_ME\n"
	if err := os.WriteFile(registry.RegistryPath(dataHome), []byte(content), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	loaded, err := registry.Load(dataHome, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return dataHome, loaded, wrapper
}

var _ = time.Second
