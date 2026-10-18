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

	// PrivateAccess covers referencing a declaration that another module
	// declares, but does not make public.
	PrivateAccess
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
	case PrivateAccess:
		return "private access"
	default:
		return "error"
	}
}

// Error is a semantic error with a source position.
type Error struct {
	// Kind categorizes the error.
	Kind ErrorKind

	// File is the source file the error was found in. It is empty when the
	// program was checked from a source string without a file identity.
	File string

	// Line is the one-based line of the offending syntax.
	Line int

	// Column is the one-based column of the offending syntax.
	Column int

	// Message explains the problem.
	Message string
}

// Error implements the error interface, formatting the position as [file:]line:column.
func (e *Error) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message)
}

// Result holds the semantic information produced by a successful check. It is
// designed so that a later code generator can consult resolved types and
// symbols without re-checking the program.
type Result struct {
	// Modules are the checked modules, with the root module first.
	Modules []*parser.Module

	// Types maps each named type visible to the root module (its own
	// declarations plus all public declarations) to its resolved type. When two
	// modules declare private types with the same name, this map holds only the
	// root module's.
	Types map[string]*Type

	// Functions maps each function name visible to the root module to its
	// signature, with the same caveat as StructTypes.
	Functions map[string]*Function

	// Builtins maps each built-in function name to its signature.
	Builtins map[string]*Function

	// TypeDecls maps each struct, enum, or union declaration to its resolved
	// type, exactly, even when private types in different modules share a name.
	TypeDecls map[parser.Decl]*Type

	// FunctionDecls maps each function declaration to its signature.
	FunctionDecls map[*parser.FunctionDecl]*Function

	// ExpressionTypes maps each checked expression node to its type. Every
	// expression in the program, including sub-expressions, appears here.
	ExpressionTypes map[parser.Expr]*Type

	// TypeRefs maps each declared type reference (a variable's declared type, a
	// parameter type, a return type, or a struct field type) to its resolved
	// type, so later phases can look up declared types without re-resolving
	// names.
	TypeRefs map[*parser.TypeRef]*Type

	// VariantRefs maps each expression that denotes a union or enum variant —
	// a qualified reference "Type.Variant" or a variant literal — to that
	// variant, so code generation can emit the right tag or constructor.
	VariantRefs map[parser.Expr]*Variant

	// Symbols maps each identifier reference that resolved to a symbol (a
	// variable, parameter, or function) to that symbol.
	Symbols map[*parser.Identifier]*Symbol
}

// moduleInfo holds the declarations of one module during checking.
type moduleInfo struct {
	path      string
	id        int
	types     map[string]*Type
	functions map[string]*Function
	declared  map[string]parser.Position
}

// checker accumulates state during a single Check call. It holds no package
// level mutable state, so checking one program never affects another.
type checker struct {
	builtins   map[string]*Function
	arrayTypes map[*Type]*Type
	mapTypes   map[mapTypeIdentity]*Type
	result     *Result

	// modules are the modules being checked and current is the module whose
	// declaration is being processed, used for name resolution and diagnostics.
	modules []*moduleInfo
	current *moduleInfo

	// publicTypes and publicFunctions hold every public declaration of every
	// module, shared across the whole program.
	publicTypes     map[string]*Type
	publicFunctions map[string]*Function

	// returnType is the return type of the function currently being checked.
	returnType *Type
}

// Check performs semantic analysis and type checking on a single parsed program.
// It is the single-module entry point; CheckModules checks a program split
// across several modules.
//
// It collects top-level declarations first, so functions and structs may refer
// to one another regardless of order, then resolves struct and function
// signatures, then checks every function body.
//
// Check is fail-fast: it reports the first semantic error it finds and returns
// a nil Result. It uses only positions recorded in the AST and never panics,
// including on hand-built or incomplete syntax trees.
func Check(program *parser.Program) (*Result, error) {
	return CheckModules([]*parser.Module{{ID: 0, Program: program}})
}

// CheckOptions carries harness-only compiler hooks. It is reachable only from
// Go code; the CLI, the loader, and source syntax cannot set it.
type CheckOptions struct {
	// RuntimeSource, when non-empty, is parsed as the synthetic <runtime>
	// module and appended after all loaded modules and after <prelude>.
	RuntimeSource string
}

