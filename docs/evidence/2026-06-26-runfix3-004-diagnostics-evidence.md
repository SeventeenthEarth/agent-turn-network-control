# RUNFIX3-004 diagnostics evidence

## Scope
- Task: `control/RUNFIX3-004`
- Repo: `agent-turn-network-control`
- Date: 2026-06-26
- Objective: provide a durable control-side evidence path for the implemented RUNFIX3 diagnostics/enforcement slice while it remains review-pending.

## Evidence summary
- Current control status surfaces remain `implementation_complete/review_pending`.
- The control slice owns lifecycle accounting, selected-runner and delivery-evidence requirements, exact thread binding diagnostics, missing-stage diagnostics, and fail-closed unresolved-closeout behavior.
- This artifact is a durable evidence pointer for verification status; it is not a RUNFIX3-wide completion claim.

## Canonical status mirrors
- `docs/roadmap.md`
- `docs/todo/implementation-decomposition.md`
- `docs/spec/live-transport-control-sot.md`


## Verification
- `HOME=/Users/draccoon git diff --check` — pass
- `HOME=/Users/draccoon make docs-guardrails` — pass
- `HOME=/Users/draccoon make check-plugin-contract` — pass
- `HOME=/Users/draccoon make test-prepare` — pass

## Accepted focused baseline
The 2026-06-26 review feedback accepted the following focused baseline as the RUNFIX3-004 verification contract:
- `HOME=/Users/draccoon git diff --check`
- `HOME=/Users/draccoon make docs-guardrails`
- `HOME=/Users/draccoon make check-plugin-contract`
- `HOME=/Users/draccoon make test-prepare`
- `HOME=/Users/draccoon go test ./internal/storage -run 'RUNFIX3004|RUNFIX2CouncilDiscussionLifecycle|IntegrationCouncilLifecycleFailClosedGuardsAndProjection|SelectedRunner|CommandIDRetryDeduplicatesLegacy' -count=1`
- `HOME=/Users/draccoon make test`

## Acceptance notes
- Focused Red/Orange/Gray re-review and Blue synthesis remain required.
- The final cross-repo RUNFIX3 closeout artifact and 주군 approval remain separate blockers for RUNFIX3-wide completion.
