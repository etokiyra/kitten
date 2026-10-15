# A lexer for Kitten, written in Kitten

This directory holds the first substantial component of the eventual self-hosting
compiler: a lexer written in Kitten, compiled by the Go bootstrap compiler, and
compared byte-for-byte against the Go reference lexer.

It is **not** the production front end. `kitten build` and `kitten run` still use
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
go build -o /tmp/kitten ./cmd/kitten
/tmp/kitten build selfhost/lexer/main.ktn -o /tmp/kitten-lexer
/tmp/kitten-lexer   # prints one "token ..." line per token
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

`cmd/kitten/differential_test.go` builds the Kitten lexer once and runs it over a
corpus of 772 inputs, comparing the result with the Go lexer:

```sh
go test -count=1 -v -run TestKittenLexer ./cmd/kitten/
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

`length` and string indexing are linear in the string's length, because the
representation is NUL-terminated and stores no length (SPEC.md 20.6). The
scanner indexes the source several times per byte, so its total work is

    Θ(n²)        n = source bytes

Measured with `TestKittenLexerScaling`, which logs its numbers
(`go test -count=1 -v -run TestKittenLexerScaling ./cmd/kitten/`), on the
development machine and with process start-up (about 1.3 ms) subtracted:

| Source | Tokens | Go lexer | Kitten lexer | Growth for a 4× input |
| --- | --- | --- | --- | --- |
| 1 KB | 253 | 0.05 ms | 0.3 ms | — |
| 4 KB | 995 | 0.2 ms | 1.6 ms | ×5 |
| 16 KB | 3963 | 0.8 ms | 15 ms | ×9 |
| 64 KB | 15821 | 3 ms | 250 ms | ×16 |

A bare loop that reads `source[index]` once per byte already accounts for about
80 ms of the 64 KB case, which is the evidence that string indexing — not token
storage, substring extraction, or allocation — is the bottleneck: each index is
a scan of the whole string.

**Recommendation.** A single source file of the size this compiler handles (a few
kilobytes) is lexed in a couple of milliseconds, so the current design is usable
for the next self-hosting step. Scanning multi-hundred-kilobyte files is not
practical: at 64 KB the scan costs 250 ms, and the quadratic term grows from
there. A separate optimization milestone — not attempted here — should give
strings a stored length so that `length` and indexing become constant-time; the
scanner would then be linear without any change to its logic. This is a
representation change with no language-visible effect (SPEC.md 16 and 25.5).

## What is deliberately not here

- A parser, checker, or code generator: this is a lexer only.
- Code-point-aware positions and messages.
- Command-line arguments, standard error, and program-selected exit statuses.
- Any optimization of the string representation.
