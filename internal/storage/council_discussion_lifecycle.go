package storage

import (
	"fmt"
	"sort"
	"strings"
)

type CouncilDiscussionLifecycle struct {
	Configured                            bool              `json:"configured"`
	MaxDiscussionTurns                    int               `json:"max_discussion_turns,omitempty"`
	LegacyMaxTurnsDisplay                 int               `json:"legacy_max_turns_display,omitempty"`
	ParticipantCount                      int               `json:"participant_count"`
	ExpectedVisibleTurns                  int               `json:"expected_visible_turns,omitempty"`
	TerminalSynthesisTurn                 int               `json:"terminal_synthesis_turn,omitempty"`
	TerminalSynthesisExpectedVisibleIndex int               `json:"terminal_synthesis_expected_visible_index,omitempty"`
	OpeningEventID                        string            `json:"opening_event_id,omitempty"`
	OpeningTurn                           int               `json:"opening_turn,omitempty"`
	DiscussionTurnsPresent                []int             `json:"discussion_turns_present,omitempty"`
	MissingDiscussionTurns                []int             `json:"missing_discussion_turns,omitempty"`
	CloseoutMembersPresent                []string          `json:"closeout_members_present,omitempty"`
	MissingCloseoutMembers                []string          `json:"missing_closeout_members,omitempty"`
	DiscussionTurnsRequired               int               `json:"discussion_turns_required,omitempty"`
	DiscussionTurnsPresentCount           int               `json:"discussion_turns_present_count"`
	DiscussionTurnsComplete               bool              `json:"discussion_turns_complete"`
	ParticipantCloseoutsRequired          int               `json:"participant_closeouts_required,omitempty"`
	ParticipantCloseoutsPresentCount      int               `json:"participant_closeouts_present_count"`
	ParticipantCloseoutsComplete          bool              `json:"participant_closeouts_complete"`
	ModeratorOpeningPresent               bool              `json:"moderator_opening_present"`
	ModeratorSynthesisPresent             bool              `json:"moderator_synthesis_present"`
	TerminalSynthesisSummaryPresent       bool              `json:"terminal_synthesis_summary_present"`
	TerminalVisibleCloseoutProofStatus    string            `json:"terminal_visible_closeout_proof_status,omitempty"`
	TerminalVisibleCloseoutProofBlocker   string            `json:"terminal_visible_closeout_proof_blocker,omitempty"`
	TerminalPhase                         string            `json:"terminal_phase,omitempty"`
	CompletionVerdict                     string            `json:"completion_verdict,omitempty"`
	TerminalEventID                       string            `json:"terminal_event_id,omitempty"`
	TerminalSynthesisEventID              string            `json:"terminal_synthesis_event_id,omitempty"`
	TerminalEventType                     string            `json:"terminal_event_type,omitempty"`
	ProposeReady                          bool              `json:"propose_ready"`
	TerminalVisible                       bool              `json:"terminal_visible"`
	BlockingReasons                       []string          `json:"blocking_reasons,omitempty"`
	VisibleTurnTotal                      int               `json:"visible_turn_total,omitempty"`
	VisibleTurnAccounting                 map[string]int    `json:"visible_turn_accounting,omitempty"`
	SpeechStages                          map[string]string `json:"-"`
	SpeechVisibleTurnIndexes              map[string]int    `json:"-"`
	SpeechVisibleTurnTotalByID            map[string]int    `json:"-"`
}

