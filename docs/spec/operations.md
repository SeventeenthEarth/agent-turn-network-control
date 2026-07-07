# Operations

---

## Merged from `docs/spec/operations.md`

# Operational Contracts

These contracts are cross-cutting and apply to every epic. Streaming, runner, protocol, storage, and engine modules depend on them. They must be fixed before RUNRT (member runtime and runner adapter).

## 0. Stream and member runtime contract

### Purpose

Make real agents active participants instead of passive one-shot subprocess responses. The daemon owns state and event durability; the Hermes plugin is the preferred agent-facing interface; the CLI is the stable canonical diagnostics/recovery/manual interface; both use the ATN protocol client/contract and member runtimes listen and act.

### Interface

```bash
# Canonical CLI fallback
atn-control stream <session_id> --member <member> --since <cursor> --follow --format ndjson
atn-control stream ack <session_id> --member <member> --cursor <cursor>

# Preferred Hermes integration
Hermes plugin stream/tail tool -> ATN protocol client/contract -> daemon stream
Hermes plugin cursor-ack tool -> ATN protocol client/contract -> daemon ack
```

Stream frames are newline-delimited JSON with `{cursor, is_replay, event}`. The event is the full envelope from `protocol-and-cli.md`.

### Rules

- Stream visibility follows JSONL durability: append first, publish second.
- Member runtimes persist their own acknowledged cursor.
- Reconnect replays missed events before live events.
- Cursor gaps, unknown schema versions, or storage corruption fail closed.
- Member-originated state changes are typed ATN commands sent through a protocol client, normally via Hermes plugin tools and always with a canonical CLI command equivalent; they are not direct daemon-state mutations.
- One-shot runner calls are bounded adapter operations. They must not be the primary council turn loop.
- Heartbeats from stream subscribers update `stream_subscribers`; stale subscribers emit `stream_subscriber_stale`.
- Participant runtime readiness is derived from durable session events, not a materialized readiness table. Required members are unready unless the log proves subscriber presence, valid cursor ack, fresh cursor ack, fresh heartbeat, attendance/preparation response or timeout/failure evidence when required, and selected-runner prerequisites when a speaker is selected.
- Stream frames carry the full event envelope. In that envelope, `from` is a string and `to` is always an array of strings (per `protocol-and-cli.md`).
- `to` is semantic addressing, not stream access control. Member runtimes may observe events not addressed to them; they decide whether to act by inspecting event type, sender, recipients, role, phase, and policy. Read permissions are governed by `12-operations.md`.

### Heartbeat cadence

- Stream subscribers send a heartbeat every **30 seconds**.
- A subscriber with no heartbeat for **90 seconds** is marked stale and the daemon emits `stream_subscriber_stale` once.
- A subscriber with no heartbeat for **300 seconds** triggers the session policy: the moderator may repoll the member runtime, mark its participation partial, or block the session per `architecture.md`.
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

### Runner stdout semantic framing

The Release v1 `hermes-agent` selected-runner producer contract is compact JSONL on stdout: exactly one JSON object per line for the canonical semantic response, no markdown fence, and no surrounding prose. Diagnostics, wrapper logs, provider warnings, and human-readable debugging belong on stderr or in structured evidence fields.

For council selected-speaker output, the canonical stdout record is a `type: "speech"` object with a `payload` containing visible `speech` plus any `claims[]`, `stance_links[]`, `contribution_type`, `new_axis_reason`, and `evidence[]` fields needed by ARGUE validation. Example:

```json
{"type":"speech","payload":{"speech":"Visible participant answer only.","claims":[],"stance_links":[],"contribution_type":"support","new_axis_reason":null,"evidence":[]}}
```

The consumer parser remains compatibility-tolerant for real Hermes output: Hermes CLI control lines such as `session_id: ...` are ignored before parsing the semantic response, and a single pretty/multiline JSON object may be normalized before canonical validation. For selected-runner `speech` events only, `payload.message`, `payload.content`, or `payload.text` may be copied into missing `payload.speech` before canonical council-speech validation; the producer contract remains `payload.speech`. CLI `council.grant` waits longer than ordinary daemon commands because selected-runner response generation may take tens of seconds; ordinary daemon command timeouts remain short. Delivery/fallback-only JSON must still classify as `adapter_command_mismatch`, and malformed JSON must still classify as `malformed_or_missing_response`.

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

Every event envelope (see `protocol-and-cli.md`) includes:

- `schema_version` — integer; bumped on any breaking envelope change. Readers refuse unknown versions unless a migration is registered.
- `event_id` — unique per persisted event (ULID recommended).
- `command_id` — set by the originator per the rules below.
- `causation_event_id` — the event that directly caused this one.
- `correlation_id` — logical thread across related commands and events; defaults to `session_id`.
- `from` — required string principal id (the single event originator).
- `to` — required `array<string>` of recipient principal ids.
- `runner` — optional object identifying a bounded adapter invocation attempt. Present on runner accounting events (`runner_invocation_started`, `runner_invocation_failed`, `runner_result_discarded`) and on terminal semantic events produced by a runner invocation. Includes `invocation_id`, `adapter_kind`, `member`, `attempt`, `is_retry`, `source_command_id`, `status`, `duration_sec`. See `protocol-and-cli.md` for the field schema and allowed status values.

Changing `to` between string and array form is a breaking envelope change. The initial Release v1 schema uses array form; implementations that have already persisted string-form events must introduce a `schema_version` migration rather than silently accepting both shapes.

`command_id` rules:

- CLI-originated events: required. Retries of the same CLI command reuse the id and are deduplicated by the daemon.
- Moderator-emitted delivery events (`user_escalation_delivered`, `user_escalation_delivery_failed`) are typed ATN writes from the moderator runtime per `01-overview.md` and `architecture.md`, normally through plugin tools with the CLI as the canonical fallback. They follow the participant-command rule: each carries its own `command_id`, retries of the same delivery report reuse the id, and a duplicate is deduplicated by the daemon. `causation_event_id` for these events points to the originating `user_escalation_requested`.
- Daemon-generated events that follow directly from a CLI command reuse or correlate with the originating `command_id` according to the command result contract. Examples: `session_created` and `task_assigned` after `atn-control delegate new`; `session_created` after `atn-control council new`; `preparation_requested` after `atn-control council prepare`; `stream_cursor_acknowledged` after `atn-control stream ack`. Participant CLI events such as `assignee_acknowledged` have their own command path and their own `command_id`.
- Daemon-generated events with no CLI origin (for example `session_budget_exceeded`, `escalation_timeout`, `redaction_applied`): may be null. When null, `causation_event_id` is required.

`causation_event_id` rules:

- Required whenever `command_id` is null.
- Empty only for session-initial events such as `session_created`.

### CLI/plugin command equivalence

Every state-mutating Hermes plugin tool or slash command must map to a canonical `protocol-and-cli.md` command and the same daemon command model. The plugin may add ergonomic defaults, gateway context capture, or slash-command parsing, but it must not create extra state transitions outside the protocol. The daemon's structured response, command-id idempotency, authorization, redaction, and fail-closed validation are authoritative for both paths.

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
- Projection rebuild is replay. `atn-control storage rebuild-projection` must follow the same side-effect-free rules as replay: no runner calls, no outbound notifications, no timer-driven event creation, no registry reinterpretation of historical sessions, and no append to `channel.jsonl`. The detailed operational procedure lives in `17-operations.md`.
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

The daemon is not ready to accept session-creating commands until registry validation succeeds. Registry validation includes both file safety (per `12-operations.md`) and schema validation. If registry validation fails before a session is bound, the failure is written to `operational.log` and the daemon reports not ready.

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
- `atn-controld`

Registry member ids must not collide with reserved principals (per `12-operations.md`).

Allowed `from` values: registry member ids, `user`, `atn-controld`.

Allowed `to` recipients: registry member ids and `user`. `to: []` is allowed only for unaddressed session audit events. `atn-controld` is not a normal recipient.

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

Escalation cap breaches emit `escalation_rate_limited` and block further escalation delivery. They do not transition the session to `blocked`; the session stays in its prior phase until the user authorizes `limits_extended` or the moderator acts through other channels. See `architecture.md` for the normative escalation policy.

### Escalation batching and waiting_user

`escalation_batched` is a pre-user-facing audit event for low-urgency escalation candidates. It does **not** enter `waiting_user`, does not increment `user_escalations_total`, and does not by itself imply user delivery. The session remains in its prior phase while a batch is pending.

When the batch is flushed, the daemon records one `user_escalation_requested` event that bundles the source events. That event increments `user_escalations_total` and transitions the session to `waiting_user`.

Deduplication and cap checks happen before the final `user_escalation_requested` is emitted: a duplicate low-urgency candidate yields `escalation_deduplicated` (no new batch item, phase unchanged), and a flush that would exceed `max_user_escalations` yields `escalation_rate_limited` (no `user_escalation_requested`, phase unchanged).

The low-urgency batching window starts when the first `escalation_batched` event for that batch is appended. The batch must be flushed, cancelled, or rate-limited by `batch_deadline_at`.

`user_escalation_requested` has origin class `mixed`. Immediate moderator escalation is a participant CLI event. Manual batch flush through `atn-control delegate escalation-flush` is a daemon-after-CLI event. Timer-driven or startup-reconciliation flush is daemon-internal. All daemon-generated flush events must carry a valid `causation_event_id` pointing to the batch or triggering policy event.

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
- `atn-control status --verbose` and `atn-control limits show` must surface the missing cost count;
- transcript and export must preserve that the cost was missing.

The daemon must not invent token or USD estimates. Missing cost does not by itself transition the session to `blocked` unless a future explicit product limit is added.

## 4. Timeouts, cancel, retries

### Timeout classes

- `dispatch_timeout_sec` — per selected-speaker runner call. Implemented default is 30 seconds when no session override is set; Discord live-visible selected-runner councils must use the implemented `NEWFIX-005` policy of 150 seconds or an explicitly approved alternative, with `selected_runner_timeout_evidence` projected through council status and `bundle_manifest.json` while `stream.status` remains unchanged. If the later daemon-effective timeout diverges from that guarded timeout, selected-runner launch must fail closed with `selected_runner_timeout_policy_blocked` and diagnostic timeout evidence.
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

`phase` is the exact lifecycle state stored in every event envelope (post-transition value, see `architecture.md`). `status` is a derived roll-up stored only in projections (`session.yaml` and SQLite `sessions.status`). Business logic must use `phase` for transitions; UI, status output, query filtering, and active-session lock checks may use `status`. The event envelope does **not** include `status`.

### Active-session status

A session is active whenever its derived `status` is not `terminal`.

Allowed `status` values:

- `open`
- `blocked`
- `terminal`

`status` is derived from the exact state-machine `phase` per `architecture.md#phase-to-status-mapping`:

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

---

## Merged from `docs/spec/operations.md`

# Security Model

These rules are mandatory. Every violation must fail closed and be recorded. Whether the record goes to `channel.jsonl` as a session `security_violation` event or to the daemon operational log depends on whether the violation is tied to an active session; see Failure behavior below.

