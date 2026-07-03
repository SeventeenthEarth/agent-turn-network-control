# Architecture

## Architectural principle

ATN control/runtime follows a daemon-authority architecture. Domain policy is separated from adapters. The daemon owns state transitions, event append, locks, replay, and projections. The CLI and plugin are clients of the daemon contract, not alternate authorities.

## Repository layout target

```text
atn-control/
  cmd/
    atn-control/       # Go CLI main
    atn-controld/      # Go daemon main
  internal/
    daemon/                     # process, local transport, stream hub
    cli/                        # canonical command handlers and stream client
    protocol/                   # command/event/stream/error models
    registry/                   # registry loader, validation, snapshots
    storage/                    # channel.jsonl, SQLite projection, replay
    engine/                     # state machines and policy decisions
    runtime/                    # member runtime coordination helpers
    runner/                     # bounded runner adapters
    transcript/                 # markdown/jsonl/brief renderers
    observability/              # metrics, health, structured diagnostics
    recovery/                   # verify/rebuild/repair helpers
  testdata/
    conformance/                # protocol fixtures consumed by plugin repo
  docs/
  Makefile
```

The companion plugin repository targets:

```text
atn-plugin/
  src/atn_plugin/
    client/                     # Python daemon client for protocol contract
    tools/                      # Hermes tool handlers
    slash_commands/             # Hermes command bindings when supported
    discord_surface/            # gateway/send_message helpers
    skills/                     # bundled Hermes skill material
  tests/
  docs/
  Makefile
```

## Runtime flow

```text
Moderator/member runtime or operator
  -> CLI or Python plugin client
    -> ATN command envelope
      -> atn-controld local transport
        -> validate registry identity and state transition
        -> append channel.jsonl
        -> update SQLite projection
        -> publish stream frame
          -> CLI stream, plugin stream, member runtime observers
```

The Go CLI and Python plugin do not share source code. They share the protocol contract and conformance fixtures. Any behavior implemented in both clients must be verified through cross-language conformance tests.

Post-Release live-local work is governed by `24-live-transport-control-sot.md`. The control `LTRAN`, `MEMBR`, and `SURFD` epics own daemon/CLI compatibility, real participant runtime invocation, and delivery-evidence projection respectively; companion plugin epics start only after the corresponding control epic boundary is complete.

## Authority boundaries

- `atn-controld` is the only component that mutates `channel.jsonl`, SQLite projections, session locks, cursor state, and replay state.
- `atn-control` CLI is canonical for diagnostics, recovery, manual operation, and plugin-failure fallback.
- `atn-plugin` is the preferred Hermes UX surface, but the plugin is not the source of truth.
- Discord is a visible room/evidence pointer, never a state authority.
- Hermes core is not patched.

## Plugin boundary

The plugin may:

- call daemon status/session/status/stream/command endpoints;
- expose Hermes tools and slash commands for implemented daemon commands;
- use Hermes gateway/send_message for visible Discord helper posts;
- record delivery evidence by sending typed commands to the daemon;
- tell the operator to use CLI fallback when the daemon/plugin compatibility check fails.

The plugin must not:

- append or rewrite `channel.jsonl`;
- mutate SQLite projections or lock files directly;
- reinterpret daemon errors as success;
- require raw Discord tokens in the daemon;
- use live Hermes/Discord resources in normal unit or integration tests.

## Protocol compatibility

The daemon exposes a version/health contract including daemon version, protocol version, supported feature flags, and minimum compatible plugin protocol. The plugin fails closed when required features are absent.

Conformance fixtures under `testdata/conformance/` are normative for cross-repo behavior. The plugin repo may copy fixtures for stability but must track the control protocol version.

## Discord-thread council surface

Discord-thread council support is a surface binding over the council state machine:

```text
Discord thread            # human-visible room
Hermes plugin/gateway     # posts visible messages
ATN daemon                # records typed events and delivery evidence
channel.jsonl             # canonical SOT
Kanban/Vault              # optional linked authority return path
```

The daemon stores external message IDs and delivery status as evidence fields only. It never derives lifecycle state from free-form Discord text.

---

## Merged from `docs/spec/architecture.md`

# Session State Machines

## Phase versus status

`phase` is the exact lifecycle state used by domain logic and stored in every event envelope. `status` is a derived roll-up (`open`/`blocked`/`terminal`) used for query, UI, and active-session lock checks; it is stored only in projections (`session.yaml`, SQLite). See `architecture.md#phase-and-status` for the deterministic mapping and `operations.md` §5 for the concurrency contract.

Every event envelope records the **post-transition** `phase`. For example, when `clarification_requested` moves a delegation session from `working` to `needs_clarification`, the event envelope has `phase: "needs_clarification"`. When `session_budget_exceeded` blocks a session, the event envelope has `phase: "blocked"` and the payload records `prior_phase` and `resume_phase`.

State transitions are driven by event type and phase, not by recipient visibility alone. The event `to` field is semantic addressing (per `protocol-and-cli.md`); it helps runtimes decide whether to act, but it is not a state-machine transition guard by itself.

## Global single active session rule

```text
idle
  -> active session of type delegation or council
  -> terminal state
  -> idle
```

A session is active whenever its derived `status` is not `terminal`:

- `status: open` is active.
- `status: blocked` is active.
- `status: terminal` is not active.

The active-session lock is released only when the session reaches a terminal phase. The normative concurrency rule (what counts as "active", how `blocked` interacts with the lock, how future releases may extend this without breaking Release v1) lives in `operations.md` §5 Concurrency model.

# Delegation state machine

```text
created
  -> assigned
  -> acknowledged
  -> working
  -> needs_clarification
  -> waiting_user
  -> working
  -> submitted
  -> under_review
  -> revision_requested
  -> working
  -> accepted | blocked | cancelled
```

## Delegation states

### assigned

Task has been sent to the assignee.

Typical incoming event: `task_assigned`.
Expected next participant event: `assignee_acknowledged`, emitted by `delegate ack`.

### acknowledged

Assignee has confirmed understanding and may provide a plan or questions.

### working

Assignee is doing the work and may send progress updates, blockers, or partial results.

### needs_clarification

Assignee needs a decision from the moderator or the user. The moderator answers directly when the answer is already determined by policy, prior user decisions, or task context. Otherwise the moderator escalates to the user.

Entry trigger: `clarification_requested` (`delegate clarify`).

Exit triggers:

- `clarification_answered` by `delegate answer-clarification` → `working`
- `user_escalation_requested` by `delegate escalate` → `waiting_user`

If the moderator decides that a clarification requires user authority, there are two paths:

1. **Immediate escalation**: record `user_escalation_requested` and transition to `waiting_user`.
2. **Low-urgency non-blocking batching**: record `escalation_batched`, remain in the current phase, and later — when the batch is flushed — record `user_escalation_requested` and transition to `waiting_user`.

A blocking clarification must not be batched as low urgency.

### waiting_user

A user-facing escalation has been recorded through `user_escalation_requested`. The session is paused because user authority is required before continuing.

`escalation_batched` alone does **not** enter `waiting_user`; the session remains in the prior phase while a low-urgency batch is pending.

The daemon has recorded `user_escalation_requested` with a `delivery_policy` hint; actual delivery (origin Hermes session, Telegram, Slack, Discord, etc.) is performed by the Hermes plugin/gateway helper or equivalent moderator runtime gateway skill, which writes back `user_escalation_delivered` once it succeeds.

Delivery audit events recorded by the moderator runtime:

- `user_escalation_delivered` (`delegate escalation-delivered`)
- `user_escalation_delivery_failed` (`delegate escalation-delivery-failed`)

These events record delivery status only. They do not by themselves resolve the user decision; resolution requires `user_escalation_resolved` (`delegate resolve-escalation`) followed by `clarification_answered --source user`.

The `user_escalation_requested` event records `prior_phase` and `resume_phase`. After `user_escalation_resolved`, the moderator must relay the answer to the affected member or reviewer through the appropriate answer event.

Exit conditions:

- `user_escalation_resolved` recorded → return to the recorded `resume_phase`
- user cancels scope → `cancelled` or `blocked`
- `escalation_response_timeout_sec` exceeded after `user_escalation_requested` → `blocked` with `escalation_timeout`

### submitted

Assignee has submitted artifacts or a result.

### under_review

The moderator or a reviewer is checking the submitted result.

Subflow:

```text
review_requested
  -> review_clarification_requested
  -> review_clarification_answered
  -> review_submitted
  -> revision_requested | work_accepted
```

Reviewer questions should normally go to the assignee, not the moderator. The moderator coordinates the exchange, enforces scope, and escalates to the user only when the question requires user authority.

### revision_requested

The moderator has requested specific changes.

### accepted

The moderator has accepted the work and the session is complete.

### blocked

The work cannot continue without user decision or external dependency. `blocked` is recoverable, not terminal.

