package main

// Differential testing for the Kitten-written declaration emitter
// (selfhost/codegen/declarations.ktn, driven by selfhost/declarations.ktn).
//
// The harness builds the declaration probe once, feeds it a pair of modules, and
// compares its standard output with the declaration section of the C the
// reference generator produces for the same modules. The probe prints the
// section one line at a time, so its output is the section byte for byte.
//
// Extracting the reference section. The reference emits the whole translation
// unit, so the declaration section has to be separated from what follows it. The
// boundary is structural, not positional: prototypes are emitted before any body,
// one per function, and a definition is the same signature followed by " {" in
// place of ";". The section therefore ends at the earliest line that is a
// prototype with its semicolon replaced by " {". Constructors cannot be mistaken
// for it, because they are named ktn_new_ and no prototype precedes them with the
// same signature, and a call inside a body is indented, so it is not a prototype
// line. The extraction never looks at the literal pool or the entry point, so it
// does not depend on whether a program uses a string literal.
//
// Scope. The declaration emitter covers the nine sections the reference emits
// before function bodies. It does not validate the entry point, which the
// reference does before emitting anything; programs the reference rejects for an
// entry-point reason are therefore skipped by the corpus comparison and covered
// by a test of their own. Function bodies, expressions, statements, the literal
// pool, and the C entry point are later phases.
//
// The reference is the Go generator in kitten/codegen, which remains the
// production backend.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/codegen"
	"kitten/lexer"
	"kitten/parser"
	"kitten/runtime"
)

// kittenDeclarationsProgram is the driver program that prints the declaration
// section.
func kittenDeclarationsProgram() string {
	return filepath.Join("..", "..", "selfhost", "declarations.ktn")
}

// buildKittenDeclarations builds the declaration probe. It fails if the build
// produces any C diagnostic, so the probe must compile warning-free.
func buildKittenDeclarations(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-declarations")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenDeclarationsProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the declaration probe: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the declaration probe:\n%s", warnings)
	}
	return executable
}

// runKittenDeclarations writes the modules and runs the declaration probe. A
// non-zero capacity is passed as the probe's "#capacity" directive, which is how
// the capacity tests ask for a specific chunk count; zero asks for the derived
// one.
func runKittenDeclarations(t *testing.T, executable, directory, root, imported string, capacity int) (string, error) {
	t.Helper()

	source := root
	if capacity != 0 {
		source = fmt.Sprintf("#capacity %d\n", capacity) + root
	}
	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte(source), 0o644); err != nil {
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
		return "", fmt.Errorf("running the declaration probe: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return "", fmt.Errorf("the declaration probe wrote to stderr: %s", stderr.String())
	}
	return stdout.String(), nil
}

// --- Reference outcome -------------------------------------------------------

// referenceModules lexes and parses the sources into modules, the first being
// the root.
func referenceModules(sources ...string) ([]*parser.Module, error) {
	modules := make([]*parser.Module, 0, len(sources))
	for index, source := range sources {
		tokens, err := lexer.Lex(source)
		if err != nil {
			return nil, err
		}
		program, err := parser.Parse(tokens)
		if err != nil {
			return nil, err
		}
		path := "input.txt"
		if index > 0 {
			path = "input2.txt"
		}
		modules = append(modules, &parser.Module{Path: path, ID: index, Program: program})
	}
	return modules, nil
}

// referenceAcceptsEntryPoint reports whether the reference generator's entry
// point rule accepts the root module, using the checker's own resolved
// signatures: a zero-parameter, void-returning "main" declared in the root.
//
// The declaration emitter deliberately does not implement this rule yet - the
// reference applies it before emitting anything, and validating the entry point
// belongs with emitting it - so the corpus comparison skips the programs it
// rejects for this reason.
func referenceAcceptsEntryPoint(modules []*parser.Module, checked *checker.Result) bool {
	for _, declaration := range modules[0].Program.Declarations {
		function, ok := declaration.(*parser.FunctionDecl)
		if !ok || function == nil || function.Name != "main" {
			continue
		}
		info := checked.FunctionDecls[function]
		if info == nil {
			return false
		}
		return len(info.ParameterTypes) == 0 && info.ReturnType.IsVoid()
	}
	return false
}

