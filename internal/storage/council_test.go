package storage

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"atn-control/internal/protocol"
	"atn-control/internal/registry"

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
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "lock-agenda", Actor: "agent-mod", CommandID: "cmd_agenda", Payload: map[string]any{"decision_question": "What should ship?", "success_criteria": "The next action is bounded and evidenced.", "out_of_scope_policy": "New topics become follow-up cards.", "max_rounds": 2}, Now: fixedRuntime().Now().Add(5 * time.Second)})
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
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_grant_member_to_mismatch", Payload: map[string]any{"turn": 1, "member": "agent-1", "to": "agent-2", "selection_mode": "moderator_direct", "reason": "targeted answer"}, Now: fixedRuntime().Now().Add(11 * time.Second)}); err == nil {
		t.Fatalf("grant payload.member/to mismatch must fail closed")
	} else {
		assertStorageIssue(t, err, CategoryInvalidEnvelope)
	}
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
	finalize := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "finalize", Actor: "agent-mod", CommandID: "cmd_finalize", Payload: map[string]any{"final_summary": "Consensus reached.", "surface_evidence": map[string]any{"status": "posted", "kind": "discord_thread", "thread_id": metadata.Surface.ThreadID, "final_message_id": "msg-council-lifecycle-final"}}, Now: fixedRuntime().Now().Add(21 * time.Second)})
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

func TestUnitCOUNCILSTAB001GrantRequiresPriorHandRaiseStanceSource(t *testing.T) {
	for _, tc := range []struct {
		name          string
		setup         func(t *testing.T, sessionDir string, metadata *SessionMetadata)
		payload       map[string]any
		wantIssuePath string
	}{
		{
			name:          "no prior hand raise",
			payload:       map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"},
			wantIssuePath: "stance_assignment",
		},
		{
			name: "caller stance assignment cannot repair missing hand raise stance",
			setup: func(t *testing.T, sessionDir string, metadata *SessionMetadata) {
				appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_stance_missing_raise", Payload: map[string]any{"turn": 1, "wants_to_speak": true}, Now: fixedRuntime().Now().Add(3 * time.Second)})
			},
			payload:       map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct", "stance_assignment": "support"},
			wantIssuePath: "stance_assignment",
		},
		{
			name: "wrong member hand raise does not satisfy grant",
			setup: func(t *testing.T, sessionDir string, metadata *SessionMetadata) {
				appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-2", CommandID: "cmd_stance_wrong_member_raise", Payload: map[string]any{"turn": 1, "intent": "challenge"}, Now: fixedRuntime().Now().Add(3 * time.Second)})
			},
			payload:       map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"},
			wantIssuePath: "stance_assignment",
		},
		{
			name: "wrong turn hand raise does not satisfy grant",
			setup: func(t *testing.T, sessionDir string, metadata *SessionMetadata) {
				appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_stance_wrong_turn_raise", Payload: map[string]any{"turn": 1, "intent": "challenge"}, Now: fixedRuntime().Now().Add(3 * time.Second)})
				appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_stance_poll_2", Payload: map[string]any{"turn": 2}, Now: fixedRuntime().Now().Add(4 * time.Second)})
			},
			payload:       map[string]any{"turn": 2, "member": "agent-1", "selection_mode": "moderator_direct"},
			wantIssuePath: "stance_assignment",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir, metadata := grantStanceCouncilForTest(t, "sess_council_stab_"+strings.ReplaceAll(tc.name, " ", "_"))
			if tc.setup != nil {
				tc.setup(t, sessionDir, metadata)
			}
			before := eventCountForTest(t, sessionDir, metadata)
			_, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_grant_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: tc.payload, Now: fixedRuntime().Now().Add(5 * time.Second)})
			if err == nil {
				t.Fatalf("grant without matching stance-bearing hand_raise must fail closed")
			}
			assertStorageIssue(t, err, CategoryInvalidEnvelope)
			if !strings.Contains(err.Error(), tc.wantIssuePath) {
				t.Fatalf("error should name %s, got %v", tc.wantIssuePath, err)
			}
			if after := eventCountForTest(t, sessionDir, metadata); after != before {
				t.Fatalf("failed grant appended event: before=%d after=%d", before, after)
			}
		})
	}
}

