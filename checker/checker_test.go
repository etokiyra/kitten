package checker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kitten/lexer"
	"kitten/parser"
)

// checkSource runs source through lexing, parsing, and checking. It is the
// integration helper used by most tests.
func checkSource(t *testing.T, source string) (*Result, error) {
	t.Helper()

	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("lexing %q: %v", source, err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parsing %q: %v", source, err)
	}
	return Check(program)
}

// assertOK checks source and fails the test if it reports an error.
func assertOK(t *testing.T, source string) *Result {
	t.Helper()

	result, err := checkSource(t, source)
	if err != nil {
		t.Fatalf("Check(%q) returned unexpected error: %v", source, err)
	}
	if result == nil {
		t.Fatalf("Check(%q) returned a nil result without an error", source)
	}
	return result
}

// assertCheckError checks source and requires a *Error of the given kind at the
// given position, with a message containing contains (when non-empty).
func assertCheckError(t *testing.T, source string, kind ErrorKind, line, column int, contains string) *Error {
	t.Helper()

	result, err := checkSource(t, source)
	if err == nil {
		t.Fatalf("Check(%q) succeeded (result %v), want %s at %d:%d", source, result != nil, kind, line, column)
	}

	var semanticError *Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("Check(%q) returned %T (%v), want *checker.Error", source, err, err)
	}
	if semanticError.Kind != kind {
		t.Errorf("Check(%q) kind = %s (%d), want %s (%d): %v", source, semanticError.Kind, semanticError.Kind, kind, kind, semanticError)
	}
	if semanticError.Line != line || semanticError.Column != column {
		t.Errorf("Check(%q) error at %d:%d, want %d:%d (%v)", source, semanticError.Line, semanticError.Column, line, column, semanticError)
	}
	if contains != "" && !strings.Contains(semanticError.Message, contains) {
		t.Errorf("Check(%q) message %q does not contain %q", source, semanticError.Message, contains)
	}
	return semanticError
}

func TestEmptyProgram(t *testing.T) {
	assertOK(t, "")
	assertOK(t, "\n\n")
	assertOK(t, "// only a comment\n")
}

func TestValidPrograms(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "primitive declarations",
			source: `fn main() {
    int score = 5;
    float temperature = 21.5;
    bool is_ready = true;
    string username = "kitten";
}
`,
		},
		{
			name: "declaration without initializer then assignment",
			source: `fn main() {
    int count;
    count = 3;
}
`,
		},
		{
			name: "all valid operators",
			source: `fn main() {
    int a = 1 + 2 * 3 - 4 / 2 % 3;
    float f = 1.5 * 2.0 - 0.5;
    bool b = 1 < 2 && 3 >= 3 || !false;
    bool same = a == 6 != false;
    a += 2;
    a -= 1;
    a++;
    --a;
    f++;
    int negated = -a;
}
`,
		},
		{
			name: "increment and decrement in both positions",
			source: `fn main() {
    int i = 0;
    i++;
    ++i;
    i--;
    --i;
    float f = 0.0;
    f++;
    --f;
}
`,
		},
		{
			name: "control flow",
			source: `fn main() {
    int i = 0;
    if (i > 0) {
        i = 1;
    } else if (i < 0) {
        i = 2;
    } else {
        i = 3;
    }
    while (i != 10) {
        i += 1;
    }
    for (int j = 0; j < 10; j++) {
        i = j;
    }
    for (;;) {
    }
}
`,
		},
		{
			name: "nested blocks shadow",
			source: `fn main() {
    int x = 1;
    {
        int x = 2;
        print(x);
    }
    {
        int x = 3;
    }
    print(x);
}
`,
		},
		{
			name: "parameters share the body scope",
			source: `fn add(int left, int right) -> int {
    return left + right;
}
fn main() {
    print(add(1, 2));
}
`,
		},
		{
			name: "recursion",
			source: `fn factorial(int value) -> int {
    if (value <= 1) {
        return 1;
    }
    return value * factorial(value - 1);
}
`,
		},
		{
			name: "all paths return through else if",
			source: `fn sign(int n) -> int {
    if (n > 0) {
        return 1;
    } else if (n < 0) {
        return -1;
    } else {
        return 0;
    }
}
`,
		},
		{
			name: "void functions",
			source: `fn log(string message) {
    print(message);
}
fn noop() -> void {
    return;
}
fn main() {
    log("hi");
    noop();
}
`,
		},
		{
			name: "structs and nested structs",
			source: `struct Point {
    int x;
    int y;
}
struct Line {
    Point start;
    Point end;
}
fn length_squared(Line line) -> int {
    int dx = line.end.x - line.start.x;
    int dy = line.end.y - line.start.y;
    return dx * dx + dy * dy;
}
fn main() {
    Point origin = Point { x = 0, y = 0 };
    Point p = Point { x = 3, y = 4 };
    Line line = Line { start = origin, end = p };
    print("length squared:", length_squared(line));
}
`,
		},
		{
			name: "struct fields are writable",
			source: `struct Point {
    int x;
}
struct Line {
    Point start;
    Point end;
}
fn main() {
    Point p = Point { x = 1 };
    p.x = 2;
    p.x += 3;
    p.x++;
    Line line = Line { start = p, end = p };
    line.start.x = 5;
}
`,
		},
		{
			name: "print supported primitives",
			source: `fn main() {
    print("text");
    print(42);
    print(2.5);
    print(true);
    print("mixed", 1, 2.0, false);
}
`,
		},
		{
			name: "empty struct literal is fine when the struct is empty",
			source: `struct Empty {
}
fn main() {
    Empty e = Empty {};
}
`,
		},
		{
			name: "forward and mutually referring declarations",
			source: `fn main() {
    print(later());
}
fn later() -> int {
    return helper();
}
fn helper() -> int {
    return 1;
}
`,
		},
		{
			name: "visibility modifiers do not affect checking",
			source: `private struct Counter {
    int value;
}
public fn increment(Counter c) -> int {
    return c.value + 1;
}
public fn main() {
    Counter c = Counter { value = 1 };
    print(increment(c));
}
`,
		},
		{
			name: "string operations",
			source: `struct Tag {
    string label;
}
fn decorate(string text) -> string {
    return "[" + text + "]";
}
fn main() {
    string empty = "";
    string literal = "abc" + "def";
    string first = "one";
    string second = "two";
    string variables = first + second;
    string mixed_left = first + "!";
    string mixed_right = "<" + second;
    string chained = first + "-" + second + "-" + literal;
    string decorated = decorate("x");
    string appended = first;
    appended += second;
    bool same = variables == "onetwo";
    bool different = variables != literal;
    Tag tag = Tag { label = "tag" + "ged" };
    print(empty, literal, variables, mixed_left, mixed_right);
    print(chained, decorated, appended, same, different, tag.label);
}
`,
		},
		{
			name: "arrays",
			source: `struct Point {
    int x;
}
fn sum(int[] values) -> int {
    int total = 0;
    for (int index = 0; index < length(values); index++) {
        total += values[index];
    }
    return total;
}
fn make_words() -> string[] {
    string[] words = new string[2];
    words[0] = "a";
    words[1] = "b";
    return words;
}
fn main() {
    int[] values = new int[3];
    values[0] = 1;
    values[1] = 2;
    values[2] = 3;
    values[1] += 1;
    values[2]++;
    int[] empty = new int[0];
    print(sum(values), length(values), length(empty));
    int[][] grid = new int[][2];
    grid[0] = new int[1];
    grid[0][0] = 7;
    print(grid[0][0], length(grid));
    Point[] points = new Point[1];
    points[0] = Point { x = 9 };
    points[0].x = 10;
    print(points[0].x);
    string[] words = make_words();
    words[0] += "!";
    print(words[0], words[1], length(words));
}
`,
		},
		{
			name: "enums and tagged unions",
			source: `enum TokenKind {
    Identifier,
    Integer,
    EndOfFile
}
union Expression {
    Literal { int value; }
    Pair { Expression left; Expression right; }
    Missing
}
fn evaluate(Expression expression) -> int {
    match (expression) {
        Literal(value) { return value; }
        Pair(left, right) { return evaluate(left) + evaluate(right); }
        Missing { return 0; }
    }
}
fn classify(TokenKind kind) -> string {
    string text = "";
    match (kind) {
        Identifier { text = "id"; }
        Integer { text = "num"; }
        EndOfFile { text = "end"; }
    }
    return text;
}
fn main() {
    TokenKind kind = TokenKind.Integer;
    print(classify(kind), kind == TokenKind.Integer, kind != TokenKind.Identifier);
    Expression expression = Expression.Pair {
        left = Expression.Literal { value = 1 },
        right = Expression.Missing
    };
    print(evaluate(expression));
    Expression[] items = new Expression[1];
    items[0] = Expression.Literal { value = 5 };
    print(evaluate(items[0]));
}
`,
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			assertOK(t, testCase.source)
		})
	}
}

