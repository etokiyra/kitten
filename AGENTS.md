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

## Licensing

Kitten is GPL-3.0-or-later, with one deliberate exception: the runtime under
`runtime/csrc/` is MIT. The split exists to serve two goals at once. The GPL
keeps the compiler free and stops anyone from forking this implementation into a
closed product. The permissive runtime keeps compiled programs free of the
compiler's license, so people can distribute what they build under any terms
they choose.

14. Keep the toolchain GPL-3.0-or-later and code linked into compiled programs
    MIT. Do not move runtime or standard-library sources under the GPL, and do
    not apply the GPL to anything that ends up inside a user's executable.
15. When adding a component, decide which side of that line it is on and give it
    the matching notice. The compiler and its tools are GPL; anything compiled
    and linked into user programs is MIT.
16. Leave the license notices at the top of the `runtime/csrc/` files intact.
    Those files are copied next to generated code and compiled by third parties,
    so the notice has to travel with them.

## Naming

The language is **Kitten**, source files are **`.ktn`**, and the command-line tool
is **`purr`**. `purr` was chosen because the Kitty terminal already ships a
`kitten` command, so a language tool named `kitten` would collide with it in
`PATH`. The project manifest is `purr.toml` and the lockfile is `purr.lock`.

17. Write new documentation against `purr`, not `kitten`. The compiler binary is
    still built from `cmd/kitten` and installed as `kitten`; renaming it is a
    separate, authorized change. Do not describe that rename as done until it is.

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
for byte. At that point assignments, calls, member access, struct and variant
literals, allocation, and indexing were still reported as not implemented rather than
rendered when a statement needed one. Phase 4D.6 added the translation unit emitter
under `selfhost/codegen/unit.ktn`, driven by `selfhost/unit.ktn`: the entry point rule,
the C entry point, and the assembly of the declaration sections, the literal pool, the
bodies, and the entry point into one complete translation unit, checked against the
reference's whole output byte for byte. Phase 4D.7 added calls: user-defined
functions, cross-module calls, and the built-ins `print`, `length`, `has`,
`substring`, and `read_file`, so a generated program can now produce output, which
the differential harness compiles, runs under GCC and Clang, and compares against the
reference program's output. Phase 4D.8 added member access and qualified variant
references, struct and variant literals, array and map creation, and indexing under
`selfhost/codegen/expressions.ktn`, so a program that constructs and inspects values
is now emitted byte for byte. Phase 4D.9 added the assignment expression, the last
unimplemented expression kind: plain assignment to every l-value form, map-entry
insertion, compound `+=` and `-=`, `++`/`--`, and string append, with the string and
map runtime helpers named in `names.ktn`. Every expression kind is now emitted, all
nine top-level fixtures under `tests/` compile, run, and match their expected output
through the Kitten backend under GCC and Clang. Phase 4E.1 added the module loader
under `selfhost/loader`, driven by `selfhost/load.ktn` and
`selfhost/loadunit.ktn`: it loads an absolute root path into the same ordered module
set as the Go loader, with the same canonical paths, ids, depth-first order, cycle
and duplicate handling, outside-root diagnostic, and lex/parse diagnostics. All
14 multi-file checker corpus programs, 20 targeted trees, and `tests/modules/` match
the reference loader byte for byte, and `tests/modules/` runs end-to-end through the
fully Kitten-written loader, lexer, parser, checker, and generator. Missing and
unreadable files and relative root paths are out of scope; Phase 4E.2 is blocked on
a SPEC decision for recoverable file input and a working-directory primitive. Phase
5.1 added the `argv()` built-in end to end in both pipelines: the generated C entry
point now takes `argc`/`argv`, saves them with `ktn_rt_set_args`, and user code sees
the vector through `ktn_rt_argv`. The phase also added a runtime test with zero, one,
and several arguments, including an empty argument and one containing spaces. Phase
5.2 added `eprint(...)` end to end: the runtime's `ktn_rt_eprint_*` helpers mirror
the print helpers, the newline helper flushes stdout before the diagnostic and then
flushes stderr, and the runtime fixture captures stdout and stderr separately through
both pipelines. Phase 5.3 added `exit(status)` and one shared flush path:
`ktn_rt_exit` flushes stdout then stderr and terminates, `ktn_rt_fail` routes through
it while keeping its message and status 1, and the runtime fixture checks stdout,
stderr, and the exact status, including POSIX low-eight-bit behavior. Phase 5.4
(`try_read_file` and the predeclared `ReadFileResult` union) is next and is the
type-system change; loader read-error parity, the driver, and the fixpoint remain
future phases.

