/*
 * Kitten runtime: declarations shared with generated code.
 *
 * This file is part of the Kitten language runtime. Generated Kitten programs
 * include it, and kitten_runtime.c implements it. It has no dependency on the
 * compiler's implementation language, so a future self-hosted Kitten compiler
 * can use the same interface.
 */
#ifndef KITTEN_RUNTIME_H
#define KITTEN_RUNTIME_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/*
 * Boehm-Demers-Weiser conservative garbage collector. Accept either the
 * <gc/gc.h> or <gc.h> layout depending on how the library was installed.
 */
#if defined(__has_include)
#  if __has_include(<gc/gc.h>)
#    include <gc/gc.h>
#  else
#    include <gc.h>
#  endif
#else
#  include <gc.h>
#endif

/*
 * Initializes the garbage collector. It must be called before any allocation.
 */
void ktn_rt_init(void);

/*
 * Allocates a zero-initialized, pointer-scanned, garbage-collected object of
 * size bytes. The collector reclaims the object automatically; Kitten programs
 * never free memory by hand.
 */
void *ktn_rt_alloc(size_t size);

/*
 * Value equality for Kitten strings, which are immutable NUL-terminated byte
 * strings. Comparison is by content, never by pointer identity. A null pointer
 * is treated as the empty string, so the zero value of a string behaves as "".
 */
bool ktn_rt_string_equal(const char *left, const char *right);

/*
 * Returns a new string that is left followed by right. The result is a fresh,
 * exactly-sized, GC-managed buffer, so it stays valid as long as Kitten code
 * can reach it and never needs a manual free. A null operand is treated as the
 * empty string, so concatenating empty strings yields an empty string.
 *
 * The representation is NUL-terminated, so embedded NUL bytes are not
 * supported; the language cannot create them.
 */
const char *ktn_rt_string_concat(const char *left, const char *right);

/*
 * Appends value to the string stored in *slot, by assigning *slot the
 * concatenation of its current value and value. It takes the address of the
 * slot so that the target is evaluated exactly once at the call site.
 */
void ktn_rt_string_append(const char **slot, const char *value);

/*
 * A dynamically sized array.
 *
 * length is the number of elements and element_size is the size in bytes of one
 * element. data points to a GC-managed, zero-initialized buffer of
 * length * element_size bytes. Arrays are references: assigning one array to
 * another copies the reference, and both names then share the same storage.
 */
typedef struct ktn_array {
    int64_t length;
    int64_t element_size;
    void *data;
} ktn_array;

/*
 * Allocates an array of length elements, each element_size bytes. Every element
 * is zero-initialized by the collector. A negative length or an allocation that
 * would overflow is a fatal runtime error.
 */
ktn_array *ktn_rt_array_new(int64_t length, int64_t element_size);

/*
 * Returns a pointer to the element at index. The index is checked against the
 * array length; a negative or out-of-range index, or a null array reference, is
 * a fatal runtime error rather than undefined behavior.
 */
void *ktn_rt_array_at(const ktn_array *array, int64_t index);

/*
 * Returns the number of elements in the array.
 */
int64_t ktn_rt_array_length(const ktn_array *array);

/*
 * The largest file, in bytes, that ktn_rt_read_file will read. Larger files are
 * rejected instead of allocated, so a program cannot be made to allocate an
 * unbounded buffer from a file on disk.
 */
#define KTN_RT_MAX_FILE_SIZE ((size_t)256 * 1024 * 1024)

/*
 * Reports an unrecoverable runtime error to standard error and terminates the
 * program with a nonzero exit status. It is used by the runtime itself and by
 * generated code, for example when a match inspects an invalid variant tag or a
 * null union reference.
 *
 * It never returns, which is declared so that generated switches with a
 * fail-fast default case do not warn about falling off the end of a function.
 */
_Noreturn void ktn_rt_fail(const char *message);

/*
 * Reads the entire contents of the regular file at path into a new,
 * GC-managed, NUL-terminated string and returns it. An empty file yields the
 * empty string.
 *
 * The path is a Kitten string. Failure is fatal: a missing or unreadable file,
 * a path that is not a seekable regular file, a file larger than
 * KTN_RT_MAX_FILE_SIZE, or a read error terminates the program with a message
 * on standard error and a nonzero exit status. The file is always closed.
 *
 * Because strings are NUL-terminated, content after the first NUL byte in the
 * file is not represented. The returned string is a snapshot of the file at the
 * time of the read; if the file shrinks while it is being read, fewer bytes are
 * returned.
 */
const char *ktn_rt_read_file(const char *path);

/*
 * Built-in print helpers. Each writes a single value. Callers separate
 * arguments with single spaces and terminate the line with
 * ktn_rt_print_newline.
 */
void ktn_rt_print_int(int64_t value);
void ktn_rt_print_float(double value);
void ktn_rt_print_bool(bool value);
void ktn_rt_print_string(const char *value);
void ktn_rt_print_space(void);
void ktn_rt_print_newline(void);

#endif /* KITTEN_RUNTIME_H */
