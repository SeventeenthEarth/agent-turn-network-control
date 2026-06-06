package protocol

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var requiredConformanceSchemas = []string{
	"schemas/command-envelope.schema.json",
	"schemas/event-envelope.schema.json",
	"schemas/structured-error.schema.json",
	"schemas/stream-frame.schema.json",
	"schemas/version-features.schema.json",
	"schemas/delivery-evidence-command.schema.json",
}

var requiredConformanceFixtures = []string{
	"fixtures/command/version-read-request.json",
	"fixtures/command/stream-replay-request.json",
	"fixtures/command/stream-ack-request.json",
	"fixtures/command/stream-ack-response.json",
	"fixtures/command/cancel-request.json",
	"fixtures/command/cancel-response.json",
	"fixtures/command/delegate-escalation-delivered-request.json",
	"fixtures/command/delegate-escalation-delivered-response.json",
	"fixtures/command/delegate-escalation-delivery-failed-request.json",
	"fixtures/command/delegate-escalation-delivery-failed-response.json",
	"fixtures/event/session-created-delegation.json",
	"fixtures/event/session-cancelled.json",
	"fixtures/event/stream-cursor-acknowledged.json",
	"fixtures/event/user-escalation-delivered.json",
	"fixtures/event/user-escalation-delivery-failed.json",
	"fixtures/event/runner-invocation-started.json",
	"fixtures/event/runner-invocation-failed-null-cost.json",
	"fixtures/event/runner-terminal-semantic-with-cost.json",
	"fixtures/event/runner-result-discarded.json",
	"fixtures/error/unsupported-feature.json",
	"fixtures/error/active-session-lock.json",
	"fixtures/error/cancel-unauthorized.json",
	"fixtures/error/cursor-gap.json",
	"fixtures/error/unauthorized-member.json",
	"fixtures/error/invalid-delivery-escalation-reference.json",
	"fixtures/error/unauthorized-delivery-reporter.json",
	"fixtures/stream/from-start.ndjson",
	"fixtures/stream/since-cursor.ndjson",
	"fixtures/stream/follow-replay-then-live.ndjson",
	"fixtures/version/version-features.json",
}

var requiredDelegationReviewFixtures = []string{
	"fixtures/command/delegate-new-request.json",
	"fixtures/command/delegate-new-response.json",
	"fixtures/command/delegate-submit-request.json",
	"fixtures/command/delegate-submit-response.json",
	"fixtures/command/delegate-submit-duplicate-request.json",
	"fixtures/command/delegate-submit-duplicate-response.json",
	"fixtures/command/delegate-review-request.json",
	"fixtures/command/delegate-review-response.json",
	"fixtures/command/delegate-review-submit-request.json",
	"fixtures/command/delegate-review-submit-response.json",
	"fixtures/command/delegate-accept-request.json",
	"fixtures/command/delegate-accept-response.json",
	"fixtures/event/task-assigned-delegation.json",
	"fixtures/event/work-submitted.json",
	"fixtures/event/review-requested.json",
	"fixtures/event/review-submitted.json",
	"fixtures/event/work-accepted.json",
	"fixtures/error/delegate-unauthorized-actor.json",
	"fixtures/error/delegate-review-wrong-phase.json",
	"fixtures/error/delegate-review-submit-invalid-verdict.json",
}

type conformanceManifest struct {
	ManifestVersion       int      `json:"manifest_version"`
	ProtocolVersion       string   `json:"protocol_version"`
	Stability             string   `json:"stability"`
	Schemas               []string `json:"schemas"`
	Fixtures              []string `json:"fixtures"`
	RequiredFeatureGroups []string `json:"required_feature_groups"`
}

func TestUnitConformanceManifestListsCanonicalDAEMN002Artifacts(t *testing.T) {
	manifest := readConformanceManifest(t)
	if manifest.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version mismatch: got %q want %q", manifest.ProtocolVersion, ProtocolVersion)
	}
	if strings.Contains(manifest.Stability, "scaffold") {
		t.Fatalf("manifest stability must not claim scaffold after fixtures land: %q", manifest.Stability)
	}
	assertExactStrings(t, "required feature groups", manifest.RequiredFeatureGroups, RequiredFeatureGroups)
	assertContainsAll(t, "schemas", manifest.Schemas, requiredConformanceSchemas)
	assertContainsAll(t, "fixtures", manifest.Fixtures, requiredConformanceFixtures)
	assertContainsAll(t, "DELEG-002 fixtures", manifest.Fixtures, requiredDelegationReviewFixtures)
	if len(manifest.Fixtures) == 0 {
		t.Fatalf("manifest fixtures must not be empty")
	}
	for _, value := range append(append([]string{}, manifest.Fixtures...), manifest.RequiredFeatureGroups...) {
		if usesStreamTail(value) {
			t.Fatalf("manifest uses non-canonical stream-tail vocabulary: %s", value)
		}
	}
}