func TestUnitCOUNCILSTAB001GrantDerivesStanceFromPriorHandRaise(t *testing.T) {
	for _, tc := range []struct {
		name        string
		handPayload map[string]any
		wantStance  string
	}{
		{name: "intent preferred over reason", handPayload: map[string]any{"turn": 1, "intent": "challenge", "reason": "fallback reason"}, wantStance: "challenge"},
		{name: "reason used without intent", handPayload: map[string]any{"turn": 1, "reason": "need a risk response"}, wantStance: "need a risk response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir, metadata := grantStanceCouncilForTest(t, "sess_council_stab_positive_"+strings.ReplaceAll(tc.name, " ", "_"))
			appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_positive_raise_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: tc.handPayload, Now: fixedRuntime().Now().Add(3 * time.Second)})
			result := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_positive_grant_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct", "stance_assignment": "caller must be ignored"}, Now: fixedRuntime().Now().Add(4 * time.Second)})
			event := eventByIDForTest(t, sessionDir, metadata, result.EventID)
			if got := payloadStringDefault(event.Payload, "stance_assignment", ""); got != tc.wantStance {
				t.Fatalf("stance_assignment = %q, want %q; payload=%#v", got, tc.wantStance, event.Payload)
			}
		})
	}
}

func TestUnitNEWFIX007CouncilLockAgendaRequiresStructuredContextAtStorageBoundary(t *testing.T) {
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{ID: "sess_newfix007_storage", Title: "NEWFIX-007 storage", Moderator: "agent-mod", EventID: "evt_newfix007_storage_created", CommandID: "cmd_newfix007_storage_new"},
		Members: []string{"agent-1"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	for _, tc := range []struct {
		name    string
		payload map[string]any
		field   string
	}{
		{name: "missing success criteria", payload: map[string]any{"decision_question": "What proves NEWFIX-007?", "out_of_scope_policy": "No hidden context."}, field: "success_criteria"},
		{name: "empty success criteria", payload: map[string]any{"decision_question": "What proves NEWFIX-007?", "success_criteria": "", "out_of_scope_policy": "No hidden context."}, field: "success_criteria"},
		{name: "whitespace success criteria", payload: map[string]any{"decision_question": "What proves NEWFIX-007?", "success_criteria": " 	\n ", "out_of_scope_policy": "No hidden context."}, field: "success_criteria"},
		{name: "missing out of scope policy", payload: map[string]any{"decision_question": "What proves NEWFIX-007?", "success_criteria": "Structured context is durable."}, field: "out_of_scope_policy"},
		{name: "non string success criteria", payload: map[string]any{"decision_question": "What proves NEWFIX-007?", "success_criteria": []string{"bad"}, "out_of_scope_policy": "No hidden context."}, field: "success_criteria"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "lock-agenda", Actor: "agent-mod", CommandID: "cmd_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: tc.payload, Now: fixedRuntime().Now().Add(time.Second)}); err == nil {
				t.Fatalf("lock-agenda without %s must fail closed", tc.field)
			} else {
				assertStorageIssue(t, err, CategoryInvalidEnvelope)
				if !strings.Contains(err.Error(), tc.field) {
					t.Fatalf("error should name %s, got %v", tc.field, err)
				}
			}
		})
	}
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "lock-agenda", Actor: "agent-mod", CommandID: "cmd_newfix007_storage_lock", Payload: map[string]any{"decision_question": "What proves NEWFIX-007?", "success_criteria": "Structured agenda context is durable.", "out_of_scope_policy": "Do not infer required context from draft prose.", "max_rounds": 2}, Now: fixedRuntime().Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("lock-agenda with required structured context: %v", err)
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
		{name: "reserved member", moderator: "agent-mod", members: []string{"atn-controld"}},
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

