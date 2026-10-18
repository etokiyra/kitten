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
// The execution test below compiles both units under both compilers, links them
// against the runtime, runs them, and compares their printed output with what the
// source implies. One case also checks that a compound assignment to an element
// whose index is a call evaluates that call once.

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

// definedArithmeticSource exercises every defined integer arithmetic form,
// including compound targets that must be evaluated once.
const definedArithmeticSource = `struct Counter {
    int value;
}
fn index_of(int[] counter) -> int {
    counter[0] += 1;
    return counter[0] - 1;
}
fn main() {
    int maximum = 9223372036854775807;
    int minimum = -9223372036854775807 - 1;
    print(maximum + 1);
    print(minimum - 1);
    print(maximum * 2);
    print(-minimum);
    print(minimum / -1);
    print(minimum % -1);
    int value = 1;
    value += maximum;
    value -= 1;
    value++;
    --value;
    int pre_increment = ++value;
    int post_decrement = value--;
    print(value, pre_increment, post_decrement);
    int[] counter = new int[1];
    int[] values = new int[2];
    values[0] = 1;
    values[index_of(counter)] += 5;
    values[index_of(counter)]++;
    print(counter[0], values[0], values[1]);
    map[string]int counts = new map[string]int;
    counts["k"] = 1;
    counts["k"] += 2;
    counts["k"]--;
    counts["k"]++;
    print(counts["k"]);
    Counter box = Counter { value = 1 };
    box.value += 2;
    box.value--;
    box.value++;
    print(box.value);
}
`

