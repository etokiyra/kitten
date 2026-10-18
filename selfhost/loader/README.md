# A module loader for Kitten, written in Kitten

`selfhost/loader` loads a root source file together with the modules it imports
and returns the ordered module set the Kitten-written checker and C generator
consume. It mirrors `loader/loader.go`: the same canonical paths, depth-first
pre-order, ids equal to positions in that order, one module per canonical path,
cycles allowed, duplicate and diamond imports deduplicated, and the same
diagnostics for paths outside the root and for lexical or syntax errors.

It is a self-hosted component, not part of the production pipeline. `purr build`
and `purr run` still use the Go loader, which remains authoritative.

## Files

- `selfhost/loader/paths.ktn` — lexical path cleaning, parent directories, joins,
  and the within-root test, matching Go's `path/filepath` on Linux.
- `selfhost/loader/loader.ktn` — `load_program`, the loader state, parsing each
  module with the Kitten-written lexer and parser, and import recursion.
- `selfhost/load.ktn` — the loader driver. It reads an absolute root path from
  `input.txt` and prints the ordered module set or the loader's failure.
- `selfhost/loadunit.ktn` — the whole-path driver. It loads, checks, and emits a
  complete C translation unit with the Kitten-written stages.
- `cmd/purr/loader_differential_test.go` — the differential harness.

## API

```kitten
public fn load_program(string root_path) -> LoadResult
```

`LoadResult` is:

- `Success { Module[] modules; int module_count; }`, with the root first, imported
  modules in depth-first import order, and `module.id` equal to its index;
- `Failure { LoadError error; }`, with `LoadError { path; line; column; message }`.

`Module` is the checker's existing parsed-module type: canonical path, id, parsed
program, and the parser's expression and type-reference counts. The loader feeds
it to `check_modules` and the emitters unchanged. The loader never injects the
`ReadFileResult` prelude; the checker appends it at `id = module_count` when a
module contains a syntactic `try_read_file` call.

`canonical_path`, `clean_path`, `parent_directory`, `join_path`, and
`within_directory` are public because they are the complete lexical path model
the loader uses; nothing in them reads the filesystem.

## What the loader does

1. Canonicalize the absolute root path with `clean_path`.
2. Set the resolution root to the root file's parent directory.
3. Load a module with this order, exactly as `loader.Load` does:
   - return immediately when its canonical path is already loaded, which
     terminates cycles and deduplicates duplicate and diamond imports;
   - reject a canonical path outside the resolution root with
     `import resolves outside the root directory <rootDirectory>`, naming the
     canonical path and carrying no line or column;
   - call `read_file` for the source;
   - lex and parse, wrapping a failure with the module's canonical path and the
     frontend's line, column, and message;
   - register the module and append it to the ordered list **before** loading its
     imports, then recurse in declaration order with id equal to the current
     module count.
4. Copy the ordered list into an exactly sized array for the result.

There is no diagnostic for a cycle or a duplicate import: the reference permits
cycles and loads a file once, and SPEC.md Section 22.5 says the same. The list
grows by doubling while modules are discovered.

### Paths

`clean_path` reproduces `filepath.Clean` for Linux: it collapses empty segments,
drops `.` components, resolves each `..` against the preceding component, drops a
leading `..` on an absolute path, keeps a leading `..` on a relative one, and
removes a trailing separator except for `/` itself. `parent_directory` matches
`filepath.Dir`. `join_path(directory, importPath)` concatenates with `/` and
cleans, so an import that looks absolute still joins **inside** the importing
file's directory, exactly as `filepath.Join` does. `within_directory` is the
lexical equivalent of the reference's `filepath.Rel` check, including the
separator boundary (`/rooted` is not inside `/root`).

A filename such as `..hidden.ktn` is an ordinary component; only the exact
component `..` is a parent.

## Two documented exclusions

**An absolute root path is required.** The self-hosted runtime exposes no working
directory, so a relative root cannot be made absolute the way `filepath.Abs` does.
The harness always supplies an absolute root. The reference loader also accepts
relative roots; that difference is not exercised here.

**Missing and unreadable files are fatal and unmatched.** SPEC.md Section 22.2
defines `read_file` as fatal on a missing, unreadable, non-regular, oversized, or
failed read, and Section 22.9 defers recoverable I/O errors. The loader therefore
calls `read_file` directly; it cannot return the reference loader's
`cannot read source file: ...` diagnostic. There is no file-exists primitive and
no preloaded manifest. This remains the 4E.2 gap, blocked on a SPEC decision for
recoverable file input.

## Running the drivers

The loaders read the root path from `input.txt`; the working directory selects it.

```sh
purr build selfhost/load.ktn -o /tmp/load-probe
purr build selfhost/loadunit.ktn -o /tmp/loadunit-probe
mkdir -p /tmp/example && cd /tmp/example
printf '%s' /absolute/path/to/main.ktn > input.txt
/tmp/load-probe
```

The loader driver prints:

```text
ok <module_count>
module <id> <canonical_path>
...
```

or one failure line in the reference loader's three shapes:

```text
error <path>:<line>:<column>: <message>
error <path>: <message>
error <message>
```

The whole-path driver prints the complete C translation unit on success and a
single diagnostic line otherwise.

## Differential testing

`cmd/purr/loader_differential_test.go` compares against `loader.Load` for the
same absolute root:

- **20 targeted trees**, covering a simple import, diamonds, cycles, self-import,
  duplicate spellings, nested directories, `..` inside and outside the root,
  modules imported from two places, imports among declarations, redundant
  separators, absolute-looking imports, dot-prefixed filenames, and lexical and
  syntax errors in imported modules;
- **`tests/modules/`**, the real nested fixture, with its actual canonical paths;
- **15 multi-file checker corpus programs**, materialized as `input.txt` and
  `input2.txt`, including malformed modules;
- **the full path** over `tests/modules/`: load, check, and emit with the
  Kitten-written stages, compare the unit with the Go reference, then compile,
  link, run, and compare the expected output under the compiler `CC` selects.

The harness compares canonical paths, ids, order, and diagnostics byte for byte,
and reports zero mismatches. Running the suite under GCC and Clang covers the
pipeline under both.

## What is not here

- Recoverable file input and therefore missing/unreadable-file diagnostics
  (4E.2; needs a SPEC decision).
- Relative root paths (no working-directory primitive).
- A compiler CLI: the drivers read a fixed path, print to standard output, and
  write no files or exit status. Those driver capabilities are a later phase.
