package main

// Focused regression tests for the string representation.
//
// A Kitten string is a pointer to a runtime object that stores the byte length
// next to the bytes, so length and indexing are constant-time. These tests cover
// the behavior that the representation must preserve everywhere a string can be
// stored, passed, or returned, which is broader than the direct runtime helpers:
// empty and one-byte strings, multibyte UTF-8 bytes, first and last positions,
// the fatal error cases, substring boundaries, concatenation with empty
// operands, equality across construction histories, strings inside structs,
// arrays, maps, and tagged unions, strings crossing a module boundary, and the
// collector's ability to keep dynamically created string bytes alive.

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runFailingProgram builds and runs a program that must fail at runtime with a
// message containing wantMessage, and returns its output before the failure.
func runFailingProgram(t *testing.T, source, wantMessage string) string {
	t.Helper()

	stdout, stderr, exitCode := runCompiledProgram(t, source)
	if exitCode == 0 {
		t.Fatalf("program exited with status 0, want a nonzero status")
	}
	if !strings.Contains(stderr, wantMessage) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, wantMessage)
	}
	return stdout
}

func TestStringLengthAndIndexBoundaries(t *testing.T) {
	requireToolchain(t)

	source := `fn describe(string text) -> string {
    return text;
}
fn main() {
    string empty = "";
    print(length(empty), length("a"), length("hello"));
    print(length("é"), length("日本"), length("aé日"), length("emoji 😀"));

    string text = "hello";
    print(text[0], text[4]);
    print(describe(text)[0], describe(text)[4]);

    // A byte value is an int, so a multibyte character is several indices.
    print("é"[0], "é"[1]);
    print("日本"[0], "日本"[5]);

    // The empty string has no valid index, and the first byte of a one-byte
    // string is also its last.
    string one = "z";
    print(length(one), one[0]);
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := strings.Join([]string{
		"0 1 5",
		"2 6 6 10",
		"104 111",
		"104 111",
		"195 169",
		"230 172",
		"1 122",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestStringIndexErrors(t *testing.T) {
	requireToolchain(t)

	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "negative index",
			source: `fn main() {
    print("abc"[-1]);
}
`,
			want: "out of range",
		},
		{
			name: "index at length",
			source: `fn main() {
    string text = "abc";
    print(text[3]);
}
`,
			want: "out of range",
		},
		{
			name: "index far past length",
			source: `fn main() {
    print("abc"[99]);
}
`,
			want: "out of range",
		},
		{
			name: "index into empty string",
			source: `fn main() {
    string empty = "";
    print(empty[0]);
}
`,
			want: "out of range",
		},
		{
			name: "index of the zero-value string",
			source: `fn main() {
    string[] words = new string[1];
    print(words[0][0]);
}
`,
			want: "out of range",
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			runFailingProgram(t, testCase.source, testCase.want)
		})
	}
}

func TestSubstringBoundariesAndErrors(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    string text = "hello";
    print("[" + substring(text, 0, 0) + "]");
    print("[" + substring(text, 2, 2) + "]");
    print("[" + substring(text, 5, 5) + "]");
    print("[" + substring(text, 0, 5) + "]");
    print("[" + substring(text, 1, 4) + "]");
    print("[" + substring(text, 4, 5) + "]");
    print(length(substring(text, 1, 3)));

    // A substring of an empty string, and one taken from a constructed value.
    print("[" + substring("", 0, 0) + "]");
    print("[" + substring("a" + "b", 1, 2) + "]");

    // The result is an independent, immutable copy.
    string piece = substring(text, 0, 2);
    piece += "!";
    print(piece, text);

    // A byte range covers the bytes of a multibyte character.
    print(length(substring("aé", 1, 2)), length(substring("aé", 1, 3)));
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := strings.Join([]string{
		"[]",
		"[]",
		"[]",
		"[hello]",
		"[ell]",
		"[o]",
		"2",
		"[]",
		"[b]",
		"he! hello",
		"1 2",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}

	errors := []struct {
		name   string
		source string
	}{
		{
			name: "negative start",
			source: `fn main() {
    print(substring("abc", -1, 2));
}
`,
		},
		{
			name: "negative end",
			source: `fn main() {
    print(substring("abc", 0, -1));
}
`,
		},
		{
			name: "end past length",
			source: `fn main() {
    print(substring("abc", 0, 4));
}
`,
		},
		{
			name: "start after end",
			source: `fn main() {
    print(substring("abc", 2, 1));
}
`,
		},
		{
			name: "both past length",
			source: `fn main() {
    print(substring("abc", 4, 5));
}
`,
		},
	}

	for _, testCase := range errors {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			runFailingProgram(t, testCase.source, "out of range")
		})
	}
}

func TestStringConcatenationWithEmptyOperands(t *testing.T) {
	requireToolchain(t)

	source := `fn main() {
    string empty = "";
    print("[" + empty + "]");
    print("[" + (empty + empty) + "]");
    print("[" + ("a" + empty) + "]");
    print("[" + (empty + "b") + "]");

    // The zero value of a string is the empty string.
    string[] unassigned = new string[1];
    print("[" + unassigned[0] + "]", length(unassigned[0]), unassigned[0] == empty);

    // += keeps the operand's bytes and grows the value.
    string text = empty;
    text += "a";
    text += empty;
    text += "b";
    print(text, length(text), text == "ab");

    // Concatenating a literal with itself shares one static literal.
    print("xy" + "xy", length("xy" + "xy"));
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := strings.Join([]string{
		"[]",
		"[]",
		"[a]",
		"[b]",
		"[] 0 true",
		"ab 2 true",
		"xyxy 4",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestStringEqualityAcrossConstructionHistories(t *testing.T) {
	requireToolchain(t)

	source := `struct Holder {
    string text;
}
fn build(int repeats) -> string {
    string result = "";
    for (int index = 0; index < repeats; index++) {
        result += "ab";
    }
    return result;
}
fn main() {
    // The same content built four different ways.
    string literal = "abab";
    string concatenated = "ab" + "ab";
    string looped = build(2);
    string sliced = substring("xababx", 1, 5);
    string copied = build(3) + "x";
    copied = substring(copied, 0, 4);

    print(literal == concatenated, literal == looped, literal == sliced, literal == copied);
    print(literal != "ab", literal != "ababa");

    // A stored and retrieved value still compares by content.
    Holder holder = Holder { text = looped };
    print(holder.text == literal, holder.text == build(2));

    // Growing a value changes it, and equality reflects the change.
    holder.text += "a";
    print(holder.text == literal, holder.text == "ababa", length(holder.text));

    // A long shared prefix and equal lengths must still compare unequal.
    print(build(3) == build(3), build(3) != build(3) + "a");
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := strings.Join([]string{
		"true true true true",
		"true true",
		"true true",
		"false true 5",
		"true true",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestStringsInEveryContainer(t *testing.T) {
	requireToolchain(t)

	source := `struct Label {
    string text;
}
union Slot {
    Filled { string text; }
    Blank
}
fn main() {
    Label label = Label { text = "struct" };
    Label[] labels = new Label[2];
    labels[0] = Label { text = "first" };
    labels[1] = Label { text = "" };
    string[] words = new string[2];
    words[0] = "array" + "0";
    map[string]string table = new map[string]string;
    table["key"] = words[0];
    Slot slot = Slot.Filled { text = "union" };

    print(label.text, length(label.text));
    print(labels[0].text, labels[1].text == "", length(labels[1].text));
    print(words[0], length(words[1]), words[1] == "");
    print(table["key"], table["key"] == words[0], has(table, "key"));
    match (slot) {
        Filled(text) { print(text, length(text), text == "union"); }
        Blank { print("blank"); }
    }

    // Mutation through every container is still by value.
    label.text += "!";
    labels[0].text += "!";
    words[0] += "!";
    table["key"] += "!";
    print(label.text, labels[0].text, words[0], table["key"]);
    print(label.text == "struct!", table["key"] == "array0!");
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := strings.Join([]string{
		"struct 6",
		"first true 0",
		"array0 0 true",
		"array0 true true",
		"union 5 true",
		"struct! first! array0! array0!",
		"true true",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestStringsAcrossModulesAndFunctions(t *testing.T) {
	requireToolchain(t)

	project := t.TempDir()
	writeSource(t, filepath.Join(project, "text.ktn"), `public struct Label {
    string text;
}
public union Slot {
    Filled { string text; }
    Blank
}
public fn greet(string name) -> string {
    return "hello, " + name;
}
public fn repeat(string unit, int count) -> string {
    string result = "";
    for (int index = 0; index < count; index++) {
        result += unit;
    }
    return result;
}
public fn label_of(string text) -> Label {
    return Label { text = text };
}
public fn text_of(Slot slot) -> string {
    match (slot) {
        Filled(text) { return text; }
        Blank { return ""; }
    }
}
public fn table_with(string key, string value) -> map[string]string {
    map[string]string table = new map[string]string;
    table[key] = value;
    return table;
}
`)
	mainPath := writeSource(t, filepath.Join(project, "main.ktn"), `import "text.ktn";
fn main() {
    string greeting = greet("kitten");
    print(greeting, length(greeting));
    print(repeat("ab", 4), length(repeat("ab", 4)));

    Label label = label_of(greeting);
    print(label.text, label.text == "hello, kitten");

    print(text_of(Slot.Filled { text = repeat("x", 3) }));
    print(text_of(Slot.Blank) == "");

    map[string]string table = table_with("k", "v");
    print(table["k"], has(table, "k"), length(table));
}
`)

	executablePath := filepath.Join(t.TempDir(), "program")
	var diagnostics bytes.Buffer
	if err := buildExecutable(mainPath, executablePath, &diagnostics); err != nil {
		t.Fatalf("building: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics:\n%s", warnings)
	}

	output, err := exec.Command(executablePath).Output()
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	want := strings.Join([]string{
		"hello, kitten 13",
		"abababab 8",
		"hello, kitten true",
		"xxx",
		"true",
		"v true 1",
		"",
	}, "\n")
	if string(output) != want {
		t.Errorf("output = %q, want %q", output, want)
	}
}

func TestFileReadStringsCarryTheirOwnLength(t *testing.T) {
	requireToolchain(t)

	directory := t.TempDir()
	writeSource(t, filepath.Join(directory, "text.txt"), "alpha\n")
	writeSource(t, filepath.Join(directory, "empty.txt"), "")
	writeSource(t, filepath.Join(directory, "multibyte.txt"), "é日\n")

	source := `fn main() {
    string text = read_file("text.txt");
    print(length(text), text == "alpha\n", text[5], text[0]);
    print("[" + substring(text, 0, 5) + "]");

    string empty = read_file("empty.txt");
    print(length(empty), empty == "", empty == read_file("empty.txt"));

    string multibyte = read_file("multibyte.txt");
    print(length(multibyte), multibyte[0], multibyte[2]);

    // A read string concatenates and compares like any other string.
    print(read_file("text.txt") + read_file("text.txt") == "alpha\nalpha\n");
    print(read_file("text.txt") == text);
}
`
	stdout, _, exitCode := runCompiledProgramInDir(t, source, directory)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	want := strings.Join([]string{
		"6 true 10 97",
		"[alpha]",
		"0 true true",
		"6 195 230",
		"true",
		"true",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestGarbageCollectionKeepsStringBytesAlive(t *testing.T) {
	requireToolchain(t)

	source := `fn build(int repeats) -> string {
    string result = "";
    for (int index = 0; index < repeats; index++) {
        result += "ab";
    }
    return result;
}
fn make(int index) -> string {
    return build(index % 4);
}
fn main() {
    // Keep one string of every length, then churn many short-lived strings.
    string[] kept = new string[64];
    for (int index = 0; index < 64; index++) {
        kept[index] = make(index);
    }

    int churn = 0;
    for (int index = 0; index < 2000; index++) {
        string temporary = "x" + make(index % 3);
        churn += length(temporary);
    }

    int keptLengths = 0;
    for (int index = 0; index < 64; index++) {
        keptLengths += length(kept[index]);
    }
    print(churn, keptLengths);
    print(kept[0] == "", kept[1] == "ab", kept[2] == "abab", kept[3] == "ababab");

    // Keys are retained by the map, so key bytes stay alive after the churn.
    map[string]string table = new map[string]string;
    for (int index = 0; index < 200; index++) {
        table[make(index)] = build(index % 4);
    }
    print(length(table), has(table, "ab"), has(table, "missing"));

    // A map copied in a struct still reaches its keys.
    print(table["abab"] == "abab", length(table["ababab"]));
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	// 2000 iterations cycling over temporary lengths 1, 3 and 5: 667 of length
	// 1, 667 of length 3, and 666 of length 5.
	want := strings.Join([]string{
		"5998 192",
		"true true true true",
		"4 true false",
		"true 6",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}

func TestMutableStringSlotInEveryPosition(t *testing.T) {
	requireToolchain(t)

	// += takes the address of the target, so the target must be an assignable
	// slot in every position a string can occupy.
	source := `struct Label {
    string text;
}
union Slot {
    Filled { string text; }
    Blank
}
fn append_to(string[] words, int index) {
    words[index] += "!";
}
fn main() {
    string local = "a";
    local += "b";

    Label label = Label { text = "c" };
    label.text += "d";

    string[] words = new string[2];
    words[0] = "e";
    words[1] = "f";
    append_to(words, 1);

    map[string]string table = new map[string]string;
    table["g"] = "h";
    table["g"] += "i";

    print(local, label.text, words[0], words[1], table["g"]);
}
`
	stdout, _, exitCode := compileAndRunSource(t, source)
	if exitCode != 0 {
		t.Fatalf("program exited with status %d", exitCode)
	}
	if want := "ab cd e f! hi\n"; stdout != want {
		t.Errorf("output = %q, want %q", stdout, want)
	}
}
