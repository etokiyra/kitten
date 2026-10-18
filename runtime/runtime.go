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

	// StringLengthFunction returns the number of bytes in a string.
	StringLengthFunction = "ktn_rt_string_length"

	// StringAtFunction returns the byte at an index, with a bounds check.
	StringAtFunction = "ktn_rt_string_at"

	// SubstringFunction returns a new string holding a byte range.
	SubstringFunction = "ktn_rt_substring"

	// ArrayNewFunction allocates a zero-initialized array.
	ArrayNewFunction = "ktn_rt_array_new"

	// ArrayAtFunction returns a bounds-checked pointer to an array element.
	ArrayAtFunction = "ktn_rt_array_at"

	// ArrayLengthFunction returns the number of elements in an array.
	ArrayLengthFunction = "ktn_rt_array_length"

	// SetArgsFunction stores the process argument vector before user code runs.
	SetArgsFunction = "ktn_rt_set_args"

	// ArgvFunction returns the process argument vector as an array of strings.
	ArgvFunction = "ktn_rt_argv"

	// MapNewFunction allocates an empty map with a given value size.
	MapNewFunction = "ktn_rt_map_new"

	// MapSetFunction inserts or updates a map entry.
	MapSetFunction = "ktn_rt_map_set"

	// MapAtFunction returns a pointer to a map value, failing when the key is
	// absent.
	MapAtFunction = "ktn_rt_map_at"

	// MapHasFunction reports whether a map contains a key.
	MapHasFunction = "ktn_rt_map_has"

	// MapLengthFunction returns the number of entries in a map.
	MapLengthFunction = "ktn_rt_map_length"

	// ReadFileFunction reads a whole file into a new string.
	ReadFileFunction = "ktn_rt_read_file"

	// FailFunction reports a fatal runtime error and terminates the program.
	FailFunction = "ktn_rt_fail"

	// ExitFunction terminates the process with a chosen status after flushing.
	ExitFunction = "ktn_rt_exit"

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

	// EprintIntFunction, EprintFloatFunction, EprintBoolFunction, and
	// EprintStringFunction write one value of the corresponding primitive type to
	// standard error.
	EprintIntFunction    = "ktn_rt_eprint_int"
	EprintFloatFunction  = "ktn_rt_eprint_float"
	EprintBoolFunction   = "ktn_rt_eprint_bool"
	EprintStringFunction = "ktn_rt_eprint_string"

	// EprintSpaceFunction writes a single space to standard error, and
	// EprintNewlineFunction terminates a diagnostic line and flushes both streams.
	EprintSpaceFunction   = "ktn_rt_eprint_space"
	EprintNewlineFunction = "ktn_rt_eprint_newline"
)

// HeaderSource returns the C declarations shared with generated code.
func HeaderSource() string { return headerSource }

// Source returns the C implementation of the runtime.
func Source() string { return sourceSource }