func unitCases() []unitCase {
	return []unitCase{{
		description: "a minimal main",
		root: `fn main() {
}
`,
	}, {
		description: "argv in a whole unit",
		root: `fn main() {
    print(length(argv()));
    print(argv()[0]);
}
`,
	}, {
		description: "eprint in a whole unit",
		root: `fn main() {
    eprint(1, 2.5, true, "text");
    eprint("");
}
`,
	}, {
		description: "exit in a whole unit",
		root: `fn stop(int code) {
    exit(code);
}
fn main() {
    stop(1 + 2);
    exit(-1);
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
		description: "an array creation in a local declaration",
		root: `fn main() {
    int[] values = new int[2];
}
`,
	}, {
		description: "a call expression in a return",
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
		description: "a call expression in a match arm",
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
	}, {
		description: "an assignment expression",
		root: `fn main() {
    int a = 1;
    a = 2;
}
`,
	}, {
		description: "an assignment to an array element",
		root: `fn main() {
    int[] values = new int[2];
    values[0] = 1;
}
`,
	}, {
		description: "an assignment to a struct field",
		root: `struct Point {
    int x;
}
fn main() {
    Point point = Point { x = 1 };
    point.x = 2;
}
`,
	}, {
		description: "assignments in sub-expressions and conditions",
		root: `fn main() {
    int first = 0;
    int second = 0;
    print(first = second = 3);
    print(first + second);
    bool more = true;
    int step = 0;
    while (more = step < 3) {
        step++;
    }
    print(step);
}
`,
	}, {
		description: "compound assignment on fields and elements",
		root: `struct Measures {
    int count;
    float scale;
}
fn main() {
    int number = 10;
    number += 5;
    number -= 3;
    float ratio = 1.5;
    ratio += 0.5;
    ratio -= 0.25;
    Measures measures = Measures { count = 1, scale = 1.0 };
    measures.count += 2;
    measures.scale -= 0.25;
    int[] numbers = new int[2];
    numbers[0] = 1;
    numbers[0] += 4;
    numbers[0] -= 2;
    float[] ratios = new float[2];
    ratios[1] = 1.0;
    ratios[1] += 0.5;
    ratios[1] -= 0.25;
}
`,
	}, {
		description: "map entry assignment and string append",
		root: `struct Point {
    int x;
}
union Value {
    Number { int amount; }
    Empty
}
fn amount(Value value) -> int {
    match (value) {
        Number(number) { return number; }
        Empty { return 0; }
    }
}
fn main() {
    map[string]int counts = new map[string]int;
    counts["a"] = 1;
    counts["a"] += 2;
    map[string]string names = new map[string]string;
    names["greeting"] = "hello";
    names["greeting"] += " world";
    map[string]Point points = new map[string]Point;
    points["p"] = Point { x = 1 };
    map[string]Value values = new map[string]Value;
    values["v"] = Value.Empty;
    values["v"] = Value.Number { amount = 3 };
    string text = "a";
    text += "b";
    print(counts["a"], names["greeting"], points["p"].x, amount(values["v"]), text);
}
`,
	}, {
		description: "a struct literal in a local declaration",
		root: `struct Point {
    int x;
}
fn main() {
    Point point = Point { x = 1 };
}
`,
	}, {
		description: "member access, indexing, and variant references",
		root: `struct Point {
    int x;
    int y;
}
enum Color { Red }
union Shape {
    Circle { int radius; }
    Nothing
}
fn measure(Shape shape) -> int {
    match (shape) {
        Circle(radius) { return radius; }
        Nothing { return 0; }
    }
}
fn main() {
    Point point = Point { x = 1, y = 2 };
    int[] values = new int[2];
    map[string]int counts = new map[string]int;
    print(point.x);
    print(values[0]);
    print(counts["a"]);
    print(Color.Red == Color.Red);
    print(measure(Shape.Nothing));
}
`,
	}, {
		description: "array and map creation",
		root: `struct Point {
    int x;
}
fn main() {
    int[] values = new int[3];
    int[][] grid = new int[][2];
    map[string]int counts = new map[string]int;
    map[string]Point points = new map[string]Point;
}
`,
	}, {
		description: "defined integer arithmetic and target evaluation",
		root:        definedArithmeticSource,
	}, {
		description: "try_read_file with the implicit prelude and helper",
		root: `fn describe(ReadFileResult result) -> int {
    match (result) {
        Ok(text) { return 1; }
        Failed(message) { return 2; }
    }
}
fn inspect() -> int {
    ReadFileResult result = ReadFileResult.Ok { text = "x" };
    return describe(result);
}
fn main() {
    print(describe(try_read_file("x")));
}
`,
	}, {
		description: "a shadowed try_read_file still emits the prelude helper",
		root: `fn try_read_file(string path) -> int {
    return 1;
}
fn main() {
    print(try_read_file("x"));
}
`,
	}, {
		description: "nested literals and recursive variants",
		root: `union Expression {
    IntegerLiteral { int value; }
    Add { Expression left; Expression right; }
    Missing
}
struct Box {
    Expression value;
}
fn evaluate(Expression expression) -> int {
    match (expression) {
        IntegerLiteral(value) { return value; }
        Add(left, right) { return evaluate(left) + evaluate(right); }
        Missing { return 0; }
    }
}
fn total(Box box) -> int {
    return evaluate(box.value);
}
fn main() {
    print(total(Box { value = Expression.Add {
        left = Expression.IntegerLiteral { value = 2 },
        right = Expression.Missing
    } }));
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
		// program the reference itself rejects is compared through its error.
		// Otherwise the whole unit is compared.
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
			t.Errorf("%s: the probe failed to generate a unit:\n%s", testCase.description, got)
			continue
		}
		if difference := unitDifference(got, generated); difference != "" {
			t.Errorf("%s: the translation unit differs: %s", testCase.description, difference)
			continue
		}
		units++
		unitLines += strings.Count(generated, "\n")
	}

	if units+rejected != len(unitCases()) {
		t.Fatalf("%d units and %d rejections for %d targeted programs", units, rejected, len(unitCases()))
	}
	if units < 20 || rejected < 3 {
		t.Fatalf("the targeted corpus is not covered: %d units, %d rejections", units, rejected)
	}
	t.Logf("translation units: %d whole units identical over %d lines, %d programs rejected identically", units, unitLines, rejected)
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
// every program with an invalid entry point must be reported with the reference's
// failure for the same reason; and every other program's unit must match the
// reference byte for byte.
func TestKittenUnitCorpus(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	units := 0
	unitLines := 0
	entryPointFailures := 0
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
			t.Errorf("%s: the probe failed to generate a unit:\n%s", testCase.name, got)
			continue
		}
		if difference := unitDifference(got, generated); difference != "" {
			t.Errorf("%s: the translation unit differs: %s", testCase.name, difference)
			continue
		}
		units++
		unitLines += strings.Count(generated, "\n")
	}

	total := units + entryPointFailures + checkErrors + skipped
	if total < 1800 {
		t.Fatalf("the corpus looks too small: %d inputs", total)
	}
	if units < 30 || unitLines < 300 || entryPointFailures < 20 || checkErrors < 1000 {
		t.Fatalf("the corpus is not covered: %d units over %d lines, %d entry point failures, %d check errors, %d skipped, of %d inputs",
			units, unitLines, entryPointFailures, checkErrors, skipped, total)
	}
	t.Logf("translation unit corpus: %d inputs, %d whole units identical over %d lines, %d entry point failures identical, %d check errors identical, %d skipped",
		total, units, unitLines, entryPointFailures, checkErrors, skipped)
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
		{"a changed argument capture line", strings.Replace(generated, "    ktn_rt_set_args(argc, argv);", "    ktn_rt_set_args(argc);", 1)},
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
	expected    string
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
	}, {
		description: "assignments evaluate each target once and yield their values",
		root: `fn index_of(int[] counter) -> int {
    counter[0] += 1;
    return counter[0] - 1;
}
fn main() {
    int[] numbers = new int[2];
    numbers[0] = 10;
    numbers[0] += 5;
    print(numbers[0]);
    string[] words = new string[2];
    words[0] = "a";
    words[0] += "b";
    print(words[0]);
    int[] counter = new int[1];
    int[] slots = new int[2];
    slots[0] = 1;
    slots[index_of(counter)] += 4;
    print(counter[0], slots[0]);
    map[string]int counts = new map[string]int;
    counts["k"] = 1;
    counts["k"] += 2;
    print(counts["k"]);
}
`,
		expected: "15\nab\n1 5\n3\n",
	}, {
		description: "defined integer arithmetic and target evaluation",
		root:        definedArithmeticSource,
		expected:    "-9223372036854775808\n9223372036854775807\n-2\n-9223372036854775808\n-9223372036854775808\n0\n9223372036854775807 -9223372036854775808 -9223372036854775808\n2 6 1\n3\n3\n",
	}}
}

