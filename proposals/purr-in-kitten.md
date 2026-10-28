# `purr` written in Kitten

> **ADOPTED (plan); implementation in progress.**
>
> The human approved this plan on 2026-10-18. Implementation proceeds one commit
> per step in the order below. The `try_getenv`/`EnvResult`,
> `try_run_capture`/`CapturedRunResult`, `try_file_info`/`FileInfoResult`,
> `try_create_temp_directory`/`TempDirectoryResult`, embedded runtime
> generation, the `purr.toml` subset parser, and the `purr new` module/probe are
> implemented so far; no `purr` command described here is implemented unless
> explicitly marked **already implemented**.
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
| 3. `try_file_info` | **implemented in 9.3** |
| 4. `try_create_temp_directory` | **implemented in 9.4** |
| 5. Embedded runtime generation | **implemented in 9.5** |
| 6. TOML subset parser | **implemented in 9.6** |
| 7. `purr new` | **implemented in 9.7 as module + probe; CLI dispatch in 9.8a** |
| 8a. Kitten `purr` entry point, help, `new` | **implemented in 9.8a; build added in 9.8b** |
| 8b. Kitten `purr` build | **implemented in 9.8b** |
| 8c. Kitten `purr` run + full parity | **run implemented; full parity suite remains** |
| 9. Project mode | **implemented: current-directory binary build/run; library builds deferred** |
| 10. Library object mode | proposed, deferred |
| 11. Freeze / name swap | proposed (9.9 milestone) |

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

### 3.2 `try_file_info` (implemented in 9.3; replaces `try_find_program`)

**Design override:** the earlier `try_find_program` built-in is **dropped**.
PATH search will be written in Kitten on top of `try_getenv("PATH")` plus this
general file-information primitive. No find-program runtime primitive is
implemented.

**Needed for:** PATH search, project discovery, `purr new` existence checks,
checking whether `target` is a directory, and future installer/`self` work.

**Implemented shape.**

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

- `lstat` classifies the path itself. `ENOENT` or `ENOTDIR` is `Missing`; any
  other `lstat` failure is `Failed` with `lstat <path>: <reason>`.
- `is_symlink` is true only when the path itself is a symbolic link. A symlink
  is followed with `stat` for kind, size, and executable. A dangling target
  (`ENOENT`/`ENOTDIR` from `stat`) is `Ok` with only `is_symlink` true, all other
  booleans false, and size 0. Other `stat` failures (for example `ELOOP` or
  `EACCES`) are `Failed` with `stat <path>: <reason>`.
- `is_file` and `is_directory` describe the followed target; FIFOs, sockets, and
  devices give both false. `size` is the regular-file target's byte size, else
  0.
- `executable` is `access(path, X_OK) == 0`, using the kernel's real-UID/GID
  check. A directory with search permission is executable. PATH-style program
  lookup must require `is_file && executable`.
- No file descriptor is opened. The runtime ABI returns `0` for `Ok`, `1` for
  `Missing`, and `2` for `Failed`, with named constants in the header:

```c
int ktn_rt_try_file_info(const ktn_string *path,
                         bool *is_file,
                         bool *is_directory,
                         bool *is_symlink,
                         bool *executable,
                         int64_t *size,
                         const ktn_string **message);
```

**Blocks:** PATH search and therefore `build`/`run`; `new` existence checks and
project discovery also use it once project mode exists.

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

### 3.4 `try_create_temp_directory` (implemented in 9.4)

**Needed for:** `build` writing `program.c`/runtime sources, and `run` writing a
temporary executable, without a C `mkdtemp` call exposed to user code.

**Implemented shape.**

```text
try_create_temp_directory(string prefix) -> TempDirectoryResult
```

```kitten
union TempDirectoryResult {
    Ok { string path; }
    Failed { string message; }
}
```

- The base is non-empty `$TMPDIR`, otherwise `/tmp`. A non-empty but unusable
  `$TMPDIR` fails; there is no fallback to `/tmp`. A relative `$TMPDIR` is made
  absolute against `getcwd` using the same behavior as
  `try_current_directory`, and the returned path is always absolute.
- The prefix is 1 to 64 bytes from ASCII `[A-Za-z0-9._-]`; `..` is additionally
  rejected even though its bytes are allowed. Any prefix outside that rule fails
  with `invalid temporary directory prefix` before any filesystem access, so
  path separators and traversal cannot enter the name.
