package codegen

import (
	"fmt"
	"strconv"

	"kitten/checker"
	"kitten/parser"
)

// Generated C identifiers use reserved prefixes so that arbitrary Kitten
// identifiers can never collide with C keywords, with runtime and entry-point
// names, or with each other. Kitten identifiers are ASCII letters, digits, and
// underscores, so prefixing always yields a valid C identifier.
const (
	functionPrefix     = "ktn_fn_"
	variablePrefix     = "ktn_var_"
	structPrefix       = "ktn_s_"
	fieldPrefix        = "ktn_f_"
	constructorPrefix  = "ktn_new_"
	argumentPrefix     = "ktn_arg_"
	enumPrefix         = "ktn_e_"
	enumCasePrefix     = "ktn_ec_"
	unionTagPrefix     = "ktn_uc_"
	variantFieldPrefix = "ktn_v_"
	matchPrefix        = "ktn_match_"
	literalPrefix      = "ktn_literal_"

	// objectName is the local used inside generated struct constructors.
	objectName = "ktn_object"

	// tagFieldName is the C member holding a union value's active variant tag.
	tagFieldName = "ktn_tag"
)

// modulePrefix returns the C name prefix for a module. The root module (ID 0)
// has no prefix, so single-module programs generate the same names as before.
// Imported modules use "<id>_"; because a Kitten identifier cannot begin with a
// digit, a mangled imported name can never collide with a root-module name, and
// distinct IDs keep imported modules distinct.
func modulePrefix(module int) string {
	if module <= 0 {
		return ""
	}
	return strconv.Itoa(module) + "_"
}

// functionName maps a Kitten function name in a module to its C identifier.
func functionName(name string, module int) string {
	return functionPrefix + modulePrefix(module) + name
}

// variableName maps a Kitten variable or parameter name to its C identifier.
// Local names are not module-scoped, so they are not mangled.
func variableName(name string) string { return variablePrefix + name }

// structName maps a Kitten struct name in a module to its C struct type name.
func structName(name string, module int) string {
	return structPrefix + modulePrefix(module) + name
}

// fieldName maps a Kitten struct field name to its C member name.
func fieldName(name string) string { return fieldPrefix + name }

// constructorName maps a Kitten struct name in a module to the name of the
// function that allocates and initializes one instance.
func constructorName(name string, module int) string {
	return constructorPrefix + modulePrefix(module) + name
}

// argumentName maps a struct field name to the parameter name used in the
// generated constructor.
func argumentName(name string) string { return argumentPrefix + name }

// enumName maps a Kitten enum type to its C typedef name.
func enumName(name string, module int) string {
	return enumPrefix + modulePrefix(module) + name
}

// enumCaseName maps an enum case to its C constant name.
func enumCaseName(enumType *checker.Type, caseName string) string {
	return enumCasePrefix + modulePrefix(enumType.Module) + enumType.Name + "_" + caseName
}

// unionTagName maps a union variant to its C tag constant name.
func unionTagName(unionType *checker.Type, variantName string) string {
	return unionTagPrefix + modulePrefix(unionType.Module) + unionType.Name + "_" + variantName
}

// variantFieldName maps a union variant's field to its C member name in the flat
// union layout. The variant name is part of the member so that variants may
// reuse field names.
func variantFieldName(variantName, fieldName string) string {
	return variantFieldPrefix + variantName + "_" + fieldName
}

// variantConstructorName maps a union variant to the function that allocates and
// initializes one instance carrying that variant.
func variantConstructorName(unionType *checker.Type, variantName string) string {
	return constructorPrefix + modulePrefix(unionType.Module) + unionType.Name + "_" + variantName
}

// cType maps a resolved Kitten type to its C representation:
//
//	int    -> int64_t             (signed 64-bit)
//	float  -> double              (IEEE 754 binary64)
//	bool   -> bool                (stdbool.h)
//	string -> const ktn_string *  (immutable, length-carrying byte string)
//	void   -> void                (function return position only)
//	struct -> struct pointer      (GC-managed reference)
//	array  -> ktn_array *         (GC-managed dynamic array)
//
// A Kitten string is a pointer to the runtime's ktn_string, which stores the
// byte length next to the bytes, so length and indexing are constant-time and
// the bytes are still NUL-terminated. The pointer is const-qualified because
// strings are immutable; a static literal is a `static const ktn_string` and a
// constructed string is a GC-managed ktn_string that the runtime fills before
// returning it.
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
		return "const ktn_string *", nil
	case kittenType == checker.VoidType:
		return "void", nil
	case kittenType.Kind == checker.KindArray:
		return "ktn_array *", nil
	case kittenType.Kind == checker.KindMap:
		return "ktn_map *", nil
	case kittenType.Kind == checker.KindEnum:
		return enumName(kittenType.Name, kittenType.Module), nil
	case kittenType.Kind == checker.KindUnion, kittenType.Kind == checker.KindStruct:
		return structName(kittenType.Name, kittenType.Module) + " *", nil
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
