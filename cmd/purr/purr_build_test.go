package main

// Tests for purr-kt build.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kitten/checker"
	"kitten/codegen"
	"kitten/loader"
)

func writeBuildFixture(t *testing.T, directory string) string {
	t.Helper()
	path := filepath.Join(directory, "hello.ktn")
	source := "fn main() {\n    print(\"Hello, world!\");\n}\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("writing build fixture: %v", err)
	}
	return path
}

func runBuildCommand(t *testing.T, executable, directory string, arguments []string, environment []string) programResult {
	t.Helper()
	return runExternal(t, executable, arguments, directory, environment)
}

func normalizeBuildUsage(t *testing.T, text string) string {
	t.Helper()
	// The Kitten usage deliberately adds the new/help lines to the Go block.
	text = strings.ReplaceAll(text, purrUsageText(), "USAGE")
	text = strings.ReplaceAll(text, goUsageText(t), "USAGE")
	return text
}

func goUsageText(t *testing.T) string {
	t.Helper()
	// Keep this independent of the real Go binary's current text: it is the
	// baseline block the 9.8a tests already diffed.
	return "Kitten compiler\n\nUsage:\n" +
		"  purr build <file.ktn> [-o <output>]   Compile a Kitten source file\n" +
		"  purr run <file.ktn>                   Compile and run a Kitten source file\n\n" +
		"Options:\n" +
		"  -o, --output <path>   Write the executable to <path> (build only)\n\n" +
		"The C compiler is taken from CC, or discovered as cc, gcc, or clang.\n" +
		"Kitten programs are linked against the Boehm garbage collector (libgc)."
}

