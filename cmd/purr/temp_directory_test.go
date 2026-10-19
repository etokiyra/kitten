package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const tempDirectorySource = `fn main() {
    match (try_create_temp_directory(argv()[1])) {
        Ok(path) { print("ok", path); }
        Failed(message) { print("failed", message); }
    }
}
`

const tempDirectoryTwoSource = `fn main() {
    int index = 0;
    while (index < 2) {
        match (try_create_temp_directory("purr")) {
            Ok(path) { print(path); }
            Failed(message) { print("failed", message); }
        }
        index++;
    }
}
`

const tempDirectoryManySource = `fn main() {
    int index = 0;
    while (index < 1000) {
        match (try_create_temp_directory("purr")) {
            Ok(path) {
                print(path);
                match (try_remove(path)) {
                    Ok { }
                    Failed(message) { print("remove failed", message); }
                }
            }
            Failed(message) { print("create failed", message); }
        }
        index++;
    }
}
`

func buildTempDirectoryPrograms(t *testing.T, source string) (string, string) {
	t.Helper()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "temp_directory.ktn")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("writing temp-directory fixture: %v", err)
	}
	goExecutable := filepath.Join(directory, "temp-directory-go")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, goExecutable, &diagnostics); err != nil {
		t.Fatalf("building Go temp-directory fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}

	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), source, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	_, _, referenceText, ok := checkProgram(t, "temp-directory fixture", source, "")
	if !ok {
		t.Fatalf("reference pipeline rejected the temp-directory fixture")
	}
	if difference := unitDifference(kittenText, referenceText); difference != "" {
		t.Fatalf("temp-directory translation unit differs: %s", difference)
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
	return goExecutable, kittenExecutable
}

func environmentWithTMPDIR(value *string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "TMPDIR=") {
			continue
		}
		environment = append(environment, entry)
	}
	if value != nil {
		environment = append(environment, "TMPDIR="+*value)
	}
	return environment
}

func runTempDirectory(t *testing.T, executable, directory string, environment []string, prefix string) programResult {
	t.Helper()
	command := exec.Command(executable, prefix)
	command.Dir = directory
	command.Env = environment
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

func runTempDirectoryWithUmask(t *testing.T, executable, directory string, environment []string, prefix, mask string) programResult {
	t.Helper()
	command := exec.Command("/bin/sh", "-c", "umask "+mask+"; exec \"$1\" \"$2\"", "sh", executable, prefix)
	command.Dir = directory
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running %s under umask %s: %v", executable, mask, err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func runTempDirectoryWithFdLimit(t *testing.T, executable, directory string, environment []string, prefix string) programResult {
	t.Helper()
	command := exec.Command("/bin/sh", "-c", "ulimit -n 16; exec \"$1\" \"$2\"", "sh", executable, prefix)
	command.Dir = directory
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running %s with a low descriptor limit: %v", executable, err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func parseTempDirectoryResult(t *testing.T, result programResult) (bool, string) {
	t.Helper()
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("status %d, stderr %q", result.status, result.stderr)
	}
	line := strings.TrimSuffix(result.stdout, "\n")
	if line == "failed invalid temporary directory prefix" {
		return false, "invalid temporary directory prefix"
	}
	if strings.HasPrefix(line, "ok ") {
		return true, strings.TrimPrefix(line, "ok ")
	}
	if strings.HasPrefix(line, "failed ") {
		return false, strings.TrimPrefix(line, "failed ")
	}
	t.Fatalf("unexpected temp-directory output %q", result.stdout)
	return false, ""
}

