# Kitten

```text
            /\_____/\
           /  O   O  \
          |     ^     |
          |  ~     ~  |
           \  \___/  /
            \_______/
           /         \
          /           \
          \           /
           \_________/
```

Kitten is a compiled, statically typed, garbage-collected programming language that
builds native executables. It is being built to be **pleasant to write, general enough to
build almost anything with, and backed by an ecosystem that belongs to the language** —
the way Cargo and crates.io are not an afterthought to Rust but part of what writing Rust
feels like.

| | |
| --- | --- |
| **Kitten** | the language |
| **`.ktn`** | source files |
| **`purr`** | the command-line tool |

Its syntax comes from **C#**, its smallness and its tooling culture come from **Go**, and
its ecosystem ambition comes from **Rust**. There is a deliberate head start on
command-line tools, because that is where a young language can actually win. The full
story is in [`VISION.md`](VISION.md).

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

**Status: early, and honest about it.** The language is at `v0.1 (provisional)`. A
complete compiler toolchain exists — lexer, parser, type checker, C code generator, C
runtime — and it produces and executes native programs. There is no standard library
beyond a handful of built-ins, no package manager, no formatter, no language server, and
no native backend that skips C. The language specification is provisional and says so;
the parts of it that are undecided are listed explicitly rather than invented in code.

