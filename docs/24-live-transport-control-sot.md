# Live Transport Control SOT

## Status

This document is the control-side Source of Truth for planned post-Release local live transport work across `kkachi-agent-networkd`, the `kkachi-agent-network` CLI, and the companion `kkachi-agent-network-plugin`.

The plugin-side companion SOT is `../../kkachi-agent-network-plugin/docs/10-live-transport-sot.md`. This control SOT owns daemon, CLI, protocol, conformance, member-runtime, and event-to-visible-surface delivery-evidence boundaries. The plugin SOT owns Python plugin transport, Hermes tool behavior, bundled skill/operator guidance, and plugin-side visible helper behavior.

This document does **not** authorize production activation, live Discord delivery, gateway/auth/token changes, active Hermes profile mutation, KAB bridge readiness, or replacing real participant profiles with role prompts. It defines repo ownership, epic/task distribution, required gates, and non-scope boundaries for post-Release-v1 live-local work.

## Scope

Control-side live transport scope covers the daemon/CLI/runtime authority required before a main agent can control a council session while participant agents observe daemon events and respond through a real member runtime path.

In scope:

- daemon-owned event/state authority for council and delivery evidence;
- CLI as the canonical main-agent/operator control plane;
- daemon protocol compatibility reads used by the plugin live transport;
- stream replay/follow/ack behavior and cursor failure handling;
- member runtime real profile/wrapper invocation evidence;
- event-to-visible-surface rendering contract and delivery-evidence pointer semantics;
- later local disposable live-local pilots that prove CLI/plugin/daemon equivalence without production activation.

Out of scope unless a later task explicitly opens it:

- production Hermes profile enablement;
- live/default Discord sending;
- daemon-created Discord threads;
- gateway, auth, token, credential, or provider mutation;
- localhost/TCP/gateway fallback;
- hidden plugin-to-CLI fallback;
- treating Discord message order as lifecycle state;
- replacing participant profiles with simulated role prompts;
- KAB bridge execution claims.

## Repository ownership

| Concern | Owning repo | Epic(s) | Notes |
|---|---|---|---|
| Daemon state, event append, stream, lock, cursor, projection | control | `LTRAN` | Control remains the state authority. |
| CLI main-agent/operator control plane | control | `LTRAN` | CLI commands are canonical for moderation, diagnostics, and recovery. |
| Protocol and conformance fixtures | control | `LTRAN`, `SURFD` | Plugin consumes fixtures; plugin does not invent daemon shapes. |
| Real participant profile/wrapper invocation | control | `MEMBR` | Existing `RUNRT` fake/local seam is a prerequisite, not live profile proof. |
| Plugin live Unix-socket transport | plugin | `LTRAN` | Explicit config only, fail closed when missing/unsafe/incompatible. |
| Participant-agent plugin stream/write path | plugin | `PARTC` | Participant-originated events only; main-agent control prefers CLI. |
| Visible helper/rendering surface | plugin | `SURFD` | Visible messages are presentation/evidence, not lifecycle authority. |

## Active epic handoff rule

Active task transfer between repos must happen only at an epic boundary.

Do **not** start a plugin task in the middle of a control epic. Do **not** start a control task in the middle of a plugin epic. If an active epic discovers a sibling-repo dependency, block the active epic with evidence, complete the sibling epic that owns the missing capability, then resume at the original epic boundary.

Recommended execution order:

| Order | Repo | Epic | Purpose | Next gate |
|---:|---|---|---|---|
| 1 | control | `LTRAN` | companion SOT, daemon/CLI compatibility reads, live-local fixture/equivalence support | plugin `LTRAN` may start only after all control `LTRAN` tasks complete |
| 2 | plugin | `LTRAN` | plugin explicit live transport and plugin/CLI/daemon equivalence | control `MEMBR` may start |
| 3 | control | `MEMBR` | real participant profile/wrapper invocation path | plugin `PARTC` may start |
| 4 | plugin | `PARTC` | participant plugin stream/write path and selected response proof | control `SURFD` may start |
| 5 | control | `SURFD` | event-to-visible rendering/evidence contract | plugin `SURFD` may start |
| 6 | plugin | `SURFD` | visible helper/rendering boundary and evidence pointers | later release/live pilot decision |

