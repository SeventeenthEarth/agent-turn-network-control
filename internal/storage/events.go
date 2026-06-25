package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"atn-control/internal/protocol"
)

var delegationPhases = map[Phase]struct{}{
	"created": {}, "assigned": {}, "acknowledged": {}, "working": {}, "needs_clarification": {},
	"waiting_user": {}, "submitted": {}, "under_review": {}, "revision_requested": {}, "blocked": {},
	"accepted": {}, "cancelled": {},
}

var councilPhases = map[Phase]struct{}{
	"created": {}, "preparation": {}, "discussion": {}, "draft_conclusion": {}, "consensus_vote": {},
	"blocked": {}, "finalized": {}, "unresolved": {}, "cancelled": {},
}

var runnerStatuses = map[string]struct{}{
	"started":                {},
	"succeeded":              {},
	"failed":                 {},
	"timeout":                {},
	"semantic_error":         {},
	"discarded_after_cancel": {},
	"cancelled":              {},
	"interrupted":            {},
}

func AppendEvent(sessionDir string, metadata *SessionMetadata, event EventEnvelope) (AppendResult, error) {
	if err := safeSessionDirForAppend(sessionDir); err != nil {
		return AppendResult{}, err
	}
	if metadata == nil {
		loaded, err := LoadSessionYAML(sessionDir)
		if err != nil {
			return AppendResult{}, err
		}
		metadata = loaded
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return AppendResult{}, err
	}
	if _, ok := index.EventIDs[event.EventID]; ok {
		return AppendResult{}, NewValidationError(CategoryDuplicateEventID, "event_id", "event id already exists in channel.jsonl")
	}
	if err := ValidateEnvelope(metadata, &event); err != nil {
		return AppendResult{}, err
	}
	sort.Strings(event.To)
	if event.CorrelationID == "" {
		event.CorrelationID = event.SessionID
	}
	content, err := json.Marshal(event)
	if err != nil {
		return AppendResult{}, NewValidationError(CategoryInvalidEnvelope, "event", err.Error())
	}
	content = append(content, '\n')
	path := filepath.Join(filepath.Clean(sessionDir), ChannelJSONLName)
	if err := ensureChannelFileSafe(path); err != nil {
		return AppendResult{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return AppendResult{}, NewValidationError(CategoryAppendFailed, path, err.Error())
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if _, err := file.Write(content); err != nil {
		return AppendResult{}, NewValidationError(CategoryAppendFailed, path, err.Error())
	}
	if err := file.Chmod(0o600); err != nil {
		return AppendResult{}, NewValidationError(CategoryAppendFailed, path, err.Error())
	}
	if err := file.Sync(); err != nil {
		return AppendResult{}, NewValidationError(CategoryAppendFailed, path, err.Error())
	}
	if err := file.Close(); err != nil {
		closed = true
		return AppendResult{}, NewValidationError(CategoryAppendFailed, path, err.Error())
	}
	closed = true
	if err := syncDirectoryBestEffort(sessionDir); err != nil {
		return AppendResult{}, NewValidationError(CategoryAppendFailed, sessionDir, err.Error())
	}
	offset := int64(len(index.Events))
	return AppendResult{
		Cursor:  cursorFor(offset, event.EventID),
		Offset:  offset,
		EventID: event.EventID,
	}, nil
}

func ReadLogIndex(sessionDir string, metadata *SessionMetadata) (*LogIndex, error) {
	if err := safeSessionDirForAppend(sessionDir); err != nil {
		return nil, err
	}
	if metadata == nil {
		loaded, err := LoadSessionYAML(sessionDir)
		if err != nil {
			return nil, err
		}
		metadata = loaded
	}
	path := filepath.Join(filepath.Clean(sessionDir), ChannelJSONLName)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LogIndex{EventIDs: map[string]struct{}{}}, nil
		}
		return nil, NewValidationError(CategoryLogCorrupt, path, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, NewValidationError(CategoryLogCorrupt, path, "channel.jsonl symlinks are forbidden")
	}
	if !info.Mode().IsRegular() {
		return nil, NewValidationError(CategoryLogCorrupt, path, "channel.jsonl is not regular")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, NewValidationError(CategoryLogCorrupt, path, err.Error())
	}
	if len(content) == 0 {
		return &LogIndex{EventIDs: map[string]struct{}{}}, nil
	}
	if !bytes.HasSuffix(content, []byte{'\n'}) {
		return nil, NewValidationError(CategoryLogCorrupt, path, "channel.jsonl is missing final newline")
	}
	lines := bytes.Split(bytes.TrimSuffix(content, []byte{'\n'}), []byte{'\n'})
	index := &LogIndex{
		Events:   make([]EventEnvelope, 0, len(lines)),
		EventIDs: make(map[string]struct{}, len(lines)),
	}
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, NewValidationError(CategoryLogCorrupt, fmt.Sprintf("%s:%d", path, i+1), "blank event line")
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, NewValidationError(CategoryLogCorrupt, fmt.Sprintf("%s:%d", path, i+1), err.Error())
		}
		if _, ok := raw["status"]; ok {
			return nil, NewValidationError(CategoryInvalidEnvelope, fmt.Sprintf("%s:%d", path, i+1), "event envelopes must not contain status")
		}
		var event EventEnvelope
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, NewValidationError(CategoryLogCorrupt, fmt.Sprintf("%s:%d", path, i+1), err.Error())
		}
		if _, ok := index.EventIDs[event.EventID]; ok {
			return nil, NewValidationError(CategoryDuplicateEventID, fmt.Sprintf("%s:%d", path, i+1), "duplicate event id in channel.jsonl")
		}
		if err := ValidateEnvelope(metadata, &event); err != nil {
			return nil, err
		}
		index.EventIDs[event.EventID] = struct{}{}
		index.Events = append(index.Events, event)
	}
	return index, nil
}

