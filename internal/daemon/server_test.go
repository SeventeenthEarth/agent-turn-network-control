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
	if _, exists := status.Result["protocol_version"]; exists {
		t.Fatalf("operator status must not grow compatibility fields: %+v", status.Result)
	}

	statusRead, err := transport.RoundTrip(dataHome, protocol.NewRequest("status-read", protocol.FeatureStatusRead, nil), time.Second)
	if err != nil {
		t.Fatalf("status.read round trip: %v", err)
	}
	statusReadJSON := mustJSON(t, statusRead.Result)
	for _, want := range []string{protocol.ProtocolVersion, "daemn-002-local", "min_plugin_protocol_version", "feature_groups", "capability_state", protocol.FeatureDiagnosticsRead, "schema_version", "fixture_manifest"} {
		if !strings.Contains(statusReadJSON, want) {
			t.Fatalf("status.read missing %q: %s", want, statusReadJSON)
		}
	}
	assertCompatibilityReadEvidence(t, "status.read", statusRead.Result)

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
	if _, exists := health.Result["protocol_version"]; exists {
		t.Fatalf("operator health must not grow compatibility fields: %+v", health.Result)
	}

	diagnosticsRead, err := transport.RoundTrip(dataHome, protocol.NewRequest("diagnostics-read", protocol.FeatureDiagnosticsRead, nil), time.Second)
	if err != nil {
		t.Fatalf("diagnostics.read round trip: %v", err)
	}
	diagnosticsJSON := mustJSON(t, diagnosticsRead.Result)
	for _, want := range []string{protocol.ProtocolVersion, "daemn-002-local", "min_plugin_protocol_version", "feature_groups", "capability_state", `"categories"`, `"readiness"`, "schema_version", "fixture_manifest"} {
		if !strings.Contains(diagnosticsJSON, want) {
			t.Fatalf("diagnostics.read missing %q: %s", want, diagnosticsJSON)
		}
	}
	assertCompatibilityReadEvidence(t, "diagnostics.read", diagnosticsRead.Result)

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

func TestUnitDaemonSessionNewRejectsMalformedPresentLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		params  map[string]any
	}{
		{
			name:    "council.new string limits",
			command: "council.new",
			params: map[string]any{
				"session_id": "sess_bad_council_limits_string",
				"moderator":  "agent-mod",
				"members":    []any{"agent-1"},
				"title":      "bad council limits",
				"limits":     "quality_required",
			},
		},
		{
			name:    "delegate.new array limits",
			command: "delegate.new",
			params: map[string]any{
				"session_id": "sess_bad_delegate_limits_array",
				"moderator":  "agent-mod",
				"assignee":   "agent-1",
				"title":      "bad delegate limits",
				"task":       "prove malformed limits fail closed",
				"limits":     []any{"not an object"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := daemonDataHome(t)
			before := treeFingerprint(t, dataHome)
			server := daemon.NewServer(dataHome, daemonFixedRuntime())
			response := server.Handle(protocol.NewRequest("bad-limits", tc.command, tc.params))
			after := treeFingerprint(t, dataHome)

			if response.OK || response.Error == nil {
				t.Fatalf("malformed present limits must fail closed: %+v", response)
			}
			if response.Error.Code == "" || !strings.Contains(response.Error.Message, "limits") {
				t.Fatalf("limits validation error should name limits, got %+v", response.Error)
			}
			if before != after {
				t.Fatalf("malformed limits request wrote files\nbefore=%s\nafter=%s", before, after)
			}
		})
	}
}

func TestUnitDaemonDAEMN002VersionFeaturesAreImplemented(t *testing.T) {
	server := daemon.NewServer("/tmp/unused", registry.DefaultRuntime())
	response := server.Handle(protocol.NewRequest("features", protocol.FeatureVersionRead, nil))
	if !response.OK {
		t.Fatalf("version.read should succeed, got %+v", response)
	}
	featuresJSON := mustJSON(t, response.Result)
	for _, want := range []string{"version.read", "status.read", "diagnostics.read", "stream.replay", "stream.follow", "stream.ack", "stream.status", "delivery_evidence", "conformance.fixtures"} {
		if !strings.Contains(featuresJSON, want) {
			t.Fatalf("version features missing %q: %s", want, featuresJSON)
		}
	}
	if strings.Contains(featuresJSON, "stream.tail") {
		t.Fatalf("version features must not advertise stream.tail: %s", featuresJSON)
	}
}

