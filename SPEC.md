# Kitten Language Specification — v0.1 (Provisional)

Status: **Provisional.** This document describes the agreed direction for the
Kitten language. Syntax in this document is fixed for Phase 1. Semantics marked
as deferred are not decided and must not be assumed by any component.

This revision covers the language surface, its static semantics, and the native
compilation model. The lexer, parser, checker, C code generator, and C runtime
are implemented, and `purr build`/`purr run` produce and execute native
executables on the host platform. Self-hosting, a native backend, and the
standard library are not implemented.

---

## 1. Language goals and design philosophy

Kitten is a compiled, statically typed, garbage-collected general-purpose
programming language.

Goals:

- **Familiar.** The syntax should feel natural to developers coming from C# and
  similar curly-brace languages, with low ceremony.
- **Concise.** Prefer short, readable declarations. Do not require noise such as
  a `var` keyword for ordinary variables.
- **Explicit where it matters.** Variables and parameters carry explicit types.
  There is no type inference in the current design.
- **Practical for real software.** The long-term target is CLI tools, GUI
  applications, games, and general-purpose software.
- **Safe by default.** Memory is managed automatically; ordinary programs do not
  free memory by hand.
- **Native.** Programs are compiled ahead of time to native executables.
- **Self-hosting.** The initial compiler is written in Go, but the long-term
  goal is for Kitten to compile its own compiler. This constrains the language
  toward being expressive and complete enough to implement a compiler, and it
  is a roadmap target only: self-hosting is not implemented now and must not
  drive premature feature work.
- **Testable and extensible.** The compiler is built as cleanly separated
  phases with tests at each stage.

Design philosophy in one line: a small, readable language that compiles to fast
native code and grows a real ecosystem (standard library, package manager,
formatter, test runner, language server, third-party libraries).

---

## 2. Source file conventions

- Source files use the `.ktn` extension.
- A source file is a sequence of Unicode code points, conventionally UTF-8
  encoded on disk. No byte-order mark handling is specified in this revision.
- Statements are terminated by a semicolon `;`.
- Blocks are delimited by braces `{` and `}`.
- A program is organized as a sequence of top-level declarations: functions and
  structs. The exact set of legal top-level declarations is finalized as the
  language grows.
- Whitespace, including newlines, separates tokens and is otherwise
  insignificant. See Section 8.
- Whether a single file may contain multiple modules is **no**: one source file
  is one module. Whether a module may span several files is also **no** in this
  revision. Modules may import one another (Section 22).

---

## 3. Lexical conventions

The lexer converts source text into a stream of tokens. Each token carries:

- a **kind**, identifying its category;
- its **text**, the exact original spelling from the source (for a string
  literal this includes the quotation marks and any escape sequences);
- a one-based **line**;
- a one-based **column**, counted in Unicode code points (a tab counts as one
  column).

The lexer exposes:

```go
func Lex(source string) ([]Token, error)
```

`Lex` scans the whole input and returns every token followed by exactly one
`EOF` token. On malformed input it stops and returns the **first** lexical error
together with a `nil` token slice. Errors report a `line:column` position. The
lexer never panics on malformed or truncated input.

Token kinds are grouped as follows:

- **Special:** `EOF`.
- **Names:** `Ident`.
- **Literals:** `Int`, `Float`, `String`, plus the boolean literals `True` and
  `False`.
- **Keywords:** see Section 5.
- **Built-in type names:** see Section 5.
- **Operators and punctuation:** see Section 7.

Each keyword, type name, operator, and punctuation mark that the parser may need
to distinguish has its own distinct token kind. The parser does not have to
re-inspect raw token text to tell tokens apart. Operator scanning uses
**longest match**, so `==`, `!=`, `<=`, `>=`, `&&`, `||`, `++`, `--`, `+=`,
`-=`, and `->` are each a single token rather than two.

Recognized token kinds (implementation names):

| Category | Kinds |
| --- | --- |
| Special | `EOF` |
| Names | `Ident` |
| Literals | `Int`, `Float`, `String` |
| Boolean literals | `True`, `False` |
| Keywords | `Struct`, `If`, `Else`, `While`, `For`, `Return`, `Fn`, `New`, `Import`, `Public`, `Private`, `Enum`, `Union`, `Match`, `Map` |
| Type names | `TypeInt`, `TypeFloat`, `TypeBool`, `TypeString`, `TypeVoid` |
| Operators | `Plus`, `Minus`, `Star`, `Slash`, `Percent`, `Assign`, `Eq`, `Neq`, `Lt`, `Lte`, `Gt`, `Gte`, `And`, `Or`, `Not`, `Inc`, `Dec`, `PlusAssign`, `MinusAssign`, `Arrow` |
| Punctuation | `LParen`, `RParen`, `LBrace`, `RBrace`, `LBracket`, `RBracket`, `Comma`, `Dot`, `Semicolon`, `Colon` |

---

## 4. Identifiers and naming conventions

An identifier begins with a letter (`A`–`Z`, `a`–`z`) or an underscore `_`, and
continues with letters, digits (`0`–`9`), or underscores.

Naming conventions (conventions only; **not enforced** by the lexer or compiler):

- `snake_case` for functions and variables.
- `PascalCase` for types, including structs.

Because naming is not enforced, `MyFunction` and `my_function` are both valid
identifiers. Supporting non-ASCII letters in identifiers is **deferred**; the
current lexer accepts ASCII letters only.

`print` is the name of a built-in function, not a reserved word. It tokenizes as
an ordinary identifier (Section 13).

---

## 5. Keywords and built-in type names

The following words are reserved keywords and cannot be used as ordinary
identifiers:

```text
struct if else while for return
fn new import public private
enum union match
map
int float bool string void
true false
```

The built-in type names are:

- `int`
- `float`
- `bool`
- `string`
- `void`

`void` denotes "no value". It is permitted only as a function return type; it
is never the type of a variable, parameter, or struct field. Whether
`fn main() -> void` is accepted in addition to `fn main()` is answered in
Section 18.6: both are accepted and mean the same thing.

`true` and `false` are the boolean literal keywords.

`var` is **not** a keyword. Variables are declared with an explicit type
(Section 10).

`public` and `private` are visibility modifiers for top-level `fn` and `struct`
declarations. A declaration is visible to other modules only when it is marked
`public`; the default is `private`, so a declaration without a modifier is
visible only within its own module. Struct fields and local variables do not
take modifiers. See Section 22.

---

## 6. Literal syntax

### 6.1 Integer literals

Integer literals are decimal, hexadecimal, or binary. A leading minus sign is
not part of the literal; it is the unary minus operator. An `int` value is a
signed 64-bit integer (Section 18.1).

```text
decimal = digit { digit } ;
hex     = "0x" hex_digit { hex_digit } ;
binary  = "0b" ( "0" | "1" ) { "0" | "1" } ;
```

The prefixes are lowercase `0x` and `0b` only. Hexadecimal digits may be upper
or lower case. Underscores and octal forms are not accepted.

A decimal integer literal must not denote a value greater than `INT64_MAX`
(`9223372036854775807`). Its value is checked by the checker. A hexadecimal or
binary literal denotes a 64-bit bit pattern: values at or above
`0x8000000000000000` denote the corresponding negative two's-complement `int`,
so `0xFFFFFFFFFFFFFFFF` is `-1` and `0x8000000000000000` is `INT64_MIN`.
Hexadecimal literals have at most 16 digits after the prefix; binary literals
have at most 64 digits.

A decimal literal above `INT64_MAX`, or a hexadecimal/binary literal with too
many digits, is a checker error with the message `integer literal out of range`.
An empty digit run (`0x`, `0b`), a non-digit suffix (`0x1g`, `0b102`), an
uppercase prefix (`0X`, `0B`), or an octal-looking form (`0o17`) is a lexical
error with the message `invalid integer literal`. Both errors are positioned at
the beginning of the literal.

`INT64_MIN` cannot be written as one decimal literal because the magnitude is
not representable; it is written with arithmetic, such as
`-9223372036854775807 - 1`, or as `0x8000000000000000`.

There is no `byte` literal. A byte constant is written explicitly, for example
`byte_from_int(65)` (Section 18.8).

Not supported in this revision (and therefore not to be added without a
specification change): octal forms, digit separators, numeric suffixes, and
exponent notation.

### 6.2 Floating-point literals

A floating-point literal is a sequence of decimal digits, a decimal point, and
at least one further decimal digit, for example `12.5` and `0.25`.

A decimal point belongs to a float literal only when it is immediately followed
by a decimal digit. Consequently:

- `1.` tokenizes as an integer (`1`) followed by a dot (`.`);
- `.5` tokenizes as a dot (`.`) followed by an integer (`5`).

A `float` value is an IEEE 754 binary64 (double-precision) value
(Section 18.1). Rounding, range, and the runtime behavior of floating-point
operations are **deferred**. Literal syntax is fixed: only the forms above.

### 6.3 String literals

String literals are enclosed in double quotes, for example `"hello"`. They may
not span multiple lines; a raw newline inside a string is a lexical error.

The following escape sequences are supported:

| Escape | Meaning |
| --- | --- |
| `\n` | newline |
| `\t` | horizontal tab |
| `\"` | double quote |
| `\\` | backslash |

Any other escape sequence is a lexical error. The token text preserves the
original spelling, including the quotes and the escapes; the lexer performs no
decoding. A string literal denotes an immutable NUL-terminated byte sequence.
String representation and dynamic string operations are described in Section 20;
byte length, byte indexing, and byte ranges are implemented there.

An unterminated string, a newline inside a string, or an unsupported escape is
reported as a lexical error at a specific `line:column`. A trailing backslash
reaching end of input is reported as an unterminated string.

> Tooling note: the separate `purr.toml` subset parser uses these escapes. In
> its diagnostics, an invalid escape whose following byte is a control byte is
> echoed as `\\xNN` after the literal escape backslash, producing two
> backslashes. This is a parser-diagnostic convention, not a language rule;
> the parser tests pin it.

### 6.4 Boolean literals

`true` and `false` are the boolean literals.

---

## 7. Operators and punctuation

Operators:

```text
+ - * / % = == != < <= > >= && || ! ++ -- += -=
```

Punctuation:

```text
( ) { } [ ] , . ; : ->
```

Notes:

- `->` is used to introduce a function return type (Section 9).
- `::` is **not** an operator in Kitten.
- Square brackets `[` and `]` are used for array types and for indexing, and by
  the array creation operator `new` (Section 21). They are also reserved for a
  possible future generic parameter syntax such as `[T]`. Generics are **not**
  implemented in this revision.
- `++` and `--` are recognized in both prefix and postfix positions, and the
  parser accepts both. What they mean, and whether their operand is a valid
  target, is **deferred** to the checker.
- The precedence and associativity of operators are defined in Section 17.
- `=` is assignment or initialization; `==` is equality. `+=` and `-=` are
  compound assignments. Assignment is an expression and associates to the
  right, so `a = b = c` parses as `a = (b = c)`.

---

## 8. Comments and whitespace

- The only comment form in this revision is the `//` line comment, which runs
  from `//` to the end of the line. Block comments are **not** specified.
- Whitespace is spaces, tabs, carriage returns, and newlines. Whitespace and
  comments separate tokens and are otherwise insignificant.
- Newlines advance the line counter and reset the column counter. Comments and
  whitespace are tracked with the same position rules as all other input, so
  token positions remain accurate across multi-line files.

---

## 9. Function declaration syntax

A function is declared with `fn`, a name, a parenthesized parameter list, an
optional return type introduced by `->`, and a body block:

```text
fn main() {
    print("hello");
}

fn add(int left, int right) -> int {
    return left + right;
}
```

- Parameters are written `type name`, separated by commas.
- The return type follows the parameter list after `->`.
- A function without a return type annotation returns no value; write the bare
  form `fn main()` and omit the arrow entirely.
- A return type may be written after `->`. Writing `-> void` is accepted and is
  exactly equivalent to omitting the return type: the function returns no value
  (Section 18.6).
- `return value;` returns a value; a bare `return;` returns no value. A bare
  `return;` is valid only in a function that returns no value, and
  `return value;` requires an exact type match with the declared return type
  (Section 18.6).
- Visibility modifiers `public` and `private` may appear before a top-level `fn`
  or `struct`. `public` makes the declaration visible to other modules; the
  default is private, so an unmarked declaration is visible only within its own
  module (Section 22). Modifiers are not accepted on struct fields or local
  variables.
- Function overloading, method semantics, default arguments, and variadic
  parameters are **deferred**.

---

## 10. Variable declaration syntax

Variables are declared with an explicit type, a name, an initializer, and a
semicolon:

```text
int score = 5;
float temperature = 21.5;
bool is_ready = true;
string username = "kitten";
```

- **Types are always written explicitly.** There is no type inference, and
  `var` is not part of the language.
- **Variables are mutable by default.** This revision introduces no immutability
  modifier such as `const`, `let`, or `final`.
- The initializer is **optional** in the language: both `int x;` and
  `int x = 1;` are valid (Section 17 and Section 18.4). A declaration without
  an initializer leaves the variable uninitialized; reading it before assigning
  is not diagnosed in this revision (Section 18.10).
- The scope and lifetime rules for variables are defined in Section 18.5.
- Local variables do not take visibility modifiers.

---

## 11. Struct declaration and literal syntax

A struct declaration names a type and lists fields, each `type name;`:

```text
struct Rectangle {
    int width;
    int height;
}
```

A struct literal names the type and assigns fields by name inside braces:

```text
Rectangle room = Rectangle {
    width = 4,
    height = 5
};
```

**Reference semantics (provisional).** A struct value is a reference to a
garbage-collected object, not an inline copy of its fields. Evaluating a struct
literal allocates an object. Assigning one struct variable to another copies the
reference, so both names refer to the same object, and a field mutated through
either name is visible through the other. Nested struct fields use the same
model, and passing or returning a struct passes or returns the reference. See
Section 19 for the full model and its consequences.

- The lexer recognizes this syntax but does not validate field names, field
  order, or struct semantics. The checker validates all of these
  (Section 18.7): field types must exist and not be `void`, field names must be
  unique, and a struct literal must name a struct and initialize every field
  exactly once with a value of the declared type.
- A struct literal is recognized whenever an identifier is immediately followed
  by `{` in an expression position (Section 17). This means a missing `)` before
  a block, as in `if (x {`, is interpreted as a struct literal named `x` and
  reported as a different error than a missing parenthesis would be.
- Struct fields take no initializer in this revision: they are written
  `type name;` (Section 17). Whether fields may be initialized at the
  declaration is **deferred**.
- Every field must be supplied in a struct literal. Omitting a field is an
  error; whether fields may be omitted later is **deferred**.
- Trailing commas are **not** accepted in struct literals in this revision
  (Section 17).
- Field visibility and the default visibility of struct members are
  **deferred**. All fields are mutable and writable in this revision
  (Section 18.7).

---

## 12. Control flow

Kitten supports `if`/`else if`/`else`, `while`, and C-style `for`:

```text
if (condition) {
    // ...
} else if (other_condition) {
    // ...
} else {
    // ...
}

while (condition) {
    // ...
}

for (int index = 0; index < 10; index++) {
    // ...
}
```

- Conditions are always parenthesized.
- Bodies are brace-delimited blocks.
- The `for` header is `(init; condition; increment)`, where `init` may declare a
  variable or be any expression. The example uses the postfix increment
  `index++`. Each of the three clauses may be empty, as in `for (;;)` or
  `for (int i = 0; ; )` (Section 17).
- Whether `break` and `continue` exist, and any loop `else` clauses, are
  **deferred**. They are not keywords in this revision.

---

## 13. Built-in `print(...)` direction

`print` is a built-in function intended to write its arguments to standard
output, one line per call, so that a later `print` appears on the following
line:

```text
print("hello, kitten");
print("5! =", factorial(5));
```

- `print` is **not** a reserved keyword; it tokenizes as an ordinary identifier
  (`Ident`). Reserving it is unnecessary because a program may name other things
  `print`-adjacent, and the parser can resolve it by name.
- `print` accepts one or more arguments. The checker models it as a built-in
  function that accepts one or more arguments, each of type `int`, `float`,
  `bool`, or `string`. Calling it with no arguments, or with a struct value, is
  a semantic error (Section 18.8). A user-defined `print` shadows the built-in.
- **Output format.** Each call writes its arguments separated by a single
  space, then a newline. An `int` is written in decimal; a `float` is written
  with C's `%g` conversion (six significant digits by default), so `2.5` prints
  as `2.5` and `4.0` prints as `4`; a `bool` is written as `true` or `false`; a
  `string` is written as its bytes. This fixes the behavior the `.ktn` fixtures
  depend on. The `float` and `bool` renderings are provisional (no fixture
  covers them) and are recorded in Section 19.

---

## 14. Memory-management direction

- Kitten is garbage-collected. Ordinary programs allocate objects and strings
  and never free them manually.
- The initial collector is the **Boehm–Demers–Weiser conservative garbage
  collector** (Boehm GC, `libgc`), linked into compiled programs. Struct
  literals are allocated with the collector (Section 19).
- The compiler generates C, which a system C compiler then compiles and links
  against Boehm GC to produce a native executable.
- The precise collector configuration, finalization behavior, and any
  distinction between stack, heap, and static storage are **deferred**
  (Section 16). There is no finalizer support and no way to trigger collection
  from Kitten code.

---

## 15. Compiler architecture

The compiler is written in Go and is organized into clearly separated packages,
one per phase:

| Package | Responsibility | Status |
| --- | --- | --- |
| `lexer` | Source text → tokens | Implemented (Phase 1) |
| `parser` | Tokens → syntax tree | Implemented (Phase 2A) |
| `loader` | Source files → parsed modules, resolving imports | Implemented (Phase 3C) |
| `checker` | Semantic analysis and type checking | Implemented (Phase 2B) |
| `codegen` | Checked modules → C source | Implemented (Phase 2C) |
| `runtime` | C runtime: GC initialization, allocation, strings, arrays, file reads, print | Implemented (Phase 2C) |

Supporting layout:

- `cmd/purr` — the `purr` command-line entry point. It runs the full
  pipeline and invokes the system C compiler and linker.
- `tests/` — `.ktn` programs and matching `.expected` output files. They are
  compiled and executed by the end-to-end tests.

Pipeline: root source → loader (file access, parsing, import resolution) →
checker (all modules) → C code generator → C compiler/linker → link with Boehm
GC → native executable. All stages are implemented for the host platform. See
Section 19 for the compilation model and Section 22 for modules.

Phase boundaries are strict: a package must not take on another phase's
responsibility, and later phases are not started without explicit authorization.

---

## 16. Deferred decisions and unresolved semantics

This revision deliberately leaves the following undecided. None of them may be
invented silently in code. When a decision is needed, raise it and record the
outcome here.

**Types and numbers**

