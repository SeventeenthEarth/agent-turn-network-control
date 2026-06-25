#!/usr/bin/env python3
"""LTRAN-003 disposable live-local CLI/daemon smoke.

This script builds local temp binaries, starts hund with a
script-owned disposable data home, exercises daemon-backed CLI reads, stream
replay/follow/ack/status, and delegate.submit idempotency/conflict, then writes
redacted evidence. It never mutates the sibling plugin repo or production data.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any

SCRUBBED_ENV_PREFIXES = ("KAB_", "GATEWAY_", "PROVIDER_", "PROFILE_", "DISCORD_", "HERMES_")
SCRUBBED_ENV_NAMES = {
    "DISCORD_TOKEN",
    "HERMES_TOKEN",
    "ANTHROPIC_API_KEY",
    "OPENAI_API_KEY",
    "HUN_HOME",
    "HUND_PATH",
    "KKACHI_AGENT_NETWORK_HOME",
    "KKACHI_AGENT_NETWORKD_PATH",
}


def repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def is_disposable_path(path: Path) -> bool:
    resolved = path.resolve()
    root = repo_root().resolve()
    home = Path.home().resolve()
    allowed = [Path(tempfile.gettempdir()).resolve(), Path("/tmp").resolve(), Path("/private/tmp").resolve()]
    try:
        if resolved == root or root in resolved.parents:
            return False
        if resolved == home or home in resolved.parents:
            return False
        sibling_plugin = root.parent / "agent-turn-network-plugin"
        if sibling_plugin.exists():
            plugin_resolved = sibling_plugin.resolve()
            if resolved == plugin_resolved or plugin_resolved in resolved.parents:
                return False
    except RuntimeError:
        return False
    return any(resolved == base or base in resolved.parents for base in allowed)


def make_env(data_home: Path, daemon_path: Path) -> tuple[dict[str, str], list[str]]:
    scrubbed: list[str] = []
    env: dict[str, str] = {}
    keep = {"PATH", "TMPDIR", "GOCACHE", "GOMODCACHE"}
    for key, value in os.environ.items():
        if key in SCRUBBED_ENV_NAMES or key.startswith(SCRUBBED_ENV_PREFIXES):
            scrubbed.append(key)
            continue
        if key in keep:
            env[key] = value
    env.setdefault("PATH", os.environ.get("PATH", "/usr/bin:/bin:/usr/sbin:/sbin"))
    env.setdefault("TMPDIR", tempfile.gettempdir())
    env.setdefault("GOCACHE", "/tmp/kkachi-go-build-cache")
    env["HOME"] = str(data_home / "fake-home")
    env["HUN_HOME"] = str(data_home)
    env["HUND_PATH"] = str(daemon_path)
    env["KAN_EXTERNAL"] = "0"
    return env, sorted(set(scrubbed))


def write_registry(data_home: Path) -> None:
    registry = """schema_version: 1
members:
  agent-mod:
    display_name: LTRAN Moderator
    wrapper: missing-ltran-moderator-wrapper
    workspace: /tmp/ltran-agent-mod
    role: moderator
    enabled: false
    adapter_kind: hermes-agent
  agent-1:
    display_name: LTRAN Agent One
    wrapper: missing-ltran-agent-1-wrapper
    workspace: /tmp/ltran-agent-1
    role: assignee
    enabled: false
    adapter_kind: hermes-agent
