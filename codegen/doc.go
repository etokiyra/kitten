// Package codegen translates a parsed, type-checked Kitten program into C
// source.
//
// The public entry point is Generate, which consumes the syntax tree together
// with the checker's result. It relies on the checker for all name resolution
// and typing; it does not re-analyze the program. The generated C is
// deterministic for a given program and is written for the runtime in the
// sibling runtime package.
//
// Type mapping: int is int64_t, float is double, bool is bool, string is
// const char *, and a struct is a pointer to a GC-managed generated C struct.
// Struct literals allocate through the runtime, so struct values have reference
// semantics.
//
// The package produces C source as a string. It performs no compilation and no
// execution; those are the command-line driver's responsibility. Generate
// returns an error rather than panicking on an inconsistent syntax tree.
package codegen
