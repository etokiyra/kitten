package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kitten/checker"
	"kitten/parser"
)

const captureSource = `fn report(CapturedRunResult result) {
    match (result) {
        Ok(status, out, err) { print("ok", status, length(out), length(err)); }
        Failed(message) { print("failed", message); }
    }
}

fn values(CapturedRunResult result) {
    match (result) {
        Ok(status, out, err) { print("status", status); print(out); print(err); }
        Failed(message) { print("failed", message); }
    }
}

fn args_from(int start) -> string[] {
    string[] arguments = new string[length(argv()) - start];
    int index = start;
    while (index < length(argv())) {
        arguments[index - start] = argv()[index];
        index++;
    }
    return arguments;
}

fn main() {
    string operation = argv()[1];
    if (operation == "report") {
        report(try_run_capture(argv()[2], args_from(3)));
    }
    if (operation == "values") {
        values(try_run_capture(argv()[2], args_from(3)));
    }
    if (operation == "ordering") {
        print("before");
        string[] child = new string[2];
        child[0] = "-c";
        child[1] = "printf childout; printf childerr >&2";
        CapturedRunResult result = try_run_capture("/bin/sh", child);
        print("after");
        report(result);
    }
}
`

func buildCaptureGoProgram(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "capture.ktn")
	if err := os.WriteFile(sourcePath, []byte(captureSource), 0o644); err != nil {
		t.Fatalf("writing capture fixture: %v", err)
	}
	executable := filepath.Join(directory, "capture")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building capture fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}
	return executable
}

