# Protocol And Cli

---

## Merged from `docs/spec/protocol-and-cli.md`

# Protocol Specification

## Source of truth

`channel.jsonl` is the durable source of truth. SQLite is a projection for fast queries.

Each line is one event object.

## Common event envelope

```json
{
  "schema_version": 1,
  "event_id": "evt_01HV...",
  "command_id": "cmd_01HV...",
  "causation_event_id": "evt_01HV...",
  "correlation_id": "corr_01HV...",
  "session_id": "sess_...",
  "session_type": "delegation",
  "turn": 12,
  "phase": "working",
  "type": "assignee_update",
  "from": "agent-1",
  "to": ["agent-mod"],
  "created_at": "2026-04-25T00:00:00Z",
  "runner": {
    "invocation_id": "run_01HV...",
    "adapter_kind": "hermes-agent",
    "member": "agent-1",
    "attempt": 1,
    "is_retry": false,
    "source_command_id": "cmd_01HV...",
    "status": "succeeded",
    "duration_sec": 12.34
  },
  "cost": {
    "tokens_in": 1234,
    "tokens_out": 567,
    "usd_estimate": 0.0321,
    "source": "hermes-agent-stderr-parse"
  },
  "payload": {}
}
```

Field rules (full contract in `operations.md`):

- `schema_version`: integer; bumped on any breaking envelope change. Readers refuse unknown versions unless a migration is registered.
- `event_id`: unique per persisted event (ULID recommended).
- `command_id`: set by the originator. CLI-originated events require it and deduplicate on retry; daemon-generated events that follow a CLI command reuse the originating id; pure daemon-originated events (for example `session_budget_exceeded`) may leave it null. Full rules in `operations.md`.
- `causation_event_id`: the event that directly caused this one. Required whenever `command_id` is null. Empty only for session-initial events.
- `correlation_id`: logical thread across related commands and events; defaults to `session_id`.
- `cost`: optional. Present on terminal runner events; the value may be an object or `null` (`null` means measured cost was unavailable, **not** zero cost). Omitted entirely on non-runner events. See `operations.md` §3.
- `runner`: optional object identifying the bounded adapter invocation attempt associated with the event. Present on runner accounting events (`runner_invocation_started`, `runner_invocation_failed`, `runner_result_discarded`) and on terminal semantic events produced by a runner invocation. Fields: `invocation_id` (unique per actual adapter invocation attempt), `adapter_kind`, `member`, `attempt` (1 for first, 2+ for retries), `is_retry`, `source_command_id`, `status`, `duration_sec` (null until the terminal runner result is known). Allowed `runner.status` values: `started`, `succeeded`, `failed`, `timeout`, `semantic_error`, `discarded_after_cancel`, `cancelled`, `interrupted`. If `runner` is present on a terminal runner event, `cost` must also be present (object or `null`). If `runner` is absent, `cost` must be omitted.
- `phase`: the exact state-machine phase of the session **after this event is applied** (post-transition value). It is part of the durable event source of truth and is used by replay to rebuild session state. Business logic and state transitions use `phase`, not `status`. Allowed values:
  - delegation: `created`, `assigned`, `acknowledged`, `working`, `needs_clarification`, `waiting_user`, `submitted`, `under_review`, `revision_requested`, `blocked`, `accepted`, `cancelled` (per `architecture.md`).
  - council: `created`, `preparation`, `discussion`, `draft_conclusion`, `consensus_vote`, `blocked`, `finalized`, `unresolved`, `cancelled` (per `architecture.md`).
  Unknown phase values fail closed at append time.

The event envelope deliberately does **not** include `status`. `status` is a derived roll-up projection (allowed values `open`/`blocked`/`terminal`) defined in `architecture.md` and `operations.md` §5; it is stored only in `session.yaml` and the SQLite `sessions.status` column for query, UI, and active-session lock checks. The durable lifecycle source is `phase`.
- `session_type`: `delegation` or `council`. Other values fail closed.
- `turn`: integer turn counter; required for council events that occur inside a turn (`hand_raise_requested`, `hand_raise`, `speaker_selected`, `speech`, `moderator_intervention`), null otherwise.
- `from`: required string. The single principal that originated the event. Allowed values are registry member ids plus the reserved principals `user` and `atn-controld`. `from` is never an array; if multiple actors are relevant (e.g. an `escalation_batched` that summarizes multiple source members), the originator stays a single string and the related actors are listed in `payload`.
- `to`: required `array<string>`. The semantic recipients of the event. A single recipient is represented as a one-element array (e.g. `["agent-mod"]`, never `"agent-mod"`). Broadcast is represented by an explicit recipient list (e.g. `["agent-1", "agent-2", "agent-3"]`); special values such as `"all"`, `["all"]`, or `"*"` are forbidden. `to: []` is allowed only for unaddressed session audit events. Recipients must be unique within a single event; duplicates are normalized away or rejected before append. Recipient order has no semantic meaning; the daemon stores recipients in canonical order for deterministic projection and transcript rendering. Allowed recipient values are registry member ids and the reserved principal `user`; `atn-controld` is not a normal recipient.

`to` is **semantic addressing**, not stream access control. A valid session participant may observe events not addressed to it; the runtime decides whether to act by inspecting `type`, `from`, `to`, role, phase, and policy. Stream read permissions are governed by `12-operations.md`.

PRSLR-004 local-control participant runtime evidence is exported on council `speech.payload.persistent_participant_runtime_evidence` and council status as `persistent_participant_runtime_evidence`. The object is derived from the durable event log and includes `status`, `session_id`, `speech_event_id`, `coverage_cursor`, `coverage_scope`, `speaker`, `required_members`, `session_registry`, and per-member rows. Speaker rows use `coverage_kind: "self_ack"`; non-speaker rows use `coverage_kind: "observe_delta"`. Every row carries `member`, `participant_session_handle`, `generation`, `last_cursor`, `observed_event_id`, `speech_event_id`, and `source: "control_local_event_log"`. Handles are scoped to `session_id + member`; cross-council handle reuse is invalid. This is local control evidence only and does not authorize plugin lifecycle/cursor ownership, live Discord delivery, or PRSLR-005 delta prompt/rehydrate behavior.

This initial Release v1 schema uses `to` as `array<string>` from the start. Changing `to` between string and array form is a breaking envelope change and requires a `schema_version` migration per `operations.md`.

## Daemon compatibility read commands

The daemon exposes explicit read-only compatibility commands for plugin live-transport negotiation:

- `version.read`: returns `schema_version`, `protocol_version`, `daemon_version`, `min_plugin_protocol_version`, `features`, `feature_groups`, and `fixture_manifest`.
- `status.read`: returns the version fields above plus daemon readiness, socket/data-home identity, `operational_readiness`, and per-feature `capability_state`. It is additive to, and does not change, the operator-facing `status` command.
- `diagnostics.read`: returns the version fields above plus health `categories`, readiness, and per-feature `capability_state`. It is additive to, and does not change, the operator-facing `health` command.

Unknown command names and unsupported features fail closed with the structured `unsupported_feature` error; readers must not guess capabilities from missing fields or fall back to another transport.

## Event origin classes

Each event type declares one of the following origin classes:

- `participant_cli`: emitted through a public CLI write command by the moderator runtime, a member runtime, or another authorized participant.
- `daemon_after_cli`: emitted by the daemon as a deterministic result of a CLI command.
- `daemon_internal`: emitted by daemon policy, timeout, retry, stream, security, budget, or projection handling without an originating CLI command.
- `mixed`: may be emitted either by a participant CLI command or by daemon policy. The allowed origins must be documented per event.

Coverage rules (cross-document invariants):

- Events with origin class `participant_cli` must have an explicit command path in `protocol-and-cli.md`. A single event type may have more than one command path only when the event-to-command matrix in `protocol-and-cli.md` lists the relationship.
- Events with origin class `daemon_after_cli` must declare the CLI command(s) that cause them.
- Events with origin class `daemon_internal` do not require public write commands.
- Origin class is consistent with the `command_id` and `causation_event_id` rules in `operations.md` §2.

## Participant roles

The envelope `from` and `to` fields carry principal identifiers. Most principals are registry member ids. The reserved principals are:

- `user`
- `atn-controld`

`from` is always a single string. `to` is always an array of strings.

Role information (`moderator`, `assignee`, `reviewer`, `participant`, `observer`) is resolved out-of-band via the registry and projected into `session_participants.role`. The protocol does not derive permissions from role strings; semantic intent comes from the event `type`, `from`, `to`, session phase, and policy. Reserved principal collision in the registry (`members.user`, `members.atn-controld`) is rejected at registry load time per `12-operations.md`.

## Stream delivery contract

The daemon exposes events to the ATN protocol client/contract and canonical CLI stream as an ordered cursor sequence. The cursor is a daemon-issued opaque value, normally derived from the append offset plus `event_id`. It is not interpreted by member runtimes.

Rules:

- `channel.jsonl` append is the source of truth; a stream event must not be visible before the append succeeds.
- Stream consumers, whether Hermes plugin tools or the canonical CLI, emit replayed events from `--since` before live events.
- Every stream frame includes `cursor`, the full event envelope, and an `is_replay` boolean.
- Member runtimes acknowledge processed cursors through the ATN protocol client/contract; `atn-control stream ack` is the canonical CLI fallback.
- If the daemon cannot satisfy a cursor because of retention, corruption, or unknown schema, it emits a stream error and refuses silent skip.
- Direct daemon connections are implementation details. Agent-facing readers use Hermes plugin stream tools when available and the canonical CLI stream for diagnostics, recovery, tests, and manual fallback.

Example stream frame:

```json
{
  "cursor": "cur_000000000012_evt_01HV...",
  "is_replay": false,
  "event": {
    "schema_version": 1,
    "event_id": "evt_01HV...",
    "session_id": "sess_...",
    "type": "hand_raise_requested",
    "from": "agent-mod",
    "to": ["agent-1", "agent-2"],
    "payload": {"turn": 5, "research_timeout_sec": 600}
  }
}
```

