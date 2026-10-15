package lexer

import "fmt"

// Kind identifies the lexical category of a Token.
//
// Every keyword, built-in type name, operator, and punctuation mark that the
// parser may need to distinguish has its own distinct Kind. The parser is
// therefore never required to re-inspect a token's raw Text to work out which
// keyword or operator it is looking at.
type Kind int

const (
	// EOF marks the end of the input. Lex always appends exactly one EOF token
	// as the final element of its result. The EOF token's Text is empty and its
	// position is the position immediately after the last source character.
	EOF Kind = iota

	// Invalid is reserved for token kinds produced in future phases. The lexer
	// never emits it; malformed input is reported as an error instead.
	Invalid

	// Ident is a user-defined identifier, including built-in function names such
	// as "print", which is not a reserved keyword.
	Ident

	// Int is a decimal integer literal such as 0 or 123.
	Int

	// Float is a decimal floating-point literal such as 12.5 or 0.25.
	Float

	// String is a double-quoted string literal. Text preserves the original
	// source spelling, including the quotation marks and any escape sequences.
	String

	// Keywords.
	Struct
	If
	Else
	While
	For
	Return
	Fn
	New
	Public
	Private

	// Built-in type names.
	TypeInt
	TypeFloat
	TypeBool
	TypeString
	TypeVoid

	// Boolean literals.
	True
	False

	// Operators.
	Plus        // +
	Minus       // -
	Star        // *
	Slash       // /
	Percent     // %
	Assign      // =
	Eq          // ==
	Neq         // !=
	Lt          // <
	Lte         // <=
	Gt          // >
	Gte         // >=
	And         // &&
	Or          // ||
	Not         // !
	Inc         // ++
	Dec         // --
	PlusAssign  // +=
	MinusAssign // -=
	Arrow       // ->

	// Punctuation.
	LParen    // (
	RParen    // )
	LBrace    // {
	RBrace    // }
	LBracket  // [
	RBracket  // ]
	Comma     // ,
	Dot       // .
	Semicolon // ;
	Colon     // :
)

// kindNames maps each Kind to a stable, human-readable name. It is used for
// debugging, test output, and error messages.
var kindNames = map[Kind]string{
	EOF:     "EOF",
	Invalid: "Invalid",

	Ident:  "Ident",
	Int:    "Int",
	Float:  "Float",
	String: "String",

	Struct:  "Struct",
	If:      "If",
	Else:    "Else",
	While:   "While",
	For:     "For",
	Return:  "Return",
	Fn:      "Fn",
	New:     "New",
	Public:  "Public",
	Private: "Private",

	TypeInt:    "TypeInt",
	TypeFloat:  "TypeFloat",
	TypeBool:   "TypeBool",
	TypeString: "TypeString",
	TypeVoid:   "TypeVoid",

	True:  "True",
	False: "False",

	Plus:        "Plus",
	Minus:       "Minus",
	Star:        "Star",
	Slash:       "Slash",
	Percent:     "Percent",
	Assign:      "Assign",
	Eq:          "Eq",
	Neq:         "Neq",
	Lt:          "Lt",
	Lte:         "Lte",
	Gt:          "Gt",
	Gte:         "Gte",
	And:         "And",
	Or:          "Or",
	Not:         "Not",
	Inc:         "Inc",
	Dec:         "Dec",
	PlusAssign:  "PlusAssign",
	MinusAssign: "MinusAssign",
	Arrow:       "Arrow",

	LParen:    "LParen",
	RParen:    "RParen",
	LBrace:    "LBrace",
	RBrace:    "RBrace",
	LBracket:  "LBracket",
	RBracket:  "RBracket",
	Comma:     "Comma",
	Dot:       "Dot",
	Semicolon: "Semicolon",
	Colon:     "Colon",
}

// String returns the human-readable name of the kind.
func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Token is a single lexical unit produced by the lexer.
type Token struct {
	// Kind is the lexical category of the token.
	Kind Kind

	// Text is the original source spelling of the token, with no transformations
	// applied. For a string literal it includes the surrounding quotation marks
	// and any escape sequences exactly as written in the source.
	Text string

	// Line is the one-based line on which the token starts.
	Line int

	// Column is the one-based column at which the token starts, counted in
	// runes. A tab counts as a single column.
	Column int
}

// String renders the token in a compact form useful for debugging and tests.
func (t Token) String() string {
	return fmt.Sprintf("%s(%q) at %d:%d", t.Kind, t.Text, t.Line, t.Column)
}

// Error describes a lexical error and the position at which it was detected.
type Error struct {
	// Line is the one-based line of the offending input.
	Line int

	// Column is the one-based column of the offending input.
	Column int

	// Message describes what went wrong.
	Message string
}

// Error implements the error interface, formatting the position as line:column.
func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message)
}
