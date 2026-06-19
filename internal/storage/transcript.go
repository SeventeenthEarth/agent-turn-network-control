package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"kkachi-agent-network-control/internal/registry"
)

const (
	TranscriptMarkdownFormat = "md"
	TranscriptJSONLFormat    = "jsonl"
	ExportsDirName           = "exports"
)

type ExportBundleOptions struct {
	OutputPath string
}

type ExportBundleResult struct {
	SessionID string   `json:"session_id"`
	BundleDir string   `json:"bundle_dir"`
	Files     []string `json:"files"`
}

type surfaceProjectionRow struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Evidence  string `json:"evidence,omitempty"`
}

func RenderTranscript(sessionDir string, metadata *SessionMetadata, format string) ([]byte, error) {
	if metadata == nil {
		loaded, err := LoadSessionYAML(sessionDir)
		if err != nil {
			return nil, err
		}
		metadata = loaded
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return nil, err
	}
	switch format {
	case TranscriptMarkdownFormat:
		return renderTranscriptMarkdown(metadata, index.Events)
	case TranscriptJSONLFormat:
		return renderTranscriptJSONL(index.Events)
	default:
		return nil, NewValidationError(CategoryInvalidEnvelope, "format", "transcript format must be md or jsonl")
	}
}

func BuildExportBundle(sessionDir string, metadata *SessionMetadata, opts ExportBundleOptions) (ExportBundleResult, error) {
	if metadata == nil {
		loaded, err := LoadSessionYAML(sessionDir)
		if err != nil {
			return ExportBundleResult{}, err
		}
		metadata = loaded
	}
	if err := safeSessionDirForAppend(sessionDir); err != nil {
		return ExportBundleResult{}, err
	}
	bundleDir, err := exportBundleDir(sessionDir, metadata.ID, opts.OutputPath)
	if err != nil {
		return ExportBundleResult{}, err
	}
	if err := validateBundleOutputTarget(bundleDir); err != nil {
		return ExportBundleResult{}, err
	}
	if err := os.MkdirAll(bundleDir, 0o700); err != nil {
		return ExportBundleResult{}, NewValidationError(CategoryPathUnsafe, bundleDir, err.Error())
	}
	if err := ensureSafeBundleDir(bundleDir); err != nil {
		return ExportBundleResult{}, err
	}

	files := []struct {
		name    string
		content []byte
	}{
		{name: "transcript.md"},
		{name: "transcript.jsonl"},
		{name: "brief.md"},
		{name: "session.json"},
		{name: "channel.jsonl"},
		{name: registry.SnapshotFileName},
		{name: "bundle_manifest.json"},
	}
	if files[0].content, err = RenderTranscript(sessionDir, metadata, TranscriptMarkdownFormat); err != nil {
		return ExportBundleResult{}, err
	}
	if files[1].content, err = RenderTranscript(sessionDir, metadata, TranscriptJSONLFormat); err != nil {
		return ExportBundleResult{}, err
	}
	files[2].content = renderBrief(metadata)
	if files[3].content, err = marshalIndentDeterministic(metadata); err != nil {
		return ExportBundleResult{}, NewValidationError(CategoryInvalidEnvelope, "session", err.Error())
	}
	if files[4].content, err = readSafeSessionFile(sessionDir, ChannelJSONLName); err != nil {
		return ExportBundleResult{}, err
	}
	if files[5].content, err = readSafeSessionFile(sessionDir, registry.SnapshotFileName); err != nil {
		return ExportBundleResult{}, err
	}
	index, err := ReadLogIndex(sessionDir, metadata)
	if err != nil {
		return ExportBundleResult{}, err
	}
	manifest := map[string]any{
		"session_id":                  metadata.ID,
		"protocol_export":             "transcript-export-v1",
		"source_event_log":            ChannelJSONLName,
		"registry_snapshot":           metadata.RegistrySnapshot,
		"includes_operator_evidence":  true,
		"selected_runner_accounting":  SelectedRunnerAccountingFromIndex(index),
		"summary_turn_accounting":     summaryTurnAccountingRows(index.Events),
		"surface_delivery_projection": visibleSurfaceProjectionRows(index.Events),
		"argument_graph_projection":   argumentGraphProjectionRows(index.Events),
		"files": []string{
			"transcript.md",
			"transcript.jsonl",
			"brief.md",
			"session.json",
			"channel.jsonl",
			registry.SnapshotFileName,
		},
	}
	if files[6].content, err = marshalIndentDeterministic(manifest); err != nil {
		return ExportBundleResult{}, NewValidationError(CategoryInvalidEnvelope, "bundle_manifest", err.Error())
	}

	written := make([]string, 0, len(files))
	for _, file := range files {
		if err := writeBundleFile(bundleDir, file.name, file.content); err != nil {
			return ExportBundleResult{}, err
		}
		written = append(written, file.name)
	}
	sort.Strings(written)
	_ = syncDirectoryBestEffort(bundleDir)
	return ExportBundleResult{SessionID: metadata.ID, BundleDir: bundleDir, Files: written}, nil
}

