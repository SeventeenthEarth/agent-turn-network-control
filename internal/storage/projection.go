package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hun-control/internal/protocol"
	"hun-control/internal/registry"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

const (
	ProjectionDBName      = "network.sqlite"
	ProjectionSchemaV1    = 1
	VerifyStatusValid     = "valid"
	VerifyStatusMissing   = "missing_projection"
	VerifyStatusMismatch  = "projection_mismatch"
	VerifyStatusUnsafe    = "unsafe"
	VerifyStatusReplay    = "replay_failure"
	VerifyStatusCorrupt   = "projection_corrupt"
	VerifyStatusSchemaGap = "schema_mismatch"
)

type ProjectionOptions struct {
	Runtime registry.Runtime
}

type VerifyOptions struct {
	Runtime registry.Runtime
}

type ProjectionReport struct {
	DataHome      string
	DBPath        string
	SchemaVersion int
	SessionCount  int
	EventCount    int
	SourceHash    string
}

type VerifyReport struct {
	DataHome                  string
	DBPath                    string
	Status                    string
	RecoverableProjectionOnly bool
	Detail                    string
	Expected                  *ProjectionReport
	ActualSourceHash          string
}

type ProjectionErrorKind string

const (
	ProjectionErrorValidation     ProjectionErrorKind = "validation"
	ProjectionErrorUnsafeDataHome ProjectionErrorKind = "unsafe_data_home"
	ProjectionErrorReplay         ProjectionErrorKind = "replay"
	ProjectionErrorMigration      ProjectionErrorKind = "migration"
	ProjectionErrorStorage        ProjectionErrorKind = "storage"
	ProjectionErrorInternal       ProjectionErrorKind = "internal"
)

type ProjectionError struct {
	Kind   ProjectionErrorKind
	Detail string
	Err    error
}

func (e *ProjectionError) Error() string {
	if e == nil {
		return "projection error"
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Detail, e.Err)
	}
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Kind, e.Detail)
	}
	return string(e.Kind)
}

func (e *ProjectionError) Unwrap() error { return e.Err }

func ProjectionKind(err error) ProjectionErrorKind {
	var projectionErr *ProjectionError
	if errors.As(err, &projectionErr) {
		return projectionErr.Kind
	}
	if IsValidationError(err) {
		for _, issue := range Issues(err) {
			switch issue.Category {
			case CategoryPathUnsafe, CategorySessionUnsafe, CategoryProjectionUnsafe:
				return ProjectionErrorUnsafeDataHome
			case CategoryLogCorrupt, CategoryDuplicateEventID, CategoryInvalidEnvelope, CategoryCommandConflict:
				return ProjectionErrorReplay
			}
		}
		return ProjectionErrorValidation
	}
	if registry.IsValidationError(err) {
		for _, issue := range registry.Issues(err) {
			if issue.Category == registry.CategoryDataHomeUnsafe {
				return ProjectionErrorUnsafeDataHome
			}
		}
		return ProjectionErrorValidation
	}
	if err == nil {
		return ""
	}
	return ProjectionErrorInternal
}

func RebuildProjection(dataHome string, opts ProjectionOptions) (*ProjectionReport, error) {
	dataHome, err := validateProjectionDataHome(dataHome, opts.Runtime)
	if err != nil {
		return nil, err
	}
	if err := validateExistingProjectionPath(dataHome, true); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "validate existing projection", err)
	}
	state, err := ReplayProjectionState(dataHome, opts)
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataHome, ProjectionDBName)
	tmpPath := filepath.Join(dataHome, fmt.Sprintf(".network.sqlite.rebuild.%d.%d", os.Getpid(), time.Now().UnixNano()))
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "create temporary projection", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = cleanupProjectionTemp(tmpPath)
		return nil, wrapProjectionError(ProjectionErrorStorage, "chmod temporary projection", err)
	}
	if err := file.Close(); err != nil {
		_ = cleanupProjectionTemp(tmpPath)
		return nil, wrapProjectionError(ProjectionErrorStorage, "close temporary projection", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = cleanupProjectionTemp(tmpPath)
		}
	}()
	if err := syncDirectoryBestEffort(dataHome); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "fsync data home after temp create", err)
	}

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		return nil, wrapProjectionError(ProjectionErrorInternal, "open temporary projection", err)
	}
	if err := configureRebuildDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	report, err := populateProjectionDB(db, dataHome, state)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "populate temporary projection", err)
	}
	if err := fsyncFileBestEffort(tmpPath); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "fsync temporary projection", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "chmod temporary projection", err)
	}
	if err := os.Rename(tmpPath, dbPath); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "replace projection", err)
	}
	cleanup = false
	if err := os.Chmod(dbPath, 0o600); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "chmod final projection", err)
	}
	if err := syncDirectoryBestEffort(dataHome); err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "fsync data home after replace", err)
	}
	report.DBPath = dbPath
	return report, nil
}

