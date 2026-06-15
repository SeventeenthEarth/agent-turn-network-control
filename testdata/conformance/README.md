# KAN Conformance Fixtures

This directory contains core-owned protocol fixtures consumed by `kkachi-agent-network-plugin`.

Current status: DAEMN-002 plus DELEG-002, COUNC-001, and ARGUE-002 static/local conformance sets. The files here define the shared protocol examples for command envelopes, event envelopes, structured errors, stream frames, version/features, delivery evidence, delegation/review command handoff, council lifecycle handoff, and council argument-graph handoff.

- Manifest: `manifest.json`
- Protocol version: `kan-protocol-v1alpha0`
- Schemas: `schemas/*.schema.json`
- Fixtures: `fixtures/{command,event,error,stream,version}/`
- Canonical stream command fixture: `stream.replay`
- Canonical DELEG-002 non-review action fixture: `delegate.accept`
- Canonical COUNC-001 feature group: `council.lifecycle`
- Canonical ARGUE-002 linked stance enum: `support`, `challenge`, `refine`, `extend`, `synthesize`, `question`, `risk_addition`, `decision_frame`

These fixtures are static only. They do not start a daemon and do not contact Hermes, Discord, KAB, auth, token, gateway, localhost, or other live services. The plugin may copy fixtures for pinned tests, but the control manifest is the compatibility source of truth.

## DELEG-002 fixture matrix

DELEG-002 publishes plugin-consumable delegation/review examples without adding a delegation-specific feature group. `required_feature_groups` includes `council.lifecycle` only because COUNC-001 now has runtime, fixture, test, and docs evidence for council lifecycle handoff.

| Behavior | Fixture paths | Public contract |
| --- | --- | --- |
| Delegation creation | `fixtures/command/delegate-new-request.json`, `fixtures/command/delegate-new-response.json`, `fixtures/event/task-assigned-delegation.json` | `delegate.new` creates a delegation session and returns `ok: true`, `result.session_id`, `result.results[]`, and `result.deduplicated`. |
| Work submission | `fixtures/command/delegate-submit-request.json`, `fixtures/command/delegate-submit-response.json`, `fixtures/event/work-submitted.json` | `delegate.submit` appends `work_submitted` and returns the standard append result shape: `cursor`, `event_id`, `offset`, `deduplicated`. |
| Duplicate/idempotent command | `fixtures/command/delegate-submit-duplicate-request.json`, `fixtures/command/delegate-submit-duplicate-response.json` | The submit duplicate is representative of general `command_id` idempotency across append-style commands, not a submit-only rule. Replaying the same logical command with the same `command_id` returns `ok: true`, the prior append result, and `result.deduplicated: true`. |
| Command-id conflict | `fixtures/error/command-id-conflict.json` | Reusing the same `command_id` with a different payload fails closed with a structured `validation_error` and `details.path: command_id`; this is the paired negative contract for duplicate/idempotent retry. |
| Review request | `fixtures/command/delegate-review-request.json`, `fixtures/command/delegate-review-response.json`, `fixtures/event/review-requested.json` | `delegate.review` moves submitted work into `under_review` and appends `review_requested`. |
| Review submission | `fixtures/command/delegate-review-submit-request.json`, `fixtures/command/delegate-review-submit-response.json`, `fixtures/event/review-submitted.json` | `delegate.review_submit` records a stable verdict/finding payload and appends `review_submitted`. |
| Canonical non-review finalization | `fixtures/command/delegate-accept-request.json`, `fixtures/command/delegate-accept-response.json`, `fixtures/event/work-accepted.json` | `delegate.accept` is the canonical non-review delegation action for plugin fixture tests because it is an existing runtime command path and terminally appends `work_accepted`. |
| Permission/validation errors | `fixtures/error/delegate-unauthorized-actor.json`, `fixtures/error/delegate-review-wrong-phase.json`, `fixtures/error/delegate-review-submit-invalid-verdict.json` | Delegation/review failures use normal structured command errors: `ok: false`, `error.code: validation_error`, `error.category: validation`, safe `message`, and safe `details`. |

## COUNC-001 fixture matrix

COUNC-001 publishes plugin-consumable council examples for static fake-daemon and parser tests. The fixtures are deterministic examples of local command/event/error envelopes; they do not claim live Discord thread readiness.

