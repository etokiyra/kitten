# Native language primitives for the zero-dependency toolchain

> **PROPOSED, not adopted.**
>
> This is a design-only proposal. It changes no code, tests, or `SPEC.md` text.
> Implementation requires explicit approval, one small commit per step. The
> language decisions in this document are recommendations for that approval, not
> current behavior.
>
> Date: 2026-10-18. Starting commit: `d95b5e1`.
> Parent design: `proposals/zero-dependency-toolchain.md`.
>
> **Phase 8.1/8.2/8.3/8.4/8.5/8.6 status:** Section 2 (defined integer arithmetic),
> Section 3 (bitwise/shift operators and hex/binary literals), Section 4
> (`byte` and `byte[]`), Section 5 (explicit conversions), Section 6
> (recoverable file, directory, and process APIs), and Section 7 (current
> directory) are **ADOPTED**. Section 8 remains **PROPOSED, not adopted**.

## 0. Summary of the proposed decisions

This proposal covers:

1. defined two's-complement integer arithmetic, including division failures;
2. bitwise and shift operators `& | ^ ~ << >>` on `int`;
3. a `byte` type and `byte[]` arrays;
4. explicit byte and string/bytes conversions;
5. recoverable file, directory, and process built-ins built from a small
   predeclared-union family;
6. `try_current_directory()`, the `cwd()`-style primitive;
7. a compiler-reserved `<runtime>` module containing the only raw-memory and
   syscall intrinsics.

The first implementation commit is the defined-integer-arithmetic fix in
Section 2. It is separate because the audit found a real, existing
undefined-behavior gap.

## 1. Audit

### 1.1 Operators and precedence today

- `lexer/token.go` defines the operator token kinds. There is no `&`, `|`, `^`,
  `~`, `<<`, or `>>`; `&` and `|` are only recognized as the first half of
  `&&` and `||` by `lexer/lexer.go:scanOperator`. A single `&` or `|`, and any
  `~`, is an “unexpected character” lexical error today.
- The lexer differential corpus in `cmd/purr/differential_test.go` currently
  classifies `0x1F` and `0b101` as invalid literals. They become valid in the
  bitwise step, while `0o17`, `1_000`, and suffixes remain invalid.
- `parser/parser.go:binaryPrecedence` and
  `selfhost/parser/parser.ktn:binary_precedence` implement the current levels:
  assignment, `||`, `&&`, equality, relational, additive, multiplicative.
  Unary operators are parsed by `parseUnary`/`parse_unary`; there is no unary
  bitwise complement.
- `checker/expr.go:checkBinary`, `checkUnary`, and `checkArithmetic`, and
  `selfhost/checker/expr.ktn:check_binary`, `check_unary`, and
  `check_arithmetic`, dispatch by operator text and require exact operand-type
  matching. `checkArithmetic` accepts `+ - * / %`; `- * /` are numeric-only,
  `%` is int-only, and `+` is also defined for strings.
- `codegen/emit.go:binaryExpression` and
  `selfhost/codegen/expressions.ktn:binary_expression` pass most operators
  through to C with explicit parentheses. String `+`, `==`, and `!=` are the
  only special cases today. `codegen/names.go:cType` and
  `selfhost/codegen/names.ktn:c_type` map the primitive types.
- `SPEC.md` Sections 17.2 and 18.3 document exactly those operators. Section
  6.1 accepts only decimal integer literals. Section 18.2 says there are no
  implicit conversions and no casts.

### 1.2 Integer overflow: a real bug in the current C pipeline

`SPEC.md` Section 18.1 says integer overflow behavior is **deferred**, and
Section 18.3 says the checker “does not guarantee overflow behavior.” Section
19.1 nevertheless says generated code relies on no undefined behavior. The
generated C currently violates that last claim.

`codegen/emit.go:expression` emits an `IntLiteral` as its source spelling
(`codegen/emit.go:228`), and `binaryExpression` emits ordinary signed C
operators (`codegen/emit.go:321`). The Kitten generator does the same in
`selfhost/codegen/expressions.ktn:1102-1141`. The C type is `int64_t`
(`codegen/names.go:cType`). No `-fwrapv` or equivalent is passed:
`cmd/purr/main.go:257` uses `-std=c11 -Wall -Wextra`.

Concrete source:

```kitten
fn main() {
    int x = 9223372036854775807;
    int y = x + 1;
    print(y);
}
```

Current generated C contains:

```c
int64_t ktn_var_x = 9223372036854775807;
int64_t ktn_var_y = (ktn_var_x + 1);
```

The addition has C signed overflow, which is undefined behavior. The current
runtime may happen to print `-9223372036854775808`, but the program is not
defined by the C standard and an optimizer is allowed to do anything with it.

The same applies to `-`, `*`, unary `-`, `+=`, `-=`, `++`, and `--`. Unary
minus of `INT64_MIN` is also signed overflow.

Division has separate undefined cases that are not diagnosed today:

- `x / 0` and `x % 0` emit direct C division. `checker/expr.go:checkArithmetic`
  and `selfhost/checker/expr.ktn:check_arithmetic` do not reject a constant zero
  and `codegen/emit.go:binaryExpression` does not add a check. A compiled
  `10 / 0` currently exits with SIGFPE (observed status 136), not a Kitten
  diagnostic.
- `INT64_MIN / -1` and `INT64_MIN % -1` are also undefined in C. The current
  generated C emits them directly; on x86-64 the observed result is SIGFPE
  (status 136).

Integer literal checking is also absent. `checker/expr.go:26` returns
`IntType` for every `IntLiteral`, and `codegen/emit.go:228` copies its text.
A decimal literal larger than `INT64_MAX` is therefore accepted by the checker
and may be emitted as a C constant of an implementation-selected type. The
Kitten checker has the same gap at `selfhost/checker/expr.ktn:32`.

**Finding: this is a real bug.** The fix belongs in the first implementation
commit, before bitwise operators add more arithmetic.

### 1.3 Conversions today

