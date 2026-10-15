# Kitten Language Specification — v0.1 (Provisional)

Status: **Provisional.** This document describes the agreed direction for the
Kitten language. Syntax in this document is fixed for Phase 1. Semantics marked
as deferred are not decided and must not be assumed by any component.

This revision covers the language surface and its static semantics. The lexer,
parser, and checker are implemented; the code generator and runtime are not.
Where this document mentions behavior that does not yet exist, it describes
intent, not current capability.

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
- Whether a single file may contain multiple modules or whether modules span
  files is **deferred**. Nothing in this revision depends on module structure.

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
| Keywords | `Struct`, `If`, `Else`, `While`, `For`, `Return`, `Fn`, `Public`, `Private` |
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
fn public private
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

`public` and `private` are visibility modifiers. Where they are required, what
they default to, and which declarations they apply to are **deferred**
(Section 16).

---

## 6. Literal syntax

### 6.1 Integer literals

Integer literals are sequences of decimal digits, for example `0`, `123`, `42`.
A leading minus sign is not part of the literal; it is the unary minus operator.
An `int` value is a signed 64-bit integer (Section 18.1).

Not supported in this revision (and therefore not to be added without a
specification change): hexadecimal, binary, and octal forms; digit separators;
numeric suffixes; exponent notation.

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
decoding. String representation, indexing, and runtime operations are
**deferred**.

An unterminated string, a newline inside a string, or an unsupported escape is
reported as a lexical error at a specific `line:column`. A trailing backslash
reaching end of input is reported as an unterminated string.

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
- Square brackets `[` and `]` are reserved for a possible future generic
  parameter syntax such as `[T]`. Generics are not implemented in this
  revision.
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
- Visibility modifiers `public` and `private` may appear on function
  declarations. Their exact placement, defaults, and effect are **deferred**.
  Provisionally (Section 17), the parser accepts a single optional modifier
  before a top-level `fn` or `struct`; it does not accept modifiers on struct
  fields or local variables.
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
- The formatting rules — how multiple arguments are separated, how values are
  rendered, and whether a trailing newline is emitted — are **deferred** to the
  runtime and code-generation phases. The `.ktn` fixtures under `tests/`
  document the intended results.
- `print` is not executable in this phase: the checker validates calls to it,
  but no code is generated and nothing runs.

---

## 14. Memory-management direction

- Kitten is garbage-collected. Ordinary programs allocate objects and strings
  and never free them manually.
- The planned initial collector is the **Boehm–Demers–Weiser conservative
  garbage collector** (Boehm GC, `libgc`), linked into compiled programs.
- The compiler is planned to generate C, which a system C compiler then compiles
  and links against Boehm GC to produce a native executable.
- The precise collector configuration, finalization behavior, and any
  distinction between stack, heap, and static storage are **deferred**
  (Section 16).

---

## 15. Compiler architecture

The compiler is written in Go and is organized into clearly separated packages,
one per phase:

| Package | Responsibility | Status |
| --- | --- | --- |
| `lexer` | Source text → tokens | Implemented (Phase 1) |
| `parser` | Tokens → syntax tree | Implemented (Phase 2A) |
| `checker` | Semantic analysis and type checking | Implemented (Phase 2B) |
| `codegen` | Checked tree → C source | Not implemented |
| `runtime` | Support code linked into compiled programs, including GC integration | Not implemented |

Supporting layout:

- `cmd/kitten` — the `kitten` command-line entry point. It recognizes
  `kitten build <file.ktn>` and `kitten run <file.ktn>`. Neither command is
  implemented; each reports that clearly instead of pretending to succeed. The
  checker is not yet wired into the command line.
- `tests/` — `.ktn` programs and matching `.expected` output files. They are
  lexer, parser, and checker inputs today and executable-program tests in later
  phases.

Pipeline (intended): source → lexer → parser → checker → code generator (C) →
system C compiler → link with Boehm GC → native executable. The lexer, parser,
and checker are implemented; the remaining stages are not.

Phase boundaries are strict: a package must not take on another phase's
responsibility, and later phases are not started without explicit authorization.

---

## 16. Deferred decisions and unresolved semantics

This revision deliberately leaves the following undecided. None of them may be
invented silently in code. When a decision is needed, raise it and record the
outcome here.

**Types and numbers**

- Exact bit widths of `int` and `float` are now provisionally fixed as signed
  64-bit and IEEE 754 binary64 (Section 18.1). Integer overflow behavior and
  runtime rounding remain **deferred** to the code-generation and runtime
  phases.
