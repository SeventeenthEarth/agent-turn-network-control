package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atn-control/internal/registry"
)

func TestRUNFIX2CouncilDiscussionLifecycleBlocksProposeUntilCloseouts(t *testing.T) {
	sessionDir, metadata := runfix2LifecycleCouncilForTest(t, "sess_runfix2_lifecycle_block")
	appendRUNFIX2LifecycleOpeningAndDiscussion(t, sessionDir, metadata)

	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_runfix2_propose_early", Payload: map[string]any{"draft": "too early"}, Now: fixedRuntime().Now().Add(30 * time.Second)}); err == nil {
		t.Fatalf("council.propose before participant closeouts must fail closed")
	} else {
		assertStorageIssue(t, err, CategoryCommandConflict)
	}

	status, err := CouncilStatusFromLogAt(sessionDir, metadata, fixedRuntime().Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("CouncilStatusFromLogAt: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if lifecycle.ExpectedVisibleTurns != 6 {
		t.Fatalf("expected visible turns = %d, want 6; lifecycle=%#v", lifecycle.ExpectedVisibleTurns, lifecycle)
	}
	if lifecycle.ProposeReady {
		t.Fatalf("lifecycle should not be propose-ready before closeouts: %#v", lifecycle)
	}
	if got := lifecycle.MissingCloseoutMembers; len(got) != 2 || got[0] != "agent-1" || got[1] != "agent-2" {
		t.Fatalf("missing closeouts = %#v, want both members; lifecycle=%#v", got, lifecycle)
	}
	diagnostics, ok := status["closeout_diagnostics"].([]map[string]any)
	if !ok || len(diagnostics) == 0 {
		t.Fatalf("closeout_diagnostics missing before closeouts: %#v", status["closeout_diagnostics"])
	}
	found := false
	for _, diagnostic := range diagnostics {
		if anyString(diagnostic, "code") == "missing_participant_closeout" {
			members, ok := diagnostic["member_ids"].([]string)
			if ok && len(members) == 2 && members[0] == "agent-1" && members[1] == "agent-2" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected missing_participant_closeout diagnostic, got %#v", diagnostics)
	}
}

func TestRUNFIX2CouncilDiscussionLifecycleAllowsProposeAfterT0ThroughT4AndExportsAccounting(t *testing.T) {
	sessionDir, metadata := runfix2LifecycleCouncilForTest(t, "sess_runfix2_lifecycle_complete")
	appendRUNFIX2LifecycleOpeningAndDiscussion(t, sessionDir, metadata)
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 3, "agent-1", 30*time.Second)
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 4, "agent-2", 40*time.Second)

	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_runfix2_propose_ready", Payload: map[string]any{"draft": "ready"}, Now: fixedRuntime().Now().Add(50 * time.Second)}); err != nil {
		t.Fatalf("council.propose after T0/T1/T2/T3/T4: %v", err)
	}

	status, err := CouncilStatusFromLogAt(sessionDir, metadata, fixedRuntime().Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("CouncilStatusFromLogAt: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if !lifecycle.ProposeReady || lifecycle.TerminalVisible {
		t.Fatalf("lifecycle readiness/terminal separation mismatch: %#v", lifecycle)
	}
	if lifecycle.ExpectedVisibleTurns != 6 || lifecycle.VisibleTurnTotal != 6 {
		t.Fatalf("visible totals mismatch: %#v", lifecycle)
	}

	manifest := runfix2BuildManifest(t, sessionDir, metadata)
	manifestLifecycle, ok := manifest["discussion_lifecycle"].(map[string]any)
	if !ok {
		t.Fatalf("manifest discussion_lifecycle missing: %#v", manifest["discussion_lifecycle"])
	}
	if manifestLifecycle["expected_visible_turns"] != float64(6) || manifestLifecycle["propose_ready"] != true {
		t.Fatalf("manifest lifecycle mismatch: %#v", manifestLifecycle)
	}
	rows := runfix016SummaryRows(t, manifest)
	wantStages := map[float64]string{1: "discussion", 2: "discussion", 3: "closeout", 4: "closeout"}
	wantIndexes := map[float64]float64{1: 2, 2: 3, 3: 4, 4: 5}
	for _, row := range rows {
		turn := row["turn"].(float64)
		if row["lifecycle_stage"] != wantStages[turn] || row["visible_turn_index"] != wantIndexes[turn] || row["visible_turn_total"] != float64(6) {
			t.Fatalf("row lifecycle accounting mismatch for turn %.0f: %#v", turn, row)
		}
	}

	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "unresolved", Actor: "agent-mod", CommandID: "cmd_runfix2_unresolved_ready", Payload: map[string]any{"reason": "bounded local closeout terminal evidence"}, Now: fixedRuntime().Now().Add(time.Minute)}); err != nil {
		t.Fatalf("council.unresolved after lifecycle closeout: %v", err)
	}
	status, err = CouncilStatusFromLogAt(sessionDir, metadata, fixedRuntime().Now().Add(time.Minute+time.Second))
	if err != nil {
		t.Fatalf("CouncilStatusFromLogAt after unresolved: %v", err)
	}
	lifecycle = status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if !lifecycle.TerminalVisible || lifecycle.TerminalEventType != "council_unresolved" || lifecycle.VisibleTurnAccounting["terminal_conclusion"] != 1 {
		t.Fatalf("final moderator conclusion should be separate terminal visible evidence: %#v", lifecycle)
	}
}

