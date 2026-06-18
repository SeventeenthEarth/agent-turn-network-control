# Operational Contracts

These contracts are cross-cutting and apply to every epic. Streaming, runner, protocol, storage, and engine modules depend on them. They must be fixed before RUNRT (member runtime and runner adapter).

## 0. Stream and member runtime contract

### Purpose

Make real agents active participants instead of passive one-shot subprocess responses. The daemon owns state and event durability; the Hermes plugin is the preferred agent-facing interface; the CLI is the stable canonical diagnostics/recovery/manual interface; both use the KAN protocol client/contract and member runtimes listen and act.

### Interface

```bash
# Canonical CLI fallback
kkachi-agent-network stream <session_id> --member <member> --since <cursor> --follow --format ndjson
kkachi-agent-network stream ack <session_id> --member <member> --cursor <cursor>

# Preferred Hermes integration
Hermes plugin stream/tail tool -> KAN protocol client/contract -> daemon stream
Hermes plugin cursor-ack tool -> KAN protocol client/contract -> daemon ack
```

Stream frames are newline-delimited JSON with `{cursor, is_replay, event}`. The event is the full envelope from `03-protocol-spec.md`.

### Rules

- Stream visibility follows JSONL durability: append first, publish second.
- Member runtimes persist their own acknowledged cursor.
- Reconnect replays missed events before live events.
- Cursor gaps, unknown schema versions, or storage corruption fail closed.
- Member-originated state changes are typed KAN commands sent through a protocol client, normally via Hermes plugin tools and always with a canonical CLI command equivalent; they are not direct daemon-state mutations.
- One-shot runner calls are bounded adapter operations. They must not be the primary council turn loop.
- Heartbeats from stream subscribers update `stream_subscribers`; stale subscribers emit `stream_subscriber_stale`.
- Participant runtime readiness is derived from durable session events, not a materialized readiness table. Required members are unready unless the log proves subscriber presence, valid cursor ack, fresh cursor ack, fresh heartbeat, attendance/preparation response or timeout/failure evidence when required, and selected-runner prerequisites when a speaker is selected.
- Stream frames carry the full event envelope. In that envelope, `from` is a string and `to` is always an array of strings (per `03-protocol-spec.md`).
- `to` is semantic addressing, not stream access control. Member runtimes may observe events not addressed to them; they decide whether to act by inspecting event type, sender, recipients, role, phase, and policy. Read permissions are governed by `12-security.md`.

### Heartbeat cadence

- Stream subscribers send a heartbeat every **30 seconds**.
- A subscriber with no heartbeat for **90 seconds** is marked stale and the daemon emits `stream_subscriber_stale` once.
- A subscriber with no heartbeat for **300 seconds** triggers the session policy: the moderator may repoll the member runtime, mark its participation partial, or block the session per `07-moderator-policy.md`.
- These thresholds (`stream_heartbeat_interval_sec`, `stream_stale_threshold_sec`, `stream_repoll_threshold_sec`) live in session `limits` and are overridable.
- Gateway/process/socket liveness, transcript/export-only evidence, manual/profile fallback text, and parent-channel fallback visibility never count as participant runtime readiness.

## 1. Runner adapter interface

### Purpose

Decouple bounded model invocation from any single CLI convention. Release v1 provides exactly one adapter — `hermes-agent` — which invokes Hermes profile alias binaries (typically under `~/.local/bin/`). The Protocol stays open so additional kinds (`claude-cli`, `codex-cli`, `gemini-cli`, custom shell wrappers, ...) can be added in future releases without changing the engine. The adapter is used by member runtimes for bounded model invocation; it is not the primary council participation model — that role belongs to the long-lived stream-driven runtime.

### Interface

```python
class RunnerAdapter(Protocol):
    kind: str  # Release v1 whitelist: "hermes-agent". Future releases may add "claude-cli", "codex-cli", etc.

    def send(
        self,
        prompt: str,
        session_handle: SessionHandle | None,
        *,
        timeout_sec: int,
        cancel_token: CancelToken,
    ) -> RunnerResult: ...

    def resume(
        self,
        prompt: str,
        session_handle: SessionHandle,
        *,
        timeout_sec: int,
        cancel_token: CancelToken,
    ) -> RunnerResult: ...

    def parse_session_handle(self, raw_output: str) -> SessionHandle | None: ...

    def cancel(self, session_handle: SessionHandle) -> None: ...
```

