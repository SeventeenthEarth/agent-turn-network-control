package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"atn-control/internal/protocol"
	"atn-control/internal/registry"
)

func CreateSession(dataHome string, loaded *registry.LoadedRegistry, spec SessionSpec, runtime registry.Runtime) (*SessionMetadata, AppendResult, error) {
	if loaded == nil {
		return nil, AppendResult{}, NewValidationError(CategorySnapshotRequired, "registry", "loaded registry is required")
	}
	runtime = runtimeWithDefaults(runtime)
	cleanDataHome := filepath.Clean(dataHome)
	if err := registry.ValidateDataHome(cleanDataHome, runtime); err != nil {
		return nil, AppendResult{}, err
	}
	if filepath.Clean(loaded.DataHome) != cleanDataHome {
		return nil, AppendResult{}, NewValidationError(CategoryRegistryMismatch, "registry.data_home", "loaded registry data home does not match session data home")
	}
	if strings.TrimSpace(loaded.SourcePath) == "" || strings.TrimSpace(loaded.SourceSHA256) == "" || len(loaded.SourceContent) == 0 {
		return nil, AppendResult{}, NewValidationError(CategorySnapshotRequired, "registry", "loaded registry metadata is incomplete")
	}
	if err := validateSessionSpec(loaded, spec); err != nil {
		return nil, AppendResult{}, err
	}
	sessionsRoot, err := ensureSessionsRoot(cleanDataHome)
	if err != nil {
		return nil, AppendResult{}, err
	}
	finalDir, err := SessionDir(cleanDataHome, spec.ID)
	if err != nil {
		return nil, AppendResult{}, err
	}
	if info, err := os.Lstat(finalDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, AppendResult{}, NewValidationError(CategorySessionExists, finalDir, "final session path is a symlink")
		}
		return nil, AppendResult{}, NewValidationError(CategorySessionExists, finalDir, "session already exists")
	} else if !os.IsNotExist(err) {
		return nil, AppendResult{}, NewValidationError(CategorySessionUnsafe, finalDir, err.Error())
	}
	if active, err := FindActiveSession(cleanDataHome, runtime); err != nil {
		return nil, AppendResult{}, err
	} else if active != nil {
		return nil, AppendResult{}, NewValidationError(CategoryCommandConflict, "active_session", fmt.Sprintf("active session %s is %s", active.SessionID, active.Status))
	}

	stagingDir := filepath.Join(sessionsRoot, fmt.Sprintf(".tmp-%s-%d", spec.ID, runtime.Now().UnixNano()))
	if !pathContains(sessionsRoot, stagingDir) {
		return nil, AppendResult{}, NewValidationError(CategoryPathUnsafe, stagingDir, "staging path escapes sessions root")
	}
	if err := os.Mkdir(stagingDir, 0o700); err != nil {
		return nil, AppendResult{}, NewValidationError(CategorySessionUnsafe, stagingDir, err.Error())
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	now := runtime.Now().UTC()
	snapshotRuntime := runtime
	snapshotRuntime.Now = func() time.Time { return now }
	if err := registry.WriteSnapshotAtomic(stagingDir, loaded, snapshotRuntime); err != nil {
		return nil, AppendResult{}, NewValidationError(CategorySnapshotFailed, registry.SnapshotFileName, err.Error())
	}
	metadata := metadataFromSpec(spec, loaded, now)
	if err := WriteSessionYAMLAtomic(stagingDir, metadata); err != nil {
		return nil, AppendResult{}, err
	}
	createdEvent := sessionCreatedEvent(metadata, spec, now)
	result, err := AppendEvent(stagingDir, metadata, createdEvent)
	if err != nil {
		return nil, AppendResult{}, err
	}
	if info, err := os.Lstat(finalDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, AppendResult{}, NewValidationError(CategorySessionExists, finalDir, "final session path is a symlink")
		}
		return nil, AppendResult{}, NewValidationError(CategorySessionExists, finalDir, "session already exists")
	} else if !os.IsNotExist(err) {
		return nil, AppendResult{}, NewValidationError(CategorySessionUnsafe, finalDir, err.Error())
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return nil, AppendResult{}, NewValidationError(CategorySessionUnsafe, finalDir, err.Error())
	}
	cleanup = false
	if err := syncDirectoryBestEffort(sessionsRoot); err != nil {
		return nil, AppendResult{}, NewValidationError(CategorySessionUnsafe, sessionsRoot, err.Error())
	}
	return metadata, result, nil
}

