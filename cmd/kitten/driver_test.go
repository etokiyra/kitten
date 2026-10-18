package main

// Tests for the Kitten-written compiler driver and the POSIX-sh wrapper.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/codegen"
	"kitten/loader"
)

// repoPath returns an absolute path inside the repository.
func repoPath(t *testing.T, parts ...string) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("resolving %v: %v", parts, err)
	}
	return path
}

// buildStage1 compiles the Kitten-written driver with the Go compiler.
func buildStage1(t *testing.T) string {
	t.Helper()

	requireToolchain(t)
	executable := filepath.Join(t.TempDir(), "kitten-stage1")
	var diagnostics bytes.Buffer
	if err := buildExecutable(repoPath(t, "..", "..", "selfhost", "driver.ktn"), executable, &diagnostics); err != nil {
		t.Fatalf("building stage 1: %v\n%s", err, diagnostics.String())
	}
	if output := strings.TrimSpace(diagnostics.String()); output != "" {
		t.Fatalf("C compiler diagnostics while building stage 1:\n%s", output)
	}
	return executable
}

// runExternal runs a program and returns its streams and status.
func runExternal(t *testing.T, executable string, arguments []string, directory string, environment []string) programResult {
	t.Helper()

	command := exec.Command(executable, arguments...)
	command.Dir = directory
	if environment != nil {
		command.Env = append(os.Environ(), environment...)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !asExitError(err, &exit) {
			t.Fatalf("running %s: %v", executable, err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

// wrapperPath returns the wrapper's absolute path.
func wrapperPath(t *testing.T) string {
	return repoPath(t, "..", "..", "selfhost", "driver", "kittenc.sh")
}

// wrapperEnvironment selects the stage-1 driver and the repository runtime.
func wrapperEnvironment(stage1 string, t *testing.T) []string {
	return []string{
		"KITTENC_DRIVER=" + stage1,
		"KITTEN_RUNTIME_DIR=" + repoPath(t, "..", "..", "runtime", "csrc"),
	}
}

// TestKittenDriverFixtures checks stage-1 C output and wrapper-built executables
// for every fixture and tests/modules.
func TestKittenDriverFixtures(t *testing.T) {
	stage1 := buildStage1(t)
	wrapper := wrapperPath(t)

	type fixture struct {
		name     string
		root     string
		expected string
	}
	var fixtures []fixture
	for _, name := range fixtureNames() {
		fixtures = append(fixtures, fixture{
			name:     name,
			root:     repoPath(t, "..", "..", "tests", name+".ktn"),
			expected: repoPath(t, "..", "..", "tests", name+".expected"),
		})
	}
	fixtures = append(fixtures, fixture{
		name:     "modules",
		root:     repoPath(t, "..", "..", "tests", "modules", "main.ktn"),
		expected: repoPath(t, "..", "..", "tests", "modules", "main.expected"),
	})

	for _, testCase := range fixtures {
		modules, err := loader.Load(testCase.root)
		if err != nil {
			t.Errorf("%s: reference loader: %v", testCase.name, err)
			continue
		}
		checked, checkError := checker.CheckModules(modules)
		if checkError != nil {
			t.Errorf("%s: reference checker: %v", testCase.name, checkError)
			continue
		}
		referenceText, generationError := codegen.Generate(modules, checked)
		if generationError != nil {
			t.Errorf("%s: reference generator: %v", testCase.name, generationError)
			continue
		}

		stageResult := runExternal(t, stage1, []string{testCase.root}, repoPath(t, "."), nil)
		if stageResult.status != 0 || stageResult.stderr != "" {
			t.Errorf("%s: stage 1 exited %d and wrote %q to stderr", testCase.name, stageResult.status, stageResult.stderr)
			continue
		}
		if stageResult.stdout != referenceText {
			t.Errorf("%s: stage 1 C differs from the reference\ngot:\n%s\nwant:\n%s", testCase.name, stageResult.stdout, referenceText)
			continue
		}

		output := filepath.Join(t.TempDir(), "program")
		buildResult := runExternal(t, wrapper, []string{"build", testCase.root, "-o", output}, repoPath(t, "."), wrapperEnvironment(stage1, t))
		if buildResult.status != 0 || buildResult.stderr != "" {
			t.Errorf("%s: wrapper build exited %d and wrote %q to stderr", testCase.name, buildResult.status, buildResult.stderr)
			continue
		}
		expected, err := os.ReadFile(testCase.expected)
		if err != nil {
			t.Errorf("%s: reading expected output: %v", testCase.name, err)
			continue
		}
		runResult := runExternal(t, output, nil, repoPath(t, "."), nil)
		if runResult.status != 0 || runResult.stderr != "" || runResult.stdout != string(expected) {
			t.Errorf("%s: wrapper program exited %d, wrote %q to stderr, printed\n%q\nwant\n%q",
				testCase.name, runResult.status, runResult.stderr, runResult.stdout, string(expected))
		}
	}
	t.Logf("driver: %d fixtures matched the reference C and expected output", len(fixtures))
}

// TestKittenDriverWrapperArgumentsAndStatus checks argument passing, exit-status
// propagation, and default output naming through the wrapper.
func TestKittenDriverWrapperArgumentsAndStatus(t *testing.T) {
	stage1 := buildStage1(t)
	wrapper := wrapperPath(t)
	environment := wrapperEnvironment(stage1, t)

	directory := t.TempDir()
	source := `fn main() {
    print(length(argv()) - 1);
    int index = 1;
    while (index < length(argv())) {
        print(argv()[index]);
        index++;
    }
    exit(7);
}
`
	sourcePath := filepath.Join(directory, "arguments.ktn")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("writing arguments.ktn: %v", err)
	}

	result := runExternal(t, wrapper, []string{"run", sourcePath, "alpha", "", "two words"}, directory, environment)
	const want = "3\nalpha\n\ntwo words\n"
	if result.status != 7 || result.stderr != "" || result.stdout != want {
		t.Errorf("wrapper run exited %d, wrote %q to stderr, printed %q (want %q)",
			result.status, result.stderr, result.stdout, want)
	}

	defaultSource := "fn main() {\n    print(\"default output\");\n}\n"
	if err := os.WriteFile(filepath.Join(directory, "default.ktn"), []byte(defaultSource), 0o644); err != nil {
		t.Fatalf("writing default.ktn: %v", err)
	}
	buildResult := runExternal(t, wrapper, []string{"build", "default.ktn"}, directory, environment)
	if buildResult.status != 0 || buildResult.stderr != "" {
		t.Fatalf("wrapper default build exited %d and wrote %q to stderr", buildResult.status, buildResult.stderr)
	}
	defaultProgram := filepath.Join(directory, "default")
	runResult := runExternal(t, defaultProgram, nil, directory, nil)
	if runResult.status != 0 || runResult.stderr != "" || runResult.stdout != "default output\n" {
		t.Errorf("default output program exited %d, wrote %q to stderr, printed %q",
			runResult.status, runResult.stderr, runResult.stdout)
	}
	t.Logf("driver: wrapper passed arguments, propagated status 7, and used the default output name")
}

// TestKittenDriverDiagnostics compares stage 1 and wrapper usage errors with the
// Go CLI and checks the documented exclusions.
func TestKittenDriverDiagnostics(t *testing.T) {
	stage1 := buildStage1(t)
	wrapper := wrapperPath(t)
	environment := wrapperEnvironment(stage1, t)

	directory := t.TempDir()
	write := func(name, source string) string {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}

	type errorCase struct {
		name string
		path string
	}
	cases := []errorCase{
		{"type error", write("type.ktn", "fn main() {\n    int x = \"s\";\n}\n")},
		{"syntax error", write("syntax.ktn", "fn main( {\n}\n")},
		{"import syntax error", write("importsyntax.ktn", "import \"bad.ktn\";\nfn main() {\n}\n")},
		{"outside root", write("inside/outside.ktn", "import \"../outside.ktn\";\nfn main() {\n}\n")},
	}
	// The imported module must exist with a syntax error for the import case.
	if err := os.WriteFile(filepath.Join(directory, "bad.ktn"), []byte("fn broken( {\n"), 0o644); err != nil {
		t.Fatalf("writing bad.ktn: %v", err)
	}

	for _, testCase := range cases {
		var goStdout, goStderr bytes.Buffer
		goStatus := run([]string{"build", testCase.path}, &goStdout, &goStderr)
		stageResult := runExternal(t, stage1, []string{testCase.path}, directory, nil)
		if stageResult.status != goStatus || stageResult.stderr != goStderr.String() || stageResult.stdout != goStdout.String() {
			t.Errorf("%s: driver differs from the Go CLI\nstatus: %d vs %d\nstderr:\n%s\nwant:\n%s\nstdout:\n%s\nwant:\n%s",
				testCase.name, stageResult.status, goStatus, stageResult.stderr, goStderr.String(), stageResult.stdout, goStdout.String())
		}
	}

	// No arguments: the wrapper must reproduce the Go CLI's usage and status.
	var goStdout, goStderr bytes.Buffer
	goStatus := run(nil, &goStdout, &goStderr)
	wrapperResult := runExternal(t, wrapper, nil, directory, environment)
	if wrapperResult.status != goStatus || wrapperResult.stderr != goStderr.String() || wrapperResult.stdout != goStdout.String() {
		t.Errorf("no arguments: wrapper differs from the Go CLI\nstatus: %d vs %d\nstderr:\n%s\nwant:\n%s",
			wrapperResult.status, goStatus, wrapperResult.stderr, goStderr.String())
	}

	// The driver itself rejects a relative source path with status 2.
	relative := runExternal(t, stage1, []string{"relative.ktn"}, directory, nil)
	if relative.status != 2 || !strings.Contains(relative.stderr, "must be absolute") {
		t.Errorf("relative path: status %d, stderr %q", relative.status, relative.stderr)
	}

	// A missing import is fatal and nonzero; its text is a documented exclusion.
	missing := write("missing.ktn", "import \"not-there.ktn\";\nfn main() {\n}\n")
	missingResult := runExternal(t, stage1, []string{missing}, directory, nil)
	if missingResult.status == 0 || missingResult.stderr == "" {
		t.Errorf("missing import: status %d, stderr %q", missingResult.status, missingResult.stderr)
	}
}
