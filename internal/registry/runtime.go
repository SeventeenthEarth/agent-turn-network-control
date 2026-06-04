package registry

import (
	"os"
	"time"
)

// Runtime keeps environment, home, uid, and clock lookups injectable for tests.
type Runtime struct {
	LookupEnv   func(string) (string, bool)
	UserHomeDir func() (string, error)
	CurrentUID  func() int
	Now         func() time.Time
}

func DefaultRuntime() Runtime {
	return Runtime{
		LookupEnv:   os.LookupEnv,
		UserHomeDir: os.UserHomeDir,
		CurrentUID:  os.Getuid,
		Now:         func() time.Time { return time.Now().UTC() },
	}
}

func (r Runtime) withDefaults() Runtime {
	defaults := DefaultRuntime()
	if r.LookupEnv == nil {
		r.LookupEnv = defaults.LookupEnv
	}
	if r.UserHomeDir == nil {
		r.UserHomeDir = defaults.UserHomeDir
	}
	if r.CurrentUID == nil {
		r.CurrentUID = defaults.CurrentUID
	}
	if r.Now == nil {
		r.Now = defaults.Now
	}
	return r
}
