package runner

import (
	"sort"
	"strings"

	"hun-control/internal/registry"
)

func EnvForMember(member registry.Member, runtime registry.Runtime) []string {
	if runtime.LookupEnv == nil {
		runtime = registry.DefaultRuntime()
	}
	seen := map[string]string{}
	for _, name := range member.EnvAllowlist {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if value, ok := runtime.LookupEnv(name); ok {
			seen[name] = value
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+seen[key])
	}
	return out
}
