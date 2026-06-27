# Live Transport Control SOT

## Status

This document is the control-side Source of Truth for planned post-Release local live transport work across `atn-controld`, the `atn-control` CLI, and the companion `atn-plugin`.

The plugin-side companion SOT is `../../agent-turn-network-plugin/docs/10-live-transport-sot.md`. This control SOT owns daemon, CLI, protocol, conformance, member-runtime, and event-to-visible-surface delivery-evidence boundaries. The plugin SOT owns Python plugin transport, Hermes tool behavior, bundled skill/operator guidance, and plugin-side visible helper behavior. For plugin visible-UX work such as `plugin/VISUX-001`, the control-owned event/outcome SOT is `docs/03-protocol-spec.md` plus `docs/07-moderator-policy.md` and `docs/13-operational-contracts.md`; this document records the cross-repo handoff boundary.

This document does **not** authorize production activation, live Discord delivery, gateway/auth/token changes, active Hermes profile mutation, KAB bridge readiness, or replacing real participant profiles with role prompts. It defines repo ownership, epic/task distribution, required gates, and non-scope boundaries for post-Release-v1 live-local work.

RUNFIX update: this SOT also records the control-owned side of `RUNFIX`, the cross-repo remediation epic created from the 2026-06-17 council dogfood issues report. `control/RUNFIX-001` and `plugin/RUNFIX-002` are accepted docs-only SOT locks after Red/Orange/Gray review, focused re-check, and Blue final synthesis; they do not by themselves authorize implementation, live profile activation, gateway mutation, Discord delivery, or production readiness claims. RUNFIX3 update: this SOT also records the control-owned follow-up from the 2026-06-25 KLM live-thread postmortem; `RUNFIX3-001` is a docs-only SOT/roadmap lock and does not authorize live Discord rollout, profile/provider/gateway/auth/token/model mutation, daemon startup, push, or production readiness. NEXFIX update: this SOT records the 2026-06-26 selected-runner prompt envelope remediation epic; `control/NEWFIX-001` is implementation_complete/review_pending as the blocking control fix for projection-backed runner prompts and missing-agenda fail-closed diagnostics, while `plugin/NEWFIX-002` is implementation_complete/review_pending as the plugin-owned guidance follow-up pending focused Blue re-review and acceptance closeout.

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

For legacy repo-owned epics, active task transfer between repos must happen only at an epic boundary.

Do **not** start a plugin task in the middle of a control-owned legacy epic. Do **not** start a control task in the middle of a plugin-owned legacy epic. If an active legacy epic discovers a sibling-repo dependency, block the active epic with evidence, complete the sibling epic that owns the missing capability, then resume at the original epic boundary. For an accepted cross-repo feature/remediation epic with one global task stream, such as RUNFIX, transfer happens at the recorded repo-qualified global task boundary instead of the legacy epic boundary.

Recommended execution order:

| Order | Repo | Epic | Purpose | Next gate |
|---:|---|---|---|---|
| 1 | control | `LTRAN` | companion SOT, daemon/CLI compatibility reads, live-local fixture/equivalence support | plugin `LTRAN` may start only after all control `LTRAN` tasks complete |
| 2 | plugin | `LTRAN` | plugin explicit live transport and plugin/CLI/daemon equivalence | control `MEMBR` may start |
| 3 | control | `MEMBR` | real participant profile/wrapper invocation path | plugin `PARTC` may start |
| 4 | plugin | `PARTC` | participant plugin stream/write path and selected response proof | control `SURFD` may start |
| 5 | control | `SURFD` | event-to-visible rendering/evidence contract | plugin `SURFD` may start |
| 6 | control | `ENSOT` | terminal outcome and moderator visible-closeout event semantics | plugin `VISUX` may start only after accepted review of the closeout SOT |
| 7 | plugin | `SURFD` / `VISUX` | visible helper/rendering boundary, moderator closeout UX, and evidence pointers | later release/live pilot decision |

When a task ID is referenced outside its repo-local roadmap or SOT table, use repo-qualified notation such as `control/LTRAN-001` or `plugin/LTRAN-001` to avoid ambiguity.

For accepted cross-repo feature/remediation epics, both repos use the same epic ID and one globally sequential task stream. The owning repo is recorded in the task citation, for example `control/RUNFIX-001` and `plugin/RUNFIX-002`; repo-local task lists may skip numbers owned by the sibling repo.

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
- Disposable smoke script `scripts/ltran003_live_local_smoke.py` builds temp CLI/daemon binaries under `/tmp`, starts `atn-controld` with a script-owned temp data home, scrubs live-service environment variables, and records redacted evidence under the KAH run directory.
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

### RUNFIX: Council runner, activation, and discussion-quality remediation

