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
the C pipeline byte for byte. Phase 10.7 then landed the runtime
foundations and the collector (`native/runtime.ktn`): every blob is one
object on the proposal Section 5.3 header ABI (SPEC Section 19.10) — an
8-byte header word carrying the whole size, an 8-byte length, an 8-byte
payload pointer, and the payload — Phase 10.7 built the mark-sweep
collector: every heap block is one object behind a size-only header word,
mark state lives in a one-byte-per-word ledger beside the heap, and a
conservative stack scan marks live objects only where the ledger
certifies a header, so a stale stack value or an interior pointer can
neither corrupt nor miss memory. The sweep builds an address-ordered free
list whose adjacent holes coalesce, allocation goes first-fit through the
list (splitting tails) before trying the untouched cursor, and after one
full collection the exact `purr: out of memory` halt at status 1 is
still the end. print(int)/print(bool) lower into decimal-digit and
true/false helpers that byte-match the C pipeline, and string concat
(`+`, `+=`), content equality (`==`, `!=`), and the `length` built-in are
the first user-reachable `$alloc` clients. The mark-sweep GC — string
length/equality, `%g` float printing, arrays/maps/structs/unions, field
access, bounds checks, tag switches, stack-alignment refinement, and
register allocation — stays with the slices that follow.

## Files

- `selfhost/native/ir.ktn` — `NativeScene`, its builders, and `check_scene`,
  the whole-scene well-formedness pass; `push_instruction` snapshots call
  argument arrays so the scene stays an immutable snapshot; the scene's
  backend-internal forms include the 10.7 collector's `RawStore64`,
  `RawLoad8`, `RawStore8`, and `StackPointer`.
- `selfhost/native/runtime.ktn` — the runtime shells: the collector (mapping,
  the mark ledger, `$search`/`$bump` allocation, conservative scan, sweep
  with free-list coalescing), the string runtime, and the print helpers,
  registered through the same builders the lowering and the probe use;
  every shell stays below the slice's fourteen-local frame layout.
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
  arithmetic scene, the divide-failure scene, the allocation-failure scene,
  the growth scene, the failure scenes, the `lower` driver over real
  sources — `load_program` → `check_modules` → `lower_program` →
  `check_scene` → `select_scene` and the writer — and `elf`, `elf-arith`,
  `elf-divzero`, `elf-oom`, and `lower <source> [<output>]`, which write
  runnable executables.
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
register in declaration order, then the allocator's shells — `$heap`, `$map`,
`$alloc` — register into every program, then one print helper per argument
kind the print-kind prescan found, and bodies lower in declaration order, so
binding order is the walk order: parameters first, then each local and
temporary as the lowering creates it. String literals pool by decoded bytes
exactly as the C generator pools its literals. The lowering emits the boolean
operators and the derived comparisons through the branch forms the scene
already carries — `!`, `!=`, `<=`, and `>=` materialize a bool through a
branch on their flipped condition — and lowers `&&` and `||` short-circuit,
so the right operand evaluates only when the first did not decide, matching
the C pipeline's generated `&&`/`||`. The `print` built-in lowers with
exactly one argument to its kind's shell: `"$print"` for a string,
`"$print_int"` for an int (digits render one byte at a time through the
recursive `"$digits"`, sharing the `"$print_newline"` tail), and
`"$print_bool"` for a bool. `exit` lowers to the backend's own raw
`exit_group` syscall. Since Phase 10.5, float values lower through the
scene's SSE2 forms — literals become `constf` constants with the encoder's
bit patterns, arithmetic and comparisons take their derived forms with IEEE
semantics — and `exit` stays the raw syscall. The slice's frame is still the
selector's: at most fourteen locals per function, so corpus functions and
runtime shells stay small; the C generator's known
self-shadowing-initializer quirk is why the corpus avoids a `local` whose
initializer names a same-scoped outer local.

## The runtime shells

`selfhost/native/runtime.ktn` is the first part of proposal Section 6.2's
runtime written in Kitten. The collector's `$heap` blob is mutable scene
data: GetStatic/SetStatic address its six words (base, cursor, limit, the
free-list head, and the sweep's last-block/last-end words) at raw offsets
24..64, whose starting bytes are the all-ones pattern, the negative value
-1, so "absent" is one test away for every table. `$map` makes the raw
`mmap`s (PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS) for the heap —
256 MiB, the proposal Section 7.1's first target — and a one-byte-per-word
mark ledger beside it, and captures the stack pointer for scanning.
`$alloc` walks first-fit through the sweep-built free list (splitting a
tail when the remainder fits a node), then bump-allocates against the
cursor, and after one full collection — conservative stack scan through
the ledger, then the size-led sweep whose address-ordered disposal
coalesces — the exact `purr: out of memory` at status 1 ends the program.
The string shells mirror the C runtime byte for byte: `$string_concat`
allocates through `$alloc` and copies both payloads, `$string_equal`
compares byte for byte, and `$string_length` reads the length word, so
`+`, `+=`, `==`, `!=` between strings and `length()` land in the language
exactly as the C pipeline words them. Every shell is under the frame cap.

## The header ABI

Every data blob is one runtime object on the layout the proposal Section 5.3
fixes and SPEC Section 19.10 records: an 8-byte header carrying the whole
object size (8-aligned; its three low bits are reserved for the collector),
an 8-byte length, an 8-byte payload pointer, and the payload, everything
8-aligned. `ConstRefData` binds the object's start; the print helpers load
length at raw offset 8 and the payload pointer at 16; GetStatic and
SetStatic address a raw offset inside the blob's whole mapped size, which
`blob_total` in `ir.ktn` computes and `check_scene` bounds.

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
function, instruction position, and role. GetStatic and SetStatic additionally
bound their blob and its whole mapped size through `blob_total`. The failure
texts are pinned in `cmd/purr/native_test.go` in both this corpus's failure
modes: `no-label`, `bad-binding`, `bad-call-count`, `bad-call-shape`,
`bad-entry`, `undefined-label`, `bad-static-blob`, `bad-static-offset`, and
`bad-static-value`, plus the fall-through of an unknown mode to the empty
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
  `= call @<index>`, `call`, `jump`, `branch`, `label`, `return`,
  `= raw_load64`, `= raw_syscall syscall`, `= get_static`, `set_static`,
  `fail_oom`; the float bit patterns render as their signed decimal
  reading).

