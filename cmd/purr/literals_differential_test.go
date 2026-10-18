package main

// Differential testing for the Kitten-written string literal pool
// (selfhost/codegen/literals.ktn, driven by selfhost/expressions.ktn).
//
// The pool is not emitted on its own: a literal is interned where it is rendered,
// which is what gives the pool the reference's first-use order and its
// deduplication. The tests therefore drive the expression probe, which renders
// every expression of a program and then prints the pool those renderings filled.
// The pool text is compared with the reference generator's own pooled
// declarations, extracted structurally: the lines between the end of the
// declaration sections and the first function definition.
//
// The expression lines are compared too, by the harness in
// expressions_differential_test.go, so a pooled symbol can never agree by accident
// while the expression that uses it disagrees.
//
// Ordering note. A literal is discovered when the expression containing it is
// rendered, so the pool's order is the reference's rendering order. The two orders
// differ from source order in exactly one place - a struct or variant literal,
// whose values the reference renders in field declaration order - and the probe's
// walk visits those values in that order, which the cases below check directly.

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// literalCase is one program whose literal pool is compared with the reference's.
type literalCase struct {
	description string
	root        string
	imported    string
}

func literalCases() []literalCase {
	return []literalCase{{
		description: "one string literal",
		root: `fn main() {
    string greeting = "hello";
    print(greeting);
}
`,
	}, {
		description: "empty strings",
		root: `fn main() {
    string empty = "";
    string filled = "x";
    string alsoEmpty = "";
    print(empty);
    print(filled);
    print(alsoEmpty);
}
`,
	}, {
		description: "multiple distinct strings",
		root: `fn main() {
    string one = "alpha";
    string two = "beta";
    string three = "gamma";
    print(one);
    print(two);
    print(three);
}
`,
	}, {
		description: "repeated identical strings in separate expressions",
		root: `fn main() {
    string first = "same";
    string second = "same" + "same";
    print(first);
    print(second);
    print("same");
}
`,
	}, {
		description: "escaped quotes and backslashes",
		root: `fn main() {
    string quoted = "a\"b";
    string slashed = "c\\d";
    string both = "\\\"";
    print(quoted);
    print(slashed);
    print(both);
}
`,
	}, {
		description: "newlines and tabs",
		root: `fn main() {
    string lines = "one\ntwo";
    string tabs = "a\tb\tc";
    string mixed = "\n\t\n";
    print(lines);
    print(tabs);
    print(mixed);
}
`,
	}, {
		description: "literals inside nested expressions",
		root: `fn main() {
    string inner = "inner";
    string nested = inner + ("mid" + ("deep" + "outer"));
    print(nested);
}
`,
	}, {
		description: "literals in call arguments, which are rendered even though the call is not",
		root: `fn helper(string value) -> string {
    return value;
}
fn main() {
    print("in-call");
    print(helper("arg"));
}
`,
	}, {
		description: "literals in nested expression kinds",
		root: `fn main() {
    map[string]string names = new map[string]string;
    print(names["key"] = "value");
    print(names["key"]);
}
`,
	}, {
		description: "struct literal values in declaration order",
		root: `struct Pair {
    string first;
    string second;
}
fn main() {
    Pair pair = Pair { first = "one", second = "two" };
    print(pair.first);
}
`,
	}, {
		description: "struct literal values out of source order",
		root: `struct Pair {
    string first;
    string second;
    string third;
}
fn main() {
    Pair pair = Pair { third = "three", first = "one", second = "two" };
    print(pair.first);
}
`,
	}, {
		description: "variant literal values out of source order",
		root: `union Msg {
    Text { string body; }
    Pair { string left; string right; }
    None
}
fn describe(Msg message) -> string {
    match (message) {
        Text(body) { return body; }
        Pair(left, right) { return left + right; }
        None { return ""; }
    }
}
fn main() {
    Msg message = Msg.Pair { right = "r", left = "l" };
    print(describe(message));
}
`,
	}, {
		description: "non-ASCII literals",
		root: `fn main() {
    string accented = "café";
    string wide = "日本語";
    string mixed = "aé中b";
    print(accented);
    print(wide);
    print(mixed);
}
`,
	}, {
		description: "literals from an imported module",
		root: `import "input2.txt";
fn main() {
    string local = "root-literal";
    print(local);
    print(fromOther());
    print(fromOther() + "tail");
}
`,
		imported: `public fn fromOther() -> string {
    string other = "imported-literal";
    print(other);
    return other;
}
`,
	}, {
		description: "a literal repeated across modules",
		root: `import "input2.txt";
fn main() {
    string local = "shared";
    print(local);
    print(fromOther());
}
`,
		imported: `public fn fromOther() -> string {
    return "shared";
}
`,
	}}
}

