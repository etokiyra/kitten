# Predeclared `ReadFileResult` and `try_read_file`

> **ADOPTED; implemented.**
>
> Phase 5.4b implemented the runtime and Go pipeline, Phase 5.4c the
> Kitten-written checker/generator, and Phase 5.5 loader read-error parity.
> Both pipelines and the loader now use `try_read_file`; the documented
> missing/unreadable-file exclusion is closed.
>
> Date: 2026-10-18. Starting commit: `498a321`.
>
> **Adopted answers to Section 11**
>
> 1. `ReadFileResult` is reserved against **any** top-level declaration with that
>    name, type or function, in every program, whether or not the syntactic
>    trigger fires. No existing corpus program uses the name.
> 2. `try_read_file` remains shadowable like `print`/`argv`; the non-static helper
>    prevents unused-function warnings.
> 3. No new size limit: `try_read_file` uses exactly the existing `read_file` limit
>    of 256 MiB (`KTN_RT_MAX_FILE_SIZE`).
> 4. Users may construct `ReadFileResult.Ok { ... }` and
>    `ReadFileResult.Failed { ... }` directly; it is a normal union.
> 5. The syntactic `try_read_file(` trigger is accepted.

## 1. Fixed decisions

The following are already adopted in `proposals/driver-capabilities.md` and are
not reopened here:

- The result type is
  `ReadFileResult { Ok { string text; } Failed { string message; } }`.
- There is no null, no exception, and no generic result type.
- `try_read_file(path: string) -> ReadFileResult` is a permanent built-in.
- Users inspect the result with the existing exhaustive `match`.
- `Failed.message` is the Go-style OS error text, **without** the loader prefix.
  The loader adds `cannot read source file: `.
- Error parity covers not-found, permission-denied, and is-a-directory, plus a
  generic fallback.
- Linux is verified; macOS is not claimed.

## 2. What the audit found

### 2.1 User unions in the Go checker

- `checker.collectDeclarations` walks each module's parsed declarations
  (`checker/checker.go:332-440`). A `*parser.UnionDecl` creates a
  `checker.Type` with:

  ```go
  &Type{
      Kind:         KindUnion,
      Name:         d.Name,
      Module:       module.id,
      VariantIndex: map[string]*Variant{},
  }
  ```

  (`checker/checker.go:379-405`). Each `Variant` records `Name`, `Index`,
  `FieldIndex`, and its source position.
- `registerType` adds the type to the module's `types` map, records it in
  `Result.TypeDecls` keyed by the AST declaration, and, for a public
  declaration, adds it to `publicTypes` while checking for duplicate public
  names across modules (`checker/checker.go:442-461`).
- Name resolution checks the current module's types first, then the shared
  `publicTypes` map (`checker/checker.go:719-730`). A public type from any loaded
  module is therefore visible to every module without an import.
- Type identity is the `*Type` pointer. A named type carries `Module` as the
  declaring module ID; `checker.Type` is in `checker/types.go:46-88`.

### 2.2 User unions in the Kitten checker

- `collect_declarations` walks `checker.modules` and calls `collect_declaration`
  (`selfhost/checker/checker.ktn:147-169`).
- A `Union(at, visibility, name, variants, variant_count)` declaration creates a
  `Type` through `named_type` with `TypeKind.Union`, the module ID, and a fresh
  type ID (`selfhost/checker/checker.ktn:216-248`). Variants are stored in
  `variants` and `variant_index` with their indices.
- `register_type` adds the type to the module info and, when public, to
  `public_types`, checking duplicate names (`selfhost/checker/checker.ktn:290-316`).
- `lookup_type` checks the current module first and then `public_types`
  (`selfhost/checker/state.ktn:624-637`). Named types compare by their numeric
  `id`, not structurally (`selfhost/checker/model.ktn:316-330`), so a prelude
  type registered once is the same type everywhere it is seen.
- The model's `ModuleModel.types` is the module's own type map, and
  `visible_types` is the public-plus-root view (`selfhost/checker/model.ktn:404-435`,
  `selfhost/checker/checker.ktn:36-101`).

### 2.3 How unions are named and emitted

Go:

- `codegen/codegen.go:283-305` builds the union declaration list by walking
  `g.modules` and collecting every `*parser.UnionDecl` in module and source
  order.
