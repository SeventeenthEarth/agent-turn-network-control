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
  operational.log               # daemon operational log; see 12-security.md
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

The normative permission rules for registry and wrapper validation live in `12-security.md`.

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

`atn-control storage rebuild-projection` (defined in `04-cli-spec.md`) rebuilds `network.sqlite` and other projections from `channel.jsonl`. It is an operational command and must not append events, invoke runners, deliver escalations, or invent timer-driven events. Detailed disaster-recovery procedure lives in `17-disaster-recovery.md`.

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
      # bonus values and recent_speaker_penalty are defined in 07-moderator-policy.md
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

`runner_calls_total` is projected from `runner_invocation_started` events. `missing_cost_runner_calls_total` is projected from terminal runner events whose `cost` is null. Token and USD totals are projected only from terminal runner events with parsed cost. See `13-operational-contracts.md` §3.

Replay handlers populate event-specific tables from durable events only: runner invocation lifecycle, escalation batching and batched user escalation, council attendance/agenda/hand-raise/vote/linked-authority results, council argument-graph relation evidence from `speech` payloads, stream cursor/subscriber events, review verdict/submission events, command-id summaries, event recipients, and artifact references from artifact-bearing work/review events.

Unknown forward-compatibility keys (limits introduced after Release v1) must not error on a Release v1 reader. See the forward-compatibility rule in `13-operational-contracts.md`.

### Phase and status

`phase` (under `state.phase`) is the exact lifecycle state from `06-state-machine.md`. `status` is a derived roll-up used for query, UI, and active-session lock checks.

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

When `session_resumed` is applied, the daemon sets `sessions.phase` to the event envelope `phase`, derives `sessions.status` from that phase, and clears `prior_phase`, `resume_phase`, and `blocked_by_event_id`. `session_resumed` lifts manual, external-dependency, and policy blocks; budget and limit blocks remain lifted only by `limits_extended` (see `13-operational-contracts.md` §5).

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
  role TEXT,  -- registry-resolved short string; recommended vocabulary in 12-security.md
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

The `events.type` column stores all protocol event types defined in `03-protocol-spec.md`, including common recovery events such as `session_blocked` and `session_resumed`, and other command-coverage events such as `delegation_message`, `assignee_update_requested`, and `consensus_vote_requested`. No dedicated projection table is required for those events unless an implementation needs query acceleration.

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
- On replay: rebuild all batch tables from `channel.jsonl`. Replay must not rely on in-memory timers and must not invent new events because a deadline has passed; timer-driven flush is daemon runtime behavior after replay completes (per `13-operational-contracts.md` §4).

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

1. validates `from` (single principal id, allowed values per `03-protocol-spec.md`);
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

`ineligibility_reason` records which rule fired (`recent_speaker`, `late_arrival`, ...). Speaker-selection consequences live in `07-moderator-policy.md`; rows with `eligible = 0` are excluded from the scoring pool.

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

Every submitted artifact passes through the ingestion pipeline defined in `12-security.md`:

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