func runCapture(t *testing.T, executable, directory string, environment []string, args []string) programResult {
	t.Helper()
	command := exec.Command(executable, args...)
	command.Dir = directory
	if environment != nil {
		command.Env = environment
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running capture fixture: %v", err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func captureCase(t *testing.T, goExecutable, kittenExecutable, directory string, environment []string, args []string, wantStdout, wantStderr string) {
	t.Helper()
	goResult := runCapture(t, goExecutable, directory, environment, args)
	kittenResult := runCapture(t, kittenExecutable, directory, environment, args)
	if goResult.stdout != wantStdout || goResult.stderr != wantStderr || goResult.status != 0 {
		t.Errorf("Go pipeline: stdout %q, stderr %q, status %d; want %q, %q, 0",
			goResult.stdout, goResult.stderr, goResult.status, wantStdout, wantStderr)
	}
	if kittenResult.stdout != wantStdout || kittenResult.stderr != wantStderr || kittenResult.status != 0 {
		t.Errorf("Kitten pipeline: stdout %q, stderr %q, status %d; want %q, %q, 0",
			kittenResult.stdout, kittenResult.stderr, kittenResult.status, wantStdout, wantStderr)
	}
	if goResult.stdout != kittenResult.stdout || goResult.stderr != kittenResult.stderr || goResult.status != kittenResult.status {
		t.Errorf("pipelines disagree: Go %q/%q/%d; Kitten %q/%q/%d",
			goResult.stdout, goResult.stderr, goResult.status, kittenResult.stdout, kittenResult.stderr, kittenResult.status)
	}
}

func TestCaptureRuntime(t *testing.T) {
	requireToolchain(t)
	for _, program := range []string{"/bin/sh", "/bin/cat", "/bin/true", "/bin/yes", "/bin/head", "/bin/sleep"} {
		if _, err := os.Stat(program); err != nil {
			t.Skipf("%s is unavailable", program)
		}
	}
	goExecutable := buildCaptureGoProgram(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), captureSource, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	_, _, referenceText, ok := checkProgram(t, "capture runtime", captureSource, "")
	if !ok {
		return
	}
	if difference := unitDifference(kittenText, referenceText); difference != "" {
		t.Fatalf("capture translation unit differs: %s", difference)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering compiler: %v", err)
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
	permissionPath := filepath.Join(t.TempDir(), "permission")
	if err := os.WriteFile(permissionPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing permission fixture: %v", err)
	}
	if os.Geteuid() != 0 {
		if err := os.Chmod(permissionPath, 0o000); err != nil {
			t.Fatalf("chmod permission fixture: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(permissionPath, 0o755) })
	}

	t.Run("output streams and status", func(t *testing.T) {
		args := []string{"values", "/bin/sh", "-c", "printf out; printf err >&2; exit 3"}
		captureCase(t, goExecutable, kittenExecutable, working, nil, args, "status 3\nout\nerr\n", "")
	})
	t.Run("empty output", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"values", "/bin/true"}, "status 0\n\n\n", "")
	})
	t.Run("signal status and partial output", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"values", "/bin/sh", "-c", "printf part; kill -9 $$"}, "status 137\npart\n\n", "")
	})
	t.Run("stdin is dev null", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/cat"}, "ok 0 0 0\n", "")
	})
	t.Run("arguments intact", func(t *testing.T) {
		args := []string{"values", "/bin/sh", "-c", `printf '%s|' "$@"`, "sh", "a b", "", "c"}
		captureCase(t, goExecutable, kittenExecutable, working, nil, args, "status 0\na b||c|\n\n", "")
	})
	t.Run("environment inherited", func(t *testing.T) {
		environment := append(os.Environ(), "KITTEN_CAPTURE_ENV=a b")
		args := []string{"values", "/bin/sh", "-c", `printf '%s' "$KITTEN_CAPTURE_ENV"`}
		captureCase(t, goExecutable, kittenExecutable, working, environment, args, "status 0\na b\n\n", "")
	})
	t.Run("working directory inherited", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"values", "/bin/pwd"}, "status 0\n"+working+"\n\n\n", "")
	})
	t.Run("ordering and capture", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"ordering"}, "before\nafter\nok 0 8 8\n", "")
	})
	t.Run("missing program", func(t *testing.T) {
		want := "failed " + startProcessError(t, missing) + "\n"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", missing}, want, "")
	})
	t.Run("non-executable program", func(t *testing.T) {
		want := "failed " + startProcessError(t, nonexec) + "\n"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", nonexec}, want, "")
	})
	t.Run("directory program", func(t *testing.T) {
		want := "failed " + startProcessError(t, directory) + "\n"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", directory}, want, "")
	})
	t.Run("no PATH search", func(t *testing.T) {
		want := "failed " + startProcessError(t, "true") + "\n"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "true"}, want, "")
	})
	if os.Geteuid() != 0 {
		t.Run("permission denied", func(t *testing.T) {
			want := "failed " + startProcessError(t, permissionPath) + "\n"
			captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", permissionPath}, want, "")
		})
	}
	t.Run("NUL in stdout", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", "printf 'A\\0B'"}, "failed run /bin/sh: stdout contains a NUL byte at offset 1\n", "")
	})
	t.Run("NUL at offset zero", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", "printf '\\0B'"}, "failed run /bin/sh: stdout contains a NUL byte at offset 0\n", "")
	})
	t.Run("NUL at end", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", "printf 'AB\\0'"}, "failed run /bin/sh: stdout contains a NUL byte at offset 2\n", "")
	})
	t.Run("NUL in stderr at end", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", "printf 'AB\\0' >&2"}, "failed run /bin/sh: stderr contains a NUL byte at offset 2\n", "")
	})
	t.Run("bytes above 0x7F", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"values", "/bin/sh", "-c", "printf '\\303\\251'"}, "status 0\n\303\251\n\n", "")
	})
	t.Run("NUL in stderr", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", "printf 'A\\0B' >&2"}, "failed run /bin/sh: stderr contains a NUL byte at offset 1\n", "")
	})
	t.Run("child closes stdout early", func(t *testing.T) {
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", "exec 1>&-; sleep 0.05; printf err >&2"}, "ok 0 0 3\n", "")
	})
	t.Run("large balanced streams", func(t *testing.T) {
		script := "yes e | head -c 1048576 >&2; yes o | head -c 1048576"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", script}, "ok 0 1048576 1048576\n", "")
	})
	t.Run("large stdout then stderr", func(t *testing.T) {
		script := "yes o | head -c 1048576; yes e | head -c 1048576 >&2"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", script}, "ok 0 1048576 1048576\n", "")
	})
	t.Run("exact stdout cap", func(t *testing.T) {
		script := "yes x | head -c 16777216"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", script}, "ok 0 16777216 0\n", "")
	})
	t.Run("stdout over cap", func(t *testing.T) {
		script := "yes x | head -c 16777217"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", script}, "failed run /bin/sh: stdout exceeds the 16777216 byte limit\n", "")
	})
	t.Run("stderr over cap", func(t *testing.T) {
		script := "yes x | head -c 16777217 >&2"
		captureCase(t, goExecutable, kittenExecutable, working, nil, []string{"report", "/bin/sh", "-c", script}, "failed run /bin/sh: stderr exceeds the 16777216 byte limit\n", "")
	})
}

