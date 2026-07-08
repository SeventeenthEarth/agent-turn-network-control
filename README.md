# atn-control

`atn-control` is the Go control-plane repository for Agent Turn Network (ATN).
It provides the local daemon and CLI that own ATN lifecycle, protocol, event log,
state transitions, replay/projection, transcripts, exports, recovery checks, and
conformance fixtures.

Current public names:

- Product/network: **Agent Turn Network (ATN)**
- Control repo: `agent-turn-network-control`
- Control CLI: `atn-control`
- Control daemon: `atn-controld`
- Current local control version: `v0.1.0`
- Companion Hermes plugin repo: `../agent-turn-network-plugin`, public name `atn-plugin`

## What this repository does

`atn-control` is the state authority. It owns durable command handling and stores
canonical session events under the configured `ATN_HOME` data home. The daemon and
CLI cover:

- daemon lifecycle: `daemon start`, `daemon stop`, `daemon status`, `doctor`;
- registry and data-home validation;
- protocol/version/feature compatibility responses;
- delegation and review session commands;
- council lifecycle commands and selected-runner event/state contracts;
- stream replay/follow/ack/status;
- deterministic transcript/export/tail rendering;
- storage verification and projection rebuild paths;
- conformance fixtures consumed by the companion plugin repo.

The control daemon is the source of truth. The plugin is not the source of truth.
Discord/Hermes visible messages are evidence pointers, not lifecycle state.

## Repository boundary

- This repo owns: Go daemon/CLI, protocol contracts, `channel.jsonl` event SOT,
  SQLite projection, structured errors, replay/recovery, transcript/export, and
  control-side conformance fixtures.
- The companion `atn-plugin` repo owns: Hermes-facing Python plugin tools,
  injected/live daemon client adapters, bundled `atn-plugin` / `atn-moderator` /
  `atn-participant` skills, and operator-side visible-surface helpers.
- For real ATN operator/participant workflows, install and verify **both** repos:
  the control daemon/CLI here and the Hermes plugin in `../agent-turn-network-plugin`.
  Control can be developed and tested standalone, but participant Hermes tools need
  the plugin and an explicit connection to this daemon.

## Install and local operation

```bash
go install github.com/SeventeenthEarth/agent-turn-network-control/cmd/atn-control@v0.1.0
go install github.com/SeventeenthEarth/agent-turn-network-control/cmd/atn-controld@v0.1.0
hash -r
command -v atn-control
command -v atn-controld

export ATN_HOME=/path/to/local/data-home
atn-control init
atn-control daemon start
atn-control daemon status
atn-control doctor
atn-control version
atn-controld version
atn-control version --features --format json
```

`go install github.com/SeventeenthEarth/agent-turn-network-control/...@v0.1.0`
downloads the released control module and installs the operator binaries into the
active Go binary directory such as `$(go env GOPATH)/bin`. Make sure that
directory is on `PATH`; otherwise the shell may keep running an older
`atn-control` / `atn-controld` from another location. If version output looks
stale, inspect the selected binary:

```bash
go version -m "$(command -v atn-control)"
go version -m "$(command -v atn-controld)"
```

Expected output for this release line:

```text
atn-control version=v0.1.0 protocol_version=atn-protocol-v1alpha0 schema_version=1
atn-controld version=v0.1.0 protocol_version=atn-protocol-v1alpha0 schema_version=1
```

`init` creates a local data home and a disabled sample `registry.yaml` only when
missing. Edit `registry.yaml` with explicit local member wrappers before creating
real sessions. Do not point local tests at production profile/gateway state unless
that exact live scope has been explicitly approved.

## CLI examples

Delegation session:

```bash
atn-control delegate new sess_example_delegation \
  --moderator agent-mod \
  --assignee agent-1 \
  --title "Implement task A" \
  --task "Implement task A"

atn-control delegate submit sess_example_delegation \
  --actor agent-1 \
  --summary "Done" \
  --command-id cmd_submit_example

atn-control delegate review sess_example_delegation \
  --actor agent-mod \
  --command-id cmd_review_example
```

Local-only council session:

```bash
atn-control council new "Decide release gate" \
  --members agent-mod,agent-1,agent-2 \
  --moderator agent-mod \
  --session-id sess_example_council \
  --requested-output-mode local-daemon-only \
  --explicit-non-visible-override true \
  --override-reason "operator requested local daemon diagnostics"
```

Read-only transcript/export/tail commands:

```bash
atn-control transcript sess_example_delegation --format md --output transcript.md
atn-control transcript sess_example_delegation --format jsonl
atn-control export sess_example_delegation --bundle
atn-control tail sess_example_delegation --limit 20 --format ndjson
```

Export bundles are local directories containing `transcript.md`,
`transcript.jsonl`, `brief.md`, `session.json`, `channel.jsonl`,
`registry_snapshot.yaml`, and `bundle_manifest.json`.

## Companion plugin reminder

Install `atn-plugin` when Hermes profiles or participant agents need ATN tools.
The plugin should be configured with an explicit daemon socket/config reference to
the control daemon; it must not auto-discover daemons or hide CLI subprocess
fallbacks. See the sibling README:

```bash
cd ../agent-turn-network-plugin
less README.md
```

Keep the compatibility contract green after control protocol changes:

```bash
make check-plugin-contract
cd ../agent-turn-network-plugin && make check-core-contract
```

## Documentation

Start at [`docs/README.md`](docs/README.md).

Key docs:

- [`docs/spec/overview.md`](docs/spec/overview.md) — purpose and repo boundary
- [`docs/spec/architecture.md`](docs/spec/architecture.md) — Go daemon/CLI architecture
- [`docs/spec/protocol-and-cli.md`](docs/spec/protocol-and-cli.md) — protocol, CLI, and conformance fixtures
- [`docs/spec/operations.md`](docs/spec/operations.md) — operational contract and daemon usage
- [`docs/spec/testing-and-tooling.md`](docs/spec/testing-and-tooling.md) — test layers, release gates, and Makefile contract
- [`docs/spec/cross-repo-contract.md`](docs/spec/cross-repo-contract.md) — control/plugin compatibility handoff

## Test targets

```bash
make test-prepare          # gofmt/lint/vet/docs guardrails/help smoke; no external resources
make test-unit             # Go unit tests
make test-int              # fake/mock/stub integration; no external resources
make test-e2e              # isolated external test environment only
make test                  # sequential all targets
make check-plugin-contract # verify companion plugin milestone/contract readiness
```

`make test-e2e` must never use the currently running Hermes profile/gateway or
production Discord rooms by default.

## Recovery and fail-close posture

`status`, `transcript`, `export`, and `tail` are read-only with respect to session
events and never invoke runners or write Discord/Kanban/Vault/Hermes/KAB state.
Malformed logs, unsafe paths, unsupported formats, unknown protocol features, and
unsupported live/plugin assumptions fail closed with structured errors.

No live Hermes, Discord, gateway, credential, token, provider, KAB, production
install, commit, push, or broad rollout readiness is implied by this README.
