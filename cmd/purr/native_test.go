package main

// Pinned tests for the native backend's typed-IR scene, driven by
// selfhost/native.ktn. The probe builds the Phase 10.1 scene corpus (the
// print-hello skeleton target and a growth scene), checks it with
// check_scene, and renders it with the text renderer; its output and every
// validation message are pinned literally here. See selfhost/native/README.md
// for the scene's contract and the spelling rules.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildNativeProbe(t *testing.T) string {
	t.Helper()
	requireToolchain(t)
	executable := filepath.Join(t.TempDir(), "native-probe")
	var diagnostics bytes.Buffer
	if err := buildExecutable(repoPath(t, "..", "..", "selfhost", "native.ktn"), executable, &diagnostics); err != nil {
		t.Fatalf("building the native probe: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C diagnostics while building the native probe:\n%s", warnings)
	}
	return executable
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
// Kitten-written sidecar as well, and the ELFs both pipelines produce must be
// byte-identical: the same selector and ELF writer run through the Kitten
// backend.
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

	goNative := filepath.Join(directory, "hello-go")
	if written := runExternal(t, goExecutable, []string{"elf", goNative}, directory, nil); written.status != 0 {
		t.Fatalf("Go pipeline elf: status %d, stderr %q", written.status, written.stderr)
	}
	ktNative := filepath.Join(directory, "hello-kt")
	if written := runExternal(t, ktExecutable, []string{"elf", ktNative}, directory, nil); written.status != 0 {
		t.Fatalf("Kitten pipeline elf: status %d, stderr %q", written.status, written.stderr)
	}
	goBytes, err := os.ReadFile(goNative)
	if err != nil {
		t.Fatalf("reading the Go-produced ELF: %v", err)
	}
	ktBytes, err := os.ReadFile(ktNative)
	if err != nil {
		t.Fatalf("reading the Kitten-produced ELF: %v", err)
	}
	if string(goBytes) != string(ktBytes) {
		t.Fatalf("the two pipelines' ELFs differ in %d bytes", len(goBytes))
	}
	if err := os.Chmod(ktNative, 0o755); err != nil {
		t.Fatalf("chmod the Kitten-produced ELF: %v", err)
	}
	run := runExternal(t, ktNative, nil, directory, nil)
	if run.status != 0 || run.stdout != "hello\n" || run.stderr != "" {
		t.Fatalf("Kitten-produced native run: %d %q %q", run.status, run.stdout, run.stderr)
	}
}