func VerifyStorage(dataHome string, opts VerifyOptions) (*VerifyReport, error) {
	dataHome, err := validateProjectionDataHome(dataHome, opts.Runtime)
	if err != nil {
		return &VerifyReport{DataHome: filepath.Clean(dataHome), Status: VerifyStatusUnsafe, Detail: err.Error()}, err
	}
	state, err := ReplayProjectionState(dataHome, ProjectionOptions(opts))
	if err != nil {
		report := &VerifyReport{DataHome: dataHome, DBPath: filepath.Join(dataHome, ProjectionDBName), Status: VerifyStatusReplay, Detail: err.Error()}
		return report, err
	}
	expectedDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, wrapProjectionError(ProjectionErrorInternal, "open expected projection", err)
	}
	defer func() { _ = expectedDB.Close() }()
	expectedReport, err := populateProjectionDB(expectedDB, dataHome, state)
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataHome, ProjectionDBName)
	if _, err := os.Lstat(dbPath); err != nil {
		if os.IsNotExist(err) {
			report := &VerifyReport{
				DataHome:                  dataHome,
				DBPath:                    dbPath,
				Status:                    VerifyStatusMissing,
				RecoverableProjectionOnly: true,
				Detail:                    "network.sqlite is missing",
				Expected:                  expectedReport,
			}
			return report, wrapProjectionError(ProjectionErrorStorage, "projection missing", NewValidationError(CategoryProjectionVerify, dbPath, "network.sqlite is missing"))
		}
		return nil, wrapProjectionError(ProjectionErrorStorage, "inspect projection", err)
	}
	if err := validateExistingProjectionPath(dataHome, false); err != nil {
		report := &VerifyReport{DataHome: dataHome, DBPath: dbPath, Status: VerifyStatusUnsafe, Detail: err.Error(), Expected: expectedReport}
		return report, wrapProjectionError(ProjectionErrorStorage, "projection unsafe", err)
	}
	actualDB, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, wrapProjectionError(ProjectionErrorStorage, "open projection read-only", err)
	}
	defer func() { _ = actualDB.Close() }()
	if err := integrityCheck(actualDB); err != nil {
		report := &VerifyReport{DataHome: dataHome, DBPath: dbPath, Status: VerifyStatusCorrupt, RecoverableProjectionOnly: true, Detail: err.Error(), Expected: expectedReport}
		return report, wrapProjectionError(ProjectionErrorStorage, "projection integrity check", err)
	}
	actualHash, err := projectionContentHash(actualDB)
	if err != nil {
		report := &VerifyReport{DataHome: dataHome, DBPath: dbPath, Status: VerifyStatusSchemaGap, RecoverableProjectionOnly: true, Detail: err.Error(), Expected: expectedReport}
		return report, wrapProjectionError(ProjectionErrorStorage, "projection schema/hash", err)
	}
	expectedHash, err := projectionContentHash(expectedDB)
	if err != nil {
		return nil, wrapProjectionError(ProjectionErrorInternal, "hash expected projection", err)
	}
	if actualHash != expectedHash {
		report := &VerifyReport{
			DataHome:                  dataHome,
			DBPath:                    dbPath,
			Status:                    VerifyStatusMismatch,
			RecoverableProjectionOnly: true,
			Detail:                    "projection rows differ from deterministic replay",
			Expected:                  expectedReport,
		}
		return report, wrapProjectionError(ProjectionErrorStorage, "projection mismatch", NewValidationError(CategoryProjectionVerify, dbPath, "projection rows differ from deterministic replay"))
	}
	sourceHash, _ := metadataValue(actualDB, "source_hash")
	report := &VerifyReport{DataHome: dataHome, DBPath: dbPath, Status: VerifyStatusValid, Expected: expectedReport, ActualSourceHash: sourceHash}
	return report, nil
}

type projectionState struct {
	sessions               map[string]*sessionProjection
	participants           map[string]*participantProjection
	events                 []*eventProjection
	recipients             []*recipientProjection
	runners                map[string]*runnerProjection
	batches                map[string]*batchProjection
	batchItems             map[string]*batchItemProjection
	streamCursors          map[string]*streamCursorProjection
	streamSubscribers      map[string]*streamSubscriberProjection
	delegationReviews      map[string]*delegationReviewProjection
	handRaises             map[string]*handRaiseProjection
	attendance             map[string]*attendanceProjection
	agendaLocks            map[string]*agendaLockProjection
	votes                  map[string]*voteProjection
	linkedAuthorityResults map[string]*linkedAuthorityProjection
	argumentGraphs         map[string]*argumentGraphProjectionRow
	commands               map[string]*commandProjection
	artifacts              map[string]*artifactProjection
	sessionCount           int
	eventCount             int
	sourceHash             string
	eventIDs               map[string]struct{}
	commandEvents          map[string][]*eventProjection
	hasher                 hashWriter
}

