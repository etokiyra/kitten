package main

// Differential testing for the Kitten-written C generator's foundation
// (selfhost/codegen.ktn and selfhost/codegen/*.ktn).
//
// The generator emitter does not exist yet, so there is no generated translation
// unit to compare. What exists is the foundation the emitter builds on - C name
// mangling, the C type mapping, C string-literal escaping, and bounded output
// accumulation - and this harness verifies those helpers against the reference
// implementation before anything depends on them.
//
// The Kitten side is selfhost/codegen.ktn, which reads directive lines from the
// fixed relative path "input.txt" and prints one result line per directive. The
// harness writes the directives and predicts each result.
//
// Predictions are anchored to the reference wherever the reference can answer:
//
//   - The Kitten probe's escape results are compared with the pooled string
//     literal declarations in C the reference generator produced for a program
//     containing exactly that literal, so both the escaping and the byte length
//     are read out of the reference's own output rather than re-derived.
//   - Predicted names and C types are required to appear in the reference C for a
//     program that uses them, so a prediction cannot drift away from what the
//     reference implementation actually emits.
//
// The reference is the Go generator in kitten/codegen, which remains the
// production backend.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"kitten/checker"
	"kitten/codegen"
	"kitten/lexer"
	"kitten/parser"
)

// kittenCodegenProgram is the driver program of the Kitten-written generator.
func kittenCodegenProgram() string {
	return filepath.Join("..", "..", "selfhost", "codegen.ktn")
}

// buildKittenCodegen builds the generator probe. It fails if the build produces
// any C diagnostic, so the probe must compile warning-free.
func buildKittenCodegen(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-codegen")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenCodegenProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the codegen probe: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the codegen probe:\n%s", warnings)
	}
	return executable
}

// runKittenCodegen writes the directives and runs the generator probe.
func runKittenCodegen(executable, directory, directives string) (string, error) {
	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte(directives), 0o644); err != nil {
		return "", fmt.Errorf("writing input.txt: %v", err)
	}

	command := exec.Command(executable)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("running the codegen probe: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return "", fmt.Errorf("the codegen probe wrote to stderr: %s", stderr.String())
	}
	return stdout.String(), nil
}

// --- The reference generator -------------------------------------------------

// referenceC lexes, parses, and checks the sources and generates C with the
// reference backend. The first source is the root module.
func referenceC(t *testing.T, sources ...string) string {
	t.Helper()

	modules := make([]*parser.Module, 0, len(sources))
	for index, source := range sources {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("lexing module %d: %v", index, err)
		}
		program, err := parser.Parse(tokens)
		if err != nil {
			t.Fatalf("parsing module %d: %v", index, err)
		}
		path := "input.txt"
		if index > 0 {
			path = fmt.Sprintf("input%d.txt", index)
		}
		modules = append(modules, &parser.Module{Path: path, ID: index, Program: program})
	}

	checked, err := checker.CheckModules(modules)
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	generated, err := codegen.Generate(modules, checked)
	if err != nil {
		t.Fatalf("generating with the reference generator: %v", err)
	}
	return generated
}

// pooledLiteral is one pooled static string literal declaration in generated C:
//
//	static const ktn_string ktn_literal_N = { LENGTH, "ESCAPED" };
type pooledLiteral struct {
	name    string
	length  string
	escaped string
}

// pooledLiteralPattern matches a pooled literal declaration. The escaped text is
// matched greedily so that a literal containing the pattern's own trailing text
// still parses, and the anchor is the end of the line, so the final "};" is the
// declaration's.
var pooledLiteralPattern = regexp.MustCompile(`^static const ktn_string (ktn_literal_[0-9]+) = \{ ([0-9]+), (.*) \};$`)

// pooledLiterals returns the pooled literals of generated C, in pool order.
func pooledLiterals(generated string) []pooledLiteral {
	var found []pooledLiteral
	for _, line := range strings.Split(generated, "\n") {
		match := pooledLiteralPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		found = append(found, pooledLiteral{name: match[1], length: match[2], escaped: match[3]})
	}
	return found
}