- Exact bit widths of `int` and `float` are now provisionally fixed as signed
  64-bit and IEEE 754 binary64 (Section 18.1). Integer arithmetic is defined by
  two's-complement wraparound (Section 18.3); runtime floating-point rounding
  remains **deferred**.
- Numeric conversions and promotion rules: **no implicit conversions** exist
  (Section 18.2). Explicit conversion syntax is **deferred**; there are no casts
  yet.
- Whether additional numeric types beyond `byte` exist.

**Strings**

- In-memory representation, mutability, indexing, and slicing. In this revision
  a string is an immutable, length-carrying byte sequence represented as a
  pointer to the runtime's `ktn_string`, whose header and bytes are one object
  for constructed strings and static storage for literals (Sections 19 and 20).
  Byte indexing, byte length, and byte ranges (`substring`) are implemented, and
  length and indexing are constant-time; strings remain immutable.
- Runtime operations (length, concatenation, comparison). Concatenation (`+`,
  `+=`), value equality (`==`, `!=`), byte length, byte indexing, and byte
  ranges are implemented (Section 20).
- String/bytes conversions are `string_to_bytes` and `bytes_to_string`
  (Section 18.8). Numeric conversions and code-point operations remain
  **deferred** (Section 20.7).
- Whether string interpolation or raw/multiline strings exist.
- Whether the representation should also support embedded NUL bytes. It does
  not: the bytes stay NUL-terminated so they remain usable with C library
  functions, and no operation can create one (Section 20.1).

**Visibility and declarations**

- Default visibility for top-level declarations is **private** (module-local);
  `public` makes a declaration visible to other modules (Section 22). Struct
  members do not take visibility modifiers and remain visible wherever the
  struct type is.
- Exactly which declarations accept `public` and `private`, and where the
  modifier is written: top-level `fn` and `struct` only.
- Whether `fn main() -> void` is accepted in addition to `fn main()`: **yes**,
  both are accepted and equivalent (Section 18.6). The entry point must be in
  the root module (Section 22).
- Whether a variable initializer is mandatory: **it is optional**
  (Section 18.4). Reading an uninitialized variable is not diagnosed.
- Whether reading an uninitialized variable should be an error.

**Functions and types**

- Function overloading and method semantics.
- Default and variadic parameters.
- Generics and other advanced type-system features. Square brackets `[` `]` are
  used for arrays (Section 21) and map types (Section 24), and remain reserved
  for a possible future `[T]` generic syntax. Maps are not generic: keys are
  always `string`.
- Whether `::` or any namespacing operator is ever introduced (currently it is
  not an operator).

**Statements and expressions**

- Operator precedence and associativity, now provisionally defined in
  Section 17. A future revision may change it, which would be a breaking
  change.
- Whether `break` and `continue` exist. They are not keywords in this revision.
- Whether a bare `return;` is allowed and how it interacts with return types: a
  bare `return;` is valid only in a function that returns no value, and
  `return value;` must match the declared return type (Section 18.6).
- Whether return-path analysis should account for loops and other constructs
  that the conservative analysis currently ignores (Section 18.6).
- The full set of statement forms. In this revision `match` is a statement, not
  an expression, and a match must be exhaustive with no wildcard arm; whether
  either should change is **deferred**.

**Modules and program structure**

- Imports and modules: **implemented** (Section 22). A source file is one
  module; modules import one another by relative path.
- Entry-point requirements and program startup. The native build requires a
  zero-parameter, void-returning `main` declared in the root module
  (Sections 19 and 22); the language itself has no implicit `main`, and `main`
  is an ordinary function to the checker.
- Qualified names, re-exporting, and non-transitive imports are **deferred**.

**Errors, concurrency, and runtime**

- Error handling.
- Reading a field of a struct reference that is null, which is what an
  unassigned struct array element or struct field holds. Arrays, maps, and
  `match` on a union all report null references as fatal runtime errors, but
  struct field access does not check: it dereferences the reference, so
  `new Label[1][0].text` faults instead of reporting an error. Making struct
  field access reject a null reference is **deferred**; the current behavior is
  a pre-existing limitation, not a property of the string representation.
- Concurrency.
- The precise behavior of the garbage collector and runtime, and the concrete
  Boehm GC configuration.
**Parser clarifications deferred**

- Whether an empty `enum` body (`enum E {}`, no cases) and an empty `union` body
  (`union U {}`, no variants) are legal. The reference parser accepts both, while
  Section 23.1 and Section 23.2 require at least one case or variant, so the
  grammar and the parser disagree (Section 17.4). The discrepancy is documented
  and neither implementation was changed.

**Lexical clarifications deferred**

- Whether identifiers may contain non-ASCII letters. The current lexer accepts
  ASCII letters only.
- Handling of a byte-order mark at the start of a file.
- Whether block comments will ever be added.

**Implementation status**

- The parser is implemented (Phase 2A) and accepts the grammar in Section 17.
- The checker is implemented (Phase 2B) and enforces the rules in Section 18.
- The C code generator and C runtime are implemented (Phase 2C), and
  `purr build` and `purr run` compile to native executables on the host
  platform.
- The `.ktn` fixtures lex, parse, check, compile, and execute; the end-to-end
  tests compare their output with the matching `.expected` files byte-for-byte.
- A lexer written in Kitten itself exists under `selfhost/lexer` and is compared
  against the Go reference lexer by a differential test, but it is not part of
  the production pipeline (Section 25).
- A parser written in Kitten itself exists under `selfhost/parser`, with its
  driver in `selfhost/parse.ktn`, and is compared against the Go reference parser
  by a differential test. It also is not part of the production pipeline
  (Section 26).
- A semantic checker written in Kitten itself exists under `selfhost/checker`,
  with its driver in `selfhost/check.ktn`, and is compared against the Go
  reference checker by a differential test. It also is not part of the production
  pipeline (Section 27).
- Self-hosting, a native (LLVM or assembly) backend, and the standard library
  are not implemented (Section 19.9).

---

## 17. Grammar and operator precedence (provisional)

This section records the grammar the parser accepts and the operator
precedence it applies. It settles only the decisions needed to parse the
currently supported language. Everything here is **provisional** and may change
in a later revision; a change to precedence is a breaking change.

### 17.1 Notation

The grammar is written in a simplified EBNF: `{ x }` means zero or more
repetitions of `x`, `[ x ]` means optional `x`, and `|` separates
alternatives. Terminals are quoted; `IDENT`, `INT`, `FLOAT`, and `STRING` are
the lexer's identifier and literal tokens (Section 3). `EOF` is the end of
input.

### 17.2 Grammar

```text
program          = { declaration } EOF ;

declaration      = [ visibility ] ( function | struct | enum | union ) | import ;
visibility       = "public" | "private" ;
import           = "import" STRING ";" ;

function         = "fn" IDENT "(" [ parameters ] ")" [ "->" type ] block ;
parameters       = parameter { "," parameter } ;
parameter        = type IDENT ;

struct           = "struct" IDENT "{" { field } "}" ;
field            = type IDENT ";" ;

enum             = "enum" IDENT "{" case { "," case } "}" ;
case             = IDENT ;
union            = "union" IDENT "{" { variant } "}" ;
variant          = IDENT [ "{" { field } "}" ] ;

type             = map_type | base_type { "[]" } ;
base_type        = "int" | "float" | "bool" | "string" | "void" | IDENT ;
map_type         = "map" "[" type "]" type ;

block            = "{" { statement } "}" ;

statement        = block
                 | if
                 | while
                 | for
                 | return
                 | match
                 | variable_declaration
                 | expression_statement ;

if               = "if" "(" expression ")" block [ "else" ( if | block ) ] ;
while            = "while" "(" expression ")" block ;
for              = "for" "(" [ for_init ] ";" [ expression ] ";" [ expression ] ")" block ;
for_init         = variable_declaration | expression ;
return           = "return" [ expression ] ";" ;
match            = "match" "(" expression ")" "{" { arm } "}" ;
arm              = IDENT [ "(" [ IDENT { "," IDENT } ] ")" ] block ;

variable_declaration = type IDENT [ "=" expression ] ";" ;
expression_statement = expression ";" ;

expression       = assignment ;
assignment       = bitwise_or { ( "=" | "+=" | "-=" ) bitwise_or } ;   // right-associative
bitwise_or       = bitwise_xor { "|" bitwise_xor } ;
bitwise_xor      = bitwise_and { "^" bitwise_and } ;
bitwise_and      = equality { "&" equality } ;
equality         = relational { ( "==" | "!=" ) relational } ;
relational       = shift { ( "<" | "<=" | ">" | ">=" ) shift } ;
shift            = additive { ( "<<" | ">>" ) additive } ;
additive         = multiplicative { ( "+" | "-" ) multiplicative } ;
multiplicative   = unary { ( "*" | "/" | "%" ) unary } ;
unary            = ( "-" | "~" | "!" | "++" | "--" ) unary | postfix ;
postfix          = primary { call | member | index | "++" | "--" } ;
call             = "(" [ arguments ] ")" ;
arguments        = expression { "," expression } ;
member           = "." IDENT ;
index            = "[" expression "]" ;
primary          = INT | FLOAT | STRING | "true" | "false"
                 | IDENT
                 | struct_literal
                 | variant_literal
                 | array_creation
                 | map_creation
                 | "(" expression ")" ;
array_creation   = "new" type "[" expression "]" ;
map_creation     = "new" "map" "[" type "]" type ;
struct_literal   = IDENT "{" [ field_inits ] "}" ;
variant_literal  = IDENT "." IDENT "{" [ field_inits ] "}" ;
field_inits      = field_init { "," field_init } ;
field_init       = IDENT "=" expression ;
```

Notes on the grammar:

- A `variable_declaration` begins with a type name. At statement level the
  parser treats an identifier optionally followed by `[]` array suffixes and
  then another identifier (`Rectangle room ...`, `Rectangle[] rooms ...`) as the
  start of a declaration. An identifier followed by `[expression]` is an
  indexing expression statement, so `rooms[0] = ...;` is an assignment rather
  than a declaration.
- Array creation uses the `new` keyword so that `new T[n]` is never confused
  with indexing an identifier (`x[n]`). The element type may itself be an array
  type written with `[]`, so `new int[][3]` creates an array of three `int[]`
  elements. Only the outermost bracket pair carries a length.
- A `struct_literal` is recognized when an identifier is immediately followed
  by `{` in an expression position. A `variant_literal` is recognized when an
  identifier is followed by `.`, another identifier, and then `{`. Neither form
  conflicts with anything else in expression position, so an expression such as
  `a.b` is member access while `A.b { ... }` constructs a union variant.
- `Type.Variant` without braces is an ordinary member access to the parser; the
  checker reinterprets it as a variant reference when `Type` names an enum or
  union (Section 23).
- Parentheses group expressions but produce no distinct syntax-tree node.
- The assignment operators are the only right-associative operators. All other
  binary operators are left-associative.
- `:` has **no syntactic role** in this grammar. Encountering it is a syntax
  error. `[` and `]` are used by array types, indexing, and array creation
  (Section 21).

### 17.3 Precedence and associativity

From lowest binding to highest:

| Level | Operators | Associativity |
| --- | --- | --- |
| 1 (lowest) | `=` `+=` `-=` | right |
| 2 | logical OR (&#124;&#124;) | left |
| 3 | `&&` | left |
| 4 | `==` `!=` | left |
| 5 | `<` `<=` `>` `>=` | left |
| 6 | `+` `-` | left |
| 7 | `*` `/` `%` | left |
| 8 | prefix `-` `!` `++` `--` | right (unary) |
| 9 (highest) | postfix `()` `[]` `.` `++` `--` | left |

### 17.4 Provisional decisions recorded

These decisions were needed to parse the language. Each is provisional:

1. A conventional C-like precedence table is used (Section 17.3), with
   assignment lowest and right-associative.
2. Both prefix and postfix `++`/`--` are accepted as expressions. Their meaning
   and operand validity are deferred to the checker.
3. An optional `public`/`private` modifier is accepted on top-level `fn` and
   `struct` declarations, and nowhere else.
4. A local variable initializer is optional (`int x;` and `int x = 1;`).
5. Struct fields are declared `type name;` with no initializer.
6. Trailing commas are **not** accepted in argument lists, parameter lists, or
   struct literals.
7. A call or struct literal with no arguments or fields (`f()`, `Point {}`) is
   accepted.
8. All three `for` header clauses may be empty.
9. Assignment is an expression with no syntactic restriction on its target;
   whether the target is assignable is deferred to the checker.
10. The parser is fail-fast: it reports the first syntax error and stops.

**Observed inconsistencies between the grammar and the reference parser**

The reference parser accepts two bodies the grammar above does not, and the
reference checker then rejects both:

- `enum E {}` is accepted with no cases, although `enum` requires at least one
  `case` and Section 23.1 states that an enum must have at least one case. The
  reference checker reports `enum "E" must have at least one case` at the
  declaration, so the rule is enforced one phase later than the grammar places it.
- `union U {}` is accepted with no variants, although Section 23.2 states that a
  union must have at least one variant; the grammar above does allow an empty
  union body (`{ variant }`), so only the prose in Section 23.2 disagrees with the
  grammar. The reference checker reports `union "U" must have at least one
  variant`, enforcing the prose.

`struct S {}` with no fields is accepted by both and is consistent with the
grammar and Section 11.

Neither discrepancy changes which programs are accepted: a program with an empty
enum or union body is rejected, by the checker rather than by the parser. The
difference is only *where* it is reported, and it is **documented, not resolved**:
moving the rule into the grammar would change the parser's accepted language, so
the choice is deferred (Section 16). The Kitten-written parser and checker follow
the reference in both places, and the differential corpora include both empty
bodies.

---

## 18. Semantic analysis and type rules (provisional)

This section records the semantic rules the checker enforces. It is
**provisional**: the rules are sufficient to check the currently supported
language and may change in a later revision. The checker is fail-fast: it
reports the first error it finds and stops. Every diagnostic carries a one-based
`line:column` position and a category.

### 18.1 Primitive types

| Name | Meaning |
| --- | --- |
| `int` | Signed 64-bit integer. |
| `float` | IEEE 754 binary64 floating-point value. |
| `bool` | Boolean. |
| `string` | String value. |
| `byte` | Unsigned 8-bit integer, 0 through 255. |
| `void` | No value. Permitted only as a function return type. |

`int` and `float` widths are provisional language semantics, not a commitment
about the runtime representation. `int` uses two's-complement 64-bit arithmetic,
defined in Section 18.3; floating-point rounding remains **deferred**. `void` is
never the type of a variable, parameter, or struct field; using it there is an
error.

### 18.2 Type compatibility

Types are compared by **exact matching**, with no implicit conversions. An `int`
is not interchangeable with a `float` or a `byte`, and mixing them in one
operation is an error. There are no casts and no generic or parameterized types;
explicit conversions are the four built-ins in Section 18.8.

A value of type `void` may not be used as a value: it may not be assigned,
returned, passed as an argument, used as a condition or operand, or used to
initialize a variable or field.

### 18.3 Operator rules

| Operators | Operand types | Result |
| --- | --- | --- |
| `+` | two `int`, two `float`, or two `string` | the operand type (`string` concatenates) |
| `-` `*` `/` | two `int`, or two `float` | the operand type |
| `%` | two `int` | `int` |
| `&` `\|` `^` | two `int` | `int` |
| `<<` `>>` | two `int` | `int` |
| `<` `<=` `>` `>=` | two `int`, or two `float` | `bool` |
| unary `~` | one `int` | `int` |
| `==` `!=` | two of the same primitive type | `bool` |
| `<` `<=` `>` `>=` | two `byte` | `bool` |
| `&&` `\|\|` | two `bool` | `bool` |
| unary `-` | one `int` or `float` | the operand type |
| unary `!` | one `bool` | `bool` |

Notes:

- Both operands of a binary operation must have the same type. Mixing `int` and
  `float`, or a `string` with a non-`string`, is an error, not a conversion.
- `+` on two `string` values concatenates them (Section 20). `+` on two `bool`,
  two struct, or two array values is an error.
- `-`, `*`, `/` are not defined for `string`.
- `%` is not defined for `float`.
- Relational operators are numeric-only, except that two `byte` values support
  unsigned `<`, `<=`, `>`, and `>=`; comparing `bool`, `string`, or mixed
  `byte`/`int` values with `<` is an error.
- Equality is defined for the primitive types and enums. Comparing two struct
  values, two array values, two union values, or two map values with `==` is an
  error. Comparing two `string` values compares their contents, never their
  addresses, and comparing two enum values compares their cases (Section 23).
- There is no unary `+` operator: the grammar in Section 17 does not include it.
- `&&` and `||` **short-circuit**: the right operand is evaluated only when the
  left operand does not determine the result, and the right operand is always
  type-checked. This is what makes the common bound-check idiom
  `position < size && text[position] != 32` safe, since the indexed access is
  not evaluated once `position < size` is false.
- `&`, `|`, and `^` are bitwise operations on the 64-bit two's-complement
  pattern. `~` is the bitwise complement. `<<` shifts left, discarding bits
  shifted out; `>>` is an arithmetic right shift, copying the sign bit into
  vacated high bits. The shift count must be an `int` in `0..63`; any other
  count is a fatal runtime error with the message
  `shift count <n> is outside 0..63`, where `<n>` is signed decimal. This check
  is performed at runtime even for a constant count.
- These operators bind as the C#-style precedence table in Section 17.2:
  `a & b == c` parses as `a & (b == c)` and is a type error; that consequence is
  intentional.
- Indexing is defined for arrays, maps, and strings. A string index is an `int`
  (a byte), not a one-character `string`; see Section 20.6.
- `int` arithmetic is defined by two's-complement 64-bit wraparound. The
  operations `+`, `-`, `*`, unary `-`, `+=`, `-=`, `++`, and `--` produce the
  unique result modulo 2^64 interpreted as a signed two's-complement 64-bit
  integer; overflow wraps and is not a runtime failure.
- The bitwise operators `&`, `|`, `^`, `~`, `<<`, and `>>` are defined for
  `int` only. Shifts outside `0..63` fail at runtime rather than relying on C
  behavior; `>>` is arithmetic for negative values.
- `byte` supports equality and unsigned relational comparison only. It has no
  arithmetic, bitwise, shift, unary `-`, `~`, `++`, `--`, `+=`, or `-=`
  operators. Convert explicitly with `int_from_byte` and `byte_from_int`.
- `/` truncates toward zero. `%` is the remainder with the sign of the dividend.
  A zero divisor is a fatal runtime error with the message
  `integer division by zero`. `INT64_MIN / -1` is `INT64_MIN` and
  `INT64_MIN % -1` is `0`; neither is undefined.

### 18.4 Variables and assignment

- A variable declaration names a type and a name, with an optional initializer.
  The declared type must exist and must not be `void`.
- If an initializer is present, its type must exactly match the declared type.
- Assignment (`=`) requires the value type to match the target type exactly.
- The assignment target must be a mutable l-value: a local variable, a
  parameter, a struct field, or an array element, each reached through an
  l-value. Assigning to a literal, a call, or any other expression is an error.
- Compound assignment (`+=`) requires both operands to be the same numeric type,
  or two `string` values, in which case it appends. `-=` requires two operands
  of the same numeric type. The result has the target's type.
- Prefix and postfix `++`/`--` require a mutable numeric l-value (`int` or
  `float`) and produce the operand's type. They are not defined for `string`,
  `bool`, or struct values.
- An assignment expression's value has the target's type, so a chain such as
  `a = b = c` type-checks when the types agree.

### 18.5 Scopes and names

- Top-level functions and structs share **one** namespace. Declaring two
  top-level names that are equal is a duplicate-declaration error, whether they
  are two functions, two structs, or one of each.
- A function's parameters and the function body's outermost statements share one
  scope. Declaring a local with the same name as a parameter is an error.
- Each nested block introduces a new scope. An inner scope may shadow names from
  an outer scope, including a parameter or an outer local.
- Declaring the same name twice in one scope is a duplicate-declaration error.
- A local variable is not visible before its declaration, including in its own
  initializer, so `int x = x;` is an undefined-name error.
- A `for` loop introduces its own scope containing the initializer, so an
  initializer variable is visible in the condition, the increment, and the body,
  but not after the loop.
- `print` is provided by the language on its own (Section 18.8). A user-defined
  top-level `print` shadows the built-in and is not a duplicate declaration.
- Name resolution inside a function searches local scopes first, then top-level
  functions, then built-ins.

### 18.6 Functions and returns

- Every argument in a call must match the corresponding parameter type exactly,
  and the number of arguments must match the number of parameters.
- The callee must be a plain identifier naming a user function or a built-in.
  Calling a variable, a type name, a member, or any other expression is an
  error.
- A function whose return type is `void` may not return a value: only a bare
  `return;` (or falling off the end) is allowed.
- A function whose return type is not `void` must return a value of exactly that
  type. A bare `return;` in such a function is an error, and a returned value of
  the wrong type is an error.
- Writing `-> void` is exactly equivalent to omitting the return type.
- Return-path analysis is conservative. A non-void function is accepted if its
  body **definitely returns**, defined as:
  - a `return value;` statement definitely returns;
  - a block definitely returns if any of its statements definitely returns;
  - an `if` statement definitely returns only when it has an `else` branch and
    both the `then` and `else` branches definitely return.
  - `while` and `for` loops never definitely return, even with a constant
    condition, and expression statements never definitely return.
  A non-void function that does not definitely return is an error. Unreachable
  code is not diagnosed, and loops are not analyzed for termination; a function
  can satisfy this rule with a return in a loop that is provably reached.
- There is no implicit `main`. `main` is an ordinary function; entry-point
  requirements and startup behavior are **deferred** to the compilation phase.

### 18.7 Structs

- A struct declaration must reference field types that exist, and a field type
  may not be `void`. Duplicate field names are an error.
- A struct may contain fields of struct type, including another struct declared
  later in the file.
- A struct literal must name a struct type. Every supplied field must exist in
  that struct, must be initialized at most once, and its value must exactly
  match the field's declared type.
- Every field of the struct must be initialized in a struct literal. Omitting a
  field is an error.
- Member access requires the receiver to be a struct value; accessing a member
  of a primitive or of a `void` value is an error. The member must be a field of
  that struct, and the member expression has the field's type.
- Struct fields are mutable; a field reached through an l-value is assignable.
- Methods, inheritance, and implicit constructors are **not** implemented.
  Whether a struct may contain itself, directly or indirectly, is **deferred**.

### 18.8 Built-in functions

Twenty-four functions are provided by the language: `print`, `eprint`, `exit`,
`length`, `argv`, `has`, `substring`, `read_file`, `try_read_file`,
`try_write_file`, `try_write_bytes`, `try_create_directory`, `try_remove`,
`try_list_directory`, `try_run`, `try_run_capture`, `try_current_directory`,
`try_getenv`, `try_file_info`, `try_create_temp_directory`, `int_from_byte`,
`byte_from_int`, `string_to_bytes`, and `bytes_to_string`. All are represented in the semantic environment rather than
special-cased in expression checking. A user-defined top-level declaration with
the same name shadows the built-in.

**`print`**

- It accepts **one or more** arguments, and every argument must be `int`,
  `float`, `bool`, `string`, or `byte`. A `byte` prints its unsigned decimal
  value. Calling it with no arguments, or with a struct, array, enum, union, or
  map value, is an error.
- Its return type is `void`, so a call to `print` cannot be used as a value.
- The output format is defined in Section 13 and the runtime in Section 19.4.

**`eprint`**

- It accepts **one or more** arguments, with exactly the same allowed types,
  separators, formatting, and trailing-newline rule as `print`: every argument
  must be `int`, `float`, `bool`, `string`, or `byte`, values are separated by
  single spaces, and the line ends with a newline.
- It writes to **standard error**, not standard output. Diagnostics therefore
  never share a stream with generated program output or generated C.
- Its return type is `void`, so a call to `eprint` cannot be used as a value.
- Calling it with no arguments, or with a struct, array, enum, union, or map
  value, is an error with the same rule and wording as `print`.
- **Flushing and ordering.** Each `eprint` line flushes standard output before
  writing its terminating newline, then writes the newline and flushes standard
  error. This makes output written by earlier `print` calls appear before the
  diagnostic when both streams share a terminal. `print` itself remains buffered
  and is flushed at normal process exit, when its buffer fills, or before a fatal
  runtime error. The streams are independent: standard output carries only
  `print` output and standard error carries only `eprint` output.

**`exit`**

- `exit(status)` takes exactly one `int` and terminates the process with that
  status. It never returns and has type `void`, so it cannot be used as a value.
  Calling it with no arguments, more than one argument, or a non-`int` argument
  is an error.
- Before terminating, it flushes standard output first and then standard error,
  so output produced before the exit is not lost and appears in that order. No
  other flushing or cleanup is promised.
- The status is passed to the host without validation or conversion. On POSIX
  systems only the low eight bits are observable, so `exit(256)` is observed as
  status 0 and `exit(-1)` as status 255. Other hosts may interpret the status
  differently.
- `exit` **does not change control-flow analysis**. It is an ordinary expression
  statement to the checker, not a terminating statement: a function with a
  non-void return type still needs a `return` on every path the conservative
  analysis accepts, even if an `exit` call precedes that path.
- The compiler driver conventionally uses status 0 for success, 1 for a
  compilation or execution failure, and 2 for a usage error. The language does
  not enforce those values.

**`length`**

- `length(collection)` takes exactly one argument, which must be an array, a
  map, or a string, and returns its number of elements or bytes as an `int`
  (Sections 20.6, 21, and 24).
- Calling it with no arguments, more than one argument, or an argument that is
  not a collection is an error.
- Its return type is `int`, and it is not variadic.

**`argv`**

- `argv()` takes no arguments and returns the process argument vector as a
  `string[]`. Index 0 is the program name as the host reported it, followed by
  the program arguments in order. The program name is not necessarily an absolute
  path. No arguments beyond the program name adds no elements.
- Every call returns a fresh array. Mutating that array, including assigning to
  its elements, does not change the process arguments or the result of a later
  `argv()` call.
- Calling it with any argument is an error.
- Its return type is `string[]`, and it is not variadic.

**`has`**

- `has(map, key)` takes exactly two arguments: a map and a key of the map's key
  type. It returns `true` when the key is present and `false` otherwise
  (Section 24).
- Passing a non-map first argument, a key of the wrong type, or the wrong number
  of arguments is an error.
- Its return type is `bool`.

**`substring`**

- `substring(text, start, end)` takes a `string` and two `int` byte offsets and
  returns a `string` holding the bytes in `[start, end)` (Section 20.6).
- Passing a non-`string` first argument, a non-`int` offset, or the wrong number
  of arguments is an error.
- A range that does not satisfy `0 <= start <= end <= byte length` is a fatal
  runtime error.
- Its return type is `string`.

**`read_file`**

- `read_file(path)` takes exactly one `string` argument and returns a `string`
  holding the entire contents of the file at that path (Section 22.2).
- Calling it with the wrong number of arguments or a non-`string` argument is an
  error.
- A failed read is a fatal runtime error, not a recoverable result; see
  Section 22.2.

**`try_read_file`**

- The language provides a predeclared public union:

  ```kitten
  union ReadFileResult {
      Ok { string text; }
      Failed { string message; }
  }
  ```

- `try_read_file(path)` takes exactly one `string` and returns a
  `ReadFileResult`. On success it returns `Ok` with the complete file contents;
  an empty file is `Ok { text = "" }`. On failure it returns `Failed` with the
  Go-compatible operating-system error text, without the loader's
  `cannot read source file: ` prefix. For example, a missing file is
  `Failed { message = "open /x.ktn: no such file or directory" }`.
- The type is visible from every module without an import. It is introduced
  implicitly when a module contains a syntactic `try_read_file` call. The name
  `ReadFileResult` is reserved: any user top-level declaration with that name is
  an error, whether or not the implicit type is introduced.
- `try_read_file` remains shadowable like the other built-ins; a user-defined
  function with that name takes precedence.
- Users may construct `ReadFileResult.Ok { text = ... }` and
  `ReadFileResult.Failed { message = ... }` directly; the type behaves as an
  ordinary union.
- `try_read_file` uses the same 256 MiB (`KTN_RT_MAX_FILE_SIZE`) limit as
  `read_file`. A larger file is a `Failed` result, not a fatal read. The same
  NUL-byte limitation as `read_file` applies.
- Calling it with the wrong number of arguments or a non-`string` argument is an
  error.
- Its return type is `ReadFileResult`, and it is not variadic.

**Predeclared result types and prelude injection**

- The language also provides the following predeclared public unions:

  ```kitten
  union FileResult {
      Ok
      Failed { string message; }
  }

  union ListDirectoryResult {
      Ok { string[] entries; }
      Failed { string message; }
  }

  union RunResult {
      Ok { int status; }
      Failed { string message; }
  }

  union CurrentDirectoryResult {
      Ok { string path; }
      Failed { string message; }
  }

  union EnvResult {
      Set { string value; }
      Unset
  }

  union CapturedRunResult {
      Ok { int status; string stdout; string stderr; }
      Failed { string message; }
  }

  union FileInfoResult {
      Ok { bool is_file; bool is_directory; bool is_symlink; bool executable; int size; }
      Missing
      Failed { string message; }
  }

  union TempDirectoryResult {
      Ok { string path; }
      Failed { string message; }
  }
  ```

- `FileResult.Ok` carries no payload; it is written without braces, as
  `FileResult.Ok`. `FileResult.Failed { message = ... }`,
  `ListDirectoryResult.Ok { entries = ... }`, `ListDirectoryResult.Failed`,
  `RunResult.Ok { status = ... }`, `RunResult.Failed { message = ... }`,
  `CurrentDirectoryResult.Ok { path = ... }`,
  `CurrentDirectoryResult.Failed { message = ... }`,
  `EnvResult.Set { value = ... }`, `EnvResult.Unset`,
  `CapturedRunResult.Ok { ... }`, `CapturedRunResult.Failed { ... }`,
  `FileInfoResult.Ok { ... }`, `FileInfoResult.Missing`,
  `FileInfoResult.Failed { ... }`, `TempDirectoryResult.Ok { ... }`, and
  `TempDirectoryResult.Failed { ... }` are ordinary variant constructions.
- The nine predeclared union names `ReadFileResult`, `FileResult`,
  `ListDirectoryResult`, `RunResult`, `CurrentDirectoryResult`, `EnvResult`,
  `CapturedRunResult`, `FileInfoResult`, and `TempDirectoryResult` are reserved
  in **every** program: any user top-level declaration with one of those names
  is an error, whether or not the corresponding implicit type is introduced.
- The prelude built-ins use one synthetic prelude module, appended
  after every loaded module with `id = module count` and path `<prelude>`. The
  module contains only the result unions required by the program's syntactic
  calls, in the fixed order `ReadFileResult`, `FileResult`,
  `ListDirectoryResult`, `RunResult`, `CurrentDirectoryResult`, `EnvResult`,
  `CapturedRunResult`, `FileInfoResult`, `TempDirectoryResult`, with each
  union's fixed source text concatenated directly (each block ends with its
  closing brace and newline). A program using only `try_read_file` therefore
  gets exactly the existing `ReadFileResult` prelude and is otherwise unchanged.
- The trigger is syntactic for each built-in name, as it is for
  `try_read_file`: an identifier call named `try_write_file` (even if shadowed
  by a user function) introduces `FileResult` and its generated helper; the same
  applies to the other new built-ins, including `try_run`/`RunResult`,
  `try_current_directory`/`CurrentDirectoryResult`,
  `try_getenv`/`EnvResult`, `try_run_capture`/`CapturedRunResult`,
  `try_file_info`/`FileInfoResult`, and
  `try_create_temp_directory`/`TempDirectoryResult`. A program with no new
  built-in and no `try_read_file` gets no prelude module.
- The same prelude module also carries a non-static generated helper for each
  used built-in. Helpers build the result union with the generated constructors;
  the runtime only returns success/failure and a message or payload. A user
  function that shadows a built-in still causes its prelude helper to be
  emitted, just as in the existing `try_read_file` rule.

**`try_write_file`**

- `try_write_file(path, text)` takes exactly one `string` path and one `string`
  text. It creates the file if necessary or truncates it, writes the string's
  bytes as stored, and closes the file. It never appends and never creates
  parent directories. The creation mode is `0644`, subject to the process
  umask.
- It returns `FileResult`. Success is `FileResult.Ok`; every failure is
  `FileResult.Failed { message = ... }` and never terminates the program.
- A failure to open or create is `open <path>: <reason>`, matching Go's
  `os.WriteFile` text. A write failure is `write <path>: <reason>`; a short
  write is `write <path>: short write`. A close failure is
  `close <path>: <reason>`. Reasons are lower-case operating-system text, such
  as `no such file or directory`, `permission denied`, or `is a directory`.
  There is no loader prefix.
- Calling it with the wrong number of arguments or with arguments that are not
  two `string` values is an error. Its return type is `FileResult`, and it is
  not variadic.

**`try_write_bytes`**

- `try_write_bytes(path, data)` has the same create-or-truncate, `0644`,
  full-write, close, and `FileResult` rules as `try_write_file`, but writes the
  elements of a `byte[]` in order. NUL bytes and arbitrary non-UTF-8 bytes are
  valid file content.
- Its open, write, short-write, and close failure texts are the same
  `open`/`write`/`close <path>: <reason>` forms.
- Calling it with the wrong number of arguments or with a non-`string` path or
  non-`byte[]` data is an error. Its return type is `FileResult`, and it is not
  variadic.

**`try_create_directory`**

- `try_create_directory(path)` creates exactly one directory level with mode
  `0755`, subject to the process umask. It does **not** create missing parents
  and fails if the path already exists. It returns `FileResult`; success is
  `FileResult.Ok`.
- A failure is `FileResult.Failed { message = ... }` with
  `mkdir <path>: <reason>`, matching Go's `os.Mkdir` text (for example
  `mkdir /x: file exists` or `mkdir /x/y: no such file or directory`).
- Calling it with the wrong number of arguments or a non-`string` argument is an
  error. Its return type is `FileResult`, and it is not variadic.

**`try_remove`**

- `try_remove(path)` removes one regular file, one symbolic link (the link
  itself, not its target), or one empty directory. It is never recursive; a
  non-empty directory is a recoverable failure. It returns `FileResult`.
- A failure is `FileResult.Failed { message = ... }` with
  `remove <path>: <reason>`, matching Go's `os.Remove` text (for example
  `remove /x: no such file or directory` or
  `remove /x: directory not empty`).
- Calling it with the wrong number of arguments or a non-`string` argument is an
  error. Its return type is `FileResult`, and it is not variadic.

**`try_list_directory`**

- `try_list_directory(path)` reads one directory and returns
  `ListDirectoryResult`. Success is `ListDirectoryResult.Ok { entries = ... }`,
  where `entries` is a fresh `string[]` containing only entry names, with no
  path prefix, excluding `.` and `..`. The names are sorted bytewise ascending,
  so the result does not depend on filesystem order. An empty directory yields
  an empty array.
- A failure is `ListDirectoryResult.Failed { message = ... }`. A failure to open
  the directory is `open <path>: <reason>` (for example
  `open /x: not a directory` for a regular file); a failure while reading
  directory entries is `readdirent <path>: <reason>`. There is no loader prefix.
- Calling it with the wrong number of arguments or a non-`string` argument is an
  error. Its return type is `ListDirectoryResult`, and it is not variadic.

**`try_run`**

- `try_run(program, args)` takes exactly one `string` program path and one
  `string[]` argument array. It executes `program` **directly** with
  `execve`-style semantics. It does not invoke a shell, search `PATH`, perform
  word splitting, or add quoting. A `program` containing no slash is resolved
  relative to the current working directory, exactly as `execv` would; it is
  **not** looked up in `PATH`. `argv[0]` is `program` as given, and the elements
  of `args` become `argv[1]`, `argv[2]`, and so on in order. Empty-string
  arguments are passed intact. Kitten strings cannot contain NUL, so no NUL
  validation is needed.
- The child inherits the environment, the current working directory, and file
  descriptors 0, 1, and 2. Before forking, the runtime flushes standard output
  and then standard error, so buffered Kitten output precedes child output and
  is not duplicated into the child. There is no stdin redirection, output
  capture, environment control, or timeout; those are **deferred**.
- `RunResult` is the predeclared result union:

  ```kitten
  union RunResult {
      Ok { int status; }
      Failed { string message; }
  }
  ```

- A child that runs and exits normally returns `RunResult.Ok { status = n }`,
  where `n` is its exit status in `0..255`. A child terminated by signal `s`
  returns `Ok { status = 128 + s }`. A stopped child is not reported; the
  runtime waits until it terminates. A nonzero child status is still `Ok`, never
  `Failed`.
- Failure to start the program (missing file, not executable, a directory, a
  resource limit, and so on) returns `RunResult.Failed { message = ... }` with
  `fork/exec <program>: <reason>`, using the same lower-case Go-compatible reason
  mapping as the file and directory built-ins. There is no loader prefix.
- Calling it with the wrong number of arguments, a non-`string` program, or a
  non-`string[]` argument array is an error. Its return type is `RunResult`, and
  it is not variadic.

**`try_run_capture`**

- `try_run_capture(program, args)` has exactly the execution semantics of
  `try_run`: it executes `program` directly with no shell or `PATH` search;
  `program` is `argv[0]`, `args` are `argv[1..]`, and the environment, working
  directory, and umask are inherited. The parent flushes stdout and stderr
  before forking. A normal exit yields `0..255`; a signal termination yields
  `128 + signal`; a nonzero exit is `Ok`, never `Failed`.
