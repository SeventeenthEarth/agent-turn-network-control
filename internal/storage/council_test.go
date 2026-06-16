package storage

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/registry"

	_ "modernc.org/sqlite"
)

func TestIntegrationCouncilLifecycleFailClosedGuardsAndProjection(t *testing.T) {
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, results, dedup, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        "sess_council_lifecycle",
			Title:     "Discuss COUNC",
			Moderator: "agent-mod",
			Surface:   &Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "1507515847227215932"},
			TurnMode:  "role_order",
			EventID:   "evt_council_created",
			CommandID: "cmd_council_new",
			Limits:    Limits{MaxConsensusRounds: 2},
		},
		Members: []string{"agent-1", "agent-2"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	if dedup || len(results) != 1 {
		t.Fatalf("expected one fresh create result, dedup=%v results=%#v", dedup, results)
	}
	if got := strings.Join(metadata.Participants, ","); got != "agent-mod,agent-1,agent-2" {
		t.Fatalf("participants order = %q", got)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	created := eventByIDForTest(t, sessionDir, metadata, "evt_council_created")
	assertCouncilStringSlice(t, created.To, []string{"agent-1", "agent-2", "agent-mod"})
	createdParticipants, ok := payloadStringSlice(created.Payload, "participants")
	if !ok {
		t.Fatalf("session_created payload participants missing: %#v", created.Payload)
	}
	if got := strings.Join(createdParticipants, ","); got != "agent-mod,agent-1,agent-2" {
		t.Fatalf("session_created payload participants order = %q", got)
	}

	before := eventCountForTest(t, sessionDir, metadata)
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "prepare", Actor: "agent-mod", CommandID: "cmd_prepare_too_early", Payload: map[string]any{"timeout_sec": 600}, Now: fixedRuntime().Now().Add(time.Second)}); err == nil {
		t.Fatalf("discord-thread prepare without attendance and agenda must fail closed")
	}
	if after := eventCountForTest(t, sessionDir, metadata); after != before {
		t.Fatalf("failed prepare appended event: before=%d after=%d", before, after)
	}

	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "request-attendance", Actor: "agent-mod", CommandID: "cmd_attendance", Payload: map[string]any{"timeout_sec": 300}, Now: fixedRuntime().Now().Add(2 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "attend", Actor: "agent-1", CommandID: "cmd_attend_1", Payload: map[string]any{"status": "present", "summary": "ready"}, Now: fixedRuntime().Now().Add(3 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "attend", Actor: "agent-2", CommandID: "cmd_attend_2", Payload: map[string]any{"status": "partial", "summary": "partial"}, Now: fixedRuntime().Now().Add(4 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "lock-agenda", Actor: "agent-mod", CommandID: "cmd_agenda", Payload: map[string]any{"decision_question": "What should ship?", "max_rounds": 2}, Now: fixedRuntime().Now().Add(5 * time.Second)})
	prepare := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "prepare", Actor: "agent-mod", CommandID: "cmd_prepare", Payload: map[string]any{"timeout_sec": 600}, Now: fixedRuntime().Now().Add(6 * time.Second)})
	prepared := eventByIDForTest(t, sessionDir, metadata, prepare.EventID)
	if prepared.Phase != "preparation" || prepared.Type != "preparation_requested" {
		t.Fatalf("prepare event = %s/%s", prepared.Type, prepared.Phase)
	}
	assertCouncilStringSlice(t, prepared.To, []string{"agent-1", "agent-2"})

	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "ready", Actor: "agent-1", CommandID: "cmd_ready_1", Payload: map[string]any{"summary": "ready"}, Now: fixedRuntime().Now().Add(7 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "prepared-partial", Actor: "agent-2", CommandID: "cmd_partial_2", Payload: map[string]any{"reason": "timeout", "summary": "partial"}, Now: fixedRuntime().Now().Add(8 * time.Second)})
	poll := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_poll", Payload: map[string]any{"research_timeout_sec": 600}, Now: fixedRuntime().Now().Add(9 * time.Second)})
	pollEvent := eventByIDForTest(t, sessionDir, metadata, poll.EventID)
	if pollEvent.Turn == nil || *pollEvent.Turn != 1 || anyInt(pollEvent.Payload, "turn") != 1 {
		t.Fatalf("poll must derive turn 1, got turn=%v payload=%#v", pollEvent.Turn, pollEvent.Payload)
	}
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_raise_1", Payload: map[string]any{"turn": 1, "intent": "risk", "relevance": 5, "urgency": 4, "reason": "important"}, Now: fixedRuntime().Now().Add(10 * time.Second)})
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_invalid_selection_mode", Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "invalid_mode", "reason": "targeted answer"}, Now: fixedRuntime().Now().Add(11 * time.Second)}); err == nil {
		t.Fatalf("unsupported selection_mode must fail closed")
	} else {
		assertStorageIssue(t, err, CategoryInvalidEnvelope)
	}
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_bad_grant", Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(11 * time.Second)}); err == nil {
		t.Fatalf("grant deviating from session turn_mode must require reason")
	}
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_grant", Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct", "reason": "targeted answer"}, Now: fixedRuntime().Now().Add(12 * time.Second)})
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_bad_speech", Payload: map[string]any{"turn": 1, "speech": "not granted"}, Now: fixedRuntime().Now().Add(13 * time.Second)}); err == nil {
		t.Fatalf("speech without grant must fail closed")
	}
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-1", CommandID: "cmd_speech", Payload: map[string]any{"turn": 1, "speech": "I support this."}, Now: fixedRuntime().Now().Add(14 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "intervene", Actor: "agent-mod", CommandID: "cmd_intervene", Payload: map[string]any{"member": "agent-1", "reason": "topic_drift", "message": "Stay scoped."}, Now: fixedRuntime().Now().Add(15 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_propose", Payload: map[string]any{"draft": "Ship the slice."}, Now: fixedRuntime().Now().Add(16 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "request-vote", Actor: "agent-mod", CommandID: "cmd_vote_request", Payload: map[string]any{"draft_version": 1, "timeout_sec": 600}, Now: fixedRuntime().Now().Add(17 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-1", CommandID: "cmd_vote_1", Payload: map[string]any{"draft_version": 1, "vote": "approve", "reason": "ok"}, Now: fixedRuntime().Now().Add(18 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-2", CommandID: "cmd_vote_2", Payload: map[string]any{"draft_version": 1, "vote": "approve_with_conditions", "reason": "ok", "required_change": "mention tests"}, Now: fixedRuntime().Now().Add(19 * time.Second)})
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-2", CommandID: "cmd_vote_2_duplicate", Payload: map[string]any{"draft_version": 1, "vote": "approve", "reason": "changed"}, Now: fixedRuntime().Now().Add(20 * time.Second)}); err == nil {
		t.Fatalf("second vote by same member in same round must fail closed")
	}
	finalize := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "finalize", Actor: "agent-mod", CommandID: "cmd_finalize", Payload: map[string]any{"final_summary": "Consensus reached."}, Now: fixedRuntime().Now().Add(21 * time.Second)})
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_poll_after_final", Payload: map[string]any{"research_timeout_sec": 10}, Now: fixedRuntime().Now().Add(22 * time.Second)}); err == nil {
		t.Fatalf("terminal council mutation must fail closed")
	}
	finalEvent := eventByIDForTest(t, sessionDir, metadata, finalize.EventID)
	if finalEvent.Phase != "finalized" || finalEvent.Type != "council_finalized" {
		t.Fatalf("final event = %s/%s", finalEvent.Type, finalEvent.Phase)
	}

	report, err := RebuildProjection(dataHome, ProjectionOptions{Runtime: fixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection: %v", err)
	}
	db := openProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	if got := scalarText(t, db, `SELECT phase FROM sessions WHERE id = 'sess_council_lifecycle'`); got != "finalized" {
		t.Fatalf("projected phase = %q", got)
	}
	if got := rowCountWhere(t, db, "council_attendance_projection", "session_id = 'sess_council_lifecycle' AND attendance_status IN ('present','partial')"); got != 2 {
		t.Fatalf("projected attendance rows = %d", got)
	}
	if got := rowCountWhere(t, db, "council_votes", "session_id = 'sess_council_lifecycle'"); got != 2 {
		t.Fatalf("projected votes = %d", got)
	}
	if active, err := FindActiveSession(dataHome, fixedRuntime()); err != nil || active != nil {
		t.Fatalf("finalized council must release active lock, active=%#v err=%v", active, err)
	}
}

