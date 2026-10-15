package lexer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// token is a small constructor that keeps the expected-token tables readable.
func token(kind Kind, text string, line, column int) Token {
	return Token{Kind: kind, Text: text, Line: line, Column: column}
}

// assertTokens lexes source and checks that it produced exactly want.
func assertTokens(t *testing.T, source string, want []Token) {
	t.Helper()

	got, err := Lex(source)
	if err != nil {
		t.Fatalf("Lex(%q) returned unexpected error: %v", source, err)
	}
	if len(got) != len(want) {
		t.Fatalf("Lex(%q) produced %d tokens, want %d\ngot:  %v\nwant: %v", source, len(got), len(want), got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("Lex(%q) token %d = %v, want %v", source, index, got[index], want[index])
		}
	}
}

// assertLexError lexes source and checks that it failed at the given position.
func assertLexError(t *testing.T, source string, wantLine, wantColumn int) *Error {
	t.Helper()

	_, err := Lex(source)
	if err == nil {
		t.Fatalf("Lex(%q) succeeded, want error at %d:%d", source, wantLine, wantColumn)
	}

	var lexError *Error
	if !errors.As(err, &lexError) {
		t.Fatalf("Lex(%q) returned %T (%v), want *Error", source, err, err)
	}
	if lexError.Line != wantLine || lexError.Column != wantColumn {
		t.Errorf("Lex(%q) reported %d:%d, want %d:%d (%v)", source, lexError.Line, lexError.Column, wantLine, wantColumn, lexError)
	}
	return lexError
}

func TestEmptyInput(t *testing.T) {
	assertTokens(t, "", []Token{token(EOF, "", 1, 1)})
}

func TestWhitespaceOnlyInput(t *testing.T) {
	// The EOF position must reflect every consumed newline and space.
	assertTokens(t, "   \n\n  ", []Token{token(EOF, "", 3, 3)})
}

func TestKeywords(t *testing.T) {
	cases := []struct {
		source string
		kind   Kind
	}{
		{"struct", Struct},
		{"if", If},
		{"else", Else},
		{"while", While},
		{"for", For},
		{"return", Return},
		{"fn", Fn},
		{"public", Public},
		{"private", Private},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			assertTokens(t, testCase.source, []Token{
				token(testCase.kind, testCase.source, 1, 1),
				token(EOF, "", 1, 1+len(testCase.source)),
			})
		})
	}
}

func TestBuiltinTypes(t *testing.T) {
	cases := []struct {
		source string
		kind   Kind
	}{
		{"int", TypeInt},
		{"float", TypeFloat},
		{"bool", TypeBool},
		{"string", TypeString},
		{"void", TypeVoid},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			assertTokens(t, testCase.source, []Token{
				token(testCase.kind, testCase.source, 1, 1),
				token(EOF, "", 1, 1+len(testCase.source)),
			})
		})
	}
}

func TestPrintIsIdentifierNotKeyword(t *testing.T) {
	assertTokens(t, "print", []Token{
		token(Ident, "print", 1, 1),
		token(EOF, "", 1, 6),
	})
}

func TestIdentifiers(t *testing.T) {
	// Both snake_case and PascalCase, plus a leading underscore, lex as the
	// same kind. Naming conventions are not enforced by the lexer.
	assertTokens(t, "total_count PascalCase _private x1", []Token{
		token(Ident, "total_count", 1, 1),
		token(Ident, "PascalCase", 1, 13),
		token(Ident, "_private", 1, 24),
		token(Ident, "x1", 1, 33),
		token(EOF, "", 1, 35),
	})
}

func TestKeywordPrefixIsIdentifier(t *testing.T) {
	// A keyword lookalike with extra characters is an ordinary identifier.
	assertTokens(t, "iffy structs w hile", []Token{
		token(Ident, "iffy", 1, 1),
		token(Ident, "structs", 1, 6),
		token(Ident, "w", 1, 14),
		token(Ident, "hile", 1, 16),
		token(EOF, "", 1, 20),
	})
}

func TestIntegerLiterals(t *testing.T) {
	assertTokens(t, "0 123 42", []Token{
		token(Int, "0", 1, 1),
		token(Int, "123", 1, 3),
		token(Int, "42", 1, 7),
		token(EOF, "", 1, 9),
	})
}

func TestFloatLiterals(t *testing.T) {
	assertTokens(t, "12.5 0.25", []Token{
		token(Float, "12.5", 1, 1),
		token(Float, "0.25", 1, 6),
		token(EOF, "", 1, 10),
	})
}

