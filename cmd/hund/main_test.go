package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestUnitDaemonEntrypointHelpNamesCanonicalBinary(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected daemon help to exit 0, got %v with output:\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{"hund", "Usage:", "Commands:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected daemon entrypoint help to contain %q, got:\n%s", want, text)
		}
	}
}
