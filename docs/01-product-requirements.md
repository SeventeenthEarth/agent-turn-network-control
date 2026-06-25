# Product Requirements

## Goal

Build a full target system for real-profile Hermes agent communication using a daemon, ATN protocol client/contract, minimal canonical CLI, and Hermes plugin integration layer, without changing Hermes core.

## Primary user stories

### Delegation

The user asks the moderator to assign work to agent-1. The moderator creates a delegation session, sends the task, receives agent-1's acknowledgement, answers clarifying questions, monitors progress, reviews submitted output, requests revisions if needed, and reports completion to the user.

### Council

The user asks the moderator to discuss topic A with agent-1, agent-2, and agent-3. The moderator creates a council session, gives members time to prepare, moderates turn-taking, asks for consensus on a draft conclusion, and reports the final decision or unresolved state to the user.

## Functional requirements

### Global session rule

The system must allow only one active session at a time across delegation and council types. Normative semantics (which states count as active, how `blocked` interacts with the lock, exit conditions, and the forward-compatibility rules for future multi-session work) are defined in `13-operational-contracts.md` §5 Concurrency model. State exit conditions are enumerated in `06-state-machine.md`.

In summary, `open` and `blocked` sessions both count as active. Only `terminal` sessions release the single-active-session lock.

### Common session requirements

- Every session must have an ID, type, title/topic, participants, state, timestamps, and limits.
- All durable events must be appended to `channel.jsonl`.
- SQLite must be a projection, not the source of truth.
- Full transcript export must be available for every session.
- The daemon must expose a replayable stream/watch interface so moderator and member runtimes can observe session events in near real time; the CLI and Hermes plugin both consume this through the ATN protocol client/contract.
- Stream delivery must be replayable from a durable cursor; disconnects must resume without losing events.
- All agent-originated writes must still go through typed ATN command models validated by the daemon. Hermes plugin tools should call the ATN protocol client/contract directly; the CLI remains the canonical fallback path. No integration may mutate daemon state directly or append to `channel.jsonl` outside the daemon.
- Every participant-originated event must have an explicit canonical CLI command path (see `04-cli-spec.md` event-to-command coverage matrix) and, when exposed through the Hermes plugin, a documented plugin tool/slash-command equivalent that maps to the same command model.
- Every state-mutating CLI command must declare the protocol event type or event sequence it emits.
- Ambiguous state-mutating commands are not allowed; clarification answers, general delegation messages, review verdicts, escalation delivery reports, and council readiness must each have explicit commands.
- Every session has an exact lifecycle `phase` and a derived roll-up `status`. `phase` is used for state transitions; `status` is used for UI, query, and active-session lock checks. Allowed `status` values are `open`, `blocked`, and `terminal`. The active-session lock is held whenever `status != terminal`.
- Every event has a single originator (`from`) and zero or more semantic recipients (`to`). `from` is always a string; `to` is always an array of strings. Broadcast events must enumerate their recipients explicitly. `to` is not stream access control; authorized session participants can observe the session stream and decide whether to act.
- Manual, external-dependency, and council blocks must be recoverable through a common `session_resumed` event recorded by `atn-control resume`.
- Budget and limit blocks must be recovered through `limits_extended`, not through `session_resumed`.

### Delegation requirements

- The moderator can assign work to one primary assignee.
- Optional reviewers can be added as quality gates.
- The assignee must acknowledge the task through `delegate ack` and may ask clarifying questions.
- The assignee can record progress updates, blockers, partial results, and completion submissions.
- The moderator can answer questions, redirect scope, request revisions, or mark blocked.
- The moderator must answer clarification requests through `delegate answer-clarification`, not through a generic message command. `delegate message` is for general delegation messages only (`delegation_message` event), not for `clarification_answered`.
- The reviewer must submit a review verdict through `delegate review-submit`.
- The moderator runtime must record escalation delivery success or failure through `delegate escalation-delivered` and `delegate escalation-delivery-failed`.
- If a clarification needs the user's authority, the moderator must escalate the question to the user. The daemon records the escalation; the moderator runtime, using the Hermes plugin/gateway surface helper or an equivalent Hermes gateway skill, performs the actual delivery (origin Hermes session, Telegram, Slack, Discord, etc.) and writes back the delivery result. The daemon must not require gateway tokens.
- Completion requires a moderator acceptance event.
- Review findings may trigger revision loops.

### Review gate requirements

- Review is a quality gate inside a delegation session.
- The reviewer may ask clarification questions directly to the assignee through the session channel.
- The moderator coordinates the exchange but should not answer assignee-owned implementation questions on the assignee's behalf.
- The assignee should answer reviewer questions, provide evidence, or acknowledge required changes.
- The moderator intervenes when the reviewer-assignee exchange stalls, goes off scope, becomes circular, or requires user authority.
- Reviewer questions that change product scope, priorities, risk acceptance, or user policy must be escalated by the moderator to the user.

