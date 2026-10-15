package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule writes a source file, creating parent directories as needed, and
// returns its path.
func writeModule(t *testing.T, directory, name, source string) string {
	t.Helper()

	path := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating directory for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// canonical returns the canonical form the loader uses for a path.
func canonical(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return filepath.Clean(absolute)
}

func TestLoadSingleModule(t *testing.T) {
	directory := t.TempDir()
	root := writeModule(t, directory, "main.ktn", "fn main() {\n}\n")

	modules, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("loaded %d modules, want 1", len(modules))
	}
	if modules[0].ID != 0 {
		t.Errorf("root module ID = %d, want 0", modules[0].ID)
	}
	if modules[0].Path != canonical(t, root) {
		t.Errorf("root path = %q, want %q", modules[0].Path, canonical(t, root))
	}
}

func TestLoadImportsInDepthFirstOrder(t *testing.T) {
	directory := t.TempDir()
	writeModule(t, directory, "b.ktn", "public fn b() -> int {\n    return 1;\n}\n")
	writeModule(t, directory, "c.ktn", "public fn c() -> int {\n    return 2;\n}\n")
	root := writeModule(t, directory, "main.ktn", "import \"b.ktn\";\nimport \"sub/child.ktn\";\nfn main() {\n}\n")
	writeModule(t, directory, "sub/child.ktn", "import \"../c.ktn\";\npublic fn child() -> int {\n    return c();\n}\n")

	modules, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(modules) != 4 {
		t.Fatalf("loaded %d modules, want 4", len(modules))
	}

	wantOrder := []string{"main.ktn", "b.ktn", "sub/child.ktn", "c.ktn"}
	for index, name := range wantOrder {
		if modules[index].ID != index {
			t.Errorf("module %d has ID %d, want %d", index, modules[index].ID, index)
		}
		if filepath.Base(modules[index].Path) != filepath.Base(name) || !strings.HasSuffix(modules[index].Path, filepath.FromSlash(name)) {
			t.Errorf("module %d path = %q, want it to end with %q", index, modules[index].Path, name)
		}
	}
}

func TestLoadDiamondLoadsSharedModuleOnce(t *testing.T) {
	directory := t.TempDir()
	writeModule(t, directory, "shared.ktn", "public fn shared() -> int {\n    return 1;\n}\n")
	writeModule(t, directory, "left.ktn", "import \"shared.ktn\";\npublic fn left() -> int {\n    return shared();\n}\n")
	writeModule(t, directory, "right.ktn", "import \"shared.ktn\";\npublic fn right() -> int {\n    return shared();\n}\n")
	root := writeModule(t, directory, "main.ktn", "import \"left.ktn\";\nimport \"right.ktn\";\nfn main() {\n}\n")

	modules, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(modules) != 4 {
		t.Fatalf("loaded %d modules, want 4 (shared loaded once)", len(modules))
	}
}

func TestLoadAllowsImportCycles(t *testing.T) {
	directory := t.TempDir()
	writeModule(t, directory, "a.ktn", "import \"b.ktn\";\npublic fn a() -> int {\n    return b();\n}\n")
	writeModule(t, directory, "b.ktn", "import \"a.ktn\";\npublic fn b() -> int {\n    return 1;\n}\n")
	root := writeModule(t, directory, "main.ktn", "import \"a.ktn\";\nfn main() {\n}\n")

	modules, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error for an import cycle: %v", err)
	}
	if len(modules) != 3 {
		t.Fatalf("loaded %d modules, want 3", len(modules))
	}
}

func TestLoadCanonicalizesDifferentSpellings(t *testing.T) {
	directory := t.TempDir()
	writeModule(t, directory, "shared.ktn", "public fn shared() -> int {\n    return 1;\n}\n")
	root := writeModule(t, directory, "main.ktn", "import \"shared.ktn\";\nimport \"./shared.ktn\";\nfn main() {\n}\n")

	modules, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("loaded %d modules, want 2 (the same file imported twice)", len(modules))
	}
}

func TestLoadIsIndependentOfWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	writeModule(t, directory, "lib.ktn", "public fn lib() -> int {\n    return 1;\n}\n")
	root := writeModule(t, directory, "main.ktn", "import \"lib.ktn\";\nfn main() {\n}\n")

	t.Chdir(t.TempDir())

	modules, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("loaded %d modules, want 2", len(modules))
	}
}

func TestLoadMissingImport(t *testing.T) {
	directory := t.TempDir()
	root := writeModule(t, directory, "main.ktn", "import \"nope.ktn\";\nfn main() {\n}\n")

	_, err := Load(root)
	if err == nil {
		t.Fatalf("Load succeeded, want an error")
	}
	var loadError *Error
	if !asLoaderError(err, &loadError) {
		t.Fatalf("error is %T (%v), want *loader.Error", err, err)
	}
	if !strings.Contains(loadError.Error(), "nope.ktn") {
		t.Errorf("error %q does not name the missing module", loadError.Error())
	}
}

func TestLoadRejectsPathEscapingRoot(t *testing.T) {
	base := t.TempDir()
	projectDirectory := filepath.Join(base, "project")
	root := writeModule(t, projectDirectory, "main.ktn", "import \"../outside.ktn\";\nfn main() {\n}\n")
	writeModule(t, base, "outside.ktn", "public fn outside() -> int {\n    return 1;\n}\n")

	_, err := Load(root)
	if err == nil {
		t.Fatalf("Load succeeded, want a path-escape error")
	}
	if !strings.Contains(err.Error(), "outside the root directory") {
		t.Errorf("error %q does not mention the root directory", err.Error())
	}
}

func TestLoadReportsErrorsInImportedModules(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		contains string
	}{
		{name: "syntax error", source: "public fn broken() -> int {\n    return 1\n}\n", contains: "';'"},
		{name: "lexical error", source: "public fn broken() -> int {\n    return 1;\n}\nstring s = \"unterminated", contains: "unterminated string literal"},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			writeModule(t, directory, "bad.ktn", testCase.source)
			root := writeModule(t, directory, "main.ktn", "import \"bad.ktn\";\nfn main() {\n}\n")

			_, err := Load(root)
			if err == nil {
				t.Fatalf("Load succeeded, want an error")
			}
			var loadError *Error
			if !asLoaderError(err, &loadError) {
				t.Fatalf("error is %T (%v), want *loader.Error", err, err)
			}
			if !strings.HasSuffix(loadError.Path, "bad.ktn") {
				t.Errorf("error path = %q, want it to name bad.ktn", loadError.Path)
			}
			if loadError.Line == 0 || loadError.Column == 0 {
				t.Errorf("error position = %d:%d, want a position", loadError.Line, loadError.Column)
			}
			if !strings.Contains(loadError.Message, testCase.contains) {
				t.Errorf("message %q does not contain %q", loadError.Message, testCase.contains)
			}
		})
	}
}

func TestLoadMissingRoot(t *testing.T) {
	directory := t.TempDir()
	_, err := Load(filepath.Join(directory, "missing.ktn"))
	if err == nil {
		t.Fatalf("Load succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "cannot read source file") {
		t.Errorf("error %q does not explain the failure", err.Error())
	}
}

// asLoaderError is a small errors.As wrapper that keeps the import list tidy.
func asLoaderError(err error, target **Error) bool {
	loadError, ok := err.(*Error)
	if ok {
		*target = loadError
	}
	return ok
}
