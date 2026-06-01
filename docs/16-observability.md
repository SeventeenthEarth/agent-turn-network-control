# Observability

## Scope

This document defines what an operator can see while `kkachi-agent-network` is running: health signals, metrics, structured logs, suggested SLOs/SLIs, alert thresholds, and dashboard organization. It is intended for local single-host deployments. It does not define a metrics export protocol; the metrics names below are stable identifiers that an exporter (Prometheus, OpenTelemetry, or a simple JSON endpoint) can map to its own format.

The normative SOT documents that observability depends on:

- Health and lifecycle: `02-architecture.md`, `06-state-machine.md`.
- Stream and runner accounting: `13-operational-contracts.md`.
- Errors and structured output: `04-cli-spec.md`.
- Security and operational log: `12-security.md`.

## Health model

Each daemon health probe answers one question:

| Question                                                | Answer source                                                            |
| ------------------------------------------------------- | ------------------------------------------------------------------------ |
| Is the daemon running and accepting commands?           | `kkachi-agent-network daemon status`                                            |
| Is the registry safe and parseable?                     | `kkachi-agent-network doctor`                                                   |
| Is the active session lock consistent?                  | `kkachi-agent-network doctor`, `kkachi-agent-network status`                           |
| Is `channel.jsonl` parseable and projection-consistent? | `kkachi-agent-network storage verify`                                           |
| Is replay reproducible?                                 | `kkachi-agent-network storage rebuild-projection` after `storage verify` passes |
| Are stream subscribers up to date?                      | `kkachi-agent-network status --verbose`                                         |

`kkachi-agent-network doctor` is the operator-facing aggregate. It is read-only by default and must not chmod, chown, rewrite, or delete anything without an explicit repair flag (per `12-security.md`).

## Metrics catalog

The daemon emits a set of stable metric identifiers. Implementations may expose them via Prometheus, OpenTelemetry, or another exporter; the names below are the contract.

| Area       | Metric                                        | Type      | Description                                            |
| ---------- | --------------------------------------------- | --------- | ------------------------------------------------------ |
| Daemon     | `kkachi_agent_network_daemon_ready`                  | gauge     | 1 when daemon can accept commands; 0 otherwise         |
| Daemon     | `kkachi_agent_network_active_sessions`               | gauge     | Active session count (Release v1: 0 or 1)              |
| Event log  | `kkachi_agent_network_event_append_latency_ms`       | histogram | JSONL append latency                                   |
| Event log  | `kkachi_agent_network_event_appends_total`           | counter   | Count of appended events, labeled by `type`            |
| Projection | `kkachi_agent_network_projection_replay_duration_ms` | histogram | SQLite rebuild or replay duration                      |
| Stream     | `kkachi_agent_network_stream_subscriber_count`       | gauge     | Active stream subscribers                              |
| Stream     | `kkachi_agent_network_stream_lag_events`             | gauge     | Latest event cursor minus acknowledged cursor          |
| Stream     | `kkachi_agent_network_stream_subscriber_stale_total` | counter   | Count of `stream_subscriber_stale` emissions           |
| Runner     | `kkachi_agent_network_runner_invocations_total`      | counter   | Count of `runner_invocation_started`                   |
| Runner     | `kkachi_agent_network_runner_failures_total`         | counter   | Runner terminal failures, labeled by `reason`          |
| Runner     | `kkachi_agent_network_runner_missing_cost_total`     | counter   | Terminal runner events with `cost: null`               |
| Runner     | `kkachi_agent_network_runner_duration_ms`            | histogram | Runner invocation duration                             |
| Escalation | `kkachi_agent_network_waiting_user_age_seconds`      | gauge     | Age of current `waiting_user` state, 0 when not active |
| Escalation | `kkachi_agent_network_pending_escalation_batches`    | gauge     | Pending batch count                                    |
| Escalation | `kkachi_agent_network_user_escalations_total`        | counter   | Count of `user_escalation_requested`                   |
| Block      | `kkachi_agent_network_blocked_session_age_seconds`   | gauge     | Age of current `blocked` state, 0 when not blocked     |
| Security   | `kkachi_agent_network_security_violations_total`     | counter   | Security violations, labeled by `category`             |
| Storage    | `kkachi_agent_network_replay_failures_total`         | counter   | Replay failures, labeled by `reason`                   |
| Storage    | `kkachi_agent_network_jsonl_bytes`                   | gauge     | Size of the active session's `channel.jsonl`           |

Counters increment monotonically and reset only when the daemon is stopped and storage is rebuilt. Gauges reflect the current state at scrape time.

