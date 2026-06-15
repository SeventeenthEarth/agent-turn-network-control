# AGENTS.md — KAN control repo

This file is the local agent contract for `/Users/draccoon/Workspace/SeventeenthEarth/kkachi/kkachi-agent-network-control`.

<!-- KAS:MANAGED:BEGIN core-behavior -->
## KAS-managed baseline behavior

These repo-local instructions preserve the useful baseline guardrails from the
`andrej-karpathy-skills` `CLAUDE.md` lineage and adapt them for KAN control.
KAS-managed instruction content is policy guidance only. It does not authorize
profile mutation, auth/token changes, provider/model/gateway changes, KAH state
writes beyond the approved project workflow, KAB runtime mutation, commits,
pushes, live activation, Discord delivery, or deployment.

Operating principles:

- Think before coding: read the named source of truth, identify constraints,
  state evidence-backed assumptions, and surface real uncertainty instead of
  guessing.
- Simplicity first: prefer the smallest change that satisfies the task; do not
  add speculative features, compatibility layers, abstractions, or fallbacks.
- Surgical changes: touch only files required by the task, preserve unrelated
  project-local text, and do not reformat or refactor adjacent work.
- Goal-driven execution: turn the task into verifiable checks, run focused tests
  or KAH/KAS gates, read the results, and report exact evidence honestly.
- Artifact-first detail: keep chat/console output compact and point to durable
  artifacts when long plans, logs, diffs, reviews, or evidence are needed.

Layer boundaries:

- KAS owns task classification, workflow guidance, prompt policy, templates, and
  evidence expectations. It must not become a second KAH state system or a KAB
  runtime controller.
- KAH owns deterministic project state, schemas, artifacts, events, locks,
  diagnostics, and pass/fail/not_applicable gates. It must not judge prose
  quality, summarize policy, or rewrite instructions.
- KAB owns backend bridge/session/control behavior and retained backend
  execution evidence. It must not decide KAS task policy, silently substitute
  lanes, or mutate auth/token/provider/model/gateway settings without explicit
  authority.
<!-- KAS:MANAGED:END core-behavior -->

<!-- PROJECT:LOCAL:BEGIN -->

## Identity and scope

- Project: `kkachi-agent-network-control`.
- Lane: KAN control repo only. Do not claim KAB, KAH, KAS, KAN plugin, or whole Kkachi authority from this file.
- Blue command owner for this lane: 마초 / `macho`.
- Companion plugin repo: `/Users/draccoon/Workspace/SeventeenthEarth/kkachi/kkachi-agent-network-plugin`.
- Team/channel ownership after KAN cutover: 마초 Blue, 서황 Red, 종회 Orange, 만총 Gray.

## KAS/KAH operating baseline

Use the profile-local KAS skill suite installed under:

```text
/Users/draccoon/.hermes/profiles/macho/skills/kan-control/
```

The suite is a KAN control semantic tailoring of the KAS project suite. Load phase skills by their `kan-control-*` names when a task enters KAS mode, for example:

- `kan-control-orchestrate`
- `kan-control-task-contract`
- `kan-control-plan`
- `kan-control-phase-state`
- `kan-control-implement`
- `kan-control-enhance-test`
- `kan-control-optimize`
- `kan-control-docs-update`
- `kan-control-review`
- `kan-control-final-verify`

Observed local tool baseline when this file was written:

```text
kkachi-agent-helper 0.1.9
kkachi-agent-skills 0.1.3
```

Verify before relying on it:

```bash
kkachi-agent-helper --version
kkachi-agent-skills --version
kkachi-agent-helper project doctor --json
kkachi-agent-helper graph validate --json
kkachi-agent-skills doctor --profile macho --project-suite --project kan-control --json
kkachi-agent-skills doctor --repo /Users/draccoon/Workspace/SeventeenthEarth/kkachi/kkachi-hermes-skills --project /Users/draccoon/Workspace/SeventeenthEarth/kkachi/kkachi-agent-network-control --workflow-graph --json
```

KAS project-suite doctor may report `project_tailoring_checksum_drift` warnings for the `kan-control-*` skills. That warning is expected for this profile-local semantic tailoring when the command still returns `ok: true`. Treat checksum mismatch errors as blockers.

## Authority order

When instructions conflict, use this order:

1. 주군's explicit current instruction.
2. Team registry and KAN cutover authority from `/Users/draccoon/Workspace/Hermes/17thHermes/01_references/team/team-agent-registry.yaml`.
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
4. `.kkachi-workflow.yaml` after it passes `kkachi-agent-helper graph validate --json`.
5. KAS promotion candidates such as `/Users/draccoon/Workspace/Hermes/17thHermes/40_outputs/projects/kkachi/2026-06-14-kas-policy-promotion-candidates.md`; these are useful guidance but are not applied KAS source until promoted.

Do not infer KAS/KAH/KAB policy from memory alone. Resolve the relevant source or evidence file before architecture, readiness, activation, review, or final claims.

## Task classification

Before KAS-mode work, classify the task and record the reason in the task contract/run artifacts:

- `development`: code, tests, build behavior, architecture, process contract, executable contract, or behavior-changing work. Use the full development spine.
- `docs_only`: durable docs/SOT/roadmap/contract edits with no executable behavior change. Use docs/contract verification and explicit skipped-phase reasons.
- `research_evidence`: read-only investigation, current-state evidence, option comparison, or log/source inspection.
- `simple_command_report`: bounded command/status check with no durable project change; keep outside KAS unless 주군 explicitly keeps it in KAS.
- `bootstrap_config`: repo/KAH project/profile/manifest/tooling/test-bed setup. Require explicit approval for auth, secret, gateway, provider, profile, daemon activation, or live runtime mutation.
- `collaboration_review`: durable review/risk/team-feedback routing; use Kanban for official team-member review.

## Current workflow graph

The project graph is `.kkachi-workflow.yaml`. Validate it before use.

Development-class spine:

```text
intake -> sot -> roadmap -> task-classification -> plan -> vet -> ask -> implement -> enhance-test -> ai-slop-cleaner -> optimize -> docs-update -> verify -> color-review -> color-adjust -> octo-review -> octo-adjudication -> post-octo-adjust -> final
```

Important semantics:

- Plan-stage development defaults to Blue vet plus official Red review.
- `delegate_task`, temporary helper agents, and ad-hoc subagents are not official review evidence.
- Implementation/final acceptance normally requires official KAN Red/Orange/Gray Kanban review plus dependent Blue synthesis.
- Color review is a convergence loop: valid requested changes return to the selected implementer lane, verification reruns, and focused re-review continues until no valid change requests remain.
- GLM Octo review is one official feedback event for implementation tasks unless explicitly waived; Octo findings are adjudicated by Blue plus color reviewers before valid findings are routed for adjustment.
- Non-development tasks must preserve explicit skipped-phase reasons instead of silently inheriting the full spine.

## KAN control repo boundaries

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

## Codex/KAB lane rule

When a KAS/KAN task mentions or requires `codex app-server`, use the actual local Codex Python SDK stdio app-server/session flow:

```text
openai_codex.Codex / CodexConfig -> codex app-server --listen stdio://
```

Do not use `codex exec` as a substitute. If the SDK/app-server path is unavailable, report the blocker instead of falling back silently. Direct Codex SDK/app-server evidence is Stage 1 direct evidence and is not a KAB Codex execution claim.

## Review and Kanban evidence

Official KAN color-review evidence must be durable Kanban/team-member evidence:

- 서황 Red: safety, fail-closed behavior, evidence sufficiency, approval boundaries, regressions.
- 종회 Orange: operator/user-visible workflow, value, clarity, handoff and acceptance criteria.
- 만총 Gray: SOT traceability, evidence paths, stale markers, component maps, and handoff records.
- 마초 Blue: final synthesis, acceptance/request-changes/hold decision, and Korean report to 주군.

A reviewer card reaching `done` is not final Blue acceptance. Preserve `조건부 승인`, `REQUEST_CHANGES`, blockers, assumptions, accepted risks, and non-scope exactly. Blue must read the durable verdicts and record synthesis before reporting official acceptance.

## Common verification commands

Use real user home in reusable artifacts and prompts:

```bash
HOME=/Users/draccoon git diff --check
HOME=/Users/draccoon make docs-guardrails
HOME=/Users/draccoon make check-plugin-contract
HOME=/Users/draccoon make test-prepare
```

For implementation/proof tasks, add focused Go tests and broader gates as scope requires:

```bash
HOME=/Users/draccoon make test
HOME=/Users/draccoon make test-release-acceptance
```

When protocol/contract shape affects plugin compatibility, run the sibling plugin gate when practical:

```bash
cd /Users/draccoon/Workspace/SeventeenthEarth/kkachi/kkachi-agent-network-plugin
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
