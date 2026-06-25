package registry

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	memberIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	envNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type Registry struct {
	SchemaVersion        int
	Members              map[string]Member
	WrapperPathAllowlist []string
	SecretPatterns       []string
}

type Member struct {
	ID              string
	DisplayName     string
	Wrapper         string
	Workspace       string
	Role            string
	Enabled         bool
	AdapterKind     string
	Strengths       []string
	EnvAllowlist    []string
	Notes           string
	RuntimeKind     string
	Autostart       *bool
	StreamFilter    string
	ResolvedWrapper *WrapperResolution
}

type LoadedRegistry struct {
	DataHome      string
	SourcePath    string
	SourceSHA256  string
	LoadedByUID   int
	Registry      Registry
	SourceContent []byte
}

type rawRegistry struct {
	SchemaVersion        int                  `yaml:"schema_version,omitempty"`
	Members              map[string]rawMember `yaml:"members"`
	WrapperPathAllowlist []string             `yaml:"wrapper_path_allowlist,omitempty"`
	SecretPatterns       []string             `yaml:"secret_patterns,omitempty"`
}

type rawMember struct {
	DisplayName  string   `yaml:"display_name"`
	Wrapper      string   `yaml:"wrapper"`
	Workspace    string   `yaml:"workspace"`
	Role         string   `yaml:"role"`
	Enabled      *bool    `yaml:"enabled"`
	AdapterKind  string   `yaml:"adapter_kind"`
	Strengths    []string `yaml:"strengths,omitempty"`
	EnvAllowlist []string `yaml:"env_allowlist,omitempty"`
	Notes        string   `yaml:"notes,omitempty"`
	RuntimeKind  string   `yaml:"runtime_kind,omitempty"`
	Autostart    *bool    `yaml:"autostart,omitempty"`
	StreamFilter string   `yaml:"stream_filter,omitempty"`
}

func ParseRegistry(content []byte) (Registry, error) {
	var raw rawRegistry
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		category := CategoryParseError
		if strings.Contains(err.Error(), "field") && strings.Contains(err.Error(), "not found") {
			category = CategoryUnknownKey
		}
		return Registry{}, NewValidationError(category, RegistryFileName, err.Error())
	}
	registry, issues := validateRawRegistry(raw)
	if len(issues) > 0 {
		return Registry{}, NewValidationErrors(issues)
	}
	return registry, nil
}

func validateRawRegistry(raw rawRegistry) (Registry, []Issue) {
	var issues []Issue
	registry := Registry{
		SchemaVersion:        raw.SchemaVersion,
		Members:              make(map[string]Member, len(raw.Members)),
		WrapperPathAllowlist: append([]string(nil), raw.WrapperPathAllowlist...),
		SecretPatterns:       append([]string(nil), raw.SecretPatterns...),
	}
	if registry.SchemaVersion == 0 {
		registry.SchemaVersion = defaultSchema
	}
	if raw.Members == nil {
		issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: "members", Message: "required root key missing"})
		return registry, issues
	}
	if registry.SchemaVersion != defaultSchema {
		issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: "schema_version", Message: fmt.Sprintf("unsupported schema version %d", registry.SchemaVersion)})
	}
	for i, pattern := range raw.SecretPatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: fmt.Sprintf("secret_patterns[%d]", i), Message: err.Error()})
		}
	}

	ids := make([]string, 0, len(raw.Members))
	for id := range raw.Members {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		member, memberIssues := validateRawMember(id, raw.Members[id])
		issues = append(issues, memberIssues...)
		registry.Members[id] = member
	}
	return registry, issues
}

func validateRawMember(id string, raw rawMember) (Member, []Issue) {
	path := "members." + id
	member := Member{
		ID:           id,
		DisplayName:  raw.DisplayName,
		Wrapper:      raw.Wrapper,
		Workspace:    raw.Workspace,
		Role:         raw.Role,
		AdapterKind:  raw.AdapterKind,
		Strengths:    append([]string(nil), raw.Strengths...),
		EnvAllowlist: append([]string(nil), raw.EnvAllowlist...),
		Notes:        raw.Notes,
		RuntimeKind:  raw.RuntimeKind,
		Autostart:    raw.Autostart,
		StreamFilter: raw.StreamFilter,
	}
	if raw.Enabled != nil {
		member.Enabled = *raw.Enabled
	}

	var issues []Issue
	if id == "user" || id == "atn-controld" {
		issues = append(issues, Issue{Category: CategoryReservedPrincipalCollision, Path: path, Message: "member id is reserved"})
	}
	if !memberIDPattern.MatchString(id) {
		issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: path, Message: "member id must use letters, digits, dot, underscore, or dash and start with a letter or digit"})
	}
	required := map[string]string{
		"display_name": raw.DisplayName,
		"wrapper":      raw.Wrapper,
		"workspace":    raw.Workspace,
		"role":         raw.Role,
		"adapter_kind": raw.AdapterKind,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: path + "." + field, Message: "required field is empty"})
		}
	}
	if raw.Enabled == nil {
		issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: path + ".enabled", Message: "required field missing"})
	}
	if raw.AdapterKind != "" && raw.AdapterKind != "hermes-agent" {
		issues = append(issues, Issue{Category: CategoryUnknownAdapterKind, Path: path + ".adapter_kind", Message: fmt.Sprintf("unsupported adapter kind %q", raw.AdapterKind)})
	}
	if raw.RuntimeKind != "" && raw.RuntimeKind != "hermes-cli-stream" {
		issues = append(issues, Issue{Category: CategoryUnknownRuntimeKind, Path: path + ".runtime_kind", Message: fmt.Sprintf("unsupported runtime kind %q", raw.RuntimeKind)})
	}
	if strings.Contains(raw.Workspace, "\x00") {
		issues = append(issues, Issue{Category: CategorySchemaInvalid, Path: path + ".workspace", Message: "workspace contains NUL"})
	}
	issues = append(issues, ValidateEnvAllowlist(raw.EnvAllowlist, path+".env_allowlist")...)
	return member, issues
}

func ValidateEnvAllowlist(names []string, path string) []Issue {
	var issues []Issue
	for i, name := range names {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if !envNamePattern.MatchString(name) {
			issues = append(issues, Issue{Category: CategoryEnvAllowlistUnsafe, Path: itemPath, Message: "env allowlist entries must be variable names only"})
			continue
		}
		if strings.HasPrefix(name, "LD_") || strings.HasPrefix(name, "DYLD_") {
			issues = append(issues, Issue{Category: CategoryEnvAllowlistUnsafe, Path: itemPath, Message: "loader override variables are forbidden"})
		}
	}
	return issues
}

func (r Registry) EffectiveSchemaVersion() int {
	if r.SchemaVersion == 0 {
		return defaultSchema
	}
	return r.SchemaVersion
}

func (r Registry) SortedMemberIDs() []string {
	ids := make([]string, 0, len(r.Members))
	for id := range r.Members {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
