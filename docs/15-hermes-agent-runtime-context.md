# Hermes Agent Runtime Context

## Status

This document is **non-normative**. It explains runtime context for reviewers who have never seen Hermes Agent. It is not the SOT for CLI commands, event schemas, security rules, or Release v1 scope. When this file and a SOT document disagree, the SOT wins.

Normative SOT for related topics:

- Release v1 scope and primary customer: `00-overview.md` and `09-implementation-epics.md`.
- CLI surface: `04-cli-spec.md`.
- Event schemas: `03-protocol-spec.md`.
- Security rules: `12-security.md`.

## Purpose

This document explains what this project means by **Hermes Agent** and how the current main-agent/sub-agent operating model works. It is written for external AI reviewers that have not seen the user's team-member profiles or the Hermes tool runtime.

**Hermes Agent is the primary customer of `hun`.** Every contract — CLI surface, daemon socket, registry shape, runner adapter, session lifecycle — is designed for Hermes Agent operation. See `00-overview.md#primary-customer` for the consequences this has on adapter scope and Release v1 boundaries. Reactive CLI tools (Claude Code, Codex CLI, Gemini CLI, OpenCode) are not first-class users; they may interact with the system only through the bundled Hermes skill, and Release v1 ships no dedicated runner adapter for them.

`hun` is being designed around this runtime model. Alternative proposals are welcome, but they should preserve the same product goals: real profile identity, durable state, auditable events, user-controlled escalation, and no silent substitution with fake role-play agents.

## What is a Hermes Agent?

In this project, a Hermes Agent is an autonomous AI runtime with:

- a persistent profile/persona and operating memory;
- access to tools for files, terminal commands, browser/web lookup, scheduled jobs, task lists, skills, and sometimes platform delivery such as Telegram;
- a conversation/session context with the user or with an orchestrator;
- the ability to call tools, inspect results, continue work, verify outputs, and report back;
- a library of reusable skills that encode project-specific procedures;
- optional profile wrappers such as `moderator`, `agent-1`, or `agent-2` that start that profile's real runtime.

A Hermes Agent is not just a single LLM completion. It is an agent loop around a model: observe context, choose a tool/action, execute it, observe the result, continue until the task is complete or blocked.

## Current main-agent role

The main agent in this workflow is the `moderator`, the orchestrator profile. Its responsibilities are:

1. understand the user's request;
2. decide whether to act directly, delegate, ask a named team member, or escalate;
3. load relevant skills before acting;
4. use tools to inspect files, run commands, create documents, schedule work, or communicate;
5. coordinate named team members without pretending that a temporary role prompt is the real team member;
6. verify outputs before reporting completion;
7. keep durable operational facts in memory or skills when they will matter again.

The main agent should not claim that work was done by a named team member unless that real profile/session was actually invoked.

## Ways the main agent can work today

The current Hermes runtime gives the main agent several ways to execute or coordinate work. They differ in identity, context continuity, latency, reliability, and suitability for this project.

### 1. Direct tool execution inside the main session

The main agent can directly use tools such as file read/write, patch, terminal, browser, web extraction, todo, memory, and skills.

Good for:

- document editing;
- code inspection;
- running tests;
- one-agent implementation work;
- deterministic file and shell operations.

Limits:

- it is still the main agent doing the work;
- it does not create a real separate team-member opinion;
- long-running collaboration can fragment the main user conversation.

### 2. Temporary delegated subagents

The main agent can spawn isolated subagents for independent tasks. These subagents can inspect files or reason in parallel and return a final summary.

Good for:

- parallel code review;
- independent research branches;
- large context isolation;
- reducing noise in the main session.

Limits:

- these are temporary workers, not the user's named Hermes team-member profiles;
- they do not have the full persistent identity of `agent-1`, `agent-2`, etc.;
- they generally cannot ask the user for clarification;
- they return summaries rather than participating in a durable shared discussion stream.

Project policy: useful as an implementation aid, but not accepted as a substitute for real named member participation.

### 3. Real member profile wrappers

The main agent can invoke a real profile wrapper, for example a profile-specific CLI command that starts/resumes a named Hermes Agent profile. This preserves the member identity better than temporary subagents.

Good for:

- asking a real team member profile for a review or opinion;
- preserving member-specific persona, skills, memory, and workspace;
- tasks where the user's organization cares who said what.

Limits:

- if invoked only as a one-shot subprocess, the member may not continuously observe the discussion;
- the caller must preserve session handles and resume correctly;
- without a shared event log, the transcript can become fragmented.

This is one reason `hun` exists.

### 4. Scheduled jobs

The main agent can create scheduled jobs that run later in fresh sessions with self-contained prompts.

Good for:

- daily briefings;
- periodic cleanup/audits;
- reminder-style automation;
- repeated monitoring tasks.

Limits:

- jobs run without the current chat context unless explicitly included;
- they should not be used for live council turns;
- autonomous cron runs cannot ask the user for clarification in the moment.

### 5. Platform delivery channels

Some profiles can report through Telegram or other platform integrations. This is useful for urgent blockers or scheduled reports.

Good for:

- notifying the user about blocked work;
- delivering scheduled summaries;
- reporting completion outside the current terminal/chat.

Limits:

- profile gateways must be isolated; one profile's gateway must not be restarted or modified while operating another profile;
- Telegram delivery is not the same as durable session state;
- the canonical event record should still live in `channel.jsonl` for this project.

### 6. Shared files and documents

The agents can coordinate through durable files: task documents, feedback files, artifacts, transcripts, and logs.

Good for:

