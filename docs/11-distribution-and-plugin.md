# Distribution and Plugin Compatibility

## Goal

A user should be able to install the Go control runtime, start the daemon, verify the CLI, then optionally install the Python Hermes plugin from `kkachi-agent-network-plugin`.

## Control distribution

The control repository ships two binaries:

- `kkachi-agent-networkd` — daemon, state authority, stream hub, storage owner
- `kkachi-agent-network` — canonical CLI for diagnostics, recovery, manual operation, and tests

Supported install shapes may include source build, release archives, Homebrew/tap, or `go install` once module paths are fixed. The exact distribution mechanism must not change the authority boundary: CLI and plugin remain clients of the daemon.

## Companion plugin distribution

The Hermes plugin is distributed separately from the control runtime, in the companion `kkachi-agent-network-plugin` repository. It contains Python plugin code, a Python daemon client, tool/slash-command bindings, and a bundled skill. The plugin repo owns its Python packaging details.

Control docs must specify the daemon contract the plugin consumes:

- command envelope schema;
- stream frame schema;
- structured error schema;
- version/feature compatibility endpoint;
- delivery evidence command path;
- transcript/export local rendering commands and control-owned conformance fixtures;
- conformance fixture version.

For TRANS-001, plugin distribution handoff is limited to consuming the control-owned `transcript.render` and `export.bundle` command fixtures plus the local bundle shape documented by the manifest. This is not a live Discord, Hermes, KAB, gateway, or production install readiness claim, and the control repo must not mutate plugin source while publishing the handoff.

## Compatibility rule

The plugin may support multiple control protocol versions only when it can prove behavior through conformance tests. If the daemon reports an unsupported protocol or missing required feature, the plugin fails closed and points to the CLI fallback.

## Root README requirements for this repo

The control root README should include:

1. What KAN control/runtime is and is not.
2. How to build/install `kkachi-agent-networkd` and `kkachi-agent-network`.
3. Data home resolution and registry setup.
4. Daemon start/status/stop.
5. First delegation example through CLI.
6. Council example through CLI.
7. Transcript/export example.
8. How to run `make test`, `make test-prepare`, `make test-unit`, `make test-int`, `make test-e2e`.
9. How to install the companion plugin and where its docs live.
10. Fail-close and recovery guidance.

## Deprecated distribution assumptions

Pre-split Python package entry points for the control repo are no longer valid. The control repo must not describe itself as a Python package or as the owner of Hermes plugin implementation files.
