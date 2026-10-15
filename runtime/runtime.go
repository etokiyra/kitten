package runtime

import _ "embed"

// The runtime C sources are embedded so the compiler can write them next to
// generated code without depending on where the Kitten repository lives.

// The C sources live in the csrc subdirectory so the Go build does not treat
// them as package C sources.

//go:embed csrc/kitten_runtime.h
var headerSource string

//go:embed csrc/kitten_runtime.c
var sourceSource string

// File names for the runtime sources. The generated program includes
// HeaderFileName, and both files are compiled together with the generated code.
const (
	HeaderFileName = "kitten_runtime.h"
	SourceFileName = "kitten_runtime.c"
)

// Runtime ABI names. Generated code calls these functions; keeping the names in
// one place documents the interface between the code generator and the runtime.
const (
	// InitFunction initializes the garbage collector.
	InitFunction = "ktn_rt_init"

	// AllocFunction allocates a zeroed, GC-managed object.
	AllocFunction = "ktn_rt_alloc"

	// StringEqualFunction compares two Kitten strings by value.
	StringEqualFunction = "ktn_rt_string_equal"

	// StringConcatFunction returns a new string that is its two arguments
	// concatenated.
	StringConcatFunction = "ktn_rt_string_concat"

	// StringAppendFunction appends to the string stored at a slot address.
	StringAppendFunction = "ktn_rt_string_append"

	// ArrayNewFunction allocates a zero-initialized array.
	ArrayNewFunction = "ktn_rt_array_new"

	// ArrayAtFunction returns a bounds-checked pointer to an array element.
	ArrayAtFunction = "ktn_rt_array_at"

	// ArrayLengthFunction returns the number of elements in an array.
	ArrayLengthFunction = "ktn_rt_array_length"

	// ReadFileFunction reads a whole file into a new string.
	ReadFileFunction = "ktn_rt_read_file"

	// FailFunction reports a fatal runtime error and terminates the program.
	FailFunction = "ktn_rt_fail"

	// PrintIntFunction, PrintFloatFunction, PrintBoolFunction, and
	// PrintStringFunction write one value of the corresponding primitive type.
	PrintIntFunction    = "ktn_rt_print_int"
	PrintFloatFunction  = "ktn_rt_print_float"
	PrintBoolFunction   = "ktn_rt_print_bool"
	PrintStringFunction = "ktn_rt_print_string"

	// PrintSpaceFunction writes a single space between printed arguments, and
	// PrintNewlineFunction terminates a printed line.
	PrintSpaceFunction   = "ktn_rt_print_space"
	PrintNewlineFunction = "ktn_rt_print_newline"
)

// HeaderSource returns the C declarations shared with generated code.
func HeaderSource() string { return headerSource }

// Source returns the C implementation of the runtime.
func Source() string { return sourceSource }
