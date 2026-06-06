# DELEG-002 Conformance Fixture Matrix

## Purpose

`DELEG-002 | Delegation/review conformance fixture matrix` is the control-owned handoff for plugin-consumable delegation/review conformance fixtures after `DELEG-001` implemented local delegation and review-gate runtime behavior.

The problem is a cross-repo fixture acceptance gap, not a new plugin-owned runtime design. The plugin must not infer daemon command, response, idempotency, permission, retry, or malformed-response shapes from Go internals, local E2E tests, chat notes, or plugin assumptions. The durable contract is this control repo's protocol/docs plus `testdata/conformance/manifest.json` and the fixture files it references.

## Roadmap status

`docs/roadmap.md` marks DELEG-002 as `completed` once this fixture/test/docs slice lands with local control verification, downstream plugin compatibility evidence, and the implementation run's final KAH gate. Roadmap completion is not claimed by docs wording alone; it is backed by the manifest entries, fixture files, conformance tests, contract checker, downstream compatibility check, and KAH evidence for the run.

## Scope

DELEG-002 updates these control-owned surfaces:

- `testdata/conformance/manifest.json`
- `testdata/conformance/fixtures/command/`
- `testdata/conformance/fixtures/error/`
- `testdata/conformance/fixtures/event/`
- `internal/protocol/conformance_test.go`
- `scripts/check_plugin_contract.py`
- `testdata/conformance/README.md`
- roadmap/testing/cross-repo docs that describe plugin consumption boundaries

## Non-scope

- No edits to `kkachi-agent-network-plugin` in this task.
- No live Discord, Hermes gateway, KAB, external daemon, auth, token, localhost, socket fallback, or network behavior.
- No weakening of existing conformance guardrails.
- No plugin-owned UX/runtime behavior inside control docs; control owns the daemon/protocol/fixture contract and plugin consumes it.
- No claim of plugin release readiness, live-client readiness, or production delegation support from fixture publication alone.
- No malformed/schema-invalid payloads in ordinary valid `manifest.json` entries.

## Published fixture matrix

All paths below are listed in `testdata/conformance/manifest.json` and must remain valid parseable fixtures.

| Behavior | Fixture paths | Contract |
| --- | --- | --- |
| Delegation creation | `fixtures/command/delegate-new-request.json`, `fixtures/command/delegate-new-response.json`, `fixtures/event/task-assigned-delegation.json` | `delegate.new` returns `ok: true`, `result.session_id`, `result.results[]`, and `result.deduplicated`. |
| Work submission | `fixtures/command/delegate-submit-request.json`, `fixtures/command/delegate-submit-response.json`, `fixtures/event/work-submitted.json` | `delegate.submit` appends `work_submitted` and returns the standard append response shape. |
| Duplicate/idempotency | `fixtures/command/delegate-submit-duplicate-request.json`, `fixtures/command/delegate-submit-duplicate-response.json` | The duplicate submit pair is representative of the general `command_id` idempotency rule for append-style commands. It is not a submit-only special case. Same logical command plus same `command_id` returns `ok: true`, prior append result fields, and `result.deduplicated: true`. |
| Review request | `fixtures/command/delegate-review-request.json`, `fixtures/command/delegate-review-response.json`, `fixtures/event/review-requested.json` | `delegate.review` appends `review_requested` and moves submitted work to `under_review`. |
| Review submission | `fixtures/command/delegate-review-submit-request.json`, `fixtures/command/delegate-review-submit-response.json`, `fixtures/event/review-submitted.json` | `delegate.review_submit` appends `review_submitted` with a stable verdict/finding payload. |
| Canonical non-review delegation action | `fixtures/command/delegate-accept-request.json`, `fixtures/command/delegate-accept-response.json`, `fixtures/event/work-accepted.json` | `delegate.accept` is the canonical non-review fixture because it is an existing runtime command path and terminally appends `work_accepted`. |
| Permission and validation errors | `fixtures/error/delegate-unauthorized-actor.json`, `fixtures/error/delegate-review-wrong-phase.json`, `fixtures/error/delegate-review-submit-invalid-verdict.json` | Delegation/review failures use normal structured command errors with `ok: false`, `error.code: validation_error`, safe `message`, and safe `details`. |

## Retryable failure policy

DELEG-002 does not publish a public retryable delegation/review command-response shape. Plugin DELRV-2 may test fail-closed preservation of fake-daemon retryability fields, but those fields remain plugin-local test inputs unless a future control task adds a valid structured-error fixture and checker coverage for a stable retryability contract.

## Malformed daemon response policy

Malformed JSON, schema-invalid daemon payloads, truncated responses, and unexpected envelopes are negative-test inputs, not ordinary conformance fixtures.

- Valid `manifest.json` entries remain parseable and schema-intended examples.
- Invalid examples must not be listed as ordinary valid fixtures unless a future manifest category explicitly supports invalid fixtures.
- Plugin DELRV-2 should derive fail-closed behavior from the command-envelope and structured-error schemas here, reject malformed fake-daemon payloads locally, and not reinterpret malformed data as success.

## Plugin consumption boundary

The plugin consumes these fixtures; it does not define daemon/runtime authority. If plugin tests need a new command, error, idempotency, retry, or malformed-response shape, that shape must first be published here or explicitly documented as plugin-local fake-daemon negative-test input.

## Verification expectations

Control-side verification for this matrix includes:

```bash
GOCACHE=/tmp/kkachi-go-build-cache go test ./...
make test-prepare
make check-plugin-contract
make test
```

Full closeout additionally needs downstream plugin `make check-core-contract` from `/Users/draccoon/Workspace/SeventeenthEarth/kkachi/kkachi-agent-network-plugin` and `kkachi-agent-helper gate final run-20260606T081553Z-f394a2b5b90a --json`.
