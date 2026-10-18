package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"kitten/checker"
	"kitten/parser"
	"kitten/runtime"
)

// emitBlock writes a brace-delimited block, indented and newline-terminated.
// The caller is responsible for any text preceding the opening brace, so that
// constructs like "if (...)" can share the line.
func (g *generator) emitBlock(block *parser.BlockStmt) error {
	if block == nil {
		return &Error{Line: 1, Column: 1, Message: "missing block"}
	}
	g.write("{\n")
	g.indent++
	for _, statement := range block.Statements {
		if statement == nil {
			return &Error{Line: block.At.Line, Column: block.At.Column, Message: "block contains a nil statement"}
		}
		if err := g.emitStatement(statement); err != nil {
			return err
		}
	}
	g.indent--
	g.line("}")
	return nil
}

// emitStatement writes one statement.
func (g *generator) emitStatement(statement parser.Stmt) error {
	switch s := statement.(type) {
	case *parser.BlockStmt:
		g.writeIndent()
		return g.emitBlock(s)

	case *parser.VarDeclStmt:
		declaration, err := g.varDeclaration(s)
		if err != nil {
			return err
		}
		g.line("%s;", declaration)
		return nil

	case *parser.ExprStmt:
		expression, err := g.expression(s.Expression)
		if err != nil {
			return err
		}
		g.line("%s;", expression)
		return nil

	case *parser.IfStmt:
		g.writeIndent()
		return g.emitIfTail(s)

	case *parser.WhileStmt:
		condition, err := g.conditionExpression(s.Condition)
		if err != nil {
			return err
		}
		g.writeIndent()
		g.write("while " + condition + " ")
		return g.emitBlock(s.Body)

	case *parser.ForStmt:
		return g.emitFor(s)

	case *parser.ReturnStmt:
		if s.Value == nil {
			g.line("return;")
			return nil
		}
		value, err := g.expression(s.Value)
		if err != nil {
			return err
		}
		g.line("return %s;", value)
		return nil

	case *parser.MatchStmt:
		return g.emitMatch(s)

	default:
		if statement == nil {
			return &Error{Line: 1, Column: 1, Message: "nil statement"}
		}
		position := statement.Pos()
		return &Error{Line: position.Line, Column: position.Column, Message: fmt.Sprintf("unsupported statement %T", statement)}
	}
}

// emitIfTail writes "if (...) { ... }" and any "else" chain. It assumes the
// caller has already written the leading indentation.
func (g *generator) emitIfTail(statement *parser.IfStmt) error {
	if statement == nil || statement.Condition == nil {
		return &Error{Line: 1, Column: 1, Message: "incomplete if statement"}
	}
	condition, err := g.conditionExpression(statement.Condition)
	if err != nil {
		return err
	}
	g.write("if " + condition + " ")
	if err := g.emitBlock(statement.Then); err != nil {
		return err
	}

	switch elseBranch := statement.Else.(type) {
	case nil:
		return nil
	case *parser.IfStmt:
		g.writeIndent()
		g.write("else ")
		return g.emitIfTail(elseBranch)
	case *parser.BlockStmt:
		g.writeIndent()
		g.write("else ")
		return g.emitBlock(elseBranch)
	default:
		return &Error{Line: statement.At.Line, Column: statement.At.Column, Message: fmt.Sprintf("unsupported else branch %T", statement.Else)}
	}
}

// emitFor writes a C-style for loop.
func (g *generator) emitFor(statement *parser.ForStmt) error {
	if statement == nil {
		return &Error{Line: 1, Column: 1, Message: "nil for statement"}
	}

	initializer := ""
	switch init := statement.Init.(type) {
	case nil:
	case *parser.VarDeclStmt:
		declaration, err := g.varDeclaration(init)
		if err != nil {
			return err
		}
		initializer = declaration
	case *parser.ExprStmt:
		if init.Expression == nil {
			return &Error{Line: statement.At.Line, Column: statement.At.Column, Message: "for-loop initializer has no expression"}
		}
		expression, err := g.expression(init.Expression)
		if err != nil {
			return err
		}
		initializer = expression
	default:
		return &Error{Line: statement.At.Line, Column: statement.At.Column, Message: fmt.Sprintf("unsupported for-loop initializer %T", statement.Init)}
	}

	condition := ""
	if statement.Condition != nil {
		rendered, err := g.conditionExpression(statement.Condition)
		if err != nil {
			return err
		}
		condition = rendered
	}

	increment := ""
	if statement.Increment != nil {
		expression, err := g.expression(statement.Increment)
		if err != nil {
			return err
		}
		increment = expression
	}

	g.writeIndent()
	g.write("for (" + initializer + "; " + condition + "; " + increment + ") ")
	return g.emitBlock(statement.Body)
}

