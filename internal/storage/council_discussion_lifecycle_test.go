package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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