func TestUnitRUNFIX3004FinalizeRequiresVisibleCloseoutProofAndExactThreadBinding(t *testing.T) {
	sessionDir, metadata := runfix3004ConsensusVoteCouncil(t, "sess_runfix3004_finalize")
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "finalize", Actor: "agent-mod", CommandID: "cmd_runfix3004_finalize_missing_surface", Payload: map[string]any{"final_summary": "done", "surface": metadata.Surface}, Now: fixedRuntime().Now().Add(90 * time.Second)}); err == nil {
		t.Fatalf("finalize without visible closeout proof must fail closed")
	} else {
		assertStorageIssue(t, err, CategoryCommandConflict)
		if !strings.Contains(err.Error(), "missing_visible_closeout_proof") {
			t.Fatalf("missing proof finalize error = %v", err)
		}
	}
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "finalize", Actor: "agent-mod", CommandID: "cmd_runfix3004_finalize_bad_thread", Payload: map[string]any{"final_summary": "done", "surface": metadata.Surface, "surface_evidence": map[string]any{"status": "posted", "kind": "discord_thread", "thread_id": "thread-other", "final_message_id": "msg-bad-thread"}}, Now: fixedRuntime().Now().Add(91 * time.Second)}); err == nil {
		t.Fatalf("finalize with mismatched thread proof must fail closed")
	} else {
		assertStorageIssue(t, err, CategoryCommandConflict)
		if !strings.Contains(err.Error(), "exact_thread_mismatch") {
			t.Fatalf("thread mismatch finalize error = %v", err)
		}
	}
	result, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "finalize", Actor: "agent-mod", CommandID: "cmd_runfix3004_finalize_ok", Payload: map[string]any{"final_summary": "done", "surface": metadata.Surface, "surface_evidence": map[string]any{"status": "posted", "kind": "discord_thread", "thread_id": metadata.Surface.ThreadID, "final_message_id": "msg-final-ok"}, "closeout_diagnostics": []any{map[string]any{"code": "forged_diag", "reason": "caller should not control diagnostics"}}}, Now: fixedRuntime().Now().Add(92 * time.Second)})
	if err != nil {
		t.Fatalf("finalize with matching closeout proof: %v", err)
	}
	finalized := eventByIDForTest(t, sessionDir, metadata, result.EventID)
	if got := closeoutDiagnosticsFromPayload(finalized.Payload); len(got) != 0 {
		t.Fatalf("successful finalize must not persist caller-supplied closeout_diagnostics: %#v", got)
	}
}

func TestUnitLVCOR002FinalizeSurfaceEvidenceDiagnosticsStayExact(t *testing.T) {
	for _, tc := range []struct {
		name         string
		payload      map[string]any
		wantCategory string
		wantSnippets []string
	}{
		{
			name:         "malformed surface evidence object",
			payload:      map[string]any{"final_summary": "done", "surface_evidence": "bad-shape"},
			wantCategory: CategoryInvalidEnvelope,
			wantSnippets: []string{"surface_evidence", "object"},
		},
		{
			name:         "unsupported surface evidence status",
			payload:      map[string]any{"final_summary": "done", "surface_evidence": map[string]any{"status": "complete", "thread_id": "thread-placeholder", "final_message_id": "msg-placeholder"}},
			wantCategory: CategoryInvalidEnvelope,
			wantSnippets: []string{"surface_evidence.status", "posted", "failed", "pending_followup"},
		},
		{
			name:         "posted without concrete final message pointer",
			payload:      map[string]any{"final_summary": "done", "surface_evidence": map[string]any{"status": "posted", "kind": "discord_thread", "thread_id": "thread-placeholder"}},
			wantCategory: CategoryCommandConflict,
			wantSnippets: []string{"missing_final_message_id", "surface_evidence.final_message_id"},
		},
		{
			name:         "posted without thread binding",
			payload:      map[string]any{"final_summary": "done", "surface_evidence": map[string]any{"status": "posted", "kind": "discord_thread", "final_message_id": "msg-no-thread"}},
			wantCategory: CategoryCommandConflict,
			wantSnippets: []string{"missing_thread_binding", "surface_evidence.thread_id"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir, metadata := runfix3004ConsensusVoteCouncil(t, "sess_"+strings.ReplaceAll(tc.name, " ", "_"))
			payload := clonePayload(tc.payload)
			if evidence, ok := payload["surface_evidence"].(map[string]any); ok {
				if evidence["thread_id"] == "thread-placeholder" {
					evidence["thread_id"] = metadata.Surface.ThreadID
				}
			}
			_, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "finalize", Actor: "agent-mod", CommandID: "cmd_" + strings.ReplaceAll(tc.name, " ", "_"), Payload: payload, Now: fixedRuntime().Now().Add(90 * time.Second)})
			if err == nil {
				t.Fatalf("%s must fail closed", tc.name)
			}
			assertStorageIssue(t, err, tc.wantCategory)
			for _, want := range tc.wantSnippets {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("%s error missing %q: %v", tc.name, want, err)
				}
			}
		})
	}
}

