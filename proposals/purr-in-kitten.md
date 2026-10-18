# `purr` written in Kitten

> **ADOPTED (plan); implementation in progress.**
>
> The human approved this plan on 2026-10-18. Implementation proceeds one commit
> per step in the order below. The `try_getenv`/`EnvResult` and
> `try_run_capture`/`CapturedRunResult` steps are implemented so far; no `purr`
> command described here is implemented unless explicitly marked **already
> implemented**.
>
> Date: 2026-10-18. Starting commit: `bbc0343`; adoption recorded at `9e98165`.
>
> Scope: the `purr` binary's structure, `purr build`, `purr run`, `purr new`,
> the first `purr.toml` subset, and the project layout. `add`, `install`, `test`,
> `fmt`, `doc`, `publish`, `lsp`, `self`, the lockfile, and the registry remain
> separate work. The native backend and runtime rewrite (Goal 2) are not part of
> this design.
>
> The referenced `proposals/toolchain-and-installer.md` does not exist in this
> tree at `bbc0343`. This design uses `proposals/zero-dependency-toolchain.md`,
> `proposals/driver-capabilities.md`, `VISION.md`, and the current code instead.

## 1. Recommendation in one screen

- Build **one `purr` binary** containing the compiler modules and the command
  implementation. Do not spawn a separate compiler executable.
- Put the build root at the repository root as `purr.ktn`; put implementation
  modules under `purr/`. The root imports `selfhost/...` and `purr/...`, so the
  existing within-root loader rule is unchanged.
- Keep the Go CLI authoritative until the Kitten `purr` passes a defined parity
  suite; then freeze it as a bootstrap/reference tool.
- `purr build <file.ktn>` and `purr run <file.ktn>` retain the current Go CLI
  behavior. Project mode is additive: `purr build` and `purr run` with no source
  inside a directory containing `purr.toml` use `src/main.ktn` or
  `src/lib.ktn`.
- Add the smallest language/runtime capabilities the tool needs:
  `try_getenv`, `try_run_capture`, `try_file_info`, and
  `try_create_temp_directory`. Add `try_executable_path` later, not as a
  `build`/`run` blocker because runtime sources are embedded. `try_find_program`
  is deliberately dropped; PATH search will be Kitten code over `try_getenv`
  and `try_file_info`.
- Embed the runtime C sources as generated Kitten string constants. Do not
  require a runtime directory next to the installed binary.
- `purr new` creates `NAME/.gitignore`, `NAME/purr.toml`, and
  `NAME/src/main.ktn`; `--lib` creates `NAME/src/lib.ktn` instead. Templates are
  user-owned and carry no license notice or license obligation.
- The first manifest subset is one `[package]` table with exactly `name` and
  `version`, both strings. Everything else is rejected with a positioned error.
- Until Goal 2 lands, the Kitten `purr` still invokes a C compiler and links
  libgc. That is an explicit temporary build requirement, not part of the final
  zero-dependency design.

### 1.1 Approved answers and implementation status

The human approved the plan and answered the open questions:

1. The new `purr` may list `new` in help. The Go CLI's help is not frozen;
   parity compares everything except the help command list, which gets its own
   pinned text.
2. The strict package-name and version grammar in Section 7 is accepted.
3. Library builds report a documented, clear error for now. No no-entry-point
   codegen mode is added yet.
4. Project discovery is the current directory only for now; parent search may be
   added later.
5. The capture policy is accepted: stdin is `/dev/null`, each stream has a
   16 MiB cap, and a NUL in captured output is an error.
6. `purr new` does not run `git init`.
7. Generated projects have no license file or prompt; their files belong to the
   user.
8. The repository-root `purr.ktn` layout is accepted.

Implementation status:

| Step | Status |
|---|---|
| 1. `try_getenv` / `EnvResult` | **implemented in 9.1** |
| 2. `try_run_capture` | **implemented in 9.2** |
| 3. `try_file_info` | proposed, next |
| 4. `try_create_temp_directory` | proposed |
| 5. Embedded runtime generation | proposed |
| 6. TOML subset parser | proposed |
| 7. `purr new` | proposed |
| 8. Kitten `purr` source-mode build/run | proposed |
| 9. Project mode | proposed |
| 10. Library object mode | proposed, deferred |
| 11. Full parity suite / Go CLI freeze | proposed |

## 2. Audit: the current Go CLI is the parity specification

### 2.1 Command grammar and exit statuses

`cmd/purr/main.go:run` dispatches only `help`, `-h`, `--help`, `build`, and
`run`. No arguments prints usage to stderr and exits 2; `help` prints the same
text to stdout and exits 0; an unknown command prints `purr: unknown command
"..."`, blank line, usage to stderr, and exits 2.

The adopted exit statuses are:

