package parser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kitten/lexer"
)

// at is a short constructor for expected positions, keeping the tables below
// readable.
func at(line, column int) Position {
	return Position{Line: line, Column: column}
}

// assertProgram parses source and checks that it produced exactly want.
func assertProgram(t *testing.T, source string, want *Program) {
	t.Helper()

	got, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource(%q) returned unexpected error: %v", source, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSource(%q) produced a different tree\n got: %+v\nwant: %+v", source, got, want)
	}
}

// assertParseError parses source and checks that it failed at the given
// position with a message containing wantMessagePart.
func assertParseError(t *testing.T, source string, wantLine, wantColumn int, wantMessagePart string) *Error {
	t.Helper()

	_, err := ParseSource(source)
	if err == nil {
		t.Fatalf("ParseSource(%q) succeeded, want error at %d:%d", source, wantLine, wantColumn)
	}

	var parseError *Error
	if !errors.As(err, &parseError) {
		t.Fatalf("ParseSource(%q) returned %T (%v), want *parser.Error", source, err, err)
	}
	if parseError.Line != wantLine || parseError.Column != wantColumn {
		t.Errorf("ParseSource(%q) error at %d:%d, want %d:%d (%v)", source, parseError.Line, parseError.Column, wantLine, wantColumn, parseError)
	}
	if wantMessagePart != "" && !strings.Contains(parseError.Message, wantMessagePart) {
		t.Errorf("ParseSource(%q) message %q does not contain %q", source, parseError.Message, wantMessagePart)
	}
	return parseError
}

// firstFunction returns the first declaration of program, requiring it to be a
// function.
func firstFunction(t *testing.T, program *Program) *FunctionDecl {
	t.Helper()
	if len(program.Declarations) == 0 {
		t.Fatalf("program has no declarations")
	}
	function, ok := program.Declarations[0].(*FunctionDecl)
	if !ok {
		t.Fatalf("first declaration is %T, want *FunctionDecl", program.Declarations[0])
	}
	return function
}

// parseExpressionSource parses a single expression by wrapping it in a trivial
// function body and returns the expression.
func parseExpressionSource(t *testing.T, source string) Expr {
	t.Helper()

	program, err := ParseSource("fn main() { " + source + "; }")
	if err != nil {
		t.Fatalf("parsing expression %q: %v", source, err)
	}
	function := firstFunction(t, program)
	if len(function.Body.Statements) != 1 {
		t.Fatalf("expression %q produced %d statements, want 1", source, len(function.Body.Statements))
	}
	statement, ok := function.Body.Statements[0].(*ExprStmt)
	if !ok {
		t.Fatalf("expression %q produced statement %T, want *ExprStmt", source, function.Body.Statements[0])
	}
	return statement.Expression
}

// renderExpression prints an expression as an S-expression so that precedence
// and associativity can be asserted compactly.
func renderExpression(expression Expr) string {
	switch node := expression.(type) {
	case *Identifier:
		return node.Name
	case *IntLiteral:
		return node.Text
	case *FloatLiteral:
		return node.Text
	case *StringLiteral:
		return node.Text
	case *BoolLiteral:
		if node.Value {
			return "true"
		}
		return "false"
	case *UnaryExpr:
		if node.Postfix {
			return fmt.Sprintf("(%s %s)", renderExpression(node.Operand), node.Operator)
		}
		return fmt.Sprintf("(%s %s)", node.Operator, renderExpression(node.Operand))
	case *BinaryExpr:
		return fmt.Sprintf("(%s %s %s)", node.Operator, renderExpression(node.Left), renderExpression(node.Right))
	case *AssignExpr:
		return fmt.Sprintf("(%s %s %s)", node.Operator, renderExpression(node.Target), renderExpression(node.Value))
	case *CallExpr:
		parts := []string{"call", renderExpression(node.Callee)}
		for _, argument := range node.Arguments {
			parts = append(parts, renderExpression(argument))
		}
		return "(" + strings.Join(parts, " ") + ")"
	case *MemberExpr:
		return fmt.Sprintf("(. %s %s)", renderExpression(node.Receiver), node.Member)
	case *StructLiteral:
		parts := []string{"structlit", node.Type.Name}
		for _, field := range node.Fields {
			parts = append(parts, fmt.Sprintf("(%s %s)", field.Name, renderExpression(field.Value)))
		}
		return "(" + strings.Join(parts, " ") + ")"
	case *VariantLiteral:
		parts := []string{"variant", node.Type.Name + "." + node.Variant}
		for _, field := range node.Fields {
			parts = append(parts, fmt.Sprintf("(%s %s)", field.Name, renderExpression(field.Value)))
		}
		return "(" + strings.Join(parts, " ") + ")"
	case *NewArrayExpr:
		return fmt.Sprintf("(new %s %s)", node.Element.String(), renderExpression(node.Length))
	case *NewMapExpr:
		return fmt.Sprintf("(newmap %s)", node.Map.String())
	case *IndexExpr:
		return fmt.Sprintf("(index %s %s)", renderExpression(node.Target), renderExpression(node.Index))
	default:
		return fmt.Sprintf("<?%T>", expression)
	}
}

func TestEmptyProgram(t *testing.T) {
	assertProgram(t, "", &Program{})
	assertProgram(t, "\n\n", &Program{})
	assertProgram(t, "// only a comment\n", &Program{})
}

func TestEmptyFunctionBody(t *testing.T) {
	assertProgram(t, "fn main() {}", &Program{Declarations: []Decl{
		&FunctionDecl{
			At:   at(1, 1),
			Name: "main",
			Body: &BlockStmt{At: at(1, 11)},
		},
	}})
}

