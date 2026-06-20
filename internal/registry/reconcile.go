package registry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReconcileReport records the daemon-owned registry membership changes applied
// before a council session is created.
type ReconcileReport struct {
	RegistryPath string
	BeforeSHA256 string
	AfterSHA256  string
	Added        []string
}

// ReconcileCouncilMembers ensures every explicit council principal exists in
// the daemon registry. Missing principals are added only when the identity is
// unambiguous: the principal id is syntactically valid, not reserved, and a
// same-named Hermes wrapper resolves through the registry wrapper allowlist.
// Existing disabled principals are intentionally not re-enabled here; session
// validation remains responsible for failing closed on disabled principals.
func ReconcileCouncilMembers(dataHome string, principals []string, runtime Runtime) (*LoadedRegistry, ReconcileReport, error) {
	runtime = runtime.withDefaults()
	cleanDataHome := filepath.Clean(dataHome)
	loaded, err := Load(cleanDataHome, runtime)
	if err != nil {
		return nil, ReconcileReport{}, err
	}
	report := ReconcileReport{RegistryPath: loaded.SourcePath, BeforeSHA256: loaded.SourceSHA256, AfterSHA256: loaded.SourceSHA256}

	missing, err := missingCouncilPrincipals(loaded, principals)
	if err != nil {
		return nil, report, err
	}
	if len(missing) == 0 {
		return loaded, report, nil
	}

	raw, err := parseRawRegistryForWrite(loaded.SourceContent)
	if err != nil {
		return nil, report, err
	}
	if raw.Members == nil {
		raw.Members = map[string]rawMember{}
	}
	for _, id := range missing {
		if _, err := resolveUniqueCouncilWrapper(id, raw.WrapperPathAllowlist, runtime); err != nil {
			return nil, report, err
		}
		raw.Members[id] = defaultCouncilMember(cleanDataHome, id)
		report.Added = append(report.Added, id)
	}
	sort.Strings(report.Added)

	content, err := marshalRawRegistry(raw)
	if err != nil {
		return nil, report, err
	}
	if err := ensureRegistryUnchanged(cleanDataHome, runtime, loaded.SourceSHA256); err != nil {
		return nil, report, err
	}
	if err := writeRegistryAtomic(loaded.SourcePath, content); err != nil {
		return nil, report, err
	}
	reloaded, err := Load(cleanDataHome, runtime)
	if err != nil {
		return nil, report, err
	}
	report.AfterSHA256 = reloaded.SourceSHA256
	return reloaded, report, nil
}

func missingCouncilPrincipals(loaded *LoadedRegistry, principals []string) ([]string, error) {
	var issues []Issue
	seen := map[string]struct{}{}
	missingSet := map[string]struct{}{}
	for _, rawID := range principals {
		id := strings.TrimSpace(rawID)
		if id == "" {
			issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: "council.principals", Message: "principal id is required"})
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if id == "user" || id == "kkachi-agent-networkd" {
			issues = append(issues, Issue{Category: CategoryReservedPrincipalCollision, Path: "members." + id, Message: "member id is reserved"})
			continue
		}
		if !memberIDPattern.MatchString(id) {
			issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: "members." + id, Message: "member id must use letters, digits, dot, underscore, or dash and start with a letter or digit"})
			continue
		}
		if member, ok := loaded.Registry.Members[id]; ok {
			if !member.Enabled {
				issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: "members." + id + ".enabled", Message: "council principal is disabled in loaded registry"})
			}
		} else {
			missingSet[id] = struct{}{}
		}
	}
	if len(issues) > 0 {
		return nil, NewValidationErrors(issues)
	}
	missing := make([]string, 0, len(missingSet))
	for id := range missingSet {
		missing = append(missing, id)
	}
	sort.Strings(missing)
	return missing, nil
}

func resolveUniqueCouncilWrapper(wrapper string, allowlist []string, runtime Runtime) (*WrapperResolution, error) {
	runtime = runtime.withDefaults()
	if strings.TrimSpace(wrapper) == "" {
		return nil, NewValidationError(CategoryWrapperUnresolvable, "", "wrapper is empty")
	}
	roots, err := expandAllowlist(allowlist, runtime)
	if err != nil {
		return nil, err
	}
	if filepath.IsAbs(wrapper) || filepath.Base(wrapper) != wrapper {
		return ResolveWrapper(wrapper, allowlist, runtime)
	}
	var matches []*WrapperResolution
	var firstValidationErr error
	for _, root := range roots {
		candidate := filepath.Join(root, wrapper)
		resolution, err := validateWrapperCandidate(candidate, wrapper, roots, runtime)
		if err == nil && resolution != nil {
			matches = append(matches, resolution)
			continue
		}
		if IsValidationError(err) && firstValidationErr == nil {
			firstValidationErr = err
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, match.ResolvedPath)
		}
		sort.Strings(paths)
		return nil, NewValidationError(CategoryWrapperUnresolvable, wrapper, "wrapper name is ambiguous across allowlist roots: "+strings.Join(paths, ", "))
	}
	if firstValidationErr != nil {
		return nil, firstValidationErr
	}
	return nil, NewValidationError(CategoryWrapperUnresolvable, wrapper, "wrapper could not be resolved in allowlist")
}

func ensureRegistryUnchanged(dataHome string, runtime Runtime, expectedSHA string) error {
	current, err := Load(dataHome, runtime)
	if err != nil {
		return err
	}
	if current.SourceSHA256 != expectedSHA {
		return NewValidationError(CategoryChangedDuringLoad, current.SourcePath, "registry changed during reconcile; reload and retry")
	}
	return nil
}

func parseRawRegistryForWrite(content []byte) (rawRegistry, error) {
	var raw rawRegistry
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		category := CategoryParseError
		if strings.Contains(err.Error(), "field") && strings.Contains(err.Error(), "not found") {
			category = CategoryUnknownKey
		}
		return rawRegistry{}, NewValidationError(category, RegistryFileName, err.Error())
	}
	return raw, nil
}

func defaultCouncilMember(dataHome string, id string) rawMember {
	enabled := true
	return rawMember{
		DisplayName:  id,
		Wrapper:      id,
		Workspace:    filepath.Join(dataHome, "workspaces", id),
		Role:         "participant",
		Enabled:      &enabled,
		AdapterKind:  "hermes-agent",
		RuntimeKind:  "hermes-cli-stream",
		EnvAllowlist: []string{},
		Notes:        "Auto-reconciled from an explicit council roster; remove or disable only through an approved registry maintenance action.",
	}
}

func marshalRawRegistry(raw rawRegistry) ([]byte, error) {
	if raw.SchemaVersion == 0 {
		raw.SchemaVersion = defaultSchema
	}
	content, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if _, err := ParseRegistry(content); err != nil {
		return nil, err
	}
	return content, nil
}

func writeRegistryAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+"-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if info.Mode()&os.ModeSymlink != 0 {
		return NewValidationError(CategorySymlinkForbidden, path, "registry symlinks are forbidden")
	} else if !info.Mode().IsRegular() {
		return NewValidationError(CategoryNotRegular, path, "registry file is not regular")
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}

func (r ReconcileReport) String() string {
	return fmt.Sprintf("registry=%s added=%v before=%s after=%s", r.RegistryPath, r.Added, r.BeforeSHA256, r.AfterSHA256)
}
