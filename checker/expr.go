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
	case *parser.VariantLiteral:
		return c.checkVariantLiteral(e, scope)
	case *parser.NewMapExpr:
		return c.checkNewMap(e, scope)
	case *parser.NewArrayExpr:
		return c.checkNewArray(e, scope)
	case *parser.IndexExpr:
		return c.checkIndex(e, scope)
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
	if _, ok := c.lookupFunction(identifier.Name); ok {
		return nil, c.errorAt(InvalidExpression, identifier.At, "function %q cannot be used as a value", identifier.Name)
	}
	if _, ok := c.builtins[identifier.Name]; ok {
		return nil, c.errorAt(InvalidExpression, identifier.At, "function %q cannot be used as a value", identifier.Name)
	}
	if _, ok := c.lookupType(identifier.Name); ok {
		return nil, c.errorAt(InvalidExpression, identifier.At, "type %q cannot be used as a value", identifier.Name)
	}
	if path, ok := c.privateFunction(identifier.Name); ok {
		return nil, c.errorAt(PrivateAccess, identifier.At, "function %q is private to %s", identifier.Name, path)
	}
	if path, ok := c.privateType(identifier.Name); ok {
		return nil, c.errorAt(PrivateAccess, identifier.At, "type %q is private to %s", identifier.Name, path)
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
// The operands must be the same type (no implicit conversions). "+" is defined
// for two ints, two floats, or two strings; on strings it concatenates. "%" is
// int-only. The remaining operators are numeric-only.
func (c *checker) checkArithmetic(operator string, left, right *Type, at parser.Position) (*Type, error) {
	if left.IsVoid() || right.IsVoid() {
		return nil, c.errorAt(InvalidOperator, at, "operator %q cannot be applied to a void value", operator)
	}
	if left != right {
		return nil, c.errorAt(TypeMismatch, at, "operator %q requires operands of the same type, found %s and %s", operator, left, right)
	}
	if operator == "+" && left == StringType {
		return StringType, nil
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
	if left.IsStruct() || left.IsArray() || left.IsUnion() || left.IsMap() {
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
		if expression.Operator == "+=" && targetType == StringType {
			// Appending is the compound form of string concatenation.
			return StringType, nil
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

	case *parser.IndexExpr:
		if e == nil || e.Target == nil {
			return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "index expression is incomplete")
		}
		targetType, err := c.checkAssignable(e.Target, scope)
		if err != nil {
			return nil, err
		}
		if targetType == StringType {
			return nil, c.errorAt(InvalidAssignment, e.At, "cannot assign to a string byte; strings are immutable")
		}
		return c.checkIndexOperands(e, targetType, scope)

	case nil:
		return nil, c.errorAt(InvalidAssignment, parser.Position{Line: 1, Column: 1}, "assignment target is missing")

	default:
		return nil, c.errorAt(InvalidAssignment, expression.Pos(), "cannot assign to %s", describeExpression(expression))
	}
}

// checkMember checks a member access expression used as a value.
//
// When the receiver is a type name rather than a value — "Enum.Case" or
// "Union.Variant" — the member is interpreted as a variant reference instead of
// a field access.
func (c *checker) checkMember(expression *parser.MemberExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Receiver == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "member expression is incomplete")
	}

	if identifier, ok := expression.Receiver.(*parser.Identifier); ok {
		if _, local := scope.lookup(identifier.Name); !local {
			if namedType, ok := c.lookupType(identifier.Name); ok && namedType.IsVariant() {
				return c.resolveVariantReference(expression, namedType)
			}
		}
	}

	receiverType, err := c.checkExpr(expression.Receiver, scope)
	if err != nil {
		return nil, err
	}
	return c.resolveMember(expression, receiverType)
}

