package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestUnitCLIEntrypointHelpNamesCanonicalBinary(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected CLI help to exit 0, got %v with output:\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{"hun", "Usage:", "Commands:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected CLI entrypoint help to contain %q, got:\n%s", want, text)
		}
	}
}
