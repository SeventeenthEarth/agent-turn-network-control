# Implementation Epics

## Release v1 target scope

Release v1 covers the Go core daemon/CLI plus the compatible Python Hermes plugin adapter. This repository owns core epics; plugin-specific implementation epics live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics.md`.

Core scope: registry, storage, daemon, CLI, protocol/conformance, stream, member runtime contract, runner adapter, delegation, review gate, council, transcript/export, distribution, reliability.

Plugin scope: Hermes plugin manifest/entrypoint, Python daemon client, tools/slash commands, bundled skill, Discord surface helper, plugin UX diagnostics, and plugin conformance tests.

## Epic dependency graph

| Epic | Depends on | Reason |
| --- | --- | --- |
| Bootstrap | none | Go scaffold and test gates |
| Epic 1 Registry/security | Bootstrap | identity and file-safety boundary |
| Epic 2 Storage/event SOT | Epic 1 | session snapshot and append require registry authority |
| Epic 3 Daemon/CLI/protocol | Epic 1,2 | command path, stream, active-session lock |
| Epic 3A Protocol/conformance contract | Bootstrap | plugin/client compatibility and fake-daemon development before full core implementation; finalized alongside Epic 3 |
| Epic 4 Runtime/runner | Epic 1-3A | stream and wrapper accounting prerequisites |
| Epic 5 Delegation | Epic 1-4 | core delegation lifecycle |
| Epic 6 Review gate | Epic 5 | review is a delegation quality gate |
| Epic 7 Council discussion | Epic 1-4 | council depends on stream-driven runtimes |
| Epic 8 Consensus | Epic 7 | voting depends on council state |
| Epic 9 Transcript/export | Epic 2,5,7,8 | rendering depends on event log |
| Epic 10 Distribution/docs | Epic 3,5-9 | install and operator docs must match commands |
| Epic 11 Reliability/ops | Epic 1-10 | full acceptance, recovery, observability |

## Implementation Phase grouping

| Implementation Phase | Scope | Primary output | Exit gate |
| --- | --- | --- | --- |
| Bootstrap | Go repo scaffold | `go.mod`, `cmd/`, `internal/`, `Makefile`, docs guardrails, help smoke tests | `make test` passes without external resources |
| Implementation Phase 1 | Epic 1 | data-home and registry validation | registry/security tests pass |
| Implementation Phase 2 | Epic 2 | `channel.jsonl`, SQLite projection, replay | append/replay/projection tests pass |
| Implementation Phase 3 | Epic 3 + 3A | daemon, CLI, stream, structured errors, conformance fixtures | CLI integration + conformance tests pass |
| Implementation Phase 4 | Epic 4 | member runtime loop contract and bounded `hermes-agent` runner | fake runner/runtime tests pass |
| Implementation Phase 5 | Epics 5-6 | delegation and review gate | delegation/review E2E via CLI and fakes pass |
| Implementation Phase 6 | Epics 7-8 | council and consensus | council/consensus tests pass |
| Implementation Phase 7 | Epics 9-10 | transcript/export/distribution docs | golden transcript + install docs pass |
| Implementation Phase 8 | Epic 11 | reliability, observability, disaster recovery | full Release v1 acceptance suite pass |

## Bootstrap story

| Story | Scope | Suggested paths | Verification |
| --- | --- | --- | --- |
| B-S1 Go repository scaffold | Create `go.mod`, `cmd/kkachi-agent-network`, `cmd/kkachi-agent-networkd`, initial `internal/protocol`, test layout, and Makefile. | `cmd/`, `internal/`, `tests/`, `Makefile` | `make test`, binary `--help` exits 0, no external resources used. |

## Epic 1: Product skeleton and registry

- Define registry schema for `<data_home>/registry.yaml`.
- Implement data-home resolution: `$KKACHI_AGENT_NETWORK_HOME` > `$XDG_DATA_HOME/kkachi-agent-network` > `~/.kkachi-agent-network/`.
- Validate safe data-home and registry permissions.
- Load registry through TOCTOU-reduced file handling.
- Reject reserved principals: `user`, `kkachi-agent-networkd`.
- Validate wrapper paths and env allowlist.
- Write per-session `registry_snapshot.yaml` before `session_created`.
- Add `registry validate` and `registry show` CLI commands.

## Epic 2: Storage and event log

- Create session directories.
- Append canonical event envelopes to `channel.jsonl`.
- Maintain SQLite projection as rebuildable cache.
- Store registry snapshot metadata.
- Project recipients, runner invocations, escalation batches, Discord surface metadata, linked authority metadata.
- Rebuild projection from event log deterministically.

## Epic 3: Daemon, CLI, and stream

- Implement `kkachi-agent-networkd` process and local command transport.
- Implement `kkachi-agent-network` CLI commands that talk to the daemon.
- Implement stream replay/follow/cursor acknowledgement.
- Implement single active-session lock.
- Implement structured JSON errors and stable exit categories.
- Add status, doctor, storage verify/rebuild, block/resume, limits extension.

## Epic 3A: Protocol and conformance contract

- Define command envelope, stream frame, structured error, and version/feature schemas.
- Add conformance fixtures under `testdata/conformance/`.
- Add tests proving CLI and daemon obey the same contract.
- Publish compatibility guidance for the plugin repo.
- Forbid shared-source assumptions; compatibility is through protocol and tests.

## Epic 4-11 summary

Later epics preserve the previous product behavior: member runtime and runner adapter, delegation, review gate, council discussion, consensus, transcript/export, distribution, observability, disaster recovery, and full testing. Each epic must include unit, integration, and, when appropriate, isolated E2E tests mapped to the Makefile target contract in `18-testing-strategy.md`.
