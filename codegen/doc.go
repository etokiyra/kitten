// Package codegen will translate a checked syntax tree into C source code,
// which is then compiled into a native executable by a system C compiler and
// linked against the Boehm garbage collector.
//
// It is intentionally empty in Phase 1. Code generation is deferred to a later
// phase and must not be implemented here until it is explicitly authorized.
package codegen