func TestErrorCases(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		kind     ErrorKind
		line     int
		column   int
		contains string
	}{
		{
			name:     "undefined variable",
			source:   "fn main() {\n    int x = y;\n}\n",
			kind:     UndefinedName,
			line:     2,
			column:   13,
			contains: "undefined name",
		},
		{
			name:     "undefined function",
			source:   "fn main() {\n    helper();\n}\n",
			kind:     UndefinedName,
			line:     2,
			column:   5,
			contains: "undefined function",
		},
		{
			name:     "undefined type in variable",
			source:   "fn main() {\n    Widget w = 1;\n}\n",
			kind:     UndefinedName,
			line:     2,
			column:   5,
			contains: "undefined type",
		},
		{
			name:     "undefined type in parameter",
			source:   "fn f(Widget w) {\n}\n",
			kind:     UndefinedName,
			line:     1,
			column:   6,
			contains: "undefined type",
		},
		{
			name:     "undefined type in return",
			source:   "fn f() -> Widget {\n}\n",
			kind:     UndefinedName,
			line:     1,
			column:   11,
			contains: "undefined type",
		},
		{
			name:     "undefined type in struct field",
			source:   "struct S {\n    Widget w;\n}\n",
			kind:     UndefinedName,
			line:     2,
			column:   5,
			contains: "undefined type",
		},
		{
			name:     "undefined struct literal type",
			source:   "fn main() {\n    Widget { x = 1 };\n}\n",
			kind:     UndefinedName,
			line:     2,
			column:   5,
			contains: "undefined type",
		},
		{
			name:     "duplicate top-level function",
			source:   "fn f() {\n}\nfn f() {\n}\n",
			kind:     DuplicateDeclaration,
			line:     3,
			column:   1,
			contains: "duplicate declaration",
		},
		{
			name:     "duplicate top-level struct",
			source:   "struct S {\n}\nstruct S {\n}\n",
			kind:     DuplicateDeclaration,
			line:     3,
			column:   1,
			contains: "duplicate declaration",
		},
		{
			name:     "struct and function share a name",
			source:   "struct S {\n}\nfn S() {\n}\n",
			kind:     DuplicateDeclaration,
			line:     3,
			column:   1,
			contains: "duplicate declaration",
		},
		{
			name:     "duplicate parameter",
			source:   "fn f(int a, int a) {\n}\n",
			kind:     DuplicateDeclaration,
			line:     1,
			column:   13,
			contains: "duplicate parameter",
		},
		{
			name:     "duplicate local in same scope",
			source:   "fn main() {\n    int x = 1;\n    int x = 2;\n}\n",
			kind:     DuplicateDeclaration,
			line:     3,
			column:   5,
			contains: "duplicate declaration",
		},
		{
			name:     "local shadows but cannot duplicate a parameter",
			source:   "fn f(int a) {\n    int a = 1;\n}\n",
			kind:     DuplicateDeclaration,
			line:     2,
			column:   5,
			contains: "duplicate declaration",
		},
		{
			name:     "use before declaration",
			source:   "fn main() {\n    int x = x;\n}\n",
			kind:     UndefinedName,
			line:     2,
			column:   13,
			contains: "undefined name",
		},
		{
			name:     "arithmetic mixes int and float",
			source:   "fn main() {\n    int x = 1 + 2.5;\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   15,
			contains: "same type",
		},
		{
			name:     "remainder on floats",
			source:   "fn main() {\n    int x = 2.5 % 1.5;\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   17,
			contains: "int operands",
		},
		{
			name:     "arithmetic on bool",
			source:   "fn main() {\n    bool b = true + false;\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   19,
			contains: "numeric operands",
		},
		{
			name:     "relational on bool",
			source:   "fn main() {\n    bool b = true < false;\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   19,
			contains: "numeric operands",
		},
		{
			name:     "relational on string",
			source:   "fn main() {\n    bool b = \"a\" < \"b\";\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   18,
			contains: "numeric operands",
		},
		{
			name:     "equality mixes int and float",
			source:   "fn main() {\n    bool b = 1 == 2.5;\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   16,
			contains: "same type",
		},
		{
			name:     "equality on structs",
			source:   "struct Point {\n    int x;\n}\nfn main() {\n    Point a = Point { x = 1 };\n    Point b = Point { x = 2 };\n    bool c = a == b;\n}\n",
			kind:     InvalidOperator,
			line:     7,
			column:   16,
			contains: "not supported",
		},
		{
			name:     "logical on int",
			source:   "fn main() {\n    bool b = 1 && 2;\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   16,
			contains: "bool operands",
		},
		{
			name:     "not on int",
			source:   "fn main() {\n    bool b = !1;\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   14,
			contains: "bool operand",
		},
		{
			name:     "negation on bool",
			source:   "fn main() {\n    int x = -true;\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   13,
			contains: "numeric operand",
		},
		{
			name:     "assignment type mismatch",
			source:   "fn main() {\n    int x = 1;\n    x = true;\n}\n",
			kind:     TypeMismatch,
			line:     3,
			column:   7,
			contains: "assignment to int",
		},
		{
			name:     "assignment to a literal",
			source:   "fn main() {\n    1 = 2;\n}\n",
			kind:     InvalidAssignment,
			line:     2,
			column:   5,
			contains: "cannot assign",
		},
		{
			name:     "assignment to a call",
			source:   "fn main() {\n    f() = 1;\n}\n",
			kind:     InvalidAssignment,
			line:     2,
			column:   5,
			contains: "cannot assign",
		},
		{
			name:     "compound assignment type mismatch",
			source:   "fn main() {\n    int x = 1;\n    x += 2.5;\n}\n",
			kind:     TypeMismatch,
			line:     3,
			column:   7,
			contains: "same type",
		},
		{
			name:     "subtraction compound assignment on string",
			source:   "fn main() {\n    string s = \"a\";\n    s -= \"b\";\n}\n",
			kind:     InvalidOperator,
			line:     3,
			column:   7,
			contains: "numeric operands",
		},
		{
			name:     "concatenate string and int",
			source:   "fn main() {\n    string s = \"a\" + 1;\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   20,
			contains: "same type",
		},
		{
			name:     "concatenate int and string",
			source:   "fn main() {\n    int x = 1 + \"a\";\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   15,
			contains: "same type",
		},
		{
			name:     "concatenate string and bool",
			source:   "fn main() {\n    string s = \"a\" + true;\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   20,
			contains: "same type",
		},
		{
			name:     "subtract strings",
			source:   "fn main() {\n    string s = \"a\" - \"b\";\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   20,
			contains: "numeric operands",
		},
		{
			name:     "multiply strings",
			source:   "fn main() {\n    string s = \"a\" * \"b\";\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   20,
			contains: "numeric operands",
		},
		{
			name:     "remainder of strings",
			source:   "fn main() {\n    string s = \"a\" % \"b\";\n}\n",
			kind:     InvalidOperator,
			line:     2,
			column:   20,
			contains: "int operands",
		},
		{
			name:     "compound append to a string literal",
			source:   "fn main() {\n    \"a\" += \"b\";\n}\n",
			kind:     InvalidAssignment,
			line:     2,
			column:   5,
			contains: "cannot assign",
		},
		{
			name:     "concatenate struct and string",
			source:   "struct S {\n    int x;\n}\nfn main() {\n    S s = S { x = 1 };\n    string t = s + \"a\";\n}\n",
			kind:     TypeMismatch,
			line:     6,
			column:   18,
			contains: "same type",
		},
		{
			name:     "index a non-array",
			source:   "fn main() {\n    int x = 1;\n    int y = x[0];\n}\n",
			kind:     TypeMismatch,
			line:     3,
			column:   14,
			contains: "cannot index",
		},
		{
			name:     "index with a non-int",
			source:   "fn main() {\n    int[] values = new int[2];\n    int y = values[true];\n}\n",
			kind:     TypeMismatch,
			line:     3,
			column:   20,
			contains: "index must be int",
		},
		{
			name:     "length of a non-array",
			source:   "fn main() {\n    int n = length(1);\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   20,
			contains: "length expects an array",
		},
		{
			name:     "length with no arguments",
			source:   "fn main() {\n    int[] values = new int[1];\n    int n = length();\n}\n",
			kind:     BadCall,
			line:     3,
			column:   19,
			contains: "expects 1 argument",
		},
		{
			name:     "array length is not an int",
			source:   "fn main() {\n    int[] values = new int[true];\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   28,
			contains: "array length must be int",
		},
		{
			name:     "array element type is void",
			source:   "fn main() {\n    new void[3];\n}\n",
			kind:     InvalidType,
			line:     2,
			column:   9,
			contains: "element type cannot be void",
		},
		{
			name:     "void array type",
			source:   "fn main() {\n    void[] items;\n}\n",
			kind:     InvalidType,
			line:     2,
			column:   5,
			contains: "element type cannot be void",
		},
		{
			name:     "array equality",
			source:   "fn main() {\n    int[] a = new int[1];\n    int[] b = new int[1];\n    bool c = a == b;\n}\n",
			kind:     InvalidOperator,
			line:     4,
			column:   16,
			contains: "not supported",
		},
		{
			name:     "assign to an element of a non-lvalue",
			source:   "fn f() -> int[] {\n    return new int[1];\n}\nfn main() {\n    f()[0] = 1;\n}\n",
			kind:     InvalidAssignment,
			line:     5,
			column:   5,
			contains: "cannot assign",
		},
		{
			name:     "print an array",
			source:   "fn main() {\n    int[] values = new int[1];\n    print(values);\n}\n",
			kind:     TypeMismatch,
			line:     3,
			column:   11,
			contains: "cannot print",
		},
		{
			name:     "length used as a value",
			source:   "fn main() {\n    int n = length;\n}\n",
			kind:     InvalidExpression,
			line:     2,
			column:   13,
			contains: "cannot be used as a value",
		},
		{
			name:     "arithmetic on arrays",
			source:   "fn main() {\n    int[] a = new int[1];\n    int[] b = a + a;\n}\n",
			kind:     InvalidOperator,
			line:     3,
			column:   17,
			contains: "numeric operands",
		},
		{
			name:     "undefined array element type",
			source:   "fn main() {\n    Widget[] items;\n}\n",
			kind:     UndefinedName,
			line:     2,
			column:   5,
			contains: "undefined type",
		},
		{
			name:     "non-exhaustive match",
			source:   "enum Kind {\n    A,\n    B\n}\nfn main() {\n    Kind kind = Kind.A;\n    match (kind) {\n        A { print(\"a\"); }\n    }\n}\n",
			kind:     InvalidType,
			line:     7,
			column:   5,
			contains: "does not handle variant",
		},
		{
			name:     "duplicate match arm",
			source:   "enum Kind {\n    A\n}\nfn main() {\n    Kind kind = Kind.A;\n    match (kind) {\n        A { print(\"a\"); }\n        A { print(\"b\"); }\n    }\n}\n",
			kind:     DuplicateDeclaration,
			line:     8,
			column:   9,
			contains: "matched more than once",
		},
		{
			name:     "match arm binding count",
			source:   "union Box {\n    Full { int value; }\n}\nfn main() {\n    Box box = Box.Full { value = 1 };\n    match (box) {\n        Full(a, b) { print(a); }\n    }\n}\n",
			kind:     BadCall,
			line:     7,
			column:   9,
			contains: "binds 2",
		},
		{
			name:     "unknown variant in match",
			source:   "union Box {\n    Full { int value; }\n}\nfn main() {\n    Box box = Box.Full { value = 1 };\n    match (box) {\n        Nope { print(\"x\"); }\n        Full(value) { print(value); }\n    }\n}\n",
			kind:     UndefinedName,
			line:     7,
			column:   9,
			contains: "has no variant",
		},
		{
			name:     "duplicate match binding",
			source:   "union Box {\n    Full { int value; int other; }\n}\nfn main() {\n    Box box = Box.Full { value = 1, other = 2 };\n    match (box) {\n        Full(value, value) { print(value); }\n    }\n}\n",
			kind:     DuplicateDeclaration,
			line:     7,
			column:   21,
			contains: "duplicate binding",
		},
		{
			name:     "match on a non-variant",
			source:   "fn main() {\n    int x = 1;\n    match (x) {\n        A { print(1); }\n    }\n}\n",
			kind:     TypeMismatch,
			line:     3,
			column:   12,
			contains: "match requires an enum or union value",
		},
		{
			name:     "field access on a union",
			source:   "union Box {\n    Full { int value; }\n}\nfn main() {\n    Box box = Box.Full { value = 1 };\n    print(box.value);\n}\n",
			kind:     TypeMismatch,
			line:     6,
			column:   14,
			contains: "use match",
		},
		{
			name:     "union equality",
			source:   "union Box {\n    Empty\n}\nfn main() {\n    Box a = Box.Empty;\n    Box b = Box.Empty;\n    bool same = a == b;\n}\n",
			kind:     InvalidOperator,
			line:     7,
			column:   19,
			contains: "not supported",
		},
		{
			name:     "enum printed",
			source:   "enum Kind {\n    A\n}\nfn main() {\n    print(Kind.A);\n}\n",
			kind:     TypeMismatch,
			line:     5,
			column:   11,
			contains: "cannot print",
		},
		{
			name:     "enum converted to int",
			source:   "enum Kind {\n    A\n}\nfn main() {\n    int value = Kind.A;\n}\n",
			kind:     TypeMismatch,
			line:     5,
			column:   17,
			contains: "expected int, found Kind",
		},
		{
			name:     "duplicate enum case",
			source:   "enum Kind {\n    A,\n    A\n}\nfn main() {\n}\n",
			kind:     DuplicateDeclaration,
			line:     3,
			column:   5,
			contains: "duplicate case",
		},
		{
			name:     "duplicate union variant",
			source:   "union Box {\n    Full { int value; }\n    Full { int other; }\n}\nfn main() {\n}\n",
			kind:     DuplicateDeclaration,
			line:     3,
			column:   5,
			contains: "duplicate variant",
		},
		{
			name:     "duplicate variant field",
			source:   "union Box {\n    Full {\n        int value;\n        int value;\n    }\n}\nfn main() {\n}\n",
			kind:     DuplicateDeclaration,
			line:     4,
			column:   9,
			contains: "duplicate field",
		},
		{
			name:     "empty enum",
			source:   "enum Kind {\n}\nfn main() {\n}\n",
			kind:     InvalidType,
			line:     1,
			column:   1,
			contains: "at least one case",
		},
		{
			name:     "empty union",
			source:   "union Box {\n}\nfn main() {\n}\n",
			kind:     InvalidType,
			line:     1,
			column:   1,
			contains: "at least one variant",
		},
		{
			name:     "unknown field in variant literal",
			source:   "union Box {\n    Full { int value; }\n}\nfn main() {\n    Box box = Box.Full { other = 1 };\n}\n",
			kind:     UndefinedName,
			line:     5,
			column:   26,
			contains: "has no field",
		},
		{
			name:     "missing field in variant literal",
			source:   "union Box {\n    Full {\n        int value;\n        int other;\n    }\n}\nfn main() {\n    Box box = Box.Full { value = 1 };\n}\n",
			kind:     TypeMismatch,
			line:     8,
			column:   24,
			contains: "missing field",
		},
		{
			name:     "payload variant referenced without braces",
			source:   "union Box {\n    Full { int value; }\n}\nfn main() {\n    Box box = Box.Full;\n}\n",
			kind:     InvalidType,
			line:     5,
			column:   18,
			contains: "must be constructed",
		},
		{
			name:     "payload-less variant constructed with braces",
			source:   "union Box {\n    Empty\n}\nfn main() {\n    Box box = Box.Empty { };\n}\n",
			kind:     InvalidType,
			line:     5,
			column:   25,
			contains: "written without braces",
		},
		{
			name:     "increment on string",
			source:   "fn main() {\n    string s = \"a\";\n    s++;\n}\n",
			kind:     InvalidAssignment,
			line:     3,
			column:   6,
			contains: "numeric",
		},
		{
			name:     "increment on a literal",
			source:   "fn main() {\n    1++;\n}\n",
			kind:     InvalidAssignment,
			line:     2,
			column:   5,
			contains: "cannot assign",
		},
		{
			name:     "wrong argument count",
			source:   "fn f(int a) {\n}\nfn main() {\n    f();\n}\n",
			kind:     BadCall,
			line:     4,
			column:   6,
			contains: "expects 1 argument",
		},
		{
			name:     "wrong argument type",
			source:   "fn f(int a) {\n}\nfn main() {\n    f(true);\n}\n",
			kind:     TypeMismatch,
			line:     4,
			column:   7,
			contains: "argument 1 of \"f\"",
		},
		{
			name:     "calling a variable",
			source:   "fn main() {\n    int x = 1;\n    x();\n}\n",
			kind:     BadCall,
			line:     3,
			column:   5,
			contains: "not a function",
		},
		{
			name:     "calling a type",
			source:   "struct Point {\n    int x;\n}\nfn main() {\n    Point();\n}\n",
			kind:     BadCall,
			line:     5,
			column:   5,
			contains: "not a function",
		},
		{
			name:     "if condition not bool",
			source:   "fn main() {\n    if (1) {\n    }\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   9,
			contains: "if condition",
		},
		{
			name:     "while condition not bool",
			source:   "fn main() {\n    while (1) {\n    }\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   12,
			contains: "while condition",
		},
		{
			name:     "for condition not bool",
			source:   "fn main() {\n    for (; 1; ) {\n    }\n}\n",
			kind:     TypeMismatch,
			line:     2,
			column:   12,
			contains: "for condition",
		},
		{
			name:     "bare return in a value function",
			source:   "fn f() -> int {\n    return;\n}\n",
			kind:     InvalidReturn,
			line:     2,
			column:   5,
			contains: "expected a return value",
		},
		{
			name:     "value returned from a void function",
			source:   "fn f() {\n    return 1;\n}\n",
			kind:     InvalidReturn,
			line:     2,
			column:   12,
			contains: "returns no value",
		},
		{
			name:     "value returned from an explicit void function",
			source:   "fn f() -> void {\n    return 1;\n}\n",
			kind:     InvalidReturn,
			line:     2,
			column:   12,
			contains: "returns no value",
		},
		{
			name:     "return value type mismatch",
			source:   "fn f() -> int {\n    return true;\n}\n",
			kind:     InvalidReturn,
			line:     2,
			column:   12,
			contains: "return value",
		},
		{
			name:     "missing return",
			source:   "fn f() -> int {\n    int x = 1;\n}\n",
			kind:     InvalidReturn,
			line:     1,
			column:   1,
			contains: "must return a value",
		},
		{
			name:     "if without else may not return",
			source:   "fn f(int a) -> int {\n    if (a > 0) {\n        return 1;\n    }\n}\n",
			kind:     InvalidReturn,
			line:     1,
			column:   1,
			contains: "must return a value",
		},
		{
			name:     "void variable",
			source:   "fn main() {\n    void v = 1;\n}\n",
			kind:     InvalidType,
			line:     2,
			column:   5,
			contains: "cannot have type void",
		},
		{
			name:     "void parameter",
			source:   "fn f(void v) {\n}\n",
			kind:     InvalidType,
			line:     1,
			column:   6,
			contains: "cannot have type void",
		},
		{
			name:     "void struct field",
			source:   "struct S {\n    void v;\n}\n",
			kind:     InvalidType,
			line:     2,
			column:   5,
			contains: "cannot have type void",
		},
		{
			name:     "duplicate struct field",
			source:   "struct S {\n    int x;\n    int x;\n}\n",
			kind:     DuplicateDeclaration,
			line:     3,
			column:   5,
			contains: "duplicate field",
		},
		{
			name:     "unknown field in struct literal",
			source:   "struct Point {\n    int x;\n}\nfn main() {\n    Point p = Point { y = 1 };\n}\n",
			kind:     UndefinedName,
			line:     5,
			column:   23,
			contains: "no field",
		},
		{
			name:     "duplicate field initializer",
			source:   "struct Point {\n    int x;\n}\nfn main() {\n    Point p = Point { x = 1, x = 2 };\n}\n",
			kind:     DuplicateDeclaration,
			line:     5,
			column:   30,
			contains: "more than once",
		},
		{
			name:     "missing field in struct literal",
			source:   "struct Point {\n    int x;\n    int y;\n}\nfn main() {\n    Point p = Point { x = 1 };\n}\n",
			kind:     TypeMismatch,
			line:     6,
			column:   21,
			contains: "missing field",
		},
		{
			name:     "field initializer type mismatch",
			source:   "struct Point {\n    int x;\n}\nfn main() {\n    Point p = Point { x = true };\n}\n",
			kind:     TypeMismatch,
			line:     5,
			column:   27,
			contains: "field \"x\"",
		},
		{
			name:     "member access on a primitive",
			source:   "fn main() {\n    int x = 1;\n    int y = x.width;\n}\n",
			kind:     TypeMismatch,
			line:     3,
			column:   14,
			contains: "cannot access member",
		},
		{
			name:     "unknown member",
			source:   "struct Point {\n    int x;\n}\nfn main() {\n    Point p = Point { x = 1 };\n    int y = p.z;\n}\n",
			kind:     UndefinedName,
			line:     6,
			column:   14,
			contains: "no field",
		},
		{
			name:     "member access on a void call",
			source:   "fn f() {\n}\nfn main() {\n    int x = f().y;\n}\n",
			kind:     TypeMismatch,
			line:     4,
			column:   16,
			contains: "cannot access member",
		},
		{
			name:     "print with no arguments",
			source:   "fn main() {\n    print();\n}\n",
			kind:     BadCall,
			line:     2,
			column:   10,
			contains: "at least one argument",
		},
		{
			name:     "print a struct",
			source:   "struct Point {\n    int x;\n}\nfn main() {\n    Point p = Point { x = 1 };\n    print(p);\n}\n",
			kind:     TypeMismatch,
			line:     6,
			column:   11,
			contains: "cannot print",
		},
		{
			name:     "void call used as a value",
			source:   "fn f() {\n}\nfn main() {\n    int x = f();\n}\n",
			kind:     TypeMismatch,
			line:     4,
			column:   13,
			contains: "void value",
		},
		{
			name:     "function used as a value",
			source:   "fn f() {\n}\nfn main() {\n    int x = f;\n}\n",
			kind:     InvalidExpression,
			line:     4,
			column:   13,
			contains: "cannot be used as a value",
		},
		{
			name:     "type used as a value",
			source:   "struct Point {\n    int x;\n}\nfn main() {\n    int x = Point;\n}\n",
			kind:     InvalidExpression,
			line:     5,
			column:   13,
			contains: "cannot be used as a value",
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			assertCheckError(t, testCase.source, testCase.kind, testCase.line, testCase.column, testCase.contains)
		})
	}
}