func ValidateEnvelope(metadata *SessionMetadata, event *EventEnvelope) error {
	if metadata == nil {
		return NewValidationError(CategoryMetadataInvalid, "session", "session metadata is required")
	}
	if event == nil {
		return NewValidationError(CategoryInvalidEnvelope, "event", "event is nil")
	}
	var issues []Issue
	add := func(category, path, message string) {
		issues = append(issues, Issue{Category: category, Path: path, Message: message})
	}
	if event.SchemaVersion != protocol.SchemaVersion {
		add(CategoryInvalidEnvelope, "schema_version", fmt.Sprintf("unsupported schema version %d", event.SchemaVersion))
	}
	if strings.TrimSpace(event.EventID) == "" {
		add(CategoryInvalidEnvelope, "event_id", "event id is required")
	}
	if event.SessionID != metadata.ID {
		add(CategoryInvalidEnvelope, "session_id", "event session_id does not match session metadata")
	}
	if err := ValidateSessionID(event.SessionID); err != nil {
		issues = append(issues, Issues(err)...)
	}
	if event.SessionType != metadata.SessionType {
		add(CategoryInvalidEnvelope, "session_type", "event session_type does not match session metadata")
	}
	if !validSessionType(event.SessionType) {
		add(CategoryInvalidEnvelope, "session_type", "session_type must be delegation or council")
	}
	if !validPhase(event.SessionType, event.Phase) {
		add(CategoryInvalidEnvelope, "phase", "phase is not valid for session_type")
	}
	if strings.TrimSpace(event.Type) == "" {
		add(CategoryInvalidEnvelope, "type", "event type is required")
	}
	if event.CreatedAt.IsZero() {
		add(CategoryInvalidEnvelope, "created_at", "created_at is required")
	}
	if event.CreatedAt.Location() != time.UTC {
		event.CreatedAt = event.CreatedAt.UTC()
	}
	if !principalAllowed(metadata, event.From, true) {
		add(CategoryPrincipalInvalid, "from", "from principal is not allowed for this session")
	}
	if len(event.To) > 0 {
		seen := map[string]struct{}{}
		for _, to := range event.To {
			if to == "all" || to == "*" || to == "atn-controld" {
				add(CategoryPrincipalInvalid, "to", "broadcast shortcuts and daemon recipient are forbidden")
			}
			if !principalAllowed(metadata, to, false) {
				add(CategoryPrincipalInvalid, "to", "recipient is not allowed for this session")
			}
			if _, ok := seen[to]; ok {
				add(CategoryPrincipalInvalid, "to", "duplicate recipient")
			}
			seen[to] = struct{}{}
		}
	}
	if event.Runner == nil && len(event.Cost) > 0 {
		add(CategoryInvalidEnvelope, "cost", "cost must be omitted when runner is absent")
	}
	if event.Runner != nil {
		if _, ok := runnerStatuses[event.Runner.Status]; !ok {
			add(CategoryInvalidEnvelope, "runner.status", "runner status is not allowed")
		}
		switch event.Type {
		case "runner_invocation_started":
			if len(event.Cost) > 0 {
				add(CategoryInvalidEnvelope, "cost", "cost must be omitted on runner_invocation_started")
			}
		default:
			if len(event.Cost) == 0 {
				add(CategoryInvalidEnvelope, "cost", "cost must be present when runner is present on terminal events")
			} else if !validRunnerCost(event.Cost) {
				add(CategoryInvalidEnvelope, "cost", "runner terminal cost must be object or null")
			}
		}
	}
	if event.Type == "session_created" {
		issues = append(issues, validateSessionCreatedPayload(metadata, event)...)
	}
	if len(issues) > 0 {
		return NewValidationErrors(issues)
	}
	return nil
}

