# Minimum driver capabilities for a Kitten-written compiler

> **ADOPTED in principle; implementation is staged.**
>
> The human approved the eight decisions below. Phases 5.1 (`argv`), 5.2
> (`eprint`), 5.3 (`exit`), and 5.6 (the Kitten driver and wrapper) are
> implemented, and Phase 5.7 reached the C-level fixpoint. The order was revised
> in Phase 5.6: `try_read_file` and the loader read-error parity are postponed
> until after the fixpoint. Until then a missing/unreadable file is fatal through
> `read_file` and is not matched.
>
> Status: adopted, Phase 5.0 proposal; Phase 5.7 done; 5.4/5.5 next. Date:
> 2026-10-18.
>
> **Adopted decisions**
>
> 1. `ReadFileResult { Ok{text}; Failed{message} }` with `try_read_file(path)` is
>    approved, in a later phase.
> 2. `argv() -> string[]` is approved, with the program name at index 0.
> 3. A POSIX-shell wrapper is acceptable for the first fixpoint. It is temporary,
>    must not be Go, and is replaced by `purr` later. The Kitten compiler writes C
>    to stdout, diagnostics to stderr, and status via `exit`; the wrapper owns
>    `-o`, temporary files, `cc`, and `run`.
> 4. Read-error parity (4E.2) is completed before any README self-hosting claim.
>    **Order revised in Phase 5.6:** it is postponed until after the fixpoint.
> 5. Parity covers the common errnos (not found, permission denied,
>    is-a-directory) plus a generic fallback, with a Go-compatible errno mapping.
> 6. `argv`, `eprint`, `exit`, and `try_read_file` are permanent built-ins.
> 7. Linux is verified; the design must not break macOS by construction, and
>    untested macOS support is not claimed.
> 8. The Kitten driver parses the source path only.

## 1. Scope and target

The target is **independence from Go** (VISION.md, "The three independences",
Goal 1): a compiler written in Kitten, compiled by itself, producing output
identical to the Go reference. The native backend, the standard library, `purr`,
package management, and the registry are separate goals and are out of scope.

This proposal asks for the smallest language/runtime surface that lets a
Kitten-written compiler:

1. accept a source path;
2. load the root module and its imports;
3. check and emit C;
4. report diagnostics separately from generated C;
5. signal success or failure to a caller;
6. report a missing or unreadable source file as a normal loader diagnostic.

It also recommends a deliberately small **wrapper architecture** so that file
writing and process launch are not language capabilities in the bootstrap.

The proposal is written against the current tree at
`b48135c feat: add Kitten-written module loader for absolute roots`.

## 2. Audit: what the Go driver does today

### 2.1 End-to-end behavior

`cmd/purr/main.go`:

1. Reads `os.Args[1:]` and dispatches `help`, `build`, or `run`
   (`main.go:57-83`). No arguments, an unknown command, a missing source, a
   wrong extension, or an unknown option is a usage error with exit status 2
   (`main.go:64-129`, `main.go:131-173`).
2. Parses `-o`/`--output`; `run` rejects `-o` (`main.go:91-129`,
   `main.go:159-168`).
3. Calls `loader.Load(sourcePath)` (`main.go:211-215`). The loader reads files,
   resolves imports relative to the importing file, canonicalizes paths, and
   lexes/parses each module (`loader/loader.go:44-126`).
4. Calls `checker.CheckModules` (`main.go:216-219`).
5. Calls `codegen.Generate` (`main.go:220-223`).
6. Discovers the C compiler from `CC`, then `cc`, `gcc`, or `clang`
   (`toolchain.go:18-37`).
7. Creates a temporary build directory and writes `program.c`,
   `kitten_runtime.c`, and `kitten_runtime.h` (`main.go:230-247`).
8. Resolves the output path absolutely (`main.go:249-252`), gets `pkg-config`
   flags for Boehm GC or falls back to `-lgc` (`toolchain.go:39-60`), and
   invokes the C compiler with `-std=c11 -Wall -Wextra` (`main.go:254-274`).
