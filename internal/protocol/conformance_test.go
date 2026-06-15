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
	"fixtures/command/transcript-render-request.json",
	"fixtures/command/transcript-render-response.json",
	"fixtures/command/export-bundle-request.json",
	"fixtures/command/export-bundle-response.json",
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

var requiredCouncilLifecycleFixtures = []string{
	"fixtures/command/council-new-request.json",
	"fixtures/command/council-new-response.json",
	"fixtures/command/council-request-attendance-request.json",
	"fixtures/command/council-request-attendance-response.json",
	"fixtures/command/council-attend-request.json",
	"fixtures/command/council-attend-response.json",
	"fixtures/command/council-lock-agenda-request.json",
	"fixtures/command/council-lock-agenda-response.json",
	"fixtures/command/council-prepare-request.json",
	"fixtures/command/council-prepare-response.json",
	"fixtures/command/council-ready-request.json",
	"fixtures/command/council-ready-response.json",
	"fixtures/command/council-prepared-partial-request.json",
	"fixtures/command/council-prepared-partial-response.json",
	"fixtures/command/council-poll-request.json",
	"fixtures/command/council-poll-response.json",
	"fixtures/command/council-hand-raise-request.json",
	"fixtures/command/council-hand-raise-response.json",
	"fixtures/command/council-grant-request.json",
	"fixtures/command/council-grant-response.json",
	"fixtures/command/council-speak-request.json",
	"fixtures/command/council-speak-response.json",
	"fixtures/command/council-intervene-request.json",
	"fixtures/command/council-intervene-response.json",
	"fixtures/command/council-propose-request.json",
	"fixtures/command/council-propose-response.json",
	"fixtures/command/council-revise-request.json",
	"fixtures/command/council-revise-response.json",
	"fixtures/command/council-request-vote-request.json",
	"fixtures/command/council-request-vote-response.json",
	"fixtures/command/council-vote-request.json",
	"fixtures/command/council-vote-response.json",
	"fixtures/command/council-finalize-request.json",
	"fixtures/command/council-finalize-response.json",
	"fixtures/command/council-unresolved-request.json",
	"fixtures/command/council-unresolved-response.json",
	"fixtures/event/session-created-council.json",
	"fixtures/event/attendance-requested-council.json",
	"fixtures/event/member-attended-council.json",
	"fixtures/event/agenda-locked-council.json",
	"fixtures/event/preparation-requested-council.json",
	"fixtures/event/member-ready-council.json",
	"fixtures/event/member-prepared-partial-council.json",
	"fixtures/event/hand-raise-requested-council.json",
	"fixtures/event/hand-raise-council.json",
	"fixtures/event/speaker-selected-council.json",
	"fixtures/event/speech-council.json",
	"fixtures/event/moderator-intervention-council.json",
	"fixtures/event/draft-conclusion-council.json",
	"fixtures/event/draft-conclusion-revised-council.json",
	"fixtures/event/consensus-vote-requested-council.json",
	"fixtures/event/consensus-vote-council.json",
	"fixtures/event/council-finalized.json",
	"fixtures/event/council-unresolved.json",
	"fixtures/error/council-missing-attendance-agenda.json",
	"fixtures/error/council-invalid-principal.json",
}