func TestFunctionDeclaration(t *testing.T) {
	source := `fn add(int left, int right) -> int {
    return left + right;
}
`
	assertProgram(t, source, &Program{Declarations: []Decl{
		&FunctionDecl{
			At:   at(1, 1),
			Name: "add",
			Parameters: []*Parameter{
				{At: at(1, 8), Name: "left", Type: &TypeRef{At: at(1, 8), Name: "int"}},
				{At: at(1, 18), Name: "right", Type: &TypeRef{At: at(1, 18), Name: "int"}},
			},
			ReturnType: &TypeRef{At: at(1, 32), Name: "int"},
			Body: &BlockStmt{At: at(1, 36), Statements: []Stmt{
				&ReturnStmt{
					At: at(2, 5),
					Value: &BinaryExpr{
						OperatorAt: at(2, 17),
						Operator:   "+",
						Left:       &Identifier{At: at(2, 12), Name: "left"},
						Right:      &Identifier{At: at(2, 19), Name: "right"},
					},
				},
			}},
		},
	}})
}

func TestFunctionWithCall(t *testing.T) {
	source := `fn main() {
    print("hello, kitten");
}
`
	assertProgram(t, source, &Program{Declarations: []Decl{
		&FunctionDecl{
			At:   at(1, 1),
			Name: "main",
			Body: &BlockStmt{At: at(1, 11), Statements: []Stmt{
				&ExprStmt{
					At: at(2, 5),
					Expression: &CallExpr{
						OpenParen: at(2, 10),
						Callee:    &Identifier{At: at(2, 5), Name: "print"},
						Arguments: []Expr{
							&StringLiteral{At: at(2, 11), Text: `"hello, kitten"`},
						},
					},
				},
			}},
		},
	}})
}

func TestStructDeclaration(t *testing.T) {
	source := `struct Rectangle {
    int width;
    int height;
}
`
	assertProgram(t, source, &Program{Declarations: []Decl{
		&StructDecl{
			At:   at(1, 1),
			Name: "Rectangle",
			Fields: []*Field{
				{At: at(2, 5), Name: "width", Type: &TypeRef{At: at(2, 5), Name: "int"}},
				{At: at(3, 5), Name: "height", Type: &TypeRef{At: at(3, 5), Name: "int"}},
			},
		},
	}})
}

func TestVisibilityModifiers(t *testing.T) {
	cases := []struct {
		source         string
		wantVisibility Visibility
	}{
		{"fn main() {}", VisibilityDefault},
		{"public fn main() {}", VisibilityPublic},
		{"private fn main() {}", VisibilityPrivate},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			program, err := ParseSource(testCase.source)
			if err != nil {
				t.Fatalf("ParseSource(%q) error: %v", testCase.source, err)
			}
			function := firstFunction(t, program)
			if function.Visibility != testCase.wantVisibility {
				t.Errorf("visibility = %v, want %v", function.Visibility, testCase.wantVisibility)
			}
			// The node position includes the modifier when one is present.
			if function.At != at(1, 1) {
				t.Errorf("position = %v, want %v", function.At, at(1, 1))
			}
		})
	}
}

