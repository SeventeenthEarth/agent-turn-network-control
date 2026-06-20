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

Field rules (full contract in `13-operational-contracts.md`):

- `schema_version`: integer; bumped on any breaking envelope change. Readers refuse unknown versions unless a migration is registered.
- `event_id`: unique per persisted event (ULID recommended).
- `command_id`: set by the originator. CLI-originated events require it and deduplicate on retry; daemon-generated events that follow a CLI command reuse the originating id; pure daemon-originated events (for example `session_budget_exceeded`) may leave it null. Full rules in `13-operational-contracts.md`.
- `causation_event_id`: the event that directly caused this one. Required whenever `command_id` is null. Empty only for session-initial events.
- `correlation_id`: logical thread across related commands and events; defaults to `session_id`.
- `cost`: optional. Present on terminal runner events; the value may be an object or `null` (`null` means measured cost was unavailable, **not** zero cost). Omitted entirely on non-runner events. See `13-operational-contracts.md` §3.
- `runner`: optional object identifying the bounded adapter invocation attempt associated with the event. Present on runner accounting events (`runner_invocation_started`, `runner_invocation_failed`, `runner_result_discarded`) and on terminal semantic events produced by a runner invocation. Fields: `invocation_id` (unique per actual adapter invocation attempt), `adapter_kind`, `member`, `attempt` (1 for first, 2+ for retries), `is_retry`, `source_command_id`, `status`, `duration_sec` (null until the terminal runner result is known). Allowed `runner.status` values: `started`, `succeeded`, `failed`, `timeout`, `semantic_error`, `discarded_after_cancel`, `cancelled`, `interrupted`. If `runner` is present on a terminal runner event, `cost` must also be present (object or `null`). If `runner` is absent, `cost` must be omitted.
- `phase`: the exact state-machine phase of the session **after this event is applied** (post-transition value). It is part of the durable event source of truth and is used by replay to rebuild session state. Business logic and state transitions use `phase`, not `status`. Allowed values:
  - delegation: `created`, `assigned`, `acknowledged`, `working`, `needs_clarification`, `waiting_user`, `submitted`, `under_review`, `revision_requested`, `blocked`, `accepted`, `cancelled` (per `06-state-machine.md`).
  - council: `created`, `preparation`, `discussion`, `draft_conclusion`, `consensus_vote`, `blocked`, `finalized`, `unresolved`, `cancelled` (per `06-state-machine.md`).
  Unknown phase values fail closed at append time.

The event envelope deliberately does **not** include `status`. `status` is a derived roll-up projection (allowed values `open`/`blocked`/`terminal`) defined in `05-storage-schema.md` and `13-operational-contracts.md` §5; it is stored only in `session.yaml` and the SQLite `sessions.status` column for query, UI, and active-session lock checks. The durable lifecycle source is `phase`.
- `session_type`: `delegation` or `council`. Other values fail closed.
- `turn`: integer turn counter; required for council events that occur inside a turn (`hand_raise_requested`, `hand_raise`, `speaker_selected`, `speech`, `moderator_intervention`), null otherwise.
- `from`: required string. The single principal that originated the event. Allowed values are registry member ids plus the reserved principals `user` and `kkachi-agent-networkd`. `from` is never an array; if multiple actors are relevant (e.g. an `escalation_batched` that summarizes multiple source members), the originator stays a single string and the related actors are listed in `payload`.
- `to`: required `array<string>`. The semantic recipients of the event. A single recipient is represented as a one-element array (e.g. `["agent-mod"]`, never `"agent-mod"`). Broadcast is represented by an explicit recipient list (e.g. `["agent-1", "agent-2", "agent-3"]`); special values such as `"all"`, `["all"]`, or `"*"` are forbidden. `to: []` is allowed only for unaddressed session audit events. Recipients must be unique within a single event; duplicates are normalized away or rejected before append. Recipient order has no semantic meaning; the daemon stores recipients in canonical order for deterministic projection and transcript rendering. Allowed recipient values are registry member ids and the reserved principal `user`; `kkachi-agent-networkd` is not a normal recipient.

