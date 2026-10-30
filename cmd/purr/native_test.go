package main

// Pinned tests for the native backend's typed-IR scene, driven by
// selfhost/native.ktn. The probe builds the Phase 10.1 scene corpus (the
// print-hello skeleton target and a growth scene) plus the Phase 10.3
// arithmetic-and-control-flow scene and its divide-failure sibling, checks
// them with check_scene, and renders them with the text renderer; their
// output and every validation message are pinned literally here. Since Phase
// 10.4 the probe also lowers real sources: the `lower` mode loads and checks
// a program with the Kitten-written loader and checker and reduces its
// scalar subset into a scene, whose rendering and executable the tests pin
// and compare with the C pipeline's behavior for the same source. The ELF
// tests execute the produced executables and compare them with the C
// pipeline's behavior for mirrored sources. See selfhost/native/README.md
// for the scene's contract and the spelling rules.

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
fn @$print (p0: ref) -> void
  b0: ref binding
  b1: int binding
  b2: ref binding
  b3: int binding
  b4: int binding
  b5: ref binding
  b6: ref binding
  b7: int binding
  b8: int binding
  b1: int = raw_load64 b0 8
  b2: ref = raw_load64 b0 16
  b3: int = const 1
  b4: int = raw_syscall syscall b3 b3 b2 b1
  b5: ref = data $blob1
  b6: ref = raw_load64 b5 16
  b7: int = const 1
  b8: int = raw_syscall syscall b7 b7 b6 b7
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
fn @$print (p0: ref) -> void
  b0: ref binding
  b1: int binding
  b2: ref binding
  b3: int binding
  b4: int binding
  b5: ref binding
  b6: ref binding
  b7: int binding
  b8: int binding
  b1: int = raw_load64 b0 8
  b2: ref = raw_load64 b0 16
  b3: int = const 1
  b4: int = raw_syscall syscall b3 b3 b2 b1
  b5: ref = data $blob0
  b6: ref = raw_load64 b5 16
  b7: int = const 1
  b8: int = raw_syscall syscall b7 b7 b6 b7
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
		{"bad-static-blob", "error function @main: instruction 0: blob 9 is out of range (the scene has 1 blobs)\n"},
		{"bad-static-offset", "error function @main: instruction 0: offset 60 does not fit blob $blob0's 32 bytes\n"},
		{"bad-static-value", "error function @main: instruction 0: value binding 0 has bool, expected int\n"},
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
	result := runExternal(t, executable, []string{"one", "two", "three", "four"}, t.TempDir(), nil)
	if result.status != 2 {
		t.Fatalf("status %d, want 2", result.status)
	}
	if result.stderr != "native: expected at most three arguments\n" {
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

// The Phase 10.4 pool render, pinned: equal literals share one blob, the
// helper's newline blob comes first, and the helper is named "$print" so no
// user name can collide with it.
const wantPoolScene = `scene
data $blob0: 48 bytes 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255
data $blob1: 16 bytes 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255
data $blob2: 1 bytes 10
data $blob3: 4 bytes 115 97 109 101
data $blob4: 5 bytes 111 116 104 101 114
entry 0 @main
fn @main () -> void
  b0: ref binding
  b1: ref binding
  b2: ref binding
  b3: ref binding
  b4: ref binding
  b5: ref binding
  b1: ref = data $blob3
  b0: ref = copy b1
  b3: ref = data $blob3
  b2: ref = copy b3
  b5: ref = data $blob4
  b4: ref = copy b5
  call @19 b0
  call @19 b2
  call @19 b4
fn @$mmap (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b1: int = const 0
  b2: int = const 3
  b3: int = const 34
  b4: int = const 9
  b5: int = raw_syscall syscall b4 b1 b0 b2 b3
  b6: bool = less_than b5 b1
  branch b6 %0 %1
  label %0
  fail_oom
  label %1
  return b5
fn @$map () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b0: int = const 268435456
  b1: int = const 33554440
  b2: int = call @1 b0
  b3: int = call @1 b1
  b4: int = stack_pointer
  b5: int = const 
  b6: int = add b2 b0
  set_static $blob0 24 b2
  set_static $blob0 32 b2
  set_static $blob0 40 b6
  set_static $blob0 48 b5
  set_static $blob0 56 b5
  set_static $blob0 64 b5
  set_static $blob1 24 b3
  set_static $blob1 32 b4
  return
fn @$pad8 (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: bool binding
  b1: int = const 15
  b2: int = const 8
  b3: int = const 16
  b4: int = add b0 b1
  b5: int = whole_divide b4 b2
  b6: int = multiply b5 b2
  b7: bool = less_than b6 b3
  branch b7 %0 %1
  label %0
  b6: int = copy b3
  label %1
  return b6
fn @$byte_set1 (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = const 1
  raw_store8 b6 0 b7
  return
fn @$byte_set0 (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = const 0
  raw_store8 b6 0 b7
  return
fn @$search (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: bool binding
  b9: int binding
  b10: bool binding
  b11: int binding
  b12: bool binding
  b13: int binding
  b14: int binding
  b1: int = const 0
  b2: int = get_static $blob0 48
  b3: int = copy b1
  b4: int = copy b2
  b5: int = const 16
  label %0
  b6: bool = less_than b4 b1
  branch b6 %9 %1
  label %1
  b7: int = raw_load64 b4 0
  b8: bool = less_than b7 b0
  branch b8 %2 %3
  label %2
  b3: int = copy b4
  b4: int = raw_load64 b4 8
  jump %0
  label %3
  b9: int = raw_load64 b4 8
  b10: bool = equal b3 b1
  branch b10 %4 %5
  label %4
  set_static $blob0 48 b9
  jump %6
  label %5
  raw_store64 b3 8 b9
  label %6
  b11: int = subtract b7 b0
  b12: bool = less_than b11 b5
  branch b12 %8 %7
  label %7
  b13: int = add b4 b0
  raw_store64 b13 0 b11
  b14: int = get_static $blob0 48
  raw_store64 b13 8 b14
  set_static $blob0 48 b13
  jump %8
  label %8
  raw_store64 b4 0 b0
  call @4 b4
  return b4
  label %9
  return b1
fn @$bump (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b1: int = const 0
  b2: int = get_static $blob0 32
  b3: int = get_static $blob0 40
  b4: int = add b2 b0
  b5: bool = less_than b3 b4
  branch b5 %0 %1
  label %1
  set_static $blob0 32 b4
  raw_store64 b2 0 b0
  call @4 b2
  return b2
  label %0
  return b1
fn @$mark_at (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: bool binding
  b3: int binding
  b4: bool binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: bool binding
  b13: int binding
  b1: int = get_static $blob0 24
  b2: bool = less_than b0 b1
  branch b2 %0 %1
  label %1
  b3: int = get_static $blob0 40
  b4: bool = less_than b3 b0
  branch b4 %0 %2
  label %2
  b5: int = const 8
  b6: int = subtract b0 b1
  b7: int = whole_divide b6 b5
  b8: int = get_static $blob1 24
  b9: int = add b8 b7
  b10: int = raw_load8 b9 0
  b11: int = const 1
  b12: bool = equal b10 b11
  branch b12 %3 %0
  label %3
  b13: int = const 2
  raw_store8 b9 0 b13
  label %0
  return
fn @$scan_roots () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b0: int = get_static $blob1 32
  b1: int = stack_pointer
  b2: int = const 7
  b3: int = const 8
  b4: int = subtract b1 b2
  b4: int = whole_divide b4 b3
  b4: int = multiply b4 b3
  label %0
  b5: bool = less_than b4 b0
  branch b5 %1 %2
  label %1
  b6: int = raw_load64 b4 0
  call @8 b6
  b4: int = add b4 b3
  jump %0
  label %2
  return
fn @$free_block (p0: int, p1: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b2: int = const 0
  b3: int = get_static $blob0 48
  b4: int = get_static $blob0 56
  b5: int = get_static $blob0 64
  b6: bool = equal b5 b0
  branch b6 %0 %1
  label %0
  b7: int = raw_load64 b4 0
  b8: int = add b7 b1
  raw_store64 b4 0 b8
  b9: int = add b0 b1
  set_static $blob0 64 b9
  call @5 b0
  return
  label %1
  b10: int = add b0 b1
  raw_store64 b0 8 b3
  set_static $blob0 48 b0
  set_static $blob0 56 b0
  set_static $blob0 64 b10
  call @5 b0
  return
fn @$sweep_block (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: bool binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = raw_load8 b6 0
  b8: int = raw_load64 b0 0
  b9: int = const 2
  b10: int = const 1
  b11: bool = equal b7 b9
  branch b11 %0 %1
  label %0
  raw_store8 b6 0 b10
  return b8
  label %1
  b11: bool = equal b7 b10
  branch b11 %2 %3
  label %2
  call @10 b0 b8
  return b8
  label %3
  return b8
fn @$sweep () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: bool binding
  b4: int binding
  b5: int binding
  b0: int = get_static $blob0 24
  b1: int = get_static $blob0 32
  b2: int = copy b0
  label %0
  b3: bool = less_than b2 b1
  branch b3 %1 %2
  label %1
  b4: int = call @11 b2
  b2: int = add b2 b4
  jump %0
  label %2
  b5: int = const 
  set_static $blob0 56 b5
  set_static $blob0 64 b5
  return
fn @$collect () -> void
  call @9
  call @12
  return
fn @$alloc (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: bool binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: bool binding
  b1: int = const 0
  b2: int = get_static $blob0 32
  b3: bool = less_than b2 b1
  branch b3 %0 %1
  label %0
  call @2
  label %1
  b4: int = call @3 b0
  b5: int = call @6 b4
  b6: bool = equal b5 b1
  branch b6 %2 %7
  label %2
  b7: int = call @7 b4
  b8: bool = equal b7 b1
  branch b8 %3 %6
  label %3
  call @13
  b5: int = call @6 b4
  b6: bool = equal b5 b1
  branch b6 %4 %7
  label %4
  b7: int = call @7 b4
  b8: bool = equal b7 b1
  branch b8 %5 %6
  label %5
  fail_oom
  label %6
  b5: int = copy b7
  label %7
  return b5
fn @$string_length (p0: ref) -> int
  b0: ref binding
  b1: int binding
  b1: int = raw_load64 b0 8
  return b1
fn @$copy_bytes (p0: int, p1: int, p2: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b3: int = const 0
  b4: int = const 1
  label %0
  b5: bool = less_than b3 b2
  branch b5 %2 %1
  label %2
  b6: int = add b1 b3
  b7: int = raw_load8 b6 0
  b8: int = add b0 b3
  raw_store8 b8 0 b7
  b3: int = add b3 b4
  jump %0
  label %1
  return
fn @$string_equal (p0: ref, p1: ref) -> bool
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b4: bool binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: int binding
  b13: int binding
  b14: bool binding
  b2: int = raw_load64 b0 8
  b3: int = raw_load64 b1 8
  b4: bool = equal b2 b3
  branch b4 %0 %1
  label %1
  b5: bool = const false
  return b5
  label %0
  b6: int = raw_load64 b0 16
  b7: int = raw_load64 b1 16
  b8: int = const 0
  b9: int = const 1
  label %2
  b4: bool = less_than b8 b2
  branch b4 %3 %4
  label %3
  b10: int = add b6 b8
  b11: int = raw_load8 b10 0
  b12: int = add b7 b8
  b13: int = raw_load8 b12 0
  b14: bool = equal b11 b13
  branch b14 %5 %1
  label %5
  b8: int = add b8 b9
  jump %2
  label %4
  b5: bool = const true
  return b5
fn @$string_concat (p0: ref, p1: ref) -> ref
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: ref binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: int binding
  b13: int binding
  b2: int = raw_load64 b0 8
  b3: int = raw_load64 b1 8
  b4: int = add b2 b3
  b5: int = const 16
  b6: int = add b4 b5
  b7: int = call @14 b6
  b8: ref = copy b7
  b9: int = const 24
  b10: int = add b8 b9
  raw_store64 b8 8 b4
  raw_store64 b8 16 b10
  b11: int = raw_load64 b0 16
  b12: int = raw_load64 b1 16
  call @16 b10 b11 b2
  b13: int = add b10 b2
  call @16 b13 b12 b3
  return b8
fn @$print (p0: ref) -> void
  b0: ref binding
  b1: int binding
  b2: ref binding
  b3: int binding
  b4: int binding
  b5: ref binding
  b6: ref binding
  b7: int binding
  b8: int binding
  b1: int = raw_load64 b0 8
  b2: ref = raw_load64 b0 16
  b3: int = const 1
  b4: int = raw_syscall syscall b3 b3 b2 b1
  b5: ref = data $blob2
  b6: ref = raw_load64 b5 16
  b7: int = const 1
  b8: int = raw_syscall syscall b7 b7 b6 b7
  return

`

// The Phase 10.5 float render, pinned: float literals bind their IEEE 754
// bit patterns as `constf`, the SSE2 forms carry the `f` suffix, and the
// branch shapes stay identical to the scalar slices.
const wantQuartersScene = `scene
data $blob0: 48 bytes 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255
data $blob1: 16 bytes 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255
data $blob2: 1 bytes 10
data $blob3: 8 bytes 113 117 97 114 116 101 114 115
entry 0 @main
fn @main () -> void
  b0: float64 binding
  b1: float64 binding
  b2: float64 binding
  b3: float64 binding
  b4: bool binding
  b5: ref binding
  b0: float = constf 4602678819172646912
  b1: float = constf 4598175219545276416
  b2: float = addf b0 b1
  b3: float = constf 4604930618986332160
  b4: bool = equalf b2 b3
  branch b4 %0 %1
  label %0
  b5: ref = data $blob3
  call @19 b5
  label %1
fn @$mmap (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b1: int = const 0
  b2: int = const 3
  b3: int = const 34
  b4: int = const 9
  b5: int = raw_syscall syscall b4 b1 b0 b2 b3
  b6: bool = less_than b5 b1
  branch b6 %0 %1
  label %0
  fail_oom
  label %1
  return b5
fn @$map () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b0: int = const 268435456
  b1: int = const 33554440
  b2: int = call @1 b0
  b3: int = call @1 b1
  b4: int = stack_pointer
  b5: int = const 
  b6: int = add b2 b0
  set_static $blob0 24 b2
  set_static $blob0 32 b2
  set_static $blob0 40 b6
  set_static $blob0 48 b5
  set_static $blob0 56 b5
  set_static $blob0 64 b5
  set_static $blob1 24 b3
  set_static $blob1 32 b4
  return
fn @$pad8 (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: bool binding
  b1: int = const 15
  b2: int = const 8
  b3: int = const 16
  b4: int = add b0 b1
  b5: int = whole_divide b4 b2
  b6: int = multiply b5 b2
  b7: bool = less_than b6 b3
  branch b7 %0 %1
  label %0
  b6: int = copy b3
  label %1
  return b6
fn @$byte_set1 (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = const 1
  raw_store8 b6 0 b7
  return
fn @$byte_set0 (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = const 0
  raw_store8 b6 0 b7
  return
fn @$search (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: bool binding
  b9: int binding
  b10: bool binding
  b11: int binding
  b12: bool binding
  b13: int binding
  b14: int binding
  b1: int = const 0
  b2: int = get_static $blob0 48
  b3: int = copy b1
  b4: int = copy b2
  b5: int = const 16
  label %0
  b6: bool = less_than b4 b1
  branch b6 %9 %1
  label %1
  b7: int = raw_load64 b4 0
  b8: bool = less_than b7 b0
  branch b8 %2 %3
  label %2
  b3: int = copy b4
  b4: int = raw_load64 b4 8
  jump %0
  label %3
  b9: int = raw_load64 b4 8
  b10: bool = equal b3 b1
  branch b10 %4 %5
  label %4
  set_static $blob0 48 b9
  jump %6
  label %5
  raw_store64 b3 8 b9
  label %6
  b11: int = subtract b7 b0
  b12: bool = less_than b11 b5
  branch b12 %8 %7
  label %7
  b13: int = add b4 b0
  raw_store64 b13 0 b11
  b14: int = get_static $blob0 48
  raw_store64 b13 8 b14
  set_static $blob0 48 b13
  jump %8
  label %8
  raw_store64 b4 0 b0
  call @4 b4
  return b4
  label %9
  return b1
fn @$bump (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b1: int = const 0
  b2: int = get_static $blob0 32
  b3: int = get_static $blob0 40
  b4: int = add b2 b0
  b5: bool = less_than b3 b4
  branch b5 %0 %1
  label %1
  set_static $blob0 32 b4
  raw_store64 b2 0 b0
  call @4 b2
  return b2
  label %0
  return b1
fn @$mark_at (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: bool binding
  b3: int binding
  b4: bool binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: bool binding
  b13: int binding
  b1: int = get_static $blob0 24
  b2: bool = less_than b0 b1
  branch b2 %0 %1
  label %1
  b3: int = get_static $blob0 40
  b4: bool = less_than b3 b0
  branch b4 %0 %2
  label %2
  b5: int = const 8
  b6: int = subtract b0 b1
  b7: int = whole_divide b6 b5
  b8: int = get_static $blob1 24
  b9: int = add b8 b7
  b10: int = raw_load8 b9 0
  b11: int = const 1
  b12: bool = equal b10 b11
  branch b12 %3 %0
  label %3
  b13: int = const 2
  raw_store8 b9 0 b13
  label %0
  return
fn @$scan_roots () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b0: int = get_static $blob1 32
  b1: int = stack_pointer
  b2: int = const 7
  b3: int = const 8
  b4: int = subtract b1 b2
  b4: int = whole_divide b4 b3
  b4: int = multiply b4 b3
  label %0
  b5: bool = less_than b4 b0
  branch b5 %1 %2
  label %1
  b6: int = raw_load64 b4 0
  call @8 b6
  b4: int = add b4 b3
  jump %0
  label %2
  return
fn @$free_block (p0: int, p1: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b2: int = const 0
  b3: int = get_static $blob0 48
  b4: int = get_static $blob0 56
  b5: int = get_static $blob0 64
  b6: bool = equal b5 b0
  branch b6 %0 %1
  label %0
  b7: int = raw_load64 b4 0
  b8: int = add b7 b1
  raw_store64 b4 0 b8
  b9: int = add b0 b1
  set_static $blob0 64 b9
  call @5 b0
  return
  label %1
  b10: int = add b0 b1
  raw_store64 b0 8 b3
  set_static $blob0 48 b0
  set_static $blob0 56 b0
  set_static $blob0 64 b10
  call @5 b0
  return
fn @$sweep_block (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: bool binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = raw_load8 b6 0
  b8: int = raw_load64 b0 0
  b9: int = const 2
  b10: int = const 1
  b11: bool = equal b7 b9
  branch b11 %0 %1
  label %0
  raw_store8 b6 0 b10
  return b8
  label %1
  b11: bool = equal b7 b10
  branch b11 %2 %3
  label %2
  call @10 b0 b8
  return b8
  label %3
  return b8
fn @$sweep () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: bool binding
  b4: int binding
  b5: int binding
  b0: int = get_static $blob0 24
  b1: int = get_static $blob0 32
  b2: int = copy b0
  label %0
  b3: bool = less_than b2 b1
  branch b3 %1 %2
  label %1
  b4: int = call @11 b2
  b2: int = add b2 b4
  jump %0
  label %2
  b5: int = const 
  set_static $blob0 56 b5
  set_static $blob0 64 b5
  return
fn @$collect () -> void
  call @9
  call @12
  return
fn @$alloc (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: bool binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: bool binding
  b1: int = const 0
  b2: int = get_static $blob0 32
  b3: bool = less_than b2 b1
  branch b3 %0 %1
  label %0
  call @2
  label %1
  b4: int = call @3 b0
  b5: int = call @6 b4
  b6: bool = equal b5 b1
  branch b6 %2 %7
  label %2
  b7: int = call @7 b4
  b8: bool = equal b7 b1
  branch b8 %3 %6
  label %3
  call @13
  b5: int = call @6 b4
  b6: bool = equal b5 b1
  branch b6 %4 %7
  label %4
  b7: int = call @7 b4
  b8: bool = equal b7 b1
  branch b8 %5 %6
  label %5
  fail_oom
  label %6
  b5: int = copy b7
  label %7
  return b5
fn @$string_length (p0: ref) -> int
  b0: ref binding
  b1: int binding
  b1: int = raw_load64 b0 8
  return b1
fn @$copy_bytes (p0: int, p1: int, p2: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b3: int = const 0
  b4: int = const 1
  label %0
  b5: bool = less_than b3 b2
  branch b5 %2 %1
  label %2
  b6: int = add b1 b3
  b7: int = raw_load8 b6 0
  b8: int = add b0 b3
  raw_store8 b8 0 b7
  b3: int = add b3 b4
  jump %0
  label %1
  return
fn @$string_equal (p0: ref, p1: ref) -> bool
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b4: bool binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: int binding
  b13: int binding
  b14: bool binding
  b2: int = raw_load64 b0 8
  b3: int = raw_load64 b1 8
  b4: bool = equal b2 b3
  branch b4 %0 %1
  label %1
  b5: bool = const false
  return b5
  label %0
  b6: int = raw_load64 b0 16
  b7: int = raw_load64 b1 16
  b8: int = const 0
  b9: int = const 1
  label %2
  b4: bool = less_than b8 b2
  branch b4 %3 %4
  label %3
  b10: int = add b6 b8
  b11: int = raw_load8 b10 0
  b12: int = add b7 b8
  b13: int = raw_load8 b12 0
  b14: bool = equal b11 b13
  branch b14 %5 %1
  label %5
  b8: int = add b8 b9
  jump %2
  label %4
  b5: bool = const true
  return b5
fn @$string_concat (p0: ref, p1: ref) -> ref
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: ref binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: int binding
  b13: int binding
  b2: int = raw_load64 b0 8
  b3: int = raw_load64 b1 8
  b4: int = add b2 b3
  b5: int = const 16
  b6: int = add b4 b5
  b7: int = call @14 b6
  b8: ref = copy b7
  b9: int = const 24
  b10: int = add b8 b9
  raw_store64 b8 8 b4
  raw_store64 b8 16 b10
  b11: int = raw_load64 b0 16
  b12: int = raw_load64 b1 16
  call @16 b10 b11 b2
  b13: int = add b10 b2
  call @16 b13 b12 b3
  return b8
fn @$print (p0: ref) -> void
  b0: ref binding
  b1: int binding
  b2: ref binding
  b3: int binding
  b4: int binding
  b5: ref binding
  b6: ref binding
  b7: int binding
  b8: int binding
  b1: int = raw_load64 b0 8
  b2: ref = raw_load64 b0 16
  b3: int = const 1
  b4: int = raw_syscall syscall b3 b3 b2 b1
  b5: ref = data $blob2
  b6: ref = raw_load64 b5 16
  b7: int = const 1
  b8: int = raw_syscall syscall b7 b7 b6 b7
  return

`

// The Phase 10.6 integer-print render, pinned: the allocator shells land
// after the program’s own functions, the digit scratch stores one byte at a
// raw offset, and each print call reaches `$print_int`.
const wantPrintIntScene = `scene
data $blob0: 48 bytes 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255
data $blob1: 16 bytes 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255
data $blob2: 1 bytes 10
data $blob3: 1 bytes 45
data $blob4: 1 bytes 48
data $blob5: 1 bytes 48
entry 0 @main
fn @main () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b0: int = const 5
  call @22 b0
  b1: int = const 0
  call @22 b1
  b2: int = const 7
  b3: int = negate b2
  call @22 b3
  b5: int = const 9223372036854775807
  b4: int = copy b5
  b6: int = const 1
  b7: int = add b4 b6
  b8: int = copy b4
  b4: int = copy b7
  call @22 b4
fn @$mmap (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b1: int = const 0
  b2: int = const 3
  b3: int = const 34
  b4: int = const 9
  b5: int = raw_syscall syscall b4 b1 b0 b2 b3
  b6: bool = less_than b5 b1
  branch b6 %0 %1
  label %0
  fail_oom
  label %1
  return b5
fn @$map () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b0: int = const 268435456
  b1: int = const 33554440
  b2: int = call @1 b0
  b3: int = call @1 b1
  b4: int = stack_pointer
  b5: int = const 
  b6: int = add b2 b0
  set_static $blob0 24 b2
  set_static $blob0 32 b2
  set_static $blob0 40 b6
  set_static $blob0 48 b5
  set_static $blob0 56 b5
  set_static $blob0 64 b5
  set_static $blob1 24 b3
  set_static $blob1 32 b4
  return
fn @$pad8 (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: bool binding
  b1: int = const 15
  b2: int = const 8
  b3: int = const 16
  b4: int = add b0 b1
  b5: int = whole_divide b4 b2
  b6: int = multiply b5 b2
  b7: bool = less_than b6 b3
  branch b7 %0 %1
  label %0
  b6: int = copy b3
  label %1
  return b6
fn @$byte_set1 (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = const 1
  raw_store8 b6 0 b7
  return
fn @$byte_set0 (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = const 0
  raw_store8 b6 0 b7
  return
fn @$search (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: bool binding
  b9: int binding
  b10: bool binding
  b11: int binding
  b12: bool binding
  b13: int binding
  b14: int binding
  b1: int = const 0
  b2: int = get_static $blob0 48
  b3: int = copy b1
  b4: int = copy b2
  b5: int = const 16
  label %0
  b6: bool = less_than b4 b1
  branch b6 %9 %1
  label %1
  b7: int = raw_load64 b4 0
  b8: bool = less_than b7 b0
  branch b8 %2 %3
  label %2
  b3: int = copy b4
  b4: int = raw_load64 b4 8
  jump %0
  label %3
  b9: int = raw_load64 b4 8
  b10: bool = equal b3 b1
  branch b10 %4 %5
  label %4
  set_static $blob0 48 b9
  jump %6
  label %5
  raw_store64 b3 8 b9
  label %6
  b11: int = subtract b7 b0
  b12: bool = less_than b11 b5
  branch b12 %8 %7
  label %7
  b13: int = add b4 b0
  raw_store64 b13 0 b11
  b14: int = get_static $blob0 48
  raw_store64 b13 8 b14
  set_static $blob0 48 b13
  jump %8
  label %8
  raw_store64 b4 0 b0
  call @4 b4
  return b4
  label %9
  return b1
fn @$bump (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b1: int = const 0
  b2: int = get_static $blob0 32
  b3: int = get_static $blob0 40
  b4: int = add b2 b0
  b5: bool = less_than b3 b4
  branch b5 %0 %1
  label %1
  set_static $blob0 32 b4
  raw_store64 b2 0 b0
  call @4 b2
  return b2
  label %0
  return b1
fn @$mark_at (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: bool binding
  b3: int binding
  b4: bool binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: bool binding
  b13: int binding
  b1: int = get_static $blob0 24
  b2: bool = less_than b0 b1
  branch b2 %0 %1
  label %1
  b3: int = get_static $blob0 40
  b4: bool = less_than b3 b0
  branch b4 %0 %2
  label %2
  b5: int = const 8
  b6: int = subtract b0 b1
  b7: int = whole_divide b6 b5
  b8: int = get_static $blob1 24
  b9: int = add b8 b7
  b10: int = raw_load8 b9 0
  b11: int = const 1
  b12: bool = equal b10 b11
  branch b12 %3 %0
  label %3
  b13: int = const 2
  raw_store8 b9 0 b13
  label %0
  return
fn @$scan_roots () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b0: int = get_static $blob1 32
  b1: int = stack_pointer
  b2: int = const 7
  b3: int = const 8
  b4: int = subtract b1 b2
  b4: int = whole_divide b4 b3
  b4: int = multiply b4 b3
  label %0
  b5: bool = less_than b4 b0
  branch b5 %1 %2
  label %1
  b6: int = raw_load64 b4 0
  call @8 b6
  b4: int = add b4 b3
  jump %0
  label %2
  return
fn @$free_block (p0: int, p1: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b2: int = const 0
  b3: int = get_static $blob0 48
  b4: int = get_static $blob0 56
  b5: int = get_static $blob0 64
  b6: bool = equal b5 b0
  branch b6 %0 %1
  label %0
  b7: int = raw_load64 b4 0
  b8: int = add b7 b1
  raw_store64 b4 0 b8
  b9: int = add b0 b1
  set_static $blob0 64 b9
  call @5 b0
  return
  label %1
  b10: int = add b0 b1
  raw_store64 b0 8 b3
  set_static $blob0 48 b0
  set_static $blob0 56 b0
  set_static $blob0 64 b10
  call @5 b0
  return
fn @$sweep_block (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: bool binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = raw_load8 b6 0
  b8: int = raw_load64 b0 0
  b9: int = const 2
  b10: int = const 1
  b11: bool = equal b7 b9
  branch b11 %0 %1
  label %0
  raw_store8 b6 0 b10
  return b8
  label %1
  b11: bool = equal b7 b10
  branch b11 %2 %3
  label %2
  call @10 b0 b8
  return b8
  label %3
  return b8
fn @$sweep () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: bool binding
  b4: int binding
  b5: int binding
  b0: int = get_static $blob0 24
  b1: int = get_static $blob0 32
  b2: int = copy b0
  label %0
  b3: bool = less_than b2 b1
  branch b3 %1 %2
  label %1
  b4: int = call @11 b2
  b2: int = add b2 b4
  jump %0
  label %2
  b5: int = const 
  set_static $blob0 56 b5
  set_static $blob0 64 b5
  return
fn @$collect () -> void
  call @9
  call @12
  return
fn @$alloc (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: bool binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: bool binding
  b1: int = const 0
  b2: int = get_static $blob0 32
  b3: bool = less_than b2 b1
  branch b3 %0 %1
  label %0
  call @2
  label %1
  b4: int = call @3 b0
  b5: int = call @6 b4
  b6: bool = equal b5 b1
  branch b6 %2 %7
  label %2
  b7: int = call @7 b4
  b8: bool = equal b7 b1
  branch b8 %3 %6
  label %3
  call @13
  b5: int = call @6 b4
  b6: bool = equal b5 b1
  branch b6 %4 %7
  label %4
  b7: int = call @7 b4
  b8: bool = equal b7 b1
  branch b8 %5 %6
  label %5
  fail_oom
  label %6
  b5: int = copy b7
  label %7
  return b5
fn @$string_length (p0: ref) -> int
  b0: ref binding
  b1: int binding
  b1: int = raw_load64 b0 8
  return b1
fn @$copy_bytes (p0: int, p1: int, p2: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b3: int = const 0
  b4: int = const 1
  label %0
  b5: bool = less_than b3 b2
  branch b5 %2 %1
  label %2
  b6: int = add b1 b3
  b7: int = raw_load8 b6 0
  b8: int = add b0 b3
  raw_store8 b8 0 b7
  b3: int = add b3 b4
  jump %0
  label %1
  return
fn @$string_equal (p0: ref, p1: ref) -> bool
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b4: bool binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: int binding
  b13: int binding
  b14: bool binding
  b2: int = raw_load64 b0 8
  b3: int = raw_load64 b1 8
  b4: bool = equal b2 b3
  branch b4 %0 %1
  label %1
  b5: bool = const false
  return b5
  label %0
  b6: int = raw_load64 b0 16
  b7: int = raw_load64 b1 16
  b8: int = const 0
  b9: int = const 1
  label %2
  b4: bool = less_than b8 b2
  branch b4 %3 %4
  label %3
  b10: int = add b6 b8
  b11: int = raw_load8 b10 0
  b12: int = add b7 b8
  b13: int = raw_load8 b12 0
  b14: bool = equal b11 b13
  branch b14 %5 %1
  label %5
  b8: int = add b8 b9
  jump %2
  label %4
  b5: bool = const true
  return b5
fn @$string_concat (p0: ref, p1: ref) -> ref
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: ref binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: int binding
  b13: int binding
  b2: int = raw_load64 b0 8
  b3: int = raw_load64 b1 8
  b4: int = add b2 b3
  b5: int = const 16
  b6: int = add b4 b5
  b7: int = call @14 b6
  b8: ref = copy b7
  b9: int = const 24
  b10: int = add b8 b9
  raw_store64 b8 8 b4
  raw_store64 b8 16 b10
  b11: int = raw_load64 b0 16
  b12: int = raw_load64 b1 16
  call @16 b10 b11 b2
  b13: int = add b10 b2
  call @16 b13 b12 b3
  return b8
fn @$write1 (p0: ref) -> void
  b0: ref binding
  b1: int binding
  b2: ref binding
  b3: int binding
  b1: int = const 1
  b2: ref = raw_load64 b0 16
  b3: int = raw_syscall syscall b1 b1 b2 b1
  return
fn @$print_newline () -> void
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b0: ref = data $blob2
  b1: ref = raw_load64 b0 16
  b2: int = const 1
  b3: int = raw_syscall syscall b2 b2 b1 b2
  return
fn @$digits (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: ref binding
  b1: int = const 10
  b2: int = whole_remainder b0 b1
  b3: int = whole_divide b0 b1
  b4: int = const 0
  b5: bool = equal b3 b4
  branch b5 %1 %0
  label %0
  call @21 b3
  label %1
  b6: int = negate b2
  b7: int = const 48
  b8: int = add b6 b7
  set_static $blob5 24 b8
  b9: ref = data $blob5
  call @19 b9
  return
fn @$print_int (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: bool binding
  b3: bool binding
  b4: ref binding
  b5: ref binding
  b1: int = const 0
  b2: bool = equal b0 b1
  branch b2 %0 %1
  label %1
  b3: bool = less_than b0 b1
  branch b3 %2 %3
  label %2
  b4: ref = data $blob3
  call @19 b4
  jump %4
  label %3
  b0: int = negate b0
  label %4
  call @21 b0
  jump %5
  label %0
  b5: ref = data $blob4
  call @19 b5
  label %5
  call @20
  return

`

// The Phase 10.6 bool-print render, pinned: the bool helper branches on the
// one-byte condition and reaches the shared string "$print" routine.
const wantPrintBoolScene = `scene
data $blob0: 48 bytes 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255
data $blob1: 16 bytes 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255
data $blob2: 1 bytes 10
data $blob3: 4 bytes 116 114 117 101
data $blob4: 5 bytes 102 97 108 115 101
entry 0 @main
fn @main () -> void
  b0: bool binding
  b1: bool binding
  b2: int binding
  b3: int binding
  b4: bool binding
  b5: bool binding
  b6: bool binding
  b7: int binding
  b8: int binding
  b9: bool binding
  b10: bool binding
  b11: int binding
  b12: int binding
  b13: bool binding
  b0: bool = const true
  call @20 b0
  b1: bool = const false
  call @20 b1
  b2: int = const 1
  b3: int = const 2
  b4: bool = less_than b2 b3
  call @20 b4
  b5: bool = const true
  branch b5 %0 %1
  label %1
  b6: bool = const true
  jump %2
  label %0
  b6: bool = const false
  label %2
  call @20 b6
  b7: int = const 3
  b8: int = const 3
  b9: bool = equal b7 b8
  branch b9 %3 %4
  label %4
  b10: bool = const false
  jump %5
  label %3
  b11: int = const 4
  b12: int = const 10
  b13: bool = less_than b11 b12
  b10: bool = copy b13
  label %5
  call @20 b10
fn @$mmap (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b1: int = const 0
  b2: int = const 3
  b3: int = const 34
  b4: int = const 9
  b5: int = raw_syscall syscall b4 b1 b0 b2 b3
  b6: bool = less_than b5 b1
  branch b6 %0 %1
  label %0
  fail_oom
  label %1
  return b5
fn @$map () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b0: int = const 268435456
  b1: int = const 33554440
  b2: int = call @1 b0
  b3: int = call @1 b1
  b4: int = stack_pointer
  b5: int = const 
  b6: int = add b2 b0
  set_static $blob0 24 b2
  set_static $blob0 32 b2
  set_static $blob0 40 b6
  set_static $blob0 48 b5
  set_static $blob0 56 b5
  set_static $blob0 64 b5
  set_static $blob1 24 b3
  set_static $blob1 32 b4
  return
fn @$pad8 (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: bool binding
  b1: int = const 15
  b2: int = const 8
  b3: int = const 16
  b4: int = add b0 b1
  b5: int = whole_divide b4 b2
  b6: int = multiply b5 b2
  b7: bool = less_than b6 b3
  branch b7 %0 %1
  label %0
  b6: int = copy b3
  label %1
  return b6
fn @$byte_set1 (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = const 1
  raw_store8 b6 0 b7
  return
fn @$byte_set0 (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = const 0
  raw_store8 b6 0 b7
  return
fn @$search (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: bool binding
  b9: int binding
  b10: bool binding
  b11: int binding
  b12: bool binding
  b13: int binding
  b14: int binding
  b1: int = const 0
  b2: int = get_static $blob0 48
  b3: int = copy b1
  b4: int = copy b2
  b5: int = const 16
  label %0
  b6: bool = less_than b4 b1
  branch b6 %9 %1
  label %1
  b7: int = raw_load64 b4 0
  b8: bool = less_than b7 b0
  branch b8 %2 %3
  label %2
  b3: int = copy b4
  b4: int = raw_load64 b4 8
  jump %0
  label %3
  b9: int = raw_load64 b4 8
  b10: bool = equal b3 b1
  branch b10 %4 %5
  label %4
  set_static $blob0 48 b9
  jump %6
  label %5
  raw_store64 b3 8 b9
  label %6
  b11: int = subtract b7 b0
  b12: bool = less_than b11 b5
  branch b12 %8 %7
  label %7
  b13: int = add b4 b0
  raw_store64 b13 0 b11
  b14: int = get_static $blob0 48
  raw_store64 b13 8 b14
  set_static $blob0 48 b13
  jump %8
  label %8
  raw_store64 b4 0 b0
  call @4 b4
  return b4
  label %9
  return b1
fn @$bump (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b1: int = const 0
  b2: int = get_static $blob0 32
  b3: int = get_static $blob0 40
  b4: int = add b2 b0
  b5: bool = less_than b3 b4
  branch b5 %0 %1
  label %1
  set_static $blob0 32 b4
  raw_store64 b2 0 b0
  call @4 b2
  return b2
  label %0
  return b1
fn @$mark_at (p0: int) -> void
  b0: int binding
  b1: int binding
  b2: bool binding
  b3: int binding
  b4: bool binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: bool binding
  b13: int binding
  b1: int = get_static $blob0 24
  b2: bool = less_than b0 b1
  branch b2 %0 %1
  label %1
  b3: int = get_static $blob0 40
  b4: bool = less_than b3 b0
  branch b4 %0 %2
  label %2
  b5: int = const 8
  b6: int = subtract b0 b1
  b7: int = whole_divide b6 b5
  b8: int = get_static $blob1 24
  b9: int = add b8 b7
  b10: int = raw_load8 b9 0
  b11: int = const 1
  b12: bool = equal b10 b11
  branch b12 %3 %0
  label %3
  b13: int = const 2
  raw_store8 b9 0 b13
  label %0
  return
fn @$scan_roots () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b0: int = get_static $blob1 32
  b1: int = stack_pointer
  b2: int = const 7
  b3: int = const 8
  b4: int = subtract b1 b2
  b4: int = whole_divide b4 b3
  b4: int = multiply b4 b3
  label %0
  b5: bool = less_than b4 b0
  branch b5 %1 %2
  label %1
  b6: int = raw_load64 b4 0
  call @8 b6
  b4: int = add b4 b3
  jump %0
  label %2
  return
fn @$free_block (p0: int, p1: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b2: int = const 0
  b3: int = get_static $blob0 48
  b4: int = get_static $blob0 56
  b5: int = get_static $blob0 64
  b6: bool = equal b5 b0
  branch b6 %0 %1
  label %0
  b7: int = raw_load64 b4 0
  b8: int = add b7 b1
  raw_store64 b4 0 b8
  b9: int = add b0 b1
  set_static $blob0 64 b9
  call @5 b0
  return
  label %1
  b10: int = add b0 b1
  raw_store64 b0 8 b3
  set_static $blob0 48 b0
  set_static $blob0 56 b0
  set_static $blob0 64 b10
  call @5 b0
  return
fn @$sweep_block (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: bool binding
  b1: int = get_static $blob0 24
  b2: int = subtract b0 b1
  b3: int = const 8
  b4: int = whole_divide b2 b3
  b5: int = get_static $blob1 24
  b6: int = add b5 b4
  b7: int = raw_load8 b6 0
  b8: int = raw_load64 b0 0
  b9: int = const 2
  b10: int = const 1
  b11: bool = equal b7 b9
  branch b11 %0 %1
  label %0
  raw_store8 b6 0 b10
  return b8
  label %1
  b11: bool = equal b7 b10
  branch b11 %2 %3
  label %2
  call @10 b0 b8
  return b8
  label %3
  return b8
fn @$sweep () -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: bool binding
  b4: int binding
  b5: int binding
  b0: int = get_static $blob0 24
  b1: int = get_static $blob0 32
  b2: int = copy b0
  label %0
  b3: bool = less_than b2 b1
  branch b3 %1 %2
  label %1
  b4: int = call @11 b2
  b2: int = add b2 b4
  jump %0
  label %2
  b5: int = const 
  set_static $blob0 56 b5
  set_static $blob0 64 b5
  return
fn @$collect () -> void
  call @9
  call @12
  return
fn @$alloc (p0: int) -> int
  b0: int binding
  b1: int binding
  b2: int binding
  b3: bool binding
  b4: int binding
  b5: int binding
  b6: bool binding
  b7: int binding
  b8: bool binding
  b1: int = const 0
  b2: int = get_static $blob0 32
  b3: bool = less_than b2 b1
  branch b3 %0 %1
  label %0
  call @2
  label %1
  b4: int = call @3 b0
  b5: int = call @6 b4
  b6: bool = equal b5 b1
  branch b6 %2 %7
  label %2
  b7: int = call @7 b4
  b8: bool = equal b7 b1
  branch b8 %3 %6
  label %3
  call @13
  b5: int = call @6 b4
  b6: bool = equal b5 b1
  branch b6 %4 %7
  label %4
  b7: int = call @7 b4
  b8: bool = equal b7 b1
  branch b8 %5 %6
  label %5
  fail_oom
  label %6
  b5: int = copy b7
  label %7
  return b5
fn @$string_length (p0: ref) -> int
  b0: ref binding
  b1: int binding
  b1: int = raw_load64 b0 8
  return b1
fn @$copy_bytes (p0: int, p1: int, p2: int) -> void
  b0: int binding
  b1: int binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b3: int = const 0
  b4: int = const 1
  label %0
  b5: bool = less_than b3 b2
  branch b5 %2 %1
  label %2
  b6: int = add b1 b3
  b7: int = raw_load8 b6 0
  b8: int = add b0 b3
  raw_store8 b8 0 b7
  b3: int = add b3 b4
  jump %0
  label %1
  return
fn @$string_equal (p0: ref, p1: ref) -> bool
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b4: bool binding
  b5: bool binding
  b6: int binding
  b7: int binding
  b8: int binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: int binding
  b13: int binding
  b14: bool binding
  b2: int = raw_load64 b0 8
  b3: int = raw_load64 b1 8
  b4: bool = equal b2 b3
  branch b4 %0 %1
  label %1
  b5: bool = const false
  return b5
  label %0
  b6: int = raw_load64 b0 16
  b7: int = raw_load64 b1 16
  b8: int = const 0
  b9: int = const 1
  label %2
  b4: bool = less_than b8 b2
  branch b4 %3 %4
  label %3
  b10: int = add b6 b8
  b11: int = raw_load8 b10 0
  b12: int = add b7 b8
  b13: int = raw_load8 b12 0
  b14: bool = equal b11 b13
  branch b14 %5 %1
  label %5
  b8: int = add b8 b9
  jump %2
  label %4
  b5: bool = const true
  return b5
fn @$string_concat (p0: ref, p1: ref) -> ref
  b0: ref binding
  b1: ref binding
  b2: int binding
  b3: int binding
  b4: int binding
  b5: int binding
  b6: int binding
  b7: int binding
  b8: ref binding
  b9: int binding
  b10: int binding
  b11: int binding
  b12: int binding
  b13: int binding
  b2: int = raw_load64 b0 8
  b3: int = raw_load64 b1 8
  b4: int = add b2 b3
  b5: int = const 16
  b6: int = add b4 b5
  b7: int = call @14 b6
  b8: ref = copy b7
  b9: int = const 24
  b10: int = add b8 b9
  raw_store64 b8 8 b4
  raw_store64 b8 16 b10
  b11: int = raw_load64 b0 16
  b12: int = raw_load64 b1 16
  call @16 b10 b11 b2
  b13: int = add b10 b2
  call @16 b13 b12 b3
  return b8
fn @$print (p0: ref) -> void
  b0: ref binding
  b1: int binding
  b2: ref binding
  b3: int binding
  b4: int binding
  b5: ref binding
  b6: ref binding
  b7: int binding
  b8: int binding
  b1: int = raw_load64 b0 8
  b2: ref = raw_load64 b0 16
  b3: int = const 1
  b4: int = raw_syscall syscall b3 b3 b2 b1
  b5: ref = data $blob2
  b6: ref = raw_load64 b5 16
  b7: int = const 1
  b8: int = raw_syscall syscall b7 b7 b6 b7
  return
fn @$print_bool (p0: bool) -> void
  b0: bool binding
  b1: ref binding
  branch b0 %0 %1
  label %0
  b1: ref = data $blob3
  call @19 b1
  return
  label %1
  b1: ref = data $blob4
  call @19 b1
  return

`

// lowerCorpusSources returns the scalar corpus programs the lowering runs.
func lowerCorpusSources(t *testing.T) []string {
	t.Helper()
	sources, err := filepath.Glob(repoPath(t, "..", "..", "tests", "native", "*.ktn"))
	if err != nil || len(sources) == 0 {
		t.Fatalf("globbing the native corpus: %v (%d sources)", err, len(sources))
	}
	return sources
}

// TestNativeProbeLowerCorpus lowers every corpus program into a static ELF
// with no C compiler, runs it with an empty PATH, and demands the output,
// standard error, and status of the C pipeline's build of the same source;
// the Kitten pipeline's production is byte-compared in its own test. The
// sources stay deliberately small because the selector's slice frame carries
// at most fourteen locals per function.
func TestNativeProbeLowerCorpus(t *testing.T) {
	requireToolchain(t)
	executable := buildNativeProbe(t)
	purr := goPurrReference(t)
	for _, source := range lowerCorpusSources(t) {
		name := strings.TrimSuffix(filepath.Base(source), ".ktn")
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			cRun := runExternal(t, purr, []string{"run", source}, directory, nil)

			native := filepath.Join(directory, name)
			written := runExternal(t, executable, []string{"lower", source, native}, directory, nil)
			if written.status != 0 || written.stderr != "" || written.stdout != "built "+native+"\n" {
				t.Fatalf("lower: status %d, stdout %q, stderr %q", written.status, written.stdout, written.stderr)
			}
			if err := os.Chmod(native, 0o755); err != nil {
				t.Fatalf("chmod native: %v", err)
			}

			run := runExternal(t, native, nil, directory, []string{"PATH=/nonexistent"})
			if run.status != cRun.status || run.stdout != cRun.stdout || run.stderr != cRun.stderr {
				t.Fatalf("native behavior differs:\nC: %d %q %q\nnative: %d %q %q",
					cRun.status, cRun.stdout, cRun.stderr, run.status, run.stdout, run.stderr)
			}

			// The production is deterministic: a rebuild is byte-identical.
			again := filepath.Join(directory, name+"-again")
			if written := runExternal(t, executable, []string{"lower", source, again}, directory, nil); written.status != 0 {
				t.Fatalf("rebuild status %d", written.status)
			}
			first, err := os.ReadFile(native)
			if err != nil {
				t.Fatalf("reading the first ELF: %v", err)
			}
			second, err := os.ReadFile(again)
			if err != nil {
				t.Fatalf("reading the rebuilt ELF: %v", err)
			}
			if string(first) != string(second) {
				t.Fatalf("two productions of %s differ in %d bytes", name, len(first))
			}
		})
	}
}