const captureLeakSource = `fn main() {
    int index = 0;
    int failures = 0;
    while (index < 500) {
        match (try_run_capture("/bin/true", new string[0])) {
            Ok(status, out, err) { if (status != 0 || length(out) != 0 || length(err) != 0) { failures++; } }
            Failed(message) { failures++; }
        }
        index++;
    }
    index = 0;
    while (index < 500) {
        match (try_run_capture("/definitely/missing-capture", new string[0])) {
            Ok(status, out, err) { failures++; }
            Failed(message) { }
        }
        index++;
    }
    print(failures);
}
`

func TestCaptureLeak(t *testing.T) {
	requireToolchain(t)
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "leak.ktn")
	if err := os.WriteFile(sourcePath, []byte(captureLeakSource), 0o644); err != nil {
		t.Fatalf("writing capture leak fixture: %v", err)
	}
	executable := filepath.Join(directory, "leak")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building capture leak fixture: %v\n%s", err, diagnostics.String())
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
			t.Fatalf("running capture leak fixture: %v", err)
		}
		status = exit.ExitCode()
	}
	if status != 0 || stderr.Len() != 0 || stdout.String() != "0\n" {
		t.Errorf("status %d, stderr %q, stdout %q; want status 0, stderr %q, stdout %q",
			status, stderr.String(), stdout.String(), "", "0\n")
	}
}

func TestCaptureSanitizer(t *testing.T) {
	requireToolchain(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), captureSource, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")
	executable, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, kittenText)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	result := runCapture(t, executable, t.TempDir(), nil, []string{"values", "/bin/sh", "-c", "printf out; printf err >&2; exit 3"})
	if result.status != 0 || result.stderr != "" || result.stdout != "status 3\nout\nerr\n" {
		t.Errorf("sanitizer run: stdout %q, stderr %q, status %d", result.stdout, result.stderr, result.status)
	}
}

const captureZombieSource = `fn main() {
    int index = 0;
    while (index < 100) {
        try_run_capture("/bin/true", new string[0]);
        index++;
    }
    string[] cap = new string[2];
    cap[0] = "-c";
    cap[1] = "yes x | head -c 16777217";
    try_run_capture("/bin/sh", cap);
    string[] args = new string[1];
    args[0] = "1";
    try_run("/bin/sleep", args);
}
`

func TestCaptureNoZombies(t *testing.T) {
	requireToolchain(t)
	for _, program := range []string{"/bin/true", "/bin/sleep"} {
		if _, err := os.Stat(program); err != nil {
			t.Skipf("%s is unavailable", program)
		}
	}
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "zombie.ktn")
	if err := os.WriteFile(sourcePath, []byte(captureZombieSource), 0o644); err != nil {
		t.Fatalf("writing capture zombie fixture: %v", err)
	}
	executable := filepath.Join(directory, "zombie")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building capture zombie fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}
	command := exec.Command(executable)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("starting capture zombie fixture: %v", err)
	}
	childrenPath := "/proc/" + itoa(command.Process.Pid) + "/task/" + itoa(command.Process.Pid) + "/children"
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
		t.Fatalf("waiting for capture zombie fixture: %v (stderr %s)", err, stderr.String())
	}
}

func TestCaptureWithRuntimeModuleIDs(t *testing.T) {
	runtimeSource, err := os.ReadFile(filepath.Join("..", "..", "runtime", "ktn", "memory.ktn"))
	if err != nil {
		t.Fatalf("reading runtime helper: %v", err)
	}
	source := `fn main() {
    try_run_capture("/bin/true", new string[0]);
    copy_bytes(new byte[0]);
}
`
	modules := []*parser.Module{
		{Path: "input.txt", ID: 0, Program: parseTestModuleForRuntime(t, source).Program},
		{Path: "input2.txt", ID: 1, Program: &parser.Program{}},
	}
	checked, err := checker.CheckModulesWithRuntime(modules, string(runtimeSource))
	if err != nil {
		t.Fatalf("checking capture with runtime: %v", err)
	}
	if len(checked.Modules) != 4 {
		t.Fatalf("module count = %d, want root, import, prelude, runtime", len(checked.Modules))
	}
	if checked.Modules[2].Path != "<prelude>" || checked.Modules[2].ID != 2 {
		t.Errorf("prelude = %q id %d", checked.Modules[2].Path, checked.Modules[2].ID)
	}
	if checked.Modules[3].Path != "<runtime>" || checked.Modules[3].ID != 3 {
		t.Errorf("runtime = %q id %d", checked.Modules[3].Path, checked.Modules[3].ID)
	}
}
