package main

// Tests for purr-kt run.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeRunFixture(t *testing.T, directory, name, source string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("writing run fixture %s: %v", name, err)
	}
	return path
}

var runDirectoryPattern = regexp.MustCompile(`purr-run[.-][A-Za-z0-9]+`)

// normalizeRunTempPaths makes compiler diagnostics comparable by replacing the
// random build and run temporary directory names. It is used only where the
// compiler reports its own argv, which necessarily contains those paths.
func normalizeRunTempPaths(text string) string {
	text = buildDirectoryPattern.ReplaceAllString(text, "purr-build.X")
	return runDirectoryPattern.ReplaceAllString(text, "purr-run.X")
}

func TestKittenPurrRun(t *testing.T) {
	requireToolchain(t)
	goPurr := buildGoPurr(t)
	stage1 := buildStage1(t)
	purrs := map[string]string{
		"Go-built":     buildPurrKTWithGo(t),
		"Kitten-built": buildPurrKTWithKitten(t, stage1),
	}

	directory := t.TempDir()
	hello := writeRunFixture(t, directory, "hello.ktn",
		"fn main() {\n    print(\"run-out\");\n    eprint(\"run-err\");\n}\n")
	statusOne := writeRunFixture(t, directory, "status1.ktn",
		"fn main() {\n    print(\"one\");\n    exit(1);\n}\n")
	statusSeven := writeRunFixture(t, directory, "status7.ktn",
		"fn main() {\n    print(\"seven\");\n    exit(7);\n}\n")
	statusHigh := writeRunFixture(t, directory, "status200.ktn",
		"fn main() {\n    print(\"two hundred\");\n    exit(200);\n}\n")
	statusMax := writeRunFixture(t, directory, "status255.ktn",
		"fn main() {\n    print(\"two hundred fifty five\");\n    exit(255);\n}\n")
	bad := writeRunFixture(t, directory, "bad.ktn",
		"fn main() {\n    int value = \"not an int\";\n}\n")
	syntax := writeRunFixture(t, directory, "syntax.ktn",
		"fn main() {\n    print(\"missing semicolon\")\n}\n")
	missing := filepath.Join(directory, "missing.ktn")
	directorySource := filepath.Join(directory, "directory.ktn")
	if err := os.Mkdir(directorySource, 0o755); err != nil {
		t.Fatalf("creating directory fixture: %v", err)
	}
	unreadable := writeRunFixture(t, directory, "unreadable.ktn",
		"fn main() {\n    print(\"unreadable\");\n}\n")
	if os.Geteuid() != 0 {
		if err := os.Chmod(unreadable, 0o000); err != nil {
			t.Fatalf("making unreadable fixture: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })
	}

	type runCase struct {
		name         string
		arguments    []string
		status       int
		stdout       string
		exactStderr  string
		prefixStderr string
		usage        bool
	}
	const outputRejected = "purr run: -o is not supported; run compiles to a temporary executable\n"
	cases := []runCase{
		{"success", []string{"run", hello}, 0, "run-out\n", "", "", false},
		{"double dash", []string{"run", "--", hello}, 0, "run-out\n", "", "", false},
		{"empty output flag", []string{"run", "-o=", hello}, 0, "run-out\n", "", "", false},
		{"empty long output flag", []string{"run", "--output=", hello}, 0, "run-out\n", "", "", false},
		{"status 1", []string{"run", statusOne}, 1, "one\n", "", "", false},
		{"status 7", []string{"run", statusSeven}, 7, "seven\n", "", "", false},
		{"status 200", []string{"run", statusHigh}, 200, "two hundred\n", "", "", false},
		{"status 255", []string{"run", statusMax}, 255, "two hundred fifty five\n", "", "", false},
		{
			name:        "output rejected",
			arguments:   []string{"run", "-o", "out", hello},
			status:      2,
			exactStderr: outputRejected,
		},
		{
			name:        "output rejected long",
			arguments:   []string{"run", "--output", "out", hello},
			status:      2,
			exactStderr: outputRejected,
		},
		{
			name:        "output rejected equals",
			arguments:   []string{"run", "-o=out", hello},
			status:      2,
			exactStderr: outputRejected,
		},
		{
			name:        "output rejected long equals",
			arguments:   []string{"run", "--output=out", hello},
			status:      2,
			exactStderr: outputRejected,
		},
		{
			name:        "output rejected after source",
			arguments:   []string{"run", hello, "-o", "out"},
			status:      2,
			exactStderr: outputRejected,
		},
		{
			name:         "missing source file",
			arguments:    []string{"run", missing},
			status:       1,
			prefixStderr: missing + ": cannot read source file: ",
		},
		{
			name:         "directory source",
			arguments:    []string{"run", directorySource},
			status:       1,
			prefixStderr: directorySource + ": cannot read source file: ",
		},
		{
			name:         "compile failure",
			arguments:    []string{"run", bad},
			status:       1,
			prefixStderr: bad + ":2:",
		},
		{
			name:         "syntax failure",
			arguments:    []string{"run", syntax},
			status:       1,
			prefixStderr: syntax + ":",
		},
		{
			name:        "extension",
			arguments:   []string{"run", filepath.Join(directory, "hello")},
			status:      2,
			exactStderr: "purr run: \"" + filepath.Join(directory, "hello") + "\" is not a Kitten source file (expected a .ktn file)\n",
		},
		{
			name:        "uppercase extension",
			arguments:   []string{"run", filepath.Join(directory, "hello.KTN")},
			status:      2,
			exactStderr: "purr run: \"" + filepath.Join(directory, "hello.KTN") + "\" is not a Kitten source file (expected a .ktn file)\n",
		},
		{
			name:        "terminator keeps flag as source",
			arguments:   []string{"run", "--", "-o"},
			status:      2,
			exactStderr: "purr run: \"-o\" is not a Kitten source file (expected a .ktn file)\n",
		},
		{"missing source argument", []string{"run"}, 2, "", "", "", true},
		{"missing source after terminator", []string{"run", "--"}, 2, "", "", "", true},
		{"missing output value", []string{"run", "-o"}, 2, "", "", "", true},
		{"missing output value long", []string{"run", "--output"}, 2, "", "", "", true},
		{"unknown option", []string{"run", "--wat", hello}, 2, "", "", "", true},
		{"unknown short option", []string{"run", "-x", hello}, 2, "", "", "", true},
		{"two sources", []string{"run", hello, hello}, 2, "", "", "", true},
	}
	if os.Geteuid() != 0 {
		cases = append(cases, runCase{
			name:         "unreadable source",
			arguments:    []string{"run", unreadable},
			status:       1,
			prefixStderr: unreadable + ": cannot read source file: ",
		})
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			goResult := runExternal(t, goPurr, testCase.arguments, directory, nil)
			if goResult.status != testCase.status || goResult.stdout != testCase.stdout {
				t.Fatalf("Go reference: status %d, stdout %q; want %d, %q",
					goResult.status, goResult.stdout, testCase.status, testCase.stdout)
			}
			for pipeline, purr := range purrs {
				result := runExternal(t, purr, testCase.arguments, directory, nil)
				if result.status != testCase.status || result.stdout != testCase.stdout {
					t.Errorf("%s: status %d, stdout %q; want %d, %q",
						pipeline, result.status, result.stdout, testCase.status, testCase.stdout)
				}
				switch {
				case testCase.exactStderr != "":
					if result.stderr != testCase.exactStderr {
						t.Errorf("%s: stderr %q, want %q", pipeline, result.stderr, testCase.exactStderr)
					}
				case testCase.usage:
					if normalizeBuildUsage(t, result.stderr) != normalizeBuildUsage(t, goResult.stderr) {
						t.Errorf("%s: usage stderr %q differs from Go %q",
							pipeline, result.stderr, goResult.stderr)
					}
				default:
					if result.stderr != goResult.stderr {
						t.Errorf("%s: stderr %q differs from Go %q",
							pipeline, result.stderr, goResult.stderr)
					}
				}
				if testCase.prefixStderr != "" && !strings.HasPrefix(result.stderr, testCase.prefixStderr) {
					t.Errorf("%s: stderr %q does not start with %q",
						pipeline, result.stderr, testCase.prefixStderr)
				}
			}
		})
	}

	t.Run("stdin passthrough", func(t *testing.T) {
		if _, err := os.Stat("/dev/stdin"); err != nil {
			t.Skip("/dev/stdin is unavailable")
		}
		// read_file requires a seekable regular file, so stdin is a regular
		// temp file here. This still verifies that purr-kt leaves fd 0
		// connected to the caller's stdin.
		stdinSource := writeRunFixture(t, directory, "stdin.ktn",
			"fn main() {\n    print(read_file(\"/dev/stdin\"));\n}\n")
		inputPath := filepath.Join(directory, "stdin-input.txt")
		if err := os.WriteFile(inputPath, []byte("stdin-data"), 0o644); err != nil {
			t.Fatalf("writing stdin input: %v", err)
		}
		runWithStdin := func(executable string) programResult {
			input, err := os.Open(inputPath)
			if err != nil {
				t.Fatalf("opening stdin input: %v", err)
			}
			defer input.Close()
			return runExternalWithInput(t, executable, []string{"run", stdinSource}, directory, input, nil)
		}
		goResult := runWithStdin(goPurr)
		if goResult.status != 0 || goResult.stdout != "stdin-data\n" || goResult.stderr != "" {
			t.Fatalf("Go reference: status %d, stdout %q, stderr %q; want 0, %q, \"\"",
				goResult.status, goResult.stdout, goResult.stderr, "stdin-data\n")
		}
		for pipeline, purr := range purrs {
			result := runWithStdin(purr)
			if result.status != 0 || result.stdout != "stdin-data\n" || result.stderr != "" {
				t.Errorf("%s: status %d, stdout %q, stderr %q; want 0, %q, \"\"",
					pipeline, result.status, result.stdout, result.stderr, "stdin-data\n")
			}
		}
	})

	t.Run("signal termination", func(t *testing.T) {
		if _, err := os.Stat("/bin/sh"); err != nil {
			t.Skip("/bin/sh is unavailable")
		}
		// The compiled program asks /bin/sh to SIGKILL its parent, so the run
		// command's direct child is terminated by SIGKILL. Go's
		// exec.ExitError.ExitCode() returns -1 for a signaled child, and
		// os.Exit(-1) is observed as 255 on POSIX. purr-kt uses try_run's
		// specified 128+signal mapping, so SIGKILL is 137. The difference is
		// intentional and unavoidable while that semantics remains the
		// language contract.
		signalSource := writeRunFixture(t, directory, "signal.ktn",
			"fn main() {\n"+
				"    string[] arguments = new string[2];\n"+
				"    arguments[0] = \"-c\";\n"+
				"    arguments[1] = \"kill -9 $PPID\";\n"+
				"    match (try_run(\"/bin/sh\", arguments)) {\n"+
				"        Ok(status) {\n"+
				"            print(\"not signaled\");\n"+
				"            exit(status);\n"+
				"        }\n"+
				"        Failed(message) {\n"+
				"            eprint(message);\n"+
				"            exit(1);\n"+
				"        }\n"+
				"    }\n"+
				"}\n")
		goResult := runExternal(t, goPurr, []string{"run", signalSource}, directory, nil)
		if goResult.status != 255 {
			t.Fatalf("Go reference: status %d, stdout %q, stderr %q; want 255",
				goResult.status, goResult.stdout, goResult.stderr)
		}
		if goResult.stdout != "" || goResult.stderr != "" {
			t.Fatalf("Go reference: stdout %q, stderr %q; want both empty",
				goResult.stdout, goResult.stderr)
		}
		for pipeline, purr := range purrs {
			result := runExternal(t, purr, []string{"run", signalSource}, directory, nil)
			if result.status != 137 {
				t.Errorf("%s: status %d, want 137 (128+SIGKILL); stdout %q, stderr %q",
					pipeline, result.status, result.stdout, result.stderr)
			}
			if result.stdout != "" || result.stderr != "" {
				t.Errorf("%s: stdout %q, stderr %q; want both empty",
					pipeline, result.stdout, result.stderr)
			}
		}
	})

	t.Run("cleanup note", func(t *testing.T) {
		// The child writes a file into its own temporary directory. Go removes
		// the whole directory recursively, so the file disappears without a
		// note. purr-kt removes the executable and then the directory
		// non-recursively, leaving the directory and reporting a note.
		cleanupSource := writeRunFixture(t, directory, "cleanup.ktn",
			"fn directory_of(string path) -> string {\n"+
				"    int index = length(path);\n"+
				"    while (index > 0 && path[index - 1] != 47) {\n"+
				"        index--;\n"+
				"    }\n"+
				"    return substring(path, 0, index);\n"+
				"}\n"+
				"\n"+
				"fn main() {\n"+
				"    string directory = directory_of(argv()[0]);\n"+
				"    if (directory == \"\") {\n"+
				"        eprint(\"executable path has no directory\");\n"+
				"        exit(1);\n"+
				"    }\n"+
				"    match (try_write_file(directory + \"/leftover\", \"kept\")) {\n"+
				"        Ok {}\n"+
				"        Failed(message) {\n"+
				"            eprint(message);\n"+
				"            exit(1);\n"+
				"        }\n"+
				"    }\n"+
				"    print(\"cleanup note\");\n"+
				"}\n")
		goTmp := t.TempDir()
		goResult := runExternal(t, goPurr, []string{"run", cleanupSource}, directory,
			[]string{"TMPDIR=" + goTmp})
		if goResult.status != 0 || goResult.stdout != "cleanup note\n" || goResult.stderr != "" {
			t.Fatalf("Go reference: status %d, stdout %q, stderr %q; want 0, %q, \"\"",
				goResult.status, goResult.stdout, goResult.stderr, "cleanup note\n")
		}
		if entries, err := os.ReadDir(goTmp); err != nil {
			t.Fatal(err)
		} else if len(entries) != 0 {
			t.Errorf("Go left temp entries: %v", entries)
		}
		for pipeline, purr := range purrs {
			tmp := t.TempDir()
			result := runExternal(t, purr, []string{"run", cleanupSource}, directory,
				[]string{"TMPDIR=" + tmp})
			if result.status != 0 || result.stdout != "cleanup note\n" {
				t.Errorf("%s: status %d, stdout %q; want 0, %q",
					pipeline, result.status, result.stdout, "cleanup note\n")
				continue
			}
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			runDirectory := ""
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "purr-run") {
					if runDirectory != "" {
						t.Errorf("%s: multiple purr-run entries", pipeline)
					}
					runDirectory = filepath.Join(tmp, entry.Name())
				} else {
					t.Errorf("%s: unexpected temp entry %q", pipeline, entry.Name())
				}
			}
			if runDirectory == "" {
				t.Errorf("%s: no purr-run directory left for the note", pipeline)
				continue
			}
			wantNote := "       note: could not remove " + runDirectory +
				": remove " + runDirectory + ": directory not empty\n"
			if result.stderr != wantNote {
				t.Errorf("%s: stderr %q, want %q", pipeline, result.stderr, wantNote)
			}
			leftovers, err := os.ReadDir(runDirectory)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, entry := range leftovers {
				names = append(names, entry.Name())
			}
			if len(names) != 1 || names[0] != "leftover" {
				t.Errorf("%s: leftover directory entries %v, want only leftover", pipeline, names)
			}
			if _, err := os.Stat(filepath.Join(runDirectory, "program")); !os.IsNotExist(err) {
				t.Errorf("%s: temporary executable was not removed (stat error %v)", pipeline, err)
			}
		}
	})

	t.Run("compiler diagnostic", func(t *testing.T) {
		fake := writeFakeCompiler(t)
		// One shared TMPDIR keeps the stable parent path identical across the
		// comparison; only the random purr-build/purr-run components differ.
		tmp := t.TempDir()
		runCompilerFailure := func(executable, label string) programResult {
			log := filepath.Join(directory, "compiler-"+label)
			result := runExternal(t, executable, []string{"run", hello}, directory,
				fakeBuildEnvironment(t, fake, log, "args-fail", "", tmp))
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "purr-run") || strings.HasPrefix(entry.Name(), "purr-build") {
					t.Errorf("%s: left temp entry %q", label, entry.Name())
				}
			}
			return result
		}
		goResult := runCompilerFailure(goPurr, "Go")
		if goResult.status != 1 || goResult.stdout != "" {
			t.Fatalf("Go compiler failure: status %d, stdout %q", goResult.status, goResult.stdout)
		}
		if !strings.Contains(goResult.stderr, "C compilation failed: exit status 1") ||
			!strings.Contains(goResult.stderr, "fake compiler diagnostic") {
			t.Fatalf("Go compiler failure stderr %q", goResult.stderr)
		}
		if !strings.Contains(goResult.stderr, "purr-build") || !strings.Contains(goResult.stderr, "purr-run") {
			t.Fatalf("Go compiler failure stderr does not contain temporary paths: %q", goResult.stderr)
		}
		want := normalizeRunTempPaths(goResult.stderr)
		for pipeline, purr := range purrs {
			result := runCompilerFailure(purr, pipeline)
			if result.status != 1 || result.stdout != "" {
				t.Errorf("%s compiler failure: status %d, stdout %q", pipeline, result.status, result.stdout)
			}
			if normalizeRunTempPaths(result.stderr) != want {
				t.Errorf("%s compiler failure stderr %q; want %q (normalized)",
					pipeline, normalizeRunTempPaths(result.stderr), want)
			}
			if !strings.Contains(result.stderr, "fake compiler diagnostic") ||
				!strings.Contains(result.stderr, "purr-build") ||
				!strings.Contains(result.stderr, "purr-run") {
				t.Errorf("%s compiler failure stderr %q", pipeline, result.stderr)
			}
		}
	})
}

func TestKittenPurrRunTempCleanup(t *testing.T) {
	requireToolchain(t)
	kt := buildPurrKTWithGo(t)
	directory := t.TempDir()
	source := writeRunFixture(t, directory, "hello.ktn",
		"fn main() {\n    print(\"Hello, world!\");\n}\n")
	tmp := t.TempDir()
	environment := []string{"TMPDIR=" + tmp}
	for _, testCase := range []struct {
		name      string
		arguments []string
		status    int
	}{
		{"success", []string{"run", source}, 0},
		{"failure", []string{"run", filepath.Join(directory, "missing.ktn")}, 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := runExternal(t, kt, testCase.arguments, directory, environment)
			if result.status != testCase.status {
				t.Fatalf("status %d, want %d; stderr %q", result.status, testCase.status, result.stderr)
			}
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "purr-run") || strings.HasPrefix(entry.Name(), "purr-build") {
					t.Errorf("left temp entry %q", entry.Name())
				}
			}
		})
	}
}