Runner budget breach can enter `blocked`:

- `session_budget_exceeded` with `limit_kind: max_runner_calls` (pre-dispatch check)
- `session_budget_exceeded` with `limit_kind: max_tokens_total` (post-event check)
- `session_budget_exceeded` with `limit_kind: max_usd` (post-event check)
- `session_budget_exceeded` with `limit_kind: max_elapsed_sec`

`max_runner_calls` is checked before launching another runner invocation; token and USD limits are checked after terminal runner events update observed cost totals (see `operations.md` §3).

Escalation timeout can enter `blocked` only after a `user_escalation_requested` exists. Pending low-urgency batches do not by themselves time out into `blocked`; they may flush into `user_escalation_requested`, be cancelled, or be rate-limited.

Exit conditions:

- Budget or limit block resolved by `limits_extended` with user authorization → return to the recorded `resume_phase` (typically `working` or `under_review`).
- Manual, external-dependency, scope-conflict, or process block resolved by `session_resumed` → return to the recorded `resume_phase`.
- Security block resolved by verified remediation and an allowed `session_resumed` path → return to the recorded `resume_phase`.
- User cancels → `cancelled`.

`session_resumed` lifts manual, external-dependency, scope-conflict, policy/process, and (when remediable) security blocks. `limits_extended` lifts only budget or limit blocks recorded by `session_budget_exceeded`. The lifting event must reference the original blocking event.

Only `accepted` and `cancelled` are truly terminal for delegation sessions.

Phase summary (mapped to `status` per `architecture.md#phase-to-status-mapping`):

- Terminal phases: `accepted`, `cancelled`.
- Recoverable blocked phase: `blocked`.
- All other phases are open.

# Council state machine

```text
created
  -> preparation
  -> discussion
  -> draft_conclusion
  -> consensus_vote
  -> finalized | unresolved | blocked | cancelled
```

Any non-terminal council phase may transition to `blocked` due to budget breach, session-scoped security violation, operational timeout, or explicit moderator block. `blocked` is recoverable. When the blocking condition is resolved, the session returns to `resume_phase` recorded by the blocking event (or by the projection rebuilt from that event).

## Council states

### created

The council session has been created but preparation has not yet started.

Entry trigger: `session_created` (`council new`).

Exit trigger: `preparation_requested` (`council prepare`) → `preparation`.

`council new` creates the session in `created` and emits only `session_created`. The preparation timeout does not start until `council prepare` records `preparation_requested`.


Discord-thread council first pass keeps attendance and agenda lock as typed events inside `created`:

```text
session_created
  -> attendance_requested
  -> one terminal member_attended record per required participant
  -> agenda_locked
  -> preparation_requested
```

This avoids adding a new `attendance` phase in the initial spec alignment. For `surface.kind=discord_thread`, the attendance/agenda sequence is mandatory before `preparation_requested`. `council prepare` fails closed while remaining in `created` if `attendance_requested` is missing, any required participant lacks a terminal `member_attended` record (`present`, `partial`, `unavailable`, or `no_response_timeout`), or `agenda_locked` is missing. If later review chooses a true `attendance` phase, `protocol-and-cli.md`, `protocol-and-cli.md`, this state machine, storage projections, replay migrations, and acceptance tests must be updated together.

### preparation

Members research the topic for up to 10 minutes.

Entry trigger: `preparation_requested` (`council prepare`).

Exit condition:

- all required members record `member_ready` (`council ready`), or
- preparation timeout records `member_prepared_partial` for timed-out members (origin class `daemon_internal`), or members record `member_prepared_partial` themselves before timeout (`council prepared-partial`).

### discussion

Turn-based moderated discussion using hand raises and speaker selection.

Per-turn sequence:

```text
hand_raise_requested
  -> hand_raise responses collected within 10 minutes
  -> speaker_selected
  -> speech collected
  -> optional moderator_intervention
  -> next turn or draft_conclusion
```

### draft_conclusion

The moderator drafts a conclusion from the discussion.

`draft_conclusion` may be emitted by either `council propose` (first version) or `council revise` (subsequent versions).

### consensus_vote

Members vote on the draft. A single block prevents finalization.

Entry trigger: `consensus_vote_requested` (`council request-vote`). Members then emit `consensus_vote` (`council vote`) only after observing the request event or its replay.

### blocked

The council cannot continue because a recoverable blocking condition occurred (budget breach, session-scoped security violation, stale runtime that the moderator chooses to block on, or another explicit moderator block).

Runner budget breach examples that can enter `blocked`: `session_budget_exceeded` with `limit_kind` of `max_runner_calls`, `max_tokens_total`, `max_usd`, or `max_elapsed_sec`. `max_runner_calls` is checked before launching another runner invocation; token and USD limits are checked after terminal runner events update observed cost totals (see `operations.md` §3).

`blocked` is **not** terminal.

Exit conditions:

- `limits_extended` with user authorization → return to the recorded `resume_phase` when the block was a budget or limit breach.
- `session_resumed` (recorded by `atn-control resume`) → return to the recorded `resume_phase` for explicit moderator blocks, external-dependency blocks, scope-conflict blocks, or policy/process blocks. `session_resumed` may also lift verified security blocks where the security model defines the violation as remediable.
- the moderator cancels the session → `cancelled`.

`limits_extended` lifts only budget or limit blocks. `session_resumed` lifts manual or external-dependency blocks; it must not be used for budget or limit blocks recorded by `session_budget_exceeded`.

### finalized

A consensus decision exists.

If the council has `linked_authority`, `finalized` means the council decision has been recorded, not necessarily that Kanban/Vault authority return is complete. Completion of the return path depends on `linked_authority_result.status: posted` plus evidence. `failed` or `pending_followup` status keeps the origin authority path blocked/pending review or requires a linked follow-up outside the council state machine.

### unresolved

The council did not reach consensus.

## No-hand-raise policy

The normative no-hand-raise policy lives in `architecture.md`. The state machine only observes the outcome of that policy: a return to a new hand-raise round, a transition to `draft_conclusion`, or a transition to `unresolved`. Speaker selection decisions (targeted question, role-relevant pick, random fallback) are policy, not state, and must not be duplicated here.

---

## Merged from `docs/spec/architecture.md`

# Storage Schema

## Data root

The data root (`<data_home>`) resolves in this order:

1. `$ATN_HOME` if set,
2. `$XDG_DATA_HOME/agent-turn-network` if `$XDG_DATA_HOME` is set,
3. `~/.atn/` otherwise.

Layout under `<data_home>`:

```text
<data_home>/
  network.sqlite
  active_session.lock
  registry.yaml                 # user-edited registry SOT
  operational.log               # daemon operational log; see 12-operations.md
  run/
    atn-controld.sock         # daemon Unix socket (HTTP transport)
  runtime/
    <member>/
      stream_cursor             # member runtime's last acknowledged cursor
  sessions/
    <session_id>/
      session.yaml
      channel.jsonl
      transcript.md
      brief.md
      registry_snapshot.yaml    # frozen copy of registry.yaml at session creation
      participants/
        agent-1/
          session_id
          stream_cursor
          notes.md
          logs/
        agent-2/
      artifacts/
      exports/
      raw_logs/
```

`registry.yaml` (the user-edited file) and `registry_snapshot.yaml` (the per-session frozen copy) are distinct files. The daemon reads `registry.yaml` at start-up and again at session creation; at session creation it writes the snapshot so post-start edits to `registry.yaml` do not change a session's recorded participants. At session creation, the daemon reads from a safely opened and validated registry file, computes its SHA-256 hash, and writes a frozen `registry_snapshot.yaml` into the session directory atomically before appending `session_created`. Subsequent edits to `registry.yaml` do not change that session's recorded participants.

## Data home permission contract

The resolved `<data_home>` must be a safe directory:

- owner: current daemon user;
- not group-writable;
- not world-writable;
- recommended mode: `0700`.

If `<data_home>` does not exist, setup/init code may create it with mode `0700`. Daemon start must fail closed if an existing `<data_home>` has unsafe ownership or permissions; permission repair is allowed only through an explicit setup/repair command.

Security-sensitive files within `<data_home>`:

- `<data_home>/registry.yaml` (recommended mode `0600`)
- `<data_home>/network.sqlite`
- `<data_home>/active_session.lock`
- `<data_home>/operational.log`
- `<data_home>/run/atn-controld.sock`
- `<data_home>/sessions/<session_id>/registry_snapshot.yaml` (recommended mode `0600`)

The normative permission rules for registry and wrapper validation live in `12-operations.md`.

## Registry snapshot

`registry_snapshot.yaml` is written atomically during session creation. Atomic write procedure:

1. write `registry_snapshot.yaml.tmp` in the target session directory;
2. `fsync` the file when possible;
3. `chmod 0600`;
4. rename to `registry_snapshot.yaml`;
5. `fsync` the parent directory when possible.