func TestStructVisibilityModifier(t *testing.T) {
	program, err := ParseSource("private struct Point { int x; }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	declaration, ok := program.Declarations[0].(*StructDecl)
	if !ok {
		t.Fatalf("declaration is %T, want *StructDecl", program.Declarations[0])
	}
	if declaration.Visibility != VisibilityPrivate {
		t.Errorf("visibility = %v, want private", declaration.Visibility)
	}
	if declaration.At != at(1, 1) {
		t.Errorf("position = %v, want 1:1", declaration.At)
	}
}

func TestVariableDeclarations(t *testing.T) {
	source := `fn main() {
    int score = 5;
    float temperature = 21.5;
    bool is_ready = true;
    string username = "kitten";
}
`
	assertProgram(t, source, &Program{Declarations: []Decl{
		&FunctionDecl{
			At:   at(1, 1),
			Name: "main",
			Body: &BlockStmt{At: at(1, 11), Statements: []Stmt{
				&VarDeclStmt{At: at(2, 5), Name: "score", Type: &TypeRef{At: at(2, 5), Name: "int"}, Initializer: &IntLiteral{At: at(2, 17), Text: "5"}},
				&VarDeclStmt{At: at(3, 5), Name: "temperature", Type: &TypeRef{At: at(3, 5), Name: "float"}, Initializer: &FloatLiteral{At: at(3, 25), Text: "21.5"}},
				&VarDeclStmt{At: at(4, 5), Name: "is_ready", Type: &TypeRef{At: at(4, 5), Name: "bool"}, Initializer: &BoolLiteral{At: at(4, 21), Value: true}},
				&VarDeclStmt{At: at(5, 5), Name: "username", Type: &TypeRef{At: at(5, 5), Name: "string"}, Initializer: &StringLiteral{At: at(5, 23), Text: `"kitten"`}},
			}},
		},
	}})
}

func TestVariableDeclarationWithoutInitializer(t *testing.T) {
	// Whether an initializer is mandatory is deferred to the checker, so the
	// parser accepts a declaration with none.
	program, err := ParseSource("fn main() { int count; }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	declaration := firstFunction(t, program).Body.Statements[0].(*VarDeclStmt)
	if declaration.Initializer != nil {
		t.Errorf("initializer = %v, want nil", declaration.Initializer)
	}
}

func TestStructLiteral(t *testing.T) {
	source := `fn main() {
    Rectangle room = Rectangle {
        width = 4,
        height = 5
    };
}
`
	assertProgram(t, source, &Program{Declarations: []Decl{
		&FunctionDecl{
			At:   at(1, 1),
			Name: "main",
			Body: &BlockStmt{At: at(1, 11), Statements: []Stmt{
				&VarDeclStmt{
					At:   at(2, 5),
					Name: "room",
					Type: &TypeRef{At: at(2, 5), Name: "Rectangle"},
					Initializer: &StructLiteral{
						OpenBrace: at(2, 32),
						Type:      &TypeRef{At: at(2, 22), Name: "Rectangle"},
						Fields: []*FieldInit{
							{At: at(3, 9), Name: "width", Value: &IntLiteral{At: at(3, 17), Text: "4"}},
							{At: at(4, 9), Name: "height", Value: &IntLiteral{At: at(4, 18), Text: "5"}},
						},
					},
				},
			}},
		},
	}})
}

func TestEmptyStructLiteral(t *testing.T) {
	program, err := ParseSource("fn main() { Rectangle room = Rectangle {}; }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	declaration := firstFunction(t, program).Body.Statements[0].(*VarDeclStmt)
	literal, ok := declaration.Initializer.(*StructLiteral)
	if !ok {
		t.Fatalf("initializer is %T, want *StructLiteral", declaration.Initializer)
	}
	if len(literal.Fields) != 0 {
		t.Errorf("fields = %v, want none", literal.Fields)
	}
}

func TestReturnStatements(t *testing.T) {
	program, err := ParseSource("fn main() { return; }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	bare := firstFunction(t, program).Body.Statements[0].(*ReturnStmt)
	if bare.Value != nil {
		t.Errorf("bare return value = %v, want nil", bare.Value)
	}

	program, err = ParseSource("fn main() { return 1 + 2; }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	withValue := firstFunction(t, program).Body.Statements[0].(*ReturnStmt)
	if withValue.Value == nil {
		t.Fatalf("return value = nil, want expression")
	}
	if rendered := renderExpression(withValue.Value); rendered != "(+ 1 2)" {
		t.Errorf("return value rendered as %q, want %q", rendered, "(+ 1 2)")
	}
}

func TestControlFlowStructure(t *testing.T) {
	source := `fn main() {
    if (a) {
    } else if (b) {
    } else {
    }
    while (c) {
    }
    for (int i = 0; i < 10; i++) {
    }
}
`
	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	statements := firstFunction(t, program).Body.Statements
	if len(statements) != 3 {
		t.Fatalf("got %d statements, want 3", len(statements))
	}

	ifStatement, ok := statements[0].(*IfStmt)
	if !ok {
		t.Fatalf("statement 0 is %T, want *IfStmt", statements[0])
	}
	if renderExpression(ifStatement.Condition) != "a" {
		t.Errorf("if condition = %q, want %q", renderExpression(ifStatement.Condition), "a")
	}
	if len(ifStatement.Then.Statements) != 0 {
		t.Errorf("if body has %d statements, want 0", len(ifStatement.Then.Statements))
	}
	elseIf, ok := ifStatement.Else.(*IfStmt)
	if !ok {
		t.Fatalf("else branch is %T, want *IfStmt", ifStatement.Else)
	}
	if renderExpression(elseIf.Condition) != "b" {
		t.Errorf("else-if condition = %q, want %q", renderExpression(elseIf.Condition), "b")
	}
	if _, ok := elseIf.Else.(*BlockStmt); !ok {
		t.Fatalf("final else branch is %T, want *BlockStmt", elseIf.Else)
	}

	whileStatement, ok := statements[1].(*WhileStmt)
	if !ok {
		t.Fatalf("statement 1 is %T, want *WhileStmt", statements[1])
	}
	if renderExpression(whileStatement.Condition) != "c" {
		t.Errorf("while condition = %q, want %q", renderExpression(whileStatement.Condition), "c")
	}

	forStatement, ok := statements[2].(*ForStmt)
	if !ok {
		t.Fatalf("statement 2 is %T, want *ForStmt", statements[2])
	}
	initializer, ok := forStatement.Init.(*VarDeclStmt)
	if !ok {
		t.Fatalf("for initializer is %T, want *VarDeclStmt", forStatement.Init)
	}
	if initializer.Name != "i" || initializer.Type.Name != "int" {
		t.Errorf("for initializer = %s %s, want int i", initializer.Type.Name, initializer.Name)
	}
	if rendered := renderExpression(forStatement.Condition); rendered != "(< i 10)" {
		t.Errorf("for condition = %q, want %q", rendered, "(< i 10)")
	}
	if rendered := renderExpression(forStatement.Increment); rendered != "(i ++)" {
		t.Errorf("for increment = %q, want %q", rendered, "(i ++)")
	}
}

func TestForLoopEmptyClauses(t *testing.T) {
	program, err := ParseSource("fn main() { for (;;) { } }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	statement := firstFunction(t, program).Body.Statements[0].(*ForStmt)
	if statement.Init != nil || statement.Condition != nil || statement.Increment != nil {
		t.Errorf("empty for clauses = (%v, %v, %v), want all nil", statement.Init, statement.Condition, statement.Increment)
	}
}

func TestForLoopExpressionInitializer(t *testing.T) {
	program, err := ParseSource("fn main() { for (start; ; ) { } }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	statement := firstFunction(t, program).Body.Statements[0].(*ForStmt)
	initializer, ok := statement.Init.(*ExprStmt)
	if !ok {
		t.Fatalf("for initializer is %T, want *ExprStmt", statement.Init)
	}
	if renderExpression(initializer.Expression) != "start" {
		t.Errorf("for initializer = %q, want %q", renderExpression(initializer.Expression), "start")
	}
}

func TestForLoopAssignmentIncrement(t *testing.T) {
	program, err := ParseSource("fn main() { for (int i = 0; i < 3; i = i + 1) { } }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	statement := firstFunction(t, program).Body.Statements[0].(*ForStmt)
	if rendered := renderExpression(statement.Increment); rendered != "(= i (+ i 1))" {
		t.Errorf("for increment = %q, want %q", rendered, "(= i (+ i 1))")
	}
}

func TestNestedBlocks(t *testing.T) {
	program, err := ParseSource("fn main() { { int x = 1; } }")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	inner, ok := firstFunction(t, program).Body.Statements[0].(*BlockStmt)
	if !ok {
		t.Fatalf("statement is %T, want *BlockStmt", firstFunction(t, program).Body.Statements[0])
	}
	if len(inner.Statements) != 1 {
		t.Fatalf("inner block has %d statements, want 1", len(inner.Statements))
	}
	if _, ok := inner.Statements[0].(*VarDeclStmt); !ok {
		t.Errorf("inner statement is %T, want *VarDeclStmt", inner.Statements[0])
	}
}

func TestCallAndMemberAccess(t *testing.T) {
	expression := parseExpressionSource(t, "print(area(room), rectangle.width)")
	if rendered := renderExpression(expression); rendered != "(call print (call area room) (. rectangle width))" {
		t.Errorf("rendered as %q", rendered)
	}
}

func TestExpressionPrecedenceAndAssociativity(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		// Multiplicative binds tighter than additive.
		{"1 + 2 * 3", "(+ 1 (* 2 3))"},
		{"1 * 2 + 3", "(+ (* 1 2) 3)"},
		{"a + b * c - d / e", "(- (+ a (* b c)) (/ d e))"},

		// Left associativity for arithmetic.
		{"1 - 2 - 3", "(- (- 1 2) 3)"},
		{"1 + 2 + 3", "(+ (+ 1 2) 3)"},
		{"8 / 4 / 2", "(/ (/ 8 4) 2)"},

		// Relational binds tighter than equality.
		{"n % 2 == 0", "(== (% n 2) 0)"},
		{"1 < 2 == true", "(== (< 1 2) true)"},
		{"a < b == c < d", "(== (< a b) (< c d))"},

		// Logical AND binds tighter than OR.
		{"a || b && c", "(|| a (&& b c))"},
		{"a && b || c", "(|| (&& a b) c)"},

		// Unary binds tighter than binary.
		{"!a && b", "(&& (! a) b)"},
		{"-a * b", "(* (- a) b)"},
		{"-a + -b", "(+ (- a) (- b))"},
		{"!!a", "(! (! a))"},

		// Assignment is lowest and right-associative.
		{"a = b = c", "(= a (= b c))"},
		{"a = b + c", "(= a (+ b c))"},
		{"a = 1 + 2 * 3", "(= a (+ 1 (* 2 3)))"},
		{"x = y == z", "(= x (== y z))"},
		{"a += 1", "(+= a 1)"},
		{"a -= b * c", "(-= a (* b c))"},

		// Parentheses override precedence.
		{"(1 + 2) * 3", "(* (+ 1 2) 3)"},

		// Member access and calls are postfix and bind tightest.
		{"a.b.c", "(. (. a b) c)"},
		{"f()", "(call f)"},
		{"f(1, 2)", "(call f 1 2)"},
		{"a.b(1)", "(call (. a b) 1)"},
		{"f(1) + 2", "(+ (call f 1) 2)"},
		{"room.width * room.height", "(* (. room width) (. room height))"},
		{"value * factorial(value - 1)", "(* value (call factorial (- value 1)))"},

		// Prefix and postfix increment and decrement.
		{"i++", "(i ++)"},
		{"++i", "(++ i)"},
		{"i--", "(i --)"},
		{"--i", "(-- i)"},
		{"n++ + 1", "(+ (n ++) 1)"},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			got := renderExpression(parseExpressionSource(t, testCase.source))
			if got != testCase.want {
				t.Errorf("%q rendered as %q, want %q", testCase.source, got, testCase.want)
			}
		})
	}
}

func TestLiteralNodes(t *testing.T) {
	source := `fn main() {
    int count = 42;
    float ratio = 3.5;
    bool ready = false;
    string name = "kitten";
}
`
	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	statements := firstFunction(t, program).Body.Statements

	check := func(index int, initializer Expr) {
		t.Helper()
		if statements[index].(*VarDeclStmt).Initializer == nil {
			t.Fatalf("statement %d has no initializer", index)
		}
	}

	check(0, statements[0].(*VarDeclStmt).Initializer)
	if _, ok := statements[0].(*VarDeclStmt).Initializer.(*IntLiteral); !ok {
		t.Errorf("initializer 0 is %T, want *IntLiteral", statements[0].(*VarDeclStmt).Initializer)
	}
	if _, ok := statements[1].(*VarDeclStmt).Initializer.(*FloatLiteral); !ok {
		t.Errorf("initializer 1 is %T, want *FloatLiteral", statements[1].(*VarDeclStmt).Initializer)
	}
	if _, ok := statements[2].(*VarDeclStmt).Initializer.(*BoolLiteral); !ok {
		t.Errorf("initializer 2 is %T, want *BoolLiteral", statements[2].(*VarDeclStmt).Initializer)
	}
	if _, ok := statements[3].(*VarDeclStmt).Initializer.(*StringLiteral); !ok {
		t.Errorf("initializer 3 is %T, want *StringLiteral", statements[3].(*VarDeclStmt).Initializer)
	}
}

func TestStructLiteralExpressionRendering(t *testing.T) {
	expression := parseExpressionSource(t, "Rectangle { width = 4, height = 5 }")
	want := "(structlit Rectangle (width 4) (height 5))"
	if rendered := renderExpression(expression); rendered != want {
		t.Errorf("rendered as %q, want %q", rendered, want)
	}
}

func TestArrayExpressionSyntax(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		{"new int[3]", "(new int 3)"},
		{"new string[count]", "(new string count)"},
		{"new int[count + 1]", "(new int (+ count 1))"},
		{"new int[][3]", "(new int[] 3)"},
		{"values[0]", "(index values 0)"},
		{"values[index + 1]", "(index values (+ index 1))"},
		{"grid[0][1]", "(index (index grid 0) 1)"},
		{"values[0] + 1", "(+ (index values 0) 1)"},
		{"-values[0]", "(- (index values 0))"},
		{"values[0]++", "((index values 0) ++)"},
		{"rooms[0].width", "(. (index rooms 0) width)"},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			got := renderExpression(parseExpressionSource(t, testCase.source))
			if got != testCase.want {
				t.Errorf("%q rendered as %q, want %q", testCase.source, got, testCase.want)
			}
		})
	}
}

func TestArrayTypeDeclaration(t *testing.T) {
	program, err := ParseSource("fn main() {\n    int[] values = new int[3];\n}\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	declaration := firstFunction(t, program).Body.Statements[0].(*VarDeclStmt)
	if declaration.Type.String() != "int[]" {
		t.Errorf("declared type = %q, want %q", declaration.Type.String(), "int[]")
	}
	if declaration.Type.Element == nil || declaration.Type.Element.Name != "int" {
		t.Fatalf("declared element type = %+v, want int", declaration.Type.Element)
	}
	creation, ok := declaration.Initializer.(*NewArrayExpr)
	if !ok {
		t.Fatalf("initializer is %T, want *NewArrayExpr", declaration.Initializer)
	}
	if creation.Element.String() != "int" {
		t.Errorf("element type = %q, want int", creation.Element.String())
	}
	if literal, ok := creation.Length.(*IntLiteral); !ok || literal.Text != "3" {
		t.Errorf("length = %v, want integer literal 3", creation.Length)
	}
}

func TestNestedArrayTypeDeclaration(t *testing.T) {
	program, err := ParseSource("fn main() {\n    int[][] grid = new int[][2];\n}\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	declaration := firstFunction(t, program).Body.Statements[0].(*VarDeclStmt)
	if declaration.Type.String() != "int[][]" {
		t.Errorf("declared type = %q, want int[][]", declaration.Type.String())
	}
	creation, ok := declaration.Initializer.(*NewArrayExpr)
	if !ok {
		t.Fatalf("initializer is %T, want *NewArrayExpr", declaration.Initializer)
	}
	if creation.Element.String() != "int[]" {
		t.Errorf("element type = %q, want int[]", creation.Element.String())
	}
}

func TestArrayDeclarationVersusIndexing(t *testing.T) {
	// An identifier followed by "[]" starts a declaration; an identifier
	// followed by "[expression]" is an indexing expression statement.
	declarationProgram, err := ParseSource("fn main() {\n    Rectangle[] rooms = new Rectangle[2];\n}\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	if _, ok := firstFunction(t, declarationProgram).Body.Statements[0].(*VarDeclStmt); !ok {
		t.Fatalf("statement is %T, want *VarDeclStmt", firstFunction(t, declarationProgram).Body.Statements[0])
	}

	indexProgram, err := ParseSource("fn main() {\n    rooms[0] = rooms[1];\n}\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	if _, ok := firstFunction(t, indexProgram).Body.Statements[0].(*ExprStmt); !ok {
		t.Fatalf("statement is %T, want *ExprStmt", firstFunction(t, indexProgram).Body.Statements[0])
	}
}

func TestCommentsAreIgnored(t *testing.T) {
	source := `// leading comment
fn main() { // trailing comment
    // body comment
    print(1); // after statement
}
`
	if _, err := ParseSource(source); err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
}

func TestAssignmentTargetIsNotCheckedByParser(t *testing.T) {
	// Assignment-target validity is a semantic question and belongs to the
	// checker. The parser records the assignment and leaves the target as
	// written.
	expression := parseExpressionSource(t, "1 = 2")
	assignment, ok := expression.(*AssignExpr)
	if !ok {
		t.Fatalf("expression is %T, want *AssignExpr", expression)
	}
	if _, ok := assignment.Target.(*IntLiteral); !ok {
		t.Errorf("target is %T, want *IntLiteral", assignment.Target)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		line     int
		column   int
		contains string
	}{
		{
			name:     "missing closing brace",
			source:   "fn main() {\n    print(1);\n",
			line:     3,
			column:   1,
			contains: "expected '}'",
		},
		{
			name:     "missing semicolon",
			source:   "fn main() {\n    int x = 5\n}\n",
			line:     3,
			column:   1,
			contains: "';'",
		},
		{
			name:     "missing expression after operator",
			source:   "fn main() {\n    int x = 1 + ;\n}\n",
			line:     2,
			column:   17,
			contains: "expected an expression",
		},
		{
			name:     "unexpected token in declaration",
			source:   "int x = 5;",
			line:     1,
			column:   1,
			contains: "expected a declaration",
		},
		{
			name:     "malformed function name",
			source:   "fn 123() {\n}\n",
			line:     1,
			column:   4,
			contains: "a function name",
		},
		{
			name:     "missing closing parenthesis",
			source:   "fn main() {\n    if (1 + 2 {\n    }\n}\n",
			line:     2,
			column:   15,
			contains: "expected ')'",
		},
		{
			name:     "missing parameter type",
			source:   "fn add(123) {\n}\n",
			line:     1,
			column:   8,
			contains: "expected a type",
		},
		{
			name:     "parameter in C-style order",
			source:   "fn add(left int) {\n}\n",
			line:     1,
			column:   13,
			contains: "a parameter name",
		},
		{
			name:     "missing function body",
			source:   "fn main()",
			line:     1,
			column:   10,
			contains: "to start a block",
		},
		{
			name:     "unexpected end of input in parameter list",
			source:   "fn main(",
			line:     1,
			column:   9,
			contains: "expected a type",
		},
		{
			name:     "trailing comma in parameters",
			source:   "fn f(int a,) {\n}\n",
			line:     1,
			column:   12,
			contains: "trailing comma",
		},
		{
			name:     "trailing comma in arguments",
			source:   "fn main() { f(1,); }",
			line:     1,
			column:   17,
			contains: "trailing comma",
		},
		{
			name:     "trailing comma in struct literal",
			source:   "fn main() { Point p = Point { x = 1, }; }",
			line:     1,
			column:   38,
			contains: "trailing comma",
		},
		{
			name:     "struct field with initializer",
			source:   "struct S {\n    int x = 1;\n}\n",
			line:     2,
			column:   11,
			contains: "';'",
		},
		{
			name:     "else without if",
			source:   "fn main() {\n    else {\n    }\n}\n",
			line:     2,
			column:   5,
			contains: "expected an expression",
		},
		{
			name:     "missing struct type name",
			source:   "struct {\n}\n",
			line:     1,
			column:   8,
			contains: "a type name",
		},
		{
			name:     "missing closing struct brace",
			source:   "struct S {\n    int x;\n",
			line:     3,
			column:   1,
			contains: "expected '}'",
		},
		{
			name:     "missing member name after dot",
			source:   "fn main() { a.; }",
			line:     1,
			column:   15,
			contains: "a member name",
		},
		{
			name:     "array creation without a length bracket",
			source:   "fn main() {\n    new int;\n}\n",
			line:     2,
			column:   12,
			contains: "expected '['",
		},
		{
			name:     "array creation without a closing bracket",
			source:   "fn main() {\n    new int[3;\n}\n",
			line:     2,
			column:   14,
			contains: "expected ']'",
		},
		{
			name:     "index without a closing bracket",
			source:   "fn main() {\n    values[0;\n}\n",
			line:     2,
			column:   13,
			contains: "expected ']'",
		},
		{
			name:     "array declaration without a name",
			source:   "fn main() {\n    int[] = 1;\n}\n",
			line:     2,
			column:   11,
			contains: "an identifier",
		},
		{
			name:     "array type with a length in a declaration",
			source:   "fn main() {\n    int[n] values = new int[1];\n}\n",
			line:     2,
			column:   8,
			contains: "an identifier",
		},
		{
			name:     "import without a path",
			source:   "import;\n",
			line:     1,
			column:   7,
			contains: "a quoted import path",
		},
		{
			name:     "import with a non-string path",
			source:   "import 5;\n",
			line:     1,
			column:   8,
			contains: "a quoted import path",
		},
		{
			name:     "import without a semicolon",
			source:   "import \"a.ktn\"",
			line:     1,
			column:   15,
			contains: "';'",
		},
		{
			name:     "enum without a case",
			source:   "enum E {",
			line:     1,
			column:   9,
			contains: "a case name",
		},
		{
			name:     "trailing comma in an enum",
			source:   "enum E { A, }",
			line:     1,
			column:   13,
			contains: "trailing comma is not allowed in an enum",
		},
		{
			name:     "union without a closing brace",
			source:   "union U {",
			line:     1,
			column:   10,
			contains: "expected '}' to close the union body",
		},
		{
			name:     "match without a closing brace",
			source:   "fn main() {\n    match (x) {\n",
			line:     3,
			column:   1,
			contains: "expected '}' to close the match body",
		},
		{
			name:     "variant literal without a closing brace",
			source:   "fn main() {\n    Box b = Box.Full { value = 1;\n}\n",
			line:     2,
			column:   33,
			contains: "to close a variant literal",
		},
		{
			name:     "map type without a bracket",
			source:   "fn main() {\n    map int table;\n}\n",
			line:     2,
			column:   9,
			contains: "expected '['",
		},
		{
			name:     "map type without a closing bracket",
			source:   "fn main() {\n    map[string table;\n}\n",
			line:     2,
			column:   16,
			contains: "expected ']'",
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			assertParseError(t, testCase.source, testCase.line, testCase.column, testCase.contains)
		})
	}
}

func TestMultiLineErrorPositions(t *testing.T) {
	// A syntax error on a later line must report that line and column.
	source := "fn main() {\n    while (x) {\n        y = ;\n    }\n}\n"
	assertParseError(t, source, 3, 13, "expected an expression")
}

func TestParseSourceReportsLexicalErrors(t *testing.T) {
	_, err := ParseSource("fn main() {\n    print(\"unterminated);\n}\n")
	if err == nil {
		t.Fatalf("ParseSource succeeded, want a lexical error")
	}
	var lexicalError *lexer.Error
	if !errors.As(err, &lexicalError) {
		t.Fatalf("error is %T (%v), want *lexer.Error", err, err)
	}
}

func TestParseRejectsNonEOFStream(t *testing.T) {
	// A token stream that does not end with EOF is tolerated: the parser treats
	// the end of the slice as end of input rather than panicking.
	tokens := []lexer.Token{{Kind: lexer.Fn, Text: "fn", Line: 1, Column: 1}}
	_, err := Parse(tokens)
	if err == nil {
		t.Fatalf("Parse succeeded, want an error")
	}
}

func TestParseNeverPanics(t *testing.T) {
	inputs := []string{
		"",
		"fn",
		"fn main",
		"fn main(",
		"fn main()",
		"fn main() {",
		"fn main() { }",
		"struct",
		"struct S",
		"struct S {",
		"fn main() { if ( }",
		"fn main() { if (a) }",
		"fn main() { while (a) }",
		"fn main() { for (;;) }",
		"fn main() { for (int i = 0; ; ) }",
		"fn main() { int x = ; }",
		"fn main() { return }",
		"fn main() { a.b. }",
		"fn main() { (1 + 2; }",
		"fn main() { Point p = Point { }; }",
		"fn main() { ++; }",
		"fn main() { = 1; }",
		"}",
		")",
		"(",
		"!!",
		"-> int",
		",,,",
		"fn main() { public }",
	}

	for _, input := range inputs {
		input := input
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			// The call must not panic. Either outcome (error or program) is
			// acceptable for these malformed inputs.
			_, _ = ParseSource(input)
		})
	}
}

func TestParseFixtures(t *testing.T) {
	fixtures := []struct {
		name              string
		declarationCount  int
		firstIsStructDecl bool
	}{
		{name: "hello.ktn", declarationCount: 1},
		{name: "fizzbuzz.ktn", declarationCount: 1},
		{name: "math.ktn", declarationCount: 3},
		{name: "room.ktn", declarationCount: 3, firstIsStructDecl: true},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join("..", "tests", fixture.name)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading fixture %s: %v", path, err)
			}

			program, err := ParseSource(string(source))
			if err != nil {
				t.Fatalf("ParseSource(%s) returned error: %v", path, err)
			}
			if len(program.Declarations) != fixture.declarationCount {
				t.Fatalf("ParseSource(%s) produced %d declarations, want %d", path, len(program.Declarations), fixture.declarationCount)
			}
			if fixture.firstIsStructDecl {
				if _, ok := program.Declarations[0].(*StructDecl); !ok {
					t.Errorf("first declaration of %s is %T, want *StructDecl", path, program.Declarations[0])
				}
			}
		})
	}
}

