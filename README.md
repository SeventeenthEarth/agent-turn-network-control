# atn-control

`atn-control` is the Go control/runtime repository for Agent Turn Network (ATN). It owns the daemon, canonical CLI, protocol contracts, event log, state machine, storage projection, recovery paths, and control test gates. The public product is Agent Turn Network; the current control binaries are `atn-control` and `atn-controld`.

The companion Python Hermes plugin adapter is public-facing as `atn-plugin`; the current local sibling workspace remains `../agent-turn-network-plugin` as a local compatibility path while public repo labels use `atn-plugin`.

## Repository boundary

- This repo: `atn-control`, plus the `atn-controld` daemon, `atn-control` CLI, `channel.jsonl` SOT, SQLite projection, protocol/conformance fixtures, security and recovery docs.
- Plugin repo: Hermes plugin manifest/tools/slash commands/skill, Python daemon client, Discord visible-surface helper.
- The plugin is not the source of truth. Discord is not the source of truth. `atn-controld` owns state mutation.

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
go build -o ./atn-control ./cmd/atn-control
go build -o ./atn-controld ./cmd/atn-controld
export ATN_HOME=/path/to/local/data-home
./atn-control init
./atn-control daemon start
./atn-control daemon status
./atn-control doctor
```

The CLI creates a sample disabled registry only when missing. Edit `registry.yaml` with local member wrappers before creating real sessions.

## Session examples

```bash
atn-control delegate new sess_example_delegation --moderator agent-mod --assignee agent-1 --title "Implement task A" --task "Implement task A"
atn-control delegate submit sess_example_delegation --actor agent-1 --summary "Done" --command-id cmd_submit_example
atn-control delegate review sess_example_delegation --actor agent-mod --command-id cmd_review_example
atn-control council new "Decide release gate" --members agent-mod,agent-1,agent-2 --moderator agent-mod --session-id sess_example_council
```

Transcript and export commands are deterministic and do not append session events:

```bash
atn-control transcript sess_example_delegation --format md --output transcript.md
atn-control transcript sess_example_delegation --format jsonl
atn-control export sess_example_delegation --bundle
atn-control tail sess_example_delegation --limit 20 --format ndjson
```

Export bundles are local directories containing `transcript.md`, `transcript.jsonl`, `brief.md`, `session.json`, `channel.jsonl`, `registry_snapshot.yaml`, and `bundle_manifest.json`.

## Plugin handoff

The companion plugin is contract-checked/tested locally from the current workspace path `../agent-turn-network-plugin` while public docs and status labels refer to the repo as `atn-plugin`. It consumes this repo's protocol schemas, version/features response, and `testdata/conformance/manifest.json`; it must continue to fail closed on unsupported protocol versions, missing feature groups, malformed fake-daemon responses, or any live-service configuration that has not been separately proven. ATN-001 locked the rename policy, and ATN-004 remains the pending control code/runtime rename for checked-in module, binary, socket, env, and protocol markers.

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
