# CLI Specification

Official CLI: `kkachi-agent-network`. The Hermes plugin is the preferred agent-facing integration layer, but this CLI is the canonical diagnostics, recovery, test, and manual-operation contract.

## Principles

- All state mutations go through the daemon.
- Commands are explicit and auditable.
- Status commands are concise by default and verbose on request.
- The CLI should be usable by the moderator agent through terminal tool calls and should remain available when the Hermes plugin is absent, disabled, or broken.
- Hermes plugin tools and slash commands must map to the same typed command models and daemon validations as the CLI. The plugin should call the KAN protocol client/contract directly, not shell out, except as a compatibility fallback or for operator-equivalent diagnostics.

## Global write command rules

Every state-mutating CLI command accepts an optional `--command-id cmd_...`. If `--command-id` is omitted, the CLI generates one. Hermes plugin tool calls that mutate state must provide or obtain the same command-id semantics through the KAN protocol client/contract. The daemon uses `command_id` for idempotency per `13-operational-contracts.md` §2; retrying the same logical command must reuse the same `command_id`.

Every state-mutating command must declare the protocol event type or event sequence it emits. A command may emit more than one event only when this document explicitly lists the emitted sequence (e.g. `delegate new` emits `session_created` followed by `task_assigned`).

Participant-originated events (origin class `participant_cli` in `03-protocol-spec.md`) must have an explicit canonical CLI command path here. If the Hermes plugin exposes the same event, the plugin command/tool must document the equivalent CLI path and emit the same daemon command model. Daemon-originated operational events do not require public write commands.

Ambiguous state-mutating commands are not allowed. In particular: clarification answers, general delegation messages, review verdicts, escalation delivery audit, and council readiness each have explicit commands and must not be conflated.

## Recipient normalization

CLI commands may accept a single `--to` value for convenience, but persisted protocol events always store `to` as `array<string>` (per `03-protocol-spec.md`):

- `--to agent-1` becomes `"to": ["agent-1"]`.
- Repeated `--to agent-1 --to agent-2` becomes `"to": ["agent-1", "agent-2"]`.
- Council `--members agent-1,agent-2,agent-3` expands to explicit recipient lists for broadcast events; the daemon must not emit `"to": ["all"]`, `"to": "*"`, or an omitted `to` field.

The daemon removes duplicate recipients, validates each recipient, and stores recipients in canonical order (session participant order, then the reserved external principal `user` last). Recipient order has no semantic meaning.

Recipient flags:

- `--to <principal>` is repeatable where multi-recipient commands are supported.
- Comma-separated input may be accepted by commands that already use comma-separated member lists (e.g. `--members`), but the daemon must normalize to the same array form.
- Unknown recipients are rejected unless the value is the reserved principal `user`.
- `kkachi-agent-networkd` is not accepted as a normal participant-supplied recipient.

`to` is **semantic addressing**, not stream access control. Stream read permissions are governed by `12-security.md`.

## Daemon commands

```bash
kkachi-agent-network daemon start
kkachi-agent-network daemon stop
kkachi-agent-network daemon status
```

`kkachi-agent-network daemon start` validates `<data_home>` and `registry.yaml` before reporting ready. If validation fails, the daemon does not accept session creation or dispatch commands; the failure is written to `<data_home>/operational.log` per `12-security.md`.

## Setup and diagnostic commands

```bash
kkachi-agent-network init
kkachi-agent-network doctor
kkachi-agent-network storage verify
kkachi-agent-network storage rebuild-projection
```

These commands cover first-run setup, environment validation, and storage recovery. They are operational tools and are not part of the session protocol.

### `kkachi-agent-network init`

Rules:

- Creates `<data_home>` with mode `0700` when missing.
- May create a sample `registry.yaml` with mode `0600` only when the file does not exist.
- Must not overwrite an existing registry.
- Must report every filesystem change it makes.
- Does not start the daemon and does not create sessions.

### `kkachi-agent-network doctor`

Rules:

- Read-only by default.
- Validates data home, registry file safety, registry schema, daemon reachability, socket path, active session lock, and projection health.
- May read and summarize operational log records, but must not expose secret values; any displayed payload passes through the same redaction rules as `channel.jsonl`.
- May support an explicit `--repair-permissions` flag.
- Without an explicit repair flag, it must not chmod, chown, rewrite, or delete anything.
- Daemon start must not silently repair unsafe ownership or permissions; permission repair is allowed only through `kkachi-agent-network init` or `kkachi-agent-network doctor --repair-permissions`, both of which must report what they changed.

### `kkachi-agent-network storage verify`

Rules:

- Read-only.
- Verifies `channel.jsonl` parseability, `schema_version` compatibility, `event_id` uniqueness, `command_id` idempotency consistency, and projection consistency between `channel.jsonl` and `network.sqlite`.
- Does not invoke runners.
- Does not deliver escalations.
- Does not append events.
- Exit codes are storage-specific: `0` valid logs and projection; `1` bad storage CLI arguments; `3` unsafe registry/data-home; `6` log, replay, migration, SQLite, missing projection, corrupt projection, or projection mismatch failure; `70` unexpected internal failure.

### `kkachi-agent-network storage rebuild-projection`

Rules:

- Rebuilds SQLite from `channel.jsonl`.
- Must be side-effect free with respect to session events.
- Must not run member wrappers.
- Must not deliver escalations.
- Must not invent timer-based events.
- Performs event-log preflight before replacing `network.sqlite`. Rebuild is allowed when only the projection is missing, corrupt, or mismatched, but refuses unsafe data homes, unsafe existing projection paths, corrupt logs, schema gaps, replay failures, and migration failures.
- Exit codes are storage-specific: `0` rebuild completed; `1` bad storage CLI arguments; `3` unsafe registry/data-home; `6` log, replay, migration, SQLite, unsafe existing projection, integrity, or atomic replace failure; `70` unexpected internal failure.

