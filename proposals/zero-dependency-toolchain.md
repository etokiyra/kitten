# The zero-dependency toolchain

> **PROPOSED, not adopted.**
>
> This is a design for approval. It changes no code, no tests, and no SPEC.md
> semantics. Goal 2 (independence from C) is authorized for **design only** in
> Phase 7.0; implementation requires separate, later authorization.
>
> Date: 2026-10-18. Starting commit: `09a60a2`.

## 1. The target

A user installs one thing: `purr`. Typing

```sh
purr build src/main.ktn -o hello
./hello
```

must work on Linux x86-64 with **no Go, no C compiler, no libc, no Boehm GC, no
assembler, and no linker** on the machine. `purr` itself is a static ELF
executable written in Kitten. It lexes, parses, checks, lays out, emits machine
code, writes a static ELF executable, and runs it for `purr run`.

This design does **not** cover the installer, package registry, `purr new`,
formatter, language server, or the rest of Goal 3. Those remain independent
roadmap items.

## 2. Audit: what the current toolchain depends on

### 2.1 The C runtime

The runtime is **867 lines / 31,631 bytes**: `kitten_runtime.c` is 552 lines /
18,989 bytes and `kitten_runtime.h` is 315 lines / 12,642 bytes. It exports
**33 functions** (`runtime/csrc/kitten_runtime.h:59-313`):

GC/initialization/allocation:

- `ktn_rt_init`, `ktn_rt_alloc`

Strings:

- `ktn_rt_string_equal`, `ktn_rt_string_concat`, `ktn_rt_string_append`,
  `ktn_rt_string_length`, `ktn_rt_string_at`, `ktn_rt_substring`

Arrays:

- `ktn_rt_array_new`, `ktn_rt_array_at`, `ktn_rt_array_length`

Arguments and process:

- `ktn_rt_set_args`, `ktn_rt_argv`, `ktn_rt_exit`, `ktn_rt_fail`

Maps:

- `ktn_rt_map_new`, `ktn_rt_map_set`, `ktn_rt_map_at`, `ktn_rt_map_has`,
  `ktn_rt_map_length`

Files:

- `ktn_rt_read_file`

Printing:

- `ktn_rt_print_int/float/bool/string/space/newline`
- `ktn_rt_eprint_int/float/bool/string/space/newline`

Generated code can call all **33** directly; the compiler's own generated C
exercises all 33, because it uses every built-in somewhere.

External dependencies in `runtime/csrc/kitten_runtime.c`:

- libc headers: `<errno.h>`, `<stdio.h>`, `<stdlib.h>`, `<string.h>`.
- libc functions: `fflush`, `fputs`, `fputc`, `fprintf`, `printf`, `fwrite`,
  `snprintf`, `exit`, `fopen`, `fclose`, `fseek`, `ftell`, `fread`, `strlen`,
  `memcpy`, `strerror`; `errno`; `FILE`, `stdout`, `stderr`, `SEEK_END`,
  `SEEK_SET`.
- Boehm GC: `GC_INIT` and `GC_MALLOC` (`kitten_runtime.c:78-83`).
- C integer/size types through the header: `int64_t`, `uint64_t`, `size_t`,
  `bool`, `NULL`, `INT64_MAX`, `SIZE_MAX`.

There is no libc `malloc`, `free`, or manual free path: all allocation is
`GC_MALLOC`.

### 2.2 What generated C needs from the C compiler

Generated code is not just expressions. It relies on the C compiler for:

- C ABI and calling convention between generated functions and the runtime.
- Struct/union layout, field offsets, `sizeof`, and alignment. The generator
  uses `sizeof(<type>)` at runtime in `ktn_rt_array_new` and `ktn_rt_map_new`
  calls (`codegen/emit.go:628`, `codegen/emit.go:648`,
  `selfhost/codegen/expressions.ktn:580`, `:617`), so layout is currently
  delegated entirely to C.
- C operators, short-circuit evaluation, casts, comma expressions, compound
  literals (`&(T){ ... }` used for map set), `NULL`, `_Noreturn`, and the C
  entry point.
- `static const ktn_string` literal objects
  (`codegen/codegen.go:198-210`).
- The C compiler's linker and startup code.

### 2.3 What `purr build` invokes