// varDeclaration renders a local variable declaration without its trailing
// semicolon, so it can also be used as a for-loop initializer.
func (g *generator) varDeclaration(declaration *parser.VarDeclStmt) (string, error) {
	if declaration == nil {
		return "", &Error{Line: 1, Column: 1, Message: "nil variable declaration"}
	}
	declaredType, err := g.declaredType(declaration.Type)
	if err != nil {
		return "", err
	}
	cDeclaredType, err := cType(declaredType)
	if err != nil {
		return "", &Error{Line: declaration.At.Line, Column: declaration.At.Column, Message: err.Error()}
	}

	text := cDeclaredType + " " + variableName(declaration.Name)
	if declaration.Initializer != nil {
		initializer, err := g.expression(declaration.Initializer)
		if err != nil {
			return "", err
		}
		text += " = " + initializer
	}
	return text, nil
}

// stringLiteralName renders a string literal as a reference to its pooled
// static object, declaring that object if this is the literal's first use.
//
// Equal literals share one object, because the C spelling produced by
// cStringLiteral is a one-to-one encoding of the literal's bytes.
func (g *generator) stringLiteralName(literal *parser.StringLiteral) (string, error) {
	escaped, length, err := cStringLiteral(literal.Text)
	if err != nil {
		return "", &Error{Line: literal.At.Line, Column: literal.At.Column, Message: err.Error()}
	}
	if name, ok := g.literalNames[escaped]; ok {
		return "(&" + name + ")", nil
	}

	name := fmt.Sprintf("%s%d", literalPrefix, len(g.literals)+1)
	g.literals = append(g.literals, stringLiteral{name: name, escaped: escaped, length: length})
	g.literalNames[escaped] = name
	return "(&" + name + ")", nil
}

// integerLiteralText renders a checked integer literal. Decimal literals keep
// their source spelling; hexadecimal and binary literals are canonicalized to a
// signed decimal C expression so their 64-bit bit pattern is unambiguous.
func integerLiteralText(text string) string {
	if len(text) < 3 || text[0] != '0' || (text[1] != 'x' && text[1] != 'b') {
		return text
	}
	base := 16
	if text[1] == 'b' {
		base = 2
	}
	value, err := strconv.ParseUint(text[2:], base, 64)
	if err != nil {
		return text
	}
	if value == uint64(1)<<63 {
		return "INT64_MIN"
	}
	return strconv.FormatInt(int64(value), 10)
}

// expression renders an expression as a C expression.
func (g *generator) expression(expression parser.Expr) (string, error) {
	switch e := expression.(type) {
	case *parser.IntLiteral:
		return integerLiteralText(e.Text), nil
	case *parser.FloatLiteral:
		return e.Text, nil
	case *parser.BoolLiteral:
		if e.Value {
			return "true", nil
		}
		return "false", nil
	case *parser.StringLiteral:
		return g.stringLiteralName(e)
	case *parser.Identifier:
		return variableName(e.Name), nil
	case *parser.UnaryExpr:
		return g.unaryExpression(e)
	case *parser.BinaryExpr:
		return g.binaryExpression(e)
	case *parser.AssignExpr:
		return g.assignExpression(e)
	case *parser.CallExpr:
		return g.callExpression(e)
	case *parser.MemberExpr:
		return g.memberExpression(e)
	case *parser.StructLiteral:
		return g.structLiteral(e)
	case *parser.VariantLiteral:
		return g.variantLiteral(e)
	case *parser.NewArrayExpr:
		return g.newArrayExpression(e)
	case *parser.NewMapExpr:
		return g.newMapExpression(e)
	case *parser.IndexExpr:
		return g.indexExpression(e)
	default:
		if expression == nil {
			return "", &Error{Line: 1, Column: 1, Message: "nil expression"}
		}
		position := expression.Pos()
		return "", &Error{Line: position.Line, Column: position.Column, Message: fmt.Sprintf("unsupported expression %T", expression)}
	}
}

// unaryExpression renders prefix and postfix unary operations. Integer
// arithmetic uses the defined runtime helpers; float operations keep their C
// spelling. Increment and decrement take the address of the target so the
// target is evaluated exactly once, and the helper returns the old value for
// postfix and the new value for prefix.
func (g *generator) unaryExpression(expression *parser.UnaryExpr) (string, error) {
	if expression == nil || expression.Operand == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete unary expression"}
	}
	operand, err := g.expression(expression.Operand)
	if err != nil {
		return "", err
	}
	operandType, err := g.expressionType(expression.Operand)
	if err != nil {
		return "", err
	}
	if operandType == checker.IntType {
		switch expression.Operator {
		case "-":
			return fmt.Sprintf("%s(%s)", runtime.I64NegFunction, operand), nil
		case "~":
			return fmt.Sprintf("(~%s)", operand), nil
		case "++":
			if expression.Postfix {
				return fmt.Sprintf("%s(&(%s))", runtime.I64PostIncFunction, operand), nil
			}
			return fmt.Sprintf("%s(&(%s))", runtime.I64PreIncFunction, operand), nil
		case "--":
			if expression.Postfix {
				return fmt.Sprintf("%s(&(%s))", runtime.I64PostDecFunction, operand), nil
			}
			return fmt.Sprintf("%s(&(%s))", runtime.I64PreDecFunction, operand), nil
		}
	}
	if expression.Postfix {
		return fmt.Sprintf("(%s%s)", operand, expression.Operator), nil
	}
	return fmt.Sprintf("(%s%s)", expression.Operator, operand), nil
}

