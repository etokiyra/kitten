package parser

import (
	"fmt"

	"kitten/lexer"
)

// Error describes a syntax error and the position at which it was detected.
type Error struct {
	// Line is the one-based line of the offending token.
	Line int

	// Column is the one-based column of the offending token.
	Column int

	// Message describes what went wrong.
	Message string
}

// Error implements the error interface, formatting the position as line:column.
func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message)
}

// Parse builds a syntax tree from a token stream produced by the lexer.
//
// The stream is expected to end with an EOF token, which lexer.Lex always
// produces. Parse is a fail-fast parser: it reports the first syntax error it
// encounters and returns a nil Program. It never panics, including on
// malformed, truncated, or empty token streams.
func Parse(tokens []lexer.Token) (*Program, error) {
	p := &parser{tokens: tokens}
	p.eof = eofToken(tokens)
	return p.parseProgram()
}

// ParseSource lexes source and parses the resulting tokens.
//
// It is a convenience for callers that hold raw source, such as the eventual
// command-line driver and the tests. It performs exactly one lexing pass.
func ParseSource(source string) (*Program, error) {
	tokens, err := lexer.Lex(source)
	if err != nil {
		return nil, err
	}
	return Parse(tokens)
}

// eofToken returns the EOF token to use when the parser reads past the end of
// the supplied stream. It preserves the position of a trailing EOF token when
// one is present, and otherwise synthesizes one at the end of the stream so
// that the parser can report accurate positions for unexpected end of input.
func eofToken(tokens []lexer.Token) lexer.Token {
	if len(tokens) == 0 {
		return lexer.Token{Kind: lexer.EOF, Line: 1, Column: 1}
	}
	last := tokens[len(tokens)-1]
	if last.Kind == lexer.EOF {
		return last
	}
	return lexer.Token{Kind: lexer.EOF, Line: last.Line, Column: last.Column}
}

// parser holds the token stream and the current read position.
type parser struct {
	tokens []lexer.Token
	pos    int
	eof    lexer.Token
}

// peek returns the current token without consuming it. Reading past the end
// yields the EOF token, so callers never index out of range.
func (p *parser) peek() lexer.Token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return p.eof
}

// peekAt returns the token offset positions ahead of the current one without
// consuming anything, or the EOF token when that position is past the end.
func (p *parser) peekAt(offset int) lexer.Token {
	index := p.pos + offset
	if index >= 0 && index < len(p.tokens) {
		return p.tokens[index]
	}
	return p.eof
}

// advance consumes and returns the current token. At end of input it returns
// the EOF token without moving past it.
func (p *parser) advance() lexer.Token {
	token := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return token
}

// expect consumes the next token if it has the given kind. Otherwise it
// returns a syntax error describing what was expected. "want" is the human
// spelling of the expected token, such as "')'", and "context" explains where,
// such as "after the parameter list".
func (p *parser) expect(kind lexer.Kind, want, context string) (lexer.Token, error) {
	token := p.peek()
	if token.Kind != kind {
		return lexer.Token{}, p.errorAt(token, "expected %s %s, found %s", want, context, describe(token))
	}
	return p.advance(), nil
}

// errorAt builds a syntax error positioned at the given token.
func (p *parser) errorAt(token lexer.Token, format string, args ...any) error {
	return &Error{
		Line:    token.Line,
		Column:  token.Column,
		Message: fmt.Sprintf(format, args...),
	}
}

// describe renders a token for use in an error message.
func describe(token lexer.Token) string {
	if token.Kind == lexer.EOF {
		return "end of input"
	}
	return fmt.Sprintf("%s %q", token.Kind, token.Text)
}

// positionOf converts a token's location into an AST Position.
func positionOf(token lexer.Token) Position {
	return Position{Line: token.Line, Column: token.Column}
}

// parseProgram parses top-level declarations until end of input.
func (p *parser) parseProgram() (*Program, error) {
	program := &Program{}
	for p.peek().Kind != lexer.EOF {
		declaration, err := p.parseDeclaration()
		if err != nil {
			return nil, err
		}
		program.Declarations = append(program.Declarations, declaration)
	}
	return program, nil
}

