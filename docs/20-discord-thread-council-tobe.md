# TOBE: Discord Thread Council Surface

## Status

- Status: TOBE source plus alignment record; canonical implementation rules now live in the main protocol, CLI, state-machine, storage, policy, operational-contract, acceptance-test, and epic docs.
- Scope: `atn-control` council UX on Discord threads
- Source request, preserved as SOT: `discord에서는 이런 토론이 하나의 쓰레드 안에서 진행되었으면 좋겠어. 내가 "토론을 시작해줘"하면 네가 쓰레드에 장수들을 한명씩 불러서 출석 체크를 한 다음에 토론이 진행되면서 공명이 발언권을 주고 토론을 체계적으로 수향하는거지.`
- Operating language: user-facing reports to 주군 are Korean; agent-facing docs and implementation notes remain English while preserving fixed Korean labels such as `17번째 지구`, `주군`, and member names.
- Governance correction: ATN is the public product/repository naming for the bounded ATN control/plugin lane. Prior Hwangcat-routed work is historical draft/spec-prep evidence only, not durable current ownership. Current ATN lane ownership is 마초/서황/종회/만총 for Blue/Red/Orange/Gray respectively; Gongmyeong/wolong may coordinate Kanban routing without becoming Blue.


## Spec alignment record

This document preserves the original Discord-thread council TOBE request and the decisions that were folded into the canonical ATN docs. Implementers should treat this file as UX/background context; when details differ, the canonical implementation spec is `03-protocol-spec.md`, `04-cli-spec.md`, `05-storage-schema.md`, `06-state-machine.md`, `07-moderator-policy.md`, `08-acceptance-tests.md`, `09-implementation-epics.md`, and `13-operational-contracts.md`. The first-class alignment is additive:

1. Keep `channel.jsonl` as the canonical council SOT and Discord thread as the human-visible surface only.
2. Add optional council `surface` and `linked_authority` metadata to `session_created.payload` and session projections.
3. Represent attendance and agenda lock as typed council events while the session remains in `created`, rather than adding a new `attendance` phase for the first pass.
4. Extend `speaker_selected.payload.selection_mode` with `moderator_direct` and `role_order` so Gongmyeong can grant floor explicitly in a Discord-thread council.
5. Keep Discord delivery outside `atn-controld`; the moderator runtime posts to Discord through Hermes plugin/gateway capability and records delivery evidence as metadata or follow-up delivery audit.
6. If `linked_authority.kanban_card_id` is present, final council outcome must be returned to the Kanban card; Vault decision-note recording remains a Gray/Gongmyeong workflow when the topic is durable architecture/process/command knowledge.

Aligned spec sections:

- `docs/README.md`: add source-of-truth and decision-log entries for Discord-thread council surface binding.
- `docs/00-overview.md`: describe Discord-thread council as an optional surface, not a replacement architecture.
- `docs/01-product-requirements.md`: add council requirements for surface binding, attendance, agenda lock, floor grants, and Kanban/Vault return path.
- `docs/02-architecture.md`: preserve Clean Architecture by keeping Discord transport outside engine/domain and outside `atn-controld` delivery responsibility.
- `docs/03-protocol-spec.md`: add `surface`/`linked_authority` metadata, `attendance_requested`, `member_attended`, `agenda_locked`, and final outcome linkage fields.
- `docs/04-cli-spec.md`: add additive flags/commands and event-to-command rows.
- `docs/05-storage-schema.md`: add optional projected session fields for `surface` and `linked_authority`.
- `docs/06-state-machine.md`: document the no-new-phase first-pass choice and allowed event ordering.
- `docs/07-moderator-policy.md`: add Discord-thread attendance, agenda-lock, role-order, and divergence controls.
- `docs/08-acceptance-tests.md`: add Discord-thread council acceptance scenario.
- `docs/09-implementation-epics.md`: add an implementation epic for Discord-thread surface binding after the existing core council engine.
- `docs/12-security.md`: state token/thread validation boundary and forbid raw Discord tokens in daemon/event payloads.
- `docs/13-operational-contracts.md`: state replay/idempotency behavior for surface metadata and final return-path evidence.

Resolved for first implementation: attendance remains a typed subflow inside `created`; a later true `attendance` phase would require a coordinated spec/replay/storage migration. Blue/Red/Orange/Gray ownership is the current internal review lane assignment; it is not a public ATN product alias.

## Executive summary

