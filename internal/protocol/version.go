package protocol

const (
	// SchemaVersion is the initial durable protocol envelope version.
	SchemaVersion = 1

	// ProtocolVersion is the current cross-repo control/plugin protocol compatibility marker.
	ProtocolVersion = "atn-protocol-v1alpha0"

	// ControlVersion is the semantic version shared by the local CLI and daemon binaries.
	ControlVersion = "v0.1.0"

	// DaemonVersion is reported by daemon compatibility/version surfaces.
	DaemonVersion = ControlVersion
)
