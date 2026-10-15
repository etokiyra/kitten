// Package loader loads a Kitten program together with the modules it imports.
//
// It performs file access, lexical analysis, and parsing for every source file
// in a program, and resolves import paths. It does not perform semantic
// analysis; the checker consumes the modules it returns.
//
// Import paths are resolved relative to the directory of the file that contains
// the import, and canonicalized to absolute paths. A module is loaded at most
// once, keyed by its canonical path, so import cycles terminate. Import paths
// that resolve outside the root source file's directory are rejected.
package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kitten/lexer"
	"kitten/parser"
)

// Error describes a failure to load a module. Path is the module's path and, for
// a failure inside a source file, Line and Column give its position.
type Error struct {
	Path    string
	Line    int
	Column  int
	Message string
}

// Error implements the error interface, formatting the position as path:line:col.
func (e *Error) Error() string {
	switch {
	case e.Path != "" && e.Line > 0:
		return fmt.Sprintf("%s:%d:%d: %s", e.Path, e.Line, e.Column, e.Message)
	case e.Path != "":
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	default:
		return e.Message
	}
}

// Load parses the module at rootPath together with the modules it imports,
// directly or indirectly. The returned slice has the root module first, and
// imported modules follow in depth-first import order. Each module's ID is its
// index in the slice.
func Load(rootPath string) ([]*parser.Module, error) {
	root, err := canonicalPath(rootPath)
	if err != nil {
		return nil, &Error{Path: rootPath, Message: err.Error()}
	}

	state := &loadState{
		rootDirectory: filepath.Dir(root),
		loaded:        map[string]*parser.Module{},
	}
	if _, err := state.load(root, 0); err != nil {
		return nil, err
	}
	return state.modules, nil
}

// loadState carries the state of one Load call: the resolution root and the
// modules loaded so far.
type loadState struct {
	rootDirectory string
	modules       []*parser.Module
	loaded        map[string]*parser.Module
}

// load loads the module at the canonical path path, assigning it the given ID,
// and then loads the modules it imports. It is a no-op if path was already
// loaded, which is what makes import cycles terminate.
func (l *loadState) load(path string, id int) (*parser.Module, error) {
	if existing, ok := l.loaded[path]; ok {
		return existing, nil
	}
	if !withinDirectory(l.rootDirectory, path) {
		return nil, &Error{Path: path, Message: fmt.Sprintf("import resolves outside the root directory %s", l.rootDirectory)}
	}

	source, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Path: path, Message: fmt.Sprintf("cannot read source file: %v", err)}
	}

	tokens, err := lexer.Lex(string(source))
	if err != nil {
		return nil, wrapFrontendError(path, err)
	}

	program, err := parser.Parse(tokens)
	if err != nil {
		return nil, wrapFrontendError(path, err)
	}

	module := &parser.Module{Path: path, ID: id, Program: program}
	// Register the module before loading its imports so that a cycle back to
	// this module resolves to the module already in progress.
	l.loaded[path] = module
	l.modules = append(l.modules, module)

	directory := filepath.Dir(path)
	for _, declaration := range program.Declarations {
		importDeclaration, ok := declaration.(*parser.ImportDecl)
		if !ok || importDeclaration == nil {
			continue
		}

		importPath, err := canonicalPath(filepath.Join(directory, importDeclaration.Path))
		if err != nil {
			return nil, &Error{
				Path:    path,
				Line:    importDeclaration.At.Line,
				Column:  importDeclaration.At.Column,
				Message: fmt.Sprintf("cannot resolve import %q: %v", importDeclaration.Path, err),
			}
		}
		if _, err := l.load(importPath, len(l.modules)); err != nil {
			return nil, err
		}
	}

	return module, nil
}

// canonicalPath returns an absolute, cleaned path.
func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

// withinDirectory reports whether path is directory or lies inside it.
func withinDirectory(directory, path string) bool {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}
	separator := string(filepath.Separator)
	return relative != ".." && !strings.HasPrefix(relative, ".."+separator)
}

// wrapFrontendError attaches a module path to a lexer or parser error.
func wrapFrontendError(path string, err error) error {
	if lexical, ok := err.(*lexer.Error); ok {
		return &Error{Path: path, Line: lexical.Line, Column: lexical.Column, Message: lexical.Message}
	}
	if syntax, ok := err.(*parser.Error); ok {
		return &Error{Path: path, Line: syntax.Line, Column: syntax.Column, Message: syntax.Message}
	}
	return &Error{Path: path, Message: err.Error()}
}