// referenceLiteral returns the reference generator's pooled entry for one Kitten
// string literal, by generating C for a program whose only literal it is. The
// program is small enough that the literal is the first and only one in the pool,
// so the entry cannot be confused with another literal's.
func referenceLiteral(t *testing.T, literal string) pooledLiteral {
	t.Helper()

	generated := referenceC(t, "fn main() {\n    print("+literal+");\n}\n")
	pool := pooledLiterals(generated)
	if len(pool) != 1 {
		t.Fatalf("expected exactly one pooled literal for %q, found %d", literal, len(pool))
	}
	if pool[0].name != "ktn_literal_1" {
		t.Fatalf("expected the first pooled literal for %q, found %q", literal, pool[0].name)
	}
	return pool[0]
}

// --- Probe output ------------------------------------------------------------

// probeLines splits probe output into its lines, dropping the final empty one.
func probeLines(output string) []string {
	return strings.Split(strings.TrimSuffix(output, "\n"), "\n")
}

// expectedProbeOutput renders the lines a probe run should print. A non-empty
// string is one complete line, so a test can state the whole expected output.
func expectedProbeOutput(lines []string) string {
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return builder.String()
}

// --- Name mangling -----------------------------------------------------------

// codegenNameCase is one program plus the name directives asked of the probe.
type codegenNameCase struct {
	description string
	root        string
	imported    string

	// directives is the probe input; want is the expected output, one line per
	// directive. The last field of every want line is a generated C name, which
	// must also appear in the reference C for the program.
	directives string
	want       []string
}

func codegenNameCases() []codegenNameCase {
	return []codegenNameCase{{
		description: "single module covers every name family",
		root: `struct Point {
    int x;
    string label;
}
enum Color { Red, Green }
union Value {
    Number { int n; }
    Text { string s; }
}
fn helper() -> int {
    return 1;
}
fn main() {
    Point p = Point { x = 1, label = "hi" };
    Color c = Color.Red;
    Value v = Value.Number { n = helper() };
    print(p.x, p.label, helper());
    match (c) {
        Red { print(1); }
        Green { print(2); }
    }
    match (v) {
        Number(n) { print(n); }
        Text(s) { print(s); }
    }
}
`,
		directives: `module-prefix 0
function 0 main
function 0 helper
variable p
struct 0 Point
field x
field label
constructor 0 Point
argument x
enum 0 Color
enum-case 0 Color Red
enum-case 0 Color Green
union-tag 0 Value Number
variant-field Number n
variant-constructor 0 Value Number
match-temporary 0
literal-name 1
object
tag-field
`,
		want: []string{
			"module-prefix 0 -",
			"function 0 main ktn_fn_main",
			"function 0 helper ktn_fn_helper",
			"variable p ktn_var_p",
			"struct 0 Point ktn_s_Point",
			"field x ktn_f_x",
			"field label ktn_f_label",
			"constructor 0 Point ktn_new_Point",
			"argument x ktn_arg_x",
			"enum 0 Color ktn_e_Color",
			"enum-case 0 Color Red ktn_ec_Color_Red",
			"enum-case 0 Color Green ktn_ec_Color_Green",
			"union-tag 0 Value Number ktn_uc_Value_Number",
			"variant-field Number n ktn_v_Number_n",
			"variant-constructor 0 Value Number ktn_new_Value_Number",
			"match-temporary 0 ktn_match_0",
			"literal-name 1 ktn_literal_1",
			"object ktn_object",
			"tag-field ktn_tag",
		},
	}, {
		description: "imported module exists",
		root: `fn main() {
    Point p = make();
    print(p.x, other());
}
`,
		imported: `public struct Point {
    int x;
}
public fn make() -> Point {
    return Point { x = 1 };
}
public fn other() -> int {
    return 2;
}
`,
		directives: `module-prefix 1
function 0 main
function 1 make
function 1 other
struct 1 Point
constructor 1 Point
field x
`,
		want: []string{
			"module-prefix 1 1_",
			"function 0 main ktn_fn_main",
			"function 1 make ktn_fn_1_make",
			"function 1 other ktn_fn_1_other",
			"struct 1 Point ktn_s_1_Point",
			"constructor 1 Point ktn_new_1_Point",
			"field x ktn_f_x",
		},
	}, {
		description: "same name in two modules stays distinct",
		root: `struct Item {
    int a;
}
fn main() {
    Item item = Item { a = 1 };
    print(item.a);
}
`,
		imported: `struct Item {
    int b;
}
fn unused() -> int {
    Item item = Item { b = 1 };
    return item.b;
}
`,
		directives: `struct 0 Item
struct 1 Item
constructor 0 Item
constructor 1 Item
`,
		want: []string{
			"struct 0 Item ktn_s_Item",
			"struct 1 Item ktn_s_1_Item",
			"constructor 0 Item ktn_new_Item",
			"constructor 1 Item ktn_new_1_Item",
		},
	}}
}

