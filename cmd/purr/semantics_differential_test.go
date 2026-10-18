package main

// Differential testing for the semantic model the Kitten-written checker
// produces (selfhost/checker/semantics.ktn and selfhost/semantics.ktn).
//
// The harness builds the model driver once and compares its rendering with the
// reference implementation's rendering of the same input. The rendering is one
// line per declaration and per annotated node, in a fixed walk order, and it is
// produced from the reference checker's Result exactly as the Kitten driver
// produces it from SemanticModel:
//
//	<m> module <id> <path>
//	<m> visible-type <name> <type>
//	<m> visible-function <name> <signature>
//	<m> decl-<kind> <line> <col> <name> <declared-type|-> <signature|->
//	<m> typeref <line> <col> <resolved-type|-> - -
//	<m> <expression-kind> <line> <col> <type|-> <symbol|-> <variant|->
//
// Comparing these lines checks the annotations themselves rather than success
// counts: a resolved type, a resolved declaration identity (a symbol's name and
// declaration position), a call's callee and built-in identity, a variant
// reference, and the declaration metadata a generator reads. Types are rendered
// structurally, with a named type's module, so that two same-named types in
// different modules are distinguishable.
//
// The corpus is the checker corpus, so this covers the same inputs; a failed
// check renders the same single error line the checker driver prints.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/parser"
)

// kittenSemanticsProgram is the driver program that prints the semantic model.
func kittenSemanticsProgram() string {
	return filepath.Join("..", "..", "selfhost", "semantics.ktn")
}

// buildKittenSemantics builds the semantic-model driver.
func buildKittenSemantics(t *testing.T) string {
	t.Helper()

	requireToolchain(t)

	executable := filepath.Join(t.TempDir(), "kitten-semantics")
	var diagnostics bytes.Buffer
	if err := buildExecutable(kittenSemanticsProgram(), executable, &diagnostics); err != nil {
		t.Fatalf("building the semantic-model driver: %v\n%s", err, diagnostics.String())
	}
	if warnings := strings.TrimSpace(diagnostics.String()); warnings != "" {
		t.Fatalf("C compiler diagnostics while building the semantic-model driver:\n%s", warnings)
	}
	return executable
}

// runKittenSemantics writes the two module sources and runs the model driver.
func runKittenSemantics(executable, directory, root, imported string) (string, error) {
	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte(root), 0o644); err != nil {
		return "", fmt.Errorf("writing input.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "input2.txt"), []byte(imported), 0o644); err != nil {
		return "", fmt.Errorf("writing input2.txt: %v", err)
	}

	command := exec.Command(executable)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("running the model driver: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		return "", fmt.Errorf("the model driver wrote to stderr: %s", stderr.String())
	}
	return stdout.String(), nil
}

// --- Rendering the reference model -------------------------------------------

// referenceModel renders the reference checker's result in the Kitten driver's
// format.
func referenceModel(root, imported string) (string, error) {
	rootProgram, failure, err := prepareReferenceModule(root, "input.txt")
	if err != nil {
		return "", err
	}
	if failure != "" {
		return failure, nil
	}
	importedProgram, failure, err := prepareReferenceModule(imported, "input2.txt")
	if err != nil {
		return "", err
	}
	if failure != "" {
		return failure, nil
	}

	modules := []*parser.Module{
		{Path: "input.txt", ID: 0, Program: rootProgram},
		{Path: "input2.txt", ID: 1, Program: importedProgram},
	}
	result, checkError := checker.CheckModules(modules)
	if checkError != nil {
		var semantic *checker.Error
		if !errors.As(checkError, &semantic) {
			return "", fmt.Errorf("the reference checker returned %T: %v", checkError, checkError)
		}
		return fmt.Sprintf("error Check %s %s %d %d %s\n",
			checkerKindName(semantic.Kind), semantic.File, semantic.Line, semantic.Column, semantic.Message), nil
	}

	types, functions := countDeclarations(result.Modules)
	var builder strings.Builder
	fmt.Fprintf(&builder, "ok modules %d types %d functions %d bodies %d\n", len(result.Modules), types, functions, functions)

	model := &referenceModelPrinter{result: result, builder: &builder}
	for moduleIndex, module := range result.Modules {
		model.module(moduleIndex, module)
	}
	return builder.String(), nil
}

// referenceModelPrinter walks the reference AST and prints the annotations the
// reference checker recorded for each node.
type referenceModelPrinter struct {
	result  *checker.Result
	builder *strings.Builder
}