func TestUnitDaemonCompatibilityReadsExposeProtocolEvidenceWithoutChangingOperatorShapes(t *testing.T) {
	dataHome := daemonDataHome(t)
	server := daemon.NewServer(dataHome, daemonFixedRuntime())

	status := server.Handle(protocol.NewRequest("status", "status", nil))
	if !status.OK {
		t.Fatalf("status should succeed: %+v", status)
	}
	if _, exists := status.Result["protocol_version"]; exists {
		t.Fatalf("operator status must not grow compatibility fields: %+v", status.Result)
	}

	statusRead := server.Handle(protocol.NewRequest("status-read", protocol.FeatureStatusRead, nil))
	if !statusRead.OK {
		t.Fatalf("status.read should succeed: %+v", statusRead)
	}
	statusReadJSON := mustJSON(t, statusRead.Result)
	for _, want := range []string{protocol.ProtocolVersion, "daemn-002-local", "min_plugin_protocol_version", "feature_groups", "capability_state", "operational_readiness", protocol.FeatureDiagnosticsRead, "schema_version", "fixture_manifest"} {
		if !strings.Contains(statusReadJSON, want) {
			t.Fatalf("status.read missing %q: %s", want, statusReadJSON)
		}
	}
	assertCompatibilityReadEvidence(t, "status.read", statusRead.Result)

	health := server.Handle(protocol.NewRequest("health", "health", nil))
	if !health.OK {
		t.Fatalf("health should succeed: %+v", health)
	}
	if _, exists := health.Result["protocol_version"]; exists {
		t.Fatalf("operator health must not grow compatibility fields: %+v", health.Result)
	}

	diagnosticsRead := server.Handle(protocol.NewRequest("diagnostics-read", protocol.FeatureDiagnosticsRead, nil))
	if !diagnosticsRead.OK {
		t.Fatalf("diagnostics.read should succeed: %+v", diagnosticsRead)
	}
	diagnosticsJSON := mustJSON(t, diagnosticsRead.Result)
	for _, want := range []string{protocol.ProtocolVersion, "daemn-002-local", "min_plugin_protocol_version", "feature_groups", "capability_state", `"categories"`, `"readiness"`, "schema_version", "fixture_manifest"} {
		if !strings.Contains(diagnosticsJSON, want) {
			t.Fatalf("diagnostics.read missing %q: %s", want, diagnosticsJSON)
		}
	}
	assertCompatibilityReadEvidence(t, "diagnostics.read", diagnosticsRead.Result)
}

func assertCompatibilityReadEvidence(t *testing.T, command string, result map[string]any) {
	t.Helper()
	features := protocol.NewVersionFeatures()
	schemaVersion, ok := numericInt(result["schema_version"])
	if !ok || schemaVersion != features.SchemaVersion {
		t.Fatalf("%s schema_version = %v, want %d", command, result["schema_version"], features.SchemaVersion)
	}
	if result["fixture_manifest"] != features.FixtureManifest {
		t.Fatalf("%s fixture_manifest = %v, want %q", command, result["fixture_manifest"], features.FixtureManifest)
	}
	if result["live_readiness"] != features.LiveReadiness {
		t.Fatalf("%s live_readiness = %v, want %v", command, result["live_readiness"], features.LiveReadiness)
	}
}

func numericInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		if typed == float64(int(typed)) {
			return int(typed), true
		}
	}
	return 0, false
}

func TestUnitDaemonCompatibilityReadsDoNotMutateDataHome(t *testing.T) {
	dataHome := daemonDataHome(t)
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	before := treeFingerprint(t, dataHome)

	for _, command := range []string{protocol.FeatureStatusRead, protocol.FeatureDiagnosticsRead} {
		response := server.Handle(protocol.NewRequest(command, command, nil))
		if !response.OK {
			t.Fatalf("%s should succeed: %+v", command, response)
		}
	}

	after := treeFingerprint(t, dataHome)
	if before != after {
		t.Fatalf("compatibility read commands wrote files\nbefore=%s\nafter=%s", before, after)
	}
}

