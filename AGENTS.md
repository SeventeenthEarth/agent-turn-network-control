# AGENTS.md — ATN control repo

This file is the local agent contract for `/Users/draccoon/Workspace/SeventeenthEarth/agent-turn-network/agent-turn-network-control`.

<!-- KAS:MANAGED:BEGIN core-behavior -->
## KAS-managed baseline behavior

These repo-local instructions preserve the useful baseline guardrails from the
`andrej-karpathy-skills` `CLAUDE.md` lineage and adapt them for the ATN control repository.
These repo-local instructions are optional development guardrails only. They do
not make KAS, KAH, KAB, or any profile-local skill suite a prerequisite for
working on this repository, and they do not authorize profile mutation,
auth/token changes, provider/model/gateway changes, commits, pushes, live
activation, Discord delivery, or deployment.

Operating principles:

- Think before coding: read the named source of truth, identify constraints,
  state evidence-backed assumptions, and surface real uncertainty instead of
  guessing.
- Simplicity first: prefer the smallest change that satisfies the task; do not
  add speculative features, compatibility layers, abstractions, or fallbacks.
- Surgical changes: touch only files required by the task, preserve unrelated
  project-local text, and do not reformat or refactor adjacent work.
- Goal-driven execution: turn the task into verifiable checks, run focused tests
  or explicitly approved project gates, read the results, and report exact
  evidence honestly.
- Artifact-first detail: keep chat/console output compact and point to durable
  artifacts when long plans, logs, diffs, reviews, or evidence are needed.

Layer boundaries:

- ATN control repository source, tests, and docs are sufficient for ordinary
  development. Optional Kkachi workflow helpers may record evidence or reviews
  when explicitly selected, but absence of those helpers or profile-local phase
  skills must not block normal code/docs work.
- Control owns daemon, CLI, protocol, event/state authority, replay/projection,
  and operational contracts.
- Backend or workflow helpers must not silently substitute lanes or mutate
  auth/token/provider/model/gateway settings without explicit authority.
<!-- KAS:MANAGED:END core-behavior -->

<!-- PROJECT:LOCAL:BEGIN -->

## Identity and scope

- Project: `atn-control`.
- Lane: ATN control repo only. Do not claim KAB, KAH, KAS, ATN plugin, or whole Kkachi authority from this file.
- Blue command owner for this lane: 마초 / `macho`.
- Companion plugin repo: `/Users/draccoon/Workspace/SeventeenthEarth/agent-turn-network/agent-turn-network-plugin`.
- Team/channel ownership after the ATN rename: 마초 Blue, 서황 Red, 종회 Orange, 만총 Gray.

## Optional development helpers

Profile-local Kkachi/KAS phase skills are development conveniences, not project
requirements and not ATN runtime/operator skills. Do not mention or require profile-local phase-skill names in this repository's product docs or install path.
Ordinary direct edits, tests, docs updates, and reviews may proceed from the repo
SOT and the commands documented here.

When an explicitly selected workflow helper is available, `.kkachi/` and
`.kkachi-workflow.yaml` may be used as evidence/state helpers. If those helpers
are unavailable, record the direct-development evidence instead of blocking the
work solely because KAS/KAH is absent.

## Authority order

When instructions conflict, use this order:

1. 주군's explicit current instruction.
2. Team registry authority from `/Users/draccoon/Workspace/Hermes/17thHermes/01_references/team/team-agent-registry.yaml`.
3. This repo's SOT/docs:
   - `docs/24-live-transport-control-sot.md`
   - `docs/09-implementation-epics.md`
   - `docs/roadmap.md`
   - `docs/03-protocol-spec.md`
   - `docs/04-cli-spec.md`
   - `docs/07-moderator-policy.md`
   - `docs/13-operational-contracts.md`
   - `docs/18-testing-strategy.md`
   - `docs/21-cross-repo-development.md`
4. Optional workflow helper state under `.kkachi/` only when that helper is explicitly selected and available.

Do not infer Kkachi helper policy from memory alone. Resolve the relevant source or evidence file before architecture, readiness, activation, review, or final claims.

## Task classification

Before broad development work, classify the task and record the reason in the task notes or workflow artifacts when such artifacts are in use:

- `development`: code, tests, build behavior, architecture, process contract, executable contract, or behavior-changing work. Use the full development spine.
- `docs_only`: durable docs/SOT/roadmap/contract edits with no executable behavior change. Use docs/contract verification and explicit skipped-phase reasons.
- `research_evidence`: read-only investigation, current-state evidence, option comparison, or log/source inspection.
- `simple_command_report`: bounded command/status check with no durable project change.
- `bootstrap_config`: repo/KAH project/profile/manifest/tooling/test-bed setup. Require explicit approval for auth, secret, gateway, provider, profile, daemon activation, or live runtime mutation.
- `collaboration_review`: durable review/risk/team-feedback routing; use Kanban for official team-member review.

## Current workflow graph

The optional project graph is `.kkachi-workflow.yaml`. Validate it before use when the helper is explicitly selected.

Optional development-class spine:

```text
intake -> sot -> roadmap -> task-classification -> plan -> vet -> ask -> implement -> enhance-test -> ai-slop-cleaner -> optimize -> docs -> verify -> review -> request-feedback-1 -> handle-feedback-1 -> mar-review -> second-color-review -> final
```

The applied graph id is `graph-kkachi-project-kkachi-agent-network-control-kas-v017-kah-v013-20260621` and the KAH apply event is `evt-002756`. That graph is historical local workflow state; current local toolchain metadata is `.kkachi/toolchain.yaml` and is refreshed for KAS v0.2.0 / KAH v0.2.0, with KAT v0.1.0 test-runner config in `.kkachi/tester.yaml`.

Important semantics:

- Plan-stage development defaults to Blue vet plus official Red/Orange plan review when KAS/KAH roadmap policy requires it.
- `delegate_task`, temporary helper agents, and ad-hoc subagents are not official review evidence.
- Implementation/final acceptance normally requires official Red/Orange/Gray Kanban review plus dependent Blue synthesis.
- Color review is a convergence loop: valid requested changes return to the selected implementer lane, verification reruns, and focused re-review continues until no valid change requests remain.
- MAR review is the default independent review lane for development/implementation tasks unless 주군 explicitly waives or replaces it before start; required roles are `logic`, `security`, `arch`, `cve`, and `test_adequacy`.
- Non-development tasks must preserve explicit skipped-phase reasons instead of silently inheriting the full spine; MAR phases are not forced on classes where policy marks them not applicable.

## ATN control repo boundaries

In scope for this repo:

- Go control daemon and CLI.
- Protocol, command envelopes, structured errors, stream, event SOT, replay/projection, transcript/export.
- Member runtime control-side invocation evidence and fail-closed runner/session contracts.
- Control-side surface delivery evidence/projection contract and local proof.
- Conformance fixtures consumed by the plugin repo.

Out of scope unless a later task explicitly opens it:

- Production daemon activation.
- Live/default Discord delivery.
- Gateway, auth, token, secret, provider, model, or profile mutation.
- Plugin implementation/readiness claims.
- KAB bridge execution claims.
- Replacing real participant profiles with role prompts.
- Hidden plugin-to-CLI fallback or localhost/TCP/gateway fallback.
- Commit or push without explicit 주군 approval.

## GJC backend lane rule

For KAS/KAH v0.2 local development in this ATN repo, use the GAJAE GJC delegated-execution lane as the default backend policy:

```text
KAS/KAH phase planning -> GJC delegated execution through KAH wrapper -> candidate design/plan/implementation artifacts
```

KAB remains non-primary unless a later task explicitly selects KAB runtime/session control. Do not use direct `codex app-server` as the default backend lane or as a silent fallback for v0.2 ATN work. Legacy Codex app-server evidence remains historical only and is not current backend-policy authority.

## Review and Kanban evidence

Official color-review evidence must be durable Kanban/team-member evidence:

- 서황 Red: safety, fail-closed behavior, evidence sufficiency, approval boundaries, regressions.
- 종회 Orange: operator/user-visible workflow, value, clarity, handoff and acceptance criteria.
- 만총 Gray: SOT traceability, evidence paths, stale markers, component maps, and handoff records.
- 마초 Blue: final synthesis, acceptance/request-changes/hold decision, and Korean report to 주군.

A reviewer card reaching `done` is not final Blue acceptance. Preserve `조건부 승인`, `REQUEST_CHANGES`, blockers, assumptions, accepted risks, and non-scope exactly. Blue must read the durable verdicts and record synthesis before reporting official acceptance.

## Common verification commands

Use real user home in reusable artifacts and prompts:

```bash
HOME=/Users/draccoon git diff --check
HOME=/Users/draccoon /Users/draccoon/.local/bin/kkachi-agent-helper-toolchain graph validate --file .kkachi-workflow.yaml --json
HOME=/Users/draccoon /Users/draccoon/.local/bin/kkachi-agent-helper-toolchain project doctor --json
HOME=/Users/draccoon /Users/draccoon/.local/bin/kkachi-agent-skills-toolchain doctor --project . --workflow-graph --json
HOME=/Users/draccoon kkachi-agent-tester run test-prepare
HOME=/Users/draccoon kkachi-agent-tester run docs-guardrails
HOME=/Users/draccoon kkachi-agent-tester run check-plugin-contract
HOME=/Users/draccoon make test-prepare
```

For implementation/proof tasks, add focused Go tests and broader gates as scope requires:

```bash
HOME=/Users/draccoon make test
HOME=/Users/draccoon make test-release-acceptance
```

When protocol/contract shape affects plugin compatibility, run the sibling plugin gate when practical:

```bash
cd /Users/draccoon/Workspace/SeventeenthEarth/agent-turn-network/agent-turn-network-plugin
HOME=/Users/draccoon make check-core-contract
```

## Reporting to 주군

User-facing reports to 주군 are Korean. Separate:

- status and scope;
- files changed;
- KAH run/graph evidence;
- commands and outputs;
- official review evidence;
- blockers/risks/non-scope;
- exact next approval needed.

Do not report completion, live readiness, plugin readiness, production readiness, commit, or push without current evidence and explicit authorization for that exact claim.
<!-- PROJECT:LOCAL:END -->
