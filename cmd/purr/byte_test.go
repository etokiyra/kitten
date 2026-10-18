package main

import (
	"path/filepath"
	"testing"
)

const byteEprintSource = `fn main() {
    eprint(byte_from_int(65), byte_from_int(255));
}
`

const byteNulSource = `fn main() {
    print("before");
    byte[] values = new byte[3];
    values[0] = byte_from_int(65);
    values[1] = byte_from_int(0);
    values[2] = byte_from_int(66);
    print(bytes_to_string(values));
}
`

const byteIndexSource = `fn main() {
    print("before");
    byte[] values = new byte[1];
    print(int_from_byte(values[1]));
}
`

// TestByteRuntime compares both pipelines for byte values, byte arrays,
// conversions, printing, and the defined runtime failures.
func TestByteRuntime(t *testing.T) {
	requireToolchain(t)

	unitExecutable := buildKittenUnit(t)
	probeDirectory := t.TempDir()
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	cases := []struct {
		name   string
		source string
		stdout string
		stderr string
		status int
	}{
		{
			name:   "byte values arrays and conversions",
			source: byteSource,
			stdout: byteExpected,
		},
		{
			name:   "eprint byte values",
			source: byteEprintSource,
			stderr: "65 255\n",
		},
		{
			name:   "bytes_to_string rejects NUL",
			source: byteNulSource,
			stdout: "before\n",
			stderr: "purr: byte array contains a NUL byte at index 1\n",
			status: 1,
		},
		{
			name:   "byte array bounds failure",
			source: byteIndexSource,
			stdout: "before\n",
			stderr: "purr: array index 1 is out of range for length 1\n",
			status: 1,
		},
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			buildDirectory := t.TempDir()
			goExecutable := buildDefinedArithmeticProgram(t, buildDirectory, "go-byte-program", testCase.source)
			goResult := runExternal(t, goExecutable, nil, buildDirectory, nil)
			if goResult.stdout != testCase.stdout || goResult.stderr != testCase.stderr || goResult.status != testCase.status {
				t.Errorf("Go pipeline: stdout %q, stderr %q, status %d; want %q, %q, %d",
					goResult.stdout, goResult.stderr, goResult.status, testCase.stdout, testCase.stderr, testCase.status)
			}

			kittenText, err := runKittenUnit(unitExecutable, probeDirectory, testCase.source, "")
			if err != nil {
				t.Fatalf("%s: generating C with the Kitten pipeline: %v", testCase.name, err)
			}
			_, _, referenceText, ok := checkProgram(t, testCase.name, testCase.source, "")
			if !ok {
				return
			}
			if difference := unitDifference(kittenText, referenceText); difference != "" {
				t.Fatalf("%s: the translation unit differs: %s", testCase.name, difference)
			}

			kittenBuildDirectory := t.TempDir()
			kittenResult, err := buildAndRunProgram(t, compiler, compileFlags, linkFlags, kittenBuildDirectory, kittenText, index)
			if err != nil {
				t.Fatalf("%s: building and running the Kitten-generated program: %v", testCase.name, err)
			}
			if kittenResult.stdout != testCase.stdout || kittenResult.stderr != testCase.stderr || kittenResult.status != testCase.status {
				t.Errorf("Kitten pipeline: stdout %q, stderr %q, status %d; want %q, %q, %d",
					kittenResult.stdout, kittenResult.stderr, kittenResult.status, testCase.stdout, testCase.stderr, testCase.status)
			}
		})
	}
}

// TestByteSanitizer compiles the byte fixture with UBSan where the selected
// compiler can link the sanitizer runtime.
func TestByteSanitizer(t *testing.T) {
	requireToolchain(t)

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")

	text, err := runKittenUnit(buildKittenUnit(t), t.TempDir(), byteSource, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	directory := t.TempDir()
	result, err := buildAndRunProgram(t, compiler, compileFlags, linkFlags, directory, text, 0)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	if result.stderr != "" || result.status != 0 {
		t.Errorf("the sanitizer reported a problem: stderr %q, status %d", result.stderr, result.status)
	}
	if result.stdout != byteExpected {
		t.Errorf("stdout %q, want %q", result.stdout, byteExpected)
	}
}
