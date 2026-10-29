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
   `gofmt` (or `gofmt -l .`) when Go code changed. Build the CLI with
   `go build -o /tmp/purr ./cmd/purr` when the command or its packaged code
   changed. The repository-root `purr/` source directory prevents the default
   output name `purr`.
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
    are not wired into `purr build` or `purr run`, which keep using the Go
    implementation. Do not make the production pipeline depend on them without
    explicit authorization.

## Process

11. Make small, focused changes. One coherent change per commit, with a clear
    message.
12. Do not begin future phases without explicit authorization. Phase 7.0
    authorizes the **design** of Goal 2 (a native backend and a runtime with no
    C), but not its implementation; implementation needs a separate, later
    authorization after the design is approved. Do not implement self-hosting
    beyond the proven C-level fixpoint, a native backend, or standard-library
    features without explicit authorization.
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

17. Write documentation against `purr`. The compiler binary is built from
    `cmd/purr` and installed as `purr`; the tool rename is done. The language
    remains **Kitten** and source files remain `.ktn`.

## Current phase

Phase 1 delivered the project skeleton, the provisional specification, the
example programs, and the lexer. Phase 2A delivered the parser and the abstract
syntax tree. Phase 2B delivered semantic analysis and the type checker. Phase 2C
delivered the C code generator, the C runtime, and native compilation through
`purr build` and `purr run`. Phase 3A delivered dynamic strings:
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
a differential corpus of 772 inputs in `cmd/purr`. Phase 3H gave strings a
stored byte length alongside the bytes, so `length` and indexing are
constant-time while the bytes stay NUL-terminated; the observable string
semantics did not change, and the Kitten-written lexer now scans linearly. Phase
4A added the next self-hosted component: a parser under `selfhost/parser`, driven
by `selfhost/parse.ktn`, that reproduces the Go reference parser's grammar,
precedence, tree, and diagnostics, and is checked against it by a differential
corpus of 262 inputs in `cmd/purr`. Phase 4B added a semantic checker under
`selfhost/checker`, driven by `selfhost/check.ktn`, that reproduces the Go
checker's passes, rules, and diagnostics. Phase 4C exposed the checker's resolved
semantic model — the types, symbols, variants, and declarations a generator reads
— for a future code generator. Phase 4D.1 began the C code generator written in
Kitten: its C naming, C type mapping, string-literal escaping, and bounded output
accumulation under `selfhost/codegen`, exposed by `selfhost/codegen.ktn` and
checked against the Go generator in `cmd/purr`. Phase 4D.2 added the declaration
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
15 multi-file checker corpus programs, 33 targeted trees, and `tests/modules/` match
the reference loader byte for byte, and `tests/modules/` runs end-to-end through the
fully Kitten-written loader, lexer, parser, checker, and generator. Missing and
unreadable files were completed by 5.5; relative root paths remain out of scope
because the self-hosted runtime has no working-directory primitive. Phase
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
stderr, and the exact status, including POSIX low-eight-bit behavior. Phase 5.6
added the Kitten-written driver `selfhost/driver.ktn` and the temporary POSIX-sh
wrapper `selfhost/driver/purr-bootstrap.sh`: the driver takes one source path,
emits byte-identical C on stdout, reports diagnostics on stderr, and uses exit
statuses 0/1/2; the wrapper builds and runs fixtures and `tests/modules/` under
GCC and Clang. The implementation order changed: `ReadFileResult`/`try_read_file`
(5.4) and loader read-error parity (5.5) were postponed until after the fixpoint
(5.7). Both are now complete: `try_read_file` is implemented in both pipelines,
and the loader reports missing, unreadable, and directory paths with the
reference's `cannot read source file: ` diagnostic. Phase 5.7 reached the C-level fixpoint: Go builds
stage 1 from `selfhost/driver.ktn`, stage 1 builds stage 2, stage 2 builds stage 3,
and the generated C for `driver.ktn` is byte-identical across Go and all three
stages; stage 2 also reproduces the Go reference for all nine fixtures and
`tests/modules/`. The stage binaries were byte-identical in the measured run, but
the claim rests on the C, not the binaries.