type hashWriter struct {
	h io.Writer
}

func newProjectionState() *projectionState {
	hash := sha256.New()
	return &projectionState{
		sessions:               map[string]*sessionProjection{},
		participants:           map[string]*participantProjection{},
		runners:                map[string]*runnerProjection{},
		batches:                map[string]*batchProjection{},
		batchItems:             map[string]*batchItemProjection{},
		streamCursors:          map[string]*streamCursorProjection{},
		streamSubscribers:      map[string]*streamSubscriberProjection{},
		delegationReviews:      map[string]*delegationReviewProjection{},
		handRaises:             map[string]*handRaiseProjection{},
		attendance:             map[string]*attendanceProjection{},
		agendaLocks:            map[string]*agendaLockProjection{},
		votes:                  map[string]*voteProjection{},
		linkedAuthorityResults: map[string]*linkedAuthorityProjection{},
		argumentGraphs:         map[string]*argumentGraphProjectionRow{},
		commands:               map[string]*commandProjection{},
		artifacts:              map[string]*artifactProjection{},
		eventIDs:               map[string]struct{}{},
		commandEvents:          map[string][]*eventProjection{},
		hasher:                 hashWriter{h: hash},
	}
}

func ReplayProjectionState(dataHome string, opts ProjectionOptions) (*projectionState, error) {
	dataHome, err := validateProjectionDataHome(dataHome, opts.Runtime)
	if err != nil {
		return nil, err
	}
	state := newProjectionState()
	root := filepath.Join(dataHome, SessionsDirName)
	entries, err := safeSessionEntries(root)
	if err != nil {
		return nil, err
	}
	state.sessionCount = len(entries)
	for _, entry := range entries {
		sessionDir := filepath.Join(root, entry)
		metadata, err := LoadSessionYAML(sessionDir)
		if err != nil {
			return nil, wrapProjectionError(ProjectionErrorReplay, "load session metadata", err)
		}
		if metadata.RegistrySnapshot.SchemaVersion != protocol.SchemaVersion {
			return nil, wrapProjectionError(ProjectionErrorMigration, "unsupported registry snapshot schema", NewValidationError(CategoryMetadataInvalid, sessionDir, fmt.Sprintf("unsupported snapshot schema version %d", metadata.RegistrySnapshot.SchemaVersion)))
		}
		index, err := ReadLogIndex(sessionDir, metadata)
		if err != nil {
			return nil, wrapProjectionError(ProjectionErrorReplay, "read channel.jsonl", err)
		}
		snapshotMembers, err := readSnapshotMembers(sessionDir)
		if err != nil {
			return nil, wrapProjectionError(ProjectionErrorReplay, "read registry snapshot", err)
		}
		state.initSession(metadata, snapshotMembers)
		for offset, event := range index.Events {
			if _, ok := state.eventIDs[event.EventID]; ok {
				return nil, wrapProjectionError(ProjectionErrorReplay, "duplicate event id across sessions", NewValidationError(CategoryDuplicateEventID, event.EventID, "duplicate event id across sessions"))
			}
			state.eventIDs[event.EventID] = struct{}{}
			if event.SchemaVersion != protocol.SchemaVersion {
				return nil, wrapProjectionError(ProjectionErrorMigration, "unsupported event schema", NewValidationError(CategoryInvalidEnvelope, event.EventID, fmt.Sprintf("unsupported schema version %d", event.SchemaVersion)))
			}
			if err := state.applyEvent(metadata, offset, event); err != nil {
				return nil, err
			}
		}
	}
	if err := state.finalizeCommands(); err != nil {
		return nil, err
	}
	state.sourceHash = state.hasher.sumHex()
	return state, nil
}

func (h hashWriter) write(parts ...string) {
	for _, part := range parts {
		_, _ = io.WriteString(h.h, part)
		_, _ = io.WriteString(h.h, "\x00")
	}
}

func (h hashWriter) sumHex() string {
	if s, ok := h.h.(interface{ Sum([]byte) []byte }); ok {
		return "sha256:" + hex.EncodeToString(s.Sum(nil))
	}
	return ""
}

func validateProjectionDataHome(dataHome string, runtime registry.Runtime) (string, error) {
	clean := filepath.Clean(dataHome)
	if strings.TrimSpace(clean) == "" || clean == "." {
		return "", wrapProjectionError(ProjectionErrorValidation, "data home is required", NewValidationError(CategoryPathUnsafe, "data_home", "data home is required"))
	}
	if err := registry.ValidateDataHome(clean, runtime); err != nil {
		return "", wrapProjectionError(ProjectionErrorUnsafeDataHome, "validate data home", err)
	}
	return clean, nil
}

