package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"kkachi-agent-network-control/internal/command"
	"kkachi-agent-network-control/internal/daemon"
	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/transport"
)

func TestIntegrationDaemonUnavailableMapsToExitTwo(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run([]string{"daemon", "status"}, &stdout, &stderr)

	if exitCode != protocol.ExitDaemonUnavailable {
		t.Fatalf("expected daemon unavailable exit 2, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `"code":"daemon_unavailable"`) {
		t.Fatalf("expected structured unavailable error, got %q", stderr.String())
	}
	assertCommandJSONError(t, stderr.String(), "daemon_unavailable", "daemon")
}

func TestIntegrationDaemonLifecycleStartStatusHealthStopAndAlreadyRunning(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
	app, cancel := cliWithInProcessDaemon(t)
	defer cancel()

	var startOut bytes.Buffer
	var startErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "start"}, &startOut, &startErr); exitCode != 0 {
		t.Fatalf("expected daemon start exit 0, got %d stdout=%q stderr=%q", exitCode, startOut.String(), startErr.String())
	}
	if !strings.Contains(startOut.String(), `"ready":true`) {
		t.Fatalf("expected ready start output, got %q", startOut.String())
	}

	var againOut bytes.Buffer
	var againErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "start"}, &againOut, &againErr); exitCode != 0 {
		t.Fatalf("expected already-running start exit 0, got %d stdout=%q stderr=%q", exitCode, againOut.String(), againErr.String())
	}
	if !strings.Contains(againOut.String(), `"already_running":true`) {
		t.Fatalf("expected already running output, got %q", againOut.String())
	}

	for _, args := range [][]string{{"daemon", "status"}, {"status"}, {"daemon", "health"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode != 0 {
			t.Fatalf("%v expected exit 0, got %d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "ready") {
			t.Fatalf("%v expected diagnostic JSON, got %q", args, stdout.String())
		}
	}

	var stopOut bytes.Buffer
	var stopErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "stop"}, &stopOut, &stopErr); exitCode != 0 {
		t.Fatalf("expected daemon stop exit 0, got %d stdout=%q stderr=%q", exitCode, stopOut.String(), stopErr.String())
	}
	if !strings.Contains(stopOut.String(), `"stopping":true`) {
		t.Fatalf("expected stopping output, got %q", stopOut.String())
	}
}

