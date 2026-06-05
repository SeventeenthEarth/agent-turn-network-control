package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	ExitOK                = 0
	ExitUsage             = 1
	ExitDaemonUnavailable = 2
	ExitUnsafe            = 3
	ExitActiveSession     = 4
	ExitReserved          = 5
	ExitStorage           = 6
	ExitInternal          = 70
)

const (
	ErrorUnsupportedFeature = "unsupported_feature"
	ErrorDaemonUnavailable  = "daemon_unavailable"
	ErrorUnsafe             = "unsafe_runtime"
	ErrorStorage            = "storage_failure"
	ErrorValidation         = "validation_error"
	ErrorInternal           = "internal_error"
)

// StructuredError is the stable JSON error schema shared by the CLI and daemon.
type StructuredError struct {
	Code     string         `json:"code"`
	Category string         `json:"category"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
	Next     []string       `json:"next,omitempty"`
	ExitCode int            `json:"-"`
}

type jsonErrorEnvelope struct {
	Error *StructuredError `json:"error"`
}

func (e *StructuredError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func NewError(code, message string, exitCode int, details map[string]any) *StructuredError {
	return NewCategorizedError(code, categoryForCode(code), message, exitCode, details, nil)
}

func NewCategorizedError(code, category, message string, exitCode int, details map[string]any, next []string) *StructuredError {
	return &StructuredError{
		Code:     code,
		Category: category,
		Message:  message,
		ExitCode: exitCode,
		Details:  cloneDetails(details),
		Next:     append([]string(nil), next...),
	}
}

func UnsupportedFeature(feature string) *StructuredError {
	return NewError(ErrorUnsupportedFeature, fmt.Sprintf("%s is not implemented in DAEMN-001", feature), ExitUsage, map[string]any{"feature": feature})
}

func DaemonUnavailable(message string) *StructuredError {
	if message == "" {
		message = "daemon is unavailable"
	}
	return NewError(ErrorDaemonUnavailable, message, ExitDaemonUnavailable, nil)
}

func UnsafeRuntime(message string, details map[string]any) *StructuredError {
	return NewError(ErrorUnsafe, message, ExitUnsafe, details)
}

func StorageFailure(message string, details map[string]any) *StructuredError {
	return NewError(ErrorStorage, message, ExitStorage, details)
}

func InternalError(message string) *StructuredError {
	if message == "" {
		message = "internal error"
	}
	return NewError(ErrorInternal, message, ExitInternal, nil)
}

func ClassifyExit(err error) int {
	if err == nil {
		return ExitOK
	}
	var structured *StructuredError
	if errors.As(err, &structured) && structured.ExitCode != 0 {
		return structured.ExitCode
	}
	return ExitInternal
}

func WriteJSONError(err error) []byte {
	structured := ToStructuredError(err)
	data, marshalErr := json.Marshal(jsonErrorEnvelope{Error: structured})
	if marshalErr != nil {
		return []byte(`{"error":{"code":"internal_error","category":"internal","message":"marshal structured error"}}`)
	}
	return append(data, '\n')
}

func ToStructuredError(err error) *StructuredError {
	if err == nil {
		return nil
	}
	var structured *StructuredError
	if errors.As(err, &structured) {
		copy := *structured
		if copy.Category == "" {
			copy.Category = categoryForCode(copy.Code)
		}
		copy.Details = cloneDetails(structured.Details)
		copy.Next = append([]string(nil), structured.Next...)
		return &copy
	}
	return InternalError(err.Error())
}

func categoryForCode(code string) string {
	switch code {
	case ErrorUnsupportedFeature, ErrorValidation:
		return "validation"
	case ErrorDaemonUnavailable:
		return "daemon"
	case ErrorUnsafe:
		return "security"
	case ErrorStorage:
		return "storage"
	case ErrorInternal:
		return "internal"
	default:
		return "validation"
	}
}

func cloneDetails(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
