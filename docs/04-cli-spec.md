# CLI Specification

Official CLI: `atn-control`. The Hermes plugin is the preferred agent-facing integration layer, but this CLI is the canonical diagnostics, recovery, test, and manual-operation contract.

## Principles

- All state mutations go through the daemon.
- Commands are explicit and auditable.
- Status commands are concise by default and verbose on request.
- The CLI should be usable by the moderator agent through terminal tool calls and should remain available when the Hermes plugin is absent, disabled, or broken.
- Hermes plugin tools and slash commands must map to the same typed command models and daemon validations as the CLI. The plugin should call the ATN protocol client/contract directly, not shell out, except as a compatibility fallback or for operator-equivalent diagnostics.

## Global write command rules

Every state-mutating CLI command accepts an optional `--command-id cmd_...`. If `--command-id` is omitted, the CLI generates one. Hermes plugin tool calls that mutate state must provide or obtain the same command-id semantics through the ATN protocol client/contract. The daemon uses `command_id` for idempotency per `13-operational-contracts.md` §2; retrying the same logical command must reuse the same `command_id`.

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
- `atn-controld` is not accepted as a normal participant-supplied recipient.

`to` is **semantic addressing**, not stream access control. Stream read permissions are governed by `12-security.md`.

## Daemon commands

```bash
atn-control daemon start
atn-control daemon stop
atn-control daemon status
```

`atn-control daemon start` validates `<data_home>` and `registry.yaml` before reporting ready. If validation fails, the daemon does not accept session creation or dispatch commands; the failure is written to `<data_home>/operational.log` per `12-security.md`.

The daemon protocol also exposes plugin-facing read-only compatibility commands `version.read`, `status.read`, and `diagnostics.read`. These are not replacements for concise operator `daemon status` / `health` output; they provide explicit protocol version, daemon version, minimum plugin protocol version, feature groups/features, capability state, and readiness/diagnostic categories for fail-closed plugin negotiation.

## Setup and diagnostic commands

```bash
atn-control init
atn-control doctor
atn-control storage verify
atn-control storage rebuild-projection
```

These commands cover first-run setup, environment validation, and storage recovery. They are operational tools and are not part of the session protocol.

### `atn-control init`

Rules:

- Creates `<data_home>` with mode `0700` when missing.
- May create a sample `registry.yaml` with mode `0600` only when the file does not exist.
- Must not overwrite an existing registry.
- Must report every filesystem change it makes.
- Does not start the daemon and does not create sessions.

### `atn-control doctor`

Rules:

- Read-only by default.
- Validates data home, registry file safety, registry schema, daemon reachability, socket path, active session lock, and projection health.
- May read and summarize operational log records, but must not expose secret values; any displayed payload passes through the same redaction rules as `channel.jsonl`.
- May support an explicit `--repair-permissions` flag.
- Without an explicit repair flag, it must not chmod, chown, rewrite, or delete anything.
- Daemon start must not silently repair unsafe ownership or permissions; permission repair is allowed only through `atn-control init` or `atn-control doctor --repair-permissions`, both of which must report what they changed.

### `atn-control storage verify`

Rules:

- Read-only.
- Verifies `channel.jsonl` parseability, `schema_version` compatibility, `event_id` uniqueness, `command_id` idempotency consistency, and projection consistency between `channel.jsonl` and `network.sqlite`.
- Does not invoke runners.
- Does not deliver escalations.
- Does not append events.
- Exit codes are storage-specific: `0` valid logs and projection; `1` bad storage CLI arguments; `3` unsafe registry/data-home; `6` log, replay, migration, SQLite, missing projection, corrupt projection, or projection mismatch failure; `70` unexpected internal failure.

