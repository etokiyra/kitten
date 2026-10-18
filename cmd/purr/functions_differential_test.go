package main

// Differential testing for the Kitten-written statement and function-body emitter
// (selfhost/codegen/statements.ktn, driven by selfhost/functions.ktn).
//
// The probe renders the body of every function and prints the pooled string
// literal declarations those bodies produced, then the definitions themselves, in
// the order the reference writes them. The harness extracts the same two regions
// from the reference generator's output and compares them byte for byte:
//
//   - the pool, between the declaration sections and the first definition, which
//     is where the reference writes it;
//   - the definitions, from the first definition to the entry point, which this
//     phase does not emit.
//
// Both boundaries are structural. The pool's lines are the only ones beginning
// with a pooled literal declaration, a definition is a prototype with " {" in
// place of ";", and the entry point is the line `int main(int argc, char **argv) {`, which no
// generated name can be because a Kitten `main` is emitted as `ktn_fn_main`.
//
// A program is only compared when the reference can generate it and this emitter
// can render it in full. When a body needs an expression kind this phase does not
// implement, the probe reports a failure and the harness requires the failure to
// name that kind, so an unsupported case can never be counted as a comparison and
// a failure for any other reason is caught.
//
// A complete translation unit also needs the declaration sections and the entry
// point. The declaration sections are compared by
// declarations_differential_test.go, and the entry point is not emitted yet, so the
// execution test below supplies one and states what it does and does not claim.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kitten/checker"
	"kitten/parser"
	"kitten/runtime"
)

// kittenFunctionsProgram is the driver program that prints function definitions.
func kittenFunctionsProgram() string {
	return filepath.Join("..", "..", "selfhost", "functions.ktn")
}

// buildKittenFunctions builds the function probe. It fails if the build produces
// any C diagnostic, so the probe must compile warning-free.
func buildKittenFunctions(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-functions")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenFunctionsProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the function probe: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the function probe:\n%s", warnings)
	}
	return executable
}

// runKittenFunctions writes the modules and runs the function probe.
func runKittenFunctions(executable, directory, root, imported string) (string, error) {
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
		return "", fmt.Errorf("running the function probe: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return "", fmt.Errorf("the function probe wrote to stderr: %s", stderr.String())
	}
	return stdout.String(), nil
}

// functionPoolMarker and functionBodyMarker are the lines the driver prints
// between the two regions.
const (
	functionPoolMarker = "=== literal pool ==="
	functionBodyMarker = "=== functions ==="
)

// splitFunctionOutput splits the probe's output into the pool and the bodies.
func splitFunctionOutput(t *testing.T, output string) (string, string) {
	t.Helper()

	first := strings.Index(output, functionPoolMarker+"\n")
	if first < 0 {
		t.Fatalf("the probe output has no %q line:\n%s", functionPoolMarker, output)
	}
	rest := output[first+len(functionPoolMarker)+1:]
	second := strings.Index(rest, functionBodyMarker+"\n")
	if second < 0 {
		t.Fatalf("the probe output has no %q line:\n%s", functionBodyMarker, output)
	}
	return rest[:second], rest[second+len(functionBodyMarker)+1:]
}

// runFunctionProbe runs the driver and returns the pool and the bodies.
func runFunctionProbe(t *testing.T, executable, directory, root, imported string) (string, string) {
	t.Helper()

	output, err := runKittenFunctions(executable, directory, root, imported)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return splitFunctionOutput(t, output)
}

// referenceFunctionDefinitions returns the reference's function definitions,
// including the blank line that follows each, up to the entry point.
func referenceFunctionDefinitions(t *testing.T, generated string) string {
	t.Helper()

	_, bodyStart := referenceSectionBoundaries(t, generated)
	lines := strings.Split(generated, "\n")
	end := -1
	for index := bodyStart; index < len(lines); index++ {
		if lines[index] == "int main(int argc, char **argv) {" {
			end = index
			break
		}
	}
	if end < 0 {
		t.Fatalf("the reference output has no entry point:\n%s", generated)
	}
	return strings.Join(lines[bodyStart:end], "\n") + "\n"
}

// --- Targeted cases ----------------------------------------------------------

// functionCase is one program whose function definitions are compared.
type functionCase struct {
	description string
	root        string
	imported    string
}

