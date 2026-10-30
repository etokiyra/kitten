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
jump, branch, and label. The instruction union is now fully encoded; every
scene the checker can form (for the scalar, allocation-free subset) lowers into
a runnable static executable. No part of the production sidecar depends on this
module yet.

## Files

- `selfhost/native/ir.ktn` — `NativeScene`, its builders, and `check_scene`,
  the whole-scene well-formedness pass; `push_instruction` snapshots call
  argument arrays so the scene stays an immutable snapshot.
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
  scenes, and `elf`, `elf-arith`, and `elf-divzero`, which write runnable
  executables.
- `cmd/purr/native_test.go` — the pinned tests, the executions and their
  C-pipeline parity, staticness checks, and both-pipeline ELF byte identity.

## The scene contract

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
  `= less_than`, `= call @<index>`, `call`, `jump`, `branch`, `label`,
  `return`).

The probe's render ends with the extra newline `print` adds; the pinned text
expects it.

## What is not here

- A lowering from the checked program: `NativeScene` is an input the lowering
  will produce, not an output it yet has. The probe builds the scene by hand.
- Float arithmetic and conversions: no float forms exist in the instruction
  union yet; they land with the selector's SSE2 work.
- Field access, allocation, bounds checks, and tag switches.
- Stack-alignment refinement, a System V interop convention, register
  allocation, and the runtime's GC. The proposal's order stages all of it
  into later slices.

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
5. Lower `whole_divide` and `whole_remainder` on SPEC's defined rules: a zero
   divisor jumps to the shared failure paragraph — write(2) the C runtime's
   exact `purr: integer division by zero` text to standard error, then
   `exit_group(1)` — and a minus-one divisor takes the wrap path (`neg rax`
   for the quotient, `xor edx,edx` for the remainder) so `idiv` can never
   trap. The paragraph and its private message blob are emitted only when
   some guard exists.
6. Lay out one string object per data blob (length, payload pointer, payload,
   padding to eight); the object's pointer field is one recorded fixup.
7. Patch all recorded fixups in place: call displacements, RIP-relative data
   references, absolute payload pointers, forward branch and jump
   displacements, the failure paragraph's own reference and guard jumps.
   Every earlier position survives patching inside mutable byte buffers.

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
- Both pipelines produce byte-identical ELFs for all three scene kinds: the
  Kitten-written `purr-kt` sidecar builds the probe and runs the whole
  suite, and its ELFs equal the Go pipeline's.
