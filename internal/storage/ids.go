package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	SessionsDirName    = "sessions"
	SessionYAMLName    = "session.yaml"
	ChannelJSONLName   = "channel.jsonl"
	maxSessionIDLength = 128
)

// ValidateSessionID enforces the Release v1 filesystem-safe session id shape.
func ValidateSessionID(id string) error {
	if id == "" {
		return NewValidationError(CategoryInvalidSessionID, "session_id", "session id is required")
	}
	if len(id) > maxSessionIDLength {
		return NewValidationError(CategoryInvalidSessionID, "session_id", fmt.Sprintf("session id exceeds %d bytes", maxSessionIDLength))
	}
	if !strings.HasPrefix(id, "sess_") {
		return NewValidationError(CategoryInvalidSessionID, "session_id", "session id must start with sess_")
	}
	if strings.Contains(id, "\x00") || strings.ContainsAny(id, `/\`) {
		return NewValidationError(CategoryInvalidSessionID, "session_id", "session id must not contain path separators or NUL")
	}
	if strings.Contains(id, "..") {
		return NewValidationError(CategoryInvalidSessionID, "session_id", "session id must not contain dot-dot segments")
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-' {
			continue
		}
		return NewValidationError(CategoryInvalidSessionID, "session_id", "session id must use ASCII letters, digits, dot, underscore, or dash")
	}
	return nil
}

func SessionDir(dataHome, id string) (string, error) {
	if err := ValidateSessionID(id); err != nil {
		return "", err
	}
	root := filepath.Join(filepath.Clean(dataHome), SessionsDirName)
	sessionDir := filepath.Join(root, id)
	if !pathContains(root, sessionDir) {
		return "", NewValidationError(CategoryPathUnsafe, sessionDir, "session path escapes sessions root")
	}
	return sessionDir, nil
}

func safeSessionDirForAppend(sessionDir string) error {
	clean := filepath.Clean(sessionDir)
	info, err := os.Lstat(clean)
	if err != nil {
		return NewValidationError(CategorySessionUnsafe, clean, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return NewValidationError(CategorySessionUnsafe, clean, "session directory symlinks are forbidden")
	}
	if !info.IsDir() {
		return NewValidationError(CategorySessionUnsafe, clean, "session path is not a directory")
	}
	return nil
}

func ensureSessionsRoot(dataHome string) (string, error) {
	root := filepath.Join(filepath.Clean(dataHome), SessionsDirName)
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.Mkdir(root, 0o700); err != nil {
				return "", NewValidationError(CategorySessionUnsafe, root, err.Error())
			}
			_ = syncDirectoryBestEffort(filepath.Clean(dataHome))
			return root, nil
		}
		return "", NewValidationError(CategorySessionUnsafe, root, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", NewValidationError(CategorySessionUnsafe, root, "sessions directory symlinks are forbidden")
	}
	if !info.IsDir() {
		return "", NewValidationError(CategorySessionUnsafe, root, "sessions path is not a directory")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return "", NewValidationError(CategorySessionUnsafe, root, fmt.Sprintf("mode %04o is group/world writable", info.Mode().Perm()))
	}
	return root, nil
}

func pathContains(root, path string) bool {
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	if cleanPath == cleanRoot {
		return true
	}
	prefix := cleanRoot
	if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
		prefix += string(os.PathSeparator)
	}
	return strings.HasPrefix(cleanPath, prefix)
}
