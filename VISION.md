# The Kitten Vision

```text
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
```

Kitten is a compiled, statically typed, garbage-collected programming language. This
document says what it is *for*, and where it is going.

`SPEC.md` is the source of truth for what the language actually does. Where this
document describes something that does not exist yet, it is describing a direction, not
a feature — `SPEC.md` wins over this file every time. `AGENTS.md` tracks the work in
progress.

---

## The one-sentence version

**A language pleasant enough to reach for by default, general enough to build almost
anything with, and shipped with an ecosystem good enough that you never have to leave
it.**

---

## Where the design comes from

Kitten is not invented in a vacuum. It is a deliberate blend of three languages the
author likes — and, just as importantly, a list of their traits it refuses to inherit.

| Language | What Kitten takes | What Kitten leaves behind |
| --- | --- | --- |
| **C#** | The *feel*. Curly braces, explicit types on declarations, familiar function and struct shapes, low ceremony. Someone who writes C# should be able to read Kitten on sight. | The enormous surface area, the platform coupling, the framework that arrives as a package deal. |
| **Go** | A language small enough to hold in your head, compilation fast enough to feel like a script, and the conviction that *tooling is part of the language* — building, testing, and formatting are not third-party concerns to be assembled. | The thin type system. Go cannot say "one of these three things". Kitten has enums and tagged unions, and an exhaustive `match` that proves you handled every case. |
| **Rust** | The ecosystem ambition, which is the thing most languages get wrong. One tool, one manifest, one command: `purr install` and the dependency is there, from a registry that belongs to the language. | The borrow checker as the price of entry. Kitten is garbage-collected. Memory is not the programmer's problem. |

The through-line: **familiar to read, small to learn, and never a reason to reach for
another language's tooling.**

---

## Positioning: a jack of all trades, CLI first

Kitten is not trying to be the best language at any single thing. It is trying to be a
language you can build *anything* in without feeling you chose wrong — with one
deliberate, temporary emphasis: **command-line tools come first and get the polish.**

That emphasis is not a compromise; it is the fastest route to everything else.

- **It is where a young language can actually win.** A CLI tool needs a small standard
  library and no ecosystem at all. It is exactly the situation where "wonderful
  language, empty registry" stops mattering.
- **It forces the fundamentals to become real.** Reading files, moving bytes, parsing
  arguments, choosing an exit status, spawning a process, writing output. Kitten's own
  toolchain is being written in Kitten, so the language's own critical path becomes the
  pressure test.
- **A native binary is what a CLI wants anyway.** Fast startup, no interpreter, nothing
  to install but the binary.
- **It is honest about where the ecosystem is.** Kitten can be genuinely excellent at
  command-line tools now and grow outward, instead of claiming to be excellent
  everywhere on day one.

Beyond that, nothing in the design should close a door. Servers, GUIs, games, data work,
scripting, developer tooling — all of it is in scope. Each of those drags in different
needs (event loops, threads, a far larger standard library, bindings to platform
toolkits), and Kitten's job is to grow into them without a rewrite. "Jack of all trades"
is the destination; "CLI first" is only the order of travel.

---

## The three independences

"Self-hosting" is used to mean at least three different things, and they are separate
projects with separate finish lines. Kitten wants all three, and it is worth being
precise about which one is under discussion.

### 1. Independent of Go

Today the compiler is written in Go. That is not the destination — it is the bootstrap.

Go was chosen for the first implementation for a reason: it builds fast, needs no
dependencies, and let the language be designed by *running* it rather than by arguing
about it. The Go compiler is the reference implementation, and it stays the reference
until a Kitten-written compiler reproduces its behaviour exactly.

The transition is being done as a **second implementation, written in Kitten, built one
stage at a time and checked against the Go reference byte for byte** — same output, same
diagnostics, same positions, same ordering. A corpus of thousands of inputs runs that
comparison. It is a slow way to write a compiler and the only way a self-hosting claim
means anything later.

**Finish line:** a compiler written in Kitten, compiled by itself, producing output
identical to the Go reference. At that point Go leaves the pipeline and becomes history.

