package main

// Differential testing for the Kitten-written function-call emitter
// (the Call case of selfhost/codegen/expressions.ktn, driven by selfhost/unit.ktn).
//
// Calls are compared the same way every other expression is: by generating the whole
// translation unit with both backends and comparing the two texts byte for byte, so
// one comparison covers the callee's name, the argument list, the argument order,
// the built-in expansion, and the literal pool the arguments filled.
//
// This is also the phase where a generated program can produce output, so the
// execution test compiles both units, links them against the real runtime, runs each
// independently, and compares stdout, stderr, and exit status - between the two
// programs and against the values the source implies.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kitten/checker"
	"kitten/codegen"
	"kitten/parser"
)

// --- Targeted programs -------------------------------------------------------

// callCase is one program whose whole translation unit is compared with the
// reference's, or which is expected to be rejected with a named reason.
type callCase struct {
	description string
	root        string
	imported    string

	// rejected, when set, is a fragment the reference's failure message must
	// contain: the program is compared through its error rather than as a unit.
	rejected string

	// unsupported, when set, is an expression kind the Kitten emitter must report
	// while the reference generates the program.
	unsupported string
}

func callCases() []callCase {
	return []callCase{{
		description: "print with every primitive argument type",
		root: `fn main() {
    print(1);
    print(2.5);
    print(true);
    print("text");
    print(1, 2.5, true, "text");
}
`,
	}, {
		description: "print with no arguments, which the checker rejects",
		root: `fn main() {
    print();
}
`,
		rejected: "expects at least one argument",
	}, {
		description: "user calls with zero, one, and several arguments",
		root: `fn nothing() {
}
fn once(int value) -> int {
    return value;
}
fn twice(int first, int second) -> int {
    return first + second;
}
fn main() {
    nothing();
    int single = once(1);
    int pair = twice(1, 2);
}
`,
	}, {
		description: "calls in every supported position",
		root: `fn value(int seed) -> int {
    return seed;
}
fn truth(int seed) -> bool {
    return seed > 0;
}
fn text_of(int seed) -> string {
    string base = "value";
    return base;
}
fn classify(int seed) -> int {
    if (truth(seed)) {
        return value(seed);
    }
    return 0;
}
enum Flag { Yes, No }
fn choose(Flag flag) -> int {
    match (flag) {
        Yes { return value(1); }
        No { return value(2); }
    }
}
fn main() {
    value(1);
    int initializer = value(2);
    int from_condition = classify(3);
    for (int step = 0; truth(step); step++) {
    }
    string text = text_of(initializer);
}
`,
		// choose is emitted and compared, but main cannot call it while an enum value
		// cannot be constructed: Flag.Yes is a member expression, which is not
		// implemented yet.
	}, {
		description: "a call inside a match arm",
		root: `enum Flag { Yes, No }
fn value(int seed) -> int {
    return seed;
}
fn choose(Flag flag) -> int {
    match (flag) {
        Yes { return value(1); }
        No { return value(2); }
    }
}
fn main() {
    print("choose is emitted but not called");
}
`,
	}, {
		description: "nested and argument-position calls",
		root: `fn twice(int value) -> int {
    return value * 2;
}
fn label(string prefix) -> string {
    return prefix + "!";
}
fn main() {
    print(twice(twice(5)));
    print(twice(twice(twice(1))));
    print(label(label("x")));
    print(twice(1), twice(2), twice(3));
}
`,
	}, {
		description: "calls that return void and calls that return values",
		root: `fn announce() {
    return;
}
fn always() -> bool {
    return true;
}
fn main() {
    announce();
    if (always()) {
        announce();
    }
}
`,
	}, {
		description: "string built-ins and the literal pool the arguments fill",
		root: `fn describe(string text) -> string {
    return text + "!";
}
fn main() {
    print(length("abcd"));
    print(substring("abcdef", 1, 3));
    print(substring(describe("hello"), 0, 2));
    print(length(describe("hello")));
    print("abcd");
}
`,
	}, {
		description: "length over array and map parameters",
		root: `fn total(int[] values) -> int {
    return length(values);
}
fn entries(map[string]int table) -> int {
    return length(table);
}
fn present(map[string]int table, string key) -> bool {
    return has(table, key);
}
fn main() {
    print("no containers are constructed here");
}
`,
	}, {
		description: "calls across modules",
		root: `import "input2.txt";
fn local(int value) -> int {
    return scaled(value) + offset();
}
fn main() {
    print(local(1));
    print(scaled(2));
    print(offset());
}
`,
		imported: `public fn scaled(int value) -> int {
    return value * factor();
}
fn factor() -> int {
    return 3;
}
public fn offset() -> int {
    return 10;
}
`,
	}, {
		description: "a private function called from its own module",
		root: `fn secret() -> int {
    return 7;
}
public fn exposed() -> int {
    return secret();
}
fn main() {
    print(exposed());
}
`,
	}, {
		description: "calls whose arguments are string literals, in order",
		root: `fn joined(string first, string second) -> string {
    return first + second;
}
fn main() {
    print(joined("alpha", "beta"));
    print(joined("gamma", "beta"));
    print(joined("alpha", "delta"));
}
`,
	}, {
		description: "a call whose argument is unsupported",
		root: `struct Point {
    int x;
}
fn take(int value) -> int {
    return value;
}
fn main() {
    int a = 1;
    print(take(a = 2));
}
`,
		unsupported: "assign",
	}, {
		description: "a call whose argument is a struct field",
		root: `struct Point {
    int x;
}
fn take(int value) -> int {
    return value;
}
fn main() {
    Point point = Point { x = 1 };
}
`,
		unsupported: "struct-literal",
	}, {
		description: "a call whose argument is an index",
		root: `fn take(int value) -> int {
    return value;
}
fn first(int[] values) -> int {
    return take(values[0]);
}
fn main() {
    print("first is emitted but not called");
}
`,
		unsupported: "index",
	}, {
		description: "a call to a callee that is not a function",
		root: `fn main() {
    int value = 1;
    value(1);
}
`,
		rejected: "is a variable, not a function",
	}, {
		description: "a call with the wrong number of arguments",
		root: `fn takes_two(int first, int second) -> int {
    return first + second;
}
fn main() {
    print(takes_two(1));
}
`,
		rejected: "expects 2 argument(s), got 1",
	}, {
		description: "a call with a mistyped argument",
		root: `fn takes_int(int value) -> int {
    return value;
}
fn main() {
    print(takes_int(true));
}
`,
		rejected: "expected int, found bool",
	}}
}

