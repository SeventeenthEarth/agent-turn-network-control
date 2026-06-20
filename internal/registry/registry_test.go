package registry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/registry"
)

func TestUnitResolveDataHomePrecedence(t *testing.T) {
	env := map[string]string{
		"KKACHI_AGENT_NETWORK_HOME": "/tmp/kan-home",
		"XDG_DATA_HOME":             "/tmp/xdg",
	}
	runtime := testRuntime(env, "/home/tester")

	got, err := registry.ResolveDataHome(runtime)
	if err != nil {
		t.Fatalf("ResolveDataHome failed: %v", err)
	}
	if got != "/tmp/kan-home" {
		t.Fatalf("expected explicit home, got %q", got)
	}

	delete(env, "KKACHI_AGENT_NETWORK_HOME")
	got, err = registry.ResolveDataHome(runtime)
	if err != nil {
		t.Fatalf("ResolveDataHome with XDG failed: %v", err)
	}
	if got != "/tmp/xdg/kkachi-agent-network" {
		t.Fatalf("expected XDG data home, got %q", got)
	}

	delete(env, "XDG_DATA_HOME")
	got, err = registry.ResolveDataHome(runtime)
	if err != nil {
		t.Fatalf("ResolveDataHome fallback failed: %v", err)
	}
	if got != "/home/tester/.kkachi-agent-network" {
		t.Fatalf("expected home fallback, got %q", got)
	}
}

func TestUnitValidateDataHomeRejectsUnsafePermissions(t *testing.T) {
	dataHome := t.TempDir()
	if err := os.Chmod(dataHome, 0o770); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	err := registry.ValidateDataHome(dataHome, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategoryDataHomeUnsafe)
}

func TestUnitLoadRejectsRegistrySymlink(t *testing.T) {
	dataHome := safeDataHome(t)
	target := filepath.Join(dataHome, "target.yaml")
	writeFile(t, target, safeRegistryYAML("example", false, "missing-wrapper", nil), 0o600)
	if err := os.Symlink(target, registry.RegistryPath(dataHome)); err != nil {
		t.Fatalf("create registry symlink: %v", err)
	}

	_, err := registry.Load(dataHome, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategorySymlinkForbidden)
}

func TestUnitLoadRejectsGroupWritableRegistry(t *testing.T) {
	dataHome := safeDataHome(t)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("example", false, "missing-wrapper", nil), 0o660)

	_, err := registry.Load(dataHome, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategoryPermissionsUnsafe)
}

func TestUnitStrictSchemaRejectsUnknownKeys(t *testing.T) {
	dataHome := safeDataHome(t)
	writeFile(t, registry.RegistryPath(dataHome), `schema_version: 1
members:
  example:
    display_name: Example
    wrapper: missing-wrapper
    workspace: /tmp/example
    role: observer
    enabled: false
    adapter_kind: hermes-agent
    surprise: no
`, 0o600)

	_, err := registry.Load(dataHome, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategoryUnknownKey)
}

func TestUnitReservedPrincipalRejected(t *testing.T) {
	dataHome := safeDataHome(t)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("user", false, "missing-wrapper", nil), 0o600)

	_, err := registry.Load(dataHome, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategoryReservedPrincipalCollision)
}

func TestUnitEnvAllowlistRejectsValuesGlobsAndLoaderOverrides(t *testing.T) {
	dataHome := safeDataHome(t)
	envAllowlist := []string{"ANTHROPIC_API_KEY=value", "TOKEN_*", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES"}
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("example", false, "missing-wrapper", envAllowlist), 0o600)

	_, err := registry.Load(dataHome, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategoryEnvAllowlistUnsafe)
}

func TestUnitDisabledMemberSkipsWrapperResolution(t *testing.T) {
	dataHome := safeDataHome(t)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("example", false, "missing-wrapper", nil), 0o600)

	loaded, err := registry.Load(dataHome, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("expected disabled member registry to load, got %v", err)
	}
	if loaded.Registry.Members["example"].ResolvedWrapper != nil {
		t.Fatalf("disabled member should not resolve wrapper")
	}
}

func TestUnitEnabledBareWrapperResolvesThroughAllowlist(t *testing.T) {
	dataHome := safeDataHome(t)
	binDir := filepath.Join(dataHome, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	wrapperPath := filepath.Join(binDir, "example-wrapper")
	writeFile(t, wrapperPath, "#!/bin/sh\nexit 0\n", 0o700)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("example", true, "example-wrapper", nil, binDir), 0o600)

	loaded, err := registry.Load(dataHome, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("expected enabled wrapper to resolve, got %v", err)
	}
	resolved := loaded.Registry.Members["example"].ResolvedWrapper
	if resolved == nil {
		t.Fatalf("expected wrapper resolution")
	}
	if resolved.ResolvedPath != wrapperPath {
		t.Fatalf("expected resolved path %q, got %q", wrapperPath, resolved.ResolvedPath)
	}
}

