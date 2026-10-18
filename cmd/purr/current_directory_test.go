package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const currentDirectorySource = `fn report(CurrentDirectoryResult result) {
    match (result) {
        Ok(path) { print("ok", path); }
        Failed(message) { print("failed", message); }
    }
}

fn main() {
    string operation = argv()[1];
    if (operation == "cwd") {
        report(try_current_directory());
    }
    if (operation == "relative") {
        match (try_write_file("relative.txt", "relative")) {
            Ok { print("write-ok"); }
            Failed(message) { print(message); }
        }
        match (try_read_file("relative.txt")) {
            Ok(text) { print(text); }
            Failed(message) { print(message); }
        }
    }
}
`

func buildCwdGoProgram(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "cwd.ktn")
	if err := os.WriteFile(sourcePath, []byte(currentDirectorySource), 0o644); err != nil {
		t.Fatalf("writing cwd fixture: %v", err)
	}
	executable := filepath.Join(directory, "cwd")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building cwd fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}
	return executable
}

func cwdEnvironment(pwd string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "PWD=") {
			environment = append(environment, item)
		}
	}
	if pwd != "" {
		environment = append(environment, "PWD="+pwd)
	}
	return environment
}

func runCwdExternal(t *testing.T, executable string, args []string, directory, pwd string) programResult {
	t.Helper()
	command := exec.Command(executable, args...)
	command.Dir = directory
	command.Env = cwdEnvironment(pwd)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running %s: %v", executable, err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func runCwdShell(t *testing.T, script string, arguments ...string) programResult {
	t.Helper()
	args := append([]string{"-c", script, "sh"}, arguments...)
	command := exec.Command("/bin/sh", args...)
	command.Env = cwdEnvironment("/misleading")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running shell: %v", err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

const deepCwdHelperSource = `#include <unistd.h>
int main(int argc, char **argv) {
    if (argc < 3) {
        return 64;
    }
    for (int index = 1; index < argc - 1; index++) {
        if (chdir(argv[index]) != 0) {
            return 65;
        }
    }
    char *child_argv[] = { argv[argc - 1], "cwd", 0 };
    execv(argv[argc - 1], child_argv);
    return 66;
}
`

func buildDeepCwdHelper(t *testing.T, compiler []string) string {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "deep-cwd-helper.c")
	if err := os.WriteFile(source, []byte(deepCwdHelperSource), 0o644); err != nil {
		t.Fatalf("writing deep cwd helper: %v", err)
	}
	executable := filepath.Join(directory, "deep-cwd-helper")
	command := exec.Command(compiler[0], "-std=c11", "-Wall", "-Wextra", "-o", executable, source)
	var diagnostics bytes.Buffer
	command.Stdout = &diagnostics
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		t.Fatalf("building deep cwd helper: %v\n%s", err, diagnostics.String())
	}
	return executable
}