func TestUnitCouncilCreationRejectsModeratorMemberCollisionsAndReservedPrincipals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		moderator string
		members   []string
	}{
		{name: "moderator collision", moderator: "agent-mod", members: []string{"agent-mod", "agent-1"}},
		{name: "duplicate member after trim", moderator: "agent-mod", members: []string{"agent-1", " agent-1 "}},
		{name: "reserved moderator", moderator: "user", members: []string{"agent-1"}},
		{name: "reserved member", moderator: "agent-mod", members: []string{"kkachi-agent-networkd"}},
		{name: "unknown member", moderator: "agent-mod", members: []string{"agent-unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome, loaded := loadedCouncilRegistry(t)
			_, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{Session: SessionSpec{ID: "sess_council_validation", Title: "validate", Moderator: tc.moderator, EventID: "evt_validate", CommandID: "cmd_validate"}, Members: tc.members, Now: fixedRuntime().Now()}, fixedRuntime())
			if err == nil {
				t.Fatalf("CreateCouncil should reject %s", tc.name)
			}
			assertStorageIssue(t, err, CategoryPrincipalInvalid)
		})
	}
}

func TestUnitCouncilLinkedAuthorityFinalizeRequiresEvidence(t *testing.T) {
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{Session: SessionSpec{ID: "sess_council_linked", Title: "linked", Moderator: "agent-mod", LinkedAuthority: &LinkedAuthority{KanbanCardID: "t_card"}, EventID: "evt_linked_created", CommandID: "cmd_linked_new"}, Members: []string{"agent-1"}, Now: fixedRuntime().Now()}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendRawEventForTest(t, sessionDir, metadata, "evt_linked_prep", "cmd_linked_prep", "preparation_requested", "preparation", "agent-mod", []string{"agent-1"}, map[string]any{"timeout_sec": 1}, time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_linked_poll", "cmd_linked_poll", "hand_raise_requested", "discussion", "agent-mod", []string{"agent-1"}, map[string]any{"turn": 1}, 2*time.Second)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_linked_propose", Payload: map[string]any{"draft": "draft"}, Now: fixedRuntime().Now().Add(3 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "request-vote", Actor: "agent-mod", CommandID: "cmd_linked_vote_request", Payload: map[string]any{"draft_version": 1}, Now: fixedRuntime().Now().Add(4 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-1", CommandID: "cmd_linked_vote", Payload: map[string]any{"draft_version": 1, "vote": "approve", "reason": "ok"}, Now: fixedRuntime().Now().Add(5 * time.Second)})
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "finalize", Actor: "agent-mod", CommandID: "cmd_linked_bad_finalize", Payload: map[string]any{"final_summary": "done"}, Now: fixedRuntime().Now().Add(6 * time.Second)}); err == nil {
		t.Fatalf("linked authority finalize without result evidence must fail closed")
	}
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "finalize", Actor: "agent-mod", CommandID: "cmd_linked_finalize", Payload: map[string]any{"final_summary": "done", "linked_authority_result": map[string]any{"status": "posted", "kanban_comment_id": "kc_123"}}, Now: fixedRuntime().Now().Add(7 * time.Second)}); err != nil {
		t.Fatalf("linked authority finalize with posted evidence: %v", err)
	}
}

