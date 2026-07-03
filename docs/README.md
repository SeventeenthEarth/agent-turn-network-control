# atn-control Documentation

This directory is the source of truth for the ATN control/runtime repo: Go daemon, CLI, protocol, event log, state machine, storage, security, operations, and release plan.

`roadmap.md` is the SSOT for control epic/task status, sequencing, and evidence pointers. Other docs describe stable requirements, architecture, specs, or implementation decomposition and should reference the roadmap instead of duplicating current task state.

## Structure

1. `roadmap.md` - control epic/task status, sequencing, and evidence-pointer SSOT
2. `spec/overview.md` - product purpose, repo boundary, requirements, and engineering principles
3. `spec/architecture.md` - daemon/CLI architecture, state machine, storage schema, and moderator policy
4. `spec/protocol-and-cli.md` - protocol, CLI surface, command fixtures, and conformance handoff
5. `spec/operations.md` - operational contracts, security, observability, and disaster recovery
6. `spec/testing-and-tooling.md` - acceptance scope, release gates, test layers, and Makefile/tooling contract
7. `spec/cross-repo-contract.md` - distribution, plugin compatibility, and cross-repo development gates
8. `spec/live-transport-control-sot.md` - control-side live transport SOT
9. `spec/council-argument-graph-sot.md` - control-side council argument graph SOT
10. `spec/agent-turn-network-control-naming-sot.md` - control-side ATN naming SOT
11. `todo/implementation-decomposition.md` - implementation decomposition; status still lives in `roadmap.md`
12. `adr/runtime-decisions.md` - runtime rationale and background decisions
13. `adr/deprecated.md` - deprecated historical notes

## Repository Split Contract

| Concern | Owning repo | Notes |
| --- | --- | --- |
| Daemon state, locks, event append, replay | `atn-control` | `channel.jsonl` is SOT. |
| Go CLI diagnostics/recovery/manual operation | `atn-control` | Must work without Hermes plugin; public CLI binary is `atn-control`. |
| Protocol schemas and conformance fixtures | `atn-control` | Plugin consumes and tests against them. |
| Hermes plugin tools/slash commands/skill | `atn-plugin` | Adapter only; no direct SOT mutation. |
| Discord visible surface helpers | `atn-plugin` | Uses Hermes gateway/send_message and records delivery evidence through daemon commands. |

## Required Makefile Targets

```bash
make test-prepare
make test-unit
make test-int
make test-e2e
make test
make check-plugin-contract
```

`live_readiness` remains `false`: live Hermes/Discord/KAB/gateway/auth/token integrations, production plugin-load evidence, and external E2E are not contacted by default test targets.