// binaryExpression renders a binary operation. Kitten and C share operator
// symbols and precedence is made explicit with parentheses, so most operators
// map directly. String operations use runtime helpers: "+" concatenates, and
// "==" / "!=" compare content rather than pointers.
func (g *generator) binaryExpression(expression *parser.BinaryExpr) (string, error) {
	if expression == nil || expression.Left == nil || expression.Right == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete binary expression"}
	}
	left, err := g.expression(expression.Left)
	if err != nil {
		return "", err
	}
	right, err := g.expression(expression.Right)
	if err != nil {
		return "", err
	}

	switch expression.Operator {
	case "+", "==", "!=":
		operandType, err := g.expressionType(expression.Left)
		if err != nil {
			return "", err
		}
		if operandType == checker.StringType {
			switch expression.Operator {
			case "+":
				return fmt.Sprintf("%s(%s, %s)", runtime.StringConcatFunction, left, right), nil
			case "==":
				return fmt.Sprintf("%s(%s, %s)", runtime.StringEqualFunction, left, right), nil
			default: // "!="
				return fmt.Sprintf("!%s(%s, %s)", runtime.StringEqualFunction, left, right), nil
			}
		}
	}

	operandType, err := g.expressionType(expression)
	if err != nil {
		return "", err
	}
	if operandType == checker.IntType {
		switch expression.Operator {
		case "+":
			return fmt.Sprintf("%s(%s, %s)", runtime.I64AddFunction, left, right), nil
		case "-":
			return fmt.Sprintf("%s(%s, %s)", runtime.I64SubFunction, left, right), nil
		case "*":
			return fmt.Sprintf("%s(%s, %s)", runtime.I64MulFunction, left, right), nil
		case "/":
			return fmt.Sprintf("%s(%s, %s)", runtime.I64DivFunction, left, right), nil
		case "%":
			return fmt.Sprintf("%s(%s, %s)", runtime.I64ModFunction, left, right), nil
		case "&":
			return fmt.Sprintf("(%s & %s)", left, right), nil
		case "|":
			return fmt.Sprintf("(%s | %s)", left, right), nil
		case "^":
			return fmt.Sprintf("(%s ^ %s)", left, right), nil
		case "<<":
			return fmt.Sprintf("%s(%s, %s)", runtime.I64ShlFunction, left, right), nil
		case ">>":
			return fmt.Sprintf("%s(%s, %s)", runtime.I64ShrFunction, left, right), nil
		}
	}

	return fmt.Sprintf("(%s %s %s)", left, expression.Operator, right), nil
}

// assignExpression renders an assignment. Kitten and C agree on "=" and "-=",
// and both yield the assigned value.
//
// String "+=" has no C equivalent, so it is lowered to a runtime append that
// receives the address of the target slot. Taking the address evaluates the
// target once, so an index target such as "a[i] += value" evaluates i once.
func (g *generator) assignExpression(expression *parser.AssignExpr) (string, error) {
	if expression == nil || expression.Target == nil || expression.Value == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete assignment"}
	}

	// A plain assignment to a map entry inserts or updates it. It must not go
	// through the reading accessor, which fails when the key is absent.
	if expression.Operator == "=" {
		if index, ok := expression.Target.(*parser.IndexExpr); ok && index.Target != nil && index.Index != nil {
			mapType, err := g.expressionType(index.Target)
			if err != nil {
				return "", err
			}
			if mapType.IsMap() {
				valueCType, err := cType(mapType.Value)
				if err != nil {
					return "", &Error{Line: expression.OperatorAt.Line, Column: expression.OperatorAt.Column, Message: err.Error()}
				}
				mapExpression, err := g.expression(index.Target)
				if err != nil {
					return "", err
				}
				keyExpression, err := g.expression(index.Index)
				if err != nil {
					return "", err
				}
				valueExpression, err := g.expression(expression.Value)
				if err != nil {
					return "", err
				}
				// The runtime copies the value bytes, so the value is passed by
				// address through a compound literal.
				return fmt.Sprintf("%s(%s, %s, &(%s){ %s })", runtime.MapSetFunction, mapExpression, keyExpression, valueCType, valueExpression), nil
			}
		}
	}

	target, err := g.expression(expression.Target)
	if err != nil {
		return "", err
	}
	value, err := g.expression(expression.Value)
	if err != nil {
		return "", err
	}

	if expression.Operator == "+=" || expression.Operator == "-=" {
		targetType, err := g.expressionType(expression.Target)
		if err != nil {
			return "", err
		}
		if targetType == checker.StringType && expression.Operator == "+=" {
			return fmt.Sprintf("%s(&(%s), %s)", runtime.StringAppendFunction, target, value), nil
		}
		if targetType == checker.IntType {
			helper := runtime.I64AddAssignFunction
			if expression.Operator == "-=" {
				helper = runtime.I64SubAssignFunction
			}
			return fmt.Sprintf("%s(&(%s), %s)", helper, target, value), nil
		}
	}

	return fmt.Sprintf("(%s %s %s)", target, expression.Operator, value), nil
}