func TestUnitConformanceManifestPathsExistAndParse(t *testing.T) {
	manifest := readConformanceManifest(t)
	for _, schema := range manifest.Schemas {
		readJSONFixture[map[string]any](t, schema)
	}
	for _, fixture := range manifest.Fixtures {
		if strings.HasSuffix(fixture, ".ndjson") {
			frames := readNDJSONFixture(t, fixture)
			if len(frames) == 0 {
				t.Fatalf("%s must contain at least one stream frame", fixture)
			}
			for _, frame := range frames {
				if frame.Cursor == "" || frame.Event.Type == "" {
					t.Fatalf("%s contains incomplete stream frame: %+v", fixture, frame)
				}
			}
			continue
		}
		readJSONFixture[map[string]any](t, fixture)
	}
}

func TestUnitConformanceFixturesUseCanonicalCommandsAndDeliveryEvidence(t *testing.T) {
	versionRequest := readJSONFixture[CommandRequest](t, "fixtures/command/version-read-request.json")
	if versionRequest.Command != FeatureVersionRead {
		t.Fatalf("version fixture command = %q, want %q", versionRequest.Command, FeatureVersionRead)
	}
	streamRequest := readJSONFixture[CommandRequest](t, "fixtures/command/stream-replay-request.json")
	if streamRequest.Command != FeatureStreamReplay {
		t.Fatalf("stream fixture command = %q, want %q", streamRequest.Command, FeatureStreamReplay)
	}
	if usesStreamTail(string(readConformanceBytes(t, "fixtures/command/stream-replay-request.json"))) {
		t.Fatalf("stream replay fixture must not mention stream.tail")
	}
	cancelRequest := readJSONFixture[CommandRequest](t, "fixtures/command/cancel-request.json")
	if cancelRequest.Command != "cancel" {
		t.Fatalf("cancel fixture command = %q, want cancel", cancelRequest.Command)
	}
	for _, key := range []string{"session_id", "actor", "reason", "cause", "command_id"} {
		if _, ok := cancelRequest.Params[key]; !ok {
			t.Fatalf("cancel fixture missing param %q in %+v", key, cancelRequest.Params)
		}
	}
	cancelResponse := readJSONFixture[CommandResponse](t, "fixtures/command/cancel-response.json")
	if !cancelResponse.OK || cancelResponse.Result["event_id"] != "evt_session_cancelled_cmd_cancel_conformance" {
		t.Fatalf("cancel response fixture should return session_cancelled event, got %+v", cancelResponse)
	}
	cancelEvent := readJSONFixture[eventEnvelopeFixture](t, "fixtures/event/session-cancelled.json")
	if cancelEvent.Type != "session_cancelled" || cancelEvent.Phase != "cancelled" || cancelEvent.From != "agent-mod" {
		t.Fatalf("session_cancelled fixture has wrong shape: %+v", cancelEvent)
	}
	assertExactStrings(t, "session_cancelled to", cancelEvent.To, []string{"agent-1", "agent-mod"})
	for _, key := range []string{"reason", "cause"} {
		if _, ok := cancelEvent.Payload[key]; !ok {
			t.Fatalf("session_cancelled fixture missing payload %q in %+v", key, cancelEvent.Payload)
		}
	}

	for _, tc := range []struct {
		path    string
		command string
		want    []string
	}{
		{
			path:    "fixtures/command/delegate-escalation-delivered-request.json",
			command: "delegate.escalation_delivered",
			want:    []string{"session_id", "escalation", "delivery_target", "platform", "command_id"},
		},
		{
			path:    "fixtures/command/delegate-escalation-delivery-failed-request.json",
			command: "delegate.escalation_delivery_failed",
			want:    []string{"session_id", "escalation", "target", "reason", "will_retry_targets", "command_id"},
		},
	} {
		request := readJSONFixture[CommandRequest](t, tc.path)
		if request.Command != tc.command {
			t.Fatalf("%s command = %q, want %q", tc.path, request.Command, tc.command)
		}
		for _, key := range tc.want {
			if _, ok := request.Params[key]; !ok {
				t.Fatalf("%s missing param %q in %+v", tc.path, key, request.Params)
			}
		}
	}

	for _, tc := range []struct {
		path      string
		eventType string
		from      string
		to        []string
		phase     string
		payload   []string
	}{
		{
			path:      "fixtures/event/user-escalation-delivered.json",
			eventType: "user_escalation_delivered",
			from:      "agent-mod",
			to:        []string{"user"},
			phase:     "waiting_user",
			payload:   []string{"escalation_event_id", "delivery_target", "platform", "message_ref", "delivered_batch_id"},
		},
		{
			path:      "fixtures/event/user-escalation-delivery-failed.json",
			eventType: "user_escalation_delivery_failed",
			from:      "agent-mod",
			to:        []string{"agent-mod"},
			phase:     "waiting_user",
			payload:   []string{"escalation_event_id", "target", "reason", "will_retry_targets"},
		},
	} {
		event := readJSONFixture[eventEnvelopeFixture](t, tc.path)
		if event.Type != tc.eventType {
			t.Fatalf("%s type = %q, want %q", tc.path, event.Type, tc.eventType)
		}
		if event.From != tc.from {
			t.Fatalf("%s from = %q, want %q", tc.path, event.From, tc.from)
		}
		assertExactStrings(t, tc.path+" to", event.To, tc.to)
		if event.Phase != tc.phase {
			t.Fatalf("%s phase = %q, want %q", tc.path, event.Phase, tc.phase)
		}
		if event.CausationEventID != "evt_user_escalation_requested_01" {
			t.Fatalf("%s must causally reference the escalation request, got %q", tc.path, event.CausationEventID)
		}
		for _, key := range tc.payload {
			if _, ok := event.Payload[key]; !ok {
				t.Fatalf("%s missing payload %q in %+v", tc.path, key, event.Payload)
			}
		}
	}
}

