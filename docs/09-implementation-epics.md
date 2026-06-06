# Implementation Epics

## Release v1 target scope

Release v1 covers the Go control daemon/CLI plus the compatible Python Hermes plugin adapter. This repository owns control epics; plugin-specific implementation epics live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics-tasks.md`.

Control scope: registry, storage, daemon, CLI, protocol/conformance, stream, member runtime contract, runner adapter, delegation, review gate, council, transcript/export, distribution, reliability.

Plugin scope: Hermes plugin manifest/entrypoint, Python daemon client, tools/slash commands, bundled skill, Discord surface helper, plugin UX diagnostics, and plugin conformance tests.

## Epic and task ID convention

Epic IDs are five-letter uppercase English slugs. Task IDs use `{EPIC}-001`, `{EPIC}-002`, and so on. The current Release v1 epic IDs are:

| Epic ID | Epic Title | Scope |
| --- | --- | --- |
| BOOTS | Bootstrap | Go scaffold and local gates |
| REGST | Registry/security | identity and file-safety boundary |
| STORE | Storage/event SOT | session directory, event log, projection, replay |
| DAEMN | Daemon/CLI/protocol | daemon, command transport, stream, conformance |
| RUNRT | Runtime/runner | member runtime loop and bounded runner adapter |
| DELEG | Delegation/review | delegation lifecycle and review gates |
| COUNC | Council/consensus | council discussion, voting, and consensus state |
| TRANS | Transcript/distribution | transcript/export, distribution docs, plugin handoff |
| RELIA | Reliability/release | observability, disaster recovery, release acceptance |

## Epic dependency graph

| Epic ID | Depends on | Reason |
| --- | --- | --- |
| BOOTS | none | Go scaffold and test gates |
| REGST | BOOTS | identity and file-safety boundary |
| STORE | REGST | session snapshot and append require registry authority |
| DAEMN | REGST, STORE | command path, stream, active-session lock; protocol/conformance can start from BOOTS and is finalized here |
| RUNRT | REGST, STORE, DAEMN | stream and wrapper accounting prerequisites |
| DELEG | RUNRT | delegation lifecycle and review gates depend on runtime sessions |
| COUNC | DAEMN, RUNRT | council depends on stream-driven runtimes |
| TRANS | STORE, DELEG, COUNC | rendering depends on event log and collaboration events |
| RELIA | REGST, STORE, DAEMN, RUNRT, DELEG, COUNC, TRANS | full acceptance, recovery, observability |

## Implementation phase grouping

| Phase | Epic IDs | Primary output | Exit gate |
| --- | --- | --- | --- |
| Bootstrap | BOOTS | `go.mod`, `cmd/`, `internal/`, `Makefile`, docs guardrails, help smoke tests | `make test` passes without external resources |
| Phase 1 | REGST | data-home and registry validation | registry/security tests pass |
| Phase 2 | STORE | `channel.jsonl`, SQLite projection, replay | append/replay/projection tests pass |
| Phase 3 | DAEMN | daemon, CLI, stream, structured errors, conformance fixtures | CLI integration + conformance tests pass |
| Phase 4 | RUNRT | member runtime loop contract and bounded `hermes-agent` runner | fake runner/runtime tests pass |
| Phase 5 | DELEG | delegation and review gate plus plugin-consumable fixture handoff | delegation/review E2E via CLI/fakes and conformance fixture checks pass |
| Phase 6 | COUNC | council and consensus | council/consensus tests pass |
| Phase 7 | TRANS | transcript/export/distribution docs | golden transcript + install docs pass |
| Phase 8 | RELIA | reliability, observability, disaster recovery | full Release v1 acceptance suite pass |

## BOOTS — Bootstrap

| Task ID | Scope | Suggested paths | Verification |
| --- | --- | --- | --- |
| BOOTS-001 | Create `go.mod`, `cmd/kkachi-agent-network`, `cmd/kkachi-agent-networkd`, initial `internal/protocol`, test layout, and Makefile. | `cmd/`, `internal/`, `tests/`, `Makefile` | `make test`, binary `--help` exits 0, no external resources used. |

## REGST — Registry/security

- Define registry schema for `<data_home>/registry.yaml`.
- Implement data-home resolution: `$KKACHI_AGENT_NETWORK_HOME` > `$XDG_DATA_HOME/kkachi-agent-network` > `~/.kkachi-agent-network/`.
- Validate safe data-home and registry permissions.
- Load registry through TOCTOU-reduced file handling.
- Reject reserved principals: `user`, `kkachi-agent-networkd`.
- Validate wrapper paths and env allowlist.
- Write per-session `registry_snapshot.yaml` before `session_created`.
- Add `registry validate` and `registry show` CLI commands.

## STORE — Storage/event SOT

- Create session directories.
- Append canonical event envelopes to `channel.jsonl`.
- Maintain SQLite projection as rebuildable cache.
- Store registry snapshot metadata.
- Project recipients, runner invocations, escalation batches, Discord surface metadata, linked authority metadata.
- Rebuild projection from event log deterministically.

## DAEMN — Daemon/CLI/protocol

- Implement `kkachi-agent-networkd` process and local command transport.
- Implement `kkachi-agent-network` CLI commands that talk to the daemon.
- Implement stream replay/follow/cursor acknowledgement.
- Implement single active-session lock.
- Implement structured JSON errors and stable exit categories.
- Add status, doctor, storage verify/rebuild, block/resume, limits extension.
- Define command envelope, stream frame, structured error, and version/feature schemas.
- Add conformance fixtures under `testdata/conformance/`.
- Add tests proving CLI and daemon obey the same contract.
- Publish compatibility guidance for the plugin repo.
- Forbid shared-source assumptions; compatibility is through protocol and tests.

## DELEG — Delegation/review

| Task ID | Scope | Suggested paths | Verification |
| --- | --- | --- | --- |
| DELEG-001 | Implement delegation lifecycle, review request/response gates, blocked/resume handling, CLI/E2E fake coverage, and audit evidence. | `internal/`, `cmd/`, `tests/`, `docs/03-protocol-spec.md`, `docs/04-cli-spec.md`, `docs/06-state-machine.md`, `docs/13-operational-contracts.md` | delegation/review local CLI and fake E2E checks, `make check-plugin-contract`, `make test` |
| DELEG-002 | Publish plugin-consumable delegation/review command and structured-error fixtures for success, duplicate/idempotency, permission/error, retryable failure policy, and malformed-response handling. | `testdata/conformance/`, conformance validation tests, `docs/22-deleg-002-conformance-fixture-matrix.md`, `docs/21-cross-repo-development.md`, `docs/18-testing-strategy.md` | manifest/fixture validation, `make check-plugin-contract`, `make test`, downstream `make check-core-contract` from the plugin repo |

## RUNRT / COUNC / TRANS / RELIA summary

Remaining summarized epics preserve the previous product behavior: member runtime and runner adapter, council discussion, consensus, transcript/export, distribution, observability, disaster recovery, and full testing. Each epic must include unit, integration, and, when appropriate, isolated E2E tests mapped to the Makefile target contract in `18-testing-strategy.md`.