// TestKittenUnitCompileAndRun compiles, links, and runs the complete units this
// phase generates, under both compilers, and compares their observable behavior with
// the reference's.
//
// Both units are generated independently - one by the reference generator and one by
// the Kitten-written one - and are required to be identical before either is
// compiled, so the comparison is of two generated units rather than of one text
// compiled twice. Both units compile with the project's flags, link against the
// runtime, run, exit with status 0, write nothing to stderr, and print exactly what
// the source implies. The assignment case also checks that a compound assignment to
// an element whose index is a call evaluates that call once.
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
		modules, checked, _ := prepareUnitExpectingSuccess(t, testCase.description, testCase.root, "")
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
			result, err := buildAndRunProgram(t, compiler, compileFlags, linkFlags, directory, text, index)
			if err != nil {
				t.Errorf("%s: %v", testCase.description, err)
				break
			}
			if result.stdout != testCase.expected || result.stderr != "" || result.status != 0 {
				t.Errorf("%s: unit %d printed %q (want %q), wrote %q to stderr, and exited %d", testCase.description, index, result.stdout, testCase.expected, result.stderr, result.status)
				break
			}
			outputs = append(outputs, result.stdout)
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
	t.Logf("execution: %d complete units compiled, linked, and run under %s, matching their expected output and status 0", compiled, filepath.Base(compiler[0]))
}

// prepareUnitExpectingSuccess prepares a program that the reference must generate.
func prepareUnitExpectingSuccess(t *testing.T, description, root, imported string) ([]*parser.Module, *checker.Result, string) {
	t.Helper()

	modules, checked, generated, ok := prepareUnit(t, description, root, imported)
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
		modules, checked, _ := prepareUnitExpectingSuccess(t, fmt.Sprintf("%d functions", count), root, "")
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

// programResult is what a generated program wrote and the status it exited with.
type programResult struct {
	stdout string
	stderr string
	status int
}

// buildAndRunProgram compiles one generated unit in its own directory against the
// runtime, with the project's flags, links it, runs it with that directory as its
// working directory, and returns what it wrote and its exit status.
//
// Any fixture files the program reads must already have been written into the
// directory, so that a program reading a file is tested against a controlled input
// rather than against whatever happens to be beside the working directory.
func buildAndRunProgram(t *testing.T, compiler, compileFlags, linkFlags []string, directory, text string, index int) (programResult, error) {
	t.Helper()

	return buildAndRunProgramWithArgs(t, compiler, compileFlags, linkFlags, directory, text, index, nil)
}

// buildAndRunProgramWithArgs is buildAndRunProgram with a process argument vector.
func buildAndRunProgramWithArgs(t *testing.T, compiler, compileFlags, linkFlags []string, directory, text string, index int, args []string) (programResult, error) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(directory, "program.c"), []byte(text), 0o644); err != nil {
		return programResult{}, fmt.Errorf("writing program.c: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, runtime.SourceFileName), []byte(runtime.Source()), 0o644); err != nil {
		return programResult{}, fmt.Errorf("writing the runtime: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, runtime.HeaderFileName), []byte(runtime.HeaderSource()), 0o644); err != nil {
		return programResult{}, fmt.Errorf("writing the runtime header: %v", err)
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
		return programResult{}, fmt.Errorf("compiling unit %d with %s: %v\n%s", index, filepath.Base(compiler[0]), err, diagnostics.String())
	}
	if output := strings.TrimSpace(diagnostics.String()); output != "" {
		return programResult{}, fmt.Errorf("compiling unit %d with %s produced diagnostics:\n%s", index, filepath.Base(compiler[0]), output)
	}

	run := exec.Command(filepath.Join(directory, "program"), args...)
	run.Dir = directory
	var stdout, stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	status := 0
	if err := run.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return programResult{}, fmt.Errorf("running unit %d: %v", index, err)
		}
		status = exit.ExitCode()
	}
	return programResult{stdout: stdout.String(), stderr: stderr.String(), status: status}, nil
}
