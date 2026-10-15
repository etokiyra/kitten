package codegen

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/lexer"
	"kitten/parser"
)

// generateSource runs a source program through the frontend and code generator.
func generateSource(t *testing.T, source string) string {
	t.Helper()

	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("lexing: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	checked, err := checker.Check(program)
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	generated, err := Generate([]*parser.Module{{ID: 0, Program: program}}, checked)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	return generated
}

func assertContains(t *testing.T, generated, substring string) {
	t.Helper()
	if !strings.Contains(generated, substring) {
		t.Errorf("generated C does not contain %q\n--- generated ---\n%s", substring, generated)
	}
}

func TestGeneratedIncludesRuntimeHeader(t *testing.T) {
	generated := generateSource(t, "fn main() {\n}\n")
	assertContains(t, generated, `#include "kitten_runtime.h"`)
	assertContains(t, generated, "int main(void) {")
	assertContains(t, generated, "ktn_rt_init();")
	assertContains(t, generated, "ktn_fn_main();")
}

func TestTypeMapping(t *testing.T) {
	generated := generateSource(t, `struct Point {
    int x;
}
fn main() {
    int a = 1;
    float b = 2.5;
    bool c = true;
    string d = "s";
    Point p = Point { x = 1 };
    print(a, b, c, d);
}
`)
	assertContains(t, generated, "int64_t ktn_var_a")
	assertContains(t, generated, "double ktn_var_b")
	assertContains(t, generated, "bool ktn_var_c")
	assertContains(t, generated, "const char * ktn_var_d")
	assertContains(t, generated, "ktn_s_Point * ktn_var_p")
	assertContains(t, generated, "struct ktn_s_Point {")
	assertContains(t, generated, "int64_t ktn_f_x;")
}

func TestIdentifierMappingAvoidsCKeywords(t *testing.T) {
	generated := generateSource(t, `struct Widget {
    int case;
}
fn main() {
    int double = 1;
    int for_in_c = 2;
    Widget w = Widget { case = double + for_in_c };
    print(w.case);
}
`)
	// Kitten identifiers that are C keywords must not leak into C.
	assertContains(t, generated, "int64_t ktn_var_double")
	assertContains(t, generated, "int64_t ktn_var_for_in_c")
	assertContains(t, generated, "ktn_f_case")
	assertContains(t, generated, "->ktn_f_case")
	if strings.Contains(generated, "->case") {
		t.Errorf("generated C used an unmapped field name\n%s", generated)
	}
}

func TestFunctionAndMainNamesDoNotCollide(t *testing.T) {
	generated := generateSource(t, "fn main() {\n}\n")
	// The Kitten main function and the C entry point are distinct symbols.
	assertContains(t, generated, "void ktn_fn_main(void);")
	assertContains(t, generated, "void ktn_fn_main(void) {")
	assertContains(t, generated, "int main(void) {")
}

func TestPrintExpansion(t *testing.T) {
	generated := generateSource(t, `fn main() {
    print("text", 1, 2.5, true);
}
`)
	assertContains(t, generated, `ktn_rt_print_string("text")`)
	assertContains(t, generated, "ktn_rt_print_space()")
	assertContains(t, generated, "ktn_rt_print_int(1)")
	assertContains(t, generated, "ktn_rt_print_float(2.5)")
	assertContains(t, generated, "ktn_rt_print_bool(true)")
	assertContains(t, generated, "ktn_rt_print_newline()")
}

func TestStringEqualityUsesRuntimeHelper(t *testing.T) {
	generated := generateSource(t, `fn main() {
    bool same = "a" == "b";
    bool different = "a" != "b";
    print(same, different);
}
`)
	assertContains(t, generated, `ktn_rt_string_equal("a", "b")`)
	assertContains(t, generated, `!ktn_rt_string_equal("a", "b")`)
}

func TestStringConcatenationUsesRuntimeHelper(t *testing.T) {
	generated := generateSource(t, `fn main() {
    string literal = "a" + "b";
    string first = "x";
    string second = "y";
    string variables = first + second;
    string mixed = first + "!";
    print(literal, variables, mixed);
}
`)
	assertContains(t, generated, `ktn_rt_string_concat("a", "b")`)
	assertContains(t, generated, "ktn_rt_string_concat(ktn_var_first, ktn_var_second)")
	assertContains(t, generated, `ktn_rt_string_concat(ktn_var_first, "!")`)
}

func TestStringCompoundAssignmentLowersToAppend(t *testing.T) {
	generated := generateSource(t, `fn main() {
    string text = "a";
    text += "b";
}
`)
	assertContains(t, generated, `ktn_rt_string_append(&(ktn_var_text), "b")`)
}

func TestStringTypeMappingIsConstPointer(t *testing.T) {
	generated := generateSource(t, `fn main() {
    string text = "a";
}
`)
	assertContains(t, generated, "const char * ktn_var_text")
}

func TestArrayCreationAndIndexing(t *testing.T) {
	generated := generateSource(t, `fn main() {
    int[] values = new int[3];
    values[0] = 1;
    values[1] += 2;
    int first = values[0];
    print(length(values), first);
}
`)
	assertContains(t, generated, "ktn_rt_array_new(3, sizeof(int64_t))")
	assertContains(t, generated, "(*((int64_t *)ktn_rt_array_at(ktn_var_values, 0))) = 1")
	assertContains(t, generated, "(*((int64_t *)ktn_rt_array_at(ktn_var_values, 1))) += 2")
	assertContains(t, generated, "ktn_rt_array_length(ktn_var_values)")
}

func TestArrayTypeMapping(t *testing.T) {
	generated := generateSource(t, `fn identity(int[] values) -> int[] {
    return values;
}
fn main() {
    int[] items = new int[2];
    print(length(identity(items)));
}
`)
	assertContains(t, generated, "ktn_array * ktn_fn_identity(ktn_array * ktn_var_values)")
	assertContains(t, generated, "ktn_array * ktn_var_items = ktn_rt_array_new(2, sizeof(int64_t))")
}

func TestNestedArrayTypeMapping(t *testing.T) {
	generated := generateSource(t, `fn main() {
    int[][] grid = new int[][2];
    grid[0] = new int[3];
    grid[0][0] = 7;
}
`)
	assertContains(t, generated, "ktn_rt_array_new(2, sizeof(ktn_array *))")
	assertContains(t, generated, "ktn_rt_array_new(3, sizeof(int64_t))")
	assertContains(t, generated, "(*((int64_t *)ktn_rt_array_at((*((ktn_array * *)ktn_rt_array_at(ktn_var_grid, 0))), 0))) = 7")
}

func TestStringArrayAppendTakesSlotAddress(t *testing.T) {
	generated := generateSource(t, `fn main() {
    string[] words = new string[1];
    words[0] += "x";
}
`)
	// The append receives the address of the element slot, so the index is
	// evaluated once.
	assertContains(t, generated, `ktn_rt_string_append(&((*((const char * *)ktn_rt_array_at(ktn_var_words, 0)))), "x")`)
}

func TestForwardReferencesAreEmittedBeforeUse(t *testing.T) {
	generated := generateSource(t, `fn main() {
    print(later());
}
fn later() -> int {
    return helper();
}
fn helper() -> int {
    return 1;
}
`)
	prototype := strings.Index(generated, "int64_t ktn_fn_later(void);")
	definition := strings.Index(generated, "int64_t ktn_fn_later(void) {")
	if prototype < 0 || definition < 0 {
		t.Fatalf("expected prototype and definition of later\n%s", generated)
	}
	if prototype > definition {
		t.Errorf("prototype of later appears after its definition\n%s", generated)
	}
	assertContains(t, generated, "int64_t ktn_fn_helper(void) {")
}

func TestForwardStructReferences(t *testing.T) {
	generated := generateSource(t, `struct Line {
    Point start;
    Point end;
}
struct Point {
    int x;
}
fn main() {
    Point p = Point { x = 1 };
    Line line = Line { start = p, end = p };
    print(line.end.x);
}
`)
	// The typedef for Point must precede the body of Line.
	pointTypedef := strings.Index(generated, "typedef struct ktn_s_Point ktn_s_Point;")
	lineBody := strings.Index(generated, "struct ktn_s_Line {")
	if pointTypedef < 0 || lineBody < 0 {
		t.Fatalf("expected Point typedef and Line body\n%s", generated)
	}
	if pointTypedef > lineBody {
		t.Errorf("Point typedef appears after the Line body\n%s", generated)
	}
}

func TestStructLiteralUsesDeclarationOrder(t *testing.T) {
	generated := generateSource(t, `struct Pair {
    int first;
    int second;
}
fn main() {
    Pair pair = Pair { second = 2, first = 1 };
    print(pair.first, pair.second);
}
`)
	// Values are reordered into declaration order: first, then second.
	assertContains(t, generated, "ktn_new_Pair(1, 2)")
}

func TestControlFlowEmission(t *testing.T) {
	generated := generateSource(t, `fn main() {
    int total = 0;
    if (total > 0) {
        total = 1;
    } else {
        total = 2;
    }
    while (total != 5) {
        total += 1;
    }
    for (int index = 0; index < 3; index++) {
        total = index;
    }
}
`)
	assertContains(t, generated, "if ((ktn_var_total > 0)) {")
	assertContains(t, generated, "else {")
	assertContains(t, generated, "while ((ktn_var_total != 5)) {")
	assertContains(t, generated, "for (int64_t ktn_var_index = 0; (ktn_var_index < 3); (ktn_var_index++)) {")
}

func TestCompoundAssignmentAndIncrement(t *testing.T) {
	generated := generateSource(t, `fn main() {
    int value = 1;
    value += 2;
    value -= 1;
    value++;
    --value;
}
`)
	assertContains(t, generated, "(ktn_var_value += 2)")
	assertContains(t, generated, "(ktn_var_value -= 1)")
	assertContains(t, generated, "(ktn_var_value++)")
	assertContains(t, generated, "(--ktn_var_value)")
}

func TestEmptyBlocksAndVoidFunctions(t *testing.T) {
	generated := generateSource(t, `fn noop() {
}
fn main() {
    noop();
}
`)
	assertContains(t, generated, "void ktn_fn_noop(void) {")
	assertContains(t, generated, "ktn_fn_noop();")
}

func TestGenerationIsDeterministic(t *testing.T) {
	source := `struct Point {
    int x;
    int y;
}
fn distance_squared(Point a, Point b) -> int {
    int dx = a.x - b.x;
    int dy = a.y - b.y;
    return dx * dx + dy * dy;
}
fn main() {
    Point origin = Point { x = 0, y = 0 };
    Point p = Point { x = 3, y = 4 };
    print(distance_squared(origin, p));
}
`
	first := generateSource(t, source)
	second := generateSource(t, source)
	if first != second {
		t.Errorf("generation is not deterministic")
	}
}

func TestEntryPointErrors(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		contains string
	}{
		{
			name:     "missing main",
			source:   "fn helper() {\n}\n",
			contains: "no entry point",
		},
		{
			name:     "main with parameters",
			source:   "fn main(int argument) {\n}\n",
			contains: "no parameters",
		},
		{
			name:     "main returning a value",
			source:   "fn main() -> int {\n    return 1;\n}\n",
			contains: "must not return a value",
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			tokens, err := lexer.Lex(testCase.source)
			if err != nil {
				t.Fatalf("lexing: %v", err)
			}
			program, err := parser.Parse(tokens)
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			checked, err := checker.Check(program)
			if err != nil {
				t.Fatalf("checking: %v", err)
			}
			_, err = Generate([]*parser.Module{{ID: 0, Program: program}}, checked)
			if err == nil {
				t.Fatalf("Generate succeeded, want an error")
			}
			var generationError *Error
			if !errors.As(err, &generationError) {
				t.Fatalf("error is %T (%v), want *codegen.Error", err, err)
			}
			if !strings.Contains(generationError.Message, testCase.contains) {
				t.Errorf("message %q does not contain %q", generationError.Message, testCase.contains)
			}
		})
	}
}

