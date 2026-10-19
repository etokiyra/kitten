package checker

import (
	"fmt"
	"strings"

	"kitten/lexer"
	"kitten/parser"
)

// The fixed source blocks of the implicit prelude. Each block ends with its
// closing brace and a newline, so concatenation puts the next union on its own
// line and the only-read source is exactly the source used before the file and
// directory built-ins existed.
const (
	readFileResultPrelude = `public union ReadFileResult {
    Ok { string text; }
    Failed { string message; }
}
`

	fileResultPrelude = `public union FileResult {
    Ok
    Failed { string message; }
}
`

	listDirectoryResultPrelude = `public union ListDirectoryResult {
    Ok { string[] entries; }
    Failed { string message; }
}
`

	runResultPrelude = `public union RunResult {
    Ok { int status; }
    Failed { string message; }
}
`

	currentDirectoryResultPrelude = `public union CurrentDirectoryResult {
    Ok { string path; }
    Failed { string message; }
}
`

	envResultPrelude = `public union EnvResult {
    Set { string value; }
    Unset
}
`

	capturedRunResultPrelude = `public union CapturedRunResult {
    Ok { int status; string stdout; string stderr; }
    Failed { string message; }
}
`

	fileInfoResultPrelude = `public union FileInfoResult {
    Ok { bool is_file; bool is_directory; bool is_symlink; bool executable; int size; }
    Missing
    Failed { string message; }
}
`

	tempDirectoryResultPrelude = `public union TempDirectoryResult {
    Ok { string path; }
    Failed { string message; }
}
`
)

// preludeModulePath and runtimeModulePath identify the two compiler-reserved
// synthetic modules. User source files cannot normally have these paths; the
// only consequence is that reserved-name and intrinsic checks treat them as
// synthetic.
const (
	preludeModulePath = "<prelude>"
	runtimeModulePath = "<runtime>"
)

// syntheticModulePaths is the table of compiler-reserved module paths. It is
// deliberately closed: adding a synthetic module requires naming it here.
var syntheticModulePaths = map[string]bool{
	preludeModulePath: true,
	runtimeModulePath: true,
}

// isSyntheticModulePath reports whether path names a compiler-reserved module.
func isSyntheticModulePath(path string) bool {
	return syntheticModulePaths[path]
}

// preludeUses records which prelude built-ins a program calls syntactically.
type preludeUses struct {
	readFile         bool
	writeFile        bool
	writeBytes       bool
	createDirectory  bool
	remove           bool
	listDirectory    bool
	run              bool
	currentDirectory bool
	environment      bool
	capture          bool
	fileInfo         bool
	tempDirectory    bool
}

// any reports whether any prelude built-in is used.
func (u preludeUses) any() bool {
	return u.readFile || u.writeFile || u.writeBytes || u.createDirectory ||
		u.remove || u.listDirectory || u.run || u.currentDirectory || u.environment || u.capture || u.fileInfo || u.tempDirectory
}

// preludeSource assembles the fixed prelude source for the used built-ins. The
// order is fixed, and a union appears only when a built-in that returns it is
// used. A program using only try_read_file gets exactly readFileResultPrelude.
func preludeSource(uses preludeUses) string {
	var source strings.Builder
	if uses.readFile {
		source.WriteString(readFileResultPrelude)
	}
	if uses.writeFile || uses.writeBytes || uses.createDirectory || uses.remove {
		source.WriteString(fileResultPrelude)
	}
	if uses.listDirectory {
		source.WriteString(listDirectoryResultPrelude)
	}
	if uses.run {
		source.WriteString(runResultPrelude)
	}
	if uses.currentDirectory {
		source.WriteString(currentDirectoryResultPrelude)
	}
	if uses.environment {
		source.WriteString(envResultPrelude)
	}
	if uses.capture {
		source.WriteString(capturedRunResultPrelude)
	}
	if uses.fileInfo {
		source.WriteString(fileInfoResultPrelude)
	}
	if uses.tempDirectory {
		source.WriteString(tempDirectoryResultPrelude)
	}
	return source.String()
}

// parsePrelude parses assembled prelude source into a module with ID 0. The
// caller assigns the final ID when appending it.
func parsePrelude(source string) (*parser.Module, error) {
	return parseSyntheticModule(preludeModulePath, source)
}

