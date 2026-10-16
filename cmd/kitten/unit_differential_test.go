package main

// Differential testing for the Kitten-written translation unit emitter
// (selfhost/codegen/unit.ktn, driven by selfhost/unit.ktn).
//
// The driver prints the whole C translation unit, so one comparison per program
// covers the generated-file header, the declaration sections, the pooled string
// literals, the function bodies, and the C entry point at once, against the
// reference generator's output for the same program, byte for byte. No section is
// compared separately here, and no expected output is hand-maintained.
//
// A program the reference rejects is compared through its error instead: the driver
// prints one error line, and the harness renders the reference's failure in the
// same shape, including the file the failure belongs to, which only the entry point
// rule sets. Those cases are counted separately from the whole-file comparisons.
//
// The supported subset cannot produce observable output yet - print, calls, and
// assignments are not implemented - so the execution test below compares what is
// genuinely observable: both units compile under both compilers, link, run, and
// exit with status 0 and no output, and the statements the generated main runs -
// a loop and a string concatenation, which goes through the collector - complete
// without a runtime failure. It claims nothing about printed values.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kitten/checker"
	"kitten/codegen"
	"kitten/parser"
	"kitten/runtime"
)

// kittenUnitProgram is the driver program that prints a translation unit.
func kittenUnitProgram() string {
	return filepath.Join("..", "..", "selfhost", "unit.ktn")
}

// buildKittenUnit builds the unit probe. It fails if the build produces any C
// diagnostic, so the probe must compile warning-free.
func buildKittenUnit(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-unit")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenUnitProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the unit probe: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the unit probe:\n%s", warnings)
	}
	return executable
}

// runKittenUnit writes the modules and runs the unit probe.
func runKittenUnit(executable, directory, root, imported string) (string, error) {
	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte(root), 0o644); err != nil {
		return "", fmt.Errorf("writing input.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "input2.txt"), []byte(imported), 0o644); err != nil {
		return "", fmt.Errorf("writing input2.txt: %v", err)
	}

	command := exec.Command(executable)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("running the unit probe: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return "", fmt.Errorf("the unit probe wrote to stderr: %s", stderr.String())
	}
	return stdout.String(), nil
}

// generateErrorLineWithFile renders a reference generation failure the way the unit
// driver prints it: the file the failure belongs to, or "-" when it belongs to none.
func generateErrorLineWithFile(err error) (string, bool) {
	var generation *codegen.Error
	if !errors.As(err, &generation) {
		return "", false
	}
	file := generation.File
	if file == "" {
		file = "-"
	}
	return fmt.Sprintf("error Generate %s %d %d %s\n", file, generation.Line, generation.Column, generation.Message), true
}

// unitDifference returns a description of the first difference between a generated
// unit and the reference's, or the empty string when they agree.
func unitDifference(got, want string) string {
	if got == want {
		return ""
	}
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for index := 0; index < len(gotLines) || index < len(wantLines); index++ {
		gotLine := ""
		if index < len(gotLines) {
			gotLine = gotLines[index]
		}
		wantLine := ""
		if index < len(wantLines) {
			wantLine = wantLines[index]
		}
		if gotLine != wantLine {
			return fmt.Sprintf("line %d differs\n got: %q\nwant: %q\n--- got ---\n%s--- want ---\n%s", index, gotLine, wantLine, got, want)
		}
	}
	return "the units differ in length only"
}

// --- Targeted programs -------------------------------------------------------

// unitCase is one program and what the unit comparison expects.
type unitCase struct {
	description string
	root        string
	imported    string

	// rejected, when set, is a fragment the reference's failure message must
	// contain: the program is compared through its error rather than as a unit.
	rejected string
}