## Common session events

### session_created

Origin class: `daemon_after_cli`.
Created by: `atn-control delegate new` or `atn-control council new`.

```json
{
  "type": "session_created",
  "from": "atn-controld",
  "to": ["agent-mod", "agent-1"],
  "payload": {
    "session_type": "delegation",
    "title": "Implement task A",
    "moderator": "agent-mod",
    "participants": ["agent-mod", "agent-1"],
    "limits": {}
  }
}
```

Council variant has `to` listing every council member:

```json
{
  "type": "session_created",
  "from": "atn-controld",
  "to": ["agent-mod", "agent-1", "agent-2", "agent-3"],
  "payload": {
    "session_type": "council",
    "title": "Discuss topic A",
    "moderator": "agent-mod",
    "participants": ["agent-mod", "agent-1", "agent-2", "agent-3"],
    "surface": {
      "kind": "discord_thread",
      "platform": "discord",
      "guild_id": "optional",
      "channel_id": "optional_parent_channel_id",
      "thread_id": "1507515847227215932",
      "origin_message_id": "optional",
      "started_by": "user",
      "delivery_owner": "moderator_runtime"
    },
    "request_context": {
      "source": "discord_thread",
      "requested_output_mode": "live_visible_thread",
      "visible_output_required": true
    },
    "linked_authority": {
      "kanban_card_id": "t_xxxxx",
      "vault_decision_note": "optional"
    },
    "turn_mode": "relevance",
    "limits": {
      "max_discussion_turns": 15,
      "dispatch_timeout_sec": 150
    }
  }
}
```

`surface` and `linked_authority` are optional. Council `session_created.payload.request_context` preserves the accepted `council.new` output intent; every council creation must declare `requested_output_mode` before session creation. Discord-origin councils default to `live_visible_thread`, while non-visible/local-daemon-only modes require explicit override evidence. When `surface.kind` is `discord_thread`, `thread_id` is required and Discord identifiers are evidence pointers, not ordering or state authority. `linked_authority.kanban_card_id` means the final council result must be returned to the named Kanban card or a clearly linked follow-up/review card. `linked_authority.vault_decision_note` is a decision-record target, not proof that the note already exists. Optional `turn_mode` is the session-level intended/default floor policy (`relevance`, `targeted`, `random`, `moderator_direct`, or `role_order`); the standard live-visible selected-speaker path defaults to `relevance` when omitted, not an unsupported `selected_runner` turn mode. It is not per-turn audit evidence; each actual floor grant records its own `speaker_selected.payload.selection_mode`. `limits.max_discussion_turns` defaults to `15` for standard live-visible decision councils when omitted so lifecycle accounting can require participant closeouts and a terminal synthesis; callers may set another positive value for a bounded council. Legacy `limits.max_turns` is not reinterpreted as lifecycle enforcement. Discord live-visible selected-runner councils default `limits.dispatch_timeout_sec` to `150` when omitted; any non-150 value requires explicit approved-alternative evidence in `request_context.selected_runner_timeout_override`.

### Standard live-visible decision council default

For Discord-origin decision-bearing councils, the default runtime policy is `standard_live_visible_decision_council`: create a live-visible Discord-thread council, bind the exact requested `channel_id/thread_id`, use bounded `max_discussion_turns`, apply a 150-second selected-runner dispatch timeout unless an approved alternative is provided, run participant readiness and grant-time freshness checks, and use `poll -> multiple hand_raise candidates where available -> relevance grant with reason -> selected_runner -> linked speech` for each discussion turn. After the discussion window, one selected closeout speech per participant is required before `council.propose`; decision-bearing councils then proceed through `council.request_vote`, all required `council.vote` events, visible moderator final synthesis, and `council.finalize` with matching posted `surface_evidence`. Exploratory councils may explicitly opt out of the proposal/vote phase, but live-visible delivery proof, selected-runner accounting, participant closeout accounting, and terminal visible closeout reporting remain separate evidence axes and are never inferred from transcript/export-only or manual/fallback speech.

When `limits.max_discussion_turns` is configured, the derived council discussion lifecycle is:

- T0 moderator opening from the first moderator `hand_raise_requested` discussion-opening event.
- T1..Tmax selected participant discussion speeches with `payload.turn <= max_discussion_turns`.
- One selected participant closeout speech per council member with `payload.turn > max_discussion_turns`.
- T(n+p+1) moderator terminal synthesis/final closeout from terminal `council_finalized` or `council_unresolved`, where `n = limits.max_discussion_turns` and `p = participant_count`.

The expected visible turn total is `max_discussion_turns + participant_count + 2`. The terminal synthesis turn is `max_discussion_turns + participant_count + 1`, and its expected visible index is the total. `council.propose` must fail closed until T0, the participant discussion window, and one closeout speech per member are present. `council.finalize` must fail closed for standard live-visible `discord_thread` councils when terminal visible closeout proof is missing, malformed, pending, failed, unproven, missing a concrete final-message/equivalent pointer, missing the configured thread binding, or bound to the wrong thread. `council.unresolved` remains available as the honest fail-closed terminal path with diagnostics. Lifecycle accounting does not repair selected-runner accounting or visible delivery proof.

### Surface rendering evidence contract

Visible rooms such as Discord threads are projections of durable ATN events. They help humans follow a council, but they are not lifecycle state authority. A renderer, transcript command, export command, or plugin-visible helper must build its view from cursor-ordered `channel.jsonl` events and may attach external message ids only as evidence pointers.

Minimum event inputs for the visible surface contract:

| Visible surface need | Durable input | Required fields / rules |
| --- | --- | --- |
| Surface identity | `session_created.payload.surface` | `kind`; for `discord_thread`, `thread_id` is required. `platform`, `guild_id`, `channel_id`, `origin_message_id`, `started_by`, and `delivery_owner` are evidence/configuration pointers only. |
| Linked return target | `session_created.payload.linked_authority` | `kanban_card_id` and/or `vault_decision_note` select where the final result must be returned; they do not prove the return has happened. |
| Floor grant | `speaker_selected` | `turn`, selected member in `to`, and `payload.selection_mode`; renderers must not infer the active speaker from Discord message order. |
| Participant speech | `speech` | `from`, `to`, `payload.turn`, `payload.speech`, optional `payload.evidence`, optional `payload.responds_to_event_id`; only successful append makes the speech renderable. |
| Moderator surface note | `moderator_intervention` and other typed council events | Renderable only as typed events; free-form Discord replies are evidence/presentation, not implicit state. |
| Draft closeout proposal | `draft_conclusion` | Renderable as a moderator draft/proposal only. It is not terminal, not a final result, and not proof that a human-readable closeout was delivered. |
| Vote closeout state | `consensus_vote_requested` and `consensus_vote` | Renderable as voting state over a specific `draft_version`. Votes do not prove final closeout until a terminal outcome event is appended and projected. |
| Final visible result | `council_finalized` | `payload.final_summary`, `payload.consensus`, required posted `payload.surface_evidence` for standard live-visible `discord_thread` finalization, and required `payload.linked_authority_result` when linked authority was configured. |
| Unresolved visible result | `council_unresolved` | Records the durable unresolved outcome plus diagnostics/follow-up evidence when closeout proof is incomplete; a visible unresolved notice must point back to this event rather than inventing a final decision. |

Control status/export expose the derived `discussion_lifecycle` object. Export bundle `summary_turn_accounting` rows keep existing turn/member/event id fields and add `lifecycle_stage`, `visible_turn_index`, and `visible_turn_total` when a speech contributes lifecycle evidence. The lifecycle object also exposes additive fail-closed status axes: required/present/complete counts for discussion turns and participant closeouts, `moderator_opening_present`, `moderator_synthesis_present`, `terminal_phase`, `terminal_visible_closeout_proof_status`, and `completion_verdict`. A finalized verdict requires a posted terminal visible closeout proof; missing, failed, pending, unproven, malformed, or mismatched proof must not be reported as finalized completion.

Cursor order is authoritative for rendering order. `created_at`, external surface timestamps, Discord message ids, and Discord message order are display/evidence data only. A renderer may surface `surface_evidence` or delivery-message ids after the corresponding durable event exists, but it must not create, reorder, or mark lifecycle progress from the external surface alone.

Delivery evidence status belongs to durable event payloads and projections:

- `posted`: concrete immutable evidence exists, such as a Discord message id, Kanban comment id, Vault note path, or equivalent pointer recorded in the applicable event payload/projection.
- `failed`: an attempted visible or linked-authority return failed; the failure reason and follow-up handling evidence are required.
- `pending_followup`: finalization/unresolve may be recorded, but the visible/linked return remains incomplete and must have a follow-up card, pending-review handoff, or equivalent pointer.
- missing status or missing evidence: unproven, not successful delivery.

Terminal outcome acceptance is split in two. A terminal council event such as `council_finalized`, `council_unresolved`, or `session_cancelled` proves the durable daemon outcome when it is validly appended. It does **not** prove that a moderator produced a human-readable visible closeout in Discord, Kanban, Vault, transcript, export, or a plugin helper. Visible closeout success requires posted delivery/projection evidence that points back to the terminal event; missing, mismatched, failed, or pending evidence fails closed and must be reported as visible closeout incomplete.

The daemon, replay, transcript, export, and projection rebuild must stay side-effect free for external surfaces: they may expose status/evidence fields, but they must not call Discord APIs, create Kanban comments, write Vault notes, or transform configured targets into `posted` evidence.

### session_cancelled

Origin class: `participant_cli`.
Canonical command: `atn-control cancel`.

```json
{
  "type": "session_cancelled",
  "from": "agent-mod",
  "to": ["agent-mod", "agent-1"],
  "payload": {"reason": "user cancelled"}
}
```

### session_blocked

Origin class: `participant_cli`.

Canonical command: `atn-control block`.