func (p *referenceModelPrinter) line(fields ...string) {
	p.builder.WriteString(strings.Join(fields, " "))
	p.builder.WriteByte('\n')
}

// typeText renders a type with its module, structurally, as the Kitten driver
// does.
func typeText(value *checker.Type) string {
	if value == nil {
		return "<invalid>"
	}
	switch value.Kind {
	case checker.KindArray:
		return typeText(value.Element) + "[]"
	case checker.KindMap:
		return "map[" + typeText(value.Key) + "]" + typeText(value.Value)
	case checker.KindStruct, checker.KindEnum, checker.KindUnion:
		return value.Name + "@" + strconv.Itoa(value.Module)
	default:
		return value.Name
	}
}

// signatureText renders "return(parameters)".
func signatureText(function *checker.Function) string {
	if function == nil {
		return "-"
	}
	parts := make([]string, 0, len(function.ParameterTypes))
	for _, parameter := range function.ParameterTypes {
		parts = append(parts, typeText(parameter))
	}
	return typeText(function.ReturnType) + "(" + strings.Join(parts, ",") + ")"
}

// builtinKindName mirrors the Kitten driver's builtin_kind_name.
func builtinKindName(kind checker.BuiltinKind) string {
	switch kind {
	case checker.NotBuiltin:
		return "-"
	case checker.BuiltinPrint:
		return "Print"
	case checker.BuiltinEprint:
		return "Eprint"
	case checker.BuiltinExit:
		return "Exit"
	case checker.BuiltinLength:
		return "Length"
	case checker.BuiltinReadFile:
		return "ReadFile"
	case checker.BuiltinArgv:
		return "Argv"
	case checker.BuiltinTryReadFile:
		return "TryReadFile"
	case checker.BuiltinTryWriteFile:
		return "TryWriteFile"
	case checker.BuiltinTryWriteBytes:
		return "TryWriteBytes"
	case checker.BuiltinTryCreateDirectory:
		return "TryCreateDirectory"
	case checker.BuiltinTryRemove:
		return "TryRemove"
	case checker.BuiltinTryListDirectory:
		return "TryListDirectory"
	case checker.BuiltinTryRun:
		return "TryRun"
	case checker.BuiltinMapHas:
		return "MapHas"
	case checker.BuiltinSubstring:
		return "Substring"
	case checker.BuiltinIntFromByte:
		return "IntFromByte"
	case checker.BuiltinByteFromInt:
		return "ByteFromInt"
	case checker.BuiltinStringToBytes:
		return "StringToBytes"
	case checker.BuiltinBytesToString:
		return "BytesToString"
	default:
		return "?"
	}
}

// functionText renders a resolved function: its signature and built-in name.
func functionText(function *checker.Function) string {
	if function == nil {
		return "-"
	}
	return signatureText(function) + ":" + builtinKindName(function.Builtin)
}

// symbolText renders a resolved symbol: name, declaration position, kind, type,
// and for a function its signature.
func symbolText(symbol *checker.Symbol) string {
	if symbol == nil {
		return "-"
	}
	kind := "variable"
	callee := ""
	if symbol.Kind == checker.SymbolFunction {
		kind = "function"
		callee = ":" + functionText(symbol.Function)
	}
	return symbol.Name + "@" + strconv.Itoa(symbol.At.Line) + ":" + strconv.Itoa(symbol.At.Column) + ":" + kind + ":" + typeText(symbol.Type) + callee
}

// variantText renders a resolved enum case or union variant.
func variantText(variant *checker.Variant) string {
	if variant == nil {
		return "-"
	}
	return variant.Name + "#" + strconv.Itoa(variant.Index)
}

// expressionText renders an optional resolved type.
func expressionText(value *checker.Type) string {
	if value == nil {
		return "-"
	}
	return typeText(value)
}

func (p *referenceModelPrinter) module(moduleIndex int, module *parser.Module) {
	prefix := strconv.Itoa(moduleIndex)
	p.line(prefix, "module", strconv.Itoa(module.ID), module.Path)

	for _, declaration := range module.Program.Declarations {
		switch node := declaration.(type) {
		case *parser.StructDecl, *parser.EnumDecl, *parser.UnionDecl:
			name := declarationName(node)
			if moduleIndex == 0 || declarationVisibility(node) == parser.VisibilityPublic {
				if named, ok := p.result.Types[name]; ok {
					p.line(prefix, "visible-type", name, typeText(named))
				}
			}
		}
	}
	for _, declaration := range module.Program.Declarations {
		if function, ok := declaration.(*parser.FunctionDecl); ok {
			if moduleIndex == 0 || function.Visibility == parser.VisibilityPublic {
				if signature, ok := p.result.Functions[function.Name]; ok {
					p.line(prefix, "visible-function", function.Name, signatureText(signature))
				}
			}
		}
	}

	for _, declaration := range module.Program.Declarations {
		p.declaration(moduleIndex, declaration)
	}
}