| Status | Meaning |
|---:|---|
| 0 | success, including `help` |
| 1 | compile/build/run failure or a failed child process |
| 2 | usage error |

`purr run` returns the child's exit code directly, including statuses above 127;
it does not normalize them itself.

### 2.2 `purr build`

`runBuild` and `parseCommandArgs` establish:

- exactly one positional source path;
- `-o`, `--output`, `-o=PATH`, and `--output=PATH`, before or after the source;
- `--` terminates option parsing;
- an unknown `-...` option is a usage error;
- a missing output path after `-o`/`--output` is a usage error;
- zero positional paths is `missing source file`;
- more than one is `expected one source file, got N`;
- the source must end in `.ktn`, else `${path}: "..." is not a Kitten source file (expected a .ktn file)` on stderr and status 2;
- with no `-o`, output is `defaultOutputPath`: the source base name without
  `.ktn`, in the **current directory**, with `a.out` for an empty/dot base;
- success prints `built <output>` to stdout.

`buildExecutable` then:

1. `loader.Load(source)` — modules are loaded depth-first, canonical paths, IDs
   by load order; imports must remain inside the root source directory
   (`loader/loader.go:Load`, `withinDirectory`);
2. `checker.CheckModules`;
3. `codegen.Generate`, which requires a zero-parameter, void-returning `main` in
   the root module (`codegen/codegen.go:validateEntryPoint`);
4. discovers the C compiler with `discoverCompiler`;
5. creates a `purr-build-` temporary directory, writes `program.c`,
   `kitten_runtime.c`, and `kitten_runtime.h` with mode 0644;
6. makes the output path absolute, obtains GC flags from `gcBuildFlags`, and
   invokes the compiler with `-std=c11 -Wall -Wextra`, `-o ABS_OUTPUT`,
   `program.c`, `kitten_runtime.c`, then link flags;
7. captures compiler stdout and stderr; on failure reports
   `<source>: C compilation failed: exit status N` and the captured output; on
   success forwards non-empty diagnostics to stderr;
8. verifies the output exists and is not a directory, then removes the build
   directory.

The C compiler is never invoked through a shell.

### 2.3 `purr run`

`runRun` accepts the same source/argument parsing but rejects any `-o` form with
`purr run: -o is not supported; run compiles to a temporary executable`. It
creates a `purr-run-` temporary directory, builds `program` there, runs it with
inherited stdin/stdout/stderr, returns its exit code, and removes the directory.
It accepts no arguments to pass to the compiled program.

### 2.4 C compiler discovery and GC flags

`cmd/purr/toolchain.go:discoverCompiler`:

- reads `CC` with `getenv`;
- trims surrounding whitespace; splits on whitespace with `strings.Fields`;
- resolves the first field through `exec.LookPath`; remaining fields are fixed
  compiler arguments;
- an empty `CC` value falls through to `cc`, `gcc`, `clang` in that order;
- failure text is `CC="..." : "..." not found` or
  `no C compiler found; set CC or install cc, gcc, or clang`.

`gcBuildFlags` runs `pkg-config --exists bdw-gc`, then captures
`pkg-config --cflags bdw-gc` and `--libs bdw-gc`; missing/failed pkg-config
falls back to `-lgc`.

### 2.5 Runtime embedding

`runtime/runtime.go` embeds `runtime/csrc/kitten_runtime.h` and `.c` with
`//go:embed` into `headerSource`/`sourceSource`, exposed as `HeaderSource()` and
`Source()`. The Go CLI writes those strings into the temporary build directory.
The installed Go binary therefore does not need the repository runtime tree.

### 2.6 Go CLI test surface already present

`cmd/purr/main_test.go` already pins usage/exit codes, argument parsing, default
output naming, fixture output, `run` output forwarding, and many language
runtime behaviors. `cmd/purr/purr_test.go` builds the Go CLI and checks the
`purr` name. The existing `TestKittenFixpoint` pins the C-level compiler
fixpoint but not a user-facing `purr` CLI parity suite.

### 2.7 What does not exist

- No `purr new`.
- No `purr.toml` parser or project discovery.
- No `src/main.ktn` or `src/lib.ktn` project mode.
- No Kitten `purr` binary or `purr.ktn` root.
- No environment-variable, PATH-search, output-capture, or temp-directory
  primitive in Kitten.
- No Kitten equivalent of `//go:embed` for the runtime sources.
- No primitive for resolving the running executable path.

## 3. Language and runtime gaps

The four-pipeline cost is the standard built-in cost in `AGENTS.md`: Go and
Kitten checkers, Go and Kitten generators, the runtime ABI, the semantic model,
the prelude/reserved-name machinery where a result union is predeclared, and
all differential corpora. Each item below is a separate proposed feature commit,
not part of the design-only phase.

### 3.1 `try_getenv` (implemented in 9.1)

