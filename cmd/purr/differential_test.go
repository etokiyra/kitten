package main

// Differential testing for the Kitten-written lexer in selfhost/lexer.
//
// This file lives in the CLI package because the harness needs the same build
// pipeline and toolchain probe as the other integration tests: it compiles
// selfhost/lexer/main.ktn with the bootstrap compiler and runs the resulting
// executable once per corpus input.
//
// The reference is the Go lexer in kitten/lexer, which remains the production
// front end. The Kitten lexer is expected to agree with it exactly on token
// kinds, token text, line numbers, and positions for ASCII input. Two
// differences are expected and are handled here rather than hidden:
//
//   - The reference lexer scans a []rune and counts columns in code points; the
//     Kitten lexer scans bytes and counts columns in bytes, because Kitten
//     strings are byte sequences (SPEC.md 20.6). The harness normalizes the
//     reference positions from rune columns to byte columns before comparing,
//     so the comparison is exact wherever the two can be.
//   - For input containing bytes >= 128 the reference lexer decodes UTF-8
//     (replacing invalid bytes with U+FFFD), so its token text and its
//     "unexpected character" message can differ from the byte-oriented
//     scanner's. The harness requires text and message parity for ASCII input
//     and for text of valid UTF-8 input, and compares kinds, lines, and
//     positions for every input.
//
// See selfhost/lexer/README.md for the full list of known differences.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"kitten/lexer"
)

// kittenLexerProgram is the driver program of the Kitten-written lexer.
func kittenLexerProgram() string {
	return filepath.Join("..", "..", "selfhost", "lexer", "main.ktn")
}

// buildKittenLexer compiles the Kitten lexer and returns the executable path.
func buildKittenLexer(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-lexer")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenLexerProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the Kitten lexer: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the Kitten lexer:\n%s", warnings)
	}
	return executable
}

// kittenToken is one token reported by the Kitten lexer.
type kittenToken struct {
	kind   string
	text   string
	line   int
	column int
}

// kittenError is the lexical error reported by the Kitten lexer.
type kittenError struct {
	line    int
	column  int
	message string
}

// runKittenLexer writes source to input.txt in directory and runs the Kitten
// lexer there, returning its stdout. A lexical error is reported on stdout and
// is not a process failure, so only an actual crash or a write to stderr is an
// error here.
//
// The Kitten runtime does not expose command-line arguments, so the driver
// reads the fixed relative path "input.txt" and the working directory selects
// the input.
func runKittenLexer(executable, directory, source string) ([]byte, error) {
	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte(source), 0o644); err != nil {
		return nil, fmt.Errorf("writing input.txt: %v", err)
	}

	command := exec.Command(executable)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("running the Kitten lexer: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return nil, fmt.Errorf("the Kitten lexer wrote to stderr: %s", stderr.String())
	}
	return stdout.Bytes(), nil
}