// parseDeclaration parses a top-level function or struct declaration, preceded
// by an optional visibility modifier.
func (p *parser) parseDeclaration() (Decl, error) {
	start := p.peek()

	visibility := VisibilityDefault
	switch start.Kind {
	case lexer.Public:
		visibility = VisibilityPublic
		p.advance()
	case lexer.Private:
		visibility = VisibilityPrivate
		p.advance()
	}

	token := p.peek()
	switch token.Kind {
	case lexer.Fn:
		return p.parseFunction(start, visibility)
	case lexer.Struct:
		return p.parseStruct(start, visibility)
	default:
		return nil, p.errorAt(token, "expected a declaration ('fn' or 'struct'), found %s", describe(token))
	}
}

// parseFunction parses a function declaration. "start" is the first token of
// the declaration, used as the node position.
func (p *parser) parseFunction(start lexer.Token, visibility Visibility) (Decl, error) {
	p.advance() // 'fn'

	name, err := p.expect(lexer.Ident, "a function name", "after 'fn'")
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(lexer.LParen, "'('", "after the function name"); err != nil {
		return nil, err
	}
	parameters, err := p.parseParameters()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RParen, "')'", "after the parameter list"); err != nil {
		return nil, err
	}

	declaration := &FunctionDecl{
		At:         positionOf(start),
		Visibility: visibility,
		Name:       name.Text,
		Parameters: parameters,
	}

	if p.peek().Kind == lexer.Arrow {
		p.advance()
		returnType, err := p.parseType()
		if err != nil {
			return nil, err
		}
		declaration.ReturnType = returnType
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	declaration.Body = body

	return declaration, nil
}

// parseParameters parses a comma-separated parameter list up to, but not
// including, the closing parenthesis.
func (p *parser) parseParameters() ([]*Parameter, error) {
	var parameters []*Parameter

	if p.peek().Kind == lexer.RParen {
		return parameters, nil
	}

	for {
		parameter, err := p.parseParameter()
		if err != nil {
			return nil, err
		}
		parameters = append(parameters, parameter)

		if p.peek().Kind != lexer.Comma {
			break
		}
		p.advance() // ','

		if p.peek().Kind == lexer.RParen {
			return nil, p.errorAt(p.peek(), "trailing comma is not allowed in a parameter list")
		}
	}

	return parameters, nil
}

// parseParameter parses a single "type name" parameter.
func (p *parser) parseParameter() (*Parameter, error) {
	typeName, err := p.parseType()
	if err != nil {
		return nil, err
	}

	name, err := p.expect(lexer.Ident, "a parameter name", "after the parameter type")
	if err != nil {
		return nil, err
	}

	return &Parameter{At: typeName.At, Name: name.Text, Type: typeName}, nil
}

// parseStruct parses a struct declaration. "start" is the first token of the
// declaration, used as the node position.
func (p *parser) parseStruct(start lexer.Token, visibility Visibility) (Decl, error) {
	p.advance() // 'struct'

	name, err := p.expect(lexer.Ident, "a type name", "after 'struct'")
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(lexer.LBrace, "'{'", "to start the struct body"); err != nil {
		return nil, err
	}

	declaration := &StructDecl{
		At:         positionOf(start),
		Visibility: visibility,
		Name:       name.Text,
	}

	for {
		token := p.peek()
		if token.Kind == lexer.RBrace {
			p.advance()
			return declaration, nil
		}
		if token.Kind == lexer.EOF {
			return nil, p.errorAt(token, "expected '}' to close the struct body, found end of input")
		}

		field, err := p.parseField()
		if err != nil {
			return nil, err
		}
		declaration.Fields = append(declaration.Fields, field)
	}
}

// parseField parses a single "type name;" struct field. Struct fields do not
// take an initializer in this revision (SPEC.md §11).
func (p *parser) parseField() (*Field, error) {
	typeName, err := p.parseType()
	if err != nil {
		return nil, err
	}

	name, err := p.expect(lexer.Ident, "a field name", "after the field type")
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(lexer.Semicolon, "';'", "after the struct field"); err != nil {
		return nil, err
	}

	return &Field{At: typeName.At, Name: name.Text, Type: typeName}, nil
}

