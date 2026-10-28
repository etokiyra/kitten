package main

// Tests for purr new project creation.

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/codegen"
	"kitten/loader"
)

type newTreeEntry struct {
	mode fs.FileMode
	data string
	link string
}

func buildNewProbeWithGo(t *testing.T) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "new_probe_go")
	var diagnostics bytes.Buffer
	if err := buildExecutable(repoPath(t, "..", "..", "purr", "new_probe.ktn"), output, &diagnostics); err != nil {
		t.Fatalf("building new probe with Go pipeline: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C diagnostics while building new probe:\n%s", warnings)
	}
	return output
}

func buildNewProbeWithKitten(t *testing.T, stage1 string) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "new_probe_kitten")
	result := runExternal(t, wrapperPath(t), []string{
		"build", repoPath(t, "..", "..", "purr", "new_probe.ktn"), "-o", output,
	}, repoPath(t, "..", ".."), wrapperEnvironment(stage1, t))
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("building new probe with Kitten pipeline: status %d, stderr %q", result.status, result.stderr)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("Kitten new probe was not produced: %v", err)
	}
	return output
}

func runNewProbe(t *testing.T, executable, directory string, arguments []string) programResult {
	t.Helper()
	return runExternal(t, executable, arguments, directory, nil)
}

func runNewProbeWithShell(t *testing.T, script, executable, directory string, arguments []string) programResult {
	t.Helper()
	commandArguments := append([]string{"-c", script, "sh", executable}, arguments...)
	command := exec.Command("/bin/sh", commandArguments...)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running new probe through shell: %v", err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func newSnapshot(t *testing.T, root string) map[string]newTreeEntry {
	t.Helper()
	entries := make(map[string]newTreeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		information, err := os.Lstat(path)
		if err != nil {
			return err
		}
		item := newTreeEntry{mode: information.Mode()}
		if information.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			item.link = target
		} else if information.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			item.data = string(data)
		}
		entries[filepath.ToSlash(relative)] = item
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", root, err)
	}
	return entries
}

func newTreeKeys(entries map[string]newTreeEntry) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertNewTree(t *testing.T, root, name string, library bool, directoryMode, fileMode fs.FileMode) {
	t.Helper()
	entries := newSnapshot(t, root)
	expected := []string{
		name,
		name + "/.gitignore",
		name + "/purr.toml",
		name + "/src",
		name + "/src/main.ktn",
	}
	sourcePath := name + "/src/main.ktn"
	if library {
		sourcePath = name + "/src/lib.ktn"
		expected[4] = sourcePath
	}
	if keys := newTreeKeys(entries); strings.Join(keys, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("tree = %v, want %v", keys, expected)
	}
	if entries[name].mode.Perm() != directoryMode || entries[name+"/src"].mode.Perm() != directoryMode {
		t.Errorf("directory modes = %o/%o, want %o", entries[name].mode.Perm(), entries[name+"/src"].mode.Perm(), directoryMode)
	}
	for _, path := range []string{name + "/.gitignore", name + "/purr.toml", sourcePath} {
		if entries[path].mode.Perm() != fileMode {
			t.Errorf("%s mode = %o, want %o", path, entries[path].mode.Perm(), fileMode)
		}
	}
	if entries[name+"/.gitignore"].data != "/target\n" {
		t.Errorf(".gitignore = %q", entries[name+"/.gitignore"].data)
	}
	wantManifest := "[package]\nname = \"" + name + "\"\nversion = \"0.1.0\"\n"
	if entries[name+"/purr.toml"].data != wantManifest {
		t.Errorf("purr.toml = %q, want %q", entries[name+"/purr.toml"].data, wantManifest)
	}
	wantSource := "fn main() {\n    print(\"Hello, world!\");\n}\n"
	if library {
		wantSource = "public fn add(int left, int right) -> int {\n    return left + right;\n}\n"
	}
	if entries[sourcePath].data != wantSource {
		t.Errorf("source = %q, want %q", entries[sourcePath].data, wantSource)
	}
}

