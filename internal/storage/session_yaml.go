package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"gopkg.in/yaml.v3"
)

func WriteSessionYAMLAtomic(sessionDir string, metadata *SessionMetadata) error {
	if metadata == nil {
		return NewValidationError(CategoryMetadataInvalid, SessionYAMLName, "session metadata is nil")
	}
	if err := safeSessionDirForAppend(sessionDir); err != nil {
		return err
	}
	path := filepath.Join(filepath.Clean(sessionDir), SessionYAMLName)
	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return NewValidationError(CategoryMetadataWrite, tmpPath, err.Error())
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	if err := encoder.Encode(metadata); err != nil {
		return NewValidationError(CategoryMetadataWrite, tmpPath, err.Error())
	}
	if err := encoder.Close(); err != nil {
		return NewValidationError(CategoryMetadataWrite, tmpPath, err.Error())
	}
	if err := file.Chmod(0o600); err != nil {
		return NewValidationError(CategoryMetadataWrite, tmpPath, err.Error())
	}
	if err := file.Sync(); err != nil {
		return NewValidationError(CategoryMetadataWrite, tmpPath, err.Error())
	}
	if err := file.Close(); err != nil {
		closed = true
		return NewValidationError(CategoryMetadataWrite, tmpPath, err.Error())
	}
	closed = true
	if err := os.Rename(tmpPath, path); err != nil {
		return NewValidationError(CategoryMetadataWrite, path, err.Error())
	}
	if err := syncDirectoryBestEffort(sessionDir); err != nil {
		return NewValidationError(CategoryMetadataWrite, sessionDir, err.Error())
	}
	return nil
}

func LoadSessionYAML(sessionDir string) (*SessionMetadata, error) {
	if err := safeSessionDirForAppend(sessionDir); err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Clean(sessionDir), SessionYAMLName)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, NewValidationError(CategoryMetadataInvalid, path, err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, NewValidationError(CategoryMetadataInvalid, path, "session.yaml symlinks are forbidden")
	}
	if !info.Mode().IsRegular() {
		return nil, NewValidationError(CategoryMetadataInvalid, path, "session.yaml is not regular")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, NewValidationError(CategoryMetadataInvalid, path, err.Error())
	}
	var metadata SessionMetadata
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&metadata); err != nil {
		return nil, NewValidationError(CategoryMetadataInvalid, path, err.Error())
	}
	return &metadata, nil
}

func syncDirectoryBestEffort(path string) error {
	dir, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
			return nil
		}
		return err
	}
	return closeErr
}