func TestUserDefinedPrintShadowsBuiltin(t *testing.T) {
	// A user-defined print takes precedence over the built-in.
	assertOK(t, "fn print(int value) {\n}\nfn main() {\n    print(1);\n}\n")

	// The user's print accepts only int, so a string argument is rejected,
	// proving the user function, not the variadic built-in, was resolved.
	assertCheckError(t, "fn print(int value) {\n}\nfn main() {\n    print(\"x\");\n}\n", TypeMismatch, 4, 11, "argument 1")
}

func TestFixtures(t *testing.T) {
	fixtures := []string{"hello.ktn", "fizzbuzz.ktn", "math.ktn", "room.ktn"}

	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "tests", name)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading fixture %s: %v", path, err)
			}
			if _, err := checkSource(t, string(source)); err != nil {
				t.Fatalf("Check(%s) returned error: %v", path, err)
			}
		})
	}
}

func TestResultRecordsTypesAndSymbols(t *testing.T) {
	source := `fn add(int left, int right) -> int {
    return left + right;
}
fn main() {
    int total = add(1, 2);
    print(total);
}
`
	result := assertOK(t, source)

	// Function signatures.
	addFunction, ok := result.Functions["add"]
	if !ok {
		t.Fatalf("Functions has no entry for add")
	}
	wantParameters := []*Type{IntType, IntType}
	if !reflect.DeepEqual(addFunction.ParameterTypes, wantParameters) {
		t.Errorf("add parameter types = %v, want %v", addFunction.ParameterTypes, wantParameters)
	}
	if addFunction.ReturnType != IntType {
		t.Errorf("add return type = %v, want int", addFunction.ReturnType)
	}

	// Built-ins.
	if result.Builtins["print"] == nil {
		t.Errorf("Builtins has no entry for print")
	}

	// Expression types, reached by walking the AST.
	mainFunction := result.Modules[0].Program.Declarations[1].(*parser.FunctionDecl)
	declaration := mainFunction.Body.Statements[0].(*parser.VarDeclStmt)
	call := declaration.Initializer.(*parser.CallExpr)
	if result.ExpressionTypes[call] != IntType {
		t.Errorf("call type = %v, want int", result.ExpressionTypes[call])
	}

	// Symbol resolution.
	printStatement := mainFunction.Body.Statements[1].(*parser.ExprStmt)
	printCall := printStatement.Expression.(*parser.CallExpr)
	totalArgument := printCall.Arguments[0].(*parser.Identifier)
	symbol, ok := result.Symbols[totalArgument]
	if !ok {
		t.Fatalf("Symbols has no entry for the total argument")
	}
	if symbol.Kind != SymbolVariable || symbol.Type != IntType {
		t.Errorf("total symbol = %+v, want a variable of type int", symbol)
	}

	// Declared type references are recorded for the backend.
	totalDeclaration := mainFunction.Body.Statements[0].(*parser.VarDeclStmt)
	if resolved := result.TypeRefs[totalDeclaration.Type]; resolved != IntType {
		t.Errorf("declared type of total = %v, want int", resolved)
	}
}

