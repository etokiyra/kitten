package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const fileInfoSource = `fn main() {
    match (try_file_info(argv()[1])) {
        Ok(is_file, is_directory, is_symlink, executable, size) {
            print("ok", is_file, is_directory, is_symlink, executable, size);
        }
        Missing { print("missing"); }
        Failed(message) { print("failed", message); }
    }
}
`

const fileInfoLoopSource = `fn main() {
    int index = 0;
    while (index < 10000) {
        try_file_info(argv()[1]);
        index++;
    }
    print("done");
}
`

func buildFileInfoGoProgram(t *testing.T, source string) string {
	t.Helper()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "file_info.ktn")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("writing file-info fixture: %v", err)
	}
	executable := filepath.Join(directory, "file-info")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building file-info fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}
	return executable
}

func isMissingError(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func goFileInfo(path string) string {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		if isMissingError(err) {
			return "missing\n"
		}
		return "failed " + err.Error() + "\n"
	}
	symlink := linkInfo.Mode()&os.ModeSymlink != 0
	followed := linkInfo
	if symlink {
		followed, err = os.Stat(path)
		if err != nil {
			if isMissingError(err) {
				return "ok false false true false 0\n"
			}
			return "failed " + err.Error() + "\n"
		}
	}
	isFile := followed.Mode().IsRegular()
	isDirectory := followed.IsDir()
	size := 0
	if isFile {
		size = int(followed.Size())
	}
	executable := syscall.Access(path, 1) == nil
	return fmt.Sprintf("ok %t %t %t %t %d\n", isFile, isDirectory, symlink, executable, size)
}

func runFileInfo(t *testing.T, executable, path string) programResult {
	t.Helper()
	return runExternal(t, executable, []string{path}, t.TempDir(), nil)
}

func TestFileInfoRuntime(t *testing.T) {
	requireToolchain(t)
	goExecutable := buildFileInfoGoProgram(t, fileInfoSource)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), fileInfoSource, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	_, _, referenceText, ok := checkProgram(t, "file-info runtime", fileInfoSource, "")
	if !ok {
		return
	}
	if difference := unitDifference(kittenText, referenceText); difference != "" {
		t.Fatalf("file-info translation unit differs: %s", difference)
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

	root := t.TempDir()
	regular := filepath.Join(root, "regular.txt")
	if err := os.WriteFile(regular, []byte("abc"), 0o644); err != nil {
		t.Fatalf("writing regular file: %v", err)
	}
	executable := filepath.Join(root, "executable")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing executable file: %v", err)
	}
	empty := filepath.Join(root, "empty")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("writing empty file: %v", err)
	}
	large := filepath.Join(root, "large")
	if err := os.WriteFile(large, bytes.Repeat([]byte("x"), 2*1024*1024), 0o644); err != nil {
		t.Fatalf("writing large file: %v", err)
	}
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("creating directory: %v", err)
	}
	linkFile := filepath.Join(root, "link-file")
	if err := os.Symlink(regular, linkFile); err != nil {
		t.Fatalf("creating file symlink: %v", err)
	}
	linkDirectory := filepath.Join(root, "link-directory")
	if err := os.Symlink(directory, linkDirectory); err != nil {
		t.Fatalf("creating directory symlink: %v", err)
	}
	linkExecutable := filepath.Join(root, "link-executable")
	if err := os.Symlink(executable, linkExecutable); err != nil {
		t.Fatalf("creating executable symlink: %v", err)
	}
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "no-target"), dangling); err != nil {
		t.Fatalf("creating dangling symlink: %v", err)
	}
	loop := filepath.Join(root, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatalf("creating symlink loop: %v", err)
	}
	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("creating fifo: %v", err)
	}
	longPath := filepath.Join(root, strings.Repeat("x", 5000))

	cases := []struct {
		name string
		path string
	}{
		{"regular", regular},
		{"regular executable", executable},
		{"empty file", empty},
		{"large file", large},
		{"directory", directory},
		{"file symlink", linkFile},
		{"directory symlink", linkDirectory},
		{"executable symlink", linkExecutable},
		{"dangling symlink", dangling},
		{"symlink loop", loop},
		{"fifo", fifo},
		{"missing", filepath.Join(root, "missing")},
		{"path through file", filepath.Join(regular, "child")},
		{"trailing slash on file", regular + "/"},
		{"empty path", ""},
		{"very long path", longPath},
	}
	if os.Geteuid() != 0 {
		locked := filepath.Join(root, "locked")
		if err := os.Mkdir(locked, 0o000); err != nil {
			t.Fatalf("creating locked directory: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		cases = append(cases, struct {
			name string
			path string
		}{"permission denied", filepath.Join(locked, "child")})
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			want := goFileInfo(testCase.path)
			goResult := runFileInfo(t, goExecutable, testCase.path)
			kittenResult := runFileInfo(t, kittenExecutable, testCase.path)
			if goResult.status != 0 || goResult.stderr != "" || goResult.stdout != want {
				t.Errorf("Go pipeline: stdout %q, stderr %q, status %d; want %q, %q, 0",
					goResult.stdout, goResult.stderr, goResult.status, want, "")
			}
			if kittenResult.status != 0 || kittenResult.stderr != "" || kittenResult.stdout != want {
				t.Errorf("Kitten pipeline: stdout %q, stderr %q, status %d; want %q, %q, 0",
					kittenResult.stdout, kittenResult.stderr, kittenResult.status, want, "")
			}
			if goResult.stdout != kittenResult.stdout || goResult.stderr != kittenResult.stderr || goResult.status != kittenResult.status {
				t.Errorf("pipelines disagree: Go %q/%q/%d; Kitten %q/%q/%d",
					goResult.stdout, goResult.stderr, goResult.status,
					kittenResult.stdout, kittenResult.stderr, kittenResult.status)
			}
		})
	}
}

func TestFileInfoLoopAndNoFds(t *testing.T) {
	requireToolchain(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "file")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	executable := buildFileInfoGoProgram(t, fileInfoLoopSource)
	command := exec.Command("/bin/sh", "-c", "ulimit -n 16; exec \"$1\" \"$2\"", "sh", executable, path)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running file-info loop: %v", err)
		}
		status = exit.ExitCode()
	}
	if status != 0 || stderr.Len() != 0 || stdout.String() != "done\n" {
		t.Errorf("status %d, stderr %q, stdout %q; want status 0, stderr %q, stdout %q",
			status, stderr.String(), stdout.String(), "", "done\n")
	}
}

func TestFileInfoSanitizer(t *testing.T) {
	requireToolchain(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), fileInfoSource, "")
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
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("writing sanitizer file: %v", err)
	}
	result := runFileInfo(t, executable, path)
	want := goFileInfo(path)
	if result.status != 0 || result.stderr != "" || result.stdout != want {
		t.Errorf("sanitizer run: stdout %q, stderr %q, status %d; want %q", result.stdout, result.stderr, result.status, want)
	}
}