// TestKittenCodegenNamesMatchReference compares the probe's mangled names with the
// reference generator's, and requires every predicted name to appear in the
// reference C for the program that uses it.
func TestKittenCodegenNamesMatchReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenCodegen(t)
	directory := t.TempDir()
	anchored := 0

	for _, testCase := range codegenNameCases() {
		want := expectedProbeOutput(testCase.want)
		got, err := runKittenCodegen(executable, directory, testCase.directives)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if got != want {
			t.Errorf("%s: probe output differs\n got:\n%s\nwant:\n%s", testCase.description, got, want)
			continue
		}

		var generated string
		if testCase.imported == "" {
			generated = referenceC(t, testCase.root)
		} else {
			generated = referenceC(t, testCase.root, testCase.imported)
		}
		for _, line := range testCase.want {
			fields := strings.Fields(line)
			name := fields[len(fields)-1]
			if name == "-" {
				continue
			}
			if !strings.Contains(generated, name) {
				t.Errorf("%s: the reference generator's C does not contain %q, so the prediction is not anchored", testCase.description, name)
			}
			anchored++
		}
	}

	if anchored < 25 {
		t.Fatalf("the name corpus anchored only %d names", anchored)
	}
	t.Logf("name mangling: %d predictions anchored to the reference generator's C", anchored)
}

// --- C types -----------------------------------------------------------------

// codegenTypeCase is one program plus the type directives asked of the probe.
type codegenTypeCase struct {
	description string

	// root is a program that declares one local of each type asked about, so that
	// the reference C spells the type in a declaration. imported is a second
	// module for the cases that need one.
	root     string
	imported string

	directives string

	// want is the expected output, one line per directive, and anchors are
	// declarations or spellings that must appear in the reference C. A case with
	// no anchor has no C spelling to look for.
	want    []string
	anchors []string
}

