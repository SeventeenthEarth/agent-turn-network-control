package storage

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPRSLR009PriorSpeakerQualityEvidenceRejectsFirstNonOpeningOrphan(t *testing.T) {
	sessionDir, metadata := prslr009QualityCouncilForTest(t, "sess_prslr009_orphan")
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr009_moderator_open", "cmd_prslr009_moderator_open", "speech", "discussion", "agent-mod", []string{"agent-1"}, map[string]any{
		"turn":              0,
		"speech":            "Moderator opening claim.",
		"claims":            []any{map[string]any{"claim_id": "M0.C1", "summary": "Moderator seeded the prior axis."}},
		"contribution_type": "new_axis",
		"new_axis_reason":   "opening frame",
	}, 2*time.Second)

	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: "cmd_prslr009_orphan_poll_1", Payload: map[string]any{"turn": 1}, Now: fixedRuntime().Now().Add(3 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: "cmd_prslr009_orphan_raise_1", Payload: map[string]any{"turn": 1, "intent": "support", "reason": "legacy hint only"}, Now: fixedRuntime().Now().Add(4 * time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: "cmd_prslr009_orphan_grant_1", Payload: map[string]any{"turn": 1, "member": "agent-1", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(5 * time.Second)})
	if _, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-1", CommandID: "cmd_prslr009_orphan_speak_1", Payload: map[string]any{
		"turn":                 1,
		"speech":               "I only rely on the legacy display hint.",
		"claims":               []any{map[string]any{"claim_id": "T1.C1", "summary": "Display hints are not ARGUE authority."}},
		"responds_to_event_id": "evt_prslr009_moderator_open",
		"contribution_type":    "support",
	}, Now: fixedRuntime().Now().Add(6 * time.Second)}); err == nil {
		t.Fatalf("quality_required must reject first non-opening participant orphan even when responds_to_event_id is present")
	} else if !strings.Contains(err.Error(), "orphan") && !strings.Contains(err.Error(), "stance_links") {
		t.Fatalf("quality_required orphan rejection should cite stance_links/orphan: %v", err)
	}

	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr009_raw_orphan", "cmd_prslr009_raw_orphan", "speech", "discussion", "agent-1", []string{"agent-mod"}, map[string]any{
		"turn":                 1,
		"speech":               "Raw legacy hint must remain diagnostic-only.",
		"claims":               []any{map[string]any{"claim_id": "T1.C1", "summary": "Raw orphan should fail quality status."}},
		"responds_to_event_id": "evt_prslr009_moderator_open",
		"contribution_type":    "support",
	}, 7*time.Second)

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	quality := status["discussion_quality"].(map[string]any)
	if quality["discussion_quality_pass"] != false {
		t.Fatalf("raw first non-opening orphan must fail discussion quality: %#v", quality)
	}
	evidence, ok := quality["prior_speaker_argue_quality_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("missing prior_speaker_argue_quality_evidence: %#v", quality)
	}
	if evidence["pass"] != false || evidence["first_orphan_event_id"] != "evt_prslr009_raw_orphan" {
		t.Fatalf("unexpected prior speaker evidence: %#v", evidence)
	}
	if evidence["display_hint_authority"] != "rejected" {
		t.Fatalf("display hints must be explicitly rejected as authority: %#v", evidence)
	}
}

func TestPRSLR009PriorSpeakerQualityEvidenceAcceptsStructuredPriorClaimLinkAndNewAxis(t *testing.T) {
	sessionDir, metadata := prslr009QualityCouncilForTest(t, "sess_prslr009_linked")
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr009_open", "cmd_prslr009_open", "speech", "discussion", "agent-mod", []string{"agent-1"}, map[string]any{
		"turn":              0,
		"speech":            "Moderator opening claim.",
		"claims":            []any{map[string]any{"claim_id": "M0.C1", "summary": "Opening claim for participant linkage."}},
		"contribution_type": "new_axis",
		"new_axis_reason":   "opening frame",
	}, 2*time.Second)
	prslr009SpeakTurn(t, sessionDir, metadata, 1, "agent-1", map[string]any{
		"turn":   1,
		"speech": "I support the prior claim with structured ARGUE evidence.",
		"claims": []any{map[string]any{"claim_id": "T1.C1", "summary": "Structured stance link is authoritative."}},
		"stance_links": []any{map[string]any{
			"target_event_id": "evt_prslr009_open",
			"target_claim_id": "M0.C1",
			"stance":          "support",
			"rationale":       "Links to the moderator's prior claim explicitly.",
		}},
		"responds_to_event_id": "evt_prslr009_open",
		"contribution_type":    "support",
	}, 3*time.Second)
	prslr009SpeakTurn(t, sessionDir, metadata, 2, "agent-1", map[string]any{
		"turn":              2,
		"speech":            "I introduce a new explicit decision axis.",
		"claims":            []any{map[string]any{"claim_id": "T2.C1", "summary": "A justified new axis is explicit."}},
		"contribution_type": "new_axis",
		"new_axis_reason":   "The prior thread lacks an implementation diagnostics axis.",
	}, 10*time.Second)

	status, err := CouncilStatusFromLog(sessionDir, metadata)
	if err != nil {
		t.Fatalf("CouncilStatusFromLog: %v", err)
	}
	quality := status["discussion_quality"].(map[string]any)
	evidence := quality["prior_speaker_argue_quality_evidence"].(map[string]any)
	if evidence["pass"] != true || quality["discussion_quality_pass"] != true {
		t.Fatalf("valid prior link plus justified new_axis should pass: quality=%#v evidence=%#v", quality, evidence)
	}
	if got := evidence["valid_prior_target_count"]; got != 1 {
		t.Fatalf("valid prior target count=%#v want 1 evidence=%#v", got, evidence)
	}
	if got := evidence["new_axis_exception_count"]; got != 1 {
		t.Fatalf("new axis exception count=%#v want 1 evidence=%#v", got, evidence)
	}
}

