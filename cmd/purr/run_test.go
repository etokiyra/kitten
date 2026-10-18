package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kitten/runtime"
)

const runSource = `fn report(RunResult result) {
    match (result) {
        Ok(status) { print("ok", status); }
        Failed(message) { print("failed", message); }
    }
}

fn main() {
    string operation = argv()[1];
    if (operation == "run") {
        string[] args = new string[length(argv()) - 3];
        int index = 3;
        while (index < length(argv())) {
            args[index - 3] = argv()[index];
            index++;
        }
        report(try_run(argv()[2], args));
    }
    if (operation == "ordering") {
        print("before");
        string[] args = new string[2];
        args[0] = "-c";
        args[1] = "printf 'child-out\\n'; printf 'child-err\\n' >&2";
        RunResult result = try_run("/bin/sh", args);
        print("after");
        report(result);
    }
    if (operation == "argv0") {
        string[] args = new string[2];
        args[0] = "-c";
        args[1] = "printf '%s' \"$0\"";
        report(try_run("/bin/sh", args));
    }
}
`

const runZombieSource = `fn main() {
    int index = 0;
    while (index < 2000) {
        try_run("/bin/true", new string[0]);
        index++;
    }
    string[] args = new string[1];
    args[0] = "1";
    try_run("/bin/sleep", args);
}
`

const runLeakSource = `fn main() {
    int index = 0;
    while (index < 2000) {
        match (try_run("/definitely/missing-kitten-run", new string[0])) {
            Ok(status) { }
            Failed(message) { }
        }
        index++;
    }
    int failures = 0;
    index = 0;
    while (index < 2000) {
        match (try_run("/bin/true", new string[0])) {
            Ok(status) { if (status != 0) { failures++; } }
            Failed(message) { failures++; }
        }
        index++;
    }
    print(failures);
}
`

func buildRunGoProgram(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "run.ktn")
	if err := os.WriteFile(sourcePath, []byte(runSource), 0o644); err != nil {
		t.Fatalf("writing run fixture: %v", err)
	}
	executable := filepath.Join(directory, "run")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building run fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}
	return executable
}

func buildKittenRunProgram(t *testing.T, compiler, compileFlags, linkFlags []string, text string) (string, error) {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "program.c"), []byte(text), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(directory, runtime.SourceFileName), []byte(runtime.Source()), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(directory, runtime.HeaderFileName), []byte(runtime.HeaderSource()), 0o644); err != nil {
		return "", err
	}
	arguments := append([]string{}, compiler[1:]...)
	arguments = append(arguments, "-std=c11", "-Wall", "-Wextra")
	arguments = append(arguments, compileFlags...)
	arguments = append(arguments, "-o", "program", "program.c", runtime.SourceFileName)
	arguments = append(arguments, linkFlags...)
	command := exec.Command(compiler[0], arguments...)
	command.Dir = directory
	var diagnostics bytes.Buffer
	command.Stdout = &diagnostics
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("building Kitten-generated C: %v\n%s", err, diagnostics.String())
	}
	if output := strings.TrimSpace(diagnostics.String()); output != "" {
		return "", fmt.Errorf("C compiler diagnostics: %s", output)
	}
	return filepath.Join(directory, "program"), nil
}

func startProcessError(t *testing.T, path string) string {
	t.Helper()
	process, err := os.StartProcess(path, []string{path}, &os.ProcAttr{})
	if err == nil {
		_, _ = process.Wait()
		t.Fatalf("os.StartProcess(%q) succeeded, want a failure", path)
	}
	return err.Error()
}