### Adding a built-in

A built-in is not registered in one place. Every item below must change together,
or the Go and Kitten pipelines stop agreeing:

1. **SPEC.md** — Section 18.8 for the signature, argument rules, output, and errors;
   Section 16 when a deferred capability is removed; Section 19.3 for runtime ABI
   names; Section 28 for the Kitten generator's expansion.
2. **Go checker** — `checker/symbols.go` (`BuiltinKind`), `checker/checker.go`
   (`builtinFunctions`, including signature/return type), and `checker/expr.go`
   (`checkCall`) for any argument rule that is not the generic exact-arity path.
3. **Kitten checker** — `selfhost/checker/state.ktn` (`BuiltinKind`,
   `builtin_functions`, and any signature helper), plus
   `selfhost/checker/expr.ktn` (`check_call`).
4. **Semantic-model naming** — `selfhost/checker/semantics.ktn`
   (`builtin_kind_name`) and the Go mirror in
   `cmd/kitten/semantics_differential_test.go` (`builtinKindName`).
5. **Runtime names** — `runtime/runtime.go` for the Go generator's constants and
   `selfhost/codegen/names.ktn` for the Kitten generator's helpers.
6. **Runtime implementation** — `runtime/csrc/kitten_runtime.h` and
   `runtime/csrc/kitten_runtime.c`, kept under the MIT runtime notice. State
   flushing and failure behavior explicitly.
7. **Go generator** — `codegen/emit.go` (call dispatch and any expansion helper);
   add or use a runtime name constant rather than a literal C name.
8. **Kitten generator** — `selfhost/codegen/expressions.ktn` (call dispatch and
   expansion) and `selfhost/codegen/names.ktn`.
9. **Harnesses and fixtures** — checker corpus valid and invalid uses, expression
   corpus, call/unit whole-unit cases, a runtime fixture when behavior is
   observable, and `selfhost/codegen/README.md` counts.
10. **Documentation** — `README.md`, `AGENTS.md`, and the relevant
    `selfhost/*/README.md`; update measured counts, never invent them.

None of the self-hosted components is part of the production pipeline: `kitten
build` and `kitten run` still use the Go lexer, parser, checker, generator, and
loader, which remain authoritative. Their deliberate differences (byte-oriented
positions, no program-selected exit status, an absolute-root Kitten loader with
fatal missing-file reads, and no production driver integration) are documented in
SPEC.md
Sections 25 to 28, `selfhost/lexer/README.md`, `selfhost/parser/README.md`,
`selfhost/checker/README.md`, `selfhost/codegen/README.md`, and
`selfhost/loader/README.md`.

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
   unit assembly, checked against the reference's whole output; Phase 4D.7 added
   function calls and built-ins, with generated programs compiled, run, and compared
   against the reference's; Phase 4D.8 added member access, qualified variant
   references, struct and variant literals, array and map creation, and indexing;
   Phase 4D.9 added assignment, map-entry insertion, compound assignment,
   increment/decrement, and string append, so every expression kind is emitted and
   all nine top-level fixtures now compile and run through the Kitten backend;
   Phase 4E.1 added the Kitten-written module loader for absolute roots, with the
   same order, ids, paths, and diagnostics as Go over the loader corpus, and a
   fully Kitten-written path through `tests/modules/`. Missing-file diagnostics and
   relative roots are deferred to 4E.2, which needs a SPEC decision; Phase 5.1
   added `argv()` in both pipelines and the `argc`/`argv`-capturing C entry point;
   Phase 5.2 added `eprint(...)`, the flushing stderr helpers, and separate-stream
   runtime coverage; Phase 5.3 added `exit(status)`, a shared flush-and-exit path,
   and exact status/stream runtime coverage).
3. Rewrite the compiler in Kitten and verify it through an actual bootstrap. This
   is independence from Go.
4. Explore an LLVM IR or native assembly/object-code backend, and eventually move
   the runtime off C as well. This is independence from C.
5. Grow a broader standard library and an ecosystem for command-line tools first,
   then GUI, servers, and games. This is independence from other languages'
   ecosystems, and it includes the whole `purr` toolchain: one-command install of
   the toolchain, the language server and editor and shell completion, `purr new`
   scaffolding for binaries and libraries, dependency management, the registry,
   publishing, and the standard library that makes them useful.

The initial compiler is written in Go. The long-term goal is for Kitten to
compile its own compiler, to stop depending on C, and to stand on its own
tooling. Those are three separate goals that do not depend on each other;
`VISION.md` states them in full. This is a direction for future design, not
permission to add language features or phases early.
