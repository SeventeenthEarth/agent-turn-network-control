# Testing And Tooling

---

## Merged legacy testing/tooling content

# Acceptance Tests

## Scenario 1: Delegation active collaboration

Given no active session exists,
when the moderator creates a delegation session for agent-1,
then the daemon creates a session directory, appends `session_created` and `task_assigned`, and publishes the assignment to the session stream for agent-1's member runtime.

When agent-1's runtime observes the assignment, it first records `assignee_acknowledged` through `atn-control delegate ack`.

When agent-1 later asks a clarification question through `atn-control delegate clarify`,
then the daemon records `clarification_requested`.

When the moderator answers through `atn-control delegate answer-clarification`,
then the daemon records `clarification_answered` whose `causation_event_id` references the originating `clarification_requested` event.

When agent-1 submits work through `atn-control delegate submit`,
then the moderator must accept (`delegate accept`), request revision (`delegate revise`), or mark blocked (`delegate block`).

Then `task_assigned` has `from: "agent-mod"` and `to: ["agent-1"]`, and `clarification_requested` has `from: "agent-1"` and `to: ["agent-mod"]`.

## Scenario 2: Delegation user escalation

Given agent-1 asks a clarification question that requires user authority,
when the moderator escalates it,
then the session enters `waiting_user` and records `user_escalation_requested` with the moderator-supplied `delivery_policy` hint preserved in the payload.

When the Hermes plugin/gateway helper or equivalent moderator gateway skill reaches the user (via origin Hermes session, Telegram, Slack, or any other configured gateway) and records the result through a typed ATN command such as the canonical `atn-control delegate escalation-delivered`,
then the daemon records `user_escalation_delivered` with `from: agent-mod`, the actual `delivery_target`, and `causation_event_id` pointing to the originating `user_escalation_requested`.

When the Hermes plugin/gateway helper or equivalent moderator gateway skill fails to reach the requested target and records the failure through a typed ATN command such as the canonical `atn-control delegate escalation-delivery-failed`,
then the daemon records `user_escalation_delivery_failed` from the moderator (does not treat the escalation as delivered) and waits for a follow-up `user_escalation_delivered` if a fallback succeeds.

The daemon must not itself open any outbound notification channel; if `user_escalation_delivered` is not recorded, no delivery has occurred.

When the user answers,
then the daemon records `user_escalation_resolved` and the moderator relays `clarification_answered` back to agent-1 with `source: user`.

Then `user_escalation_requested` has `to: ["user"]`, and `user_escalation_resolved` has `from: "user"` and `to: ["agent-mod"]`.

Given agent-1 asks a clarification question that requires immediate user authority,
when the moderator escalates it with `urgency: blocked`,
then the daemon records `user_escalation_requested`, transitions the session to `waiting_user`, and does not batch the question.

Given the Hermes plugin/gateway helper or equivalent moderator gateway skill has not recorded `user_escalation_delivered`,
then no delivery has occurred, even though `user_escalation_requested` exists.

Given the user answers,
then the daemon records `user_escalation_resolved`, returns the session to `resume_phase`, and the moderator relays the answer through `clarification_answered`.

## Scenario 3: Delegation review gate

Given agent-1 has submitted work,
when the moderator requests agent-2 review,
then agent-2 receives the artifacts and review focus.

When agent-2 has a question about implementation intent or evidence,
then agent-2 asks agent-1 through `review_clarification_requested`, not the moderator.

When agent-1 answers through `review_clarification_answered`,
then agent-2 uses that answer in the review verdict.

When agent-2 returns a review verdict through `atn-control delegate review-submit`,
then the daemon records `review_submitted` with the verdict.

When the verdict is `changes_requested`,
then the moderator can issue `atn-control delegate revise`, which records `revision_requested`, and the session remains active.

## Scenario 4: Full council consensus path

Given no active session exists,
when the moderator creates a council with agent-1, agent-2, and agent-3 through `atn-control council new` with an explicit `requested_output_mode` and either live-visible Discord surface fields or explicit non-visible override evidence,
then the daemon records `session_created` and the council remains in `created`.

When the moderator starts preparation through `atn-control council prepare`,
then the daemon records `preparation_requested` and the council enters `preparation`.

When all member runtimes observe `preparation_requested` through the stream,
then each ready member records `member_ready` through `atn-control council ready`, and the council enters discussion.

When member runtimes observe `hand_raise_requested`, the daemon opens a 120-second response window. Members respond through `atn-control council hand-raise` or canonical `atn-control council drop`; the window closes early when all required members respond. If the deadline elapses first, the daemon records timeout `hand_raise_dropped` events for non-responders and restart replay must not duplicate those auto-drops. Late `council hand-raise` and manual `council drop` commands fail closed without appending an event; daemon-owned timeout auto-drop is the only supported post-deadline drop path. When the moderator selects speakers through `atn-control council grant`,
then no speaker speaks twice in a row.

When a selected member records canonical `speech`, the speech event includes local control `persistent_participant_runtime_evidence` with all council members covered: the speaker row is `self_ack`, each non-speaker row is `observe_delta`, each row advances to the speech cursor, and participant session handles are reused only within the same council/member scope.

When the moderator proposes a conclusion through `atn-control council propose` and requests a consensus vote through `atn-control council request-vote`,
then the daemon records `consensus_vote_requested`, and member runtimes vote (`atn-control council vote`) only after observing it.