func unitCases() []unitCase {
	return []unitCase{{
		description: "a minimal main",
		root: `fn main() {
}
`,
	}, {
		description: "a main whose body does something",
		root: `fn main() {
    int index = 0;
    while (index < 5) {
        index++;
    }
    string text = "one" + "two";
}
`,
	}, {
		description: "several functions in declaration order",
		root: `fn third() -> int {
    return 3;
}
fn first() -> int {
    return 1;
}
fn second() -> int {
    return 2;
}
fn main() {
}
`,
	}, {
		description: "nested blocks and control flow",
		root: `fn classify(int value) -> int {
    if (value > 10) {
        {
            return 1;
        }
    } else if (value > 5) {
        return 2;
    } else {
        return 3;
    }
}
fn deeper(int value) -> int {
    while (value > 0) {
        if (value > 2) {
            {
                return value;
            }
        }
        value--;
    }
    return 0;
}
fn main() {
}
`,
	}, {
		description: "loops of every shape",
		root: `fn loops(int limit) -> int {
    int index = 0;
    while (index < limit) {
        index++;
    }
    for (int step = 0; step < limit; step++) {
        index++;
    }
    for (; index < limit; index++) {
    }
    return index;
}
fn main() {
}
`,
	}, {
		description: "match statements over enums and unions",
		root: `enum Kind { A, B, C }
union Value {
    Number { int amount; }
    Pair { int left; int right; }
    Empty
}
fn describe(Kind kind) -> int {
    match (kind) {
        A { return 1; }
        B { return 2; }
        C { return 3; }
    }
}
fn total(Value value) -> int {
    match (value) {
        Number(amount) { return amount; }
        Pair(left, right) { return left + right; }
        Empty { return 0; }
    }
}
fn main() {
}
`,
	}, {
		description: "structs, enums, and unions with constructors",
		root: `struct Point {
    int x;
    string label;
}
struct Empty {
    int placeholder;
}
enum Color { Red, Green, Blue }
union Value {
    Number { int amount; }
    Text { string body; }
    Nothing
}
fn main() {
}
`,
	}, {
		description: "no pooled strings",
		root: `fn arithmetic(int value) -> int {
    int doubled = value * 2;
    int shifted = doubled + 1;
    bool compared = shifted > value;
    return shifted;
}
fn main() {
}
`,
	}, {
		description: "string literals, empty strings, escapes, and repetition",
		root: `fn strings() -> string {
    string empty = "";
    string quoted = "a\"b";
    string slashed = "c\\d";
    string spaced = "one\ttwo\nthree";
    string again = "";
    string repeat = "a\"b";
    return repeat;
}
fn main() {
}
`,
	}, {
		description: "pool order when strings appear in different functions",
		root: `fn second() -> string {
    return "beta";
}
fn first() -> string {
    return "alpha";
}
fn main() {
}
`,
	}, {
		description: "strings inside nested statements",
		root: `fn nested(bool flag) -> string {
    string outer = "outer";
    if (flag) {
        string inner = "inner";
        while (flag) {
            string deepest = "deepest";
            return deepest;
        }
    }
    return outer;
}
fn main() {
}
`,
	}, {
		description: "several modules with their own functions",
		root: `import "input2.txt";
fn local(int value) -> int {
    string text = "root";
    return value;
}
fn main() {
}
`,
		imported: `public struct Imported {
    int value;
    string label;
}
public enum Mode { On, Off }
public fn describe(Mode mode) -> int {
    match (mode) {
        On { return 1; }
        Off { return 0; }
    }
}
fn helper(string text) -> string {
    return text;
}
`,
	}, {
		description: "an imported module that declares its own main",
		root: `import "input2.txt";
fn main() {
}
`,
		imported: `fn main() {
    int ignored = 1;
}
`,
	}, {
		description: "no entry point",
		root: `fn helper() -> int {
    return 1;
}
`,
		rejected: "no entry point",
	}, {
		description: "an entry point with a parameter",
		root: `fn main(int argument) {
}
`,
		rejected: "must take no parameters",
	}, {
		description: "an entry point with a return type",
		root: `fn main() -> int {
    return 1;
}
`,
		rejected: "must not return a value",
	}, {
		description: "an imported entry point does not count",
		root: `import "input2.txt";
fn helper() -> int {
    return 1;
}
`,
		imported: `public fn main() {
}
`,
		rejected: "no entry point",
	}, {
		description: "an unsupported expression in a local declaration",
		root: `fn main() {
    int[] values = new int[2];
}
`,
	}, {
		description: "an unsupported expression in a return",
		root: `fn helper() -> int {
    return 1;
}
fn uses() -> int {
    return helper();
}
fn main() {
}
`,
	}, {
		description: "an unsupported expression in a match arm",
		root: `enum Kind { A }
fn describe(Kind kind) -> int {
    match (kind) {
        A { return missing(); }
    }
}
fn missing() -> int {
    return 1;
}
fn main() {
}
`,
	}}
}