`RunnerResult`: `{ok, stdout, stderr, exit_code, duration_sec, cost, session_handle, semantic_status}`.
`SessionHandle`: opaque adapter-specific string; never interpreted by the engine.
`CancelToken`: cooperative cancellation signal observed by long runs.

`RunnerResult.cost` is parsed cost information only. It is **not** used to determine whether a runner call occurred. A runner call is recorded through a durable `runner_invocation_started` event before the adapter subprocess is launched. Every actual adapter invocation attempt receives a unique `runner.invocation_id`.

### Registry integration

```yaml
members:
  agent-1:
    wrapper: agent-1-wrapper
    adapter_kind: hermes-agent
    env_allowlist: [ANTHROPIC_API_KEY]
```

Unknown `adapter_kind` fails registry validation. New adapters are added through the adapter registry, not by extending the engine.

### Release v1 adapter whitelist

The canonical Release v1 adapter whitelist is exactly:

```python
RELEASE_V1_ADAPTER_WHITELIST = {"hermes-agent"}
```

The constant name `RELEASE_V1_ADAPTER_WHITELIST` is a code identifier referring to Release v1; it does not introduce a separate `v1` term in the docs. This list is normative for Release v1.

This list is normative. Adding a new kind requires both:

1. a code-level registration that maps the kind string to a `RunnerAdapter` implementation, and
2. an entry in this section naming the kind, its expected wrapper invocation contract, and its `cost.source` value (see §3 Cost accounting).

The registry validator rejects any `adapter_kind` not present in the in-code registry; documentation alone does not enable an adapter, and code alone does not enable an adapter without a docs entry.

## 2. Idempotency, correlation, and versioning

### Envelope additions

Every event envelope (see `03-protocol-spec.md`) includes:

- `schema_version` — integer; bumped on any breaking envelope change. Readers refuse unknown versions unless a migration is registered.
- `event_id` — unique per persisted event (ULID recommended).
- `command_id` — set by the originator per the rules below.
- `causation_event_id` — the event that directly caused this one.
- `correlation_id` — logical thread across related commands and events; defaults to `session_id`.
- `from` — required string principal id (the single event originator).
- `to` — required `array<string>` of recipient principal ids.
- `runner` — optional object identifying a bounded adapter invocation attempt. Present on runner accounting events (`runner_invocation_started`, `runner_invocation_failed`, `runner_result_discarded`) and on terminal semantic events produced by a runner invocation. Includes `invocation_id`, `adapter_kind`, `member`, `attempt`, `is_retry`, `source_command_id`, `status`, `duration_sec`. See `03-protocol-spec.md` for the field schema and allowed status values.

Changing `to` between string and array form is a breaking envelope change. The initial Release v1 schema uses array form; implementations that have already persisted string-form events must introduce a `schema_version` migration rather than silently accepting both shapes.

`command_id` rules:

- CLI-originated events: required. Retries of the same CLI command reuse the id and are deduplicated by the daemon.
- Moderator-emitted delivery events (`user_escalation_delivered`, `user_escalation_delivery_failed`) are typed KAN writes from the moderator runtime per `01-product-requirements.md` and `07-moderator-policy.md`, normally through plugin tools with the CLI as the canonical fallback. They follow the participant-command rule: each carries its own `command_id`, retries of the same delivery report reuse the id, and a duplicate is deduplicated by the daemon. `causation_event_id` for these events points to the originating `user_escalation_requested`.
- Daemon-generated events that follow directly from a CLI command reuse or correlate with the originating `command_id` according to the command result contract. Examples: `session_created` and `task_assigned` after `kkachi-agent-network delegate new`; `session_created` after `kkachi-agent-network council new`; `preparation_requested` after `kkachi-agent-network council prepare`; `stream_cursor_acknowledged` after `kkachi-agent-network stream ack`. Participant CLI events such as `assignee_acknowledged` have their own command path and their own `command_id`.
- Daemon-generated events with no CLI origin (for example `session_budget_exceeded`, `escalation_timeout`, `redaction_applied`): may be null. When null, `causation_event_id` is required.

`causation_event_id` rules:

- Required whenever `command_id` is null.
- Empty only for session-initial events such as `session_created`.

