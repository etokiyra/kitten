/*
 * Kitten runtime: implementation. See kitten_runtime.h for the interface.
 *
 * This file is part of the Kitten language runtime, which is compiled into
 * every generated Kitten program.
 *
 * The Kitten runtime is licensed permissively, unlike the compiler itself, so
 * that programs compiled with Kitten can be distributed under any terms the
 * author chooses. See LICENSE in this directory.
 *
 * SPDX-License-Identifier: MIT
 *
 * Copyright (c) 2026 etokiyra
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to
 * deal in the Software without restriction, including without limitation the
 * rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
 * sell copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
 * FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS
 * IN THE SOFTWARE.
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
static void ktn_rt_fail_path(const char *action, const ktn_string *path, int error) {
    char message[1024];
    snprintf(message, sizeof(message), "%s '%s': %s", action, path->data, strerror(error));
    ktn_rt_fail(message);
}

void ktn_rt_init(void) {
    GC_INIT();
}

void *ktn_rt_alloc(size_t size) {
    return GC_MALLOC(size);
}

/*
 * The static empty string. The zero value of a string is a null pointer, so
 * normalization maps every null string to this object, whose data is the static
 * empty C string. This makes unassigned string array elements and struct fields
 * safe to read.
 */
static const ktn_string ktn_rt_empty_string = { 0, "" };

/*
 * Returns value, or the static empty string when value is null.
 *
 * Every runtime function that reads a string normalizes first, so no caller has
 * to check for a null reference and no function has to read through a null
 * pointer.
 */
static const ktn_string *ktn_rt_string_value(const ktn_string *value) {
    return value == NULL ? &ktn_rt_empty_string : value;
}

/*
 * Allocates a new string of exactly length bytes and returns it.
 *
 * The header is fully initialized: length is set, data points at the length
 * bytes that immediately follow it, and a NUL terminator is written one past
 * them, so the value is always a valid C string even while it is being filled.
 * The collector zeroes the allocation, so the bytes are a defined all-zero
 * sequence rather than indeterminate storage, and the bytes are written by the
 * caller through ktn_rt_string_bytes before the string becomes observable.
 */
static ktn_string *ktn_rt_string_allocate(int64_t length) {
    if (length < 0) {
        ktn_rt_fail("string length cannot be negative");
    }
    if ((uint64_t)length > (uint64_t)SIZE_MAX - sizeof(ktn_string) - 1) {
        ktn_rt_fail("string is too large");
    }

    ktn_string *result = (ktn_string *)ktn_rt_alloc(sizeof(ktn_string) + (size_t)length + 1);
    result->length = length;
    char *bytes = (char *)(result + 1);
    bytes[length] = '\0';
    result->data = bytes;
    return result;
}

/*
 * Returns the bytes of a string that was just allocated, for filling in.
 *
 * A string's bytes are immutable once the string is observable, but the bytes
 * of a freshly allocated object are written during construction. Only
 * construction sites, which hold the object just returned by
 * ktn_rt_string_allocate, call this.
 */
static char *ktn_rt_string_bytes(ktn_string *value) {
    return (char *)value->data;
}

bool ktn_rt_string_equal(const ktn_string *left, const ktn_string *right) {
    left = ktn_rt_string_value(left);
    right = ktn_rt_string_value(right);

    if (left->length != right->length) {
        return false;
    }
    if (left->length == 0) {
        return true;
    }
    return memcmp(left->data, right->data, (size_t)left->length) == 0;
}

const ktn_string *ktn_rt_string_concat(const ktn_string *left, const ktn_string *right) {
    left = ktn_rt_string_value(left);
    right = ktn_rt_string_value(right);

    /*
     * Guard the combined length against overflow. Two strings this large cannot
     * both exist in memory, so this is defensive, but it keeps the size
     * computation well defined instead of allocating too little.
     */
    if (left->length > INT64_MAX - right->length) {
        ktn_rt_fail("string concatenation result is too large");
    }

    int64_t length = left->length + right->length;
    ktn_string *result = ktn_rt_string_allocate(length);
    char *bytes = ktn_rt_string_bytes(result);
    memcpy(bytes, left->data, (size_t)left->length);
    memcpy(bytes + left->length, right->data, (size_t)right->length);
    return result;
}

void ktn_rt_string_append(const ktn_string **slot, const ktn_string *value) {
    *slot = ktn_rt_string_concat(*slot, value);
}

int64_t ktn_rt_string_length(const ktn_string *value) {
    return ktn_rt_string_value(value)->length;
}

int64_t ktn_rt_string_at(const ktn_string *value, int64_t index) {
    const ktn_string *checked = ktn_rt_string_value(value);
    if (index < 0 || index >= checked->length) {
        char message[128];
        snprintf(message, sizeof(message), "string index %lld is out of range for length %lld", (long long)index, (long long)checked->length);
        ktn_rt_fail(message);
    }
    return (int64_t)(unsigned char)checked->data[index];
}