func TestUnitRUNFIX3004UnresolvedAppendsWithCloseoutDiagnostics(t *testing.T) {
	sessionDir, metadata := runfix3004LifecycleCouncilForTest(t, "sess_runfix3004_unresolved")
	appendRUNFIX2LifecycleOpeningAndDiscussion(t, sessionDir, metadata)
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 3, "agent-1", 30*time.Second)
	result, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "unresolved", Actor: "agent-mod", CommandID: "cmd_runfix3004_unresolved", Payload: map[string]any{"reason": "closeout proof incomplete"}, Now: fixedRuntime().Now().Add(40 * time.Second)})
	if err != nil {
		t.Fatalf("unresolved with incomplete closeout should stay appendable: %v", err)
	}
	event := eventByIDForTest(t, sessionDir, metadata, result.EventID)
	diagnostics := closeoutDiagnosticsFromPayload(event.Payload)
	if len(diagnostics) == 0 {
		t.Fatalf("unresolved event missing closeout_diagnostics: %#v", event.Payload)
	}
	for _, want := range []string{"missing_participant_closeout", "missing_moderator_synthesis", "missing_visible_closeout_proof"} {
		if !strings.Contains(mustCompactJSON(diagnostics), want) {
			t.Fatalf("unresolved diagnostics missing %q: %#v", want, diagnostics)
		}
	}
	status, err := CouncilStatusFromLogAt(sessionDir, metadata, fixedRuntime().Now().Add(41*time.Second))
	if err != nil {
		t.Fatalf("CouncilStatusFromLogAt: %v", err)
	}
	if !strings.Contains(mustCompactJSON(status["closeout_diagnostics"]), "missing_participant_closeout") {
		t.Fatalf("status closeout_diagnostics missing unresolved details: %#v", status["closeout_diagnostics"])
	}
}

func runfix3004ConsensusVoteCouncil(t *testing.T, sessionID string) (string, *SessionMetadata) {
	t.Helper()
	sessionDir, metadata := runfix3004LifecycleCouncilForTest(t, sessionID)
	appendRUNFIX2LifecycleOpeningAndDiscussion(t, sessionDir, metadata)
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 3, "agent-1", 30*time.Second)
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 4, "agent-2", 40*time.Second)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_propose", Payload: map[string]any{"draft": "ready"}, Now: fixedRuntime().Now().Add(50 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "request-vote", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_request_vote", Payload: map[string]any{"draft_version": 1}, Now: fixedRuntime().Now().Add(60 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-1", CommandID: "cmd_" + sessionID + "_vote_1", Payload: map[string]any{"draft_version": 1, "vote": "approve", "reason": "ok"}, Now: fixedRuntime().Now().Add(70 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-2", CommandID: "cmd_" + sessionID + "_vote_2", Payload: map[string]any{"draft_version": 1, "vote": "approve", "reason": "ok"}, Now: fixedRuntime().Now().Add(80 * time.Second)})
	return sessionDir, metadata
}