9. Verifies that the compiler really produced a non-directory output, and
   forwards compiler warnings to stderr (`main.go:276-288`).
10. For `run`, creates another temporary executable, executes it with inherited
    stdin/stdout/stderr, and propagates its exit code (`main.go:158-200`).

The C compiler is the backend today; step 9 above is the end of the pipeline.
Error reporting goes to stderr, including usage, loader/checker/codegen
diagnostics, and C compiler output (`main.go:304-331`). The process exit codes
are 0 success, 1 compilation/execution failure, and 2 usage error
(`main.go:50-55`).

### 2.2 What a Kitten driver must do versus delegate

| Step | Bootstrap owner |
| --- | --- |
| Parse its own arguments | Kitten driver (`argv` capability needed) |
| Load modules and resolve imports | Kitten loader already exists |
| Lex, parse, check, emit C | Kitten stages already exist |
| Write diagnostics separately from C | Kitten driver (`stderr` capability needed) |
| Signal success/failure | Kitten driver (`exit` capability needed) |
| Create temporary directory | External wrapper |
| Write generated C and runtime files | External wrapper (redirect stdout; copy runtime) |
| Discover and invoke the C compiler | External wrapper |
| Validate/link the output executable | External wrapper |
| Compile-and-run mode | External wrapper (compile, then execute) |

The Kitten compiler can therefore be a **filter**: on success it writes one C
translation unit to stdout and exits 0; on failure it writes diagnostics to
stderr and exits nonzero. A POSIX-shell wrapper (not Go) can supply the source
path, redirect stdout to a temporary `program.c`, copy the runtime sources next
to it, invoke `cc`, and propagate the result. That is still a compiler written
in Kitten; the wrapper is the host-integration layer, not the compiler.

### 2.3 Current built-in registration

Built-ins are hard-coded in both implementations:

- Go: `checker/checker.go:274-307` (`builtinFunctions`),
  `checker/symbols.go:34-55` (`BuiltinKind`), with special call checking in
  `checker/expr.go:460-498`.
- Kitten: `selfhost/checker/state.ktn:488-574` (`builtin_functions`),
  `selfhost/checker/state.ktn:260-266` (`BuiltinKind`), and dispatch in
  `selfhost/checker/expr.ktn`.
- Go code generation: `codegen/emit.go` (call expression) and
  `codegen/names.go` (runtime names).
- Kitten code generation: `selfhost/codegen/expressions.ktn:700-725` (call
  dispatch) and `selfhost/codegen/names.ktn` (runtime names).

Any new built-in or built-in return type must be added in **both** stacks and
in both differential harnesses, or the two compilers stop agreeing.

### 2.4 Current runtime surface

`runtime/csrc/kitten_runtime.h` exposes GC initialization/allocation, strings,
arrays, maps, fatal failure, `read_file`, and print helpers. There is no args
API, no non-fatal read, no program-facing stderr writer, and no program-selected
exit. `ktn_rt_fail` writes to stderr and calls `exit(1)`
(`kitten_runtime.c:45-63`). `ktn_rt_read_file` is fatal on every failure
(`kitten_runtime.h:248-262`).

Generated entry points are `int main(void)`, call `ktn_rt_init()`, then the
Kitten `main` (`codegen/codegen.go:606-617`,
`selfhost/codegen/unit.ktn:148-159`). Passing argv therefore reaches both the
runtime and both unit emitters.

### 2.5 Current self-hosted drivers

`selfhost/load.ktn` and `selfhost/loadunit.ktn` read an absolute root path from
the fixed file `input.txt`; the loader requires an absolute root because the
runtime has no working directory (`selfhost/loader/README.md`). They cannot
report a missing source file as a loader error because `read_file` is fatal
(`selfhost/loader/loader.ktn`, read call; SPEC.md Sections 22.2 and 22.9).

The self-hosted checker has built-ins in
`selfhost/checker/state.ktn`, and the self-hosted generator dispatches them in
`selfhost/codegen/expressions.ktn`. Both need the same additions as Go.