// parseType parses a type: a built-in type keyword or a user-defined identifier
// such as a struct name, followed by any number of "[]" array suffixes.
func (p *parser) parseType() (*TypeRef, error) {
	base, err := p.parseBaseType()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == lexer.LBracket && p.peekAt(1).Kind == lexer.RBracket {
		p.advance() // '['
		p.advance() // ']'
		base = &TypeRef{At: base.At, Element: base}
	}
	return base, nil
}

// parseBaseType parses a single type name without array suffixes.
func (p *parser) parseBaseType() (*TypeRef, error) {
	token := p.peek()
	switch token.Kind {
	case lexer.TypeInt, lexer.TypeFloat, lexer.TypeBool, lexer.TypeString, lexer.TypeVoid, lexer.Ident:
		p.advance()
		return &TypeRef{At: positionOf(token), Name: token.Text}, nil
	default:
		return nil, p.errorAt(token, "expected a type, found %s", describe(token))
	}
}

// parseBlock parses a brace-delimited block of statements.
func (p *parser) parseBlock() (*BlockStmt, error) {
	open, err := p.expect(lexer.LBrace, "'{'", "to start a block")
	if err != nil {
		return nil, err
	}

	block := &BlockStmt{At: positionOf(open)}
	for {
		token := p.peek()
		switch token.Kind {
		case lexer.RBrace:
			p.advance()
			return block, nil
		case lexer.EOF:
			return nil, p.errorAt(token, "expected '}' to close the block, found end of input")
		}

		statement, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		block.Statements = append(block.Statements, statement)
	}
}

// parseStatement parses a single statement.
func (p *parser) parseStatement() (Stmt, error) {
	switch p.peek().Kind {
	case lexer.LBrace:
		return p.parseBlock()
	case lexer.If:
		return p.parseIf()
	case lexer.While:
		return p.parseWhile()
	case lexer.For:
		return p.parseFor()
	case lexer.Return:
		return p.parseReturn()
	}

	if p.startsVariableDeclaration() {
		return p.parseVarDeclStatement()
	}
	return p.parseExprStatement()
}

// startsVariableDeclaration reports whether the upcoming tokens begin a local
// variable declaration.
//
// A declaration begins with a built-in type keyword, or with an identifier that
// names a type, optionally followed by "[]" array suffixes, and then the
// declared name. This distinguishes "Rectangle room = ...;" and
// "Rectangle[] rooms = ...;" from expression statements such as "room = ...;"
// or "rooms[0] = ...;".
func (p *parser) startsVariableDeclaration() bool {
	if isTypeKeyword(p.peek().Kind) {
		return true
	}
	if p.peek().Kind != lexer.Ident {
		return false
	}

	offset := 1
	for p.peekAt(offset).Kind == lexer.LBracket && p.peekAt(offset+1).Kind == lexer.RBracket {
		offset += 2
	}
	return p.peekAt(offset).Kind == lexer.Ident
}

// isTypeKeyword reports whether a token kind is a built-in type name.
func isTypeKeyword(kind lexer.Kind) bool {
	switch kind {
	case lexer.TypeInt, lexer.TypeFloat, lexer.TypeBool, lexer.TypeString, lexer.TypeVoid:
		return true
	default:
		return false
	}
}

// parseVarDeclStatement parses a local variable declaration and its trailing
// semicolon.
func (p *parser) parseVarDeclStatement() (Stmt, error) {
	declaration, err := p.parseVarDecl()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.Semicolon, "';'", "after the variable declaration"); err != nil {
		return nil, err
	}
	return declaration, nil
}

// parseVarDecl parses a local variable declaration without its trailing
// semicolon, so that it can be reused inside a for-loop header.
//
// The initializer is optional: both "int x;" and "int x = 1;" parse. Whether an
// initializer is mandatory is deferred to the checker (SPEC.md §10).
func (p *parser) parseVarDecl() (*VarDeclStmt, error) {
	typeName, err := p.parseType()
	if err != nil {
		return nil, err
	}

	name, err := p.expect(lexer.Ident, "an identifier", "for the variable name")
	if err != nil {
		return nil, err
	}

	declaration := &VarDeclStmt{At: typeName.At, Name: name.Text, Type: typeName}

	if p.peek().Kind == lexer.Assign {
		p.advance()
		initializer, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		declaration.Initializer = initializer
	}

	return declaration, nil
}