// checkErrorLine renders a semantic error the way the probe's driver prints it.
func checkErrorLine(err error) (string, bool) {
	var semantic *checker.Error
	if !errors.As(err, &semantic) {
		return "", false
	}
	return fmt.Sprintf("error Check %s %s %d %d %s\n",
		checkerKindName(semantic.Kind), semantic.File, semantic.Line, semantic.Column, semantic.Message), true
}

// generateErrorLine renders a code generation error the way the probe's driver
// prints it. The reference records the file for the entry point rule only; the
// declaration sections never set it, and the driver reads fixed paths, so the
// rendering omits it.
func generateErrorLine(err error) (string, bool) {
	var generation *codegen.Error
	if !errors.As(err, &generation) {
		return "", false
	}
	return fmt.Sprintf("error Generate %d %d %s\n", generation.Line, generation.Column, generation.Message), true
}

// --- Section extraction ------------------------------------------------------

// referenceDeclarationSection returns everything the reference generator emitted
// before its first function body or pooled string literal.
//
// The reference writes the declaration sections into its prologue buffer, then
// generates the bodies into a separate buffer, then appends the pooled string
// literals to the prologue and the bodies after them. The section therefore ends
// at whichever comes first:
//
//   - a pooled literal declaration, which is the only line that begins with
//     "static const ktn_string ktn_literal_";
//   - a function definition, which is a prototype with " {" in place of ";".
//
// Both are recognized structurally. Constructors cannot be mistaken for a
// definition, because they are named ktn_new_ and no prototype shares a
// constructor's signature, and a call inside a body is indented, so it is not a
// prototype line.
func referenceDeclarationSection(t *testing.T, generated string) string {
	t.Helper()

	lines := strings.Split(generated, "\n")

	// Prototypes are the only unindented lines that end with a semicolon and
	// mention a generated function name.
	var signatures []string
	for _, line := range lines {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if !strings.HasSuffix(line, ";") || !strings.Contains(line, " ktn_fn_") {
			continue
		}
		signatures = append(signatures, strings.TrimSuffix(line, ";"))
	}
	if len(signatures) == 0 {
		t.Fatalf("the reference output has no function prototype:\n%s", generated)
	}

	end := -1
	for index, line := range lines {
		cut := pooledLiteralPattern.MatchString(line)
		if !cut {
			for _, signature := range signatures {
				if line == signature+" {" {
					cut = true
					break
				}
			}
		}
		if cut {
			end = index
			break
		}
	}
	if end < 0 {
		t.Fatalf("the reference output has no function definition:\n%s", generated)
	}
	return strings.Join(lines[:end], "\n") + "\n"
}

// referenceDeclarations runs the reference generator and returns its declaration
// section. The program must check and generate successfully.
func referenceDeclarations(t *testing.T, sources ...string) string {
	t.Helper()

	generated := referenceC(t, sources...)
	return referenceDeclarationSection(t, generated)
}

// --- Targeted cases ----------------------------------------------------------

// declarationsCase is one pair of modules and the reference comparison for it.
type declarationsCase struct {
	description string
	root        string
	imported    string
}