### 2.6 Other deferred-decision lists

The remaining deferred lists were checked for driver requirements:

- **20.7 strings** — no string/number conversions are needed. Diagnostics already
  print numbers as separate arguments, and a lexer produces lexemes with
  `substring`.
- **21.10 arrays** — no append, slicing, or iteration helper is required. The
  loader grows its module array by copying, as it already does, and `argv()` can
  return an exact-size array.
- **23.10 enums/unions** — no enum/integer conversion or union comparison is
  needed. The proposed read result is inspected with the existing exhaustive
  `match`.
- **24.9 maps** — no map iteration or optional map value is needed by the driver.
- **25.6/26.6/27.6** — these record extension gaps of the lexer, parser, and
  checker, not new driver capabilities; the missing one is the compiler driver
  itself.
- **28.12** — records writing/building, recoverable file input, relative roots,
  and the driver capabilities. This proposal addresses the language/runtime
  subset and leaves writing/building to the wrapper.

No capability beyond the items below was found necessary for the bootstrap.

## 3. Proposed capabilities

Each item states the need, proposed SPEC wording, runtime ABI, checker/codegen
impact, risks, and alternatives. Names are part of the proposal and may be
amended.

### 3.1 Program arguments: `argv()`

**Need.** The current Go driver takes the source path from `os.Args`
(`cmd/purr/main.go:57-83`, `98-128`). The Kitten drivers instead read
`input.txt` (`selfhost/load.ktn`, `selfhost/loadunit.ktn`), which cannot become a
real compiler CLI without a wrapper renaming or copying inputs. A Kitten
compiler must accept at least the root source path, and the eventual `run` mode
and `-o` handling need the full argument vector. This is directly required by
Goal 1 and by VISION.md's CLI-first premise (`VISION.md:50-64`).

**Proposed SPEC wording** (to replace the corresponding item in Section 16 and
to be added to Section 18.8):

> `argv() -> string[]` returns the process argument vector as a new array of
> strings, in order, including the program name at index 0. The strings are
> snapshots and cannot be used to change the process arguments. The array always
> contains at least the program name, which is whatever the host reported, not
> necessarily an absolute path.

**Runtime ABI.**

```c
/* Called by the generated C entry point after ktn_rt_init. */
void ktn_rt_set_args(int argc, char **argv);

/* Returns a new ktn_array whose elements are const ktn_string *. */
ktn_array *ktn_rt_argv(void);
```

The entry point becomes:

```c
int main(int argc, char **argv) {
    ktn_rt_init();
    ktn_rt_set_args(argc, argv);
    ktn_fn_main();
    return 0;
}
```

`ktn_rt_argv` allocates a `ktn_array` of `argc` pointer-sized elements and
converts each C string to a GC-managed Kitten string with its byte length. The
raw `argv` pointers remain valid for the process lifetime; each call returns a
fresh array (the driver normally calls it once). Empty arguments are ordinary
empty strings.

**Checker/codegen impact.**

- New `BuiltinKind.Argv`, return type `string[]`, no parameters.
- Go and Kitten checkers register it; both emitters map a call to
  `ktn_rt_argv()`, with the runtime name added to `codegen/names.go`,
  `selfhost/codegen/names.ktn`, and `runtime/runtime.go`.
- Both unit emitters change the C entry point signature and add
  `ktn_rt_set_args(argc, argv)`, which changes the whole-unit golden
  comparisons; the harnesses are differential, so they update naturally.
- Indexing the returned array already works through the existing `IndexExpr`
  emitter; `string[]` maps to `ktn_array *`, and element access casts to
  `const ktn_string *`.

**Risks.** Index 0 convention differs from a language-level `args()` that
excludes the program name; choose one and document it. Program names may not be
canonical. A wrapper may pass an absolute root path even though `argv()[0]` is
whatever the OS reported. The runtime array is mutable, but that cannot mutate
the process vector.

