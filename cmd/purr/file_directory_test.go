package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const fileDirectorySource = `fn report(FileResult result) {
    match (result) {
        Ok { print("ok"); }
        Failed(message) { print("failed", message); }
    }
}

fn report_list(ListDirectoryResult result) {
    match (result) {
        Ok(entries) {
            print("ok");
            print(length(entries));
            int index = 0;
            while (index < length(entries)) {
                print(entries[index]);
                index++;
            }
        }
        Failed(message) { print("failed", message); }
    }
}

fn main() {
    string operation = argv()[1];
    if (operation == "write") {
        report(try_write_file(argv()[2], argv()[3]));
    }
    if (operation == "writebytes") {
        byte[] data = new byte[3];
        data[0] = byte_from_int(0);
        data[1] = byte_from_int(10);
        data[2] = byte_from_int(255);
        report(try_write_bytes(argv()[2], data));
    }
    if (operation == "mkdir") {
        report(try_create_directory(argv()[2]));
    }
    if (operation == "remove") {
        report(try_remove(argv()[2]));
    }
    if (operation == "list") {
        report_list(try_list_directory(argv()[2]));
    }
}
`

type fileDirectoryCase struct {
	name       string
	operation  string
	path       string
	text       string
	setup      func(t *testing.T, root string)
	wantOutput func(t *testing.T, root string) string
	sideEffect func(t *testing.T, root string) string
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func fileContents(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "missing:" + err.Error()
	}
	info, err := os.Stat(path)
	if err != nil {
		return "stat-error:" + err.Error()
	}
	return fmt.Sprintf("%q mode %o", data, info.Mode().Perm())
}

func pathState(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return "missing:" + err.Error()
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(path)
		if readErr != nil {
			return "link-error:" + readErr.Error()
		}
		return "link:" + target
	}
	if info.IsDir() {
		return fmt.Sprintf("dir %o", info.Mode().Perm())
	}
	return "file"
}

func listOutput(t *testing.T, path string) string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		return "failed " + err.Error() + "\n"
	}
	var output strings.Builder
	output.WriteString("ok\n")
	output.WriteString(strconv.Itoa(len(entries)))
	output.WriteString("\n")
	for _, entry := range entries {
		output.WriteString(entry.Name())
		output.WriteString("\n")
	}
	return output.String()
}

func goWriteError(path string, data []byte) string {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "failed " + err.Error() + "\n"
	}
	return "unexpected success\n"
}

func goMkdirError(path string) string {
	if err := os.Mkdir(path, 0o755); err != nil {
		return "failed " + err.Error() + "\n"
	}
	return "unexpected success\n"
}

func goRemoveError(path string) string {
	if err := os.Remove(path); err != nil {
		return "failed " + err.Error() + "\n"
	}
	return "unexpected success\n"
}

func rootPath(root, name string) string {
	return filepath.Join(root, name)
}