func declarationsCases() []declarationsCase {
	return []declarationsCase{{
		description: "minimal entry point",
		root:        "fn main() {\n}\n",
	}, {
		description: "prototypes, mutual recursion, and forward references",
		root: `fn even(int n) -> bool {
    if (n == 0) {
        return true;
    }
    return odd(n - 1);
}
fn odd(int n) -> bool {
    if (n == 0) {
        return false;
    }
    return even(n - 1);
}
fn main() {
    print(even(4));
}
`,
	}, {
		description: "primitive and string signatures",
		root: `fn combine(int a, float b, bool c, string d) -> string {
    return d;
}
fn ratio() -> float {
    return 1.5;
}
fn nothing() {
}
fn main() {
    print(combine(1, 2.0, true, "x"), ratio());
    nothing();
}
`,
	}, {
		description: "collections in parameters, returns, and fields",
		root: `struct Bag {
    int[] values;
    string[] names;
    map[string]int counts;
    int[][] grid;
}
fn total(int[] values) -> int {
    return length(values);
}
fn counts() -> map[string]int {
    return new map[string]int;
}
fn main() {
    Bag bag = Bag {
        values = new int[1],
        names = new string[1],
        counts = new map[string]int,
        grid = new int[][1]
    };
    print(total(bag.values), length(counts()));
}
`,
	}, {
		description: "structs referring to structs declared later",
		root: `struct Outer {
    Inner inner;
    int tag;
}
struct Inner {
    int value;
}
fn make() -> Outer {
    Inner inner = Inner { value = 1 };
    return Outer { inner = inner, tag = 2 };
}
fn main() {
    print(make().tag);
}
`,
	}, {
		description: "enums with several cases",
		root: `enum Color {
    Red,
    Green,
    Blue
}
enum Direction {
    North,
    South
}
fn main() {
    Color c = Color.Blue;
    print(c == Color.Red);
}
`,
	}, {
		description: "unions with payloads",
		root: `struct Node {
    int value;
}
union Value {
    Number { int amount; }
    Pair { int left; int right; }
    Text { string label; }
    Empty
}
union Wrapper {
    Inner { Value value; }
    None
}
fn describe(Value v) -> int {
    match (v) {
        Number(amount) { return amount; }
        Pair(left, right) { return left + right; }
        Text(label) { return length(label); }
        Empty { return 0; }
    }
}
fn main() {
    print(describe(Value.Pair { left = 1, right = 2 }));
}
`,
	}, {
		description: "collection and named types in variant payloads",
		root: `enum Kind { A, B }
struct Item {
    int value;
}
union Payload {
    Items { Item[] items; }
    Table { map[string]int table; }
    Kinds { Kind[] kinds; }
    Nested { Payload[] children; }
    Nothing
}
fn count(Payload p) -> int {
    match (p) {
        Items(items) { return length(items); }
        Table(table) { return length(table); }
        Kinds(kinds) { return length(kinds); }
        Nested(children) { return length(children); }
        Nothing { return 0; }
    }
}
fn main() {
    print(count(Payload.Nothing));
}
`,
	}, {
		description: "public and private declarations",
		root: `public struct Shown {
    int a;
}
private struct Hidden {
    int b;
}
public fn shown() -> int {
    return 1;
}
private fn hidden() -> int {
    return 2;
}
fn main() {
    Shown s = Shown { a = 1 };
    Hidden h = Hidden { b = 2 };
    print(s.a, h.b, shown(), hidden());
}
`,
	}, {
		description: "an imported module supplies declarations",
		root: `import "input2.txt";
fn main() {
    Point p = make();
    print(p.x, other());
}
`,
		imported: `public struct Point {
    int x;
    string label;
}
public enum Color {
    Red,
    Green
}
public union Value {
    Number { int n; }
    Empty
}
public fn make() -> Point {
    return Point { x = 1, label = "p" };
}
public fn other() -> int {
    return 2;
}
private fn secret() -> int {
    return 3;
}
`,
	}, {
		description: "identically named private declarations in two modules",
		root: `import "input2.txt";
struct Item {
    int a;
}
fn main() {
    Item item = Item { a = 1 };
    print(item.a, other());
}
`,
		imported: `struct Item {
    int b;
    string c;
}
public fn other() -> int {
    Item item = Item { b = 1, c = "x" };
    return item.b;
}
`,
	}, {
		description: "declaration order does not affect shared names",
		root: `fn main() {
    print(later(), first());
}
fn first() -> int {
    return 1;
}
enum Order { One, Two }
struct Late {
    Order order;
    int value;
}
fn later() -> int {
    Late late = Late { order = Order.Two, value = 2 };
    return late.value;
}
union Either {
    Left { Late late; }
    Right
}
`,
	}, {
		description: "bodies with and without string literals",
		root: `fn with_literal() -> string {
    string a = "one";
    string b = "two";
    return a + b;
}
fn without_literal() -> int {
    int total = 0;
    int index = 0;
    while (index < 3) {
        total += index;
        index++;
    }
    return total;
}
fn main() {
    print(with_literal(), without_literal());
}
`,
	}, {
		description: "duplicate literal text shared by the pool",
		root: `fn main() {
    print("same", "same", "other");
}
`,
	}, {
		description: "many declarations of every kind",
		root:        generatedDeclarationProgram(12),
	}, {
		description: "wide field and parameter lists",
		root:        longListsProgram(20),
	}}
}

