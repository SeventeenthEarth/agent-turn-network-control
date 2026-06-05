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
	"kkachi-agent-network-control/internal/storage"
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

func TestUnitDaemonDAEMN002VersionFeaturesAreImplemented(t *testing.T) {
	server := daemon.NewServer("/tmp/unused", registry.DefaultRuntime())
	response := server.Handle(protocol.NewRequest("features", protocol.FeatureVersionRead, nil))
	if !response.OK {
		t.Fatalf("version.read should succeed, got %+v", response)
	}
	featuresJSON := mustJSON(t, response.Result)
	for _, want := range []string{"version.read", "stream.replay", "stream.follow", "stream.ack", "stream.status", "delivery_evidence", "conformance.fixtures"} {
		if !strings.Contains(featuresJSON, want) {
			t.Fatalf("version features missing %q: %s", want, featuresJSON)
		}
	}
	if strings.Contains(featuresJSON, "stream.tail") {
		t.Fatalf("version features must not advertise stream.tail: %s", featuresJSON)
	}
}

func TestIntegrationDaemonStreamAckStatusAndDeliveryEvidence(t *testing.T) {
	dataHome := daemonDataHome(t)
	metadata := daemonSessionFixture(t, dataHome)
	server := daemon.NewServer(dataHome, daemonFixedRuntime())

	replay := server.Handle(protocol.NewRequest("replay", protocol.FeatureStreamReplay, map[string]any{"session_id": metadata.ID, "member": "agent-1", "from_start": true, "follow": true, "follow_timeout_ms": 1}))
	if !replay.OK {
		t.Fatalf("stream.replay failed: %+v", replay)
	}
	frames, ok := replay.Result["frames"].([]storage.StreamFrame)
	if !ok || len(frames) == 0 || !frames[0].IsReplay {
		t.Fatalf("unexpected replay frames: %#v", replay.Result["frames"])
	}

	ack := server.Handle(protocol.NewRequest("ack", protocol.FeatureStreamAck, map[string]any{"session_id": metadata.ID, "member": "agent-1", "cursor": "cur_000000000000_evt_created_001", "command_id": "cmd_ack_daemon"}))
	if !ack.OK {
		t.Fatalf("stream.ack failed: %+v", ack)
	}
	again := server.Handle(protocol.NewRequest("ack2", protocol.FeatureStreamAck, map[string]any{"session_id": metadata.ID, "member": "agent-1", "cursor": "cur_000000000000_evt_created_001", "command_id": "cmd_ack_daemon"}))
	if !again.OK || again.Result["deduplicated"] != true {
		t.Fatalf("duplicate ack should dedupe: %+v", again)
	}
	conflict := server.Handle(protocol.NewRequest("ack3", protocol.FeatureStreamAck, map[string]any{"session_id": metadata.ID, "member": "agent-1", "cursor": "cur_000000000001_evt_user_escalation_requested_01", "command_id": "cmd_ack_daemon"}))
	if conflict.OK || conflict.Error == nil || conflict.Error.Code != protocol.ErrorValidation {
		t.Fatalf("mismatched duplicate ack should fail closed: %+v", conflict)
	}
	unknown := server.Handle(protocol.NewRequest("ack4", protocol.FeatureStreamAck, map[string]any{"session_id": metadata.ID, "member": "ghost", "cursor": "cur_000000000000_evt_created_001"}))
	if unknown.OK || unknown.Error == nil {
		t.Fatalf("unknown member ack should fail closed: %+v", unknown)
	}

	status := server.Handle(protocol.NewRequest("status", protocol.FeatureStreamStatus, map[string]any{"session_id": metadata.ID}))
	if !status.OK || status.Result["session_id"] != metadata.ID {
		t.Fatalf("stream.status failed: %+v", status)
	}

	delivered := server.Handle(protocol.NewRequest("delivered", "delegate.escalation_delivered", map[string]any{"session_id": metadata.ID, "escalation": "evt_user_escalation_requested_01", "delivery_target": "origin", "platform": "hermes", "message_ref": "msg_1", "command_id": "cmd_delivered"}))
	if !delivered.OK {
		t.Fatalf("delivery evidence failed: %+v", delivered)
	}
	failed := server.Handle(protocol.NewRequest("failed", "delegate.escalation_delivery_failed", map[string]any{"session_id": metadata.ID, "escalation": "evt_user_escalation_requested_01", "target": "telegram", "reason": "unreachable", "will_retry_targets": []any{"origin"}, "command_id": "cmd_delivery_failed"}))
	if !failed.OK {
		t.Fatalf("delivery failure evidence failed: %+v", failed)
	}
	bad := server.Handle(protocol.NewRequest("bad", "delegate.escalation_delivered", map[string]any{"session_id": metadata.ID, "escalation": "evt_missing", "delivery_target": "origin", "platform": "hermes"}))
	if bad.OK || bad.Error == nil {
		t.Fatalf("invalid escalation reference should fail closed: %+v", bad)
	}
}