When a task ID is referenced outside its repo-local roadmap or SOT table, use repo-qualified notation such as `control/LTRAN-001` or `plugin/LTRAN-001` to avoid ambiguity.

## Control epics and tasks

### LTRAN: Live transport control compatibility

Exit: control exposes or confirms the daemon/CLI/protocol behavior needed for plugin live-local transport and equivalence pilots, with no production activation claim.

| Task ID | Task Title | Task Status | Task Description |
|---|---|---|---|
| LTRAN-001 | Control live transport SOT and mapping | completed | Add this companion SOT, update control roadmap/docs, cross-link plugin SOT, and record the repo-owned epic/task split and active epic handoff rule. This is a docs-only SOT/mapping exit and does not unblock plugin live transport by itself. |
| LTRAN-002 | Confirm daemon compatibility reads | planned | Confirm existing `status`, `health`, `version.read`, `stream.replay`, `stream.follow`, `stream.ack`, and council command paths are sufficient for plugin live transport, or add minimal `status.read`/diagnostic shapes with conformance tests if required. |
| LTRAN-003 | Prove CLI/daemon live-local support | planned | Run disposable data-home evidence showing CLI status/version/stream/write paths address the same daemon state needed by plugin live-local equivalence tests, including command-id/idempotency and structured-error behavior. |

#### control/LTRAN-001 docs-only acceptance

`control/LTRAN-001` is complete when the control docs record the live transport SOT, repo ownership split, cross-repo handoff rule, and companion plugin SOT link. It is a documentation and mapping task only.

Acceptance evidence:

- `docs/24-live-transport-control-sot.md` names control-owned daemon/CLI/protocol/member-runtime/surface boundaries and plugin-owned transport/helper boundaries.
- `docs/21-cross-repo-development.md` states that control `LTRAN` gates plugin `LTRAN`, while `control/LTRAN-001` alone does not unblock plugin live transport implementation.
- `docs/09-implementation-epics.md`, `docs/README.md`, and `docs/roadmap.md` keep `LTRAN-001` docs-only and leave `LTRAN-002`/`LTRAN-003` planned.
- `docs/kkachi-docs-map.yaml` already indexes this SOT and the relevant docs guardrails.

`control/LTRAN-001` does **not** confirm daemon read compatibility, create or change protocol shapes, prove disposable live-local daemon behavior, change plugin code, or authorize production/live activation. Those exits remain owned by `control/LTRAN-002`, `control/LTRAN-003`, and then plugin `LTRAN`.

#### Control-side operation mapping for later LTRAN tasks

This table mirrors the plugin SOT in control terms without implementing or confirming new daemon shapes. `control/LTRAN-002` owns compatibility confirmation or minimal conformance-backed additions; `control/LTRAN-003` owns disposable live-local proof.

| Plugin-side operation | Current or candidate control path | Control owner | Requirement before plugin equivalence claims |
|---|---|---|---|
| `version.read` | daemon/CLI `version.read` | `control/LTRAN-002` confirmation | Direct compatibility read must expose protocol version and feature evidence. |
| `status.read` | existing `status` or future `status.read` | `control/LTRAN-002` confirmation/addition | Response must identify protocol version, daemon version, feature groups, and capability state without plugin guessing. |
| `diagnostics.read` | existing `health` or future `diagnostics.read` | `control/LTRAN-002` confirmation/addition | Plugin diagnostics must normalize from explicit daemon output and fail closed on missing/unknown shape. |
| `stream.tail` | `stream.replay` / bounded replay-tail behavior | `control/LTRAN-002` confirmation | Preserve replay-before-live, cursor, member, and limit semantics. |
| future `stream.follow` | `stream.replay` with bounded follow or daemon follow stream | `control/LTRAN-002` confirmation | Must be bounded, resumable, and fail closed on gaps or unknown schemas. |
| future `stream.ack` | `stream.ack` | `control/LTRAN-002` confirmation | Acknowledge only after processing or durable failure recording. |
| `command.submit` | concrete daemon commands such as `delegate.*`, `council.*`, and delivery-evidence commands | `control/LTRAN-002` confirmation plus `control/LTRAN-003` local proof | Do not assume a generic daemon alias unless implemented; command idempotency and structured errors must be proven before plugin equivalence claims. |

