# Release v1 Roadmap

## Roadmap rule

This roadmap is for the Go control repository. Python Hermes plugin roadmap items live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics-tasks.md`.

Roadmap tasks must be **capability-sized**, not file-sized. Each row should normally be large enough for one Kkachi/KAH task contract, one implementation lane, tests, docs/evidence update, role review, and one commit. Split a task only when dependency order, approval gate, failure domain, or reviewer specialty is materially different.

Epic IDs are five-letter uppercase English slugs. Task IDs are derived from the epic ID as `{EPIC}-001`, `{EPIC}-002`, and so on. For cross-repo epics, control and plugin share one global sequential task stream and cite tasks as `control/<task-id>` or `plugin/<task-id>`; repo-local numbering gaps are expected. Status values are compact operator-facing values: `planned`, `in_progress`, `completed`, or `blocked`.

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
| ARGUE-002 | Protocol shape and conformance fixtures | completed | Additive control protocol shape and plugin-consumable static conformance fixtures for `claims[]`, `stance_links[]`, contribution type, hand-raise `target_links[]`, and structured negative examples were accepted for bounded local scope under KAS/KAH run `run-20260615T145822Z-caab064cf550` after Red `t_e2ced3fc`, Orange `t_fd35e83a`, Gray `t_c9e20348`, Blue synthesis `t_ade91c69`, and final gate `evt-001437`. This does not claim runtime validation/scoring, transcript/export rendering, plugin implementation, live Discord delivery, production activation, commit/push beyond explicit approval, or readiness for ARGUE-003/004/005. |
| ARGUE-003 | Validation and moderator scoring hooks | completed | Local Stage 1 direct Codex SDK/app-server implementation completed under KAS/KAH run `run-20260615T181228Z-b79cfade404a`: daemon/storage validation, quality-required rejection, quality-warn diagnostics, and moderator graph-need scoring hooks have focused local test evidence, color review, Octo review, post-Octo re-review, final KAH gate, and commit approval. This does not claim plugin readiness, live readiness, production activation, or push approval. |
| ARGUE-004 | Transcript/export/projection preservation | completed | Local Stage 1 direct Codex SDK/app-server implementation completed under KAS/KAH run `run-20260616T073755Z-f2fe201156c7`: ARGUE relation evidence is preserved in transcript/export/projection surfaces with malformed relation diagnostics, focused storage/transcript/projection tests, first color review, Red R1 color-adjust, focused Red re-review, official KAB GLM Octo review, and post-Octo Red/Orange/Gray re-review plus Blue synthesis. This does not claim plugin readiness, live readiness, production activation, commit/push beyond explicit approval, or operator-visible pilot readiness; plugin JSON-string parsing and missing-field transcript UX conditions remain handoff/pilot gates. |
| ARGUE-005 | Control integration verification gate | completed | Local control-side integration verification gate completed under KAS/KAH run `run-20260616T132731Z-781418864c04`: plugin-contract compatibility, ARGUE conformance fixtures, validation/scoring regressions, transcript/export/projection preservation, and full local `make test` passed after observed `plugin/ARGUE-003` commit `3f0dc55`. This does not claim plugin roadmap cleanup, plugin/ARGUE-004, live-local pilot readiness, production activation, Discord delivery, profile/plugin install, gateway/auth/token/provider mutation, commit/push beyond explicit approval, or operator-visible pilot readiness. |

Every roadmap item must map to the Makefile target taxonomy in `18-testing-strategy.md` and to the phase dependencies in `09-implementation-epics.md`. For legacy repo-owned epics, active task transfer between this control repo and the plugin repo happens only at an epic boundary. For an accepted cross-repo epic such as RUNFIX, transfer happens at the globally ordered task boundary recorded in both repos' SOT tables. When a task ID is cited outside its repo-local roadmap, qualify it as `control/<task-id>` or `plugin/<task-id>`.


## RUNFIX — KAN council runner, activation, and discussion-quality remediation

RUNFIX is a cross-repo remediation epic using a single global task sequence across control and plugin. This control roadmap lists only control-owned rows; plugin-owned rows live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics-tasks.md`.

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| RUNFIX-001 | Control remediation SOT and roadmap lock | completed | Accepted docs-only SOT lock for the control-side RUNFIX remediation contract, global cross-repo DAG, canonical readiness/fallback label taxonomy, selected-speaker runner/adaptor/ARGUE quality handoff boundaries, and roadmap/docs-map entries. Review evidence: Red `t_612b4d58`, Orange `t_c673aed4`, Gray `t_ce1b0c31`, focused Orange `t_131ea8c9`, focused Gray `t_7cec278f`, Blue synthesis `t_1bb67569`. This does not authorize RUNFIX-003..010 implementation or live readiness. |
| RUNFIX-003 | Selected-speaker member runtime dispatch | completed/local-control | Wired automatic selected-speaker dispatch so `speaker_selected` can start the selected member runner path without a custom harness. The local/control implementation preserves snapshot member identity, cursor/idempotency, canonical speech validation for selected-runner success, durable discard/failure diagnostics, and started-only replay/restart protection. Evidence does not claim live Discord delivery, production daemon activation, profile/provider/gateway/auth/token mutation, commit/push, or KAB `native_codex`. |
| RUNFIX-004 | Hermes adapter command contract and diagnostics | completed/local-control | Replaced the incorrect platform-delivery command assumption with an explicit response-generation command contract, preserved redacted invocation diagnostics, and prevented stale runner phase evidence. Completed as a local/control Stage 1 implementation under KAS/KAH run `run-20260617T101645Z-1757e05ffbcf`; final gate passed and the amended RUNFIX-004 commit includes this docs-status cleanup. This does not claim live daemon readiness, plugin readiness, production Discord delivery, profile/provider/gateway/auth/token mutation, push, or broad rollout. |
| RUNFIX-005 | ARGUE and moderator quality gates | completed/local-control | Local/control Stage 1 implementation under KAH run `run-20260618T020120Z-fe2144618fe6` separates lifecycle status from `discussion_quality`, exposes ARGUE diagnostic counts and hard-warning codes, keeps `quality_required` fail-closed after the opening window, accepts `quality_warn` speech without text mutation or inferred durable links, and adds deterministic linked hand-raise `graph_need` counts. Local evidence passed `git diff --check`, focused storage/protocol/daemon/command tests, `make docs-guardrails`, `make check-plugin-contract`, `make test-prepare`, and `make test`; Red `t_1d5692f1`, Orange `t_388bb347`, Gray `t_6fb40282`, and Blue synthesis `t_1eb87c6b` accepted the bounded local-control closeout. This does not claim live Discord delivery, production daemon activation, profile/provider/gateway/auth/token mutation, plugin implementation/readiness, commit, push, or broad rollout. |