There are none. `checker/types.go` compares types by identity; `primitiveTypes`
contains only `int`, `float`, `bool`, `string`, and `void`. `SPEC.md` Sections
6.1, 18.2, and 20.7 say integer/float/string conversions are deferred and that
there are no casts. The only existing formatting path is `print`, whose
`printHelper`/`print_helper` selects a runtime formatter by type
(`codegen/emit.go:851`, `selfhost/codegen/expressions.ktn:860`). `try_read_file`
is the only predeclared result union (`checker/prelude.go`,
`selfhost/checker/prelude.ktn`).

This matters for `byte`: a new primitive with “no implicit conversions” needs a
deliberate explicit-conversion surface, not an accidental cast through C.

### 1.4 Arrays, strings, and byte buffers

- An array is `ktn_array *`, a GC-managed reference with a length, an element
  size, and zero-initialized storage (`runtime/csrc/kitten_runtime.h`,
  `ktn_rt_array_new`/`ktn_rt_array_at`; `codegen/names.go:cType`). Element size
  is supplied by the generator through `sizeof(<element C type>)`
  (`codegen/emit.go:newArrayExpression`,
  `selfhost/codegen/expressions.ktn:new_array_expression`). A `byte[]` therefore
  fits the existing representation with element size 1.
- A string is an immutable, length-carrying, NUL-terminated object
  (`runtime/csrc/kitten_runtime.h`), indexed as an `int` byte (0..255), not a
  one-character string. The current string helpers have no byte-array
  conversion. `lexer/string.go:DecodeString` decodes literal escapes; it is not
  a general string/bytes conversion.
- The runtime can allocate a byte array and copy bytes, but it currently has no
  API that exposes such a buffer to Kitten code. `runtime/csrc` has no file
  write or directory API.

### 1.5 Built-ins and the prelude mechanism

- The current SPEC.md Section 18.8 list is `print`, `eprint`, `exit`,
  `length`, `argv`, `has`, `substring`, `read_file`, and `try_read_file`. Go
  built-ins are declared in `checker/checker.go:builtinFunctions` and keyed in
  `checker/symbols.go`; the Kitten mirror is
  `selfhost/checker/state.ktn:builtin_functions`. `try_read_file` is registered
  late against the prelude type by `checker.CheckModules` and the Kitten
  checker. Dispatch is in `checker/expr.go:checkCall` and
  `selfhost/checker/expr.ktn:check_call`; lowering is in
  `codegen/emit.go:callExpression` and
  `selfhost/codegen/expressions.ktn:call_expression`.
- The existing prelude is one fixed source parsed by
  `checker/prelude.go:parseReadFileResultPrelude`, injected by
  `checker.CheckModules` when `usesTryReadFile` finds a syntactic
  `try_read_file` call. The Kitten mirror is
  `selfhost/checker/prelude.ktn` and `selfhost/checker/checker.ktn`. Its name is
  reserved in every program by `checker/checker.go:collectDeclarations` and
  `selfhost/checker/checker.ktn:collect_declaration`. The generated helper is
  emitted by `codegen/codegen.go:emitPreludeHelper` and
  `selfhost/codegen/declarations.ktn:emit_prelude_helper_section`.
- The mechanism generalizes, but not by replacing the one fixed source with one
  universal prelude. That would change every existing `try_read_file` program’s
  generated C by adding unused types and helpers. The recommended generalization
  is per-feature source fragments, assembled in a fixed order, with one
  synthetic module. Section 6 gives the exact rule.

### 1.6 The four-pipeline change map

Every feature must change both implementations and their differential harnesses.
The current “Adding a built-in” checklist in `AGENTS.md` covers the built-in
cases; operators and types need these additional rows:

| Area | Go | Kitten | Shared verification |
| --- | --- | --- | --- |
| Tokens | `lexer/token.go`, `lexer/lexer.go`, `lexer/lexer_test.go` | `selfhost/lexer/tokens.ktn`, `selfhost/lexer/lexer.ktn` | `cmd/purr/lexer_differential_test.go` |
| AST/grammar | `parser/ast.go`, `parser/parser.go`, `parser/parser_test.go` | `selfhost/parser/ast.ktn`, `selfhost/parser/parser.ktn` | `cmd/purr/parser_differential_test.go` |
| Types | `checker/types.go`, `checker/checker.go` | `selfhost/checker/model.ktn`, `selfhost/checker/state.ktn` | checker and semantic corpora |
| Operator rules | `checker/expr.go` | `selfhost/checker/expr.ktn` | `cmd/purr/checker_differential_test.go`, `cmd/purr/semantics_differential_test.go` |
| C names/layout | `codegen/names.go` | `selfhost/codegen/names.ktn` | declaration/expression corpora |
| C expressions | `codegen/emit.go` | `selfhost/codegen/expressions.ktn` | expression, function, unit, call corpora |
| Runtime API | `runtime/runtime.go`, `runtime/csrc/*` | native runtime later | runtime fixtures; C/native parity later |
| Prelude | `checker/prelude.go`, `checker/checker.go`, `codegen/codegen.go` | `selfhost/checker/prelude.ktn`, `checker.ktn`, `selfhost/codegen/declarations.ktn` | checker/prelude and whole-unit corpora |
| Diagnostics | checker error sites | Kitten mirror | checker/message corpora |
| Documentation | `SPEC.md`, `README.md`, `AGENTS.md` | `selfhost/*/README.md` | measured counts |

`selfhost/driver.ktn` and the fixpoint must be rerun whenever the Kitten
compiler’s own source changes. The fixpoint is the only check that proves the
new Go and Kitten implementations agree on the compiler itself.

### 1.7 What the Kitten compiler’s own source needs, and collisions

- **`byte` collides with existing identifiers.** `byte` is currently an
  ordinary identifier used by the compiler’s own source: for example
  `selfhost/lexer/lexer.ktn:advance`, `is_digit`, `is_identifier_start`,
  `is_identifier_part`, `hex_pair`, and `quoted_byte`; `selfhost/parser/
  parser.ktn:decode_string` and `hex_pair`; `selfhost/codegen/names.ktn:
  octal_three`; and `tests/lexer_proof.ktn`. Test source strings in
  `checker/checker_test.go` and `cmd/purr/main_test.go` also use `int byte`.
  Making `byte` a reserved keyword is a **breaking source change**. The byte
  commit must mechanically rename those locals/parameters (for example to
  `unit` or `octet`) and update the test strings. No output changes are
  expected from the rename, but every corpus must be rerun.