func TestUnitCouncilArgumentGraphRejectsMalformedPresentFields(t *testing.T) {
	for _, tc := range []struct {
		name    string
		action  string
		payload map[string]any
	}{
		{name: "claims not array", action: "speak", payload: map[string]any{"claims": "bad"}},
		{name: "claims not objects", action: "speak", payload: map[string]any{"claims": []any{"bad"}}},
		{name: "duplicate claim id", action: "speak", payload: map[string]any{"claims": []any{
			map[string]any{"claim_id": "T1.C1", "summary": "one"},
			map[string]any{"claim_id": "T1.C1", "summary": "two"},
		}}},
		{name: "stance links not array", action: "speak", payload: map[string]any{"stance_links": "bad"}},
		{name: "bad stance", action: "speak", payload: map[string]any{"stance_links": []any{map[string]any{"target_event_id": "evt_speech_cmd_argue_target_speech", "target_claim_id": "T1.C1", "stance": "agree"}}}},
		{name: "missing target event", action: "speak", payload: map[string]any{"stance_links": []any{map[string]any{"target_event_id": "evt_missing", "target_claim_id": "T1.C1", "stance": "support"}}}},
		{name: "non speech target", action: "speak", payload: map[string]any{"stance_links": []any{map[string]any{"target_event_id": "evt_argue_target_poll", "stance": "support"}}}},
		{name: "unknown target claim", action: "speak", payload: map[string]any{"stance_links": []any{map[string]any{"target_event_id": "evt_speech_cmd_argue_target_speech", "target_claim_id": "T1.C404", "stance": "support"}}}},
		{name: "bad contribution", action: "speak", payload: map[string]any{"contribution_type": "essay"}},
		{name: "synthesize one target", action: "speak", payload: map[string]any{"contribution_type": "synthesize", "stance_links": []any{map[string]any{"target_event_id": "evt_speech_cmd_argue_target_speech", "target_claim_id": "T1.C1", "stance": "synthesize"}}}},
		{name: "new axis no reason", action: "speak", payload: map[string]any{"contribution_type": "new_axis"}},
		{name: "target links not array", action: "hand-raise", payload: map[string]any{"target_links": "bad"}},
		{name: "target links bad target claim", action: "hand-raise", payload: map[string]any{"target_links": []any{map[string]any{"target_event_id": "evt_speech_cmd_argue_target_speech", "target_claim_id": "T1.C404", "stance": "challenge"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_malformed_"+strings.ReplaceAll(tc.name, " ", "_"), Limits{})
			appendArgumentGraphTargetForTest(t, sessionDir, metadata)
			if tc.action == "hand-raise" {
				payload := clonePayload(tc.payload)
				payload["turn"] = 2
				if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-2", CommandID: "cmd_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: payload, Now: fixedRuntime().Now().Add(20 * time.Second)}); err == nil {
					t.Fatalf("%s must fail closed", tc.name)
				} else {
					assertStorageIssue(t, err, CategoryInvalidEnvelope)
				}
				return
			}
			appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_grant_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: map[string]any{"turn": 2, "member": "agent-2", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(21 * time.Second)})
			payload := clonePayload(tc.payload)
			payload["turn"] = 2
			payload["speech"] = "candidate speech"
			if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_speak_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: payload, Now: fixedRuntime().Now().Add(22 * time.Second)}); err == nil {
				t.Fatalf("%s must fail closed", tc.name)
			} else {
				assertStorageIssue(t, err, CategoryInvalidEnvelope)
			}
		})
	}
}

