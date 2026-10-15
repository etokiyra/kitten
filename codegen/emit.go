package codegen

import (
	"fmt"
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
		condition, err := g.expression(s.Condition)
		if err != nil {
			return err
		}
		g.writeIndent()
		g.write("while (" + condition + ") ")
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
	condition, err := g.expression(statement.Condition)
	if err != nil {
		return err
	}
	g.write("if (" + condition + ") ")
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
		expression, err := g.expression(statement.Condition)
		if err != nil {
			return err
		}
		condition = expression
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

// expression renders an expression as a C expression.
func (g *generator) expression(expression parser.Expr) (string, error) {
	switch e := expression.(type) {
	case *parser.IntLiteral:
		return e.Text, nil
	case *parser.FloatLiteral:
		return e.Text, nil
	case *parser.BoolLiteral:
		if e.Value {
			return "true", nil
		}
		return "false", nil
	case *parser.StringLiteral:
		literal, err := cStringLiteral(e.Text)
		if err != nil {
			return "", &Error{Line: e.At.Line, Column: e.At.Column, Message: err.Error()}
		}
		return literal, nil
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
	default:
		if expression == nil {
			return "", &Error{Line: 1, Column: 1, Message: "nil expression"}
		}
		position := expression.Pos()
		return "", &Error{Line: position.Line, Column: position.Column, Message: fmt.Sprintf("unsupported expression %T", expression)}
	}
}

// unaryExpression renders prefix and postfix unary operations. Kitten and C
// agree on the operators, so they map directly.
func (g *generator) unaryExpression(expression *parser.UnaryExpr) (string, error) {
	if expression == nil || expression.Operand == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete unary expression"}
	}
	operand, err := g.expression(expression.Operand)
	if err != nil {
		return "", err
	}
	if expression.Postfix {
		return fmt.Sprintf("(%s%s)", operand, expression.Operator), nil
	}
	return fmt.Sprintf("(%s%s)", expression.Operator, operand), nil
}

// binaryExpression renders a binary operation. Kitten and C share operator
// symbols and precedence is made explicit with parentheses, so most operators
// map directly. String equality uses a runtime helper because C would otherwise
// compare pointers.
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

	if expression.Operator == "==" || expression.Operator == "!=" {
		operandType, err := g.expressionType(expression.Left)
		if err != nil {
			return "", err
		}
		if operandType == checker.StringType {
			comparison := fmt.Sprintf("%s(%s, %s)", runtime.StringEqualFunction, left, right)
			if expression.Operator == "==" {
				return comparison, nil
			}
			return "!" + comparison, nil
		}
	}

	return fmt.Sprintf("(%s %s %s)", left, expression.Operator, right), nil
}

// assignExpression renders an assignment. Kitten and C agree on "=", "+=", and
// "-=", and both yield the assigned value.
func (g *generator) assignExpression(expression *parser.AssignExpr) (string, error) {
	if expression == nil || expression.Target == nil || expression.Value == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete assignment"}
	}
	target, err := g.expression(expression.Target)
	if err != nil {
		return "", err
	}
	value, err := g.expression(expression.Value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("(%s %s %s)", target, expression.Operator, value), nil
}

// memberExpression renders struct field access.
func (g *generator) memberExpression(expression *parser.MemberExpr) (string, error) {
	if expression == nil || expression.Receiver == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete member expression"}
	}
	receiver, err := g.expression(expression.Receiver)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("(%s)->%s", receiver, fieldName(expression.Member)), nil
}

// structLiteral renders a struct literal as a call to the struct's constructor,
// with values reordered into field declaration order.
func (g *generator) structLiteral(expression *parser.StructLiteral) (string, error) {
	if expression == nil || expression.Type == nil {
		return "", &Error{Line: 1, Column: 1, Message: "struct literal is missing its type"}
	}
	structType, ok := g.checked.StructTypes[expression.Type.Name]
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
	return fmt.Sprintf("%s(%s)", constructorName(structType.Name), strings.Join(arguments, ", ")), nil
}

// callExpression renders a function call. The built-in print is expanded into a
// comma expression of runtime helpers; user functions become plain C calls.
func (g *generator) callExpression(expression *parser.CallExpr) (string, error) {
	if expression == nil || expression.Callee == nil {
		return "", &Error{Line: 1, Column: 1, Message: "incomplete call expression"}
	}

	if g.isBuiltinPrintCall(expression) {
		return g.printExpression(expression)
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
	return fmt.Sprintf("%s(%s)", functionName(identifier.Name), strings.Join(arguments, ", ")), nil
}

// isBuiltinPrintCall reports whether a call resolves to the built-in print.
func (g *generator) isBuiltinPrintCall(expression *parser.CallExpr) bool {
	identifier, ok := expression.Callee.(*parser.Identifier)
	if !ok {
		return false
	}
	symbol := g.checked.Symbols[identifier]
	if symbol == nil || symbol.Function == nil {
		return false
	}
	return symbol.Function.Builtin
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
	default:
		return "", fmt.Errorf("print does not support values of type %s", kittenType)
	}
}