func fileDirectoryCases() []fileDirectoryCase {
	return []fileDirectoryCase{
		{
			name:      "write text and overwrite",
			operation: "write",
			path:      "text.txt",
			text:      "hello\nworld\n",
			setup: func(t *testing.T, root string) {
				writeTestFile(t, rootPath(root, "text.txt"), "old contents that are longer")
			},
			wantOutput: func(t *testing.T, root string) string { return "ok\n" },
			sideEffect: func(t *testing.T, root string) string {
				return fileContents(rootPath(root, "text.txt"))
			},
		},
		{
			name:       "write empty text",
			operation:  "write",
			path:       "empty.txt",
			text:       "",
			setup:      func(t *testing.T, root string) {},
			wantOutput: func(t *testing.T, root string) string { return "ok\n" },
			sideEffect: func(t *testing.T, root string) string {
				return fileContents(rootPath(root, "empty.txt"))
			},
		},
		{
			name:      "write into missing directory",
			operation: "write",
			path:      "missing/child.txt",
			text:      "x",
			setup:     func(t *testing.T, root string) {},
			wantOutput: func(t *testing.T, root string) string {
				return goWriteError(rootPath(root, "missing/child.txt"), []byte("x"))
			},
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "missing/child.txt"))
			},
		},
		{
			name:      "write onto directory",
			operation: "write",
			path:      "directory",
			text:      "x",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(rootPath(root, "directory"), 0o755); err != nil {
					t.Fatalf("creating directory: %v", err)
				}
			},
			wantOutput: func(t *testing.T, root string) string {
				return goWriteError(rootPath(root, "directory"), []byte("x"))
			},
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "directory"))
			},
		},
		{
			name:       "write bytes including NUL newline and high byte",
			operation:  "writebytes",
			path:       "bytes.bin",
			setup:      func(t *testing.T, root string) {},
			wantOutput: func(t *testing.T, root string) string { return "ok\n" },
			sideEffect: func(t *testing.T, root string) string {
				return fileContents(rootPath(root, "bytes.bin"))
			},
		},
		{
			name:      "write bytes into missing directory",
			operation: "writebytes",
			path:      "missing/bytes.bin",
			setup:     func(t *testing.T, root string) {},
			wantOutput: func(t *testing.T, root string) string {
				return goWriteError(rootPath(root, "missing/bytes.bin"), []byte{0, 10, 255})
			},
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "missing/bytes.bin"))
			},
		},
		{
			name:       "create directory",
			operation:  "mkdir",
			path:       "newdir",
			setup:      func(t *testing.T, root string) {},
			wantOutput: func(t *testing.T, root string) string { return "ok\n" },
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "newdir"))
			},
		},
		{
			name:      "create existing directory",
			operation: "mkdir",
			path:      "existing",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(rootPath(root, "existing"), 0o755); err != nil {
					t.Fatalf("creating directory: %v", err)
				}
			},
			wantOutput: func(t *testing.T, root string) string {
				return goMkdirError(rootPath(root, "existing"))
			},
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "existing"))
			},
		},
		{
			name:      "create under missing parent",
			operation: "mkdir",
			path:      "missing/child",
			setup:     func(t *testing.T, root string) {},
			wantOutput: func(t *testing.T, root string) string {
				return goMkdirError(rootPath(root, "missing/child"))
			},
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "missing/child"))
			},
		},
		{
			name:      "remove file",
			operation: "remove",
			path:      "remove.txt",
			setup: func(t *testing.T, root string) {
				writeTestFile(t, rootPath(root, "remove.txt"), "x")
			},
			wantOutput: func(t *testing.T, root string) string { return "ok\n" },
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "remove.txt"))
			},
		},
		{
			name:      "remove empty directory",
			operation: "remove",
			path:      "empty-dir",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(rootPath(root, "empty-dir"), 0o755); err != nil {
					t.Fatalf("creating directory: %v", err)
				}
			},
			wantOutput: func(t *testing.T, root string) string { return "ok\n" },
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "empty-dir"))
			},
		},
		{
			name:      "remove non-empty directory",
			operation: "remove",
			path:      "nonempty",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(rootPath(root, "nonempty"), 0o755); err != nil {
					t.Fatalf("creating directory: %v", err)
				}
				writeTestFile(t, rootPath(root, "nonempty/child"), "x")
			},
			wantOutput: func(t *testing.T, root string) string {
				return goRemoveError(rootPath(root, "nonempty"))
			},
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "nonempty/child"))
			},
		},
		{
			name:      "remove missing path",
			operation: "remove",
			path:      "missing",
			setup:     func(t *testing.T, root string) {},
			wantOutput: func(t *testing.T, root string) string {
				return goRemoveError(rootPath(root, "missing"))
			},
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "missing"))
			},
		},
		{
			name:      "remove symbolic link itself",
			operation: "remove",
			path:      "link",
			setup: func(t *testing.T, root string) {
				writeTestFile(t, rootPath(root, "target"), "target")
				if err := os.Symlink(rootPath(root, "target"), rootPath(root, "link")); err != nil {
					t.Fatalf("creating symlink: %v", err)
				}
			},
			wantOutput: func(t *testing.T, root string) string { return "ok\n" },
			sideEffect: func(t *testing.T, root string) string {
				return pathState(rootPath(root, "target")) + "|" + pathState(rootPath(root, "link"))
			},
		},
		{
			name:      "list sorted names",
			operation: "list",
			path:      "sorted",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(rootPath(root, "sorted"), 0o755); err != nil {
					t.Fatalf("creating directory: %v", err)
				}
				for _, name := range []string{"c", "a", "b"} {
					writeTestFile(t, rootPath(root, "sorted/"+name), name)
				}
			},
			wantOutput: func(t *testing.T, root string) string {
				return listOutput(t, rootPath(root, "sorted"))
			},
			sideEffect: func(t *testing.T, root string) string { return "" },
		},
		{
			name:      "list empty directory",
			operation: "list",
			path:      "empty-list",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(rootPath(root, "empty-list"), 0o755); err != nil {
					t.Fatalf("creating directory: %v", err)
				}
			},
			wantOutput: func(t *testing.T, root string) string {
				return listOutput(t, rootPath(root, "empty-list"))
			},
			sideEffect: func(t *testing.T, root string) string { return "" },
		},
		{
			name:      "list directory with subdirectory",
			operation: "list",
			path:      "with-subdir",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(rootPath(root, "with-subdir"), 0o755); err != nil {
					t.Fatalf("creating directory: %v", err)
				}
				writeTestFile(t, rootPath(root, "with-subdir/z.txt"), "z")
				if err := os.Mkdir(rootPath(root, "with-subdir/nested"), 0o755); err != nil {
					t.Fatalf("creating subdirectory: %v", err)
				}
			},
			wantOutput: func(t *testing.T, root string) string {
				return listOutput(t, rootPath(root, "with-subdir"))
			},
			sideEffect: func(t *testing.T, root string) string { return "" },
		},
		{
			name:      "list missing path",
			operation: "list",
			path:      "missing-list",
			setup:     func(t *testing.T, root string) {},
			wantOutput: func(t *testing.T, root string) string {
				return listOutput(t, rootPath(root, "missing-list"))
			},
			sideEffect: func(t *testing.T, root string) string { return "" },
		},
		{
			name:      "list regular file",
			operation: "list",
			path:      "list-file",
			setup: func(t *testing.T, root string) {
				writeTestFile(t, rootPath(root, "list-file"), "x")
			},
			wantOutput: func(t *testing.T, root string) string {
				return listOutput(t, rootPath(root, "list-file"))
			},
			sideEffect: func(t *testing.T, root string) string {
				return fileContents(rootPath(root, "list-file"))
			},
		},
	}
}