**Status:** the C-level fixpoint is reached. Stage 1 is built by Go; stage 1 builds
stage 2 from the same source; stage 2 builds stage 3; and the generated C for
`selfhost/driver.ktn` is byte-identical across Go, stage 1, stage 2, and stage 3.
Stage 2 also reproduces the Go reference for every fixture and `tests/modules/`.
Go still builds stage 1 and the production CLI still uses the Go pipeline, so this
is not yet full independence from Go. The README's self-hosting section states the
claim and the remaining limits precisely.

### 2. Independent of C

Today Kitten emits C, and the host C compiler turns that C into machine code. This is a
legitimate place to stop — several serious languages have shipped that way for their
entire lives, and it buys portability for very little work.

It is not where Kitten stops. The target is Kitten emitting machine code or assembly
directly: no C in the pipeline, no translation step to reason about, and the ability to
control code generation, linking, and startup properly instead of approximating them
through a second language's conventions.

Two things make this a real project rather than a small one:

- **The backend has to be written**, including code generation, register allocation, and
  an object/linking story. That is the work.
- **The runtime is also currently C** — the garbage collector integration and the string,
  array, and map primitives. A compiler that emits machine code but still links a C
  runtime is independent of C in a technical sense and not in spirit, so the runtime
  belongs to this project too.

**Finish line:** Kitten source goes in, a native executable comes out, with no C
involved at any step.

**Status:** design authorized, implementation not started. The zero-dependency
design is in `proposals/zero-dependency-toolchain.md`; `AGENTS.md` still requires
separate authorization before implementation, precisely because it should not be
attempted before the language is finished enough to be worth compiling.

### 3. Independent ecosystem

The compiler can be self-hosted and native and still be useless to a newcomer if they
cannot install a library. This is the independence that shows up in the first five
minutes of using the language, and it is the one most languages leave until it is too
late.

**Finish line:** a developer creates a project, adds a dependency by name, builds, tests,
formats, documents, publishes, and shares all of it using tooling that belongs to
Kitten, backed by a registry that belongs to Kitten, and never once needs another
language's package manager.

**Status:** not started beyond `build` and `run`.

### They do not depend on each other

This matters, because it is easy to turn them into one intimidating blob:

- You can be self-hosted while still emitting C.
- You can have a native backend while still bootstrapping from Go.
- The ecosystem depends on neither — it needs a package format, a registry, and a tool,
  none of which care how the compiler is written or what it emits.

---

## The toolchain: `purr`

The language is **Kitten**. Source files are **`.ktn`**. The command is **`purr`**.

Why not `kitten`: the Kitty terminal already ships a `kitten` command, and a language
tool that fights a terminal emulator for the same word in `PATH` loses on every machine
that has both.

Why `purr`: it is short, it is unambiguously on-theme, it does not collide with anything
a developer is likely to have installed, and it reads well in the sentence the whole
ecosystem effort exists to make possible — `purr install`.

The trio is deliberately parallel to Rust's: **Kitten** / **`.ktn`** / **`purr`** sits
alongside **Rust** / **`.rs`** / **`cargo`**.

The project manifest is `purr.toml` and the lockfile is `purr.lock`, named after the
tool that reads them, the way `Cargo.toml` is named after Cargo.

---

## The ecosystem, concretely

"Like Cargo and crates" is the goal. Written out, that means a specific list of things,
none of which exist yet:

| Capability | What it has to feel like | Status |
| --- | --- | --- |
| Build and run | `purr build`, `purr run` — no build system to configure | partially: the binary exists, the project model does not |
| Scaffolding | `purr new hello-world` creates a directory with a manifest, a `.gitignore`, and `src/main.ktn`; `purr new --lib catnip` creates the same shape with `src/lib.ktn` | not started |
| Dependencies | `purr add serde`, version resolved and written to the manifest | not started |
| Install | `purr install <package>` fetches, builds, and puts a working binary on the path, in one command | not started |
| One-command setup | one command installs the whole toolchain — `purr`, the compiler, the language server, the formatter, and editor and shell completion — and one command updates it | not started |
| Reproducible builds | `purr.lock` pins exact versions; the same lockfile means the same build | not started |
| Tests | `purr test` compiles and runs a project's tests | not started |
| Formatting | `purr fmt` — one canonical style, no configuration to argue about, because the language defines the style | not started |
| Documentation | `purr doc` generates browsable documentation from source comments | not started |
| Editing | `purr lsp`, a language server giving diagnostics, completion, go-to-definition, hover, and find-references in any LSP-capable editor | not started |
| Shell and editor integration | completion for `purr` itself, syntax highlighting, and a maintained editor extension | not started |
| A registry | versioned, namespaced packages, published and fetched by `purr` itself | not started |
| Distribution | one command to build a release binary for a platform | not started |