// CheckModulesWithRuntime is the harness hook for the synthetic <runtime>
// module. A production caller uses CheckModules, which supplies no runtime
// source and therefore adds no runtime module.
func CheckModulesWithRuntime(modules []*parser.Module, runtimeSource string) (*Result, error) {
	return CheckModulesWithOptions(modules, CheckOptions{RuntimeSource: runtimeSource})
}

// CheckModules performs semantic analysis and type checking on a program that
// may span several modules. The first module is the root module; the others are
// imported.
//
// Every module resolves names against its own declarations first, then against
// the public declarations of all modules, then against the built-ins. A
// declaration is public only when it is marked public, so private declarations
// are never visible to other modules.
func CheckModules(modules []*parser.Module) (*Result, error) {
	return CheckModulesWithOptions(modules, CheckOptions{})
}

// CheckModulesWithOptions is CheckModules plus harness-only synthetic-module
// injection.
func CheckModulesWithOptions(modules []*parser.Module, options CheckOptions) (*Result, error) {
	if len(modules) == 0 {
		return nil, &Error{Kind: InvalidType, Line: 1, Column: 1, Message: "cannot check a program with no modules"}
	}

	preludeUses := scanPreludeUses(modules)
	if preludeUses.any() {
		prelude, err := parsePrelude(preludeSource(preludeUses))
		if err != nil {
			return nil, err
		}
		prelude.ID = len(modules)
		extended := make([]*parser.Module, len(modules)+1)
		copy(extended, modules)
		extended[len(modules)] = prelude
		modules = extended
	}

	if options.RuntimeSource != "" {
		runtimeModule, err := parseSyntheticModule(runtimeModulePath, options.RuntimeSource)
		if err != nil {
			return nil, err
		}
		runtimeModule.ID = len(modules)
		extended := make([]*parser.Module, len(modules)+1)
		copy(extended, modules)
		extended[len(modules)] = runtimeModule
		modules = extended
	}

	c := &checker{
		arrayTypes:      map[*Type]*Type{},
		mapTypes:        map[mapTypeIdentity]*Type{},
		publicTypes:     map[string]*Type{},
		publicFunctions: map[string]*Function{},
		result: &Result{
			Modules:         modules,
			Types:           map[string]*Type{},
			Functions:       map[string]*Function{},
			Builtins:        map[string]*Function{},
			TypeDecls:       map[parser.Decl]*Type{},
			FunctionDecls:   map[*parser.FunctionDecl]*Function{},
			ExpressionTypes: map[parser.Expr]*Type{},
			TypeRefs:        map[*parser.TypeRef]*Type{},
			VariantRefs:     map[parser.Expr]*Variant{},
			Symbols:         map[*parser.Identifier]*Symbol{},
		},
	}
	c.builtins = c.builtinFunctions()

	for _, module := range modules {
		if module == nil || module.Program == nil {
			return nil, &Error{Kind: InvalidType, Line: 1, Column: 1, Message: "module has no parsed program"}
		}
		c.modules = append(c.modules, &moduleInfo{
			path:      module.Path,
			id:        module.ID,
			types:     map[string]*Type{},
			functions: map[string]*Function{},
			declared:  map[string]parser.Position{},
		})
	}

	for name, function := range c.builtins {
		c.result.Builtins[name] = function
	}

	if err := c.collectDeclarations(); err != nil {
		return nil, err
	}
	if preludeUses.any() {
		if err := c.registerPreludeBuiltins(preludeUses); err != nil {
			return nil, err
		}
	}
	if err := c.resolveStructs(); err != nil {
		return nil, err
	}
	if err := c.resolveUnionFields(); err != nil {
		return nil, err
	}
	if err := c.resolveFunctions(); err != nil {
		return nil, err
	}
	if err := c.checkBodies(); err != nil {
		return nil, err
	}
	c.current = nil
	return c.result, nil
}

