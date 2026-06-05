package storage

import "encoding/json"

// EffectiveLimits returns session limits after applying limits_extended events from
// channel.jsonl. It is read-only: event log replay remains the source of truth and
// session.yaml is not mutated.
func EffectiveLimits(sessionDir string, metadata *SessionMetadata) (Limits, error) {
	limits := Limits{}
	if metadata != nil {
		limits = metadata.Limits
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return limits, err
	}
	for _, event := range index.Events {
		if event.Type != "limits_extended" {
			continue
		}
		changes, ok := event.Payload["changes"].(map[string]any)
		if !ok || len(changes) == 0 {
			return limits, NewValidationError(CategoryInvalidEnvelope, "changes", "limits_extended event requires changes")
		}
		content, err := json.Marshal(changes)
		if err != nil {
			return limits, NewValidationError(CategoryInvalidEnvelope, "changes", err.Error())
		}
		if err := json.Unmarshal(content, &limits); err != nil {
			return limits, NewValidationError(CategoryInvalidEnvelope, "changes", err.Error())
		}
	}
	return limits, nil
}
