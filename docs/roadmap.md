# Release v1 Roadmap

## Roadmap rule

This roadmap is for the Go control repository. Python Hermes plugin roadmap items live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics-tasks.md`.

Roadmap tasks must be **capability-sized**, not file-sized. Each row should normally be large enough for one Kkachi/KAH task contract, one implementation lane, tests, docs/evidence update, role review, and one commit. Split a task only when dependency order, approval gate, failure domain, or reviewer specialty is materially different.

## Bootstrap

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| control-bootstrap-001 | Control scaffold and local gates | `go.mod`, `cmd/kkachi-agent-network`, `cmd/kkachi-agent-networkd`, `internal/`, Makefile target contract, docs guardrails, and binary help smoke tests pass without external resources | Planned |

## Epic 1 Registry/security

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| registry-001 | Registry authority capability | deterministic data-home resolution, strict registry schema, fail-closed permissions/symlink/TOCTOU checks, reserved principal rejection, wrapper/env validation, per-session registry snapshot, and `registry validate/show` CLI covered by tests | Planned |

## Epic 2 Storage/event SOT

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| storage-001 | Event-store append capability | safe session directories, session metadata, registry snapshot metadata, canonical `channel.jsonl` event append, and surface/linked-authority evidence fields covered by unit and integration tests | Planned |
| storage-002 | Projection and replay capability | SQLite projection as a rebuildable cache, deterministic replay/rebuild, storage verify/rebuild commands or equivalent diagnostics, and projection/replay tests | Planned |

## Epic 3 Daemon/CLI/protocol

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| control-api-001 | Daemon and CLI command capability | `kkachi-agent-networkd` lifecycle, local command transport, canonical `kkachi-agent-network` commands, status/doctor/health, structured JSON errors, and stable exit categories verified through CLI integration tests | Planned |
| control-api-002 | Stream and conformance capability | stream replay/follow/cursor acknowledgement, active-session lock, version/feature endpoint, command/event/error/stream fixtures under `testdata/conformance/`, and plugin-compatible protocol checks | Planned |

## Later Release v1 capability slices

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| runtime-001 | Member runtime and runner capability | member runtime loop contract, bounded `hermes-agent` runner adapter, wrapper accounting, fake-runner tests, and operator docs | Planned |
| delegation-review-001 | Delegation and review-gate capability | delegation lifecycle, review request/response gates, blocked/resume handling, CLI/E2E fake coverage, and audit evidence | Planned |
| council-consensus-001 | Council and consensus capability | stream-driven council discussion, speaker/moderator policy, voting/consensus state, and council/consensus tests | Planned |
| transcript-distribution-001 | Transcript/export and distribution capability | golden transcript/export rendering, install/distribution docs, plugin handoff checks, and operator acceptance evidence | Planned |
| reliability-001 | Reliability and release acceptance capability | observability, disaster recovery, corruption handling, replay rebuild, full Release v1 acceptance suite, and release readiness evidence | Planned |

Every roadmap item must map to the Makefile target taxonomy in `18-testing-strategy.md` and to the phase dependencies in `09-implementation-epics.md`.