func TestRUNFIX2CouncilDiscussionLifecycleDoesNotCountUngrantableCloseout(t *testing.T) {
	sessionDir, metadata := runfix2LifecycleCouncilForTest(t, "sess_runfix2_lifecycle_bad_closeout")
	appendRUNFIX2LifecycleOpeningAndDiscussion(t, sessionDir, metadata)
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 3, "agent-1", 30*time.Second)

	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-2", CommandID: "cmd_runfix2_bad_closeout", Payload: map[string]any{"turn": 4, "speech": "no grant closeout"}, Now: fixedRuntime().Now().Add(40 * time.Second)}); err == nil {
		t.Fatalf("closeout speech without same-turn same-member grant must fail closed")
	} else {
		assertStorageIssue(t, err, CategoryCommandConflict)
	}
	appendRawEventForTest(t, sessionDir, metadata, "evt_runfix2_raw_bad_closeout", "cmd_runfix2_raw_bad_closeout", "speech", "discussion", "agent-2", []string{"agent-mod", "agent-1"}, map[string]any{"turn": 4, "speech": "raw ungranted closeout"}, 41*time.Second)

	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_runfix2_propose_after_bad_closeout", Payload: map[string]any{"draft": "still blocked"}, Now: fixedRuntime().Now().Add(50 * time.Second)}); err == nil {
		t.Fatalf("raw ungranted closeout must not satisfy lifecycle completion")
	} else {
		assertStorageIssue(t, err, CategoryCommandConflict)
	}

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if lifecycle.ProposeReady || len(lifecycle.MissingCloseoutMembers) != 1 || lifecycle.MissingCloseoutMembers[0] != "agent-2" {
		t.Fatalf("bad closeout counted toward lifecycle: %#v", lifecycle)
	}
}

func TestRUNFIX2CouncilDiscussionLifecycleDoesNotCountFutureGrantForPastSpeech(t *testing.T) {
	sessionDir, metadata := runfix2LifecycleCouncilForTest(t, "sess_runfix2_lifecycle_future_grant")
	appendRUNFIX2LifecycleOpeningAndDiscussion(t, sessionDir, metadata)
	appendRawEventForTest(t, sessionDir, metadata, "evt_runfix2_raw_closeout_before_grant", "cmd_runfix2_raw_closeout_before_grant", "speech", "discussion", "agent-1", []string{"agent-mod", "agent-2"}, map[string]any{"turn": 3, "speech": "raw closeout before grant"}, 30*time.Second)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_runfix2_future_raise_agent_1", Payload: map[string]any{"turn": 3, "intent": "closeout", "reason": "future grant after raw speech"}, Now: fixedRuntime().Now().Add(30*time.Second + 500*time.Millisecond)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_runfix2_future_grant_agent_1", Payload: map[string]any{"turn": 3, "member": "agent-1", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(31 * time.Second)})
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 4, "agent-2", 40*time.Second)

	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_runfix2_propose_after_future_grant", Payload: map[string]any{"draft": "must still be blocked"}, Now: fixedRuntime().Now().Add(50 * time.Second)}); err == nil {
		t.Fatalf("future grant must not retroactively satisfy an earlier closeout speech")
	} else {
		assertStorageIssue(t, err, CategoryCommandConflict)
	}

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if lifecycle.ProposeReady || len(lifecycle.MissingCloseoutMembers) != 1 || lifecycle.MissingCloseoutMembers[0] != "agent-1" {
		t.Fatalf("future grant retroactively counted toward lifecycle: %#v", lifecycle)
	}
}

