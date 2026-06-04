package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

type SnapshotMetadata struct {
	SourcePath    string    `yaml:"source_path"`
	SourceSHA256  string    `yaml:"source_sha256"`
	LoadedAt      time.Time `yaml:"loaded_at"`
	LoadedByUID   int       `yaml:"loaded_by_uid"`
	SchemaVersion int       `yaml:"schema_version"`
}

type snapshotFile struct {
	Metadata SnapshotMetadata     `yaml:"snapshot_metadata"`
	Members  map[string]rawMember `yaml:"members"`
}

func WriteSnapshotAtomic(sessionDir string, loaded *LoadedRegistry, runtime Runtime) error {
	runtime = runtime.withDefaults()
	if loaded == nil {
		return NewValidationError(CategorySnapshotWriteFailed, sessionDir, "loaded registry is nil")
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return NewValidationError(CategorySnapshotWriteFailed, sessionDir, err.Error())
	}
	path := filepath.Join(sessionDir, SnapshotFileName)
	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return NewValidationError(CategorySnapshotWriteFailed, tmpPath, err.Error())
	}
	closed := false
	closeFile := func() {
		if !closed {
			_ = file.Close()
			closed = true
		}
	}
	defer closeFile()

	doc := snapshotFile{
		Metadata: SnapshotMetadata{
			SourcePath:    loaded.SourcePath,
			SourceSHA256:  loaded.SourceSHA256,
			LoadedAt:      runtime.Now().UTC(),
			LoadedByUID:   loaded.LoadedByUID,
			SchemaVersion: loaded.Registry.EffectiveSchemaVersion(),
		},
		Members: membersForSnapshot(loaded.Registry),
	}
	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return NewValidationError(CategorySnapshotWriteFailed, tmpPath, err.Error())
	}
	if err := encoder.Close(); err != nil {
		return NewValidationError(CategorySnapshotWriteFailed, tmpPath, err.Error())
	}
	if err := file.Chmod(0o600); err != nil {
		return NewValidationError(CategorySnapshotWriteFailed, tmpPath, err.Error())
	}
	if err := file.Sync(); err != nil {
		return NewValidationError(CategorySnapshotWriteFailed, tmpPath, err.Error())
	}
	if err := file.Close(); err != nil {
		closed = true
		return NewValidationError(CategorySnapshotWriteFailed, tmpPath, err.Error())
	}
	closed = true
	if err := os.Rename(tmpPath, path); err != nil {
		return NewValidationError(CategorySnapshotWriteFailed, path, err.Error())
	}
	if err := syncDirectory(sessionDir); err != nil {
		return NewValidationError(CategorySnapshotWriteFailed, sessionDir, err.Error())
	}
	return nil
}

func membersForSnapshot(registry Registry) map[string]rawMember {
	out := make(map[string]rawMember, len(registry.Members))
	ids := registry.SortedMemberIDs()
	for _, id := range ids {
		member := registry.Members[id]
		enabled := member.Enabled
		out[id] = rawMember{
			DisplayName:  member.DisplayName,
			Wrapper:      member.Wrapper,
			Workspace:    member.Workspace,
			Role:         member.Role,
			Enabled:      &enabled,
			AdapterKind:  member.AdapterKind,
			Strengths:    append([]string(nil), member.Strengths...),
			EnvAllowlist: append([]string(nil), member.EnvAllowlist...),
			Notes:        member.Notes,
			RuntimeKind:  member.RuntimeKind,
			Autostart:    member.Autostart,
			StreamFilter: member.StreamFilter,
		}
	}
	return out
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}

func SnapshotYAMLForTest(loaded *LoadedRegistry, at time.Time) ([]byte, error) {
	if loaded == nil {
		return nil, fmt.Errorf("loaded registry is nil")
	}
	doc := snapshotFile{
		Metadata: SnapshotMetadata{
			SourcePath:    loaded.SourcePath,
			SourceSHA256:  loaded.SourceSHA256,
			LoadedAt:      at.UTC(),
			LoadedByUID:   loaded.LoadedByUID,
			SchemaVersion: loaded.Registry.EffectiveSchemaVersion(),
		},
		Members: membersForSnapshot(loaded.Registry),
	}
	ids := make([]string, 0, len(doc.Members))
	for id := range doc.Members {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return yaml.Marshal(doc)
}
