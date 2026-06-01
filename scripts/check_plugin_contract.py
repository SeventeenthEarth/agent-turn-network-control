from __future__ import annotations

import json
import os
from pathlib import Path

CORE = Path(__file__).resolve().parents[1]
PLUGIN = Path(os.environ.get("KAN_PLUGIN_REPO", CORE.parent / "kkachi-agent-network-plugin")).resolve()
EXPECTED_PROTOCOL = "kan-protocol-v1alpha0"


def require(path: Path, label: str) -> str:
    if not path.exists():
        raise SystemExit(f"missing {label}: {path}")
    return path.read_text(encoding="utf-8")


manifest_path = CORE / "testdata" / "conformance" / "manifest.json"
manifest = json.loads(require(manifest_path, "core conformance manifest"))
if manifest.get("protocol_version") != EXPECTED_PROTOCOL:
    raise SystemExit(f"core manifest protocol mismatch: {manifest.get('protocol_version')} != {EXPECTED_PROTOCOL}")

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
