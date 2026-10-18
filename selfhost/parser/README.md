# A parser for Kitten, written in Kitten

This directory holds the second component of the eventual self-hosting compiler:
a parser written in Kitten that consumes the tokens produced by the
Kitten-written lexer and builds an abstract syntax tree.

It is **not** the production front end. `purr build` and `purr run` still use
the Go parser in `parser/`, which remains authoritative. Nothing here changes the
language, and no language feature was added to make the parser easier.

## Files

| File | Contents |
| --- | --- |
| `ast.ktn` | the AST types: declarations, statements, expressions, types, positions |
| `parser.ktn` | the parser: token access, error construction, and every grammar production |
| `printer.ktn` | the canonical AST rendering used by the driver and by differential testing |

The driver is `../parse.ktn`, one directory up, rather than `main.ktn` here. The
loader requires every import to resolve **inside the directory of the file being
compiled**, so a driver that uses both the lexer and the parser has to sit above
both `../lexer/` and `../parser/`. This is a property of the current import
policy, not of the parser.

## Building and running

```sh
# From the repository root. The driver reads the fixed relative path
# "input.txt", so the working directory selects the input.
printf 'fn add(int a, int b) -> int {\n    return a + b;\n}\n' > input.txt
go build -o /tmp/purr ./cmd/purr
/tmp/purr build selfhost/parse.ktn -o /tmp/purr-parser
/tmp/purr-parser
```

On a successful parse the driver prints the canonical AST, one node per line,
indented two spaces per level:

```text
program
  fn 1 1 default add
    param 1 8 a
      type-name 1 8 int
    param 1 15 b
      type-name 1 15 int
    ret 1 25
      type-name 1 25 int
    block 1 29
      return 2 5
        present
          binary 2 12 2 14 +
            ident 2 12 a
            ident 2 16 b
```

Each line is a node kind followed by the fields that identify it: positions,
names, operators, and literal text, then the children on the following lines. On
a lexical or syntax error it prints one line instead:

```text
error <line> <column> <message>
```

The driver cannot take a path from the command line, because the Kitten runtime
exposes no command-line arguments, no standard error, and no program-selected
exit status (SPEC.md 16 and 25.4); it reports failure on standard output and
leaves the exit status to the runtime, exactly like the lexer driver.

## The AST

`ast.ktn` mirrors `parser/ast.go`: the same node kinds, the same fields, and the
same positions. It contains no semantic information, resolves no names, and
assigns no types.

Two representation choices are worth knowing:

- **Nodes reference tokens instead of copying them.** A node that needs a lexeme
  keeps the lexer's `Token`, which already carries the kind, the text, and the
  one-based line and column. Nothing is duplicated, and no position is stored
  twice: a node's position is computed, by `decl_position`, `stmt_position`,
  `expr_position`, and `type_position`, using exactly the rules the reference
  AST's `Pos` methods use.
- **Nodes carry a stable id.** Every expression and type reference is built with
  an integer id from a per-parse counter, and `ParseResult.Success` reports how
  many were assigned. A later phase attaches resolved types, symbols, and variants
  to nodes by that id, which is what makes the semantic model's keys stable under
  nesting, repetition, and multiple modules; source coordinates are not identity,
  because several nodes share a position.
- **Absence is a value, not a null.** Kitten has no null: a local variable
  declared without an initializer is undefined rather than empty, and there is no
  literal that denotes "no object". Optional sub-nodes - an else branch, a
  variable initializer, a return value, a for-loop clause, a function's return
  type - are therefore modelled as `OptionalStmt`, `OptionalExpr`, and
  `OptionalType`, each with an `Absent` case and a `Present` case. A `match` must
  handle `Absent`, so no code path can forget that a part of the tree may be
  missing. This is the smallest change that keeps the AST explicit and needs no
  new language feature; a null or optional type in the language would be the
  alternative, and is **deferred**.

## Grammar coverage

The parser accepts exactly the grammar in SPEC.md Section 17, which is the
grammar the Go reference parser accepts:

- top-level `fn`, `struct`, `enum`, and `union` declarations with an optional
  `public` or `private` modifier, and `import "path";`;
- types: the five built-in type names, user-defined names, `[]` array suffixes,
  and `map[K]V`;
- statements: blocks, local declarations with an optional initializer,
  expression statements, `if`/`else if`/`else`, `while`, `for` with any of its
  three clauses empty, `return` with or without a value, and `match` with
  binding lists;
