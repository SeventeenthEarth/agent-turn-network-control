# Cross-Repo Parallel Development

## Goal

Enable `kkachi-agent-network-control` and `kkachi-agent-network-plugin` to develop independently while checking each other's milestones through an explicit protocol/conformance contract.

This document is the control-side SOT for cross-repo development. The plugin-side companion is `../../kkachi-agent-network-plugin/docs/07-core-compatibility.md`.

## Development principle

The repositories do not share source code. They share:

- a protocol version;
- command envelope schemas;
- stream frame schemas;
- structured error schemas;
- version/feature compatibility semantics;
- conformance fixtures under `testdata/conformance/`;
- milestone dependency rules.

The control repo can move without waiting for plugin UX work. The plugin can move without waiting for full control implementation by using fake daemon behavior and conformance fixtures. A plugin feature becomes release-ready only when the matching control milestone is implemented and the cross-repo checks pass.

## Current contract version

| Field | Value |
| --- | --- |
| Protocol version | `kan-protocol-v1alpha0` |
| Fixture manifest | `testdata/conformance/manifest.json` |
| Stability | draft local implementation + static DAEMN/DELEG/COUNC conformance fixtures |
| Live readiness | `false`; no live Hermes/Discord/KAB/gateway/auth/token support is claimed |
| Breaking-change rule | allowed before the first stable protocol release, but must update manifest, control docs, plugin compatibility docs, and checks together |

## Milestone unlock matrix

| Control milestone | Control output | Plugin work unlocked | Plugin limit before control exists |
| --- | --- | --- | --- |
| BOOTS-001 | Go module, CLI/daemon help, Makefile | Plugin P0 scaffold can proceed independently | plugin uses fake daemon only |
| DAEMN-002 version/feature contract | implemented local daemon/CLI `version.read`, protocol version, feature list, and static version fixture | Plugin P1 compatibility check | no live gateway/runtime readiness claim |
| DAEMN-002 command envelope fixture | implemented command envelope parsing plus request/response/idempotency/error fixtures | Python daemon client request builder | no live wrappers or external side effects by default |
| DAEMN-002 stream frame fixture | implemented local daemon/CLI stream replay, bounded follow over durable `channel.jsonl`, ack, status, cursor validation, and stream fixtures | stream parser and diagnostic tools | bounded local follow only; no long-lived production streaming over Hermes/Discord/KAB |
| DAEMN-002 structured error fixture | implemented structured error categories and JSON shape for local daemon/CLI failures | plugin error rendering and fail-closed UX | no success reinterpretation allowed |
| DELEG-001 delegation/review commands | implemented daemon/CLI delegation lifecycle, review gates, blocked/resume handling, canonical `cancel` / `session_cancelled`, and local/fake coverage | Plugin P3 delegation/review tool scaffolding | skeleton/fake-daemon tests only; plugin must not invent missing fixture shapes |
| DELEG-002 delegation/review fixture matrix | plugin-consumable command and structured-error fixtures for delegation/review success, canonical non-review `delegate.accept`, duplicate/idempotency, permission/error, retryable failure policy, and malformed-response fail-closed policy | Plugin DELRV-2 delegation/review failure coverage | no live gateway/runtime readiness claim; plugin consumes control fixtures and remains fail-closed on malformed daemon responses |
| COUNC-001 council commands | implemented local council lifecycle commands plus static command/event/error fixtures and `council.lifecycle` feature group | Plugin P4 council tools and static fake-daemon/parser handoff | `live_readiness=false`; no live Discord support without isolated E2E |
| DAEMN-002 delivery evidence commands | implemented local delivery success/failure evidence fixtures and daemon/CLI checks | Discord surface helper audit | fake gateway only until isolated e2e target exists |
| TRANS-001 transcript/export | implemented control-owned `transcript.render` and `export.bundle` command fixtures, deterministic local transcript/export rendering, and bundle output fixtures under `testdata/conformance/` | Plugin transcript/export tools can consume the control fixture manifest and render/parser contract | fixture/local rendering only; no live Discord/Hermes/KAB/gateway readiness; no plugin source mutation |
| LTRAN control epic | control companion SOT, daemon/CLI compatibility read confirmation, disposable live-local control evidence | Plugin `LTRAN` explicit live daemon transport and plugin/CLI/daemon equivalence | plugin `LTRAN` stays blocked until control `LTRAN` is complete; `control/LTRAN-001` records docs-only SOT/mapping and does not unblock plugin live transport by itself; no production activation claim |
| MEMBR control epic | real participant profile/wrapper invocation evidence for `speaker_selected` success/failure | Plugin `PARTC` participant stream/write and selected response proof | no simulated role prompt substitution; no always-on production runtime claim |
| SURFD control epic | event-to-visible rendering contract and delivery evidence projection/fixture support | Plugin `SURFD` visible helper/rendering boundary | visible messages remain presentation/evidence, not lifecycle authority |

