package daemon_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atn-control/internal/daemon"
	"atn-control/internal/protocol"
	"atn-control/internal/runner"
	"atn-control/internal/storage"
)

func TestPRSLR011DialogueQualityE2EAcceptanceProof(t *testing.T) {
	dataHome, metadata, sessionDir := createPRSLR011DialogueCouncil(t)
	adapter := &fakeRunRTAdapter{results: []fakeRunRTResult{
		{result: runner.Result{
			OK:                true,
			SemanticEventType: "speech",
			SemanticStatus:    "succeeded",
			Payload: map[string]any{
				"turn":              1,
				"speech":            "Agent 1 opens the PRSLR-011 local dialogue-quality proof with a bounded control claim.",
				"claims":            []any{map[string]any{"claim_id": "T1.C1", "summary": "PRSLR-011 acceptance must be proven by local control evidence before any live claim.", "kind": "requirement"}},
				"contribution_type": "new_axis",
				"new_axis_reason":   "opening proof axis for the bounded PRSLR-011 fixture",
				"surface_evidence": map[string]any{
					"status":            "posted",
					"kind":              "discord_thread",
					"platform":          "discord",
					"channel_id":        "chan-prslr011-local",
					"thread_id":         "thread-prslr011-local",
					"message_id":        "msg-prslr011-turn-1-local",
					"posting_path":      "selected_member_profile_send",
					"sender_member":     "agent-1",
					"local_default_off": true,
				},
			},
			Cost: &runner.Cost{TokensIn: 11, TokensOut: 17, USDEstimate: 0.03, Source: runner.HermesAgentCostSource},
		}},
		{result: runner.Result{
			OK:                true,
			SemanticEventType: "speech",
			SemanticStatus:    "succeeded",
			Payload: map[string]any{
				"turn":   2,
				"speech": "Agent 2 supports Agent 1's bounded proof requirement and adds projection evidence coverage.",
				"claims": []any{map[string]any{"claim_id": "T2.C1", "summary": "Projection, transcript, and export rows must preserve the same selected-runner and ARGUE evidence.", "kind": "evidence"}},
				"stance_links": []any{map[string]any{
					"target_event_id": "evt_pending_first_speech",
					"target_claim_id": "T1.C1",
					"stance":          "support",
					"rationale":       "Agent 2 directly supports the prior speaker's bounded-proof requirement.",
				}},
				"contribution_type": "support",
				"surface_evidence": map[string]any{
					"status":            "posted",
					"kind":              "discord_thread",
					"platform":          "discord",
					"channel_id":        "chan-prslr011-local",
					"thread_id":         "thread-prslr011-local",
					"message_id":        "msg-prslr011-turn-2-local",
					"posting_path":      "selected_member_profile_send",
					"sender_member":     "agent-2",
					"local_default_off": true,
				},
			},
			Cost: &runner.Cost{TokensIn: 13, TokensOut: 19, USDEstimate: 0.04, Source: runner.HermesAgentCostSource},
		}},
	}}
	server := daemon.NewServer(dataHome, daemonFixedRuntime())
	server.RunnerAdapter = adapter
	server.DispatchLocks = &daemon.DispatchLocks{}

	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_prslr011_poll_1", map[string]any{"turn": 1}, 7*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-1", "cmd_prslr011_raise_1", map[string]any{"turn": 1, "intent": "open", "reason": "open the bounded proof axis"}, 8*time.Second)
	runGrant(t, server, metadata.ID, "cmd_prslr011_grant_1", "agent-1", 1)
	indexAfterFirst, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex after first grant: %v", err)
	}
	firstSpeechEventID := lastEventOfType(t, indexAfterFirst.Events, "speech").EventID
	adapter.results[1].result.Payload["stance_links"] = []any{map[string]any{
		"target_event_id": firstSpeechEventID,
		"target_claim_id": "T1.C1",
		"stance":          "support",
		"rationale":       "Agent 2 directly supports the prior speaker's bounded-proof requirement.",
	}}

	appendCouncilEventForDispatch(t, sessionDir, metadata, "poll", "agent-mod", "cmd_prslr011_poll_2", map[string]any{"turn": 2}, 9*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "hand-raise", "agent-2", "cmd_prslr011_raise_2", map[string]any{
		"turn":   2,
		"intent": "support",
		"reason": "support prior speaker with structured ARGUE evidence",
		"target_links": []any{map[string]any{
			"target_event_id": firstSpeechEventID,
			"target_claim_id": "T1.C1",
			"stance":          "support",
		}},
	}, 10*time.Second)
	runGrant(t, server, metadata.ID, "cmd_prslr011_grant_2", "agent-2", 2)
	indexAfterSecond, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex after second grant: %v", err)
	}
	secondSpeechEventID := lastEventOfType(t, indexAfterSecond.Events, "speech").EventID

	appendCouncilEventForDispatch(t, sessionDir, metadata, "propose", "agent-mod", "cmd_prslr011_synthesis", map[string]any{"draft": "Moderator synthesis: Agent 1 established the bounded local proof requirement; Agent 2 supported it with projection/export coverage. The fixture remains local/default-off and does not claim live Discord readiness."}, 12*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-vote", "agent-mod", "cmd_prslr011_vote_request", map[string]any{"timeout_sec": 30}, 13*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "vote", "agent-1", "cmd_prslr011_vote_agent_1", map[string]any{"vote": "approve", "reason": "Runner and ARGUE proof are present."}, 14*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "vote", "agent-2", "cmd_prslr011_vote_agent_2", map[string]any{"vote": "approve", "reason": "Projection and export proof are present."}, 15*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "finalize", "agent-mod", "cmd_prslr011_finalize", map[string]any{
		"final_summary": "PRSLR-011 local/default-off dialogue-quality fixture accepted: selected-runner success, canonical speech linkage, prior-speaker ARGUE coverage, moderator synthesis, transcript/projection/export evidence, and non-live boundaries are preserved.",
		"surface_evidence": map[string]any{
			"status":            "posted",
			"kind":              "discord_thread",
			"platform":          "discord",
			"channel_id":        "chan-prslr011-local",
			"thread_id":         "thread-prslr011-local",
			"final_message_id":  "msg-prslr011-final-local",
			"local_default_off": true,
		},
		"linked_authority_result": map[string]any{"status": "posted", "kanban_card_id": "t_prslr011", "kanban_comment_id": "comment-prslr011-local", "evidence": map[string]any{"source": "control-local-e2e"}},
	}, 16*time.Second)

	if adapter.calls != 2 || len(adapter.reqs) != 2 || adapter.reqs[0].Member.ID != "agent-1" || adapter.reqs[1].Member.ID != "agent-2" {
		t.Fatalf("PRSLR-011 must invoke selected participants exactly once each, calls=%d reqs=%#v", adapter.calls, adapter.reqs)
	}
	index, err := storage.ReadLogIndex(sessionDir, metadata)
	if err != nil {
		t.Fatalf("ReadLogIndex: %v", err)
	}
	if eventTypeCount(index.Events, "speaker_selected") != 2 || eventTypeCount(index.Events, "runner_invocation_succeeded") != 2 || eventTypeCount(index.Events, "speech") != 2 {
		t.Fatalf("PRSLR-011 selected-runner/speech counts mismatch: %#v", index.Events)
	}
	for _, speechID := range []string{firstSpeechEventID, secondSpeechEventID} {
		speech := eventByIDForPRSLR011(t, index.Events, speechID)
		if speech.Runner == nil || speech.Runner.InvocationID == "" || speech.CausationEventID == "" {
			t.Fatalf("PRSLR-011 canonical speech lacks runner/causation evidence: %#v", speech)
		}
	}

	status, err := storage.CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	selectedAccounting := status["selected_runner_accounting"].(storage.SelectedRunnerAccounting)
	if !selectedAccounting.SelectedRunnerPass || selectedAccounting.LinkedRunnerSpeechCount != 2 || selectedAccounting.RunnerSucceededCount != 2 || selectedAccounting.RunnerlessSpeechCount != 0 {
		t.Fatalf("PRSLR-011 selected-runner accounting must pass with two linked speeches and no runnerless repair: %#v", selectedAccounting)
	}
	quality := status["discussion_quality"].(map[string]any)
	priorEvidence := quality["prior_speaker_argue_quality_evidence"].(map[string]any)
	if quality["mode"] != "quality_required" || quality["discussion_quality_pass"] != true || priorEvidence["pass"] != true {
		t.Fatalf("PRSLR-011 ARGUE quality must pass from explicit prior-speaker relation evidence: quality=%#v prior=%#v", quality, priorEvidence)
	}
	if quality["orphan_speech_count"] != 0 || quality["linked_speech_count"] != 1 || quality["target_link_count"] != 1 {
		t.Fatalf("PRSLR-011 quality counts should show one linked non-opening speech and no orphan speeches: %#v", quality)
	}

	transcript, err := storage.RenderTranscript(sessionDir, metadata, storage.TranscriptMarkdownFormat)
	if err != nil {
		t.Fatalf("RenderTranscript: %v", err)
	}
	for _, want := range []string{"Runner And Cost Summary", "runner_calls_total: `2`", "Argument Graph Projection Summary", "selected_runner_pass: `true`", "PRSLR-011 local/default-off dialogue-quality fixture accepted", "msg-prslr011-turn-1-local", "msg-prslr011-turn-2-local"} {
		if !strings.Contains(string(transcript), want) {
			t.Fatalf("PRSLR-011 transcript missing %q:\n%s", want, string(transcript))
		}
	}

	export := server.Handle(protocol.NewRequest("cmd_prslr011_export", "export.bundle", map[string]any{"session_id": metadata.ID}))
	if !export.OK {
		t.Fatalf("export.bundle failed: %+v", export)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(fmt.Sprint(export.Result["bundle_dir"]), "bundle_manifest.json"))
	if err != nil {
		t.Fatalf("read bundle_manifest.json: %v", err)
	}
	for _, want := range []string{"selected_runner_accounting", "surface_delivery_projection", "argument_graph_projection", "prior_speaker_argue_quality_evidence", "msg-prslr011-final-local"} {
		if !strings.Contains(string(manifestBytes), want) {
			t.Fatalf("PRSLR-011 export manifest missing %q:\n%s", want, string(manifestBytes))
		}
	}

	report, err := storage.RebuildProjection(dataHome, storage.ProjectionOptions{Runtime: daemonFixedRuntime()})
	if err != nil {
		t.Fatalf("RebuildProjection: %v", err)
	}
	db := openRUNFIX009ProjectionDB(t, report.DBPath)
	defer func() { _ = db.Close() }()
	if got := runfix009ScalarText(t, db, `SELECT status FROM runner_invocations WHERE session_id = '`+metadata.ID+`' AND member = 'agent-1'`); got != "succeeded" {
		t.Fatalf("agent-1 runner projection status=%q want succeeded", got)
	}
	if got := runfix009ScalarText(t, db, `SELECT status FROM runner_invocations WHERE session_id = '`+metadata.ID+`' AND member = 'agent-2'`); got != "succeeded" {
		t.Fatalf("agent-2 runner projection status=%q want succeeded", got)
	}
	if got := runfix009ScalarText(t, db, `SELECT stance_links_json FROM council_argument_graph_projection WHERE event_id = '`+secondSpeechEventID+`'`); !strings.Contains(got, firstSpeechEventID) || !strings.Contains(got, "support") {
		t.Fatalf("PRSLR-011 ARGUE projection missing prior-speaker support link: %s", got)
	}
}

