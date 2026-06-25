package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"atn-control/internal/daemon"
	"atn-control/internal/protocol"
	"atn-control/internal/registry"
	"atn-control/internal/storage"
	"atn-control/internal/transport"
)

// App describes a minimal local-only command surface for the ATN binary.
type App struct {
	Name        string
	Description string
	Commands    []CommandSummary
	Runtime     registry.Runtime
	Kind        appKind
	StartDaemon func(dataHome string, runtime registry.Runtime) error
}

type appKind string

const (
	appKindCLI    appKind = "cli"
	appKindDaemon appKind = "daemon"
)

const (
	cliBinaryName     = "atn-control"
	daemonBinaryName  = "atn-controld"
	dataHomeEnvName   = "ATN_HOME"
	daemonPathEnvName = "ATN_CONTROLD_PATH"
)

// CommandSummary is a help-only command listing.
type CommandSummary struct {
	Name        string
	Description string
}

// NewCLI returns the canonical operator CLI.
func NewCLI() App {
	return NewCLIWithRuntime(registry.DefaultRuntime())
}

func NewCLIWithRuntime(runtime registry.Runtime) App {
	return App{
		Name:        cliBinaryName,
		Description: "Agent Turn Network control CLI for diagnostics, recovery, and manual operation.",
		Runtime:     runtime,
		Kind:        appKindCLI,
		StartDaemon: startDaemonProcess,
		Commands: []CommandSummary{
			{Name: "daemon", Description: "Manage or inspect the local daemon lifecycle."},
			{Name: "doctor", Description: "Run read-only diagnostics for the control runtime."},
			{Name: "init", Description: "Create a safe data home and sample registry when missing."},
			{Name: "registry", Description: "Validate or show the local registry authority."},
			{Name: "storage", Description: "Verify or rebuild the local SQLite storage projection."},
			{Name: "stream", Description: "Replay, follow, acknowledge, or inspect session event streams."},
			{Name: "compat", Description: "Read daemon-backed plugin compatibility version, status, and diagnostics."},
			{Name: "conformance", Description: "Show local protocol conformance fixtures."},
			{Name: "delegate", Description: "Create delegation sessions and record delegation audit events."},
			{Name: "council", Description: "Create council sessions and record council consensus events."},
			{Name: "cancel", Description: "Cancel an active session with a durable session_cancelled event."},
			{Name: "block", Description: "Block an active session until a resumable condition is resolved."},
			{Name: "resume", Description: "Resume a blocked session from its recorded resume phase."},
			{Name: "limits", Description: "Show or extend local session limits with explicit authorization."},
			{Name: "status", Description: "Show daemon or session status."},
			{Name: "transcript", Description: "Render a deterministic local session transcript."},
			{Name: "export", Description: "Create a deterministic local session export bundle."},
			{Name: "tail", Description: "Print recent session stream frames without appending events."},
			{Name: "version", Description: "Print protocol and binary version information."},
		},
	}
}

// NewDaemon returns the daemon binary command surface.
func NewDaemon() App {
	return App{
		Name:        daemonBinaryName,
		Description: "Agent Turn Network daemon authority for state transitions, event append, replay, and projections.",
		Runtime:     registry.DefaultRuntime(),
		Kind:        appKindDaemon,
		Commands: []CommandSummary{
			{Name: "run", Description: "Start the daemon foreground process."},
			{Name: "version", Description: "Print protocol and binary version information."},
		},
	}
}

