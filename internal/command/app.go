package command

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/storage"
)

// App describes a minimal local-only command surface for a KAN binary.
type App struct {
	Name        string
	Description string
	Commands    []CommandSummary
	Runtime     registry.Runtime
	Kind        appKind
}

type appKind string

const (
	appKindCLI    appKind = "cli"
	appKindDaemon appKind = "daemon"
)

// CommandSummary is a help-only command listing for the bootstrap scaffold.
type CommandSummary struct {
	Name        string
	Description string
}

// NewCLI returns the canonical operator CLI scaffold.
func NewCLI() App {
	return NewCLIWithRuntime(registry.DefaultRuntime())
}

func NewCLIWithRuntime(runtime registry.Runtime) App {
	return App{
		Name:        "kkachi-agent-network",
		Description: "Canonical KAN control CLI for diagnostics, recovery, and manual operation.",
		Runtime:     runtime,
		Kind:        appKindCLI,
		Commands: []CommandSummary{
			{Name: "daemon", Description: "Manage or inspect the local daemon lifecycle."},
			{Name: "doctor", Description: "Run read-only diagnostics for the control runtime."},
			{Name: "init", Description: "Create a safe data home and sample registry when missing."},
			{Name: "registry", Description: "Validate or show the local registry authority."},
			{Name: "storage", Description: "Verify or rebuild the local SQLite storage projection."},
			{Name: "status", Description: "Show daemon or session status."},
			{Name: "version", Description: "Print protocol and binary version information."},
		},
	}
}

// NewDaemon returns the daemon binary scaffold.
func NewDaemon() App {
	return App{
		Name:        "kkachi-agent-networkd",
		Description: "KAN control daemon authority for state transitions, event append, replay, and projections.",
		Runtime:     registry.DefaultRuntime(),
		Kind:        appKindDaemon,
		Commands: []CommandSummary{
			{Name: "run", Description: "Start the daemon foreground process once implemented."},
			{Name: "version", Description: "Print protocol and binary version information."},
		},
	}
}

// Run executes the bootstrap-safe command behavior. Only help/version-safe
// surfaces are available until later roadmap tasks implement daemon features.
func (a App) Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if a.Runtime.LookupEnv == nil {
		a.Runtime = registry.DefaultRuntime()
	}
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(stdout, a.Help())
		return 0
	}

	if args[0] == "version" {
		_, _ = fmt.Fprintf(stdout, "%s bootstrap protocol_version=%s schema_version=%d\n", a.Name, protocol.ProtocolVersion, protocol.SchemaVersion)
		return 0
	}

	if a.Kind == appKindCLI {
		switch args[0] {
		case "init":
			return a.runInit(args[1:], stdout, stderr)
		case "doctor":
			return a.runDoctor(args[1:], stdout, stderr)
		case "registry":
			return a.runRegistry(args[1:], stdout, stderr)
		case "storage":
			return a.runStorage(args[1:], stdout, stderr)
		}
	}

	_, _ = fmt.Fprintf(stderr, "%s: unsupported bootstrap command %q\nRun '%s --help' for available scaffold commands.\n", a.Name, args[0], a.Name)
	return 3
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
	fmt.Fprintln(&b, "Status: bootstrap scaffold; state-mutating daemon features are not implemented in BOOTS-001.")
	return b.String()
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
	_, _ = fmt.Fprintln(stdout, "daemon_status: not_implemented")
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
		return 1
	}
	_, _ = fmt.Fprintf(stderr, "%s failed: %v\n", command, err)
	return 70
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

func isHelp(arg string) bool {
	switch arg {
	case "--help", "-h", "help":
		return true
	default:
		return false
	}
}
