// Package parser turns the token stream produced by the lexer into a Kitten
// abstract syntax tree (AST).
//
// The public entry points are Parse, which operates on a token slice, and
// ParseSource, which lexes raw source and parses the result. Both are
// fail-fast: they report the first syntax error as an *Error carrying a
// one-based line:column position, and return a nil *Program on failure. The
// parser never panics, including on malformed, truncated, or empty input.
//
// The parser performs syntax analysis only. It does not resolve names, assign
// or check types, or decide whether a construct is semantically meaningful;
// those responsibilities belong to the checker. The AST records the syntax it
// was given, annotated with source positions, so that later phases can produce
// diagnostics that point back into the original source.
package parser
