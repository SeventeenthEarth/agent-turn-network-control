package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
)

const operationalLogName = "operational.log"

type operationalRecord struct {
	TS       string         `json:"ts"`
	Level    string         `json:"level"`
	Event    string         `json:"event"`
	Category string         `json:"category"`
	Payload  map[string]any `json:"payload"`
}

// RecordPreSessionViolation writes a redacted operational record only after the
// data home itself is safe. It intentionally fails silent so logging cannot
// weaken the fail-closed path that called it.
func RecordPreSessionViolation(dataHome string, runtime registry.Runtime, event, action string, err error) bool {
	if err == nil {
		return false
	}
	if validateErr := registry.ValidateDataHome(dataHome, runtime); validateErr != nil {
		return false
	}
	category, observed := violationDetails(err)
	if category == "" {
		category = protocol.ToStructuredError(err).Code
	}
	payload := map[string]any{
		"category": category,
		"observed": observed,
		"action":   action,
	}
	record := operationalRecord{
		TS:       now(runtime).Format(time.RFC3339Nano),
		Level:    "error",
		Event:    event,
		Category: category,
		Payload:  payload,
	}
	data, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		return false
	}
	path := filepath.Join(filepath.Clean(dataHome), operationalLogName)
	file, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	if _, writeErr := file.Write(append(data, '\n')); writeErr != nil {
		return false
	}
	_ = file.Chmod(0o600)
	return true
}

func violationDetails(err error) (string, map[string]any) {
	if !registry.IsValidationError(err) {
		return protocol.ToStructuredError(err).Code, map[string]any{"detail": "[REDACTED]"}
	}
	issues := registry.Issues(err)
	if len(issues) == 0 {
		return "", map[string]any{"detail": "[REDACTED]"}
	}
	issue := issues[0]
	observed := map[string]any{"detail": "[REDACTED]"}
	if issue.Path != "" {
		observed["path"] = issue.Path
	}
	return issue.Category, observed
}

func now(runtime registry.Runtime) time.Time {
	if runtime.Now != nil {
		return runtime.Now().UTC()
	}
	return time.Now().UTC()
}