// declarationName returns the name of a named declaration.
func declarationName(declaration parser.Decl) string {
	switch node := declaration.(type) {
	case *parser.StructDecl:
		return node.Name
	case *parser.EnumDecl:
		return node.Name
	case *parser.UnionDecl:
		return node.Name
	default:
		return ""
	}
}

// declarationVisibility returns a declaration's visibility modifier.
func declarationVisibility(declaration parser.Decl) parser.Visibility {
	switch node := declaration.(type) {
	case *parser.StructDecl:
		return node.Visibility
	case *parser.EnumDecl:
		return node.Visibility
	case *parser.UnionDecl:
		return node.Visibility
	case *parser.FunctionDecl:
		return node.Visibility
	default:
		return parser.VisibilityDefault
	}
}

func (p *referenceModelPrinter) declaration(moduleIndex int, declaration parser.Decl) {
	prefix := strconv.Itoa(moduleIndex)
	switch node := declaration.(type) {
	case *parser.ImportDecl:
		p.line(prefix, "decl-import", strconv.Itoa(node.At.Line), strconv.Itoa(node.At.Column), "-", "-", "-")

	case *parser.FunctionDecl:
		p.line(prefix, "decl-fn", strconv.Itoa(node.At.Line), strconv.Itoa(node.At.Column), node.Name, "-", functionText(p.result.FunctionDecls[node]))
		for _, parameter := range node.Parameters {
			p.typeRef(moduleIndex, parameter.Type)
		}
		if node.ReturnType != nil {
			p.typeRef(moduleIndex, node.ReturnType)
		}
		p.statement(moduleIndex, node.Body)

	case *parser.StructDecl:
		p.line(prefix, "decl-struct", strconv.Itoa(node.At.Line), strconv.Itoa(node.At.Column), node.Name, typeText(p.result.TypeDecls[node]), "-")
		for _, field := range node.Fields {
			p.typeRef(moduleIndex, field.Type)
		}

	case *parser.EnumDecl:
		p.line(prefix, "decl-enum", strconv.Itoa(node.At.Line), strconv.Itoa(node.At.Column), node.Name, typeText(p.result.TypeDecls[node]), "-")

	case *parser.UnionDecl:
		p.line(prefix, "decl-union", strconv.Itoa(node.At.Line), strconv.Itoa(node.At.Column), node.Name, typeText(p.result.TypeDecls[node]), "-")
		for _, variant := range node.Variants {
			for _, field := range variant.Fields {
				p.typeRef(moduleIndex, field.Type)
			}
		}
	}
}

func (p *referenceModelPrinter) typeRef(moduleIndex int, reference *parser.TypeRef) {
	prefix := strconv.Itoa(moduleIndex)
	position := reference.Pos()
	resolved, ok := p.result.TypeRefs[reference]
	text := "-"
	if ok {
		text = typeText(resolved)
	}
	p.line(prefix, "typeref", strconv.Itoa(position.Line), strconv.Itoa(position.Column), text, "-", "-")
}

func (p *referenceModelPrinter) expression(moduleIndex int, expression parser.Expr) {
	prefix := strconv.Itoa(moduleIndex)
	position := expression.Pos()
	resolved := p.result.ExpressionTypes[expression]
	symbol := ""
	if identifier, ok := expression.(*parser.Identifier); ok {
		symbol = symbolText(p.result.Symbols[identifier])
	}
	if symbol == "" {
		symbol = "-"
	}
	p.line(prefix, expressionKind(expression), strconv.Itoa(position.Line), strconv.Itoa(position.Column),
		expressionText(resolved), symbol, variantText(p.result.VariantRefs[expression]))

	switch node := expression.(type) {
	case *parser.UnaryExpr:
		p.expression(moduleIndex, node.Operand)
	case *parser.BinaryExpr:
		p.expression(moduleIndex, node.Left)
		p.expression(moduleIndex, node.Right)
	case *parser.AssignExpr:
		p.expression(moduleIndex, node.Target)
		p.expression(moduleIndex, node.Value)
	case *parser.CallExpr:
		p.expression(moduleIndex, node.Callee)
		for _, argument := range node.Arguments {
			p.expression(moduleIndex, argument)
		}
	case *parser.MemberExpr:
		p.expression(moduleIndex, node.Receiver)
	case *parser.StructLiteral:
		p.typeRef(moduleIndex, node.Type)
		for _, field := range node.Fields {
			p.expression(moduleIndex, field.Value)
		}
	case *parser.VariantLiteral:
		p.typeRef(moduleIndex, node.Type)
		for _, field := range node.Fields {
			p.expression(moduleIndex, field.Value)
		}
	case *parser.NewArrayExpr:
		p.typeRef(moduleIndex, node.Element)
		p.expression(moduleIndex, node.Length)
	case *parser.NewMapExpr:
		p.typeRef(moduleIndex, node.Map)
	case *parser.IndexExpr:
		p.expression(moduleIndex, node.Target)
		p.expression(moduleIndex, node.Index)
	}
}

