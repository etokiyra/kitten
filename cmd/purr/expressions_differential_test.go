package main

// Differential testing for the Kitten-written core expression emitter
// (selfhost/codegen/expressions.ktn, driven by selfhost/expressions.ktn).
//
// The probe prints one line per expression node, in the order the reference
// generator renders bodies. The harness walks the same AST in that same order and
// pairs the two lists line by line, so a probe that visited the wrong node, in the
// wrong order, or reported the wrong position or kind cannot pass.
//
// A node's rendering is compared when the reference's own output contains it in an
// extractable position. The context used here is a print argument: the reference
// expands print into a comma expression whose every argument sits inside a runtime
// helper call, so an argument is the balanced parenthesized text after a print
// helper's opening parenthesis, taken in emission order. That extraction depends
// on neither the position nor the shape of the expression, and no rendering is
// re-derived from the reference's logic.
//
// A kind this phase does not implement is compared against the emitter's own
// failure message instead of a rendering, so a missing implementation can never be
// mistaken for a rendered expression. Every other node is checked for its kind and
// position; its text is not compared, because the reference emits only the text of
// the outermost expression of a context. A composite expression that is compared
// as a whole still validates its subexpressions, and each subexpression form is
// additionally printed on its own somewhere in the targeted corpus.
//
// The corpus is the checker corpus, so the probe is exercised over every construct
// the language has, including the expression kinds this phase does not implement.

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
	"kitten/codegen"
	"kitten/lexer"
	"kitten/parser"
)

// kittenExpressionsProgram is the driver program that prints expression
// renderings.
func kittenExpressionsProgram() string {
	return filepath.Join("..", "..", "selfhost", "expressions.ktn")
}

// buildKittenExpressions builds the expression probe. It fails if the build
// produces any C diagnostic, so the probe must compile warning-free.
func buildKittenExpressions(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-expressions")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenExpressionsProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the expression probe: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the expression probe:\n%s", warnings)
	}
	return executable
}

// runKittenExpressions writes the modules and runs the expression probe.
func runKittenExpressions(executable, directory, root, imported string) (string, error) {
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
		return "", fmt.Errorf("running the expression probe: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return "", fmt.Errorf("the expression probe wrote to stderr: %s", stderr.String())
	}
	return stdout.String(), nil
}

// referenceGenerate runs the reference generator over a checked module set.
func referenceGenerate(modules []*parser.Module, checked *checker.Result) (string, error) {
	return codegen.Generate(modules, checked)
}

// probeSeparator is the line the expression driver prints between the expression
// lines and the literal pool its renderings filled.
const probeSeparator = "=== literal pool ==="

// runProbe runs the expression driver and returns the expression lines and the
// pooled literal declarations separately.
func runProbe(t *testing.T, executable, directory, root, imported string) (string, string) {
	t.Helper()

	output, err := runKittenExpressions(executable, directory, root, imported)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return splitProbeOutput(t, output)
}

// splitProbeOutput splits a probe run's output into the expression lines and the
// pooled literal declarations that follow them.
func splitProbeOutput(t *testing.T, output string) (string, string) {
	t.Helper()

	marker := probeSeparator + "\n"
	index := strings.Index(output, marker)
	if index < 0 {
		t.Fatalf("the probe output has no %q line:\n%s", probeSeparator, output)
	}
	return output[:index], output[index+len(marker):]
}

// --- Extractable reference text ----------------------------------------------

// printArgumentTexts returns the rendering of every argument of every print
// expansion in generated C, in emission order.
//
// The reference expands a print statement into one comma-expression line:
//
//	(ktn_rt_print_int(ARG), ktn_rt_print_space(), ktn_rt_print_string(ARG), ktn_rt_print_newline());
//
// so a line that begins with a print helper carries that statement's arguments in
// order, and the helpers that separate and terminate values carry none.
func printArgumentTexts(t *testing.T, generated string) []string {
	t.Helper()

	var texts []string
	for _, line := range strings.Split(generated, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "(ktn_rt_print_") {
			continue
		}
		texts = append(texts, printHelperArguments(trimmed)...)
	}
	return texts
}

// printHelperArguments returns the argument of every print helper call in one
// print expansion, skipping the helpers that take no argument.
func printHelperArguments(line string) []string {
	var texts []string
	index := 0
	for index < len(line) {
		at := strings.Index(line[index:], "ktn_rt_print_")
		if at < 0 {
			return texts
		}
		at += index
		open := at
		for open < len(line) && line[open] != '(' {
			open++
		}
		if open >= len(line) {
			return texts
		}
		name := line[at:open]
		close := matchingParenthesis(line, open)
		if close < 0 {
			return texts
		}
		if name != "ktn_rt_print_space" && name != "ktn_rt_print_newline" {
			texts = append(texts, line[open+1:close])
		}
		index = close + 1
	}
	return texts
}

