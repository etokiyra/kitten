package checker

import (
	"fmt"

	"kitten/lexer"
	"kitten/parser"
)

// readFileResultPrelude is the fixed source of the implicit prelude. It is
// parsed by the ordinary lexer and parser, so it uses the same union and field
// registration as any user source.
const readFileResultPrelude = `public union ReadFileResult {
    Ok { string text; }
    Failed { string message; }
}
`

// preludeModulePath identifies the synthetic module. A user source file cannot
// normally have this path; the only consequence is that reserved-name checks
// skip the prelude itself.
const preludeModulePath = "<prelude>"

// parseReadFileResultPrelude parses the fixed prelude into a module with ID 0.
// The caller assigns the final ID when appending it.
func parseReadFileResultPrelude() (*parser.Module, error) {
	tokens, err := lexer.Lex(readFileResultPrelude)
	if err != nil {
		return nil, fmt.Errorf("parsing the ReadFileResult prelude: %w", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		return nil, fmt.Errorf("parsing the ReadFileResult prelude: %w", err)
	}
	return &parser.Module{Path: preludeModulePath, Program: program}, nil
}

// usesTryReadFile reports whether any module contains a syntactic call whose
// callee is the identifier "try_read_file". It is deliberately syntactic: a
// user function with that name may cause the prelude to be injected unused, but
// the helper is non-static and no semantic rule depends on the false positive.
func usesTryReadFile(modules []*parser.Module) bool {
	for _, module := range modules {
		if module == nil || module.Program == nil {
			continue
		}
		for _, declaration := range module.Program.Declarations {
			if declarationUsesTryReadFile(declaration) {
				return true
			}
		}
	}
	return false
}

// declarationUsesTryReadFile scans a top-level declaration. Only a function body
// can contain an expression.
func declarationUsesTryReadFile(declaration parser.Decl) bool {
	function, ok := declaration.(*parser.FunctionDecl)
	if !ok || function == nil {
		return false
	}
	return statementUsesTryReadFile(function.Body)
}

// statementUsesTryReadFile scans one statement and its children.
func statementUsesTryReadFile(statement parser.Stmt) bool {
	switch s := statement.(type) {
	case *parser.BlockStmt:
		if s == nil {
			return false
		}
		for _, inner := range s.Statements {
			if statementUsesTryReadFile(inner) {
				return true
			}
		}
	case *parser.VarDeclStmt:
		return s != nil && expressionUsesTryReadFile(s.Initializer)
	case *parser.ExprStmt:
		return s != nil && expressionUsesTryReadFile(s.Expression)
	case *parser.IfStmt:
		return s != nil &&
			(expressionUsesTryReadFile(s.Condition) ||
				statementUsesTryReadFile(s.Then) ||
				statementUsesTryReadFile(s.Else))
	case *parser.WhileStmt:
		return s != nil && (expressionUsesTryReadFile(s.Condition) || statementUsesTryReadFile(s.Body))
	case *parser.ForStmt:
		return s != nil &&
			(statementUsesTryReadFile(s.Init) ||
				expressionUsesTryReadFile(s.Condition) ||
				expressionUsesTryReadFile(s.Increment) ||
				statementUsesTryReadFile(s.Body))
	case *parser.ReturnStmt:
		return s != nil && expressionUsesTryReadFile(s.Value)
	case *parser.MatchStmt:
		if s == nil {
			return false
		}
		if expressionUsesTryReadFile(s.Scrutinee) {
			return true
		}
		for _, arm := range s.Arms {
			if arm != nil && statementUsesTryReadFile(arm.Body) {
				return true
			}
		}
	}
	return false
}

// expressionUsesTryReadFile scans one expression and its children.
func expressionUsesTryReadFile(expression parser.Expr) bool {
	switch e := expression.(type) {
	case nil:
		return false
	case *parser.Identifier, *parser.IntLiteral, *parser.FloatLiteral,
		*parser.StringLiteral, *parser.BoolLiteral, *parser.NewMapExpr:
		return false
	case *parser.UnaryExpr:
		return e != nil && expressionUsesTryReadFile(e.Operand)
	case *parser.BinaryExpr:
		return e != nil && (expressionUsesTryReadFile(e.Left) || expressionUsesTryReadFile(e.Right))
	case *parser.AssignExpr:
		return e != nil && (expressionUsesTryReadFile(e.Target) || expressionUsesTryReadFile(e.Value))
	case *parser.CallExpr:
		if e == nil {
			return false
		}
		if identifier, ok := e.Callee.(*parser.Identifier); ok &&
			identifier != nil && identifier.Name == "try_read_file" {
			return true
		}
		if expressionUsesTryReadFile(e.Callee) {
			return true
		}
		for _, argument := range e.Arguments {
			if expressionUsesTryReadFile(argument) {
				return true
			}
		}
	case *parser.MemberExpr:
		return e != nil && expressionUsesTryReadFile(e.Receiver)
	case *parser.StructLiteral:
		if e == nil {
			return false
		}
		for _, field := range e.Fields {
			if field != nil && expressionUsesTryReadFile(field.Value) {
				return true
			}
		}
	case *parser.VariantLiteral:
		if e == nil {
			return false
		}
		for _, field := range e.Fields {
			if field != nil && expressionUsesTryReadFile(field.Value) {
				return true
			}
		}
	case *parser.NewArrayExpr:
		return e != nil && expressionUsesTryReadFile(e.Length)
	case *parser.IndexExpr:
		return e != nil && (expressionUsesTryReadFile(e.Target) || expressionUsesTryReadFile(e.Index))
	}
	return false
}

// declarationName returns the name and position of a top-level declaration.
func declarationName(declaration parser.Decl) (string, parser.Position, bool) {
	switch d := declaration.(type) {
	case *parser.FunctionDecl:
		if d != nil {
			return d.Name, d.At, true
		}
	case *parser.StructDecl:
		if d != nil {
			return d.Name, d.At, true
		}
	case *parser.EnumDecl:
		if d != nil {
			return d.Name, d.At, true
		}
	case *parser.UnionDecl:
		if d != nil {
			return d.Name, d.At, true
		}
	}
	return "", parser.Position{}, false
}