// generatedDeclarationProgram builds a valid program with many declarations, so
// that the emitter is compared at a size the fixed corpus does not reach. Each
// kind contributes the same number of declarations, which makes the sections
// long enough to exercise the counting pass across many chunks.
func generatedDeclarationProgram(size int) string {
	var builder strings.Builder
	for index := 0; index < size; index++ {
		fmt.Fprintf(&builder, "struct S%d {\n    int a%d;\n    string b%d;\n    int[] c%d;\n}\n",
			index, index, index, index)
		fmt.Fprintf(&builder, "enum E%d {\n    A%d,\n    B%d,\n    C%d\n}\n", index, index, index, index)
		fmt.Fprintf(&builder, "union U%d {\n    N%d { int v%d; S%d item; }\n    Z%d\n}\n",
			index, index, index, index, index)
		fmt.Fprintf(&builder, "fn f%d(int p%d, S%d q%d, E%d e%d) -> int {\n    return p%d + q%d.a%d;\n}\n",
			index, index, index, index, index, index, index, index, index)
	}
	builder.WriteString("fn main() {\n")
	for index := 0; index < size; index++ {
		fmt.Fprintf(&builder, "    S%d s%d = S%d { a%d = 1, b%d = \"x\", c%d = new int[1] };\n",
			index, index, index, index, index, index)
		fmt.Fprintf(&builder, "    print(f%d(1, s%d, E%d.A%d));\n", index, index, index, index)
	}
	builder.WriteString("}\n")
	return builder.String()
}

// longListsProgram builds a program with wide field and parameter lists, so that
// long declaration lines are compared as well as many of them.
func longListsProgram(count int) string {
	var builder strings.Builder
	builder.WriteString("struct Wide {\n")
	for index := 0; index < count; index++ {
		fmt.Fprintf(&builder, "    int f%d;\n", index)
	}
	builder.WriteString("}\nfn wide(")
	for index := 0; index < count; index++ {
		if index > 0 {
			builder.WriteString(", ")
		}
		fmt.Fprintf(&builder, "int a%d", index)
	}
	builder.WriteString(") -> int {\n    return a0;\n}\nfn main() {\n    Wide container = Wide {")
	for index := 0; index < count; index++ {
		if index > 0 {
			builder.WriteString(", ")
		}
		fmt.Fprintf(&builder, "f%d = %d", index, index)
	}
	builder.WriteString("};\n    print(container.f0, wide(")
	for index := 0; index < count; index++ {
		if index > 0 {
			builder.WriteString(", ")
		}
		fmt.Fprintf(&builder, "%d", index)
	}
	builder.WriteString("));\n}\n")
	return builder.String()
}

// TestKittenDeclarationsMatchReference compares the probe's declaration section
// with the reference generator's for each targeted program.
func TestKittenDeclarationsMatchReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	directory := t.TempDir()

	compared := 0
	for _, testCase := range declarationsCases() {
		got, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, 0)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		var want string
		if testCase.imported == "" {
			want = referenceDeclarations(t, testCase.root, "")
		} else {
			want = referenceDeclarations(t, testCase.root, testCase.imported)
		}
		if got != want {
			t.Errorf("%s: declaration section differs\n--- probe ---\n%s--- reference ---\n%s", testCase.description, got, want)
			continue
		}
		if !strings.HasPrefix(got, "/* Generated by the Kitten compiler. Do not edit. */\n") {
			t.Errorf("%s: the section does not begin with the generated-file comment", testCase.description)
		}
		if !strings.Contains(got, "#include \"kitten_runtime.h\"\n") {
			t.Errorf("%s: the section does not include the runtime header", testCase.description)
		}
		compared++
	}

	if compared != len(declarationsCases()) {
		t.Fatalf("%d of %d targeted programs were compared", compared, len(declarationsCases()))
	}
	t.Logf("declaration sections: %d targeted programs match the reference byte for byte", compared)
}

