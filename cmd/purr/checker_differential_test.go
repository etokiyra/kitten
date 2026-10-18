package main

// Differential testing for the Kitten-written semantic checker in
// selfhost/checker.
//
// This file lives in the CLI package because the harness needs the same build
// pipeline and toolchain probe as the other integration tests: it compiles
// selfhost/check.ktn with the bootstrap compiler and runs the resulting
// executable once per corpus input.
//
// The reference is the Go checker in kitten/checker. Both implementations render
// one line per result:
//
//	ok modules <n> types <n> functions <n> bodies <n>
//	error <phase> <kind> <file> <line> <column> <message>
//
// and the harness compares the two lines byte for byte. That covers whether
// checking succeeded, the first error's category, position, and message when it
// did not, and the declaration counts when it did. Both implementations are
// fail-fast, so the first error is the whole observable outcome.
//
// The driver checks two modules: input.txt as the root and input2.txt as an
// import. The harness builds the same two modules for the reference, with the
// same paths and ids, so cross-module rules are exercised. A single-module
// program is checked with an empty second module, which contributes no
// declarations; TestEmptySecondModuleIsInert checks that this cannot change the
// outcome.
//
// Two differences are expected and are handled here rather than hidden. Both
// come from the shared Kitten lexer and parser, not from the checker: a column
// after a byte at or above 128 counts bytes rather than code points, and an
// error message that quotes such a byte names the byte. The harness therefore
// requires exact equality for input whose bytes are all ASCII, and compares the
// error phase, kind, and position for other input.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/lexer"
	"kitten/parser"
)

// kittenCheckerProgram is the driver program of the Kitten-written checker.
func kittenCheckerProgram() string {
	return filepath.Join("..", "..", "selfhost", "check.ktn")
}

// buildKittenChecker compiles the Kitten checker and returns the executable path.
func buildKittenChecker(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-checker")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenCheckerProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the Kitten checker: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the Kitten checker:\n%s", warnings)
	}
	return executable
}

// runKittenChecker writes the two module sources into directory and runs the
// Kitten checker there, returning its stdout.
func runKittenChecker(executable, directory, root, imported string) (string, error) {
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
		return "", fmt.Errorf("running the Kitten checker: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return "", fmt.Errorf("the Kitten checker wrote to stderr: %s", stderr.String())
	}
	return stdout.String(), nil
}

// checkerKindName maps an error kind to the single-token name the Kitten driver
// prints, which is the harness's normalized identity for the category.
func checkerKindName(kind checker.ErrorKind) string {
	switch kind {
	case checker.UndefinedName:
		return "UndefinedName"
	case checker.DuplicateDeclaration:
		return "DuplicateDeclaration"
	case checker.InvalidOperator:
		return "InvalidOperator"
	case checker.TypeMismatch:
		return "TypeMismatch"
	case checker.InvalidAssignment:
		return "InvalidAssignment"
	case checker.BadCall:
		return "BadCall"
	case checker.InvalidReturn:
		return "InvalidReturn"
	case checker.InvalidType:
		return "InvalidType"
	case checker.InvalidExpression:
		return "InvalidExpression"
	case checker.PrivateAccess:
		return "PrivateAccess"
	default:
		return "Unknown"
	}
}

// renderReferenceCheck lexes, parses, and checks a two-module program with the
// reference implementation and renders the same line the Kitten driver produces.
func renderReferenceCheck(root, imported string) (string, error) {
	rootProgram, failure, err := prepareReferenceModule(root, "input.txt")
	if err != nil {
		return "", err
	}
	if failure != "" {
		return failure, nil
	}
	importedProgram, failure, err := prepareReferenceModule(imported, "input2.txt")
	if err != nil {
		return "", err
	}
	if failure != "" {
		return failure, nil
	}

	modules := []*parser.Module{
		{Path: "input.txt", ID: 0, Program: rootProgram},
		{Path: "input2.txt", ID: 1, Program: importedProgram},
	}

	result, checkError := checker.CheckModules(modules)
	if checkError != nil {
		var semantic *checker.Error
		if !errors.As(checkError, &semantic) {
			return "", fmt.Errorf("the reference checker returned %T: %v", checkError, checkError)
		}
		return fmt.Sprintf("error Check %s %s %d %d %s\n",
			checkerKindName(semantic.Kind), semantic.File, semantic.Line, semantic.Column, semantic.Message), nil
	}

	types, functions := countDeclarations(result.Modules)
	return fmt.Sprintf("ok modules %d types %d functions %d bodies %d\n",
		len(result.Modules), types, functions, functions), nil
}

// prepareReferenceModule lexes and parses one module, rendering the lexical or
// syntax failure the driver would print.
func prepareReferenceModule(source, path string) (*parser.Program, string, error) {
	tokens, err := lexer.Lex(source)
	if err != nil {
		var lexError *lexer.Error
		if !errors.As(err, &lexError) {
			return nil, "", fmt.Errorf("the reference lexer returned %T: %v", err, err)
		}
		return nil, fmt.Sprintf("error Lex - %s %d %d %s\n", path, lexError.Line, lexError.Column, lexError.Message), nil
	}

	program, err := parser.Parse(tokens)
	if err != nil {
		var parseError *parser.Error
		if !errors.As(err, &parseError) {
			return nil, "", fmt.Errorf("the reference parser returned %T: %v", err, err)
		}
		return nil, fmt.Sprintf("error Parse - %s %d %d %s\n", path, parseError.Line, parseError.Column, parseError.Message), nil
	}
	return program, "", nil
}

// countDeclarations counts the named types and functions a program declares,
// which is what the driver's summary reports.
func countDeclarations(modules []*parser.Module) (int, int) {
	types := 0
	functions := 0
	for _, module := range modules {
		for _, declaration := range module.Program.Declarations {
			switch declaration.(type) {
			case *parser.StructDecl, *parser.EnumDecl, *parser.UnionDecl:
				types++
			case *parser.FunctionDecl:
				functions++
			}
		}
	}
	return types, functions
}