func TestUnitCouncilArgumentGraphQualityRequiredRejectsDefects(t *testing.T) {
	qualityRequired := Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{
		Mode:                           "quality_required",
		OpeningUnlinkedTurns:           1,
		RequireClaims:                  true,
		RequireStanceLinksAfterOpening: true,
		AllowNewAxisWithReason:         true,
		MaxConsecutiveNewAxis:          1,
	}}}
	for _, tc := range []struct {
		name          string
		payload       map[string]any
		targetedGrant bool
	}{
		{name: "orphan after opening", payload: map[string]any{"claims": []any{map[string]any{"claim_id": "T2.C1", "summary": "orphan"}}}},
		{name: "missing claims and stance links", payload: map[string]any{}},
		{name: "runtime noise", payload: map[string]any{"speech": "WARNING: max iteration reached", "claims": []any{map[string]any{"claim_id": "T2.C1", "summary": "noise"}}, "stance_links": []any{map[string]any{"target_event_id": "evt_speech_cmd_argue_target_speech", "target_claim_id": "T1.C1", "stance": "support", "rationale": "connects"}}}},
		{name: "graph need omitted", targetedGrant: true, payload: map[string]any{"claims": []any{map[string]any{"claim_id": "T2.C1", "summary": "partial"}}, "stance_links": []any{map[string]any{"target_event_id": "evt_speech_cmd_argue_target_speech", "target_claim_id": "T1.C1", "stance": "synthesize", "rationale": "only one target"}}, "contribution_type": "support"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_required_"+strings.ReplaceAll(tc.name, " ", "_"), qualityRequired)
			appendArgumentGraphTargetForTest(t, sessionDir, metadata)
			grantPayload := map[string]any{"turn": 2, "member": "agent-2", "selection_mode": "moderator_direct"}
			if tc.targetedGrant {
				grantPayload["graph_need"] = map[string]any{"type": "synthesis", "target_links": []any{
					map[string]any{"target_event_id": "evt_speech_cmd_argue_target_speech", "target_claim_id": "T1.C1"},
					map[string]any{"target_event_id": "evt_argue_second_target_speech", "target_claim_id": "T1.C2"},
				}}
				appendRawEventForTest(t, sessionDir, metadata, "evt_argue_second_target_speech", "cmd_argue_second_target_speech", "speech", "discussion", "agent-2", []string{"agent-mod", "agent-1"}, map[string]any{"turn": 1, "speech": "Second target", "claims": []any{map[string]any{"claim_id": "T1.C2", "summary": "second"}}}, 12*time.Second)
			}
			appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_required_grant_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: grantPayload, Now: fixedRuntime().Now().Add(21 * time.Second)})
			payload := clonePayload(tc.payload)
			payload["turn"] = 2
			if _, ok := payload["speech"]; !ok {
				payload["speech"] = "candidate speech"
			}
			if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_required_speak_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: payload, Now: fixedRuntime().Now().Add(22 * time.Second)}); err == nil {
				t.Fatalf("%s must fail closed", tc.name)
			} else {
				assertStorageIssue(t, err, CategoryInvalidEnvelope)
			}
		})
	}
	t.Run("repeated new axis", func(t *testing.T) {
		sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_required_repeated_new_axis", qualityRequired)
		appendArgumentGraphTargetForTest(t, sessionDir, metadata)
		appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_required_new_axis_grant", Payload: map[string]any{"turn": 2, "member": "agent-2", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(21 * time.Second)})
		if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_required_new_axis_speak", Payload: map[string]any{"turn": 2, "speech": "another new axis", "claims": []any{map[string]any{"claim_id": "T2.C1", "summary": "new"}}, "contribution_type": "new_axis", "new_axis_reason": "another axis"}, Now: fixedRuntime().Now().Add(22 * time.Second)}); err == nil {
			t.Fatalf("repeated new_axis must fail closed")
		} else {
			assertStorageIssue(t, err, CategoryInvalidEnvelope)
		}
	})
	t.Run("targeted moderator reason permits repeated new axis", func(t *testing.T) {
		sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_required_repeated_new_axis_allowed", qualityRequired)
		appendArgumentGraphTargetForTest(t, sessionDir, metadata)
		appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_required_new_axis_targeted_grant", Payload: map[string]any{"turn": 2, "member": "agent-2", "selection_mode": "moderator_direct", "reason": "new axis requested to cover missing decision dimension"}, Now: fixedRuntime().Now().Add(21 * time.Second)})
		result := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_required_new_axis_targeted_speak", Payload: map[string]any{"turn": 2, "speech": "another justified axis", "claims": []any{map[string]any{"claim_id": "T2.C1", "summary": "new"}}, "contribution_type": "new_axis", "new_axis_reason": "moderator requested the missing decision dimension"}, Now: fixedRuntime().Now().Add(22 * time.Second)})
		event := eventByIDForTest(t, sessionDir, metadata, result.EventID)
		if event.Payload["contribution_type"] != "new_axis" {
			t.Fatalf("targeted repeated new_axis speech not recorded: %#v", event.Payload)
		}
	})
}