### `atn-control storage rebuild-projection`

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
atn-control registry validate
atn-control registry show
```

### Validate registry

`atn-control registry validate` is read-only and validates:

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

`atn-control registry show` prints enabled members, display names, roles, adapter kinds, wrapper resolution status, and workspaces. It must not print secret values.

See `atn-control init` and `atn-control doctor` under Setup and diagnostic commands above for first-run setup, permission diagnostics, and the explicit repair flow.

## Common session commands

```bash
atn-control status
atn-control status <session_id> --verbose
atn-control cancel <session_id> --reason "..."
atn-control block <session_id> --category external_dependency --reason "..."
atn-control resume <session_id> --blocked-event evt_01HV... --reason "..."
atn-control transcript <session_id> --format md --output transcript.md
atn-control transcript <session_id> --format jsonl
atn-control export <session_id> --bundle
atn-control tail <session_id>
```

`atn-control cancel` emits `session_cancelled`. `status`, `transcript`, `export`, and `tail` are read-only with respect to session events and emit no events.

### Transcript, export, and tail

```bash
atn-control transcript <session_id> --format md [--output transcript.md]
atn-control transcript <session_id> --format jsonl [--output transcript.jsonl]
atn-control export <session_id> --bundle [--output export-directory]
atn-control tail <session_id> [--limit 20] --format ndjson
```

Rules:

- `transcript --format md` renders a deterministic Markdown view from `session.yaml`, `registry_snapshot.yaml` metadata, and `channel.jsonl`. It includes surface and linked-authority metadata, semantic recipients, council attendance/agenda and linked-authority-result payloads when present, delegation/review evidence, blockers, terminal/cancelled phase evidence, a runner/cost summary when recorded, and additive selected-runner accounting for council logs. Selected-runner accounting reports selected speakers, runner starts/successes/failures/discards/dispatch failures, linked runner speech, runnerless/manual/fallback speech, diagnostics, and `selected_runner_pass`; runnerless/manual/fallback speech is lifecycle/fallback evidence only and cannot repair a selected runner terminal failure.
- `transcript --format jsonl` emits the canonical event stream from `channel.jsonl` in persisted order. It does not invent fields and does not read live services.
- `transcript --output <path>` writes a local operator file after daemon rendering succeeds. Output paths must be local, regular, non-symlink paths and must not contain NUL or dot-dot segments. Existing regular files are overwritten deterministically.
- `export --bundle` writes a deterministic local directory. Without `--output`, the daemon writes under `<session_dir>/exports/<session_id>-bundle`. With `--output`, the path must pass the same local safety checks. The bundle includes `transcript.md`, `transcript.jsonl`, `brief.md`, `session.json`, `channel.jsonl`, `registry_snapshot.yaml`, and `bundle_manifest.json`; existing regular bundle files are overwritten deterministically. `bundle_manifest.json` includes the same additive `selected_runner_accounting` object as report/status surfaces, the `selected_runner_timeout_evidence` object for guarded live-visible council sessions, plus derived `discussion_lifecycle` and lifecycle fields on `summary_turn_accounting` rows when `limits.max_discussion_turns` evidence exists. The projected timeout object is the normalized persisted session snapshot; later guarded-lane drift is emitted as timeout-policy diagnostic evidence instead of rewriting bundle history.
- `tail` is a CLI convenience over bounded replay of existing stream frames. It is not a protocol feature group and must not be advertised as `stream.tail`.
- These commands must not append events, invoke member runners, deliver escalations, create Kanban comments, write Vault notes, send Discord messages, call Hermes/KAB/gateway/auth/token endpoints, or treat linked authorities as ordering/state authority.

### Block and resume

```bash
atn-control block <session_id> \
  --category external_dependency \
  --reason "External dependency is unavailable."

atn-control resume <session_id> \
  --blocked-event evt_01HV... \
  --reason "External dependency is now available."
```

Rules:

- `atn-control block` emits `session_blocked` with envelope `phase: blocked`. The emitted event payload includes `prior_phase` and `resume_phase` (both required, even when equal). It applies to both delegation and council sessions.
- `atn-control resume` emits `session_resumed` and returns the session to the recorded `resume_phase`. `--blocked-event` must reference the originating `session_blocked` event for that session.
- `atn-control delegate block` remains a delegation-specific compatibility path for `session_blocked`, but the common command is canonical for new documentation.
- Budget and limit blocks are lifted by `atn-control limits extend`, not by `atn-control resume`. Attempting `atn-control resume` against a `session_budget_exceeded`-originated block must fail with a clear error pointing to `atn-control limits extend`.
- Security blocks may be resumed through `atn-control resume` only when the security model defines the violation category as remediable and the remediation has been verified.

Allowed `--category` values for `atn-control block`:

- `external_dependency`
- `user_decision_required`
- `scope_conflict`
- `policy_or_security`
- `budget_or_limit`
- `other`

`budget_or_limit` is allowed for manual moderator blocks but is unusual; daemon-originated budget breaches use `session_budget_exceeded` and are recovered through `atn-control limits extend`, not through `atn-control resume`.

`atn-control status` shows both `status` (derived roll-up: `open`/`blocked`/`terminal`) and `phase` (exact lifecycle phase). Example:

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
atn-control stream <session_id> --member agent-1 --since cursor_... --follow --format ndjson
atn-control stream <session_id> --member agent-1 --from-start --format ndjson
atn-control stream ack <session_id> --member agent-1 --cursor cursor_...
atn-control stream status <session_id>
```

