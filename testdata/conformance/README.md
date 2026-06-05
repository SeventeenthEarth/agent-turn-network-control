# KAN Conformance Fixtures

This directory contains core-owned protocol fixtures consumed by `kkachi-agent-network-plugin`.

Current status: DAEMN-002 static/local draft conformance set. The files here define the shared protocol examples for command envelopes, event envelopes, structured errors, stream frames, version/features, and delivery evidence.

- Manifest: `manifest.json`
- Protocol version: `kan-protocol-v1alpha0`
- Schemas: `schemas/*.schema.json`
- Fixtures: `fixtures/{command,event,error,stream,version}/`
- Canonical stream command fixture: `stream.replay`

These fixtures are static only. They do not start a daemon and do not contact Hermes, Discord, KAB, auth, token, gateway, or other live services. The plugin may copy fixtures for pinned tests, but the core manifest is the compatibility source.