// TestNativeProbeLowerRender pins the pooled string-literal scene's
// rendering, byte for byte.
func TestNativeProbeLowerRender(t *testing.T) {
	executable := buildNativeProbe(t)
	source := repoPath(t, "..", "..", "tests", "native", "pool.ktn")
	result := runExternal(t, executable, []string{"lower", source}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("pool render: status %d, stderr %q", result.status, result.stderr)
	}
	if result.stdout != wantPoolScene {
		t.Fatalf("pool render differs:\n--- got ---\n%s--- want ---\n%s", result.stdout, wantPoolScene)
	}
}

// TestNativeProbeFloatEncode drives the decimal-to-binary64 encoder through
// the probe's `float` mode: every case the corpus carries must equal the
// bit pattern the language-level oracle (binary64 of the same literal)
// produces, across both pipelines' builds. The fixed list pins every edge
// the slice's claims rest on: exact halves and thirds, halfway ties at
// powers of two, the round-trip precision edge, the smallest subnormal, the
// maximal normal's boundary, and the saturations. The seeded random corpus
// stays within the seventeen-digit precision bound; literals beyond it fail
// deterministically and belong to the failure table.
func TestNativeProbeFloatEncode(t *testing.T) {
	requireToolchain(t)
	executable := buildNativeProbe(t)
	directory := t.TempDir()
	cases := []string{
		// Exact dyadic halves at every exponent the bits carry.
		"0.5", "1.5", "2.5", "4.5", "1125899906842624.5", "2251799813685248.5",
		"4503599627370496.5", "9007199254740991.5",
		// Ties where round-to-nearest-even keeps the even side.
		"4503599627370495.5",
		// Thirds and the decimal classics.
		"0.1", "0.2", "0.25", "0.3", "0.75", "3.0625", "3.14159265358979",
		"3.141592653589793", "2.718281828459045", "0.30000000000000001",
		"0.30000000000000004", "0.99999999999999999", "100.5", "512.025",
		// Precision and range edges.
		"9007199254740992.0", "99999999999999999.0", "17976931348623157.0",
		"0.0000000000000000000005", "0.05", "0.005", "0.0005",
		// Saturation and underflow.
		"1" + strings.Repeat("0", 308) + ".0",
		"1" + strings.Repeat("0", 309) + ".0",
		"0" + strings.Repeat("0", 323) + "5.0",
		"0" + strings.Repeat("0", 323) + "25.0",
	}
	rng := rand.New(rand.NewSource(10260030))
	for index := 0; index < 1200; index++ {
		integers := 1 + rng.Intn(5)
		digits := []byte{byte('1' + rng.Intn(9))}
		for position := 1; position < integers; position++ {
			digits = append(digits, byte('0'+rng.Intn(10)))
		}
		fractional := 17 - integers
		if rng.Intn(2) == 0 {
			fractional = 1 + rng.Intn(6)
			if integers+fractional > 17 {
				fractional = 17 - integers
			}
		}
		if fractional < 1 {
			fractional = 1
		}
		for position := 0; position < fractional; position++ {
			digits = append(digits, byte('0'+rng.Intn(10)))
		}
		cases = append(cases, string(digits[:integers])+"."+string(digits[integers:]))
	}
	badCase := "0.300000000000000044"
	failures := 0
	for _, text := range cases {
		result := runExternal(t, executable, []string{"float", text}, directory, nil)
		bits, err := strconv.ParseUint(strings.TrimSpace(result.stdout), 10, 64)
		if err != nil {
			t.Errorf("float %q: probe printed %q", text, result.stdout)
			failures++
			continue
		}
		want := math.Float64bits(mustParseFloat(t, text))
		if uint64(bits) != want {
			t.Errorf("float %q: bits %d, want %d", text, bits, want)
			failures++
		}
	}
	if failures != 0 {
		t.Fatalf("%d float encodings differ from the oracle", failures)
	}
	broken := runExternal(t, executable, []string{"float", badCase}, directory, nil)
	if broken.status != 1 || broken.stdout != "error float literals with more than 17 significant digits are not supported yet\n" {
		t.Fatalf("float %q: status %d, stdout %q", badCase, broken.status, broken.stdout)
	}
}

