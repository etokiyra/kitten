package main

// Tests for the generated purr/embedded.ktn runtime embedding.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

const embeddedPieceLimit = 84
const embeddedChunkLimit = 4096

func embeddedHeaderComment() string {
	return "// GENERATED FILE - DO NOT EDIT.\n" +
		"// Regenerate from the repository root:\n" +
		"//   purr build tools/embed_runtime.ktn -o /tmp/embed_runtime\n" +
		"//   /tmp/embed_runtime <repo-root>\n" +
		"//\n" +
		"// The embedded strings are the MIT-licensed runtime sources from\n" +
		"// runtime/csrc/kitten_runtime.h and runtime/csrc/kitten_runtime.c.\n" +
		"// Their MIT notice is preserved verbatim inside those strings. The rest\n" +
		"// of this file is generator output covered by the project's\n" +
		"// GPL-3.0-or-later toolchain license.\n\n"
}

func embeddedChunkFunction(prefix string, index int, body string, pieces int) string {
	result := fmt.Sprintf("fn %s_chunk_%d() -> string {\n", prefix, index)
	if pieces == 0 {
		return result + "    return \"\";\n}\n\n"
	}
	return result + body + "        \"\";\n}\n\n"
}

func embeddedEscapeUnit(data []byte, index int) (string, int, error) {
	if data[index] == 0 {
		return "", 0, fmt.Errorf("NUL byte at offset %d", index)
	}
	if data[index] < 0x80 {
		switch data[index] {
		case '\n':
			return `\n`, 1, nil
		case '\t':
			return `\t`, 1, nil
		case '"':
			return `\"`, 1, nil
		case '\\':
			return `\\`, 1, nil
		default:
			return string(data[index : index+1]), 1, nil
		}
	}
	r, size := utf8.DecodeRune(data[index:])
	if r == utf8.RuneError && size == 1 {
		return "", 0, fmt.Errorf("invalid UTF-8 at offset %d", index)
	}
	return string(data[index : index+size]), size, nil
}

func embeddedBuildChunks(data []byte, prefix string) (string, int, error) {
	var declarations strings.Builder
	piece := ""
	chunkBody := ""
	chunkDecoded := 0
	chunkPieces := 0
	chunkIndex := 0

	appendPiece := func() {
		if piece == "" {
			return
		}
		if chunkPieces == 0 {
			chunkBody += "    return \"" + piece + "\" +\n"
		} else {
			chunkBody += "        \"" + piece + "\" +\n"
		}
		chunkPieces++
		piece = ""
	}
	appendChunk := func() {
		declarations.WriteString(embeddedChunkFunction(prefix, chunkIndex, chunkBody, chunkPieces))
		chunkIndex++
		chunkBody = ""
		chunkDecoded = 0
		chunkPieces = 0
	}

	index := 0
	for index < len(data) {
		unit, width, err := embeddedEscapeUnit(data, index)
		if err != nil {
			return "", 0, err
		}
		if chunkDecoded > 0 && chunkDecoded+width > embeddedChunkLimit {
			appendPiece()
			appendChunk()
		}
		if piece != "" && len(piece)+len(unit) > embeddedPieceLimit {
			appendPiece()
		}
		piece += unit
		chunkDecoded += width
		index += width
	}
	appendPiece()
	if chunkPieces > 0 || chunkDecoded == 0 {
		appendChunk()
	}
	return declarations.String(), chunkIndex, nil
}

func embeddedPublicFunction(prefix, short string, chunks int) string {
	var result strings.Builder
	result.WriteString(fmt.Sprintf("public fn %s() -> string {\n", prefix))
	current := make([]string, chunks)
	for index := 0; index < chunks; index++ {
		current[index] = fmt.Sprintf("%s_chunk_%d()", prefix, index)
	}
	count := chunks
	counter := 0
	for count > 1 {
		nextCount := (count + 1) / 2
		next := make([]string, nextCount)
		readIndex := 0
		writeIndex := 0
		for readIndex < count {
			if readIndex+1 < count {
				name := fmt.Sprintf("%s%d", short, counter)
				result.WriteString(fmt.Sprintf("    string %s = %s + %s;\n", name, current[readIndex], current[readIndex+1]))
				next[writeIndex] = name
				readIndex += 2
				counter++
			} else {
				next[writeIndex] = current[readIndex]
				readIndex++
			}
			writeIndex++
		}
		current = next
		count = nextCount
	}
	result.WriteString(fmt.Sprintf("    return %s;\n}\n", current[0]))
	return result.String()
}