// resolveVariantReference resolves a qualified variant reference, "Type.Variant",
// which denotes the variant itself. Only a variant with no fields is a value.
func (c *checker) resolveVariantReference(expression *parser.MemberExpr, namedType *Type) (*Type, error) {
	variant, ok := namedType.VariantIndex[expression.Member]
	if !ok {
		return nil, c.errorAt(UndefinedName, expression.Dot, "%s %s has no variant %q", variantKindWord(namedType), namedType.Name, expression.Member)
	}
	if variant.HasFields() {
		return nil, c.errorAt(InvalidType, expression.Dot, "variant %s.%s carries fields and must be constructed as %s.%s { ... }", namedType.Name, variant.Name, namedType.Name, variant.Name)
	}
	c.result.VariantRefs[expression] = variant
	return namedType, nil
}

// variantKindWord names the kind of a variant type for diagnostics.
func variantKindWord(namedType *Type) string {
	if namedType.IsEnum() {
		return "enum"
	}
	return "union"
}

// checkVariantLiteral checks a tagged-union variant construction,
// "Union.Variant { field = value, ... }".
func (c *checker) checkVariantLiteral(expression *parser.VariantLiteral, scope *Scope) (*Type, error) {
	if expression == nil || expression.Type == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "variant literal is missing its type")
	}

	unionType, err := c.resolveType(expression.Type)
	if err != nil {
		return nil, err
	}
	if !unionType.IsUnion() {
		return nil, c.errorAt(InvalidType, expression.Type.Pos(), "%s is not a union type", unionType)
	}

	variant, ok := unionType.VariantIndex[expression.Variant]
	if !ok {
		return nil, c.errorAt(UndefinedName, expression.At, "union %s has no variant %q", unionType.Name, expression.Variant)
	}
	if !variant.HasFields() {
		return nil, c.errorAt(InvalidType, expression.OpenBrace, "variant %s.%s carries no fields and is written without braces", unionType.Name, variant.Name)
	}

	initialized := map[string]*parser.FieldInit{}
	for _, field := range expression.Fields {
		if field == nil {
			return nil, c.errorAt(InvalidType, expression.OpenBrace, "variant literal for %s.%s has a nil field initializer", unionType.Name, variant.Name)
		}
		fieldDefinition, ok := variant.FieldIndex[field.Name]
		if !ok {
			return nil, c.errorAt(UndefinedName, field.At, "variant %s has no field %q", variant.Name, field.Name)
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
		context := fmt.Sprintf("field %q of %s.%s", field.Name, unionType.Name, variant.Name)
		if err := c.requireSameType(TypeMismatch, valueType, fieldDefinition.Type, field.Value.Pos(), context); err != nil {
			return nil, err
		}
	}

	for _, fieldDefinition := range variant.Fields {
		if _, ok := initialized[fieldDefinition.Name]; !ok {
			return nil, c.errorAt(TypeMismatch, expression.OpenBrace, "variant literal for %s.%s is missing field %q", unionType.Name, variant.Name, fieldDefinition.Name)
		}
	}

	c.result.VariantRefs[expression] = variant
	return unionType, nil
}