// Run executes the local-only command behavior. Unsupported commands
// fail closed with the shared structured error schema.
func (a App) Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if a.Runtime.LookupEnv == nil {
		a.Runtime = registry.DefaultRuntime()
	}
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(stdout, a.Help())
		return 0
	}

	if args[0] == "version" {
		return a.runVersion(args[1:], stdout, stderr)
	}

	if a.Kind == appKindCLI {
		switch args[0] {
		case "daemon":
			return a.runDaemon(args[1:], stdout, stderr)
		case "init":
			return a.runInit(args[1:], stdout, stderr)
		case "doctor":
			return a.runDoctor(args[1:], stdout, stderr)
		case "registry":
			return a.runRegistry(args[1:], stdout, stderr)
		case "storage":
			return a.runStorage(args[1:], stdout, stderr)
		case "stream":
			return a.runStream(args[1:], stdout, stderr)
		case "compat":
			return a.runCompat(args[1:], stdout, stderr)
		case "conformance":
			return a.runConformance(args[1:], stdout, stderr)
		case "delegate":
			return a.runDelegate(args[1:], stdout, stderr)
		case "council":
			return a.runCouncil(args[1:], stdout, stderr)
		case "cancel":
			return a.runCancel(args[1:], stdout, stderr)
		case "block":
			return a.runBlock(args[1:], stdout, stderr)
		case "resume":
			return a.runResume(args[1:], stdout, stderr)
		case "limits":
			return a.runLimits(args[1:], stdout, stderr)
		case "status":
			return a.runStatus(args[1:], stdout, stderr)
		case "transcript":
			return a.runTranscript(args[1:], stdout, stderr)
		case "export":
			return a.runExport(args[1:], stdout, stderr)
		case "tail":
			return a.runTail(args[1:], stdout, stderr)
		}
	}
	if a.Kind == appKindDaemon {
		switch args[0] {
		case "run":
			return a.runDaemonForeground(args[1:], stdout, stderr)
		}
	}

	return writeProtocolError(stderr, protocol.UnsupportedFeature(args[0]))
}

// Help renders deterministic local help text.
func (a App) Help() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", a.Name)
	fmt.Fprintf(&b, "%s\n\n", a.Description)
	fmt.Fprintf(&b, "Usage:\n  %s [--help] <command> [options]\n\n", a.Name)
	fmt.Fprintln(&b, "Commands:")
	for _, cmd := range a.Commands {
		fmt.Fprintf(&b, "  %-12s %s\n", cmd.Name, cmd.Description)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Status: Local lifecycle, stream, and session controls use daemon-backed structured responses; live gateway/provider activation is not implied.")
	return b.String()
}

func (a App) runCompat(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s compat version --format json\n  %s compat status --format json\n  %s compat diagnostics --format json\n\nReads daemon-backed plugin compatibility responses without expanding concise operator status/health output.\n", a.Name, a.Name, a.Name)
		return protocol.ExitOK
	}
	if len(args) != 3 || args[1] != "--format" || args[2] != "json" {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "compat requires <version|status|diagnostics> --format json", protocol.ExitUsage, nil))
	}
	switch args[0] {
	case "version":
		return a.daemonRequest(stdout, stderr, protocol.FeatureVersionRead)
	case "status":
		return a.daemonRequest(stdout, stderr, protocol.FeatureStatusRead)
	case "diagnostics":
		return a.daemonRequest(stdout, stderr, protocol.FeatureDiagnosticsRead)
	default:
		return writeProtocolError(stderr, protocol.UnsupportedFeature("compat "+args[0]))
	}
}

func (a App) runDaemon(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s daemon start\n  %s daemon stop\n  %s daemon status\n  %s daemon health\n", a.Name, a.Name, a.Name, a.Name)
		return 0
	}
	switch args[0] {
	case "start":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s daemon start: unexpected arguments: %s\n", a.Name, strings.Join(args[1:], " "))
			return protocol.ExitUsage
		}
		return a.daemonStart(stdout, stderr)
	case "stop":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s daemon stop: unexpected arguments: %s\n", a.Name, strings.Join(args[1:], " "))
			return protocol.ExitUsage
		}
		return a.daemonStop(stdout, stderr)
	case "status":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s daemon status: unexpected arguments: %s\n", a.Name, strings.Join(args[1:], " "))
			return protocol.ExitUsage
		}
		return a.daemonRequest(stdout, stderr, "status")
	case "health":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s daemon health: unexpected arguments: %s\n", a.Name, strings.Join(args[1:], " "))
			return protocol.ExitUsage
		}
		return a.daemonRequest(stdout, stderr, "health")
	default:
		return writeProtocolError(stderr, protocol.UnsupportedFeature("daemon "+args[0]))
	}
}

