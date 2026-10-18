package main

// Tests that the renamed command reports itself as purr.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPurrUsageText(t *testing.T) {
	repository := repoPath(t, "..", "..")
	executable := filepath.Join(t.TempDir(), "purr")

	build := exec.Command("go", "build", "-o", executable, "./cmd/purr")
	build.Dir = repository
	build.Env = os.Environ()
	var buildOutput strings.Builder
	build.Stdout = &buildOutput
	build.Stderr = &buildOutput
	if err := build.Run(); err != nil {
		t.Fatalf("building purr: %v\n%s", err, buildOutput.String())
	}

	noArguments := runExternal(t, executable, nil, repository, nil)
	if noArguments.status != 2 {
		t.Errorf("no arguments exited %d, want 2", noArguments.status)
	}
	if noArguments.stdout != "" {
		t.Errorf("no arguments wrote %q to stdout", noArguments.stdout)
	}
	for _, expected := range []string{"purr build", "purr run"} {
		if !strings.Contains(noArguments.stderr, expected) {
			t.Errorf("usage does not contain %q:\n%s", expected, noArguments.stderr)
		}
	}
	for _, unexpected := range []string{"kitten build", "kitten run"} {
		if strings.Contains(noArguments.stderr, unexpected) {
			t.Errorf("usage still contains %q:\n%s", unexpected, noArguments.stderr)
		}
	}

	help := runExternal(t, executable, []string{"--help"}, repository, nil)
	if help.status != 0 {
		t.Errorf("--help exited %d, want 0", help.status)
	}
	if !strings.Contains(help.stdout, "purr build") || !strings.Contains(help.stdout, "purr run") {
		t.Errorf("--help does not name purr:\n%s", help.stdout)
	}
}