func runNewSuccess(t *testing.T, executable, directory, name string, library bool) programResult {
	t.Helper()
	arguments := []string{name}
	if library {
		arguments = append([]string{"--lib"}, arguments...)
	}
	result := runNewProbe(t, executable, directory, arguments)
	want := "     Created binary (application) " + name + "\n"
	if library {
		want = "     Created library " + name + "\n"
	}
	if result.status != 0 || result.stdout != "" || result.stderr != want {
		t.Fatalf("new %s: status %d, stdout %q, stderr %q; want status 0, stdout empty, stderr %q",
			name, result.status, result.stdout, result.stderr, want)
	}
	return result
}

func TestNewProjectSuccess(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	probes := map[string]string{
		"Go":     buildNewProbeWithGo(t),
		"Kitten": buildNewProbeWithKitten(t, stage1),
	}
	for pipeline, probe := range probes {
		for _, library := range []bool{false, true} {
			name := "hello"
			if library {
				name = "catnip"
			}
			directory := t.TempDir()
			arguments := []string{name}
			if library {
				arguments = append([]string{"--lib"}, arguments...)
			}
			result := runNewProbeWithShell(t, "umask 022; exec \"$@\"", probe, directory, arguments)
			want := "     Created binary (application) " + name + "\n"
			if library {
				want = "     Created library " + name + "\n"
			}
			if result.status != 0 || result.stdout != "" || result.stderr != want {
				t.Fatalf("%s new %s: status %d, stdout %q, stderr %q; want %q",
					pipeline, name, result.status, result.stdout, result.stderr, want)
			}
			assertNewTree(t, directory, name, library, 0o755, 0o644)
			t.Logf("%s %s created exact tree", pipeline, name)
		}
	}

	// --lib is accepted before and after the name in both pipelines.
	for pipeline, probe := range probes {
		directory := t.TempDir()
		result := runNewProbeWithShell(t, "umask 022; exec \"$@\"", probe, directory, []string{"tail", "--lib"})
		if result.status != 0 || result.stdout != "" || result.stderr != "     Created library tail\n" {
			t.Fatalf("%s --lib after name: status %d, stdout %q, stderr %q", pipeline, result.status, result.stdout, result.stderr)
		}
		assertNewTree(t, directory, "tail", true, 0o755, 0o644)
	}
}