// parseKittenOutput parses the driver's output format:
//
//	token <kind> <line> <column> <byte-length> <text>
//	error <line> <column> <message>
//
// The text is last so that it may contain spaces, and its declared byte length
// is checked against the bytes actually present.
func parseKittenOutput(output []byte) ([]kittenToken, *kittenError, error) {
	text := string(output)
	if text != "" && !strings.HasSuffix(text, "\n") {
		return nil, nil, fmt.Errorf("output does not end with a newline: %q", text)
	}

	var tokens []kittenToken
	var lexError *kittenError
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		switch {
		case line == "":
			continue

		case strings.HasPrefix(line, "token "):
			fields := strings.SplitN(line, " ", 6)
			if len(fields) != 6 {
				return nil, nil, fmt.Errorf("malformed token line %q", line)
			}
			lineNumber, lineError := strconv.Atoi(fields[2])
			column, columnError := strconv.Atoi(fields[3])
			declared, lengthError := strconv.Atoi(fields[4])
			if lineError != nil || columnError != nil || lengthError != nil {
				return nil, nil, fmt.Errorf("malformed token line %q", line)
			}
			if len(fields[5]) != declared {
				return nil, nil, fmt.Errorf("token line %q declares %d bytes of text but has %d", line, declared, len(fields[5]))
			}
			tokens = append(tokens, kittenToken{kind: fields[1], text: fields[5], line: lineNumber, column: column})

		case strings.HasPrefix(line, "error "):
			fields := strings.SplitN(line, " ", 4)
			if len(fields) != 4 {
				return nil, nil, fmt.Errorf("malformed error line %q", line)
			}
			lineNumber, lineError := strconv.Atoi(fields[1])
			column, columnError := strconv.Atoi(fields[2])
			if lineError != nil || columnError != nil {
				return nil, nil, fmt.Errorf("malformed error line %q", line)
			}
			if lexError != nil {
				return nil, nil, fmt.Errorf("more than one error line, second is %q", line)
			}
			lexError = &kittenError{line: lineNumber, column: column, message: fields[3]}

		default:
			return nil, nil, fmt.Errorf("unrecognized output line %q", line)
		}
	}
	return tokens, lexError, nil
}

// referenceToken is one token of the Go reference lexer, with its position
// normalized to a byte column.
type referenceToken struct {
	kind   string
	text   string
	line   int
	column int
}

// referenceLexer runs the Go reference lexer on source and returns its tokens
// and error, with every position converted from a rune column to a byte column.
//
// A line index is built once so that converting a position costs one scan of
// its line rather than one scan of the whole input.
func referenceLexer(source string) ([]referenceToken, *lexer.Error, error) {
	lines := newLineIndex(source)

	tokens, err := lexer.Lex(source)
	if err != nil {
		var lexError *lexer.Error
		if !errors.As(err, &lexError) {
			return nil, nil, fmt.Errorf("the reference lexer returned %T: %v", err, err)
		}
		return nil, &lexer.Error{
			Line:    lexError.Line,
			Column:  lines.byteColumn(lexError.Line, lexError.Column),
			Message: lexError.Message,
		}, nil
	}

	normalized := make([]referenceToken, 0, len(tokens))
	for _, token := range tokens {
		normalized = append(normalized, referenceToken{
			kind:   token.Kind.String(),
			text:   token.Text,
			line:   token.Line,
			column: lines.byteColumn(token.Line, token.Column),
		})
	}
	return normalized, nil, nil
}

// lineIndex holds the byte range of every line of a source string.
type lineIndex struct {
	source string
	starts []int
}

// newLineIndex records the byte offset at which each line starts.
func newLineIndex(source string) lineIndex {
	starts := []int{0}
	for index := 0; index < len(source); index++ {
		if source[index] == '\n' {
			starts = append(starts, index+1)
		}
	}
	return lineIndex{source: source, starts: starts}
}

// byteColumn converts a one-based rune column on a given line into the
// corresponding one-based byte column.
//
// Ranging over a string yields the byte index of each rune, and an invalid byte
// yields one rune whose index is that byte, so this handles malformed UTF-8 as
// well as well-formed input. A column past the last rune of the line, which is
// where the reference lexer reports an end-of-input position, maps to one past
// the line's last byte.
func (l lineIndex) byteColumn(line, runeColumn int) int {
	if line < 1 || line > len(l.starts) {
		return runeColumn
	}
	start := l.starts[line-1]
	end := l.sourceEnd(line)

	count := 0
	for index := range l.source[start:end] {
		if count == runeColumn-1 {
			return index + 1
		}
		count++
	}
	return (end - start) + 1
}

// sourceEnd returns the byte offset just past the last byte of a line, not
// counting its terminating newline.
func (l lineIndex) sourceEnd(line int) int {
	if line < len(l.starts) {
		return l.starts[line] - 1
	}
	return len(l.source)
}

// lexerCase is one differential test input.
type lexerCase struct {
	name   string
	source string
}

