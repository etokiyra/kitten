package main

// The two-stage self-hosting fixpoint test.
//
// Stage 1 is built from selfhost/driver.ktn by the Go compiler. Stage 2 is built
// from that source by stage 1 through the wrapper, and stage 3 by stage 2. The
// generated C for driver.ktn must be byte-identical across the Go reference and
// all stages; stage 2 must also reproduce the Go reference for every fixture.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kitten/checker"
	"kitten/codegen"
	"kitten/loader"
)

// referenceCForRoot renders the Go pipeline's C for one absolute root path.
func referenceCForRoot(t *testing.T, root string) string {
	t.Helper()

	modules, err := loader.Load(root)
	if err != nil {
		t.Fatalf("reference loader for %s: %v", root, err)
	}
	checked, checkError := checker.CheckModules(modules)
	if checkError != nil {
		t.Fatalf("reference checker for %s: %v", root, checkError)
	}
	text, generationError := codegen.Generate(modules, checked)
	if generationError != nil {
		t.Fatalf("reference generator for %s: %v", root, generationError)
	}
	return text
}

// stageOutput runs one stage on a root and returns its C.
func stageOutput(t *testing.T, stage, root string) string {
	t.Helper()

	result := runExternal(t, stage, []string{root}, repoPath(t, "."), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("%s: exited %d with stderr %q", stage, result.status, result.stderr)
	}
	return result.stdout
}

// buildStageThroughWrapper builds the next stage with the previous stage.
func buildStageThroughWrapper(t *testing.T, wrapper, driver, driverSource, output string) {
	t.Helper()

	environment := []string{
		"PURR_BOOTSTRAP_DRIVER=" + driver,
		"PURR_RUNTIME_DIR=" + repoPath(t, "..", "..", "runtime", "csrc"),
	}
	result := runExternal(t, wrapper, []string{"build", driverSource, "-o", output}, repoPath(t, "."), environment)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("building %s with %s: exited %d with stderr %q", output, driver, result.status, result.stderr)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("building %s: %v", output, err)
	}
}