// parseExprStatement parses an expression followed by a semicolon.
func (p *parser) parseExprStatement() (Stmt, error) {
	expression, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.Semicolon, "';'", "after the expression"); err != nil {
		return nil, err
	}
	return &ExprStmt{At: expression.Pos(), Expression: expression}, nil
}

// parseIf parses an if statement, including any "else if" or "else" clause.
func (p *parser) parseIf() (Stmt, error) {
	keyword := p.advance() // 'if'

	if _, err := p.expect(lexer.LParen, "'('", "after 'if'"); err != nil {
		return nil, err
	}
	condition, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RParen, "')'", "after the if condition"); err != nil {
		return nil, err
	}

	thenBranch, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	statement := &IfStmt{At: positionOf(keyword), Condition: condition, Then: thenBranch}

	if p.peek().Kind == lexer.Else {
		p.advance()
		if p.peek().Kind == lexer.If {
			elseIf, err := p.parseIf()
			if err != nil {
				return nil, err
			}
			statement.Else = elseIf
		} else {
			elseBranch, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			statement.Else = elseBranch
		}
	}

	return statement, nil
}

// parseWhile parses a while loop.
func (p *parser) parseWhile() (Stmt, error) {
	keyword := p.advance() // 'while'

	if _, err := p.expect(lexer.LParen, "'('", "after 'while'"); err != nil {
		return nil, err
	}
	condition, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RParen, "')'", "after the while condition"); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &WhileStmt{At: positionOf(keyword), Condition: condition, Body: body}, nil
}

// parseFor parses a C-style for loop:
//
//	for (Init; Condition; Increment) { Body }
//
// Each of the three header clauses may be empty.
func (p *parser) parseFor() (Stmt, error) {
	keyword := p.advance() // 'for'

	if _, err := p.expect(lexer.LParen, "'('", "after 'for'"); err != nil {
		return nil, err
	}

	statement := &ForStmt{At: positionOf(keyword)}

	if p.peek().Kind != lexer.Semicolon {
		initializer, err := p.parseForInit()
		if err != nil {
			return nil, err
		}
		statement.Init = initializer
	}
	if _, err := p.expect(lexer.Semicolon, "';'", "after the for-loop initializer"); err != nil {
		return nil, err
	}

	if p.peek().Kind != lexer.Semicolon {
		condition, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		statement.Condition = condition
	}
	if _, err := p.expect(lexer.Semicolon, "';'", "after the for-loop condition"); err != nil {
		return nil, err
	}

	if p.peek().Kind != lexer.RParen {
		increment, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		statement.Increment = increment
	}
	if _, err := p.expect(lexer.RParen, "')'", "after the for-loop header"); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	statement.Body = body

	return statement, nil
}

// parseForInit parses the initializer clause of a for loop, which may be a
// variable declaration or an expression.
func (p *parser) parseForInit() (Stmt, error) {
	if p.startsVariableDeclaration() {
		return p.parseVarDecl()
	}

	expression, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	return &ExprStmt{At: expression.Pos(), Expression: expression}, nil
}

// parseReturn parses a return statement, with or without a value.
func (p *parser) parseReturn() (Stmt, error) {
	keyword := p.advance() // 'return'

	statement := &ReturnStmt{At: positionOf(keyword)}
	if p.peek().Kind != lexer.Semicolon {
		value, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		statement.Value = value
	}

	if _, err := p.expect(lexer.Semicolon, "';'", "after the return statement"); err != nil {
		return nil, err
	}
	return statement, nil
}

// Expression precedence levels, from lowest to highest binding. Assignment is
// the lowest and right-associative; the remaining binary operators are
// left-associative. These levels are recorded in SPEC.md §7.
const (
	precedenceAssignment = iota + 1
	precedenceLogicalOr
	precedenceLogicalAnd
	precedenceEquality
	precedenceRelational
	precedenceAdditive
	precedenceMultiplicative
)

