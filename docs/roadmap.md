# Release v1 Roadmap

## Roadmap rule

This roadmap is for the Go control repository. Python Hermes plugin roadmap items live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics-tasks.md`.

Roadmap tasks must be **capability-sized**, not file-sized. Each row should normally be large enough for one Kkachi/KAH task contract, one implementation lane, tests, docs/evidence update, role review, and one commit. Split a task only when dependency order, approval gate, failure domain, or reviewer specialty is materially different.

Epic IDs are five-letter uppercase English slugs. Task IDs are derived from the epic ID as `{EPIC}-001`, `{EPIC}-002`, and so on. Status values are compact operator-facing values: `planned`, `in_progress`, `completed`, or `blocked`.

## BOOTS — Bootstrap

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| BOOTS-001 | Control scaffold and local gates | completed | Create `go.mod`, `cmd/kkachi-agent-network`, `cmd/kkachi-agent-networkd`, `internal/`, Makefile target contract, docs guardrails, and binary help smoke tests that pass without external resources. |

## REGST — Registry/security

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| REGST-001 | Registry authority | completed | Implement deterministic data-home resolution, strict registry schema, fail-closed permissions/symlink/TOCTOU checks, reserved principal rejection, wrapper/env validation, per-session registry snapshot, and `registry validate/show` CLI tests. |

## STORE — Storage/event SOT

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| STORE-001 | Event-store append | completed | Implement safe session directories, session metadata, registry snapshot metadata, canonical `channel.jsonl` event append, and surface/linked-authority evidence fields with unit and integration coverage. |
| STORE-002 | Projection and replay | completed | Implement SQLite projection as a rebuildable cache, deterministic replay/rebuild, `storage verify`/`storage rebuild-projection`, doctor storage health, and projection/replay/CLI tests. |

## DAEMN — Daemon/CLI/protocol

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| DAEMN-001 | Daemon and CLI commands | completed | Implement `kkachi-agent-networkd` lifecycle, local command transport, canonical `kkachi-agent-network` commands, status/doctor/health, structured JSON errors, and stable exit categories verified through CLI integration tests. |
| DAEMN-002 | Stream and conformance | completed | Implement stream replay/follow/cursor acknowledgement, active-session lock, version/feature endpoint, command/event/error/stream fixtures under `testdata/conformance/`, and plugin-compatible protocol checks. |

## RUNRT — Runtime/runner

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| RUNRT-001 | Member runtime and runner | completed | Implement member runtime loop contract, bounded `hermes-agent` runner adapter, wrapper accounting, fake-runner tests, and operator docs. |

## DELEG — Delegation/review

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| DELEG-001 | Delegation and review gates | completed | Implement delegation lifecycle, review request/response gates, blocked/resume handling, CLI/E2E fake coverage, and audit evidence. |
| DELEG-002 | Delegation/review conformance fixture matrix | completed | Publish plugin-consumable delegation/review command and structured-error fixtures for success, duplicate/idempotency, permission/error, retryable failure policy, and malformed-response handling so kan-plugin DELRV-2 can add failure coverage without inventing control-owned shapes. |

## COUNC — Council/consensus

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| COUNC-001 | Council and consensus | completed | Implemented local council lifecycle commands, speaker/moderator policy, voting/consensus state, static conformance fixture handoff, and council tests. |

## TRANS — Transcript/distribution

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| TRANS-001 | Transcript and distribution | completed | Implemented golden transcript/export rendering, install/distribution docs, plugin handoff checks, and operator acceptance evidence. |

## RELIA — Reliability/release

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| RELIA-001 | Reliability and release acceptance | completed | Implement observability, disaster recovery, corruption handling, replay rebuild, full Release v1 acceptance suite, and release readiness evidence. |

## LTRAN — Live transport control compatibility

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| LTRAN-001 | Control live transport SOT and mapping | completed | Record the control companion SOT, roadmap/docs cross-links, daemon/CLI/plugin/member-runtime ownership split, and epic-boundary repo handoff rule. |
| LTRAN-002 | Confirm daemon compatibility reads | completed | Added/confirmed explicit `version.read`, `status.read`, `diagnostics.read`, bounded `stream.replay` follow, `stream.status`, `stream.ack`, and concrete command-path compatibility evidence with conformance fixtures/checks; operator `status`/`health` remain concise and no live-local proof is claimed. |
| LTRAN-003 | Prove CLI/daemon live-local support | completed | Proved disposable data-home CLI/daemon live-local support with daemon-backed `compat` reads, stream replay/follow/ack/status, `delegate.submit` write/idempotency, structured command-id conflict behavior, first color review, GLM Octo, post-Octo re-review, and local/cross-repo verification evidence; no production activation or plugin mutation is claimed. |

## MEMBR — Member runtime profile invocation

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| MEMBR-001 | Select member runtime pilot mode | completed | Selected main-agent mediated bounded runner invocation as the first disposable local proof before long-lived member runtimes, with real profile/wrapper identity, runner/session evidence requirements, fail-closed policy, and no role substitution. |
| MEMBR-002 | Prove selected participant invocation | candidate/isolated proof | Blue accepted an isolated fake-wrapper implementation proof that `speaker_selected` dispatches only the selected registry member through the bounded runner path and records success or durable failure evidence. Real-profile invocation, live daemon/profile activation, provider/gateway/auth/token mutation, and production readiness remain unproven and approval-gated. |

## SURFD — Surface delivery evidence contract

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| SURFD-001 | Define surface rendering evidence contract | completed/docs-only | Defines the daemon event fields, transcript/projection inputs, delivery evidence status, and failure/pending-follow-up semantics needed for visible speech/final-result rendering; Blue accepted the docs-only contract after KAN Red/Orange/Gray review. Runtime projection proof remains `control/SURFD-002`. |
| SURFD-002 | Prove delivery evidence projection | completed/local proof | Local transcript/export proof exposes speech renderability, finalization, unresolved/cancelled outcomes, and fail-closed `posted`/`failed`/`pending_followup`/missing delivery-evidence states for plugin-visible rendering tests; Blue accepted after KAN Red/Orange/Gray review. |

## ENSOT — Event/outcome visible-closeout SOT

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| ENSOT-001 | Council terminal outcome visible-surface SOT | completed/docs-only | Docs-only SOT gate for plugin `VISUX`: `draft_conclusion` and `consensus_vote*` are visible process milestones only, `council_finalized` / `council_unresolved` are durable terminal outcomes, and human-readable moderator closeout requires posted surface/projection evidence that points back to the terminal event. Accepted after KAN Red/Orange/Gray review and Blue synthesis; no plugin implementation, live Discord delivery, production activation, or commit approval is claimed. |

## ARGUE — Council argument graph and discussion quality

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| ARGUE-001 | Council argument graph SOT closeout | completed | Accepted/completed docs-only closeout for the control-owned SOT, docs index, docs map, implementation epic, and roadmap links for council discussion-quality argument graph work. Official review passed with Red `t_4a2e735f`, Orange `t_9f4b2b9c`, and Gray `t_b196d630`; Blue synthesis is satisfied by this closeout. This does not authorize protocol implementation, fixture publication, plugin changes, live Discord delivery, production activation, or `control/ARGUE-002`. |
| ARGUE-002 | Protocol shape and conformance fixtures | planned | Define additive control protocol shape and publish plugin-consumable conformance fixtures for `claims[]`, `stance_links[]`, contribution type, quality modes, hand-raise targets, invalid references, orphan speech, and transcript/export preservation. Remains not started and gated on separate explicit authorization; not authorized or unblocked by `control/ARGUE-001` acceptance alone. |
| ARGUE-003 | Validation and moderator scoring hooks | planned | Implement daemon/CLI validation, quality-required rejection, quality-warn diagnostics, and moderator graph-need scoring hooks after `control/ARGUE-002` fixtures are accepted. Not started. |
| ARGUE-004 | Transcript/export/projection preservation | planned | Preserve and render relation graph evidence in control transcript/export/projection surfaces after protocol and validation behavior are in place. Not started. |
| ARGUE-005 | Control integration verification gate | planned | Run the control-side integration gate proving protocol, validation, transcript/export/projection, and plugin-consumable fixture compatibility before any live-local pilot handoff. Not started and gated by prior ARGUE tasks. |

Every roadmap item must map to the Makefile target taxonomy in `18-testing-strategy.md` and to the phase dependencies in `09-implementation-epics.md`. Active task transfer between this control repo and the plugin repo must happen only at an epic boundary; do not leave a control epic midstream to execute plugin tasks, and do not interrupt a plugin epic with control tasks except by blocking the active epic and completing the required sibling epic first. When a task ID is cited outside its repo-local roadmap, qualify it as `control/<task-id>` or `plugin/<task-id>`.