Phase 6.0 renamed the command-line tool from `kitten` to `purr`: the source
directory is `cmd/purr`, the built binary is `purr`, CLI text says `purr`, the
temporary wrapper is `selfhost/driver/purr-bootstrap.sh` with
`PURR_BOOTSTRAP_DRIVER` and `PURR_RUNTIME_DIR`, and tests, build commands, and
documentation use the new name. The language remains **Kitten**, source files
remain `.ktn`, and generated C, runtime file names, `ktn_` identifiers, and
`#include "kitten_runtime.h"` are unchanged.

Phase 5.4b implemented the adopted `ReadFileResult`/`try_read_file` design in the
C runtime and the Go pipeline: the predeclared union and on-use prelude, the Go
checker/generator, and Go-compatible errno text. Phase 5.4c mirrored it in the
Kitten-written checker, semantic model, and generator: the checker appends the
fixed prelude as module `n` on a syntactic trigger, reserves `ReadFileResult`,
and registers the builtin after collecting the type; the generator emits the
normal union and a non-static helper and lowers the call to it. The shared
checker, semantic, expression, function, unit, declarations, and loader corpora
grew, and a both-pipeline runtime fixture covers existing, empty, missing,
directory, and unreadable paths under both compilers. Phase 5.5 completed loader
read-error parity: the Kitten loader uses `try_read_file`, returns the canonical
path with no position and the `cannot read source file: ` prefix, keeps the
outside-root check before the read, and matches the Go loader byte for byte,
including through stage 1 and stage 2. Phase 8.1 implemented the adopted
defined-integer-arithmetic design in both pipelines and the C runtime:
`+ - *`, unary `-`, `+=`, `-=`, `++`, and `--` wrap modulo 2^64; `/` and `%`
have defined truncation/remainder rules; zero divisors fail with
`integer division by zero`; `INT64_MIN / -1` wraps and `% -1` is zero; decimal
literals above `INT64_MAX` are rejected with `integer literal out of range`;
and generated C calls the MIT `ktn_rt_i64_*` helpers rather than evaluating
signed overflow. Phase 8.2 added `& | ^ ~ << >>`, C#-style precedence and
arithmetic shifts, checked shift counts, and lowercase `0x`/`0b` literals with
64-bit bit-pattern semantics and identical lexical/checker errors. Phase 8.3a
renamed every `byte` identifier in Kitten source, fixtures, and embedded test
source to `octet`, with no language change. Phase 8.3b added `byte` as a reserved
unsigned 8-bit type, `byte[]`, byte comparisons and printing, and the four
conversions `int_from_byte`, `byte_from_int`, `string_to_bytes`, and
`bytes_to_string`. Phase 8.4 added the recoverable file and directory built-ins,
their `FileResult` and `ListDirectoryResult` prelude unions, Go-compatible error
text, and the generalized on-use prelude. Phase 8.5 added the recoverable
`try_run` process-launch built-in and its `RunResult` prelude union, with direct
`execve` semantics, inherited environment/working directory/descriptors, status
and signal reporting, and pre-fork stream flushing. Phase 8.6 added the
recoverable `try_current_directory` built-in and its `CurrentDirectoryResult`
prelude union, using the kernel's `getcwd` path, ignoring `$PWD`, and growing its
buffer on `ERANGE`. Phase 8.7 added the compiler-reserved `<runtime>` synthetic
module and its harness-only injection hook, runtime-only intrinsics, C lowering,
and the MIT `runtime/ktn/memory.ktn` helper source. The primitive checklist is
complete. Phase 9.0 produced the design-only `proposals/purr-in-kitten.md` for
rewriting `purr`, `purr new`, and `purr.toml`; it is **ADOPTED as the plan;
implementation complete through 9.9**. Phase 9.1 implemented its first step, the `try_getenv`
built-in and predeclared `EnvResult` union, and Phase 9.2 implemented
`try_run_capture` and `CapturedRunResult`, Phase 9.3 implemented
`try_file_info` and `FileInfoResult`, Phase 9.4 implemented
`try_create_temp_directory` and `TempDirectoryResult`, Phase 9.5 embedded the C
runtime sources in `purr/embedded.ktn`, Phase 9.6 implemented the `purr.toml`
subset parser in `purr/manifest.ktn`, Phase 9.7 implemented the `purr new`
module and probe in `purr/new.ktn` and `purr/new_probe.ktn`, and Phase 9.8a
implemented the `purr-kt` entry point, help/usage, and `new` dispatch in
`purr.ktn` and `purr/cli.ktn`, in both pipelines, including the dedicated
`purr new -h`/`--help` text. Phase 9.8b added the
in-process `build` command in `purr/build.ktn`, and Phase 9.8c implemented the
`run` command in `purr/cli.ktn` with focused both-pipeline tests. Project mode
then added `purr/project.ktn`, current-directory manifest discovery, and
`target/<name>` build/run for binary projects; library builds remain the
approved deferred error. The 9.8c parity suite is now complete: the cases from
`proposals/purr-in-kitten.md` Section 12 are covered across
`cmd/purr/purr_build_test.go`, `purr_build_fake_test.go`, `purr_run_test.go`,
`purr_project_test.go`, and `purr_kt_test.go`, including the last path-only
loader failure (unreadable source), loader import cycles and generated-C
identity, the `.ktn`/`a.out` default-output fallback, lex-error diagnostics,
`pkg-config` absent and failing modes, and `CC`/`cc`/`gcc`/`clang` discovery.
Phase 9.9 then completed the freeze and name swap: the suite's freeze run
passed under the discovered `cc`, `CC=gcc`, and `CC=clang`;
`TestKittenPurrReleaseBuild` verifies the documented release flow from a clean
git checkout (bootstrap `go build -o ./cmd/purr`, then build `purr.ktn`, install
the result as `purr`, and use it for `new` and a project `run`); the Go CLI is
frozen as the bootstrap and testing reference, and the Kitten-written sidecar
takes over the `purr` name — `purr-kt` survives only as the harness label for
pipeline-specific test builds. Library object builds (Proposal Section 12.3)
remain the approved deferred case.
`try_find_program` is
intentionally dropped: PATH search will be Kitten code over `try_getenv` and
`try_file_info`.
Phase 9.10 then resolved the deferred relative loader roots:
`selfhost/loader/resolve_root` resolves an absolute and a relative root exactly
as the reference's `canonicalPath` does with `filepath.Abs`, using
`try_current_directory` (the kernel's `getcwd`, ignoring `$PWD`) for the
relative case; `load_program`, the `load.ktn`/`loadunit.ktn` probes, and
`selfhost/driver.ktn` accept relative roots, and the differential corpus grew to
165 relative spellings plus `tests/modules/` and the whole unit pipeline in both
root forms. File writing now unblocks `purr new` and process launch
makes `purr run`-style tooling writable in Kitten, both as separate later
phases. A SPEC cleanup of Go/C-flavored wording remains a later candidate. The
native backend and a standard library remain unauthorized.

### Generated runtime embedding

`purr/embedded.ktn` is generated from `runtime/csrc/kitten_runtime.h` and
`runtime/csrc/kitten_runtime.c` by `tools/embed_runtime.ktn`. Regenerate it from
the repository root with:

```sh
purr build tools/embed_runtime.ktn -o /tmp/embed_runtime
/tmp/embed_runtime .
```

Any change under `runtime/csrc/` requires regenerating and committing
`purr/embedded.ktn`; the three-way drift test regenerates it through the Go
pipeline, the Kitten pipeline, and an independent Go reference and fails if the
committed file differs. The embedded strings are MIT-licensed runtime sources
with their notices preserved; the generated file's own header records the
toolchain license.

Because `purr/` is now a source directory, `go build ./cmd/purr` cannot write a
repository-root binary named `purr`; build it to an external path or install
location, for example `go build -o /tmp/purr ./cmd/purr`.

### Manifest parser

`purr/manifest.ktn` is the plain-Kitten parser for the strict `[package]`
manifest subset. It has no imports and performs no file I/O; its public API is
`parse_manifest(text) -> ManifestResult`. `purr/manifest_probe.ktn` is the
test/CLI-shaped probe; it lives under `purr/` because the loader only permits
imports below the root source's directory, so `tools/manifest_probe.ktn` could
not import `../purr/manifest.ktn`.

The pinned, randomized, both-pipeline, performance, and sanitizer tests live in
`cmd/purr/manifest_test.go`; the independent Go oracle is in
`cmd/purr/manifest_oracle_test.go`. Additional keys, `[dependencies]`, and a
manifest writer remain deferred and must grow additively.

### Project creation module

`purr/new.ktn` implements `new_project(name, library)` and the `NewResult`
union; `purr/new_probe.ktn` is the command-shaped test driver. The module
imports `manifest.ktn` for name validation and uses only `try_file_info`,
`try_create_directory`, `try_write_file`, and `try_remove` for filesystem work.
It tracks only paths it created, removes them in reverse order on failure, never
recurses, and reports cleanup notes. Modes honor the umask; no chmod exists.

The Go CLI does not dispatch `new` yet, so `purr/new_probe.ktn` built with the
Go pipeline remains the reference. `purr-kt` dispatches `new` with the probe's
argument grammar, messages, and statuses. Any change to the templates or cleanup
behavior must update the both-pipeline tests in `cmd/purr/new_test.go` and the
purr-kt parity tests in `cmd/purr/purr_kt_test.go`.

### Kitten `purr` sidecar

`purr.ktn` is the repository-root entry point; `purr/cli.ktn` parses argv and
imports `new.ktn`, `build.ktn`, and `project.ktn`. `purr/build.ktn` imports the
in-process loader, lexer, parser, checker, code generator, loader path helpers,
and `embedded.ktn`; it does not import the probe drivers that define their own
`main`. The binary name is always `purr-kt`; the Go CLI remains the authoritative
`purr`. Help and usage name the program `purr` deterministically, never argv[0].

`purr-kt` supports `help`, `new`, `build`, and `run`. `new -h`/`--help` prints
the dedicated project-scaffolding text from `purr/new.ktn`. The build path
mirrors the Go CLI's argument grammar, compiler/PATH/pkg-config discovery,
compiler argv, output validation, diagnostics, and statuses, with the two
documented deviations for combined stream order and signal status. `run`
shares that argument grammar, rejects `-o`, builds into a `purr-run.` temporary
directory, executes with `try_run`, propagates the child's status, and cleans up
non-recursively with notes. A signaled child therefore reports `128 + signal`
through `try_run`; the Go CLI's `ExitCode()` path reports `-1`, observed as 255
on POSIX. That difference is unavoidable while the documented `try_run` mapping
remains. Without a source argument, no output option, and a `purr.toml` in the
current directory, `build` and `run` enter binary project mode: they use the
manifest and `src/main.ktn`, write `target/<name>`, and run that executable.
Library projects report the approved deferred errors. The run feature and
project mode are implemented, the 9.8c parity suite is complete, and after the
9.9 freeze the binary built from `purr.ktn` takes over the `purr` name while
the Go CLI at `./cmd/purr` stays frozen as the bootstrap and testing
reference. Tests must keep the Go CLI behavior unchanged.

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
   `cmd/purr/semantics_differential_test.go` (`builtinKindName`).
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

A **prelude built-in** additionally changes `checker/prelude.go` and
`selfhost/checker/prelude.ktn` (source blocks, syntactic trigger, late
registration, and reserved names), both generators' helper emission, and the
prelude subset and whole-unit corpora.

A **runtime intrinsic** additionally changes the synthetic-module table and
`<runtime>` hook in both checkers, the runtime-only resolution gate, both
generators' lowering, the MIT `runtime/csrc` header helpers, and the runtime
module fixture. Intrinsic names are never reserved in user modules.

For a new **operator or primitive type**, this built-in checklist is not enough:
also update both lexers' token kinds and scans, both parsers' precedence or type
grammar, both checker type tables and operator rules, both code generators' C
type/operator lowering, the semantic-model type rendering, and the lexer,
parser, checker, semantic, expression, function, unit, and call corpora. A
prelude type follows the prelude rows above in addition to these.

None of the self-hosted components is part of the production pipeline: `purr
build` and `purr run` still use the Go lexer, parser, checker, generator, and
loader, which remain authoritative. The Kitten-written driver and its POSIX
wrapper are test infrastructure for the fixpoint, not the production CLI. Their
deliberate differences (byte-oriented positions, an absolute-root Kitten loader,
and no production driver integration) are
documented in SPEC.md Sections 25 to 28, `selfhost/lexer/README.md`,
`selfhost/parser/README.md`, `selfhost/checker/README.md`,
`selfhost/codegen/README.md`, `selfhost/loader/README.md`, and
`selfhost/driver/README.md`.

The C-level self-hosting fixpoint is reached, but full independence from Go is
not: Go still builds stage 1. The design of the zero-dependency native toolchain
(Goal 2) lives in `proposals/zero-dependency-toolchain.md`, and **Phase 10.0
approved its Section 14 answers, authorizing implementation**: the typed-IR,
stack-first, direct-ELF backend is approved; unsafe operations stay only in the
compiler-reserved `<runtime>` module; the first shippable GC is non-moving
conservative mark-sweep; the native runtime implements `%g` exactly; and after
native parity the Go source is removed entirely — an accepted loss of the
independent cross-check oracle, with the Go files frozen until that parity is
proven. The staged native-backend work is in that proposal's implementation
order; a complete standard library remains not designed. Do not claim
zero dependencies, a native fixpoint, or Go removal before the proposal's
staged evidence exists.

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
   fully Kitten-written path through `tests/modules/`. Relative roots were
   deferred then because there was no working-directory primitive, and were
   accepted in Phase 9.10; Phase 5.1
   added `argv()` in both pipelines and the `argc`/`argv`-capturing C entry point;
   Phase 5.2 added `eprint(...)`, the flushing stderr helpers, and separate-stream
   runtime coverage; Phase 5.3 added `exit(status)`, a shared flush-and-exit path,
   and exact status/stream runtime coverage; Phase 5.6 added the Kitten-written
   driver and temporary POSIX-shell wrapper, with byte-identical C and native
   fixture execution under GCC and Clang; Phase 5.7 reached the C-level fixpoint;
   Phase 5.4c brought `try_read_file` and `ReadFileResult` to the Kitten-written
   checker and generator; Phase 5.5 closed loader read-error parity; Phase 8.1
   made integer arithmetic and decimal literal ranges defined in both pipelines
   and the C runtime; Phase 8.2 added bitwise/shift operators and hex/binary
   literals; Phase 8.3a freed the `byte` identifier by renaming it to `octet`
   in source and fixtures; Phase 8.3b added the `byte` type, `byte[]`, and
   explicit byte/string conversions; Phase 8.4 added recoverable file and
   directory operations and their on-use result unions; Phase 8.5 added
   recoverable process launch and `RunResult`; Phase 8.6 added the current
   directory query and `CurrentDirectoryResult`; Phase 8.7 added the
   compiler-reserved `<runtime>` module and its intrinsic mechanism).