func resetFileDirectoryRoot(t *testing.T, root string) {
	t.Helper()
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("removing test root: %v", err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("creating test root: %v", err)
	}
}

func runKittenFileDirectory(t *testing.T, compiler, compileFlags, linkFlags []string, text string, index int, args []string) programResult {
	t.Helper()
	result, err := buildAndRunProgramWithArgs(t, compiler, compileFlags, linkFlags, t.TempDir(), text, index, args)
	if err != nil {
		t.Fatalf("building and running the Kitten-generated program: %v", err)
	}
	return result
}

// applyReference performs the Go os equivalent of one case on the prepared root.
// It supplies the expected side effect; errors are expected for failure cases and
// are checked through the case's wanted output instead.
func applyReference(t *testing.T, testCase fileDirectoryCase, root string) {
	t.Helper()
	path := rootPath(root, testCase.path)
	switch testCase.operation {
	case "write":
		_ = os.WriteFile(path, []byte(testCase.text), 0o644)
	case "writebytes":
		_ = os.WriteFile(path, []byte{0, 10, 255}, 0o644)
	case "mkdir":
		_ = os.Mkdir(path, 0o755)
	case "remove":
		_ = os.Remove(path)
	}
}

func TestFileDirectoryRuntime(t *testing.T) {
	requireToolchain(t)

	root := t.TempDir()
	sourceDirectory := t.TempDir()
	sourcePath := filepath.Join(sourceDirectory, "file_directory.ktn")
	if err := os.WriteFile(sourcePath, []byte(fileDirectorySource), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	goExecutable := filepath.Join(t.TempDir(), "go-file-directory")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, goExecutable, &diagnostics); err != nil {
		t.Fatalf("building the Go fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}

	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), fileDirectorySource, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	_, _, referenceText, ok := checkProgram(t, "file directory runtime", fileDirectorySource, "")
	if !ok {
		return
	}
	if difference := unitDifference(kittenText, referenceText); difference != "" {
		t.Fatalf("the file-directory translation unit differs: %s", difference)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	cases := fileDirectoryCases()
	if os.Geteuid() != 0 {
		cases = append(cases,
			fileDirectoryCase{
				name:      "write permission denied",
				operation: "write",
				path:      "locked/file.txt",
				text:      "x",
				setup: func(t *testing.T, root string) {
					if err := os.Mkdir(rootPath(root, "locked"), 0o555); err != nil {
						t.Fatalf("creating locked directory: %v", err)
					}
					t.Cleanup(func() { _ = os.Chmod(rootPath(root, "locked"), 0o755) })
				},
				wantOutput: func(t *testing.T, root string) string {
					return goWriteError(rootPath(root, "locked/file.txt"), []byte("x"))
				},
				sideEffect: func(t *testing.T, root string) string {
					return pathState(rootPath(root, "locked/file.txt"))
				},
			},
			fileDirectoryCase{
				name:      "list permission denied",
				operation: "list",
				path:      "unreadable",
				setup: func(t *testing.T, root string) {
					if err := os.Mkdir(rootPath(root, "unreadable"), 0o000); err != nil {
						t.Fatalf("creating unreadable directory: %v", err)
					}
					t.Cleanup(func() { _ = os.Chmod(rootPath(root, "unreadable"), 0o755) })
				},
				wantOutput: func(t *testing.T, root string) string {
					return listOutput(t, rootPath(root, "unreadable"))
				},
				sideEffect: func(t *testing.T, root string) string { return "" },
			},
		)
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := rootPath(root, testCase.path)
			args := []string{testCase.operation, path}
			if testCase.operation == "write" {
				args = append(args, testCase.text)
			}

			resetFileDirectoryRoot(t, root)
			testCase.setup(t, root)
			wantOutput := testCase.wantOutput(t, root)
			applyReference(t, testCase, root)
			wantSideEffect := testCase.sideEffect(t, root)

			resetFileDirectoryRoot(t, root)
			testCase.setup(t, root)
			goResult := runExternal(t, goExecutable, args, root, nil)
			goSideEffect := testCase.sideEffect(t, root)

			resetFileDirectoryRoot(t, root)
			testCase.setup(t, root)
			kittenResult := runKittenFileDirectory(t, compiler, compileFlags, linkFlags, kittenText, index, args)
			kittenSideEffect := testCase.sideEffect(t, root)

			if goResult.status != 0 || goResult.stderr != "" || goResult.stdout != wantOutput {
				t.Errorf("Go pipeline: status %d, stderr %q, stdout %q; want status 0, stderr %q, stdout %q",
					goResult.status, goResult.stderr, goResult.stdout, "", wantOutput)
			}
			if kittenResult.status != 0 || kittenResult.stderr != "" || kittenResult.stdout != wantOutput {
				t.Errorf("Kitten pipeline: status %d, stderr %q, stdout %q; want status 0, stderr %q, stdout %q",
					kittenResult.status, kittenResult.stderr, kittenResult.stdout, "", wantOutput)
			}
			if goSideEffect != wantSideEffect {
				t.Errorf("Go pipeline side effect %q; want %q", goSideEffect, wantSideEffect)
			}
			if kittenSideEffect != wantSideEffect {
				t.Errorf("Kitten pipeline side effect %q; want %q", kittenSideEffect, wantSideEffect)
			}
			if goResult.stdout != kittenResult.stdout || goResult.stderr != kittenResult.stderr || goResult.status != kittenResult.status {
				t.Errorf("pipelines disagree: Go stdout %q stderr %q status %d; Kitten stdout %q stderr %q status %d",
					goResult.stdout, goResult.stderr, goResult.status,
					kittenResult.stdout, kittenResult.stderr, kittenResult.status)
			}
			if goSideEffect != kittenSideEffect {
				t.Errorf("pipelines disagree on side effects: Go %q; Kitten %q", goSideEffect, kittenSideEffect)
			}
		})
	}
}