- `unionDeclarations` is the only source of:
  - forward typedefs (`emitTypeForwardDeclarations`, `codegen/codegen.go:320-340`);
  - tag constants (`emitUnionTagDeclarations`, `codegen/codegen.go:366-384`);
  - flat struct definitions (`emitUnionDefinitions`, `codegen/codegen.go:470-498`);
  - variant constructors (`emitUnionConstructors`, `codegen/codegen.go:503-547`).
- The names come from `codegen/names.go:41-100`:
  - `structName(name, module)` → `ktn_s_<modulePrefix><Name>`;
  - `unionTagName(type, variant)` → `ktn_uc_<modulePrefix><Union>_<Variant>`;
  - `variantConstructorName(type, variant)` → `ktn_new_<modulePrefix><Union>_<Variant>`;
  - `variantFieldName(variant, field)` → `ktn_v_<Variant>_<Field>`.
- `cType` maps a union to `ktn_s_<modulePrefix><Name> *`
  (`codegen/names.go:119-143`).
- `match` emission reads `expressionType(scrutinee).VariantIndex`, then emits
  `case unionTagName(...)`, binds fields through `variantFieldName(...)`, and
  validates exhaustiveness in the checker (`codegen/emit.go:715-790`).

Kitten:

- `selfhost/codegen/declarations.ktn:365-390` walks every module and every
  declaration with `for_each_union`; union sections call
  `emit_union_declaration` (`declarations.ktn:586-733`).
- It uses the same name helpers in `selfhost/codegen/names.ktn`.
- `selfhost/codegen/statements.ktn:572-665` emits `switch` cases from
  `union_tag_name`, reads fields through `variant_field_name`, and uses
  `c_type` for bindings.

### 2.4 Everything that assumes a union comes from user source

- Go `unionDeclarations()` is driven exclusively by `*parser.UnionDecl` nodes in
  `g.modules` (`codegen/codegen.go:283-305`).
- Go's declaration emitter looks up the resolved type by the AST declaration
  (`g.checked.TypeDecls[entry.declaration]`) rather than by name
  (`codegen/codegen.go:370-380`, `475-485`, `510-520`).
- Kitten `for_each_union` is driven exclusively by `Union` declarations in each
  `ModuleModel.program` (`selfhost/codegen/declarations.ktn:365-390`).
- Both `match` emitters assume `VariantIndex`/`variant_index` is already built by
  the checker and that the union type has a real module ID for mangling.
- The checker's own declaration collectors only create named types from parsed
  declarations. There is no predeclared named type table in either checker.
- Go `codegen.Generate(modules, checked)` iterates its `modules` argument
  (`codegen/codegen.go:51-69`), while the Kitten unit emitter iterates
  `SemanticModel.modules` (`selfhost/codegen/unit.ktn:52-75`). If a checker adds
  a synthetic module internally, Go codegen will not see it unless the caller
  passes the extended slice or `Generate` uses `checked.Modules`.

### 2.5 Module IDs and ordering

- The Go loader canonicalizes the root first, appends each module when first
  encountered, and assigns `ID` as the current slice length
  (`loader/loader.go:44-48`, `loader/loader.go:72-123`). The root is ID 0; imports
  follow in depth-first order. The Kitten loader mirrors this (4E.1).
- C name mangling uses that ID (`codegen/names.go:41-46`,
  `selfhost/codegen/names.ktn` `module_prefix`).
- Therefore an **appended** synthetic module with ID `n` does not renumber any
  existing module or change any existing mangled name. It can only add new names
  at ID `n`.
- The root remains module 0 for the entry-point rule
  (`codegen/codegen.go:214-241`, `selfhost/codegen/unit.ktn:88-139`).
- The checker summary includes `module_count`; appending a module changes that
  count for programs that receive it. On-use injection keeps programs that do
  not use `try_read_file` unchanged.

## 3. Options considered

### Option A — implicit prelude module (recommended)

A fixed Kitten source text declares `ReadFileResult` as a public union. When a
program needs `try_read_file`, the checker appends a parsed prelude module after
all loaded modules. The normal union, constructor, and `match` machinery then
applies unchanged. The generated C helper for `try_read_file` is emitted by the
code generator, not the runtime, and calls a runtime primitive that does not know
the union layout.

**Files touched**