// lexerCorpus returns every input used for differential testing.
func lexerCorpus(t *testing.T) []lexerCase {
	t.Helper()

	var cases []lexerCase
	add := func(name, source string) {
		cases = append(cases, lexerCase{name: name, source: source})
	}

	// Empty input and one-character inputs, including one character of every
	// kind and one byte of a multi-byte character.
	add("empty input", "")
	for _, character := range []string{"a", "Z", "_", "0", "9", "+", "-", "(", ")", "\"", "\\", "/", "@", "\n", "\t", "\r", " ", "é", "\xff"} {
		add("one character "+strconv.Quote(character), character)
	}

	// Keywords and built-in type names, alone and in a small context.
	keywords := []string{"struct", "if", "else", "while", "for", "return", "fn", "new", "import", "public", "private", "enum", "union", "match", "map"}
	typeNames := []string{"int", "float", "bool", "string", "void", "byte"}
	for _, word := range append(append([]string{}, keywords...), typeNames...) {
		add("word "+word, word)
		add("word "+word+" in context", "x = "+word+";")
	}
	for _, word := range []string{"true", "false"} {
		add("boolean literal "+word, word)
		add("boolean literal "+word+" in context", "if ("+word+") {}")
	}

	// Identifiers, including keyword prefixes and built-in function names.
	identifiers := []string{
		"x", "x1", "total_count", "PascalCase", "_private", "__", "_", "a1b2c3", "length", "has", "substring", "read_file", "print",
		"iffy", "structs", "elsewhere", "returns", "fnord", "newton", "imported", "publics", "privately",
		"enumerator", "unions", "matches", "mapping", "interior", "floaty", "strings", "voidless", "truely", "falsely", "integer",
		"MAP", "Struct", "IF", "True", "FALSE", "Int", "Print",
	}
	for _, identifier := range identifiers {
		add("identifier "+identifier, identifier+" = 1;")
	}

	// Operators and punctuation, alone and in combination.
	operators := []string{"+", "-", "*", "/", "%", "=", "==", "!=", "<", "<=", ">", ">=", "&&", "||", "!", "++", "--", "+=", "-=", "->", "&", "|", "^", "~", "<<", ">>"}
	punctuation := []string{"(", ")", "{", "}", "[", "]", ",", ".", ";", ":"}
	for _, operator := range operators {
		add("operator "+operator, operator)
		add("operator "+operator+" between identifiers", "a "+operator+" b")
	}
	for _, mark := range punctuation {
		add("punctuation "+mark, mark)
	}
	add("all operators separated", strings.Join(operators, " "))
	add("all operators adjacent", strings.Join(operators, ""))
	add("all punctuation separated", strings.Join(punctuation, " "))
	add("all punctuation adjacent", strings.Join(punctuation, ""))
	add("all operators in context", "x = a "+strings.Join(operators, " b ")+" c;")

	// Juxtapositions that could be split into shorter tokens, and operators that
	// interact with comments.
	juxtapositions := []string{
		"===", "!==", "<=<", "><=", "&&&&", "||||", "++++", "----", "->>", "-->", "a=-1", "a=-b", "x=+1",
		"1<=2", "1>=2", "1!=2", "1==2", "a&&b", "a||b", "!a", "!!a", "**", "/*", "*/", "/**/", "/* /* */",
		"//", "///", "////", "i++++", "j----", "k+=+", "m-=-", "p->->q", "<==>", "=<==", "!==!", "&&&", "|||", "*/*", "//*",
	}
	for _, source := range juxtapositions {
		add("juxtaposition "+strconv.Quote(source), source)
	}
	// Every ordered pair of operators. This systematically checks that the
	// longest-match rules never split a pair of operators differently from the
	// reference lexer, including the pairs that form "//" and "/*".
	for _, first := range operators {
		for _, second := range operators {
			add("operator pair "+first+second, first+second+" 0")
		}
	}

	// Numbers: integer and floating-point forms and the boundaries of the
	// literal rules, including inputs that are not numbers.
	numbers := []string{
		"0", "1", "9", "10", "42", "1234567890", "9223372036854775807", "99999999999999999999999999", "0000000000",
		"1.5", "0.25", "1.0", "12.5", "3.14159", "0.0", "1.2345678901234567890123", "00.5", "5.5", "100000000000000000000000000",
		"1.", "1.5.5", "1..2", "1...2", ".5", ".", "..", "1 . 5", "1.2e3", "1e5", "0x1F", "0x0", "0xAbCd", "0xFFFFFFFFFFFFFFFF", "0b101", "0b0", "0o17", "1_000", "12abc", "abc12", "1a2b3", "007",
		"0x", "0b", "0X1", "0B1", "0x1g", "0b102", "0x1_", "0x10000000000000000", "0b" + strings.Repeat("0", 65),
		"0b" + strings.Repeat("1", 64),
	}
	for _, source := range numbers {
		add("number "+strconv.Quote(source), source)
	}
	add("numbers in context", "a[0] = 1 + 2.5*3;")

	// String literals: every supported escape, and every error form.
	stringLiterals := []string{
		`""`, `"hello"`, `"hello, kitten"`, `"a"`, `" "`, `"  "`, `"a b"`,
		`"\n"`, `"\t"`, `"\""`, `"\\"`, `"a\nb\tc\"d\\e"`, `"\n\n\n"`, `"\\\\"`, `"\\n"`,
		`"a\qb"`, `"\q"`, `"a\0b"`, `"a\ab"`, `"a\'b"`, `"a\ b"`,
		`"abc`, `"abc\`, `"\`, `"""`, `"\"`, `"\\`, `"a\`,
		`"é"`, `"日本語"`, `"café"`, `"emoji 😀"`, `"tab\there"`,
		`x = "a" + "b"`, `print("hi");`,
	}
	for _, source := range stringLiterals {
		add("string "+strconv.Quote(source), source)
	}
	for _, source := range []string{
		"\"ab\ncd\"", "\"a\rb\"", "\"\r\"", "\"line\nbreak\"", "\"unterminated\n", "\"a\"\n\"b",
	} {
		add("string with line break "+strconv.Quote(source), source)
	}

	// Comments and their interaction with division.
	comments := []string{
		"//", "// comment", "//\n1", "a // c\nb", "// only a comment", "6 / 2", "6 // 2", "a//b", "///", "////",
		"// \"quoted\" text\nx", "x // trailing\n// leading\ny", "// comment\r\nx", "a/ /b", "*/", "/*", "/**/",
		"// comment with 日本語\nx", "//\n//\n//\n",
	}
	for _, source := range comments {
		add("comment "+strconv.Quote(source), source)
	}

	// Whitespace, line endings, and position tracking.
	whitespace := []string{
		"", " ", "\t", "\n", "\r", "\r\n", "   \n\n  ", "\t\t", " \t \r \n ", "\n\n\n", "a\n", "\na", "a\r", "\ra",
	}
	for _, source := range whitespace {
		add("whitespace "+strconv.Quote(source), source)
	}
	for _, source := range []string{
		"a\tb", "a  b", "a\r\nb", "a\rb", "a\nb", "a \n b", "\t\na\n\tb\t\n",
	} {
		add("whitespace between tokens "+strconv.Quote(source), source)
	}

	// Small programs, including multi-line position tracking and mixed line
	// endings.
	programs := []string{
		"fn main() {\n    int x = 5;\n}\n",
		"fn main() {\n    print(\"hi\");\n}\n",
		"// leading comment\n\nfn main() {\n}\n",
		"struct Room {\n    int width;\n    int height;\n}\n\nfn main() {\n    Room room = Room { width = 4, height = 5 };\n    print(room.width);\n}\n",
		"public enum Kind {\n    A,\n    B\n}\n\nfn f(Kind kind) -> int {\n    match (kind) {\n        A { return 1; }\n        B { return 2; }\n    }\n}\n",
		"map[string]int table = new map[string]int;\ntable[\"x\"] = 1;\n",
		"\tif (a && b) {\r\n\t\tx++;\r\n\t}\r\n",
		"int[] values = new int[3];\nvalues[0] = -1;\nfloat f = 0.5;\n",
		"union Value {\n    Number { int amount; }\n    Empty\n}\n",
		"x->y = z; // arrow\n",
		"a = 1; /* not a comment */ b = 2;\n",
	}
	for index, source := range programs {
		add("program "+strconv.Itoa(index+1), source)
	}

	// Non-ASCII input, both valid and invalid as UTF-8.
	nonASCII := []string{
		"é", "aé", "éa", "café", "naïve", "日本", "日本語", "a日本b", "emoji 😀", "\té", "é\né", "é = 1;",
		`"é"`, `"日本語"`, `"aé"`, "x = \"é\";", "// é\nx", "æøå = 1;",
		"\xc3", "\xff", "a\xffb", "\xe2\x82\xac", "\x80\x80", "x = \"a\xffb\";", "\xc3\x28", "é\xffé", "\xfe\xff",
	}
	for _, source := range nonASCII {
		add("non-ASCII "+strconv.Quote(source), source)
	}

	// Every fixture in tests/, so that the comparison also covers realistic
	// multi-line programs, including the multi-file module fixture, plus the
	// Kitten lexer's own sources, which are the most realistic input there is.
	fixtures, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.ktn"))
	if err != nil {
		t.Fatalf("globbing fixtures: %v", err)
	}
	fixtures = append(fixtures,
		filepath.Join("..", "..", "tests", "modules", "main.ktn"),
		filepath.Join("..", "..", "tests", "modules", "geometry.ktn"),
		filepath.Join("..", "..", "tests", "modules", "util", "text.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "tokens.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "lexer.ktn"),
		filepath.Join("..", "..", "selfhost", "lexer", "main.ktn"),
	)
	sort.Strings(fixtures)
	for _, fixture := range fixtures {
		content, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatalf("reading fixture %s: %v", fixture, err)
		}
		add("fixture "+filepath.Base(fixture), string(content))
	}

	if len(cases) < 400 {
		t.Fatalf("the differential corpus is unexpectedly small: %d inputs", len(cases))
	}
	return cases
}

