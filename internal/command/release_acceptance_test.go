package command_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/command"
	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/storage"

	_ "modernc.org/sqlite"
)

func TestReleaseAcceptanceChannelCorruptionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, channel string)
	}{
		{
			name: "truncated tail",
			mutate: func(t *testing.T, channel string) {
				t.Helper()
				content := readFile(t, channel)
				writeFile(t, channel, content[:len(content)-1])
			},
		},
		{
			name: "malformed mid file json",
			mutate: func(t *testing.T, channel string) {
				t.Helper()
				content := readFile(t, channel)
				lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
				lines = append(lines[:1], append([]string{`{"event_id":`}, lines[1:]...)...)
				writeFile(t, channel, []byte(strings.Join(lines, "\n")+"\n"))
			},
		},
		{
			name: "duplicate event id",
			mutate: func(t *testing.T, channel string) {
				t.Helper()
				content := readFile(t, channel)
				lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
				writeFile(t, channel, []byte(strings.Join(append(lines, lines[0]), "\n")+"\n"))
			},
		},
		{
			name: "unsupported schema version",
			mutate: func(t *testing.T, channel string) {
				t.Helper()
				content := readFile(t, channel)
				writeFile(t, channel, bytes.Replace(content, []byte(`"schema_version":1`), []byte(`"schema_version":999`), 1))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := commandStorageFixture(t)
			t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
			tc.mutate(t, filepath.Join(dataHome, storage.SessionsDirName, "sess_command", storage.ChannelJSONLName))

			stdout, stderr, exitCode := runReleaseCLI("storage", "verify")

			if exitCode != protocol.ExitStorage {
				t.Fatalf("expected storage exit %d, got %d stdout=%q stderr=%q", protocol.ExitStorage, exitCode, stdout, stderr)
			}
			if !strings.Contains(stdout, "storage_status: replay_failure") {
				t.Fatalf("expected replay failure status, got stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestReleaseAcceptanceRegistrySnapshotFailuresAndLiveRegistryMutation(t *testing.T) {
	t.Run("missing snapshot fails closed", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
		if err := os.Remove(filepath.Join(dataHome, storage.SessionsDirName, "sess_command", registry.SnapshotFileName)); err != nil {
			t.Fatalf("remove registry snapshot: %v", err)
		}

		stdout, stderr, exitCode := runReleaseCLI("storage", "verify")

		if exitCode != protocol.ExitStorage || !strings.Contains(stdout, "storage_status: replay_failure") {
			t.Fatalf("expected snapshot replay failure exit, got %d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
	})

	t.Run("corrupt snapshot fails closed", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
		writeFile(t, filepath.Join(dataHome, storage.SessionsDirName, "sess_command", registry.SnapshotFileName), []byte("members:\n  - not-a-map\n"))

		stdout, stderr, exitCode := runReleaseCLI("storage", "verify")

		if exitCode != protocol.ExitStorage || !strings.Contains(stdout, "storage_status: replay_failure") {
			t.Fatalf("expected corrupt snapshot replay failure exit, got %d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
	})

	t.Run("live registry mutation does not reinterpret historical sessions", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
		registryPath := registry.RegistryPath(dataHome)
		content := string(readFile(t, registryPath))
		content = strings.Replace(content, "display_name: Agent One", "display_name: Agent Mutated", 1)
		writeFile(t, registryPath, []byte(content))

		stdout, stderr, exitCode := runReleaseCLI("storage", "rebuild-projection")
		if exitCode != protocol.ExitOK {
			t.Fatalf("expected rebuild exit 0, got %d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		db, err := sql.Open("sqlite", filepath.Join(dataHome, storage.ProjectionDBName))
		if err != nil {
			t.Fatalf("open projection: %v", err)
		}
		defer func() { _ = db.Close() }()
		var displayName string
		if err := db.QueryRow(`SELECT display_name FROM session_participants WHERE session_id = ? AND member = ?`, "sess_command", "agent-1").Scan(&displayName); err != nil {
			t.Fatalf("query participant display name: %v", err)
		}
		if displayName != "Agent One" {
			t.Fatalf("historical session was reinterpreted from live registry, display_name=%q", displayName)
		}
	})
}

func TestReleaseAcceptanceReplayRebuildIsSideEffectFree(t *testing.T) {
	dataHome := commandStorageFixture(t)
	t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
	channel := filepath.Join(dataHome, storage.SessionsDirName, "sess_command", storage.ChannelJSONLName)
	beforeHash := fileSHA256(t, channel)
	beforeEvents := strings.Count(string(readFile(t, channel)), "\n")

	stdout, stderr, exitCode := runReleaseCLI("storage", "rebuild-projection")

	if exitCode != protocol.ExitOK {
		t.Fatalf("expected rebuild exit 0, got %d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if after := fileSHA256(t, channel); after != beforeHash {
		t.Fatalf("rebuild mutated channel.jsonl: before=%s after=%s", beforeHash, after)
	}
	if afterEvents := strings.Count(string(readFile(t, channel)), "\n"); afterEvents != beforeEvents {
		t.Fatalf("rebuild appended events: before=%d after=%d", beforeEvents, afterEvents)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataHome, storage.ProjectionDBName))
	if err != nil {
		t.Fatalf("open projection: %v", err)
	}
	defer func() { _ = db.Close() }()
	for label, query := range map[string]string{
		"runner calls":      `SELECT count(*) FROM runner_invocations`,
		"outbound delivery": `SELECT count(*) FROM events WHERE type IN ('user_escalation_delivered','user_escalation_delivery_failed')`,
		"timer events":      `SELECT count(*) FROM events WHERE sender = 'timer' OR type LIKE '%timeout%'`,
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatalf("query %s: %v", label, err)
		}
		if count != 0 {
			t.Fatalf("rebuild produced %s side effects: %d", label, count)
		}
	}
}

func TestReleaseAcceptanceProjectionAndPathFaults(t *testing.T) {
	t.Run("missing projection verifies as recoverable then rebuilds", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)

		stdout, stderr, exitCode := runReleaseCLI("storage", "verify")
		if exitCode != protocol.ExitStorage || !strings.Contains(stdout, "storage_status: missing_projection") || !strings.Contains(stdout, "recoverable_projection_only: true") {
			t.Fatalf("expected recoverable missing projection, got %d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}

		stdout, stderr, exitCode = runReleaseCLI("storage", "rebuild-projection")
		if exitCode != protocol.ExitOK {
			t.Fatalf("expected rebuild exit 0, got %d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}

		stdout, stderr, exitCode = runReleaseCLI("storage", "verify")
		if exitCode != protocol.ExitOK || !strings.Contains(stdout, "storage valid:") {
			t.Fatalf("expected valid storage after rebuild, got %d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
	})

	t.Run("unsafe projection path fails closed", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
		target := filepath.Join(t.TempDir(), "outside.sqlite")
		writeFile(t, target, []byte("outside"))
		if err := os.Symlink(target, filepath.Join(dataHome, storage.ProjectionDBName)); err != nil {
			t.Fatalf("symlink projection: %v", err)
		}

		stdout, stderr, exitCode := runReleaseCLI("storage", "verify")

		if exitCode != protocol.ExitStorage || !strings.Contains(stdout, "storage_status: unsafe") {
			t.Fatalf("expected unsafe projection failure, got %d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
	})
}

func TestReleaseAcceptanceObservabilitySurfacesAreLocalReadOnlyAndRedacted(t *testing.T) {
	t.Run("storage and doctor summaries are read only and redacted", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
		t.Setenv("ANTHROPIC_API_KEY", "secret-value")
		before := commandTreeFingerprint(t, dataHome)

		for _, args := range [][]string{{"storage", "verify"}, {"doctor"}} {
			stdout, stderr, exitCode := runReleaseCLI(args...)
			if exitCode != protocol.ExitStorage && exitCode != protocol.ExitOK {
				t.Fatalf("%v expected local diagnostic exit, got %d stdout=%q stderr=%q", args, exitCode, stdout, stderr)
			}
			output := stdout + stderr
			if !strings.Contains(output, "storage_status:") {
				t.Fatalf("%v missing actionable storage status, got %q", args, output)
			}
			assertNoReleaseDiagnosticLeak(t, args, output)
		}

		after := commandTreeFingerprint(t, dataHome)
		if before != after {
			t.Fatalf("storage/doctor diagnostics changed files\nbefore=%s\nafter=%s", before, after)
		}
	})

	t.Run("daemon status and health are read only and redacted", func(t *testing.T) {
		dataHome := commandDaemonFixture(t)
		t.Setenv("KKACHI_AGENT_NETWORK_HOME", dataHome)
		t.Setenv("ANTHROPIC_API_KEY", "secret-value")
		app, cancel := cliWithInProcessDaemon(t)
		defer cancel()
		var startOut bytes.Buffer
		var startErr bytes.Buffer
		if exitCode := app.Run([]string{"daemon", "start"}, &startOut, &startErr); exitCode != protocol.ExitOK {
			t.Fatalf("start daemon: exit=%d stdout=%q stderr=%q", exitCode, startOut.String(), startErr.String())
		}
		before := commandTreeFingerprint(t, dataHome)

		for _, args := range [][]string{{"daemon", "status"}, {"status"}, {"daemon", "health"}} {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if exitCode := app.Run(args, &stdout, &stderr); exitCode != protocol.ExitOK {
				t.Fatalf("%v expected exit 0, got %d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
			}
			output := stdout.String() + stderr.String()
			if !strings.Contains(output, "ready") {
				t.Fatalf("%v missing actionable readiness diagnostic, got %q", args, output)
			}
			assertNoReleaseDiagnosticLeak(t, args, output)
		}

		after := commandTreeFingerprint(t, dataHome)
		if before != after {
			t.Fatalf("daemon status/health diagnostics changed files\nbefore=%s\nafter=%s", before, after)
		}
	})
}

func TestReleaseAcceptanceActiveSessionLockMismatchRecovery(t *testing.T) {
	t.Run("stale terminal metadata recovers from open durable log", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		sessionDir := filepath.Join(dataHome, storage.SessionsDirName, "sess_command")
		metadata, err := storage.LoadSessionYAML(sessionDir)
		if err != nil {
			t.Fatalf("load metadata: %v", err)
		}
		metadata.Status = storage.StatusTerminal
		metadata.State.Phase = "accepted"
		if err := storage.WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
			t.Fatalf("write stale metadata: %v", err)
		}

		active, err := storage.FindActiveSession(dataHome, registry.DefaultRuntime())

		if err != nil {
			t.Fatalf("FindActiveSession: %v", err)
		}
		if active == nil || active.SessionID != "sess_command" || active.Status != storage.StatusOpen || active.Phase != "working" {
			t.Fatalf("expected active session recovered from durable log, got %#v", active)
		}
	})

	t.Run("stale open metadata releases on terminal durable log", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		sessionDir := filepath.Join(dataHome, storage.SessionsDirName, "sess_command")
		metadata, err := storage.LoadSessionYAML(sessionDir)
		if err != nil {
			t.Fatalf("load metadata: %v", err)
		}
		terminal := storage.EventEnvelope{
			SchemaVersion: protocol.SchemaVersion,
			EventID:       "evt_release_terminal",
			CommandID:     "cmd_release_terminal",
			CorrelationID: metadata.ID,
			SessionID:     metadata.ID,
			SessionType:   metadata.SessionType,
			Phase:         "accepted",
			Type:          "work_accepted",
			From:          "agent-mod",
			To:            []string{"agent-1"},
			CreatedAt:     time.Date(2026, 6, 4, 12, 2, 0, 0, time.UTC),
			Payload:       map[string]any{"accepted_artifacts": []any{}},
		}
		if _, err := storage.AppendEvent(sessionDir, metadata, terminal); err != nil {
			t.Fatalf("append terminal event: %v", err)
		}

		active, err := storage.FindActiveSession(dataHome, registry.DefaultRuntime())

		if err != nil {
			t.Fatalf("FindActiveSession: %v", err)
		}
		if active != nil {
			t.Fatalf("terminal durable log should release active-session lock, got %#v", active)
		}
	})
}

func runReleaseCLI(args ...string) (string, string, int) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), exitCode
}

func assertNoReleaseDiagnosticLeak(t *testing.T, args []string, output string) {
	t.Helper()
	for _, forbidden := range []string{"secret-value", "ANTHROPIC_API_KEY", "Moderator", "Agent One"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("%v leaked secret or raw registry content %q in %s", args, forbidden, output)
		}
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return content
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	sum := sha256.Sum256(readFile(t, path))
	return "sha256:" + hex.EncodeToString(sum[:])
}