**Alternatives.** A split `argc()`/`argv(index)` pair is smaller in the runtime
but more awkward in Kitten; an array is the natural collection. Environment
variables are not needed for the bootstrap. A fixed `input.txt` avoids argv but
is not a real compiler CLI and forces the wrapper to stage input.

**Recommendation: adopt `argv()` including the program name.**

### 3.2 Writing to stderr: `eprint(...)`

**Need.** The Go driver puts usage, loader/checker/codegen diagnostics, and C
compiler output on stderr (`cmd/purr/main.go:64-83`, `304-331`). The Kitten
driver prints everything with `print`, which is stdout. If diagnostics share
stdout with generated C, a wrapper cannot separate a translation unit from an
error message. `purr run` also needs to keep the compiled program's stdout
clean. SPEC.md Section 16 currently records the absence of stderr access
(`SPEC.md:597-600`), and Section 25.4 documents it for the lexer driver.

**Proposed SPEC wording** (Section 18.8, next to `print`):

> `eprint(...)` accepts one or more values of `int`, `float`, `bool`, or
> `string`, writes them in order to standard error separated by single spaces,
> and terminates the line with a newline. It returns `void`. Calling it with no
> arguments, or with a struct, array, enum, union, or map value, is an error.
> Its output is not affected by `print` and does not flush stdout.

**Runtime ABI.** Mirror the print helpers:

```c
void ktn_rt_eprint_int(int64_t value);
void ktn_rt_eprint_float(double value);
void ktn_rt_eprint_bool(bool value);
void ktn_rt_eprint_string(const ktn_string *value);
void ktn_rt_eprint_space(void);
void ktn_rt_eprint_newline(void);
```

Each writes to `stderr`; `ktn_rt_eprint_newline` writes `\n` and flushes stderr
so a fatal error or a process exit cannot lose the diagnostic.

**Checker/codegen impact.** New `BuiltinKind.Eprint`, variadic with the same
allowed types as `print`; Go and Kitten checkers share the existing variadic
rule but need a distinct kind so the code generator chooses the stderr helpers.
Both emitters expand `eprint` exactly like `print` with `ktn_rt_eprint_*`.
`checkVariadicCall`'s "cannot print" wording should become kind-aware (or say
"cannot write") so diagnostics remain accurate.

**Risks.** Two parallel helper sets; a future unified formatting layer could
collapse them. Flushing stderr on every line is slightly slower but correct for
diagnostics. Programs can now write to stderr, so tests that previously expected
empty stderr must be explicit about intent.

**Alternatives.** A single `write(int fd, string)` is lower-level and needs
numeric formatting in Kitten, which the language does not have. A string-only
`eprint` would require conversion helpers that do not exist. Mirroring
`print` is the smallest consistent design.

**Recommendation: adopt `eprint(...)`.**

### 3.3 Program-selected exit status: `exit(status)`

**Need.** The Go driver returns 0/1/2 (`cmd/purr/main.go:50-55`) and `run`
propagates the child's status (`main.go:192-199`). Without a language-level
exit, a Kitten compiler can only succeed or die through a runtime fatal error;
a wrapper cannot distinguish "usage error", "source error", and "success" in
the intended way. SPEC.md Section 16 records the absence of chosen exit status
(`SPEC.md:597-600`); Section 25.4 records the same limitation for the lexer
driver.

**Proposed SPEC wording** (Section 18.8 and Section 16):

> `exit(status)` takes exactly one `int` and terminates the process immediately
> with that status, after flushing standard output and standard error. It does
> not return and cannot be used as a value. On POSIX hosts only the low eight
> bits of the status are observable. The compiler driver uses 0 for success, 1
> for a compilation or execution failure, and 2 for a usage error.

**Runtime ABI.**

```c
_Noreturn void ktn_rt_exit(int64_t status);
```

`ktn_rt_exit` calls `fflush(stdout)`, `fflush(stderr)`, then
`exit((int)status)`. `ktn_rt_fail` should flush and then call
`ktn_rt_exit(1)` so fatal runtime errors and explicit exits share one path.

