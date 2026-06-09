# Disaster Recovery

## Scope

This document defines how an operator backs up, restores, and recovers `kkachi-agent-network` state. It covers `channel.jsonl` corruption, SQLite projection corruption, registry snapshot loss, active-session lock mismatch, and unsafe data home permissions. The goal is to bring the daemon back to a verifiable state without inventing events, replacing real member runtimes, or weakening the security model.

The normative SOT documents that disaster recovery depends on:

- Source-of-truth boundary: `channel.jsonl` is durable; SQLite is a projection (`05-storage-schema.md`, `13-operational-contracts.md` §2).
- Replay determinism: replay is side-effect free (`13-operational-contracts.md` §2).
- Security model: `12-security.md`.
- CLI commands: `04-cli-spec.md`.

## Recovery principles

- `channel.jsonl` is the only file whose content cannot be regenerated. Protect it first.
- Projections (`network.sqlite`, `event_recipients`, `runner_invocations`, `escalation_batches`, `escalation_batch_items`, `stream_cursors`, `stream_subscribers`, `delegation_reviews`, `council_hand_raises`, `council_votes`, `commands_seen`, `artifacts`) are derived from events and can be rebuilt.
- Replay must not invoke runners, deliver escalations, or invent events because a deadline passed.
- Registry must not be reinterpreted for historical sessions; per-session `registry_snapshot.yaml` is authoritative.
- Active-session state is recovered from the recorded lifecycle events, not from stale metadata or runtime lock artifacts.

## What must be backed up

| Path                                   | Backup required | Reason                                          |
| -------------------------------------- | --------------: | ----------------------------------------------- |
| `sessions/<id>/channel.jsonl`          |             Yes | Event SOT                                       |
| `sessions/<id>/registry_snapshot.yaml` |             Yes | Session participant authority                   |
| `sessions/<id>/artifacts/`             |             Yes | Submitted artifacts referenced by events        |
| `sessions/<id>/session.yaml`           |             Yes | Session metadata used during recovery           |
| `registry.yaml`                        |             Yes | Live registry SOT (used for new sessions)       |
| `network.sqlite`                       |        Optional | Projection, rebuildable from event log          |
| `operational.log`                      |        Optional | Pre-session failure audit (2-day retention)     |
| `raw_logs/`                            |        Optional | Operational artifact, short retention           |
| `runtime/<member>/stream_cursor`       |        Optional | Recoverable from `stream_cursor_acknowledged`   |
| `active_session.lock` / runtime lock artifacts |              No | Recoverable from session lifecycle events       |
| `run/kkachi-agent-networkd.sock`              |              No | Process socket; recreated by daemon start       |

Backups should preserve file mode and ownership where possible. After restore, ownership and modes must satisfy the rules in `12-security.md`; `kkachi-agent-network doctor` (with `--repair-permissions`) or `kkachi-agent-network init` may correct them with explicit reporting.

## What can be rebuilt

- `network.sqlite` and every projection table — rebuilt by `kkachi-agent-network storage rebuild-projection`.
- `transcript.md` and `brief.md` — regenerated from `channel.jsonl` (`05-storage-schema.md#retention`).
- `event_recipients`, `runner_invocations`, `escalation_batches`, `escalation_batch_items`, `commands_seen`, `artifacts` projections — rebuilt by replay.
- `stream_cursor` files — rebuilt from `stream_cursor_acknowledged` events.

## Backup procedure

1. Stop the daemon (`kkachi-agent-network daemon stop`) before copying files. This avoids racing with appends.
2. Copy `<data_home>` recursively, preserving permissions and ownership.
3. Verify the copy by running `kkachi-agent-network storage verify --data-home <backup_path>` against the copy (when supported by the implementation) or by inspecting `channel.jsonl` parseability.
4. Encrypt the backup if the destination is not on the same trust boundary as `<data_home>`. Backups inherit the same secret hygiene rules: artifacts may carry redacted user content, and `registry_snapshot.yaml` carries member identity.
5. Restart the daemon (`kkachi-agent-network daemon start`).

A live backup (without stopping the daemon) is acceptable only when the implementation provides a snapshot mechanism that guarantees a consistent `channel.jsonl` view; otherwise prefer a brief stop-and-copy.

