package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// requireToolchain skips a test when no usable native toolchain is present. The
// probe compiles and runs a trivial program once per test binary, so tests that
// need a C compiler and Boehm GC report the prerequisite clearly instead of
// silently passing.
func requireToolchain(t *testing.T) {
	t.Helper()

	toolchainOnce.Do(func() {
		toolchainError = probeToolchain()
	})
	if toolchainError != nil {
		t.Skipf("prerequisite not met: %v", toolchainError)
	}
}

var (
	toolchainOnce  sync.Once
	toolchainError error
)

func probeToolchain() error {
	directory, err := os.MkdirTemp("", "kitten-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)

	sourcePath := filepath.Join(directory, "probe.ktn")
	executablePath := filepath.Join(directory, "probe")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n    print(\"probe\");\n}\n"), 0o644); err != nil {
		return err
	}

	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executablePath, &diagnostics); err != nil {
		return errors.New("need a C compiler (set CC or install cc/gcc/clang) and Boehm GC development files (libgc): " + err.Error() + "\n" + diagnostics.String())
	}
	output, err := exec.Command(executablePath).Output()
	if err != nil {
		return errors.New("could not run the probe executable: " + err.Error())
	}
	if string(output) != "probe\n" {
		return errors.New("the probe executable produced unexpected output: " + string(output))
	}
	return nil
}

// compileAndRunFile builds a source file and runs the executable, returning its
// stdout, any C compiler diagnostics, and its exit code.
func compileAndRunFile(t *testing.T, sourcePath string) (string, string, int) {
	t.Helper()

	directory := t.TempDir()
	executablePath := filepath.Join(directory, "program")

	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executablePath, &diagnostics); err != nil {
		t.Fatalf("building %s: %v\n%s", sourcePath, err, diagnostics.String())
	}

	command := exec.Command(executablePath)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return stdout.String(), diagnostics.String(), exitError.ExitCode()
		}
		t.Fatalf("running %s: %v", executablePath, err)
	}
	return stdout.String(), diagnostics.String(), 0
}

// compileAndRunSource writes source to a temporary file, then builds and runs it.
func compileAndRunSource(t *testing.T, source string) (string, string, int) {
	t.Helper()

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "program.ktn")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	return compileAndRunFile(t, sourcePath)
}

func TestParseCommandArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    commandArgs
		wantErr bool
	}{
		{name: "source only", args: []string{"main.ktn"}, want: commandArgs{source: "main.ktn"}},
		{name: "output before", args: []string{"-o", "out", "main.ktn"}, want: commandArgs{source: "main.ktn", output: "out"}},
		{name: "output after", args: []string{"main.ktn", "-o", "out"}, want: commandArgs{source: "main.ktn", output: "out"}},
		{name: "output equals", args: []string{"main.ktn", "-o=out"}, want: commandArgs{source: "main.ktn", output: "out"}},
		{name: "long output", args: []string{"--output", "out", "main.ktn"}, want: commandArgs{source: "main.ktn", output: "out"}},
		{name: "double dash", args: []string{"--", "-weird-name.ktn"}, want: commandArgs{source: "-weird-name.ktn"}},
		{name: "missing source", args: nil, wantErr: true},
		{name: "too many sources", args: []string{"a.ktn", "b.ktn"}, wantErr: true},
		{name: "missing output path", args: []string{"main.ktn", "-o"}, wantErr: true},
		{name: "unknown option", args: []string{"--verbose", "main.ktn"}, wantErr: true},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			got, err := parseCommandArgs(testCase.args)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("parseCommandArgs(%v) = %+v, want an error", testCase.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCommandArgs(%v) returned error: %v", testCase.args, err)
			}
			if got != testCase.want {
				t.Errorf("parseCommandArgs(%v) = %+v, want %+v", testCase.args, got, testCase.want)
			}
		})
	}
}