func validRunnerCost(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	if trimmed[0] != '{' {
		return false
	}
	var payload map[string]any
	return json.Unmarshal(trimmed, &payload) == nil
}

func validateSessionCreatedPayload(metadata *SessionMetadata, event *EventEnvelope) []Issue {
	var issues []Issue
	add := func(path, message string) {
		issues = append(issues, Issue{Category: CategoryInvalidEnvelope, Path: path, Message: message})
	}
	if event.From != "atn-controld" {
		add("from", "session_created must come from atn-controld")
	}
	if event.Phase != PhaseCreated {
		add("phase", "session_created phase must be created")
	}
	if event.Payload == nil {
		add("payload", "session_created payload is required")
		return issues
	}
	if got, ok := payloadString(event.Payload, "session_type"); !ok || SessionType(got) != metadata.SessionType {
		add("payload.session_type", "payload session_type must match metadata")
	}
	if got, ok := payloadString(event.Payload, "title"); !ok || got != metadata.Title {
		add("payload.title", "payload title must match metadata")
	}
	if got, ok := payloadString(event.Payload, "moderator"); !ok || got != metadata.Moderator {
		add("payload.moderator", "payload moderator must match metadata")
	}
	if got, ok := payloadStringSlice(event.Payload, "participants"); !ok || !sameStringSet(got, metadata.Participants) {
		add("payload.participants", "payload participants must match metadata")
	}
	if metadata.Surface != nil {
		value, ok := event.Payload["surface"]
		if !ok {
			add("payload.surface", "surface is required when session metadata has surface")
		} else if err := validateSurfaceValue(value); err != nil {
			issues = append(issues, Issues(err)...)
		}
	}
	if metadata.LinkedAuthority != nil {
		if _, ok := event.Payload["linked_authority"]; !ok {
			add("payload.linked_authority", "linked_authority is required when session metadata has linked_authority")
		}
	}
	return issues
}

func validateSurfaceValue(value any) error {
	content, err := json.Marshal(value)
	if err != nil {
		return NewValidationError(CategoryInvalidEnvelope, "payload.surface", err.Error())
	}
	var surface Surface
	if err := json.Unmarshal(content, &surface); err != nil {
		return NewValidationError(CategoryInvalidEnvelope, "payload.surface", err.Error())
	}
	if surface.Kind == "discord_thread" && strings.TrimSpace(surface.ThreadID) == "" {
		return NewValidationError(CategoryInvalidEnvelope, "payload.surface.thread_id", "discord_thread surface requires thread_id")
	}
	return nil
}

func ensureChannelFileSafe(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return NewValidationError(CategoryAppendFailed, path, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return NewValidationError(CategoryLogCorrupt, path, "channel.jsonl symlinks are forbidden")
	}
	if !info.Mode().IsRegular() {
		return NewValidationError(CategoryLogCorrupt, path, "channel.jsonl is not regular")
	}
	return nil
}

func cursorFor(offset int64, eventID string) string {
	return fmt.Sprintf("cur_%012d_%s", offset, eventID)
}

func validSessionType(sessionType SessionType) bool {
	return sessionType == SessionTypeDelegation || sessionType == SessionTypeCouncil
}

func validPhase(sessionType SessionType, phase Phase) bool {
	switch sessionType {
	case SessionTypeDelegation:
		_, ok := delegationPhases[phase]
		return ok
	case SessionTypeCouncil:
		_, ok := councilPhases[phase]
		return ok
	default:
		return false
	}
}

func principalAllowed(metadata *SessionMetadata, principal string, allowDaemon bool) bool {
	if principal == "" {
		return false
	}
	if principal == "user" {
		return true
	}
	if principal == "atn-controld" {
		return allowDaemon
	}
	for _, participant := range metadata.Participants {
		if principal == participant {
			return true
		}
	}
	return false
}

func payloadString(payload map[string]any, key string) (string, bool) {
	value, ok := payload[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func payloadStringSlice(payload map[string]any, key string) ([]string, bool) {
	value, ok := payload[key]
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