func referenceEmbeddedRuntime(header, source []byte) ([]byte, error) {
	headerDeclarations, headerChunks, err := embeddedBuildChunks(header, "embedded_runtime_header")
	if err != nil {
		return nil, err
	}
	sourceDeclarations, sourceChunks, err := embeddedBuildChunks(source, "embedded_runtime_source")
	if err != nil {
		return nil, err
	}
	var result strings.Builder
	result.WriteString(embeddedHeaderComment())
	result.WriteString(headerDeclarations)
	result.WriteString(embeddedPublicFunction("embedded_runtime_header", "h", headerChunks))
	result.WriteString("\n")
	result.WriteString(sourceDeclarations)
	result.WriteString(embeddedPublicFunction("embedded_runtime_source", "s", sourceChunks))
	return []byte(result.String()), nil
}

func readEmbeddedInputs(t *testing.T) ([]byte, []byte) {
	t.Helper()
	header, err := os.ReadFile(repoPath(t, "..", "..", "runtime", "csrc", runtimeHeaderFile))
	if err != nil {
		t.Fatalf("reading runtime header: %v", err)
	}
	source, err := os.ReadFile(repoPath(t, "..", "..", "runtime", "csrc", runtimeSourceFile))
	if err != nil {
		t.Fatalf("reading runtime source: %v", err)
	}
	return header, source
}

const runtimeHeaderFile = "kitten_runtime.h"
const runtimeSourceFile = "kitten_runtime.c"

func TestEmbeddedRuntimeReferenceMatchesCommitted(t *testing.T) {
	header, source := readEmbeddedInputs(t)
	want, err := referenceEmbeddedRuntime(header, source)
	if err != nil {
		t.Fatalf("reference generation: %v", err)
	}
	got, err := os.ReadFile(repoPath(t, "..", "..", "purr", "embedded.ktn"))
	if err != nil {
		t.Fatalf("reading committed embedded.ktn: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("committed purr/embedded.ktn differs from the reference: %s", unitDifference(string(got), string(want)))
	}
}

func TestEmbeddedRuntimeNotIgnored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := repoPath(t, "..", "..")
	command := exec.Command("git", "check-ignore", "-q", "purr/embedded.ktn")
	command.Dir = root
	if err := command.Run(); err == nil {
		t.Fatalf("git check-ignore reports purr/embedded.ktn as ignored")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("git check-ignore failed unexpectedly: %v", err)
	}
	command = exec.Command("git", "check-ignore", "-v", "purr/embedded.ktn")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			t.Fatalf("git check-ignore -v failed unexpectedly: %v (%s)", err, output)
		}
	}
	if len(output) != 0 {
		t.Fatalf("git check-ignore -v printed a pattern for purr/embedded.ktn:\n%s", output)
	}
}

const embeddedRoundTripSource = `import "purr/embedded.ktn";

fn write_checked(string path, string text) {
    match (try_write_file(path, text)) {
        Ok { }
        Failed(message) { eprint(message); exit(1); }
    }
}

fn main() {
    string output = argv()[1];
    write_checked(output + "/kitten_runtime.h", embedded_runtime_header());
    write_checked(output + "/kitten_runtime.c", embedded_runtime_source());
}
`

func buildGeneratorWithGo(t *testing.T) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "embed_runtime_go")
	var diagnostics bytes.Buffer
	if err := buildExecutable(repoPath(t, "..", "..", "tools", "embed_runtime.ktn"), output, &diagnostics); err != nil {
		t.Fatalf("building the Go-pipeline generator: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C diagnostics while building the Go-pipeline generator:\n%s", warnings)
	}
	return output
}

