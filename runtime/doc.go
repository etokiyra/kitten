// Package runtime will hold the language runtime: the support code linked into
// compiled Kitten programs, including garbage-collection integration, built-in
// functions such as print, and any other runtime services the language needs.
//
// It is intentionally empty in Phase 1. The runtime and its garbage-collection
// strategy are deferred to a later phase and must not be implemented here until
// they are explicitly authorized.
package runtime