func TestIntegrationDaemonStreamFollowEmitsReplayThenLive(t *testing.T) {
	dataHome := daemonDataHome(t)
	metadata := daemonSessionFixture(t, dataHome)
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.StreamFollowAfterReplay = func() error {
		_, err := storage.AppendEvent(sessionDir, metadata, storage.EventEnvelope{
			SchemaVersion: protocol.SchemaVersion,
			EventID:       "evt_daemon_live_follow",
			CommandID:     "cmd_daemon_live_follow",
			CorrelationID: metadata.ID,
			SessionID:     metadata.ID,
			SessionType:   metadata.SessionType,
			Phase:         "working",
			Type:          "assignee_update",
			From:          "agent-1",
			To:            []string{"agent-mod"},
			CreatedAt:     daemonFixedRuntime().Now().Add(time.Second),
			Payload:       map[string]any{"message": "live follow"},
		})
		return err
	}

	replay := server.Handle(protocol.NewRequest("replay-follow-live", protocol.FeatureStreamReplay, map[string]any{
		"session_id":        metadata.ID,
		"member":            "agent-1",
		"from_start":        true,
		"follow":            true,
		"follow_timeout_ms": 500,
		"follow_poll_ms":    5,
	}))
	if !replay.OK {
		t.Fatalf("stream.replay follow failed: %+v", replay)
	}
	frames, ok := replay.Result["frames"].([]storage.StreamFrame)
	if !ok {
		t.Fatalf("unexpected frame type: %#v", replay.Result["frames"])
	}
	if len(frames) != 3 {
		t.Fatalf("expected two replay frames and one live frame, got %#v", frames)
	}
	if !frames[0].IsReplay || !frames[1].IsReplay {
		t.Fatalf("existing durable frames must be replay first: %#v", frames)
	}
	if frames[2].IsReplay || frames[2].Event.EventID != "evt_daemon_live_follow" {
		t.Fatalf("appended durable event must be emitted as live, got %#v", frames[2])
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

func daemonSessionFixture(t *testing.T, dataHome string) *storage.SessionMetadata {
	t.Helper()
	loaded, err := registry.Load(dataHome, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	metadata, _, err := storage.CreateSession(dataHome, loaded, storage.SessionSpec{
		ID:           "sess_daemon",
		SessionType:  storage.SessionTypeDelegation,
		Title:        "Daemon stream fixture",
		Moderator:    "agent-mod",
		Participants: []string{"agent-mod", "agent-1"},
		EventID:      "evt_created_001",
		CommandID:    "cmd_create_daemon",
	}, daemonFixedRuntime())
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
		CommandID:     "cmd_escalate_fixture",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "waiting_user",
		Type:          "user_escalation_requested",
		From:          "agent-1",
		To:            []string{"agent-mod"},
		CreatedAt:     daemonFixedRuntime().Now(),
		Payload:       map[string]any{"question": "Need input", "urgency": "blocked"},
	}
	if _, err := storage.AppendEvent(sessionDir, metadata, escalation); err != nil {
		t.Fatalf("append escalation: %v", err)
	}
	return metadata
}

func daemonFixedRuntime() registry.Runtime {
	return registry.Runtime{
		LookupEnv:   func(string) (string, bool) { return "", false },
		UserHomeDir: func() (string, error) { return "/tmp/home", nil },
		CurrentUID:  os.Getuid,
		Now:         func() time.Time { return time.Date(2026, 6, 5, 1, 0, 0, 0, time.UTC) },
	}
}