- The child's stdin is `/dev/null`. Its stdout and stderr are captured
  separately into fresh GC-managed strings. A failure to start is
  `Failed { message = "fork/exec <program>: <reason>" }`, using the same
  Go-compatible reason mapping as `try_run`.
- `CapturedRunResult` is the predeclared result union:

  ```kitten
  union CapturedRunResult {
      Ok { int status; string stdout; string stderr; }
      Failed { string message; }
  }
  ```

- Each captured stream is capped at exactly 16 MiB (16,777,216 bytes). Exactly
  that many bytes is allowed. If either stream would exceed it, the runtime
  kills the child with `SIGKILL`, closes both pipes, reaps the child, and returns
  `Failed` with
  `run <program>: stdout exceeds the 16777216 byte limit` (or `stderr`).
- A NUL byte in either captured stream is a `Failed` result after the child has
  been reaped, with
  `run <program>: stdout contains a NUL byte at offset <n>` (or `stderr`),
  where `<n>` is the zero-based decimal offset of the first NUL. Bytes above
  `0x7F` and invalid UTF-8 are preserved as bytes. A `byte[]`-returning variant
  is **deferred**.
- Calling it with the wrong number of arguments, a non-`string` program, or a
  non-`string[]` argument array is an error. Its return type is
  `CapturedRunResult`, and it is not variadic.

**`try_file_info`**

- `try_file_info(path)` takes exactly one `string` and returns
  `FileInfoResult`:

  ```kitten
  union FileInfoResult {
      Ok { bool is_file; bool is_directory; bool is_symlink; bool executable; int size; }
      Missing
      Failed { string message; }
  }
  ```

- `lstat` classifies the path itself. `ENOENT` or `ENOTDIR` is `Missing`; any
  other `lstat` failure is `Failed` with `lstat <path>: <reason>`.
- `is_symlink` reports whether the path itself is a symbolic link. If it is,
  `stat` follows it. A dangling target (`ENOENT`/`ENOTDIR` from `stat`) is
  `Ok { is_symlink = true, all other booleans false, size = 0 }`. Any other
  `stat` failure is `Failed` with `stat <path>: <reason>`.
- `is_file` and `is_directory` describe the followed target (or the non-link
  path): regular file and directory respectively. FIFOs, sockets, and devices
  give both false. `size` is the followed target's byte size when it is a
  regular file, otherwise 0.
- `executable` is true when `access(path, X_OK)` succeeds. The kernel uses the
  real UID/GID for this check. A directory with its search bit is executable.
  PATH-style program lookup must therefore require `is_file && executable`, not
  `executable` alone.
- Calling it with the wrong number of arguments or a non-`string` argument is an
  error. Its return type is `FileInfoResult`, and it is not variadic.

**`try_create_temp_directory`**

- `try_create_temp_directory(prefix)` takes exactly one `string` prefix and
  returns `TempDirectoryResult`:

  ```kitten
  union TempDirectoryResult {
      Ok { string path; }
      Failed { string message; }
  }
  ```

- The base directory is `$TMPDIR` when it is set to a non-empty value, and
  `/tmp` otherwise. An empty `TMPDIR` is treated as unset. There is no silent
  fallback when a non-empty `TMPDIR` is not usable; the result is `Failed`.
- A relative `TMPDIR` is made absolute against the process's current directory
  using the same `getcwd` behavior as `try_current_directory`. The returned path
  is always absolute.
- The prefix must be non-empty, at most 64 bytes, and contain only ASCII
  letters, digits, `.`, `_`, and `-`. The prefix `..` is additionally rejected
  even though its bytes are allowed. An invalid prefix is
  `Failed { message = "invalid temporary directory prefix" }` before any
  filesystem access.
- The directory is created atomically by `mkdtemp(3)` with the template
  `<base>/<prefix>.XXXXXX`. The six replacement characters are the C library's
  random name characters; on the verified Linux/glibc host they are ASCII
  letters and digits. The creation mode is exactly `0700` regardless of the
  process umask: the runtime clears the umask only around the atomic
  `mkdtemp` call and restores it immediately. Thread-safe use is not required,
  and a post-creation `chmod` would race with another process.
- A failure to create is `Failed`. When `mkdtemp` reports `ENOENT` or
  `ENOTDIR`, the base is checked with `stat`; if that check also reports
  `ENOENT` or `ENOTDIR`, the message is `stat <base>: <reason>`, matching Go's
  `os.MkdirTemp`. Otherwise the message is `mkdir <base>/<prefix>.XXXXXX:
  <reason>`, using the same lower-case Go-compatible reason mapping as the other
  file built-ins. A missing base therefore reports
  `stat <base>: no such file or directory`; a regular-file or unwritable base
  reports `mkdir <base>/<prefix>.XXXXXX: <reason>`.
- The returned path is a fresh GC-managed string. The runtime registers no
  cleanup hook: the caller removes the directory with `try_remove` after
  emptying it.
- The built-in opens no file descriptor and installs no signal handler. Its
  runtime ABI is:

  ```c
  bool ktn_rt_try_create_temp_directory(const ktn_string *prefix,
                                        const ktn_string **path,
                                        const ktn_string **message);
  ```

- Calling it with the wrong number of arguments or a non-`string` argument is an
  error. Its return type is `TempDirectoryResult`, and it is not variadic.
- The future libc-free runtime must obtain the random suffix from
  `getrandom(2)` or `/dev/urandom` and create the directory with `mkdir(2)` in
  mode `0700` while no umask can reduce it, retrying on `EEXIST`.

**`try_current_directory`**

- `try_current_directory()` takes no arguments and returns the process's current
  working directory as an absolute path. It uses the kernel's `getcwd` result
  and does **not** consult `$PWD`. There is no `chdir` built-in. The runtime grows
  its buffer when `getcwd` reports `ERANGE`, so there is no `PATH_MAX`-sized
  limit.
- `CurrentDirectoryResult` is the predeclared result union:

  ```kitten
  union CurrentDirectoryResult {
      Ok { string path; }
      Failed { string message; }
  }
  ```

- Success returns `CurrentDirectoryResult.Ok { path = ... }` with a fresh
  GC-managed absolute path. Failure is recoverable and returns
  `CurrentDirectoryResult.Failed { message = ... }` with
  `getwd: <reason>`, using the same lower-case Go-compatible reason mapping as
  the other recoverable built-ins. A removed current directory therefore fails
  with `getwd: no such file or directory`.
- Calling it with any argument is an error. Its return type is
  `CurrentDirectoryResult`, and it is not variadic.

**`try_getenv`**

- `try_getenv(name)` takes exactly one `string` and returns `EnvResult`:

  ```kitten
  union EnvResult {
      Set { string value; }
      Unset
  }
  ```

- It reads the process environment. A variable set to the empty string returns
  `Set { value = "" }`; a variable that is not present returns `Unset`. An empty
  name, or a name containing `=`, returns `Unset`, matching `getenv` returning
  no match. The value is copied as the bytes stored by the OS, including bytes
  above `0x7F`, and is a fresh GC-managed string. NUL cannot occur because
  Kitten strings cannot contain it.
- There is no setter: `setenv` is not provided, and `try_run` continues to
  inherit the environment unchanged. The future native runtime must read the
  environment from the process's initial stack (`envp`), because there is no
  libc `getenv` in that runtime.
- Calling it with the wrong number of arguments or a non-`string` argument is
  an error. Its return type is `EnvResult`, and it is not variadic.

**`int_from_byte`**

- `int_from_byte(value)` takes exactly one `byte` and returns the same numeric
  value as an `int` in `0..255`.
- Calling it with the wrong number of arguments or a non-`byte` argument is an
  error.
- Its return type is `int`.

**`byte_from_int`**

- `byte_from_int(value)` takes exactly one `int` and returns its low eight bits
  as a `byte`, `0..255`. It wraps and never fails.
- Calling it with the wrong number of arguments or a non-`int` argument is an
  error.
- Its return type is `byte`.

**`string_to_bytes`**

- `string_to_bytes(value)` takes exactly one `string` and returns a fresh
  `byte[]` containing the string's bytes, in stored order and without the NUL
  terminator.
- Calling it with the wrong number of arguments or a non-`string` argument is an
  error.
- Its return type is `byte[]`.

**`bytes_to_string`**

- `bytes_to_string(value)` takes exactly one `byte[]` and returns a fresh
  `string` containing the same bytes.
- A NUL byte anywhere in the array is a fatal runtime error with the message
  `byte array contains a NUL byte at index <i>`, where `<i>` is decimal.
- Calling it with the wrong number of arguments or a non-`byte[]` argument is an
  error.
- Its return type is `string`.

`byte[]` is an ordinary array type. It is created with `new byte[n]`, is
zero-filled, supports indexing and assignment with the existing bounds checks,
and may be used as a parameter type, return type, struct field, union payload,
and map value. Map keys remain `string`-only. String indexing and
`length(string)` remain as they are: indexing yields an `int` byte value, and
`length` yields the byte count.

### 18.9 Diagnostics

The checker reports a single `Error` containing:

- a **category**: undefined name, duplicate declaration, invalid operator, type
  mismatch, invalid assignment, bad call, invalid return, invalid type, or
  invalid expression;
- a one-based `line:column` position taken from the AST (the checker never
  re-lexes source); and
- a message naming the actual mismatch or unresolved name.

Diagnostics are deterministic for the same input. The checker does not panic on
malformed or incomplete syntax trees.

### 18.10 Provisional decisions recorded

1. `int` is signed 64-bit, `float` is IEEE 754 binary64, and `byte` is
   unsigned 8-bit.
2. Types match exactly; there are no implicit conversions and no casts.
3. `void` is only a function return type.
4. `-> void` is equivalent to no return type.
5. Struct equality is not supported; string `==`/`!=` compare contents.
6. Relational operators are numeric-only; equality is defined for primitives
   and enums and is not supported for structs, arrays, or unions.
7. A variable initializer is optional, and reading an uninitialized variable is
   not diagnosed.
8. Visibility modifiers are accepted on top-level `fn` and `struct`; the default
   is private (module-local) and `public` exports a declaration (Section 22).
9. Return-path analysis is conservative: loops never definitely return, and
   unreachable code is not diagnosed.
10. Struct literals must initialize every field.
11. Checker errors are fail-fast: only the first error is reported.
12. There is no implicit `main`; the entry point is validated at build time and
    must be in the root module (Sections 19 and 22).
13. Maps are reference types whose only creator is assignment to a new key; a
    missing-key lookup is fatal (Section 24).

---

## 19. Compilation model (provisional)

This section records how Kitten programs are compiled to native executables. It
is **provisional**: the representation choices are the first working backend and
are expected to evolve.

### 19.1 C type mapping

| Kitten | C |
| --- | --- |
| `int` | `int64_t` (signed 64-bit) |
| `float` | `double` (IEEE 754 binary64) |
| `bool` | `bool` (`<stdbool.h>`) |
| `byte` | `uint8_t` (unsigned 8-bit) |
| `string` | `const ktn_string *` (an immutable, length-carrying byte string) |
| `void` | `void` (function return position only) |
| struct type `S` | `ktn_s_S *` (a GC-managed object reference) |
| enum type `E` | `ktn_e_E` (a typedef for `int64_t`) |
| union type `U` | `ktn_s_U *` (a GC-managed object reference) |
| map type `map[string]V` | `ktn_map *` (a GC-managed hash table) |
| array type `T[]` | `ktn_array *` (a GC-managed dynamic array) |

Generated code includes `<stdint.h>` and `<stdbool.h>` through the runtime
header, so no C integer-width assumption is made and no undefined behavior is
relied upon. Integer arithmetic is emitted through the runtime's
`ktn_rt_i64_*` helpers, which use unsigned bit patterns and explicit division
checks; generated C never evaluates signed overflow, division by zero, or a
signed shift with an out-of-range count. `&`, `|`, `^`, and `~` may use direct C
operators; `<<` and `>>` use `ktn_rt_i64_shl` and `ktn_rt_i64_shr`.

### 19.2 Struct representation and reference semantics

A struct value is a **reference to a heap object**, not an inline copy:

- Evaluating a struct literal allocates a fresh object with the garbage
  collector and initializes its fields. The collector zeroes the memory, so the
  object is safely initialized before fields are written.
- Assigning one struct variable to another copies the reference. Both names then
  refer to the same object, and mutating a field through either name is visible
  through the other.
- Nested struct fields are references, so a field access chain such as
  `line.start.x` reads the object reached through the outer references.
- Passing a struct as a function argument, and returning a struct from a
  function, pass and return the reference. A function that mutates a struct
  parameter mutates the caller's object.
- Struct fields are mutable; a field reached through an l-value is assignable,
  including fields at any nesting depth.

Because values are references, two struct expressions are never structurally
equal; struct equality is a checker error (Section 18.3). Whether Kitten should
instead offer value semantics, an explicit reference type, or copy-on-assignment
is **deferred**.

### 19.3 Runtime and garbage collection

- The runtime is written in C and has no dependency on the compiler's
  implementation language.
- `ktn_rt_init` initializes the Boehm–Demers–Weiser conservative collector. The
  generated C entry point calls it before any allocation, then calls
  `ktn_rt_set_args(argc, argv)` before the Kitten `main` runs.
- `ktn_rt_alloc` allocates a zero-initialized, pointer-scanned object with the
  collector. Struct allocation and constructed strings go through it. The
  collector reclaims objects automatically; Kitten code never frees memory by
  hand.
- `ktn_string` is the runtime's string object: it stores a byte length and a
  pointer to the bytes, which are still NUL-terminated (Section 20.1). A
  constructed string is one GC-managed object holding its header and its bytes
  together; a literal is a `static const ktn_string` whose bytes are static.
- The runtime also provides `ktn_rt_string_equal`, `ktn_rt_string_concat`,
  `ktn_rt_string_append`, `ktn_rt_string_length`, `ktn_rt_string_at`,
  `ktn_rt_substring`, `ktn_rt_array_new`, `ktn_rt_array_at`,
  `ktn_rt_array_length`, `ktn_rt_set_args`, `ktn_rt_argv`, `ktn_rt_map_new`,
  `ktn_rt_map_set`, `ktn_rt_map_at`, `ktn_rt_map_has`, `ktn_rt_map_length`,
  `ktn_rt_read_file`, `ktn_rt_try_read_file`, `ktn_rt_try_write_file`,
  `ktn_rt_try_write_bytes`, `ktn_rt_try_create_directory`,
  `ktn_rt_try_remove`, `ktn_rt_try_list_directory`, `ktn_rt_try_run`,
  `ktn_rt_try_current_directory`, `ktn_rt_try_getenv`,
  `ktn_rt_try_run_capture`, `ktn_rt_try_file_info`,
  `ktn_rt_try_create_temp_directory`, `ktn_rt_exit`, the
  `ktn_rt_i64_*` integer helpers, `ktn_rt_string_to_bytes`,
  `ktn_rt_bytes_to_string`, and the built-in print and eprint helpers.
- Strings are pointer-carrying objects, so structs may still contain pointers
  (to strings and to nested structs) and must be scanned; the runtime therefore
  uses the scanning allocator (`GC_MALLOC`), not the atomic one. Array element
  buffers and map key, hash, and value buffers use the same scanning allocator,
  so arrays and maps of strings, structs, unions, and nested collections are
  handled correctly, and a string stored in any of them keeps its bytes alive
  for as long as it is reachable.
- Exceptions and finalizers are **not** implemented.

### 19.4 Built-in `print` behavior

`print` is expanded by the code generator into calls to typed runtime helpers.
Each argument is written with the helper for its type; arguments are separated
by a single space; the call ends with a newline (Section 13). Printed strings
are written with `fputs`, never as a `printf` format string, so a string value
cannot be interpreted as a format specifier.

Rendering is provisional: `int` uses `%lld` of the 64-bit value; `float` uses
`%g` (six significant digits by default); `bool` prints `true` or `false`;
`string` prints its bytes. The `float` and `bool` renderings are not exercised
by the current fixtures and may change.

### 19.5 Entry-point contract

To build an executable, a program must define:

```text
fn main() {
    // ...
}
```

or the equivalent `fn main() -> void`. The entry point:

- must be named `main`;
- must take no parameters; and
- must return no value.

A missing or invalid entry point is rejected at build time with a diagnostic;
the compiler never invents a `main`. The generated C entry point is the C
function `main`, which calls the runtime initializer, saves its `argc`/`argv` for
`argv()` (Section 18.8), and then calls the compiled Kitten `main` function. The
two never collide because Kitten functions are emitted with a reserved prefix.

### 19.6 Native build pipeline and command line

`purr build <file.ktn> [-o <output>]` runs the whole pipeline and writes a
native executable. Without `-o`, the executable is written to the current
directory as the source file's base name without the `.ktn` extension. The
command never reports success unless the executable was produced.

`purr run <file.ktn>` compiles through the same pipeline into a temporary
location, executes the program, forwards its standard output and standard error,
and removes the temporary artifacts. It returns the program's exit status.

Toolchain discovery:

- A compiler set through the `CC` environment variable is used, before
  `cc`/`gcc`/`clang` are searched for on `PATH`.
- Boehm GC flags are taken from `pkg-config bdw-gc` when available, and
  otherwise link against `-lgc` using the system include path.
- Missing compilers, headers, or libraries are reported as actionable errors.
  The compiler does not install packages or run privileged commands, and never
  falls back to a non-GC implementation.

The compiler invokes subprocesses with argument arrays, never through a shell,
so paths containing spaces or shell metacharacters are safe. The initial backend
targets the host platform; cross-compilation and Windows are **not** supported.

### 19.7 Generated code conventions

- Generated C is deterministic for a given program: declarations are emitted in
  source order and generated names follow a fixed scheme.
- Kitten identifiers are never copied verbatim into C. Functions, variables,
  struct types, struct fields, and constructors are emitted with reserved
  prefixes (`ktn_fn_`, `ktn_var_`, `ktn_s_`, `ktn_f_`, `ktn_new_`), so C
  keywords and generated helper names cannot collide, and shadowing maps onto C
  block scoping.
- Struct declarations are forward-declared, and function prototypes are emitted
  before bodies, so functions and structs may refer to declarations that appear
  later in the source, including recursion and mutual recursion.
- String literals are decoded and then re-encoded for C: quotes, backslashes,
  control characters, question marks, and non-ASCII bytes are escaped
  explicitly. Source text is never inserted into a C string literal unchanged.
- Code generation returns an error, rather than panicking, when the syntax tree
  is inconsistent with the checked result.

### 19.8 The compiler-reserved `<runtime>` module