// memberExpression renders struct field access, or a qualified variant
// reference ("Enum.Case" or "Union.Variant") when the checker recorded one.
func (g *generator) memberExpression(expression *parser.MemberExpr) (string, error) {
	if expression == nil || expression.Receiver == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete member expression"}
	}

	if variant, ok := g.checked.VariantRefs[expression]; ok {
		namedType, err := g.expressionType(expression)
		if err != nil {
			return "", err
		}
		return g.variantReference(namedType, variant)
	}

	receiver, err := g.expression(expression.Receiver)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("(%s)->%s", receiver, fieldName(expression.Member)), nil
}

// variantReference renders a payload-less variant used as a value: an enum case
// is a tag constant, and a payload-less union variant is a constructor call.
func (g *generator) variantReference(namedType *checker.Type, variant *checker.Variant) (string, error) {
	if namedType == nil || variant == nil {
		return "", &Error{Line: 1, Column: 1, Message: "variant reference was not resolved"}
	}
	if namedType.IsUnion() {
		return fmt.Sprintf("%s()", variantConstructorName(namedType, variant.Name)), nil
	}
	return enumCaseName(namedType, variant.Name), nil
}

// variantLiteral renders "Union.Variant { field = value, ... }" as a call to the
// variant's constructor, with values reordered into field declaration order.
func (g *generator) variantLiteral(expression *parser.VariantLiteral) (string, error) {
	if expression == nil || expression.Type == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete variant literal"}
	}
	unionType, err := g.declaredType(expression.Type)
	if err != nil {
		return "", err
	}
	if !unionType.IsUnion() {
		return "", &Error{Line: expression.At.Line, Column: expression.At.Column, Message: fmt.Sprintf("%s is not a union type", unionType)}
	}
	variant := unionType.VariantIndex[expression.Variant]
	if variant == nil {
		return "", &Error{Line: expression.At.Line, Column: expression.At.Column, Message: fmt.Sprintf("union %s has no variant %q", unionType.Name, expression.Variant)}
	}

	values := make(map[string]parser.Expr, len(expression.Fields))
	for _, field := range expression.Fields {
		if field == nil {
			return "", &Error{Line: expression.At.Line, Column: expression.At.Column, Message: "variant literal contains a nil field"}
		}
		values[field.Name] = field.Value
	}

	arguments := make([]string, 0, len(variant.Fields))
	for _, field := range variant.Fields {
		value, ok := values[field.Name]
		if !ok {
			return "", &Error{Line: expression.OpenBrace.Line, Column: expression.OpenBrace.Column, Message: fmt.Sprintf("variant literal for %s.%s is missing field %q", unionType.Name, variant.Name, field.Name)}
		}
		rendered, err := g.expression(value)
		if err != nil {
			return "", err
		}
		arguments = append(arguments, rendered)
	}
	return fmt.Sprintf("%s(%s)", variantConstructorName(unionType, variant.Name), strings.Join(arguments, ", ")), nil
}

// structLiteral renders a struct literal as a call to the struct's constructor,
// with values reordered into field declaration order.
func (g *generator) structLiteral(expression *parser.StructLiteral) (string, error) {
	if expression == nil || expression.Type == nil {
		return "", &Error{Line: 1, Column: 1, Message: "struct literal is missing its type"}
	}
	structType, ok := g.checked.TypeRefs[expression.Type]
	if !ok || structType == nil || structType.Struct == nil {
		return "", &Error{Line: expression.Type.At.Line, Column: expression.Type.At.Column, Message: fmt.Sprintf("struct %q was not checked", expression.Type.Name)}
	}

	values := make(map[string]parser.Expr, len(expression.Fields))
	for _, field := range expression.Fields {
		if field == nil {
			return "", &Error{Line: expression.Type.At.Line, Column: expression.Type.At.Column, Message: "struct literal contains a nil field"}
		}
		values[field.Name] = field.Value
	}

	arguments := make([]string, 0, len(structType.Struct.Fields))
	for _, field := range structType.Struct.Fields {
		value, ok := values[field.Name]
		if !ok {
			return "", &Error{Line: expression.OpenBrace.Line, Column: expression.OpenBrace.Column, Message: fmt.Sprintf("struct literal for %s is missing field %q", structType.Name, field.Name)}
		}
		rendered, err := g.expression(value)
		if err != nil {
			return "", err
		}
		arguments = append(arguments, rendered)
	}
	return fmt.Sprintf("%s(%s)", constructorName(structType.Name, structType.Module), strings.Join(arguments, ", ")), nil
}

