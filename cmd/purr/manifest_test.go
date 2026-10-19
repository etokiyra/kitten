package main

// Tests for the strict purr.toml subset parser and its probe.

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type manifestCase struct {
	name   string
	text   string
	stdout string
	stderr string
	status int
}

func manifestExpected(text string) manifestCase {
	name, version, message := oracleParseManifest([]byte(text))
	if message != "" {
		return manifestCase{text: text, stderr: message + "\n", status: 1}
	}
	return manifestCase{text: text, stdout: "ok " + name + " " + version + "\n", status: 0}
}

func buildManifestProbeWithGo(t *testing.T) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "manifest_probe_go")
	var diagnostics bytes.Buffer
	if err := buildExecutable(repoPath(t, "..", "..", "purr", "manifest_probe.ktn"), output, &diagnostics); err != nil {
		t.Fatalf("building manifest probe with Go pipeline: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C diagnostics while building manifest probe:\n%s", warnings)
	}
	return output
}

func buildManifestProbeWithKitten(t *testing.T, stage1 string) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "manifest_probe_kitten")
	result := runExternal(t, wrapperPath(t), []string{
		"build", repoPath(t, "..", "..", "purr", "manifest_probe.ktn"), "-o", output,
	}, repoPath(t, "..", ".."), wrapperEnvironment(stage1, t))
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("building manifest probe with Kitten pipeline: status %d, stderr %q", result.status, result.stderr)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("Kitten-pipeline probe was not produced: %v", err)
	}
	return output
}

func runManifestProbe(t *testing.T, executable, text string) (string, string, int) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "purr.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing manifest fixture: %v", err)
	}
	result := runExternal(t, executable, []string{path}, directory, nil)
	return result.stdout, result.stderr, result.status
}

func checkManifestCase(t *testing.T, executable, pipeline string, testCase manifestCase) {
	t.Helper()
	stdout, stderr, status := runManifestProbe(t, executable, testCase.text)
	if stdout != testCase.stdout || stderr != testCase.stderr || status != testCase.status {
		t.Errorf("%s %q: got stdout %q, stderr %q, status %d; want stdout %q, stderr %q, status %d",
			pipeline, testCase.name, stdout, stderr, status, testCase.stdout, testCase.stderr, testCase.status)
	}
}

func randomManifestName(random *rand.Rand) string {
	letters := "abcdefghijklmnopqrstuvwxyz0123456789_-"
	length := 1 + random.Intn(24)
	var builder strings.Builder
	builder.WriteByte("abcdefghijklmnopqrstuvwxyz"[random.Intn(26)])
	for index := 1; index < length; index++ {
		builder.WriteByte(letters[random.Intn(len(letters))])
	}
	name := builder.String()
	if strings.HasSuffix(name, "-") || strings.HasSuffix(name, "_") {
		name += "a"
	}
	return name
}

