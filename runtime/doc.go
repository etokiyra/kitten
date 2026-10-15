// Package runtime provides the C runtime support that compiled Kitten programs
// are linked against.
//
// The runtime is written in C and has no dependency on the Go compiler, so a
// future self-hosted Kitten compiler can keep using the same interface. It
// provides:
//
//   - garbage-collector initialization and managed allocation, backed by the
//     Boehm-Demers-Weiser conservative collector; and
//   - helpers for the built-in print function and for string value equality.
//
// HeaderSource and Source return the embedded C sources; the compiler writes
// them alongside generated code and compiles them together. The package
// performs no compilation or execution itself.
//
// The runtime is intentionally minimal. Dynamic strings, string concatenation,
// arrays, exceptions, and other runtime features are not implemented.
package runtime