When all members approve, the council finalizes (`atn-control council finalize`) and transcript export includes all turns and votes.

Then `preparation_requested` and `hand_raise_requested` use explicit recipient lists containing all council members; the daemon must not emit `to: "all"` or `to: ["all"]`.

## Scenario 5: Council preparation timeout

Given a member does not finish preparation within 10 minutes,
when the timeout expires,
then the daemon records `member_prepared_partial` and instructs the member to proceed with partial notes.

If the member runtime itself records partial preparation before timeout through `atn-control council prepared-partial`,
then the daemon records `member_prepared_partial` with origin class `participant_cli`.

If timeout expires first, the daemon records `member_prepared_partial` with origin class `daemon_internal`.

## Scenario 6: No hand raises

Given no eligible member raises a hand,
when the moderator evaluates the state,
then the system must apply the no-hand-raise policy defined in `architecture.md`: draft a conclusion when possible, otherwise ask a targeted missing-perspective question and repoll.

Random speaker selection is not permitted as default behavior. It is allowed only for tie-breaking or early exploration, and the `speaker_selected` event must carry `selection_mode: "random"` with a reason.

## Scenario 7: Block prevents council finalization

Given the moderator proposes a draft conclusion,
when one member votes `block`,
then `finalize` fails and the council returns to discussion or revision.

The final report must not claim consensus.


## Scenario 7A: Discord-thread council surface binding

Given no active session exists,
when the moderator creates a council through `atn-control council new` with `--request-source discord_thread`, `--requested-output-mode live_visible_thread`, `--visible-output-required true`, `--surface discord-thread`, `--surface-platform discord`, `--thread-id`, and `--kanban-card`,
then `session_created.payload.request_context.requested_output_mode` is `live_visible_thread`, `session_created.payload.surface.kind` is `discord_thread`, `session_created.payload.surface.platform` is `discord`, `session_created.payload.surface.thread_id` is set, and `channel.jsonl` remains the source of truth.

When the moderator records `attendance_requested`, members record one terminal `member_attended` record each (`present`, `partial`, `unavailable`, or `no_response_timeout`), and the moderator records `agenda_locked`,
then those events remain in the `created` phase and `council prepare` is still the event that enters `preparation`.

Given the same Discord-thread-bound council lacks `attendance_requested`, lacks a terminal `member_attended` record for any required participant, or lacks `agenda_locked`,
when the moderator runs `atn-control council prepare`,
then the command fails closed, appends no `preparation_requested` event, keeps the council in `created`, and reports the missing prerequisite(s).

When the moderator grants floor with `atn-control council grant --mode role_order` or `--mode moderator_direct`,
then the daemon records `speaker_selected` with that per-turn `selection_mode`, and only the selected member can record the next normal `speech` event.

Given the council was created with `--turn-mode role_order`,
when a turn is granted with `--mode moderator_direct`,
then `speaker_selected.payload.selection_mode` is `moderator_direct` and `payload.reason` is required to explain the deviation from the session default.

When the council finalizes with linked authority return already posted,
then transcript/export includes the Discord thread pointer, the locked agenda, all attendance/floor/speech/vote events, and `linked_authority_result.status: posted` with the Kanban comment id, Vault note path, or equivalent evidence.

Given linked authority was configured,
when `council_finalized` lacks `linked_authority_result.status` or lacks required evidence for `posted`,
then linked authority return completion cannot be claimed and transcript/export acceptance fails.

When Kanban/Vault return fails or requires follow-up,
then `council_finalized` still records the council decision but `linked_authority_result.status` is `failed` or `pending_followup`, final reports do not claim return completion, and the origin card remains blocked/pending review or a clearly linked follow-up card is recorded.

Given replay/export rebuilds a Discord-thread-bound council,
when projection is missing `attendance_requested`, terminal `member_attended` for any required participant, or `agenda_locked`,
then replay/export acceptance fails and the implementation must not treat Discord thread messages as replacement state evidence.

Then the daemon must not call the Discord API, create Kanban comments, write Vault notes, use Discord message order for state transitions, or treat the Discord transcript as authoritative. Replay/projection rebuild remains side-effect free and must not convert a configured `linked_authority` target into posted evidence.

## Scenario 8: Single active session enforcement

Given a session has `status: open` and `phase: working`,
when another session is created,
then the daemon rejects the new session and reports the active session ID.

Given a session has `status: blocked` and `phase: blocked`,
when another session is created,
then the daemon still rejects the new session because blocked sessions are active.

Given a session has `status: terminal`,
when another session is created,
then the daemon may create the new session.

## Scenario 9: Transcript completeness

Given a session that has reached a terminal state (`accepted`, `cancelled`, `finalized`, `unresolved`) or is currently `blocked`,
when `atn-control transcript <id> --format md` is run,
then output includes all major events from start to finish.

## Scenario 10: Real member profile evidence

Given a member is configured in the registry,
when the member runtime joins a session,
then it must identify as that registry member, preserve its stream cursor, and use the configured real wrapper or session adapter when it needs model execution.

The system must not silently replace the member with a simulated subagent or a one-shot role prompt.

## Scenario 11: Registry failure

Given the registry is missing, invalid, unsafe, or unreadable,
when the daemon starts or attempts to create a session,
then the operation fails closed with a clear error and no member dispatch occurs.

