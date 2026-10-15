// Command kitten is the command-line entry point for the Kitten compiler.
//
// Phase 1 implements only argument handling and usage reporting. The build and
// run commands are recognized but not implemented: neither one compiles or
// executes anything. When they are implemented they will eventually produce C
// source, compile it with a system C compiler, and link it against the Boehm
// garbage collector.
//
// Exit codes:
//
//	0  success (for example, help was requested)
//	1  the requested command is recognized but not implemented
//	2  usage error (unknown command, missing or invalid argument)
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const usageText = `Kitten compiler

Usage:
  kitten build <file.ktn>   Compile a Kitten source file
  kitten run <file.ktn>     Compile and run a Kitten source file

Neither command is implemented yet. This is a Phase 1 scaffold.`

// sourceExtension is the required extension for Kitten source files.
const sourceExtension = ".ktn"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses the command line and returns the process exit code. It writes
// usage and diagnostics to the provided streams so that the behavior is easy to
// exercise without spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usageText)
		return 2
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, usageText)
		return 0
	case "build", "run":
		return runCommand(args[0], args[1:], stderr)
	default:
		fmt.Fprintf(stderr, "kitten: unknown command %q\n\n", args[0])
		fmt.Fprintln(stderr, usageText)
		return 2
	}
}

// runCommand validates the argument for a recognized command. Because neither
// command is implemented, a valid invocation reports that clearly rather than
// pretending to compile or run anything.
func runCommand(command string, args []string, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "kitten %s: missing source file\n\n", command)
		fmt.Fprintln(stderr, usageText)
		return 2
	}
	if len(args) > 1 {
		fmt.Fprintf(stderr, "kitten %s: expected one source file, got %d arguments\n\n", command, len(args))
		fmt.Fprintln(stderr, usageText)
		return 2
	}

	source := args[0]
	if !strings.HasSuffix(source, sourceExtension) {
		fmt.Fprintf(stderr, "kitten %s: %q is not a Kitten source file (expected a %s file)\n", command, source, sourceExtension)
		return 2
	}

	fmt.Fprintf(stderr, "kitten %s: not implemented yet (cannot process %q)\n", command, source)
	return 1
}
