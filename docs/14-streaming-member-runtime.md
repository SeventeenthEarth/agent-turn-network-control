# Streaming Member Runtime Design

## Status

This document is **non-normative** design rationale for the stream-driven runtime model. It must not introduce command names, event schemas, cursor semantics, timeout rules, or security rules that are absent from the normative source-of-truth (SOT) documents. When this file and a SOT document disagree, the SOT wins.

Normative SOT for the topics this file discusses:

- Cursor format, replay rules, heartbeat cadence, acknowledgement semantics: `13-operational-contracts.md` §0.
- Canonical CLI surface and plugin equivalence (command names, flags, error text): `04-cli-spec.md`.
- Stream frame envelope, event schemas, origin classes: `03-protocol-spec.md`.
- State transitions and exit conditions: `06-state-machine.md`.
- Security rules for stream access and registry validation: `12-security.md`.

This file may explain *why* the design is shaped the way it is. It must not be cited as the basis for an implementation decision.

## Decision

If the reader does not know what Hermes Agent is, read `15-hermes-agent-runtime-context.md` first.

`kkachi-agent-network` is a stream-driven agent network, not a one-shot worker dispatcher. The daemon is always-on and owns durable state. Agents participate through the KAN protocol client/contract, normally exposed through the Hermes plugin, with the CLI as the canonical fallback:

```text
preferred read:  Hermes plugin stream/tail tool -> KAN protocol client/contract
preferred write: Hermes plugin typed KAN tool -> KAN protocol client/contract
fallback read:   kkachi-agent-network stream ... --follow
fallback write:  kkachi-agent-network <typed command> ...
```

The daemon may internally expose SSE, WebSocket, Unix socket, or local HTTP. That transport is private. The agent-facing contract is the plugin protocol-client stream plus typed KAN writes, with the CLI stream and canonical CLI command paths as the diagnostics/recovery/manual fallback.

## Participants

### Moderator runtime

The moderator runtime creates sessions, watches the stream, applies policy, grants turns, escalates to the user, requests reviews, and accepts or finalizes outcomes.

### Member runtime

Each member runtime is a long-lived loop for one real Hermes profile. It:

1. loads its member identity from the registry,
2. subscribes to the active session stream,
3. persists its acknowledged cursor,
4. resumes its real AI session or wrapper when thinking is required,
5. writes typed events back through the KAN protocol client/contract or canonical CLI fallback.

A member runtime is not a simulated role prompt. It must preserve the member profile identity and session handle.

## Runtime loop

```text
start
  -> validate live registry identity for startup/discovery before session binding
  -> find active session where member is a participant
  -> validate registry identity against the active session's `registry_snapshot.yaml` once session-bound
  -> KAN protocol client/contract stream subscribe, or kkachi-agent-network stream <session_id> --member <member> --since <cursor> --follow as fallback
  -> for each event:
       - treat event.to as semantic addressing, not access control
       - decide whether to act based on event.type, event.from, event.to, role, phase, and local policy
       - ignore events outside member interest
       - update local context from transcript/brief if needed
       - run/resume member AI session when needed
       - emit typed KAN command through plugin protocol client or canonical CLI fallback
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
kkachi-agent-network stream sess_123 --member agent-1 --since cur_42 --follow --format ndjson
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

`from` is a string and `to` is always an array of strings (per `03-protocol-spec.md`).

Acknowledgement:

```bash
kkachi-agent-network stream ack sess_123 --member agent-1 --cursor cur_000000000043_evt_...
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

This keeps agent operation simple: one runtime reads the plugin protocol-client stream or canonical CLI NDJSON fallback; actions are separate typed KAN command calls.

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

Long-lived member runtimes remain the target model because KAN is stream-driven: each participant should eventually observe replay/live frames, manage its own cursor, preserve its real profile identity, and write typed KAN events as the participant. They are not the first proof mode because the next risk to retire is narrower: prove one selected participant can be invoked through a real profile/wrapper boundary and leave enough durable evidence to distinguish success, failure, timeout, and unsafe setup. The bounded pilot keeps that proof disposable and reviewable before introducing always-on participant loops.

The minimum runner/session evidence for the pilot is:

- selected profile/member identity and the session `registry_snapshot.yaml` binding used for that invocation;
- command, session, and request identifiers for the selected turn;
- one runner invocation id preserved from start through terminal outcome;
- wrapper, backend, and session handle, or a redacted equivalent sufficient to prove real invocation without exposing secrets;
- started timestamp and terminal timestamp/status;
- stdout, stderr, log, and artifact pointers as redacted evidence pointers only, not inline secret-bearing payloads;
- on success, the produced typed KAN event such as `council.speak` when applicable;
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

Runtimes may consult the projected `status` (`open`/`blocked`/`terminal`) from `kkachi-agent-network status` for high-level decisions (e.g. whether the active-session lock is held), but transition logic uses `phase`.
