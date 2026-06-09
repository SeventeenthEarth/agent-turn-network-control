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
| LTRAN | Live transport control compatibility | daemon/CLI compatibility reads and live-local support |
| MEMBR | Member runtime profile invocation | real participant profile/wrapper invocation proof |
| SURFD | Surface delivery evidence contract | event-to-visible rendering and evidence projection |

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
| LTRAN | RELIA | live-local control compatibility starts after Release v1 local acceptance |
| MEMBR | LTRAN | real participant invocation needs live-local stream/control compatibility |
| SURFD | MEMBR, TRANS | visible rendering needs participant response evidence plus transcript/export surfaces |

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
| Phase 9 | LTRAN | live-local daemon/CLI compatibility for plugin transport | disposable control live-local evidence and compatibility checks pass |
| Phase 10 | MEMBR | real participant profile/wrapper invocation path | selected participant invocation evidence passes without role substitution |
| Phase 11 | SURFD | surface delivery evidence contract | projection/transcript/export or fixture evidence supports visible rendering |

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

Remaining summarized Release v1 epics preserve the previous product behavior: member runtime and runner adapter, council discussion, consensus, transcript/export, distribution, observability, disaster recovery, and full testing. Each epic must include unit, integration, and, when appropriate, isolated E2E tests mapped to the Makefile target contract in `18-testing-strategy.md`.

## Post-Release live-local epics

These epics are planned after Release v1 local acceptance. They are repo-owned control epics that gate companion plugin epics in `../../kkachi-agent-network-plugin/docs/06-implementation-epics-tasks.md`.

Active task transfer between control and plugin happens only at an epic boundary. Do not interrupt a control epic with plugin tasks, and do not interrupt a plugin epic with control tasks. If a missing sibling capability is found, block the active epic with evidence and complete the sibling epic that owns the missing capability before resuming.

| Task ID | Scope | Suggested paths | Verification |
| --- | --- | --- | --- |
| LTRAN-001 | Add the control companion live transport SOT, roadmap entries, docs-map/index updates, and cross-repo handoff rule. | `docs/24-live-transport-control-sot.md`, `docs/roadmap.md`, `docs/README.md`, `docs/kkachi-docs-map.yaml`, `docs/21-cross-repo-development.md` | docs guardrails, `make check-plugin-contract`, plugin `make check-core-contract` when practical |
| LTRAN-002 | Confirm or add daemon compatibility read shapes needed by plugin live transport. | `internal/daemon/`, `internal/command/`, `internal/protocol/`, `testdata/conformance/`, `docs/03-protocol-spec.md`, `docs/04-cli-spec.md` | status/version/health/stream/council command tests, conformance checks, `make test` |
| LTRAN-003 | Prove disposable CLI/daemon live-local support for plugin equivalence. | CLI integration tests, conformance fixtures, release/local scripts, docs evidence | disposable data-home smoke, command-id/idempotency checks, stream replay/follow/ack checks |
| MEMBR-001 | Select and document first participant invocation pilot mode and evidence rules. | `docs/14-streaming-member-runtime.md`, `docs/24-live-transport-control-sot.md`, runtime docs/evidence | docs guardrails and review acceptance |
| MEMBR-002 | Prove selected real participant profile/wrapper invocation and durable success/failure event recording. | `internal/memberruntime/`, `internal/runner/`, CLI/runtime tests, docs | fake/isolated wrapper tests plus real-profile evidence when authorized; no role substitution |
| SURFD-001 | Define event-to-visible-surface rendering/evidence contract. | `docs/03-protocol-spec.md`, `docs/07-moderator-policy.md`, `docs/13-operational-contracts.md`, `docs/24-live-transport-control-sot.md` | docs guardrails, protocol consistency checks |
| SURFD-002 | Prove projection/transcript/export or fixture evidence for visible rendering tests. | `internal/transcript/`, `internal/storage/`, `testdata/conformance/`, docs | transcript/export/projection tests, delivery evidence fixture checks, `make test` |