func validateSessionSpec(loaded *registry.LoadedRegistry, spec SessionSpec) error {
	var issues []Issue
	add := func(category, path, message string) {
		issues = append(issues, Issue{Category: category, Path: path, Message: message})
	}
	if err := ValidateSessionID(spec.ID); err != nil {
		issues = append(issues, Issues(err)...)
	}
	if !validSessionType(spec.SessionType) {
		add(CategoryMetadataInvalid, "session_type", "session_type must be delegation or council")
	}
	if strings.TrimSpace(spec.Title) == "" {
		add(CategoryMetadataInvalid, "title", "title is required")
	}
	if strings.TrimSpace(spec.Moderator) == "" {
		add(CategoryMetadataInvalid, "moderator", "moderator is required")
	}
	if len(spec.Participants) == 0 {
		add(CategoryMetadataInvalid, "participants", "at least one participant is required")
	}
	if !containsString(spec.Participants, spec.Moderator) {
		add(CategoryMetadataInvalid, "participants", "moderator must be included in participants")
	}
	seen := map[string]struct{}{}
	for _, id := range spec.Participants {
		if strings.TrimSpace(id) == "" {
			add(CategoryPrincipalInvalid, "participants", "participant id is required")
			continue
		}
		if _, ok := seen[id]; ok {
			add(CategoryPrincipalInvalid, "participants", "duplicate participant")
		}
		seen[id] = struct{}{}
		if _, ok := loaded.Registry.Members[id]; !ok {
			add(CategoryPrincipalInvalid, "participants", "participant is not in loaded registry")
		}
	}
	if _, ok := loaded.Registry.Members[spec.Moderator]; !ok {
		add(CategoryPrincipalInvalid, "moderator", "moderator is not in loaded registry")
	}
	if spec.EventID == "" {
		add(CategoryInvalidEnvelope, "event_id", "session_created event id is required")
	}
	if spec.Surface != nil && spec.Surface.Kind == "discord_thread" && strings.TrimSpace(spec.Surface.ThreadID) == "" {
		add(CategoryInvalidEnvelope, "surface.thread_id", "discord_thread surface requires thread_id")
	}
	if spec.TurnMode != "" && !validTurnMode(spec.TurnMode) {
		add(CategoryMetadataInvalid, "turn_mode", "unsupported turn_mode")
	}
	if spec.SessionType == SessionTypeCouncil {
		if err := validateDiscussionLifecycleLimits(spec.Limits); err != nil {
			issues = append(issues, Issues(err)...)
		}
		if err := validateDiscussionQualityLimits(spec.Limits); err != nil {
			issues = append(issues, Issues(err)...)
		}
	}
	if len(issues) > 0 {
		return NewValidationErrors(issues)
	}
	return nil
}

func metadataFromSpec(spec SessionSpec, loaded *registry.LoadedRegistry, now time.Time) *SessionMetadata {
	return &SessionMetadata{
		ID:              spec.ID,
		SessionType:     spec.SessionType,
		Status:          StatusOpen,
		Title:           spec.Title,
		Moderator:       spec.Moderator,
		Participants:    append([]string(nil), spec.Participants...),
		Surface:         cloneSurface(spec.Surface),
		LinkedAuthority: cloneLinkedAuthority(spec.LinkedAuthority),
		TurnMode:        spec.TurnMode,
		CreatedAt:       now,
		Limits:          spec.Limits,
		State: SessionState{
			Phase: PhaseCreated,
		},
		Cost: CostSummary{},
		Escalations: EscalationSummary{
			WaitingUser: nil,
		},
		RegistrySnapshot: registry.SnapshotMetadata{
			SourcePath:    loaded.SourcePath,
			SourceSHA256:  loaded.SourceSHA256,
			LoadedAt:      now,
			LoadedByUID:   loaded.LoadedByUID,
			SchemaVersion: loaded.Registry.EffectiveSchemaVersion(),
		},
	}
}

func sessionCreatedEvent(metadata *SessionMetadata, spec SessionSpec, now time.Time) EventEnvelope {
	to := append([]string(nil), metadata.Participants...)
	sort.Strings(to)
	payload := map[string]any{
		"session_type": string(metadata.SessionType),
		"title":        metadata.Title,
		"moderator":    metadata.Moderator,
		"participants": append([]string(nil), metadata.Participants...),
		"limits":       metadata.Limits,
	}
	if metadata.Surface != nil {
		payload["surface"] = metadata.Surface
	}
	if len(spec.RequestContext) > 0 {
		payload["request_context"] = cloneMap(spec.RequestContext)
	}
	if metadata.LinkedAuthority != nil {
		payload["linked_authority"] = metadata.LinkedAuthority
	}
	if metadata.TurnMode != "" {
		payload["turn_mode"] = metadata.TurnMode
	}
	correlationID := spec.CorrelationID
	if correlationID == "" {
		correlationID = metadata.ID
	}
	return EventEnvelope{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       spec.EventID,
		CommandID:     spec.CommandID,
		CorrelationID: correlationID,
		SessionID:     metadata.ID,
		SessionType:   metadata.SessionType,
		Phase:         PhaseCreated,
		Type:          "session_created",
		From:          "atn-controld",
		To:            to,
		CreatedAt:     now,
		Payload:       payload,
	}
}

func cloneMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func runtimeWithDefaults(runtime registry.Runtime) registry.Runtime {
	defaults := registry.DefaultRuntime()
	if runtime.LookupEnv == nil {
		runtime.LookupEnv = defaults.LookupEnv
	}
	if runtime.UserHomeDir == nil {
		runtime.UserHomeDir = defaults.UserHomeDir
	}
	if runtime.CurrentUID == nil {
		runtime.CurrentUID = defaults.CurrentUID
	}
	if runtime.Now == nil {
		runtime.Now = defaults.Now
	}
	return runtime
}

func validTurnMode(mode string) bool {
	switch mode {
	case "relevance", "targeted", "random", "moderator_direct", "role_order":
		return true
	default:
		return false
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cloneSurface(surface *Surface) *Surface {
	if surface == nil {
		return nil
	}
	copy := *surface
	return &copy
}

func cloneLinkedAuthority(linked *LinkedAuthority) *LinkedAuthority {
	if linked == nil {
		return nil
	}
	copy := *linked
	return &copy
}