// describeCase renders a case for a failure message, truncating long sources.
func describeCase(testCase lexerCase) string {
	source := testCase.source
	const limit = 120
	if len(source) > limit {
		source = source[:limit] + "..."
	}
	return fmt.Sprintf("%s %q", testCase.name, source)
}

// compareLexers checks the Kitten lexer's result against the reference lexer's.
func compareLexers(t *testing.T, testCase lexerCase, kittenTokens []kittenToken, kittenError *kittenError) {
	t.Helper()

	referenceTokens, referenceError, err := referenceLexer(testCase.source)
	if err != nil {
		t.Fatalf("%s: %v", describeCase(testCase), err)
	}

	// The reference lexer decodes UTF-8, so its text can differ from a
	// byte-oriented scanner's only when the input contains bytes >= 128. Its
	// messages can differ whenever any byte >= 128 is present at all.
	asciiOnly := isASCII(testCase.source)
	compareText := asciiOnly || utf8.ValidString(testCase.source)
	compareMessage := asciiOnly

	if referenceError != nil {
		if kittenError == nil {
			t.Errorf("%s: the reference lexer failed at %d:%d (%s) but the Kitten lexer succeeded with %d tokens",
				describeCase(testCase), referenceError.Line, referenceError.Column, referenceError.Message, len(kittenTokens))
			return
		}
		if len(kittenTokens) != 0 {
			t.Errorf("%s: the Kitten lexer reported a lexical error together with %d tokens",
				describeCase(testCase), len(kittenTokens))
		}
		if kittenError.line != referenceError.Line || kittenError.column != referenceError.Column {
			t.Errorf("%s: the Kitten lexer failed at %d:%d, want %d:%d",
				describeCase(testCase), kittenError.line, kittenError.column, referenceError.Line, referenceError.Column)
		}
		if compareMessage && kittenError.message != referenceError.Message {
			t.Errorf("%s: the Kitten lexer message is %q, want %q",
				describeCase(testCase), kittenError.message, referenceError.Message)
		}
		return
	}

	if kittenError != nil {
		t.Errorf("%s: the Kitten lexer failed at %d:%d (%s) but the reference lexer succeeded with %d tokens",
			describeCase(testCase), kittenError.line, kittenError.column, kittenError.message, len(referenceTokens))
		return
	}

	if len(kittenTokens) != len(referenceTokens) {
		t.Errorf("%s: the Kitten lexer produced %d tokens, want %d",
			describeCase(testCase), len(kittenTokens), len(referenceTokens))
	}

	shared := len(kittenTokens)
	if len(referenceTokens) < shared {
		shared = len(referenceTokens)
	}
	for index := 0; index < shared; index++ {
		got := kittenTokens[index]
		want := referenceTokens[index]
		if got.kind != want.kind || got.line != want.line || got.column != want.column {
			t.Errorf("%s: token %d is %s at %d:%d, want %s at %d:%d",
				describeCase(testCase), index, got.kind, got.line, got.column, want.kind, want.line, want.column)
			continue
		}
		if compareText && got.text != want.text {
			t.Errorf("%s: token %d text is %q, want %q",
				describeCase(testCase), index, got.text, want.text)
		}
	}
}