// TestKittenDeclarationsSectionsArePresent checks that the targeted programs
// actually exercise every declaration section, so a section that silently emits
// nothing cannot pass the comparison.
func TestKittenDeclarationsSectionsArePresent(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	directory := t.TempDir()

	var sections = []struct {
		description string
		marker      string
	}{
		{"forward typedefs", "typedef struct ktn_s_Point ktn_s_Point;"},
		{"struct definitions", "struct ktn_s_Point {"},
		{"struct constructors", "ktn_s_Point *ktn_new_Point("},
		{"enum declarations", "typedef int64_t ktn_e_Color;"},
		{"enum cases", "ktn_ec_Color_Red = 0,"},
		{"union tag constants", "ktn_uc_Value_Number = 0,"},
		{"union definitions", "struct ktn_s_Value {"},
		{"union tag member", "int64_t ktn_tag;"},
		{"variant payload members", "int64_t ktn_v_Number_n;"},
		{"variant constructors", "ktn_s_Value *ktn_new_Value_Number("},
		{"function prototypes", "void ktn_fn_main(void);"},
		{"allocation call", "ktn_rt_alloc(sizeof(ktn_s_Point))"},
	}
	root := `struct Point {
    int x;
}
enum Color { Red, Green }
union Value {
    Number { int n; }
    Empty
}
fn main() {
    Point p = Point { x = 1 };
    print(p.x);
}
`
	got, err := runKittenDeclarations(t, executable, directory, root, "", 0)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, section := range sections {
		if !strings.Contains(got, section.marker) {
			t.Errorf("the %s section is missing %q:\n%s", section.description, section.marker, got)
		}
	}
}

// TestKittenDeclarationsEmptySecondModuleIsInert checks that an empty imported
// module changes nothing, which is what lets the probe always read two modules
// while a targeted case supplies one.
func TestKittenDeclarationsEmptySecondModuleIsInert(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	directory := t.TempDir()

	for _, testCase := range declarationsCases() {
		if testCase.imported != "" {
			continue
		}
		withEmpty, err := runKittenDeclarations(t, executable, directory, testCase.root, "", 0)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		oneModule := referenceDeclarations(t, testCase.root)
		if withEmpty != oneModule {
			t.Errorf("%s: an empty second module changed the section\n--- two modules ---\n%s--- one module ---\n%s", testCase.description, withEmpty, oneModule)
		}
	}
}

// --- Corpus ------------------------------------------------------------------

// TestKittenDeclarationsCorpus compares the probe with the reference over the
// checker corpus, which includes every fixture, every self-hosted source, and the
// targeted checker cases. A program the checker rejects must be reported by the
// probe with the same error, and so must a program the reference generator
// rejects for a reason the declaration sections can reach. A program the
// reference rejects for its entry point rule is skipped, because that rule is not
// part of this phase.
func TestKittenDeclarationsCorpus(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	directory := t.TempDir()

	cases := checkerCorpus(t)
	sections := 0
	sectionLines := 0
	checkErrors := 0
	generationErrors := 0
	skipped := 0

	for _, testCase := range cases {
		modules, err := referenceModules(testCase.root, testCase.imported)
		if err != nil {
			// A program that does not lex or parse is already covered by the
			// lexer and parser harnesses, and the probe reports the same error.
			continue
		}

		checked, checkError := checker.CheckModules(modules)
		if checkError != nil {
			want, ok := checkErrorLine(checkError)
			if !ok {
				t.Errorf("%s: the reference checker returned %T", testCase.name, checkError)
				continue
			}
			got, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, 0)
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

		if !referenceAcceptsEntryPoint(modules, checked) {
			skipped++
			continue
		}

		generated, generationError := codegen.Generate(modules, checked)
		if generationError != nil {
			want, ok := generateErrorLine(generationError)
			if !ok {
				t.Errorf("%s: the reference generator returned %T", testCase.name, generationError)
				continue
			}
			got, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, 0)
			if err != nil {
				t.Errorf("%s: %v", testCase.name, err)
				continue
			}
			if got != want {
				t.Errorf("%s: the reported error differs\n got: %q\nwant: %q", testCase.name, got, want)
				continue
			}
			generationErrors++
			continue
		}

		want := referenceDeclarationSection(t, generated)
		got, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, 0)
		if err != nil {
			t.Errorf("%s: %v", testCase.name, err)
			continue
		}
		if got != want {
			t.Errorf("%s: declaration section differs\n--- probe ---\n%s--- reference ---\n%s", testCase.name, got, want)
			continue
		}
		sections++
		sectionLines += strings.Count(got, "\n")
	}

	total := sections + checkErrors + generationErrors + skipped
	if total < 1800 {
		t.Fatalf("the corpus looks too small: %d inputs", total)
	}
	// Most of the corpus is invalid programs, so the sections are the smaller
	// half; the scale of the sections is covered by the generated programs in the
	// targeted cases.
	if sections < 30 || checkErrors < 1000 {
		t.Fatalf("the corpus is not covered: %d sections with %d lines, %d check errors, %d generation errors, %d skipped, of %d inputs",
			sections, sectionLines, checkErrors, generationErrors, skipped, total)
	}
	t.Logf("declaration corpus: %d inputs, %d sections identical over %d lines, %d check errors identical, %d generation errors identical, %d skipped for a missing entry point",
		total, sections, sectionLines, checkErrors, generationErrors, skipped)
}

