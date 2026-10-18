package codegen

import (
	"strings"
	"testing"

	"kitten/checker"
	"kitten/lexer"
	"kitten/parser"
)

func parseReadFileModule(t *testing.T, path string, id int, source string) *parser.Module {
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

func TestTryReadFileCIsExact(t *testing.T) {
	const source = `fn main() {
    match (try_read_file("x")) {
        Ok(text) { print(text); }
        Failed(message) { print(message); }
    }
}
`
	generated := generateSource(t, source)

	expectedForward := `typedef struct ktn_s_1_ReadFileResult ktn_s_1_ReadFileResult;

enum {
    ktn_uc_1_ReadFileResult_Ok = 0,
    ktn_uc_1_ReadFileResult_Failed = 1,
};
`
	expectedStruct := `struct ktn_s_1_ReadFileResult {
    int64_t ktn_tag;
    const ktn_string * ktn_v_Ok_text;
    const ktn_string * ktn_v_Failed_message;
};
`
	expectedOkConstructor := `ktn_s_1_ReadFileResult *ktn_new_1_ReadFileResult_Ok(const ktn_string * ktn_arg_text) {
    ktn_s_1_ReadFileResult *ktn_object = (ktn_s_1_ReadFileResult *)ktn_rt_alloc(sizeof(ktn_s_1_ReadFileResult));
    ktn_object->ktn_tag = ktn_uc_1_ReadFileResult_Ok;
    ktn_object->ktn_v_Ok_text = ktn_arg_text;
    return ktn_object;
}
`
	expectedFailedConstructor := `ktn_s_1_ReadFileResult *ktn_new_1_ReadFileResult_Failed(const ktn_string * ktn_arg_message) {
    ktn_s_1_ReadFileResult *ktn_object = (ktn_s_1_ReadFileResult *)ktn_rt_alloc(sizeof(ktn_s_1_ReadFileResult));
    ktn_object->ktn_tag = ktn_uc_1_ReadFileResult_Failed;
    ktn_object->ktn_v_Failed_message = ktn_arg_message;
    return ktn_object;
}
`
	expectedHelper := `ktn_s_1_ReadFileResult *ktn_prelude_1_try_read_file(const ktn_string *ktn_path) {
    const ktn_string *ktn_text = NULL;
    const ktn_string *ktn_message = NULL;
    if (ktn_rt_try_read_file(ktn_path, &ktn_text, &ktn_message)) {
        return ktn_new_1_ReadFileResult_Ok(ktn_text);
    }
    return ktn_new_1_ReadFileResult_Failed(ktn_message);
}
`
	assertContains(t, generated, expectedForward)
	assertContains(t, generated, expectedStruct)
	assertContains(t, generated, expectedOkConstructor)
	assertContains(t, generated, expectedFailedConstructor)
	assertContains(t, generated, expectedHelper)
	if !strings.Contains(generated, "ktn_prelude_1_try_read_file(") {
		t.Errorf("try_read_file call was not lowered to the prelude helper:\n%s", generated)
	}
}

func TestTryReadFileDoesNotChangePlainProgram(t *testing.T) {
	generated := generateSource(t, "fn main() {\n}\n")
	if strings.Contains(generated, "ReadFileResult") || strings.Contains(generated, "ktn_prelude_") {
		t.Errorf("plain program gained prelude output:\n%s", generated)
	}
}

func TestTryReadFilePreludeUsesCheckedModules(t *testing.T) {
	root := parseReadFileModule(t, "input.txt", 0, `import "input2.txt";
fn main() {
    match (try_read_file("x")) {
        Ok(text) { print(text); }
        Failed(message) { print(message); }
    }
}
`)
	imported := parseReadFileModule(t, "input2.txt", 1, "public struct S {\n    int value;\n}\n")
	modules := []*parser.Module{root, imported}
	checked, err := checker.CheckModules(modules)
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	// Pass only the pre-prelude set: Generate must use checked.Modules.
	generated, err := Generate(modules, checked)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	if !strings.Contains(generated, "ktn_s_1_S") {
		t.Errorf("imported type was renumbered:\n%s", generated)
	}
	if !strings.Contains(generated, "ktn_s_2_ReadFileResult") ||
		!strings.Contains(generated, "ktn_prelude_2_try_read_file") {
		t.Errorf("prelude was not appended at module id 2:\n%s", generated)
	}
}