`cmd/purr/main.go:211-289` loads, checks, generates C, then:

- creates a temporary build directory;
- writes generated C, `kitten_runtime.c`, and `kitten_runtime.h`;
- discovers `CC` or `cc`/`gcc`/`clang` (`cmd/purr/toolchain.go:18-37`);
- asks `pkg-config` for `bdw-gc` flags, falling back to `-lgc`
  (`cmd/purr/toolchain.go:39-60`);
- invoke the C compiler with `-std=c11 -Wall -Wextra` and links the result
  (`cmd/purr/main.go:254-274`).

The production `purr` is still the Go implementation:
`cmd/purr/main.go` calls `loader.Load`, `checker.CheckModules`, and
`codegen.Generate`. The Kitten-written driver/fixpoint is not yet the production
pipeline.

### 2.4 What the Go dependency means today

- **End users:** a prebuilt `purr` does not need Go at run time, but building a
  program still needs a C compiler, libgc, and (currently) an installed `purr`
  built somewhere with Go. There is no one-line installer yet.
- **Building the toolchain:** Go is required to build `cmd/purr`; Go plus a C
  compiler is required to build the Kitten-written stage-1 driver. The native
  path still has no compiler backend in Kitten.
- **The test suite:** every differential harness is Go code. It builds Kitten
  stages with the Go C pipeline and runs C compilers under `CC=gcc`/`CC=clang`.
  The tests therefore require Go, a C compiler, and libgc even after the
  C-level fixpoint.

### 2.5 What “Go leaves the pipeline” means

There are three separate questions:

1. **User runtime:** can a user build and run Kitten programs with only `purr`?
2. **Toolchain build:** can `purr` itself be rebuilt without Go?
3. **Verification:** can correctness be established without Go as the oracle?

The three possible Go-exit models:

- **Ship generated C as a bootstrap seed.** Check in the C generated from
  `selfhost/driver.ktn`, let users compile it with a C compiler, then use the
  resulting `purr` to build native code. This removes Go from user builds but
  retains a C compiler for bootstrap and keeps a second language in the trust
  chain.
- **Promote the Kitten compiler to reference status.** The Kitten compiler
  becomes the normative implementation; Go is never invoked. The Go-pipeline
  cross-check disappears. It can be replaced by a frozen golden corpus of
  generated C/native behavior, plus stage-to-stage fixpoint comparisons. That is
  weaker evidence than two independent implementations and must be stated.
- **Freeze Go as a bootstrap-only tool.** Keep the Go implementation in the
  repository for regenerating seeds and investigating discrepancies, but remove
  it from the production `purr` path. This is the most honest transition: the
  reference remains available historically, but the shipped compiler is Kitten.

The recommended end state is the third model followed by the second: first
freeze Go as a bootstrap/reference tool while native parity is established, then
make the Kitten compiler normative once the native fixpoint and golden corpus are
stable. “Go leaves the pipeline” must mean no Go in `purr build`, `purr run`, the
minimal dependency test, or a user's build; it cannot mean the Go source is
deleted from history.

### 2.6 Licensing boundary

The existing split stays unblurred:

- The compiler, native backend, ELF writer, loader, checker, and CLI are
  **GPL-3.0-or-later**.
- `runtime/csrc/` remains **MIT**.
- A new runtime written in Kitten — GC, strings, arrays, maps, printing, file
  and syscall wrappers — is **MIT**, under a runtime directory with its own MIT
  notice, because its code is linked or embedded into every user program. It
  must not be placed under the GPL compiler tree.
- If the compiler embeds the Kitten runtime source to include it in a generated
  program, the MIT notice must travel with that embedded text, just as the
  current `runtime/csrc/` notice travels with copied runtime files.

No new user-program component may fall under the GPL merely because the compiler
that emits it is GPL.

## 3. Language and model gaps

### 3.1 Missing Kitten capabilities

The current language cannot express a runtime or native backend by itself.

