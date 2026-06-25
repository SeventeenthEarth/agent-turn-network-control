from __future__ import annotations

from pathlib import Path
import re
import tempfile
import unittest

import guardrails


def write(root: Path, rel: str, text: str) -> None:
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


class GuardrailsTest(unittest.TestCase):
    def test_canonical_atn_terms_pass(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write(
                root,
                "README.md",
                "Agent Turn Network ATN atn-control atn-controld ATN_HOME ~/.atn atn-controld.sock atn-protocol-v1alpha0\n",
            )

            findings, metadata_errors = guardrails.scan_forbidden_terms(
                root,
                accepted_hits=[],
                scan_roots=["README.md"],
            )

            self.assertEqual(findings, [])
            self.assertEqual(metadata_errors, [])

    def test_synthetic_stale_legacy_term_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write(root, "README.md", "Install the old kkachi-agent-network command.\n")

            findings, metadata_errors = guardrails.scan_forbidden_terms(
                root,
                accepted_hits=[],
                scan_roots=["README.md"],
            )

            self.assertEqual(metadata_errors, [])
            self.assertEqual(len(findings), 1)
            self.assertEqual(findings[0].term, "kkachi-agent-network")

    def test_accepted_hits_require_matching_metadata(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write(root, "docs/history.md", "Legacy `kan-*` labels are historical/provenance labels.\n")
            accepted_hit = guardrails.AcceptedHit(
                "docs/history.md",
                "kan-* public label",
                re.compile(r"Legacy `kan-\*` labels are historical/provenance labels\."),
                "historical/provenance",
                re.compile(r"`kan-\*`"),
            )

            findings, metadata_errors = guardrails.scan_forbidden_terms(
                root,
                accepted_hits=[accepted_hit],
                scan_roots=["docs"],
            )

            self.assertEqual(findings, [])
            self.assertEqual(metadata_errors, [])

    def test_accepted_hits_reject_wrong_context(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write(root, "docs/history.md", "Use kan-old-command as a public alias.\n")
            accepted_hit = guardrails.AcceptedHit(
                "docs/history.md",
                "kan-* public label",
                re.compile(r"historical/provenance"),
                "historical/provenance",
                re.compile(r"`kan-\*`"),
            )

            findings, metadata_errors = guardrails.scan_forbidden_terms(
                root,
                accepted_hits=[accepted_hit],
                scan_roots=["docs"],
            )

            self.assertEqual(len(findings), 1)
            self.assertIn("stale accepted-hit metadata", "\n".join(metadata_errors))

    def test_accepted_hit_reason_must_be_known(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write(root, "docs/history.md", "Legacy `kan-*` labels are historical/provenance labels.\n")
            accepted_hit = guardrails.AcceptedHit(
                "docs/history.md",
                "kan-* public label",
                re.compile(r"historical/provenance"),
                "broad-docs-exception",
                re.compile(r"`kan-\*`"),
            )

            findings, metadata_errors = guardrails.scan_forbidden_terms(
                root,
                accepted_hits=[accepted_hit],
                scan_roots=["docs"],
            )

            self.assertEqual(findings, [])
            self.assertIn("invalid accepted-hit reason", "\n".join(metadata_errors))

    def test_stale_accepted_hit_metadata_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write(root, "docs/history.md", "No legacy token remains here.\n")
            accepted_hit = guardrails.AcceptedHit(
                "docs/history.md",
                "kan-* public label",
                re.compile(r"historical/provenance"),
                "historical/provenance",
                re.compile(r"`kan-\*`"),
            )

            findings, metadata_errors = guardrails.scan_forbidden_terms(
                root,
                accepted_hits=[accepted_hit],
                scan_roots=["docs"],
            )

            self.assertEqual(findings, [])
            self.assertIn("stale accepted-hit metadata", "\n".join(metadata_errors))

    def test_unapproved_second_forbidden_occurrence_on_accepted_line_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write(root, "docs/history.md", "Legacy `kan-*` labels are historical/provenance labels, but KAN must not reappear.\n")
            accepted_hit = guardrails.AcceptedHit(
                "docs/history.md",
                "kan-* public label",
                re.compile(r"Legacy `kan-\*` labels are historical/provenance labels"),
                "historical/provenance",
                re.compile(r"`kan-\*`"),
            )

            findings, metadata_errors = guardrails.scan_forbidden_terms(
                root,
                accepted_hits=[accepted_hit],
                scan_roots=["docs"],
            )

            self.assertEqual(metadata_errors, [])
            self.assertEqual(len(findings), 1)
            self.assertEqual(findings[0].term, "KAN public label")

    def test_new_kkachi_agent_networkd_line_in_accepted_file_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write(
                root,
                "internal/storage/events.go",
                'const daemonPrincipal = "kkachi-agent-networkd"\n'
                'const stalePublicAlias = "kkachi-agent-networkd"\n',
            )
            accepted_hit = guardrails.AcceptedHit(
                "internal/storage/events.go",
                "kkachi-agent-networkd",
                re.compile(r'const daemonPrincipal = "kkachi-agent-networkd"'),
                "safety-boundary",
                re.compile(r"kkachi-agent-networkd"),
            )

            findings, metadata_errors = guardrails.scan_forbidden_terms(
                root,
                accepted_hits=[accepted_hit],
                scan_roots=["internal"],
            )

            self.assertEqual(metadata_errors, [])
            self.assertEqual(len(findings), 1)
            self.assertEqual(findings[0].term, "kkachi-agent-networkd")


if __name__ == "__main__":
    unittest.main()