const fileDirectoryLeakSource = `fn main() {
    int index = 0;
    string message = "";
    while (index < 2000) {
        match (try_write_file("/dev/full", "x")) {
            Ok { message = "unexpected success"; }
            Failed(value) { message = value; }
        }
        index++;
    }
    print(message);
}
`

func TestFileDirectoryNoDescriptorLeak(t *testing.T) {
	requireToolchain(t)
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full is unavailable")
	}

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "leak.ktn")
	if err := os.WriteFile(sourcePath, []byte(fileDirectoryLeakSource), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	executable := filepath.Join(directory, "leak")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building leak fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}

	writeErr := os.WriteFile("/dev/full", []byte("x"), 0o644)
	if writeErr == nil {
		t.Fatalf("os.WriteFile(/dev/full) succeeded, want an error")
	}
	want := writeErr.Error() + "\n"

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
	if status != 0 || stderr.Len() != 0 || stdout.String() != want {
		t.Errorf("status %d, stderr %q, stdout %q; want status 0, stderr %q, stdout %q",
			status, stderr.String(), stdout.String(), "", want)
	}
}

func TestFileDirectorySanitizer(t *testing.T) {
	requireToolchain(t)

	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), fileDirectorySource, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")

	result, err := buildAndRunProgramWithArgs(t, compiler, compileFlags, linkFlags, t.TempDir(), kittenText, 0, []string{"list", t.TempDir()})
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	if result.stderr != "" || result.status != 0 {
		t.Errorf("the sanitizer reported a problem: stderr %q, status %d", result.stderr, result.status)
	}
}