func (a App) runStatus(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s status\n  %s status <session_id> [--verbose]\n", a.Name, a.Name)
		return 0
	}
	if len(args) == 0 {
		return a.daemonRequest(stdout, stderr, "status")
	}
	if len(args) > 2 || (len(args) == 2 && args[1] != "--verbose") {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "status requires at most session_id and --verbose", protocol.ExitUsage, nil))
	}
	return a.daemonRequestWithParams(stdout, stderr, "status.session", map[string]any{"session_id": args[0], "verbose": len(args) == 2})
}

func (a App) runDaemonForeground(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s run\n\nRuns the local daemon in the foreground on the Unix socket under data_home/run.\n", a.Name)
		return 0
	}
	if len(args) != 0 {
		_, _ = fmt.Fprintf(stderr, "%s run: unexpected arguments: %s\n", a.Name, strings.Join(args, " "))
		return protocol.ExitUsage
	}
	dataHome, err := registry.ResolveDataHome(a.Runtime)
	if err != nil {
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	server := daemon.NewServer(dataHome, a.Runtime)
	if err := server.Run(context.Background()); err != nil {
		if protocol.ClassifyExit(err) == protocol.ExitOK {
			_, _ = fmt.Fprintln(stdout, "daemon already running")
			return protocol.ExitOK
		}
		return writeClassifiedError(stderr, err)
	}
	return protocol.ExitOK
}