- **`~`, `<<`, and `>>` do not collide with valid source.** They are currently
  lexical errors or two adjacent `>`/`<` tokens. `lexer/lexer_test.go` has
  explicit “unexpected character” cases for `&`, `|`, and `~`; those cases must
  change into token cases. No current `.ktn` file uses a bitwise expression.
- **`&` and `|` already have `&&`/`||` spellings.** The lexer must implement the
  longest match: `&&` and `||` remain their current tokens; a single character
  becomes the new bitwise token.
- `selfhost/checker/expr.ktn`, `selfhost/codegen/expressions.ktn`, and the
  compiler’s own parser use integer arithmetic heavily. The defined-arithmetic
  fix changes their generated C but not their source behavior. The fixpoint
  must still pass byte for byte.

### 1.8 Prelude-family cost

The current single prelude costs one fixed source parse, one appended module,
one public type, one reserved name, and one non-static helper. A family does not
need generics:

- With **per-feature fragments**, each triggered feature adds its fixed union
  source to one assembled prelude module. `ReadFileResult` alone still assembles
  exactly today’s source, so existing `try_read_file` C is unchanged.
- Each additional result type costs one `union` declaration, one
  `public_types` entry, one reserved name in both checkers, and one constructor
  per variant. Each triggered builtin costs one `Function` entry and one
  generated helper. All of this is O(types + builtins), not O(program size).
- The scanner must return a set of used built-ins, not one boolean. Go and
  Kitten both need the same set and the same fragment order.
- The loader does not change: preludes remain a checker concern and are never
  loaded from disk.

## 2. Defined integer arithmetic (ADOPTED in Phase 8.1)

### 2.1 Need

The native backend, ELF writer, GC, and runtime cannot rely on C’s undefined
signed behavior. The current C pipeline is already wrong for programs that
overflow, divide by zero, or divide `INT64_MIN` by `-1`. A defined Kitten
semantic is required before the native backend can share behavior with the C
backend.

### 2.2 Proposed SPEC.md replacement text

Replace the deferred sentences in Sections 18.1, 18.3, and 19 with:

> `int` is a signed 64-bit two's-complement integer. Every arithmetic result is
> the unique value modulo 2^64 interpreted as a signed two's-complement 64-bit
> integer. This applies to `+`, `-`, `*`, unary `-`, `+=`, `-=`, `++`, and
> `--`; overflow wraps and is not a runtime failure.
>
> `/` truncates toward zero. `%` is the remainder with the sign of the
> dividend. Division or remainder by zero is a fatal runtime error with the
> message `integer division by zero`. `INT64_MIN / -1` is `INT64_MIN` and
> `INT64_MIN % -1` is `0`; neither is undefined.
>
> An integer literal that is not representable under the rules in Section 6.1
> is a checker error. A hexadecimal or binary literal is a 64-bit bit pattern;
> a value with the high bit set denotes the corresponding negative
> two's-complement value.

Section 19.1 must additionally say:

> Generated C performs integer arithmetic through the runtime's defined
> 64-bit helpers. It never evaluates a signed C overflow, division by zero, or
> `INT64_MIN / -1`.

### 2.3 Failure and lowering

Runtime failure (C pipeline):

```text
purr: integer division by zero
```

Generated C helpers (names are MIT runtime API):

```c
int64_t ktn_rt_i64_add(int64_t a, int64_t b);
int64_t ktn_rt_i64_sub(int64_t a, int64_t b);
int64_t ktn_rt_i64_mul(int64_t a, int64_t b);
int64_t ktn_rt_i64_neg(int64_t a);
int64_t ktn_rt_i64_div(int64_t a, int64_t b);
int64_t ktn_rt_i64_mod(int64_t a, int64_t b);
int64_t ktn_rt_i64_add_assign(int64_t *slot, int64_t b);
int64_t ktn_rt_i64_sub_assign(int64_t *slot, int64_t b);
int64_t ktn_rt_i64_pre_inc(int64_t *slot);
int64_t ktn_rt_i64_post_inc(int64_t *slot);
int64_t ktn_rt_i64_pre_dec(int64_t *slot);
int64_t ktn_rt_i64_post_dec(int64_t *slot);
```

`add`, `sub`, `mul`, and `neg` operate on `uint64_t` bit patterns and convert
the result back to `int64_t` using a `memcpy` bit reinterpretation (or an
equivalent target-ABI-defined conversion); they contain no signed overflow. The
pre/post helpers return the new and old values respectively, as prefix and
postfix require.
`div` and `mod` check zero, special-case `INT64_MIN / -1` as above, and otherwise
use the now-defined C division. `add_assign` and `sub_assign` take the address of the
target so an index or field expression is evaluated once, preserving the
current compound-assignment rule.

Generated C becomes, for example:

```c
int64_t ktn_var_y = ktn_rt_i64_add(ktn_var_x, 1);
int64_t ktn_var_q = ktn_rt_i64_div(ktn_var_x, ktn_var_y);
```

Do not use `-fwrapv` as the primary fix. It is a compiler extension, it does not
make the emitted C defined by the C standard, and it does not address division
by zero, `INT64_MIN / -1`, or a checked assignment target. `-fwrapv` may be used
as an extra diagnostic aid, not as the semantics.

### 2.4 Files and acceptance

Files: `checker/expr.go`, `selfhost/checker/expr.ktn`, `codegen/emit.go`,
`selfhost/codegen/expressions.ktn`, `runtime/runtime.go`,
`runtime/csrc/kitten_runtime.{c,h}`, both `codegen/names` files, all arithmetic
corpora, `tests/` overflow cases, and the fixpoint.

Acceptance: new tests for `INT64_MAX + 1`, `INT64_MIN - 1`, multiplication, unary
minus of `INT64_MIN`, `+=`, `-=`, `++`, `--`, `INT64_MIN / -1`,
`INT64_MIN % -1`, `/ 0`, and `% 0`; exact expected stdout/stderr/status; the
full suite under GCC and Clang; and Go/Kitten generated C byte-identical for
every existing corpus and fixture. This commit must land before bitwise
operators.