Plugin-owned `plugin/RUNFIX-006`, `plugin/RUNFIX-007`, and `plugin/RUNFIX-008` now have local implementation proof under plugin KAH runs `run-20260618T045937Z-2e173b8309f3`, `run-20260618T081811Z-23d10e2a4634`, and `run-20260618T092359Z-401d6e5bedc0`. RUNFIX-007 extends the pure/local `kan_discussion_activation_plan` with effective Discord eligibility evidence, eligible-only `allow_list_targets`, profile remediation, parent-channel proof state, thread-only proof rejection, fallback audit, and `live_readiness: false`. RUNFIX-008 extends the same pure/local planner and packaged operator guidance with participant ARGUE response evidence, ARGUE counts, selected-runner evidence, canonical `speaker_selected -> speech` linkage, and diagnostic-only fallback disclosure. `plugin/RUNFIX-010` completed under KAH run `run-20260618T112843Z-40b023a5d9c8` with bounded PASS_WITH_RISK parent-channel visible pilot evidence, final operator package, Discord-origin live-visible default, artifact-only/daemon-CLI confirmation guardrail, and final-report separation of lifecycle/visible-turn/profile-gateway/CLI-only evidence. This control roadmap does not convert those plugin-owned proofs into control live readiness, daemon startup authority, Discord delivery, production activation, profile/provider/gateway/auth/token mutation, or broad rollout readiness. `plugin/RUNFIX-015` now has local implementation proof under plugin KAH run `run-20260619T071526Z-7d2ba33b07d5`; `plugin/RUNFIX-017` has plugin-owned local implementation proof under plugin KAH run `run-20260619T101255Z-189d01ba8b8f`; `plugin/RUNFIX-019` has plugin-owned local implementation proof under plugin KAH run `run-20260619T214004Z-4d1e54b7304a` for registry membership/reconcile activation-planner guidance after `control/RUNFIX-018`. This control roadmap records the dependency boundary only.