### CLI/plugin command equivalence

Every state-mutating Hermes plugin tool or slash command must map to a canonical `04-cli-spec.md` command and the same daemon command model. The plugin may add ergonomic defaults, gateway context capture, or slash-command parsing, but it must not create extra state transitions outside the protocol. The daemon's structured response, command-id idempotency, authorization, redaction, and fail-closed validation are authoritative for both paths.

### Idempotency enforcement

- The daemon maintains a `commands_seen` table keyed by `command_id`.
- A duplicate `command_id` within a session returns the prior recorded result and does not re-execute.
- A duplicate `event_id` on append is a hard error; the daemon stops writes and flags storage corruption.

### Replay rules

- `channel.jsonl` can be replayed from offset 0 to rebuild `network.sqlite`.
- Replay is deterministic and side-effect free. No runner calls. No outbound notifications of any kind (the daemon never originates outbound delivery in normal operation either; this is doubly true during replay).
- Replay reads `schema_version` per event and routes through registered migrations. Unknown versions halt replay with `migration_required`. Replay is independent of whether the original command arrived through the CLI or Hermes plugin because both paths emit the same daemon command/event model.
- Replay rebuilds pending escalation batch projections (`escalation_batches`, `escalation_batch_items`, session pending counters, `waiting_user_escalation_event_id`) from `escalation_batched`, `user_escalation_requested`, `escalation_rate_limited`, and related events. Replay must not deliver escalation notifications and must not create new `user_escalation_requested` events merely because a batch deadline is in the past — timer-driven flush is daemon runtime behavior after replay completes.
- Replay uses `channel.jsonl` and existing session files. It must **not** reread live `<data_home>/registry.yaml` to reinterpret historical session participants; historical sessions use their recorded events and per-session `registry_snapshot.yaml`. Live registry edits must not alter past session meaning.
- Projection rebuild is replay. `kkachi-agent-network storage rebuild-projection` must follow the same side-effect-free rules as replay: no runner calls, no outbound notifications, no timer-driven event creation, no registry reinterpretation of historical sessions, and no append to `channel.jsonl`. The detailed operational procedure lives in `17-disaster-recovery.md`.
- Replay rebuilds `surface` and `linked_authority` projections only from durable events such as `session_created`, `council_finalized`, and `council_unresolved`. Replay must not call Discord APIs, create Kanban comments, write Vault notes, or infer missing return-path evidence from thread/card/note identifiers.
- Replay and projection rebuild for Discord-thread councils must also rebuild attendance and agenda projections from `attendance_requested`, terminal `member_attended` events for required participants, and `agenda_locked`. Missing projection for those events is an operational/acceptance failure, not a reason to infer state from Discord messages or Hermes plugin/gateway delivery history.

### Linked authority return evidence

Kanban/Vault authority return is a moderator/Gray workflow, not a daemon side effect.

Rules:

- `council_finalized` records the council decision. It does not by itself prove that a linked Kanban card or Vault decision note has been updated.
- When `session_created.payload.linked_authority` is present, finalization records `linked_authority_result.status` as `posted`, `failed`, or `pending_followup`.
- `posted` requires concrete evidence such as a Kanban comment id, Vault note path, or equivalent immutable pointer.
- `failed` requires `failure_reason` and must keep the origin card blocked/pending review or produce a linked follow-up.
- `pending_followup` requires a linked follow-up/review card, pending-review handoff, or equivalent evidence pointer.
- `failed` and `pending_followup` are not terminal success for the authority return path. Final user/operator reports may say the council decision finalized, but must not say Kanban/Vault return completed.
- Replay, transcript, export, status, and projection rebuild remain side-effect free: they must not create Kanban comments, write Vault notes, or transform configured targets into `posted` evidence.
- Transcript/export/status must expose `linked_authority_result.status` and evidence. If status is missing, or if `posted` lacks concrete evidence, linked authority return completion is unproven. If status is `failed` or `pending_followup`, the council decision may be finalized but Kanban/Vault return remains incomplete.

### Visible surface rendering evidence

Visible surface rendering is a read/projection concern over durable events. Discord threads, gateway messages, Kanban comments, Vault notes, transcript rows, and export rows are not lifecycle sources. The authoritative rendering order is the stream cursor sequence, not message timestamps or external message order.

