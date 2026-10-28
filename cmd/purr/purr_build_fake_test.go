package main

// Fake-compiler and discovery tests for purr-kt build.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeFakeCompiler(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "fake-cc")
	script := `#!/bin/sh
log=${FAKE_LOG:?}
{
  printf 'cwd=%s\n' "$PWD"
  printf 'argc=%s\n' "$#"
  index=0
  for argument in "$@"; do
    printf 'arg%s=%s\n' "$index" "$argument"
    index=$((index + 1))
  done
} > "$log.args"
cp program.c "$log.program.c"
cp kitten_runtime.c "$log.runtime.c"
cp kitten_runtime.h "$log.runtime.h"
mode=${FAKE_MODE:-success}
case "$mode" in
  success)
    exec "$REAL_CC" "$@"
    ;;
  warnings)
    printf 'fake compiler warning\n' >&2
    exec "$REAL_CC" "$@"
    ;;
  fail)
    printf 'fake compiler error\n' >&2
    exit 1
    ;;
  both)
    printf 'fake stdout\n'
    printf 'fake stderr\n' >&2
    exit 1
    ;;
  args-fail)
    printf 'fake compiler diagnostic\n' >&2
    index=0
    for argument in "$@"; do
      printf 'arg%s=%s\n' "$index" "$argument" >&2
      index=$((index + 1))
    done
    exit 1
    ;;
  no-output)
    exit 0
    ;;
  directory)
    mkdir -p "$FAKE_OUTPUT"
    exit 0
    ;;
  signal)
    kill -TERM $$
    ;;
  extra-success)
    touch extra
    exec "$REAL_CC" "$@"
    ;;
  extra-fail)
    touch extra
    printf 'fake compiler error\n' >&2
    exit 1
    ;;
  fake-output)
    : > "$FAKE_OUTPUT"
    exit 0
    ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake compiler: %v", err)
	}
	return path
}

func fakeBuildEnvironment(t *testing.T, fake, log, mode, output, tmp string) []string {
	environment := []string{
		"CC=" + fake,
		"REAL_CC=" + realCompilerPath(t),
		"FAKE_LOG=" + log,
		"FAKE_MODE=" + mode,
		"TMPDIR=" + tmp,
	}
	if output != "" {
		environment = append(environment, "FAKE_OUTPUT="+output)
	}
	return environment
}

func realCompilerPath(t *testing.T) string {
	t.Helper()
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering real compiler: %v", err)
	}
	return compiler[0]
}

var buildDirectoryPattern = regexp.MustCompile(`purr-build[.-][A-Za-z0-9]+`)

func normalizeBuildLog(text string) string {
	return buildDirectoryPattern.ReplaceAllString(text, "purr-build.X")
}

func readFakeLog(t *testing.T, prefix, suffix string) string {
	t.Helper()
	data, err := os.ReadFile(prefix + suffix)
	if err != nil {
		t.Fatalf("reading fake log %s%s: %v", prefix, suffix, err)
	}
	return string(data)
}

func TestKittenPurrBuildFakeCompilerFixtures(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	goPurr := buildGoPurr(t)
	purrs := map[string]string{
		"Go":     buildPurrKTWithGo(t),
		"Kitten": buildPurrKTWithKitten(t, stage1),
	}
	fake := writeFakeCompiler(t)
	roots := []string{
		repoPath(t, "..", "..", "tests", "hello.ktn"),
		repoPath(t, "..", "..", "tests", "fizzbuzz.ktn"),
		repoPath(t, "..", "..", "tests", "math.ktn"),
		repoPath(t, "..", "..", "tests", "room.ktn"),
		repoPath(t, "..", "..", "tests", "strings.ktn"),
		repoPath(t, "..", "..", "tests", "arrays.ktn"),
		repoPath(t, "..", "..", "tests", "variants.ktn"),
		repoPath(t, "..", "..", "tests", "maps.ktn"),
		repoPath(t, "..", "..", "tests", "lexer_proof.ktn"),
		repoPath(t, "..", "..", "tests", "modules", "main.ktn"),
	}
	for _, root := range roots {
		cwd := t.TempDir()
		tmp := t.TempDir()
		output := filepath.Join(cwd, "program")
		run := func(executable, label string) (programResult, string, string, string, string) {
			prefix := filepath.Join(cwd, label)
			result := runBuildCommand(t, executable, cwd,
				[]string{"build", root, "-o", output},
				fakeBuildEnvironment(t, fake, prefix, "success", output, tmp))
			return result,
				readFakeLog(t, prefix, ".args"),
				readFakeLog(t, prefix, ".program.c"),
				readFakeLog(t, prefix, ".runtime.c"),
				readFakeLog(t, prefix, ".runtime.h")
		}
		goResult, goArgs, goProgram, goRuntimeC, goRuntimeH := run(goPurr, "go")
		if goResult.status != 0 || goResult.stderr != "" {
			t.Fatalf("Go fake build %s: %d %q", root, goResult.status, goResult.stderr)
		}
		_ = os.Remove(output)
		for pipeline, executable := range purrs {
			result, args, program, runtimeC, runtimeH := run(executable, "kt-"+pipeline)
			if result.status != 0 || result.stderr != "" {
				t.Errorf("%s fake build %s: %d %q", pipeline, root, result.status, result.stderr)
				continue
			}
			if normalizeBuildLog(args) != normalizeBuildLog(goArgs) {
				t.Errorf("%s args differ for %s:\n%s\nwant:\n%s", pipeline, root, normalizeBuildLog(args), normalizeBuildLog(goArgs))
			}
			if program != goProgram || runtimeC != goRuntimeC || runtimeH != goRuntimeH {
				t.Errorf("%s embedded files differ for %s", pipeline, root)
			}
			_ = os.Remove(output)
		}
	}
}

func TestKittenPurrBuildFakeModes(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	goPurr := buildGoPurr(t)
	purrs := map[string]string{
		"Go":     buildPurrKTWithGo(t),
		"Kitten": buildPurrKTWithKitten(t, stage1),
	}
	fake := writeFakeCompiler(t)
	cwd := t.TempDir()
	source := writeBuildFixture(t, cwd)
	tmp := t.TempDir()
	output := filepath.Join(cwd, "app")

	run := func(executable, mode string) programResult {
		_ = os.RemoveAll(output)
		return runBuildCommand(t, executable, cwd,
			[]string{"build", source, "-o", output},
			fakeBuildEnvironment(t, fake, filepath.Join(cwd, "log"), mode, output, tmp))
	}

	t.Run("warnings", func(t *testing.T) {
		goResult := run(goPurr, "warnings")
		if goResult.status != 0 || goResult.stdout != "built "+output+"\n" || !strings.Contains(goResult.stderr, "fake compiler warning") {
			t.Fatalf("Go warnings: %d %q %q", goResult.status, goResult.stdout, goResult.stderr)
		}
		for pipeline, executable := range purrs {
			result := run(executable, "warnings")
			if result.status != 0 || result.stdout != goResult.stdout || !strings.Contains(result.stderr, "fake compiler warning") {
				t.Errorf("%s warnings: %d %q %q", pipeline, result.status, result.stdout, result.stderr)
			}
		}
	})

	t.Run("failure", func(t *testing.T) {
		goResult := run(goPurr, "fail")
		if goResult.status != 1 || goResult.stdout != "" || !strings.Contains(goResult.stderr, "fake compiler error") {
			t.Fatalf("Go failure: %d %q %q", goResult.status, goResult.stdout, goResult.stderr)
		}
		for pipeline, executable := range purrs {
			result := run(executable, "fail")
			if result.status != 1 || result.stdout != "" || !strings.Contains(result.stderr, "fake compiler error") {
				t.Errorf("%s failure: %d %q %q", pipeline, result.status, result.stdout, result.stderr)
			}
		}
	})

	t.Run("both streams", func(t *testing.T) {
		result := run(purrs["Go"], "both")
		wantDiagnostics := "fake stdout\nfake stderr"
		if result.status != 1 || result.stdout != "" || !strings.Contains(result.stderr, wantDiagnostics) {
			t.Errorf("both streams: %d %q %q", result.status, result.stdout, result.stderr)
		}
		stdoutAt := strings.Index(result.stderr, "fake stdout")
		stderrAt := strings.Index(result.stderr, "fake stderr")
		if stdoutAt < 0 || stderrAt < 0 || stdoutAt > stderrAt {
			t.Errorf("combined output is not stdout then stderr: %q", result.stderr)
		}
	})

	t.Run("no output", func(t *testing.T) {
		for pipeline, executable := range purrs {
			result := run(executable, "no-output")
			want := source + ": C compiler reported success but " + output + " was not produced\n"
			if result.status != 1 || result.stdout != "" || result.stderr != want {
				t.Errorf("%s no output: %d %q %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
			}
		}
	})

	t.Run("directory output", func(t *testing.T) {
		for pipeline, executable := range purrs {
			result := run(executable, "directory")
			want := source + ": output path " + output + " is a directory\n"
			if result.status != 1 || result.stdout != "" || result.stderr != want {
				t.Errorf("%s directory: %d %q %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
			}
		}
		_ = os.RemoveAll(output)
	})

	t.Run("signal", func(t *testing.T) {
		goResult := run(goPurr, "signal")
		if goResult.status != 1 || !strings.Contains(goResult.stderr, "signal:") {
			t.Fatalf("Go signal: %d %q", goResult.status, goResult.stderr)
		}
		for pipeline, executable := range purrs {
			result := run(executable, "signal")
			if result.status != 1 || !strings.Contains(result.stderr, "exit status 143") {
				t.Errorf("%s signal: %d %q", pipeline, result.status, result.stderr)
			}
		}
	})

	t.Run("cleanup notes", func(t *testing.T) {
		for pipeline, executable := range purrs {
			result := run(executable, "extra-success")
			if result.status != 0 || result.stdout != "built "+output+"\n" {
				t.Errorf("%s extra success: %d %q %q", pipeline, result.status, result.stdout, result.stderr)
			}
			if !strings.Contains(result.stderr, "note: could not remove") {
				t.Errorf("%s extra success: no cleanup note: %q", pipeline, result.stderr)
			}
			if !strings.Contains(result.stderr, "purr-build") {
				t.Errorf("%s extra success: note does not name the temp directory: %q", pipeline, result.stderr)
			}
		}
		entries, _ := os.ReadDir(tmp)
		for _, entry := range entries {
			_ = os.RemoveAll(filepath.Join(tmp, entry.Name()))
		}
	})
}

func TestKittenPurrBuildCompilerDiscovery(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	purrs := map[string]string{
		"Go":     buildPurrKTWithGo(t),
		"Kitten": buildPurrKTWithKitten(t, stage1),
	}
	fake := writeFakeCompiler(t)
	real := realCompilerPath(t)

	t.Run("CC with leading arguments", func(t *testing.T) {
		cwd := t.TempDir()
		source := writeBuildFixture(t, cwd)
		output := filepath.Join(cwd, "app")
		for pipeline, executable := range purrs {
			log := filepath.Join(cwd, "log-"+pipeline)
			result := runBuildCommand(t, executable, cwd,
				[]string{"build", source, "-o", output},
				[]string{"CC=" + fake + " -DX", "REAL_CC=" + real, "FAKE_LOG=" + log, "FAKE_MODE=fake-output", "FAKE_OUTPUT=" + output, "TMPDIR=" + t.TempDir()})
			if result.status != 0 {
				t.Errorf("%s CC args: %d %q", pipeline, result.status, result.stderr)
			}
			args := readFakeLog(t, log, ".args")
			if !strings.Contains(args, "arg0=-DX") {
				t.Errorf("%s leading arg missing: %s", pipeline, args)
			}
		}
	})

	t.Run("CC missing", func(t *testing.T) {
		cwd := t.TempDir()
		source := writeBuildFixture(t, cwd)
		for pipeline, executable := range purrs {
			result := runBuildCommand(t, executable, cwd,
				[]string{"build", source},
				[]string{"CC=missing", "PATH=" + t.TempDir()})
			want := source + ": CC=\"missing\": \"missing\" not found\n"
			if result.status != 1 || result.stdout != "" || result.stderr != want {
				t.Errorf("%s CC missing: %d %q %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
			}
		}
	})

	t.Run("no compiler", func(t *testing.T) {
		cwd := t.TempDir()
		source := writeBuildFixture(t, cwd)
		for pipeline, executable := range purrs {
			result := runBuildCommand(t, executable, cwd,
				[]string{"build", source},
				[]string{"CC=", "PATH=" + t.TempDir()})
			want := source + ": no C compiler found; set CC or install cc, gcc, or clang\n"
			if result.status != 1 || result.stdout != "" || result.stderr != want {
				t.Errorf("%s no compiler: %d %q %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
			}
		}
	})

	t.Run("empty PATH entries are relative", func(t *testing.T) {
		cwd := t.TempDir()
		source := writeBuildFixture(t, cwd)
		if err := os.Symlink(fake, filepath.Join(cwd, "cc")); err != nil {
			t.Fatal(err)
		}
		for pipeline, executable := range purrs {
			result := runBuildCommand(t, executable, cwd,
				[]string{"build", source},
				[]string{"CC=", "PATH=:", "REAL_CC=" + real, "FAKE_LOG=" + filepath.Join(cwd, "log-"+pipeline), "FAKE_MODE=fake-output"})
			want := source + ": no C compiler found; set CC or install cc, gcc, or clang\n"
			if result.status != 1 || result.stdout != "" || result.stderr != want {
				t.Errorf("%s empty PATH: %d %q %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
			}
		}
	})

	t.Run("relative PATH entries are ErrDot", func(t *testing.T) {
		cwd := t.TempDir()
		source := writeBuildFixture(t, cwd)
		if err := os.Mkdir(filepath.Join(cwd, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(fake, filepath.Join(cwd, "bin", "cc")); err != nil {
			t.Fatal(err)
		}
		for pipeline, executable := range purrs {
			result := runBuildCommand(t, executable, cwd,
				[]string{"build", source},
				[]string{"CC=", "PATH=bin", "REAL_CC=" + real, "FAKE_LOG=" + filepath.Join(cwd, "log-"+pipeline), "FAKE_MODE=fake-output"})
			want := source + ": no C compiler found; set CC or install cc, gcc, or clang\n"
			if result.status != 1 || result.stdout != "" || result.stderr != want {
				t.Errorf("%s relative PATH: %d %q %q; want %q", pipeline, result.status, result.stdout, result.stderr, want)
			}
		}
	})

	t.Run("directory and non-executable candidates", func(t *testing.T) {
		cases := []struct {
			name  string
			setup func(string) error
		}{
			{"directory", func(bin string) error { return os.Mkdir(filepath.Join(bin, "cc"), 0o755) }},
			{"non-executable", func(bin string) error { return os.WriteFile(filepath.Join(bin, "cc"), []byte("x"), 0o644) }},
		}
		for _, testCase := range cases {
			bin := t.TempDir()
			if err := testCase.setup(bin); err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			source := writeBuildFixture(t, cwd)
			for pipeline, executable := range purrs {
				result := runBuildCommand(t, executable, cwd,
					[]string{"build", source},
					[]string{"CC=", "PATH=" + bin})
				want := source + ": no C compiler found; set CC or install cc, gcc, or clang\n"
				if result.status != 1 || result.stdout != "" || result.stderr != want {
					t.Errorf("%s %s: %d %q %q; want %q", pipeline, testCase.name, result.status, result.stdout, result.stderr, want)
				}
			}
		}
	})

	t.Run("CC empty uses PATH", func(t *testing.T) {
		cwd := t.TempDir()
		source := writeBuildFixture(t, cwd)
		bin := filepath.Join(cwd, "bin")
		if err := os.Mkdir(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(fake, filepath.Join(bin, "cc")); err != nil {
			t.Fatal(err)
		}
		for pipeline, executable := range purrs {
			log := filepath.Join(cwd, "log-"+pipeline)
			output := filepath.Join(cwd, "app")
			result := runBuildCommand(t, executable, cwd,
				[]string{"build", source, "-o", output},
				[]string{"CC=", "PATH=" + bin + ":/usr/bin:/bin", "REAL_CC=" + real, "FAKE_LOG=" + log, "FAKE_MODE=fake-output", "FAKE_OUTPUT=" + output, "TMPDIR=" + t.TempDir()})
			if result.status != 0 {
				t.Errorf("%s CC empty: %d %q", pipeline, result.status, result.stderr)
			}
			if _, err := os.Stat(log + ".args"); err != nil {
				t.Errorf("%s CC empty did not use PATH compiler", pipeline)
			}
		}
	})
}

func TestKittenPurrBuildPkgConfig(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	purrs := map[string]string{
		"Go":     buildPurrKTWithGo(t),
		"Kitten": buildPurrKTWithKitten(t, stage1),
	}
	fakeCC := writeFakeCompiler(t)
	real := realCompilerPath(t)

	writePkg := func(t *testing.T, mode string) (string, string) {
		directory := t.TempDir()
		path := filepath.Join(directory, "pkg-config")
		script := `#!/bin/sh