## Registry commands

```bash
kkachi-agent-network registry validate
kkachi-agent-network registry show
```

### Validate registry

`kkachi-agent-network registry validate` is read-only and validates:

- `<data_home>` safety;
- `registry.yaml` file safety (regular non-symlink file, owner, permissions);
- YAML schema;
- member ids;
- wrapper path resolution;
- adapter kind whitelist;
- runtime kind whitelist;
- env allowlist safety.

It does not start a session.

### Show registry summary

`kkachi-agent-network registry show` prints enabled members, display names, roles, adapter kinds, wrapper resolution status, and workspaces. It must not print secret values.

See `kkachi-agent-network init` and `kkachi-agent-network doctor` under Setup and diagnostic commands above for first-run setup, permission diagnostics, and the explicit repair flow.

## Common session commands

```bash
kkachi-agent-network status
kkachi-agent-network status <session_id> --verbose
kkachi-agent-network cancel <session_id> --reason "..."
kkachi-agent-network block <session_id> --category external_dependency --reason "..."
kkachi-agent-network resume <session_id> --blocked-event evt_01HV... --reason "..."
kkachi-agent-network transcript <session_id> --format md --output transcript.md
kkachi-agent-network transcript <session_id> --format jsonl
kkachi-agent-network export <session_id> --bundle
kkachi-agent-network tail <session_id>
```

`kkachi-agent-network cancel` emits `session_cancelled`. `status`, `transcript`, `export`, and `tail` are read-only with respect to session events and emit no events.

### Transcript, export, and tail

```bash
kkachi-agent-network transcript <session_id> --format md [--output transcript.md]
kkachi-agent-network transcript <session_id> --format jsonl [--output transcript.jsonl]
kkachi-agent-network export <session_id> --bundle [--output export-directory]
kkachi-agent-network tail <session_id> [--limit 20] --format ndjson
```

Rules:

- `transcript --format md` renders a deterministic Markdown view from `session.yaml`, `registry_snapshot.yaml` metadata, and `channel.jsonl`. It includes surface and linked-authority metadata, semantic recipients, council attendance/agenda and linked-authority-result payloads when present, delegation/review evidence, blockers, terminal/cancelled phase evidence, and a runner/cost summary when recorded.
- `transcript --format jsonl` emits the canonical event stream from `channel.jsonl` in persisted order. It does not invent fields and does not read live services.
- `transcript --output <path>` writes a local operator file after daemon rendering succeeds. Output paths must be local, regular, non-symlink paths and must not contain NUL or dot-dot segments. Existing regular files are overwritten deterministically.
- `export --bundle` writes a deterministic local directory. Without `--output`, the daemon writes under `<session_dir>/exports/<session_id>-bundle`. With `--output`, the path must pass the same local safety checks. The bundle includes `transcript.md`, `transcript.jsonl`, `brief.md`, `session.json`, `channel.jsonl`, `registry_snapshot.yaml`, and `bundle_manifest.json`; existing regular bundle files are overwritten deterministically.
- `tail` is a CLI convenience over bounded replay of existing stream frames. It is not a protocol feature group and must not be advertised as `stream.tail`.
- These commands must not append events, invoke member runners, deliver escalations, create Kanban comments, write Vault notes, send Discord messages, call Hermes/KAB/gateway/auth/token endpoints, or treat linked authorities as ordering/state authority.

### Block and resume

```bash
kkachi-agent-network block <session_id> \
  --category external_dependency \
  --reason "External dependency is unavailable."

kkachi-agent-network resume <session_id> \
  --blocked-event evt_01HV... \
  --reason "External dependency is now available."
```

Rules:

- `kkachi-agent-network block` emits `session_blocked` with envelope `phase: blocked`. The emitted event payload includes `prior_phase` and `resume_phase` (both required, even when equal). It applies to both delegation and council sessions.
- `kkachi-agent-network resume` emits `session_resumed` and returns the session to the recorded `resume_phase`. `--blocked-event` must reference the originating `session_blocked` event for that session.
- `kkachi-agent-network delegate block` remains a delegation-specific compatibility path for `session_blocked`, but the common command is canonical for new documentation.
- Budget and limit blocks are lifted by `kkachi-agent-network limits extend`, not by `kkachi-agent-network resume`. Attempting `kkachi-agent-network resume` against a `session_budget_exceeded`-originated block must fail with a clear error pointing to `kkachi-agent-network limits extend`.
- Security blocks may be resumed through `kkachi-agent-network resume` only when the security model defines the violation category as remediable and the remediation has been verified.

Allowed `--category` values for `kkachi-agent-network block`:

- `external_dependency`
- `user_decision_required`
- `scope_conflict`
- `policy_or_security`
- `budget_or_limit`
- `other`

`budget_or_limit` is allowed for manual moderator blocks but is unusual; daemon-originated budget breaches use `session_budget_exceeded` and are recovered through `kkachi-agent-network limits extend`, not through `kkachi-agent-network resume`.

`kkachi-agent-network status` shows both `status` (derived roll-up: `open`/`blocked`/`terminal`) and `phase` (exact lifecycle phase). Example:

```text
session_id: sess_20260425_0130_a
session_type: delegation
status: open
phase: working
title: Implement task A
participants: agent-mod, agent-1
```

## Stream commands

These commands are the public real-time interface for moderator and member runtimes. The CLI may implement them over local SSE, WebSocket, Unix socket, or local HTTP, but that transport is private. The stable contract is newline-delimited JSON on stdout.

```bash
kkachi-agent-network stream <session_id> --member agent-1 --since cursor_... --follow --format ndjson
kkachi-agent-network stream <session_id> --member agent-1 --from-start --format ndjson
kkachi-agent-network stream ack <session_id> --member agent-1 --cursor cursor_...
kkachi-agent-network stream status <session_id>
```

