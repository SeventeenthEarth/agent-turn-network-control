# Runtime Decisions

---

## Merged from `docs/adr/runtime-decisions.md`

# Streaming Member Runtime Design

## Status

This document is **non-normative** design rationale for the stream-driven runtime model. It must not introduce command names, event schemas, cursor semantics, timeout rules, or security rules that are absent from the normative source-of-truth (SOT) documents. When this file and a SOT document disagree, the SOT wins.

Normative SOT for the topics this file discusses:

- Cursor format, replay rules, heartbeat cadence, acknowledgement semantics: `operations.md` §0.
- Canonical CLI surface and plugin equivalence (command names, flags, error text): `protocol-and-cli.md`.
- Stream frame envelope, event schemas, origin classes: `protocol-and-cli.md`.
- State transitions and exit conditions: `architecture.md`.
- Security rules for stream access and registry validation: `12-operations.md`.

This file may explain *why* the design is shaped the way it is. It must not be cited as the basis for an implementation decision.

## Decision

If the reader does not know what Hermes Agent is, read `runtime-decisions.md` first.

`atn-control` is a stream-driven agent network, not a one-shot worker dispatcher. The daemon is always-on and owns durable state. Agents participate through the ATN protocol client/contract, normally exposed through the Hermes plugin, with the CLI as the canonical fallback:

```text
preferred read:  Hermes plugin stream/tail tool -> ATN protocol client/contract
preferred write: Hermes plugin typed ATN tool -> ATN protocol client/contract
fallback read:   atn-control stream ... --follow
fallback write:  atn-control <typed command> ...
```

The daemon may internally expose SSE, WebSocket, Unix socket, or local HTTP. That transport is private. The agent-facing contract is the plugin protocol-client stream plus typed ATN writes, with the CLI stream and canonical CLI command paths as the diagnostics/recovery/manual fallback.

## Participants

### Moderator runtime

The moderator runtime creates sessions, watches the stream, applies policy, grants turns, escalates to the user, requests reviews, and accepts or finalizes outcomes.

### Member runtime

Each member runtime is a long-lived loop for one real Hermes profile. It:

1. loads its member identity from the registry,
2. subscribes to the active session stream,
3. persists its acknowledged cursor,
4. resumes its real AI session or wrapper when thinking is required,
5. writes typed events back through the ATN protocol client/contract or canonical CLI fallback.

A member runtime is not a simulated role prompt. It must preserve the member profile identity and session handle.

## Runtime loop

```text
start
  -> validate live registry identity for startup/discovery before session binding
  -> find active session where member is a participant
  -> validate registry identity against the active session's `registry_snapshot.yaml` once session-bound
  -> ATN protocol client/contract stream subscribe, or atn-control stream <session_id> --member <member> --since <cursor> --follow as fallback
  -> for each event:
       - treat event.to as semantic addressing, not access control
       - decide whether to act based on event.type, event.from, event.to, role, phase, and local policy
       - ignore events outside member interest
       - update local context from transcript/brief if needed
       - run/resume member AI session when needed
       - emit typed ATN command through plugin protocol client or canonical CLI fallback
       - acknowledge cursor only after successful local processing
  -> reconnect from last acknowledged cursor on disconnect
```

## Council flow

```text
moderator -> council new
daemon    -> session_created, preparation_requested
members   -> observe stream, research, council ready or council prepared-partial
daemon    -> hand_raise_requested
members   -> observe stream, research/fact-check, council hand-raise
moderator -> council grant
speaker   -> observe speaker_selected, council speak
moderator -> council propose / council revise -> draft_conclusion
moderator -> council request-vote -> consensus_vote_requested
members   -> observe consensus_vote_requested, council vote
moderator -> council finalize | council unresolved
```

No council turn should depend on spawning a fresh one-shot subprocess as the primary loop. A member may call its wrapper internally, but the runtime remains the participant.

## Delegation flow

