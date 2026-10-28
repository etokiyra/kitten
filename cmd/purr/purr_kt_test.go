package main

// Tests for the Kitten-written purr-kt entry point.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kitten/checker"
	"kitten/codegen"
	"kitten/loader"
)

func buildGoPurr(t *testing.T) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "purr-go")
	build := exec.Command("go", "build", "-o", executable, "./cmd/purr")
	build.Dir = repoPath(t, "..", "..")
	var diagnostics bytes.Buffer
	build.Stdout = &diagnostics
	build.Stderr = &diagnostics
	if err := build.Run(); err != nil {
		t.Fatalf("building the Go purr: %v\n%s", err, diagnostics.String())
	}
	return executable
}

func buildPurrKTWithGo(t *testing.T) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "purr-kt-go")
	var diagnostics bytes.Buffer
	if err := buildExecutable(repoPath(t, "..", "..", "purr.ktn"), output, &diagnostics); err != nil {
		t.Fatalf("building purr-kt with Go pipeline: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C diagnostics while building purr-kt with Go pipeline:\n%s", warnings)
	}
	return output
}

func buildPurrKTWithKitten(t *testing.T, stage1 string) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "purr-kt-kitten")
	result := runExternal(t, wrapperPath(t), []string{
		"build", repoPath(t, "..", "..", "purr.ktn"), "-o", output,
	}, repoPath(t, "..", ".."), wrapperEnvironment(stage1, t))
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("building purr-kt with Kitten pipeline: status %d, stderr %q", result.status, result.stderr)
	}
	return output
}

func runPurrKT(t *testing.T, executable, directory string, arguments []string) programResult {
	t.Helper()
	return runExternal(t, executable, arguments, directory, nil)
}

func purrUsageText() string {
	return "Kitten compiler\n" +
		"\n" +
		"Usage:\n" +
		"  purr build <file.ktn> [-o <output>]   Compile a Kitten source file\n" +
		"  purr run <file.ktn>                   Compile and run a Kitten source file\n" +
		"  purr new [--lib] <name>               Create a new project\n" +
		"  purr help                             Show this help\n" +
		"\n" +
		"Options:\n" +
		"  -o, --output <path>   Write the executable to <path> (build only)\n" +
		"\n" +
		"The C compiler is taken from CC, or discovered as cc, gcc, or clang.\n" +
		"Kitten programs are linked against the Boehm garbage collector (libgc)."
}