The manifest intentionally includes request/response fixtures for the full public council command family from `docs/04-cli-spec.md`:

- `council.new`
- `council.request_attendance`
- `council.attend`
- `council.lock_agenda`
- `council.prepare`
- `council.ready`
- `council.prepared_partial`
- `council.poll`
- `council.hand_raise`
- `council.grant`
- `council.speak`
- `council.intervene`
- `council.propose`
- `council.revise`
- `council.request_vote`
- `council.vote`
- `council.finalize`
- `council.unresolved`

| Behavior | Fixture paths | Public contract |
| --- | --- | --- |
| Council creation | `fixtures/command/council-new-request.json`, `fixtures/command/council-new-response.json`, `fixtures/event/session-created-council.json` | `council.new` creates a council session and returns `ok: true`, `result.session_id`, one `session_created` append result, and `result.deduplicated`. |
| Attendance and agenda lock | `fixtures/command/council-request-attendance-request.json`, `fixtures/command/council-request-attendance-response.json`, `fixtures/command/council-attend-request.json`, `fixtures/command/council-attend-response.json`, `fixtures/command/council-lock-agenda-request.json`, `fixtures/command/council-lock-agenda-response.json`, `fixtures/event/attendance-requested-council.json`, `fixtures/event/member-attended-council.json`, `fixtures/event/agenda-locked-council.json` | Attendance and agenda commands append `attendance_requested`, `member_attended`, and `agenda_locked` as static local evidence before preparation. |
| Preparation readiness | `fixtures/command/council-prepare-request.json`, `fixtures/command/council-prepare-response.json`, `fixtures/command/council-ready-request.json`, `fixtures/command/council-ready-response.json`, `fixtures/command/council-prepared-partial-request.json`, `fixtures/command/council-prepared-partial-response.json`, `fixtures/event/preparation-requested-council.json`, `fixtures/event/member-ready-council.json`, `fixtures/event/member-prepared-partial-council.json` | `council.prepare` appends member-broadcast `preparation_requested`; member readiness commands append moderator-addressed preparation evidence. |
| Discussion turns | `fixtures/command/council-poll-request.json`, `fixtures/command/council-poll-response.json`, `fixtures/command/council-hand-raise-request.json`, `fixtures/command/council-hand-raise-response.json`, `fixtures/command/council-grant-request.json`, `fixtures/command/council-grant-response.json`, `fixtures/command/council-speak-request.json`, `fixtures/command/council-speak-response.json`, `fixtures/command/council-intervene-request.json`, `fixtures/command/council-intervene-response.json`, `fixtures/event/hand-raise-requested-council.json`, `fixtures/event/hand-raise-council.json`, `fixtures/event/speaker-selected-council.json`, `fixtures/event/speech-council.json`, `fixtures/event/moderator-intervention-council.json` | Poll, hand-raise, grant, speak, and intervention fixtures cover turn-scoped discussion events and explicit recipients. |
| Drafting and revision | `fixtures/command/council-propose-request.json`, `fixtures/command/council-propose-response.json`, `fixtures/command/council-revise-request.json`, `fixtures/command/council-revise-response.json`, `fixtures/event/draft-conclusion-council.json`, `fixtures/event/draft-conclusion-revised-council.json` | `council.propose` and `council.revise` append `draft_conclusion`; the revision fixture records `revision_reason` and `supersedes_draft_version`. |
| Vote request and vote | `fixtures/command/council-request-vote-request.json`, `fixtures/command/council-request-vote-response.json`, `fixtures/command/council-vote-request.json`, `fixtures/command/council-vote-response.json`, `fixtures/event/consensus-vote-requested-council.json`, `fixtures/event/consensus-vote-council.json` | `council.request_vote` opens a consensus round for the current draft; `council.vote` records member votes with reason payloads. |
| Terminal outcomes | `fixtures/command/council-finalize-request.json`, `fixtures/command/council-finalize-response.json`, `fixtures/command/council-unresolved-request.json`, `fixtures/command/council-unresolved-response.json`, `fixtures/event/council-finalized.json`, `fixtures/event/council-unresolved.json` | `council.finalize` and `council.unresolved` are terminal append command fixtures for completed and unresolved councils. |
| Permission/guard errors | `fixtures/error/council-missing-attendance-agenda.json`, `fixtures/error/council-invalid-principal.json` | Council failures use normal structured command errors: `ok: false`, `error.code: validation_error`, `error.category: validation`, safe `message`, and safe `details`. |

