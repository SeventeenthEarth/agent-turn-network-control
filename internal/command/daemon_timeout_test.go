package command

import (
	"testing"
	"time"
)

func TestUnitDaemonCommandTimeoutAllowsSelectedRunnerGrant(t *testing.T) {
	if got := daemonCommandTimeout("council.grant"); got < 2*time.Minute {
		t.Fatalf("council.grant timeout=%s, want long enough for selected-runner response generation", got)
	}
	if got := daemonCommandTimeout("council.poll"); got != time.Second {
		t.Fatalf("ordinary command timeout=%s, want 1s", got)
	}
}