// parseSyntheticModule parses source for one compiler-reserved module.
func parseSyntheticModule(path, source string) (*parser.Module, error) {
	tokens, err := lexer.Lex(source)
	if err != nil {
		return nil, fmt.Errorf("parsing the prelude: %w", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		return nil, fmt.Errorf("parsing the prelude: %w", err)
	}
	return &parser.Module{Path: path, Program: program}, nil
}

// scanPreludeUses scans every module for syntactic calls to the twelve prelude
// built-ins. It is deliberately syntactic: a user function with one of these
// names may cause the prelude to be injected unused, but helpers are non-static
// and no semantic rule depends on the false positive.
func scanPreludeUses(modules []*parser.Module) preludeUses {
	return preludeUses{
		readFile:         usesCallee(modules, "try_read_file"),
		writeFile:        usesCallee(modules, "try_write_file"),
		writeBytes:       usesCallee(modules, "try_write_bytes"),
		createDirectory:  usesCallee(modules, "try_create_directory"),
		remove:           usesCallee(modules, "try_remove"),
		listDirectory:    usesCallee(modules, "try_list_directory"),
		run:              usesCallee(modules, "try_run"),
		currentDirectory: usesCallee(modules, "try_current_directory"),
		environment:      usesCallee(modules, "try_getenv"),
		capture:          usesCallee(modules, "try_run_capture"),
		fileInfo:         usesCallee(modules, "try_file_info"),
		tempDirectory:    usesCallee(modules, "try_create_temp_directory"),
	}
}

// usesCallee reports whether any module contains a syntactic call whose callee
// is the given identifier.
func usesCallee(modules []*parser.Module, calleeName string) bool {
	for _, module := range modules {
		if module == nil || module.Program == nil {
			continue
		}
		for _, declaration := range module.Program.Declarations {
			if declarationUsesCallee(declaration, calleeName) {
				return true
			}
		}
	}
	return false
}

// declarationUsesCallee scans a top-level declaration. Only a function body can
// contain an expression.
func declarationUsesCallee(declaration parser.Decl, calleeName string) bool {
	function, ok := declaration.(*parser.FunctionDecl)
	if !ok || function == nil {
		return false
	}
	return statementUsesCallee(function.Body, calleeName)
}

// statementUsesCallee scans one statement and its children.
func statementUsesCallee(statement parser.Stmt, calleeName string) bool {
	switch s := statement.(type) {
	case *parser.BlockStmt:
		if s == nil {
			return false
		}
		for _, inner := range s.Statements {
			if statementUsesCallee(inner, calleeName) {
				return true
			}
		}
	case *parser.VarDeclStmt:
		return s != nil && expressionUsesCallee(s.Initializer, calleeName)
	case *parser.ExprStmt:
		return s != nil && expressionUsesCallee(s.Expression, calleeName)
	case *parser.IfStmt:
		return s != nil &&
			(expressionUsesCallee(s.Condition, calleeName) ||
				statementUsesCallee(s.Then, calleeName) ||
				statementUsesCallee(s.Else, calleeName))
	case *parser.WhileStmt:
		return s != nil && (expressionUsesCallee(s.Condition, calleeName) || statementUsesCallee(s.Body, calleeName))
	case *parser.ForStmt:
		return s != nil &&
			(statementUsesCallee(s.Init, calleeName) ||
				expressionUsesCallee(s.Condition, calleeName) ||
				expressionUsesCallee(s.Increment, calleeName) ||
				statementUsesCallee(s.Body, calleeName))
	case *parser.ReturnStmt:
		return s != nil && expressionUsesCallee(s.Value, calleeName)
	case *parser.MatchStmt:
		if s == nil {
			return false
		}
		if expressionUsesCallee(s.Scrutinee, calleeName) {
			return true
		}
		for _, arm := range s.Arms {
			if arm != nil && statementUsesCallee(arm.Body, calleeName) {
				return true
			}
		}
	}
	return false
}

// expressionUsesCallee scans one expression and its children.
func expressionUsesCallee(expression parser.Expr, calleeName string) bool {
	switch e := expression.(type) {
	case nil:
		return false
	case *parser.Identifier, *parser.IntLiteral, *parser.FloatLiteral,
		*parser.StringLiteral, *parser.BoolLiteral, *parser.NewMapExpr:
		return false
	case *parser.UnaryExpr:
		return e != nil && expressionUsesCallee(e.Operand, calleeName)
	case *parser.BinaryExpr:
		return e != nil && (expressionUsesCallee(e.Left, calleeName) || expressionUsesCallee(e.Right, calleeName))
	case *parser.AssignExpr:
		return e != nil && (expressionUsesCallee(e.Target, calleeName) || expressionUsesCallee(e.Value, calleeName))
	case *parser.CallExpr:
		if e == nil {
			return false
		}
		if identifier, ok := e.Callee.(*parser.Identifier); ok &&
			identifier != nil && identifier.Name == calleeName {
			return true
		}
		if expressionUsesCallee(e.Callee, calleeName) {
			return true
		}
		for _, argument := range e.Arguments {
			if expressionUsesCallee(argument, calleeName) {
				return true
			}
		}
	case *parser.MemberExpr:
		return e != nil && expressionUsesCallee(e.Receiver, calleeName)
	case *parser.StructLiteral:
		if e == nil {
			return false
		}
		for _, field := range e.Fields {
			if field != nil && expressionUsesCallee(field.Value, calleeName) {
				return true
			}
		}
	case *parser.VariantLiteral:
		if e == nil {
			return false
		}
		for _, field := range e.Fields {
			if field != nil && expressionUsesCallee(field.Value, calleeName) {
				return true
			}
		}
	case *parser.NewArrayExpr:
		return e != nil && expressionUsesCallee(e.Length, calleeName)
	case *parser.IndexExpr:
		return e != nil && (expressionUsesCallee(e.Target, calleeName) || expressionUsesCallee(e.Index, calleeName))
	}
	return false
}

// declarationName returns the name and position of a top-level declaration.
func declarationName(declaration parser.Decl) (string, parser.Position, bool) {
	switch d := declaration.(type) {
	case *parser.FunctionDecl:
		if d != nil {
			return d.Name, d.At, true
		}
	case *parser.StructDecl:
		if d != nil {
			return d.Name, d.At, true
		}
	case *parser.EnumDecl:
		if d != nil {
			return d.Name, d.At, true
		}
	case *parser.UnionDecl:
		if d != nil {
			return d.Name, d.At, true
		}
	}
	return "", parser.Position{}, false
}

// preludeReservedNames are reserved against every user top-level declaration,
// whether or not the corresponding prelude union is introduced.
var preludeReservedNames = map[string]bool{
	"ReadFileResult":         true,
	"FileResult":             true,
	"ListDirectoryResult":    true,
	"RunResult":              true,
	"CurrentDirectoryResult": true,
	"EnvResult":              true,
	"CapturedRunResult":      true,
	"FileInfoResult":         true,
	"TempDirectoryResult":    true,
}

// registerPreludeBuiltins adds the built-ins whose calls triggered the prelude.
// Each result type is read from the collected prelude, so a builtin's return
// type is the prelude's resolved union.
func (c *checker) registerPreludeBuiltins(uses preludeUses) error {
	type registration struct {
		used       bool
		name       string
		parameters []*Type
		result     string
		builtin    BuiltinKind
	}
	registrations := []registration{
		{uses.readFile, "try_read_file", []*Type{StringType}, "ReadFileResult", BuiltinTryReadFile},
		{uses.writeFile, "try_write_file", []*Type{StringType, StringType}, "FileResult", BuiltinTryWriteFile},
		{uses.writeBytes, "try_write_bytes", []*Type{StringType, c.arrayType(ByteType)}, "FileResult", BuiltinTryWriteBytes},
		{uses.createDirectory, "try_create_directory", []*Type{StringType}, "FileResult", BuiltinTryCreateDirectory},
		{uses.remove, "try_remove", []*Type{StringType}, "FileResult", BuiltinTryRemove},
		{uses.listDirectory, "try_list_directory", []*Type{StringType}, "ListDirectoryResult", BuiltinTryListDirectory},
		{uses.run, "try_run", []*Type{StringType, c.arrayType(StringType)}, "RunResult", BuiltinTryRun},
		{uses.currentDirectory, "try_current_directory", []*Type{}, "CurrentDirectoryResult", BuiltinTryCurrentDirectory},
		{uses.environment, "try_getenv", []*Type{StringType}, "EnvResult", BuiltinTryGetenv},
		{uses.capture, "try_run_capture", []*Type{StringType, c.arrayType(StringType)}, "CapturedRunResult", BuiltinTryRunCapture},
		{uses.fileInfo, "try_file_info", []*Type{StringType}, "FileInfoResult", BuiltinTryFileInfo},
		{uses.tempDirectory, "try_create_temp_directory", []*Type{StringType}, "TempDirectoryResult", BuiltinTryCreateTempDirectory},
	}
	for _, entry := range registrations {
		if !entry.used {
			continue
		}
		resultType, ok := c.publicTypes[entry.result]
		if !ok || resultType == nil {
			return &Error{
				Kind:    InvalidType,
				Line:    1,
				Column:  1,
				Message: fmt.Sprintf("the %s prelude was not registered", entry.result),
			}
		}
		function := &Function{
			Name:           entry.name,
			ParameterTypes: entry.parameters,
			ReturnType:     resultType,
			Builtin:        entry.builtin,
		}
		c.builtins[entry.name] = function
		c.result.Builtins[entry.name] = function
	}
	return nil
}