Unsafe registry examples include:

- `registry.yaml` is a symlink;
- `registry.yaml` is not a regular file;
- `registry.yaml` is group-writable;
- `registry.yaml` is world-writable;
- `registry.yaml` is owned by another non-root user;
- `<data_home>` is group- or world-writable;
- registry contains an unknown adapter kind;
- registry contains unknown keys;
- registry uses a reserved principal as member id.

## Scenario 12: Command idempotency

Given a CLI command is issued with a fixed `command_id`,
when the CLI retries the same command after a transient daemon error,
then the daemon must not execute the underlying action twice and must return the prior recorded result.

The `commands_seen` table must contain exactly one row for that `command_id`. A duplicate `event_id` append must halt writes and raise a corruption alert.

## Scenario 13: Session budget breach

Given a session with `max_usd: 1.00`,
when the accumulated `usd_estimate_total` exceeds the limit,
then the daemon emits `session_budget_exceeded` with `limit_kind: max_usd`, envelope `phase: blocked`, payload `prior_phase` (the phase at the moment of breach), and payload `resume_phase`. Further dispatches are rejected.

Then the SQLite projection and `session.yaml` show:

- `status: blocked`
- `state.phase: blocked`
- `blocked_by_event_id` set to the budget event
- `resume_phase` set to the phase that should be restored after authorization

The session resumes only after a `limits_extended` event with `authorized_by: user` is recorded; the daemon then returns the session to `resume_phase`.

Given a session has `max_runner_calls: 1`,
when the daemon records the first `runner_invocation_started`,
then `runner_calls_total` becomes 1.

When another runner invocation is requested,
then the daemon must not launch the subprocess, must emit `session_budget_exceeded` with `limit_kind: max_runner_calls`, and must transition the session to `blocked`.

Given a runner invocation produces `cost: null`,
then `runner_calls_total` still increments and `missing_cost_runner_calls_total` increments; `tokens_in_total`, `tokens_out_total`, and `usd_estimate_total` do not increment for that invocation.

## Scenario 14: Escalation debounce and cap

Given a session has already recorded 10 user escalations,
when the moderator attempts an 11th escalation,
then the daemon records `escalation_rate_limited` and refuses delivery until `limits_extended` is recorded.

Given two escalations with identical question payload hash within 10 minutes,
when the second is attempted,
then the daemon records `escalation_deduplicated` and does not re-deliver the notification.

Given a low-urgency non-blocking escalation candidate,
when the daemon accepts it for batching,
then the daemon records `escalation_batched`, keeps the session in its prior phase, and does not increment `user_escalations_total`.

Given the batch window expires,
when the daemon flushes the batch,
then it records one `user_escalation_requested` event with `batch: true`, includes all source event ids, increments `user_escalations_total` by one, and transitions the session to `waiting_user`.

## Scenario 15: Artifact ingestion safety

Given a `work_submitted` event references a path outside the member workspace,
when the daemon attempts ingestion,
then the ingestion fails closed with `security_violation` and the session transitions to `blocked`.

Given an artifact exceeds `max_artifact_bytes`,
when the daemon attempts ingestion,
then the ingestion fails closed and no file is copied under `sessions/<id>/artifacts/`.

Given a valid artifact source path is submitted through `atn-control delegate submit --artifact result.md`,
when the daemon ingests the artifact,
then the persisted `work_submitted` event references the ingested artifact record with `artifact_id`, `stored_path`, `sha256`, `size_bytes`, and `mime`. The persisted event must not contain the raw source path as the artifact reference.

## Scenario 16: Runner adapter unknown kind

Given a registry entry with `adapter_kind: unknown-cli`,
when the daemon starts,
then registry validation fails closed at load time and no session creation is accepted.

Given `registry.yaml` has unsafe file permissions,
when the daemon starts,
then file safety validation fails before adapter kind validation is trusted (file safety runs before schema validation per `12-operations.md`).

Given file safety passes but `adapter_kind` is unknown,
when the daemon starts,
then registry validation fails closed at schema validation time.

## Scenario 17: Schema version refusal

Given `channel.jsonl` contains an event with `schema_version` the current reader does not recognize,
when the daemon replays the log,
then the replay refuses the unknown version and reports `migration_required`. The daemon does not continue with a partial projection.

## Scenario 18: Subprocess isolation

Given a member wrapper is invoked,
when the daemon runs the subprocess,
then the command must be invoked with an argv list (no shell), and the process environment must contain only the variables permitted by the global defaults plus the member `env_allowlist`.

A wrapper that is group- or world-writable, resolves outside the configured allowlist, or is a non-regular file must be rejected with the corresponding `security_violation` category — `wrapper_permissions_unsafe`, `wrapper_outside_allowlist`, or `wrapper_unresolvable` respectively (see `12-operations.md`).

Given a valid wrapper exists,
but `registry.yaml` is group-writable,
when the daemon starts or creates a session,
then the daemon rejects the registry before considering the wrapper valid (registry file safety precedes wrapper validation).

## Scenario 19: Stream reconnect and cursor safety

Given agent-1's member runtime has acknowledged cursor `cur_10`,
when the runtime disconnects and reconnects with `atn-control stream <id> --member agent-1 --since cur_10 --follow`,
then the daemon must replay every later event before emitting live events.

If the cursor cannot be reconciled, the stream must fail closed with a clear error. It must not skip events silently.