| RUNFIX-009 | Integrated control smoke fixtures | local implementation proof | Control KAH run `run-20260618T102752Z-7d8ccfa584e4` adds an integrated daemon smoke fixture and transcript summary fix proving runner invocation, canonical `speaker_selected -> speech` linkage, ARGUE `quality_warn` diagnostics/hard-warning exposure, and transcript/export/projection closeout evidence from deterministic local events. Verification passed focused RUNFIX-009 daemon smoke, related selected-speaker/export tests, storage package tests, `git diff --check`, `make docs-guardrails`, `make check-plugin-contract`, `make test-prepare`, and `make test`; Red plan review `t_3e1a6ecd` accepted scope. This remains control-local proof only and does not claim live Discord delivery, production daemon activation, profile/provider/gateway/auth/token mutation, plugin/RUNFIX-010 execution, commit, push, or broad rollout. |
| RUNFIX-011 | Participant runtime readiness and attendance preflight | local implementation proof | Control KAH run `run-20260618T162156Z-419f3769f2cc` adds derived participant runtime readiness from durable subscriber/cursor ack/heartbeat/attendance/preparation/selected-runner evidence, Discord-thread attendance and preparation timeout preflight, fail-closed prepare/poll/grant behavior, verbose status and `stream.status` readiness diagnostics, and deterministic injected-clock tests. This remains control-local repository proof only: no live daemon activation, live Discord delivery, profile/provider/gateway/auth/token/model mutation, plugin/RUNFIX-012 implementation, production readiness, commit, push, or broad rollout is claimed. |

Plugin-owned `plugin/RUNFIX-012` now has local implementation proof under plugin KAH run `run-20260618T231811Z-ee5c6394d1fe` for explicit consumption of the new `control/RUNFIX-011` readiness evidence in activation planner/operator guardrails. `plugin/RUNFIX-013` now has local implementation proof under plugin KAH run `run-20260619T001719Z-9c41001040ef` for bundled/operator moderation hard rules that keep live councils lifecycle-first, prevent predeclared complete speaker-order debate simulation, require per-turn justified `speaker_selected` evidence, preserve `role_order` only as a per-turn justified option, and treat daemon `speech` as state authority. `plugin/RUNFIX-015` now has local implementation proof under plugin KAH run `run-20260619T071526Z-7d2ba33b07d5` for pure/local visible-author guard planning. Plugin-owned proof rows live in the plugin roadmap and shared RUNFIX SOT tables; this control roadmap records cross-repo traceability and does not convert plugin-owned proof into control implementation authority, live Discord delivery, production/live readiness, profile/provider/gateway/auth/token/model mutation, commit, push, or broad rollout readiness. `plugin/RUNFIX-017` now has plugin-owned local implementation proof under plugin KAH run `run-20260619T101255Z-189d01ba8b8f` for ARGUE quality-required prompt/harness hardening after `control/RUNFIX-016` local implementation proof; `plugin/RUNFIX-019` now has plugin-owned local implementation proof under plugin KAH run `run-20260619T214004Z-4d1e54b7304a` for daemon registry membership planner guidance after `control/RUNFIX-018` local implementation proof. This control roadmap records cross-repo traceability only and does not convert plugin-owned proof into control implementation authority.