- Go checker: prelude source/embedding, detection, module append, reserved name,
  builtin signature.
- Go codegen: use `checked.Modules`, prelude helper emission, builtin call
  emission.
- Kitten checker: same prelude source, detection, append/copy, reserved name,
  builtin signature.
- Kitten codegen: helper emission, builtin call emission.
- Runtime: nonfatal read primitive and error formatting.
- Harnesses/tests and docs.

**Go/Kitten divergence risk:** moderate, but the union declaration, tags,
constructors, and `match` are already differential-tested. The new duplicated
surface is the prelude source text and one small helper emission in each code
generator. Both can be compared byte for byte.

**Effect on user programs:** none unless a program uses `try_read_file` or uses
the reserved name `ReadFileResult`. On-use injection means existing generated C,
module counts, and mangled names are unchanged.

**Future built-in types:** good. A future predeclared type can be another
implicit prelude module appended in a fixed order. Existing user module IDs stay
stable; only programs using the new type gain the new prelude at the end.

### Option B — hand-written C layout and special-cased type metadata

The runtime header defines the C struct, tag constants, and field names. Both
checkers inject a `Type` with `KindUnion`/`TypeKind.Union` and hand-written
variant metadata. Both generators special-case `cType` and the builtin call.
Constructors either must be hand-written in the runtime or users must be
forbidden from constructing the type.

**Files touched:** all four compiler stages plus the runtime, with layout
duplication in the runtime header and the two checkers.

**Divergence risk:** high. The runtime, Go checker, Kitten checker, Go codegen,
and Kitten codegen all have to agree on tag numbers, field names, and C type
names. The existing declaration/constructor emitters cannot be reused. A future
built-in type repeats the same special cases.

**Effect on user programs:** can be on-use and low-blast-radius, but only by
adding more conditionals to codegen and match emission.

**Future built-in types:** poor unless a generic predeclared-type mechanism is
invented, which is a larger language change.

### Option C — synthetic AST declaration without a parsed module

The checker hand-builds a `parser.UnionDecl`/Kitten `Decl.Union` and injects it
without parsing a prelude source. The normal emitters then apply.

**Files touched:** similar to A, minus the prelude source string, plus hand-built
AST construction in both checkers.

**Divergence risk:** high. The hand-built AST must reproduce parser field order,
positions, names, and annotations exactly in both languages, and any parser AST
change silently breaks it. A parsed prelude source is evidence-backed by the
existing parser differential corpus and cannot drift structurally.

**Effect on user programs:** same as A if injected on use.

**Future built-in types:** requires repeating hand-built AST construction.

### Option A variant: always-on prelude

Appending the prelude to every program is simpler than detection, but it changes
the checker module count, semantic-model output, and generated C for every
program, and grows every translation unit. That is unnecessary for 5.4 and makes
existing expectations churn. It is rejected in favor of on-use injection.

## 4. Chosen design: on-use implicit prelude

### 4.1 Prelude source

The canonical prelude text is:

```kitten
public union ReadFileResult {
    Ok { string text; }
    Failed { string message; }
}
```

It is embedded identically in the Go and Kitten checkers. The Go side can use
`//go:embed` or a string constant; the Kitten side needs a string constant because
the self-hosted toolchain has no source-embedding facility. A differential test
must compare the generated C for a program using `try_read_file` so drift in
either copy is caught.

The synthetic module carries the path `<prelude>` and is appended after all
loaded modules. Its module ID is `n`, the number of loaded modules. Existing IDs
`0..n-1` are unchanged.

### 4.2 When it is injected

Before checking, scan the parsed modules for a call expression whose callee is
the identifier `try_read_file`. If at least one exists, parse the prelude source
and append the prelude module. This is a syntactic trigger, not name resolution:
if a user function named `try_read_file` shadows the built-in, the prelude may be
injected and emitted unused. That is deterministic, harmless, and avoids
mid-check module mutation. The helper is non-`static` so an unused helper cannot
produce an unused-function warning.

The reserved type name `ReadFileResult` is rejected in user declarations
regardless of whether the prelude was injected. This avoids a private user type
shadowing the predeclared type in one module while `try_read_file` returns the
public one. A user function named `try_read_file` remains legal and shadows the
built-in, consistent with `print` and the other built-ins.