- `<runtime>` is a compiler-reserved synthetic module, separate from
  `<prelude>`. It is never loadable from disk, cannot be imported by user code,
  and is never injected by `purr build` or `purr run`. An explicit Go/Kitten
  harness hook supplies its source text; the production CLI cannot reach that
  hook.
- The hook parses the supplied source with the ordinary lexer and parser and
  appends it after every loaded module and after `<prelude>` if that module
  exists. Its path is `<runtime>` and its id is the current module count. A
  program using both synthetic modules therefore has loaded modules, then
  `<prelude>`, then `<runtime>`; a program using neither has no synthetic
  module.
- Intrinsic names resolve only while checking `<runtime>`. In every other
  module they are ordinary names: an undeclared intrinsic call is the ordinary
  undefined-function error, a user may declare a function with that name, and
  no intrinsic name is reserved. The intrinsic names are:

  ```text
  raw_load8(int address) -> byte
  raw_store8(int address, byte value)
  raw_load64(int address) -> int
  raw_store64(int address, int value)
  raw_copy(int destination, int source, int count)
  raw_zero(int address, int count)
  address_of(byte[] data) -> int
  address_of(string value) -> int
  syscall(int number, int a, int b, int c, int d, int e, int f) -> int
  ```

  `address_of` is the one overloaded intrinsic; its argument type selects the
  byte-array or string data pointer. `raw_alloc` and `stack_bounds` are
  **deferred** to the GC phase.
- Raw memory operations are trusted code. They do not validate addresses and
  cannot uphold the language's no-undefined-behavior guarantee; that guarantee
  applies to every module reachable from user source, where the intrinsics are
  unreachable. A negative size passed to `raw_copy` or `raw_zero` is a fatal
  runtime error with `negative size <n>`.
- Generated C lowers raw loads/stores with `memcpy` on an `unsigned char *`
  cast of the address, raw copy/zero with `memmove`/`memset`, `address_of` to a
  data-pointer cast to `int64_t`, and `syscall` to the C library `syscall()`
  with `long` casts. The helpers are static inline `ktn_rt_raw_*` and
  `ktn_rt_syscall` in the MIT runtime header.
- The runtime module source lives under `runtime/ktn/` with the MIT notice. Its
  public functions live in that module under the ordinary public/private rules.

### 19.9 Limitations and provisional decisions

1. Struct values are GC-managed references with pointer-copy assignment.
2. `int` is `int64_t` and `float` is `double`.
3. Strings are immutable NUL-terminated byte sequences. `+` and `+=`
   concatenate, `==`/`!=` compare content, and byte length, byte indexing, and
   byte ranges are implemented (Section 20); embedded NUL bytes are unsupported,
   and there are no string/number conversions or code-point operations.
4. The runtime provides GC initialization, struct allocation, string
   concatenation/append/equality/length/indexing/ranges, dynamic arrays, maps,
   the process argument vector, print and eprint helpers, and process exit.
5. The entry point must be a zero-parameter, void-returning `main`; the generated
   C entry point captures `argc`/`argv` for `argv()` before calling it.
6. Default `build` output is the source base name in the current directory.
7. Only the host platform is supported; there is no cross-compilation.
8. Code generation targets C; there is no LLVM or native object-code backend.
9. `float` prints with `%g` and `bool` prints as `true`/`false`.
10. Optimization is left to the system C compiler; the compiler passes no
    optimization level of its own.
11. Self-hosting and the standard library are not implemented. The long-term
    roadmap is recorded in AGENTS.md.
12. Arrays are GC-managed references with pointer-copy assignment; elements are
    zero-initialized; access is bounds-checked at runtime (Section 21).
13. A program may span several modules; a file is a module, imports resolve by
    relative path, and default visibility is private (Section 22).
14. File reading is `read_file` (fatal on failure) or `try_read_file`
    (recoverable `ReadFileResult`); both use the same 256 MiB limit
    (Sections 18.8 and 22.2).
15. Enums are value types compared by case; unions are GC-managed references
    inspected only with an exhaustive `match` (Section 23).
16. Maps are GC-managed references with string keys, an inline value, and a
    fatal missing-key lookup (Section 24).
17. String length and indexing are byte-oriented, `text[i]` yields an `int`
    byte, and `substring` yields a byte range (Section 20.6).

---

## 20. Strings (provisional)

This section records the implemented string semantics. It is **provisional**:
the representation is the first working model and may evolve.

### 20.1 Representation

- A `string` is an immutable, length-carrying sequence of bytes. The C
  representation is `const ktn_string *` (Section 19.1): the runtime's
  `ktn_string` stores the number of bytes and a pointer to the bytes, which are
  NUL-terminated as well so that they remain usable with C library functions.
  Storing the length makes `length` and indexing constant-time (Section 20.6).
- A string literal denotes a string backed by static storage: the code generator
  emits one `static const ktn_string` per distinct literal, with the byte length
  it computed from the decoded text and the bytes as a static C string. The lexer
  supports the escapes `\n`, `\t`, `\"`, and `\\` (Section 6.3); the code
  generator decodes them and re-encodes the bytes safely for C.
- A string produced by concatenation or by `substring` is a freshly allocated,
  exactly-sized, GC-managed `ktn_string` whose header and bytes are one object.
  It remains valid as long as Kitten code can reach it, and it is never freed by
  hand. Copying the pointer copies the string value, and because strings are
  immutable the copies never diverge.
- Strings are byte sequences, not necessarily valid text; concatenation and
  equality are byte operations.
- **Embedded NUL bytes are not supported.** Because the bytes are
  NUL-terminated, a string cannot contain a NUL byte in the middle. Kitten
  provides no way to create one: there is no `\0` escape, and concatenation of
  NUL-free strings is NUL-free. This is a limitation of the representation, not
  a hidden behavior.

### 20.2 Concatenation

- `left + right` concatenates two `string` values and produces a new `string`.
- Both operands must be `string`. A `string` operand mixed with an `int`,
  `float`, `bool`, or struct is a type error; there are no implicit conversions
  (there is no automatic conversion of other values to text).
- Concatenation is left-associative, so `a + b + c` groups as `(a + b) + c`.
- `target += value` appends `value` to `target` when both are `string`. It
  requires `target` to be a mutable l-value, exactly like other compound
  assignments.
- Concatenating with an empty string (`""`) yields a string equal to the other
  operand. Concatenating two empty strings yields an empty string.
- Repeated concatenation copies each intermediate result, so a long chain costs
  more than a builder would. A builder or join operation is **deferred**;
  correctness is prioritized over this optimization.

### 20.3 Equality

- `==` and `!=` compare two `string` values **by content**, never by address.
  Two distinct strings with the same bytes are equal.
- Equality requires both operands to be `string`; mixing types is a type error.
- Ordering comparisons (`<`, `<=`, `>`, `>=`) are numeric-only and are not
  defined for `string`.

### 20.4 Print behavior

`print` writes a string as its bytes (Section 19.4). Printing an empty string
produces an empty field; printing a string containing `\n` produces multiple
output lines.

### 20.5 Type rules summary

| Expression | Operand types | Result |
| --- | --- | --- |
| `a + b` | `string`, `string` | `string` (concatenation) |
| `a += b` | `string` target, `string` | `string` |
| `a == b`, `a != b` | `string`, `string` | `bool` (content comparison) |
| `length(a)` | `string` | `int` (byte count) |
| `a[i]` | `string`, `int` | `int` (byte value 0..255) |
| `substring(a, start, end)` | `string`, `int`, `int` | `string` |

### 20.6 Byte length, indexing, and ranges

String access is **byte-oriented, never code-point-oriented**. This is
consistent with the rest of the string design: concatenation, equality, and the
NUL-terminated representation all operate on bytes, so length and indexing do
too. No operation mixes bytes and code points.

- `length(text)` returns the number of **bytes**, excluding the terminator. An
  empty string has length 0. For example, `"é"` has length 2 because the
  character is encoded as two UTF-8 bytes.
- `text[index]` returns the **byte value** at `index` as an `int` in 0..255. A
  byte is a number, not a one-character string, so it is classified with integer
  comparisons (`byte >= 97 && byte <= 122` is a lowercase ASCII letter). A
  multi-byte UTF-8 character occupies several indices.
- Indexing is bounds-checked. A negative index, or an index at or beyond the
  byte length, is a **fatal runtime error**; it is never an out-of-bounds read
  and never a fabricated value.
- String indexing is **read-only**: strings are immutable, and assigning to
  `text[index]` is a checker error.
- `substring(text, start, end)` returns a new string holding the bytes in
  `[start, end)` — `start` inclusive, `end` exclusive. The range must satisfy
  `0 <= start <= end <= byte length`; any other range is a fatal runtime error.
  `substring(text, i, i)` is the empty string, and `substring(text, i, i + 1)`
  is the byte at `i` as a one-byte string.
- `length` and indexing are constant-time, because the length is stored with
  the bytes; they do not scan the string. `substring` and concatenation copy the
  bytes they produce, so they are linear in the size of their result, and they
  allocate one object per result.

### 20.7 Deferred string operations

The following are **not** implemented in this revision and must not be assumed:

- Ordering comparisons and string ordering (so range checks over strings must be
  written as explicit byte comparisons).
- Searching, replacing, splitting, and joining.
- Interpolation, raw strings, and multiline strings.
- Mutable strings and in-place mutation.
- **Conversions between strings and other types**, including `int` to `string`
  and `string` to `int`. This is deliberately deferred: diagnostics can print
  numbers directly (`print("at", line, column)` accepts several arguments), and a
  lexeme is produced with `substring`, so neither conversion is required for a
  lexer in this revision.
- **Code-point (Unicode) operations**: there is no way to decode a UTF-8
  sequence into a code point, and no character type. Non-ASCII text is handled
  as bytes.

### 20.8 Provisional decisions recorded

1. Strings keep the single NUL-terminated `const char *` representation; no
   separate string type or struct wrapper was introduced.
2. Concatenation allocates a fresh GC buffer of exactly `len(left) + len(right)
   + 1` bytes; empty operands are handled by the same path.
3. `+` and `+=` are concatenation for two strings; `-=` and the other arithmetic
   operators remain numeric-only.
4. Equality is content-based, as it already was.
5. Embedded NUL bytes are unsupported and cannot be created.
6. No implicit conversion from other types to `string` was added.
7. Length, indexing, and substrings are **byte-oriented**; no code-point
   operation exists, and none is implied.
8. `text[index]` yields an `int` byte value, which makes byte classification use
   the existing integer comparisons without new syntax.
9. Indexing is read-only, and both indexing and `substring` fail deterministically
   when out of range.
10. `int`-to-`string` and `string`-to-`int` conversions are deferred (Section 20.7).

---

## 21. Arrays (provisional)

This section records the implemented dynamic-array semantics. It is
**provisional**: the representation is the first working model and may evolve.

### 21.1 Representation

- An array value is a reference to a GC-managed object holding the length, the
  element size, and a pointer to the element storage. Its C representation is
  `ktn_array *` (Section 19.1).
- The element storage is allocated with the scanning collector (`GC_MALLOC`)
  and zero-initialized, so arrays of `string`, structs, and nested arrays are
  handled correctly by the collector.
- All array types share this one representation. The element type is enforced by
  the checker, not by the C type; generated code casts the element pointer to
  the element's C type.

### 21.2 Array types

- An array type is written by appending `[]` to an element type: `int[]`,
  `float[]`, `bool[]`, `string[]`, `Rectangle[]`, and nested `int[][]`.
- An element type must not be `void`; `void[]` and `new void[3]` are errors.
- Array types match exactly. `int[]` and `float[]` are different types, and there
  are no implicit conversions between array types.

### 21.3 Creation

- `new Element[length]` creates an array of `length` elements of type `Element`.
  The `length` expression must be `int`.
- The `length` expression is evaluated once when the array creation expression is
  evaluated.
- The element type may itself be an array type written with `[]`, so
  `new int[][3]` creates an array of three `int[]` elements; each element starts
  as a null reference and must be assigned before use.
- A negative length at runtime is a fatal runtime error (Section 21.6).
- Every element is zero-initialized (Section 21.8).

### 21.4 Indexing and assignment

- `array[index]` reads the element at `index`. The index expression must be
  `int`, and the result has the array's element type.
- An indexed element is an l-value. `array[index] = value`,
  `array[index] += value`, `array[index] -= value`, `array[index]++`, and
  `array[index]--` are all allowed when the element type supports the operation.
- The array expression and the index expression are each evaluated exactly once,
  including for compound assignment and increment, because element access is
  compiled to a single C l-value.
- Assignment requires the element type and the assigned value to match exactly.
- An element reached only through a non-l-value (for example `f()[0]`, where `f`
  returns an array) may be read but not assigned as an l-value; bind the array
  to a variable first.

### 21.5 Length

- `length(array)` returns the number of elements as an `int` (Section 18.8).
- An empty array has length 0. `new int[0]` is valid.

### 21.6 Bounds checking and runtime errors

- Every element access is bounds-checked against the array length before the
  element is addressed.
- A negative index, or an index greater than or equal to the length, terminates
  the program with a message on standard error and a nonzero exit status. This
  is deterministic; there is no out-of-bounds memory access or undefined
  behavior.
- Accessing a null array reference, such as an unassigned nested-array element,
  is also a fatal runtime error rather than undefined behavior.
- The checker statically rejects indexing a value that is not an array and
  indexing with a non-`int`. The runtime check covers indices, which cannot be
  validated statically.

### 21.7 Aliasing and mutation

- Arrays are references. Assigning one array variable to another copies the
  reference, so both names refer to the same storage and a mutation through
  either name is visible through the other.
- Passing an array as a function argument and returning an array from a function
  pass and return the reference. A function that mutates an element mutates the
  caller's array.
- Arrays cannot be compared with `==` or `!=`; compare elements or lengths
  instead.

### 21.8 Element types and zero values

- Every element is zero-initialized when the array is created: `int` is `0`,
  `float` is `0.0`, `bool` is `false`, `byte` is `0`, `string` is the empty
  string, and a struct or nested-array reference is null.
- Because the zero value of `string` is the empty string, a string element is
  safe to read and print before it is assigned (Section 20.1).
- The zero value of a struct or nested-array element is a null reference. It
  must be assigned before it is used. Array access on a null array is caught
  (Section 21.6), but member access on a null struct reference is not checked in
  this revision and is a defect in the program.
- Any supported element type may be used: the primitive types, `string`, structs,
  and other arrays.

### 21.9 Print behavior

`print` does not accept arrays: passing an array to `print` is a semantic error
(Section 18.8). Print elements individually, for example in a loop over
`length(array)`.

### 21.10 Deferred array operations

The following are **not** implemented in this revision and must not be assumed:

- Array literals and initializer lists.
- Resizing, appending, inserting, and removing elements.
- Slicing and subarrays.
- Multi-dimensional creation syntax such as `new int[3][4]` (nest manually).
- Iteration helpers such as a `for value in array` form.
- Array equality by content.
- Sorting, searching, copying, and other library operations.
- A null/optional concept, and null-reference checking for structs.

### 21.11 Provisional decisions recorded

1. All array types share the C representation `ktn_array *`; element types are
   enforced by the checker.
2. Array creation uses the `new` keyword: `new Element[length]`. This avoids the
   ambiguity between creating an array of a named type and indexing a variable
   of the same name.
3. `length` is a built-in function, not a method or an operator.
4. Elements are zero-initialized by the collector.
5. Element access is bounds-checked at runtime; an out-of-range index is a fatal
   error rather than undefined behavior.
6. Arrays are references with pointer-copy assignment.
7. Array equality is not supported.
8. Null struct and nested-array references are not dereference-checked in this
   revision.
9. Element access compiles to a single C l-value, so compound assignment and
   increment evaluate the array and index exactly once.

---

## 22. File I/O and modules (provisional)

This section records how a Kitten program reads files and how a program can be
split across several source files. It is **provisional**: the module model is a
first working design and may evolve.

### 22.1 Modules

- One source file is one **module**. A module's identity is the canonical
  (absolute, cleaned) path of its file.
- A module begins with any number of **import declarations**:

  ```text
  import "geometry.ktn";
  import "util/text.ktn";
  ```

  The path is a string literal naming a file relative to the directory of the
  file that contains the import. Escapes in the literal are decoded as in any
  string (Section 6.3). Imports are top-level declarations and may appear
  anywhere among a module's declarations.
- The module reachable from the file being compiled is the **root module**. The
  root module and every module it imports, directly or indirectly, make up the
  program.
- Import declarations themselves introduce no names.

### 22.2 File I/O

The file and directory built-ins are recoverable except for the existing
fatal `read_file`. All return fresh GC-managed strings and arrays, close every
descriptor on every path, and use Go-compatible operating-system text without a
loader prefix.

- `read_file(path) -> string` reads the entire contents of the regular file at
  `path` into a new GC-managed string. An empty file yields the empty string, so
  an empty file is distinguishable from an error: a successful read of an empty
  file returns `""`, while a failure terminates the program.
- `try_read_file(path) -> ReadFileResult` is the recoverable form: it returns
  `Ok { text }` with the complete contents, or `Failed { message }` with the
  Go-compatible operating-system error text (Section 18.8). The loader adds its
  own `cannot read source file: ` prefix when it consumes this result.
- `read_file` failure is a **fatal runtime error**: a missing or unreadable file,
  a path that is not a seekable regular file, a file larger than the runtime's
  maximum read size (256 MiB), or a read error prints a message on standard
  error and exits with a nonzero status. `try_read_file` reports those file
  failures as `Failed` instead, without terminating.
- `try_write_file(path, text) -> FileResult` creates or truncates the file and
  writes the string bytes with creation mode `0644` subject to the umask.
  `try_write_bytes(path, data) -> FileResult` does the same for a `byte[]`.
  Neither appends nor creates parent directories. Success is `FileResult.Ok`;
  failures are recoverable and use `open`, `write`, or `close` path text.
- `try_create_directory(path) -> FileResult` creates one level with mode `0755`
  subject to the umask. It neither creates parents nor treats an existing path
  as success; failures use `mkdir <path>: <reason>`.
- `try_remove(path) -> FileResult` removes a regular file, a symbolic link
  itself, or an empty directory, never recursively. A non-empty directory is a
  recoverable failure; failures use `remove <path>: <reason>`.
- `try_list_directory(path) -> ListDirectoryResult` returns entry names only,
  sorted bytewise ascending, excluding `.` and `..`; an empty directory yields
  an empty array. Failures use `open <path>: <reason>` or
  `readdirent <path>: <reason>`.
- Every result is a snapshot at the time of the call. The read functions apply
  the 256 MiB limit and are always closed, including on failure paths. The
  returned read string is NUL-terminated; because strings cannot contain an
  embedded NUL byte (Section 20.1), content after the first NUL byte in the file
  is not represented, while `try_write_bytes` can write NUL bytes and
  `string_to_bytes`/`bytes_to_string` preserve byte content except for the
  NUL restriction on `bytes_to_string`.