const ktn_string *ktn_rt_substring(const ktn_string *value, int64_t start, int64_t end) {
    const ktn_string *checked = ktn_rt_string_value(value);
    if (start < 0 || end < start || end > checked->length) {
        char message[160];
        snprintf(message, sizeof(message), "substring range %lld..%lld is out of range for length %lld", (long long)start, (long long)end, (long long)checked->length);
        ktn_rt_fail(message);
    }

    int64_t count = end - start;
    ktn_string *result = ktn_rt_string_allocate(count);
    memcpy(ktn_rt_string_bytes(result), checked->data + start, (size_t)count);
    return result;
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

/*
 * The process argument vector saved by ktn_rt_set_args. It is initialized to an
 * empty vector so that ktn_rt_argv is safe before the generated entry point has
 * run, although generated programs always set it before user code.
 */
static int ktn_rt_saved_argc = 0;
static char **ktn_rt_saved_argv = NULL;

void ktn_rt_set_args(int argc, char **argv) {
    ktn_rt_saved_argc = argc < 0 ? 0 : argc;
    ktn_rt_saved_argv = argv;
}

ktn_array *ktn_rt_argv(void) {
    int count = ktn_rt_saved_argc;
    ktn_array *result = ktn_rt_array_new((int64_t)count, sizeof(const ktn_string *));
    const ktn_string **slots = (const ktn_string **)result->data;
    int index = 0;
    while (index < count) {
        const char *text = "";
        if (ktn_rt_saved_argv != NULL && ktn_rt_saved_argv[index] != NULL) {
            text = ktn_rt_saved_argv[index];
        }
        size_t length = strlen(text);
        ktn_string *value = ktn_rt_string_allocate((int64_t)length);
        memcpy(ktn_rt_string_bytes(value), text, length);
        slots[index] = value;
        index++;
    }
    return result;
}

/*
 * FNV-1a hash of a string, computed over its bytes so that two distinct strings
 * with the same contents hash identically.
 */
static uint64_t ktn_rt_string_hash(const ktn_string *value) {
    uint64_t hash = 14695981039346656037ULL; /* FNV-1a 64-bit offset basis */
    const unsigned char *bytes = (const unsigned char *)value->data;
    for (int64_t index = 0; index < value->length; index++) {
        hash ^= (uint64_t)bytes[index];
        hash *= 1099511628211ULL; /* FNV-1a 64-bit prime */
    }
    return hash;
}

/*
 * Returns the index of the slot holding key, or -1 when the key is absent.
 *
 * The table uses open addressing with linear probing over a power-of-two
 * capacity. Entries are never removed, so the first empty slot proves the key
 * is absent. The probe is bounded by the capacity so that a corrupted table
 * cannot loop forever.
 */
static int64_t ktn_rt_map_find(const ktn_map *map, const ktn_string *key, uint64_t hash) {
    if (map->capacity == 0) {
        return -1;
    }
    size_t mask = (size_t)map->capacity - 1;
    size_t index = (size_t)hash & mask;
    for (int64_t probes = 0; probes < map->capacity; probes++) {
        const ktn_string *existing = map->keys[index];
        if (existing == NULL) {
            return -1;
        }
        if (map->hashes[index] == hash && ktn_rt_string_equal(existing, key)) {
            return (int64_t)index;
        }
        index = (index + 1) & mask;
    }
    return -1;
}

/* Stores key and a copy of the value's bytes into the first empty slot. */
static void ktn_rt_map_place(ktn_map *map, const ktn_string *key, uint64_t hash, const void *value) {
    size_t mask = (size_t)map->capacity - 1;
    size_t index = (size_t)hash & mask;
    for (int64_t probes = 0; probes < map->capacity; probes++) {
        if (map->keys[index] == NULL) {
            map->keys[index] = key;
            map->hashes[index] = hash;
            memcpy((char *)map->values + index * (size_t)map->value_size, value, (size_t)map->value_size);
            map->length++;
            return;
        }
        index = (index + 1) & mask;
    }
    ktn_rt_fail("map is full");
}

/*
 * Grows the table when inserting one more entry would exceed a 3/4 load factor.
 * All existing entries are rehashed into the new table, so no entry is lost.
 */
static void ktn_rt_map_reserve(ktn_map *map) {
    if (map->capacity != 0 && (map->length + 1) * 4 <= map->capacity * 3) {
        return;
    }
    if (map->capacity > INT64_MAX / 2) {
        ktn_rt_fail("map is too large");
    }
    int64_t capacity = map->capacity == 0 ? 8 : map->capacity * 2;

    if ((uint64_t)capacity > (uint64_t)SIZE_MAX / sizeof(const ktn_string *) ||
        (uint64_t)capacity > (uint64_t)SIZE_MAX / sizeof(uint64_t) ||
        (uint64_t)capacity > (uint64_t)SIZE_MAX / (uint64_t)map->value_size) {
        ktn_rt_fail("map allocation is too large");
    }

    /* Keep the old table reachable while the new one is built, so the collector
     * does not reclaim keys or pointer values that are still being copied. */
    const ktn_string **old_keys = map->keys;
    uint64_t *old_hashes = map->hashes;
    void *old_values = map->values;
    int64_t old_capacity = map->capacity;

    map->capacity = capacity;
    map->length = 0;
    map->keys = (const ktn_string **)ktn_rt_alloc((size_t)capacity * sizeof(const ktn_string *));
    map->hashes = (uint64_t *)ktn_rt_alloc((size_t)capacity * sizeof(uint64_t));
    map->values = ktn_rt_alloc((size_t)capacity * (size_t)map->value_size);

    for (int64_t index = 0; index < old_capacity; index++) {
        if (old_keys[index] != NULL) {
            ktn_rt_map_place(map, old_keys[index], old_hashes[index],
                             (const char *)old_values + (size_t)index * (size_t)map->value_size);
        }
    }
}

ktn_map *ktn_rt_map_new(int64_t value_size) {
    if (value_size <= 0) {
        ktn_rt_fail("map value size must be positive");
    }
    ktn_map *map = (ktn_map *)ktn_rt_alloc(sizeof(ktn_map));
    map->length = 0;
    map->capacity = 0;
    map->value_size = value_size;
    map->keys = NULL;
    map->hashes = NULL;
    map->values = NULL;
    return map;
}

void ktn_rt_map_set(ktn_map *map, const ktn_string *key, const void *value) {
    if (map == NULL) {
        ktn_rt_fail("map assignment on a null map reference");
    }
    if (value == NULL) {
        ktn_rt_fail("map value is missing");
    }

    /* Normalize a null key to the static empty string so that stored keys are
     * never null and an empty key can be distinguished from an empty slot. */
    const ktn_string *checked = ktn_rt_string_value(key);
    uint64_t hash = ktn_rt_string_hash(checked);

    int64_t index = ktn_rt_map_find(map, checked, hash);
    if (index >= 0) {
        memcpy((char *)map->values + (size_t)index * (size_t)map->value_size, value, (size_t)map->value_size);
        return;
    }

    ktn_rt_map_reserve(map);
    ktn_rt_map_place(map, checked, hash, value);
}

void *ktn_rt_map_at(const ktn_map *map, const ktn_string *key) {
    if (map == NULL) {
        ktn_rt_fail("map lookup on a null map reference");
    }
    const ktn_string *checked = ktn_rt_string_value(key);
    int64_t index = ktn_rt_map_find(map, checked, ktn_rt_string_hash(checked));
    if (index < 0) {
        char message[256];
        snprintf(message, sizeof(message), "map has no entry for key \"%.*s\"", 160, checked->data);
        ktn_rt_fail(message);
    }
    return (char *)map->values + (size_t)index * (size_t)map->value_size;
}

bool ktn_rt_map_has(const ktn_map *map, const ktn_string *key) {
    if (map == NULL) {
        ktn_rt_fail("has on a null map reference");
    }
    const ktn_string *checked = ktn_rt_string_value(key);
    return ktn_rt_map_find(map, checked, ktn_rt_string_hash(checked)) >= 0;
}

int64_t ktn_rt_map_length(const ktn_map *map) {
    if (map == NULL) {
        ktn_rt_fail("length of a null map reference");
    }
    return map->length;
}

const ktn_string *ktn_rt_read_file(const ktn_string *path) {
    const ktn_string *checked = ktn_rt_string_value(path);

    FILE *file = fopen(checked->data, "rb");
    if (file == NULL) {
        int error = errno;
        ktn_rt_fail_path("read_file: cannot open", checked, error);
    }

    if (fseek(file, 0, SEEK_END) != 0) {
        int error = errno;
        fclose(file);
        ktn_rt_fail_path("read_file: cannot determine the size of", checked, error);
    }

    long length = ftell(file);
    if (length < 0) {
        int error = errno;
        fclose(file);
        ktn_rt_fail_path("read_file: cannot determine the size of", checked, error);
    }
    if ((unsigned long)length > KTN_RT_MAX_FILE_SIZE) {
        fclose(file);
        ktn_rt_fail("read_file: file is larger than the maximum supported size");
    }

    if (fseek(file, 0, SEEK_SET) != 0) {
        int error = errno;
        fclose(file);
        ktn_rt_fail_path("read_file: cannot rewind", checked, error);
    }

    ktn_string *result = ktn_rt_string_allocate((int64_t)length);
    size_t read = fread(ktn_rt_string_bytes(result), 1, (size_t)length, file);
    if (ferror(file)) {
        int error = errno;
        fclose(file);
        ktn_rt_fail_path("read_file: cannot read", checked, error);
    }

    /* A file that shrank while it was being read yields the bytes actually read,
     * so the stored length is adjusted to match the bytes present. */
    result->length = (int64_t)read;
    ktn_rt_string_bytes(result)[read] = '\0';
    fclose(file);
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

void ktn_rt_print_string(const ktn_string *value) {
    const ktn_string *checked = ktn_rt_string_value(value);
    if (checked->length > 0) {
        fwrite(checked->data, 1, (size_t)checked->length, stdout);
    }
}

void ktn_rt_print_space(void) {
    fputc(' ', stdout);
}

void ktn_rt_print_newline(void) {
    fputc('\n', stdout);
}
