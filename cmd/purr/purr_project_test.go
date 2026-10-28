package main

// Tests for purr-kt current-directory project mode.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func writeBinaryProject(t *testing.T, root, name, source string) {
	t.Helper()
	writeProjectFile(t, filepath.Join(root, "purr.toml"),
		"[package]\nname = \""+name+"\"\nversion = \"0.1.0\"\n")
	writeProjectFile(t, filepath.Join(root, "src", "main.ktn"), source)
}

func createNewProject(t *testing.T, purr, directory, name string, library bool) string {
	t.Helper()
	arguments := []string{"new", name}
	want := "     Created binary (application) " + name + "\n"
	if library {
		arguments = []string{"new", "--lib", name}
		want = "     Created library " + name + "\n"
	}
	result := runPurrKTWithShell(t, "umask 022; exec \"$@\"", purr, directory, arguments)
	if result.status != 0 || result.stdout != "" || result.stderr != want {
		t.Fatalf("new %s: status %d, stdout %q, stderr %q; want status 0, stdout empty, stderr %q",
			name, result.status, result.stdout, result.stderr, want)
	}
	return filepath.Join(directory, name)
}

func pathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

func TestKittenPurrProjectMode(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	purrs := map[string]string{
		"Go-built":     buildPurrKTWithGo(t),
		"Kitten-built": buildPurrKTWithKitten(t, stage1),
	}

	t.Run("build and run", func(t *testing.T) {
		for pipeline, purr := range purrs {
			t.Run(pipeline, func(t *testing.T) {
				directory := t.TempDir()
				project := createNewProject(t, purr, directory, "demo", false)
				target := filepath.Join(project, "target")
				executable := filepath.Join(target, "demo")
				if pathExists(t, target) {
					t.Fatalf("new project already has a target directory")
				}

				build := runPurrKTWithShell(t, "umask 022; exec \"$@\"", purr, project, []string{"build"})
				if build.status != 0 || build.stdout != "built target/demo\n" || build.stderr != "" {
					t.Fatalf("project build: status %d, stdout %q, stderr %q",
						build.status, build.stdout, build.stderr)
				}
				information, err := os.Stat(target)
				if err != nil {
					t.Fatalf("stat target: %v", err)
				}
				if information.Mode().Perm() != 0o755 {
					t.Errorf("target mode = %o, want 755", information.Mode().Perm())
				}
				information, err = os.Stat(executable)
				if err != nil {
					t.Fatalf("stat target executable: %v", err)
				}
				if information.Mode().Perm()&0o111 == 0 {
					t.Errorf("target executable mode = %o, want executable bits", information.Mode().Perm())
				}

				run := runExternal(t, executable, nil, project, nil)
				if run.status != 0 || run.stdout != "Hello, world!\n" || run.stderr != "" {
					t.Errorf("built project: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
				}
				run = runPurrKT(t, purr, project, []string{"run"})
				if run.status != 0 || run.stdout != "Hello, world!\n" || run.stderr != "" {
					t.Errorf("project run: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
				}
				if !pathExists(t, executable) {
					t.Errorf("project run removed the built target")
				}

				// A missing target is recreated on the next build.
				if err := os.RemoveAll(target); err != nil {
					t.Fatalf("removing target: %v", err)
				}
				rebuild := runPurrKTWithShell(t, "umask 022; exec \"$@\"", purr, project, []string{"build"})
				if rebuild.status != 0 || rebuild.stdout != "built target/demo\n" || rebuild.stderr != "" {
					t.Errorf("rebuild after removing target: status %d, stdout %q, stderr %q",
						rebuild.status, rebuild.stdout, rebuild.stderr)
				}
				if !pathExists(t, executable) {
					t.Errorf("rebuild did not recreate the target executable")
				}
			})
		}
	})

	t.Run("run propagates child status and streams", func(t *testing.T) {
		for pipeline, purr := range purrs {
			t.Run(pipeline, func(t *testing.T) {
				directory := t.TempDir()
				writeBinaryProject(t, directory, "status",
					"fn main() {\n    print(\"seven\");\n    eprint(\"diagnostic\");\n    exit(7);\n}\n")
				result := runPurrKT(t, purr, directory, []string{"run"})
				if result.status != 7 || result.stdout != "seven\n" || result.stderr != "diagnostic\n" {
					t.Errorf("project run: status %d, stdout %q, stderr %q",
						result.status, result.stdout, result.stderr)
				}
				if !pathExists(t, filepath.Join(directory, "target", "status")) {
					t.Errorf("project run did not leave its built target")
				}
			})
		}
	})

	t.Run("errors and source mode", func(t *testing.T) {
		for pipeline, purr := range purrs {
			t.Run(pipeline, func(t *testing.T) {
				const source = "fn main() {\n    print(\"Hello, world!\");\n}\n"

				t.Run("no manifest", func(t *testing.T) {
					directory := t.TempDir()
					writeProjectFile(t, filepath.Join(directory, "src", "main.ktn"), source)
					for _, command := range []string{"build", "run"} {
						result := runPurrKT(t, purr, directory, []string{command})
						want := "purr " + command + ": missing source file\n\n" + purrUsageText() + "\n"
						if result.status != 2 || result.stdout != "" || result.stderr != want {
							t.Errorf("%s: status %d, stdout %q, stderr %q; want status 2, stderr %q",
								command, result.status, result.stdout, result.stderr, want)
						}
					}
					if pathExists(t, filepath.Join(directory, "target")) {
						t.Errorf("no-manifest invocation created target")
					}
				})

				t.Run("parent manifest is ignored", func(t *testing.T) {
					directory := t.TempDir()
					writeBinaryProject(t, directory, "parent", source)
					child := filepath.Join(directory, "child")
					if err := os.Mkdir(child, 0o755); err != nil {
						t.Fatalf("creating child directory: %v", err)
					}
					result := runPurrKT(t, purr, child, []string{"build"})
					want := "purr build: missing source file\n\n" + purrUsageText() + "\n"
					if result.status != 2 || result.stdout != "" || result.stderr != want {
						t.Errorf("status %d, stdout %q, stderr %q; want status 2, stderr %q",
							result.status, result.stdout, result.stderr, want)
					}
					if pathExists(t, filepath.Join(directory, "target")) || pathExists(t, filepath.Join(child, "target")) {
						t.Errorf("parent or child target was created")
					}
				})

				t.Run("malformed manifest", func(t *testing.T) {
					directory := t.TempDir()
					writeProjectFile(t, filepath.Join(directory, "purr.toml"),
						"[package]\nname = \"bad name\"\nversion = \"0.1.0\"\n")
					writeProjectFile(t, filepath.Join(directory, "src", "main.ktn"), source)
					for _, command := range []string{"build", "run"} {
						result := runPurrKT(t, purr, directory, []string{command})
						prefix := "purr " + command + ": purr.toml:2:8: invalid package name "
						if result.status != 1 || result.stdout != "" || !strings.HasPrefix(result.stderr, prefix) {
							t.Errorf("%s: status %d, stdout %q, stderr %q; want status 1 and prefix %q",
								command, result.status, result.stdout, result.stderr, prefix)
						}
					}
					if pathExists(t, filepath.Join(directory, "target")) {
						t.Errorf("malformed manifest created target")
					}
				})

				t.Run("conflicting sources", func(t *testing.T) {
					directory := t.TempDir()
					writeBinaryProject(t, directory, "both", source)
					writeProjectFile(t, filepath.Join(directory, "src", "lib.ktn"),
						"public fn add(int left, int right) -> int {\n    return left + right;\n}\n")
					for _, command := range []string{"build", "run"} {
						result := runPurrKT(t, purr, directory, []string{command})
						want := "purr " + command + ": project has both src/main.ktn and src/lib.ktn\n"
						if result.status != 1 || result.stdout != "" || result.stderr != want {
							t.Errorf("%s: status %d, stdout %q, stderr %q; want status 1, stderr %q",
								command, result.status, result.stdout, result.stderr, want)
						}
					}
				})

				t.Run("missing sources", func(t *testing.T) {
					directory := t.TempDir()
					writeProjectFile(t, filepath.Join(directory, "purr.toml"),
						"[package]\nname = \"empty\"\nversion = \"0.1.0\"\n")
					for _, command := range []string{"build", "run"} {
						result := runPurrKT(t, purr, directory, []string{command})
						want := "purr " + command + ": project has neither src/main.ktn nor src/lib.ktn\n"
						if result.status != 1 || result.stdout != "" || result.stderr != want {
							t.Errorf("%s: status %d, stdout %q, stderr %q; want status 1, stderr %q",
								command, result.status, result.stdout, result.stderr, want)
						}
					}
				})

				t.Run("library is deferred", func(t *testing.T) {
					directory := t.TempDir()
					project := createNewProject(t, purr, directory, "kitty", true)
					build := runPurrKT(t, purr, project, []string{"build"})
					if build.status != 1 || build.stdout != "" || build.stderr != "purr build: library builds are not implemented yet\n" {
						t.Errorf("library build: status %d, stdout %q, stderr %q",
							build.status, build.stdout, build.stderr)
					}
					run := runPurrKT(t, purr, project, []string{"run"})
					if run.status != 1 || run.stdout != "" || run.stderr != "purr run: library projects cannot be run\n" {
						t.Errorf("library run: status %d, stdout %q, stderr %q",
							run.status, run.stdout, run.stderr)
					}
					if pathExists(t, filepath.Join(project, "target")) {
						t.Errorf("library command created target")
					}
				})

				t.Run("target is a file", func(t *testing.T) {
					directory := t.TempDir()
					writeBinaryProject(t, directory, "conflict", source)
					target := filepath.Join(directory, "target")
					if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
						t.Fatalf("writing target file: %v", err)
					}
					result := runPurrKT(t, purr, directory, []string{"build"})
					if result.status != 1 || result.stdout != "" || result.stderr != "purr build: target is not a directory\n" {
						t.Errorf("target file: status %d, stdout %q, stderr %q",
							result.status, result.stdout, result.stderr)
					}
					data, err := os.ReadFile(target)
					if err != nil || string(data) != "keep" {
						t.Errorf("target file changed: %v %q", err, data)
					}
				})

				t.Run("failed build cleans a new target", func(t *testing.T) {
					directory := t.TempDir()
					writeBinaryProject(t, directory, "broken",
						"fn main() {\n    int value = \"not an int\";\n}\n")
					badSource := filepath.Join(directory, "src", "main.ktn")
					for _, command := range []string{"build", "run"} {
						result := runPurrKT(t, purr, directory, []string{command})
						if result.status != 1 || result.stdout != "" ||
							!strings.Contains(result.stderr, badSource+":2:") {
							t.Errorf("%s: status %d, stdout %q, stderr %q",
								command, result.status, result.stdout, result.stderr)
						}
						if pathExists(t, filepath.Join(directory, "target")) {
							t.Errorf("%s left a target directory after failure", command)
						}
					}
				})

				t.Run("failed build keeps an existing target", func(t *testing.T) {
					directory := t.TempDir()
					writeBinaryProject(t, directory, "broken",
						"fn main() {\n    int value = \"not an int\";\n}\n")
					target := filepath.Join(directory, "target")
					writeProjectFile(t, filepath.Join(target, "keep"), "keep")
					result := runPurrKT(t, purr, directory, []string{"build"})
					if result.status != 1 {
						t.Fatalf("status %d, want 1", result.status)
					}
					data, err := os.ReadFile(filepath.Join(target, "keep"))
					if err != nil || string(data) != "keep" {
						t.Errorf("pre-existing target changed: %v %q", err, data)
					}
					if pathExists(t, filepath.Join(target, "broken")) {
						t.Errorf("failed build left a partial executable")
					}
				})

				t.Run("explicit source ignores the project", func(t *testing.T) {
					directory := t.TempDir()
					writeBinaryProject(t, directory, "explicit", source)
					result := runPurrKT(t, purr, directory, []string{"build", "src/main.ktn", "-o", "explicit"})
					if result.status != 0 || result.stdout != "built explicit\n" || result.stderr != "" {
						t.Errorf("explicit build: status %d, stdout %q, stderr %q",
							result.status, result.stdout, result.stderr)
					}
					if !pathExists(t, filepath.Join(directory, "explicit")) {
						t.Errorf("explicit source did not produce its output")
					}
					if pathExists(t, filepath.Join(directory, "target")) {
						t.Errorf("explicit source entered project mode")
					}
					run := runPurrKT(t, purr, directory, []string{"run", "src/main.ktn"})
					if run.status != 0 || run.stdout != "Hello, world!\n" || run.stderr != "" {
						t.Errorf("explicit run: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
					}
					if pathExists(t, filepath.Join(directory, "target")) {
						t.Errorf("explicit run entered project mode")
					}
				})

				t.Run("output option without source is not project mode", func(t *testing.T) {
					directory := t.TempDir()
					writeBinaryProject(t, directory, "option", source)
					result := runPurrKT(t, purr, directory, []string{"build", "-o", "out"})
					want := "purr build: missing source file\n\n" + purrUsageText() + "\n"
					if result.status != 2 || result.stdout != "" || result.stderr != want {
						t.Errorf("status %d, stdout %q, stderr %q; want status 2, stderr %q",
							result.status, result.stdout, result.stderr, want)
					}
					if pathExists(t, filepath.Join(directory, "target")) || pathExists(t, filepath.Join(directory, "out")) {
						t.Errorf("output option without source produced an artifact")
					}
				})
			})
		}
	})
}