Two constraints on all of it:

1. **Independence.** None of this may be a wrapper around another ecosystem. A Kitten
   package is not a Go module or an npm package wearing a hat; the registry, the
   manifest, and the resolver are Kitten's own.
2. **No configuration sprawl.** The defaults should be the answer. Formatting has one
   style. A new project has no build file to write. The tool asks for configuration only
   when something genuinely varies.

### Arriving installed and configured

An ecosystem that is pleasant to use *and* miserable to install is still a language people
bounce off. So the setup experience is a goal in its own right, not an afterthought:

- **One command from nothing to working.** Not a compiler here, a language server there,
  and an editor plugin found by searching. Installing Kitten installs the components that
  make it usable, at versions that are known to work together.
- **The editor works immediately.** Completion, diagnostics, go-to-definition, and hover
  are what make a language feel real in the first ten minutes, so they ship with the
  language rather than as a community project that may or may not keep up.
- **Updating is equally unceremonious.** The toolchain is a set of components, so pieces
  can be added or refreshed individually once installed, and no one has to assemble a
  working setup by hand.

### Scaffolding that looks familiar

`purr new` should produce a layout that anyone from the Rust ecosystem recognises on
sight — a manifest, a source directory, and nothing to configure:

```text
hello-world/
├── .gitignore
├── purr.toml
└── src/
    └── main.ktn
```

A library is the same shape with `src/lib.ktn` in place of `src/main.ktn`, so creating a
library and publishing it are the same workflow as creating and shipping a binary.

The similarity to Cargo is intentional and not cosmetic: the manifest is named after the
tool that reads it (`purr.toml` next to `Cargo.toml`), the source lives in `src/`, and the
default layout is the correct layout, so no project begins by writing build
configuration.


---

## What Kitten is deliberately not doing

Being clear about this is as useful as the goals:

- **Not chasing benchmark leadership.** Kitten should be fast enough that speed is never
  the reason you leave. It does not need to win.
- **Not adopting a borrow checker.** Automatic memory management was a deliberate choice.
  Kitten trades peak control for everyday clarity, and will not drift into Rust's
  ownership model later.
- **Not a research language.** No feature lands because it is novel. Features land
  because they make ordinary code shorter, clearer, or safer.
- **Not a superset of anything.** Kitten will not grow a mode for compiling C, C++, or
  JavaScript. It is its own language.
- **Not owned by a company.** The compiler is GPL-3.0-or-later, so it stays free: no
  vendor can close it off from everyone else, and no one can take it private.

---

## Principles that are not negotiable

These are the parts of the design that should survive any amount of growth:

- **Familiar.** Syntax that C-family developers read without a manual.
- **Concise.** Short declarations, no ceremony demanded for its own sake.
- **Explicit where it matters.** Variables and parameters state their types. No inference
  quietly changing what a line means.
- **No null.** Absence is a variant you declare and the compiler makes you handle.
- **No undefined behavior.** A failing operation is a defined, reported failure, not
  memory corruption.
- **No implicit conversions.** Conversions happen where they are written.
- **Exhaustive `match`.** Inspecting a variant covers every case or it does not compile.
- **Managed memory.** You allocate; the collector reclaims.
- **Native output.** Ahead-of-time compiled to a real executable, not interpreted.

---

## How far along this is

The frontend, the type checker, the C generator, and the runtime are implemented and
tested, and the compiler produces and executes native programs today. The second
implementation in Kitten covers the lexer, parser, checker, semantic model, module
loader, driver, and C generator, and reaches a C-level self-hosting fixpoint: Go,
stage 1, stage 2, and stage 3 generate byte-identical C for the compiler source, and
stage 2 reproduces the Go reference for every fixture.

The remaining limits are stated precisely in the README: Go still builds stage 1, a C
compiler and the temporary POSIX wrapper are required, the runtime is C, and the
`purr` ecosystem and native backend have not started. Nothing here claims full independence from Go.