func buildGeneratorWithKitten(t *testing.T, stage1 string) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "embed_runtime_kitten")
	result := runExternal(t, wrapperPath(t), []string{
		"build", repoPath(t, "..", "..", "tools", "embed_runtime.ktn"), "-o", output,
	}, repoPath(t, "..", ".."), wrapperEnvironment(stage1, t))
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("building the Kitten-pipeline generator: status %d, stderr %q", result.status, result.stderr)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("Kitten-pipeline generator was not produced: %v", err)
	}
	return output
}

func prepareEmbeddedRoot(t *testing.T, header, source []byte) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "runtime", "csrc")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("creating runtime/csrc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, runtimeHeaderFile), header, 0o644); err != nil {
		t.Fatalf("writing runtime header: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, runtimeSourceFile), source, 0o644); err != nil {
		t.Fatalf("writing runtime source: %v", err)
	}
	return root
}

func runEmbeddedGenerator(t *testing.T, generator, root string) {
	t.Helper()
	result := runExternal(t, generator, []string{root}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("generator exited %d, stderr %q", result.status, result.stderr)
	}
}

func readGeneratedEmbedded(t *testing.T, root string) []byte {
	t.Helper()
	output, err := os.ReadFile(filepath.Join(root, "purr", "embedded.ktn"))
	if err != nil {
		t.Fatalf("reading generated embedded.ktn: %v", err)
	}
	return output
}

func checkEmbeddedShape(t *testing.T, text []byte) {
	t.Helper()
	if !utf8.Valid(text) {
		t.Fatalf("generated file is not valid UTF-8")
	}
	if !bytes.HasSuffix(text, []byte("\n")) || bytes.HasSuffix(text, []byte("\n\n")) {
		t.Fatalf("generated file does not end with exactly one newline")
	}
	for index, line := range strings.Split(strings.TrimSuffix(string(text), "\n"), "\n") {
		if len(line) > 100 {
			t.Fatalf("generated line %d has %d columns: %q", index+1, len(line), line)
		}
	}
}

func buildRoundTripWithGo(t *testing.T, root, sourcePath string) string {
	t.Helper()
	output := filepath.Join(root, "roundtrip_go")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, output, &diagnostics); err != nil {
		t.Fatalf("building round-trip program with Go pipeline: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C diagnostics for Go round-trip program:\n%s", warnings)
	}
	return output
}

func buildRoundTripWithKitten(t *testing.T, stage1, root, sourcePath string) string {
	t.Helper()
	output := filepath.Join(root, "roundtrip_kitten")
	result := runExternal(t, wrapperPath(t), []string{"build", sourcePath, "-o", output}, root, wrapperEnvironment(stage1, t))
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("building round-trip program with Kitten pipeline: status %d, stderr %q", result.status, result.stderr)
	}
	return output
}

func runRoundTrip(t *testing.T, executable, root string) ([]byte, []byte) {
	t.Helper()
	outputDirectory, err := os.MkdirTemp(root, "out-")
	if err != nil {
		t.Fatalf("creating round-trip output directory: %v", err)
	}
	result := runExternal(t, executable, []string{outputDirectory}, root, nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("round-trip exited %d, stderr %q", result.status, result.stderr)
	}
	header, err := os.ReadFile(filepath.Join(outputDirectory, runtimeHeaderFile))
	if err != nil {
		t.Fatalf("reading round-trip header: %v", err)
	}
	source, err := os.ReadFile(filepath.Join(outputDirectory, runtimeSourceFile))
	if err != nil {
		t.Fatalf("reading round-trip source: %v", err)
	}
	return header, source
}

