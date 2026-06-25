# hun-control

`hun-control` is the Go control/runtime repository for Hermes Unified Network (HUN). It owns the daemon, canonical CLI, protocol contracts, event log, state machine, storage projection, recovery paths, and control test gates. The public product is Hermes Unified Network; the current control binaries are `hun` and `hund`.

The companion Python Hermes plugin adapter is public-facing as `hun-plugin`; the current local sibling workspace remains `../kkachi-agent-network-plugin` as a local compatibility path while public repo labels use `hun-plugin`.

## Repository boundary

- This repo: `hun-control`, plus the `hund` daemon, `hun` CLI, `channel.jsonl` SOT, SQLite projection, protocol/conformance fixtures, security and recovery docs.
- Plugin repo: Hermes plugin manifest/tools/slash commands/skill, Python daemon client, Discord visible-surface helper.
- The plugin is not the source of truth. Discord is not the source of truth. `hund` owns state mutation.

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
go build -o ./hun ./cmd/hun
go build -o ./hund ./cmd/hund
export HUN_HOME=/path/to/local/data-home
./hun init
./hun daemon start
./hun daemon status
./hun doctor
```

The CLI creates a sample disabled registry only when missing. Edit `registry.yaml` with local member wrappers before creating real sessions.

## Session examples

```bash
hun delegate new sess_example_delegation --moderator agent-mod --assignee agent-1 --title "Implement task A" --task "Implement task A"
hun delegate submit sess_example_delegation --actor agent-1 --summary "Done" --command-id cmd_submit_example
hun delegate review sess_example_delegation --actor agent-mod --command-id cmd_review_example
hun council new "Decide release gate" --members agent-mod,agent-1,agent-2 --moderator agent-mod --session-id sess_example_council
```

Transcript and export commands are deterministic and do not append session events:

```bash
hun transcript sess_example_delegation --format md --output transcript.md
hun transcript sess_example_delegation --format jsonl
hun export sess_example_delegation --bundle
hun tail sess_example_delegation --limit 20 --format ndjson
```

Export bundles are local directories containing `transcript.md`, `transcript.jsonl`, `brief.md`, `session.json`, `channel.jsonl`, `registry_snapshot.yaml`, and `bundle_manifest.json`.

## Plugin handoff

The companion plugin is contract-checked/tested locally from the current workspace path `../kkachi-agent-network-plugin` while public docs and status labels refer to the repo as `hun-plugin`. It consumes this repo's protocol schemas, version/features response, and `testdata/conformance/manifest.json`; it must continue to fail closed on unsupported protocol versions, missing feature groups, malformed fake-daemon responses, or any live-service configuration that has not been separately proven. HUN-014 is the active bounded compatibility proof for this local/public naming split.

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