func (a App) daemonStart(stdout io.Writer, stderr io.Writer) int {
	dataHome, err := registry.ResolveDataHome(a.Runtime)
	if err != nil {
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	if err := registry.ValidateDataHome(dataHome, a.Runtime); err != nil {
		return writeClassifiedError(stderr, err)
	}
	if _, err := registry.Load(dataHome, a.Runtime); err != nil {
		daemon.RecordPreSessionViolation(dataHome, a.Runtime, "security_violation", "daemon_start_rejected", err)
		return writeClassifiedError(stderr, err)
	}
	if _, err := transport.EnsureRuntimeDir(dataHome, a.Runtime); err != nil {
		daemon.RecordPreSessionViolation(dataHome, a.Runtime, "security_violation", "daemon_start_rejected", err)
		return writeClassifiedError(stderr, err)
	}
	socketPath := transport.SocketPath(dataHome)
	status := transport.ClassifySocket(socketPath, 200*time.Millisecond)
	switch status.State {
	case transport.SocketLive:
		_, _ = fmt.Fprintln(stdout, `{"daemon":"running","already_running":true}`)
		return protocol.ExitOK
	case transport.SocketMissing, transport.SocketStale:
	case transport.SocketAmbiguous:
		err := protocol.UnsafeRuntime("socket path is ambiguous", map[string]any{"socket": socketPath, "detail": status.Detail})
		daemon.RecordPreSessionViolation(dataHome, a.Runtime, "security_violation", "daemon_start_rejected", err)
		return writeProtocolError(stderr, err)
	}
	starter := a.StartDaemon
	if starter == nil {
		starter = startDaemonProcess
	}
	if err := starter(dataHome, a.Runtime); err != nil {
		daemon.RecordPreSessionViolation(dataHome, a.Runtime, "daemon_start_failed", "daemon_start_rejected", err)
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := transport.RoundTripWithRuntime(dataHome, a.Runtime, protocol.NewRequest("cli-start-ready", "ping", nil), 250*time.Millisecond)
		if err == nil && response.OK {
			_, _ = fmt.Fprintln(stdout, `{"daemon":"running","ready":true}`)
			return protocol.ExitOK
		}
		time.Sleep(50 * time.Millisecond)
	}
	return writeProtocolError(stderr, protocol.DaemonUnavailable("daemon did not become ready before timeout"))
}

func (a App) daemonStop(stdout io.Writer, stderr io.Writer) int {
	dataHome, err := registry.ResolveDataHome(a.Runtime)
	if err != nil {
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	response, err := transport.RoundTripWithRuntime(dataHome, a.Runtime, protocol.NewRequest("cli-stop", "shutdown", nil), time.Second)
	if err != nil {
		return writeProtocolError(stderr, protocol.ToStructuredError(err))
	}
	writeJSON(stdout, response.Result)
	return protocol.ExitOK
}

func (a App) daemonRequest(stdout io.Writer, stderr io.Writer, command string) int {
	dataHome, err := registry.ResolveDataHome(a.Runtime)
	if err != nil {
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	response, err := transport.RoundTripWithRuntime(dataHome, a.Runtime, protocol.NewRequest("cli-"+command, command, nil), time.Second)
	if err != nil {
		if response.Error != nil {
			return writeProtocolError(stderr, response.Error)
		}
		return writeProtocolError(stderr, protocol.ToStructuredError(err))
	}
	writeJSON(stdout, response.Result)
	return protocol.ExitOK
}

func (a App) runInit(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s init\n\nCreates data home 0700 and sample registry.yaml 0600 only when missing.\n", a.Name)
		return 0
	}
	if len(args) != 0 {
		_, _ = fmt.Fprintf(stderr, "%s init: unexpected arguments: %s\n", a.Name, strings.Join(args, " "))
		return 3
	}
	dataHome, err := registry.ResolveDataHome(a.Runtime)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s init: %v\n", a.Name, err)
		return 70
	}
	createdHome, err := registry.EnsureDataHome(dataHome, a.Runtime)
	if err != nil {
		return writeRegistryError(stderr, "init", err)
	}
	if createdHome {
		_, _ = fmt.Fprintf(stdout, "created data_home: %s\n", dataHome)
	} else {
		_, _ = fmt.Fprintf(stdout, "data_home exists: %s\n", dataHome)
	}
	registryPath := registry.RegistryPath(dataHome)
	if _, err := os.Lstat(registryPath); err == nil {
		_, _ = fmt.Fprintf(stdout, "registry exists: %s\n", registryPath)
		return 0
	} else if !os.IsNotExist(err) {
		_, _ = fmt.Fprintf(stderr, "%s init: inspect registry: %v\n", a.Name, err)
		return 70
	}
	if err := writeSampleRegistry(registryPath, dataHome); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s init: create sample registry: %v\n", a.Name, err)
		return 70
	}
	_, _ = fmt.Fprintf(stdout, "created registry: %s\n", registryPath)
	return 0
}