func checkTempDirectoryPath(t *testing.T, path, base, prefix string) {
	t.Helper()
	if !filepath.IsAbs(path) {
		t.Errorf("path %q is not absolute", path)
	}
	information, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !information.IsDir() {
		t.Errorf("%q is not a directory", path)
	}
	if information.Mode().Perm() != 0o700 {
		t.Errorf("mode = %#o, want 0700", information.Mode().Perm())
	}
	if stat, ok := information.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		t.Errorf("owner = %v, want uid %d", information.Sys(), os.Geteuid())
	}
	if filepath.Dir(path) != filepath.Clean(base) {
		t.Errorf("parent = %q, want %q", filepath.Dir(path), filepath.Clean(base))
	}
	name := filepath.Base(path)
	if !strings.HasPrefix(name, prefix+".") {
		t.Errorf("name %q does not start with %q", name, prefix+".")
	}
	suffix := strings.TrimPrefix(name, prefix+".")
	if len(suffix) != 6 {
		t.Errorf("suffix %q is not six bytes", suffix)
	}
	for index := 0; index < len(suffix); index++ {
		value := suffix[index]
		allowed := (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9')
		if !allowed {
			t.Errorf("suffix byte %q is not an ASCII letter or digit", value)
		}
	}
}

func normalizeGoTempError(base, prefix, message string) string {
	goPrefix := filepath.Join(base, prefix)
	head := "mkdir " + goPrefix
	if !strings.HasPrefix(message, head) {
		return message
	}
	rest := strings.TrimPrefix(message, head)
	separator := strings.Index(rest, ": ")
	if separator < 0 {
		return message
	}
	return "mkdir " + goPrefix + ".XXXXXX" + rest[separator:]
}

func goTempDirectoryFailure(t *testing.T, base, prefix string) string {
	t.Helper()
	path, err := os.MkdirTemp(base, prefix)
	if err == nil {
		_ = os.Remove(path)
		t.Fatalf("os.MkdirTemp(%q, %q) unexpectedly succeeded", base, prefix)
	}
	return normalizeGoTempError(base, prefix, err.Error())
}