// TestKittenUnitMatchesReference compares the whole generated translation unit with
// the reference generator's for each targeted program, and the error for each
// program the reference rejects.
func TestKittenUnitMatchesReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	units := 0
	unitLines := 0
	rejected := 0
	unsupported := 0
	for _, testCase := range unitCases() {
		modules, checked, generated, ok := prepareUnit(t, testCase.description, testCase.root, testCase.imported)
		if !ok {
			continue
		}
		got, err := runKittenUnit(executable, directory, testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}

		// What the reference does with the program decides what is compared. A
		// program the reference itself rejects is compared through its error. A
		// program the reference generates but this emitter does not - because a body
		// needs an expression kind it does not implement - must report that kind and
		// is not counted as a unit comparison. Otherwise the whole unit is compared.
		_, generationError := codegen.Generate(modules, checked)
		if testCase.rejected != "" {
			if generationError == nil {
				t.Errorf("%s: the reference accepted a program that should be rejected", testCase.description)
				continue
			}
			want, ok := generateErrorLineWithFile(generationError)
			if !ok {
				t.Errorf("%s: the reference returned %T", testCase.description, generationError)
				continue
			}
			if !strings.Contains(want, testCase.rejected) {
				t.Errorf("%s: the reference failure does not contain %q: %q", testCase.description, testCase.rejected, want)
				continue
			}
			if got != want {
				t.Errorf("%s: the reported failure differs\n got: %q\nwant: %q", testCase.description, got, want)
				continue
			}
			rejected++
			continue
		}

		if generationError != nil {
			t.Errorf("%s: the reference rejected the program: %v", testCase.description, generationError)
			continue
		}
		if strings.HasPrefix(got, "error ") {
			if !strings.Contains(got, "not implemented yet") {
				t.Errorf("%s: the program failed for a reason that is not an unimplemented expression kind:\n%s", testCase.description, got)
				continue
			}
			unsupported++
			continue
		}
		if difference := unitDifference(got, generated); difference != "" {
			t.Errorf("%s: the translation unit differs: %s", testCase.description, difference)
			continue
		}
		units++
		unitLines += strings.Count(generated, "\n")
	}

	if units+rejected+unsupported != len(unitCases()) {
		t.Fatalf("%d units, %d rejections, and %d unsupported for %d targeted programs", units, rejected, unsupported, len(unitCases()))
	}
	if units < 10 || rejected < 3 || unsupported < 3 {
		t.Fatalf("the targeted corpus is not covered: %d units, %d rejections, %d unsupported", units, rejected, unsupported)
	}
	t.Logf("translation units: %d whole units identical over %d lines, %d programs rejected identically, %d reported as needing an unimplemented kind", units, unitLines, rejected, unsupported)
}

// prepareUnit prepares, checks, and generates a program with the reference, or
// reports why it could not be prepared.
func prepareUnit(t *testing.T, description, root, imported string) ([]*parser.Module, *checker.Result, string, bool) {
	t.Helper()

	modules, err := referenceModules(root, imported)
	if err != nil {
		t.Errorf("%s: %v", description, err)
		return nil, nil, "", false
	}
	checked, checkError := checker.CheckModules(modules)
	if checkError != nil {
		t.Errorf("%s: the reference checker rejected the program: %v", description, checkError)
		return nil, nil, "", false
	}
	generated, _ := codegen.Generate(modules, checked)
	return modules, checked, generated, true
}

// --- Corpus ------------------------------------------------------------------