Rules:

- `stream` emits replayed events first, then live events when `--follow` is set. `stream` and `stream status` are read-only.
- `stream ack` emits `stream_cursor_acknowledged`.
- Every emitted line includes `event_id`, `cursor`, `session_id`, `type`, `from`, `to`, and `payload`. `from` is a string; `to` is always an array of strings (per `03-protocol-spec.md`).
- The stream command does **not** hide events based on `to`. A member runtime may observe events not addressed to it; it decides whether to act by inspecting event type, sender, recipients, role, phase, and policy.
- Member runtimes must acknowledge processed cursors.
- Disconnects are not fatal; runtimes resume from the last acknowledged cursor.
- A cursor gap or unknown event schema fails closed and requires replay or migration.

If a session is already active, creating another session must fail.

## Limits and budget

### Extend session limits

```bash
kkachi-agent-network limits extend <session_id> \
  --key max_usd --value 50.00 \
  --authorized-by user \
  --reason "Approved additional budget to finish review."
```

The daemon emits `limits_extended` with the authorization record. Each key extension lifts a specific previously-recorded block; extending an unrelated key does not unblock anything.

| Block cause (event) | Key(s) that lift the block | Effect of extension |
|---|---|---|
| `session_budget_exceeded` with `limit_kind: max_usd` | `max_usd` raised above observed | Session returns to its prior phase (typically `working` or `under_review`). |
| `session_budget_exceeded` with `limit_kind: max_tokens_total` | `max_tokens_total` raised above observed | Same as above. |
| `session_budget_exceeded` with `limit_kind: max_runner_calls` | `max_runner_calls` raised above observed | Same as above. |
| `session_budget_exceeded` with `limit_kind: max_elapsed_sec` | `max_elapsed_sec` raised above observed | Same as above. |
| `escalation_rate_limited` (cap reached) | `max_user_escalations` raised above observed | Subsequent `user_escalation_requested` events are recorded again. The session phase does not change because `escalation_rate_limited` does not itself transition the session. |
| `escalation_timeout` (no user response within window) | Either `escalation_response_timeout_sec` raised, or the moderator records a follow-up escalation; both restore acceptance of new escalations. The session was moved to `blocked` by the timeout and returns to `waiting_user` only after a fresh user response is recorded. |

Multiple keys can be extended in one command by repeating `--key` and `--value`. A single command emits a single `limits_extended` event listing all changes.

`--authorized-by` must be `user` (or a documented automation principal) and is recorded verbatim. Extensions without authorization are rejected.

### Show current limits and usage

```bash
kkachi-agent-network limits show <session_id>
```

Reports each limit, observed usage, and remaining headroom. Output includes runner accounting:

```text
session_id: sess_20260425_0130_a
limits:
  max_runner_calls: 500
  max_tokens_total: 2000000
  max_usd: 25.00
observed:
  runner_calls_total: 42
  missing_cost_runner_calls_total: 3
  tokens_in_total: 180000
  tokens_out_total: 42000
  usd_estimate_total: 2.13
remaining:
  runner_calls: 458
  tokens_total: 1778000
  usd: 22.87
warnings:
- 3 runner calls have missing measured cost. Token and USD totals may be incomplete.
```

`kkachi-agent-network status <session_id> --verbose` includes the same runner accounting block (`runner_calls_total`, `missing_cost_runner_calls_total`, current token and USD totals, any unresolved budget block, and recent runner invocation failures), plus pending escalation batches and waiting-user state.

Pending batch example:

```text
session_id: sess_20260425_0130_a
status: open
phase: working
pending_escalation_batches:
- batch_id: escbatch_01HV...
  pending_count: 2
  deadline_at: 2026-04-25T00:30:00Z
  will_enter_waiting_user_on_flush: true
```

Waiting-user example:

```text
session_id: sess_20260425_0130_a
status: open
phase: waiting_user
waiting_on:
  escalation_event_id: evt_user_escalation_requested_01
  batch_id: escbatch_01HV...
  delivered: true
  delivery_target: origin
  response_timeout_at: 2026-04-26T00:00:00Z
```

`kkachi-agent-network limits show` adds an escalation usage block:

```text
escalations:
  user_escalations_total: 3
  max_user_escalations: 10
  pending_batched_candidates: 2
  pending_batches: 1
  note: pending batched candidates do not count against max_user_escalations until flushed into user_escalation_requested
```

## Delegation commands

### Start delegation

```bash
kkachi-agent-network delegate new "Implement task A" \
  --to agent-1 \
  --context task.md \
  --acceptance acceptance.md
```

Emits: `session_created` followed by `task_assigned`.

### Member acknowledges assignment

```bash
kkachi-agent-network delegate ack <session_id> \
  --from agent-1 \
  --understanding "I understand that A is the priority." \
  --plan plan.md \
  --question "Should B be excluded from this task?"
```

Emits: `assignee_acknowledged`.

Rules:

- `--understanding` is required.
- `--plan` may point to a markdown file or be repeated as inline text, depending on implementation.
- `--question` is repeatable.
- If the acknowledgement contains blocking questions, the daemon may transition to `needs_clarification` per `06-state-machine.md`.

### Send general delegation message

```bash
kkachi-agent-network delegate message <session_id> \
  --from agent-mod \
  --to agent-1 \
  --kind scope_correction \
  --message "Prioritize A; split B into follow-up."
```

Emits: `delegation_message`.

This command is **not** used to answer a specific `clarification_requested` event. Use `kkachi-agent-network delegate answer-clarification` for direct clarification answers.

Allowed `--kind` values: `note`, `instruction`, `scope_correction`, `priority_update`, `process_guidance`.

### Member asks clarification