- Creation is atomic through `mkdtemp` with the template
  `<base>/<prefix>.XXXXXX`, giving a six-character random suffix and mode
  exactly 0700 regardless of umask. The runtime clears the umask only around
  the atomic call and restores it immediately; thread safety is not required,
  and a post-creation chmod would race.
- Success returns the absolute path as a fresh GC-managed string. The runtime
  registers no cleanup hook; the caller empties and removes the directory with
  `try_remove`.
- A missing base reports `stat <base>: <reason>`, matching Go's
  `os.MkdirTemp`. A regular-file or unwritable base reports
  `mkdir <base>/<prefix>.XXXXXX: <reason>`, the template form of Go's
  `mkdir <base>/<prefix><random>: <reason>`. Reasons use the shared
  Go-compatible errno mapping.
- No file descriptor is opened and no signal handler is installed. Thread-safe
  use is not required.
- The future libc-free runtime must source the random suffix from
  `getrandom(2)` or `/dev/urandom` and create the directory with `mkdir(2)` in
  mode 0700 while no umask can reduce it, retrying on `EEXIST`. This
  requirement is recorded but not implemented here.

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
go build -o /tmp/purr-bootstrap ./cmd/purr
/tmp/purr-bootstrap build purr.ktn -o /tmp/purr-kitten
```

The second command is a normal Go-pipeline build of the Kitten source. After
the Kitten `purr` exists, it can build `purr.ktn` itself and the result can be
compared with the Go-built binary's generated C for the fixpoint. The
repository-root `purr/` source directory means a binary cannot also be written
as repository-root `purr`; this command uses external paths.

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

## 7. `purr.toml`: the smallest useful TOML subset (implemented in 9.6)

The first manifest is deliberately smaller than TOML. A file outside the
subset is rejected, never guessed at. The parser is `purr/manifest.ktn`, plain
Kitten with no imports and no file I/O.

### 7.1 Accepted grammar

```text
file     := (blank | comment)* "[package]" ws* comment? eol (blank | comment | entry)*
entry    := key ws* "=" ws* string ws* comment? eol
key      := "name" | "version"
string   := '"' (char | escape)* '"'
blank    := ws* eol
comment  := ws* "#" anything-to-eol
ws       := space or tab
eol      := LF | CRLF | end of file
```

- Exactly one `[package]` header, before both keys. `name` and `version` are
  each required exactly once, in either order.
- A final line may lack a newline. `[package]` may be followed by spaces/tabs or
  a comment. Leading whitespace before a header or key is rejected.
- A bare CR not followed by LF is an error.
- String values admit any byte except raw LF, raw CR, and NUL. The escapes are
  `\\`, `\"`, `\n`, `\t`, and `\r`. Other escapes, literal strings,
  multiline strings, arrays, inline tables, numbers, booleans, dates, dotted
  keys, and quoted keys are rejected.

### 7.2 Value rules

- `name`: 1..64 bytes; first byte lowercase ASCII; the rest lowercase letters,
  digits, `_`, or `-`; must not end with `-` or `_`.
- `version`: exactly `MAJOR.MINOR.PATCH`; each part 1..9 ASCII digits, with no
  leading zero unless the part is exactly `0`. No pre-release, build metadata,
  `v` prefix, or numeric conversion.
- A name/version error is reported at the opening quote of the value.

### 7.3 API and errors

```kitten
public union ManifestResult {
    Ok { string name; string version; }
    Failed { string message; }
}

public fn parse_manifest(string text) -> ManifestResult
```

Errors are exactly:

```text
purr.toml:<line>:<column>: <message>
```

Line and column are one-based; columns count bytes. CRLF is one line break and
its CR contributes no column. The first error stops the parser. At end of file,
the position is just past the last byte. Bytes at or above 0x7F and control
bytes in echoed slots render as lowercase `\xNN`. A control byte after an
invalid escape is echoed as `\\xNN` (two backslashes) by design, pinned by the
parser tests.

Messages:

```text
expected [package]
unexpected table <bracketed text>
expected key name or version
unknown key <key>
expected = after key <key>
expected a string value for <key>
unterminated string
invalid escape \<c>
stray carriage return
unexpected text after value
duplicate key <key>
missing key name
missing key version
duplicate [package] table
invalid package name <value>: <reason>
invalid version <value>: <reason>
```

