# Tooling

## Scope

This document defines the control repository toolchain after the repo split. The control daemon and CLI are implemented in Go. The Python Hermes plugin tooling lives in `../../kkachi-agent-network-plugin/docs/04-tooling.md`.

## Baseline decisions

| Item | Decision |
| --- | --- |
| Control language | Go |
| Binaries | `kkachi-agent-networkd`, `kkachi-agent-network` |
| Source layout | `cmd/`, `internal/`, `pkg/` only if public API is needed |
| Protocol fixtures | `testdata/conformance/` |
| Test runner | `go test` |
| Formatting | `gofmt` |
| Vet/static checks | `go vet`; optional `golangci-lint` when configured |
| Operator entrypoint | `Makefile` |

## Target layout

```text
kkachi-agent-network-control/
  go.mod
  cmd/
    kkachi-agent-network/
      main.go
    kkachi-agent-networkd/
      main.go
  internal/
    command/
    cli/
    daemon/
    engine/
    observability/
    protocol/
    recovery/
    registry/
    runner/
    storage/
    transcript/
    transport/
  testdata/
    conformance/
  tests/
    integration/
    e2e/
  docs/
  Makefile
```

## Makefile targets

```bash
make test-prepare
make test-unit
make test-int
make test-e2e
make test
```

`test-prepare` performs `gofmt` checks, lint, `go vet`, and docs/guardrail checks. It must not use external resources.

`test-unit` runs unit tests only.

`test-int` runs integration tests using fake runners, fake gateways, temporary data homes, and deterministic clocks. It must not use external Hermes or Discord resources.

`test-e2e` runs real external integration only when a test environment is explicitly configured. It must not touch the currently running Hermes profile/gateway or production Discord rooms.

`test` runs the four targets sequentially.

## Bootstrap smoke tests

The first Go scaffold PR must prove:

- `go test ./...` passes.
- `go vet ./...` passes.
- `gofmt` reports no changed files.
- `kkachi-agent-network --help` exits 0.
- `kkachi-agent-networkd --help` exits 0.
- `make test` succeeds without external resources in docs/scaffold mode.

## Guardrails

The control repo docs must not reintroduce pre-split Python-core assumptions. Guardrails should reject stale wording that says the control repo is a Python package or that CLI/plugin share a Python client implementation. The valid split is: Go control runtime, Python plugin, shared protocol contract, conformance tests.
