package main

// Runtime tests for the argv() built-in. The generated C is compared between the
// Go and Kitten pipelines, then each pipeline's executable is run with the same
// argument vectors and must print its own program name followed by every
// argument. An empty argument and an argument containing spaces are included.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// argvSource prints the argument count and every argument, including the
// program name at index 0.
const argvSource = `fn main() {
    print(length(argv()));
    int index = 0;
    while (index < length(argv())) {
        print(argv()[index]);
        index++;
    }
}
`

// expectedArgvOutput renders what the fixture prints for one executable and its
// arguments.
func expectedArgvOutput(program string, arguments []string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%d\n", len(arguments)+1)
	builder.WriteString(program)
	builder.WriteString("\n")
	for _, argument := range arguments {
		builder.WriteString(argument)
		builder.WriteString("\n")
	}
	return builder.String()
}

// runProgramWithArgs runs an already-built program and returns its streams and
// status.
func runProgramWithArgs(t *testing.T, program, directory string, arguments []string) programResult {
	t.Helper()

	command := exec.Command(program, arguments...)
	command.Dir = directory
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !asExitError(err, &exit) {
			t.Fatalf("running %s: %v", program, err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

// asExitError is errors.As for *exec.ExitError without another import in this
// file.
func asExitError(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

// TestArgvRuntime checks argv() through both pipelines with several argument
// vectors.
func TestArgvRuntime(t *testing.T) {
	requireToolchain(t)

	sourceDirectory := t.TempDir()
	sourcePath := filepath.Join(sourceDirectory, "main.ktn")
	if err := os.WriteFile(sourcePath, []byte(argvSource), 0o644); err != nil {
		t.Fatalf("writing main.ktn: %v", err)
	}

	goExecutable := filepath.Join(sourceDirectory, "go-program")
	var diagnostics strings.Builder
	if err := buildExecutable(sourcePath, goExecutable, &diagnostics); err != nil {
		t.Fatalf("building the Go-pipeline program: %v\n%s", err, diagnostics.String())
	}

	kittenExecutable := buildKittenUnit(t)
	kittenProbeDirectory := t.TempDir()
	kittenText, err := runKittenUnit(kittenExecutable, kittenProbeDirectory, argvSource, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	_, _, generated, ok := prepareUnit(t, "argv runtime", argvSource, "")
	if !ok {
		t.Fatalf("the reference rejected the argv program")
	}
	if difference := unitDifference(kittenText, generated); difference != "" {
		t.Fatalf("the argv translation units differ: %s", difference)
	}

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	cases := [][]string{
		{},
		{"one"},
		{"alpha", "", "two words", "--flag=value"},
	}
	for _, arguments := range cases {
		goResult := runProgramWithArgs(t, goExecutable, sourceDirectory, arguments)
		goWant := expectedArgvOutput(goExecutable, arguments)
		if goResult.stdout != goWant || goResult.stderr != "" || goResult.status != 0 {
			t.Errorf("Go pipeline with %q printed %q (want %q), wrote %q to stderr, exited %d",
				arguments, goResult.stdout, goWant, goResult.stderr, goResult.status)
		}

		kittenDirectory := t.TempDir()
		kittenResult, err := buildAndRunProgramWithArgs(t, compiler, compileFlags, linkFlags, kittenDirectory, kittenText, 0, arguments)
		if err != nil {
			t.Fatalf("Kitten pipeline with %q: %v", arguments, err)
		}
		kittenWant := expectedArgvOutput(filepath.Join(kittenDirectory, "program"), arguments)
		if kittenResult.stdout != kittenWant || kittenResult.stderr != "" || kittenResult.status != 0 {
			t.Errorf("Kitten pipeline with %q printed %q (want %q), wrote %q to stderr, exited %d",
				arguments, kittenResult.stdout, kittenWant, kittenResult.stderr, kittenResult.status)
		}
	}
	t.Logf("argv: %d argument vectors matched under %s", len(cases), filepath.Base(compiler[0]))
}