// matchingParenthesis returns the index of the parenthesis that closes the one at
// open, or -1 when there is none. A quoted region is skipped, so a parenthesis
// inside a string literal cannot close an argument early.
func matchingParenthesis(line string, open int) int {
	depth := 0
	inString := false
	for index := open; index < len(line); index++ {
		character := line[index]
		if inString {
			if character == '\\' {
				index++
				continue
			}
			if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

// --- Expected probe output ---------------------------------------------------

// unsupportedKinds lists the expression kinds this generator does not implement,
// in a stable order, with the failure message the emitter must report for each.
// Every kind is implemented now, so the list is empty; the harness keeps the
// mechanism so that a future unimplemented kind cannot silently look like a
// rendering.
var unsupportedKinds = []string{}

// unsupportedMessage is the failure the probe must report for a kind.
func unsupportedMessage(kind string) string {
	return kind + " expressions are not implemented yet"
}

// expectedExpression is what the probe must print for one expression node.
type expectedExpression struct {
	kind   string
	line   int
	column int

	// want is the rendering, or the failure message when failure is true.
	want     string
	compared bool
	failure  bool
}

// prefix is the part of the probe's line that does not depend on the rendering.
func (e expectedExpression) prefix() string {
	report := "expr"
	if e.failure {
		report = "fail"
	}
	return fmt.Sprintf("%s %d %d %s ", report, e.line, e.column, e.kind)
}

// referenceExpressions walks the reference AST in the probe's order and returns
// the expected line for every expression node.
func referenceExpressions(t *testing.T, modules []*parser.Module, checked *checker.Result, generated string) []expectedExpression {
	t.Helper()

	walk := &expressionWalk{checked: checked, printTexts: printArgumentTexts(t, generated)}
	for _, module := range modules {
		for _, declaration := range module.Program.Declarations {
			function, ok := declaration.(*parser.FunctionDecl)
			if !ok || function == nil || function.Body == nil {
				continue
			}
			walk.statement(function.Body)
		}
	}
	if walk.printIndex != len(walk.printTexts) {
		t.Fatalf("the walk consumed %d of %d print arguments", walk.printIndex, len(walk.printTexts))
	}
	return walk.expectations
}

// expressionWalk pairs each expression node with what the probe must print for it.
type expressionWalk struct {
	checked *checker.Result

	// printTexts holds the print arguments in emission order, and printIndex is
	// how many have been consumed.
	printTexts []string
	printIndex int

	expectations []expectedExpression
}

// unsupportedKindMessage returns the failure the emitter reports for a kind it does
// not implement, or the empty string for a supported kind.
func unsupportedKindMessage(kind string) string {
	for _, unsupported := range unsupportedKinds {
		if unsupported == kind {
			return unsupportedMessage(kind)
		}
	}
	return ""
}

// structLiteral visits a struct literal's values in field declaration order, which
// is the order the reference renders them in.
//
// A literal whose type the checker did not record keeps source order, which is what
// a tree the checker rejected would have; the checker records a type for every
// literal it accepts, so that cannot occur.
func (w *expressionWalk) structLiteral(node *parser.StructLiteral) {
	resolved, ok := w.checked.TypeRefs[node.Type]
	if !ok || resolved == nil || resolved.Struct == nil {
		for _, field := range node.Fields {
			if field != nil {
				w.expression(field.Value, "")
			}
		}
		return
	}
	for _, declared := range resolved.Struct.Fields {
		if value := literalFieldValue(node.Fields, declared.Name); value != nil {
			w.expression(value, "")
		}
	}
}

// variantLiteral visits a variant literal's values in field declaration order.
func (w *expressionWalk) variantLiteral(node *parser.VariantLiteral) {
	resolved, ok := w.checked.TypeRefs[node.Type]
	if !ok || resolved == nil || !resolved.IsUnion() {
		for _, field := range node.Fields {
			if field != nil {
				w.expression(field.Value, "")
			}
		}
		return
	}
	variant := resolved.VariantIndex[node.Variant]
	if variant == nil {
		for _, field := range node.Fields {
			if field != nil {
				w.expression(field.Value, "")
			}
		}
		return
	}
	for _, declared := range variant.Fields {
		if value := literalFieldValue(node.Fields, declared.Name); value != nil {
			w.expression(value, "")
		}
	}
}

// literalFieldValue returns the value a literal gives a field, or nil when it does
// not initialize that field.
func literalFieldValue(fields []*parser.FieldInit, name string) parser.Expr {
	for _, field := range fields {
		if field != nil && field.Name == name {
			return field.Value
		}
	}
	return nil
}

// isPrintCall reports whether a call is the built-in print, which the reference
// recognizes through the resolved symbol of its callee.
func (w *expressionWalk) isPrintCall(call *parser.CallExpr) bool {
	identifier, ok := call.Callee.(*parser.Identifier)
	if !ok || identifier == nil {
		return false
	}
	symbol := w.checked.Symbols[identifier]
	return symbol != nil && symbol.Function != nil && symbol.Function.Builtin == checker.BuiltinPrint
}

// argumentTexts returns the rendering of a call's arguments, consuming the print
// texts when the call is a print expansion. A call that is not print has no
// extractable arguments.
func (w *expressionWalk) argumentTexts(call *parser.CallExpr) []string {
	texts := make([]string, len(call.Arguments))
	if !w.isPrintCall(call) {
		return texts
	}
	for index := range call.Arguments {
		if w.printIndex < len(w.printTexts) {
			texts[index] = w.printTexts[w.printIndex]
			w.printIndex++
		}
	}
	return texts
}

// expression visits one expression and then its children, in the order the probe
// visits them, and returns the failure the probe must report for that expression,
// or the empty string when the probe rendered it.
//
// The node's slot is reserved before its children are visited, because the probe
// reports a node before it reports the expressions the node contains; what the
// node's own line says is filled in once its children are known.
func (w *expressionWalk) expression(expression parser.Expr, want string) string {
	position := expression.Pos()
	slot := len(w.expectations)
	w.expectations = append(w.expectations, expectedExpression{
		kind:   expressionKind(expression),
		line:   position.Line,
		column: position.Column,
	})

	ownFailure := unsupportedKindMessage(expressionKind(expression))

	// A child's failure is reported from the expression that contains it, and the
	// emitter stops at the first child that fails, in the order it evaluates them.
	childFailure := ""
	note := func(failure string) {
		if childFailure == "" && failure != "" {
			childFailure = failure
		}
	}

	switch node := expression.(type) {
	case *parser.UnaryExpr:
		note(w.expression(node.Operand, ""))

	case *parser.BinaryExpr:
		note(w.expression(node.Left, ""))
		note(w.expression(node.Right, ""))

	case *parser.AssignExpr:
		note(w.expression(node.Target, ""))
		note(w.expression(node.Value, ""))

	case *parser.CallExpr:
		texts := w.argumentTexts(node)
		note(w.expression(node.Callee, ""))
		for index, argument := range node.Arguments {
			note(w.expression(argument, texts[index]))
		}

	case *parser.MemberExpr:
		note(w.expression(node.Receiver, ""))

	case *parser.StructLiteral:
		w.structLiteral(node)

	case *parser.VariantLiteral:
		w.variantLiteral(node)

	case *parser.NewArrayExpr:
		note(w.expression(node.Length, ""))

	case *parser.NewMapExpr:

	case *parser.IndexExpr:
		note(w.expression(node.Target, ""))
		note(w.expression(node.Index, ""))

	case *parser.Identifier, *parser.IntLiteral, *parser.FloatLiteral, *parser.StringLiteral, *parser.BoolLiteral:
	}

	// A node whose own kind is not implemented is reported before its children are
	// reached: the emitter does not render the children of such a node at all.
	failure := ownFailure
	if failure == "" {
		failure = childFailure
	}
	expected := &w.expectations[slot]
	if failure != "" {
		expected.want = failure
		expected.compared = true
		expected.failure = true
	} else if want != "" {
		expected.want = want
		expected.compared = true
	}
	return failure
}

// statement visits the expressions of one statement in emission order.
func (w *expressionWalk) statement(statement parser.Stmt) {
	switch node := statement.(type) {
	case *parser.BlockStmt:
		for _, inner := range node.Statements {
			if inner != nil {
				w.statement(inner)
			}
		}

	case *parser.VarDeclStmt:
		if node.Initializer != nil {
			w.expression(node.Initializer, "")
		}

	case *parser.ExprStmt:
		w.expression(node.Expression, "")

	case *parser.IfStmt:
		w.expression(node.Condition, "")
		w.statement(node.Then)
		if node.Else != nil {
			w.statement(node.Else)
		}

	case *parser.WhileStmt:
		w.expression(node.Condition, "")
		w.statement(node.Body)

	case *parser.ForStmt:
		if node.Init != nil {
			w.statement(node.Init)
		}
		if node.Condition != nil {
			w.expression(node.Condition, "")
		}
		if node.Increment != nil {
			w.expression(node.Increment, "")
		}
		w.statement(node.Body)

	case *parser.ReturnStmt:
		if node.Value != nil {
			w.expression(node.Value, "")
		}

	case *parser.MatchStmt:
		w.expression(node.Scrutinee, "")
		for _, arm := range node.Arms {
			if arm != nil && arm.Body != nil {
				w.statement(arm.Body)
			}
		}
	}
}

// --- Comparison --------------------------------------------------------------

// expressionOutcome is how one comparison ended.
type expressionOutcome struct {
	lines    int
	compared int
}

// compareExpressions compares the probe's output with the expected lines, one per
// expression node, in order. The pair of inputs is ASCII, so the positions the
// probe reports are the positions the reference reports.
func compareExpressions(t *testing.T, description, got string, expected []expectedExpression) expressionOutcome {
	t.Helper()

	lines := probeLines(got)
	outcome := expressionOutcome{lines: len(expected)}
	if len(lines) != len(expected) {
		shown := got
		if len(shown) > 400 {
			shown = shown[:400] + "..."
		}
		t.Errorf("%s: the probe printed %d lines, expected %d\n--- probe ---\n%s", description, len(lines), len(expected), shown)
		return outcome
	}
	for index, want := range expected {
		gotLine := lines[index]
		prefix := want.prefix()
		if !strings.HasPrefix(gotLine, prefix) {
			t.Errorf("%s: line %d: expected a report beginning %q\n got: %q", description, index, strings.TrimSpace(prefix), gotLine)
			continue
		}
		if !want.compared {
			continue
		}
		outcome.compared++
		if gotLine != prefix+want.want {
			t.Errorf("%s: line %d: rendering differs\n got: %q\nwant: %q", description, index, gotLine, prefix+want.want)
		}
	}
	return outcome
}

// checkProgram prepares, checks, and generates one program with the reference, or
// reports why it could not be used.
func checkProgram(t *testing.T, description, root, imported string) ([]*parser.Module, *checker.Result, string, bool) {
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
	if !referenceAcceptsEntryPoint(modules, checked) {
		t.Errorf("%s: the reference generator's entry point rule rejects the program", description)
		return nil, nil, "", false
	}
	generated, generationError := referenceGenerate(modules, checked)
	if generationError != nil {
		t.Errorf("%s: the reference generator rejected the program: %v", description, generationError)
		return nil, nil, "", false
	}
	return modules, checked, generated, true
}

// --- Targeted cases ----------------------------------------------------------

// expressionCase is one program whose print arguments are the expressions under
// test, plus the expressions that surround them.
type expressionCase struct {
	description string
	root        string
	imported    string
}

// expressionCases covers every literal kind, every unary operator, every binary
// operator, precedence and associativity, parenthesized source, mixed operand
// types, and identifiers in an imported module. Composite expressions are printed
// whole, so their subexpressions are validated transitively, and each subexpression
// form is also printed on its own so that it is compared directly.
func expressionCases() []expressionCase {
	return []expressionCase{{
		description: "integer literals",
		root: `fn main() {
    print(0);
    print(1);
    print(42);
    print(9223372036854775807);
    print(-9223372036854775807);
}
`,
	}, {
		description: "integer arithmetic and comparisons",
		root: `fn main() {
    int a = 2;
    int b = 3;
    print(a);
    print(b);
    print(a + b);
    print(a - b);
    print(a * b);
    print(a / b);
    print(a % b);
    print(a < b);
    print(a <= b);
    print(a > b);
    print(a >= b);
    print(a == b);
    print(a != b);
}
`,
	}, {
		description: "floating-point literals",
		root: `fn main() {
    print(0.0);
    print(1.0);
    print(12.5);
    print(0.25);
    print(00.5);
    print(3.141592653589793);
    print(0.000000000000000000001);
    print(12345678901234567890.5);
    print(-0.0);
}
`,
	}, {
		description: "floating-point arithmetic and comparisons",
		root: `fn main() {
    float a = 1.5;
    float b = 0.25;
    print(a);
    print(b);
    print(a + b);
    print(a - b);
    print(a * b);
    print(a / b);
    print(a < b);
    print(a >= b);
    print(a == b);
    print(a != b);
}
`,
	}, {
		description: "boolean literals",
		root: `fn main() {
    print(true);
    print(false);
}
`,
	}, {
		description: "boolean operators",
		root: `fn main() {
    bool yes = true;
    bool no = false;
    print(yes);
    print(no);
    print(!yes);
    print(!no);
    print(yes && no);
    print(yes || no);
    print(yes == no);
    print(yes != no);
}
`,
	}, {
		description: "unary operators on integers",
		root: `fn main() {
    int a = 1;
    print(a);
    print(-a);
    print(- -a);
    print(++a);
    print(--a);
    print(a++);
    print(a--);
}
`,
	}, {
		description: "unary operators on floats",
		root: `fn main() {
    float f = 2.5;
    print(f);
    print(-f);
    print(- -f);
    print(++f);
    print(f++);
}
`,
	}, {
		description: "mixed precedence and associativity",
		root: `fn main() {
    int a = 1;
    int b = 2;
    int c = 3;
    int d = 4;
    print(a);
    print(b);
    print(c);
    print(d);
    print(a + b * c);
    print(a * b + c);
    print(a - b - c);
    print(a - (b - c));
    print((a - b) - c);
    print(a * b / c % d);
    print(a + b * c - d / a);
    print(-a + b);
    print(!(a < b));
    print(a < b && b <= c);
    print(a < b || b > c);
    print((a < b) == (c > d));
    print((a < b) != (c > d));
    print((a + b) * (c - d));
    print(a % (b + c));
}
`,
	}, {
		description: "nested and redundantly parenthesized expressions",
		root: `fn main() {
    int a = 2;
    print(a);
    print((((a))));
    print(-(-(-a)));
    print((a + 1) * ((a - 1) + (a * 2)));
    print(!(!(a == a)));
}
`,
	}, {
		description: "identifiers of every printable type",
		root: `fn main() {
    int i = 1;
    float f = 2.5;
    bool b = true;
    string s = "text";
    print(i);
    print(f);
    print(b);
    print(s);
}
`,
	}, {
		description: "string operators use the runtime helpers",
		root: `fn main() {
    string left = "a";
    string right = "b";
    print(left);
    print(right);
    print(left + right);
    print(left == right);
    print(left != right);
    print(left + "c");
}
`,
	}, {
		description: "eprint calls with every printable type",
		root: `fn main() {
    eprint(1);
    eprint(2.5);
    eprint(true);
    eprint("text");
    eprint(1, 2.5, true, "text");
    eprint("");
}
`,
	}, {
		description: "exit calls in expression position",
		root: `fn stop(int code) {
    exit(code);
}
fn main() {
    stop(1 + 2);
}
`,
	}, {
		description: "expressions around control flow",
		root: `fn main() {
    int i = 0;
    int total = 0;
    while (i < 4) {
        total = total + i;
        i = i + 1;
    }
    print(i);
    print(total);
    print(total == 6);
    if (total > 0) {
        print(total * 2);
    } else {
        print(total - 1);
    }
    print(classify(total));
}
fn classify(int value) -> bool {
    print(value > 0);
    print(value == 0);
    return value != 0;
}
`,
	}, {
		description: "expressions in an imported module",
		root: `import "input2.txt";
fn main() {
    print(scaled(3));
    print(1 + 2);
    print(-1);
}
`,
		imported: `public fn scaled(int value) -> int {
    print(value);
    print(value * 2);
    print(-value);
    print(value + 1 == 4);
    return value + 1;
}
`,
	}, {
		description: "member access and qualified variant references",
		root: `struct Point {
    int x;
    int y;
}
enum Color { Red, Green }
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
    print(point.x);
    print(point.y);
    Color color = Color.Red;
    print(color == Color.Red);
    print(color != Color.Green);
    print(measure(Shape.Nothing));
}
`,
	}, {
		description: "index over arrays, maps, and strings",
		root: `fn main() {
    int[] values = new int[3];
    print(values[0]);
    print(values[2]);
    map[string]int counts = new map[string]int;
    print(counts["a"]);
    print("kitten"[0]);
    print("kitten"[5]);
    int[][] grid = new int[][2];
    print(grid[0][0]);
    print(grid[1][1]);
}
`,
	}, {
		description: "member of an index of a member",
		root: `struct Point {
    int x;
    int y;
}
struct Row {
    Point[] cells;
}
fn main() {
    Row[] rows = new Row[1];
    print(rows[0].cells[0].x);
    print(rows[0].cells[1].y);
}
`,
	}, {
		description: "a struct literal containing a variant literal",
		root: `union Value {
    Number { int amount; }
    Empty
}
struct Box {
    Value value;
}
fn amount_of(Box box) -> int {
    match (box.value) {
        Number(amount) { return amount; }
        Empty { return 0; }
    }
}
fn main() {
    print(amount_of(Box { value = Value.Number { amount = 7 } }));
    print(amount_of(Box { value = Value.Empty }));
}
`,
	}, {
		description: "a new array of structs and a new map with struct values",
		root: `struct Point {
    int x;
    int y;
}
fn main() {
    print(length(new Point[2]));
    print(length(new map[string]Point));
    print(new int[3][1]);
    print(length(new int[][2]));
    print(length(new map[string]int[]));
    map[string]Point points = new map[string]Point;
    print(points["a"].x);
}
`,
	}, {
		description: "recursive union variants",
		root: `union Expression {
    IntegerLiteral { int value; }
    Name { string name; }
    Add { Expression left; Expression right; }
    Missing
}
fn evaluate(Expression expression) -> int {
    match (expression) {
        IntegerLiteral(value) { return value; }
        Name(name) { return 0; }
        Add(left, right) { return evaluate(left) + evaluate(right); }
        Missing { return 0; }
    }
}
fn main() {
    print(evaluate(Expression.Add {
        left = Expression.IntegerLiteral { value = 2 },
        right = Expression.Add {
            left = Expression.IntegerLiteral { value = 3 },
            right = Expression.Missing
        }
    }));
}
`,
	}, {
		description: "assignment to locals, fields, elements, and nested targets",
		root: `struct Inner {
    int value;
}
struct Outer {
    Inner inner;
}
struct Point {
    int x;
    int y;
}
struct Row {
    Point[] cells;
}
fn main() {
    int a = 1;
    int b = 2;
    print(a = 3);
    print(b = a = 4);
    print(a + b);
    Outer outer = Outer { inner = Inner { value = 0 } };
    print(outer.inner.value = 5);
    print(outer.inner.value);
    Point point = Point { x = 0, y = 0 };
    print(point.x = 6);
    print(point.y = point.x + 1);
    int[] values = new int[3];
    print(values[0] = 7);
    print(values[1] = values[0] + 1);
    values[2] = 9;
    print(values[2]);
    Row[] rows = new Row[1];
    rows[0] = Row { cells = new Point[2] };
    rows[0].cells[0] = Point { x = 10, y = 11 };
    print(rows[0].cells[0].x = 12);
    print(rows[0].cells[0].y);
    print(rows[0].cells[0].x);
}
`,
	}, {
		description: "compound assignment on locals, fields, and elements",
		root: `struct Measures {
    int count;
    float scale;
}
fn main() {
    int number = 10;
    print(number += 5);
    print(number -= 3);
    float ratio = 1.5;
    print(ratio += 0.5);
    print(ratio -= 0.25);
    Measures measures = Measures { count = 1, scale = 1.0 };
    print(measures.count += 2);
    print(measures.count -= 1);
    print(measures.scale += 0.5);
    print(measures.scale -= 0.25);
    int[] numbers = new int[2];
    numbers[0] = 1;
    print(numbers[0] += 4);
    print(numbers[0] -= 2);
    float[] ratios = new float[2];
    ratios[1] = 1.0;
    print(ratios[1] += 0.5);
    print(ratios[1] -= 0.25);
}
`,
	}, {
		description: "increment and decrement in every position and target form",
		root: `struct Counter {
    int value;
    float scale;
}
fn main() {
    int number = 0;
    print(++number);
    print(--number);
    print(number++);
    print(number--);
    float ratio = 0.5;
    print(++ratio);
    print(--ratio);
    print(ratio++);
    print(ratio--);
    Counter counter = Counter { value = 0, scale = 0.5 };
    print(++counter.value);
    print(counter.value++);
    print(--counter.value);
    print(counter.value--);
    print(++counter.scale);
    print(counter.scale++);
    print(--counter.scale);
    print(counter.scale--);
    int[] numbers = new int[2];
    numbers[0] = 0;
    print(++numbers[0]);
    print(numbers[0]++);
    print(--numbers[0]);
    print(numbers[0]--);
    float[] scales = new float[2];
    scales[1] = 0.5;
    print(++scales[1]);
    print(scales[1]++);
    print(--scales[1]);
    print(scales[1]--);
}
`,
	}, {
		description: "string append to a local, a field, and an element",
		root: `struct Tag {
    string label;
}
fn main() {
    string text = "a";
    print(text += "b");
    Tag tag = Tag { label = "c" };
    print(tag.label += "d");
    string[] parts = new string[2];
    parts[0] = "e";
    print(parts[0] += "f");
    print(text);
    print(tag.label);
    print(parts[0]);
}
`,
	}, {
		description: "map entry assignment for every value shape",
		root: `struct Point {
    int x;
}
union Value {
    Number { int amount; }
    Empty
}
fn point_x(Point point) -> int {
    return point.x;
}
fn amount(Value value) -> int {
    match (value) {
        Number(number) { return number; }
        Empty { return 0; }
    }
}
fn main() {
    map[string]int counts = new map[string]int;
    print(counts["a"] = 1);
    print(counts["a"] = 2);
    counts["b"] = 3;
    print(counts["b"] += 4);
    map[string]string names = new map[string]string;
    print(names["greeting"] = "hello");
    print(names["greeting"] += " world");
    map[string]Point points = new map[string]Point;
    points["p"] = Point { x = 1 };
    print(point_x(points["p"] = Point { x = 2 }));
    map[string]Value values = new map[string]Value;
    values["v"] = Value.Empty;
    print(amount(values["v"] = Value.Number { amount = 3 }));
}
`,
	}, {
		description: "assignment as a sub-expression and in conditions",
		root: `fn main() {
    int a = 0;
    int b = 0;
    int c = 0;
    print(a = b = c = 7);
    print(a + b + c);
    print((a = a + 1));
    print(a += b);
    print(a -= b);
    bool more = true;
    int counter = 0;
    while (more = counter < 3) {
        counter++;
    }
    print(counter);
    bool flag = false;
    if (flag = counter > 2) {
        print(flag);
    }
}
`,
	}, {
		description: "file and directory prelude calls and matches",
		root: `fn main() {
    match (try_write_file("x", "y")) {
        Ok { print("ok"); }
        Failed(message) { print(message); }
    }
    match (try_list_directory("d")) {
        Ok(entries) { print(length(entries)); }
        Failed(message) { print(message); }
    }
}
`,
	}, {
		description: "byte values, arrays, and conversions",
		root: `fn main() {
    byte a = byte_from_int(65);
    byte b = byte_from_int(256);
    print(int_from_byte(a), int_from_byte(b));
    print(a == b, a < b);
    byte[] values = new byte[3];
    values[0] = a;
    print(length(values), int_from_byte(values[0]));
    byte[] text = string_to_bytes("Aé");
    print(length(text), int_from_byte(text[0]));
    string round = bytes_to_string(text);
    print(round);
}
`,
	}, {
		description: "bitwise and shift operators",
		root: `fn main() {
    int mask = 0xF0;
    print(mask & 0x0F);
    print(mask | 0x0F);
    print(mask ^ 0xFF);
    print(~mask);
    print(1 << 3);
    print(0x8000000000000000 >> 63);
    print(-8 >> 1);
    print((mask | 0x0F) & 0xFF);
}
`,
	}, {
		description: "defined integer wrapping, division, and remainder",
		root: `fn main() {
    int maximum = 9223372036854775807;
    int minimum = -9223372036854775807 - 1;
    print(maximum + 1);
    print(minimum - 1);
    print(maximum * 2);
    print(-minimum);
    print(minimum / -1);
    print(minimum % -1);
    print(maximum / 7);
    print(-maximum % 7);
}
`,
	}, {
		description: "try_read_file call and direct variant construction",
		root: `fn main() {
    try_read_file("x");
    ReadFileResult result = ReadFileResult.Ok { text = "x" };
}
`,
	}}
}

// TestKittenExpressionsMatchReference compares the probe's rendering of every
// expression of each targeted program with the reference generator's.
func TestKittenExpressionsMatchReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	compared := 0
	lines := 0
	for _, testCase := range expressionCases() {
		modules, checked, generated, ok := checkProgram(t, testCase.description, testCase.root, testCase.imported)
		if !ok {
			continue
		}
		expected := referenceExpressions(t, modules, checked, generated)
		expressions, pool := runProbe(t, executable, directory, testCase.root, testCase.imported)
		if want := referenceLiteralPool(t, generated); pool != want {
			t.Errorf("%s: literal pool differs\n--- probe ---\n%s--- reference ---\n%s", testCase.description, pool, want)
		}
		outcome := compareExpressions(t, testCase.description, expressions, expected)
		compared += outcome.compared
		lines += outcome.lines
	}

	if compared < 100 {
		t.Fatalf("only %d of %d expected lines were compared", compared, lines)
	}
	t.Logf("core expressions: %d targeted programs, %d of %d expected lines compared", len(expressionCases()), compared, lines)
}

// TestKittenExpressionsEveryKind checks that the targeted corpus really renders
// every expression kind this phase adds, so one of them cannot silently regress to
// a failure while the corpus comparison still passes for other reasons. A kind is
// counted only when the probe reports it as a rendering, never as a propagated
// failure.
func TestKittenExpressionsEveryKind(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	rendered := make(map[string]bool)
	for _, testCase := range expressionCases() {
		expressions, _ := runProbe(t, executable, directory, testCase.root, testCase.imported)
		for _, line := range probeLines(expressions) {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[0] == "expr" {
				rendered[fields[3]] = true
			}
		}
	}
	for _, kind := range []string{"member", "struct-literal", "variant-literal", "new-array", "new-map", "index", "assign"} {
		if !rendered[kind] {
			t.Errorf("the %s kind was never rendered by the targeted corpus", kind)
		}
	}
}

// TestKittenExpressionsEveryOperator checks that the targeted corpus really prints
// every unary and binary operator the language defines, so an operator cannot
// disappear from the corpus unnoticed. The operators are read from the lexer's
// tokens, so an operator inside a string literal or a comment cannot satisfy the
// check.
func TestKittenExpressionsEveryOperator(t *testing.T) {
	operators := []string{"+", "-", "*", "/", "%", "<", "<=", ">", ">=", "==", "!=", "&&", "||", "!", "++", "--"}

	used := make(map[string]bool)
	for _, testCase := range expressionCases() {
		for _, source := range []string{testCase.root, testCase.imported} {
			if source == "" {
				continue
			}
			tokens, err := lexer.Lex(source)
			if err != nil {
				t.Fatalf("%s: %v", testCase.description, err)
			}
			for _, token := range tokens {
				used[token.Text] = true
			}
		}
	}

	for _, operator := range operators {
		if !used[operator] {
			t.Errorf("the targeted corpus does not use the %q operator", operator)
		}
	}
}

// --- Every kind --------------------------------------------------------------

// allKindsSource exercises every expression kind, including assignment, in one
// program. Every line the probe prints for it must be a rendering: no kind is
// unimplemented now.
func allKindsSource() string {
	return `struct Point {
    int x;
}
enum Color { Red }
union Value {
    Number { int amount; }
    Empty
}
fn helper(int value) -> int {
    return value;
}
fn main() {
    Point p = Point { x = 1 };
    Color c = Color.Red;
    Value v = Value.Number { amount = 1 };
    Value w = Value.Empty;
    int[] values = new int[2];
    map[string]int counts = new map[string]int;
    map[string]int other = new map[string]int;
    string s = "text";
    int i = 0;
    i = 1;
    i += 2;
    counts["a"] = 1;
    values[0] = 2;
    print(helper(i));
    print(helper(i = 3));
    print(s);
    print(p.x);
    print(values[0]);
    print(counts["a"]);
    print(c == Color.Red);
    print(length(other));
    match (v) {
        Number(amount) { print(amount); }
        Empty { print(1); }
    }
    match (w) {
        Number(amount) { print(amount); }
        Empty { print(0); }
    }
}
`
}

// TestKittenExpressionsEveryKindRenders checks that a program using every kind
// produces renderings only, with no failure at all, so an unimplemented kind
// cannot hide behind another expression's report.
func TestKittenExpressionsEveryKindRenders(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	got, err := runKittenExpressions(executable, directory, allKindsSource(), "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	expressions, pool := splitProbeOutput(t, got)

	renderedKinds := make(map[string]bool)
	for _, line := range probeLines(expressions) {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			t.Errorf("malformed probe line: %q", line)
			continue
		}
		if fields[0] != "expr" {
			t.Errorf("an expression kind failed: %q", line)
			continue
		}
		renderedKinds[fields[3]] = true
	}

	for _, kind := range []string{"assign", "member", "struct-literal", "variant-literal", "new-array", "new-map", "index", "call"} {
		if !renderedKinds[kind] {
			t.Errorf("the %s kind was not rendered by the all-kinds program", kind)
		}
	}

	// A string literal renders as a reference to its pooled object, and the
	// program's literal is interned.
	if !strings.Contains(expressions, "string (&ktn_literal_1)") {
		t.Errorf("the string literal was not rendered as a pooled reference:\n%s", expressions)
	}
	if !strings.Contains(pool, `static const ktn_string ktn_literal_1 = { 4, "text" };`) {
		t.Errorf("the string literal was not pooled:\n%s", pool)
	}
}

// TestKittenExpressionsAllKindsAgreeWithTheReference checks that the all-kinds
// program is generated and checked by the reference without complaint, so a
// failure would be this generator's rather than the language's.
func TestKittenExpressionsAllKindsAgreeWithTheReference(t *testing.T) {
	requireToolchain(t)

	if _, _, _, ok := checkProgram(t, "all kinds", allKindsSource(), ""); !ok {
		t.Fatalf("the reference rejected the program that exercises every kind")
	}
}

// --- Corpus ------------------------------------------------------------------

// TestKittenExpressionsCorpus runs the probe over the checker corpus. Every
// program the checker rejects must be reported with the checker's own error, and
// every program it accepts must produce exactly the expected lines, of which the
// extractable ones are compared byte for byte.
func TestKittenExpressionsCorpus(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	programs := 0
	compared := 0
	lines := 0
	poolEntries := 0
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
			got, err := runKittenExpressions(executable, directory, testCase.root, testCase.imported)
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
		// this phase does not implement, and a non-ASCII input shifts the positions
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

		expected := referenceExpressions(t, modules, checked, generated)
		expressions, pool := runProbe(t, executable, directory, testCase.root, testCase.imported)
		if want := referenceLiteralPool(t, generated); pool != want {
			t.Errorf("%s: literal pool differs\n--- probe ---\n%s--- reference ---\n%s", testCase.name, pool, want)
		}
		poolEntries += strings.Count(pool, "static const ktn_string ")
		outcome := compareExpressions(t, testCase.name, expressions, expected)
		compared += outcome.compared
		lines += outcome.lines
		programs++
	}

	if programs < 20 || compared < 100 || poolEntries < 20 {
		t.Fatalf("the corpus looks too small: %d programs, %d of %d lines compared, %d pool entries, %d check errors, %d skipped",
			programs, compared, lines, poolEntries, checkErrors, skipped)
	}
	t.Logf("expression corpus: %d programs, %d of %d lines compared, %d pooled entries compared, %d check errors identical, %d skipped",
		programs, compared, lines, poolEntries, checkErrors, skipped)
}

// --- Floating-point syntax ---------------------------------------------------

// TestKittenExpressionsFloatSyntax documents the floating-point forms the language
// can express and checks the emitter echoes each of them verbatim.
//
// The reference never reformats a floating-point literal: it emits the source
// spelling, so no rounding, no exponent form, and no change of precision can be
// introduced by generation. The forms the language cannot express are checked as a
// lexical boundary rather than tested as inputs.
func TestKittenExpressionsFloatSyntax(t *testing.T) {
	requireToolchain(t)

	// A decimal point begins a float literal only when a digit follows it.
	tokens, err := lexer.Lex("1.")
	if err != nil {
		t.Errorf("lexing \"1.\": %v", err)
	} else if len(tokens) < 2 || tokens[0].Kind != lexer.Int {
		t.Errorf("\"1.\" did not lex as an integer followed by a dot: %v", tokens)
	}
	tokens, err = lexer.Lex(".5")
	if err != nil {
		t.Errorf("lexing \".5\": %v", err)
	} else if len(tokens) < 2 || tokens[0].Kind != lexer.Dot {
		t.Errorf("\".5\" did not lex as a dot followed by an integer: %v", tokens)
	}
	// An exponent form is not a lexical error: the language defines no exponent
	// notation, so "1e5" lexes as the integer 1 followed by the identifier e5,
	// which the parser then rejects.
	tokens, err = lexer.Lex("1e5")
	if err != nil {
		t.Errorf("lexing \"1e5\": %v", err)
	} else if len(tokens) < 2 || tokens[0].Kind != lexer.Int || tokens[0].Text != "1" || tokens[1].Kind != lexer.Ident {
		t.Errorf("\"1e5\" did not lex as an integer followed by an identifier: %v", tokens)
	}

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	root := `fn main() {
    print(0.0);
    print(0.5);
    print(00.5);
    print(1.0);
    print(10.25);
    print(1000000.0000001);
    print(0.000000000000000000001);
    print(12345678901234567890.5);
    print(3.141592653589793238462643383279);
    print(-0.0);
}
`
	modules, checked, generated, ok := checkProgram(t, "floating-point spellings", root, "")
	if !ok {
		t.Fatalf("the floating-point program was rejected")
	}
	expected := referenceExpressions(t, modules, checked, generated)
	got, pool := runProbe(t, executable, directory, root, "")
	if want := referenceLiteralPool(t, generated); pool != want {
		t.Errorf("floating-point spellings: literal pool differs\n--- probe ---\n%s--- reference ---\n%s", pool, want)
	}
	outcome := compareExpressions(t, "floating-point spellings", got, expected)
	if outcome.compared == 0 {
		t.Fatalf("no floating-point literal was compared")
	}

	// The rendering must be the source spelling, character for character.
	spellings := []string{"0.0", "0.5", "00.5", "1.0", "10.25", "1000000.0000001", "0.000000000000000000001", "12345678901234567890.5", "3.141592653589793238462643383279"}
	for _, spelling := range spellings {
		if !strings.Contains(got, spelling) {
			t.Errorf("the probe did not render the literal %s verbatim:\n%s", spelling, got)
		}
	}
	// A negative literal is the unary minus operator around a positive literal,
	// which the emitter parenthesizes.
	if !strings.Contains(got, "(-0.0)") {
		t.Errorf("the probe did not render a negative float as a parenthesized unary minus:\n%s", got)
	}
	t.Logf("floating-point literals: %d compared, every spelling echoed verbatim", outcome.compared)
}

// --- Determinism, format, and cost -------------------------------------------

// TestKittenExpressionsIsDeterministic checks that the probe depends only on its
// input: the same program must produce the same bytes after a different program.
func TestKittenExpressionsIsDeterministic(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	root := "fn main() {\n    int a = 1;\n    print(a + 2 * 3);\n    print(-a);\n}\n"
	first, err := runKittenExpressions(executable, directory, root, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := runKittenExpressions(executable, directory, "fn main() {\n    print(1 == 2);\n}\n", ""); err != nil {
		t.Fatalf("%v", err)
	}
	second, err := runKittenExpressions(executable, directory, root, "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if first != second {
		t.Errorf("the probe's output depends on the previous run\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestKittenExpressionsReportFormat pins the probe's line format with explicit
// expectations, so a format change is caught even if both sides change.
func TestKittenExpressionsReportFormat(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	got, err := runKittenExpressions(executable, directory, "fn main() {\n    int a = 1;\n    print(a + 2);\n}\n", "")
	if err != nil {
		t.Fatalf("%v", err)
	}
	want := expectedProbeOutput([]string{
		"expr 2 13 int 1",
		"expr 3 5 call (ktn_rt_print_int(ktn_rt_i64_add(ktn_var_a, 2)), ktn_rt_print_newline())",
		"expr 3 5 ident ktn_var_print",
		"expr 3 11 binary ktn_rt_i64_add(ktn_var_a, 2)",
		"expr 3 11 ident ktn_var_a",
		"expr 3 15 int 2",
		probeSeparator,
	})
	if got != want {
		t.Errorf("probe output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestKittenExpressionsReportErrors checks that a program the checker rejects is
// reported with the checker's own error rather than as expressions.
func TestKittenExpressionsReportErrors(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	for _, root := range []string{
		"fn main() {\n    int a = true;\n}\n",
		"fn main() {\n    int a = 1;\n    print(a + true);\n}\n",
		"fn main() {\n    bool b = true;\n    print(b + b);\n}\n",
		"fn main() {\n    print(missing);\n}\n",
		"fn main() {\n    print(1e5);\n}\n",
	} {
		got, err := runKittenExpressions(executable, directory, root, "")
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		if !strings.HasPrefix(got, "error ") {
			t.Errorf("a rejected program was not reported as an error: %q", got)
		}
		if strings.Contains(got, "expr ") || strings.Contains(got, "fail ") {
			t.Errorf("a rejected program produced expression lines:\n%s", got)
		}
	}
}

// nestedExpressionProgram builds a program whose single expression is a chain of
// depth additions, so that the cost of rendering a deeply nested expression can be
// measured. Depth zero is a bare identifier.
func nestedExpressionProgram(depth int) string {
	var builder strings.Builder
	builder.WriteString("fn main() {\n    int a = 1;\n    print(")
	for index := 0; index < depth; index++ {
		builder.WriteString("(a + ")
	}
	builder.WriteString("a")
	for index := 0; index < depth; index++ {
		builder.WriteString(")")
	}
	builder.WriteString(");\n}\n")
	return builder.String()
}

// TestKittenExpressionsScaling measures rendering a deeply nested expression and
// checks the probe still agrees with the reference at that depth.
//
// Rendering one node concatenates its children's text, so the text of a chain of
// depth d is about d characters at the deepest node and about d squared characters
// in total. The reference generator has exactly the same shape, because it builds
// the same string the same way: this is a property of the representation the
// reference chose, not a regression introduced here. The measurement records the
// cost, and the bound only catches a change in the order of the cost - an
// exponential blow-up rather than the quadratic one that matching the reference
// requires.
func TestKittenExpressionsScaling(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	previous := time.Duration(0)
	first := time.Duration(0)
	last := time.Duration(0)
	compared := 0
	for _, depth := range []int{100, 200, 400} {
		root := nestedExpressionProgram(depth)
		modules, checked, generated, ok := checkProgram(t, fmt.Sprintf("depth %d", depth), root, "")
		if !ok {
			continue
		}
		expected := referenceExpressions(t, modules, checked, generated)

		best := time.Duration(0)
		var got string
		for repetition := 0; repetition < 3; repetition++ {
			start := time.Now()
			rendered, err := runKittenExpressions(executable, directory, root, "")
			if err != nil {
				t.Fatalf("depth %d: %v", depth, err)
			}
			if elapsed := time.Since(start); best == 0 || elapsed < best {
				best = elapsed
			}
			got = rendered
		}
		expressions, _ := splitProbeOutput(t, got)
		outcome := compareExpressions(t, fmt.Sprintf("depth %d", depth), expressions, expected)
		compared += outcome.compared

		growth := ""
		if previous > 0 {
			growth = fmt.Sprintf(", x%.1f", float64(best)/float64(previous))
		}
		t.Logf("depth=%5d nodes=%5d bytes=%8d  %10s%s", depth, outcome.lines, len(got), best.Round(time.Millisecond), growth)
		previous = best
		if first == 0 {
			first = best
		}
		last = best
	}

	if compared == 0 {
		t.Fatalf("no nested expression was compared")
	}
	// Four times the depth, and the quadratic cost matching the reference would be
	// sixteen times the work. The bound is far looser, so it fails only on a
	// different order of cost.
	if last > first*100 {
		t.Errorf("a four-fold deeper expression cost %v against %v, which is a different order of cost", last, first)
	}
}