## Threat model

- The registry file is an execution-permission boundary. A writable registry grants subprocess execution under the current user.
- Because the registry controls member identity, wrapper executable paths, workspaces, adapter kinds, and environment allowlists, the registry file and its containing data directory are security-sensitive. Unsafe ownership, unsafe permissions, symlinks, or parse-time ambiguity must fail closed before the daemon accepts commands.
- Member wrappers run with full user privilege. A misconfigured or malicious registry is an RCE surface.
- Transcripts, artifacts, and raw logs may carry secrets. `channel.jsonl` is durable, so redaction must occur before writes.
- CLI arguments and Hermes plugin tool inputs from the moderator flow into event payloads and into runner prompts. They must be treated as untrusted data.

## Registry validation

### Registry file safety

`<data_home>/registry.yaml` is a security-sensitive execution configuration file. The daemon validates the registry file before parsing the YAML schema.

Required file properties:

- path is exactly `<data_home>/registry.yaml`;
- file exists;
- file is a regular file;
- file is not a symlink;
- owner is the current daemon user or root;
- file is not group-writable;
- file is not world-writable.

Recommended mode: `0600` (also acceptable: `0640`, `0644`). Group/world readable registry files are allowed because the registry must not contain secret values; secret values must never be placed in the registry, and `env_allowlist` contains variable names only.

### Data home safety

The resolved `<data_home>` must be a safe directory before the daemon reads the registry or writes runtime state.

Required directory properties:

- exists;
- is a directory;
- owner is the current daemon user;
- not group-writable;
- not world-writable.

Recommended mode: `0700`. If `<data_home>` does not exist, setup/init code may create it with mode `0700`. If `<data_home>` exists with unsafe ownership or permissions, the daemon fails closed and does not automatically `chmod` it during start; permission repair is allowed only through an explicit setup or repair command that reports the action.

`atn-control doctor` is read-only by default. Permission repair may be offered only through an explicit `--repair-permissions` flag or through `atn-control init`, both of which must report every change they make. Daemon start must not silently repair unsafe ownership or permissions.

### Registry symlink rule

Registry symlinks are forbidden. Unlike wrapper paths, which allow restricted canonicalization for Hermes alias workflows, `registry.yaml` is a fixed source-of-truth file and must be a regular file at the expected path. If `lstat(<data_home>/registry.yaml)` reports a symlink, validation fails with `registry_symlink_forbidden`.

### Registry load procedure

The daemon must reduce check/use ambiguity (TOCTOU) when loading the registry. Required procedure:

1. Resolve `<data_home>/registry.yaml`.
2. `lstat` the path.
3. Reject if the path is a symlink.
4. Open the file read-only.
5. `fstat` the opened file descriptor.
6. Verify regular file, owner, and mode on the opened file.
7. Read YAML content from the same file descriptor.
8. Parse and validate the strict registry schema.
9. Compute a SHA-256 hash of the loaded content.
10. Use that same loaded content for session creation and snapshot writing.

The daemon must not validate one file and parse another. If metadata observed before open and after open indicate an unexpected file replacement, validation fails with `registry_changed_during_load`.

File safety validation runs **before** YAML schema validation; schema validation runs only after file safety passes. Any validation failure aborts daemon start or session creation.

### Registry schema validation

- Registry YAML loads through a strict schema. Required per-member fields: `display_name`, `wrapper`, `workspace`, `role`, `enabled`, `adapter_kind`.
- The registry root requires `members` and may include only `schema_version`, `wrapper_path_allowlist`, and `secret_patterns`.
- `role` is a free-form short string. Recommended vocabulary: `moderator`, `assignee`, `reviewer`, `participant`, `observer`. Other values are accepted; the daemon does not derive permissions from `role` (it is informational and projected into `session_participants.role` for query/UI use only).
- Optional fields: `strengths`, `env_allowlist`, `notes`, `runtime_kind`, `autostart`, `stream_filter`.
- Unknown `runtime_kind` is rejected at load time when present. Supported initial value: `hermes-cli-stream`.
- Unknown keys are rejected, not ignored.
- Unknown `adapter_kind` is rejected at load time.
- Any validation failure aborts daemon start. The daemon never runs on a partially valid registry.
- Disabled members still pass schema, id, workspace, adapter/runtime, and env allowlist validation, but wrapper resolution is required only for enabled members.

Reserved principal names cannot be used as registry member ids. Reserved principals:

- `user`
- `atn-controld`

Examples rejected at registry load time: `members.user`, `members.atn-controld`. The daemon uses these reserved principals in event envelopes; allowing registry members with the same ids would make audit records ambiguous.

## Wrapper execution policy

- `wrapper` is either an absolute path on disk or a bare command name. Bare names are resolved through a configured `wrapper_path_allowlist`; the daemon never consults `$PATH`.
- The default `wrapper_path_allowlist` is `["/usr/local/bin", "/opt/hermes", "~/.local/bin"]`. `~/.local/bin` is **required** in the default list because Hermes Agent creates per-agent alias binaries there (e.g. `wolong` for a member named `wolong`); removing it would block the documented Hermes alias workflow.
- `~/.local/bin` is user-writable. The residual risk is mitigated by the per-file checks below (regular file, owned by the current user or root, not group- or world-writable, single-symlink canonicalization) and by the env allowlist. Operators who do not use Hermes aliases may shrink the allowlist by configuration.
- After resolution, the daemon canonicalizes the path (following a single symlink) and verifies: the target exists, is a regular executable file, is not group- or world-writable, is owned by the current user or root, and lies under the allowlist after canonicalization.
- Resolution and canonicalization failures are distinct `security_violation` categories: `wrapper_unresolvable`, `wrapper_outside_allowlist`, `wrapper_permissions_unsafe`.
- Wrappers are invoked with an argv list, never through a shell string. `shell=True` is forbidden.
- Prompt text is passed as an argv slot or on stdin. It is never interpolated into a command string.
- Working directory for the subprocess is the member `workspace`. Daemon never executes commands with `cwd` outside the workspace or the session directory.