func TestIntegrationDaemonStartCleansStaleSocketAndFailsClosedOnAmbiguousSocket(t *testing.T) {
	t.Run("stale socket", func(t *testing.T) {
		dataHome := commandDaemonFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
		app, cancel := cliWithInProcessDaemon(t)
		defer cancel()
		dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
		if err != nil {
			t.Fatalf("ensure runtime dir: %v", err)
		}
		socketPath := filepath.Join(dir, transport.SocketName)
		makeCommandStaleSocket(t, socketPath)

		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := app.Run([]string{"daemon", "start"}, &stdout, &stderr); exitCode != 0 {
			t.Fatalf("expected daemon start over stale socket exit 0, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
		}
	})

	t.Run("ambiguous socket", func(t *testing.T) {
		dataHome := commandDaemonFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
		app, _ := cliWithInProcessDaemon(t)
		dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
		if err != nil {
			t.Fatalf("ensure runtime dir: %v", err)
		}
		socketPath := filepath.Join(dir, transport.SocketName)
		if err := os.WriteFile(socketPath, []byte("not a socket"), 0o600); err != nil {
			t.Fatalf("write ambiguous socket: %v", err)
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := app.Run([]string{"daemon", "start"}, &stdout, &stderr); exitCode != protocol.ExitUnsafe {
			t.Fatalf("expected ambiguous socket exit 3, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
		}
	})
}

func TestIntegrationDaemonStartUnsafeRegistryMapsToExitThree(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
	if err := os.Chmod(registry.RegistryPath(dataHome), 0o666); err != nil {
		t.Fatalf("chmod unsafe registry: %v", err)
	}
	app, _ := cliWithInProcessDaemon(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := app.Run([]string{"daemon", "start"}, &stdout, &stderr)

	if exitCode != protocol.ExitUnsafe {
		t.Fatalf("expected unsafe registry exit 3, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	assertCommandJSONError(t, stderr.String(), "unsafe_runtime", "security")
	log := readOperationalLog(t, dataHome)
	if !strings.Contains(log, `"event":"security_violation"`) || !strings.Contains(log, `"category":"registry_permissions_unsafe"`) {
		t.Fatalf("expected registry permissions violation in operational log, got %q", log)
	}
	if strings.Contains(log, "Moderator") || strings.Contains(log, "Agent One") {
		t.Fatalf("operational log leaked registry content: %q", log)
	}
}

func TestIntegrationDaemonStartUnsafeDataHomeFailsClosedWithoutOperationalLog(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
	if err := os.Chmod(dataHome, 0o777); err != nil {
		t.Fatalf("chmod unsafe data home: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run([]string{"daemon", "start"}, &stdout, &stderr)

	if exitCode != protocol.ExitUnsafe {
		t.Fatalf("expected unsafe data home exit 3, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	assertCommandJSONError(t, stderr.String(), "unsafe_runtime", "security")
	if _, err := os.Lstat(filepath.Join(dataHome, "operational.log")); !os.IsNotExist(err) {
		t.Fatalf("unsafe data home must not receive operational log, err=%v", err)
	}
}

func TestIntegrationDaemonRequestsFailClosedBeforeUnsafeDial(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		setup func(t *testing.T, dataHome string)
	}{
		{
			name: "unsafe data home daemon status",
			args: []string{"daemon", "status"},
			setup: func(t *testing.T, dataHome string) {
				t.Helper()
				if err := os.Chmod(dataHome, 0o777); err != nil {
					t.Fatalf("chmod unsafe data home: %v", err)
				}
			},
		},
		{
			name: "unsafe run dir top status",
			args: []string{"status"},
			setup: func(t *testing.T, dataHome string) {
				t.Helper()
				dir := filepath.Join(dataHome, transport.RuntimeDirName)
				if err := os.Mkdir(dir, 0o777); err != nil {
					t.Fatalf("mkdir unsafe run dir: %v", err)
				}
				if err := os.Chmod(dir, 0o777); err != nil {
					t.Fatalf("chmod unsafe run dir: %v", err)
				}
			},
		},
		{
			name: "symlink socket daemon health",
			args: []string{"daemon", "health"},
			setup: func(t *testing.T, dataHome string) {
				t.Helper()
				dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
				if err != nil {
					t.Fatalf("ensure runtime dir: %v", err)
				}
				if err := os.Symlink("/tmp/not-a-kan-socket", filepath.Join(dir, transport.SocketName)); err != nil {
					t.Fatalf("symlink socket: %v", err)
				}
			},
		},
		{
			name: "non socket daemon stop",
			args: []string{"daemon", "stop"},
			setup: func(t *testing.T, dataHome string) {
				t.Helper()
				dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
				if err != nil {
					t.Fatalf("ensure runtime dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, transport.SocketName), []byte("spoof"), 0o600); err != nil {
					t.Fatalf("write spoof socket path: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := commandDaemonFixture(t)
			t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
			tc.setup(t, dataHome)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := command.NewCLI().Run(tc.args, &stdout, &stderr)

			if exitCode != protocol.ExitUnsafe {
				t.Fatalf("expected unsafe exit 3, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			assertCommandJSONError(t, stderr.String(), "unsafe_runtime", "security")
		})
	}
}

func TestIntegrationUnsupportedCLICommandReturnsJSONAndDoesNotWrite(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
	before := commandTreeFingerprint(t, dataHome)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run([]string{"stream", "sess_1", "--follow"}, &stdout, &stderr)
	after := commandTreeFingerprint(t, dataHome)

	if exitCode != protocol.ExitUsage {
		t.Fatalf("expected unsupported validation exit 1, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `"code":"unsupported_feature"`) {
		t.Fatalf("expected structured unsupported error, got %q", stderr.String())
	}
	if before != after {
		t.Fatalf("unsupported CLI command wrote files\nbefore=%s\nafter=%s", before, after)
	}
}

func TestIntegrationDoctorAndDaemonHealthAreReadOnlyAndRedacted(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
	t.Setenv("ANTHROPIC_API_KEY", "secret-value")
	app, cancel := cliWithInProcessDaemon(t)
	defer cancel()
	var startOut bytes.Buffer
	var startErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "start"}, &startOut, &startErr); exitCode != 0 {
		t.Fatalf("start daemon: exit=%d stdout=%q stderr=%q", exitCode, startOut.String(), startErr.String())
	}
	before := commandTreeFingerprint(t, dataHome)

	for _, args := range [][]string{{"doctor"}, {"daemon", "health"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode != 0 {
			t.Fatalf("%v expected exit 0, got %d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
		output := stdout.String() + stderr.String()
		for _, forbidden := range []string{"secret-value", "ANTHROPIC_API_KEY", "Moderator", "Agent One"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("%v leaked secret or raw registry content %q in %s", args, forbidden, output)
			}
		}
	}
	after := commandTreeFingerprint(t, dataHome)
	if before != after {
		t.Fatalf("doctor/health changed files\nbefore=%s\nafter=%s", before, after)
	}
}

func TestUnitFutureDAEMN002CLICommandsAreNotImplemented(t *testing.T) {
	for _, args := range [][]string{
		{"stream", "sess_1"},
		{"stream", "ack", "sess_1", "--cursor", "cursor_1"},
		{"status", "sess_1"},
		{"conformance", "fixtures"},
		{"version", "--features"},
		{"active-session", "lock"},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := command.NewCLI().Run(args, &stdout, &stderr)
		if exitCode != protocol.ExitUsage && (args[0] != "version" || exitCode != 0) {
			t.Fatalf("%v expected unsupported validation exit 1, got %d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
		if args[0] == "version" && len(args) > 1 && exitCode == 0 {
			t.Fatalf("version feature endpoint should not be implemented, got stdout=%q", stdout.String())
		}
	}
}

func assertCommandJSONError(t *testing.T, stderr string, wantCode string, wantCategory string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code     string `json:"code"`
			Category string `json:"category"`
			Message  string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
		t.Fatalf("decode stderr JSON envelope %q: %v", stderr, err)
	}
	if envelope.Error.Code != wantCode || envelope.Error.Category != wantCategory || envelope.Error.Message == "" {
		t.Fatalf("unexpected JSON error envelope: %+v", envelope)
	}
}

func readOperationalLog(t *testing.T, dataHome string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dataHome, "operational.log"))
	if err != nil {
		t.Fatalf("read operational log: %v", err)
	}
	return string(data)
}

func commandDaemonFixture(t *testing.T) string {
	t.Helper()
	dataHome, err := os.MkdirTemp("/private/tmp", "kan-command-daemon-")
	if err != nil {
		t.Fatalf("make short temp data home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataHome) })
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	registryYAML := `schema_version: 1
members:
  agent-mod:
    display_name: Moderator
    wrapper: missing-agent-mod-wrapper
    workspace: /tmp/agent-mod
    role: moderator
    enabled: false
    adapter_kind: hermes-agent
  agent-1:
    display_name: Agent One
    wrapper: missing-agent-1-wrapper
    workspace: /tmp/agent-1
    role: assignee
    enabled: false
    adapter_kind: hermes-agent
`
	if err := os.WriteFile(registry.RegistryPath(dataHome), []byte(registryYAML), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return dataHome
}

func cliWithInProcessDaemon(t *testing.T) (command.App, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	app := command.NewCLIWithRuntime(registry.DefaultRuntime())
	app.StartDaemon = func(dataHome string, runtime registry.Runtime) error {
		server := daemon.NewServer(dataHome, runtime)
		go func() {
			_ = server.Run(ctx)
		}()
		return nil
	}
	return app, cancel
}

func commandTreeFingerprint(t *testing.T, root string) string {
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

func makeCommandStaleSocket(t *testing.T, path string) {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("create stale socket fd: %v", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		t.Fatalf("bind stale socket fixture: %v", err)
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("close stale socket fd: %v", err)
	}
}