```bash
kkachi-agent-network delegate clarify <session_id> \
  --from agent-1 \
  --question "Requirement A conflicts with B. Which wins?" \
  --urgency blocked
```

Emits: `clarification_requested`.

### Answer assignee clarification

```bash
kkachi-agent-network delegate answer-clarification <session_id> \
  --to agent-1 \
  --in-reply-to evt_01HV... \
  --answer "Prioritize A and split B into follow-up scope." \
  --source agent-mod
```

User-derived answer example:

```bash
kkachi-agent-network delegate answer-clarification <session_id> \
  --to agent-1 \
  --in-reply-to evt_01HV... \
  --answer "A is the priority. Split B into follow-up scope." \
  --source user
```

Emits: `clarification_answered`.

Rules:

- `--in-reply-to` must reference a `clarification_requested` event; the resulting `clarification_answered` event sets `causation_event_id` to that reference.
- `--source` must be `agent-mod`, `user`, or another documented authority label.
- This command is distinct from `delegate message`.

### Member records progress update

```bash
kkachi-agent-network delegate update <session_id> \
  --from agent-1 \
  --progress-status working \
  --summary "Implementation is halfway done." \
  --next-step "Run tests."
```

Emits: `assignee_update`.

`--progress-status` is the assignee's self-reported work status; it is **not** the session lifecycle `phase` and does not directly change the session phase. To move the session phase to `blocked`, use `kkachi-agent-network delegate block` (or wait for a daemon-internal blocking event such as `session_budget_exceeded`).

Allowed `--progress-status` values: `working`, `blocked`, `partial`, `testing`, `ready_to_submit`.

### Escalate a member question to the user

```bash
kkachi-agent-network delegate escalate <session_id> \
  --from agent-1 \
  --source-event evt_01HV... \
  --question "Requirements A and B conflict. Which should be prioritized?" \
  --option "A" \
  --option "B" \
  --option "split scope" \
  --recommendation "Prioritize A" \
  --urgency blocked \
  --delivery origin-or-telegram \
  --batch-policy auto
```

Emits: `user_escalation_requested` for non-batched cases; `escalation_batched` when the daemon batches a low-urgency candidate.

Allowed `--urgency` values: `low`, `normal`, `urgent`, `blocked`.

Allowed `--batch-policy` values: `auto`, `never`, `force-low-only`.

- `--batch-policy auto` allows batching only when `--urgency low` and the escalation is non-blocking.
- `--batch-policy never` forces immediate `user_escalation_requested`.
- `--batch-policy force-low-only` asks the daemon to batch low-urgency candidates when allowed; it must fail if urgency is not `low`.

If the daemon batches the escalation, the command emits `escalation_batched`, not `user_escalation_requested`, and the session phase remains the prior phase. If the daemon records a user-facing escalation, the command emits `user_escalation_requested` and the session enters `waiting_user`.

`--delivery` is a **hint to the moderator's Hermes plugin/gateway helper or equivalent gateway skill**, not an instruction to the daemon. The daemon records the value in the `user_escalation_requested` payload and stops there; it never opens an outbound channel itself. The moderator runtime reads the hint, decides how to actually reach the user (Telegram/Slack/Discord/the origin Hermes session/etc.), performs the delivery through Hermes gateway capability, and writes back `user_escalation_delivered` (or `user_escalation_delivery_failed` followed by a fallback delivery) through a typed KAN command. The CLI command remains the canonical fallback for recording the same event.

Recognized hint values:

- `origin`: prefer the original Hermes session.
- `telegram`: prefer the Telegram gateway.
- `origin-or-telegram`: prefer origin, fall back to Telegram on urgency or unreachability.
- `both`: deliver via origin and Telegram.

These names are conventional. The moderator skill may understand additional gateway labels (e.g. `slack`, `discord`); unknown labels are passed through unchanged so the moderator can interpret them.

### Record user escalation delivery

```bash
kkachi-agent-network delegate escalation-delivered <session_id> \
  --escalation evt_01HV... \
  --delivery-target origin \
  --platform hermes-session \
  --message-ref "optional external delivery reference"
```

Emits: `user_escalation_delivered`.

Rules:

- This command records a delivery already performed by the Hermes plugin/gateway helper or equivalent moderator runtime gateway skill. The daemon does not deliver to Telegram, Slack, Discord, or the origin Hermes session itself.
- `--escalation` must reference the originating `user_escalation_requested` event; the emitted event sets `causation_event_id` to that reference.

### Record user escalation delivery failure

```bash
kkachi-agent-network delegate escalation-delivery-failed <session_id> \
  --escalation evt_01HV... \
  --target telegram \
  --reason gateway_unreachable \
  --will-retry-target origin
```

Emits: `user_escalation_delivery_failed`.

Rules:

- This command records that the Hermes plugin/gateway helper or equivalent moderator runtime gateway skill failed to reach a target.
- A later fallback delivery may be recorded with `delegate escalation-delivered`.
- `--will-retry-target` is repeatable.

### Flush a pending escalation batch

```bash
kkachi-agent-network delegate escalation-flush <session_id> \
  --batch-id escbatch_01HV... \
  --reason "User is available now."
```

Emits: `user_escalation_requested`.

Rules:

- This command flushes a pending low-urgency escalation batch into a single `user_escalation_requested` event.
- The session enters `waiting_user`.
- The daemon validates that the batch exists, is pending, and is not empty.

### Inspect pending escalation batches

```bash
kkachi-agent-network delegate escalation-batches <session_id>
```

Read-only command. Output example:

```text
pending escalation batches:
- batch_id: escbatch_01HV...
  pending_count: 2
  first_event_id: evt_q1
  deadline_at: 2026-04-25T00:30:00Z
  phase_at_batch: working
  can_flush: true
```

### Resolve a user escalation