func (a App) runDoctor(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s doctor\n\nRuns read-only data-home and registry diagnostics.\n", a.Name)
		return 0
	}
	if len(args) != 0 {
		_, _ = fmt.Fprintf(stderr, "%s doctor: unexpected arguments: %s\n", a.Name, strings.Join(args, " "))
		return 3
	}
	dataHome, err := registry.ResolveDataHome(a.Runtime)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s doctor: %v\n", a.Name, err)
		return 70
	}
	_, _ = fmt.Fprintf(stdout, "data_home: %s\n", dataHome)
	if err := registry.ValidateDataHome(dataHome, a.Runtime); err != nil {
		_, _ = fmt.Fprintln(stdout, "data_home_status: invalid")
		return writeRegistryError(stderr, "doctor", err)
	}
	_, _ = fmt.Fprintln(stdout, "data_home_status: valid")
	loaded, err := registry.Load(dataHome, a.Runtime)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "registry_status: invalid")
		return writeRegistryError(stderr, "doctor", err)
	}
	_, _ = fmt.Fprintln(stdout, "registry_status: valid")
	_, _ = fmt.Fprintf(stdout, "registry_sha256: %s\n", loaded.SourceSHA256)
	socketPath := transport.SocketPath(dataHome)
	socketStatus := transport.ClassifySocket(socketPath, 50*time.Millisecond)
	_, _ = fmt.Fprintf(stdout, "socket_path: %s\n", socketPath)
	_, _ = fmt.Fprintf(stdout, "socket_status: %s\n", socketStatus.State)
	if socketStatus.State == transport.SocketLive {
		_, _ = fmt.Fprintln(stdout, "daemon_status: reachable")
	} else {
		_, _ = fmt.Fprintln(stdout, "daemon_status: unavailable")
	}
	report, err := storage.VerifyStorage(dataHome, storage.VerifyOptions{Runtime: a.Runtime})
	if err != nil {
		status := "invalid"
		detail := err.Error()
		if report != nil {
			status = report.Status
			detail = report.Detail
		}
		_, _ = fmt.Fprintf(stdout, "storage_status: %s\n", status)
		if detail != "" {
			_, _ = fmt.Fprintf(stdout, "storage_detail: %s\n", detail)
		}
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "storage_status: %s\n", report.Status)
	_, _ = fmt.Fprintf(stdout, "storage_source_hash: %s\n", report.ActualSourceHash)
	return 0
}

func (a App) runStorage(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s storage verify\n  %s storage rebuild-projection\n\nVerifies or rebuilds the read-only SQLite projection from channel.jsonl.\n", a.Name, a.Name)
		return 0
	}
	switch args[0] {
	case "verify":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s storage verify: unexpected arguments: %s\n", a.Name, strings.Join(args[1:], " "))
			return 1
		}
		dataHome, err := registry.ResolveDataHome(a.Runtime)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%s storage verify: %v\n", a.Name, err)
			return 70
		}
		report, err := storage.VerifyStorage(dataHome, storage.VerifyOptions{Runtime: a.Runtime})
		if err != nil {
			return writeStorageError(stdout, stderr, "storage verify", report, err)
		}
		_, _ = fmt.Fprintf(stdout, "storage valid: %s\n", report.DBPath)
		_, _ = fmt.Fprintf(stdout, "schema_version: %d\n", report.Expected.SchemaVersion)
		_, _ = fmt.Fprintf(stdout, "source_sessions: %d\n", report.Expected.SessionCount)
		_, _ = fmt.Fprintf(stdout, "source_events: %d\n", report.Expected.EventCount)
		_, _ = fmt.Fprintf(stdout, "source_hash: %s\n", report.ActualSourceHash)
		return 0
	case "rebuild-projection":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s storage rebuild-projection: unexpected arguments: %s\n", a.Name, strings.Join(args[1:], " "))
			return 1
		}
		dataHome, err := registry.ResolveDataHome(a.Runtime)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%s storage rebuild-projection: %v\n", a.Name, err)
			return 70
		}
		report, err := storage.RebuildProjection(dataHome, storage.ProjectionOptions{Runtime: a.Runtime})
		if err != nil {
			return writeStorageError(stdout, stderr, "storage rebuild-projection", nil, err)
		}
		_, _ = fmt.Fprintf(stdout, "projection rebuilt: %s\n", report.DBPath)
		_, _ = fmt.Fprintf(stdout, "schema_version: %d\n", report.SchemaVersion)
		_, _ = fmt.Fprintf(stdout, "source_sessions: %d\n", report.SessionCount)
		_, _ = fmt.Fprintf(stdout, "source_events: %d\n", report.EventCount)
		_, _ = fmt.Fprintf(stdout, "source_hash: %s\n", report.SourceHash)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "%s storage: unsupported command %q\n", a.Name, args[0])
		return 1
	}
}

