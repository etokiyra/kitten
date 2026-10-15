/*
 * Kitten runtime: implementation. See kitten_runtime.h for the interface.
 */
#include "kitten_runtime.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/*
 * Reports an unrecoverable runtime error to standard error and terminates the
 * program. Used for conditions that cannot be recovered from, such as an
 * out-of-range array index, an allocation that would overflow, or inspecting a
 * null union reference.
 */
void ktn_rt_fail(const char *message) {
    fflush(stdout);
    fputs("kitten: ", stderr);
    fputs(message, stderr);
    fputc('\n', stderr);
    exit(1);
}

/*
 * Reports an unrecoverable error that concerns a path, including the operating
 * system message for a saved errno value.
 */
static void ktn_rt_fail_path(const char *action, const char *path, int error) {
    char message[1024];
    snprintf(message, sizeof(message), "%s '%s': %s", action, path, strerror(error));
    ktn_rt_fail(message);
}

void ktn_rt_init(void) {
    GC_INIT();
}

void *ktn_rt_alloc(size_t size) {
    return GC_MALLOC(size);
}

/*
 * The zero value of a string is the empty string, so a null pointer is treated
 * as "". This makes unassigned string array elements safe to read.
 */
static const char *ktn_rt_string_value(const char *value) {
    return value == NULL ? "" : value;
}

bool ktn_rt_string_equal(const char *left, const char *right) {
    return strcmp(ktn_rt_string_value(left), ktn_rt_string_value(right)) == 0;
}

const char *ktn_rt_string_concat(const char *left, const char *right) {
    left = ktn_rt_string_value(left);
    right = ktn_rt_string_value(right);

    size_t left_length = strlen(left);
    size_t right_length = strlen(right);

    /*
     * Guard the allocation size against overflow. Two strings this large cannot
     * both exist in memory, so this is defensive, but it keeps the size
     * computation well defined instead of allocating too little.
     */
    if (left_length > SIZE_MAX - right_length - 1) {
        ktn_rt_fail("string concatenation result is too large");
    }

    size_t result_length = left_length + right_length;
    char *result = (char *)ktn_rt_alloc(result_length + 1);
    memcpy(result, left, left_length);
    memcpy(result + left_length, right, right_length + 1);
    return result;
}

void ktn_rt_string_append(const char **slot, const char *value) {
    *slot = ktn_rt_string_concat(*slot, value);
}

ktn_array *ktn_rt_array_new(int64_t length, int64_t element_size) {
    if (length < 0) {
        ktn_rt_fail("array length cannot be negative");
    }
    if (element_size <= 0) {
        ktn_rt_fail("array element size must be positive");
    }
    if ((uint64_t)length > (uint64_t)SIZE_MAX / (uint64_t)element_size) {
        ktn_rt_fail("array allocation is too large");
    }

    ktn_array *array = (ktn_array *)ktn_rt_alloc(sizeof(ktn_array));
    array->length = length;
    array->element_size = element_size;

    size_t bytes = (size_t)length * (size_t)element_size;
    if (bytes == 0) {
        /* Never make a zero-size allocation; an empty array is still a valid
         * object with a non-null data pointer. */
        bytes = 1;
    }
    array->data = ktn_rt_alloc(bytes);
    return array;
}

void *ktn_rt_array_at(const ktn_array *array, int64_t index) {
    if (array == NULL) {
        ktn_rt_fail("array access on a null array reference");
    }
    if (index < 0 || index >= array->length) {
        char message[128];
        snprintf(message, sizeof(message), "array index %lld is out of range for length %lld", (long long)index, (long long)array->length);
        ktn_rt_fail(message);
    }
    return (char *)array->data + (size_t)index * (size_t)array->element_size;
}

int64_t ktn_rt_array_length(const ktn_array *array) {
    if (array == NULL) {
        ktn_rt_fail("length of a null array reference");
    }
    return array->length;
}

const char *ktn_rt_read_file(const char *path) {
    if (path == NULL) {
        ktn_rt_fail("read_file: path is null");
    }

    FILE *file = fopen(path, "rb");
    if (file == NULL) {
        int error = errno;
        ktn_rt_fail_path("read_file: cannot open", path, error);
    }

    if (fseek(file, 0, SEEK_END) != 0) {
        int error = errno;
        fclose(file);
        ktn_rt_fail_path("read_file: cannot determine the size of", path, error);
    }

    long length = ftell(file);
    if (length < 0) {
        int error = errno;
        fclose(file);
        ktn_rt_fail_path("read_file: cannot determine the size of", path, error);
    }
    if ((unsigned long)length > KTN_RT_MAX_FILE_SIZE) {
        fclose(file);
        ktn_rt_fail("read_file: file is larger than the maximum supported size");
    }

    if (fseek(file, 0, SEEK_SET) != 0) {
        int error = errno;
        fclose(file);
        ktn_rt_fail_path("read_file: cannot rewind", path, error);
    }

    char *buffer = (char *)ktn_rt_alloc((size_t)length + 1);
    size_t read = fread(buffer, 1, (size_t)length, file);
    if (ferror(file)) {
        int error = errno;
        fclose(file);
        ktn_rt_fail_path("read_file: cannot read", path, error);
    }

    buffer[read] = '\0';
    fclose(file);
    return buffer;
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
    fputs(ktn_rt_string_value(value), stdout);
}

void ktn_rt_print_space(void) {
    fputc(' ', stdout);
}

void ktn_rt_print_newline(void) {
    fputc('\n', stdout);
}