| Capability | Current state | Why the native toolchain needs it |
| --- | --- | --- |
| File writing | **Deferred**; SPEC 22.2 has only `read_file`, and 22.9 defers writing | `purr build` must write ELF output; `run` needs temp/output files. |
| Directory operations/metadata | **Deferred** (22.9) | Later compiler/installer discovery; not required for the first single-file/import-explicit build if paths are supplied. |
| Mutable byte buffers | No byte type; `string` is immutable and NUL-terminated; arrays are fixed-size typed arrays | ELF bytes include NUL; code/data buffers, file I/O, GC metadata need mutable bytes. |
| Bitwise operators | **Absent**. SPEC 18.3 lists only arithmetic/comparison/logical operators; grammar 17.2 has no `&`, `\|`, `^`, `~`, `<<`, `>>` | Opcode encoding, tags, hashes, masks, alignment, ELF fields, GC mark bits. |
| Unsigned integer semantics | `int` is signed 64-bit (18.1); no unsigned type | Addresses, sizes, bit patterns, heap metadata. |
| Raw memory / pointers | No pointers; arrays/maps/structs are references but not addressable bytes | Runtime allocation, GC scanning, syscall buffers, ELF data. |
| Syscalls | No API | File, memory, process, exit, output. |
| String/byte conversions | Deferred (20.7): no int↔string, no byte array | Number formatting for `print`, diagnostics, ELF encoding. |
| Process launch | No API | `purr run` must execute the produced program. |
| Recoverable file read | Deferred; design in `proposals/read-file-result.md` | Loader parity and native runtime file handling. |
| Explicit casts / integer widths | Deferred; no casts (18.2) | Raw address/bit manipulation is impossible in safe source. |

The following are **not** language gaps for the first slice: structs, enums,
unions, `match`, arrays, maps, strings, `argv`, `eprint`, and `exit` already
exist and are fixed points. The gap is everything below them.

### 3.2 Semantic model and layout

The checker model carries:

- type kind, name, module, element/key/value for arrays/maps;
- `StructInfo` with fields (`name`, type, position);
- variants with fields and indices;
- expression/type-reference annotations and symbols.

It does **not** carry:

- byte size, alignment, or field offsets;
- union flat-field offsets or storage size;
- string/array/map runtime header layout;
- stack frame layout or register allocation information;
- GC root/type descriptors.

Evidence: `checker/types.go:46-88` and `checker/StructInfo` at `:148-195`;
`selfhost/checker/model.ktn:90-117` and `:65-88`. The C backend avoids needing
this because C computes layout; a native backend must add a layout pass. That
pass is compiler-internal, but its choices affect runtime representation and
must be recorded in SPEC Section 19 (compilation model), not silently invented.

## 4. Target architecture

```text
purr (static ELF, written in Kitten)
  -> read source and imports
  -> lex, parse, check
  -> typed IR + layout
  -> x86-64 instruction selection
  -> direct ELF writer (no assembler, no linker, no libc)
  -> write executable / exec it for run
```

`purr` itself uses the same native runtime that generated programs use. There is
no hosted C runtime in the final state.

## 5. Native backend

### 5.1 Options

| Option | Advantages | Problems |
| --- | --- | --- |
| **Typed IR + small x86-64 selector + direct ELF writer** | No external tools; full control; can share layout/type info with the checker; deterministic. | Most implementation work; must own ELF, calling convention, and machine code correctness. |
| Emit assembly for `as`/`ld` | Much faster to start; assembler/linker handle encoding and ELF. | Not zero-dependency: needs system assembler and linker; debugger-only stepping stone, not the target. |
| QBE / LLVM | Mature optimization and codegen. | External toolchain/library; violates the independence goal; ABI/runtime integration and licensing/trust complexity; not acceptable as the zero-dependency backend. |

**Recommendation: the typed IR + direct ELF path.** Keep an assembly-text
printer as a debugging aid only if useful, never as a required build step.
QBE/LLVM may be used as an optional development oracle but not in the shipped
pipeline.

### 5.2 Typed IR and calling convention

- Lower the checked AST into a typed, three-address IR: integer/float/bool/
  reference values, locals in stack slots, globals/static data, labels, calls,
  branches, loads/stores, allocation, bounds checks, tag switches, and runtime
  calls.
- **First calling convention: a simple internal stack convention.** All
  arguments are placed in caller-allocated stack slots; return values use
  `rax` for integer/reference/bool and `xmm0` for float; void returns nothing.
  The caller cleans up. There is no need to match System V AMD64 initially
  because every callee is compiler-generated and the only external boundary is
  Linux syscalls. Later, a SysV-compatible convention can be introduced for
  interop, but it is not required for zero dependencies.
