from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import re
from typing import Iterable


ROOT = Path(__file__).resolve().parents[1]
VALID_REASONS = {
    "historical/provenance",
    "safety-boundary",
    "local-workspace-compatibility",
    "test-fixture-legacy-compatibility",
}
REQUIRED_DOCS = [
    "README.md",
    "00-overview.md",
    "02-architecture.md",
    "09-implementation-epics.md",
    "11-distribution-and-plugin.md",
    "18-testing-strategy.md",
    "19-tooling.md",
    "21-cross-repo-development.md",
]
REQUIRED_MAKE_TARGETS = [
    "test-prepare:",
    "test-unit:",
    "test-int:",
    "test-e2e:",
    "test:",
    "check-plugin-contract:",
    "docs-guardrails:",
    "guardrails-test:",
]
SCAN_ROOTS = [
    "README.md",
    "Makefile",
    "go.mod",
    "cmd",
    "docs",
    "internal",
    "scripts",
    "testdata/conformance",
    "tests",
]
EXCLUDED_PATHS = {
    "scripts/guardrails.py",
    "scripts/guardrails_test.py",
}
EXCLUDED_DIRS = {
    ".git",
    ".kkachi",
    ".omx",
    ".codegraph",
    "__pycache__",
}
TEXT_SUFFIXES = {
    ".go",
    ".json",
    ".md",
    ".ndjson",
    ".py",
    ".schema",
    ".txt",
    ".yaml",
    ".yml",
}


@dataclass(frozen=True)
class ForbiddenTerm:
    name: str
    pattern: re.Pattern[str]


@dataclass(frozen=True)
class AcceptedHit:
    path: str
    term: str
    line_pattern: re.Pattern[str]
    reason: str
    occurrence_pattern: re.Pattern[str] | None = None
    line_number: int | None = None


@dataclass(frozen=True)
class Finding:
    path: str
    line: int
    column: int
    term: str
    text: str

    def render(self) -> str:
        return f"{self.path}:{self.line}:{self.column}:{self.term}: {self.text.strip()}"


FORBIDDEN_TERMS = [
    ForbiddenTerm("shared Python KAN client/core", re.compile(r"shared Python KAN client/core", re.IGNORECASE)),
    ForbiddenTerm("shared client/core", re.compile(r"shared[- ]client(?:/core)?", re.IGNORECASE)),
    ForbiddenTerm("src/kkachi_agent_network/", re.compile(r"src/kkachi_agent_network/")),
    ForbiddenTerm("kkachi_agent_network", re.compile(r"\bkkachi_agent_network\w*\b")),
    ForbiddenTerm("KKACHI_AGENT_NETWORK", re.compile(r"\bKKACHI_AGENT_NETWORK\w*\b")),
    ForbiddenTerm("kkachi-agent-network-control", re.compile(r"\bkkachi-agent-network-control\b")),
    ForbiddenTerm("kkachi-agent-network-plugin", re.compile(r"\bkkachi-agent-network-plugin\b")),
    ForbiddenTerm("kkachi-agent-networkd", re.compile(r"\bkkachi-agent-networkd\b")),
    ForbiddenTerm("kkachi-agent-network", re.compile(r"\bkkachi-agent-network\b(?![-A-Za-z0-9_])")),
    ForbiddenTerm("kan-plugin", re.compile(r"\bkan-plugin\b")),
    ForbiddenTerm("kan_discussion", re.compile(r"\bkan_discussion_[A-Za-z0-9_]+\b")),
    ForbiddenTerm("kan_selected", re.compile(r"\bkan_selected_[A-Za-z0-9_]+\b")),
    ForbiddenTerm("KAN public label", re.compile(r"\bKAN\b")),
    ForbiddenTerm("kan-* public label", re.compile(r"(?:`kan-\*`|\bkan-(?!plugin\b)[A-Za-z0-9_-]+\b)")),
]


def accepted_hit(
    path: str,
    term: str,
    line: str,
    reason: str,
    occurrence: str | None = None,
    line_number: int | None = None,
) -> AcceptedHit:
    return AcceptedHit(
        path,
        term,
        re.compile(line),
        reason,
        re.compile(occurrence if occurrence is not None else line),
        line_number,
    )


def accepted_literal(
    path: str,
    term: str,
    line_text: str,
    reason: str,
    occurrence: str | None = None,
    line_number: int | None = None,
) -> AcceptedHit:
    return accepted_hit(path, term, re.escape(line_text), reason, re.escape(occurrence or term), line_number)