func TestKittenPurrHelpAndDispatch(t *testing.T) {
	requireToolchain(t)
	stage1Start := time.Now()
	stage1 := buildStage1(t)
	t.Logf("stage 1 build: %s", time.Since(stage1Start).Round(time.Millisecond))
	goPurr := buildGoPurr(t)
	goKT := buildPurrKTWithGo(t)
	kittenKT := buildPurrKTWithKitten(t, stage1)
	for _, executable := range []string{goKT, kittenKT} {
		if information, err := os.Stat(executable); err != nil {
			t.Fatalf("stat purr-kt: %v", err)
		} else {
			t.Logf("purr-kt size: %d bytes", information.Size())
		}
	}

	goHelp := runPurrKT(t, goPurr, t.TempDir(), []string{"help"})
	if goHelp.status != 0 || goHelp.stderr != "" {
		t.Fatalf("Go help: status %d, stderr %q", goHelp.status, goHelp.stderr)
	}
	goUsage := strings.TrimSuffix(goHelp.stdout, "\n")
	wantUsage := strings.Replace(goUsage,
		"\n\nOptions:",
		"\n  purr new [--lib] <name>               Create a new project\n"+
			"  purr help                             Show this help\n\nOptions:",
		1)
	if wantUsage != purrUsageText() {
		t.Fatalf("usage relationship differs from the Go text:\n%s", wantUsage)
	}

	type cliCase struct {
		name      string
		arguments []string
		status    int
		stdout    string
		stderr    string
	}
	cases := []cliCase{
		{"no arguments", nil, 2, "", purrUsageText() + "\n"},
		{"help", []string{"help"}, 0, purrUsageText() + "\n", ""},
		{"dash h", []string{"-h"}, 0, purrUsageText() + "\n", ""},
		{"dash dash help", []string{"--help"}, 0, purrUsageText() + "\n", ""},
		{"help extra", []string{"help", "extra"}, 0, purrUsageText() + "\n", ""},
		{"dash h extra", []string{"-h", "extra"}, 0, purrUsageText() + "\n", ""},
		{"dash dash help extra", []string{"--help", "extra"}, 0, purrUsageText() + "\n", ""},
		{"unknown", []string{"wat"}, 2, "", "purr: unknown command \"wat\"\n\n" + purrUsageText() + "\n"},
		{"empty command", []string{""}, 2, "", "purr: unknown command \"\"\n\n" + purrUsageText() + "\n"},
		{"flag command", []string{"--version"}, 2, "", "purr: unknown command \"--version\"\n\n" + purrUsageText() + "\n"},
		{"build usage", []string{"build"}, 2, "", "purr build: missing source file\n\n" + purrUsageText() + "\n"},
		{"build extension", []string{"build", "x"}, 2, "", "purr build: \"x\" is not a Kitten source file (expected a .ktn file)\n"},
		{"run usage", []string{"run"}, 2, "", "purr run: missing source file\n\n" + purrUsageText() + "\n"},
		{"run extension", []string{"run", "x"}, 2, "", "purr run: \"x\" is not a Kitten source file (expected a .ktn file)\n"},
		{"run output rejected", []string{"run", "-o", "out", "x.ktn"}, 2, "", "purr run: -o is not supported; run compiles to a temporary executable\n"},
		{"run missing output value", []string{"run", "-o"}, 2, "", "purr run: -o requires a path\n\n" + purrUsageText() + "\n"},
	}
	// Go's %q and Kitten's quote_argument agree on these ASCII forms. The
	// expected text exercises the same quoting in both implementations. They
	// deliberately do not agree on printable non-ASCII UTF-8: Go's %q emits
	// the rune, while Kitten's byte-oriented quote_argument emits \xNN for each
	// byte above 0x7F. That known gap is not changed in this test-only slice.
	for _, command := range []string{"a\"b", "a\\b", "a\nb", "a\tb", "a\rb", "\x01", "\x7f"} {
		cases = append(cases, cliCase{
			name:      "unknown " + quoteForTest(command),
			arguments: []string{command},
			status:    2,
			stdout:    "",
			stderr:    "purr: unknown command " + quoteForTest(command) + "\n\n" + purrUsageText() + "\n",
		})
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// The Go CLI and both Kitten builds must agree on status, stdout,
			// and stderr. The only permitted difference is the documented
			// usage block, which the Kitten sidecar extends by two lines.
			goResult := runPurrKT(t, goPurr, t.TempDir(), testCase.arguments)
			if goResult.status != testCase.status {
				t.Errorf("Go status %d, want %d", goResult.status, testCase.status)
			}
			if normalizeBuildUsage(t, goResult.stdout) != normalizeBuildUsage(t, testCase.stdout) {
				t.Errorf("Go stdout %q, want %q (normalized)", goResult.stdout, testCase.stdout)
			}
			if normalizeBuildUsage(t, goResult.stderr) != normalizeBuildUsage(t, testCase.stderr) {
				t.Errorf("Go stderr %q, want %q (normalized)", goResult.stderr, testCase.stderr)
			}
			for pipeline, executable := range map[string]string{"Go-built": goKT, "Kitten-built": kittenKT} {
				result := runPurrKT(t, executable, t.TempDir(), testCase.arguments)
				if result.status != testCase.status || result.stdout != testCase.stdout || result.stderr != testCase.stderr {
					t.Errorf("%s %s: status %d, stdout %q, stderr %q; want %d, %q, %q",
						pipeline, testCase.name, result.status, result.stdout, result.stderr,
						testCase.status, testCase.stdout, testCase.stderr)
				}
			}
		})
	}
}

