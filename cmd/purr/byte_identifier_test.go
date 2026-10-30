package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kitten/lexer"
)

// TestNoByteIdentifierTokens is the migration guard for Phase 8.3a/8.3b. It
// tokenizes every Kitten source file in the repository so comments and string
// contents cannot hide or falsely report the word, and fails when byte is still
// used as an identifier. After 8.3b, byte as a type keyword has its own token
// kind and is intentionally not reported here. The native lowering's
// `tests/native/bad` directory is skipped: its failure-mode sources
// intentionally include lexical errors, which are pinned as such.
func TestNoByteIdentifierTokens(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}

	var offenders []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}
			if entry.Name() == "bad" && strings.HasSuffix(filepath.ToSlash(path), "tests/native/bad") {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".ktn" {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		tokens, lexErr := lexer.Lex(string(source))
		if lexErr != nil {
			t.Errorf("%s: lexing failed: %v", path, lexErr)
			return nil
		}
		for _, token := range tokens {
			if token.Kind == lexer.Ident && token.Text == "byte" {
				offenders = append(offenders, fmt.Sprintf("%s:%d:%d", path, token.Line, token.Column))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking Kitten sources: %v", err)
	}
	if len(offenders) != 0 {
		t.Errorf("byte is still used as an identifier at:\n%s", joinOffenders(offenders))
	}
}

func joinOffenders(offenders []string) string {
	result := ""
	for _, offender := range offenders {
		result += offender + "\n"
	}
	return result
}
