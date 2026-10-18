package main

// Differential testing for the Kitten-written parser in selfhost/parser.
//
// This file lives in the CLI package because the harness needs the same build
// pipeline and toolchain probe as the other integration tests: it compiles
// selfhost/parse.ktn with the bootstrap compiler and runs the resulting
// executable once per corpus input.
//
// The reference is the Go parser in kitten/parser. Both implementations print
// the same canonical rendering of the AST - the format documented in
// selfhost/parser/printer.ktn - and the harness compares the two renderings byte
// for byte, so the comparison is over normalized tree structure rather than over
// implementation-specific formatting: node kinds, positions, names, operators,
// literal text, and nesting are all covered, and an error is compared as a
// single "error <line> <column> <message>" line.
//
// Two differences are expected and are handled here rather than hidden:
//
//   - Position columns. The Go lexer counts columns in code points and the
//     Kitten lexer counts bytes, so a position after a byte >= 128 can differ.
//     The harness therefore requires exact equality only for input whose bytes
//     are all ASCII; for other input it still compares the rendered tree shape
//     after marking the difference, and the corpus records which inputs those
//     are.
//   - Error messages that quote non-ASCII token text, where Go's %q prints a
//     code point and the Kitten parser prints \xNN for the bytes.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"kitten/lexer"
	"kitten/parser"
)

// kittenParserProgram is the driver program of the Kitten-written parser.
func kittenParserProgram() string {
	return filepath.Join("..", "..", "selfhost", "parse.ktn")
}

// buildKittenParser compiles the Kitten parser and returns the executable path.
func buildKittenParser(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-parser")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenParserProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the Kitten parser: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the Kitten parser:\n%s", warnings)
	}
	return executable
}

// runKittenParser writes source to input.txt in directory and runs the Kitten
// parser there, returning its stdout.
func runKittenParser(executable, directory, source string) (string, error) {
	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte(source), 0o644); err != nil {
		return "", fmt.Errorf("writing input.txt: %v", err)
	}

	command := exec.Command(executable)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("running the Kitten parser: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return "", fmt.Errorf("the Kitten parser wrote to stderr: %s", stderr.String())
	}
	return stdout.String(), nil
}

// renderReference lexes and parses source with the Go reference implementation
// and renders the same canonical text the Kitten parser produces.
func renderReference(source string) (string, error) {
	tokens, err := lexer.Lex(source)
	if err != nil {
		var lexError *lexer.Error
		if !asLexerError(err, &lexError) {
			return "", fmt.Errorf("the reference lexer returned %T: %v", err, err)
		}
		return errorLine(lexError.Line, lexError.Column, lexError.Message), nil
	}

	program, err := parser.Parse(tokens)
	if err != nil {
		var parseError *parser.Error
		if !asParserError(err, &parseError) {
			return "", fmt.Errorf("the reference parser returned %T: %v", err, err)
		}
		return errorLine(parseError.Line, parseError.Column, parseError.Message), nil
	}

	printer := &astPrinter{}
	printer.program(program)
	return printer.builder.String(), nil
}

// asLexerError and asParserError avoid importing errors for two small checks.
func asLexerError(err error, target **lexer.Error) bool {
	converted, ok := err.(*lexer.Error)
	if ok {
		*target = converted
	}
	return ok
}

func asParserError(err error, target **parser.Error) bool {
	converted, ok := err.(*parser.Error)
	if ok {
		*target = converted
	}
	return ok
}

// errorLine renders a diagnostic in the driver's format.
func errorLine(line, column int, message string) string {
	return fmt.Sprintf("error %d %d %s\n", line, column, message)
}

// astPrinter renders the reference AST in the canonical format.
//
// Every line is an indent of two spaces per depth level followed by
// space-separated fields, matching printer.ktn: the Kitten printer builds the
// same string by printing the indented kind first and the remaining fields
// after it, so a line whose last field is empty keeps its trailing space in both
// implementations.
type astPrinter struct {
	builder strings.Builder
}

func (p *astPrinter) line(depth int, fields ...string) {
	p.builder.WriteString(strings.Repeat("  ", depth))
	p.builder.WriteString(strings.Join(fields, " "))
	p.builder.WriteByte('\n')
}

func (p *astPrinter) program(program *parser.Program) {
	p.line(0, "program")
	for _, declaration := range program.Declarations {
		p.declaration(declaration, 1)
	}
}