```text
moderator -> delegate new
daemon    -> session_created, task_assigned
assignee  -> observe stream, delegate ack, work
assignee  -> delegate clarify | delegate update | delegate submit
moderator -> delegate answer-clarification | delegate message | delegate escalate
moderator -> delegate review | delegate revise | delegate accept | delegate block
moderator -> delegate escalate (low urgency)
daemon    -> escalation_batched, phase unchanged
daemon/moderator -> flush batch (timer or `delegate escalation-flush`)
daemon    -> user_escalation_requested, phase = waiting_user
moderator -> delegate escalation-delivered | delegate escalation-delivery-failed
user      -> answer
moderator -> delegate resolve-escalation -> delegate answer-clarification --source user
reviewer  -> delegate review-question | delegate review-submit
assignee  -> delegate review-answer
```

The same stream contract handles work updates, clarification, review questions, and user escalation resolution.

## CLI stream contract

Example:

```bash
atn-control stream sess_123 --member agent-1 --since cur_42 --follow --format ndjson
```

Each line:

```json
{
  "cursor": "cur_000000000043_evt_...",
  "is_replay": false,
  "event": {
    "event_id": "evt_...",
    "type": "hand_raise_requested",
    "from": "agent-mod",
    "to": ["agent-1", "agent-2", "agent-3"],
    "payload": {}
  }
}
```

`from` is a string and `to` is always an array of strings (per `protocol-and-cli.md`).

Acknowledgement:

```bash
atn-control stream ack sess_123 --member agent-1 --cursor cur_000000000043_evt_...
```

Rules:

- Append to `channel.jsonl` before publish.
- Replay missed events before live events.
- Acknowledge only after local processing.
- Do not silently skip unknown events, cursor gaps, or schema versions.
- All writes go through typed commands such as `delegate clarify`, `delegate update`, `council hand-raise`, `council speak`, and `council vote`.

## Transport guidance

Recommended implementation order:

1. Local HTTP SSE or Unix-socket NDJSON for daemon-to-client streaming, with the CLI stream as canonical fallback.
2. Protocol-client writes as ordinary request/response commands, with CLI writes as canonical diagnostics/recovery/manual fallback.
3. WebSocket only if one bidirectional connection becomes necessary.

This keeps agent operation simple: one runtime reads the plugin protocol-client stream or canonical CLI NDJSON fallback; actions are separate typed ATN command calls.

## RUNRT-001 local implementation note

The current control-repo implementation provides local/fake RUNRT support only:

- `internal/memberruntime.Runtime` owns one `(session_id, member)` identity and reads replay frames before live frames through an injected stream client.
- Cursor acknowledgement is emitted after ignored events are processed, and after actionable events only when local handling succeeds or a durable failure event has already been recorded. The cursor store is written only after ack succeeds.
- Runtime action filtering considers event type, sender, recipient hints, phase/role policy inputs, and member identity. It does not treat `event.to` as a visibility boundary; a runtime can ignore an event for action while still acknowledging it as processed.
- Malformed cursor stores, cursor gaps, corrupt frames, and unknown schema versions fail closed.
- Same `(session_id, member)` dispatch is serialized by the dispatch lock helpers; different members can be locked independently.

The bounded runner seam uses only fake adapters/wrappers in tests. It does not implement DELEG/COUNC lifecycle, live Hermes dispatch readiness, Discord delivery, KAB integration, or gateway behavior.

## MEMBR-001 pilot decision

The first participant invocation pilot uses main-agent mediated bounded runner invocation as a disposable local proof step before long-lived member runtimes.

This pilot means the main agent or operator-controlled lane observes the daemon-selected participant turn, invokes the selected member through the existing bounded runner path, and records the result back to the daemon with durable runner/session evidence. It is not a simulated role prompt and it must not replace the participant profile with an ad hoc Codex role. The selected member's real registry identity, wrapper boundary, and session handle or redacted equivalent remain part of the evidence contract.