**Needed for:** `purr build`/`run` discovering `CC`; `purr` reading `PATH` and
`TMPDIR`; project/tool configuration later.

**Implemented shape.**

```text
try_getenv(string name) -> EnvResult
```

```kitten
union EnvResult {
    Set { string value; }
    Unset
}
```

`Unset` is separate from `Set { value = "" }`. An empty name, or a name
containing `=`, is `Unset`, mirroring `getenv` returning no match. The value is
a fresh GC-managed copy of the OS bytes, including bytes above `0x7F`; NUL cannot
occur because Kitten strings cannot contain it. There is no setter; `try_run`
continues to inherit the environment unchanged.

Runtime ABI:

```c
bool ktn_rt_try_getenv(const ktn_string *name, const ktn_string **value);
```

It returns true and stores the copied value when the variable exists, including
an empty value, and false when unset.

Checker/generator impact as implemented:

- predeclared `EnvResult` is reserved in every program and appended on use after
  `CurrentDirectoryResult` in the single `<prelude>` module;
- `try_getenv` is registered late like the other prelude builtins;
- one non-static helper, `ktn_prelude_<N>_try_getenv`, builds `Set` or `Unset`;
- the C runtime uses `getenv` and copies before returning.

The future native runtime must read the environment from the process's initial
stack (`envp`), because there is no libc `getenv` there. That is not implemented
now.

**Blocks:** `build` and `run` (compiler discovery); `new` does not need it.

### 3.2 `try_file_info` (proposed; replaces `try_find_program`)

**Design override:** the earlier `try_find_program` built-in is **dropped**.
PATH search will be written in Kitten on top of `try_getenv("PATH")` plus this
general file-information primitive. No find-program runtime primitive is
implemented.

**Needed for:** PATH search, project discovery, `purr new` existence checks,
checking whether `target` is a directory, and future installer/`self` work.

**Proposal.**

```text
try_file_info(string path) -> FileInfoResult
```

```kitten
union FileInfoResult {
    Ok {
        bool is_file;
        bool is_directory;
        bool is_symlink;
        bool executable;
        int size;
    }
    Missing
    Failed { string message; }
}
```

- `lstat` classifies the path itself; a symlink sets `is_symlink` and does not
  claim to be the target's file/directory kind.
- `executable` uses `access(path, X_OK)` and therefore follows symlinks; it is
  the bit PATH search needs.
- `size` is the regular file's byte size, or 0 for directories and symlinks.
- `Missing` means `ENOENT`/`ENOTDIR`; other failures use the Go-compatible
  `stat <path>: <reason>` style.
- No `follow_symlinks` flag is needed in the first version; a caller that wants
  target metadata can add it later without changing existing fields.

**Blocks:** PATH search and therefore `build`/`run`; `new` existence checks and
project discovery also use it once project mode exists.

### 3.3 `try_run_capture` (implemented in 9.2)

**Implemented shape.**

```text
try_run_capture(string program, string[] args) -> CapturedRunResult
```

```kitten
union CapturedRunResult {
    Ok { int status; string stdout; string stderr; }
    Failed { string message; }
}
```

It uses exactly `try_run`'s direct-exec semantics, including inherited
environment/working directory/umask, `argv[0]`, status/signal mapping, and
`fork/exec <program>: <reason>` start failures. stdin is `/dev/null`; stdout and
stderr are captured separately. The parent flushes both streams before forking
and reads both pipes concurrently with `poll` so neither can deadlock the other.

Each stream is capped at exactly 16,777,216 bytes. Over-limit output kills and
reaps the child and fails with:

```text
run <program>: stdout exceeds the 16777216 byte limit
```

(or `stderr`). A NUL byte in either stream fails after the child is reaped with:

```text
run <program>: stdout contains a NUL byte at offset <n>
```

(or `stderr`), where `<n>` is zero-based. Invalid UTF-8 and bytes above `0x7F`
are preserved. A `byte[]`-returning variant is deferred.

Runtime ABI:

```c
bool ktn_rt_try_run_capture(const ktn_string *program,
                            const ktn_array *args,
                            int64_t *status,
                            const ktn_string **out,
                            const ktn_string **err,
                            const ktn_string **message);
```

**Blocks:** `build` and `run`; not `new`.

### 3.4 `try_create_temp_directory`

**Needed for:** `build` writing `program.c`/runtime sources, and `run` writing a
temporary executable, without a C `mkdtemp` call exposed to user code.

**Proposal.**

```text
try_create_temp_directory(string prefix) -> TempDirectoryResult
```

```kitten
union TempDirectoryResult {
    Ok { string path; }
    Failed { string message; }
}
```

- Equivalent to `mkdtemp` under `TMPDIR` or `/tmp`.
- Absolute path, unique, mode 0700.
- The prefix is a literal safe component; path separators are rejected with a
  clear error.
