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

// TestTryReadFileKittenRuntime compiles the fixture through the Kitten-written
// pipeline, checks that its C is byte-identical to the Go generator's, and runs
// both executables over the same existing, empty, missing, directory, and
// unreadable paths. It compares stdout, stderr, and the exit status exactly.
func TestTryReadFileKittenRuntime(t *testing.T) {
	requireToolchain(t)

	unitExecutable := buildKittenUnit(t)
	probeDirectory := t.TempDir()
	kittenText, err := runKittenUnit(unitExecutable, probeDirectory, tryReadFileSource, "")
	if err != nil {
		t.Fatalf("generating try_read_file C with the Kitten pipeline: %v", err)
	}
	_, _, referenceText, ok := checkProgram(t, "try_read_file runtime", tryReadFileSource, "")
	if !ok {
		return
	}
	if difference := unitDifference(kittenText, referenceText); difference != "" {
		t.Fatalf("the try_read_file translation unit differs: %s", difference)
	}

	goExecutable, _ := buildTryReadFileProgram(t, t.TempDir())
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	directory := t.TempDir()
	writeFile := func(name, content string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}
	existing := writeFile("existing.txt", "hello\nworld\n")
	empty := writeFile("empty.txt", "")
	unreadable := writeFile("unreadable.txt", "secret")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("chmod unreadable.txt: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	cases := []struct {
		name string
		path string
	}{
		{"existing file", existing},
		{"empty file", empty},
		{"missing file", filepath.Join(directory, "missing.txt")},
		{"directory", directory},
	}
	if os.Geteuid() != 0 {
		cases = append(cases, struct {
			name string
			path string
		}{"permission denied", unreadable})
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			want := runExternal(t, goExecutable, []string{testCase.path}, directory, nil)
			buildDirectory := t.TempDir()
			got, err := buildAndRunProgramWithArgs(t, compiler, compileFlags, linkFlags, buildDirectory, kittenText, index, []string{testCase.path})
			if err != nil {
				t.Fatalf("building and running the Kitten-generated program: %v", err)
			}
			if got.status != want.status || got.stderr != want.stderr || got.stdout != want.stdout {
				t.Errorf("Kitten pipeline: status %d, stderr %q, stdout %q; Go pipeline: status %d, stderr %q, stdout %q",
					got.status, got.stderr, got.stdout, want.status, want.stderr, want.stdout)
			}
		})
	}
}