func codegenTypeCases() []codegenTypeCase {
	return []codegenTypeCase{{
		description: "primitive types",
		root: `fn main() {
    int i = 1;
    float f = 1.5;
    bool b = true;
    string s = "x";
    print(i, f, b, s);
}
`,
		directives: "ctype int\nctype float\nctype bool\nctype string\n",
		want: []string{
			"ctype int int64_t",
			"ctype float double",
			"ctype bool bool",
			"ctype string const ktn_string *",
		},
		anchors: []string{
			"int64_t ktn_var_i",
			"double ktn_var_f",
			"bool ktn_var_b",
			"const ktn_string * ktn_var_s",
		},
	}, {
		description: "void return and no parameters",
		root: `fn nothing() {
}
fn main() {
    nothing();
}
`,
		directives: "ctype void\n",
		want:       []string{"ctype void void"},
		anchors:    []string{"void ktn_fn_nothing(void)"},
	}, {
		description: "collections",
		root: `fn main() {
    int[] values = new int[1];
    string[] names = new string[1];
    int[][] grid = new int[][1];
    map[string]int counts = new map[string]int;
    print(length(values), length(names), length(grid), length(counts));
}
`,
		directives: "ctype array\nctype map\n",
		want: []string{
			"ctype array ktn_array *",
			"ctype map ktn_map *",
		},
		anchors: []string{
			"ktn_array * ktn_var_values",
			"ktn_array * ktn_var_names",
			"ktn_array * ktn_var_grid",
			"ktn_map * ktn_var_counts",
		},
	}, {
		description: "named types",
		root: `enum Color { Red }
struct Point {
    int x;
}
union Value {
    Empty
}
fn main() {
    Color c = Color.Red;
    Point p = Point { x = 1 };
    Value v = Value.Empty;
    print(c == Color.Red, p.x);
    match (v) {
        Empty { print(1); }
    }
}
`,
		directives: "ctype-enum 0 Color\nctype-struct 0 Point\nctype-union 0 Value\n",
		want: []string{
			"ctype-enum 0 Color ktn_e_Color",
			"ctype-struct 0 Point ktn_s_Point *",
			"ctype-union 0 Value ktn_s_Value *",
		},
		anchors: []string{
			"ktn_e_Color ktn_var_c",
			"ktn_s_Point * ktn_var_p",
			"ktn_s_Value * ktn_var_v",
		},
	}, {
		description: "named types from an imported module",
		root: `fn main() {
    Point p = make();
    print(p.x);
}
`,
		imported: `public enum Color { Red }
public struct Point {
    int x;
}
public union Value {
    Empty
}
public fn make() -> Point {
    return Point { x = 1 };
}
`,
		directives: "ctype-enum 1 Color\nctype-struct 1 Point\nctype-union 1 Value\n",
		want: []string{
			"ctype-enum 1 Color ktn_e_1_Color",
			"ctype-struct 1 Point ktn_s_1_Point *",
			"ctype-union 1 Value ktn_s_1_Value *",
		},
		anchors: []string{
			"typedef int64_t ktn_e_1_Color;",
			"ktn_s_1_Point *",
			"struct ktn_s_1_Value {",
		},
	}, {
		description: "the absent type has no C representation",
		directives:  "ctype-none\n",
		want:        []string{`ctype-none failed unsupported type "<invalid>"`},
	}}
}

// TestKittenCodegenTypesMatchReference compares the probe's C type mapping with
// the reference generator's spelling, and requires the complex spellings to
// appear in reference C.
func TestKittenCodegenTypesMatchReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenCodegen(t)
	directory := t.TempDir()
	anchored := 0

	for _, testCase := range codegenTypeCases() {
		want := expectedProbeOutput(testCase.want)
		got, err := runKittenCodegen(executable, directory, testCase.directives)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if got != want {
			t.Errorf("%s: probe output differs\n got:\n%s\nwant:\n%s", testCase.description, got, want)
			continue
		}

		if len(testCase.anchors) == 0 {
			continue
		}
		var generated string
		if testCase.imported != "" {
			generated = referenceC(t, testCase.root, testCase.imported)
		} else {
			generated = referenceC(t, testCase.root)
		}
		for _, anchor := range testCase.anchors {
			if !strings.Contains(generated, anchor) {
				t.Errorf("%s: the reference generator's C does not contain %q", testCase.description, anchor)
			}
			anchored++
		}
	}

	if anchored < 10 {
		t.Fatalf("the type corpus anchored only %d spellings", anchored)
	}
	t.Logf("C types: %d spellings anchored to the reference generator's C", anchored)
}

// --- String literal escaping -------------------------------------------------

// curatedEscapeLiterals exercise every branch of the C re-encoding: the escapes
// the language defines, the question mark, printable ASCII, control bytes, and
// bytes at or above 128. The last group is written as raw bytes, because the
// language defines no escape that produces one.
func curatedEscapeLiterals() []string {
	return []string{
		`""`,
		`"a"`,
		`"hello world"`,
		`"\"quoted\""`,
		`"back\\slash"`,
		`"tab\there"`,
		`"newline\nhere"`,
		`"question?"`,
		`"??!"`,
		`"}"`,
		`"\\"`,
		`"a\nb\tc\\d\"e?f"`,
		"\"\x01\"",
		"\"\x0d\x7f\"",
		"\"café\"",
		"\"\u00ff\u4e2d\"",
	}
}