func (a App) runRegistry(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s registry validate\n  %s registry show\n", a.Name, a.Name)
		return 0
	}
	switch args[0] {
	case "validate":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s registry validate: unexpected arguments: %s\n", a.Name, strings.Join(args[1:], " "))
			return 3
		}
		dataHome, err := registry.ResolveDataHome(a.Runtime)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%s registry validate: %v\n", a.Name, err)
			return 70
		}
		loaded, err := registry.Load(dataHome, a.Runtime)
		if err != nil {
			return writeRegistryError(stderr, "registry validate", err)
		}
		_, _ = fmt.Fprintf(stdout, "registry valid: %s\n", loaded.SourcePath)
		_, _ = fmt.Fprintf(stdout, "source_sha256: %s\n", loaded.SourceSHA256)
		return 0
	case "show":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s registry show: unexpected arguments: %s\n", a.Name, strings.Join(args[1:], " "))
			return 3
		}
		dataHome, err := registry.ResolveDataHome(a.Runtime)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%s registry show: %v\n", a.Name, err)
			return 70
		}
		loaded, err := registry.Load(dataHome, a.Runtime)
		if err != nil {
			return writeRegistryError(stderr, "registry show", err)
		}
		writeRegistrySummary(stdout, loaded)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "%s registry: unsupported command %q\n", a.Name, args[0])
		return 3
	}
}

func writeRegistryError(stderr io.Writer, command string, err error) int {
	if registry.IsValidationError(err) {
		_, _ = fmt.Fprintf(stderr, "%s failed:\n", command)
		for _, issue := range registry.Issues(err) {
			_, _ = fmt.Fprintf(stderr, "  %s\n", issue.String())
		}
		return classifyRegistryExit(err)
	}
	_, _ = fmt.Fprintf(stderr, "%s failed: %v\n", command, err)
	return 70
}

func writeClassifiedError(stderr io.Writer, err error) int {
	if registry.IsValidationError(err) {
		return writeProtocolError(stderr, registryProtocolError(err))
	}
	if storage.ProjectionKind(err) != "" {
		switch storage.ProjectionKind(err) {
		case storage.ProjectionErrorUnsafeDataHome:
			return writeProtocolError(stderr, protocol.UnsafeRuntime(err.Error(), nil))
		case storage.ProjectionErrorReplay, storage.ProjectionErrorMigration, storage.ProjectionErrorStorage:
			return writeProtocolError(stderr, protocol.StorageFailure(err.Error(), nil))
		case storage.ProjectionErrorValidation:
			return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, err.Error(), protocol.ExitUsage, nil))
		default:
			return writeProtocolError(stderr, protocol.InternalError(err.Error()))
		}
	}
	return writeProtocolError(stderr, protocol.ToStructuredError(err))
}

func registryProtocolError(err error) *protocol.StructuredError {
	if classifyRegistryExit(err) == protocol.ExitUnsafe {
		return protocol.UnsafeRuntime("registry or data_home is unsafe", map[string]any{"detail": "[REDACTED]"})
	}
	return protocol.NewError(protocol.ErrorValidation, err.Error(), protocol.ExitUsage, nil)
}

func classifyRegistryExit(err error) int {
	for _, issue := range registry.Issues(err) {
		switch issue.Category {
		case registry.CategoryDataHomeUnsafe, registry.CategoryNotRegular, registry.CategorySymlinkForbidden,
			registry.CategoryOwnerUnsafe, registry.CategoryPermissionsUnsafe, registry.CategoryChangedDuringLoad:
			return protocol.ExitUnsafe
		}
	}
	return protocol.ExitUsage
}

func writeProtocolError(stderr io.Writer, err error) int {
	structured := protocol.ToStructuredError(err)
	_, _ = stderr.Write(protocol.WriteJSONError(structured))
	return protocol.ClassifyExit(structured)
}

func writeJSON(stdout io.Writer, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, `{"error":"marshal_failed"}`)
		return
	}
	_, _ = stdout.Write(append(data, '\n'))
}

