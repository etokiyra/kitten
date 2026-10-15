package parser

// This file defines the Kitten abstract syntax tree (AST).
//
// The AST is a faithful, position-annotated record of the syntax accepted by
// the parser. It deliberately contains no semantic information: it does not
// resolve names, assign types, or decide whether a construct is meaningful.
// Those decisions belong to the future checker.
//
// Every node exposes Pos, which reports the one-based line and column where the
// node begins. Positions are copied from the lexer tokens, so diagnostics built
// from the AST point back into the original source.

// Position is a one-based source location.
type Position struct {
	Line   int
	Column int
}

// Node is implemented by every AST node.
type Node interface {
	// Pos reports the position where the node begins.
	Pos() Position
}

// Decl is a top-level declaration (a function or a struct).
type Decl interface {
	Node
	declNode()
}

// Stmt is a statement.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is an expression.
type Expr interface {
	Node
	exprNode()
}

// Visibility is the optional access modifier written before a top-level
// declaration.
//
// Whether a modifier is required, what it defaults to, and its exact effect are
// deferred to the checker (SPEC.md §16). The parser only records what was
// written.
type Visibility int

const (
	// VisibilityDefault means no modifier was written.
	VisibilityDefault Visibility = iota

	// VisibilityPublic means the declaration was prefixed with "public".
	VisibilityPublic

	// VisibilityPrivate means the declaration was prefixed with "private".
	VisibilityPrivate
)

// String returns the source spelling of the modifier, or "default".
func (v Visibility) String() string {
	switch v {
	case VisibilityPublic:
		return "public"
	case VisibilityPrivate:
		return "private"
	default:
		return "default"
	}
}

// Program is a parsed source file: a sequence of top-level declarations.
//
// An empty source file parses to a Program with no declarations.
type Program struct {
	Declarations []Decl
}

// TypeRef names a type.
//
// A named type has Name set and Element nil. An array type has Element set to
// its element type and Name empty, so `int[]` is an array whose element is
// `int`, and `int[][]` nests. The parser does not verify that a named type
// exists.
type TypeRef struct {
	At      Position
	Name    string
	Element *TypeRef
}

// Pos reports where the type name begins.
func (t *TypeRef) Pos() Position { return t.At }

// String renders the type as written: a name, or an element type followed by
// "[]".
func (t *TypeRef) String() string {
	if t == nil {
		return "<invalid>"
	}
	if t.Element != nil {
		return t.Element.String() + "[]"
	}
	return t.Name
}

// FunctionDecl is a top-level function declaration:
//
//	fn name(parameters) -> ReturnType { body }
type FunctionDecl struct {
	At         Position
	Visibility Visibility
	Name       string
	Parameters []*Parameter
	// ReturnType is nil when no "-> type" annotation was written.
	ReturnType *TypeRef
	Body       *BlockStmt
}

// Parameter is a single function parameter, written "type name".
type Parameter struct {
	At   Position
	Name string
	Type *TypeRef
}

// Pos reports where the parameter begins (its type).
func (p *Parameter) Pos() Position { return p.At }

// StructDecl is a top-level struct declaration:
//
//	struct Name { fields }
type StructDecl struct {
	At         Position
	Visibility Visibility
	Name       string
	Fields     []*Field
}

// Field is a single struct field, written "type name;".
type Field struct {
	At   Position
	Name string
	Type *TypeRef
}

// Pos reports where the field begins (its type).
func (f *Field) Pos() Position { return f.At }

// Pos reports where the function declaration begins, including any visibility
// modifier.
func (d *FunctionDecl) Pos() Position { return d.At }

// Pos reports where the struct declaration begins, including any visibility
// modifier.
func (d *StructDecl) Pos() Position { return d.At }

func (*FunctionDecl) declNode() {}
func (*StructDecl) declNode()   {}

// BlockStmt is a brace-delimited sequence of statements. It is used for
// function bodies and for the bodies of control-flow statements.
type BlockStmt struct {
	At         Position
	Statements []Stmt
}

// Pos reports the position of the opening brace.
func (s *BlockStmt) Pos() Position { return s.At }