// isASCII reports whether every byte of source is below 128.
func isASCII(source string) bool {
	for index := 0; index < len(source); index++ {
		if source[index] >= 128 {
			return false
		}
	}
	return true
}

// TestKittenLexerMatchesReference runs the whole corpus through both lexers and
// compares the results. It is the central check that the Kitten-written lexer
// reproduces the reference lexer's behavior.
func TestKittenLexerMatchesReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenLexer(t)
	directory := t.TempDir()
	cases := lexerCorpus(t)
	t.Logf("differential corpus: %d inputs", len(cases))

	for _, testCase := range cases {
		output, err := runKittenLexer(executable, directory, testCase.source)
		if err != nil {
			t.Errorf("%s: %v", describeCase(testCase), err)
			continue
		}
		kittenTokens, kittenError, err := parseKittenOutput(output)
		if err != nil {
			t.Errorf("%s: %v", describeCase(testCase), err)
			continue
		}
		compareLexers(t, testCase, kittenTokens, kittenError)
	}
}

// TestKittenLexerOutputFormat pins the driver's output format with explicit
// expected output, so that a change to the format is caught even if both lexers
// change together.
func TestKittenLexerOutputFormat(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenLexer(t)
	directory := t.TempDir()

	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "tokens",
			source: "fn main() {\n}\n",
			want: strings.Join([]string{
				"token Fn 1 1 2 fn",
				"token Ident 1 4 4 main",
				"token LParen 1 8 1 (",
				"token RParen 1 9 1 )",
				"token LBrace 1 11 1 {",
				"token RBrace 2 1 1 }",
				"token EOF 3 1 0 ",
				"",
			}, "\n"),
		},
		{
			name:   "error",
			source: `"a\qb"`,
			want:   "error 1 4 invalid escape sequence \\q\n",
		},
		{
			name:   "empty input",
			source: "",
			want:   "token EOF 1 1 0 \n",
		},
	}

	for _, testCase := range cases {
		output, err := runKittenLexer(executable, directory, testCase.source)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		if string(output) != testCase.want {
			t.Errorf("%s: output = %q, want %q", testCase.name, output, testCase.want)
		}
	}
}

