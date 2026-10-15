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

	// StructTypes maps each struct name visible to the root module (its own
	// declarations plus all public declarations) to its resolved type. When two
	// modules declare private structs with the same name, this map holds only
	// the root module's.
	StructTypes map[string]*Type

	// Functions maps each function name visible to the root module to its
	// signature, with the same caveat as StructTypes.
	Functions map[string]*Function

	// Builtins maps each built-in function name to its signature.
	Builtins map[string]*Function

	// StructDecls maps each struct declaration to its resolved type, exactly,
	// even when private structs in different modules share a name.
	StructDecls map[*parser.StructDecl]*Type

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

	// Symbols maps each identifier reference that resolved to a symbol (a
	// variable, parameter, or function) to that symbol.
	Symbols map[*parser.Identifier]*Symbol
}

// moduleInfo holds the declarations of one module during checking.
type moduleInfo struct {
	path      string
	id        int
	structs   map[string]*Type
	functions map[string]*Function
	declared  map[string]parser.Position
}

// checker accumulates state during a single Check call. It holds no package
// level mutable state, so checking one program never affects another.
type checker struct {
	builtins   map[string]*Function
	arrayTypes map[*Type]*Type
	result     *Result

	// modules are the modules being checked and current is the module whose
	// declaration is being processed, used for name resolution and diagnostics.
	modules []*moduleInfo
	current *moduleInfo

	// publicStructs and publicFunctions hold every public declaration of every
	// module, shared across the whole program.
	publicStructs   map[string]*Type
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

// CheckModules performs semantic analysis and type checking on a program that
// may span several modules. The first module is the root module; the others are
// imported.
//
// Every module resolves names against its own declarations first, then against
// the public declarations of all modules, then against the built-ins. A
// declaration is public only when it is marked public, so private declarations
// are never visible to other modules.
func CheckModules(modules []*parser.Module) (*Result, error) {
	if len(modules) == 0 {
		return nil, &Error{Kind: InvalidType, Line: 1, Column: 1, Message: "cannot check a program with no modules"}
	}

	c := &checker{
		builtins:        builtinFunctions(),
		arrayTypes:      map[*Type]*Type{},
		publicStructs:   map[string]*Type{},
		publicFunctions: map[string]*Function{},
		result: &Result{
			Modules:         modules,
			StructTypes:     map[string]*Type{},
			Functions:       map[string]*Function{},
			Builtins:        map[string]*Function{},
			StructDecls:     map[*parser.StructDecl]*Type{},
			FunctionDecls:   map[*parser.FunctionDecl]*Function{},
			ExpressionTypes: map[parser.Expr]*Type{},
			TypeRefs:        map[*parser.TypeRef]*Type{},
			Symbols:         map[*parser.Identifier]*Symbol{},
		},
	}

	for _, module := range modules {
		if module == nil || module.Program == nil {
			return nil, &Error{Kind: InvalidType, Line: 1, Column: 1, Message: "module has no parsed program"}
		}
		c.modules = append(c.modules, &moduleInfo{
			path:      module.Path,
			id:        module.ID,
			structs:   map[string]*Type{},
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
	if err := c.resolveStructs(); err != nil {
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

// builtinFunctions returns the language's built-in functions. print and length
// are provided by the language, not declared by the user.
func builtinFunctions() map[string]*Function {
	return map[string]*Function{
		"print": {
			Name:         "print",
			ReturnType:   VoidType,
			Builtin:      BuiltinPrint,
			Variadic:     true,
			AllowedTypes: []*Type{IntType, FloatType, BoolType, StringType},
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
			switch d := declaration.(type) {
			case *parser.ImportDecl:
				// Import declarations carry no language declarations; the module
				// loader resolves them before checking.
				continue

			case *parser.StructDecl:
				if d == nil {
					return c.errorAt(InvalidType, parser.Position{Line: 1, Column: 1}, "a module contains a nil struct declaration")
				}
				if earlier, ok := module.declared[d.Name]; ok {
					return c.errorAt(DuplicateDeclaration, d.At, "duplicate declaration of %q (previously declared at %d:%d)", d.Name, earlier.Line, earlier.Column)
				}
				module.declared[d.Name] = d.At

				structType := &Type{
					Kind:   KindStruct,
					Name:   d.Name,
					Module: module.id,
					Struct: &StructInfo{Name: d.Name, FieldIndex: map[string]*StructField{}, Decl: d},
				}
				module.structs[d.Name] = structType
				c.result.StructDecls[d] = structType

				if d.Visibility == parser.VisibilityPublic {
					if existing, ok := c.publicStructs[d.Name]; ok {
						return c.errorAt(DuplicateDeclaration, d.At, "duplicate public declaration of %q (also declared in %s)", d.Name, c.modulePath(existing.Module))
					}
					c.publicStructs[d.Name] = structType
				}
				if index == 0 || d.Visibility == parser.VisibilityPublic {
					c.result.StructTypes[d.Name] = structType
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
			info := c.result.StructDecls[d].Struct
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
		if structType, ok := c.lookupStruct(reference.Name); ok {
			resolved = structType
			break
		}
		if path, ok := c.privateStruct(reference.Name); ok {
			return nil, c.errorAt(PrivateAccess, reference.Pos(), "type %q is private to %s", reference.Name, path)
		}
		return nil, c.errorAt(UndefinedName, reference.Pos(), "undefined type %q", reference.Name)
	}

	c.result.TypeRefs[reference] = resolved
	return resolved, nil
}

// lookupStruct resolves a struct name in the current module's declarations and
// then in the shared public declarations.
func (c *checker) lookupStruct(name string) (*Type, bool) {
	if c.current != nil {
		if structType, ok := c.current.structs[name]; ok {
			return structType, true
		}
	}
	if structType, ok := c.publicStructs[name]; ok {
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

// privateStruct reports whether name is a struct declared privately in another
// module, and returns that module's path.
func (c *checker) privateStruct(name string) (string, bool) {
	for _, module := range c.modules {
		if module == c.current {
			continue
		}
		if _, ok := module.structs[name]; ok {
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