If `registry_snapshot.yaml` cannot be written atomically, session creation must fail before `session_created` is appended. A session must not exist without its registry snapshot.

`registry_snapshot.yaml` includes source metadata plus the loaded members:

```yaml
snapshot_metadata:
  source_path: "<data_home>/registry.yaml"
  source_sha256: "sha256:..."
  loaded_at: "2026-04-25T00:00:00Z"
  loaded_by_uid: 501
  schema_version: 1
members:
  agent-mod:
    display_name: Moderator
    wrapper: agent-mod-wrapper
    adapter_kind: hermes-agent
    workspace: <workspace_root>/agent-mod
    role: moderator
    enabled: true
```

`source_sha256` is for audit and debugging; the snapshot content itself remains the source of truth for that session's participants. The snapshot is a session artifact and is treated as part of the session's immutable authority record.

## Source of truth

- `channel.jsonl` is the event SOT.
- `network.sqlite` is a query and status projection.
- `transcript.md` and `brief.md` are generated renderings.

`atn-control storage rebuild-projection` (defined in `protocol-and-cli.md`) rebuilds `network.sqlite` and other projections from `channel.jsonl`. It is an operational command and must not append events, invoke runners, deliver escalations, or invent timer-driven events. Detailed disaster-recovery procedure lives in `17-operations.md`.

Release v1 schema implementation creates all named projection tables plus `projection_metadata`. Metadata records `schema_version`, `source_session_count`, `source_event_count`, and deterministic `source_hash`. Empty event-specific tables mean no matching durable event was replayed; they are not deferred schema.

## Session YAML

```yaml
id: sess_20260425_013000_a
session_type: delegation
status: open
title: Implement task A
moderator: agent-mod
participants: [agent-mod, agent-1]
surface:
  kind: discord_thread
  platform: discord
  guild_id: optional
  channel_id: optional_parent_channel_id
  thread_id: "1507515847227215932"
  origin_message_id: optional
  started_by: user
  delivery_owner: moderator_runtime
linked_authority:
  kanban_card_id: t_xxxxx
  vault_decision_note: optional
created_at: 2026-04-25T01:30:00Z
limits:
  max_turns: 300
  max_consensus_rounds: 20
  consensus_round_warning_threshold: 3
  no_progress_round_limit: 3
  preparation_timeout_sec: 600
  hand_raise_research_timeout_sec: 600
  dispatch_timeout_sec: 30  # implemented selected-speaker default when no session override is set; live-visible councils now require the implemented NEWFIX-005 policy of 120 or an approved explicit alternative
  clarification_response_timeout_sec: 3600
  escalation_response_timeout_sec: 86400
  runner_max_retries: 2
  max_runner_calls: 500
  max_tokens_total: 2000000
  max_usd: 25.00
  max_user_escalations: 10
  max_elapsed_sec: 86400
  max_artifact_bytes: 26214400
  stream_heartbeat_interval_sec: 30
  stream_stale_threshold_sec: 90
  stream_repoll_threshold_sec: 300
  council:
    speaker_scoring:
      w_relevance: 3
      w_urgency: 2
      w_role_match: 3
      # bonus values and recent_speaker_penalty are defined in architecture.md
      # and are overridable per session by listing them here.
state:
  phase: working
  current_turn: 4
  last_speaker: null
  consensus_round: 0
cost:
  tokens_in_total: 0
  tokens_out_total: 0
  usd_estimate_total: 0.0
  runner_calls_total: 0
  missing_cost_runner_calls_total: 0
escalations:
  user_escalations_total: 0
  pending_batches_total: 0
  pending_batched_candidates_total: 0
  waiting_user: null
```

`surface` and `linked_authority` are optional session metadata projected from `session_created.payload`. Projection preserves these values for status, transcript, and export convenience only; replay authority remains the original `channel.jsonl` event. Discord message ids and thread ids are evidence pointers and must not be used for event ordering.

`selected_runner_timeout_evidence` is optional council session metadata persisted from `session_created.payload` when `NEWFIX-005` applies. Its typed object has:
- `policy_required`
- `configured_timeout_sec`
- `effective_timeout_sec`
- `effective_source`
- `approved_alternative`
- `approval_basis`
- `compliant`

`configured_timeout_sec` is the persisted guarded timeout, and the persisted `effective_timeout_sec` / `effective_source` fields record the normalized timeout snapshot accepted at session creation. Later guarded-lane drift is checked separately at selected-runner launch time; when the daemon detects drift it emits `selected_runner_dispatch_failed` with `reason=selected_runner_timeout_policy_blocked` plus a re-evaluated timeout-evidence payload instead of rewriting the stored session snapshot.

For a Discord-thread-bound council, storage and projection are mandatory for the whole authority trail, not optional display polish. The event log must persist `attendance_requested`, one terminal `member_attended` event for each required participant, `agenda_locked`, and `council_finalized.payload.linked_authority_result` when `linked_authority` is configured. SQLite/status projections, transcript, and export must expose those facts so an operator can verify attendance, locked agenda, and Kanban/Vault return evidence without reinterpreting Discord messages.

`user_escalations_total` belongs to the `escalations` projection summary, not to the `cost` block. The `escalations` block is a projection summary (source of truth remains `channel.jsonl`). When the session is in `waiting_user`, `escalations.waiting_user` carries the active `escalation_event_id`, `batch_id`, `delivered` flag, and `response_timeout_at`. `user_escalations_total` counts only `user_escalation_requested` events; `escalation_batched` does not count.

`runner_calls_total` is projected from `runner_invocation_started` events. `missing_cost_runner_calls_total` is projected from terminal runner events whose `cost` is null. Token and USD totals are projected only from terminal runner events with parsed cost. See `operations.md` §3.

Replay handlers populate event-specific tables from durable events only: runner invocation lifecycle, escalation batching and batched user escalation, council attendance/agenda/hand-raise/vote/linked-authority results, council argument-graph relation evidence from `speech` payloads, stream cursor/subscriber events, review verdict/submission events, command-id summaries, event recipients, and artifact references from artifact-bearing work/review events.

Unknown forward-compatibility keys (limits introduced after Release v1) must not error on a Release v1 reader. See the forward-compatibility rule in `operations.md`.

### Phase and status

`phase` (under `state.phase`) is the exact lifecycle state from `architecture.md`. `status` is a derived roll-up used for query, UI, and active-session lock checks.

Allowed `status` values:

- `open`
- `blocked`
- `terminal`

The mapping from `phase` to `status` is deterministic:

- terminal phases map to `terminal`;
- `blocked` maps to `blocked`;
- all other non-terminal phases map to `open`.

The daemon derives `status` from `phase` during event append and replay. Implementations must not accept arbitrary `status` values from CLI commands.

### Blocked session example

```yaml
id: sess_20260425_013000_a
session_type: delegation
status: blocked
title: Implement task A
moderator: agent-mod
participants: [agent-mod, agent-1]
state:
  phase: blocked
  blocked_by_event_id: evt_01HV...
  prior_phase: under_review
  resume_phase: under_review
cost:
  tokens_in_total: 10000
  tokens_out_total: 5000
  usd_estimate_total: 25.17
  runner_calls_total: 20
  missing_cost_runner_calls_total: 0
escalations:
  user_escalations_total: 2
```

### Terminal session example

```yaml
id: sess_20260425_013000_a
session_type: delegation
status: terminal
state:
  phase: accepted
closed_at: 2026-04-25T05:00:00Z
```

`prior_phase`, `resume_phase`, and `blocked_by_event_id` are projection fields; the source of truth is the blocking event payload in `channel.jsonl`.

When `session_resumed` is applied, the daemon sets `sessions.phase` to the event envelope `phase`, derives `sessions.status` from that phase, and clears `prior_phase`, `resume_phase`, and `blocked_by_event_id`. `session_resumed` lifts manual, external-dependency, and policy blocks; budget and limit blocks remain lifted only by `limits_extended` (see `operations.md` §5).

## Phase to status mapping

### Delegation

| phase | status |
|---|---|
| `created` | `open` |
| `assigned` | `open` |
| `acknowledged` | `open` |
| `working` | `open` |
| `needs_clarification` | `open` |
| `waiting_user` | `open` |
| `submitted` | `open` |
| `under_review` | `open` |
| `revision_requested` | `open` |
| `blocked` | `blocked` |
| `accepted` | `terminal` |
| `cancelled` | `terminal` |

### Council

| phase | status |
|---|---|
| `created` | `open` |
| `preparation` | `open` |
| `discussion` | `open` |
| `draft_conclusion` | `open` |
| `consensus_vote` | `open` |
| `blocked` | `blocked` |
| `finalized` | `terminal` |
| `unresolved` | `terminal` |
| `cancelled` | `terminal` |

## SQLite projection

### sessions