func createPRSLR011DialogueCouncil(t *testing.T) (string, *storage.SessionMetadata, string) {
	t.Helper()
	dataHome, loaded, _ := dispatchDataHomeWithMembers(t, "agent-1", "agent-2")
	metadata, _, _, err := storage.CreateCouncil(dataHome, loaded, storage.CouncilStartSpec{
		Session: storage.SessionSpec{
			ID:        "sess_prslr011_dialogue_quality_e2e",
			Title:     "PRSLR-011 dialogue quality E2E",
			Moderator: "agent-mod",
			Surface: &storage.Surface{
				Kind:     "local_daemon_only",
				Platform: "local",
			},
			LinkedAuthority: &storage.LinkedAuthority{KanbanCardID: "t_prslr011"},
			Limits: storage.Limits{
				PreparationTimeoutSec:    30,
				StreamStaleThresholdSec:  90,
				StreamRepollThresholdSec: 300,
				Council: storage.CouncilLimits{DiscussionQuality: &storage.DiscussionQualityLimits{
					Mode:                           "quality_required",
					OpeningUnlinkedTurns:           1,
					RequireClaims:                  true,
					RequireStanceLinksAfterOpening: true,
					AllowNewAxisWithReason:         true,
					MaxConsecutiveNewAxis:          1,
				}},
			},
			EventID:   "evt_prslr011_created",
			CommandID: "cmd_prslr011_created",
		},
		Members: []string{"agent-1", "agent-2"},
		Now:     daemonFixedRuntime().Now(),
	}, daemonFixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, err := storage.SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendCouncilEventForDispatch(t, sessionDir, metadata, "request-attendance", "agent-mod", "cmd_prslr011_attendance", map[string]any{"timeout_sec": 30}, time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-1", "cmd_prslr011_attend_agent_1", map[string]any{"status": "present", "summary": "ready"}, 2*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "attend", "agent-2", "cmd_prslr011_attend_agent_2", map[string]any{"status": "present", "summary": "ready"}, 2500*time.Millisecond)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "lock-agenda", "agent-mod", "cmd_prslr011_agenda", map[string]any{"decision_question": "What proves PRSLR-011 locally?", "success_criteria": "Selected-runner success plus linked canonical speech, prior-speaker ARGUE coverage, and transcript/projection/export evidence.", "out_of_scope_policy": "Do not claim live Discord delivery, production readiness, or fallback/manual repair."}, 3*time.Second)
	appendRuntimeEvidenceForRUNFIX011(t, sessionDir, metadata, "agent-1", 3500*time.Millisecond)
	appendRuntimeEvidenceForRUNFIX011(t, sessionDir, metadata, "agent-2", 3600*time.Millisecond)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "prepare", "agent-mod", "cmd_prslr011_prepare", map[string]any{"timeout_sec": 30}, 4*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "ready", "agent-1", "cmd_prslr011_ready_agent_1", map[string]any{"summary": "ready"}, 5*time.Second)
	appendCouncilEventForDispatch(t, sessionDir, metadata, "ready", "agent-2", "cmd_prslr011_ready_agent_2", map[string]any{"summary": "ready"}, 5500*time.Millisecond)
	return dataHome, metadata, sessionDir
}

func runGrant(t *testing.T, server *daemon.Server, sessionID, commandID, member string, turn int) {
	t.Helper()
	response := server.Handle(councilEventRequest(sessionID, "council.grant", commandID, map[string]any{"turn": turn, "member": member, "selection_mode": "moderator_direct"}))
	if !response.OK {
		t.Fatalf("council.grant %s failed: %+v", commandID, response)
	}
}

func eventByIDForPRSLR011(t *testing.T, events []storage.EventEnvelope, eventID string) storage.EventEnvelope {
	t.Helper()
	for _, event := range events {
		if event.EventID == eventID {
			return event
		}
	}
	t.Fatalf("missing event_id %s in %#v", eventID, events)
	return storage.EventEnvelope{}
}