- Cleanup uses existing `try_remove` after removing known files; no recursive
  removal is added.

Runtime ABI:

```c
bool ktn_rt_try_create_temp_directory(const ktn_string *prefix,
                                      const ktn_string **path,
                                      const ktn_string **message);
```

**Blocks:** `build` and `run`; not `new` (which writes directly into a newly
created project directory).

### 3.5 `try_executable_path` (not blocking)

**Needed later for:** `purr self`, installer integration, and diagnostics that
name the installed binary. It is not needed for `new`, `build`, or `run` if the
runtime sources and templates are embedded.

**Proposal.**

```text
try_executable_path() -> ExecutablePathResult
```

```kitten
union ExecutablePathResult {
    Ok { string path; }
    Failed { string message; }
}
```

Linux implementation reads `/proc/self/exe` with a growing buffer, like
`try_current_directory`. No `argv()[0]` guessing.

### 3.6 Existing capabilities that suffice

- Project discovery and manifest reading: `try_current_directory`,
  `try_read_file`, `try_list_directory`.
- File writing and template creation: `try_write_file` (mode 0644),
  `try_create_directory` (one level, mode 0755).
- Cleanup: `try_remove` (files, links, empty directories) after known files.
- Running the built program: `try_run` (inherited stdio).
- Compiler diagnostics: `try_run_capture` supplies them.
- Runtime source embedding: no runtime primitive; generated constants.
- Output executable mode: the C compiler creates the executable mode; `purr`
  does not need `chmod` for its output. `purr new` needs only 0644 files and
  0755 directories.

### 3.7 Blocking summary

| Capability | `new` | `build` | `run` |
|---|---:|---:|---:|
| `try_getenv` | no | **yes** | **yes** |
| `try_file_info` | **yes** | **yes** | **yes** |
| `try_run_capture` | no | **yes** | **yes** |
| `try_create_temp_directory` | no | **yes** | **yes** |
| `try_executable_path` | no | no | no |
| TOML parser | **yes** (manifest) | yes | yes |
| Runtime-source embedding | no | **yes** | **yes** |

`new` needs only existing directory/file primitives, the TOML writer/parser
subset, and the templates.

## 4. Repository layout and the loader's within-root rule

`loader/loader.go:withinDirectory` rejects an import whose canonical path is not
below the root source's directory. Moving the Kitten `purr` root under `purr/`
and importing `../selfhost/...` would therefore fail.

### Recommended: root `purr.ktn`

```text
purr.ktn                    # Kitten purr entry point, imports compiler + purr modules
purr/
  cli.ktn                   # argument grammar and dispatch
  build.ktn                 # build/run orchestration
  project.ktn               # purr.toml discovery and project mode
  manifest.ktn              # TOML subset parser and writer
  new.ktn                   # purr new templates and file operations
  embedded.ktn              # generated runtime C source constants
  diagnostics.ktn           # message formatting and exit statuses
selfhost/                   # existing compiler modules, unchanged
runtime/csrc/               # existing MIT C runtime, embedded at build time
runtime/ktn/                # existing MIT Kitten runtime helpers
```

`purr.ktn` sits at the repository root, so its loader root is the repository
root and every `selfhost/...` and `purr/...` import is within it. Existing
`selfhost/*.ktn` roots and every differential harness path remain unchanged.
The cost is one new top-level file; there is no path migration.

Build it, while Go is authoritative, with:

```sh
go build -o purr ./cmd/purr
./purr build purr.ktn -o purr
```

The second command is a normal Go-pipeline build of the Kitten source. After
the Kitten `purr` exists, it can build `purr.ktn` itself and the result can be
compared with the Go-built binary's generated C for the fixpoint.

### Rejected alternatives

- **Move `selfhost/` under `purr/`.** Requires changing every loader, driver,
  differential harness, and documentation path. High churn, no benefit.
- **Symlink compiler modules into `purr/`.** The loader canonicalizes by
  `Abs`/`Clean`, not `EvalSymlinks`; symlink-relative imports are fragile and
  would create two path identities for one module.
- **Lift the within-root rule.** This is a language/loader semantic change,
  changes import behavior and diagnostics, and invalidates loader parity. It
  needs a SPEC decision; it is not necessary for this design.

## 5. One binary, not a compiler subprocess

Recommendation: link the Kitten compiler modules and the `purr` command into one
binary. `purr build` calls `loader.Load`, `checker.CheckModules`, and
`codegen.Generate` in-process, exactly as the Go CLI does, then invokes only the
C compiler and linker as a subprocess.

Why:

- the fixpoint story stays the compiler's fixpoint, not a process-discovery
  protocol;
- no second executable needs to be found adjacent to `purr`;
- no IPC/serialization for modules or diagnostics;
- build time is the compiler's current roughly 2.2 s/78 MiB plus CLI work;
- the extra command code is small relative to the approximately 383 KB driver;
- `purr self` can later rebuild the same source without a separate compiler
  binary contract.