// countFunctionDeclarations counts the function declarations in a module set.
func countFunctionDeclarations(modules []*parser.Module) int {
	total := 0
	for _, module := range modules {
		for _, declaration := range module.Program.Declarations {
			if function, ok := declaration.(*parser.FunctionDecl); ok && function != nil {
				total++
			}
		}
	}
	return total
}

// TestKittenDeclarationsCorpusWithoutEntryPoint verifies the declaration section
// of the valid corpus programs whose root module has no entry point.
//
// The reference validates the entry point before it emits anything, so it refuses
// such a program and its section cannot be read directly. It can be derived
// instead: prepending "fn main() {}" to the root adds exactly one declaration - a
// function with an empty body, which contributes one prototype line and nothing
// else - and prototypes are emitted in declaration order, so that prototype is
// the first line of the prototype block.
//
// The prototype block is one line per function followed by one blank line when
// there is at least one function, so what has to be removed is exactly the
// prepended line, plus that block's blank line when the original program declared
// no function at all. The test asserts both halves of the derivation: the
// prepended prototype must occur exactly once as a whole line, and what remains
// must equal what the probe emitted for the unmodified program. A program whose
// modified form does not check or generate is left out, which is how a collision
// with a declaration named main is handled.
func TestKittenDeclarationsCorpusWithoutEntryPoint(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	directory := t.TempDir()

	verified := 0
	leftOut := 0
	for _, testCase := range checkerCorpus(t) {
		modules, err := referenceModules(testCase.root, testCase.imported)
		if err != nil {
			continue
		}
		checked, checkError := checker.CheckModules(modules)
		if checkError != nil {
			continue
		}
		if referenceAcceptsEntryPoint(modules, checked) {
			continue
		}

		// Only a program the reference would generate for, once an entry point is
		// added, can be compared this way.
		modified, err := referenceModules("fn main() {\n}\n"+testCase.root, testCase.imported)
		if err != nil {
			leftOut++
			continue
		}
		if _, checkError := checker.CheckModules(modified); checkError != nil {
			leftOut++
			continue
		}

		got, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, 0)
		if err != nil {
			t.Errorf("%s: %v", testCase.name, err)
			continue
		}
		if strings.HasPrefix(got, "error ") {
			t.Errorf("%s: the probe reported a failure for a program the checker accepts:\n%s", testCase.name, got)
			continue
		}

		withEntryPoint := referenceDeclarations(t, "fn main() {\n}\n"+testCase.root, testCase.imported)

		// Removing the prepended prototype must leave no trace of the prototype
		// block when the original program had no functions either.
		prototype := "void ktn_fn_main(void);\n"
		if countFunctionDeclarations(modules) == 0 {
			prototype += "\n"
		}
		if count := strings.Count(withEntryPoint, prototype); count != 1 {
			t.Errorf("%s: the prepended prototype occurs %d times in the reference section", testCase.name, count)
			continue
		}
		want := strings.Replace(withEntryPoint, prototype, "", 1)
		if got != want {
			t.Errorf("%s: declaration section differs\n--- probe ---\n%s--- derived ---\n%s", testCase.name, got, want)
			continue
		}
		verified++
	}

	if verified < 50 {
		t.Fatalf("only %d programs without an entry point were verified", verified)
	}
	t.Logf("declaration corpus without an entry point: %d programs verified by deriving the reference section, %d left out", verified, leftOut)
}

// --- Capacity ----------------------------------------------------------------