func TestGenerateRejectsNilInputs(t *testing.T) {
	if _, err := Generate(nil, nil); err == nil {
		t.Errorf("Generate(nil, nil) succeeded, want an error")
	}
}

// emptyResult builds a checker result with no declarations, for hand-built
// malformed syntax trees.
func emptyResult() *checker.Result {
	return &checker.Result{
		Functions:       map[string]*checker.Function{},
		StructTypes:     map[string]*checker.Type{},
		FunctionDecls:   map[*parser.FunctionDecl]*checker.Function{},
		StructDecls:     map[*parser.StructDecl]*checker.Type{},
		ExpressionTypes: map[parser.Expr]*checker.Type{},
		TypeRefs:        map[*parser.TypeRef]*checker.Type{},
		Symbols:         map[*parser.Identifier]*checker.Symbol{},
	}
}

// resultForMain builds a checker result that registers the declarations of a
// hand-built program, so code generation proceeds far enough to reach the
// malformed syntax the test is exercising.
func resultForMain(program *parser.Program) *checker.Result {
	result := emptyResult()
	for _, declaration := range program.Declarations {
		switch d := declaration.(type) {
		case *parser.FunctionDecl:
			result.FunctionDecls[d] = &checker.Function{Name: d.Name, ReturnType: checker.VoidType}
		case *parser.StructDecl:
			result.StructDecls[d] = &checker.Type{
				Kind:   checker.KindStruct,
				Name:   d.Name,
				Struct: &checker.StructInfo{Name: d.Name, FieldIndex: map[string]*checker.StructField{}},
			}
		}
	}
	return result
}

