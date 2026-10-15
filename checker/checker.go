package checker

import (
	"fmt"

	"kitten/parser"
)

// ErrorKind categorizes a semantic error so that callers can distinguish the
// kind of problem without matching on message text.
type ErrorKind int

const (
	// UndefinedName covers references to names that do not exist: undefined
	// variables, functions, types, struct fields, and struct member access.
	UndefinedName ErrorKind = iota

	// DuplicateDeclaration covers names declared more than once in the same
	// scope: top-level declarations, parameters, fields, locals, and struct
	// literal fields.
	DuplicateDeclaration

	// InvalidOperator covers an operator used with operand types it does not
	// support, such as "%" on floats or "&&" on integers.
	InvalidOperator

	// TypeMismatch covers operands or results whose types do not match exactly:
	// initializers, assignments, arguments, return values, conditions, and
	// struct literal fields.
	TypeMismatch

	// InvalidAssignment covers assignment to a non-assignable target and
	// increment or decrement of a non-assignable or non-numeric target.
	InvalidAssignment

	// BadCall covers calling something that is not a function and calling a
	// function with the wrong number of arguments.
	BadCall

	// InvalidReturn covers returns that conflict with the function's declared
	// return type, and functions that may fall off the end without returning.
	InvalidReturn

	// InvalidType covers misuse of types themselves: void where a value type is
	// required, a struct literal naming a non-struct type, and similar.
	InvalidType

	// InvalidExpression covers expressions used in ways they cannot support,
	// such as using a function or type name as a value.
	InvalidExpression
)

// String returns a stable label for the kind, useful in tests and diagnostics.
func (k ErrorKind) String() string {
	switch k {
	case UndefinedName:
		return "undefined name"
	case DuplicateDeclaration:
		return "duplicate declaration"
	case InvalidOperator:
		return "invalid operator"
	case TypeMismatch:
		return "type mismatch"
	case InvalidAssignment:
		return "invalid assignment"
	case BadCall:
		return "bad call"
	case InvalidReturn:
		return "invalid return"
	case InvalidType:
		return "invalid type"
	case InvalidExpression:
		return "invalid expression"
	default:
		return "error"
	}
}

// Error is a semantic error with a source position.
type Error struct {
	// Kind categorizes the error.
	Kind ErrorKind

	// Line is the one-based line of the offending syntax.
	Line int

	// Column is the one-based column of the offending syntax.
	Column int

	// Message explains the problem.
	Message string
}

// Error implements the error interface, formatting the position as line:column.
func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message)
}

// Result holds the semantic information produced by a successful check. It is
// designed so that a later code generator can consult resolved types and
// symbols without re-checking the program.
type Result struct {
	// Program is the checked program.
	Program *parser.Program

	// StructTypes maps each struct name to its resolved type.
	StructTypes map[string]*Type

	// Functions maps each user-defined function name to its signature.
	Functions map[string]*Function

	// Builtins maps each built-in function name to its signature.
	Builtins map[string]*Function

	// ExpressionTypes maps each checked expression node to its type. Every
	// expression in the program, including sub-expressions, appears here.
	ExpressionTypes map[parser.Expr]*Type

	// TypeRefs maps each declared type reference (a variable's declared type, a
	// parameter type, a return type, or a struct field type) to its resolved
	// type, so later phases can look up declared types without re-resolving
	// names.
	TypeRefs map[*parser.TypeRef]*Type

	// Symbols maps each identifier reference that resolved to a symbol (a
	// variable, parameter, or function) to that symbol.
	Symbols map[*parser.Identifier]*Symbol
}

// checker accumulates state during a single Check call. It holds no package
// level mutable state, so checking one program never affects another.
type checker struct {
	structs   map[string]*Type
	functions map[string]*Function
	builtins  map[string]*Function
	declared  map[string]parser.Position

	result *Result

	// returnType is the return type of the function currently being checked.
	returnType *Type
}