// resolveMember resolves a field access on a receiver of the given type.
func (c *checker) resolveMember(expression *parser.MemberExpr, receiverType *Type) (*Type, error) {
	if receiverType == nil || receiverType.IsVoid() {
		return nil, c.errorAt(TypeMismatch, expression.Dot, "cannot access member %q on a value with no value", expression.Member)
	}
	if receiverType.IsUnion() {
		return nil, c.errorAt(TypeMismatch, expression.Dot, "cannot access member %q on union %s; use match to inspect a variant", expression.Member, receiverType.Name)
	}
	if receiverType.IsEnum() {
		return nil, c.errorAt(TypeMismatch, expression.Dot, "cannot access member %q on enum %s", expression.Member, receiverType.Name)
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

	switch function.Builtin {
	case BuiltinPrint:
		return c.checkVariadicCall(expression, function, scope)
	case BuiltinLength:
		return c.checkLengthCall(expression, function, scope)
	case BuiltinMapHas:
		return c.checkMapHasCall(expression, function, scope)
	case BuiltinSubstring:
		return c.checkSubstringCall(expression, function, scope)
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
	if function, ok := c.lookupFunction(identifier.Name); ok {
		c.result.Symbols[identifier] = functionSymbol(function, identifier.At)
		return function, nil
	}
	if function, ok := c.builtins[identifier.Name]; ok {
		c.result.Symbols[identifier] = functionSymbol(function, identifier.At)
		return function, nil
	}
	if _, ok := c.lookupType(identifier.Name); ok {
		return nil, c.errorAt(BadCall, identifier.At, "type %q is not a function", identifier.Name)
	}
	if path, ok := c.privateFunction(identifier.Name); ok {
		return nil, c.errorAt(PrivateAccess, identifier.At, "function %q is private to %s", identifier.Name, path)
	}
	if path, ok := c.privateType(identifier.Name); ok {
		return nil, c.errorAt(PrivateAccess, identifier.At, "type %q is private to %s", identifier.Name, path)
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

// checkNewArray checks an array creation expression, "new Element[length]".
func (c *checker) checkNewArray(expression *parser.NewArrayExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Element == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "array creation is missing its element type")
	}
	elementType, err := c.resolveType(expression.Element)
	if err != nil {
		return nil, err
	}
	if elementType.IsVoid() {
		return nil, c.errorAt(InvalidType, expression.Element.Pos(), "array element type cannot be void")
	}

	if expression.Length == nil {
		return nil, c.errorAt(InvalidExpression, expression.At, "array creation is missing its length")
	}
	lengthType, err := c.checkExpr(expression.Length, scope)
	if err != nil {
		return nil, err
	}
	if lengthType != IntType {
		return nil, c.errorAt(TypeMismatch, expression.Length.Pos(), "array length must be int, found %s", lengthType)
	}
	return c.arrayType(elementType), nil
}

// checkIndex checks an indexing expression used as a value.
func (c *checker) checkIndex(expression *parser.IndexExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Target == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "index expression is incomplete")
	}
	targetType, err := c.checkExpr(expression.Target, scope)
	if err != nil {
		return nil, err
	}
	return c.checkIndexOperands(expression, targetType, scope)
}

// checkIndexOperands checks the index of an indexing expression whose target
// already has type targetType, and returns the element, value, or byte type.
func (c *checker) checkIndexOperands(expression *parser.IndexExpr, targetType *Type, scope *Scope) (*Type, error) {
	if !targetType.IsArray() && !targetType.IsMap() && targetType != StringType {
		return nil, c.errorAt(TypeMismatch, expression.At, "cannot index a value of type %s", targetType)
	}
	if expression.Index == nil {
		return nil, c.errorAt(InvalidExpression, expression.At, "index expression is missing its index")
	}
	indexType, err := c.checkExpr(expression.Index, scope)
	if err != nil {
		return nil, err
	}

	if targetType.IsMap() {
		if indexType != targetType.Key {
			return nil, c.errorAt(TypeMismatch, expression.Index.Pos(), "map key must be %s, found %s", targetType.Key, indexType)
		}
		return targetType.Value, nil
	}

	if indexType != IntType {
		if targetType == StringType {
			return nil, c.errorAt(TypeMismatch, expression.Index.Pos(), "string index must be int, found %s", indexType)
		}
		return nil, c.errorAt(TypeMismatch, expression.Index.Pos(), "array index must be int, found %s", indexType)
	}
	if targetType == StringType {
		// A string is indexed by byte and yields the byte's value.
		return IntType, nil
	}
	return targetType.Element, nil
}