## SLO and SLI guidance

Default targets for a single-host deployment. Operators may relax these for development environments.

- Local event append p95 < 100 ms (`kkachi_agent_network_event_append_latency_ms`).
- Stream delivery p95 < 1 s after append (latest event cursor is observed by all live subscribers within 1 s).
- Stream reconnect replays missed events without silent skip (verified by integration tests; cursor gap fails closed).
- Projection rebuild from 10,000 events completes within a documented local benchmark target (recorded by `18-testing-strategy.md` load tests).
- `kkachi_agent_network_daemon_ready` is 1 for ≥ 99% of operator-active hours.
- `kkachi_agent_network_runner_failures_total` rate over 1 hour is bounded by session budgets and is reported in `kkachi-agent-network status --verbose`.

These are operator-facing targets, not protocol invariants. Failing an SLO is an alert signal, not a daemon fault.

## Alerting guidance

Recommended local alerts. They map to operator action, not to daemon recovery (the daemon is fail-closed by design).

- `daemon_ready == 0` for more than 60 s — daemon down or registry unsafe.
- `blocked_session_age_seconds` exceeds a configured threshold (e.g. 24 h) — session has been blocked too long.
- `waiting_user_age_seconds` > `escalation_response_timeout_sec * 0.8` — close to escalation timeout; the moderator should chase the user.
- `stream_subscriber_stale_total` increases — a member runtime stopped acknowledging cursors.
- `runner_missing_cost_total` increases — token and USD totals are becoming incomplete.
- `security_violations_total` increases — investigate immediately; do not suppress.
- `replay_failures_total` increases — possible storage corruption; run `kkachi-agent-network storage verify` and follow `17-disaster-recovery.md`.

## Dashboard guidance

A useful local dashboard groups widgets by concern:

- **Daemon health** — `daemon_ready`, `active_sessions`, daemon uptime, last `kkachi-agent-network doctor` result.
- **Active session** — current `phase` and `status`, session ID, blocked-session age, waiting-user age, pending escalation batches.
- **Throughput** — `event_appends_total` over time (rate), `event_append_latency_ms` p50/p95/p99.
- **Runner accounting** — `runner_invocations_total`, `runner_failures_total`, `runner_missing_cost_total`, current `runner_calls_total`, current token/USD totals.
- **Stream** — `stream_subscriber_count`, `stream_lag_events`, `stream_subscriber_stale_total`.
- **Storage** — `jsonl_bytes`, last replay duration, `replay_failures_total`.
- **Security** — `security_violations_total` by category.

## Structured logs

The daemon uses three log streams:

1. `channel.jsonl` per session — the durable event log. SOT for sessions.
2. `<data_home>/operational.log` — JSON Lines. Pre-session events, daemon lifecycle, subprocess audit. SOT for pre-session failures only (`12-security.md`).
3. Stdout/stderr — process-level supervision messages. Not durable. Should not be parsed for compliance.

Every operational log line includes `ts`, `level`, `event`, `category`, and a redacted `payload`. The redaction pipeline runs before write, so secret values never reach the operational log.

## Tracing and correlation

Causal correlation uses the existing protocol fields:

- `command_id` ties a CLI command to its produced events.
- `causation_event_id` ties a daemon-emitted event to the event that caused it.
- `correlation_id` ties an event back to the originating session or logical thread.
- `runner.invocation_id` ties runner accounting events together.

Operators should be able to assemble a per-session trace by filtering `channel.jsonl` and operational log lines by `session_id` or `correlation_id`. Distributed tracing (OpenTelemetry spans) is optional; if implemented, span IDs must not replace these protocol fields.

## Operational commands

Observability surfaces the following commands without weakening security:

- `kkachi-agent-network daemon status` — daemon liveness and version.
- `kkachi-agent-network doctor` — read-only health summary; `--repair-permissions` for explicit fixes.
- `kkachi-agent-network status` and `kkachi-agent-network status <session_id> --verbose` — session-level summary.
- `kkachi-agent-network limits show <session_id>` — budget/escalation accounting view.
- `kkachi-agent-network storage verify` — JSONL parse, schema, and projection consistency check.

These commands are part of `04-cli-spec.md`; this document only summarizes their observability role.

## Non-goals

- Observability must not require the daemon to deliver external notifications. Alert routing is an operator or gateway concern.
- The daemon must not become an alert manager, paging system, or notification gateway.
- Metrics must not contain secret values. The same redaction rules that protect `channel.jsonl` apply to any structured log or metric label.
- Observability must not introduce new event types that bypass the normal protocol. New observability data must be derived from existing events or operational log entries.