- `try_run(program, args) -> RunResult` executes a program directly with the
  inherited environment, working directory, and standard descriptors. The
  runtime flushes both streams before forking, passes `program` as `argv[0]`,
  and passes each string in `args` unchanged as the remaining arguments. It
  does not use a shell or search `PATH`; a name without a slash is resolved
  relative to the current working directory. A normal exit yields its `0..255`
  status; a signal termination yields `128 + signal`. A failure to start is a
  recoverable `fork/exec <program>: <reason>`. Output capture, stdin
  redirection, environment control, and timeouts are **deferred**.
- `try_current_directory() -> CurrentDirectoryResult` returns the kernel's
  absolute current-directory path. It ignores `$PWD`, has no `chdir`
  counterpart, grows its buffer on `ERANGE`, and reports a removed directory as
  `getwd: no such file or directory`. The path is a fresh GC-managed string.
- `try_getenv(name) -> EnvResult` returns the environment value as `Set`, or
  `Unset` for an absent variable, an empty name, or a name containing `=`. The
  empty-string value is distinct from `Unset`. Environment values are copied
  bytes and are not modifiable through this API; `try_run` inherits the
  environment unchanged. The future native runtime must read `envp` from the
  process's initial stack.
- `try_run_capture(program, args) -> CapturedRunResult` executes exactly as
  `try_run`, but with stdin from `/dev/null` and stdout/stderr captured into
  separate strings. It caps each stream at 16 MiB and rejects NUL bytes with the
  exact `run ...` messages above. A byte-array capture variant is deferred.
- `try_file_info(path) -> FileInfoResult` reports the path's own symlink status
  and the followed target's file/directory/executable/size facts. Missing paths,
  dangling links, symlink loops, and permission failures use the exact shapes
  above. It opens no file descriptor.
- `try_create_temp_directory(prefix) -> TempDirectoryResult` creates an
  absolute, uniquely named directory under a non-empty `$TMPDIR` or `/tmp`,
  using an atomic `mkdtemp` creation with a six-character random suffix and mode
  `0700`. Invalid prefixes are rejected before filesystem access, an unusable
  `$TMPDIR` is a recoverable failure with Go-compatible text, and the caller
  removes the directory with `try_remove`.

Current-working-directory changes, recursive removal, atomic replacement, and
file metadata beyond the operations above are **not** implemented.

### 22.3 Visibility and name resolution

- A top-level `fn` or `struct` is visible to other modules only when it is
  marked `public`. The default is **private**: an unmarked declaration is
  visible only within its own module. Struct fields and local variables do not
  take visibility modifiers.
- Within a module, a name resolves against the module's own declarations first
  (its private and public declarations), then against the public declarations of
  all modules in the program, then against the built-ins.
- The public declarations of all loaded modules form one shared namespace, so a
  public declaration is visible to every module, including transitively imported
  ones.
- Two modules may each declare a private declaration with the same name; private
  names are module-local and do not conflict. Two modules may **not** declare
  public declarations with the same name: that is a duplicate-declaration error
  that names both modules.
- Referencing a name that is declared privately in another module is a
  **private access** error that names the module that declared it.

### 22.4 Import resolution and the resolution root

- Import paths are resolved relative to the directory of the file that contains
  the import, then canonicalized.
- The **resolution root** is the directory of the root source file. An import
  whose canonical path lies outside that directory (for example `../outside.ktn`
  from the root) is rejected with an error. This keeps a program's sources
  inside the directory the compilation was started from.
- Resolution does not depend on the current working directory, only on the root
  file's location.
- A module is loaded at most once, keyed by its canonical path, so importing the
  same file through different relative paths loads it once.

### 22.5 Import cycles

Import cycles are **allowed**. Because the loader records a module before
loading its imports and because name resolution is order-independent, two
modules may import each other. Mutual recursion between modules is therefore
supported.

### 22.6 Multi-file compilation

- The compiler loads the root module, then its imports, and checks every module
  together.
- Declarations from every module are emitted into one C translation unit.
- To prevent private declarations in different modules from colliding in C,
  imported modules mangle top-level names with their module number: a
  root-module function `f` becomes `ktn_fn_f`, and the same name in imported
  module 1 becomes `ktn_fn_1_f`. Struct types use `ktn_s_1_F` and constructors
  `ktn_new_1_F`. Because a Kitten identifier cannot begin with a digit, a
  mangled imported name can never collide with a root-module name, and distinct
  module numbers keep imported modules distinct. Local variables, parameters,
  and struct fields are not mangled.
- Single-file programs are unaffected: the root module has no prefix, so their
  generated C is unchanged.

### 22.7 Entry point

- An executable program must declare a zero-parameter, void-returning `main` in
  its **root module**. Imported modules are never entry points, even if they
  declare a `main`.
- A missing or malformed entry point is rejected at build time with a
  diagnostic; the compiler never invents a `main`.

### 22.8 Command line

`purr build <file.ktn> [-o <output>]` and `purr run <file.ktn>` are
unchanged in shape. `<file.ktn>` is the root module; imported modules are found
relative to it. Diagnostics name the file an error was found in, with a
line and column where the error is inside a source file.

### 22.9 Deferred module and I/O features

The following are **not** implemented and must not be assumed:

- Qualified or namespaced references to imported declarations; imported names
  are unqualified.
- Selective imports, aliases, and re-exporting.
- Search paths, package manifests, and dependency resolution beyond relative
  paths.
- Metadata, recursive operations, atomic replacement, and other filesystem
  operations beyond the built-ins in Section 18.8.
- A `byte[]`-returning capture variant, stdin redirection, environment control,
  process timeouts, and changing the current directory.

- Separate compilation or incremental builds; the whole program is reloaded and
  recompiled each time.
- Compiled module caching.

### 22.10 Provisional decisions recorded

1. A file is a module; a module is identified by its canonical path.
2. Import syntax is `import "relative/path";`, resolved relative to the
   importing file and restricted to the root source's directory.
3. Imports are transitive: public declarations form one shared namespace.
4. Default visibility is private; `public` exports a declaration.
5. Import cycles are allowed.
6. Imported modules mangle top-level C names with their module number.
7. The entry point must be in the root module.
8. `read_file` is fatal on failure; `try_read_file` returns a recoverable
   `ReadFileResult`.
9. The whole program is loaded and compiled together, with no caching.
10. File and directory built-ins return predeclared `FileResult` or
    `ListDirectoryResult` values, inject their implicit prelude module only when
    a corresponding call is present, and use Go-compatible error text.
11. `try_list_directory` returns sorted entry names only; `try_remove` is never
    recursive.
12. `try_run` executes directly, inherits the environment, working directory,
    and standard descriptors, and reports normal or signal exit statuses as
    `RunResult.Ok`; only a start failure is `Failed`.
13. `try_current_directory` returns the kernel's absolute current-directory
    path, ignores `$PWD`, and has no `chdir` counterpart.
14. `try_getenv` returns `EnvResult.Set` for present variables (including an
    empty value) and `EnvResult.Unset` otherwise; it has no setter and leaves
    the environment unchanged.
15. `try_run_capture` uses `try_run` execution semantics with `/dev/null` stdin
    and separate stdout/stderr strings, capped at 16 MiB per stream; NUL and
    over-limit results are `Failed`.
16. `try_file_info` follows symlinks for kind/size/executable, reports the link
    itself through `is_symlink`, treats dangling links as `Ok` with false facts,
    and uses `lstat`/`stat` path errors. PATH lookup must require
    `is_file && executable`.
17. `try_create_temp_directory` uses non-empty `$TMPDIR` or `/tmp` with no
    fallback, validates a 64-byte ASCII prefix before filesystem access, creates
    atomically with `mkdtemp` and mode `0700`, returns an absolute path, and
    reports missing or unusable bases with Go-compatible text. The runtime
    registers no cleanup hook.

---

## 23. Enums and tagged unions (provisional)

This section records how a Kitten value can hold one of several explicitly
declared alternatives. It is **provisional**: the feature is the first working
design and may evolve.

### 23.1 Enums

An enum names a fixed set of alternatives:

```text
enum TokenKind {
    Identifier,
    Integer,
    StringLiteral,
    EndOfFile
}
```

- A case is written with a comma between cases; a trailing comma is not allowed.
  An enum must have at least one case. (The reference *parser* accepts
  `enum E {}` with no cases and the reference *checker* then rejects it, so this
  sentence is enforced one phase later than it is written; the discrepancy is
  recorded in Section 17.4.)
- A value of the enum type is one of its cases. A case reference is written
  `EnumName.CaseName` and has the enum type.
- Enum types match exactly. A `TokenKind` is not an `int`, and there is no
  implicit conversion between an enum and an integer in either direction.
- Two enum values may be compared with `==` and `!=`, which compare the cases
  they denote. Relational operators are not defined for enums.
- A case name must be unique within its enum.

### 23.2 Tagged unions

A tagged union holds exactly one of its variants, and each variant may carry
fields:

```text
union Expression {
    IntegerLiteral { int value; }
    Name { string nickname; }
    Binary { Expression left; Expression right; }
    Missing
}
```

- A variant with fields is written `VariantName { type name; ... }`; a variant
  with no fields is just `VariantName`. Variants are self-delimiting, so no
  separator is needed between them. A union must have at least one variant.
  (The reference *parser* accepts `union U {}` with no variants and the reference
  *checker* then rejects it, so this sentence is enforced one phase later than it
  is written; the discrepancy is recorded in Section 17.4.)
- A variant name must be unique within its union, and a field name must be
  unique within its variant.
- A union value is a **reference** to a GC-managed object, like a struct value.
  Copying a union value copies the reference.

### 23.3 Construction

- A payload-less variant used as a value is written `TypeName.VariantName` — for
  example `Expression.Missing`. It has the union type.
- A variant with fields is constructed with named field initializers:
  `TypeName.VariantName { field = value, ... }`. Every field of the variant must
  be initialized exactly once, and each value must have the field's declared
  type. There is no positional form and no trailing comma.
- Using a payload variant without braces, or constructing a payload-less variant
  with braces, is an error.

### 23.4 Matching: safe variant inspection

`match` is the only way to reach a variant's fields:

```text
match (expression) {
    IntegerLiteral(value) {
        print(value);
    }
    Binary(left, right) {
        print("binary");
    }
    Missing {
        print("missing");
    }
}
```

- The scrutinee must be an enum or union value; otherwise it is an error.
- Each arm names a variant and may bind its fields to fresh locals in
  parentheses, in field order, with the fields' declared types. The bindings are
  visible only inside the arm body.
- A match must have exactly one arm for every variant of the scrutinee's type,
  so it is **exhaustive**. A missing arm, a duplicate arm, an unknown variant,
  and a binding count that does not match the variant's field count are errors.
  There is no wildcard arm in this revision.
- Inside an arm, only that variant's fields are bound, so reading a field that
  belongs to a different variant is impossible: the language provides no other
  way to read a payload.
- There is no `.field` access on an enum or union value. Attempting it is an
  error that points at `match`.
- The scrutinee is evaluated once.

### 23.5 Recursive and mutually recursive types

Because a union value is a reference, a variant field may be of the union's own
type (`Expression left;`) or of another struct or union type, including types
declared later or in another module. Recursive and mutually recursive types are
therefore supported and require no manual memory management.

### 23.6 Zero values and uninitialized values

- A new union object is allocated and its tag set by its constructor, so every
  constructed union value is well defined.
- Where a union value is zero-initialized (for example an unassigned array
  element), its reference is null. Matching a null union reference is a fatal,
  deterministic runtime error ("match on a null union reference"), never
  undefined behavior.
- An enum is a value type. A zero-initialized enum (for example an unassigned
  array element) is the case with tag 0, which is the first case declared.
- An uninitialized local variable of any type is not diagnosed in this revision
  (Section 18.4).

### 23.7 Equality, printing, and conversions

- Enums support `==` and `!=`; unions, like structs and arrays, do not.
- `print` does not accept enum or union values. Print the fields or the result
  of a `match` instead.
- There is no conversion between an enum and an integer, and no way to obtain a
  variant's tag as a value.

### 23.8 Visibility and modules

- An `enum` or `union` declaration may be marked `public` or `private`, with the
  same rules as structs: the default is private (module-local) and `public`
  makes it visible to other modules (Section 22).
- A type's variants are visible wherever the type is; variants are not
  separately visible. A private enum or union referenced from another module is
  a private-access error.

### 23.9 C representation

- An enum is a C `int64_t` typedef, `ktn_e_<Module>Name`, together with one
  integer constant per case, `ktn_ec_<Module>Enum_Case`, numbered in declaration
  order.
- A union is a C struct, `ktn_s_<Module>Name`, holding an `int64_t` tag member
  and every variant's fields as separate, variant-prefixed members
  (`ktn_v_<Variant>_<field>`). A flat layout is used rather than a C `union` so
  that reading a field is never undefined behavior; only the active variant's
  members are read, always after the tag is checked.
- Each variant has a constructor, `ktn_new_<Module>Union_Variant`, which
  allocates through the GC, sets the tag, and then sets the fields.
- A match compiles to a `switch` on the tag. The tag is compared against the
  union's variant constants or the enum's case constants. Composite and imported
  types are mangled by module number like other top-level names (Section 22.6).

### 23.10 Deferred

The following are **not** implemented and must not be assumed:

- `match` as an expression, and a wildcard or default arm.
- Converting an enum to or from an integer, and iterating an enum's cases.
- Comparing unions structurally, and hashing or ordering variants.
- Variant payloads declared with anonymous struct types, tuples, or generics.
- A `switch` statement, `break`, and `continue` (unrelated to `match`).
- Exhaustiveness warnings when a type gains a variant in another module are
  errors rather than warnings, because matches are checked exhaustively.

### 23.11 Provisional decisions recorded

1. Enums are value types (`int64_t` tags); unions are GC-managed references.
2. Variant construction uses named fields, `Type.Variant { field = value }`, and
   a payload-less variant is written `Type.Variant`.
3. `match` is a statement and must be exhaustive, with no wildcard arm.
4. Payload fields are reachable only through `match` bindings; there is no
   field access on a union.
5. Matching a null union reference is a fatal runtime error.
6. Enums are comparable with `==`/`!=`; unions are not comparable, printable, or
   convertible to integers.
7. Union values use a flat C struct rather than a C `union`, trading memory for
   well-defined behavior.

---

## 24. Maps (provisional)

This section records the implemented string-keyed map. It is **provisional**:
the feature is the first working design and may evolve.

### 24.1 Type

- A map type is written `map[Key]Value`. The key type must be `string`; any
  other key type is an error.
- The value type may be any type except `void`: the primitive types, `string`,
  enums, structs, unions, arrays, and other maps. A `void` value type is an
  error.
- Map types match exactly. `map[string]int` and `map[string]float` are different
  types, and there are no implicit conversions between them.
- A map value is a **reference** to a GC-managed hash table, like a struct or a
  union. Assigning one map to another copies the reference.

### 24.2 Creation

`new map[Key]Value` creates an empty map:

```text
map[string]int counts = new map[string]int;
```

A freshly created map has length 0. A null map reference is never produced by
creation; where a map is zero-initialized (for example an unassigned array
element), its reference is null, and every map operation on a null reference is
a fatal runtime error.

### 24.3 Assignment: the only way to create an entry

```text
counts["total"] = 1;      // inserts a new entry
counts["total"] = 2;      // updates the existing entry, length unchanged
```

- `map[key] = value` inserts the entry when the key is absent and updates it when
  the key is present. The value type must match the map's value type exactly.
- This is the **only** operation that creates an entry.
- The map expression must be a mutable l-value, exactly as for an array element.

### 24.4 Reading and missing keys

```text
int total = counts["total"];   // fatal when "total" is absent
```

- `map[key]` reads the stored value and requires the key to be present. A
  missing key is a **fatal runtime error** ("map has no entry for key ..."), not
  a fabricated or uninitialized value. This is the deliberate consequence of the
  language having no optional or result type.
- A program checks membership before reading when a key may be absent.
- Compound assignment (`+=`, `-=`) and increment/decrement on a map entry are
  read-modify-writes, so they require the key to be present, just like a read.
- The map expression and the key expression are each evaluated exactly once.

### 24.5 Membership and length

- `has(map, key) -> bool` reports whether the key is present. It never fails for
  an absent key.
- `length(map) -> int` returns the number of entries.

### 24.6 Keys and equality

- Keys are compared by string **content**, not by pointer identity, so two
  distinct strings with identical contents address the same entry.
- A key may be the empty string; it is an ordinary key.
- Map keys and values are **retained, not copied**: the map stores a reference
  to the key string and copies the value's bytes. Kitten strings are immutable
  and garbage collected, so a stored key can never be mutated and stays alive as
  long as the map reaches it.
- Maps cannot be compared with `==` or `!=`; map equality is not supported.
- Maps cannot be printed.

### 24.7 Aliasing

Maps are references, so assignment and function passing copy the reference. A
mutation through one name is visible through every name that refers to the same
map, and a map stored in a struct field or array element is shared the same way.

### 24.8 Runtime representation

- A map is a C `ktn_map` value: an entry count, a capacity, a value size, and
  garbage-collected arrays of keys, hashes, and value bytes.
- The runtime uses **open addressing with linear probing** over a power-of-two
  table. Keys are hashed with FNV-1a over their bytes. Entries are never removed,
  so the first empty slot proves a key is absent.
- The table grows when inserting one more entry would exceed a three-quarters
  load factor, doubling its capacity and rehashing every existing entry, so no
  entry is lost.
- Values are stored inline as exactly `value_size` bytes each, where the size is
  the value type's C size. The compiler passes `sizeof(ValueCType)`, so no
  unsafe type punning is used and pointer-containing values (strings, structs,
  unions, arrays, nested maps) are traced correctly by the collector.
- Allocation sizes are checked for overflow; a negative or zero value size, an
  allocation that would overflow, or a table that is too large is a fatal
  runtime error.
- No manual memory management is required from Kitten programs.

### 24.9 Deferred

The following are **not** implemented and must not be assumed:

- Iteration over a map's entries or keys.
- Removing entries, clearing a map, or copying a map.
- Non-string keys.
- Ordered iteration, stable ordering, or any ordering guarantee.
- Optional or result types, and therefore recoverable missing-key handling.
- Map equality, hashing, or printing.
- Any performance guarantee beyond the implemented hashing scheme: the
  implementation has no deletion and no tombstones, and no benchmark establishes
  a bound.

### 24.10 Provisional decisions recorded

1. Keys are `string` only; the key type is still written in the type for clarity
   and future extension.
2. `new map[K]V` creates an empty map, mirroring `new T[n]` for arrays.
3. `map[key] = value` is the only operation that creates an entry.
4. Reading a missing key is a fatal runtime error; `has` is the membership test.
5. Maps are GC-managed references with shared mutation.
6. Values are stored inline by size, so any non-`void` value type is supported
   without type punning.
