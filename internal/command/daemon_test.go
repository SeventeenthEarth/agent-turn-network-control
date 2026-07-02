package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"atn-control/internal/command"
	"atn-control/internal/daemon"
	"atn-control/internal/protocol"
	"atn-control/internal/registry"
	"atn-control/internal/storage"
	"atn-control/internal/transport"
)

func TestIntegrationDaemonUnavailableMapsToExitTwo(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("ATN_HOME", dataHome)

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

func TestUnitCOUNCILSTAB001CouncilGrantMemberAliasParsesBeforeDaemon(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	app := command.NewCLI()

	var stdout, stderr bytes.Buffer
	exitCode := app.Run([]string{"council", "grant", "sess_member_alias", "--from", "agent-mod", "--member", "agent-1", "--command-id", "cmd_member_alias"}, &stdout, &stderr)
	if exitCode != protocol.ExitDaemonUnavailable {
		t.Fatalf("--member should parse as grant target before daemon submit, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `"code":"daemon_unavailable"`) {
		t.Fatalf("--member should reach daemon transport path, got stderr=%q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = app.Run([]string{"council", "grant", "sess_member_alias", "--from", "agent-mod", "--to", "agent-1", "--member", "agent-2", "--command-id", "cmd_member_alias_conflict"}, &stdout, &stderr)
	if exitCode != protocol.ExitUsage {
		t.Fatalf("conflicting --to/--member should fail usage before daemon, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "member") || !strings.Contains(stderr.String(), "to") {
		t.Fatalf("conflict error should mention member and to, got %q", stderr.String())
	}
}

func TestUnitLVCOR003CouncilLifecycleFromFileSchemaDiagnostics(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	app := command.NewCLI()
	path := filepath.Join(t.TempDir(), "hand-raise-legacy.json")
	if err := os.WriteFile(path, []byte(`{"round":1,"member":"agent-1","reason":"legacy round field"}`), 0o600); err != nil {
		t.Fatalf("write legacy hand-raise payload: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := app.Run([]string{"council", "hand-raise", "sess_lvcor003_schema", "--from", "agent-1", "--command-id", "cmd_lvcor003_schema", "--from-file", path}, &stdout, &stderr)
	if exitCode != protocol.ExitUsage {
		t.Fatalf("legacy lifecycle payload should fail before daemon submit, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	for _, want := range []string{"payload.round", "canonical field", "turn", "council hand-raise --from-file"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("schema diagnostic missing %q: %s", want, stderr.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = app.Run([]string{"council", "--help"}, &stdout, &stderr)
	if exitCode != protocol.ExitOK {
		t.Fatalf("council help exit=%d stderr=%q", exitCode, stderr.String())
	}
	for _, want := range []string{"--turn <n>", "--from-file <json>", "final_summary", "surface_evidence"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("council help missing canonical lifecycle hint %q:\n%s", want, stdout.String())
		}
	}
}

func TestUnitTranscriptExportTailCLIValidationDoesNotRequireDaemon(t *testing.T) {
	app := command.NewCLIWithRuntime(commandFixedRuntime())
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "transcript format", args: []string{"transcript", "sess_command", "--format", "html"}, want: "transcript format must be md or jsonl"},
		{name: "export unsafe output", args: []string{"export", "sess_command", "--bundle", "--output", "../escape"}, want: "output path must not contain NUL or dot-dot segments"},
		{name: "tail format", args: []string{"tail", "sess_command"}, want: "tail requires --format ndjson"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if exitCode := app.Run(tc.args, &stdout, &stderr); exitCode != protocol.ExitUsage {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr missing %q: %s", tc.want, stderr.String())
			}
		})
	}

	var helpOut bytes.Buffer
	var helpErr bytes.Buffer
	if exitCode := app.Run([]string{"transcript", "--help"}, &helpOut, &helpErr); exitCode != 0 {
		t.Fatalf("help exit=%d stderr=%q", exitCode, helpErr.String())
	}
	if !strings.Contains(helpOut.String(), "transcript <session_id> --format md|jsonl") {
		t.Fatalf("transcript help missing usage: %s", helpOut.String())
	}
}

func TestUnitRootHelpListsTranscriptExportTailAndCompat(t *testing.T) {
	help := command.NewCLI().Help()
	for _, want := range []string{"transcript", "export", "tail", "compat"} {
		if !strings.Contains(help, want) {
			t.Fatalf("root help missing %q:\n%s", want, help)
		}
	}
}

func TestIntegrationDaemonLifecycleStartStatusHealthStopAndAlreadyRunning(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("ATN_HOME", dataHome)
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
		t.Setenv("ATN_HOME", dataHome)
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
		t.Setenv("ATN_HOME", dataHome)
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
	t.Setenv("ATN_HOME", dataHome)
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
	t.Setenv("ATN_HOME", dataHome)
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
			t.Setenv("ATN_HOME", dataHome)
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
	t.Setenv("ATN_HOME", dataHome)
	before := commandTreeFingerprint(t, dataHome)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run([]string{"stream", "sess_1", "--member", "agent-1", "--from-start", "--format", "ndjson", "--teleport"}, &stdout, &stderr)
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
	t.Setenv("ATN_HOME", dataHome)
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

func TestUnitDAEMN002VersionAndConformanceCLIAreLocalJSON(t *testing.T) {
	for _, args := range [][]string{
		{"version", "--features", "--format", "json"},
		{"conformance", "fixtures", "--format", "json"},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := command.NewCLI().Run(args, &stdout, &stderr)
		if exitCode != 0 {
			t.Fatalf("%v expected exit 0, got %d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
		if !json.Valid(stdout.Bytes()) {
			t.Fatalf("%v expected JSON stdout, got %q", args, stdout.String())
		}
		if strings.Contains(stdout.String(), "stream.tail") {
			t.Fatalf("%v must not expose stream.tail: %s", args, stdout.String())
		}
	}
}

func TestIntegrationDAEMN002CLIStreamAckStatusAndDeliveryEvidence(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	metadata := commandSessionFixture(t, dataHome)
	app, cancel := cliWithInProcessDaemonAndFollowHook(t, metadata)
	defer cancel()
	var startOut, startErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "start"}, &startOut, &startErr); exitCode != 0 {
		t.Fatalf("start daemon: exit=%d stdout=%q stderr=%q", exitCode, startOut.String(), startErr.String())
	}

	var streamOut, streamErr bytes.Buffer
	if exitCode := app.Run([]string{"stream", metadata.ID, "--member", "agent-1", "--from-start", "--format", "ndjson"}, &streamOut, &streamErr); exitCode != 0 {
		t.Fatalf("stream from-start: exit=%d stdout=%q stderr=%q", exitCode, streamOut.String(), streamErr.String())
	}
	if lines := strings.Split(strings.TrimSpace(streamOut.String()), "\n"); len(lines) < 2 || !strings.Contains(lines[0], `"is_replay":true`) {
		t.Fatalf("unexpected stream ndjson: %q", streamOut.String())
	}

	var sinceOut, sinceErr bytes.Buffer
	if exitCode := app.Run([]string{"stream", metadata.ID, "--member", "agent-1", "--since", "cur_000000000000_evt_created_001", "--follow", "--follow-timeout-ms", "500", "--follow-poll-ms", "5", "--format", "ndjson"}, &sinceOut, &sinceErr); exitCode != 0 {
		t.Fatalf("stream since/follow: exit=%d stdout=%q stderr=%q", exitCode, sinceOut.String(), sinceErr.String())
	}
	if strings.Contains(sinceOut.String(), "evt_created_001") || !strings.Contains(sinceOut.String(), "evt_user_escalation_requested_01") {
		t.Fatalf("since cursor should be exclusive, got %q", sinceOut.String())
	}
	if !strings.Contains(sinceOut.String(), "evt_cli_live_follow") || !strings.Contains(sinceOut.String(), `"is_replay":false`) {
		t.Fatalf("follow should emit appended live frame after replay, got %q", sinceOut.String())
	}

	var ackOut, ackErr bytes.Buffer
	if exitCode := app.Run([]string{"stream", "ack", metadata.ID, "--member", "agent-1", "--cursor", "cur_000000000000_evt_created_001", "--command-id", "cmd_cli_ack"}, &ackOut, &ackErr); exitCode != 0 {
		t.Fatalf("stream ack: exit=%d stdout=%q stderr=%q", exitCode, ackOut.String(), ackErr.String())
	}
	if !strings.Contains(ackOut.String(), "evt_stream_ack") {
		t.Fatalf("expected ack result, got %q", ackOut.String())
	}

	var statusOut, statusErr bytes.Buffer
	if exitCode := app.Run([]string{"stream", "status", metadata.ID}, &statusOut, &statusErr); exitCode != 0 {
		t.Fatalf("stream status: exit=%d stdout=%q stderr=%q", exitCode, statusOut.String(), statusErr.String())
	}
	if !strings.Contains(statusOut.String(), "cmd_cli_ack") && !strings.Contains(statusOut.String(), "cur_000000000000_evt_created_001") {
		t.Fatalf("status missing ack cursor: %q", statusOut.String())
	}

	var deliveredOut, deliveredErr bytes.Buffer
	if exitCode := app.Run([]string{"delegate", "escalation-delivered", metadata.ID, "--escalation", "evt_user_escalation_requested_01", "--delivery-target", "origin", "--platform", "hermes", "--message-ref", "msg_1", "--command-id", "cmd_cli_delivered"}, &deliveredOut, &deliveredErr); exitCode != 0 {
		t.Fatalf("delivery evidence: exit=%d stdout=%q stderr=%q", exitCode, deliveredOut.String(), deliveredErr.String())
	}
	var failedOut, failedErr bytes.Buffer
	if exitCode := app.Run([]string{"delegate", "escalation-delivery-failed", metadata.ID, "--escalation", "evt_user_escalation_requested_01", "--target", "telegram", "--reason", "unreachable", "--will-retry-target", "origin", "--command-id", "cmd_cli_delivery_failed"}, &failedOut, &failedErr); exitCode != 0 {
		t.Fatalf("delivery failure evidence: exit=%d stdout=%q stderr=%q", exitCode, failedOut.String(), failedErr.String())
	}
}

func TestIntegrationLTRAN003LiveLocalCLIProof(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	app, cancel := cliWithInProcessDaemon(t)
	defer cancel()
	var startOut, startErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "start"}, &startOut, &startErr); exitCode != 0 {
		t.Fatalf("start daemon: exit=%d stdout=%q stderr=%q", exitCode, startOut.String(), startErr.String())
	}

	runOK := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode != 0 {
			t.Fatalf("%v exit=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
		if len(stdout.Bytes()) > 0 && !json.Valid(stdout.Bytes()) && args[0] != "stream" {
			t.Fatalf("%v expected JSON stdout, got %q", args, stdout.String())
		}
		return stdout.String()
	}
	runFail := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode == 0 {
			t.Fatalf("%v expected failure, stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), `"error"`) {
			t.Fatalf("%v expected structured error, got stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		return stderr.String()
	}

	aliasConflict := runFail("council", "new", "Conflicting output mode aliases", "--members", "agent-1,agent-2", "--moderator", "agent-mod", "--requested-output-mode", "live_visible_thread", "--output-mode", "local-daemon-only", "--explicit-non-visible-override", "true", "--override-reason", "operator explicitly requested local diagnostics")
	if !strings.Contains(aliasConflict, "output-mode aliases") {
		t.Fatalf("CLI alias conflict should fail closed before daemon write, got %s", aliasConflict)
	}

	for _, args := range [][]string{
		{"compat", "version", "--format", "json"},
		{"compat", "status", "--format", "json"},
		{"compat", "diagnostics", "--format", "json"},
	} {
		output := runOK(args...)
		for _, want := range []string{protocol.ProtocolVersion, "schema_version", "daemon_version", "min_plugin_protocol_version", "version.read", "status.read", "diagnostics.read", "stream.replay", "stream.ack", "stream.status"} {
			if !strings.Contains(output, want) {
				t.Fatalf("%v missing %q: %s", args, want, output)
			}
		}
	}

	runOK("daemon", "status")
	runOK("status")
	runOK("daemon", "health")
	runOK("delegate", "new", "sess_ltran_003", "--moderator", "agent-mod", "--assignee", "agent-1", "--title", "LTRAN-003 disposable proof", "--task", "prove live-local write path", "--event-id", "evt_ltran003_created", "--assignment-event-id", "evt_ltran003_assigned", "--command-id", "cmd_ltran003_new")

	replay := runOK("stream", "sess_ltran_003", "--member", "agent-1", "--from-start", "--format", "ndjson")
	if !strings.Contains(replay, `"is_replay":true`) || !strings.Contains(replay, "evt_ltran003_created") {
		t.Fatalf("replay missing durable frames: %s", replay)
	}
	runFail("stream", "sess_ltran_003", "--member", "agent-1", "--since", "cur_999999999999_evt_gap", "--format", "ndjson")
	runFail("stream", "sess_ltran_003", "--member", "agent-1", "--from-start", "--format", "ndjson", "--teleport")

	runOK("stream", "ack", "sess_ltran_003", "--member", "agent-1", "--cursor", "cur_000000000000_evt_ltran003_created", "--command-id", "cmd_ltran003_ack_cursor")
	status := runOK("stream", "status", "sess_ltran_003")
	if !strings.Contains(status, "cmd_ltran003_ack_cursor") || !strings.Contains(status, "cur_000000000000_evt_ltran003_created") {
		t.Fatalf("stream status missing ack evidence: %s", status)
	}

	runOK("delegate", "ack", "sess_ltran_003", "--actor", "agent-1", "--understanding", "ready", "--command-id", "cmd_ltran003_ack")
	submit := runOK("delegate", "submit", "sess_ltran_003", "--actor", "agent-1", "--summary", "first submit", "--command-id", "cmd_ltran003_submit")
	if !strings.Contains(submit, `"deduplicated":false`) {
		t.Fatalf("first submit should append: %s", submit)
	}
	duplicate := runOK("delegate", "submit", "sess_ltran_003", "--actor", "agent-1", "--summary", "first submit", "--command-id", "cmd_ltran003_submit")
	if !strings.Contains(duplicate, `"deduplicated":true`) {
		t.Fatalf("duplicate submit should dedupe: %s", duplicate)
	}
	conflict := runFail("delegate", "submit", "sess_ltran_003", "--actor", "agent-1", "--summary", "different submit", "--command-id", "cmd_ltran003_submit")
	if !strings.Contains(conflict, "command_id already used with different payload") {
		t.Fatalf("conflict missing command-id message: %s", conflict)
	}
}

func TestIntegrationNEWFIX007CouncilLockAgendaFromFileParsesRequiredContext(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	binDir := filepath.Join(dataHome, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir fake wrapper dir: %v", err)
	}
	for _, wrapper := range []string{"agent-mod", "agent-1", "agent-2"} {
		if err := os.WriteFile(filepath.Join(binDir, wrapper), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatalf("write fake wrapper %s: %v", wrapper, err)
		}
	}
	registryYAML := fmt.Sprintf(`schema_version: 1
wrapper_path_allowlist:
  - %s
members:
  agent-mod:
    display_name: Moderator
    wrapper: agent-mod
    workspace: /tmp/agent-mod
    role: moderator
    enabled: true
    adapter_kind: hermes-agent
  agent-1:
    display_name: Agent One
    wrapper: agent-1
    workspace: /tmp/agent-1
    role: assignee
    enabled: true
    adapter_kind: hermes-agent
  agent-2:
    display_name: Agent Two
    wrapper: agent-2
    workspace: /tmp/agent-2
    role: assignee
    enabled: true
    adapter_kind: hermes-agent
`, binDir)
	if err := os.WriteFile(registry.RegistryPath(dataHome), []byte(registryYAML), 0o600); err != nil {
		t.Fatalf("write enabled registry: %v", err)
	}
	t.Setenv("ATN_HOME", dataHome)
	app, cancel := cliWithInProcessDaemon(t)
	defer cancel()
	var startOut, startErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "start"}, &startOut, &startErr); exitCode != 0 {
		t.Fatalf("start daemon: exit=%d stdout=%q stderr=%q", exitCode, startOut.String(), startErr.String())
	}

	runOK := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode != 0 {
			t.Fatalf("%v exit=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
		if !json.Valid(stdout.Bytes()) {
			t.Fatalf("%v expected JSON stdout, got %q", args, stdout.String())
		}
		return stdout.String()
	}
	runFail := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode == 0 {
			t.Fatalf("%v expected failure, stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), `"error"`) {
			t.Fatalf("%v expected structured error, got stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		return stderr.String()
	}

	runOK("council", "new", "NEWFIX-007 local proof", "--session-id", "sess_newfix007_cli", "--members", "agent-1,agent-2", "--moderator", "agent-mod", "--requested-output-mode", "local-daemon-only", "--explicit-non-visible-override", "true", "--override-reason", "operator explicitly requested local contract proof", "--command-id", "cmd_newfix007_new")

	agendaPath := filepath.Join(t.TempDir(), "agenda.json")
	if err := os.WriteFile(agendaPath, []byte(`{"decision_question":"What proves NEWFIX-007?","success_criteria":"The locked agenda carries mandatory selected-runner context.","out_of_scope_policy":"Do not infer required agenda context from draft prose.","max_rounds":2}`), 0o600); err != nil {
		t.Fatalf("write agenda: %v", err)
	}
	runOK("council", "lock-agenda", "sess_newfix007_cli", "--from", "agent-mod", "--command-id", "cmd_newfix007_lock", "--from-file", agendaPath)
	sessionDir, err := storage.SessionDir(dataHome, "sess_newfix007_cli")
	if err != nil {
		t.Fatalf("session dir: %v", err)
	}
	channelBytes, err := os.ReadFile(filepath.Join(sessionDir, storage.ChannelJSONLName))
	if err != nil {
		t.Fatalf("read council channel: %v", err)
	}
	channel := string(channelBytes)
	for _, want := range []string{"What proves NEWFIX-007?", "mandatory selected-runner context", "Do not infer required agenda context", `"max_rounds":2`} {
		if !strings.Contains(channel, want) {
			t.Fatalf("lock-agenda event missing %q: %s", want, channel)
		}
	}
	if strings.Contains(channel, `"draft"`) {
		t.Fatalf("lock-agenda --from-file must not collapse structured agenda JSON into draft: %s", channel)
	}

	missingPath := filepath.Join(t.TempDir(), "missing.json")
	if err := os.WriteFile(missingPath, []byte(`{"decision_question":"What proves NEWFIX-007?","out_of_scope_policy":"No inference."}`), 0o600); err != nil {
		t.Fatalf("write missing agenda: %v", err)
	}
	missing := runFail("council", "lock-agenda", "sess_newfix007_cli", "--from", "agent-mod", "--command-id", "cmd_newfix007_missing", "--from-file", missingPath)
	if !strings.Contains(missing, "missing required field success_criteria") {
		t.Fatalf("missing required context should fail closed, got %s", missing)
	}

	emptyPath := filepath.Join(t.TempDir(), "empty_success.json")
	if err := os.WriteFile(emptyPath, []byte(`{"decision_question":"What proves NEWFIX-007?","success_criteria":"","out_of_scope_policy":"No inference."}`), 0o600); err != nil {
		t.Fatalf("write empty success agenda: %v", err)
	}
	empty := runFail("council", "lock-agenda", "sess_newfix007_cli", "--from", "agent-mod", "--command-id", "cmd_newfix007_empty_success", "--from-file", emptyPath)
	if !strings.Contains(empty, "success_criteria must be a non-empty string") {
		t.Fatalf("empty success_criteria should fail closed, got %s", empty)
	}

	whitespacePath := filepath.Join(t.TempDir(), "whitespace_success.json")
	if err := os.WriteFile(whitespacePath, []byte(`{"decision_question":"What proves NEWFIX-007?","success_criteria":"     ","out_of_scope_policy":"No inference."}`), 0o600); err != nil {
		t.Fatalf("write whitespace success agenda: %v", err)
	}
	whitespace := runFail("council", "lock-agenda", "sess_newfix007_cli", "--from", "agent-mod", "--command-id", "cmd_newfix007_whitespace_success", "--from-file", whitespacePath)
	if !strings.Contains(whitespace, "success_criteria must be a non-empty string") {
		t.Fatalf("whitespace success_criteria should fail closed, got %s", whitespace)
	}

	unsupportedPath := filepath.Join(t.TempDir(), "unsupported.json")
	if err := os.WriteFile(unsupportedPath, []byte(`{"decision_question":"Q","success_criteria":"S","out_of_scope_policy":"O","agenda_items":["unsupported"]}`), 0o600); err != nil {
		t.Fatalf("write unsupported agenda: %v", err)
	}
	unsupported := runFail("council", "lock-agenda", "sess_newfix007_cli", "--from", "agent-mod", "--command-id", "cmd_newfix007_unsupported", "--from-file", unsupportedPath)
	if !strings.Contains(unsupported, "unsupported field") || !strings.Contains(unsupported, "agenda_items") {
		t.Fatalf("unsupported agenda field should fail closed, got %s", unsupported)
	}

	legacyHintPath := filepath.Join(t.TempDir(), "legacy_hint.json")
	if err := os.WriteFile(legacyHintPath, []byte(`{"decision_question":"Q","success_criteria":"S","out_of_scope_policy":"O","summary":"unsupported display hint"}`), 0o600); err != nil {
		t.Fatalf("write legacy hint agenda: %v", err)
	}
	legacyHint := runFail("council", "lock-agenda", "sess_newfix007_cli", "--from", "agent-mod", "--command-id", "cmd_newfix007_legacy_hint", "--from-file", legacyHintPath)
	if !strings.Contains(legacyHint, "unsupported field") || !strings.Contains(legacyHint, "summary") {
		t.Fatalf("legacy agenda hint should fail closed, got %s", legacyHint)
	}
}

func TestIntegrationLVCOR002CouncilFinalizeAndUnresolvedFromFileJSON(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	binDir := filepath.Join(dataHome, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatalf("mkdir LVCOR-002 wrapper bin: %v", err)
	}
	for _, wrapper := range []string{"agent-mod", "agent-1"} {
		if err := os.WriteFile(filepath.Join(binDir, wrapper), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatalf("write LVCOR-002 wrapper %s: %v", wrapper, err)
		}
	}
	registryYAML := fmt.Sprintf(`schema_version: 1
wrapper_path_allowlist:
  - %s
members:
  agent-mod:
    display_name: Moderator
    wrapper: agent-mod
    workspace: /tmp/agent-mod
    role: moderator
    enabled: true
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
  agent-1:
    display_name: Agent One
    wrapper: agent-1
    workspace: /tmp/agent-1
    role: council_member
    enabled: true
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
`, binDir)
	if err := os.WriteFile(registry.RegistryPath(dataHome), []byte(registryYAML), 0o600); err != nil {
		t.Fatalf("write LVCOR-002 enabled registry: %v", err)
	}
	t.Setenv("ATN_HOME", dataHome)
	app, cancel := cliWithInProcessDaemon(t)
	defer cancel()
	var startOut, startErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "start"}, &startOut, &startErr); exitCode != 0 {
		t.Fatalf("start daemon: exit=%d stdout=%q stderr=%q", exitCode, startOut.String(), startErr.String())
	}

	runOK := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode != 0 {
			t.Fatalf("%v exit=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
		if !json.Valid(stdout.Bytes()) {
			t.Fatalf("%v expected JSON stdout, got %q", args, stdout.String())
		}
		return stdout.String()
	}
	runFail := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode == 0 {
			t.Fatalf("%v expected failure, stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), `"error"`) {
			t.Fatalf("%v expected structured error, got stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		return stderr.String()
	}

	runOK("council", "new", "LVCOR-002 finalize file", "--session-id", "sess_lvcor002_finalize_cli", "--members", "agent-1", "--moderator", "agent-mod", "--requested-output-mode", "local-daemon-only", "--explicit-non-visible-override", "true", "--override-reason", "CLI from-file schema regression only; live-visible storage proof is covered by storage tests.", "--command-id", "cmd_lvcor002_finalize_new")
	runOK("council", "request-attendance", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--timeout", "5m", "--command-id", "cmd_lvcor002_finalize_attendance")
	runOK("council", "attend", "sess_lvcor002_finalize_cli", "--from", "agent-1", "--status", "present", "--summary", "Present.", "--command-id", "cmd_lvcor002_finalize_attend")
	runOK("council", "lock-agenda", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--decision-question", "What proves LVCOR-002?", "--success-criteria", "Finalize records exact visible closeout proof.", "--out-of-scope-policy", "Do not infer missing closeout proof.", "--command-id", "cmd_lvcor002_finalize_agenda")
	runOK("council", "prepare", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--timeout", "10m", "--command-id", "cmd_lvcor002_finalize_prepare")
	runOK("council", "ready", "sess_lvcor002_finalize_cli", "--from", "agent-1", "--summary", "Ready.", "--command-id", "cmd_lvcor002_finalize_ready")
	runOK("council", "poll", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--research-timeout", "10m", "--command-id", "cmd_lvcor002_finalize_poll")
	runOK("council", "hand-raise", "sess_lvcor002_finalize_cli", "--from", "agent-1", "--round", "1", "--intent", "closeout", "--reason", "Need one bounded closeout turn.", "--command-id", "cmd_lvcor002_finalize_raise")
	runOK("council", "grant", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--member", "agent-1", "--round", "1", "--mode", "moderator_direct", "--command-id", "cmd_lvcor002_finalize_grant")
	runOK("council", "speak", "sess_lvcor002_finalize_cli", "--from", "agent-1", "--round", "1", "--speech", "Lifecycle evidence is complete.", "--command-id", "cmd_lvcor002_finalize_speak")
	draftPath := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(draftPath, []byte("Finalize from-file proof is now canonical."), 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}
	runOK("council", "propose", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--command-id", "cmd_lvcor002_finalize_propose", "--from-file", draftPath)
	runOK("council", "request-vote", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--draft-version", "1", "--timeout", "10m", "--command-id", "cmd_lvcor002_finalize_request_vote")
	runOK("council", "vote", "sess_lvcor002_finalize_cli", "--from", "agent-1", "--vote", "approve", "--reason", "Ready.", "--command-id", "cmd_lvcor002_finalize_vote")
	finalizePath := filepath.Join(t.TempDir(), "finalize.json")
	if err := os.WriteFile(finalizePath, []byte(`{"final_summary":"LVCOR-002 finalize from-file proof.","surface_evidence":{"status":"posted","kind":"discord_thread","thread_id":"thread-lvcor002-finalize","final_message_id":"msg-lvcor002-finalize"},"linked_authority_result":{"status":"posted","kanban_comment_id":"kc_lvcor002_finalize"}}`), 0o600); err != nil {
		t.Fatalf("write finalize payload: %v", err)
	}
	runOK("council", "finalize", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--command-id", "cmd_lvcor002_finalize", "--from-file", finalizePath)
	finalizeDir, err := storage.SessionDir(dataHome, "sess_lvcor002_finalize_cli")
	if err != nil {
		t.Fatalf("finalize session dir: %v", err)
	}
	finalizeMeta, err := storage.LoadSessionYAML(finalizeDir)
	if err != nil {
		t.Fatalf("load finalize metadata: %v", err)
	}
	finalizeIndex, err := storage.ReadLogIndex(finalizeDir, finalizeMeta)
	if err != nil {
		t.Fatalf("read finalize log: %v", err)
	}
	finalized := finalizeIndex.Events[len(finalizeIndex.Events)-1]
	if finalized.Type != "council_finalized" {
		t.Fatalf("finalize from-file type = %s payload=%#v", finalized.Type, finalized.Payload)
	}
	if _, ok := finalized.Payload["surface_evidence"].(map[string]any); !ok {
		t.Fatalf("finalize from-file surface_evidence missing: %#v", finalized.Payload)
	}
	if _, ok := finalized.Payload["linked_authority_result"].(map[string]any); !ok {
		t.Fatalf("finalize from-file linked_authority_result missing: %#v", finalized.Payload)
	}
	if got := finalized.Payload["final_summary"]; got != "LVCOR-002 finalize from-file proof." {
		t.Fatalf("finalize from-file summary = %#v", got)
	}

	missingFinalizePath := filepath.Join(t.TempDir(), "finalize-missing.json")
	if err := os.WriteFile(missingFinalizePath, []byte(`{"surface_evidence":{"status":"posted","thread_id":"thread-lvcor002-finalize","final_message_id":"msg-lvcor002-finalize"}}`), 0o600); err != nil {
		t.Fatalf("write finalize missing payload: %v", err)
	}
	missingFinalize := runFail("council", "finalize", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--command-id", "cmd_lvcor002_finalize_missing", "--from-file", missingFinalizePath)
	if !strings.Contains(missingFinalize, "missing required field final_summary") {
		t.Fatalf("finalize --from-file missing summary should fail closed, got %s", missingFinalize)
	}

	malformedSurfacePath := filepath.Join(t.TempDir(), "finalize-malformed-surface.json")
	if err := os.WriteFile(malformedSurfacePath, []byte(`{"final_summary":"Malformed surface evidence must fail.","surface_evidence":"posted"}`), 0o600); err != nil {
		t.Fatalf("write malformed surface payload: %v", err)
	}
	malformedSurface := runFail("council", "finalize", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--command-id", "cmd_lvcor002_finalize_malformed_surface", "--from-file", malformedSurfacePath)
	if !strings.Contains(malformedSurface, "surface_evidence must be a non-empty object") {
		t.Fatalf("finalize --from-file malformed surface_evidence should fail closed, got %s", malformedSurface)
	}

	unsupportedFinalizePath := filepath.Join(t.TempDir(), "finalize-unsupported.json")
	if err := os.WriteFile(unsupportedFinalizePath, []byte(`{"final_summary":"Unsupported field must fail.","summary":"legacy hint"}`), 0o600); err != nil {
		t.Fatalf("write unsupported finalize payload: %v", err)
	}
	unsupportedFinalize := runFail("council", "finalize", "sess_lvcor002_finalize_cli", "--from", "agent-mod", "--command-id", "cmd_lvcor002_finalize_unsupported", "--from-file", unsupportedFinalizePath)
	if !strings.Contains(unsupportedFinalize, "unsupported field") || !strings.Contains(unsupportedFinalize, "summary") {
		t.Fatalf("finalize --from-file unsupported field should fail closed, got %s", unsupportedFinalize)
	}

	runOK("council", "new", "LVCOR-002 unresolved file", "--session-id", "sess_lvcor002_unresolved_cli", "--members", "agent-1", "--moderator", "agent-mod", "--requested-output-mode", "local-daemon-only", "--explicit-non-visible-override", "true", "--override-reason", "CLI from-file schema regression only; unresolved remains an honest terminal alternative.", "--command-id", "cmd_lvcor002_unresolved_new")
	runOK("council", "request-attendance", "sess_lvcor002_unresolved_cli", "--from", "agent-mod", "--timeout", "5m", "--command-id", "cmd_lvcor002_unresolved_attendance")
	runOK("council", "attend", "sess_lvcor002_unresolved_cli", "--from", "agent-1", "--status", "present", "--summary", "Present.", "--command-id", "cmd_lvcor002_unresolved_attend")
	runOK("council", "lock-agenda", "sess_lvcor002_unresolved_cli", "--from", "agent-mod", "--decision-question", "What proves unresolved from-file support?", "--success-criteria", "Unresolved accepts a structured JSON payload.", "--out-of-scope-policy", "Do not invent finalization success.", "--command-id", "cmd_lvcor002_unresolved_agenda")
	runOK("council", "prepare", "sess_lvcor002_unresolved_cli", "--from", "agent-mod", "--timeout", "10m", "--command-id", "cmd_lvcor002_unresolved_prepare")
	runOK("council", "ready", "sess_lvcor002_unresolved_cli", "--from", "agent-1", "--summary", "Ready.", "--command-id", "cmd_lvcor002_unresolved_ready")
	runOK("council", "poll", "sess_lvcor002_unresolved_cli", "--from", "agent-mod", "--research-timeout", "10m", "--command-id", "cmd_lvcor002_unresolved_poll")
	unresolvedPath := filepath.Join(t.TempDir(), "unresolved.json")
	if err := os.WriteFile(unresolvedPath, []byte(`{"reason":"Visible closeout proof still needs follow-up.","timeout_evidence":"operator follow-up required","surface_evidence":{"status":"pending_followup","kind":"discord_thread","thread_id":"thread-lvcor002-unresolved","followup_card_id":"card-lvcor002-unresolved"}}`), 0o600); err != nil {
		t.Fatalf("write unresolved payload: %v", err)
	}
	runOK("council", "unresolved", "sess_lvcor002_unresolved_cli", "--from", "agent-mod", "--command-id", "cmd_lvcor002_unresolved", "--from-file", unresolvedPath)
	unresolvedDir, err := storage.SessionDir(dataHome, "sess_lvcor002_unresolved_cli")
	if err != nil {
		t.Fatalf("unresolved session dir: %v", err)
	}
	unresolvedMeta, err := storage.LoadSessionYAML(unresolvedDir)
	if err != nil {
		t.Fatalf("load unresolved metadata: %v", err)
	}
	unresolvedIndex, err := storage.ReadLogIndex(unresolvedDir, unresolvedMeta)
	if err != nil {
		t.Fatalf("read unresolved log: %v", err)
	}
	unresolved := unresolvedIndex.Events[len(unresolvedIndex.Events)-1]
	if unresolved.Type != "council_unresolved" {
		t.Fatalf("unresolved from-file type = %s payload=%#v", unresolved.Type, unresolved.Payload)
	}
	if got := unresolved.Payload["reason"]; got != "Visible closeout proof still needs follow-up." {
		t.Fatalf("unresolved from-file reason = %#v", got)
	}
	if _, ok := unresolved.Payload["surface_evidence"].(map[string]any); !ok {
		t.Fatalf("unresolved from-file surface_evidence missing: %#v", unresolved.Payload)
	}

	unsupportedUnresolvedPath := filepath.Join(t.TempDir(), "unresolved-unsupported.json")
	if err := os.WriteFile(unsupportedUnresolvedPath, []byte(`{"reason":"still blocked","summary":"unsupported"}`), 0o600); err != nil {
		t.Fatalf("write unresolved unsupported payload: %v", err)
	}
	unsupportedUnresolved := runFail("council", "unresolved", "sess_lvcor002_unresolved_cli", "--from", "agent-mod", "--command-id", "cmd_lvcor002_unresolved_unsupported", "--from-file", unsupportedUnresolvedPath)
	if !strings.Contains(unsupportedUnresolved, "unsupported field") || !strings.Contains(unsupportedUnresolved, "summary") {
		t.Fatalf("unresolved --from-file unsupported field should fail closed, got %s", unsupportedUnresolved)
	}
}

func TestIntegrationCLIDelegateBlockResumeLimitsAndCancel(t *testing.T) {
	dataHome := commandDaemonFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	app, cancel := cliWithInProcessDaemon(t)
	defer cancel()
	var startOut, startErr bytes.Buffer
	if exitCode := app.Run([]string{"daemon", "start"}, &startOut, &startErr); exitCode != 0 {
		t.Fatalf("start daemon: exit=%d stdout=%q stderr=%q", exitCode, startOut.String(), startErr.String())
	}

	runOK := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode != 0 {
			t.Fatalf("%v exit=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
		if !json.Valid(stdout.Bytes()) {
			t.Fatalf("%v expected JSON stdout, got %q", args, stdout.String())
		}
		return stdout.String()
	}
	runFail := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := app.Run(args, &stdout, &stderr); exitCode == 0 {
			t.Fatalf("%v expected failure, stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), `"error"`) {
			t.Fatalf("%v expected structured error, got stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		return stderr.String()
	}

	runOK("delegate", "new", "sess_cli_deleg", "--moderator", "agent-mod", "--assignee", "agent-1", "--title", "CLI DELEG", "--task", "prove CLI path", "--event-id", "evt_cli_deleg_created", "--assignment-event-id", "evt_cli_deleg_assigned", "--command-id", "cmd_cli_deleg_new")
	runOK("delegate", "ack", "sess_cli_deleg", "--actor", "agent-1", "--understanding", "ok", "--command-id", "cmd_cli_deleg_ack")
	runFail("block", "sess_cli_deleg", "--actor", "agent-1", "--category", "external_dependency", "--reason", "not moderator")
	blockJSON := runOK("block", "sess_cli_deleg", "--category", "external_dependency", "--reason", "dependency down", "--command-id", "cmd_cli_block")
	var blockResult struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal([]byte(blockJSON), &blockResult); err != nil || blockResult.EventID == "" {
		t.Fatalf("decode block event id: result=%+v err=%v json=%s", blockResult, err, blockJSON)
	}
	runOK("resume", "sess_cli_deleg", "--blocked-event", blockResult.EventID, "--reason", "dependency recovered", "--command-id", "cmd_cli_resume")
	cancelJSON := runOK("cancel", "sess_cli_deleg", "--reason", "done testing", "--command-id", "cmd_cli_cancel")
	if !strings.Contains(cancelJSON, "evt_session_cancelled") {
		t.Fatalf("cancel should return session_cancelled event id, got %s", cancelJSON)
	}

	runOK("delegate", "new", "sess_cli_budget", "--moderator", "agent-mod", "--assignee", "agent-1", "--title", "CLI budget", "--task", "prove limits path", "--event-id", "evt_cli_budget_created", "--assignment-event-id", "evt_cli_budget_assigned", "--command-id", "cmd_cli_budget_new")
	budgetDir, err := storage.SessionDir(dataHome, "sess_cli_budget")
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	budgetMeta, err := storage.LoadSessionYAML(budgetDir)
	if err != nil {
		t.Fatalf("LoadSessionYAML: %v", err)
	}
	if _, err := storage.AppendEvent(budgetDir, budgetMeta, storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_cli_budget_block",
		CommandID:     "cmd_cli_budget_block",
		CorrelationID: budgetMeta.ID,
		SessionID:     budgetMeta.ID,
		SessionType:   budgetMeta.SessionType,
		Phase:         "blocked",
		Type:          "session_budget_exceeded",
		From:          "atn-controld",
		To:            []string{"agent-mod"},
		CreatedAt:     commandFixedRuntime().Now().Add(time.Second),
		Payload:       map[string]any{"limit_kind": "max_runner_calls", "observed": 1, "limit": 1, "prior_phase": "working", "resume_phase": "working", "action": "session_blocked"},
	}); err != nil {
		t.Fatalf("append budget block: %v", err)
	}
	runFail("resume", "sess_cli_budget", "--blocked-event", "evt_cli_budget_block", "--reason", "manual resume")
	runFail("limits", "extend", "sess_cli_budget", "--blocked-event", "evt_cli_budget_block", "--key", "max_runner_calls", "--value", "2")
	runOK("limits", "extend", "sess_cli_budget", "--blocked-event", "evt_cli_budget_block", "--key", "max_runner_calls", "--value", "2", "--authorized-by", "user", "--reason", "approved", "--command-id", "cmd_cli_limits_extend")
	statusJSON := runOK("status", "sess_cli_budget", "--verbose")
	if !strings.Contains(statusJSON, `"phase":"working"`) || !strings.Contains(statusJSON, `"status":"open"`) {
		t.Fatalf("status should show limits-resumed open working session, got %s", statusJSON)
	}
	limitsJSON := runOK("limits", "show", "sess_cli_budget")
	if !strings.Contains(limitsJSON, `"max_runner_calls"`) {
		t.Fatalf("limits show missing limit fields: %s", limitsJSON)
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

func cliWithInProcessDaemonAndFollowHook(t *testing.T, metadata *storage.SessionMetadata) (command.App, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	app := command.NewCLIWithRuntime(registry.DefaultRuntime())
	app.StartDaemon = func(dataHome string, runtime registry.Runtime) error {
		server := daemon.NewServer(dataHome, runtime)
		sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
		if err != nil {
			return err
		}
		server.StreamFollowAfterReplay = func() error {
			var appendErr error
			once.Do(func() {
				_, appendErr = storage.AppendEvent(sessionDir, metadata, storage.EventEnvelope{
					SchemaVersion: protocol.SchemaVersion,
					EventID:       "evt_cli_live_follow",
					CommandID:     "cmd_cli_live_follow",
					CorrelationID: metadata.ID,
					SessionID:     metadata.ID,
					SessionType:   metadata.SessionType,
					Phase:         "working",
					Type:          "assignee_update",
					From:          "agent-1",
					To:            []string{"agent-mod"},
					CreatedAt:     commandFixedRuntime().Now().Add(time.Second),
					Payload:       map[string]any{"message": "live follow"},
				})
			})
			return appendErr
		}
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

func commandSessionFixture(t *testing.T, dataHome string) *storage.SessionMetadata {
	t.Helper()
	loaded, err := registry.Load(dataHome, commandFixedRuntime())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{
		ID:           "sess_command",
		SessionType:  storage.SessionTypeDelegation,
		Title:        "Command stream fixture",
		Moderator:    "agent-mod",
		Participants: []string{"agent-mod", "agent-1"},
		EventID:      "evt_created_001",
		CommandID:    "cmd_create_command",
	}, commandFixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	escalation := storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_user_escalation_requested_01",
		CommandID:     "cmd_escalate_command_fixture",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "waiting_user",
		Type:          "user_escalation_requested",
		From:          "agent-1",
		To:            []string{"agent-mod"},
		CreatedAt:     commandFixedRuntime().Now(),
		Payload:       map[string]any{"question": "Need input", "urgency": "blocked"},
	}
	if _, err := storage.AppendEvent(sessionDir, metadata, escalation); err != nil {
		t.Fatalf("append escalation: %v", err)
	}
	return metadata
}

func commandFixedRuntime() registry.Runtime {
	return registry.Runtime{
		LookupEnv:   func(string) (string, bool) { return "", false },
		UserHomeDir: func() (string, error) { return "/tmp/home", nil },
		CurrentUID:  os.Getuid,
		Now:         func() time.Time { return time.Date(2026, 6, 5, 1, 0, 0, 0, time.UTC) },
	}
}