func TestUnitDaemonTranscriptExportAndTailAreReadOnlyCommandPaths(t *testing.T) {
	dataHome := daemonDataHome(t)
	metadata := daemonSessionFixture(t, dataHome)
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	beforeLog := readFileForDaemonTest(t, filepath.Join(sessionDir, storage.ChannelJSONLName))
	server := daemon.NewServer(dataHome, daemonFixedRuntime())

	transcript := server.Handle(protocol.NewRequest("transcript", "transcript.render", map[string]any{"session_id": metadata.ID, "format": "md"}))
	if !transcript.OK || !strings.Contains(fmt.Sprint(transcript.Result["content"]), "Daemon stream fixture") {
		t.Fatalf("unexpected transcript response: %+v", transcript)
	}
	jsonl := server.Handle(protocol.NewRequest("transcript-jsonl", "transcript.render", map[string]any{"session_id": metadata.ID, "format": "jsonl"}))
	if !jsonl.OK || !strings.Contains(fmt.Sprint(jsonl.Result["content"]), `"event_id":"evt_created_001"`) {
		t.Fatalf("unexpected transcript jsonl response: %+v", jsonl)
	}
	tail := server.Handle(protocol.NewRequest("tail", "tail.session", map[string]any{"session_id": metadata.ID, "limit": 1}))
	if !tail.OK {
		t.Fatalf("unexpected tail response: %+v", tail)
	}
	export := server.Handle(protocol.NewRequest("export", "export.bundle", map[string]any{"session_id": metadata.ID}))
	if !export.OK || !strings.Contains(fmt.Sprint(export.Result["bundle_dir"]), filepath.Join("exports", metadata.ID+"-bundle")) {
		t.Fatalf("unexpected export response: %+v", export)
	}
	afterLog := readFileForDaemonTest(t, filepath.Join(sessionDir, storage.ChannelJSONLName))
	if beforeLog != afterLog {
		t.Fatalf("read-only transcript/export/tail commands changed channel.jsonl")
	}
	if strings.Contains(mustJSON(t, protocol.NewVersionFeatures()), "stream.tail") {
		t.Fatalf("version features must not advertise stream.tail")
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

func TestIntegrationDaemonDelegationBlockResumeLimitsEscalationAndCancel(t *testing.T) {
	dataHome := daemonDataHome(t)
	server := daemon.NewServer(dataHome, daemonFixedRuntime())

	created := server.Handle(protocol.NewRequest("deleg-new", "delegate.new", map[string]any{
		"session_id":          "sess_daemon_deleg",
		"moderator":           "agent-mod",
		"assignee":            "agent-1",
		"title":               "Daemon DELEG",
		"task":                "prove daemon path",
		"event_id":            "evt_daemon_deleg_created",
		"assignment_event_id": "evt_daemon_deleg_assigned",
		"command_id":          "cmd_daemon_deleg_new",
	}))
	if !created.OK {
		t.Fatalf("delegate.new failed: %+v", created)
	}
	ack := server.Handle(protocol.NewRequest("deleg-ack", "delegate.ack", map[string]any{
		"session_id": "sess_daemon_deleg",
		"actor":      "agent-1",
		"command_id": "cmd_daemon_deleg_ack",
		"payload":    map[string]any{"understanding": "ok"},
	}))
	if !ack.OK {
		t.Fatalf("delegate.ack failed: %+v", ack)
	}
	badBlock := server.Handle(protocol.NewRequest("bad-block", "block", map[string]any{"session_id": "sess_daemon_deleg", "actor": "agent-1", "category": "external_dependency", "reason": "not moderator"}))
	if badBlock.OK || badBlock.Error == nil {
		t.Fatalf("non-moderator block must fail closed: %+v", badBlock)
	}
	blocked := server.Handle(protocol.NewRequest("block", "block", map[string]any{"session_id": "sess_daemon_deleg", "category": "external_dependency", "reason": "dependency down", "command_id": "cmd_daemon_block"}))
	if !blocked.OK || blocked.Result["event_id"] == "" {
		t.Fatalf("block failed: %+v", blocked)
	}
	resumed := server.Handle(protocol.NewRequest("resume", "resume", map[string]any{"session_id": "sess_daemon_deleg", "blocked_event_id": blocked.Result["event_id"], "reason": "dependency recovered", "command_id": "cmd_daemon_resume"}))
	if !resumed.OK {
		t.Fatalf("resume failed: %+v", resumed)
	}
	cancelled := server.Handle(protocol.NewRequest("cancel", "cancel", map[string]any{"session_id": "sess_daemon_deleg", "reason": "done testing", "command_id": "cmd_daemon_cancel"}))
	if !cancelled.OK {
		t.Fatalf("cancel failed: %+v", cancelled)
	}
	sessionDir, _ := storage.SessionDir(dataHome, "sess_daemon_deleg")
	metadata, _ := storage.LoadSessionYAML(sessionDir)
	index, _ := storage.ReadLogIndex(sessionDir, metadata)
	if got := index.Events[len(index.Events)-1].Type; got != "session_cancelled" {
		t.Fatalf("common cancel must append session_cancelled, got %q", got)
	}

	budgetCreated := server.Handle(protocol.NewRequest("budget-new", "delegate.new", map[string]any{
		"session_id":          "sess_daemon_budget",
		"moderator":           "agent-mod",
		"assignee":            "agent-1",
		"title":               "Budget DELEG",
		"task":                "prove limits path",
		"event_id":            "evt_daemon_budget_created",
		"assignment_event_id": "evt_daemon_budget_assigned",
		"command_id":          "cmd_daemon_budget_new",
	}))
	if !budgetCreated.OK {
		t.Fatalf("budget delegate.new failed: %+v", budgetCreated)
	}
	budgetDir, _ := storage.SessionDir(dataHome, "sess_daemon_budget")
	budgetMeta, _ := storage.LoadSessionYAML(budgetDir)
	if _, err := storage.AppendEvent(budgetDir, budgetMeta, storage.EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "evt_daemon_budget_block",
		CommandID:     "cmd_daemon_budget_block",
		CorrelationID: budgetMeta.ID,
		SessionID:     budgetMeta.ID,
		SessionType:   budgetMeta.SessionType,
		Phase:         "blocked",
		Type:          "session_budget_exceeded",
		From:          "kkachi-agent-networkd",
		To:            []string{"agent-mod"},
		CreatedAt:     daemonFixedRuntime().Now().Add(time.Second),
		Payload:       map[string]any{"limit_kind": "max_runner_calls", "observed": 1, "limit": 1, "prior_phase": "working", "resume_phase": "working", "action": "session_blocked"},
	}); err != nil {
		t.Fatalf("append budget block: %v", err)
	}
	badResume := server.Handle(protocol.NewRequest("bad-resume", "resume", map[string]any{"session_id": budgetMeta.ID, "blocked_event_id": "evt_daemon_budget_block", "reason": "manual resume"}))
	if badResume.OK || badResume.Error == nil {
		t.Fatalf("manual resume of budget block must fail closed: %+v", badResume)
	}
	badExtend := server.Handle(protocol.NewRequest("bad-extend", "limits.extend", map[string]any{"session_id": budgetMeta.ID, "blocked_event_id": "evt_daemon_budget_block", "changes": map[string]any{"max_runner_calls": 2}}))
	if badExtend.OK || badExtend.Error == nil {
		t.Fatalf("limits.extend without user authorization must fail closed: %+v", badExtend)
	}
	extended := server.Handle(protocol.NewRequest("extend", "limits.extend", map[string]any{"session_id": budgetMeta.ID, "blocked_event_id": "evt_daemon_budget_block", "authorized_by": "user", "changes": map[string]any{"max_runner_calls": 2}, "command_id": "cmd_daemon_limits_extend"}))
	if !extended.OK {
		t.Fatalf("limits.extend failed: %+v", extended)
	}
	status := server.Handle(protocol.NewRequest("status", "status.session", map[string]any{"session_id": budgetMeta.ID}))
	if !status.OK || status.Result["phase"] != storage.Phase("working") || status.Result["status"] != storage.StatusOpen {
		t.Fatalf("status.session should show resumed working state: %+v", status)
	}
	escalated := server.Handle(protocol.NewRequest("escalate-low", "delegate.escalate", map[string]any{"session_id": budgetMeta.ID, "actor": "agent-1", "command_id": "cmd_daemon_escalate_low", "payload": map[string]any{"question": "batch me", "urgency": "low"}}))
	if !escalated.OK {
		t.Fatalf("low urgency escalation should batch locally: %+v", escalated)
	}
	batches := server.Handle(protocol.NewRequest("batches", "delegate.escalation_batches", map[string]any{"session_id": budgetMeta.ID}))
	if !batches.OK || !strings.Contains(mustJSON(t, batches.Result), "escbatch") {
		t.Fatalf("escalation batches missing pending batch: %+v", batches)
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

func readFileForDaemonTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
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
