package main

// Runtime tests for the eprint() built-in. The generated C is compared between
// the Go and Kitten pipelines, then each pipeline's executable is run and its
// stdout and stderr are captured separately: stdout must contain only print
// output and stderr only eprint output.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// eprintSource exercises every printable type on both streams, including empty
// strings and multiple arguments.
const eprintSource = `fn main() {
    print("out one");
    eprint("err one");
    print("out two", 2, 2.5, true);
    eprint("err two", 2, 2.5, true);
    print("");
    eprint("");
}
`

// TestEprintRuntime checks stdout and stderr separately through both pipelines.
func TestEprintRuntime(t *testing.T) {
	requireToolchain(t)

	sourceDirectory := t.TempDir()
	sourcePath := filepath.Join(sourceDirectory, "main.ktn")
	if err := os.WriteFile(sourcePath, []byte(eprintSource), 0o644); err != nil {
		t.Fatalf("writing main.ktn: %v", err)
	}

	goExecutable := filepath.Join(sourceDirectory, "go-program")
	var diagnostics strings.Builder
	if err := buildExecutable(sourcePath, goExecutable, &diagnostics); err != nil {
		t.Fatalf("building the Go-pipeline program: %v\n%s", err, diagnostics.String())
	}

	kittenExecutable := buildKittenUnit(t)
	kittenProbeDirectory := t.TempDir()
	kittenText, err := runKittenUnit(kittenExecutable, kittenProbeDirectory, eprintSource, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	_, _, generated, ok := prepareUnit(t, "eprint runtime", eprintSource, "")
	if !ok {
		t.Fatalf("the reference rejected the eprint program")
	}
	if difference := unitDifference(kittenText, generated); difference != "" {
		t.Fatalf("the eprint translation units differ: %s", difference)
	}

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	const wantStdout = "out one\nout two 2 2.5 true\n\n"
	const wantStderr = "err one\nerr two 2 2.5 true\n\n"

	goResult := runProgramWithArgs(t, goExecutable, sourceDirectory, nil)
	if goResult.stdout != wantStdout || goResult.stderr != wantStderr || goResult.status != 0 {
		t.Errorf("Go pipeline printed stdout %q (want %q), stderr %q (want %q), exited %d",
			goResult.stdout, wantStdout, goResult.stderr, wantStderr, goResult.status)
	}

	kittenDirectory := t.TempDir()
	kittenResult, err := buildAndRunProgramWithArgs(t, compiler, compileFlags, linkFlags, kittenDirectory, kittenText, 0, nil)
	if err != nil {
		t.Fatalf("Kitten pipeline: %v", err)
	}
	if kittenResult.stdout != wantStdout || kittenResult.stderr != wantStderr || kittenResult.status != 0 {
		t.Errorf("Kitten pipeline printed stdout %q (want %q), stderr %q (want %q), exited %d",
			kittenResult.stdout, wantStdout, kittenResult.stderr, wantStderr, kittenResult.status)
	}
	t.Logf("eprint: stdout and stderr matched separately under %s", filepath.Base(compiler[0]))
}