```bash
kkachi-agent-network delegate resolve-escalation <session_id> \
  --escalation evt_user_escalation_requested_01 \
  --answer "A is the priority. Split B into follow-up scope."
```

Batch answer example:

```bash
kkachi-agent-network delegate resolve-escalation <session_id> \
  --escalation evt_user_escalation_requested_01 \
  --answer-file user_answers.json
```

`user_answers.json` example:

```json
[
  {"source_event_id": "evt_q1", "answer": "Yes, prefer option A."},
  {"source_event_id": "evt_q2", "answer": "Reject risk R for now."}
]
```

Emits: `user_escalation_resolved`.

Rules:

- `--escalation` must reference the final `user_escalation_requested` event id, not an `escalation_batched` event id.
- After resolution, the moderator must relay each answer to the affected member or reviewer through `delegate answer-clarification --source user` (or the corresponding review answer path).
- This command is normally executed by the moderator runtime after receiving the user's answer. The persisted event uses `from: "user"` because the semantic originator of the answer is the user. The command's `command_id` still belongs to the CLI write that recorded the answer.

### Moderator requests an update

```bash
kkachi-agent-network delegate request-update <session_id> \
  --to agent-1 \
  --reason "No progress update has been recorded recently." \
  --requested-detail progress \
  --requested-detail blockers \
  --requested-detail next_step
```

Emits: `assignee_update_requested`.

### Submit or record artifacts manually

```bash
kkachi-agent-network delegate submit <session_id> \
  --from agent-1 \
  --summary "Implemented the storage projection and replay path." \
  --artifact result.md \
  --artifact test-log.txt \
  --verification "pytest tests/storage passed" \
  --known-risk "Migration tests are still minimal."
```

Emits: `work_submitted`.

Rules:

- `--summary` is required.
- `--artifact` is repeatable.
- `--verification` is repeatable.
- `--known-risk` is repeatable.
- Artifact ingestion follows `12-security.md` and `05-storage-schema.md`.
- `--artifact` values are source paths supplied to the CLI. The daemon ingests each source path before appending `work_submitted`. The persisted `work_submitted.payload.artifacts` list contains ingested artifact references (`artifact_id`, `stored_path`, `sha256`, `size_bytes`, `mime`), not raw source paths.

### Request review gate

```bash
kkachi-agent-network delegate review <session_id> \
  --by agent-2 \
  --focus risk,missing-constraints
```

Emits: `review_requested`.

### Reviewer asks assignee clarification

```bash
kkachi-agent-network delegate review-question <session_id> \
  --from agent-2 \
  --to agent-1 \
  --question "Why did the implementation choose this retry boundary?" \
  --needed-for "Decide whether this is intentional or a bug"
```

Emits: `review_clarification_requested`.

### Assignee answers reviewer clarification

```bash
kkachi-agent-network delegate review-answer <session_id> \
  --from agent-1 \
  --to agent-2 \
  --answer "The boundary follows the existing timeout policy." \
  --evidence path/to/source.py
```

Emits: `review_clarification_answered`.

### Reviewer submits verdict

```bash
kkachi-agent-network delegate review-submit <session_id> \
  --from agent-2 \
  --verdict changes_requested \
  --findings findings.json \
  --clarification-considered evt_01HV...
```

Emits: `review_submitted`.

Allowed `--verdict` values: `approved`, `comments`, `changes_requested`, `blocked`.

Rules:

- `--findings` points to a JSON file containing review findings.
- `--clarification-considered` is repeatable.
- High-severity `changes_requested` findings normally lead to `revision_requested` unless the moderator rejects the finding with a recorded reason.

### Request revision

```bash
kkachi-agent-network delegate revise <session_id> \
  --to agent-1 \
  --changes changes.md
```

Emits: `revision_requested`.

### Accept

```bash
kkachi-agent-network delegate accept <session_id>
```

Emits: `work_accepted`.

### Block (delegation compatibility path)

```bash
kkachi-agent-network delegate block <session_id> \
  --category external_dependency \
  --reason "External dependency is unavailable."
```

Emits: `session_blocked` with envelope `phase: blocked`. The emitted event payload includes `prior_phase` and `resume_phase` (both required, even when equal).

Allowed `--category` values: `external_dependency`, `user_decision_required`, `scope_conflict`, `policy_or_security`, `budget_or_limit`, `other`.

This is a manual block, distinct from daemon-originated `session_budget_exceeded`/`escalation_timeout` which may also move a session into a blocked state per `13-operational-contracts.md`.

`kkachi-agent-network delegate block` is retained as a delegation-specific compatibility path. The canonical common command for both delegation and council sessions is `kkachi-agent-network block` (see Common session commands above). Both paths emit the same `session_blocked` event; new documentation should prefer the common command.

## Council commands

### Start council

```bash
kkachi-agent-network council new "Discuss topic A" \
  --members agent-1,agent-2,agent-3 \
  --moderator agent-mod
```

Emits: `session_created`.

`--members` defines the council member list. Broadcast council events (`preparation_requested`, `hand_raise_requested`, `draft_conclusion`, `consensus_vote_requested`) use explicit `to` arrays derived from this list. The daemon must not emit `"to": ["all"]` or an omitted `to` field for those events.


Optional Discord-thread council binding flags:

```bash
kkachi-agent-network council new "Discuss topic A" \
  --members agent-1,agent-2,agent-3 \
  --moderator agent-mod \
  --surface discord-thread \
  --thread-id 1507515847227215932 \
  --kanban-card t_xxxxx \
  --vault-decision-note docs/decisions/topic-a.md \
  --turn-mode role_order
```

These flags populate optional `session_created.payload.surface`, `session_created.payload.linked_authority`, and `session_created.payload.turn_mode`. They do not authorize Discord API access by `kkachi-agent-networkd`, and they do not make Discord the source of truth. `--turn-mode` is the session-level intended/default floor policy only; each actual floor grant still records `speaker_selected.payload.selection_mode` as the per-turn audit fact.