// checkerCase is one differential test input: a root module and, optionally, the
// module it imports.
type checkerCase struct {
	name     string
	root     string
	imported string
}

// checkerCorpus returns every input used for checker differential testing.
func checkerCorpus(t *testing.T) []checkerCase {
	t.Helper()

	var cases []checkerCase
	add := func(name, root string) {
		cases = append(cases, checkerCase{name: name, root: root})
	}
	addModules := func(name, root, imported string) {
		cases = append(cases, checkerCase{name: name, root: root, imported: imported})
	}

	// Valid programs.
	add("empty program", "")
	add("empty main", "fn main() {}\n")
	add("main with return type", "fn main() -> void {}\n")
	add("literals", "fn main() {\n    int a = 1;\n    float b = 1.5;\n    bool c = true;\n    string d = \"x\";\n}\n")
	add("arithmetic", "fn main() {\n    int a = 1 + 2 * 3 - 4 / 2 % 3;\n    float b = 1.0 + 2.0;\n    string c = \"a\" + \"b\";\n}\n")
	add("comparisons", "fn main() {\n    bool a = 1 < 2;\n    bool b = 1 <= 2;\n    bool c = 1 > 2;\n    bool d = 1 >= 2;\n    bool e = 1 == 2;\n    bool f = 1 != 2;\n    bool g = true && false;\n    bool h = true || false;\n    bool i = !true;\n}\n")
	add("unary and increment", "fn main() {\n    int a = -1;\n    a++;\n    a--;\n    ++a;\n    --a;\n}\n")
	add("assignments", "fn main() {\n    int a = 1;\n    a = 2;\n    a += 3;\n    a -= 4;\n    string s = \"a\";\n    s += \"b\";\n}\n")
	add("struct declaration and literal", "struct Point {\n    int x;\n    int y;\n}\nfn main() {\n    Point p = Point { x = 1, y = 2 };\n    int a = p.x + p.y;\n    p.x = 3;\n}\n")
	add("struct with collections", "struct Bag {\n    int[] values;\n    map[string]int counts;\n}\nfn main() {\n    Bag b = Bag { values = new int[2], counts = new map[string]int };\n}\n")
	add("enum", "enum Colour {\n    Red,\n    Green\n}\nfn main() {\n    Colour c = Colour.Red;\n    bool same = c == Colour.Green;\n}\n")
	add("union and match", "union Value {\n    Number { int amount; }\n    Empty\n}\nfn describe(Value v) -> int {\n    match (v) {\n        Number(amount) { return amount; }\n        Empty { return 0; }\n    }\n}\nfn main() {\n    print(describe(Value.Number { amount = 1 }), describe(Value.Empty));\n}\n")
	add("match with enum", "enum Kind { A, B }\nfn f(Kind k) -> int {\n    match (k) {\n        A { return 1; }\n        B { return 2; }\n    }\n}\n")
	add("recursive union", "union Expr {\n    Num { int value; }\n    Add { Expr left; Expr right; }\n}\nfn eval(Expr e) -> int {\n    match (e) {\n        Num(value) { return value; }\n        Add(left, right) { return eval(left) + eval(right); }\n    }\n}\n")
	add("array operations", "fn main() {\n    int[] values = new int[3];\n    values[0] = 1;\n    values[0] += 2;\n    values[0]++;\n    int total = length(values);\n    int[][] grid = new int[][2];\n    grid[0] = new int[1];\n}\n")
	add("map operations", "fn main() {\n    map[string]int table = new map[string]int;\n    table[\"a\"] = 1;\n    table[\"a\"] += 2;\n    int v = table[\"a\"];\n    bool present = has(table, \"a\");\n    int n = length(table);\n}\n")
	add("string operations", "fn main() {\n    string s = \"hello\";\n    int n = length(s);\n    int b = s[0];\n    string t = substring(s, 1, 2);\n    bool same = s == \"hello\";\n}\n")
	add("builtins", "fn main() {\n    print(1, 2.5, true, \"x\");\n    eprint(1, 2.5, true, \"x\");\n    print(length(new int[1]));\n    print(read_file(\"x\") == \"\");\n    print(length(argv()), argv()[0]);\n}\n")
	add("exit built-in", "fn stop(int code) {\n    exit(code);\n    exit(1 + 2);\n}\nfn main() {\n}\n")
	add("defined integer arithmetic", `fn main() {
    int maximum = 9223372036854775807;
    int minimum = -9223372036854775807 - 1;
    int sum = maximum + 1;
    int difference = minimum - 1;
    int product = maximum * 2;
    int negated = -minimum;
    sum += 1;
    sum -= 1;
    sum++;
    ++sum;
    sum--;
    --sum;
    int[] values = new int[1];
    values[0] += 1;
    values[0]--;
    map[string]int counts = new map[string]int;
    counts["k"] = 1;
    counts["k"] += 2;
    counts["k"]--;
    int quotient = minimum / -1;
    int remainder = minimum % -1;
    int zero_divisor = 0;
    int by_zero = 1 / zero_divisor;
}
`)
	add("try_read_file match on both variants", `fn main() {
    match (try_read_file("x")) {
        Ok(text) { print(text); }
        Failed(message) { print(message); }
    }
}
`)
	add("try_read_file direct variant construction", `fn trigger() {
    try_read_file("x");
}
fn main() {
    ReadFileResult ok = ReadFileResult.Ok { text = "x" };
    ReadFileResult failed = ReadFileResult.Failed { message = "y" };
}
`)
	add("shadowed try_read_file still injects the prelude", `fn try_read_file(string path) -> int {
    return 1;
}
fn main() {
    print(try_read_file("x"));
}
`)
	add("try_read_file spelling in a string is not a trigger", `fn main() {
    print("try_read_file(");
}
`)
	add("file and directory builtins and result unions", `fn describe_file(FileResult result) -> int {
    match (result) {
        Ok { return 1; }
        Failed(message) { return 2; }
    }
}
fn describe_list(ListDirectoryResult result) -> int {
    match (result) {
        Ok(entries) {
            int index = 0;
            while (index < length(entries)) {
                print(entries[index]);
                index++;
            }
            return length(entries);
        }
        Failed(message) { return -1; }
    }
}
fn main() {
    FileResult written = try_write_file("x", "y");
    FileResult bytes = try_write_bytes("x", string_to_bytes("y"));
    FileResult made = try_create_directory("d");
    FileResult removed = try_remove("d");
    ListDirectoryResult listed = try_list_directory("x");
    FileResult direct_ok = FileResult.Ok;
    FileResult direct_failed = FileResult.Failed { message = "m" };
    string[] entries = new string[1];
    entries[0] = "a";
    ListDirectoryResult direct_list = ListDirectoryResult.Ok { entries = entries };
    ListDirectoryResult direct_list_failed = ListDirectoryResult.Failed { message = "m" };
    print(describe_file(written), describe_file(bytes), describe_file(made), describe_file(removed));
    print(describe_file(direct_ok), describe_file(direct_failed));
    print(describe_list(listed), describe_list(direct_list), describe_list(direct_list_failed));
}
`)
	add("try_write_file only", `fn main() {
    match (try_write_file("x", "y")) {
        Ok { print("ok"); }
        Failed(message) { print(message); }
    }
}
`)
	add("try_list_directory only", `fn main() {
    match (try_list_directory("x")) {
        Ok(entries) { print(length(entries)); }
        Failed(message) { print(message); }
    }
}
`)
	add("try_read_file with a new builtin", `fn main() {
    try_read_file("x");
    try_write_file("y", "z");
}
`)
	add("shadowed try_write_file still injects its prelude", `fn try_write_file(string path, string text) -> int {
    return 1;
}
fn main() {
    print(try_write_file("x", "y"));
}
`)
	add("bitwise and shift operators", `fn main() {
    int mask = 0xFF;
    int bits = 0b1010;
    int a = mask & 0x0F;
    int b = mask | 0xF0;
    int c = mask ^ 0xAA;
    int d = ~mask;
    int e = 0x8000000000000000 >> 4;
    int f = 1 << 63;
    int count = 6;
    int g = 1 << count;
    int h = -8 >> count;
}
`)
	add("byte values, arrays, and conversions", `struct ByteBox {
    byte value;
    byte[] bytes;
}
union ByteValue {
    B { byte value; byte[] bytes; }
    Empty
}
fn make_bytes(byte first, byte[] rest) -> byte[] {
    byte[] result = new byte[2];
    result[0] = first;
    result[1] = rest[0];
    return result;
}
fn main() {
    byte a = byte_from_int(65);
    byte b = byte_from_int(256);
    byte[] values = new byte[3];
    values[0] = a;
    values[1] = b;
    byte[] made = make_bytes(a, values);
    print(a, b, int_from_byte(a), int_from_byte(values[1]));
    print(a == b, a != b, a < b, a <= b, a > b, a >= b);
    eprint(a);
    map[string]byte table = new map[string]byte;
    table["k"] = a;
    print(int_from_byte(table["k"]));
    ByteBox box = ByteBox { value = a, bytes = made };
    print(int_from_byte(box.value), length(box.bytes));
    ByteValue value = ByteValue.B { value = b, bytes = made };
    match (value) {
        B(code, bytes) { print(int_from_byte(code), length(bytes)); }
        Empty { print(0); }
    }
    byte[] converted = string_to_bytes("Aé");
    print(length(converted), int_from_byte(converted[0]), int_from_byte(converted[2]));
    string text = bytes_to_string(converted);
    print(text);
}
`)
	add("control flow", "fn main() {\n    int i = 0;\n    if (i == 0) {\n        i = 1;\n    } else if (i == 1) {\n        i = 2;\n    } else {\n        i = 3;\n    }\n    while (i > 0) {\n        i--;\n    }\n    for (int j = 0; j < 3; j++) {\n        i += j;\n    }\n    for (;;) {\n    }\n}\n")
	add("nested blocks and shadowing", "fn main() {\n    int x = 1;\n    {\n        int x = 2;\n        {\n            int x = 3;\n        }\n    }\n    if (true) {\n        int x = 4;\n    }\n}\n")
	add("forward references", "fn main() {\n    print(later());\n    Point p = make();\n}\nfn later() -> int {\n    return 1;\n}\nstruct Point { int x; }\nfn make() -> Point {\n    return Point { x = 1 };\n}\n")
	add("mutual recursion", "fn even(int n) -> bool {\n    if (n == 0) {\n        return true;\n    }\n    return odd(n - 1);\n}\nfn odd(int n) -> bool {\n    if (n == 0) {\n        return false;\n    }\n    return even(n - 1);\n}\n")
	add("returns on all paths", "fn f(int n) -> int {\n    if (n > 0) {\n        return 1;\n    } else {\n        return 2;\n    }\n}\nfn g(int n) -> int {\n    match (n) {\n        A { return 1; }\n        B { return 2; }\n    }\n}\n")
	add("user print shadows builtin", "fn print(int value) -> int {\n    return value;\n}\nfn main() {\n    print(1);\n}\n")
	add("parameters and locals share a scope", "fn f(int a) {\n    int b = a;\n}\n")

	// Invalid programs: one per semantic rule, with the reference's wording.
	invalid := []string{
		// Declarations.
		"enum E {}\n",
		"union U {}\n",
		"enum E { A, A }\n",
		"union U { A A }\n",
		"struct S { int a; int a; }\n",
		"union U { A { int x; int x; } }\n",
		"struct S { }\nstruct S { }\n",
		"fn f() {}\nfn f() {}\n",
		"struct S {}\nfn S() {}\n",
		"fn f() {}\nstruct f {}\n",
		"enum E { A }\nfn E() {}\n",
		"struct S { void a; }\n",
		"union U { A { void x; } }\n",
		"fn f(void a) {}\n",
		"fn f(int a, int a) {}\n",
		"fn f(void) {}\n",
		"fn f() -> void { int x = 1; }\nstruct S { int a; }\nstruct T { void v; }\n",
		"fn f() { S s = S { x = void_call() }; }\nfn void_call() {}\nstruct S { int x; }\n",

		// Type resolution.
		"fn f() { Unknown x = 1; }\n",
		"fn f() -> Unknown {}\n",
		"struct S { Unknown field; }\n",
		"fn f() { Unknown[] x; }\n",
		"fn f() { int[] x = new Unknown[1]; }\n",
		"fn f() { map[int]int m = new map[int]int; }\n",
		"fn f() { map[string]void m = new map[string]void; }\n",
		"fn f() { void[] x; }\n",
		"fn f() { void x; }\n",
		"fn f() { int[] x = new void[1]; }\n",

		// Variables and assignment.
		"fn f() { int x = \"s\"; }\n",
		"fn f() { int x; int x; }\n",
		"fn f(int a) { int a; }\n",
		"fn f() { int x = y; }\n",
		"fn f() { 1 = 2; }\n",
		"fn f() { \"a\" = \"b\"; }\n",
		"fn f() { true = false; }\n",
		"fn f() { int x = 1; x = \"s\"; }\n",
		"fn f() { int x = 1; x += \"s\"; }\n",
		"fn f() { string s = \"a\"; s -= \"b\"; }\n",
		"fn f() { string s = \"a\"; s[0] = 1; }\n",
		"fn f() { f() = 1; }\n",
		"fn f() { bool b = true; b++; }\n",
		"fn f() { string s = \"a\"; s++; }\n",
		"fn f() { S s = S { x = 1 }; s.x++; }\nstruct S { int x; }\n",
		"fn f() { S s = S { x = 1 }; s.y = 1; }\nstruct S { int x; }\n",

		// Operators.
		"fn f() { int x = 1 + 1.5; }\n",
		"fn f() { int x = 1 % 1.5; }\n",
		"fn f() { float x = 1.5 % 1.5; }\n",
		"fn f() { bool b = \"a\" < \"b\"; }\n",
		"fn f() { bool b = true < false; }\n",
		"fn f() { bool b = 1 && 1; }\n",
		"fn f() { bool b = 1 || true; }\n",
		"fn f() { int x = -\"a\"; }\n",
		"fn f() { bool b = !1; }\n",
		"fn f() { bool b = -true; }\n",
		"fn f() { int x = 1 + true; }\n",
		"fn f() { int x = \"a\" * \"b\"; }\n",
		"fn f() { bool b = \"a\" == 1; }\n",
		"struct S { int x; }\nfn f() { bool b = S { x = 1 } == S { x = 1 }; }\n",
		"fn f() { bool b = new int[1] == new int[1]; }\n",
		"fn f() { bool b = new map[string]int == new map[string]int; }\n",
		"union U { A }\nfn f() { bool b = U.A == U.A; }\n",
		"enum E { A }\nfn f() { bool b = E.A < E.A; }\n",
		"enum E { A }\nfn f() { int x = E.A + E.A; }\n",

		// Conditions.
		"fn f() { if (1) {} }\n",
		"fn f() { while (\"a\") {} }\n",
		"fn f() { for (; 1; ) {} }\n",

		// Returns.
		"fn f() { return 1; }\n",
		"fn f() -> int { return; }\n",
		"fn f() -> int { return \"a\"; }\n",
		"fn f() -> int {\n    if (true) {\n        return 1;\n    }\n}\n",
		"fn f() -> int {\n    while (true) {\n        return 1;\n    }\n}\n",
		"fn f() -> int { int x = 1; }\n",
		"fn f() -> int {\n    match (E.A) {\n        A { return 1; }\n    }\n}\nenum E { A }\n",

		// Calls.
		"fn f() { g(); }\n",
		"fn g(int a) {}\nfn f() { g(); }\n",
		"fn g(int a) {}\nfn f() { g(1, 2); }\n",
		"fn g(int a) {}\nfn f() { g(\"s\"); }\n",
		"fn f() { int x = 1; x(); }\n",
		"fn f() { int x = 1; }\nfn g() { f()(); }\n",
		"struct S { int x; }\nfn f() { S(1); }\n",
		"fn f() { print(); }\n",
		"struct S { int x; }\nfn f() { print(S { x = 1 }); }\n",
		"fn f() { print(new int[1]); }\n",
		"fn f() { print(f); }\n",

		// Built-ins.
		"fn f() { length(); }\n",
		"fn f() { length(1); }\n",
		"fn f() { length(1, 2); }\n",
		"fn f() { has(new int[1], \"a\"); }\n",
		"fn f() { has(new map[string]int); }\n",
		"fn f() { has(new map[string]int, 1); }\n",
		"fn f() { substring(\"a\"); }\n",
		"fn f() { substring(1, 0, 1); }\n",
		"fn f() { substring(\"a\", \"b\", 1); }\n",
		"fn f() { read_file(); }\n",
		"fn f() { read_file(1); }\n",
		"fn f() { argv(1); }\n",
		"fn f() { string text = argv(); }\n",
		"fn f() { eprint(); }\n",
		"fn f() { eprint(new int[1]); }\n",
		"fn f() { exit(); }\n",
		"fn f() { exit(1, 2); }\n",
		"fn f() { exit(\"x\"); }\n",
		"fn f() { exit(1.5); }\n",
		"fn f() { exit(true); }\n",
		"fn f() -> int { exit(1); }\n",
		"fn f() { int x = 9223372036854775808; }\n",
		"fn f() { bool b = true; int x = b & 1; }\n",
		"fn f() { float b = 1.0; int x = b | 1; }\n",
		"fn f() { string b = \"x\"; int x = b ^ 1; }\n",
		"fn f() { bool b = true; int x = ~b; }\n",
		"fn f() { int x = 1 << true; }\n",
		"fn f() { bool b = true; bool c = false; int a = 1; int x = a & b == c; }\n",
		"fn f() { byte a = byte_from_int(1); byte b = a + a; }\n",
		"fn f() { byte a = byte_from_int(1); byte b = a & a; }\n",
		"fn f() { byte a = byte_from_int(1); byte b = -a; }\n",
		"fn f() { byte a = byte_from_int(1); bool b = a == 1; }\n",
		"fn f() { byte a = byte_from_int(1); bool b = a < 1; }\n",
		"fn f() { int x = 1; byte b = x; }\n",
		"fn f() { byte b = byte_from_int(1); int x = b; }\n",
		"fn g(int value) {}\nfn f() { byte b = byte_from_int(1); g(b); }\n",
		"fn f() { byte b = byte_from_int(1); if (b) {} }\n",
		"fn f() { byte b = byte_from_int(1); b++; }\n",
		"fn f() { byte b = byte_from_int(1); b += b; }\n",
		"fn f() { map[byte]int m = new map[byte]int; }\n",
		"fn f() { int x = int_from_byte(); }\n",
		"fn f() { int x = int_from_byte(1); }\n",
		"fn f() { byte b = byte_from_int(); }\n",
		"fn f() { byte b = byte_from_int(true); }\n",
		"fn f() { byte[] b = string_to_bytes(); }\n",
		"fn f() { byte[] b = string_to_bytes(1); }\n",
		"fn f() { string s = bytes_to_string(); }\n",
		"fn f() { string s = bytes_to_string(\"x\"); }\n",
		"fn f() { int x = 0x; }\n",
		"fn f() { int x = 0b102; }\n",
		"fn ReadFileResult() {}\n",
		"struct ReadFileResult {}\n",
		"enum ReadFileResult { A }\n",
		"union ReadFileResult { A }\n",
		"fn f() { match (try_read_file(\"x\")) { Ok(text) { } } }\n",
		"fn f() { try_read_file(); }\n",
		"fn f() { try_read_file(1); }\n",
		"fn f() { match (try_write_file(\"x\", \"y\")) { Ok { } } }\n",
		"fn f() { match (try_list_directory(\"x\")) { Ok(entries) { } } }\n",
		"fn f() { try_write_file(); }\n",
		"fn f() { try_write_file(\"x\"); }\n",
		"fn f() { try_write_file(1, \"y\"); }\n",
		"fn f() { try_write_file(\"x\", 1); }\n",
		"fn f() { try_write_bytes(\"x\"); }\n",
		"fn f() { try_write_bytes(\"x\", \"y\"); }\n",
		"fn f() { try_create_directory(); }\n",
		"fn f() { try_create_directory(1); }\n",
		"fn f() { try_remove(); }\n",
		"fn f() { try_remove(1); }\n",
		"fn f() { try_list_directory(); }\n",
		"fn f() { try_list_directory(1); }\n",
		"fn FileResult() {}\n",
		"struct FileResult {}\n",
		"enum FileResult { A }\n",
		"union FileResult { A }\n",
		"fn ListDirectoryResult() {}\n",
		"struct ListDirectoryResult {}\n",
		"enum ListDirectoryResult { A }\n",
		"union ListDirectoryResult { A }\n",

		// Indexing.
		"fn f() { int x = 1; int y = x[0]; }\n",
		"fn f() { int[] a = new int[1]; int y = a[\"s\"]; }\n",
		"fn f() { string s = \"a\"; int y = s[\"s\"]; }\n",
		"fn f() { map[string]int m = new map[string]int; int y = m[1]; }\n",
		"fn f() { bool b = true; bool y = b[0]; }\n",
		"fn f() { int[] a = new int[1]; a[true] = 1; }\n",

		// Struct literals and members.
		"struct S { int x; }\nfn f() { S s = S {}; }\n",
		"struct S { int x; }\nfn f() { S s = S { y = 1 }; }\n",
		"struct S { int x; }\nfn f() { S s = S { x = 1, x = 2 }; }\n",
		"struct S { int x; }\nfn f() { S s = S { x = \"a\" }; }\n",
		"struct S { int x; }\nfn f() { int y = S { x = 1 }.z; }\n",
		"enum E { A }\nfn f() { int y = E { }; }\n",
		"union U { A }\nfn f() { U u = U { }; }\n",
		"fn f() { int y = 1.x; }\n",
		"fn f() { string s = \"a\"; int y = s.x; }\n",
		"enum E { A }\nfn f() { int y = E.A.x; }\n",
		"union U { A }\nfn f(U u) { int y = u.x; }\n",
		"struct S { int x; }\nfn f(S s) { int y = s.zzz; }\n",

		// Enums and unions.
		"enum E { A }\nfn f() { E e = E.B; }\n",
		"union U { A }\nfn f() { U u = U.B; }\n",
		"union U { A { int x; } }\nfn f() { U u = U.A; }\n",
		"union U { A }\nfn f() { U u = U.A { }; }\n",
		"union U { A { int x; } }\nfn f() { U u = U.A { }; }\n",
		"union U { A { int x; } }\nfn f() { U u = U.A { x = 1, x = 2 }; }\n",
		"union U { A { int x; } }\nfn f() { U u = U.A { x = \"s\" }; }\n",
		"union U { A { int x; } }\nfn f() { U u = U.A { y = 1 }; }\n",
		"struct S { int x; }\nfn f() { union_local(); }\nunion_local();\n",
		"fn f() { S s = S { x = 1 }; }\nstruct S { int x; }\n",

		// Match.
		"fn f(int n) { match (n) { A { } } }\n",
		"enum E { A, B }\nfn f(E e) { match (e) { A { } } }\n",
		"enum E { A }\nfn f(E e) { match (e) { A { } A { } } }\n",
		"enum E { A }\nfn f(E e) { match (e) { B { } } }\n",
		"enum E { A }\nfn f(E e) { match (e) { A(x) { } } }\n",
		"union U { A { int x; } }\nfn f(U u) { match (u) { A { } } }\n",
		"union U { A { int x; } }\nfn f(U u) { match (u) { A(a, b) { } } }\n",
		"union U { A { int x; int y; } }\nfn f(U u) { match (u) { A(a, a) { } } }\n",
		"enum E { A }\nfn f(E e) { match (e) { A { } }\n}\n",
		"enum E { A }\nfn f(E e) { match (e) { A { return; } } }\nfn g() {}\n",

		// Scoping and shadowing edge cases.
		"fn f() { int x = x; }\n",
		"fn f() { { int y = 1; } int z = y; }\n",
		"fn f() { for (int i = 0; i < 1; i++) { } int j = i; }\n",
		"fn f() { int i = 0; for (int i = 0; i < 1; i++) { } }\n",
		"enum E { A }\nfn f(E e) { match (e) { A { int v = 1; } int w = v; } }\n",
		"fn f() { if (true) { int a = 1; } int b = a; }\n",
		"fn main() { print(main); }\n",
		"struct S { int x; }\nfn main() { print(S); }\n",
		"fn main() { print(main()); }\n",
	}
	for index, source := range invalid {
		add("invalid "+strconv.Itoa(index+1), source)
	}

	// Systematic operator matrices: each binary operator applied to each type.
	operandTypes := map[string]string{
		"int":    "1",
		"float":  "1.5",
		"bool":   "true",
		"string": "\"s\"",
		"struct": "P { x = 1 }",
		"array":  "new int[1]",
		"map":    "new map[string]int",
		"enum":   "E.A",
		"union":  "U.A { x = 1 }",
		"void":   "nothing()",
	}
	typeNames := []string{"int", "float", "bool", "string", "struct", "array", "map", "enum", "union", "void"}
	operators := []string{"+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "&&", "||"}
	prefix := "struct P { int x; }\nenum E { A }\nunion U { A { int x; } }\nfn nothing() {}\n"
	for _, operator := range operators {
		for _, leftName := range typeNames {
			for _, rightName := range typeNames {
				source := fmt.Sprintf("%s\nfn f() {\n    bool result = %s %s %s;\n}\n",
					prefix, operandTypes[leftName], operator, operandTypes[rightName])
				add("operator "+operator+" "+leftName+" "+rightName, source)
			}
		}
	}

	// Assignment matrices: every assignment operator to every target type.
	for _, operator := range []string{"=", "+=", "-="} {
		for _, leftName := range typeNames {
			for _, rightName := range typeNames {
				source := fmt.Sprintf("%s\nfn f() {\n    %s target = %s;\n    target %s %s;\n}\n",
					prefix, assignmentType(leftName), operandTypes[leftName], operator, operandTypes[rightName])
				add("assignment "+operator+" "+leftName+" "+rightName, source)
			}
		}
	}

	// Indexing matrices: every target type indexed by every index type.
	indexTypes := []string{"int", "string", "bool"}
	for _, targetName := range typeNames {
		for _, indexName := range indexTypes {
			source := fmt.Sprintf("%s\nfn f() {\n    %s target = %s;\n    int result = target[%s];\n}\n",
				prefix, assignmentType(targetName), operandTypes[targetName], operandTypes[indexName])
			add("index "+targetName+" "+indexName, source)
		}
	}

	// Deeply nested and larger generated inputs.
	add("deep parentheses", "fn f() {\n    int x = "+strings.Repeat("(", 40)+"1"+strings.Repeat(")", 40)+";\n}\n")
	add("long chain", "fn f() {\n    int x = "+strings.Repeat("1 + ", 120)+"1;\n}\n")
	add("many declarations", strings.Repeat("struct S0 { int x; }\n", 1)+strings.Repeat("fn f0() {}\n", 0)+buildManyFunctions(120))
	add("long function body", "fn f() {\n"+strings.Repeat("    int x0 = 1;\n", 1)+strings.Repeat("    x0 = x0 + 1;\n", 200)+"}\n")

	// Lexical and syntax failures, which must be distinguished from semantic
	// failures.
	malformed := []string{
		"fn f( {\n",
		"fn f() { int x }\n",
		"fn f() { return }\n",
		"enum E { A, }\n",
		"struct S { int }\n",
		"@\n",
		"\"unterminated\n",
		"fn f() { x = ; }\n",
		"",
	}
	for index, source := range malformed {
		add("malformed "+strconv.Itoa(index+1), source)
	}

	// Cross-module cases: the imported module is input2.txt.
	addModules("imports public struct", "import \"input2.txt\";\nfn main() {\n    P p = P { x = 1 };\n    print(p.x, label());\n}\n",
		"public struct P {\n    int x;\n}\npublic fn label() -> string {\n    return \"lib\";\n}\n")
	addModules("imports private function is rejected", "fn main() {\n    print(secret());\n}\n",
		"private fn secret() -> int {\n    return 1;\n}\n")
	addModules("imports private type is rejected", "fn main() {\n    Hidden h = Hidden { x = 1 };\n}\n",
		"private struct Hidden {\n    int x;\n}\n")
	addModules("imports private function in a call is rejected", "fn main() {\n    hidden();\n}\n",
		"fn hidden() {\n}\n")
	addModules("imports private type as a value is rejected", "fn main() {\n    print(Hidden);\n}\n",
		"private struct Hidden {\n    int x;\n}\n")
	addModules("duplicate public declaration", "fn main() {\n    print(shared());\n}\n",
		"public fn shared() -> int {\n    return 1;\n}\n")
	addModules("duplicate public declaration twice", "public fn shared() -> int {\n    return 0;\n}\nfn main() {\n    print(shared());\n}\n",
		"public fn shared() -> int {\n    return 1;\n}\n")
	addModules("public enum and union across modules", "import \"input2.txt\";\nfn describe(Kind k) -> int {\n    match (k) {\n        A { return 1; }\n        B { return 2; }\n    }\n}\nfn main() {\n    Value v = Value.Number { amount = 1 };\n    print(describe(Kind.A));\n}\n",
		"public enum Kind {\n    A,\n    B\n}\npublic union Value {\n    Number { int amount; }\n    Empty\n}\n")
	addModules("imported type in a struct field", "import \"input2.txt\";\nstruct Wrapper {\n    P p;\n}\nfn main() {\n    Wrapper w = Wrapper { p = P { x = 1 } };\n}\n",
		"public struct P {\n    int x;\n}\n")
	addModules("module sees its own private declaration", "import \"input2.txt\";\nfn main() {\n    print(other());\n}\n",
		"private fn hidden() -> int {\n    return 1;\n}\npublic fn other() -> int {\n    return hidden();\n}\n")
	addModules("both modules declare a private name", "struct Hidden { int x; }\nfn main() {\n    Hidden h = Hidden { x = 1 };\n}\n",
		"struct Hidden { int x; }\npublic fn make() -> int {\n    Hidden h = Hidden { x = 1 };\n    return h.x;\n}\n")
	addModules("map of imported type", "import \"input2.txt\";\nfn main() {\n    map[string]P table = new map[string]P;\n    table[\"a\"] = P { x = 1 };\n}\n",
		"public struct P {\n    int x;\n}\n")
	addModules("try_write_file from an imported module", `import "input2.txt";
fn main() {
    print(classify(try_write_file("x", "y")));
}
`, `public fn classify(FileResult result) -> int {
    match (result) {
        Ok { return 1; }
        Failed(message) { return 2; }
    }
}
`)
	addModules("try_list_directory from an imported module", `import "input2.txt";
fn main() {
    print(classify(try_list_directory("x")));
}
`, `public fn classify(ListDirectoryResult result) -> int {
    match (result) {
        Ok(entries) { return length(entries); }
        Failed(message) { return -1; }
    }
}
`)
	addModules("try_read_file from an imported module", `import "input2.txt";
fn main() {
    print(classify(try_read_file("x")));
}
`, `public fn classify(ReadFileResult result) -> int {
    match (result) {
        Ok(text) { return 1; }
        Failed(message) { return 2; }
    }
}
`)
	addModules("imported malformed module", "fn main() {}\n", "fn broken( {\n")
	addModules("root malformed with valid import", "fn broken( {\n", "fn ok() {}\n")

	// Every fixture and self-hosted source, checked as the root module.
	inputs, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.ktn"))
	if err != nil {
		t.Fatalf("globbing fixtures: %v", err)
	}
	inputs = append(inputs,
		filepath.Join("..", "..", "tests", "modules", "main.ktn"),
		filepath.Join("..", "..", "tests", "modules", "geometry.ktn"),
		filepath.Join("..", "..", "tests", "modules", "util", "text.ktn"),
		filepath.Join("..", "..", "selfhost", "parse.ktn"),
		filepath.Join("..", "..", "selfhost", "check.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "tokens.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "lexer.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "main.ktn"),
		filepath.Join("..", "..", "selfhost", "parser", "ast.ktn"),
		filepath.Join("..", "..", "selfhost", "parser", "parser.ktn"),
		filepath.Join("..", "..", "selfhost", "parser", "printer.ktn"),
		filepath.Join("..", "..", "selfhost", "checker", "model.ktn"),
		filepath.Join("..", "..", "selfhost", "checker", "state.ktn"),
		filepath.Join("..", "..", "selfhost", "checker", "expr.ktn"),
		filepath.Join("..", "..", "selfhost", "checker", "stmt.ktn"),
		filepath.Join("..", "..", "selfhost", "checker", "prelude.ktn"),
		filepath.Join("..", "..", "selfhost", "checker", "checker.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen", "names.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen", "output.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen", "errors.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen", "expressions.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen", "literals.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen", "statements.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen", "unit.ktn"),
		filepath.Join("..", "..", "selfhost", "codegen", "declarations.ktn"),
		filepath.Join("..", "..", "selfhost", "declarations.ktn"),
		filepath.Join("..", "..", "selfhost", "expressions.ktn"),
		filepath.Join("..", "..", "selfhost", "functions.ktn"),
		filepath.Join("..", "..", "selfhost", "unit.ktn"),
	)
	sort.Strings(inputs)
	for _, input := range inputs {
		content, err := os.ReadFile(input)
		if err != nil {
			t.Fatalf("reading %s: %v", input, err)
		}
		add("source "+filepath.Base(input), string(content))
	}

	if len(cases) < 500 {
		t.Fatalf("the checker corpus is unexpectedly small: %d inputs", len(cases))
	}
	return cases
}

// assignmentType returns a declaration type name for an operand type.
func assignmentType(name string) string {
	switch name {
	case "struct":
		return "P"
	case "array":
		return "int[]"
	case "map":
		return "map[string]int"
	case "enum":
		return "E"
	case "union":
		return "U"
	case "void":
		return "int"
	default:
		return name
	}
}

// buildManyFunctions returns many function declarations, to exercise scale.
func buildManyFunctions(count int) string {
	var builder strings.Builder
	for index := 0; index < count; index++ {
		fmt.Fprintf(&builder, "fn f%d(int a, int b) -> int {\n    return a + b;\n}\n", index)
	}
	return builder.String()
}

// describeCheckerCase renders a case for a failure message, truncating long
// sources.
func describeCheckerCase(testCase checkerCase) string {
	root := testCase.root
	const limit = 160
	if len(root) > limit {
		root = root[:limit] + "..."
	}
	if testCase.imported != "" {
		return fmt.Sprintf("%s %q with import %q", testCase.name, root, testCase.imported)
	}
	return fmt.Sprintf("%s %q", testCase.name, root)
}

// TestKittenCheckerMatchesReference runs the whole corpus through both
// implementations and compares their rendered results.
func TestKittenCheckerMatchesReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenChecker(t)
	directory := t.TempDir()
	cases := checkerCorpus(t)
	t.Logf("checker differential corpus: %d inputs", len(cases))

	exact := 0
	loose := 0
	for _, testCase := range cases {
		want, err := renderReferenceCheck(testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", describeCheckerCase(testCase), err)
			continue
		}

		got, err := runKittenChecker(executable, directory, testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", describeCheckerCase(testCase), err)
			continue
		}

		if got == want {
			exact++
			continue
		}

		// A difference is only expected when the input contains a byte >= 128,
		// where byte-oriented column counting can change a position or a quoted
		// lexeme.
		if !isASCII(testCase.root) || !isASCII(testCase.imported) {
			loose++
			compareNonASCIIChecker(t, testCase, want, got)
			continue
		}

		t.Errorf("checker mismatch for %s\n--- reference ---\n%s--- Kitten ---\n%s", describeCheckerCase(testCase), want, got)
	}

	t.Logf("checker corpus: %d inputs, %d byte-identical, %d compared without text (non-ASCII)", len(cases), exact, loose)
}

// compareNonASCIIChecker compares the two renderings for input containing a byte
// at or above 128, accepting only the documented differences: the phase, the
// error category, and the position must match, while a message that quotes such
// a byte and the declaration counts are not compared.
func compareNonASCIIChecker(t *testing.T, testCase checkerCase, want, got string) {
	t.Helper()

	wantFields := strings.Fields(strings.TrimSuffix(want, "\n"))
	gotFields := strings.Fields(strings.TrimSuffix(got, "\n"))
	if len(wantFields) < 2 || len(gotFields) < 2 {
		t.Errorf("unexpected rendering for %s\n--- reference ---\n%s--- Kitten ---\n%s", describeCheckerCase(testCase), want, got)
		return
	}
	if wantFields[0] != gotFields[0] {
		t.Errorf("outcome differs for %s: %q, want %q", describeCheckerCase(testCase), gotFields[0], wantFields[0])
		return
	}
	if wantFields[0] == "error" {
		if len(wantFields) < 6 || len(gotFields) < 6 {
			t.Errorf("unexpected error rendering for %s", describeCheckerCase(testCase))
			return
		}
		// error <phase> <kind> <file> <line> <column> <message...>
		if wantFields[1] != gotFields[1] || wantFields[2] != gotFields[2] {
			t.Errorf("error phase or kind differs for %s: %q, want %q", describeCheckerCase(testCase), got, want)
		}
		if wantFields[4] != gotFields[4] || wantFields[5] != gotFields[5] {
			t.Errorf("error position differs for %s: %q, want %q", describeCheckerCase(testCase), got, want)
		}
		return
	}
	if want != got {
		t.Errorf("summary differs for %s\n--- reference ---\n%s--- Kitten ---\n%s", describeCheckerCase(testCase), want, got)
	}
}

// TestKittenCheckerOutputFormat pins the driver's output format with explicit
// expected text, so that a change to the format is caught even if both
// implementations change together.
func TestKittenCheckerOutputFormat(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenChecker(t)
	directory := t.TempDir()

	cases := []struct {
		name     string
		root     string
		imported string
		want     string
	}{
		{
			name: "success",
			root: "struct Point { int x; }\nfn main() {\n    Point p = Point { x = 1 };\n}\n",
			want: "ok modules 2 types 1 functions 1 bodies 1\n",
		},
		{
			name: "semantic error",
			root: "fn main() {\n    int x = \"s\";\n}\n",
			want: "error Check TypeMismatch input.txt 2 13 initializer for variable \"x\": expected int, found string\n",
		},
		{
			name: "undefined name",
			root: "fn main() {\n    print(nowhere);\n}\n",
			want: "error Check UndefinedName input.txt 2 11 undefined name \"nowhere\"\n",
		},
		{
			name:     "imported module error",
			root:     "fn main() {\n    print(hidden());\n}\n",
			imported: "fn hidden() {\n}\n",
			want:     "error Check PrivateAccess input.txt 2 11 function \"hidden\" is private to input2.txt\n",
		},
		{
			name: "syntax error",
			root: "fn f( {\n",
			want: "error Parse - input.txt 1 7 expected a type, found LBrace \"{\"\n",
		},
		{
			name: "lexical error",
			root: "\"unterminated\n",
			want: "error Lex - input.txt 1 14 newline in string literal\n",
		},
	}

	for _, testCase := range cases {
		got, err := runKittenChecker(executable, directory, testCase.root, testCase.imported)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Errorf("%s: output = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// TestEmptySecondModuleIsInert checks the harness convention that a single-module
// program can be checked as two modules when the second is empty: the reference
// reports the same outcome either way, on the whole single-module corpus.
func TestEmptySecondModuleIsInert(t *testing.T) {
	cases := checkerCorpus(t)
	checked := 0
	for _, testCase := range cases {
		if testCase.imported != "" {
			continue
		}

		program, failure, err := prepareReferenceModule(testCase.root, "input.txt")
		if err != nil || failure != "" {
			continue
		}

		single, singleError := checker.Check(program)
		two, twoError := checker.CheckModules([]*parser.Module{
			{Path: "input.txt", ID: 0, Program: program},
			{Path: "input2.txt", ID: 1, Program: emptyProgram(t)},
		})

		if (singleError == nil) != (twoError == nil) {
			t.Errorf("%s: single-module check %v but two-module check %v", testCase.name, singleError, twoError)
			continue
		}
		if singleError != nil {
			var first, second *checker.Error
			if !errors.As(singleError, &first) || !errors.As(twoError, &second) {
				continue
			}
			if first.Kind != second.Kind || first.Line != second.Line || first.Column != second.Column || first.Message != second.Message {
				t.Errorf("%s: single-module error %v differs from two-module error %v", testCase.name, first, second)
			}
			checked++
			continue
		}
		if single == nil || two == nil {
			t.Errorf("%s: one check returned no result", testCase.name)
			continue
		}
		checked++
	}
	if checked < 100 {
		t.Fatalf("only %d cases were comparable", checked)
	}
	t.Logf("empty second module is inert on %d inputs", checked)
}

// emptyProgram parses an empty source and returns its program.
func emptyProgram(t *testing.T) *parser.Program {
	t.Helper()
	tokens, err := lexer.Lex("")
	if err != nil {
		t.Fatalf("lexing the empty program: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parsing the empty program: %v", err)
	}
	return program
}