func randomManifestVersion(random *rand.Rand) string {
	parts := []string{}
	for index := 0; index < 3; index++ {
		length := 1 + random.Intn(9)
		var builder strings.Builder
		for digit := 0; digit < length; digit++ {
			builder.WriteByte(byte('0' + random.Intn(10)))
		}
		part := builder.String()
		if len(part) > 1 && part[0] == '0' {
			part = "1" + part[1:]
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ".")
}

func randomValidManifest(random *rand.Rand) string {
	var builder strings.Builder
	if random.Intn(2) == 0 {
		builder.WriteString("# leading comment\n")
	}
	if random.Intn(2) == 0 {
		builder.WriteString("\n")
	}
	builder.WriteString("[package]")
	if random.Intn(2) == 0 {
		builder.WriteString(" # table comment")
	}
	builder.WriteString("\n")
	name := randomManifestName(random)
	version := randomManifestVersion(random)
	first := "name"
	second := "version"
	if random.Intn(2) == 1 {
		first, second = second, first
	}
	for _, key := range []string{first, second} {
		if random.Intn(3) == 0 {
			builder.WriteString("# key comment\n")
		}
		builder.WriteString(key)
		if random.Intn(2) == 0 {
			builder.WriteString(randomWhitespace(random))
		}
		builder.WriteString("=")
		if random.Intn(2) == 0 {
			builder.WriteString(randomWhitespace(random))
		}
		builder.WriteByte('"')
		if key == "name" {
			builder.WriteString(name)
		} else {
			builder.WriteString(version)
		}
		builder.WriteByte('"')
		if random.Intn(2) == 0 {
			builder.WriteString(randomWhitespace(random))
		}
		if random.Intn(2) == 0 {
			builder.WriteString("# value comment")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func randomWhitespace(random *rand.Rand) string {
	var builder strings.Builder
	for index := 0; index < random.Intn(4); index++ {
		if random.Intn(2) == 0 {
			builder.WriteByte(' ')
		} else {
			builder.WriteByte('\t')
		}
	}
	return builder.String()
}

func mutateManifest(random *rand.Rand, source string) string {
	if source == "" {
		return source
	}
	data := []byte(source)
	alphabet := []byte("abcXYZ019[]\"'#=.-_ \t\n\r")
	switch random.Intn(5) {
	case 0:
		data[random.Intn(len(data))] = alphabet[random.Intn(len(alphabet))]
	case 1:
		index := random.Intn(len(data))
		data = append(data[:index], data[index+1:]...)
	case 2:
		index := random.Intn(len(data) + 1)
		value := alphabet[random.Intn(len(alphabet))]
		data = append(data, 0)
		copy(data[index+1:], data[index:])
		data[index] = value
	case 3:
		return strings.ReplaceAll(source, "\n", "\r\n")
	case 4:
		return strings.ReplaceAll(source, "\r\n", "\n")
	}
	return string(data)
}

var manifestPinnedCases = []manifestCase{
	{"basic", "[package]\nname = \"hello\"\nversion = \"1.2.3\"\n", "ok hello 1.2.3\n", "", 0},
	{"order", "[package]\nversion = \"0.0.0\"\nname = \"a\"\n", "ok a 0.0.0\n", "", 0},
	{"no final newline", "[package]\nname = \"hello\"\nversion = \"1.2.3\"", "ok hello 1.2.3\n", "", 0},
	{"crlf", "[package]\r\nname = \"hello\"\r\nversion = \"1.2.3\"\r\n", "ok hello 1.2.3\n", "", 0},
	{"crlf no final newline", "[package]\r\nname = \"hello\"\r\nversion = \"1.2.3\"", "ok hello 1.2.3\n", "", 0},
	{"leading comments", "# one\n\n# two\n[package]\nname = \"hello\"\nversion = \"1.2.3\"\n", "ok hello 1.2.3\n", "", 0},
	{"header comment", "[package] # hi\nname = \"hello\"\nversion = \"1.2.3\"\n", "ok hello 1.2.3\n", "", 0},
	{"header comment no space", "[package]#hi\nname = \"hello\"\nversion = \"1.2.3\"\n", "ok hello 1.2.3\n", "", 0},
	{"value comments", "[package]\nname = \"hello\" # x\nversion = \"1.2.3\" # y\n", "ok hello 1.2.3\n", "", 0},
	{"blank whitespace", "[package]\n \t \nname = \"hello\"\n\t\nversion = \"1.2.3\"\n", "ok hello 1.2.3\n", "", 0},
	{"header trailing ws", "[package] \t \nname = \"hello\"\nversion = \"1.2.3\"\n", "ok hello 1.2.3\n", "", 0},
	{"equals ws", "[package]\nname \t= \t\"hello\"\nversion\t=\t\"1.2.3\"\n", "ok hello 1.2.3\n", "", 0},
	{"name one byte", "[package]\nname = \"a\"\nversion = \"1.2.3\"\n", "ok a 1.2.3\n", "", 0},
	{"name 64", "[package]\nname = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\nversion = \"1.2.3\"\n", "ok aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1.2.3\n", "", 0},
	{"name allowed", "[package]\nname = \"a0_-b\"\nversion = \"1.2.3\"\n", "ok a0_-b 1.2.3\n", "", 0},
	{"version zero", "[package]\nname = \"a\"\nversion = \"0.0.0\"\n", "ok a 0.0.0\n", "", 0},
	{"version larger", "[package]\nname = \"a\"\nversion = \"1.10.100\"\n", "ok a 1.10.100\n", "", 0},
	{"version nine", "[package]\nname = \"a\"\nversion = \"999999999.1.2\"\n", "ok a 999999999.1.2\n", "", 0},
	{"trailing tabs", "[package]\nname = \"hello\"\t\nversion = \"1.2.3\"\t", "ok hello 1.2.3\n", "", 0},
	{"comment eof", "[package]\nname = \"hello\"\nversion = \"1.2.3\"\n# eof", "ok hello 1.2.3\n", "", 0},
	{"empty", "", "", "purr.toml:1:1: expected [package]\n", 1},
	{"bom", "\ufeff[package]\nname = \"a\"\nversion = \"1.2.3\"\n", "", "purr.toml:1:1: expected [package]\n", 1},
	{"only comment", "# only\n", "", "purr.toml:2:1: expected [package]\n", 1},
	{"only blank", "\n \n", "", "purr.toml:3:1: expected [package]\n", 1},
	{"leading space header", " [package]\n", "", "purr.toml:1:1: expected [package]\n", 1},
	{"leading tab header", "\t[package]\n", "", "purr.toml:1:1: expected [package]\n", 1},
	{"key before header", "name = \"a\"\n", "", "purr.toml:1:1: expected [package]\n", 1},
	{"text before header", "hello\n", "", "purr.toml:1:1: expected [package]\n", 1},
	{"cr at start", "\r[package]\n", "", "purr.toml:1:1: stray carriage return\n", 1},
	{"spaces only", "   ", "", "purr.toml:1:4: expected [package]\n", 1},
	{"comments only eof", "# x", "", "purr.toml:1:4: expected [package]\n", 1},
	{"comment high", "# café\n", "", "purr.toml:2:1: expected [package]\n", 1},
	{"unexpected dependencies", "[dependencies]\n", "", "purr.toml:1:1: unexpected table [dependencies]\n", 1},
	{"unexpected package dot", "[package.x]\n", "", "purr.toml:1:1: unexpected table [package.x]\n", 1},
	{"unexpected array", "[[array]]\n", "", "purr.toml:1:1: unexpected table [[array]]\n", 1},
	{"unexpected spaces", "[ package ]\n", "", "purr.toml:1:1: unexpected table [ package ]\n", 1},
	{"unexpected missing close", "[package\n", "", "purr.toml:1:1: unexpected table [package\n", 1},
	{"unexpected trailing no space", "[package]x\n", "", "purr.toml:1:10: unexpected text after value\n", 1},
	{"unexpected after header space", "[package] [x]\n", "", "purr.toml:1:11: unexpected text after value\n", 1},
	{"unexpected a", "[a]\n", "", "purr.toml:1:1: unexpected table [a]\n", 1},
	{"unexpected control", "[\x01]\n", "", "purr.toml:1:1: unexpected table [\\x01]\n", 1},
	{"unexpected high", "[é]\n", "", "purr.toml:1:1: unexpected table [\\xc3\\xa9]\n", 1},
	{"unexpected double package", "[[package]]\n", "", "purr.toml:1:1: unexpected table [[package]]\n", 1},
	{"unexpected after blank", "\n[dependencies]\n", "", "purr.toml:2:1: unexpected table [dependencies]\n", 1},
	{"quoted key", "[package]\n\"name\" = \"a\"\n", "", "purr.toml:2:1: expected key name or version\n", 1},
	{"digit key", "[package]\n1 = \"a\"\n", "", "purr.toml:2:1: expected key name or version\n", 1},
	{"underscore key", "[package]\n_name = \"a\"\n", "", "purr.toml:2:1: expected key name or version\n", 1},
	{"hyphen key", "[package]\n-name = \"a\"\n", "", "purr.toml:2:1: expected key name or version\n", 1},
	{"dot key", "[package]\n.name = \"a\"\n", "", "purr.toml:2:1: expected key name or version\n", 1},
	{"indented key", "[package]\n  name = \"a\"\n", "", "purr.toml:2:1: expected key name or version\n", 1},
	{"tab indented key", "[package]\n\tname = \"a\"\n", "", "purr.toml:2:1: expected key name or version\n", 1},
	{"equals key", "[package]\n= \"a\"\n", "", "purr.toml:2:1: expected key name or version\n", 1},
	{"comment then indented key", "[package]\n# c\n  name = \"a\"\n", "", "purr.toml:3:1: expected key name or version\n", 1},
	{"unknown edition", "[package]\nedition = \"x\"\n", "", "purr.toml:2:1: unknown key edition\n", 1},
	{"unknown namex", "[package]\nnamex = \"x\"\n", "", "purr.toml:2:1: unknown key namex\n", 1},
	{"unknown dotted", "[package]\nname.extra = \"x\"\n", "", "purr.toml:2:1: unknown key name.extra\n", 1},
	{"unknown hyphen", "[package]\nfoo-bar = \"x\"\n", "", "purr.toml:2:1: unknown key foo-bar\n", 1},
	{"unknown true", "[package]\ntrue = \"x\"\n", "", "purr.toml:2:1: unknown key true\n", 1},
	{"unknown capital", "[package]\nName = \"x\"\n", "", "purr.toml:2:1: unknown key Name\n", 1},
	{"unknown slash", "[package]\nname/thing = \"x\"\n", "", "purr.toml:2:5: expected = after key name\n", 1},
	{"unknown versionx", "[package]\nversionx = \"x\"\n", "", "purr.toml:2:1: unknown key versionx\n", 1},
	{"unknown abc", "[package]\nabc = \"x\"\n", "", "purr.toml:2:1: unknown key abc\n", 1},
	{"missing equals", "[package]\nname \"a\"\n", "", "purr.toml:2:6: expected = after key name\n", 1},
	{"missing equals newline", "[package]\nname\n", "", "purr.toml:2:5: expected = after key name\n", 1},
	{"missing equals comment", "[package]\nname # c\n", "", "purr.toml:2:6: expected = after key name\n", 1},
	{"missing equals version", "[package]\nversion \"1.2.3\"\n", "", "purr.toml:2:9: expected = after key version\n", 1},
	{"missing equals version newline", "[package]\nversion\n", "", "purr.toml:2:8: expected = after key version\n", 1},
	{"missing equals colon", "[package]\nname: \"a\"\n", "", "purr.toml:2:5: expected = after key name\n", 1},
	{"string integer", "[package]\nname = 1\n", "", "purr.toml:2:8: expected a string value for name\n", 1},
	{"string boolean", "[package]\nname = true\n", "", "purr.toml:2:8: expected a string value for name\n", 1},
	{"string single quote", "[package]\nname = 'a'\n", "", "purr.toml:2:8: expected a string value for name\n", 1},
	{"string triple", "[package]\nname = \"\"\"a\"\"\"\n", "", "purr.toml:2:10: unexpected text after value\n", 1},
	{"string missing eof", "[package]\nname =", "", "purr.toml:2:7: expected a string value for name\n", 1},
	{"string missing comment", "[package]\nname = # c\n", "", "purr.toml:2:8: expected a string value for name\n", 1},
	{"string missing bracket", "[package]\nname = [\n", "", "purr.toml:2:8: expected a string value for name\n", 1},
	{"string version integer", "[package]\nversion = 1\n", "", "purr.toml:2:11: expected a string value for version\n", 1},
	{"string version null", "[package]\nversion = null\n", "", "purr.toml:2:11: expected a string value for version\n", 1},
	{"string missing newline", "[package]\nname = \n", "", "purr.toml:2:8: expected a string value for name\n", 1},
	{"unterminated eof", "[package]\nname = \"x", "", "purr.toml:2:8: unterminated string\n", 1},
	{"unterminated empty eof", "[package]\nname = \"", "", "purr.toml:2:8: unterminated string\n", 1},
	{"unterminated newline", "[package]\nname = \"x\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: unterminated string\n", 1},
	{"unterminated crlf", "[package]\nname = \"x\r\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: unterminated string\n", 1},
	{"unterminated backslash eof", "[package]\nname = \"x\\", "", "purr.toml:2:8: unterminated string\n", 1},
	{"unterminated newline two", "[package]\nversion = \"1.2.3\n", "", "purr.toml:2:11: unterminated string\n", 1},
	{"unterminated crlf two", "[package]\nversion = \"1.2.3\r\n", "", "purr.toml:2:11: unterminated string\n", 1},
	{"invalid escape u", "[package]\nname = \"a\\u0062\"\n", "", "purr.toml:2:11: invalid escape \\u\n", 1},
	{"invalid escape q", "[package]\nname = \"a\\q\"\n", "", "purr.toml:2:11: invalid escape \\q\n", 1},
	{"invalid escape quote", "[package]\nname = \"a\\'\"\n", "", "purr.toml:2:11: invalid escape \\'\n", 1},
	{"invalid escape zero", "[package]\nname = \"a\\0\"\n", "", "purr.toml:2:11: invalid escape \\0\n", 1},
	{"invalid escape space", "[package]\nname = \"a\\ \"\n", "", "purr.toml:2:11: invalid escape \\ \n", 1},
	{"invalid escape version", "[package]\nversion = \"1\\q.2.3\"\n", "", "purr.toml:2:14: invalid escape \\q\n", 1},
	{"invalid escape version two", "[package]\nversion = \"1.2.\\u0033\"\n", "", "purr.toml:2:17: invalid escape \\u\n", 1},
	{"invalid escape control", "[package]\nname = \"a\\\x01\"\n", "", "purr.toml:2:11: invalid escape \\\\x01\n", 1},
	{"stray start", "\r", "", "purr.toml:1:1: stray carriage return\n", 1},
	{"stray header", "[package]\r", "", "purr.toml:1:10: stray carriage return\n", 1},
	{"stray after name", "[package]\nname = \"a\"\r", "", "purr.toml:2:11: stray carriage return\n", 1},
	{"stray after name text", "[package]\nname = \"a\"\rversion = \"1.2.3\"\n", "", "purr.toml:2:11: stray carriage return\n", 1},
	{"stray string", "[package]\nname = \"a\rb\"\n", "", "purr.toml:2:10: stray carriage return\n", 1},
	{"stray comment", "[package]\n# comment\r", "", "purr.toml:2:10: stray carriage return\n", 1},
	{"stray blank", "[package]\n \r", "", "purr.toml:2:2: stray carriage return\n", 1},
	{"stray tab", "\t\r", "", "purr.toml:1:2: stray carriage return\n", 1},
	{"text after value", "[package]\nname = \"a\" x\n", "", "purr.toml:2:12: unexpected text after value\n", 1},
	{"text after value quote", "[package]\nname = \"a\" \"b\"\n", "", "purr.toml:2:12: unexpected text after value\n", 1},
	{"text after header", "[package] x\n", "", "purr.toml:1:11: unexpected text after value\n", 1},
	{"text after header digit", "[package] 1\n", "", "purr.toml:1:11: unexpected text after value\n", 1},
	{"text after value tab", "[package]\nname = \"a\"\t x\n", "", "purr.toml:2:13: unexpected text after value\n", 1},
	{"text after value control", "[package]\nname = \"a\"\v\n", "", "purr.toml:2:11: unexpected text after value\n", 1},
	{"duplicate name", "[package]\nname = \"a\"\nname = \"b\"\nversion = \"1.2.3\"\n", "", "purr.toml:3:1: duplicate key name\n", 1},
	{"duplicate name later", "[package]\nname = \"a\"\nversion = \"1.2.3\"\nname = \"b\"\n", "", "purr.toml:4:1: duplicate key name\n", 1},
	{"duplicate version", "[package]\nversion = \"1.2.3\"\nversion = \"1.2.4\"\nname = \"a\"\n", "", "purr.toml:3:1: duplicate key version\n", 1},
	{"duplicate version later", "[package]\nname = \"a\"\nversion = \"1.2.3\"\n# c\nversion = \"1.2.4\"\n", "", "purr.toml:5:1: duplicate key version\n", 1},
	{"missing name header", "[package]\n", "", "purr.toml:2:1: missing key name\n", 1},
	{"missing name no newline", "[package]", "", "purr.toml:1:10: missing key name\n", 1},
	{"missing name version only", "[package]\nversion = \"1.2.3\"\n", "", "purr.toml:3:1: missing key name\n", 1},
	{"missing name version no newline", "[package]\nversion = \"1.2.3\"", "", "purr.toml:2:18: missing key name\n", 1},
	{"missing name comments", "[package]\n# c\n", "", "purr.toml:3:1: missing key name\n", 1},
	{"missing version name only", "[package]\nname = \"a\"\n", "", "purr.toml:3:1: missing key version\n", 1},
	{"missing version no newline", "[package]\nname = \"a\"", "", "purr.toml:2:11: missing key version\n", 1},
	{"missing version comments", "[package]\nname = \"a\"\n# c\n", "", "purr.toml:4:1: missing key version\n", 1},
	{"missing version crlf", "[package]\r\nname = \"a\"\r\n", "", "purr.toml:3:1: missing key version\n", 1},
	{"duplicate table", "[package]\n[package]\n", "", "purr.toml:2:1: duplicate [package] table\n", 1},
	{"duplicate table after keys", "[package]\nname = \"a\"\nversion = \"1.2.3\"\n[package]\n", "", "purr.toml:4:1: duplicate [package] table\n", 1},
	{"duplicate table after name", "[package]\nname = \"a\"\n[package]\nversion = \"1.2.3\"\n", "", "purr.toml:3:1: duplicate [package] table\n", 1},
	{"duplicate table comment", "[package]\n# c\n\n[package]\n", "", "purr.toml:4:1: duplicate [package] table\n", 1},
	{"duplicate table crlf", "[package]\r\n[package]\r\n", "", "purr.toml:2:1: duplicate [package] table\n", 1},
	{"name empty", "[package]\nname = \"\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name : must not be empty\n", 1},
	{"name empty version", "[package]\nversion = \"1.2.3\"\nname = \"\"\n", "", "purr.toml:3:8: invalid package name : must not be empty\n", 1},
	{"name 65", "[package]\nname = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: must be at most 64 bytes\n", 1},
	{"name 65 version", "[package]\nversion = \"1.2.3\"\nname = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n", "", "purr.toml:3:8: invalid package name aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: must be at most 64 bytes\n", 1},
	{"name upper first", "[package]\nname = \"Hello\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name Hello: must start with a lowercase letter\n", 1},
	{"name digit first", "[package]\nname = \"1abc\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name 1abc: must start with a lowercase letter\n", 1},
	{"name underscore first", "[package]\nname = \"_abc\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name _abc: must start with a lowercase letter\n", 1},
	{"name hyphen first", "[package]\nname = \"-abc\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name -abc: must start with a lowercase letter\n", 1},
	{"name high first", "[package]\nname = \"éabc\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name \\xc3\\xa9abc: must start with a lowercase letter\n", 1},
	{"name dot", "[package]\nname = \"a.b\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name a.b: may contain only lowercase letters, digits, _ and -\n", 1},
	{"name upper rest", "[package]\nname = \"aB\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name aB: may contain only lowercase letters, digits, _ and -\n", 1},
	{"name space", "[package]\nname = \"a b\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name a b: may contain only lowercase letters, digits, _ and -\n", 1},
	{"name end hyphen", "[package]\nname = \"a-\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name a-: must not end with - or _\n", 1},
	{"name end underscore", "[package]\nname = \"a_\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name a_: must not end with - or _\n", 1},
	{"name quote", "[package]\nname = \"a\\\"b\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name a\"b: may contain only lowercase letters, digits, _ and -\n", 1},
	{"name tab", "[package]\nname = \"a\\t\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name a\\x09: may contain only lowercase letters, digits, _ and -\n", 1},
	{"name newline", "[package]\nname = \"a\\n\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name a\\x0a: may contain only lowercase letters, digits, _ and -\n", 1},
	{"name high", "[package]\nname = \"aé\"\nversion = \"1.2.3\"\n", "", "purr.toml:2:8: invalid package name a\\xc3\\xa9: may contain only lowercase letters, digits, _ and -\n", 1},
	{"version two", "[package]\nname = \"a\"\nversion = \"1.2\"\n", "", "purr.toml:3:11: invalid version 1.2: expected MAJOR.MINOR.PATCH\n", 1},
	{"version four", "[package]\nname = \"a\"\nversion = \"1.2.3.4\"\n", "", "purr.toml:3:11: invalid version 1.2.3.4: expected MAJOR.MINOR.PATCH\n", 1},
	{"version empty part", "[package]\nname = \"a\"\nversion = \"1..2\"\n", "", "purr.toml:3:11: invalid version 1..2: expected MAJOR.MINOR.PATCH\n", 1},
	{"version trailing dot", "[package]\nname = \"a\"\nversion = \"1.2.\"\n", "", "purr.toml:3:11: invalid version 1.2.: expected MAJOR.MINOR.PATCH\n", 1},
	{"version leading dot", "[package]\nname = \"a\"\nversion = \".1.2\"\n", "", "purr.toml:3:11: invalid version .1.2: expected MAJOR.MINOR.PATCH\n", 1},
	{"version v prefix", "[package]\nname = \"a\"\nversion = \"v1.2.3\"\n", "", "purr.toml:3:11: invalid version v1.2.3: a version part must have 1 to 9 digits\n", 1},
	{"version non digit", "[package]\nname = \"a\"\nversion = \"1.2.x\"\n", "", "purr.toml:3:11: invalid version 1.2.x: a version part must have 1 to 9 digits\n", 1},
	{"version suffix", "[package]\nname = \"a\"\nversion = \"1.2.3a\"\n", "", "purr.toml:3:11: invalid version 1.2.3a: a version part must have 1 to 9 digits\n", 1},
	{"version ten digits", "[package]\nname = \"a\"\nversion = \"1.2.1234567890\"\n", "", "purr.toml:3:11: invalid version 1.2.1234567890: a version part must have 1 to 9 digits\n", 1},
	{"version nine digits", "[package]\nname = \"a\"\nversion = \"1.2.123456789\"\n", "ok a 1.2.123456789\n", "", 0},
	{"version leading zero", "[package]\nname = \"a\"\nversion = \"01.2.3\"\n", "", "purr.toml:3:11: invalid version 01.2.3: a version part must not have a leading zero\n", 1},
	{"version leading zero two", "[package]\nname = \"a\"\nversion = \"1.02.3\"\n", "", "purr.toml:3:11: invalid version 1.02.3: a version part must not have a leading zero\n", 1},
	{"version leading zero three", "[package]\nname = \"a\"\nversion = \"1.2.03\"\n", "", "purr.toml:3:11: invalid version 1.2.03: a version part must not have a leading zero\n", 1},
	{"version space", "[package]\nname = \"a\"\nversion = \"1.2.3 \"\n", "", "purr.toml:3:11: invalid version 1.2.3 : a version part must have 1 to 9 digits\n", 1},
	{"version plus", "[package]\nname = \"a\"\nversion = \"1.2.+3\"\n", "", "purr.toml:3:11: invalid version 1.2.+3: a version part must have 1 to 9 digits\n", 1},
	{"version minus", "[package]\nname = \"a\"\nversion = \"1.2.-3\"\n", "", "purr.toml:3:11: invalid version 1.2.-3: a version part must have 1 to 9 digits\n", 1},
	{"version escaped newline", "[package]\nname = \"a\"\nversion = \"1.2.3\\n\"\n", "", "purr.toml:3:11: invalid version 1.2.3\\x0a: a version part must have 1 to 9 digits\n", 1},
	{"version escaped cr", "[package]\nname = \"a\"\nversion = \"1.2.3\\r\"\n", "", "purr.toml:3:11: invalid version 1.2.3\\x0d: a version part must have 1 to 9 digits\n", 1},
	{"version escaped tab", "[package]\nname = \"a\"\nversion = \"1.2.3\\t\"\n", "", "purr.toml:3:11: invalid version 1.2.3\\x09: a version part must have 1 to 9 digits\n", 1},
	{"version high", "[package]\nname = \"a\"\nversion = \"1.2.é\"\n", "", "purr.toml:3:11: invalid version 1.2.\\xc3\\xa9: a version part must have 1 to 9 digits\n", 1},
	{"version four dots", "[package]\nname = \"a\"\nversion = \"1.2.3.4.5\"\n", "", "purr.toml:3:11: invalid version 1.2.3.4.5: expected MAJOR.MINOR.PATCH\n", 1},
}

func TestManifestPinned(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	goProbe := buildManifestProbeWithGo(t)
	kittenProbe := buildManifestProbeWithKitten(t, stage1)
	for _, testCase := range manifestPinnedCases {
		expected := manifestExpected(testCase.text)
		if expected.stdout != testCase.stdout || expected.stderr != testCase.stderr || expected.status != testCase.status {
			t.Fatalf("%s: oracle disagrees with the pinned output", testCase.name)
		}
		checkManifestCase(t, goProbe, "Go", testCase)
		checkManifestCase(t, kittenProbe, "Kitten", testCase)
	}

	probeSource, err := os.ReadFile(repoPath(t, "..", "..", "purr", "manifest_probe.ktn"))
	if err != nil {
		t.Fatalf("reading manifest probe: %v", err)
	}
	manifestSource, err := os.ReadFile(repoPath(t, "..", "..", "purr", "manifest.ktn"))
	if err != nil {
		t.Fatalf("reading manifest parser: %v", err)
	}
	_, _, goC, ok := checkProgram(t, "manifest probe C", string(probeSource), string(manifestSource))
	if !ok {
		return
	}
	kittenC, err := runKittenUnit(buildKittenUnit(t), t.TempDir(), string(probeSource), string(manifestSource))
	if err != nil {
		t.Fatalf("generating manifest probe C with Kitten pipeline: %v", err)
	}
	if difference := unitDifference(kittenC, goC); difference != "" {
		t.Fatalf("manifest probe C differs: %s", difference)
	}
}

func TestManifestUsage(t *testing.T) {
	probe := buildManifestProbeWithGo(t)
	result := runExternal(t, probe, nil, t.TempDir(), nil)
	if result.status != 2 || result.stderr != "usage: manifest_probe <purr.toml>\n" || result.stdout != "" {
		t.Errorf("usage: status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	result = runExternal(t, probe, []string{filepath.Join(t.TempDir(), "missing.toml")}, t.TempDir(), nil)
	if result.status != 1 || result.stdout != "" || !strings.Contains(result.stderr, "no such file or directory") {
		t.Errorf("missing file: status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
}

func TestManifestRandomizedBothPipelines(t *testing.T) {
	requireToolchain(t)
	stage1 := buildStage1(t)
	goProbe := buildManifestProbeWithGo(t)
	kittenProbe := buildManifestProbeWithKitten(t, stage1)
	directory := t.TempDir()
	path := filepath.Join(directory, "purr.toml")
	random := rand.New(rand.NewSource(9062026))
	for index := 0; index < 5000; index++ {
		source := randomValidManifest(random)
		mutations := random.Intn(4)
		for mutation := 0; mutation < mutations; mutation++ {
			source = mutateManifest(random, source)
		}
		expected := manifestExpected(source)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("writing randomized manifest: %v", err)
		}
		for pipeline, executable := range map[string]string{"Go": goProbe, "Kitten": kittenProbe} {
			result := runExternal(t, executable, []string{path}, directory, nil)
			if result.stdout != expected.stdout || result.stderr != expected.stderr || result.status != expected.status {
				t.Fatalf("random %d %s: input %q\nprobe stdout %q, stderr %q, status %d\noracle stdout %q, stderr %q, status %d",
					index, pipeline, source, result.stdout, result.stderr, result.status,
					expected.stdout, expected.stderr, expected.status)
			}
		}
	}
}

func TestManifestPerformance(t *testing.T) {
	probe := buildManifestProbeWithGo(t)
	directory := t.TempDir()
	small := strings.Repeat("#", 256*1024)
	large := strings.Repeat("#", 1024*1024)
	path := filepath.Join(directory, "purr.toml")

	measure := func(text string, want manifestCase) time.Duration {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatalf("writing performance input: %v", err)
		}
		start := time.Now()
		for run := 0; run < 5; run++ {
			result := runExternal(t, probe, []string{path}, directory, nil)
			if result.stdout != want.stdout || result.stderr != want.stderr || result.status != want.status {
				t.Fatalf("performance input differed: got %d/%d/%d bytes, want %d/%d/%d",
					len(result.stdout), len(result.stderr), result.status,
					len(want.stdout), len(want.stderr), want.status)
			}
		}
		return time.Since(start) / 5
	}

	smallExpected := manifestCase{stderr: "purr.toml:1:262145: expected [package]\n", status: 1}
	largeExpected := manifestCase{stderr: "purr.toml:1:1048577: expected [package]\n", status: 1}
	smallTime := measure(small, smallExpected)
	largeTime := measure(large, largeExpected)
	t.Logf("comments: 256 KiB %s, 1 MiB %s, ratio %.2f", smallTime, largeTime, float64(largeTime)/float64(smallTime))
	if largeTime >= time.Second {
		t.Errorf("1 MiB comment file took %s, want under 1s", largeTime)
	}
	if largeTime >= smallTime*8 {
		t.Errorf("1 MiB / 256 KiB ratio is %.2f, want well under 16", float64(largeTime)/float64(smallTime))
	}

	hugeName := "[package]\nname = \"" + strings.Repeat("a", 1024*1024) + "\"\nversion = \"1.2.3\"\n"
	if err := os.WriteFile(path, []byte(hugeName), 0o644); err != nil {
		t.Fatalf("writing huge value: %v", err)
	}
	wantHuge := manifestCase{
		stderr: "purr.toml:2:8: invalid package name " + strings.Repeat("a", 1024*1024) + ": must be at most 64 bytes\n",
		status: 1,
	}
	start := time.Now()
	result := runExternal(t, probe, []string{path}, directory, nil)
	elapsed := time.Since(start)
	if result.stdout != wantHuge.stdout || result.stderr != wantHuge.stderr || result.status != wantHuge.status {
		t.Fatalf("1 MiB value differed: got %d/%d/%d bytes, want %d/%d/%d",
			len(result.stdout), len(result.stderr), result.status,
			len(wantHuge.stdout), len(wantHuge.stderr), wantHuge.status)
	}
	t.Logf("1 MiB name value: %s", elapsed)
	if elapsed >= time.Second {
		t.Errorf("1 MiB value took %s, want under 1s", elapsed)
	}

	garbage := "[package]\n" + strings.Repeat("x", 1024*1024)
	if err := os.WriteFile(path, []byte(garbage), 0o644); err != nil {
		t.Fatalf("writing deep garbage: %v", err)
	}
	wantGarbage := manifestCase{
		stderr: "purr.toml:2:1: unknown key " + strings.Repeat("x", 1024*1024) + "\n",
		status: 1,
	}
	start = time.Now()
	result = runExternal(t, probe, []string{path}, directory, nil)
	elapsed = time.Since(start)
	if result.stdout != wantGarbage.stdout || result.stderr != wantGarbage.stderr || result.status != wantGarbage.status {
		t.Fatalf("deep garbage differed: got %d/%d/%d bytes, want %d/%d/%d",
			len(result.stdout), len(result.stderr), result.status,
			len(wantGarbage.stdout), len(wantGarbage.stderr), wantGarbage.status)
	}
	t.Logf("1 MiB unknown key: %s", elapsed)
	if elapsed >= time.Second {
		t.Errorf("1 MiB garbage took %s, want under 1s", elapsed)
	}
}

func TestManifestSanitizer(t *testing.T) {
	requireToolchain(t)
	probeSource, err := os.ReadFile(repoPath(t, "..", "..", "purr", "manifest_probe.ktn"))
	if err != nil {
		t.Fatalf("reading manifest probe: %v", err)
	}
	manifestSource, err := os.ReadFile(repoPath(t, "..", "..", "purr", "manifest.ktn"))
	if err != nil {
		t.Fatalf("reading manifest parser: %v", err)
	}
	_, _, text, ok := checkProgram(t, "manifest sanitizer", string(probeSource), string(manifestSource))
	if !ok {
		return
	}
	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()
	compileFlags = append(compileFlags, "-fsanitize=undefined")
	executable, err := buildKittenRunProgram(t, compiler, compileFlags, linkFlags, text)
	if err != nil {
		t.Skipf("undefined behavior sanitizer is unavailable with %s: %v", filepath.Base(compiler[0]), err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "purr.toml")
	for _, testCase := range manifestPinnedCases {
		if err := os.WriteFile(path, []byte(testCase.text), 0o644); err != nil {
			t.Fatalf("writing sanitizer manifest: %v", err)
		}
		result := runExternal(t, executable, []string{path}, directory, nil)
		if result.stdout != testCase.stdout || result.stderr != testCase.stderr || result.status != testCase.status {
			t.Fatalf("%s: sanitizer stdout %q, stderr %q, status %d", testCase.name, result.stdout, result.stderr, result.status)
		}
	}
}
