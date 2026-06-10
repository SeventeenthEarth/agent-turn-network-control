package protocol

import "testing"

func TestUnitVersionFeaturesUseCanonicalDAEMN002Groups(t *testing.T) {
	features := NewVersionFeatures()
	seen := map[string]bool{}
	for _, group := range features.FeatureGroups {
		seen[group] = true
		if group == "stream.tail" || group == "version_features" {
			t.Fatalf("non-canonical feature group present: %s", group)
		}
	}
	for _, want := range []string{
		FeatureVersionRead,
		FeatureStatusRead,
		FeatureDiagnosticsRead,
		FeatureCommandEnvelope,
		FeatureEventEnvelope,
		FeatureStructuredError,
		FeatureStreamFrame,
		FeatureStreamReplay,
		FeatureStreamFollow,
		FeatureStreamAck,
		FeatureStreamStatus,
		FeatureActiveSessionLock,
		FeatureDeliveryEvidence,
		FeatureConformanceFixtures,
		FeatureCouncilLifecycle,
	} {
		if !seen[want] {
			t.Fatalf("missing feature group %q in %#v", want, features.FeatureGroups)
		}
	}
}

func TestUnitFeatureCapabilityStateMarksReadOnlyAndWriteFeatures(t *testing.T) {
	state := FeatureCapabilityState(RequiredFeatureGroups)

	for _, feature := range []string{
		FeatureVersionRead,
		FeatureStatusRead,
		FeatureDiagnosticsRead,
		FeatureStreamReplay,
		FeatureStreamFollow,
		FeatureStreamStatus,
		FeatureTranscriptRender,
		FeatureExportBundle,
	} {
		assertFeatureReadOnly(t, state, feature, true)
	}

	for _, feature := range []string{
		FeatureStreamAck,
		FeatureActiveSessionLock,
		FeatureDeliveryEvidence,
		FeatureCouncilLifecycle,
	} {
		assertFeatureReadOnly(t, state, feature, false)
	}
}

func assertFeatureReadOnly(t *testing.T, state map[string]any, feature string, want bool) {
	t.Helper()
	capability, ok := state[feature].(map[string]any)
	if !ok {
		t.Fatalf("missing capability state for %s in %#v", feature, state)
	}
	if capability["state"] != "supported" {
		t.Fatalf("%s state = %v, want supported", feature, capability["state"])
	}
	if got := capability["read_only"]; got != want {
		t.Fatalf("%s read_only = %v, want %v", feature, got, want)
	}
}