func TestLVCOR001DiscussionOnlyLifecycleUsesParameterizedVerdict(t *testing.T) {
	sessionDir, metadata := lvcor001LifecycleCouncilForTest(t, "sess_lvcor001_discussion_only", 15, []string{"agent-1", "agent-2", "agent-3", "agent-4"})
	appendLVCOR001OpeningAndDiscussion(t, sessionDir, metadata, 15)

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if lifecycle.ExpectedVisibleTurns != 21 || lifecycle.DiscussionTurnsRequired != 15 || lifecycle.ParticipantCloseoutsRequired != 4 {
		t.Fatalf("parameterized lifecycle counts mismatch: %#v", lifecycle)
	}
	if lifecycle.DiscussionTurnsPresentCount != 15 || !lifecycle.DiscussionTurnsComplete {
		t.Fatalf("discussion count/complete mismatch: %#v", lifecycle)
	}
	if lifecycle.ParticipantCloseoutsPresentCount != 0 || lifecycle.ParticipantCloseoutsComplete {
		t.Fatalf("closeout count/complete mismatch before closeouts: %#v", lifecycle)
	}
	if !lifecycle.ModeratorOpeningPresent || lifecycle.ModeratorSynthesisPresent {
		t.Fatalf("moderator opening/synthesis mismatch before terminal: %#v", lifecycle)
	}
	if lifecycle.TerminalPhase != "not_terminal" || lifecycle.TerminalVisibleCloseoutProofStatus != "not_terminal" {
		t.Fatalf("terminal phase/proof status before terminal mismatch: %#v", lifecycle)
	}
	if lifecycle.CompletionVerdict != "discussion_complete_closeout_pending" {
		t.Fatalf("completion verdict = %q, want discussion_complete_closeout_pending; lifecycle=%#v", lifecycle.CompletionVerdict, lifecycle)
	}
}

func TestLVCOR001CloseoutsBeforeTerminalRequireModeratorSynthesis(t *testing.T) {
	sessionDir, metadata := lvcor001LifecycleCouncilForTest(t, "sess_lvcor001_closeouts_before_terminal", 15, []string{"agent-1", "agent-2", "agent-3", "agent-4"})
	appendLVCOR001OpeningAndDiscussion(t, sessionDir, metadata, 15)
	appendLVCOR001Closeouts(t, sessionDir, metadata, 16, []string{"agent-1", "agent-2", "agent-3", "agent-4"})

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if lifecycle.ExpectedVisibleTurns != 21 || lifecycle.ParticipantCloseoutsPresentCount != 4 || !lifecycle.ParticipantCloseoutsComplete {
		t.Fatalf("parameterized closeout lifecycle mismatch: %#v", lifecycle)
	}
	if lifecycle.TerminalPhase != "not_terminal" || lifecycle.TerminalVisibleCloseoutProofStatus != "not_terminal" {
		t.Fatalf("terminal phase/proof status before synthesis mismatch: %#v", lifecycle)
	}
	if lifecycle.CompletionVerdict != "participant_closeouts_complete_moderator_synthesis_pending" {
		t.Fatalf("completion verdict = %q, want participant_closeouts_complete_moderator_synthesis_pending; lifecycle=%#v", lifecycle.CompletionVerdict, lifecycle)
	}
}

