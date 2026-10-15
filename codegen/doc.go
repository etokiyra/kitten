// Package codegen translates a parsed, type-checked Kitten program into C
// source.
//
// The public entry point is Generate, which consumes the program's modules
// together with the checker's result. It relies on the checker for all name
// resolution and typing; it does not re-analyze the program. Declarations from
// every module are emitted into one translation unit, with imported modules'
// top-level names mangled by module number so that private declarations in
// different modules cannot collide. The generated C is deterministic for a
// given program and is written for the runtime in the sibling runtime package.
//
// Type mapping: int is int64_t, float is double, bool is bool, string is
// const char *, a struct or union is a pointer to a GC-managed generated C
// struct, an enum is an int64_t typedef with one constant per case, and an array
// is a ktn_array pointer. Struct and union literals allocate through the runtime,
// so struct and union values have reference semantics, while enums are values.
// Union payloads are read only through a bounds-safe switch generated for match,
// never through unchecked field access.
//
// The package produces C source as a string. It performs no compilation and no
// execution; those are the command-line driver's responsibility. Generate
// returns an error rather than panicking on an inconsistent syntax tree.
package codegen