- Numeric conversions and promotion rules: **no implicit conversions** exist
  (Section 18.2). Explicit conversion syntax is **deferred**; there are no casts
  yet.
- Whether additional numeric types (for example sized or unsigned integers)
  exist.

**Strings**

- In-memory representation, mutability, indexing, and slicing.
- Runtime operations (length, concatenation, comparison) and their semantics.
- Whether string interpolation or raw/multiline strings exist.

**Visibility and declarations**

- Default visibility for top-level declarations and struct members. The parser
  and checker accept `public`/`private` on top-level `fn` and `struct`
  (Section 17.4), but the checker gives visibility no semantic effect yet; it is
  recorded and ignored.
- Exactly which declarations accept `public` and `private`, and where the
  modifier is written, beyond top-level `fn` and `struct`.
- Whether `fn main() -> void` is accepted in addition to `fn main()`: **yes**,
  both are accepted and equivalent (Section 18.6). Entry-point requirements and
  startup behavior remain **deferred** to the compilation phase.
- Whether a variable initializer is mandatory: **it is optional**
  (Section 18.4). Reading an uninitialized variable is not diagnosed.
- Whether reading an uninitialized variable should be an error.

**Functions and types**

- Function overloading and method semantics.
- Default and variadic parameters.
- Generics and other advanced type-system features. Square brackets `[` `]` are
  reserved for a possible future `[T]` syntax.
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
- The full set of statement forms.

**Modules and program structure**

- Imports and modules, including whether a file maps to a module.
- Entry-point requirements and program startup.

**Errors, concurrency, and runtime**

- Error handling.
- Concurrency.
- The precise behavior of the garbage collector and runtime, and the concrete
  Boehm GC configuration.

**Lexical clarifications deferred**

- Whether identifiers may contain non-ASCII letters. The current lexer accepts
  ASCII letters only.
- Handling of a byte-order mark at the start of a file.
- Whether block comments will ever be added.

**Implementation status**

- The parser is implemented (Phase 2A) and accepts the grammar in Section 17.
- The checker is implemented (Phase 2B) and enforces the rules in Section 18.
- The code generator and runtime are not implemented.
- The `.ktn` fixtures lex, parse, and check successfully but do not execute;
  the lexer, parser, and checker process them, and no native code is produced.

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

declaration      = [ visibility ] ( function | struct ) ;
visibility       = "public" | "private" ;

function         = "fn" IDENT "(" [ parameters ] ")" [ "->" type ] block ;
parameters       = parameter { "," parameter } ;
parameter        = type IDENT ;

struct           = "struct" IDENT "{" { field } "}" ;
field            = type IDENT ";" ;

type             = "int" | "float" | "bool" | "string" | "void" | IDENT ;

block            = "{" { statement } "}" ;

statement        = block
                 | if
                 | while
                 | for
                 | return
                 | variable_declaration
                 | expression_statement ;

if               = "if" "(" expression ")" block [ "else" ( if | block ) ] ;
while            = "while" "(" expression ")" block ;
for              = "for" "(" [ for_init ] ";" [ expression ] ";" [ expression ] ")" block ;
for_init         = variable_declaration | expression ;
return           = "return" [ expression ] ";" ;

variable_declaration = type IDENT [ "=" expression ] ";" ;
expression_statement = expression ";" ;

expression       = assignment ;
assignment       = logical_or { ( "=" | "+=" | "-=" ) logical_or } ;   // right-associative
logical_or       = logical_and { "||" logical_and } ;
logical_and      = equality { "&&" equality } ;
equality         = relational { ( "==" | "!=" ) relational } ;
relational       = additive { ( "<" | "<=" | ">" | ">=" ) additive } ;
additive         = multiplicative { ( "+" | "-" ) multiplicative } ;
multiplicative   = unary { ( "*" | "/" | "%" ) unary } ;
unary            = ( "-" | "!" | "++" | "--" ) unary | postfix ;
postfix          = primary { call | member | "++" | "--" } ;
call             = "(" [ arguments ] ")" ;
arguments        = expression { "," expression } ;
member           = "." IDENT ;
primary          = INT | FLOAT | STRING | "true" | "false"
                 | IDENT
                 | struct_literal
                 | "(" expression ")" ;