Name reasons: `must not be empty`, `must be at most 64 bytes`,
`must start with a lowercase letter`,
`may contain only lowercase letters, digits, _ and -`,
`must not end with - or _`.

Version reasons: `expected MAJOR.MINOR.PATCH`,
`a version part must have 1 to 9 digits`,
`a version part must not have a leading zero`.

The `ManifestResult` union is an ordinary user declaration in
`purr/manifest.ktn`; it does not use or collide with a predeclared prelude name.

### 7.4 Parser design and tests

The parser is one byte-index loop with a small state struct: no regex, no TOML
dependency, no map iteration, no file access, and no recursion proportional to
input. String decoding collects source runs and escape pieces and joins them
with a balanced concatenation tree, so a 1 MiB value stays linear. Name/version
validation is separate and stops at the first violated rule.

`purr/manifest_probe.ktn` imports `manifest.ktn` (the loader's within-root rule
prevents a `tools/` probe from importing `../purr/manifest.ktn`). It reads
`argv()[1]`, prints `ok <name> <version>` on success, or the exact error line on
stderr, with statuses 0, 1, and 2 for success, read/parse failure, and usage.

The test suite pins 164 inputs with exact outputs, including every message at
least twice, CRLF/no-final-newline cases, comments, all value boundaries,
echoed controls and non-ASCII, bare CR, BOM, and duplicate/missing forms. It
also runs 5,000 fixed-seed mutations through both pipelines and an independent
Go oracle, compares the probe's generated C byte for byte between pipelines,
checks embedded-runtime regression, and measures 256 KiB versus 1 MiB inputs
for linear behavior.

### 7.5 Growth without breaking files

Adding a new optional key or table is backward compatible: every currently
accepted file remains accepted with unchanged meaning. Unknown constructs
continue to be rejected until the parser is deliberately extended. Adding a
required key or changing a value's meaning requires a manifest version decision
and is not done silently. Additional keys, `[dependencies]`, arrays, writers,
and project discovery remain deferred.

## 8. `purr new` (module and probe implemented in 9.7; CLI in 9.8)

### 8.1 API and behavior

```kitten
public union NewResult {
    Created { string message; }
    Failed { string message; }
}

public fn new_project(string name, bool library) -> NewResult
```

`name` is both the package name and the directory name and must satisfy the
manifest name rules exactly. Validation calls
`manifest_name_error_text` from `purr/manifest.ktn`; the rules and `\xNN`
echoing are not duplicated. It happens before any filesystem access.

The probe `purr/new_probe.ktn` implements the command-shaped surface:

```text
new_probe [--lib] <name>
```

`--lib` may appear before or after the name. Any other flag, a second name, a
missing name, or `--` is a usage error with exit 2. The probe prints `Created`
messages to stderr, nothing to stdout, and exits 0; `Failed` messages go to
stderr with exit 1.

Because a name beginning with `-` is a flag under this grammar, `-x` is a usage
error through the probe; the module API test calls `new_project("-x", false)` to
pin the invalid-name behavior.

Success messages, unquoted with exactly five leading spaces:

```text
     Created binary (application) hello-world
     Created library catnip
```

Destination failure:

```text
error: destination hello-world already exists
```

Filesystem failures are:

```text
error: <runtime message>
```

where `<runtime message>` is exactly the message the failed `try_*` built-in
returned, with paths relative as built by `new_project`.

### 8.2 Creation and cleanup

1. Validate the name.
2. `try_file_info(name)`: `Missing` continues; `Ok` for any file, directory, or
   symlink (including a dangling symlink) is the destination-exists error;
   `Failed` is `error: <message>`. Nothing is touched in these paths.
3. `try_create_directory(name)`. A failure is reported and nothing is cleaned: a
   concurrent creator owns that path.
4. `try_create_directory(name + "/src")`.
5. Write `.gitignore`, `purr.toml`, then `src/main.ktn` or `src/lib.ktn`.

Only paths created by this invocation are tracked, in creation order. On any
failure after step 3 they are removed in reverse order with `try_remove` only:
files first, then `src`, then `name`. A failed write that nevertheless created a
file is detected and tracked. No path outside the created list is removed, no
removal is recursive, and a non-empty directory is left in place.

If cleanup fails, removal continues for the remaining paths. The original error
is followed by one line per leftover:

```text
       note: could not remove <path>: <runtime message>
```

with seven leading spaces.