Rules:

- `stream` emits replayed events first, then live events when `--follow` is set. `stream` and `stream status` are read-only.
- `stream ack` emits `stream_cursor_acknowledged`.
- `stream status` includes derived `participant_runtime_readiness` for required participants. Daemon/socket/gateway liveness, transcript/export artifacts, and visible-surface pointers are not inputs to this readiness result.
- Council `status` / `stream status` include derived `discussion_lifecycle` when available. With `limits.max_discussion_turns` configured, `council propose` requires the T0 moderator opening, T1..Tmax selected participant discussion speeches, and one selected closeout speech per participant; `council unresolved` remains available as the fail-closed terminal path. The lifecycle status is additive and fail-closed: operator finality should use `completion_verdict` plus `terminal_visible_closeout_proof_status`, not legacy `propose_ready`, discussion counts, or terminal event presence alone. `completion_verdict=finalized` is valid only when the terminal visible closeout proof status is `posted`.
- Every emitted line includes `event_id`, `cursor`, `session_id`, `type`, `from`, `to`, and `payload`. `from` is a string; `to` is always an array of strings (per `03-protocol-spec.md`).
- The stream command does **not** hide events based on `to`. A member runtime may observe events not addressed to it; it decides whether to act by inspecting event type, sender, recipients, role, phase, and policy.
- Member runtimes must acknowledge processed cursors.
- Disconnects are not fatal; runtimes resume from the last acknowledged cursor.
- A cursor gap or unknown event schema fails closed and requires replay or migration.

If a session is already active, creating another session must fail.

## Limits and budget

### Extend session limits

```bash
atn-control limits extend <session_id> \
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
atn-control limits show <session_id>
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

`atn-control status <session_id> --verbose` includes the same runner accounting block (`runner_calls_total`, `missing_cost_runner_calls_total`, current token and USD totals, any unresolved budget block, and recent runner invocation failures), plus pending escalation batches and waiting-user state. For council sessions, the verbose `council` block also carries the persisted `selected_runner_timeout_evidence` snapshot when present. Later guarded timeout drift still fails closed with timeout-policy-blocked diagnostics, but `stream.status` omits the timeout object and verbose status does not rewrite its historical snapshot.

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

`atn-control limits show` adds an escalation usage block:

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
atn-control delegate new "Implement task A" \
  --to agent-1 \
  --context task.md \
  --acceptance acceptance.md
```

Emits: `session_created` followed by `task_assigned`.

### Member acknowledges assignment

```bash
atn-control delegate ack <session_id> \
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
atn-control delegate message <session_id> \
  --from agent-mod \
  --to agent-1 \
  --kind scope_correction \
  --message "Prioritize A; split B into follow-up."
```

Emits: `delegation_message`.

This command is **not** used to answer a specific `clarification_requested` event. Use `atn-control delegate answer-clarification` for direct clarification answers.

Allowed `--kind` values: `note`, `instruction`, `scope_correction`, `priority_update`, `process_guidance`.

### Member asks clarification

```bash
atn-control delegate clarify <session_id> \
  --from agent-1 \
  --question "Requirement A conflicts with B. Which wins?" \
  --urgency blocked
```

Emits: `clarification_requested`.

### Answer assignee clarification

```bash
atn-control delegate answer-clarification <session_id> \
  --to agent-1 \
  --in-reply-to evt_01HV... \
  --answer "Prioritize A and split B into follow-up scope." \
  --source agent-mod
```

User-derived answer example:

```bash
atn-control delegate answer-clarification <session_id> \
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
atn-control delegate update <session_id> \
  --from agent-1 \
  --progress-status working \
  --summary "Implementation is halfway done." \
  --next-step "Run tests."
```

Emits: `assignee_update`.

`--progress-status` is the assignee's self-reported work status; it is **not** the session lifecycle `phase` and does not directly change the session phase. To move the session phase to `blocked`, use `atn-control delegate block` (or wait for a daemon-internal blocking event such as `session_budget_exceeded`).