func TestParseFixtureMathShape(t *testing.T) {
	path := filepath.Join("..", "tests", "math.ktn")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	program, err := ParseSource(string(source))
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}

	names := make([]string, 0, len(program.Declarations))
	for _, declaration := range program.Declarations {
		function, ok := declaration.(*FunctionDecl)
		if !ok {
			t.Fatalf("declaration is %T, want *FunctionDecl", declaration)
		}
		names = append(names, function.Name)
	}
	want := []string{"factorial", "collatz_steps", "main"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("function names = %v, want %v", names, want)
	}
}

func TestImportDeclaration(t *testing.T) {
	program, err := ParseSource("import \"geometry.ktn\";\nimport \"sub/text.ktn\";\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	if len(program.Declarations) != 2 {
		t.Fatalf("got %d declarations, want 2", len(program.Declarations))
	}

	first, ok := program.Declarations[0].(*ImportDecl)
	if !ok {
		t.Fatalf("declaration 0 is %T, want *ImportDecl", program.Declarations[0])
	}
	if first.Path != "geometry.ktn" {
		t.Errorf("path = %q, want %q", first.Path, "geometry.ktn")
	}
	if first.At != at(1, 1) {
		t.Errorf("position = %v, want 1:1", first.At)
	}

	second, ok := program.Declarations[1].(*ImportDecl)
	if !ok {
		t.Fatalf("declaration 1 is %T, want *ImportDecl", program.Declarations[1])
	}
	if second.Path != "sub/text.ktn" {
		t.Errorf("path = %q, want %q", second.Path, "sub/text.ktn")
	}
	if second.At != at(2, 1) {
		t.Errorf("position = %v, want 2:1", second.At)
	}
}