Modes honor the process umask; there is no chmod. `try_create_directory` requests
`0755` and `try_write_file` requests `0644`, so umasks `022`, `077`, and `000`
produce `0755/0644`, `0700/0600`, and `0755/0644` respectively.

### 8.3 Exact templates

`.gitignore`:

```text
/target
```

`purr.toml`:

```text
[package]
name = "<name>"
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

Every file ends with one LF, uses four-space indentation, and has no tabs,
license notice, README, lockfile, `target/`, or `git init`.

### 8.4 Tests

Both pipelines and both compilers cover binary/library success, exact trees,
bytes, and modes under `022`, `077`, and `000`; `--lib` before and after; both
generated projects parsing/compiling/running; generated-C parity; deterministic
bytes; destination files, directories, dangling symlinks, and directory
symlinks; read-only parents; a real step-4 failure via `umask 0777`; real steps
5a/5b/5c write failures through byte-granular `RLIMIT_FSIZE` with `SIGXFSZ`
ignored so cleanup runs; a model race and a model cleanup-note variant; usage
and invalid-name cases; and Clang UBSan.

The CLI entry point, argument parsing in the final binary, and help remain for
9.8. `purr new` is therefore a tested module and probe, not yet a user-facing
command.

### 8.5 Kitten `purr` sidecar (9.8a)

`purr.ktn` is the repository-root entry point and `purr/cli.ktn` parses argv.
The sidecar binary is always named `purr-kt`; the Go CLI remains the
authoritative `purr`. In 9.8a the sidecar imported only `new.ktn` and stayed
small. 9.8b adds `build.ktn` and links the in-process compiler modules, so the
current sidecar is substantially larger. 9.8c implements `run`; the full parity
suite remains.

Implemented in 9.8a:

- no arguments: usage on stderr, exit 2;
- `help`, `-h`, `--help`: usage on stdout, exit 0, extra arguments ignored;
- unknown command: `purr: unknown command "<name>"`, blank line, usage, stderr,
  exit 2;
- `new`: the 9.7 module and probe behavior, with output and statuses unchanged;
- `build`: the in-process 9.8b implementation, with Go-compatible argument
  parsing, diagnostics, streams, and statuses; and
- `run`: shared Go-compatible argument parsing, `-o` rejection, a `purr-run.`
  temporary directory, an in-process build to `<temp>/program`, execution with
  `try_run`, direct child-status propagation, and non-recursive cleanup notes.
  A signaled child reports `128 + signal`; the Go CLI observes 255 because
  `ExitCode()` is `-1` and `os.Exit(-1)` is 255 on POSIX.

The usage text is the Go text plus two lines, in this order:

```text
  purr new [--lib] <name>               Create a new project
  purr help                             Show this help
```

The sidecar uses the deterministic program name `purr`, never `argv()[0]`.

## 9. Project mode and output naming (implemented for binary projects)

Project mode activates only for `purr build`/`purr run` with no source argument,
no output option, and a `purr.toml` in the **current directory**. It never
searches parent directories. The implementation lives in `purr/project.ktn` and
is dispatched from `purr/cli.ktn`.

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
  slice lands, `purr build` on a library reports `purr build: library builds are
  not implemented yet` and `purr run` on a library reports `purr run: library
  projects cannot be run`. `purr new --lib` still scaffolds the layout.
- No parent-directory manifest search, workspace, profiles, features, or
  dependency sections in 9.0.
- A malformed manifest is reported after the command prefix with the parser's
  positioned message. Both source files report `project has both src/main.ktn
  and src/lib.ktn`; neither reports `project has neither src/main.ktn nor
  src/lib.ktn`.
- A failed build removes a partial output it created and removes a `target`
  directory it created once it is empty. A pre-existing target, its contents,
  and a pre-existing output are never removed.

`target/` is the only generated project directory; `.gitignore` contains
`/target`.

## 10. Runtime source embedding and installed layout (implemented in 9.5)

The Go CLI embeds `runtime/csrc` with `//go:embed`. The Kitten toolchain now has
the equivalent in `tools/embed_runtime.ktn`, a Kitten program with no imports:

1. `embed_runtime <repo-root>` reads
   `<repo-root>/runtime/csrc/kitten_runtime.h` and `kitten_runtime.c` with
   `try_read_file`;
2. it verifies each Kitten string length against the `try_file_info` file size,
   rejects NUL and invalid UTF-8 with the file named, and rejects any byte class
   it cannot reproduce;