func runfix3004LifecycleCouncilForTest(t *testing.T, sessionID string) (string, *SessionMetadata) {
	t.Helper()
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        sessionID,
			Title:     "RUNFIX3 closeout",
			Moderator: "agent-mod",
			Surface:   &Surface{Kind: "discord_thread", Platform: "discord", ThreadID: "thread-" + sessionID},
			EventID:   "evt_" + sessionID + "_created",
			CommandID: "cmd_" + sessionID + "_new",
			Limits:    Limits{MaxDiscussionTurns: 2},
		},
		Members: []string{"agent-1", "agent-2"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "request-attendance", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_attendance", Payload: map[string]any{"timeout_sec": 60}, Now: fixedRuntime().Now().Add(time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "attend", Actor: "agent-1", CommandID: "cmd_" + sessionID + "_attend_1", Payload: map[string]any{"status": "present", "summary": "ready"}, Now: fixedRuntime().Now().Add(2 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "attend", Actor: "agent-2", CommandID: "cmd_" + sessionID + "_attend_2", Payload: map[string]any{"status": "present", "summary": "ready"}, Now: fixedRuntime().Now().Add(3 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "lock-agenda", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_agenda", Payload: map[string]any{"decision_question": "RUNFIX3?", "success_criteria": "Record closeout proof only when all required evidence is present.", "out_of_scope_policy": "Do not infer missing closeout evidence."}, Now: fixedRuntime().Now().Add(4 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "prepare", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_prepare", Payload: map[string]any{"timeout_sec": 60}, Now: fixedRuntime().Now().Add(5 * time.Second)})
	return sessionDir, metadata
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

func TestUnitCouncilArgumentGraphStatusSeparatesLifecycleAndDiscussionQuality(t *testing.T) {
	limits := Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{Mode: "quality_warn", OpeningUnlinkedTurns: 1}}}
	sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_warn_status", limits)
	appendArgumentGraphTargetForTest(t, sessionDir, metadata)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_warn_status_grant", Payload: map[string]any{"turn": 2, "member": "agent-2", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(21 * time.Second)})
	result := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_warn_status_speak", Payload: map[string]any{"turn": 2, "speech": "accepted but unlinked"}, Now: fixedRuntime().Now().Add(22 * time.Second)})
	event := eventByIDForTest(t, sessionDir, metadata, result.EventID)
	if _, ok := event.Payload["stance_links"]; ok {
		t.Fatalf("quality_warn must not infer durable stance_links: %#v", event.Payload)
	}

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	quality := status["discussion_quality"].(map[string]any)
	if quality["lifecycle_pass"] != false {
		t.Fatalf("open lifecycle should not pass: %#v", quality)
	}
	if quality["discussion_quality_pass"] != false {
		t.Fatalf("orphan warning must fail discussion quality: %#v", quality)
	}
	if quality["mode"] != "quality_warn" {
		t.Fatalf("quality mode not reported: %#v", quality)
	}
	hardWarnings := quality["hard_warning_counts"].(map[string]int)
	if hardWarnings["orphan_speech"] != 1 {
		t.Fatalf("orphan hard warning count missing: %#v", quality)
	}
	if quality["speech_count"] != 2 || quality["orphan_speech_count"] != 1 || quality["linked_speech_count"] != 0 {
		t.Fatalf("speech summary counts wrong: %#v", quality)
	}
}

