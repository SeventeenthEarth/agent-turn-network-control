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

Documentation/scaffold stage. Go source scaffolding is not created yet, so Makefile code checks skip with explicit messages while docs guardrails still run.

## Test targets

```bash
make test-prepare  # gofmt/lint/vet/docs guardrails; no external resources
make test-unit     # unit tests; currently docs-only scaffold pass
make test-int      # fake/mock/stub integration; no external resources
make test-e2e      # isolated external test environment only
make test          # sequential all targets
make check-plugin-contract  # verify companion plugin milestone/contract readiness
```

`make test-e2e` must never use the currently running Hermes profile/gateway or production Discord rooms by default.

## Next scaffold step

Create `go.mod`, `cmd/kkachi-agent-network`, `cmd/kkachi-agent-networkd`, `internal/protocol`, and first help smoke tests as described in `docs/09-implementation-epics.md`.
