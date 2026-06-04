package command_test

import (
	"bytes"
	"strings"
	"testing"

	"kkachi-agent-network-control/internal/command"
)

func TestUnitCLIHelpNamesCanonicalBinaryAndHasNoErrors(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := command.NewCLI().Run([]string{"--help"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected help exit code 0, got %d", exitCode)
	}
	out := stdout.String()
	for _, want := range []string{
		"kkachi-agent-network",
		"Usage:",
		"Commands:",
		"daemon",
		"doctor",
		"status",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected CLI help to contain %q, got:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr for help, got %q", stderr.String())
	}
}

func TestUnitDaemonHelpNamesDaemonAndHasNoErrors(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := command.NewDaemon().Run([]string{"--help"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected help exit code 0, got %d", exitCode)
	}
	out := stdout.String()
	for _, want := range []string{
		"kkachi-agent-networkd",
		"Usage:",
		"Commands:",
		"run",
		"version",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected daemon help to contain %q, got:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr for help, got %q", stderr.String())
	}
}

func TestUnitUnsupportedBootstrapCommandFailsClosed(t *testing.T) {
	for _, app := range []command.App{command.NewCLI(), command.NewDaemon()} {
		t.Run(app.Name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := app.Run([]string{"mutate-state"}, &stdout, &stderr)

			if exitCode != 2 {
				t.Fatalf("expected unsupported command exit code 2, got %d", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("expected empty stdout for unsupported command, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), "unsupported bootstrap command") {
				t.Fatalf("expected fail-closed stderr, got %q", stderr.String())
			}
		})
	}
}

func TestUnitVersionReportsProtocolMetadata(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := command.NewCLI().Run([]string{"version"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected version exit code 0, got %d", exitCode)
	}
	out := stdout.String()
	for _, want := range []string{
		"kkachi-agent-network",
		"protocol_version=kan-protocol-v1alpha0",
		"schema_version=1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected version output to contain %q, got:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr for version, got %q", stderr.String())
	}
}
