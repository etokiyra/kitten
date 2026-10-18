package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const divisionByZeroSource = `fn main() {
    print("before");
    int zero = 0;
    int value = 1 / zero;
    print(value);
}
`

const missingMapEntrySource = `fn main() {
    print("before");
    map[string]int counts = new map[string]int;
    counts["missing"] += 1;
}
`

const negativeShiftSource = `fn main() {
    print("before");
    int value = 1;
    print(value << -1);
}
`

const largeShiftSource = `fn main() {
    print("before");
    int value = 1;
    print(value >> 64);
}
`

// buildDefinedArithmeticProgram builds one source through the Go pipeline.
func buildDefinedArithmeticProgram(t *testing.T, directory, name, source string) string {
	t.Helper()

	sourcePath := filepath.Join(directory, name+".ktn")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("writing %s: %v", sourcePath, err)
	}
	executable := filepath.Join(directory, name)
	var diagnostics strings.Builder
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building %s: %v\n%s", name, err, diagnostics.String())
	}
	if output := strings.TrimSpace(diagnostics.String()); output != "" {
		t.Fatalf("C compiler diagnostics while building %s:\n%s", name, output)
	}
	return executable
}

// TestDefinedArithmeticRuntime compares both pipelines for defined wrapping,
// division, remainder, compound assignment, and failure text/status.
func TestDefinedArithmeticRuntime(t *testing.T) {
	requireToolchain(t)

	unitExecutable := buildKittenUnit(t)
	probeDirectory := t.TempDir()
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	cases := []struct {
		name     string
		source   string
		expected string
		stderr   string
		status   int
	}{
		{
			name:     "wrapping and defined division",
			source:   definedArithmeticSource,
			expected: "-9223372036854775808\n9223372036854775807\n-2\n-9223372036854775808\n-9223372036854775808\n0\n9223372036854775807 -9223372036854775808 -9223372036854775808\n2 6 1\n3\n3\n",
		},
		{
			name:     "division by zero",
			source:   divisionByZeroSource,
			expected: "before\n",
			stderr:   "purr: integer division by zero\n",
			status:   1,
		},
		{
			name:     "missing map entry on compound assignment",
			source:   missingMapEntrySource,
			expected: "before\n",
			stderr:   "purr: map has no entry for key \"missing\"\n",
			status:   1,
		},
		{
			name:     "bitwise and shift operators",
			source:   bitwiseSource,
			expected: bitwiseExpected,
		},
		{
			name:     "negative shift count",
			source:   negativeShiftSource,
			expected: "before\n",
			stderr:   "purr: shift count -1 is outside 0..63\n",
			status:   1,
		},
		{
			name:     "shift count 64",
			source:   largeShiftSource,
			expected: "before\n",
			stderr:   "purr: shift count 64 is outside 0..63\n",
			status:   1,
		},
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			buildDirectory := t.TempDir()
			goExecutable := buildDefinedArithmeticProgram(t, buildDirectory, "go-program", testCase.source)
			goResult := runExternal(t, goExecutable, nil, buildDirectory, nil)
			if goResult.stdout != testCase.expected || goResult.stderr != testCase.stderr || goResult.status != testCase.status {
				t.Errorf("Go pipeline: stdout %q, stderr %q, status %d; want %q, %q, %d",
					goResult.stdout, goResult.stderr, goResult.status, testCase.expected, testCase.stderr, testCase.status)
			}

			kittenText, err := runKittenUnit(unitExecutable, probeDirectory, testCase.source, "")
			if err != nil {
				t.Fatalf("generating C with the Kitten pipeline: %v", err)
			}
			_, _, referenceText, ok := checkProgram(t, testCase.name, testCase.source, "")
			if !ok {
				return
			}
			if difference := unitDifference(kittenText, referenceText); difference != "" {
				t.Fatalf("the %s translation unit differs: %s", testCase.name, difference)
			}

			kittenBuildDirectory := t.TempDir()
			kittenResult, err := buildAndRunProgram(t, compiler, compileFlags, linkFlags, kittenBuildDirectory, kittenText, index)
			if err != nil {
				t.Fatalf("building and running the Kitten-generated program: %v", err)
			}
			if kittenResult.stdout != testCase.expected || kittenResult.stderr != testCase.stderr || kittenResult.status != testCase.status {
				t.Errorf("Kitten pipeline: stdout %q, stderr %q, status %d; want %q, %q, %d",
					kittenResult.stdout, kittenResult.stderr, kittenResult.status, testCase.expected, testCase.stderr, testCase.status)
			}
		})
	}
}

// TestDefinedArithmeticSanitizer compiles and runs the wrapping fixture with
// UndefinedBehaviorSanitizer. The test skips only when the selected compiler
// cannot link the sanitizer runtime.
func TestDefinedArithmeticSanitizer(t *testing.T) {
	requireToolchain(t)

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")

	unitExecutable := buildKittenUnit(t)
	probeDirectory := t.TempDir()
	for index, testCase := range []struct {
		name   string
		source string
	}{
		{"defined integer arithmetic", definedArithmeticSource},
		{"bitwise and shift operators", bitwiseSource},
	} {
		text, err := runKittenUnit(unitExecutable, probeDirectory, testCase.source, "")
		if err != nil {
			t.Fatalf("%s: generating C with the Kitten pipeline: %v", testCase.name, err)
		}
		directory := t.TempDir()
		result, err := buildAndRunProgram(t, compiler, compileFlags, linkFlags, directory, text, index)
		if err != nil {
			t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
		}
		if result.stderr != "" || result.status != 0 {
			t.Errorf("%s: the sanitizer reported a problem: stderr %q, status %d", testCase.name, result.stderr, result.status)
		}
	}
}
