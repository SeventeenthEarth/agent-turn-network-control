# Agent Turn Network Control Naming SOT

## Status

Task: `cross-repo/ATN-001`  
Status: completed/docs-only SOT lock after Red/Orange/Gray review and Blue synthesis consensus.  
Scope: control repository naming, cross-repo task sequencing, public rename boundaries, and no-alias policy only.

This document is the control-side Source of Truth for the **Agent Turn Network** public rename. It locks the future public naming contract before code, binary, fixture, package, tool, skill, and broad documentation rewrite tasks start.

This SOT does not rename source files, change binaries, mutate live profiles, start a daemon, contact Discord, change provider/gateway/auth/token settings, publish packages, push commits, or create repository-hosting redirects. Those actions remain later task scope and require their own evidence and approval.

## Canonical public names

| Surface | Canonical name |
| --- | --- |
| Product | Agent Turn Network |
| Short product label | ATN |
| Control repository | `atn-control` |
| Control CLI binary | `atn-control` |
| Control daemon binary | `atn-controld` |
| Go module name before hosted import-path decision | `atn-control` |
| Default user data directory | `~/.atn` |
| XDG data directory name | `agent-turn-network` |
| Primary data-home environment variable | `ATN_HOME` |
| Daemon executable override environment variable | `ATN_CONTROLD_PATH` |
| Local socket file name | `atn-controld.sock` |
| Protocol family label | `atn` |
| Protocol compatibility version | `atn-protocol-v1alpha0` |
| Minimum plugin protocol marker | `atn-protocol-v1alpha0` |

The final hosted import path is intentionally not asserted here because repository hosting and remote rename are admin actions outside this docs-only task. Until that remote is approved, `atn-control` is the local Go module target for implementation tasks.

## Compatibility and alias policy

The public repository must not keep prior product, project, command, tool, package, skill, protocol, environment-variable, or documentation aliases. The rename is a clean public rename, not a compatibility shim.

Required consequences:

- no prior command binary aliases;
- no prior daemon binary aliases;
- no prior data-home environment variable aliases;
- no prior protocol family aliases;
- no prior documentation wording or examples;
- no transitional public docs that spell out earlier product names;
- no code comments, fixtures, schemas, or tests that preserve earlier public labels for compatibility.

Private operator notes may discuss internal history outside the public repositories. Repository content must converge to ATN-only wording by the final guardrail task.

## Control-owned rename boundaries

The control repository owns these rename surfaces:

| Surface | Control target |
| --- | --- |
| CLI command help and examples | `atn-control ...` |
| Daemon process help and examples | `atn-controld ...` |
| Data-home resolution | `ATN_HOME`, XDG `agent-turn-network`, default `~/.atn` |
| Local daemon executable override | `ATN_CONTROLD_PATH` may point ATN control CLI behavior at a local `atn-controld` executable path |
| Socket and service examples | `atn-controld.sock`, `atn-controld` |
| Command transport docs | ATN daemon protocol, no plugin-to-CLI fallback |
| Protocol/conformance fixtures | ATN labels only; `protocol_version` and `min_plugin_protocol_version` target `atn-protocol-v1alpha0` |
| Transcript/export/status examples | ATN labels only |
| Makefile and smoke checks | ATN binary names and ATN contract checks |
| Companion plugin check | validates ATN plugin contract after the plugin rename lands |

The control repository does not own Python package imports, Hermes plugin manifest names, bundled skills, or Hermes tool schemas. Those are plugin repository surfaces defined by the plugin-side ATN SOT.

`ATN_CONTROLD_PATH` is only a local operator override for the `atn-controld` executable path used by ATN control CLI behavior. It is not live activation, package publication, provider/profile/gateway/auth/token mutation, hosted repository rename, or a protocol/plugin surface rename.

## Runtime authority model retained by the rename

The rename must not change runtime authority:

1. The daemon remains the sole event/state authority.
2. The CLI remains the main-agent/operator control plane.
3. Participant agents use the plugin/protocol-client tool surface for observation, typed writes, selected responses, and cursor acknowledgements.
4. Visible surfaces are presentation and evidence pointers only; they are not lifecycle authority.
5. Hidden plugin-to-CLI fallback remains forbidden.
6. Manual/fallback profile text cannot repair selected-runner failure evidence.
7. Live readiness, production activation, Discord delivery, profile/provider/gateway/auth/token mutation, package publication, and hosted repository rename remain separately approved scopes.

## ATN task sequence

ATN uses one five-task cross-repo sequence. The owning repository is part of the task citation when the task is executed.

| Task | Repo | Initial status | Purpose |
| --- | --- | --- | --- |
| ATN-001 | cross-repo | completed/docs-only | Lock control and plugin ATN naming SOT documents, roadmap entries, and clean no-alias policy. Review consensus: Red `t_d43402f0`, Orange `t_6d6bb8e8`, Gray `t_7ebc9e1e`, Blue synthesis `t_8e348f72`. |
| ATN-002 | control | completed/local-docs-proof | Rewrote control public docs, roadmap/index/map surfaces, protocol wording, examples, and operator-facing text to ATN-only wording without binary/code rename. |
| ATN-003 | plugin | completed/local-docs-proof | Rewrote plugin public docs, package/docs metadata, operator guide, bundled skill documentation, and local sibling workspace references to ATN-only wording ahead of ATN-005. |
| ATN-004 | control | completed/local-proof | Renamed control code, Go module, CLI binary, daemon binary, data-home/env/socket/protocol markers, daemon principal, conformance fixtures, tests, scripts, and Makefile surfaces to ATN names with no aliases. |
| ATN-005 | plugin | completed/local-proof | Renamed plugin package/import/manifest/tools/bundled skills to ATN names, updated final no-alias guardrails, and closed cross-repo compatibility proof. |

## Acceptance criteria for ATN-001

ATN-001 is accepted when:

- control and plugin SOT documents define canonical ATN names;
- control and plugin roadmaps record ATN-001 through ATN-005;
- docs index and docs map surfaces point to the ATN SOTs;
- the policy forbids in-repository legacy aliases;
- runtime authority boundaries remain unchanged;
- Red, Orange, Gray, and Blue review agree that later rename implementation tasks have a clear, testable contract.

## Non-scope

ATN-001 does not perform implementation rename work. It does not update every existing stale reference in either repository, because later ATN implementation and guardrail tasks own that full sweep. It also does not claim public release readiness, live readiness, package publication, hosted repository rename, push, or production rollout.