func quoteForTest(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch character {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\t':
			builder.WriteString(`\t`)
		case '\r':
			builder.WriteString(`\r`)
		default:
			if character < 32 || character >= 127 {
				builder.WriteString("\\x")
				const digits = "0123456789abcdef"
				builder.WriteByte(digits[character/16])
				builder.WriteByte(digits[character%16])
			} else {
				builder.WriteByte(character)
			}
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func TestKittenPurrCParity(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	root := repoPath(t, "..", "..")
	rootPath := filepath.Join(root, "purr.ktn")
	modules, err := loader.Load(rootPath)
	if err != nil {
		t.Fatalf("loading purr.ktn: %v", err)
	}
	checked, err := checker.CheckModules(modules)
	if err != nil {
		t.Fatalf("checking purr.ktn: %v", err)
	}
	goC, err := codegen.Generate(modules, checked)
	if err != nil {
		t.Fatalf("generating purr.ktn C with Go: %v", err)
	}
	result := runExternal(t, stage1, []string{rootPath}, root, nil)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("stage 1 purr.ktn C: status %d, stderr %q", result.status, result.stderr)
	}
	if difference := unitDifference(result.stdout, goC); difference != "" {
		t.Fatalf("purr.ktn C differs: %s", difference)
	}
}

func runPurrKTWithShell(t *testing.T, script, executable, directory string, arguments []string) programResult {
	t.Helper()
	commandArguments := append([]string{"-c", script, "sh", executable}, arguments...)
	command := exec.Command("/bin/sh", commandArguments...)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running purr-kt through shell: %v", err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func compareNewParity(t *testing.T, probeResult, purrResult programResult, probeDir, purrDir string) {
	t.Helper()
	if probeResult.status != purrResult.status || probeResult.stdout != purrResult.stdout || probeResult.stderr != purrResult.stderr {
		t.Errorf("probe %d/%q/%q, purr-kt %d/%q/%q",
			probeResult.status, probeResult.stdout, probeResult.stderr,
			purrResult.status, purrResult.stdout, purrResult.stderr)
	}
	if !sameSnapshot(newSnapshot(t, probeDir), newSnapshot(t, purrDir)) {
		t.Errorf("probe and purr-kt trees differ")
	}
}

func runNewParityCase(t *testing.T, probe string, purrs map[string]string, probeArguments, purrArguments []string, script string, setup func(*testing.T, string)) {
	t.Helper()
	probeDir := t.TempDir()
	if setup != nil {
		setup(t, probeDir)
	}
	var probeResult programResult
	if script == "" {
		probeResult = runNewProbe(t, probe, probeDir, probeArguments)
	} else {
		probeResult = runNewProbeWithShell(t, script, probe, probeDir, probeArguments)
	}
	for pipeline, purr := range purrs {
		purrDir := t.TempDir()
		if setup != nil {
			setup(t, purrDir)
		}
		var purrResult programResult
		if script == "" {
			purrResult = runPurrKT(t, purr, purrDir, purrArguments)
		} else {
			purrResult = runPurrKTWithShell(t, script, purr, purrDir, purrArguments)
		}
		if probeResult.status != purrResult.status || probeResult.stdout != purrResult.stdout || probeResult.stderr != purrResult.stderr {
			t.Errorf("%s: probe %d/%q/%q, purr-kt %d/%q/%q",
				pipeline, probeResult.status, probeResult.stdout, probeResult.stderr,
				purrResult.status, purrResult.stdout, purrResult.stderr)
		}
		if !sameSnapshot(newSnapshot(t, probeDir), newSnapshot(t, purrDir)) {
			t.Errorf("%s: probe and purr-kt trees differ", pipeline)
		}
	}
}

func TestKittenPurrNewParity(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	probe := buildNewProbeWithGo(t)
	purrs := map[string]string{
		"Go":     buildPurrKTWithGo(t),
		"Kitten": buildPurrKTWithKitten(t, stage1),
	}

	t.Run("success and modes", func(t *testing.T) {
		cases := []struct {
			mask    string
			name    string
			library bool
			after   bool
		}{
			{"022", "hello", false, false},
			{"022", "catnip", true, false},
			{"077", "restricted", false, false},
			{"000", "permissive", false, false},
			{"022", "tail", true, true},
		}
		for _, testCase := range cases {
			probeArguments := []string{testCase.name}
			purrArguments := []string{"new", testCase.name}
			if testCase.library {
				if testCase.after {
					probeArguments = append([]string{testCase.name}, "--lib")
					purrArguments = append([]string{"new", testCase.name}, "--lib")
				} else {
					probeArguments = append([]string{"--lib"}, testCase.name)
					purrArguments = append([]string{"new", "--lib"}, testCase.name)
				}
			}
			runNewParityCase(t, probe, purrs, probeArguments, purrArguments,
				"umask "+testCase.mask+"; exec \"$@\"", nil)
		}
	})

	t.Run("invalid and usage", func(t *testing.T) {
		argumentSets := [][]string{
			{""},
			{strings.Repeat("a", 65)},
			{"A"},
			{"a/b"},
			{"."},
			{".."},
			{"x-"},
			{"x_"},
			{"a b"},
			{"caf\u00e9"},
			{"a\n"},
			{"one", "two"},
			{"--bad", "one"},
			{"--lib", "--lib", "one"},
			{"--"},
			{"-x"},
		}
		for _, arguments := range argumentSets {
			purrArguments := append([]string{"new"}, arguments...)
			runNewParityCase(t, probe, purrs, arguments, purrArguments, "", nil)
		}
	})

	t.Run("destinations", func(t *testing.T) {
		setups := []struct {
			name  string
			setup func(*testing.T, string)
		}{
			{"regular file", func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, "a"), []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
			}},
			{"directory", func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "a"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "a", "keep"), []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
			}},
			{"dangling symlink", func(t *testing.T, dir string) {
				if err := os.Symlink("missing", filepath.Join(dir, "a")); err != nil {
					t.Fatal(err)
				}
			}},
			{"directory symlink", func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "target"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target", filepath.Join(dir, "a")); err != nil {
					t.Fatal(err)
				}
			}},
		}
		for _, testCase := range setups {
			runNewParityCase(t, probe, purrs, []string{"a"}, []string{"new", "a"}, "", testCase.setup)
		}
	})

	t.Run("read-only parent", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission checks are not meaningful as root")
		}
		setup := func(t *testing.T, dir string) {
			if err := os.Chmod(dir, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		}
		runNewParityCase(t, probe, purrs, []string{"a"}, []string{"new", "a"}, "", setup)
	})

	t.Run("step 4 and 5 failures", func(t *testing.T) {
		if _, err := exec.LookPath("prlimit"); err != nil {
			t.Skip("prlimit unavailable")
		}
		cases := []struct {
			name    string
			script  string
			library bool
		}{
			{"step 4", "umask 0777; exec \"$@\"", false},
			{"step 5a", "trap '' XFSZ; exec prlimit --fsize=0 -- \"$@\"", false},
			{"step 5b", "trap '' XFSZ; exec prlimit --fsize=16 -- \"$@\"", false},
			{"step 5c", "trap '' XFSZ; exec prlimit --fsize=40 -- \"$@\"", false},
			{"step 5c library", "trap '' XFSZ; exec prlimit --fsize=50 -- \"$@\"", true},
		}
		for _, testCase := range cases {
			probeArguments := []string{"a"}
			purrArguments := []string{"new", "a"}
			if testCase.library {
				probeArguments = append([]string{"--lib"}, probeArguments...)
				purrArguments = append([]string{"new", "--lib"}, "a")
			}
			runNewParityCase(t, probe, purrs, probeArguments, purrArguments, testCase.script, nil)
		}
	})
}

