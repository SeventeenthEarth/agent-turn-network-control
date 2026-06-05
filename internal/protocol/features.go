package protocol

const (
	FeatureVersionRead         = "version.read"
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
)

var RequiredFeatureGroups = []string{
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
}

type VersionFeatures struct {
	SchemaVersion            int      `json:"schema_version"`
	ProtocolVersion          string   `json:"protocol_version"`
	DaemonVersion            string   `json:"daemon_version"`
	MinPluginProtocolVersion string   `json:"min_plugin_protocol_version"`
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
		Features:                 append([]string(nil), groups...),
		FeatureGroups:            groups,
		FixtureManifest:          "testdata/conformance/manifest.json",
	}
}
