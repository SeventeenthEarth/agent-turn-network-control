# KAN Conformance Fixtures

This directory contains core-owned protocol fixtures consumed by `kkachi-agent-network-plugin`.

Current status: DAEMN-002 plus DELEG-002 static/local conformance set. The files here define the shared protocol examples for command envelopes, event envelopes, structured errors, stream frames, version/features, delivery evidence, and delegation/review command handoff.

- Manifest: `manifest.json`
- Protocol version: `kan-protocol-v1alpha0`
- Schemas: `schemas/*.schema.json`
- Fixtures: `fixtures/{command,event,error,stream,version}/`
- Canonical stream command fixture: `stream.replay`
- Canonical DELEG-002 non-review action fixture: `delegate.accept`

These fixtures are static only. They do not start a daemon and do not contact Hermes, Discord, KAB, auth, token, gateway, localhost, or other live services. The plugin may copy fixtures for pinned tests, but the control manifest is the compatibility source of truth.

## DELEG-002 fixture matrix

DELEG-002 publishes plugin-consumable delegation/review examples without adding a new feature group. `required_feature_groups` remains limited to the existing protocol groups until a future task backs a delegation/review feature advertisement with runtime, fixture, test, and docs evidence.

| Behavior | Fixture paths | Public contract |
| --- | --- | --- |
| Delegation creation | `fixtures/command/delegate-new-request.json`, `fixtures/command/delegate-new-response.json`, `fixtures/event/task-assigned-delegation.json` | `delegate.new` creates a delegation session and returns `ok: true`, `result.session_id`, `result.results[]`, and `result.deduplicated`. |
| Work submission | `fixtures/command/delegate-submit-request.json`, `fixtures/command/delegate-submit-response.json`, `fixtures/event/work-submitted.json` | `delegate.submit` appends `work_submitted` and returns the standard append result shape: `cursor`, `event_id`, `offset`, `deduplicated`. |
| Duplicate/idempotent command | `fixtures/command/delegate-submit-duplicate-request.json`, `fixtures/command/delegate-submit-duplicate-response.json` | The submit duplicate is representative of general `command_id` idempotency across append-style commands, not a submit-only rule. Replaying the same logical command with the same `command_id` returns `ok: true`, the prior append result, and `result.deduplicated: true`. |
| Review request | `fixtures/command/delegate-review-request.json`, `fixtures/command/delegate-review-response.json`, `fixtures/event/review-requested.json` | `delegate.review` moves submitted work into `under_review` and appends `review_requested`. |
| Review submission | `fixtures/command/delegate-review-submit-request.json`, `fixtures/command/delegate-review-submit-response.json`, `fixtures/event/review-submitted.json` | `delegate.review_submit` records a stable verdict/finding payload and appends `review_submitted`. |
| Canonical non-review finalization | `fixtures/command/delegate-accept-request.json`, `fixtures/command/delegate-accept-response.json`, `fixtures/event/work-accepted.json` | `delegate.accept` is the canonical non-review delegation action for plugin fixture tests because it is an existing runtime command path and terminally appends `work_accepted`. |
| Permission/validation errors | `fixtures/error/delegate-unauthorized-actor.json`, `fixtures/error/delegate-review-wrong-phase.json`, `fixtures/error/delegate-review-submit-invalid-verdict.json` | Delegation/review failures use normal structured command errors: `ok: false`, `error.code: validation_error`, `error.category: validation`, safe `message`, and safe `details`. |

## Retry and malformed-response policy

DELEG-002 does not publish a public retryable delegation/review command-response shape. If a plugin fake daemon wants to exercise retry preservation, that retryability is a plugin-local negative-test input and must not be treated as a stable control daemon contract unless a future control fixture explicitly publishes it.

Malformed JSON, schema-invalid daemon payloads, truncated responses, and unexpected response envelopes are also plugin-side fail-closed negative-test inputs. They must not be listed in ordinary `manifest.json` fixtures, because every manifest entry is expected to be valid and parseable by control conformance tests and `make check-plugin-contract`.

Plugin consumers should derive fail-closed behavior from the command-envelope and structured-error schemas here: accept only parseable known-good shapes from the manifest, reject malformed or schema-invalid fake-daemon payloads locally, and keep control as the daemon/runtime authority.
