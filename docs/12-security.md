# Security Model

These rules are mandatory. Every violation must fail closed and be recorded. Whether the record goes to `channel.jsonl` as a session `security_violation` event or to the daemon operational log depends on whether the violation is tied to an active session; see Failure behavior below.

## Threat model

- The registry file is an execution-permission boundary. A writable registry grants subprocess execution under the current user.
- Because the registry controls member identity, wrapper executable paths, workspaces, adapter kinds, and environment allowlists, the registry file and its containing data directory are security-sensitive. Unsafe ownership, unsafe permissions, symlinks, or parse-time ambiguity must fail closed before the daemon accepts commands.
- Member wrappers run with full user privilege. A misconfigured or malicious registry is an RCE surface.
- Transcripts, artifacts, and raw logs may carry secrets. `channel.jsonl` is durable, so redaction must occur before writes.
- CLI arguments and Hermes plugin tool inputs from the moderator flow into event payloads and into runner prompts. They must be treated as untrusted data.

## Registry validation

### Registry file safety

`<data_home>/registry.yaml` is a security-sensitive execution configuration file. The daemon validates the registry file before parsing the YAML schema.

Required file properties:

- path is exactly `<data_home>/registry.yaml`;
- file exists;
- file is a regular file;
- file is not a symlink;
- owner is the current daemon user or root;
- file is not group-writable;
- file is not world-writable.

Recommended mode: `0600` (also acceptable: `0640`, `0644`). Group/world readable registry files are allowed because the registry must not contain secret values; secret values must never be placed in the registry, and `env_allowlist` contains variable names only.

### Data home safety

The resolved `<data_home>` must be a safe directory before the daemon reads the registry or writes runtime state.

Required directory properties:

- exists;
- is a directory;
- owner is the current daemon user;
- not group-writable;
- not world-writable.

Recommended mode: `0700`. If `<data_home>` does not exist, setup/init code may create it with mode `0700`. If `<data_home>` exists with unsafe ownership or permissions, the daemon fails closed and does not automatically `chmod` it during start; permission repair is allowed only through an explicit setup or repair command that reports the action.

`kkachi-agent-network doctor` is read-only by default. Permission repair may be offered only through an explicit `--repair-permissions` flag or through `kkachi-agent-network init`, both of which must report every change they make. Daemon start must not silently repair unsafe ownership or permissions.

### Registry symlink rule

Registry symlinks are forbidden. Unlike wrapper paths, which allow restricted canonicalization for Hermes alias workflows, `registry.yaml` is a fixed source-of-truth file and must be a regular file at the expected path. If `lstat(<data_home>/registry.yaml)` reports a symlink, validation fails with `registry_symlink_forbidden`.

### Registry load procedure

The daemon must reduce check/use ambiguity (TOCTOU) when loading the registry. Required procedure:

1. Resolve `<data_home>/registry.yaml`.
2. `lstat` the path.
3. Reject if the path is a symlink.
4. Open the file read-only.
5. `fstat` the opened file descriptor.
6. Verify regular file, owner, and mode on the opened file.
7. Read YAML content from the same file descriptor.
8. Parse and validate the strict registry schema.
9. Compute a SHA-256 hash of the loaded content.
10. Use that same loaded content for session creation and snapshot writing.

The daemon must not validate one file and parse another. If metadata observed before open and after open indicate an unexpected file replacement, validation fails with `registry_changed_during_load`.

File safety validation runs **before** YAML schema validation; schema validation runs only after file safety passes. Any validation failure aborts daemon start or session creation.

### Registry schema validation

- Registry YAML loads through a strict schema. Required per-member fields: `display_name`, `wrapper`, `workspace`, `role`, `enabled`, `adapter_kind`.
- The registry root requires `members` and may include only `schema_version`, `wrapper_path_allowlist`, and `secret_patterns`.
- `role` is a free-form short string. Recommended vocabulary: `moderator`, `assignee`, `reviewer`, `participant`, `observer`. Other values are accepted; the daemon does not derive permissions from `role` (it is informational and projected into `session_participants.role` for query/UI use only).
- Optional fields: `strengths`, `env_allowlist`, `notes`, `runtime_kind`, `autostart`, `stream_filter`.
- Unknown `runtime_kind` is rejected at load time when present. Supported initial value: `hermes-cli-stream`.
- Unknown keys are rejected, not ignored.
- Unknown `adapter_kind` is rejected at load time.
- Any validation failure aborts daemon start. The daemon never runs on a partially valid registry.
- Disabled members still pass schema, id, workspace, adapter/runtime, and env allowlist validation, but wrapper resolution is required only for enabled members.

