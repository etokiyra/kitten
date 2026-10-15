// Command kitten compiles Kitten source files into native executables.
//
// The pipeline is: source -> lexer -> parser -> checker -> C code generation ->
// C compiler/linker -> native executable, linked against the Kitten runtime and
// the Boehm garbage collector.
//
// Usage:
//
//	kitten build <file.ktn> [-o <output>]   Compile a source file
//	kitten run <file.ktn>                   Compile and run a source file
//
// Exit codes:
//
//	0  success
//	1  compilation or execution failure
//	2  usage error (unknown command, missing or invalid argument)
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"kitten/checker"
	"kitten/codegen"
	"kitten/loader"
	"kitten/parser"
	"kitten/runtime"
)

const usageText = `Kitten compiler

Usage:
  kitten build <file.ktn> [-o <output>]   Compile a Kitten source file
  kitten run <file.ktn>                   Compile and run a Kitten source file

Options:
  -o, --output <path>   Write the executable to <path> (build only)

The C compiler is taken from CC, or discovered as cc, gcc, or clang.
Kitten programs are linked against the Boehm garbage collector (libgc).`

// sourceExtension is the required extension for Kitten source files.
const sourceExtension = ".ktn"

// Exit codes.
const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses the command line and returns the process exit code. It writes
// usage and diagnostics to the provided streams so the behavior is easy to
// exercise without spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usageText)
		return exitUsage
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, usageText)
		return exitSuccess
	case "build":
		return runBuild(args[1:], stdout, stderr)
	case "run":
		return runRun(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "kitten: unknown command %q\n\n", args[0])
		fmt.Fprintln(stderr, usageText)
		return exitUsage
	}
}

// commandArgs holds the parsed arguments of the build and run commands.
type commandArgs struct {
	source string
	output string
}

// parseCommandArgs parses positional arguments and the optional -o/--output
// flag. Flags may appear before or after the source file.
func parseCommandArgs(args []string) (commandArgs, error) {
	var parsed commandArgs
	var positionals []string

	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "-o" || argument == "--output":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("%s requires a path", argument)
			}
			parsed.output = args[index+1]
			index++
		case strings.HasPrefix(argument, "-o="):
			parsed.output = strings.TrimPrefix(argument, "-o=")
		case strings.HasPrefix(argument, "--output="):
			parsed.output = strings.TrimPrefix(argument, "--output=")
		case argument == "--":
			positionals = append(positionals, args[index+1:]...)
			index = len(args)
		case len(argument) > 1 && strings.HasPrefix(argument, "-"):
			return parsed, fmt.Errorf("unknown option %q", argument)
		default:
			positionals = append(positionals, argument)
		}
	}

	switch len(positionals) {
	case 0:
		return parsed, fmt.Errorf("missing source file")
	case 1:
		parsed.source = positionals[0]
	default:
		return parsed, fmt.Errorf("expected one source file, got %d", len(positionals))
	}
	return parsed, nil
}

// runBuild compiles a source file to a native executable.
func runBuild(args []string, stdout, stderr io.Writer) int {
	parsed, err := parseCommandArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "kitten build: %v\n\n", err)
		fmt.Fprintln(stderr, usageText)
		return exitUsage
	}
	if !strings.HasSuffix(parsed.source, sourceExtension) {
		fmt.Fprintf(stderr, "kitten build: %q is not a Kitten source file (expected a %s file)\n", parsed.source, sourceExtension)
		return exitUsage
	}

	output := parsed.output
	if output == "" {
		output = defaultOutputPath(parsed.source)
	}

	if err := buildExecutable(parsed.source, output, stderr); err != nil {
		reportError(stderr, parsed.source, err)
		return exitFailure
	}

	fmt.Fprintf(stdout, "built %s\n", output)
	return exitSuccess
}

// runRun compiles a source file to a temporary executable and runs it.
func runRun(args []string, stdout, stderr io.Writer) int {
	parsed, err := parseCommandArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "kitten run: %v\n\n", err)
		fmt.Fprintln(stderr, usageText)
		return exitUsage
	}
	if parsed.output != "" {
		fmt.Fprintln(stderr, "kitten run: -o is not supported; run compiles to a temporary executable")
		return exitUsage
	}
	if !strings.HasSuffix(parsed.source, sourceExtension) {
		fmt.Fprintf(stderr, "kitten run: %q is not a Kitten source file (expected a %s file)\n", parsed.source, sourceExtension)
		return exitUsage
	}

	directory, err := os.MkdirTemp("", "kitten-run-")
	if err != nil {
		fmt.Fprintf(stderr, "kitten run: %v\n", err)
		return exitFailure
	}
	defer os.RemoveAll(directory)

	executable := filepath.Join(directory, "program")
	if err := buildExecutable(parsed.source, executable, stderr); err != nil {
		reportError(stderr, parsed.source, err)
		return exitFailure
	}

	command := exec.Command(executable)
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode()
		}
		fmt.Fprintf(stderr, "kitten run: %v\n", err)
		return exitFailure
	}
	return exitSuccess
}

