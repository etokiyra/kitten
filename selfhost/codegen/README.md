# A C code generator for Kitten, written in Kitten

This directory holds a C code generator written in Kitten, in layers that are
being built one at a time:

- the helper layer - C names, the C type mapping, C string-literal escaping, and
  bounded output accumulation;
- the declaration emitter - the nine sections the reference generator emits
  before any function body; and
- the core expression emitter - integer, floating-point, and boolean literals,
  string literals, identifiers, unary expressions, binary expressions, calls to
  user functions and built-ins, member access and variant references, struct and
  variant literals, array and map creation, indexing, and assignment; and
- the string literal pool - the `static const ktn_string` objects the emitted code
  refers to; and
- the statement and function emitter - the bodies of the generated functions; and
- the translation unit emitter - the entry point rule, the C entry point, and the
  assembly of the whole unit.

A module loader now exists under `selfhost/loader`, and the `selfhost/loadunit.ktn`
driver uses it to feed this generator; a pipeline that writes and builds the output
does not exist yet.

The reference is the Go generator in `codegen/`, which remains the production
backend. `kitten build` and `kitten run` are unchanged and do not use anything
here.

## Files

- `selfhost/codegen/names.ktn` — name mangling, the C type mapping, and
  string-literal escaping. The Kitten counterpart of `codegen/names.go` and
  `codegen/cstring.go`.
- `selfhost/codegen/output.ktn` — bounded, chunked output accumulation and
  finalization.
- `selfhost/codegen/errors.ktn` — the failure type every generator stage reports.
- `selfhost/codegen/declarations.ktn` — the declaration emitter.
- `selfhost/codegen/expressions.ktn` — the core expression emitter.
- `selfhost/codegen/literals.ktn` — the string literal pool.
- `selfhost/codegen/statements.ktn` — the statement and function-body emitter.
- `selfhost/codegen/unit.ktn` — the entry point rule and the translation unit
  assembly.
- `selfhost/codegen.ktn` — the helper probe driver: it reads directives from
  `input.txt` and prints one result line per directive.
- `selfhost/declarations.ktn` — the declaration driver: it reads two modules,
  runs the Kitten front end, and prints their declaration section.
- `selfhost/expressions.ktn` — the expression driver: it reads two modules, runs
  the Kitten front end, and prints the rendering of every expression in their
  function bodies.
- `selfhost/functions.ktn` — the function driver: it reads two modules, runs the
  Kitten front end, and prints the pooled declarations and the function
  definitions those bodies produced.
- `selfhost/unit.ktn` — the unit driver: it reads two modules, runs the Kitten
  front end, and prints the complete C translation unit.
- `cmd/kitten/codegen_differential_test.go`,
  `cmd/kitten/declarations_differential_test.go`,
  `cmd/kitten/expressions_differential_test.go`,
  `cmd/kitten/literals_differential_test.go`,
  `cmd/kitten/functions_differential_test.go`, and
  `cmd/kitten/unit_differential_test.go` — the differential harnesses.

## Building and running

```sh
# From the repository root. The drivers read fixed relative paths, so the
# working directory selects the input.
kitten build selfhost/codegen.ktn -o /tmp/codegen-probe
kitten build selfhost/declarations.ktn -o /tmp/declarations-probe
kitten build selfhost/expressions.ktn -o /tmp/expressions-probe
kitten build selfhost/functions.ktn -o /tmp/functions-probe
kitten build selfhost/unit.ktn -o /tmp/unit-probe
cd /tmp/example && printf 'function 0 main\nescape "a\\tb"\n' > input.txt && /tmp/codegen-probe
```

Every driver writes nothing to standard error.

## The directive interface

A directive names one helper call and its arguments. Arguments are separated by
single spaces; the `escape` directive takes everything after its first space as
the literal, because a literal may contain spaces. Blank lines and lines whose
first character is `#` are ignored.

| Directive | Result line |
| --- | --- |
| `module-prefix <module>` | `module-prefix <module> <prefix>` (`-` for the root module) |
| `function <module> <name>` | `function <module> <name> <c-name>` |
| `variable <name>` | `variable <name> <c-name>` |
| `struct <module> <name>` | `struct <module> <name> <c-type>` |
| `field <name>` | `field <name> <c-member>` |
| `constructor <module> <name>` | `constructor <module> <name> <c-name>` |
| `argument <name>` | `argument <name> <c-name>` |
| `enum <module> <name>` | `enum <module> <name> <c-typedef>` |
| `enum-case <module> <enum> <case>` | `enum-case <module> <enum> <case> <c-constant>` |
| `union-tag <module> <union> <variant>` | `union-tag <module> <union> <variant> <c-constant>` |
| `variant-field <variant> <field>` | `variant-field <variant> <field> <c-member>` |
| `variant-constructor <module> <union> <variant>` | `variant-constructor <module> <union> <variant> <c-name>` |
| `match-temporary <index>` | `match-temporary <index> <c-name>` |
| `literal-name <index>` | `literal-name <index> <c-name>` |
| `object` | `object <c-name>` |
| `tag-field` | `tag-field <c-member>` |
| `escape <literal>` | `escape <c-literal> <byte-length>` |
| `ctype int\|float\|bool\|string\|void\|array\|map` | `ctype <kind> <c-type>` |
| `ctype-enum <module> <name>` | `ctype-enum <module> <name> <c-type>` |
| `ctype-struct <module> <name>` | `ctype-struct <module> <name> <c-type>` |
| `ctype-union <module> <name>` | `ctype-union <module> <name> <c-type>` |
| `ctype-none` | `ctype-none failed <message>` |
| `output <capacity> <lines>` | a summary line, then the accumulated text |
| `fragments <capacity>` | a summary line, then the accumulated text |

