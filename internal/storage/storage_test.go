package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
)

func TestUnitValidateSessionID(t *testing.T) {
	valid := []string{
		"sess_20260604_145133_a",
		"sess_alpha-1",
		"sess_alpha.1_test",
	}
	for _, id := range valid {
		if err := ValidateSessionID(id); err != nil {
			t.Fatalf("expected %q to validate: %v", id, err)
		}
	}

	invalid := []string{
		"",
		"session_1",
		"sess_../escape",
		"sess_..",
		"sess_a/b",
		`sess_a\b`,
		"sess_\x00",
		"sess_한글",
		"sess_" + strings.Repeat("a", 200),
	}
	for _, id := range invalid {
		err := ValidateSessionID(id)
		assertStorageIssue(t, err, CategoryInvalidSessionID)
	}
}

func TestUnitWriteSessionYAMLAtomicPersistsMetadataShape(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	metadata.Surface = &Surface{
		Kind:          "discord_thread",
		Platform:      "discord",
		ThreadID:      "1507515847227215932",
		StartedBy:     "user",
		DeliveryOwner: "moderator_runtime",
	}
	metadata.LinkedAuthority = &LinkedAuthority{KanbanCardID: "t_xxxxx"}

	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("WriteSessionYAMLAtomic failed: %v", err)
	}
	loaded, err := LoadSessionYAML(sessionDir)
	if err != nil {
		t.Fatalf("LoadSessionYAML failed: %v", err)
	}
	if loaded.Status != StatusOpen || loaded.State.Phase != PhaseCreated {
		t.Fatalf("expected open/created metadata, got status=%q phase=%q", loaded.Status, loaded.State.Phase)
	}
	if loaded.Surface == nil || loaded.Surface.ThreadID != metadata.Surface.ThreadID {
		t.Fatalf("surface not preserved: %#v", loaded.Surface)
	}
	if loaded.LinkedAuthority == nil || loaded.LinkedAuthority.KanbanCardID != "t_xxxxx" {
		t.Fatalf("linked authority not preserved: %#v", loaded.LinkedAuthority)
	}
	info, err := os.Stat(filepath.Join(sessionDir, SessionYAMLName))
	if err != nil {
		t.Fatalf("stat session.yaml: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected session.yaml mode 0600, got %04o", info.Mode().Perm())
	}
}

func TestUnitValidateEnvelopeRejectsStatusInExistingLog(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	line := `{"schema_version":1,"event_id":"evt_bad_status","session_id":"sess_test","session_type":"delegation","phase":"created","type":"session_created","from":"kkachi-agent-networkd","to":["agent-mod"],"created_at":"2026-06-04T12:00:00Z","status":"open","payload":{"session_type":"delegation","title":"Test session","moderator":"agent-mod","participants":["agent-mod"],"limits":{}}}` + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, ChannelJSONLName), []byte(line), 0o600); err != nil {
		t.Fatalf("write channel: %v", err)
	}
	_, err := ReadLogIndex(sessionDir, metadata)
	assertStorageIssue(t, err, CategoryInvalidEnvelope)
}

func TestUnitAppendEventRejectsDuplicateAndCorruptLog(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	if err := WriteSessionYAMLAtomic(sessionDir, metadata); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	event := testEvent(metadata, "evt_000001")
	result, err := AppendEvent(sessionDir, metadata, event)
	if err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}
	if result.Cursor != "cur_000000000000_evt_000001" || result.Offset != 0 {
		t.Fatalf("unexpected cursor result: %#v", result)
	}
	_, err = AppendEvent(sessionDir, metadata, event)
	assertStorageIssue(t, err, CategoryDuplicateEventID)

	path := filepath.Join(sessionDir, ChannelJSONLName)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read channel: %v", err)
	}
	if err := os.WriteFile(path, bytesTrimFinalNewline(content), 0o600); err != nil {
		t.Fatalf("truncate channel newline: %v", err)
	}
	_, err = AppendEvent(sessionDir, metadata, testEvent(metadata, "evt_000002"))
	assertStorageIssue(t, err, CategoryLogCorrupt)
}