Allowed `--progress-status` values: `working`, `blocked`, `partial`, `testing`, `ready_to_submit`.

### Escalate a member question to the user

```bash
atn-control delegate escalate <session_id> \
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

`--delivery` is a **hint to the moderator's Hermes plugin/gateway helper or equivalent gateway skill**, not an instruction to the daemon. The daemon records the value in the `user_escalation_requested` payload and stops there; it never opens an outbound channel itself. The moderator runtime reads the hint, decides how to actually reach the user (Telegram/Slack/Discord/the origin Hermes session/etc.), performs the delivery through Hermes gateway capability, and writes back `user_escalation_delivered` (or `user_escalation_delivery_failed` followed by a fallback delivery) through a typed ATN command. The CLI command remains the canonical fallback for recording the same event.

Recognized hint values:

- `origin`: prefer the original Hermes session.
- `telegram`: prefer the Telegram gateway.
- `origin-or-telegram`: prefer origin, fall back to Telegram on urgency or unreachability.
- `both`: deliver via origin and Telegram.

These names are conventional. The moderator skill may understand additional gateway labels (e.g. `slack`, `discord`); unknown labels are passed through unchanged so the moderator can interpret them.

### Record user escalation delivery

```bash
atn-control delegate escalation-delivered <session_id> \
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
atn-control delegate escalation-delivery-failed <session_id> \
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
atn-control delegate escalation-flush <session_id> \
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
atn-control delegate escalation-batches <session_id>
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
atn-control delegate resolve-escalation <session_id> \
  --escalation evt_user_escalation_requested_01 \
  --answer "A is the priority. Split B into follow-up scope."
```

Batch answer example:

```bash
atn-control delegate resolve-escalation <session_id> \
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
atn-control delegate request-update <session_id> \
  --to agent-1 \
  --reason "No progress update has been recorded recently." \
  --requested-detail progress \
  --requested-detail blockers \
  --requested-detail next_step
```

Emits: `assignee_update_requested`.

### Submit or record artifacts manually

```bash
atn-control delegate submit <session_id> \
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
atn-control delegate review <session_id> \
  --by agent-2 \
  --focus risk,missing-constraints
```

Emits: `review_requested`.

### Reviewer asks assignee clarification

```bash
atn-control delegate review-question <session_id> \
  --from agent-2 \
  --to agent-1 \
  --question "Why did the implementation choose this retry boundary?" \
  --needed-for "Decide whether this is intentional or a bug"
```

Emits: `review_clarification_requested`.

### Assignee answers reviewer clarification

```bash
atn-control delegate review-answer <session_id> \
  --from agent-1 \
  --to agent-2 \
  --answer "The boundary follows the existing timeout policy." \
  --evidence path/to/source.py
```

Emits: `review_clarification_answered`.

### Reviewer submits verdict

```bash
atn-control delegate review-submit <session_id> \
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
atn-control delegate revise <session_id> \
  --to agent-1 \
  --changes changes.md
```

Emits: `revision_requested`.

### Accept

```bash
atn-control delegate accept <session_id>
```

Emits: `work_accepted`.

### Block (delegation compatibility path)

```bash
atn-control delegate block <session_id> \
  --category external_dependency \
  --reason "External dependency is unavailable."
```

Emits: `session_blocked` with envelope `phase: blocked`. The emitted event payload includes `prior_phase` and `resume_phase` (both required, even when equal).

Allowed `--category` values: `external_dependency`, `user_decision_required`, `scope_conflict`, `policy_or_security`, `budget_or_limit`, `other`.

This is a manual block, distinct from daemon-originated `session_budget_exceeded`/`escalation_timeout` which may also move a session into a blocked state per `13-operational-contracts.md`.

`atn-control delegate block` is retained as a delegation-specific compatibility path. The canonical common command for both delegation and council sessions is `atn-control block` (see Common session commands above). Both paths emit the same `session_blocked` event; new documentation should prefer the common command.

## Council commands

### Start council

```bash
atn-control council new "Discuss topic A" \
  --members agent-1,agent-2,agent-3 \
  --moderator agent-mod \
  --request-source discord_thread \
  --requested-output-mode live_visible_thread \
  --visible-output-required true \
  --surface discord-thread \
  --surface-platform discord \
  --thread-id 1507515847227215932
