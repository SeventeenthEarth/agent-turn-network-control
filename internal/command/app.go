package command

import (
	"fmt"
	"io"
	"strings"

	"kkachi-agent-network-control/internal/protocol"
)

// App describes a minimal local-only command surface for a KAN binary.
type App struct {
	Name        string
	Description string
	Commands    []CommandSummary
}

// CommandSummary is a help-only command listing for the bootstrap scaffold.
type CommandSummary struct {
	Name        string
	Description string
}

// NewCLI returns the canonical operator CLI scaffold.
func NewCLI() App {
	return App{
		Name:        "kkachi-agent-network",
		Description: "Canonical KAN control CLI for diagnostics, recovery, and manual operation.",
		Commands: []CommandSummary{
			{Name: "daemon", Description: "Manage or inspect the local daemon lifecycle."},
			{Name: "doctor", Description: "Run read-only diagnostics for the control runtime."},
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
		Commands: []CommandSummary{
			{Name: "run", Description: "Start the daemon foreground process once implemented."},
			{Name: "version", Description: "Print protocol and binary version information."},
		},
	}
}

// Run executes the bootstrap-safe command behavior. Only help/version-safe
// surfaces are available until later roadmap tasks implement daemon features.
func (a App) Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(stdout, a.Help())
		return 0
	}

	if args[0] == "version" {
		_, _ = fmt.Fprintf(stdout, "%s bootstrap protocol_version=%s schema_version=%d\n", a.Name, protocol.ProtocolVersion, protocol.SchemaVersion)
		return 0
	}

	_, _ = fmt.Fprintf(stderr, "%s: unsupported bootstrap command %q\nRun '%s --help' for available scaffold commands.\n", a.Name, args[0], a.Name)
	return 2
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

func isHelp(arg string) bool {
	switch arg {
	case "--help", "-h", "help":
		return true
	default:
		return false
	}
}
