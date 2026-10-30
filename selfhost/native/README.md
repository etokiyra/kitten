# The native backend's typed IR, written in Kitten

`selfhost/native` is the zero-dependency backend: a typed, three-address scene
that the lowering fills and that later native stages — the layout pass, the
x86-64 selector, the ELF writer — read. Following
`proposals/zero-dependency-toolchain.md` Section 5, the well-formedness gate
(`check_scene`) is the backend's first verification point, the selector is the
x86-64 encoding layer, and the ELF writer is the file layer. Phase 10.0
approved the design; Phase 10.1 landed the scene; Phase 10.2 landed the
skeleton slice — constants, calls, the program's opening paragraph, write and
exit syscalls, and string-literal printing, no GC and no C compiler anywhere.
Not every scene the checker could form lowers yet: unsupported instruction
forms fail the selection with a deterministic message naming function and
instruction. No part of the production sidecar depends on this module yet.

## Files

- `selfhost/native/ir.ktn` — `NativeScene`, its builders, and `check_scene`,
  the whole-scene well-formedness pass.
- `selfhost/native/text.ktn` — the deterministic text renderer whose pinned
  output protects the scene's shape as the backend grows around it.
- `selfhost/native/selector.ktn` — the raw byte buffer, the frame layout, the
  x86-64 encodings the slice supports, the fixup recorder, and `select_scene`,
  the selection pass that lays out code, data, and patches.
- `selfhost/native/elf.ktn` — the static ELF64 writer: the 64-byte header,
  the two program headers, header-and-code text segment, page-aligned data
  segment, and the entry point.
- `selfhost/native.ktn` — the probe: the skeleton target's scene, the growth
  scene, the failure scenes, and `elf <path>`, which writes a runnable
  executable.
- `cmd/purr/native_test.go` — the pinned tests, the skeleton's execution and
  its staticness checks, C-pipeline program parity, and both-pipeline ELF
  byte identity.

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
- The rest of the instruction set (arithmetic, comparisons, branches, labels,
  floats, field access, allocation, bounds checks, tag switches) beyond what
  the selector's `supported` gate names: unsupported forms fail with a
  deterministic message.
- Stack-alignment refinement, a System V interop convention, register
  allocation, and the runtime's GC. The proposal's order stages all of it
  into later slices, and the skeleton's acceptance rung passed with the
  smallest set.

## What the 10.2 selector does

1. Check the frame: parameters and locals must fit the slice's one-byte slot
   displacements (fourteen each); anything more fails with its message.
2. Emit the program's opening paragraph: `call` the entry function, then exit
   with status 0 through the `exit_group` syscall. argv is not read in this
   slice.
3. Lower every function in order with the entry paragraph's frame: a caller's
   frame ends up `push rbp; mov rbp, rsp; sub rsp, 8·locals`; parameter k
   lives in the caller's argument area at rbp+16+8k; locals live at rbp-8
   downward; results return in rax. Calls push their argument slots in
   reverse declaration order, then call, then pop; the whole convention is
   the proposal Section 5.2's internal stack convention, before alignment
   refinement and register allocation.
4. Lay out one string object per data blob (length, payload pointer, payload,
   padding to eight); the object's pointer field is one recorded fixup.
5. Patch all recorded fixups in place: call displacements, RIP-relative data
   references, absolute payload pointers. Every earlier position survives
   patching inside mutable byte buffers.
6. Unsupported instruction forms (`add` through `label` in this slice's
   instruction union) fail `select_scene` with a deterministic message rather
   than inventing bytes.

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
/tmp/native-probe no-label                   # one validation message per pinned mode
/tmp/native-probe elf /tmp/hello && /tmp/hello   # write and run the native executable
```

## Verification

- The hello scene's rendering is pinned byte for byte.
- The growth scene is pinned structurally (line counts and spot lines), so
  every table-doubling path is exercised deterministically.
- The six validation messages plus the unknown-mode fall-through are pinned.
- The skeleton writes a static ELF (no interpreter, no dynamic section, two
  program headers) whose run output and status match the C pipeline's
  program for the same source; repeated productions are byte-identical.
- Both pipelines produce byte-identical ELFs: the Kitten-written `purr-kt`
  sidecar builds the probe and runs the whole skeleton, and its ELF bytes
  equal the Go pipeline's.