7. Keys are retained by reference; the byte contents are never copied and never
   mutated.
8. Growth doubles capacity and rehashes all entries; there is no shrinking and
   no removal.

---

## 25. Kitten-written lexer (experimental)

This section records a component that is written in Kitten itself and runs under
the Go bootstrap compiler. It is **not** the production front end: `purr build`
and `purr run` continue to use the Go lexer in `lexer/`, and Sections 3–8
remain the normative description of lexical structure. The section exists so that
the artifact, its deliberate differences, and its known limits are documented
rather than assumed.

### 25.1 Location and role

The sources are:

- `selfhost/lexer/tokens.ktn` — the token vocabulary: the `TokenKind` enum, the
  `Token` struct, the `LexResult` union, and the mapping from a kind to its name.
- `selfhost/lexer/lexer.ktn` — the scanner and the public `lex` function.
- `selfhost/lexer/main.ktn` — a driver program that reads a file and prints
  tokens.

`selfhost/lexer/README.md` describes how to build and run them. This is the first
component of the eventual self-hosting compiler (Section 15); the parser, the
checker, and the code generator are still written only in Go.

### 25.2 Fidelity to the reference lexer

The Kitten lexer reproduces the reference behavior described in Sections 3–8: the
same token kinds, the same token text, the same one-based line and column
positions, the same first-error behavior, and the same messages. A differential
test in `cmd/purr` compiles the Kitten lexer and compares it against the Go
lexer over a corpus of hundreds of inputs; the corpus, the comparison rules, and
the results are recorded in `selfhost/lexer/README.md`.

### 25.3 Token storage

A lexing pass stores its tokens in a `Token[]` whose length is
`length(source) + 1`, together with a separate count of populated entries. The
bound is exact: every token other than `EOF` consumes at least one source byte,
so a source of *n* bytes produces at most *n* such tokens, and the single `EOF`
token needs one more slot. Only the populated prefix is ever read, so no
uninitialized token is observed, and no slot is written twice.

### 25.4 Documented differences from the reference lexer

- **Positions are counted in bytes.** The reference lexer scans a `[]rune` and
  counts columns in code points (Section 3); the Kitten lexer scans bytes,
  because Kitten strings are byte sequences with byte length, byte indexing, and
  byte ranges (Section 20.6). For ASCII input the two agree exactly. For input
  containing bytes at or above 128, the column numbers after those bytes differ,
  and the "unexpected character" message names a byte (`'\xc3'`) where the
  reference names a decoded code point.
- **Embedded NUL bytes.** A Kitten string cannot contain a NUL byte
  (Section 20.1), so a source file containing one is truncated at that byte by
  `read_file`; the reference lexer would continue past it.
- **Fixed driver input and output.** The lexer driver still reads the fixed
  relative path `input.txt` and reports a lexical error on standard output,
  although the language now provides `argv()`, `eprint(...)`, and `exit(...)`
  (Section 18.8). Those driver capabilities are separate from the lexer itself.

None of these differences changes the language: they are properties of this
experimental implementation, and the reference lexer remains authoritative.

### 25.5 Performance

`length` and string indexing are constant-time, because the representation
stores the byte length next to the bytes (Section 20.6), so the scanner's work
grows **linearly** with the input size. Measured on the development machine by
running the compiled driver (which prints every token) with process start-up
subtracted, and the Go reference lexer on the same input:

| Source | Tokens | Go lexer | Kitten lexer |
| --- | --- | --- | --- |
| 1 KB | 253 | 0.08 ms | 0.3 ms |
| 4 KB | 995 | 0.3 ms | 2.4 ms |
| 16 KB | 3963 | 1.2 ms | 7.6 ms |
| 64 KB | 15821 | 5.6 ms | 31 ms |
| 256 KB | 63281 | 22 ms | 116 ms |

A fourfold input increases the Kitten lexer's time by a factor of about four,
which is linear within measurement noise. Scanning itself is now the smaller
part of the cost: a run that only lexes and prints the token count takes about
249 ms at 1 MB, while printing every token takes about 461 ms, so the driver's
output accounts for nearly half of the time on large inputs. The remaining
costs per token are its allocation (the token, the scan result, and the lexeme)
and the collector's work over them; all are linear in the number of tokens.

For comparison, the previous NUL-terminated representation, whose `length` and
indexing each scanned the whole string, took about 379 ms for 64 KB and 7.6 s
for 256 KB on the same machine, growing by a factor of about 13 to 20 for each
fourfold input. The length-carrying representation is therefore about 14 times
faster at 64 KB and about 65 times faster at 256 KB, and it removes the
quadratic term rather than merely reducing it.

### 25.6 Deferred

The following are **not** implemented and must not be assumed:

- Any part of the compiler other than the lexer: there is no Kitten-written
  parser, checker, or code generator.
- Parsing, or any check that the tokens form a valid program: the driver reports
  tokens and lexical errors only.
- Command-line arguments, standard error, and program-selected exit statuses.
- Code-point (UTF-8) aware positions and messages.

---

## 26. Kitten-written parser (experimental)

This section records a second component written in Kitten itself, alongside the
lexer of Section 25. It is **not** the production front end: `purr build` and
`purr run` continue to use the Go parser in `parser/`, and Section 17 remains
the normative description of the grammar. The section exists so that the
artifact, its design decisions, and its known limits are documented rather than
assumed.

### 26.1 Location and role

The sources are:

- `selfhost/parser/ast.ktn` — the abstract syntax tree types and the position
  helpers that compute each node's position.
- `selfhost/parser/parser.ktn` — the parser and the public `parse` function.
- `selfhost/parser/printer.ktn` — the canonical rendering of the tree.
- `selfhost/parse.ktn` — the driver, which reads a file, lexes it with the
  Kitten-written lexer, parses the tokens, and prints the result.

The driver is `selfhost/parse.ktn` rather than a file inside `selfhost/parser/`
because imports must resolve inside the directory of the file being compiled
(Section 22.4), so a driver that uses both the lexer and the parser has to sit
above both directories. `selfhost/parser/README.md` describes how to build and
run it. The parser parses one token stream and performs no module resolution,
exactly like the reference parser.

### 26.2 Fidelity to the reference parser

The Kitten parser accepts the grammar of Section 17, applies the precedence and
associativity of Section 17.3, and produces the same tree as the Go reference
parser: the same node kinds, the same fields, the same positions, the same
fail-fast diagnostics, and the same messages. A differential test in
`cmd/purr` compiles the parser and compares its rendering of the tree against
the reference implementation's rendering of the same source, over a corpus that
includes every grammar production, every precedence boundary, deeply nested
constructs, malformed input, every fixture, and the self-hosted sources;
`selfhost/parser/README.md` records the corpus and the results.

### 26.3 The abstract syntax tree

- The tree mirrors `parser/ast.go`: declarations, types, statements, and
  expressions, with a position for every node. It carries no semantic
  information and resolves no names.
- A node that needs a lexeme keeps the lexer's token, which already carries the
  kind, the text, and the position, so no lexeme or position is stored twice. A
  node's position is computed from the token that begins it, by the same rules
  the reference AST uses.
- **Absence is explicit.** Kitten has no null value: a local declared without an
  initializer is undefined rather than empty, and no literal denotes "no object".
  The optional parts of the grammar — an else branch, a variable initializer, a
  return value, a for-loop clause, a function's return type — are modelled as
  unions with an `Absent` case and a `Present` case, so a `match` must handle
  absence and no code path can forget it. A null or optional type in the language
  would be an alternative and is **deferred** (Section 16).
- Failure is a value: every step that can fail returns a result carrying either
  the value or a `ParseError`, and the first error wins, as in Section 17.4.

### 26.4 Documented differences

The parser introduces no difference of its own. Because the driver lexes with the
Kitten-written lexer, the differences recorded in Section 25.4 appear here too: a
column after a byte at or above 128 can differ from the reference lexer's
code-point column, and an error message that quotes such a byte names the byte.
The differential test therefore requires exact equality for input whose bytes are
all ASCII, and compares the AST shape and error positions for other input.

### 26.5 Performance

Parsing is roughly linear in the input size. Each AST list is allocated with a
capacity derived from the extent of the enclosing delimited group — the tokens up
to the first unmatched closing delimiter — rather than from the size of the file,
so a small list inside a large file does not allocate space proportional to the
whole file. Measured on the development machine, including the cost of rendering
the tree and starting the process, parsing takes about 15 ms for 11 KB, 111 ms for
109 KB, and 423 ms for 436 KB of generated declarations.

### 26.6 Deferred

The following are **not** implemented and must not be assumed:

- A Kitten-written semantic checker, code generator, or compiler driver. This
  section covers the parser and its tree only.
- Module resolution, import loading, and multi-file parsing.
- Recovery from a syntax error: the parser reports the first error and stops.
- A null or optional type in the language, which would replace the explicit
  `Absent`/`Present` unions of Section 26.3.

---

## 27. Kitten-written semantic checker (experimental)

This section records a third component written in Kitten itself, alongside the
lexer of Section 25 and the parser of Section 26. It is **not** the production
front end: `purr build` and `purr run` continue to use the Go checker in
`checker/`, and Sections 18 and 22–24 remain the normative description of the
rules. The section exists so that the artifact, its design decisions, and its
known limits are documented rather than assumed.

### 27.1 Location and role

The sources are:

- `selfhost/checker/model.ktn` — the data model: types, variant and struct
  definitions, and the optional wrappers.
- `selfhost/checker/state.ktn` — symbols, scopes, functions, the checker state,
  name and type resolution, and diagnostics.
- `selfhost/checker/expr.ktn` — expression checking.
- `selfhost/checker/stmt.ktn` — statement checking and the conservative
  control-flow analysis.
- `selfhost/checker/checker.ktn` — the five passes and the public entry points.
- `selfhost/check.ktn` — the driver.

`selfhost/checker/README.md` describes how to build and run it. The checker driver
still checks exactly the two modules it is given — `input.txt` as the root and
`input2.txt` as an import — so that cross-module checking can be exercised without
a loader. The Kitten-written loader under `selfhost/loader` discovers imports for an
absolute root and feeds the checker directly through `selfhost/loadunit.ktn`; its
documented exclusion is a relative root (Sections 22.2 and 22.4).

### 27.2 API

```kitten
public fn check_modules(Module[] modules, int module_count) -> CheckResult
public fn check_program(Module module) -> CheckResult
```

`Module` is the parser's `parser.Module` plus the sizes of the annotation tables,
which come from the parser's result. `CheckResult` is `Success`, carrying a
summary of the modules, named types, functions, and checked bodies together with
a `SemanticModel`, or `Failure`, carrying `ErrorKind`, file, position, and
message. Checking is fail-fast: the first error is returned, as in the reference.
The checker keeps no state between calls.

### 27.2.1 The semantic result

A successful check produces a `SemanticModel`: the facts a later phase needs so
that it can generate code without repeating name resolution or type checking. It
mirrors the reference checker's `Result`, which associates each fact with an AST
node:

| Fact | Reference | Kitten |
| --- | --- | --- |
| An expression's type | `ExpressionTypes` (by node) | `ExprInfo.type` at the node's id |
| A variant reference | `VariantRefs` (by node) | `ExprInfo.variant` |
| A resolved name | `Symbols` (by identifier node) | `ExprInfo.symbol`, which also carries the callee's signature and built-in |
| A declared type reference's type | `TypeRefs` (by node) | `ModuleModel.type_refs` at the node's id |
| A named declaration's type | `TypeDecls` (by node) | `ModuleModel.types`, keyed by name |
| A function declaration's signature | `FunctionDecls` (by node) | `ModuleModel.functions`, keyed by name |
| Types and functions visible to the root module | `Types`, `Functions` | `visible_types`, `visible_functions` |
| The built-ins | `Builtins` | `builtins` |

A declaration is keyed by name rather than by node because duplicate names in one
module are rejected, so a name identifies a declaration exactly as its node does;
this is the same lookup the checker itself uses (Section 27.3). An absent entry
means the same thing as an absent map entry in the reference: the node was not
annotated, which is why an expression that is never checked, such as the type
name in a qualified variant reference, has no type.

### 27.2.2 Node identity

Associating a fact with a node requires a key that survives nesting, repetition,
and multiple modules. The reference uses the node's address, which Kitten cannot
compare or hash. Three alternatives were considered:

- **Annotate the AST nodes with a stable integer id**, assigned by the parser at
  construction: chosen. A consumer reads the id from the node it is already
  walking and indexes the model, with no ordering assumption and no dependence on
  source coordinates.
- **Key by source position**, possibly with a kind: rejected, because several
  nodes share a position — a member access and its receiver start at the same
  column, as do a call and its callee — and positions are not semantic identity.
- **Parallel arrays in traversal order**: rejected, because it would couple a
  consumer to the checker's internal visit order, so any change to that order
  would silently misassociate facts.

Ids are assigned per module and are unique within one parse: the parser reports
how many it assigned, and the checker sizes its tables exactly. A node's id is
therefore meaningful together with its module, which a per-module generator
naturally has.

### 27.3 Types, symbols, and scopes

- Named types carry an integer id and are compared by it; array and map types are
  compared through their parts. The reference interns composite types and compares
  every type by pointer, which yields the same answers: composite types are equal
  exactly when their parts are, and named types are equal exactly when they are
  the same declaration.
- Absence is a value: an absent type is the `None` kind, and an absent payload or
  parent is the `Absent` case of an optional union. Kitten cannot construct a
  struct value recursively, so optional payloads are unions rather than
  placeholder objects.
- Scopes are the reference's: a scope is a name table plus an optional enclosing
  scope, a declaration is refused when the name is already declared in that same
  scope, and a lookup walks outward, which is what allows shadowing.
- A declaration is resolved by its module and name rather than by AST node
  identity, which Kitten cannot key a map by; duplicate detection makes the pair
  exact.

### 27.4 Rules implemented

The checker performs the reference's five passes in the reference's order —
collect declarations, resolve struct fields, resolve union variant fields,
resolve function signatures, check bodies — each walking the modules and each
module's declarations in source order, and reports the first error. Every rule of
Sections 18 and 22–24 that the reference enforces is implemented with the same
diagnostic at the same position, including declaration and duplicate detection,
name and type resolution, private-access detection, `void` where a value type is
required, non-`string` map keys, function signatures and calls, struct literals
and field access, enum and union variant references and construction, `match`
exhaustiveness and bindings, scoping and shadowing, assignment and compound
assignment, every unary and binary operator, arrays, maps, strings, the built-ins,
conditions, `return`, and the conservative return-path analysis.

The reference's guards against a nil node (a nil declaration, identifier, field,
parameter, or arm, an unsupported node kind, or a function without a body) are not
implemented, because a tree produced by the parser can contain neither a nil node
nor a missing block, so those branches are unreachable for the reference as well.

### 27.5 Differential testing

A differential test in `cmd/purr` compiles the checker and compares its
rendering — the outcome, the first error's category, position, and message, and
the declaration counts on success — against the reference implementation's
rendering of the same input, over a corpus that includes every construct, every
operator and assignment matrix, every collection operation, malformed input,
cross-module cases, every fixture, and the self-hosted sources.
`selfhost/checker/README.md` records the corpus and the results.

### 27.6 Deferred

- **Code generation.** The semantic result of Section 27.2.1 carries every fact
  the reference generator reads. The first layer of a Kitten-written generator —
  naming, the C type mapping, escaping, and output accumulation — and the emitters
  that consume this result are described in Section 28. The contract they rely on is
  documented in `selfhost/checker/README.md`.
- Command-line arguments, standard error, and a program-selected exit status
  (Section 16).
- Recovery from an error: the parser and the checker both report the first error
  and stop.

---

## 28. Kitten-written C generator (experimental)

Section 19 describes the compilation model, and Section 27 describes the
Kitten-written checker that resolves a program. This section describes the
Kitten-written code generator: the layer that turns resolved names and types into
C text and the bounded accumulation it writes into, the declaration emitter, the
expression emitter, the string literal pool, the statement and function-body
emitter, and the translation unit assembly. The Kitten-written module loader under
`selfhost/loader` supplies the module set for an absolute root path; its README
records the exact parity and the two exclusions.

### 28.1 Location and role

The sources are:

- `selfhost/codegen/names.ktn` — name mangling, the C type mapping, and
  string-literal escaping, mirroring `codegen/names.go` and `codegen/cstring.go`.
- `selfhost/codegen/output.ktn` — bounded output accumulation and finalization.
- `selfhost/codegen/errors.ktn` — the failure type every stage reports.
- `selfhost/codegen/declarations.ktn` — the declaration emitter.
- `selfhost/codegen/expressions.ktn` — the core expression emitter.
- `selfhost/codegen/literals.ktn` — the string literal pool.
- `selfhost/codegen/statements.ktn` — the statement and function-body emitter.
- `selfhost/codegen/unit.ktn` — the entry point rule and the translation unit
  assembly.
- `selfhost/codegen.ktn` — the helper probe driver.
- `selfhost/declarations.ktn` — the declaration driver.
- `selfhost/expressions.ktn` — the expression driver.
- `selfhost/functions.ktn` — the function driver.
- `selfhost/unit.ktn` — the unit driver.

`selfhost/codegen/README.md` describes how to build and run each driver, the
directive interface the helper probe exposes, and the measured costs. The pool has
no driver of its own: it is exercised through the expression driver, which prints
the declarations its renderings produced.

Five drivers exist because the layers are reached differently. The helper probe
reads directive lines from the fixed relative path `input.txt` and prints one
result line per directive, so that every helper is reachable without a program to
walk. The declaration driver reads the two module paths the checker's driver
reads, runs the same front end, and prints the declaration section. The expression
driver reads the same two modules and prints the rendering of every expression in
their function bodies, one line per expression node. The function driver reads the
same two modules and prints the pooled declarations and the function definitions
those bodies produced. None is the finished compiler stage, which will emit a whole
translation unit for a program.

### 28.2 API

`names.ktn` exposes the reserved prefixes and standalone generated names, the
derived name forms, `c_type`, and `c_string_literal`:

- `c_type(type) -> CTypeResult` where `CTypeResult` is `Ok { text }` or
  `Failed { message }`. The absent type has no C representation and maps to
  `Failed`; the message carries no position, because it is the caller that knows
  where the type was written.
- `c_string_literal(literal) -> CStringLiteral` where `CStringLiteral` is
  `{ escaped, length }`. It has no failure case: the reference decoder reports an
  error for an unsupported escape, but the lexer rejects every unsupported escape
  before the parser or the generator sees the token, so decoding cannot fail. The
  helper decodes with the parser's `decode_string`, which is exported for it, so
  that the two decodings cannot drift.

- `header_file_name()` and `alloc_function()` are the two runtime interface names
  the declaration sections mention: the header they include and the allocation the
  generated constructors call.