func functionCases() []functionCase {
	return []functionCase{{
		description: "empty bodies",
		root: `fn nothing() {
}
fn main() {
}
`,
	}, {
		description: "locals with and without initializers",
		root: `fn locals() -> int {
    int initialized = 1;
    int untouched;
    string text;
    bool flag;
    return initialized;
}
fn main() {
}
`,
	}, {
		description: "expression statements",
		root: `fn counting(int limit) -> int {
    int index = 0;
    while (index < limit) {
        index++;
    }
    int second = 0;
    while (second < limit) {
        ++second;
    }
    int third = 0;
    while (third < limit) {
        third--;
        third++;
    }
    return index;
}
fn main() {
}
`,
	}, {
		description: "returns with and without a value",
		root: `fn value() -> int {
    return 1;
}
fn nothing() {
    return;
}
fn main() {
}
`,
	}, {
		description: "if, else, and else-if chains",
		root: `fn choose(int value) -> int {
    if (value > 10) {
        return 1;
    } else if (value > 5) {
        return 2;
    } else if (value > 0) {
        return 3;
    } else {
        return 4;
    }
}
fn guard(bool first, bool second) -> bool {
    if (first && second) {
        return true;
    } else {
        return false;
    }
}
fn single(bool flag) -> int {
    if (flag) {
        return 1;
    }
    return 0;
}
fn main() {
}
`,
	}, {
		description: "nested blocks and shadowed names",
		root: `fn shadow(int value) -> int {
    int name = 1;
    {
        int name = 2;
        {
            int name = 3;
            string text = "inner";
        }
        string text = "middle";
    }
    string text = "outer";
    return name;
}
fn main() {
}
`,
	}, {
		description: "while loops, nested",
		root: `fn nested_while(int limit) -> int {
    int outer = 0;
    while (outer < limit) {
        int inner = 0;
        while (inner < limit) {
            inner++;
        }
        outer++;
    }
    return outer;
}
fn main() {
}
`,
	}, {
		description: "for loops with every combination of components",
		root: `fn all_components(int limit) -> int {
    int index = 0;
    for (int step = 0; step < limit; step++) {
        index++;
    }
    return index;
}
fn no_initializer(int limit) -> int {
    int index = 0;
    for (; index < limit; index++) {
    }
    return index;
}
fn no_increment(int limit) -> int {
    int index = 0;
    for (int step = 0; step < limit; ) {
        step++;
        index++;
    }
    return index;
}
fn only_body(int limit) -> int {
    int index = 0;
    for (; ; ) {
    }
    return index;
}
fn main() {
}
`,
	}, {
		description: "conditions parenthesize only what they need to",
		root: `fn conditions(int value, bool flag) -> int {
    if (flag) {
        return 1;
    }
    if (value < 1) {
        return 2;
    }
    if ((value < 1) && (value > 0)) {
        return 3;
    }
    while (flag) {
        return 4;
    }
    for (; flag; ) {
        return 5;
    }
    return 6;
}
fn main() {
}
`,
	}, {
		description: "enum and union match statements with bindings",
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
fn unused_binding(Value value) -> int {
    match (value) {
        Number(amount) { return 1; }
        Pair(left, right) { return 2; }
        Empty { return 3; }
    }
}
fn main() {
}
`,
	}, {
		description: "match statements nested in statements and functions",
		root: `enum Kind { A, B }
fn nested(Kind kind, bool flag) -> int {
    match (kind) {
        A {
            if (flag) {
                return 1;
            }
            int local = 2;
            return local;
        }
        B {
            while (flag) {
                return 3;
            }
            return 4;
        }
    }
}
fn twice(Kind first, Kind second) -> int {
    match (first) {
        A { return 1; }
        B { return 2; }
    }
}
fn thrice(Kind kind) -> int {
    match (kind) {
        A { return 1; }
        B { return 2; }
    }
}
fn main() {
}
`,
	}, {
		description: "string literals in nested statements keep the pool's order",
		root: `fn strings(bool flag) -> string {
    string first = "one";
    {
        string second = "two";
        if (flag) {
            string third = "three";
            return third;
        }
    }
    string fourth = "one";
    return fourth;
}
fn shared() -> string {
    string again = "one";
    return again;
}
fn main() {
}
`,
	}, {
		description: "a second module's functions follow the first module's",
		root: `import "input2.txt";
fn main() {
}
fn local(int value) -> int {
    string text = "root";
    return value;
}
`,
		imported: `public fn imported(int value) -> int {
    string text = "imported";
    return value + 1;
}
fn helper(string text) -> string {
    return text;
}
`,
	}, {
		description: "an expression as a for-loop initializer",
		root: `fn stepping(int limit) -> int {
    int index = 0;
    for (index++; index < limit; index++) {
    }
    return index;
}
fn pre_increment(int limit) -> int {
    int index = 0;
    for (++index; index < limit; index++) {
    }
    return index;
}
fn main() {
}
`,
	}, {
		description: "a union with a string payload",
		root: `union Message {
    Text { string body; }
    Count { int amount; }
    Empty
}
fn render(Message message) -> string {
    match (message) {
        Text(body) { return body; }
        Count(amount) { return "count"; }
        Empty { return "empty"; }
    }
}
fn main() {
}
`,
	}, {
		description: "match statements inside loops and nested loops",
		root: `enum Kind { A, B }
fn inside(Kind kind, int limit) -> int {
    int index = 0;
    while (index < limit) {
        match (kind) {
            A { index++; }
            B { index++; }
        }
    }
    for (int outer = 0; outer < limit; outer++) {
        match (kind) {
            A { }
            B { }
        }
    }
    return index;
}
fn main() {
}
`,
	}, {
		description: "EnvResult parameters",
		root: `fn describe(EnvResult result) -> int {
    match (result) {
        Set(value) { return length(value); }
        Unset { return -1; }
    }
}
fn main() {
    print(describe(try_getenv("PATH")));
}
`,
	}, {
		description: "CurrentDirectoryResult parameters",
		root: `fn describe(CurrentDirectoryResult result) -> int {
    match (result) {
        Ok(path) { return length(path); }
        Failed(message) { return -1; }
    }
}
fn main() {
    print(describe(try_current_directory()));
}
`,
	}, {
		description: "RunResult parameters",
		root: `fn describe(RunResult result) -> int {
    match (result) {
        Ok(status) { return status; }
        Failed(message) { return -1; }
    }
}
fn main() {
    print(describe(try_run("/bin/true", new string[0])));
}
`,
	}, {
		description: "file and directory result parameters",
		root: `fn inspect(FileResult result) -> int {
    match (result) {
        Ok { return 1; }
        Failed(message) { return 2; }
    }
}
fn count(ListDirectoryResult result) -> int {
    match (result) {
        Ok(entries) { return length(entries); }
        Failed(message) { return -1; }
    }
}
fn main() {
    print(inspect(try_write_file("x", "y")));
    print(count(try_list_directory("d")));
}
`,
	}, {
		description: "byte parameters and byte arrays",
		root: `fn first(byte value, byte[] values) -> byte {
    return values[0];
}
fn main() {
    byte value = byte_from_int(65);
    byte[] values = new byte[2];
    values[0] = value;
}
`,
	}, {
		description: "bitwise and shift expressions",
		root: `fn mix(int value) -> int {
    int mask = 0xF0;
    int shifted = value << 3;
    return (shifted & mask) | (~value);
}
fn main() {
}
`,
	}, {
		description: "defined integer arithmetic and compound forms",
		root: `fn arithmetic(int value) -> int {
    int maximum = 9223372036854775807;
    int minimum = -9223372036854775807 - 1;
    value += maximum;
    value -= 1;
    value++;
    --value;
    return value + 1;
}
fn main() {
}
`,
	}, {
		description: "try_read_file result matching",
		root: `fn describe(ReadFileResult result) -> int {
    match (result) {
        Ok(text) { return 1; }
        Failed(message) { return 2; }
    }
}
fn main() {
    print(describe(try_read_file("x")));
}
`,
	}}
}

// TestKittenFunctionsNumberMatchTemporaries checks that the temporaries a match
// statement needs are numbered once per translation unit, in emission order, which
// is what makes their names unique and deterministic and what the reference does.
func TestKittenFunctionsNumberMatchTemporaries(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenFunctions(t)
	directory := t.TempDir()

	root := `enum Kind { A, B }
fn first(Kind kind) -> int {
    match (kind) {
        A { return 1; }
        B { return 2; }
    }
}
fn second(Kind kind) -> int {
    match (kind) {
        A { return 3; }
        B { return 4; }
    }
}
fn third(Kind kind) -> int {
    match (kind) {
        A { return 5; }
        B { return 6; }
    }
}
fn main() {
}
`
	_, _, generated, ok := checkProgram(t, "temporaries", root, "")
	if !ok {
		t.Fatalf("the reference rejected the program")
	}
	pool, bodies := runFunctionProbe(t, executable, directory, root, "")
	if difference := functionDifference(pool, bodies, referenceLiteralPool(t, generated), referenceFunctionDefinitions(t, generated)); difference != "" {
		t.Fatalf("%s", difference)
	}
	for index, name := range []string{"ktn_match_0", "ktn_match_1", "ktn_match_2"} {
		if !strings.Contains(bodies, name+" = ktn_var_kind;") {
			t.Errorf("the temporaries are not numbered in emission order: %s is missing\n%s", name, bodies)
		}
		_ = index
	}
	if strings.Contains(bodies, "ktn_match_3") {
		t.Errorf("a fourth temporary was numbered for three matches:\n%s", bodies)
	}
}

// TestKittenFunctionsMatchReference compares the probe's pooled declarations and
// function definitions with the reference generator's for each targeted program.
func TestKittenFunctionsMatchReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenFunctions(t)
	directory := t.TempDir()

	programs := 0
	definitions := 0
	comparedLines := 0
	for _, testCase := range functionCases() {
		modules, _, generated, ok := checkProgram(t, testCase.description, testCase.root, testCase.imported)
		if !ok {
			continue
		}
		wantPool := referenceLiteralPool(t, generated)
		wantBodies := referenceFunctionDefinitions(t, generated)

		pool, bodies := runFunctionProbe(t, executable, directory, testCase.root, testCase.imported)
		if difference := functionDifference(pool, bodies, wantPool, wantBodies); difference != "" {
			t.Errorf("%s: %s", testCase.description, difference)
			continue
		}
		// The definition count is checked against the reference's own declarations,
		// so that a body silently dropped cannot pass as a match.
		wantDefinitions := 0
		for _, module := range modules {
			for _, declaration := range module.Program.Declarations {
				if function, isFunction := declaration.(*parser.FunctionDecl); isFunction && function != nil {
					wantDefinitions++
				}
			}
		}
		if got := countDefinitions(bodies); got != wantDefinitions {
			t.Errorf("%s: %d definitions were emitted, want %d", testCase.description, got, wantDefinitions)
			continue
		}
		programs++
		definitions += wantDefinitions
		comparedLines += strings.Count(bodies, "\n") + strings.Count(pool, "\n")
	}

	if programs != len(functionCases()) {
		t.Fatalf("%d of %d targeted programs were compared", programs, len(functionCases()))
	}
	t.Logf("function bodies: %d targeted programs, %d functions, %d lines compared byte for byte", programs, definitions, comparedLines)
}

// countDefinitions returns the number of function definitions in a rendered body
// region: the unindented lines that open a brace after a parameter list.
func countDefinitions(bodies string) int {
	total := 0
	for _, line := range strings.Split(bodies, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if strings.HasSuffix(line, ") {") {
			total++
		}
	}
	return total
}

// --- Corpus ------------------------------------------------------------------

// TestKittenFunctionsCorpus runs the probe over the checker corpus. Every program
// the checker rejects must be reported with the checker's own error; every valid
// program whose bodies the reference can generate must have its pool and
// definitions match the reference byte for byte.
func TestKittenFunctionsCorpus(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenFunctions(t)
	directory := t.TempDir()

	comparedPrograms := 0
	comparedLines := 0
	comparedFunctions := 0
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
			got, err := runKittenFunctions(executable, directory, testCase.root, testCase.imported)
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

		// The reference validates the entry point before it emits anything, which
		// this phase does not implement, and non-ASCII input shifts the positions
		// the probe reports, because Kitten counts bytes.
		if !referenceAcceptsEntryPoint(modules, checked) || !isASCII(testCase.root) || !isASCII(testCase.imported) {
			skipped++
			continue
		}
		generated, generationError := referenceGenerate(modules, checked)
		if generationError != nil {
			skipped++
			continue
		}

		got, err := runKittenFunctions(executable, directory, testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", testCase.name, err)
			continue
		}
		if strings.HasPrefix(got, "error ") {
			t.Errorf("%s: the probe failed to generate a body:\n%s", testCase.name, got)
			continue
		}

		pool, bodies := splitFunctionOutput(t, got)
		if difference := functionDifference(pool, bodies, referenceLiteralPool(t, generated), referenceFunctionDefinitions(t, generated)); difference != "" {
			t.Errorf("%s: %s", testCase.name, difference)
			continue
		}
		comparedPrograms++
		comparedLines += strings.Count(bodies, "\n") + strings.Count(pool, "\n")
		comparedFunctions += countDefinitions(bodies)
	}

	total := comparedPrograms + checkErrors + skipped
	if total < 1800 {
		t.Fatalf("the corpus looks too small: %d inputs", total)
	}
	if comparedPrograms < 30 || comparedLines < 200 || comparedFunctions < 30 || checkErrors < 1000 {
		t.Fatalf("the corpus is not covered: %d compared programs over %d lines and %d functions, %d check errors, %d skipped, of %d inputs",
			comparedPrograms, comparedLines, comparedFunctions, checkErrors, skipped, total)
	}
	t.Logf("function corpus: %d inputs, %d programs identical over %d lines and %d functions, %d check errors identical, %d skipped",
		total, comparedPrograms, comparedLines, comparedFunctions, checkErrors, skipped)
}

// --- Assignment cases --------------------------------------------------------

// TestKittenFunctionsAssignmentsMatchReference renders assignments in every
// statement position and compares the whole pool and body region with the
// reference's. A body that contains an assignment is no longer rejected: it is
// emitted and compared like every other body.
func TestKittenFunctionsAssignmentsMatchReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenFunctions(t)
	directory := t.TempDir()

	cases := []struct {
		description string
		root        string
	}{
		{"assignment in a statement", "fn main() {\n    int a = 1;\n    a = 2;\n}\n"},
		{"assignment to a struct field", "struct P {\n    int x;\n}\nfn main() {\n    P p = P { x = 1 };\n    p.x = 2;\n}\n"},
		{"assignment to an index element", "fn main() {\n    int[] values = new int[2];\n    values[0] = 1;\n}\n"},
		{"compound assignment", "fn main() {\n    int a = 1;\n    a += 2;\n}\n"},
		{"assignment in a while condition", "fn main() {\n    bool first = true;\n    bool second = false;\n    while (first = second) {\n    }\n}\n"},
		{"assignment in a return", "fn value() -> int {\n    int a = 1;\n    return a = 2;\n}\nfn main() {\n}\n"},
		{"assignment in a for initializer", "fn main() {\n    int a = 1;\n    for (a = 0; a < 2; a++) {\n    }\n}\n"},
		{"assignment as a call argument", "fn take(int value) -> int {\n    return value;\n}\nfn main() {\n    int a = 1;\n    print(take(a = 2));\n}\n"},
		{"assignment in a match arm", "enum K { A }\nfn f(K k) -> int {\n    match (k) {\n        A { int a = 1; a = 2; }\n    }\n    return 0;\n}\nfn main() {\n}\n"},
		{"map entry assignment", "fn main() {\n    map[string]int counts = new map[string]int;\n    counts[\"a\"] = 1;\n    counts[\"a\"] += 2;\n    print(counts[\"a\"]);\n}\n"},
		{"string append", "fn main() {\n    string text = \"a\";\n    text += \"b\";\n    print(text);\n}\n"},
	}

	programs := 0
	lines := 0
	for _, testCase := range cases {
		modules, checked, generated, ok := prepareUnit(t, testCase.description, testCase.root, "")
		if !ok {
			continue
		}
		if generationError := generationErrorOf(modules, checked); generationError != nil {
			t.Errorf("%s: the reference rejected the program: %v", testCase.description, generationError)
			continue
		}
		got, err := runKittenFunctions(executable, directory, testCase.root, "")
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if strings.HasPrefix(got, "error ") {
			t.Errorf("%s: the probe rejected a program with an assignment:\n%s", testCase.description, got)
			continue
		}
		pool, bodies := splitFunctionOutput(t, got)
		wantPool := referenceLiteralPool(t, generated)
		wantBodies := referenceFunctionDefinitions(t, generated)
		if difference := functionDifference(pool, bodies, wantPool, wantBodies); difference != "" {
			t.Errorf("%s: %s", testCase.description, difference)
			continue
		}
		programs++
		lines += strings.Count(pool, "\n") + strings.Count(bodies, "\n")
	}

	if programs != len(cases) {
		t.Fatalf("%d of %d assignment programs were compared", programs, len(cases))
	}
	t.Logf("assignment bodies: %d programs over %d lines compared byte for byte", programs, lines)
}

// TestKittenFunctionsRejectInvalidPrograms checks that a program the front end
// rejects is reported with the front end's own error and produces no definitions.
func TestKittenFunctionsRejectInvalidPrograms(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenFunctions(t)
	directory := t.TempDir()

	cases := []struct {
		description string
		root        string
		wantPrefix  string
	}{
		{"lexical error", "fn main() {\n    int a = @;\n}\n", "error Lex "},
		{"syntax error", "fn main( {\n}\n", "error Parse "},
		{"type mismatch", "fn main() {\n    int a = true;\n}\n", "error Check TypeMismatch "},
		{"undefined name", "fn main() {\n    int a = missing;\n}\n", "error Check UndefinedName "},
		{"duplicate declaration", "fn f() {\n}\nfn f() {\n}\nfn main() {\n}\n", "error Check DuplicateDeclaration "},
		{"bad return type", "fn f() -> int {\n}\nfn main() {\n}\n", "error Check InvalidReturn "},
	}

	for _, testCase := range cases {
		got, err := runKittenFunctions(executable, directory, testCase.root, "")
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if !strings.HasPrefix(got, testCase.wantPrefix) {
			t.Errorf("%s: expected a %q report, got:\n%s", testCase.description, testCase.wantPrefix, got)
		}
		if strings.Contains(got, "ktn_fn_") {
			t.Errorf("%s: a rejected program produced definitions:\n%s", testCase.description, got)
		}
	}
}

// --- Sensitivity -------------------------------------------------------------

// functionDifference returns a description of the first difference between a
// probe's output and the reference's two regions, or the empty string when they
// agree. The tests above report what it returns, and the sensitivity test below
// requires it to reject a corrupted output.
func functionDifference(pool, bodies, wantPool, wantBodies string) string {
	if pool != wantPool {
		return fmt.Sprintf("literal pool differs\n--- probe ---\n%s--- reference ---\n%s", pool, wantPool)
	}
	if bodies != wantBodies {
		return fmt.Sprintf("function definitions differ\n--- probe ---\n%s--- reference ---\n%s", bodies, wantBodies)
	}
	return ""
}

// TestKittenFunctionsComparisonIsSensitive checks that the comparison the tests
// above rely on rejects the kinds of difference it is meant to catch: a changed
// indentation, a dropped pair of parentheses, reordered statements and functions,
// and a reordered pool. It corrupts a correct probe output and requires the
// comparison to notice, so a comparison that accepted everything could not pass.
func TestKittenFunctionsComparisonIsSensitive(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenFunctions(t)
	directory := t.TempDir()

	root := `fn mixed(int limit) -> int {
    int index = 0;
    while (index < limit) {
        index++;
    }
    string first = "one";
    string second = "two";
    if (index < limit) {
        return index;
    }
    return 0;
}
fn other() -> int {
    return 1;
}
fn main() {
}
`
	_, _, generated, ok := checkProgram(t, "sensitivity", root, "")
	if !ok {
		t.Fatalf("the reference rejected the program")
	}
	wantPool := referenceLiteralPool(t, generated)
	wantBodies := referenceFunctionDefinitions(t, generated)
	pool, bodies := runFunctionProbe(t, executable, directory, root, "")

	if difference := functionDifference(pool, bodies, wantPool, wantBodies); difference != "" {
		t.Fatalf("the uncorrupted output already differs: %s", difference)
	}
	if wantPool == "" || strings.Count(wantPool, "ktn_literal_") < 2 {
		t.Fatalf("the sensitivity program must pool at least two literals:\n%s", wantPool)
	}

	corruptions := []struct {
		description string
		pool        string
		bodies      string
	}{
		{"identical output", wantPool, wantBodies},
		{"changed indentation", wantPool, strings.Replace(wantBodies, "    int64_t ktn_var_index = 0;", "  int64_t ktn_var_index = 0;", 1)},
		{"dropped parentheses", wantPool, strings.Replace(wantBodies, "(ktn_var_index < ktn_var_limit)", "ktn_var_index < ktn_var_limit", 1)},
		{"reordered statements", wantPool, swapLines(wantBodies, "ktn_var_index = 0;", "return 0;")},
		{"reordered functions", wantPool, swapLines(wantBodies, "int64_t ktn_fn_mixed", "int64_t ktn_fn_other")},
		{"reordered pool", swapLines(wantPool, "ktn_literal_1", "ktn_literal_2"), wantBodies},
		{"dropped definition", wantPool, strings.Replace(wantBodies, "\nint64_t ktn_fn_other(void) {\n    return 1;\n}\n", "", 1)},
	}
	for _, corruption := range corruptions {
		difference := functionDifference(corruption.pool, corruption.bodies, wantPool, wantBodies)
		if corruption.description == "identical output" {
			if difference != "" {
				t.Errorf("identical output was reported as different: %s", difference)
			}
			continue
		}
		if corruption.pool == wantPool && corruption.bodies == wantBodies {
			t.Errorf("%s: the corruption changed nothing", corruption.description)
			continue
		}
		if difference == "" {
			t.Errorf("%s: the comparison accepted a corrupted output", corruption.description)
		}
	}
}

// swapLines exchanges the first line containing first with the first line
// containing second, so that a comparison can be shown to notice an order change.
// A direct exchange is used rather than a move, so that adjacent lines are
// reordered too.
func swapLines(text, first, second string) string {
	lines := strings.Split(text, "\n")
	firstIndex := -1
	secondIndex := -1
	for index, line := range lines {
		if firstIndex < 0 && strings.Contains(line, first) {
			firstIndex = index
		}
		if secondIndex < 0 && strings.Contains(line, second) {
			secondIndex = index
		}
	}
	if firstIndex < 0 || secondIndex < 0 || firstIndex == secondIndex {
		return text
	}
	lines[firstIndex], lines[secondIndex] = lines[secondIndex], lines[firstIndex]
	return strings.Join(lines, "\n")
}

// --- Execution ---------------------------------------------------------------

// TestKittenFunctionsCompileAndRun compiles and runs the code this emitter
// produces, under both compilers.
//
// The unit it runs is not a translation unit this phase emits: it is the
// reference's declaration section - which declarations_differential_test.go
// already compares byte for byte - followed by the pool and the definitions this
// emitter produced, followed by a test-supplied entry point that calls the
// generated functions and prints their results with printf. The entry point is
// supplied here because this phase does not emit one, and the expectation is
// written out by hand rather than read from the reference, so a textual agreement
// is not what is being checked: the generated code is compiled, linked against the
// runtime, executed, and its output compared with the values the source implies.
func TestKittenFunctionsCompileAndRun(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenFunctions(t)
	probeDirectory := t.TempDir()

	root := `fn count(int limit) -> int {
    int index = 0;
    while (index < limit) {
        index++;
    }
    for (int step = 0; step < 2; step++) {
        index++;
    }
    return index;
}
fn choose(int value) -> int {
    if (value > 10) {
        return 1;
    } else if (value > 5) {
        return 2;
    } else {
        return 3;
    }
}
fn greeting(string prefix) -> string {
    string base = "hello, ";
    return base + prefix;
}
fn describe(int kind) -> int {
    if (kind > 1) {
        return 10;
    }
    return 20;
}
fn main() {
}
`
	_, _, generated, ok := checkProgram(t, "execution", root, "")
	if !ok {
		t.Fatalf("the reference rejected the program")
	}
	pool, bodies := runFunctionProbe(t, executable, probeDirectory, root, "")
	if want := referenceLiteralPool(t, generated); pool != want {
		t.Fatalf("literal pool differs\n--- probe ---\n%s--- reference ---\n%s", pool, want)
	}
	if want := referenceFunctionDefinitions(t, generated); bodies != want {
		t.Fatalf("function definitions differ\n--- probe ---\n%s--- reference ---\n%s", bodies, want)
	}

	// The entry point the test supplies: it initializes the runtime, calls the
	// generated functions, and prints what they return.
	entry := `#include <stdio.h>

static const ktn_string prefix = { 5, "world" };

int main(void) {
    ktn_rt_init();
    printf("%lld\n", (long long)ktn_fn_count(3));
    printf("%lld\n", (long long)ktn_fn_choose(20));
    printf("%lld\n", (long long)ktn_fn_choose(7));
    printf("%lld\n", (long long)ktn_fn_choose(0));
    printf("%lld\n", (long long)ktn_fn_describe(2));
    ktn_rt_print_string(ktn_fn_greeting((&prefix)));
    ktn_rt_print_newline();
    return 0;
}
`
	unit := referenceDeclarationSection(t, generated) + pool + bodies + entry

	// count(3) counts to 3 and adds two more, choose picks by threshold, and
	// greeting concatenates.
	want := "5\n1\n2\n3\n10\nhello, world\n"

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "program.c"), []byte(unit), 0o644); err != nil {
		t.Fatalf("writing program.c: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, runtime.SourceFileName), []byte(runtime.Source()), 0o644); err != nil {
		t.Fatalf("writing the runtime: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, runtime.HeaderFileName), []byte(runtime.HeaderSource()), 0o644); err != nil {
		t.Fatalf("writing the runtime header: %v", err)
	}

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
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
		t.Fatalf("compiling the generated unit: %v\n%s", err, diagnostics.String())
	}
	if output := strings.TrimSpace(diagnostics.String()); output != "" {
		t.Fatalf("compiling the generated unit produced diagnostics:\n%s", output)
	}

	run := exec.Command(filepath.Join(directory, "program"))
	var stdout, stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("running the generated program: %v (stderr: %s)", err, stderr.String())
	}
	if stdout.String() != want {
		t.Errorf("the generated program printed\n%q\nwant\n%q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Errorf("the generated program wrote to stderr: %s", stderr.String())
	}
	t.Logf("generated unit compiled and ran under %s with output %q", filepath.Base(compiler[0]), stdout.String())
}

// --- Performance -------------------------------------------------------------

// nestedStatementProgram builds a program whose body nests blocks, ifs, and loops
// to the given depth, so that the cost of emitting statements can be measured.
func nestedStatementProgram(depth int) string {
	var builder strings.Builder
	builder.WriteString("fn nested(int limit) -> int {\n")
	builder.WriteString("    int index = 0;\n")
	for level := 0; level < depth; level++ {
		builder.WriteString("    while (index < limit) {\n")
		builder.WriteString("        if (index < limit) {\n")
	}
	builder.WriteString("        index++;\n")
	for level := 0; level < depth; level++ {
		builder.WriteString("        }\n")
		builder.WriteString("    }\n")
	}
	builder.WriteString("    return index;\n}\nfn main() {\n}\n")
	return builder.String()
}

// TestKittenFunctionsScaling measures emitting a deeply nested body and checks the
// definitions still agree with the reference at each depth.
func TestKittenFunctionsScaling(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenFunctions(t)
	directory := t.TempDir()

	previous := time.Duration(0)
	first := time.Duration(0)
	last := time.Duration(0)
	lines := 0
	for _, depth := range []int{20, 40, 80} {
		root := nestedStatementProgram(depth)
		_, _, generated, ok := checkProgram(t, fmt.Sprintf("depth %d", depth), root, "")
		if !ok {
			continue
		}
		wantPool := referenceLiteralPool(t, generated)
		wantBodies := referenceFunctionDefinitions(t, generated)

		best := time.Duration(0)
		var bodies string
		for repetition := 0; repetition < 3; repetition++ {
			start := time.Now()
			pool, rendered := runFunctionProbe(t, executable, directory, root, "")
			if elapsed := time.Since(start); best == 0 || elapsed < best {
				best = elapsed
			}
			if pool != wantPool {
				t.Fatalf("depth %d: literal pool differs", depth)
			}
			bodies = rendered
		}
		if bodies != wantBodies {
			t.Fatalf("depth %d: function definitions differ\n--- probe ---\n%s--- reference ---\n%s", depth, bodies, wantBodies)
		}
		lines += strings.Count(bodies, "\n")

		growth := ""
		if previous > 0 {
			growth = fmt.Sprintf(", x%.1f", float64(best)/float64(previous))
		}
		t.Logf("depth=%4d lines=%6d  %10s%s", depth, strings.Count(bodies, "\n"), best.Round(time.Millisecond), growth)
		previous = best
		if first == 0 {
			first = best
		}
		last = best
	}

	if lines == 0 {
		t.Fatalf("no nested statement body was compared")
	}
	// Four times the nesting, and the bound is far looser than linear, so it only
	// catches a different order of cost.
	if last > first*100 {
		t.Errorf("a four-fold deeper body cost %v against %v, which is a different order of cost", last, first)
	}
}