An unrecognized directive prints `unknown <directive>`.

## Public API

`names.ktn`:

- `function_prefix`, `variable_prefix`, `struct_prefix`, `field_prefix`,
  `constructor_prefix`, `argument_prefix`, `enum_prefix`, `enum_case_prefix`,
  `union_tag_prefix`, `variant_field_prefix`, `match_prefix`, `literal_prefix`,
  `object_name`, `tag_field_name` — the reserved prefixes and standalone names,
  each identical to the constant of the same meaning in `codegen/names.go`.
- `module_prefix(module)`, `function_name(name, module)`,
  `variable_name(name)`, `struct_name(name, module)`, `field_name(name)`,
  `constructor_name(name, module)`, `argument_name(name)`,
  `enum_name(name, module)`, `enum_case_name(enum_type, case)`,
  `union_tag_name(union_type, variant)`,
  `variant_field_name(variant, field)`,
  `variant_constructor_name(union_type, variant)`,
  `match_temporary_name(index)`, `literal_name(index)`.
- `c_type(type) -> CTypeResult`, with `c_type_ok`, `c_type_text`, and
  `c_type_message` to read the result.
- `c_string_literal(literal) -> CStringLiteral`, a struct of the escaped C
  literal and its decoded byte length. It has no failure case: the reference
  reports an error for an unsupported escape, but the lexer rejects every
  unsupported escape before the parser or the generator sees the token, so
  decoding cannot fail here.
- `header_file_name()` and `alloc_function()`, the two runtime interface names the
  declaration section mentions: the header it includes and the allocation the
  generated constructors call. `string_concat_function()`,
  `string_equal_function()`, and `string_append_function()` are the runtime helpers
  a string `+`, `==`, `!=`, or `+=` becomes. `fail_function()` is the runtime failure
  a match statement's defensive arm calls, `print_*_function()` are the helpers a
  `print` call expands into (integer, float, bool, string, space, and newline),
  `array_length_function()`, `map_length_function()`, and
  `string_length_function()` are the `length` helpers the argument's type chooses
  between, `substring_function()`, `map_has_function()`,
  `read_file_function()`, and `argv_function()` are the remaining built-ins,
  `set_args_function()` is the entry-point call that saves `argc`/`argv`, and
  `string_at_function()`, `array_new_function()`, `array_at_function()`,
  `map_new_function()`, `map_set_function()`, and `map_at_function()` are the
  indexing, creation, and assignment helpers.

`output.ktn`:

- `new_output(capacity)`, `output_write(output, text)`, `output_indent(output,
  level)`, `output_line(output, level, text)`, each returning `OutputResult`, and
  `output_text(output)`, which finalizes. `output_ok` reads an `OutputResult`.

`errors.ktn`:

- `GenerateError`, the position and message a stage reports for a failure, and
  `generate_error(line, column, message)`, which builds one. The reference reports
  the same shape, so a failure from this generator can be compared with one from
  the reference directly.

## C type mapping

| Kitten type | C representation |
| --- | --- |
| `int` | `int64_t` |
| `float` | `double` |
| `bool` | `bool` |
| `string` | `const ktn_string *` |
| `void` | `void` |
| `struct`, `union` | `ktn_s_<module><Name> *` |
| `enum` | `ktn_e_<module><Name>` |
| array | `ktn_array *` |
| map | `ktn_map *` |

The absent type, `None`, has no representation and maps to a failure.

## Name mangling

Generated C identifiers use the reserved prefixes above so that a Kitten
identifier can never collide with a C keyword, with a runtime or entry-point
name, or with another generated name. The root module is unmangled; an imported
module is qualified by its id as `<id>_`, and because a Kitten identifier cannot
begin with a digit a mangled imported name can never collide with a root-module
name.

| Kitten | C |
| --- | --- |
| function `f` in module `m` | `ktn_fn_<m>f` |
| local or parameter `x` | `ktn_var_x` |
| struct or union `S` in module `m` | `ktn_s_<m>S` |
| field `x` | `ktn_f_x` |
| struct constructor for `S` | `ktn_new_<m>S` |
| constructor parameter for field `x` | `ktn_arg_x` |
| enum `E` in module `m` | `ktn_e_<m>E` |
| enum case `C` of `E` | `ktn_ec_<m>E_C` |
| union variant `V` of `U` | `ktn_uc_<m>U_V` |
| field `x` of variant `V` | `ktn_v_V_x` |
| variant constructor for `U.V` | `ktn_new_<m>U_V` |
| match temporary *n* | `ktn_match_<n>` (zero-based) |
| pooled literal *n* | `ktn_literal_<n>` (one-based, first-use order) |
| constructor local | `ktn_object` |
| union tag member | `ktn_tag` |

## String literal escaping

A literal token's escapes are decoded with the parser's `decode_string` — the
same decoder the reference calls as `lexer.DecodeString` — and the resulting
bytes are re-encoded for C. Re-encoding rather than copying the source text is
what guarantees the generated C is well-formed: a quote, a backslash, a newline,
a tab, a carriage return, and a question mark each become their escape; printable
ASCII is emitted verbatim; every other byte becomes a three-digit octal escape,
which cannot merge with a following digit. The reported length is the number of
decoded bytes, excluding the terminator C appends, and is what the generated
string object stores.

## Declaration emitter

`selfhost/codegen/declarations.ktn` emits the sections the reference generator
writes before any function body, in its order and with its spacing:

1. the generated-file comment, the runtime include, and a blank line;
2. a forward typedef per struct and then per union;
3. a typedef and one constant per case for each enum;
4. one tag constant per variant for each union;
5. a prototype per function;
6. the C struct body of each struct;
7. one flat struct per union, holding the tag and every variant's fields;
8. one allocation-and-initialization function per struct;
9. one allocation-and-initialization function per union variant.

### API

- `emit_declarations(model) -> DeclarationsResult` emits the section, sizing the
  output exactly.
- `emit_declarations_with_capacity(model, capacity) -> DeclarationsResult` emits
  into an output of a chosen size. The derived size is exact, so this entry point
  exists so that a caller can supply a different one, including one deliberately
  too small.
- `DeclarationsResult` is `Success { text; lines; capacity }` or
  `Failure { error }`, and `GenerateError` is `{ line; column; message }`.

Emission never turns a missing annotation into a declaration: a declaration the
checker did not record is reported, with the position and wording the reference
uses for the same failure. Every name comes from `names.ktn` and every type from
the semantic model; nothing here resolves a name, infers a type, or re-reads
source text.

### Deriving the capacity

The output accumulator is bounded, so the number of lines the section will need
must be known before it is written. A formula over declaration counts would drift
from the emission as soon as a section changed, and a conservative bound would
need the same review for the same reason. Instead the emitter runs the same
traversal twice: a dry run counts the lines through the same `emit_line` calls
that write them, and the real run is given exactly that many chunks.

The count is exact rather than a bound because one emitted line is one chunk: a
line ends with a newline, and a newline completes exactly one chunk. Whether a
section is followed by a blank line is decided by comparing the line counter
before and after the section, so the dry run and the real run cannot take
different paths. A capacity that is too small is reported as a failure, never as
a short section.

### Running the declaration driver

```sh
# From the repository root.
kitten build selfhost/declarations.ktn -o /tmp/declarations-probe
mkdir -p /tmp/example && cd /tmp/example
printf 'struct S {\n    int a;\n}\nfn main() {\n}\n' > input.txt
printf '' > input2.txt
/tmp/declarations-probe
```

`input.txt` is the root module and `input2.txt` the imported one; an empty second
file is a module with no declarations, which is inert. A first line of the form
`#capacity <n>` in `input.txt` asks for an output of exactly `n` lines instead of
the derived capacity. The line is replaced by an empty line, so no other source
position moves, and a Kitten program cannot begin with `#`, so the prefix is
unambiguous. That is how the capacity tests ask for a capacity they know to be
too small.

The section is printed one line at a time, so the driver's standard output is the
section itself, byte for byte. A failed preparation, check, or emission prints a
single diagnostic line beginning with `error` instead.

## Core expression emitter

`selfhost/codegen/expressions.ktn` renders the expressions the reference generator
handles with no more than a recursive walk and parentheses:

| Kind | Rendering |
| --- | --- |
| integer literal | the source spelling, for example `42` |
| floating-point literal | the source spelling, for example `10.25` |
| boolean literal | `true` or `false` |
| string literal | `(&ktn_literal_<n>)`, the pooled object's address |
| identifier | `ktn_var_<name>` |
| unary `-` `!` `++` `--` | `(<op><operand>)` for a prefix, `(<operand><op>)` for a postfix |
| binary `+` `-` `*` `/` `%` `<` `<=` `>` `>=` `==` `!=` `&&` `\|\|` | `(<left> <op> <right>)` |
| call to a function | `<ktn_fn_name>(<argument>, ...)`, named by the module that declared it |
| call to a built-in | `print`, `length`, `has`, and `substring` are expanded; `read_file` and `argv` are one runtime call |
| member access | `(<receiver>)->ktn_f_<field>` |
| qualified variant reference | `ktn_ec_<Module><Enum>_<Case>` for an enum case, or `ktn_new_<Module><Union>_<Variant>()` for a payload-less union variant |
| struct literal | `<ktn_new_Module><Struct>(<value>, ...)`, values in field declaration order |
| variant literal | `<ktn_new_Module><Union>_<Variant>(<value>, ...)`, values in field declaration order |
| array creation | `ktn_rt_array_new(<length>, sizeof(<element C type>))` |
| map creation | `ktn_rt_map_new(sizeof(<value C type>))` |
| index | `ktn_rt_string_at(<target>, <index>)` for a string, otherwise `(*((<result C type> *)ktn_rt_array_at(<target>, <index>)))` or its `ktn_rt_map_at` counterpart |
| assignment `=` to a map entry | `ktn_rt_map_set(<map>, <key>, &(<value C type>){ <value> })`, which inserts or updates |
| assignment `=` elsewhere | `(<target> = <value>)` |
| compound `+=` `-=` on numbers, including map entries | `(<target> <op> <value>)` |
| string `+=` | `ktn_rt_string_append(&(<target>), <value>)` |

Three binary operators are special on strings, because a Kitten string is a
length-carrying pointer: `+` becomes `ktn_rt_string_concat(left, right)`, `==`
becomes `ktn_rt_string_equal(left, right)`, and `!=` becomes
`!ktn_rt_string_equal(left, right)`. The left operand's resolved type decides
that, which is the only semantic fact the emitter reads.

### Calls

A call is resolved through the checker's own annotations: the callee's symbol names
the function it resolved to, and its `builtin` field distinguishes a built-in from a
user function. Nothing is looked up by source text, and a callee the model does not
resolve is reported rather than named after what it was written as.