func TestPRSLR009QualityRequiredRejectsInvalidPriorTargets(t *testing.T) {
	sessionDir, metadata := prslr009QualityCouncilForTest(t, "sess_prslr009_invalid_targets")
	appendRawEventForTest(t, sessionDir, metadata, "evt_prslr009_invalid_open", "cmd_prslr009_invalid_open", "speech", "discussion", "agent-mod", []string{"agent-1"}, map[string]any{
		"turn":   0,
		"speech": "Opening without the requested target claim.",
		"claims": []any{map[string]any{"claim_id": "M0.C1", "summary": "Only this claim exists."}},
	}, 2*time.Second)

	cases := []struct {
		name string
		link map[string]any
	}{
		{name: "missing_event", link: map[string]any{"target_event_id": "evt_missing", "target_claim_id": "M0.C1", "stance": "support", "rationale": "missing event"}},
		{name: "not_speech", link: map[string]any{"target_event_id": "evt_" + metadata.ID + "_created", "stance": "support", "rationale": "session_created is not speech"}},
		{name: "missing_claim", link: map[string]any{"target_event_id": "evt_prslr009_invalid_open", "target_claim_id": "M0.MISSING", "stance": "support", "rationale": "missing claim"}},
	}
	for idx, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			turn := idx + 1
			appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: fmt.Sprintf("cmd_prslr009_invalid_poll_%d", turn), Payload: map[string]any{"turn": turn}, Now: fixedRuntime().Now().Add(time.Duration(10+idx*10) * time.Second)})
			appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: "agent-1", CommandID: fmt.Sprintf("cmd_prslr009_invalid_raise_%d", turn), Payload: map[string]any{"turn": turn, "intent": "support", "reason": "invalid target test"}, Now: fixedRuntime().Now().Add(time.Duration(11+idx*10) * time.Second)})
			appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: fmt.Sprintf("cmd_prslr009_invalid_grant_%d", turn), Payload: map[string]any{"turn": turn, "member": "agent-1", "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(time.Duration(12+idx*10) * time.Second)})
			_, _, err := RecordCouncilEvent(sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: "agent-1", CommandID: fmt.Sprintf("cmd_prslr009_invalid_speak_%d", turn), Payload: map[string]any{
				"turn":         turn,
				"speech":       "Invalid stance link must fail closed.",
				"claims":       []any{map[string]any{"claim_id": fmt.Sprintf("T%d.C1", turn), "summary": "Invalid target should reject."}},
				"stance_links": []any{tc.link},
			}, Now: fixedRuntime().Now().Add(time.Duration(13+idx*10) * time.Second)})
			if err == nil {
				t.Fatalf("quality_required must reject invalid target %s", tc.name)
			}
		})
	}
}

func prslr009QualityCouncilForTest(t *testing.T, sessionID string) (string, *SessionMetadata) {
	t.Helper()
	dataHome, loaded := loadedCouncilRegistry(t)
	metadata, _, _, err := CreateCouncil(dataHome, loaded, CouncilStartSpec{
		Session: SessionSpec{
			ID:        sessionID,
			Title:     "PRSLR-009 quality",
			Moderator: "agent-mod",
			EventID:   "evt_" + sessionID + "_created",
			CommandID: "cmd_" + sessionID + "_new",
			Limits: Limits{Council: CouncilLimits{DiscussionQuality: &DiscussionQualityLimits{
				Mode:                           "quality_required",
				OpeningUnlinkedTurns:           1,
				RequireClaims:                  true,
				RequireStanceLinksAfterOpening: true,
				AllowNewAxisWithReason:         true,
				MaxConsecutiveNewAxis:          2,
			}}},
		},
		Members: []string{"agent-1"},
		Now:     fixedRuntime().Now(),
	}, fixedRuntime())
	if err != nil {
		t.Fatalf("CreateCouncil: %v", err)
	}
	sessionDir, err := SessionDir(dataHome, metadata.ID)
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "prepare", Actor: "agent-mod", CommandID: "cmd_" + sessionID + "_prepare", Payload: map[string]any{"timeout_sec": 60}, Now: fixedRuntime().Now().Add(time.Second)})
	return sessionDir, metadata
}

func prslr009SpeakTurn(t *testing.T, sessionDir string, metadata *SessionMetadata, turn int, member string, payload map[string]any, delta time.Duration) {
	t.Helper()
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "poll", Actor: "agent-mod", CommandID: fmt.Sprintf("cmd_%s_poll_%d", metadata.ID, turn), Payload: map[string]any{"turn": turn}, Now: fixedRuntime().Now().Add(delta)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "hand-raise", Actor: member, CommandID: fmt.Sprintf("cmd_%s_raise_%d", metadata.ID, turn), Payload: map[string]any{"turn": turn, "intent": "support", "reason": "structured ARGUE test"}, Now: fixedRuntime().Now().Add(delta + time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "grant", Actor: "agent-mod", CommandID: fmt.Sprintf("cmd_%s_grant_%d", metadata.ID, turn), Payload: map[string]any{"turn": turn, "member": member, "selection_mode": "moderator_direct"}, Now: fixedRuntime().Now().Add(delta + 2*time.Second)})
	appendCouncilForTest(t, sessionDir, metadata, CouncilEventSpec{Action: "speak", Actor: member, CommandID: fmt.Sprintf("cmd_%s_speak_%d", metadata.ID, turn), Payload: payload, Now: fixedRuntime().Now().Add(delta + 3*time.Second)})
}