// TestKittenLiteralsMatchReference compares the probe's pooled declarations, and
// the expressions that produced them, with the reference generator's.
func TestKittenLiteralsMatchReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	entries := 0
	compared := 0
	for _, testCase := range literalCases() {
		modules, checked, generated, ok := checkProgram(t, testCase.description, testCase.root, testCase.imported)
		if !ok {
			continue
		}
		want := referenceLiteralPool(t, generated)
		expressions, pool := runProbe(t, executable, directory, testCase.root, testCase.imported)
		if pool != want {
			t.Errorf("%s: literal pool differs\n--- probe ---\n%s--- reference ---\n%s", testCase.description, pool, want)
			continue
		}
		if pool != "" {
			entries += strings.Count(pool, "static const ktn_string ")
		}
		// The expression lines must agree too, so a pooled symbol cannot be right
		// while the expression that refers to it is wrong.
		expected := referenceExpressions(t, modules, checked, generated)
		outcome := compareExpressions(t, testCase.description, expressions, expected)
		compared += outcome.compared
	}

	if entries < 20 {
		t.Fatalf("only %d pooled entries were compared", entries)
	}
	t.Logf("literal pools: %d programs, %d entries, %d expression lines compared", len(literalCases()), entries, compared)
}

// TestKittenLiteralsDeduplicate checks that equal literal text shares one entry and
// that every use refers to that one entry, which a comparison alone would not make
// obvious.
func TestKittenLiteralsDeduplicate(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	root := `fn main() {
    string a = "same";
    string b = "same";
    string c = "other";
    string d = "same" + "other" + "same";
    print(a);
    print(b);
    print(c);
    print(d);
}
`
	modules, checked, generated, ok := checkProgram(t, "deduplication", root, "")
	if !ok {
		t.Fatalf("the reference rejected the deduplication program")
	}
	expressions, pool := runProbe(t, executable, directory, root, "")
	if want := referenceLiteralPool(t, generated); pool != want {
		t.Fatalf("literal pool differs\n--- probe ---\n%s--- reference ---\n%s", pool, want)
	}

	// Two distinct literals, four uses.
	if count := strings.Count(pool, "static const ktn_string "); count != 2 {
		t.Errorf("the pool holds %d entries, want 2:\n%s", count, pool)
	}
	if !strings.Contains(pool, `ktn_literal_1 = { 4, "same" };`) {
		t.Errorf("the first entry is not the first literal used:\n%s", pool)
	}
	if !strings.Contains(pool, `ktn_literal_2 = { 5, "other" };`) {
		t.Errorf("the second entry is not the second literal used:\n%s", pool)
	}
	for _, symbol := range []string{"(&ktn_literal_1)", "(&ktn_literal_2)"} {
		if !strings.Contains(expressions, symbol) {
			t.Errorf("no expression refers to %s:\n%s", symbol, expressions)
		}
	}
	if strings.Contains(expressions, "ktn_literal_3") {
		t.Errorf("a third entry was created for a repeated literal:\n%s", expressions)
	}

	// The expressions must still agree with the reference.
	expected := referenceExpressions(t, modules, checked, generated)
	compareExpressions(t, "deduplication", expressions, expected)
}