func TestRunRuntime(t *testing.T) {
	requireToolchain(t)
	for _, program := range []string{"/bin/sh", "/bin/true", "/bin/false"} {
		if _, err := os.Stat(program); err != nil {
			t.Skipf("%s is unavailable", program)
		}
	}

	goExecutable := buildRunGoProgram(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), runSource, "")
	if err != nil {
		t.Fatalf("generating try_run C with the Kitten pipeline: %v", err)
	}
	_, _, referenceText, ok := checkProgram(t, "try_run runtime", runSource, "")
	if !ok {
		return
	}
	if difference := unitDifference(kittenText, referenceText); difference != "" {
		t.Fatalf("the try_run translation unit differs: %s", difference)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	kittenExecutable, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, kittenText)
	if err != nil {
		t.Fatalf("%v", err)
	}

	working := t.TempDir()
	nonexec := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(nonexec, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatalf("writing non-executable file: %v", err)
	}
	directory := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	permissionPath := filepath.Join(t.TempDir(), "permission-denied")
	if err := os.WriteFile(permissionPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing permission fixture: %v", err)
	}
	if os.Geteuid() != 0 {
		if err := os.Chmod(permissionPath, 0o000); err != nil {
			t.Fatalf("chmod permission fixture: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(permissionPath, 0o755) })
	}

	argsCase := []string{
		"-c", `printf '%s|' "$@"`, "sh", "a b", "", "c",
	}
	cases := []struct {
		name        string
		args        []string
		directory   string
		environment []string
		stdout      string
		stderr      string
	}{
		{"true", []string{"run", "/bin/true"}, working, nil, "ok 0\n", ""},
		{"false", []string{"run", "/bin/false"}, working, nil, "ok 1\n", ""},
		{"shell exit 3", []string{"run", "/bin/sh", "-c", "exit 3"}, working, nil, "ok 3\n", ""},
		{"signal", []string{"run", "/bin/sh", "-c", "kill -9 $$"}, working, nil, "ok 137\n", ""},
		{"arguments intact", append([]string{"run", "/bin/sh"}, argsCase...), working, nil, "a b||c|ok 0\n", ""},
		{"argv0 is the program", []string{"argv0"}, working, nil, "/bin/shok 0\n", ""},
		{"environment inherited", []string{"run", "/bin/sh", "-c", `printf '%s\n' "$KITTEN_RUN_VALUE"`}, working, []string{"KITTEN_RUN_VALUE=from-test"}, "from-test\nok 0\n", ""},
		{"working directory inherited", []string{"run", "/bin/pwd"}, working, nil, working + "\nok 0\n", ""},
		{"output ordering", []string{"ordering"}, working, nil, "before\nchild-out\nafter\nok 0\n", "child-err\n"},
		{"missing program", []string{"run", missing}, working, nil, "failed " + startProcessError(t, missing) + "\n", ""},
		{"non-executable program", []string{"run", nonexec}, working, nil, "failed " + startProcessError(t, nonexec) + "\n", ""},
		{"directory is not executable", []string{"run", directory}, working, nil, "failed " + startProcessError(t, directory) + "\n", ""},
		{"no PATH search", []string{"run", "true"}, working, nil, "failed " + startProcessError(t, "true") + "\n", ""},
	}
	if os.Geteuid() != 0 {
		cases = append(cases, struct {
			name        string
			args        []string
			directory   string
			environment []string
			stdout      string
			stderr      string
		}{"permission denied", []string{"run", permissionPath}, working, nil, "failed " + startProcessError(t, permissionPath) + "\n", ""})
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			goResult := runExternal(t, goExecutable, testCase.args, testCase.directory, testCase.environment)
			kittenResult := runExternal(t, kittenExecutable, testCase.args, testCase.directory, testCase.environment)
			if goResult.stdout != testCase.stdout || goResult.stderr != testCase.stderr || goResult.status != 0 {
				t.Errorf("Go pipeline: stdout %q, stderr %q, status %d; want %q, %q, 0",
					goResult.stdout, goResult.stderr, goResult.status, testCase.stdout, testCase.stderr)
			}
			if kittenResult.stdout != testCase.stdout || kittenResult.stderr != testCase.stderr || kittenResult.status != 0 {
				t.Errorf("Kitten pipeline: stdout %q, stderr %q, status %d; want %q, %q, 0",
					kittenResult.stdout, kittenResult.stderr, kittenResult.status, testCase.stdout, testCase.stderr)
			}
			if goResult.stdout != kittenResult.stdout || goResult.stderr != kittenResult.stderr || goResult.status != kittenResult.status {
				t.Errorf("pipelines disagree: Go stdout %q stderr %q status %d; Kitten stdout %q stderr %q status %d",
					goResult.stdout, goResult.stderr, goResult.status, kittenResult.stdout, kittenResult.stderr, kittenResult.status)
			}
		})
	}
}

func TestRunLeakAndZombies(t *testing.T) {
	requireToolchain(t)
	if _, err := os.Stat("/bin/true"); err != nil {
		t.Skip("/bin/true is unavailable")
	}

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "leak.ktn")
	if err := os.WriteFile(sourcePath, []byte(runLeakSource), 0o644); err != nil {
		t.Fatalf("writing leak fixture: %v", err)
	}
	executable := filepath.Join(directory, "leak")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building leak fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}

	command := exec.Command("/bin/sh", "-c", "ulimit -n 16; exec \"$1\"", "sh", executable)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running leak fixture: %v", err)
		}
		status = exit.ExitCode()
	}
	if status != 0 || stderr.Len() != 0 || stdout.String() != "0\n" {
		t.Errorf("status %d, stderr %q, stdout %q; want status 0, stderr %q, stdout %q",
			status, stderr.String(), stdout.String(), "", "0\n")
	}
}

func TestRunNoZombies(t *testing.T) {
	requireToolchain(t)
	for _, program := range []string{"/bin/true", "/bin/sleep"} {
		if _, err := os.Stat(program); err != nil {
			t.Skipf("%s is unavailable", program)
		}
	}

	sourcePath := filepath.Join(t.TempDir(), "zombie.ktn")
	if err := os.WriteFile(sourcePath, []byte(runZombieSource), 0o644); err != nil {
		t.Fatalf("writing zombie fixture: %v", err)
	}
	executable := filepath.Join(t.TempDir(), "zombie")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building zombie fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}

	command := exec.Command(executable)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("starting zombie fixture: %v", err)
	}
	childrenPath := fmt.Sprintf("/proc/%d/task/%d/children", command.Process.Pid, command.Process.Pid)
	deadline := time.Now().Add(10 * time.Second)
	var children []string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(childrenPath)
		if err == nil {
			children = strings.Fields(string(data))
			if len(children) > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(children) != 1 {
		t.Errorf("child list at %s has %d entries (%v), want one sleeping child and no zombies", childrenPath, len(children), children)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("waiting for zombie fixture: %v (stderr %s)", err, stderr.String())
	}
}

func TestRunSanitizer(t *testing.T) {
	requireToolchain(t)
	if _, err := os.Stat("/bin/true"); err != nil {
		t.Skip("/bin/true is unavailable")
	}

	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), runSource, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")
	executable, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, kittenText)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	result := runExternal(t, executable, []string{"run", "/bin/true"}, t.TempDir(), nil)
	if result.status != 0 || result.stderr != "" || result.stdout != "ok 0\n" {
		t.Errorf("sanitizer run: stdout %q, stderr %q, status %d; want %q, %q, 0",
			result.stdout, result.stderr, result.status, "ok 0\n", "")
	}
}