```

Emits: `session_created`.

`--members` defines the council member list. Broadcast council events (`preparation_requested`, `hand_raise_requested`, `draft_conclusion`, `consensus_vote_requested`) use explicit `to` arrays derived from this list. The daemon must not emit `"to": ["all"]` or an omitted `to` field for those events.

Explicit non-visible/local-daemon-only diagnostic creation is a separate approved path and must carry a supported non-visible output mode plus override evidence:

```bash
atn-control council new "Discuss topic A" \
  --members agent-1,agent-2,agent-3 \
  --moderator agent-mod \
  --requested-output-mode local-daemon-only \
  --explicit-non-visible-override true \
  --override-reason "주군 explicitly requested local-daemon-only diagnostics"
```

Discord-thread council binding flags for live-visible mode:

```bash
atn-control council new "Discuss topic A" \
  --members agent-1,agent-2,agent-3 \
  --moderator agent-mod \
  --request-source discord_thread \
  --requested-output-mode live_visible_thread \
  --visible-output-required true \
  --surface discord-thread \
  --surface-platform discord \
  --thread-id 1507515847227215932 \
  --kanban-card t_xxxxx \
  --vault-decision-note docs/decisions/topic-a.md \
  --turn-mode role_order
```

These flags populate `session_created.payload.request_context`, optional `session_created.payload.surface`, `session_created.payload.linked_authority`, and `session_created.payload.turn_mode`. They do not authorize Discord API access by `atn-controld`, and they do not make Discord the source of truth. `--turn-mode` is the session-level intended/default floor policy only; each actual floor grant still records `speaker_selected.payload.selection_mode` as the per-turn audit fact.

### Attendance and agenda lock

```bash
atn-control council request-attendance <session_id> --timeout 5m
atn-control council attend <session_id> --from agent-1 --status present --summary "Present."
atn-control council lock-agenda <session_id> \
  --decision-question "Decide next action for Kanban card t_xxxxx" \
  --success-criteria "Consensus identifies the bounded next action, owner, and evidence requirement" \
  --out-of-scope-policy "New topics become follow-up card candidates, not current-thread expansion" \
  --max-rounds 2
```

Emits:

- `council request-attendance` → `attendance_requested`.
- `council attend` → `member_attended`.
- `council lock-agenda` → `agenda_locked`.

`council lock-agenda --from-file <agenda.json>` is a command-specific structured JSON path, not the legacy raw-draft file path used by conclusion commands. The file must be a JSON object with exactly these supported fields:

- required non-empty strings: `decision_question`, `success_criteria`, `out_of_scope_policy`
- optional positive integer: `max_rounds`

Missing required fields, empty/non-string required values, malformed JSON, and unsupported fields such as `summary`, `notes`, `intent`, `draft_version`, `timeout_sec`, `research_timeout_sec`, or `agenda_items` fail closed before daemon submission. Inline CLI flags remain available for `--decision-question`, `--success-criteria`, `--out-of-scope-policy`, and `--max-rounds`; unrelated council subcommands keep their existing `--from-file` behavior.

Example agenda file:

```json
{
  "decision_question": "Decide next action for Kanban card t_xxxxx",
  "success_criteria": "Consensus identifies the bounded next action, owner, and evidence requirement",
  "out_of_scope_policy": "New topics become follow-up card candidates, not current-thread expansion",
  "max_rounds": 2
}
```

First-pass rule: these commands operate while the council is in `created`. `council prepare` remains the transition into `preparation`. For `surface.kind=discord_thread`, these are not optional presentation commands: `council prepare` must fail closed unless `attendance_requested`, one terminal `member_attended` record per required participant (`present`, `partial`, `unavailable`, or `no_response_timeout`), and `agenda_locked` already exist. Rejection leaves the session in `created` and appends no event.

### Preparation

```bash
atn-control council prepare <session_id> --timeout 10m
```

Emits: `preparation_requested`.

For Discord-thread-bound councils, this command validates the attendance/agenda guard above before emitting `preparation_requested`. The CLI must surface missing prerequisites with a clear error naming the missing attendance member(s) or missing `agenda_locked` event.

### Member marks preparation ready

```bash
atn-control council ready <session_id> \
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
atn-control council prepared-partial <session_id> \
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
atn-control council poll <session_id> --research-timeout 10m
atn-control council hand-raise <session_id> --from agent-3 --intent rebuttal --relevance 5 --urgency 4 --reason "This changes the risk decision."
atn-control council grant <session_id> --auto
atn-control council grant <session_id> --to agent-3
atn-control council grant <session_id> --member agent-3
atn-control council grant <session_id> --to agent-3 --mode role_order --round 1 --reason "Round 1 risk review"
atn-control council speak <session_id> --from agent-3 --stdin
```

Emits:

- `council poll` → `hand_raise_requested`.
- `council hand-raise` → `hand_raise`.
- `council grant` → `speaker_selected`.
- `council speak` → `speech`.

`council grant --mode <mode>` writes the per-turn `speaker_selected.payload.selection_mode`. If a session was created with `--turn-mode`, the grant mode may match that default or deliberately deviate from it. Any deviation requires `--reason`; the persisted `speaker_selected` event must include that reason so audit/replay can explain the difference.

`council grant --member <id>` is an operator alias for `--to <id>`. Supplying both with different values fails closed. For selected-runner councils, `grant` derives `speaker_selected.payload.stance_assignment` only from a matching same-member/same-turn `hand_raise.payload.intent` or `hand_raise.payload.reason`; caller-supplied grant payload stance is not authority, and a grant without that stance-bearing hand raise is rejected before appending `speaker_selected`.

### Intervene

```bash
atn-control council intervene <session_id> \
  --to agent-2 \
  --reason "topic drift" \
  --message "Tie this back to the decision criteria or withdraw it."
