package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

func Load(dataHome string, runtime Runtime) (*LoadedRegistry, error) {
	runtime = runtime.withDefaults()
	if err := ValidateDataHome(dataHome, runtime); err != nil {
		return nil, err
	}
	sourcePath := RegistryPath(dataHome)
	before, err := os.Lstat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, NewValidationError(CategoryMissing, sourcePath, "registry file does not exist")
		}
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, NewValidationError(CategorySymlinkForbidden, sourcePath, "registry symlinks are forbidden")
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := compareFileMetadata(sourcePath, before, after); err != nil {
		return nil, err
	}
	if err := validateRegistryFileInfo(sourcePath, after, runtime); err != nil {
		return nil, err
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(content)
	registry, err := ParseRegistry(content)
	if err != nil {
		return nil, err
	}
	loaded := &LoadedRegistry{
		DataHome:      dataHome,
		SourcePath:    sourcePath,
		SourceSHA256:  "sha256:" + hex.EncodeToString(sum[:]),
		LoadedByUID:   runtime.CurrentUID(),
		Registry:      registry,
		SourceContent: append([]byte(nil), content...),
	}
	if err := resolveEnabledWrappers(loaded, runtime); err != nil {
		return nil, err
	}
	return loaded, nil
}

func compareFileMetadata(path string, before, after os.FileInfo) error {
	beforeDev, beforeIno, beforeOK := fileIdentity(before)
	afterDev, afterIno, afterOK := fileIdentity(after)
	if !beforeOK || !afterOK || beforeDev != afterDev || beforeIno != afterIno {
		return NewValidationError(CategoryChangedDuringLoad, path, "registry file identity changed during load")
	}
	if before.Mode().Type() != after.Mode().Type() || before.Size() != after.Size() {
		return NewValidationError(CategoryChangedDuringLoad, path, "registry file metadata changed during load")
	}
	return nil
}

func validateRegistryFileInfo(path string, info os.FileInfo, runtime Runtime) error {
	if !info.Mode().IsRegular() {
		return NewValidationError(CategoryNotRegular, path, "registry file is not regular")
	}
	uid, ok := fileOwnerUID(info)
	if !ok {
		return NewValidationError(CategoryOwnerUnsafe, path, "could not determine registry owner")
	}
	if uid != 0 && uid != runtime.CurrentUID() {
		return NewValidationError(CategoryOwnerUnsafe, path, fmt.Sprintf("owner uid %d is not current uid %d or root", uid, runtime.CurrentUID()))
	}
	if info.Mode().Perm()&0o022 != 0 {
		return NewValidationError(CategoryPermissionsUnsafe, path, fmt.Sprintf("mode %04o is group/world writable", info.Mode().Perm()))
	}
	return nil
}

func resolveEnabledWrappers(loaded *LoadedRegistry, runtime Runtime) error {
	var issues []Issue
	ids := loaded.Registry.SortedMemberIDs()
	for _, id := range ids {
		member := loaded.Registry.Members[id]
		if !member.Enabled {
			continue
		}
		resolution, err := ResolveWrapper(member.Wrapper, loaded.Registry.WrapperPathAllowlist, runtime)
		if err != nil {
			issues = append(issues, Issues(err)...)
			continue
		}
		member.ResolvedWrapper = resolution
		loaded.Registry.Members[id] = member
	}
	if len(issues) > 0 {
		return NewValidationErrors(issues)
	}
	return nil
}