func renderTranscriptJSONL(events []EventEnvelope) ([]byte, error) {
	var out bytes.Buffer
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return nil, NewValidationError(CategoryInvalidEnvelope, event.EventID, err.Error())
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func renderTranscriptMarkdown(metadata *SessionMetadata, events []EventEnvelope) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", metadata.Title)
	fmt.Fprintf(&b, "- session_id: `%s`\n", metadata.ID)
	fmt.Fprintf(&b, "- session_type: `%s`\n", metadata.SessionType)
	fmt.Fprintf(&b, "- status: `%s`\n", metadata.Status)
	fmt.Fprintf(&b, "- phase: `%s`\n", metadata.State.Phase)
	fmt.Fprintf(&b, "- moderator: `%s`\n", metadata.Moderator)
	fmt.Fprintf(&b, "- participants: `%s`\n", strings.Join(sortedStrings(metadata.Participants), "`, `"))
	if metadata.Surface != nil {
		fmt.Fprintf(&b, "- surface: `%s`\n", mustCompactJSON(metadata.Surface))
	}
	if metadata.LinkedAuthority != nil {
		fmt.Fprintf(&b, "- linked_authority: `%s`\n", mustCompactJSON(metadata.LinkedAuthority))
	}
	fmt.Fprintf(&b, "- registry_snapshot_sha256: `%s`\n", metadata.RegistrySnapshot.SourceSHA256)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Runner And Cost Summary")
	cost := transcriptCostSummary(metadata, events)
	fmt.Fprintf(&b, "- runner_calls_total: `%d`\n", cost.RunnerCallsTotal)
	fmt.Fprintf(&b, "- usd_estimate_total: `%.6f`\n", cost.USDEstimateTotal)
	fmt.Fprintf(&b, "- missing_cost_runner_calls_total: `%d`\n\n", cost.MissingCostRunnerCallsTotal)
	renderSelectedRunnerAccountingSummary(&b, SelectedRunnerAccountingFromIndex(&LogIndex{Events: events}))
	renderVisibleSurfaceProjectionSummary(&b, events)
	renderArgumentGraphProjectionSummary(&b, events)
	fmt.Fprintln(&b, "## Events")
	for i, event := range events {
		fmt.Fprintf(&b, "\n### %03d `%s`\n\n", i, event.EventID)
		fmt.Fprintf(&b, "- type: `%s`\n", event.Type)
		fmt.Fprintf(&b, "- created_at: `%s`\n", event.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"))
		fmt.Fprintf(&b, "- phase: `%s`\n", event.Phase)
		fmt.Fprintf(&b, "- from: `%s`\n", event.From)
		fmt.Fprintf(&b, "- to: `%s`\n", strings.Join(sortedStrings(event.To), "`, `"))
		if event.CommandID != "" {
			fmt.Fprintf(&b, "- command_id: `%s`\n", event.CommandID)
		}
		if event.Runner != nil {
			fmt.Fprintf(&b, "- runner: `%s`\n", mustCompactJSON(event.Runner))
		}
		if len(event.Cost) > 0 {
			fmt.Fprintf(&b, "- cost: `%s`\n", string(event.Cost))
		}
		for _, key := range []string{"surface", "linked_authority", "linked_authority_result", "attendance", "agenda", "blocker", "blockers", "recipients", "review", "verdict"} {
			if value, ok := event.Payload[key]; ok {
				fmt.Fprintf(&b, "- %s: `%s`\n", key, mustCompactJSON(value))
			}
		}
		payload, err := marshalIndentDeterministic(event.Payload)
		if err != nil {
			return nil, NewValidationError(CategoryInvalidEnvelope, event.EventID+".payload", err.Error())
		}
		fmt.Fprintln(&b, "\n```json")
		b.Write(bytes.TrimSpace(payload))
		fmt.Fprintln(&b, "\n```")
	}
	return []byte(b.String()), nil
}

func transcriptCostSummary(metadata *SessionMetadata, events []EventEnvelope) CostSummary {
	if len(events) == 0 {
		return metadata.Cost
	}
	summary := CostSummary{}
	for _, event := range events {
		if event.Runner == nil {
			continue
		}
		if event.Type == "runner_invocation_started" {
			summary.RunnerCallsTotal++
			continue
		}
		cost := parseCost(event.Cost)
		if cost.nullOrMissing {
			summary.MissingCostRunnerCallsTotal++
			continue
		}
		if cost.hasTokensIn {
			summary.TokensInTotal += cost.tokensIn
		}
		if cost.hasTokensOut {
			summary.TokensOutTotal += cost.tokensOut
		}
		if cost.hasUSD {
			summary.USDEstimateTotal += cost.usd
		}
	}
	return summary
}

func renderSelectedRunnerAccountingSummary(b *strings.Builder, accounting SelectedRunnerAccounting) {
	if accounting.SelectedSpeakerCount == 0 && accounting.RunnerStartedCount == 0 && accounting.RunnerlessSpeechCount == 0 {
		return
	}
	fmt.Fprintln(b, "## Selected Runner Accounting")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "- selected_runner_pass: `%t`\n", accounting.SelectedRunnerPass)
	fmt.Fprintf(b, "- selected_speaker_count: `%d`\n", accounting.SelectedSpeakerCount)
	fmt.Fprintf(b, "- runner_started_count: `%d`\n", accounting.RunnerStartedCount)
	fmt.Fprintf(b, "- runner_succeeded_count: `%d`\n", accounting.RunnerSucceededCount)
	fmt.Fprintf(b, "- runner_failed_count: `%d`\n", accounting.RunnerFailedCount)
	fmt.Fprintf(b, "- terminal_discard_count: `%d`\n", accounting.TerminalDiscardCount)
	fmt.Fprintf(b, "- dispatch_failure_count: `%d`\n", accounting.DispatchFailureCount)
	fmt.Fprintf(b, "- linked_runner_speech_count: `%d`\n", accounting.LinkedRunnerSpeechCount)
	fmt.Fprintf(b, "- runnerless_speech_count: `%d`\n", accounting.RunnerlessSpeechCount)
	fmt.Fprintf(b, "- manual_or_fallback_speech_count: `%d`\n", accounting.ManualOrFallbackSpeechCount)
	if len(accounting.Diagnostics) > 0 {
		fmt.Fprintln(b, "- diagnostics:")
		for _, diagnostic := range accounting.Diagnostics {
			fmt.Fprintf(b, "  - `%s`", diagnostic.Code)
			if diagnostic.EventID != "" {
				fmt.Fprintf(b, " event=`%s`", diagnostic.EventID)
			}
			if diagnostic.SelectedEventID != "" {
				fmt.Fprintf(b, " selected=`%s`", diagnostic.SelectedEventID)
			}
			if diagnostic.RunnerInvocationID != "" {
				fmt.Fprintf(b, " invocation=`%s`", diagnostic.RunnerInvocationID)
			}
			fmt.Fprintln(b)
		}
	}
	fmt.Fprintln(b)
}

