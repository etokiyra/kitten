/*
 * Kitten runtime: implementation. See kitten_runtime.h for the interface.
 */
#include "kitten_runtime.h"

#include <stdio.h>
#include <string.h>

void ktn_rt_init(void) {
    GC_INIT();
}

void *ktn_rt_alloc(size_t size) {
    return GC_MALLOC(size);
}

bool ktn_rt_string_equal(const char *left, const char *right) {
    return strcmp(left, right) == 0;
}

void ktn_rt_print_int(int64_t value) {
    printf("%lld", (long long)value);
}

void ktn_rt_print_float(double value) {
    printf("%g", value);
}

void ktn_rt_print_bool(bool value) {
    fputs(value ? "true" : "false", stdout);
}

void ktn_rt_print_string(const char *value) {
    fputs(value, stdout);
}

void ktn_rt_print_space(void) {
    fputc(' ', stdout);
}

void ktn_rt_print_newline(void) {
    fputc('\n', stdout);
}