// intrinsicCall renders a runtime-module intrinsic as one runtime helper call.
func (g *generator) intrinsicCall(expression *parser.CallExpr, function string, expected int) (string, error) {
	arguments := make([]string, 0, expected)
	for _, argument := range expression.Arguments {
		rendered, err := g.expression(argument)
		if err != nil {
			return "", err
		}
		arguments = append(arguments, rendered)
	}
	if len(arguments) != expected {
		return "", &Error{Line: expression.OpenParen.Line, Column: expression.OpenParen.Column, Message: fmt.Sprintf("intrinsic %s expects %d argument(s), got %d", function, expected, len(arguments))}
	}
	return fmt.Sprintf("%s(%s)", function, strings.Join(arguments, ", ")), nil
}

// addressOfExpression lowers the overloaded address_of intrinsic.
func (g *generator) addressOfExpression(expression *parser.CallExpr) (string, error) {
	if len(expression.Arguments) != 1 {
		return "", &Error{Line: expression.OpenParen.Line, Column: expression.OpenParen.Column, Message: "address_of expects one argument"}
	}
	argumentType, err := g.expressionType(expression.Arguments[0])
	if err != nil {
		return "", err
	}
	rendered, err := g.expression(expression.Arguments[0])
	if err != nil {
		return "", err
	}
	switch {
	case argumentType == checker.StringType:
		return fmt.Sprintf("((int64_t)(uintptr_t)(((const ktn_string *)(%s))->data))", rendered), nil
	case argumentType.Kind == checker.KindArray && argumentType.Element == checker.ByteType:
		return fmt.Sprintf("((int64_t)(uintptr_t)(((ktn_array *)(%s))->data))", rendered), nil
	default:
		return "", &Error{Line: expression.Arguments[0].Pos().Line, Column: expression.Arguments[0].Pos().Column, Message: fmt.Sprintf("address_of does not accept %s", argumentType)}
	}
}

// preludeCall lowers a file or directory built-in call to its generated prelude
// helper. The checker guarantees the argument count and types; this reports a
// malformed tree rather than emitting a partial call.
func (g *generator) preludeCall(expression *parser.CallExpr, builtin string, expected int) (string, error) {
	if len(expression.Arguments) < expected {
		return "", &Error{Line: expression.OpenParen.Line, Column: expression.OpenParen.Column, Message: builtin + " is missing an argument"}
	}
	resultType, err := g.expressionType(expression)
	if err != nil {
		return "", err
	}
	arguments := make([]string, 0, expected)
	for _, argument := range expression.Arguments {
		rendered, err := g.expression(argument)
		if err != nil {
			return "", err
		}
		arguments = append(arguments, rendered)
	}
	return fmt.Sprintf("%s(%s)", preludeHelperName(resultType.Module, builtin), strings.Join(arguments, ", ")), nil
}