// VarDeclStmt is a local variable declaration, written "type name;" or
// "type name = expression;". The initializer may be nil.
type VarDeclStmt struct {
	At          Position
	Name        string
	Type        *TypeRef
	Initializer Expr
}

// Pos reports where the declared type begins.
func (s *VarDeclStmt) Pos() Position { return s.At }

// ExprStmt is an expression used as a statement, such as a call or an
// assignment, terminated by a semicolon.
type ExprStmt struct {
	At         Position
	Expression Expr
}

// Pos reports where the expression begins.
func (s *ExprStmt) Pos() Position { return s.At }

// IfStmt is an if/else-if/else statement.
//
// Else is nil when there is no else clause, a *BlockStmt for "else { ... }",
// or an *IfStmt for "else if (...) { ... }".
type IfStmt struct {
	At        Position
	Condition Expr
	Then      *BlockStmt
	Else      Stmt
}

// Pos reports the position of the "if" keyword.
func (s *IfStmt) Pos() Position { return s.At }

// WhileStmt is a while loop.
type WhileStmt struct {
	At        Position
	Condition Expr
	Body      *BlockStmt
}

// Pos reports the position of the "while" keyword.
func (s *WhileStmt) Pos() Position { return s.At }

// ForStmt is a C-style for loop: for (Init; Condition; Increment) { Body }.
//
// Init is nil when the initializer is omitted, a *VarDeclStmt when it declares
// a variable, or an *ExprStmt otherwise. Condition and Increment are nil when
// omitted.
type ForStmt struct {
	At        Position
	Init      Stmt
	Condition Expr
	Increment Expr
	Body      *BlockStmt
}

// Pos reports the position of the "for" keyword.
func (s *ForStmt) Pos() Position { return s.At }

// ReturnStmt is a return statement. Value is nil for a bare "return;".
//
// Whether a bare "return;" is allowed, and how a returned value relates to the
// declared return type, are deferred to the checker.
type ReturnStmt struct {
	At    Position
	Value Expr
}

// Pos reports the position of the "return" keyword.
func (s *ReturnStmt) Pos() Position { return s.At }

func (*BlockStmt) stmtNode()   {}
func (*VarDeclStmt) stmtNode() {}
func (*ExprStmt) stmtNode()    {}
func (*IfStmt) stmtNode()      {}
func (*WhileStmt) stmtNode()   {}
func (*ForStmt) stmtNode()     {}
func (*ReturnStmt) stmtNode()  {}

// Identifier is a reference to a name. Whether the name is defined, and what it
// refers to, are deferred to the checker.
type Identifier struct {
	At   Position
	Name string
}

// Pos reports where the identifier begins.
func (e *Identifier) Pos() Position { return e.At }

// IntLiteral is a decimal integer literal. Text preserves the original source
// spelling; parsing it into a numeric value is deferred.
type IntLiteral struct {
	At   Position
	Text string
}

// Pos reports where the literal begins.
func (e *IntLiteral) Pos() Position { return e.At }

// FloatLiteral is a decimal floating-point literal. Text preserves the original
// source spelling; parsing it into a numeric value is deferred.
type FloatLiteral struct {
	At   Position
	Text string
}

// Pos reports where the literal begins.
func (e *FloatLiteral) Pos() Position { return e.At }

// StringLiteral is a double-quoted string literal. Text preserves the original
// source spelling, including the quotation marks and escape sequences; decoding
// the escapes is deferred.
type StringLiteral struct {
	At   Position
	Text string
}

// Pos reports where the literal begins.
func (e *StringLiteral) Pos() Position { return e.At }

// BoolLiteral is the literal true or false.
type BoolLiteral struct {
	At    Position
	Value bool
}

// Pos reports where the literal begins.
func (e *BoolLiteral) Pos() Position { return e.At }

// UnaryExpr is a prefix or postfix unary operation.
//
// Operator is one of "-", "!", "++", or "--". Postfix is false for a prefix
// operator and true for a postfix one; only "++" and "--" can be postfix.
// OperatorAt reports the position of the operator. What these operators mean,
// and whether an operand is a valid target, are deferred to the checker.
type UnaryExpr struct {
	OperatorAt Position
	Operator   string
	Operand    Expr
	Postfix    bool
}