func TestGenerateRejectsMalformedTrees(t *testing.T) {
	cases := []struct {
		name    string
		program *parser.Program
		checked *checker.Result
	}{
		{
			name: "function without body",
			program: &parser.Program{Declarations: []parser.Decl{
				&parser.FunctionDecl{At: parser.Position{Line: 1, Column: 1}, Name: "main"},
			}},
			checked: nil,
		},
		{
			name: "variable declaration without type",
			program: &parser.Program{Declarations: []parser.Decl{
				&parser.FunctionDecl{
					At:   parser.Position{Line: 1, Column: 1},
					Name: "main",
					Body: &parser.BlockStmt{
						At: parser.Position{Line: 1, Column: 11},
						Statements: []parser.Stmt{
							&parser.VarDeclStmt{At: parser.Position{Line: 2, Column: 5}, Name: "x"},
						},
					},
				},
			}},
			checked: nil,
		},
		{
			name: "expression statement without expression",
			program: &parser.Program{Declarations: []parser.Decl{
				&parser.FunctionDecl{
					At:   parser.Position{Line: 1, Column: 1},
					Name: "main",
					Body: &parser.BlockStmt{
						At:         parser.Position{Line: 1, Column: 11},
						Statements: []parser.Stmt{&parser.ExprStmt{At: parser.Position{Line: 2, Column: 5}}},
					},
				},
			}},
			checked: nil,
		},
		{
			name: "struct literal without type",
			program: &parser.Program{Declarations: []parser.Decl{
				&parser.FunctionDecl{
					At:   parser.Position{Line: 1, Column: 1},
					Name: "main",
					Body: &parser.BlockStmt{
						At: parser.Position{Line: 1, Column: 11},
						Statements: []parser.Stmt{
							&parser.ExprStmt{
								At: parser.Position{Line: 2, Column: 5},
								Expression: &parser.StructLiteral{
									OpenBrace: parser.Position{Line: 2, Column: 9},
								},
							},
						},
					},
				},
			}},
			checked: nil,
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			checked := testCase.checked
			if checked == nil {
				checked = resultForMain(testCase.program)
			}
			if _, err := Generate([]*parser.Module{{ID: 0, Program: testCase.program}}, checked); err == nil {
				t.Fatalf("Generate succeeded, want an error")
			}
		})
	}
}