// callExpression renders a function call. The built-in print is expanded into a
// comma expression of runtime helpers, the built-in length reads the array
// header, and user functions become plain C calls.
func (g *generator) callExpression(expression *parser.CallExpr) (string, error) {
	if expression == nil || expression.Callee == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete call expression"}
	}

	switch g.builtinKind(expression.Callee) {
	case checker.BuiltinPrint:
		return g.printExpression(expression)
	case checker.BuiltinEprint:
		return g.eprintExpression(expression)
	case checker.BuiltinExit:
		argument, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", runtime.ExitFunction, argument), nil
	case checker.BuiltinLength:
		argumentType, err := g.expressionType(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		lengthFunction := runtime.ArrayLengthFunction
		switch {
		case argumentType.IsMap():
			lengthFunction = runtime.MapLengthFunction
		case argumentType == checker.StringType:
			lengthFunction = runtime.StringLengthFunction
		}
		argument, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", lengthFunction, argument), nil
	case checker.BuiltinSubstring:
		arguments := make([]string, 0, 3)
		for _, argumentExpression := range expression.Arguments {
			rendered, err := g.expression(argumentExpression)
			if err != nil {
				return "", err
			}
			arguments = append(arguments, rendered)
		}
		return fmt.Sprintf("%s(%s)", runtime.SubstringFunction, strings.Join(arguments, ", ")), nil
	case checker.BuiltinMapHas:
		mapExpression, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		keyExpression, err := g.expression(expression.Arguments[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s, %s)", runtime.MapHasFunction, mapExpression, keyExpression), nil
	case checker.BuiltinReadFile:
		argument, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", runtime.ReadFileFunction, argument), nil
	case checker.BuiltinArgv:
		return fmt.Sprintf("%s()", runtime.ArgvFunction), nil
	case checker.BuiltinTryReadFile:
		if len(expression.Arguments) < 1 {
			position := expression.OpenParen
			return "", &Error{Line: position.Line, Column: position.Column, Message: "try_read_file is missing its path argument"}
		}
		resultType, err := g.expressionType(expression)
		if err != nil {
			return "", err
		}
		argument, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", preludeTryReadFileHelperName(resultType.Module), argument), nil
	case checker.BuiltinTryWriteFile:
		return g.preludeCall(expression, "try_write_file", 2)
	case checker.BuiltinTryWriteBytes:
		return g.preludeCall(expression, "try_write_bytes", 2)
	case checker.BuiltinTryCreateDirectory:
		return g.preludeCall(expression, "try_create_directory", 1)
	case checker.BuiltinTryRemove:
		return g.preludeCall(expression, "try_remove", 1)
	case checker.BuiltinTryListDirectory:
		return g.preludeCall(expression, "try_list_directory", 1)
	case checker.BuiltinTryRun:
		return g.preludeCall(expression, "try_run", 2)
	case checker.BuiltinTryCurrentDirectory:
		return g.preludeCall(expression, "try_current_directory", 0)
	case checker.BuiltinRawLoad8:
		return g.intrinsicCall(expression, runtime.RawLoad8Function, 1)
	case checker.BuiltinRawStore8:
		return g.intrinsicCall(expression, runtime.RawStore8Function, 2)
	case checker.BuiltinRawLoad64:
		return g.intrinsicCall(expression, runtime.RawLoad64Function, 1)
	case checker.BuiltinRawStore64:
		return g.intrinsicCall(expression, runtime.RawStore64Function, 2)
	case checker.BuiltinRawCopy:
		return g.intrinsicCall(expression, runtime.RawCopyFunction, 3)
	case checker.BuiltinRawZero:
		return g.intrinsicCall(expression, runtime.RawZeroFunction, 2)
	case checker.BuiltinAddressOf:
		return g.addressOfExpression(expression)
	case checker.BuiltinSyscall:
		return g.intrinsicCall(expression, runtime.SyscallFunction, 7)
	case checker.BuiltinIntFromByte:
		argument, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("((int64_t)(%s))", argument), nil
	case checker.BuiltinByteFromInt:
		argument, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("((uint8_t)((uint64_t)(%s) & UINT64_C(0xFF)))", argument), nil
	case checker.BuiltinStringToBytes:
		argument, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", runtime.StringToBytesFunction, argument), nil
	case checker.BuiltinBytesToString:
		argument, err := g.expression(expression.Arguments[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", runtime.BytesToStringFunction, argument), nil
	}

	identifier, ok := expression.Callee.(*parser.Identifier)
	if !ok {
		position := expression.Callee.Pos()
		return "", &Error{Line: position.Line, Column: position.Column, Message: fmt.Sprintf("unsupported call target %T", expression.Callee)}
	}

	arguments := make([]string, 0, len(expression.Arguments))
	for _, argument := range expression.Arguments {
		rendered, err := g.expression(argument)
		if err != nil {
			return "", err
		}
		arguments = append(arguments, rendered)
	}

	function := g.calleeFunction(identifier)
	if function == nil {
		return "", &Error{Line: identifier.At.Line, Column: identifier.At.Column, Message: fmt.Sprintf("function %q was not resolved", identifier.Name)}
	}
	return fmt.Sprintf("%s(%s)", functionName(identifier.Name, function.Module), strings.Join(arguments, ", ")), nil
}

// calleeFunction returns the resolved user function for a call's callee
// identifier, or nil when the callee is a built-in or was not resolved.
func (g *generator) calleeFunction(identifier *parser.Identifier) *checker.Function {
	symbol := g.checked.Symbols[identifier]
	if symbol == nil || symbol.Function == nil || symbol.Function.Builtin != checker.NotBuiltin {
		return nil
	}
	return symbol.Function
}

// builtinKind reports which built-in function a callee resolves to, or
// checker.NotBuiltin when the callee is not a built-in.
func (g *generator) builtinKind(callee parser.Expr) checker.BuiltinKind {
	identifier, ok := callee.(*parser.Identifier)
	if !ok {
		return checker.NotBuiltin
	}
	symbol := g.checked.Symbols[identifier]
	if symbol == nil || symbol.Function == nil {
		return checker.NotBuiltin
	}
	return symbol.Function.Builtin
}

// newArrayExpression renders "new Element[length]" as a runtime allocation.
// The allocation is zero-initialized by the collector.
func (g *generator) newArrayExpression(expression *parser.NewArrayExpr) (string, error) {
	if expression == nil || expression.Element == nil || expression.Length == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete array creation"}
	}
	elementType, err := g.declaredType(expression.Element)
	if err != nil {
		return "", err
	}
	elementCType, err := cType(elementType)
	if err != nil {
		return "", &Error{Line: expression.At.Line, Column: expression.At.Column, Message: err.Error()}
	}
	length, err := g.expression(expression.Length)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s(%s, sizeof(%s))", runtime.ArrayNewFunction, length, elementCType), nil
}

// newMapExpression renders "new map[Key]Value" as a runtime allocation of an
// empty map whose values are stored inline at the value type's size.
func (g *generator) newMapExpression(expression *parser.NewMapExpr) (string, error) {
	if expression == nil || expression.Map == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete map creation"}
	}
	mapType, err := g.declaredType(expression.Map)
	if err != nil {
		return "", err
	}
	if !mapType.IsMap() {
		return "", &Error{Line: expression.At.Line, Column: expression.At.Column, Message: fmt.Sprintf("%s is not a map type", mapType)}
	}
	valueCType, err := cType(mapType.Value)
	if err != nil {
		return "", &Error{Line: expression.At.Line, Column: expression.At.Column, Message: err.Error()}
	}
	return fmt.Sprintf("%s(sizeof(%s))", runtime.MapNewFunction, valueCType), nil
}

// indexExpression renders "target[index]".
//
// For an array, the runtime accessor performs the bounds check and returns a
// pointer to the element. For a map, it returns a pointer to the value and fails
// when the key is absent. In both cases the result is a C l-value, so compound
// assignment and increment evaluate the target and index exactly once.
func (g *generator) indexExpression(expression *parser.IndexExpr) (string, error) {
	if expression == nil || expression.Target == nil || expression.Index == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete index expression"}
	}
	targetType, err := g.expressionType(expression.Target)
	if err != nil {
		return "", err
	}
	resultType, err := g.expressionType(expression)
	if err != nil {
		return "", err
	}
	resultCType, err := cType(resultType)
	if err != nil {
		return "", &Error{Line: expression.At.Line, Column: expression.At.Column, Message: err.Error()}
	}
	target, err := g.expression(expression.Target)
	if err != nil {
		return "", err
	}
	index, err := g.expression(expression.Index)
	if err != nil {
		return "", err
	}

	// String indexing yields an int byte value directly and is not an l-value.
	if targetType == checker.StringType {
		return fmt.Sprintf("%s(%s, %s)", runtime.StringAtFunction, target, index), nil
	}

	accessor := runtime.ArrayAtFunction
	if targetType.IsMap() {
		accessor = runtime.MapAtFunction
	}
	return fmt.Sprintf("(*((%s *)%s(%s, %s)))", resultCType, accessor, target, index), nil
}