3. it writes `<repo-root>/purr/embedded.ktn`, creating `purr/` only when
   `try_file_info` says it is missing;
4. the generated module exposes exactly
   `embedded_runtime_header()` and `embedded_runtime_source()`, with private
   chunk helpers, bounded literal lines, and a balanced concatenation tree;
5. a drift test regenerates the module through the Go pipeline, the Kitten
   pipeline, and an independent Go reference, and byte-compares all three with
   the committed file.

Regeneration from the repository root:

```sh
purr build tools/embed_runtime.ktn -o /tmp/embed_runtime
/tmp/embed_runtime .
```

**Escaping and shape.** The supported Kitten escapes are `\n`, `\t`, `\"`, and
`\\`. Valid UTF-8 and raw CR/control bytes are copied literally; NUL and invalid
UTF-8 fail loudly. Each source literal is at most 84 escaped bytes, so every
generated line is under 100 columns, and each chunk returns at most 4096 decoded
bytes. The public functions combine chunks in a balanced tree, so materializing
a string copies O(n log n) bytes rather than the quadratic cost of a growing
left-to-right chain. The generated file is 98,173 bytes and 1,110 lines, with a
maximum line length of 99.

**Repository finding.** `.gitignore` already anchored the root binary as
`/purr`, but Git still treats that pattern as matching a directory named
`purr/`, which silently ignored the generated module. 9.5 adds
`!/purr/` so only the root binary remains ignored. The hygiene test runs
`git check-ignore -q` and `git check-ignore -v` and requires no match for
`purr/embedded.ktn`; the file is tracked after the commit.

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

**9.8b intentional deviations.** The Kitten `build` implementation uses
`try_run_capture`, whose streams are separate. Its combined compiler text is
captured stdout followed by captured stderr, with no separator. Go's
`CombinedOutput` interleaves the streams; real GCC and Clang write diagnostics
to stderr only, so their output agrees. A compiler killed by a signal appears as
`exit status <128+n>` in the Kitten CLI; Go prints `signal: <name>`.
`try_run_capture` also cannot distinguish that synthesized status from a real
exit of the same value. The private build directory uses the prefix
`purr-build.` rather than Go's `purr-build-`. The compiler is invoked through a
fixed `/bin/sh` `cd` plus `exec` so its working directory matches Go's
`command.Dir`; the compiler argument vector is unchanged. Cleanup is
non-recursive and reports leftover paths as `note: could not remove ...` lines
instead of silently removing the tree. These are tool-behavior differences, not
language changes.

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
3. **File information** (`try_file_info`) — implemented in 9.3; PATH search is
   then written in Kitten, with no `try_find_program` built-in.
4. **Temp directory** (`try_create_temp_directory`) — implemented in 9.4;
   mode/unique/cleanup. Acceptance: build/run temp trees and failure cleanup.
5. **Embedded runtime generation** — implemented in 9.5; Kitten generator,
   generated `purr/embedded.ktn`, three-way drift test, MIT notice.
6. **TOML subset parser** — implemented in 9.6; `purr/manifest.ktn`,
   pinned and randomized tests, exact errors. Acceptance: valid/unsupported
   corpus.
7. **`purr new`** — implemented in 9.7 as `purr/new.ktn` plus the
   `purr/new_probe.ktn` driver; exact tree/bytes/messages/modes, cleanup notes,
   no license in templates. The CLI entry point remains 9.8.
8. **Kitten `purr`**
   8a. **Entry point, help, and `new`** — implemented in 9.8a as
   `purr.ktn` + `purr/cli.ktn`; `build` was added in 9.8b, `run` remains a
   stub.
   8b. **In-process `build`** — implemented in 9.8b; compiler linkage, C
   compiler subprocess, temp dirs, current diagnostics and statuses.
   8c. **`run` and full parity suite** — next; compile to a temporary executable,
   run it, preserve statuses, and pass the complete Go/Kitten parity suite.
9. **Project mode** — manifest discovery, target output, binary build/run.
   Acceptance: project cases; source mode unchanged.
10. **Library build** (if approved) — no-entry generation, object output.
11. **Freeze and name swap (9.9)** — all legacy and new cases, fixpoint,
    release-build check. Acceptance: parity report, Go CLI frozen, and the
    Kitten `purr` takes over the `purr` name.

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