> The tool is called `purr`. The binary is still built as `kitten` from `cmd/kitten`
> while the rename is pending; see [Building and running](#building-and-running).

---

## Design principles

Readable before clever, familiar before novel:

- **Familiar and concise.** `int total = 0;`, not `let mut total: i32 = 0;`. No `var`
  keyword, no ceremony demanded for its own sake.
- **Explicit where it matters.** Variables and parameters carry their types, and there is
  no type inference. What a value is, is written down.
- **No null.** Absence is an explicit variant of a union you declare, and the compiler
  makes you handle it. Nothing silently means "nothing".
- **No undefined behavior, no implicit conversions, no casts.** Operations are checked
  before the program runs; an operation that cannot be represented is a compile error, and
  a runtime failure is a defined, reported failure rather than memory corruption.
- **Exhaustive `match`.** Inspecting an enum or a tagged union covers every case or it
  does not compile, and the compiler proves the arms are exhaustive.
- **Managed memory.** Programs allocate and never free by hand.
- **Native output.** Ahead-of-time compiled to a real executable, not interpreted.

`VISION.md` explains what these principles are for, what Kitten deliberately refuses to
become, and the three kinds of independence the project is working toward.

---

## A quick taste

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

Tagged unions carry typed payloads, `match` binds them, and there is no way to read a
variant you did not check:

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

## What works today

| Component | State |
| --- | --- |
| Lexer, parser, AST | complete for the specified language |
| Type checker | complete for the specified language, with diagnostics and positions |
| C code generator | complete for the specified language; emits C, compiled by the host C compiler |
| C runtime | strings, arrays, maps, file reading, printing, and the garbage collector |
| The compiler CLI | `build` and `run`, including multi-file programs |
| Module system | `import`, cross-module visibility, `public` / `private` |
| Core types | `int`, `float`, `bool`, `string`, `struct`, `enum`, tagged `union`, arrays, maps |
| Built-ins | `print`, `length`, `substring`, `has`, `read_file` |
| Fixtures | every program under `tests/` compiles, runs, and matches its expected output |

**Not there yet:** the standard library beyond the built-ins above, generics, traits or
interfaces, closures, package management, a formatter, a language server, a debugger,
cross-compilation, and a native backend that does not go through C. The specification
lists every deferred decision; nothing is silently invented.

---

## The ecosystem

A language is only as good as the code people can find and reuse in it — and the
ecosystem belongs to Kitten, not to another language's package manager. One tool owns the
whole workflow:

| Piece | What it does | Status |
| --- | --- | --- |
| `purr build`, `purr run` | compile a source file, or compile and execute it | **works** (as `kitten`) |
| `purr test` | compile and run a project's tests | planned |
| `purr fmt` | one canonical formatting, no options to argue about | planned |
| `purr add`, `purr remove` | manage dependencies in a manifest | planned |
| `purr install` | fetch, build, and install a package in one command | planned |
| `purr publish` | publish a package to the registry | planned |
| `purr.toml`, `purr.lock` | manifest and lockfile for reproducible builds | planned |
| a registry | namespaced, versioned, with docs generated from source | planned |

None of the ecosystem pieces exist yet beyond `build` and `run`. They are the roadmap,
not a claim.

---

## Self-hosting, and how it is being done

The long-term goal is that Kitten compiles its own compiler. Today the compiler is written
in Go, and that is deliberate: it is the bootstrap, and it stays the reference.
`VISION.md` explains why this is only one of three separate independences the project is
working toward.

The path to self-hosting is a **second implementation of the compiler, written in
Kitten**, built one stage at a time and checked against the Go implementation *byte for
byte*. Every stage has the same shape:

1. The Kitten-written stage processes a program.
2. The Go reference implementation processes the same program.
3. A differential harness compares the two outputs — not "does it work", but "is it the
   same output", including diagnostics, positions, ordering, and formatting.
4. A corpus of thousands of inputs runs that comparison, and any mismatch is investigated
   until its cause is understood.

That is a slower way to write a compiler and a much faster way to *know* one is correct.
It is also the only way a self-hosting claim means anything.

**Where the second implementation stands.** These stages are written in Kitten and
verified against the Go reference over their corpora:

| Stage | Verified against the Go reference |
| --- | --- |
| Lexer | 772 inputs |
| Parser + AST | 262 inputs |
| Checker + semantic model | 1,862 inputs, byte-identical |
| Declaration, expression, statement, and literal-pool emission | whole translation units, byte-for-byte |
| Entry point and translation-unit assembly | 38 whole units, 1,158 lines |
| Generated programs executed | 12 programs compiled and run under GCC and Clang, outputs compared |

**What it cannot compile yet, and why it is a short list.** Seven expression kinds are
still unimplemented in the Kitten-written generator — `assign`, `member`,
`struct-literal`, `variant-literal`, `new-array`, `new-map`, and `index` — plus a
Kitten-written module loader. Each is an exact, known port of a Go function that already
exists; none of them is an open design question. Of the nine fixtures in `tests/`, two
compile end-to-end through the Kitten-written backend today, and the other seven report
precisely which of those seven kinds they need.

The Go compiler remains the bootstrap and the only pipeline the CLI uses. Kitten is
**not** self-hosting yet, and this README will not say it is until a Kitten-built compiler
compiles Kitten's own source and reproduces the reference behavior.

---

## Repository layout

```
SPEC.md        the language specification — the source of truth for semantics
VISION.md      what Kitten is for: goals, positioning, and the road to independence
AGENTS.md      how the project is built: process, architecture, current phase
lexer/         Go: source text -> tokens
parser/        Go: tokens -> AST
checker/       Go: AST -> typed, resolved program
codegen/       Go: checked program -> C
runtime/       Go wrapper around the C runtime (csrc/)
loader/        Go: source files -> modules, resolving imports
cmd/kitten/    Go: the compiler CLI and the differential test harnesses
selfhost/      Kitten, in Kitten: the second implementation and its drivers
tests/         .ktn fixtures with their expected program output
```

`selfhost/` mirrors the compiler stage by stage: a lexer, a parser, a checker, a semantic
model, and a C code generator, each with a driver that prints what it produced so the
Go-side harnesses can compare it with the reference.

## Building and running

You need Go, a C compiler (`cc`, `gcc`, or `clang`), and Boehm GC (`libgc`) with its
development headers.

```sh
git clone https://github.com/etokiyra/kitten.git
cd kitten

# Build the compiler. The tool is `purr`; its source directory is still named
# cmd/kitten while that rename is pending.
go build -o purr ./cmd/kitten

# Compile and run a program.
./purr run tests/fizzbuzz.ktn

# Compile it to an executable.
./purr build tests/fizzbuzz.ktn -o fizzbuzz
./fizzbuzz
```

## Testing

```sh
go test ./...              # everything
CC=gcc go test ./...       # the same suite with a chosen C compiler
CC=clang go test ./...
```

The suite includes both ordinary unit tests and the differential harnesses described
above: the Kitten-written lexer, parser, checker, semantic model, code generator, and
translation-unit emitter are each compared against the Go implementation over a large
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
