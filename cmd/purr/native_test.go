package main

// Pinned tests for the native backend's typed-IR scene, driven by
// selfhost/native.ktn. The probe builds the Phase 10.1 scene corpus (the
// print-hello skeleton target and a growth scene) plus the Phase 10.3
// arithmetic-and-control-flow scene and its divide-failure sibling, checks
// them with check_scene, and renders them with the text renderer; their
// output and every validation message are pinned literally here. The ELF
// tests execute the produced executables and compare them with the C
// pipeline's behavior for mirrored sources. See selfhost/native/README.md
// for the scene's contract and the spelling rules.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildNativeProbe(t *testing.T) string {
	t.Helper()
	requireToolchain(t)
	return cachedBuild(t, "native-probe", cachedBuildHash(t, "native-probe", []string{
		repoPath(t, "..", "..", "selfhost"),
		repoPath(t, "..", "..", "runtime", "csrc"),
	}), func(executable string) error {
		var diagnostics bytes.Buffer
		if err := buildExecutable(repoPath(t, "..", "..", "selfhost", "native.ktn"), executable, &diagnostics); err != nil {
			return err
		}
		if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
			return fmt.Errorf("C diagnostics while building the native probe:\n%s", warnings)
		}
		return nil
	})
}

// The exact rendering of the skeleton target: two data blobs, a runtime
// print routine that loads the string object's length and pointer and makes
// two write syscalls, and an entry that binds the literal and calls it.
const wantHelloScene = `scene
data $blob0: 5 bytes 104 101 108 108 111
data $blob1: 1 bytes 10
entry 1 @main
fn @print (p0: ref) -> void
  b0: ref binding
  b1: int binding
  b2: ref binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: ref binding
  b7: ref binding
  b8: int binding
  b9: int binding
  b10: int binding
  b1: int = raw_load64 b0 0
  b2: ref = raw_load64 b0 8
  b3: int = const 1
  b4: int = const 1
  b5: int = raw_syscall syscall b4 b3 b2 b1
  b6: ref = data $blob1
  b7: ref = raw_load64 b6 8
  b8: int = const 1
  b9: int = const 1
  b10: int = raw_syscall syscall b4 b8 b7 b9
  return
fn @main () -> void
  b0: ref binding
  b0: ref = data $blob0
  call @0 b0
  return

`

// The exact rendering of the Phase 10.3 arithmetic scene: the shared print
// routine, a step helper whose body multiplies and subtracts, and a main
// that loops a call through defined divide, remainder, and add, then
// compares twice and prints one of three literals.
const wantArithScene = `scene
data $blob0: 1 bytes 10
data $blob1: 5 bytes 108 97 114 103 101
data $blob2: 5 bytes 115 109 97 108 108
data $blob3: 6 bytes 110 101 103 103 101 100
entry 2 @main
fn @print (p0: ref) -> void
  b0: ref binding
  b1: int binding
  b2: ref binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: ref binding
  b7: ref binding
  b8: int binding
  b9: int binding
  b10: int binding
  b1: int = raw_load64 b0 0
  b2: ref = raw_load64 b0 8
  b3: int = const 1
  b4: int = const 1
  b5: int = raw_syscall syscall b4 b3 b2 b1
  b6: ref = data $blob0
  b7: ref = raw_load64 b6 8
  b8: int = const 1
  b9: int = const 1
  b10: int = raw_syscall syscall b4 b8 b7 b9
  return
fn @step (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b1: int = const 3
  b2: int = multiply b0 b1
  b3: int = const 1
  b4: int = subtract b2 b3
  return b4
fn @main () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: bool binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: bool binding
  b10: ref binding
  b11: ref binding
  b0: int = const 47
  b1: int = const 0
  b2: int = const 5
  b3: int = const 1
  label %0
  b4: bool = less_than b1 b2
  branch b4 %1 %2
  label %1
  b5: int = call @1 b0
  b6: int = whole_divide b5 b2
  b7: int = whole_remainder b0 b2
  b0: int = add b6 b7
  b1: int = add b1 b3
  jump %0
  label %2
  b8: int = const 19
  b9: bool = less_than b8 b0
  branch b9 %3 %4
  label %3
  b10: ref = data $blob1
  call @0 b10
  jump %5
  label %4
  b11: ref = data $blob2
  call @0 b11
  label %5
  b8: int = negate b3
  b5: int = whole_divide b0 b8
  b6: int = whole_remainder b0 b8
  b9: bool = less_than b5 b6
  branch b9 %6 %7
  label %6
  b10: ref = data $blob3
  call @0 b10
  label %7
  return

`

