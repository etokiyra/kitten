# A semantic checker for Kitten, written in Kitten

This directory holds the third component of the eventual self-hosting compiler: a
semantic checker written in Kitten that consumes the AST produced by the
Kitten-written parser and validates it exactly as the Go reference checker does.

It is **not** the production front end. `purr build` and `purr run` still use
the Go checker in `checker/`, which remains authoritative. Nothing here changes
the language, and no language feature was added to make the checker easier.

## Files

| File | Contents |
| --- | --- |
| `model.ktn` | the data model: types, variant and struct definitions, optional wrappers |
| `state.ktn` | symbols, functions, scopes, the checker state, name and type resolution, diagnostics |
| `expr.ktn` | expression checking |
| `stmt.ktn` | statement checking and the conservative control-flow analysis |
| `prelude.ktn` | the admissible prelude union blocks, their syntactic triggers, and late builtin registration |
| `checker.ktn` | the five passes and the public entry points |

The driver is `../check.ktn`, one directory up, rather than `main.ktn` here. The
loader requires every import to resolve **inside the directory of the file being
compiled**, so a driver that uses the lexer, the parser, and the checker has to
sit above all three. This is a property of the current import policy, not of the
checker.

## Building and running

```sh
# From the repository root. The driver reads fixed relative paths, so the
# working directory selects the input.
printf 'struct Point { int x; }\nfn main() {\n    Point p = Point { x = 1 };\n}\n' > input.txt
printf '' > input2.txt
go build -o /tmp/purr ./cmd/purr
/tmp/purr build selfhost/check.ktn -o /tmp/purr-check
/tmp/purr-check
# ok modules 2 types 1 functions 1 bodies 1
```

The driver checks **two modules**: `input.txt` is the root module and
`input2.txt` is imported. Cross-module checking needs a module context, and the
runtime exposes no way to discover or enumerate files, so the context is fixed:
a single-module program supplies an empty `input2.txt`, which declares nothing and
cannot change the outcome (`TestEmptySecondModuleIsInert` checks exactly that on
the corpus). Output is one line:

```text
ok modules <n> types <n> functions <n> bodies <n>
error <phase> <kind> <file> <line> <column> <message>
```

`<phase>` is `Lex`, `Parse`, or `Check`, so a lexical or syntax failure is
distinguishable from a semantic one; `<kind>` is the semantic error's category
for `Check` and `-` for the other phases. The driver cannot take a path from the
command line, write to standard error, or choose an exit status, because the
runtime exposes none of those (SPEC.md 16), so it reports on standard output and
leaves the exit status to the runtime, exactly like the lexer and parser drivers.

## Public API

`checker.ktn` exposes two entry points. Both are meant to be called directly by a
future compiler driver; nothing here is reached by parsing printed output.

```kitten
public fn check_modules(Module[] modules, int module_count) -> CheckResult
public fn check_program(Module module) -> CheckResult
```

`check_program` is the single-module entry point, mirroring the reference's
`Check`; `check_modules` is the general one, mirroring `CheckModules`. A caller
with one module passes an array of one to `check_modules`, which is exactly what
`check_program` does.

`Module` is `{ string path; int id; Program program; int expression_count; int
type_ref_count; }`: `parser.Module` plus the sizes of the annotation tables the
parser's result reports. `CheckResult` is a union: `Success` carries a
`CheckSummary` of what was covered (module, named-type, function, and checked-body
counts) together with the `SemanticModel`, and `Failure` carries a `CheckError` of
`{ ErrorKind kind; string file; int line; int column; string message; }`.
Checking is fail-fast: the first error is returned and nothing else is reported,
exactly as in the reference.

Data flow: a caller loads and parses one or more modules, passes them to
`check_modules`, and receives either a summary with the model or the first error.
The checker owns its state for the duration of one call and keeps none between
calls, so checking one program never affects another — as in the reference, which
holds no package-level mutable state.

## The semantic result

A successful check produces a `SemanticModel` (`model.ktn`), which is the facts a
generator needs so that it never repeats name resolution or type inference:

| Reference `Result` entry | Kitten |
| --- | --- |
| `ExpressionTypes` | `ExprInfo.type` for the node's id |
| `VariantRefs` | `ExprInfo.variant` |
| `Symbols` | `ExprInfo.symbol`, carrying the declaration position, kind, type, and for a callee its signature and which built-in it is |
| `TypeRefs` | `ModuleModel.type_refs` for the node's id |
| `TypeDecls` | `ModuleModel.types`, keyed by name |
| `FunctionDecls` | `ModuleModel.functions`, keyed by name |
| `Types`, `Functions` | `visible_types`, `visible_functions` |
| `Builtins` | `builtins` |
| `Modules` | `ModuleModel.program` and its path and id |

Reading it:

```kitten
// A generator walks a module's program and, at each expression node, reads the
// node's parser-assigned id from the node itself.
ModuleModel module = module_model(model, module_index);
match (expression) {
    Call(node_id, open_paren, callee, arguments, argument_count) {
        ExprInfo info = expression_info(module, node_id);
        // info.type        the call's resolved type
        // info.symbol      present when the callee resolved; info.symbol.function
        //                  carries the signature and built-in, and
        //                  info.symbol.at is the declaration's position
    }
    Identifier(node_id, token) {
        ExprInfo info = expression_info(module, node_id);
        // info.type, info.symbol: which variable or parameter the name denotes
    }
    _ { }
}
```

Absence is explicit: `ExprInfo.type` is `Absent` for a node the checker never
annotated, which is exactly when the reference has no map entry — the type name in
a qualified variant reference, or the callee of a call, which the reference
resolves through `Symbols` alone. A declaration is keyed by name rather than by
node because duplicate names in one module are rejected, so a name identifies a
declaration exactly as its node does.

## Node identity

Facts are associated with nodes by an integer id the **parser** assigns while it
builds each expression and type reference, stored in the node, and reported as a
count in `ParseResult.Success`. The checker indexes its tables by that id, and a
consumer reads the id from the node it is already walking.

Alternatives and why they were not taken:

- **Source coordinates** (with or without a node kind) are not semantic identity:
  a member access and its receiver start at the same column, as do a call and its
  callee, and a shadowed name's two uses can sit at the same column on different
  lines.
- **Parallel arrays in traversal order** would couple every consumer to the
  checker's internal visit order, so a change to that order would silently
  misassociate facts.
- **Pointer identity** is not expressible: Kitten compares no reference types
  with `==`, and its maps are keyed by strings.

Ids are per module: the parser assigns them, so a node's id is meaningful with the
module it came from, which a per-module generator has. Tables are sized by the
counts the parser reports, so every id in `1..count` names a node.

## Types, symbols, and scopes

Types mirror `checker/types.go`: `int`, `float`, `bool`, `string`, `void`,
struct, array, enum, union, and map. Two representations differ, both because
Kitten has no null value and no pointer identity to compare with `==`:

- **Named types carry an integer id** and are compared by it; array and map types
  are compared through their parts. The reference interns array and map types and
  compares every type by pointer, which gives the same answers: two composite
  types are equal exactly when their parts are, and two named types are equal
  exactly when they are the same declaration.
- **Absence is a value.** An absent type is the `None` kind, an absent payload
  slot is `Absent` in an optional union, and a scope's parent is an optional
  scope. A struct value cannot be constructed recursively in Kitten, so an
  optional payload is a union rather than a placeholder object.

Symbols mirror `checker/symbols.go`: a scope is a name table plus an optional
enclosing scope, `declare` refuses a duplicate within one scope (which is what
makes shadowing work in a nested one), and `lookup` walks outward. The reference
records a resolved function on the symbol of a call callee; nothing that decides
whether a program is valid reads that field, so it is not carried here.

## Passes

The checker performs the reference's five passes, in the reference's order, each
walking the modules and each module's declarations in source order, and reports
the first error:

1. **Collect declarations.** Registers every named type and function, creating
   placeholder types and signatures, so declarations may refer to one another in
   any order. Detects duplicate names within a module, duplicate public names
   across modules, duplicate enum cases, and duplicate union variants. Rejects an
   enum with no cases and a union with no variants, as the reference does.
2. **Resolve struct fields.** Resolves each field's type, rejects a `void` field,
   and detects duplicate fields.
