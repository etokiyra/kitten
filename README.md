# Kitten

<p align="center">
  <a href="LICENSE"><img alt="License: GPL-3.0-or-later + MIT" src="https://img.shields.io/badge/license-GPL--3.0--or--later%20%2B%20MIT-blue.svg"></a>
  <img alt="Status: early, v0.1 provisional" src="https://img.shields.io/badge/status-early%20%C2%B7%20v0.1%20provisional-orange.svg">
  <img alt="Bootstrap: Go" src="https://img.shields.io/badge/bootstrap-Go-00ADD8.svg">
  <img alt="Self-hosting: C-level fixpoint" src="https://img.shields.io/badge/self--hosting-C--level%20fixpoint-yellow.svg">
  <img alt="Tool: purr" src="https://img.shields.io/badge/tool-purr-ff69b4.svg">
  <img alt="Platform: Linux | macOS" src="https://img.shields.io/badge/platform-linux%20%7C%20macOS-lightgrey.svg">
</p>

```text
.==============================================.
|                                              |
|                           .'\                |
|                          //  ;               |
|                         /'   |               |
|        .----..._    _../ |   \               |
|         \'---._ `.-'      `  .'              |
|          `.    '              `.             |
|            :            _,.    '.            |
|            |     ,_    (() '    |            |
|            ;   .'(().  '      _/__..-        |
|            \ _ '       __  _.-'--._          |
|            ,'.'...____'::-'  \     `'        |
|           / |   /         .---.              |
|     .-.  '  '  / ,---.   (     )             |
|    / /       ,' (     )---`-`-`-.._          |
|   : '       /  '-`-`-`..........--'\         |
|   ' :      /  /                     '.       |
|   :  \    |  .'         o             \      |
|    \  '  .' /          o       .       '     |
|     \  `.|  :      ,    : _o--'.\      |     |
|      `. /  '       ))    (   )  \>     |     |
|        ;   |      ((      \ /    \___  |     |
|        ;   |      _))      `'.-'. ,-'` '     |
|        |    `.   ((`            |/    /      |
|        \     ).  .))            '    .       |
|     ----`-'-'  `''.::.________:::mx'' ---    |
|                                              |
|                                              |
|                                              |
'=============================================='
```

**Kitten** is a compiled, statically typed, garbage-collected programming language that
builds native executables. It is being built to be *pleasant enough to reach for by
default, general enough to build almost anything with, and backed by an ecosystem that
belongs to the language.*

<p align="center">
  <b>Kitten</b> — the language &nbsp;·&nbsp; <b><code>.ktn</code></b> — the files &nbsp;·&nbsp; <b><code>purr</code></b> — the tool
</p>

Its syntax comes from **C#**, its smallness and its tooling culture come from **Go**, and
its ecosystem ambition comes from **Rust**. There is a deliberate head start on
command-line tools, because that is where a young language can actually win.

> **Read next:** [`VISION.md`](VISION.md) — what Kitten is for, what it refuses to become,
> and the three kinds of independence it is working toward.

---

## Status

**Early, and honest about it.** The language is `v0.1 (provisional)`, which means the
specification is deliberately not frozen and the parts that are undecided are *listed* as
undecided rather than quietly invented in code.

| | |
| --- | --- |
| **Real today** | a full compiler frontend, type checker, C generator and C runtime; a CLI that compiles and runs native programs, including multi-file ones; a Kitten-written compiler that reaches a C-level self-hosting fixpoint |
| **Not there yet** | full independence from Go (Go still builds stage 1), a standard library, packages, a formatter, a language server, and a backend that skips C |
| **How decisions are made** | `SPEC.md` is the source of truth; code does not get to override it |

---

## Why Kitten

Most languages make you choose: simple but limited, or powerful but a career to learn;
delightful to write, or equipped with an ecosystem you can actually use. Kitten is an
attempt to refuse that trade.

- **Familiar, so there is nothing to unlearn.** Curly braces, explicit types on
  declarations, functions and structs that look like they should.
- **Small, so it fits in your head.** A handful of keywords, no inference quietly
  changing what a line means, no ceremony demanded for its own sake.
- **Safe by construction.** No null, no undefined behavior, no implicit conversions,
  exhaustive `match` on every variant, and a garbage collector instead of manual memory.
- **Native, so it ships as one file.** Ahead-of-time compiled to a real executable, not
  interpreted, not a bytecode VM.
- **Backed by its own tooling.** Building, testing, formatting, documenting, installing,
  publishing, and editing all belong to `purr` — not to a patchwork of other languages'
  tools.

---

## A quick taste

```kitten
fn main() {
    for (int number = 1; number <= 15; number++) {
        if (number % 15 == 0) {
            print("FizzBuzz");
        } else if (number % 3 == 0) {
            print("Fizz");
        } else if (number % 5 == 0) {
            print("Buzz");
        } else {
            print(number);
        }
    }
}
```

Tagged unions carry typed payloads, `match` binds them, and there is no way to read a
variant you did not check:

```kitten
enum Shape {
    Circle,
    Square,
    Triangle
}