## Plugin milestone expectations

| Plugin milestone | Must check from control | Must not claim before |
| --- | --- | --- |
| P0 Scaffold | control docs/21 exists, control Makefile exposes `check-plugin-contract` | installed/working Hermes integration; P0 may claim scaffold readiness only |
| P1 Python daemon client | fixture manifest and version/feature contract | release-ready write behavior |
| P2 Hermes status/diagnostic tools | daemon status/session/stream fixtures | domain command coverage |
| P3 Delegation/review tools | control delegation/review CLI behavior plus DELEG-002 fixture matrix for plugin-consumable command/error shapes | production delegation support |
| P4 Council/Discord surface | `council.lifecycle` feature group, COUNC-001 command/event/error fixtures, and delivery evidence contract | live Discord support without isolated E2E |
| P5 Skill/distribution | implemented command matrix and compatibility version | general install readiness |

## Cross-repo check commands

From control:

```bash
make check-plugin-contract
```

Checks that the sibling plugin repo exists, has required compatibility docs, exposes `check-core-contract`, declares the same protocol version, and names the current control fixture manifest.

From plugin:

```bash
make check-core-contract
```

Checks that the sibling control repo exists, has required cross-repo docs, exposes `check-plugin-contract`, provides the fixture manifest, and declares the same protocol version.

These checks are not substitutes for unit/integration/e2e tests. They are early milestone guardrails for parallel development.

## Fixture publication rule

Control owns `testdata/conformance/manifest.json`. Every contract-affecting change must update:

1. `testdata/conformance/manifest.json`;
2. this document;
3. plugin `docs/07-core-compatibility.md` or its supported-version matrix;
4. cross-repo check scripts if the expected shape changes.

Valid fixture manifest entries should remain schema-valid examples. Malformed JSON or intentionally schema-invalid daemon payloads are negative-test inputs: either place them in a clearly marked invalid-fixture policy surface or document them as plugin-local fail-closed tests derived from the command/structured-error schemas. Do not list invalid payloads as ordinary valid conformance fixtures unless the manifest and checker explicitly support that category.

## Active epic handoff rule

Cross-repo active work transfers only at repo-owned epic boundaries. Control `LTRAN` gates plugin `LTRAN`; control `MEMBR` gates plugin `PARTC`; control `SURFD` gates plugin `SURFD`. `control/LTRAN-001` is only the control-side SOT/mapping task, and `control/LTRAN-002` covers daemon compatibility reads/conformance evidence only; plugin `LTRAN` remains blocked until `control/LTRAN-003` proves disposable live-local support. A missing sibling capability found mid-epic blocks the active epic with evidence; it does not authorize switching into the sibling repo for an individual task while the current epic remains active.

## Parallel development modes

### Stub mode

Plugin work uses fake daemon responses and control fixture files. Good for P0-P2 before a real daemon exists.

### Fixture mode

Control publishes conformance fixtures and plugin runs parser/client tests against those fixtures. Good for P1-P4 before full daemon command implementation.

### Live local mode

Plugin runs against a locally built `kkachi-agent-networkd` with a disposable data home. Good for integration and isolated E2E once control commands exist.

### External E2E mode

Plugin may touch Hermes/Discord only with explicit isolated test environment variables. It must never default to the currently running Hermes profile or active Discord thread.

## Release gate

A cross-repo feature is release-ready only when:

- control implemented the matching daemon/CLI command or stream behavior;
- plugin mapped the feature to that daemon contract;
- both `make test` commands pass;
- both cross-repo check commands pass;
- plugin fake-daemon integration tests pass;
- isolated E2E is either configured and passing or explicitly documented as not part of that milestone.