## Workspace isolation

- The daemon does not write into member workspaces.
- Artifact ingestion occurs only through explicit `work_submitted.artifacts` entries.
- Artifact paths are canonicalized. After canonicalization the path must lie under the member's `workspace` or under the current session directory (`sessions/<id>/`). Paths containing `..`, paths escaping both roots, and symlinks leaving the permitted roots are all rejected.

## Artifact contract

For every artifact reference:

- source path, stored path (`sessions/<id>/artifacts/<artifact_id>`), size in bytes, SHA-256 hash, MIME sniff result, and ingestion timestamp are recorded in the `artifacts` table.
- Files exceeding `max_artifact_bytes` (default 25 MB) are rejected.
- MIME whitelist is session-configurable. Default allows text, source code, markdown, JSON, YAML, PDF, and common image formats.
- The daemon copies the file into the session directory. The `work_submitted` payload always references the copy, never the source.

## Environment sanitization

- Member subprocess environment is minimal by default: `PATH`, `HOME`, `LANG`, `LC_*`. Nothing else from the daemon environment is propagated.
- Additional variables are passed only when explicitly listed in the member `env_allowlist`.
- Variables matching known secret prefixes/patterns (`*_API_KEY`, `*_TOKEN`, `*_SECRET`, plus the user-configurable `secret_patterns` regex list) are **blocked by default** even if present in the daemon environment. They are passed to the subprocess only when explicitly listed in `env_allowlist`.
- Variables that match a secret pattern *and* are explicitly listed in `env_allowlist` are passed to the subprocess in plaintext, but every reference to them in the operational log (`<data_home>/operational.log`) records only the variable name with the value rendered as `<redacted>`. The plaintext value never reaches `channel.jsonl`, `raw_logs/`, transcripts, or any export bundle.
- Forbidden in `env_allowlist`: literal values for secrets, glob patterns wider than a single variable name, and any name beginning with `LD_` or `DYLD_` (those override loader behavior and are rejected with `security_violation: env_allowlist_unsafe`).

## Secret redaction

- All runner `stdout`/`stderr` is scanned before being written to `raw_logs/`. Detected patterns include: Anthropic/OpenAI/Google API key prefixes, AWS access keys, JWT tokens, PEM private key headers, and user-configured regex.
- Matches are replaced with `<redacted:class>` in the durable text.
- Payload fields that carry user-supplied content (`question`, `answer`, `message`, `summary`, `speech`, `final_summary`) are scanned with the same rules before append to `channel.jsonl`.
- Every redaction emits a `redaction_applied` event with pattern class, count, and the source event id. The redacted value itself is never stored.

## Command-injection surface

- CLI arguments and Hermes plugin tool inputs flowing into event payloads and runner prompts are treated as opaque data. No CLI or plugin path interpolates payload content into shell commands.
- Registry-defined `workspace` is used only as a read reference for artifact source and as `cwd` for the wrapper invocation. It is never used as a base for arbitrary subprocess calls.
- CLI itself never spawns shells; the Hermes plugin must not spawn shells for normal ATN state mutations; only the daemon runner invokes subprocesses, and only through the adapter interface defined in `operations.md`.
- CLI-supplied and plugin-supplied `from` and `to` principals are treated as untrusted input. The daemon validates that `from` is an authorized originator for the command, that `to` contains only valid recipients for the session or the reserved external principal `user`, that `to` does not contain duplicates after normalization, and that `atn-controld` is not accepted as a normal participant-supplied recipient. Invalid principal references fail closed.


## Hermes plugin security boundary

- The Hermes plugin is not trusted as a state authority. It is an adapter over the ATN protocol client/contract and daemon command transport.
- The plugin must not append to `channel.jsonl`, write directly to `network.sqlite`, mutate daemon locks, or bypass daemon validation.
- Plugin load, reload, unload, Hermes gateway restart, or Hermes Agent restart must not mutate, corrupt, truncate, or otherwise alter `channel.jsonl`, SQLite projections, locks, or daemon state. Daemon durability is independent of plugin lifecycle.
- The plugin must not require ATN daemon access to Hermes profile secrets, gateway credentials, or Discord bot tokens. Gateway credentials remain inside Hermes/gateway configuration.
- Plugin tool inputs are untrusted and must receive the same validation, redaction, command-id/idempotency, and structured-error handling as CLI inputs.
- If the plugin shells out to `atn-control` for a compatibility fallback, argv-only invocation is required and shell interpolation is forbidden. Normal operation should use the ATN protocol client/contract.
- Plugin failure must fail closed and direct the operator to canonical CLI diagnostics/recovery; it must not simulate member profiles or continue by writing private state.

## Discord-thread surface security

For `surface.kind: discord_thread`, Discord ids and message ids are untrusted evidence pointers:

- `atn-controld` must not require or store raw Discord bot tokens for the first-pass surface binding; Discord delivery belongs to Hermes plugin/gateway capability or a separately approved bridge.
- Raw Discord tokens, webhook secrets, or gateway credentials must never appear in `surface`, `linked_authority`, event payloads, transcripts, exports, `channel.jsonl`, or `operational.log`.
- If a future Discord bridge is approved, token scope, allowed guild/channel/thread validation, inbound message validation, and redaction must be specified as a separate transport/security design before implementation.
- Discord message order is not accepted as causality or lifecycle authority. The daemon uses `channel.jsonl` cursor order only.
- Thread ids, channel ids, guild ids, and message ids are opaque strings validated for shape/length only unless a future bridge adds explicit Discord API verification.

## Stream access policy

- A stream subscriber must identify as a registry member or as the moderator. Unknown member names are rejected.
- Stream frames are read-only. All writes use typed ATN commands, exposed through plugin tools or canonical CLI fallback, that re-enter normal daemon validation and idempotency.
- A member runtime may acknowledge only its own cursor. The moderator may inspect all cursors but should not advance a member cursor.
- Cursor gaps, unknown schema versions, or replay corruption fail closed. The daemon must not silently skip events to keep a stream alive.
- The event envelope `to` field is **semantic addressing**, not access control. A valid session participant may observe session events even if a specific event is not addressed to that participant. The daemon must not rely on `to` alone to decide stream read permissions; read permissions are based on registry identity, moderator authority, session participation, and the rules in this section.

## Failure behavior

All violations of this document must:

1. abort the affected dispatch or ingestion immediately,
2. record the violation with `category`, redacted `observed`, and `action`,
3. transition the session to `blocked` if the violation concerns an active session.

Recording target:

- Session-scoped violations emit a `security_violation` event to `channel.jsonl` under the affected `session_id`. When the violation transitions the session to `blocked`, the event envelope uses `phase: "blocked"` and the payload records `prior_phase` and `resume_phase` (see `operations.md` §5 and `protocol-and-cli.md`).
- Pre-session violations (registry load failure, daemon start failure, or any violation raised before a session is bound) write the same payload shape to the daemon operational log instead. They do **not** carry session `phase`/`status`/`prior_phase`/`resume_phase` because no session exists yet. This is the one documented case where the log — not `channel.jsonl` — is the system of record.

### Registry violation routing

Pre-session registry violations are written to `<data_home>/operational.log`:

- missing registry at daemon start;
- unsafe registry owner;
- group/world writable registry file;
- registry symlink;
- unsafe `<data_home>`;
- registry schema parse failure during daemon start;
- registry validation failure during session creation **before** the session directory is committed.

If the resolved `<data_home>` itself is unsafe, unavailable, or ambiguous, the daemon fails closed without writing `operational.log`; writing to that location would rely on the path that was just rejected. Operators must repair or reinitialize the data home before pre-session logging can resume.

Session-scoped registry or snapshot violations emit `security_violation` to the active session's `channel.jsonl`:

- an active session's `registry_snapshot.yaml` is missing when dispatch requires it;
- an active session's snapshot is not a regular file;
- snapshot hash or schema validation fails during session-bound dispatch;
- snapshot read failure prevents safe member dispatch.

### Registry violation categories

Distinct from wrapper categories (`wrapper_unresolvable`, `wrapper_outside_allowlist`, `wrapper_permissions_unsafe`):

- `registry_missing`
- `registry_not_regular`
- `registry_symlink_forbidden`
- `registry_owner_unsafe`
- `registry_permissions_unsafe`
- `registry_data_home_unsafe`
- `registry_parse_error`
- `registry_schema_invalid`
- `registry_unknown_key`
- `registry_unknown_runtime_kind`
- `registry_unknown_adapter_kind`
- `registry_reserved_principal_collision`
- `registry_snapshot_write_failed`
- `registry_changed_during_load`

Category payload shape:

```json
{
  "category": "registry_permissions_unsafe",
  "observed": {
    "path": "<data_home>/registry.yaml",
    "mode": "0664",
    "owner_uid": 501
  },
  "action": "daemon_start_rejected"
}
```

Registry violation payloads must not include secret values. If a parse error includes content snippets, snippets must pass through the redaction pipeline before logging.

Release v1 validates the resolved `<data_home>` directory itself through `registry_data_home_unsafe`. It does not validate the full parent directory chain. If future releases add parent-chain validation, they must define exact rules, including symlink and sticky-bit behavior.

## Operational log

- Path: `<data_home>/operational.log`.
- Format: JSON Lines. Each line is one record with the keys `ts`, `level`, `event`, `category`, and a free-form `payload` object. The same redaction pipeline that protects `channel.jsonl` runs on every payload before it is written.
- Retention: 2 days from last activity (matches the raw runner log retention in `architecture.md`). Older lines are pruned by the daemon's housekeeping pass.
- Durability: operational, not durable. The system of record for anything that has a session is `channel.jsonl`; the operational log is only authoritative for pre-session events (per the rule above).

`atn-control doctor` may read and summarize operational log records, but it must not expose secret values. Any displayed payload must pass through the same redaction rules as `channel.jsonl`.

## Auditability

Every subprocess invocation is logged to the operational log with: wrapper path, adapter kind, argv length, cwd, environment keys (names only — values listed in `env_allowlist` that match a secret pattern are recorded as `<redacted>`), start timestamp, exit code, duration, and truncated stdout/stderr byte counts. The corresponding durable session record begins with `runner_invocation_started` in `channel.jsonl`; a terminal semantic event, `runner_invocation_failed`, or `runner_result_discarded` later records the outcome using the same `runner.invocation_id`.