Recommended checker error for a user declaration named `ReadFileResult`:

```text
cannot declare "ReadFileResult": the name is reserved for the predeclared type
```

The error kind is the existing duplicate-declaration category, reported at the
declaration's position.

### 4.3 Visible semantics

- `ReadFileResult` is a public union visible from every module without an import,
  because the prelude module is public and the checker's name resolution already
  consults `publicTypes`/`public_types`.
- `ReadFileResult.Ok` and `ReadFileResult.Failed` are ordinary union variants.
  Users may construct them; constructors are emitted by the normal union
  machinery. No special constructor restrictions are added.
- `try_read_file(path)` has the ordinary built-in name-resolution rule: a
  user-defined top-level function with that name shadows it.
- `match` on a result is exhaustive over `Ok` and `Failed`; a missing or duplicate
  arm produces the existing union diagnostics.
- The type is available only when the prelude is injected, but the name is
  reserved always. A program that does not call `try_read_file` therefore cannot
  declare `ReadFileResult`, but otherwise sees no change.
- If a user calls `try_read_file` in a module while another module defines a
  private `try_read_file`, the syntactic trigger still injects the prelude; the
  call resolves by the normal rules. Users should avoid that name for anything
  else.

### 4.4 Exact generated C

For a prelude module with ID `P` (where `P` is the number of loaded modules), the
prelude contributes the following fragments through the normal union emitter.
They are not necessarily contiguous in the full file: other modules' function
prototypes appear between the tag enum and the struct section, because that is
the existing section order.

```c
typedef struct ktn_s_P_ReadFileResult ktn_s_P_ReadFileResult;

enum {
    ktn_uc_P_ReadFileResult_Ok = 0,
    ktn_uc_P_ReadFileResult_Failed = 1,
};

struct ktn_s_P_ReadFileResult {
    int64_t ktn_tag;
    const ktn_string * ktn_v_Ok_text;
    const ktn_string * ktn_v_Failed_message;
};

ktn_s_P_ReadFileResult *ktn_new_P_ReadFileResult_Ok(const ktn_string * ktn_arg_text) {
    ktn_s_P_ReadFileResult *ktn_object = (ktn_s_P_ReadFileResult *)ktn_rt_alloc(sizeof(ktn_s_P_ReadFileResult));
    ktn_object->ktn_tag = ktn_uc_P_ReadFileResult_Ok;
    ktn_object->ktn_v_Ok_text = ktn_arg_text;
    return ktn_object;
}

ktn_s_P_ReadFileResult *ktn_new_P_ReadFileResult_Failed(const ktn_string * ktn_arg_message) {
    ktn_s_P_ReadFileResult *ktn_object = (ktn_s_P_ReadFileResult *)ktn_rt_alloc(sizeof(ktn_s_P_ReadFileResult));
    ktn_object->ktn_tag = ktn_uc_P_ReadFileResult_Failed;
    ktn_object->ktn_v_Failed_message = ktn_arg_message;
    return ktn_object;
}
```

The generator additionally emits one non-`static` helper after the union
constructors and before the literal pool:

```c
ktn_s_P_ReadFileResult *ktn_prelude_P_try_read_file(const ktn_string *ktn_path) {
    const ktn_string *ktn_text = NULL;
    const ktn_string *ktn_message = NULL;
    if (ktn_rt_try_read_file(ktn_path, &ktn_text, &ktn_message)) {
        return ktn_new_P_ReadFileResult_Ok(ktn_text);
    }
    return ktn_new_P_ReadFileResult_Failed(ktn_message);
}
```

`NULL` is available through the runtime header's `<stddef.h>` include. The helper
is not `static` so it remains legal if the prelude was injected for a shadowed
name and the helper is never called.

`try_read_file(p)` lowers to exactly:

```c
ktn_prelude_P_try_read_file(p)
```

The builtin call does not know the tag layout; the helper does, through the
normal generated constructor names. The runtime does not know the union layout.

### 4.5 Always versus on use

**Chosen: on use.** The prelude module is injected only when the source contains
a syntactic `try_read_file(` call. Consequences:

- Programs that do not mention `try_read_file`: no module, no generated C
  change, no checker module/type count change.
- Programs that mention it: prelude module appended at `P`, union declarations
  and helper emitted after user constructors, module and type counts grow by one.