```sql
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  session_type TEXT NOT NULL,
  title TEXT NOT NULL,
  moderator TEXT NOT NULL,
  status TEXT NOT NULL,           -- derived roll-up: open | blocked | terminal
  phase TEXT NOT NULL,            -- exact lifecycle phase
  prior_phase TEXT,               -- set when phase = blocked; from blocking event payload
  resume_phase TEXT,              -- set when phase = blocked; from blocking event payload
  blocked_by_event_id TEXT,       -- set when phase = blocked; references blocking event
  current_turn INTEGER NOT NULL DEFAULT 0,
  consensus_round INTEGER NOT NULL DEFAULT 0,
  last_speaker TEXT,
  tokens_in_total INTEGER NOT NULL DEFAULT 0,
  tokens_out_total INTEGER NOT NULL DEFAULT 0,
  usd_estimate_total REAL NOT NULL DEFAULT 0.0,
  runner_calls_total INTEGER NOT NULL DEFAULT 0,
  missing_cost_runner_calls_total INTEGER NOT NULL DEFAULT 0,
  user_escalations_total INTEGER NOT NULL DEFAULT 0,
  pending_escalation_batches_total INTEGER NOT NULL DEFAULT 0,
  pending_batched_candidates_total INTEGER NOT NULL DEFAULT 0,
  waiting_user_escalation_event_id TEXT,
  waiting_user_batch_id TEXT,
  surface_json TEXT,
  linked_authority_json TEXT,
  linked_authority_result_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  closed_at TEXT
);
```

`phase` stores the exact lifecycle phase. `status` stores the derived roll-up (`open`/`blocked`/`terminal`) per the mapping above; the daemon derives it from `phase` during event append and replay and must not accept arbitrary values from CLI commands. `prior_phase`, `resume_phase`, and `blocked_by_event_id` are populated only while `phase = blocked`; the source of truth is the blocking event payload in `channel.jsonl`.

`surface_json` and `linked_authority_json` are canonical JSON projections from `session_created.payload`. `linked_authority_result_json` is a canonical JSON projection from `council_finalized.payload.linked_authority_result` or `council_unresolved.payload.linked_authority_result` when present. These columns are query/status conveniences only; they must be rebuilt from `channel.jsonl` during replay and must never be inferred from configured thread/card/note identifiers.

### session_participants

```sql
CREATE TABLE session_participants (
  session_id TEXT NOT NULL,
  member TEXT NOT NULL,
  display_name TEXT,
  role TEXT,  -- registry-resolved short string; recommended vocabulary in 12-operations.md
  wrapper TEXT,
  session_ref TEXT,
  stream_cursor TEXT,
  runtime_status TEXT,
  last_heartbeat_at TEXT,
  participant_status TEXT,
  speaking_count INTEGER NOT NULL DEFAULT 0,
  last_spoke_turn INTEGER,
  PRIMARY KEY (session_id, member)
);
```

### events

```sql
CREATE TABLE events (
  event_id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL,
  command_id TEXT,
  causation_event_id TEXT,
  correlation_id TEXT,
  session_id TEXT NOT NULL,
  session_type TEXT NOT NULL,
  turn INTEGER,
  phase TEXT NOT NULL,
  type TEXT NOT NULL,
  sender TEXT NOT NULL,           -- envelope `from` (single principal id)
  recipient_json TEXT NOT NULL,   -- envelope `to` as canonical JSON array text; `[]` for unaddressed audit events
  runner_json TEXT,               -- envelope `runner` object as canonical JSON text; null for non-runner events
  cost_tokens_in INTEGER,
  cost_tokens_out INTEGER,
  cost_usd REAL,
  cost_source TEXT,               -- envelope `cost.source`; null when cost is null or absent
  created_at TEXT NOT NULL,
  payload_json TEXT NOT NULL
);

CREATE INDEX events_by_correlation ON events(correlation_id);
CREATE INDEX events_by_command ON events(command_id);
```

`sender` stores envelope `from`. `recipient_json` stores envelope `to` as canonical JSON array text and is always present; unaddressed audit events store `[]`. The daemon must not store a bare string recipient in `recipient_json`.

The `events.type` column stores all protocol event types defined in `protocol-and-cli.md`, including common recovery events such as `session_blocked` and `session_resumed`, and other command-coverage events such as `delegation_message`, `assignee_update_requested`, and `consensus_vote_requested`. No dedicated projection table is required for those events unless an implementation needs query acceleration.

### runner_invocations

```sql
CREATE TABLE runner_invocations (
  invocation_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  member TEXT NOT NULL,
  adapter_kind TEXT NOT NULL,
  source_command_id TEXT,
  started_event_id TEXT NOT NULL,
  terminal_event_id TEXT,
  attempt INTEGER NOT NULL,
  is_retry INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  cost_tokens_in INTEGER,
  cost_tokens_out INTEGER,
  cost_usd REAL,
  cost_source TEXT,
  cost_missing INTEGER NOT NULL DEFAULT 0,
  duration_sec REAL,
  started_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX runner_invocations_by_session
  ON runner_invocations(session_id, started_at);

CREATE INDEX runner_invocations_by_member
  ON runner_invocations(session_id, member);

CREATE INDEX runner_invocations_by_source_command
  ON runner_invocations(session_id, source_command_id);
```

Allowed `status` values: `started`, `succeeded`, `failed`, `timeout`, `semantic_error`, `discarded_after_cancel`, `cancelled`, `interrupted`.

Projection rules:

- On `runner_invocation_started`: insert one row into `runner_invocations`; increment `sessions.runner_calls_total`.
- On a terminal semantic runner event: update `runner_invocations.status` to `succeeded`; set `terminal_event_id`, duration, cost fields; if `cost` is null, set `cost_missing = 1` and increment `sessions.missing_cost_runner_calls_total`.
- On `runner_invocation_failed`: update status according to failure reason; set `terminal_event_id`; apply the same cost/null-cost projection rule.
- On `runner_result_discarded`: update status to `discarded_after_cancel`; set `terminal_event_id`; apply the same cost/null-cost projection rule.

`runner_invocations` is a projection. `channel.jsonl` remains the source of truth. Replay rebuilds `runner_invocations` from `runner_invocation_started` and terminal runner events.

Pending or interrupted invocation handling:

- If replay finds a `runner_invocation_started` event without a terminal event, the projection keeps the row with `status: started`.
- On daemon startup, if such a pending invocation cannot still be reconciled with a live subprocess, the daemon may append `runner_invocation_failed` with `reason: interrupted` (or `runner.status: interrupted`).
- Replay itself remains deterministic and side-effect free; it must not invent that event.

### escalation_batches

```sql
CREATE TABLE escalation_batches (
  batch_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  status TEXT NOT NULL,                -- pending | flushed | cancelled | rate_limited
  first_event_id TEXT NOT NULL,
  latest_event_id TEXT NOT NULL,
  user_escalation_event_id TEXT,       -- set when status = flushed
  batch_window_sec INTEGER NOT NULL,
  batch_deadline_at TEXT NOT NULL,
  prior_phase TEXT NOT NULL,
  resume_phase TEXT,
  pending_count INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  flushed_at TEXT,
  cancelled_at TEXT
);

CREATE INDEX escalation_batches_by_session
  ON escalation_batches(session_id, status, batch_deadline_at);
```

### escalation_batch_items

```sql
CREATE TABLE escalation_batch_items (
  batch_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  source_event_id TEXT NOT NULL,
  added_event_id TEXT NOT NULL,
  source_member TEXT,
  question_hash TEXT NOT NULL,
  urgency TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (batch_id, source_event_id)
);

CREATE INDEX escalation_batch_items_by_session
  ON escalation_batch_items(session_id, batch_id);
```

Projection rules:

- On `escalation_batched` with action `created` or `added`: create or update `escalation_batches`; insert one row into `escalation_batch_items`; increment pending batch/candidate projections.
- On `user_escalation_requested` with `batch: true`: mark the referenced batch `flushed`; set `user_escalation_event_id`; decrement pending counters; increment `user_escalations_total`.
- On `escalation_rate_limited` that references a batch: mark the batch `rate_limited` if policy blocks its flush.
- On replay: rebuild all batch tables from `channel.jsonl`. Replay must not rely on in-memory timers and must not invent new events because a deadline has passed; timer-driven flush is daemon runtime behavior after replay completes (per `operations.md` §4).

### event_recipients

```sql
CREATE TABLE event_recipients (
  event_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  recipient TEXT NOT NULL,
  ordinal INTEGER NOT NULL,
  PRIMARY KEY (event_id, recipient),
  UNIQUE (event_id, ordinal)
);

CREATE INDEX event_recipients_by_session_recipient
  ON event_recipients(session_id, recipient, event_id);

CREATE INDEX event_recipients_by_event
  ON event_recipients(event_id);
```