// repeatToSize builds a source of at least size bytes out of one representative
// line of Kitten code.
func repeatToSize(line string, size int) string {
	var builder strings.Builder
	builder.Grow(size + len(line))
	for builder.Len() < size {
		builder.WriteString(line)
	}
	return builder.String()
}

// measureKittenLexer runs the Kitten lexer on source and returns the fastest of
// several runs. The minimum is a more stable estimate than the mean, which is
// sensitive to scheduling noise.
func measureKittenLexer(t *testing.T, executable, directory, source string, repetitions int) time.Duration {
	t.Helper()

	best := time.Duration(0)
	for index := 0; index < repetitions; index++ {
		start := time.Now()
		if _, err := runKittenLexer(executable, directory, source); err != nil {
			t.Fatalf("%v", err)
		}
		if elapsed := time.Since(start); best == 0 || elapsed < best {
			best = elapsed
		}
	}
	return best
}

// measureReferenceLexer runs the Go reference lexer on source and returns the
// fastest of several runs.
func measureReferenceLexer(t *testing.T, source string, repetitions int) time.Duration {
	t.Helper()

	best := time.Duration(0)
	for index := 0; index < repetitions; index++ {
		start := time.Now()
		if _, _, err := referenceLexer(source); err != nil {
			t.Fatalf("%v", err)
		}
		if elapsed := time.Since(start); best == 0 || elapsed < best {
			best = elapsed
		}
	}
	return best
}