func TestDecimalPointRules(t *testing.T) {
	// "1." is an integer followed by a dot; ".5" is a dot followed by an
	// integer. A decimal point belongs to a float only when a digit follows.
	assertTokens(t, "1. .5", []Token{
		token(Int, "1", 1, 1),
		token(Dot, ".", 1, 2),
		token(Dot, ".", 1, 4),
		token(Int, "5", 1, 5),
		token(EOF, "", 1, 6),
	})
}

func TestMinusIsNotPartOfNumber(t *testing.T) {
	assertTokens(t, "-5", []Token{
		token(Minus, "-", 1, 1),
		token(Int, "5", 1, 2),
		token(EOF, "", 1, 3),
	})
}

func TestStringLiterals(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"simple", `"hello"`},
		{"empty", `""`},
		{"with spaces", `"hello, kitten"`},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			assertTokens(t, testCase.source, []Token{
				token(String, testCase.source, 1, 1),
				token(EOF, "", 1, 1+len(testCase.source)),
			})
		})
	}
}

func TestStringEscapes(t *testing.T) {
	// The token text preserves the escapes exactly as written in the source;
	// the lexer does not interpret them.
	source := `"a\nb\tc\"d\\e"`
	assertTokens(t, source, []Token{
		token(String, source, 1, 1),
		token(EOF, "", 1, 1+len(source)),
	})
}

func TestEachEscapeIndividually(t *testing.T) {
	cases := []string{
		`"\n"`,
		`"\t"`,
		`"\""`,
		`"\\"`,
	}

	for _, source := range cases {
		source := source
		t.Run(source, func(t *testing.T) {
			assertTokens(t, source, []Token{
				token(String, source, 1, 1),
				token(EOF, "", 1, 1+len(source)),
			})
		})
	}
}

func TestOperators(t *testing.T) {
	cases := []struct {
		source string
		kind   Kind
	}{
		{"+", Plus},
		{"++", Inc},
		{"+=", PlusAssign},
		{"-", Minus},
		{"--", Dec},
		{"-=", MinusAssign},
		{"->", Arrow},
		{"*", Star},
		{"/", Slash},
		{"%", Percent},
		{"=", Assign},
		{"==", Eq},
		{"!", Not},
		{"!=", Neq},
		{"<", Lt},
		{"<=", Lte},
		{">", Gt},
		{">=", Gte},
		{"&&", And},
		{"||", Or},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			// Exactly two tokens proves the operator matched as one unit.
			assertTokens(t, testCase.source, []Token{
				token(testCase.kind, testCase.source, 1, 1),
				token(EOF, "", 1, 1+len(testCase.source)),
			})
		})
	}
}

func TestLongestMatchOperators(t *testing.T) {
	// Overlapping operators must not split into shorter tokens.
	assertTokens(t, "a==b c<=d e>=f g!=h i++ j-- k+=l m-=n o->p", []Token{
		token(Ident, "a", 1, 1),
		token(Eq, "==", 1, 2),
		token(Ident, "b", 1, 4),
		token(Ident, "c", 1, 6),
		token(Lte, "<=", 1, 7),
		token(Ident, "d", 1, 9),
		token(Ident, "e", 1, 11),
		token(Gte, ">=", 1, 12),
		token(Ident, "f", 1, 14),
		token(Ident, "g", 1, 16),
		token(Neq, "!=", 1, 17),
		token(Ident, "h", 1, 19),
		token(Ident, "i", 1, 21),
		token(Inc, "++", 1, 22),
		token(Ident, "j", 1, 25),
		token(Dec, "--", 1, 26),
		token(Ident, "k", 1, 29),
		token(PlusAssign, "+=", 1, 30),
		token(Ident, "l", 1, 32),
		token(Ident, "m", 1, 34),
		token(MinusAssign, "-=", 1, 35),
		token(Ident, "n", 1, 37),
		token(Ident, "o", 1, 39),
		token(Arrow, "->", 1, 40),
		token(Ident, "p", 1, 42),
		token(EOF, "", 1, 43),
	})
}

func TestPunctuation(t *testing.T) {
	cases := []struct {
		source string
		kind   Kind
	}{
		{"(", LParen},
		{")", RParen},
		{"{", LBrace},
		{"}", RBrace},
		{"[", LBracket},
		{"]", RBracket},
		{",", Comma},
		{".", Dot},
		{";", Semicolon},
		{":", Colon},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			assertTokens(t, testCase.source, []Token{
				token(testCase.kind, testCase.source, 1, 1),
				token(EOF, "", 1, 2),
			})
		})
	}
}

