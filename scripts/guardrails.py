from pathlib import Path
root = Path(__file__).resolve().parents[1]
docs = root / "docs"
required = [
    "README.md", "00-overview.md", "02-architecture.md", "09-implementation-epics.md",
    "11-distribution-and-plugin.md", "18-testing-strategy.md", "19-tooling.md", "21-cross-repo-development.md",
]
missing = [p for p in required if not (docs / p).exists()]
if missing:
    raise SystemExit(f"missing required docs: {missing}")
forbidden = [
    "shared Python KAN client/core",
    "shared client",
    "shared-client",
    "shared client/core",
    "src/kkachi_agent_network/",
    "uv tool install git+https://github.com/<owner>/kkachi-agent-network",
    "`../kkachi-agent-network-plugin",
]
violations = []
for path in docs.glob("*.md"):
    text = path.read_text(encoding="utf-8")
    lowered = text.lower()
    for token in forbidden:
        if token.lower() in lowered:
            violations.append(f"{path.relative_to(root)}:{token}")
if violations:
    raise SystemExit("stale core/plugin split wording found:\n" + "\n".join(violations))
make = (root / "Makefile").read_text(encoding="utf-8")
for target in ["test-prepare:", "test-unit:", "test-int:", "test-e2e:", "test:", "check-plugin-contract:"]:
    if target not in make:
        raise SystemExit(f"missing Makefile target {target}")
print("docs-guardrails: ok")