## Scenario 20: Council without one-shot worker dependency

Given a council session with agent-1 and agent-2,
when `hand_raise_requested` is recorded,
then both member runtimes must observe the event through the stream and decide whether to submit `hand_raise`.

The moderator must not rely on spawning a fresh one-shot subprocess for each member turn as the primary council loop.

## Scenario 21: Event-to-command coverage

Given the protocol defines a participant-originated event,
then `protocol-and-cli.md` must list an explicit CLI command path for that event in the event-to-command coverage matrix.

Given a state-mutating CLI command,
then `protocol-and-cli.md` must list the event type or event sequence emitted by that command.

Given `atn-control delegate message`,
then it must emit only `delegation_message` and must not emit `clarification_answered`.

Given `atn-control delegate answer-clarification`,
then it must emit `clarification_answered` and require `--in-reply-to`, and the resulting event's `causation_event_id` must reference the originating `clarification_requested`.

Given `atn-control council request-vote`,
then it must emit `consensus_vote_requested`, and member runtimes must not vote until they observe that event (or its replay).

Given a daemon-originated operational event such as `session_budget_exceeded`,
then no public write command is required.

Given `user_escalation_requested` can be emitted by immediate escalation, manual batch flush, or daemon timer flush,
then `protocol-and-cli.md` marks it as `mixed`,
and `protocol-and-cli.md` lists both CLI command paths plus daemon runtime batch flush.

Given a low-urgency escalation command is batched,
then the emitted event is `escalation_batched`, not `user_escalation_requested`.

Given a pending batch is flushed manually through `delegate escalation-flush`,
then the emitted event is `user_escalation_requested` and the session enters `waiting_user`.

Given the daemon flushes a pending batch due to timer expiry,
then the emitted event is `user_escalation_requested` with `from: atn-controld` and a valid `causation_event_id`.

## Scenario 22: Phase and status semantics

Given a delegation session is in `working`,
when the daemon appends an `assignee_update`,
then the event envelope has `phase: working` and the projected session has `status: open`.

Given the assignee records `progress_status: blocked` through `atn-control delegate update`,
then the daemon records the self-report but does **not** automatically change the session phase to `blocked`; the projected `status` remains `open`.

Given the moderator records a manual block (`delegate block` → `session_blocked`) or the daemon records a blocking operational event (`session_budget_exceeded`, `escalation_timeout`, session-scoped `security_violation`),
then the event envelope has `phase: blocked` with payload `prior_phase` and `resume_phase`, the projected session has `status: blocked`, and the active-session lock remains held.

Given the moderator records a manual block through `atn-control delegate block`,
then the daemon records `session_blocked` with envelope `phase: blocked` and payload `prior_phase` and `resume_phase` (both required, even when equal).

Given a terminal event such as `work_accepted`, `session_cancelled`, `council_finalized`, or `council_unresolved`,
then the projected session has `status: terminal` and the active-session lock is released.

Given a replay from `channel.jsonl`,
then the daemon derives the same `phase`, `status`, `prior_phase`, `resume_phase`, and `closed_at` projection deterministically.

## Scenario 23: Recipient normalization and projection

Given a CLI command addresses a single recipient with `--to agent-1`,
when the daemon appends the corresponding event,
then the persisted envelope has `"to": ["agent-1"]`, not `"to": "agent-1"`.

Given a council broadcast event targets agent-1, agent-2, and agent-3,
when the daemon appends the event,
then the persisted envelope has an explicit recipient list `"to": ["agent-1", "agent-2", "agent-3"]`.

Given an event is an unaddressed audit event,
when the daemon appends it,
then `"to": []` is allowed, and `event_recipients` contains zero rows for that event.

Given an event uses `"to": "agent-1"` in persisted JSON,
when replay reads it under the Release v1 schema,
then replay fails closed unless a schema migration is registered.

Given an event has `"to": ["agent-1", "agent-1"]`,
when the daemon validates it,
then the daemon normalizes or rejects duplicates per the protocol rule, and the persisted event contains unique recipients.

Given a recipient is unknown and is not the reserved principal `user`,
when a CLI command attempts to address that recipient,
then the daemon rejects the command before append.

Given a registry defines `members.user` or `members.atn-controld`,
when the daemon validates the registry,
then validation fails closed.

Given an event is addressed to `agent-2`,
when `agent-1` is also a session participant and reads the stream,
then stream access is **not** denied solely because `agent-1` is not in `to`.

Given an event with `"to": ["agent-1", "agent-2"]`,
when the daemon appends it,
then `events.recipient_json` stores `["agent-1","agent-2"]` (canonical) and `event_recipients` contains exactly two rows for the event.

## Scenario 24: Runner invocation accounting is independent from cost

Given a bounded runner adapter invocation is about to launch,
when the daemon passes validation and budget checks,
then it first appends `runner_invocation_started` with a unique `runner.invocation_id`.

Given the runner later produces a valid semantic event,
then that event includes the same `runner.invocation_id` and terminal `runner.status: succeeded`.

Given the runner cost parser fails,
then the semantic event has `cost: null`, `runner_calls_total` remains counted from `runner_invocation_started`, and `missing_cost_runner_calls_total` increments.

Given the runner times out before producing a semantic event,
then the daemon appends `runner_invocation_failed` with the same `runner.invocation_id`.

