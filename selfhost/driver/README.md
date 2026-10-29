# The Kitten-written compiler driver and wrapper

`selfhost/driver.ktn` is the Kitten-written compiler driver. It is compiled by the
Go compiler into a stage-1 executable, then uses only the Kitten-written loader,
lexer, parser, checker, and C generator.

`selfhost/driver/purr-bootstrap.sh` is a temporary POSIX-shell wrapper that turns the
driver's C output into native executables. It is not the production `purr`
pipeline and is not a self-hosting claim; `purr build` and `purr run` still
use the Go implementation. `purr` will replace the wrapper later.

## Renamed in 6.0

The tool rename changed the wrapper and its environment names:
`kittenc.sh` became `purr-bootstrap.sh`, `KITTENC_DRIVER` became
`PURR_BOOTSTRAP_DRIVER`, and `KITTEN_RUNTIME_DIR` became `PURR_RUNTIME_DIR`.
The driver's usage prefix changed from `kitten-selfhost` to `purr-selfhost`.

## Driver

Usage:

```text
purr-selfhost <source.ktn>
```

- It reads exactly one argument, `argv()[1]`. It may be relative; the loader's
  `resolve_root` resolves a relative spelling against the current directory
  exactly as the reference's `canonicalPath` does with `filepath.Abs`, so a
  relative source produces the same unit as its absolute form.
- It writes one complete C translation unit to standard output.
- Diagnostics go to standard error.
- Exit status is 0 on success, 1 for a load/check/generation failure, and 2 for a
  usage error (a wrong argument count).
- It parses no options, writes no files, and invokes no processes.

The driver is at `selfhost/driver.ktn`, not `selfhost/driver/main.ktn`, because the
Kitten loader restricts imports to the root source's directory. From
`selfhost/driver.ktn` every import stays inside `selfhost/`.

Build stage 1 with the Go compiler:

```sh
purr build selfhost/driver.ktn -o /tmp/purr-stage1
/tmp/purr-stage1 /absolute/path/to/main.ktn > program.c
```

The generated C is byte-identical to the Go pipeline's output for the same
program. The driver uses `print` for C and `eprint` for diagnostics, so the two
streams never mix.

## Wrapper

`selfhost/driver/purr-bootstrap.sh` is POSIX `sh`. It owns:

- making source and output paths absolute;
- `build <src> [-o out]` and `run <src> [args...]`;
- temporary files and directories, with cleanup on success, failure, and signals;
- `CC` (including arguments) or discovery of `cc`, `gcc`, or `clang`;
- the runtime sources and Boehm GC flags, using `pkg-config bdw-gc` when
  available and `-lgc` otherwise;
- the default output name (source basename without `.ktn`, or `a.out`); and
- propagating the compiled program's exit status for `run`.

The wrapper's user-facing `build`/`run` usage and compile diagnostics mirror the
Go CLI. `run` additionally accepts program arguments after the source, even
though the usage line is kept identical to the Go CLI's.

Environment:

- `PURR_BOOTSTRAP_DRIVER` — the stage-1 executable. Default:
  `selfhost/driver/purr-stage1`.
- `PURR_RUNTIME_DIR` — the directory holding `kitten_runtime.c` and
  `kitten_runtime.h`. Default: `runtime/csrc` in this repository.
- `CC` — the C compiler command. The wrapper splits it into words, as the Go CLI
  does. If unset, it tries `cc`, `gcc`, then `clang`.
- `TMPDIR` — the parent for temporary build directories.

Example:

```sh
purr build selfhost/driver.ktn -o /tmp/purr-stage1
PURR_BOOTSTRAP_DRIVER=/tmp/purr-stage1 \
  selfhost/driver/purr-bootstrap.sh build tests/modules/main.ktn -o /tmp/modules
/tmp/modules
```

`run`:

```sh
PURR_BOOTSTRAP_DRIVER=/tmp/purr-stage1 \
  selfhost/driver/purr-bootstrap.sh run program.ktn alpha "" "two words"
```

## Diagnostics and limits

