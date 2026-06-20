package storage

import "strings"

type summaryTurnAccountingRow struct {
	Turn                   int    `json:"turn"`
	Member                 string `json:"member"`
	SpeakerSelectedEventID string `json:"speaker_selected_event_id"`
	SpeechEventID          string `json:"speech_event_id"`
	RunnerInvocationID     string `json:"runner_invocation_id"`
	VisibleMessageID       string `json:"visible_message_id"`
	LifecycleStage         string `json:"lifecycle_stage,omitempty"`
	VisibleTurnIndex       int    `json:"visible_turn_index,omitempty"`
	VisibleTurnTotal       int    `json:"visible_turn_total,omitempty"`
}

type summaryTurnKey struct {
	turn   int
	member string
}

func summaryTurnAccountingRows(metadata *SessionMetadata, index *LogIndex) []summaryTurnAccountingRow {
	events := index.Events
	lifecycle := councilDiscussionLifecycle(metadata, index)
	rows := make([]summaryTurnAccountingRow, 0)
	rowIndex := map[summaryTurnKey]int{}
	for _, event := range events {
		switch event.Type {
		case "speaker_selected":
			turn, ok := payloadInt(event.Payload, "turn")
			if !ok {
				continue
			}
			member := selectedRunnerMember(event)
			if member == "" && len(event.To) > 0 {
				member = strings.TrimSpace(event.To[0])
			}
			if member == "" {
				continue
			}
			key := summaryTurnKey{turn: turn, member: member}
			if _, ok := rowIndex[key]; ok {
				continue
			}
			rowIndex[key] = len(rows)
			rows = append(rows, summaryTurnAccountingRow{
				Turn:                   turn,
				Member:                 member,
				SpeakerSelectedEventID: event.EventID,
			})
		case "speech":
			turn, ok := payloadInt(event.Payload, "turn")
			if !ok {
				continue
			}
			member := strings.TrimSpace(event.From)
			if member == "" {
				continue
			}
			key := summaryTurnKey{turn: turn, member: member}
			index, ok := rowIndex[key]
			if !ok {
				continue
			}
			row := &rows[index]
			if row.SpeechEventID == "" {
				row.SpeechEventID = event.EventID
			}
			if row.LifecycleStage == "" {
				row.LifecycleStage = lifecycle.SpeechStages[event.EventID]
			}
			if row.VisibleTurnIndex == 0 {
				row.VisibleTurnIndex = lifecycle.SpeechVisibleTurnIndexes[event.EventID]
			}
			if row.VisibleTurnTotal == 0 {
				row.VisibleTurnTotal = lifecycle.SpeechVisibleTurnTotalByID[event.EventID]
			}
			if row.RunnerInvocationID == "" && event.Runner != nil {
				row.RunnerInvocationID = strings.TrimSpace(event.Runner.InvocationID)
			}
			if row.VisibleMessageID == "" {
				row.VisibleMessageID = visibleMessageIDFromSummaryPayload(event.Payload)
			}
		}
	}
	return rows
}

func visibleMessageIDFromSummaryPayload(payload map[string]any) string {
	for _, key := range []string{"plugin_evidence", "evidence", "surface_evidence"} {
		if value, ok := payload[key]; ok {
			if id := explicitVisibleMessageID(value); id != "" {
				return id
			}
		}
	}
	return ""
}

func explicitVisibleMessageID(value any) string {
	if id := explicitVisibleMessageIDFromObject(value); id != "" {
		return id
	}
	switch typed := value.(type) {
	case []map[string]any:
		for _, item := range typed {
			if id := explicitVisibleMessageIDFromObject(item); id != "" {
				return id
			}
		}
	case []any:
		for _, item := range typed {
			if id := explicitVisibleMessageIDFromObject(item); id != "" {
				return id
			}
		}
	}
	return ""
}

func explicitVisibleMessageIDFromObject(value any) string {
	payload, ok := asStringMap(value)
	if !ok {
		return ""
	}
	if id, ok := stringValue(payload["visible_message_id"]); ok {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			return trimmed
		}
	}
	if !isExplicitVisibleMessageEvidence(payload) {
		return ""
	}
	for _, key := range []string{
		"message_id",
		"final_message_id",
		"discord_message_id",
		"thread_message_id",
	} {
		if id, ok := stringValue(payload[key]); ok {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func isExplicitVisibleMessageEvidence(payload map[string]any) bool {
	for _, key := range []string{"kind", "target", "type", "platform"} {
		value, ok := stringValue(payload[key])
		if !ok {
			continue
		}
		switch strings.TrimSpace(value) {
		case "discord", "discord_message", "discord_thread", "visible_message", "visible_surface", "platform_delivery":
			return true
		}
	}
	return false
}