**Checker/codegen impact.** New `BuiltinKind.Exit`, one `int` parameter, return
type `void`; both checkers and both emitters register it. The runtime name is
added to `runtime/runtime.go`, `codegen/names.go`, and
`selfhost/codegen/names.ktn`. A call is one `ktn_rt_exit(<status>)` expression,
valid wherever a `void` call is valid.

**Risks.** `exit` is a common identifier; built-ins are shadowable by
user-defined functions (as `print` is), so a program that defines `exit` loses
the built-in. Negative or >255 statuses are host-dependent; the SPEC fixes only
the POSIX low-eight-bit observation. Exiting skips deferred cleanup, but Kitten
has no destructors and the collector does not require finalization.

**Alternatives.** A `_Noreturn` fatal-only model cannot express usage statuses.
Returning a status from `main` would change the entry-point rule and cannot
terminate from an arbitrary call site. A wrapper could infer success from empty
stderr/stdout, but that is not honest or robust.

**Recommendation: adopt `exit(status)`.**

### 3.4 Recoverable file read: `try_read_file(path) -> ReadFileResult`

**Need.** Phase 4E.1's loader calls `read_file`, which SPEC.md Section 22.2
defines as fatal. The reference loader instead returns
`cannot read source file: open <path>: no such file or directory`
(`loader/loader.go:83-86`), and the Kitten loader cannot reproduce it
(`selfhost/loader/README.md`). Section 22.9 defers recoverable I/O, and the
Phase 4E.1 report records this as the 4E.2 blocker. A compiler also needs to
distinguish an empty file (successful `Ok` with `""`) from a failed read, which
a fatal read cannot do.

This capability is needed for **parity and useful diagnostics**. It is not
strictly needed on the happy path of a fixpoint run, where every source file
exists; it is needed for the compiler to reproduce the reference's behavior on
missing or unreadable inputs. The proposal recommends it as part of the
credible driver, not as a substitute for the fixpoint.

#### Comparison of the three requested forms

| Form | Fits Kitten? | Preserves Go error text? | Main problems |
| --- | --- | --- | --- |
| Built-in tagged union `Ok{text}` / `Failed{message}` | **Best.** Kitten already has tagged unions and exhaustive `match`; no null, no exceptions, no generics. | **Yes**, if the runtime constructs the message with Go-compatible operation names and errno text. | Largest implementation: a predeclared result type must exist in both checkers and both generators, and codegen needs a built-in union mapping. |
| Bool plus out-parameter | Workable only through a mutable array slot, because Kitten has no references or pointers. | Possible if the slot receives the message. | Unidiomatic; the caller must allocate a slot array and remember which index means what; no type-level exhaustiveness; easy to misuse. |
| Split exists/read pair | Easy to write. | **No.** An existence check cannot report permission, directory, size, or read errors, and cannot supply the OS message. | TOCTOU between the check and the read; the read can still be fatal; missing vs unreadable is lost. |

The split pair does **not** close the 4E.2 gap. The bool-plus-out form can carry
a message but fits the language poorly. The tagged union is the only form that
is both type-safe in Kitten and capable of exact message parity.

#### Proposed SPEC wording

Add to Section 22.2, and remove the "recoverable I/O" sentence from Section
22.9 once adopted:

> ```kitten
> union ReadFileResult {
>     Ok { string text; }
>     Failed { string message; }
> }
> try_read_file(path) -> ReadFileResult
> ```
>
> `try_read_file` reads the entire contents of the regular file at `path` and
> returns `Ok` with the bytes on success, or `Failed` with a diagnostic on
> failure. An empty file is `Ok { text = "" }`. `Failed.message` is the
> operating-system error in the same shape as Go's `os.ReadFile`: for example
> `open /x.ktn: no such file or directory`, without the caller's prefix. The
> existing `read_file` remains fatal and unchanged. The result type is
> predeclared and cannot be redeclared by a program.

The loader then writes:

```kitten
match (try_read_file(path)) {
    Ok(text) { /* parse text */ }
    Failed(error) {
        return LoadError {
            path = path,
            line = 0,
            column = 0,
            message = "cannot read source file: " + error.message
        };
    }
}
```

**Runtime ABI.** The runtime owns both the read and the error formatting:

```c
typedef struct ktn_read_file_result {
    int64_t tag; /* 0 = Ok, 1 = Failed; codegen must agree */
    const ktn_string *text;
    const ktn_string *message;
} ktn_read_file_result;

const ktn_read_file_result *ktn_rt_try_read_file(const ktn_string *path);
```

The runtime opens and reads the file, closes it on every path, and allocates a
GC-managed result. On failure it fills `message` with the Go-compatible text.
The existing 256 MiB limit remains and is reported as a `Failed` message.

**Error-text parity is a runtime responsibility.** C's `strerror` capitalizes
messages ("No such file or directory") and may localize them; Go's `os.ReadFile`
does not. The runtime must therefore format a Go-compatible message, at minimum
for `ENOENT`, `EACCES`, `EISDIR`, `ENOTDIR`, `ENAMETOOLONG`, `EMFILE`, `ENFILE`,
and `ELOOP`, with the operation name (`open` or `read`) that Go would report.
The loader corpus currently exercises `ENOENT`; the proposal deliberately calls
out that plain `strerror` is not sufficient for byte parity.

**Checker/codegen impact.** This is the largest item:

- A predeclared `ReadFileResult` union type must be available without a user
  declaration. Both type checkers need to register it, both code generators
  need to map it to the runtime struct, and both `match` emitters need its tag
  values and field names. No user union declaration should be emitted for it.
- Add `BuiltinKind.TryReadFile`, parameter `string`, return `ReadFileResult`,
  to both checkers and both emitters, plus the runtime name in all name tables.
- The Kitten-written checker's `TypeKind.Union` can represent the type, but the
  type has no parsed declaration; the model and generator need a reserved
  built-in type identity. This is the main design risk.
- Existing `read_file` behavior and all its tests remain unchanged.

**Risks.**

- Predeclared union types are new machinery, not just a runtime function.
- Tag numbering and field layout must be duplicated consistently between the
  runtime header and generated `match` code; a mismatch is a wrong-variant bug.
- Error-text parity needs a Go-compatible errno mapping, not raw `strerror`.
- A failed read still cannot represent embedded NUL bytes differently from the
  existing string representation; the limitation is unchanged.

**Alternatives considered.**

- **Bool plus out-array slot** (`try_read_file(path, string[] out) -> bool`):
  smallest runtime/checker change, but no result type, no exhaustiveness, and
  an unusual calling convention. Reject as the recommended design; keep as the
  fallback if a predeclared type is deemed too large.
- **Split `file_exists`/`read_file`:** rejected; cannot preserve error text and
  remains fatal on permission or read errors.
- **`read_file_error(path) -> string` followed by `read_file`:** preserves text
  only by opening twice, adds TOCTOU, and still has a fatal second read. Not
  recommended.
- **Change `read_file` itself to return a union:** breaks the existing
  built-in's type and every caller; not backward-compatible.

**Recommendation: adopt the predeclared tagged union and `try_read_file`.**

### 3.5 File writing and process launch

**Do not add either in the bootstrap phase.**

The recommended architecture is:

```text
purr-selfhost <absolute-root.ktn> > program.c
        # stdout: one C translation unit, stderr: diagnostics
        # exit status: 0 success, nonzero failure

wrapper:
    create temp directory
    run purr-selfhost > temp/program.c
    copy kitten_runtime.c and kitten_runtime.h into temp/
    run cc ... temp/program.c temp/kitten_runtime.c -o <output>
    for "run": execute <output>, propagate stdin/stdout/stderr/status
```

This is enough for Goal 1 because the Kitten compiler owns every language
decision and all generated bytes; the wrapper owns host integration. The wrapper
must not be Go if the goal is independence from Go; a POSIX shell script is
sufficient on the supported host platforms. A later Kitten-written `purr` can
replace it once file writing and process launch exist.