func TestUnitAppendEventRejectsSymlinkSessionDirFailClosed(t *testing.T) {
	targetDir := createBareSessionDir(t)
	parent := filepath.Dir(targetDir)
	linkDir := filepath.Join(parent, "linked-session")
	if err := os.Symlink(targetDir, linkDir); err != nil {
		t.Fatalf("create symlink session dir: %v", err)
	}
	metadata := testMetadata()
	result, err := AppendEvent(linkDir, metadata, testEvent(metadata, "evt_symlink"))
	if err == nil {
		t.Fatalf("expected symlink sessionDir append to fail, got result %#v", result)
	}
	assertStorageIssue(t, err, CategorySessionUnsafe)
	if _, statErr := os.Lstat(filepath.Join(targetDir, ChannelJSONLName)); !os.IsNotExist(statErr) {
		t.Fatalf("append through symlink should not create channel.jsonl, stat err=%v", statErr)
	}
}

func TestUnitAppendEventRejectsSymlinkChannelFailClosed(t *testing.T) {
	sessionDir := createBareSessionDir(t)
	metadata := testMetadata()
	targetPath := filepath.Join(t.TempDir(), "target-channel.jsonl")
	targetContent := []byte("external target must remain unchanged\n")
	if err := os.WriteFile(targetPath, targetContent, 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(targetPath, filepath.Join(sessionDir, ChannelJSONLName)); err != nil {
		t.Fatalf("create channel symlink: %v", err)
	}

	result, err := AppendEvent(sessionDir, metadata, testEvent(metadata, "evt_channel_symlink"))
	if err == nil {
		t.Fatalf("expected channel.jsonl symlink append to fail, got result %#v", result)
	}
	assertStorageIssue(t, err, CategoryLogCorrupt)
	got, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("read symlink target: %v", readErr)
	}
	if string(got) != string(targetContent) {
		t.Fatalf("append through channel symlink changed target: got %q want %q", got, targetContent)
	}
}

func TestUnitValidateRunnerCostEnvelopeRules(t *testing.T) {
	metadata := testMetadata()
	started := testEvent(metadata, "evt_runner_started")
	started.Type = "runner_invocation_started"
	started.Runner = &RunnerInfo{InvocationID: "run_started", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, Status: "started"}
	started.Cost = nil
	if err := ValidateEnvelope(metadata, &started); err != nil {
		t.Fatalf("runner_invocation_started without cost should validate: %v", err)
	}

	startedWithCost := started
	startedWithCost.EventID = "evt_runner_started_cost"
	startedWithCost.Cost = rawJSON(t, nil)
	assertStorageIssue(t, ValidateEnvelope(metadata, &startedWithCost), CategoryInvalidEnvelope)

	terminalNullCost := testEvent(metadata, "evt_runner_failed")
	terminalNullCost.Type = "runner_invocation_failed"
	terminalNullCost.Runner = &RunnerInfo{InvocationID: "run_failed", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, Status: "failed"}
	terminalNullCost.Cost = rawJSON(t, nil)
	if err := ValidateEnvelope(metadata, &terminalNullCost); err != nil {
		t.Fatalf("terminal runner event with null cost should validate: %v", err)
	}

	invalidStatus := terminalNullCost
	invalidStatus.EventID = "evt_runner_bad_status"
	invalidStatus.Runner = &RunnerInfo{InvocationID: "run_bad_status", AdapterKind: "hermes-agent", Member: "agent-1", Attempt: 1, Status: "nonzero_exit"}
	assertStorageIssue(t, ValidateEnvelope(metadata, &invalidStatus), CategoryInvalidEnvelope)

	terminalObjectCost := terminalNullCost
	terminalObjectCost.EventID = "evt_runner_done"
	terminalObjectCost.Type = "assignee_update"
	terminalObjectCost.Cost = rawJSON(t, map[string]any{"tokens_in": 1, "tokens_out": 2, "usd_estimate": 0.01, "source": "hermes-agent-stderr-parse"})
	if err := ValidateEnvelope(metadata, &terminalObjectCost); err != nil {
		t.Fatalf("terminal semantic runner event with object cost should validate: %v", err)
	}

	terminalMissingCost := terminalNullCost
	terminalMissingCost.EventID = "evt_runner_missing_cost"
	terminalMissingCost.Cost = nil
	assertStorageIssue(t, ValidateEnvelope(metadata, &terminalMissingCost), CategoryInvalidEnvelope)

	nonRunnerCost := testEvent(metadata, "evt_non_runner_cost")
	nonRunnerCost.Cost = rawJSON(t, nil)
	assertStorageIssue(t, ValidateEnvelope(metadata, &nonRunnerCost), CategoryInvalidEnvelope)
}