The subprocess alternative would need `try_executable_path` immediately, a
stable compiler protocol, separate diagnostics, and a second binary install;
it is deferred unless profiling later shows a real startup problem.

`purr build` still needs an external C compiler and libgc until Goal 2. That is
temporary:

- the C source emitted by the compiler and the MIT runtime sources are written
  to a temp directory;
- Kitten code splits `PATH` and uses `try_file_info` to resolve `CC`/`cc`/`gcc`/`clang`;
- `try_run_capture` runs `pkg-config` and the compiler;
- the output executable is linked against libgc.

After Goal 2, only this invocation layer changes; the Kitten `purr` command
grammar and project model do not.

## 6. Command-line grammar

The final grammar is additive over the current CLI:

```text
purr help | -h | --help
purr build [<file.ktn>] [-o <output>]
purr run [<file.ktn>]
purr new [--lib] <name>
```

- No arguments: usage to stderr, exit 2 (current behavior).
- `help`/`-h`/`--help`: usage to stdout, exit 0.
- Unknown command: current `purr: unknown command "..."` shape, exit 2.
- `build`/`run` source mode retains the current parser, flags, defaults,
  messages, exit statuses, and child-status propagation.
- `build`/`run` with no source enters project mode **only if `purr.toml` exists
  in the current directory**; otherwise the current missing-source usage error
  is preserved. Parent-directory manifest search is deferred.
- `new` takes exactly one name; `--lib` selects the library layout; `--help`
  prints new help. Extra names, unknown flags, and missing names are usage
  errors (exit 2).
- `build`/`run` help remains the current text during parity so the Go CLI and
  Kitten `purr` can be compared byte for byte. Adding `new` to the top-level help
  is a post-freeze documentation change, or a separately approved parity
  exception. This is an explicit question for the human, not silently resolved.

Proposed `new` usage/help text is new, with no Go reference:

```text
Kitten project scaffolding

Usage:
  purr new [--lib] <name>

Options:
  --lib    Create a library project instead of a binary
  -h, --help
```

## 7. `purr.toml`: the smallest useful TOML subset

The first manifest is deliberately smaller than TOML. A file outside the
subset is rejected, never guessed at.

### 7.1 Accepted grammar

```text
manifest      = package
package       = "[package]" newline { line }
line          = blank | comment | field
field         = key ws "=" ws string newline
key           = "name" | "version"
string        = '"' { string-char } '"'
string-char   = printable ASCII except '"' and '\'
                | escape
escape        = "\\" | "\"" | "\n" | "\t" | "\r"
ws            = { " " | "\t" }
comment       = "#" { any byte except newline } newline
```

- `[package]` occurs exactly once.
- `name` and `version` are required exactly once each.
- Keys may appear in either order.
- Empty strings are syntactically allowed but rejected by field validation.
- Only LF newlines; a CR immediately before LF is accepted and ignored.
- The file must end after a newline or at EOF.
- No other table, key, number, boolean, array, inline table, dotted key,
  single-quoted string, multiline string, or bare value is accepted.

### 7.2 Validation

- `name`: 1–64 bytes, starts with a lowercase ASCII letter, then lowercase ASCII
  letters, digits, `_`, or `-`; no `/`, `.`, whitespace, or path component.
- `version`: exactly `MAJOR.MINOR.PATCH`, each one or more decimal digits with no
  leading zero unless the value is exactly `0`.
- `name` is also the project directory name and the default target name.
- `src/main.ktn` means binary; `src/lib.ktn` means library. Both present or
  neither present is a project error.

### 7.3 Errors and positions

`ManifestError` carries 1-based line and byte column and one stable message:

```text
expected "[package]" at line 1, column 1
unknown table "[dependencies]" at line 4, column 1
unknown key "edition" in [package] at line 3, column 1
expected "=" after key at line 3, column 9
expected a double-quoted string at line 3, column 11
duplicate key "name" at line 4, column 1
missing required key "name"
invalid package name "Hello" at line 3, column 8: names must start with a lowercase ASCII letter and contain only lowercase ASCII letters, digits, "_", and "-"
invalid package version "1.0" at line 4, column 11: expected MAJOR.MINOR.PATCH
file contains a NUL byte at line 2, column 7
```

The parser returns `ManifestResult { Ok { Manifest }; Failed { ManifestError } }`;
the CLI renders the error with the manifest path and exits 1.

### 7.4 Growth without breaking files

Adding a new optional key or table is backward compatible: every currently
accepted file remains accepted with unchanged meaning. Unknown constructs
continue to be rejected until the parser is deliberately extended. Adding a
required key or changing a value's meaning requires a manifest version decision
and is not done silently.

### 7.5 Parser design