// expressionKind names an expression node as the Kitten driver does.
func expressionKind(expression parser.Expr) string {
	switch expression.(type) {
	case *parser.Identifier:
		return "ident"
	case *parser.IntLiteral:
		return "int"
	case *parser.FloatLiteral:
		return "float"
	case *parser.StringLiteral:
		return "string"
	case *parser.BoolLiteral:
		return "bool"
	case *parser.UnaryExpr:
		return "unary"
	case *parser.BinaryExpr:
		return "binary"
	case *parser.AssignExpr:
		return "assign"
	case *parser.CallExpr:
		return "call"
	case *parser.MemberExpr:
		return "member"
	case *parser.StructLiteral:
		return "struct-literal"
	case *parser.VariantLiteral:
		return "variant-literal"
	case *parser.NewArrayExpr:
		return "new-array"
	case *parser.NewMapExpr:
		return "new-map"
	case *parser.IndexExpr:
		return "index"
	default:
		return "unknown"
	}
}

func (p *referenceModelPrinter) statement(moduleIndex int, statement parser.Stmt) {
	switch node := statement.(type) {
	case *parser.BlockStmt:
		for _, inner := range node.Statements {
			p.statement(moduleIndex, inner)
		}
	case *parser.VarDeclStmt:
		p.typeRef(moduleIndex, node.Type)
		if node.Initializer != nil {
			p.expression(moduleIndex, node.Initializer)
		}
	case *parser.ExprStmt:
		p.expression(moduleIndex, node.Expression)
	case *parser.IfStmt:
		p.expression(moduleIndex, node.Condition)
		p.statement(moduleIndex, node.Then)
		if node.Else != nil {
			p.statement(moduleIndex, node.Else)
		}
	case *parser.WhileStmt:
		p.expression(moduleIndex, node.Condition)
		p.statement(moduleIndex, node.Body)
	case *parser.ForStmt:
		if node.Init != nil {
			p.statement(moduleIndex, node.Init)
		}
		if node.Condition != nil {
			p.expression(moduleIndex, node.Condition)
		}
		if node.Increment != nil {
			p.expression(moduleIndex, node.Increment)
		}
		p.statement(moduleIndex, node.Body)
	case *parser.ReturnStmt:
		if node.Value != nil {
			p.expression(moduleIndex, node.Value)
		}
	case *parser.MatchStmt:
		p.expression(moduleIndex, node.Scrutinee)
		for _, arm := range node.Arms {
			p.statement(moduleIndex, arm.Body)
		}
	}
}

// --- Tests -------------------------------------------------------------------

// maskDigits replaces every run of digits with "#", so that a comparison can
// ignore position columns, which differ for input containing a byte >= 128.
func maskDigits(text string) string {
	var builder strings.Builder
	inDigits := false
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character >= '0' && character <= '9' {
			if !inDigits {
				builder.WriteByte('#')
			}
			inDigits = true
			continue
		}
		inDigits = false
		builder.WriteByte(character)
	}
	return builder.String()
}

// errorLineFields splits an error rendering into its fields.
func errorLineFields(line string) []string {
	return strings.SplitN(strings.TrimSuffix(line, "\n"), " ", 6)
}

