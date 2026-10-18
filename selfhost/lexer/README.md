# A lexer for Kitten, written in Kitten

This directory holds the first substantial component of the eventual self-hosting
compiler: a lexer written in Kitten, compiled by the Go bootstrap compiler, and
compared byte-for-byte against the Go reference lexer.

It is **not** the production front end. `purr build` and `purr run` still use
the Go lexer in `lexer/`, which remains authoritative. Nothing here changes the
language, and no language feature was added to make it easier.

## Files

| File | Contents |
| --- | --- |
| `tokens.ktn` | `TokenKind` enum, `Token` struct, `LexResult` union, `kind_name` |
| `lexer.ktn` | the `Scanner` state, all scanning helpers, and the public `lex` |
| `main.ktn` | a driver program that reads `input.txt` and prints the result |

## Building and running

```sh
# From the repository root. The driver reads the fixed relative path
# "input.txt", so the working directory selects the input.
printf 'fn main() {\n    int x = 42 + 3.5;\n}\n' > input.txt
go build -o /tmp/purr ./cmd/purr
/tmp/purr build selfhost/lexer/main.ktn -o /tmp/purr-lexer
/tmp/purr-lexer   # prints one "token ..." line per token
```

Output format, one line per token:

```text
token <kind> <line> <column> <byte-length> <text>
```

and one line for the first lexical error:

```text
error <line> <column> <message>
```

The text is last so it may contain spaces, and its byte length is printed before
it so a reader can recover it exactly. `kind_name` returns the same names the Go
lexer reports through `Kind.String`, which is what makes the outputs comparable
as text.

### Why the input path is fixed

The Kitten runtime exposes no way for a program to read its command-line
arguments, to write to standard error, or to choose its exit status; the only
nonzero exit is a fatal runtime error. The driver therefore cannot take a path
from the command line, and it reports a lexical error on standard output rather
than by failing. Argument access and exit status are deferred language and
runtime work (SPEC.md 16 and 25.4); the harness here works around them by
choosing the working directory instead.

## How the scanner mirrors the reference lexer

`lexer.ktn` follows the structure of `lexer/lexer.go` step for step, so that the
two can be read side by side:

- whitespace, `//` line comments, and the newline left behind by a comment;
- identifiers and keyword classification (`print` is an identifier, not a
  keyword);
- numbers, where a decimal point belongs to a literal only when a digit follows
  it, so `1.` is an integer and a dot while `.5` is a dot and an integer;
- string literals with the four escapes `\n`, `\t`, `\"`, `\\`, with distinct
  errors for an unterminated literal, a newline inside one, and an unsupported
  escape;
- operators and punctuation with longest-match preference, so `==`, `->`, and
  `++` are single tokens;
- one-based line and column tracking, where only a newline advances the line;
- exactly one `EOF` token at the end, positioned after the last byte;
- the first lexical error, returned instead of any tokens.

Loops are written with explicit conditions, because `break` and `continue` are
not implemented. Union matching, rather than a sentinel, carries the
token-or-error outcome of a single scan step.

## Differential testing

`cmd/purr/differential_test.go` builds the Kitten lexer once and runs it over a
corpus of 772 inputs, comparing the result with the Go lexer:

```sh
go test -count=1 -v -run TestKittenLexer ./cmd/purr/
```

The corpus covers every keyword, type name, and boolean literal; identifiers and
keyword-prefix identifiers; every operator and punctuation mark, every ordered
pair of operators, and juxtapositions that could be split differently; integer
and floating-point boundaries and non-numbers; every supported escape, every
string error form, and strings containing line breaks; comments and their
interaction with division; whitespace and `\n`, `\r`, and `\r\n` line endings;
multi-line position tracking; non-ASCII input, both valid and invalid UTF-8;
every `.ktn` fixture in `tests/`; and the three sources of this lexer, so it is
checked on the most realistic input available — its own source.