The current ATN documentation defines an event-sourced daemon, ATN protocol client/contract, minimal canonical CLI, preferred Hermes plugin integration layer, `council` sessions, durable event streams, real Hermes profile participants, and a strict non-goal of modifying Hermes core. The Discord-thread TOBE should therefore **not replace the core ATN architecture**. It should add a first-class **Discord thread council surface** on top of the existing event-sourced session model; Discord is not a state authority.

Target model:

```text
Discord thread = human-visible council room
channel.jsonl = canonical session SOT
atn-controld = state machine, event log, locks, replay, transcript/export
Hermes plugin = preferred agent-facing ATN tools/slash commands and Discord visible-post helper
Gongmyeong/wolong = possible moderator runtime and user-facing Korean reporter when assigned
member profiles = real 장수 participants
Kanban/Vault = task authority, final decision, traceability
```

The implementation should preserve the existing principle:

```text
Do not modify Hermes core.
Do not make Discord transcript the SOT.
Do not let free-form multi-agent Discord replies drive state directly.
```

## Current documented state

The existing docs define these reusable foundations:

- `atn-control` CLI and `atn-controld` daemon.
- `session_type: council` with preparation, hand-raise discussion, draft conclusion, consensus vote, and finalized/unresolved states.
- `channel.jsonl` as durable source of truth, SQLite as projection.
- `atn-control stream` as the stable member runtime observation interface.
- Real Hermes member profiles, not simulated prompt personas.
- No Hermes core modification.
- External delivery to Telegram/Slack/Discord/origin is currently the moderator runtime's responsibility, not the daemon's direct responsibility.

The TOBE change is a **surface and orchestration UX addition**, not a full architecture replacement.

## Target user experience

When 주군 writes in a Discord thread:

```text
공명, 토론을 시작해줘.
참여: 마초, 서황, 종회, 만총
안건: 이 Kanban 카드의 다음 행동을 결정하자.
```

Gongmyeong starts a ATN council bound to that exact thread.

Expected visible flow:

```text
[1] Gongmyeong opens the council in the thread.
[2] Gongmyeong announces the decision question, participants, role boundaries, and rules.
[3] Gongmyeong calls one member at a time for attendance.
[4] Each member reports presence in the same thread.
[5] Gongmyeong locks the agenda.
[6] Gongmyeong grants the floor to one member at a time.
[7] Each member speaks only when granted.
[8] Gongmyeong summarizes unresolved issues between rounds.
[9] Optional second round is limited to unresolved issues only.
[10] Gongmyeong drafts a conclusion and requests consensus or marks unresolved.
[11] Final summary is written back to Kanban and/or Vault when configured.
```

The Discord thread should feel like a real council, but every meaningful transition must be represented by typed ATN events.

## Non-goals

- Do not turn arbitrary Discord messages into implicit state transitions.
- Do not let all members free-reply at once.
- Do not use Discord message order as the authoritative state machine.
- Do not require Hermes core patches.
- Do not make MCP or the Hermes plugin a state authority; the Hermes plugin is now the preferred integration surface, but canonical CLI diagnostics/recovery/manual paths must still work.
- Do not give `atn-controld` broad Discord bot-token responsibility; use Hermes plugin/gateway capability for first-pass visible posting unless a later design explicitly approves a Discord bridge component and its security model.
- Do not bypass Kanban review and traceability rules when a council is tied to a work item.

## Surface binding model

Add optional council metadata that binds a session to a Discord thread.

Proposed session metadata:

```yaml
surface:
  kind: discord_thread
  platform: discord
  guild_id: "optional"
  channel_id: "optional parent channel id"
  thread_id: "required when kind=discord_thread"
  origin_message_id: "optional"
  started_by: "user|moderator"
  delivery_owner: "hermes_plugin_or_moderator_runtime"
linked_authority:
  kanban_card_id: "optional"
  vault_decision_note: "optional"
```

Design rules:

1. `surface.kind=discord_thread` means the thread is the human-visible room.
2. `channel.jsonl` remains the canonical SOT.
3. Discord links and message ids are evidence pointers, not state authority.
4. If linked to Kanban, the final council result must be posted back to the same Kanban card or a clearly linked child/review card.
5. If a Vault decision note is required, Gray or Gongmyeong records the final decision and evidence links after the council.

## Council phases for Discord-thread UX

The current council state machine is:

```text
created -> preparation -> discussion -> draft_conclusion -> consensus_vote -> finalized | unresolved | blocked | cancelled
```

For Discord-thread UX, introduce an explicit attendance subflow while preserving the current lifecycle for the first pass.

Recommended first-pass option for Release v1 planning:

```text
created
  -> attendance_requested
  -> member_attended events or timeout records
  -> agenda_locked
  -> preparation
  -> discussion
  -> draft_conclusion
  -> consensus_vote
  -> finalized | unresolved | blocked | cancelled
```

This keeps `attendance_requested` and `member_attended` as typed events while the session remains in `created`, then transitions to `preparation` only after attendance closes and the agenda is locked. The user experience still shows attendance as a separate visible step.

A true `attendance` phase is an explicit open alternative, not the selected first-pass alignment. Choosing it later would require coordinated changes across protocol, CLI, state machine, storage, replay, and acceptance tests.

## Proposed event additions

### `attendance_requested`

Purpose: Gongmyeong begins thread attendance check.

```json
{
  "type": "attendance_requested",
  "from": "wolong",
  "to": ["macho", "seohwang", "jonghoe", "manchong"],
  "payload": {
    "surface_kind": "discord_thread",
    "thread_id": "1507515847227215932",
    "timeout_sec": 300,
    "instructions": "Report present only when called or when the moderator grants attendance turn."
  }
}
```

### `member_attended`

Purpose: A member confirms participation.

```json
{
  "type": "member_attended",
  "from": "macho",
  "to": ["wolong"],
  "payload": {
    "status": "present",
    "summary": "Present. I will evaluate the harness/spec governance boundary.",
    "discord_message_id": "optional"
  }
}
```

Allowed attendance status values:

```text
present
partial
unavailable
no_response_timeout
```

### `agenda_locked`

Purpose: The moderator freezes the decision question and prevents topic drift.

```json
{
  "type": "agenda_locked",
  "from": "wolong",
  "to": ["macho", "seohwang", "jonghoe", "manchong"],
  "payload": {
    "decision_question": "Decide the next action for Kanban card t_xxxxx.",
    "out_of_scope_policy": "New topics become follow-up card candidates, not current-thread expansion.",
    "max_rounds": 2
  }
}
```

### Existing events to keep

The current events remain useful:

```text
preparation_requested
member_ready
member_prepared_partial
hand_raise_requested
hand_raise
speaker_selected
speech
moderator_intervention
draft_conclusion
consensus_vote_requested
consensus_vote
council_finalized
council_unresolved
```

For Discord-thread council, `speaker_selected` should be the durable representation of Gongmyeong granting the floor.

## Turn control model

The visible council must use strict turn control.

Rules:

1. Only the current `speaker_selected.payload.member` may produce a `speech` event.
2. A member may not speak twice in a row unless the moderator records an explicit exception event or intervention.
3. Off-topic speech triggers `moderator_intervention`.
4. New topics are split to follow-up candidates, not debated in the current council.
5. The second round may address only unresolved issues identified by the moderator.
6. The moderator's substantive opinion must be recorded as a separate participant-style turn, distinct from moderation actions.

For the role-ordered council UX, add or document a `grant` mode:

```bash
atn-control council grant <session_id> \
  --to macho \
  --mode moderator_direct \
  --round 1 \
  --reason "Round 1 owner-side spec governance boundary"
```

Proposed `speaker_selected.payload.selection_mode` values:

```text
relevance        # existing scoring-based path
targeted         # moderator seeks a missing perspective
random           # documented tie-break/early exploration only
moderator_direct # explicit moderator floor grant
role_order       # deterministic round-robin by declared role order
```

`moderator_direct` and `role_order` are the best fits for the Discord-thread UX.

## CLI TOBE

Minimal additive CLI changes:

```bash
atn-control council new "<topic>" \
  --members macho,seohwang,jonghoe,manchong \
  --moderator wolong \
  --surface discord-thread \
  --thread-id 1507515847227215932 \
  --kanban-card t_xxxxx \
  --turn-mode role_order

atn-control council attend <session_id> \
  --from macho \
  --status present \
  --summary "Present. Owner-side spec governance perspective ready."

atn-control council lock-agenda <session_id> \
  --decision-question "Decide next action for Kanban card t_xxxxx" \
  --max-rounds 2

atn-control council grant <session_id> \
  --to macho \
  --mode role_order \
  --round 1 \
  --reason "Owner-side spec governance first pass"

atn-control council speak <session_id> \
  --from macho \
  --stdin

# Optional future UX, not part of the minimal first-pass alignment unless
# separately added to the protocol/CLI spec:
# atn-control council close-round <session_id> \
#   --round 1 \
#   --summary-file unresolved-issues.md

atn-control council propose <session_id> --stdin
atn-control council request-vote <session_id>
atn-control council vote <session_id> --from macho --vote approve
atn-control council finalize <session_id>
```

Existing commands should remain backward compatible. The new flags are additive.