// Check performs semantic analysis and type checking on a parsed program.
//
// It collects top-level declarations first, so functions and structs may refer
// to one another regardless of order, then resolves struct and function
// signatures, then checks every function body.
//
// Check is fail-fast: it reports the first semantic error it finds and returns
// a nil Result. It uses only positions recorded in the AST and never panics,
// including on hand-built or incomplete syntax trees.
func Check(program *parser.Program) (*Result, error) {
	if program == nil {
		return nil, &Error{Kind: InvalidType, Line: 1, Column: 1, Message: "cannot check a nil program"}
	}

	c := &checker{
		structs:   map[string]*Type{},
		functions: map[string]*Function{},
		builtins:  builtinFunctions(),
		declared:  map[string]parser.Position{},
		result: &Result{
			Program:         program,
			StructTypes:     map[string]*Type{},
			Functions:       map[string]*Function{},
			Builtins:        map[string]*Function{},
			ExpressionTypes: map[parser.Expr]*Type{},
			TypeRefs:        map[*parser.TypeRef]*Type{},
			Symbols:         map[*parser.Identifier]*Symbol{},
		},
	}
	for name, function := range c.builtins {
		c.result.Builtins[name] = function
	}

	if err := c.collectDeclarations(); err != nil {
		return nil, err
	}
	if err := c.resolveStructs(); err != nil {
		return nil, err
	}
	if err := c.resolveFunctions(); err != nil {
		return nil, err
	}
	if err := c.checkBodies(); err != nil {
		return nil, err
	}
	return c.result, nil
}

// builtinFunctions returns the language's built-in functions. print is provided
// by the language, not declared by the user.
func builtinFunctions() map[string]*Function {
	return map[string]*Function{
		"print": {
			Name:         "print",
			ReturnType:   VoidType,
			Builtin:      true,
			Variadic:     true,
			AllowedTypes: []*Type{IntType, FloatType, BoolType, StringType},
		},
	}
}

// collectDeclarations records every top-level name and creates placeholder
// types and signatures. It detects duplicate top-level names. Structs and
// functions share one top-level namespace.
func (c *checker) collectDeclarations() error {
	for _, declaration := range c.result.Program.Declarations {
		switch d := declaration.(type) {
		case *parser.StructDecl:
			if d == nil {
				return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "program contains a nil struct declaration")
			}
			if earlier, ok := c.declared[d.Name]; ok {
				return c.errorAt(DuplicateDeclaration, d.At, "duplicate declaration of %q (previously declared at %d:%d)", d.Name, earlier.Line, earlier.Column)
			}
			c.declared[d.Name] = d.At
			structType := &Type{
				Kind:   KindStruct,
				Name:   d.Name,
				Struct: &StructInfo{Name: d.Name, FieldIndex: map[string]*StructField{}, Decl: d},
			}
			c.structs[d.Name] = structType
			c.result.StructTypes[d.Name] = structType

		case *parser.FunctionDecl:
			if d == nil {
				return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "program contains a nil function declaration")
			}
			if earlier, ok := c.declared[d.Name]; ok {
				return c.errorAt(DuplicateDeclaration, d.At, "duplicate declaration of %q (previously declared at %d:%d)", d.Name, earlier.Line, earlier.Column)
			}
			c.declared[d.Name] = d.At
			function := &Function{Name: d.Name, Decl: d}
			c.functions[d.Name] = function
			c.result.Functions[d.Name] = function

		default:
			if declaration == nil {
				return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "program contains a nil declaration")
			}
			return c.errorAt(InvalidType, declaration.Pos(), "unsupported declaration %T", declaration)
		}
	}
	return nil
}

// resolveStructs resolves each struct's field types and detects duplicate
// fields. Field types must exist and must not be void.
func (c *checker) resolveStructs() error {
	for _, declaration := range c.result.Program.Declarations {
		d, ok := declaration.(*parser.StructDecl)
		if !ok || d == nil {
			continue
		}
		info := c.structs[d.Name].Struct
		for _, field := range d.Fields {
			if field == nil {
				return c.errorAt(InvalidType, d.At, "struct %q has a nil field declaration", d.Name)
			}
			fieldType, err := c.resolveType(field.Type)
			if err != nil {
				return err
			}
			if fieldType.IsVoid() {
				return c.errorAt(InvalidType, field.Type.Pos(), "struct field %q cannot have type void", field.Name)
			}
			if earlier, ok := info.FieldIndex[field.Name]; ok {
				return c.errorAt(DuplicateDeclaration, field.At, "duplicate field %q in struct %q (previously declared at %d:%d)", field.Name, d.Name, earlier.At.Line, earlier.At.Column)
			}
			structField := &StructField{Name: field.Name, Type: fieldType, At: field.At}
			info.Fields = append(info.Fields, structField)
			info.FieldIndex[field.Name] = structField
		}
	}
	return nil
}

