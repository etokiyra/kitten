// Package checker performs semantic analysis and static type checking on a
// parsed Kitten program.
//
// The public entry point is Check, which takes the *parser.Program produced by
// the parser and returns a *Result describing the resolved program together
// with an error. On success the Result records struct types, function
// signatures, the type of every expression, and the symbol each identifier
// resolved to, so that later phases can consume the checked program without
// redoing analysis.
//
// Check is fail-fast: it reports the first semantic error as an *Error carrying
// a one-based line:column position and a category, and returns a nil Result. It
// never panics, including on hand-built or incomplete syntax trees.
//
// The checker implements the type, scope, function, return, struct, and built-in
// rules recorded in SPEC.md. It does not generate code, run the program, or
// perform any optimization; those are separate phases.
package checker