Compatibility command path: `atn-control delegate block` for delegation sessions.

`session_blocked` is the common manual block event used by both delegation and council sessions. It is distinct from daemon-originated operational events (`session_budget_exceeded`, `escalation_timeout`, session-scoped `security_violation`) which may also move a session into a blocked state per `operations.md`. Manual `session_blocked` transitions the session to `blocked`; the envelope `phase` is `blocked` and the payload records both `prior_phase` and `resume_phase`, even when they are the same value.

```json
{
  "type": "session_blocked",
  "from": "agent-mod",
  "to": ["agent-mod", "agent-1"],
  "phase": "blocked",
  "payload": {
    "category": "external_dependency",
    "reason": "External dependency is unavailable.",
    "prior_phase": "working",
    "resume_phase": "working"
  }
}
```

Allowed `category` values:

- `external_dependency`
- `user_decision_required`
- `scope_conflict`
- `policy_or_security`
- `budget_or_limit`
- `other`

`budget_or_limit` is allowed for manual moderator blocks but is unusual; daemon-originated budget breaches use `session_budget_exceeded` instead, and recovery for those breaches requires `limits_extended`, not `session_resumed`.

### session_resumed

Origin class: `participant_cli`.

Canonical command: `atn-control resume`.

`session_resumed` lifts a recoverable manual, external-dependency, policy, or council block and returns the session to the `resume_phase` recorded by the blocking event. It must not be used to lift budget or limit blocks that require `limits_extended`.

The envelope `phase` is the post-transition phase, normally the blocking event's `payload.resume_phase`.

```json
{
  "type": "session_resumed",
  "from": "agent-mod",
  "to": ["agent-mod", "agent-1"],
  "phase": "working",
  "causation_event_id": "evt_session_blocked_01",
  "payload": {
    "blocked_event_id": "evt_session_blocked_01",
    "reason": "External dependency is now available.",
    "resume_phase": "working",
    "resolved_by": "agent-mod"
  }
}
```

Rules:

- `blocked_event_id` must reference the event that moved the session to `blocked`.
- The referenced blocking event must have a compatible category for manual resume.
- Budget and limit blocks are lifted by `limits_extended`, not by `session_resumed`.
- Security blocks may be resumed only if the security model explicitly allows the category to be remediated and the remediation has been verified.
- The daemon clears `blocked_by_event_id`, `prior_phase`, and `resume_phase` projection fields after applying this event.

## Delegation events

### task_assigned

Origin class: `daemon_after_cli`.
Created by: `atn-control delegate new`.

```json
{
  "type": "task_assigned",
  "from": "agent-mod",
  "to": ["agent-1"],
  "payload": {
    "task": "...",
    "context": "...",
    "acceptance_criteria": ["..."],
    "expected_outputs": ["..."]
  }
}
```

### assignee_acknowledged

Origin class: `participant_cli`.
Canonical command: `atn-control delegate ack`.

```json
{
  "type": "assignee_acknowledged",
  "from": "agent-1",
  "to": ["agent-mod"],
  "payload": {
    "understanding": "...",
    "plan": ["..."],
    "questions": []
  }
}
```

### clarification_requested

Origin class: `participant_cli`.
Canonical command: `atn-control delegate clarify`.

```json
{
  "type": "clarification_requested",
  "from": "agent-1",
  "to": ["agent-mod"],
  "payload": {
    "question": "Requirements A and B conflict. Which should be prioritized?",
    "options": ["A", "B", "split scope"]
  }
}
```

### clarification_answered

Origin class: `participant_cli`.
Canonical command: `atn-control delegate answer-clarification`.

`clarification_answered` must reference the clarification request it answers through `causation_event_id`.

```json
{
  "type": "clarification_answered",
  "from": "agent-mod",
  "to": ["agent-1"],
  "payload": {
    "answer": "Prioritize A and split B into follow-up scope.",
    "source": "agent-mod"
  }
}
```

### delegation_message

Origin class: `participant_cli`.
Canonical command: `atn-control delegate message`.

General delegation message that is **not** a direct answer to a specific `clarification_requested` event. Use `clarification_answered` for direct clarification answers.

```json
{
  "type": "delegation_message",
  "from": "agent-mod",
  "to": ["agent-1"],
  "payload": {
    "kind": "scope_correction",
    "message": "Keep B out of this task. Treat it as follow-up scope."
  }
}
```

Allowed `kind` values:

- `note`
- `instruction`
- `scope_correction`
- `priority_update`
- `process_guidance`

### assignee_update_requested

Origin class: `participant_cli`.
Canonical command: `atn-control delegate request-update`.

```json
{
  "type": "assignee_update_requested",
  "from": "agent-mod",
  "to": ["agent-1"],
  "payload": {
    "reason": "No progress update has been recorded recently.",
    "requested_detail": ["progress", "blockers", "next_step"]
  }
}
```

### user_escalation_requested

Origin class: `mixed`.

Allowed origins:

- `participant_cli`: emitted by `atn-control delegate escalate` when the escalation is immediate and not batched.
- `daemon_after_cli`: emitted by the daemon as the deterministic result of `atn-control delegate escalation-flush`.
- `daemon_internal`: emitted by daemon runtime policy when a pending batch is flushed by timer expiry, higher-urgency pressure, startup reconciliation, or phase-change pressure.

Command paths:

- `atn-control delegate escalate`
- `atn-control delegate escalation-flush`

Daemon-internal flushes do not require a public write command but must include `causation_event_id` pointing to the pending batch or policy-triggering event.

When `user_escalation_requested` is emitted by a daemon-internal or daemon-after-CLI batch flush, `from` may be `atn-controld`. When it is emitted by an immediate moderator escalation, `from` is the authorized participant principal, normally `agent-mod`.

```json
{
  "type": "user_escalation_requested",
  "from": "agent-mod",
  "to": ["user"],
  "phase": "waiting_user",
  "payload": {
    "batch": false,
    "batch_id": null,
    "included_event_ids": ["evt_clarification_01"],
    "source_member": "agent-1",
    "question": "Requirements A and B conflict. Which should be prioritized?",
    "context": "agent-1 found a scope conflict while working on the delegated task.",
    "options": ["A", "B", "split scope"],
    "recommendation": "Prioritize A and split B into follow-up scope.",
    "urgency": "blocked",
    "delivery_policy": "origin_or_telegram",
    "prior_phase": "needs_clarification",
    "resume_phase": "working",
    "response_timeout_sec": 86400
  }
}
```

Batch example (multiple low-urgency questions flushed into one user-facing escalation):

```json
{
  "type": "user_escalation_requested",
  "from": "atn-controld",
  "to": ["user"],
  "phase": "waiting_user",
  "payload": {
    "batch": true,
    "batch_id": "escbatch_01HV...",
    "included_event_ids": ["evt_q1", "evt_q2"],
    "questions": [
      {
        "source_event_id": "evt_q1",
        "source_member": "agent-1",
        "question": "Should option A be preferred?",
        "context": "Affects the delegated task's next step.",
        "options": ["yes", "no"],
        "recommendation": "yes"
      },
      {
        "source_event_id": "evt_q2",
        "source_member": "agent-2",
        "question": "Should the review accept risk R?",
        "context": "Affects review acceptance.",
        "options": ["accept", "reject"],
        "recommendation": "reject"
      }
    ],
    "urgency": "normal",
    "delivery_policy": "origin_or_telegram",
    "prior_phase": "working",
    "resume_phase": "working",
    "response_timeout_sec": 86400
  }
}
```

Rules:

- `user_escalation_requested` enters `waiting_user`. The envelope `phase` is `waiting_user`, and the payload records `prior_phase`/`resume_phase` so recovery can return the session after `user_escalation_resolved`.
- For batched escalations, `batch` is `true`, `batch_id` references the originating `escalation_batched` chain, and `included_event_ids` lists the source events that were batched.
- `user_escalation_requested` increments `user_escalations_total`. `escalation_batched` does not.
- `escalation_response_timeout_sec` starts from this event, not from `escalation_batched`.

### user_escalation_delivered

Origin class: `participant_cli`.
Canonical command: `atn-control delegate escalation-delivered`.

`causation_event_id` points to the originating `user_escalation_requested` event.

Emitted by the moderator runtime (not by the daemon) after the Hermes plugin/gateway helper or equivalent Hermes gateway skill has actually delivered the escalation to the user. The daemon records this event through the normal typed ATN command path, with the CLI as the canonical fallback; the daemon itself never opens an outbound notification channel.

```json
{
  "type": "user_escalation_delivered",
  "from": "agent-mod",
  "to": ["user"],
  "phase": "waiting_user",
  "causation_event_id": "evt_user_escalation_requested_01",
  "payload": {
    "escalation_event_id": "evt_user_escalation_requested_01",
    "delivery_target": "origin",
    "platform": "hermes-session",
    "message_ref": "optional external delivery reference",
    "delivered_batch_id": null
  }
}
```

### user_escalation_resolved

Origin class: `participant_cli`.
Canonical command: `atn-control delegate resolve-escalation`.

```json
{
  "type": "user_escalation_resolved",
  "from": "user",
  "to": ["agent-mod"],
  "phase": "working",
  "causation_event_id": "evt_user_escalation_requested_01",
  "payload": {
    "escalation_event_id": "evt_user_escalation_requested_01",
    "answer": "A is the priority. Split B into follow-up scope.",
    "resolved_event_ids": ["evt_clarification_01"],
    "resume_phase": "working"
  }
}
```

Batch example:

```json
{
  "type": "user_escalation_resolved",
  "from": "user",
  "to": ["agent-mod"],
  "phase": "working",
  "causation_event_id": "evt_user_escalation_requested_01",
  "payload": {
    "escalation_event_id": "evt_user_escalation_requested_01",
    "batch_id": "escbatch_01HV...",
    "answers": [
      {"source_event_id": "evt_q1", "answer": "Yes, prefer option A."},
      {"source_event_id": "evt_q2", "answer": "Reject risk R for now."}
    ],
    "resolved_event_ids": ["evt_q1", "evt_q2"],
    "resume_phase": "working"
  }
}
```