func TestDefaultOutputPath(t *testing.T) {
	cases := map[string]string{
		"hello.ktn":            "hello",
		"tests/hello.ktn":      "hello",
		"/tmp/a/b/program.ktn": "program",
	}
	for input, want := range cases {
		if got := defaultOutputPath(input); got != want {
			t.Errorf("defaultOutputPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUsageAndExitCodes(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantCode int
		contains string
	}{
		{name: "no arguments", args: nil, wantCode: exitUsage, contains: "Usage:"},
		{name: "help", args: []string{"--help"}, wantCode: exitSuccess, contains: "Usage:"},
		{name: "unknown command", args: []string{"frobnicate"}, wantCode: exitUsage, contains: "unknown command"},
		{name: "build missing source", args: []string{"build"}, wantCode: exitUsage, contains: "missing source file"},
		{name: "build wrong extension", args: []string{"build", "main.txt"}, wantCode: exitUsage, contains: "not a Kitten source file"},
		{name: "run with output flag", args: []string{"run", "main.ktn", "-o", "out"}, wantCode: exitUsage, contains: "-o is not supported"},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(testCase.args, &stdout, &stderr)
			if code != testCase.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %s)", code, testCase.wantCode, stderr.String())
			}
			combined := stdout.String() + stderr.String()
			if !strings.Contains(combined, testCase.contains) {
				t.Errorf("output %q does not contain %q", combined, testCase.contains)
			}
		})
	}
}

func TestFixturesEndToEnd(t *testing.T) {
	requireToolchain(t)

	fixtures := []string{"hello", "fizzbuzz", "math", "room", "strings", "arrays", "variants", "maps"}
	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			sourcePath := filepath.Join("..", "..", "tests", name+".ktn")
			expectedPath := filepath.Join("..", "..", "tests", name+".expected")

			expected, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("reading %s: %v", expectedPath, err)
			}

			stdout, diagnostics, exitCode := compileAndRunFile(t, sourcePath)
			if exitCode != 0 {
				t.Fatalf("program exited with status %d", exitCode)
			}
			if stdout != string(expected) {
				t.Errorf("output mismatch\n got: %q\nwant: %q", stdout, string(expected))
			}
			if warnings := strings.TrimSpace(diagnostics); warnings != "" {
				t.Errorf("C compiler emitted diagnostics for %s:\n%s", name, warnings)
			}
		})
	}
}

func TestRunCommandForwardsOutput(t *testing.T) {
	requireToolchain(t)

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "greet.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n    print(\"run works\");\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"run", sourcePath}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	if stdout.String() != "run works\n" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "run works\n")
	}
}