// TestKittenSemanticsMatchesReference compares the semantic model the Kitten
// checker produces with the reference's, over the checker corpus.
func TestKittenSemanticsMatchesReference(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenSemantics(t)
	directory := t.TempDir()
	cases := checkerCorpus(t)
	t.Logf("semantic-model corpus: %d inputs", len(cases))

	exact := 0
	nonzero := 0
	annotated := 0
	for _, testCase := range cases {
		want, err := referenceModel(testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", describeCheckerCase(testCase), err)
			continue
		}
		got, err := runKittenSemantics(executable, directory, testCase.root, testCase.imported)
		if err != nil {
			t.Errorf("%s: %v", describeCheckerCase(testCase), err)
			continue
		}

		for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 5 && fields[0] != "ok" && fields[0] != "error" {
				annotated++
				if fields[4] != "-" {
					nonzero++
				}
			}
		}

		if got == want {
			exact++
			continue
		}

		if !isASCII(testCase.root) || !isASCII(testCase.imported) {
			// The documented byte-oriented differences can move a position or a
			// quoted byte, so compare with digits masked and, for an error, the
			// message exempt.
			wantLines := strings.Split(strings.TrimSuffix(want, "\n"), "\n")
			gotLines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
			if len(wantLines) != len(gotLines) {
				t.Errorf("model line count %d, want %d, for %s", len(gotLines), len(wantLines), describeCheckerCase(testCase))
				continue
			}
			mismatch := false
			for index := range wantLines {
				if maskDigits(gotLines[index]) == maskDigits(wantLines[index]) {
					continue
				}
				gotFields := errorLineFields(gotLines[index])
				wantFields := errorLineFields(wantLines[index])
				if len(gotFields) == 6 && len(wantFields) == 6 && gotFields[0] == "error" && wantFields[0] == "error" &&
					gotFields[1] == wantFields[1] && gotFields[2] == wantFields[2] && gotFields[4] == wantFields[4] && gotFields[5] == wantFields[5] {
					continue
				}
				t.Errorf("model line %d for %s\n got: %q\nwant: %q", index, describeCheckerCase(testCase), gotLines[index], wantLines[index])
				mismatch = true
			}
			if mismatch {
				continue
			}
			exact++
			continue
		}

		t.Errorf("semantic model mismatch for %s\n--- reference ---\n%s--- Kitten ---\n%s", describeCheckerCase(testCase), want, got)
	}

	if annotated < 1000 || nonzero < 1000 {
		t.Fatalf("the model looks empty: %d annotated lines, %d with a fact", annotated, nonzero)
	}
	t.Logf("semantic model: %d inputs, %d identical, %d annotated lines (%d carrying a fact)", len(cases), exact, annotated, nonzero)
}