`to` is **semantic addressing**, not stream access control. A valid session participant may observe events not addressed to it; the runtime decides whether to act by inspecting `type`, `from`, `to`, role, phase, and policy. Stream read permissions are governed by `12-security.md`.

This initial Release v1 schema uses `to` as `array<string>` from the start. Changing `to` between string and array form is a breaking envelope change and requires a `schema_version` migration per `13-operational-contracts.md`.

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

- Events with origin class `participant_cli` must have an explicit command path in `04-cli-spec.md`. A single event type may have more than one command path only when the event-to-command matrix in `04-cli-spec.md` lists the relationship.
- Events with origin class `daemon_after_cli` must declare the CLI command(s) that cause them.
- Events with origin class `daemon_internal` do not require public write commands.
- Origin class is consistent with the `command_id` and `causation_event_id` rules in `13-operational-contracts.md` §2.

## Participant roles

The envelope `from` and `to` fields carry principal identifiers. Most principals are registry member ids. The reserved principals are:

- `user`
- `kkachi-agent-networkd`

`from` is always a single string. `to` is always an array of strings.

Role information (`moderator`, `assignee`, `reviewer`, `participant`, `observer`) is resolved out-of-band via the registry and projected into `session_participants.role`. The protocol does not derive permissions from role strings; semantic intent comes from the event `type`, `from`, `to`, session phase, and policy. Reserved principal collision in the registry (`members.user`, `members.kkachi-agent-networkd`) is rejected at registry load time per `12-security.md`.

## Stream delivery contract

The daemon exposes events to the KAN protocol client/contract and canonical CLI stream as an ordered cursor sequence. The cursor is a daemon-issued opaque value, normally derived from the append offset plus `event_id`. It is not interpreted by member runtimes.

Rules:

- `channel.jsonl` append is the source of truth; a stream event must not be visible before the append succeeds.
- Stream consumers, whether Hermes plugin tools or the canonical CLI, emit replayed events from `--since` before live events.
- Every stream frame includes `cursor`, the full event envelope, and an `is_replay` boolean.
- Member runtimes acknowledge processed cursors through the KAN protocol client/contract; `kkachi-agent-network stream ack` is the canonical CLI fallback.
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
Created by: `kkachi-agent-network delegate new` or `kkachi-agent-network council new`.

```json
{
  "type": "session_created",
  "from": "kkachi-agent-networkd",
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
  "from": "kkachi-agent-networkd",
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
    "linked_authority": {
      "kanban_card_id": "t_xxxxx",
      "vault_decision_note": "optional"
    },
    "turn_mode": "role_order",
    "limits": {}
  }
}
```

`surface` and `linked_authority` are optional. When `surface.kind` is `discord_thread`, `thread_id` is required and Discord identifiers are evidence pointers, not ordering or state authority. `linked_authority.kanban_card_id` means the final council result must be returned to the named Kanban card or a clearly linked follow-up/review card. `linked_authority.vault_decision_note` is a decision-record target, not proof that the note already exists. Optional `turn_mode` is the session-level intended/default floor policy (`relevance`, `targeted`, `random`, `moderator_direct`, or `role_order`). It is not per-turn audit evidence; each actual floor grant records its own `speaker_selected.payload.selection_mode`. Optional `limits.max_discussion_turns` is the explicit council participant discussion-turn limit; legacy `limits.max_turns` is not reinterpreted as lifecycle enforcement.

When `limits.max_discussion_turns` is configured, the derived council discussion lifecycle is:

- T0 moderator opening from the first moderator `hand_raise_requested` discussion-opening event.
- T1..Tmax selected participant discussion speeches with `payload.turn <= max_discussion_turns`.
- One selected participant closeout speech per council member with `payload.turn > max_discussion_turns`.
- Final moderator conclusion from terminal `council_finalized` or `council_unresolved`.

The expected visible turn total is `max_discussion_turns + participant_count + 2`. `council.propose` must fail closed until T0, the participant discussion window, and one closeout speech per member are present. `council.unresolved` remains available as a fail-closed terminal path. Lifecycle accounting does not repair selected-runner accounting or visible delivery proof.

### Surface rendering evidence contract

