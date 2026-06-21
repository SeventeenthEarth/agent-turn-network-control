package protocol_test

import (
	"encoding/json"
	"errors"
	"testing"

	"hun-control/internal/protocol"
)

func TestUnitExitTaxonomyAndStructuredUnsupportedFeature(t *testing.T) {
	if got := []int{protocol.ExitOK, protocol.ExitUsage, protocol.ExitDaemonUnavailable, protocol.ExitUnsafe, protocol.ExitActiveSession, protocol.ExitReserved, protocol.ExitStorage, protocol.ExitInternal}; len(got) != 8 {
		t.Fatalf("taxonomy missing entries: %v", got)
	}
	err := protocol.UnsupportedFeature("stream")
	if err.Code != protocol.ErrorUnsupportedFeature {
		t.Fatalf("expected unsupported_feature, got %q", err.Code)
	}
	if err.Category != "validation" {
		t.Fatalf("expected validation category, got %q", err.Category)
	}
	if protocol.ClassifyExit(err) != protocol.ExitUsage {
		t.Fatalf("expected unsupported feature to use validation exit 1")
	}
	var envelope struct {
		Error struct {
			Code     string         `json:"code"`
			Category string         `json:"category"`
			Message  string         `json:"message"`
			Details  map[string]any `json:"details"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal(protocol.WriteJSONError(err), &envelope); decodeErr != nil {
		t.Fatalf("decode JSON error envelope: %v", decodeErr)
	}
	if envelope.Error.Code != "unsupported_feature" || envelope.Error.Category != "validation" {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
	if envelope.Error.Details["feature"] != "stream" {
		t.Fatalf("expected feature detail, got %+v", envelope.Error.Details)
	}
}

func TestUnitInternalErrorFallbackClassifiesExitSeventy(t *testing.T) {
	if got := protocol.ClassifyExit(errors.New("boom")); got != protocol.ExitInternal {
		t.Fatalf("expected internal exit 70, got %d", got)
	}
}