3. **Resolve union variant fields.** The same, per variant.
4. **Resolve function signatures.** Resolves parameter and return types, rejects a
   `void` parameter, and detects duplicate parameters.
5. **Check bodies.** Checks every function body, with parameters and the body's
   outermost statements sharing one scope, and requires a non-void function to
   return on every path the conservative analysis can prove.

The reference resolves a declaration by AST node identity, in maps keyed by the
declaration pointer. Kitten has no map keyed by object identity, so a declaration
is looked up by its module and name, which duplicate detection makes exact.

## Implicit file and directory prelude

A syntactic call whose callee is one of `try_read_file`, `try_write_file`,
`try_write_bytes`, `try_create_directory`, `try_remove`, or
`try_list_directory` causes `check_modules` to append one synthetic module after
every loaded module, at `id = module_count` and path `<prelude>`. The module
contains only the result unions required by the calls, in the fixed order

1. `ReadFileResult` for `try_read_file`
2. `FileResult` for `try_write_file`, `try_write_bytes`,
   `try_create_directory`, or `try_remove`
3. `ListDirectoryResult` for `try_list_directory`

with each fixed block concatenated directly. A `try_read_file`-only program gets
exactly the old source and C. A program with no matching call gets no extra
module and no extra type.

The prelude is parsed with the ordinary lexer and parser and checked by the
ordinary passes, so each union is a real public union with the same registration,
field, and variant rules as a user declaration. The loaded module ids and every
mangled name do not change.

The trigger is deliberately syntactic: a user function with one of these names
still injects its result union, and the generated helper is non-static and may be
unused. The built-ins remain shadowable. The prelude type names `ReadFileResult`,
`FileResult`, and `ListDirectoryResult` are reserved against every user top-level
declaration, whether or not the program has a trigger, with the reference's exact
diagnostic. A builtin itself is registered only after its result type has been
collected, so its return type is the prelude's resolved type. The loader does
**not** inject the prelude; this is the checker's responsibility.

## Coverage

Every rule the reference enforces is implemented, with the same diagnostics at
the same positions: declaration registration and duplicates, name resolution
including private-access detection, type resolution (`void` in every position
that forbids it, non-string map keys, `void` map values), function signatures and
calls, struct literals and field access, enum and union variant references and
construction, `match` exhaustiveness, duplicate and unknown bindings, local
variables and shadowing, assignment and compound assignment, every unary and
binary operator with its operand rules, array creation, indexing and length, maps
and `has`, strings and `substring`, `print`'s variadic argument rule, conditions,
`return`, the conservative return-path analysis, defined two's-complement
integer arithmetic and decimal-literal range checking, and the implicit
file/directory prelude with its three reserved names, the bitwise/shift
operators, hexadecimal/binary literal rules, the `byte` type and `byte[]`, the
four byte/string conversion built-ins, and the five recoverable file/directory
built-ins.

The reference's nil-guards (`"a module contains a nil struct declaration"`,
`"identifier is nil"`, `"unsupported expression %T"`, `"function %q has no
body"`, and similar) are **not** implemented, because a parser-produced tree
cannot contain a nil node or a missing block: those branches are unreachable for
the reference too. Absence that *is* reachable — a missing initializer, a bare
`return`, an empty `for` clause, a function without a return type — is modelled
explicitly and handled.

## What is produced

The reference builds a `Result` for a later code generator: resolved types per
expression and per type reference, variant references, resolved symbols, and
visible type and function tables. The Kitten checker produces the equivalent
`SemanticModel` and returns it from both entry points; `selfhost/codegen` reads
it rather than repeating name resolution or type inference. Nothing that decides
whether a program is valid reads those annotations.

The language has no built-in integer-to-string conversion, so a checker that must
embed a position or a count in a message performs one (`decimal` in `state.ktn`).
That is a helper inside a Kitten program, not a language feature; the built-in
conversion remains deferred.

## Differential testing

`cmd/purr/checker_differential_test.go` builds the checker once and runs it over
a corpus of **1947 inputs**, comparing its rendering with the reference
implementation's rendering of the same input:

```sh
go test -count=1 -v -run TestKittenChecker ./cmd/purr/
```

The corpus covers valid programs for every construct; one invalid program per
rule with the reference's wording; a systematic matrix of every binary operator
applied to every type (13 × 100 cases); a matrix of every assignment operator to
every target type (3 × 100); a matrix of every indexable type indexed by every
index type; name resolution and forward references; scoping, shadowing, and
duplicate declarations; enum, union, variant, and `match` rules; arrays, maps, and
built-ins; lexical and syntax failures, which must be distinguished from semantic
ones; deeply nested and generated larger inputs; seventeen cross-module cases
covering public and private functions and types, duplicate public declarations,
imported types in fields and maps, and a malformed imported module; and every
fixture and every self-hosted source, including the checker's own and the code
generator's. A self-hosted source that declares `import` statements is checked as
a one-module program, so the corpus exercises the reference's undefined-name
diagnostics on them rather than checking them cleanly.

**All 1947 inputs render byte-identically**, so this is exact parity on
success/failure, error category, position, message, and the declaration counts
reported on success. `TestKittenCheckerOutputFormat` additionally pins the
rendering with hand-written expectations, and `TestEmptySecondModuleIsInert`
confirms that checking a single-module program as two modules cannot change the
outcome.

The only expected differences are inherited from the shared Kitten lexer and
parser rather than from the checker: a column after a byte at or above 128 counts
bytes rather than code points, and a message that quotes such a byte names the
byte. The harness requires exact equality for ASCII input and compares the phase,
category, and position otherwise.

### The semantic model

`TestKittenSemanticsMatchesReference` builds `selfhost/semantics.ktn` and compares
its rendering of the model with the reference's rendering of the same Result, over
the same corpus. The rendering is one line per declaration and per annotated node
in a fixed walk order — the node's kind and position, its resolved type, its
resolved symbol with the declaration position it denotes, and its variant — with
types rendered structurally and named types carrying their module. It covers the
annotations themselves rather than success counts: **1947 inputs, all identical,
5371 annotated lines of which 5068 carry a fact** (the remainder are the callee
nodes and type names that the reference leaves unannotated).

`TestKittenSemanticsOutputFormat` pins the rendering, and
`TestAnnotationIdentitiesDistinguishSameNames` checks the identity decisions
directly: a shadowed local's two uses resolve to declarations at different
positions, two enums with a same-named case are distinguished by the owning type,
and two same-named private structs in different modules stay distinct.
`TestEmptyModelOnFailure` confirms a failed check reports only an error, so no
partial model can be mistaken for a successful one, and
`TestModelIsIsolatedBetweenInvocations` confirms one check cannot contaminate the
next.

## Contract for a future generator

A Kitten-written C generator may rely on exactly this much:

1. It receives modules that were parsed by the Kitten parser (so every
   expression and type reference has a node id and the module carries the counts)
   and a `CheckResult.Success` from `check_modules`.
2. It never resolves a name, infers a type, or re-reads source text: it reads
   `ExprInfo.type`, `ExprInfo.symbol`, `ExprInfo.variant`,
   `ModuleModel.type_refs`, `ModuleModel.types`, and `ModuleModel.functions`.
3. It may assume every node it reaches has an id in `1..count` for its module,
   that a declaration it finds in `ModuleModel.types` or `ModuleModel.functions`
   is the one its name denotes, and that a named type carries the module that
   declared it, which is how it distinguishes two same-named types.
4. It must handle `Absent` exactly where the reference has no entry.

The generator's first layer — C naming, the C type mapping, string-literal
escaping, and bounded output accumulation — lives in `selfhost/codegen`; see
`selfhost/codegen/README.md` for what it provides and what is still missing.

## What is deliberately not here

- Code generation. The semantic result exists and is tested; the generator's
  first layer — naming, the C type mapping, escaping, and output accumulation —
  is in `selfhost/codegen`, and the emitter that consumes this result does not
  exist yet.
- A module loader: the driver checks the two modules it is given, and a caller
  supplies the module set as the reference's loader does.
- Command-line arguments, standard error, and a chosen exit status.
- Recovery from an error: the first error is reported and checking stops.