### User escalation requirements

- The daemon records when a member question is escalated to the user. It does not deliver the question itself.
- Escalations must include source member, question, context, options, urgency, the moderator's recommendation when available, and a `delivery_policy` hint (`origin`, `telegram`, `origin_or_telegram`, `both`, or any other gateway label the moderator's skill understands).
- **Delivery is the moderator runtime's responsibility, not the daemon's.** The moderator uses its Hermes gateway skill (Telegram/Slack/Discord/origin Hermes session/etc.) to actually reach the user, then writes `user_escalation_delivered` back through the CLI to record what was delivered where. The daemon never opens an outbound notification channel of its own.
- The `delivery_policy` field is an instruction to the moderator skill, not to the daemon. The moderator may follow, override, or downgrade it (e.g. fall back from `telegram` to `origin` if its Telegram gateway is unreachable) and must record the actual delivery target.
- After `user_escalation_requested` is recorded, the session enters `waiting_user` and remains there until `user_escalation_resolved` is recorded, the escalation is cancelled, or the session is blocked/cancelled by another valid transition.
- Low-urgency escalation candidates may be batched before `user_escalation_requested` is emitted. While only `escalation_batched` exists, the session remains in its prior phase. `escalation_batched` is an audit event for a pending batch; it is not user delivery, does not enter `waiting_user`, and does not increment `user_escalations_total`. When the batch is flushed, the daemon records a single `user_escalation_requested` event that bundles the batched questions; that event is what enters `waiting_user`.
- Batching changes only when a user-facing escalation request is created. It does not change the delivery responsibility split: the daemon still never opens an outbound notification channel, and the moderator runtime delivers only after observing the final `user_escalation_requested` event and then writes back `user_escalation_delivered` or `user_escalation_delivery_failed`.
- After the user answers, the moderator must relay the resolved clarification back to the assignee and continue the delegation session.

### Council requirements

- On council start, the daemon records and broadcasts the topic to every member runtime through the session stream. The corresponding event uses an explicit `to` recipient list containing the council members; it must not use `all` or an omitted recipient field.
- A council may include optional `surface` metadata. For `surface.kind: discord_thread`, the Discord thread is a human-visible room only; the canonical session state remains `channel.jsonl`.
- A council may include optional `linked_authority` metadata. When `linked_authority.kanban_card_id` is present, the final result must be returned to that card or to a clearly linked follow-up/review card. When `linked_authority.vault_decision_note` is required, Gray/Gongmyeong records the decision note after the council.
- Discord-thread councils must support an explicit attendance check through typed events before preparation starts. The first-pass spec keeps attendance inside the `created` phase unless Gongmyeong/user review chooses a new `attendance` phase.
- Discord-thread councils must support an explicit agenda lock before substantive discussion. The locked agenda is the decision question used for topic-drift control and final outcome evaluation.
- Each member runtime gets up to 10 minutes for initial research and records `member_ready` (through `council ready`) or `member_prepared_partial` (through `council prepared-partial`) through typed ATN commands exposed by the plugin protocol client or canonical CLI fallback. The daemon also emits `member_prepared_partial` internally when preparation timeout expires.
- The moderator requests consensus voting through `council request-vote`, which emits a stream-visible `consensus_vote_requested` event; member runtimes must not vote until they observe it (or its replay).
- Timed-out members are not excluded; they proceed with material prepared so far.
- Before each turn, the daemon records `hand_raise_requested`; member runtimes observe it through the stream and decide whether to raise a hand.
- During hand raise, members may research or fact-check for up to 10 minutes before submitting `hand_raise`.
- A member that raises a hand must be ready to speak immediately if selected.
- The previous speaker may not speak on the next turn.
- The moderator may grant floor explicitly with `moderator_direct` or `role_order` selection modes for Discord-thread councils; this is recorded through `speaker_selected`, not inferred from Discord message order.
- A draft conclusion requires a consensus vote from every member runtime.
- A single `block` prevents finalization.
- The daemon must not depend on one-shot subprocess dispatch as the primary council participation model.

## Engineering requirements

Implementation must follow `10-engineering-principles.md`.

Required baseline:

- Clean Architecture and SRP are mandatory.
- Maintainability, extensibility, and performance must be considered in design decisions.
- Prefer optimal changes that fix root causes over minimal symptom patches.
- Fallback behavior requires explicit user approval or a defined product contract.
- Invalid state must fail closed and be recorded clearly.

## Operational requirements