Long-lived member runtimes remain the target model because ATN is stream-driven: each participant should eventually observe replay/live frames, manage its own cursor, preserve its real profile identity, and write typed ATN events as the participant. They are not the first proof mode because the next risk to retire is narrower: prove one selected participant can be invoked through a real profile/wrapper boundary and leave enough durable evidence to distinguish success, failure, timeout, and unsafe setup. The bounded pilot keeps that proof disposable and reviewable before introducing always-on participant loops.

The minimum runner/session evidence for the pilot is:

- selected profile/member identity and the session `registry_snapshot.yaml` binding used for that invocation;
- command, session, and request identifiers for the selected turn;
- one runner invocation id preserved from start through terminal outcome;
- wrapper, backend, and session handle, or a redacted equivalent sufficient to prove real invocation without exposing secrets;
- started timestamp and terminal timestamp/status;
- stdout, stderr, log, and artifact pointers as redacted evidence pointers only, not inline secret-bearing payloads;
- on success, the produced typed ATN event such as `council.speak` when applicable;
- on failure, a durable failure event that records the failed invocation instead of fake progress.

The pilot fails closed on registry mismatch, missing wrapper, unsafe profile, missing evidence, command id conflict, timeout, unsupported transport, cursor gap, or schema gap. A missing real member must never fall back to a role prompt.

MEMBR-002 owns implementation and proof of this selected invocation path. Its tests start with fake or isolated wrapper coverage, and real-profile evidence is allowed only when explicitly authorized. MEMBR-002 must not substitute simulated role prompts for participant profiles.

## Failure policy

- Daemon unavailable: runtime stops or retries with bounded backoff; no fake progress event.
- Cursor gap: fail closed and request replay/recovery.
- Registry mismatch or unsafe registry state: fail closed. Once a session exists, member runtime identity is checked against the session's frozen `registry_snapshot.yaml`; live registry edits must not silently change the participants of an active session. A member runtime must not replace a missing or invalid registered member with a simulated role prompt.
- Stale heartbeat: daemon emits `stream_subscriber_stale`; moderator may repoll, mark partial, or block according to session policy.
- Member wrapper failure: record the invocation through `runner_invocation_started`, then record the outcome through a terminal semantic event, `runner_invocation_failed`, or `runner_result_discarded`. Do not replace the member with a simulated member unless the user explicitly approves.

A member runtime or daemon compatibility path that uses the bounded runner adapter must preserve the `runner.invocation_id` across the started event and its terminal outcome event.

## Recipient visibility for runtimes

A member runtime must not treat absence from `event.to` as proof that the event is invisible or unauthorized. It may ignore the event for action purposes, but cursor acknowledgement should still follow the runtime's normal processed-event policy.

## Escalation lifecycle for runtimes

Member runtimes should treat `escalation_batched` as audit context only. They should not assume the user has been asked until they observe `user_escalation_requested` and, for delivery assurance, `user_escalation_delivered`. When the session is in `waiting_user`, member runtimes should avoid proceeding on the blocked decision until `user_escalation_resolved` and the corresponding relay event are observed.

## Phase vs status for runtimes

Member runtimes should interpret `event.phase` as the **post-transition** lifecycle phase and key state-specific behavior off `phase`. They should **not** infer lifecycle transitions from `assignee_update.payload.progress_status`; that field is only the assignee's self-report.

Runtimes may consult the projected `status` (`open`/`blocked`/`terminal`) from `atn-control status` for high-level decisions (e.g. whether the active-session lock is held), but transition logic uses `phase`.

---

## Merged from `docs/adr/runtime-decisions.md`

# Hermes Agent Runtime Context

## Status

This document is **non-normative**. It explains runtime context for reviewers who have never seen Hermes Agent. It is not the SOT for CLI commands, event schemas, security rules, or Release v1 scope. When this file and a SOT document disagree, the SOT wins.

Normative SOT for related topics:

- Release v1 scope and primary customer: `../spec/overview.md`; current task status lives in `../roadmap.md`.
- CLI surface: `protocol-and-cli.md`.
- Event schemas: `protocol-and-cli.md`.
- Security rules: `12-operations.md`.

## Purpose

This document explains what this project means by **Hermes Agent** and how the current main-agent/sub-agent operating model works. It is written for external AI reviewers that have not seen the user's team-member profiles or the Hermes tool runtime.

**Hermes Agent is the primary customer of `atn-control`.** Every contract — CLI surface, daemon socket, registry shape, runner adapter, session lifecycle — is designed for Hermes Agent operation. See `../spec/overview.md#primary-customer` for the consequences this has on adapter scope and Release v1 boundaries. Reactive CLI tools (Claude Code, Codex CLI, Gemini CLI, OpenCode) are not first-class users; they may interact with the system only through the bundled Hermes skill, and Release v1 ships no dedicated runner adapter for them.

`atn-control` is being designed around this runtime model. Alternative proposals are welcome, but they should preserve the same product goals: real profile identity, durable state, auditable events, user-controlled escalation, and no silent substitution with fake role-play agents.

## What is a Hermes Agent?

In this project, a Hermes Agent is an autonomous AI runtime with:

- a persistent profile/persona and operating memory;
- access to tools for files, terminal commands, browser/web lookup, scheduled jobs, task lists, skills, and sometimes platform delivery such as Telegram;
- a conversation/session context with the user or with an orchestrator;
- the ability to call tools, inspect results, continue work, verify outputs, and report back;
- a library of reusable skills that encode project-specific procedures;
- optional profile wrappers such as `moderator`, `agent-1`, or `agent-2` that start that profile's real runtime.

A Hermes Agent is not just a single LLM completion. It is an agent loop around a model: observe context, choose a tool/action, execute it, observe the result, continue until the task is complete or blocked.

## Current main-agent role

The main agent in this workflow is the `moderator`, the orchestrator profile. Its responsibilities are:

1. understand the user's request;
2. decide whether to act directly, delegate, ask a named team member, or escalate;
3. load relevant skills before acting;
4. use tools to inspect files, run commands, create documents, schedule work, or communicate;
5. coordinate named team members without pretending that a temporary role prompt is the real team member;
6. verify outputs before reporting completion;
7. keep durable operational facts in memory or skills when they will matter again.

The main agent should not claim that work was done by a named team member unless that real profile/session was actually invoked.

## Ways the main agent can work today

The current Hermes runtime gives the main agent several ways to execute or coordinate work. They differ in identity, context continuity, latency, reliability, and suitability for this project.

### 1. Direct tool execution inside the main session

The main agent can directly use tools such as file read/write, patch, terminal, browser, web extraction, todo, memory, and skills.

Good for:

- document editing;
- code inspection;
- running tests;
- one-agent implementation work;
- deterministic file and shell operations.

Limits:

- it is still the main agent doing the work;
- it does not create a real separate team-member opinion;
- long-running collaboration can fragment the main user conversation.

### 2. Temporary delegated subagents

The main agent can spawn isolated subagents for independent tasks. These subagents can inspect files or reason in parallel and return a final summary.

Good for:

- parallel code review;
- independent research branches;
- large context isolation;
- reducing noise in the main session.

Limits:

- these are temporary workers, not the user's named Hermes team-member profiles;
- they do not have the full persistent identity of `agent-1`, `agent-2`, etc.;
- they generally cannot ask the user for clarification;
- they return summaries rather than participating in a durable shared discussion stream.

Project policy: useful as an implementation aid, but not accepted as a substitute for real named member participation.

### 3. Real member profile wrappers

The main agent can invoke a real profile wrapper, for example a profile-specific CLI command that starts/resumes a named Hermes Agent profile. This preserves the member identity better than temporary subagents.

Good for:

- asking a real team member profile for a review or opinion;
- preserving member-specific persona, skills, memory, and workspace;
- tasks where the user's organization cares who said what.