Minimum rendering/projection inputs:

- `session_created.payload.surface`: identifies the visible room and delivery owner. For `discord_thread`, `thread_id` is required. These fields are evidence/configuration pointers only.
- `session_created.payload.linked_authority`: identifies required return targets, not completed returns.
- `speaker_selected`: grants the floor for a turn. Visible renderers must not infer floor ownership from Discord author/order.
- `speech`: carries participant-visible speech content. A renderable member speech turn requires both the `speech` event and a matching floor grant for the same turn.
- `moderator_intervention`, `draft_conclusion`, and vote events: render typed moderator/consensus state as non-terminal presentation over durable events, never as external free-form state. A draft or vote is not a final closeout.
- `council_finalized`, `council_unresolved`, and other terminal outcome events: render the durable outcome only after the terminal event exists, and treat visible closeout completion as a separate delivery/projection evidence fact.
- `council_finalized.payload.surface_evidence` and `payload.linked_authority_result`: expose delivery/return status and evidence pointers for final result delivery.

Delivery/evidence statuses are interpreted uniformly across status, transcript, export, and projection rebuild:

- `posted`: a concrete immutable pointer exists for the performed visible or linked-authority return.
- `failed`: the attempt failed and records a reason plus follow-up handling evidence.
- `pending_followup`: the durable decision may exist, but visible/linked return remains incomplete and points to a follow-up or pending-review handoff.
- missing status or `posted` without evidence: unproven delivery.

Terminal outcome projection must preserve the split between daemon outcome and visible UX success. A valid terminal event moves the durable session to terminal status, but transcript/export/status/plugin projections must not mark the moderator closeout as visibly delivered unless they can point to posted surface evidence for that terminal event. If the terminal event is present and delivery evidence is missing, failed, pending, or points to a different draft/version/event, projection must fail closed as visible closeout incomplete.

Replay, transcript, export, status, and projection rebuild must remain side-effect free. They may rebuild and expose surface/return evidence from existing durable events; they must not call Discord APIs, create Kanban comments, write Vault notes, synthesize message ids, infer delivery from configured thread/card/note targets, or convert failed/pending/missing evidence into `posted`.

### Registry readiness

The daemon is not ready to accept session-creating commands until registry validation succeeds. Registry validation includes both file safety (per `12-security.md`) and schema validation. If registry validation fails before a session is bound, the failure is written to `operational.log` and the daemon reports not ready.

Session creation revalidates the registry and writes `registry_snapshot.yaml` atomically before appending `session_created`. After session creation, dispatch and replay use the per-session snapshot as the authority for participant identity.

After replay on daemon startup, the daemon scans pending escalation batches. If a pending batch deadline has passed, the daemon may append a new runtime event to flush, cancel, or rate-limit the batch according to policy. This happens after replay completes and is not part of replay itself.

### Schema migration procedure

- Migrations live as numbered modules in `internal/storage/migrations/`, one per `from_version → to_version` step (e.g. `m_001_to_002.py`).
- Each migration exports a pure function `migrate(event: dict) -> dict` that returns a transformed event envelope. Side effects are forbidden.
- Registration is by file presence: at startup the daemon discovers every `m_*.py` and builds the chain. A gap in the chain (e.g. `m_001_to_002.py` and `m_003_to_004.py` present but `m_002_to_003.py` missing) is a startup error.
- Replay applies migrations in order until the event reaches the daemon's current `schema_version`. If no chain reaches the current version, replay halts with `migration_required` and the missing step is named in the error.
- Migrations only transform event payloads; they do not invent new events, drop existing ones, or reorder the log.

### Principal addressing

`from` is the single event originator. `to` is the list of semantic recipients.

Reserved principals:

- `user`
- `kkachi-agent-networkd`

Registry member ids must not collide with reserved principals (per `12-security.md`).

Allowed `from` values: registry member ids, `user`, `kkachi-agent-networkd`.

Allowed `to` recipients: registry member ids and `user`. `to: []` is allowed only for unaddressed session audit events. `kkachi-agent-networkd` is not a normal recipient.

Broadcast is represented by an explicit recipient list. Special values such as `"all"`, `["all"]`, or `"*"` are forbidden.

Recipients within a single event are unique after normalization, and the daemon stores them in canonical order (session participant order, with the reserved external principal `user` last). Recipient order has no semantic meaning.