func TestNativeProbeHelloScene(t *testing.T) {
	executable := buildNativeProbe(t)
	result := runExternal(t, executable, nil, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("hello scene: status %d, stderr %q", result.status, result.stderr)
	}
	if result.stdout != wantHelloScene {
		t.Fatalf("hello scene rendering differs:\n--- got ---\n%s--- want ---\n%s", result.stdout, wantHelloScene)
	}
}

// The growth scene must exercise every table-doubling path: twelve functions
// (more than the first function allocation), nine blobs (more than the first
// blob allocation), and seventeen bindings per function (more than the first
// binding allocation). The structural counts here are pinned next to spot
// lines, because the whole 444-line rendering is mechanical.
func TestNativeProbeBigScene(t *testing.T) {
	executable := buildNativeProbe(t)
	result := runExternal(t, executable, []string{"big"}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("big scene: status %d, stderr %q", result.status, result.stderr)
	}
	lines := strings.Split(strings.TrimRight(result.stdout, "\n"), "\n")

	// One scene header, nine data blob lines, one entry line, and twelve
	// functions of a signature line, seventeen binding lines, and eighteen
	// instruction lines each: 1 + 9 + 1 + 12*36.
	if len(lines) != 1+9+1+12*(1+17+18) {
		t.Fatalf("big scene has %d lines", len(lines))
	}
	if lines[0] != "scene" {
		t.Fatalf("big scene does not open with scene: %q", lines[0])
	}
	for _, want := range []string{
		"data $blob0: 1 bytes 97",
		"data $blob8: 1 bytes 97",
		"entry 0 @f0",
		"fn @f0 () -> int",
		"  b16: int = const 16",
		"  return b16",
		"fn @f11 () -> int",
		"  b16: int = const 203",
		"  return b16",
	} {
		if !strings.Contains(result.stdout, want+"\n") {
			t.Fatalf("big scene misses %q", want)
		}
	}
	for _, unexpected := range []string{
		"data $blob9",
		"fn @f12",
	} {
		if strings.Contains(result.stdout, unexpected+"\n") {
			t.Fatalf("big scene unexpectedly contains %q", unexpected)
		}
	}
}

func TestNativeProbeFailures(t *testing.T) {
	executable := buildNativeProbe(t)
	cases := []struct {
		mode string
		want string
	}{
		{"no-label", "error function @main: instruction 0: label 0 is out of range (the function defines 0 labels)\n"},
		{"bad-binding", "error function @main: instruction 0: target binding 0 is out of range (the function has 0 bindings)\n"},
		{"bad-call-count", "error function @main: instruction 0: the call passes 0 arguments, but @print takes 1\n"},
		{"bad-call-shape", "error function @main: instruction 0: argument 0 binding 0 has bool, expected ref\n"},
		{"bad-entry", "error entry 5 is out of range (the scene has 1 functions)\n"},
		{"undefined-label", "error function @main: instruction 1: true label 1 is out of range (the function defines 1 labels)\n"},
		// An unknown mode falls through to the empty scene, whose entry
		// reference is the first failure the checker reports.
		{"wat", "error entry 0 is out of range (the scene has 0 functions)\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.mode, func(t *testing.T) {
			result := runExternal(t, executable, []string{testCase.mode}, t.TempDir(), nil)
			if result.status != 1 {
				t.Fatalf("status %d, want 1", result.status)
			}
			if result.stdout != testCase.want {
				t.Fatalf("stdout differs:\n--- got ---\n%s--- want ---\n%s", result.stdout, testCase.want)
			}
			if result.stderr != "" {
				t.Fatalf("stderr %q, want empty", result.stderr)
			}
		})
	}
}

func TestNativeProbeTooManyArguments(t *testing.T) {
	executable := buildNativeProbe(t)
	result := runExternal(t, executable, []string{"one", "two", "three"}, t.TempDir(), nil)
	if result.status != 2 {
		t.Fatalf("status %d, want 2", result.status)
	}
	if result.stderr != "native: expected at most two arguments\n" {
		t.Fatalf("stderr %q", result.stderr)
	}
	if result.stdout != "" {
		t.Fatalf("stdout %q, want empty", result.stdout)
	}

	unknown := runExternal(t, executable, []string{"wat", "out"}, t.TempDir(), nil)
	if unknown.status != 2 {
		t.Fatalf("unknown status %d, want 2", unknown.status)
	}
	if unknown.stderr != "native: unknown mode wat\n" {
		t.Fatalf("unknown stderr %q", unknown.stderr)
	}
}

