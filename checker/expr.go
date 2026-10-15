package checker

import (
	"fmt"

	"kitten/parser"
)

// checkExpr checks an expression and returns its type. Every expression and
// sub-expression that is checked has its type recorded in the Result.
func (c *checker) checkExpr(expression parser.Expr, scope *Scope) (*Type, error) {
	if expression == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "expression is missing")
	}
	expressionType, err := c.checkExpression(expression, scope)
	if err != nil {
		return nil, err
	}
	c.result.ExpressionTypes[expression] = expressionType
	return expressionType, nil
}

// checkExpression dispatches on the concrete expression node.
func (c *checker) checkExpression(expression parser.Expr, scope *Scope) (*Type, error) {
	switch e := expression.(type) {
	case *parser.IntLiteral:
		return IntType, nil
	case *parser.FloatLiteral:
		return FloatType, nil
	case *parser.StringLiteral:
		return StringType, nil
	case *parser.BoolLiteral:
		return BoolType, nil
	case *parser.Identifier:
		return c.checkIdentifier(e, scope)
	case *parser.UnaryExpr:
		return c.checkUnary(e, scope)
	case *parser.BinaryExpr:
		return c.checkBinary(e, scope)
	case *parser.AssignExpr:
		return c.checkAssignment(e, scope)
	case *parser.CallExpr:
		return c.checkCall(e, scope)
	case *parser.MemberExpr:
		return c.checkMember(e, scope)
	case *parser.StructLiteral:
		return c.checkStructLiteral(e, scope)
	default:
		return nil, c.errorAt(InvalidExpression, expression.Pos(), "unsupported expression %T", expression)
	}
}

// checkIdentifier resolves an identifier used as a value. Only variables and
// parameters are values; functions and types are not.
func (c *checker) checkIdentifier(identifier *parser.Identifier, scope *Scope) (*Type, error) {
	if identifier == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "identifier is nil")
	}

	if symbol, ok := scope.lookup(identifier.Name); ok {
		c.result.Symbols[identifier] = symbol
		return symbol.Type, nil
	}
	if _, ok := c.functions[identifier.Name]; ok {
		return nil, c.errorAt(InvalidExpression, identifier.At, "function %q cannot be used as a value", identifier.Name)
	}
	if _, ok := c.builtins[identifier.Name]; ok {
		return nil, c.errorAt(InvalidExpression, identifier.At, "function %q cannot be used as a value", identifier.Name)
	}
	if _, ok := c.structs[identifier.Name]; ok {
		return nil, c.errorAt(InvalidExpression, identifier.At, "type %q cannot be used as a value", identifier.Name)
	}
	return nil, c.errorAt(UndefinedName, identifier.At, "undefined name %q", identifier.Name)
}

// checkUnary checks a prefix or postfix unary expression.
func (c *checker) checkUnary(expression *parser.UnaryExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Operand == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "unary expression is incomplete")
	}

	switch expression.Operator {
	case "-":
		operandType, err := c.checkExpr(expression.Operand, scope)
		if err != nil {
			return nil, err
		}
		if !operandType.IsNumeric() {
			return nil, c.errorAt(InvalidOperator, expression.OperatorAt, "operator '-' requires a numeric operand, found %s", operandType)
		}
		return operandType, nil

	case "!":
		operandType, err := c.checkExpr(expression.Operand, scope)
		if err != nil {
			return nil, err
		}
		if operandType != BoolType {
			return nil, c.errorAt(InvalidOperator, expression.OperatorAt, "operator '!' requires a bool operand, found %s", operandType)
		}
		return BoolType, nil

	case "++", "--":
		operandType, err := c.checkAssignable(expression.Operand, scope)
		if err != nil {
			return nil, err
		}
		if !operandType.IsNumeric() {
			return nil, c.errorAt(InvalidAssignment, expression.OperatorAt, "operator %q requires a numeric variable or field, found %s", expression.Operator, operandType)
		}
		return operandType, nil

	default:
		return nil, c.errorAt(InvalidOperator, expression.OperatorAt, "unsupported unary operator %q", expression.Operator)
	}
}

// checkBinary checks a binary operation.
func (c *checker) checkBinary(expression *parser.BinaryExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Left == nil || expression.Right == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "binary expression is incomplete")
	}

	leftType, err := c.checkExpr(expression.Left, scope)
	if err != nil {
		return nil, err
	}
	rightType, err := c.checkExpr(expression.Right, scope)
	if err != nil {
		return nil, err
	}

	switch expression.Operator {
	case "+", "-", "*", "/", "%":
		return c.checkArithmetic(expression.Operator, leftType, rightType, expression.OperatorAt)
	case "<", "<=", ">", ">=":
		return c.checkRelational(expression.Operator, leftType, rightType, expression.OperatorAt)
	case "==", "!=":
		return c.checkEquality(expression.Operator, leftType, rightType, expression.OperatorAt)
	case "&&", "||":
		return c.checkLogical(expression.Operator, leftType, rightType, expression.OperatorAt)
	default:
		return nil, c.errorAt(InvalidOperator, expression.OperatorAt, "unsupported binary operator %q", expression.Operator)
	}
}