func runDeepCwd(t *testing.T, helper, root string, components []string, executable string) programResult {
	t.Helper()
	arguments := append(append([]string{}, components...), executable)
	command := exec.Command(helper, arguments...)
	command.Dir = root
	command.Env = cwdEnvironment("/misleading-pwd")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running deep cwd helper: %v", err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func TestCurrentDirectoryRuntime(t *testing.T) {
	requireToolchain(t)
	for _, program := range []string{"/bin/sh", "/bin/rmdir"} {
		if _, err := os.Stat(program); err != nil {
			t.Skipf("%s is unavailable", program)
		}
	}

	goExecutable := buildCwdGoProgram(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), currentDirectorySource, "")
	if err != nil {
		t.Fatalf("generating cwd C with the Kitten pipeline: %v", err)
	}
	_, _, referenceText, ok := checkProgram(t, "cwd runtime", currentDirectorySource, "")
	if !ok {
		return
	}
	if difference := unitDifference(kittenText, referenceText); difference != "" {
		t.Fatalf("the cwd translation unit differs: %s", difference)
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
	deepHelper := buildDeepCwdHelper(t, compiler)

	realDirectory := t.TempDir()
	realPath, err := filepath.EvalSymlinks(realDirectory)
	if err != nil {
		t.Fatalf("resolving real directory: %v", err)
	}
	symlink := filepath.Join(t.TempDir(), "cwd-link")
	if err := os.Symlink(realDirectory, symlink); err != nil {
		t.Fatalf("creating cwd symlink: %v", err)
	}
	symlinkPath, err := filepath.EvalSymlinks(symlink)
	if err != nil {
		t.Fatalf("resolving symlink cwd: %v", err)
	}

	normalWant := "ok " + realPath + "\n"
	for _, testCase := range []struct {
		name       string
		executable string
		args       []string
		directory  string
		want       string
		pwd        string
	}{
		{"normal cwd", "", []string{"cwd"}, realDirectory, normalWant, "/misleading-pwd"},
		{"symlink cwd resolves", "", []string{"cwd"}, symlink, "ok " + symlinkPath + "\n", "/misleading-pwd"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			goResult := runCwdExternal(t, goExecutable, testCase.args, testCase.directory, testCase.pwd)
			kittenResult := runCwdExternal(t, kittenExecutable, testCase.args, testCase.directory, testCase.pwd)
			if goResult.status != 0 || goResult.stderr != "" || goResult.stdout != testCase.want {
				t.Errorf("Go pipeline: stdout %q, stderr %q, status %d; want %q, %q, 0",
					goResult.stdout, goResult.stderr, goResult.status, testCase.want, "")
			}
			if kittenResult.status != 0 || kittenResult.stderr != "" || kittenResult.stdout != testCase.want {
				t.Errorf("Kitten pipeline: stdout %q, stderr %q, status %d; want %q, %q, 0",
					kittenResult.stdout, kittenResult.stderr, kittenResult.status, testCase.want, "")
			}
			if goResult.stdout != kittenResult.stdout || goResult.stderr != kittenResult.stderr || goResult.status != kittenResult.status {
				t.Errorf("pipelines disagree: Go %q/%q/%d; Kitten %q/%q/%d",
					goResult.stdout, goResult.stderr, goResult.status,
					kittenResult.stdout, kittenResult.stderr, kittenResult.status)
			}
		})
	}

	t.Run("relative paths still work", func(t *testing.T) {
		directory := t.TempDir()
		want := "write-ok\nrelative\n"
		for _, executable := range []string{goExecutable, kittenExecutable} {
			result := runCwdExternal(t, executable, []string{"relative"}, directory, "/misleading-pwd")
			if result.status != 0 || result.stderr != "" || result.stdout != want {
				t.Errorf("%s: stdout %q, stderr %q, status %d; want %q, %q, 0",
					executable, result.stdout, result.stderr, result.status, want, "")
			}
			data, err := os.ReadFile(filepath.Join(directory, "relative.txt"))
			if err != nil || string(data) != "relative" {
				t.Errorf("%s: relative.txt = %q, %v; want %q", executable, data, err, "relative")
			}
			if err := os.Remove(filepath.Join(directory, "relative.txt")); err != nil {
				t.Fatalf("removing relative.txt: %v", err)
			}
		}
	})

	t.Run("deep cwd grows the buffer", func(t *testing.T) {
		original, err := os.Getwd()
		if err != nil {
			t.Fatalf("getting original cwd: %v", err)
		}
		root := t.TempDir()
		components := make([]string, 0, 30)
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir root: %v", err)
		}
		for index := 0; index < 30; index++ {
			name := fmt.Sprintf("%03d-%s", index, strings.Repeat("x", 180))
			if err := os.Mkdir(name, 0o755); err != nil {
				_ = os.Chdir(original)
				t.Fatalf("creating deep component: %v", err)
			}
			if err := os.Chdir(name); err != nil {
				_ = os.Chdir(original)
				t.Fatalf("chdir deep component: %v", err)
			}
			components = append(components, name)
		}
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restoring cwd: %v", err)
		}
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatalf("resolving deep root: %v", err)
		}
		deepPath := filepath.Join(append([]string{resolvedRoot}, components...)...)
		if len(deepPath) <= 4096 {
			t.Fatalf("deep cwd length = %d, want > 4096", len(deepPath))
		}
		want := "ok " + deepPath + "\n"
		for _, executable := range []string{goExecutable, kittenExecutable} {
			result := runDeepCwd(t, deepHelper, root, components, executable)
			if result.status != 0 || result.stderr != "" || result.stdout != want {
				t.Errorf("%s: stdout %q, stderr %q, status %d; want %q, %q, 0",
					executable, result.stdout, result.stderr, result.status, want, "")
			}
		}
	})

	t.Run("deleted cwd fails with Go text", func(t *testing.T) {
		original, err := os.Getwd()
		if err != nil {
			t.Fatalf("getting original cwd: %v", err)
		}
		expectedDoomed := t.TempDir()
		if err := os.Chdir(expectedDoomed); err != nil {
			t.Fatalf("chdir expected doomed: %v", err)
		}
		if err := os.Remove(expectedDoomed); err != nil {
			_ = os.Chdir(original)
			t.Fatalf("removing expected doomed cwd: %v", err)
		}
		_, getwdErr := syscall.Getwd()
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restoring cwd: %v", err)
		}
		if getwdErr == nil {
			t.Fatalf("syscall.Getwd succeeded for a deleted cwd")
		}
		want := "failed getwd: " + getwdErr.Error() + "\n"
		script := "cd \"$1\" && rmdir \"$1\" && exec \"$2\" cwd"
		for _, executable := range []string{goExecutable, kittenExecutable} {
			doomed := t.TempDir()
			result := runCwdShell(t, script, doomed, executable)
			if result.status != 0 || result.stderr != "" || result.stdout != want {
				t.Errorf("%s: stdout %q, stderr %q, status %d; want %q, %q, 0",
					executable, result.stdout, result.stderr, result.status, want, "")
			}
		}
	})
}

func TestCurrentDirectorySanitizer(t *testing.T) {
	requireToolchain(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), currentDirectorySource, "")
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
	result := runCwdExternal(t, executable, []string{"cwd"}, t.TempDir(), "/misleading-pwd")
	if result.status != 0 || result.stderr != "" || !strings.HasPrefix(result.stdout, "ok /") {
		t.Errorf("sanitizer run: stdout %q, stderr %q, status %d", result.stdout, result.stderr, result.status)
	}
}