func TestUnitCouncilArgumentGraphStatusPassesLinkedQualityBeforeLifecycleCompletion(t *testing.T) {
	limits := Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{
		Mode:                           "quality_required",
		OpeningUnlinkedTurns:           1,
		RequireClaims:                  true,
		RequireStanceLinksAfterOpening: true,
		AllowNewAxisWithReason:         true,
		MaxConsecutiveNewAxis:          1,
	}}}
	sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_required_status_pass", limits)
	appendArgumentGraphTargetForTest(t, sessionDir, metadata)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_required_status_grant", Payload: map[string]any{"turn": 2, "member": "agent-2", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(21 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_required_status_speak", Payload: map[string]any{
		"turn":   2,
		"speech": "linked response",
		"claims": []any{map[string]any{"claim_id": "T2.C1", "summary": "linked claim"}},
		"stance_links": []any{map[string]any{
			"target_event_id": "evt_speech_cmd_argue_target_speech",
			"target_claim_id": "T1.C1",
			"stance":          "support",
			"rationale":       "connects to the opening claim",
		}},
		"contribution_type": "support",
	}, Now: fixedRuntime().Now().Add(22 * time.Second)})

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	quality := status["discussion_quality"].(map[string]any)
	if quality["lifecycle_pass"] != false {
		t.Fatalf("open lifecycle should not pass: %#v", quality)
	}
	if quality["discussion_quality_pass"] != true {
		t.Fatalf("linked ARGUE quality should pass independently of lifecycle: %#v", quality)
	}
	relationCounts := quality["relation_counts"].(map[string]int)
	if relationCounts["support"] != 1 || quality["target_link_count"] != 1 || quality["linked_speech_count"] != 1 {
		t.Fatalf("relation summary counts wrong: %#v", quality)
	}
}

