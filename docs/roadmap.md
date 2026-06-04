# Release v1 Roadmap

## Roadmap rule

This roadmap is for the Go control repository. Python Hermes plugin roadmap items live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics-tasks.md`.

Roadmap tasks must be **capability-sized**, not file-sized. Each row should normally be large enough for one Kkachi/KAH task contract, one implementation lane, tests, docs/evidence update, role review, and one commit. Split a task only when dependency order, approval gate, failure domain, or reviewer specialty is materially different.

Epic IDs are five-letter uppercase English slugs. Task IDs are derived from the epic ID as `{EPIC}-001`, `{EPIC}-002`, and so on. Status values are compact operator-facing values: `planned`, `in_progress`, `completed`, or `blocked`.

## BOOTS — Bootstrap

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| BOOTS-001 | Control scaffold and local gates | completed | Create `go.mod`, `cmd/kkachi-agent-network`, `cmd/kkachi-agent-networkd`, `internal/`, Makefile target contract, docs guardrails, and binary help smoke tests that pass without external resources. |

## REGST — Registry/security

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| REGST-001 | Registry authority | completed | Implement deterministic data-home resolution, strict registry schema, fail-closed permissions/symlink/TOCTOU checks, reserved principal rejection, wrapper/env validation, per-session registry snapshot, and `registry validate/show` CLI tests. |

## STORE — Storage/event SOT

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| STORE-001 | Event-store append | planned | Implement safe session directories, session metadata, registry snapshot metadata, canonical `channel.jsonl` event append, and surface/linked-authority evidence fields with unit and integration coverage. |
| STORE-002 | Projection and replay | planned | Implement SQLite projection as a rebuildable cache, deterministic replay/rebuild, storage verify/rebuild commands or equivalent diagnostics, and projection/replay tests. |

## DAEMN — Daemon/CLI/protocol

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| DAEMN-001 | Daemon and CLI commands | planned | Implement `kkachi-agent-networkd` lifecycle, local command transport, canonical `kkachi-agent-network` commands, status/doctor/health, structured JSON errors, and stable exit categories verified through CLI integration tests. |
| DAEMN-002 | Stream and conformance | planned | Implement stream replay/follow/cursor acknowledgement, active-session lock, version/feature endpoint, command/event/error/stream fixtures under `testdata/conformance/`, and plugin-compatible protocol checks. |

## RUNRT — Runtime/runner

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| RUNRT-001 | Member runtime and runner | planned | Implement member runtime loop contract, bounded `hermes-agent` runner adapter, wrapper accounting, fake-runner tests, and operator docs. |

## DELEG — Delegation/review

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| DELEG-001 | Delegation and review gates | planned | Implement delegation lifecycle, review request/response gates, blocked/resume handling, CLI/E2E fake coverage, and audit evidence. |

## COUNC — Council/consensus

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| COUNC-001 | Council and consensus | planned | Implement stream-driven council discussion, speaker/moderator policy, voting/consensus state, and council/consensus tests. |

## TRANS — Transcript/distribution

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| TRANS-001 | Transcript and distribution | planned | Implement golden transcript/export rendering, install/distribution docs, plugin handoff checks, and operator acceptance evidence. |

## RELIA — Reliability/release

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| RELIA-001 | Reliability and release acceptance | planned | Implement observability, disaster recovery, corruption handling, replay rebuild, full Release v1 acceptance suite, and release readiness evidence. |

Every roadmap item must map to the Makefile target taxonomy in `18-testing-strategy.md` and to the phase dependencies in `09-implementation-epics.md`.