Limits:

- if invoked only as a one-shot subprocess, the member may not continuously observe the discussion;
- the caller must preserve session handles and resume correctly;
- without a shared event log, the transcript can become fragmented.

This is one reason `atn-control` exists.

### 4. Scheduled jobs

The main agent can create scheduled jobs that run later in fresh sessions with self-contained prompts.

Good for:

- daily briefings;
- periodic cleanup/audits;
- reminder-style automation;
- repeated monitoring tasks.

Limits:

- jobs run without the current chat context unless explicitly included;
- they should not be used for live council turns;
- autonomous cron runs cannot ask the user for clarification in the moment.

### 5. Platform delivery channels

Some profiles can report through Telegram or other platform integrations. This is useful for urgent blockers or scheduled reports.

Good for:

- notifying the user about blocked work;
- delivering scheduled summaries;
- reporting completion outside the current terminal/chat.

Limits:

- profile gateways must be isolated; one profile's gateway must not be restarted or modified while operating another profile;
- Telegram delivery is not the same as durable session state;
- the canonical event record should still live in `channel.jsonl` for this project.

### 6. Shared files and documents

The agents can coordinate through durable files: task documents, feedback files, artifacts, transcripts, and logs.

Good for:

- long-lived design context;
- human-readable review;
- artifact handoff;
- recovery after context loss.

Limits:

- file polling is less precise than an event stream;
- without an explicit protocol, it is easy to miss causality and turn order.

### 7. MCP or plugin integrations

Hermes may use MCP servers or plugins when configured. MCP is a useful standard for exposing tools/resources/prompts to LLM applications.

Good for:

- standardized tool/resource interfaces;
- external service integration;
- potential future adapter layer.

Limits for this project:

- MCP is not the source of truth for `atn-control`;
- requiring MCP first would couple the design to one integration surface;
- the current project priority is daemon plus ATN protocol client/contract, preferred Hermes plugin integration, and canonical CLI fallback, with MCP as a possible thin adapter later.

### 8. The proposed `atn-control` stream model

The target design adds a durable communication layer:

```text
main/moderator runtime / Hermes plugin
  -> ATN protocol client/contract typed commands
  -> atn-controld durable event log and state engine
  -> ATN protocol client/contract stream, with atn-control stream as fallback
  -> member runtimes
  -> real profile wrappers / resumed AI sessions
  -> typed ATN commands through plugin protocol client, with CLI as fallback
```

Good for:

- real-time or near-real-time council participation;
- preserving turn order and causality;
- letting members decide when to raise a hand or respond;
- reconnect/replay through durable cursors;
- auditability and transcript generation;
- avoiding one-shot worker context loss.

Limits:

- more complex than a simple subprocess call;
- needs member runtime supervision and heartbeat handling;
- needs cursor, replay, schema migration, and failure policy.

## Preferred approach for this project

This project should proceed with the stream-driven member runtime model.

Priority order:

1. **Daemon plus protocol contract, Hermes plugin adapter, and canonical CLI fallback is the product boundary.** Agents prefer plugin tools/slash commands and can fall back to `atn-control`; direct daemon APIs are internal.
2. **Event log first.** `channel.jsonl` is the source of truth; SQLite is a projection.
3. **Stream for observation.** Moderator and members observe sessions via the ATN protocol client/contract, normally through the Hermes plugin; `atn-control stream` remains the canonical fallback with cursors and replay.
4. **Typed commands for writes.** Members do not mutate daemon internals; they use typed ATN commands such as `delegate clarify`, `delegate update`, `council hand-raise`, `council speak`, and `council vote`, exposed through plugin tools or canonical CLI commands.
5. **Real profile identity.** Named members must be real profiles/wrappers/runtimes, not temporary role-prompt simulations.
6. **Runner adapters are bounded helpers.** One-shot subprocess calls may be used inside a member runtime or for compatibility, but not as the primary council loop.
7. **Fail closed.** Unknown members, cursor gaps, unknown schema versions, storage corruption, and unsafe wrappers stop the affected flow rather than silently continuing.
8. **MCP later.** MCP can be added later as a thin adapter if it helps external integrations, but it is not the SOT.