ACCEPTED_HITS = [
    # Historical local workflow ids still use the prior private control path.
    AcceptedHit("docs/kkachi-docs-map.yaml", "kkachi-agent-network-control", re.compile(r'graph_id: "graph-kkachi-project-kkachi-agent-network-control'), "local-workspace-compatibility"),
    # Safety-boundary references: explicit legacy rejection, env scrub, temp-path isolation, and protocol principals.
    AcceptedHit("internal/command/atn004_test.go", "kkachi-agent-network", re.compile(r"cmd.*kkachi-agent-network|forbidden.*kkachi-agent-network"), "safety-boundary"),
    AcceptedHit("internal/command/atn004_test.go", "kkachi-agent-networkd", re.compile(r"cmd.*kkachi-agent-networkd|kkachi-agent-networkd\.sock|forbidden.*kkachi-agent-networkd"), "safety-boundary"),
    AcceptedHit("internal/command/atn004_test.go", "KKACHI_AGENT_NETWORK", re.compile(r"KKACHI_AGENT_NETWORK_HOME"), "safety-boundary"),
    AcceptedHit("scripts/ltran003_live_local_smoke.py", "KKACHI_AGENT_NETWORK", re.compile(r'KKACHI_AGENT_NETWORK(?:D_PATH|_HOME)'), "safety-boundary"),
    AcceptedHit("docs/kkachi-docs-map.yaml", "kan-* public label", re.compile(r"kan_team:|discord-kan-control-workflow"), "safety-boundary"),
    AcceptedHit("internal/command/daemon_test.go", "kan-* public label", re.compile(r"not-a-kan-socket|kan-command-daemon"), "safety-boundary"),
    AcceptedHit("internal/daemon/server_test.go", "kan-* public label", re.compile(r"kan-daemon-"), "safety-boundary"),
    AcceptedHit("internal/transport/unix_test.go", "kan-* public label", re.compile(r"not-a-kan-socket|kan-transport-"), "safety-boundary"),
    # Test fixture compatibility: stable old fixture/principal strings consumed by protocol compatibility tests.
    AcceptedHit("testdata/conformance/fixtures/command/status-read-response.json", "kkachi-agent-network", re.compile(r'"/tmp/kkachi-agent-network'), "test-fixture-legacy-compatibility"),
]




def _read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def _relative(root: Path, path: Path) -> str:
    return path.relative_to(root).as_posix()


def _is_text_file(path: Path) -> bool:
    return path.name in {"Makefile", "go.mod", "README.md"} or path.suffix in TEXT_SUFFIXES


def _is_excluded(rel: str) -> bool:
    parts = set(Path(rel).parts)
    return rel in EXCLUDED_PATHS or bool(parts & EXCLUDED_DIRS)


def iter_scan_files(root: Path, scan_roots: Iterable[str] = SCAN_ROOTS) -> list[Path]:
    files: list[Path] = []
    for entry in scan_roots:
        path = root / entry
        if not path.exists():
            continue
        if path.is_file():
            rel = _relative(root, path)
            if not _is_excluded(rel) and _is_text_file(path):
                files.append(path)
            continue
        for child in path.rglob("*"):
            if not child.is_file():
                continue
            rel = _relative(root, child)
            if _is_excluded(rel) or not _is_text_file(child):
                continue
            files.append(child)
    return sorted(files)


def check_required_docs(root: Path = ROOT) -> list[str]:
    docs = root / "docs"
    missing = [p for p in REQUIRED_DOCS if not (docs / p).exists()]
    return [f"missing required docs: {missing}"] if missing else []


def check_make_targets(root: Path = ROOT) -> list[str]:
    make = _read_text(root / "Makefile")
    missing = [target for target in REQUIRED_MAKE_TARGETS if target not in make]
    return [f"missing Makefile target {target}" for target in missing]


def _matches_acceptance(
    hit: AcceptedHit,
    rel: str,
    term: ForbiddenTerm,
    line_no: int,
    line: str,
    span: tuple[int, int],
) -> bool:
    if hit.path != rel or hit.term != term.name or hit.line_pattern.search(line) is None:
        return False
    if hit.line_number is not None and hit.line_number != line_no:
        return False
    occurrence_pattern = hit.occurrence_pattern or term.pattern
    for occurrence in occurrence_pattern.finditer(line):
        if occurrence.start() <= span[0] and occurrence.end() >= span[1]:
            return True
    return False


def scan_forbidden_terms(
    root: Path = ROOT,
    terms: Iterable[ForbiddenTerm] = FORBIDDEN_TERMS,
    accepted_hits: Iterable[AcceptedHit] = ACCEPTED_HITS,
    scan_roots: Iterable[str] = SCAN_ROOTS,
) -> tuple[list[Finding], list[str]]:
    accepted = list(accepted_hits)
    invalid_reasons = [
        f"{hit.path}:{hit.term}: invalid accepted-hit reason {hit.reason}"
        for hit in accepted
        if hit.reason not in VALID_REASONS
    ]
    used_hits: set[AcceptedHit] = set()
    findings: list[Finding] = []

    for path in iter_scan_files(root, scan_roots):
        rel = _relative(root, path)
        text = _read_text(path)
        for line_no, line in enumerate(text.splitlines(), start=1):
            used_line_hits: set[AcceptedHit] = set()
            for term in terms:
                for match in term.pattern.finditer(line):
                    matches = [
                        hit
                        for hit in accepted
                        if hit not in used_line_hits and _matches_acceptance(hit, rel, term, line_no, line, match.span())
                    ]
                    if matches:
                        used_line_hits.add(matches[0])
                        used_hits.add(matches[0])
                    else:
                        findings.append(Finding(rel, line_no, match.start() + 1, term.name, line))

    stale_hits = sorted(
        {
            f"{hit.path}:{hit.term}: stale accepted-hit metadata ({hit.reason})"
            for hit in accepted
            if hit not in used_hits
        }
    )
    return findings, invalid_reasons + stale_hits


def check_guardrails(root: Path = ROOT) -> list[str]:
    errors = []
    errors.extend(check_required_docs(root))
    errors.extend(check_make_targets(root))
    findings, metadata_errors = scan_forbidden_terms(root)
    if findings:
        errors.append("forbidden stale legacy/private public aliases found:\n" + "\n".join(f.render() for f in findings))
    if metadata_errors:
        errors.append("accepted-hit metadata errors:\n" + "\n".join(metadata_errors))
    return errors


def main() -> None:
    errors = check_guardrails(ROOT)
    if errors:
        raise SystemExit("\n\n".join(errors))
    print("docs-guardrails: ok")


if __name__ == "__main__":
    main()