- expressions: integer, float, string, boolean, and identifier primaries;
  struct and variant literals; array and map creation; calls; member access;
  indexing; prefix and postfix `-`, `!`, `++`, `--`; every binary operator; and
  the three assignment operators;
- the precedence and associativity table in SPEC.md Section 17.3, with
  assignment lowest and right-associative and every other binary operator
  left-associative, implemented by precedence climbing exactly as the reference
  parser does.

Constructs that need lookahead are handled as the reference parser handles them:
a struct literal is an identifier immediately followed by `{`, a variant literal
is identifier, `.`, identifier, `{`, a local declaration is a type keyword, or an
identifier followed by `[]` suffixes and another identifier, and array creation
uses `new`. No construct needs more than three tokens of lookahead, and the
parser never reads past the token array: `peek` and `peek_at` return the
end-of-input token for any position outside it.

## Failure handling and termination

Kitten has no exceptions, so every step that can fail returns a small result
union carrying either the value or a `ParseError`, and callers propagate the
first failure unchanged. Propagation is explicit: a caller cannot read a value
without having handled the failure case, and the first error always wins,
matching the reference parser's fail-fast behavior.

Every loop either consumes a token or returns, so no input can make the parser
spin; the differential corpus includes truncated and malformed inputs that
exercise each loop's exit. Recursion follows the nesting of the source, exactly
as the reference parser's does.

## Array capacities

Kitten arrays are fixed-size, so each AST list is allocated with a capacity
computed by `list_capacity` and carries a separate count. The capacity is the
**extent of the enclosing delimited group**: the number of tokens from the
current position to the first closing delimiter that no opening delimiter
matches, plus one. Every element of the list lies inside that group and consumes
at least one token, so the bound is safe; it is also local to the construct being
parsed, which keeps parsing roughly linear. Sizing lists by the whole file
instead would make a file with many small lists - an argument list in a call, a
statement list in a function body - cost quadratically in memory and collector
work. A capacity that were too small would be a fatal bounds error rather than an
out-of-bounds write, and the differential corpus checks the bound on every input.

## Differential testing

`cmd/purr/parser_differential_test.go` builds the Kitten parser once and runs
it over a corpus of 262 inputs, comparing its rendering with the reference
implementation's rendering of the same source:

```sh
go test -count=1 -v -run TestKittenParser ./cmd/purr/
```

The corpus covers every grammar production listed above, empty and one-token
programs, all four declaration forms with and without visibility, every operator
alone and in precedence-boundary combinations, every break in the precedence
table, nested and deeply nested constructs (forty levels of parentheses, unary
operators, calls, member access, and blocks), collections and their literals,
`match` arms with and without bindings, sixty-five malformed or truncated
inputs, non-ASCII input both valid and invalid as UTF-8, every `.ktn` fixture,
and the self-hosted sources themselves.

Results: **257 of the 262 inputs render byte-identically**. The remaining five
contain a byte at or above 128, where the two implementations differ only in the
documented lexer-level ways - a byte-oriented column, and a lexeme or message
that Go renders by decoding UTF-8 - and the harness verifies those by comparing
the AST shape, node kind by kind and depth by depth, plus the error position.

`TestKittenParserOutputFormat` also pins the rendering with hand-written
expectations, so a format regression is caught even if both implementations
change together.

## Performance

Parsing is roughly linear in the input size. Measured with
`TestKittenParserScaling`, which also checks that both implementations agree on
the largest input (`go test -count=1 -v -run TestKittenParserScaling
./cmd/purr/`), including the cost of rendering the AST and starting the
process:

| Source | Functions | AST lines | Time |
| --- | --- | --- | --- |
| 11 KB | 100 | 2701 | 15 ms |
| 109 KB | 1000 | 27001 | 111 ms |
| 436 KB | 4000 | 108001 | 423 ms |

A tenfold input costs about seven times as much and a fourfold input about four
times as much, which is linear within measurement noise. Rendering the tree is a
large part of the cost, as it is for the lexer driver.

## What is deliberately not here

- A code generator. A semantic checker that consumes this AST exists in
  `selfhost/checker`, with its driver in `selfhost/check.ktn`.
- Module resolution: the parser parses one token stream, exactly like the
  reference parser.
- A null or optional type in the language, which would replace the explicit
  `Absent`/`Present` unions.
- Any recovery from a syntax error beyond reporting the first one.