## Restore procedure

1. Stop the daemon if it is running.
2. Place the backup at the desired `<data_home>` path.
3. Verify file ownership and permissions match the rules in `12-security.md` (data home `0700` recommended, registry `0600` recommended, owner is the daemon user). Use `kkachi-agent-network doctor` to inspect; use `kkachi-agent-network doctor --repair-permissions` or `kkachi-agent-network init` to fix with explicit reporting.
4. Run `kkachi-agent-network storage verify`.
5. Run `kkachi-agent-network storage rebuild-projection` if any projection is missing or fails verification.
6. Start the daemon.
7. Run `kkachi-agent-network doctor` and `kkachi-agent-network status` to confirm the active session and phase match expectations.

## Projection rebuild

Projection rebuild is the most common recovery operation. It is replay applied to projection state.

```text
1. Stop the daemon.
2. Run `kkachi-agent-network storage verify`.
3. If verification reports only a missing, corrupt, or mismatched projection, run `kkachi-agent-network storage rebuild-projection`; rebuild performs its own event-log preflight before replacement.
4. Start the daemon.
5. Run `kkachi-agent-network doctor`.
6. Confirm active session status and phase.
```

The rebuild must:

- read `channel.jsonl` from offset 0;
- apply registered schema migrations in order;
- rebuild every projection table from events;
- not invoke runners, not deliver escalations, not append events, and not invent timer-driven flushes;
- not reread live `registry.yaml` to reinterpret historical sessions.

If verification fails because `channel.jsonl` is corrupt, schema migration is missing, or the data home/projection path is unsafe, do not rebuild until the relevant safety or corruption recovery procedure below is complete.

## channel.jsonl corruption

Symptoms: `kkachi-agent-network storage verify` reports parse error, duplicate `event_id`, schema gap, or `migration_required`.

Procedure:

1. Stop the daemon.
2. Copy the corrupted `channel.jsonl` to a quarantine path before any repair attempt; do not overwrite it.
3. Inspect the failing line(s) and the surrounding context. Determine whether corruption is at the tail (truncated final line) or in the middle.
4. Tail truncation: if the final line is partial JSON, truncate the file at the last newline-terminated valid line. Document the action in `operational.log` manually (operator action, not a session event).
5. Mid-file corruption: do not silently delete events. Restore from the most recent verified backup, then replay any events that arrived after the backup point only if they exist as a separate, verifiable record (rare). If no verifiable record exists, mark the affected session as unrecoverable through `kkachi-agent-network cancel <session_id>` after restoring from backup.
6. Run `kkachi-agent-network storage verify` and `kkachi-agent-network storage rebuild-projection`.
7. Start the daemon and verify session state.

The daemon must never auto-skip events to keep replay alive.

## SQLite corruption

Symptoms: `kkachi-agent-network status` returns inconsistent values, projection queries fail, or `kkachi-agent-network storage verify` reports projection mismatch.

Procedure:

1. Stop the daemon.
2. Move the corrupted `network.sqlite` aside (do not delete until rebuild succeeds).
3. Run `kkachi-agent-network storage rebuild-projection` to rebuild from `channel.jsonl`.
4. Start the daemon and verify status.

Because SQLite is a projection, a clean rebuild is always safe as long as `channel.jsonl` is intact.

## active-session lock mismatch

Symptoms: runtime lock artifacts or `session.yaml` claim a session is active but the recorded lifecycle events show terminal state, or vice versa.

Procedure:

1. Stop the daemon.
2. Inspect the latest events for the suspected session in `channel.jsonl`. The lifecycle events (`session_created`, `session_cancelled`, `work_accepted`, `council_finalized`, `council_unresolved`) are the truth.
3. If lifecycle events show a terminal phase, remove stale runtime lock artifacts and trust replay-derived terminal state.
4. If lifecycle events show a non-terminal phase but the runtime lock is missing, recreate the lock from the recorded session id during daemon start. Replay-derived active-session discovery uses the latest durable event rather than stale `session.yaml` phase/status.
5. Start the daemon.
6. Run `kkachi-agent-network status` to confirm the lock matches the recorded state.

## registry_snapshot.yaml missing or corrupt

