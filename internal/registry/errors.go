package registry

import (
	"errors"
	"fmt"
	"strings"
)

const (
	CategoryDataHomeUnsafe             = "registry_data_home_unsafe"
	CategoryMissing                    = "registry_missing"
	CategoryNotRegular                 = "registry_not_regular"
	CategorySymlinkForbidden           = "registry_symlink_forbidden"
	CategoryOwnerUnsafe                = "registry_owner_unsafe"
	CategoryPermissionsUnsafe          = "registry_permissions_unsafe"
	CategoryParseError                 = "registry_parse_error"
	CategorySchemaInvalid              = "registry_schema_invalid"
	CategoryUnknownKey                 = "registry_unknown_key"
	CategoryUnknownRuntimeKind         = "registry_unknown_runtime_kind"
	CategoryUnknownAdapterKind         = "registry_unknown_adapter_kind"
	CategoryReservedPrincipalCollision = "registry_reserved_principal_collision"
	CategorySnapshotWriteFailed        = "registry_snapshot_write_failed"
	CategoryChangedDuringLoad          = "registry_changed_during_load"
	CategoryWrapperUnresolvable        = "wrapper_unresolvable"
	CategoryWrapperOutsideAllowlist    = "wrapper_outside_allowlist"
	CategoryWrapperPermissionsUnsafe   = "wrapper_permissions_unsafe"
	CategoryEnvAllowlistUnsafe         = "env_allowlist_unsafe"
)

// Issue is a fail-closed validation finding with the security category used by
// the operator docs.
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

// ValidationError represents one or more registry validation issues.
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
		return "registry validation failed"
	}
	if len(e.Issues) == 1 {
		return e.Issues[0].String()
	}
	lines := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		lines = append(lines, issue.String())
	}
	return fmt.Sprintf("%d registry validation issues: %s", len(e.Issues), strings.Join(lines, "; "))
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
	return []Issue{{Category: CategorySchemaInvalid, Message: err.Error()}}
}