struct_literal   = IDENT "{" [ field_inits ] "}" ;
field_inits      = field_init { "," field_init } ;
field_init       = IDENT "=" expression ;
```

Notes on the grammar:

- A `variable_declaration` begins with a type name. At statement level the
  parser treats an identifier followed by another identifier
  (`Rectangle room ...`) as the start of a declaration, and anything else as an
  expression statement. This distinguishes declarations from assignments and
  calls.
- A `struct_literal` is recognized when an identifier is immediately followed
  by `{` in an expression position. There is no other prefix `{` construct in
  expression position, so this is unambiguous for valid programs, but it means
  malformed conditions such as `if (x {` are read as a struct literal named
  `x`.
- Parentheses group expressions but produce no distinct syntax-tree node.
- The assignment operators are the only right-associative operators. All other
  binary operators are left-associative.
- `[` and `]` (reserved for possible future generics) and `:` have **no
  syntactic role** in this grammar. Encountering them is a syntax error.

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
| 9 (highest) | postfix `()` `.` `++` `--` | left |

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
| `void` | No value. Permitted only as a function return type. |

`int` and `float` widths are provisional language semantics, not a commitment
about the runtime representation. Integer overflow behavior and floating-point
rounding are **deferred**. `void` is never the type of a variable, parameter, or
struct field; using it there is an error.

### 18.2 Type compatibility

Types are compared by **exact matching**, with no implicit conversions. An `int`
is not interchangeable with a `float`, and mixing them in one operation is an
error. There are no casts, and no generic or parameterized types.

A value of type `void` may not be used as a value: it may not be assigned,
returned, passed as an argument, used as a condition or operand, or used to
initialize a variable or field.

### 18.3 Operator rules

| Operators | Operand types | Result |
| --- | --- | --- |
| `+` `-` `*` `/` | two `int`, or two `float` | the operand type |
| `%` | two `int` | `int` |
| `<` `<=` `>` `>=` | two `int`, or two `float` | `bool` |
| `==` `!=` | two of the same primitive type | `bool` |
| `&&` `\|\|` | two `bool` | `bool` |
| unary `-` | one `int` or `float` | the operand type |
| unary `!` | one `bool` | `bool` |

Notes:

- Both operands of a binary operation must have the same type. Mixing `int` and
  `float` is an error, not a promotion.
- `%` is not defined for `float`.
- String concatenation with `+` is **not** supported in this revision; `+` on
  strings is an error.
- Relational operators are numeric-only; comparing `bool` or `string` values
  with `<` is an error.
- Equality is defined for the primitive types only. Comparing two struct values
  with `==` is an error.
- There is no unary `+` operator: the grammar in Section 17 does not include it.
- The checker does not diagnose constant division by zero and does not guarantee
  overflow behavior; those are **deferred**.

### 18.4 Variables and assignment

- A variable declaration names a type and a name, with an optional initializer.
  The declared type must exist and must not be `void`.
- If an initializer is present, its type must exactly match the declared type.
- Assignment (`=`) requires the value type to match the target type exactly.
- The assignment target must be a mutable l-value: a local variable, a
  parameter, or a struct field reached through an l-value. Assigning to a
  literal, a call, or any other expression is an error.
- Compound assignment (`+=`, `-=`) requires both operands to be the same numeric
  type. The result has the target's type.
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

### 18.8 Built-in `print`

- `print` is a built-in function, represented in the semantic environment rather
  than special-cased in expression checking.
- It accepts **one or more** arguments, and every argument must be `int`,
  `float`, `bool`, or `string`. Calling it with no arguments, or with a struct
  value, is an error.
- Its return type is `void`, so a call to `print` cannot be used as a value.
- A user-defined top-level `print` shadows the built-in.
- The output format and whether a trailing newline is emitted are **deferred**
  to the runtime and code-generation phases.

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

1. `int` is signed 64-bit and `float` is IEEE 754 binary64.
2. Types match exactly; there are no implicit conversions and no casts.
3. `void` is only a function return type.
4. `-> void` is equivalent to no return type.
5. String concatenation and struct equality are not supported.
6. Relational operators are numeric-only; equality is primitive-only.
7. A variable initializer is optional, and reading an uninitialized variable is
   not diagnosed.
8. Visibility modifiers are accepted on top-level `fn` and `struct` but have no
   semantic effect yet.
9. Return-path analysis is conservative: loops never definitely return, and
   unreachable code is not diagnosed.
10. Struct literals must initialize every field.
11. Checker errors are fail-fast: only the first error is reported.
12. There is no implicit `main` or entry-point check.