func safeSessionEntries(root string) ([]string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, wrapProjectionError(ProjectionErrorUnsafeDataHome, "inspect sessions root", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, wrapProjectionError(ProjectionErrorUnsafeDataHome, "sessions root symlink", NewValidationError(CategorySessionUnsafe, root, "sessions directory symlinks are forbidden"))
	}
	if !info.IsDir() {
		return nil, wrapProjectionError(ProjectionErrorUnsafeDataHome, "sessions root not directory", NewValidationError(CategorySessionUnsafe, root, "sessions path is not a directory"))
	}
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, wrapProjectionError(ProjectionErrorUnsafeDataHome, "read sessions root", err)
	}
	names := make([]string, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		name := dirEntry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if err := ValidateSessionID(name); err != nil {
			return nil, wrapProjectionError(ProjectionErrorUnsafeDataHome, "unsafe session id", err)
		}
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, wrapProjectionError(ProjectionErrorUnsafeDataHome, "inspect session path", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, wrapProjectionError(ProjectionErrorUnsafeDataHome, "unsafe session path", NewValidationError(CategorySessionUnsafe, path, "session path must be a regular directory"))
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func validateExistingProjectionPath(dataHome string, missingOK bool) error {
	path := filepath.Join(dataHome, ProjectionDBName)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) && missingOK {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return NewValidationError(CategoryProjectionUnsafe, path, "network.sqlite symlinks are forbidden")
	}
	if !info.Mode().IsRegular() {
		return NewValidationError(CategoryProjectionUnsafe, path, "network.sqlite is not a regular file")
	}
	return nil
}

func wrapProjectionError(kind ProjectionErrorKind, detail string, err error) error {
	if err == nil {
		return nil
	}
	var projectionErr *ProjectionError
	if errors.As(err, &projectionErr) {
		return projectionErr
	}
	return &ProjectionError{Kind: kind, Detail: detail, Err: err}
}

type snapshotMember struct {
	DisplayName string `yaml:"display_name"`
	Wrapper     string `yaml:"wrapper"`
	Workspace   string `yaml:"workspace"`
	Role        string `yaml:"role"`
}

type snapshotDoc struct {
	Members map[string]snapshotMember `yaml:"members"`
}

func readSnapshotMembers(sessionDir string) (map[string]snapshotMember, error) {
	path := filepath.Join(sessionDir, registry.SnapshotFileName)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewValidationError(CategorySnapshotRequired, path, "registry snapshot is required for replay")
		}
		return nil, NewValidationError(CategorySnapshotRequired, path, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, NewValidationError(CategorySnapshotRequired, path, "registry snapshot symlinks are forbidden")
	}
	if !info.Mode().IsRegular() {
		return nil, NewValidationError(CategorySnapshotRequired, path, "registry snapshot is not regular")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, NewValidationError(CategorySnapshotRequired, path, err.Error())
	}
	var doc snapshotDoc
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, NewValidationError(CategorySnapshotRequired, path, err.Error())
	}
	if len(doc.Members) == 0 {
		return nil, NewValidationError(CategorySnapshotRequired, path, "registry snapshot members are required")
	}
	return doc.Members, nil
}

func statusFromPhase(phase Phase) Status {
	switch phase {
	case "accepted", "cancelled", "finalized", "unresolved":
		return StatusTerminal
	case "blocked":
		return StatusBlocked
	default:
		return StatusOpen
	}
}

func canonicalJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func canonicalJSONRequired(value any) (string, error) {
	text, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "null", nil
	}
	return text, nil
}

func timeText(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func anyString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if text, ok := value.(string); ok {
				return text
			}
			if value != nil {
				return fmt.Sprint(value)
			}
		}
	}
	return ""
}

func anyBool(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if b, ok := value.(bool); ok {
				return b
			}
		}
	}
	return false
}

func anyInt(payload map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			switch typed := value.(type) {
			case int:
				return typed
			case int64:
				return int(typed)
			case float64:
				return int(typed)
			case json.Number:
				n, _ := typed.Int64()
				return int(n)
			}
		}
	}
	return 0
}

func anyFloat(payload map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			switch typed := value.(type) {
			case float64:
				return typed, true
			case int:
				return float64(typed), true
			case json.Number:
				n, err := typed.Float64()
				return n, err == nil
			}
		}
	}
	return 0, false
}

func anyMap(payload map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if m, ok := value.(map[string]any); ok {
				return m
			}
		}
	}
	return nil
}

func anySlice(payload map[string]any, keys ...string) []any {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if s, ok := value.([]any); ok {
				return s
			}
		}
	}
	return nil
}
