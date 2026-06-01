# Engineering Principles

These principles are mandatory for `kkachi-agent-network` design, implementation, review, and future maintenance.

## Core motto

Build the system correctly, not merely minimally. Problems must be understood at the root and fixed at the proper boundary.

## Principles

### Clean Architecture

- Keep domain logic independent from CLI, daemon transport, storage, and member runner details.
- The debate/delegation state machines belong in the domain layer.
- SQLite, JSONL, sockets, plugin tools, CLI commands, and subprocess invocation are adapters. (External delivery — Telegram, Slack, Discord, etc. — is not part of the daemon at all; it lives in the Hermes plugin/gateway helper or equivalent moderator runtime gateway skill, which records its results back through typed KAN commands.)
- Adapters must not own policy decisions.

### Single Responsibility Principle

Each module should have one reason to change.

Examples:

- Registry loader validates member metadata only.
- Runner invokes member wrappers only.
- Storage appends events and maintains projections only.
- Engine decides valid transitions and policy outcomes only.
- Transcript renderer renders events only.

### Maintainability, extensibility, performance

Every meaningful design decision must consider:

1. maintainability
2. future feature extension
3. performance and operational cost

The preferred design is the one that balances all three for the product direction, not the one with the smallest immediate diff.

### Optimal change over minimal change

Do not patch symptoms just to make the current case pass. Fix the real cause at the correct layer, while keeping the change reviewable.

### Root-cause first

Before changing behavior, inspect the actual code path, protocol state, storage record, adapter boundary, or runtime output that caused the problem.

### No masking failures

Do not hide invalid state, missing authority, broken protocol, or failed dispatch behind a superficial fallback.

### Fallback only with explicit user permission

Fallback behavior is allowed only when the user has approved it or when the product contract explicitly defines it.

If fallback is approved, it must be visible in events and reports.

### Fail closed by default

When data, authority, protocol state, lifecycle status, or invariants are invalid, the system must stop the affected operation and report a clear blocked/error state.

Examples:

- Missing registry: do not dispatch.
- Unknown member wrapper: do not substitute another member.
- Broken event log: stop writes and require recovery.
- Block vote in council: do not finalize.
- User-authority question in delegation: enter `waiting_user`, do not guess.

### Auditability

All important decisions, fallbacks, failures, escalations, interventions, votes, review findings, and acceptances must be recorded in `channel.jsonl`.

### Review standard

Code review must check:

- architecture boundary violations
- SRP violations
- hidden fallback behavior
- fail-open behavior
- insufficient root-cause analysis
- missing event/audit records
- performance risks in daemon loops, subprocess handling, and transcript generation
- security invariant violations (see `12-security.md`): unchecked wrapper paths, `shell=True`, unsanitized environment, missing redaction, artifact paths escaping the workspace, unsafe registry file handling (registry symlinks, group/world writable registry files, unsafe data home permissions, owner mismatch, parsing a different file than the one validated, missing registry snapshot, non-atomic snapshot writes)
- operational contract violations (see `13-operational-contracts.md`): missing `schema_version` or `correlation_id`; missing `command_id` where required (CLI-originated or daemon-follow-CLI events); missing `causation_event_id` when `command_id` is null; runner-accounted events missing `runner` metadata; terminal runner events missing a `cost` field; using `cost.source` as the runner invocation marker; failure to count runner calls when `cost: null`; missing durable runner failure or discard records; missing budget breach events; idempotency bypass; operational events not carrying `session_id`; session creation without a frozen `registry_snapshot.yaml`; using live `registry.yaml` to reinterpret an existing session. (`cost: null` is allowed only as an explicit missing-cost record for terminal runner events; it must remain visible in projections and reports.)
- hard-coded constants that should live in session limits (timeouts, token caps, budget ceilings)
- timeout classes applied to the wrong boundary (dispatch vs. research vs. clarification vs. escalation)
- missing observability for critical daemon loops, stream lag, append latency, replay duration, runner failures, and blocked-session age (see `16-observability.md`)
- missing disaster recovery procedure for storage corruption, projection mismatch, registry snapshot failure, or active-session lock mismatch (see `17-disaster-recovery.md`)
- missing tests for command idempotency, fake runner behavior, stream cursor replay, blocked-state recovery (`session_resumed` and `limits_extended`), structured JSON errors, and projection rebuild (see `18-testing-strategy.md`)
- undocumented changes to the developer toolchain or package layout; update `19-tooling.md` before changing Python version, build backend, test runner, lint/format tool, or type checker