**Honest weaknesses.**

- The compiler is not a single self-contained CLI binary; a wrapper is required.
- Redirecting a large translation unit through stdout costs a pipe/file copy
  and makes interactive debugging slightly less direct.
- The wrapper must create temp directories, copy runtime sources, discover the
  compiler, honor `CC`, obtain GC flags, and clean up. Those are real driver
  behaviors today (`cmd/purr/main.go:225-288`).
- `build`/`run` semantics, `-o`, default output names, and C compiler
  diagnostics live in the wrapper, not in Kitten.
- A shell wrapper is not portable to Windows and is not yet the `purr` tool.
- If the wrapper is sloppy about stdout/stderr separation, an error can be
  captured as C; the `eprint`/`exit` capabilities make a correct wrapper
  possible but do not enforce it.

**Why deferring does not block Goal 1.** Independence from Go needs the compiler
pipeline in Kitten and a host-level way to invoke a C compiler. A shell wrapper
supplies the latter without adding language semantics. File writing and process
launch become necessary for Goal 3 (the ecosystem) and for a polished `purr`
CLI, not for the first fixpoint. The proposal recommends adding them in a later
phase, with SPEC decisions for file-writing failure semantics and process
launch/stdio/status, once the fixpoint is demonstrated.

**Recommendation: no file-writing or process-launch built-ins in 5.x; use a
wrapper and revisit in the `purr` phase.**

### 3.6 Working directory and relative paths

**No cwd primitive is needed for the bootstrap if the wrapper passes absolute
paths.**

The Kitten loader already requires an absolute root
(`selfhost/loader/README.md`, paths.ktn), and import paths are resolved relative
to the importing module, so the loader never needs the process cwd. The wrapper
can resolve the user's source path to an absolute path with the host shell
(`realpath`, or `cd` plus `pwd`), and can pass absolute paths for the runtime
files and output. The generated C is written to stdout, so the compiler never
needs an output path. This is sufficient for the fixpoint.

A cwd primitive becomes useful for a user-facing CLI that wants to preserve
relative-path diagnostics or default output placement, but the wrapper can
handle those presentation decisions. If Kitten later owns the CLI, add
`cwd()`/`chdir` (or an absolute-path helper) in the `purr` phase; do not add it
to the bootstrap.

**Recommendation: wrapper-supplied absolute paths now; no cwd built-in.**

## 4. Proposed implementation order

The order was revised in Phase 5.6 and the fixpoint is now done. `argv`,
`eprint`, `exit`, the Kitten driver, and the fixpoint are implemented.
`try_read_file`/`ReadFileResult` and loader read-error parity remain postponed
until after the fixpoint and are next. Each step is one coherent commit with its
own acceptance checks.

1. **`argv()` and the argument-carrying entry point.**
   - Runtime `ktn_rt_set_args`/`ktn_rt_argv`; Go and Kitten checkers/code
     generators; both unit emitters; tests for no arguments, several arguments,
     empty arguments, and `argv()[0]`.
   - Acceptance: `go test -count=1 ./...`, `CC=gcc go test -count=1 ./...`,
     `CC=clang go test -count=1 ./...`; a compiled test program prints its
     arguments byte-for-byte; whole-unit harnesses agree on the new entry point.

2. **`eprint(...)`.**
   - Runtime helpers; both built-in tables and emitters; diagnostics tests.
   - Acceptance: a program writes to stdout and stderr; a harness captures both
     separately and compares exact bytes; `print` tests remain unchanged.

3. **`exit(status)`.**
   - Runtime `ktn_rt_exit`; `ktn_rt_fail` routes through it; both built-in tables
     and emitters; status tests for 0, 1, 2, and a larger value.
   - Acceptance: a compiled program exits with each requested status; stdout is
     flushed before exit; stderr is flushed; the existing fatal-error tests still
     pass.