// TestKittenLiteralsOrderFollowsFieldDeclaration pins the one place where literal
// discovery order differs from source order: the values of a struct literal are
// rendered in field declaration order, so a literal written last can be pooled
// first.
func TestKittenLiteralsOrderFollowsFieldDeclaration(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	root := `struct Pair {
    string first;
    string second;
}
fn main() {
    Pair pair = Pair { second = "written-first", first = "written-second" };
    print(pair.first);
}
`
	_, _, generated, ok := checkProgram(t, "field order", root, "")
	if !ok {
		t.Fatalf("the reference rejected the program")
	}
	_, pool := runProbe(t, executable, directory, root, "")
	if want := referenceLiteralPool(t, generated); pool != want {
		t.Fatalf("literal pool differs\n--- probe ---\n%s--- reference ---\n%s", pool, want)
	}

	// The reference renders the first declared field first, so the literal written
	// second in the source is the first pooled entry.
	first := strings.Index(pool, `"written-second"`)
	second := strings.Index(pool, `"written-first"`)
	if first < 0 || second < 0 {
		t.Fatalf("the pool does not hold both literals:\n%s", pool)
	}
	if first > second {
		t.Errorf("the pool follows source order rather than field declaration order:\n%s", pool)
	}
}

// --- Capacity ----------------------------------------------------------------

// TestKittenLiteralsCapacity checks the pool's entry capacity: an exact capacity
// succeeds and produces the reference's pool, one entry fewer reports the literal
// that did not fit, and a larger capacity changes nothing.
func TestKittenLiteralsCapacity(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	root := `fn main() {
    string one = "alpha";
    string two = "beta";
    string three = "gamma";
    string four = "alpha";
    print(one);
    print(two);
    print(three);
    print(four);
}
`
	_, _, generated, ok := checkProgram(t, "capacity", root, "")
	if !ok {
		t.Fatalf("the reference rejected the program")
	}
	want := referenceLiteralPool(t, generated)

	derived, derivedPool := runProbe(t, executable, directory, root, "")
	if derivedPool != want {
		t.Fatalf("the derived pool differs\n--- probe ---\n%s--- reference ---\n%s", derivedPool, want)
	}
	entries := strings.Count(derivedPool, "static const ktn_string ")
	if entries != 3 {
		t.Fatalf("the pool holds %d entries, want 3:\n%s", entries, derivedPool)
	}

	// The derived capacity is what the driver computes, so ask for the same number
	// explicitly: it must succeed and produce the same pool.
	//
	// The "#pool" line is an extra line in the source, so the line numbers the
	// expressions report shift by one; the pool itself carries no positions, which
	// is why the capacity checks compare the pool and the expression lines with the
	// shift accounted for.
	exact := fmt.Sprintf("#pool %d\n", entries)
	exactOutput, err := runKittenExpressions(executable, directory, exact+root, "")
	if err != nil {
		t.Fatalf("exact capacity: %v", err)
	}
	exactExpressions, exactPool := splitProbeOutput(t, exactOutput)
	if exactPool != want {
		t.Errorf("an exact capacity changed the pool\n--- probe ---\n%s--- reference ---\n%s", exactPool, want)
	}
	if got, expected := probeLines(exactExpressions), shiftLineNumbers(derived); !equalLines(got, expected) {
		t.Errorf("an exact capacity changed the expression lines beyond a one-line shift\n got:\n%s\nwant:\n%s", exactExpressions, derived)
	}

	// A roomier capacity must also produce the same pool.
	roomyOutput, err := runKittenExpressions(executable, directory, fmt.Sprintf("#pool %d\n", entries+8)+root, "")
	if err != nil {
		t.Fatalf("roomier capacity: %v", err)
	}
	if _, roomyPool := splitProbeOutput(t, roomyOutput); roomyPool != want {
		t.Errorf("a roomier capacity changed the pool\n--- probe ---\n%s--- reference ---\n%s", roomyPool, want)
	}

	// One entry fewer must be reported at the literal that did not fit, and the
	// pool must hold only the entries that did fit.
	shortOutput, err := runKittenExpressions(executable, directory, fmt.Sprintf("#pool %d\n", entries-1)+root, "")
	if err != nil {
		t.Fatalf("short capacity: %v", err)
	}
	shortExpressions, shortPool := splitProbeOutput(t, shortOutput)
	if !strings.Contains(shortExpressions, "fail ") {
		t.Errorf("a literal that did not fit was not reported:\n%s", shortExpressions)
	}
	if !strings.Contains(shortExpressions, "the literal pool does not fit in a capacity of 2 entries") {
		t.Errorf("the failure does not name the capacity:\n%s", shortExpressions)
	}
	if strings.Count(shortPool, "static const ktn_string ") != entries-1 {
		t.Errorf("the short pool holds %d entries, want %d:\n%s", strings.Count(shortPool, "static const ktn_string "), entries-1, shortPool)
	}
	if shortPool == want {
		t.Errorf("a pool that could not hold every literal reported the whole pool")
	}
	// The entries that did fit are the reference's entries, in order. The pool's
	// trailing blank line is not part of the entries, so it is trimmed before the
	// prefix comparison.
	shortEntries := strings.TrimSuffix(shortPool, "\n")
	wantEntries := strings.TrimSuffix(want, "\n")
	if !strings.HasPrefix(wantEntries, shortEntries) {
		t.Errorf("the short pool is not a prefix of the reference's\n--- short ---\n%s--- reference ---\n%s", shortPool, want)
	}
}