func TestUnitConfiguredWrapperAllowlistReplacesDefaults(t *testing.T) {
	dataHome := safeDataHome(t)
	homeDir := filepath.Join(dataHome, "home")
	defaultBinDir := filepath.Join(homeDir, ".local", "bin")
	configuredBinDir := filepath.Join(dataHome, "configured-bin")
	if err := os.MkdirAll(defaultBinDir, 0o700); err != nil {
		t.Fatalf("mkdir default bin: %v", err)
	}
	if err := os.Mkdir(configuredBinDir, 0o700); err != nil {
		t.Fatalf("mkdir configured bin: %v", err)
	}
	writeFile(t, filepath.Join(defaultBinDir, "example-wrapper"), "#!/bin/sh\nexit 0\n", 0o700)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("example", true, "example-wrapper", nil, configuredBinDir), 0o600)

	_, err := registry.Load(dataHome, testRuntime(map[string]string{}, homeDir))
	assertIssue(t, err, registry.CategoryWrapperUnresolvable)
}

func TestUnitWrapperSymlinkTargetMustRemainUnderAllowlist(t *testing.T) {
	dataHome := safeDataHome(t)
	binDir := filepath.Join(dataHome, "bin")
	outsideDir := filepath.Join(dataHome, "outside")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.Mkdir(outsideDir, 0o700); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	outsideWrapper := filepath.Join(outsideDir, "example-wrapper")
	writeFile(t, outsideWrapper, "#!/bin/sh\nexit 0\n", 0o700)
	if err := os.Symlink(outsideWrapper, filepath.Join(binDir, "example-wrapper")); err != nil {
		t.Fatalf("symlink wrapper: %v", err)
	}
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("example", true, "example-wrapper", nil, binDir), 0o600)

	_, err := registry.Load(dataHome, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategoryWrapperOutsideAllowlist)
}

func TestUnitWriteSnapshotAtomicIncludesSchemaVersion(t *testing.T) {
	dataHome := safeDataHome(t)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("example", false, "missing-wrapper", nil), 0o600)
	loaded, err := registry.Load(dataHome, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	sessionDir := filepath.Join(dataHome, "sessions", "sess_test")
	runtime := registry.DefaultRuntime()
	runtime.Now = func() time.Time { return time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC) }

	if err := registry.WriteSnapshotAtomic(sessionDir, loaded, runtime); err != nil {
		t.Fatalf("WriteSnapshotAtomic failed: %v", err)
	}
	snapshotPath := filepath.Join(sessionDir, registry.SnapshotFileName)
	content, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	text := string(content)
	for _, want := range []string{"snapshot_metadata:", "source_path:", "source_sha256:", "loaded_by_uid:", "schema_version: 1", "members:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected snapshot to contain %q, got:\n%s", want, text)
		}
	}
	info, err := os.Stat(snapshotPath)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected snapshot mode 0600, got %04o", info.Mode().Perm())
	}
}

func TestUnitReconcileCouncilMembersAddsMissingExplicitPrincipal(t *testing.T) {
	dataHome := safeDataHome(t)
	binDir := filepath.Join(dataHome, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	writeFile(t, filepath.Join(binDir, "agent-mod"), "#!/bin/sh\nexit 0\n", 0o700)
	writeFile(t, filepath.Join(binDir, "agent-new"), "#!/bin/sh\nexit 0\n", 0o700)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("agent-mod", true, "agent-mod", nil, binDir), 0o600)

	loaded, report, err := registry.ReconcileCouncilMembers(dataHome, []string{"agent-mod", "agent-new"}, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("ReconcileCouncilMembers failed: %v", err)
	}
	if len(report.Added) != 1 || report.Added[0] != "agent-new" {
		t.Fatalf("expected agent-new added, got %#v", report.Added)
	}
	member, ok := loaded.Registry.Members["agent-new"]
	if !ok {
		t.Fatalf("expected agent-new in loaded registry")
	}
	if !member.Enabled || member.Wrapper != "agent-new" || member.AdapterKind != "hermes-agent" || member.RuntimeKind != "hermes-cli-stream" {
		t.Fatalf("unexpected reconciled member: %#v", member)
	}
	if member.ResolvedWrapper == nil || member.ResolvedWrapper.ResolvedPath != filepath.Join(binDir, "agent-new") {
		t.Fatalf("expected resolved wrapper through allowlist, got %#v", member.ResolvedWrapper)
	}
	if report.BeforeSHA256 == report.AfterSHA256 {
		t.Fatalf("expected registry sha to change after reconcile")
	}
}

