# Session State Machines

## Phase versus status

`phase` is the exact lifecycle state used by domain logic and stored in every event envelope. `status` is a derived roll-up (`open`/`blocked`/`terminal`) used for query, UI, and active-session lock checks; it is stored only in projections (`session.yaml`, SQLite). See `05-storage-schema.md#phase-and-status` for the deterministic mapping and `13-operational-contracts.md` §5 for the concurrency contract.

Every event envelope records the **post-transition** `phase`. For example, when `clarification_requested` moves a delegation session from `working` to `needs_clarification`, the event envelope has `phase: "needs_clarification"`. When `session_budget_exceeded` blocks a session, the event envelope has `phase: "blocked"` and the payload records `prior_phase` and `resume_phase`.

State transitions are driven by event type and phase, not by recipient visibility alone. The event `to` field is semantic addressing (per `03-protocol-spec.md`); it helps runtimes decide whether to act, but it is not a state-machine transition guard by itself.

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

The active-session lock is released only when the session reaches a terminal phase. The normative concurrency rule (what counts as "active", how `blocked` interacts with the lock, how future releases may extend this without breaking Release v1) lives in `13-operational-contracts.md` §5 Concurrency model.

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

`max_runner_calls` is checked before launching another runner invocation; token and USD limits are checked after terminal runner events update observed cost totals (see `13-operational-contracts.md` §3).

Escalation timeout can enter `blocked` only after a `user_escalation_requested` exists. Pending low-urgency batches do not by themselves time out into `blocked`; they may flush into `user_escalation_requested`, be cancelled, or be rate-limited.

Exit conditions:

- Budget or limit block resolved by `limits_extended` with user authorization → return to the recorded `resume_phase` (typically `working` or `under_review`).
- Manual, external-dependency, scope-conflict, or process block resolved by `session_resumed` → return to the recorded `resume_phase`.
- Security block resolved by verified remediation and an allowed `session_resumed` path → return to the recorded `resume_phase`.
- User cancels → `cancelled`.

`session_resumed` lifts manual, external-dependency, scope-conflict, policy/process, and (when remediable) security blocks. `limits_extended` lifts only budget or limit blocks recorded by `session_budget_exceeded`. The lifting event must reference the original blocking event.

Only `accepted` and `cancelled` are truly terminal for delegation sessions.

Phase summary (mapped to `status` per `05-storage-schema.md#phase-to-status-mapping`):

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

This avoids adding a new `attendance` phase in the initial spec alignment. For `surface.kind=discord_thread`, the attendance/agenda sequence is mandatory before `preparation_requested`. `council prepare` fails closed while remaining in `created` if `attendance_requested` is missing, any required participant lacks a terminal `member_attended` record (`present`, `partial`, `unavailable`, or `no_response_timeout`), or `agenda_locked` is missing. If later review chooses a true `attendance` phase, `03-protocol-spec.md`, `04-cli-spec.md`, this state machine, storage projections, replay migrations, and acceptance tests must be updated together.

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

Runner budget breach examples that can enter `blocked`: `session_budget_exceeded` with `limit_kind` of `max_runner_calls`, `max_tokens_total`, `max_usd`, or `max_elapsed_sec`. `max_runner_calls` is checked before launching another runner invocation; token and USD limits are checked after terminal runner events update observed cost totals (see `13-operational-contracts.md` §3).

`blocked` is **not** terminal.

Exit conditions:

- `limits_extended` with user authorization → return to the recorded `resume_phase` when the block was a budget or limit breach.
- `session_resumed` (recorded by `hun resume`) → return to the recorded `resume_phase` for explicit moderator blocks, external-dependency blocks, scope-conflict blocks, or policy/process blocks. `session_resumed` may also lift verified security blocks where the security model defines the violation as remediable.
- the moderator cancels the session → `cancelled`.

`limits_extended` lifts only budget or limit blocks. `session_resumed` lifts manual or external-dependency blocks; it must not be used for budget or limit blocks recorded by `session_budget_exceeded`.

### finalized

A consensus decision exists.

If the council has `linked_authority`, `finalized` means the council decision has been recorded, not necessarily that Kanban/Vault authority return is complete. Completion of the return path depends on `linked_authority_result.status: posted` plus evidence. `failed` or `pending_followup` status keeps the origin authority path blocked/pending review or requires a linked follow-up outside the council state machine.

### unresolved

The council did not reach consensus.

## No-hand-raise policy

The normative no-hand-raise policy lives in `07-moderator-policy.md`. The state machine only observes the outcome of that policy: a return to a new hand-raise round, a transition to `draft_conclusion`, or a transition to `unresolved`. Speaker selection decisions (targeted question, role-relevant pick, random fallback) are policy, not state, and must not be duplicated here.
