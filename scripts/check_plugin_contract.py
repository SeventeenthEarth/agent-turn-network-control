from __future__ import annotations

import json
import os
from pathlib import Path

CORE = Path(__file__).resolve().parents[1]
PLUGIN = Path(os.environ.get("KAN_PLUGIN_REPO", CORE.parent / "kkachi-agent-network-plugin")).resolve()
EXPECTED_PROTOCOL = "kan-protocol-v1alpha0"
CONFORMANCE = CORE / "testdata" / "conformance"

REQUIRED_FEATURE_GROUPS = [
    "version.read",
    "status.read",
    "diagnostics.read",
    "command_envelope",
    "event_envelope",
    "structured_error",
    "stream_frame",
    "stream.replay",
    "stream.follow",
    "stream.ack",
    "stream.status",
    "active_session.lock",
    "delivery_evidence",
    "conformance.fixtures",
    "council.lifecycle",
    "transcript.render",
    "export.bundle",
]

REQUIRED_SCHEMAS = [
    "schemas/command-envelope.schema.json",
    "schemas/event-envelope.schema.json",
    "schemas/structured-error.schema.json",
    "schemas/stream-frame.schema.json",
    "schemas/version-features.schema.json",
    "schemas/delivery-evidence-command.schema.json",
]

REQUIRED_DELIVERY_FIXTURES = [
    "fixtures/command/delegate-escalation-delivered-request.json",
    "fixtures/command/delegate-escalation-delivered-response.json",
    "fixtures/command/delegate-escalation-delivery-failed-request.json",
    "fixtures/command/delegate-escalation-delivery-failed-response.json",
    "fixtures/event/user-escalation-delivered.json",
    "fixtures/event/user-escalation-delivery-failed.json",
]

REQUIRED_CANCEL_FIXTURES = [
    "fixtures/command/cancel-request.json",
    "fixtures/command/cancel-response.json",
    "fixtures/event/session-cancelled.json",
    "fixtures/error/cancel-unauthorized.json",
]

REQUIRED_DELEGATION_REVIEW_FIXTURES = [
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
]

REQUIRED_COUNCIL_LIFECYCLE_FIXTURES = [
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
]

REQUIRED_FIXTURES = [
    "fixtures/command/version-read-request.json",
    "fixtures/command/status-read-request.json",
    "fixtures/command/status-read-response.json",
    "fixtures/command/diagnostics-read-request.json",
    "fixtures/command/diagnostics-read-response.json",
    "fixtures/command/stream-replay-request.json",
    "fixtures/command/stream-follow-request.json",
    "fixtures/command/stream-ack-request.json",
    "fixtures/command/stream-ack-response.json",
    "fixtures/command/stream-status-request.json",
    "fixtures/command/stream-status-response.json",
    "fixtures/command/transcript-render-request.json",
    "fixtures/command/transcript-render-response.json",
    "fixtures/command/export-bundle-request.json",
    "fixtures/command/export-bundle-response.json",
    *REQUIRED_CANCEL_FIXTURES[:2],
    *REQUIRED_DELIVERY_FIXTURES[:4],
    "fixtures/event/session-created-delegation.json",
    REQUIRED_CANCEL_FIXTURES[2],
    "fixtures/event/stream-cursor-acknowledged.json",
    *REQUIRED_DELIVERY_FIXTURES[4:],
    *REQUIRED_DELEGATION_REVIEW_FIXTURES,
    *REQUIRED_COUNCIL_LIFECYCLE_FIXTURES,
    "fixtures/error/unsupported-feature.json",
    "fixtures/error/active-session-lock.json",
    REQUIRED_CANCEL_FIXTURES[3],
    "fixtures/error/cursor-gap.json",
    "fixtures/error/unauthorized-member.json",
    "fixtures/error/invalid-delivery-escalation-reference.json",
    "fixtures/error/unauthorized-delivery-reporter.json",
    "fixtures/stream/from-start.ndjson",
    "fixtures/stream/since-cursor.ndjson",
    "fixtures/stream/follow-replay-then-live.ndjson",
    "fixtures/version/version-features.json",
]


def require(path: Path, label: str) -> str:
    if not path.exists():
        raise SystemExit(f"missing {label}: {path}")
    return path.read_text(encoding="utf-8")


def require_list(manifest: dict, key: str) -> list[str]:
    value = manifest.get(key)
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise SystemExit(f"core manifest {key} must be a string list")
    return value


def reject_stream_tail(values: list[str], label: str) -> None:
    for value in values:
        normalized = value.lower()
        if "stream-tail" in normalized or "stream.tail" in normalized:
            raise SystemExit(f"{label} uses non-canonical stream-tail vocabulary: {value}")