- CLI commands talk to the daemon.
- If Hermes is running, the daemon should also be spawned and kept alive without Hermes core changes.
- The daemon must fail closed if the registry is missing or invalid.
- Member profile invocations must preserve session continuity through session IDs.
- Member runtimes must preserve their own stream cursor and member AI session handle across reconnects.
- The daemon may provide local SSE, WebSocket, Unix socket, or local HTTP internally, but the stable agent-facing interface is the plugin protocol-client stream plus typed ATN commands, with the CLI stream and canonical CLI command paths as the diagnostics/recovery/manual fallback.
- The system must not restart or alter unrelated team member gateways.
- The system must expose operational health, metrics, and structured errors sufficient for local monitoring and debugging. See `16-observability.md`.
- The system must document backup, restore, corruption recovery, and projection rebuild procedures. See `17-disaster-recovery.md`.
- The system must support setup and diagnostic commands (`atn-control init`, `atn-control doctor`, `atn-control storage verify`, `atn-control storage rebuild-projection`) without weakening the security model. See `04-cli-spec.md` and `12-security.md`.

## Distribution requirements

- The repository README must be written so a user can ask Hermes Agent to install `atn-control` from GitHub and Hermes can complete the setup without hidden context.
- The Go build must expose both `atn-control` and `atn-controld` binaries.
- The data home must resolve in the order `$ATN_HOME` > `$XDG_DATA_HOME/agent-turn-network` > `~/.atn/`.
- The companion plugin repository must include a bundled Hermes skill that teaches Hermes how to operate `atn-control` safely through the daemon/CLI contract.
- The Release v1 skill must cover delegation, review gate, council preparation/discussion/consensus, transcript, user escalation (delivery policy and rate limits), daemon status checks, budget and limit extensions, and fail-close behavior; the plugin repository owns the skill implementation, while this repository owns the daemon/CLI contract it documents (see `11-distribution-and-plugin.md`).
- Installation docs must distinguish code install, data home, registry setup, daemon setup, and optional launchd integration.

## Default limits

Process limits (full set in `13-operational-contracts.md`):

- `max_turns`: 300 for council discussions
- `max_consensus_rounds`: 20 for council consensus (hard cap; the moderator should normally resolve far earlier)
- `consensus_round_warning_threshold`: 3 (soft control, moderator policy guidance, see `07-moderator-policy.md`)
- `no_progress_round_limit`: 3 (soft control, moderator policy guidance, see `07-moderator-policy.md`)
- `preparation_timeout`: 10 minutes
- `hand_raise_research_timeout`: 10 minutes
- `delegation_update_timeout`: configurable per task
- `dispatch_timeout_sec`: 180 per runner call
- `clarification_response_timeout_sec`: 3600
- `escalation_response_timeout_sec`: 86400
- Single active session only

Budget limits:

- `max_runner_calls`: 500 per session
- `max_tokens_total`: 2,000,000 per session
- `max_usd`: 25.00 per session
- `max_elapsed_sec`: 86,400 per session

Budget breaches emit `session_budget_exceeded` with envelope `phase: blocked`, payload `prior_phase` and `resume_phase`, and the projected session `status` becomes `blocked`. Extending a budget requires explicit user authorization recorded as `limits_extended`; the session then returns to `resume_phase`.

Runner call accounting is independent from cost parsing. A runner invocation counts against `max_runner_calls` once the daemon records `runner_invocation_started`; an invocation with missing parsed cost (`cost: null`) still counts. Parsed token and USD totals are used for `max_tokens_total` and `max_usd`. If cost is missing, token and USD totals remain incomplete and the session surfaces the missing-cost count in status, transcript, and export.

For `max_runner_calls`, the daemon checks **before** launching the next runner invocation. For `max_tokens_total` and `max_usd`, the daemon checks **after** terminal runner events with parsed cost update the observed totals.

Escalation limits (normative policy in `07-moderator-policy.md`):

- `max_user_escalations`: 10 per session
- Duplicate-question debounce: 10 minutes
- Low-urgency batching window: 30 minutes

Escalation cap breaches emit `escalation_rate_limited` and block further escalation delivery. They do not by themselves move the session to `blocked`; the session stays in its prior phase (typically `working` or `waiting_user`) until the user authorizes `limits_extended` or the moderator accepts, cancels, or blocks the session through other means.

Low-urgency batching window starts when the first `escalation_batched` event for a batch is appended. User response timeout (`escalation_response_timeout_sec`) starts when `user_escalation_requested` is appended, not when `escalation_batched` is appended. Only `user_escalation_requested` increments `user_escalations_total`; `escalation_batched` does not count against `max_user_escalations` until it is flushed into `user_escalation_requested`.

## Release v1 scope

Release v1 delivers delegation sessions, the review gate, and council sessions (preparation, hand-raise discussion, and the consensus gate). The full specification in this docs directory is the product vision; Release v1 is implemented through sequential Implementation Phases and must not introduce contracts that would block future releases (envelope must be versioned, adapter interface must remain pluggable, scoring weights must be externally configurable). Release v1 ships exactly one runner adapter (`hermes-agent`); other adapter kinds are Future release candidates. See `09-implementation-epics.md` and `13-operational-contracts.md` for scope boundaries.
