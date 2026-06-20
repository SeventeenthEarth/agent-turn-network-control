package storage

import (
	"fmt"
	"sort"
	"strings"
)

type CouncilDiscussionLifecycle struct {
	Configured                 bool              `json:"configured"`
	MaxDiscussionTurns         int               `json:"max_discussion_turns,omitempty"`
	LegacyMaxTurnsDisplay      int               `json:"legacy_max_turns_display,omitempty"`
	ParticipantCount           int               `json:"participant_count"`
	ExpectedVisibleTurns       int               `json:"expected_visible_turns,omitempty"`
	OpeningEventID             string            `json:"opening_event_id,omitempty"`
	OpeningTurn                int               `json:"opening_turn,omitempty"`
	DiscussionTurnsPresent     []int             `json:"discussion_turns_present,omitempty"`
	MissingDiscussionTurns     []int             `json:"missing_discussion_turns,omitempty"`
	CloseoutMembersPresent     []string          `json:"closeout_members_present,omitempty"`
	MissingCloseoutMembers     []string          `json:"missing_closeout_members,omitempty"`
	TerminalEventID            string            `json:"terminal_event_id,omitempty"`
	TerminalEventType          string            `json:"terminal_event_type,omitempty"`
	ProposeReady               bool              `json:"propose_ready"`
	TerminalVisible            bool              `json:"terminal_visible"`
	BlockingReasons            []string          `json:"blocking_reasons,omitempty"`
	VisibleTurnTotal           int               `json:"visible_turn_total,omitempty"`
	VisibleTurnAccounting      map[string]int    `json:"visible_turn_accounting,omitempty"`
	SpeechStages               map[string]string `json:"-"`
	SpeechVisibleTurnIndexes   map[string]int    `json:"-"`
	SpeechVisibleTurnTotalByID map[string]int    `json:"-"`
}

func councilDiscussionLifecycle(metadata *SessionMetadata, index *LogIndex) CouncilDiscussionLifecycle {
	lifecycle := CouncilDiscussionLifecycle{
		ParticipantCount:           len(councilMembers(metadata)),
		LegacyMaxTurnsDisplay:      metadata.Limits.MaxTurns,
		SpeechStages:               map[string]string{},
		SpeechVisibleTurnIndexes:   map[string]int{},
		SpeechVisibleTurnTotalByID: map[string]int{},
	}
	maxDiscussionTurns := metadata.Limits.MaxDiscussionTurns
	if maxDiscussionTurns <= 0 {
		return lifecycle
	}
	lifecycle.Configured = true
	lifecycle.MaxDiscussionTurns = maxDiscussionTurns
	lifecycle.ExpectedVisibleTurns = maxDiscussionTurns + lifecycle.ParticipantCount + 2
	lifecycle.VisibleTurnTotal = lifecycle.ExpectedVisibleTurns

	discussionTurns := map[int]bool{}
	closeoutMembers := map[string]bool{}
	closeoutOrder := map[string]int{}
	grantedSpeakers := map[string]bool{}
	nextCloseoutIndex := maxDiscussionTurns + 2

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
			lifecycle.TerminalEventType = event.Type
			lifecycle.TerminalVisible = true
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
	lifecycle.VisibleTurnAccounting = map[string]int{
		"moderator_opening":    boolCount(lifecycle.OpeningEventID != ""),
		"discussion_speeches":  len(lifecycle.DiscussionTurnsPresent),
		"participant_closeout": len(lifecycle.CloseoutMembersPresent),
		"terminal_conclusion":  boolCount(lifecycle.TerminalVisible),
	}
	return lifecycle
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