### MEMBR: Member runtime profile invocation

Exit: a selected participant can be invoked through a real member profile/wrapper path and produce or fail a participant response with durable daemon evidence. This exit does not claim always-on production runtimes.

| Task ID | Task Title | Task Status | Task Description |
|---|---|---|---|
| MEMBR-001 | Select member runtime pilot mode | planned | Decide and document whether the first pilot uses main-agent mediated invocation or long-lived member runtimes, including runner/session evidence requirements and failure policy. |
| MEMBR-002 | Prove selected participant invocation | planned | Prove `speaker_selected` causes a real participant profile/wrapper invocation, records successful `council.speak` or durable failure evidence, and does not substitute simulated role prompts. |

### SURFD: Surface delivery evidence contract

Exit: control defines and verifies the event-to-visible-surface rendering/evidence contract needed by plugin-visible helpers without making visible messages the lifecycle source of truth.

| Task ID | Task Title | Task Status | Task Description |
|---|---|---|---|
| SURFD-001 | Define surface rendering evidence contract | planned | Define the daemon event fields, transcript/projection inputs, delivery evidence status, and failure/pending-follow-up semantics needed for visible speech/final-result rendering. |
| SURFD-002 | Prove delivery evidence projection | planned | Prove local projection/transcript/export or equivalent fixtures expose speech, finalization, unresolved/cancelled, and delivery-evidence pointer states for plugin-visible rendering tests. |

## Control implementation requirements

Control tasks must preserve these invariants:

- `kkachi-agent-networkd` is the only lifecycle state authority.
- `channel.jsonl` remains canonical for ordering and phase transitions.
- CLI commands are canonical for main-agent/operator control and diagnostics.
- Plugin compatibility is through protocol/conformance, not shared source code.
- Missing or unsupported daemon features fail closed and are not guessed by the plugin.
- Participant identity is validated against the session registry snapshot.
- Cursor acknowledgement happens only after successful processing or durable failure recording.
- Delivery evidence is a pointer/status record, not proof that lifecycle progressed.

## Verification requirements

`control/LTRAN-001` docs-only verification:

```bash
make docs-guardrails
make check-plugin-contract
make test-prepare
```

Optional companion guardrail when practical:

```bash
cd ../kkachi-agent-network-plugin && make check-core-contract
```

`control/LTRAN-001` verification does not require live daemon, Hermes, Discord, KAB, gateway/auth/token, plugin mutation, external E2E, or localhost/TCP fallback evidence.

Full control live transport work is not complete without command evidence for all applicable later layers.

Baseline checks for `control/LTRAN-002`/`control/LTRAN-003` and later post-Release live-local work:

```bash
make test-prepare
make check-plugin-contract
make test-release-acceptance
make test
```

Task-specific checks must include, as applicable:

- disposable data-home daemon/CLI smoke;
- CLI status/version/health output with protocol/feature evidence;
- stream replay/follow/ack behavior with cursor gap and unknown schema failure coverage;
- command-id/idempotency behavior for participant-originated council writes;
- member runtime real profile/wrapper invocation evidence;
- delivery evidence projection/transcript/export evidence;
- sibling plugin `make check-core-contract` when protocol/fixture shapes change.

## Open decisions before implementation

1. Is existing `status`/`health`/`version.read` sufficient for plugin live transport, or is a dedicated `status.read` command required?
2. Does the first participant pilot use main-agent mediated invocation or long-lived member runtimes?
3. What exact runner/session evidence proves that a response came from the real participant profile?
4. Which event/projection fields are the minimum rendering contract for plugin-visible surface delivery?
5. Which local pilot is sufficient before any later production activation discussion?

Until these decisions are resolved, implementation may proceed only on tasks that do not depend on the unresolved decision, or the task contract must record the selected default before coding.
