package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var DefaultWrapperPathAllowlist = []string{"/usr/local/bin", "/opt/hermes", "~/.local/bin"}

type WrapperResolution struct {
	Input          string
	CandidatePath  string
	ResolvedPath   string
	AllowlistRoot  string
	ResolvedVia    string
	ExecutableMode os.FileMode
}

func ResolveWrapper(wrapper string, allowlist []string, runtime Runtime) (*WrapperResolution, error) {
	runtime = runtime.withDefaults()
	if strings.TrimSpace(wrapper) == "" {
		return nil, NewValidationError(CategoryWrapperUnresolvable, "", "wrapper is empty")
	}
	roots, err := expandAllowlist(allowlist, runtime)
	if err != nil {
		return nil, err
	}
	var candidates []string
	if filepath.IsAbs(wrapper) {
		candidates = []string{filepath.Clean(wrapper)}
	} else if filepath.Base(wrapper) == wrapper {
		for _, root := range roots {
			candidates = append(candidates, filepath.Join(root, wrapper))
		}
	} else {
		return nil, NewValidationError(CategoryWrapperUnresolvable, wrapper, "wrapper must be absolute or a bare command name")
	}

	var firstValidationErr error
	for _, candidate := range candidates {
		resolution, err := validateWrapperCandidate(candidate, wrapper, roots, runtime)
		if err == nil && resolution != nil {
			return resolution, nil
		}
		if IsValidationError(err) && firstValidationErr == nil {
			firstValidationErr = err
		}
	}
	if firstValidationErr != nil {
		return nil, firstValidationErr
	}
	return nil, NewValidationError(CategoryWrapperUnresolvable, wrapper, "wrapper could not be resolved in allowlist")
}

func expandAllowlist(extra []string, runtime Runtime) ([]string, error) {
	values := DefaultWrapperPathAllowlist
	if len(extra) > 0 {
		values = extra
	}
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(values))
	for _, value := range values {
		expanded, err := expandHome(value, runtime)
		if err != nil {
			return nil, err
		}
		root := filepath.Clean(expanded)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	return roots, nil
}

func expandHome(path string, runtime Runtime) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := runtime.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func validateWrapperCandidate(candidate, input string, roots []string, runtime Runtime) (*WrapperResolution, error) {
	info, err := os.Lstat(candidate)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	resolved := candidate
	resolvedVia := "direct"
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(candidate)
		if err != nil {
			return nil, err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(candidate), target)
		}
		resolved = filepath.Clean(target)
		resolvedVia = "single-symlink"
		info, err = os.Lstat(resolved)
		if err != nil {
			return nil, NewValidationError(CategoryWrapperUnresolvable, candidate, err.Error())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, NewValidationError(CategoryWrapperUnresolvable, candidate, "wrapper symlink target is another symlink")
		}
	}
	if !info.Mode().IsRegular() {
		return nil, NewValidationError(CategoryWrapperPermissionsUnsafe, resolved, "wrapper target is not a regular file")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return nil, NewValidationError(CategoryWrapperPermissionsUnsafe, resolved, fmt.Sprintf("mode %04o is not executable", info.Mode().Perm()))
	}
	if info.Mode().Perm()&0o022 != 0 {
		return nil, NewValidationError(CategoryWrapperPermissionsUnsafe, resolved, fmt.Sprintf("mode %04o is group/world writable", info.Mode().Perm()))
	}
	uid, ok := fileOwnerUID(info)
	if !ok {
		return nil, NewValidationError(CategoryWrapperPermissionsUnsafe, resolved, "could not determine wrapper owner")
	}
	if uid != 0 && uid != runtime.CurrentUID() {
		return nil, NewValidationError(CategoryWrapperPermissionsUnsafe, resolved, fmt.Sprintf("owner uid %d is not current uid %d or root", uid, runtime.CurrentUID()))
	}
	root, ok := containingAllowlistRoot(resolved, roots)
	if !ok {
		return nil, NewValidationError(CategoryWrapperOutsideAllowlist, resolved, "canonical wrapper target is outside wrapper_path_allowlist")
	}
	return &WrapperResolution{
		Input:          input,
		CandidatePath:  candidate,
		ResolvedPath:   filepath.Clean(resolved),
		AllowlistRoot:  root,
		ResolvedVia:    resolvedVia,
		ExecutableMode: info.Mode().Perm(),
	}, nil
}

func containingAllowlistRoot(path string, roots []string) (string, bool) {
	cleanPath := filepath.Clean(path)
	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if cleanPath == cleanRoot {
			return cleanRoot, true
		}
		prefix := cleanRoot
		if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
			prefix += string(os.PathSeparator)
		}
		if strings.HasPrefix(cleanPath, prefix) {
			return cleanRoot, true
		}
	}
	return "", false
}
