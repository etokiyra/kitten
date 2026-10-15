package checker

import (
	"fmt"

	"kitten/parser"
)

// checkStatements checks a list of statements in the given scope.
func (c *checker) checkStatements(statements []parser.Stmt, scope *Scope) error {
	for _, statement := range statements {
		if err := c.checkStatement(statement, scope); err != nil {
			return err
		}
	}
	return nil
}

// checkStatement checks a single statement in the given scope.
func (c *checker) checkStatement(statement parser.Stmt, scope *Scope) error {
	switch s := statement.(type) {
	case *parser.BlockStmt:
		if s == nil {
			return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "block statement is nil")
		}
		return c.checkBlock(s, scope)

	case *parser.VarDeclStmt:
		return c.checkVarDecl(s, scope)

	case *parser.ExprStmt:
		if s == nil || s.Expression == nil {
			return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "expression statement has no expression")
		}
		_, err := c.checkExpr(s.Expression, scope)
		return err

	case *parser.IfStmt:
		return c.checkIf(s, scope)

	case *parser.WhileStmt:
		return c.checkWhile(s, scope)

	case *parser.ForStmt:
		return c.checkFor(s, scope)

	case *parser.ReturnStmt:
		return c.checkReturn(s, scope)

	default:
		if statement == nil {
			return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "statement is nil")
		}
		return c.errorAt(InvalidType, statement.Pos(), "unsupported statement %T", statement)
	}
}

// checkBlock checks a block's statements in a new nested scope.
func (c *checker) checkBlock(block *parser.BlockStmt, scope *Scope) error {
	if block == nil {
		return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "block is nil")
	}
	return c.checkStatements(block.Statements, newScope(scope))
}

// checkVarDecl checks a local variable declaration and introduces the name into
// scope. The initializer is checked before the name is declared, so a variable
// is not visible in its own initializer.
func (c *checker) checkVarDecl(declaration *parser.VarDeclStmt, scope *Scope) error {
	if declaration == nil {
		return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "variable declaration is nil")
	}

	declaredType, err := c.resolveType(declaration.Type)
	if err != nil {
		return err
	}
	if declaredType.IsVoid() {
		return c.errorAt(InvalidType, declaration.Type.Pos(), "variable %q cannot have type void", declaration.Name)
	}

	if declaration.Initializer != nil {
		initializerType, err := c.checkExpr(declaration.Initializer, scope)
		if err != nil {
			return err
		}
		context := fmt.Sprintf("initializer for variable %q", declaration.Name)
		if err := c.requireSameType(TypeMismatch, initializerType, declaredType, declaration.Initializer.Pos(), context); err != nil {
			return err
		}
	}

	symbol := &Symbol{Name: declaration.Name, Kind: SymbolVariable, Type: declaredType, At: declaration.At}
	if earlier := scope.declare(symbol); earlier != nil {
		return c.errorAt(DuplicateDeclaration, declaration.At, "duplicate declaration of %q in this scope (previously declared at %d:%d)", declaration.Name, earlier.At.Line, earlier.At.Column)
	}
	return nil
}

// checkIf checks an if statement. The condition is checked in the enclosing
// scope; each branch body gets its own nested scope.
func (c *checker) checkIf(statement *parser.IfStmt, scope *Scope) error {
	if statement == nil {
		return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "if statement is nil")
	}
	if err := c.checkCondition(statement.Condition, scope, "if"); err != nil {
		return err
	}
	if err := c.checkBlock(statement.Then, scope); err != nil {
		return err
	}
	if statement.Else != nil {
		// An else-if is an *IfStmt checked in the enclosing scope; an else body
		// is a block checked in a nested scope.
		return c.checkStatement(statement.Else, scope)
	}
	return nil
}

// checkWhile checks a while loop.
func (c *checker) checkWhile(statement *parser.WhileStmt, scope *Scope) error {
	if statement == nil {
		return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "while statement is nil")
	}
	if err := c.checkCondition(statement.Condition, scope, "while"); err != nil {
		return err
	}
	return c.checkBlock(statement.Body, scope)
}

// checkFor checks a C-style for loop.
//
// The loop gets its own scope containing the initializer, so an initializer
// variable is visible in the condition, the increment, and the body, but not
// after the loop.
func (c *checker) checkFor(statement *parser.ForStmt, scope *Scope) error {
	if statement == nil {
		return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "for statement is nil")
	}

	loopScope := newScope(scope)

	if statement.Init != nil {
		switch init := statement.Init.(type) {
		case *parser.VarDeclStmt:
			if err := c.checkVarDecl(init, loopScope); err != nil {
				return err
			}
		case *parser.ExprStmt:
			if init == nil || init.Expression == nil {
				return c.errorAt(InvalidType, statement.At, "for-loop initializer has no expression")
			}
			if _, err := c.checkExpr(init.Expression, loopScope); err != nil {
				return err
			}
		default:
			if err := c.checkStatement(statement.Init, loopScope); err != nil {
				return err
			}
		}
	}

	if statement.Condition != nil {
		if err := c.checkCondition(statement.Condition, loopScope, "for"); err != nil {
			return err
		}
	}

	if statement.Increment != nil {
		if _, err := c.checkExpr(statement.Increment, loopScope); err != nil {
			return err
		}
	}

	return c.checkBlock(statement.Body, loopScope)
}

// checkReturn checks a return statement against the enclosing function's
// return type.
func (c *checker) checkReturn(statement *parser.ReturnStmt, scope *Scope) error {
	if statement == nil {
		return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "return statement is nil")
	}
	if c.returnType == nil {
		return c.errorAt(InvalidReturn, statement.At, "return statement is not inside a function")
	}

	if c.returnType.IsVoid() {
		if statement.Value != nil {
			return c.errorAt(InvalidReturn, statement.Value.Pos(), "cannot return a value from a function that returns no value")
		}
		return nil
	}

	if statement.Value == nil {
		return c.errorAt(InvalidReturn, statement.At, "expected a return value of type %s", c.returnType)
	}

	valueType, err := c.checkExpr(statement.Value, scope)
	if err != nil {
		return err
	}
	return c.requireSameType(InvalidReturn, valueType, c.returnType, statement.Value.Pos(), "return value")
}

// checkCondition checks that an expression is a bool condition.
func (c *checker) checkCondition(expression parser.Expr, scope *Scope, construct string) error {
	if expression == nil {
		return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "%s condition is missing", construct)
	}
	conditionType, err := c.checkExpr(expression, scope)
	if err != nil {
		return err
	}
	if conditionType != BoolType {
		return c.errorAt(TypeMismatch, expression.Pos(), "%s condition must be bool, found %s", construct, conditionType)
	}
	return nil
}

// blockReturns reports whether every path through a block definitely returns.
//
// This is a conservative, syntactic analysis. A block definitely returns when
// any of its statements definitely returns; statements after a return are
// ignored for this purpose (unreachable code is not diagnosed). Loops and
// expression statements never definitely return.
func blockReturns(block *parser.BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, statement := range block.Statements {
		if statementReturns(statement) {
			return true
		}
	}
	return false
}

// statementReturns reports whether a statement definitely returns on every
// path. An if statement returns only when it has an else branch and both
// branches return.
func statementReturns(statement parser.Stmt) bool {
	switch s := statement.(type) {
	case *parser.ReturnStmt:
		return s != nil
	case *parser.BlockStmt:
		return blockReturns(s)
	case *parser.IfStmt:
		if s == nil || s.Else == nil {
			return false
		}
		return blockReturns(s.Then) && statementReturns(s.Else)
	default:
		return false
	}
}
