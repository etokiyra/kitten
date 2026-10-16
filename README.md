# Kitten

Kitten is a small, statically typed, garbage-collected programming language that
compiles ahead of time to native executables.

It is being built with two goals that pull in the same direction: **make a language
that is genuinely pleasant to write code in**, and **grow an ecosystem that belongs
to the language** — a package manager, a registry, a test runner, a formatter — the
way Cargo and crates.io are not an afterthought to Rust but part of what writing
Rust feels like.

```
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
runtime — and `kitten build` / `kitten run` produce and execute native programs. There
is no standard library beyond a handful of built-ins, no package manager, no
formatter, no language server. The language specification is provisional and says so;
the sections of it that are undecided are listed explicitly rather than invented in
code.

---

## What we are trying to build

### A language that is nice to write

Readable before clever. The syntax is familiar to anyone coming from C#, Java, Go, or
Rust — curly braces, explicit types, no ceremony:

- **Familiar and concise.** `int total = 0;`, not `let mut total: i32 = 0;`. No `var`
  keyword, no required semicolon dance around one-line bodies.
- **Explicit where it matters.** Variables and parameters carry their types, and
  there is no type inference. What a value is, is written down.
- **No null.** Absence is an explicit variant of a union you declare, and the compiler
  makes you handle it. There is no value that silently means "nothing".
- **No undefined behavior, no implicit conversions, no casts.** Operations are checked
  before the program runs; an operation that cannot be represented is a compile error,
  and a runtime failure is a defined, reported failure rather than memory corruption.
- **Exhaustive `match`.** Inspecting an enum or a tagged union covers every case or it
  does not compile, and the compiler proves the arms are exhaustive.
- **Managed memory.** Programs allocate and never free by hand; a production
  garbage collector does that.
- **Native output.** Programs compile ahead of time to native executables, not to an
  interpreter or a bytecode VM.

### An ecosystem that belongs to the language

A language is only as good as the code people can find and reuse in it. The plan is
that the toolchain owns the whole developer workflow rather than leaving it to a
patchwork of external tools:

| Piece | What it does | Status |
| --- | --- | --- |
| `kitten build`, `kitten run` | compile a source file, or compile and execute it | **works** |
| `kitten test` | compile and run a package's tests, with a real runner | planned |
| `kitten fmt` | one canonical formatting, no options to argue about | planned |
| `kitten add`, `kitten remove` | manage dependencies in a manifest | planned |
| `kitten publish` | publish a package to the registry | planned |
| `kitten.toml`, `kitten.lock` | manifest and lockfile for reproducible builds | planned |
| a registry | namespaced, versioned, with docs generated from source | planned |

The intent is that a Kitten project is created, built, tested, documented, and shared
with the language's own tools, and that nothing about that workflow depends on another
language's ecosystem.

None of the ecosystem pieces exist yet. They are the roadmap, not a claim.

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
$ kitten run taste.ktn
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

## What works today

| Component | State |
| --- | --- |
| Lexer, parser, AST | complete for the specified language |
| Type checker | complete for the specified language, with diagnostics and positions |
| C code generator | complete for the specified language; emits C, compiled by the host C compiler |
| C runtime | strings, arrays, maps, file reading, printing, and the garbage collector |
| `kitten build` / `kitten run` | complete, including multi-file programs |
| Module system | `import`, cross-module visibility, `public` / `private` |
| Core types | `int`, `float`, `bool`, `string`, `struct`, `enum`, tagged `union`, arrays, maps |
| Built-ins | `print`, `length`, `substring`, `has`, `read_file` |
| Fixtures | every program under `tests/` compiles, runs, and matches its expected output |

**Not there yet:** the standard library beyond the built-ins above, generics, traits
or interfaces, closures, package management, a formatter, a language server, a
debugger, cross-compilation, and a native backend that does not go through C.
The specification lists every deferred decision; nothing is silently invented.

---

## Self-hosting, and how it is being done

The long-term goal is that Kitten compiles its own compiler. Today the compiler is
written in Go, and that is deliberate: it is the bootstrap, and it stays the
reference.

The path to self-hosting is a **second implementation of the compiler, written in
Kitten**, built one stage at a time and checked against the Go implementation
*byte for byte*. Every stage has the same shape:

1. The Kitten-written stage processes a program.
2. The Go reference implementation processes the same program.
3. A differential harness compares the two outputs — not "does it work", but "is it
   the same output", including diagnostics, positions, ordering, and formatting.
4. A corpus of thousands of inputs runs that comparison, and any mismatch is
   investigated until its cause is understood.

That is a slower way to write a compiler and a much faster way to *know* one is
correct. It is also the only way a self-hosting claim means anything.

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

**What it cannot compile yet, and why it is a short list.** Seven expression kinds
are still unimplemented in the Kitten-written generator — `assign`, `member`,
`struct-literal`, `variant-literal`, `new-array`, `new-map`, and `index` — plus a
Kitten-written module loader. Each is an exact, known port of a Go function that
already exists; none of them is an open design question. Of the nine fixtures in
`tests/`, two compile end-to-end through the Kitten-written backend today and the
other seven report precisely which of those seven kinds they need.

The Go compiler remains the bootstrap and the only pipeline `kitten build` and
`kitten run` use. Kitten is **not** self-hosting yet, and this README will not say it
is until a Kitten-built compiler compiles Kitten's own source and reproduces the
reference behavior.

---

## Repository layout

```
SPEC.md        the language specification — the source of truth for semantics
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

`selfhost/` mirrors the compiler stage by stage: a lexer, a parser, a checker, a
semantic model, and a C code generator, each with a driver that prints what it
produced so the Go-side harnesses can compare it with the reference.

## Building and running

You need Go, a C compiler (`cc`, `gcc`, or `clang`), and Boehm GC (`libgc`) with its
development headers.

```sh
git clone https://github.com/etokiyra/kitten.git
cd kitten

# Build the compiler.
go build -o kitten ./cmd/kitten

# Compile and run a program.
./kitten run tests/fizzbuzz.ktn

# Compile it to an executable.
./kitten build tests/fizzbuzz.ktn -o fizzbuzz
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
corpus, and every fixture is compiled, executed, and compared against its expected
output under both GCC and Clang.

## How the project is developed

- `SPEC.md` is the source of truth for language semantics. If code and the
  specification disagree, the specification wins until it is deliberately changed.
- Semantics are never changed silently. An undecided question is recorded as deferred
  in the specification and raised, not invented in code.
- The compiler is built as cleanly separated phases — lexer, parser, loader, checker,
  code generator, runtime — and a package must not take on another phase's work.
- Every feature has tests, and every stage of the second implementation is verified
  against the reference rather than trusted.
- Features are added in authorized phases, one coherent change per commit.

## License

Kitten is free software: you can redistribute it and/or modify it under the terms of
the GNU General Public License as published by the Free Software Foundation, either
version 3 of the License, or (at your option) any later version. See `LICENSE`.

**The toolchain is GPL-3.0-or-later.** The compiler, the parser, the checker, the code
generator, and the command-line tools stay free software. No one can take this
implementation, close it, and ship it privately.

**The runtime is MIT.** This is the deliberate exception. `runtime/csrc/` is the part
that gets compiled and linked into every program you build, so a permissive license
there is what keeps **the code you write in Kitten yours**: keep it private, sell it, or
ship it under any terms you like. The GPL on the compiler does not reach your programs —
using a compiler is not the same as being part of it, which is why the GPL on GCC has
never required anyone to publish the programs they compile with it.

Going forward, anything that ends up linked into a compiled program — the runtime, and
any standard library — is MIT; everything else is GPL-3.0-or-later.