func TestUnitCouncilArgumentGraphDiscussionQualityModeResolutionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits Limits
	}{
		{
			name:   "present missing mode",
			limits: Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{}}},
		},
		{
			name:   "unsupported mode",
			limits: Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{Mode: "best_effort"}}},
		},
		{
			name:   "negative opening window",
			limits: Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{Mode: "quality_required", OpeningUnlinkedTurns: -1}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome, loaded := loadedCouncilRegistry(t)
			_, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
				Session: SessionSpec{
					ID:        "sess_argue_limits_" + strings.ReplaceAll(tc.name, " ", "_"),
					Title:     "argue limits",
					Moderator: "agent-mod",
					EventID:   "evt_argue_limits_" + strings.ReplaceAll(tc.name, " ", "_"),
					CommandID: "cmd_argue_limits_" + strings.ReplaceAll(tc.name, " ", "_"),
					Limits:    tc.limits,
				},
				Members: []string{"agent-1"},
				Now:     fixedRuntime().Now(),
			}, fixedRuntime())
			if err == nil {
				t.Fatalf("malformed discussion_quality must fail closed")
			}
			assertStorageIssue(t, err, CategoryInvalidEnvelope)
		})
	}
}

func TestUnitCouncilArgumentGraphQualityWarnDiagnosticsDoNotMutateSpeechOrInferLinks(t *testing.T) {
	limits := Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{Mode: "quality_warn", OpeningUnlinkedTurns: 1}}}
	sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_warn", limits)
	appendArgumentGraphTargetForTest(t, sessionDir, metadata)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_warn_grant", Payload: map[string]any{"turn": 2, "member": "agent-2", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(21 * time.Second)})
	originalSpeech := "An unlinked but accepted warning-mode speech."
	result := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_warn_speak", Payload: map[string]any{"turn": 2, "speech": originalSpeech}, Now: fixedRuntime().Now().Add(22 * time.Second)})
	event := eventByIDForTest(t, sessionDir, metadata, result.EventID)
	if event.Payload["speech"] != originalSpeech {
		t.Fatalf("quality_warn must not mutate speech: %#v", event.Payload)
	}
	if _, ok := event.Payload["stance_links"]; ok {
		t.Fatalf("quality_warn must not infer durable stance_links: %#v", event.Payload)
	}
	diagnostics, ok := event.Payload["quality_diagnostics"].([]any)
	if !ok || len(diagnostics) == 0 {
		t.Fatalf("quality_warn diagnostics missing: %#v", event.Payload)
	}
}

func TestUnitCouncilArgumentGraphAutoSelectionScopeAndNoConsecutiveSpeaker(t *testing.T) {
	t.Run("quality aware no eligible fails closed", func(t *testing.T) {
		limits := Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{Mode: "quality_warn"}}}
		sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_auto_no_eligible_quality", limits)
		if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_auto_quality_no_eligible", Payload: map[string]any{"turn": 1, "auto": true}, Now: fixedRuntime().Now().Add(10 * time.Second)}); err == nil {
			t.Fatalf("quality-aware auto-selection without eligible hand_raise must fail closed")
		} else {
			assertStorageIssue(t, err, CategoryCommandConflict)
		}
	})
	t.Run("compatibility no eligible preserves fallback", func(t *testing.T) {
		sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_auto_no_eligible_compat", Limits{})
		result := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_auto_compat_no_eligible", Payload: map[string]any{"turn": 1, "auto": true}, Now: fixedRuntime().Now().Add(10 * time.Second)})
		event := eventByIDForTest(t, sessionDir, metadata, result.EventID)
		if event.Payload["member"] != "agent-1" {
			t.Fatalf("compatibility auto fallback selected %#v", event.Payload)
		}
		if event.Payload["selection_mode"] != "relevance" {
			t.Fatalf("compatibility auto fallback should retain the additive relevance mode marker: %#v", event.Payload)
		}
		if _, ok := event.Payload["selection_score"]; !ok {
			t.Fatalf("compatibility auto fallback should record selection_score: %#v", event.Payload)
		}
	})
	t.Run("quality-aware auto avoids immediately previous speaker when another eligible member exists", func(t *testing.T) {
		limits := Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{Mode: "quality_warn"}}}
		sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_auto_no_consecutive", limits)
		appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_auto_first_grant", Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(10 * time.Second)})
		appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-1", CommandID: "cmd_auto_first_speak", Payload: map[string]any{"turn": 1, "speech": "opening"}, Now: fixedRuntime().Now().Add(11 * time.Second)})
		appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_auto_poll_2", Payload: map[string]any{"turn": 2}, Now: fixedRuntime().Now().Add(12 * time.Second)})
		appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-2", CommandID: "cmd_auto_raise_2", Payload: map[string]any{"turn": 2, "intent": "support", "relevance": 1}, Now: fixedRuntime().Now().Add(13 * time.Second)})
		appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_auto_raise_1", Payload: map[string]any{"turn": 2, "intent": "risk", "relevance": 9}, Now: fixedRuntime().Now().Add(14 * time.Second)})
		result := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_auto_no_consecutive_grant", Payload: map[string]any{"turn": 2, "auto": true}, Now: fixedRuntime().Now().Add(15 * time.Second)})
		event := eventByIDForTest(t, sessionDir, metadata, result.EventID)
		if event.Payload["member"] != "agent-2" {
			t.Fatalf("auto-selection must avoid immediately previous speaker when possible: %#v", event.Payload)
		}
		if event.Payload["selection_mode"] != "relevance" {
			t.Fatalf("auto-selection should record relevance mode: %#v", event.Payload)
		}
	})
}