func TestStringEqualityIsValueBased(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    string first = "a" + "b";
    string second = "a" + "b";
    print(first == second, first != second);
    string empty = "";
    print(first == (empty + first));
    print(("x" + "y") == "xy");
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := "true false\ntrue\ntrue\n"
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestStringConcatenationChain(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    string result = "";
    int index = 0;
    while (index < 5) {
        result += "ab";
        index++;
    }
    print(result);
    print(result == "ababababab");
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := "ababababab\ntrue\n"
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestStringStructFieldMutation(t *testing.T) {
	requireToolchain(t)

	source := `struct Builder {
    string text;
}
fn main() {
    Builder builder = Builder { text = "a" };
    builder.text += "b";
    builder.text = builder.text + "c";
    print(builder.text);
    print(builder.text == "abc");
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := "abc\ntrue\n"
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

// runCompiledProgram builds source and runs the program, returning its stdout,
// stderr, and exit code. It fails the test on a build error or unexpected C
// compiler diagnostics.
func runCompiledProgram(t *testing.T, source string) (string, string, int) {
	t.Helper()
	return runCompiledProgramInDir(t, source, "")
}

// runCompiledProgramInDir is runCompiledProgram with an explicit working
// directory for the executed program, used by file-I/O tests.
func runCompiledProgramInDir(t *testing.T, source, workingDirectory string) (string, string, int) {
	t.Helper()

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "program.ktn")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	executablePath := filepath.Join(directory, "program")

	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executablePath, &diagnostics); err != nil {
		t.Fatalf("building: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics:\n%s", warnings)
	}

	command := exec.Command(executablePath)
	command.Dir = workingDirectory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return stdout.String(), stderr.String(), exitError.ExitCode()
		}
		t.Fatalf("running %s: %v", executablePath, err)
	}
	return stdout.String(), stderr.String(), 0
}

func TestArrayOutOfRangeIndexIsARuntimeError(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    int[] values = new int[2];
    values[0] = 1;
    print(values[0]);
    print(values[5]);
}
`
	stdout, stderr, exitCode := runCompiledProgram(t, source)
	if exitCode == 0 {
		t.Fatalf("program exited with status 0, want a nonzero status")
	}
	if !strings.Contains(stderr, "out of range") {
		t.Errorf("stderr = %q, want an out-of-range message", stderr)
	}
	if !strings.Contains(stdout, "1") {
		t.Errorf("stdout = %q, want output produced before the failing access", stdout)
	}
}

func TestArrayNegativeIndexIsARuntimeError(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    int[] values = new int[2];
    values[-1] = 1;
}
`
	_, stderr, exitCode := runCompiledProgram(t, source)
	if exitCode == 0 {
		t.Fatalf("program exited with status 0, want a nonzero status")
	}
	if !strings.Contains(stderr, "out of range") {
		t.Errorf("stderr = %q, want an out-of-range message", stderr)
	}
}

func TestArrayIndexEvaluatedOnceInModification(t *testing.T) {
	requireToolchain(t)

	// The index expression has an observable side effect. Compound append and
	// increment must evaluate it exactly once.
	source := `fn pick() -> int {
    print("pick");
    return 0;
}
fn main() {
    string[] words = new string[1];
    words[0] = "a";
    words[pick()] += "b";
    int[] numbers = new int[1];
    numbers[pick()]++;
    print(words[0], numbers[0]);
}
`
	stdout, _, exitCode := runCompiledProgram(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := "pick\npick\nab 1\n"
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestStructAliasingAndMutation(t *testing.T) {
	requireToolchain(t)

	source := `struct Point {
    int x;
    int y;
}
fn move(Point p, int dx, int dy) {
    p.x = p.x + dx;
    p.y = p.y + dy;
}
fn main() {
    Point a = Point { x = 1, y = 2 };
    Point b = a;
    b.x = 10;
    print(a.x, a.y, b.x, b.y);
    move(a, 5, 5);
    print(a.x, a.y);
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	if want := "10 2 10 2\n15 7\n"; stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestNestedStructsAndReturnedStructs(t *testing.T) {
	requireToolchain(t)

	source := `struct Point {
    int x;
    int y;
}
struct Line {
    Point start;
    Point end;
}
fn make_point() -> Point {
    return Point { x = 7, y = 8 };
}
fn length_squared(Line line) -> int {
    int dx = line.end.x - line.start.x;
    int dy = line.end.y - line.start.y;
    return dx * dx + dy * dy;
}
fn main() {
    Point origin = Point { x = 0, y = 0 };
    Point p = Point { x = 3, y = 4 };
    Line line = Line { start = origin, end = p };
    print(length_squared(line));
    line.start.x = 3;
    print(line.start.x, line.end.x);
    Point returned = make_point();
    print(returned.x, returned.y);
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := "25\n3 3\n7 8\n"
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestArithmeticComparisonAndControlFlow(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    int a = 7;
    int b = 3;
    print(a + b, a - b, a * b, a / b, a % b);
    print(a == 7, a != 7, a < b, a > b, a <= 7, a >= 7);
    print(true && false, true || false, !true);
    float f = 2.5;
    float g = 4.0;
    print(f + g, f * g);
    bool flag = false;
    if (a > b) {
        flag = true;
    }
    print(flag);
    int sum = 0;
    for (int i = 1; i <= 4; i++) {
        sum += i;
    }
    print(sum);
    int countdown = 3;
    while (countdown > 0) {
        print(countdown);
        countdown--;
    }
    string s = "hi";
    print(s == "hi", s != "bye");
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := strings.Join([]string{
		"10 4 21 2 1",
		"true false false true true true",
		"false true false",
		"6.5 10",
		"true",
		"10",
		"3",
		"2",
		"1",
		"true true",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestBuildWritesExecutableAtExplicitPath(t *testing.T) {
	requireToolchain(t)

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "program.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n    print(\"built\");\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	for _, flag := range [][]string{{"-o"}, {"--output"}} {
		output := filepath.Join(directory, strings.Join(flag, "")+".out")
		args := append(append([]string{"build", sourcePath}, flag...), output)
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitSuccess {
			t.Fatalf("run(%v) exit = %d, stderr: %s", args, code, stderr.String())
		}
		info, err := os.Stat(output)
		if err != nil {
			t.Fatalf("expected executable at %s: %v", output, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable", output)
		}
	}
}

func TestBuildUsesDefaultOutputPath(t *testing.T) {
	requireToolchain(t)

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "default.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n    print(\"default\");\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(originalDirectory)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", "default.ktn"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(directory, "default")); err != nil {
		t.Fatalf("expected executable next to the source: %v", err)
	}
}

func TestPathWithSpaces(t *testing.T) {
	requireToolchain(t)

	base := t.TempDir()
	directory := filepath.Join(base, "dir with spaces")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sourcePath := filepath.Join(directory, "hello program.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n    print(\"spaced\");\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	executablePath := filepath.Join(directory, "out binary")

	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executablePath, &diagnostics); err != nil {
		t.Fatalf("build failed: %v\n%s", err, diagnostics.String())
	}
	output, err := exec.Command(executablePath).Output()
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if string(output) != "spaced\n" {
		t.Errorf("output = %q, want %q", output, "spaced\n")
	}
}

func TestSemanticErrorIsRejectedBeforeCompilation(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "bad.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n    int x = y;\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	output := filepath.Join(directory, "bad")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", sourcePath, "-o", output}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if _, err := os.Stat(output); err == nil {
		t.Errorf("an executable was produced for an invalid program")
	}
	message := stderr.String()
	if !strings.Contains(message, "undefined name") {
		t.Errorf("stderr %q does not mention the undefined name", message)
	}
	if !strings.Contains(message, "2:13") {
		t.Errorf("stderr %q does not include the source position", message)
	}
}

func TestSyntaxErrorIsRejected(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "bad.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n    int x = ;\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", sourcePath}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), "expected an expression") {
		t.Errorf("stderr %q does not describe the syntax error", stderr.String())
	}
}

func TestMissingEntryPointIsReported(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "library.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn helper() {\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", sourcePath, "-o", filepath.Join(directory, "out")}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), "no entry point") {
		t.Errorf("stderr %q does not mention the missing entry point", stderr.String())
	}
}

func TestMissingCompilerIsReported(t *testing.T) {
	t.Setenv("CC", "definitely-not-a-real-compiler-binary")

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "program.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", sourcePath, "-o", filepath.Join(directory, "out")}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), "CC") {
		t.Errorf("stderr %q does not mention CC", stderr.String())
	}
}

func TestRunDoesNotLeakTemporaryDirectories(t *testing.T) {
	requireToolchain(t)

	before := temporaryBuildDirectories(t)

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "program.ktn")
	if err := os.WriteFile(sourcePath, []byte("fn main() {\n    print(\"cleanup\");\n}\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", sourcePath}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}

	after := temporaryBuildDirectories(t)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("temporary build directories leaked:\n before: %v\n after:  %v", before, after)
	}
}

func temporaryBuildDirectories(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("reading temp directory: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "kitten-run-") || strings.HasPrefix(entry.Name(), "kitten-build-") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestModuleFixtureEndToEnd(t *testing.T) {
	requireToolchain(t)

	sourcePath := filepath.Join("..", "..", "tests", "modules", "main.ktn")
	expectedPath := filepath.Join("..", "..", "tests", "modules", "main.expected")
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("reading %s: %v", expectedPath, err)
	}

	stdout, diagnostics, exitCode := compileAndRunFile(t, sourcePath)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	if stdout != string(expected) {
		t.Errorf("output mismatch\n got: %q\nwant: %q", stdout, string(expected))
	}
	if warnings := strings.TrimSpace(diagnostics); warnings != "" {
		t.Errorf("C compiler diagnostics:\n%s", warnings)
	}
}

func TestMultiModuleProgramInTemporaryProject(t *testing.T) {
	requireToolchain(t)

	project := t.TempDir()
	writeSource(t, filepath.Join(project, "lib.ktn"), "public fn value() -> int {\n    return 42;\n}\n")
	writeSource(t, filepath.Join(project, "sub", "helper.ktn"), "import \"../lib.ktn\";\npublic fn doubled() -> int {\n    return value() * 2;\n}\n")
	mainPath := writeSource(t, filepath.Join(project, "main.ktn"), "import \"sub/helper.ktn\";\nfn main() {\n    print(doubled());\n}\n")

	// Build and run with a working directory unrelated to the project, to prove
	// resolution is relative to the sources rather than the current directory.
	executablePath := filepath.Join(t.TempDir(), "program")
	var diagnostics bytes.Buffer
	if err := buildExecutable(mainPath, executablePath, &diagnostics); err != nil {
		t.Fatalf("building: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics:\n%s", warnings)
	}

	command := exec.Command(executablePath)
	command.Dir = t.TempDir()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if string(output) != "84\n" {
		t.Errorf("output = %q, want %q", output, "84\n")
	}
}

func TestReadFileNative(t *testing.T) {
	requireToolchain(t)

	directory := t.TempDir()
	writeSource(t, filepath.Join(directory, "data.txt"), "alpha\nbeta\n")
	writeSource(t, filepath.Join(directory, "empty.txt"), "")

	source := `fn main() {
    string data = read_file("data.txt");
    print(data == "alpha\nbeta\n");
    print(data);
    print("[" + read_file("empty.txt") + "]");
}
`
	stdout, _, exitCode := runCompiledProgramInDir(t, source, directory)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := "true\nalpha\nbeta\n\n[]\n"
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestReadFileMissingIsFatal(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    print(read_file("missing.txt"));
}
`
	stdout, stderr, exitCode := runCompiledProgramInDir(t, source, t.TempDir())
	if exitCode == 0 {
		t.Fatalf("program exited with status 0, want a nonzero status")
	}
	if !strings.Contains(stderr, "cannot open") {
		t.Errorf("stderr = %q, want a read_file failure message", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want no output", stdout)
	}
}

func TestModuleMissingImportIsReported(t *testing.T) {
	directory := t.TempDir()
	mainPath := writeSource(t, filepath.Join(directory, "main.ktn"), "import \"nope.ktn\";\nfn main() {\n}\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", mainPath, "-o", filepath.Join(directory, "out")}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), "nope.ktn") {
		t.Errorf("stderr %q does not name the missing module", stderr.String())
	}
}

func TestModuleWithoutEntryPointIsRejected(t *testing.T) {
	directory := t.TempDir()
	writeSource(t, filepath.Join(directory, "lib.ktn"), "public fn value() -> int {\n    return 1;\n}\n")
	mainPath := writeSource(t, filepath.Join(directory, "main.ktn"), "import \"lib.ktn\";\nfn helper() {\n}\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", mainPath, "-o", filepath.Join(directory, "out")}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), "no entry point") {
		t.Errorf("stderr %q does not mention the missing entry point", stderr.String())
	}
}

