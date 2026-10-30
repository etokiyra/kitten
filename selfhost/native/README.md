# The native backend's typed IR, written in Kitten

`selfhost/native` is the zero-dependency backend: a typed, three-address scene
that the lowering fills and that later native stages — the x86-64 selector, the
ELF writer — read. Following `proposals/zero-dependency-toolchain.md` Section 5,
the well-formedness gate (`check_scene`) is the backend's first verification
point, the selector is the x86-64 encoding layer, and the ELF writer is the file
layer. Phase 10.0 approved the design; Phase 10.1 landed the scene; Phase 10.2
landed the skeleton slice — constants, calls, the program's opening paragraph,
write and exit syscalls, and string-literal printing, no GC and no C compiler
anywhere. Phase 10.3 added step 2's scalar arithmetic and control flow:
integer add through guard-led divide and remainder, negate, equal, less_than,
jump, branch, and label. The instruction union is now fully encoded. Phase
10.4 lowered the checked program: `native/lower.ktn` reduces a checked
single-module program's semantic model into a scene for the scalar,
allocation-free subset, with one deterministic message per unsupported
construct, and the probe's `lower` mode runs real `.ktn` sources from disk
through loader, checker, lowering, selector, and ELF writer. Every corpus
program's executable matches the C pipeline's behavior; no part of the
production sidecar depends on this module yet. Phase 10.5 then added the
float slice: `native/floats.ktn` encodes decimal literal text into exact
IEEE 754 binary64 bit patterns in pure integer Kitten (one 53-bit rounding,
deterministic failure past seventeen significant digits, saturation and
underflow as the C toolchain does), the scene grew its SSE2 forms
(`constf`, four arithmetic forms, three comparison forms, `negatef`) with
the exact NaN-handling sequences the C generators emit, float results
return in `xmm0`, and the corpus exercises the edges — ties, NaN, infinity,
the subnormal rounding-to-zero edge, both increment positions — against
the C pipeline byte for byte. Printing floats and value conversions stay
with the runtime slice.

## Files

- `selfhost/native/ir.ktn` — `NativeScene`, its builders, and `check_scene`,
  the whole-scene well-formedness pass; `push_instruction` snapshots call
  argument arrays so the scene stays an immutable snapshot.
- `selfhost/native/lower.ktn` — the lowering from a checked program's
  semantic model to one scene, covering the scalar subset, with one
  deterministic message for every unsupported construct (top of the
  file lists the subset).
- `selfhost/native/floats.ktn` — the float literal encoder: decimal text to
  an exact IEEE 754 binary64 bit pattern in pure integer Kitten, with one
  deterministic failure for anything past the seventeen-digit precision
  bound.
- `selfhost/native/text.ktn` — the deterministic text renderer whose pinned
  output protects the scene's shape as the backend grows around it.
- `selfhost/native/selector.ktn` — the raw byte buffer, the frame layout, the
  x86-64 encodings, the fixup recorder, and `select_scene`, the selection pass
  that lays out code, data, and patches.
- `selfhost/native/elf.ktn` — the static ELF64 writer: the 64-byte header,
  the two program headers, header-and-code text segment, page-aligned data
  segment, and the entry point.
- `selfhost/native.ktn` — the probe: the skeleton target's scene, the
  arithmetic scene, the divide-failure scene, the growth scene, the failure
  scenes, the `lower` driver over real sources — `load_program` →
  `check_modules` → `lower_program` → `check_scene` → `select_scene` and the
  writer — and `elf`, `elf-arith`, `elf-divzero`, and `lower <source>
  [<output>]`, which write runnable executables.
- `cmd/purr/native_test.go` — the pinned tests, the executions and their
  C-pipeline parity, staticness checks, and both-pipeline ELF byte identity.
- `tests/native/*.ktn` — the scalar corpus: every program lowers, and its
  native executable's output, standard error, and status match the C
  pipeline's `purr run` of the same source. `tests/native/bad/*.ktn` — one
  deterministic failure message per unsupported construct and per load, lex,
  parse, and check failure.