Given a retry is attempted,
then `runner_retry_attempted` records the retry policy decision, and the actual retry call is counted only when a new `runner_invocation_started` event is appended (with a new `runner.invocation_id`).

Given a runner result arrives after session cancellation,
then the daemon records `runner_result_discarded` with the original `runner.invocation_id`, and the invocation remains counted.

Given replay rebuilds SQLite from `channel.jsonl`,
then `sessions.runner_calls_total`, `sessions.missing_cost_runner_calls_total`, and `runner_invocations` are reconstructed deterministically without re-running any runner.

Given `runner_invocation_started` and `runner_invocation_failed` are protocol events,
then the CLI operational matrix lists them as `daemon_internal` events requiring no public write command.

## Scenario 25: Escalation batching does not enter waiting_user

Given a delegation session is in `working`,
and the moderator records a low-urgency non-blocking escalation candidate,
when the daemon batches the candidate,
then the daemon emits `escalation_batched`, and the session phase remains `working`.

Given the pending batch has not been flushed,
then `user_escalations_total` is unchanged, and the moderator must not deliver anything to the user because no `user_escalation_requested` exists.

When the batch is flushed (timer expiry, higher-urgency arrival, `delegate escalation-flush`, or phase-change pressure),
then the daemon emits one `user_escalation_requested` with `batch: true`, sets phase to `waiting_user`, and starts `escalation_response_timeout_sec` from that event (not from `escalation_batched`).

When the moderator records `user_escalation_delivered`,
then the session remains `waiting_user`.

When the user answer is recorded through `user_escalation_resolved`,
then the session returns to the recorded `resume_phase`, and the moderator relays each answer through `clarification_answered --source user`.

Given a flush that would exceed `max_user_escalations`,
when the daemon evaluates the cap,
then it emits `escalation_rate_limited` (no `user_escalation_requested`, phase unchanged) until `limits_extended` is recorded.

When replay rebuilds projections from `channel.jsonl`,
then pending batch status, included source events, deadline, `user_escalations_total`, and waiting-user state are reconstructed deterministically. Replay does **not** create a new `user_escalation_requested` merely because a batch deadline is in the past.

## Scenario 26: Registry file permission safety

Given `<data_home>` exists and is group-writable,
when the daemon starts,
then the daemon fails closed with `registry_data_home_unsafe` and does not accept session creation.

Given `<data_home>/registry.yaml` is a symlink,
when the daemon validates the registry,
then validation fails with `registry_symlink_forbidden`.

Given `<data_home>/registry.yaml` is owned by another non-root user,
when the daemon validates the registry,
then validation fails with `registry_owner_unsafe`.

Given `<data_home>/registry.yaml` is group-writable or world-writable,
when the daemon validates the registry,
then validation fails with `registry_permissions_unsafe`.

Given the registry file is safe and schema-valid,
when the moderator creates a session,
then the daemon writes `registry_snapshot.yaml` atomically before appending `session_created`.

Given registry snapshot writing fails,
when the moderator creates a session,
then session creation aborts with `registry_snapshot_write_failed` before `session_created` and no active session lock is taken.

Given a session already exists,
when live `registry.yaml` is edited later,
then the active session continues to use its frozen `registry_snapshot.yaml`.

Given replay rebuilds projections,
then replay does not reinterpret historical participants from the current live `registry.yaml`.

Given a pre-session registry validation failure occurs,
then the violation is recorded in `<data_home>/operational.log`.

Given a session-scoped snapshot validation failure occurs during active dispatch,
then the violation is recorded as `security_violation` in that session's `channel.jsonl`.

## Scenario 27: Common block and resume

Given a delegation session is in `working`,
when the moderator records a manual block through `atn-control block`,
then the daemon records `session_blocked` with envelope `phase: blocked`, payload `prior_phase`, and payload `resume_phase`.

Given the blocking condition is resolved,
when the moderator records `atn-control resume --blocked-event <event_id>`,
then the daemon records `session_resumed`, returns the session to the recorded `resume_phase`, and clears blocked projection fields.

Given the block was caused by `session_budget_exceeded`,
when the moderator attempts `atn-control resume`,
then the daemon rejects it and requires `atn-control limits extend` with user authorization.

Given a council session is in `discussion`,
when the moderator records `atn-control block`,
then the council enters `blocked` and still holds the active-session lock.

When the moderator records `atn-control resume`,
then the council returns to the recorded `resume_phase`.

Given `atn-control delegate block` is used in place of the common command for a delegation session,
then the daemon records the same `session_blocked` event; both command paths are accepted.

## Scenario 28: Daemon-originated partial preparation

Given a council member does not record `member_ready` before preparation timeout,
when the daemon records `member_prepared_partial`,
then the event has `from: "atn-controld"` and `payload.member` contains the timed-out member id.

Given a member runtime explicitly records partial preparation through `atn-control council prepared-partial`,
then the event has `from` equal to the member id, omits `payload.member`, and does not pretend to be daemon-originated.

## Scenario 29: Structured CLI errors

Given a blocked session holds the active-session lock,
when another session is created with `--format json`,
then the CLI returns a JSON error envelope with code `ACTIVE_SESSION_EXISTS`, the active session id, status, phase, blocked event id when available, and suggested next commands.

Given registry validation fails,
when the command is run with `--format json`,
then the CLI returns a JSON error envelope with a stable code and category matching the security violation category, and exit code 3.