- A user shadow named `try_read_file` may cause an unused prelude module and
  helper. The helper is non-`static`, so no unused-function warning. This is the
  only extra-output case.
- Always emitting the prelude would change every translation unit and every
  checker summary, so it is rejected for output size and blast radius.

### 4.6 Module ID and ordering impact

- No existing module is renumbered. The prelude is appended at the end with ID
  `P = n`.
- Existing mangled names are untouched: the root remains 0, imports keep
  `1..n-1`, and only the prelude's names use `P`.
- The entry point remains module 0.
- The prelude has no imports, no functions, and no literals. It contributes only
  its type declaration and constructor C.
- A future predeclared type appends another module after the current end in a
  fixed order; user IDs remain stable. Programs using multiple preludes get
  deterministic IDs in list order.

## 5. Runtime ABI

### 5.1 Signatures

```c
bool ktn_rt_try_read_file(const ktn_string *path,
                          const ktn_string **text,
                          const ktn_string **message);
```

- Returns `true` on success and `false` on failure.
- On success, `*text` is a new GC-managed, immutable Kitten string containing the
  complete file, and `*message` is set to `NULL`. An empty file yields a
  zero-length `text` (never a failure).
- On failure, `*message` is a new GC-managed string containing the Go-style OS
  error text, and `*text` is set to `NULL`.
- Both output pointers must be non-null; the generated helper always passes
  addresses. The runtime may treat null output pointers as a fatal internal error.
- The runtime allocates both strings with `ktn_rt_string_allocate`, so they obey
  the existing GC and NUL-termination rules. The union constructor stores the
  pointer; the collector keeps it alive through the union object.
- Existing `ktn_rt_read_file` remains fatal and unchanged.

### 5.2 Operation and error text

The runtime mirrors Go's `os.ReadFile` error shape:

| Condition | Operation | Exact message |
| --- | --- | --- |
| `ENOENT` | open | `open <path>: no such file or directory` |
| `EACCES` | open | `open <path>: permission denied` |
| `EISDIR` | read | `read <path>: is a directory` |
| generic open/stat failure | open | `open <path>: <strerror(errno)>` |
| generic read failure | read | `read <path>: <strerror(errno)>` |

`<path>` is the exact byte content of the Kitten `path` string, inserted without
quotes. The loader passes a canonical absolute path, so loader diagnostics match
Go's `cannot read source file: open <canonical>: ...`.

For the generic fallback, the runtime should use `strerror(errno)` and lowercase
its first ASCII letter, because Go's `syscall.Errno.Error` texts are lowercase.
The common cases above are explicit table entries; the fallback is best-effort
for uncommon errnos. The design does not promise byte parity for every possible
libc-localized message.

Implementation should attempt `open` first. A directory opens successfully on
Linux and fails on the first read, producing the `read ... is a directory`
message. A missing file or permission failure fails at open, producing the `open`
form. The runtime must close the file on every path.

### 5.3 Limits and existing string constraints

The runtime keeps the existing `KTN_RT_MAX_FILE_SIZE` guard (256 MiB) for
`try_read_file` as well. A larger file returns `Failed` with a clear message
rather than allocating arbitrarily. This is a deliberate runtime safety limit,
not a Go-loader parity guarantee; a source file larger than 256 MiB is out of
scope for the loader parity work.

Because Kitten strings are NUL-terminated and cannot contain embedded NUL bytes,
`try_read_file` has the same NUL limitation as `read_file`; content after the
first NUL is not represented. This is an existing language limitation and is
documented, not introduced by this design.

## 6. Checker, model, and diagnostics

### 6.1 Go

- Embed the prelude source and a small parser helper. `CheckModules` scans the
  parsed modules for a syntactic `try_read_file` call; if found, parses the
  prelude and appends `&parser.Module{Path: "<prelude>", ID: len(modules),
  Program: preludeProgram}` to its local module slice. Set `Result.Modules` to
  this extended slice.
- `collectDeclarations` registers the prelude's `ReadFileResult` through the
  normal `*parser.UnionDecl` path, so it gets a normal `Type`, variants, public
  visibility, and `TypeDecls` entry.
- Add `BuiltinKind.TryReadFile` and a built-in with one `string` parameter. Its
  return type must be the prelude type; because built-ins are created before
  collection, patch `c.builtins["try_read_file"].ReturnType` after
  `collectDeclarations` (or create the built-in then). Both are internal.