// resolveFunctions resolves each function's parameter and return types and
// detects duplicate parameters. Parameter types must exist and must not be
// void. A return type may be void, which means the function returns no value.
func (c *checker) resolveFunctions() error {
	for _, declaration := range c.result.Program.Declarations {
		d, ok := declaration.(*parser.FunctionDecl)
		if !ok || d == nil {
			continue
		}
		function := c.functions[d.Name]

		seen := map[string]parser.Position{}
		for _, parameter := range d.Parameters {
			if parameter == nil {
				return c.errorAt(InvalidType, d.At, "function %q has a nil parameter", d.Name)
			}
			parameterType, err := c.resolveType(parameter.Type)
			if err != nil {
				return err
			}
			if parameterType.IsVoid() {
				return c.errorAt(InvalidType, parameter.Type.Pos(), "parameter %q cannot have type void", parameter.Name)
			}
			if earlier, ok := seen[parameter.Name]; ok {
				return c.errorAt(DuplicateDeclaration, parameter.At, "duplicate parameter %q (previously declared at %d:%d)", parameter.Name, earlier.Line, earlier.Column)
			}
			seen[parameter.Name] = parameter.At
			function.ParameterNames = append(function.ParameterNames, parameter.Name)
			function.ParameterTypes = append(function.ParameterTypes, parameterType)
		}

		returnType := VoidType
		if d.ReturnType != nil {
			resolved, err := c.resolveType(d.ReturnType)
			if err != nil {
				return err
			}
			returnType = resolved
		}
		function.ReturnType = returnType
	}
	return nil
}

// checkBodies checks every user-defined function body.
func (c *checker) checkBodies() error {
	for _, declaration := range c.result.Program.Declarations {
		d, ok := declaration.(*parser.FunctionDecl)
		if !ok || d == nil {
			continue
		}
		if err := c.checkFunction(d); err != nil {
			return err
		}
	}
	return nil
}

// checkFunction checks a single function body. Parameters and the body's
// outermost statements share one scope. A non-void function must return a value
// on every path the conservative analysis can prove.
func (c *checker) checkFunction(declaration *parser.FunctionDecl) error {
	function := c.functions[declaration.Name]
	if declaration.Body == nil {
		return c.errorAt(InvalidType, declaration.At, "function %q has no body", declaration.Name)
	}

	scope := newScope(nil)
	for index, name := range function.ParameterNames {
		scope.declare(&Symbol{
			Name: name,
			Kind: SymbolVariable,
			Type: function.ParameterTypes[index],
			At:   declaration.Parameters[index].At,
		})
	}

	previousReturn := c.returnType
	c.returnType = function.ReturnType
	err := c.checkStatements(declaration.Body.Statements, scope)
	c.returnType = previousReturn
	if err != nil {
		return err
	}

	if !function.ReturnType.IsVoid() && !blockReturns(declaration.Body) {
		return c.errorAt(InvalidReturn, declaration.At, "function %q must return a value of type %s on all control-flow paths", declaration.Name, function.ReturnType)
	}
	return nil
}

// resolveType resolves a declared type name to a type. It does not reject void;
// callers decide whether void is acceptable in their context.
func (c *checker) resolveType(reference *parser.TypeRef) (*Type, error) {
	if reference == nil {
		return nil, &Error{Kind: InvalidType, Line: 1, Column: 1, Message: "missing type reference"}
	}

	var resolved *Type
	switch {
	case primitiveTypes[reference.Name] != nil:
		resolved = primitiveTypes[reference.Name]
	case c.structs[reference.Name] != nil:
		resolved = c.structs[reference.Name]
	default:
		return nil, c.errorAt(UndefinedName, reference.Pos(), "undefined type %q", reference.Name)
	}

	c.result.TypeRefs[reference] = resolved
	return resolved, nil
}

// errorAt builds a semantic error positioned at pos.
func (c *checker) errorAt(kind ErrorKind, pos parser.Position, format string, args ...any) error {
	return &Error{
		Kind:    kind,
		Line:    pos.Line,
		Column:  pos.Column,
		Message: fmt.Sprintf(format, args...),
	}
}

// requireSameType reports an error unless actual and expected are the same type.
func (c *checker) requireSameType(kind ErrorKind, actual, expected *Type, pos parser.Position, context string) error {
	if actual == expected {
		return nil
	}
	if actual != nil && actual.IsVoid() {
		return c.errorAt(kind, pos, "%s: a void value cannot be used here", context)
	}
	return c.errorAt(kind, pos, "%s: expected %s, found %s", context, expected, actual)
}