func TestKittenPurrBuildArguments(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	goPurr := buildGoPurr(t)
	purrs := map[string]string{
		"Go":     buildPurrKTWithGo(t),
		"Kitten": buildPurrKTWithKitten(t, stage1),
	}
	directory := t.TempDir()
	source := writeBuildFixture(t, directory)

	errorCases := []struct {
		name      string
		arguments []string
		usage     bool
	}{
		{"no arguments", []string{"build"}, true},
		{"missing output value", []string{"build", "-o"}, true},
		{"missing output value long", []string{"build", "--output"}, true},
		{"unknown option", []string{"build", "-x"}, true},
		{"unknown option long", []string{"build", "--wat"}, true},
		{"two sources", []string{"build", "one.ktn", "two.ktn"}, true},
		{"extension", []string{"build", "hello"}, false},
		{"extension uppercase", []string{"build", "hello.KTN"}, false},
	}
	for _, testCase := range errorCases {
		expectedPrefix := "purr build: "
		expectedSuffix := ""
		switch testCase.name {
		case "no arguments":
			expectedSuffix = "missing source file\n"
		case "missing output value":
			expectedSuffix = "-o requires a path\n"
		case "missing output value long":
			expectedSuffix = "--output requires a path\n"
		case "unknown option":
			expectedSuffix = "unknown option \"-x\"\n"
		case "unknown option long":
			expectedSuffix = "unknown option \"--wat\"\n"
		case "two sources":
			expectedSuffix = "expected one source file, got 2\n"
		case "extension":
			expectedPrefix = "purr build: \"hello\" is not a Kitten source file (expected a .ktn file)\n"
			expectedSuffix = ""
		case "extension uppercase":
			expectedPrefix = "purr build: \"hello.KTN\" is not a Kitten source file (expected a .ktn file)\n"
			expectedSuffix = ""
		}
		goResult := runBuildCommand(t, goPurr, directory, testCase.arguments, nil)
		for pipeline, executable := range purrs {
			before := newSnapshot(t, directory)
			result := runBuildCommand(t, executable, directory, testCase.arguments, nil)
			if normalizeBuildUsage(t, goResult.stderr) != normalizeBuildUsage(t, result.stderr) ||
				goResult.stdout != result.stdout || goResult.status != result.status {
				t.Errorf("%s %s differs from Go: Go %d/%q/%q, Kitten %d/%q/%q",
					pipeline, testCase.name, goResult.status, goResult.stdout, goResult.stderr,
					result.status, result.stdout, result.stderr)
			}
			if result.status != 2 || result.stdout != "" {
				t.Errorf("%s %s: status %d, stdout %q", pipeline, testCase.name, result.status, result.stdout)
			}
			if testCase.usage {
				want := expectedPrefix + expectedSuffix + "\n" + purrUsageText() + "\n"
				if result.stderr != want {
					t.Errorf("%s %s: stderr %q, want %q", pipeline, testCase.name, result.stderr, want)
				}
			} else if result.stderr != expectedPrefix {
				t.Errorf("%s %s: stderr %q, want %q", pipeline, testCase.name, result.stderr, expectedPrefix)
			}
			if !sameSnapshot(before, newSnapshot(t, directory)) {
				t.Errorf("%s %s touched the filesystem", pipeline, testCase.name)
			}
		}
	}

	successCases := [][]string{
		{"build", source},
		{"build", source, "-o", "out1"},
		{"build", "-o", "out2", source},
		{"build", "--output", "out3", source},
		{"build", "-o=out4", source},
		{"build", "--output=out5", source},
		{"build", "--", source},
	}
	for _, arguments := range successCases {
		goResult := runBuildCommand(t, goPurr, directory, arguments, nil)
		for pipeline, executable := range purrs {
			result := runBuildCommand(t, executable, directory, arguments, nil)
			if result.status != 0 || result.stderr != "" {
				t.Errorf("%s build %v: status %d, stderr %q", pipeline, arguments, result.status, result.stderr)
			}
			if arguments[1] == source && len(arguments) == 2 {
				if result.stdout != "built hello\n" {
					t.Errorf("%s default output: stdout %q", pipeline, result.stdout)
				}
			}
			if result.status != goResult.status || result.stdout != goResult.stdout || result.stderr != goResult.stderr {
				t.Errorf("%s build %v differs from Go: Go %d/%q/%q, Kitten %d/%q/%q",
					pipeline, arguments, goResult.status, goResult.stdout, goResult.stderr,
					result.status, result.stdout, result.stderr)
			}
		}
	}
}

func TestKittenPurrBuildRealFixtures(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	goPurr := buildGoPurr(t)
	purrs := map[string]string{
		"Go":     buildPurrKTWithGo(t),
		"Kitten": buildPurrKTWithKitten(t, stage1),
	}
	roots := []struct{ name, path, expected string }{
		{"hello", repoPath(t, "..", "..", "tests", "hello.ktn"), "hello"},
		{"fizzbuzz", repoPath(t, "..", "..", "tests", "fizzbuzz.ktn"), "fizzbuzz"},
		{"math", repoPath(t, "..", "..", "tests", "math.ktn"), "math"},
		{"room", repoPath(t, "..", "..", "tests", "room.ktn"), "room"},
		{"strings", repoPath(t, "..", "..", "tests", "strings.ktn"), "strings"},
		{"arrays", repoPath(t, "..", "..", "tests", "arrays.ktn"), "arrays"},
		{"variants", repoPath(t, "..", "..", "tests", "variants.ktn"), "variants"},
		{"maps", repoPath(t, "..", "..", "tests", "maps.ktn"), "maps"},
		{"lexer_proof", repoPath(t, "..", "..", "tests", "lexer_proof.ktn"), "lexer_proof"},
		{"modules", repoPath(t, "..", "..", "tests", "modules", "main.ktn"), "modules"},
	}
	for _, root := range roots {
		goOutput := filepath.Join(t.TempDir(), "program-go")
		if result := runBuildCommand(t, goPurr, t.TempDir(), []string{"build", root.path, "-o", goOutput}, nil); result.status != 0 || result.stderr != "" {
			t.Fatalf("Go build %s: %d %q", root.name, result.status, result.stderr)
		}
		goRun := runExternal(t, goOutput, nil, t.TempDir(), nil)
		for pipeline, executable := range purrs {
			output := filepath.Join(t.TempDir(), "program-"+pipeline)
			result := runBuildCommand(t, executable, t.TempDir(), []string{"build", root.path, "-o", output}, nil)
			if result.status != 0 || result.stderr != "" {
				t.Errorf("%s build %s: status %d, stderr %q", pipeline, root.name, result.status, result.stderr)
				continue
			}
			run := runExternal(t, output, nil, t.TempDir(), nil)
			if run.status != goRun.status || run.stdout != goRun.stdout || run.stderr != goRun.stderr {
				t.Errorf("%s %s run differs: %d/%q/%q vs Go %d/%q/%q",
					pipeline, root.name, run.status, run.stdout, run.stderr,
					goRun.status, goRun.stdout, goRun.stderr)
			}
		}
	}
}