// corpusLiterals returns every distinct string literal of the corpus inputs that
// lex, in the order it is first seen.
//
// A source whose bytes are not valid UTF-8 is skipped, and so is a literal whose
// bytes are not. The reference lexer scans runes, so an invalid byte is replaced
// before a literal is spelled out, which is the lexer's documented byte-oriented
// difference rather than a difference in escaping, and it belongs to the lexer
// harness rather than here.
func corpusLiterals(t *testing.T) []string {
	t.Helper()

	seen := make(map[string]bool)
	var literals []string
	add := func(source string) {
		if !utf8.ValidString(source) {
			return
		}
		tokens, err := lexer.Lex(source)
		if err != nil {
			return
		}
		for _, token := range tokens {
			if token.Kind != lexer.String || !utf8.ValidString(token.Text) {
				continue
			}
			if seen[token.Text] {
				continue
			}
			seen[token.Text] = true
			literals = append(literals, token.Text)
		}
	}
	for _, testCase := range checkerCorpus(t) {
		add(testCase.root)
		add(testCase.imported)
	}
	return literals
}

// TestKittenCodegenEscapingMatchesReference compares the probe's escaping and
// byte lengths with the pooled literal declarations of the reference generator,
// for every literal in the curated set and the corpus.
func TestKittenCodegenEscapingMatchesReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenCodegen(t)
	directory := t.TempDir()

	literals := curatedEscapeLiterals()
	literals = append(literals, corpusLiterals(t)...)
	t.Logf("escaping corpus: %d literals", len(literals))

	var directives strings.Builder
	for _, literal := range literals {
		directives.WriteString("escape " + literal + "\n")
	}
	got, err := runKittenCodegen(executable, directory, directives.String())
	if err != nil {
		t.Fatalf("%v", err)
	}

	lines := probeLines(got)
	if len(lines) != len(literals) {
		t.Fatalf("the probe printed %d lines for %d literals", len(lines), len(literals))
	}

	checked := 0
	for index, literal := range literals {
		want := referenceLiteral(t, literal)
		expected := "escape " + want.escaped + " " + want.length
		if lines[index] != expected {
			t.Errorf("escaping %q: the probe printed %q, the reference generator pooled %q with length %s",
				literal, lines[index], want.escaped, want.length)
			continue
		}
		checked++
	}

	if checked < 20 {
		t.Fatalf("only %d literals were compared", checked)
	}
	t.Logf("string escaping: %d literals match the reference generator's pooled literals", checked)
}

// TestKittenCodegenLiteralPoolOrder checks that equal literals share one pooled
// object and that the pool is numbered in first-use order, which the escaping
// comparison cannot see because it compares one literal at a time.
func TestKittenCodegenLiteralPoolOrder(t *testing.T) {
	requireToolchain(t)

	generated := referenceC(t, "fn main() {\n    print(\"second\", \"first\", \"second\");\n}\n")
	pool := pooledLiterals(generated)
	if len(pool) != 2 {
		t.Fatalf("expected two pooled literals, found %d", len(pool))
	}
	if pool[0].name != "ktn_literal_1" || pool[0].escaped != "\"second\"" {
		t.Errorf("the first pooled literal is %q with text %s, want ktn_literal_1 with \"second\"", pool[0].name, pool[0].escaped)
	}
	if pool[1].name != "ktn_literal_2" || pool[1].escaped != "\"first\"" {
		t.Errorf("the second pooled literal is %q with text %s, want ktn_literal_2 with \"first\"", pool[1].name, pool[1].escaped)
	}
}

// --- Output accumulation -----------------------------------------------------

// accumulatedLine is the line the probe accumulates at one index: its nesting
// level cycles through four levels, so indentation is exercised too.
func accumulatedLine(index int) string {
	return strings.Repeat("    ", index%4) + "line " + strconv.Itoa(index) + "\n"
}

// accumulatedText is the text the probe accumulates for a run of lines.
func accumulatedText(lines int) string {
	var builder strings.Builder
	for index := 0; index < lines; index++ {
		builder.WriteString(accumulatedLine(index))
	}
	return builder.String()
}

// codegenOutputCase is one accumulation directive and its complete expected
// output. The probe prints a summary line and then the accumulated text, which
// print terminates with a newline, so the expected output is stated in full.
type codegenOutputCase struct {
	description string
	directives  string
	want        string
}