Exit: ATN can be installed and activated for an approved live-local Discord discussion only after explicit control dependency checks, eligible participant profiles, bot-to-bot-free channel policy, parent-channel allow-listing, selected-speaker runner evidence, canonical speech linkage, visible-surface evidence, fallback disclosure, and ARGUE quality diagnostics have accepted evidence. Visible surface policy is thread-preferred under the approved parent channel, with direct parent-channel fallback allowed only when thread creation/posting is unsupported and explicitly disclosed. Discord-origin council requests default to live visible thread output; unless artifact-only or daemon CLI actor speech mode is explicitly confirmed before `council.new`, bootstrap/preflight must prove a bound thread or disclosed parent-channel fallback, turn-posting path, visible closeout path, real profile/gateway replies, and non-CLI-actor speech path. Until that exit, RUNFIX work must not be described as live council readiness.

The dogfood issue report showed that fallback-visible messages and lifecycle counts are not sufficient evidence of a working ATN council. RUNFIX therefore separates canonical evidence labels. Operator reports must use the snake_case label; the parenthetical prose label is display text only:

- `lifecycle_pass` (**lifecycle pass**): daemon events can complete a nominal council flow;
- `fallback_profile_pass` (**manual/fallback profile pass**): an operator obtained participant-like text through a manual profile or fallback route;
- `selected_runner_pass` (**selected-runner pass**): durable evidence must prove the selected-member causal chain `speaker_selected -> runner_invocation_started -> runner_invocation_succeeded -> speech`, where the selected runner submitted linked canonical speech for the same selected member and invocation. Durable runner failure, fallback/profile/manual text, or runner-tagged speech without `runner_invocation_succeeded` is separate diagnostic evidence and blocks `selected_runner_pass`;
- `visible_surface_pass` (**visible-surface pass**): daemon events were rendered to the approved visible surface with reconstructable delivery/projection evidence;
- `discussion_quality_pass` (**discussion-quality pass**): non-opening speech preserves ARGUE relation evidence or a justified `new_axis`, with orphan/repetition diagnostics exposed.

Transcript/export success must not be reported as visible discussion completion by itself. Final reports must separate `ATN lifecycle finalized`, `Discord visible turns posted: N/expected`, `real profile/gateway replies`, and `CLI actor speech only` so a daemon-only CLI actor run cannot be mistaken for live visible Discord output.

Discord-origin council creation is live-visible by default. Every `council.new` must explicitly declare `requested_output_mode` before the session is created; a visible `surface` or Discord `source` alone is not output intent. For `council.new`, a Discord-origin request, `requested_output_mode=live_visible_thread`, or `visible_output_required=true` requires a Discord visible `surface` before the session is created. Non-visible modes such as `activation_planning_only`, artifact-only, transcript/export aliases (`transcript/export-only` or `transcript_export_only`), daemon CLI actor speech, or local-daemon-only aliases (`local-daemon-only` or `local_daemon_only`) are allowed only when the request explicitly carries a supported `requested_output_mode`, `explicit_non_visible_override=true`, and a non-empty `override_reason` showing the user asked for that non-visible mode. The local daemon remains lifecycle/event/state authority only; it is not a user-facing discussion surface.

Legacy pre-ATN artifact and label fragments in older RUNFIX rows are historical/provenance only. They are not current public ATN aliases.

