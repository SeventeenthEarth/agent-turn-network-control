# Release v1 Acceptance

## Scope

Release v1 acceptance is a local control-repo gate. It provides deterministic evidence for storage, replay, observability, and recovery behavior in the Go control runtime using temporary data homes and fake/local fixtures.

It does not prove live plugin load, live Discord delivery, Hermes profile execution, KAB review, gateway config, credentials, tokens, auth, or production install readiness.

## Local gate

Run:

```bash
GOCACHE=/tmp/kkachi-go-build-cache make test-release-acceptance
```

The target runs:

```bash
KAN_TEST_MODE=release KAN_EXTERNAL=0 go test ./internal/command -run 'TestReleaseAcceptance' -count=1
```

It is included in `make test` because it is deterministic and local-only.

## Evidence covered

The release acceptance suite covers:

- `channel.jsonl` corruption: truncated final line, malformed mid-file JSON, duplicate `event_id`, and unsupported `schema_version`.
- Registry snapshot safety: missing or corrupt `registry_snapshot.yaml` fails closed, and replay/rebuild does not reinterpret historical sessions from a mutated live `registry.yaml`.
- Projection recovery: missing projection reports `recoverable_projection_only`, rebuild creates a valid projection, and unsafe projection paths fail closed.
- Replay/rebuild purity: rebuild does not append events, create runner rows, record outbound delivery events, or synthesize timer/timeout events.
- Active-session recovery: replay-derived lifecycle state overrides stale `session.yaml` phase/status in both stale-terminal and stale-open directions.
- Observability surfaces: `storage verify`, `doctor`, `daemon status`, root `status`, and `daemon health` provide actionable local diagnostics, remain read-only, and do not leak secrets or raw registry names.

Supporting tests outside the release target also cover operational-log redaction for unsafe registry startup rejection, unsafe data-home rejection without log writes, and storage verify/rebuild exit-category mapping.

## Required verification spine

Local release-readiness evidence should record these commands separately:

```bash
HOME=/Users/draccoon kkachi-agent-helper graph validate --json
HOME=/Users/draccoon kkachi-agent-helper project doctor --json
GOCACHE=/tmp/kkachi-go-build-cache codegraph status
GOCACHE=/tmp/kkachi-go-build-cache make check-plugin-contract
GOCACHE=/tmp/kkachi-go-build-cache make test-prepare
GOCACHE=/tmp/kkachi-go-build-cache make test
GOCACHE=/tmp/kkachi-go-build-cache make test-release-acceptance
GOCACHE=/tmp/kkachi-go-build-cache go test ./internal/storage ./internal/command ./internal/daemon -run 'Release|Reliability|Storage|Doctor|Replay|Corrupt|Lock' -count=1
git diff --check
```

Any skipped command must be reported as a verification gap. Passing this local spine still must not be described as live plugin, Discord, Hermes, KAB, credential, gateway, or production readiness.

## Load split

Default release acceptance keeps load bounded. A large 100k-event replay check is opt-in and should be recorded as separate load evidence or an explicit skip with reason.