## The lowering

`lower_program(model)` walks one checked single-module program and fills the
scene directly from the model the Kitten-written checker produced; it never
resolves a name, infers a type, or re-reads source text. Function shells
register in declaration order, the print helper — when resolution needs it —
lands after them, and bodies lower in declaration order, so binding order is
the walk order: parameters first, then each local and temporary as the
lowering creates it. String literals pool by decoded bytes exactly as the C
generator pools its literals. The lowering emits the boolean operators and
the derived comparisons through the branch forms the scene already carries —
`!`, `!=`, `<=`, and `>=` materialize a bool through a branch on their
flipped condition — and lowers `&&` and `||` short-circuit, so the right
operand evaluates only when the first did not decide, matching the C
pipeline's generated `&&`/`||`. The `print` built-in lowers with exactly one
string argument to a scene-internal helper named `"$print"`, which no user
name can collide with, and `exit` lowers to the backend's own raw
`exit_group` syscall. Since Phase 10.5, float values lower through the
scene's SSE2 forms — literals become `constf` constants with the encoder's
bit patterns, arithmetic and comparisons take their derived forms with
IEEE semantics, and `exit` stays the raw syscall. The slice's frame is still
the selector's: at most fourteen locals per function, so corpus functions
stay small; the C generator's known self-shadowing-initializer quirk is why
the corpus avoids a `local` whose initializer names a same-scoped outer
local.

## The float encoder

`selfhost/native/floats.ktn` is the piece the C pipeline delegated to its C
compiler: a float literal's decimal text turns into the exact IEEE 754
binary64 bit pattern through pure integer Kitten — 16-bit limbs, exact x10
multiplications and bit shifts, one 53-bit rounding against guard and
sticky, subnormal and saturating assembly. The precision bound: literals
longer than seventeen significant digits fail deterministically —
seventeen covers every double's round-trip precision, and longer runs
upgrade with the runtime. Saturation to infinity and underflow to zero
match the C toolchain's text conversion. The probe's `float <text>` mode
prints the pattern, and the Go test drives every corpus case against Go's
`strconv` oracle plus a seeded random corpus.

## Scene contract

A `NativeScene` is ordered functions, ordered static blobs, and an entry index.
Each `NativeFunction` has a result shape, a binding table, and an ordered
instruction list.

Shapes (`NativeType`) and their sizes and alignments come from the proposal's
layout table, and `type_size`/`type_alignment` enforce the scalar rows:

| Shape | Size | Alignment |
| --- | --- | --- |
| `int` | 8 | 8 |
| `float64` | 8 | 8 |
| `bool` | 1 | 1 |
| `ref` (strings, structs, unions, arrays, maps) | 8 | 8 |
| `void` | — | — |

Bindings are stack slots by default, numbered per function with parameters
first and temporaries in creation order. Labels are per-function indices and
`push_instruction` keeps the label count exact as `Label` instructions are
appended, so branch and jump validation is index-exact. Calls name their callee
by scene function index; forward and self calls are legal because
`check_scene` runs once, after every function exists. `ConstRefData` binds a
reference to a data blob's address, which is how a lowered string literal will
reach the data section.

`push_instruction` snapshots call instructions: `Call`, `CallVoid`, and
`RawSyscall` carry their argument arrays by reference, so the stored
instruction receives its own copy. A builder that reuses one argument array
across several calls then keeps every instruction's own arguments.

`check_scene` validates, per function: no shapeless parameter or binding, every
label referenced has a definition, and every instruction's index ranges, operand
shapes, and call agreement are checked with one deterministic message naming
function, instruction position, and role. The failure texts are pinned in
`cmd/purr/native_test.go` in both this corpus's failure modes: `no-label`,
`bad-binding`, `bad-call-count`, `bad-call-shape`, `bad-entry`, and
`undefined-label`, plus the fall-through of an unknown mode to the empty
scene's entry failure.

## Renderer spellings

The renderer output is deterministic and pinned:

- `scene`, then one `data $blob<N>: <length> bytes <octets>` per blob;
- `entry <index> @<name>`;
- `fn @<name>(p0: ref, ...) -> <shape>` signatures;
- `  b<N>: <shape> binding` per binding;
- one instruction line per instruction, with its fixed spelling
  (`= const`, `= data`, `= copy`, `= add`, `= subtract`, `= multiply`,
  `= whole_divide`, `= whole_remainder`, `= negate`, `= equal`,
  `= less_than`, `= constf`, `= addf`, `= subtractf`, `= multiplyf`,
  `= dividef`, `= negatef`, `= equalf`, `= greaterf`, `= less_or_equalf`,
  `= call @<index>`, `call`, `jump`, `branch`, `label`, `return`; the float
  bit patterns render as their signed decimal reading).

The probe's render ends with the extra newline `print` adds; the pinned text
expects it.

## What is not here

- Field access, allocation, bounds checks, and tag switches: strings lower
  as static literal blobs alone; concatenation, string equality, and every
  composite type fail with their message.
- Printing floats and any int-to-float or float-to-int conversion: the
  rendering work belongs to the runtime slice (`%g` exactly), and the
  conversions have no lowering yet.
- Bitwise and shift operators, byte values, imports, and `match`: the
  scalar subset is single-module, and each fails with its message.
- Stack-alignment refinement, a System V interop convention, register
  allocation, and the runtime's GC. The frame still carries at most fourteen
  parameters and fourteen locals, which shapes the corpus; the proposal's
  order stages the rest into later slices.
- The print helper is the only runtime-shaped routine the lowering emits;
  the runtime written in Kitten arrives with the runtime slice.

## What the selector lowers

1. Check the frame: parameters and locals must fit the slice's one-byte slot
   displacements (fourteen each); anything more fails with its message.
2. Emit the program's opening paragraph: `call` the entry function, then exit
   with status 0 through the `exit_group` syscall. argv is not read yet.
3. Lower every function in order with the entry paragraph's frame: a caller's
   frame ends up `push rbp; mov rbp, rsp; sub rsp, 8·locals`; parameter k
   lives in the caller's argument area at rbp+16+8k; locals live at rbp-8
   downward; results return in rax. Calls push their argument slots in
   reverse declaration order, then call, then pop; the whole convention is
   the proposal Section 5.2's internal stack convention, before alignment
   refinement and register allocation.
4. Lower the scalar instructions: `add`, `subtract`, `multiply` fold two
   slots through rax; `negate` negates through ax; `equal` and `less_than`
   compare through rax and materialize a normalized 0/1 through a setCC and
   a zero-extension, so every bool slot holds all eight bytes of its bit;
   `jump`, `branch`, and `label` record forward-jump fixups that patch after
   the function's labels are all defined — label positions are absolute
   code-buffer indices. `branch` reads its condition as one byte, so a
   `ConstBool` slot's unused upper bytes can never steer it.
5. Lower the float instructions through xmm0: `constf` moves the bit
   pattern through rax (`movq xmm0, rax`); `addf` through `dividef` are
   `movsd` / `addsd` / `subsd` / `mulsd` / `divsd` with the same
   load-fold-store shape as the integer forms — division kept unguarded,
   so a zero divisor yields infinity or NaN exactly as the C pipeline's
   generated division of doubles does; `equalf` pairs `sete` with `setnp`
   (an unordered compare also sets ZF, so the parity flag carries the
   ordered test) and ANDs the two flags; `greaterf` and
   `less_or_equalf` are `seta` and `setae`, whose carry-flag reading makes
   unordered values false on their own; `negatef` flips the double's sign
   bit through a `movsd`-loaded mask, which avoids every SSE2 alignment
   requirement. Float results return in xmm0 and float call targets read
   it back, per the proposal's convention.
6. Lower `whole_divide` and `whole_remainder` on SPEC's defined rules: a zero
   divisor jumps to the shared failure paragraph — write(2) the C runtime's
   exact `purr: integer division by zero` text to standard error, then
   `exit_group(1)` — and a minus-one divisor takes the wrap path (`neg rax`
   for the quotient, `xor edx,edx` for the remainder) so `idiv` can never
   trap. The paragraph and its private message blob are emitted only when
   some guard exists.