def require_relative_file(relative: str, label: str) -> Path:
    path = (CONFORMANCE / relative).resolve()
    if not path.is_relative_to(CONFORMANCE.resolve()):
        raise SystemExit(f"{label} path escapes conformance root: {relative}")
    if not path.exists():
        raise SystemExit(f"missing {label}: {relative}")
    return path


def require_json_file(relative: str, label: str) -> None:
    path = require_relative_file(relative, label)
    try:
        json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SystemExit(f"invalid JSON {label}: {relative}: {exc}") from exc


def require_ndjson_file(relative: str, label: str) -> None:
    path = require_relative_file(relative, label)
    lines = [line for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
    if not lines:
        raise SystemExit(f"empty NDJSON {label}: {relative}")
    for line_no, line in enumerate(lines, 1):
        try:
            json.loads(line)
        except json.JSONDecodeError as exc:
            raise SystemExit(f"invalid NDJSON {label}: {relative}:{line_no}: {exc}") from exc


manifest_path = CONFORMANCE / "manifest.json"
manifest = json.loads(require(manifest_path, "core conformance manifest"))
if manifest.get("protocol_version") != EXPECTED_PROTOCOL:
    raise SystemExit(f"core manifest protocol mismatch: {manifest.get('protocol_version')} != {EXPECTED_PROTOCOL}")

fixtures = require_list(manifest, "fixtures")
schemas = require_list(manifest, "schemas")
feature_groups = require_list(manifest, "required_feature_groups")
if not fixtures:
    raise SystemExit("core manifest fixtures must not be empty")
reject_stream_tail(fixtures + schemas, "core manifest path")
reject_stream_tail(feature_groups, "core manifest feature group")

missing_groups = sorted(set(REQUIRED_FEATURE_GROUPS) - set(feature_groups))
if missing_groups:
    raise SystemExit(f"core manifest missing required feature groups: {', '.join(missing_groups)}")
extra_groups = sorted(set(feature_groups) - set(REQUIRED_FEATURE_GROUPS))
if extra_groups:
    raise SystemExit(f"core manifest has unexpected feature groups: {', '.join(extra_groups)}")

missing_schemas = sorted(set(REQUIRED_SCHEMAS) - set(schemas))
if missing_schemas:
    raise SystemExit(f"core manifest missing required schemas: {', '.join(missing_schemas)}")
missing_fixtures = sorted(set(REQUIRED_FIXTURES) - set(fixtures))
if missing_fixtures:
    raise SystemExit(f"core manifest missing required fixtures: {', '.join(missing_fixtures)}")
missing_delivery = sorted(set(REQUIRED_DELIVERY_FIXTURES) - set(fixtures))
if missing_delivery:
    raise SystemExit(f"core manifest missing delivery-evidence fixtures: {', '.join(missing_delivery)}")
missing_cancel = sorted(set(REQUIRED_CANCEL_FIXTURES) - set(fixtures))
if missing_cancel:
    raise SystemExit(f"core manifest missing cancel/session_cancelled fixtures: {', '.join(missing_cancel)}")
missing_delegation_review = sorted(set(REQUIRED_DELEGATION_REVIEW_FIXTURES) - set(fixtures))
if missing_delegation_review:
    raise SystemExit(
        "core manifest missing DELEG-002 delegation/review fixtures: "
        + ", ".join(missing_delegation_review)
    )
missing_council_lifecycle = sorted(set(REQUIRED_COUNCIL_LIFECYCLE_FIXTURES) - set(fixtures))
if missing_council_lifecycle:
    raise SystemExit(
        "core manifest missing COUNC-001 council lifecycle fixtures: "
        + ", ".join(missing_council_lifecycle)
    )

for schema in schemas:
    require_json_file(schema, "schema")
for fixture in fixtures:
    if fixture.endswith(".ndjson"):
        require_ndjson_file(fixture, "fixture")
    else:
        require_json_file(fixture, "fixture")

plugin_readme = require(PLUGIN / "docs" / "README.md", "plugin docs README")
compat = require(PLUGIN / "docs" / "07-core-compatibility.md", "plugin core compatibility doc")
contract = require(PLUGIN / "docs" / "02-plugin-contract.md", "plugin contract doc")
makefile = require(PLUGIN / "Makefile", "plugin Makefile")

required_phrases = [
    EXPECTED_PROTOCOL,
    "fail closed",
    "Fixture manifest",
    "make check-core-contract",
    "plugin is not the source of truth",
]
combined = "\n".join([plugin_readme, compat, contract])
for phrase in required_phrases:
    if phrase not in combined:
        raise SystemExit(f"plugin docs missing phrase: {phrase}")

if "check-core-contract:" not in makefile:
    raise SystemExit("plugin Makefile missing check-core-contract target")

print(f"check-plugin-contract: ok ({PLUGIN})")