| Global Order | Repo | Task ID | Task Status | Task Description |
|---:|---|---|---|---|
| 1 | control | RUNFIX-001 | completed/docs-only | Locked this control SOT, implementation epic, roadmap, and docs-map with the RUNFIX DAG, canonical evidence labels, fallback-disclosure rules, and control-owned implementation boundaries. Accepted after Red `t_612b4d58`, Orange `t_c673aed4`, Gray `t_ce1b0c31`, focused Orange `t_131ea8c9`, focused Gray `t_7cec278f`, and Blue synthesis `t_1bb67569`. |
| 2 | plugin | RUNFIX-002 | completed/docs-only | Locked the companion plugin activation/operator SOT, roadmap, bundled guidance boundary, control dependency, and approval-gated activation contract. Accepted after the same RUNFIX-001/002 review and Blue synthesis gate. |
| 3 | control | RUNFIX-003 | completed/local-control | Wired selected-speaker dispatch from `speaker_selected` to the selected member runner path with snapshot member resolution, canonical speech validation for selected-runner success, durable discard/failure diagnostics, and started-only replay/restart protection. Evidence remains local/control-only: no live Discord delivery, production daemon activation, profile/provider/gateway/auth/token mutation, commit/push, or KAB `native_codex` claim. |
| 4 | control | RUNFIX-004 | completed | Corrected the Hermes adapter response-generation command contract and runner diagnostics, including fail-closed response-shape validation and command mismatch diagnostics. Historical implementation evidence remains local/control-only in control commit `0138b59` and KAH run `run-20260617T101645Z-1757e05ffbcf`; the current `completed` row is a later status normalization after focused re-validation, not a new readiness claim. |
| 5 | control | RUNFIX-005 | completed/local-control | Local/control implementation under KAH run `run-20260618T020120Z-fe2144618fe6` exposes `discussion_quality` separately from lifecycle state, quality diagnostics, hard-warning codes, and linked hand-raise `graph_need` counts; local tests and gates passed (`git diff --check`, focused storage/protocol/daemon/command tests, `make docs-guardrails`, `make check-plugin-contract`, `make test-prepare`, `make test`). Red `t_1d5692f1`, Orange `t_388bb347`, Gray `t_6fb40282`, and Blue synthesis `t_1eb87c6b` accepted bounded local-control closeout. No live Discord delivery, production daemon activation, profile/provider/gateway/auth/token mutation, plugin implementation/readiness, commit, push, or broad rollout is claimed. |
| 6 | plugin | RUNFIX-006 | local implementation proof | Plugin KAH run `run-20260618T045937Z-2e173b8309f3` adds pure/local `atn_discussion_activation_plan` for explicit control/RUNFIX-005 dependency, plugin install/tool visibility, daemon config/compatibility evidence, participant eligibility, parent-channel inheritance proof, planned changes, rollback, verification commands, approval gates, blockers, and separated RUNFIX evidence labels. The tool reports eligible/excluded/blocked profiles and keeps `live_readiness: false`; no apply/live-local pilot, live Discord delivery, daemon startup, profile/provider/gateway/auth/token/model mutation, production activation, commit, push, or broad rollout is claimed. |
| 7 | plugin | RUNFIX-007 | local implementation proof | Plugin KAH run `run-20260618T081811Z-23d10e2a4634` extends the existing pure/local activation planner for effective Discord eligibility evidence, eligible-only allow-list targets, excluded/blocked profile remediation, parent-channel proof state, thread-only/current-channel/manual proof rejection, and fallback audit while keeping `live_readiness: false`. No live Discord delivery, daemon startup/discovery, profile/gateway/provider/auth/token/model mutation, production activation, commit, push, or broad rollout is claimed. |
| 8 | plugin | RUNFIX-008 | local implementation proof | Plugin KAH run `run-20260618T092359Z-401d6e5bedc0` extends the pure/local activation planner, packaged operator guide, and bundled skill guidance for participant ARGUE response evidence, `claims[]`, `stance_links[]`, `contribution_type`, `new_axis_reason`, optional `evidence[]`, ARGUE counts, selected-runner evidence, canonical `speaker_selected -> speech` linkage, and diagnostic-only fallback disclosure while keeping `live_readiness: false`. Red `t_30b22678` and Orange `t_381805a1` accepted the bounded local plugin scope; Gray `t_6cb777d2` requested cross-repo status reconciliation and focused Gray `t_d65b0a83` accepted that reconciliation. No live Discord delivery, daemon startup/discovery, profile/gateway/provider/auth/token/model mutation, production activation, commit, push, or broad rollout is claimed. |
| 9 | control | RUNFIX-009 | local implementation proof | Control KAH run `run-20260618T102752Z-7d8ccfa584e4` adds an integrated daemon smoke fixture and transcript summary fix proving runner invocation, canonical `speaker_selected -> speech` linkage, ARGUE `quality_warn` diagnostics/hard-warning exposure, and transcript/export/projection closeout evidence from deterministic local events. Verification passed focused RUNFIX-009 daemon smoke, related selected-speaker/export tests, storage package tests, `git diff --check`, `make docs-guardrails`, `make check-plugin-contract`, `make test-prepare`, and `make test`; Red plan review `t_3e1a6ecd` accepted the bounded local-control scope. No live Discord delivery, production daemon activation, profile/gateway/provider/auth/token mutation, plugin/RUNFIX-010 execution, commit, push, or broad rollout is claimed. |
| 10 | plugin | RUNFIX-010 | completed/PASS_WITH_RISK | Ran the approved bounded visible-local activation pilot and published the final operator package/readiness classification, including Discord-origin `live_visible_thread` default, unified non-visible override guardrails requiring supported `requested_output_mode` plus `explicit_non_visible_override=true` and non-empty `override_reason`, and final-report separation of lifecycle/visible-turn/profile-gateway/CLI-only evidence. Bounded parent-channel visible evidence exists under plugin KAH run `run-20260618T112843Z-40b023a5d9c8`; final gate passed with carried risks. Real selected-speaker runner readiness, full ATN roster coverage, no-restart thread readiness, and always-on participant runtime readiness remain unproven. |
| 11 | control | RUNFIX-011 | local implementation proof | Control KAH run `run-20260618T162156Z-419f3769f2cc` implements derived participant runtime readiness and Discord-thread attendance/preparation preflight after `sess_rename_atn-control_20260618T152054Z` showed `attendance_requested` with no member cursors/subscribers. Control now derives readiness from durable subscriber presence, valid/fresh cursor ack, fresh heartbeat, attendance/preparation success or timeout/failure evidence, and selected-runner prerequisites; prepare/poll/grant fail closed with diagnostics when evidence is missing or stale. This is control-local repository proof only and does not claim live Discord delivery, production daemon activation, profile/gateway/provider/auth/token/model mutation, plugin/RUNFIX-012 consumption, or live readiness. |
| 12 | plugin | RUNFIX-012 | local implementation proof | Plugin KAH run `run-20260618T231811Z-ee5c6394d1fe` consumes the explicit `control/RUNFIX-011` local control proof dependency in plugin-owned activation planner/operator guardrails and reconciles plugin RUNFIX docs so gateway liveness, transcript/export artifacts, and parent-channel visible fallback cannot be reported as participant-runtime/live discussion readiness. Plugin consumption is explicit-only: this control SOT records cross-repo traceability but does not claim control implementation for plugin/RUNFIX-012, live Discord delivery, production/live readiness, profile/provider/gateway/auth/token/model mutation, commit, push, broad rollout, or plugin/RUNFIX-013 implementation. |
| 13 | plugin | RUNFIX-013 | local implementation proof | Plugin KAH run `run-20260619T001719Z-9c41001040ef` adds bundled/operator council moderation hard rules from 주유's moderation-skill-gap report: lifecycle-first prerequisites, no predeclared complete live speaker order, per-turn poll/hand-raise evaluation, justified daemon `speaker_selected`, `relevance` default with per-turn justified `targeted`, `random`, `moderator_direct`, and `role_order`, daemon `speech` event authority, moderator-opinion participant-style turns, and cancel/restart versus repair-forward guidance. This control SOT records cross-repo traceability only and does not claim live daemon/runtime activation, Discord delivery, production/live readiness, profile/provider/gateway/auth/token/model mutation, commit, push, broad rollout, or control implementation for plugin/RUNFIX-013. |
| 14 | control | RUNFIX-014 | local implementation proof | Control KAH run `run-20260619T051710Z-8e1f6efb61ec` implements selected-runner terminal accounting and report/status/export guardrails from 주유's selected-runner failure feedback session evidence. A run with `runner_invocation_started` and `runner_invocation_failed` before later fallback/manual canonical `speech` remains lifecycle/fallback evidence, not automatic selected-runner success. Control exposes started/succeeded/failed runner counts, canonical speech linkage, and fallback/manual harness flags before any selected-runner or live-readiness claim. This is control-local repository proof only and does not claim live Discord delivery, production daemon activation, profile/gateway/provider/auth/token/model mutation, plugin/RUNFIX-015 implementation, push, broad rollout, or live readiness. |
| 15 | plugin | RUNFIX-015 | local implementation proof | Plugin KAH run `run-20260619T071526Z-7d2ba33b07d5` implements pure/local pre-`council.new` visible author guard proof in `atn_discussion_activation_plan`. Plugin/operator evidence must provide explicit same-path per-profile Discord author probes, expected author source (`registry_snapshot` or approved profile-author map), source-env/posting-path evidence, shared-default-then-profile-local env precedence proof, per-turn Discord message id/member/author/speech linkage, and separated final result fields; missing/shared-default/unexpected evidence fails closed without live Discord delivery or runtime/profile/provider/gateway/auth/token/model mutation. |
| 16 | control | RUNFIX-016 | local implementation proof | Control KAH run `run-20260619T083649Z-d10e1f5cc20b` final gate passed and local commit `9c15d22` adds `internal/storage/summary_turn_accounting.go` and export manifest key `summary_turn_accounting` for canonical summary/turn-accounting rows from `channel.jsonl` and export bundles. Rows are stable across turn, member, `speaker_selected_event_id`, `speech_event_id`, `runner_invocation_id`, and visible message id where available. The helper tolerates `payload.plugin_evidence` objects, explicit visible/Discord evidence objects/lists, missing optional evidence after `council_finalized`, and unsupported evidence maps/lists without crashing. Unsupported arbitrary maps/lists do not become visible delivery proof, `selected_runner_pass`, live readiness, plugin readiness, or production readiness. Focused RUNFIX-016/RUNFIX-014/SURFD-002 storage tests, full `internal/storage`, `git diff --check`, `make test`, docs guardrails, plugin-contract check, and plugin `check-core-contract` passed in run artifacts; official color/final gate passed. |
| 17 | plugin | RUNFIX-017 | local implementation proof | Plugin-owned local implementation proof under plugin KAH run `run-20260619T101255Z-189d01ba8b8f` after official color review, Blue synthesis, and final KAH gate. ARGUE quality-required prompt and runner contract hardening provides compact prior claim graph targets to selected participants, preserves `claims[]`, `stance_links[]`, `contribution_type`, `new_axis_reason`, and `evidence[]`, and fails `discussion_quality_pass` on the first orphan non-opening speech in `quality_required` pilots while exposing repeated-orphan counts without synthetic relation inference. This control SOT records cross-repo traceability only and does not claim final local implementation proof, live readiness, production readiness, Discord delivery, install, commit, push, rollout, daemon activation, or profile/provider/gateway/auth/token mutation for plugin/RUNFIX-017. |
| 18 | control | RUNFIX-018 | local implementation proof | Control KAH run `run-20260619T214003Z-8a2afe33923f` adds daemon-owned registry reconciliation for `council.new`: when the approved council roster is explicit, each missing principal id is syntactically valid/non-reserved, and a same-named Hermes wrapper resolves through the loaded registry allow-list, the daemon writes the missing member to `registry.yaml`, reloads it, snapshots it into the session, and returns `registry_reconcile` evidence. Ambiguous principals, unresolved wrappers, disabled existing principals, invalid ids, or missing registry evidence fail closed before session creation. Registry membership is persistent identity authority; subscription/heartbeat remains session-scoped runtime evidence. |