// TestNativeProbeSkeleton writes the skeleton target's native ELF with no C
// compiler, then executes it: the output and status must match the C
// pipeline's program for the same source, the ELF must be static (no
// interpreter, no dynamic section), and repeated productions must be
// byte-identical.
func TestNativeProbeSkeleton(t *testing.T) {
	requireToolchain(t)
	executable := buildNativeProbe(t)
	directory := t.TempDir()

	native := filepath.Join(directory, "hello-native")
	written := runExternal(t, executable, []string{"elf", native}, directory, nil)
	if written.status != 0 || written.stderr != "" || written.stdout != "built "+native+"\n" {
		t.Fatalf("skeleton write: status %d, stdout %q, stderr %q", written.status, written.stdout, written.stderr)
	}
	if err := os.Chmod(native, 0o755); err != nil {
		t.Fatalf("chmod native: %v", err)
	}

	run := runExternal(t, native, nil, directory, []string{"PATH=/nonexistent"})
	if run.status != 0 || run.stderr != "" || run.stdout != "hello\n" {
		t.Fatalf("native run: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
	}

	// A second production is byte-identical: the selector, the layout, and
	// the ELF writer are deterministic.
	again := filepath.Join(directory, "hello-native-again")
	if written := runExternal(t, executable, []string{"elf", again}, directory, nil); written.status != 0 {
		t.Fatalf("second write status %d", written.status)
	}
	first, err := os.ReadFile(native)
	if err != nil {
		t.Fatalf("reading the first ELF: %v", err)
	}
	secondFile, err := os.ReadFile(again)
	if err != nil {
		t.Fatalf("reading the second ELF: %v", err)
	}
	if string(first) != string(secondFile) {
		t.Fatalf("two native productions differ in %d bytes", len(first))
	}

	// The ELF is static: no interpreter, no dynamic section, no needed
	// library, and two PT_LOAD program headers.
	if readelf, err := exec.LookPath("readelf"); err == nil {
		listing := exec.Command(readelf, "-ld", native)
		var out bytes.Buffer
		listing.Stdout = &out
		listing.Stderr = &out
		_ = listing.Run()
		text := out.String()
		if strings.Contains(text, "INTERP") || strings.Contains(text, "NEEDED") || strings.Contains(text, "Dynamic section") {
			t.Fatalf("native ELF is not static:\n%s", text)
		}
		if !strings.Contains(text, "There are 2 program headers") {
			t.Fatalf("native ELF's program headers got not read back:\n%s", text)
		}
	}

	// The C pipeline's program for the same source behaves identically.
	cSource := filepath.Join(directory, "hello-c.ktn")
	if err := os.WriteFile(cSource, []byte("fn main() {\n    print(\"hello\");\n}\n"), 0o644); err != nil {
		t.Fatalf("writing the C-parity source: %v", err)
	}
	cRun := runExternal(t, goPurrReference(t), []string{"run", cSource}, directory, nil)
	if cRun.status != 0 || cRun.stdout != "hello\n" || cRun.stderr != "" {
		t.Fatalf("C reference run: status %d, stdout %q, stderr %q", cRun.status, cRun.stdout, cRun.stderr)
	}
}

// TestNativeProbeArithScene pins the arithmetic scene's rendering.
func TestNativeProbeArithScene(t *testing.T) {
	executable := buildNativeProbe(t)
	result := runExternal(t, executable, []string{"arith"}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("arith scene: status %d, stderr %q", result.status, result.stderr)
	}
	if result.stdout != wantArithScene {
		t.Fatalf("arith scene rendering differs:\n--- got ---\n%s--- want ---\n%s", result.stdout, wantArithScene)
	}
}

// TestNativeProbeArith writes the arithmetic-and-control-flow executable
// with no C compiler, runs it with an empty PATH, and checks its output
// against the C pipeline's build of the mirrored source; it also confirms a
// repeated production is byte-identical.
func TestNativeProbeArith(t *testing.T) {
	requireToolchain(t)
	executable := buildNativeProbe(t)
	directory := t.TempDir()

	native := filepath.Join(directory, "arith-native")
	written := runExternal(t, executable, []string{"elf-arith", native}, directory, nil)
	if written.status != 0 || written.stderr != "" || written.stdout != "built "+native+"\n" {
		t.Fatalf("arith write: status %d, stdout %q, stderr %q", written.status, written.stdout, written.stderr)
	}
	if err := os.Chmod(native, 0o755); err != nil {
		t.Fatalf("chmod native: %v", err)
	}

	run := runExternal(t, native, nil, directory, []string{"PATH=/nonexistent"})
	if run.status != 0 || run.stderr != "" || run.stdout != "small\nnegged\n" {
		t.Fatalf("native arith run: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
	}

	again := filepath.Join(directory, "arith-native-again")
	if written := runExternal(t, executable, []string{"elf-arith", again}, directory, nil); written.status != 0 {
		t.Fatalf("second write status %d", written.status)
	}
	first, err := os.ReadFile(native)
	if err != nil {
		t.Fatalf("reading the first ELF: %v", err)
	}
	secondFile, err := os.ReadFile(again)
	if err != nil {
		t.Fatalf("reading the second ELF: %v", err)
	}
	if string(first) != string(secondFile) {
		t.Fatalf("two arith productions differ in %d bytes", len(first))
	}

	cSource := filepath.Join(directory, "arith-c.ktn")
	source := `fn step(int value) -> int {
    return value * 3 - 1;
}
fn main() {
    int total = 47;
    int round = 0;
    while (round < 5) {
        total = step(total) / 5 + total % 5;
        round = round + 1;
    }
    if (total > 19) {
        print("large");
    } else {
        print("small");
    }
    int under = -1;
    int flipped = total / under;
    int remains = total % under;
    if (flipped < remains) {
        print("negged");
    }
}
`
	if err := os.WriteFile(cSource, []byte(source), 0o644); err != nil {
		t.Fatalf("writing the C-parity source: %v", err)
	}
	cRun := runExternal(t, goPurrReference(t), []string{"run", cSource}, directory, nil)
	if cRun.status != 0 || cRun.stdout != "small\nnegged\n" || cRun.stderr != "" {
		t.Fatalf("C reference arith run: status %d, stdout %q, stderr %q", cRun.status, cRun.stdout, cRun.stderr)
	}
}

// TestNativeProbeDivzero writes the divide-failure executable with no C
// compiler and runs it: zero divisors must halt with the C runtime's exact
// text, standard error only, and status 1, matching the C pipeline's build
// of the mirrored source.
func TestNativeProbeDivzero(t *testing.T) {
	requireToolchain(t)
	executable := buildNativeProbe(t)
	directory := t.TempDir()

	native := filepath.Join(directory, "divzero-native")
	written := runExternal(t, executable, []string{"elf-divzero", native}, directory, nil)
	if written.status != 0 || written.stderr != "" || written.stdout != "built "+native+"\n" {
		t.Fatalf("divzero write: status %d, stdout %q, stderr %q", written.status, written.stdout, written.stderr)
	}
	if err := os.Chmod(native, 0o755); err != nil {
		t.Fatalf("chmod native: %v", err)
	}

	run := runExternal(t, native, nil, directory, []string{"PATH=/nonexistent"})
	if run.status != 1 || run.stderr != "purr: integer division by zero\n" || run.stdout != "" {
		t.Fatalf("native divzero run: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
	}

	cSource := filepath.Join(directory, "divzero-c.ktn")
	source := "fn main() {\n    int zero = 0;\n    print(5 / zero);\n}\n"
	if err := os.WriteFile(cSource, []byte(source), 0o644); err != nil {
		t.Fatalf("writing the C-parity source: %v", err)
	}
	cRun := runExternal(t, goPurrReference(t), []string{"run", cSource}, directory, nil)
	if cRun.status != 1 || cRun.stdout != "" || cRun.stderr != "purr: integer division by zero\n" {
		t.Fatalf("C reference divzero run: status %d, stdout %q, stderr %q", cRun.status, cRun.stdout, cRun.stderr)
	}
}

// goPurrReferenceExecutive building the test reference each time is wasteful;
// this helper builds the reference once per test.
func goPurrReference(t *testing.T) string {
	t.Helper()
	return buildGoPurr(t)
}

// TestNativeProbeKittenPipeline builds the probe with the Kitten-written
// purr-kt sidecar, so the typed-IR module also compiles and behaves through
// the Kitten pipeline, and compares every probe output against the pinned
// texts the Go pipeline produced.
func TestNativeProbeKittenPipeline(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	kittenPurr := buildPurrKTWithKitten(t, stage1)
	executable := filepath.Join(t.TempDir(), "native-probe-kt")
	result := runPurrKT(t, kittenPurr, t.TempDir(), []string{
		"build", repoPath(t, "..", "..", "selfhost", "native.ktn"), "-o", executable,
	})
	if result.status != 0 || result.stderr != "" || result.stdout != "built "+executable+"\n" {
		t.Fatalf("Kitten build of the native probe: status %d, stdout %q, stderr %q",
			result.status, result.stdout, result.stderr)
	}

	goProbe := buildNativeProbe(t)
	cases := []struct {
		mode string
		want string
	}{
		{"", wantHelloScene},
		{"big", "scene\n"},
		{"arith", wantArithScene},
		{"simple", "scene\n"},
		{"no-label", "error function @main: instruction 0: label 0 is out of range (the function defines 0 labels)\n"},
		{"bad-binding", "error function @main: instruction 0: target binding 0 is out of range (the function has 0 bindings)\n"},
		{"bad-call-count", "error function @main: instruction 0: the call passes 0 arguments, but @print takes 1\n"},
		{"bad-call-shape", "error function @main: instruction 0: argument 0 binding 0 has bool, expected ref\n"},
		{"bad-entry", "error entry 5 is out of range (the scene has 1 functions)\n"},
		{"undefined-label", "error function @main: instruction 1: true label 1 is out of range (the function defines 1 labels)\n"},
		{"wat", "error entry 0 is out of range (the scene has 0 functions)\n"},
	}
	for _, testCase := range cases {
		arguments := []string{}
		if testCase.mode != "" {
			arguments = append(arguments, testCase.mode)
		}
		goResult := runExternal(t, goProbe, arguments, t.TempDir(), nil)
		ktResult := runExternal(t, executable, arguments, t.TempDir(), nil)
		if ktResult.status != goResult.status ||
			ktResult.stdout != goResult.stdout ||
			ktResult.stderr != goResult.stderr {
			t.Errorf("mode %q: Kitten %d/%q/%q, Go %d/%q/%q",
				testCase.mode, ktResult.status, ktResult.stdout, ktResult.stderr,
				goResult.status, goResult.stdout, goResult.stderr)
		}
	}
}

// TestNativeProbeKittenPipelineSkeleton builds the probe with the
// Kitten-written sidecar as well, and the ELFs both pipelines produce must
// be byte-identical for every scene kind: the same selector and ELF writer
// run through the Kitten backend. The hello executable's behavior is
// asserted directly; arith and divzero already are in their own tests.
func TestNativeProbeKittenPipelineSkeleton(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	kittenPurr := buildPurrKTWithKitten(t, stage1)
	ktExecutable := filepath.Join(t.TempDir(), "native-probe-kt")
	result := runPurrKT(t, kittenPurr, t.TempDir(), []string{
		"build", repoPath(t, "..", "..", "selfhost", "native.ktn"), "-o", ktExecutable,
	})
	if result.status != 0 || result.stderr != "" || result.stdout != "built "+ktExecutable+"\n" {
		t.Fatalf("Kitten build of the native probe: status %d, stdout %q, stderr %q",
			result.status, result.stdout, result.stderr)
	}

	goExecutable := buildNativeProbe(t)
	directory := t.TempDir()

	// One ELF per scene kind: unadorned "elf" lowers hello, and the two
	// Phase 10.3 scene names lower their own executable.
	sceneKinds := []struct {
		mode string
		name string
	}{
		{"elf", "hello"},
		{"elf-arith", "arith"},
		{"elf-divzero", "divzero"},
	}
	for _, kind := range sceneKinds {
		goNative := filepath.Join(directory, kind.name+"-go")
		if written := runExternal(t, goExecutable, []string{kind.mode, goNative}, directory, nil); written.status != 0 {
			t.Fatalf("Go pipeline %s elf: status %d, stderr %q", kind.name, written.status, written.stderr)
		}
		ktNative := filepath.Join(directory, kind.name+"-kt")
		if written := runExternal(t, ktExecutable, []string{kind.mode, ktNative}, directory, nil); written.status != 0 {
			t.Fatalf("Kitten pipeline %s elf: status %d, stderr %q", kind.name, written.status, written.stderr)
		}
		goBytes, err := os.ReadFile(goNative)
		if err != nil {
			t.Fatalf("reading the Go-produced %s ELF: %v", kind.name, err)
		}
		ktBytes, err := os.ReadFile(ktNative)
		if err != nil {
			t.Fatalf("reading the Kitten-produced %s ELF: %v", kind.name, err)
		}
		if string(goBytes) != string(ktBytes) {
			t.Fatalf("the two pipelines' %s ELFs differ in %d bytes", kind.name, len(goBytes))
		}
		// The hello executable's behavior is asserted directly; arith and
		// divzero already are in their own tests.
		if kind.name == "hello" {
			if err := os.Chmod(ktNative, 0o755); err != nil {
				t.Fatalf("chmod the Kitten-produced ELF: %v", err)
			}
			run := runExternal(t, ktNative, nil, directory, nil)
			if run.status != 0 || run.stdout != "hello\n" || run.stderr != "" {
				t.Fatalf("Kitten-produced native run: %d %q %q", run.status, run.stdout, run.stderr)
			}
		}
	}
}