func TestLVCOR001FiveTwoFixtureProvesNoHardCodedT20(t *testing.T) {
	sessionDir, metadata := lvcor001LifecycleCouncilForTest(t, "sess_lvcor001_five_two", 5, []string{"agent-1", "agent-2"})
	appendLVCOR001OpeningAndDiscussion(t, sessionDir, metadata, 5)
	appendLVCOR001Closeouts(t, sessionDir, metadata, 6, []string{"agent-1", "agent-2"})
	appendRawEventForTest(t, sessionDir, metadata, "evt_lvcor001_five_two_final", "cmd_lvcor001_five_two_final", "council_finalized", "finalized", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"final_summary": "done", "surface_evidence": map[string]any{"status": "posted", "final_message_id": "msg-five-two-final"}}, 90*time.Second)

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if lifecycle.ExpectedVisibleTurns != 9 || lifecycle.VisibleTurnTotal != 9 {
		t.Fatalf("5/2 expected visible turns mismatch: %#v", lifecycle)
	}
	if lifecycle.TerminalPhase != "finalized" || lifecycle.TerminalVisibleCloseoutProofStatus != "posted" || lifecycle.CompletionVerdict != "finalized" {
		t.Fatalf("5/2 terminal verdict mismatch: %#v", lifecycle)
	}
}

func TestLVCOR001FinalizedWithoutVisibleProofFailsClosed(t *testing.T) {
	sessionDir, metadata := lvcor001LifecycleCouncilForTest(t, "sess_lvcor001_finalized_missing_proof", 5, []string{"agent-1", "agent-2"})
	appendLVCOR001OpeningAndDiscussion(t, sessionDir, metadata, 5)
	appendLVCOR001Closeouts(t, sessionDir, metadata, 6, []string{"agent-1", "agent-2"})
	appendRawEventForTest(t, sessionDir, metadata, "evt_lvcor001_final_missing_proof", "cmd_lvcor001_final_missing_proof", "council_finalized", "finalized", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"final_summary": "done"}, 90*time.Second)

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if !lifecycle.ModeratorSynthesisPresent || lifecycle.TerminalEventType != "council_finalized" {
		t.Fatalf("terminal event/synthesis missing: %#v", lifecycle)
	}
	if lifecycle.TerminalPhase != "terminal_blocked_missing_visible_proof" || lifecycle.TerminalVisibleCloseoutProofStatus != "missing" {
		t.Fatalf("terminal phase/proof status should fail closed without visible proof: %#v", lifecycle)
	}
	if lifecycle.CompletionVerdict != "terminal_visible_closeout_proof_missing" {
		t.Fatalf("completion verdict = %q, want terminal_visible_closeout_proof_missing; lifecycle=%#v", lifecycle.CompletionVerdict, lifecycle)
	}
}

func TestLVCOR001FinalizedVisibleProofNonPostedStatesFailClosedWithExactVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		payload     map[string]any
		wantStatus  string
		wantPhase   string
		wantVerdict string
	}{
		{
			name:        "failed",
			payload:     map[string]any{"final_summary": "done", "surface_evidence": map[string]any{"status": "failed", "failure_reason": "discord send failed"}},
			wantStatus:  "failed",
			wantPhase:   "terminal_blocked_visible_proof_failed",
			wantVerdict: "terminal_visible_closeout_proof_failed",
		},
		{
			name:        "pending_followup",
			payload:     map[string]any{"final_summary": "done", "surface_evidence": map[string]any{"status": "pending_followup", "followup_card_id": "card-closeout"}},
			wantStatus:  "pending_followup",
			wantPhase:   "terminal_pending_visible_proof_followup",
			wantVerdict: "terminal_visible_closeout_proof_pending_followup",
		},
		{
			name:        "unproven",
			payload:     map[string]any{"final_summary": "done", "surface_evidence": map[string]any{"status": "posted"}},
			wantStatus:  "unproven",
			wantPhase:   "terminal_blocked_visible_proof_unproven",
			wantVerdict: "terminal_visible_closeout_proof_unproven",
		},
		{
			name:        "thread_mismatch",
			payload:     map[string]any{"final_summary": "done", "surface_evidence": map[string]any{"status": "posted", "final_message_id": "msg-wrong-thread", "thread_id": "thread-other"}, "closeout_diagnostics": []map[string]any{{"code": "exact_thread_mismatch", "stage": "finalized", "reason": "visible closeout thread does not match the configured council thread", "expected_thread_id": "thread-expected", "observed_thread_id": "thread-other"}}},
			wantStatus:  "unproven",
			wantPhase:   "terminal_blocked_visible_proof_unproven",
			wantVerdict: "terminal_visible_closeout_proof_unproven",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir, metadata := lvcor001LifecycleCouncilForTest(t, "sess_lvcor001_"+tc.name, 5, []string{"agent-1", "agent-2"})
			appendLVCOR001OpeningAndDiscussion(t, sessionDir, metadata, 5)
			appendLVCOR001Closeouts(t, sessionDir, metadata, 6, []string{"agent-1", "agent-2"})
			appendRawEventForTest(t, sessionDir, metadata, "evt_lvcor001_"+tc.name, "cmd_lvcor001_"+tc.name, "council_finalized", "finalized", "agent-mod", []string{"agent-1", "agent-2"}, tc.payload, 90*time.Second)

			status, err := CouncilStatusFromLog(sessionDir, metadata)
			if err != nil {
				t.Fatalf("CouncilStatusFromLog: %v", err)
			}
			lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
			if lifecycle.TerminalVisibleCloseoutProofStatus != tc.wantStatus || lifecycle.TerminalPhase != tc.wantPhase || lifecycle.CompletionVerdict != tc.wantVerdict {
				t.Fatalf("proof status/phase/verdict mismatch: %#v", lifecycle)
			}
		})
	}
}