4. **POSTPONED — `try_read_file` and `ReadFileResult`.** The design in
   `proposals/read-file-result.md` stays proposed and untouched until after the
   fixpoint.
   - Predeclared type in both checkers/generators; runtime result object and
     Go-compatible error formatting; existing `read_file` untouched.
   - Acceptance: the Phase 4E loader harness gains missing, unreadable, and
     empty-file cases and reports zero mismatches against `loader.Load`,
     including `cannot read source file: open <path>: no such file or directory`;
     the full suite passes under both compilers.

5. **DONE — Kitten compiler driver.**
   - `selfhost/driver.ktn` reads `argv()`, requires an absolute root,
     loads/checks/emits, writes diagnostics with `eprint`, and uses `exit`; it
     does no file writing or C compiler invocation.
   - `selfhost/driver/purr-bootstrap.sh` owns temp files, runtime copying, C compiler
     discovery/`CC`/GC flags, output path, and run-status propagation.
   - Acceptance: the wrapper builds and runs every fixture and `tests/modules/`;
     the emitted C is byte-identical to the Go reference; the wrapper's
     stdout/stderr/status match `purr build`/`purr run` for the corpus; the
     wrapper itself contains no Go.

6. **DONE — Fixpoint proof.**
   - Compile the Kitten compiler's source with the Go compiler (stage 0), then
     with the Kitten driver/wrapper (stage 1), then stage 1 compiles its own
     source (stage 2).
   - Acceptance: stage 1 and stage 2 C outputs are byte-identical for the
     compiler source and for the fixture/corpus set; `tests/modules/` and the
     loader corpus produce the expected output; the wrapper and C compiler remain
     the only host dependencies.

## 5. What the audit found that argues against this direction

- **The tagged union is much more than a runtime helper.** Kitten has no
  predeclared types today; adding one touches both checkers, both generators,
  the runtime header, and the match emitter. If the human wants the absolute
  minimum, the bool-plus-out fallback is smaller but materially worse as a
  language design.
- **`strerror` is not Go-compatible.** The runtime must own an errno-to-message
  mapping for parity; a new read helper alone does not close 4E.2.
- **The wrapper is real driver code.** It must reproduce `CC`, `pkg-config`,
  `-std=c11 -Wall -Wextra`, temp-file cleanup, `-o`, default output names, and
  `run` status propagation. It is small but not trivial, and it must not be
  written in Go.
- **A stdout filter is not a complete CLI.** `-o`, usage text, and `run` belong
  to the wrapper until `purr` exists. This is acceptable for Goal 1 but should
  not be described as a finished toolchain.
- **The current platform/path model is POSIX/Linux-centric.** The loader's path
  helpers and the proposed wrapper assume `/` and absolute paths; Windows is
  not addressed.
- **No standard library exists.** Built-ins are the only available surface;
  `argv`/`eprint`/`exit` are therefore built-ins, not library functions, which
  grows the built-in table and both compilers.

## 6. Questions only the human can answer

1. Approve the predeclared `ReadFileResult` union (and its implementation cost),
   or choose the smaller bool-plus-out fallback and accept its ergonomics?
2. Should `argv()` include the program name at index 0, or return only the
   program arguments? This proposal recommends including it.
3. Is a POSIX-shell wrapper acceptable for the first fixpoint, or must the
   wrapper itself be Kitten (which would require file writing and process launch
   now)?
4. Does the fixpoint acceptance include missing/unreadable-file diagnostics, or
   is that allowed to remain 4E.2 after the happy-path fixpoint?
5. Should read-failure parity cover every errno/operation shape, or only the
   common cases in the proposal's table?
6. Should `eprint`/`exit` be built-ins permanently, or provisional until a
   standard library exists?
7. What is the supported host platform set for this phase: Linux only, or
   Linux and macOS? That affects the wrapper, path rules, and error text.
8. Should the source path and `-o` be parsed by the Kitten driver or by the
   wrapper? This proposal gives the wrapper `-o`/temp-file policy and the Kitten
   driver the root path.