Control-owned RUNFIX implementation must fail closed on missing selected-speaker dispatch evidence, missing participant runtime subscriber/ack/heartbeat readiness, stale cursor ack, invalid cursor, attendance/preparation timeout or missing response evidence, adapter response-generation mismatch, registry/profile identity mismatch, cursor gap, orphan speech in quality-required mode, stale runner phase, or fallback-only evidence mislabeled as ATN success.


### RUNFIX2 discussion-runtime hardening contract

RUNFIX2 is the follow-up hardening epic for the 2026-06-20 live KLM/주유 dogfood where visible Discord turns were posted but daemon selected-runner invocations failed with `malformed_or_missing_response`. The root contract is:

- Enablement/config defaults and evidence labels are separate. Production/operator activation may require live-visible output, participant runtime readiness, and selected-runner execution by default; `visible_surface_pass`, `participant_runtime_live_ready`, `selected_runner_pass`, and `discussion_quality_pass` remain derived from durable evidence and fail closed when evidence is missing or failed.
- Terminal-session readiness must not be reinterpreted only through current heartbeat freshness after finalization. Final reports need a grant/turn-time readiness verdict, plus any current/stale diagnostic separately. Control `stream.status` now exposes `generated_at` separately from `evaluated_at`/`evaluation_mode`/freshness reference fields so terminal reports can preserve event-time readiness without forcing pass labels true.
- Selected-runner success requires `speaker_selected -> runner_invocation_started -> runner_invocation_succeeded -> speech` for the selected member. A daemon adapter that calls a delivery-only Hermes command such as `send <prompt>` is a contract failure; fallback/profile CLI text is diagnostic only and cannot repair `selected_runner_pass` after `runner_invocation_failed`. Selected-runner participant/moderator stdout uses compact JSONL with `payload.speech` as the producer contract; the consumer must ignore Hermes CLI control lines such as `session_id: ...`, may normalize a single pretty/multiline JSON object as compatibility input, and may copy `payload.message`/`content`/`text` into missing `payload.speech` before canonical validation. CLI `council.grant` uses an extended selected-runner timeout so normal response generation is not misreported as a daemon socket timeout.
- Discussion turns use `limits.max_discussion_turns` for participant discussion turns only. Control status/export expose `discussion_lifecycle`, and export `summary_turn_accounting` rows add lifecycle stage plus visible turn index/total where evidence exists. Total visible turns are `max_discussion_turns + participant_count + 2`: T0 moderator opening, discussion turns, one closeout turn per participant, and final moderator summary/conclusion.
- Human-visible Discord transcript rows must be concise. Event ids, session ids, runner ids, role/color, and detailed `speaker_selected`/`speech` identifiers remain in audit/export/status evidence, not in the Discord message body.

