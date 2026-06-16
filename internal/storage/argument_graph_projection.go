package storage

import (
	"encoding/json"
	"fmt"
	"strings"
)

type argumentGraphProjectionRow struct {
	SessionID              string `json:"session_id"`
	EventID                string `json:"event_id"`
	EventOrdinal           int    `json:"event_ordinal"`
	Turn                   int    `json:"turn"`
	Speaker                string `json:"speaker"`
	Speech                 string `json:"speech"`
	ContributionType       string `json:"contribution_type,omitempty"`
	NewAxisReason          string `json:"new_axis_reason,omitempty"`
	Status                 string `json:"status"`
	Diagnostic             string `json:"diagnostic,omitempty"`
	ClaimsJSON             string `json:"claims_json,omitempty"`
	StanceLinksJSON        string `json:"stance_links_json,omitempty"`
	EvidenceJSON           string `json:"evidence_json,omitempty"`
	QualityDiagnosticsJSON string `json:"quality_diagnostics_json,omitempty"`
	RelationAuditJSON      string `json:"relation_audit_json,omitempty"`
}

func argumentGraphProjectionRows(events []EventEnvelope) []argumentGraphProjectionRow {
	rows := make([]argumentGraphProjectionRow, 0)
	for ordinal, event := range events {
		if event.Type != "speech" {
			continue
		}
		rows = append(rows, argumentGraphProjectionRowFromEvent(ordinal, event))
	}
	return rows
}

func argumentGraphProjectionRowFromEvent(ordinal int, event EventEnvelope) argumentGraphProjectionRow {
	turn, _ := payloadInt(event.Payload, "turn")
	row := argumentGraphProjectionRow{
		SessionID:              event.SessionID,
		EventID:                event.EventID,
		EventOrdinal:           ordinal,
		Turn:                   turn,
		Speaker:                event.From,
		Speech:                 payloadStringDefault(event.Payload, "speech", ""),
		Status:                 "projected",
		Diagnostic:             "",
		ClaimsJSON:             "null",
		StanceLinksJSON:        "null",
		EvidenceJSON:           "null",
		QualityDiagnosticsJSON: "null",
		RelationAuditJSON:      "null",
	}
	diagnostics := []string{}
	relationFieldsPresent := false

	if value, ok := event.Payload["claims"]; ok {
		relationFieldsPresent = true
		if argumentGraphClaimsArray(value) {
			row.ClaimsJSON = mustCompactJSON(value)
		} else {
			diagnostics = append(diagnostics, "malformed_claims")
		}
	}
	if value, ok := event.Payload["stance_links"]; ok {
		relationFieldsPresent = true
		if argumentGraphStanceLinksArray(value) {
			row.StanceLinksJSON = mustCompactJSON(value)
		} else {
			diagnostics = append(diagnostics, "malformed_stance_links")
		}
	}
	if value, ok := event.Payload["contribution_type"]; ok {
		relationFieldsPresent = true
		text, ok := value.(string)
		if !ok {
			diagnostics = append(diagnostics, "malformed_contribution_type")
		} else {
			text = strings.TrimSpace(text)
			if _, allowed := argumentGraphContributionTypes[text]; !allowed {
				diagnostics = append(diagnostics, "unsupported_contribution_type")
			} else {
				row.ContributionType = text
			}
		}
	}
	if value, ok := event.Payload["new_axis_reason"]; ok {
		relationFieldsPresent = true
		text, ok := value.(string)
		if !ok && value != nil {
			diagnostics = append(diagnostics, "malformed_new_axis_reason")
		} else {
			row.NewAxisReason = strings.TrimSpace(text)
		}
	}
	if value, ok := event.Payload["evidence"]; ok {
		relationFieldsPresent = true
		if argumentGraphEvidenceArray(value) {
			row.EvidenceJSON = mustCompactJSON(value)
		} else {
			diagnostics = append(diagnostics, "malformed_evidence")
		}
	}
	if value, ok := event.Payload["quality_diagnostics"]; ok {
		relationFieldsPresent = true
		if argumentGraphQualityDiagnosticsArray(value) {
			row.QualityDiagnosticsJSON = mustCompactJSON(value)
		} else {
			diagnostics = append(diagnostics, "malformed_quality_diagnostics")
		}
	}
	if audit, ok := argumentGraphRelationAudit(event.Payload); ok {
		relationFieldsPresent = true
		row.RelationAuditJSON = mustCompactJSON(audit)
	}

	if !relationFieldsPresent {
		diagnostics = append(diagnostics, "missing_argument_graph_fields")
	}
	if len(diagnostics) > 0 {
		row.Status = "diagnostic"
		row.Diagnostic = strings.Join(diagnostics, ",")
	}
	return row
}

func argumentGraphClaimsArray(value any) bool {
	return argumentGraphObjectArray(value, []string{"id", "claim_id", "text", "summary", "content"})
}