struct Point {
    float x;
    float y;
}

fn area(float side) -> float {
    return side * side;
}

fn describe(Shape shape) -> string {
    match (shape) {
        Circle { return "circle"; }
        Square { return "square"; }
        Triangle { return "triangle"; }
    }
}

fn main() {
    Point origin = Point { x = 0.0, y = 0.0 };
    Shape shape = Shape.Square;
    print(describe(shape), area(4.0));

    int[] values = new int[3];
    values[0] = 10;
    map[string]string names = new map[string]string;
    names["first"] = "kitten";
    print(values[0], names["first"], length("kitten"), origin.x);
}
```

```
$ purr run taste.ktn
square 16
10 kitten 6 0
```

Recursive unions work the same way:

```kitten
union Expression {
    IntegerLiteral { int value; }
    Add { Expression left; Expression right; }
    Missing
}

fn evaluate(Expression expression) -> int {
    match (expression) {
        IntegerLiteral(value) { return value; }
        Add(left, right) { return evaluate(left) + evaluate(right); }
        Missing { return 0; }
    }
}
```

---

## Getting Kitten

### The goal: one command

Going from nothing to a working, autocompleting Kitten toolchain should be one line. Not
a compiler here, a language server there, and an editor plugin found by searching:

<!-- purr new and purr run work today; purr install awaits the registry. -->
```sh
purr new hello-world     # scaffold a project
purr run                 # build and run it
purr install <package>   # fetch, build, and install something from the registry
```

The intended shape is that installing Kitten installs *everything* — `purr`, the
compiler, the language server, the formatter, and completion for your shell and editor —
and that updating it later is equally unceremonious. The toolchain is a set of
components, so they can be added or refreshed individually once installed, but a new
developer never has to assemble them by hand.

### Scaffolding: `purr new`

Creating a project produces the layout you already recognise, with a manifest, a
source directory, and nothing to configure.

<!-- As implemented by purr new; matching Cargo's conventions. Library builds
     still report a deferred error. -->
```text
$ purr new hello-world
     Created binary (application) hello-world

hello-world/
├── .gitignore
├── purr.toml
└── src/
    └── main.ktn

$ purr new --lib catnip
     Created library catnip

catnip/
├── .gitignore
├── purr.toml
└── src/
    └── lib.ktn
```

A library is the same shape with `src/lib.ktn` instead of `src/main.ktn`, so publishing
one is a build step rather than a separate workflow.

### Today: build from source

You need Go, a C compiler (`cc`, `gcc`, or `clang`), and Boehm GC (`libgc`) with its
development headers.

```sh
git clone https://github.com/etokiyra/kitten.git
cd kitten

# Build the bootstrap outside the repository root; purr/ is a source directory.
go build -o /tmp/purr-bootstrap ./cmd/purr

# Compile and run a program.
/tmp/purr-bootstrap run tests/fizzbuzz.ktn