func (p *astPrinter) declaration(declaration parser.Decl, depth int) {
	switch node := declaration.(type) {
	case *parser.ImportDecl:
		p.line(depth, "import", number(node.At.Line), number(node.At.Column), node.Path)

	case *parser.FunctionDecl:
		p.line(depth, "fn", number(node.At.Line), number(node.At.Column), node.Visibility.String(), node.Name)
		for _, parameter := range node.Parameters {
			p.line(depth+1, "param", number(parameter.At.Line), number(parameter.At.Column), parameter.Name)
			p.typeRef(parameter.Type, depth+2)
		}
		if node.ReturnType == nil {
			p.line(depth+1, "ret-none")
		} else {
			p.line(depth+1, "ret", number(node.ReturnType.At.Line), number(node.ReturnType.At.Column))
			p.typeRef(node.ReturnType, depth+2)
		}
		p.statement(node.Body, depth+1)

	case *parser.StructDecl:
		p.line(depth, "struct", number(node.At.Line), number(node.At.Column), node.Visibility.String(), node.Name)
		for _, field := range node.Fields {
			p.field(field, depth+1)
		}

	case *parser.EnumDecl:
		p.line(depth, "enum", number(node.At.Line), number(node.At.Column), node.Visibility.String(), node.Name)
		for _, enumCase := range node.Cases {
			p.line(depth+1, "case", number(enumCase.At.Line), number(enumCase.At.Column), enumCase.Name)
		}

	case *parser.UnionDecl:
		p.line(depth, "union", number(node.At.Line), number(node.At.Column), node.Visibility.String(), node.Name)
		for _, variant := range node.Variants {
			p.line(depth+1, "variant", number(variant.At.Line), number(variant.At.Column), variant.Name)
			for _, field := range variant.Fields {
				p.field(field, depth+2)
			}
		}
	}
}

func (p *astPrinter) field(field *parser.Field, depth int) {
	p.line(depth, "field", number(field.At.Line), number(field.At.Column), field.Name)
	p.typeRef(field.Type, depth+1)
}

func (p *astPrinter) typeRef(reference *parser.TypeRef, depth int) {
	position := reference.Pos()
	switch {
	case reference.MapValue != nil:
		p.line(depth, "type-map", number(position.Line), number(position.Column))
		p.typeRef(reference.MapKey, depth+1)
		p.typeRef(reference.MapValue, depth+1)
	case reference.Element != nil:
		p.line(depth, "type-array", number(position.Line), number(position.Column))
		p.typeRef(reference.Element, depth+1)
	default:
		p.line(depth, "type-name", number(position.Line), number(position.Column), reference.Name)
	}
}

func (p *astPrinter) statement(statement parser.Stmt, depth int) {
	switch node := statement.(type) {
	case *parser.BlockStmt:
		p.line(depth, "block", number(node.At.Line), number(node.At.Column))
		for _, inner := range node.Statements {
			p.statement(inner, depth+1)
		}

	case *parser.VarDeclStmt:
		p.line(depth, "var", number(node.At.Line), number(node.At.Column), node.Name)
		p.typeRef(node.Type, depth+1)
		p.optionalExpression(node.Initializer, depth+1)

	case *parser.ExprStmt:
		p.line(depth, "expr-stmt", number(node.At.Line), number(node.At.Column))
		p.expression(node.Expression, depth+1)

	case *parser.IfStmt:
		p.line(depth, "if", number(node.At.Line), number(node.At.Column))
		p.expression(node.Condition, depth+1)
		p.statement(node.Then, depth+1)
		if node.Else == nil {
			p.line(depth+1, "absent")
		} else {
			p.line(depth+1, "present")
			p.statement(node.Else, depth+2)
		}

	case *parser.WhileStmt:
		p.line(depth, "while", number(node.At.Line), number(node.At.Column))
		p.expression(node.Condition, depth+1)
		p.statement(node.Body, depth+1)

	case *parser.ForStmt:
		p.line(depth, "for", number(node.At.Line), number(node.At.Column))
		if node.Init == nil {
			p.line(depth+1, "absent")
		} else {
			p.line(depth+1, "present")
			p.statement(node.Init, depth+2)
		}
		p.optionalExpression(node.Condition, depth+1)
		p.optionalExpression(node.Increment, depth+1)
		p.statement(node.Body, depth+1)

	case *parser.ReturnStmt:
		p.line(depth, "return", number(node.At.Line), number(node.At.Column))
		p.optionalExpression(node.Value, depth+1)

	case *parser.MatchStmt:
		p.line(depth, "match", number(node.At.Line), number(node.At.Column))
		p.expression(node.Scrutinee, depth+1)
		for _, arm := range node.Arms {
			p.line(depth+1, "arm", number(arm.At.Line), number(arm.At.Column), arm.Variant, number(len(arm.Bindings)))
			for index, binding := range arm.Bindings {
				position := arm.BindingsAt[index]
				p.line(depth+2, "binding", number(position.Line), number(position.Column), binding)
			}
			p.statement(arm.Body, depth+2)
		}
	}
}

func (p *astPrinter) optionalExpression(expression parser.Expr, depth int) {
	if expression == nil {
		p.line(depth, "absent")
		return
	}
	p.line(depth, "present")
	p.expression(expression, depth+1)
}