func TestNoStateLeakBetweenChecks(t *testing.T) {
	// The first program defines helper; the second must not see it.
	assertOK(t, "fn helper() {\n}\nfn main() {\n    helper();\n}\n")
	assertCheckError(t, "fn main() {\n    helper();\n}\n", UndefinedName, 2, 5, "undefined function")

	// A struct defined in one program must not leak into the next.
	assertOK(t, "struct Point {\n    int x;\n}\n")
	assertCheckError(t, "fn main() {\n    Point p = Point { x = 1 };\n}\n", UndefinedName, 2, 5, "undefined type")
}

func TestCheckIsDeterministic(t *testing.T) {
	source := `struct Point {
    int x;
}
fn area(Point p) -> int {
    return p.x * p.x;
}
fn main() {
    Point p = Point { x = 3 };
    print(area(p));
}
`
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("lexing: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	first, err := Check(program)
	if err != nil {
		t.Fatalf("first Check: %v", err)
	}
	second, err := Check(program)
	if err != nil {
		t.Fatalf("second Check: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("successive checks of the same program differ")
	}
}

// functionWithBody builds a minimal function declaration for the malformed-AST
// tests.
func functionWithBody(name string, statements ...parser.Stmt) *parser.FunctionDecl {
	return &parser.FunctionDecl{
		At:   parser.Position{Line: 1, Column: 1},
		Name: name,
		Body: &parser.BlockStmt{At: parser.Position{Line: 1, Column: 11}, Statements: statements},
	}
}

func TestMalformedProgramsReturnErrorsNotPanics(t *testing.T) {
	cases := []struct {
		name    string
		program *parser.Program
	}{
		{
			name:    "nil program",
			program: nil,
		},
		{
			name:    "nil declaration",
			program: &parser.Program{Declarations: []parser.Decl{nil}},
		},
		{
			name: "function without body",
			program: &parser.Program{Declarations: []parser.Decl{
				&parser.FunctionDecl{At: parser.Position{Line: 1, Column: 1}, Name: "f"},
			}},
		},
		{
			name: "nil parameter",
			program: &parser.Program{Declarations: []parser.Decl{
				&parser.FunctionDecl{
					At:         parser.Position{Line: 1, Column: 1},
					Name:       "f",
					Parameters: []*parser.Parameter{nil},
					Body:       &parser.BlockStmt{At: parser.Position{Line: 1, Column: 14}},
				},
			}},
		},
		{
			name: "nil struct field",
			program: &parser.Program{Declarations: []parser.Decl{
				&parser.StructDecl{
					At:     parser.Position{Line: 1, Column: 1},
					Name:   "S",
					Fields: []*parser.Field{nil},
				},
			}},
		},
		{
			name: "variable declaration without type",
			program: &parser.Program{Declarations: []parser.Decl{
				functionWithBody("main", &parser.VarDeclStmt{At: parser.Position{Line: 2, Column: 5}, Name: "x"}),
			}},
		},
		{
			name: "nil statement",
			program: &parser.Program{Declarations: []parser.Decl{
				functionWithBody("main", nil),
			}},
		},
		{
			name: "expression statement without expression",
			program: &parser.Program{Declarations: []parser.Decl{
				functionWithBody("main", &parser.ExprStmt{At: parser.Position{Line: 2, Column: 5}}),
			}},
		},
		{
			name: "struct literal without a type",
			program: &parser.Program{Declarations: []parser.Decl{
				functionWithBody("main", &parser.ExprStmt{
					At:         parser.Position{Line: 2, Column: 5},
					Expression: &parser.StructLiteral{OpenBrace: parser.Position{Line: 2, Column: 12}},
				}),
			}},
		},
		{
			name: "struct literal naming a primitive",
			program: &parser.Program{Declarations: []parser.Decl{
				functionWithBody("main", &parser.ExprStmt{
					At: parser.Position{Line: 2, Column: 5},
					Expression: &parser.StructLiteral{
						OpenBrace: parser.Position{Line: 2, Column: 9},
						Type:      &parser.TypeRef{At: parser.Position{Line: 2, Column: 5}, Name: "int"},
					},
				}),
			}},
		},
		{
			name: "value returned from an implicitly void function",
			program: &parser.Program{Declarations: []parser.Decl{
				&parser.FunctionDecl{
					At:   parser.Position{Line: 1, Column: 1},
					Name: "main",
					Body: &parser.BlockStmt{
						At: parser.Position{Line: 1, Column: 11},
						Statements: []parser.Stmt{
							&parser.ReturnStmt{
								At:    parser.Position{Line: 2, Column: 5},
								Value: &parser.IntLiteral{At: parser.Position{Line: 2, Column: 12}, Text: "1"},
							},
						},
					},
				},
			}},
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			result, err := Check(testCase.program)
			if err == nil {
				t.Fatalf("Check succeeded (result %v), want an error", result != nil)
			}
			var semanticError *Error
			if !errors.As(err, &semanticError) {
				t.Fatalf("error is %T (%v), want *checker.Error", err, err)
			}
			if semanticError.Message == "" {
				t.Errorf("error message is empty")
			}
		})
	}
}

// modulesFromSources parses each source into a module, numbered by its position.
func modulesFromSources(t *testing.T, sources ...string) []*parser.Module {
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
	return modules
}

func assertModulesOK(t *testing.T, sources ...string) *Result {
	t.Helper()

	result, err := CheckModules(modulesFromSources(t, sources...))
	if err != nil {
		t.Fatalf("CheckModules returned unexpected error: %v", err)
	}
	return result
}

func assertModuleError(t *testing.T, kind ErrorKind, contains string, sources ...string) *Error {
	t.Helper()

	_, err := CheckModules(modulesFromSources(t, sources...))
	if err == nil {
		t.Fatalf("CheckModules succeeded, want %s", kind)
	}
	var checkError *Error
	if !errors.As(err, &checkError) {
		t.Fatalf("error is %T (%v), want *checker.Error", err, err)
	}
	if checkError.Kind != kind {
		t.Errorf("error kind = %s, want %s (%v)", checkError.Kind, kind, checkError)
	}
	if contains != "" && !strings.Contains(checkError.Message, contains) {
		t.Errorf("message %q does not contain %q", checkError.Message, contains)
	}
	return checkError
}

func TestModulesResolvePublicDeclarations(t *testing.T) {
	result := assertModulesOK(t,
		`fn main() {
    Rectangle room = Rectangle { width = 3, height = 4 };
    print(label(), area(room));
}
`,
		`public struct Rectangle {
    int width;
    int height;
}
public fn label() -> string {
    return "lib";
}
public fn area(Rectangle rectangle) -> int {
    return rectangle.width * rectangle.height;
}
`,
	)

	// The root module's by-name view sees the imported public declarations.
	if result.Types["Rectangle"] == nil {
		t.Errorf("Rectangle is not visible to the root module")
	}
	if result.Functions["area"] == nil {
		t.Errorf("area is not visible to the root module")
	}
	if len(result.Modules) != 2 {
		t.Errorf("result has %d modules, want 2", len(result.Modules))
	}
}

func TestModuleUsesItsOwnPrivateDeclarations(t *testing.T) {
	assertModulesOK(t,
		`fn main() {
    print(helper());
}
private fn helper() -> int {
    return 1;
}
`,
	)
}

func TestModulePrivateNamesDoNotCollideAcrossModules(t *testing.T) {
	// Two modules may each declare their own private helper with different
	// signatures, because private names are module-local.
	assertModulesOK(t,
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
}

func TestModulePublicFunctionCallsAnotherModule(t *testing.T) {
	assertModulesOK(t,
		`import "lib.ktn";
fn main() {
    print(combined());
}
`,
		`public fn combined() -> int {
    return base() + extra();
}
public fn base() -> int {
    return 2;
}
private fn extra() -> int {
    return 3;
}
`,
	)
}

func TestModulePrivateFunctionAccessIsRejected(t *testing.T) {
	checkError := assertModuleError(t, PrivateAccess, "private to", `fn main() {
    print(secret());
}
`,
		`private fn secret() -> int {
    return 1;
}
`,
	)
	if checkError.File != "module0.ktn" {
		t.Errorf("error file = %q, want module0.ktn", checkError.File)
	}
}

func TestModulePrivateTypeAccessIsRejected(t *testing.T) {
	assertModuleError(t, PrivateAccess, "private to", `fn main() {
    Hidden hidden = Hidden { value = 1 };
    print(hidden.value);
}
`,
		`private struct Hidden {
    int value;
}
`,
	)
}

func TestModuleDuplicatePublicDeclarationIsRejected(t *testing.T) {
	assertModuleError(t, DuplicateDeclaration, "duplicate public declaration", `fn main() {
    print(shared());
}
`,
		`public fn shared() -> int {
    return 1;
}
`,
		`public fn shared() -> int {
    return 2;
}
`,
	)
}

func TestModuleUndefinedNameIsRejected(t *testing.T) {
	assertModuleError(t, UndefinedName, "undefined function", `fn main() {
    print(nowhere());
}
`,
	)
}

func TestModuleLocalDuplicateIsRejected(t *testing.T) {
	assertModuleError(t, DuplicateDeclaration, "duplicate declaration", `fn main() {
}
fn main() {
    print(1);
}
`,
	)
}

func TestModulesResolvePublicVariants(t *testing.T) {
	result := assertModulesOK(t,
		`fn main() {
    Kind kind = Kind.Integer;
    Box box = Box.Full { value = 3 };
    print(describe(kind), unwrap(box));
}
`,
		`public enum Kind {
    Integer,
    Text
}
public union Box {
    Full { int value; }
    Empty
}
public fn describe(Kind kind) -> string {
    match (kind) {
        Integer { return "int"; }
        Text { return "text"; }
    }
}
public fn unwrap(Box box) -> int {
    match (box) {
        Full(value) { return value; }
        Empty { return 0; }
    }
}
`,
	)

	if result.Types["Kind"] == nil {
		t.Errorf("Kind is not visible to the root module")
	}
	if result.Types["Box"] == nil {
		t.Errorf("Box is not visible to the root module")
	}
}

func TestModulePrivateEnumAccessIsRejected(t *testing.T) {
	assertModuleError(t, PrivateAccess, "private to", `fn main() {
    Kind kind = Kind.A;
}
`,
		`private enum Kind {
    A
}
`,
	)
}

func TestModulePrivateUnionAccessIsRejected(t *testing.T) {
	assertModuleError(t, PrivateAccess, "private to", `fn main() {
    Box box = Box.Empty;
}
`,
		`private union Box {
    Empty
}
`,
	)
}