// binaryPrecedence returns the precedence of a binary or assignment operator.
// The second result is false when the token is not such an operator.
func binaryPrecedence(kind lexer.Kind) (int, bool) {
	switch kind {
	case lexer.Assign, lexer.PlusAssign, lexer.MinusAssign:
		return precedenceAssignment, true
	case lexer.Or:
		return precedenceLogicalOr, true
	case lexer.And:
		return precedenceLogicalAnd, true
	case lexer.Eq, lexer.Neq:
		return precedenceEquality, true
	case lexer.Lt, lexer.Lte, lexer.Gt, lexer.Gte:
		return precedenceRelational, true
	case lexer.Plus, lexer.Minus:
		return precedenceAdditive, true
	case lexer.Star, lexer.Slash, lexer.Percent:
		return precedenceMultiplicative, true
	default:
		return 0, false
	}
}

// isRightAssociative reports whether an operator associates to the right. Only
// the assignment operators do.
func isRightAssociative(kind lexer.Kind) bool {
	switch kind {
	case lexer.Assign, lexer.PlusAssign, lexer.MinusAssign:
		return true
	default:
		return false
	}
}

// parseExpression parses a full expression, including assignment.
func (p *parser) parseExpression() (Expr, error) {
	return p.parseBinary(precedenceAssignment)
}

// parseBinary implements precedence climbing for the binary and assignment
// operators. It parses operators whose precedence is at least minPrecedence.
func (p *parser) parseBinary(minPrecedence int) (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		operator := p.peek()
		precedence, ok := binaryPrecedence(operator.Kind)
		if !ok || precedence < minPrecedence {
			return left, nil
		}
		p.advance()

		nextPrecedence := precedence + 1
		if isRightAssociative(operator.Kind) {
			nextPrecedence = precedence
		}
		right, err := p.parseBinary(nextPrecedence)
		if err != nil {
			return nil, err
		}

		switch operator.Kind {
		case lexer.Assign, lexer.PlusAssign, lexer.MinusAssign:
			left = &AssignExpr{
				OperatorAt: positionOf(operator),
				Operator:   operator.Text,
				Target:     left,
				Value:      right,
			}
		default:
			left = &BinaryExpr{
				OperatorAt: positionOf(operator),
				Operator:   operator.Text,
				Left:       left,
				Right:      right,
			}
		}
	}
}

// parseUnary parses a prefix unary expression ("-", "!", "++", "--") or, when
// none applies, a postfix expression.
func (p *parser) parseUnary() (Expr, error) {
	token := p.peek()
	switch token.Kind {
	case lexer.Minus, lexer.Not, lexer.Inc, lexer.Dec:
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{
			OperatorAt: positionOf(token),
			Operator:   token.Text,
			Operand:    operand,
			Postfix:    false,
		}, nil
	default:
		return p.parsePostfix()
	}
}

// parsePostfix parses a primary expression followed by any number of postfix
// operations: calls, member access, and postfix "++"/"--".
func (p *parser) parsePostfix() (Expr, error) {
	expression, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for {
		switch p.peek().Kind {
		case lexer.LParen:
			open := p.advance()
			arguments, err := p.parseArguments()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.RParen, "')'", "after the argument list"); err != nil {
				return nil, err
			}
			expression = &CallExpr{OpenParen: positionOf(open), Callee: expression, Arguments: arguments}

		case lexer.Dot:
			dot := p.advance()
			member, err := p.expect(lexer.Ident, "a member name", "after '.'")
			if err != nil {
				return nil, err
			}
			expression = &MemberExpr{Dot: positionOf(dot), Receiver: expression, Member: member.Text}

		case lexer.LBracket:
			bracket := p.advance()
			index, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.RBracket, "']'", "after the index expression"); err != nil {
				return nil, err
			}
			expression = &IndexExpr{At: positionOf(bracket), Target: expression, Index: index}

		case lexer.Inc, lexer.Dec:
			operator := p.advance()
			expression = &UnaryExpr{
				OperatorAt: positionOf(operator),
				Operator:   operator.Text,
				Operand:    expression,
				Postfix:    true,
			}

		default:
			return expression, nil
		}
	}
}

