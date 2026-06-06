# Testing Strategy

## Scope

This document defines the test layers for KAN control/runtime and the Makefile target contract shared with the plugin repository.

## Makefile target contract

Both repositories must expose these targets:

| Target | Purpose | External resources |
| --- | --- | --- |
| `test-prepare` | formatting, lint, vet/typecheck, docs guardrails, static safety checks | forbidden |
| `test-unit` | isolated unit tests for functions/types/domain logic | forbidden |
| `test-int` | integration between internal components using mock/fake/stub dependencies | forbidden |
| `test-e2e` | real external integration tests against isolated test resources | allowed only in test environment |
| `test` | sequentially runs all four targets above | follows each target |

`make test` must run in this order: `test-prepare`, `test-unit`, `test-int`, `test-e2e`.

## Control test layers

| Layer | Target | Examples |
| --- | --- | --- |
| Unit | protocol, engine, registry, security helpers | phase transitions, strict schema, safe path validation |
| Unit | storage primitives | event envelope validation, cursor math, redaction helper |
| Integration | daemon + storage + CLI using temp data home | append/replay/projection, storage verify/rebuild exit codes, idempotency, JSON errors |
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
- local/fake RUNRT runner event envelopes (`runner_invocation_started`, `runner_invocation_failed`, terminal semantic runner events, and `runner_result_discarded`).

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

## CI guidance

- `test-prepare`, `test-unit`, and `test-int` run on every commit/PR.
- `test-e2e` runs only when isolated external resources are configured.
- E2E absence is a skipped environment, not silent success, once tests exist.
- A failed test is fixed at the owning boundary; tests are not weakened to pass broken behavior.