func councilDiscussionLifecycle(metadata *SessionMetadata, index *LogIndex) CouncilDiscussionLifecycle {
	lifecycle := CouncilDiscussionLifecycle{
		ParticipantCount:                   len(councilMembers(metadata)),
		LegacyMaxTurnsDisplay:              metadata.Limits.MaxTurns,
		TerminalPhase:                      "not_terminal",
		TerminalVisibleCloseoutProofStatus: "not_terminal",
		CompletionVerdict:                  "not_configured",
		SpeechStages:                       map[string]string{},
		SpeechVisibleTurnIndexes:           map[string]int{},
		SpeechVisibleTurnTotalByID:         map[string]int{},
	}
	maxDiscussionTurns := metadata.Limits.MaxDiscussionTurns
	if maxDiscussionTurns <= 0 {
		return lifecycle
	}
	lifecycle.Configured = true
	lifecycle.MaxDiscussionTurns = maxDiscussionTurns
	lifecycle.DiscussionTurnsRequired = maxDiscussionTurns
	lifecycle.ParticipantCloseoutsRequired = lifecycle.ParticipantCount
	lifecycle.ExpectedVisibleTurns = maxDiscussionTurns + lifecycle.ParticipantCount + 2
	lifecycle.TerminalSynthesisTurn = maxDiscussionTurns + lifecycle.ParticipantCount + 1
	lifecycle.TerminalSynthesisExpectedVisibleIndex = lifecycle.ExpectedVisibleTurns
	lifecycle.VisibleTurnTotal = lifecycle.ExpectedVisibleTurns

	discussionTurns := map[int]bool{}
	closeoutMembers := map[string]bool{}
	closeoutOrder := map[string]int{}
	grantedSpeakers := map[string]bool{}
	nextCloseoutIndex := maxDiscussionTurns + 2
	var terminalPayload map[string]any

	for _, event := range index.Events {
		switch event.Type {
		case "speaker_selected":
			turn := eventTurn(event)
			member := strings.TrimSpace(payloadStringDefault(event.Payload, "member", ""))
			if member == "" && len(event.To) == 1 {
				member = strings.TrimSpace(event.To[0])
			}
			if turn > 0 && member != "" {
				grantedSpeakers[discussionLifecycleGrantKey(member, turn)] = true
			}
		case "hand_raise_requested":
			if lifecycle.OpeningEventID == "" && event.From == metadata.Moderator {
				lifecycle.OpeningEventID = event.EventID
				lifecycle.OpeningTurn = eventTurn(event)
			}
		case "speech":
			turn := eventTurn(event)
			member := strings.TrimSpace(event.From)
			if turn <= 0 || !councilMember(metadata, member) || !grantedSpeakers[discussionLifecycleGrantKey(member, turn)] {
				continue
			}
			if turn <= maxDiscussionTurns {
				discussionTurns[turn] = true
				lifecycle.SpeechStages[event.EventID] = "discussion"
				lifecycle.SpeechVisibleTurnIndexes[event.EventID] = turn + 1
				lifecycle.SpeechVisibleTurnTotalByID[event.EventID] = lifecycle.ExpectedVisibleTurns
				continue
			}
			if !closeoutMembers[member] {
				closeoutMembers[member] = true
				closeoutOrder[member] = nextCloseoutIndex
				nextCloseoutIndex++
				lifecycle.SpeechStages[event.EventID] = "closeout"
				lifecycle.SpeechVisibleTurnIndexes[event.EventID] = closeoutOrder[member]
				lifecycle.SpeechVisibleTurnTotalByID[event.EventID] = lifecycle.ExpectedVisibleTurns
			}
		case "council_finalized", "council_unresolved":
			lifecycle.TerminalEventID = event.EventID
			lifecycle.TerminalSynthesisEventID = event.EventID
			lifecycle.TerminalEventType = event.Type
			lifecycle.TerminalVisible = true
			terminalPayload = event.Payload
		}
	}

	for turn := 1; turn <= maxDiscussionTurns; turn++ {
		if discussionTurns[turn] {
			lifecycle.DiscussionTurnsPresent = append(lifecycle.DiscussionTurnsPresent, turn)
		} else {
			lifecycle.MissingDiscussionTurns = append(lifecycle.MissingDiscussionTurns, turn)
		}
	}
	for _, member := range councilMembers(metadata) {
		if closeoutMembers[member] {
			lifecycle.CloseoutMembersPresent = append(lifecycle.CloseoutMembersPresent, member)
		} else {
			lifecycle.MissingCloseoutMembers = append(lifecycle.MissingCloseoutMembers, member)
		}
	}
	sort.Ints(lifecycle.DiscussionTurnsPresent)
	sort.Ints(lifecycle.MissingDiscussionTurns)
	sort.Strings(lifecycle.CloseoutMembersPresent)
	sort.Strings(lifecycle.MissingCloseoutMembers)

	lifecycle.DiscussionTurnsPresentCount = len(lifecycle.DiscussionTurnsPresent)
	lifecycle.DiscussionTurnsComplete = lifecycle.Configured && len(lifecycle.MissingDiscussionTurns) == 0
	lifecycle.ParticipantCloseoutsPresentCount = len(lifecycle.CloseoutMembersPresent)
	lifecycle.ParticipantCloseoutsComplete = lifecycle.Configured && len(lifecycle.MissingCloseoutMembers) == 0
	lifecycle.ModeratorOpeningPresent = lifecycle.OpeningEventID != ""
	lifecycle.TerminalSynthesisSummaryPresent = terminalSynthesisSummaryPresent(lifecycle.TerminalEventType, terminalPayload)
	lifecycle.ModeratorSynthesisPresent = lifecycleTerminalSynthesisPresent(lifecycle, terminalPayload)

	if lifecycle.OpeningEventID == "" {
		lifecycle.BlockingReasons = append(lifecycle.BlockingReasons, "missing_moderator_opening")
	}
	for _, turn := range lifecycle.MissingDiscussionTurns {
		lifecycle.BlockingReasons = append(lifecycle.BlockingReasons, fmt.Sprintf("missing_discussion_turn_%d", turn))
	}
	for _, member := range lifecycle.MissingCloseoutMembers {
		lifecycle.BlockingReasons = append(lifecycle.BlockingReasons, "missing_closeout_"+member)
	}
	lifecycle.ProposeReady = lifecycle.OpeningEventID != "" && len(lifecycle.MissingDiscussionTurns) == 0 && len(lifecycle.MissingCloseoutMembers) == 0
	lifecycle.TerminalVisibleCloseoutProofStatus, lifecycle.TerminalVisibleCloseoutProofBlocker = terminalVisibleCloseoutProofStatus(lifecycle.TerminalEventType, terminalPayload)
	lifecycle.TerminalPhase = terminalLifecyclePhase(lifecycle.TerminalEventType, lifecycle.TerminalVisibleCloseoutProofStatus)
	lifecycle.CompletionVerdict = lifecycleCompletionVerdict(lifecycle)
	if lifecycle.TerminalEventType == "council_finalized" && lifecycle.TerminalVisibleCloseoutProofStatus != "posted" {
		lifecycle.BlockingReasons = append(lifecycle.BlockingReasons, "terminal_visible_closeout_proof_"+lifecycle.TerminalVisibleCloseoutProofStatus)
	}
	lifecycle.VisibleTurnAccounting = map[string]int{
		"moderator_opening":    boolCount(lifecycle.OpeningEventID != ""),
		"discussion_speeches":  len(lifecycle.DiscussionTurnsPresent),
		"participant_closeout": len(lifecycle.CloseoutMembersPresent),
		"terminal_conclusion":  boolCount(lifecycle.TerminalVisible),
	}
	return lifecycle
}