### Attendance and agenda lock

```bash
kkachi-agent-network council request-attendance <session_id> --timeout 5m
kkachi-agent-network council attend <session_id> --from agent-1 --status present --summary "Present."
kkachi-agent-network council lock-agenda <session_id> \
  --decision-question "Decide next action for Kanban card t_xxxxx" \
  --max-rounds 2
```

Emits:

- `council request-attendance` → `attendance_requested`.
- `council attend` → `member_attended`.
- `council lock-agenda` → `agenda_locked`.

First-pass rule: these commands operate while the council is in `created`. `council prepare` remains the transition into `preparation`. For `surface.kind=discord_thread`, these are not optional presentation commands: `council prepare` must fail closed unless `attendance_requested`, one terminal `member_attended` record per required participant (`present`, `partial`, `unavailable`, or `no_response_timeout`), and `agenda_locked` already exist. Rejection leaves the session in `created` and appends no `preparation_requested` event.

### Preparation

```bash
kkachi-agent-network council prepare <session_id> --timeout 10m
```

Emits: `preparation_requested`.

For Discord-thread-bound councils, this command validates the attendance/agenda guard above before emitting `preparation_requested`. The CLI must surface missing prerequisites with a clear error naming the missing attendance member(s) or missing `agenda_locked` event.

### Member marks preparation ready

```bash
kkachi-agent-network council ready <session_id> \
  --from agent-1 \
  --summary "Found three relevant constraints." \
  --notes notes.md
```

Emits: `member_ready`.

Rules:

- Member runtimes emit this after observing `preparation_requested` and completing preparation.
- `--notes` may point to a file under the member workspace or session participant directory.

### Member records partial preparation

```bash
kkachi-agent-network council prepared-partial <session_id> \
  --from agent-1 \
  --reason timeout \
  --summary "Partial notes are available." \
  --notes notes.md
```

Emits: `member_prepared_partial`.

Rules:

- A member runtime may emit this explicitly.
- The daemon may also emit `member_prepared_partial` internally when preparation timeout expires (origin class `daemon_internal`).

### Poll and grant turns

```bash
kkachi-agent-network council poll <session_id> --research-timeout 10m
kkachi-agent-network council hand-raise <session_id> --from agent-3 --intent rebuttal --relevance 5 --urgency 4 --reason "This changes the risk decision."
kkachi-agent-network council grant <session_id> --auto
kkachi-agent-network council grant <session_id> --to agent-3
kkachi-agent-network council grant <session_id> --to agent-3 --mode role_order --round 1 --reason "Round 1 risk review"
kkachi-agent-network council speak <session_id> --from agent-3 --stdin
```

Emits:

- `council poll` → `hand_raise_requested`.
- `council hand-raise` → `hand_raise`.
- `council grant` → `speaker_selected`.
- `council speak` → `speech`.

`council grant --mode <mode>` writes the per-turn `speaker_selected.payload.selection_mode`. If a session was created with `--turn-mode`, the grant mode may match that default or deliberately deviate from it. Any deviation requires `--reason`; the persisted `speaker_selected` event must include that reason so audit/replay can explain the difference.

### Intervene

```bash
kkachi-agent-network council intervene <session_id> \
  --to agent-2 \
  --reason "topic drift" \
  --message "Tie this back to the decision criteria or withdraw it."
```

Emits: `moderator_intervention`.

### Propose, vote, revise, finish

```bash
kkachi-agent-network council propose <session_id> --from-file draft.md
kkachi-agent-network council request-vote <session_id> --draft-version 1 --timeout 10m
kkachi-agent-network council vote <session_id> --from agent-3 --vote approve_with_conditions --reason "..." --required-change "..."
kkachi-agent-network council revise <session_id> --from-file draft_v2.md --reason "Addressed agent-2 block vote."
kkachi-agent-network council finalize <session_id> \
  --authority-return-status posted \
  --kanban-comment-id kc_123 \
  --vault-decision-note docs/decisions/topic-a.md
kkachi-agent-network council unresolved <session_id> --reason "persistent blocking objection"
```

Emits:

- `council propose` → `draft_conclusion` with `draft_version: 1`. Draft text may come from `--from-file` or stdin.
- `council revise` → `draft_conclusion` with incremented `draft_version`, `revision_reason`, and `supersedes_draft_version`.
- `council request-vote` → `consensus_vote_requested`. Member runtimes vote only after observing this event or its replay.
- `council vote` → `consensus_vote`.
- `council finalize` → `council_finalized`.
- `council unresolved` → `council_unresolved`.

When the session has `linked_authority`, `council finalize` must record `linked_authority_result.status` as `posted`, `failed`, or `pending_followup`:

- `posted` requires evidence such as `--kanban-comment-id`, `--vault-decision-note`, or an explicit `--return-evidence` pointer.
- `failed` requires `--failure-reason` and must not be reported as completed.
- `pending_followup` requires `--followup-card-id` or equivalent `--return-evidence`.

The plugin/CLI/daemon path records this evidence only. It must not create Kanban comments or Vault notes as a side effect of replay, transcript, export, status, or projection rebuild. The moderator/Gray workflow performs those writes through the appropriate external authority path before or alongside recording the evidence.

## Event-to-command coverage

Every state-mutating CLI command must declare the event type or event sequence it emits. Participant-originated events must have an explicit CLI command path. Daemon-originated operational events do not require public write commands.

### Common session matrix

| Event | Origin class | Command path |
|---|---|---|
| `session_blocked` | `participant_cli` | `kkachi-agent-network block ...`; `kkachi-agent-network delegate block ...` for delegation compatibility |
| `session_resumed` | `participant_cli` | `kkachi-agent-network resume ...` |
| `session_cancelled` | `participant_cli` | `kkachi-agent-network cancel ...` |