func TestUnitReconcileCouncilMembersFailsClosedWhenWrapperMissing(t *testing.T) {
	dataHome := safeDataHome(t)
	binDir := filepath.Join(dataHome, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	writeFile(t, filepath.Join(binDir, "agent-mod"), "#!/bin/sh\nexit 0\n", 0o700)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("agent-mod", true, "agent-mod", nil, binDir), 0o600)

	_, _, err := registry.ReconcileCouncilMembers(dataHome, []string{"agent-mod", "agent-missing"}, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategoryWrapperUnresolvable)
	loaded, loadErr := registry.Load(dataHome, registry.DefaultRuntime())
	if loadErr != nil {
		t.Fatalf("load after failed reconcile: %v", loadErr)
	}
	if _, ok := loaded.Registry.Members["agent-missing"]; ok {
		t.Fatalf("failed reconcile must not mutate registry")
	}
}

func TestUnitReconcileCouncilMembersFailsClosedWhenWrapperNameAmbiguous(t *testing.T) {
	dataHome := safeDataHome(t)
	binA := filepath.Join(dataHome, "bin-a")
	binB := filepath.Join(dataHome, "bin-b")
	if err := os.Mkdir(binA, 0o700); err != nil {
		t.Fatalf("mkdir bin-a: %v", err)
	}
	if err := os.Mkdir(binB, 0o700); err != nil {
		t.Fatalf("mkdir bin-b: %v", err)
	}
	writeFile(t, filepath.Join(binA, "agent-mod"), "#!/bin/sh\nexit 0\n", 0o700)
	writeFile(t, filepath.Join(binA, "agent-new"), "#!/bin/sh\nexit 0\n", 0o700)
	writeFile(t, filepath.Join(binB, "agent-new"), "#!/bin/sh\nexit 0\n", 0o700)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("agent-mod", true, "agent-mod", nil, binA, binB), 0o600)

	_, _, err := registry.ReconcileCouncilMembers(dataHome, []string{"agent-new"}, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategoryWrapperUnresolvable)
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity in error, got %v", err)
	}
	loaded, loadErr := registry.Load(dataHome, registry.DefaultRuntime())
	if loadErr != nil {
		t.Fatalf("load after failed reconcile: %v", loadErr)
	}
	if _, ok := loaded.Registry.Members["agent-new"]; ok {
		t.Fatalf("ambiguous reconcile must not mutate registry")
	}
}

func TestUnitReconcileCouncilMembersFailsClosedWhenExistingPrincipalDisabled(t *testing.T) {
	dataHome := safeDataHome(t)
	writeFile(t, registry.RegistryPath(dataHome), safeRegistryYAML("agent-disabled", false, "missing-wrapper", nil), 0o600)

	_, _, err := registry.ReconcileCouncilMembers(dataHome, []string{"agent-disabled"}, registry.DefaultRuntime())
	assertIssue(t, err, registry.CategorySchemaInvalid)
}

func safeDataHome(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	return dataHome
}

func writeFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func safeRegistryYAML(id string, enabled bool, wrapper string, envAllowlist []string, allowlist ...string) string {
	var b strings.Builder
	b.WriteString("schema_version: 1\n")
	if len(allowlist) > 0 {
		b.WriteString("wrapper_path_allowlist:\n")
		for _, item := range allowlist {
			b.WriteString("  - ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	b.WriteString("members:\n")
	b.WriteString("  " + id + ":\n")
	b.WriteString("    display_name: Example\n")
	b.WriteString("    wrapper: " + wrapper + "\n")
	b.WriteString("    workspace: /tmp/example\n")
	b.WriteString("    role: observer\n")
	if enabled {
		b.WriteString("    enabled: true\n")
	} else {
		b.WriteString("    enabled: false\n")
	}
	b.WriteString("    adapter_kind: hermes-agent\n")
	b.WriteString("    runtime_kind: hermes-cli-stream\n")
	if envAllowlist != nil {
		b.WriteString("    env_allowlist:\n")
		for _, item := range envAllowlist {
			b.WriteString("      - ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func assertIssue(t *testing.T, err error, category string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected issue %s, got nil", category)
	}
	for _, issue := range registry.Issues(err) {
		if issue.Category == category {
			return
		}
	}
	t.Fatalf("expected issue %s, got %v", category, err)
}

func testRuntime(env map[string]string, home string) registry.Runtime {
	return registry.Runtime{
		LookupEnv: func(key string) (string, bool) {
			value, ok := env[key]
			return value, ok
		},
		UserHomeDir: func() (string, error) { return home, nil },
		CurrentUID:  os.Getuid,
		Now:         func() time.Time { return time.Now().UTC() },
	}
}