func terminalVisibleCloseoutProofStatus(eventType string, payload map[string]any) (string, string) {
	if eventType == "" {
		return "not_terminal", ""
	}
	if payload == nil {
		return "missing", "surface_evidence missing"
	}
	surfaceValue, ok := payload["surface_evidence"]
	if !ok {
		return "missing", "surface_evidence missing"
	}
	status, evidence := visibleCloseoutSurfaceStatus(surfaceValue)
	status, evidence = closeoutProjectionStatus(payload, status, evidence)
	switch status {
	case "posted", "failed", "pending_followup":
		return status, evidence
	case "missing/unproven":
		if strings.TrimSpace(evidence) == "" {
			return "unproven", "surface_evidence unproven"
		}
		return "unproven", evidence
	default:
		if strings.TrimSpace(evidence) == "" {
			return "unproven", "surface_evidence unsupported"
		}
		return "unproven", evidence
	}
}

func terminalSynthesisSummaryPresent(eventType string, payload map[string]any) bool {
	switch eventType {
	case "council_finalized":
		return strings.TrimSpace(payloadStringDefault(payload, "final_summary", "")) != ""
	case "council_unresolved":
		return strings.TrimSpace(payloadStringDefault(payload, "reason", "")) != ""
	default:
		return false
	}
}