- **Register allocation: none first.** Every IR value gets a stack slot; use
  scratch registers (`rax`, `rcx`, `rdx`, `r10`, `r11`, `xmm0`-`xmm1`) for
  instruction lowering. Then add **linear scan** over live intervals. Do not
  start with graph coloring. Correctness and deterministic output matter more
  than speed for the first fixpoint.
- Floating-point arithmetic uses SSE2 scalar double instructions; comparisons
  and conversions use the same explicit rules as SPEC.

### 5.3 Layout

Proposed native layout (to be recorded in SPEC Section 19 if approved):

| Value | Size/alignment |
| --- | --- |
| `int` | 8 bytes, align 8 |
| `float` | 8 bytes, align 8 |
| `bool` | 1 byte, align 1 |
| `string`, struct/union/array/map references | 8-byte pointer |
| enum | 8-byte signed tag |
| struct object | fields in declaration order, each aligned to its type; object size rounded to 8 |
| union object | 8-byte tag, then each variant's fields at aligned offsets in one flat struct; size rounded to 8 |
| array object | header: length, element_size, data pointer; elements stored inline in a separate buffer |
| map object | header: length, capacity, value_size, key/hash/value buffer pointers |
| string object | length, data pointer; bytes stored in a separate/adjacent buffer |

All references are pointers. Structs/unions/arrays/maps/strings are GC-managed
objects. Runtime headers must be fixed and documented because both the compiler
and runtime must agree; they are not user-visible.

### 5.4 Control flow and safety checks

- `match`: evaluate the scrutinee once into a temp; for a union, check for null
  and fail deterministically; load the tag; compare cases; bind fields from
  fixed offsets; default fails. This mirrors the C generator’s current shape.
- Array/string/map checks: explicit compares and calls to the failure path;
  negative lengths and allocation overflow fail. No undefined behavior.
- Assignment/compound assignment/indexing reuse the same l-value lowering; the
  target is evaluated once.
- Calls: stack-based frame, deterministic argument order, explicit return slot.

### 5.5 Direct ELF

Emit a static ELF64 `ET_EXEC` with:

- ELF header and one or two `PT_LOAD` program headers;
- `R-X` text segment and `R/W` data/bss segment;
- entry label `_start`, which reads `argc`/`argv` from the initial stack, calls
  the runtime init and the Kitten `main`, then exits via `exit_group`;
- no section headers required for execution, no dynamic section, no
  `PT_INTERP`, no relocations after final layout;
- a two-pass encoder: first assign addresses/label offsets, then patch branch
  and absolute references. If absolute addresses are difficult, use
  PC-relative data references and a data pointer register.

The ELF writer is a Kitten module using the runtime’s file/syscall intrinsics.
`purr build` writes the file; `purr run` writes a temporary file or uses
`memfd_create`/`execveat`, then waits and propagates the status.

## 6. Runtime written in Kitten

### 6.1 Intrinsic boundary

The runtime needs operations that user code must not be able to invoke. The
recommended boundary is a **compiler-only runtime module**:

- a reserved module path/identity (for example `runtime/core`) may use
  intrinsics;
- the checker rejects those intrinsics in every other module with a clear
  diagnostic;
- user source cannot name raw pointers, addresses, or syscalls;
- no user-facing `unsafe` block is introduced.

This preserves “no undefined behavior” for user programs: the unsafe surface
exists only inside the compiler's own runtime, which is implemented and audited
as part of the toolchain. The exact gating mechanism is a SPEC decision.

Proposed internal intrinsics:

```text
raw_alloc(size) -> address/int
raw_load8/16/32/64(address) -> int
raw_store8/16/32/64(address, value)
raw_copy(destination, source, count)
raw_zero(destination, count)
syscall(number, args...) -> int
address_of(local/reference)
stack_bounds()
```

The address representation should be a compiler-internal pointer type, not a
user-visible `int` conversion. If a user-visible `ptr`/`uint` type is preferred,
it needs an explicit SPEC decision; the design recommends keeping it internal.

### 6.2 Runtime components