The driver reproduces the Go CLI's diagnostic text for loader, checker, and
generator failures, including the file/line/column shape and the no-position
loader form (`<path>: <message>`) for outside-root and read failures. Usage
status is 2; compilation failures are status 1.

**Read failures are matched.** The Kitten loader uses `try_read_file`, so a
missing, unreadable, or directory root or import produces the Go loader's
no-position `cannot read source file: ...` diagnostic. Both stage 1 and stage 2
are compared byte for byte with the Go CLI's stderr and exit status.

A relative path is accepted by both: the wrapper makes it absolute, and the
driver itself accepts it and resolves it with `resolve_root` the way the
reference's `canonicalPath` does, so the driver's run on the relative spelling is
byte-identical to its run on the absolute path.

Linux is verified. The wrapper is POSIX shell and avoids bashisms, but macOS is
not claimed as tested.

## Fixpoint

The C-level fixpoint is reached. Stage 1 is built by the Go compiler; stage 1
builds stage 2 from `selfhost/driver.ktn`; stage 2 builds stage 3; and the C for
`driver.ktn` is byte-identical across Go, stage 1, stage 2, and stage 3. Stage 2
also reproduces the Go reference for all nine fixtures and `tests/modules/`.

```sh
purr build selfhost/driver.ktn -o /tmp/stage1
PURR_BOOTSTRAP_DRIVER=/tmp/stage1 PURR_RUNTIME_DIR=runtime/csrc \
  selfhost/driver/purr-bootstrap.sh build selfhost/driver.ktn -o /tmp/stage2
PURR_BOOTSTRAP_DRIVER=/tmp/stage2 PURR_RUNTIME_DIR=runtime/csrc \
  selfhost/driver/purr-bootstrap.sh build selfhost/driver.ktn -o /tmp/stage3
/tmp/stage1 "$PWD/selfhost/driver.ktn" > /tmp/stage1.c
/tmp/stage2 "$PWD/selfhost/driver.ktn" > /tmp/stage2.c
/tmp/stage3 "$PWD/selfhost/driver.ktn" > /tmp/stage3.c
cmp /tmp/stage1.c /tmp/stage2.c
cmp /tmp/stage2.c /tmp/stage3.c
```

The Go reference C for `driver.ktn` is produced by
`loader.Load` → `checker.CheckModules` → `codegen.Generate` and is also compared
byte for byte by `TestKittenFixpoint`.

Measured on the Linux development host: stage 1 built by Go in about 2.5 s;
stage 2 and stage 3 built by the previous stage in about 2.9–3.0 s each; each
driver emitted the 928,224-byte C unit in about 0.4–0.5 s. The three stage
binaries were byte-identical in that run, but the claim rests on the generated C,
not on binary reproducibility.

## Testing

`cmd/purr/driver_test.go` builds stage 1 with the Go compiler and checks:

- every top-level fixture and `tests/modules/`: stage-1 stdout is byte-identical
  to `codegen.Generate` for the same loader/checker result, and wrapper-built
  executables print their `.expected` output under the compiler `CC` selects;
- wrapper argument passing, exit-status propagation, and default output naming;
- type, root syntax, imported-module syntax, and outside-root diagnostics against
  the Go CLI's stderr and status;
- no-argument usage against the Go CLI;
- the driver's relative-path status-2 contract; and
- that missing roots and missing imports match the Go CLI in stderr text and status.

`TestKittenFixpoint` in the same file additionally builds stages 1–3, compares the
C for `driver.ktn` across Go and all stages, checks stage-2 determinism and a
one-byte negative control, replays the 10 fixture roots through stage 2, and
repeats the error and usage comparisons with stage 2. It is skipped only under
`go test -short`.

The wrapper is checked with `sh -n`; `shellcheck` is not installed in the test
environment.

Measured on the development machine with the Go-built `purr` CLI: building
stage 1 from `selfhost/driver.ktn` took about 2.9 s and 70 MB RSS, producing a
304 KB executable. Stage 1 then emitted C for `lexer_proof`, `maps`, and
`tests/modules/` in 5–10 ms with about 5.4 MB RSS. These are measurements, not
performance guarantees.