// parseArguments parses a comma-separated argument list up to, but not
// including, the closing parenthesis. The opening parenthesis has already been
// consumed.
func (p *parser) parseArguments() ([]Expr, error) {
	var arguments []Expr

	if p.peek().Kind == lexer.RParen {
		return arguments, nil
	}

	for {
		argument, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, argument)

		if p.peek().Kind != lexer.Comma {
			break
		}
		p.advance() // ','

		if p.peek().Kind == lexer.RParen {
			return nil, p.errorAt(p.peek(), "trailing comma is not allowed in an argument list")
		}
	}

	return arguments, nil
}

// parsePrimary parses a literal, identifier, struct literal, array creation, or
// parenthesized expression.
func (p *parser) parsePrimary() (Expr, error) {
	token := p.peek()

	switch token.Kind {
	case lexer.Int:
		p.advance()
		return &IntLiteral{At: positionOf(token), Text: token.Text}, nil

	case lexer.Float:
		p.advance()
		return &FloatLiteral{At: positionOf(token), Text: token.Text}, nil

	case lexer.String:
		p.advance()
		return &StringLiteral{At: positionOf(token), Text: token.Text}, nil

	case lexer.True:
		p.advance()
		return &BoolLiteral{At: positionOf(token), Value: true}, nil

	case lexer.False:
		p.advance()
		return &BoolLiteral{At: positionOf(token), Value: false}, nil

	case lexer.New:
		return p.parseNewArray()

	case lexer.Ident:
		if p.peekAt(1).Kind == lexer.LBrace {
			return p.parseStructLiteral()
		}
		p.advance()
		return &Identifier{At: positionOf(token), Name: token.Text}, nil

	case lexer.LParen:
		p.advance()
		inner, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RParen, "')'", "after the parenthesized expression"); err != nil {
			return nil, err
		}
		return inner, nil

	default:
		return nil, p.errorAt(token, "expected an expression, found %s", describe(token))
	}
}

// parseNewArray parses array creation, "new ElementType[Length]".
//
// The element type may itself be an array type written with "[]", so
// "new int[][3]" creates an array of three int[] elements. Only the outermost
// bracket pair carries a length.
func (p *parser) parseNewArray() (Expr, error) {
	keyword := p.advance() // 'new'

	element, err := p.parseType()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(lexer.LBracket, "'['", "after the array element type"); err != nil {
		return nil, err
	}
	length, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RBracket, "']'", "after the array length"); err != nil {
		return nil, err
	}

	return &NewArrayExpr{At: positionOf(keyword), Element: element, Length: length}, nil
}

// parseStructLiteral parses a struct literal expression, "Name { field = value,
// ... }". The type name is the current identifier token.
func (p *parser) parseStructLiteral() (Expr, error) {
	name := p.advance() // type name

	open, err := p.expect(lexer.LBrace, "'{'", "to start the struct literal")
	if err != nil {
		return nil, err
	}

	literal := &StructLiteral{
		OpenBrace: positionOf(open),
		Type:      &TypeRef{At: positionOf(name), Name: name.Text},
	}

	if p.peek().Kind == lexer.RBrace {
		p.advance()
		return literal, nil
	}

	for {
		field, err := p.parseFieldInit()
		if err != nil {
			return nil, err
		}
		literal.Fields = append(literal.Fields, field)

		if p.peek().Kind != lexer.Comma {
			break
		}
		p.advance() // ','

		if p.peek().Kind == lexer.RBrace {
			return nil, p.errorAt(p.peek(), "trailing comma is not allowed in a struct literal")
		}
	}

	if _, err := p.expect(lexer.RBrace, "'}'", "to close the struct literal"); err != nil {
		return nil, err
	}
	return literal, nil
}

// parseFieldInit parses one "name = value" entry in a struct literal.
func (p *parser) parseFieldInit() (*FieldInit, error) {
	name, err := p.expect(lexer.Ident, "a field name", "in the struct literal")
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(lexer.Assign, "'='", "after the field name"); err != nil {
		return nil, err
	}

	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	return &FieldInit{At: positionOf(name), Name: name.Text, Value: value}, nil
}