| Call | Rendering |
| --- | --- |
| a user function | `<mangled name>(<arguments>)`, with the name taken from the callee and mangled by the module the resolved function was declared in |
| `print(a, b)` | `(ktn_rt_print_<t>(a), ktn_rt_print_space(), ktn_rt_print_<u>(b), ktn_rt_print_newline())` |
| `length(x)` | `ktn_rt_array_length`, `ktn_rt_map_length`, or `ktn_rt_string_length`, chosen by the argument's type |
| `has(map, key)` | `ktn_rt_map_has(map, key)` |
| `substring(s, start, end)` | `ktn_rt_substring(s, start, end)` |
| `read_file(path)` | `ktn_rt_read_file(path)` |
| `argv()` | `ktn_rt_argv()` |

Arguments are rendered left to right, and the first failure among them stops the
call: a call is never emitted with a hole where an argument failed. `print` chooses
each argument's helper from the argument's recorded type *before* rendering it,
which is the order the reference uses and the reason an argument `print` cannot
write is reported at that argument. A comma expression has type `void`, which is
`print`'s own type, so it can appear wherever the checker allowed the call.

The built-ins whose argument is a container — `length` over an array or a map, and
`has` — choose their helper from the argument's type. Their arguments can now be
constructed in the same expression, because `new` is implemented.

An assignment that must create a map entry never goes through the reading
accessor, which fails when the key is absent; the plain map path renders the map,
key, and value once each and passes the value by address through a compound
literal. A string `+=` takes the address of its target, so an index target is
evaluated once. Increment and decrement are ordinary unary expressions over the
same l-values, so they need no separate path.

A string literal is pooled rather than emitted as a C string of its own: its bytes
are escaped once, an equal literal shares the object, and the reference is the
object's address. The pool is described below.

### API

- `emit_expression(context, expression) -> ExpressionResult`, where the context
  is the semantic model, the index of the module the expression belongs to, and the
  literal pool its string literals are interned in. The module's id, which the
  model carries, is what names are mangled by; the index selects the module's
  annotations. Both are needed, and neither is derivable from the other. The pool
  is part of the context rather than a return value because a literal is interned
  where it is rendered, which is what gives the pool its order.
- `ExpressionResult` is `Ok { text }` or `Failed { error }`. A failure is
  propagated from the expression that contains it, exactly as the reference
  reports a child's error from its parent, so a caller can never see a partly
  rendered expression.
- `expression_kind_name(expression)` names a kind, which the driver reports and
  the emitter's failure message uses.

An expression is a fragment, not a line, so it is returned as a string rather than
written through the output accumulator. Rendering a node concatenates its
children's text, which is the shape the reference uses; for a chain of depth *d*
that copies about *d* squared characters in total. The corpus measures the cost
(`TestKittenExpressionsScaling`) rather than hiding it: the quadratic term is a
property of the representation the reference chose, so matching the reference
byte for byte means inheriting it. A fragment-based representation could remove it
in a later phase, and would need the emitter to return something richer than a
string.

### Running the expression driver

```sh
# From the repository root.
kitten build selfhost/expressions.ktn -o /tmp/expressions-probe
mkdir -p /tmp/example && cd /tmp/example
printf 'fn main() {\n    int a = 1;\n    print(a + 2);\n}\n' > input.txt
printf '' > input2.txt
/tmp/expressions-probe
```

The driver walks the function bodies of both modules and prints one line per
expression node, in the order the reference renders bodies:

```
expr <line> <column> <kind> <rendering>
fail <line> <column> <kind> <message>
```

then the marker

```
=== literal pool ===
```

and then the pooled string literal declarations those renderings produced. The
pool comes last because it is only complete once every expression has been
rendered, even though in a generated file the declarations precede the bodies.

The walk exists only to reach expressions; it emits no C for a statement and
implements no part of the statement emitter.

A first line of `#pool <n>` in `input.txt` sizes the pool to `n` entries rather
than the derived bound, which is how a pool known to be too small is supplied.

## String literal pool

`selfhost/codegen/literals.ktn` holds the string literals a translation unit uses.
It reproduces the reference's pool:

- **Collection order.** A literal is interned where it is rendered, so the entries
  are in first-use order. The order is the reference's rendering order, which for
  every construct is also source order except a struct or variant literal: the
  reference renders those values in field declaration order, not source order, and
  so does the emitter's walk. A literal written last in such a literal can
  therefore be pooled first, which
  `TestKittenLiteralsOrderFollowsFieldDeclaration` checks directly.
- **Deduplication.** The key is the literal's C spelling, exactly as in the
  reference, so two literals that share a spelling share one object whatever
  expression they appear in.
- **Ownership.** The pool names the objects `ktn_literal_<n>`, one-based, using the
  `literal_name` helper from `names.ktn`, and expression emission refers to one as
  `(&ktn_literal_<n>)`. Nothing else may name a pooled object.
- **Placement.** `literal_pool_text` returns the declarations, each line exactly
  the reference's, with one blank line after them, and the empty string for a
  program with no literals. In a generated file they belong after the constructors
  and before the function bodies, which is where the reference writes them.

### API

- `new_literal_pool(capacity)`, `literal_count(pool)`, and
  `literal_symbol(pool, escaped, length) -> LiteralResult`, which interns a literal
  and returns its one-based index or an explicit failure. `literal_ok`,
  `literal_index`, and `literal_error` read the result.
- `literal_pool_lines(pool)` is the number of lines the declarations occupy, so a
  caller that writes them into an output of its own can size it exactly;
  `literal_pool_text(pool)` does that itself and returns the text.
- `LiteralEntry` is the escaped spelling and the decoded byte length of one entry;
  `LiteralPool` is the entries, their count, the capacity, and the lookup map.

### Capacity

