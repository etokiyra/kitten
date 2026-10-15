# AGENTS.md

Project instructions for everyone (human or AI) working on Kitten.

## Source of truth

1. `SPEC.md` is the source of truth for language semantics. If code and
   `SPEC.md` disagree, the specification wins until it is deliberately changed.
2. Never change established language semantics without explicit authorization.
   If a change is needed, update `SPEC.md` first and call the change out.
3. If a design decision is unresolved and affects implementation, ask before
   proceeding when necessary. Do not silently invent semantics. Record the open
   question under "Deferred decisions and unresolved semantics" in `SPEC.md`.

## Testing

4. Every language feature must have appropriate tests. Once executable language
   features exist, add a `.ktn` program and a matching `.expected` file under
   `tests/`, and compare the program's actual output against the expected output.
5. Run `go test ./...` before completing a task. Also run `go vet ./...` and
   `gofmt` (or `gofmt -l .`) when Go code changed.
6. Never claim a feature is implemented without verifying it. Run the relevant
   tests or commands and report what actually happened.

The current `.ktn` files under `tests/` are lexer and parser inputs and future
executable-program tests. Their `.expected` files document intended program
output. The current compiler cannot execute them; the lexer tokenizes them and
the parser builds a syntax tree from them. Do not describe them as passing run
tests.

## Architecture

7. Keep lexer, parser, checker, code generator, and runtime concerns separated
   into their own packages: `lexer`, `parser`, `checker`, `codegen`, `runtime`.
   A package must not reach into another phase's responsibilities.
8. Keep the compiler's architecture easy to test and extend. Prefer clear
   interfaces and table-driven tests over clever or tightly coupled code.
9. Use descriptive identifiers. Avoid unexplained single-letter variable names.
   Conventional short names are fine for loop indices and local coordinates.

## Process

10. Make small, focused changes. One coherent change per commit, with a clear
    message.
11. Do not begin future phases without explicit authorization. Packages for
    later phases currently contain documentation only.
12. Update documentation when established behavior changes: `SPEC.md` for
    language behavior, `AGENTS.md` for process and architecture.

## Current phase

Phase 1 delivered the project skeleton, the provisional specification, the
example programs, and the lexer. Phase 2A delivered the parser and the abstract
syntax tree. The checker, code generator, and runtime are still intentionally
unimplemented: do not add implementation code to `checker/`, `codegen/`, or
`runtime/` without explicit authorization, and do not begin later phases.

## Roadmap

The initial compiler is written in Go. The long-term goal is for Kitten to
compile its own compiler (self-hosting). This is a direction for future design,
not permission to add language features or phases early.