func TestImportPathDecodesEscapes(t *testing.T) {
	program, err := ParseSource(`import "a\\b.ktn";`)
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	importDeclaration, ok := program.Declarations[0].(*ImportDecl)
	if !ok {
		t.Fatalf("declaration is %T, want *ImportDecl", program.Declarations[0])
	}
	if importDeclaration.Path != `a\b.ktn` {
		t.Errorf("path = %q, want %q", importDeclaration.Path, `a\b.ktn`)
	}
}

func TestEnumDeclaration(t *testing.T) {
	program, err := ParseSource("enum TokenKind {\n    Identifier,\n    Integer,\n    EndOfFile\n}\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	declaration, ok := program.Declarations[0].(*EnumDecl)
	if !ok {
		t.Fatalf("declaration is %T, want *EnumDecl", program.Declarations[0])
	}
	if declaration.Name != "TokenKind" {
		t.Errorf("name = %q, want TokenKind", declaration.Name)
	}
	if declaration.At != at(1, 1) {
		t.Errorf("position = %v, want 1:1", declaration.At)
	}
	want := []string{"Identifier", "Integer", "EndOfFile"}
	if len(declaration.Cases) != len(want) {
		t.Fatalf("got %d cases, want %d", len(declaration.Cases), len(want))
	}
	for index, name := range want {
		if declaration.Cases[index].Name != name {
			t.Errorf("case %d = %q, want %q", index, declaration.Cases[index].Name, name)
		}
	}
}

func TestUnionDeclaration(t *testing.T) {
	program, err := ParseSource("union Expression {\n    IntegerLiteral { int value; }\n    Empty\n}\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	declaration, ok := program.Declarations[0].(*UnionDecl)
	if !ok {
		t.Fatalf("declaration is %T, want *UnionDecl", program.Declarations[0])
	}
	if declaration.Name != "Expression" {
		t.Errorf("name = %q, want Expression", declaration.Name)
	}
	if len(declaration.Variants) != 2 {
		t.Fatalf("got %d variants, want 2", len(declaration.Variants))
	}

	first := declaration.Variants[0]
	if first.Name != "IntegerLiteral" || len(first.Fields) != 1 {
		t.Fatalf("variant 0 = %q with %d fields, want IntegerLiteral with 1", first.Name, len(first.Fields))
	}
	if first.Fields[0].Name != "value" || first.Fields[0].Type.Name != "int" {
		t.Errorf("field 0 = %s %s, want int value", first.Fields[0].Type.Name, first.Fields[0].Name)
	}
	if second := declaration.Variants[1]; second.Name != "Empty" || len(second.Fields) != 0 {
		t.Errorf("variant 1 = %q with %d fields, want Empty with 0", second.Name, len(second.Fields))
	}
}

func TestVariantLiteralExpression(t *testing.T) {
	expression := parseExpressionSource(t, "Box.Full { value = 1 }")
	want := "(variant Box.Full (value 1))"
	if rendered := renderExpression(expression); rendered != want {
		t.Errorf("rendered as %q, want %q", rendered, want)
	}

	literal, ok := expression.(*VariantLiteral)
	if !ok {
		t.Fatalf("expression is %T, want *VariantLiteral", expression)
	}
	if literal.Type.Name != "Box" || literal.Variant != "Full" {
		t.Errorf("literal = %s.%s, want Box.Full", literal.Type.Name, literal.Variant)
	}
}

func TestMatchStatement(t *testing.T) {
	program, err := ParseSource(`fn classify(Box box) -> int {
    match (box) {
        Full(value) {
            return value;
        }
        Empty {
            return 0;
        }
    }
}
`)
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	statement, ok := firstFunction(t, program).Body.Statements[0].(*MatchStmt)
	if !ok {
		t.Fatalf("statement is %T, want *MatchStmt", firstFunction(t, program).Body.Statements[0])
	}
	if statement.At != at(2, 5) {
		t.Errorf("position = %v, want 2:5", statement.At)
	}
	if len(statement.Arms) != 2 {
		t.Fatalf("got %d arms, want 2", len(statement.Arms))
	}

	full := statement.Arms[0]
	if full.Variant != "Full" {
		t.Errorf("arm 0 variant = %q, want Full", full.Variant)
	}
	if len(full.Bindings) != 1 || full.Bindings[0] != "value" {
		t.Errorf("arm 0 bindings = %v, want [value]", full.Bindings)
	}
	if len(full.Body.Statements) != 1 {
		t.Errorf("arm 0 body has %d statements, want 1", len(full.Body.Statements))
	}

	empty := statement.Arms[1]
	if empty.Variant != "Empty" || len(empty.Bindings) != 0 {
		t.Errorf("arm 1 = %q with bindings %v, want Empty with none", empty.Variant, empty.Bindings)
	}
}

func TestMapExpressionSyntax(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		{"new map[string]int", "(newmap map[string]int)"},
		{"new map[string]string", "(newmap map[string]string)"},
		{"new map[string]int[]", "(newmap map[string]int[])"},
		{"new map[string]map[string]int", "(newmap map[string]map[string]int)"},
		{"table[key]", "(index table key)"},
		{"table[\"a\"]", `(index table "a")`},
		{"table[a + \"b\"]", `(index table (+ a "b"))`},
		{"table[key] + 1", "(+ (index table key) 1)"},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			got := renderExpression(parseExpressionSource(t, testCase.source))
			if got != testCase.want {
				t.Errorf("%q rendered as %q, want %q", testCase.source, got, testCase.want)
			}
		})
	}
}

