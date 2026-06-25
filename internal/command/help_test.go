package command_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"atn-control/internal/command"
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
		"atn-control",
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
		"atn-controld",
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

			if exitCode != 1 {
				t.Fatalf("expected unsupported command exit code 1, got %d", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("expected empty stdout for unsupported command, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), `"code":"unsupported_feature"`) {
				t.Fatalf("expected structured fail-closed stderr, got %q", stderr.String())
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
		"atn-control",
		"protocol_version=atn-protocol-v1alpha0",
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

func TestIntegrationInitCreatesSafeDataHomeAndDisabledSampleRegistry(t *testing.T) {
	dataHome := t.TempDir()
	if err := os.Remove(dataHome); err != nil {
		t.Fatalf("remove temp data home before init: %v", err)
	}
	t.Setenv("ATN_HOME", dataHome)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := command.NewCLI().Run([]string{"init"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected init exit 0, got %d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "created data_home:") || !strings.Contains(stdout.String(), "created registry:") {
		t.Fatalf("expected init to report creations, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	info, err := os.Stat(dataHome)
	if err != nil {
		t.Fatalf("stat data home: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("expected data home mode 0700, got %04o", info.Mode().Perm())
	}
	registryInfo, err := os.Stat(dataHome + "/registry.yaml")
	if err != nil {
		t.Fatalf("stat registry: %v", err)
	}
	if registryInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected registry mode 0600, got %04o", registryInfo.Mode().Perm())
	}
}

func TestIntegrationRegistryValidateAndShowUseFakeWrapperOnly(t *testing.T) {
	dataHome := t.TempDir()
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	binDir := dataHome + "/bin"
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(binDir+"/agent-one", []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake wrapper: %v", err)
	}
	if err := os.Chmod(binDir+"/agent-one", 0o700); err != nil {
		t.Fatalf("chmod fake wrapper: %v", err)
	}
	registryYAML := `schema_version: 1
wrapper_path_allowlist:
  - ` + binDir + `
members:
  agent-one:
    display_name: Agent One
    wrapper: agent-one
    workspace: /tmp/agent-one
    role: reviewer
    enabled: true
    adapter_kind: hermes-agent
    env_allowlist: [ANTHROPIC_API_KEY]
`
	if err := os.WriteFile(dataHome+"/registry.yaml", []byte(registryYAML), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if err := os.Chmod(dataHome+"/registry.yaml", 0o600); err != nil {
		t.Fatalf("chmod registry: %v", err)
	}
	t.Setenv("ATN_HOME", dataHome)
	t.Setenv("ANTHROPIC_API_KEY", "must-not-appear")

	var validateOut bytes.Buffer
	var validateErr bytes.Buffer
	validateExit := command.NewCLI().Run([]string{"registry", "validate"}, &validateOut, &validateErr)
	if validateExit != 0 {
		t.Fatalf("expected validate exit 0, got %d stdout=%q stderr=%q", validateExit, validateOut.String(), validateErr.String())
	}

	var showOut bytes.Buffer
	var showErr bytes.Buffer
	showExit := command.NewCLI().Run([]string{"registry", "show"}, &showOut, &showErr)
	if showExit != 0 {
		t.Fatalf("expected show exit 0, got %d stdout=%q stderr=%q", showExit, showOut.String(), showErr.String())
	}
	out := showOut.String()
	for _, want := range []string{"enabled_members: 1", "id: agent-one", "display_name: Agent One", "wrapper_status: resolved", binDir + "/agent-one", "workspace: /tmp/agent-one"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected show output to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "must-not-appear") || strings.Contains(out, "ANTHROPIC_API_KEY") {
		t.Fatalf("show output leaked env name/value:\n%s", out)
	}
}
