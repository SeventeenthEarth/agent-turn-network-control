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