```

Emits: `moderator_intervention`.

### Propose, vote, revise, finish

```bash
atn-control council propose <session_id> --from-file draft.md
atn-control council request-vote <session_id> --draft-version 1 --timeout 10m
atn-control council vote <session_id> --from agent-3 --vote approve_with_conditions --reason "..." --required-change "..."
atn-control council revise <session_id> --from-file draft_v2.md --reason "Addressed agent-2 block vote."
atn-control council finalize <session_id> \
  --authority-return-status posted \
  --kanban-comment-id kc_123 \
  --vault-decision-note docs/decisions/topic-a.md
atn-control council unresolved <session_id> --reason "persistent blocking objection"
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
| `session_blocked` | `participant_cli` | `atn-control block ...`; `atn-control delegate block ...` for delegation compatibility |
| `session_resumed` | `participant_cli` | `atn-control resume ...` |
| `session_cancelled` | `participant_cli` | `atn-control cancel ...` |

### Delegation matrix

| Event | Origin class | Command path |
|---|---|---|
| `session_created` | `daemon_after_cli` | `atn-control delegate new ...` |
| `task_assigned` | `daemon_after_cli` | `atn-control delegate new ...` |
| `assignee_acknowledged` | `participant_cli` | `atn-control delegate ack ...` |
| `clarification_requested` | `participant_cli` | `atn-control delegate clarify ...` |
| `clarification_answered` | `participant_cli` | `atn-control delegate answer-clarification ...` |
| `delegation_message` | `participant_cli` | `atn-control delegate message ...` |
| `assignee_update_requested` | `participant_cli` | `atn-control delegate request-update ...` |
| `assignee_update` | `participant_cli` | `atn-control delegate update ...` |
| `user_escalation_requested` | `mixed` | `atn-control delegate escalate ...`; `atn-control delegate escalation-flush ...`; daemon runtime batch flush |
| `user_escalation_delivered` | `participant_cli` | `atn-control delegate escalation-delivered ...` |
| `user_escalation_delivery_failed` | `participant_cli` | `atn-control delegate escalation-delivery-failed ...` |
| `user_escalation_resolved` | `participant_cli` | `atn-control delegate resolve-escalation ...` (semantic originator is the user; `from: "user"`) |
| `work_submitted` | `participant_cli` | `atn-control delegate submit ...` |
| `review_requested` | `participant_cli` | `atn-control delegate review ...` |
| `review_clarification_requested` | `participant_cli` | `atn-control delegate review-question ...` |
| `review_clarification_answered` | `participant_cli` | `atn-control delegate review-answer ...` |
| `review_submitted` | `participant_cli` | `atn-control delegate review-submit ...` |
| `revision_requested` | `participant_cli` | `atn-control delegate revise ...` |
| `work_accepted` | `participant_cli` | `atn-control delegate accept ...` |
| `session_blocked` | `participant_cli` | `atn-control block ...`; `atn-control delegate block ...` (delegation compatibility) |
| `session_resumed` | `participant_cli` | `atn-control resume ...` |
| `session_cancelled` | `participant_cli` | `atn-control cancel ...` |