Given an `atn-control resume` command targets a budget-originated block,
when the command is run with `--format json`,
then the CLI returns a JSON error envelope with category `session_lock`, a code identifying the budget mismatch, and `next` listing `atn-control limits extend ...` instead of `atn-control resume`.

---

## Merged legacy testing/tooling content

# Testing Strategy

## Scope

This document defines the test layers for ATN control/runtime and the Makefile target contract shared with the plugin repository.

## Makefile target contract

Both repositories must expose these targets:

| Target | Purpose | External resources |
| --- | --- | --- |
| `test-prepare` | formatting, lint, vet/typecheck, docs guardrails, static safety checks | forbidden |
| `test-unit` | isolated unit tests for functions/types/domain logic | forbidden |
| `test-int` | integration between internal components using mock/fake/stub dependencies | forbidden |
| `test-e2e` | real external integration tests against isolated test resources | allowed only in test environment |
| `test` | sequentially runs all targets above | follows each target |

The control repo also exposes `test-release-acceptance` for RELIA-001 local storage/replay/recovery/observability evidence. That target is not a plugin-owned requirement unless a later plugin task adds compatible local evidence. Control `make test` runs `test-prepare`, `test-unit`, `test-int`, `test-release-acceptance`, then `test-e2e`; plugin `make test` may omit release acceptance until it owns such evidence.

## Control test layers

| Layer | Target | Examples |
| --- | --- | --- |
| Unit | protocol, engine, registry, security helpers | phase transitions, strict schema, safe path validation |
| Unit | storage primitives | event envelope validation, cursor math, redaction helper |
| Integration | daemon + storage + CLI using temp data home | append/replay/projection, storage verify/rebuild exit codes, idempotency, JSON errors |
| Release acceptance | local CLI/storage/doctor fault matrix | corrupt logs, snapshot failures, unsafe paths, side-effect-free rebuild, active-session recovery |
| Integration | fake member/runtime/runner | stream reconnect, cursor ack, timeout, cost parsing |
| E2E | isolated Hermes/Discord test environment | plugin-visible session flow, Discord delivery evidence in a sandbox thread |
| Fault injection | failure paths | truncation, projection corruption, late runner result, incompatible protocol |
| Load | local performance | replay 10k/100k events, stream fanout |

## External-resource rule

`test-prepare`, `test-unit`, and `test-int` must not contact live Hermes profiles, the current Hermes gateway, production Discord, network APIs, or user workspaces. They use temporary directories, fake wrappers, fake gateways, and deterministic clocks.

`test-e2e` may contact real external systems only when explicitly configured for an isolated test environment. Required safeguards:

- use a disposable `HERMES_HOME`/profile home, never the current running Hermes profile;
- use a dedicated test Discord guild/channel/thread or a fake gateway unless `DISCORD_TEST_TARGET` is set;
- never post to 주군's active production thread by default;
- clean up or clearly label test artifacts;
- fail closed when test credentials/targets are absent.

## Required fixtures

- `temp_data_home` with safe permissions.
- `safe_registry` and unsafe registry variants.
- projection fixtures with missing, mismatched, corrupt, and rebuilt `network.sqlite`.
- fake Hermes wrapper that returns deterministic semantic output and optional cost JSON.
- fake runner timeout/nonzero/malformed-output variants.
- fake stream client with durable cursor file.
- RUNRT local dispatcher fakes that assert append-before-launch, retry accounting, null-cost failures, and late-result discard without contacting live Hermes.
- deterministic clock.
- event and command envelope factory.
- conformance fixture loader shared with plugin tests.

## Conformance tests

Control conformance fixtures are stored under `testdata/conformance/` once code scaffolding begins. They cover:

- command envelope validation;
- event envelope validation;
- stream frame replay/follow semantics;
- structured errors;
- version/feature compatibility responses;
- delivery evidence commands for Discord/helper surfaces;
- DELEG-001 local/fake delegation and review-gate behavior, including canonical `cancel` / `session_cancelled` coverage;
- DELEG-002 plugin-consumable delegation/review command envelopes, structured-error fixtures, duplicate/idempotency policy, permission/error examples, retryable failure policy, and malformed-response fail-closed policy;
- local/fake RUNRT runner event envelopes (`runner_invocation_started`, `runner_invocation_failed`, terminal semantic runner events, and `runner_result_discarded`);
- TRANS-001 transcript/export command envelopes, deterministic renderer golden coverage, local bundle contents, and plugin handoff fixture checks.

Transcript/export tests must cover deterministic Markdown and JSONL rendering, output path rejection, missing/corrupt session errors, export bundle contents, read-only fingerprints for `status`/`transcript`/`export`/`tail`, council linked-authority evidence, delegation/review evidence, terminal/blocked state rendering, runner/cost summaries, and selected-runner terminal accounting where runner failure/discard/dispatch failure blocks `selected_runner_pass` even if later runnerless/manual/fallback speech exists.

The plugin repository must run its Python client against either copied fixtures or a temporary daemon built from this repo.

## DELEG-001 local verification scope

DELEG-001 tests are local/fake only. The control repo verifies:

- daemon/CLI/storage delegation lifecycle commands from `delegate new` through acknowledgement, clarification, messaging, updates, submit, review/revise/accept, block/resume, escalation audit, and canonical `cancel`;
- fail-closed actor, recipient, phase, causation, duplicate command-id, unsafe artifact, malformed review finding, terminal cancel/accept, and budget-block resume validation;
- projection/replay behavior for review rows, artifact references, blocked metadata, `limits_extended` unblocking, terminal `cancelled` status, `closed_at`, and active-session lock release;
- local/fake evidence for delegation/review command, event, response, and structured-error behavior. Plugin-consumable fixture publication is completed by DELEG-002.

Passing DELEG-001 tests does **not** imply live Hermes, Discord, KAB, gateway, or plugin readiness.

## DELEG-002 conformance fixture publication scope

DELEG-002 tests must keep the fixture contract plugin-consumable without turning plugin assumptions into control authority. The control repo verifies:

- `testdata/conformance/manifest.json` references only valid fixture entries unless an explicit invalid-fixture policy is added;
- delegation/review success request/response examples are available for the canonical command models needed by plugin DELRV-2, including `delegate.new`, `delegate.review`, `delegate.review_submit`, and canonical non-review `delegate.accept`;
- duplicate/idempotency behavior is represented by one explicit control-owned response shape; the `delegate.submit` duplicate fixture is representative of the general `command_id` idempotency rule, not submit-only behavior;
- permission and validation errors use safe structured-error fields with no secrets or live identifiers;
- retryable failure exposure is either implemented as a public structured-error fixture or explicitly documented as outside the public command-response contract;
- malformed daemon payload handling is documented as fail-closed negative-test policy and is not silently treated as a valid success shape;
- cross-repo checks still pass without contacting live Hermes, Discord, KAB, gateway, auth, token, or external daemon resources.

## RUNRT-001 local verification scope

RUNRT-001 tests are local/fake only. The control repo verifies:

- `internal/runner` adapter registration, wrapper argv invocation, env allowlist propagation, timeout behavior, and Hermes stderr cost parsing from the last 32 KB;
- `internal/memberruntime` replay-first stream consumption, action filtering that does not use `to` as visibility control, cursor ack/persistence ordering, fail-closed cursor/frame/schema handling, and same-member dispatch serialization;
- `internal/daemon` bounded runner dispatch seams with append-before-launch accounting, retry events with new invocation ids, explicit `cost: null` failures, adapter-kind rejection before dispatch, and cancellation/late-result discard coverage;
- storage/projection accounting where `runner_calls_total` comes only from `runner_invocation_started`, token/USD totals come only from terminal cost objects, and `missing_cost_runner_calls_total` comes from terminal `cost: null`.

These tests must use temp data homes, fake wrappers/adapters/streams, and deterministic clocks. Passing RUNRT-001 tests does **not** imply live Hermes, Discord, KAB, gateway, or plugin readiness.

## MEMBR-001 docs gate and MEMBR-002 test shape

MEMBR-001 is docs-only. Its verification is limited to documentation guardrails and contract checks; it does not run real member profiles, activate daemons, execute KAB, mutate gateway/auth/token/provider/profile state, or claim production/live readiness.

MEMBR-002 owns implementation and proof for the selected participant invocation pilot. The first proof mode is main-agent mediated bounded runner invocation as a disposable local step before long-lived member runtimes. Local tests must use fake or isolated wrappers first, preserve real profile/wrapper identity in the evidence model, and fail closed on registry mismatch, missing wrapper, unsafe profile, missing evidence, command id conflict, timeout, unsupported transport, cursor gaps, or schema gaps. Real-profile evidence is permitted only when explicitly authorized and must record redacted artifact/log pointers rather than secret-bearing inline output.

## RELIA-001 release acceptance scope

Control `make test-release-acceptance` is RELIA-001 local evidence for the control repo. It runs deterministic temp-data-home tests and must not contact live Hermes, Discord, KAB, gateway, auth, token, production install, or plugin-load resources. It is not a plugin-owned target unless a later plugin task adds compatible local evidence.

The current release acceptance suite verifies:

- `channel.jsonl` corruption fails closed for truncated tail, malformed mid-file JSON, duplicate `event_id`, and unsupported `schema_version`.
- `registry_snapshot.yaml` missing or corrupt fails replay/rebuild closed, and live `registry.yaml` mutation does not reinterpret existing sessions.
- `storage verify` reports missing projection as recoverable projection-only evidence, `storage rebuild-projection` rebuilds it, and unsafe projection paths fail closed.
- Rebuild is side-effect free: it does not append events, invoke runner rows, synthesize timer/timeout events, or record outbound delivery events.
- Active-session recovery is derived from durable lifecycle events, so stale terminal/open metadata does not override `channel.jsonl`.

The suite is not live readiness. It does not prove plugin load, production Discord delivery, Hermes profile execution, KAB review, credentials, gateway config, or production install readiness.

Heavy replay/load tests are outside the default release acceptance target unless bounded to practical local runtime. A 100k-event replay check must remain opt-in or explicitly skipped with evidence.

## CI guidance

- `test-prepare`, `test-unit`, `test-int`, and `test-release-acceptance` run on every commit/PR.
- `test-e2e` runs only when isolated external resources are configured.
- E2E absence is a skipped environment, not silent success, once tests exist.
- A failed test is fixed at the owning boundary; tests are not weakened to pass broken behavior.

---

## Merged legacy testing/tooling content

# Tooling

## Scope