- Reject any user top-level declaration named `ReadFileResult` in
  `collectDeclarations`/`registerType` with the reserved-name diagnostic. This
  applies even when the prelude is not injected.
- `try_read_file` wrong arity/type goes through the existing generic builtin
  argument path; no dedicated checker rule is needed.
- `generate.Generate` must use `checked.Modules`, not its `modules` argument, so
  the appended prelude reaches the Go declaration emitter. The `modules` argument
  can remain in the signature for compatibility, but its module set must equal
  `checked.Modules`; alternatively the signature can be reduced in a later
  cleanup. The Kitten generator already uses the model's modules.

### 6.2 Kitten

- Add the same prelude source as a Kitten string constant. `check_modules` scans
  `checker.modules` for a syntactic call; if found, parses the prelude with the
  Kitten lexer/parser, builds a `Module` with ID `module_count`, and copies the
  modules into an array of capacity `module_count + 1` before constructing
  `Checker`/`models`. `CheckSummary.module_count` and `SemanticModel.module_count`
  grow by one.
- Add `BuiltinKind.TryReadFile`, a `Function` signature, and patch its
  `return_type` after collection from `checker.public_types["ReadFileResult"]`.
  The Kitten checker already records public types and visible types in
  `register_type`; no new model field is needed.
- Add the reserved-name check in `collect_declaration`/`register_type`.
- The Kitten `match`/union codegen needs no change: the prelude type is a normal
  union with `variant_index` and variant field metadata.
- The prelude module's `Program` has one `Union` declaration; its expression and
  type-reference counts come from the Kitten parser and are used for annotation
  tables.

### 6.3 Diagnostics

- Reserved declaration: `cannot declare "ReadFileResult": the name is reserved for
  the predeclared type`, existing duplicate-declaration category, at the
  declaration position.
- Wrong arity: `function "try_read_file" expects 1 argument(s), got N` (the
  existing generic built-in message).
- Wrong type: `argument 1 of "try_read_file": expected string, found <type>`
  (the existing generic built-in message).
- A missing or duplicate `match` arm uses the existing union diagnostics.
- The prelude module path `<prelude>` appears only in an internal diagnostic if
  the prelude itself is malformed, which cannot happen for the fixed source.

## 7. Codegen impact

- Go: add a builtin call case that emits
  `ktn_prelude_<P>_try_read_file(<argument>)`; add an `emitPreludeHelper`
  section after union constructors when a prelude module is present. The helper
  text is the exact block in Section 4.4. `cType` already maps the union to
  `ktn_s_<P>_ReadFileResult *`; no cType special case is needed.
- Kitten: the same builtin call case and helper emission after union
  constructors in `declarations.ktn`. The union sections already walk the model's
  modules, so the prelude's declarations are emitted through the normal path.
- Both generators must place the helper at the same point relative to the
  literal pool and function bodies. It is a fragment emitted once after the
  declaration sections, not a function body.
- Programs without a prelude emit byte-identical C to today. Programs with a
  prelude add the union sections and helper; their module/type counts grow by
  one. Existing generated-C expectations are unaffected.

## 8. Implementation split

### 5.4b — Go pipeline and runtime

1. Add the runtime primitive and error mapping, with Go-level runtime tests
   (`ktn_rt_try_read_file` success, empty file, ENOENT, EACCES, EISDIR, fallback).
2. Add the Go prelude source/scanning/module append, reserved name, builtin, and
   `checked.Modules` codegen change.
3. Add Go-only checker/codegen tests and a Go-pipeline runtime test. Do not add
   `try_read_file` to the shared differential corpus yet; the Kitten side must
   not fail on a feature it does not implement.
4. Acceptance:
   - `go test -count=1 ./checker ./codegen ./runtime`
   - `go test -count=1 ./cmd/purr -run 'TestTryReadFile|TestReadFileResult'`
   - a Go-built program exercising Ok/Failed, checked against stdout/stderr/status;
   - `gofmt -l .`, `git diff --check`, `go vet ./...`, `go build ./cmd/purr`.

### 5.4c — Kitten pipeline and differential parity

1. Add the same prelude source, scanning, module append, reserved name, builtin
   patch, and helper emission in the Kitten checker/generator.
