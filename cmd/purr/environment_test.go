package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/parser"
)

const environmentSource = `fn show(string name) {
    match (try_getenv(name)) {
        Set(value) { print("set", length(value), value); }
        Unset { print("unset"); }
    }
}

fn main() {
    int index = 1;
    while (index < length(argv())) {
        show(argv()[index]);
        index++;
    }
}
`

func buildEnvironmentGoProgram(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "environment.ktn")
	if err := os.WriteFile(sourcePath, []byte(environmentSource), 0o644); err != nil {
		t.Fatalf("writing environment fixture: %v", err)
	}
	executable := filepath.Join(directory, "environment")
	var diagnostics bytes.Buffer
	if err := buildExecutable(sourcePath, executable, &diagnostics); err != nil {
		t.Fatalf("building environment fixture: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics: %s", warnings)
	}
	return executable
}

func environmentFromList(environment []string) map[string]string {
	values := map[string]string{}
	for _, item := range environment {
		if index := strings.IndexByte(item, '='); index >= 0 {
			values[item[:index]] = item[index+1:]
		}
	}
	return values
}

func lookupEnvironment(environment []string, name string) (string, bool) {
	if name == "" || strings.Contains(name, "=") {
		return "", false
	}
	values := environmentFromList(environment)
	value, ok := values[name]
	return value, ok
}

func expectedEnvironmentOutput(environment []string, names []string) string {
	var builder strings.Builder
	for _, name := range names {
		value, ok := lookupEnvironment(environment, name)
		if ok {
			builder.WriteString("set ")
			builder.WriteString(itoa(len(value)))
			builder.WriteString(" ")
			builder.WriteString(value)
			builder.WriteString("\n")
		} else {
			builder.WriteString("unset\n")
		}
	}
	return builder.String()
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}

func runEnvironment(t *testing.T, executable string, environment []string, names []string) programResult {
	t.Helper()
	arguments := append([]string{}, names...)
	command := exec.Command(executable, arguments...)
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	status := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running environment fixture: %v", err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

func environmentWithout(environment []string, name string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment))
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return result
}

func TestEnvironmentRuntime(t *testing.T) {
	requireToolchain(t)
	goExecutable := buildEnvironmentGoProgram(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), environmentSource, "")
	if err != nil {
		t.Fatalf("generating C with the Kitten pipeline: %v", err)
	}
	_, _, referenceText, ok := checkProgram(t, "environment runtime", environmentSource, "")
	if !ok {
		return
	}
	if difference := unitDifference(kittenText, referenceText); difference != "" {
		t.Fatalf("the environment translation unit differs: %s", difference)
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

	complexValue := " leading\tand\ntrailing \u00e9\u00ff"
	largeValue := strings.Repeat("x", 100*1024)
	base := environmentWithout(os.Environ(), "KITTEN_ENV_UNSET")
	base = environmentWithout(base, "KITTEN_ENV_REMOVED")
	base = environmentWithout(base, "KITTEN_ENV_EMPTY")
	base = environmentWithout(base, "KITTEN_ENV_COMPLEX")
	base = environmentWithout(base, "KITTEN_ENV_LARGE")
	environment := append([]string{}, base...)
	environment = append(environment, "KITTEN_ENV_NORMAL=normal value")
	environment = append(environment, "KITTEN_ENV_EMPTY=")
	environment = append(environment, "KITTEN_ENV_COMPLEX="+complexValue)
	environment = append(environment, "KITTEN_ENV_LARGE="+largeValue)

	cases := []struct {
		name  string
		env   []string
		names []string
	}{
		{"normal and empty", environment, []string{"KITTEN_ENV_NORMAL", "KITTEN_ENV_EMPTY"}},
		{"unset and removed", environment, []string{"KITTEN_ENV_UNSET", "KITTEN_ENV_REMOVED"}},
		{"empty and equals names", environment, []string{"", "A=B"}},
		{"complex bytes", environment, []string{"KITTEN_ENV_COMPLEX"}},
		{"large value", environment, []string{"KITTEN_ENV_LARGE"}},
		{"PATH matches Go", environment, []string{"PATH"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			want := expectedEnvironmentOutput(testCase.env, testCase.names)
			goResult := runEnvironment(t, goExecutable, testCase.env, testCase.names)
			kittenResult := runEnvironment(t, kittenExecutable, testCase.env, testCase.names)
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

func TestEnvironmentWithRuntimeModuleIDs(t *testing.T) {
	runtimeSource, err := os.ReadFile(filepath.Join("..", "..", "runtime", "ktn", "memory.ktn"))
	if err != nil {
		t.Fatalf("reading runtime helper: %v", err)
	}
	source := `fn main() {
    try_getenv("PATH");
    copy_bytes(new byte[0]);
}
`
	modules := []*parser.Module{
		{Path: "input.txt", ID: 0, Program: parseTestModuleForRuntime(t, source).Program},
		{Path: "input2.txt", ID: 1, Program: &parser.Program{}},
	}
	checked, err := checker.CheckModulesWithRuntime(modules, string(runtimeSource))
	if err != nil {
		t.Fatalf("checking environment with runtime: %v", err)
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

func TestEnvironmentSanitizer(t *testing.T) {
	requireToolchain(t)
	unitExecutable := buildKittenUnit(t)
	kittenText, err := runKittenUnit(unitExecutable, t.TempDir(), environmentSource, "")
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
	environment := append(os.Environ(), "KITTEN_ENV_SANITIZER=ok")
	result := runEnvironment(t, executable, environment, []string{"KITTEN_ENV_SANITIZER"})
	if result.status != 0 || result.stderr != "" || result.stdout != "set 2 ok\n" {
		t.Errorf("sanitizer run: stdout %q, stderr %q, status %d", result.stdout, result.stderr, result.status)
	}
}