// builtinFunctions returns the language's built-in functions. They are provided
// by the language, not declared by the user. It is a method so that argv can
// return the checker's interned string-array type.
func (c *checker) builtinFunctions() map[string]*Function {
	return map[string]*Function{
		"print": {
			Name:         "print",
			ReturnType:   VoidType,
			Builtin:      BuiltinPrint,
			Variadic:     true,
			AllowedTypes: []*Type{IntType, FloatType, BoolType, StringType, ByteType},
		},
		"eprint": {
			Name:         "eprint",
			ReturnType:   VoidType,
			Builtin:      BuiltinEprint,
			Variadic:     true,
			AllowedTypes: []*Type{IntType, FloatType, BoolType, StringType, ByteType},
		},
		"exit": {
			Name:           "exit",
			ParameterTypes: []*Type{IntType},
			ReturnType:     VoidType,
			Builtin:        BuiltinExit,
		},
		"length": {
			Name:       "length",
			ReturnType: IntType,
			Builtin:    BuiltinLength,
		},
		"read_file": {
			Name:           "read_file",
			ParameterTypes: []*Type{StringType},
			ReturnType:     StringType,
			Builtin:        BuiltinReadFile,
		},
		"argv": {
			Name:       "argv",
			ReturnType: c.arrayType(StringType),
			Builtin:    BuiltinArgv,
		},
		"has": {
			Name:       "has",
			ReturnType: BoolType,
			Builtin:    BuiltinMapHas,
		},
		"substring": {
			Name:       "substring",
			ReturnType: StringType,
			Builtin:    BuiltinSubstring,
		},
		"int_from_byte": {
			Name:           "int_from_byte",
			ParameterTypes: []*Type{ByteType},
			ReturnType:     IntType,
			Builtin:        BuiltinIntFromByte,
		},
		"byte_from_int": {
			Name:           "byte_from_int",
			ParameterTypes: []*Type{IntType},
			ReturnType:     ByteType,
			Builtin:        BuiltinByteFromInt,
		},
		"string_to_bytes": {
			Name:           "string_to_bytes",
			ParameterTypes: []*Type{StringType},
			ReturnType:     c.arrayType(ByteType),
			Builtin:        BuiltinStringToBytes,
		},
		"bytes_to_string": {
			Name:           "bytes_to_string",
			ParameterTypes: []*Type{c.arrayType(ByteType)},
			ReturnType:     StringType,
			Builtin:        BuiltinBytesToString,
		},
	}
}

