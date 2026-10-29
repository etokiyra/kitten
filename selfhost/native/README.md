# The native backend's typed IR, written in Kitten

`selfhost/native` is the zero-dependency backend's first layer: a typed,
three-address scene that every later native stage — the layout pass, the x86-64
selector, the ELF writer — reads. It is the staging laid down by
`proposals/zero-dependency-toolchain.md` Section 5, and its
well-formedness gate (`check_scene`) is the backend's first verification point.
Phase 10.0 approved the design; Phase 10.1 landed this scene and nothing
beyond it: there is no lowering yet, no machine code, and no ELF output, and no
part of the production sidecar depends on it.

## Files

- `selfhost/native/ir.ktn` — `NativeScene`, its builders, and `check_scene`,
  the whole-scene well-formedness pass.
- `selfhost/native/text.ktn` — the deterministic text renderer whose pinned
  output protects the scene's shape as the backend grows around it.
- `selfhost/native.ktn` — the probe that renders the skeleton-target scene and
  prints validation messages for the failure modes.
- `cmd/purr/native_test.go` — the pinned tests, including a both-pipeline run
  of the probe.

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
  will produce, not an output it yet has.
- Float operations, conversions, struct/union field access, allocation, bounds
  checks, tag switches, runtime-module intrinsics, and every selector or ELF
  concern. The instruction union is open, and the validation grows with each
  added form.

## Running the probe

```sh
purr build selfhost/native.ktn -o /tmp/native-probe
/tmp/native-probe            # print hello scene
/tmp/native-probe big        # growth corpus: 12 functions, 9 blobs, 17 bindings each
/tmp/native-probe no-label   # one validation message per pinned mode
```

## Verification

- The hello scene's rendering is pinned byte for byte.
- The growth scene is pinned structurally (line counts and spot lines), so
  every table-doubling path is exercised deterministically.
- The six validation messages plus the unknown-mode fall-through are pinned.
- The probe is also built and run through the Kitten-written `purr-kt`
  sidecar, and all outputs agree with the Go pipeline byte for byte.