## Why not only one-shot subprocess workers?

A one-shot worker model is attractive because it is simple:

```text
daemon -> spawn member wrapper -> capture answer -> store event
```

But it is weak for this product:

- members do not continuously observe the discussion;
- context depends on prompt reconstruction and session resume correctness;
- hand raising becomes artificial because the moderator has to ask each member every time;
- failure recovery is harder when the live participant is only a subprocess call;
- it blurs the difference between a real member runtime and a temporary prompt.

One-shot calls remain useful as bounded model-invocation adapters, but they should live under the member runtime model.

## Design questions external AI reviewers should consider

When reviewing or proposing alternatives, please address these questions explicitly:

1. How does a member observe new events without polling too slowly or missing context?
2. How is the member's last processed event cursor persisted?
3. How are writes validated and deduplicated?
4. What happens if a member runtime disconnects mid-council?
5. How does the design prove that `agent-1` was a real profile and not a simulated role prompt?
6. How are user escalations represented and delivered without losing the session state?
7. Which parts are product contracts and which are replaceable implementation details?
8. How would MCP, WebSocket, SSE, local sockets, or a task queue fit without replacing the daemon/protocol/plugin/CLI SOT?

## Reference documents

These references are not requirements, but they help explain the design space.

- Model Context Protocol specification: https://modelcontextprotocol.io/specification
  - Useful for understanding standardized LLM tool/resource/prompt integrations and JSON-RPC-based client/server architecture.
- JSON-RPC 2.0 specification: https://www.jsonrpc.org/specification
  - Useful if the daemon control channel uses request/response method calls.
- OpenAI Agents SDK docs: https://openai.github.io/openai-agents-python/
  - Useful for concepts such as agents, tools, handoffs, guardrails, tracing, and agent orchestration.
- LangGraph durable execution: https://docs.langchain.com/oss/python/langgraph/durable-execution
  - Useful for thinking about resumable long-running workflows and human-in-the-loop pauses.
- MDN Server-sent events: https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events
  - Useful for daemon-to-CLI event streaming if SSE is chosen internally.
- JSON Lines format: https://jsonlines.org/
  - Useful for `channel.jsonl`, stream frames, and append-only event logs.

## Terminology mapping

```text
Hermes Agent          = persistent tool-using AI runtime profile
main agent            = moderator/orchestrator
member agent          = real team-member profile such as agent-1 or agent-2
member runtime        = long-lived loop that watches the stream and acts for one member
runner adapter        = bounded invocation of a model/profile wrapper
subagent              = temporary delegated worker; useful, but not a named member
atn-controld        = daemon owning state, locks, event log, stream hub
atn-control         = stable CLI used by humans and agents
channel.jsonl         = source-of-truth event log
stream cursor         = durable position in a session event stream
```

---

## Merged from `docs/adr/runtime-decisions.md`

# TOBE: Discord Thread Council Surface

## Status

- Status: TOBE source plus alignment record; canonical implementation rules now live in the main protocol, CLI, state-machine, storage, policy, operational-contract, acceptance-test, and epic docs.
- Scope: `atn-control` council UX on Discord threads
- Source request, preserved as SOT: `discord에서는 이런 토론이 하나의 쓰레드 안에서 진행되었으면 좋겠어. 내가 "토론을 시작해줘"하면 네가 쓰레드에 장수들을 한명씩 불러서 출석 체크를 한 다음에 토론이 진행되면서 공명이 발언권을 주고 토론을 체계적으로 수향하는거지.`
- Operating language: user-facing reports to 주군 are Korean; agent-facing docs and implementation notes remain English while preserving fixed Korean labels such as `17번째 지구`, `주군`, and member names.
- Governance correction: ATN is the public product/repository naming for the bounded ATN control/plugin lane. Prior Hwangcat-routed work is historical draft/spec-prep evidence only, not durable current ownership. Current ATN lane ownership is 마초/서황/종회/만총 for Blue/Red/Orange/Gray respectively; Gongmyeong/wolong may coordinate Kanban routing without becoming Blue.