// mustParseFloat is the language-level oracle: Go's strconv parses a
// decimal literal exactly as correctly as the C toolchain's conversion.
// The range error is the saturation the double performs on out-of-band
// magnitudes, and the returned magnitude (infinite or zero) is what the
// conversion produces, so it is not an oracle disagreement.
func mustParseFloat(t *testing.T, text string) float64 {
	t.Helper()
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		if rangeError, ok := err.(*strconv.NumError); ok && rangeError.Err == strconv.ErrRange {
			return value
		}
		t.Fatalf("the oracle rejects %q: %v", text, err)
	}
	return value
}

// TestNativeProbeLowerFloatRender pins the float scene's rendering, byte
// for byte: bit patterns as constf constants and the SSE2 spellings.
func TestNativeProbeLowerFloatRender(t *testing.T) {
	executable := buildNativeProbe(t)
	source := repoPath(t, "..", "..", "tests", "native", "quarters.ktn")
	result := runExternal(t, executable, []string{"lower", source}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("quarters render: status %d, stderr %q", result.status, result.stderr)
	}
	if result.stdout != wantQuartersScene {
		t.Fatalf("quarters render differs:\n--- got ---\n%s--- want ---\n%s", result.stdout, wantQuartersScene)
	}
}

// TestNativeProbeLowerIntRender pins the integer-print scene's rendering,
// byte for byte: the allocator shells trail the program's functions, the
// digit scratch stores one byte at a raw offset, and every print(int) call
// reaches "$print_int".
func TestNativeProbeLowerIntRender(t *testing.T) {
	executable := buildNativeProbe(t)
	source := repoPath(t, "..", "..", "tests", "native", "printint.ktn")
	result := runExternal(t, executable, []string{"lower", source}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("printint render: status %d, stderr %q", result.status, result.stderr)
	}
	if result.stdout != wantPrintIntScene {
		t.Fatalf("printint render differs:\n--- got ---\n%s--- want ---\n%s", result.stdout, wantPrintIntScene)
	}
}