func TestUnitCouncilStatusFromLogSummarizesVerboseStatus(t *testing.T) {
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        "sess_council_status",
			Title:     "status",
			Moderator: "agent-mod",
			Surface:   &Surface{Kind: "discord_thread", ThreadID: "thread-1"},
			TurnMode:  "role_order",
			EventID:   "evt_status_created",
			CommandID: "cmd_status_new",
		},
		Members: []string{"agent-1", "agent-2"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "request-attendance", Actor: "agent-mod", CommandID: "cmd_status_attendance", Payload: map[string]any{"timeout_sec": 300}, Now: fixedRuntime().Now().Add(time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "attend", Actor: "agent-1", CommandID: "cmd_status_attend_1", Payload: map[string]any{"status": "present", "summary": "ready"}, Now: fixedRuntime().Now().Add(2 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "attend", Actor: "agent-2", CommandID: "cmd_status_attend_2", Payload: map[string]any{"status": "unavailable", "summary": "offline"}, Now: fixedRuntime().Now().Add(3 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "lock-agenda", Actor: "agent-mod", CommandID: "cmd_status_agenda", Payload: map[string]any{"decision_question": "Ship?"}, Now: fixedRuntime().Now().Add(4 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "prepare", Actor: "agent-mod", CommandID: "cmd_status_prepare", Payload: map[string]any{"timeout_sec": 600}, Now: fixedRuntime().Now().Add(5 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "ready", Actor: "agent-1", CommandID: "cmd_status_ready", Payload: map[string]any{"summary": "ready"}, Now: fixedRuntime().Now().Add(6 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "prepared-partial", Actor: "agent-2", CommandID: "cmd_status_partial", Payload: map[string]any{"reason": "offline"}, Now: fixedRuntime().Now().Add(7 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_status_poll", Payload: map[string]any{"research_timeout_sec": 600}, Now: fixedRuntime().Now().Add(8 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_status_raise", Payload: map[string]any{"turn": 1, "intent": "support", "reason": "enough evidence"}, Now: fixedRuntime().Now().Add(9 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_status_grant", Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "role_order"}, Now: fixedRuntime().Now().Add(10 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-1", CommandID: "cmd_status_speak", Payload: map[string]any{"turn": 1, "speech": "ship"}, Now: fixedRuntime().Now().Add(11 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_status_propose", Payload: map[string]any{"draft": "ship"}, Now: fixedRuntime().Now().Add(12 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "request-vote", Actor: "agent-mod", CommandID: "cmd_status_vote_request", Payload: map[string]any{"draft_version": 1}, Now: fixedRuntime().Now().Add(13 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-1", CommandID: "cmd_status_vote_1", Payload: map[string]any{"draft_version": 1, "vote": "approve", "reason": "ok"}, Now: fixedRuntime().Now().Add(14 * time.Second)})

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	if status["phase"] != Phase("consensus_vote") || status["status"] != StatusOpen {
		t.Fatalf("phase/status = %#v/%#v", status["phase"], status["status"])
	}
	if status["current_turn"] != 1 || status["consensus_round"] != 1 {
		t.Fatalf("turn/round = %#v/%#v", status["current_turn"], status["consensus_round"])
	}
	attendance := status["attendance"].(map[string]any)
	if attendance["complete"] != true {
		t.Fatalf("attendance should be complete: %#v", attendance)
	}
	agenda := status["agenda"].(map[string]any)
	if agenda["decision_question"] != "Ship?" {
		t.Fatalf("agenda = %#v", agenda)
	}
	vote := status["vote"].(map[string]any)
	if vote["open"] != true || vote["complete"] != false {
		t.Fatalf("vote status = %#v", vote)
	}
	missing := vote["missing_members"].([]string)
	if len(missing) != 1 || missing[0] != "agent-2" {
		t.Fatalf("missing voters = %#v", missing)
	}
	handRaises := status["hand_raises"].([]map[string]any)
	if len(handRaises) != 1 || handRaises[0]["member"] != "agent-1" {
		t.Fatalf("hand raises = %#v", handRaises)
	}
}

func TestUnitCouncilCommandIDRetryDeduplicatesAndConflictingPayloadFailsClosed(t *testing.T) {
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        "sess_council_idempotency",
			Title:     "idempotency",
			Moderator: "agent-mod",
			EventID:   "evt_idempotency_created",
			CommandID: "cmd_idempotency_new",
		},
		Members: []string{"agent-1"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{
		Action:    "prepare",
		Actor:     "agent-mod",
		CommandID: "cmd_council_prepare_idempotent",
		Payload:   map[string]any{"topic": "idempotency", "timeout_sec": 600},
		Now:       fixedRuntime().Now().Add(time.Second),
	})

	readySpec := CouncilEventSpec{
		Action:    "ready",
		Actor:     "agent-1",
		CommandID: "cmd_council_ready_idempotent",
		Payload:   map[string]any{"summary": "ready with same payload"},
		Now:       fixedRuntime().Now().Add(2 * time.Second),
	}
	first, firstDedup, err := RecordCouncilEvent(sessionDir, metadata, readySpec)
	if err != nil {
		t.Fatalf("RecordCouncilEvent ready first: %v", err)
	}
	if firstDedup {
		t.Fatalf("first ready append should not deduplicate")
	}
	beforeRetry := eventCountForTest(t, sessionDir, metadata)
	replayed, replayDedup, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{
		Action:    readySpec.Action,
		Actor:     readySpec.Actor,
		CommandID: readySpec.CommandID,
		Payload:   map[string]any{"summary": "ready with same payload"},
		Now:       fixedRuntime().Now().Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("RecordCouncilEvent ready idempotent replay: %v", err)
	}
	if !replayDedup || replayed.EventID != first.EventID || replayed.Offset != first.Offset {
		t.Fatalf("expected idempotent ready replay, got result=%#v dedup=%v first=%#v", replayed, replayDedup, first)
	}
	if afterRetry := eventCountForTest(t, sessionDir, metadata); afterRetry != beforeRetry {
		t.Fatalf("idempotent retry appended duplicate event: before=%d after=%d", beforeRetry, afterRetry)
	}

	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{
		Action:    readySpec.Action,
		Actor:     readySpec.Actor,
		CommandID: readySpec.CommandID,
		Payload:   map[string]any{"summary": "different payload must conflict"},
		Now:       fixedRuntime().Now().Add(4 * time.Second),
	}); err == nil {
		t.Fatalf("same council command_id with different payload must fail closed")
	} else {
		assertStorageIssue(t, err, CategoryCommandConflict)
	}
	if afterConflict := eventCountForTest(t, sessionDir, metadata); afterConflict != beforeRetry {
		t.Fatalf("conflicting retry appended event: before=%d after=%d", beforeRetry, afterConflict)
	}
}

func TestIntegrationCouncilCommonBlockResumeCancelAndActiveSessionLock(t *testing.T) {
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        "sess_council_common_controls",
			Title:     "common controls",
			Moderator: "agent-mod",
			EventID:   "evt_common_controls_created",
			CommandID: "cmd_common_controls_new",
		},
		Members: []string{"agent-1"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	active, err := FindActiveSession(dataHome, fixedRuntime())
	if err != nil {
		t.Fatalf("FindActiveSession after council create: %v", err)
	}
	if active == nil || active.SessionID != metadata.ID || active.Status != StatusOpen {
		t.Fatalf("created council should hold open active-session lock, active=%#v", active)
	}

	if _, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        "sess_council_second_while_open",
			Title:     "second",
			Moderator: "agent-mod",
			EventID:   "evt_second_open_created",
			CommandID: "cmd_second_open_new",
		},
		Members: []string{"agent-1"},
		Now:     fixedRuntime().Now().Add(time.Second),
	}, fixedRuntime()); err == nil {
		t.Fatalf("open council active-session lock must reject a second session")
	} else {
		assertStorageIssue(t, err, CategoryCommandConflict)
	}

	blocked, blockDedup, err := BlockSession(sessionDir, metadata, SessionBlockSpec{
		Actor:     "agent-mod",
		Category:  "external_dependency",
		Reason:    "awaiting external evidence",
		CommandID: "cmd_council_block",
		Now:       fixedRuntime().Now().Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("BlockSession council: %v", err)
	}
	if blockDedup {
		t.Fatalf("first council block should not deduplicate")
	}
	blockEvent := eventByIDForTest(t, sessionDir, metadata, blocked.EventID)
	if blockEvent.Type != "session_blocked" || blockEvent.Phase != "blocked" || blockEvent.SessionType != SessionTypeCouncil {
		t.Fatalf("council block event has wrong shape: %+v", blockEvent)
	}
	active, err = FindActiveSession(dataHome, fixedRuntime())
	if err != nil {
		t.Fatalf("FindActiveSession after council block: %v", err)
	}
	if active == nil || active.SessionID != metadata.ID || active.Status != StatusBlocked || active.Phase != "blocked" {
		t.Fatalf("blocked council should keep active-session lock as blocked, active=%#v", active)
	}

	if _, _, err := ResumeSession(sessionDir, metadata, SessionResumeSpec{
		Actor:          "agent-mod",
		BlockedEventID: blocked.EventID,
		Reason:         "evidence arrived",
		CommandID:      "cmd_council_resume",
		Now:            fixedRuntime().Now().Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("ResumeSession council: %v", err)
	}
	active, err = FindActiveSession(dataHome, fixedRuntime())
	if err != nil {
		t.Fatalf("FindActiveSession after council resume: %v", err)
	}
	if active == nil || active.SessionID != metadata.ID || active.Status != StatusOpen || active.Phase != "created" {
		t.Fatalf("resumed council should keep active-session lock in resume phase, active=%#v", active)
	}

	cancelled, cancelDedup, err := CancelSession(sessionDir, metadata, SessionCancelSpec{
		Actor:     "agent-mod",
		Reason:    "scope closed",
		Cause:     "user_request",
		CommandID: "cmd_council_cancel",
		Now:       fixedRuntime().Now().Add(4 * time.Second),
	})
	if err != nil {
		t.Fatalf("CancelSession council: %v", err)
	}
	if cancelDedup {
		t.Fatalf("first council cancel should not deduplicate")
	}
	cancelEvent := eventByIDForTest(t, sessionDir, metadata, cancelled.EventID)
	if cancelEvent.Type != "session_cancelled" || cancelEvent.Phase != "cancelled" || cancelEvent.SessionType != SessionTypeCouncil {
		t.Fatalf("council cancel event has wrong shape: %+v", cancelEvent)
	}
	if active, err := FindActiveSession(dataHome, fixedRuntime()); err != nil || active != nil {
		t.Fatalf("cancelled council must release active-session lock, active=%#v err=%v", active, err)
	}

	if _, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        "sess_council_after_cancel",
			Title:     "after cancel",
			Moderator: "agent-mod",
			EventID:   "evt_after_cancel_created",
			CommandID: "cmd_after_cancel_new",
		},
		Members: []string{"agent-1"},
		Now:     fixedRuntime().Now().Add(5 * time.Second),
	}, fixedRuntime()); err != nil {
		t.Fatalf("active-session lock should allow new council after cancel: %v", err)
	}
}

func appendCouncilForTest(t *testing.T, sessionDir string, metadata *SessionMetadata, spec CouncilEventSpec) AppendResult {
	t.Helper()
	result, _, err := RecordCouncilEvent(sessionDir, metadata, spec)
	if err != nil {
		t.Fatalf("RecordCouncilEvent(%s): %v", spec.Action, err)
	}
	return result
}

func argumentGraphCouncilForTest(t *testing.T, sessionID string, limits Limits) (string, *SessionMetadata) {
	t.Helper()
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        sessionID,
			Title:     "argue",
			Moderator: "agent-mod",
			EventID:   "evt_" + sessionID + "_created",
			CommandID: "cmd_" + sessionID + "_new",
			Limits:    limits,
		},
		Members: []string{"agent-1", "agent-2"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendRawEventForTest(t, sessionDir, metadata, "evt_"+sessionID+"_prep", "cmd_"+sessionID+"_prep", "preparation_requested", "preparation", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"timeout_sec": 1}, time.Second)
	appendRawEventForTest(t, sessionDir, metadata, "evt_"+sessionID+"_poll_1", "cmd_"+sessionID+"_poll_1", "hand_raise_requested", "discussion", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"turn": 1}, 2*time.Second)
	return sessionDir, metadata
}