Implemented in Kitten using the intrinsic layer:

- allocation and GC (Section 7);
- `ktn_string`: length-carrying immutable bytes, concatenation, equality,
  substring, indexing, NUL handling;
- arrays: length, element size, bounds-checked access;
- maps: FNV-1a or another fixed hash, open addressing, inline value copies;
- `print`/`eprint`: integer/string/bool formatting; float formatting must match
  the specified `%g` behavior or be deliberately changed by SPEC;
- `argv` from the initial stack; `exit` via syscall; failure via write(2) and
  `exit_group(1)`;
- `read_file` via syscalls; the postponed `try_read_file`/`ReadFileResult`
  design supplies the recoverable path;
- file creation/write/close for ELF and output;
- process launch for `purr run`;
- no libc/libgc symbols remain.

The runtime's functions become ordinary Kitten functions with a reserved
internal module; generated code calls them through the same calling convention.
The compiler does not need C ABI compatibility in the zero-dependency state.

### 6.3 Float formatting

SPEC 19.4 currently defines `%g` with six significant digits as provisional.
Reimplementing libc’s `%g` exactly is nontrivial (rounding, exponent choice,
`-0`, `inf`/`nan`). The design requires a SPEC decision:

- implement a documented `%g`-compatible formatter and keep current output, or
- change the language’s float formatting to a precisely specified algorithm and
  update the expected outputs deliberately.

It must not silently drift. Integer, bool, and string printing are
straightforward.

## 7. Garbage collector

### 7.1 Options

| Option | Needs from backend | Self-compile timeline | Managed-memory fit |
| --- | --- | --- | --- |
| Bump allocator, process-lifetime arena | `raw_alloc`, no roots | Fastest | Not shipped: no reclamation; violates the principle for long-running programs. |
| Non-moving mark-sweep, conservative stack scan | Allocation headers, stack bounds, safepoints/spills | Soon | Good: reclaims; no pointer updates; simple. False retention is acceptable initially. |
| Precise mark-sweep with stack maps/type descriptors | Type/layout metadata and per-safepoint root maps | Later | Best: precise and fast; more backend work. |

**Recommendation:** implement **non-moving mark-sweep with conservative stack
scanning** as the first shippable GC, after a bump-allocator-only milestone if
that unblocks native self-compilation. The compiler needs roughly 70 MB while
compiling itself; a 256 MB heap with a mark-sweep threshold is a sane initial
target. A bump allocator can be used for one-compilation processes only, behind
an explicit bootstrap flag, and must not be presented as the language’s managed
memory model.

Conservative GC requirements:

- allocation header before every GC object, with size/mark/free-list links;
- a way to map interior pointers to object bases (block table or aligned
  header search);
- scan the active stack (and spilled registers) at safe points;
- allocation safepoints where all live pointers are in stack slots;
- no moving, so internal pointers remain valid;
- write barriers are unnecessary without movement.

The 70 MB compiler working set makes a non-collecting allocator unattractive as
a permanent design. Mark-sweep is the smallest design that honors VISION’s
managed-memory principle.

## 8. Verification ladder

Every rung is a command/byte-level check, not a claim:

1. **ELF writer and syscall runtime.** Compile `fn main() { print("hello"); }`
   to a static ELF with no C compiler, libc, or libgc; run it with an empty
   `PATH` apart from the produced program; `readelf -d` must show no dynamic
   dependencies. Compare stdout/status with the C pipeline.
2. **Expressions, functions, control flow, no heap.** Native-compile a corpus
   of arithmetic/control programs; compare stdout/stderr/status with the C
   pipeline for every fixture and targeted program that does not allocate.
3. **Runtime and GC.** Add strings, arrays, maps, printing, bounds checks, and
   failure paths; compare all nine fixtures and `tests/modules/` against the C
   pipeline under a no-cc environment.
4. **Native compiler stage.** The Kitten compiler (still built by the C pipeline
   as stage 1) uses the native backend to compile `selfhost/driver.ktn` into a
   native stage 2. Stage 2 must reproduce the C pipeline's behavior on the
   fixture/corpus set and must compile itself to native stage 3.