func TestUnitConformanceDelegationReviewFixtureMatrix(t *testing.T) {
	for _, tc := range []struct {
		path    string
		command string
		params  []string
	}{
		{
			path:    "fixtures/command/delegate-new-request.json",
			command: "delegate.new",
			params:  []string{"session_id", "moderator", "assignee", "task", "command_id", "assignment_event_id"},
		},
		{
			path:    "fixtures/command/delegate-submit-request.json",
			command: "delegate.submit",
			params:  []string{"session_id", "actor", "command_id", "payload"},
		},
		{
			path:    "fixtures/command/delegate-submit-duplicate-request.json",
			command: "delegate.submit",
			params:  []string{"session_id", "actor", "command_id", "payload"},
		},
		{
			path:    "fixtures/command/delegate-review-request.json",
			command: "delegate.review",
			params:  []string{"session_id", "actor", "recipients", "command_id", "payload"},
		},
		{
			path:    "fixtures/command/delegate-review-submit-request.json",
			command: "delegate.review_submit",
			params:  []string{"session_id", "actor", "command_id", "payload"},
		},
		{
			path:    "fixtures/command/delegate-accept-request.json",
			command: "delegate.accept",
			params:  []string{"session_id", "actor", "command_id", "payload"},
		},
	} {
		request := readJSONFixture[CommandRequest](t, tc.path)
		if request.Command != tc.command {
			t.Fatalf("%s command = %q, want %q", tc.path, request.Command, tc.command)
		}
		for _, key := range tc.params {
			if _, ok := request.Params[key]; !ok {
				t.Fatalf("%s missing param %q in %+v", tc.path, key, request.Params)
			}
		}
	}

	newResponse := readJSONFixture[CommandResponse](t, "fixtures/command/delegate-new-response.json")
	if !newResponse.OK || newResponse.RequestID != "req_delegate_new_001" {
		t.Fatalf("delegate.new response has wrong envelope: %+v", newResponse)
	}
	results, ok := newResponse.Result["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("delegate.new response must expose session_created and task_assigned results, got %+v", newResponse.Result["results"])
	}
	if newResponse.Result["session_id"] != "sess_conformance_delegation" || newResponse.Result["deduplicated"] != false {
		t.Fatalf("delegate.new response has wrong result shape: %+v", newResponse.Result)
	}

	for _, tc := range []struct {
		path      string
		requestID string
		eventID   string
		dedup     bool
	}{
		{
			path:      "fixtures/command/delegate-submit-response.json",
			requestID: "req_delegate_submit_001",
			eventID:   "evt_work_submitted_cmd_delegate_submit_conformance",
			dedup:     false,
		},
		{
			path:      "fixtures/command/delegate-submit-duplicate-response.json",
			requestID: "req_delegate_submit_002_duplicate",
			eventID:   "evt_work_submitted_cmd_delegate_submit_conformance",
			dedup:     true,
		},
		{
			path:      "fixtures/command/delegate-review-response.json",
			requestID: "req_delegate_review_001",
			eventID:   "evt_review_requested_cmd_delegate_review_conformance",
			dedup:     false,
		},
		{
			path:      "fixtures/command/delegate-review-submit-response.json",
			requestID: "req_delegate_review_submit_001",
			eventID:   "evt_review_submitted_cmd_delegate_review_submit_conformance",
			dedup:     false,
		},
		{
			path:      "fixtures/command/delegate-accept-response.json",
			requestID: "req_delegate_accept_001",
			eventID:   "evt_work_accepted_cmd_delegate_accept_conformance",
			dedup:     false,
		},
	} {
		response := readJSONFixture[CommandResponse](t, tc.path)
		if !response.OK || response.RequestID != tc.requestID {
			t.Fatalf("%s has wrong response envelope: %+v", tc.path, response)
		}
		if response.Result["event_id"] != tc.eventID || response.Result["deduplicated"] != tc.dedup {
			t.Fatalf("%s has wrong event/dedup result: %+v", tc.path, response.Result)
		}
		if response.Result["cursor"] == "" || response.Result["offset"] == nil {
			t.Fatalf("%s missing cursor/offset result: %+v", tc.path, response.Result)
		}
	}

	duplicateRequest := readJSONFixture[CommandRequest](t, "fixtures/command/delegate-submit-duplicate-request.json")
	submitRequest := readJSONFixture[CommandRequest](t, "fixtures/command/delegate-submit-request.json")
	if duplicateRequest.Params["command_id"] != submitRequest.Params["command_id"] {
		t.Fatalf("duplicate fixture must reuse command_id to represent general command-id idempotency")
	}

	for _, tc := range []struct {
		path      string
		eventType string
		from      string
		to        []string
		phase     string
		payload   []string
	}{
		{
			path:      "fixtures/event/task-assigned-delegation.json",
			eventType: "task_assigned",
			from:      "agent-mod",
			to:        []string{"agent-1"},
			phase:     "assigned",
			payload:   []string{"task", "context", "acceptance_criteria", "expected_outputs"},
		},
		{
			path:      "fixtures/event/work-submitted.json",
			eventType: "work_submitted",
			from:      "agent-1",
			to:        []string{"agent-mod"},
			phase:     "submitted",
			payload:   []string{"summary"},
		},
		{
			path:      "fixtures/event/review-requested.json",
			eventType: "review_requested",
			from:      "agent-mod",
			to:        []string{"agent-1"},
			phase:     "under_review",
			payload:   []string{"review_focus"},
		},
		{
			path:      "fixtures/event/review-submitted.json",
			eventType: "review_submitted",
			from:      "agent-1",
			to:        []string{"agent-mod"},
			phase:     "under_review",
			payload:   []string{"verdict", "findings"},
		},
		{
			path:      "fixtures/event/work-accepted.json",
			eventType: "work_accepted",
			from:      "agent-mod",
			to:        []string{"agent-1", "agent-mod"},
			phase:     "accepted",
			payload:   []string{"final_summary"},
		},
	} {
		event := readJSONFixture[eventEnvelopeFixture](t, tc.path)
		if event.Type != tc.eventType || event.From != tc.from || event.Phase != tc.phase {
			t.Fatalf("%s has wrong envelope shape: %+v", tc.path, event)
		}
		assertExactStrings(t, tc.path+" to", event.To, tc.to)
		for _, key := range tc.payload {
			if _, ok := event.Payload[key]; !ok {
				t.Fatalf("%s missing payload %q in %+v", tc.path, key, event.Payload)
			}
		}
	}
}

