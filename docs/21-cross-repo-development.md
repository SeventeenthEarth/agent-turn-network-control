# Cross-Repo Parallel Development

## Goal

Enable `kkachi-agent-network` core and `kkachi-agent-network-plugin` to develop independently while checking each other's milestones through an explicit protocol/conformance contract.

This document is the core-side SOT for cross-repo development. The plugin-side companion is `../../kkachi-agent-network-plugin/docs/07-core-compatibility.md`.

## Development principle

The repositories do not share source code. They share:

- a protocol version;
- command envelope schemas;
- stream frame schemas;
- structured error schemas;
- version/feature compatibility semantics;
- conformance fixtures under `testdata/conformance/`;
- milestone dependency rules.

The core can move without waiting for plugin UX work. The plugin can move without waiting for full core implementation by using fake daemon behavior and conformance fixtures. A plugin feature becomes release-ready only when the matching core milestone is implemented and the cross-repo checks pass.

## Current contract version

| Field | Value |
| --- | --- |
| Protocol version | `kan-protocol-v1alpha0` |
| Fixture manifest | `testdata/conformance/manifest.json` |
| Stability | draft, docs/scaffold only |
| Breaking-change rule | allowed before first implementation, but must update manifest and plugin compatibility docs together |

## Milestone unlock matrix

| Core milestone | Core output | Plugin work unlocked | Plugin limit before core exists |
| --- | --- | --- | --- |
| Core Bootstrap | Go module, CLI/daemon help, Makefile | Plugin P0 scaffold can proceed independently | plugin uses fake daemon only |
| Core 3A.1 Version/feature contract | daemon status shape, protocol version, feature list | Plugin P1 compatibility check | fake status fixture until daemon exists |
| Core 3A.2 Command envelope fixture | request/response/idempotency/error fixture | Python daemon client request builder | no write tools marked release-ready |
| Core 3A.3 Stream frame fixture | replay/follow/cursor frame fixture | stream parser and tail/diagnostic tools | fake stream only |
| Core 3A.4 Structured error fixture | error categories and JSON shape | plugin error rendering and fail-closed UX | no success reinterpretation allowed |
| Core Epic 5 Delegation commands | implemented daemon/CLI delegation commands | Plugin P3 delegation tools | skeleton/fake-daemon tests only |
| Core Epic 6 Review commands | review/revision/accept commands | Plugin review tools | skeleton/fake-daemon tests only |
| Core Epic 7-8 Council commands | council prepare/speak/vote/finalize commands | Plugin P4 council tools | skeleton/fake-daemon tests only |
| Core delivery evidence commands | delivery success/failure typed commands | Discord surface helper audit | fake gateway only until isolated e2e target exists |
| Core transcript/export | transcript and export commands | Plugin transcript/export tools | fixture rendering only |

## Plugin milestone expectations

| Plugin milestone | Must check from core | Must not claim before |
| --- | --- | --- |
| P0 Scaffold | core docs/21 exists, core Makefile exposes `check-plugin-contract` | installed/working Hermes integration; P0 may claim scaffold readiness only |
| P1 Python daemon client | fixture manifest and version/feature contract | release-ready write behavior |
| P2 Hermes status/diagnostic tools | daemon status/session/stream fixtures | domain command coverage |
| P3 Delegation/review tools | core delegation/review command fixtures or implemented CLI | production delegation support |
| P4 Council/Discord surface | council command fixtures plus delivery evidence contract | live Discord support without isolated E2E |
| P5 Skill/distribution | implemented command matrix and compatibility version | general install readiness |

## Cross-repo check commands

From core:

```bash
make check-plugin-contract
```

Checks that the sibling plugin repo exists, has required compatibility docs, exposes `check-core-contract`, declares the same protocol version, and names the current core fixture manifest.

From plugin:

```bash
make check-core-contract
```

Checks that the sibling core repo exists, has required cross-repo docs, exposes `check-plugin-contract`, provides the fixture manifest, and declares the same protocol version.

These checks are not substitutes for unit/integration/e2e tests. They are early milestone guardrails for parallel development.

## Fixture publication rule

Core owns `testdata/conformance/manifest.json`. Every contract-affecting change must update:

1. `testdata/conformance/manifest.json`;
2. this document;
3. plugin `docs/07-core-compatibility.md` or its supported-version matrix;
4. cross-repo check scripts if the expected shape changes.

## Parallel development modes

### Stub mode

Plugin work uses fake daemon responses and core fixture files. Good for P0-P2 before a real daemon exists.

### Fixture mode

Core publishes conformance fixtures and plugin runs parser/client tests against those fixtures. Good for P1-P4 before full daemon command implementation.

### Live local mode

Plugin runs against a locally built `kkachi-agent-networkd` with a disposable data home. Good for integration and isolated E2E once core commands exist.

### External E2E mode

Plugin may touch Hermes/Discord only with explicit isolated test environment variables. It must never default to the currently running Hermes profile or active Discord thread.

## Release gate

A cross-repo feature is release-ready only when:

- core implemented the matching daemon/CLI command or stream behavior;
- plugin mapped the feature to that daemon contract;
- both `make test` commands pass;
- both cross-repo check commands pass;
- plugin fake-daemon integration tests pass;
- isolated E2E is either configured and passing or explicitly documented as not part of that milestone.