The pool is bounded, and the caller sizes it. A sound bound is the number of
expression nodes in the modules being generated - `literal_capacity` in the
expression driver sums each module's `expression_count` - because every literal is
an expression node, so the bound cannot be too small. A literal that does not fit
is reported at the literal, with the capacity named, and the entries that did fit
are the reference's entries in order; the caller must abandon generation, since a
pool that cannot hold every literal would refer to objects that were never
declared.

Two failure modes cannot arise in this design and are not tested: a missing entry,
because resolving a literal interns it, and an overflow of the declarations'
output, because the output is sized from the pool and holds exactly the lines
written.

## Statement and function emitter

`selfhost/codegen/statements.ktn` renders the body of every function, and
`emit_function_bodies` returns the whole body section: one definition per function
declaration, in module and source order, each followed by a blank line, exactly as
the reference emits them.

### Supported statements

| Statement | Rendering |
| --- | --- |
| block | `{` on the current line, its statements indented, `}` on its own line |
| local declaration | `<c-type> <name>;`, or `... = <initializer>;` when it has one |
| expression statement | `<expression>;` |
| `if` / `else` / `else if` | `if (<condition>) { ... }`, with `else ` starting the next line, and `else if (...)` continuing on that line |
| `while` | `while (<condition>) { ... }` |
| `for` | `for (<init>; <condition>; <increment>) { ... }`, each clause empty when omitted |
| `return` | `return;` or `return <value>;` |
| `match` | a temporary, then a `switch` with one `case` per arm |

Every brace, indentation, blank line, and parenthesis is the reference's. The
language has no `break`, `continue`, or `goto`: no statement kind emits one, and the
`break;` inside a `match` case is emitted by the reference and therefore here.

**Conditions.** A compound expression is already emitted fully parenthesized, so a
condition that is a comparison or a logical operation keeps its own parentheses; a
condition that is a bare identifier or a literal is wrapped. This matches the
reference, which wraps only what is not already parenthesized, because Clang warns
about a redundant parenthesized equality comparison in a condition. The reference
also adds a layer around a condition that is an assignment, because C's
`-Wparentheses` warns when an assignment is used as a condition without its own
parentheses.

**Match.** The scrutinee is evaluated once into a temporary named
`ktn_match_<n>`, numbered across the whole unit in emission order, which is what
makes the names unique and deterministic. A union scrutinee is checked for null
before its tag is read, so matching a null reference is a deterministic runtime
error rather than undefined behavior. Each arm becomes `case <tag>: { ... }`, binds
the variant's fields to locals of the field types, emits `(void)<binding>;` so that
an unused binding does not warn, then the arm's statements, then `break;`. The
`default:` arm fails through `ktn_rt_fail`, which the checker's exhaustiveness rule
makes unreachable.

### API

- `emit_function_bodies(model, pool) -> BodyResult` renders every function body and
  interns the string literals they use into the pool the caller supplies. It
  returns `Success { text; lines }` or `Failure { error }`, and a failure means no
  bodies were produced.
- `body_ok` and `body_error` read a `BodyStep`, which is what one statement's
  emission returns.

The body section is not a translation unit: a unit also needs the declaration
sections of the declaration emitter, the pool's declarations, and the entry point,
which the unit emitter adds. The order in a generated file is: declaration
sections, pool, bodies, entry point.

### Errors and unsupported expressions

An expression the expression emitter does not implement is reported at the
statement that contains it, naming that expression kind, and the body is not
produced. A body is therefore never reported as rendered when part of it could not
be.

### Capacity

A body's line count is only known once it has been rendered, and rendering twice
would render every expression twice and intern every literal twice. Instead
`body_line_bound` counts the units that can produce a line - statements, match arms,
arm bindings, and functions - without rendering anything and without touching the
pool, and sizes the output for them with a generous factor; the bodies are rendered
exactly once. A bound that proved too small is reported, never silently truncated,
and the corpus assertions below would catch it, because an undersized bound would
report an overflow instead of the body the reference produces.

### Running the function driver

```sh
# From the repository root.
kitten build selfhost/functions.ktn -o /tmp/functions-probe
kitten build selfhost/unit.ktn -o /tmp/unit-probe
mkdir -p /tmp/example && cd /tmp/example
printf 'fn count(int limit) -> int {\n    int index = 0;\n    while (index < limit) {\n        index++;\n    }\n    return index;\n}\nfn main() {\n}\n' > input.txt
printf '' > input2.txt
/tmp/functions-probe
```

The driver prints the pooled declarations the renderings produced, then the
definitions, in the order a generated file has them.

## Translation unit

`selfhost/codegen/unit.ktn` renders a complete C translation unit:

```kitten
emit_translation_unit(model) -> TranslationUnitResult
```

The result is `Success { text; lines }` or `Failure { error }`, and a failure means
no unit was produced: every section either completes or reports the failure that
stopped it.

The sections, in the reference's order, with the reference's blank lines:

1. the generated-file comment, the `#include` of the runtime header, and a blank
   line, then the declaration sections of the declaration emitter;
2. the pooled string literal declarations, followed by one blank line, or nothing at
   all for a program with no literals;
3. one definition per function, each followed by a blank line, from the statement
   emitter;
4. `int main(int argc, char **argv)`, which calls `ktn_rt_init()`, saves the
   argument vector with `ktn_rt_set_args(argc, argv)`, and then calls the root module's
   `ktn_fn_main()`, and returns 0.

The parts are concatenated once each, so the assembly itself copies the unit a
constant number of times.

### The entry point rule

The rule runs **before anything is emitted**, exactly as the reference does, so a
program with both an invalid entry point and an expression the emitter cannot render
reports the entry point, and a rejected program produces no C:

| Program | Failure |
| --- | --- |
| no `main` in the root module | `program has no entry point: define fn main() in the root module`, at 1:1 |
| `main` with parameters | `entry point main must take no parameters` |
| `main` returning a value | `entry point main must not return a value, but returns <type>` |
| a `main` the checker did not annotate | `entry point main was not checked` |

The rule looks at the module at index zero, which is the root, and at its first
declaration named `main`; an imported module's `main` is never an entry point. The
failure carries the root module's path, which is the only failure that does; every
other failure belongs to the module being emitted and reports its position alone,
exactly as the reference's failures do.

### The two-pass dependency

The pool is written before the bodies but is only complete after they have been
rendered, because a literal is interned where it is rendered. The unit emitter
therefore renders the bodies first and concatenates afterwards, and renders them
exactly once: rendering them a second time to discover the pool would render every
expression twice and intern every literal twice, and would leave the pool's order
and the match temporary numbering dependent on a pass the file never shows. The
statement emitter's counted line bound is what makes one pass enough.

### Running the unit driver

```sh
# From the repository root.
kitten build selfhost/unit.ktn -o /tmp/unit-probe
mkdir -p /tmp/example && cd /tmp/example
printf 'fn main() {\n}\n' > input.txt
printf '' > input2.txt
/tmp/unit-probe
```

Standard output is the complete translation unit, byte for byte, so one comparison
covers every section. A rejected program prints a single `error ...` line instead.

## Output accumulation

Kitten strings are immutable: `+` allocates a copy of both operands. Accumulating
a translation unit with `buffer += text` therefore costs the size of the output
per append, which is quadratic overall. Kitten has no string builder, no
resizable array, and no runtime append that grows in place —
`ktn_rt_string_append` is itself a concatenation.

Text is therefore accumulated as a bounded array of chunks, exactly as the parser
accumulates AST lists. `output_write` appends to a pending line and moves each
completed line into one chunk, so the string being appended to is never longer
than a single line however large the output is. Finalization joins the chunks in
halves, which copies the text once per level of recursion.

The caller sizes the chunk array — the number of chunks is the number of lines —
and capacity is tested before any state changes, so a line that does not fit is
rejected whole rather than half recorded:

- `OutputResult.Overflow` means the write did not happen. The output holds
  exactly what earlier writes added. The caller must abandon generation, because
  a fixed capacity cannot recover.
- Rejecting the write rather than letting it accumulate in `pending` keeps the
  failure honest: an accepted write that could never be chunked would leave a
  single growing string, which is the quadratic accumulation the chunks exist to
  avoid.

Measured on a 2.5 GHz x86-64 host, accumulating *n* lines and finalizing them:

| Lines | Balanced join | Left-to-right join |
| --- | --- | --- |
| 2,000 | 0.011 s | 0.038 s |
| 8,000 | 0.016 s | 0.491 s |
| 32,000 | 0.073 s | 10.245 s |
| 128,000 | 0.308 s | 211.620 s |

Balanced joining grows linearly — four times the input costs about four times as
much — while left-to-right joining grows quadratically. The measurement is
reproduced by `TestKittenCodegenAccumulationScaling`, which also fails if
sixteen times the input costs more than forty times as much.

## Differential testing

`cmd/kitten/codegen_differential_test.go` builds the probe once and compares it
with the reference generator in `codegen/`.

The probe's results are compared with values the reference generator itself
produced, so a difference is a real disagreement rather than a copy of the
reference's logic:

- **Escaping and byte length.** For every literal in the curated set and the
  corpus, the harness generates C with the reference generator for a program
  whose only string literal it is, extracts the pooled declaration
  `static const ktn_string ktn_literal_1 = { LENGTH, "ESCAPED" };`, and requires
  the probe's escaped text and length to equal it. Both fields are read out of
  the reference's own output, and the single-literal program makes the
  association between literal and pooled entry unambiguous.
- **Names.** Each predicted name must appear in the reference C for a program
  that uses it, so a prediction cannot drift away from what the reference emits.
  The direction is one-way: the harness checks that the reference uses the name
  the probe predicted, not that the reference uses no other name.
- **C types.** Each complex spelling must appear in the reference C in a
  declaration of a local of that type, for example `ktn_array * ktn_var_values`.

The escaping corpus is every distinct string literal, valid as UTF-8, in the
checker corpus: invalid-UTF-8 input is skipped, because the reference lexer scans
runes and would replace an invalid byte before the literal is spelled out. That is
the lexer's documented byte-oriented difference, and it is covered by the lexer
harness rather than here.

Running `go test -count=1 -v -run TestKittenCodegen ./cmd/kitten/` reports:

- 29 mangled names anchored to the reference generator's C.
- 15 C type spellings anchored to the reference generator's C.
- 707 string literals whose escaping and byte length match the reference
  generator's pooled literals exactly.
- The exact accumulated text for empty, small, many-chunk, capacity-boundary, and
  overflow runs, including a partial line that never ends.
- Linear accumulation, with the growth of each step logged.

`cmd/kitten/declarations_differential_test.go` compares the declaration section
itself. The reference emits the whole translation unit, so the harness separates
the section structurally rather than by offset: the section ends at whichever
comes first of a pooled literal declaration, which is the only line beginning
`static const ktn_string ktn_literal_`, and a function definition, which is a
prototype with ` {` in place of `;`. Neither boundary depends on a string literal
being present, and neither can be confused with a constructor, which is named
`ktn_new_`.

Running `go test -count=1 -v -run TestKittenDeclarations ./cmd/kitten/` reports:

- 16 targeted programs whose sections match byte for byte, covering primitives,
  strings, arrays, maps, structs, structs referring to later structs, enums,
  unions with payloads, collection-typed payloads, public and private
  declarations, two modules, identically named private declarations in different
  modules, declaration order, bodies with and without literals, a shared literal
  pool, many declarations of every kind, and wide field and parameter lists.
- 35 corpus programs whose sections match byte for byte, over 544 lines, and
  1759 whose check error the probe reports exactly as the reference checker does.
- 57 corpus programs without an entry point, verified by deriving the reference
  section: prepending `fn main() {}` adds exactly one prototype line and nothing
  else, and removing it - together with the prototype block's blank line when the
  original declared no function - yields the original section.
- The derived capacity is exact: emitting with it reproduces the section, one line
  fewer is reported as a failure that prints no part of a section, a larger
  capacity changes nothing, and bisecting for the smallest capacity that works
  finds exactly the section's line count.
- Representative sections compile with `-std=c11 -Wall -Wextra` and produce no
  diagnostics.

`cmd/kitten/expressions_differential_test.go` compares expression renderings. The
probe prints one line per expression node in emission order, and the harness walks
the reference AST in that same order and pairs the two lists line by line, so a
wrong node, a wrong order, a wrong position, or a wrong kind cannot pass. A
rendering is compared when the reference's own output contains it in an
extractable position: a print argument is the balanced parenthesized text after a
print helper's opening parenthesis, in emission order. Every kind is implemented,
so the emitter's failure message has no remaining kind to describe.

Running `go test -count=1 -v -run TestKittenExpressions ./cmd/kitten/` reports:

- 200 of 1264 expected lines of the 26 targeted programs compared byte for byte,
  covering every literal kind, both booleans, every unary and binary operator,
  precedence, associativity, redundant parentheses, nesting, mixed operand
  types, member access and qualified variant references, struct and variant
  literals, array and map creation, indexing, assignment to every target form,
  every compound operator, string append, map entry assignment, and the nested
  combinations of them. The lines that are not compared are the subexpressions
  of a compared expression, which the compared whole already validates.
- 121 of 1230 expected lines of 34 corpus programs compared byte for byte, over
  the checker corpus, and whose **71 pooled entries** are compared as well. The
  compared-line count falls because assignment now renders rather than reporting
  an unimplemented message, and an assignment that is a statement is not a print
  argument; the function and unit harnesses compare those assignments as part of
  the whole body or unit.
- 20 floating-point spellings compared, with the boundary forms the language
  cannot express (`1.`, `.5`, `1e5`) checked at the lexer.
- Every unary and binary operator present in the targeted corpus, read from the
  lexer's tokens.
- The nested-expression measurements in the section above.

`cmd/kitten/literals_differential_test.go` compares the pool itself. The pool
appears in the reference's own output between the declaration sections and the
first function body, and the harness extracts exactly those lines, so the
comparison is a byte-for-byte comparison of the declarations rather than of a
reconstruction. Each case also compares the expression lines that produced the
pool, so a pooled symbol cannot be right while the expression referring to it is
wrong.

Running `go test -count=1 -v -run TestKittenLiterals ./cmd/kitten/` reports:

- 15 programs whose pools match byte for byte, over **36 pooled entries**: one
  literal, empty strings, multiple distinct strings, a literal repeated across
  expressions, escaped quotes and backslashes, newlines and tabs, literals inside
  nested expressions, literals in nested expression kinds,
  struct and variant literal values in field declaration order, non-ASCII text,
  two modules, and a literal shared across modules.
- A focused deduplication check: two distinct literals used four times give two
  entries, and the two symbols are the ones the expressions refer to.
- The ordering rule above, checked directly.
- The pool's capacity: an exact capacity and a roomier one reproduce the
  reference's pool, one entry fewer reports the literal that did not fit and keeps
  the entries that did, in the reference's order.
- A literal the lexer rejects is reported as a lexical error with no pool.
- A program with no literals produces no pool.
- A 400-byte literal with escapes is pooled whole, with the decoded byte length.
- Interning measurements: 100, 200, and 400 literals cost 5 ms, 8 ms, and 18 ms,
  which is linear in the number of literals.

`cmd/kitten/functions_differential_test.go` compares whole function definitions.
The probe renders the pool and the bodies; the harness extracts the same two
regions from the reference's own output - the pool between the declaration sections
and the first definition, and the definitions from there to the entry point - and
compares each byte for byte. A body that cannot be rendered fails the comparison
rather than being counted as a match.

Running `go test -count=1 -v -run TestKittenFunctions ./cmd/kitten/` reports:

- 16 targeted programs over **46 functions and 438 lines** compared byte for byte:
  empty bodies, locals with and without initializers, expression statements,
  returns with and without values, `if`/`else`/`else if` chains, nested blocks and
  shadowed names, nested `while` loops, `for` loops with every combination of
  components, conditions that keep or gain their parentheses, `match` on enums and
  unions with bindings, `match` nested in statements and functions, a `for`
  initializer that is an expression, a union with a string payload, `match` inside
  loops, string literals in nested statements that keep the pool's order, and a
  second module.
- 34 corpus programs over 693 lines and 55 functions compared byte for byte.
- Match temporaries numbered in emission order across a unit, checked directly.
- 11 assignment programs over 85 lines compared byte for byte, covering an
  assignment statement, a field, an element, a compound assignment, a loop
  condition, a return, a `for` initializer, a call argument, a match arm, a map
  entry, and a string append; 6 malformed or invalid programs are rejected with
  the front end's own error.
- 7 corruptions of a correct output - indentation, parentheses, statement order,
  function order, pool order, and a dropped definition - are each rejected by the
  comparison, and an identical output is accepted.