`user_escalation_resolved` exits `waiting_user` and returns to `resume_phase`. After resolution, the moderator must relay each answer to affected members through `clarification_answered` (with `--source user`) or the corresponding review answer path. Do not emit `user_escalation_resolved` for incomplete or ambiguous user replies that still require follow-up.

This event may be recorded through a CLI command executed by the moderator runtime after receiving the user's answer. The persisted event uses `from: "user"` because the semantic originator of the answer is the user. The `command_id` still belongs to the CLI command that recorded the answer.

### assignee_update

Origin class: `participant_cli`.
Canonical command: `atn-control delegate update`.

When this event is the terminal result of a bounded runner invocation (e.g. the daemon resumed the assignee's wrapper to capture progress), it carries the originating `runner.invocation_id` plus a `cost` field. `runner_calls_total` was already incremented by the corresponding `runner_invocation_started`; if `cost` is `null`, `missing_cost_runner_calls_total` increments instead of token/USD totals. Direct CLI-originated updates that do not pass through the runner adapter omit both `runner` and `cost`.

```json
{
  "type": "assignee_update",
  "from": "agent-1",
  "to": ["agent-mod"],
  "phase": "working",
  "runner": {
    "invocation_id": "run_01HV...",
    "adapter_kind": "hermes-agent",
    "member": "agent-1",
    "attempt": 1,
    "is_retry": false,
    "source_command_id": "cmd_01HV...",
    "status": "succeeded",
    "duration_sec": 12.34
  },
  "cost": null,
  "payload": {
    "progress_status": "working",
    "summary": "...",
    "blockers": [],
    "next_step": "..."
  }
}
```

The same pattern applies to other semantic events that may originate from runner invocations: `assignee_acknowledged`, `clarification_requested`, `work_submitted`, `review_submitted`, `member_ready`, `member_prepared_partial`, `hand_raise`, `speech`, `consensus_vote`. Direct CLI-originated variants of these events omit the `runner` and `cost` fields.

`progress_status` is the assignee's self-reported work status; it is **not** the session lifecycle `phase` and does not by itself change the session phase. To move the session phase to `blocked`, use a state-transition event such as `session_blocked` (`delegate block`) or a daemon-internal blocking event (`session_budget_exceeded`, `escalation_timeout`, `security_violation`).

Allowed `progress_status` values:

- `working`
- `blocked`
- `partial`
- `testing`
- `ready_to_submit`

### work_submitted

Origin class: `participant_cli`.
Canonical command: `atn-control delegate submit`.

```json
{
  "type": "work_submitted",
  "from": "agent-1",
  "to": ["agent-mod"],
  "payload": {
    "summary": "...",
    "artifacts": [
      {
        "artifact_id": "art_01HV...",
        "stored_path": "sessions/sess_.../artifacts/art_01HV_result.md",
        "sha256": "sha256:...",
        "size_bytes": 12345,
        "mime": "text/markdown"
      }
    ],
    "verification": ["tests passed"],
    "known_risks": []
  }
}
```

CLI commands may accept source artifact paths, but persisted `work_submitted` events reference only ingested artifact records (`artifact_id`, `stored_path`, `sha256`, `size_bytes`, `mime`). The daemon must not persist arbitrary source paths in `work_submitted.payload.artifacts`. The artifact ingestion contract is defined in `architecture.md` and `12-operations.md`.

### review_requested

Origin class: `participant_cli`.
Canonical command: `atn-control delegate review`.

```json
{
  "type": "review_requested",
  "from": "agent-mod",
  "to": ["agent-2"],
  "payload": {
    "target_artifacts": [
      {
        "artifact_id": "art_01HV...",
        "stored_path": "sessions/sess_.../artifacts/art_01HV_result.md"
      }
    ],
    "review_focus": ["risk", "missing constraints"]
  }
}
```

`target_artifacts` references previously ingested artifact records by `artifact_id` and `stored_path`; full artifact metadata lives in the `artifacts` projection (see `architecture.md`).

### review_clarification_requested

Origin class: `participant_cli`.
Canonical command: `atn-control delegate review-question`.

Reviewer asks the assignee a question inside the delegation session. The moderator coordinates delivery and records the exchange.

```json
{
  "type": "review_clarification_requested",
  "from": "agent-2",
  "to": ["agent-1"],
  "payload": {
    "question": "Why did the implementation choose this retry boundary?",
    "context": "Reviewing artifact path/to/result.md",
    "needed_for": "Determine whether this is a bug or an intentional design choice",
    "urgency": "normal"
  }
}
```

### review_clarification_answered

Origin class: `participant_cli`.
Canonical command: `atn-control delegate review-answer`.

```json
{
  "type": "review_clarification_answered",
  "from": "agent-1",
  "to": ["agent-2"],
  "payload": {
    "answer": "The boundary follows the existing timeout policy in ...",
    "evidence": ["path/to/source.py", "test name or log reference"],
    "changes_needed": false
  }
}
```

### review_submitted

Origin class: `participant_cli`.
Canonical command: `atn-control delegate review-submit`.

```json
{
  "type": "review_submitted",
  "from": "agent-2",
  "to": ["agent-mod"],
  "payload": {
    "verdict": "changes_requested",
    "findings": [
      {"severity": "high", "issue": "...", "required_change": "..."}
    ],
    "clarifications_considered": ["evt_..."]
  }
}
```

### revision_requested

Origin class: `participant_cli`.
Canonical command: `atn-control delegate revise`.

```json
{
  "type": "revision_requested",
  "from": "agent-mod",
  "to": ["agent-1"],
  "payload": {
    "required_changes": ["..."],
    "source_review_event_id": "evt_..."
  }
}
```

### work_accepted

Origin class: `participant_cli`.
Canonical command: `atn-control delegate accept`.

```json
{
  "type": "work_accepted",
  "from": "agent-mod",
  "to": ["agent-mod", "agent-1"],
  "payload": {
    "final_summary": "...",
    "accepted_artifacts": [
      {
        "artifact_id": "art_01HV...",
        "stored_path": "sessions/sess_.../artifacts/art_01HV_result.md"
      }
    ],
    "verification": ["..."]
  }
}
```

## Operational events

These events are emitted by `atn-controld` itself rather than by a participant (origin class `daemon_internal` unless noted), but they remain session-scoped: every operational event carries the `session_id`, `session_type`, and `phase` of the affected session. Pre-session failures (registry load failure, daemon start failure) are recorded in the daemon operational log, not in `channel.jsonl`.

### session_budget_exceeded

Origin class: `daemon_internal`.

This event transitions the session to `blocked`; the envelope `phase` is `blocked` and the payload records `prior_phase` and `resume_phase` so that recovery can return the session to its prior phase after `limits_extended`.

For `max_runner_calls`, this event is emitted **before** launching the next runner invocation (pre-dispatch check). For `max_tokens_total` and `max_usd`, it is emitted **after** a terminal runner event with parsed cost updates the observed counters (post-event check). See `operations.md` §3 (Session budget limits).

```json
{
  "type": "session_budget_exceeded",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "blocked",
  "payload": {
    "limit_kind": "max_usd",
    "observed": 25.17,
    "limit": 25.00,
    "prior_phase": "under_review",
    "resume_phase": "under_review",
    "action": "session_blocked"
  }
}
```

`max_runner_calls` example (pre-dispatch):

```json
{
  "type": "session_budget_exceeded",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "blocked",
  "payload": {
    "limit_kind": "max_runner_calls",
    "observed": 500,
    "limit": 500,
    "prior_phase": "working",
    "resume_phase": "working",
    "action": "session_blocked"
  }
}
```

### limits_extended

Origin class: `participant_cli`.
Canonical command: `atn-control limits extend`.

```json
{
  "type": "limits_extended",
  "from": "agent-mod",
  "to": ["agent-mod", "agent-1"],
  "payload": {
    "authorized_by": "user",
    "changes": {"max_usd": 50.00}
  }
}
```

### runner_retry_attempted

Origin class: `daemon_internal`.

`runner_retry_attempted` records the retry **policy decision**. It does **not** itself count as a runner call. The actual retry call is counted by a new `runner_invocation_started` event with a new `runner.invocation_id`. This event does not carry `runner` metadata in the default design.

```json
{
  "type": "runner_retry_attempted",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "payload": {
    "attempt": 2,
    "prior_error": "dispatch_timeout",
    "original_command_id": "cmd_01HV...",
    "target_member": "agent-1"
  }
}
```

### runner_invocation_started

Origin class: `daemon_internal`.

Emitted by `atn-controld` immediately before launching a bounded runner adapter subprocess. This is the durable accounting root for `runner_calls_total`.

```json
{
  "type": "runner_invocation_started",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "working",
  "runner": {
    "invocation_id": "run_01HV...",
    "adapter_kind": "hermes-agent",
    "member": "agent-1",
    "attempt": 1,
    "is_retry": false,
    "source_command_id": "cmd_01HV...",
    "status": "started",
    "duration_sec": null
  },
  "payload": {
    "reason": "assignment_followup",
    "timeout_sec": 180
  }
}
```

Rules:

- The daemon appends this event before subprocess launch.
- If the append fails, the subprocess must not be launched.
- `runner_calls_total` is computed from this event, not from terminal event `cost`.
- The event has no `cost` field because cost is not yet known.

### runner_invocation_failed

Origin class: `daemon_internal`.

Emitted when a runner invocation cannot produce a semantic participant event.

```json
{
  "type": "runner_invocation_failed",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "working",
  "runner": {
    "invocation_id": "run_01HV...",
    "adapter_kind": "hermes-agent",
    "member": "agent-1",
    "attempt": 2,
    "is_retry": true,
    "source_command_id": "cmd_01HV...",
    "status": "timeout",
    "duration_sec": 180.0
  },
  "cost": null,
  "payload": {
    "reason": "dispatch_timeout",
    "retry_remaining": 1,
    "stdout_bytes": 0,
    "stderr_bytes": 1024
  }
}
```

Allowed `payload.reason` values:

- `dispatch_timeout`
- `transport_error`
- `nonzero_exit_empty_output`
- `semantic_parse_error`
- `wrapper_unavailable_after_start`
- `cancelled`
- `interrupted`
- `other`

### user_escalation_delivery_failed

Origin class: `participant_cli`.
Canonical command: `atn-control delegate escalation-delivery-failed`.

`causation_event_id` points to the originating `user_escalation_requested` event.

Emitted by the moderator runtime after the Hermes plugin/gateway helper or equivalent Hermes gateway skill failed to reach the requested target. The moderator may then attempt another target and emit a follow-up `user_escalation_delivered` if a fallback succeeds. The daemon does not generate this event; it only records it.

Delivery failure does **not** resolve the escalation. The session remains in `waiting_user` until `user_escalation_resolved`, cancellation, or timeout/block.

```json
{
  "type": "user_escalation_delivery_failed",
  "from": "agent-mod",
  "to": ["agent-mod"],
  "phase": "waiting_user",
  "causation_event_id": "evt_user_escalation_requested_01",
  "payload": {
    "escalation_event_id": "evt_user_escalation_requested_01",
    "target": "telegram",
    "reason": "gateway_unreachable",
    "will_retry_targets": ["origin"]
  }
}
```

### escalation_deduplicated

Origin class: `mixed`.

Allowed origins:

- `daemon_after_cli`: emitted when a CLI escalation command is deduplicated against an existing escalation or batch item.
- `daemon_internal`: emitted when daemon runtime policy deduplicates a pending batch item during reconciliation.

This event does not enter `waiting_user`, does not increment `user_escalations_total`, and does not create a user-facing escalation.

```json
{
  "type": "escalation_deduplicated",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "payload": {
    "duplicate_of_event_id": "evt_01HV...",
    "similarity": "payload_hash"
  }
}
```

### escalation_rate_limited

Origin class: `mixed`.

Allowed origins:

- `daemon_after_cli`: emitted when a CLI escalation or escalation flush command would exceed `max_user_escalations`.
- `daemon_internal`: emitted when daemon runtime policy attempts to flush a pending batch but the escalation cap has been reached.

This event blocks further escalation delivery but does **not** transition the session to `blocked`. The envelope `phase` is the prior session phase (typically `working` or `waiting_user`) and the projected `status` remains whatever that phase maps to (typically `open`). It does **not** enter `waiting_user` and does **not** create a `user_escalation_requested`. It blocks further `user_escalation_requested` recording until `limits_extended`.

```json
{
  "type": "escalation_rate_limited",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "working",
  "payload": {
    "limit_kind": "max_user_escalations",
    "observed": 10,
    "limit": 10,
    "blocked_batch_id": "escbatch_01HV...",
    "blocked_source_event_ids": ["evt_q1", "evt_q2"],
    "action": "block_further_escalation_recording"
  }
}
```

### security_violation

A session-scoped violation that transitions the session to `blocked` carries envelope `phase: blocked` and payload `prior_phase`/`resume_phase`. Pre-session violations are written to `operational.log` (per `12-operations.md`) and do **not** carry session `phase`/`status`/`prior_phase`/`resume_phase`.

```json
{
  "type": "security_violation",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "blocked",
  "payload": {
    "category": "wrapper_outside_allowlist",
    "observed": "<redacted>",
    "prior_phase": "working",
    "resume_phase": "working",
    "action": "dispatch_blocked"
  }
}
```

### redaction_applied

```json
{
  "type": "redaction_applied",
  "from": "atn-controld",
  "to": [],
  "payload": {
    "pattern_class": "anthropic_api_key",
    "count": 2,
    "source_event_id": "evt_01HV..."
  }
}
```

### escalation_batched

Origin class: `mixed`.

Allowed origins:

- `daemon_after_cli`: emitted when `atn-control delegate escalate --urgency low` is accepted into a pending batch.
- `daemon_internal`: emitted when daemon policy updates, flushes, cancels, or rate-limits an existing batch.

`escalation_batched` is never a user-facing escalation and never enters `waiting_user`.

```json
{
  "type": "escalation_batched",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "working",
  "payload": {
    "batch_id": "escbatch_01HV...",
    "action": "added",
    "batch_window_sec": 1800,
    "batch_deadline_at": "2026-04-25T00:30:00Z",
    "source_event_id": "evt_01HV...",
    "source_member": "agent-1",
    "question_hash": "sha256:...",
    "urgency": "low",
    "included_event_ids": ["evt_01HV..."],
    "pending_count": 1,
    "prior_phase": "working"
  }
}
```

Allowed `payload.action` values: `created`, `added`, `updated`, `flushed`, `cancelled`, `rate_limited`.

`escalation_batched` records a pending low-urgency batch decision. It does **not** enter `waiting_user`. The envelope `phase` remains the prior phase because batching does not change lifecycle state. A later `user_escalation_requested` references the batch through `batch_id` and `included_event_ids`. `escalation_batched` does not increment `user_escalations_total`.

### escalation_timeout

This event transitions the session to `blocked`; the envelope `phase` is `blocked` and the payload records `prior_phase` and `resume_phase`.

`escalation_timeout` applies only after `user_escalation_requested` exists. It does **not** apply to pending `escalation_batched` events.

```json
{
  "type": "escalation_timeout",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "blocked",
  "payload": {
    "escalation_event_id": "evt_user_escalation_requested_01",
    "batch_id": "escbatch_01HV...",
    "waited_sec": 86400,
    "timeout_started_at_event_id": "evt_user_escalation_requested_01",
    "prior_phase": "waiting_user",
    "resume_phase": "waiting_user",
    "action": "session_blocked"
  }
}
```

### runner_result_discarded

```json
{
  "type": "runner_result_discarded",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "phase": "cancelled",
  "runner": {
    "invocation_id": "run_01HV...",
    "adapter_kind": "hermes-agent",
    "member": "agent-1",
    "attempt": 1,
    "is_retry": false,
    "source_command_id": "cmd_01HV...",
    "status": "discarded_after_cancel",
    "duration_sec": 191.3
  },
  "cost": null,
  "payload": {
    "original_command_id": "cmd_01HV...",
    "reason": "arrived_after_cancel"
  }
}
```

### session_handle_rotated

```json
{
  "type": "session_handle_rotated",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "payload": {
    "member": "agent-1",
    "reason": "retry_unsafe_to_reuse"
  }
}
```

## Stream events

Stream events are session-scoped operational events. They may occur in delegation or council sessions.

Important stream event types and origin classes:

- `stream_subscriber_connected` — `daemon_internal`
- `stream_subscriber_heartbeat` — `daemon_internal`
- `stream_cursor_acknowledged` — `daemon_after_cli` (created by `atn-control stream ack`)
- `stream_subscriber_stale` — `daemon_internal`

Stream subscriber stale payload:

```json
{
  "member": "agent-1",
  "subscriber_id": "sub_...",
  "last_cursor": "cur_...",
  "last_heartbeat_at": "2026-04-25T00:00:00Z",
  "action": "repoll_or_mark_partial"
}
```

`stream.status` includes a derived `participant_runtime_readiness` object for
required participants. It is computed from durable stream subscriber,
cursor-ack, heartbeat, attendance/preparation, and selected-runner evidence.
Gateway/process/socket liveness, transcript/export-only evidence, manual profile
fallback text, visible-surface pointers, or parent-channel fallback visibility
must not satisfy participant runtime readiness. The report separates generation
time from readiness-evaluation time: `generated_at` records when the status was
rendered, while `evaluated_at`, `evaluation_mode`, and optional
`freshness_reference_event_id` / `freshness_reference_event_type` record the
time used for cursor/heartbeat freshness. Open sessions use current freshness.
Terminal council sessions use event-time freshness (`evaluation_mode:
terminal_event_time`) so final reports judge participant runtime readiness at
the latest grant/turn evidence when present, or the latest readiness evidence
otherwise, instead of retroactively failing a completed council only because
heartbeat/ack evidence is naturally stale after finalization.

## Council events

Council events use the same envelope with `session_type: council`.


### attendance_requested

Origin class: `participant_cli`.
Canonical command: `atn-control council request-attendance`.

For the first Discord-thread council pass, attendance is a typed subflow inside the `created` phase. It does not introduce a new lifecycle phase unless a later reviewed decision changes the state machine. When `surface.kind` is `discord_thread`, this event is mandatory before `preparation_requested`.

```json
{
  "type": "attendance_requested",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "phase": "created",
  "payload": {
    "surface_kind": "discord_thread",
    "thread_id": "1507515847227215932",
    "timeout_sec": 300,
    "instructions": "Report present only when called or when the moderator grants attendance turn."
  }
}
```

### member_attended

Origin class: `mixed`.

Allowed origins:

- `participant_cli`: the member records attendance explicitly through `atn-control council attend`.
- `daemon_internal`: attendance timeout records `no_response_timeout` for a missing member.

```json
{
  "type": "member_attended",
  "from": "agent-1",
  "to": ["agent-mod"],
  "phase": "created",
  "payload": {
    "status": "present",
    "summary": "Present and ready for the council.",
    "discord_message_id": "optional"
  }
}
```

Allowed `status` values: `present`, `partial`, `unavailable`, `no_response_timeout`.

When the daemon emits timeout attendance, the event uses `from: "atn-controld"`, `to: ["agent-mod"]`, and `payload.member` records the affected member. Timeout payloads must preserve `status: "no_response_timeout"` and timeout source evidence so they remain distinguishable from participant success or partial-success records.

For `surface.kind=discord_thread`, `preparation_requested` is valid only after one terminal `member_attended` record exists for every required participant named by the council membership/attendance request. Terminal attendance status is one of `present`, `partial`, `unavailable`, or `no_response_timeout`. Missing attendance records, duplicate unresolved attendance state, or attendance for only a subset of required participants must fail closed at append time for `preparation_requested`.

Before appending `preparation_requested` for a Discord-thread council, the daemon
must apply expired attendance timeouts and then fail closed unless required
participant runtime readiness is explicit: subscriber presence, valid cursor ack,
fresh cursor ack, fresh heartbeat, and attendance response/timeout evidence. A
failure leaves no `preparation_requested` event; timeout diagnostics remain
durable `member_attended` events.

### agenda_locked

Origin class: `participant_cli`.
Canonical command: `atn-control council lock-agenda`.

`agenda_locked` freezes the decision question and required selected-runner agenda context before substantive preparation/discussion. Its payload must include non-empty string `decision_question`, `success_criteria`, and `out_of_scope_policy`; topic-drift policy, final summaries, and Kanban/Vault return paths must refer back to this locked agenda. The daemon/storage event boundary rejects `lock-agenda` commands missing any of those three fields, so direct daemon/plugin paths cannot persist an agenda that only contains a decision question. When `surface.kind` is `discord_thread`, this event is mandatory before `preparation_requested`.

```json
{
  "type": "agenda_locked",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "phase": "created",
  "payload": {
    "decision_question": "Decide next action for Kanban card t_xxxxx.",
    "success_criteria": "Consensus identifies the next bounded action, owner, and evidence requirement.",
    "out_of_scope_policy": "New topics become follow-up card candidates, not current-thread expansion.",
    "max_rounds": 2
  }
}
```

### preparation_requested

Origin class: `daemon_after_cli`.
Created by: `atn-control council prepare`.

For `surface.kind=discord_thread`, `preparation_requested` is fail-closed unless the session is still in `created` and the prior event log contains, in order, `attendance_requested`, one terminal `member_attended` record for each required participant, and `agenda_locked`. Rejection must leave the session in `created` and must not append a partial `preparation_requested` event.

```json
{
  "type": "preparation_requested",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "payload": {
    "topic": "Discuss topic A",
    "timeout_sec": 600
  }
}
```

### member_ready

Origin class: `participant_cli`.
Canonical command: `atn-control council ready`.

```json
{
  "type": "member_ready",
  "from": "agent-1",
  "to": ["agent-mod"],
  "payload": {
    "summary": "Found three relevant constraints.",
    "notes": "sessions/sess_.../participants/agent-1/notes.md"
  }
}
```

### member_prepared_partial

Origin class: `mixed`.

Allowed origins:

- `participant_cli`: the member records partial preparation explicitly.
- `daemon_internal`: preparation timeout expires before the member marks ready.

Canonical command for participant-originated partial preparation: `atn-control council prepared-partial`.

Participant-originated example:

```json
{
  "type": "member_prepared_partial",
  "from": "agent-1",
  "to": ["agent-mod"],
  "payload": {
    "reason": "member_reported_partial",
    "summary": "Partial notes are available.",
    "notes": "sessions/sess_.../participants/agent-1/notes.md"
  }
}
```

Daemon-timeout example:

```json
{
  "type": "member_prepared_partial",
  "from": "atn-controld",
  "to": ["agent-mod"],
  "payload": {
    "member": "agent-1",
    "reason": "timeout",
    "summary": "Preparation timed out; proceed with partial or empty notes.",
    "notes": "sessions/sess_.../participants/agent-1/notes.md"
  }
}
```

Rules:

- When a member runtime explicitly records partial preparation, `from` is the member id and `payload.reason` is normally `member_reported_partial`.
- When the daemon emits `member_prepared_partial` because the preparation timeout expired, the event originator is `atn-controld`, the affected member is recorded in `payload.member`, and `payload.reason` is `timeout`.
- Before appending `hand_raise_requested` for a Discord-thread council, the daemon must apply expired preparation timeouts and then fail closed unless required participant runtime readiness remains explicit and every required participant has preparation success or partial/failure evidence. Timeout diagnostics remain durable `member_prepared_partial` events and must not be rewritten as success.

### hand_raise_requested

Origin class: `daemon_after_cli`.
Created by: `atn-control council poll`.

```json
{
  "type": "hand_raise_requested",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "payload": {
    "turn": 5,
    "research_timeout_sec": 600,
    "question": "Who has a material objection or additional evidence?",
    "response_window_id": "rw_sess_example_5_evt_hand_raise_requested_cmd_poll_5",
    "response_window_duration_sec": 120,
    "response_window_opened_at": "2026-07-04T13:30:02Z",
    "response_window_deadline_at": "2026-07-04T13:32:02Z",
    "required_members": ["agent-1", "agent-2", "agent-3"]
  }
}
```

PRSLR-003 makes each `hand_raise_requested` event open a daemon-owned 120-second response window for the turn. `status.session` exposes `response_window_accounting` with `state`, `closed_reason`, required/responded/missing members, and timeout auto-drop counts. The window closes early when every required member records either `hand_raise` or canonical `hand_raise_dropped`. `hand_raise` and manual `council.drop` after the deadline fail closed without appending an event; daemon-owned timeout auto-drop is the only supported post-deadline drop path. On timeout, the daemon sweeper records `hand_raise_dropped` from `atn-controld` for missing members with `payload.auto: true`, `payload.auto_reason: "timeout"`, and a deterministic command id so daemon restart/replay does not append duplicate auto-drops. This is local control behavior only and does not claim participant runtime readiness; `response_window_accounting.participant_runtime_readiness` remains `not_claimed_prslr004_pending` until PRSLR-004.

### hand_raise

Origin class: `participant_cli`.
Canonical command: `atn-control council hand-raise`.

```json
{
  "type": "hand_raise",
  "from": "agent-3",
  "to": ["agent-mod"],
  "payload": {
    "turn": 5,
    "wants_to_speak": true,
    "intent": "risk",
    "relevance": 5,
    "urgency": 4,
    "research_done": true,
    "evidence_summary": "Checked the relevant docs and found a constraint conflict.",
    "reason": "This should be resolved before drafting the conclusion.",
    "target_links": [
      {
        "target_event_id": "evt_01HV...",
        "target_claim_id": "T02.C1",
        "stance": "challenge"
      }
    ]
  }
}
```

ARGUE-002 adds optional `target_links[]` for argument-graph-aware hand raises. Each target link pairs `target_event_id`, `target_claim_id`, and intended `stance` in one object. The stable linked-stance enum for ARGUE-002 fixtures is `support`, `challenge`, `refine`, `extend`, `synthesize`, `question`, `risk_addition`, and `decision_frame`. `target_event_ids[]` and `target_claim_ids[]` parallel arrays are not the ARGUE handoff shape.

COUNCIL-STAB-001 makes `hand_raise.payload.intent` or `hand_raise.payload.reason` the required stance source for selected-runner floor grants. The daemon must not infer that stance from prose, ARGUE display hints, or caller-supplied grant payload fields; if the selected member has no matching same-turn hand raise with a non-empty `intent` or `reason`, `council.grant` fails closed before appending `speaker_selected`.

### speaker_selected

Origin class: `participant_cli`.
Canonical command: `atn-control council grant`.

```json
{
  "type": "speaker_selected",
  "from": "agent-mod",
  "to": ["agent-3"],
  "payload": {
    "turn": 5,
    "selection_mode": "relevance",
    "score": 31,
    "reason": "Highest eligible score."
  }
}
```

`selection_mode` and scoring rules are normative in `architecture.md`.

For Discord-thread councils, `selection_mode` may also be `moderator_direct` or `role_order` per `architecture.md`. These modes still use `speaker_selected` as the durable floor-grant record. If the session has `session_created.payload.turn_mode`, that value is only the intended/default policy. The durable per-turn audit fact is this event's `payload.selection_mode`. A per-turn `selection_mode` may deviate from `turn_mode` only when `payload.reason` is present and names the operational reason for the deviation.

For selected-runner dispatch, `speaker_selected.payload.stance_assignment` is control-owned derived evidence from the prior matching `hand_raise` event. It is not accepted from the `council.grant` request payload as an authority shortcut.

### speech

Origin class: `participant_cli`.
Canonical command: `atn-control council speak`.

```json
{
  "type": "speech",
  "from": "agent-3",
  "to": ["agent-mod", "agent-1", "agent-2"],
  "payload": {
    "turn": 5,
    "speech": "...",
    "evidence": ["path/or/url"],
    "responds_to_event_id": "evt_01HV...",
    "claims": [
      {
        "claim_id": "T05.C1",
        "summary": "Static fixtures should precede runtime enforcement.",
        "kind": "proposal"
      }
    ],
    "stance_links": [
      {
        "target_event_id": "evt_01HV...",
        "target_claim_id": "T02.C1",
        "stance": "support",
        "rationale": "The prior claim establishes the same fixture-first sequence."
      }
    ],
    "contribution_type": "support"
  }
}
```

For visible rendering, `speech` is the only participant-originated council utterance event that may be rendered as a member speech turn. The active speaker is proven by the preceding cursor-ordered `speaker_selected` event for the same `payload.turn`; renderers must flag or fail closed on a missing/mismatched floor grant instead of treating an external message author as authority. `payload.evidence` is supporting material, not a delivery receipt. A surface message id may be recorded separately only after the durable `speech` event exists.

ARGUE-002 adds optional argument-graph fields to `speech.payload` without changing the schema version:

- `claims[]`: concise participant assertions with `claim_id`, `summary`, and optional `kind`.
- `stance_links[]`: links from the current speech to earlier speech claims. Each link has `target_event_id`, `target_claim_id`, `stance`, and `rationale`. The ARGUE-002 stable linked-stance enum is `support`, `challenge`, `refine`, `extend`, `synthesize`, `question`, `risk_addition`, and `decision_frame`.
- `contribution_type`: the primary contribution role for the speech. It may be one of the linked stance values or `new_axis`.
- `new_axis_reason`: required by quality-required policy when `contribution_type` is `new_axis`.

`new_axis` is not a linked stance because it has no prior target. Existing `responds_to_event_id` is a coarse legacy display hint; when `stance_links[]` is present, ARGUE-aware consumers use `stance_links[]` as the relation authority. ARGUE-002 publishes static conformance fixtures only. `control/ARGUE-003` covers append-time validation, quality-required rejection, quality-warn diagnostics, and moderator scoring hooks. `control/ARGUE-004` adds side-effect-free transcript/export/SQLite projection preservation for ARGUE relation evidence from cursor-ordered `speech` events.

### moderator_intervention

Origin class: `participant_cli`.
Canonical command: `atn-control council intervene`.

```json
{
  "type": "moderator_intervention",
  "from": "agent-mod",
  "to": ["agent-2"],
  "payload": {
    "reason": "topic_drift",
    "message": "Tie this back to the decision criteria or withdraw it."
  }
}
```

### draft_conclusion

Origin class: `participant_cli`.

Command paths:

- `atn-control council propose`
- `atn-control council revise`

```json
{
  "type": "draft_conclusion",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "payload": {
    "draft_version": 2,
    "draft": "...",
    "revision_reason": "Addressed agent-2 block vote.",
    "supersedes_draft_version": 1
  }
}
```

First proposal omits `revision_reason` and `supersedes_draft_version`. Subsequent revisions increment `draft_version` and reference the prior version.

### consensus_vote_requested

Origin class: `participant_cli`.
Canonical command: `atn-control council request-vote`.

Stream-visible event that tells member runtimes a consensus vote is open. Member runtimes should not vote until they observe this event (or its replay).

```json
{
  "type": "consensus_vote_requested",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "payload": {
    "draft_version": 1,
    "timeout_sec": 600
  }
}
```

### consensus_vote

Origin class: `participant_cli`.
Canonical command: `atn-control council vote`.

```json
{
  "type": "consensus_vote",
  "from": "agent-3",
  "to": ["agent-mod"],
  "payload": {
    "draft_version": 1,
    "vote": "block",
    "reason": "...",
    "required_change": "..."
  }
}
```

### council_finalized

Origin class: `participant_cli`.
Canonical command: `atn-control council finalize`.


```json
{
  "type": "council_finalized",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "payload": {
    "final_summary": "...",
    "consensus": "approve",
    "decision_question_event_id": "evt_agenda_locked_01",
    "surface_evidence": {
      "status": "posted",
      "kind": "discord_thread",
      "thread_id": "1507515847227215932",
      "final_message_id": "msg_123"
    },
    "linked_authority_result": {
      "status": "posted",
      "kanban_card_id": "t_xxxxx",
      "kanban_comment_id": "optional",
      "vault_decision_note": "optional",
      "followup_card_id": "optional",
      "failure_reason": "optional",
      "evidence": ["optional path or url"]
    }
  }
}
```

`council_finalized` records the council decision. It does not by itself prove that linked Kanban/Vault authority return is complete. When `linked_authority` was configured, `linked_authority_result.status` is required and must be one of `posted`, `failed`, or `pending_followup`:

- `posted`: moderator/Gray evidence proves the Kanban comment and/or Vault decision note was written; the corresponding id/path/evidence field must be present.
- `failed`: the return attempt failed; `failure_reason` and follow-up handling evidence are required.
- `pending_followup`: a clearly linked follow-up/review card or pending-review handoff remains; `followup_card_id` or equivalent evidence is required.

`surface_evidence` records visible-room delivery evidence for the final result. For standard live-visible `discord_thread` councils, `council.finalize` requires `surface_evidence` to be an object with explicit `status: "posted"`, the configured `thread_id`, and a visible-message proof pointer: `final_message_id`, `message_id`, `message_ref`, or an explicitly typed visible-message equivalent accepted by control. Missing/empty `status`, malformed object shape, unsupported status, failed, pending, unproven, missing visible-message pointer, missing-thread, or wrong-thread evidence fails closed before finalization. `kanban_comment_id`, `vault_decision_note`, and generic linked-authority evidence are not visible-surface proof and must be recorded under `linked_authority_result` instead. Non-visible/local-daemon-only councils may still record durable finalization without visible-surface proof when explicitly approved, but that is not visible UX success. The posted pointer is evidence that the final summary was posted; it is not the source of the final decision.

`council.unresolved` is the honest terminal alternative when visible closeout proof is incomplete or follow-up is required. It may carry `surface_evidence.status` such as `pending_followup` plus `timeout_evidence`/follow-up pointers and must not be reported as finalized success.

The daemon/replay must not create Kanban comments, Vault notes, or visible-room messages directly. Absence of posted evidence, or status `failed`/`pending_followup`, means the origin authority path remains blocked/pending review or must be represented by a linked follow-up; final reports must not claim linked authority return or visible delivery is complete.

### council_unresolved

Origin class: `participant_cli`.
Canonical command: `atn-control council unresolved`.

---

## Merged from `docs/spec/protocol-and-cli.md`

# CLI Specification

Official CLI: `atn-control`. The Hermes plugin is the preferred agent-facing integration layer, but this CLI is the canonical diagnostics, recovery, test, and manual-operation contract.

## Principles

- All state mutations go through the daemon.
- Commands are explicit and auditable.
- Status commands are concise by default and verbose on request.
- The CLI should be usable by the moderator agent through terminal tool calls and should remain available when the Hermes plugin is absent, disabled, or broken.
- Hermes plugin tools and slash commands must map to the same typed command models and daemon validations as the CLI. The plugin should call the ATN protocol client/contract directly, not shell out, except as a compatibility fallback or for operator-equivalent diagnostics.

## Global write command rules

Every state-mutating CLI command accepts an optional `--command-id cmd_...`. If `--command-id` is omitted, the CLI generates one. Hermes plugin tool calls that mutate state must provide or obtain the same command-id semantics through the ATN protocol client/contract. The daemon uses `command_id` for idempotency per `operations.md` §2; retrying the same logical command must reuse the same `command_id`.

Every state-mutating command must declare the protocol event type or event sequence it emits. A command may emit more than one event only when this document explicitly lists the emitted sequence (e.g. `delegate new` emits `session_created` followed by `task_assigned`).

Participant-originated events (origin class `participant_cli` in `protocol-and-cli.md`) must have an explicit canonical CLI command path here. If the Hermes plugin exposes the same event, the plugin command/tool must document the equivalent CLI path and emit the same daemon command model. Daemon-originated operational events do not require public write commands.

Ambiguous state-mutating commands are not allowed. In particular: clarification answers, general delegation messages, review verdicts, escalation delivery audit, and council readiness each have explicit commands and must not be conflated.

## Recipient normalization

CLI commands may accept a single `--to` value for convenience, but persisted protocol events always store `to` as `array<string>` (per `protocol-and-cli.md`):

- `--to agent-1` becomes `"to": ["agent-1"]`.
- Repeated `--to agent-1 --to agent-2` becomes `"to": ["agent-1", "agent-2"]`.
- Council `--members agent-1,agent-2,agent-3` expands to explicit recipient lists for broadcast events; the daemon must not emit `"to": ["all"]`, `"to": "*"`, or an omitted `to` field.

The daemon removes duplicate recipients, validates each recipient, and stores recipients in canonical order (session participant order, then the reserved external principal `user` last). Recipient order has no semantic meaning.

Recipient flags:

- `--to <principal>` is repeatable where multi-recipient commands are supported.
- Comma-separated input may be accepted by commands that already use comma-separated member lists (e.g. `--members`), but the daemon must normalize to the same array form.
- Unknown recipients are rejected unless the value is the reserved principal `user`.
- `atn-controld` is not accepted as a normal participant-supplied recipient.

`to` is **semantic addressing**, not stream access control. Stream read permissions are governed by `12-operations.md`.

## Daemon commands

```bash
atn-control daemon start
atn-control daemon stop
atn-control daemon status
```

`atn-control daemon start` validates `<data_home>` and `registry.yaml` before reporting ready. If validation fails, the daemon does not accept session creation or dispatch commands; the failure is written to `<data_home>/operational.log` per `12-operations.md`.

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
- Council `status` / `stream status` include derived `discussion_lifecycle` when available. With `limits.max_discussion_turns` configured, `council propose` requires the T0 moderator opening, T1..Tmax selected participant discussion speeches, and one selected closeout speech per participant; the terminal moderator synthesis/final closeout is accounted as T(n+p+1), with `terminal_synthesis_turn`, `terminal_synthesis_expected_visible_index`, and terminal event/summary fields derived from session parameters. `council finalize` requires posted visible closeout proof for standard live-visible `discord_thread` sessions; `council unresolved` remains available as the fail-closed terminal path. The lifecycle status is additive and fail-closed: operator finality should use `completion_verdict` plus `terminal_visible_closeout_proof_status`, not legacy `propose_ready`, discussion counts, or terminal event presence alone. `completion_verdict=finalized` is valid only when the terminal visible closeout proof status is `posted`.
- Every emitted line includes `event_id`, `cursor`, `session_id`, `type`, `from`, `to`, and `payload`. `from` is a string; `to` is always an array of strings (per `protocol-and-cli.md`).
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
- If the acknowledgement contains blocking questions, the daemon may transition to `needs_clarification` per `architecture.md`.

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
- Artifact ingestion follows `12-operations.md` and `architecture.md`.
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

This is a manual block, distinct from daemon-originated `session_budget_exceeded`/`escalation_timeout` which may also move a session into a blocked state per `operations.md`.

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
atn-control council grant <session_id> --to agent-3 --mode role_order --turn 1 --reason "Turn 1 risk review"
atn-control council speak <session_id> --from agent-3 --stdin
```

Emits:

- `council poll` → `hand_raise_requested`.
- `council hand-raise` → `hand_raise`.
- `council grant` → `speaker_selected`.
- `council speak` → `speech`.

`council grant --mode <mode>` writes the per-turn `speaker_selected.payload.selection_mode`. If a session was created with `--turn-mode`, the grant mode may match that default or deliberately deviate from it. Any deviation requires `--reason`; the persisted `speaker_selected` event must include that reason so audit/replay can explain the difference.

`council grant --member <id>` is an operator alias for `--to <id>`. Supplying both with different values fails closed. `--turn <n>` is the canonical turn field; `--round <n>` remains a CLI compatibility alias only and writes `payload.turn`. Structured lifecycle `--from-file` payloads for `hand-raise`, `grant`, and `speak` require canonical field names and reject legacy `payload.round` before daemon submission with a migration hint to `turn`.

For selected-runner councils, `grant` derives `speaker_selected.payload.stance_assignment` only from a matching same-member/same-turn `hand_raise.payload.intent` or `hand_raise.payload.reason`; caller-supplied grant payload stance is not authority, and a grant without that stance-bearing hand raise is rejected before appending `speaker_selected`.

`council.grant` transport/runner status is not collapsed into daemon command failure. If the `speaker_selected` append succeeds but selected-runner dispatch times out or reaches another terminal runner failure, the command response remains OK with `append_status=accepted`, `dispatch_status=runner_terminal_failure`, `runner_status` such as `timeout`, `speech_link_status=missing_linked_runner_speech`, `followup_required=true`, and event pointers such as `grant_event_id` plus `runner_failure_event_id`. A daemon command failure is reserved for validation, lifecycle, storage, or unsupported-runtime errors before the durable append/status handoff.

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
atn-control council finalize <session_id> --from-file finalize.json
atn-control council unresolved <session_id> --from-file unresolved.json
```

Example `finalize.json` for a standard live-visible `discord_thread` council:

```json
{
  "final_summary": "Decision summary visible to the council.",
  "surface_evidence": {
    "status": "posted",
    "kind": "discord_thread",
    "thread_id": "thread-id-from-session-surface",
    "final_message_id": "discord-message-id"
  },
  "linked_authority_result": {
    "status": "posted",
    "kanban_comment_id": "kc_123"
  }
}
```

Example `unresolved.json`:

```json
{
  "reason": "visible closeout proof still needs follow-up",
  "timeout_evidence": "operator follow-up required",
  "surface_evidence": {
    "status": "pending_followup",
    "kind": "discord_thread",
    "thread_id": "thread-id-from-session-surface",
    "followup_card_id": "t_followup"
  }
}
```

Emits:

- `council propose` → `draft_conclusion` with `draft_version: 1`. Draft text may come from `--from-file` or stdin.
- `council revise` → `draft_conclusion` with incremented `draft_version`, `revision_reason`, and `supersedes_draft_version`.
- `council request-vote` → `consensus_vote_requested`. Member runtimes vote only after observing this event or its replay.
- `council vote` → `consensus_vote`.
- `council finalize` → `council_finalized`.
- `council unresolved` → `council_unresolved`.

`council finalize --from-file <finalize.json>` is a command-specific structured JSON path. The file must be a JSON object with required non-empty string `final_summary`, optional object `surface_evidence`, and optional object `linked_authority_result`. Unsupported fields fail closed before daemon submission. For standard live-visible `discord_thread` councils, daemon validation then requires posted visible closeout proof: `surface_evidence.status` must be explicitly `posted`, `surface_evidence.thread_id` must match the configured session thread, and `surface_evidence.final_message_id`, `message_id`, `message_ref`, or an explicitly typed visible-message equivalent must be present. `kanban_comment_id`, `vault_decision_note`, and generic return evidence belong in `linked_authority_result`; they do not satisfy visible-surface proof. Invalid statuses such as `complete`, missing/empty status, malformed/non-object `surface_evidence`, missing visible-message pointers, missing thread binding, failed/pending/unproven evidence, or wrong-thread evidence produce structured validation errors rather than finalized success.

`council unresolved --from-file <unresolved.json>` is also structured JSON. It requires non-empty string `reason` and may include `timeout_evidence` plus object `surface_evidence`; unsupported fields fail closed. `unresolved` records an honest terminal alternative and follow-up evidence, not finalized success.

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

---

## Merged from `docs/spec/protocol-and-cli.md`

# DELEG-002 Conformance Fixture Matrix

## Purpose

`DELEG-002 | Delegation/review conformance fixture matrix` is the control-owned handoff for plugin-consumable delegation/review conformance fixtures after `DELEG-001` implemented local delegation and review-gate runtime behavior.

The problem is a cross-repo fixture acceptance gap, not a new plugin-owned runtime design. The plugin must not infer daemon command, response, idempotency, permission, retry, or malformed-response shapes from Go internals, local E2E tests, chat notes, or plugin assumptions. The durable contract is this control repo's protocol/docs plus `testdata/conformance/manifest.json` and the fixture files it references.

## Roadmap status

`docs/roadmap.md` marks DELEG-002 as `completed` once this fixture/test/docs slice lands with local control verification, downstream plugin compatibility evidence, and the implementation run's final KAH gate. Roadmap completion is not claimed by docs wording alone; it is backed by the manifest entries, fixture files, conformance tests, contract checker, downstream compatibility check, and KAH evidence for the run.

## Scope

DELEG-002 updates these control-owned surfaces:

- `testdata/conformance/manifest.json`
- `testdata/conformance/fixtures/command/`
- `testdata/conformance/fixtures/error/`
- `testdata/conformance/fixtures/event/`
- `internal/protocol/conformance_test.go`
- `scripts/check_plugin_contract.py`
- `testdata/conformance/README.md`
- roadmap/testing/cross-repo docs that describe plugin consumption boundaries

## Non-scope

- No edits to `atn-plugin` in this task.
- No live Discord, Hermes gateway, KAB, external daemon, auth, token, localhost, socket fallback, or network behavior.
- No weakening of existing conformance guardrails.
- No plugin-owned UX/runtime behavior inside control docs; control owns the daemon/protocol/fixture contract and plugin consumes it.
- No claim of plugin release readiness, live-client readiness, or production delegation support from fixture publication alone.
- No malformed/schema-invalid payloads in ordinary valid `manifest.json` entries.

## Published fixture matrix

All paths below are listed in `testdata/conformance/manifest.json` and must remain valid parseable fixtures.

| Behavior | Fixture paths | Contract |
| --- | --- | --- |
| Delegation creation | `fixtures/command/delegate-new-request.json`, `fixtures/command/delegate-new-response.json`, `fixtures/event/task-assigned-delegation.json` | `delegate.new` returns `ok: true`, `result.session_id`, `result.results[]`, and `result.deduplicated`. |
| Work submission | `fixtures/command/delegate-submit-request.json`, `fixtures/command/delegate-submit-response.json`, `fixtures/event/work-submitted.json` | `delegate.submit` appends `work_submitted` and returns the standard append response shape. |
| Duplicate/idempotency | `fixtures/command/delegate-submit-duplicate-request.json`, `fixtures/command/delegate-submit-duplicate-response.json` | The duplicate submit pair is representative of the general `command_id` idempotency rule for append-style commands. It is not a submit-only special case. Same logical command plus same `command_id` returns `ok: true`, prior append result fields, and `result.deduplicated: true`. |
| Review request | `fixtures/command/delegate-review-request.json`, `fixtures/command/delegate-review-response.json`, `fixtures/event/review-requested.json` | `delegate.review` appends `review_requested` and moves submitted work to `under_review`. |
| Review submission | `fixtures/command/delegate-review-submit-request.json`, `fixtures/command/delegate-review-submit-response.json`, `fixtures/event/review-submitted.json` | `delegate.review_submit` appends `review_submitted` with a stable verdict/finding payload. |
| Canonical non-review delegation action | `fixtures/command/delegate-accept-request.json`, `fixtures/command/delegate-accept-response.json`, `fixtures/event/work-accepted.json` | `delegate.accept` is the canonical non-review fixture because it is an existing runtime command path and terminally appends `work_accepted`. |
| Permission and validation errors | `fixtures/error/delegate-unauthorized-actor.json`, `fixtures/error/delegate-review-wrong-phase.json`, `fixtures/error/delegate-review-submit-invalid-verdict.json` | Delegation/review failures use normal structured command errors with `ok: false`, `error.code: validation_error`, safe `message`, and safe `details`. |

## Retryable failure policy

DELEG-002 does not publish a public retryable delegation/review command-response shape. Plugin DELRV-2 may test fail-closed preservation of fake-daemon retryability fields, but those fields remain plugin-local test inputs unless a future control task adds a valid structured-error fixture and checker coverage for a stable retryability contract.

## Malformed daemon response policy

Malformed JSON, schema-invalid daemon payloads, truncated responses, and unexpected envelopes are negative-test inputs, not ordinary conformance fixtures.

- Valid `manifest.json` entries remain parseable and schema-intended examples.
- Invalid examples must not be listed as ordinary valid fixtures unless a future manifest category explicitly supports invalid fixtures.
- Plugin DELRV-2 should derive fail-closed behavior from the command-envelope and structured-error schemas here, reject malformed fake-daemon payloads locally, and not reinterpret malformed data as success.

## Plugin consumption boundary

The plugin consumes these fixtures; it does not define daemon/runtime authority. If plugin tests need a new command, error, idempotency, retry, or malformed-response shape, that shape must first be published here or explicitly documented as plugin-local fake-daemon negative-test input.

## Verification expectations

Control-side verification for this matrix includes:

```bash
GOCACHE=/tmp/kkachi-go-build-cache go test ./...
make test-prepare
make check-plugin-contract
make test
```

Full closeout additionally needs downstream plugin `make check-core-contract` from the current local workspace path `/Users/draccoon/Workspace/SeventeenthEarth/kkachi/agent-turn-network-plugin` (public repo label `atn-plugin`; current local compatibility path) and `kkachi-agent-helper gate final run-20260606T081553Z-f394a2b5b90a --json`.
