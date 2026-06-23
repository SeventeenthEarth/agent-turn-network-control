# Architecture

## Architectural principle

HUN control/runtime follows a daemon-authority architecture. Domain policy is separated from adapters. The daemon owns state transitions, event append, locks, replay, and projections. The CLI and plugin are clients of the daemon contract, not alternate authorities.

## Repository layout target

```text
hun-control/
  cmd/
    hun/       # Go CLI main
    hund/      # Go daemon main
  internal/
    daemon/                     # process, local transport, stream hub
    cli/                        # canonical command handlers and stream client
    protocol/                   # command/event/stream/error models
    registry/                   # registry loader, validation, snapshots
    storage/                    # channel.jsonl, SQLite projection, replay
    engine/                     # state machines and policy decisions
    runtime/                    # member runtime coordination helpers
    runner/                     # bounded runner adapters
    transcript/                 # markdown/jsonl/brief renderers
    observability/              # metrics, health, structured diagnostics
    recovery/                   # verify/rebuild/repair helpers
  testdata/
    conformance/                # protocol fixtures consumed by plugin repo
  docs/
  Makefile
```

The companion plugin repository targets:

```text
hun-plugin/
  src/hun_plugin/
    client/                     # Python daemon client for protocol contract
    tools/                      # Hermes tool handlers
    slash_commands/             # Hermes command bindings when supported
    discord_surface/            # gateway/send_message helpers
    skills/                     # bundled Hermes skill material
  tests/
  docs/
  Makefile
```

## Runtime flow

```text
Moderator/member runtime or operator
  -> CLI or Python plugin client
    -> HUN command envelope
      -> hund local transport
        -> validate registry identity and state transition
        -> append channel.jsonl
        -> update SQLite projection
        -> publish stream frame
          -> CLI stream, plugin stream, member runtime observers
```

The Go CLI and Python plugin do not share source code. They share the protocol contract and conformance fixtures. Any behavior implemented in both clients must be verified through cross-language conformance tests.

Post-Release live-local work is governed by `24-live-transport-control-sot.md`. The control `LTRAN`, `MEMBR`, and `SURFD` epics own daemon/CLI compatibility, real participant runtime invocation, and delivery-evidence projection respectively; companion plugin epics start only after the corresponding control epic boundary is complete.

## Authority boundaries

- `hund` is the only component that mutates `channel.jsonl`, SQLite projections, session locks, cursor state, and replay state.
- `hun` CLI is canonical for diagnostics, recovery, manual operation, and plugin-failure fallback.
- `hun-plugin` is the preferred Hermes UX surface, but the plugin is not the source of truth.
- Discord is a visible room/evidence pointer, never a state authority.
- Hermes core is not patched.

## Plugin boundary

The plugin may:

- call daemon status/session/status/stream/command endpoints;
- expose Hermes tools and slash commands for implemented daemon commands;
- use Hermes gateway/send_message for visible Discord helper posts;
- record delivery evidence by sending typed commands to the daemon;
- tell the operator to use CLI fallback when the daemon/plugin compatibility check fails.

The plugin must not:

- append or rewrite `channel.jsonl`;
- mutate SQLite projections or lock files directly;
- reinterpret daemon errors as success;
- require raw Discord tokens in the daemon;
- use live Hermes/Discord resources in normal unit or integration tests.

## Protocol compatibility

The daemon exposes a version/health contract including daemon version, protocol version, supported feature flags, and minimum compatible plugin protocol. The plugin fails closed when required features are absent.

Conformance fixtures under `testdata/conformance/` are normative for cross-repo behavior. The plugin repo may copy fixtures for stability but must track the control protocol version.

## Discord-thread council surface

Discord-thread council support is a surface binding over the council state machine:

```text
Discord thread            # human-visible room
Hermes plugin/gateway     # posts visible messages
HUN daemon                # records typed events and delivery evidence
channel.jsonl             # canonical SOT
Kanban/Vault              # optional linked authority return path
```

The daemon stores external message IDs and delivery status as evidence fields only. It never derives lifecycle state from free-form Discord text.