Reserved principal names cannot be used as registry member ids. Reserved principals:

- `user`
- `kkachi-agent-networkd`

Examples rejected at registry load time: `members.user`, `members.kkachi-agent-networkd`. The daemon uses these reserved principals in event envelopes; allowing registry members with the same ids would make audit records ambiguous.

## Wrapper execution policy

- `wrapper` is either an absolute path on disk or a bare command name. Bare names are resolved through a configured `wrapper_path_allowlist`; the daemon never consults `$PATH`.
- The default `wrapper_path_allowlist` is `["/usr/local/bin", "/opt/hermes", "~/.local/bin"]`. `~/.local/bin` is **required** in the default list because Hermes Agent creates per-agent alias binaries there (e.g. `wolong` for a member named `wolong`); removing it would block the documented Hermes alias workflow.
- `~/.local/bin` is user-writable. The residual risk is mitigated by the per-file checks below (regular file, owned by the current user or root, not group- or world-writable, single-symlink canonicalization) and by the env allowlist. Operators who do not use Hermes aliases may shrink the allowlist by configuration.
- After resolution, the daemon canonicalizes the path (following a single symlink) and verifies: the target exists, is a regular executable file, is not group- or world-writable, is owned by the current user or root, and lies under the allowlist after canonicalization.
- Resolution and canonicalization failures are distinct `security_violation` categories: `wrapper_unresolvable`, `wrapper_outside_allowlist`, `wrapper_permissions_unsafe`.
- Wrappers are invoked with an argv list, never through a shell string. `shell=True` is forbidden.
- Prompt text is passed as an argv slot or on stdin. It is never interpolated into a command string.
- Working directory for the subprocess is the member `workspace`. Daemon never executes commands with `cwd` outside the workspace or the session directory.

## Workspace isolation

- The daemon does not write into member workspaces.
- Artifact ingestion occurs only through explicit `work_submitted.artifacts` entries.
- Artifact paths are canonicalized. After canonicalization the path must lie under the member's `workspace` or under the current session directory (`sessions/<id>/`). Paths containing `..`, paths escaping both roots, and symlinks leaving the permitted roots are all rejected.

## Artifact contract

For every artifact reference:

- source path, stored path (`sessions/<id>/artifacts/<artifact_id>`), size in bytes, SHA-256 hash, MIME sniff result, and ingestion timestamp are recorded in the `artifacts` table.
- Files exceeding `max_artifact_bytes` (default 25 MB) are rejected.
- MIME whitelist is session-configurable. Default allows text, source code, markdown, JSON, YAML, PDF, and common image formats.
- The daemon copies the file into the session directory. The `work_submitted` payload always references the copy, never the source.

## Environment sanitization

- Member subprocess environment is minimal by default: `PATH`, `HOME`, `LANG`, `LC_*`. Nothing else from the daemon environment is propagated.
- Additional variables are passed only when explicitly listed in the member `env_allowlist`.
- Variables matching known secret prefixes/patterns (`*_API_KEY`, `*_TOKEN`, `*_SECRET`, plus the user-configurable `secret_patterns` regex list) are **blocked by default** even if present in the daemon environment. They are passed to the subprocess only when explicitly listed in `env_allowlist`.
- Variables that match a secret pattern *and* are explicitly listed in `env_allowlist` are passed to the subprocess in plaintext, but every reference to them in the operational log (`<data_home>/operational.log`) records only the variable name with the value rendered as `<redacted>`. The plaintext value never reaches `channel.jsonl`, `raw_logs/`, transcripts, or any export bundle.
- Forbidden in `env_allowlist`: literal values for secrets, glob patterns wider than a single variable name, and any name beginning with `LD_` or `DYLD_` (those override loader behavior and are rejected with `security_violation: env_allowlist_unsafe`).

## Secret redaction