func TestStructLiteralPunctuation(t *testing.T) {
	// A representative slice of struct-literal syntax exercises brackets,
	// braces, commas, dots, colons, and equals together.
	assertTokens(t, "x[0] = Room { width = 4, height = 5 };", []Token{
		token(Ident, "x", 1, 1),
		token(LBracket, "[", 1, 2),
		token(Int, "0", 1, 3),
		token(RBracket, "]", 1, 4),
		token(Assign, "=", 1, 6),
		token(Ident, "Room", 1, 8),
		token(LBrace, "{", 1, 13),
		token(Ident, "width", 1, 15),
		token(Assign, "=", 1, 21),
		token(Int, "4", 1, 23),
		token(Comma, ",", 1, 24),
		token(Ident, "height", 1, 26),
		token(Assign, "=", 1, 33),
		token(Int, "5", 1, 35),
		token(RBrace, "}", 1, 37),
		token(Semicolon, ";", 1, 38),
		token(EOF, "", 1, 39),
	})
}

func TestLineComments(t *testing.T) {
	assertTokens(t, "a // comment\nb", []Token{
		token(Ident, "a", 1, 1),
		token(Ident, "b", 2, 1),
		token(EOF, "", 2, 2),
	})
}

func TestCommentWithoutTrailingNewline(t *testing.T) {
	source := "// only a comment"
	assertTokens(t, source, []Token{
		token(EOF, "", 1, 1+len(source)),
	})
}

func TestEmptyComment(t *testing.T) {
	assertTokens(t, "//\n1", []Token{
		token(Int, "1", 2, 1),
		token(EOF, "", 2, 2),
	})
}

func TestSlashIsNotAComment(t *testing.T) {
	// A single slash is division, not the start of a comment.
	assertTokens(t, "6 / 2", []Token{
		token(Int, "6", 1, 1),
		token(Slash, "/", 1, 3),
		token(Int, "2", 1, 5),
		token(EOF, "", 1, 6),
	})
}

func TestLineAndColumnTracking(t *testing.T) {
	source := "fn main() {\n    int x = 5;\n}\n"
	assertTokens(t, source, []Token{
		token(Fn, "fn", 1, 1),
		token(Ident, "main", 1, 4),
		token(LParen, "(", 1, 8),
		token(RParen, ")", 1, 9),
		token(LBrace, "{", 1, 11),
		token(TypeInt, "int", 2, 5),
		token(Ident, "x", 2, 9),
		token(Assign, "=", 2, 11),
		token(Int, "5", 2, 13),
		token(Semicolon, ";", 2, 14),
		token(RBrace, "}", 3, 1),
		token(EOF, "", 4, 1),
	})
}

func TestUnknownCharacters(t *testing.T) {
	cases := []struct {
		source string
		line   int
		column int
	}{
		{"@", 1, 1},
		{"a @ b", 1, 3},
		{"$", 1, 1},
		{"&", 1, 1},
		{"|", 1, 1},
		{"fn main() { # }", 1, 13},
		{"a\n\t~", 2, 2},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			assertLexError(t, testCase.source, testCase.line, testCase.column)
		})
	}
}

func TestUnterminatedString(t *testing.T) {
	cases := []struct {
		source string
		line   int
		column int
	}{
		{`"abc`, 1, 1},
		{`a = "unterminated`, 1, 5},
		{`"abc\`, 1, 1},
		{`"\`, 1, 1},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.source, func(t *testing.T) {
			assertLexError(t, testCase.source, testCase.line, testCase.column)
		})
	}
}

func TestInvalidEscape(t *testing.T) {
	lexError := assertLexError(t, `"a\qb"`, 1, 4)
	if lexError.Message == "" {
		t.Errorf("expected a descriptive message, got an empty one")
	}
}

func TestNewlineInString(t *testing.T) {
	// A raw newline inside a string is an error; multiline strings are not
	// supported. The error points at the offending newline.
	assertLexError(t, "\"line\nbreak\"", 1, 6)
}

func TestErrorIsReportedOnce(t *testing.T) {
	// Lex reports the first error and returns no tokens.
	tokens, err := Lex("a @ @ b")
	if err == nil {
		t.Fatalf("Lex returned no error for malformed input")
	}
	if tokens != nil {
		t.Errorf("Lex returned tokens %v alongside an error, want nil", tokens)
	}
}

func TestEveryFixtureLexes(t *testing.T) {
	fixtures := []string{
		"hello.ktn",
		"fizzbuzz.ktn",
		"math.ktn",
		"room.ktn",
	}

	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "tests", name)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading fixture %s: %v", path, err)
			}

			tokens, err := Lex(string(source))
			if err != nil {
				t.Fatalf("Lex(%s) returned error: %v", path, err)
			}
			if len(tokens) == 0 {
				t.Fatalf("Lex(%s) returned no tokens", path)
			}
			if last := tokens[len(tokens)-1]; last.Kind != EOF {
				t.Errorf("Lex(%s) ended with %v, want EOF", path, last)
			}
		})
	}
}