// buildExecutable runs the whole pipeline and writes a native executable to
// outputPath. Intermediate C files are written to a temporary directory that is
// removed before returning; the executable is written to outputPath, which is
// outside that directory.
//
// Any output the C compiler produces is forwarded to diagnostics; on failure it
// is attached to the returned error instead so it is reported once, with
// context.
func buildExecutable(sourcePath, outputPath string, diagnostics io.Writer) error {
	modules, err := loader.Load(sourcePath)
	if err != nil {
		return err
	}
	checked, err := checker.CheckModules(modules)
	if err != nil {
		return err
	}
	cSource, err := codegen.Generate(modules, checked)
	if err != nil {
		return err
	}

	compiler, err := discoverCompiler()
	if err != nil {
		return err
	}

	buildDirectory, err := os.MkdirTemp("", "kitten-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(buildDirectory)

	generatedPath := filepath.Join(buildDirectory, "program.c")
	runtimeCPath := filepath.Join(buildDirectory, runtime.SourceFileName)
	runtimeHPath := filepath.Join(buildDirectory, runtime.HeaderFileName)
	if err := os.WriteFile(generatedPath, []byte(cSource), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(runtimeCPath, []byte(runtime.Source()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(runtimeHPath, []byte(runtime.HeaderSource()), 0o644); err != nil {
		return err
	}

	absoluteOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}

	compileFlags, linkFlags := gcBuildFlags()
	arguments := make([]string, 0, 12+len(compileFlags)+len(linkFlags))
	arguments = append(arguments, compiler[1:]...)
	arguments = append(arguments, "-std=c11", "-Wall", "-Wextra")
	arguments = append(arguments, compileFlags...)
	arguments = append(arguments, "-o", absoluteOutput, generatedPath, runtimeCPath)
	arguments = append(arguments, linkFlags...)

	// Arguments are passed as an array; no shell is involved, so paths with
	// spaces or shell metacharacters are handled safely.
	command := exec.Command(compiler[0], arguments...)
	command.Dir = buildDirectory
	var compilerOutput strings.Builder
	command.Stdout = &compilerOutput
	command.Stderr = &compilerOutput
	if err := command.Run(); err != nil {
		return &compileError{
			message: fmt.Sprintf("C compilation failed: %v", err),
			output:  compilerOutput.String(),
		}
	}

	info, err := os.Stat(absoluteOutput)
	if err != nil {
		return fmt.Errorf("C compiler reported success but %s was not produced", absoluteOutput)
	}
	if info.IsDir() {
		return fmt.Errorf("output path %s is a directory", absoluteOutput)
	}

	// Surface any compiler warnings so they are visible on success.
	if output := strings.TrimSpace(compilerOutput.String()); output != "" {
		fmt.Fprintln(diagnostics, output)
	}
	return nil
}

// compileError reports a failure while invoking the C compiler or linker. It
// carries the compiler's output so it can be shown to the user.
type compileError struct {
	message string
	output  string
}

// Error implements the error interface.
func (e *compileError) Error() string { return e.message }

// reportError writes a user-facing diagnostic. Frontend errors from the lexer,
// parser, checker, and code generator carry a source position; compiler
// failures include the compiler's own output.
func reportError(stderr io.Writer, sourcePath string, err error) {
	var compilerError *compileError
	if errors.As(err, &compilerError) {
		fmt.Fprintf(stderr, "%s: %s\n", sourcePath, compilerError.message)
		if output := strings.TrimSpace(compilerError.output); output != "" {
			fmt.Fprintln(stderr, output)
		}
		return
	}

	if position, message, ok := diagnostic(err); ok {
		file := sourcePath
		if diagnosticFile(err) != "" {
			file = diagnosticFile(err)
		}
		fmt.Fprintf(stderr, "%s:%d:%d: %s\n", file, position.Line, position.Column, message)
		return
	}

	// Errors that already name their own file, such as a loader error without a
	// position, format themselves.
	var loadError *loader.Error
	if errors.As(err, &loadError) {
		fmt.Fprintln(stderr, loadError.Error())
		return
	}

	fmt.Fprintf(stderr, "%s: %v\n", sourcePath, err)
}

// diagnosticFile returns the source file an error belongs to, or "" when the
// error does not carry one.
func diagnosticFile(err error) string {
	var loadError *loader.Error
	if errors.As(err, &loadError) {
		return loadError.Path
	}
	var checkError *checker.Error
	if errors.As(err, &checkError) {
		return checkError.File
	}
	var codegenError *codegen.Error
	if errors.As(err, &codegenError) {
		return codegenError.File
	}
	return ""
}

// diagnostic extracts a source position and message from a compiler error.
func diagnostic(err error) (parser.Position, string, bool) {
	var loadError *loader.Error
	if errors.As(err, &loadError) && loadError.Line > 0 {
		return parser.Position{Line: loadError.Line, Column: loadError.Column}, loadError.Message, true
	}
	var checkError *checker.Error
	if errors.As(err, &checkError) {
		return parser.Position{Line: checkError.Line, Column: checkError.Column}, checkError.Message, true
	}
	var codegenError *codegen.Error
	if errors.As(err, &codegenError) {
		return parser.Position{Line: codegenError.Line, Column: codegenError.Column}, codegenError.Message, true
	}
	return parser.Position{}, "", false
}

// defaultOutputPath returns the executable path used when -o is not given: the
// source file's base name without the .ktn extension, in the current directory.
func defaultOutputPath(sourcePath string) string {
	base := strings.TrimSuffix(filepath.Base(sourcePath), sourceExtension)
	if base == "" || base == "." {
		return "a.out"
	}
	return base
}