// TestKittenCallsMatchReference compares whole translation units for programs whose
// expressions are calls, and compares the reference's failure for the programs it
// rejects.
func TestKittenCallsMatchReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	units := 0
	unitLines := 0
	rejected := 0
	unsupported := 0
	for _, testCase := range callCases() {
		got, err := runKittenUnit(executable, directory, testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}

		// A program the checker rejects is reported before generation, and the
		// driver prints the checker's own error.
		if testCase.rejected != "" {
			checkError := checkErrorOf(t, testCase.root, testCase.imported)
			if checkError == nil {
				t.Errorf("%s: the reference checker accepted a program that should be rejected", testCase.description)
				continue
			}
			want, ok := checkErrorLine(checkError)
			if !ok {
				t.Errorf("%s: the reference checker returned an unexpected error type", testCase.description)
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

		modules, checked, generated, ok := prepareUnit(t, testCase.description, testCase.root, testCase.imported)
		if !ok {
			continue
		}
		if testCase.unsupported != "" {
			want := testCase.unsupported + " expressions are not implemented yet"
			if !strings.HasPrefix(got, "error Generate ") || !strings.Contains(got, want) {
				t.Errorf("%s: expected a failure naming %q, got:\n%s", testCase.description, want, got)
				continue
			}
			if strings.Contains(got, "int main(void)") {
				t.Errorf("%s: a rejected program produced a unit:\n%s", testCase.description, got)
				continue
			}
			unsupported++
			continue
		}
		if generationError := generationErrorOf(modules, checked); generationError != nil {
			t.Errorf("%s: the reference rejected the program: %v", testCase.description, generationError)
			continue
		}
		if difference := unitDifference(got, generated); difference != "" {
			t.Errorf("%s: the translation unit differs: %s", testCase.description, difference)
			continue
		}
		units++
		unitLines += strings.Count(generated, "\n")
	}

	if units+rejected+unsupported != len(callCases()) {
		t.Fatalf("%d units, %d rejections, and %d unsupported for %d programs", units, rejected, unsupported, len(callCases()))
	}
	if units < 8 || rejected < 3 || unsupported < 3 {
		t.Fatalf("the call corpus is not covered: %d units, %d rejections, %d unsupported", units, rejected, unsupported)
	}
	t.Logf("calls: %d whole units identical over %d lines, %d programs rejected identically, %d reported as needing an unimplemented kind",
		units, unitLines, rejected, unsupported)
}

// checkErrorOf returns the reference checker's error for a program.
func checkErrorOf(t *testing.T, root, imported string) error {
	t.Helper()

	modules, err := referenceModules(root, imported)
	if err != nil {
		t.Fatalf("%v", err)
	}
	_, checkError := checker.CheckModules(modules)
	return checkError
}

// generationErrorOf returns the reference generator's error for a program, or nil.
func generationErrorOf(modules []*parser.Module, checked *checker.Result) error {
	_, generationError := codegen.Generate(modules, checked)
	return generationError
}

// --- Execution ---------------------------------------------------------------

// fixtureFile is a file written into each program's working directory before it is
// compiled and run, so that a program reading a file reads a controlled input.
type fixtureFile struct {
	name    string
	content string
}

// callExecutionCase is one program whose generated unit is compiled and run.
type callExecutionCase struct {
	description string
	root        string
	imported    string
	files       []fixtureFile

	// expected is the exact stdout, or empty when the case cannot have a portable
	// expectation and only the two programs are compared with each other.
	expected string

	// note explains why expected is empty, when it is.
	note string
}

func callExecutionCases() []callExecutionCase {
	return []callExecutionCase{{
		description: "print every primitive type",
		root: `fn main() {
    print(1);
    print(2.5);
    print(true);
    print("text");
    print(1, 2.5, true, "text");
}
`,
		expected: "1\n2.5\ntrue\ntext\n1 2.5 true text\n",
	}, {
		description: "print from several calls shows call order",
		root: `fn report(int value) {
    print("value", value);
}
fn main() {
    report(1);
    report(2);
    report(3);
}
`,
		expected: "value 1\nvalue 2\nvalue 3\n",
	}, {
		description: "return values reach the caller",
		root: `fn twice(int value) -> int {
    return value * 2;
}
fn label(string prefix) -> string {
    return prefix + "!";
}
fn main() {
    print(twice(21));
    print(twice(twice(5)));
    print(label("hi"));
}
`,
		expected: "42\n20\nhi!\n",
	}, {
		description: "string built-ins compute what they should",
		root: `fn main() {
    print(length("abcd"));
    print(substring("abcdef", 1, 3));
    print(substring("abcdef", 2, 2));
    print("one" + "two");
    print("one" == "one");
    print("one" != "two");
}
`,
		expected: "4\nbc\n\nonetwo\ntrue\ntrue\n",
	}, {
		description: "reading a file prints its bytes",
		root: `fn main() {
    print(read_file("data.txt"));
    print(length(read_file("data.txt")));
}
`,
		files:    []fixtureFile{{name: "data.txt", content: "file-content"}},
		expected: "file-content\n12\n",
	}, {
		description: "calls across modules",
		root: `import "input2.txt";
fn main() {
    print(scaled(4));
    print(offset());
    print(label("x"));
}
`,
		imported: `public fn scaled(int value) -> int {
    return value * factor();
}
fn factor() -> int {
    return 3;
}
public fn offset() -> int {
    return 10;
}
public fn label(string prefix) -> string {
    return prefix + "!";
}
`,
		expected: "12\n10\nx!\n",
	}, {
		description: "calls in conditions",
		root: `fn positive(int value) -> bool {
    return value > 0;
}
fn value_of(int seed) -> int {
    return seed;
}
fn main() {
    if (positive(1)) {
        print("positive");
    }
    print(value_of(7));
}
`,
		expected: "positive\n7\n",
	}, {
		description: "nested calls and repeated literals share pool entries",
		root: `fn wrap(int value) -> int {
    return value + 1;
}
fn main() {
    print("same", wrap(wrap(1)));
    print("same");
    print(wrap(0), "same");
}
`,
		expected: "same 3\nsame\n1 same\n",
	}, {
		description: "argument evaluation order is observable",
		root: `fn say(int value) -> int {
    print("evaluated", value);
    return value;
}
fn main() {
    print("total", say(1) + say(2));
}
`,
		note: "the C standard leaves argument evaluation order unspecified, so the expectation is the reference program's own output rather than a fixed string",
	}, {
		description: "a program that prints a lot",
		root: `fn fib(int n) -> int {
    if (n < 2) {
        return n;
    }
    return fib(n - 1) + fib(n - 2);
}
fn banner(string title) {
    print("==", title, "==");
}
fn main() {
    banner("start");
    print(fib(1), fib(2), fib(3), fib(4), fib(5), fib(6), fib(7));
    banner("end");
}
`,
		expected: "== start ==\n1 1 2 3 5 8 13\n== end ==\n",
	}}
}

// TestKittenCallsExecute compiles, links, and runs both generated programs for every
// execution case, under the compiler the environment selects, and compares stdout,
// stderr, and exit status between them and against the values the source implies.
//
// Both units are generated independently - one by the reference generator and one by
// the Kitten-written one - and required to be identical before either is compiled, so
// this compares two generated programs rather than one text compiled twice. The
// generated entry point is the one the backend emits; nothing is injected.
func TestKittenCallsExecute(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	probeDirectory := t.TempDir()

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	executed := 0
	expectedChecked := 0
	for _, testCase := range callExecutionCases() {
		modules, checked, _ := prepareUnitExpectingSuccess(t, testCase.description, testCase.root, testCase.imported)
		if modules == nil {
			continue
		}
		referenceText, generationError := codegen.Generate(modules, checked)
		if generationError != nil {
			t.Errorf("%s: the reference rejected the program: %v", testCase.description, generationError)
			continue
		}
		kittenText, err := runKittenUnit(executable, probeDirectory, testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if difference := unitDifference(kittenText, referenceText); difference != "" {
			t.Errorf("%s: the translation unit differs: %s", testCase.description, difference)
			continue
		}

		results := make([]programResult, 0, 2)
		failed := false
		for index, text := range []string{referenceText, kittenText} {
			directory := t.TempDir()
			for _, file := range testCase.files {
				if err := os.WriteFile(filepath.Join(directory, file.name), []byte(file.content), 0o644); err != nil {
					t.Fatalf("writing %s: %v", file.name, err)
				}
			}
			result, err := buildAndRunProgram(t, compiler, compileFlags, linkFlags, directory, text, index)
			if err != nil {
				t.Errorf("%s: %v", testCase.description, err)
				failed = true
				break
			}
			results = append(results, result)
		}
		if failed || len(results) != 2 {
			continue
		}

		if results[0] != results[1] {
			t.Errorf("%s: the two programs behaved differently:\n reference: %+v\n     Kitten: %+v", testCase.description, results[0], results[1])
			continue
		}
		if results[0].status != 0 || results[0].stderr != "" {
			t.Errorf("%s: the program exited %d and wrote %q to stderr", testCase.description, results[0].status, results[0].stderr)
			continue
		}
		if testCase.expected != "" {
			if results[0].stdout != testCase.expected {
				t.Errorf("%s: the program printed\n%q\nwant\n%q", testCase.description, results[0].stdout, testCase.expected)
				continue
			}
			expectedChecked++
		} else if testCase.note == "" {
			t.Errorf("%s: the case has neither an expectation nor a note explaining why", testCase.description)
			continue
		}
		executed++
	}

	if executed != len(callExecutionCases()) {
		t.Fatalf("%d of %d programs were compiled and run", executed, len(callExecutionCases()))
	}
	t.Logf("execution: %d programs compiled, linked, and run under %s, %d checked against an expected output",
		executed, filepath.Base(compiler[0]), expectedChecked)
}

// --- Sensitivity -------------------------------------------------------------

// TestKittenCallsComparisonIsSensitive checks that the whole-unit comparison rejects
// the kinds of difference a call can get wrong. It corrupts a correct unit and
// requires the comparison to notice, so a comparison that accepted everything could
// not pass.
func TestKittenCallsComparisonIsSensitive(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	root := `fn twice(int value) -> int {
    return value * 2;
}
fn label(string prefix) -> string {
    return prefix + "!";
}
fn main() {
    print(twice(3));
    print(label("x"));
    print(1, 2);
    print(length("abcd"));
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
		{"a wrong callee name", strings.Replace(generated, "ktn_fn_twice(3)", "ktn_fn_thrice(3)", 1)},
		{"a dropped argument", strings.Replace(generated, "ktn_fn_twice(3)", "ktn_fn_twice()", 1)},
		{"reordered arguments", strings.Replace(generated, "ktn_rt_print_int(1), ktn_rt_print_space(), ktn_rt_print_int(2)", "ktn_rt_print_int(2), ktn_rt_print_space(), ktn_rt_print_int(1)", 1)},
		{"omitted parentheses", strings.Replace(generated, "ktn_fn_twice(3)", "ktn_fn_twice 3", 1)},
		{"a wrong print helper", strings.Replace(generated, "ktn_rt_print_int(ktn_fn_twice(3))", "ktn_rt_print_float(ktn_fn_twice(3))", 1)},
		{"a missing space helper", strings.Replace(generated, "ktn_rt_print_space(), ", "", 1)},
		{"a wrong length helper", strings.Replace(generated, "ktn_rt_string_length", "ktn_rt_array_length", 1)},
		{"a reordered literal pool", swapLines(generated, "ktn_literal_1", "ktn_literal_2")},
		{"a dropped call statement", strings.Replace(generated, "    (ktn_rt_print_int(ktn_fn_twice(3)), ktn_rt_print_newline());\n", "", 1)},
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

// --- Determinism and cost ----------------------------------------------------

// TestKittenCallsAreDeterministic checks that generation depends only on its input:
// the same program must produce the same bytes after a different program.
func TestKittenCallsAreDeterministic(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	root := "fn twice(int value) -> int {\n    return value * 2;\n}\nfn main() {\n    print(twice(3), \"text\");\n}\n"
	first, err := runKittenUnit(executable, directory, root, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := runKittenUnit(executable, directory, "fn main() {\n    print(substring(\"abc\", 0, 1));\n}\n", ""); err != nil {
		t.Fatalf("%v", err)
	}
	second, err := runKittenUnit(executable, directory, root, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if first != second {
		t.Errorf("generation depends on the previous run\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// nestedCallProgram builds a program whose single expression nests calls to the given
// depth, so that the cost of emitting calls can be measured.
func nestedCallProgram(depth int) string {
	var builder strings.Builder
	builder.WriteString("fn wrap(int value) -> int {\n    return value + 1;\n}\nfn main() {\n    print(")
	for index := 0; index < depth; index++ {
		builder.WriteString("wrap(")
	}
	builder.WriteString("0")
	for index := 0; index < depth; index++ {
		builder.WriteString(")")
	}
	builder.WriteString(");\n}\n")
	return builder.String()
}

// TestKittenCallsScaling measures emitting and generating a unit with deeply nested
// calls and checks it still matches the reference.
func TestKittenCallsScaling(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	directory := t.TempDir()

	previous := time.Duration(0)
	first := time.Duration(0)
	last := time.Duration(0)
	lines := 0
	for _, depth := range []int{25, 50, 100} {
		root := nestedCallProgram(depth)
		modules, checked, _ := prepareUnitExpectingSuccess(t, fmt.Sprintf("depth %d", depth), root, "")
		if modules == nil {
			continue
		}
		wantText, generationError := codegen.Generate(modules, checked)
		if generationError != nil {
			t.Fatalf("depth %d: the reference rejected the program: %v", depth, generationError)
		}

		best := time.Duration(0)
		var got string
		for repetition := 0; repetition < 3; repetition++ {
			start := time.Now()
			rendered, err := runKittenUnit(executable, directory, root, "")
			if err != nil {
				t.Fatalf("depth %d: %v", depth, err)
			}
			if elapsed := time.Since(start); best == 0 || elapsed < best {
				best = elapsed
			}
			got = rendered
		}
		if difference := unitDifference(got, wantText); difference != "" {
			t.Fatalf("depth %d: the translation unit differs: %s", depth, difference)
		}
		lines += strings.Count(wantText, "\n")

		growth := ""
		if previous > 0 {
			growth = fmt.Sprintf(", x%.1f", float64(best)/float64(previous))
		}
		t.Logf("depth=%4d lines=%6d bytes=%8d  %10s%s", depth, strings.Count(wantText, "\n"), len(got), best.Round(time.Millisecond), growth)
		previous = best
		if first == 0 {
			first = best
		}
		last = best
	}

	if lines == 0 {
		t.Fatalf("no unit was compared")
	}
	// Four times the nesting, and the bound is far looser than linear, so it only
	// catches a different order of cost.
	if last > first*100 {
		t.Errorf("a four-fold deeper nest cost %v against %v, which is a different order of cost", last, first)
	}
}
