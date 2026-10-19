package checker

import (
	"strings"
	"testing"

	"kitten/parser"
)

func TestPreludeSourceSubsets(t *testing.T) {
	cases := []struct {
		name string
		uses preludeUses
		want string
	}{
		{"read only", preludeUses{readFile: true}, readFileResultPrelude},
		{"write file only", preludeUses{writeFile: true}, fileResultPrelude},
		{"write bytes only", preludeUses{writeBytes: true}, fileResultPrelude},
		{"create directory only", preludeUses{createDirectory: true}, fileResultPrelude},
		{"remove only", preludeUses{remove: true}, fileResultPrelude},
		{"list only", preludeUses{listDirectory: true}, listDirectoryResultPrelude},
		{"write and list", preludeUses{writeFile: true, listDirectory: true}, fileResultPrelude + listDirectoryResultPrelude},
		{"read and write", preludeUses{readFile: true, writeBytes: true}, readFileResultPrelude + fileResultPrelude},
		{"read list", preludeUses{readFile: true, listDirectory: true}, readFileResultPrelude + listDirectoryResultPrelude},
		{"run only", preludeUses{run: true}, runResultPrelude},
		{"read and run", preludeUses{readFile: true, run: true}, readFileResultPrelude + runResultPrelude},
		{"current directory only", preludeUses{currentDirectory: true}, currentDirectoryResultPrelude},
		{"current directory and run", preludeUses{currentDirectory: true, run: true}, runResultPrelude + currentDirectoryResultPrelude},
		{"environment only", preludeUses{environment: true}, envResultPrelude},
		{"environment and run", preludeUses{environment: true, run: true}, runResultPrelude + envResultPrelude},
		{"capture only", preludeUses{capture: true}, capturedRunResultPrelude},
		{"capture and environment", preludeUses{capture: true, environment: true}, envResultPrelude + capturedRunResultPrelude},
		{"file info only", preludeUses{fileInfo: true}, fileInfoResultPrelude},
		{"file info and capture", preludeUses{fileInfo: true, capture: true}, capturedRunResultPrelude + fileInfoResultPrelude},
		{"temp directory only", preludeUses{tempDirectory: true}, tempDirectoryResultPrelude},
		{"temp directory and file info", preludeUses{tempDirectory: true, fileInfo: true}, fileInfoResultPrelude + tempDirectoryResultPrelude},
		{"all", preludeUses{readFile: true, writeFile: true, writeBytes: true, createDirectory: true, remove: true, listDirectory: true, run: true, currentDirectory: true, environment: true, capture: true, fileInfo: true, tempDirectory: true}, readFileResultPrelude + fileResultPrelude + listDirectoryResultPrelude + runResultPrelude + currentDirectoryResultPrelude + envResultPrelude + capturedRunResultPrelude + fileInfoResultPrelude + tempDirectoryResultPrelude},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := preludeSource(testCase.uses); got != testCase.want {
				t.Errorf("prelude source = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestPreludeModuleSubsets(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		types       []string
		unusedTypes []string
		builtin     string
	}{
		{"no trigger", "fn main() {\n}\n", nil, []string{"ReadFileResult", "FileResult", "ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"}, ""},
		{"read", "fn main() {\n    try_read_file(\"x\");\n}\n", []string{"ReadFileResult"}, []string{"FileResult", "ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"}, "try_read_file"},
		{"write", "fn main() {\n    try_write_file(\"x\", \"y\");\n}\n", []string{"FileResult"}, []string{"ReadFileResult", "ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"}, "try_write_file"},
		{"write bytes", "fn main() {\n    try_write_bytes(\"x\", new byte[1]);\n}\n", []string{"FileResult"}, []string{"ReadFileResult", "ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"}, "try_write_bytes"},
		{"create", "fn main() {\n    try_create_directory(\"x\");\n}\n", []string{"FileResult"}, []string{"ReadFileResult", "ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"}, "try_create_directory"},
		{"remove", "fn main() {\n    try_remove(\"x\");\n}\n", []string{"FileResult"}, []string{"ReadFileResult", "ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"}, "try_remove"},
		{"list", "fn main() {\n    try_list_directory(\"x\");\n}\n", []string{"ListDirectoryResult"}, []string{"ReadFileResult", "FileResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"}, "try_list_directory"},
		{"run", "fn main() {\n    try_run(\"/bin/true\", new string[0]);\n}\n", []string{"RunResult"}, []string{"ReadFileResult", "FileResult", "ListDirectoryResult", "CurrentDirectoryResult"}, "try_run"},
		{"read and write", "fn main() {\n    try_read_file(\"x\");\n    try_write_file(\"y\", \"z\");\n}\n", []string{"ReadFileResult", "FileResult"}, []string{"ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"}, "try_write_file"},
		{"temp directory", "fn main() {\n    try_create_temp_directory(\"x\");\n}\n", []string{"TempDirectoryResult"}, []string{"ReadFileResult", "FileResult", "ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult"}, "try_create_temp_directory"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checked, err := CheckModules([]*parser.Module{parseTestModule(t, "input.txt", 0, testCase.source)})
			if err != nil {
				t.Fatalf("checking: %v", err)
			}
			wantModules := 1
			if testCase.builtin != "" {
				wantModules = 2
			}
			if len(checked.Modules) != wantModules {
				t.Errorf("module count = %d, want %d", len(checked.Modules), wantModules)
			}
			for _, name := range testCase.types {
				if _, ok := checked.Types[name]; !ok {
					t.Errorf("%s is not visible", name)
				}
			}
			for _, name := range testCase.unusedTypes {
				if _, ok := checked.Types[name]; ok {
					t.Errorf("%s is visible without a matching call", name)
				}
			}
			if testCase.builtin == "" {
				return
			}
			function, ok := checked.Builtins[testCase.builtin]
			if !ok || function == nil {
				t.Fatalf("%s is not registered", testCase.builtin)
			}
			if function.ReturnType == nil {
				t.Errorf("%s has no return type", testCase.builtin)
			}
		})
	}
}

func TestFileAndDirectoryResultNamesAreReserved(t *testing.T) {
	kinds := []struct {
		name   string
		source string
	}{
		{"function", "fn %s() {\n}\n"},
		{"struct", "struct %s {\n    int value;\n}\n"},
		{"enum", "enum %s { A }\n"},
		{"union", "union %s { A }\n"},
	}
	for _, typeName := range []string{"FileResult", "ListDirectoryResult", "RunResult", "CurrentDirectoryResult", "EnvResult", "CapturedRunResult", "FileInfoResult", "TempDirectoryResult"} {
		for _, kind := range kinds {
			t.Run(typeName+"/"+kind.name, func(t *testing.T) {
				source := strings.Replace(kind.source, "%s", typeName, 1)
				checked, err := CheckModules([]*parser.Module{parseTestModule(t, "input.txt", 0, source)})
				if err == nil {
					t.Fatalf("declaring %s as a %s was accepted: %+v", typeName, kind.name, checked)
				}
				want := "cannot declare \"" + typeName + "\": the name is reserved for the predeclared type"
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			})
		}
	}
}

func TestFileDirectoryBuiltinsHaveExactSignatures(t *testing.T) {
	source := `fn main() {
    try_write_file("x", "y");
    try_write_bytes("x", new byte[1]);
    try_create_directory("x");
    try_remove("x");
    try_list_directory("x");
    try_run("/bin/true", new string[0]);
    try_current_directory();
    try_getenv("PATH");
    try_run_capture("/bin/true", new string[0]);
    try_file_info("x");
    try_create_temp_directory("x");
}
`
	checked, err := CheckModules([]*parser.Module{parseTestModule(t, "input.txt", 0, source)})
	if err != nil {
		t.Fatalf("checking builtins: %v", err)
	}
	if len(checked.Modules) != 2 {
		t.Fatalf("module count = %d, want root plus one prelude", len(checked.Modules))
	}
	fileResult := checked.Types["FileResult"]
	listResult := checked.Types["ListDirectoryResult"]
	runResult := checked.Types["RunResult"]
	currentResult := checked.Types["CurrentDirectoryResult"]
	envResult := checked.Types["EnvResult"]
	captureResult := checked.Types["CapturedRunResult"]
	fileInfoResult := checked.Types["FileInfoResult"]
	tempDirectoryResult := checked.Types["TempDirectoryResult"]
	if fileResult == nil || listResult == nil || runResult == nil || currentResult == nil || envResult == nil || captureResult == nil || fileInfoResult == nil || tempDirectoryResult == nil {
		t.Fatalf("prelude result types are missing")
	}
	checks := []struct {
		name    string
		builtin BuiltinKind
		params  int
		result  *Type
	}{
		{"try_write_file", BuiltinTryWriteFile, 2, fileResult},
		{"try_write_bytes", BuiltinTryWriteBytes, 2, fileResult},
		{"try_create_directory", BuiltinTryCreateDirectory, 1, fileResult},
		{"try_remove", BuiltinTryRemove, 1, fileResult},
		{"try_list_directory", BuiltinTryListDirectory, 1, listResult},
		{"try_run", BuiltinTryRun, 2, runResult},
		{"try_current_directory", BuiltinTryCurrentDirectory, 0, currentResult},
		{"try_getenv", BuiltinTryGetenv, 1, envResult},
		{"try_run_capture", BuiltinTryRunCapture, 2, captureResult},
		{"try_file_info", BuiltinTryFileInfo, 1, fileInfoResult},
		{"try_create_temp_directory", BuiltinTryCreateTempDirectory, 1, tempDirectoryResult},
	}
	for _, check := range checks {
		function, ok := checked.Builtins[check.name]
		if !ok || function == nil {
			t.Fatalf("%s is not registered", check.name)
		}
		if function.Builtin != check.builtin || len(function.ParameterTypes) != check.params || function.ReturnType != check.result {
			t.Errorf("%s = %+v, want builtin %v with %d parameters and result %v", check.name, function, check.builtin, check.params, check.result)
		}
	}
}
