package command_test

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atn-control/internal/command"
	"atn-control/internal/protocol"
	"atn-control/internal/registry"
	"atn-control/internal/storage"

	_ "modernc.org/sqlite"
)

func TestUnitCLIHelpIncludesStorageCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := command.NewCLI().Run([]string{"storage", "--help"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected storage help exit 0, got %d", exitCode)
	}
	for _, want := range []string{"storage verify", "storage rebuild-projection"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected storage help to contain %q, got:\n%s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestIntegrationStorageRebuildThenVerifyCommands(t *testing.T) {
	dataHome := commandStorageFixture(t)
	t.Setenv("ATN_HOME", dataHome)

	var rebuildOut bytes.Buffer
	var rebuildErr bytes.Buffer
	rebuildExit := command.NewCLI().Run([]string{"storage", "rebuild-projection"}, &rebuildOut, &rebuildErr)
	if rebuildExit != 0 {
		t.Fatalf("expected rebuild exit 0, got %d stdout=%q stderr=%q", rebuildExit, rebuildOut.String(), rebuildErr.String())
	}
	if !strings.Contains(rebuildOut.String(), "projection rebuilt:") {
		t.Fatalf("expected rebuild output, got %q", rebuildOut.String())
	}

	var verifyOut bytes.Buffer
	var verifyErr bytes.Buffer
	verifyExit := command.NewCLI().Run([]string{"storage", "verify"}, &verifyOut, &verifyErr)
	if verifyExit != 0 {
		t.Fatalf("expected verify exit 0, got %d stdout=%q stderr=%q", verifyExit, verifyOut.String(), verifyErr.String())
	}
	if !strings.Contains(verifyOut.String(), "storage valid:") || !strings.Contains(verifyOut.String(), "source_events: 2") {
		t.Fatalf("expected valid storage output, got %q", verifyOut.String())
	}
}

func TestIntegrationStorageVerifyFailsOnProjectionMismatchWithStorageExit(t *testing.T) {
	dataHome := commandStorageFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	if _, err := storage.RebuildProjection(dataHome, storage.ProjectionOptions{}); err != nil {
		t.Fatalf("rebuild fixture projection: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataHome, storage.ProjectionDBName))
	if err != nil {
		t.Fatalf("open projection: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM events WHERE event_id = ?`, "evt_command_extra"); err != nil {
		_ = db.Close()
		t.Fatalf("mutate projection: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close projection: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run([]string{"storage", "verify"}, &stdout, &stderr)

	if exitCode != 6 {
		t.Fatalf("expected projection mismatch exit 6, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "storage_status: projection_mismatch") {
		t.Fatalf("expected projection mismatch status, got %q", stdout.String())
	}
}

func TestIntegrationStorageVerifyFailsOnCorruptProjectionBytesWithStorageExit(t *testing.T) {
	dataHome := commandStorageFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	if err := os.WriteFile(filepath.Join(dataHome, storage.ProjectionDBName), []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt projection: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run([]string{"storage", "verify"}, &stdout, &stderr)

	if exitCode != 6 {
		t.Fatalf("expected corrupt projection exit 6, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "storage_status: projection_corrupt") {
		t.Fatalf("expected corrupt projection status, got %q", stdout.String())
	}
}

func TestIntegrationStorageCommandsMapValidationUnsafeAndReplayFailures(t *testing.T) {
	t.Run("bad args exit one", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := command.NewCLI().Run([]string{"storage", "verify", "--bad"}, &stdout, &stderr)
		if exitCode != 1 {
			t.Fatalf("expected bad args exit 1, got %d", exitCode)
		}
	})

	t.Run("unsafe data home exit three", func(t *testing.T) {
		dataHome := t.TempDir()
		if err := os.Chmod(dataHome, 0o777); err != nil {
			t.Fatalf("chmod unsafe data home: %v", err)
		}
		t.Setenv("ATN_HOME", dataHome)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := command.NewCLI().Run([]string{"storage", "verify"}, &stdout, &stderr)
		if exitCode != 3 {
			t.Fatalf("expected unsafe data home exit 3, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
		}
	})

	t.Run("corrupt log exit six", func(t *testing.T) {
		dataHome := commandStorageFixture(t)
		t.Setenv("ATN_HOME", dataHome)
		channel := filepath.Join(dataHome, storage.SessionsDirName, "sess_command", storage.ChannelJSONLName)
		content, err := os.ReadFile(channel)
		if err != nil {
			t.Fatalf("read channel: %v", err)
		}
		if err := os.WriteFile(channel, content[:len(content)-1], 0o600); err != nil {
			t.Fatalf("truncate channel: %v", err)
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := command.NewCLI().Run([]string{"storage", "rebuild-projection"}, &stdout, &stderr)
		if exitCode != 6 {
			t.Fatalf("expected corrupt log exit 6, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
		}
	})
}

func TestIntegrationDoctorReportsStorageHealthReadOnly(t *testing.T) {
	dataHome := commandStorageFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	before := projectionExists(t, dataHome)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run([]string{"doctor"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected doctor exit 0, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "storage_status: missing_projection") {
		t.Fatalf("expected doctor to report missing projection, got %q", stdout.String())
	}
	after := projectionExists(t, dataHome)
	if before != after {
		t.Fatalf("doctor modified projection presence: before=%v after=%v", before, after)
	}
}

func TestIntegrationDoctorReportsValidStorageAfterRebuild(t *testing.T) {
	dataHome := commandStorageFixture(t)
	t.Setenv("ATN_HOME", dataHome)
	rebuild, err := storage.RebuildProjection(dataHome, storage.ProjectionOptions{})
	if err != nil {
		t.Fatalf("rebuild fixture projection: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := command.NewCLI().Run([]string{"doctor"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected doctor exit 0, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "storage_status: valid") {
		t.Fatalf("expected doctor to report valid storage, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "storage_source_hash: "+rebuild.SourceHash) {
		t.Fatalf("expected doctor to report rebuilt source hash %q, got %q", rebuild.SourceHash, stdout.String())
	}
}

func commandStorageFixture(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
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
	loaded, err := registry.Load(dataHome, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	spec := storage.SessionSpec{
		ID:           "sess_command",
		SessionType:  storage.SessionTypeDelegation,
		Title:        "Command storage fixture",
		Moderator:    "agent-mod",
		Participants: []string{"agent-mod", "agent-1"},
		EventID:      "evt_command_created",
		CommandID:    "cmd_command_fixture",
	}
	metadata, _, err := storage.CreateSession(dataHome, loaded, spec, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("session dir: %v", err)
	}
	event := storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_command_extra",
		CommandID:     "cmd_command_fixture",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "working",
		Type:          "assignee_update",
		From:          "agent-1",
		To:            []string{"agent-mod"},
		CreatedAt:     time.Date(2026, 6, 4, 12, 1, 0, 0, time.UTC),
		Payload:       map[string]any{"message": "working"},
	}
	if _, err := storage.AppendEvent(sessionDir, metadata, event); err != nil {
		t.Fatalf("append event: %v", err)
	}
	return dataHome
}

func projectionExists(t *testing.T, dataHome string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dataHome, storage.ProjectionDBName))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat projection: %v", err)
	return false
}