## 3. Bitwise and shift operators (ADOPTED in Phase 8.2)

### 3.1 Need

The native backend and ELF writer need masks (`&`), combining (`|`), XOR (`^`),
complement (`~`), field packing (`<<`), and field extraction (`>>`) on integer
bit patterns. Runtime hashes, tags, alignment, ELF headers, and GC mark bits all
need them. Making them user-facing avoids a language that can only implement
its own runtime through hidden unsafe calls.

### 3.2 Proposed grammar and precedence

Replace the relevant expression levels with:

```text
assignment       = bitwise_or { ( "=" | "+=" | "-=" ) bitwise_or } ;
bitwise_or       = bitwise_xor { "|" bitwise_xor } ;
bitwise_xor      = bitwise_and { "^" bitwise_and } ;
bitwise_and      = equality { "&" equality } ;
equality         = relational { ( "==" | "!=" ) relational } ;
relational       = shift { ( "<" | "<=" | ">" | ">=" ) shift } ;
shift            = additive { ( "<<" | ">>" ) additive } ;
additive         = multiplicative { ( "+" | "-" ) multiplicative } ;
multiplicative   = unary { ( "*" | "/" | "%" ) unary } ;
unary            = ( "-" | "!" | "~" | "++" | "--" ) unary | postfix ;
```

All binary levels are left-associative except assignment. Precedence from
lowest to highest is now: assignment, `||`, `&&`, `|`, `^`, `&`, equality,
relational, shift, additive, multiplicative. This is the C# ordering in the two
places that matter here: shifts bind tighter than relational operators, and
`&` binds tighter than `^`, which binds tighter than `|`, which binds tighter
than `&&`.

### 3.3 Proposed typing and runtime rules

Proposed SPEC Section 18.3 additions:

| Operators | Operand types | Result |
| --- | --- | --- |
| `&` `\|` `^` | two `int` | `int` |
| `~` | one `int` | `int` |
| `<<` `>>` | two `int` | `int` |

- The operands must be exactly `int`; `byte`, `bool`, and `float` are errors.
- `~` is a prefix unary operator.
- `&`, `|`, and `^` operate on the 64-bit two's-complement bit pattern.
- `<<` shifts left, discarding bits shifted out; the result is interpreted as a
  signed two's-complement `int`.
- `>>` is an **arithmetic** right shift: vacated high bits copy the sign bit.
- The right operand of `<<` or `>>` must be in `0..63`. A negative count or a
  count greater than 63 is a fatal runtime error with the message
  `shift count <n> is outside 0..63`. This check is performed at runtime for
  every shift; constant folding must not bypass it.
- The left and count expressions are each evaluated once, left first.

### 3.4 Literal rules

Proposed SPEC Section 6.1 replacement:

> An integer literal is decimal, hexadecimal, or binary. Decimal literals are a
> sequence of decimal digits with value at most `INT64_MAX`. Hexadecimal
> literals start with lowercase `0x`; binary literals start with lowercase
> `0b`. Hexadecimal digits may be upper or lower case. Underscores, octal forms,
> signs, and suffixes are not part of a literal; a leading `-` is unary minus.
>
> A hexadecimal or binary literal denotes a 64-bit bit pattern. Hexadecimal
> literals have at most 16 digits after the prefix; binary literals have at most
> 64. Its value must be at most `0xFFFFFFFFFFFFFFFF`; values at or above
> `0x8000000000000000` denote the corresponding negative two's-complement
> `int`. Decimal literals may not denote a value above `INT64_MAX`.
>
> A decimal literal above `INT64_MAX`, or a hexadecimal/binary literal with too
> many digits, is a checker error at the literal: `integer literal out of
> range`. An empty digit run (`0x`, `0b`), a non-digit suffix (`0x1g`,
> `0b102`), uppercase prefixes (`0X`, `0B`), or an octal-looking form (`0o17`)
> is a lexical error at the literal's start: `invalid integer literal`.

There is no `byte` literal. A byte value is written through
`byte_from_int(0x41)`.

`0x8000000000000000` denotes `INT64_MIN`; `0xFFFFFFFFFFFFFFFF` denotes `-1`.
This is the intended way to write bit masks; no implicit conversion is
involved.

### 3.5 C lowering

- `&`, `|`, `^`, and `~`: emit direct parenthesized C operations. These are
  defined on signed integers; the target is two's complement.
- `<<` and `>>`: emit calls to the MIT runtime helpers
  `ktn_rt_i64_shl(left, count)` and `ktn_rt_i64_shr(left, count)`. The helpers
  validate `0 <= count <= 63` and perform the operation on `uint64_t`, with
  arithmetic sign fill for `>>`.
- Hex/binary literals: the lexer validates their digit runs and widths, the
  checker accepts their bit patterns, and both generators emit a canonical C
  integer. Values in `0..INT64_MAX` emit decimal; `2^63` emits `INT64_MIN`;
  larger values emit their negative decimal bit-pattern value
  (`0xFFFFFFFFFFFFFFFF` emits `-1`). Existing decimal literals keep their source
  spelling so existing generated C stays stable.
- The full-suite C comparison must include the compiler source; this feature is
  not complete until the fixpoint still passes. Shifts are checked by the MIT
  `ktn_rt_i64_shl`/`ktn_rt_i64_shr` helpers; `&`, `|`, `^`, and `~` are direct
  C operators.

### 3.6 Diagnostics

Use the existing `InvalidOperator`/`TypeMismatch` categories with wording
consistent with `checkArithmetic`:

- `operator "&" requires int operands, found <type>`
- `operator "<<" requires int operands, found <type>`
- `operator '~' requires an int operand, found <type>`
- `operator "&" requires operands of the same type, found int and byte`

### 3.7 Files, risks, alternatives