| RUNFIX-014 | Selected-runner terminal accounting guard | local implementation proof | Control KAH run `run-20260619T051710Z-8e1f6efb61ec` adds selected-runner terminal accounting and report/status/export guardrails so `runner_invocation_started` + `runner_invocation_failed` are not hidden by later canonical `speech` events. Control summaries now expose runner started/succeeded/failed counts, canonical speech linkage, and fallback/manual harness flags before selected-runner or live-readiness claims. Trigger evidence: 주유 rename-council handoff `/Users/draccoon/Workspace/Hermes/17thHermes/40_outputs/team/jooyoo/kan/2026-06-19-kan-rename-council-improvements-for-dev-team.md` and session `sess_kan_rename_hun2_20260619T033549Z` with 15 runner failures and 15 speech events. Local verification passed focused storage/protocol tests, full `make test`, docs guardrails, plugin-contract checks, CodeGraph refresh gate, Red/Orange/Gray review, and KAH final gate. This does not claim live Discord delivery, production daemon activation, profile/gateway/provider/auth/token/model mutation, plugin/RUNFIX-015 runtime enforcement, push, broad rollout, or live readiness. |
| RUNFIX-016 | Summary schema robustness helper | local implementation proof | Control KAH run `run-20260619T083649Z-d10e1f5cc20b` final gate passed and local commit `9c15d22` adds `internal/storage/summary_turn_accounting.go` and export manifest key `summary_turn_accounting` so `channel.jsonl` and export bundles expose stable rows with turn, member, `speaker_selected_event_id`, `speech_event_id`, `runner_invocation_id`, and visible message id. The helper tolerates `payload.plugin_evidence` objects, explicit visible/Discord evidence objects/lists, missing optional evidence after `council_finalized`, and unsupported evidence maps/lists without crashing. Arbitrary unsupported maps/lists still fail closed: they do not become visible delivery proof, `selected_runner_pass`, live readiness, plugin readiness, or production readiness. Verification evidence exists in the KAH run for focused RUNFIX-016/RUNFIX-014/SURFD-002 storage tests, full `internal/storage`, `git diff --check`, `make test`, docs guardrails, plugin-contract check, and plugin `check-core-contract`; official color/final gate passed. |
| RUNFIX-018 | Explicit council roster registry reconciliation | local implementation proof | Control KAH run `run-20260619T214003Z-8a2afe33923f` adds daemon-owned registry reconciliation before `council.new`. Missing selected moderator/participant principals are persisted to `registry.yaml` only when the roster is explicit, the principal id is valid/non-reserved, and same-named wrapper resolution succeeds through the loaded allow-list. Disabled existing principals, invalid ids, unresolved wrappers, or ambiguous/missing identity evidence fail closed before session creation. The daemon reloads the registry, snapshots it into the session, and returns `registry_reconcile` evidence; subscription/heartbeat/ack readiness remains session-scoped. |


## RUNFIX2 — KAN discussion runtime usability hardening

RUNFIX2 is a planned five-PR cross-repo epic created from the 2026-06-20 KLM/주유 live discussion dogfood. This control roadmap lists control-owned rows; plugin-owned rows live in `../../kkachi-agent-network-plugin/docs/06-implementation-epics-tasks.md`. The epic distinguishes production/operator enablement defaults from evidence-derived pass labels and does not authorize production activation, profile/provider/gateway/auth/token mutation, push, broad rollout, or unapproved live Discord delivery.

| Task ID | Task Title | Task Status | Task Description |
| --- | --- | --- | --- |
| RUNFIX2-001 | Evidence/config semantics and terminal readiness model | completed/local-control | Local control implementation separates `generated_at` from `evaluated_at` / `evaluation_mode` / freshness-reference fields in `participant_runtime_readiness`. Open sessions still use current heartbeat/ack freshness; terminal councils evaluate readiness at the latest grant/turn evidence when present, or latest readiness evidence otherwise, so naturally stale post-final heartbeat/ack does not retroactively fail the original runtime prerequisites. `*_pass` labels remain evidence-derived and are not forced true. KAH run `run-20260620T083015Z-501b09d6d173` passed final gates with official Red/Orange/Gray review and Blue acceptance; push/live activation remain separately approval-bound. |
| RUNFIX2-002 | Selected-runner Hermes adapter response-generation fix | completed/local-control | Local control implementation changes the default Hermes selected-runner argv from delivery-only `send <prompt>` to response-generation `chat -Q -q <prompt>`, records `runner_invocation_succeeded` before linked canonical `speech`, and keeps delivery/fallback output as terminal `adapter_command_mismatch` failure evidence that fallback/manual speech cannot repair. Verification used fake/isolated wrappers only; no live Hermes profiles, providers, gateway, Discord, auth, tokens, daemon activation, push, or production readiness claim. |
| RUNFIX2-003 | Discussion lifecycle closeout turns | completed/local-control | Local control implementation adds explicit `limits.max_discussion_turns` lifecycle accounting in status/export and blocks `council.propose` until T0 moderator opening, T1..Tmax selected participant discussion turns, and one selected closeout speech per participant are present. Total visible turns are `max_discussion_turns + participant_count + 2`. Existing `max_turns`, selected-runner accounting, and same-turn same-member `speaker_selected -> speech` validation remain separate. Local storage/daemon verification passed; plugin RUNFIX2-004/005 remain planned. |