Cost parsing occurs only after stderr has passed through the redaction pipeline (per `Secret redaction` above). If cost cannot be parsed after redaction, the terminal runner event records `cost: null`. The daemon must **not** treat cost parse failure as proof that no runner invocation occurred — invocation accounting is anchored on `runner_invocation_started`, not on `cost.source`.

---

## Merged from `docs/spec/operations.md`

# Observability

## Scope

This document defines what an operator can see while `atn-control` is running: health signals, metrics, structured logs, suggested SLOs/SLIs, alert thresholds, and dashboard organization. It is intended for local single-host deployments. It does not define a metrics export protocol; the metrics names below are stable identifiers that an exporter (Prometheus, OpenTelemetry, or a simple JSON endpoint) can map to its own format.

The normative SOT documents that observability depends on:

- Health and lifecycle: `02-architecture.md`, `architecture.md`.
- Stream and runner accounting: `operations.md`.
- Errors and structured output: `protocol-and-cli.md`.
- Security and operational log: `12-operations.md`.

## Health model

Each daemon health probe answers one question:

| Question                                                | Answer source                                                            |
| ------------------------------------------------------- | ------------------------------------------------------------------------ |
| Is the daemon running and accepting commands?           | `atn-control daemon status`                                            |
| Is the registry safe and parseable?                     | `atn-control doctor`                                                   |
| Is the active session lock consistent?                  | `atn-control doctor`, `atn-control status`                           |
| Is `channel.jsonl` parseable and projection-consistent? | `atn-control storage verify`                                           |
| Is replay reproducible?                                 | `atn-control storage rebuild-projection` after `storage verify` passes |
| Are stream subscribers up to date?                      | `atn-control status --verbose`                                         |

`atn-control doctor` is the operator-facing aggregate. It is read-only by default and must not chmod, chown, rewrite, or delete anything without an explicit repair flag (per `12-operations.md`).

## Metrics catalog

The daemon emits a set of stable metric identifiers. Implementations may expose them via Prometheus, OpenTelemetry, or another exporter; the names below are the contract.

| Area       | Metric                                        | Type      | Description                                            |
| ---------- | --------------------------------------------- | --------- | ------------------------------------------------------ |
| Daemon     | `atn-control_daemon_ready`                  | gauge     | 1 when daemon can accept commands; 0 otherwise         |
| Daemon     | `atn-control_active_sessions`               | gauge     | Active session count (Release v1: 0 or 1)              |
| Event log  | `atn-control_event_append_latency_ms`       | histogram | JSONL append latency                                   |
| Event log  | `atn-control_event_appends_total`           | counter   | Count of appended events, labeled by `type`            |
| Projection | `atn-control_projection_replay_duration_ms` | histogram | SQLite rebuild or replay duration                      |
| Stream     | `atn-control_stream_subscriber_count`       | gauge     | Active stream subscribers                              |
| Stream     | `atn-control_stream_lag_events`             | gauge     | Latest event cursor minus acknowledged cursor          |
| Stream     | `atn-control_stream_subscriber_stale_total` | counter   | Count of `stream_subscriber_stale` emissions           |
| Runner     | `atn-control_runner_invocations_total`      | counter   | Count of `runner_invocation_started`                   |
| Runner     | `atn-control_runner_failures_total`         | counter   | Runner terminal failures, labeled by `reason`          |
| Runner     | `atn-control_runner_missing_cost_total`     | counter   | Terminal runner events with `cost: null`               |
| Runner     | `atn-control_runner_duration_ms`            | histogram | Runner invocation duration                             |
| Escalation | `atn-control_waiting_user_age_seconds`      | gauge     | Age of current `waiting_user` state, 0 when not active |
| Escalation | `atn-control_pending_escalation_batches`    | gauge     | Pending batch count                                    |
| Escalation | `atn-control_user_escalations_total`        | counter   | Count of `user_escalation_requested`                   |
| Block      | `atn-control_blocked_session_age_seconds`   | gauge     | Age of current `blocked` state, 0 when not blocked     |
| Security   | `atn-control_security_violations_total`     | counter   | Security violations, labeled by `category`             |
| Storage    | `atn-control_replay_failures_total`         | counter   | Replay failures, labeled by `reason`                   |
| Storage    | `atn-control_jsonl_bytes`                   | gauge     | Size of the active session's `channel.jsonl`           |

Counters increment monotonically and reset only when the daemon is stopped and storage is rebuilt. Gauges reflect the current state at scrape time.

## SLO and SLI guidance

Default targets for a single-host deployment. Operators may relax these for development environments.

- Local event append p95 < 100 ms (`atn-control_event_append_latency_ms`).
- Stream delivery p95 < 1 s after append (latest event cursor is observed by all live subscribers within 1 s).
- Stream reconnect replays missed events without silent skip (verified by integration tests; cursor gap fails closed).
- Projection rebuild from 10,000 events completes within a documented local benchmark target (recorded by `docs/spec/testing-and-tooling.md` load tests).
- `atn-control_daemon_ready` is 1 for ≥ 99% of operator-active hours.
- `atn-control_runner_failures_total` rate over 1 hour is bounded by session budgets and is reported in `atn-control status --verbose`.

These are operator-facing targets, not protocol invariants. Failing an SLO is an alert signal, not a daemon fault.

## Alerting guidance

Recommended local alerts. They map to operator action, not to daemon recovery (the daemon is fail-closed by design).