## Discord delivery responsibility

Recommended Release v1-compatible approach:

```text
atn-controld records typed events.
Moderator runtime observes events and posts human-visible messages to the Discord thread through Hermes gateway capability.
Member runtimes emit typed events after they act.
Discord delivery audit is recorded as metadata or delivery events when needed.
```

Do not put raw Discord bot token handling into the daemon in the first pass.

If a future Discord bridge is approved, it should be a separate infrastructure component, not domain logic:

```text
engine/session state machine -> no Discord API dependency
transport/bridge -> Discord post/fetch/audit only
security -> token scope, allowed channel/thread validation, redaction
```

## Kanban and Vault return path

When `--kanban-card` is set:

1. The opening council message should name the card id and decision question.
2. The final conclusion must be posted back to the same card as a comment or review summary.
3. If the council creates follow-up items, each must be listed as a candidate or explicitly created through Kanban.
4. If user approval is required, the Kanban card should remain blocked or pending approval rather than silently marked done.
5. Gray/Vault notes should record durable decisions when the topic affects architecture, process, command structure, or long-term operations.

Recommended final Kanban comment shape:

```text
ATN council finalized in Discord thread <thread link>.
Decision: ...
Consensus: approve / approve_with_conditions / unresolved
Participants: ...
Key reasons:
- ...
Follow-up candidates:
- ...
Vault note: <path if created>
```

## Divergence control

This is the core difference between a simple round-robin moderator and ATN.

Mandatory controls:

```text
One council = one decision question.
One speaker = one role boundary.
One turn = one verdict + limited supporting points.
New topic = follow-up card candidate.
Second round = unresolved issues only.
Discord transcript = presentation surface, not SOT.
Final result = Kanban/Vault return path.
```

Moderator intervention template:

```text
This appears outside the current decision question.
Please choose one:
1. connect it directly to the locked agenda,
2. withdraw it,
3. mark it as a follow-up card candidate.
```

## Implementation phasing

### Phase A: Spec alignment

- This TOBE document is indexed from `README.md`.
- Additive changes are reflected in `03-protocol-spec.md`, `04-cli-spec.md`, `05-storage-schema.md`, `06-state-machine.md`, `07-moderator-policy.md`, `08-acceptance-tests.md`, `09-implementation-epics.md`, and `13-operational-contracts.md`.
- First implementation keeps attendance as typed events within `created`; do not add a new `attendance` phase without a later coordinated migration.

### Phase B: Core/plugin bootstrap split

- Core bootstrap follows `19-tooling.md`: create `go.mod`, `cmd/atn-control`, `cmd/atn-controld`, `internal/`, `tests/`, and help smoke tests.
- Plugin bootstrap follows `../../agent-turn-network-plugin/docs/06-implementation-epics-tasks.md`: create `pyproject.toml`, `plugin.yaml`, `src/atn_plugin/`, fake daemon tests, and Hermes plugin smoke tests.
- Do not start with Discord API integration.

### Phase C: Core council engine with surface metadata

- Implement `council new` with optional `surface` metadata.
- Implement session storage/projection of surface metadata.
- Implement storage/projection of linked authority result status/evidence.
- Implement `transcript` output showing Discord thread links and linked authority return state.

### Phase D: Attendance and agenda lock

- Implement `attendance_requested`, `member_attended`, and `agenda_locked`.
- Add required storage/projection for attendance request, terminal attendance for every required participant, and agenda lock.
- Add CLI commands for attendance and agenda lock.

### Phase E: Role-order / moderator-direct floor grants

- Add `speaker_selected.selection_mode` support for `moderator_direct` and/or `role_order`.
- Keep hand-raise support for broader councils.

### Phase F: Discord-thread presentation path

- First implementation can rely on the moderator runtime/Hermes gateway to post visible messages.
- A future bridge can be considered only after the core state machine and transcript are stable.

### Phase G: Kanban/Vault integration

- Add optional `--kanban-card` binding.
- Ensure final council outcome can produce a Kanban-ready summary.
- Persist and project `linked_authority_result.status` with evidence; `failed` and `pending_followup` remain incomplete return states.
- Add Vault decision-note guidance for Gray/Gongmyeong workflows.

## Acceptance criteria

A minimal successful Discord-thread council should prove:

1. A council session can be created with `surface.kind=discord_thread` and `thread_id`.
2. Attendance is recorded for each participant, with timeout behavior for missing members.
3. The moderator locks one decision question.
4. The moderator grants the floor to one member at a time.
5. Speech from non-current speakers is rejected or recorded as a policy violation, not accepted as normal speech.
6. Moderator intervention can redirect topic drift.
7. A draft conclusion can be proposed.
8. Each member can vote.
9. The council finalizes or marks unresolved.
10. Transcript includes the Discord thread pointer and all typed events.
11. If a Kanban card is linked, final return evidence records `posted`, `failed`, or `pending_followup`; only `posted` proves the card/Vault return completed.
12. No Hermes core modification is required.

## Resolved and deferred design decisions

1. First-pass alignment keeps attendance as typed events inside `created`; a later true `attendance` phase would require a coordinated spec/replay/storage migration.
2. First-pass alignment splits optional session-level `turn_mode` as intended/default policy from per-turn `speaker_selected.payload.selection_mode` as durable audit evidence.
3. Member speech may come from long-lived member runtimes or bounded runner invocations, but every durable speech event must still be a typed ATN event with the runner/accounting fields required by `03-protocol-spec.md` and `13-operational-contracts.md`.
4. Discord message IDs are evidence pointers. Opening/final delivery evidence is required when it proves visible-post or linked-authority return completion; additional per-post IDs are allowed but must not become ordering or lifecycle authority.
5. Kanban/Vault binding uses generic `linked_authority` metadata on `session_created.payload` and `linked_authority_result` on final/unresolved council outcomes.
6. Follow-up topic candidates must not be auto-created as Kanban cards by the daemon. They should be listed for Gongmyeong/user approval or created by the appropriate moderator/Gray workflow with explicit evidence.

## Owner review alignment record

Patch status: initial Discord-thread spec patch applied in Kanban task `t_38fd3fec`; follow-up risk-lock task `t_d7d903ba` froze storage/projection and linked-authority return evidence contracts for Samaui Red recheck. The decisions below are now incorporated into the canonical docs; keep this section as review traceability, not as a separate implementation gate.

Owner-side direction now reflected in canonical docs:

1. Make Discord-thread attendance and agenda lock mandatory before preparation.
   - Keep attendance as typed events inside `created` for the first pass; do not add a new `attendance` phase yet.
   - For `surface.kind=discord_thread`, `preparation_requested` should fail closed unless `attendance_requested`, one terminal `member_attended` record per required participant (`present`, `partial`, `unavailable`, or `no_response_timeout`), and `agenda_locked` already exist.
   - Reflected in `03-protocol-spec.md`, `04-cli-spec.md`, `06-state-machine.md`, `07-moderator-policy.md`, and the negative acceptance test in `08-acceptance-tests.md` for prepare-before-attendance/agenda rejection.
2. Treat Kanban/Vault return as a post-finalization authority-return gate, not daemon side effect.
   - `council_finalized` may record the council decision, but linked authority is not complete until moderator/Gray evidence records `posted`, `failed`, or `pending_followup` for Kanban/Vault return.
   - The daemon must never create Kanban comments or Vault notes directly and replay must remain side-effect free.
   - If return fails or remains pending, the origin Kanban card must stay blocked/pending review or a clearly linked follow-up card must be created; final reports must not claim the return path is complete without evidence.
   - Reflected in `03-protocol-spec.md` for `linked_authority_result.status`, `13-operational-contracts.md` for fail/pending semantics, and `08-acceptance-tests.md` for failed-return evidence behavior.
3. Split session default turn policy from per-turn floor-grant evidence.
   - Keep optional session-level `turn_mode` as the default intended policy from `council new`.
   - Keep `speaker_selected.payload.selection_mode` as the per-turn audit fact; it may match `turn_mode` or deliberately deviate with a required reason.
   - Reflected in `03-protocol-spec.md` session metadata, `04-cli-spec.md` `--turn-mode`, `07-moderator-policy.md` selection rules, and `08-acceptance-tests.md` assertions that per-turn `selection_mode` is recorded.

## Historical next-assignment note

The following was the historical next assignment before the alignment patches were applied. It is retained only to explain the review sequence. Do not route ATN implementation planning to Hwangcat or unrelated Kkachi ownership tracks unless 주군 later assigns that explicitly. Current Blue/Red/Orange/Gray review ownership is 마초/서황/종회/만총; those internal lane labels are not public ATN product aliases.

Historical first Kanban task:

```text
Goal: Update the ATN documentation set so Discord thread council is a first-class TOBE surface while preserving existing event-sourced daemon/protocol/plugin/CLI architecture.
Scope: docs only.
No code implementation.
Expected output: proposed patches to protocol, CLI, state machine, moderator policy, implementation epics, and acceptance tests.
Required review: Red review for scope/security/runtime risk before implementation begins.
```
