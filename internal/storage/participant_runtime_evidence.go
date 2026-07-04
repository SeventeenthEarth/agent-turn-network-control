package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

func attachPersistentParticipantRuntimeEvidence(metadata *SessionMetadata, index *LogIndex, event *EventEnvelope) {
	if metadata == nil || index == nil || event == nil || event.Type != "speech" || event.Payload == nil {
		return
	}
	cursor := CursorFor(int64(len(index.Events)), event.EventID)
	event.Payload["persistent_participant_runtime_evidence"] = persistentParticipantRuntimeEvidenceForSpeech(metadata, index, *event, cursor)
}

func persistentParticipantRuntimeEvidenceForSpeech(metadata *SessionMetadata, index *LogIndex, event EventEnvelope, cursor string) map[string]any {
	members := councilMembers(metadata)
	rows := make([]map[string]any, 0, len(members))
	handles := make([]map[string]any, 0, len(members))
	for _, member := range members {
		coverageKind := "observe_delta"
		if member == event.From {
			coverageKind = "self_ack"
		}
		handle := persistentParticipantHandle(metadata.ID, member)
		generation := persistentParticipantGeneration(index, metadata.ID, member, handle)
		row := map[string]any{
			"session_id":                 metadata.ID,
			"member":                     member,
			"participant_session_handle": handle,
			"generation":                 generation,
			"coverage_kind":              coverageKind,
			"status":                     "ready",
			"speech_event_id":            event.EventID,
			"observed_event_id":          event.EventID,
			"last_cursor":                cursor,
			"source":                     "control_local_event_log",
			"prslr_scope":                "PRSLR-004",
			"updated_at":                 event.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		if strings.TrimSpace(event.CausationEventID) != "" {
			row["request_event_id"] = event.CausationEventID
		}
		rows = append(rows, row)
		handles = append(handles, map[string]any{
			"session_id":                 metadata.ID,
			"member":                     member,
			"participant_session_handle": handle,
			"generation":                 generation,
			"status":                     "open",
		})
	}
	return map[string]any{
		"status":           "coverage_complete",
		"session_id":       metadata.ID,
		"speech_event_id":  event.EventID,
		"coverage_cursor":  cursor,
		"coverage_scope":   "all_council_members",
		"speaker":          event.From,
		"required_members": append([]string(nil), members...),
		"members":          rows,
		"session_registry": handles,
		"evidence_kind":    "persistent_participant_runtime_evidence",
		"prslr_scope":      "PRSLR-004",
	}
}

func latestPersistentParticipantRuntimeEvidenceFromIndex(index *LogIndex) map[string]any {
	if index == nil {
		return nil
	}
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Type != "speech" || event.Payload == nil {
			continue
		}
		if evidence, ok := event.Payload["persistent_participant_runtime_evidence"].(map[string]any); ok && evidence != nil {
			return evidence
		}
	}
	return nil
}

func persistentParticipantGeneration(index *LogIndex, sessionID, member, handle string) int {
	if index == nil {
		return 1
	}
	for i := len(index.Events) - 1; i >= 0; i-- {
		event := index.Events[i]
		if event.Payload == nil {
			continue
		}
		evidence, ok := event.Payload["persistent_participant_runtime_evidence"].(map[string]any)
		if !ok || evidence == nil {
			continue
		}
		rows, ok := evidence["members"].([]any)
		if !ok {
			continue
		}
		for _, value := range rows {
			row, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if rowString(row, "session_id") == sessionID && rowString(row, "member") == member && rowString(row, "participant_session_handle") == handle {
				if generation := anyInt(row, "generation"); generation > 0 {
					return generation
				}
			}
		}
	}
	return 1
}

func persistentParticipantHandle(sessionID, member string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(member)))
	return "psh_" + hex.EncodeToString(sum[:])[:24]
}

func rowString(row map[string]any, key string) string {
	if row == nil {
		return ""
	}
	if value, ok := row[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
