# KAN Conformance Fixtures

This directory contains core-owned protocol fixtures consumed by `kkachi-agent-network-plugin`.

Current status: manifest-only scaffold. BOOTS-001 preserves the directory as part of the control/plugin compatibility contract; concrete protocol fixture JSON files remain deferred until DAEMN-002 defines stable command envelope, stream frame, structured error, delivery evidence, and version/feature shapes.

- Manifest: `manifest.json`
- Protocol version: `kan-protocol-v1alpha0`
- Fixture files will be added as command envelope, stream frame, structured error, and delivery evidence contracts are implemented.

The plugin may copy fixtures for pinned tests, but the core manifest is the compatibility source.
