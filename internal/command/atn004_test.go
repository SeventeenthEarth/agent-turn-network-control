package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atn-control/internal/command"
	"atn-control/internal/registry"
	"atn-control/internal/transport"
)

func TestUnitATN004CanonicalBinarySurfacesHaveNoLegacyPublicAliases(t *testing.T) {
	for _, dir := range []string{
		filepath.Join("..", "..", "cmd", "atn-control"),
		filepath.Join("..", "..", "cmd", "atn-controld"),
	} {
		assertCommandDirExists(t, dir)
	}
	for _, dir := range []string{
		filepath.Join("..", "..", "cmd", "kkachi-agent-network"),
		filepath.Join("..", "..", "cmd", "kkachi-agent-networkd"),
		filepath.Join("..", "..", "cmd", "hun"),
		filepath.Join("..", "..", "cmd", "hund"),
	} {
		assertCommandDirMissing(t, dir)
	}

	for _, app := range []command.App{command.NewCLI(), command.NewDaemon()} {
		var helpOut bytes.Buffer
		var helpErr bytes.Buffer
		if exitCode := app.Run([]string{"--help"}, &helpOut, &helpErr); exitCode != 0 {
			t.Fatalf("%s help exit=%d stdout=%q stderr=%q", app.Name, exitCode, helpOut.String(), helpErr.String())
		}
		assertNoLegacyBinaryName(t, app.Name+" help", helpOut.String()+helpErr.String())

		var versionOut bytes.Buffer
		var versionErr bytes.Buffer
		if exitCode := app.Run([]string{"version"}, &versionOut, &versionErr); exitCode != 0 {
			t.Fatalf("%s version exit=%d stdout=%q stderr=%q", app.Name, exitCode, versionOut.String(), versionErr.String())
		}
		assertNoLegacyBinaryName(t, app.Name+" version", versionOut.String()+versionErr.String())
	}
}

func TestUnitATN004RuntimeAliasesAreNotAccepted(t *testing.T) {
	env := map[string]string{
		"KKACHI_AGENT_NETWORK_HOME": "/tmp/legacy-home",
		"HUN_HOME":                  "/tmp/hun-home",
		"XDG_DATA_HOME":             "/tmp/xdg",
	}
	runtime := registry.Runtime{
		LookupEnv: func(key string) (string, bool) {
			value, ok := env[key]
			return value, ok
		},
		UserHomeDir: func() (string, error) { return "/home/tester", nil },
		CurrentUID:  func() int { return os.Getuid() },
	}

	got, err := registry.ResolveDataHome(runtime)
	if err != nil {
		t.Fatalf("ResolveDataHome with legacy env alias present: %v", err)
	}
	if got != "/tmp/xdg/agent-turn-network" {
		t.Fatalf("legacy env alias should be ignored in favor of canonical XDG path, got %q", got)
	}

	delete(env, "XDG_DATA_HOME")
	got, err = registry.ResolveDataHome(runtime)
	if err != nil {
		t.Fatalf("ResolveDataHome with only legacy env alias present: %v", err)
	}
	if got != "/home/tester/.atn" {
		t.Fatalf("legacy env alias should not replace canonical home fallback, got %q", got)
	}

	dataHome := "/tmp/atn-control"
	socket := transport.SocketPath(dataHome)
	if socket != filepath.Join(dataHome, "run", "atn-controld.sock") {
		t.Fatalf("expected canonical socket path, got %q", socket)
	}
	if strings.Contains(socket, "kkachi-agent-networkd.sock") || strings.Contains(socket, "hund.sock") {
		t.Fatalf("legacy socket alias must not be exposed, got %q", socket)
	}
}

func assertCommandDirExists(t *testing.T, dir string) {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected canonical command directory %s to exist, info=%v err=%v", dir, info, err)
	}
}

func assertCommandDirMissing(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("legacy command directory %s must not be preserved as a wrapper, err=%v", dir, err)
	}
}

func assertNoLegacyBinaryName(t *testing.T, label string, output string) {
	t.Helper()
	for _, forbidden := range []string{"kkachi-agent-network", "kkachi-agent-networkd", "hun", "hund"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("%s exposed legacy binary name %q in:\n%s", label, forbidden, output)
		}
	}
}
