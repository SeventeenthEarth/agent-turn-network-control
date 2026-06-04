# kkachi-agent-network-control

`kkachi-agent-network-control` is the Go control/runtime repository for KAN. It owns the daemon, canonical CLI, protocol contracts, event log, state machine, storage projection, recovery paths, and control test gates. The user-facing product and binaries remain `kkachi-agent-network` / `kkachi-agent-networkd` unless a later release decision changes them.

The companion Python Hermes plugin adapter lives in `../kkachi-agent-network-plugin`.

## Repository boundary

- This repo: `kkachi-agent-network-control`, plus the `kkachi-agent-networkd` daemon, `kkachi-agent-network` CLI, `channel.jsonl` SOT, SQLite projection, protocol/conformance fixtures, security and recovery docs.
- Plugin repo: Hermes plugin manifest/tools/slash commands/skill, Python daemon client, Discord visible-surface helper.
- The plugin is not the source of truth. Discord is not the source of truth. `kkachi-agent-networkd` owns state mutation.

## Documentation

Start at [`docs/README.md`](docs/README.md).

Key docs:

- [`docs/00-overview.md`](docs/00-overview.md) — purpose and repo boundary
- [`docs/02-architecture.md`](docs/02-architecture.md) — Go daemon/CLI architecture
- [`docs/18-testing-strategy.md`](docs/18-testing-strategy.md) — test layers and isolated E2E policy
- [`docs/19-tooling.md`](docs/19-tooling.md) — Go tooling and Makefile contract
- [`docs/11-distribution-and-plugin.md`](docs/11-distribution-and-plugin.md) — core distribution and plugin compatibility handoff

## Current state

BOOTS-001 implementation is in progress. The initial Go module, CLI/daemon entrypoints, internal command scaffold, protocol version scaffold, test layout, and local help smoke tests exist. State-mutating daemon/runtime features are intentionally not implemented yet.

## Test targets

```bash
make test-prepare  # gofmt/lint/vet/docs guardrails/help smoke; no external resources
make test-unit     # Go unit tests for current scaffold
make test-int      # fake/mock/stub integration; no external resources
make test-e2e      # isolated external test environment only
make test          # sequential all targets
make check-plugin-contract  # verify companion plugin milestone/contract readiness
```

`make test-e2e` must never use the currently running Hermes profile/gateway or production Discord rooms by default.

## Next scaffold gate

Before BOOTS-001 can be reported complete, finish review/feedback handling, GLM Octo or an explicit waiver, post-fix verification, second color re-review when required, final KAH gate evidence, and commit approval.