// conditionExpression renders an expression for use as the condition of an if,
// while, or for.
//
// Compound expressions are already emitted fully parenthesized, so adding
// another pair of parentheses around them is redundant; Clang in particular
// warns about a redundant parenthesized equality comparison in an if condition.
// An assignment keeps its own extra layer, because C's -Wparentheses warns when
// an assignment is used as a condition without its own parentheses. A condition
// that is not already parenthesized, such as a bare identifier or a call, is
// wrapped so that it is parenthesized.
func (g *generator) conditionExpression(expression parser.Expr) (string, error) {
	rendered, err := g.expression(expression)
	if err != nil {
		return "", err
	}
	if _, isAssignment := expression.(*parser.AssignExpr); isAssignment {
		return "(" + rendered + ")", nil
	}
	if len(rendered) >= 2 && rendered[0] == '(' && rendered[len(rendered)-1] == ')' {
		return rendered, nil
	}
	return "(" + rendered + ")", nil
}

// emitMatch writes a match statement.
//
// The scrutinee is evaluated once into a temporary. For a union, the temporary
// is checked for null before its tag is read, so matching a null reference is a
// deterministic runtime error rather than undefined behavior. Each arm becomes a
// switch case that binds the variant's fields to locals of the field types; the
// checker has already proved the arms are exhaustive and the bindings match.
func (g *generator) emitMatch(statement *parser.MatchStmt) error {
	if statement == nil || statement.Scrutinee == nil {
		return &Error{Line: 1, Column: 1, Message: "incomplete match statement"}
	}
	scrutineeType, err := g.expressionType(statement.Scrutinee)
	if err != nil {
		return err
	}
	if !scrutineeType.IsVariant() {
		return &Error{Line: statement.At.Line, Column: statement.At.Column, Message: fmt.Sprintf("match requires an enum or union value, found %s", scrutineeType)}
	}

	cScrutinee, err := cType(scrutineeType)
	if err != nil {
		return &Error{Line: statement.At.Line, Column: statement.At.Column, Message: err.Error()}
	}
	scrutinee, err := g.expression(statement.Scrutinee)
	if err != nil {
		return err
	}

	temporary := g.newTemp()
	g.line("%s %s = %s;", cScrutinee, temporary, scrutinee)

	if scrutineeType.IsUnion() {
		g.line("if (%s == NULL) { %s(\"match on a null union reference\"); }", temporary, runtime.FailFunction)
	}

	g.writeIndent()
	if scrutineeType.IsUnion() {
		g.write(fmt.Sprintf("switch (%s->%s) {\n", temporary, tagFieldName))
	} else {
		g.write(fmt.Sprintf("switch (%s) {\n", temporary))
	}
	g.indent++

	for _, arm := range statement.Arms {
		variant := scrutineeType.VariantIndex[arm.Variant]
		if variant == nil {
			return &Error{Line: arm.At.Line, Column: arm.At.Column, Message: fmt.Sprintf("variant %q was not resolved", arm.Variant)}
		}

		g.writeIndent()
		if scrutineeType.IsUnion() {
			g.write(fmt.Sprintf("case %s: {\n", unionTagName(scrutineeType, variant.Name)))
		} else {
			g.write(fmt.Sprintf("case %s: {\n", enumCaseName(scrutineeType, variant.Name)))
		}
		g.indent++

		for bindingIndex, binding := range arm.Bindings {
			field := variant.Fields[bindingIndex]
			fieldCType, err := cType(field.Type)
			if err != nil {
				return &Error{Line: arm.At.Line, Column: arm.At.Column, Message: err.Error()}
			}
			g.line("%s %s = %s->%s;", fieldCType, variableName(binding), temporary, variantFieldName(variant.Name, field.Name))
			// The arm body may not use every binding; a void cast keeps an
			// unused binding from warning without changing behavior.
			g.line("(void)%s;", variableName(binding))
		}

		for _, bodyStatement := range arm.Body.Statements {
			if err := g.emitStatement(bodyStatement); err != nil {
				return err
			}
		}
		g.line("break;")
		g.indent--
		g.line("}")
	}

	// The checker proves the arms are exhaustive, so this default is defensive:
	// an out-of-range tag fails deterministically instead of doing nothing.
	g.indent--
	g.line("default:")
	g.indent++
	g.line("%s(\"match on an invalid variant tag\");", runtime.FailFunction)
	g.indent--
	g.line("}")
	return nil
}