func TestMapTypeDeclaration(t *testing.T) {
	program, err := ParseSource("fn main() {\n    map[string]int table = new map[string]int;\n}\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	declaration := firstFunction(t, program).Body.Statements[0].(*VarDeclStmt)
	if declaration.Type.String() != "map[string]int" {
		t.Errorf("declared type = %q, want map[string]int", declaration.Type.String())
	}
	if declaration.Type.MapKey == nil || declaration.Type.MapKey.Name != "string" {
		t.Fatalf("declared key type = %+v, want string", declaration.Type.MapKey)
	}
	if declaration.Type.MapValue == nil || declaration.Type.MapValue.Name != "int" {
		t.Fatalf("declared value type = %+v, want int", declaration.Type.MapValue)
	}

	creation, ok := declaration.Initializer.(*NewMapExpr)
	if !ok {
		t.Fatalf("initializer is %T, want *NewMapExpr", declaration.Initializer)
	}
	if creation.Map.String() != "map[string]int" {
		t.Errorf("creation type = %q, want map[string]int", creation.Map.String())
	}
}

func TestNestedMapTypeDeclaration(t *testing.T) {
	program, err := ParseSource("fn main() {\n    map[string]int[] groups;\n    map[string]map[string]int nested;\n}\n")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	statements := firstFunction(t, program).Body.Statements

	groups := statements[0].(*VarDeclStmt)
	if groups.Type.String() != "map[string]int[]" {
		t.Errorf("groups type = %q, want map[string]int[]", groups.Type.String())
	}
	if groups.Type.MapValue.Element == nil || groups.Type.MapValue.Element.Name != "int" {
		t.Errorf("groups value type = %q, want int[]", groups.Type.MapValue.String())
	}

	nested := statements[1].(*VarDeclStmt)
	if nested.Type.String() != "map[string]map[string]int" {
		t.Errorf("nested type = %q, want map[string]map[string]int", nested.Type.String())
	}
	if nested.Type.MapValue.MapValue == nil {
		t.Errorf("nested value type is not a map")
	}
}