func TestNewProjectModes(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	probes := map[string]string{
		"Go":     buildNewProbeWithGo(t),
		"Kitten": buildNewProbeWithKitten(t, stage1),
	}
	cases := []struct {
		mask      string
		directory fs.FileMode
		file      fs.FileMode
	}{
		{"022", 0o755, 0o644},
		{"077", 0o700, 0o600},
		{"000", 0o755, 0o644},
	}
	for pipeline, probe := range probes {
		for _, testCase := range cases {
			t.Run(pipeline+"/"+testCase.mask, func(t *testing.T) {
				directory := t.TempDir()
				script := "umask " + testCase.mask + "; exec \"$@\""
				result := runNewProbeWithShell(t, script, probe, directory, []string{"masked"})
				if result.status != 0 || result.stdout != "" || result.stderr != "     Created binary (application) masked\n" {
					t.Fatalf("status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
				}
				assertNewTree(t, directory, "masked", false, testCase.directory, testCase.file)
			})
		}
	}
}

func TestNewProjectInvalidNames(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	probes := map[string]string{
		"Go":     buildNewProbeWithGo(t),
		"Kitten": buildNewProbeWithKitten(t, stage1),
	}
	names := []string{"", strings.Repeat("a", 65), "A", "a/b", ".", "..", "x-", "x_", "a b", "caf\u00e9", "a\n"}
	for _, name := range names {
		expected := "error: " + oracleNameError(name) + "\n"
		for pipeline, probe := range probes {
			directory := t.TempDir()
			before := newSnapshot(t, directory)
			result := runNewProbe(t, probe, directory, []string{name})
			if result.status != 1 || result.stdout != "" || result.stderr != expected {
				t.Errorf("%s name %q: status %d, stdout %q, stderr %q; want status 1, stderr %q",
					pipeline, name, result.status, result.stdout, result.stderr, expected)
			}
			after := newSnapshot(t, directory)
			if !sameSnapshot(before, after) {
				t.Errorf("%s name %q touched the filesystem", pipeline, name)
			}
		}
	}
}

func oracleNameError(name string) string {
	p := newManifestOracle(nil)
	p.validateName(name, 1, 1)
	if p.error == "" {
		return ""
	}
	prefix := "purr.toml:1:1: "
	return strings.TrimPrefix(p.error, prefix)
}

func sameSnapshot(left, right map[string]newTreeEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for path, entry := range left {
		other, ok := right[path]
		if !ok || other != entry {
			return false
		}
	}
	return true
}

func newHelpText() string {
	return "Kitten project scaffolding\n" +
		"\n" +
		"Usage:\n" +
		"  purr new [--lib] <name>\n" +
		"\n" +
		"Options:\n" +
		"  --lib    Create a library project instead of a binary\n" +
		"  -h, --help"
}

func TestNewProjectHelp(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	probes := map[string]string{
		"Go":     buildNewProbeWithGo(t),
		"Kitten": buildNewProbeWithKitten(t, stage1),
	}
	// Help wins over every other argument, including an unknown flag or a
	// name, matching the top-level help flags' treatment of extra arguments.
	cases := [][]string{
		{"--help"},
		{"-h"},
		{"--help", "one"},
		{"--lib", "--help"},
		{"--bad", "--help"},
		{"one", "--help"},
	}
	for _, arguments := range cases {
		for name, probe := range probes {
			directory := t.TempDir()
			before := newSnapshot(t, directory)
			result := runNewProbe(t, probe, directory, arguments)
			if result.status != 0 || result.stdout != newHelpText()+"\n" || result.stderr != "" {
				t.Errorf("%s %v: status %d, stdout %q, stderr %q; want status 0, stdout %q, stderr empty",
					name, arguments, result.status, result.stdout, result.stderr, newHelpText()+"\n")
			}
			if !sameSnapshot(before, newSnapshot(t, directory)) {
				t.Errorf("%s %v touched the filesystem", name, arguments)
			}
		}
	}
}

func TestNewProjectUsage(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	probes := map[string]string{
		"Go":     buildNewProbeWithGo(t),
		"Kitten": buildNewProbeWithKitten(t, stage1),
	}
	cases := [][]string{
		nil,
		{"one", "two"},
		{"--bad", "one"},
		{"--lib", "--lib", "one"},
		{"--"},
		{"-x"},
	}
	for _, arguments := range cases {
		for name, probe := range probes {
			directory := t.TempDir()
			before := newSnapshot(t, directory)
			result := runNewProbe(t, probe, directory, arguments)
			if result.status != 2 || result.stdout != "" || result.stderr != "usage: purr new [--lib] <name>\n" {
				t.Errorf("%s %v: status %d, stdout %q, stderr %q", name, arguments, result.status, result.stdout, result.stderr)
			}
			if !sameSnapshot(before, newSnapshot(t, directory)) {
				t.Errorf("%s %v touched the filesystem", name, arguments)
			}
		}
	}
}

func TestNewProjectDeterministic(t *testing.T) {
	requireToolchain(t)
	probe := buildNewProbeWithGo(t)
	first := t.TempDir()
	second := t.TempDir()
	runNewSuccess(t, probe, first, "repeat", false)
	runNewSuccess(t, probe, second, "repeat", false)
	if !sameSnapshot(newSnapshot(t, first), newSnapshot(t, second)) {
		t.Fatal("two successful projects differ")
	}
}

func TestNewProjectGeneratedProjectWorks(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	probe := buildNewProbeWithGo(t)
	manifestProbe := buildManifestProbeWithGo(t)
	unit := buildKittenUnit(t)
	for _, library := range []bool{false, true} {
		directory := t.TempDir()
		name := "project"
		runNewSuccess(t, probe, directory, name, library)
		project := filepath.Join(directory, name)

		manifestResult := runExternal(t, manifestProbe, []string{filepath.Join(project, "purr.toml")}, directory, nil)
		if manifestResult.status != 0 || manifestResult.stdout != "ok project 0.1.0\n" || manifestResult.stderr != "" {
			t.Fatalf("generated manifest: status %d, stdout %q, stderr %q", manifestResult.status, manifestResult.stdout, manifestResult.stderr)
		}

		if library {
			libPath := filepath.Join(project, "src", "lib.ktn")
			testPath := filepath.Join(project, "test_main.ktn")
			testSource := "import \"src/lib.ktn\";\nfn main() {\n    print(add(2, 3));\n}\n"
			if err := os.WriteFile(testPath, []byte(testSource), 0o644); err != nil {
				t.Fatalf("writing library test main: %v", err)
			}
			goProgram := filepath.Join(directory, "lib-program-go")
			var diagnostics bytes.Buffer
			if err := buildExecutable(testPath, goProgram, &diagnostics); err != nil {
				t.Fatalf("building library test with Go: %v\n%s", err, diagnostics.String())
			}
			kittenProgram := filepath.Join(directory, "lib-program-kitten")
			result := runExternal(t, wrapperPath(t), []string{"build", testPath, "-o", kittenProgram}, project, wrapperEnvironment(stage1, t))
			if result.status != 0 || result.stderr != "" {
				t.Fatalf("building library test with Kitten: %d %q", result.status, result.stderr)
			}
			for pipeline, executable := range map[string]string{"Go": goProgram, "Kitten": kittenProgram} {
				run := runExternal(t, executable, nil, project, nil)
				if run.status != 0 || run.stdout != "5\n" || run.stderr != "" {
					t.Errorf("%s library run: %d %q %q", pipeline, run.status, run.stdout, run.stderr)
				}
			}
			libSource, _ := os.ReadFile(libPath)
			_, _, goC, ok := checkProgram(t, "new library", testSource, string(libSource))
			if !ok {
				return
			}
			kittenC, err := runKittenUnit(unit, t.TempDir(), testSource, string(libSource))
			if err != nil {
				t.Fatalf("Kitten library C: %v", err)
			}
			if difference := unitDifference(kittenC, goC); difference != "" {
				t.Fatalf("library C differs: %s", difference)
			}
		} else {
			mainPath := filepath.Join(project, "src", "main.ktn")
			mainSource, _ := os.ReadFile(mainPath)
			for pipeline, build := range map[string]func(string) string{
				"Go": func(destination string) string {
					var diagnostics bytes.Buffer
					if err := buildExecutable(mainPath, destination, &diagnostics); err != nil {
						t.Fatalf("Go main build: %v\n%s", err, diagnostics.String())
					}
					return destination
				},
				"Kitten": func(destination string) string {
					result := runExternal(t, wrapperPath(t), []string{"build", mainPath, "-o", destination}, project, wrapperEnvironment(stage1, t))
					if result.status != 0 || result.stderr != "" {
						t.Fatalf("Kitten main build: %d %q", result.status, result.stderr)
					}
					return destination
				},
			} {
				executable := build(filepath.Join(directory, "main-"+pipeline))
				run := runExternal(t, executable, nil, project, nil)
				if run.status != 0 || run.stdout != "Hello, world!\n" || run.stderr != "" {
					t.Errorf("%s main run: %d %q %q", pipeline, run.status, run.stdout, run.stderr)
				}
			}
			_, _, goC, ok := checkProgram(t, "new binary", string(mainSource), "")
			if !ok {
				return
			}
			kittenC, err := runKittenUnit(unit, t.TempDir(), string(mainSource), "")
			if err != nil {
				t.Fatalf("Kitten main C: %v", err)
			}
			if difference := unitDifference(kittenC, goC); difference != "" {
				t.Fatalf("main C differs: %s", difference)
			}
		}
	}
}

func TestNewProjectFailures(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	probes := map[string]string{
		"Go":     buildNewProbeWithGo(t),
		"Kitten": buildNewProbeWithKitten(t, stage1),
	}

	t.Run("destination regular file", func(t *testing.T) {
		for pipeline, probe := range probes {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "a"), []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			assertNewDestinationFailure(t, probe, directory, "a", pipeline)
		}
	})

	t.Run("destination directory with contents", func(t *testing.T) {
		for pipeline, probe := range probes {
			directory := t.TempDir()
			if err := os.Mkdir(filepath.Join(directory, "a"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "a", "keep"), []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			assertNewDestinationFailure(t, probe, directory, "a", pipeline)
		}
	})

	t.Run("destination dangling symlink", func(t *testing.T) {
		for pipeline, probe := range probes {
			directory := t.TempDir()
			if err := os.Symlink("missing-target", filepath.Join(directory, "a")); err != nil {
				t.Fatal(err)
			}
			assertNewDestinationFailure(t, probe, directory, "a", pipeline)
		}
	})

	t.Run("destination symlink to directory", func(t *testing.T) {
		for pipeline, probe := range probes {
			directory := t.TempDir()
			if err := os.Mkdir(filepath.Join(directory, "target"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("target", filepath.Join(directory, "a")); err != nil {
				t.Fatal(err)
			}
			assertNewDestinationFailure(t, probe, directory, "a", pipeline)
		}
	})

	t.Run("read-only parent", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission checks are not meaningful as root")
		}
		for pipeline, probe := range probes {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o555); err != nil {
				t.Fatal(err)
			}
			result := runNewProbe(t, probe, directory, []string{"a"})
			if err := os.Chmod(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			want := "error: mkdir a: permission denied\n"
			if result.status != 1 || result.stdout != "" || result.stderr != want {
				t.Errorf("%s: status %d, stdout %q, stderr %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
			}
			if _, err := os.Stat(filepath.Join(directory, "a")); !os.IsNotExist(err) {
				t.Errorf("%s: destination was created", pipeline)
			}
		}
	})

	t.Run("step 4 mode failure", func(t *testing.T) {
		for pipeline, probe := range probes {
			directory := t.TempDir()
			result := runNewProbeWithShell(t, "umask 0777; exec \"$@\"", probe, directory, []string{"a"})
			want := "error: mkdir a/src: permission denied\n"
			if result.status != 1 || result.stdout != "" || result.stderr != want {
				t.Errorf("%s: status %d, stdout %q, stderr %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
			}
			if _, err := os.Stat(filepath.Join(directory, "a")); !os.IsNotExist(err) {
				t.Errorf("%s: project was not cleaned", pipeline)
			}
		}
	})

	if _, err := exec.LookPath("prlimit"); err != nil {
		t.Skip("prlimit is unavailable; write-failure steps are model-tested only")
	}
	writeCases := []struct {
		name      string
		limit     int
		library   bool
		wantError string
	}{
		{"step 5a", 0, false, "error: write a/.gitignore: file too large\n"},
		{"step 5b", 16, false, "error: write a/purr.toml: file too large\n"},
		{"step 5c", 40, false, "error: write a/src/main.ktn: file too large\n"},
		{"step 5c library", 50, true, "error: write a/src/lib.ktn: file too large\n"},
	}
	for _, testCase := range writeCases {
		t.Run(testCase.name, func(t *testing.T) {
			for pipeline, probe := range probes {
				directory := t.TempDir()
				if err := os.WriteFile(filepath.Join(directory, "sibling"), []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
				arguments := []string{"a"}
				if testCase.library {
					arguments = append([]string{"--lib"}, arguments...)
				}
				script := "trap '' XFSZ; exec prlimit --fsize=" + strconv.Itoa(testCase.limit) + " -- \"$@\""
				result := runNewProbeWithShell(t, script, probe, directory, arguments)
				if result.status != 1 || result.stdout != "" || result.stderr != testCase.wantError {
					t.Errorf("%s: status %d, stdout %q, stderr %q; want %q",
						pipeline, result.status, result.stdout, result.stderr, testCase.wantError)
				}
				if _, err := os.Stat(filepath.Join(directory, "a")); !os.IsNotExist(err) {
					t.Errorf("%s: project was not cleaned", pipeline)
				}
				data, err := os.ReadFile(filepath.Join(directory, "sibling"))
				if err != nil || string(data) != "keep" {
					t.Errorf("%s: pre-existing sibling changed: %v %q", pipeline, err, data)
				}
			}
		})
	}
}

func assertNewDestinationFailure(t *testing.T, probe, directory, name, pipeline string) {
	t.Helper()
	before := newSnapshot(t, directory)
	result := runNewProbe(t, probe, directory, []string{name})
	want := "error: destination " + name + " already exists\n"
	if result.status != 1 || result.stdout != "" || result.stderr != want {
		t.Errorf("%s: status %d, stdout %q, stderr %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
	}
	if !sameSnapshot(before, newSnapshot(t, directory)) {
		t.Errorf("%s: destination changed", pipeline)
	}
}

func writeNewVariant(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	manifestSource, err := os.ReadFile(repoPath(t, "..", "..", "purr", "manifest.ktn"))
	if err != nil {
		t.Fatalf("reading manifest module: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.ktn"), manifestSource, 0o644); err != nil {
		t.Fatalf("writing variant manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "new_variant.ktn"), []byte(source), 0o644); err != nil {
		t.Fatalf("writing new variant: %v", err)
	}
	driver := `import "new_variant.ktn";

fn main() {
    match (new_project(argv()[1], false)) {
        Created(message) {
            eprint(message);
        }
        Failed(message) {
            eprint(message);
            exit(1);
        }
    }
}
`
	if err := os.WriteFile(filepath.Join(root, "driver.ktn"), []byte(driver), 0o644); err != nil {
		t.Fatalf("writing variant driver: %v", err)
	}
	return root
}

func buildVariantProbes(t *testing.T, root, stage1 string) map[string]string {
	t.Helper()
	driverPath := filepath.Join(root, "driver.ktn")
	goProbe := filepath.Join(root, "driver-go")
	var diagnostics bytes.Buffer
	if err := buildExecutable(driverPath, goProbe, &diagnostics); err != nil {
		t.Fatalf("building Go variant probe: %v\n%s", err, diagnostics.String())
	}
	kittenProbe := filepath.Join(root, "driver-kitten")
	result := runExternal(t, wrapperPath(t), []string{"build", driverPath, "-o", kittenProbe}, root, wrapperEnvironment(stage1, t))
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("building Kitten variant probe: status %d, stderr %q", result.status, result.stderr)
	}
	return map[string]string{"Go": goProbe, "Kitten": kittenProbe}
}

func TestNewProjectDashNameAPI(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	newSource, err := os.ReadFile(repoPath(t, "..", "..", "purr", "new.ktn"))
	if err != nil {
		t.Fatalf("reading new module: %v", err)
	}
	root := writeNewVariant(t, string(newSource))
	probes := buildVariantProbes(t, root, stage1)
	for pipeline, probe := range probes {
		directory := t.TempDir()
		before := newSnapshot(t, directory)
		result := runNewProbe(t, probe, directory, []string{"-x"})
		want := "error: " + oracleNameError("-x") + "\n"
		if result.status != 1 || result.stdout != "" || result.stderr != want {
			t.Errorf("%s: status %d, stdout %q, stderr %q; want status 1, stderr %q",
				pipeline, result.status, result.stdout, result.stderr, want)
		}
		if !sameSnapshot(before, newSnapshot(t, directory)) {
			t.Errorf("%s: invalid API name touched the filesystem", pipeline)
		}
	}
}

func TestNewProjectRaceModel(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	newSource, err := os.ReadFile(repoPath(t, "..", "..", "purr", "new.ktn"))
	if err != nil {
		t.Fatalf("reading new module: %v", err)
	}
	variant := string(newSource) + `
fn try_file_info(string path) -> FileInfoResult {
    if (length(path) == 0) {
        return FileInfoResult.Missing;
    }
    return FileInfoResult.Missing;
}
`
	root := writeNewVariant(t, variant)
	probes := buildVariantProbes(t, root, stage1)
	for pipeline, probe := range probes {
		directory := t.TempDir()
		if err := os.Mkdir(filepath.Join(directory, "a"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "a", "keep"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		before := newSnapshot(t, directory)
		result := runNewProbe(t, probe, directory, []string{"a"})
		want := "error: mkdir a: file exists\n"
		if result.status != 1 || result.stdout != "" || result.stderr != want {
			t.Errorf("%s: status %d, stdout %q, stderr %q; want %q",
				pipeline, result.status, result.stdout, result.stderr, want)
		}
		if !sameSnapshot(before, newSnapshot(t, directory)) {
			t.Errorf("%s: race model changed the pre-existing directory", pipeline)
		}
	}
}

func TestNewProjectCleanupNotesModel(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	newSource, err := os.ReadFile(repoPath(t, "..", "..", "purr", "new.ktn"))
	if err != nil {
		t.Fatalf("reading new module: %v", err)
	}
	anchor := `        Ok {
            created[created_count] = source_dir;
            created_count++;
        }
    }
`
	insert := anchor + `
    match (try_write_file(name + "/src/extra", "x")) {
        Failed(message) {
            return fail(message, created, created_count);
        }
        Ok { }
    }
`
	variant := strings.Replace(string(newSource), anchor, insert, 1)
	if variant == string(newSource) {
		t.Fatal("could not inject the cleanup model")
	}
	root := writeNewVariant(t, variant)
	probes := buildVariantProbes(t, root, stage1)
	if _, err := exec.LookPath("prlimit"); err != nil {
		t.Skip("prlimit unavailable")
	}
	for pipeline, probe := range probes {
		directory := t.TempDir()
		script := "trap '' XFSZ; exec prlimit --fsize=4 -- \"$@\""
		result := runNewProbeWithShell(t, script, probe, directory, []string{"a"})
		want := "error: write a/.gitignore: file too large\n" +
			"       note: could not remove a/src: remove a/src: directory not empty\n" +
			"       note: could not remove a: remove a: directory not empty\n"
		if result.status != 1 || result.stdout != "" || result.stderr != want {
			t.Errorf("%s: status %d, stdout %q, stderr %q; want %q",
				pipeline, result.status, result.stdout, result.stderr, want)
		}
		data, err := os.ReadFile(filepath.Join(directory, "a", "src", "extra"))
		if err != nil || string(data) != "x" {
			t.Errorf("%s: model leftovers are not as expected: %v %q", pipeline, err, data)
		}
		if err := os.RemoveAll(filepath.Join(directory, "a")); err != nil {
			t.Fatalf("removing model leftovers: %v", err)
		}
	}
}

func TestNewProjectProbeCParity(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	root := repoPath(t, "..", "..")
	probePath := filepath.Join(root, "purr", "new_probe.ktn")
	modules, err := loader.Load(probePath)
	if err != nil {
		t.Fatalf("loading new probe: %v", err)
	}
	checked, err := checker.CheckModules(modules)
	if err != nil {
		t.Fatalf("checking new probe: %v", err)
	}
	goC, err := codegen.Generate(modules, checked)
	if err != nil {
		t.Fatalf("generating new probe C with Go: %v", err)
	}
	result := runExternal(t, stage1, []string{probePath}, root, nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("stage 1 new probe C: status %d, stderr %q", result.status, result.stderr)
	}
	if difference := unitDifference(result.stdout, goC); difference != "" {
		t.Fatalf("new probe C differs: %s", difference)
	}
}

func TestNewProjectSanitizer(t *testing.T) {
	requireToolchain(t)
	probePath := repoPath(t, "..", "..", "purr", "new_probe.ktn")
	modules, err := loader.Load(probePath)
	if err != nil {
		t.Fatalf("loading new probe: %v", err)
	}
	checked, err := checker.CheckModules(modules)
	if err != nil {
		t.Fatalf("checking new probe: %v", err)
	}
	text, err := codegen.Generate(modules, checked)
	if err != nil {
		t.Fatalf("generating new probe C: %v", err)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")
	executable, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, text)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	directory := t.TempDir()
	result := runExternal(t, executable, []string{"a"}, directory, nil)
	if result.status != 0 || result.stdout != "" || result.stderr != "     Created binary (application) a\n" {
		t.Fatalf("sanitized success: status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	result = runExternal(t, executable, []string{"A"}, directory, nil)
	if result.status != 1 || result.stdout != "" || !strings.Contains(result.stderr, "invalid package name A") {
		t.Fatalf("sanitized failure: status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
}