`event_recipients` is a query projection over `events.recipient_json`. For an event with `"to": ["agent-1", "agent-2"]`, the table contains two rows; for `"to": []`, no rows. `ordinal` is for deterministic transcript rendering and debugging only; recipient order has no semantic meaning.

## Transcript and export renderings

`channel.jsonl` remains the source of truth. `transcript.md`, `transcript.jsonl`, `brief.md`, and export bundle files are generated renderings and may be recreated from the session directory.

Renderer rules:

- Markdown transcript rendering reads `session.yaml`, `registry_snapshot.yaml` metadata, and `channel.jsonl` only. It renders stable event order, `from`/`to` semantic recipients, surface metadata, linked-authority metadata and `linked_authority_result` payloads, council attendance/agenda evidence, delegation/review evidence, blockers, terminal/cancelled phase evidence, runner/cost summary values when present, and additive selected-runner accounting for council logs.
- JSONL transcript rendering is a deterministic re-emission of persisted event envelopes from `channel.jsonl`; it does not add status fields or plugin-only fields.
- Export bundles are local directories containing `transcript.md`, `transcript.jsonl`, `brief.md`, `session.json`, `channel.jsonl`, `registry_snapshot.yaml`, and `bundle_manifest.json`.
- `stream status`, council status, Markdown transcripts, and `bundle_manifest.json` include the additive `selected_runner_accounting` object for council logs. It reports selected speakers, runner starts/successes/failures/discards/dispatch failures, linked succeeded runner speech, runnerless/manual/fallback speech, diagnostics, and `selected_runner_pass`. Runnerless/manual/fallback speech and linked speech whose `runner.status` is not `succeeded` must not repair selected-runner pass after terminal failure or missing succeeded runner speech.
- Council status and `bundle_manifest.json` also project the exact stored `selected_runner_timeout_evidence` object when it is persisted on the session. For `NEWFIX-005`, this object is intentionally not part of `stream status` or transcript rendering; those surfaces remain unchanged. Later guarded timeout drift is emitted as `selected_runner_dispatch_failed.reason = selected_runner_timeout_policy_blocked` with a re-evaluated timeout-evidence payload rather than by mutating the projected status/export snapshot.
- `bundle_manifest.json` includes additive `surface_delivery_projection` and `argument_graph_projection` arrays. `argument_graph_projection` is built only from cursor-ordered `speech` events and preserves `claims[]`, `stance_links[]`, `contribution_type`, `new_axis_reason`, `evidence[]`, `quality_diagnostics`, and relation audit data where present. Missing or malformed ARGUE relation shapes are marked diagnostic; renderers must not infer links from `responds_to_event_id`, author, timestamps, Discord order, visible-surface order, or floor-grant ordering.
- Default export output is `<session_dir>/exports/<session_id>-bundle`. Explicit output paths must not contain NUL or dot-dot segments and must resolve to regular non-symlink files/directories. Existing regular generated files are overwritten deterministically.
- Transcript/export/status/tail are side-effect free for session state: they do not append events, rebuild projections, invoke runners, deliver escalations, or write any linked-authority external system.

### Append and replay rule

When appending an event the daemon:

1. validates `from` (single principal id, allowed values per `protocol-and-cli.md`);
2. validates and normalizes `to` (array form, deduplicated, canonical order, allowed recipients);
3. stores the full envelope in `channel.jsonl` (the source of truth);
4. inserts the event row with `sender` and canonical `recipient_json`;
5. inserts one `event_recipients` row per recipient (zero rows if `to: []`).

On replay, `event_recipients` is rebuilt from `recipient_json`. `channel.jsonl` remains the source of truth; `recipient_json` and `event_recipients` are projections for query, status, stream tooling, and transcript rendering.

### stream_cursors

```sql
CREATE TABLE stream_cursors (
  session_id TEXT NOT NULL,
  member TEXT NOT NULL,
  cursor TEXT NOT NULL,
  event_id TEXT NOT NULL,
  acknowledged_at TEXT NOT NULL,
  PRIMARY KEY (session_id, member)
);
```

### stream_subscribers

```sql
CREATE TABLE stream_subscribers (
  session_id TEXT NOT NULL,
  member TEXT NOT NULL,
  subscriber_id TEXT NOT NULL,
  connected_at TEXT NOT NULL,
  last_heartbeat_at TEXT NOT NULL,
  last_cursor TEXT,
  status TEXT NOT NULL,
  PRIMARY KEY (session_id, member, subscriber_id)
);
```

### delegation_reviews

```sql
CREATE TABLE delegation_reviews (
  session_id TEXT NOT NULL,
  review_round INTEGER NOT NULL,
  reviewer TEXT NOT NULL,
  verdict TEXT NOT NULL,
  findings_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (session_id, review_round, reviewer)
);
```

### council_hand_raises

```sql
CREATE TABLE council_hand_raises (
  session_id TEXT NOT NULL,
  turn INTEGER NOT NULL,
  member TEXT NOT NULL,
  wants_to_speak INTEGER NOT NULL,
  intent TEXT,
  relevance INTEGER,
  urgency INTEGER,
  reason TEXT,
  evidence_summary TEXT,
  eligible INTEGER NOT NULL DEFAULT 1,
  ineligibility_reason TEXT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (session_id, turn, member)
);
```

`eligible` is `0` when **any** of the following holds:

- the member was the immediately previous speaker (recorded in `sessions.last_speaker`);
- the `hand_raise` arrived after `hand_raise_research_timeout_sec` for this turn (the moderator may still consider late raises out-of-band but the projection does not include them as eligible candidates).

`ineligibility_reason` records which rule fired (`recent_speaker`, `late_arrival`, ...). Speaker-selection consequences live in `architecture.md`; rows with `eligible = 0` are excluded from the scoring pool.

### council_attendance_projection

```sql
CREATE TABLE council_attendance_projection (
  session_id TEXT NOT NULL,
  member TEXT NOT NULL,
  required INTEGER NOT NULL DEFAULT 1,
  attendance_requested_event_id TEXT NOT NULL,
  member_attended_event_id TEXT,
  attendance_status TEXT,
  attendance_summary TEXT,
  surface_evidence_json TEXT,
  requested_at TEXT NOT NULL,
  attended_at TEXT,
  PRIMARY KEY (session_id, member)
);

CREATE INDEX council_attendance_by_session_status
  ON council_attendance_projection(session_id, attendance_status);
```

Projection rules:

- On `attendance_requested`: create one required row for each addressed required participant and store the request event id.
- On terminal `member_attended`: update that member's row with status, summary, event id, and any surface evidence pointer.
- Allowed terminal `attendance_status` values are `present`, `partial`, `unavailable`, and `no_response_timeout`.
- For `surface.kind=discord_thread`, prepare/readiness checks must use this projection plus `channel.jsonl` replay evidence to confirm that every required participant has a terminal attendance row before `preparation_requested` can be accepted.

### council_agenda_locks

```sql
CREATE TABLE council_agenda_locks (
  session_id TEXT PRIMARY KEY,
  agenda_locked_event_id TEXT NOT NULL,
  locked_by TEXT NOT NULL,
  decision_question TEXT NOT NULL,
  constraints_json TEXT,
  surface_evidence_json TEXT,
  locked_at TEXT NOT NULL
);
```

Projection rules:

- On `agenda_locked`: create or update the session's agenda-lock projection from the event payload.
- For `surface.kind=discord_thread`, `agenda_locked_event_id` and `decision_question` must be available to status, transcript, and export. Missing agenda-lock projection is an acceptance failure, even if the Discord thread contains an informal agenda message.

### council_votes

```sql
CREATE TABLE council_votes (
  session_id TEXT NOT NULL,
  consensus_round INTEGER NOT NULL,
  draft_version INTEGER NOT NULL,
  member TEXT NOT NULL,
  vote TEXT NOT NULL,
  reason TEXT,
  required_change TEXT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (session_id, consensus_round, draft_version, member)
);
```

### council_argument_graph_projection

```sql
CREATE TABLE council_argument_graph_projection (
  session_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  event_ordinal INTEGER NOT NULL,
  turn INTEGER NOT NULL,
  speaker TEXT NOT NULL,
  speech TEXT NOT NULL,
  contribution_type TEXT,
  new_axis_reason TEXT,
  status TEXT NOT NULL,
  diagnostic TEXT,
  claims_json TEXT NOT NULL,
  stance_links_json TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  quality_diagnostics_json TEXT NOT NULL,
  relation_audit_json TEXT NOT NULL,
  PRIMARY KEY (session_id, event_id)
);

CREATE INDEX council_argument_graph_projection_by_session_turn
  ON council_argument_graph_projection(session_id, turn, event_ordinal);
```

Projection rules:

- Replay adds one row for each cursor-ordered `speech` event.
- `status = projected` means the ARGUE relation fields present on the payload had projection-compatible shapes and were copied into deterministic JSON columns.
- `status = diagnostic` means relation fields were missing or malformed for projection. This is replay/export evidence only and must not weaken append-time ARGUE-003 validation.
- `claims_json` is populated only when every `claims[]` entry is an object with a non-empty string `id`, `claim_id`, `text`, `summary`, or `content`; malformed arrays remain `null` and add `malformed_claims`.
- `stance_links_json` is populated only when every `stance_links[]` entry is an object with a non-empty string `target_event_id`, `target_claim_id`, `claim_id`, `target`, or `stance`; malformed arrays remain `null` and add `malformed_stance_links`.
- `quality_diagnostics_json` is populated only when every `quality_diagnostics[]` entry is an object with a non-empty string `code`, `diagnostic`, `message`, `severity`, `field`, or `detail`; scalar or unrecognized entries remain `null` and add `malformed_quality_diagnostics`.
- `evidence_json` preserves `evidence[]` entries that are strings or objects. Other evidence element types remain `null` and add `malformed_evidence`.
- `speech` is copied exactly from `payload.speech` for query/export convenience. Human transcript rendering may summarize relation evidence, but it must not rewrite, normalize, or enrich `payload.speech`.
- Missing relation fields, legacy `responds_to_event_id`, author, timestamps, Discord order, visible-surface order, and floor-grant ordering must not be used to fabricate `stance_links[]`, `contribution_type`, or claim targets.

### linked_authority_results

```sql
CREATE TABLE linked_authority_results (
  session_id TEXT PRIMARY KEY,
  terminal_event_id TEXT NOT NULL,
  status TEXT NOT NULL,
  kanban_card_id TEXT,
  kanban_comment_id TEXT,
  vault_decision_note TEXT,
  followup_card_id TEXT,
  failure_reason TEXT,
  evidence_json TEXT NOT NULL,
  recorded_at TEXT NOT NULL
);
```

Allowed `status` values are `posted`, `failed`, and `pending_followup`.

Projection rules:

- On `council_finalized` or `council_unresolved` with configured `linked_authority`, persist and project `payload.linked_authority_result` into `linked_authority_results` and `sessions.linked_authority_result_json`.
- `posted` requires concrete evidence such as a Kanban comment id, Vault note path, or equivalent immutable pointer in `evidence_json`.
- `failed` requires `failure_reason` plus follow-up handling evidence. `pending_followup` requires a linked follow-up/review card, pending-review handoff, or equivalent evidence pointer.
- `failed` and `pending_followup` are not completed Kanban/Vault return. Transcript/export/status must distinguish "council decision finalized" from "linked authority return completed".

### commands_seen

```sql
CREATE TABLE commands_seen (
  command_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  first_event_id TEXT NOT NULL,
  result_summary TEXT,
  created_at TEXT NOT NULL
);
```

### artifacts

```sql
CREATE TABLE artifacts (
  session_id TEXT NOT NULL,
  artifact_id TEXT NOT NULL,
  source_path TEXT NOT NULL,
  stored_path TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  mime TEXT,
  ingested_at TEXT NOT NULL,
  PRIMARY KEY (session_id, artifact_id)
);
```

## Artifact contract

Every submitted artifact passes through the ingestion pipeline defined in `12-operations.md`:

1. The source path is canonicalized. Paths escaping the member workspace or the session directory are rejected with `security_violation`.
2. The daemon copies the file into `sessions/<session_id>/artifacts/<artifact_id>`.
3. The copy is hashed (SHA-256) and MIME-sniffed.
4. A row is inserted into the `artifacts` table.
5. The `work_submitted` event references the stored path and artifact id, never the source path.

Size ceiling is `max_artifact_bytes` (default 25 MB). The MIME whitelist is session-configurable, defaulting to text, source code, markdown, JSON, YAML, PDF, and common image formats.

The persisted `work_submitted` protocol event references the ingested artifact record, not the original source path. Required artifact reference fields in `work_submitted.payload.artifacts`:

- `artifact_id`
- `stored_path`
- `sha256`
- `size_bytes`
- `mime`

`source_path` remains available in the SQLite `artifacts` projection for audit and debugging, but it is not used as the durable event payload reference. `review_requested.payload.target_artifacts` and `work_accepted.payload.accepted_artifacts` follow the same form (at minimum `artifact_id` and `stored_path`).

## Retention

- Event logs and transcripts are durable.
- Raw member CLI logs are operational artifacts and may be deleted after 2 days from last activity.
- Generated transcripts can be regenerated from `channel.jsonl`.
- `commands_seen` entries are retained for the session lifetime plus 7 days to absorb delayed CLI retries. On session close, older entries are pruned. The durable causality record remains in `channel.jsonl` via `command_id` on each event.
- `stream_cursors` are retained for the session lifetime. They are operational projections; the replay source remains `channel.jsonl`.
- Ingested artifacts are durable and follow the same retention as the session directory.

---

## Merged from `docs/spec/architecture.md`

# Orchestration and Moderator Policy

## Moderator role

The moderator is the default orchestrator. In delegation sessions the moderator supervises work. In council sessions the moderator moderates process.

When the moderator has a substantive opinion in a council, it must be recorded as a separate participant-style turn, distinct from moderation actions.

The moderator and members operate through the `atn-control` CLI. Long-lived runtimes observe `atn-control stream` and write typed commands back to the daemon. The moderator must not treat fresh one-shot subprocess prompts as the primary council participation loop.

# Delegation policy

## Active collaboration

Delegation is not fire-and-forget. The assignee should actively communicate through explicit commands:

- acknowledgement: `delegate ack`
- clarifying questions: `delegate clarify`
- progress updates: `delegate update`
- blockers or partial results: `delegate update` or `delegate block` depending on ownership
- completion submission: `delegate submit`

The moderator should actively respond through explicit commands:

- general guidance: `delegate message`
- clarification answers: `delegate answer-clarification`
- user escalation: `delegate escalate`
- escalation delivery audit: `delegate escalation-delivered` or `delegate escalation-delivery-failed`
- review request: `delegate review`
- revision request: `delegate revise`
- acceptance: `delegate accept`
- manual block: `delegate block`

The moderator must address commands to the intended participant explicitly. For broadcast-like council actions, the moderator or daemon uses explicit recipient lists derived from the session participant list, not an `all` recipient.

## User escalation policy

When an assignee asks a question, the moderator must decide whether it can be answered locally or must be escalated to the user.

The moderator may answer directly when:

- the answer is already in project docs or prior user instruction
- the question is a technical detail within the approved scope
- the choice does not change product direction, risk, cost, timeline, or authority

The moderator must escalate to the user when the question involves:

- product direction or priority
- scope, cost, timeline, or trade-off decisions
- risky actions, deletion, credential handling, or policy changes
- conflicting user requirements
- uncertainty where the moderator lacks authority

Delivery responsibility split:

- The **daemon** records the escalation (`user_escalation_requested`), counts it against `max_user_escalations`, applies dedupe and batching windows, and emits the audit events. It does not open any outbound notification channel.
- The **moderator runtime**, using the Hermes plugin/gateway helper or an equivalent Hermes gateway skill, decides which gateway to use (Telegram/Slack/Discord/the origin Hermes session/etc.), performs the delivery, and writes `user_escalation_delivered` (or `user_escalation_delivery_failed` plus a fallback attempt) back through a typed ATN command. The canonical CLI remains the recovery/manual path for the same event.

Delivery hint to the moderator skill (the `delivery_policy` payload field):

- Prefer the origin Hermes session when the user is actively interacting there.
- Use Telegram (or whichever durable gateway the user has configured) for urgent blocked work, long-running delegated work, or when the origin session is not a reliable notification channel.
- `both` is allowed for high-importance blockers.
- Outbound gateways must come from the orchestrator/root reporting channel, not from a delegated member bot, unless explicitly requested by the user. This invariant lives in the moderator skill, not in the daemon.

### Escalation urgency and batching

Allowed urgency values:

- `low`: user input is useful but not immediately blocking; eligible for batching.
- `normal`: user input is needed soon; do not batch by default.
- `urgent`: user input is needed quickly; do not batch.
- `blocked`: the session cannot continue safely without the user's answer; do not batch and enter `waiting_user`.

Only `low` urgency escalation candidates may be batched. A moderator must not mark a blocking decision as `low` merely to reduce user interruption. If the assignee or reviewer cannot continue without the answer, the escalation must be `blocked` or `urgent`.

## Escalation rate control

User attention is the scarcest resource in this system. The daemon enforces these limits on escalation **recording**; outbound rate-limiting on a particular gateway is the moderator skill's concern.

