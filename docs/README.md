# kkachi-agent-network-control Documentation

This directory is the source of truth for the **KAN control/runtime repo**: the Go daemon, Go CLI, protocol, event log, state machine, storage, security, operations, and release plan.

The Python Hermes plugin has its own repository and documentation at `../../kkachi-agent-network-plugin/docs/`. This repo may repeat plugin-facing compatibility rules, but it must not own plugin implementation details beyond the daemon contract the plugin must obey.

## Terminology

- **Release v1** — the first product release target for KAN control/runtime plus the matching plugin adapter.
- **Implementation Phase N** — build sequencing bucket, not a product version.
- **Control repo** — this repository, `kkachi-agent-network-control`, containing daemon/CLI authority.
- **Plugin repo** — `kkachi-agent-network-plugin`, containing the Python Hermes plugin adapter.
- **Protocol contract** — command envelopes, stream frames, structured errors, version compatibility, and schema fixtures used by both repos.

## Repository split contract

| Concern | Owning repo | Notes |
| --- | --- | --- |
| Daemon state, locks, event append, replay | `kkachi-agent-network-control` | `channel.jsonl` is SOT. |
| Go CLI diagnostics/recovery/manual operation | `kkachi-agent-network-control` | Must work without Hermes plugin; CLI binary remains `kkachi-agent-network`. |
| Protocol schemas and conformance fixtures | `kkachi-agent-network-control` | Plugin consumes and tests against them. |
| Hermes plugin tools/slash commands/skill | `kkachi-agent-network-plugin` | Adapter only; no direct SOT mutation. |
| Discord visible surface helpers | `kkachi-agent-network-plugin` | Uses Hermes gateway/send_message and records delivery evidence through daemon commands. |
| End-user UX summaries | both | May duplicate, but authority labels must be explicit. |

## Documents

1. `00-overview.md` — project purpose, repo boundary, non-goals, Release v1 scope
2. `01-product-requirements.md` — functional and operational requirements
3. `02-architecture.md` — Go control/runtime architecture and plugin boundary
4. `03-protocol-spec.md` — canonical event protocol and schemas
5. `04-cli-spec.md` — canonical CLI surface and plugin equivalence rules
6. `05-storage-schema.md` — filesystem and SQLite projection schema
7. `06-state-machine.md` — delegation/council lifecycle transitions
8. `07-moderator-policy.md` — orchestration, review, speaker, and consensus policy
9. `08-acceptance-tests.md` — end-to-end product scenarios
10. `09-implementation-epics.md` — phased implementation plan
11. `10-engineering-principles.md` — implementation and review invariants
12. `11-distribution-and-plugin.md` — Go control/runtime distribution and plugin compatibility handoff
13. `12-security.md` — registry, subprocess, workspace, and secret safety
14. `13-operational-contracts.md` — stream, idempotency, cost, timeouts, schema migration
15. `14-streaming-member-runtime.md` — member runtime rationale
16. `15-hermes-agent-runtime-context.md` — Hermes Agent context for KAN implementers
17. `16-observability.md` — health, metrics, SLO/SLI, structured diagnostics
18. `17-disaster-recovery.md` — backup, restore, corruption handling, replay rebuild
19. `18-testing-strategy.md` — test layers and Makefile target contract
20. `19-tooling.md` — Go control/runtime scaffold, Makefile, local/CI commands
21. `20-discord-thread-council-tobe.md` — Discord thread council surface design
22. `21-cross-repo-development.md` — parallel control/plugin milestone, conformance, and cross-repo check contract
23. `22-deleg-002-conformance-fixture-matrix.md` — DELEG-002 fixture publication task brief for delegation/review plugin handoff

`11-distribution-and-skill.md` is deprecated by the repo split and replaced by `11-distribution-and-plugin.md`.

## Required Makefile targets

Both the control and plugin repositories must expose the same operator targets:

```bash
make test-prepare  # lint/vet/formatting/guardrails; no external resources
make test-unit     # unit tests
make test-int      # integration tests with mock/fake/stub only; no external resources
make test-e2e      # real external integrations only in isolated test environment
make test          # sequential: test-prepare -> test-unit -> test-int -> test-e2e
make check-plugin-contract  # verify companion plugin milestone/contract readiness
```

The control repo Makefile owns Go checks and control docs guardrails. The plugin repo Makefile owns Python/Hermes plugin checks and plugin docs guardrails.

## Reading order

1. `00-overview.md`
2. `02-architecture.md`
3. `13-operational-contracts.md`
4. `12-security.md`
5. `06-state-machine.md`
6. `03-protocol-spec.md`
7. `04-cli-spec.md`
8. `18-testing-strategy.md`
9. `19-tooling.md`
10. `21-cross-repo-development.md`
11. `09-implementation-epics.md`
12. `22-deleg-002-conformance-fixture-matrix.md` when planning or implementing DELEG-002 / plugin DELRV-2 unblock work
13. plugin docs in `../../kkachi-agent-network-plugin/docs/`

## Current implementation state

This repository has the BOOTS-001 scaffold plus DAEMN-002 local control surfaces and DELEG-001 local delegation/review gates implemented. The local daemon/CLI now expose protocol/version features, structured command envelopes, stream replay with bounded local follow over durable `channel.jsonl`, stream ack/status, structured errors, active-session lock evidence, delivery-evidence fixtures/checks, and static conformance fixtures under `testdata/conformance/`.

DELEG-002 publishes the control-owned plugin-consumable delegation/review fixture matrix for success, duplicate/idempotency, permission/error, retryable failure policy, and malformed-response handling. Plugin delegation/review failure coverage must consume these control fixtures and must not invent control-owned command or error shapes.

`live_readiness` remains `false`: real wrappers/RUNRT, live Hermes/Discord/KAB/gateway/auth/token integrations, and external E2E are still future scope and are not contacted by default test targets.