func TestCStringLiteral(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "plain", input: `"hello"`, want: `"hello"`},
		{name: "empty", input: `""`, want: `""`},
		{name: "newline", input: `"a\nb"`, want: `"a\nb"`},
		{name: "tab", input: `"a\tb"`, want: `"a\tb"`},
		{name: "quote", input: `"a\"b"`, want: `"a\"b"`},
		{name: "backslash", input: `"a\\b"`, want: `"a\\b"`},
		{name: "question mark", input: `"a?b"`, want: `"a\?b"`},
		{name: "non ascii", input: `"é"`, want: `"\303\251"`},
		{name: "malformed", input: `hello`, wantErr: true},
		{name: "unsupported escape", input: `"a\qb"`, wantErr: true},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			got, err := cStringLiteral(testCase.input)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("cStringLiteral(%q) = %q, want an error", testCase.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("cStringLiteral(%q) returned error: %v", testCase.input, err)
			}
			if got != testCase.want {
				t.Errorf("cStringLiteral(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

// generateModules parses each source into a module, checks them together, and
// generates C.
func generateModules(t *testing.T, sources ...string) (string, error) {
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
		modules = append(modules, &parser.Module{
			Path:    fmt.Sprintf("module%d.ktn", index),
			ID:      index,
			Program: program,
		})
	}
	checked, err := checker.CheckModules(modules)
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	return Generate(modules, checked)
}

func TestModuleNamesAreMangled(t *testing.T) {
	generated, err := generateModules(t,
		`fn main() {
    Point point = Point { x = 1 };
    print(scale(point), helper());
}
`,
		`public struct Point {
    int x;
}
public fn scale(Point point) -> int {
    return point.x * factor();
}
public fn helper() -> int {
    return factor();
}
private fn factor() -> int {
    return 2;
}
`,
	)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}

	// Root-module names keep their plain form.
	assertContains(t, generated, "void ktn_fn_main(void)")
	// Imported declarations are prefixed with the module ID.
	assertContains(t, generated, "ktn_s_1_Point")
	assertContains(t, generated, "ktn_new_1_Point")
	assertContains(t, generated, "ktn_fn_1_scale")
	assertContains(t, generated, "ktn_fn_1_helper")
	assertContains(t, generated, "ktn_fn_1_factor")
}