func renderVisibleSurfaceProjectionSummary(b *strings.Builder, events []EventEnvelope) {
	rows := visibleSurfaceProjectionRows(events)
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(b, "## Visible Surface Projection Summary")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| event_id | type | target | status | evidence |")
	fmt.Fprintln(b, "| --- | --- | --- | --- | --- |")
	for _, row := range rows {
		fmt.Fprintf(b, "| `%s` | `%s` | `%s` | `%s` | `%s` |\n", row.EventID, row.EventType, row.Target, row.Status, escapeMarkdownTableCell(row.Evidence))
	}
	fmt.Fprintln(b)
}

func visibleSurfaceProjectionRows(events []EventEnvelope) []surfaceProjectionRow {
	selectedByTurn := map[int]string{}
	rows := make([]surfaceProjectionRow, 0)
	for _, event := range events {
		if event.Type == "speaker_selected" {
			turn, ok := payloadInt(event.Payload, "turn")
			if ok && len(event.To) > 0 {
				selectedByTurn[turn] = event.To[0]
			}
			continue
		}
		switch event.Type {
		case "speech":
			turn, ok := payloadInt(event.Payload, "turn")
			selected := ""
			status := "floor_grant_missing_or_mismatched"
			if ok {
				selected = selectedByTurn[turn]
				if selected == event.From {
					status = "renderable"
				}
			}
			evidence := "turn=unknown selected=" + selected + " speaker=" + event.From
			if ok {
				evidence = fmt.Sprintf("turn=%d selected=%s speaker=%s", turn, selected, event.From)
			}
			rows = append(rows, surfaceProjectionRow{EventID: event.EventID, EventType: event.Type, Target: "speech", Status: status, Evidence: evidence})
		case "council_finalized", "council_unresolved", "session_cancelled":
			status, evidence := deliveryEvidenceStatus(event.Payload["surface_evidence"])
			rows = append(rows, surfaceProjectionRow{EventID: event.EventID, EventType: event.Type, Target: "visible_surface", Status: status, Evidence: evidence})
			if _, ok := event.Payload["linked_authority_result"]; ok {
				linkedStatus, linkedEvidence := deliveryEvidenceStatus(event.Payload["linked_authority_result"])
				rows = append(rows, surfaceProjectionRow{EventID: event.EventID, EventType: event.Type, Target: "linked_authority", Status: linkedStatus, Evidence: linkedEvidence})
			}
		}
	}
	return rows
}

