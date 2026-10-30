package main

// Content-hashed caching for the test harness's repeated native builds.
//
// The differential suite builds the same large binaries over and over: the
// stage-1 driver, the Go CLI, both purr-kt builds, and the Kitten-written
// stage probes. Each build runs the full toolchain against Boehm GC, which is
// minutes of work on a modest machine per round. The cache keys a build by
// the content of everything it depends on — the source trees, the resolved C
// compiler, and the GC flags — and every cache entry is content-addressed by
// that key, so any change to a dependency invalidates the entry automatically
// and a hit can never serve stale bytes.
//
// Every produced entry is immutable: a build publishes its binary with an
// atomic rename after writing it under a temporary name, and callers treat
// the returned path as read-only. Two racing processes may both build the
// same key; the rename makes the loser's overwrite harmless, and the bytes
// are either identical or irrelevant.
//
// The cache is a development convenience for the verification tiers
// documented in AGENTS.md; it changes no claims: a build is verified exactly
// when it runs, and the cache only means that verified bytes get reused until
// their inputs change. Set KITTEN_TEST_CACHE to an explicit directory (or to
// "off") to control or disable it; the default is a directory next to the
// platform's user cache.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// verifyCacheDirectory resolves and creates the cache directory, or returns
// "" when caching is disabled or unavailable.
func verifyCacheDirectory(t *testing.T) string {
	t.Helper()
	value := os.Getenv("KITTEN_TEST_CACHE")
	if value == "off" {
		return ""
	}
	base := value
	if base == "" {
		defaultBase, err := os.UserCacheDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(defaultBase, "kitten-verify")
	}
	target := filepath.Join(base, "builds")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return ""
	}
	return target
}

// cachedBuildHash computes the content key for one kind of build: everything
// under roots, hashed in a deterministic order, with the compiler
// environment, the resolved compiler, and the GC flags folded in, so two
// builds share an entry only when every input matches.
func cachedBuildHash(t *testing.T, kind string, roots []string) string {
	t.Helper()
	hasher := sha256.New()
	fmt.Fprintf(hasher, "kind=%s\npartition=%s\n", kind, os.Getenv("KITTEN_TEST_CACHE"))

	// The compiler selection and the GC flags live in the key: binaries may
	// differ per compiler, and pkg-config output changes link lines.
	hasher.Write([]byte("CC=" + strings.Join(os.Environ(), "\n") + "\n"))
	if compiler, err := discoverCompiler(); err != nil {
		hasher.Write([]byte("compiler=unavailable\n"))
	} else {
		hasher.Write([]byte("compiler=" + strings.Join(compiler, "\x00") + "\n"))
	}
	compileFlags, linkFlags := gcBuildFlags()
	hasher.Write([]byte("flags=" + strings.Join(compileFlags, "\x00") + "\x00" + strings.Join(linkFlags, "\x00") + "\n"))

	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			if strings.HasSuffix(path, ".ktn") || strings.HasSuffix(path, ".c") || strings.HasSuffix(path, ".h") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			hasher.Write([]byte("root=" + root + ":error\n"))
			continue
		}
	}
	sort.Strings(files)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(hasher, "file=%s:error\n", path)
			continue
		}
		fmt.Fprintf(hasher, "file=%s:%d:", path, len(data))
		hasher.Write(data)
		hasher.Write([]byte("\x00"))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// fileHash hashes one file's bytes, for keys that depend on another build's
// output.
func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "unavailable"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// cachedBuild returns the compiled binary for one content key, building and
// publishing it when the cache has no entry. The returned path is immutable:
// callers may run it and must not overwrite or delete it. A failed build is
// never published.
func cachedBuild(t *testing.T, kind, key string, build func(path string) error) string {
	t.Helper()
	directory := verifyCacheDirectory(t)
	if directory == "" {
		fallback := filepath.Join(t.TempDir(), kind)
		if err := build(fallback); err != nil {
			t.Fatalf("building %s: %v", kind, err)
		}
		return fallback
	}
	entry := filepath.Join(directory, kind+"-"+key)
	if information, err := os.Stat(entry); err == nil && information.Size() > 0 && information.Mode().Perm()&0o111 != 0 {
		return entry
	}
	temporary := entry + ".partial-" + filepath.Base(t.TempDir())
	if err := build(temporary); err != nil {
		if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
			t.Fatalf("cleaning a failed build %s: %v", kind, err)
		}
		t.Fatalf("building %s: %v", kind, err)
	}
	if err := os.Chmod(temporary, 0o755); err != nil {
		t.Fatalf("chmod a new build %s: %v", kind, err)
	}
	if err := os.Rename(temporary, entry); err != nil {
		// Another process may have published the same key first; either
		// winner's bytes are content-correct for this key.
		_ = os.Remove(temporary)
	}
	return entry
}
