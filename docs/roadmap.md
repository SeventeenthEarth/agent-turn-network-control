# Release v1 Roadmap

## Roadmap rule

This roadmap is for the Go core repository. Python Hermes plugin roadmap items live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics.md`.

## Bootstrap

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| core-bootstrap-001 | Go scaffold | `go.mod`, `cmd/`, `internal/`, `Makefile`, docs guardrails | Planned |
| core-bootstrap-002 | Help smoke tests | `kkachi-agent-network --help`, `kkachi-agent-networkd --help` | Planned |
| core-bootstrap-003 | Test target contract | `make test-prepare`, `test-unit`, `test-int`, `test-e2e`, `test` | Planned |

## Epic 1 Registry/security

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| registry-001 | Data-home resolution | deterministic path resolution | Planned |
| registry-002 | Registry file safety | fail-closed permissions/symlink checks | Planned |
| registry-003 | Strict schema | valid/invalid registry tests | Planned |
| registry-004 | Snapshot writer | per-session registry snapshot | Planned |
| registry-005 | Registry CLI | validate/show commands | Planned |

## Epic 2 Storage/event SOT

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| storage-001 | Session directories | safe directories and metadata | Planned |
| storage-002 | Event append | canonical `channel.jsonl` append | Planned |
| storage-003 | SQLite projection | rebuildable projection tables | Planned |
| storage-004 | Replay | deterministic rebuild from event log | Planned |
| storage-005 | Surface projections | Discord/linked authority evidence fields | Planned |

## Epic 3 Daemon/CLI/protocol

| ID | Task | Outcome | Status |
| --- | --- | --- | --- |
| daemon-cli-001 | Daemon lifecycle | start/status/stop/health | Planned |
| daemon-cli-002 | Command transport | local socket or HTTP transport | Planned |
| daemon-cli-003 | CLI framework | canonical commands and structured errors | Planned |
| daemon-cli-004 | Stream | replay/follow/cursor ack | Planned |
| conformance-001 | Protocol fixtures | command/event/error/stream fixtures | Planned |
| conformance-002 | Version endpoint | compatibility and feature flags | Planned |

## Later epics

Delegation, review, council, consensus, transcript/export, distribution, reliability, observability, disaster recovery, and full acceptance testing follow `09-implementation-epics.md`. Every roadmap item must map to the Makefile target taxonomy in `18-testing-strategy.md`.