func (p *astPrinter) expression(expression parser.Expr, depth int) {
	switch node := expression.(type) {
	case *parser.Identifier:
		p.line(depth, "ident", number(node.At.Line), number(node.At.Column), node.Name)

	case *parser.IntLiteral:
		p.line(depth, "int", number(node.At.Line), number(node.At.Column), node.Text)

	case *parser.FloatLiteral:
		p.line(depth, "float", number(node.At.Line), number(node.At.Column), node.Text)

	case *parser.StringLiteral:
		p.line(depth, "string", number(node.At.Line), number(node.At.Column), node.Text)

	case *parser.BoolLiteral:
		p.line(depth, "bool", number(node.At.Line), number(node.At.Column), strconv.FormatBool(node.Value))

	case *parser.UnaryExpr:
		position := node.Pos()
		fixity := "prefix"
		if node.Postfix {
			fixity = "postfix"
		}
		p.line(depth, "unary", number(position.Line), number(position.Column),
			number(node.OperatorAt.Line), number(node.OperatorAt.Column), node.Operator, fixity)
		p.expression(node.Operand, depth+1)

	case *parser.BinaryExpr:
		position := node.Pos()
		p.line(depth, "binary", number(position.Line), number(position.Column),
			number(node.OperatorAt.Line), number(node.OperatorAt.Column), node.Operator)
		p.expression(node.Left, depth+1)
		p.expression(node.Right, depth+1)

	case *parser.AssignExpr:
		position := node.Pos()
		p.line(depth, "assign", number(position.Line), number(position.Column),
			number(node.OperatorAt.Line), number(node.OperatorAt.Column), node.Operator)
		p.expression(node.Target, depth+1)
		p.expression(node.Value, depth+1)

	case *parser.CallExpr:
		position := node.Pos()
		p.line(depth, "call", number(position.Line), number(position.Column),
			number(node.OpenParen.Line), number(node.OpenParen.Column))
		p.expression(node.Callee, depth+1)
		for _, argument := range node.Arguments {
			p.expression(argument, depth+1)
		}

	case *parser.MemberExpr:
		position := node.Pos()
		p.line(depth, "member", number(position.Line), number(position.Column),
			number(node.Dot.Line), number(node.Dot.Column), node.Member)
		p.expression(node.Receiver, depth+1)

	case *parser.StructLiteral:
		position := node.Pos()
		p.line(depth, "struct-literal", number(position.Line), number(position.Column),
			number(node.OpenBrace.Line), number(node.OpenBrace.Column))
		p.typeRef(node.Type, depth+1)
		for _, field := range node.Fields {
			p.fieldInit(field, depth+1)
		}

	case *parser.VariantLiteral:
		position := node.Pos()
		p.line(depth, "variant-literal", number(position.Line), number(position.Column),
			number(node.OpenBrace.Line), number(node.OpenBrace.Column), node.Variant)
		p.typeRef(node.Type, depth+1)
		for _, field := range node.Fields {
			p.fieldInit(field, depth+1)
		}

	case *parser.NewArrayExpr:
		p.line(depth, "new-array", number(node.At.Line), number(node.At.Column))
		p.typeRef(node.Element, depth+1)
		p.expression(node.Length, depth+1)

	case *parser.NewMapExpr:
		p.line(depth, "new-map", number(node.At.Line), number(node.At.Column))
		p.typeRef(node.Map, depth+1)

	case *parser.IndexExpr:
		position := node.Pos()
		p.line(depth, "index", number(position.Line), number(position.Column),
			number(node.At.Line), number(node.At.Column))
		p.expression(node.Target, depth+1)
		p.expression(node.Index, depth+1)
	}
}

func (p *astPrinter) fieldInit(field *parser.FieldInit, depth int) {
	p.line(depth, "field-init", number(field.At.Line), number(field.At.Column), field.Name)
	p.expression(field.Value, depth+1)
}

// number renders an integer for the canonical format.
func number(value int) string {
	return strconv.Itoa(value)
}

// parserCase is one differential test input.
type parserCase struct {
	name   string
	source string
}