func writeStorageError(stdout io.Writer, stderr io.Writer, command string, report *storage.VerifyReport, err error) int {
	if report != nil {
		_, _ = fmt.Fprintf(stdout, "storage_status: %s\n", report.Status)
		if report.RecoverableProjectionOnly {
			_, _ = fmt.Fprintln(stdout, "recoverable_projection_only: true")
		}
		if report.Detail != "" {
			_, _ = fmt.Fprintf(stdout, "storage_detail: %s\n", report.Detail)
		}
	}
	_, _ = fmt.Fprintf(stderr, "%s failed: %v\n", command, err)
	switch storage.ProjectionKind(err) {
	case storage.ProjectionErrorValidation:
		return 1
	case storage.ProjectionErrorUnsafeDataHome:
		return 3
	case storage.ProjectionErrorReplay, storage.ProjectionErrorMigration, storage.ProjectionErrorStorage:
		return 6
	default:
		return 70
	}
}

func writeSampleRegistry(path string, dataHome string) error {
	sampleWorkspace := filepath.Join(dataHome, "workspaces", "example-member")
	content := fmt.Sprintf(`schema_version: 1
members:
  example-member:
    display_name: Example Member
    wrapper: example-member
    workspace: %s
    role: observer
    enabled: false
    adapter_kind: hermes-agent
    runtime_kind: hermes-cli-stream
    env_allowlist: []
`, sampleWorkspace)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(file, content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeRegistrySummary(stdout io.Writer, loaded *registry.LoadedRegistry) {
	enabled := make([]registry.Member, 0, len(loaded.Registry.Members))
	for _, id := range loaded.Registry.SortedMemberIDs() {
		member := loaded.Registry.Members[id]
		if member.Enabled {
			enabled = append(enabled, member)
		}
	}
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].ID < enabled[j].ID })
	_, _ = fmt.Fprintf(stdout, "registry: %s\n", loaded.SourcePath)
	_, _ = fmt.Fprintf(stdout, "schema_version: %d\n", loaded.Registry.EffectiveSchemaVersion())
	_, _ = fmt.Fprintf(stdout, "enabled_members: %d\n", len(enabled))
	for _, member := range enabled {
		status := "unresolved"
		path := ""
		if member.ResolvedWrapper != nil {
			status = "resolved"
			path = member.ResolvedWrapper.ResolvedPath
		}
		_, _ = fmt.Fprintf(stdout, "- id: %s\n", member.ID)
		_, _ = fmt.Fprintf(stdout, "  display_name: %s\n", member.DisplayName)
		_, _ = fmt.Fprintf(stdout, "  role: %s\n", member.Role)
		_, _ = fmt.Fprintf(stdout, "  adapter_kind: %s\n", member.AdapterKind)
		_, _ = fmt.Fprintf(stdout, "  wrapper_status: %s\n", status)
		_, _ = fmt.Fprintf(stdout, "  wrapper_path: %s\n", path)
		_, _ = fmt.Fprintf(stdout, "  workspace: %s\n", member.Workspace)
	}
}

func startDaemonProcess(dataHome string, runtime registry.Runtime) error {
	path, err := daemonExecutable()
	if err != nil {
		return err
	}
	cmd := exec.Command(path, "run")
	cmd.Env = append(os.Environ(), dataHomeEnvName+"="+dataHome)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func daemonExecutable() (string, error) {
	if value := os.Getenv(daemonPathEnvName); value != "" {
		return value, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	base := filepath.Base(exe)
	if base == daemonBinaryName {
		return exe, nil
	}
	sibling := filepath.Join(filepath.Dir(exe), daemonBinaryName)
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		return sibling, nil
	}
	if path, err := exec.LookPath(daemonBinaryName); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%s executable not found", daemonBinaryName)
}

func isHelp(arg string) bool {
	switch arg {
	case "--help", "-h", "help":
		return true
	default:
		return false
	}
}