Files: `lexer/token.go`, `lexer/lexer.go`, `parser/parser.go`,
`checker/expr.go`, `codegen/names.go`, `codegen/emit.go`, `runtime/csrc` (shift
helpers), all corresponding `selfhost` files, `lexer/lexer_test.go`, and the
lexer/parser/checker/semantics/expressions/functions/unit/calls corpora.

Risks: precedence is a breaking change if a future grammar adds operators;
`>>` must be lexed before `>`; `>>` on negative values must be arithmetic and
tested; shift failure checks add a branch. Alternatives: runtime intrinsics only
(rejected: the compiler needs readable bit operations), C# precedence (chosen),
or a separate `>>>` unsigned shift (not proposed; the native backend can use
`int_from_byte`/bit patterns).

## 4. `byte` and `byte[]` (ADOPTED in Phase 8.3)

### 4.1 Need

ELF images contain NUL and arbitrary bytes; strings are immutable and
NUL-terminated. The runtime and native compiler need mutable 8-bit buffers for
code, data, hashes, GC metadata, and file writes. A user-visible `byte` is also
the smallest typed element for file contents.

### 4.2 Proposed SPEC wording

Add to Section 18.1:

> `byte` is an unsigned 8-bit integer with values 0 through 255. Its zero value
> is 0. It is a primitive type, distinct from `int` and `bool`. There are no
> implicit conversions between `byte` and any other type.

Add to Sections 18.2 and 18.3:

> `byte` supports `==`, `!=`, `<`, `<=`, `>`, and `>=` with another `byte`,
> using unsigned comparison. `byte` does **not** support arithmetic, bitwise,
> shift, unary `-`, `~`, `++`, `--`, `+=`, or `-=`; convert explicitly with
> `int_from_byte` and `byte_from_int`. `print` and `eprint` accept `byte` and
> write its unsigned decimal value.
>
> `byte[]` is an array type built by the ordinary array rules. `new byte[n]`
> creates a zero-initialized byte array. Array indexing is by `int`; a
> `byte[]` element has type `byte`. String indexing remains an `int` byte value
> for compatibility; use `string_to_bytes` to obtain a mutable byte array.

Add to Section 19.1:

> `byte` maps to `uint8_t`. A `byte[]` uses the existing `ktn_array`
> representation with an element size of `sizeof(uint8_t)`, which is one byte.

### 4.3 C lowering

- `cType(byte)` is `uint8_t` in both generators.
- `new byte[n]` is `ktn_rt_array_new(n, sizeof(uint8_t))`.
- `bytes[i]` is `*((uint8_t *)ktn_rt_array_at(bytes, i))`.
- `print(byte_value)`/`eprint(byte_value)` reuse `ktn_rt_print_int`/`eprint_int`
  with `(int64_t)value`; no new runtime formatter is required.
- Structs and map values containing `byte` use C's normal layout; native layout
  will later record byte size 1 and alignment 1.

### 4.4 `byte` is a reserved keyword

`byte` must be added to the Go `lexer.keywords` and to
`selfhost/lexer/tokens.ktn`/`lexer.ktn`, and to `is_type_keyword` in both
parsers. It is a reserved type keyword, not an ordinary identifier.

This is the collision reported in Section 1.7. The byte commit must rename every
existing `byte` identifier in the compiler and tests. Suggested migration:
rename value locals and parameters to `unit` or `octet`; leave prose/comments
alone. No generated C should change from the rename, but every corpus and the
fixpoint must be re-run.

### 4.5 Diagnostics and files

- `operator '+' requires numeric operands, found byte`
- `operator '&' requires int operands, found byte`
- `array index must be int, found byte`
- `cannot convert byte to int implicitly; use int_from_byte`

Files: `checker/types.go`, `checker/checker.go`, `checker/expr.go`,
`selfhost/checker/model.ktn`, `state.ktn`, `expr.ktn`, both lexers/parser,
both codegen names/expressions, both print helpers, semantic model rendering,
and the compiler’s `byte` identifier migration.

Risks: breaking existing user code named `byte`; C `uint8_t` promotion in
comparisons/print is safe but must be explicit in generated C; no arithmetic
means users must convert often. Alternatives: `u8`/`uint8` to avoid collision;
soft keyword; internal-only byte buffer. The human decision names `byte`, so the
proposal accepts the rename.

## 5. Explicit conversions (ADOPTED in Phase 8.3)

### 5.1 Need and audit

The current language has no casts and no conversions (`checker/types.go`,
`SPEC.md` 18.2/20.7). `byte` cannot be used without explicit conversion, and
the native compiler/runtime need to turn strings into byte buffers and back.

### 5.2 Proposed built-ins

Add these to Section 18.8:

```text
int_from_byte(byte value) -> int
byte_from_int(int value) -> byte
string_to_bytes(string value) -> byte[]
bytes_to_string(byte[] value) -> string
```

Rules:

- `int_from_byte` zero-extends: result is `0..255`.
- `byte_from_int` keeps the low eight bits: result is `value & 0xFF` under the
  two's-complement bit pattern. It never fails.
- `string_to_bytes` returns a fresh GC-managed `byte[]` of the string's byte
  length, containing the same bytes.
- `bytes_to_string` copies the array bytes into a fresh immutable string. If
  any byte is NUL, it is a fatal runtime error:
  `purr: byte array contains a NUL byte at index <i>`, where `<i>` is decimal.
  (A recoverable result would add another predeclared union; that is not
  proposed.)
- All four are ordinary built-ins: exact arity, exact argument type, shadowable
  by a user declaration, and no implicit conversions.

### 5.3 Deferred conversions

The audit found no int/float or string/number conversions today. This proposal
does not add them: `float` formatting remains exactly today’s `%g` behavior
(deferred), and int↔float would need explicit rounding, NaN, and out-of-range
decisions. `int_from_float`/`float_from_int` and string/number parsing are a
separate future decision. The native runtime can format its own diagnostics
internally.

### 5.4 C lowering and files

- `int_from_byte(x)` -> `((int64_t)(x))`
- `byte_from_int(x)` -> `((uint8_t)((uint64_t)(x) & UINT64_C(0xFF)))`
- `string_to_bytes` -> `ktn_rt_string_to_bytes(value)`
- `bytes_to_string` -> `ktn_rt_bytes_to_string(value)`