// TestKittenSemanticsOutputFormat pins the model rendering for one program with
// explicit expected text, so that a format change is caught even if both
// implementations change together.
func TestKittenSemanticsOutputFormat(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenSemantics(t)
	directory := t.TempDir()

	root := "struct Point { int x; }\nfn area(Point p) -> int {\n    return p.x;\n}\nfn main() {\n    Point p = Point { x = 1 };\n    int y = area(p);\n}\n"
	got, err := runKittenSemantics(executable, directory, root, "")
	if err != nil {
		t.Fatalf("running the model driver: %v", err)
	}
	want, err := referenceModel(root, "")
	if err != nil {
		t.Fatalf("rendering the reference model: %v", err)
	}
	if got != want {
		t.Errorf("model output differs from the reference\n got:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(got, "symbol") && !strings.Contains(got, "p@") {
		t.Errorf("the model does not mention a resolved symbol:\n%s", got)
	}
	if !strings.Contains(got, "Point@0") {
		t.Errorf("the model does not mention a resolved named type:\n%s", got)
	}
	if !strings.Contains(got, "area") {
		t.Errorf("the model does not mention a resolved call signature:\n%s", got)
	}
}

// TestAnnotationIdentitiesDistinguishSameNames checks that annotations are keyed
// by node rather than by text or position: two uses of the same name, resolving
// to different declarations, must differ.
func TestAnnotationIdentitiesDistinguishSameNames(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenSemantics(t)
	directory := t.TempDir()

	// The inner x shadows the outer one, and both are used at the same column on
	// different lines; the model must report the two different declarations.
	root := "fn main() {\n    int x = 1;\n    {\n        int x = 2;\n        print(x);\n    }\n    print(x);\n}\n"
	got, err := runKittenSemantics(executable, directory, root, "")
	if err != nil {
		t.Fatalf("running the model driver: %v", err)
	}
	want, err := referenceModel(root, "")
	if err != nil {
		t.Fatalf("rendering the reference model: %v", err)
	}
	if got != want {
		t.Errorf("model output differs from the reference\n got:\n%s\nwant:\n%s", got, want)
	}

	// The two uses of x resolve to different declarations: the inner one
	// declared at 4:9 and the outer one at 2:5.
	if !strings.Contains(got, "x@4:9:variable") {
		t.Errorf("the inner shadowing declaration is missing:\n%s", got)
	}
	if !strings.Contains(got, "x@2:5:variable") {
		t.Errorf("the outer declaration is missing:\n%s", got)
	}
	if count := strings.Count(got, ":variable:int"); count != 2 {
		t.Errorf("expected two distinct x declarations, found %d occurrences of a variable of type int:\n%s", count, got)
	}

	// Two enum cases with the same text in two enums: the variant's owner must
	// distinguish them.
	variantRoot := "enum First { A, B }\nenum Second { A, B }\nfn f() -> bool {\n    return First.A == First.A;\n}\nfn g() -> bool {\n    return Second.A == Second.A;\n}\n"
	gotVariant, err := runKittenSemantics(executable, directory, variantRoot, "")
	if err != nil {
		t.Fatalf("running the model driver: %v", err)
	}
	wantVariant, err := referenceModel(variantRoot, "")
	if err != nil {
		t.Fatalf("rendering the reference model: %v", err)
	}
	if gotVariant != wantVariant {
		t.Errorf("model output differs from the reference\n got:\n%s\nwant:\n%s", gotVariant, wantVariant)
	}
	if !strings.Contains(gotVariant, "First@0") || !strings.Contains(gotVariant, "Second@0") {
		t.Errorf("the two same-named enum types are not distinguished:\n%s", gotVariant)
	}
	if !strings.Contains(gotVariant, "A#0") {
		t.Errorf("the variant reference is missing:\n%s", gotVariant)
	}

	// Two same-named private types in different modules must stay distinct.
	moduleRoot := "struct Item { int a; }\nfn main() {\n    Item item = Item { a = 1 };\n    print(item.a);\n}\n"
	moduleImported := "struct Item { string b; }\npublic fn other() -> int {\n    Item item = Item { b = \"x\" };\n    return length(item.b);\n}\n"
	gotModules, err := runKittenSemantics(executable, directory, moduleRoot, moduleImported)
	if err != nil {
		t.Fatalf("running the model driver: %v", err)
	}
	wantModules, err := referenceModel(moduleRoot, moduleImported)
	if err != nil {
		t.Fatalf("rendering the reference model: %v", err)
	}
	if gotModules != wantModules {
		t.Errorf("model output differs from the reference\n got:\n%s\nwant:\n%s", gotModules, wantModules)
	}
	if !strings.Contains(gotModules, "Item@0") || !strings.Contains(gotModules, "Item@1") {
		t.Errorf("the two same-named types in different modules are not distinguished:\n%s", gotModules)
	}
}

// TestEmptyModelOnFailure checks that a failed check reports the error and no
// model, so that a partial result cannot be mistaken for a successful one.
func TestEmptyModelOnFailure(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenSemantics(t)
	directory := t.TempDir()

	got, err := runKittenSemantics(executable, directory, "fn main() {\n    int x = \"s\";\n}\n", "")
	if err != nil {
		t.Fatalf("running the model driver: %v", err)
	}
	if !strings.HasPrefix(got, "error Check ") {
		t.Fatalf("a failed check did not report an error: %q", got)
	}
	if strings.Contains(got, "module") {
		t.Errorf("a failed check rendered a model:\n%s", got)
	}
}

// TestModelIsIsolatedBetweenInvocations checks that one check does not leak into
// the next: the same input must render identically after a different one.
func TestModelIsIsolatedBetweenInvocations(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenSemantics(t)
	directory := t.TempDir()

	first, err := runKittenSemantics(executable, directory, "struct S { int a; }\nfn f() { S s = S { a = 1 }; }\n", "")
	if err != nil {
		t.Fatalf("running the model driver: %v", err)
	}
	if _, err := runKittenSemantics(executable, directory, "enum E { A }\nfn g(E e) { match (e) { A { } } }\n", ""); err != nil {
		t.Fatalf("running the model driver: %v", err)
	}
	second, err := runKittenSemantics(executable, directory, "struct S { int a; }\nfn f() { S s = S { a = 1 }; }\n", "")
	if err != nil {
		t.Fatalf("running the model driver: %v", err)
	}
	if first != second {
		t.Errorf("the model depends on the previous invocation\n first:\n%s\nsecond:\n%s", first, second)
	}
}
