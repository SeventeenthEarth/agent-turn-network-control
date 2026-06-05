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

REQUIRED_FIXTURES = [
    "fixtures/command/version-read-request.json",
    "fixtures/command/stream-replay-request.json",
    "fixtures/command/stream-ack-request.json",
    "fixtures/command/stream-ack-response.json",
    *REQUIRED_DELIVERY_FIXTURES[:4],
    "fixtures/event/session-created-delegation.json",
    "fixtures/event/stream-cursor-acknowledged.json",
    *REQUIRED_DELIVERY_FIXTURES[4:],
    "fixtures/error/unsupported-feature.json",
    "fixtures/error/active-session-lock.json",
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
