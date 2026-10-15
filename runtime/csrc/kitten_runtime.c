/*
 * Kitten runtime: implementation. See kitten_runtime.h for the interface.
 */
#include "kitten_runtime.h"

#include <stdio.h>
#include <stdlib.h>
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

const char *ktn_rt_string_concat(const char *left, const char *right) {
    size_t left_length = strlen(left);
    size_t right_length = strlen(right);

    /*
     * Guard the allocation size against overflow. Two strings this large cannot
     * both exist in memory, so this is defensive, but it keeps the size
     * computation well defined instead of allocating too little.
     */
    if (left_length > SIZE_MAX - right_length - 1) {
        fputs("kitten: string concatenation result is too large\n", stderr);
        exit(1);
    }

    size_t result_length = left_length + right_length;
    char *result = (char *)ktn_rt_alloc(result_length + 1);
    memcpy(result, left, left_length);
    memcpy(result + left_length, right, right_length + 1);
    return result;
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