func codegenOutputCases() []codegenOutputCase {
	const fragments = "alpha beta gamma\n    delta\ntail"
	return []codegenOutputCase{{
		description: "no lines",
		directives:  "output 4 0\n",
		want:        "output 4 0 ok 0\n\n",
	}, {
		description: "three lines at four levels",
		directives:  "output 4 3\n",
		want:        "output 4 3 ok 3\n" + accumulatedText(3) + "\n",
	}, {
		description: "exactly capacity many lines",
		directives:  "output 300 300\n",
		want:        "output 300 300 ok 300\n" + accumulatedText(300) + "\n",
	}, {
		description: "one line more than capacity",
		directives:  "output 299 300\n",
		want:        "output 299 300 overflow 299\n" + accumulatedText(299) + "\n",
	}, {
		description: "overflow rejects the whole line",
		directives:  "output 2 3\n",
		want:        "output 2 3 overflow 2\n" + accumulatedText(2) + "\n",
	}, {
		description: "fragments, indentation, and an unterminated tail",
		directives:  "fragments 4\n",
		want:        "fragments 4 0 ok 2\n" + fragments + "\n",
	}, {
		description: "a fragment overflow leaves accepted text intact",
		directives:  "fragments 1\n",
		want:        "fragments 1 0 overflow 1\nalpha beta gamma\n    \n",
	}}
}