RUNFIX2 task order and current local-proof status:

| Global Order | Repo | Task ID | Status | Control-owned acceptance |
|---:|---|---|---|---|
| 1 | control | RUNFIX2-001 | completed/local-control | Local control implementation now separates status generation time from readiness evaluation time and tests terminal event-time participant runtime readiness without forcing pass labels true. KAH final gates, official Red/Orange/Gray review, and Blue acceptance passed; push/live activation remain separately approval-bound. |
| 2 | control | RUNFIX2-002 | completed/local-control | The Hermes selected-runner adapter now defaults to response-generation `chat -Q -q <prompt>`, records `runner_invocation_succeeded` plus linked canonical selected-member `speech`, accepts compact JSONL as the producer stdout contract, normalizes single pretty/multiline JSON objects as compatibility input, and preserves delivery/fallback output as terminal `adapter_command_mismatch` diagnostics. Proof is local fake/isolated wrapper and daemon/storage tests only; any real profile pilot remains separately approval-bound. |
| 3 | control | RUNFIX2-003 | completed/local-control | Local control status/export now expose `discussion_lifecycle`, summary turn rows add lifecycle stage and visible turn index/total where evidence exists, and `council.propose` fails closed until the configured participant discussion window plus one selected closeout speech per member are present. `council.unresolved` remains the fail-closed terminal path; selected-runner and visible-surface pass labels remain separate evidence-derived gates. |
| 4 | plugin | RUNFIX2-004 | local plugin implementation proof | Plugin KAH run `run-20260620T121230Z-dea687353d54` adds local pure-renderer visible transcript cleanup that consumes control audit/status evidence without hiding audit data. No live Discord delivery, production readiness, pilot success, daemon/profile/provider/gateway/auth/token mutation, commit, push, or broad rollout is claimed. |
| 5 | plugin | RUNFIX2-005 | local plugin implementation proof | Plugin KAH run `run-20260620T131542Z-ae344b0d5ee9` adds local explicit-evidence planner/schema/test support for `integrated_discussion_proof`. The proof axes are separated: lifecycle, selected-runner, participant runtime readiness at grant/turns, visible-surface, clean-transcript, visible-closeout, fallback, discussion-quality, and final labels. Selected-runner proof requires explicit runner success plus canonical linked speech for the selected member; manual/profile fallback remains diagnostic-only and cannot repair selected-runner failure. This control SOT records the plugin-owned companion status only: `live_readiness` remains false, and this is not live pilot success, production readiness, live Discord delivery, daemon/profile/provider/gateway/auth/token/model mutation, push, or broad rollout. |