5. **Native fixpoint.** Stage 2 and stage 3 native binaries are compared
   byte-for-byte (if deterministic) and behaviorally against the frozen C
   reference. The compiler source is the same; the claim is native stage-to-stage
   reproducibility, not C-output equality.
6. **Zero-dependency test.** In a minimal root/container:
   - `purr` is the only executable on `PATH`;
   - `go`, `cc`, `gcc`, `clang`, `as`, `ld`, `pkg-config`, and libgc are absent;
   - `purr build` and `purr run` compile and run all fixtures/modules;
   - `readelf -d` shows no `NEEDED` entries and no interpreter;
   - `strace -f -e execve` shows no child tool invocation;
   - `purr` builds itself and the `purr` native stage fixpoint passes.

The kernel is still a dependency; “zero dependencies” means no user-space
toolchain/library beyond `purr` and the Linux syscall ABI.

## 9. Implementation plan

Every step is one commit and requires the SPEC decisions noted. The smallest
first slice is deliberately small: **static ELF + write/exit syscalls + string
literal printing, with no GC**.

0. **DESIGN APPROVAL / SPEC DECISIONS (human).** Decide the items in Section 10
   before code that depends on them.
1. **Native backend skeleton.** Typed IR, layout pass, x86-64 encoder, static
   ELF writer, minimal `_start`/write/exit runtime; compile and run a string-only
   program without cc/libc. Acceptance: `readelf` static, empty-PATH run,
   output/status match the C pipeline.
2. **Scalar functions and control flow.** Calling convention, stack frames,
   integer/float/bool operations, branches, loops, returns; no allocation.
   Acceptance: native/C output parity for non-allocating corpus programs.
3. **GC and basic runtime.** Header layout, mark-sweep, conservative stack scan,
   strings, printing, failure. Acceptance: string/print/fatal tests under a
   no-cc environment.
4. **Arrays, maps, and file I/O.** Complete the runtime and the compiler
   built-ins; acceptance: all fixtures and `tests/modules/` native/C parity.
5. **Native compiler stage.** Native backend compiles `selfhost/driver.ktn`;
   stage 2 compiles itself; acceptance: fixture parity and native stage fixpoint.
6. **Zero-dependency hard cut.** Remove the C runtime/cc path from production
   `purr`; retain Go as a frozen bootstrap/oracle tool. Acceptance: Section 8
   rung 6 passes; C pipeline outputs remain available as golden references.
7. **Goal 3 roadmap only.** Installer, package manager, formatter, LSP, and
   registry are separate, not part of this plan.

Before step 1, land **5.4/5.5** (the `ReadFileResult`/`try_read_file` design and
loader parity). It is small, independent, and supplies the file-error behavior
and test oracle that the native runtime must reproduce. It also prevents the
native runtime from baking in the current fatal-read gap.

## 10. SPEC decisions that gate implementation

These are real semantic/representation choices; they require the human, not a
silent implementation decision:

1. **Bitwise operators and shifts.** Add `&`, `|`, `^`, `~`, `<<`, `>>` with
   defined signed/bit-width semantics, or provide them only as runtime
   intrinsics. The compiler needs them extensively; user-visible syntax is a
   language decision.
2. **Byte/unsigned type or byte arrays.** Add a byte type/`byte[]` for mutable
   file, ELF, and memory buffers, or keep it internal to the runtime. The design
   recommends internal raw memory plus a user-facing byte type only if the
   standard library needs it.
3. **Intrinsic/unsafe boundary.** Exactly which compiler intrinsics exist, which
   module may use them, and the diagnostic for user misuse.
4. **File writing and directory/process APIs.** Whether these become language
   built-ins or only internal runtime intrinsics; error semantics; reopen
   SPEC 22.2/22.9.
5. **Native layout/ABI.** The table in Section 5.3; bool size/alignment, object
   headers, field offsets, array/map/string headers. This belongs in SPEC
   Section 19 because it is the compilation model.
6. **GC semantics.** Conservative vs precise roots, interior-pointer handling,
   stack scanning, allocation failure, and whether user code can observe
   collection. Managed memory is a VISION principle; the implementation choice
   must be explicit.
7. **Float formatting.** Preserve `%g` exactly or define a new deterministic
   format (Section 6.3).
8. **Integer overflow.** SPEC defers it; native arithmetic must choose wrapping,
   trapping, or checked behavior.