3. Rewrite the compiler in Kitten and verify it through an actual bootstrap. This
   is independence from Go. **C-level fixpoint reached** (5.7): Go builds stage 1,
   stage 1 builds stage 2, stage 2 builds stage 3, and the C for `driver.ktn` is
   byte-identical across Go and all stages. Full independence remains open because
   Go still builds stage 1; relative loader roots and a Go-free bootstrap install
   remain open work, not part of this finish line yet.
4. Explore an LLVM IR or native assembly/object-code backend, and eventually move
   the runtime off C as well. This is independence from C. **Authorized** (Phase
   10.0): the typed-IR/stack-first/direct-ELF design is approved and
   implementation may begin in the proposal's staged order; nothing is
   implemented yet.
5. Grow a broader standard library and an ecosystem for command-line tools first,
   then GUI, servers, and games. This is independence from other languages'
   ecosystems, and it includes the whole `purr` toolchain: one-command install of
   the toolchain, the language server and editor and shell completion, `purr new`
   scaffolding for binaries and libraries, dependency management, the registry,
   publishing, and the standard library that makes them useful.
6. One-line installer (rustup-style) that installs `purr` and the whole
   toolchain: components, update model, completion, and language server. **Planned,
   not designed**; design it after the zero-dependency path is designed so it does
   not advertise a toolchain that still needs Go, C, or libgc.

The initial compiler is written in Go. The long-term goal is for Kitten to
compile its own compiler, to stop depending on C, and to stand on its own
tooling. Those are three separate goals that do not depend on each other;
`VISION.md` states them in full. This is a direction for future design, not
permission to add language features or phases early.
