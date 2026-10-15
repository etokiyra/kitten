// Package checker performs semantic analysis and static type checking on a
// parsed Kitten program.
//
// The public entry points are Check, which checks a single *parser.Program, and
// CheckModules, which checks a program split across several modules. Both return
// a *Result describing the resolved program together with an error. On success
// the Result records struct types, function signatures, the type of every
// expression, and the symbol each identifier resolved to, so that later phases
// can consume the checked program without redoing analysis.
//
// CheckModules resolves a name against the module that contains its use, then
// against the public declarations shared across all modules, then against the
// built-ins. Private declarations are therefore visible only within their own
// module, and a reference to another module's private declaration is reported
// as a private-access error.
//
// The checker is fail-fast: it reports the first semantic error as an *Error
// carrying a file, a one-based line:column position, and a category, and
// returns a nil Result. It never panics, including on hand-built or incomplete
// syntax trees.
//
// The checker implements the type, scope, function, return, struct, enum,
// union, built-in, visibility, and module rules recorded in SPEC.md. It does not
// generate code, run the program, or perform any optimization; those are
// separate phases.
package checker
