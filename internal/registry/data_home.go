package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	RegistryFileName  = "registry.yaml"
	SnapshotFileName  = "registry_snapshot.yaml"
	defaultSchema     = 1
	defaultDataHome   = ".kkachi-agent-network"
	dataHomeDirectory = "kkachi-agent-network"
)

// ResolveDataHome resolves the deterministic KAN data home.
func ResolveDataHome(runtime Runtime) (string, error) {
	runtime = runtime.withDefaults()
	if value, ok := runtime.LookupEnv("KKACHI_AGENT_NETWORK_HOME"); ok && value != "" {
		return filepath.Clean(value), nil
	}
	if value, ok := runtime.LookupEnv("XDG_DATA_HOME"); ok && value != "" {
		return filepath.Join(filepath.Clean(value), dataHomeDirectory), nil
	}
	home, err := runtime.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if home == "" {
		return "", errors.New("resolve home directory: empty home")
	}
	return filepath.Join(home, defaultDataHome), nil
}

func RegistryPath(dataHome string) string {
	return filepath.Join(filepath.Clean(dataHome), RegistryFileName)
}

// EnsureDataHome creates a missing data home with 0700 and then validates it.
func EnsureDataHome(dataHome string, runtime Runtime) (bool, error) {
	created := false
	if _, err := os.Lstat(dataHome); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(dataHome, 0o700); err != nil {
				return false, err
			}
			if err := os.Chmod(dataHome, 0o700); err != nil {
				return false, err
			}
			created = true
		} else {
			return false, err
		}
	}
	if err := ValidateDataHome(dataHome, runtime); err != nil {
		return created, err
	}
	return created, nil
}

// ValidateDataHome enforces the registry data-home safety contract.
func ValidateDataHome(dataHome string, runtime Runtime) error {
	runtime = runtime.withDefaults()
	info, err := os.Lstat(dataHome)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewValidationError(CategoryDataHomeUnsafe, dataHome, "data home does not exist")
		}
		return err
	}
	if !info.IsDir() {
		return NewValidationError(CategoryDataHomeUnsafe, dataHome, "data home is not a directory")
	}
	uid, ok := fileOwnerUID(info)
	if !ok {
		return NewValidationError(CategoryDataHomeUnsafe, dataHome, "could not determine data home owner")
	}
	if uid != runtime.CurrentUID() {
		return NewValidationError(CategoryDataHomeUnsafe, dataHome, fmt.Sprintf("owner uid %d is not current uid %d", uid, runtime.CurrentUID()))
	}
	if info.Mode().Perm()&0o022 != 0 {
		return NewValidationError(CategoryDataHomeUnsafe, dataHome, fmt.Sprintf("mode %04o is group/world writable", info.Mode().Perm()))
	}
	return nil
}

func fileOwnerUID(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}

func fileIdentity(info os.FileInfo) (dev uint64, ino uint64, ok bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return uint64(stat.Dev), uint64(stat.Ino), true
}