// Pos reports where the unary expression begins: the operator for a prefix
// operation, or the operand for a postfix one.
func (e *UnaryExpr) Pos() Position {
	if e.Postfix {
		return e.Operand.Pos()
	}
	return e.OperatorAt
}

// BinaryExpr is a binary operation such as "+", "==", or "&&". OperatorAt
// reports the position of the operator.
type BinaryExpr struct {
	OperatorAt Position
	Operator   string
	Left       Expr
	Right      Expr
}

// Pos reports where the left operand begins.
func (e *BinaryExpr) Pos() Position { return e.Left.Pos() }

// AssignExpr is an assignment: "=", "+=", or "-=". OperatorAt reports the
// position of the operator.
//
// The parser does not verify that Target is assignable; that is a semantic
// check deferred to the checker.
type AssignExpr struct {
	OperatorAt Position
	Operator   string
	Target     Expr
	Value      Expr
}

// Pos reports where the assignment target begins.
func (e *AssignExpr) Pos() Position { return e.Target.Pos() }

// CallExpr is a function call. Callee is the called expression (usually an
// Identifier or a MemberExpr) and OpenParen reports the position of "(". Whether
// the callee names a function, and whether the arguments match, are deferred to
// the checker.
type CallExpr struct {
	OpenParen Position
	Callee    Expr
	Arguments []Expr
}

// Pos reports where the callee begins.
func (e *CallExpr) Pos() Position { return e.Callee.Pos() }

// MemberExpr is member access, written "receiver.member". Dot reports the
// position of the dot.
type MemberExpr struct {
	Dot      Position
	Receiver Expr
	Member   string
}

// Pos reports where the receiver begins.
func (e *MemberExpr) Pos() Position { return e.Receiver.Pos() }

// StructLiteral is a struct literal expression, written
// "TypeName { field = value, ... }". OpenBrace reports the position of the
// opening brace.
//
// The parser does not verify that TypeName names a struct, that the fields
// exist, or that every field is initialized; those are deferred to the checker.
type StructLiteral struct {
	OpenBrace Position
	Type      *TypeRef
	Fields    []*FieldInit
}

// Pos reports where the type name begins.
func (e *StructLiteral) Pos() Position { return e.Type.Pos() }

// FieldInit is one "name = value" entry inside a struct literal.
type FieldInit struct {
	At    Position
	Name  string
	Value Expr
}

// Pos reports where the field name begins.
func (f *FieldInit) Pos() Position { return f.At }

// NewArrayExpr creates an array, written "new Element[Length]".
//
// Element is the element type and Length is an int expression giving the number
// of elements. The resulting array has length Length and every element is
// zero-initialized. At reports the position of the "new" keyword.
type NewArrayExpr struct {
	At      Position
	Element *TypeRef
	Length  Expr
}

// Pos reports where the "new" keyword begins.
func (e *NewArrayExpr) Pos() Position { return e.At }

// IndexExpr reads or writes one element of an array, written "Target[Index]".
//
// At reports the position of the "[" and Pos reports where the target begins,
// so the node is positioned like other postfix expressions.
type IndexExpr struct {
	At     Position
	Target Expr
	Index  Expr
}

// Pos reports where the indexed target begins.
func (e *IndexExpr) Pos() Position { return e.Target.Pos() }

func (*Identifier) exprNode()    {}
func (*IntLiteral) exprNode()    {}
func (*FloatLiteral) exprNode()  {}
func (*StringLiteral) exprNode() {}
func (*BoolLiteral) exprNode()   {}
func (*UnaryExpr) exprNode()     {}
func (*BinaryExpr) exprNode()    {}
func (*AssignExpr) exprNode()    {}
func (*CallExpr) exprNode()      {}
func (*MemberExpr) exprNode()    {}
func (*StructLiteral) exprNode() {}
func (*NewArrayExpr) exprNode()  {}
func (*IndexExpr) exprNode()     {}
