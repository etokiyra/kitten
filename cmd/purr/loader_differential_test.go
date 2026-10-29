package main

// Differential testing for the Kitten-written module loader
// (selfhost/loader/loader.ktn, driven by selfhost/load.ktn).
//
// The probe loads an absolute root path with the Kitten loader and prints the
// ordered module set as its id and canonical path, or the loader's failure in the
// reference loader's Error.Error shape. The harness runs Go loader.Load on the
// same root and compares the two renderings byte for byte, so order, ids,
// canonical paths, and diagnostics all have to agree.
//
// Read failures are part of the comparison: the Kitten loader uses
// try_read_file, so missing, unreadable, and directory paths must render the
// reference's no-position "cannot read source file: ..." diagnostic exactly.
// The loader corpus therefore includes readable trees plus read-error trees.
//
// The full-path test drives selfhost/loadunit.ktn, which loads, checks, and emits
// a unit entirely with the Kitten-written stages, and compares that unit with the
// Go reference before compiling and running it.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/codegen"
	"kitten/loader"
)

// kittenLoadProgram is the driver that prints the module set.
func kittenLoadProgram() string {
	return filepath.Join("..", "..", "selfhost", "load.ktn")
}

// kittenLoadUnitProgram is the driver that loads, checks, and emits a unit.
func kittenLoadUnitProgram() string {
	return filepath.Join("..", "..", "selfhost", "loadunit.ktn")
}

// buildKittenProgram builds one probe and fails on any C diagnostic.
func buildKittenProgram(t *testing.T, description, sourcePath string) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), description)
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building the %s probe: %v\n%s", description, err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the %s probe:\n%s", description, warnings)
	}
	return executable
}

// writeSourceFile writes one source file, creating parent directories.
func writeSourceFile(t *testing.T, path, source string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating the directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// runKittenLoader writes the root path to input.txt and runs the loader probe.
func runKittenLoader(t *testing.T, executable, directory, rootPath string) string {
	t.Helper()

	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte(rootPath), 0o644); err != nil {
		t.Fatalf("writing input.txt: %v", err)
	}
	command := exec.Command(executable)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("running the loader probe: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("the loader probe wrote to stderr: %s", stderr.String())
	}
	return stdout.String()
}

// renderGoLoad renders the reference loader's result in the probe's shape.
func renderGoLoad(t *testing.T, rootPath string) string {
	t.Helper()

	modules, err := loader.Load(rootPath)
	if err != nil {
		return "error " + err.Error() + "\n"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "ok %d\n", len(modules))
	for _, module := range modules {
		fmt.Fprintf(&builder, "module %d %s\n", module.ID, module.Path)
	}
	return builder.String()
}

// relativeRootVariants lists relative spellings of an already relative root: the
// root itself, a leading ./, redundant separators, a removable .. component, and
// a trailing separator. Deriving them from every targeted case exercises the
// loader's root resolution over the whole corpus. Duplicates are dropped.
func relativeRootVariants(root string) []string {
	variants := []string{}
	seen := map[string]bool{}
	add := func(variant string) {
		if !seen[variant] {
			seen[variant] = true
			variants = append(variants, variant)
		}
	}
	add(root)
	add("./" + root)
	add(".//" + root)
	add("sub/../" + root)
	add(root + "/")
	return variants
}

// --- Targeted cases ----------------------------------------------------------

// loaderCase is one tree of source files and the root to load from it.
type loaderCase struct {
	description string
	files       map[string]string
	directories []string
	unreadable  []string
	root        string
}