- one line at a time, byte-oriented, using the existing string indexing and
  `substring` operations;
- a tiny parser state (current table, seen-name, seen-version);
- no regex, no TOML dependency, no map iteration;
- comments and whitespace stripped before a line is classified;
- strings decoded into ordinary Kitten strings;
- a fixed `Manifest` struct and a `ManifestError` union;
- no writer is needed for 9.0; `purr new` writes the exact fixed template.

Test strategy: independent Kitten test programs that import `purr/manifest.ktn`
and print normalized parse results or exact errors; matching `.expected` files.
No Go reference exists for this new parser, so expected text is reviewed and
pinned directly. A table of valid manifests and every rejected construct is
required before `purr new` is accepted.

## 8. `purr new`

### 8.1 Behavior

```sh
purr new hello-world
purr new --lib catnip
```

Name validation is the manifest rule above. A name that is also a path
(`a/b`, `..`, `.`) is rejected before touching the filesystem. The parent must
exist; `new` creates one project directory, not a parent chain.

Success messages (proposed; Cargo-like, but plain and stable):

```text
     Created binary (application) `hello-world`
     Created library `catnip`
```

Exact files and modes:

```text
hello-world/          0755
hello-world/.gitignore       0644
hello-world/purr.toml        0644
hello-world/src/             0755
hello-world/src/main.ktn     0644
```

Library shape uses `src/lib.ktn` instead.

### 8.2 Exact templates

`.gitignore`:

```text
/target
```

`purr.toml`:

```text
[package]
name = "hello-world"
version = "0.1.0"
```

`src/main.ktn`:

```kitten
fn main() {
    print("Hello, world!");
}
```

`src/lib.ktn`:

```kitten
public fn add(int left, int right) -> int {
    return left + right;
}
```

The name appears only in `purr.toml`; the source templates are name-independent
so they can be compared exactly. The generated files carry **no license notice
and no GPL or MIT obligation**; they belong to the user. `purr new` does not run
`git init` and does not create a license file, README, lockfile, or target
directory.

### 8.3 Existing directory and failure cleanup

- If `NAME` exists as any kind of path, print
  `error: destination `NAME` already exists` to stderr and exit 1; make no
  changes.
- Create the project directory, `src`, and the three files in order.
- On any create/write failure, remove the files it created and then the
  directories it created (deepest first), using `try_remove`; report the first
  error and exit 1.
- This is cleanup-complete, not crash-atomic. There is no rename primitive; a
  process crash can leave a partial directory. Atomic staging with rename is
  deferred until a `try_rename` primitive exists.
- `new` does not overwrite or prompt.

## 9. Project mode and output naming

Project mode activates only for `purr build`/`purr run` with no source argument
and a `purr.toml` in the **current directory**.

- Parse `purr.toml`; detect exactly one of `src/main.ktn` (binary) or
  `src/lib.ktn` (library).
- Binary `purr build` output is `target/<name>`; create `target` mode 0755 if
  absent. If `target` exists as a file, fail.
- Binary `purr run` builds `target/<name>` and runs it with inherited stdio and
  child status.
- Source mode is unchanged: `purr build path/file.ktn` uses the source base name
  in the current directory, and `purr run path/file.ktn` uses a temporary
  executable. An explicit source argument never enters project mode.
- A library has no `main` and today's `codegen.Generate` requires one. The
  first library implementation slice will add a no-entry-point generation mode
  and compile `target/<name>.o` with the C compiler (no executable). Until that
  slice lands, `purr build` on a library reports a clear
  `library builds are not implemented yet` and `purr run` on a library is a
  project error. `purr new --lib` still scaffolds the layout.
- No parent-directory manifest search, workspace, profiles, features, or
  dependency sections in 9.0.

`target/` is the only generated project directory; `.gitignore` contains
`/target`.

## 10. Runtime source embedding and installed layout

The Go CLI embeds `runtime/csrc` with `//go:embed`. Kitten needs an equivalent:

1. a GPL build-time generator (initially a small Go command, later a Kitten
   generator in the bootstrap) reads `runtime/csrc/kitten_runtime.c` and `.h`;
2. it writes `purr/embedded.ktn`, a generated module of string constants with
   C-safe escaping and the MIT notice preserved in the embedded text;
3. a test regenerates and byte-compares the module, so the checked-in generated
   file cannot silently drift;
4. `purr build` writes those strings to its temporary build directory with
   `try_write_file` (0644).

Installed layout is then one binary and no runtime directory. This is required
for a later one-line installer: the installer only places `purr` and any
components, not a repository-shaped runtime tree. `try_executable_path` remains
useful for `purr self` and diagnostics but does not block build/run.

Template text is embedded directly in `purr/new.ktn`; it is small and reviewed,
not generated.

## 11. Compiler invocation and bootstrap

`purr build` in-process pipeline:

```text
argv parse
  -> loader.Load(source)
  -> checker.CheckModules
  -> codegen.Generate
  -> find C compiler / pkg-config
  -> temp dir: write program.c + embedded runtime.c/.h
  -> try_run_capture(compiler, args)
  -> verify output exists and is not a directory
  -> cleanup temp files/dir
```

`purr run` is the same build followed by `try_run(executable, [])`, inheriting
stdin/stdout/stderr and returning the child status.

Bootstrap sequence:

1. Go builds the current `purr` (authoritative).
2. Go-built `purr` builds `purr.ktn` into a Kitten `purr` using the Go pipeline.
3. Kitten `purr` builds `purr.ktn` into stage 2; stage 2 builds stage 3.
4. C output for `purr.ktn` is byte-compared across Go and Kitten stages, as the
   compiler fixpoint already does for `selfhost/driver.ktn`.

The Go CLI is frozen after the parity suite passes, but the source remains as a
bootstrap/reference tool. It is not deleted.

## 12. Parity suite and freeze criteria

Parity is defined against the Go CLI at `bbc0343`. The suite builds both
binaries and runs them in isolated temporary directories with a controlled
`PATH`, `CC`, `TMPDIR`, compiler, and fixture set.

### 12.1 Legacy command cases

| Case | Compare |
|---|---|
| no args, `help`, `-h`, `--help`, unknown command | stdout, stderr, exit |
| `build` missing source, wrong extension, unknown option, missing `-o` value, too many sources | exact messages and exit 2 |
| `build` source only, `-o` before/after, `-o=`, `--output`, `--`, `a.out` fallback | output path, announcement, exit |
| `build` source errors: missing file, unreadable, directory, import outside root, cycle, lex/parse/check/generate | exact path/position/message, exit 1 |
| `build` output directory, C compiler failure, missing output, compiler warnings | exact current wording, captured diagnostics, exit 1 |
| `run` source only, `-o` rejection, wrong extension, unknown option | exact messages, exit |
| `run` child output, stdin passthrough, statuses 0, 1, 7, 255 | stdout, stderr, exit |
| `CC` unset / `cc` / `gcc` / `clang` / invalid; pkg-config present/absent | compiler choice and resulting output |
| all 9 fixtures, `tests/modules/`, `selfhost/driver.ktn` | executable behavior and generated C byte-identical |

Temporary-path differences (`purr-build-*`, `purr-run-*`) are normalized to a
stable placeholder in diagnostics; the design expects the Kitten `purr` to use
the same prefixes so the normalization is minimal.

### 12.2 New behavior cases (no Go reference)

`purr new`, `purr.toml`, and project mode are tested with independently pinned
expectations:

- exact tree, file bytes, modes, and stdout/stderr;
- existing destination, invalid name, invalid version, trailing arguments,
  unknown flags, parent missing;
- malformed/unsupported TOML: exact line/column/message;
- project binary build/run output naming and `target/` mode;
- library scaffold; library build's documented deferred error until the
  no-entry slice;
- cleanup after an injected write failure (test seam or read-only directory).

### 12.3 Library build (planned)

The first project library slice needs a compiler-side no-entry mode:

- `codegen.GenerateLibrary` or a `Generate` option that skips
  `validateEntryPoint` and omits the C `main`;
- `purr build` writes the C and invokes the C compiler with `-c -o
  target/<name>.o target/<name>.c`;
- no link, no executable; runtime symbols remain unresolved for a later link;
- tests compare object existence/mode and compile/link smoke behavior, not a
  stable binary format.

If the human prefers to keep 9.0 to scaffolding only, this slice can be
deferred; `purr new --lib` still works.

### 12.4 Freeze

The Go CLI can be frozen only when:

- every legacy case above is byte-identical or explicitly normalized only for
  temp paths;
- the Kitten `purr` builds all fixtures and `tests/modules/` with identically
  behaving executables;
- the Go-vs-Kitten compiler C fixpoint remains byte-identical;
- `purr new` and TOML expectations pass;
- the build is reproducible from a clean checkout with documented commands.

After freeze, new features land only in the Kitten `purr`; the Go CLI is kept
for bootstrap/reference and its tests are not the definition of `purr`.

## 13. Implementation plan

Each item is a separate commit after approval; none is authorized by this
design-only phase.

1. **Environment primitive** (`try_getenv` and `EnvResult`) — implemented in
   9.1. Acceptance: exact missing/empty values; compiler discovery can read
   `CC` and `PATH`; no production behavior change.
2. **Capture primitive** (`try_run_capture` and `CapturedRunResult`) —
   implemented in 9.2. Acceptance: both pipes, start failure text, no deadlock,
   exact limits and statuses.
3. **File information** (`try_file_info`) — proposed for 9.3; PATH search is
   then written in Kitten, with no `try_find_program` built-in.
