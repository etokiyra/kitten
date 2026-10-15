package checker

import "kitten/parser"

// Kind classifies a semantic type.
type Kind int

const (
	// KindInt is the 64-bit signed integer type.
	KindInt Kind = iota

	// KindFloat is the IEEE 754 binary64 floating-point type.
	KindFloat

	// KindBool is the boolean type.
	KindBool

	// KindString is the string type.
	KindString

	// KindVoid is the type of a function that returns no value. It is never the
	// type of a variable, parameter, or struct field.
	KindVoid

	// KindStruct is a user-defined struct type.
	KindStruct

	// KindArray is a dynamically sized array type. Its element type is Type.Element.
	KindArray

	// KindEnum is a user-defined enum type: a fixed set of payload-less cases.
	KindEnum

	// KindUnion is a user-defined tagged-union type: a set of variants, each of
	// which may carry fields.
	KindUnion
)

// Type is a resolved Kitten type.
//
// Primitive types are the package-level singletons below. Each struct
// declaration produces exactly one *Type that is shared by every reference to
// it. Types are therefore compared with pointer equality, which is exactly the
// "exact type matching, no implicit conversions" rule in the specification.
type Type struct {
	// Kind classifies the type.
	Kind Kind

	// Name is the type's source name: a built-in name such as "int", or a
	// struct name.
	Name string

	// Struct is non-nil when Kind is KindStruct.
	Struct *StructInfo

	// Element is non-nil when Kind is KindArray and holds the element type.
	Element *Type

	// Variants holds an enum's cases or a union's variants, in declaration
	// order. It is empty for other kinds.
	Variants []*Variant

	// VariantIndex maps a variant name to its definition for enum and union
	// types.
	VariantIndex map[string]*Variant

	// Module is the ID of the module that declared this type. It is 0 for
	// primitive and array types, and for a type in the root module.
	Module int
}

// String returns the type's source name.
func (t *Type) String() string {
	if t == nil {
		return "<invalid>"
	}
	return t.Name
}

// IsNumeric reports whether t is int or float.
func (t *Type) IsNumeric() bool {
	return t == IntType || t == FloatType
}

// IsVoid reports whether t is the void type.
func (t *Type) IsVoid() bool {
	return t == VoidType
}

// IsStruct reports whether t is a struct type.
func (t *Type) IsStruct() bool {
	return t != nil && t.Kind == KindStruct
}

// IsArray reports whether t is an array type.
func (t *Type) IsArray() bool {
	return t != nil && t.Kind == KindArray
}

// IsEnum reports whether t is an enum type.
func (t *Type) IsEnum() bool {
	return t != nil && t.Kind == KindEnum
}

// IsUnion reports whether t is a tagged-union type.
func (t *Type) IsUnion() bool {
	return t != nil && t.Kind == KindUnion
}

// IsVariant reports whether t has named variants: an enum or a tagged union.
func (t *Type) IsVariant() bool {
	return t.IsEnum() || t.IsUnion()
}

// The primitive type singletons.
var (
	IntType    = &Type{Kind: KindInt, Name: "int"}
	FloatType  = &Type{Kind: KindFloat, Name: "float"}
	BoolType   = &Type{Kind: KindBool, Name: "bool"}
	StringType = &Type{Kind: KindString, Name: "string"}
	VoidType   = &Type{Kind: KindVoid, Name: "void"}
)

// primitiveTypes maps the built-in type names to their types.
var primitiveTypes = map[string]*Type{
	"int":    IntType,
	"float":  FloatType,
	"bool":   BoolType,
	"string": StringType,
	"void":   VoidType,
}

// StructInfo is the definition of a struct type.
type StructInfo struct {
	// Name is the struct's name.
	Name string

	// Fields are the fields in declaration order. Iterating this slice is
	// deterministic and is used when reporting a missing field.
	Fields []*StructField

	// FieldIndex maps a field name to its definition.
	FieldIndex map[string]*StructField

	// Decl is the syntax node the struct was declared by.
	Decl *parser.StructDecl
}

// StructField is a single struct field.
type StructField struct {
	// Name is the field name.
	Name string

	// Type is the field's resolved type. It is never void.
	Type *Type

	// At is the position of the field declaration.
	At parser.Position
}

// Variant is one case of an enum or one variant of a tagged union.
type Variant struct {
	// Name is the variant name.
	Name string

	// Index is the variant's tag value, assigned in declaration order.
	Index int

	// Fields are the variant's payload fields in declaration order. It is empty
	// for an enum case and for a payload-less union variant.
	Fields []*StructField

	// FieldIndex maps a field name to its definition.
	FieldIndex map[string]*StructField

	// At is the position of the variant name.
	At parser.Position
}

// HasFields reports whether the variant carries a payload.
func (v *Variant) HasFields() bool {
	return len(v.Fields) > 0
}