### RUNFIX3 live-visible council contract hardening

RUNFIX3 records the 2026-06-25 KLM live Discord thread postmortem. The SOT is `17thHermes:40_outputs/team/macho/atn/2026-06-25-atn-live-visible-council-contract-hardening-sot.md`. The failed session `sess_klm_selected_runner_20260625T085557Z` is diagnostic-only evidence and must not be counted as a successful live-visible council.

The control-owned contract impact is:

- `limits.max_discussion_turns` means participant discussion turns T1..TN. Expected visible turns are T0 moderator opening, T1..TN selected participant discussion, TN+1..TN+P participant closeouts, and TN+P+1 moderator final synthesis: `max_discussion_turns + participant_count + 2`.
- `council.propose` / finalization paths must fail closed until lifecycle prerequisites are satisfied unless the moderator explicitly closes unresolved.
- `selected_runner_pass` requires durable runner success plus linked canonical selected-member `speech`; manual/profile/fallback text and moderator reposting are diagnostic only.
- `selected_runner_pass` is frozen for RUNFIX3 as an evidence-derived label. It is not a daemon `selection_mode`, selection policy, or feature toggle; any future mandatory selected-runner policy needs a separately named and separately approved field such as `selected_runner_required`.
- Exact origin Discord thread binding and per-turn delivery evidence remain visible-surface proof requirements; display-name targets are diagnostic only.
- Control follow-up work is `control/RUNFIX3-004`; plugin guidance and planner/schema work remains plugin-owned in `plugin/RUNFIX3-002` and `plugin/RUNFIX3-003` and must consume this frozen control evidence contract.
- RUNFIX3-wide completion requires a final closeout artifact at `40_outputs/team/macho/atn/<date>-runfix3-final-live-visible-council-closeout.md` with bound-thread evidence, per-turn daemon/runner/speech/delivery linkage, negative fail-closed matrix results, Red/Orange/Gray review, 마초 Blue synthesis, and 주군 approval.

RUNFIX3 task order and current status:

| Global Order | Repo | Task ID | Status | Control-owned acceptance |
|---:|---|---|---|---|
| 1 | cross-repo | RUNFIX3-001 | completed | Docs-only SOT and roadmap lock accepted after Red/Orange/Gray review and Blue synthesis. It records the KLM live-thread failure as diagnostic-only and defines the follow-up task split; no runtime mutation or live readiness claim. |
| 2 | plugin | RUNFIX3-002 | implementation_complete/review_pending | Plugin-owned moderator/operator guidance hardening against the frozen control evidence contract is implemented and awaiting focused reviewer acceptance after cross-repo mirror/status reconciliation. Fixed minimum plugin gates passed; focused Red/Orange/Gray review and Blue synthesis remain pending acceptance. |
| 3 | plugin | RUNFIX3-003 | implementation_complete/review_pending | Plugin-owned planner/schema/operator-evidence support that consumes the frozen control evidence contract is implemented and awaiting focused reviewer acceptance. Exact-origin proof now fails closed on mismatched observed targets, visible-turn accounting is grounded to `max_discussion_turns + participant_count + 2`, daemon registry membership remains a live-visible start gate, and RUNFIX3 final-acceptance axes remain separate from start authority while staying fail-closed in the report surface. Fixed minimum implementation/test gates include plugin docs guardrails, core contract, test-prepare, and a completed `make test-unit` run as a deliberate tightening over the older waiver wording. Focused Red/Orange/Gray review and Blue synthesis remain pending acceptance. |
| 4 | control | RUNFIX3-004 | implementation_complete/review_pending | Control-owned diagnostics/enforcement follow-up against frozen evidence-label semantics is implemented and under focused review. Current scope covers lifecycle, selected-runner, delivery evidence, exact thread binding, missing closeouts, missing synthesis, and unresolved closeout diagnostics. Fixed minimum gates passed; Red/Orange/Gray review plus Blue synthesis remain pending acceptance. Durable verification evidence: `docs/evidence/2026-06-26-runfix3-004-diagnostics-evidence.md`. |


### NEXFIX selected-runner prompt envelope remediation

NEXFIX records the 2026-06-26 KLM token/speed dogfood defect where lifecycle/runtime readiness passed, `agenda_locked` contained the decision question and success criteria, but selected-runner dispatch used a default prompt envelope that omitted agenda and prior-context. The participant therefore produced canonical `speech` content saying the agenda body was missing. The SOT lock is `17thHermes:40_outputs/team/macho/atn/2026-06-26-atn-selected-runner-prompt-envelope-nexfix-sot.md`. Control `NEWFIX-001` and plugin `NEWFIX-002` are completed for local implementation/documentation scope after focused color review, traceability repair, and Blue synthesis recorded in `/Users/draccoon/Workspace/SeventeenthEarth/agent-turn-network/feedback.md`.