// collectDeclarations records every top-level name of every module and creates
// placeholder types and signatures. It detects duplicate names within a module
// and duplicate public names across modules. Within a module, structs and
// functions share one namespace.
func (c *checker) collectDeclarations() error {
	for index, module := range c.modules {
		c.current = module
		for _, declaration := range c.result.Modules[index].Program.Declarations {
			if !isSyntheticModulePath(module.path) {
				if name, at, ok := declarationName(declaration); ok && preludeReservedNames[name] {
					return c.errorAt(DuplicateDeclaration, at, "cannot declare %q: the name is reserved for the predeclared type", name)
				}
			}
			switch d := declaration.(type) {
			case *parser.ImportDecl:
				// Import declarations carry no language declarations; the module
				// loader resolves them before checking.
				continue

			case *parser.StructDecl:
				if d == nil {
					return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "a module contains a nil struct declaration")
				}
				namedType := &Type{
					Kind:   KindStruct,
					Name:   d.Name,
					Module: module.id,
					Struct: &StructInfo{Name: d.Name, FieldIndex: map[string]*StructField{}, Decl: d},
				}
				if err := c.registerType(index, module, d, d.Name, d.At, d.Visibility, namedType); err != nil {
					return err
				}

			case *parser.EnumDecl:
				if d == nil {
					return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "a module contains a nil enum declaration")
				}
				if len(d.Cases) == 0 {
					return c.errorAt(InvalidType, d.At, "enum %q must have at least one case", d.Name)
				}
				namedType := &Type{Kind: KindEnum, Name: d.Name, Module: module.id, VariantIndex: map[string]*Variant{}}
				for variantIndex, enumCase := range d.Cases {
					if enumCase == nil {
						return c.errorAt(InvalidType, d.At, "enum %q has a nil case", d.Name)
					}
					if _, ok := namedType.VariantIndex[enumCase.Name]; ok {
						return c.errorAt(DuplicateDeclaration, enumCase.At, "duplicate case %q in enum %q", enumCase.Name, d.Name)
					}
					variant := &Variant{Name: enumCase.Name, Index: variantIndex, At: enumCase.At}
					namedType.Variants = append(namedType.Variants, variant)
					namedType.VariantIndex[enumCase.Name] = variant
				}
				if err := c.registerType(index, module, d, d.Name, d.At, d.Visibility, namedType); err != nil {
					return err
				}

			case *parser.UnionDecl:
				if d == nil {
					return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "a module contains a nil union declaration")
				}
				if len(d.Variants) == 0 {
					return c.errorAt(InvalidType, d.At, "union %q must have at least one variant", d.Name)
				}
				namedType := &Type{Kind: KindUnion, Name: d.Name, Module: module.id, VariantIndex: map[string]*Variant{}}
				for variantIndex, variantDeclaration := range d.Variants {
					if variantDeclaration == nil {
						return c.errorAt(InvalidType, d.At, "union %q has a nil variant", d.Name)
					}
					if _, ok := namedType.VariantIndex[variantDeclaration.Name]; ok {
						return c.errorAt(DuplicateDeclaration, variantDeclaration.At, "duplicate variant %q in union %q", variantDeclaration.Name, d.Name)
					}
					variant := &Variant{
						Name:       variantDeclaration.Name,
						Index:      variantIndex,
						FieldIndex: map[string]*StructField{},
						At:         variantDeclaration.At,
					}
					namedType.Variants = append(namedType.Variants, variant)
					namedType.VariantIndex[variantDeclaration.Name] = variant
				}
				if err := c.registerType(index, module, d, d.Name, d.At, d.Visibility, namedType); err != nil {
					return err
				}

			case *parser.FunctionDecl:
				if d == nil {
					return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "a module contains a nil function declaration")
				}
				if earlier, ok := module.declared[d.Name]; ok {
					return c.errorAt(DuplicateDeclaration, d.At, "duplicate declaration of %q (previously declared at %d:%d)", d.Name, earlier.Line, earlier.Column)
				}
				module.declared[d.Name] = d.At

				function := &Function{Name: d.Name, Module: module.id, Decl: d}
				module.functions[d.Name] = function
				c.result.FunctionDecls[d] = function

				if d.Visibility == parser.VisibilityPublic {
					if existing, ok := c.publicFunctions[d.Name]; ok {
						return c.errorAt(DuplicateDeclaration, d.At, "duplicate public declaration of %q (also declared in %s)", d.Name, c.modulePath(existing.Module))
					}
					c.publicFunctions[d.Name] = function
				}
				if index == 0 || d.Visibility == parser.VisibilityPublic {
					c.result.Functions[d.Name] = function
				}

			default:
				if declaration == nil {
					return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "a module contains a nil declaration")
				}
				return c.errorAt(InvalidType, declaration.Pos(), "unsupported declaration %T", declaration)
			}
		}
	}
	c.current = nil
	return nil
}

// registerType adds a named type to its module and, when it is public, to the
// shared public namespace, detecting duplicate names.
func (c *checker) registerType(moduleIndex int, module *moduleInfo, declaration parser.Decl, name string, at parser.Position, visibility parser.Visibility, namedType *Type) error {
	if earlier, ok := module.declared[name]; ok {
		return c.errorAt(DuplicateDeclaration, at, "duplicate declaration of %q (previously declared at %d:%d)", name, earlier.Line, earlier.Column)
	}
	module.declared[name] = at
	module.types[name] = namedType
	c.result.TypeDecls[declaration] = namedType

	if visibility == parser.VisibilityPublic {
		if existing, ok := c.publicTypes[name]; ok {
			return c.errorAt(DuplicateDeclaration, at, "duplicate public declaration of %q (also declared in %s)", name, c.modulePath(existing.Module))
		}
		c.publicTypes[name] = namedType
	}
	if moduleIndex == 0 || visibility == parser.VisibilityPublic {
		c.result.Types[name] = namedType
	}
	return nil
}

