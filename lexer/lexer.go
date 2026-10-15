// Package lexer turns Kitten source text into a stream of tokens.
//
// The public entry point is Lex, which scans an entire source string and
// returns the tokens it contains, followed by a single EOF token. Lex stops at
// the first lexical error and returns it as an *Error carrying a one-based
// line:column position.
//
// The lexer performs no parsing and no semantic analysis. It recognizes the
// syntax described in SPEC.md and preserves each token's original source
// spelling in Token.Text.
package lexer

import "fmt"

// eof is the sentinel rune returned by lookahead when the input is exhausted.
// It cannot collide with any real source character.
const eof = rune(-1)

// keywords maps reserved words and built-in type names to their token kinds.
// Words absent from this map, including "print", lex as Ident.
var keywords = map[string]Kind{
	"struct":  Struct,
	"if":      If,
	"else":    Else,
	"while":   While,
	"for":     For,
	"return":  Return,
	"fn":      Fn,
	"new":     New,
	"import":  Import,
	"public":  Public,
	"private": Private,
	"enum":    Enum,
	"union":   Union,
	"match":   Match,
	"map":     Map,

	"int":    TypeInt,
	"float":  TypeFloat,
	"bool":   TypeBool,
	"string": TypeString,
	"void":   TypeVoid,

	"true":  True,
	"false": False,
}

// Lex scans source and returns every token it contains, terminated by a single
// EOF token. It returns the first lexical error encountered; on error the
// returned token slice is nil.
func Lex(source string) ([]Token, error) {
	scanner := &lexer{
		source: []rune(source),
		line:   1,
		column: 1,
	}

	tokens := make([]Token, 0, len(scanner.source)/2+1)
	for {
		token, err := scanner.scan()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
		if token.Kind == EOF {
			return tokens, nil
		}
	}
}

// lexer walks a rune slice while tracking one-based line and column positions.
type lexer struct {
	source []rune
	pos    int
	line   int
	column int
}

// scan returns the next token, skipping any leading whitespace and comments.
func (l *lexer) scan() (Token, error) {
	l.skipTrivia()
	if l.atEnd() {
		return Token{Kind: EOF, Text: "", Line: l.line, Column: l.column}, nil
	}

	startLine, startColumn := l.line, l.column
	current := l.peek()

	switch {
	case isIdentifierStart(current):
		return l.scanIdentifier(startLine, startColumn), nil
	case isDigit(current):
		return l.scanNumber(startLine, startColumn), nil
	case current == '"':
		return l.scanString(startLine, startColumn)
	default:
		return l.scanOperator(startLine, startColumn)
	}
}

// skipTrivia consumes spaces, tabs, carriage returns, newlines, and // line
// comments.
func (l *lexer) skipTrivia() {
	for !l.atEnd() {
		switch l.peek() {
		case ' ', '\t', '\r', '\n':
			l.advance()
		case '/':
			if l.peekAt(1) != '/' {
				return
			}
			l.skipLineComment()
		default:
			return
		}
	}
}

// skipLineComment consumes a // comment up to, but not including, the newline
// that ends it. The newline is left for the whitespace loop to consume so that
// line and column tracking stays in one place.
func (l *lexer) skipLineComment() {
	for !l.atEnd() && l.peek() != '\n' {
		l.advance()
	}
}

// scanIdentifier consumes an identifier or keyword.
func (l *lexer) scanIdentifier(startLine, startColumn int) Token {
	start := l.pos
	for !l.atEnd() && isIdentifierPart(l.peek()) {
		l.advance()
	}
	text := string(l.source[start:l.pos])

	kind, ok := keywords[text]
	if !ok {
		kind = Ident
	}
	return Token{Kind: kind, Text: text, Line: startLine, Column: startColumn}
}

// scanNumber consumes a decimal integer or floating-point literal.
//
// A decimal point is part of the literal only when it is immediately followed
// by a decimal digit. Thus "1." is an integer followed by a dot and ".5" is a
// dot followed by an integer.
func (l *lexer) scanNumber(startLine, startColumn int) Token {
	start := l.pos
	for !l.atEnd() && isDigit(l.peek()) {
		l.advance()
	}

	kind := Int
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		kind = Float
		l.advance() // the decimal point
		for !l.atEnd() && isDigit(l.peek()) {
			l.advance()
		}
	}

	text := string(l.source[start:l.pos])
	return Token{Kind: kind, Text: text, Line: startLine, Column: startColumn}
}