// TestKittenUnitCorpus compares whole translation units over the checker corpus.
// Every program the checker rejects must be reported with the checker's own error;
// every program with an invalid entry point or an unimplemented expression must be
// reported with the reference's failure for the same reason; and every other
// program's unit must match the reference byte for byte.
func TestKittenUnitCorpus(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	units := 0
	unitLines := 0
	entryPointFailures := 0
	unsupported := 0
	checkErrors := 0
	skipped := 0

	for _, testCase := range checkerCorpus(t) {
		modules, err := referenceModules(testCase.root, testCase.imported)
		if err != nil {
			continue
		}
		checked, checkError := checker.CheckModules(modules)
		if checkError != nil {
			want, ok := checkErrorLine(checkError)
			if !ok {
				t.Errorf("%s: the reference checker returned %T", testCase.name, checkError)
				continue
			}
			got, err := runKittenUnit(executable, directory, testCase.root, testCase.imported)
			if err != nil {
				t.Errorf("%s: %v", testCase.name, err)
				continue
			}
			if got != want {
				t.Errorf("%s: the reported error differs\n got: %q\nwant: %q", testCase.name, got, want)
				continue
			}
			checkErrors++
			continue
		}

		// A non-ASCII input shifts the positions the probe reports, because Kitten
		// counts bytes.
		if !isASCII(testCase.root) || !isASCII(testCase.imported) {
			skipped++
			continue
		}

		got, err := runKittenUnit(executable, directory, testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", testCase.name, err)
			continue
		}

		if !referenceAcceptsEntryPoint(modules, checked) {
			if generationError := entryPointError(modules, checked); generationError != nil {
				want, ok := generateErrorLineWithFile(generationError)
				if !ok {
					t.Errorf("%s: the reference generator returned %T", testCase.name, generationError)
					continue
				}
				if got != want {
					t.Errorf("%s: the entry point failure differs\n got: %q\nwant: %q", testCase.name, got, want)
					continue
				}
				entryPointFailures++
				continue
			}
			t.Errorf("%s: the reference accepted a program with an invalid entry point", testCase.name)
			continue
		}

		generated, generationError := codegen.Generate(modules, checked)
		if generationError != nil {
			// The entry point rule runs first and is handled above, so a valid
			// program cannot fail generation for any other reason.
			t.Errorf("%s: the reference generator failed after the entry point: %v", testCase.name, generationError)
			continue
		}
		if strings.HasPrefix(got, "error ") {
			// The reference generates the program, so a failure from this emitter
			// can only be an expression kind it does not implement.
			if !strings.Contains(got, "not implemented yet") {
				t.Errorf("%s: the program failed for a reason that is not an unimplemented expression kind:\n%s", testCase.name, got)
				continue
			}
			unsupported++
			continue
		}
		if difference := unitDifference(got, generated); difference != "" {
			t.Errorf("%s: the translation unit differs: %s", testCase.name, difference)
			continue
		}
		units++
		unitLines += strings.Count(generated, "\n")
	}

	total := units + entryPointFailures + unsupported + checkErrors + skipped
	if total < 1800 {
		t.Fatalf("the corpus looks too small: %d inputs", total)
	}
	if units < 5 || unitLines < 50 || entryPointFailures < 20 || unsupported < 20 || checkErrors < 1000 {
		t.Fatalf("the corpus is not covered: %d units over %d lines, %d entry point failures, %d unsupported, %d check errors, %d skipped, of %d inputs",
			units, unitLines, entryPointFailures, unsupported, checkErrors, skipped, total)
	}
	t.Logf("translation unit corpus: %d inputs, %d whole units identical over %d lines, %d entry point failures identical, %d unsupported expressions, %d check errors identical, %d skipped",
		total, units, unitLines, entryPointFailures, unsupported, checkErrors, skipped)
}

// entryPointError renders the reference's entry point failure for a program whose
// root does not satisfy the rule, by asking the reference generator for its error.
func entryPointError(modules []*parser.Module, checked *checker.Result) error {
	_, generationError := codegen.Generate(modules, checked)
	return generationError
}