// TestNativeProbeLowerBoolRender pins the bool-print scene's rendering,
// byte for byte: the bool helper branches on the one-byte condition and
// reaches the shared string "$print" routine.
func TestNativeProbeLowerBoolRender(t *testing.T) {
	executable := buildNativeProbe(t)
	source := repoPath(t, "..", "..", "tests", "native", "printbool.ktn")
	result := runExternal(t, executable, []string{"lower", source}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("printbool render: status %d, stderr %q", result.status, result.stderr)
	}
	if result.stdout != wantPrintBoolScene {
		t.Fatalf("printbool render differs:\n--- got ---\n%s--- want ---\n%s", result.stdout, wantPrintBoolScene)
	}
}

// TestNativeProbeOOM writes the allocation-failure executable with no C
// compiler, runs it with an empty PATH, and demands the C runtime's exact
// ktn_rt_fail spelling for the out-of-memory text: standard error only,
// status 1. A repeated production is byte-identical.
func TestNativeProbeOOM(t *testing.T) {
	requireToolchain(t)
	executable := buildNativeProbe(t)
	directory := t.TempDir()

	native := filepath.Join(directory, "oom-native")
	written := runExternal(t, executable, []string{"elf-oom", native}, directory, nil)
	if written.status != 0 || written.stderr != "" || written.stdout != "built "+native+"\n" {
		t.Fatalf("oom write: status %d, stdout %q, stderr %q", written.status, written.stdout, written.stderr)
	}
	if err := os.Chmod(native, 0o755); err != nil {
		t.Fatalf("chmod native: %v", err)
	}

	run := runExternal(t, native, nil, directory, []string{"PATH=/nonexistent"})
	if run.status != 1 || run.stderr != "purr: out of memory\n" || run.stdout != "" {
		t.Fatalf("native oom run: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
	}

	again := filepath.Join(directory, "oom-native-again")
	if written := runExternal(t, executable, []string{"elf-oom", again}, directory, nil); written.status != 0 {
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
		t.Fatalf("two oom productions differ in %d bytes", len(first))
	}
}

// TestNativeProbeGC writes the collection-mirror executable with no C
// compiler and runs it with an empty PATH: six hundred one-megabyte
// allocations over the collector, of which only the last reference
// survives, must keep the loop correct through the 256 MiB limit and print
// the close, with status 0. A repeated production is byte-identical.
func TestNativeProbeGC(t *testing.T) {
	requireToolchain(t)
	executable := buildNativeProbe(t)
	directory := t.TempDir()

	native := filepath.Join(directory, "gc-native")
	written := runExternal(t, executable, []string{"elf-gc", native}, directory, nil)
	if written.status != 0 || written.stderr != "" || written.stdout != "built "+native+"\n" {
		t.Fatalf("gc write: status %d, stdout %q, stderr %q", written.status, written.stdout, written.stderr)
	}
	if err := os.Chmod(native, 0o755); err != nil {
		t.Fatalf("chmod native: %v", err)
	}

	run := runExternal(t, native, nil, directory, []string{"PATH=/nonexistent"})
	if run.status != 0 || run.stderr != "" || run.stdout != "done\n" {
		t.Fatalf("native gc run: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
	}

	again := filepath.Join(directory, "gc-native-again")
	if written := runExternal(t, executable, []string{"elf-gc", again}, directory, nil); written.status != 0 {
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
		t.Fatalf("two gc productions differ in %d bytes", len(first))
	}
}

// lowerFailureCase is one unsupported-construct source with its expected
// message tail. A positionless case omits the position; the loader's
// read-failure spelling (path with no position) is the pathOnly kind.
type lowerFailureCase struct {
	file    string
	line    string
	col     string
	kind    string // "", "pathless", or "readerror"
	message string
}

func lowerFailureCases(badDirectory string) []lowerFailureCase {
	return []lowerFailureCase{
		{file: "array.ktn", line: "2", col: "5", message: "array values are not supported yet"},
		{file: "bits.ktn", line: "2", col: "18", message: "bitwise operators are not supported yet"},
		{file: "booleq.ktn", line: "2", col: "22", message: "comparing bool values is not supported yet"},
		{file: "enum.ktn", line: "1", col: "1", message: "enum declarations are not supported yet"},
		{file: "eprint.ktn", line: "2", col: "5", message: `built-in "eprint" is not supported yet`},
		{file: "float.ktn", line: "2", col: "21", message: "float literals with more than 17 significant digits are not supported yet"},

		{file: "imported.ktn", kind: "pathless", message: "the program has no main function"},
		{file: "import.ktn", kind: "pathless", message: "the native lowering supports a single-module program, but the program has 2 modules"},
		{file: "index.ktn", line: "2", col: "10", message: "indexing is not supported yet"},
		{file: "lex.ktn", line: "2", col: "29", message: "newline in string literal"},
		{file: "map.ktn", line: "2", col: "5", message: "map values are not supported yet"},
		{file: "missing.ktn", kind: "readerror", message: "cannot read source file: open " + filepath.Join(badDirectory, "not_here.ktn") + ": no such file or directory"},
		{file: "parse.ktn", line: "2", col: "17", message: `expected an expression, found Semicolon ";"`},
		{file: "printmany.ktn", line: "2", col: "10", message: "print is supported with exactly one argument, but the call passes 2"},
		{file: "substring.ktn", line: "2", col: "11", message: `built-in "substring" is not supported yet`},
		{file: "struct.ktn", line: "1", col: "1", message: "struct declarations are not supported yet"},
		{file: "typeerror.ktn", line: "2", col: "17", message: `initializer for variable "value": expected int, found string`},
		{file: "union.ktn", line: "1", col: "1", message: "union declarations are not supported yet"},
	}
}

// lowerFailureWant composes the expected `error` line for one case under one
// bad directory spelling.
func lowerFailureWant(badDirectory string, failure lowerFailureCase) string {
	source := filepath.Join(badDirectory, failure.file)
	lead := "error " + source
	if failure.kind == "pathless" {
		return "error " + failure.message + "\n"
	}
	if failure.kind == "readerror" || failure.line == "0" {
		if failure.kind == "readerror" {
			// A read failure names the missing file, not the importer.
			missing := filepath.Join(badDirectory, "not_here.ktn")
			return "error " + missing + ": " + failure.message + "\n"
		}
		return lead + ": " + failure.message + "\n"
	}
	return "error " + source + ":" + failure.line + ":" + failure.col + ": " + failure.message + "\n"
}

// TestNativeProbeLowerFailures pins one deterministic message per
// unsupported construct, lex error, parse error, check error, and load
// failure: on standard output, with status 1 and empty standard error,
// exactly like the failure scenes above.
func TestNativeProbeLowerFailures(t *testing.T) {
	executable := buildNativeProbe(t)
	bad := repoPath(t, "..", "..", "tests", "native", "bad")
	directory := t.TempDir()
	for _, failure := range lowerFailureCases(bad) {
		t.Run(failure.file, func(t *testing.T) {
			source := filepath.Join(bad, failure.file)
			result := runExternal(t, executable, []string{"lower", source}, directory, nil)
			if result.status != 1 {
				t.Fatalf("status %d, want 1", result.status)
			}
			want := lowerFailureWant(filepath.Clean(bad), failure)
			if result.stdout != want {
				t.Fatalf("failure message differs:\n--- got ---\n%s--- want ---\n%s", result.stdout, want)
			}
			if result.stderr != "" {
				t.Fatalf("stderr %q, want empty", result.stderr)
			}
		})
	}
}

// TestNativeProbeLowerFrameLimit lowers an oversized function: the scene is
// valid, so the failure arrives from the selector's frame layout, with the
// C runtime's status behavior intact and no file written.
func TestNativeProbeLowerFrameLimit(t *testing.T) {
	requireToolchain(t)
	executable := buildNativeProbe(t)
	directory := t.TempDir()
	source := repoPath(t, "..", "..", "tests", "native", "bad", "bigfn.ktn")
	native := filepath.Join(directory, "big")
	result := runExternal(t, executable, []string{"lower", source, native}, directory, nil)
	if result.status != 1 {
		t.Fatalf("status %d, want 1", result.status)
	}
	if result.stderr != "native: function @main: 30 locals do not fit the slice's frame layout\n" {
		t.Fatalf("stderr %q", result.stderr)
	}
	if result.stdout != "" {
		t.Fatalf("stdout %q, want empty", result.stdout)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Fatalf("an oversized function's production exists")
	}
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
		{"bad-static-blob", "error function @main: instruction 0: blob 9 is out of range (the scene has 1 blobs)\n"},
		{"bad-static-offset", "error function @main: instruction 0: offset 60 does not fit blob $blob0's 32 bytes\n"},
		{"bad-static-value", "error function @main: instruction 0: value binding 0 has bool, expected int\n"},
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
		{"elf-oom", "oom"},
		{"elf-gc", "gc"},
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

// TestNativeProbeKittenPipelineLower builds the probe with the Kitten-written
// purr-kt sidecar as well and opposes the two pipelines over the whole
// lowering corpus: every scene rendering and every lowering failure message
// must agree byte for byte, and each corpus program's ELF must be
// byte-identical. The Kitten-produced arithmetic executable's behavior is
// asserted directly against the C pipeline's build; the rest share the ELF
// bytes exactly. The bad corpus lives in one directory used for every run,
// so the canonical paths inside message texts agree too.
func TestNativeProbeKittenPipelineLower(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	kittenPurr := buildPurrKTWithKitten(t, stage1)
	ktExecutable := filepath.Join(t.TempDir(), "native-probe-kt")
	built := runPurrKT(t, kittenPurr, t.TempDir(), []string{
		"build", repoPath(t, "..", "..", "selfhost", "native.ktn"), "-o", ktExecutable,
	})
	if built.status != 0 || built.stderr != "" || built.stdout != "built "+ktExecutable+"\n" {
		t.Fatalf("Kitten build of the native probe: status %d, stdout %q, stderr %q",
			built.status, built.stdout, built.stderr)
	}
	goExecutable := buildNativeProbe(t)

	// One directory for every run, so message texts carrying paths agree.
	directory := t.TempDir()
	sources := lowerCorpusSources(t)

	for _, source := range sources {
		name := filepath.Base(source)
		t.Run(name, func(t *testing.T) {
			goRender := runExternal(t, goExecutable, []string{"lower", source}, directory, nil)
			ktRender := runExternal(t, ktExecutable, []string{"lower", source}, directory, nil)
			if ktRender.status != goRender.status || ktRender.stdout != goRender.stdout || ktRender.stderr != goRender.stderr {
				t.Fatalf("renders differ:\nGo: %d %q %q\nKitten: %d %q %q",
					goRender.status, goRender.stdout, goRender.stderr,
					ktRender.status, ktRender.stdout, ktRender.stderr)
			}

			goNative := filepath.Join(directory, name+"-go")
			pthread := filepath.Join(directory, name+"-kt")
			if written := runExternal(t, goExecutable, []string{"lower", source, goNative}, directory, nil); written.status != 0 {
				t.Fatalf("Go pipeline lower: status %d, stderr %q", written.status, written.stderr)
			}
			if written := runExternal(t, ktExecutable, []string{"lower", source, pthread}, directory, nil); written.status != 0 {
				t.Fatalf("Kitten pipeline lower: status %d, stderr %q", written.status, written.stderr)
			}
			goBytes, err := os.ReadFile(goNative)
			if err != nil {
				t.Fatalf("reading the Go-produced ELF: %v", err)
			}
			ktBytes, err := os.ReadFile(pthread)
			if err != nil {
				t.Fatalf("reading the Kitten-produced ELF: %v", err)
			}
			if string(goBytes) != string(ktBytes) {
				t.Fatalf("the two pipelines' %s ELFs differ in %d bytes", name, len(goBytes))
			}

			if name == "arith.ktn" {
				if err := os.Chmod(pthread, 0o755); err != nil {
					t.Fatalf("chmod the Kitten-produced ELF: %v", err)
				}
				run := runExternal(t, pthread, nil, directory, []string{"PATH=/nonexistent"})
				if run.status != 0 || run.stdout != "large\nnegged\n" || run.stderr != "" {
					t.Fatalf("Kitten-produced arith run: %d %q %q", run.status, run.stdout, run.stderr)
				}
			}
		})
	}

	bad := filepath.Clean(repoPath(t, "..", "..", "tests", "native", "bad"))
	for _, failure := range lowerFailureCases(bad) {
		t.Run("bad-"+failure.file, func(t *testing.T) {
			source := filepath.Join(bad, failure.file)
			goResult := runExternal(t, goExecutable, []string{"lower", source}, directory, nil)
			ktResult := runExternal(t, ktExecutable, []string{"lower", source}, directory, nil)
			if ktResult.status != goResult.status || ktResult.stdout != goResult.stdout || ktResult.stderr != goResult.stderr {
				t.Fatalf("failures differ for %s:\nGo: %d %q %q\nKitten: %d %q %q",
					failure.file, goResult.status, goResult.stdout, goResult.stderr,
					ktResult.status, ktResult.stdout, ktResult.stderr)
			}
		})
	}
}