### Clock authority

- The cursor sequence (`cur_<offset>_<event_id>`) is the authoritative ordering for all events in a session. Consumers must rely on cursor order, not timestamps.
- External surface timestamps or Discord message order are display/evidence data only and never override cursor order.
- `created_at` on every event is a daemon-side wall-clock value used for human display and audit only. It is never used for ordering, deduplication, or causality.
- Member-supplied timestamps inside event payloads (e.g. a `last_heartbeat_at` reported by a runtime) are advisory. The daemon treats them as untrusted display data and never compares them across members for ordering.
- If the daemon's wall clock jumps backward between events, cursor ordering remains correct. Audit tooling that surfaces non-monotonic `created_at` should flag it but must not reject the event.

## 3. Cost accounting

### Runner invocation accounting

Runner invocation accounting is **separate** from cost accounting.

- `cost` records parsed token and USD information.
- `runner` records actual bounded adapter invocation metadata.
- `runner_calls_total` is computed from durable `runner_invocation_started` events, not from `cost.source`.
- A runner invocation with `cost: null` still counts as a runner call.

### Per-event cost

Runner terminal events include a `cost` field in the envelope. The value may be an object or `null`.

- Parsed cost available: `cost` is an object.
- Parsed cost unavailable: `cost` is `null`.
- Non-runner events omit `cost` entirely.

`cost: null` means missing measured cost. It must **not** be treated as zero cost.

```json
"cost": {
  "tokens_in": 1234,
  "tokens_out": 567,
  "usd_estimate": 0.0321,
  "source": "hermes-agent-stderr-parse"
}
```

Missing cost example:

```json
"cost": null
```

### `cost.source` per adapter

Each registered adapter declares the `cost.source` string it produces and the format the parser expects:

| `adapter_kind` | `cost.source` | Format |
|---|---|---|
| `hermes-agent` | `hermes-agent-stderr-parse` | The Hermes profile wrapper writes a single-line JSON object to **stderr** with the shape `{"hermes_cost": {"tokens_in": int, "tokens_out": int, "usd_estimate": float}}` immediately before exit. The parser scans the last 32 KB of stderr for the first line that JSON-parses and contains the `hermes_cost` key. If no such line is found, the runner emits the event with `cost: null` (legitimate missing cost), not an estimate. The redaction pipeline runs before this parse so secrets in earlier stderr lines do not leak through the cost extraction path. |

Future adapters added per §1 must add a row here with their own source string and parsing rule. A daemon must refuse to load an adapter whose declared `cost.source` is missing from this table.

### Session budget limits

```yaml
limits:
  max_runner_calls: 500
  max_tokens_total: 2000000
  max_usd: 25.00
  max_elapsed_sec: 86400
```

Budget breaches emit `session_budget_exceeded` with `limit_kind`, `observed`, `limit`, and transition the session to `blocked`. Exit from `blocked` requires `limits_extended` with explicit user authorization recorded in the payload.

Pre-dispatch checks (before launching a runner invocation):

- current session is not terminal;
- session is not blocked by an unresolved budget/security condition;
- `runner_calls_total < max_runner_calls`;
- `max_elapsed_sec` has not been exceeded;
- registry and wrapper validation still pass;
- member dispatch lock allows the invocation.

If `runner_calls_total >= max_runner_calls`, the daemon must not launch the subprocess. It emits `session_budget_exceeded` with `limit_kind: max_runner_calls` unless the same unresolved block already exists.

Post-event checks (after appending a terminal runner event with parsed cost):

- if `tokens_in_total + tokens_out_total` exceeds `max_tokens_total`, the daemon emits `session_budget_exceeded` with `limit_kind: max_tokens_total`;
- if `usd_estimate_total` exceeds `max_usd`, the daemon emits `session_budget_exceeded` with `limit_kind: max_usd`.

### Escalation limits

```yaml
limits:
  max_user_escalations: 10
```

Escalation cap breaches emit `escalation_rate_limited` and block further escalation delivery. They do not transition the session to `blocked`; the session stays in its prior phase until the user authorizes `limits_extended` or the moderator acts through other channels. See `07-moderator-policy.md` for the normative escalation policy.

### Escalation batching and waiting_user