// checkArithmetic checks "+", "-", "*", "/", and "%".
//
// The operands must be the same numeric type (no implicit conversions). "%" is
// int-only. String concatenation is not supported.
func (c *checker) checkArithmetic(operator string, left, right *Type, at parser.Position) (*Type, error) {
	if left.IsVoid() || right.IsVoid() {
		return nil, c.errorAt(InvalidOperator, at, "operator %q cannot be applied to a void value", operator)
	}
	if left != right {
		return nil, c.errorAt(TypeMismatch, at, "operator %q requires operands of the same type, found %s and %s", operator, left, right)
	}
	if operator == "%" {
		if left != IntType {
			return nil, c.errorAt(InvalidOperator, at, "operator '%%' requires int operands, found %s", left)
		}
		return IntType, nil
	}
	if !left.IsNumeric() {
		return nil, c.errorAt(InvalidOperator, at, "operator %q requires numeric operands, found %s", operator, left)
	}
	return left, nil
}

// checkRelational checks "<", "<=", ">", and ">=". The operands must be the
// same numeric type; the result is bool.
func (c *checker) checkRelational(operator string, left, right *Type, at parser.Position) (*Type, error) {
	if left.IsVoid() || right.IsVoid() {
		return nil, c.errorAt(InvalidOperator, at, "operator %q cannot be applied to a void value", operator)
	}
	if left != right {
		return nil, c.errorAt(TypeMismatch, at, "operator %q requires operands of the same type, found %s and %s", operator, left, right)
	}
	if !left.IsNumeric() {
		return nil, c.errorAt(InvalidOperator, at, "operator %q requires numeric operands, found %s", operator, left)
	}
	return BoolType, nil
}

// checkEquality checks "==" and "!=". The operands must be the same primitive
// type; the result is bool. Struct equality is not supported.
func (c *checker) checkEquality(operator string, left, right *Type, at parser.Position) (*Type, error) {
	if left.IsVoid() || right.IsVoid() {
		return nil, c.errorAt(InvalidOperator, at, "operator %q cannot be applied to a void value", operator)
	}
	if left != right {
		return nil, c.errorAt(TypeMismatch, at, "operator %q requires operands of the same type, found %s and %s", operator, left, right)
	}
	if left.IsStruct() {
		return nil, c.errorAt(InvalidOperator, at, "operator %q is not supported for values of type %s", operator, left)
	}
	return BoolType, nil
}

// checkLogical checks "&&" and "||". Both operands must be bool; the result is
// bool.
func (c *checker) checkLogical(operator string, left, right *Type, at parser.Position) (*Type, error) {
	if left != BoolType || right != BoolType {
		return nil, c.errorAt(InvalidOperator, at, "operator %q requires bool operands, found %s and %s", operator, left, right)
	}
	return BoolType, nil
}

// checkAssignment checks "=", "+=", and "-=".
func (c *checker) checkAssignment(expression *parser.AssignExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Target == nil || expression.Value == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "assignment is incomplete")
	}

	targetType, err := c.checkAssignable(expression.Target, scope)
	if err != nil {
		return nil, err
	}
	valueType, err := c.checkExpr(expression.Value, scope)
	if err != nil {
		return nil, err
	}

	switch expression.Operator {
	case "=":
		context := fmt.Sprintf("assignment to %s", targetType)
		if err := c.requireSameType(TypeMismatch, valueType, targetType, expression.OperatorAt, context); err != nil {
			return nil, err
		}
		return targetType, nil

	case "+=", "-=":
		if targetType.IsVoid() || valueType.IsVoid() {
			return nil, c.errorAt(InvalidOperator, expression.OperatorAt, "operator %q cannot be applied to a void value", expression.Operator)
		}
		if targetType != valueType {
			return nil, c.errorAt(TypeMismatch, expression.OperatorAt, "operator %q requires operands of the same type, found %s and %s", expression.Operator, targetType, valueType)
		}
		if !targetType.IsNumeric() {
			return nil, c.errorAt(InvalidOperator, expression.OperatorAt, "operator %q requires numeric operands, found %s", expression.Operator, targetType)
		}
		return targetType, nil

	default:
		return nil, c.errorAt(InvalidOperator, expression.OperatorAt, "unsupported assignment operator %q", expression.Operator)
	}
}

