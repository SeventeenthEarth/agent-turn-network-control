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
| LTRAN-002 | Confirm daemon compatibility reads | completed | Added explicit `status.read` and `diagnostics.read` compatibility reads, confirmed `version.read`, bounded `stream.replay` follow, `stream.status`, `stream.ack`, and concrete command-path feature evidence through conformance fixtures/checks. Operator `status`/`health` stay concise. |
| LTRAN-003 | Prove CLI/daemon live-local support | completed | Proved disposable data-home CLI/daemon live-local support with daemon-backed `compat` reads, stream replay/follow/ack/status, `delegate.submit` write/idempotency, structured command-id conflict behavior, first color review, GLM Octo, post-Octo re-review, and local/cross-repo verification evidence; no production activation or plugin mutation is claimed. |

#### control/LTRAN-001 docs-only acceptance

`control/LTRAN-001` is complete when the control docs record the live transport SOT, repo ownership split, cross-repo handoff rule, and companion plugin SOT link. It is a documentation and mapping task only.

Acceptance evidence:

- `docs/24-live-transport-control-sot.md` names control-owned daemon/CLI/protocol/member-runtime/surface boundaries and plugin-owned transport/helper boundaries.
- `docs/21-cross-repo-development.md` states that control `LTRAN` gates plugin `LTRAN`, while `control/LTRAN-001` alone does not unblock plugin live transport implementation.
- `docs/09-implementation-epics.md`, `docs/README.md`, and `docs/roadmap.md` keep `LTRAN-001` docs-only; at LTRAN-001 closeout they left `LTRAN-002`/`LTRAN-003` planned.
- `docs/kkachi-docs-map.yaml` already indexes this SOT and the relevant docs guardrails.

`control/LTRAN-001` does **not** confirm daemon read compatibility, create or change protocol shapes, prove disposable live-local daemon behavior, change plugin code, or authorize production/live activation. Those exits remain owned by `control/LTRAN-002`, `control/LTRAN-003`, and then plugin `LTRAN`.

#### control/LTRAN-002 compatibility-read acceptance

`control/LTRAN-002` is complete when the daemon/protocol contract exposes explicit plugin-facing compatibility reads and conformance-backed stream/command capability evidence without changing plugin code, mutating production/live integrations, or claiming disposable live-local equivalence.

Acceptance evidence:

- `docs/03-protocol-spec.md` and `docs/04-cli-spec.md` document plugin-facing `version.read`, `status.read`, and `diagnostics.read` compatibility commands as additive to operator-facing `status` and `health`.
- `internal/protocol/features.go`, `testdata/conformance/manifest.json`, and command fixtures publish `status.read`, `diagnostics.read`, bounded follow over `stream.replay`, `stream.status`, and existing command/stream feature groups for plugin fail-closed negotiation.
- Daemon tests cover compatibility reads exposing protocol/version/feature/capability evidence, preserving concise operator `status`/`health` shapes, and avoiding data-home mutation for read commands.
- Verification for run `run-20260610T014610Z-208f4877d244` passed: `git diff --check`, `make test-prepare`, `make check-plugin-contract`, `make test-release-acceptance`, `make test`, and sibling plugin `make check-core-contract`.

`control/LTRAN-002` does **not** prove disposable live-local CLI/daemon equivalence, mutate plugin code, activate production transport, contact Discord/Hermes/gateway/auth/token/provider/profile systems, or implement a KAB bridge. Those exits remain owned by `control/LTRAN-003` and later plugin-side work.

#### control/LTRAN-003 disposable live-local acceptance

`control/LTRAN-003` is complete: disposable data-home evidence proves the CLI and daemon can exercise the local state surfaces needed by plugin live-local equivalence tests, required color/Octo/final reviews passed, and no production/live activation is claimed.

Completion evidence:

- CLI `compat version|status|diagnostics --format json` reads daemon-backed plugin compatibility responses while operator-facing `status` and `daemon health/status` stay concise.
- Disposable smoke script `scripts/ltran003_live_local_smoke.py` builds temp CLI/daemon binaries under `/tmp`, starts `kkachi-agent-networkd` with a script-owned temp data home, scrubs live-service environment variables, and records redacted evidence under the KAH run directory.
- The smoke evidence covers stream replay, bounded follow, stream ack/status, invalid cursor fail-closed behavior, unsupported option fail-closed behavior, first `delegate.submit`, exact retry dedupe, and same `command_id` conflict structured error handling.
- The control repo remains the only mutated repo for this task. Plugin `LTRAN` may use this as the control-side prerequisite after the committed run closes, but plugin work still requires its own task contract, verification, and review.

This task does **not** authorize production daemon activation, live Hermes/Discord/KAB/gateway/provider/profile mutation, secret access, or plugin code mutation.

#### Control-side operation mapping for later LTRAN tasks

This table mirrors the plugin SOT in control terms. `control/LTRAN-002` adds explicit conformance-backed compatibility reads where the existing operator shapes were insufficient; `control/LTRAN-003` owns disposable live-local proof.

| Plugin-side operation | Control path | Control owner | Requirement before plugin equivalence claims |
|---|---|---|---|
| `version.read` | daemon/CLI `version.read` | `control/LTRAN-002` confirmation | Direct compatibility read must expose protocol version and feature evidence. |
| `status.read` | daemon `status.read` | `control/LTRAN-002` addition | Response identifies protocol version, daemon version, minimum plugin protocol version, feature groups/features, capability state, and operational readiness without changing operator-facing `status`. |
| `diagnostics.read` | daemon `diagnostics.read` | `control/LTRAN-002` addition | Response identifies protocol/version/feature evidence plus readiness and diagnostic categories without changing operator-facing `health`; missing/unknown shapes fail closed. |
| `stream.tail` | `stream.replay` / bounded replay-tail behavior | `control/LTRAN-002` confirmation | Preserve replay-before-live, cursor, member, and limit semantics. |
| `stream.follow` | `stream.replay` with bounded follow parameters | `control/LTRAN-002` confirmation | Must be bounded, resumable, and fail closed on gaps or unknown schemas. |
| `stream.ack` | `stream.ack` | `control/LTRAN-002` confirmation | Acknowledge only after processing or durable failure recording. |
| `command.submit` | concrete daemon commands such as `delegate.*`, `council.*`, and delivery-evidence commands | `control/LTRAN-002` confirmation plus `control/LTRAN-003` local proof | Do not assume a generic daemon alias unless implemented; command idempotency and structured errors must be proven before plugin equivalence claims. |

### MEMBR: Member runtime profile invocation

Exit: a selected participant can be invoked through a real member profile/wrapper path and produce or fail a participant response with durable daemon evidence. This exit does not claim always-on production runtimes.

| Task ID | Task Title | Task Status | Task Description |
|---|---|---|---|
| MEMBR-001 | Select member runtime pilot mode | completed | Select main-agent mediated bounded runner invocation as the first disposable local proof mode before long-lived member runtimes, with minimum runner/session evidence, fail-closed policy, and MEMBR-002 handoff conditions. |
| MEMBR-002 | Prove selected participant invocation | candidate/isolated proof | Blue accepted an isolated fake-wrapper implementation proof that `speaker_selected` dispatches only the selected registry member through the bounded runner path and records success or durable failure evidence. Real-profile invocation, live daemon/profile activation, provider/gateway/auth/token mutation, and production readiness remain unproven and approval-gated. |

#### control/MEMBR-001 docs-only acceptance

`control/MEMBR-001` is a documentation gate. It selects the first participant invocation pilot mode and records the evidence and failure rules that `control/MEMBR-002` must implement. It does not edit source code, run member profiles, activate daemons, execute KAB, mutate providers/gateways/auth/tokens/secrets, or claim production/live readiness.

Selected first pilot mode:

- Use main-agent mediated bounded runner invocation as a disposable local proof step.
- Preserve the selected member's real registry profile, wrapper boundary, and backend/session handle or redacted equivalent.
- Record durable runner/session evidence from selection through terminal outcome.
- Do not replace a missing or unsafe participant with a simulated role prompt.

Long-lived member runtimes remain the target model because participant agents ultimately need replay-first stream observation, cursor ownership, real profile/session continuity, and typed participant-originated KAN writes. They are not the first proof mode because `control/MEMBR-002` needs a smaller local proof: one selected participant invocation through a real profile/wrapper boundary with durable success or failure evidence before always-on runtime loops are introduced.

Minimum runner/session evidence:

- selected profile/member identity and the session `registry_snapshot.yaml` binding;
- command id, session id, and request id for the selected participant turn;
- runner invocation id preserved from `runner_invocation_started` through terminal outcome;
- wrapper, backend, and session handle, or redacted equivalent sufficient to prove real invocation;
- started timestamp and terminal timestamp/status;
- stdout, stderr, log, and artifact pointers as redacted evidence pointers only;
- produced typed KAN event on success, for example `council.speak` when applicable;
- durable failure event on failure, timeout, or unsafe setup.

Failure policy:

- Fail closed on registry mismatch, missing wrapper, unsafe profile, missing evidence, command id conflict, timeout, unsupported transport, cursor gap, or schema gap.
- Record durable failure instead of fake progress.
- Do not fall back from a missing real member to a role prompt.

KAS lane contract:

- Task/phase contract: `control/MEMBR-001` is docs-only; `control/MEMBR-002` owns implementation and proof of the selected invocation path.
- Prompt/profile boundary: the participant identity comes from the registry snapshot and wrapper/backend session evidence, not from a substituted role prompt.
- Acceptance criteria: the docs select the pilot mode, explain why long-lived member runtimes remain the target, define minimum evidence, define fail-closed behavior, and record MEMBR-002 handoff conditions.
- MEMBR-002 handoff/status: isolated fake-wrapper implementation proof has been Blue-accepted as candidate evidence; real-profile evidence remains separate and may run only when explicitly authorized.

KAH lane contract:

- Run/evidence rule: every proof run must have a stable run id, command/session/request ids, and a runner invocation id preserved across start and terminal outcome.
- Gate/schema rule: registry snapshot binding, wrapper/backend/session evidence, cursor continuity, supported transport, and known schema version are mandatory.
- Artifact rule: stdout, stderr, logs, and artifacts are stored as redacted pointers; secret-bearing inline output is not evidence.
- Event rule: success produces a typed KAN event such as `council.speak` when applicable; failure produces durable failure evidence.
- Failure policy: missing or inconsistent evidence fails the run closed and must not be rewritten as progress.

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
- CLI/daemon status.read/version.read/diagnostics.read output with protocol/feature evidence;
- stream replay/follow/ack behavior with cursor gap and unknown schema failure coverage;
- command-id/idempotency behavior for participant-originated council writes;
- member runtime real profile/wrapper invocation evidence;
- delivery evidence projection/transcript/export evidence;
- sibling plugin `make check-core-contract` when protocol/fixture shapes change.

## Open decisions before implementation

1. Resolved by `control/LTRAN-002`: dedicated `status.read` and `diagnostics.read` commands are required because operator-facing `status`/`health` remain concise and do not carry the full compatibility contract.
2. Resolved by `control/MEMBR-001`: the first participant pilot uses main-agent mediated bounded runner invocation as a disposable local proof before long-lived member runtimes.
3. Resolved by `control/MEMBR-001`: runner/session evidence must bind selected profile/member identity, registry snapshot, command/session/request ids, preserved runner invocation id, wrapper/backend/session handle or redacted equivalent, timestamps/status, redacted evidence pointers, and the typed success or durable failure event.
4. Which event/projection fields are the minimum rendering contract for plugin-visible surface delivery?
5. Which local pilot is sufficient before any later production activation discussion?

Until these decisions are resolved, implementation may proceed only on tasks that do not depend on the unresolved decision, or the task contract must record the selected default before coding.