func TestIntegrationCreateSessionWritesSnapshotMetadataAndCreatedEvent(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	runtime := fixedRuntime()
	spec := testSessionSpec()

	metadata, result, err := CreateSession(dataHome, loaded, spec, runtime)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if result.Cursor != "cur_000000000000_evt_created_001" {
		t.Fatalf("unexpected created cursor: %#v", result)
	}
	sessionDir, err := SessionDir(dataHome, spec.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	for _, name := range []string{registry.SnapshotFileName, SessionYAMLName, ChannelJSONLName} {
		if _, err := os.Stat(filepath.Join(sessionDir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
	if metadata.RegistrySnapshot.SourceSHA256 != loaded.SourceSHA256 {
		t.Fatalf("snapshot metadata sha mismatch")
	}

	loadedMetadata, err := LoadSessionYAML(sessionDir)
	if err != nil {
		t.Fatalf("load session yaml: %v", err)
	}
	if loadedMetadata.RegistrySnapshot.SourcePath != loaded.SourcePath || loadedMetadata.RegistrySnapshot.SchemaVersion != 1 {
		t.Fatalf("registry snapshot metadata not persisted: %#v", loadedMetadata.RegistrySnapshot)
	}
	if loadedMetadata.Surface == nil || loadedMetadata.Surface.ThreadID != spec.Surface.ThreadID {
		t.Fatalf("session surface not persisted: %#v", loadedMetadata.Surface)
	}
	if loadedMetadata.LinkedAuthority == nil || loadedMetadata.LinkedAuthority.KanbanCardID != spec.LinkedAuthority.KanbanCardID {
		t.Fatalf("linked authority not persisted: %#v", loadedMetadata.LinkedAuthority)
	}

	raw := readFirstEventRaw(t, sessionDir)
	if _, ok := raw["status"]; ok {
		t.Fatalf("event envelope unexpectedly contains status: %s", mustJSON(t, raw))
	}
	payload := rawPayload(t, raw)
	if payload["surface"] == nil || payload["linked_authority"] == nil {
		t.Fatalf("session_created payload missing evidence fields: %s", mustJSON(t, payload))
	}
	if got := int(rawNumber(t, raw, "schema_version")); got != protocol.SchemaVersion {
		t.Fatalf("schema version mismatch: got %d", got)
	}
}

func TestIntegrationAppendEventDurableAndReadable(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	metadata, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}

	result, err := AppendEvent(sessionDir, metadata, testEvent(metadata, "evt_000002"))
	if err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}
	if result.Cursor != "cur_000000000001_evt_000002" || result.Offset != 1 {
		t.Fatalf("unexpected second cursor: %#v", result)
	}
	content, err := os.ReadFile(filepath.Join(sessionDir, ChannelJSONLName))
	if err != nil {
		t.Fatalf("read channel: %v", err)
	}
	if !strings.HasSuffix(string(content), "\n") {
		t.Fatalf("channel.jsonl missing final newline")
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex failed: %v", err)
	}
	if len(index.Events) != 2 || index.Events[1].EventID != "evt_000002" {
		t.Fatalf("unexpected log index: %#v", index.Events)
	}
}

func TestIntegrationCreateSessionRejectsExistingSymlinkFinalDir(t *testing.T) {
	dataHome, loaded := loadedTestRegistry(t)
	sessionsRoot := filepath.Join(dataHome, SessionsDirName)
	if err := os.Mkdir(sessionsRoot, 0o700); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}
	target := filepath.Join(dataHome, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(sessionsRoot, "sess_test")); err != nil {
		t.Fatalf("symlink final session dir: %v", err)
	}
	_, _, err := CreateSession(dataHome, loaded, testSessionSpec(), fixedRuntime())
	assertStorageIssue(t, err, CategorySessionExists)
	if _, statErr := os.Lstat(filepath.Join(target, ChannelJSONLName)); !os.IsNotExist(statErr) {
		t.Fatalf("CreateSession should not append through final symlink, stat err=%v", statErr)
	}
}

func loadedTestRegistry(t *testing.T) (string, *registry.LoadedRegistry) {
	t.Helper()
	dataHome := t.TempDir()
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	content := `schema_version: 1
members:
  agent-mod:
    display_name: Moderator
    wrapper: missing-agent-mod-wrapper
    workspace: /tmp/agent-mod
    role: moderator
    enabled: false
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
  agent-1:
    display_name: Agent One
    wrapper: missing-agent-1-wrapper
    workspace: /tmp/agent-1
    role: assignee
    enabled: false
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
`
	if err := os.WriteFile(registry.RegistryPath(dataHome), []byte(content), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	loaded, err := registry.Load(dataHome, fixedRuntime())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return dataHome, loaded
}

func fixedRuntime() registry.Runtime {
	return registry.Runtime{
		LookupEnv:   func(string) (string, bool) { return "", false },
		UserHomeDir: func() (string, error) { return "/tmp/home", nil },
		CurrentUID:  os.Getuid,
		Now:         func() time.Time { return time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC) },
	}
}

func testSessionSpec() SessionSpec {
	return SessionSpec{
		ID:           "sess_test",
		SessionType:  SessionTypeDelegation,
		Title:        "Test session",
		Moderator:    "agent-mod",
		Participants: []string{"agent-mod", "agent-1"},
		Surface: &Surface{
			Kind:          "discord_thread",
			Platform:      "discord",
			ThreadID:      "1507515847227215932",
			StartedBy:     "user",
			DeliveryOwner: "moderator_runtime",
		},
		LinkedAuthority: &LinkedAuthority{KanbanCardID: "t_xxxxx", VaultDecisionNote: "decision-note"},
		TurnMode:        "role_order",
		EventID:         "evt_created_001",
		CommandID:       "cmd_create_001",
	}
}

func createBareSessionDir(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	sessionDir := filepath.Join(dataHome, SessionsDirName, "sess_test")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	return sessionDir
}

func testMetadata() *SessionMetadata {
	at := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	return &SessionMetadata{
		ID:           "sess_test",
		SessionType:  SessionTypeDelegation,
		Status:       StatusOpen,
		Title:        "Test session",
		Moderator:    "agent-mod",
		Participants: []string{"agent-mod", "agent-1"},
		CreatedAt:    at,
		State:        SessionState{Phase: PhaseCreated},
		RegistrySnapshot: registry.SnapshotMetadata{
			SourcePath:    "/tmp/registry.yaml",
			SourceSHA256:  "sha256:test",
			LoadedAt:      at,
			LoadedByUID:   os.Getuid(),
			SchemaVersion: 1,
		},
	}
}

func testEvent(metadata *SessionMetadata, eventID string) EventEnvelope {
	return EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventID,
		CommandID:     "cmd_" + eventID,
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
}

func readFirstEventRaw(t *testing.T, sessionDir string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(sessionDir, ChannelJSONLName))
	if err != nil {
		t.Fatalf("read channel: %v", err)
	}
	line := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")[0]
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		t.Fatalf("unmarshal first event: %v", err)
	}
	return raw
}

func rawPayload(t *testing.T, raw map[string]any) map[string]any {
	t.Helper()
	payload, ok := raw["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is not object: %s", mustJSON(t, raw))
	}
	return payload
}

func rawNumber(t *testing.T, raw map[string]any, key string) float64 {
	t.Helper()
	value, ok := raw[key].(float64)
	if !ok {
		t.Fatalf("%s is not number: %s", key, mustJSON(t, raw))
	}
	return value
}

func bytesTrimFinalNewline(content []byte) []byte {
	if len(content) == 0 || content[len(content)-1] != '\n' {
		return content
	}
	return content[:len(content)-1]
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(content)
}

func assertStorageIssue(t *testing.T, err error, category string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected issue %s, got nil", category)
	}
	for _, issue := range Issues(err) {
		if issue.Category == category {
			return
		}
	}
	t.Fatalf("expected issue %s, got %v", category, err)
}