### Council matrix

| Event | Origin class | Command path |
|---|---|---|
| `session_created` | `daemon_after_cli` | `atn-control council new ...` |
| `attendance_requested` | `participant_cli` | `atn-control council request-attendance ...` |
| `member_attended` | `mixed` | `atn-control council attend ...` when participant-originated (`from: <member>`); daemon-internal on timeout (`from: "atn-controld"`, `payload.member` records the affected member) |
| `agenda_locked` | `participant_cli` | `atn-control council lock-agenda ...` |
| `preparation_requested` | `daemon_after_cli` | `atn-control council prepare ...` |
| `member_ready` | `participant_cli` | `atn-control council ready ...` |
| `member_prepared_partial` | `mixed` | `atn-control council prepared-partial ...` when participant-originated (`from: <member>`); daemon-internal on timeout (`from: "atn-controld"`, `payload.member` records the affected member) |
| `hand_raise_requested` | `daemon_after_cli` | `atn-control council poll ...` |
| `hand_raise` | `participant_cli` | `atn-control council hand-raise ...` |
| `speaker_selected` | `participant_cli` | `atn-control council grant ...` |
| `speech` | `participant_cli` | `atn-control council speak ...` |
| `moderator_intervention` | `participant_cli` | `atn-control council intervene ...` |
| `draft_conclusion` | `participant_cli` | `atn-control council propose ...` or `atn-control council revise ...` |
| `consensus_vote_requested` | `participant_cli` | `atn-control council request-vote ...` |
| `consensus_vote` | `participant_cli` | `atn-control council vote ...` |
| `council_finalized` | `participant_cli` | `atn-control council finalize ...` |
| `council_unresolved` | `participant_cli` | `atn-control council unresolved ...` |
| `session_blocked` | `participant_cli` | `atn-control block ...` (council uses the common block path) |
| `session_resumed` | `participant_cli` | `atn-control resume ...` |
| `session_cancelled` | `participant_cli` | `atn-control cancel ...` |

### Operational matrix

| Event | Origin class | Public write command needed? |
|---|---|---|
| `session_budget_exceeded` | `daemon_internal` | No |
| `limits_extended` | `participant_cli` | Yes — `atn-control limits extend ...` |
| `runner_retry_attempted` | `daemon_internal` | No |
| `escalation_deduplicated` | `mixed` | No — CLI escalation attempts may cause it; daemon-internal reconciliation may also cause it |
| `escalation_rate_limited` | `mixed` | No — CLI escalation or flush attempts may cause it; daemon-internal timer flush may also cause it |
| `security_violation` | `daemon_internal` | No |
| `redaction_applied` | `daemon_internal` | No |
| `escalation_batched` | `mixed` | No — `atn-control delegate escalate ...` may cause it; daemon-internal batch updates do not require a public command |
| `escalation_timeout` | `daemon_internal` | No |
| `runner_invocation_started` | `daemon_internal` | No |
| `runner_invocation_failed` | `daemon_internal` | No |
| `runner_result_discarded` | `daemon_internal` | No |
| `session_handle_rotated` | `daemon_internal` | No |
| `stream_subscriber_connected` | `daemon_internal` | No |
| `stream_cursor_acknowledged` | `daemon_after_cli` | Yes — `atn-control stream ack ...` |
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
      "atn-control status sess_... --verbose",
      "atn-control resume sess_... --blocked-event evt_...",
      "atn-control cancel sess_... --reason ..."
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
- atn-control status sess_20260425_0130_a
- atn-control delegate accept sess_20260425_0130_a
- atn-control cancel sess_20260425_0130_a
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
- atn-control status sess_20260425_0130_a --verbose
- atn-control limits show sess_20260425_0130_a
- atn-control limits extend sess_20260425_0130_a --key max_usd --value 50.00 --authorized-by user --reason "Approved additional budget."
- atn-control cancel sess_20260425_0130_a --reason "User cancelled blocked session."
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
- atn-controld
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
- atn-control limits show sess_20260425_0130_a
- atn-control limits extend sess_20260425_0130_a --key max_runner_calls --value 750 --authorized-by user --reason "Approved additional runner calls."
- atn-control cancel sess_20260425_0130_a --reason "Budget exhausted."
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