This document defines the control repository toolchain after the repo split. The control daemon and CLI are implemented in Go. The Python Hermes plugin tooling lives in `../../agent-turn-network-plugin/docs/spec/testing-and-tooling.md`.

## Baseline decisions

| Item | Decision |
| --- | --- |
| Control language | Go |
| Binaries | `atn-controld`, `atn-control` |
| Source layout | `cmd/`, `internal/`, `pkg/` only if public API is needed |
| Protocol fixtures | `testdata/conformance/` |
| Test runner | `go test` |
| Formatting | `gofmt` |
| Vet/static checks | `go vet`; optional `golangci-lint` when configured |
| Operator entrypoint | `Makefile` |

## Target layout

```text
atn-control/
  go.mod
  cmd/
    atn-control/
      main.go
    atn-controld/
      main.go
  internal/
    command/
    cli/
    daemon/
    memberruntime/
    engine/
    observability/
    protocol/
    recovery/
    registry/
    runner/
    storage/
    transcript/
    transport/
  testdata/
    conformance/
  tests/
    integration/
    e2e/
  docs/
  Makefile
```

## Makefile targets

```bash
make test-prepare
make test-unit
make test-int
make test-e2e
make test
```

`test-prepare` performs `gofmt` checks, lint, `go vet`, and docs/guardrail checks. It must not use external resources.

`test-unit` runs unit tests only.

`test-int` runs integration tests using fake runners, fake member-runtime streams, fake gateways, temporary data homes, and deterministic clocks. It must not use external Hermes, Discord, KAB, gateway, or plugin resources.

`test-e2e` runs real external integration only when a test environment is explicitly configured. It must not touch the currently running Hermes profile/gateway or production Discord rooms.

`test` runs the four targets sequentially.

## Bootstrap smoke tests

The first Go scaffold PR must prove:

- `go test ./...` passes.
- `go vet ./...` passes.
- `gofmt` reports no changed files.
- `atn-control --help` exits 0.
- `atn-controld --help` exits 0.
- `make test` succeeds without external resources in docs/scaffold mode.

## Guardrails

The control repo docs must not reintroduce pre-split Python-core assumptions. Guardrails should reject stale wording that says the control repo is a Python package or that CLI/plugin share a Python client implementation. The valid split is: Go control runtime, Python plugin, shared protocol contract, conformance tests.

---

## Merged legacy testing/tooling content

# Release v1 Acceptance

## Scope

Release v1 acceptance is a local control-repo gate. It provides deterministic evidence for storage, replay, observability, and recovery behavior in the Go control runtime using temporary data homes and fake/local fixtures.

It does not prove live plugin load, live Discord delivery, Hermes profile execution, KAB review, gateway config, credentials, tokens, auth, or production install readiness.

## Local gate

Run:

```bash
GOCACHE=/tmp/kkachi-go-build-cache make test-release-acceptance
```

The target runs:

```bash
KAN_TEST_MODE=release KAN_EXTERNAL=0 go test ./internal/command -run 'TestReleaseAcceptance' -count=1
```

It is included in `make test` because it is deterministic and local-only.

## Evidence covered

The release acceptance suite covers:

- `channel.jsonl` corruption: truncated final line, malformed mid-file JSON, duplicate `event_id`, and unsupported `schema_version`.
- Registry snapshot safety: missing or corrupt `registry_snapshot.yaml` fails closed, and replay/rebuild does not reinterpret historical sessions from a mutated live `registry.yaml`.
- Projection recovery: missing projection reports `recoverable_projection_only`, rebuild creates a valid projection, and unsafe projection paths fail closed.
- Replay/rebuild purity: rebuild does not append events, create runner rows, record outbound delivery events, or synthesize timer/timeout events.
- Active-session recovery: replay-derived lifecycle state overrides stale `session.yaml` phase/status in both stale-terminal and stale-open directions.
- Observability surfaces: `storage verify`, `doctor`, `daemon status`, root `status`, and `daemon health` provide actionable local diagnostics, remain read-only, and do not leak secrets or raw registry names.

Supporting tests outside the release target also cover operational-log redaction for unsafe registry startup rejection, unsafe data-home rejection without log writes, and storage verify/rebuild exit-category mapping.

## Required verification spine

Local release-readiness evidence should record these commands separately:

```bash
HOME=/Users/draccoon kkachi-agent-helper graph validate --json
HOME=/Users/draccoon kkachi-agent-helper project doctor --json
GOCACHE=/tmp/kkachi-go-build-cache codegraph status
GOCACHE=/tmp/kkachi-go-build-cache make check-plugin-contract
GOCACHE=/tmp/kkachi-go-build-cache make test-prepare
GOCACHE=/tmp/kkachi-go-build-cache make test
GOCACHE=/tmp/kkachi-go-build-cache make test-release-acceptance
GOCACHE=/tmp/kkachi-go-build-cache go test ./internal/storage ./internal/command ./internal/daemon -run 'Release|Reliability|Storage|Doctor|Replay|Corrupt|Lock' -count=1
git diff --check
```

Any skipped command must be reported as a verification gap. Passing this local spine still must not be described as live plugin, Discord, Hermes, KAB, credential, gateway, or production readiness.

## Load split

Default release acceptance keeps load bounded. A large 100k-event replay check is opt-in and should be recorded as separate load evidence or an explicit skip with reason.