Visible rooms such as Discord threads are projections of durable KAN events. They help humans follow a council, but they are not lifecycle state authority. A renderer, transcript command, export command, or plugin-visible helper must build its view from cursor-ordered `channel.jsonl` events and may attach external message ids only as evidence pointers.

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
| Final visible result | `council_finalized` | `payload.final_summary`, `payload.consensus`, optional `payload.surface_evidence`, and required `payload.linked_authority_result` when linked authority was configured. |
| Unresolved visible result | `council_unresolved` | Records the durable unresolved outcome; a visible unresolved notice must point back to this event rather than inventing a final decision. |

Control status/export expose the derived `discussion_lifecycle` object. Export bundle `summary_turn_accounting` rows keep existing turn/member/event id fields and add `lifecycle_stage`, `visible_turn_index`, and `visible_turn_total` when a speech contributes lifecycle evidence.

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
Canonical command: `kkachi-agent-network cancel`.

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

Canonical command: `kkachi-agent-network block`.

Compatibility command path: `kkachi-agent-network delegate block` for delegation sessions.

`session_blocked` is the common manual block event used by both delegation and council sessions. It is distinct from daemon-originated operational events (`session_budget_exceeded`, `escalation_timeout`, session-scoped `security_violation`) which may also move a session into a blocked state per `13-operational-contracts.md`. Manual `session_blocked` transitions the session to `blocked`; the envelope `phase` is `blocked` and the payload records both `prior_phase` and `resume_phase`, even when they are the same value.

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

Canonical command: `kkachi-agent-network resume`.

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
Created by: `kkachi-agent-network delegate new`.

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
Canonical command: `kkachi-agent-network delegate ack`.

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
Canonical command: `kkachi-agent-network delegate clarify`.

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
Canonical command: `kkachi-agent-network delegate answer-clarification`.

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
Canonical command: `kkachi-agent-network delegate message`.

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
Canonical command: `kkachi-agent-network delegate request-update`.

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

- `participant_cli`: emitted by `kkachi-agent-network delegate escalate` when the escalation is immediate and not batched.
- `daemon_after_cli`: emitted by the daemon as the deterministic result of `kkachi-agent-network delegate escalation-flush`.
- `daemon_internal`: emitted by daemon runtime policy when a pending batch is flushed by timer expiry, higher-urgency pressure, startup reconciliation, or phase-change pressure.

Command paths:

- `kkachi-agent-network delegate escalate`
- `kkachi-agent-network delegate escalation-flush`

Daemon-internal flushes do not require a public write command but must include `causation_event_id` pointing to the pending batch or policy-triggering event.

When `user_escalation_requested` is emitted by a daemon-internal or daemon-after-CLI batch flush, `from` may be `kkachi-agent-networkd`. When it is emitted by an immediate moderator escalation, `from` is the authorized participant principal, normally `agent-mod`.

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
  "from": "kkachi-agent-networkd",
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
Canonical command: `kkachi-agent-network delegate escalation-delivered`.

`causation_event_id` points to the originating `user_escalation_requested` event.

Emitted by the moderator runtime (not by the daemon) after the Hermes plugin/gateway helper or equivalent Hermes gateway skill has actually delivered the escalation to the user. The daemon records this event through the normal typed KAN command path, with the CLI as the canonical fallback; the daemon itself never opens an outbound notification channel.

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
Canonical command: `kkachi-agent-network delegate resolve-escalation`.

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
Canonical command: `kkachi-agent-network delegate update`.

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
Canonical command: `kkachi-agent-network delegate submit`.

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

CLI commands may accept source artifact paths, but persisted `work_submitted` events reference only ingested artifact records (`artifact_id`, `stored_path`, `sha256`, `size_bytes`, `mime`). The daemon must not persist arbitrary source paths in `work_submitted.payload.artifacts`. The artifact ingestion contract is defined in `05-storage-schema.md` and `12-security.md`.

### review_requested

Origin class: `participant_cli`.
Canonical command: `kkachi-agent-network delegate review`.

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

`target_artifacts` references previously ingested artifact records by `artifact_id` and `stored_path`; full artifact metadata lives in the `artifacts` projection (see `05-storage-schema.md`).

### review_clarification_requested

