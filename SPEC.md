# Kitten Language Specification — v0.1 (Provisional)

Status: **Provisional.** This document describes the agreed direction for the
Kitten language. Syntax in this document is fixed for Phase 1. Semantics marked
as deferred are not decided and must not be assumed by any component.

This revision covers the language surface. The lexer and the parser are
implemented; the checker, code generator, and runtime are not. Where this
document mentions behavior that does not yet exist, it describes intent, not
current capability.

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

Range, precision, and rounding of floating-point values are **deferred**.

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
- `void` remains a built-in type name and may be written where a return type is
  expected. Whether `fn main() -> void` is accepted in addition to `fn main()`
  is **unresolved** (Section 16). The parser accepts the syntax `-> void` and
  records it as a return type named `void`; whether that is treated as
  equivalent to no return type is a semantic decision.
- `return value;` returns a value. How a bare `return;` interacts with a
  declared return type is **deferred**. The parser accepts both forms.
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
- The initializer is **optional** in the grammar: both `int x;` and `int x = 1;`
  parse (Section 17). Whether an initializer is required is **deferred** to the
  checker.
- The scope and lifetime rules for variables are **deferred**.
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
  order, or struct semantics. Those checks belong to the checker.
- A struct literal is recognized whenever an identifier is immediately followed
  by `{` in an expression position (Section 17). This means a missing `)` before
  a block, as in `if (x {`, is interpreted as a struct literal named `x` and
  reported as a different error than a missing parenthesis would be.
- Struct fields take no initializer in this revision: they are written
  `type name;` (Section 17). Whether fields may be initialized at the
  declaration is **deferred**.
- Whether every field must be initialized, whether fields may be omitted, and
  whether positional literals are allowed are **deferred**.
- Trailing commas are **not** accepted in struct literals in this revision
  (Section 17).
- Field visibility and the default visibility of struct members are
  **deferred**.

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
- `print` accepts one or more arguments. The exact formatting rules — how
  multiple arguments are separated, how numbers and strings are rendered, and
  whether a trailing newline is always emitted — are **deferred**. The `.ktn`
  fixtures under `tests/` document the intended results for the current
  examples.
- `print` is not executable in this phase. The lexer only tokenizes it.

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
| `checker` | Semantic analysis and type checking | Not implemented |
| `codegen` | Checked tree → C source | Not implemented |
| `runtime` | Support code linked into compiled programs, including GC integration | Not implemented |

Supporting layout:

- `cmd/kitten` — the `kitten` command-line entry point. It recognizes
  `kitten build <file.ktn>` and `kitten run <file.ktn>`. Neither command is
  implemented; each reports that clearly instead of pretending to succeed.
- `tests/` — `.ktn` programs and matching `.expected` output files. They are
  lexer and parser inputs today and executable-program tests in later phases.

Pipeline (intended): source → lexer → parser → checker → code generator (C) →
system C compiler → link with Boehm GC → native executable.

Phase boundaries are strict: a package must not take on another phase's
responsibility, and later phases are not started without explicit authorization.

---

## 16. Deferred decisions and unresolved semantics

This revision deliberately leaves the following undecided. None of them may be
invented silently in code. When a decision is needed, raise it and record the
outcome here.

**Types and numbers**

- Exact bit widths of `int` and `float`, and integer overflow behavior.
- Numeric conversions and promotion rules.
- Whether additional numeric types (for example sized or unsigned integers)
  exist.

**Strings**

- In-memory representation, mutability, indexing, and slicing.
- Runtime operations (length, concatenation, comparison) and their semantics.
- Whether string interpolation or raw/multiline strings exist.

**Visibility and declarations**

- Default visibility for top-level declarations and struct members.
- Exactly which declarations accept `public` and `private`, and where the
  modifier is written.
- Whether `fn main() -> void` is accepted in addition to `fn main()`.
- Whether a variable initializer is mandatory.

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
- Whether a bare `return;` is allowed and how it interacts with return types.
  The parser accepts it (Section 17).
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
- The checker, code generator, and runtime are not implemented.
- The `.ktn` fixtures lex and parse successfully but do not execute; only the
  lexer and parser process them.

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