// --- Comparison sensitivity --------------------------------------------------

// TestKittenUnitComparisonIsSensitive checks that the whole-unit comparison rejects
// the kinds of difference it is meant to catch. It corrupts a correct unit and
// requires the comparison to notice, so a comparison that accepted everything could
// not pass.
func TestKittenUnitComparisonIsSensitive(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	root := `struct Point {
    int x;
}
enum Kind { A, B }
fn describe(Kind kind) -> int {
    match (kind) {
        A { return 1; }
        B { return 2; }
    }
}
fn label() -> string {
    string text = "hello";
    return text;
}
fn main() {
}
`
	_, _, generated, ok := prepareUnit(t, "sensitivity", root, "")
	if !ok {
		t.Fatalf("the reference rejected the program")
	}
	got, err := runKittenUnit(executable, directory, root, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if difference := unitDifference(got, generated); difference != "" {
		t.Fatalf("the uncorrupted unit already differs: %s", difference)
	}

	corruptions := []struct {
		description string
		text        string
	}{
		{"identical unit", generated},
		{"a missing blank line", strings.Replace(generated, "\n\n", "\n", 1)},
		{"an extra blank line", strings.Replace(generated, "#include \"kitten_runtime.h\"\n\n", "#include \"kitten_runtime.h\"\n\n\n", 1)},
		{"a changed literal number", strings.Replace(generated, "ktn_literal_1", "ktn_literal_2", 1)},
		{"a reordered function", swapLines(generated, "int64_t ktn_fn_describe", "const ktn_string * ktn_fn_label")},
		{"a changed entry point line", strings.Replace(generated, "    ktn_fn_main();", "    ktn_fn_main;", 1)},
		{"a dropped function definition", strings.Replace(generated, "\nvoid ktn_fn_main(void) {\n}\n", "\n", 1)},
		{"a removed declaration section", strings.Replace(generated, "typedef struct ktn_s_Point ktn_s_Point;\n", "", 1)},
		{"a dropped include", strings.Replace(generated, "#include \"kitten_runtime.h\"\n", "", 1)},
	}
	for _, corruption := range corruptions {
		difference := unitDifference(corruption.text, generated)
		if corruption.description == "identical unit" {
			if difference != "" {
				t.Errorf("an identical unit was reported as different: %s", difference)
			}
			continue
		}
		if corruption.text == generated {
			t.Errorf("%s: the corruption changed nothing", corruption.description)
			continue
		}
		if difference == "" {
			t.Errorf("%s: the comparison accepted a corrupted unit", corruption.description)
		}
	}
}

// --- Execution ---------------------------------------------------------------

// runnableUnitCase is one program whose complete unit is compiled, linked, and run.
type runnableUnitCase struct {
	description string
	root        string
}

func runnableUnitCases() []runnableUnitCase {
	return []runnableUnitCase{{
		description: "a minimal program",
		root: `fn main() {
}
`,
	}, {
		description: "a program whose main runs statements",
		root: `enum Kind { A, B }
fn describe(Kind kind) -> int {
    match (kind) {
        A { return 1; }
        B { return 2; }
    }
}
fn count(int limit) -> int {
    int index = 0;
    while (index < limit) {
        index++;
    }
    return index;
}
fn main() {
    int index = 0;
    while (index < 4) {
        index++;
    }
    string text = "one" + "two";
    if (text == "onetwo") {
        index++;
    }
    if (index > 3) {
        return;
    }
    index++;
}
`,
	}}
}

// TestKittenUnitCompileAndRun compiles, links, and runs the complete units this
// phase generates, under both compilers, and compares their observable behavior with
// the reference's.
//
// Both units are generated independently - one by the reference generator and one by
// the Kitten-written one - and are required to be identical before either is
// compiled, so the comparison is of two generated units rather than of one text
// compiled twice. The supported subset cannot produce output: print, calls, and
// assignments are not implemented, so stdout is empty for both. What this checks is
// therefore which the task requires and what is genuinely available: both units
// compile with the project's flags, link against the runtime, run, exit with status
// 0, write nothing to either stream, and complete the statements their main runs -
// including a string concatenation, which allocates through the collector - without
// a runtime failure. It does not compare printed values, because the subset cannot
// print.
func TestKittenUnitCompileAndRun(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	probeDirectory := t.TempDir()

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	compiled := 0
	for _, testCase := range runnableUnitCases() {
		modules, checked, _ := prepareUnitExpectingSuccess(t, testCase.description, testCase.root)
		if modules == nil {
			continue
		}
		referenceText, generationError := codegen.Generate(modules, checked)
		if generationError != nil {
			t.Errorf("%s: the reference rejected the program: %v", testCase.description, generationError)
			continue
		}
		kittenText, err := runKittenUnit(executable, probeDirectory, testCase.root, "")
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if difference := unitDifference(kittenText, referenceText); difference != "" {
			t.Errorf("%s: the translation unit differs: %s", testCase.description, difference)
			continue
		}

		// Both texts are identical, and each is compiled and run from its own
		// directory so that neither result can mask the other.
		outputs := make([]string, 0, 2)
		for index, text := range []string{referenceText, kittenText} {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "program.c"), []byte(text), 0o644); err != nil {
				t.Fatalf("writing program.c: %v", err)
			}
			if err := os.WriteFile(filepath.Join(directory, runtime.SourceFileName), []byte(runtime.Source()), 0o644); err != nil {
				t.Fatalf("writing the runtime: %v", err)
			}
			if err := os.WriteFile(filepath.Join(directory, runtime.HeaderFileName), []byte(runtime.HeaderSource()), 0o644); err != nil {
				t.Fatalf("writing the runtime header: %v", err)
			}
			arguments := append([]string{}, compiler[1:]...)
			arguments = append(arguments, "-std=c11", "-Wall", "-Wextra")
			arguments = append(arguments, compileFlags...)
			arguments = append(arguments, "-o", "program", "program.c", runtime.SourceFileName)
			arguments = append(arguments, linkFlags...)
			build := exec.Command(compiler[0], arguments...)
			build.Dir = directory
			var diagnostics bytes.Buffer
			build.Stdout = &diagnostics
			build.Stderr = &diagnostics
			if err := build.Run(); err != nil {
				t.Errorf("%s: compiling unit %d with %s: %v\n%s", testCase.description, index, filepath.Base(compiler[0]), err, diagnostics.String())
				break
			}
			if output := strings.TrimSpace(diagnostics.String()); output != "" {
				t.Errorf("%s: compiling unit %d with %s produced diagnostics:\n%s", testCase.description, index, filepath.Base(compiler[0]), output)
				break
			}

			run := exec.Command(filepath.Join(directory, "program"))
			var stdout, stderr bytes.Buffer
			run.Stdout = &stdout
			run.Stderr = &stderr
			status := 0
			if err := run.Run(); err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) {
					status = exit.ExitCode()
				} else {
					t.Errorf("%s: running unit %d: %v", testCase.description, index, err)
					break
				}
			}
			if stdout.Len() != 0 || stderr.Len() != 0 || status != 0 {
				t.Errorf("%s: unit %d wrote %q and %q and exited %d", testCase.description, index, stdout.String(), stderr.String(), status)
				break
			}
			outputs = append(outputs, stdout.String())
		}
		if len(outputs) != 2 {
			continue
		}
		if outputs[0] != outputs[1] {
			t.Errorf("%s: the two units behaved differently: %q against %q", testCase.description, outputs[0], outputs[1])
			continue
		}
		compiled++
	}

	if compiled != len(runnableUnitCases()) {
		t.Fatalf("%d of %d runnable programs were compiled and run", compiled, len(runnableUnitCases()))
	}
	t.Logf("execution: %d complete units compiled, linked, and run under %s with no output and status 0", compiled, filepath.Base(compiler[0]))
}