# Compile it to an executable.
/tmp/purr-bootstrap build tests/fizzbuzz.ktn -o fizzbuzz
./fizzbuzz

# Install the Kitten-written `purr`; the install location is outside the
# repository, where purr/ is not a source directory.
/tmp/purr-bootstrap build purr.ktn -o ~/.local/bin/purr
```

The tool you install as `purr` is the one written in Kitten — the sidecar that
owns `new`, `build`, and `run` end to end. The Go CLI at `./cmd/purr` remains in
the repository as the frozen bootstrap and testing reference; building the
Kitten `purr` still requires it. `purr new` and `purr run` work today; the
registry commands await the package ecosystem.

---

## What works today

| Component | State |
| --- | --- |
| Lexer, parser, AST | complete for the specified language |
| Type checker | complete for the specified language, with diagnostics and positions |
| C code generator | complete for the specified language; emits C, compiled by the host C compiler |
| C runtime | strings, arrays, maps, file reading, printing, and the garbage collector |
| Kitten `purr` CLI, `purr new`, and `purr.toml` | design adopted in `proposals/purr-in-kitten.md`, implemented through 9.9: embedded runtime, the `purr.toml` parser, `purr new`, and the `help`/`new`/`build`/`run` sidecar with binary project mode; the Go/Kitten parity suite and freeze run are complete, the Go CLI is frozen as the bootstrap/reference, and the Kitten-written sidecar takes over the `purr` name |
| The compiler CLI | `build` and `run`, including multi-file programs |
| Module system | `import`, cross-module visibility, `public` / `private` |
| Core types | `int`, `float`, `bool`, `string`, `struct`, `enum`, tagged `union`, arrays, maps |
| Built-ins | `print`, `eprint`, `exit`, `length`, `argv`, `has`, `substring`, `read_file`, `try_read_file`, `try_write_file`, `try_write_bytes`, `try_create_directory`, `try_remove`, `try_list_directory`, `try_run`, `try_run_capture`, `try_current_directory`, `try_getenv`, `try_file_info`, `try_create_temp_directory`, `int_from_byte`, `byte_from_int`, `string_to_bytes`, `bytes_to_string` |
| Fixtures | every program under `tests/` compiles, runs, and matches its expected output |

**Not there yet:** the standard library beyond the built-ins above, generics, traits or
interfaces, closures, package management, a formatter, a language server, a debugger,
cross-compilation, and a backend that does not go through C. The specification lists every
deferred decision; nothing is silently invented.

### The Kitten-written `purr`

`purr.ktn` (with `purr/cli.ktn`, `purr/build.ktn`, `purr/new.ktn`, and
`purr/project.ktn`) is the toolchain written in Kitten. It supports
`help`, `new`, `build`, and `run`. `build` links the Kitten-written compiler
frontend and invokes the C compiler in process; `run` builds into a temporary
directory, executes the program, propagates its status, and cleans up. `new`
includes its dedicated `-h`/`--help` text. With no source argument and a
`purr.toml` in the current directory, `build` and `run` use the manifest and
`src/main.ktn`, write `target/<name>`, and run that executable. Library builds
remain a documented deferred error.

After the 9.9 freeze, this sidecar takes over the `purr` name: the Go CLI at
`./cmd/purr` is frozen as the bootstrap and testing reference, and the tool you
install as `purr` is the binary built from `purr.ktn` — verified end to end
from a clean checkout by `TestKittenPurrReleaseBuild` in
`cmd/purr/purr_kt_test.go`. The sidecar's messages, help, and usage always name
the program `purr` deterministically; "purr-kt" survives only as the
differential harnesses' label for pipeline-specific test builds. Building it
still requires the Go bootstrap; full independence from Go is a separate goal
that has not started. The Go/Kitten parity suite from
`proposals/purr-in-kitten.md` Section 12 is implemented in `cmd/purr/`:
command grammar and usage, the compiler and `pkg-config` discovery
and argv, every fixture and `tests/modules/` (with byte-identical generated C
through a logged fake compiler), source and compiler failure diagnostics,
`run` statuses, streams, and cleanup, `new`, `purr.toml`, and project mode.

---

## The toolchain and the ecosystem

A language is only as good as the code people can find and reuse in it — and that
ecosystem belongs to Kitten, not to another language's package manager. One tool owns the
whole workflow, from the first `purr new` to publishing, and it is designed to arrive
installed and configured.

### The `purr` command

| Piece | What it does | Status |
| --- | --- | --- |
| `purr build`, `purr run` | compile a source file, or compile and execute it | **works** |
| `purr new` | scaffold a project or a library with the standard layout; `new` works in the Kitten-written `purr`, while library builds report a documented deferred error | works (scaffolding) |
| `purr add`, `purr remove` | manage dependencies in the manifest | planned |
| `purr install` | fetch, build, and install a package in one command | planned |
| `purr test` | compile and run a project's tests | planned |
| `purr fmt` | one canonical formatting, no options to argue about | planned |
| `purr doc` | generate browsable documentation from source | planned |
| `purr publish` | publish a package to the registry | planned |
| `purr lsp` | a language server: diagnostics, completion, go-to-definition, hover, references | planned |
| `purr self`, `purr component` | update the toolchain, add or refresh components | planned |
| One-line installer | rustup-style install of `purr` and the whole toolchain: components, update model, completion, language server | planned, not designed; design after the zero-dependency path |
| `purr.toml`, `purr.lock` | manifest and lockfile for reproducible builds | planned |
| a registry | namespaced, versioned packages, fetched by `purr` itself | planned |

### Editor and shell integration

Being easy to *install* is not the same as being easy to *live in*. The goal is that
Kitten arrives with the things people otherwise spend an afternoon wiring up:

- **A language server** behind `purr lsp`, so any LSP-capable editor gets diagnostics,
  completion, go-to-definition, hover, and find-references.
- **Editor support**, including a maintained extension for the common editors and
  syntax highlighting that does not lag behind the language.
- **Shell completion** for `purr` itself — subcommands, flags, and package names.

### The registry

Packages are versioned, namespaced, and fetched by `purr` itself, with documentation
generated from source. Nothing about using the ecosystem may require another language's
package manager, and a Kitten package is never a foreign package wearing a costume.

Beyond `build`, `run`, and `new`, this does not exist yet. It is the roadmap, not a claim.

---

## Self-hosting

The long-term goal is that Kitten compiles its own compiler. Today the compiler is written
in Go, and that is deliberate: it is the bootstrap, and it stays the reference.
[`VISION.md`](VISION.md) explains why that is only one of three separate independences the
project is working toward.

The path is a **second implementation of the compiler, written in Kitten**, built one
stage at a time and verified against the Go implementation **byte for byte** — same
output, same diagnostics, same positions, same ordering.

<details>
<summary><b>How the differential verification works</b></summary>

Every stage has the same shape:

1. The Kitten-written stage processes a program.
2. The Go reference implementation processes the same program.
3. A differential harness compares the two outputs — not "does it work", but "is it the
   same output", down to diagnostics, positions, ordering, and formatting.
4. A corpus of thousands of inputs runs that comparison, and any mismatch is investigated
   until its cause is understood.

That is a slower way to write a compiler and a much faster way to *know* one is correct.
It is also the only way a self-hosting claim means anything.

| Stage | Verified against the Go reference |
| --- | --- |
| Lexer | 1,076 inputs |
| Parser + AST | 289 inputs |
| Checker + semantic model | 2,030 inputs, byte-identical |
| Declaration, expression, statement, and literal-pool emission | whole translation units, byte-for-byte |
| Entry point and translation-unit assembly | 174 whole units, 10,307 lines |
| Module loader | 33 targeted trees, 165 relative root spellings, `tests/modules/` in both root forms, 23 multi-file corpus programs |
| Full Kitten-written path | `tests/modules/` loaded, checked, emitted, compiled, run, and matched |
| `argv()` runtime | 3 argument vectors through both pipelines, under GCC and Clang |
| `eprint()` runtime | stdout and stderr captured separately through both pipelines |
| `exit()` runtime | 6 scenarios, checking stdout, stderr, and status through both pipelines |
| Kitten driver + wrapper | 10 fixture roots, byte-identical C, native builds under GCC and Clang |
| Two-stage fixpoint | Go, stage 1, stage 2, and stage 3 C for `driver.ktn` byte-identical; stage 2 matches all 10 roots |
| Generated programs executed | 11,158 program runs under GCC and Clang, outputs compared, plus 170 Clang UBSan runs |
| Defined arithmetic/bitwise/byte runtime | wrapping, shifts, byte arrays, conversions, and failures through both pipelines |
| `try_read_file` runtime | existing, empty, missing, directory, and unreadable paths through both pipelines |
| File/directory runtime | writes, bytes, truncation, mkdir, remove, sorted listing, Go-identical errors, and permission paths through both pipelines |
| Descriptor leak check | 2,000 failing writes under a 16-descriptor limit |
| `try_run` runtime | statuses, signals, inherited environment/cwd/fds, argument preservation, ordering, and start failures through both pipelines; a 2,000-run leak loop and zombie check |
| `try_current_directory` runtime | real, symlinked, deep, deleted, and `$PWD`-misleading cases through both pipelines |
| Compiler-reserved `<runtime>` | harness-only injection, runtime-only intrinsics, raw memory, syscall, helper source, and C lowering through both pipelines |
| `try_getenv` runtime | set, empty, unset, removed, invalid names, large/complex values, PATH, and runtime-module id interaction through both pipelines |
| `try_run_capture` runtime | streams, statuses, signal, stdin, arguments, environment/cwd, start failures, NUL/limits, deadlock, and leak/zombie checks through both pipelines |
| `try_file_info` runtime | files, directories, modes, sizes, symlinks, dangling links, loops, missing, long/empty paths, FIFOs, permissions, and 10,000 calls through both pipelines |
| `try_create_temp_directory` runtime | `TMPDIR` selection, relative/symlinked bases, invalid prefixes, mode under four umasks, uniqueness, 1,000 creations, Go-compatible failures, and a descriptor-limited loop through both pipelines |
| Embedded runtime module | 98,173-byte generated `purr/embedded.ktn`; three-way byte identity, generated-file round trip, embedded-runtime fixture, NUL/invalid-UTF-8 failures, and Clang UBSan |
| `purr.toml` parser | 164 pinned inputs, 5,000 fixed-seed randomized cases through both pipelines, exact positions/messages, independent Go oracle, and 1 MiB linear-time checks |
| `purr new` module/probe | binary and library trees, exact bytes/modes, dedicated `-h`/`--help` text, generated-project compilation, destination/read-only/step-4/step-5 failures, cleanup notes, race model, and Clang UBSan through both pipelines |
| `purr-kt` entry point | help/dispatch, Go-usage diff, real `new` parity, in-process `build` argument/discovery/pkg-config/failure parity, fixtures and modules, generated projects, binary project mode, capstone fixpoint, C parity, size/timing, and Clang UBSan |

</details>

**The C-level fixpoint is reached.** Stage 1 is built from
`selfhost/driver.ktn` by the Go compiler. Stage 1 builds stage 2 from the same
source, and stage 2 builds stage 3. The generated C for `driver.ktn` is
byte-identical across the Go reference and all three stages. Stage 2 also
reproduces the Go reference C for all nine fixtures and `tests/modules/`, and the
resulting programs match their expected output under GCC and Clang. That is the
precise claim: **the Kitten compiler, built by the Kitten compiler, reproduces
the Go reference's generated C byte for byte.**

**What is still not independent.** Go still builds stage 1; the bootstrap has not
left the pipeline. A C compiler and the temporary POSIX wrapper
(`selfhost/driver/purr-bootstrap.sh`) are still required, and the runtime is still C.
There is no known behavioral gap against the Go reference in the covered
corpora. Missing, unreadable, and directory imports are **matched** by the
Kitten loader, which now uses `try_read_file`; its diagnostics agree with the Go
loader's. Relative source roots are matched as well: the loader resolves a
relative spelling against the kernel's current directory exactly as
`filepath.Abs` does, and the wrapper still absolutizes paths for good measure.
Goal 2 (no C) and Goal 3 (the `purr`
ecosystem) have not started.

To reproduce the fixpoint by hand:

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

The Go compiler remains the bootstrap and the only pipeline the CLI uses. This
README does not claim that the CLI is Kitten-built or that the compiler is fully
independent of Go.

---

## Repository layout

```text
SPEC.md        the language specification — the source of truth for semantics
VISION.md      what Kitten is for: goals, positioning, and the road to independence
AGENTS.md      how the project is built: process, architecture, current phase
lexer/         Go: source text -> tokens
parser/        Go: tokens -> AST
checker/       Go: AST -> typed, resolved program
codegen/       Go: checked program -> C
runtime/       Go wrapper around the C runtime (csrc/)
loader/        Go: source files -> modules, resolving imports
cmd/purr/    Go: the compiler CLI and the differential test harnesses
selfhost/      Kitten, in Kitten: the second implementation and its drivers
tests/         .ktn fixtures with their expected program output
```

`selfhost/` mirrors the compiler stage by stage: a lexer, a parser, a checker, a semantic
model, a module loader, a compiler driver with a temporary POSIX wrapper, and a C code
generator, each with a driver that prints what it produced so the Go-side harnesses can
compare it with the reference.

## Testing

```sh
go test ./...              # everything
CC=gcc go test ./...       # the same suite with a chosen C compiler
CC=clang go test ./...
```

The suite includes both ordinary unit tests and the differential harnesses described
above: the Kitten-written lexer, parser, checker, semantic model, module loader, compiler
driver, and code generator are each compared against the Go implementation over a large
corpus, and every fixture is compiled, executed, and compared against its expected output
under both GCC and Clang.

## How the project is developed

- `SPEC.md` is the source of truth for language semantics. If code and the specification
  disagree, the specification wins until it is deliberately changed.
- Semantics are never changed silently. An undecided question is recorded as deferred in
  the specification and raised, not invented in code.
- The compiler is built as cleanly separated phases — lexer, parser, loader, checker, code
  generator, runtime — and a package must not take on another phase's work.
- Every feature has tests, and every stage of the second implementation is verified
  against the reference rather than trusted.
- Features are added in authorized phases, one coherent change per commit.

## License

Kitten is free software: you can redistribute it and/or modify it under the terms of the
GNU General Public License as published by the Free Software Foundation, either version 3
of the License, or (at your option) any later version. See `LICENSE`.

**The toolchain is GPL-3.0-or-later.** The compiler, the parser, the checker, the code
generator, and the command-line tools stay free software. No one can take this
implementation, close it, and ship it privately.

**The runtime is MIT.** This is the deliberate exception. `runtime/csrc/` is the part that
gets compiled and linked into every program you build, so a permissive license there is
what keeps **the code you write in Kitten yours**: keep it private, sell it, or ship it
under any terms you like. The GPL on the compiler does not reach your programs — using a
compiler is not the same as being part of it, which is why the GPL on GCC has never
required anyone to publish the programs they compile with it.

Going forward, anything that ends up linked into a compiled program — the runtime, and any
standard library — is MIT; everything else is GPL-3.0-or-later.

---

<div align="center">
<pre>
 ,_     _
 |\\_,-~/
 / _  _ |    ,--.
(  @  @ )   / ,-'
 \  _T_/-._( (
 /         `. \
|         _  \ |
 \ \ ,  /      |
  || |-_\__   /
 ((_/`(____,-'
</pre>
</div>