- `daemon_ready == 0` for more than 60 s — daemon down or registry unsafe.
- `blocked_session_age_seconds` exceeds a configured threshold (e.g. 24 h) — session has been blocked too long.
- `waiting_user_age_seconds` > `escalation_response_timeout_sec * 0.8` — close to escalation timeout; the moderator should chase the user.
- `stream_subscriber_stale_total` increases — a member runtime stopped acknowledging cursors.
- `runner_missing_cost_total` increases — token and USD totals are becoming incomplete.
- `security_violations_total` increases — investigate immediately; do not suppress.
- `replay_failures_total` increases — possible storage corruption; run `atn-control storage verify` and follow `17-operations.md`.

## Dashboard guidance

A useful local dashboard groups widgets by concern:

- **Daemon health** — `daemon_ready`, `active_sessions`, daemon uptime, last `atn-control doctor` result.
- **Active session** — current `phase` and `status`, session ID, blocked-session age, waiting-user age, pending escalation batches.
- **Throughput** — `event_appends_total` over time (rate), `event_append_latency_ms` p50/p95/p99.
- **Runner accounting** — `runner_invocations_total`, `runner_failures_total`, `runner_missing_cost_total`, current `runner_calls_total`, current token/USD totals.
- **Stream** — `stream_subscriber_count`, `stream_lag_events`, `stream_subscriber_stale_total`.
- **Storage** — `jsonl_bytes`, last replay duration, `replay_failures_total`.
- **Security** — `security_violations_total` by category.

## Structured logs

The daemon uses three log streams:

1. `channel.jsonl` per session — the durable event log. SOT for sessions.
2. `<data_home>/operational.log` — JSON Lines. Pre-session events, daemon lifecycle, subprocess audit. SOT for pre-session failures only (`12-operations.md`).
3. Stdout/stderr — process-level supervision messages. Not durable. Should not be parsed for compliance.

Every operational log line includes `ts`, `level`, `event`, `category`, and a redacted `payload`. The redaction pipeline runs before write, so secret values never reach the operational log.

## Tracing and correlation

Causal correlation uses the existing protocol fields:

- `command_id` ties a CLI command to its produced events.
- `causation_event_id` ties a daemon-emitted event to the event that caused it.
- `correlation_id` ties an event back to the originating session or logical thread.
- `runner.invocation_id` ties runner accounting events together.

Operators should be able to assemble a per-session trace by filtering `channel.jsonl` and operational log lines by `session_id` or `correlation_id`. Distributed tracing (OpenTelemetry spans) is optional; if implemented, span IDs must not replace these protocol fields.

## Operational commands

Observability surfaces the following commands without weakening security:

- `atn-control daemon status` — daemon liveness and version.
- `atn-control doctor` — read-only health summary; `--repair-permissions` for explicit fixes.
- `atn-control status` and `atn-control status <session_id> --verbose` — session-level summary.
- `atn-control limits show <session_id>` — budget/escalation accounting view.
- `atn-control storage verify` — JSONL parse, schema, and projection consistency check.

These commands are part of `protocol-and-cli.md`; this document only summarizes their observability role.

## Non-goals

- Observability must not require the daemon to deliver external notifications. Alert routing is an operator or gateway concern.
- The daemon must not become an alert manager, paging system, or notification gateway.
- Metrics must not contain secret values. The same redaction rules that protect `channel.jsonl` apply to any structured log or metric label.
- Observability must not introduce new event types that bypass the normal protocol. New observability data must be derived from existing events or operational log entries.

---

## Merged from `docs/spec/operations.md`

# Disaster Recovery

## Scope

This document defines how an operator backs up, restores, and recovers `atn-control` state. It covers `channel.jsonl` corruption, SQLite projection corruption, registry snapshot loss, active-session lock mismatch, and unsafe data home permissions. The goal is to bring the daemon back to a verifiable state without inventing events, replacing real member runtimes, or weakening the security model.

The normative SOT documents that disaster recovery depends on:

- Source-of-truth boundary: `channel.jsonl` is durable; SQLite is a projection (`architecture.md`, `operations.md` §2).
- Replay determinism: replay is side-effect free (`operations.md` §2).
- Security model: `12-operations.md`.
- CLI commands: `protocol-and-cli.md`.

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
| `run/atn-controld.sock`              |              No | Process socket; recreated by daemon start       |

Backups should preserve file mode and ownership where possible. After restore, ownership and modes must satisfy the rules in `12-operations.md`; `atn-control doctor` (with `--repair-permissions`) or `atn-control init` may correct them with explicit reporting.

## What can be rebuilt

- `network.sqlite` and every projection table — rebuilt by `atn-control storage rebuild-projection`.
- `transcript.md` and `brief.md` — regenerated from `channel.jsonl` (`architecture.md#retention`).
- `event_recipients`, `runner_invocations`, `escalation_batches`, `escalation_batch_items`, `commands_seen`, `artifacts` projections — rebuilt by replay.
- `stream_cursor` files — rebuilt from `stream_cursor_acknowledged` events.

## Backup procedure

1. Stop the daemon (`atn-control daemon stop`) before copying files. This avoids racing with appends.
2. Copy `<data_home>` recursively, preserving permissions and ownership.
3. Verify the copy by running `atn-control storage verify --data-home <backup_path>` against the copy (when supported by the implementation) or by inspecting `channel.jsonl` parseability.
4. Encrypt the backup if the destination is not on the same trust boundary as `<data_home>`. Backups inherit the same secret hygiene rules: artifacts may carry redacted user content, and `registry_snapshot.yaml` carries member identity.
5. Restart the daemon (`atn-control daemon start`).

A live backup (without stopping the daemon) is acceptable only when the implementation provides a snapshot mechanism that guarantees a consistent `channel.jsonl` view; otherwise prefer a brief stop-and-copy.

## Restore procedure