// prepareUnitExpectingSuccess prepares a program that the reference must generate.
func prepareUnitExpectingSuccess(t *testing.T, description, root string) ([]*parser.Module, *checker.Result, string) {
	t.Helper()

	modules, checked, generated, ok := prepareUnit(t, description, root, "")
	if !ok {
		return nil, nil, ""
	}
	if generated == "" {
		t.Errorf("%s: the reference did not generate a unit, but it must for a runnable case", description)
		return nil, nil, ""
	}
	return modules, checked, generated
}

// --- State isolation and performance -----------------------------------------

// TestKittenUnitIsDeterministic checks that generation depends only on its input:
// the same program must produce the same bytes after a different program.
func TestKittenUnitIsDeterministic(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	root := "struct S {\n    int a;\n}\nfn value(int x) -> int {\n    string s = \"text\";\n    return x + 1;\n}\nfn main() {\n}\n"
	first, err := runKittenUnit(executable, directory, root, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := runKittenUnit(executable, directory, "enum E { A }\nfn other(E e) -> int {\n    match (e) {\n        A { return 1; }\n    }\n}\nfn main() {\n}\n", ""); err != nil {
		t.Fatalf("%v", err)
	}
	second, err := runKittenUnit(executable, directory, root, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if first != second {
		t.Errorf("generation depends on the previous run\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	// The same root with a different imported module must still produce the same
	// unit as the single-module program, because an empty module contributes
	// nothing and no state leaks between runs.
	withEmpty, err := runKittenUnit(executable, directory, root, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if withEmpty != first {
		t.Errorf("a repeated generation differs\nfirst:\n%s\nsecond:\n%s", first, withEmpty)
	}
}

// programWithFunctions builds a program with count functions, each with a loop and a
// string literal, so that the cost of generating a whole unit can be measured.
func programWithFunctions(count int) string {
	var builder strings.Builder
	for index := 0; index < count; index++ {
		fmt.Fprintf(&builder, "fn function%d(int limit) -> int {\n    int index = 0;\n    string text = \"literal-%d\";\n    while (index < limit) {\n        index++;\n    }\n    return index;\n}\n", index, index)
	}
	builder.WriteString("fn main() {\n    int index = 0;\n    while (index < 2) {\n        index++;\n    }\n}\n")
	return builder.String()
}

// TestKittenUnitScaling measures generating a whole translation unit of growing size
// and checks it still matches the reference.
func TestKittenUnitScaling(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	previous := time.Duration(0)
	first := time.Duration(0)
	last := time.Duration(0)
	lines := 0
	for _, count := range []int{20, 40, 80} {
		root := programWithFunctions(count)
		modules, checked, _ := prepareUnitExpectingSuccess(t, fmt.Sprintf("%d functions", count), root)
		if modules == nil {
			continue
		}
		wantText, generationError := codegen.Generate(modules, checked)
		if generationError != nil {
			t.Fatalf("%d functions: the reference rejected the program: %v", count, generationError)
		}

		best := time.Duration(0)
		var got string
		for repetition := 0; repetition < 3; repetition++ {
			start := time.Now()
			rendered, err := runKittenUnit(executable, directory, root, "")
			if err != nil {
				t.Fatalf("%d functions: %v", count, err)
			}
			if elapsed := time.Since(start); best == 0 || elapsed < best {
				best = elapsed
			}
			got = rendered
		}
		if difference := unitDifference(got, wantText); difference != "" {
			t.Fatalf("%d functions: the translation unit differs: %s", count, difference)
		}
		lines += strings.Count(wantText, "\n")

		growth := ""
		if previous > 0 {
			growth = fmt.Sprintf(", x%.1f", float64(best)/float64(previous))
		}
		t.Logf("functions=%4d lines=%6d  %10s%s", count, strings.Count(wantText, "\n"), best.Round(time.Millisecond), growth)
		previous = best
		if first == 0 {
			first = best
		}
		last = best
	}

	if lines == 0 {
		t.Fatalf("no unit was compared")
	}
	// Four times the program, and the bound is far looser than linear, so it only
	// catches a different order of cost.
	if last > first*100 {
		t.Errorf("a four-fold larger program cost %v against %v, which is a different order of cost", last, first)
	}
}