// checkLengthCall checks a call to the built-in length, which takes exactly one
// array, map, or string argument and returns its number of elements or bytes as
// an int.
func (c *checker) checkLengthCall(expression *parser.CallExpr, function *Function, scope *Scope) (*Type, error) {
	if len(expression.Arguments) != 1 {
		return nil, c.errorAt(BadCall, expression.OpenParen, "function %q expects 1 argument, got %d", function.Name, len(expression.Arguments))
	}
	argument := expression.Arguments[0]
	argumentType, err := c.checkExpr(argument, scope)
	if err != nil {
		return nil, err
	}
	if !argumentType.IsArray() && !argumentType.IsMap() && argumentType != StringType {
		return nil, c.errorAt(TypeMismatch, argument.Pos(), "length expects an array, map, or string, found %s", argumentType)
	}
	return IntType, nil
}

// checkMapHasCall checks a call to the built-in has, which takes a map and a key
// and reports whether the key is present.
func (c *checker) checkMapHasCall(expression *parser.CallExpr, function *Function, scope *Scope) (*Type, error) {
	if len(expression.Arguments) != 2 {
		return nil, c.errorAt(BadCall, expression.OpenParen, "function %q expects 2 arguments, got %d", function.Name, len(expression.Arguments))
	}

	mapArgument := expression.Arguments[0]
	mapType, err := c.checkExpr(mapArgument, scope)
	if err != nil {
		return nil, err
	}
	if !mapType.IsMap() {
		return nil, c.errorAt(TypeMismatch, mapArgument.Pos(), "%q expects a map as its first argument, found %s", function.Name, mapType)
	}

	keyArgument := expression.Arguments[1]
	keyType, err := c.checkExpr(keyArgument, scope)
	if err != nil {
		return nil, err
	}
	if keyType != mapType.Key {
		return nil, c.errorAt(TypeMismatch, keyArgument.Pos(), "map key must be %s, found %s", mapType.Key, keyType)
	}
	return BoolType, nil
}

// checkSubstringCall checks a call to the built-in substring, which takes a
// string and two int byte offsets and returns the bytes in [start, end).
func (c *checker) checkSubstringCall(expression *parser.CallExpr, function *Function, scope *Scope) (*Type, error) {
	if len(expression.Arguments) != 3 {
		return nil, c.errorAt(BadCall, expression.OpenParen, "function %q expects 3 arguments, got %d", function.Name, len(expression.Arguments))
	}

	sourceType, err := c.checkExpr(expression.Arguments[0], scope)
	if err != nil {
		return nil, err
	}
	if sourceType != StringType {
		return nil, c.errorAt(TypeMismatch, expression.Arguments[0].Pos(), "%q expects a string as its first argument, found %s", function.Name, sourceType)
	}

	for _, argument := range expression.Arguments[1:] {
		offsetType, err := c.checkExpr(argument, scope)
		if err != nil {
			return nil, err
		}
		if offsetType != IntType {
			return nil, c.errorAt(TypeMismatch, argument.Pos(), "substring offsets must be int, found %s", offsetType)
		}
	}
	return StringType, nil
}

// checkNewMap checks map creation, "new map[Key]Value".
func (c *checker) checkNewMap(expression *parser.NewMapExpr, scope *Scope) (*Type, error) {
	if expression == nil || expression.Map == nil {
		return nil, c.errorAt(InvalidExpression, parser.Position{Line: 1, Column: 1}, "map creation is missing its type")
	}
	mapType, err := c.resolveType(expression.Map)
	if err != nil {
		return nil, err
	}
	if !mapType.IsMap() {
		return nil, c.errorAt(InvalidType, expression.Map.Pos(), "%s is not a map type", mapType)
	}
	return mapType, nil
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
	case *parser.VariantLiteral:
		return "a variant literal"
	case *parser.NewArrayExpr:
		return "an array creation"
	case *parser.NewMapExpr:
		return "a map creation"
	case *parser.IndexExpr:
		return "an indexed element"
	case nil:
		return "a missing expression"
	default:
		return "that expression"
	}
}