// scanString consumes a double-quoted string literal, validating escapes.
//
// The returned token's Text is the original source spelling, including the
// surrounding quotation marks and the escape sequences verbatim.
func (l *lexer) scanString(startLine, startColumn int) (Token, error) {
	start := l.pos
	l.advance() // opening quote

	for {
		if l.atEnd() {
			return Token{}, &Error{
				Line:    startLine,
				Column:  startColumn,
				Message: "unterminated string literal",
			}
		}

		switch l.peek() {
		case '"':
			l.advance() // closing quote
			text := string(l.source[start:l.pos])
			return Token{Kind: String, Text: text, Line: startLine, Column: startColumn}, nil

		case '\n':
			// Multiline strings are not supported; the newline marks the end of
			// the line on which the literal was opened.
			return Token{}, &Error{
				Line:    l.line,
				Column:  l.column,
				Message: "newline in string literal",
			}

		case '\\':
			l.advance() // the backslash
			if l.atEnd() {
				return Token{}, &Error{
					Line:    startLine,
					Column:  startColumn,
					Message: "unterminated string literal",
				}
			}
			if err := l.scanEscape(); err != nil {
				return Token{}, err
			}

		default:
			l.advance()
		}
	}
}

// scanEscape validates a single escape sequence. The leading backslash has
// already been consumed and the current rune is the escaped character.
func (l *lexer) scanEscape() error {
	escapeLine, escapeColumn := l.line, l.column
	escaped := l.advance()

	switch escaped {
	case 'n', 't', '"', '\\':
		return nil
	default:
		return &Error{
			Line:    escapeLine,
			Column:  escapeColumn,
			Message: fmt.Sprintf("invalid escape sequence \\%c", escaped),
		}
	}
}

// scanOperator consumes a single operator or punctuation mark, preferring the
// longest match so that multi-character operators such as "==" and "->" become
// a single token.
func (l *lexer) scanOperator(startLine, startColumn int) (Token, error) {
	start := l.pos
	build := func(kind Kind) Token {
		return Token{Kind: kind, Text: string(l.source[start:l.pos]), Line: startLine, Column: startColumn}
	}

	switch l.advance() {
	case '+':
		switch l.peek() {
		case '+':
			l.advance()
			return build(Inc), nil
		case '=':
			l.advance()
			return build(PlusAssign), nil
		}
		return build(Plus), nil

	case '-':
		switch l.peek() {
		case '-':
			l.advance()
			return build(Dec), nil
		case '=':
			l.advance()
			return build(MinusAssign), nil
		case '>':
			l.advance()
			return build(Arrow), nil
		}
		return build(Minus), nil

	case '*':
		return build(Star), nil
	case '/':
		return build(Slash), nil
	case '%':
		return build(Percent), nil

	case '=':
		if l.peek() == '=' {
			l.advance()
			return build(Eq), nil
		}
		return build(Assign), nil

	case '!':
		if l.peek() == '=' {
			l.advance()
			return build(Neq), nil
		}
		return build(Not), nil

	case '<':
		if l.peek() == '=' {
			l.advance()
			return build(Lte), nil
		}
		return build(Lt), nil

	case '>':
		if l.peek() == '=' {
			l.advance()
			return build(Gte), nil
		}
		return build(Gt), nil

	case '&':
		if l.peek() == '&' {
			l.advance()
			return build(And), nil
		}
		return Token{}, l.unexpected(startLine, startColumn, '&')

	case '|':
		if l.peek() == '|' {
			l.advance()
			return build(Or), nil
		}
		return Token{}, l.unexpected(startLine, startColumn, '|')

	case '(':
		return build(LParen), nil
	case ')':
		return build(RParen), nil
	case '{':
		return build(LBrace), nil
	case '}':
		return build(RBrace), nil
	case '[':
		return build(LBracket), nil
	case ']':
		return build(RBracket), nil
	case ',':
		return build(Comma), nil
	case '.':
		return build(Dot), nil
	case ';':
		return build(Semicolon), nil
	case ':':
		return build(Colon), nil

	default:
		return Token{}, l.unexpected(startLine, startColumn, l.source[start])
	}
}

// unexpected builds an error for a character that cannot begin a token.
func (l *lexer) unexpected(line, column int, character rune) error {
	return &Error{
		Line:    line,
		Column:  column,
		Message: fmt.Sprintf("unexpected character %q", character),
	}
}

// atEnd reports whether the scanner has consumed all input.
func (l *lexer) atEnd() bool {
	return l.pos >= len(l.source)
}

// peek returns the current rune without consuming it, or eof at the end.
func (l *lexer) peek() rune {
	if l.atEnd() {
		return eof
	}
	return l.source[l.pos]
}

// peekAt returns the rune offset positions ahead of the current one without
// consuming anything, or eof if that position is past the end.
func (l *lexer) peekAt(offset int) rune {
	if l.pos+offset >= len(l.source) || l.pos+offset < 0 {
		return eof
	}
	return l.source[l.pos+offset]
}

// advance consumes and returns the current rune, updating line and column.
func (l *lexer) advance() rune {
	character := l.source[l.pos]
	l.pos++
	if character == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}
	return character
}

// isIdentifierStart reports whether the rune may begin an identifier.
func isIdentifierStart(character rune) bool {
	return character == '_' || (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
}

// isIdentifierPart reports whether the rune may appear after the first
// character of an identifier.
func isIdentifierPart(character rune) bool {
	return isIdentifierStart(character) || isDigit(character)
}

// isDigit reports whether the rune is a decimal digit.
func isDigit(character rune) bool {
	return character >= '0' && character <= '9'
}