"""
    (data_home / "fake-home").mkdir(mode=0o700, parents=True, exist_ok=True)
    (data_home / "registry.yaml").write_text(registry)
    os.chmod(data_home / "registry.yaml", 0o600)


def decode_stdout(stdout: str, ndjson: bool = False) -> Any:
    if not stdout.strip():
        return None
    if ndjson:
        return [json.loads(line) for line in stdout.splitlines() if line.strip()]
    return json.loads(stdout)


class Smoke:
    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.root = repo_root()
        self.created_data_home = False
        if args.data_home:
            self.data_home = Path(args.data_home).expanduser().resolve()
            if self.data_home.exists() and not args.allow_existing_disposable:
                raise SystemExit("--data-home exists; refusing unless --allow-existing-disposable is set")
            self.data_home.mkdir(mode=0o700, parents=True, exist_ok=True)
        else:
            self.data_home = Path(tempfile.mkdtemp(prefix="kan-ltran-003-", dir="/tmp")).resolve()
            self.created_data_home = True
        if not is_disposable_path(self.data_home):
            raise SystemExit(f"refusing non-disposable data home: {self.data_home}")
        os.chmod(self.data_home, 0o700)
        self.bin_dir = Path(tempfile.mkdtemp(prefix="kan-ltran-003-bin-", dir="/tmp")).resolve()
        self.cli = self.bin_dir / "hun"
        self.daemon = self.bin_dir / "hund"
        self.env, self.scrubbed = make_env(self.data_home, self.daemon)
        self.commands: list[dict[str, Any]] = []
        self.checks: list[str] = []
        self.errors: list[str] = []
        self.stopped = False
        self.daemon_proc: subprocess.Popen[str] | None = None
        self.daemon_record: dict[str, Any] | None = None

    def run(self, argv: list[str], *, expect: int = 0, ndjson: bool = False, timeout: float = 10.0) -> dict[str, Any]:
        proc = subprocess.run(argv, cwd=self.root, env=self.env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
        record: dict[str, Any] = {
            "args": [Path(argv[0]).name if i == 0 and Path(argv[0]).parent == self.bin_dir else value for i, value in enumerate(argv)],
            "exit_code": proc.returncode,
            "stdout": proc.stdout,
            "stderr": proc.stderr,
        }
        try:
            record["parsed_stdout"] = decode_stdout(proc.stdout, ndjson=ndjson)
        except Exception as exc:  # evidence should retain raw output if JSON parsing fails
            record["stdout_parse_error"] = str(exc)
        try:
            record["parsed_stderr"] = decode_stdout(proc.stderr)
        except Exception:
            pass
        self.commands.append(record)
        if proc.returncode != expect:
            raise AssertionError(f"{argv} exit {proc.returncode}, want {expect}; stdout={proc.stdout!r} stderr={proc.stderr!r}")
        return record

    def build(self) -> None:
        self.run(["go", "build", "-o", str(self.cli), "./cmd/hun"], timeout=60)
        self.run(["go", "build", "-o", str(self.daemon), "./cmd/hund"], timeout=60)
        self.checks.append("built temp CLI and daemon binaries")

    def start_daemon(self) -> None:
        socket_path = self.data_home / "run" / "hund.sock"
        self.daemon_proc = subprocess.Popen([str(self.daemon), "run"], cwd=self.root, env=self.env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.daemon_record = {"args": ["hund", "run"], "exit_code": None, "stdout": "", "stderr": "", "started": True}
        self.commands.append(self.daemon_record)
        deadline = time.monotonic() + 5.0
        while time.monotonic() < deadline:
            if self.daemon_proc.poll() is not None:
                stdout, stderr = self.daemon_proc.communicate(timeout=1)
                self.daemon_record.update({"exit_code": self.daemon_proc.returncode, "stdout": stdout, "stderr": stderr})
                try:
                    self.daemon_record["parsed_stderr"] = decode_stdout(stderr)
                except Exception:
                    pass
                raise AssertionError(f"daemon exited before ready: {self.daemon_record}")
            if socket_path.exists():
                self.checks.append(f"socket under disposable data home: {socket_path}")
                return
            time.sleep(0.05)
        raise AssertionError(f"daemon socket not under disposable data home: {socket_path}")

    def exercise(self) -> None:
        write_registry(self.data_home)
        self.build()
        self.start_daemon()

        for sub in ("version", "status", "diagnostics"):
            out = self.run([str(self.cli), "compat", sub, "--format", "json"])["parsed_stdout"]
            for key in ("schema_version", "protocol_version", "daemon_version", "min_plugin_protocol_version", "feature_groups"):
                if key not in out:
                    raise AssertionError(f"compat {sub} missing {key}: {out}")
            for feature in ("version.read", "status.read", "diagnostics.read", "stream.replay", "stream.ack", "stream.status"):
                if feature not in out.get("feature_groups", []) and feature not in out.get("features", []):
                    raise AssertionError(f"compat {sub} missing feature {feature}")
        self.checks.append("daemon-backed compat version/status/diagnostics reads expose protocol evidence")

        for argv in ([str(self.cli), "daemon", "status"], [str(self.cli), "status"], [str(self.cli), "daemon", "health"]):
            out = self.run(argv)["parsed_stdout"]
            if "protocol_version" in out:
                raise AssertionError(f"operator command grew compat fields: {argv} -> {out}")
        self.checks.append("operator status/health remain concise")

        self.run([str(self.cli), "delegate", "new", "sess_ltran_003", "--moderator", "agent-mod", "--assignee", "agent-1", "--title", "LTRAN-003 disposable proof", "--task", "prove live-local write path", "--event-id", "evt_ltran003_created", "--assignment-event-id", "evt_ltran003_assigned", "--command-id", "cmd_ltran003_new"])
        replay = self.run([str(self.cli), "stream", "sess_ltran_003", "--member", "agent-1", "--from-start", "--format", "ndjson"], ndjson=True)["parsed_stdout"]
        if not replay or not all(frame.get("is_replay") for frame in replay):
            raise AssertionError(f"replay frames not marked replay: {replay}")

        follow_proc = subprocess.Popen([str(self.cli), "stream", "sess_ltran_003", "--member", "agent-1", "--since", "cur_000000000000_evt_ltran003_created", "--follow", "--follow-timeout-ms", "1200", "--follow-poll-ms", "10", "--format", "ndjson"], cwd=self.root, env=self.env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        time.sleep(0.1)
        self.run([str(self.cli), "delegate", "ack", "sess_ltran_003", "--actor", "agent-1", "--understanding", "ready", "--command-id", "cmd_ltran003_ack"])
        follow_stdout, follow_stderr = follow_proc.communicate(timeout=5)
        follow_record = {"args": ["hun", "stream", "sess_ltran_003", "--member", "agent-1", "--since", "cur_000000000000_evt_ltran003_created", "--follow", "--follow-timeout-ms", "1200", "--follow-poll-ms", "10", "--format", "ndjson"], "exit_code": follow_proc.returncode, "stdout": follow_stdout, "stderr": follow_stderr, "parsed_stdout": decode_stdout(follow_stdout, ndjson=True)}
        self.commands.append(follow_record)
        if follow_proc.returncode != 0:
            raise AssertionError(f"follow failed: {follow_record}")
        if not any(not frame.get("is_replay") and frame.get("event", {}).get("event_id") == "evt_assignee_acknowledged_cmd_ltran003_ack" for frame in follow_record["parsed_stdout"]):
            raise AssertionError(f"follow did not emit live ack frame: {follow_record['parsed_stdout']}")
        self.checks.append("stream replay and bounded follow emitted replay before live appended event")

        self.run([str(self.cli), "stream", "ack", "sess_ltran_003", "--member", "agent-1", "--cursor", "cur_000000000000_evt_ltran003_created", "--command-id", "cmd_ltran003_ack_cursor"])
        status = self.run([str(self.cli), "stream", "status", "sess_ltran_003"])["parsed_stdout"]
        if "cmd_ltran003_ack_cursor" not in json.dumps(status) or "cur_000000000000_evt_ltran003_created" not in json.dumps(status):
            raise AssertionError(f"stream status missing ack evidence: {status}")
        self.run([str(self.cli), "stream", "sess_ltran_003", "--member", "agent-1", "--since", "cur_999999999999_evt_gap", "--format", "ndjson"], expect=70)
        self.run([str(self.cli), "stream", "sess_ltran_003", "--member", "agent-1", "--from-start", "--format", "ndjson", "--teleport"], expect=1)
        self.checks.append("stream ack/status plus gap and unsupported-option fail-closed checks passed")

        first = self.run([str(self.cli), "delegate", "submit", "sess_ltran_003", "--actor", "agent-1", "--summary", "first submit", "--command-id", "cmd_ltran003_submit"])["parsed_stdout"]
        duplicate = self.run([str(self.cli), "delegate", "submit", "sess_ltran_003", "--actor", "agent-1", "--summary", "first submit", "--command-id", "cmd_ltran003_submit"])["parsed_stdout"]
        conflict = self.run([str(self.cli), "delegate", "submit", "sess_ltran_003", "--actor", "agent-1", "--summary", "different submit", "--command-id", "cmd_ltran003_submit"], expect=70)
        if first.get("deduplicated") is not False or duplicate.get("deduplicated") is not True:
            raise AssertionError(f"submit idempotency failed: first={first} duplicate={duplicate}")
        if "command_id already used with different payload" not in conflict.get("stderr", ""):
            raise AssertionError(f"submit conflict missing structured message: {conflict}")
        self.checks.append("delegate.submit first write, exact retry dedupe, and command-id conflict passed")

    def stop(self) -> None:
        if self.stopped:
            return
        self.stopped = True
        try:
            self.run([str(self.cli), "daemon", "stop"], timeout=5)
        except Exception as exc:
            self.errors.append(f"daemon stop cleanup failed: {exc}")
        if self.daemon_proc is not None:
            try:
                stdout, stderr = self.daemon_proc.communicate(timeout=5)
                if self.daemon_record is not None:
                    self.daemon_record.update({"exit_code": self.daemon_proc.returncode, "stdout": stdout, "stderr": stderr})
            except subprocess.TimeoutExpired:
                self.daemon_proc.kill()
                stdout, stderr = self.daemon_proc.communicate(timeout=2)
                if self.daemon_record is not None:
                    self.daemon_record.update({"exit_code": self.daemon_proc.returncode, "stdout": stdout, "stderr": stderr, "killed_after_stop_timeout": True})
                self.errors.append("daemon process did not exit after stop and was killed")

    def evidence_path(self) -> Path:
        if self.args.evidence_path:
            return Path(self.args.evidence_path).resolve()
        if self.args.run_id:
            return self.root / ".kkachi" / "runs" / self.args.run_id / "ltran-003-live-local-evidence.json"
        return self.data_home / "ltran-003-live-local-evidence.json"

    def write_evidence(self, passed: bool) -> Path:
        path = self.evidence_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        evidence = {
            "schema_version": 1,
            "task": "control/LTRAN-003",
            "run_id": self.args.run_id,
            "passed": passed,
            "repo": str(self.root),
            "data_home": str(self.data_home),
            "disposable_data_home": is_disposable_path(self.data_home),
            "daemon_socket": str(self.data_home / "run" / "hund.sock"),
            "production_activation_claim": False,
            "plugin_repo_mutated": False,
            "live_service_contact": False,
            "env_summary": {
                "scrubbed_live_env_names": self.scrubbed,
                "home_overridden_to_disposable_fake_home": True,
                "kan_external": self.env.get("KAN_EXTERNAL"),
            },
            "checks": self.checks,
            "errors": self.errors,
            "commands": self.commands,
        }
        path.write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n")
        return path

    def cleanup(self) -> None:
        shutil.rmtree(self.bin_dir, ignore_errors=True)
        if self.created_data_home and not self.args.keep_data_home:
            shutil.rmtree(self.data_home, ignore_errors=True)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run LTRAN-003 disposable live-local smoke and write redacted evidence.")
    parser.add_argument("--run-id", help="KAH run id; writes evidence under .kkachi/runs/<run-id>/ by default")
    parser.add_argument("--data-home", help="Optional disposable temp data home to use")
    parser.add_argument("--allow-existing-disposable", action="store_true", help="Allow an existing temp data home after disposable-path validation")
    parser.add_argument("--evidence-path", help="Override evidence output path")
    parser.add_argument("--keep-data-home", action="store_true", help="Keep script-created disposable data home for debugging")
    args = parser.parse_args()

    smoke = Smoke(args)
    passed = False
    evidence = None
    try:
        smoke.exercise()
        passed = True
        return_code = 0
    except Exception as exc:
        smoke.errors.append(str(exc))
        return_code = 1
    finally:
        smoke.stop()
        evidence = smoke.write_evidence(passed)
        smoke.cleanup()
    print(json.dumps({"passed": passed, "evidence_path": str(evidence)}, sort_keys=True))
    return return_code


if __name__ == "__main__":
    sys.exit(main())
