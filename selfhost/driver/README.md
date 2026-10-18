# The Kitten-written compiler driver and wrapper

`selfhost/driver.ktn` is the Kitten-written compiler driver. It is compiled by the
Go compiler into a stage-1 executable, then uses only the Kitten-written loader,
lexer, parser, checker, and C generator.

`selfhost/driver/kittenc.sh` is a temporary POSIX-shell wrapper that turns the
driver's C output into native executables. It is not the production `kitten`
pipeline and is not a self-hosting claim; `kitten build` and `kitten run` still
use the Go implementation. `purr` will replace the wrapper later.

## Driver

Usage:

```text
kitten-selfhost <absolute-source.ktn>
```

- It reads exactly one argument, `argv()[1]`, which must be an absolute path.
- It writes one complete C translation unit to standard output.
- Diagnostics go to standard error.
- Exit status is 0 on success, 1 for a load/check/generation failure, and 2 for a
  usage error (wrong argument count or a non-absolute path).
- It parses no options, writes no files, and invokes no processes.

The driver is at `selfhost/driver.ktn`, not `selfhost/driver/main.ktn`, because the
Kitten loader restricts imports to the root source's directory. From
`selfhost/driver.ktn` every import stays inside `selfhost/`.

Build stage 1 with the Go compiler:

```sh
kitten build selfhost/driver.ktn -o /tmp/kitten-stage1
/tmp/kitten-stage1 /absolute/path/to/main.ktn > program.c
```

The generated C is byte-identical to the Go pipeline's output for the same
program. The driver uses `print` for C and `eprint` for diagnostics, so the two
streams never mix.

## Wrapper

`selfhost/driver/kittenc.sh` is POSIX `sh`. It owns:

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

- `KITTENC_DRIVER` — the stage-1 executable. Default:
  `selfhost/driver/kitten-stage1`.
- `KITTEN_RUNTIME_DIR` — the directory holding `kitten_runtime.c` and
  `kitten_runtime.h`. Default: `runtime/csrc` in this repository.
- `CC` — the C compiler command. The wrapper splits it into words, as the Go CLI
  does. If unset, it tries `cc`, `gcc`, then `clang`.
- `TMPDIR` — the parent for temporary build directories.

Example:

```sh
kitten build selfhost/driver.ktn -o /tmp/kitten-stage1
KITTENC_DRIVER=/tmp/kitten-stage1 \
  selfhost/driver/kittenc.sh build tests/modules/main.ktn -o /tmp/modules
/tmp/modules
```

`run`:

```sh
KITTENC_DRIVER=/tmp/kitten-stage1 \
  selfhost/driver/kittenc.sh run program.ktn alpha "" "two words"
```

## Diagnostics and exclusions

The driver reproduces the Go CLI's diagnostic text for loader, checker, and
generator failures, including the file/line/column shape and the no-position
loader form (`<path>: import resolves outside the root directory <root>`).
Usage status is 2; compilation failures are status 1.

**Missing/unreadable files are not matched.** Until the postponed
`ReadFileResult`/`try_read_file` work lands, the Kitten loader calls the fatal
`read_file` built-in (SPEC.md Section 22.2). A missing import therefore
terminates the process with the runtime's fatal message rather than the Go
loader's `cannot read source file: ...` diagnostic. Missing-import parity is
explicitly out of scope for 5.6 and is not tested for text.

A relative path is accepted by the wrapper, which makes it absolute, matching the
Go CLI. The driver itself rejects a relative path with status 2 because it has no
working-directory primitive; that is the driver's documented contract, not a
wrapper-visible error.

Linux is verified. The wrapper is POSIX shell and avoids bashisms, but macOS is
not claimed as tested.

## Testing

`cmd/kitten/driver_test.go` builds stage 1 with the Go compiler and checks:

- every top-level fixture and `tests/modules/`: stage-1 stdout is byte-identical
  to `codegen.Generate` for the same loader/checker result, and wrapper-built
  executables print their `.expected` output under the compiler `CC` selects;
- wrapper argument passing, exit-status propagation, and default output naming;
- type, root syntax, imported-module syntax, and outside-root diagnostics against
  the Go CLI's stderr and status;
- no-argument usage against the Go CLI;
- the driver's relative-path status-2 contract; and
- that a missing import is fatal and nonzero, without comparing its text.

The wrapper is checked with `sh -n`; `shellcheck` is not installed in the test
environment.

Measured on the development machine with the Go-built `kitten` CLI: building
stage 1 from `selfhost/driver.ktn` took about 2.9 s and 70 MB RSS, producing a
304 KB executable. Stage 1 then emitted C for `lexer_proof`, `maps`, and
`tests/modules/` in 5–10 ms with about 5.4 MB RSS. These are measurements, not
performance guarantees.