func deliveryEvidenceStatus(value any) (string, string) {
	payload, ok := asStringMap(value)
	if !ok || len(payload) == 0 {
		return "missing/unproven", ""
	}
	status, _ := stringValue(payload["status"])
	status = strings.TrimSpace(status)
	if status != "" {
		switch status {
		case "posted":
			if !hasAnyEvidencePointer(payload, "final_message_id", "message_id", "message_ref", "kanban_comment_id", "vault_decision_note", "evidence") {
				status = "missing/unproven"
			}
		case "failed":
			if !hasAnyEvidencePointer(payload, "failure_reason", "reason") {
				status = "missing/unproven"
			}
		case "pending_followup":
			if !hasAnyEvidencePointer(payload, "followup_card_id", "pending_review", "handoff", "reason") {
				status = "missing/unproven"
			}
		default:
			status = "missing/unproven"
		}
	}
	if status == "" {
		switch {
		case hasAnyEvidencePointer(payload, "failure_reason", "reason"):
			status = "failed"
		case hasAnyEvidencePointer(payload, "followup_card_id", "pending_review", "handoff"):
			status = "pending_followup"
		case hasAnyEvidencePointer(payload, "final_message_id", "message_id", "message_ref", "kanban_comment_id", "vault_decision_note", "evidence"):
			status = "posted"
		default:
			status = "missing/unproven"
		}
	}
	return status, mustCompactJSON(payload)
}

func payloadInt(payload map[string]any, key string) (int, bool) {
	value, ok := payload[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func asStringMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		converted := make(map[string]any, len(typed))
		for key, val := range typed {
			converted[key] = val
		}
		return converted, true
	default:
		return nil, false
	}
}

func stringValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	default:
		return "", false
	}
}

func hasAnyEvidencePointer(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if key == "evidence" {
			if evidenceStringPointer(value) {
				return true
			}
			continue
		}
		if isNonEmptyEvidencePointer(value) {
			return true
		}
	}
	return false
}