1. Stop the daemon if it is running.
2. Place the backup at the desired `<data_home>` path.
3. Verify file ownership and permissions match the rules in `12-operations.md` (data home `0700` recommended, registry `0600` recommended, owner is the daemon user). Use `atn-control doctor` to inspect; use `atn-control doctor --repair-permissions` or `atn-control init` to fix with explicit reporting.
4. Run `atn-control storage verify`.
5. Run `atn-control storage rebuild-projection` if any projection is missing or fails verification.
6. Start the daemon.
7. Run `atn-control doctor` and `atn-control status` to confirm the active session and phase match expectations.

## Projection rebuild

Projection rebuild is the most common recovery operation. It is replay applied to projection state.

```text
1. Stop the daemon.
2. Run `atn-control storage verify`.
3. If verification reports only a missing, corrupt, or mismatched projection, run `atn-control storage rebuild-projection`; rebuild performs its own event-log preflight before replacement.
4. Start the daemon.
5. Run `atn-control doctor`.
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

Symptoms: `atn-control storage verify` reports parse error, duplicate `event_id`, schema gap, or `migration_required`.

Procedure:

1. Stop the daemon.
2. Copy the corrupted `channel.jsonl` to a quarantine path before any repair attempt; do not overwrite it.
3. Inspect the failing line(s) and the surrounding context. Determine whether corruption is at the tail (truncated final line) or in the middle.
4. Tail truncation: if the final line is partial JSON, truncate the file at the last newline-terminated valid line. Document the action in `operational.log` manually (operator action, not a session event).
5. Mid-file corruption: do not silently delete events. Restore from the most recent verified backup, then replay any events that arrived after the backup point only if they exist as a separate, verifiable record (rare). If no verifiable record exists, mark the affected session as unrecoverable through `atn-control cancel <session_id>` after restoring from backup.
6. Run `atn-control storage verify` and `atn-control storage rebuild-projection`.
7. Start the daemon and verify session state.

The daemon must never auto-skip events to keep replay alive.

## SQLite corruption

Symptoms: `atn-control status` returns inconsistent values, projection queries fail, or `atn-control storage verify` reports projection mismatch.

Procedure:

1. Stop the daemon.
2. Move the corrupted `network.sqlite` aside (do not delete until rebuild succeeds).
3. Run `atn-control storage rebuild-projection` to rebuild from `channel.jsonl`.
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
6. Run `atn-control status` to confirm the lock matches the recorded state.

## registry_snapshot.yaml missing or corrupt

Symptoms: dispatch fails with a session-scoped registry violation; `atn-control storage verify` reports replay failure for a missing, unsafe, empty, or corrupt session snapshot.

Procedure:

1. Stop the daemon.
2. Restore `sessions/<session_id>/registry_snapshot.yaml` from the most recent backup.
3. If no backup exists, the session cannot be rebuilt as valid local release evidence. Replay must not regenerate the snapshot from the live `registry.yaml` (per `12-operations.md` and `operations.md` §2).
4. Run `atn-control storage verify` and start the daemon.
5. Cancel the session through `atn-control cancel <session_id>` if the snapshot cannot be restored.

## Unsafe data home or registry permissions

Symptoms: daemon refuses to start with a category from `12-operations.md` (e.g. `registry_data_home_unsafe`, `registry_permissions_unsafe`, `registry_owner_unsafe`).

Procedure:

1. Stop the daemon (if running).
2. Run `atn-control doctor` to enumerate the unsafe paths and recommended fixes.
3. Run `atn-control doctor --repair-permissions` or `atn-control init` to apply fixes; both must report every change.
4. Re-run `atn-control doctor` to confirm.
5. Start the daemon.

Daemon start must not silently chmod or chown anything.

## Restore to a new data_home

Operators sometimes need to move `<data_home>` to a new path or host.

Procedure:

1. Stop the daemon.
2. Copy `<data_home>` to the new path with permissions preserved.
3. Set `$ATN_HOME` to the new path or leave the resolution order alone if the new path follows `$XDG_DATA_HOME/agent-turn-network` or `~/.atn/`.
4. Run `atn-control doctor` against the new path.
5. Run `atn-control storage verify` and `atn-control storage rebuild-projection` if needed.
6. Start the daemon.

Per-session `registry_snapshot.yaml` is portable; the live `registry.yaml` may need adjustment if member workspace paths changed.

## Verification checklist

After any recovery operation, verify:

- [ ] `atn-control doctor` reports no unsafe paths.
- [ ] `atn-control daemon status` reports ready.
- [ ] `atn-control storage verify` passes.
- [ ] `atn-control status` matches the expected active session and phase.
- [ ] `atn-control limits show <session_id>` returns sane runner accounting and escalation counters.
- [ ] No `security_violation` events were created during recovery.
- [ ] `operational.log` records the recovery action (manual entry by the operator if no automated event exists).

## Non-goals

- Disaster recovery must not reinterpret historical sessions using the live registry.
- Disaster recovery must not invent missing events or synthesize replacements for corrupted lines.
- Disaster recovery must not rerun member wrappers or deliver escalations during replay.
- Disaster recovery must not overwrite `channel.jsonl` to "clean it up". The original corrupted file is preserved for forensic review.
- The daemon must not become a backup orchestrator; backup scheduling is an operator concern.

## Appendix: Schema migration example

Schema migrations are part of replay (`operations.md` §2). They are pure functions that transform a single event envelope. They are relevant to disaster recovery because a backup may contain events at an older `schema_version` and a new daemon must run the migration chain before projection rebuild succeeds.

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