`output.ktn` exposes `new_output`, `output_write`, `output_indent`, `output_line`,
each returning `OutputResult`, and `output_text`.

Nothing in this layer inspects a program, resolves a name, or infers a type: every
helper maps a name, a module, a resolved type, or a literal token to C text. The
declaration emitter's own API is in Section 28.6, and the core expression
emitter's in Section 28.7.

### 28.3 C type mapping and name mangling

The mapping and the mangling are the ones in Section 19.1 and Section 19.7,
derived from the reference generator rather than restated from the language:
primitives map to `int64_t`, `double`, `bool`, `const ktn_string *`, and `void`;
a struct or union maps to a pointer to `ktn_s_<module><Name>`; an enum maps to the
typedef `ktn_e_<module><Name>`; and an array or a map maps to `ktn_array *` or
`ktn_map *`. The root module is unmangled and an imported module is qualified by
its id, which cannot collide with a root-module name because a Kitten identifier
cannot begin with a digit.

### 28.4 String literal escaping

A literal token is decoded and its bytes are re-encoded for C: quote, backslash,
newline, tab, carriage return, and question mark become their escape; printable
ASCII is emitted verbatim; every other byte becomes a three-digit octal escape,
which cannot merge with a following digit. The reported length is the number of
decoded bytes, excluding the terminator C appends.

### 28.5 Output accumulation

Kitten strings are immutable, so appending to a growing buffer costs the size of
the output per append. Output is therefore accumulated as a bounded array of
chunks — one chunk per completed line — and finalized by joining the chunks in
halves, which copies the text once per level of recursion. The caller sizes the
chunk array, and capacity is tested before any state changes, so a line that does
not fit is rejected whole rather than half recorded: `OutputResult.Overflow`
means the write did not happen and the caller must abandon generation.

`selfhost/codegen/README.md` records the measurements: balanced joining grows
linearly, where joining left to right grows quadratically.

### 28.6 The declaration emitter

`selfhost/codegen/declarations.ktn` emits the sections the reference generator
writes before any function body, in its order and with its spacing: the
generated-file comment and runtime include, forward typedefs, enum declarations,
union tag constants, function prototypes, struct definitions, union definitions,
struct constructors, and union constructors. `selfhost/declarations.ktn` is its
driver: it reads two modules, runs the Kitten front end, and prints the section.

The API is `emit_declarations(model)`, which sizes the output exactly, and
`emit_declarations_with_capacity(model, capacity)`, which emits into a chosen
size so that a capacity known to be too small can be supplied and its failure
observed. A result is either the section with its line and capacity counts, or a
failure carrying a position and a message. A declaration the checker did not
record is a failure, never a declaration: the emitter never turns a missing
annotation into plausible output, and an emission that cannot be completed
produces no section at all.

Every name comes from Section 28.3 and every type from the semantic model of
Section 27.2.1. The emitter resolves no name, infers no type, and reads no source
text.

**Capacity.** The output accumulator of Section 28.5 is bounded, so the number of
lines the section will need must be known before it is written. The emitter runs
the same traversal twice: a dry run counts the lines through the same calls that
write them, and the real run is given exactly that many chunks. The count is exact
because one emitted line is one chunk — a line ends with a newline, and a newline
completes exactly one chunk — and because every section rule, including whether a
section is followed by a blank line, is decided by the same line counter the dry
run advances.

### 28.7 The core expression emitter

`selfhost/codegen/expressions.ktn` renders the expressions the reference generator
handles with a recursive walk and parentheses: integer, floating-point, boolean, and
string literals; identifiers; unary `-`, `!`, `++`, and `--`; binary `+`, `-`, `*`,
`/`, `%`, `<`, `<=`, `>`, `>=`, `==`, `!=`, `&&`, and `||`; calls; member access and
qualified variant references; struct and variant literals; array and map creation;
indexing; and assignment. Every unary and binary operation is parenthesized, so the generated C never
depends on C's precedence matching Kitten's. A string literal renders as a reference to
its pooled object, described in Section 28.8.

A literal is emitted as its source spelling, exactly as the reference emits it, so
no rounding, no exponent form, and no change of precision can be introduced by
generation; the language defines no exponent notation and no form that C would
spell differently. An identifier is emitted as a variable name and needs no lookup,
because the checker rejects a name that is not a local variable or parameter in
expression position.

Three binary operators are string operations: `+` concatenates and `==` and `!=`
compare content, through the runtime helpers, because a Kitten string is a
length-carrying pointer. The left operand's resolved type decides that, and it is
the only semantic fact the emitter reads, which mirrors the reference exactly.

The API is `emit_expression(context, expression)`, where the context carries the
semantic model and the index of the module the expression belongs to: the index
selects the module's annotations, and the module's id, which the model carries, is
what names are mangled by. A result is the rendering or a failure, and a failure is
propagated from the expression that contains it, exactly as the reference reports a
child's error from its parent, so a partly rendered expression is never returned.

**Calls** are resolved through the checker's annotations rather than from source
text: the callee's symbol names the function it resolved to, and its built-in field
distinguishes a built-in from a user function. A user call becomes
`<mangled name>(<arguments>)`, with the name mangled by the module that declared the
resolved function. The built-ins are expanded: `print` and `eprint` become comma
expressions of the runtime helpers for each argument's type, separated by the
space helper and terminated by the newline helper, which has type void and is the
built-in's own type; `length` becomes the array, map, or string length helper
chosen by the argument's type; and `has`, `substring`, `read_file`, `argv`, and
`exit` are one runtime call each. Every prelude built-in is a call to its
non-static generated helper, which builds the result union from the runtime's
success flag and payload or message. Arguments
are rendered left to right and the first failure among them stops the call, so a call
is never emitted with a hole where an argument failed. The built-ins whose argument
is a container choose their length or membership helper from the argument's type, and
their argument can be constructed in the same expression because `new` is rendered.

**Member access** renders as `<receiver>->ktn_f_<field>`, unless the checker recorded
the member as a qualified variant reference, in which case an enum case renders as its
tag constant and a payload-less union variant renders as a call to its constructor.
**Struct and variant literals** render as calls to the generated constructors, with
their values reordered into field declaration order, which is also the order their
string literals are pooled in. **Array creation** renders as a runtime allocation
whose element size is the element type's C size, and **map creation** as an empty map
whose values are stored inline at the value type's size. **Indexing** renders as a
runtime accessor: a string index yields a byte value, and an array or map index
dereferences the accessor's pointer, which is a C l-value, so a target and index are
evaluated once whether the index is read or written.

**Assignment** renders a plain map-entry target through the runtime setter, which
inserts or updates the entry and passes the value by address through a compound
literal; every other plain assignment and every numeric compound assignment keeps
the reference's parenthesized C form. A string `+=` becomes the runtime append applied
to the address of the target, so an index target is evaluated once. Increment and
decrement are ordinary unary expressions over the same l-value forms and need no
separate path.

An expression is a fragment, not a line, so it is returned as a string rather than
written through the output accumulator of Section 28.5. Rendering a node
concatenates its children's text, which is the reference's own shape: for a chain
of depth *d* it copies about *d* squared characters in total. Matching the
reference byte for byte means inheriting that cost, and the differential suite
measures it rather than hiding it.

### 28.8 The string literal pool

`selfhost/codegen/literals.ktn` holds the string literals a translation unit uses.
A literal is interned where it is rendered, so the entries are in first-use order,
and two literals that share a C spelling share one object, which is exactly the
reference's rule. The order is the reference's rendering order, which is source
order for every construct except a struct or variant literal: the reference renders
those values in field declaration order rather than source order, so a literal
written last there can be the first pooled entry.

The pool names its objects `ktn_literal_<n>`, one-based, using the helper of
Section 28.3, and expression emission refers to one as `(&ktn_literal_<n>)`.
`literal_pool_text` returns the declarations, each line built from the escaped form
of Section 28.4:

	static const ktn_string ktn_literal_<n> = { <length>, "<escaped>" };

with one blank line after them, and the empty string for a program with no
literals. In a generated file they belong after the constructors and before the
function bodies, which is where the reference writes them; `literal_pool_lines`
reports how many lines they occupy, so a caller that writes them into an output of
its own can size that output exactly.

The pool is bounded and the caller sizes it. A sound bound is the number of
expression nodes in the modules being generated, because every literal is an
expression node and the parser reports how many it assigned; the expression driver
sums each module's count. A literal that does not fit is reported at the literal,
and the entries that did fit are the reference's entries in order: a pool that
cannot hold every literal would refer to objects that were never declared, so the
caller must abandon generation.

Two states cannot arise and are therefore not tested: a missing entry, because
resolving a literal interns it, and an overflow of the declarations' own output,
because that output is sized from the pool.

### 28.9 The statement and function emitter

`selfhost/codegen/statements.ktn` renders the body of every function.
`emit_function_bodies(model, pool)` returns the whole body section - one definition
per function declaration, in module and source order, each followed by a blank line
- or a failure, which means no bodies were produced. The string literals the bodies
use are interned into the pool the caller supplies, in the order the reference
interns them.

The statements are a block, a local declaration, an expression statement, `if` with
its `else` and `else if` forms, `while`, `for`, `return`, and `match`. Braces,
indentation, blank lines, and parentheses are the reference's. The language defines
no `break`, `continue`, or `goto`, so no statement emits one; the `break;` a `match`
case ends with is the reference's, and so is the `default:` arm that fails through
the runtime, which the checker's exhaustiveness rule makes unreachable.

A condition keeps the parentheses of a compound expression and gains them when it
is a bare identifier or a literal, which is the reference's rule; a condition that is
an assignment keeps an extra layer of parentheses, because C's `-Wparentheses` warns
about an assignment used as a condition without them.

A `match` evaluates its scrutinee once into a temporary named `ktn_match_<n>`,
numbered across the whole unit in emission order, checks a union scrutinee for null
before reading its tag, and turns each arm into a `case` that binds the variant's
fields, casts each unused binding to void, emits the arm's statements, and breaks.

A statement whose expression the expression emitter cannot render is reported with
that expression's failure and produces no bodies, so an incomplete body can never be
reported as rendered. The body section is not a translation unit: a
unit is the declaration sections of Section 28.6, then the pool of Section 28.8,
then these bodies, then the entry point, which is deferred.

**Capacity.** A body's line count is only known once it has been rendered, so the
module renders twice: a dry run counts the lines through the same calls that write
them, and the real run is given exactly that many chunks. The two runs are the same
traversal with the same rules, and the match temporaries are numbered identically
because the counter starts at zero in each run.

### 28.10 The translation unit

`selfhost/codegen/unit.ktn` renders a complete C translation unit:
`emit_translation_unit(model)` returns `Success { text; lines }` or a failure, and a
failure means no unit was produced.

The sections are the reference's, in its order and with its blank lines: the
generated-file comment, the `#include` of the runtime header, and a blank line,
followed by the declaration sections of Section 28.6; the pooled literal
declarations of Section 28.8, with one blank line after them or nothing at all for a
program with no literals; one definition per function from Section 28.9, each
followed by a blank line; and the C entry point, which calls the runtime
initialization, saves `argc`/`argv` for `argv()`, then calls the root module's
`ktn_fn_main` and returns 0.

**The entry point rule** runs before anything is emitted, exactly as the reference
runs it, so a program with both an invalid entry point and an expression the emitter
cannot render reports the entry point, and a rejected program produces no C. The rule
requires a zero-parameter, void-returning `main` in the root module, which is the
module at index zero; an imported module's `main` is never an entry point, and the
compiler never invents one. The failures are, with the root module's path as their
file: `program has no entry point: define fn main() in the root module` at 1:1,
`entry point main must take no parameters`, `entry point main must not return a
value, but returns <type>`, and `entry point main was not checked`. The entry point
rule is the only failure that carries a file; every other failure belongs to the
module being emitted and reports its position alone, as in the reference.

**The pool's dependency on the bodies** is why the bodies are rendered before the
text is assembled even though the pool precedes them in the file: a literal is
interned where it is rendered. The unit emitter renders the bodies exactly once and
concatenates the parts once each; rendering them again to discover the pool would
render every expression twice and intern every literal twice.

### 28.11 Differential testing

The differential tests in `cmd/purr` compare the Kitten-written generator with
the reference. The helper test compares values the reference produced rather than a
copy of its logic: for every literal of the corpus the harness reads the pooled
declaration out of the reference's own output and compares both the escaped text
and the byte length, and every predicted name and complex C type spelling is
required to appear in reference C for a program that uses it.

The declaration test compares the section itself, byte for byte. The reference
emits a whole translation unit, so the harness separates the section
structurally: it ends at whichever comes first of a pooled literal declaration and
a function definition, neither of which can be confused with a constructor or
depends on a literal being present. Programs the reference rejects for its
entry-point rule, which this phase does not implement, are verified by deriving
the section instead: prepending an empty `main` adds exactly one prototype line
and nothing else.

The expression test compares renderings node by node. The driver prints one line
per expression node in emission order, and the harness walks the reference AST in
that same order and pairs the two lists, so a wrong node, order, position, or kind
cannot pass. A rendering is compared when the reference's own output contains it in
an extractable position, which for this phase is a print argument: the balanced
parenthesized text after a print helper's opening parenthesis, in emission order.
Every expression kind is implemented, so the emitter's failure message no longer
describes a remaining kind.

The pool is compared as well: the reference writes the declarations between the
declaration sections and the first function body, and the harness compares exactly
those lines, together with the expression lines that produced them. The function
harness compares whole definitions: the pool and the bodies the probe rendered
against the same two regions of the reference's own output, byte for byte. The unit
harness compares the complete translation unit, one comparison per program covering
the header, the declaration sections, the pool, the bodies, and the entry point at
once. The call harness compares whole units for programs whose expressions are calls
and, because such a program can now print, compiles, links, and runs both generated
programs and compares what they write, their standard error, and their exit status. A
program the entry point rule rejects is compared through its error, so it is never
counted as a unit comparison. The fixture harness hands every top-level fixture under
`tests/` to the Kitten backend: each fixture must match the reference's unit and
produce its expected output when compiled and run.

`selfhost/codegen/README.md` records the corpora and the results.

### 28.12 Deferred

- **Writing and building the unit.** The unit driver prints the complete C to
  standard output; nothing writes a file, invokes a C compiler, or sets a process
  exit status, because the self-hosted runtime offers none of those.
- **Relative root paths.** The Kitten-written loader reports missing, unreadable,
  and directory paths as loader errors, but still requires an absolute root
  because the self-hosted runtime has no working directory (Sections 22.2 and
  22.4).
- Driver integration of process launch and the file-writing and directory
  operations; the self-hosted driver does not use them.

Kitten is not self-hosting. The lexer, the parser, the checker, the module loader,
and the generator's helper, declaration, expression, literal-pool, statement, and
unit layers are written in Kitten, and every expression kind the language has is now
emitted; but relative roots still need a language decision, and production
integration, the pipeline that writes and compiles the output, and the production
compiler are Go. The Go compiler must therefore remain
the bootstrap reference until the Kitten-written compiler can compile its own source
through a Kitten-written pipeline and reproduce the same behavior.


---

## 29. Kitten-written `purr` sidecar (experimental)

`purr.ktn`, `purr/cli.ktn`, `purr/build.ktn`, and `purr/project.ktn` are a
Kitten-written sidecar command, built and tested as `purr-kt`. The Go CLI remains
the authoritative `purr`. The sidecar supports `help`, `new`, `build`, and `run`,
including current-directory binary project mode. Library project builds remain
deferred.

`build` links the Kitten-written loader, lexer, parser, checker, and generator
in process, discovers `CC` or `cc`/`gcc`/`clang` with PATH rules matching Go's
`exec.LookPath` (relative PATH results are rejected as Go's `ErrDot`), reads
`pkg-config bdw-gc` flags, writes the generated C and embedded runtime to a
private temp tree, invokes the C compiler, and validates the output.

`run` builds into a `purr-run.` temporary directory, executes the program with
inherited standard streams, propagates its status, and cleans up
non-recursively.

`new` accepts the 9.7 argument grammar. `-h` and `--help` are recognized
anywhere in the `new` argument list, print the dedicated project-scaffolding help
to stdout, and exit 0 without touching the filesystem; every other argument is
ignored when help is requested. The help text is:

```text
Kitten project scaffolding

Usage:
  purr new [--lib] <name>

Options:
  --lib    Create a library project instead of a binary
  -h, --help
```

Without help, `new` still requires exactly one name, accepts `--lib` once, and
reports usage errors on stderr with exit 2.

### 29.1 Project mode

Project mode activates only for `purr-kt build` or `purr-kt run` with no source
argument, no `-o`/`--output` option, and a `purr.toml` in the **current
directory**. There is no parent-directory search. The manifest is parsed with
the strict subset parser in `purr/manifest.ktn`, and exactly one of
`src/main.ktn` (binary) or `src/lib.ktn` (library) must exist.

For a binary project named by the manifest:

- `purr-kt build` writes `target/<name>`, creates `target` with mode `0755`
  (subject to the umask) when absent, and prints `built target/<name>`.
- If `target` exists and is not a directory, the build fails with
  `purr build: target is not a directory`; the existing path is untouched.
- `purr-kt run` builds the same target and executes it with inherited standard
  streams, propagating the child's status. The built target remains.
- A failed build removes a partial output that this invocation created and
  removes a `target` directory that this invocation created once it is empty.
  A pre-existing target, its contents, and a pre-existing output are never
  removed.

A library project is deferred: `purr-kt build` reports
`purr build: library builds are not implemented yet` and `purr-kt run` reports
`purr run: library projects cannot be run`; neither creates `target`.

Project-discovery failures use the command prefix and exit 1:

- a manifest read or parse failure reports the runtime or parser message after
  `purr build: ` or `purr run: `;
- both source files report `project has both src/main.ktn and src/lib.ktn`;
- neither source reports `project has neither src/main.ktn nor src/lib.ktn`.

Without `purr.toml`, the existing `missing source file` usage error and status 2
are preserved. An explicit source argument, or any output option without a
source, retains source-mode parsing and never enters project mode.

The sidecar intentionally differs from the Go CLI in four tool-behavior details:

- combined compiler output is captured stdout followed by captured stderr,
  without a separator; real GCC and Clang put diagnostics on stderr only;
- a compiler killed by a signal reports `exit status <128+n>` rather than Go's
  `signal: <name>`;
- the private temp directory is named `purr-build.<suffix>` rather than
  `purr-build-<digits>`;
- cleanup is non-recursive and reports leftovers as
  `note: could not remove ...` lines instead of Go's silent `RemoveAll`.

The compiler is executed through a fixed `/bin/sh` `cd` plus `exec` wrapper so
its working directory matches Go's build-tree `command.Dir`; the compiler
argument vector is unchanged. These differences do not change the language.