- The generated definitions, compiled and linked against the runtime and run under
  both GCC and Clang, print the values the source implies.

`cmd/kitten/unit_differential_test.go` compares whole translation units, so one
comparison per program covers the header, the declaration sections, the pool, the
bodies, and the entry point at once, and no expected output is hand-maintained. A
program the reference rejects is compared through its error instead, rendered in the
driver's shape including the file, which only the entry point rule sets.

Running `go test -count=1 -v -run TestKittenUnit ./cmd/kitten/` reports:

- **27 whole units identical over 1064 lines** in the targeted corpus: a minimal
  `main`; a `main` that runs a loop, a string concatenation, and an `if`; several
  functions in declaration order; nested blocks and control flow; loops of every
  shape; `match` over enums and unions; structs, enums, and unions with
  constructors; member access, indexing, and qualified variant references; struct
  and union literals; array and map creation; assignments in sub-expressions and
  conditions; compound assignment on fields and elements; map entry assignment and
  string append; a program with no pooled strings; empty strings, escaped strings,
  and repeated literals; the pool's order when strings appear in different
  functions; strings inside nested statements; and two modules, one of which
  declares its own `main`.
- **34 whole corpus units identical over 1415 lines**, plus **57 corpus programs whose
  entry point failure is reported identically**, and 1759 check errors reported
  identically.
- **4 targeted and 57 corpus entry point rejections**, covering a missing `main`,
  parameters, a return value, and an imported module that declares `main` but is not
  the root.
- 8 corruptions of a correct unit - a missing and an extra blank line, a changed
  literal number, a reordered function, a changed entry point line, a dropped
  function definition, a removed declaration section, and a dropped include - are
  each rejected by the comparison the tests use, and an identical unit is accepted.
- Generation is deterministic across repeated runs and across a different program in
  between.
- Whole units of growing size: 20, 40, and 80 functions cost 4, 6, and 11 ms.
- **Execution:** three complete units are compiled, linked against the runtime, and
  run under both GCC and Clang, matching their expected output, writing nothing to
  stderr, and exiting with status 0. One of them checks that a compound assignment
  to an element whose index is a call evaluates that call once.

`cmd/kitten/calls_differential_test.go` compares whole translation units for programs
whose expressions are calls, and, because a program can now print, compiles, links,
and runs both generated programs and compares what they write.

Running `go test -count=1 -v -run TestKittenCalls ./cmd/kitten/` reports:

- **17 whole units identical over 538 lines**: `print` with every primitive type and
  with several at once; user calls with zero, one, and several arguments; calls in
  an expression statement, a local initializer, a condition, a loop condition, a
  return, a match arm, and an argument position; nested calls; void and value
  returning calls; `length` over a string, an array parameter, and a map parameter;
  `substring`, `has`, and `read_file`; a local struct literal and an index argument;
  assignment arguments to locals, elements, and fields; cross-module calls including
  a private helper called from its own module; and the literal-pool order that call
  arguments produce.
- 4 programs the checker rejects, reported identically: `print` with no arguments, a
  variable called as a function, a call with the wrong number of arguments, and a
  call with a mistyped argument.
- 10 corruptions of a correct unit — a wrong callee name, a dropped argument,
  reordered arguments, omitted parentheses, a wrong print helper, a missing space
  helper, a wrong length helper, a reordered literal pool, and a dropped call
  statement — each rejected by the comparison the tests use, with an identical unit
  accepted.
- **10 programs compiled, linked, and run**, 9 of them against an expected output
  and the tenth against the reference program's own output, because the C standard
  leaves argument evaluation order unspecified. Every comparison also requires the
  two programs to agree on stdout, stderr, and exit status.
- Deterministic generation across repeated runs and across a different program in
  between, and nested calls of depth 25, 50, and 100 costing 3, 4, and 6 ms.

`cmd/kitten/fixtures_differential_test.go` puts the fixtures under `tests/` through
the Kitten backend end to end. Each fixture is handed to the unit driver: a fixture
the backend generates must match the reference's unit byte for byte and, compiled
against the real runtime and run under the environment's C compiler, print exactly
its `.expected` file.

Running `go test -count=1 -v -run TestKittenFixtures ./cmd/kitten/` reports:

- **All 9 fixtures** generated, compiled, run, and matching their expected output:
  `hello`, `fizzbuzz`, `math`, `room`, `strings`, `arrays`, `variants`, `maps`, and
  `lexer_proof`.
- Running the suite under GCC and under Clang covers the fixture programs under both
  compilers, because the harness uses the compiler `CC` selects.

## What is deliberately not here

- **Production integration of the loader.** The Kitten-written loader lives under
  `selfhost/loader` and discovers imports for an absolute root path, but `kitten
  build` still uses the Go loader. Missing and unreadable files remain fatal
  through `read_file` until 4E.2 decides recoverable file input.
- **Writing the unit anywhere but standard output.** The unit driver prints the C;
  the self-hosted runtime offers no file writing, no command-line arguments, and no
  program-selected exit status.

Kitten is not self-hosting, and these phases do not make it so: the backend emits
complete translation units for every expression kind the language has and the
Kitten-written loader discovers imports for an absolute root path, but a full
compiler still needs a driver that writes and builds, recoverable file input for
missing-file diagnostics, and a bootstrap in which the Kitten-written compiler
compiles its own source. Until then the Go compiler remains the bootstrap
reference and the only pipeline `kitten build` and `kitten run` use: the lexer, the
parser, the checker, and the generator's helper, declaration, expression,
literal-pool, statement, and unit layers are Kitten, and the loader core is Kitten
too, while the production loader integration, the pipeline that writes and
compiles the output, and the production compiler itself are Go.
