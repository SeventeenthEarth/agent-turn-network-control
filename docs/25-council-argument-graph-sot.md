# Council Argument Graph SOT

Status: Accepted/completed docs-only SOT for `control/ARGUE-001`. Official KAN review passed with Red card `t_4a2e735f`, Orange card `t_9f4b2b9c`, and Gray card `t_b196d630`; this closeout records Blue synthesis as satisfied for ARGUE-001. This document is the first durable SOT for KAN council discussion-quality work. ARGUE-001 did not start or authorize `control/ARGUE-002`; `control/ARGUE-002` was separately accepted for bounded local static protocol/fixture scope under KAS/KAH run `run-20260615T145822Z-caab064cf550` after Red `t_e2ced3fc`, Orange `t_fd35e83a`, Gray `t_c9e20348`, Blue synthesis `t_ade91c69`, and final gate `evt-001437`. This SOT does not enable live/default/production runtime behavior by itself.

Date: 2026-06-15
Owner: 마초 / `macho` for the bounded KAN control/plugin lanes
Companion repos:

- Control authority: `kkachi-agent-network-control`
- Plugin adapter: `kkachi-agent-network-plugin`

## 1. Purpose

KAN council discussions must be more than mechanically valid turn logs. A successful council must preserve evidence that participants engaged each other's claims: supporting, challenging, refining, extending, questioning, synthesizing, or deliberately opening a new axis.

The failed quality pattern this SOT prevents is:

```text
hand_raise/speaker_selected/speech counts all look correct,
but every speaker only says an independent mini-essay,
with no durable relation to prior claims.
```

KAN therefore models discussion quality as an **argument graph layered over the event sequence**. The event log remains ordered and append-only; the argument graph records semantic links between claims inside that log.

## 2. Authority and boundary

### 2.1 KAN independence

KAN is independent of KAS. KAS does not install, own, or activate KAN control, KAN plugin, KAN bundled operator guidance, or KAN participant profile state.

### 2.2 Control authority

`kkachi-agent-network-control` owns:

- daemon event/state authority;
- protocol shape and conformance fixtures;
- append-time validation and quality-required mode behavior;
- transcript/export/projection semantics;
- moderator policy rules that depend on durable event state.

### 2.3 Plugin authority

`kkachi-agent-network-plugin` owns:

- Hermes participant-agent tool surface;
- plugin schemas and handler validation before command submission;
- participant response framing for `kan_selected_participant_response`;
- visible surface rendering of relation evidence;
- packaged KAN operator guidance bundled inside the plugin package.

The plugin must not become a second lifecycle state authority, must not infer council state from Discord order, and must not hide CLI/daemon fallback behavior.

### 2.4 Profile activation boundary

Actual Hermes profile plugin enablement, participant profile refresh, gateway restart, Discord delivery, or profile-local copying/registering of packaged operator guidance is an explicit activation flow. It is not implied by this SOT and requires separate approval and evidence.

## 3. Design principle

The durable requirement is **meaningful engagement with the prior claim graph**, not mandatory response to the immediately previous turn.

Allowed and desired behavior:

- A turn may support a claim from many turns earlier.
- A turn may challenge one prior claim while extending another.
- A turn may synthesize several earlier claims into a proposed decision frame.
- A turn may open a new axis when the participant states why that axis is necessary.

Rejected behavior in quality-required mode:

- repeated unlinked mini-essays after the opening turn;
- scripted round-robin speeches with pre-fixed focus and no relation to prior claims;
- visible runtime warning/noise presented as participant speech;
- `new_axis` used as a default escape hatch without rationale.

## 4. Core concepts

### 4.1 Claim

A `claim` is a concise assertion or decision-relevant point made by a council participant in a `speech` event.

Minimum shape:

```json
{
  "claim_id": "T03.C1",
  "summary": "Fail-closed validation is required before visible pilot acceptance.",
  "kind": "requirement"
}
```

Rules:

- `claim_id` is stable within the speech payload.
- Preferred local form is `T<turn>.C<n>` for human readability.
- After append, the canonical reference is the pair `(speech.event_id, claim_id)`.
- `summary` must be short enough for transcript rendering and review.
- `kind` is optional in compatibility mode, but if present must be one of: `observation`, `requirement`, `risk`, `decision_frame`, `evidence`, `open_question`, `proposal`.

### 4.2 Stance link

A `stance_link` records how the current speech relates to an earlier speech or claim.

Minimum shape:

```json
{
  "target_event_id": "evt_speech_T02",
  "target_claim_id": "T02.C1",
  "stance": "challenge",
  "rationale": "The cost concern is valid, but it does not justify accepting an unverifiable pilot."
}
```

Rules:

- `target_event_id` is required.
- `target_claim_id` is required when the target speech exposes `claims[]`; it may be omitted only when linking to legacy speech without claim extraction.
- The target must be a previous `speech` event in the same council session.
- Self-links and future links are invalid.
- `rationale` is required in quality-required mode.

Allowed `stance` values:

- `support` — agrees with or reinforces the target claim.
- `challenge` — disputes the target claim or its implication.
- `refine` — narrows, corrects, or qualifies the target claim.
- `extend` — adds a compatible implication or adjacent requirement.
- `synthesize` — combines multiple prior claims into a higher-level frame.
- `question` — asks for clarification or missing evidence about the target.
- `risk_addition` — adds a concrete risk to an existing claim or proposal.
- `decision_frame` — maps prior claims into a decision criterion or acceptance frame.

`new_axis` is not a stance link because it has no prior target. It is represented as `contribution_type: "new_axis"` with `new_axis_reason`.

### 4.3 Legacy `responds_to_event_id` relationship

Existing `speech.payload.responds_to_event_id` is a coarse single-target compatibility pointer. `stance_links[]` is the argument-graph relation authority for new ARGUE-capable clients.

Rules:

- If both fields are present, `responds_to_event_id` must either equal one `stance_links[].target_event_id` or be treated as a legacy display hint with no validation authority.
- New quality-aware validation, transcript/export, and plugin rendering must use `stance_links[]` rather than inferring stance from `responds_to_event_id`.
- Existing readers may continue displaying `responds_to_event_id`, but must not drop `stance_links[]` when re-exporting a speech event.
- `control/ARGUE-002` must publish fixtures for both a legacy-only speech and a dual-field speech so the plugin can preserve backward compatibility without inventing control-owned semantics.

### 4.4 Contribution type

`contribution_type` records the primary role of the current speech in the discussion.

Allowed values:

- `support`
- `challenge`
- `refine`
- `extend`
- `synthesize`
- `question`
- `risk_addition`
- `decision_frame`
- `new_axis`

Rules:

- One primary `contribution_type` is required in quality-required mode.
- A speech with several relation types still chooses the dominant contribution type; detailed multiplicity lives in `stance_links[]`.
- `contribution_type: "synthesize"` requires at least two stance links to prior claims or prior speech events.
- `contribution_type: "new_axis"` requires `new_axis_reason`.

### 4.5 Orphan speech

An orphan speech is a non-opening speech that has neither:

- a valid `stance_links[]` entry to the prior claim graph, nor
- `contribution_type: "new_axis"` with a valid `new_axis_reason`.

In compatibility/default mode, orphan speech is allowed but may be projected as a quality warning. In quality-required mode, orphan speech is rejected after the opening contribution window.

## 5. Protocol extension

### 5.1 `speech.payload`

The `speech` payload gains additive fields:

```json
{
  "turn": 3,
  "speech": "I agree with 주유's traceability point, but challenge 사마의's claim that cost should block the pilot...",
  "claims": [
    {
      "claim_id": "T03.C1",
      "summary": "Fail-closed traceability is a prerequisite for pilot acceptance.",
      "kind": "requirement"
    }
  ],
  "stance_links": [
    {
      "target_event_id": "evt_T01_speech",
      "target_claim_id": "T01.C1",
      "stance": "support",
      "rationale": "Traceability is the right acceptance axis."
    },
    {
      "target_event_id": "evt_T02_speech",
      "target_claim_id": "T02.C1",
      "stance": "challenge",
      "rationale": "Cost risk should shape rollout, not replace evidence requirements."
    }
  ],
  "contribution_type": "challenge",
  "new_axis_reason": null,
  "evidence": []
}
```

Compatibility rule:

- These fields are additive to schema version 1 until a later explicitly approved breaking schema migration.
- Existing readers may ignore the new fields, but must not rewrite or drop them during transcript/export/projection.
- New quality-aware readers must preserve them exactly enough to reconstruct the relation graph.

### 5.2 `hand_raise.payload`

`hand_raise` may declare intended relation before the floor grant:

```json
{
  "intent": "challenge",
  "target_links": [
    {
      "target_event_id": "evt_T02_speech",
      "target_claim_id": "T02.C1",
      "stance": "challenge"
    }
  ],
  "relevance": 5,
  "urgency": 4,
  "evidence_summary": "Cost concern conflicts with fail-closed pilot criteria."
}
```

Rules:

- `target_links[]` is the preferred ARGUE shape because it keeps event/claim/stance pairing unambiguous. `target_event_ids[]` and `target_claim_ids[]`, if retained for compatibility, are deprecated display hints and must not be used as independent parallel arrays for validation.
- `intent` may use either the existing moderator-scoring vocabulary (`risk`, `block`, `rebuttal`, `note`, etc.) or the ARGUE vocabulary. When ARGUE terms are used, map them as follows for existing scoring until `control/ARGUE-003` implements native scoring: `challenge` -> `rebuttal`, `risk_addition` -> `risk`, `question` -> `note`, `support`/`refine`/`extend`/`synthesize`/`decision_frame` -> `note` plus relevance/role scoring.
- Intended targets are advisory until the selected speech is appended.
- The moderator may use intended targets to score unresolved gaps, conflicts, or synthesis opportunities.
- A selected speaker's final `speech.payload.stance_links[]` may differ from the hand raise intent when the participant records a rationale or the discussion changed before the floor grant.

### 5.3 Session-level quality mode

A council session may declare discussion-quality expectations in `session_created.payload.limits.council.discussion_quality`:

```json
{
  "mode": "quality_required",
  "opening_unlinked_turns": 1,
  "require_claims": true,
  "require_stance_links_after_opening": true,
  "allow_new_axis_with_reason": true,
  "max_consecutive_new_axis": 1
}
```

Allowed modes:

- `compatibility` — fields optional; invalid fields fail validation if present, but missing fields do not reject speech.
- `quality_warn` — fields optional; projections flag orphan speech, weak rationale, and repeated new axes.
- `quality_required` — relation fields are required after the configured opening window, and quality defects reject append.

Default mode is `compatibility` until an implementation task explicitly changes the product default.

## 6. Validation policy

### 6.1 Hard validation failures when fields are present

The daemon rejects an append when:

- `claims` is not an array of objects;
- duplicate `claim_id` appears inside one speech;
- `session_created.payload.limits.council.discussion_quality.require_claims` is true and `claims[]` is missing or empty after the configured opening window;
- `stance_links` is not an array of objects;
- `stance` is outside the allowed enum;
- `target_event_id` does not exist;
- target event belongs to another session;
- target event is not earlier than the current speech;
- target event is not a `speech` event;
- `target_claim_id` is provided but absent from the target speech's `claims[]`;
- `rationale` is missing when quality mode requires it;
- `contribution_type` is outside the allowed enum;
- `contribution_type: "synthesize"` has fewer than two valid prior targets;
- `contribution_type: "new_axis"` lacks `new_axis_reason`.

### 6.2 Quality-required failures

When `mode: "quality_required"`, the daemon also rejects:

- orphan speech after `opening_unlinked_turns`;
- repeated `new_axis` beyond `max_consecutive_new_axis` unless moderator policy explicitly records a targeted reason;
- speech text that is only runtime/system noise rather than participant content, when detectable by deterministic guardrails;
- a speech submitted for a turn whose durable `speaker_selected.payload.reason` or `speaker_selected.payload.graph_need` explicitly requested synthesis of named earlier claim ids, when the speech only links to the immediately previous event and omits the named targets.

Do not hard-reject a generic pattern merely because several turns happen to reference the immediately previous speech. Dogfeeding detection must be tied to deterministic event data such as named unresolved claim ids, `graph_need`, or moderator-selected synthesis targets. Otherwise record a `quality_warn` diagnostic or scoring penalty rather than rejecting append.

### 6.3 Compatibility warnings

When the mode is `quality_warn`, the daemon/projection may emit quality diagnostics but must not alter the original speech text or silently add inferred links. Inferred relations are not durable truth.

## 7. Moderator policy

The moderator must grant floor based on claim-graph needs, not scripted round-robin order.

Preferred grant reasons include:

- unresolved challenge needs response;
- prior risk needs evidence;
- two claims need synthesis;
- a required role perspective is absent;
- a new axis is justified by a named missing decision dimension;
- a stale branch should be closed or marked follow-up.

Rules:

- Do not pre-fix a complete `TURN_PLAN` containing all speakers, intents, and conclusions before participants observe each other's speech.
- Do not require every speech to respond to the immediately previous turn.
- Do require the moderator to name the graph gap, conflict, synthesis need, or missing role perspective when using `targeted`, `moderator_direct`, or `role_order` selection.
- `role_order` is permitted for bounded onboarding or attendance-style rounds, but it is not sufficient evidence of discussion quality.
- New topics become follow-up candidates unless the moderator records why `new_axis` is necessary for the locked decision question.

## 8. Transcript and export requirements

Transcript/export/projection must preserve relation evidence in human-readable and machine-checkable form.

Minimum human rendering:

```text
T03 방통 — challenge
↳ support T01.C1 주유: Traceability is the right acceptance axis.
↳ challenge T02.C1 사마의: Cost risk shapes rollout but does not remove evidence requirements.
Claim T03.C1: Fail-closed traceability is required before pilot acceptance.
```

Minimum machine export fields:

- `event_id`
- `turn`
- `speaker`
- `speech`
- `claims[]`
- `stance_links[]`
- `contribution_type`
- quality diagnostics, if any

Renderers must not infer links from Discord replies, timestamps, or message order.

## 9. Plugin requirements derived from this SOT

The plugin implementation must:

- consume control conformance fixtures for argument-graph speech payloads;
- expose schemas that allow `claims[]`, `stance_links[]`, `contribution_type`, and `new_axis_reason`;
- frame selected participant responses so agents produce structured relation evidence;
- fail closed when participant output contains runtime warning prefixes, max-iteration noise, or malformed relation payloads that would become visible speech;
- preserve profile/session/provenance evidence separately from speech text;
- render visible transcript relation summaries without claiming lifecycle authority;
- update packaged operator guidance as a KAN plugin artifact, not as a KAS install claim.

## 10. Conformance fixture requirements

Control must publish fixtures covering at least:

1. valid opening `new_axis` with reason;
2. valid support to a prior claim several turns earlier;
3. valid challenge to one prior claim and support of another in the same speech;
4. valid synthesize with two or more prior targets;
5. valid dual-field speech containing both legacy `responds_to_event_id` and ARGUE `stance_links[]`;
6. valid `hand_raise.payload.target_links[]` pairing event id, claim id, and intended stance;
7. invalid future reference;
8. invalid cross-session reference;
9. invalid unknown `target_claim_id`;
10. invalid `new_axis` without reason;
11. invalid synthesize with fewer than two targets;
12. invalid quality-required missing or empty `claims[]` after the opening window when `require_claims` is true;
13. invalid runtime/system-noise speech payload such as warning prefixes or max-iteration noise when it would become visible speech;
14. quality-required orphan speech after opening;
15. quality-warn orphan speech that is accepted with diagnostics;
16. visible transcript/export preserving relation graph fields.

Plugin must consume these fixtures rather than inventing incompatible control-owned shapes.

## 11. ARGUE implementation DAG

The SOT-backed task DAG is:

```text
control/ARGUE-001  Council argument graph SOT and roadmap/index links
  -> control/ARGUE-002  Protocol shape and conformance fixtures
      -> plugin/ARGUE-001  Plugin schema/client/tool contract updates
  -> control/ARGUE-003  Daemon/CLI validation and moderator scoring hooks
      -> plugin/ARGUE-002  Participant response generation and fail-closed handler behavior
  -> control/ARGUE-004  Transcript/export/projection relation preservation
      -> plugin/ARGUE-003  Visible relation rendering
  -> control/ARGUE-005  Control integration verification gate
      + plugin/ARGUE-004  Packaged operator guidance and pilot harness notes
          -> ARGUE-LIVE-001  Approved live-local quality pilot
```

`ARGUE-LIVE-001` must not run until the control and plugin sides both pass their local gates and actual participant profile activation is separately verified.

`control/ARGUE-002` is now separately accepted for bounded local static protocol shape and conformance fixtures under KAS/KAH run `run-20260615T145822Z-caab064cf550`. ARGUE-001 acceptance did not authorize protocol implementation, fixture publication, daemon/CLI changes, plugin adapter work, or live activation by itself, and ARGUE-002 does not claim runtime validation/scoring, transcript/export rendering, plugin adapter work, or live activation.

## 12. Acceptance criteria for discussion-quality pilot

A future live-local pilot may claim discussion-quality success only when evidence shows:

- actual named participant profiles had KAN plugin/tool visibility before the run;
- daemon/CLI event stream is authoritative and replayable;
- each non-opening speech in quality-required mode has valid relation evidence or justified `new_axis`;
- at least one relation targets a claim more than one turn earlier;
- at least two stance categories appear across the discussion, with one of them being `challenge`, `refine`, or `synthesize`;
- no runtime warning/noise appears as visible participant speech;
- transcript/export and visible projection both show the relation graph;
- final reporting separates mechanical lifecycle pass from discussion-quality pass.

## 13. Non-goals

This SOT does not:

- enable production/live readiness;
- authorize gateway, provider, token, profile, Discord, or daemon runtime mutation;
- require KAS to install or own KAN artifacts;
- force direct reply to the immediately previous turn;
- require automatic natural-language claim extraction in the first implementation slice;
- authorize hidden fallback from plugin to CLI or from visible surface to lifecycle state.

## 14. Open implementation decisions

These are implementation choices for later ARGUE tasks, not blockers for this SOT:

- exact CLI flags for setting `discussion_quality` mode;
- whether claim graph indexes live only in projection or also in a dedicated SQLite table;
- exact diagnostics event type for `quality_warn` mode;
- whether hand-raise target claims influence speaker scoring directly or only as moderator-visible metadata;
- whether a later schema migration makes argument-graph fields mandatory by default.