- Each session enforces `max_user_escalations` (default 10). Exceeding it blocks further escalation recording until `limits_extended` is authorized by the user. Only `user_escalation_requested` events count against this limit; `escalation_batched` does not.
- Two escalations with semantically equivalent questions within 10 minutes collapse. Equivalence is checked by payload hash plus a similarity heuristic on the question text. The second escalation records `escalation_deduplicated` and is not surfaced to the moderator for delivery.
- Escalations with `urgency: low` may be batched **before a user-facing escalation is created**. The daemon records `escalation_batched` and keeps the session in its prior phase. The daemon flushes the batch into a single `user_escalation_requested` when:
  1. the batching window expires;
  2. a higher-urgency escalation arrives and policy chooses to flush pending low-urgency items together;
  3. the moderator explicitly flushes the batch;
  4. the session is about to enter a phase where the batched question becomes blocking.
  The flushed `user_escalation_requested` is what enters `waiting_user`.
- Per-gateway outbound rate-limiting (e.g. "no more than one Telegram message per minute per session") is the moderator skill's responsibility, not the daemon's. The daemon does not throttle outbound delivery because it does not perform delivery.
- Every batching, deduplication, or rate-limit decision emits an event (`escalation_batched`, `escalation_deduplicated`, `escalation_rate_limited`) with enough context to audit the decision.

### Moderator responsibility

The moderator must distinguish:

- queued escalation candidate: `escalation_batched`
- user-facing escalation request: `user_escalation_requested`
- delivery audit: `user_escalation_delivered` or `user_escalation_delivery_failed`
- user answer: `user_escalation_resolved`
- relay back to member: `clarification_answered` or the corresponding review answer event

The moderator must not claim that the user was asked until `user_escalation_requested` has been delivered and `user_escalation_delivered` has been recorded.

## Completion gate

A delegation session is complete only when the moderator records `work_accepted`.

A short completion summary from the assignee is not enough. The moderator must check artifacts, logs, tests, or the requested output before acceptance.

## Assignee self-report vs session phase

A member's `assignee_update.payload.progress_status: blocked` is a self-report; it does **not** by itself move the session to the `blocked` phase. The moderator must decide whether to:

- answer locally,
- escalate to the user,
- request an update,
- record a manual block (`delegate block` → `session_blocked`),
- cancel,
- or continue in the current phase.

Daemon-internal blocking events (`session_budget_exceeded`, `escalation_timeout`, session-scoped `security_violation`) move the session phase to `blocked` independently of the assignee's self-report.

## Review gate

Review is a delegation quality gate, not a separate top-level session type.

Default communication path:

```text
reviewer question -> assignee answer -> reviewer verdict -> the moderator decision
```

The moderator should not answer assignee-owned implementation questions on the assignee's behalf. The moderator's role is to route, constrain, and resolve process issues.

The reviewer records the final verdict through `delegate review-submit`.

A reviewer may return:

- `approved`
- `comments`
- `changes_requested`
- `blocked`

Reviewer clarification rules:

- The reviewer asks the assignee for intent, evidence, rationale, or missing verification.
- The assignee answers with evidence, acknowledges a defect, or proposes a correction.
- The moderator intervenes if the exchange becomes circular, off-scope, stalled, or authority-sensitive.
- User escalation happens only when the review question changes product scope, priority, risk acceptance, or policy.

Any high-severity `changes_requested` finding should become a `revision_requested` event unless the moderator explicitly rejects it with a reason.

# Council policy

## Standard live-visible decision council default

For Discord-origin decision-bearing councils, the moderator should run `standard_live_visible_decision_council` by default: exact live thread binding, bounded `max_discussion_turns`, selected-runner dispatch timeout defaulted to 120 seconds unless an approved alternative is recorded, lifecycle prerequisites before discussion, per-turn `poll` or hand-raise evaluation, competitive hand-raise candidates where available, `relevance` grant with a recorded reason, selected-runner linked speech only, visible delivery proof for each selected speech, one participant closeout per member after the discussion window, proposal, all-member vote, visible moderator synthesis, and terminal `council.finalize` with matching posted `surface_evidence`.

This is a lifecycle policy, not a `turn_mode=selected_runner` metadata switch. `turn_mode` may declare the intended floor policy such as `relevance`; the durable audit facts are the per-turn `speaker_selected.payload.selection_mode`, runner events, canonical `speech`, vote events, and final visible closeout evidence. Exploratory councils may explicitly opt out of proposal/vote, but they must still report live-visible delivery, selected-runner accounting, closeout, and terminal outcome evidence separately.

## Speaker selection

Eligible speakers are scored after each hand raise. The default Release v1 formula is fixed below; weights live in a config module (`engine.policy.scoring`) and are overridable per session via `limits.council.speaker_scoring`.

```text
score = relevance       * w_relevance        # default w_relevance       = 3
      + urgency         * w_urgency          # default w_urgency         = 2
      + role_match      * w_role_match       # default w_role_match      = 3
      + dissent_bonus
      + under_spoken_bonus
      + evidence_bonus
      - recent_speaker_penalty
```

### Input definitions and ranges

| Term | Source | Range | Notes |
|------|--------|-------|-------|
| `relevance` | `hand_raise.relevance` (member self-rated 0–5) | 0–5 | Lower-bound clamped at 0, upper-bound clamped at 5. |
| `urgency` | `hand_raise.urgency` (member self-rated 0–5) | 0–5 | Same clamping. |
| `role_match` | derived: 1 if member's registry `role` matches the moderator-tagged turn topic role, else 0 | 0–1 | The moderator may tag a turn with a role hint; absent tag defaults to 0 for everyone. |
| `dissent_bonus` | derived: 5 if `hand_raise.intent` is `risk`/`block`/`rebuttal` *and* no prior turn this session has resolved that point, else 0 | 0–5 | Resets to 0 once the dissenting point has been addressed in a later speech. |
| `under_spoken_bonus` | derived: `max(0, average_speech_count_so_far − this_member.speech_count_so_far)` capped at 4 | 0–4 | Encourages fairness without dominating the score. |
| `evidence_bonus` | 2 if `hand_raise.research_done` is true *and* `hand_raise.evidence_summary` is non-empty, else 0 | 0–2 | Rewards prepared contributions. |
| `recent_speaker_penalty` | 100 if member is the immediately previous speaker, else 0 | 0 or 100 | Effectively disqualifying; the previous speaker cannot speak next. |

The `recent_speaker_penalty` value of 100 is large by design so that no realistic combination of the other terms can override it; this enforces the "no consecutive speaker" rule through the same scoring path rather than as a separate filter.

### Maximum non-disqualifying score

With default weights and full-strength bonuses: `5*3 + 5*2 + 1*3 + 5 + 4 + 2 = 39`. Two members tied at the same score trigger tie-break per `selection_mode` below.

### Scoring examples

Example 1: Risk objection beats normal contribution

| Member | relevance | urgency | role_match | intent | research_done | evidence_summary | score notes |
|---|---:|---:|---:|---|---|---|---|
| agent-1 | 5 | 2 | 1 | note | true | present | `5*3 + 2*2 + 1*3 + 0 + 0 + 2 = 24` |
| agent-2 | 4 | 5 | 0 | risk | true | present | `4*3 + 5*2 + 0 + 5 + 0 + 2 = 29` |

`agent-2` is selected because unresolved risk receives `dissent_bonus` and its higher urgency outweighs `agent-1`'s role match.

Example 2: Previous speaker disqualification

If `agent-3` was the immediately previous speaker, `recent_speaker_penalty = 100`. Because the maximum non-disqualifying score is 39, the previous speaker cannot win the next turn regardless of relevance, urgency, or bonus values.

Example 3: Tie-break to `random`

If `agent-1` and `agent-2` produce identical scores after every term resolves, `selection_mode` is `random` with a recorded reason. The moderator must record `random` rather than silently choosing one member, so audit logs can later confirm the tie was real.

### `selection_mode` enumeration

Every `speaker_selected` event carries `selection_mode` and (when applicable) `reason`. Session-level `turn_mode`, when present, is only the intended/default floor policy from `council new`; it is not evidence that any specific turn followed that policy. The durable per-turn audit fact is `speaker_selected.payload.selection_mode`:

- `relevance`: highest score wins, no ties. The default mode.
- `targeted`: the moderator picks a specific member to fill a missing perspective. `reason` is required and names the perspective being sought.
- `random`: used only for tie-breaks between equally scored speakers, or during early exploration when no member has yet formed a perspective and a targeted question is not yet useful. `reason` is required.
- `moderator_direct`: the moderator explicitly grants floor to a named member. `reason` is required and must name the decision need or missing role perspective.
- `role_order`: deterministic turn order by the declared council role/order for a bounded round. `round` and `reason` are required.

### Rules

- The immediately previous speaker cannot speak again on the next turn (enforced via `recent_speaker_penalty`).
- Role relevance is preferred over randomness.
- Risk or blocking objections receive priority when unresolved (via `dissent_bonus`).
- Underrepresented speakers receive a fairness bonus (via `under_spoken_bonus`).
- Random selection is only for tie-breaking or early exploration; never the default mode.
- Discord-thread councils may use `moderator_direct` or `role_order` so the visible thread feels like a chaired council. These modes are still durable `speaker_selected` events and never inferred from Discord message order.
- If a session has `turn_mode`, a per-turn `selection_mode` that differs from it must include a `reason` naming the missing perspective, risk, timeout, chairing need, or other operational reason for the deviation. Silent deviation is a policy defect.