Runtime helpers return `ktn_array *`/`const ktn_string *`; the byte-array
helper allocates `sizeof(uint8_t)` elements and copies the string bytes. The
string helper checks the byte array length and each byte, reports the first
NUL index, allocates a string, and copies the bytes.

Files: both built-in tables/checkers, both call lowerings, both `names` files,
`runtime/runtime.go`, `runtime/csrc`, checker/expression/unit/calls corpora, and
the runtime fixture. Risks: NUL policy, allocation failure, and aliasing; the
copying rules above avoid aliasing surprises.

Alternatives: `as` casts (new grammar and precedence; rejected for now),
overloading by argument type (new dispatch; rejected), implicit byte
conversions (forbidden by the decision).

## 6. Recoverable file, directory, and process APIs (ADOPTED in Phases 8.4 and 8.5)

### 6.1 Need

The native `purr build` must write an ELF file; `purr run` must launch it;
`purr new`, `purr add`, formatter, documentation, and tests need directory
creation/listing/removal and file writes. These must be recoverable: a path
failure is data, not a process termination. Reuse the 5.4 prelude decisions
rather than adding generics.

### 6.2 Predeclared source

Use these public unions, assembled only as needed (Section 6.3):

```kitten
public union FileResult {
    Ok
    Failed { string message; }
}

public union ListDirectoryResult {
    Ok { string[] entries; }
    Failed { string message; }
}

public union RunResult {
    Ok { int status; }
    Failed { string message; }
}

public union CurrentDirectoryResult {
    Ok { string path; }
    Failed { string message; }
}
```

`ReadFileResult` remains exactly as it is. The adopted names `FileResult`,
`ListDirectoryResult`, `RunResult`, and `CurrentDirectoryResult` are reserved
against every user top-level declaration in every program, whether or not a
trigger fires, following the `ReadFileResult` rule.

### 6.3 Built-ins and exact rules

Add to Section 18.8:

```text
try_write_file(string path, string text) -> FileResult
try_write_bytes(string path, byte[] bytes) -> FileResult
try_create_directory(string path) -> FileResult
try_remove(string path) -> FileResult
try_list_directory(string path) -> ListDirectoryResult
try_run(string path, string[] arguments) -> RunResult
```

- `try_write_file` creates or truncates a regular file with mode `0644`, writes
  every byte of `text`, closes it, and returns `FileResult.Ok`; empty text is
  valid. `try_write_bytes` is the same for arbitrary bytes, including NUL.
- `try_create_directory` creates exactly one directory with mode `0755`. It does
  not create missing parents. An existing path is a failure.
- `try_remove` removes a file or an empty directory, like `os.Remove`; a
  non-empty directory is a failure. Recursive removal is not part of this
  proposal.
- `try_list_directory` returns the entry names only, excluding `.` and `..`,
  sorted by bytewise name order, in `entries`. It returns a fresh `string[]`;
  it does not return metadata and does not recurse.
- `try_run` executes the program at `path`, with `arguments` supplying
  `argv[1..]`; the runtime uses `path` as `argv[0]`. It does not invoke a shell
  and does not search `PATH`; the path is passed to the OS. It inherits the
  current directory, environment, and standard streams, waits for the process,
  and returns `RunResult.Ok { status = ... }`.
- A normal exit returns its status in `0..255`. A process terminated by signal
  `n` returns `128 + n`. Failure to start or wait returns
  `RunResult.Failed { message = ... }`.
- All failures are recoverable. `Failed.message` uses the runtime's
  Go-compatible OS text, without a `purr:` prefix or loader prefix. Examples:
  `open /x: no such file or directory`, `mkdir /x: file exists`,
  `remove /x: directory not empty`, `exec /x: no such file or directory`,
  `getcwd: ...`.
- All built-ins are shadowable. A syntactic call triggers the corresponding
  prelude type even if the name is shadowed.
- Output capture, stdin redirection, environment control, and timeouts are
  **deferred**.

### 6.4 Prelude injection and cost

Replace the single `usesTryReadFile` boolean with a set of used prelude names.
Trigger names are:

```text
try_read_file try_write_file try_write_bytes try_create_directory
try_remove try_list_directory try_run try_current_directory
```

If the set is non-empty, `CheckModules` appends exactly one synthetic module
with path `<prelude>` and id equal to the loaded module count. Its source is the
concatenation of the fragments for the triggered names in this fixed order:

1. `ReadFileResult` (for `try_read_file`)
2. `FileResult` (for `try_write_file`, `try_write_bytes`,
   `try_create_directory`, `try_remove`)
3. `ListDirectoryResult` (for `try_list_directory`)
4. `RunResult` (for `try_run`)
5. `CurrentDirectoryResult` (for `try_current_directory`, Section 7)

A `try_read_file`-only program therefore assembles exactly today’s source and
keeps its generated C unchanged. Shared `FileResult` is included once. All
prelude type names are reserved in every program. Only triggered built-ins are
registered; a built-in whose name is not called remains undefined, and a user
function of the same name still shadows it.

Cost per additional result type: one fixed fragment, one parsed union, one
public type, one reserved name, one constructor per variant. Cost per additional
built-in: one `Function` entry and one non-static generated helper. The Go and
Kitten checkers each duplicate the fragment text; differential whole-unit tests
must compare the assembled module and generated C. The loader is unchanged.

### 6.5 C lowering and runtime ABI

Generated helpers use the existing non-static pattern, for example:

```c
FileResult *ktn_prelude_<P>_try_write_file(
    const ktn_string *ktn_path, const ktn_string *ktn_text);
```

The helper calls the MIT runtime primitive and constructs `Ok` or `Failed`.
Proposed runtime declarations:

```c
bool ktn_rt_try_write_file(const ktn_string *path,
                           const ktn_string *text,
                           const ktn_string **message);
bool ktn_rt_try_write_bytes(const ktn_string *path,
                            const ktn_array *bytes,
                            const ktn_string **message);
bool ktn_rt_try_create_directory(const ktn_string *path,
                                 const ktn_string **message);
bool ktn_rt_try_remove(const ktn_string *path,
                       const ktn_string **message);
bool ktn_rt_try_list_directory(const ktn_string *path,
                               ktn_array **entries,
                               const ktn_string **message);
bool ktn_rt_try_run(const ktn_string *path,
                    const ktn_array *arguments,
                    int64_t *status,
                    const ktn_string **message);
```

