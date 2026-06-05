package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/daemon"
	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/transport"
)

func TestIntegrationDaemonLifecycleStatusHealthAndShutdown(t *testing.T) {
	dataHome := daemonDataHome(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := daemon.NewServer(dataHome, registry.DefaultRuntime())
	errCh := make(chan error, 1)
	go func() { errCh <- server.Run(ctx) }()
	waitForDaemon(t, dataHome)

	status, err := transport.RoundTrip(dataHome, protocol.NewRequest("status", "status", nil), time.Second)
	if err != nil {
		t.Fatalf("status round trip: %v", err)
	}
	if !status.OK || status.Result["daemon"] != "running" || status.Result["ready"] != true {
		t.Fatalf("unexpected status response: %+v", status)
	}

	health, err := transport.RoundTrip(dataHome, protocol.NewRequest("health", "health", nil), time.Second)
	if err != nil {
		t.Fatalf("health round trip: %v", err)
	}
	healthJSON := mustJSON(t, health.Result)
	for _, want := range []string{`"liveness"`, `"readiness"`, `"data_home"`, `"registry"`, `"storage"`, `"socket"`} {
		if !strings.Contains(healthJSON, want) {
			t.Fatalf("expected health to contain %s, got %s", want, healthJSON)
		}
	}

	stop, err := transport.RoundTrip(dataHome, protocol.NewRequest("stop", "shutdown", nil), time.Second)
	if err != nil {
		t.Fatalf("shutdown round trip: %v", err)
	}
	if !stop.OK || stop.Result["stopping"] != true {
		t.Fatalf("unexpected stop response: %+v", stop)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("daemon did not stop")
	}
}

func TestUnitDaemonUnsupportedTransportCommandFailsClosedWithoutWrites(t *testing.T) {
	dataHome := daemonDataHome(t)
	before := treeFingerprint(t, dataHome)
	server := daemon.NewServer(dataHome, registry.DefaultRuntime())
	response := server.Handle(protocol.NewRequest("unsupported", "stream.follow", nil))
	after := treeFingerprint(t, dataHome)

	if response.OK || response.Error == nil || response.Error.Code != protocol.ErrorUnsupportedFeature {
		t.Fatalf("expected unsupported_feature response, got %+v", response)
	}
	if response.Error.ExitCode == 0 {
		t.Fatalf("expected non-zero unsupported result")
	}
	if before != after {
		t.Fatalf("unsupported daemon command wrote files\nbefore=%s\nafter=%s", before, after)
	}
}

func TestUnitDaemonFutureDAEMN002CommandsAreNotImplemented(t *testing.T) {
	server := daemon.NewServer("/tmp/unused", registry.DefaultRuntime())
	for _, command := range []string{"stream", "stream.ack", "conformance.fixtures", "version.features", "active_session.lock"} {
		response := server.Handle(protocol.NewRequest("future", command, nil))
		if response.OK || response.Error == nil || response.Error.Code != protocol.ErrorUnsupportedFeature {
			t.Fatalf("%s should fail closed as unsupported_feature, got %+v", command, response)
		}
	}
}

func TestIntegrationDaemonHealthRedactsRegistryAndSecretContent(t *testing.T) {
	dataHome := daemonDataHome(t)
	secret := "DISCORD_TOKEN_SHOULD_NOT_LEAK"
	t.Setenv("DISCORD_TOKEN", secret)
	server := daemon.NewServer(dataHome, registry.DefaultRuntime())
	response := server.Handle(protocol.NewRequest("health", "health", nil))
	output := mustJSON(t, response)
	for _, forbidden := range []string{secret, "DISCORD_TOKEN", "Reviewer Secret", "missing-secret-wrapper"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("health leaked registry or secret content %q in %s", forbidden, output)
		}
	}
}

func waitForDaemon(t *testing.T, dataHome string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := transport.RoundTrip(dataHome, protocol.NewRequest("ping", "ping", nil), 100*time.Millisecond); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon did not become reachable")
}

func daemonDataHome(t *testing.T) string {
	t.Helper()
	dataHome, err := os.MkdirTemp("/private/tmp", "kan-daemon-")
	if err != nil {
		t.Fatalf("make short temp data home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataHome) })
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	registryYAML := `schema_version: 1
members:
  reviewer-secret:
    display_name: Reviewer Secret
    wrapper: missing-secret-wrapper
    workspace: /tmp/reviewer-secret
    role: reviewer
    enabled: false
    adapter_kind: hermes-agent
`
	if err := os.WriteFile(registry.RegistryPath(dataHome), []byte(registryYAML), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return dataHome
}

func treeFingerprint(t *testing.T, root string) string {
	t.Helper()
	var parts []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts = append(parts, fmt.Sprintf("%s|%s|%d", rel, info.Mode(), info.Size()))
		return nil
	})
	if err != nil {
		t.Fatalf("walk tree: %v", err)
	}
	return strings.Join(parts, "\n")
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
}