`escalation_batched` is a pre-user-facing audit event for low-urgency escalation candidates. It does **not** enter `waiting_user`, does not increment `user_escalations_total`, and does not by itself imply user delivery. The session remains in its prior phase while a batch is pending.

When the batch is flushed, the daemon records one `user_escalation_requested` event that bundles the source events. That event increments `user_escalations_total` and transitions the session to `waiting_user`.

Deduplication and cap checks happen before the final `user_escalation_requested` is emitted: a duplicate low-urgency candidate yields `escalation_deduplicated` (no new batch item, phase unchanged), and a flush that would exceed `max_user_escalations` yields `escalation_rate_limited` (no `user_escalation_requested`, phase unchanged).

The low-urgency batching window starts when the first `escalation_batched` event for that batch is appended. The batch must be flushed, cancelled, or rate-limited by `batch_deadline_at`.

`user_escalation_requested` has origin class `mixed`. Immediate moderator escalation is a participant CLI event. Manual batch flush through `kkachi-agent-network delegate escalation-flush` is a daemon-after-CLI event. Timer-driven or startup-reconciliation flush is daemon-internal. All daemon-generated flush events must carry a valid `causation_event_id` pointing to the batch or triggering policy event.

### Session roll-up

`sessions` table counters are projected at event append time and replay time:

- `runner_calls_total`: count of `runner_invocation_started` events.
- `tokens_in_total`: sum of `cost.tokens_in` from terminal runner events where `cost` is not null.
- `tokens_out_total`: sum of `cost.tokens_out` from terminal runner events where `cost` is not null.
- `usd_estimate_total`: sum of `cost.usd_estimate` from terminal runner events where `cost` is not null.
- `missing_cost_runner_calls_total`: count of terminal runner invocations whose `cost` is null.
- `user_escalations_total`: count of `user_escalation_requested` events.

The daemon must **not** compute `runner_calls_total` from `cost.source`.

### Missing cost

Cost parsing failure is allowed but visible.

When a terminal runner event has `runner` metadata and `cost: null`:

- the runner invocation remains counted in `runner_calls_total`;
- token and USD totals are not incremented;
- `missing_cost_runner_calls_total` increments;
- `kkachi-agent-network status --verbose` and `kkachi-agent-network limits show` must surface the missing cost count;
- transcript and export must preserve that the cost was missing.

The daemon must not invent token or USD estimates. Missing cost does not by itself transition the session to `blocked` unless a future explicit product limit is added.

## 4. Timeouts, cancel, retries

### Timeout classes

- `dispatch_timeout_sec` — per runner call. Default 180.
- `preparation_timeout_sec` — council prep. Default 600.
- `hand_raise_research_timeout_sec` — council research window. Default 600.
- `clarification_response_timeout_sec` — assignee clarification wait. Default 3600; the moderator may re-prompt or block.
- `escalation_response_timeout_sec` — user escalation wait **after `user_escalation_requested`**. Default 86400; exceeding moves the session to `blocked` with `escalation_timeout`. The timer does not start at `escalation_batched`; pending low-urgency batches use the batching window, not this timeout.

All are overridable in session `limits`.

### Cancellation

- `session_cancelled` triggers `RunnerAdapter.cancel(handle)` for every pending dispatch.
- Adapters treat cancel as cooperative best-effort. The daemon hard-kills the subprocess after `dispatch_timeout_sec + 15s`.
- In-flight runner results that arrive after cancel are discarded and recorded as `runner_result_discarded`.
- `runner_result_discarded` must include the same `runner.invocation_id` as the invocation it terminates. The invocation remains counted because `runner_invocation_started` was already appended.

### Retries

- Runner invocations retry up to `runner_max_retries` (default 2) on non-semantic failures: dispatch timeout, transport error, non-zero exit with empty output.
- Retries reuse the original `command_id`. They reuse the `session_handle` when safe; otherwise they capture a fresh handle and record `session_handle_rotated`.
- `runner_retry_attempted` records the retry **policy decision** with attempt number and prior error class. It does **not** itself count as a runner call.
- The actual retry subprocess is counted only when the daemon appends a new `runner_invocation_started` event with a new `runner.invocation_id`.
- Retries reuse the original `command_id` for idempotency correlation, but each retry has its own `runner.invocation_id`.
- Semantic failures (well-formed error payloads from the runner) are not retried.

## 5. Concurrency model