7. Lay out one string object per data blob (length, payload pointer, payload,
   padding to eight); the object's pointer field is one recorded fixup. The
   float sign mask, when a `negatef` exists, appends to the selector's
   private data with one RIP-relative fixup of its own.
8. Patch all recorded fixups in place: call displacements, RIP-relative data
   references, absolute payload pointers, forward branch and jump
   displacements, the failure paragraph's own reference and guard jumps,
   and the sign-mask references. Every earlier position survives patching
   inside mutable byte buffers.

## The ELF layout

The text segment covers the file's first bytes (header, both program headers,
and the code) at `0x400000`, read-and-executable; the data segment covers a
page-aligned data section at `0xf00000`, read-and-writable; the entry point is
the program's opening paragraph. There are no section headers, no dynamic
section, no interpreter, and no relocations after layout.

## Running the probe

```sh
purr build selfhost/native.ktn -o /tmp/native-probe
/tmp/native-probe                            # print hello scene
/tmp/native-probe big                        # growth corpus: 12 functions, 9 blobs, 17 bindings each
/tmp/native-probe arith                      # arithmetic-and-control-flow scene
/tmp/native-probe simple                     # argument-snapshot scene: two calls, one reused array
/tmp/native-probe no-label                   # one validation message per pinned mode
/tmp/native-probe elf /tmp/hello && /tmp/hello               # write and run the skeleton executable
/tmp/native-probe elf-arith /tmp/arith && /tmp/arith         # write and run the arithmetic executable
/tmp/native-probe elf-divzero /tmp/dv && /tmp/dv             # write and run the divide-failure executable
/tmp/native-probe lower tests/native/pool.ktn                # render a real source's lowered scene
/tmp/native-probe lower tests/native/arith.ktn /tmp/a        # write and run a real source's executable
/tmp/native-probe lower tests/native/floats.ktn /tmp/f && /tmp/f   # float corpus, no C anywhere
/tmp/native-probe float 0.300000000000000044                 # the precision bound's message
```

## Verification

- The hello scene's rendering is pinned byte for byte; the arithmetic scene's
  rendering is pinned byte for byte too.
- The growth scene is pinned structurally (line counts and spot lines), so
  every table-doubling path is exercised deterministically; the
  argument-snapshot scene's calls are pinned literally in the both-pipeline
  comparison.
- The six validation messages plus the unknown-mode fall-through are pinned.
- The skeleton writes a static ELF (no interpreter, no dynamic section, two
  program headers) whose run output and status match the C pipeline's
  program for the same source; repeated productions are byte-identical.
- The arithmetic executable prints `small` then `negged` with status 0 and
  the divide-failure executable prints `purr: integer division by zero` to
  standard error with status 1; each matches the C pipeline's build of the
  mirrored source byte for byte.
- The Phase 10.4 lowering corpus (`tests/native/*.ktn`) lowers and runs with
  output, standard error, and status matching the C pipeline's `purr run` of
  the same source, deterministically per source; the pooled-literal scene's
  rendering is pinned byte for byte; and one message per unsupported
  construct and per load/lex/parse/check failure is pinned.
- The Phase 10.5 float slice: the encoder's corpus of fixed edges plus a
  seeded random corpus matches the `strconv` oracle's bit patterns with no
  disagreement; the float corpus programs (arithmetic through recursion,
  NaN and infinity behavior, the subnormal edge, both increment positions,
  tie rounding) match the C pipeline byte for byte with an empty PATH; and
  the float scene's rendering is pinned.
- Both pipelines produce byte-identical ELFs for all three scene kinds and
  for every corpus program, and their `lower`-mode renderings and failure
  messages agree byte for byte: the Kitten-written `purr-kt` sidecar builds
  the probe and runs the whole suite, and its outputs equal the Go
  pipeline's.