// TestKittenDeclarationsCapacity checks the derived capacity is exact: emitting
// with it succeeds and produces the section, emitting with one line fewer fails
// rather than producing a short section, and no smaller capacity succeeds either,
// which is what "exact" means for a chunk count.
func TestKittenDeclarationsCapacity(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	directory := t.TempDir()

	for _, testCase := range declarationsCases() {
		derived, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, 0)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		lines := strings.Count(derived, "\n")
		if lines == 0 {
			t.Errorf("%s: the section has no lines", testCase.description)
			continue
		}

		// succeeds reports whether a capacity produces exactly the section.
		succeeds := func(capacity int) bool {
			got, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, capacity)
			if err != nil {
				t.Errorf("%s: capacity %d: %v", testCase.description, capacity, err)
				return false
			}
			return got == derived
		}

		// The same capacity the emitter derives must reproduce the section.
		if !succeeds(lines) {
			t.Errorf("%s: emitting with the exact capacity did not reproduce the section", testCase.description)
		}
		roomy, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, lines+64)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if roomy != derived {
			t.Errorf("%s: a larger capacity changed the section", testCase.description)
		}

		// One line fewer must be rejected, and must not yield a short section.
		short, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, lines-1)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if short == derived {
			t.Errorf("%s: emitting with one line fewer still produced the whole section", testCase.description)
			continue
		}
		if !strings.HasPrefix(short, "error Generate ") {
			t.Errorf("%s: emitting with one line fewer did not report a failure:\n%s", testCase.description, short)
			continue
		}
		if !strings.Contains(short, "capacity") {
			t.Errorf("%s: the failure does not name the capacity: %q", testCase.description, short)
		}
		if strings.Contains(short, "typedef") || strings.Contains(short, "ktn_fn_") {
			t.Errorf("%s: the failure printed part of a section:\n%s", testCase.description, short)
		}

		// No smaller capacity may succeed: doubling finds a capacity that works,
		// and bisecting finds the smallest one, which must be the line count.
		failing := 0
		succeeding := 1
		for !succeeds(succeeding) {
			failing = succeeding
			succeeding = succeeding * 2
		}
		for succeeding-failing > 1 {
			middle := (failing + succeeding) / 2
			if succeeds(middle) {
				succeeding = middle
			} else {
				failing = middle
			}
		}
		if succeeding != lines {
			t.Errorf("%s: the smallest capacity that works is %d, but the section has %d lines", testCase.description, succeeding, lines)
		}
	}
}

// --- Failures and the entry point boundary -----------------------------------

// TestKittenDeclarationsRejectsInvalidPrograms checks that a program the checker
// rejects is never reported as a section, and that the error is the checker's
// own.
func TestKittenDeclarationsRejectsInvalidPrograms(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	directory := t.TempDir()

	cases := []struct {
		description string
		root        string
		imported    string
		wantPrefix  string
	}{
		{"duplicate struct declaration", "struct S {\n    int a;\n}\nstruct S {\n    int b;\n}\nfn main() {\n}\n", "", "error Check DuplicateDeclaration "},
		{"duplicate function declaration", "fn f() {\n}\nfn f() {\n}\nfn main() {\n}\n", "", "error Check DuplicateDeclaration "},
		{"void struct field", "struct S {\n    void a;\n}\nfn main() {\n}\n", "", "error Check InvalidType "},
		{"undefined type", "struct S {\n    Missing a;\n}\nfn main() {\n}\n", "", "error Check UndefinedName "},
		{"duplicate field", "struct S {\n    int a;\n    int a;\n}\nfn main() {\n}\n", "", "error Check DuplicateDeclaration "},
		{"duplicate variant", "union U {\n    A\n    A\n}\nfn main() {\n}\n", "", "error Check DuplicateDeclaration "},
		{"undefined call", "fn main() {\n    missing();\n}\n", "", "error Check UndefinedName "},
		{"private access across modules", "import \"input2.txt\";\nfn main() {\n    hidden();\n}\n", "fn hidden() {\n}\n", "error Check PrivateAccess "},
		{"lexical error", "fn main() {\n    int x = @;\n}\n", "", "error Lex "},
		{"syntax error", "fn main( {\n}\n", "", "error Parse "},
	}

	for _, testCase := range cases {
		got, err := runKittenDeclarations(t, executable, directory, testCase.root, testCase.imported, 0)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if !strings.HasPrefix(got, testCase.wantPrefix) {
			t.Errorf("%s: expected a %q report, got:\n%s", testCase.description, testCase.wantPrefix, got)
			continue
		}
		if strings.Contains(got, "ktn_") {
			t.Errorf("%s: a rejected program produced declaration text:\n%s", testCase.description, got)
		}
	}
}

