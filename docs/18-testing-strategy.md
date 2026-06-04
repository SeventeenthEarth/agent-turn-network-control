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
| Integration | daemon + storage + CLI using temp data home | append/replay/projection, idempotency, JSON errors |
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
- fake Hermes wrapper that returns deterministic semantic output and optional cost JSON.
- fake runner timeout/nonzero/malformed-output variants.
- fake stream client with durable cursor file.
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
- delivery evidence commands for Discord/helper surfaces.

The plugin repository must run its Python client against either copied fixtures or a temporary daemon built from this repo.

## CI guidance

- `test-prepare`, `test-unit`, and `test-int` run on every commit/PR.
- `test-e2e` runs only when isolated external resources are configured.
- E2E absence is a skipped environment, not silent success, once tests exist.
- A failed test is fixed at the owning boundary; tests are not weakened to pass broken behavior.