2. Add valid/invalid checker cases and expression/call/unit differential cases.
   The expected whole units now include the prelude at ID `n`.
3. Add a runtime fixture that runs both pipelines with Ok, missing, permission,
   and directory cases, comparing stdout, stderr, exit status, and the exact
   `Failed.message`.
4. Acceptance:
   - `go test -count=1 -run 'TestKitten(Checker|Expressions|Calls|Unit|Semantics)' ./cmd/purr`
   - `go test -count=1 ./...`, `CC=gcc go test -count=1 ./...`,
     `CC=clang go test -count=1 ./...` (the full suite may be run once at the end
     of this split)
   - zero mismatches for every `try_read_file` C unit and diagnostic.

### 5.5 — Loader read-error parity

1. Replace `read_file` in `selfhost/loader/loader.ktn` with
   `try_read_file`, returning `LoadError{path,0,0,"cannot read source file: " +
   message}` on `Failed`.
2. Add missing-import, missing-root, unreadable-file, and directory cases to the
   loader differential harness; compare against Go `loader.Load` byte for byte,
   including the no-position error shape.
3. Remove the “missing/unreadable imports are fatal” exclusion from
   `selfhost/loader/README.md`.
4. Acceptance: loader harness zero mismatches; all nine fixtures and
   `tests/modules/` still pass; the seven checks pass.

## 9. Loader use in 5.5

The loader calls `try_read_file(path)` for every module. On `Ok(text)` it
lexes/parses exactly as before. On `Failed(message)` it returns:

```kitten
LoadError {
    path = path,
    line = 0,
    column = 0,
    message = "cannot read source file: " + message
}
```

The Go loader’s `os.ReadFile` failure is
`cannot read source file: open <canonical>: no such file or directory` for a
missing file. The runtime message is `open <canonical>: no such file or
directory`; adding the prefix reproduces the reference exactly. The outside-root
check still runs before the read, so it keeps its own path/message and no
position. Lex/parse failures keep their existing path/line/column.

## 10. Risks and arguments against

- **Go codegen module source.** `Generate` currently uses its `modules` argument,
  not `checked.Modules`. The design requires making `checked.Modules`
  authoritative. If that is missed, Go emits no prelude while Kitten does. This
  is the highest integration risk.
- **Prelude source duplication.** Go and Kitten each carry the prelude text. A
  differential whole-unit test is mandatory; otherwise the two copies can drift.
- **Module count changes only for users.** Programs using `try_read_file` see
  one extra internal module and one extra type in checker summaries. This is
  observable in the checker driver and must be reflected in 5.4c expectations.
- **Shadowed builtin.** A user function named `try_read_file` can cause the
  syntactic trigger to emit an unused prelude/helper. The helper is non-static to
  avoid a warning; this is extra output, not a semantic change. The alternative
  is reserving the builtin name, which would be inconsistent with existing
  built-ins.
- **Error text fallback.** The explicit errno table covers the required common
  cases; uncommon errnos use a lowercased `strerror` fallback and are not
  guaranteed byte-identical to Go.
- **Size/NUL limits.** The 256 MiB runtime limit and the no-embedded-NUL string
  representation remain exceptions to full Go file parity.
- **Predeclared construction.** Allowing users to construct `ReadFileResult`
  variants is normal union behavior but means the type is not a sealed result
  type. Forbidding construction would add special checker cases and was rejected.
- **Always-on alternative.** It would make every generated C and checker count
  change; the on-use design avoids that at the cost of a syntactic trigger.

## 11. Human-only questions

1. Approve the reserved declaration name `ReadFileResult` (rejecting both a user
   type and a user function of that name), or reserve only the type name?
2. Keep `try_read_file` shadowable like `print`/`argv`, accepting a possible
   unused prelude/helper when shadowed, or reserve it too?
3. Keep the 256 MiB `try_read_file` limit (documented parity exception), or read
   without that limit to match Go for arbitrarily large files?
4. Should users be allowed to construct `ReadFileResult` values directly, or only
   obtain them from `try_read_file`? This design recommends allowing normal union
   construction.
5. Is the on-use syntactic trigger acceptable, or should the prelude be injected
   only when the checker actually resolves the built-in (which requires a
   two-phase declaration/collection change)?