func lifecycleTerminalSynthesisPresent(lifecycle CouncilDiscussionLifecycle, payload map[string]any) bool {
	if lifecycle.TerminalEventID == "" || !lifecycle.Configured {
		return false
	}
	if !lifecycle.ModeratorOpeningPresent || !lifecycle.DiscussionTurnsComplete || !lifecycle.ParticipantCloseoutsComplete {
		return false
	}
	return terminalSynthesisSummaryPresent(lifecycle.TerminalEventType, payload)
}

func terminalLifecyclePhase(eventType, proofStatus string) string {
	switch eventType {
	case "":
		return "not_terminal"
	case "council_unresolved":
		return "terminal_unresolved"
	case "council_finalized":
		switch proofStatus {
		case "posted":
			return "finalized"
		case "failed":
			return "terminal_blocked_visible_proof_failed"
		case "pending_followup":
			return "terminal_pending_visible_proof_followup"
		case "unproven":
			return "terminal_blocked_visible_proof_unproven"
		default:
			return "terminal_blocked_missing_visible_proof"
		}
	default:
		return "not_terminal"
	}
}

func lifecycleCompletionVerdict(lifecycle CouncilDiscussionLifecycle) string {
	if !lifecycle.Configured {
		return "not_configured"
	}
	if !lifecycle.ModeratorOpeningPresent {
		return "opening_missing"
	}
	if !lifecycle.DiscussionTurnsComplete {
		return "discussion_pending"
	}
	if !lifecycle.ParticipantCloseoutsComplete {
		return "discussion_complete_closeout_pending"
	}
	if !lifecycle.ModeratorSynthesisPresent {
		return "participant_closeouts_complete_moderator_synthesis_pending"
	}
	if lifecycle.TerminalEventType == "council_unresolved" {
		return "terminal_unresolved"
	}
	if lifecycle.TerminalEventType == "council_finalized" {
		switch lifecycle.TerminalVisibleCloseoutProofStatus {
		case "posted":
			return "finalized"
		case "failed":
			return "terminal_visible_closeout_proof_failed"
		case "pending_followup":
			return "terminal_visible_closeout_proof_pending_followup"
		case "unproven":
			return "terminal_visible_closeout_proof_unproven"
		default:
			return "terminal_visible_closeout_proof_missing"
		}
	}
	return "participant_closeouts_complete_moderator_synthesis_pending"
}

func eventTurn(event EventEnvelope) int {
	if event.Turn != nil && *event.Turn > 0 {
		return *event.Turn
	}
	return anyInt(event.Payload, "turn")
}

func discussionLifecycleGrantKey(member string, turn int) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(member), turn)
}

func boolCount(ok bool) int {
	if ok {
		return 1
	}
	return 0
}

func validateDiscussionLifecycleLimits(limits Limits) error {
	if limits.MaxDiscussionTurns < 0 {
		return NewValidationError(CategoryInvalidEnvelope, "limits.max_discussion_turns", "max_discussion_turns must be positive when configured")
	}
	return nil
}

func validateCouncilProposeLifecycle(metadata *SessionMetadata, index *LogIndex) error {
	lifecycle := councilDiscussionLifecycle(metadata, index)
	if !lifecycle.Configured || lifecycle.ProposeReady {
		return nil
	}
	return NewValidationError(CategoryCommandConflict, "discussion_lifecycle", "council.propose requires discussion window and participant closeouts: "+strings.Join(lifecycle.BlockingReasons, ","))
}
