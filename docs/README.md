# hun-control Documentation

This directory is the source of truth for the **HUN control/runtime repo**: the Go daemon, Go CLI, protocol, event log, state machine, storage, security, operations, and release plan.

The Python Hermes plugin has its own public-facing repository label `hun-plugin`; local cross-repo checks currently resolve its workspace documentation at `../../kkachi-agent-network-plugin/docs/` as a local compatibility path. This repo may repeat plugin-facing compatibility rules, but it must not own plugin implementation details beyond the daemon contract the plugin must obey.

## Terminology

- **Release v1** — the first product release target for HUN control/runtime plus the matching plugin adapter.
- **Implementation Phase N** — build sequencing bucket, not a product version.
- **Control repo** — this repository, `hun-control`, containing daemon/CLI authority.
- **Plugin repo** — `hun-plugin`, containing the Python Hermes plugin adapter.
- **Protocol contract** — command envelopes, stream frames, structured errors, version compatibility, and schema fixtures used by both repos.

## Repository split contract

| Concern | Owning repo | Notes |
| --- | --- | --- |
| Daemon state, locks, event append, replay | `hun-control` | `channel.jsonl` is SOT. |
| Go CLI diagnostics/recovery/manual operation | `hun-control` | Must work without Hermes plugin; CLI binary remains `hun`. |
| Protocol schemas and conformance fixtures | `hun-control` | Plugin consumes and tests against them. |
| Hermes plugin tools/slash commands/skill | `hun-plugin` | Adapter only; no direct SOT mutation. |
| Discord visible surface helpers | `hun-plugin` | Uses Hermes gateway/send_message and records delivery evidence through daemon commands. |
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
16. `15-hermes-agent-runtime-context.md` — Hermes Agent context for HUN implementers
17. `16-observability.md` — health, metrics, SLO/SLI, structured diagnostics
18. `17-disaster-recovery.md` — backup, restore, corruption handling, replay rebuild
19. `18-testing-strategy.md` — test layers and Makefile target contract
20. `19-tooling.md` — Go control/runtime scaffold, Makefile, local/CI commands
21. `20-discord-thread-council-tobe.md` — Discord thread council surface design
22. `21-cross-repo-development.md` — parallel control/plugin milestone, conformance, and cross-repo check contract
23. `22-deleg-002-conformance-fixture-matrix.md` — DELEG-002 fixture publication task brief for delegation/review plugin handoff
24. `23-release-v1-acceptance.md` — local Release v1 acceptance scope, gates, evidence, and non-live-readiness boundaries
25. `24-live-transport-control-sot.md` — control-side live transport SOT for `LTRAN` / `MEMBR` / `SURFD`, companion to the plugin live transport SOT
26. `25-council-argument-graph-sot.md` — control-side council argument graph SOT for `ARGUE`, discussion-quality protocol/fixture planning, and plugin handoff boundaries
27. `26-hermes-unified-network-control-naming-sot.md` — control-side Hermes Unified Network naming SOT for `HUN`, public rename boundaries, clean no-alias policy, and downstream control task sequencing
28. `27-agent-turn-network-control-naming-sot.md` — control-side Agent Turn Network naming SOT for `ATN`, public rename boundaries, clean no-alias policy, and ATN-001..ATN-005 task sequencing

`11-distribution-and-skill.md` is deprecated by the repo split and replaced by `11-distribution-and-plugin.md`.

## Required Makefile targets

Both the control and plugin repositories must expose these baseline operator targets:

```bash
make test-prepare  # lint/vet/formatting/guardrails; no external resources
make test-unit     # unit tests
make test-int      # integration tests with mock/fake/stub only; no external resources
make test-e2e      # real external integrations only in isolated test environment
make test          # sequential baseline targets; control also runs local release acceptance
make check-plugin-contract  # verify companion plugin milestone/contract readiness
```

The control repo Makefile owns Go checks, control docs guardrails, and RELIA-001 `test-release-acceptance` local storage/replay/recovery evidence. The plugin repo Makefile owns Python/Hermes plugin checks and plugin docs guardrails; `test-release-acceptance` is not plugin-owned unless a later plugin task adds compatible local evidence.

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
13. `23-release-v1-acceptance.md` when validating Release v1 local readiness
14. `24-live-transport-control-sot.md` when planning post-Release `LTRAN`, `MEMBR`, `SURFD`, or `ENSOT` live-local / visible-closeout work
15. `25-council-argument-graph-sot.md` when planning `ARGUE` discussion-quality argument graph work; `control/ARGUE-001` is accepted docs-only, and `control/ARGUE-002` is accepted for bounded local static protocol/fixture scope under KAS/KAH run `run-20260615T145822Z-caab064cf550`
16. `26-hermes-unified-network-control-naming-sot.md` when reading the completed prior public rename proof
17. `27-agent-turn-network-control-naming-sot.md` when planning `ATN` public rename work
18. plugin docs in the local workspace path `../../kkachi-agent-network-plugin/docs/` while the public repo label remains `hun-plugin`; HUN-014 is the active compatibility proof for this split until ATN tasks replace the public labels

## Current implementation state

This repository has the BOOTS-001 scaffold plus DAEMN-002 local control surfaces and DELEG-001 local delegation/review gates implemented. The local daemon/CLI now expose protocol/version features, structured command envelopes, stream replay with bounded local follow over durable `channel.jsonl`, stream ack/status, structured errors, active-session lock evidence, delivery-evidence fixtures/checks, and static conformance fixtures under `testdata/conformance/`.

DELEG-002 publishes the control-owned plugin-consumable delegation/review fixture matrix for success, duplicate/idempotency, permission/error, retryable failure policy, and malformed-response handling. Plugin delegation/review failure coverage must consume these control fixtures and must not invent control-owned command or error shapes.

RELIA-001 release acceptance is complete for local Release v1 readiness. `LTRAN-001` records the control-side live transport SOT/mapping only. `LTRAN-002` is complete for daemon compatibility reads and conformance-backed capability evidence. `LTRAN-003` is complete for disposable data-home CLI/daemon live-local proof: daemon-backed `compat` reads, stream replay/follow/ack/status, `delegate.submit` idempotency, structured command-id conflicts, color review, GLM Octo, and cross-repo checks passed without plugin mutation or production activation. `MEMBR-001` is complete, `MEMBR-002` is candidate/isolated proof, `SURFD` has docs/local proof acceptance, and `ENSOT-001` is accepted as a docs-only SOT gate for plugin `VISUX` visible closeout semantics after KAN Red/Orange/Gray review and Blue synthesis. `ARGUE-001` is accepted/completed docs-only for council discussion-quality SOT closeout after Red `t_4a2e735f`, Orange `t_9f4b2b9c`, Gray `t_b196d630`, and Blue synthesis; that ARGUE-001 acceptance did not authorize ARGUE-002. `ARGUE-002` protocol/fixture work is now separately accepted for bounded local scope under KAS/KAH run `run-20260615T145822Z-caab064cf550` after Red `t_e2ced3fc`, Orange `t_fd35e83a`, Gray `t_c9e20348`, Blue synthesis `t_ade91c69`, and final gate `evt-001437`. These epics do not claim production activation until their own exits are verified.

`live_readiness` remains `false`: live Hermes/Discord/KAB/gateway/auth/token integrations, production plugin-load evidence, and external E2E are not contacted by default test targets.
