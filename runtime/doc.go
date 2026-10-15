// Package runtime provides the C runtime support that compiled Kitten programs
// are linked against.
//
// The runtime is written in C and has no dependency on the Go compiler, so a
// future self-hosted Kitten compiler can keep using the same interface. It
// provides:
//
//   - garbage-collector initialization and managed allocation, backed by the
//     Boehm-Demers-Weiser conservative collector;
//   - strings: an immutable, length-carrying byte sequence represented by the
//     ktn_string type, with constant-time length and indexing, value equality,
//     concatenation, append, and byte ranges;
//   - dynamic arrays: allocation, bounds-checked element access, and length;
//   - string-keyed maps: a hash table with insert, lookup, membership, and
//     length operations;
//   - whole-file reading; and
//   - helpers for the built-in print function.
//
// HeaderSource and Source return the embedded C sources; the compiler writes
// them alongside generated code and compiles them together. The package
// performs no compilation or execution itself.
//
// The runtime is intentionally minimal. Exceptions, finalizers, and file
// writing are not implemented.
//
// The package also records the runtime's ABI names, which are the identifiers
// generated code calls.
package runtime