## ARGUE-002 fixture matrix

ARGUE-002 publishes plugin-consumable static examples for council argument-graph payload shape. These positive fixtures are graph-shape snippets, not complete lifecycle replay sequences; they intentionally reuse the conformance council envelope to show payload compatibility without proving full turn replay. They are additive to COUNC-001 lifecycle fixtures and do not prove daemon validation, moderator scoring, transcript rendering, export rendering, plugin implementation, or live discussion quality.

`new_axis` is represented as `speech.payload.contribution_type: "new_axis"` plus `new_axis_reason`; it is not a linked stance value. Linked `stance_links[].stance` and hand-raise `target_links[].stance` use the stable linked-stance enum: `support`, `challenge`, `refine`, `extend`, `synthesize`, `question`, `risk_addition`, `decision_frame`.

| Behavior | Fixture paths | Public contract |
| --- | --- | --- |
| Opening new axis | `fixtures/event/argument-graph-opening-new-axis-council.json` | A first speech may publish `claims[]`, `contribution_type: "new_axis"`, and `new_axis_reason` without `stance_links[]`. |
| Prior support | `fixtures/event/argument-graph-support-prior-council.json` | A later speech may support an earlier claim with `stance_links[]` containing `target_event_id`, `target_claim_id`, `stance`, and `rationale`. |
| Multi-link challenge/support | `fixtures/event/argument-graph-multi-link-council.json`, `fixtures/command/council-speak-argument-graph-request.json` | One speech may support one prior claim and challenge another, including a non-immediately previous target. |
| Synthesis | `fixtures/event/argument-graph-synthesize-council.json` | `contribution_type: "synthesize"` is represented with at least two linked prior claim targets. |
| Legacy compatibility | `fixtures/event/argument-graph-dual-field-speech-council.json` | `responds_to_event_id` may appear beside `stance_links[]`; ARGUE-aware consumers treat `stance_links[]` as relation authority. |
| Legacy-only compatibility | `fixtures/event/argument-graph-legacy-only-speech-council.json` | A speech may expose legacy `responds_to_event_id` without `stance_links[]` so older clients can preserve compatibility without inventing ARGUE semantics. |
| Risk and decision frame stances | `fixtures/event/argument-graph-risk-decision-frame-council.json` | Linked stance and contribution vocabulary includes `risk_addition` and `decision_frame` alongside support/challenge/refine/extend/synthesize/question. |
| Hand-raise intent links | `fixtures/event/argument-graph-hand-raise-target-links-council.json`, `fixtures/command/council-hand-raise-argument-graph-request.json` | Hand raises use `target_links[]` objects instead of parallel target arrays for intended event/claim/stance pairing. |
| Static negative examples | `fixtures/error/argument-graph-invalid-stance.json`, `fixtures/error/argument-graph-future-reference.json`, `fixtures/error/argument-graph-cross-session-reference.json`, `fixtures/error/argument-graph-unknown-target-claim.json`, `fixtures/error/argument-graph-new-axis-missing-reason.json`, `fixtures/error/argument-graph-synthesize-single-target.json`, `fixtures/error/argument-graph-quality-required-missing-claims.json`, `fixtures/error/argument-graph-runtime-noise-speech.json`, `fixtures/error/argument-graph-quality-required-orphan-speech.json` | Negative examples are valid structured command responses for parser/fail-closed tests. They are static protocol examples only; ARGUE-003 owns runtime validation and scoring behavior. |

## Retry and malformed-response policy

DELEG-002 does not publish a public retryable delegation/review command-response shape. If a plugin fake daemon wants to exercise retry preservation, that retryability is a plugin-local negative-test input and must not be treated as a stable control daemon contract unless a future control fixture explicitly publishes it.

Malformed JSON, schema-invalid daemon payloads, truncated responses, and unexpected response envelopes are also plugin-side fail-closed negative-test inputs. They must not be listed in ordinary `manifest.json` fixtures, because every manifest entry is expected to be valid and parseable by control conformance tests and `make check-plugin-contract`.

Plugin consumers should derive fail-closed behavior from the command-envelope and structured-error schemas here: accept only parseable known-good shapes from the manifest, reject malformed or schema-invalid fake-daemon payloads locally, and keep control as the daemon/runtime authority.