func roundTripCommitted(t *testing.T, stage1 string, committed []byte) {
	t.Helper()
	root := prepareEmbeddedRoot(t, nil, nil)
	if err := os.RemoveAll(filepath.Join(root, "runtime")); err != nil {
		t.Fatalf("removing placeholder runtime: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "purr"), 0o755); err != nil {
		t.Fatalf("creating purr directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "purr", "embedded.ktn"), committed, 0o644); err != nil {
		t.Fatalf("writing embedded.ktn: %v", err)
	}
	sourcePath := filepath.Join(root, "main.ktn")
	if err := os.WriteFile(sourcePath, []byte(embeddedRoundTripSource), 0o644); err != nil {
		t.Fatalf("writing round-trip source: %v", err)
	}
	for pipeline, executable := range map[string]string{
		"Go":     buildRoundTripWithGo(t, root, sourcePath),
		"Kitten": buildRoundTripWithKitten(t, stage1, root, sourcePath),
	} {
		header, source := runRoundTrip(t, executable, root)
		wantHeader, err := os.ReadFile(repoPath(t, "..", "..", "runtime", "csrc", runtimeHeaderFile))
		if err != nil {
			t.Fatalf("reading expected runtime header: %v", err)
		}
		wantSource, err := os.ReadFile(repoPath(t, "..", "..", "runtime", "csrc", runtimeSourceFile))
		if err != nil {
			t.Fatalf("reading expected runtime source: %v", err)
		}
		if !bytes.Equal(header, wantHeader) {
			t.Errorf("%s pipeline round-trip header differs", pipeline)
		}
		if !bytes.Equal(source, wantSource) {
			t.Errorf("%s pipeline round-trip source differs", pipeline)
		}
	}
}

func TestEmbeddedRuntimeGeneration(t *testing.T) {
	requireToolchain(t)
	header, source := readEmbeddedInputs(t)
	want, err := referenceEmbeddedRuntime(header, source)
	if err != nil {
		t.Fatalf("reference generation: %v", err)
	}
	committed, err := os.ReadFile(repoPath(t, "..", "..", "purr", "embedded.ktn"))
	if err != nil {
		t.Fatalf("reading committed embedded.ktn: %v", err)
	}
	if !bytes.Equal(committed, want) {
		t.Fatalf("committed embedded.ktn differs from the independent reference")
	}
	checkEmbeddedShape(t, committed)

	stage1 := buildStage1(t)
	goGenerator := buildGeneratorWithGo(t)
	kittenGenerator := buildGeneratorWithKitten(t, stage1)

	goRoot := prepareEmbeddedRoot(t, header, source)
	kittenRoot := prepareEmbeddedRoot(t, header, source)
	runEmbeddedGenerator(t, goGenerator, goRoot)
	runEmbeddedGenerator(t, kittenGenerator, kittenRoot)
	goOutput := readGeneratedEmbedded(t, goRoot)
	kittenOutput := readGeneratedEmbedded(t, kittenRoot)
	if !bytes.Equal(goOutput, want) {
		t.Errorf("Go-pipeline generation differs from the independent reference")
	}
	if !bytes.Equal(kittenOutput, want) {
		t.Errorf("Kitten-pipeline generation differs from the independent reference")
	}
	if !bytes.Equal(goOutput, kittenOutput) {
		t.Errorf("Go and Kitten generation differ")
	}
	checkEmbeddedShape(t, goOutput)

	roundTripCommitted(t, stage1, committed)
}

func TestEmbeddedRuntimeFixture(t *testing.T) {
	requireToolchain(t)
	committed, err := os.ReadFile(repoPath(t, "..", "..", "purr", "embedded.ktn"))
	if err != nil {
		t.Fatalf("reading embedded.ktn: %v", err)
	}
	root := prepareEmbeddedRoot(t, nil, nil)
	if err := os.RemoveAll(filepath.Join(root, "runtime")); err != nil {
		t.Fatalf("removing placeholder runtime: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "purr"), 0o755); err != nil {
		t.Fatalf("creating purr directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "purr", "embedded.ktn"), committed, 0o644); err != nil {
		t.Fatalf("writing embedded.ktn: %v", err)
	}
	sourcePath := filepath.Join(root, "main.ktn")
	if err := os.WriteFile(sourcePath, []byte(embeddedRoundTripSource), 0o644); err != nil {
		t.Fatalf("writing round-trip source: %v", err)
	}
	executable := buildRoundTripWithGo(t, root, sourcePath)
	header, source := runRoundTrip(t, executable, root)
	if err := os.WriteFile(filepath.Join(root, runtimeHeaderFile), header, 0o644); err != nil {
		t.Fatalf("writing embedded header: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, runtimeSourceFile), source, 0o644); err != nil {
		t.Fatalf("writing embedded source: %v", err)
	}

	fixtureSource, err := os.ReadFile(repoPath(t, "..", "..", "tests", "hello.ktn"))
	if err != nil {
		t.Fatalf("reading hello fixture: %v", err)
	}
	expected, err := os.ReadFile(repoPath(t, "..", "..", "tests", "hello.expected"))
	if err != nil {
		t.Fatalf("reading hello expected output: %v", err)
	}
	_, _, programC, ok := checkProgram(t, "embedded runtime fixture", string(fixtureSource), "")
	if !ok {
		return
	}
	programPath := filepath.Join(root, "program.c")
	if err := os.WriteFile(programPath, []byte(programC), 0o644); err != nil {
		t.Fatalf("writing fixture C: %v", err)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	arguments := append([]string{}, compiler[1:]...)
	arguments = append(arguments, "-std=c11", "-Wall", "-Wextra")
	arguments = append(arguments, compileFlags...)
	arguments = append(arguments, "-o", "program", "program.c", runtimeSourceFile)
	arguments = append(arguments, linkFlags...)
	build := exec.Command(compiler[0], arguments...)
	build.Dir = root
	var diagnostics bytes.Buffer
	build.Stdout = &diagnostics
	build.Stderr = &diagnostics
	if err := build.Run(); err != nil {
		t.Fatalf("compiling fixture against embedded runtime: %v\n%s", err, diagnostics.String())
	}
	if output := strings.TrimSpace(diagnostics.String()); output != "" {
		t.Fatalf("fixture compilation diagnostics:\n%s", output)
	}
	result := runExternal(t, filepath.Join(root, "program"), nil, root, nil)
	if result.status != 0 || result.stderr != "" || result.stdout != string(expected) {
		t.Fatalf("fixture against embedded runtime: status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
}

func prepareFailureRoot(t *testing.T, header, source []byte) (string, string, []byte) {
	t.Helper()
	root := prepareEmbeddedRoot(t, header, source)
	directory := filepath.Join(root, "purr")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("creating purr directory: %v", err)
	}
	outputPath := filepath.Join(directory, "embedded.ktn")
	sentinel := []byte("sentinel output\n")
	if err := os.WriteFile(outputPath, sentinel, 0o644); err != nil {
		t.Fatalf("writing sentinel output: %v", err)
	}
	return root, outputPath, sentinel
}

func assertGeneratorFailure(t *testing.T, generator, root, outputPath, want string) {
	t.Helper()
	result := runExternal(t, generator, []string{root}, t.TempDir(), nil)
	if result.status != 1 {
		t.Fatalf("generator status = %d, want 1 (stderr %q)", result.status, result.stderr)
	}
	if result.stderr == "" {
		t.Fatalf("generator wrote no failure message")
	}
	if want != "" && !strings.Contains(result.stderr, want) {
		t.Errorf("generator stderr %q does not contain %q", result.stderr, want)
	}
	if outputPath != "" {
		got, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("reading untouched output: %v", err)
		}
		if string(got) != "sentinel output\n" {
			t.Errorf("output was changed on failure: %q", got)
		}
	}
}

func TestEmbeddedRuntimeFailures(t *testing.T) {
	requireToolchain(t)
	header, source := readEmbeddedInputs(t)
	stage1 := buildStage1(t)
	generators := map[string]string{
		"Go":     buildGeneratorWithGo(t),
		"Kitten": buildGeneratorWithKitten(t, stage1),
	}

	t.Run("missing repo root", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "not-there")
		for name, generator := range generators {
			result := runExternal(t, generator, []string{missing}, t.TempDir(), nil)
			if result.status != 1 || result.stderr == "" {
				t.Errorf("%s: status %d, stderr %q; want status 1 and a message", name, result.status, result.stderr)
			}
		}
	})

	t.Run("missing runtime file", func(t *testing.T) {
		for name, generator := range generators {
			root, outputPath, _ := prepareFailureRoot(t, header, source)
			if err := os.Remove(filepath.Join(root, "runtime", "csrc", runtimeSourceFile)); err != nil {
				t.Fatalf("%s: removing runtime source: %v", name, err)
			}
			assertGeneratorFailure(t, generator, root, outputPath, runtimeSourceFile)
		}
	})

	t.Run("NUL input", func(t *testing.T) {
		nulHeader := append(append([]byte(nil), header[:100]...), 0)
		nulHeader = append(nulHeader, header[100:]...)
		for name, generator := range generators {
			root, outputPath, _ := prepareFailureRoot(t, nulHeader, source)
			assertGeneratorFailure(t, generator, root, outputPath, runtimeHeaderFile)
			got, err := os.ReadFile(filepath.Join(root, "runtime", "csrc", runtimeHeaderFile))
			if err != nil {
				t.Fatalf("%s: reading NUL input: %v", name, err)
			}
			if len(got) != len(header)+1 {
				t.Errorf("%s: NUL fixture length = %d, want %d", name, len(got), len(header)+1)
			}
		}
	})

	t.Run("invalid UTF-8 input", func(t *testing.T) {
		bad := append(append([]byte(nil), header[:100]...), 0xFF)
		bad = append(bad, header[100:]...)
		for _, generator := range generators {
			root, outputPath, _ := prepareFailureRoot(t, bad, source)
			assertGeneratorFailure(t, generator, root, outputPath, runtimeHeaderFile)
		}
	})

	t.Run("wrong argument count", func(t *testing.T) {
		for name, generator := range generators {
			result := runExternal(t, generator, nil, t.TempDir(), nil)
			if result.status != 2 {
				t.Errorf("%s: status = %d, want 2", name, result.status)
			}
			if !strings.Contains(result.stderr, "usage: embed_runtime <repo-root>") {
				t.Errorf("%s: stderr %q does not contain usage", name, result.stderr)
			}
		}
	})

	t.Run("unwritable output directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission checks are not meaningful as root")
		}
		for name, generator := range generators {
			root := prepareEmbeddedRoot(t, header, source)
			directory := filepath.Join(root, "purr")
			if err := os.Mkdir(directory, 0o500); err != nil {
				t.Fatalf("%s: creating unwritable purr directory: %v", name, err)
			}
			result := runExternal(t, generator, []string{root}, t.TempDir(), nil)
			if err := os.Chmod(directory, 0o755); err != nil {
				t.Fatalf("%s: restoring purr directory permissions: %v", name, err)
			}
			if result.status != 1 || result.stderr == "" {
				t.Errorf("%s: status %d, stderr %q; want a failure", name, result.status, result.stderr)
			}
		}
	})

	t.Run("existing output is overwritten", func(t *testing.T) {
		want, err := referenceEmbeddedRuntime(header, source)
		if err != nil {
			t.Fatalf("reference generation: %v", err)
		}
		for name, generator := range generators {
			root, outputPath, _ := prepareFailureRoot(t, header, source)
			runEmbeddedGenerator(t, generator, root)
			got, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("%s: reading overwritten output: %v", name, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s: overwritten output differs from the reference", name)
			}
		}
	})
}

func generateSynthetic(t *testing.T, generators map[string]string, header, source []byte) []byte {
	t.Helper()
	want, err := referenceEmbeddedRuntime(header, source)
	if err != nil {
		t.Fatalf("reference generation: %v", err)
	}
	for name, generator := range generators {
		root := prepareEmbeddedRoot(t, header, source)
		runEmbeddedGenerator(t, generator, root)
		got := readGeneratedEmbedded(t, root)
		checkEmbeddedShape(t, got)
		if !bytes.Equal(got, want) {
			t.Fatalf("%s generation differs from the reference for synthetic input", name)
		}
	}
	return want
}

func roundTripSynthetic(t *testing.T, stage1 string, embedded, header, source []byte) {
	t.Helper()
	root := prepareEmbeddedRoot(t, nil, nil)
	if err := os.RemoveAll(filepath.Join(root, "runtime")); err != nil {
		t.Fatalf("removing placeholder runtime: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "purr"), 0o755); err != nil {
		t.Fatalf("creating purr directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "purr", "embedded.ktn"), embedded, 0o644); err != nil {
		t.Fatalf("writing synthetic embedded.ktn: %v", err)
	}
	sourcePath := filepath.Join(root, "main.ktn")
	if err := os.WriteFile(sourcePath, []byte(embeddedRoundTripSource), 0o644); err != nil {
		t.Fatalf("writing synthetic round-trip source: %v", err)
	}
	for name, executable := range map[string]string{
		"Go":     buildRoundTripWithGo(t, root, sourcePath),
		"Kitten": buildRoundTripWithKitten(t, stage1, root, sourcePath),
	} {
		gotHeader, gotSource := runRoundTrip(t, executable, root)
		if !bytes.Equal(gotHeader, header) {
			t.Errorf("%s synthetic header round-trip differs", name)
		}
		if !bytes.Equal(gotSource, source) {
			t.Errorf("%s synthetic source round-trip differs", name)
		}
	}
}

func TestEmbeddedRuntimeStress(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	generators := map[string]string{
		"Go":     buildGeneratorWithGo(t),
		"Kitten": buildGeneratorWithKitten(t, stage1),
	}

	cases := []struct {
		name   string
		header []byte
		source []byte
	}{
		{
			name:   "every escape",
			header: []byte("quote \" backslash \\ newline\n tab\t cr\r control\x01 del\x7f\n"),
			source: []byte("source with \"quotes\" and \\slashes\\\n"),
		},
		{
			name:   "valid non-ASCII",
			header: []byte("caf\u00e9 \U0001f600\n"),
			source: []byte("\u00e9\u00e8\u00ea\n"),
		},
		{
			name:   "long line",
			header: append(bytes.Repeat([]byte("a"), 1200), '\n'),
			source: []byte("long-lines\n"),
		},
		{
			name:   "line before split",
			header: append(bytes.Repeat([]byte("a"), 84), '\n'),
			source: append(bytes.Repeat([]byte("b"), 83), '\n'),
		},
		{
			name:   "empty files",
			header: []byte{},
			source: []byte{},
		},
		{
			name:   "no trailing newline",
			header: []byte("header without newline"),
			source: []byte("source without newline"),
		},
		{
			name:   "exactly one chunk",
			header: bytes.Repeat([]byte("a"), 4096),
			source: []byte("one chunk\n"),
		},
		{
			name:   "two chunks",
			header: append(bytes.Repeat([]byte("a"), 4097), '\n'),
			source: []byte("two chunks\n"),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			embedded := generateSynthetic(t, generators, testCase.header, testCase.source)
			roundTripSynthetic(t, stage1, embedded, testCase.header, testCase.source)
		})
	}
}

func TestEmbeddedRuntimeSanitizer(t *testing.T) {
	requireToolchain(t)
	header, source := readEmbeddedInputs(t)
	want, err := referenceEmbeddedRuntime(header, source)
	if err != nil {
		t.Fatalf("reference generation: %v", err)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")

	generatorSource, err := os.ReadFile(repoPath(t, "..", "..", "tools", "embed_runtime.ktn"))
	if err != nil {
		t.Fatalf("reading generator source: %v", err)
	}
	_, _, generatorC, ok := checkProgram(t, "embedded generator sanitizer", string(generatorSource), "")
	if !ok {
		return
	}
	generator, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, generatorC)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	root := prepareEmbeddedRoot(t, header, source)
	result := runExternal(t, generator, []string{root}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("sanitized generator exited %d, stderr %q", result.status, result.stderr)
	}
	generated := readGeneratedEmbedded(t, root)
	if !bytes.Equal(generated, want) {
		t.Errorf("sanitized generator output differs from the reference")
	}

	committed, err := os.ReadFile(repoPath(t, "..", "..", "purr", "embedded.ktn"))
	if err != nil {
		t.Fatalf("reading embedded.ktn: %v", err)
	}
	_, _, roundTripC, ok := checkProgram(t, "embedded round-trip sanitizer", embeddedRoundTripSource, string(committed))
	if !ok {
		return
	}
	roundTrip, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, roundTripC)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	roundTripRoot := prepareEmbeddedRoot(t, header, source)
	gotHeader, gotSource := runRoundTrip(t, roundTrip, roundTripRoot)
	if !bytes.Equal(gotHeader, header) || !bytes.Equal(gotSource, source) {
		t.Errorf("sanitized round-trip output differs from the runtime sources")
	}
}