// writeSource writes a file, creating parent directories, and returns its path.
func writeSource(t *testing.T, path, content string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestMatchOnNullUnionIsARuntimeError(t *testing.T) {
	requireToolchain(t)

	source := `union Box {
    Full { int value; }
    Empty
}
fn main() {
    Box[] boxes = new Box[1];
    Box box = boxes[0];
    match (box) {
        Full(value) { print(value); }
        Empty { print("empty"); }
    }
}
`
	_, stderr, exitCode := runCompiledProgram(t, source)
	if exitCode == 0 {
		t.Fatalf("program exited with status 0, want a nonzero status")
	}
	if !strings.Contains(stderr, "null union reference") {
		t.Errorf("stderr = %q, want a null union reference message", stderr)
	}
}

func TestVariantModulesInTemporaryProject(t *testing.T) {
	requireToolchain(t)

	project := t.TempDir()
	writeSource(t, filepath.Join(project, "model.ktn"), `public enum Kind {
    Integer,
    Text
}
public union Value {
    Number { int amount; }
    Empty
}
public fn describe(Kind kind) -> string {
    match (kind) {
        Integer { return "int"; }
        Text { return "text"; }
    }
}
public fn amount(Value value) -> int {
    match (value) {
        Number(amount) { return amount; }
        Empty { return 0; }
    }
}
`)
	mainPath := writeSource(t, filepath.Join(project, "main.ktn"), `import "model.ktn";
fn main() {
    Kind kind = Kind.Text;
    Value value = Value.Number { amount = 7 };
    print(describe(kind), amount(value));
    print(amount(Value.Empty));
}
`)

	executablePath := filepath.Join(t.TempDir(), "program")
	var diagnostics bytes.Buffer
	if err := buildExecutable(mainPath, executablePath, &diagnostics); err != nil {
		t.Fatalf("building: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics:\n%s", warnings)
	}

	output, err := exec.Command(executablePath).Output()
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if string(output) != "text 7\n0\n" {
		t.Errorf("output = %q, want %q", output, "text 7\n0\n")
	}
}

func TestMapMissingKeyIsARuntimeError(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    map[string]int table = new map[string]int;
    table["present"] = 1;
    print(has(table, "absent"));
    print(table["absent"]);
}
`
	stdout, stderr, exitCode := runCompiledProgram(t, source)
	if exitCode == 0 {
		t.Fatalf("program exited with status 0, want a nonzero status")
	}
	if !strings.Contains(stderr, "no entry for key") {
		t.Errorf("stderr = %q, want a missing-key message", stderr)
	}
	if stdout != "false\n" {
		t.Errorf("stdout = %q, want output produced before the failing lookup", stdout)
	}
}

func TestMapHandlesHashCollisions(t *testing.T) {
	requireToolchain(t)

	// "aa" and "ai" both hash to slot 7 in a freshly grown table (FNV-1a modulo
	// 8), so they collide and force linear probing.
	source := `fn main() {
    map[string]int table = new map[string]int;
    table["aa"] = 1;
    table["ai"] = 2;
    print(length(table), table["aa"], table["ai"], has(table, "ab"));
    table["aa"] = 11;
    print(table["aa"], table["ai"], length(table));
}
`
	stdout, _, exitCode := runCompiledProgram(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := "2 1 2 false\n11 2 2\n"
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestMapGrowthPreservesEntries(t *testing.T) {
	requireToolchain(t)

	const keyCount = 100
	var builder strings.Builder
	builder.WriteString("fn main() {\n")
	builder.WriteString("    map[string]int table = new map[string]int;\n")
	for index := 0; index < keyCount; index++ {
		fmt.Fprintf(&builder, "    table[\"key%d\"] = %d;\n", index, index)
	}
	builder.WriteString("    int total = 0;\n")
	for index := 0; index < keyCount; index++ {
		fmt.Fprintf(&builder, "    total += table[\"key%d\"];\n", index)
	}
	builder.WriteString("    print(length(table), total, has(table, \"key42\"), has(table, \"key100\"));\n")
	builder.WriteString("}\n")

	stdout, stderr, exitCode := runCompiledProgram(t, builder.String())
	if exitCode != 0 {
		t.Fatalf("program exited with status %d (stderr %q)", exitCode, stderr)
	}
	want := "100 4950 true false\n"
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestMapModulesInTemporaryProject(t *testing.T) {
	requireToolchain(t)

	project := t.TempDir()
	writeSource(t, filepath.Join(project, "symbols.ktn"), `public struct Symbol {
    int line;
}
public fn line_of(map[string]Symbol table, string name) -> int {
    if (has(table, name)) {
        return table[name].line;
    }
    return -1;
}
`)
	mainPath := writeSource(t, filepath.Join(project, "main.ktn"), `import "symbols.ktn";
fn main() {
    map[string]Symbol symbols = new map[string]Symbol;
    symbols["parse"] = Symbol { line = 42 };
    print(line_of(symbols, "parse"), line_of(symbols, "missing"), length(symbols));
}
`)

	executablePath := filepath.Join(t.TempDir(), "program")
	var diagnostics bytes.Buffer
	if err := buildExecutable(mainPath, executablePath, &diagnostics); err != nil {
		t.Fatalf("building: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics:\n%s", warnings)
	}

	output, err := exec.Command(executablePath).Output()
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if string(output) != "42 -1 1\n" {
		t.Errorf("output = %q, want %q", output, "42 -1 1\n")
	}
}