- Release v1 invariant: exactly one active session across delegation and council modes.
- Within a session, stream subscribers for different members may run concurrently. Bounded dispatch to different members may run concurrently. Dispatch to the same member is serialized by the member dispatch lock.
- The engine applies state transitions on a single worker thread. Runner I/O runs on a bounded thread pool (`runner_worker_count`, default 4).
- Ordering guarantee: every state mutation commits to `channel.jsonl` before the corresponding SQLite projection update. A write that reached JSONL but not SQLite is tolerated and repaired on startup by replay.
- Future concurrency (multi-session, per-member queues) is a Future release candidate; it must be additive over this contract. Release v1 implementations must not assume single-session anywhere outside the active-session lock.

### Phase and status

`phase` is the exact lifecycle state stored in every event envelope (post-transition value, see `06-state-machine.md`). `status` is a derived roll-up stored only in projections (`session.yaml` and SQLite `sessions.status`). Business logic must use `phase` for transitions; UI, status output, query filtering, and active-session lock checks may use `status`. The event envelope does **not** include `status`.

### Active-session status

A session is active whenever its derived `status` is not `terminal`.

Allowed `status` values:

- `open`
- `blocked`
- `terminal`

`status` is derived from the exact state-machine `phase` per `05-storage-schema.md#phase-to-status-mapping`:

- terminal phases map to `terminal`;
- `blocked` maps to `blocked`;
- all other non-terminal phases map to `open`.

The active-session lock is held for both `open` and `blocked` sessions; only `terminal` releases it.

### Blocking and resume metadata

A blocking event that transitions the session to `blocked` records:

- envelope `phase: "blocked"`;
- payload `prior_phase` (the phase at the moment of the block);
- payload `resume_phase` (the phase to restore once the block is lifted);
- enough context to identify the blocking condition (limit kind, escalation reference, security category, etc.).

Recoverable block lifting is event-specific:

- Budget and limit blocks (recorded by `session_budget_exceeded`) are lifted by `limits_extended`.
- Manual, external-dependency, scope-conflict, and policy/process blocks (recorded by `session_blocked`) are lifted by `session_resumed`.
- Security blocks (recorded by `security_violation` with session phase `blocked`) may be lifted by `session_resumed` only when the security model defines the violation category as remediable and verification has passed.
- Escalation timeouts (recorded by `escalation_timeout`) are lifted by recording a fresh user response or by extending `escalation_response_timeout_sec`.

The lifting event must reference the original blocking event. The daemon must reject a lifting event that does not address the same blocking condition.

`limits_extended` must not become a generic unblock event. It records user authorization for budget or limit changes. `session_resumed` records that a non-budget recoverable blocking condition has been resolved.

Because `escalation_rate_limited` does **not** by itself transition the session to `blocked`, its envelope keeps the prior phase, and the derived `status` remains whatever that phase maps to (typically `open`).

## 6. Release v1 scope and Future release candidates

Terminology used throughout this section follows `README.md#terminology`: `Release v1` is the product release label; `Implementation Phase N` is the build-out execution order; `Future release candidates` are deferred capabilities not pre-assigned to a numbered future release.

### Release v1 delivers

Epics 1–11: registry, storage, daemon, CLI, runner adapter (`hermes-agent` only), delegation engine, review gate, council preparation/discussion/consensus, transcript/export, distribution, reliability.

### Future release candidates

No fixed epic numbers yet: advanced reviewer aggregation, additional runner adapter kinds (e.g. `claude-cli`, `codex-cli`, `gemini-cli`), moderator handover, speaker-scoring strategy plug-ins beyond the Release v1 default, future multi-session concurrency, and per-member queues.

### Forward-compatibility rules

Release v1 implementations must:

- use the full envelope (including `schema_version`, `command_id`, `causation_event_id`, `correlation_id`) from day one;
- implement stream cursors and member runtime acknowledgement from day one, exercised by both delegation and council flows;
- pass `adapter_kind` through the runner path, not bake one adapter into the engine, even though the Release v1 whitelist is exactly `["hermes-agent"]`;
- expose speaker-scoring weights and bonuses through configuration (engine reads from a config module overridable per session), so future scoring strategies are additive.

The full specification in this docs directory remains the product vision. Release v1 must not introduce contracts that would block Future release candidates.