// TestKittenDeclarationsWithoutEntryPoint pins the documented boundary: the
// declaration emitter does not validate the entry point, so it emits a section
// for a program the reference generator rejects for that reason. The expected
// section is written out by hand, so this also pins the exact format.
func TestKittenDeclarationsWithoutEntryPoint(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	directory := t.TempDir()

	root := "struct S {\n    int a;\n}\nfn helper() -> int {\n    return 1;\n}\n"
	got, err := runKittenDeclarations(t, executable, directory, root, "", 0)
	if err != nil {
		t.Fatalf("%v", err)
	}

	want := expectedProbeOutput([]string{
		"/* Generated by the Kitten compiler. Do not edit. */",
		"#include \"kitten_runtime.h\"",
		"",
		"typedef struct ktn_s_S ktn_s_S;",
		"",
		"int64_t ktn_fn_helper(void);",
		"",
		"struct ktn_s_S {",
		"    int64_t ktn_f_a;",
		"};",
		"",
		"ktn_s_S *ktn_new_S(int64_t ktn_arg_a) {",
		"    ktn_s_S *ktn_object = (ktn_s_S *)ktn_rt_alloc(sizeof(ktn_s_S));",
		"    ktn_object->ktn_f_a = ktn_arg_a;",
		"    return ktn_object;",
		"}",
		"",
	})
	if got != want {
		t.Errorf("the declaration section differs\n got:\n%s\nwant:\n%s", got, want)
	}

	// The reference rejects the same program before emitting anything, which is
	// the rule this phase leaves to the entry-point milestone.
	modules, err := referenceModules(root)
	if err != nil {
		t.Fatalf("preparing the reference modules: %v", err)
	}
	checked, checkError := checker.CheckModules(modules)
	if checkError != nil {
		t.Fatalf("checking with the reference: %v", checkError)
	}
	if _, generationError := codegen.Generate(modules, checked); generationError == nil {
		t.Fatalf("the reference generator accepted a program without an entry point")
	}
}

// --- Native compilation ------------------------------------------------------

// TestKittenDeclarationsCompile checks that representative declaration sections
// are valid C: each is compiled on its own against the runtime header with the
// project's warning flags, under both discovered compilers when the environment
// selects them.
//
// A declaration section is not a complete translation unit - it has no function
// bodies and no entry point - so this compiles it, it does not link it, and it
// does not claim that a complete generated program builds. That belongs to the
// phase that emits the rest of the unit.
func TestKittenDeclarationsCompile(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenDeclarations(t)
	probeDirectory := t.TempDir()

	for _, testCase := range declarationsCases() {
		section, err := runKittenDeclarations(t, executable, probeDirectory, testCase.root, testCase.imported, 0)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if strings.HasPrefix(section, "error ") {
			t.Errorf("%s: the probe reported a failure: %s", testCase.description, section)
			continue
		}

		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "program.c"), []byte(section), 0o644); err != nil {
			t.Fatalf("writing program.c: %v", err)
		}
		if err := os.WriteFile(filepath.Join(directory, runtime.HeaderFileName), []byte(runtime.HeaderSource()), 0o644); err != nil {
			t.Fatalf("writing the runtime header: %v", err)
		}

		compiler, err := discoverCompiler()
		if err != nil {
			t.Fatalf("discovering the C compiler: %v", err)
		}
		compileFlags, _ := gcBuildFlags()
		arguments := append([]string{}, compiler[1:]...)
		arguments = append(arguments, "-std=c11", "-Wall", "-Wextra")
		arguments = append(arguments, compileFlags...)
		arguments = append(arguments, "-c", "program.c", "-o", "program.o")

		command := exec.Command(compiler[0], arguments...)
		command.Dir = directory
		var diagnostics bytes.Buffer
		command.Stdout = &diagnostics
		command.Stderr = &diagnostics
		if err := command.Run(); err != nil {
			t.Errorf("%s: compiling the declaration section: %v\n%s", testCase.description, err, diagnostics.String())
			continue
		}
		if output := strings.TrimSpace(diagnostics.String()); output != "" {
			t.Errorf("%s: compiling the declaration section produced diagnostics:\n%s", testCase.description, output)
		}
	}
}