// printExpression expands print(a, b, ...) into a comma expression that writes
// each argument, separated by single spaces and terminated by a newline. A comma
// expression has type void, matching print's return type, so it can appear
// wherever the checker allowed the call.
func (g *generator) printExpression(expression *parser.CallExpr) (string, error) {
	parts := make([]string, 0, 2*len(expression.Arguments)+1)
	for index, argument := range expression.Arguments {
		argumentType, err := g.expressionType(argument)
		if err != nil {
			return "", err
		}
		helper, err := printHelper(argumentType)
		if err != nil {
			position := argument.Pos()
			return "", &Error{Line: position.Line, Column: position.Column, Message: err.Error()}
		}
		rendered, err := g.expression(argument)
		if err != nil {
			return "", err
		}
		if index > 0 {
			parts = append(parts, runtime.PrintSpaceFunction+"()")
		}
		parts = append(parts, helper+"("+rendered+")")
	}
	parts = append(parts, runtime.PrintNewlineFunction+"()")
	return "(" + strings.Join(parts, ", ") + ")", nil
}

// printHelper returns the runtime function that prints a value of the given
// type. The checker only allows int, float, bool, and string.
func printHelper(kittenType *checker.Type) (string, error) {
	switch kittenType {
	case checker.IntType:
		return runtime.PrintIntFunction, nil
	case checker.FloatType:
		return runtime.PrintFloatFunction, nil
	case checker.BoolType:
		return runtime.PrintBoolFunction, nil
	case checker.StringType:
		return runtime.PrintStringFunction, nil
	case checker.ByteType:
		return runtime.PrintIntFunction, nil
	default:
		return "", fmt.Errorf("print does not support values of type %s", kittenType)
	}
}

// eprintExpression expands an eprint call into a comma expression that writes
// each argument to standard error, separated by single spaces and terminated by
// a flushing newline.
func (g *generator) eprintExpression(expression *parser.CallExpr) (string, error) {
	parts := make([]string, 0, 2*len(expression.Arguments)+1)
	for index, argument := range expression.Arguments {
		argumentType, err := g.expressionType(argument)
		if err != nil {
			return "", err
		}
		helper, err := eprintHelper(argumentType)
		if err != nil {
			position := argument.Pos()
			return "", &Error{Line: position.Line, Column: position.Column, Message: err.Error()}
		}
		rendered, err := g.expression(argument)
		if err != nil {
			return "", err
		}
		if index > 0 {
			parts = append(parts, runtime.EprintSpaceFunction+"()")
		}
		parts = append(parts, helper+"("+rendered+")")
	}
	parts = append(parts, runtime.EprintNewlineFunction+"()")
	return "(" + strings.Join(parts, ", ") + ")", nil
}

// eprintHelper returns the runtime function that writes a value to standard
// error. The checker only allows int, float, bool, and string.
func eprintHelper(kittenType *checker.Type) (string, error) {
	switch kittenType {
	case checker.IntType:
		return runtime.EprintIntFunction, nil
	case checker.FloatType:
		return runtime.EprintFloatFunction, nil
	case checker.BoolType:
		return runtime.EprintBoolFunction, nil
	case checker.StringType:
		return runtime.EprintStringFunction, nil
	case checker.ByteType:
		return runtime.EprintIntFunction, nil
	default:
		return "", fmt.Errorf("eprint does not support values of type %s", kittenType)
	}
}