The probe's render ends with the extra newline `print` adds; the pinned text
expects it.

## What is not here

- Field access, allocation of non-string types, bounds checks, and tag
  switches: strings allocate and concatenate; every composite type user-side
  fails with its message. The collector runs on every program that allocates
  and is exercised end-to-end through `elf-gc`, but the mark ledger's
  growth beyond one flat region and precise (collector-visible) safepoints
  are later slices.
- Printing floats and any int-to-float or float-to-int conversion: the
  rendering work belongs to the runtime slices (`%g` exactly), and the
  conversions have no lowering yet.
- Bitwise and shift operators, byte values, imports, and `match`: the
  scalar subset is single-module, and each fails with its message.
- Stack-alignment refinement, a System V interop convention, and register
  allocation. The frame still carries at most fourteen parameters and
  fourteen locals, which shapes the corpus and the shells; the proposal's
  order stages the rest into later slices.

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
7. Lower the runtime forms: `get_static` reads eight bytes straight out of a
   blob's raw offset (`mov rax, [rip+disp]`) and `set_static` stores them
   (`mov [rip+disp], rax`); `fail_oom` is one unconditional jump to the
   shared out-of-memory paragraph, which writes the C runtime's exact
   `purr: out of memory` text to standard error and exits with status 1.
   That paragraph and its private message blob exist only when some
   `fail_oom` was emitted.
8. Lay out one object per data blob, eight-aligned, on the object-header
   ABI: the header word (the whole size), the payload length, the payload
   pointer (patched in place the moment the blob starts), the payload, and
   the padding. The failure messages are private objects of the same shape;
   the float sign mask, when a `negatef` exists, stays raw with one
   RIP-relative fixup of its own.
9. Patch all recorded fixups in place: call displacements, RIP-relative data
   references, forward branch and jump displacements, the failure
   paragraphs' own references and guard jumps, and the sign-mask
   references. Every earlier position survives patching inside mutable
   byte buffers.

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
/tmp/native-probe elf-oom /tmp/oom && /tmp/oom               # write and run the allocation-failure executable
/tmp/native-probe elf-gc /tmp/gc && /tmp/gc                  # drive 600 MiB through the collector
/tmp/native-probe lower tests/native/printint.ktn            # render a real source's lowered scene
/tmp/native-probe lower tests/native/printint.ktn /tmp/pi     # write and run a real source's executable
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
- The nine validation messages plus the unknown-mode fall-through are pinned:
  the six original modes, the three Phase 10.6 static-data modes
  (`bad-static-blob`, `bad-static-offset`, `bad-static-value`), and the
  fall-through.
- The skeleton writes a static ELF (no interpreter, no dynamic section, two
  program headers) whose run output and status match the C pipeline's
  program for the same source; repeated productions are byte-identical.
- The arithmetic executable prints `small` then `negged` with status 0 and
  the divide-failure executable prints `purr: integer division by zero` to
  standard error with status 1; each matches the C pipeline's build of the
  mirrored source byte for byte.
- The allocation-failure executable prints the C runtime's exact
  `purr: out of memory` to standard error with status 1, byte for byte and
  deterministically.
- The Phase 10.7 collector: `elf-gc` drives six hundred one-megabyte
  allocations through the collector, printing `done` with status 0, byte
  for byte and deterministically; the string corpus (`concat.ktn`,
  `equal.ktn`, `lengthgc.ktn`, `gcstress.ktn`) covers concatenation,
  content equality, and `length` against the C pipeline byte for byte,
  inside the general corpus run; the string scenes' renderings are pinned
  alongside the print scenes'; and the three validation messages stay
  pinned.
- The Phase 10.4 lowering corpus (`tests/native/*.ktn`) lowers and runs with
  output, standard error, and status matching the C pipeline's `purr run` of
  the same source, deterministically per source; the pooled-literal scene's
  rendering is pinned byte for byte; and one message per unsupported
  construct and per load/lex/parse/check failure is pinned.
- The Phase 10.6 print corpus (`printint.ktn`, `printbool.ktn`,
  `negatives.ktn`, `mixed.ktn`) covers zero, negative, wrap-to-INT64_MIN,
  and bool printings against the C pipeline byte for byte, inside the
  general corpus run; the integer and bool print scenes' renderings are
  pinned.
- The Phase 10.5 float slice: the encoder's corpus of fixed edges plus a
  seeded random corpus matches the `strconv` oracle's bit patterns with no
  disagreement; the float corpus programs (arithmetic through recursion,
  NaN and infinity behavior, the subnormal edge, both increment positions,
  tie rounding) match the C pipeline byte for byte with an empty PATH; and
  the float scene's rendering is pinned.
- Both pipelines produce byte-identical ELFs for all five scene kinds and
  for every corpus program, and their `lower`-mode renderings and failure
  messages agree byte for byte: the Kitten-written `purr-kt` sidecar builds
  the probe and runs the whole suite, and its outputs equal the Go
  pipeline's.