func TestLVCOR001UnresolvedTerminalUsesUnresolvedVerdict(t *testing.T) {
	sessionDir, metadata := lvcor001LifecycleCouncilForTest(t, "sess_lvcor001_unresolved", 5, []string{"agent-1", "agent-2"})
	appendLVCOR001OpeningAndDiscussion(t, sessionDir, metadata, 5)
	appendLVCOR001Closeouts(t, sessionDir, metadata, 6, []string{"agent-1", "agent-2"})
	appendRawEventForTest(t, sessionDir, metadata, "evt_lvcor001_unresolved", "cmd_lvcor001_unresolved", "council_unresolved", "unresolved", "agent-mod", []string{"agent-1", "agent-2"}, map[string]any{"reason": "proof incomplete"}, 90*time.Second)

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
	if lifecycle.TerminalPhase != "terminal_unresolved" || lifecycle.CompletionVerdict != "terminal_unresolved" {
		t.Fatalf("unresolved terminal verdict mismatch: %#v", lifecycle)
	}
}

func TestCOUNCILSTAB001CouncilTerminalEventUpdatesSessionYAMLStatus(t *testing.T) {
	for _, tc := range []struct {
		name       string
		action     string
		payload    map[string]any
		wantPhase  Phase
		wantStatus Status
	}{
		{name: "unresolved", action: "unresolved", payload: map[string]any{"reason": "proof incomplete"}, wantPhase: "unresolved", wantStatus: StatusTerminal},
		{name: "finalized", action: "finalize", payload: map[string]any{"final_summary": "done", "linked_authority_result": map[string]any{"status": "posted", "kanban_comment_id": "kc_done"}}, wantPhase: "finalized", wantStatus: StatusTerminal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir, metadata := runfix2LifecycleCouncilForTest(t, "sess_council_stab_terminal_"+tc.name)
			appendRUNFIX2LifecycleOpeningAndDiscussion(t, sessionDir, metadata)
			if tc.action == "finalize" {
				appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 3, "agent-1", 30*time.Second)
				appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 4, "agent-2", 40*time.Second)
				appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "propose", Actor: "agent-mod", CommandID: "cmd_terminal_" + tc.name + "_propose", Payload: map[string]any{"draft": "done"}, Now: fixedRuntime().Now().Add(50 * time.Second)})
				appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "request-vote", Actor: "agent-mod", CommandID: "cmd_terminal_" + tc.name + "_request_vote", Payload: map[string]any{"draft_version": 1}, Now: fixedRuntime().Now().Add(51 * time.Second)})
				appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-1", CommandID: "cmd_terminal_" + tc.name + "_vote_1", Payload: map[string]any{"draft_version": 1, "vote": "approve", "reason": "ok"}, Now: fixedRuntime().Now().Add(52 * time.Second)})
				appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "vote", Actor: "agent-2", CommandID: "cmd_terminal_" + tc.name + "_vote_2", Payload: map[string]any{"draft_version": 1, "vote": "approve", "reason": "ok"}, Now: fixedRuntime().Now().Add(53 * time.Second)})
			}
			appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: tc.action, Actor: "agent-mod", CommandID: "cmd_terminal_" + tc.name, Payload: tc.payload, Now: fixedRuntime().Now().Add(60 * time.Second)})

			reloaded, err := LoadSessionYAML(sessionDir)
			if err != nil {
				t.Fatalf("LoadSessionYAML: %v", err)
			}
			if reloaded.Status != tc.wantStatus || reloaded.State.Phase != tc.wantPhase {
				t.Fatalf("session.yaml status/phase = %s/%s, want %s/%s", reloaded.Status, reloaded.State.Phase, tc.wantStatus, tc.wantPhase)
			}
			status, err := CouncilStatusFromLog(sessionDir, reloaded)
			if err != nil {
				t.Fatalf("CouncilStatusFromLog: %v", err)
			}
			lifecycle := status["discussion_lifecycle"].(CouncilDiscussionLifecycle)
			if lifecycle.TerminalEventType == "" {
				t.Fatalf("terminal event missing from status lifecycle: %#v", lifecycle)
			}

			appendRawEventForTest(t, sessionDir, reloaded, "evt_terminal_"+tc.name+"_post_terminal_diagnostic", "cmd_terminal_"+tc.name+"_post_terminal_diagnostic", "diagnostic", "discussion", "agent-mod", []string{"agent-1"}, map[string]any{"message": "post-terminal diagnostic must not reopen session metadata"}, 70*time.Second)
			afterDiagnostic, err := LoadSessionYAML(sessionDir)
			if err != nil {
				t.Fatalf("LoadSessionYAML after diagnostic: %v", err)
			}
			if afterDiagnostic.Status != tc.wantStatus || afterDiagnostic.State.Phase != tc.wantPhase {
				t.Fatalf("post-terminal diagnostic reopened session.yaml status/phase = %s/%s, want %s/%s", afterDiagnostic.Status, afterDiagnostic.State.Phase, tc.wantStatus, tc.wantPhase)
			}
		})
	}
}

