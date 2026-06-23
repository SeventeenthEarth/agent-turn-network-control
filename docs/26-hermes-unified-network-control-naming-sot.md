# Hermes Unified Network Control Naming SOT

## Status

Task: `control/HUN-001`  
Status: completed/docs-only SOT lock after local Blue implementation and color-review synthesis.  
Scope: control repository naming, roadmap sequencing, and public rename boundaries only.

This document is the control-side Source of Truth for the Hermes Unified Network rename. It locks the future public naming contract before code, binary, fixture, package, and documentation rewrite tasks start.

This SOT does not rename files, change binaries, mutate live profiles, start a daemon, contact Discord, change provider/gateway/auth/token settings, publish packages, or create repository-hosting redirects. Those actions remain later task scope and require their own evidence.

## Canonical public names

| Surface | Canonical name |
| --- | --- |
| Product | Hermes Unified Network |
| Short product label | HUN |
| Control repository | `hun-control` |
| Control CLI binary | `hun` |
| Control daemon binary | `hund` |
| Go module name before hosted import-path decision | `hun-control` |
| Default user data directory | `~/.hun` |
| XDG data directory name | `hermes-unified-network` |
| Primary data-home environment variable | `HUN_HOME` |
| Daemon executable override environment variable | `HUND_PATH` |
| Local socket file name | `hund.sock` |
| Protocol family label | `hun` |
| Protocol compatibility version | `hun-protocol-v1alpha0` |
| Minimum plugin protocol marker | `hun-protocol-v1alpha0` |

The final hosted import path is intentionally not asserted here because repository hosting and remote rename are admin actions outside a docs-only PR. Until that remote is approved, `hun-control` is the local Go module target for implementation tasks.

## Compatibility and alias policy

The public repository must not keep legacy product, project, command, tool, package, skill, or documentation aliases. The rename is a clean public rename, not a compatibility shim.

Required consequences:

- no old command binary aliases;
- no old daemon binary aliases;
- no old data-home environment variable aliases;
- no old protocol family aliases;
- no old documentation wording or examples;
- no transitional public docs that spell out legacy project names;
- no code comments, fixtures, schemas, or tests that preserve old public labels for compatibility.

Private operator notes may discuss internal history outside this repository. Repository content must converge to HUN-only wording by the final guardrail tasks.

## Control-owned rename boundaries

The control repository owns these rename surfaces:

| Surface | Control target |
| --- | --- |
| CLI command help and examples | `hun ...` |
| Daemon process help and examples | `hund ...` |
| Data-home resolution | `HUN_HOME`, XDG `hermes-unified-network`, default `~/.hun` |
| Local daemon executable override | `HUND_PATH` may point HUN control CLI behavior at a local `hund` executable path |
| Socket and service examples | `hund.sock`, `hund` |
| Command transport docs | HUN daemon protocol, no plugin-to-CLI fallback |
| Protocol/conformance fixtures | HUN labels only; `protocol_version` and `min_plugin_protocol_version` target `hun-protocol-v1alpha0` |
| Transcript/export/status examples | HUN labels only |
| Makefile and smoke checks | HUN binary names and HUN contract checks |
| Companion plugin check | validates HUN plugin contract after the plugin rename lands |

The control repository does not own Python package imports, Hermes plugin manifest names, bundled skills, or Hermes tool schemas. Those are plugin repository surfaces defined by the plugin-side HUN SOT.

`HUND_PATH` is only a local operator override for the `hund` executable path used by HUN control CLI behavior. It is not live activation, package publication, provider/profile/gateway/auth/token mutation, hosted repository rename, or a protocol/plugin surface rename.

## Runtime authority model retained by the rename

The rename must not change runtime authority:

1. The daemon remains the sole event/state authority.
2. The CLI remains the main-agent/operator control plane.
3. Participant agents use the plugin/protocol-client tool surface for observation, typed writes, selected responses, and cursor acknowledgements.
4. Visible surfaces are presentation and evidence pointers only; they are not lifecycle authority.
5. Hidden plugin-to-CLI fallback remains forbidden.
6. Manual/fallback profile text cannot repair selected-runner failure evidence.
7. Live readiness, production activation, Discord delivery, profile/provider/gateway/auth/token mutation, package publication, and hosted repository rename remain separately approved scopes.

## Control task sequence

| Task | Repo | Status | Purpose |
| --- | --- | --- | --- |
| HUN-001 | control | completed/docs-only | Lock this control naming SOT and control roadmap entries. |
| HUN-003 | control | completed/local-proof | Rename Go module, CLI binary, daemon binary, help text, Makefile surfaces, and control command examples. Local proof passed after MAR/second color review and post-fix verification: MAR coverage PASS, Blue MAR `PASS_WITH_FINDINGS_HANDLED`, post-fix verification refresh pass, and second color review `final_gate_may_proceed=true`; live/runtime/package/plugin/protocol/commit/push readiness remains out of scope. |
| HUN-005 | control | completed/local-proof | Renamed protocol docs, conformance manifests, fixtures, schemas, CLI tests, and plugin-contract checks to `hun-protocol-v1alpha0` with no legacy alias/fallback. Sibling plugin protocol marker sync completed in plugin repo-local companion run `run-20260622T003936Z-0d6e8b285d41`; runtime/live/package/commit/push readiness remains out of scope. |
| HUN-007 | control | completed/local-proof | Local control run `run-20260622T114551Z-2120517080cb` hardens selected-runner proof and adapter identity under HUN naming: runner-tagged speech no longer counts as runner success without durable `runner_invocation_succeeded`, and unsupported selected-speaker adapters fail closed before surfaced grant/runner/speech events. Review/MAR/second-color/final gate, run close, and local commit are complete; live/runtime/Discord/profile/provider/gateway/auth/token/package readiness and push remain out of scope. Companion plugin docs/10 selected-runner taxonomy stale wording remains a plugin-owned follow-up caveat. |
| HUN-011 | control | completed/local-docs-proof | Scrubbed control public README/docs/examples and docs-map status text for HUN public names, with remaining legacy/internal labels marked as historical/provenance or safety-boundary references. Local docs proof passed on 2026-06-23; HUN-012 guardrails, HUN-014 compatibility, live/runtime/package/hosted-rename/push readiness, Discord delivery, and profile/provider/gateway/auth/token mutation remain out of scope. |
| HUN-012 | control | completed/local-guardrail-proof | Added control-side fail-closed forbidden-term guardrails with occurrence-specific accepted-hit metadata, focused guardrail tests wired into `test-prepare`, and Python cache hygiene for repeated local test runs. Local proof passed on 2026-06-23; plugin/HUN-013, HUN-014 final compatibility, live/runtime/Discord/package/profile/provider/gateway/auth/token mutation, push, and hosted rename remain out of scope. |
| HUN-014 | cross-repo | planned | Final cross-repo HUN compatibility, stale-reference scan, and release-readiness sync. |

Plugin-owned tasks are recorded in the plugin roadmap. Cross-repo references must use repo-qualified task names when needed, for example `control/HUN-001` and `plugin/HUN-002`.

## Acceptance criteria for HUN-001

HUN-001 is accepted when:

- this SOT exists and defines canonical control names;
- the control roadmap records the HUN sequence;
- docs index and docs map point to this SOT;
- the policy forbids in-repo legacy aliases;
- runtime authority boundaries remain unchanged;
- Red, Orange, Gray, and Blue review agree that later rename implementation tasks have a clear, testable contract.

## Non-scope

HUN-001 does not perform implementation rename work. It does not update every existing old reference in the repository, because later HUN implementation and guardrail tasks own that full sweep. It also does not claim public release readiness.