func TestKittenPurrBuildDefaultAndPaths(t *testing.T) {
	requireToolchain(t)
	goPurr := buildGoPurr(t)
	kt := buildPurrKTWithGo(t)
	root := t.TempDir()
	writeBuildFixture(t, root)

	cases := []struct {
		name      string
		arguments []string
		output    string
	}{
		{"default", []string{"build", "hello.ktn"}, "hello"},
		{"relative output dir", []string{"build", "hello.ktn", "-o", "out/app"}, "out/app"},
		{"absolute output", []string{"build", "hello.ktn", "-o", filepath.Join(root, "absolute")}, filepath.Join(root, "absolute")},
		{"source dot dot", []string{"build", "sub/../hello.ktn"}, "hello"},
	}
	if err := os.Mkdir(filepath.Join(root, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range cases {
		for pipeline, executable := range map[string]string{"Go": goPurr, "Kitten": kt} {
			result := runBuildCommand(t, executable, root, testCase.arguments, nil)
			if result.status != 0 || result.stderr != "" {
				t.Errorf("%s %s: status %d, stderr %q", pipeline, testCase.name, result.status, result.stderr)
			}
			if result.stdout != "built "+testCase.output+"\n" {
				t.Errorf("%s %s: stdout %q", pipeline, testCase.name, result.stdout)
			}
		}
	}
}

func TestKittenPurrBuildDiagnostics(t *testing.T) {
	requireToolchain(t)
	goPurr := buildGoPurr(t)
	kt := buildPurrKTWithGo(t)
	cases := []struct {
		name string
		root string
	}{
		{"type error", "fn main() {\n    int x = \"text\";\n}\n"},
		{"syntax error", "fn main( {\n}\n"},
		{"missing main", "fn helper() {\n}\n"},
	}
	for _, testCase := range cases {
		directory := t.TempDir()
		path := filepath.Join(directory, "bad.ktn")
		if err := os.WriteFile(path, []byte(testCase.root), 0o644); err != nil {
			t.Fatal(err)
		}
		goResult := runBuildCommand(t, goPurr, directory, []string{"build", path}, nil)
		ktResult := runBuildCommand(t, kt, directory, []string{"build", path}, nil)
		if goResult.status != 1 || ktResult.status != 1 {
			t.Errorf("%s: status Go %d, Kitten %d", testCase.name, goResult.status, ktResult.status)
		}
		if goResult.stdout != ktResult.stdout || goResult.stderr != ktResult.stderr {
			t.Errorf("%s diagnostics differ:\nGo %q\nKitten %q", testCase.name, goResult.stderr, ktResult.stderr)
		}
	}

	t.Run("missing source and directory", func(t *testing.T) {
		directory := t.TempDir()
		missing := filepath.Join(directory, "missing.ktn")
		asDirectory := filepath.Join(directory, "directory.ktn")
		if err := os.Mkdir(asDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{missing, asDirectory} {
			goResult := runBuildCommand(t, goPurr, directory, []string{"build", path}, nil)
			ktResult := runBuildCommand(t, kt, directory, []string{"build", path}, nil)
			if goResult.status != 1 || ktResult.status != 1 || goResult.stdout != ktResult.stdout || goResult.stderr != ktResult.stderr {
				t.Errorf("%s: Go %d/%q/%q, Kitten %d/%q/%q", path,
					goResult.status, goResult.stdout, goResult.stderr,
					ktResult.status, ktResult.stdout, ktResult.stderr)
			}
		}
	})

	t.Run("import failures", func(t *testing.T) {
		root := t.TempDir()
		project := filepath.Join(root, "project")
		if err := os.Mkdir(project, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "outside.ktn"), []byte("fn outside() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name string
			main string
		}{
			{"missing import", "import \"missing.ktn\";\nfn main() {}\n"},
			{"import outside root", "import \"../outside.ktn\";\nfn main() {}\n"},
			{"imported syntax", "import \"bad.ktn\";\nfn main() {}\n"},
		}
		for _, testCase := range cases {
			mainPath := filepath.Join(project, "main.ktn")
			if err := os.WriteFile(mainPath, []byte(testCase.main), 0o644); err != nil {
				t.Fatal(err)
			}
			if testCase.name == "imported syntax" {
				if err := os.WriteFile(filepath.Join(project, "bad.ktn"), []byte("fn broken( {\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			goResult := runBuildCommand(t, goPurr, root, []string{"build", mainPath}, nil)

			ktResult := runBuildCommand(t, kt, root, []string{"build", mainPath}, nil)
			if goResult.status != 1 || ktResult.status != 1 || goResult.stdout != ktResult.stdout || goResult.stderr != ktResult.stderr {
				t.Errorf("%s: Go %d/%q/%q, Kitten %d/%q/%q", testCase.name,
					goResult.status, goResult.stdout, goResult.stderr,
					ktResult.status, ktResult.stdout, ktResult.stderr)
			}
		}
	})
}

func TestKittenPurrBuildTempCleanup(t *testing.T) {
	requireToolchain(t)
	goPurr := buildGoPurr(t)
	kt := buildPurrKTWithGo(t)
	directory := t.TempDir()
	tmp := t.TempDir()
	source := writeBuildFixture(t, directory)
	environment := []string{"TMPDIR=" + tmp}
	for _, testCase := range []struct {
		name      string
		arguments []string
		status    int
	}{
		{"success", []string{"build", source, "-o", filepath.Join(directory, "ok")}, 0},
		{"failure", []string{"build", filepath.Join(directory, "missing.ktn")}, 1},
	} {
		for pipeline, executable := range map[string]string{"Go": goPurr, "Kitten": kt} {
			result := runBuildCommand(t, executable, directory, testCase.arguments, environment)
			if result.status != testCase.status {
				t.Errorf("%s %s: status %d, want %d", pipeline, testCase.name, result.status, testCase.status)
			}
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "purr-build") {
					t.Errorf("%s %s left temp entry %q", pipeline, testCase.name, entry.Name())
				}
			}
		}
	}
}

func TestKittenPurrBuildCapstone(t *testing.T) {
	requireToolchain(t)
	goPurr := buildGoPurr(t)
	kt := buildPurrKTWithGo(t)
	root := repoPath(t, "..", "..")
	driver := filepath.Join(root, "selfhost", "driver.ktn")
	work := t.TempDir()
	ktStage := filepath.Join(work, "stage-kt")
	start := time.Now()
	result := runBuildCommand(t, kt, work, []string{"build", driver, "-o", ktStage}, nil)
	elapsed := time.Since(start)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("purr-kt capstone build: %d %q", result.status, result.stderr)
	}
	t.Logf("purr-kt capstone build time: %s", elapsed)
	ktC := runExternal(t, ktStage, []string{driver}, root, nil)
	if ktC.status != 0 || ktC.stderr != "" {
		t.Fatalf("capstone stage output: %d %q", ktC.status, ktC.stderr)
	}
	goStage := filepath.Join(work, "stage-go")
	if result := runBuildCommand(t, goPurr, work, []string{"build", driver, "-o", goStage}, nil); result.status != 0 || result.stderr != "" {
		t.Fatalf("Go capstone build: %d %q", result.status, result.stderr)
	}
	goC := runExternal(t, goStage, []string{driver}, root, nil)
	if goC.status != 0 || goC.stderr != "" {
		t.Fatalf("Go stage output: %d %q", goC.status, goC.stderr)
	}
	if difference := unitDifference(ktC.stdout, goC.stdout); difference != "" {
		t.Fatalf("capstone driver C differs: %s", difference)
	}
}

func TestKittenPurrBuildSanitizer(t *testing.T) {
	requireToolchain(t)
	root := repoPath(t, "..", "..")
	rootPath := filepath.Join(root, "purr.ktn")
	modules, err := loader.Load(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := checker.CheckModules(modules)
	if err != nil {
		t.Fatal(err)
	}
	text, err := codegen.Generate(modules, checked)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatal(err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")
	executable, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, text)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	directory := t.TempDir()
	source := writeBuildFixture(t, directory)
	result := runExternal(t, executable, []string{"build", source, "-o", filepath.Join(directory, "san")}, directory, nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("sanitized build: status %d, stderr %q", result.status, result.stderr)
	}
	run := runExternal(t, filepath.Join(directory, "san"), nil, directory, nil)
	if run.status != 0 || run.stdout != "Hello, world!\n" || run.stderr != "" {
		t.Fatalf("sanitized built program: %d/%q/%q", run.status, run.stdout, run.stderr)
	}
}

func TestKittenPurrBuildOutputFailures(t *testing.T) {
	requireToolchain(t)
	goPurr := buildGoPurr(t)
	kt := buildPurrKTWithGo(t)
	root := t.TempDir()
	writeBuildFixture(t, root)

	t.Run("existing output directory", func(t *testing.T) {
		output := filepath.Join(root, "outdir")
		if err := os.Mkdir(output, 0o755); err != nil {
			t.Fatal(err)
		}
		goResult := runBuildCommand(t, goPurr, root, []string{"build", "hello.ktn", "-o", output}, nil)
		ktResult := runBuildCommand(t, kt, root, []string{"build", "hello.ktn", "-o", output}, nil)
		if goResult.status != 1 || ktResult.status != 1 {
			t.Fatalf("status Go %d, Kitten %d", goResult.status, ktResult.status)
		}
		if normalizeBuildLog(goResult.stderr) != normalizeBuildLog(ktResult.stderr) {
			t.Errorf("output directory diagnostics differ:\nGo %q\nKitten %q", goResult.stderr, ktResult.stderr)
		}
	})

	t.Run("unwritable output directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission checks are not meaningful as root")
		}
		outputDir := filepath.Join(root, "locked")
		if err := os.Mkdir(outputDir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(outputDir, 0o755) })
		output := filepath.Join(outputDir, "app")
		goResult := runBuildCommand(t, goPurr, root, []string{"build", "hello.ktn", "-o", output}, nil)
		ktResult := runBuildCommand(t, kt, root, []string{"build", "hello.ktn", "-o", output}, nil)
		if goResult.status != 1 || ktResult.status != 1 {
			t.Fatalf("status Go %d, Kitten %d", goResult.status, ktResult.status)
		}
		if normalizeBuildLog(goResult.stderr) != normalizeBuildLog(ktResult.stderr) {
			t.Errorf("unwritable output diagnostics differ:\nGo %q\nKitten %q", goResult.stderr, ktResult.stderr)
		}
	})
}