`try_list_directory` returns a `ktn_array` of `const ktn_string *` elements in
sorted order in `entries`. `try_run` builds an argv vector from `path` and the argument
array, forks/execs, waits, and reports a normalized status. The byte array
argument of `try_write_bytes` is the existing `ktn_array *` with element size
one.

### 6.6 Diagnostics and files

Wrong arity/type diagnostics use the generic built-in path:
`function "try_write_file" expects 2 argument(s), got 1`,
`argument 2 of "try_write_file" expected string, found int`, and the same for
the other APIs. OS failures are returned, not diagnosed by the checker.

Files: `checker/checker.go`, `checker/expr.go`, `checker/prelude.go`,
`selfhost/checker/state.ktn`, `expr.ktn`, `prelude.ktn`, `checker.ktn`, both
`codegen` call paths/names/declarations, `runtime/runtime.go`,
`runtime/csrc/kitten_runtime.{c,h}`, `cmd/purr/try_read_file_test.go` or a new
runtime fixture, checker/semantics/unit/calls corpora, and the fixpoint.

Risks: prelude assembly drift between Go and Kitten; reserved-name churn;
directory sorting/locale; permission and root tests; process security; no
recursive remove; no metadata. Alternatives: individual result unions (more
types), one universal prelude (changes existing C), fatal APIs (rejected),
generic `Result<T>` (no generics).

## 7. `try_current_directory` (ADOPTED in Phase 8.6)

### 7.1 Need and exact rule

A `cwd()`-style primitive is needed to interpret relative paths, choose default
output locations, and support `purr new` and future package commands. It is
recoverable because `getcwd` can fail. Add to Section 18.8:

```text
try_current_directory() -> CurrentDirectoryResult
```

It returns the current working directory as an absolute path in
`CurrentDirectoryResult.Ok { path }`, or a `Failed { message }` from the OS.
It uses the kernel's `getcwd` result, ignores `$PWD`, and grows its buffer on
`ERANGE` rather than imposing a `PATH_MAX` limit. Failure text is
`getwd: <reason>`. The prelude fragment is the `CurrentDirectoryResult` union,
appended only when this builtin is called. Its C helper is
`ktn_prelude_<P>_try_current_directory`, backed by:

```c
bool ktn_rt_try_current_directory(const ktn_string **path,
                                  const ktn_string **message);
```

The name is deliberately `try_current_directory`, not `cwd`, because the
operation can fail. The Kitten driver can combine it with its path-cleaning
logic to accept relative roots; the current wrapper may continue to absolutize
paths. This built-in does not by itself add a working-directory *change*
primitive (`chdir` is not proposed).

Files: same prelude/builtin/checker/codegen/runtime set as Section 6, plus the
driver/CLI path-resolution tests when the Kitten CLI adopts it. Risks: symlink
and canonicalization semantics; current wrapper contract; relative-path
diagnostics. Alternative: return `Failed` through a file-style union (chosen);
a total `cwd()` string (rejected because `getcwd` can fail).

## 8. Compiler-reserved runtime module and intrinsics

### 8.1 Boundary

Name the module **`<runtime>`**. It is a synthetic compiler module, like
`<prelude>`: the loader can never produce that path from a file, and only the
native build embeds/appends it. The checker recognizes it by
`module.path == "<runtime>"`. User code cannot import it; an ordinary import
resolves a filesystem path and cannot name the synthetic module.

The source lives under `runtime/ktn/` (for example `core.ktn`) with an MIT
notice. The compiler and backend remain GPL-3.0-or-later; the runtime source is
MIT and its notice must travel with any embedded copy, exactly as
`runtime/csrc` does today. No user-program component becomes GPL.

### 8.2 Intrinsics visible only in `<runtime>`

Proposed intrinsics:

```text
raw_alloc(int size) -> int
raw_load8(int address) -> int
raw_load16(int address) -> int
raw_load32(int address) -> int
raw_load64(int address) -> int
raw_store8(int address, int value)
raw_store16(int address, int value)
raw_store32(int address, int value)
raw_store64(int address, int value)
raw_copy(int destination, int source, int count)
raw_zero(int destination, int count)
syscall(int number, int arg0, int arg1, int arg2, int arg3, int arg4, int arg5) -> int
address_of(T value) -> int
stack_bounds() -> int[]
```

- Addresses are `int` values only inside `<runtime>`; there is no user-facing
  pointer type and no `unsafe` block. `address_of` is type-directed: it accepts
  an lvalue or reference value and returns its address; the checker permits it
  only in `<runtime>`.
- `raw_alloc` returns a zeroed allocation; failure is fatal. `raw_*` access is
  unchecked by design
  and is only reachable from audited runtime code.
- `syscall` is the Linux x86-64 syscall ABI; all six argument slots are always
  supplied, with `0` for unused slots. It returns the raw syscall result.
- `stack_bounds()` returns a two-element `int[]` `[low, high]` for the
  conservative collector.
- User code that names any intrinsic in any other module receives the
  diagnostic `intrinsic "<name>" is only available in the compiler runtime
  module`. The names are not ordinary built-ins.

The compiler and native backend may call runtime functions, but only the
`<runtime>` module may use the intrinsic names. The C pipeline is unaffected;
the native runtime is linked/embedded and uses these calls instead of libc or
libgc.

### 8.3 Files and risks

Files: new `runtime/ktn/` MIT sources, native backend/checker gate, loader
identity handling, and native tests. The C runtime and C generator do not use
`<runtime>`. Risks: bypassing the gate, leaking addresses into user values,
auditing unsafe code, licensing the embedded source, and bootstrapping an
allocator before the GC exists. Alternatives: user-facing `unsafe` (rejected),
intrinsics as ordinary built-ins (rejected), a separate package registry
(rejected for the bootstrap).

## 9. Ordered implementation plan