## Spec alignment record

This document preserves the original Discord-thread council TOBE request and the decisions that were folded into the canonical ATN docs. Implementers should treat this file as UX/background context; when details differ, the canonical implementation spec is `protocol-and-cli.md`, `protocol-and-cli.md`, `architecture.md`, `architecture.md`, `architecture.md`, `testing-and-tooling.md`, `docs/todo/implementation-decomposition.md`, and `operations.md`. The first-class alignment is additive:

1. Keep `channel.jsonl` as the canonical council SOT and Discord thread as the human-visible surface only.
2. Add optional council `surface` and `linked_authority` metadata to `session_created.payload` and session projections.
3. Represent attendance and agenda lock as typed council events while the session remains in `created`, rather than adding a new `attendance` phase for the first pass.
4. Extend `speaker_selected.payload.selection_mode` with `moderator_direct` and `role_order` so Gongmyeong can grant floor explicitly in a Discord-thread council.
5. Keep Discord delivery outside `atn-controld`; the moderator runtime posts to Discord through Hermes plugin/gateway capability and records delivery evidence as metadata or follow-up delivery audit.
6. If `linked_authority.kanban_card_id` is present, final council outcome must be returned to the Kanban card; Vault decision-note recording remains a Gray/Gongmyeong workflow when the topic is durable architecture/process/command knowledge.

Aligned spec sections:

- `docs/README.md`: add source-of-truth and decision-log entries for Discord-thread council surface binding.
- `docs/spec/overview.md`: describe Discord-thread council as an optional surface, not a replacement architecture.
- `docs/spec/overview.md`: add council requirements for surface binding, attendance, agenda lock, floor grants, and Kanban/Vault return path.
- `docs/spec/architecture.md`: preserve Clean Architecture by keeping Discord transport outside engine/domain and outside `atn-controld` delivery responsibility.
- `docs/spec/protocol-and-cli.md`: add `surface`/`linked_authority` metadata, `attendance_requested`, `member_attended`, `agenda_locked`, and final outcome linkage fields.
- `docs/spec/protocol-and-cli.md`: add additive flags/commands and event-to-command rows.
- `docs/spec/architecture.md`: add optional projected session fields for `surface` and `linked_authority`.
- `docs/spec/architecture.md`: document the no-new-phase first-pass choice and allowed event ordering.
- `docs/spec/architecture.md`: add Discord-thread attendance, agenda-lock, role-order, and divergence controls.
- `docs/spec/testing-and-tooling.md`: add Discord-thread council acceptance scenario.
- `docs/todo/implementation-decomposition.md`: add an implementation epic for Discord-thread surface binding after the existing core council engine.
- `docs/spec/operations.md`: state token/thread validation boundary and forbid raw Discord tokens in daemon/event payloads.
- `docs/spec/operations.md`: state replay/idempotency behavior for surface metadata and final return-path evidence.

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
    "success_criteria": "Consensus identifies the bounded next action, owner, and evidence requirement.",
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
  --request-source discord_thread \
  --requested-output-mode live_visible_thread \
  --visible-output-required true \
  --surface discord-thread \
  --surface-platform discord \
  --thread-id 1507515847227215932 \
  --kanban-card t_xxxxx \
  --turn-mode role_order

atn-control council attend <session_id> \
  --from macho \
  --status present \
  --summary "Present. Owner-side spec governance perspective ready."