The comparison is exact — kind, text, line, and column — for ASCII input. It
normalizes two things, both documented above and in the test file:

- **Rune columns to byte columns.** The reference lexer counts columns in code
  points and the Kitten lexer counts bytes, so the harness converts the reference
  positions before comparing. This is a mechanical, total conversion: invalid
  UTF-8 bytes count as one rune each, exactly as Go's `[]rune` conversion does.
- **Text and messages for bytes >= 128.** The reference lexer decodes UTF-8 and
  replaces invalid bytes with U+FFFD, so its token text can differ from a
  byte-oriented scanner's. For input containing such bytes the harness still
  compares kinds, token counts, lines, columns, and error positions, but not
  text or message text.

Two further cases are checked by hand-written expectations in
`TestKittenLexerOutputFormat`, so that a format regression is caught even if both
lexers change together.

## Known differences from the reference lexer

1. **Columns are byte offsets, not code-point offsets.** For ASCII input the two
   agree exactly. After a byte at or above 128, the reference column counts one
   rune where this scanner counts one to four bytes.
2. **The "unexpected character" message names a byte.** For `0xC3` this scanner
   says `'\xc3'`; the reference decodes the sequence and names the code point,
   or U+FFFD for invalid UTF-8.
3. **Embedded NUL is impossible.** A Kitten string cannot contain a NUL byte, so
   `read_file` truncates a source file at the first one; the reference lexer
   would continue past it.
4. **No command-line arguments or chosen exit status.** See above.

Everything else — kinds, token text, lines, byte-normalized columns, error
positions, and error messages for ASCII input — matches exactly on the corpus.

## Performance

`length` and string indexing are constant-time, because a Kitten string stores
its byte length next to its bytes (SPEC.md 20.6), so the scanner's work is

    Θ(n)        n = source bytes

plus the per-token cost of allocating a token, a scan result, and a lexeme, which
is also linear in the number of tokens. Measured with `TestKittenLexerScaling`,
which logs its numbers (`go test -count=1 -v -run TestKittenLexerScaling
./cmd/purr/`), on the development machine and with process start-up (about
1.3 ms) subtracted:

| Source | Tokens | Go lexer | Kitten lexer | Growth for a 4× input |
| --- | --- | --- | --- | --- |
| 1 KB | 253 | 0.08 ms | 0.3 ms | — |
| 4 KB | 995 | 0.3 ms | 2.4 ms | ×9 (measurement noise dominates) |
| 16 KB | 3963 | 1.2 ms | 7.6 ms | ×3.1 |
| 64 KB | 15821 | 5.6 ms | 31 ms | ×4.1 |
| 256 KB | 63281 | 22 ms | 116 ms | ×3.8 |

A fourfold input costs about four times as much, which is linear. For
comparison, before the representation stored a length, the same program took
about 379 ms for 64 KB and 7.6 s for 256 KB on the same machine, with a growth
factor of about 13 to 20 per fourfold input: the old implementation was
quadratic, and the change made it about 14× faster at 64 KB and about 65× faster
at 256 KB.

Scanning is no longer the dominant cost. At 1 MB, lexing and printing only the
token count takes about 249 ms, while printing every token takes about 461 ms, so
the driver's output is nearly half the time. Within the lexer, the remaining
per-token cost is allocation — the token, the scan result, and the lexeme — and
the collector's work over those objects, both proportional to the number of
tokens.

## What is deliberately not here

- A code generator. A parser that consumes these tokens exists in
  `selfhost/parser`, with its driver in `selfhost/parse.ktn`, and a semantic
  checker exists in `selfhost/checker`, with its driver in `selfhost/check.ktn`;
  both reproduce the reference implementation and are checked against it by their
  own differential corpora.
- Code-point-aware positions and messages.
- Command-line arguments, standard error, and program-selected exit statuses.
- Any further optimization: the remaining cost is per-token allocation and the
  driver's printing, both linear, not scanning.