// parserCorpus returns every input used for parser differential testing.
func parserCorpus(t *testing.T) []parserCase {
	t.Helper()

	var cases []parserCase
	add := func(name, source string) {
		cases = append(cases, parserCase{name: name, source: source})
	}

	// Empty input and a minimal program.
	add("empty input", "")
	add("empty program with newline", "\n")
	add("comment only", "// nothing here\n")
	add("minimal main", "fn main() {}\n")

	// Visibility modifiers.
	for _, visibility := range []string{"", "public ", "private "} {
		add("function visibility "+visibility, visibility+"fn f() {}\n")
		add("struct visibility "+visibility, visibility+"struct S {}\n")
		add("enum visibility "+visibility, visibility+"enum E {}\n")
		add("union visibility "+visibility, visibility+"union U {}\n")
	}

	// Imports.
	add("import", "import \"geometry.ktn\";\n")
	add("import with path segments", "import \"util/text.ktn\";\n")
	add("import empty path", "import \"\";\n")
	add("import with escape", "import \"a\\tb.ktn\";\n")
	add("import before function", "import \"a.ktn\";\nimport \"b.ktn\";\nfn main() {}\n")
	add("import with visibility", "public import \"a.ktn\";\n")

	// Struct declarations.
	add("empty struct", "struct S {}\n")
	add("struct fields", "struct S {\n    int a;\n    float b;\n    bool c;\n    string d;\n}\n")
	add("struct with collections", "struct S {\n    int[] a;\n    int[][] b;\n    map[string]int c;\n    map[string]int[] d;\n    S[] e;\n}\n")
	add("struct with trailing content", "struct S { int a; }\nfn main() {}\n")

	// Enum declarations.
	add("empty enum", "enum E {}\n")
	add("single case enum", "enum E { A }\n")
	add("enum cases", "enum E {\n    A,\n    B,\n    C\n}\n")
	add("enum one line", "enum E { A, B }\n")

	// Union declarations.
	add("empty union", "union U {}\n")
	add("payloadless union", "union U {\n    A\n    B\n}\n")
	add("union with payloads", "union U {\n    A { int x; }\n    B { string s; U nested; }\n    C\n}\n")
	add("union recursive", "union Expression {\n    IntegerLiteral { int value; }\n    Add { Expression left; Expression right; }\n    Missing\n}\n")

	// Function signatures.
	add("function parameters", "fn f(int a, float b, bool c, byte d) {}\n")
	add("function return types", "fn f() -> int {}\nfn g() -> void {}\nfn h() -> string {}\nfn i() -> int[] {}\nfn j() -> map[string]int {}\nfn k() -> byte {}\nfn l() -> byte[] {}\n")
	add("function with everything", "public fn f(int[] a, map[string]int b) -> int[][] {}\n")
	add("function parameter types", "fn f(S s, E e, int[] a, map[string]S m, byte b, byte[] bytes) {}\n")
	add("byte in every type position", "struct B { byte value; byte[] bytes; }\nunion U { B { byte value; byte[] bytes; } }\nfn f(byte a) -> byte { return a; }\nfn main() { byte x = byte_from_int(1); byte[] ys = new byte[2]; byte[][] grid = new byte[][1]; map[string]byte m = new map[string]byte; }\n")

	// Type references on their own, through variable declarations.
	types := []string{"int", "float", "bool", "string", "void", "byte", "S", "int[]", "int[][]", "int[][][]", "byte[]", "byte[][]", "map[string]int", "map[string]byte", "map[string]int[]", "map[int]string", "map[string]map[string]int", "S[]"}
	for _, typeName := range types {
		add("type "+typeName, "fn main() {\n    "+typeName+" value;\n}\n")
	}

	// Variable declarations and expression statements.
	add("var without initializer", "fn main() {\n    int x;\n}\n")
	add("var with initializer", "fn main() {\n    int x = 1;\n}\n")
	add("many vars", "fn main() {\n    int a = 1;\n    string b = \"two\";\n    bool c = true;\n    S d = S { f = 1 };\n}\n")
	add("expression statement", "fn main() {\n    call();\n    x = 1;\n    x += 1;\n    x -= 1;\n    x++;\n    x--;\n}\n")
	add("nested blocks", "fn main() {\n    {\n        {\n            int x = 1;\n        }\n    }\n}\n")

	// Control flow.
	add("if without else", "fn main() {\n    if (a) {\n        b();\n    }\n}\n")
	add("if else", "fn main() {\n    if (a) {\n        b();\n    } else {\n        c();\n    }\n}\n")
	add("else if chain", "fn main() {\n    if (a) {\n        b();\n    } else if (c) {\n        d();\n    } else if (e) {\n        f();\n    } else {\n        g();\n    }\n}\n")
	add("while", "fn main() {\n    while (a < b) {\n        a++;\n    }\n}\n")
	add("for full", "fn main() {\n    for (int i = 0; i < 10; i++) {\n        print(i);\n    }\n}\n")
	add("for empty condition and increment", "fn main() {\n    for (int i = 0; ; ) {\n    }\n}\n")
	add("for empty initializer", "fn main() {\n    for (; i < 10; i++) {\n    }\n}\n")
	add("for totally empty", "fn main() {\n    for (;;) {\n    }\n}\n")
	add("for with expression initializer", "fn main() {\n    for (i = 0; i < 10; i++) {\n    }\n}\n")
	add("for with declaration initializer", "fn main() {\n    for (int[] i = new int[1]; ; ) {\n    }\n}\n")
	add("return bare", "fn f() {\n    return;\n}\n")
	add("return value", "fn f() -> int {\n    return 1;\n}\n")
	add("return expression", "fn f() -> int {\n    return a + b * c;\n}\n")

	// Match statements.
	add("match no bindings", "fn f(E e) -> int {\n    match (e) {\n        A { return 1; }\n        B { return 2; }\n    }\n}\n")
	add("match with bindings", "fn f(U u) -> int {\n    match (u) {\n        Full(value, label) { return value; }\n        Empty { return 0; }\n    }\n}\n")
	add("match empty bindings", "fn f(U u) {\n    match (u) {\n        Full() {\n        }\n    }\n}\n")
	add("match nested", "fn f(U u) {\n    match (u) {\n        A(x) {\n            match (x) {\n                B(y) {\n                }\n                C {\n                }\n            }\n        }\n    }\n}\n")

	// Expressions: literals.
	add("literals", "fn main() {\n    x = 0;\n    x = 1234567890;\n    x = 0x1F;\n    x = 0b101;\n    x = 1.5;\n    x = 0.25;\n    x = \"text\";\n    x = \"\";\n    x = \"a\\nb\\tc\\\"d\\\\e\";\n    x = true;\n    x = false;\n}\n")
	add("prefixed literal boundaries", "fn main() {\n    x = 0xFFFFFFFFFFFFFFFF;\n    x = 0x8000000000000000;\n    x = 0b"+strings.Repeat("1", 64)+";\n}\n")
	add("parenthesized", "fn main() {\n    x = ((1));\n    x = (a + b) * c;\n}\n")

	// Expressions: unary and postfix.
	add("prefix operators", "fn main() {\n    a = -b;\n    a = !b;\n    a = ~b;\n    a = ++b;\n    a = --b;\n    a = - -b;\n    a = !!b;\n    a = -~b;\n}\n")
	add("postfix operators", "fn main() {\n    a = b++;\n    a = b--;\n    a = b++ + c--;\n}\n")
	add("mixed fixity", "fn main() {\n    a = -b++;\n    a = ++b--;\n}\n")

	// Expressions: every binary operator.
	binaryOperators := []string{"+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "&&", "||", "&", "|", "^", "<<", ">>"}
	for _, operator := range binaryOperators {
		add("binary "+operator, "fn main() {\n    a = b "+operator+" c;\n}\n")
		add("binary "+operator+" chained", "fn main() {\n    a = b "+operator+" c "+operator+" d;\n}\n")
	}
	for _, operator := range []string{"=", "+=", "-="} {
		add("assignment "+operator, "fn main() {\n    a "+operator+" b;\n}\n")
		add("assignment "+operator+" chained", "fn main() {\n    a "+operator+" b "+operator+" c;\n}\n")
	}

	// Precedence and associativity boundaries.
	precedenceCases := []string{
		"a = b = c",
		"a + b * c",
		"a * b + c",
		"a - b - c",
		"a / b / c",
		"a % b * c",
		"a + b - c + d",
		"a < b == c",
		"a == b != c",
		"a < b && c > d",
		"a && b || c && d",
		"a || b && c",
		"a == b && c != d || e < f",
		"a + b < c + d",
		"!a && b",
		"-a * b",
		"a = b + c * d - e / f % g",
		"a || b || c",
		"a && b && c",
		"a = b || c && d",
		"x = -y + z",
		"x = a = b + c",
		"a & b | c ^ d",
		"a | b ^ c & d",
		"a << 1 + 2",
		"a & b == c",
		"~a + 1",
		"-~a",
		"a << b << c",
		"1 < 2 << 3",
		"a + (b << c)",
		"(a + b) << c",
	}
	for _, expression := range precedenceCases {
		add("precedence "+expression, "fn main() {\n    "+expression+";\n}\n")
	}

	// Expressions: calls.
	add("call no arguments", "fn main() {\n    f();\n}\n")
	add("call one argument", "fn main() {\n    f(1);\n}\n")
	add("call many arguments", "fn main() {\n    f(1, 2.5, \"three\", true, x + y);\n}\n")
	add("call nested", "fn main() {\n    f(g(h(1)), i(), j(k(2, 3)));\n}\n")
	add("call on member", "fn main() {\n    a.b.c(1);\n}\n")
	add("call on index", "fn main() {\n    a[0](1);\n}\n")
	add("call then member then call", "fn main() {\n    f(1).g[2](3).h;\n}\n")

	// Expressions: member access and indexing.
	add("member chain", "fn main() {\n    a = b.c.d.e;\n}\n")
	add("index", "fn main() {\n    a = b[0];\n}\n")
	add("index nested", "fn main() {\n    a = b[0][1][2];\n}\n")
	add("index with expression", "fn main() {\n    a = b[i + 1];\n}\n")
	add("index then member", "fn main() {\n    a = b[0].c;\n}\n")
	add("member then index", "fn main() {\n    a = b.c[0];\n}\n")
	add("index assignment", "fn main() {\n    b[0] = 1;\n    b[0] += 1;\n    b[0]++;\n}\n")

	// Expressions: literals for structs, variants, arrays, and maps.
	add("struct literal empty", "fn main() {\n    a = S {};\n}\n")
	add("struct literal one field", "fn main() {\n    a = S { f = 1 };\n}\n")
	add("struct literal many fields", "fn main() {\n    a = S { x = 1, y = 2.5, name = \"n\" };\n}\n")
	add("struct literal nested", "fn main() {\n    a = S { inner = S { x = 1 }, list = new int[2] };\n}\n")
	add("variant literal", "fn main() {\n    a = U.Full { value = 1, label = \"x\" };\n}\n")
	add("variant literal nested", "fn main() {\n    a = U.Outer { inner = U.Inner { v = 1 }, other = U.Empty };\n}\n")
	add("variant and member together", "fn main() {\n    a = x.y;\n    b = U.V { f = 1 };\n}\n")
	add("array creation", "fn main() {\n    a = new int[3];\n    b = new int[][2];\n    c = new S[n + 1];\n    d = new byte[3];\n    e = new byte[][2];\n}\n")
	add("map creation", "fn main() {\n    a = new map[string]int;\n    b = new map[string]int[];\n    c = new map[string]map[string]int;\n}\n")
	add("collection operations", "fn main() {\n    a = new map[string]int;\n    a[\"k\"] = 1;\n    print(length(a), has(a, \"k\"));\n    print(substring(\"abc\", 0, 1));\n}\n")

	// Deep nesting, to exercise recursion and bounded lookahead.
	add("deep parentheses", "fn main() {\n    x = "+strings.Repeat("(", 40)+"1"+strings.Repeat(")", 40)+";\n}\n")
	add("deep unary", "fn main() {\n    x = "+strings.Repeat("-", 40)+"1;\n}\n")
	add("long additive chain", "fn main() {\n    x = "+strings.Repeat("a + ", 60)+"a;\n}\n")
	add("deep calls", "fn main() {\n    x = "+strings.Repeat("f(", 30)+"1"+strings.Repeat(")", 30)+";\n}\n")
	add("deep member access", "fn main() {\n    x = a"+strings.Repeat(".f", 40)+";\n}\n")
	add("deep blocks", "fn main() {\n"+strings.Repeat("{\n", 30)+strings.Repeat("}\n", 30)+"}\n")
	add("many statements", "fn main() {\n"+strings.Repeat("    x = 1;\n", 200)+"}\n")
	add("many declarations", strings.Repeat("fn f() {}\n", 100))

	// Malformed input, truncated declarations, and missing delimiters.
	malformed := []string{
		"fn ",
		"fn f",
		"fn f(",
		"fn f()",
		"fn f() {",
		"fn f(int a",
		"fn f(int a,",
		"fn f(int a,) {}",
		"fn f(int) {}",
		"fn f(int a int b) {}",
		"fn f() -> {}",
		"fn f() -> int",
		"fn 1() {}",
		"fn f() { int x }",
		"fn f() { int byte = 1; }",
		"fn f() { int x = }",
		"fn f() { int }",
		"fn f() { return }",
		"fn f() { if }",
		"fn f() { if ( }",
		"fn f() { if (a) }",
		"fn f() { if (a) { } else }",
		"fn f() { while (a }",
		"fn f() { for (; ; }",
		"fn f() { for (int i = 0; i < 1; i++ }",
		"fn f() { match (x) { A } }",
		"fn f() { match (x) { A(1) { } } }",
		"fn f() { match (x) { A(a,) { } } }",
		"fn f() { match (x) }",
		"fn f() { f(1,) }",
		"fn f() { a.b. }",
		"fn f() { a[0 }",
		"fn f() { new int }",
		"fn f() { new int[x }",
		"fn f() { new map[int }",
		"fn f() { S { a = } }",
		"fn f() { S { a = 1, } }",
		"fn f() { S { a 1 } }",
		"struct",
		"struct S",
		"struct S {",
		"struct S { int }",
		"struct S { int a }",
		"enum",
		"enum E {",
		"enum E { A, }",
		"enum E { A B }",
		"enum E { 1 }",
		"union",
		"union U {",
		"union U { A { int x } }",
		"union U { A { } B }",
		"union U { 1 }",
		"import",
		"import \"a\"",
		"import a;",
		"public import \"a\";",
		"private import \"a\";",
		"}",
		"@",
		"fn f() { x = \"unterminated\n}",
		"fn f() { x = @ }",
		"fn f() { x = ; }",
		"fn f() { x == ; }",
		"fn f() { ; }",
		"fn f() { x = 1 + }",
		"fn f() { x = * 1 }",
		"fn f() { x = a b }",
		"fn f() { x: 1 }",
		"fn f() { break; }",
		"fn f() { continue; }",
		"fn f() { switch (x) { } }",
	}
	for index, source := range malformed {
		add("malformed "+strconv.Itoa(index+1)+" "+strconv.Quote(source), source)
	}

	// Non-ASCII input, valid and invalid as UTF-8.
	nonASCII := []string{
		"fn café() {}\n",
		"fn main() {\n    string s = \"café\";\n}\n",
		"fn main() {\n    // comment with 日本語\n    int x = 1;\n}\n",
		"fn main() {\n    string s = \"日本\";\n    print(s);\n}\n",
		"fn main() {\n    x = 1;\n}\né",
		"fn main() {\n    string s = \"a\xffb\";\n}\n",
		"\xff",
	}
	for _, source := range nonASCII {
		add("non-ASCII "+strconv.Quote(source), source)
	}

	// Every fixture and self-hosted source, which are the most realistic inputs
	// available: the parser is checked on the compiler's own test programs and on
	// its own source.
	inputs, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.ktn"))
	if err != nil {
		t.Fatalf("globbing fixtures: %v", err)
	}
	inputs = append(inputs,
		filepath.Join("..", "..", "tests", "modules", "main.ktn"),
		filepath.Join("..", "..", "tests", "modules", "geometry.ktn"),
		filepath.Join("..", "..", "tests", "modules", "util", "text.ktn"),
		filepath.Join("..", "..", "selfhost", "parse.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "tokens.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "lexer.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "main.ktn"),
		filepath.Join("..", "..", "selfhost", "parser", "ast.ktn"),
		filepath.Join("..", "..", "selfhost", "parser", "parser.ktn"),
		filepath.Join("..", "..", "selfhost", "parser", "printer.ktn"),
	)
	sort.Strings(inputs)
	for _, input := range inputs {
		content, err := os.ReadFile(input)
		if err != nil {
			t.Fatalf("reading %s: %v", input, err)
		}
		add("source "+filepath.Base(input), string(content))
	}

	if len(cases) < 250 {
		t.Fatalf("the parser corpus is unexpectedly small: %d inputs", len(cases))
	}
	return cases
}

// describeParserCase renders a case for a failure message, truncating long
// sources.
func describeParserCase(testCase parserCase) string {
	source := testCase.source
	const limit = 160
	if len(source) > limit {
		source = source[:limit] + "..."
	}
	return fmt.Sprintf("%s %q", testCase.name, source)
}

// errorPosition extracts the position from an "error <line> <column> <message>"
// rendering, reporting whether the rendering is an error line at all.
func errorPosition(rendering string) (line, column int, ok bool) {
	trimmed := strings.TrimSuffix(rendering, "\n")
	fields := strings.SplitN(trimmed, " ", 4)
	if len(fields) != 4 || fields[0] != "error" {
		return 0, 0, false
	}
	lineValue, lineError := strconv.Atoi(fields[1])
	columnValue, columnError := strconv.Atoi(fields[2])
	if lineError != nil || columnError != nil {
		return 0, 0, false
	}
	return lineValue, columnValue, true
}

// kindOutline reduces a rendering to its tree shape: the node kind of every
// line together with its depth. Positions, names, operators, and literal text
// are dropped.
//
// It is used for input containing bytes >= 128, where byte-oriented column
// counting and the reference lexer's UTF-8 substitution can change a position or
// a quoted lexeme. The outline still catches any difference in which nodes were
// produced and how they nest.
func kindOutline(rendering string) string {
	var builder strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(rendering, "\n"), "\n") {
		if line == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		kind := strings.Fields(line)[0]
		builder.WriteString(strconv.Itoa(indent / 2))
		builder.WriteByte(':')
		builder.WriteString(kind)
		builder.WriteByte('\n')
	}
	return builder.String()
}

// compareNonASCII compares a reference rendering with the Kitten rendering for
// input that contains bytes >= 128, accepting only the documented differences: a
// byte-oriented column, a quoted lexeme that differs because the reference lexer
// decodes UTF-8, and an error message that quotes such a lexeme.
func compareNonASCII(t *testing.T, testCase parserCase, want, got string) {
	t.Helper()

	wantLine, wantColumn, wantIsError := errorPosition(want)
	gotLine, gotColumn, gotIsError := errorPosition(got)
	if wantIsError || gotIsError {
		if !wantIsError || !gotIsError {
			t.Errorf("only one implementation reported an error for %s\n--- reference ---\n%s--- Kitten ---\n%s",
				describeParserCase(testCase), want, got)
			return
		}
		if wantLine != gotLine || wantColumn != gotColumn {
			t.Errorf("error position %d:%d, want %d:%d for %s", gotLine, gotColumn, wantLine, wantColumn, describeParserCase(testCase))
		}
		return
	}

	if wantOutline, gotOutline := kindOutline(want), kindOutline(got); wantOutline != gotOutline {
		t.Errorf("AST shape mismatch for %s\n--- reference ---\n%s--- Kitten ---\n%s",
			describeParserCase(testCase), wantOutline, gotOutline)
	}
}

// TestKittenParserOutputFormat pins the driver's output format with explicit
// expected text, so that a change to the format is caught even if both
// implementations change together. The expectations are the reference
// implementation's rendering of the same inputs, which the differential test
// above also checks.
func TestKittenParserOutputFormat(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenParser(t)
	directory := t.TempDir()

	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "function",
			source: "fn f(int a) -> int {\n    return a;\n}\n",
			want: strings.Join([]string{
				"program",
				"  fn 1 1 default f",
				"    param 1 6 a",
				"      type-name 1 6 int",
				"    ret 1 16",
				"      type-name 1 16 int",
				"    block 1 20",
				"      return 2 5",
				"        present",
				"          ident 2 12 a",
				"",
			}, "\n"),
		},
		{
			name:   "syntax error",
			source: "fn f( {\n",
			want:   "error 1 7 expected a type, found LBrace \"{\"\n",
		},
		{
			name:   "lexical error",
			source: "\"unterminated\n",
			want:   "error 1 14 newline in string literal\n",
		},
		{
			name:   "empty input",
			source: "",
			want:   "program\n",
		},
	}

	for _, testCase := range cases {
		got, err := runKittenParser(executable, directory, testCase.source)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Errorf("%s: output = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// TestKittenParserScaling measures the Kitten parser on growing inputs, and
// checks that the largest one parses successfully.
//
// The measurements are logged rather than asserted: they document that the AST
// lists are sized by the construct being parsed rather than by the whole file,
// which keeps parsing roughly linear. Run it with -v to see the numbers.
func TestKittenParserScaling(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenParser(t)
	directory := t.TempDir()
	unit := "fn name(int a, int b) -> int {\n    int x = a + b * 2;\n    while (x > 0) {\n        x--;\n    }\n    return x;\n}\n"

	previous := time.Duration(0)
	for _, count := range []int{100, 1000, 4000} {
		source := strings.Repeat(unit, count)

		best := time.Duration(0)
		for repetition := 0; repetition < 3; repetition++ {
			start := time.Now()
			if _, err := runKittenParser(executable, directory, source); err != nil {
				t.Fatalf("functions %d: %v", count, err)
			}
			if elapsed := time.Since(start); best == 0 || elapsed < best {
				best = elapsed
			}
		}

		want, err := renderReference(source)
		if err != nil {
			t.Fatalf("functions %d: %v", count, err)
		}
		got, err := runKittenParser(executable, directory, source)
		if err != nil {
			t.Fatalf("functions %d: %v", count, err)
		}
		if got != want {
			t.Fatalf("functions %d: the renderings differ", count)
		}

		growth := ""
		if previous > 0 {
			growth = fmt.Sprintf(", x%.1f", float64(best)/float64(previous))
		}
		t.Logf("functions=%5d bytes=%7d lines=%7d  Kitten=%10s%s",
			count, len(source), strings.Count(got, "\n"), best.Round(time.Millisecond), growth)
		previous = best
	}
}

// TestKittenParserMatchesReference runs the whole corpus through both
// implementations and compares their canonical renderings.
func TestKittenParserMatchesReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenParser(t)
	directory := t.TempDir()
	cases := parserCorpus(t)
	t.Logf("parser differential corpus: %d inputs", len(cases))

	exact := 0
	loose := 0
	for _, testCase := range cases {
		want, err := renderReference(testCase.source)
		if err != nil {
			t.Errorf("%s: %v", describeParserCase(testCase), err)
			continue
		}

		got, err := runKittenParser(executable, directory, testCase.source)
		if err != nil {
			t.Errorf("%s: %v", describeParserCase(testCase), err)
			continue
		}

		if got == want {
			exact++
			continue
		}

		// A difference is only expected for input containing a byte >= 128, where
		// byte-oriented column counting and the reference lexer's UTF-8 handling
		// can change a position or a quoted lexeme.
		if !isASCII(testCase.source) {
			loose++
			compareNonASCII(t, testCase, want, got)
			continue
		}

		t.Errorf("AST mismatch for %s\n--- reference ---\n%s--- Kitten ---\n%s", describeParserCase(testCase), want, got)
	}

	t.Logf("parser corpus: %d inputs, %d byte-identical, %d compared by AST shape only (non-ASCII)", len(cases), exact, loose)
}