atn-control council lock-agenda <session_id> \
  --decision-question "Decide next action for Kanban card t_xxxxx" \
  --success-criteria "Consensus identifies the bounded next action, owner, and evidence requirement" \
  --out-of-scope-policy "New topics become follow-up card candidates, not current-thread expansion" \
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
- Additive changes are reflected in `protocol-and-cli.md`, `protocol-and-cli.md`, `architecture.md`, `architecture.md`, `architecture.md`, `testing-and-tooling.md`, `docs/todo/implementation-decomposition.md`, and `operations.md`.
- First implementation keeps attendance as typed events within `created`; do not add a new `attendance` phase without a later coordinated migration.

### Phase B: Core/plugin bootstrap split

- Core bootstrap follows `19-testing-and-tooling.md`: create `go.mod`, `cmd/atn-control`, `cmd/atn-controld`, `internal/`, `tests/`, and help smoke tests.
- Plugin bootstrap follows `../../agent-turn-network-plugin/docs/roadmap.md`: create `pyproject.toml`, `plugin.yaml`, `src/atn_plugin/`, fake daemon tests, and Hermes plugin smoke tests.
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
3. Member speech may come from long-lived member runtimes or bounded runner invocations, but every durable speech event must still be a typed ATN event with the runner/accounting fields required by `protocol-and-cli.md` and `operations.md`.
4. Discord message IDs are evidence pointers. Opening/final delivery evidence is required when it proves visible-post or linked-authority return completion; additional per-post IDs are allowed but must not become ordering or lifecycle authority.
5. Kanban/Vault binding uses generic `linked_authority` metadata on `session_created.payload` and `linked_authority_result` on final/unresolved council outcomes.
6. Follow-up topic candidates must not be auto-created as Kanban cards by the daemon. They should be listed for Gongmyeong/user approval or created by the appropriate moderator/Gray workflow with explicit evidence.

## Owner review alignment record

Patch status: initial Discord-thread spec patch applied in Kanban task `t_38fd3fec`; follow-up risk-lock task `t_d7d903ba` froze storage/projection and linked-authority return evidence contracts for Samaui Red recheck. The decisions below are now incorporated into the canonical docs; keep this section as review traceability, not as a separate implementation gate.

Owner-side direction now reflected in canonical docs:

1. Make Discord-thread attendance and agenda lock mandatory before preparation.
   - Keep attendance as typed events inside `created` for the first pass; do not add a new `attendance` phase yet.
   - For `surface.kind=discord_thread`, `preparation_requested` should fail closed unless `attendance_requested`, one terminal `member_attended` record per required participant (`present`, `partial`, `unavailable`, or `no_response_timeout`), and `agenda_locked` already exist.
   - Reflected in `protocol-and-cli.md`, `protocol-and-cli.md`, `architecture.md`, `architecture.md`, and the negative acceptance test in `testing-and-tooling.md` for prepare-before-attendance/agenda rejection.
2. Treat Kanban/Vault return as a post-finalization authority-return gate, not daemon side effect.
   - `council_finalized` may record the council decision, but linked authority is not complete until moderator/Gray evidence records `posted`, `failed`, or `pending_followup` for Kanban/Vault return.
   - The daemon must never create Kanban comments or Vault notes directly and replay must remain side-effect free.
   - If return fails or remains pending, the origin Kanban card must stay blocked/pending review or a clearly linked follow-up card must be created; final reports must not claim the return path is complete without evidence.
   - Reflected in `protocol-and-cli.md` for `linked_authority_result.status`, `operations.md` for fail/pending semantics, and `testing-and-tooling.md` for failed-return evidence behavior.
3. Split session default turn policy from per-turn floor-grant evidence.
   - Keep optional session-level `turn_mode` as the default intended policy from `council new`.
   - Keep `speaker_selected.payload.selection_mode` as the per-turn audit fact; it may match `turn_mode` or deliberately deviate with a required reason.
   - Reflected in `protocol-and-cli.md` session metadata, `protocol-and-cli.md` `--turn-mode`, `architecture.md` selection rules, and `testing-and-tooling.md` assertions that per-turn `selection_mode` is recorded.

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