// modulePath returns the source path of the module with the given ID, or a
// descriptive fallback.
func (c *checker) modulePath(id int) string {
	for _, module := range c.modules {
		if module.id == id {
			if module.path != "" {
				return module.path
			}
			return "the main module"
		}
	}
	return "another module"
}

// resolveStructs resolves each struct's field types and detects duplicate
// fields. Field types must exist and must not be void.
func (c *checker) resolveStructs() error {
	for index, module := range c.modules {
		c.current = module
		for _, declaration := range c.result.Modules[index].Program.Declarations {
			d, ok := declaration.(*parser.StructDecl)
			if !ok || d == nil {
				continue
			}
			info := c.result.TypeDecls[d].Struct
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
	}
	c.current = nil
	return nil
}

// resolveUnionFields resolves each union variant's field types and detects
// duplicate fields. Field types must exist and must not be void.
func (c *checker) resolveUnionFields() error {
	for index, module := range c.modules {
		c.current = module
		for _, declaration := range c.result.Modules[index].Program.Declarations {
			d, ok := declaration.(*parser.UnionDecl)
			if !ok || d == nil {
				continue
			}
			unionType := c.result.TypeDecls[d]
			for variantIndex, variantDeclaration := range d.Variants {
				if err := c.resolveVariantFields(variantDeclaration, unionType.Variants[variantIndex], d.Name); err != nil {
					return err
				}
			}
		}
	}
	c.current = nil
	return nil
}

// resolveVariantFields resolves the fields of one union variant.
func (c *checker) resolveVariantFields(declaration *parser.VariantDecl, variant *Variant, unionName string) error {
	if declaration == nil || variant == nil {
		return &Error{Kind: InvalidType, Line: 1, Column: 1, Message: "union variant is missing a declaration"}
	}
	for _, field := range declaration.Fields {
		if field == nil {
			return c.errorAt(InvalidType, declaration.At, "variant %q of union %q has a nil field declaration", variant.Name, unionName)
		}
		fieldType, err := c.resolveType(field.Type)
		if err != nil {
			return err
		}
		if fieldType.IsVoid() {
			return c.errorAt(InvalidType, field.Type.Pos(), "field %q of variant %q cannot have type void", field.Name, variant.Name)
		}
		if _, ok := variant.FieldIndex[field.Name]; ok {
			return c.errorAt(DuplicateDeclaration, field.At, "duplicate field %q in variant %q", field.Name, variant.Name)
		}
		structField := &StructField{Name: field.Name, Type: fieldType, At: field.At}
		variant.Fields = append(variant.Fields, structField)
		variant.FieldIndex[field.Name] = structField
	}
	return nil
}

// resolveFunctions resolves each function's parameter and return types and
// detects duplicate parameters. Parameter types must exist and must not be
// void. A return type may be void, which means the function returns no value.
func (c *checker) resolveFunctions() error {
	for index, module := range c.modules {
		c.current = module
		for _, declaration := range c.result.Modules[index].Program.Declarations {
			d, ok := declaration.(*parser.FunctionDecl)
			if !ok || d == nil {
				continue
			}
			function := c.result.FunctionDecls[d]

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
	}
	c.current = nil
	return nil
}

// checkBodies checks every user-defined function body in every module.
func (c *checker) checkBodies() error {
	for index, module := range c.modules {
		c.current = module
		for _, declaration := range c.result.Modules[index].Program.Declarations {
			d, ok := declaration.(*parser.FunctionDecl)
			if !ok || d == nil {
				continue
			}
			if err := c.checkFunction(d); err != nil {
				return err
			}
		}
	}
	c.current = nil
	return nil
}

// checkFunction checks a single function body. Parameters and the body's
// outermost statements share one scope. A non-void function must return a value
// on every path the conservative analysis can prove.
func (c *checker) checkFunction(declaration *parser.FunctionDecl) error {
	function := c.result.FunctionDecls[declaration]
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

	if reference.MapValue != nil {
		keyType, err := c.resolveType(reference.MapKey)
		if err != nil {
			return nil, err
		}
		valueType, err := c.resolveType(reference.MapValue)
		if err != nil {
			return nil, err
		}
		if keyType != StringType {
			return nil, c.errorAt(InvalidType, reference.MapKey.Pos(), "map keys must be string, found %s", keyType)
		}
		if valueType.IsVoid() {
			return nil, c.errorAt(InvalidType, reference.MapValue.Pos(), "map value type cannot be void")
		}
		resolved := c.mapType(keyType, valueType)
		c.result.TypeRefs[reference] = resolved
		return resolved, nil
	}

	if reference.Element != nil {
		element, err := c.resolveType(reference.Element)
		if err != nil {
			return nil, err
		}
		if element.IsVoid() {
			return nil, c.errorAt(InvalidType, reference.Pos(), "array element type cannot be void")
		}
		resolved := c.arrayType(element)
		c.result.TypeRefs[reference] = resolved
		return resolved, nil
	}

	var resolved *Type
	switch {
	case primitiveTypes[reference.Name] != nil:
		resolved = primitiveTypes[reference.Name]
	default:
		if structType, ok := c.lookupType(reference.Name); ok {
			resolved = structType
			break
		}
		if path, ok := c.privateType(reference.Name); ok {
			return nil, c.errorAt(PrivateAccess, reference.Pos(), "type %q is private to %s", reference.Name, path)
		}
		return nil, c.errorAt(UndefinedName, reference.Pos(), "undefined type %q", reference.Name)
	}

	c.result.TypeRefs[reference] = resolved
	return resolved, nil
}

// lookupType resolves a struct name in the current module's declarations and
// then in the shared public declarations.
func (c *checker) lookupType(name string) (*Type, bool) {
	if c.current != nil {
		if structType, ok := c.current.types[name]; ok {
			return structType, true
		}
	}
	if structType, ok := c.publicTypes[name]; ok {
		return structType, true
	}
	return nil, false
}

// lookupFunction resolves a function name in the current module's declarations
// and then in the shared public declarations.
func (c *checker) lookupFunction(name string) (*Function, bool) {
	if c.current != nil {
		if function, ok := c.current.functions[name]; ok {
			return function, true
		}
	}
	if function, ok := c.publicFunctions[name]; ok {
		return function, true
	}
	return nil, false
}

// privateType reports whether name is a struct declared privately in another
// module, and returns that module's path.
func (c *checker) privateType(name string) (string, bool) {
	for _, module := range c.modules {
		if module == c.current {
			continue
		}
		if _, ok := module.types[name]; ok {
			return c.modulePath(module.id), true
		}
	}
	return "", false
}

// privateFunction reports whether name is a function declared privately in
// another module, and returns that module's path.
func (c *checker) privateFunction(name string) (string, bool) {
	for _, module := range c.modules {
		if module == c.current {
			continue
		}
		if _, ok := module.functions[name]; ok {
			return c.modulePath(module.id), true
		}
	}
	return "", false
}

// arrayType returns the interned array type whose element type is element, so
// that the same element type always yields the same *Type.
func (c *checker) arrayType(element *Type) *Type {
	if existing, ok := c.arrayTypes[element]; ok {
		return existing
	}
	resolved := &Type{Kind: KindArray, Name: element.Name + "[]", Element: element}
	c.arrayTypes[element] = resolved
	return resolved
}

// mapTypeIdentity identifies a map type by its key and value types.
type mapTypeIdentity struct {
	key   *Type
	value *Type
}

// mapType returns the interned map type with the given key and value types, so
// that the same key and value types always yield the same *Type.
func (c *checker) mapType(key, value *Type) *Type {
	identity := mapTypeIdentity{key: key, value: value}
	if existing, ok := c.mapTypes[identity]; ok {
		return existing
	}
	resolved := &Type{Kind: KindMap, Name: "map[" + key.Name + "]" + value.Name, Key: key, Value: value}
	c.mapTypes[identity] = resolved
	return resolved
}

// errorAt builds a semantic error positioned at pos and located in the module
// currently being checked.
func (c *checker) errorAt(kind ErrorKind, pos parser.Position, format string, args ...any) error {
	file := ""
	if c.current != nil {
		file = c.current.path
	}
	return &Error{
		Kind:    kind,
		File:    file,
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