### Delegation matrix

| Event | Origin class | Command path |
|---|---|---|
| `session_created` | `daemon_after_cli` | `kkachi-agent-network delegate new ...` |
| `task_assigned` | `daemon_after_cli` | `kkachi-agent-network delegate new ...` |
| `assignee_acknowledged` | `participant_cli` | `kkachi-agent-network delegate ack ...` |
| `clarification_requested` | `participant_cli` | `kkachi-agent-network delegate clarify ...` |
| `clarification_answered` | `participant_cli` | `kkachi-agent-network delegate answer-clarification ...` |
| `delegation_message` | `participant_cli` | `kkachi-agent-network delegate message ...` |
| `assignee_update_requested` | `participant_cli` | `kkachi-agent-network delegate request-update ...` |
| `assignee_update` | `participant_cli` | `kkachi-agent-network delegate update ...` |
| `user_escalation_requested` | `mixed` | `kkachi-agent-network delegate escalate ...`; `kkachi-agent-network delegate escalation-flush ...`; daemon runtime batch flush |
| `user_escalation_delivered` | `participant_cli` | `kkachi-agent-network delegate escalation-delivered ...` |
| `user_escalation_delivery_failed` | `participant_cli` | `kkachi-agent-network delegate escalation-delivery-failed ...` |
| `user_escalation_resolved` | `participant_cli` | `kkachi-agent-network delegate resolve-escalation ...` (semantic originator is the user; `from: "user"`) |
| `work_submitted` | `participant_cli` | `kkachi-agent-network delegate submit ...` |
| `review_requested` | `participant_cli` | `kkachi-agent-network delegate review ...` |
| `review_clarification_requested` | `participant_cli` | `kkachi-agent-network delegate review-question ...` |
| `review_clarification_answered` | `participant_cli` | `kkachi-agent-network delegate review-answer ...` |
| `review_submitted` | `participant_cli` | `kkachi-agent-network delegate review-submit ...` |
| `revision_requested` | `participant_cli` | `kkachi-agent-network delegate revise ...` |
| `work_accepted` | `participant_cli` | `kkachi-agent-network delegate accept ...` |
| `session_blocked` | `participant_cli` | `kkachi-agent-network block ...`; `kkachi-agent-network delegate block ...` (delegation compatibility) |
| `session_resumed` | `participant_cli` | `kkachi-agent-network resume ...` |
| `session_cancelled` | `participant_cli` | `kkachi-agent-network cancel ...` |

### Council matrix

| Event | Origin class | Command path |
|---|---|---|
| `session_created` | `daemon_after_cli` | `kkachi-agent-network council new ...` |
| `attendance_requested` | `participant_cli` | `kkachi-agent-network council request-attendance ...` |
| `member_attended` | `mixed` | `kkachi-agent-network council attend ...` when participant-originated (`from: <member>`); daemon-internal on timeout (`from: "kkachi-agent-networkd"`, `payload.member` records the affected member) |
| `agenda_locked` | `participant_cli` | `kkachi-agent-network council lock-agenda ...` |
| `preparation_requested` | `daemon_after_cli` | `kkachi-agent-network council prepare ...` |
| `member_ready` | `participant_cli` | `kkachi-agent-network council ready ...` |
| `member_prepared_partial` | `mixed` | `kkachi-agent-network council prepared-partial ...` when participant-originated (`from: <member>`); daemon-internal on timeout (`from: "kkachi-agent-networkd"`, `payload.member` records the affected member) |
| `hand_raise_requested` | `daemon_after_cli` | `kkachi-agent-network council poll ...` |
| `hand_raise` | `participant_cli` | `kkachi-agent-network council hand-raise ...` |
| `speaker_selected` | `participant_cli` | `kkachi-agent-network council grant ...` |
| `speech` | `participant_cli` | `kkachi-agent-network council speak ...` |
| `moderator_intervention` | `participant_cli` | `kkachi-agent-network council intervene ...` |
| `draft_conclusion` | `participant_cli` | `kkachi-agent-network council propose ...` or `kkachi-agent-network council revise ...` |
| `consensus_vote_requested` | `participant_cli` | `kkachi-agent-network council request-vote ...` |
| `consensus_vote` | `participant_cli` | `kkachi-agent-network council vote ...` |
| `council_finalized` | `participant_cli` | `kkachi-agent-network council finalize ...` |
| `council_unresolved` | `participant_cli` | `kkachi-agent-network council unresolved ...` |
| `session_blocked` | `participant_cli` | `kkachi-agent-network block ...` (council uses the common block path) |
| `session_resumed` | `participant_cli` | `kkachi-agent-network resume ...` |
| `session_cancelled` | `participant_cli` | `kkachi-agent-network cancel ...` |

### Operational matrix

| Event | Origin class | Public write command needed? |
|---|---|---|
| `session_budget_exceeded` | `daemon_internal` | No |
| `limits_extended` | `participant_cli` | Yes — `kkachi-agent-network limits extend ...` |
| `runner_retry_attempted` | `daemon_internal` | No |
| `escalation_deduplicated` | `mixed` | No — CLI escalation attempts may cause it; daemon-internal reconciliation may also cause it |
| `escalation_rate_limited` | `mixed` | No — CLI escalation or flush attempts may cause it; daemon-internal timer flush may also cause it |
| `security_violation` | `daemon_internal` | No |
| `redaction_applied` | `daemon_internal` | No |
| `escalation_batched` | `mixed` | No — `kkachi-agent-network delegate escalate ...` may cause it; daemon-internal batch updates do not require a public command |
| `escalation_timeout` | `daemon_internal` | No |
| `runner_invocation_started` | `daemon_internal` | No |
| `runner_invocation_failed` | `daemon_internal` | No |
| `runner_result_discarded` | `daemon_internal` | No |
| `session_handle_rotated` | `daemon_internal` | No |
| `stream_subscriber_connected` | `daemon_internal` | No |
| `stream_cursor_acknowledged` | `daemon_after_cli` | Yes — `kkachi-agent-network stream ack ...` |
| `stream_subscriber_stale` | `daemon_internal` | No |

