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
10. Components written in Kitten for the eventual self-hosting compiler live
    under `selfhost/`. They are compiled and tested by the bootstrap compiler but
    are not wired into `kitten build` or `kitten run`, which keep using the Go
    implementation. Do not make the production pipeline depend on them without
    explicit authorization.

## Process

11. Make small, focused changes. One coherent change per commit, with a clear
    message.
12. Do not begin future phases without explicit authorization. Do not implement
    self-hosting, a native backend, or standard-library features without
    explicit authorization.
13. Update documentation when established behavior changes: `SPEC.md` for
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
table in the runtime. Phase 3F prepared for a Kitten-written lexer: string byte
length, bounds-checked byte indexing, and byte ranges (`substring`). Phase 3G
delivered the first real component written in Kitten: a lexer under
`selfhost/lexer` that mirrors the Go reference lexer and is checked against it by
a differential corpus of 772 inputs in `cmd/kitten`. Phase 3H gave strings a
stored byte length alongside the bytes, so `length` and indexing are
constant-time while the bytes stay NUL-terminated; the observable string
semantics did not change, and the Kitten-written lexer now scans linearly. Phase
4A added the next self-hosted component: a parser under `selfhost/parser`, driven
by `selfhost/parse.ktn`, that reproduces the Go reference parser's grammar,
precedence, tree, and diagnostics, and is checked against it by a differential
corpus of 262 inputs in `cmd/kitten`. Phase 4B added a semantic checker under
`selfhost/checker`, driven by `selfhost/check.ktn`, that reproduces the Go
checker's passes, rules, and diagnostics. Phase 4C exposed the checker's resolved
semantic model — the types, symbols, variants, and declarations a generator reads
— for a future code generator. Phase 4D.1 began the C code generator written in
Kitten: its C naming, C type mapping, string-literal escaping, and bounded output
accumulation under `selfhost/codegen`, exposed by `selfhost/codegen.ktn` and
checked against the Go generator in `cmd/kitten`. Phase 4D.2 added the declaration
emitter under `selfhost/codegen/declarations.ktn`, driven by
`selfhost/declarations.ktn`: it reproduces the nine sections the reference
generator emits before any function body, byte for byte, and is checked against
the reference over a targeted corpus, the checker corpus, and representative
native compilations. Phase 4D.3 added the core expression emitter under
`selfhost/codegen/expressions.ktn`, driven by `selfhost/expressions.ktn`: integer,
floating-point, boolean, and string literals, identifiers, unary expressions, and
binary expressions, checked against the reference node by node. Phase 4D.4 added
the string literal pool under `selfhost/codegen/literals.ktn`, which the expression
emitter fills as it renders and which the differential harness compares with the
reference's own pooled declarations. Phase 4D.5 added the statement and function
emitter under `selfhost/codegen/statements.ktn`, driven by `selfhost/functions.ktn`:
blocks, local declarations, expression statements, `if`/`else`, `while`, `for`,
`return`, and `match`, checked against the reference's own function definitions byte
for byte. Assignments, calls, member access, struct and variant literals,
allocation, and indexing are still reported as not implemented rather than rendered
when a statement needs one. Phase 4D.6 added the translation unit emitter under
`selfhost/codegen/unit.ktn`, driven by `selfhost/unit.ktn`: the entry point rule, the
C entry point, and the assembly of the declaration sections, the literal pool, the
bodies, and the entry point into one complete translation unit, checked against the
reference's whole output byte for byte. A module loader and the eight remaining
expression kinds are still to come.

None of the self-hosted components is part of the production pipeline: `kitten
build` and `kitten run` still use the Go lexer, parser, checker, and generator,
which remain authoritative. Their deliberate differences (byte-oriented
positions, no command-line arguments or chosen exit status, and no entry point or
module loader yet) are documented in SPEC.md
Sections 25 to 28, `selfhost/lexer/README.md`, `selfhost/parser/README.md`,
`selfhost/checker/README.md`, and `selfhost/codegen/README.md`.

Self-hosting, a native backend, a complete standard library, and file writing
are still intentionally unimplemented: do not start them without explicit
authorization.

## Roadmap

1. Go-based compiler frontend and C backend. **Done** (Phases 1–2C).
2. A working, tested native compiler for the current language, including the
   language foundations self-hosting needs. **In progress** (Phase 3A added
   dynamic strings; Phase 3B added dynamic arrays; Phase 3C added file I/O and
   modules; Phase 3D added enums and tagged unions; Phase 3E added typed maps;
   Phase 3F added string byte access; Phase 3G added a Kitten-written lexer
   checked against the Go lexer, without replacing it; Phase 3H made strings
   length-carrying so that scanning is linear; Phase 4A added a Kitten-written
   parser checked against the Go parser, without replacing it; Phase 4B added a
   Kitten-written semantic checker checked against the Go checker, without
   replacing it; Phase 4C exposed the checker's resolved semantic model for a
   future code generator; Phase 4D.1 began the Kitten-written C generator with
   its naming, C type mapping, string escaping, and bounded output accumulation;
   Phase 4D.2 added its declaration emitter, checked against the reference
   generator byte for byte; Phase 4D.3 added its core expression emitter, checked
   node by node against the reference; Phase 4D.4 added string literal pooling,
   checked against the reference's pooled declarations; Phase 4D.5 added the
   statement and function-body emitter, checked against the reference's own
   definitions; Phase 4D.6 added the entry point rule and the complete translation
   unit assembly, checked against the reference's whole output).
3. Rewrite the compiler in Kitten and verify it through an actual bootstrap.
4. Explore an LLVM IR or native assembly/object-code backend.
5. Grow a broader standard library and ecosystem for CLI, GUI, and game
   development.

The initial compiler is written in Go. The long-term goal is for Kitten to
compile its own compiler (self-hosting). This is a direction for future design,
not permission to add language features or phases early.
