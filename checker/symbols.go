package checker

import "kitten/parser"

// SymbolKind distinguishes the kinds of name a scope can hold.
type SymbolKind int

const (
	// SymbolVariable is a local variable or a function parameter.
	SymbolVariable SymbolKind = iota

	// SymbolFunction is a function (user-defined or built-in).
	SymbolFunction
)

// Symbol is a resolved name.
type Symbol struct {
	// Name is the declared name.
	Name string

	// Kind is what the name refers to.
	Kind SymbolKind

	// Type is the variable's type, or the function's return type.
	Type *Type

	// Function is set when Kind is SymbolFunction.
	Function *Function

	// At is the position where the name was declared.
	At parser.Position
}

// BuiltinKind identifies a language built-in function. NotBuiltin is the zero
// value, used for every user-defined function.
type BuiltinKind int

const (
	// NotBuiltin marks a user-defined function.
	NotBuiltin BuiltinKind = iota

	// BuiltinPrint is the variadic print function.
	BuiltinPrint

	// BuiltinEprint is the variadic standard-error writer.
	BuiltinEprint

	// BuiltinExit terminates the process with an int status.
	BuiltinExit

	// BuiltinLength returns the length of an array.
	BuiltinLength

	// BuiltinReadFile reads a whole file into a string.
	BuiltinReadFile

	// BuiltinArgv returns the process argument vector.
	BuiltinArgv

	// BuiltinTryReadFile reads a file into the predeclared result union.
	BuiltinTryReadFile

	// BuiltinTryWriteFile creates or truncates a file and writes a string.
	BuiltinTryWriteFile

	// BuiltinTryWriteBytes creates or truncates a file and writes a byte array.
	BuiltinTryWriteBytes

	// BuiltinTryCreateDirectory creates one directory level.
	BuiltinTryCreateDirectory

	// BuiltinTryRemove removes one file, link, or empty directory.
	BuiltinTryRemove

	// BuiltinTryListDirectory lists sorted directory entry names.
	BuiltinTryListDirectory

	// BuiltinTryRun launches a process and returns its status.
	BuiltinTryRun

	// BuiltinTryCurrentDirectory returns the absolute current directory.
	BuiltinTryCurrentDirectory

	// BuiltinTryGetenv reads one environment variable.
	BuiltinTryGetenv

	// BuiltinTryRunCapture runs a program and captures its streams.
	BuiltinTryRunCapture

	// BuiltinTryFileInfo reports file metadata without opening a file.
	BuiltinTryFileInfo

	// BuiltinRawLoad8, BuiltinRawStore8, BuiltinRawLoad64, and
	// BuiltinRawStore64 are runtime-module memory intrinsics.
	BuiltinRawLoad8
	BuiltinRawStore8
	BuiltinRawLoad64
	BuiltinRawStore64

	// BuiltinRawCopy and BuiltinRawZero are runtime-module bulk operations.
	BuiltinRawCopy
	BuiltinRawZero

	// BuiltinAddressOf returns the first-byte address of a byte array or string.
	BuiltinAddressOf

	// BuiltinSyscall invokes the host system call primitive.
	BuiltinSyscall

	// BuiltinMapHas reports whether a map contains a key.
	BuiltinMapHas

	// BuiltinSubstring returns a byte range of a string.
	BuiltinSubstring

	// BuiltinIntFromByte converts a byte to an int.
	BuiltinIntFromByte

	// BuiltinByteFromInt converts an int to its low byte.
	BuiltinByteFromInt

	// BuiltinStringToBytes returns a string's bytes as a byte array.
	BuiltinStringToBytes

	// BuiltinBytesToString returns a byte array as a string.
	BuiltinBytesToString
)

// Function is a resolved function signature, either user-defined or built-in.
type Function struct {
	// Name is the function name.
	Name string

	// ParameterNames are the parameter names in order. Empty for a built-in.
	ParameterNames []string

	// ParameterTypes are the parameter types in order.
	ParameterTypes []*Type

	// ReturnType is the declared return type, or void when none was written.
	ReturnType *Type

	// Module is the ID of the module that declared this function. It is 0 for a
	// function in the root module and for a built-in.
	Module int

	// Builtin identifies a language provided function, or NotBuiltin.
	Builtin BuiltinKind

	// Variadic is true for a built-in that accepts one or more arguments, each
	// of which must be one of AllowedTypes. print uses this form.
	Variadic bool

	// AllowedTypes lists the argument types a variadic built-in accepts.
	AllowedTypes []*Type

	// Decl is the syntax node for a user-defined function. It is nil for a
	// built-in.
	Decl *parser.FunctionDecl
}

// Scope is a lexical scope: a set of names plus an optional enclosing scope.
type Scope struct {
	parent  *Scope
	symbols map[string]*Symbol
}

// newScope returns an empty scope nested inside parent. A nil parent is the
// outermost scope.
func newScope(parent *Scope) *Scope {
	return &Scope{parent: parent, symbols: map[string]*Symbol{}}
}

// declare adds symbol to this scope. If a symbol with the same name already
// exists in this scope, declare returns it and adds nothing, so the caller can
// report a duplicate. Names in enclosing scopes are not consulted, which is
// what allows an inner scope to shadow an outer one.
func (s *Scope) declare(symbol *Symbol) *Symbol {
	if existing, ok := s.symbols[symbol.Name]; ok {
		return existing
	}
	s.symbols[symbol.Name] = symbol
	return nil
}

// lookup returns the symbol for name, searching this scope and then enclosing
// scopes.
func (s *Scope) lookup(name string) (*Symbol, bool) {
	for current := s; current != nil; current = current.parent {
		if symbol, ok := current.symbols[name]; ok {
			return symbol, true
		}
	}
	return nil, false
}