Control-owned contract impact:
- `speaker_selected` may remain a compact floor-grant event, but selected-runner dispatch must build the participant prompt from authoritative daemon projection over the session event log.
- Mandatory prompt context for `control/NEWFIX-001` includes `session_id`, locked `decision_question`, `success_criteria`, `out_of_scope_policy`, selected member id plus role/stance assignment, turn, causation event id, required response schema, bounded prior speech/claim context when present, discussion-quality/ARGUE stance rule when prior claims exist, and an explicit missing-context fail-closed instruction.
- If the locked agenda cannot be reconstructed, control must fail closed before invoking the runner with a generic prompt and must record durable diagnostic evidence.
- Prompt evidence must be reviewable through a redacted excerpt, deterministic fixture hash, or equivalent non-secret proof that agenda context was included.
- The named control-to-plugin handoff target is `selected_runner_prompt_evidence`. `control/NEWFIX-001` must define and expose it through status/export, deterministic fixture output, or an equivalent non-secret prompt-capture artifact with at least: `session_id`, `speaker_selected_event_id`, `selected_member`, `turn`, `agenda_event_id` or agenda source event ids, prior-context source event ids when present, `included_context[]`, `missing_required_context[]`, `prompt_context_sha256`, redacted prompt excerpt or fixture id, and `result` (`pass` or `blocked`). Plugin/operator guidance in `plugin/NEWFIX-002` must consume this named evidence contract rather than inventing a separate proof surface.
- Plugin hints such as `agenda_event_id` or `prompt_context_hint` are not authority in this epic unless a later approved task adds them; daemon projection remains authoritative.

NEXFIX task order and current status:

| Global Order | Repo | Task ID | Status | Control-owned acceptance |
|---:|---|---|---|---|
| 1 | control | NEWFIX-001 | completed | Projection-backed selected-runner prompt envelope, durable `selected_runner_prompt_evidence`, and fail-closed diagnostics are accepted after focused color review and traceability repair. Hidden generic fallback is removed from `SelectedSpeakerDispatchHandler`; nil or empty prompt-builder output now records a durable `selected_runner_dispatch_failed` instead of launching the runner. Verification passed: `git diff --check`, `go test ./internal/storage ./internal/daemon -run 'NEWFIX001|SelectedRunnerPrompt|SelectedSpeaker|RUNFIX009|RUNFIX011' -count=1`, and `make test`. |
| 2 | plugin | NEWFIX-002 | completed | Plugin-owned content-plane readiness guidance and missing-context participant diagnostics are accepted after focused color review and traceability repair. Verification passed in plugin: `git diff --check` and `make test`. Control records dependency/status only and does not treat plugin guidance as daemon prompt authority. |


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

Long-lived member runtimes remain the target model because participant agents ultimately need replay-first stream observation, cursor ownership, real profile/session continuity, and typed participant-originated ATN writes. They are not the first proof mode because `control/MEMBR-002` needs a smaller local proof: one selected participant invocation through a real profile/wrapper boundary with durable success or failure evidence before always-on runtime loops are introduced.

Minimum runner/session evidence:

- selected profile/member identity and the session `registry_snapshot.yaml` binding;
- command id, session id, and request id for the selected participant turn;
- runner invocation id preserved from `runner_invocation_started` through terminal outcome;
- wrapper, backend, and session handle, or redacted equivalent sufficient to prove real invocation;
- started timestamp and terminal timestamp/status;
- stdout, stderr, log, and artifact pointers as redacted evidence pointers only;
- produced typed ATN event on success, for example `council.speak` when applicable;
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
- Event rule: success produces a typed ATN event such as `council.speak` when applicable; failure produces durable failure evidence.
- Failure policy: missing or inconsistent evidence fails the run closed and must not be rewritten as progress.

### SURFD: Surface delivery evidence contract

Exit: control defines and verifies the event-to-visible-surface rendering/evidence contract needed by plugin-visible helpers without making visible messages the lifecycle source of truth.

| Task ID | Task Title | Task Status | Task Description |
|---|---|---|---|
| SURFD-001 | Define surface rendering evidence contract | completed/docs-only | Defines the daemon event fields, transcript/projection inputs, delivery evidence status, and failure/pending-follow-up semantics needed for visible speech/final-result rendering. Blue accepted the docs-only contract after ATN Red/Orange/Gray review; runtime projection proof remains `control/SURFD-002`. |
| SURFD-002 | Prove delivery evidence projection | completed/local proof | Local transcript/export proof exposes speech renderability, finalization, unresolved/cancelled outcomes, and `posted`/`failed`/`pending_followup`/missing delivery-evidence states for plugin-visible rendering tests. Blue accepted the local proof after ATN Red/Orange/Gray review; plugin implementation remains separate. |