// TestKittenLexerScaling measures the Kitten lexer against the Go lexer on
// growing inputs, and checks that the two agree on the largest one.
//
// The measurements are logged rather than asserted: they document the cost of
// scanning, and they are the evidence behind the performance notes in
// selfhost/lexer/README.md. Run it with -v to see the numbers.
//
// String length and indexing are constant-time since the representation stores
// the byte length, so the scanner's scanning work grows linearly with the input.
// A growth factor of about 4 for a 4x input therefore indicates linear behavior;
// the factors reported below are that plus allocation and collector overhead.
func TestKittenLexerScaling(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenLexer(t)
	directory := t.TempDir()
	line := "    int value42 = compute(alpha, beta) + 3.5 * 2; // note\n"
	const repetitions = 5

	// Running the lexer on empty input measures process start-up, which is
	// included in every measurement below.
	startup := measureKittenLexer(t, executable, directory, "", repetitions)
	t.Logf("process start-up baseline (empty input): %s", startup.Round(time.Microsecond))

	previous := time.Duration(0)
	for _, size := range []int{1 << 10, 1 << 12, 1 << 14, 1 << 16, 1 << 18} {
		source := repeatToSize(line, size)

		kittenElapsed := measureKittenLexer(t, executable, directory, source, repetitions)
		goElapsed := measureReferenceLexer(t, source, repetitions)

		referenceTokens, referenceError, err := referenceLexer(source)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if referenceError != nil {
			t.Fatalf("size %d: the reference lexer failed: %v", size, referenceError)
		}
		output, err := runKittenLexer(executable, directory, source)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		kittenTokens, kittenError, err := parseKittenOutput(output)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if kittenError != nil {
			t.Fatalf("size %d: the Kitten lexer failed: %v", size, kittenError)
		}
		if len(kittenTokens) != len(referenceTokens) {
			t.Fatalf("size %d: the Kitten lexer produced %d tokens, want %d", size, len(kittenTokens), len(referenceTokens))
		}

		net := kittenElapsed - startup
		if net < 0 {
			net = 0
		}
		growth := ""
		if previous > 0 && net > 0 {
			growth = fmt.Sprintf(", x%.1f for a x4 input", float64(net)/float64(previous))
		}
		t.Logf("bytes=%6d tokens=%6d  Go=%10s  Kitten total=%10s  net=%10s%s",
			len(source), len(referenceTokens), goElapsed.Round(time.Microsecond),
			kittenElapsed.Round(time.Microsecond), net.Round(time.Microsecond), growth)
		previous = net
	}
}