func lvcor001LoadedCouncilRegistry(t *testing.T, members []string) (string, *registry.LoadedRegistry) {
	t.Helper()
	dataHome := t.TempDir()
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	var b strings.Builder
	b.WriteString(`schema_version: 1
members:
  agent-mod:
    display_name: Moderator
    wrapper: missing-agent-mod-wrapper
    workspace: /tmp/agent-mod
    role: moderator
    enabled: false
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
`)
	for _, member := range members {
		fmt.Fprintf(&b, "  %s:\n    display_name: %s\n    wrapper: missing-%s-wrapper\n    workspace: /tmp/%s\n    role: council_member\n    enabled: false\n    adapter_kind: hermes-agent\n    runtime_kind: hermes-cli-stream\n", member, member, member, member)
	}
	if err := os.WriteFile(registry.RegistryPath(dataHome), []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	loaded, err := registry.Load(dataHome, fixedRuntime())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return dataHome, loaded
}

func lvcor001LifecycleCouncilForTest(t *testing.T, sessionID string, maxDiscussionTurns int, members []string) (string, *SessionMetadata) {
	return lvcor001LifecycleCouncilForTestWithSurface(t, sessionID, maxDiscussionTurns, members, nil)
}

func lvcor001LifecycleCouncilForTestWithSurface(t *testing.T, sessionID string, maxDiscussionTurns int, members []string, surface *Surface) (string, *SessionMetadata) {
	t.Helper()
	dataHome, loaded := lvcor001LoadedCouncilRegistry(t, members)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        sessionID,
			Title:     "LVCOR-001 lifecycle",
			Moderator: "agent-mod",
			Surface:   surface,
			EventID:   "evt_" + sessionID + "_created",
			CommandID: "cmd_" + sessionID + "_new",
			Limits:    Limits{MaxDiscussionTurns: maxDiscussionTurns},
		},
		Members: members,
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, _ := SessionDir(dataHome, metadata.ID)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "prepare", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_prepare", Payload: map[string]any{"timeout_sec": 60}, Now: fixedRuntime().Now().Add(time.Second)})
	return sessionDir, metadata
}