func TestKittenPurrGeneratedProject(t *testing.T) {
	requireToolchain(t)
	purr := buildPurrKTWithGo(t)
	goPurr := buildGoPurr(t)
	directory := t.TempDir()
	start := time.Now()
	result := runPurrKT(t, purr, directory, []string{"new", "fromkitten"})
	elapsed := time.Since(start)
	if result.status != 0 || result.stdout != "" || result.stderr != "     Created binary (application) fromkitten\n" {
		t.Fatalf("purr-kt new: status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	t.Logf("purr-kt new wall time: %s", elapsed)
	project := filepath.Join(directory, "fromkitten")
	executable := filepath.Join(directory, "fromkitten-bin")
	build := runExternal(t, goPurr, []string{"build", filepath.Join(project, "src", "main.ktn"), "-o", executable}, project, nil)
	if build.status != 0 || build.stderr != "" {
		t.Fatalf("Go purr build of generated project: status %d, stderr %q", build.status, build.stderr)
	}
	run := runExternal(t, executable, nil, project, nil)
	if run.status != 0 || run.stdout != "Hello, world!\n" || run.stderr != "" {
		t.Fatalf("generated binary: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
	}
	run = runExternal(t, goPurr, []string{"run", filepath.Join(project, "src", "main.ktn")}, project, nil)
	if run.status != 0 || run.stdout != "Hello, world!\n" || run.stderr != "" {
		t.Fatalf("Go purr run of generated project: status %d, stdout %q, stderr %q", run.status, run.stdout, run.stderr)
	}

	ktExecutable := filepath.Join(directory, "fromkitten-bin-kt")
	ktBuild := runPurrKT(t, purr, project, []string{"build", filepath.Join(project, "src", "main.ktn"), "-o", ktExecutable})
	if ktBuild.status != 0 || ktBuild.stderr != "" {
		t.Fatalf("purr-kt build of generated project: status %d, stderr %q", ktBuild.status, ktBuild.stderr)
	}
	ktRun := runExternal(t, ktExecutable, nil, project, nil)
	if ktRun.status != 0 || ktRun.stdout != "Hello, world!\n" || ktRun.stderr != "" {
		t.Fatalf("purr-kt built project: status %d, stdout %q, stderr %q", ktRun.status, ktRun.stdout, ktRun.stderr)
	}
}

func TestKittenPurrSanitizer(t *testing.T) {
	requireToolchain(t)
	root := repoPath(t, "..", "..")
	rootPath := filepath.Join(root, "purr.ktn")
	modules, err := loader.Load(rootPath)
	if err != nil {
		t.Fatalf("loading purr.ktn: %v", err)
	}
	checked, err := checker.CheckModules(modules)
	if err != nil {
		t.Fatalf("checking purr.ktn: %v", err)
	}
	text, err := codegen.Generate(modules, checked)
	if err != nil {
		t.Fatalf("generating purr.ktn C: %v", err)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")
	executable, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, text)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	directory := t.TempDir()
	result := runExternal(t, executable, []string{"help"}, directory, nil)
	if result.status != 0 || result.stdout != purrUsageText()+"\n" || result.stderr != "" {
		t.Fatalf("sanitized help: status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	result = runExternal(t, executable, []string{"new", "a"}, directory, nil)
	if result.status != 0 || result.stdout != "" || result.stderr != "     Created binary (application) a\n" {
		t.Fatalf("sanitized new: status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
}
