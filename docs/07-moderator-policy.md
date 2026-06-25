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
- Visible speech/final-result rendering follows `03-protocol-spec.md#surface-rendering-evidence-contract`: render from cursor-ordered durable events first, then attach Discord/Kanban/Vault ids only as delivery evidence pointers.

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

This is the single normative source for no-hand-raise handling. The state machine (`06-state-machine.md`) and acceptance tests (`08-acceptance-tests.md`) defer here.

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