// checkAssignable checks that an expression is a mutable l-value and returns its
// type. Only local variables, parameters, and struct fields reached through an
// l-value are assignable.
func (c *checker) checkAssignable(expression parser.Expr, scope *Scope) (*Type, error) {
	expressionType, err := c.assignableType(expression, scope)
	if err != nil {
		return nil, err
	}
	c.result.ExpressionTypes[expression] = expressionType
	return expressionType, nil
}

// assignableType implements checkAssignable without recording the expression
// type, so that the recording happens exactly once in checkAssignable.
func (c *checker) assignableType(expression parser.Expr, scope *Scope) (*Type, error) {
	switch e := expression.(type) {
	case *parser.Identifier:
		if e == nil {
			return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "identifier is nil")
		}
		symbol, ok := scope.lookup(e.Name)
		if !ok {
			return nil, c.errorAt(UndefinedName, e.At, "undefined name %q", e.Name)
		}
		c.result.Symbols[e] = symbol
		if symbol.Kind != SymbolVariable {
			return nil, c.errorAt(InvalidAssignment, e.At, "%q is not assignable", e.Name)
		}
		return symbol.Type, nil

	case *parser.MemberExpr:
		if e == nil || e.Receiver == nil {
			return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "member expression is incomplete")
		}
		receiverType, err := c.checkAssignable(e.Receiver, scope)
		if err != nil {
			return nil, err
		}
		return c.resolveMember(e, receiverType)

	case nil:
		return nil, c.errorAt(InvalidAssignment, parser.Position{Line: 1, Column: 1}, "assignment target is missing")

	default:
		return nil, c.errorAt(InvalidAssignment, expression.Pos(), "cannot assign to %s", describeExpression(expression))
	}
}

// checkMember checks a member access expression used as a value.
func (c *checker) checkMember(expression *parser.MemberExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Receiver == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "member expression is incomplete")
	}
	receiverType, err := c.checkExpr(expression.Receiver, scope)
	if err != nil {
		return nil, err
	}
	return c.resolveMember(expression, receiverType)
}

// resolveMember resolves a field access on a receiver of the given type.
func (c *checker) resolveMember(expression *parser.MemberExpr, receiverType *Type) (*Type, error) {
	if receiverType == nil || receiverType.IsVoid() {
		return nil, c.errorAt(TypeMismatch, expression.Dot, "cannot access member %q on a value with no value", expression.Member)
	}
	if !receiverType.IsStruct() {
		return nil, c.errorAt(TypeMismatch, expression.Dot, "cannot access member %q on a value of type %s", expression.Member, receiverType)
	}
	field, ok := receiverType.Struct.FieldIndex[expression.Member]
	if !ok {
		return nil, c.errorAt(UndefinedName, expression.Dot, "struct %s has no field %q", receiverType.Name, expression.Member)
	}
	return field.Type, nil
}

// checkCall checks a function call.
func (c *checker) checkCall(expression *parser.CallExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Callee == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "call expression is incomplete")
	}

	function, err := c.resolveCallee(expression.Callee, scope)
	if err != nil {
		return nil, err
	}

	if function.Variadic {
		return c.checkVariadicCall(expression, function, scope)
	}

	if len(expression.Arguments) != len(function.ParameterTypes) {
		return nil, c.errorAt(BadCall, expression.OpenParen, "function %q expects %d argument(s), got %d", function.Name, len(function.ParameterTypes), len(expression.Arguments))
	}

	for index, argument := range expression.Arguments {
		argumentType, err := c.checkExpr(argument, scope)
		if err != nil {
			return nil, err
		}
		context := fmt.Sprintf("argument %d of %q", index+1, function.Name)
		if err := c.requireSameType(TypeMismatch, argumentType, function.ParameterTypes[index], argument.Pos(), context); err != nil {
			return nil, err
		}
	}

	return function.ReturnType, nil
}

// resolveCallee resolves the callee of a call expression to a function. The
// callee must be a plain identifier naming a user function or a built-in.
func (c *checker) resolveCallee(callee parser.Expr, scope *Scope) (*Function, error) {
	identifier, ok := callee.(*parser.Identifier)
	if !ok || identifier == nil {
		return nil, c.errorAt(BadCall, callee.Pos(), "cannot call %s", describeExpression(callee))
	}

	if symbol, ok := scope.lookup(identifier.Name); ok {
		c.result.Symbols[identifier] = symbol
		return nil, c.errorAt(BadCall, identifier.At, "%q is a variable, not a function", identifier.Name)
	}
	if function, ok := c.functions[identifier.Name]; ok {
		c.result.Symbols[identifier] = functionSymbol(function, identifier.At)
		return function, nil
	}
	if function, ok := c.builtins[identifier.Name]; ok {
		c.result.Symbols[identifier] = functionSymbol(function, identifier.At)
		return function, nil
	}
	if _, ok := c.structs[identifier.Name]; ok {
		return nil, c.errorAt(BadCall, identifier.At, "type %q is not a function", identifier.Name)
	}
	return nil, c.errorAt(UndefinedName, identifier.At, "undefined function %q", identifier.Name)
}

