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
 * Value equality for Kitten strings, which are NUL-terminated byte strings.
 */
bool ktn_rt_string_equal(const char *left, const char *right);

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