func appendLVCOR001OpeningAndDiscussion(t *testing.T, sessionDir string, metadata *SessionMetadata, maxDiscussionTurns int) {
	t.Helper()
	members := councilMembers(metadata)
	if len(members) == 0 {
		t.Fatalf("LVCOR-001 test requires members")
	}
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_" + metadata.ID + "_poll_1", Payload: map[string]any{"turn": 1}, Now: fixedRuntime().Now().Add(2 * time.Second)})
	for turn := 1; turn <= maxDiscussionTurns; turn++ {
		member := members[(turn-1)%len(members)]
		appendLVCOR001SpeechTurn(t, sessionDir, metadata, turn, member, time.Duration(10+turn*2)*time.Second)
	}
}

func appendLVCOR001Closeouts(t *testing.T, sessionDir string, metadata *SessionMetadata, firstTurn int, members []string) {
	t.Helper()
	for idx, member := range members {
		appendLVCOR001SpeechTurn(t, sessionDir, metadata, firstTurn+idx, member, time.Duration(60+idx*2)*time.Second)
	}
}

func appendLVCOR001SpeechTurn(t *testing.T, sessionDir string, metadata *SessionMetadata, turn int, member string, delta time.Duration) {
	t.Helper()
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: member, CommandID: fmt.Sprintf("cmd_%s_raise_%s_%d", metadata.ID, member, turn), Payload: map[string]any{"turn": turn, "intent": "discussion", "reason": "selected lifecycle turn"}, Now: fixedRuntime().Now().Add(delta - time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: fmt.Sprintf("cmd_%s_grant_%s_%d", metadata.ID, member, turn), Payload: map[string]any{"turn": turn, "member": member, "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(delta)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: member, CommandID: fmt.Sprintf("cmd_%s_speak_%s_%d", metadata.ID, member, turn), Payload: map[string]any{"turn": turn, "speech": "turn speech"}, Now: fixedRuntime().Now().Add(delta + time.Second)})
}

func runfix2LifecycleCouncilForTest(t *testing.T, sessionID string) (string, *SessionMetadata) {
	t.Helper()
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        sessionID,
			Title:     "RUNFIX2 lifecycle",
			Moderator: "agent-mod",
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
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "prepare", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_prepare", Payload: map[string]any{"timeout_sec": 60}, Now: fixedRuntime().Now().Add(time.Second)})
	return sessionDir, metadata
}

func appendRUNFIX2LifecycleOpeningAndDiscussion(t *testing.T, sessionDir string, metadata *SessionMetadata) {
	t.Helper()
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_" + metadata.ID + "_poll_1", Payload: map[string]any{"turn": 1}, Now: fixedRuntime().Now().Add(2 * time.Second)})
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 1, "agent-1", 10*time.Second)
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_" + metadata.ID + "_poll_2", Payload: map[string]any{"turn": 2}, Now: fixedRuntime().Now().Add(20 * time.Second)})
	appendRUNFIX2LifecycleCloseout(t, sessionDir, metadata, 2, "agent-2", 21*time.Second)
}

func appendRUNFIX2LifecycleCloseout(t *testing.T, sessionDir string, metadata *SessionMetadata, turn int, member string, delta time.Duration) {
	t.Helper()
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: member, CommandID: fmt.Sprintf("cmd_%s_raise_%s_%d", metadata.ID, member, turn), Payload: map[string]any{"turn": turn, "intent": "closeout", "reason": "selected closeout turn"}, Now: fixedRuntime().Now().Add(delta - time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: fmt.Sprintf("cmd_%s_grant_%s_%d", metadata.ID, member, turn), Payload: map[string]any{"turn": turn, "member": member, "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(delta)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: member, CommandID: fmt.Sprintf("cmd_%s_speak_%s_%d", metadata.ID, member, turn), Payload: map[string]any{"turn": turn, "speech": "turn speech"}, Now: fixedRuntime().Now().Add(delta + time.Second)})
}

func runfix2BuildManifest(t *testing.T, sessionDir string, metadata *SessionMetadata) map[string]any {
	t.Helper()
	result, err := BuildExportBundle(sessionDir, metadata, ExportBundleOptions{})
	if err != nil {
		t.Fatalf("BuildExportBundle: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(result.BundleDir, "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal bundle_manifest.json: %v\n%s", err, string(manifestBytes))
	}
	return manifest
}