func appendArgumentGraphTargetForTest(t *testing.T, sessionDir string, metadata *SessionMetadata) {
	t.Helper()
	appendRawEventForTest(t, sessionDir, metadata, "evt_argue_target_poll", "cmd_argue_target_poll", "hand_raise_requested", "discussion", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"turn": 1}, 9*time.Second)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_argue_target_grant", Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(10 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-1", CommandID: "cmd_argue_target_speech", Payload: map[string]any{"turn": 1, "speech": "Opening claim.", "claims": []any{map[string]any{"claim_id": "T1.C1", "summary": "opening claim"}}, "contribution_type": "new_axis", "new_axis_reason": "opening axis"}, Now: fixedRuntime().Now().Add(11 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_argue_poll_2", Payload: map[string]any{"turn": 2}, Now: fixedRuntime().Now().Add(12 * time.Second)})
}

func loadedCouncilRegistry(t *testing.T) (string, *registry.LoadedRegistry) {
	t.Helper()
	dataHome := t.TempDir()
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	content := `schema_version: 1
members:
  agent-mod:
    display_name: Moderator
    wrapper: missing-agent-mod-wrapper
    workspace: /tmp/agent-mod
    role: moderator
    enabled: false
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
  agent-1:
    display_name: Agent One
    wrapper: missing-agent-1-wrapper
    workspace: /tmp/agent-1
    role: council_member
    enabled: false
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
  agent-2:
    display_name: Agent Two
    wrapper: missing-agent-2-wrapper
    workspace: /tmp/agent-2
    role: council_member
    enabled: false
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
`
	if err := os.WriteFile(registry.RegistryPath(dataHome), []byte(content), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	loaded, err := registry.Load(dataHome, fixedRuntime())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return dataHome, loaded
}

func assertCouncilStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice len mismatch got=%#v want=%#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("slice mismatch got=%#v want=%#v", got, want)
		}
	}
}

func rowCountWhere(t *testing.T, db *sql.DB, table, where string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE " + where).Scan(&count); err != nil {
		t.Fatalf("count %s where %s: %v", table, where, err)
	}
	return count
}