func evidenceStringPointer(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []string:
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
		return false
	case []any:
		for _, item := range typed {
			text, ok := item.(string)
			if ok && strings.TrimSpace(text) != "" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func isNonEmptyEvidencePointer(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []string:
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
		return false
	case []any:
		for _, item := range typed {
			if isNonEmptyEvidencePointer(item) {
				return true
			}
		}
		return false
	case map[string]string:
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range typed {
			if isNonEmptyEvidencePointer(item) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func escapeMarkdownTableCell(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}

func renderBrief(metadata *SessionMetadata) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Brief: %s\n\n", metadata.Title)
	fmt.Fprintf(&b, "- session_id: `%s`\n", metadata.ID)
	fmt.Fprintf(&b, "- status: `%s`\n", metadata.Status)
	fmt.Fprintf(&b, "- phase: `%s`\n", metadata.State.Phase)
	fmt.Fprintf(&b, "- registry_snapshot_sha256: `%s`\n", metadata.RegistrySnapshot.SourceSHA256)
	return []byte(b.String())
}

func exportBundleDir(sessionDir, sessionID, outputPath string) (string, error) {
	if strings.TrimSpace(outputPath) == "" {
		return filepath.Join(filepath.Clean(sessionDir), ExportsDirName, sessionID+"-bundle"), nil
	}
	clean := filepath.Clean(outputPath)
	if strings.Contains(clean, "\x00") || strings.Contains(clean, "..") {
		return "", NewValidationError(CategoryPathUnsafe, "output", "output path must not contain NUL or dot-dot segments")
	}
	return clean, nil
}

func ensureSafeBundleDir(bundleDir string) error {
	info, err := os.Lstat(bundleDir)
	if err != nil {
		return NewValidationError(CategoryPathUnsafe, bundleDir, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return NewValidationError(CategoryPathUnsafe, bundleDir, "bundle directory symlinks are forbidden")
	}
	if !info.IsDir() {
		return NewValidationError(CategoryPathUnsafe, bundleDir, "bundle path is not a directory")
	}
	return nil
}

func validateBundleOutputTarget(bundleDir string) error {
	clean := filepath.Clean(bundleDir)
	if filepath.Dir(clean) == clean {
		return NewValidationError(CategoryPathUnsafe, clean, "bundle output directory must not be a filesystem root")
	}
	info, err := os.Lstat(clean)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return NewValidationError(CategoryPathUnsafe, clean, "bundle directory symlinks are forbidden")
		}
		if !info.IsDir() {
			return NewValidationError(CategoryPathUnsafe, clean, "bundle path is not a directory")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return NewValidationError(CategoryPathUnsafe, clean, err.Error())
	}
	return nil
}

func writeBundleFile(bundleDir, name string, content []byte) error {
	path := filepath.Join(bundleDir, name)
	if !pathContains(bundleDir, path) {
		return NewValidationError(CategoryPathUnsafe, path, "bundle file escapes output directory")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return NewValidationError(CategoryPathUnsafe, path, "bundle file must be regular non-symlink")
		}
	} else if !os.IsNotExist(err) {
		return NewValidationError(CategoryPathUnsafe, path, err.Error())
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return NewValidationError(CategoryPathUnsafe, tmp, err.Error())
	}
	if err := os.Rename(tmp, path); err != nil {
		return NewValidationError(CategoryPathUnsafe, path, err.Error())
	}
	return nil
}

func readSafeSessionFile(sessionDir, name string) ([]byte, error) {
	path := filepath.Join(filepath.Clean(sessionDir), name)
	if !pathContains(sessionDir, path) {
		return nil, NewValidationError(CategoryPathUnsafe, path, "session file escapes session directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, NewValidationError(CategorySessionUnsafe, path, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, NewValidationError(CategoryPathUnsafe, path, "session file must be regular non-symlink")
	}
	return os.ReadFile(path)
}

func marshalIndentDeterministic(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}

func mustCompactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("marshal_error:%s", err)
	}
	return string(data)
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