func TestUnitConformanceVersionFixtureMatchesRequiredGroups(t *testing.T) {
	features := readJSONFixture[VersionFeatures](t, "fixtures/version/version-features.json")
	if features.ProtocolVersion != ProtocolVersion {
		t.Fatalf("version fixture protocol = %q, want %q", features.ProtocolVersion, ProtocolVersion)
	}
	assertExactStrings(t, "version feature_groups", features.FeatureGroups, RequiredFeatureGroups)
	assertExactStrings(t, "version features", features.Features, RequiredFeatureGroups)
	if features.FixtureManifest != "testdata/conformance/manifest.json" {
		t.Fatalf("unexpected fixture manifest: %q", features.FixtureManifest)
	}
}

func TestUnitConformanceStructuredErrorFixtures(t *testing.T) {
	for _, fixture := range []string{
		"fixtures/error/unsupported-feature.json",
		"fixtures/error/active-session-lock.json",
		"fixtures/error/cancel-unauthorized.json",
		"fixtures/error/cursor-gap.json",
		"fixtures/error/unauthorized-member.json",
		"fixtures/error/invalid-delivery-escalation-reference.json",
		"fixtures/error/unauthorized-delivery-reporter.json",
		"fixtures/error/delegate-unauthorized-actor.json",
		"fixtures/error/delegate-review-wrong-phase.json",
		"fixtures/error/delegate-review-submit-invalid-verdict.json",
	} {
		response := readJSONFixture[CommandResponse](t, fixture)
		if response.OK {
			t.Fatalf("%s should be an error response", fixture)
		}
		if response.Error == nil || response.Error.Code == "" || response.Error.Category == "" || response.Error.Message == "" {
			t.Fatalf("%s has incomplete structured error: %+v", fixture, response.Error)
		}
		if response.Error.Code == ErrorValidation && response.Error.Category != "validation" {
			t.Fatalf("%s validation error should use validation category: %+v", fixture, response.Error)
		}
		serialized := string(readConformanceBytes(t, fixture))
		for _, forbidden := range []string{"DISCORD_TOKEN", "gateway", "auth token", "Bearer ", "KAB_SECRET", "localhost"} {
			if strings.Contains(serialized, forbidden) {
				t.Fatalf("%s leaks forbidden live-service/secret wording %q", fixture, forbidden)
			}
		}
	}
}

