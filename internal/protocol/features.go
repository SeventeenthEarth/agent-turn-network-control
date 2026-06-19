package protocol

const (
	FeatureVersionRead         = "version.read"
	FeatureStatusRead          = "status.read"
	FeatureDiagnosticsRead     = "diagnostics.read"
	FeatureCommandEnvelope     = "command_envelope"
	FeatureEventEnvelope       = "event_envelope"
	FeatureStructuredError     = "structured_error"
	FeatureStreamFrame         = "stream_frame"
	FeatureStreamReplay        = "stream.replay"
	FeatureStreamFollow        = "stream.follow"
	FeatureStreamAck           = "stream.ack"
	FeatureStreamStatus        = "stream.status"
	FeatureActiveSessionLock   = "active_session.lock"
	FeatureDeliveryEvidence    = "delivery_evidence"
	FeatureConformanceFixtures = "conformance.fixtures"
	FeatureCouncilLifecycle    = "council.lifecycle"
	FeatureTranscriptRender    = "transcript.render"
	FeatureExportBundle        = "export.bundle"
)

var RequiredFeatureGroups = []string{
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
	FeatureTranscriptRender,
	FeatureExportBundle,
}

type VersionFeatures struct {
	SchemaVersion            int      `json:"schema_version"`
	ProtocolVersion          string   `json:"protocol_version"`
	DaemonVersion            string   `json:"daemon_version"`
	MinPluginProtocolVersion string   `json:"min_plugin_protocol_version"`
	LiveReadiness            bool     `json:"live_readiness"`
	Features                 []string `json:"features"`
	FeatureGroups            []string `json:"feature_groups"`
	FixtureManifest          string   `json:"fixture_manifest"`
}

func NewVersionFeatures() VersionFeatures {
	groups := append([]string(nil), RequiredFeatureGroups...)
	return VersionFeatures{
		SchemaVersion:            SchemaVersion,
		ProtocolVersion:          ProtocolVersion,
		DaemonVersion:            "daemn-002-local",
		MinPluginProtocolVersion: ProtocolVersion,
		LiveReadiness:            true,
		Features:                 append([]string(nil), groups...),
		FeatureGroups:            groups,
		FixtureManifest:          "testdata/conformance/manifest.json",
	}
}

func FeatureCapabilityState(featureGroups []string) map[string]any {
	state := make(map[string]any, len(featureGroups))
	for _, feature := range featureGroups {
		state[feature] = map[string]any{
			"state":     "supported",
			"read_only": readOnlyFeature(feature),
		}
	}
	return state
}

func readOnlyFeature(feature string) bool {
	switch feature {
	case FeatureVersionRead, FeatureStatusRead, FeatureDiagnosticsRead,
		FeatureStreamReplay, FeatureStreamFollow, FeatureStreamStatus,
		FeatureTranscriptRender, FeatureExportBundle,
		FeatureCommandEnvelope, FeatureEventEnvelope, FeatureStructuredError,
		FeatureStreamFrame, FeatureConformanceFixtures:
		return true
	default:
		return false
	}
}