func TestPrivateNamesAcrossModulesDoNotCollideInC(t *testing.T) {
	generated, err := generateModules(t,
		`fn main() {
    print(helper());
}
private fn helper() -> int {
    return 1;
}
`,
		`private fn helper() -> string {
    return "other";
}
`,
	)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	assertContains(t, generated, "int64_t ktn_fn_helper(void)")
	assertContains(t, generated, "const char * ktn_fn_1_helper(void)")
}

func TestEntryPointMustBeInTheRootModule(t *testing.T) {
	_, err := generateModules(t,
		`fn helper() -> int {
    return 1;
}
`,
		`public fn main() {
}
`,
	)
	if err == nil {
		t.Fatalf("Generate succeeded, want a missing-entry-point error")
	}
	if !strings.Contains(err.Error(), "no entry point") {
		t.Errorf("error %q does not mention the entry point", err.Error())
	}
}

func TestMultiModuleGenerationIsDeterministic(t *testing.T) {
	sources := []string{
		`fn main() {
    Point point = Point { x = 2 };
    print(scale(point));
}
`,
		`public struct Point {
    int x;
}
public fn scale(Point point) -> int {
    return point.x * 3;
}
`,
	}

	first, err := generateModules(t, sources...)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	second, err := generateModules(t, sources...)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	if first != second {
		t.Errorf("multi-module generation is not deterministic")
	}
}