- All runner `stdout`/`stderr` is scanned before being written to `raw_logs/`. Detected patterns include: Anthropic/OpenAI/Google API key prefixes, AWS access keys, JWT tokens, PEM private key headers, and user-configured regex.
- Matches are replaced with `<redacted:class>` in the durable text.
- Payload fields that carry user-supplied content (`question`, `answer`, `message`, `summary`, `speech`, `final_summary`) are scanned with the same rules before append to `channel.jsonl`.
- Every redaction emits a `redaction_applied` event with pattern class, count, and the source event id. The redacted value itself is never stored.

## Command-injection surface

- CLI arguments and Hermes plugin tool inputs flowing into event payloads and runner prompts are treated as opaque data. No CLI or plugin path interpolates payload content into shell commands.
- Registry-defined `workspace` is used only as a read reference for artifact source and as `cwd` for the wrapper invocation. It is never used as a base for arbitrary subprocess calls.
- CLI itself never spawns shells; the Hermes plugin must not spawn shells for normal KAN state mutations; only the daemon runner invokes subprocesses, and only through the adapter interface defined in `13-operational-contracts.md`.
- CLI-supplied and plugin-supplied `from` and `to` principals are treated as untrusted input. The daemon validates that `from` is an authorized originator for the command, that `to` contains only valid recipients for the session or the reserved external principal `user`, that `to` does not contain duplicates after normalization, and that `kkachi-agent-networkd` is not accepted as a normal participant-supplied recipient. Invalid principal references fail closed.


## Hermes plugin security boundary

- The Hermes plugin is not trusted as a state authority. It is an adapter over the KAN protocol client/contract and daemon command transport.
- The plugin must not append to `channel.jsonl`, write directly to `network.sqlite`, mutate daemon locks, or bypass daemon validation.
- Plugin load, reload, unload, Hermes gateway restart, or Hermes Agent restart must not mutate, corrupt, truncate, or otherwise alter `channel.jsonl`, SQLite projections, locks, or daemon state. Daemon durability is independent of plugin lifecycle.
- The plugin must not require KAN daemon access to Hermes profile secrets, gateway credentials, or Discord bot tokens. Gateway credentials remain inside Hermes/gateway configuration.
- Plugin tool inputs are untrusted and must receive the same validation, redaction, command-id/idempotency, and structured-error handling as CLI inputs.
- If the plugin shells out to `kkachi-agent-network` for a compatibility fallback, argv-only invocation is required and shell interpolation is forbidden. Normal operation should use the KAN protocol client/contract.
- Plugin failure must fail closed and direct the operator to canonical CLI diagnostics/recovery; it must not simulate member profiles or continue by writing private state.

## Discord-thread surface security

For `surface.kind: discord_thread`, Discord ids and message ids are untrusted evidence pointers:

- `kkachi-agent-networkd` must not require or store raw Discord bot tokens for the first-pass surface binding; Discord delivery belongs to Hermes plugin/gateway capability or a separately approved bridge.
- Raw Discord tokens, webhook secrets, or gateway credentials must never appear in `surface`, `linked_authority`, event payloads, transcripts, exports, `channel.jsonl`, or `operational.log`.
- If a future Discord bridge is approved, token scope, allowed guild/channel/thread validation, inbound message validation, and redaction must be specified as a separate transport/security design before implementation.
- Discord message order is not accepted as causality or lifecycle authority. The daemon uses `channel.jsonl` cursor order only.
- Thread ids, channel ids, guild ids, and message ids are opaque strings validated for shape/length only unless a future bridge adds explicit Discord API verification.

## Stream access policy

- A stream subscriber must identify as a registry member or as the moderator. Unknown member names are rejected.
- Stream frames are read-only. All writes use typed KAN commands, exposed through plugin tools or canonical CLI fallback, that re-enter normal daemon validation and idempotency.
- A member runtime may acknowledge only its own cursor. The moderator may inspect all cursors but should not advance a member cursor.
- Cursor gaps, unknown schema versions, or replay corruption fail closed. The daemon must not silently skip events to keep a stream alive.
- The event envelope `to` field is **semantic addressing**, not access control. A valid session participant may observe session events even if a specific event is not addressed to that participant. The daemon must not rely on `to` alone to decide stream read permissions; read permissions are based on registry identity, moderator authority, session participation, and the rules in this section.

## Failure behavior

All violations of this document must:

1. abort the affected dispatch or ingestion immediately,
2. record the violation with `category`, redacted `observed`, and `action`,
3. transition the session to `blocked` if the violation concerns an active session.

Recording target:

- Session-scoped violations emit a `security_violation` event to `channel.jsonl` under the affected `session_id`. When the violation transitions the session to `blocked`, the event envelope uses `phase: "blocked"` and the payload records `prior_phase` and `resume_phase` (see `13-operational-contracts.md` §5 and `03-protocol-spec.md`).
- Pre-session violations (registry load failure, daemon start failure, or any violation raised before a session is bound) write the same payload shape to the daemon operational log instead. They do **not** carry session `phase`/`status`/`prior_phase`/`resume_phase` because no session exists yet. This is the one documented case where the log — not `channel.jsonl` — is the system of record.

### Registry violation routing

Pre-session registry violations are written to `<data_home>/operational.log`:

- missing registry at daemon start;
- unsafe registry owner;
- group/world writable registry file;
- registry symlink;
- unsafe `<data_home>`;
- registry schema parse failure during daemon start;
- registry validation failure during session creation **before** the session directory is committed.

Session-scoped registry or snapshot violations emit `security_violation` to the active session's `channel.jsonl`:

- an active session's `registry_snapshot.yaml` is missing when dispatch requires it;
- an active session's snapshot is not a regular file;
- snapshot hash or schema validation fails during session-bound dispatch;
- snapshot read failure prevents safe member dispatch.

### Registry violation categories

Distinct from wrapper categories (`wrapper_unresolvable`, `wrapper_outside_allowlist`, `wrapper_permissions_unsafe`):

- `registry_missing`
- `registry_not_regular`
- `registry_symlink_forbidden`
- `registry_owner_unsafe`
- `registry_permissions_unsafe`
- `registry_data_home_unsafe`
- `registry_parse_error`
- `registry_schema_invalid`
- `registry_unknown_key`
- `registry_unknown_runtime_kind`
- `registry_unknown_adapter_kind`
- `registry_reserved_principal_collision`
- `registry_snapshot_write_failed`
- `registry_changed_during_load`

Category payload shape:

```json
{
  "category": "registry_permissions_unsafe",
  "observed": {
    "path": "<data_home>/registry.yaml",
    "mode": "0664",
    "owner_uid": 501
  },
  "action": "daemon_start_rejected"
}
```

Registry violation payloads must not include secret values. If a parse error includes content snippets, snippets must pass through the redaction pipeline before logging.

Release v1 validates the resolved `<data_home>` directory itself through `registry_data_home_unsafe`. It does not validate the full parent directory chain. If future releases add parent-chain validation, they must define exact rules, including symlink and sticky-bit behavior.

## Operational log

- Path: `<data_home>/operational.log`.
- Format: JSON Lines. Each line is one record with the keys `ts`, `level`, `event`, `category`, and a free-form `payload` object. The same redaction pipeline that protects `channel.jsonl` runs on every payload before it is written.
- Retention: 2 days from last activity (matches the raw runner log retention in `05-storage-schema.md`). Older lines are pruned by the daemon's housekeeping pass.
- Durability: operational, not durable. The system of record for anything that has a session is `channel.jsonl`; the operational log is only authoritative for pre-session events (per the rule above).

`kkachi-agent-network doctor` may read and summarize operational log records, but it must not expose secret values. Any displayed payload must pass through the same redaction rules as `channel.jsonl`.

## Auditability

Every subprocess invocation is logged to the operational log with: wrapper path, adapter kind, argv length, cwd, environment keys (names only — values listed in `env_allowlist` that match a secret pattern are recorded as `<redacted>`), start timestamp, exit code, duration, and truncated stdout/stderr byte counts. The corresponding durable session record begins with `runner_invocation_started` in `channel.jsonl`; a terminal semantic event, `runner_invocation_failed`, or `runner_result_discarded` later records the outcome using the same `runner.invocation_id`.

Cost parsing occurs only after stderr has passed through the redaction pipeline (per `Secret redaction` above). If cost cannot be parsed after redaction, the terminal runner event records `cost: null`. The daemon must **not** treat cost parse failure as proof that no runner invocation occurred — invocation accounting is anchored on `runner_invocation_started`, not on `cost.source`.