- long-lived design context;
- human-readable review;
- artifact handoff;
- recovery after context loss.

Limits:

- file polling is less precise than an event stream;
- without an explicit protocol, it is easy to miss causality and turn order.

### 7. MCP or plugin integrations

Hermes may use MCP servers or plugins when configured. MCP is a useful standard for exposing tools/resources/prompts to LLM applications.

Good for:

- standardized tool/resource interfaces;
- external service integration;
- potential future adapter layer.

Limits for this project:

- MCP is not the source of truth for `hun`;
- requiring MCP first would couple the design to one integration surface;
- the current project priority is daemon plus HUN protocol client/contract, preferred Hermes plugin integration, and canonical CLI fallback, with MCP as a possible thin adapter later.

### 8. The proposed `hun` stream model

The target design adds a durable communication layer:

```text
main/moderator runtime / Hermes plugin
  -> HUN protocol client/contract typed commands
  -> hund durable event log and state engine
  -> HUN protocol client/contract stream, with hun stream as fallback
  -> member runtimes
  -> real profile wrappers / resumed AI sessions
  -> typed HUN commands through plugin protocol client, with CLI as fallback
```

Good for:

- real-time or near-real-time council participation;
- preserving turn order and causality;
- letting members decide when to raise a hand or respond;
- reconnect/replay through durable cursors;
- auditability and transcript generation;
- avoiding one-shot worker context loss.

Limits:

- more complex than a simple subprocess call;
- needs member runtime supervision and heartbeat handling;
- needs cursor, replay, schema migration, and failure policy.

## Preferred approach for this project

This project should proceed with the stream-driven member runtime model.

Priority order:

1. **Daemon plus protocol contract, Hermes plugin adapter, and canonical CLI fallback is the product boundary.** Agents prefer plugin tools/slash commands and can fall back to `hun`; direct daemon APIs are internal.
2. **Event log first.** `channel.jsonl` is the source of truth; SQLite is a projection.
3. **Stream for observation.** Moderator and members observe sessions via the HUN protocol client/contract, normally through the Hermes plugin; `hun stream` remains the canonical fallback with cursors and replay.
4. **Typed commands for writes.** Members do not mutate daemon internals; they use typed HUN commands such as `delegate clarify`, `delegate update`, `council hand-raise`, `council speak`, and `council vote`, exposed through plugin tools or canonical CLI commands.
5. **Real profile identity.** Named members must be real profiles/wrappers/runtimes, not temporary role-prompt simulations.
6. **Runner adapters are bounded helpers.** One-shot subprocess calls may be used inside a member runtime or for compatibility, but not as the primary council loop.
7. **Fail closed.** Unknown members, cursor gaps, unknown schema versions, storage corruption, and unsafe wrappers stop the affected flow rather than silently continuing.
8. **MCP later.** MCP can be added later as a thin adapter if it helps external integrations, but it is not the SOT.

## Why not only one-shot subprocess workers?

A one-shot worker model is attractive because it is simple:

```text
daemon -> spawn member wrapper -> capture answer -> store event
```

But it is weak for this product:

- members do not continuously observe the discussion;
- context depends on prompt reconstruction and session resume correctness;
- hand raising becomes artificial because the moderator has to ask each member every time;
- failure recovery is harder when the live participant is only a subprocess call;
- it blurs the difference between a real member runtime and a temporary prompt.

One-shot calls remain useful as bounded model-invocation adapters, but they should live under the member runtime model.

## Design questions external AI reviewers should consider

When reviewing or proposing alternatives, please address these questions explicitly:

1. How does a member observe new events without polling too slowly or missing context?
2. How is the member's last processed event cursor persisted?
3. How are writes validated and deduplicated?
4. What happens if a member runtime disconnects mid-council?
5. How does the design prove that `agent-1` was a real profile and not a simulated role prompt?
6. How are user escalations represented and delivered without losing the session state?
7. Which parts are product contracts and which are replaceable implementation details?
8. How would MCP, WebSocket, SSE, local sockets, or a task queue fit without replacing the daemon/protocol/plugin/CLI SOT?

## Reference documents

These references are not requirements, but they help explain the design space.

- Model Context Protocol specification: https://modelcontextprotocol.io/specification
  - Useful for understanding standardized LLM tool/resource/prompt integrations and JSON-RPC-based client/server architecture.
- JSON-RPC 2.0 specification: https://www.jsonrpc.org/specification
  - Useful if the daemon control channel uses request/response method calls.
- OpenAI Agents SDK docs: https://openai.github.io/openai-agents-python/
  - Useful for concepts such as agents, tools, handoffs, guardrails, tracing, and agent orchestration.
- LangGraph durable execution: https://docs.langchain.com/oss/python/langgraph/durable-execution
  - Useful for thinking about resumable long-running workflows and human-in-the-loop pauses.
- MDN Server-sent events: https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events
  - Useful for daemon-to-CLI event streaming if SSE is chosen internally.
- JSON Lines format: https://jsonlines.org/
  - Useful for `channel.jsonl`, stream frames, and append-only event logs.

## Terminology mapping

```text
Hermes Agent          = persistent tool-using AI runtime profile
main agent            = moderator/orchestrator
member agent          = real team-member profile such as agent-1 or agent-2
member runtime        = long-lived loop that watches the stream and acts for one member
runner adapter        = bounded invocation of a model/profile wrapper
subagent              = temporary delegated worker; useful, but not a named member
hund        = daemon owning state, locks, event log, stream hub
hun         = stable CLI used by humans and agents
channel.jsonl         = source-of-truth event log
stream cursor         = durable position in a session event stream
```
