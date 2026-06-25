# Agent Turn Network Overview

## Purpose

`atn-control` is the control/runtime authority repository for Agent Turn Network (ATN) real Hermes team member coordination. The product/runtime surface is `atn-control`. It owns durable sessions for delegation, review, and council discussion through a Go daemon, a minimal Go CLI, typed protocol contracts, an append-only `channel.jsonl` event log, and SQLite projections.

ATN is not a Discord bot and not a Hermes plugin. Discord and Hermes are important surfaces, but the canonical state remains daemon-owned typed events.

## Repository boundary

This repository is the **control authority repository**:

- repo: `atn-control`
- implementation language for control runtime: Go
- binaries: `atn-controld` and `atn-control`
- SOT documents: protocol, state machine, storage, security, operations, testing, and release roadmap
- companion plugin repository: `../../agent-turn-network-plugin`

The companion repository contains the Python Hermes plugin adapter and its own docs. Duplication is allowed only for operator-facing summaries and compatibility contracts. If the same rule appears in both repositories, the daemon/state/protocol authority lives here unless explicitly marked as plugin UX guidance.

## Primary customer

The first-class runtime user is Hermes Agent: long-lived moderator and member profile processes that can observe a stream, persist cursors, and write typed ATN commands. Reactive terminal tools may be invoked by adapters, but they are not the primary coordination runtime.

## Control model

```text
User / external authority
  -> Moderator Hermes runtime or operator
    -> ATN command contract
      -> atn-controld
        -> validate identity, command, and state transition
        -> append channel.jsonl
        -> update SQLite projection
        -> publish stream frames
          -> member Hermes runtimes / plugin / CLI stream observers
```

The Go CLI uses the same protocol contract as other clients. The Python plugin implements a separate client in the plugin repo; it does not share source code with the Go CLI. Cross-language compatibility is enforced by protocol schemas and conformance tests, not by shared implementation files.

## Session types

- `delegation` — moderator assigns work to one or more real members, receives progress/questions/submissions, and finalizes through acceptance or cancellation.
- `council` — multiple members prepare, speak under turn control, and finalize a conclusion or unresolved report.

Review is a quality gate inside delegation, not a separate top-level session type.

## Non-goals

- Do not modify Hermes core.
- Do not make the Hermes plugin the source of truth or the only recovery path.
- Do not require Discord tokens inside `atn-controld`.
- Do not treat Discord message order or transcript text as authoritative state.
- Do not replace real member profiles with simulated role prompts.
- Do not run multiple concurrent sessions in Release v1.

## Release v1 scope

Release v1 covers registry, storage, daemon, CLI, protocol/conformance contracts, member runtime contract, `hermes-agent` runner adapter, delegation, review gate, council, transcript/export, distribution, observability, disaster recovery, and tests. The Python Hermes plugin is delivered by the companion repository and must remain an adapter over this daemon contract.