Each item is one small commit after approval. Each commit ends with
`gofmt -l .`, `git diff --check`, `go vet ./...`, `go build ./cmd/purr`, and the
full suite under the default compiler, `CC=gcc`, and `CC=clang`; every change
that affects selfhost reruns `TestKittenFixpoint` and compares generated C byte
for byte.

1. **Defined integer arithmetic and literal range** (Section 2). First commit,
   because the current overflow/division behavior is a real bug. Acceptance:
   overflow/division/status tests; no signed overflow or division UB in
   generated C; all existing C and fixtures unchanged except the intended
   helper calls; fixpoint passes.
2. **Bitwise and shift operators, hexadecimal/binary literals** (Section 3).
   Acceptance: lexer/parser precedence cases, checker diagnostics, shift failure
   tests, every operator in the expression/function/unit corpora, C parity, and
   fixpoint.
3. **`byte`, `byte[]`, and the `byte` keyword migration** (Section 4).
   Acceptance: byte type/array/print/equality tests; `uint8_t` C; element size
   one; renamed compiler identifiers; all corpora and fixpoint.
4. **Explicit byte and string/bytes conversions** (Section 5). Acceptance:
   round trips, truncation, NUL failure, wrong-type diagnostics, C parity,
   runtime fixture, and fixpoint.
5. **File and directory APIs and `FileResult`/`ListDirectoryResult`**
   (Section 6). Acceptance: both pipelines on existing, missing, unreadable,
   directory, and permission paths; sorted listing; prelude trigger/reserved-name
   tests; no-trigger programs byte-identical; whole-unit and runtime parity.
6. **Process launch and `RunResult`** (Section 6). Acceptance: build a helper
   program, run it with arguments, compare status/signal behavior, no shell/PATH
   surprises, both pipelines, and fixpoint.
7. **`try_current_directory` and `CurrentDirectoryResult`** (Section 7).
   Acceptance: absolute path, failure shape, relative-path driver test, and
   no-trigger C unchanged.
8. **Reserved `<runtime>` module and intrinsics** (Section 8). Acceptance:
   user-code rejection, runtime-module compile, license/notice check, and the
   native backend's first use. This step does not affect existing C output; it
   is the gate for Goal 2 implementation.

### 9.1 Interleaving with Goal 3

Yes: file writing, directory create/list/remove, and `try_current_directory`
directly unblock Goal 3 work and can be implemented independently of the native
backend once approved. They make Kitten-written versions of:

- `purr new`: create directories and write scaffold files;
- `purr add`/`purr remove`: read and write `purr.toml`/`purr.lock`;
- `purr fmt`, `purr doc`, `purr publish`: write output/package files;
- `purr test`: create temporary directories and write test artifacts;
- `purr run`: process launch (separate commit);
- `purr build`: write the ELF output (also needs native backend).

The compiler/runtime primitives (steps 1-4) should not be mixed with Goal 3
feature work: they change core semantics and every corpus. Steps 5-7 can be
interleaved after their own approval, but the native backend must not depend on
an unreviewed file API; steps 5 and 7 should land before the native ELF writer
starts. Step 8 is native-only and should follow the file APIs.

## 10. Risks and objections

- **The overflow fix is a large generated-C change.** It touches every integer
  operation, the C runtime, both generators, the corpora, and the fixpoint. It
  is still the first step because the alternative is shipping known undefined
  behavior.
- **`byte` is a breaking keyword.** Existing compiler source and tests use
  `byte` as an identifier; user code may too. The migration is mechanical but
  not free.
- **Prelude growth is a compatibility surface.** Extra unions, reserved names,
  module counts, and helpers are observable in checker output and generated C.
  Per-feature fragments preserve the try-only output; a universal prelude would
  not.
- **Filesystem semantics are Linux-specific.** Error text, permissions,
  directory order, and process status are not portable; no macOS/Windows claim.
- **Process launch is security-sensitive.** `try_run` deliberately has no
  shell, no PATH search, and no environment mutation. `purr run` still needs a
  path and status policy.
- **Unsafe intrinsics create a trusted boundary.** The checker gate, module
  identity, and MIT notice must be audited; a mistake exposes raw memory to user
  code.
- **This is not the native backend.** Layout, GC, native calling convention,
  ELF writing, and float formatting remain design work. These primitives unblock
  them; they do not implement them.

## 11. Questions only the human can answer

1. Confirm that `byte` is a reserved keyword and that the compiler’s own `byte`
   identifiers may be renamed, accepting the breaking change; or choose a
   soft-keyword/`u8` alternative instead.
2. Confirm the exact literal rules: decimal capped at `INT64_MAX`, hex/binary
   up to 64 bits with high-bit patterns interpreted as negative, no `byte`
   literal.
3. Confirm division semantics: `/ 0` and `% 0` are fatal, while
   `INT64_MIN / -1` wraps and `INT64_MIN % -1` is zero. Is that the intended
   reading of “overflow wraps”?
4. Confirm whether int↔float and string↔number conversions should be added now
   or remain deferred; this proposal adds only byte and string/bytes
   conversions.
5. Confirm the file API set and names: `try_write_file`, `try_write_bytes`,
   `try_create_directory` (one level), `try_remove` (file/empty directory), and
   sorted name-only `try_list_directory`.
6. Confirm `try_run` semantics: no shell, no PATH search, inherited cwd/env/fds,
   `argv[0] = path`, normal status or `128 + signal`, failure on spawn/wait.
7. Confirm `try_current_directory` as the cwd-style name and recoverable result;
   `chdir` is not proposed.
8. Confirm the `<runtime>` module path, `int`-address intrinsic representation,
   the intrinsic list, and the MIT runtime boundary.
9. Confirm that file/directory/cwd steps may be implemented in parallel with
   Goal 3 work after approval, while the native backend waits for the core
   arithmetic/byte decisions.

## 12. Verification of this proposal

No code, tests, or `SPEC.md` were changed. The only repository change is this
file. The required checks are:

```sh
git diff --stat
gofmt -l .
git diff --check
```

`git diff --stat` must show only `proposals/native-language-primitives.md`;
the other two commands must produce no output.
