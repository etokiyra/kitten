package checker

import (
	"strings"
	"testing"

	"kitten/lexer"
	"kitten/parser"
)

// parseTestModule parses one source into a module with the given identity.
func parseTestModule(t *testing.T, path string, id int, source string) *parser.Module {
	t.Helper()

	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("lexing %s: %v", path, err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return &parser.Module{Path: path, ID: id, Program: program}
}

func TestTryReadFilePreludeInjectedOnUse(t *testing.T) {
	source := `fn main() {
    match (try_read_file("x")) {
        Ok(text) { print(text); }
        Failed(message) { print(message); }
    }
}
`
	checked, err := CheckModules([]*parser.Module{parseTestModule(t, "input.txt", 0, source)})
	if err != nil {
		t.Fatalf("checking try_read_file: %v", err)
	}
	if len(checked.Modules) != 2 {
		t.Fatalf("module count = %d, want root plus prelude", len(checked.Modules))
	}
	prelude := checked.Modules[1]
	if prelude.Path != preludeModulePath || prelude.ID != 1 {
		t.Errorf("prelude = path %q id %d, want %q id 1", prelude.Path, prelude.ID, preludeModulePath)
	}
	resultType, ok := checked.Types["ReadFileResult"]
	if !ok || resultType == nil || !resultType.IsUnion() {
		t.Errorf("ReadFileResult is not a visible union: %v", resultType)
	}
	function, ok := checked.Builtins["try_read_file"]
	if !ok || function == nil || function.Builtin != BuiltinTryReadFile {
		t.Errorf("try_read_file builtin = %+v", function)
	} else if resultType != nil && function.ReturnType != resultType {
		t.Errorf("try_read_file does not return the prelude type")
	}
}

func TestTryReadFilePreludeNotInjectedWithoutTrigger(t *testing.T) {
	checked, err := CheckModules([]*parser.Module{parseTestModule(t, "input.txt", 0, "fn main() {\n}\n")})
	if err != nil {
		t.Fatalf("checking plain program: %v", err)
	}
	if len(checked.Modules) != 1 {
		t.Errorf("module count = %d, want 1", len(checked.Modules))
	}
	if _, ok := checked.Types["ReadFileResult"]; ok {
		t.Errorf("ReadFileResult is visible without a trigger")
	}
	if _, ok := checked.Builtins["try_read_file"]; ok {
		t.Errorf("try_read_file is registered without a trigger")
	}
}

func TestTryReadFileNonExhaustiveMatchRejected(t *testing.T) {
	source := `fn main() {
    match (try_read_file("x")) {
        Ok(text) { print(text); }
    }
}
`
	checked, err := CheckModules([]*parser.Module{parseTestModule(t, "input.txt", 0, source)})
	if err == nil {
		t.Fatalf("non-exhaustive match accepted: %+v", checked)
	}
	if !strings.Contains(err.Error(), `does not handle variant "Failed"`) {
		t.Errorf("error %q does not report the missing Failed arm", err)
	}
}

func TestReadFileResultNameIsReserved(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"struct", "struct ReadFileResult {\n    int value;\n}\n"},
		{"function", "fn ReadFileResult() {\n}\n"},
		{"enum", "enum ReadFileResult { A }\n"},
		{"union", "union ReadFileResult { A }\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assertCheckError(t, testCase.source, DuplicateDeclaration, 1, 1,
				`cannot declare "ReadFileResult": the name is reserved for the predeclared type`)
		})
	}
}

func TestTryReadFileCanBeShadowed(t *testing.T) {
	source := `fn try_read_file(string path) -> int {
    return 1;
}
fn main() {
    print(try_read_file("x"));
}
`
	checked, err := CheckModules([]*parser.Module{parseTestModule(t, "input.txt", 0, source)})
	if err != nil {
		t.Fatalf("shadowed try_read_file rejected: %v", err)
	}
	// The syntactic trigger still injects the prelude; the user function wins at
	// the call site, which is the documented shadowing behavior.
	if len(checked.Modules) != 2 {
		t.Errorf("module count = %d, want root plus prelude", len(checked.Modules))
	}
}

func TestTryReadFileDirectConstruction(t *testing.T) {
	source := `fn main() {
    ReadFileResult value = ReadFileResult.Ok { text = "x" };
    match (value) {
        Ok(text) { print(text); }
        Failed(message) { print(message); }
    }
    match (try_read_file("x")) {
        Ok(text) { print(text); }
        Failed(message) { print(message); }
    }
}
`
	if _, err := CheckModules([]*parser.Module{parseTestModule(t, "input.txt", 0, source)}); err != nil {
		t.Fatalf("direct ReadFileResult construction rejected: %v", err)
	}
}

func TestTryReadFileVisibleFromImportedModule(t *testing.T) {
	root := `import "input2.txt";
fn main() {
    match (read()) {
        Ok(text) { print(text); }
        Failed(message) { print(message); }
    }
}
`
	imported := `public fn read() -> ReadFileResult {
    return try_read_file("x");
}
`
	modules := []*parser.Module{
		parseTestModule(t, "input.txt", 0, root),
		parseTestModule(t, "input2.txt", 1, imported),
	}
	checked, err := CheckModules(modules)
	if err != nil {
		t.Fatalf("imported try_read_file rejected: %v", err)
	}
	if len(checked.Modules) != 3 {
		t.Errorf("module count = %d, want root, import, prelude", len(checked.Modules))
	}
	if checked.Modules[2].Path != preludeModulePath || checked.Modules[2].ID != 2 {
		t.Errorf("prelude = path %q id %d, want %q id 2",
			checked.Modules[2].Path, checked.Modules[2].ID, preludeModulePath)
	}
}