// TestKittenCodegenOutputAccumulation pins the accumulation and finalization
// behavior: the exact text for empty, small, and many-chunk runs, the chunk
// boundary, the rejection of a line that does not fit, and a partial line that
// never ends.
func TestKittenCodegenOutputAccumulation(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenCodegen(t)
	directory := t.TempDir()

	chunks := 0
	for _, testCase := range codegenOutputCases() {
		got, err := runKittenCodegen(executable, directory, testCase.directives)
		if err != nil {
			t.Errorf("%s: %v", testCase.description, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("%s: accumulated output differs\n got: %q\nwant: %q", testCase.description, got, testCase.want)
			continue
		}
		chunks++
	}

	if chunks < 5 {
		t.Fatalf("only %d accumulation cases ran", chunks)
	}
}

// TestKittenCodegenIgnoresBlankAndCommentLines checks that the directive reader
// skips what it is documented to skip and still reads every directive.
func TestKittenCodegenIgnoresBlankAndCommentLines(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenCodegen(t)
	directory := t.TempDir()

	got, err := runKittenCodegen(executable, directory, "# a comment\n\nfield x\n\n# another\nobject\n")
	if err != nil {
		t.Fatalf("%v", err)
	}
	want := "field x ktn_f_x\nobject ktn_object\n"
	if got != want {
		t.Errorf("probe output differs\n got: %q\nwant: %q", got, want)
	}
}

// --- Probe format and determinism --------------------------------------------

// TestKittenCodegenProbeFormat pins the directive interface and the shape of
// every result line with hand-written expectations, so that a format change is
// caught even if the harness's tables changed with it.
func TestKittenCodegenProbeFormat(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenCodegen(t)
	directory := t.TempDir()

	directives := strings.Join([]string{
		"module-prefix 0",
		"module-prefix 3",
		"function 3 main",
		"variable x",
		"struct 3 Point",
		"field x",
		"constructor 3 Point",
		"argument x",
		"enum 3 Color",
		"enum-case 3 Color Red",
		"union-tag 3 Value Number",
		"variant-field Number n",
		"variant-constructor 3 Value Number",
		"match-temporary 1",
		"literal-name 2",
		"object",
		"tag-field",
		`escape "a\tb"`,
		"ctype int",
		"ctype string",
		"ctype map",
		"ctype-enum 3 Color",
		"ctype-struct 3 Point",
		"ctype-union 3 Value",
		"ctype-none",
		"ctype nonsense",
		"output 1 1",
		"nonsense-directive",
	}, "\n") + "\n"

	got, err := runKittenCodegen(executable, directory, directives)
	if err != nil {
		t.Fatalf("%v", err)
	}

	want := expectedProbeOutput([]string{
		"module-prefix 0 -",
		"module-prefix 3 3_",
		"function 3 main ktn_fn_3_main",
		"variable x ktn_var_x",
		"struct 3 Point ktn_s_3_Point",
		"field x ktn_f_x",
		"constructor 3 Point ktn_new_3_Point",
		"argument x ktn_arg_x",
		"enum 3 Color ktn_e_3_Color",
		"enum-case 3 Color Red ktn_ec_3_Color_Red",
		"union-tag 3 Value Number ktn_uc_3_Value_Number",
		"variant-field Number n ktn_v_Number_n",
		"variant-constructor 3 Value Number ktn_new_3_Value_Number",
		"match-temporary 1 ktn_match_1",
		"literal-name 2 ktn_literal_2",
		"object ktn_object",
		"tag-field ktn_tag",
		`escape "a\tb" 3`,
		"ctype int int64_t",
		"ctype string const ktn_string *",
		"ctype map ktn_map *",
		"ctype-enum 3 Color ktn_e_3_Color",
		"ctype-struct 3 Point ktn_s_3_Point *",
		"ctype-union 3 Value ktn_s_3_Value *",
		`ctype-none failed unsupported type "<invalid>"`,
		`ctype nonsense failed unknown ctype kind "nonsense"`,
		"output 1 1 ok 1",
		"line 0",
		"",
		"unknown nonsense-directive",
	})
	if got != want {
		t.Errorf("probe output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestKittenCodegenAccumulationScaling checks that accumulating and finalizing a
// large output costs time proportional to its size.
//
// Chunks exist so that appending stays linear, and the final join is balanced so
// that concatenating the chunks stays near-linear too. Joining the chunks left to
// right instead would copy the growing result once per chunk, which is quadratic:
// measured on a 2.5 GHz x86-64 host, 32,000 accumulated lines took 10.2 s and
// 128,000 took 211.6 s, against 0.07 s and 0.31 s joined in halves.
func TestKittenCodegenAccumulationScaling(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenCodegen(t)
	directory := t.TempDir()

	previous := time.Duration(0)
	first := time.Duration(0)
	last := time.Duration(0)
	for _, lines := range []int{2000, 8000, 32000} {
		directives := fmt.Sprintf("output %d %d\n", lines, lines)

		best := time.Duration(0)
		var output string
		for repetition := 0; repetition < 3; repetition++ {
			start := time.Now()
			got, err := runKittenCodegen(executable, directory, directives)
			if err != nil {
				t.Fatalf("lines %d: %v", lines, err)
			}
			if elapsed := time.Since(start); best == 0 || elapsed < best {
				best = elapsed
			}
			output = got
		}

		if want := "output " + strconv.Itoa(lines) + " " + strconv.Itoa(lines) + " ok " + strconv.Itoa(lines) + "\n" + accumulatedText(lines) + "\n"; output != want {
			t.Fatalf("lines %d: the accumulated text differs", lines)
		}

		growth := ""
		if previous > 0 {
			growth = fmt.Sprintf(", x%.1f", float64(best)/float64(previous))
		}
		t.Logf("lines=%6d bytes=%8d  %10s%s", lines, len(output), best.Round(time.Millisecond), growth)
		previous = best
		if first == 0 {
			first = best
		}
		last = best
	}

	// The last input is sixteen times the first. Linear accumulation costs about
	// sixteen times as much, and quadratic accumulation costs about 256 times as
	// much, so a bound well below that separates the two even on a loaded host.
	if last > first*40 {
		t.Errorf("accumulating sixteen times the input cost %v, against %v for the smaller input; the cost is not linear", last, first)
	}
}

// TestKittenCodegenIsDeterministic checks that the probe's output depends only on
// its input: the same directives must produce the same bytes, in any order of
// runs and after other inputs.
func TestKittenCodegenIsDeterministic(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenCodegen(t)
	directory := t.TempDir()

	directives := "function 0 main\nstruct 2 Point\nenum-case 2 Color Red\nctype-struct 2 Point\noutput 4 3\nfragments 2\n"
	first, err := runKittenCodegen(executable, directory, directives)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := runKittenCodegen(executable, directory, "escape \"other\"\n"); err != nil {
		t.Fatalf("%v", err)
	}
	second, err := runKittenCodegen(executable, directory, directives)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if first != second {
		t.Errorf("the probe's output depends on the previous run\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