Symptoms: dispatch fails with a session-scoped registry violation; `kkachi-agent-network storage verify` reports replay failure for a missing, unsafe, empty, or corrupt session snapshot.

Procedure:

1. Stop the daemon.
2. Restore `sessions/<session_id>/registry_snapshot.yaml` from the most recent backup.
3. If no backup exists, the session cannot be rebuilt as valid local release evidence. Replay must not regenerate the snapshot from the live `registry.yaml` (per `12-security.md` and `13-operational-contracts.md` §2).
4. Run `kkachi-agent-network storage verify` and start the daemon.
5. Cancel the session through `kkachi-agent-network cancel <session_id>` if the snapshot cannot be restored.

## Unsafe data home or registry permissions

Symptoms: daemon refuses to start with a category from `12-security.md` (e.g. `registry_data_home_unsafe`, `registry_permissions_unsafe`, `registry_owner_unsafe`).

Procedure:

1. Stop the daemon (if running).
2. Run `kkachi-agent-network doctor` to enumerate the unsafe paths and recommended fixes.
3. Run `kkachi-agent-network doctor --repair-permissions` or `kkachi-agent-network init` to apply fixes; both must report every change.
4. Re-run `kkachi-agent-network doctor` to confirm.
5. Start the daemon.

Daemon start must not silently chmod or chown anything.

## Restore to a new data_home

Operators sometimes need to move `<data_home>` to a new path or host.

Procedure:

1. Stop the daemon.
2. Copy `<data_home>` to the new path with permissions preserved.
3. Set `$KKACHI_AGENT_NETWORK_HOME` to the new path or leave the resolution order alone if the new path follows `$XDG_DATA_HOME/kkachi-agent-network` or `~/.kkachi-agent-network/`.
4. Run `kkachi-agent-network doctor` against the new path.
5. Run `kkachi-agent-network storage verify` and `kkachi-agent-network storage rebuild-projection` if needed.
6. Start the daemon.

Per-session `registry_snapshot.yaml` is portable; the live `registry.yaml` may need adjustment if member workspace paths changed.

## Verification checklist

After any recovery operation, verify:

- [ ] `kkachi-agent-network doctor` reports no unsafe paths.
- [ ] `kkachi-agent-network daemon status` reports ready.
- [ ] `kkachi-agent-network storage verify` passes.
- [ ] `kkachi-agent-network status` matches the expected active session and phase.
- [ ] `kkachi-agent-network limits show <session_id>` returns sane runner accounting and escalation counters.
- [ ] No `security_violation` events were created during recovery.
- [ ] `operational.log` records the recovery action (manual entry by the operator if no automated event exists).

## Non-goals

- Disaster recovery must not reinterpret historical sessions using the live registry.
- Disaster recovery must not invent missing events or synthesize replacements for corrupted lines.
- Disaster recovery must not rerun member wrappers or deliver escalations during replay.
- Disaster recovery must not overwrite `channel.jsonl` to "clean it up". The original corrupted file is preserved for forensic review.
- The daemon must not become a backup orchestrator; backup scheduling is an operator concern.

## Appendix: Schema migration example

Schema migrations are part of replay (`13-operational-contracts.md` §2). They are pure functions that transform a single event envelope. They are relevant to disaster recovery because a backup may contain events at an older `schema_version` and a new daemon must run the migration chain before projection rebuild succeeds.

### Example: schema_version 1 to 2

Migration module path:

```text
internal/storage/migrations/m_001_to_002.py
```

Rules:

- The migration is a pure function.
- It transforms one event envelope.
- It must not append events.
- It must not drop events.
- It must not call runners.
- It must not read the live registry.

Example code:

```python
def migrate(event: dict) -> dict:
    migrated = dict(event)
    migrated["schema_version"] = 2
    payload = dict(migrated.get("payload") or {})
    if migrated.get("type") == "example_event":
        payload["new_field"] = payload.get("new_field", None)
    migrated["payload"] = payload
    return migrated
```

Discovery is by file presence: at startup the daemon enumerates every `m_*.py` file and builds the chain. A gap (e.g. `m_001_to_002.py` and `m_003_to_004.py` present but `m_002_to_003.py` missing) is a startup error. Replay halts with `migration_required` if the chain cannot reach the daemon's current `schema_version`.