func TestUnitCouncilArgumentGraphStatusSummarizesHandRaiseTargetLinks(t *testing.T) {
	limits := Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{Mode: "quality_warn"}}}
	sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_hand_raise_status", limits)
	appendArgumentGraphTargetForTest(t, sessionDir, metadata)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-2", CommandID: "cmd_hand_raise_status", Payload: map[string]any{
		"turn":   2,
		"intent": "challenge",
		"target_links": []any{map[string]any{
			"target_event_id": "evt_speech_cmd_argue_target_speech",
			"target_claim_id": "T1.C1",
			"stance":          "challenge",
		}},
	}, Now: fixedRuntime().Now().Add(20 * time.Second)})
	grant := appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_hand_raise_status_auto_grant", Payload: map[string]any{"turn": 2, "auto": true}, Now: fixedRuntime().Now().Add(21 * time.Second)})
	selected := eventByIDForTest(t, sessionDir, metadata, grant.EventID)
	graphNeed, ok := selected.Payload["graph_need"].(map[string]any)
	if !ok {
		t.Fatalf("auto selection should preserve graph_need: %#v", selected.Payload)
	}
	if anyInt(graphNeed, "relation_count") != 1 || anyInt(graphNeed, "target_link_count") != 1 {
		t.Fatalf("graph_need relation counts missing: %#v", graphNeed)
	}

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	quality := status["discussion_quality"].(map[string]any)
	if quality["hand_raise_target_link_count"] != 1 {
		t.Fatalf("hand raise target links not summarized: %#v", quality)
	}
	handRaiseRelations := quality["hand_raise_relation_counts"].(map[string]int)
	if handRaiseRelations["challenge"] != 1 {
		t.Fatalf("hand raise relation counts wrong: %#v", quality)
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
	t.Run("compatibility no eligible fails without stance source", func(t *testing.T) {
		sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_auto_no_eligible_compat", Limits{})
		if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_auto_compat_no_eligible", Payload: map[string]any{"turn": 1, "auto": true}, Now: fixedRuntime().Now().Add(10 * time.Second)}); err == nil {
			t.Fatalf("auto grant without stance-bearing hand_raise must fail closed")
		} else {
			assertStorageIssue(t, err, CategoryInvalidEnvelope)
			if !strings.Contains(err.Error(), "stance_assignment") {
				t.Fatalf("auto grant error should name stance_assignment, got %v", err)
			}
		}
	})
	t.Run("quality-aware auto avoids immediately previous speaker when another eligible member exists", func(t *testing.T) {
		limits := Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{Mode: "quality_warn"}}}
		sessionDir, metadata := argumentGraphCouncilForTest(t, "sess_argue_auto_no_consecutive", limits)
		appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_auto_first_raise", Payload: map[string]any{"turn": 1, "intent": "opening"}, Now: fixedRuntime().Now().Add(9 * time.Second)})
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
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "lock-agenda", Actor: "agent-mod", CommandID: "cmd_status_agenda", Payload: map[string]any{"decision_question": "Ship?", "success_criteria": "Members can vote with sufficient locked context.", "out_of_scope_policy": "Do not infer agenda context from later vote prose."}, Now: fixedRuntime().Now().Add(4 * time.Second)})
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
func TestUnitCouncilCommandIDRetryDeduplicatesLegacyCouncilPayloadWithoutInjectedMetadata(t *testing.T) {
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        "sess_council_idempotency_legacy",
			Title:     "legacy-idempotency",
			Moderator: "agent-mod",
			EventID:   "evt_idempotency_legacy_created",
			CommandID: "cmd_idempotency_legacy_new",
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
		CommandID: "cmd_council_prepare_legacy_idempotent",
		Payload:   map[string]any{"topic": "legacy idempotency", "timeout_sec": 600},
		Now:       fixedRuntime().Now().Add(time.Second),
	})
	legacyReady := EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventIDFromCommand("evt_member_ready", "cmd_council_ready_legacy_idempotent"),
		CommandID:     "cmd_council_ready_legacy_idempotent",
		CorrelationID: metadata.ID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         "preparation",
		Type:          "member_ready",
		From:          "agent-1",
		To:            []string{"agent-mod"},
		CreatedAt:     fixedRuntime().Now().Add(2 * time.Second),
		Payload:       map[string]any{"summary": "legacy ready payload without injected metadata"},
	}
	if _, err := AppendEvent(sessionDir, metadata, legacyReady); err != nil {
		t.Fatalf("AppendEvent legacy ready: %v", err)
	}
	beforeRetry := eventCountForTest(t, sessionDir, metadata)
	replayed, dedup, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{
		Action:    "ready",
		Actor:     "agent-1",
		CommandID: legacyReady.CommandID,
		Payload:   map[string]any{"summary": "legacy ready payload without injected metadata"},
		Now:       fixedRuntime().Now().Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("RecordCouncilEvent legacy ready replay: %v", err)
	}
	if !dedup || replayed.EventID != legacyReady.EventID {
		t.Fatalf("expected legacy idempotent replay, got result=%#v dedup=%v legacy=%#v", replayed, dedup, legacyReady)
	}
	if afterRetry := eventCountForTest(t, sessionDir, metadata); afterRetry != beforeRetry {
		t.Fatalf("legacy idempotent retry appended duplicate event: before=%d after=%d", beforeRetry, afterRetry)
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

func grantStanceCouncilForTest(t *testing.T, sessionID string) (string, *SessionMetadata) {
	t.Helper()
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        sessionID,
			Title:     "grant stance",
			Moderator: "agent-mod",
			EventID:   "evt_" + sessionID + "_created",
			CommandID: "cmd_" + sessionID + "_new",
		},
		Members: []string{"agent-1", "agent-2"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendRawEventForTest(t, sessionDir, metadata, "evt_"+sessionID+"_prep", "cmd_"+sessionID+"_prep", "preparation_requested", "preparation", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"timeout_sec": 1}, time.Second)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_poll_1", Payload: map[string]any{"turn": 1}, Now: fixedRuntime().Now().Add(2 * time.Second)})
	return sessionDir, metadata
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
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_argue_target_raise", Payload: map[string]any{"turn": 1, "intent": "opening", "reason": "opening target claim"}, Now: fixedRuntime().Now().Add(9*time.Second + 500*time.Millisecond)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_argue_target_grant", Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(10 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-1", CommandID: "cmd_argue_target_speech", Payload: map[string]any{"turn": 1, "speech": "Opening claim.", "claims": []any{map[string]any{"claim_id": "T1.C1", "summary": "opening claim"}}, "contribution_type": "new_axis", "new_axis_reason": "opening axis"}, Now: fixedRuntime().Now().Add(11 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_argue_poll_2", Payload: map[string]any{"turn": 2}, Now: fixedRuntime().Now().Add(12 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-2", CommandID: "cmd_argue_raise_2", Payload: map[string]any{"turn": 2, "intent": "response", "reason": "respond to opening claim"}, Now: fixedRuntime().Now().Add(13 * time.Second)})
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