// functionSymbol builds the Symbol recorded for a resolved call callee.
func functionSymbol(function *Function, at parser.Position) *Symbol {
	return &Symbol{
		Name:     function.Name,
		Kind:     SymbolFunction,
		Type:     function.ReturnType,
		Function: function,
		At:       at,
	}
}

// checkVariadicCall checks a call to a variadic built-in such as print. It
// requires at least one argument, and every argument must be one of the
// built-in's allowed types.
func (c *checker) checkVariadicCall(expression *parser.CallExpr, function *Function, scope *Scope) (*Type, error) {
	if len(expression.Arguments) == 0 {
		return nil, c.errorAt(BadCall, expression.OpenParen, "function %q expects at least one argument", function.Name)
	}
	for index, argument := range expression.Arguments {
		argumentType, err := c.checkExpr(argument, scope)
		if err != nil {
			return nil, err
		}
		if !containsType(function.AllowedTypes, argumentType) {
			return nil, c.errorAt(TypeMismatch, argument.Pos(), "argument %d of %q cannot print a value of type %s", index+1, function.Name, argumentType)
		}
	}
	return function.ReturnType, nil
}

// checkStructLiteral checks a struct literal expression.
//
// The named type must be a struct; every supplied field must exist, be
// initialized at most once, and have the declared type; and every field of the
// struct must be initialized.
func (c *checker) checkStructLiteral(expression *parser.StructLiteral, scope *Scope) (*Type, error) {
	if expression == nil || expression.Type == nil {
		return nil, c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "struct literal is missing its type")
	}

	structType, err := c.resolveType(expression.Type)
	if err != nil {
		return nil, err
	}
	if !structType.IsStruct() {
		return nil, c.errorAt(InvalidType, expression.Type.Pos(), "%s is not a struct type", structType)
	}

	info := structType.Struct
	initialized := map[string]*parser.FieldInit{}

	for _, field := range expression.Fields {
		if field == nil {
			return nil, c.errorAt(InvalidType, expression.Type.Pos(), "struct literal for %s has a nil field initializer", structType.Name)
		}
		structField, ok := info.FieldIndex[field.Name]
		if !ok {
			return nil, c.errorAt(UndefinedName, field.At, "struct %s has no field %q", structType.Name, field.Name)
		}
		if earlier, ok := initialized[field.Name]; ok {
			return nil, c.errorAt(DuplicateDeclaration, field.At, "field %q is initialized more than once (first at %d:%d)", field.Name, earlier.At.Line, earlier.At.Column)
		}
		initialized[field.Name] = field

		if field.Value == nil {
			return nil, c.errorAt(InvalidType, field.At, "field %q has no value", field.Name)
		}
		valueType, err := c.checkExpr(field.Value, scope)
		if err != nil {
			return nil, err
		}
		context := fmt.Sprintf("field %q of %s", field.Name, structType.Name)
		if err := c.requireSameType(TypeMismatch, valueType, structField.Type, field.Value.Pos(), context); err != nil {
			return nil, err
		}
	}

	for _, structField := range info.Fields {
		if _, ok := initialized[structField.Name]; !ok {
			return nil, c.errorAt(TypeMismatch, expression.OpenBrace, "struct literal for %s is missing field %q", structType.Name, structField.Name)
		}
	}

	return structType, nil
}

// containsType reports whether types contains target.
func containsType(types []*Type, target *Type) bool {
	for _, candidate := range types {
		if candidate == target {
			return true
		}
	}
	return false
}

// describeExpression renders an expression for use in an error message.
func describeExpression(expression parser.Expr) string {
	switch e := expression.(type) {
	case *parser.Identifier:
		return fmt.Sprintf("variable %q", e.Name)
	case *parser.IntLiteral:
		return "an integer literal"
	case *parser.FloatLiteral:
		return "a float literal"
	case *parser.StringLiteral:
		return "a string literal"
	case *parser.BoolLiteral:
		return "a bool literal"
	case *parser.CallExpr:
		return "a function call"
	case *parser.MemberExpr:
		return fmt.Sprintf("member %q", e.Member)
	case *parser.BinaryExpr:
		return fmt.Sprintf("a %q expression", e.Operator)
	case *parser.UnaryExpr:
		return fmt.Sprintf("a %q expression", e.Operator)
	case *parser.AssignExpr:
		return "an assignment"
	case *parser.StructLiteral:
		return "a struct literal"
	case nil:
		return "a missing expression"
	default:
		return "that expression"
	}
}