func TestTempDirectoryRuntime(t *testing.T) {
	requireToolchain(t)
	goExecutable, kittenExecutable := buildTempDirectoryPrograms(t, tempDirectorySource)
	root := t.TempDir()
	base := filepath.Join(root, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatalf("creating base directory: %v", err)
	}
	t.Setenv("TMPDIR", base)

	t.Run("normal creation", func(t *testing.T) {
		environment := environmentWithTMPDIR(&base)
		for name, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
			result := runTempDirectory(t, executable, root, environment, "purr")
			ok, path := parseTempDirectoryResult(t, result)
			if !ok {
				t.Errorf("%s pipeline failed: %s", name, path)
				continue
			}
			t.Cleanup(func() { _ = os.RemoveAll(path) })
			checkTempDirectoryPath(t, path, base, "purr")
		}
	})

	t.Run("modes under umasks", func(t *testing.T) {
		for _, mask := range []string{"022", "000", "077", "777"} {
			t.Run(mask, func(t *testing.T) {
				environment := environmentWithTMPDIR(&base)
				for name, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
					result := runTempDirectoryWithUmask(t, executable, root, environment, "mode", mask)
					ok, path := parseTempDirectoryResult(t, result)
					if !ok {
						t.Errorf("%s pipeline failed: %s", name, path)
						continue
					}
					t.Cleanup(func() { _ = os.RemoveAll(path) })
					checkTempDirectoryPath(t, path, base, "mode")
				}
			})
		}
	})

	t.Run("two calls are distinct", func(t *testing.T) {
		twoGo, twoKitten := buildTempDirectoryPrograms(t, tempDirectoryTwoSource)
		environment := environmentWithTMPDIR(&base)
		for name, executable := range map[string]string{"Go": twoGo, "Kitten": twoKitten} {
			result := runTempDirectory(t, executable, root, environment, "purr")
			if result.status != 0 || result.stderr != "" {
				t.Fatalf("%s pipeline: status %d, stderr %q", name, result.status, result.stderr)
			}
			lines := strings.Split(strings.TrimSuffix(result.stdout, "\n"), "\n")
			if len(lines) != 2 {
				t.Fatalf("%s pipeline printed %d paths, want 2", name, len(lines))
			}
			if lines[0] == lines[1] {
				t.Errorf("%s pipeline reused path %q", name, lines[0])
			}
			for _, path := range lines {
				t.Cleanup(func() { _ = os.RemoveAll(path) })
				checkTempDirectoryPath(t, path, base, "purr")
			}
		}
	})

	t.Run("unset and empty TMPDIR use /tmp", func(t *testing.T) {
		for name, environment := range map[string][]string{
			"unset": environmentWithTMPDIR(nil),
			"empty": environmentWithTMPDIR(func() *string { value := ""; return &value }()),
		} {
			t.Run(name, func(t *testing.T) {
				for pipeline, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
					result := runTempDirectory(t, executable, root, environment, "purr")
					ok, path := parseTempDirectoryResult(t, result)
					if !ok {
						t.Errorf("%s pipeline failed: %s", pipeline, path)
						continue
					}
					t.Cleanup(func() { _ = os.RemoveAll(path) })
					checkTempDirectoryPath(t, path, "/tmp", "purr")
				}
			})
		}
	})

	t.Run("relative TMPDIR is absolute", func(t *testing.T) {
		workspace := t.TempDir()
		if err := os.Mkdir(filepath.Join(workspace, "relative"), 0o755); err != nil {
			t.Fatalf("creating relative base: %v", err)
		}
		relative := "relative"
		environment := environmentWithTMPDIR(&relative)
		for pipeline, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
			result := runTempDirectory(t, executable, workspace, environment, "purr")
			ok, path := parseTempDirectoryResult(t, result)
			if !ok {
				t.Errorf("%s pipeline failed: %s", pipeline, path)
				continue
			}
			t.Cleanup(func() { _ = os.RemoveAll(path) })
			checkTempDirectoryPath(t, path, filepath.Join(workspace, "relative"), "purr")
			if !strings.HasPrefix(path, filepath.Join(workspace, "relative")+string(os.PathSeparator)) {
				t.Errorf("%s path %q is not under the absolute relative base", pipeline, path)
			}
		}
	})

	t.Run("symlinked TMPDIR is accepted", func(t *testing.T) {
		real := filepath.Join(root, "real")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatalf("creating real base: %v", err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatalf("creating base symlink: %v", err)
		}
		environment := environmentWithTMPDIR(&link)
		for pipeline, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
			result := runTempDirectory(t, executable, root, environment, "purr")
			ok, path := parseTempDirectoryResult(t, result)
			if !ok {
				t.Errorf("%s pipeline failed: %s", pipeline, path)
				continue
			}
			t.Cleanup(func() { _ = os.RemoveAll(path) })
			if !strings.HasPrefix(path, link+string(os.PathSeparator)) {
				t.Errorf("%s path %q does not preserve symlinked base %q", pipeline, path, link)
			}
			information, err := os.Stat(path)
			if err != nil || !information.IsDir() {
				t.Errorf("%s path %q is not a directory: %v", pipeline, path, err)
			}
		}
	})

	t.Run("invalid prefixes create nothing", func(t *testing.T) {
		missing := filepath.Join(root, "must-not-exist")
		environment := environmentWithTMPDIR(&missing)
		prefixes := []string{"", strings.Repeat("a", 65), "a/b", "..", "a b", "caf\u00e9", "a\n"}
		for _, prefix := range prefixes {
			result := runTempDirectory(t, goExecutable, root, environment, prefix)
			ok, message := parseTempDirectoryResult(t, result)
			if ok || message != "invalid temporary directory prefix" {
				t.Errorf("Go prefix %q: ok=%t message=%q", prefix, ok, message)
			}
			result = runTempDirectory(t, kittenExecutable, root, environment, prefix)
			ok, message = parseTempDirectoryResult(t, result)
			if ok || message != "invalid temporary directory prefix" {
				t.Errorf("Kitten prefix %q: ok=%t message=%q", prefix, ok, message)
			}
			if _, err := os.Stat(missing); !os.IsNotExist(err) {
				t.Errorf("invalid prefix %q touched the base: %v", prefix, err)
			}
		}
	})

	t.Run("64 byte prefix succeeds", func(t *testing.T) {
		prefix := strings.Repeat("a", 64)
		environment := environmentWithTMPDIR(&base)
		for pipeline, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
			result := runTempDirectory(t, executable, root, environment, prefix)
			ok, path := parseTempDirectoryResult(t, result)
			if !ok {
				t.Errorf("%s pipeline failed: %s", pipeline, path)
				continue
			}
			t.Cleanup(func() { _ = os.RemoveAll(path) })
			checkTempDirectoryPath(t, path, base, prefix)
		}
	})

	t.Run("missing base", func(t *testing.T) {
		missing := filepath.Join(root, "missing-base")
		environment := environmentWithTMPDIR(&missing)
		want := goTempDirectoryFailure(t, missing, "purr")
		for pipeline, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
			result := runTempDirectory(t, executable, root, environment, "purr")
			ok, message := parseTempDirectoryResult(t, result)
			if ok || message != want {
				t.Errorf("%s: ok=%t message=%q, want %q", pipeline, ok, message, want)
			}
		}
	})

	t.Run("regular file base", func(t *testing.T) {
		file := filepath.Join(root, "base-file")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatalf("writing base file: %v", err)
		}
		environment := environmentWithTMPDIR(&file)
		want := goTempDirectoryFailure(t, file, "purr")
		for pipeline, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
			result := runTempDirectory(t, executable, root, environment, "purr")
			ok, message := parseTempDirectoryResult(t, result)
			if ok || message != want {
				t.Errorf("%s: ok=%t message=%q, want %q", pipeline, ok, message, want)
			}
		}
	})

	t.Run("unwritable base", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission checks are not meaningful as root")
		}
		locked := filepath.Join(root, "locked")
		if err := os.Mkdir(locked, 0o500); err != nil {
			t.Fatalf("creating locked base: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		environment := environmentWithTMPDIR(&locked)
		want := goTempDirectoryFailure(t, locked, "purr")
		for pipeline, executable := range map[string]string{"Go": goExecutable, "Kitten": kittenExecutable} {
			result := runTempDirectory(t, executable, root, environment, "purr")
			ok, message := parseTempDirectoryResult(t, result)
			if ok || message != want {
				t.Errorf("%s: ok=%t message=%q, want %q", pipeline, ok, message, want)
			}
		}
	})

	t.Run("thousand creations", func(t *testing.T) {
		manyGo, manyKitten := buildTempDirectoryPrograms(t, tempDirectoryManySource)
		environment := environmentWithTMPDIR(&base)
		for pipeline, executable := range map[string]string{"Go": manyGo, "Kitten": manyKitten} {
			result := runTempDirectoryWithFdLimit(t, executable, root, environment, "purr")
			if result.status != 0 || result.stderr != "" {
				t.Fatalf("%s pipeline: status %d, stderr %q", pipeline, result.status, result.stderr)
			}
			lines := strings.Split(strings.TrimSuffix(result.stdout, "\n"), "\n")
			if len(lines) != 1000 {
				t.Fatalf("%s pipeline printed %d lines, want 1000", pipeline, len(lines))
			}
			seen := make(map[string]bool, len(lines))
			for _, path := range lines {
				if strings.Contains(path, " failed") {
					t.Errorf("%s pipeline reported a failure: %s", pipeline, path)
					continue
				}
				if seen[path] {
					t.Errorf("%s pipeline reused path %q", pipeline, path)
				}
				seen[path] = true
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("%s path %q still exists after try_remove: %v", pipeline, path, err)
				}
			}
		}
	})
}

func TestTempDirectorySanitizer(t *testing.T) {
	requireToolchain(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), tempDirectorySource, "")
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
	base := t.TempDir()
	environment := environmentWithTMPDIR(&base)
	result := runTempDirectory(t, executable, base, environment, "purr")
	ok, path := parseTempDirectoryResult(t, result)
	if !ok {
		t.Fatalf("sanitizer run failed: %s", path)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	checkTempDirectoryPath(t, path, base, "purr")
}
