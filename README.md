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

The Go control runtime implements the local daemon/CLI spine, deterministic `channel.jsonl` storage, stream replay/ack/status, delegation/review commands, council lifecycle commands, local runner event handling, and deterministic transcript/export rendering. All implemented paths are local-first; no live Hermes, Discord, KAB, gateway, credential, token, or production install readiness is claimed.

## Build and local operation

```bash
go build -o ./kkachi-agent-network ./cmd/kkachi-agent-network
go build -o ./kkachi-agent-networkd ./cmd/kkachi-agent-networkd
export KKACHI_AGENT_NETWORK_HOME=/path/to/local/data-home
./kkachi-agent-network init
./kkachi-agent-network daemon start
./kkachi-agent-network daemon status
./kkachi-agent-network doctor
```

The CLI creates a sample disabled registry only when missing. Edit `registry.yaml` with local member wrappers before creating real sessions.

## Session examples

```bash
kkachi-agent-network delegate new sess_example_delegation --moderator agent-mod --assignee agent-1 --title "Implement task A" --task "Implement task A"
kkachi-agent-network delegate submit sess_example_delegation --actor agent-1 --summary "Done" --command-id cmd_submit_example
kkachi-agent-network delegate review sess_example_delegation --actor agent-mod --command-id cmd_review_example
kkachi-agent-network council new "Decide release gate" --members agent-mod,agent-1,agent-2 --moderator agent-mod --session-id sess_example_council
```

Transcript and export commands are deterministic and do not append session events:

```bash
kkachi-agent-network transcript sess_example_delegation --format md --output transcript.md
kkachi-agent-network transcript sess_example_delegation --format jsonl
kkachi-agent-network export sess_example_delegation --bundle
kkachi-agent-network tail sess_example_delegation --limit 20 --format ndjson
```

Export bundles are local directories containing `transcript.md`, `transcript.jsonl`, `brief.md`, `session.json`, `channel.jsonl`, `registry_snapshot.yaml`, and `bundle_manifest.json`.

## Plugin handoff

The companion plugin is contract-checked/tested locally from `../kkachi-agent-network-plugin`. It consumes this repo's protocol schemas, version/features response, and `testdata/conformance/manifest.json`; it must continue to fail closed on unsupported protocol versions, missing feature groups, malformed fake-daemon responses, or any live-service configuration that has not been separately proven.

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

## Recovery and fail-close posture

`status`, `transcript`, `export`, and `tail` are read-only with respect to session events and never invoke runners or write Discord/Kanban/Vault/Hermes/KAB state. Malformed logs, unsafe paths, unsupported formats, and unknown protocol features fail closed with structured errors.