log=${PKG_LOG:?}
printf '%s\n' "$@" > "$log"
case ${PKG_MODE:-normal} in
  normal)
    case "$1" in
      --exists) exit 0 ;;
      --cflags) printf '%s\n' '-DFAKE -I/inc'; exit 0 ;;
      --libs) printf '%s\n' '-lfoo -lbar'; exit 0 ;;
    esac
    ;;
  fail)
    exit 1
    ;;
  empty)
    case "$1" in
      --exists) exit 0 ;;
      *) exit 0 ;;
    esac
    ;;
  odd)
    case "$1" in
      --exists) exit 0 ;;
      --cflags) printf '%s\n' '-DODD'; exit 0 ;;
      --libs) printf '%s\n' '-lodd'; exit 0 ;;
    esac
    ;;
esac
exit 1
`
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		return directory, path
	}

	cases := []struct {
		name  string
		mode  string
		found bool
	}{
		{"normal", "normal", true},
		{"failing", "fail", false},
		{"empty", "empty", false},
		{"odd", "odd", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			bin, _ := writePkg(t, testCase.mode)
			cwd := t.TempDir()
			tmp := t.TempDir()
			source := writeBuildFixture(t, cwd)
			output := filepath.Join(cwd, "app")
			logs := map[string]string{}
			for pipeline, executable := range purrs {
				log := filepath.Join(cwd, "log-"+pipeline)
				logs[pipeline] = log
				environment := []string{
					"CC=" + fakeCC,
					"REAL_CC=" + real,
					"FAKE_LOG=" + log,
					"FAKE_MODE=fake-output",
					"FAKE_OUTPUT=" + output,
					"PATH=" + bin + ":/usr/bin:/bin",
					"PKG_LOG=" + filepath.Join(cwd, "pkg-"+pipeline),
					"PKG_MODE=" + testCase.mode,
					"TMPDIR=" + tmp,
				}
				result := runBuildCommand(t, executable, cwd, []string{"build", source, "-o", output}, environment)
				if result.status != 0 {
					t.Errorf("%s pkg %s: %d %q", pipeline, testCase.name, result.status, result.stderr)
				}
			}
			argsGo := normalizeBuildLog(readFakeLog(t, logs["Go"], ".args"))
			argsKt := normalizeBuildLog(readFakeLog(t, logs["Kitten"], ".args"))
			if argsGo != argsKt {
				t.Errorf("pkg %s args differ:\nGo %s\nKitten %s", testCase.name, argsGo, argsKt)
			}
			switch testCase.mode {
			case "normal":
				if !strings.Contains(argsGo, "-lfoo") || !strings.Contains(argsGo, "-lbar") || !strings.Contains(argsGo, "-DFAKE") {
					t.Errorf("pkg normal flags missing:\n%s", argsGo)
				}
			case "odd":
				if !strings.Contains(argsGo, "-lodd") || !strings.Contains(argsGo, "-DODD") || strings.Contains(argsGo, "-lgc") {
					t.Errorf("pkg odd flags wrong:\n%s", argsGo)
				}
			default:
				if !strings.Contains(argsGo, "-lgc") {
					t.Errorf("pkg %s missing -lgc fallback:\n%s", testCase.name, argsGo)
				}
			}
		})
	}
}