## Discord-thread council policy

When a council has `surface.kind: discord_thread`, the moderator must preserve the surface/SOT split:

- Discord thread is the human-visible room.
- `channel.jsonl` is the canonical event SOT.
- Typed ATN events validated by the daemon are the only state transitions; they may arrive through Hermes plugin tools or the canonical CLI fallback.
- Free-form Discord replies are evidence or user-facing presentation, not implicit state.
- Visible speech/final-result rendering follows `protocol-and-cli.md#surface-rendering-evidence-contract`: render from cursor-ordered durable events first, then attach Discord/Kanban/Vault ids only as delivery evidence pointers.

The recommended visible flow is:

1. Announce the session and linked authority target in the thread.
2. Request attendance through `attendance_requested`.
3. Accept exactly one terminal `member_attended` record or timeout record for each required participant.
4. Lock one decision question through `agenda_locked`.
5. Grant floor one member at a time through `speaker_selected` using `role_order`, `moderator_direct`, or the normal scored modes.
6. Intervene on topic drift with `moderator_intervention`.
7. Propose, vote, and finalize/unresolve through the existing consensus events.
8. Return the final result to Kanban and/or Vault when `linked_authority` requires it, then record return evidence as `posted`, `failed`, or `pending_followup`.

For `surface.kind=discord_thread`, the moderator must not start preparation until `attendance_requested`, terminal attendance for every required participant (`present`, `partial`, `unavailable`, or `no_response_timeout`), participant runtime subscriber/cursor/heartbeat readiness, and `agenda_locked` are already in the event log. If attendance timeout has expired, the daemon records `no_response_timeout` before rejecting or continuing. If any prerequisite remains missing, the correct action is to complete the missing attendance/agenda/runtime step or block, not to call `council prepare`.

Linked authority return policy:

- `council_finalized` records the council decision, but it is not proof that Kanban/Vault return completed.
- Moderator/Gray evidence must record `linked_authority_result.status: posted`, `failed`, or `pending_followup`.
- `posted` requires a Kanban comment id, Vault note path, or equivalent evidence pointer.
- `failed` requires a failure reason and keeps the origin card blocked/pending review until a follow-up resolves the return.
- `pending_followup` requires a linked follow-up/review card or equivalent handoff evidence.
- The daemon/replay must never create Kanban comments or Vault notes; only the moderator/Gray workflow performs those external writes.

Visible delivery policy:

- A moderator may post human-readable session announcements, floor grants, speech turns, interventions, and final/unresolved results to the thread, but each visible item must correspond to a durable event cursor or be clearly labeled as non-state presentation.
- A member's free-form Discord reply is not a `speech` event. If it should become participant speech, the moderator/member runtime must record `council speak` (or the equivalent typed plugin command) and then surface the resulting event.
- Final visible delivery is proven only by `council_finalized.payload.surface_evidence` or an equivalent projection/export evidence pointer. A visible final message without the durable `council_finalized` event is not a finalized council.
- If visible posting fails after finalization, record `failed` with a reason and follow-up handling; if posting is deferred, record `pending_followup`. User reports may say the council finalized only when the durable event exists, and may say visible delivery completed only when posted evidence exists.
- Transcript/export/status rendering must preserve `posted`, `failed`, `pending_followup`, and missing/unproven delivery distinctly. Do not collapse `failed` or `pending_followup` into success.

Moderator closeout sequence:

1. `draft_conclusion` may be surfaced as a draft for participant review, but it is not a terminal result and must not be phrased as final closeout.
2. `consensus_vote_requested` and `consensus_vote` may be surfaced as voting progress over the named `draft_version`, but they do not by themselves prove final closeout.
3. `council_finalized` or `council_unresolved` is the durable terminal outcome. Terminal daemon outcome and human-readable visible closeout are separate acceptance facts.
4. A visible moderator closeout is accepted only when the terminal outcome has posted surface evidence or an equivalent transcript/export/projection pointer that references the terminal event. Missing, mismatched, failed, or pending evidence is fail-closed for visible UX success, even when the durable council outcome is terminal.
5. The plugin-side visible helper may render and deliver the closeout, but it must report evidence back against the control-owned event contract; it must not invent finality from Discord/helper output alone.

Divergence controls:

- One council has one locked decision question.
- New topics become follow-up candidates, not current-thread expansion.
- A second round may address only unresolved issues named by the moderator.
- The moderator's substantive opinion is recorded as a participant-style turn, not hidden inside moderation text.

## Research policy

### Initial council preparation

Each member receives the topic and has up to 10 minutes to prepare. Ready members record `member_ready` through `council ready`. Timed-out members proceed with partial preparation; this is recorded as `member_prepared_partial` either by the member through `council prepared-partial` or by the daemon on timeout (origin class `daemon_internal`).

For Discord-thread councils, the moderator must not poll for hand raises until preparation success, partial, or timeout/failure evidence exists for every required participant and runtime subscriber/cursor/heartbeat readiness remains fresh. Timeout/failure evidence is diagnostic and must not be reported as live participant readiness.

Preparation requests are addressed to the council members with an explicit `to` list. Members may still observe events not addressed to them, but they should act only when the event type, recipient list, role, and current phase require action.

### In-discussion research

During each hand raise window, members may research or fact-check for up to 10 minutes. If they raise a hand, they should be ready to speak immediately when selected.

## No-hand-raise policy

This is the single normative source for no-hand-raise handling. The state machine (`architecture.md`) and acceptance tests (`testing-and-tooling.md`) defer here.

When a hand-raise round closes with no eligible member raising a hand:

1. The moderator evaluates whether a draft conclusion is possible from the material already spoken.
2. If yes, transition to `draft_conclusion`.
3. If key perspectives are missing, the moderator asks a targeted question and opens a new hand-raise round. This is the default behavior.
4. Random speaker selection is not a default. It is permitted only:
   - as a tie-breaker between equally scored speakers, or
   - during early exploration when no member has yet formed a perspective and a targeted question is not yet useful.
   In both cases the `speaker_selected` event must carry `selection_mode: "random"` with a reason.
5. If successive no-hand-raise rounds yield no new perspective and no draft conclusion, move to `unresolved` with a reason.

## Intervention policy

The moderator should intervene when a member:

- drifts from the topic
- repeats a point without adding information
- makes unsupported claims
- violates role boundaries
- turns a decision discussion into broad brainstorming
- blocks without a required change

Intervention template:

```text
This appears to be off the current decision path.
Please choose one:
1. connect it directly to the session goal,
2. withdraw it,
3. mark it as a separate follow-up topic.
```

## Consensus policy

A council draft conclusion cannot finalize until all members vote.

The moderator requests voting through `council request-vote`, which emits `consensus_vote_requested`. Member runtimes vote only after observing that event or its replayed stream frame.

Vote handling:

- `approve`: counts toward consensus.
- `approve_with_conditions`: requires revision and re-vote unless purely editorial.
- `block`: prevents finalization.

A block must include reason, required change, and what would resolve the block.

### Consensus runaway prevention

The default `max_consensus_rounds` is 20. The moderator should normally resolve or mark unresolved far earlier; the hard cap exists only to prevent unbounded loops.

Recommended soft controls (overridable per session through `limits`):

- `consensus_round_warning_threshold`: 3 — beyond this, the moderator should record an explicit reason for continuing each additional round.
- `no_progress_round_limit`: 3 — beyond this, the same blocking objection has remained unresolved across multiple revision cycles.

If the same blocking objection remains unresolved across `no_progress_round_limit` revision cycles, the moderator should:

1. narrow the decision so the unresolved point becomes follow-up scope, or
2. escalate the unresolved authority question to the user, or
3. record `council_unresolved` with a clear reason.

These soft controls are moderator policy guidance; they are not hard caps and they do not by themselves transition the council. Only `max_consensus_rounds` enforces a hard cap, and reaching it should record `council_unresolved`.

# Reporting policy

Final user-facing report should include:

- session type
- derived `status` (`open`/`blocked`/`terminal`)
- exact `phase` when the session is not terminal
- final conclusion or accepted work summary
- key events and decisions
- remaining risks or blockers
- runner usage summary: runner calls used, token and USD totals when available, missing-cost count, unresolved budget blocks
- transcript path

When a session is blocked by `session_budget_exceeded`, the moderator must not continue dispatching member runner work until the user authorizes `limits_extended`. If cost is missing for some runner calls (`missing_cost_runner_calls_total > 0`), the moderator must report that token and USD totals are incomplete rather than presenting them as exact.
