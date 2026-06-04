package storage

import (
	"errors"
	"fmt"
	"strings"
)

const (
	CategoryInvalidSessionID = "storage_invalid_session_id"
	CategoryPathUnsafe       = "storage_path_unsafe"
	CategorySessionExists    = "storage_session_exists"
	CategorySessionUnsafe    = "storage_session_unsafe"
	CategorySnapshotRequired = "storage_snapshot_required"
	CategorySnapshotFailed   = "storage_snapshot_failed"
	CategoryMetadataInvalid  = "storage_metadata_invalid"
	CategoryMetadataWrite    = "storage_metadata_write_failed"
	CategoryInvalidEnvelope  = "storage_invalid_envelope"
	CategoryLogCorrupt       = "storage_log_corrupt"
	CategoryDuplicateEventID = "storage_duplicate_event_id"
	CategoryAppendFailed     = "storage_append_failed"
	CategoryPrincipalInvalid = "storage_principal_invalid"
	CategoryRegistryMismatch = "storage_registry_mismatch"
	CategoryProjectionFailed = "storage_projection_failed"
	CategoryProjectionUnsafe = "storage_projection_unsafe"
	CategoryProjectionVerify = "storage_projection_verify_failed"
	CategoryCommandConflict  = "storage_command_conflict"
)

// Issue is a fail-closed storage validation finding.
type Issue struct {
	Category string
	Path     string
	Message  string
}

func (i Issue) String() string {
	var parts []string
	if i.Category != "" {
		parts = append(parts, i.Category)
	}
	if i.Path != "" {
		parts = append(parts, i.Path)
	}
	if i.Message != "" {
		parts = append(parts, i.Message)
	}
	return strings.Join(parts, ": ")
}

// ValidationError represents one or more storage validation issues.
type ValidationError struct {
	Issues []Issue
}

func NewValidationError(category, path, message string) *ValidationError {
	return &ValidationError{Issues: []Issue{{Category: category, Path: path, Message: message}}}
}

func NewValidationErrors(issues []Issue) *ValidationError {
	return &ValidationError{Issues: issues}
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "storage validation failed"
	}
	if len(e.Issues) == 1 {
		return e.Issues[0].String()
	}
	lines := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		lines = append(lines, issue.String())
	}
	return fmt.Sprintf("%d storage validation issues: %s", len(e.Issues), strings.Join(lines, "; "))
}

func IsValidationError(err error) bool {
	var validationErr *ValidationError
	return errors.As(err, &validationErr)
}

func Issues(err error) []Issue {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return append([]Issue(nil), validationErr.Issues...)
	}
	if err == nil {
		return nil
	}
	return []Issue{{Category: CategoryMetadataInvalid, Message: err.Error()}}
}