func argumentGraphStanceLinksArray(value any) bool {
	return argumentGraphObjectArray(value, []string{"target_event_id", "target_claim_id", "claim_id", "target", "stance"})
}

func argumentGraphQualityDiagnosticsArray(value any) bool {
	return argumentGraphObjectArray(value, []string{"code", "diagnostic", "message", "severity", "field", "detail"})
}

func argumentGraphObjectArray(value any, projectionKeys []string) bool {
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok || !argumentGraphObjectHasProjectionString(object, projectionKeys) {
				return false
			}
		}
		return true
	case []map[string]any:
		for _, item := range items {
			if !argumentGraphObjectHasProjectionString(item, projectionKeys) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func argumentGraphObjectHasProjectionString(object map[string]any, projectionKeys []string) bool {
	for _, key := range projectionKeys {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func argumentGraphEvidenceArray(value any) bool {
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			if !argumentGraphEvidenceElement(item) {
				return false
			}
		}
		return true
	case []string, []map[string]any:
		return true
	default:
		return false
	}
}

func argumentGraphEvidenceElement(value any) bool {
	switch value.(type) {
	case string, map[string]any:
		return true
	default:
		return false
	}
}

func argumentGraphRelationAudit(payload map[string]any) (map[string]any, bool) {
	out := map[string]any{}
	for _, key := range []string{"relation_audit", "argument_graph_audit"} {
		if value, ok := payload[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func renderArgumentGraphProjectionSummary(b *strings.Builder, events []EventEnvelope) {
	rows := argumentGraphProjectionRows(events)
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(b, "## Argument Graph Projection Summary")
	fmt.Fprintln(b)
	for _, row := range rows {
		fmt.Fprintf(b, "### `%s` T%d `%s`", row.EventID, row.Turn, row.Speaker)
		if row.ContributionType != "" {
			fmt.Fprintf(b, " -- `%s`", row.ContributionType)
		}
		fmt.Fprintln(b)
		fmt.Fprintln(b)
		fmt.Fprintf(b, "- status: `%s`\n", row.Status)
		if row.Diagnostic != "" {
			fmt.Fprintf(b, "- diagnostic: `%s`\n", row.Diagnostic)
		}
		if row.NewAxisReason != "" {
			fmt.Fprintf(b, "- new_axis_reason: %s\n", escapeMarkdownTableCell(row.NewAxisReason))
		}
		renderArgumentGraphClaims(b, row.ClaimsJSON)
		renderArgumentGraphStanceLinks(b, row.StanceLinksJSON)
		renderArgumentGraphJSONField(b, "evidence", row.EvidenceJSON)
		renderArgumentGraphJSONField(b, "quality_diagnostics", row.QualityDiagnosticsJSON)
		renderArgumentGraphJSONField(b, "relation_audit", row.RelationAuditJSON)
		fmt.Fprintln(b)
	}
}

func renderArgumentGraphClaims(b *strings.Builder, claimsJSON string) {
	var claims []map[string]any
	if claimsJSON == "" || claimsJSON == "null" || !decodeCompactJSON(claimsJSON, &claims) {
		return
	}
	for _, claim := range claims {
		id := strings.TrimSpace(anyString(claim, "claim_id"))
		summary := strings.TrimSpace(anyString(claim, "summary"))
		if id == "" && summary == "" {
			continue
		}
		fmt.Fprintf(b, "- claim `%s`: %s\n", id, escapeMarkdownTableCell(summary))
	}
}

func renderArgumentGraphStanceLinks(b *strings.Builder, linksJSON string) {
	var links []map[string]any
	if linksJSON == "" || linksJSON == "null" || !decodeCompactJSON(linksJSON, &links) {
		return
	}
	for _, link := range links {
		targetEventID := strings.TrimSpace(anyString(link, "target_event_id"))
		targetClaimID := strings.TrimSpace(anyString(link, "target_claim_id"))
		stance := strings.TrimSpace(anyString(link, "stance"))
		rationale := strings.TrimSpace(anyString(link, "rationale"))
		target := targetEventID
		if targetClaimID != "" {
			target += "/" + targetClaimID
		}
		fmt.Fprintf(b, "- link `%s` -> `%s`", stance, target)
		if rationale != "" {
			fmt.Fprintf(b, ": %s", escapeMarkdownTableCell(rationale))
		}
		fmt.Fprintln(b)
	}
}

func renderArgumentGraphJSONField(b *strings.Builder, label, value string) {
	if value == "" || value == "null" {
		return
	}
	fmt.Fprintf(b, "- %s: `%s`\n", label, escapeMarkdownTableCell(value))
}

func decodeCompactJSON(text string, out any) bool {
	err := json.Unmarshal([]byte(text), out)
	return err == nil
}