type eventEnvelopeFixture struct {
	SchemaVersion    int            `json:"schema_version"`
	EventID          string         `json:"event_id"`
	CommandID        string         `json:"command_id,omitempty"`
	CausationEventID string         `json:"causation_event_id,omitempty"`
	CorrelationID    string         `json:"correlation_id,omitempty"`
	SessionID        string         `json:"session_id"`
	SessionType      string         `json:"session_type"`
	Phase            string         `json:"phase"`
	Type             string         `json:"type"`
	From             string         `json:"from"`
	To               []string       `json:"to"`
	CreatedAt        string         `json:"created_at"`
	Payload          map[string]any `json:"payload"`
}

type streamFrameFixture struct {
	Cursor   string               `json:"cursor"`
	IsReplay bool                 `json:"is_replay"`
	Event    eventEnvelopeFixture `json:"event"`
}

func readConformanceManifest(t *testing.T) conformanceManifest {
	t.Helper()
	return readJSONFixture[conformanceManifest](t, "manifest.json")
}

func readJSONFixture[T any](t *testing.T, relative string) T {
	t.Helper()
	data := readConformanceBytes(t, relative)
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s: %v", relative, err)
	}
	return out
}

func readNDJSONFixture(t *testing.T, relative string) []streamFrameFixture {
	t.Helper()
	path := conformancePath(t, relative)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", relative, err)
	}
	defer func() {
		_ = file.Close()
	}()
	var frames []streamFrameFixture
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var frame streamFrameFixture
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("decode %s line %d: %v", relative, len(frames)+1, err)
		}
		frames = append(frames, frame)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return frames
}

func readConformanceBytes(t *testing.T, relative string) []byte {
	t.Helper()
	path := conformancePath(t, relative)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return data
}

func conformancePath(t *testing.T, relative string) string {
	t.Helper()
	if filepath.IsAbs(relative) || strings.Contains(relative, "..") {
		t.Fatalf("unsafe conformance path: %s", relative)
	}
	path := filepath.Clean(filepath.Join("..", "..", "testdata", "conformance", relative))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("missing conformance artifact %s: %v", relative, err)
	}
	return path
}

func assertContainsAll(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			t.Fatalf("%s missing %q in %#v", label, value, got)
		}
	}
}

func assertExactStrings(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d: %#v", label, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q; all=%#v", label, i, got[i], want[i], got)
		}
	}
}

func usesStreamTail(value string) bool {
	normalized := strings.ToLower(value)
	return strings.Contains(normalized, "stream-tail") || strings.Contains(normalized, "stream.tail")
}