// TestKittenFixpoint proves that the Kitten compiler reaches a C-level fixpoint.
func TestKittenFixpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("fixpoint test builds three compiler stages")
	}
	requireToolchain(t)

	wrapper := wrapperPath(t)
	driverSource := repoPath(t, "..", "..", "selfhost", "driver.ktn")
	work := t.TempDir()

	start := time.Now()
	stage1 := buildStage1(t)
	t.Logf("stage 1 built by Go in %s", time.Since(start).Round(time.Millisecond))

	stage2 := filepath.Join(work, "stage2")
	start = time.Now()
	buildStageThroughWrapper(t, wrapper, stage1, driverSource, stage2)
	t.Logf("stage 2 built by stage 1 in %s", time.Since(start).Round(time.Millisecond))

	stage3 := filepath.Join(work, "stage3")
	start = time.Now()
	buildStageThroughWrapper(t, wrapper, stage2, driverSource, stage3)
	t.Logf("stage 3 built by stage 2 in %s", time.Since(start).Round(time.Millisecond))

	start = time.Now()
	goC := referenceCForRoot(t, driverSource)
	stage1C := stageOutput(t, stage1, driverSource)
	stage2C := stageOutput(t, stage2, driverSource)
	stage3C := stageOutput(t, stage3, driverSource)
	t.Logf("four driver translations emitted in %s", time.Since(start).Round(time.Millisecond))

	// (a) Go reference == stage 1.
	if difference := unitDifference(stage1C, goC); difference != "" {
		t.Fatalf("stage 1 driver C differs from Go: %s", difference)
	}
	// (b) stage 1 == stage 2.
	if difference := unitDifference(stage2C, stage1C); difference != "" {
		t.Fatalf("stage 2 driver C differs from stage 1: %s", difference)
	}
	// (c) stage 2 == stage 3; this is the fixpoint.
	if difference := unitDifference(stage3C, stage2C); difference != "" {
		t.Fatalf("stage 3 driver C differs from stage 2: %s", difference)
	}

	// (f) stage 2 is deterministic.
	stage2Again := stageOutput(t, stage2, driverSource)
	if stage2Again != stage2C {
		t.Fatalf("stage 2 produced different C on a repeated run")
	}

	// (g) the comparison is not vacuous: a one-byte mutation is detected.
	mutated := []byte(stage2C)
	if len(mutated) == 0 {
		t.Fatalf("stage 2 produced empty C")
	}
	mutated[len(mutated)/2] ^= 1
	if unitDifference(string(mutated), stage2C) == "" {
		t.Fatalf("the comparison accepted a one-byte mutation")
	}

	// (d) stage 2 reproduces the Go reference for every root, and the stage-2 C
	// compiles, runs, and prints the expected output under the selected CC.
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	type rootCase struct {
		name     string
		root     string
		expected string
	}
	var roots []rootCase
	for _, name := range fixtureNames() {
		roots = append(roots, rootCase{
			name:     name,
			root:     repoPath(t, "..", "..", "tests", name+".ktn"),
			expected: repoPath(t, "..", "..", "tests", name+".expected"),
		})
	}
	roots = append(roots, rootCase{
		name:     "modules",
		root:     repoPath(t, "..", "..", "tests", "modules", "main.ktn"),
		expected: repoPath(t, "..", "..", "tests", "modules", "main.expected"),
	})

	for _, testCase := range roots {
		wantC := referenceCForRoot(t, testCase.root)
		gotC := stageOutput(t, stage2, testCase.root)
		if difference := unitDifference(gotC, wantC); difference != "" {
			t.Errorf("%s: stage 2 C differs from Go: %s", testCase.name, difference)
			continue
		}
		expected, err := os.ReadFile(testCase.expected)
		if err != nil {
			t.Errorf("%s: reading expected output: %v", testCase.name, err)
			continue
		}
		directory := t.TempDir()
		result, err := buildAndRunProgramWithArgs(t, compiler, compileFlags, linkFlags, directory, gotC, 0, nil)
		if err != nil {
			t.Errorf("%s: building stage-2 C: %v", testCase.name, err)
			continue
		}
		if result.status != 0 || result.stderr != "" || result.stdout != string(expected) {
			t.Errorf("%s: stage-2 program exited %d, stderr %q, printed %q; want %q",
				testCase.name, result.status, result.stderr, result.stdout, string(expected))
		}
	}

	// (e) stage 2 diagnostics match the Go CLI for the 5.6 error cases.
	errorDirectory := t.TempDir()
	write := func(name, source string) string {
		path := filepath.Join(errorDirectory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}
	if err := os.WriteFile(filepath.Join(errorDirectory, "bad.ktn"), []byte("fn broken( {\n"), 0o644); err != nil {
		t.Fatalf("writing bad.ktn: %v", err)
	}
	errorPaths := []struct {
		name string
		path string
	}{
		{"type error", write("type.ktn", "fn main() {\n    int x = \"s\";\n}\n")},
		{"syntax error", write("syntax.ktn", "fn main( {\n}\n")},
		{"import syntax error", write("importsyntax.ktn", "import \"bad.ktn\";\nfn main() {\n}\n")},
		{"outside root", write("inside/outside.ktn", "import \"../outside.ktn\";\nfn main() {\n}\n")},
	}
	for _, testCase := range errorPaths {
		var goStdout, goStderr bytes.Buffer
		goStatus := run([]string{"build", testCase.path}, &goStdout, &goStderr)
		stageResult := runExternal(t, stage2, []string{testCase.path}, errorDirectory, nil)
		if stageResult.status != goStatus || stageResult.stderr != goStderr.String() || stageResult.stdout != goStdout.String() {
			t.Errorf("%s: stage 2 differs from the Go CLI\nstatus %d vs %d\nstderr %q\nwant %q",
				testCase.name, stageResult.status, goStatus, stageResult.stderr, goStderr.String())
		}
	}

	// Usage via the wrapper must match the Go CLI with no arguments.
	var goStdout, goStderr bytes.Buffer
	goStatus := run(nil, &goStdout, &goStderr)
	wrapperResult := runExternal(t, wrapper, nil, errorDirectory, []string{
		"PURR_BOOTSTRAP_DRIVER=" + stage2,
		"PURR_RUNTIME_DIR=" + repoPath(t, "..", "..", "runtime", "csrc"),
	})
	if wrapperResult.status != goStatus || wrapperResult.stderr != goStderr.String() || wrapperResult.stdout != goStdout.String() {
		t.Errorf("usage: wrapper staged with stage 2 differs from the Go CLI\nstatus %d vs %d\nstderr %q\nwant %q",
			wrapperResult.status, goStatus, wrapperResult.stderr, goStderr.String())
	}

	// Report, but do not require, binary reproducibility.
	stage1Bytes, err := os.ReadFile(stage1)
	if err != nil {
		t.Fatalf("reading stage 1: %v", err)
	}
	stage2Bytes, err := os.ReadFile(stage2)
	if err != nil {
		t.Fatalf("reading stage 2: %v", err)
	}
	stage3Bytes, err := os.ReadFile(stage3)
	if err != nil {
		t.Fatalf("reading stage 3: %v", err)
	}
	t.Logf("stage binaries: stage1=stage2 %t, stage2=stage3 %t, size %d bytes",
		bytes.Equal(stage1Bytes, stage2Bytes), bytes.Equal(stage2Bytes, stage3Bytes), len(stage2Bytes))
	t.Logf("fixpoint: Go, stage 1, stage 2, and stage 3 C for driver.ktn are byte-identical")
}