9. **Recoverable file read.** Approve/land the existing `ReadFileResult` design
   before or alongside native file I/O.
10. **Process launch for `purr run`.** Whether it is internal only or a user
    API; Linux x86-64 syscall semantics.

## 11. Where `purr` ends up

`purr` is a Kitten program with:

- the existing front end (lexer, parser, checker, loader);
- a native layout/IR/backend module;
- the Kitten runtime/GC;
- the ELF writer and CLI (`build`/`run`);
- later, the installer/package/LSP/format components as separate Goal 3 work.

The production CLI no longer invokes Go, C, libgc, an assembler, or a linker.
The Go implementation remains a frozen bootstrap/reference tool until native
parity is established, then becomes historical. The C backend may remain as an
optional oracle/debug backend but is not part of the shipped pipeline.

## 12. Platform story

**Linux x86-64 is the only planned platform.** The design relies on the Linux
syscall ABI, ELF64, and x86-64 encoding. No other platform is claimed.

- **arm64 Linux:** different syscall numbers and registers, ELF machine type,
  and instruction selection; a separate backend/runtime port, not a small flag.
- **macOS:** Mach-O, not ELF; process/file APIs go through libSystem, and
  binaries need code signing. “Zero dependencies” there may mean system
  libraries, not raw syscalls; that is a distinct design and is not justified
  now.
- **Windows:** PE/COFF, WinAPI/kernel32 rather than a stable raw syscall
  interface, different exception/unwind and calling conventions. “Zero
  dependencies” would mean system DLLs and a PE loader; not ELF/syscall design.
- **x86-64 only initially:** the IR and layout can be shared, but instruction
  selection and object format are platform-specific.

## 13. Risks and recommendation

- **Largest effort in the project.** Native codegen, ELF, runtime, GC, and
  self-hosted compiler together are much larger than the C fixpoint.
- **Loss of the independent Go oracle.** Keep the frozen Go/C outputs as golden
  references; do not pretend stage-to-stage agreement is independent
  verification.
- **GC correctness and interior pointers.** Conservative scanning is forgiving
  but must map pointers to bases and scan stacks safely.
- **Float formatting** is a real semantic cliff; decide it before native output
  is claimed equivalent.
- **Runtime self-hosting circularity.** A Kitten runtime needs allocation and
  raw memory before it can implement arrays/maps; the intrinsic layer and a
  minimal bootstrap runtime must be designed first.
- **Installer/ecosystem mismatch.** Shipping a one-line installer before
  zero-dependency builds would still require users to install C/Go/libgc.
  Do not promise that.

**Recommendation on order:** finish **5.4/5.5** first, then implement the native
backend in the staged order above. Do not make `purr` the production native
compiler until the zero-dependency test and native fixpoint pass. Keep Goal 3
(installer/ecosystem) on the roadmap but do not bundle it; the installer should
be designed after the zero-dependency path so it does not advertise a toolchain
that still needs a C compiler. If the human prefers faster user-facing progress,
Goal 3 can proceed independently, but it does not shorten Goal 2 and must not
claim zero-dependency installation.

## 14. Human-only questions

1. Approve the recommended backend: typed IR + stack-first calling convention +
   linear scan later + direct ELF writer, with assembly/QBE/LLVM rejected for
   the shipped path?
2. Approve the intrinsic boundary: unsafe operations only in a compiler-reserved
   runtime module, with no user-facing unsafe block?
3. Which SPEC choices from Section 10 should be decided first: bitwise/shift
   operators, a byte type, file/process APIs, and native layout?
4. Approve non-moving conservative mark-sweep as the first shippable GC, with a
   bump allocator only as a temporary bootstrap milestone?
5. Approve the float-format decision: implement `%g` exactly, or deliberately
   define a new deterministic format and update expected outputs?
6. Should 5.4/5.5 land before native runtime work, as recommended, or should
   native codegen proceed in parallel?
7. Is the frozen-Go bootstrap/reference model acceptable after native parity, or
   must the Go source be removed entirely (which would lose the independent
   cross-check)?
8. Should the native backend remain available alongside the C backend as an
   oracle, or should C be removed from the shipped toolchain immediately after
   zero-dependency parity?