`control/SURFD-001` resolves the minimum rendering contract as a docs-only SOT gate:

- `channel.jsonl` cursor order is the rendering authority; visible room order, timestamps, and message ids are evidence/display data only.
- `session_created.payload.surface` identifies the visible room; `session_created.payload.linked_authority` identifies required return targets but does not prove return completion.
- `speaker_selected` proves the floor grant; `speech` carries renderable participant utterance content; renderers must fail closed on missing/mismatched floor-grant evidence instead of trusting external message authorship.
- `council_finalized` / `council_unresolved` are the durable final/unresolved outcomes; visible final messages are evidence pointers, not lifecycle authority.
- Delivery/return statuses are `posted`, `failed`, `pending_followup`, or missing/unproven; `failed`, `pending_followup`, and missing evidence must not be reported as completed visible delivery.
- Replay, transcript, export, status, and projection rebuild expose evidence fields but remain side-effect free: no Discord API calls, Kanban comments, Vault writes, synthesized message ids, or inferred `posted` evidence.

Review evidence: Red `t_c0eff6d8`, Orange `t_d6beef4e`, Gray `t_a39ec23b`, Blue synthesis `t_37d1f0b9`. This acceptance is docs-only and does not approve live/default Discord delivery, gateway/auth/token/provider/profile mutation, live daemon activation, plugin SURFD implementation readiness, or production readiness.

`control/SURFD-002` accepted local proof implements the first local projection evidence over this contract:

- Markdown transcripts include a `Visible Surface Projection Summary` derived from cursor-ordered events.
- Speech rows show whether the selected-speaker floor grant makes a `speech` event renderable or fail-closed as `floor_grant_missing_or_mismatched`.
- Finalized, unresolved, and cancelled terminal events project visible-surface delivery as `posted`, `failed`, `pending_followup`, or `missing/unproven` without collapsing failure/pending/missing into success.
- Explicit delivery statuses require reconstructable non-empty evidence pointers; unsupported, proofless, or empty evidence values fail closed as `missing/unproven`.
- Export bundles include `surface_delivery_projection` in `bundle_manifest.json`, preserving visible-surface and linked-authority evidence pointers for plugin-visible fixture checks.
- This remains local proof only; it does not perform external delivery, mutate gateway/auth/token/provider/profile state, start live daemons, or approve plugin SURFD readiness.

Review evidence: initial Red `t_592ce309`, Orange request-change `t_89ec92f3`, Gray request-change `t_b6872961`, Blue request-change `t_ab9fa678`; remediation re-reviews Red `t_5fd8db68`, Orange `t_b970af89`, Gray request-change `t_3e602238`, Red `t_e0a198b5`, Gray request-change `t_5471dea7`, Red `t_d6e6102d`, Gray approve `t_f5a57911`; final Blue synthesis `t_aaafacae`.

### ENSOT: Event/outcome visible-closeout SOT

Exit: control identifies the canonical daemon event semantics and evidence split that plugin visible-UX work must implement when presenting council terminal closeout to an operator.

| Task ID | Task Title | Task Status | Task Description |
|---|---|---|---|
| ENSOT-001 | Council terminal outcome visible-surface SOT | completed/docs-only | Docs-only SOT gate clarifying that `draft_conclusion` and `consensus_vote*` are non-terminal visible process milestones, `council_finalized` / `council_unresolved` are durable terminal outcomes, and human-readable moderator closeout is accepted only with posted surface/projection evidence that points back to the terminal event. Accepted after ATN Red/Orange/Gray review and Blue synthesis; this does not claim plugin implementation, live Discord delivery, production activation, or commit approval. |

`control/ENSOT-001` handoff requirements for `plugin/VISUX-001`:

- Plugin visible UX must render `draft_conclusion` as a draft/proposal and `consensus_vote_requested` / `consensus_vote` as voting progress, not as a final closeout.
- Plugin visible UX may render final/unresolved closeout only from `council_finalized` or `council_unresolved` terminal events, preserving cursor order and the exact terminal event pointer.
- Plugin visible UX must not treat a terminal daemon event alone as proof that a human-readable moderator closeout was delivered. It needs posted surface evidence or an equivalent transcript/export/projection pointer.
- Missing, failed, pending, or mismatched closeout evidence must fail closed as visible closeout incomplete, even when the durable council outcome is terminal.
- Plugin implementation remains responsible for rendering/delivery behavior and Discord/helper mechanics; control owns the event semantics, evidence contract, and fail-closed acceptance boundary.

## Control implementation requirements

Control tasks must preserve these invariants:

- `atn-controld` is the only lifecycle state authority.
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
cd ../agent-turn-network-plugin && make check-core-contract
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
4. Resolved by `control/SURFD-001`: the minimum rendering contract is cursor-ordered durable events plus explicit surface/linked-authority evidence pointers; visible room artifacts are evidence/display data only, and delivery completion requires `posted` evidence.
5. Which local pilot is sufficient before any later production activation discussion?

Until these decisions are resolved, implementation may proceed only on tasks that do not depend on the unresolved decision, or the task contract must record the selected default before coding.
