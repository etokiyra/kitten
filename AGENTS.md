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

The `.ktn` files under `tests/` are end-to-end tests: the lexer tokenizes them,
the parser builds a syntax tree, the checker validates them, the code generator
emits C, and the native executable's output is compared byte-for-byte with the
matching `.expected` file. Some fixtures are multi-file: `tests/modules/` has a
root file with imports; add the matching `.expected` next to the root file. Keep
the `.expected` files in sync with actual, verified program output; never edit
them to match buggy behavior.

## Architecture

7. Keep lexer, parser, loader, checker, code generator, and runtime concerns
   separated into their own packages: `lexer`, `parser`, `loader`, `checker`,
   `codegen`, `runtime`. A package must not reach into another phase's
   responsibilities.
8. Keep the compiler's architecture easy to test and extend. Prefer clear
   interfaces and table-driven tests over clever or tightly coupled code.
9. Use descriptive identifiers. Avoid unexplained single-letter variable names.
   Conventional short names are fine for loop indices and local coordinates.

## Process

10. Make small, focused changes. One coherent change per commit, with a clear
    message.
11. Do not begin future phases without explicit authorization. Do not implement
    self-hosting, a native backend, or standard-library features without
    explicit authorization.
12. Update documentation when established behavior changes: `SPEC.md` for
    language behavior, `AGENTS.md` for process and architecture.

## Current phase

Phase 1 delivered the project skeleton, the provisional specification, the
example programs, and the lexer. Phase 2A delivered the parser and the abstract
syntax tree. Phase 2B delivered semantic analysis and the type checker. Phase 2C
delivered the C code generator, the C runtime, and native compilation through
`kitten build` and `kitten run`. Phase 3A delivered dynamic strings:
concatenation with `+` and `+=`, and content-based equality, over the existing
NUL-terminated representation. Phase 3B delivered dynamic arrays: typed array
declarations, `new Element[length]` creation, bounds-checked indexing,
`length`, and GC-managed reference semantics. Phase 3C delivered file I/O
(`read_file`), a module system with `import`, cross-module visibility, and
multi-file compilation. Phase 3D delivered enums and tagged unions: named
alternatives, variants with typed payloads, safe construction, and an exhaustive
`match` statement for inspecting variants. Phase 3E delivered typed maps:
`map[string]V` with string keys, GC-managed reference semantics, and a hash
table in the runtime.

Self-hosting, a native backend, a complete standard library, and file writing
are still intentionally unimplemented: do not start them without explicit
authorization.

## Roadmap

1. Go-based compiler frontend and C backend. **Done** (Phases 1–2C).
2. A working, tested native compiler for the current language, including the
   language foundations self-hosting needs. **In progress** (Phase 3A added
   dynamic strings; Phase 3B added dynamic arrays; Phase 3C added file I/O and
   modules; Phase 3D added enums and tagged unions; Phase 3E added typed maps).
3. Rewrite the compiler in Kitten and verify it through an actual bootstrap.
4. Explore an LLVM IR or native assembly/object-code backend.
5. Grow a broader standard library and ecosystem for CLI, GUI, and game
   development.

The initial compiler is written in Go. The long-term goal is for Kitten to
compile its own compiler (self-hosting). This is a direction for future design,
not permission to add language features or phases early.