4. **Temp directory** (`try_create_temp_directory`) — mode/unique/cleanup.
   Acceptance: build/run temp trees and failure cleanup.
5. **Embedded runtime generation** — generator, generated `purr/embedded.ktn`,
   drift test, MIT notice. Acceptance: regenerate byte-identical.
6. **TOML subset parser** — `purr/manifest.ktn`, independent Kitten tests and
   exact errors. Acceptance: valid/unsupported corpus.
7. **`purr new`** — CLI, templates, modes, cleanup, name validation. Acceptance:
   exact tree/bytes/messages/modes, no license in templates.
8. **Kitten `purr` root and source-mode build/run** — `purr.ktn`, compiler
   linkage, in-process build, C compiler subprocess, temp dirs, current
   diagnostics and statuses. Acceptance: legacy parity suite subset.
9. **Project mode** — manifest discovery, target output, binary build/run.
   Acceptance: project cases; source mode unchanged.
10. **Library build** (if approved) — no-entry generation, object output.
11. **Full parity suite and Go CLI freeze** — all legacy and new cases, fixpoint,
    release-build check. Acceptance: parity report and `cmd/purr` frozen.

The recommended order matches the dependency chain: environment -> capture ->
file_info -> temp -> embedding -> TOML -> new -> build/run -> project ->
library -> freeze.

## 14. Out-of-scope commands and what each needs

The following are not designed here. This table records the next language/tool
surface each will eventually need; it does not duplicate the zero-dependency
proposal.

| Command / feature | Needed from Kitten/tooling |
|---|---|
| `purr add`, `purr remove` | TOML read/write (writer in 9.0), dependency model, resolver, lockfile, registry client |
| `purr install` | registry/network/TLS, archives, hashing, resolver, install layout, executable path |
| `purr test` | test discovery, project compilation, process exit aggregation, reporting |
| `purr fmt` | parser/printer, string/file rewriting, project traversal |
| `purr doc` | source comments, parser/printer, HTML/text output |
| `purr publish` | network/TLS, authentication, archives, manifest/lock validation |
| `purr lsp` | JSON protocol, stdio framing, incremental analysis, editor integration |
| `purr self` | executable path, process launch, installer/update protocol, component layout |
| `purr.lock` | stable lockfile format, hashing, resolver output, TOML writer |
| Registry | network/TLS, authentication, storage, version resolution, publishing |
| One-line installer | component layout, binary path, update/uninstall model; only requires that `purr` be a self-contained binary with an embedded runtime/templates |

## 15. Risks and arguments against this design

- **Primitive creep.** The environment, capture, temp-dir, and PATH primitives
  are user-visible language features, not private tool APIs. Each needs both
  pipelines, predeclared unions/reserved names, corpora, and runtime reviews.
- **Capture complexity.** Reading two pipes concurrently without deadlock,
  bounding output, NUL policy, and start-failure reporting are nontrivial C
  runtime work.
- **Process/temp race surface.** Temp directory names, cleanup on failure, and
  permissions are security-sensitive; tests must cover failure paths.
- **Help-text parity tension.** Adding `new` to global help necessarily changes
  a byte-identical surface. The design proposes preserving legacy help during
  parity and documenting the transition explicitly.
- **Project model before package model.** `purr.toml` name/version is useful for
  `new` and builds, but a real manifest will grow into dependencies and
  workspaces; the subset must be strict to avoid accidental incompatible
  semantics.
- **Library build is underspecified by the current C backend.** A no-entry
  codegen mode and object/link convention are real compiler work, not just a
  CLI branch.
- **Embedding is a build-system problem.** A checked-in generated module can
  drift; a generator must be part of the bootstrap and tested.
- **No Go reference for `new`/TOML.** Independent expectations are required;
  the design must not pretend byte parity where none exists.

## 16. Open questions (approved answers are recorded in Section 1.1)

1. During parity, should global `purr help` stay byte-identical to the Go CLI
   (and omit `new`), or may it list `new` and make help an approved parity
   exception?
2. Is the proposed strict package name/version grammar accepted, or should
   names be looser/Cargo-compatible?
3. Should library `purr build` emit `target/<name>.o` in the first implementation,
   or should library builds remain a documented deferred error while
   `purr new --lib` only scaffolds?
4. Should project discovery search parent directories (Cargo-like) immediately,
   or only the current directory in the first slice?
5. Is the proposed capture policy (stdin `/dev/null`, 16 MiB per stream, NUL is
   an error) accepted?
6. Should `purr new` run `git init`? This design says no; only `.gitignore` is
   written.
7. Should generated project files have any license choice prompt/file? This
   design says none and leaves the files entirely the user's.
8. Is the repository-root `purr.ktn` layout acceptable, or should the compiler
   modules be relocated under a common tool tree despite the churn?