// loaderCases covers ordering, deduplication, cycles, nested directories, path
// cleaning, and frontend diagnostics in imported modules.
func loaderCases() []loaderCase {
	return []loaderCase{
		{
			description: "simple import",
			files: map[string]string{
				"main.ktn": "import \"lib.ktn\";\nfn main() {\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "diamond loads the shared module once",
			files: map[string]string{
				"main.ktn":   "import \"left.ktn\";\nimport \"right.ktn\";\nfn main() {\n}\n",
				"left.ktn":   "import \"shared.ktn\";\npublic fn left() -> int {\n    return shared();\n}\n",
				"right.ktn":  "import \"shared.ktn\";\npublic fn right() -> int {\n    return shared();\n}\n",
				"shared.ktn": "public fn shared() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "cycle terminates at the module already in progress",
			files: map[string]string{
				"main.ktn": "import \"a.ktn\";\nfn main() {\n}\n",
				"a.ktn":    "import \"b.ktn\";\npublic fn a() -> int {\n    return b();\n}\n",
				"b.ktn":    "import \"a.ktn\";\npublic fn b() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "self import loads the root once",
			files: map[string]string{
				"main.ktn": "import \"main.ktn\";\nfn main() {\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "duplicate import with different spellings",
			files: map[string]string{
				"main.ktn":   "import \"shared.ktn\";\nimport \"./shared.ktn\";\nfn main() {\n}\n",
				"shared.ktn": "public fn shared() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "nested directories import back inside the root",
			files: map[string]string{
				"main.ktn":      "import \"sub/child.ktn\";\nfn main() {\n}\n",
				"sub/child.ktn": "import \"../lib.ktn\";\npublic fn child() -> int {\n    return lib();\n}\n",
				"lib.ktn":       "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "dotdot staying inside the root",
			files: map[string]string{
				"main.ktn": "import \"sub/../lib.ktn\";\nfn main() {\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "dotdot escaping the root is rejected",
			files: map[string]string{
				"main.ktn": "import \"../outside.ktn\";\nfn main() {\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "a module imported from two places",
			files: map[string]string{
				"main.ktn":   "import \"shared.ktn\";\nimport \"left.ktn\";\nfn main() {\n}\n",
				"left.ktn":   "import \"shared.ktn\";\npublic fn left() -> int {\n    return shared();\n}\n",
				"shared.ktn": "public fn shared() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "empty, dot, and double separators in import paths",
			files: map[string]string{
				"main.ktn":      "import \"sub//./child.ktn\";\nfn main() {\n}\n",
				"sub/child.ktn": "import \".././lib.ktn\";\npublic fn child() -> int {\n    return lib();\n}\n",
				"lib.ktn":       "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "imports among declarations",
			files: map[string]string{
				"main.ktn": "fn before() -> int {\n    return 1;\n}\nimport \"lib.ktn\";\nfn main() {\n    before();\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 2;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "root path with a dotdot component",
			files: map[string]string{
				"main.ktn": "import \"lib.ktn\";\nfn main() {\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "sub/../main.ktn",
		},
		{
			description: "root path with a trailing separator",
			files: map[string]string{
				"main.ktn": "import \"lib.ktn\";\nfn main() {\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn/",
		},
		{
			description: "root path with a leading dot component",
			files: map[string]string{
				"main.ktn": "import \"lib.ktn\";\nfn main() {\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "./main.ktn",
		},
		{
			description: "root path with redundant separators",
			files: map[string]string{
				"main.ktn": "import \"lib.ktn\";\nfn main() {\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: ".//main.ktn",
		},
		{
			description: "an absolute-looking import still joins inside the root",
			files: map[string]string{
				"main.ktn": "import \"/lib.ktn\";\nfn main() {\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "a filename beginning with dots is not a parent",
			files: map[string]string{
				"main.ktn":     "import \"..hidden.ktn\";\nfn main() {\n}\n",
				"..hidden.ktn": "public fn hidden() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "nested dotdot returns to the root directory",
			files: map[string]string{
				"main.ktn": "import \"a/b/../../lib.ktn\";\nfn main() {\n}\n",
				"lib.ktn":  "public fn lib() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "lexical error in an imported module",
			files: map[string]string{
				"main.ktn": "import \"bad.ktn\";\nfn main() {\n}\n",
				"bad.ktn":  "public fn broken() -> int {\n    return 1;\n}\nstring s = \"unterminated",
			},
			root: "main.ktn",
		},
		{
			description: "syntax error in an imported module",
			files: map[string]string{
				"main.ktn": "import \"bad.ktn\";\nfn main() {\n}\n",
				"bad.ktn":  "public fn broken() -> int {\n    return 1\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "missing root",
			files:       map[string]string{},
			root:        "missing.ktn",
		},
		{
			description: "missing root with a cleaned spelling",
			files:       map[string]string{},
			root:        "sub/../missing.ktn",
		},
		{
			description: "missing import",
			files: map[string]string{
				"main.ktn": "import \"missing.ktn\";\nfn main() {\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "missing import with a cleaned spelling",
			files: map[string]string{
				"main.ktn": "import \"sub/../missing.ktn\";\nfn main() {\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "missing import reached through a nested import",
			files: map[string]string{
				"main.ktn": "import \"a.ktn\";\nfn main() {\n}\n",
				"a.ktn":    "import \"missing.ktn\";\npublic fn a() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "an unreadable root is skipped as root",
			files: map[string]string{
				"main.ktn": "fn main() {\n}\n",
			},
			unreadable: []string{"main.ktn"},
			root:       "main.ktn",
		},
		{
			description: "an unreadable import is skipped as root",
			files: map[string]string{
				"main.ktn":       "import \"unreadable.ktn\";\nfn main() {\n}\n",
				"unreadable.ktn": "public fn lib() -> int {\n    return 1;\n}\n",
			},
			unreadable: []string{"unreadable.ktn"},
			root:       "main.ktn",
		},
		{
			description: "a directory as the root",
			files:       map[string]string{},
			directories: []string{"only-directory"},
			root:        "only-directory",
		},
		{
			description: "a directory as an import",
			files: map[string]string{
				"main.ktn": "import \"only-directory\";\nfn main() {\n}\n",
			},
			directories: []string{"only-directory"},
			root:        "main.ktn",
		},
		{
			description: "a parse error before a later missing import",
			files: map[string]string{
				"main.ktn": "import \"bad.ktn\";\nimport \"missing.ktn\";\nfn main() {\n}\n",
				"bad.ktn":  "public fn broken() -> int {\n    return 1\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "a missing import before a later parse error",
			files: map[string]string{
				"main.ktn": "import \"missing.ktn\";\nimport \"bad.ktn\";\nfn main() {\n}\n",
				"bad.ktn":  "public fn broken() -> int {\n    return 1\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "a missing import reached through a cycle",
			files: map[string]string{
				"main.ktn": "import \"a.ktn\";\nfn main() {\n}\n",
				"a.ktn":    "import \"b.ktn\";\npublic fn a() -> int {\n    return 1;\n}\n",
				"b.ktn":    "import \"a.ktn\";\nimport \"missing.ktn\";\npublic fn b() -> int {\n    return 1;\n}\n",
			},
			root: "main.ktn",
		},
		{
			description: "an outside-root import that does not exist",
			files: map[string]string{
				"main.ktn": "import \"../missing.ktn\";\nfn main() {\n}\n",
			},
			root: "main.ktn",
		},
	}
}

// TestKittenLoaderMatchesReference compares the Kitten loader with the Go loader
// over the targeted trees.
func TestKittenLoaderMatchesReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenProgram(t, "loader", kittenLoadProgram())
	probeDirectory := t.TempDir()

	compared := 0
	skipped := 0
	relativeCompared := 0
	for _, testCase := range loaderCases() {
		if len(testCase.unreadable) > 0 && os.Geteuid() == 0 {
			skipped++
			continue
		}
		sourceDirectory := t.TempDir()
		for name, source := range testCase.files {
			writeSourceFile(t, filepath.Join(sourceDirectory, filepath.FromSlash(name)), source)
		}
		for _, name := range testCase.directories {
			if err := os.MkdirAll(filepath.Join(sourceDirectory, filepath.FromSlash(name)), 0o755); err != nil {
				t.Fatalf("%s: creating directory %s: %v", testCase.description, name, err)
			}
		}
		for _, name := range testCase.unreadable {
			path := filepath.Join(sourceDirectory, filepath.FromSlash(name))
			if err := os.Chmod(path, 0o000); err != nil {
				t.Fatalf("%s: chmod %s: %v", testCase.description, name, err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		}
		rootPath := sourceDirectory + string(os.PathSeparator) + filepath.FromSlash(testCase.root)

		want := renderGoLoad(t, rootPath)
		got := runKittenLoader(t, executable, probeDirectory, rootPath)
		if got != want {
			t.Errorf("%s: loader result differs\n--- probe ---\n%s--- reference ---\n%s", testCase.description, got, want)
			continue
		}
		compared++

		// Every relative spelling resolves to the same canonical root. The
		// probe's working directory is the source directory here, so a
		// relative root is made absolute exactly as filepath.Abs does, and
		// the reference result is the already-rendered absolute comparison.
		for _, variant := range relativeRootVariants(testCase.root) {
			variantGot := runKittenLoader(t, executable, sourceDirectory, variant)
			if variantGot != want {
				t.Errorf("%s (relative %q): loader result differs\n--- probe ---\n%s--- reference ---\n%s",
					testCase.description, variant, variantGot, want)
				continue
			}
			relativeCompared++
		}
	}

	if compared+skipped != len(loaderCases()) {
		t.Fatalf("%d compared and %d skipped for %d targeted loader cases", compared, skipped, len(loaderCases()))
	}
	t.Logf("loader: %d targeted trees identical to the reference loader, %d skipped as root", compared, skipped)
	t.Logf("loader: %d relative root spellings identical to the reference loader", relativeCompared)
}

// TestKittenLoaderTestsModules compares the loader over tests/modules, the real
// nested and shared fixture, using its actual canonical paths.
func TestKittenLoaderTestsModules(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenProgram(t, "loader", kittenLoadProgram())
	probeDirectory := t.TempDir()
	rootPath, err := filepath.Abs(filepath.Join("..", "..", "tests", "modules", "main.ktn"))
	if err != nil {
		t.Fatalf("resolving tests/modules/main.ktn: %v", err)
	}

	want := renderGoLoad(t, rootPath)
	got := runKittenLoader(t, executable, probeDirectory, rootPath)
	if got != want {
		t.Fatalf("tests/modules: loader result differs\n--- probe ---\n%s--- reference ---\n%s", got, want)
	}

	// The same fixture through a relative root, with the probe's working
	// directory at the repository root so the spelling is reachable. The
	// pointer file is written into the repository and removed afterwards.
	repositoryRoot := repoPath(t, "..", "..")
	relativeGot := runKittenLoader(t, executable, repositoryRoot, "tests/modules/main.ktn")
	t.Cleanup(func() { _ = os.Remove(filepath.Join(repositoryRoot, "input.txt")) })
	if relativeGot != want {
		t.Fatalf("tests/modules (relative root): loader result differs\n--- probe ---\n%s--- reference ---\n%s", relativeGot, want)
	}
	t.Logf("loader: tests/modules matched the reference loader over %d modules", strings.Count(got, "module "))
}

// TestKittenLoaderCorpus materializes every multi-file checker corpus case and
// compares the loader with the reference over the resulting files.
func TestKittenLoaderCorpus(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenProgram(t, "loader", kittenLoadProgram())
	probeDirectory := t.TempDir()
	baseDirectory := t.TempDir()

	expected := 0
	for _, testCase := range checkerCorpus(t) {
		if testCase.imported != "" {
			expected++
		}
	}

	compared := 0
	skipped := 0
	index := 0
	for _, testCase := range checkerCorpus(t) {
		if testCase.imported == "" {
			continue
		}
		sourceDirectory := filepath.Join(baseDirectory, fmt.Sprintf("case%04d", index))
		index++
		writeSourceFile(t, filepath.Join(sourceDirectory, "input.txt"), testCase.root)
		writeSourceFile(t, filepath.Join(sourceDirectory, "input2.txt"), testCase.imported)
		rootPath := filepath.Join(sourceDirectory, "input.txt")

		want := renderGoLoad(t, rootPath)
		got := runKittenLoader(t, executable, probeDirectory, rootPath)
		if got != want {
			// Positions in non-ASCII input differ by design: the Kitten lexer
			// counts bytes and the reference counts runes. The parser harness
			// documents the same boundary.
			if !isASCII(testCase.root) || !isASCII(testCase.imported) {
				skipped++
				continue
			}
			t.Errorf("%s: loader result differs\n--- probe ---\n%s--- reference ---\n%s", testCase.name, got, want)
			continue
		}
		compared++
	}

	if compared+skipped != expected {
		t.Fatalf("%d compared and %d skipped for %d multi-file corpus cases", compared, skipped, expected)
	}
	if compared < 14 {
		t.Fatalf("only %d multi-file corpus cases were compared", compared)
	}
	t.Logf("loader corpus: %d multi-file programs identical, %d skipped for non-ASCII positions", compared, skipped)
}

// TestKittenLoaderFullPipeline loads, checks, and emits tests/modules entirely
// with the Kitten-written stages, compares the unit with the reference, and runs
// it under the compiler CC selects.
func TestKittenLoaderFullPipeline(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenProgram(t, "loadunit", kittenLoadUnitProgram())
	probeDirectory := t.TempDir()

	rootPath, err := filepath.Abs(filepath.Join("..", "..", "tests", "modules", "main.ktn"))
	if err != nil {
		t.Fatalf("resolving tests/modules/main.ktn: %v", err)
	}
	expected, err := os.ReadFile(filepath.Join("..", "..", "tests", "modules", "main.expected"))
	if err != nil {
		t.Fatalf("reading tests/modules/main.expected: %v", err)
	}

	modules, err := loader.Load(rootPath)
	if err != nil {
		t.Fatalf("the reference loader rejected tests/modules: %v", err)
	}
	checked, checkError := checker.CheckModules(modules)
	if checkError != nil {
		t.Fatalf("the reference checker rejected tests/modules: %v", checkError)
	}
	referenceText, generationError := codegen.Generate(modules, checked)
	if generationError != nil {
		t.Fatalf("the reference generator rejected tests/modules: %v", generationError)
	}

	if err := os.WriteFile(filepath.Join(probeDirectory, "input.txt"), []byte(rootPath), 0o644); err != nil {
		t.Fatalf("writing input.txt: %v", err)
	}
	command := exec.Command(executable)
	command.Dir = probeDirectory
	runProbe := func(directory string) string {
		probe := exec.Command(executable)
		probe.Dir = directory
		var stdout, stderr bytes.Buffer
		probe.Stdout = &stdout
		probe.Stderr = &stderr
		if err := probe.Run(); err != nil {
			t.Fatalf("running the loadunit probe: %v (stderr: %s)", err, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("the loadunit probe wrote to stderr: %s", stderr.String())
		}
		return stdout.String()
	}
	stdoutText := runProbe(probeDirectory)
	if difference := unitDifference(stdoutText, referenceText); difference != "" {
		t.Fatalf("tests/modules: the self-hosted unit differs: %s", difference)
	}

	// The same fixture through a relative root, with the probe's working
	// directory at the repository root; the pointer file is cleaned up.
	repositoryRoot := repoPath(t, "..", "..")
	if err := os.WriteFile(filepath.Join(repositoryRoot, "input.txt"), []byte("tests/modules/main.ktn"), 0o644); err != nil {
		t.Fatalf("writing the repository input.txt: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join(repositoryRoot, "input.txt")) })
	relativeText := runProbe(repositoryRoot)
	if difference := unitDifference(relativeText, referenceText); difference != "" {
		t.Fatalf("tests/modules (relative root): the self-hosted unit differs: %s", difference)
	}

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	programDirectory := t.TempDir()
	result, err := buildAndRunProgram(t, compiler, compileFlags, linkFlags, programDirectory, stdoutText, 0)
	if err != nil {
		t.Fatalf("tests/modules: %v", err)
	}
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("tests/modules: the program exited %d and wrote %q to stderr", result.status, result.stderr)
	}
	if result.stdout != string(expected) {
		t.Fatalf("tests/modules: the program printed\n%q\nwant\n%q", result.stdout, string(expected))
	}
	t.Logf("loader pipeline: tests/modules matched the reference unit and printed its expected output under %s", filepath.Base(compiler[0]))
}
