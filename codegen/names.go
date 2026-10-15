package codegen

import (
	"fmt"

	"kitten/checker"
	"kitten/parser"
)

// Generated C identifiers use reserved prefixes so that arbitrary Kitten
// identifiers can never collide with C keywords, with runtime and entry-point
// names, or with each other. Kitten identifiers are ASCII letters, digits, and
// underscores, so prefixing always yields a valid C identifier.
const (
	functionPrefix    = "ktn_fn_"
	variablePrefix    = "ktn_var_"
	structPrefix      = "ktn_s_"
	fieldPrefix       = "ktn_f_"
	constructorPrefix = "ktn_new_"
	argumentPrefix    = "ktn_arg_"

	// objectName is the local used inside generated struct constructors.
	objectName = "ktn_object"
)

// functionName maps a Kitten function name to its C identifier.
func functionName(name string) string { return functionPrefix + name }

// variableName maps a Kitten variable or parameter name to its C identifier.
func variableName(name string) string { return variablePrefix + name }

// structName maps a Kitten struct name to its C struct type name.
func structName(name string) string { return structPrefix + name }

// fieldName maps a Kitten struct field name to its C member name.
func fieldName(name string) string { return fieldPrefix + name }

// constructorName maps a Kitten struct name to the name of the function that
// allocates and initializes one instance.
func constructorName(name string) string { return constructorPrefix + name }

// argumentName maps a struct field name to the parameter name used in the
// generated constructor.
func argumentName(name string) string { return argumentPrefix + name }

// cType maps a resolved Kitten type to its C representation:
//
//	int    -> int64_t        (signed 64-bit)
//	float  -> double         (IEEE 754 binary64)
//	bool   -> bool           (stdbool.h)
//	string -> const char *   (immutable NUL-terminated byte string)
//	void   -> void           (function return position only)
//	struct -> struct pointer (GC-managed reference)
//	array  -> ktn_array *    (GC-managed dynamic array)
func cType(kittenType *checker.Type) (string, error) {
	switch {
	case kittenType == nil:
		return "", fmt.Errorf("missing type")
	case kittenType == checker.IntType:
		return "int64_t", nil
	case kittenType == checker.FloatType:
		return "double", nil
	case kittenType == checker.BoolType:
		return "bool", nil
	case kittenType == checker.StringType:
		return "const char *", nil
	case kittenType == checker.VoidType:
		return "void", nil
	case kittenType.Kind == checker.KindArray:
		return "ktn_array *", nil
	case kittenType.Kind == checker.KindStruct:
		return structName(kittenType.Name) + " *", nil
	default:
		return "", fmt.Errorf("unsupported type %q", kittenType)
	}
}

// declaredType returns the resolved type of a declared type reference, as
// recorded by the checker. Code generation never re-resolves names.
func (g *generator) declaredType(reference *parser.TypeRef) (*checker.Type, error) {
	if reference == nil {
		return nil, &Error{Line: 1, Column: 1, Message: "missing type reference"}
	}
	resolved, ok := g.checked.TypeRefs[reference]
	if !ok || resolved == nil {
		return nil, &Error{Line: reference.At.Line, Column: reference.At.Column, Message: fmt.Sprintf("unresolved type %q", reference.Name)}
	}
	return resolved, nil
}

// expressionType returns the type the checker recorded for an expression.
func (g *generator) expressionType(expression parser.Expr) (*checker.Type, error) {
	if expression == nil {
		return nil, &Error{Line: 1, Column: 1, Message: "missing expression"}
	}
	resolved, ok := g.checked.ExpressionTypes[expression]
	if !ok || resolved == nil {
		return nil, &Error{Line: expression.Pos().Line, Column: expression.Pos().Column, Message: "expression has no recorded type"}
	}
	return resolved, nil
}