// shiftLineNumbers returns expression lines with one added to each reported line,
// which is what writing a directive line above a program does to its positions.
func shiftLineNumbers(expressions string) []string {
	var shifted []string
	for _, line := range probeLines(expressions) {
		fields := strings.Fields(line)
		if (fields[0] == "expr" || fields[0] == "fail") && len(fields) > 1 {
			if number, err := strconv.Atoi(fields[1]); err == nil {
				line = strings.Replace(line, " "+fields[1]+" ", fmt.Sprintf(" %d ", number+1), 1)
			}
		}
		shifted = append(shifted, line)
	}
	return shifted
}

// equalLines reports whether two line lists are equal.
func equalLines(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// --- Negative and boundary cases ---------------------------------------------

// TestKittenLiteralsRejectInvalidEscape checks that a literal the lexer rejects is
// reported as a lexical error and produces no pool. The reference's lexer rejects
// the same escape, so this is a property of the language rather than of the pool.
func TestKittenLiteralsRejectInvalidEscape(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	for _, root := range []string{
		"fn main() {\n    string bad = \"a\\qb\";\n}\n",
		"fn main() {\n    string unterminated = \"oops;\n}\n",
		"fn main() {\n    string newline = \"one\ntwo\";\n}\n",
	} {
		got, err := runKittenExpressions(executable, directory, root, "")
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.HasPrefix(got, "error Lex ") {
			t.Errorf("a rejected literal was not reported as a lexical error: %q", got)
		}
		if strings.Contains(got, "ktn_literal_") || strings.Contains(got, "=== literal pool ===") {
			t.Errorf("a rejected literal produced a pool:\n%s", got)
		}
	}
}

// TestKittenLiteralsEmptyPool checks that a program with no string literals
// produces no pool, which is what the reference does.
func TestKittenLiteralsEmptyPool(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	root := `fn main() {
    int a = 1;
    int b = 2;
    print(a + b);
}
`
	_, _, generated, ok := checkProgram(t, "empty pool", root, "")
	if !ok {
		t.Fatalf("the reference rejected the program")
	}
	if want := referenceLiteralPool(t, generated); want != "" {
		t.Fatalf("the reference pooled something for a program with no literals: %q", want)
	}
	expressions, pool := runProbe(t, executable, directory, root, "")
	if pool != "" {
		t.Errorf("a program with no literals produced a pool:\n%s", pool)
	}
	if !strings.Contains(expressions, "expr ") {
		t.Errorf("the program's expressions were not rendered:\n%s", expressions)
	}
}

// TestKittenLiteralsLongLiteralIsNotTruncated checks a literal long enough that a
// fixed-size buffer would show: every byte is pooled, and the byte length is the
// decoded length rather than the source length.
func TestKittenLiteralsLongLiteralIsNotTruncated(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	body := strings.Repeat("a", 400) + "\\n\\t" + strings.Repeat("b", 400)
	root := "fn main() {\n    string long = \"" + body + "\";\n    print(length(long));\n}\n"

	_, _, generated, ok := checkProgram(t, "long literal", root, "")
	if !ok {
		t.Fatalf("the reference rejected the program")
	}
	want := referenceLiteralPool(t, generated)
	_, pool := runProbe(t, executable, directory, root, "")
	if pool != want {
		t.Errorf("the pool for a long literal differs\n--- probe ---\n%s--- reference ---\n%s", pool, want)
	}
	// 400 + two decoded escapes + 400 bytes.
	if !strings.Contains(pool, "= { 802, ") {
		t.Errorf("the pooled length is not the decoded byte length:\n%s", pool)
	}
}

// --- Performance -------------------------------------------------------------

// manyLiteralsProgram builds a program with count distinct string literals, each
// used once, so that interning is measured over a growing pool.
func manyLiteralsProgram(count int) string {
	var builder strings.Builder
	builder.WriteString("fn main() {\n")
	for index := 0; index < count; index++ {
		fmt.Fprintf(&builder, "    string name%d = \"literal-%d\";\n", index, index)
	}
	for index := 0; index < count; index++ {
		fmt.Fprintf(&builder, "    print(name%d);\n", index)
	}
	builder.WriteString("}\n")
	return builder.String()
}

// TestKittenLiteralsScaling measures interning and emitting a growing pool, and
// checks the pool still agrees with the reference at each size.
func TestKittenLiteralsScaling(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenExpressions(t)
	directory := t.TempDir()

	previous := time.Duration(0)
	first := time.Duration(0)
	last := time.Duration(0)
	entries := 0
	for _, count := range []int{100, 200, 400} {
		root := manyLiteralsProgram(count)
		_, _, generated, ok := checkProgram(t, fmt.Sprintf("%d literals", count), root, "")
		if !ok {
			continue
		}
		want := referenceLiteralPool(t, generated)

		best := time.Duration(0)
		var pool string
		for repetition := 0; repetition < 3; repetition++ {
			start := time.Now()
			_, rendered := runProbe(t, executable, directory, root, "")
			if elapsed := time.Since(start); best == 0 || elapsed < best {
				best = elapsed
			}
			pool = rendered
		}
		if pool != want {
			t.Fatalf("%d literals: the pool differs\n--- probe ---\n%s--- reference ---\n%s", count, pool, want)
		}
		entries += strings.Count(pool, "static const ktn_string ")

		growth := ""
		if previous > 0 {
			growth = fmt.Sprintf(", x%.1f", float64(best)/float64(previous))
		}
		t.Logf("literals=%4d pool-bytes=%8d  %10s%s", count, len(pool), best.Round(time.Millisecond), growth)
		previous = best
		if first == 0 {
			first = best
		}
		last = best
	}

	if entries != 700 {
		t.Errorf("interning %d entries in total, want 700", entries)
	}
	// Four times the literals, and the bound is far looser than linear, so it only
	// catches a different order of cost.
	if last > first*100 {
		t.Errorf("a four-fold larger pool cost %v against %v, which is a different order of cost", last, first)
	}
}