var requiredCouncilArgumentGraphFixtures = []string{
	"fixtures/event/argument-graph-opening-new-axis-council.json",
	"fixtures/event/argument-graph-support-prior-council.json",
	"fixtures/event/argument-graph-multi-link-council.json",
	"fixtures/event/argument-graph-synthesize-council.json",
	"fixtures/event/argument-graph-dual-field-speech-council.json",
	"fixtures/event/argument-graph-legacy-only-speech-council.json",
	"fixtures/event/argument-graph-risk-decision-frame-council.json",
	"fixtures/event/argument-graph-hand-raise-target-links-council.json",
	"fixtures/command/council-speak-argument-graph-request.json",
	"fixtures/command/council-hand-raise-argument-graph-request.json",
	"fixtures/error/argument-graph-invalid-stance.json",
	"fixtures/error/argument-graph-future-reference.json",
	"fixtures/error/argument-graph-cross-session-reference.json",
	"fixtures/error/argument-graph-unknown-target-claim.json",
	"fixtures/error/argument-graph-new-axis-missing-reason.json",
	"fixtures/error/argument-graph-synthesize-single-target.json",
	"fixtures/error/argument-graph-quality-required-missing-claims.json",
	"fixtures/error/argument-graph-runtime-noise-speech.json",
	"fixtures/error/argument-graph-quality-required-orphan-speech.json",
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
	assertContainsAll(t, "COUNC-001 fixtures", manifest.Fixtures, requiredCouncilLifecycleFixtures)
	assertContainsAll(t, "ARGUE-002 fixtures", manifest.Fixtures, requiredCouncilArgumentGraphFixtures)
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
	statusReadRequest := readJSONFixture[CommandRequest](t, "fixtures/command/status-read-request.json")
	if statusReadRequest.Command != FeatureStatusRead {
		t.Fatalf("status.read fixture command = %q, want %q", statusReadRequest.Command, FeatureStatusRead)
	}
	statusReadResponse := readJSONFixture[CommandResponse](t, "fixtures/command/status-read-response.json")
	for _, key := range []string{"schema_version", "protocol_version", "daemon_version", "min_plugin_protocol_version", "feature_groups", "fixture_manifest", "capability_state", "operational_readiness"} {
		if _, ok := statusReadResponse.Result[key]; !ok {
			t.Fatalf("status.read response missing %q in %+v", key, statusReadResponse.Result)
		}
	}
	diagnosticsReadRequest := readJSONFixture[CommandRequest](t, "fixtures/command/diagnostics-read-request.json")
	if diagnosticsReadRequest.Command != FeatureDiagnosticsRead {
		t.Fatalf("diagnostics.read fixture command = %q, want %q", diagnosticsReadRequest.Command, FeatureDiagnosticsRead)
	}
	diagnosticsReadResponse := readJSONFixture[CommandResponse](t, "fixtures/command/diagnostics-read-response.json")
	for _, key := range []string{"schema_version", "protocol_version", "daemon_version", "min_plugin_protocol_version", "feature_groups", "fixture_manifest", "capability_state", "categories"} {
		if _, ok := diagnosticsReadResponse.Result[key]; !ok {
			t.Fatalf("diagnostics.read response missing %q in %+v", key, diagnosticsReadResponse.Result)
		}
	}

	streamRequest := readJSONFixture[CommandRequest](t, "fixtures/command/stream-replay-request.json")
	if streamRequest.Command != FeatureStreamReplay {
		t.Fatalf("stream fixture command = %q, want %q", streamRequest.Command, FeatureStreamReplay)
	}
	streamFollowRequest := readJSONFixture[CommandRequest](t, "fixtures/command/stream-follow-request.json")
	if streamFollowRequest.Command != FeatureStreamReplay || streamFollowRequest.Params["follow"] != true || streamFollowRequest.Params["follow_timeout_ms"] == nil || streamFollowRequest.Params["follow_poll_ms"] == nil {
		t.Fatalf("stream follow fixture has wrong bounded follow shape: %+v", streamFollowRequest)
	}
	streamStatusRequest := readJSONFixture[CommandRequest](t, "fixtures/command/stream-status-request.json")
	if streamStatusRequest.Command != FeatureStreamStatus || streamStatusRequest.Params["session_id"] == "" {
		t.Fatalf("stream status fixture has wrong command shape: %+v", streamStatusRequest)
	}
	streamStatusResponse := readJSONFixture[CommandResponse](t, "fixtures/command/stream-status-response.json")
	for _, key := range []string{"session_id", "latest_cursor", "latest_event_id", "cursors", "subscribers"} {
		if _, ok := streamStatusResponse.Result[key]; !ok {
			t.Fatalf("stream.status response missing %q in %+v", key, streamStatusResponse.Result)
		}
	}
	if usesStreamTail(string(readConformanceBytes(t, "fixtures/command/stream-replay-request.json")) + string(readConformanceBytes(t, "fixtures/command/stream-follow-request.json")) + string(readConformanceBytes(t, "fixtures/command/stream-status-request.json"))) {
		t.Fatalf("stream fixtures must not mention stream.tail")
	}
	transcriptRequest := readJSONFixture[CommandRequest](t, "fixtures/command/transcript-render-request.json")
	if transcriptRequest.Command != FeatureTranscriptRender || transcriptRequest.Params["format"] != "md" {
		t.Fatalf("transcript fixture has wrong command shape: %+v", transcriptRequest)
	}
	exportRequest := readJSONFixture[CommandRequest](t, "fixtures/command/export-bundle-request.json")
	if exportRequest.Command != FeatureExportBundle {
		t.Fatalf("export fixture command = %q, want %q", exportRequest.Command, FeatureExportBundle)
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

func TestUnitConformanceCouncilLifecycleFixtureMatrix(t *testing.T) {
	for _, tc := range []struct {
		path    string
		command string
		params  []string
	}{
		{path: "fixtures/command/council-new-request.json", command: "council.new", params: []string{"session_id", "moderator", "members", "title", "surface", "event_id", "command_id"}},
		{path: "fixtures/command/council-request-attendance-request.json", command: "council.request_attendance", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-attend-request.json", command: "council.attend", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-lock-agenda-request.json", command: "council.lock_agenda", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-prepare-request.json", command: "council.prepare", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-ready-request.json", command: "council.ready", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-prepared-partial-request.json", command: "council.prepared_partial", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-poll-request.json", command: "council.poll", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-hand-raise-request.json", command: "council.hand_raise", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-grant-request.json", command: "council.grant", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-speak-request.json", command: "council.speak", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-intervene-request.json", command: "council.intervene", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-propose-request.json", command: "council.propose", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-revise-request.json", command: "council.revise", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-request-vote-request.json", command: "council.request_vote", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-vote-request.json", command: "council.vote", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-finalize-request.json", command: "council.finalize", params: []string{"session_id", "actor", "command_id", "payload"}},
		{path: "fixtures/command/council-unresolved-request.json", command: "council.unresolved", params: []string{"session_id", "actor", "command_id", "payload"}},
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

	newResponse := readJSONFixture[CommandResponse](t, "fixtures/command/council-new-response.json")
	if !newResponse.OK || newResponse.Result["session_id"] != "sess_conformance_council" {
		t.Fatalf("council.new response has wrong shape: %+v", newResponse)
	}
	results, ok := newResponse.Result["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("council.new response must expose session_created result only, got %+v", newResponse.Result["results"])
	}

	for _, tc := range []struct {
		path      string
		requestID string
		eventID   string
	}{
		{path: "fixtures/command/council-request-attendance-response.json", requestID: "req_council_request_attendance_001", eventID: "evt_attendance_requested_cmd_council_request_attendance_conformance"},
		{path: "fixtures/command/council-attend-response.json", requestID: "req_council_attend_001", eventID: "evt_member_attended_cmd_council_attend_conformance"},
		{path: "fixtures/command/council-lock-agenda-response.json", requestID: "req_council_lock_agenda_001", eventID: "evt_agenda_locked_cmd_council_lock_agenda_conformance"},
		{path: "fixtures/command/council-prepare-response.json", requestID: "req_council_prepare_001", eventID: "evt_preparation_requested_cmd_council_prepare_conformance"},
		{path: "fixtures/command/council-ready-response.json", requestID: "req_council_ready_001", eventID: "evt_member_ready_cmd_council_ready_conformance"},
		{path: "fixtures/command/council-prepared-partial-response.json", requestID: "req_council_prepared_partial_001", eventID: "evt_member_prepared_partial_cmd_council_prepared_partial_conformance"},
		{path: "fixtures/command/council-poll-response.json", requestID: "req_council_poll_001", eventID: "evt_hand_raise_requested_cmd_council_poll_conformance"},
		{path: "fixtures/command/council-hand-raise-response.json", requestID: "req_council_hand_raise_001", eventID: "evt_hand_raise_cmd_council_hand_raise_conformance"},
		{path: "fixtures/command/council-grant-response.json", requestID: "req_council_grant_001", eventID: "evt_speaker_selected_cmd_council_grant_conformance"},
		{path: "fixtures/command/council-speak-response.json", requestID: "req_council_speak_001", eventID: "evt_speech_cmd_council_speak_conformance"},
		{path: "fixtures/command/council-intervene-response.json", requestID: "req_council_intervene_001", eventID: "evt_moderator_intervention_cmd_council_intervene_conformance"},
		{path: "fixtures/command/council-propose-response.json", requestID: "req_council_propose_001", eventID: "evt_draft_conclusion_cmd_council_propose_conformance"},
		{path: "fixtures/command/council-revise-response.json", requestID: "req_council_revise_001", eventID: "evt_draft_conclusion_cmd_council_revise_conformance"},
		{path: "fixtures/command/council-request-vote-response.json", requestID: "req_council_request_vote_001", eventID: "evt_consensus_vote_requested_cmd_council_request_vote_conformance"},
		{path: "fixtures/command/council-vote-response.json", requestID: "req_council_vote_001", eventID: "evt_consensus_vote_cmd_council_vote_conformance"},
		{path: "fixtures/command/council-finalize-response.json", requestID: "req_council_finalize_001", eventID: "evt_council_finalized_cmd_council_finalize_conformance"},
		{path: "fixtures/command/council-unresolved-response.json", requestID: "req_council_unresolved_001", eventID: "evt_council_unresolved_cmd_council_unresolved_conformance"},
	} {
		response := readJSONFixture[CommandResponse](t, tc.path)
		if !response.OK || response.RequestID != tc.requestID {
			t.Fatalf("%s has wrong response envelope: %+v", tc.path, response)
		}
		if response.Result["event_id"] != tc.eventID || response.Result["deduplicated"] != false {
			t.Fatalf("%s has wrong event/dedup result: %+v", tc.path, response.Result)
		}
		if response.Result["cursor"] == "" || response.Result["offset"] == nil {
			t.Fatalf("%s missing cursor/offset result: %+v", tc.path, response.Result)
		}
	}

	for _, tc := range []struct {
		path        string
		eventType   string
		sessionType string
		from        string
		to          []string
		phase       string
		payload     []string
	}{
		{path: "fixtures/event/session-created-council.json", eventType: "session_created", sessionType: "council", from: "kkachi-agent-networkd", to: []string{"agent-1", "agent-2", "agent-mod"}, phase: "created", payload: []string{"session_type", "title", "moderator", "participants", "surface"}},
		{path: "fixtures/event/attendance-requested-council.json", eventType: "attendance_requested", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "created", payload: []string{"timeout_sec", "required_members", "surface_kind"}},
		{path: "fixtures/event/member-attended-council.json", eventType: "member_attended", sessionType: "council", from: "agent-1", to: []string{"agent-mod"}, phase: "created", payload: []string{"status", "summary"}},
		{path: "fixtures/event/agenda-locked-council.json", eventType: "agenda_locked", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "created", payload: []string{"decision_question", "max_rounds"}},
		{path: "fixtures/event/preparation-requested-council.json", eventType: "preparation_requested", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "preparation", payload: []string{"topic", "timeout_sec"}},
		{path: "fixtures/event/member-ready-council.json", eventType: "member_ready", sessionType: "council", from: "agent-1", to: []string{"agent-mod"}, phase: "preparation", payload: []string{"summary", "notes_ref"}},
		{path: "fixtures/event/member-prepared-partial-council.json", eventType: "member_prepared_partial", sessionType: "council", from: "agent-2", to: []string{"agent-mod"}, phase: "preparation", payload: []string{"reason", "summary"}},
		{path: "fixtures/event/hand-raise-requested-council.json", eventType: "hand_raise_requested", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "discussion", payload: []string{"turn", "research_timeout_sec"}},
		{path: "fixtures/event/hand-raise-council.json", eventType: "hand_raise", sessionType: "council", from: "agent-1", to: []string{"agent-mod"}, phase: "discussion", payload: []string{"turn", "intent", "reason", "wants_to_speak"}},
		{path: "fixtures/event/speaker-selected-council.json", eventType: "speaker_selected", sessionType: "council", from: "agent-mod", to: []string{"agent-1"}, phase: "discussion", payload: []string{"turn", "member", "selection_mode"}},
		{path: "fixtures/event/speech-council.json", eventType: "speech", sessionType: "council", from: "agent-1", to: []string{"agent-2", "agent-mod"}, phase: "discussion", payload: []string{"turn", "speech"}},
		{path: "fixtures/event/moderator-intervention-council.json", eventType: "moderator_intervention", sessionType: "council", from: "agent-mod", to: []string{"agent-1"}, phase: "discussion", payload: []string{"member", "reason", "message"}},
		{path: "fixtures/event/draft-conclusion-council.json", eventType: "draft_conclusion", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "draft_conclusion", payload: []string{"draft", "draft_version"}},
		{path: "fixtures/event/draft-conclusion-revised-council.json", eventType: "draft_conclusion", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "draft_conclusion", payload: []string{"draft", "draft_version", "revision_reason", "supersedes_draft_version"}},
		{path: "fixtures/event/consensus-vote-requested-council.json", eventType: "consensus_vote_requested", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "consensus_vote", payload: []string{"draft_version", "consensus_round", "timeout_sec"}},
		{path: "fixtures/event/consensus-vote-council.json", eventType: "consensus_vote", sessionType: "council", from: "agent-1", to: []string{"agent-mod"}, phase: "consensus_vote", payload: []string{"draft_version", "consensus_round", "vote", "reason"}},
		{path: "fixtures/event/council-finalized.json", eventType: "council_finalized", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "finalized", payload: []string{"final_summary"}},
		{path: "fixtures/event/council-unresolved.json", eventType: "council_unresolved", sessionType: "council", from: "agent-mod", to: []string{"agent-1", "agent-2"}, phase: "unresolved", payload: []string{"reason", "timeout_evidence"}},
	} {
		event := readJSONFixture[eventEnvelopeFixture](t, tc.path)
		if event.Type != tc.eventType || event.SessionType != tc.sessionType || event.From != tc.from || event.Phase != tc.phase {
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

func TestUnitConformanceCouncilArgumentGraphFixtureMatrix(t *testing.T) {
	speechFixtures := []string{
		"fixtures/event/argument-graph-opening-new-axis-council.json",
		"fixtures/event/argument-graph-support-prior-council.json",
		"fixtures/event/argument-graph-multi-link-council.json",
		"fixtures/event/argument-graph-synthesize-council.json",
		"fixtures/event/argument-graph-dual-field-speech-council.json",
		"fixtures/event/argument-graph-risk-decision-frame-council.json",
	}
	linkedStances := []string{"support", "challenge", "refine", "extend", "synthesize", "question", "risk_addition", "decision_frame"}
	contributionTypes := append(append([]string{}, linkedStances...), "new_axis")
	speechEvents := map[string]eventEnvelopeFixture{}
	targetClaims := map[string]map[string]bool{}
	for _, fixture := range speechFixtures {
		event := readJSONFixture[eventEnvelopeFixture](t, fixture)
		if event.Type != "speech" || event.SessionType != "council" || event.Phase != "discussion" {
			t.Fatalf("%s has wrong ARGUE speech envelope: %+v", fixture, event)
		}
		requireTurn(t, fixture, event.Payload)
		speechEvents[event.EventID] = event
		claims := requireObjectArray(t, fixture, event.Payload, "claims")
		if len(claims) == 0 {
			t.Fatalf("%s must expose at least one claim", fixture)
		}
		targetClaims[event.EventID] = map[string]bool{}
		for _, claim := range claims {
			claimID := requireStringField(t, fixture, claim, "claim_id")
			requireStringField(t, fixture, claim, "summary")
			if kind, ok := claim["kind"].(string); ok {
				requireOneOf(t, fixture, "claim kind", kind, []string{"observation", "requirement", "risk", "decision_frame", "evidence", "open_question", "proposal"})
			}
			targetClaims[event.EventID][claimID] = true
		}
	}

	for _, fixture := range speechFixtures {
		event := readJSONFixture[eventEnvelopeFixture](t, fixture)
		contributionType := requireStringField(t, fixture, event.Payload, "contribution_type")
		requireOneOf(t, fixture, "contribution_type", contributionType, contributionTypes)
		if contributionType == "new_axis" {
			requireStringField(t, fixture, event.Payload, "new_axis_reason")
			continue
		}
		links := requireObjectArray(t, fixture, event.Payload, "stance_links")
		if contributionType == "synthesize" && len(links) < 2 {
			t.Fatalf("%s synthesize fixture must expose at least two stance links", fixture)
		}
		for _, link := range links {
			targetEventID := requireStringField(t, fixture, link, "target_event_id")
			targetClaimID := requireStringField(t, fixture, link, "target_claim_id")
			stance := requireStringField(t, fixture, link, "stance")
			requireOneOf(t, fixture, "stance", stance, linkedStances)
			requireStringField(t, fixture, link, "rationale")
			targetEvent, ok := speechEvents[targetEventID]
			if !ok {
				t.Fatalf("%s targets unknown event %s", fixture, targetEventID)
			}
			if targetEvent.SessionID != event.SessionID {
				t.Fatalf("%s targets cross-session event %s", fixture, targetEventID)
			}
			if targetEvent.EventID == event.EventID {
				t.Fatalf("%s must not self-link to %s", fixture, targetEventID)
			}
			if requireTurn(t, fixture, targetEvent.Payload) >= requireTurn(t, fixture, event.Payload) {
				t.Fatalf("%s must link only to prior events, target=%s", fixture, targetEventID)
			}
			if targetClaims[targetEventID] == nil || !targetClaims[targetEventID][targetClaimID] {
				t.Fatalf("%s targets unknown claim %s/%s", fixture, targetEventID, targetClaimID)
			}
		}
	}

	dualField := readJSONFixture[eventEnvelopeFixture](t, "fixtures/event/argument-graph-dual-field-speech-council.json")
	respondsTo := requireStringField(t, "dual-field speech", dualField.Payload, "responds_to_event_id")
	if !stanceLinksContainTarget(t, "dual-field speech", dualField.Payload, respondsTo) {
		t.Fatalf("dual-field speech responds_to_event_id must match one stance_links target")
	}

	legacyOnly := readJSONFixture[eventEnvelopeFixture](t, "fixtures/event/argument-graph-legacy-only-speech-council.json")
	if legacyOnly.Type != "speech" || legacyOnly.SessionType != "council" || legacyOnly.Phase != "discussion" {
		t.Fatalf("legacy-only ARGUE speech fixture has wrong envelope: %+v", legacyOnly)
	}
	legacyTarget := requireStringField(t, "legacy-only speech", legacyOnly.Payload, "responds_to_event_id")
	if _, ok := speechEvents[legacyTarget]; !ok {
		t.Fatalf("legacy-only speech targets unknown event %s", legacyTarget)
	}
	assertPayloadKeyAbsent(t, "legacy-only speech", legacyOnly.Payload, "stance_links")

	handRaise := readJSONFixture[eventEnvelopeFixture](t, "fixtures/event/argument-graph-hand-raise-target-links-council.json")
	if handRaise.Type != "hand_raise" || handRaise.SessionType != "council" || handRaise.Phase != "discussion" {
		t.Fatalf("ARGUE hand_raise fixture has wrong envelope: %+v", handRaise)
	}
	targetLinks := requireObjectArray(t, "ARGUE hand_raise", handRaise.Payload, "target_links")
	if len(targetLinks) == 0 {
		t.Fatalf("ARGUE hand_raise target_links must not be empty")
	}
	assertPayloadKeyAbsent(t, "ARGUE hand_raise", handRaise.Payload, "target_event_ids")
	assertPayloadKeyAbsent(t, "ARGUE hand_raise", handRaise.Payload, "target_claim_ids")
	for _, link := range targetLinks {
		targetEventID := requireStringField(t, "ARGUE hand_raise", link, "target_event_id")
		targetClaimID := requireStringField(t, "ARGUE hand_raise", link, "target_claim_id")
		stance := requireStringField(t, "ARGUE hand_raise", link, "stance")
		requireOneOf(t, "ARGUE hand_raise", "stance", stance, linkedStances)
		targetEvent, ok := speechEvents[targetEventID]
		if !ok {
			t.Fatalf("ARGUE hand_raise targets unknown event %s", targetEventID)
		}
		if targetEvent.SessionID != handRaise.SessionID {
			t.Fatalf("ARGUE hand_raise targets cross-session event %s", targetEventID)
		}
		if requireTurn(t, "ARGUE hand_raise target", targetEvent.Payload) >= requireTurn(t, "ARGUE hand_raise", handRaise.Payload) {
			t.Fatalf("ARGUE hand_raise must link only to prior events, target=%s", targetEventID)
		}
		if targetClaims[targetEventID] == nil || !targetClaims[targetEventID][targetClaimID] {
			t.Fatalf("ARGUE hand_raise targets unknown claim %s/%s", targetEventID, targetClaimID)
		}
	}

	for _, tc := range []struct {
		path    string
		command string
		payload []string
	}{
		{
			path:    "fixtures/command/council-speak-argument-graph-request.json",
			command: "council.speak",
			payload: []string{"turn", "speech", "claims", "stance_links", "contribution_type"},
		},
		{
			path:    "fixtures/command/council-hand-raise-argument-graph-request.json",
			command: "council.hand_raise",
			payload: []string{"turn", "wants_to_speak", "intent", "target_links", "reason"},
		},
	} {
		request := readJSONFixture[CommandRequest](t, tc.path)
		if request.Command != tc.command {
			t.Fatalf("%s command = %q, want %q", tc.path, request.Command, tc.command)
		}
		paramsPayload, ok := request.Params["payload"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing payload object: %+v", tc.path, request.Params)
		}
		for _, key := range tc.payload {
			if _, ok := paramsPayload[key]; !ok {
				t.Fatalf("%s missing payload %q in %+v", tc.path, key, paramsPayload)
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
	argumentGraphErrorFixtures := map[string]bool{
		"fixtures/error/argument-graph-invalid-stance.json":                  true,
		"fixtures/error/argument-graph-future-reference.json":                true,
		"fixtures/error/argument-graph-cross-session-reference.json":         true,
		"fixtures/error/argument-graph-unknown-target-claim.json":            true,
		"fixtures/error/argument-graph-new-axis-missing-reason.json":         true,
		"fixtures/error/argument-graph-synthesize-single-target.json":        true,
		"fixtures/error/argument-graph-quality-required-missing-claims.json": true,
		"fixtures/error/argument-graph-runtime-noise-speech.json":            true,
		"fixtures/error/argument-graph-quality-required-orphan-speech.json":  true,
	}
	for _, fixture := range []string{
		"fixtures/error/unsupported-feature.json",
		"fixtures/error/active-session-lock.json",
		"fixtures/error/cancel-unauthorized.json",
		"fixtures/error/cursor-gap.json",
		"fixtures/error/command-id-conflict.json",
		"fixtures/error/unauthorized-member.json",
		"fixtures/error/invalid-delivery-escalation-reference.json",
		"fixtures/error/unauthorized-delivery-reporter.json",
		"fixtures/error/delegate-unauthorized-actor.json",
		"fixtures/error/delegate-review-wrong-phase.json",
		"fixtures/error/delegate-review-submit-invalid-verdict.json",
		"fixtures/error/council-missing-attendance-agenda.json",
		"fixtures/error/council-invalid-principal.json",
		"fixtures/error/argument-graph-invalid-stance.json",
		"fixtures/error/argument-graph-future-reference.json",
		"fixtures/error/argument-graph-cross-session-reference.json",
		"fixtures/error/argument-graph-unknown-target-claim.json",
		"fixtures/error/argument-graph-new-axis-missing-reason.json",
		"fixtures/error/argument-graph-synthesize-single-target.json",
		"fixtures/error/argument-graph-quality-required-missing-claims.json",
		"fixtures/error/argument-graph-runtime-noise-speech.json",
		"fixtures/error/argument-graph-quality-required-orphan-speech.json",
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
		if argumentGraphErrorFixtures[fixture] {
			if response.Error.Details["category"] != "argument_graph_static_protocol_example" || response.Error.Details["runtime_enforcement"] != false {
				t.Fatalf("%s must retain ARGUE static-boundary metadata: %+v", fixture, response.Error.Details)
			}
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

func requireObjectArray(t *testing.T, label string, payload map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("%s missing object array %q in %+v", label, key, payload)
	}
	out := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s %q[%d] is not an object: %+v", label, key, i, item)
		}
		out = append(out, object)
	}
	return out
}

func requireStringField(t *testing.T, label string, payload map[string]any, key string) string {
	t.Helper()
	value, ok := payload[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		t.Fatalf("%s missing non-empty string %q in %+v", label, key, payload)
	}
	return value
}

func requireTurn(t *testing.T, label string, payload map[string]any) int {
	t.Helper()
	value, ok := payload["turn"].(float64)
	if !ok || value < 1 || value != float64(int(value)) {
		t.Fatalf("%s missing positive integer turn in %+v", label, payload)
	}
	return int(value)
}

func requireOneOf(t *testing.T, label, field, value string, allowed []string) {
	t.Helper()
	for _, candidate := range allowed {
		if value == candidate {
			return
		}
	}
	t.Fatalf("%s %s = %q, want one of %#v", label, field, value, allowed)
}

func assertPayloadKeyAbsent(t *testing.T, label string, payload map[string]any, key string) {
	t.Helper()
	if _, ok := payload[key]; ok {
		t.Fatalf("%s must not expose deprecated parallel-array key %q in %+v", label, key, payload)
	}
}

func stanceLinksContainTarget(t *testing.T, label string, payload map[string]any, target string) bool {
	t.Helper()
	for _, link := range requireObjectArray(t, label, payload, "stance_links") {
		if link["target_event_id"] == target {
			return true
		}
	}
	return false
}

func usesStreamTail(value string) bool {
	normalized := strings.ToLower(value)
	return strings.Contains(normalized, "stream-tail") || strings.Contains(normalized, "stream.tail")
}
