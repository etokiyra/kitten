package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tryReadFileSource = `fn main() {
    int index = 1;
    while (index < length(argv())) {
        match (try_read_file(argv()[index])) {
            Ok(text) { print("ok"); print(text); }
            Failed(message) { print("failed", message); }
        }
        index++;
    }
}
`

// buildTryReadFileProgram builds the fixture through the Go pipeline.
func buildTryReadFileProgram(t *testing.T, directory string) (string, string) {
	t.Helper()

	sourcePath := filepath.Join(directory, "try_read_file.ktn")
	if err := os.WriteFile(sourcePath, []byte(tryReadFileSource), 0o644); err != nil {
		t.Fatalf("writing try_read_file.ktn: %v", err)
	}
	executable := filepath.Join(directory, "try-read-file")
	var diagnostics strings.Builder
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building try_read_file fixture: %v\n%s", err, diagnostics.String())
	}
	if output := strings.TrimSpace(diagnostics.String()); output != "" {
		t.Fatalf("C compiler diagnostics: %s", output)
	}
	return executable, sourcePath
}

func TestTryReadFileRuntime(t *testing.T) {
	requireToolchain(t)

	directory := t.TempDir()
	executable, _ := buildTryReadFileProgram(t, directory)

	writeFile := func(name, content string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}

	existing := writeFile("existing.txt", "hello\nworld\n")
	empty := writeFile("empty.txt", "")

	t.Run("existing file", func(t *testing.T) {
		result := runExternal(t, executable, []string{existing}, directory, nil)
		want := "ok\nhello\nworld\n\n"
		if result.status != 0 || result.stderr != "" || result.stdout != want {
			t.Errorf("status %d, stderr %q, stdout %q; want %q", result.status, result.stderr, result.stdout, want)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		result := runExternal(t, executable, []string{empty}, directory, nil)
		want := "ok\n\n"
		if result.status != 0 || result.stderr != "" || result.stdout != want {
			t.Errorf("status %d, stderr %q, stdout %q; want %q", result.status, result.stderr, result.stdout, want)
		}
	})

	failure := func(t *testing.T, path string) string {
		_, err := os.ReadFile(path)
		if err == nil {
			t.Fatalf("os.ReadFile(%q) succeeded, want an error", path)
		}
		message := err.Error()
		result := runExternal(t, executable, []string{path}, directory, nil)
		want := "failed " + message + "\n"
		if result.status != 0 || result.stderr != "" || result.stdout != want {
			t.Errorf("status %d, stderr %q, stdout %q; want %q", result.status, result.stderr, result.stdout, want)
		}
		return message
	}

	t.Run("missing file", func(t *testing.T) {
		failure(t, filepath.Join(directory, "missing.txt"))
	})

	t.Run("directory", func(t *testing.T) {
		failure(t, directory)
	})

	t.Run("permission denied", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission denied is not observable as root")
		}
		path := writeFile("unreadable.txt", "secret")
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatalf("chmod unreadable.txt: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		failure(t, path)
	})
}