Origin class: `participant_cli`.
Canonical command: `kkachi-agent-network delegate review-question`.

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
Canonical command: `kkachi-agent-network delegate review-answer`.

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
Canonical command: `kkachi-agent-network delegate review-submit`.

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
Canonical command: `kkachi-agent-network delegate revise`.

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
Canonical command: `kkachi-agent-network delegate accept`.

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

These events are emitted by `kkachi-agent-networkd` itself rather than by a participant (origin class `daemon_internal` unless noted), but they remain session-scoped: every operational event carries the `session_id`, `session_type`, and `phase` of the affected session. Pre-session failures (registry load failure, daemon start failure) are recorded in the daemon operational log, not in `channel.jsonl`.

### session_budget_exceeded

Origin class: `daemon_internal`.

This event transitions the session to `blocked`; the envelope `phase` is `blocked` and the payload records `prior_phase` and `resume_phase` so that recovery can return the session to its prior phase after `limits_extended`.

For `max_runner_calls`, this event is emitted **before** launching the next runner invocation (pre-dispatch check). For `max_tokens_total` and `max_usd`, it is emitted **after** a terminal runner event with parsed cost updates the observed counters (post-event check). See `13-operational-contracts.md` §3 (Session budget limits).

```json
{
  "type": "session_budget_exceeded",
  "from": "kkachi-agent-networkd",
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
  "from": "kkachi-agent-networkd",
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
Canonical command: `kkachi-agent-network limits extend`.

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
  "from": "kkachi-agent-networkd",
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

Emitted by `kkachi-agent-networkd` immediately before launching a bounded runner adapter subprocess. This is the durable accounting root for `runner_calls_total`.

```json
{
  "type": "runner_invocation_started",
  "from": "kkachi-agent-networkd",
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
  "from": "kkachi-agent-networkd",
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
Canonical command: `kkachi-agent-network delegate escalation-delivery-failed`.

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
  "from": "kkachi-agent-networkd",
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
  "from": "kkachi-agent-networkd",
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

A session-scoped violation that transitions the session to `blocked` carries envelope `phase: blocked` and payload `prior_phase`/`resume_phase`. Pre-session violations are written to `operational.log` (per `12-security.md`) and do **not** carry session `phase`/`status`/`prior_phase`/`resume_phase`.

```json
{
  "type": "security_violation",
  "from": "kkachi-agent-networkd",
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
  "from": "kkachi-agent-networkd",
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

- `daemon_after_cli`: emitted when `kkachi-agent-network delegate escalate --urgency low` is accepted into a pending batch.
- `daemon_internal`: emitted when daemon policy updates, flushes, cancels, or rate-limits an existing batch.

`escalation_batched` is never a user-facing escalation and never enters `waiting_user`.

```json
{
  "type": "escalation_batched",
  "from": "kkachi-agent-networkd",
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
  "from": "kkachi-agent-networkd",
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
  "from": "kkachi-agent-networkd",
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
  "from": "kkachi-agent-networkd",
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
- `stream_cursor_acknowledged` — `daemon_after_cli` (created by `kkachi-agent-network stream ack`)
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
Canonical command: `kkachi-agent-network council request-attendance`.

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

- `participant_cli`: the member records attendance explicitly through `kkachi-agent-network council attend`.
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

When the daemon emits timeout attendance, the event uses `from: "kkachi-agent-networkd"`, `to: ["agent-mod"]`, and `payload.member` records the affected member. Timeout payloads must preserve `status: "no_response_timeout"` and timeout source evidence so they remain distinguishable from participant success or partial-success records.

For `surface.kind=discord_thread`, `preparation_requested` is valid only after one terminal `member_attended` record exists for every required participant named by the council membership/attendance request. Terminal attendance status is one of `present`, `partial`, `unavailable`, or `no_response_timeout`. Missing attendance records, duplicate unresolved attendance state, or attendance for only a subset of required participants must fail closed at append time for `preparation_requested`.

Before appending `preparation_requested` for a Discord-thread council, the daemon
must apply expired attendance timeouts and then fail closed unless required
participant runtime readiness is explicit: subscriber presence, valid cursor ack,
fresh cursor ack, fresh heartbeat, and attendance response/timeout evidence. A
failure leaves no `preparation_requested` event; timeout diagnostics remain
durable `member_attended` events.

### agenda_locked

Origin class: `participant_cli`.
Canonical command: `kkachi-agent-network council lock-agenda`.

`agenda_locked` freezes the decision question before substantive preparation/discussion. Topic-drift policy, final summaries, and Kanban/Vault return paths must refer back to this locked agenda. When `surface.kind` is `discord_thread`, this event is mandatory before `preparation_requested`.

```json
{
  "type": "agenda_locked",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "phase": "created",
  "payload": {
    "decision_question": "Decide next action for Kanban card t_xxxxx.",
    "out_of_scope_policy": "New topics become follow-up card candidates, not current-thread expansion.",
    "max_rounds": 2
  }
}
```

### preparation_requested

Origin class: `daemon_after_cli`.
Created by: `kkachi-agent-network council prepare`.

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
Canonical command: `kkachi-agent-network council ready`.

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

Canonical command for participant-originated partial preparation: `kkachi-agent-network council prepared-partial`.

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
  "from": "kkachi-agent-networkd",
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
- When the daemon emits `member_prepared_partial` because the preparation timeout expired, the event originator is `kkachi-agent-networkd`, the affected member is recorded in `payload.member`, and `payload.reason` is `timeout`.
- Before appending `hand_raise_requested` for a Discord-thread council, the daemon must apply expired preparation timeouts and then fail closed unless required participant runtime readiness remains explicit and every required participant has preparation success or partial/failure evidence. Timeout diagnostics remain durable `member_prepared_partial` events and must not be rewritten as success.

### hand_raise_requested

Origin class: `daemon_after_cli`.
Created by: `kkachi-agent-network council poll`.

```json
{
  "type": "hand_raise_requested",
  "from": "agent-mod",
  "to": ["agent-1", "agent-2", "agent-3"],
  "payload": {
    "turn": 5,
    "research_timeout_sec": 600,
    "question": "Who has a material objection or additional evidence?"
  }
}
```

### hand_raise

Origin class: `participant_cli`.
Canonical command: `kkachi-agent-network council hand-raise`.

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

### speaker_selected

Origin class: `participant_cli`.
Canonical command: `kkachi-agent-network council grant`.

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

`selection_mode` and scoring rules are normative in `07-moderator-policy.md`.

For Discord-thread councils, `selection_mode` may also be `moderator_direct` or `role_order` per `07-moderator-policy.md`. These modes still use `speaker_selected` as the durable floor-grant record. If the session has `session_created.payload.turn_mode`, that value is only the intended/default policy. The durable per-turn audit fact is this event's `payload.selection_mode`. A per-turn `selection_mode` may deviate from `turn_mode` only when `payload.reason` is present and names the operational reason for the deviation.

### speech

Origin class: `participant_cli`.
Canonical command: `kkachi-agent-network council speak`.

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
Canonical command: `kkachi-agent-network council intervene`.

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

- `kkachi-agent-network council propose`
- `kkachi-agent-network council revise`

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
Canonical command: `kkachi-agent-network council request-vote`.

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
Canonical command: `kkachi-agent-network council vote`.

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
Canonical command: `kkachi-agent-network council finalize`.


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
      "kind": "discord_thread",
      "thread_id": "1507515847227215932",
      "final_message_id": "optional"
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

`surface_evidence` records visible-room delivery evidence for the final result. It is optional because a council may finalize before a moderator/Gray workflow posts the final summary to the visible room. When present, a `final_message_id` or equivalent pointer is evidence that the final summary was posted; it is not the source of the final decision. If a visible moderator closeout is required, `council_finalized` without posted `surface_evidence` or an equivalent projection/export pointer is durable finalization only, not visible UX success.

The daemon/replay must not create Kanban comments, Vault notes, or visible-room messages directly. Absence of posted evidence, or status `failed`/`pending_followup`, means the origin authority path remains blocked/pending review or must be represented by a linked follow-up; final reports must not claim linked authority return or visible delivery is complete.

### council_unresolved

Origin class: `participant_cli`.
Canonical command: `kkachi-agent-network council unresolved`.