## Structured output and exit codes

All commands that can fail must support `--format text` and `--format json`. The default may remain text. JSON errors use a stable envelope so Hermes Agent runtimes and tooling can parse failure conditions without screen-scraping.

### JSON error envelope

```json
{
  "error": {
    "code": "ACTIVE_SESSION_EXISTS",
    "category": "session_lock",
    "message": "active session already exists",
    "details": {
      "active_session_id": "sess_...",
      "status": "blocked",
      "phase": "blocked",
      "blocked_by_event_id": "evt_..."
    },
    "next": [
      "kkachi-agent-network status sess_... --verbose",
      "kkachi-agent-network resume sess_... --blocked-event evt_...",
      "kkachi-agent-network cancel sess_... --reason ..."
    ]
  }
}
```

Rules:

- `code` is a stable machine-readable identifier; new codes are additive.
- `category` groups related codes for routing (e.g. `session_lock`, `validation`, `security`, `storage`, `daemon`, `replay`).
- `message` is a single short human sentence; longer explanations live in `details` and `next`.
- `details` captures structured context relevant to the failure.
- `next` lists suggested follow-up commands the runtime can execute.

### Exit codes

| Exit code | Meaning |
|---:|---|
| 0 | Success |
| 1 | Validation error |
| 2 | Daemon unavailable |
| 3 | Registry or data-home unsafe |
| 4 | Active session exists |
| 5 | Session blocked or budget/security gate |
| 6 | Replay, migration, or storage failure |
| 70 | Internal error |

JSON error envelopes accompany the matching exit code. Text-mode errors use the same exit codes so callers can branch on the numeric value alone if they do not parse JSON.

DAEMN-001 commands that are intentionally deferred to DAEMN-002+ return `unsupported_feature` in the JSON error envelope with exit code `1` (validation error). Exit code `4` remains reserved for active-session lifecycle rejection and must not be reused for unsupported features.

## Error examples

### Active session exists (open)

```text
ERROR: active session already exists
active_session_id: sess_20260425_0130_a
session_type: delegation
status: open
phase: working
next:
- kkachi-agent-network status sess_20260425_0130_a
- kkachi-agent-network delegate accept sess_20260425_0130_a
- kkachi-agent-network cancel sess_20260425_0130_a
```

### Active session exists (blocked)

`blocked` sessions are still active and continue to hold the single-active-session lock.

```text
ERROR: active session already exists
active_session_id: sess_20260425_0130_a
session_type: delegation
status: blocked
phase: blocked
blocked_by_event_id: evt_01HV...
resume_phase: under_review
next:
- kkachi-agent-network status sess_20260425_0130_a --verbose
- kkachi-agent-network limits show sess_20260425_0130_a
- kkachi-agent-network limits extend sess_20260425_0130_a --key max_usd --value 50.00 --authorized-by user --reason "Approved additional budget."
- kkachi-agent-network cancel sess_20260425_0130_a --reason "User cancelled blocked session."
```

### Registry missing

```text
ERROR: registry not found: <data_home>/registry.yaml
The daemon will not dispatch members without a valid registry.
```

### Unknown recipient

```text
ERROR: unknown recipient
recipient: agent-x
session_id: sess_20260425_0130_a
valid_recipients:
- agent-mod
- agent-1
- agent-2
- user
```

### Reserved principal collision

```text
ERROR: invalid registry member id
member: user
reason: reserved principal name
reserved_principals:
- user
- kkachi-agent-networkd
```

### Session budget exceeded

```text
ERROR: session budget exceeded
session_id: sess_20260425_0130_a
limit_kind: max_runner_calls
observed: 500
limit: 500
status: blocked
phase: blocked
next:
- kkachi-agent-network limits show sess_20260425_0130_a
- kkachi-agent-network limits extend sess_20260425_0130_a --key max_runner_calls --value 750 --authorized-by user --reason "Approved additional runner calls."
- kkachi-agent-network cancel sess_20260425_0130_a --reason "Budget exhausted."
```

### Runner cost missing warning

```text
WARNING: runner cost missing
session_id: sess_20260425_0130_a
missing_cost_runner_calls_total: 3
note: token and USD totals exclude runner calls whose cost could not be parsed
```

### Registry unsafe permissions

```text
ERROR: unsafe registry permissions
path: <data_home>/registry.yaml
category: registry_permissions_unsafe
observed_mode: 0664
required: not group-writable and not world-writable
suggested_fix:
- chmod 0600 <data_home>/registry.yaml
```

### Registry symlink

```text
ERROR: registry.yaml must be a regular file
path: <data_home>/registry.yaml
category: registry_symlink_forbidden
reason: symlink registry files are not allowed
suggested_fix:
- replace the symlink with a regular file
```

### Unsafe data home

```text
ERROR: unsafe data home permissions
path: <data_home>
category: registry_data_home_unsafe
observed_mode: 0775
required: owned by current daemon user and not group/world writable
suggested_fix:
- chmod 0700 <data_home>
```

### Registry owner unsafe

```text
ERROR: unsafe registry owner
path: <data_home>/registry.yaml
category: registry_owner_unsafe
owner_uid: 502
current_uid: 501
required: current daemon user or root
```

### Registry snapshot write failure

```text
ERROR: registry snapshot write failed
path: <data_home>/sessions/<session_id>/registry_snapshot.yaml
category: registry_snapshot_write_failed
action: session creation aborted before session_created
```
