#!/bin/sh
# purr-bootstrap.sh - temporary POSIX-sh wrapper around the Kitten-written driver.
#
# It mirrors the Go CLI's build/run surface for the subset that the Kitten
# driver supports: build <src> [-o out] and run <src> [args...]. The driver is
# expected in PURR_BOOTSTRAP_DRIVER (default: purr-stage1 next to this script) and
# the runtime sources in PURR_RUNTIME_DIR (default: runtime/csrc in this
# repository). The wrapper owns absolute paths, temporary files, the C compiler,
# CC, GC flags, output naming, and run-status propagation.

set -u

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd -P) || exit 2
repo_root=$(CDPATH= cd "$script_dir/../.." && pwd -P) || exit 2
driver=${PURR_BOOTSTRAP_DRIVER:-$script_dir/purr-stage1}
runtime_dir=${PURR_RUNTIME_DIR:-$repo_root/runtime/csrc}

usage_text='Kitten compiler

Usage:
  purr build <file.ktn> [-o <output>]   Compile a Kitten source file
  purr run <file.ktn>                   Compile and run a Kitten source file

Options:
  -o, --output <path>   Write the executable to <path> (build only)

The C compiler is taken from CC, or discovered as cc, gcc, or clang.
Kitten programs are linked against the Boehm garbage collector (libgc).'

usage() {
    printf '%s\n' "$usage_text"
}

build_usage_error() {
    printf 'purr build: %s\n\n' "$1" >&2
    usage >&2
    exit 2
}

run_usage_error() {
    printf 'purr run: %s\n\n' "$1" >&2
    usage >&2
    exit 2
}

cleanup_build() {
    if [ -n "${build_tmp:-}" ] && [ -d "$build_tmp" ]; then
        rm -rf "$build_tmp"
    fi
    build_tmp=""
}

absolute_path() {
    case $1 in
        /*) printf '%s\n' "$1" ;;
        *) printf '%s/%s\n' "$(pwd -P)" "$1" ;;
    esac
}

select_compiler() {
    if [ -n "${CC:-}" ]; then
        compiler=$CC
        return 0
    fi
    for candidate in cc gcc clang; do
        if command -v "$candidate" >/dev/null 2>&1; then
            compiler=$candidate
            return 0
        fi
    done
    return 1
}

gc_flags() {
    gc_cflags=""
    gc_libs="-lgc"
    if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists bdw-gc >/dev/null 2>&1; then
        gc_cflags=$(pkg-config --cflags bdw-gc 2>/dev/null || true)
        gc_libs=$(pkg-config --libs bdw-gc 2>/dev/null || true)
    fi
    if [ -z "$gc_libs" ]; then
        gc_libs="-lgc"
    fi
}

# prepare_build compiles src_abs to out_abs. An empty out_abs means a temporary
# program inside build_tmp. It leaves build_tmp set for the caller to clean.
prepare_build() {
    src_display=$1
    src_abs=$2
    out_display=$3
    out_abs=$4
    announce=$5

    if [ ! -x "$driver" ]; then
        printf 'purr: Kitten driver not found or not executable: %s\n' "$driver" >&2
        return 1
    fi
    if [ ! -f "$runtime_dir/kitten_runtime.c" ] || [ ! -f "$runtime_dir/kitten_runtime.h" ]; then
        printf 'purr: runtime sources not found in %s\n' "$runtime_dir" >&2
        return 1
    fi
    if ! select_compiler; then
        printf 'purr: no C compiler found; set CC or install cc, gcc, or clang\n' >&2
        return 1
    fi
    gc_flags

    build_tmp=$(mktemp -d "${TMPDIR:-/tmp}/purr-build.XXXXXX") || {
        printf 'purr: cannot create a temporary build directory\n' >&2
        return 1
    }
    trap 'cleanup_build' EXIT HUP INT TERM

    if [ -z "$out_abs" ]; then
        out_abs=$build_tmp/program
        out_display=$out_abs
    fi

    if ! "$driver" "$src_abs" >"$build_tmp/program.c"; then
        status=$?
        cleanup_build
        trap - EXIT HUP INT TERM
        return "$status"
    fi

    if ! cp "$runtime_dir/kitten_runtime.c" "$build_tmp/kitten_runtime.c" ||
       ! cp "$runtime_dir/kitten_runtime.h" "$build_tmp/kitten_runtime.h"; then
        printf 'purr: cannot copy the runtime sources\n' >&2
        cleanup_build
        trap - EXIT HUP INT TERM
        return 1
    fi

    log=$build_tmp/compiler.log
    # shellcheck disable=SC2086
    if ! (cd "$build_tmp" && $compiler $gc_cflags -std=c11 -Wall -Wextra \
            -o "$out_abs" program.c kitten_runtime.c $gc_libs) >"$log" 2>&1; then
        status=$?
        printf '%s: C compilation failed: exit status %d\n' "$src_display" "$status" >&2
        cat "$log" >&2
        cleanup_build
        trap - EXIT HUP INT TERM
        return 1
    fi
    if [ -s "$log" ]; then
        cat "$log" >&2
    fi

    if [ -d "$out_abs" ]; then
        printf '%s: output path %s is a directory\n' "$src_display" "$out_abs" >&2
        cleanup_build
        trap - EXIT HUP INT TERM
        return 1
    fi
    if [ ! -f "$out_abs" ]; then
        printf '%s: C compiler reported success but %s was not produced\n' "$src_display" "$out_abs" >&2
        cleanup_build
        trap - EXIT HUP INT TERM
        return 1
    fi

    build_out=$out_abs
    if [ "$announce" = yes ]; then
        printf 'built %s\n' "$out_display"
    fi
    return 0
}

build_command() {
    source=""
    output=""
    positional_count=0
    end_options=0

    while [ $# -gt 0 ]; do
        argument=$1
        if [ "$end_options" -eq 1 ]; then
            positional_count=$((positional_count + 1))
            if [ "$positional_count" -eq 1 ]; then
                source=$argument
            fi
            shift
            continue
        fi
        case $argument in
            -o|--output)
                if [ $# -lt 2 ]; then
                    build_usage_error "$argument requires a path"
                fi
                output=$2
                shift 2
                ;;
            -o=*)
                output=${argument#-o=}
                shift
                ;;
            --output=*)
                output=${argument#--output=}
                shift
                ;;
            --)
                end_options=1
                shift
                ;;
            -?*)
                build_usage_error "unknown option \"$argument\""
                ;;
            *)
                positional_count=$((positional_count + 1))
                if [ "$positional_count" -eq 1 ]; then
                    source=$argument
                fi
                shift
                ;;
        esac
    done

    if [ "$positional_count" -eq 0 ]; then
        build_usage_error "missing source file"
    fi
    if [ "$positional_count" -gt 1 ]; then
        build_usage_error "expected one source file, got $positional_count"
    fi
    case $source in
        *.ktn) ;;
        *) build_usage_error "\"$source\" is not a Kitten source file (expected a .ktn file)" ;;
    esac

    if [ -z "$output" ]; then
        base=${source##*/}
        base=${base%.ktn}
        if [ -z "$base" ] || [ "$base" = "." ]; then
            output=a.out
        else
            output=$base
        fi
    fi

    source_abs=$(absolute_path "$source")
    output_abs=$(absolute_path "$output")
    prepare_build "$source" "$source_abs" "$output" "$output_abs" yes
    status=$?
    cleanup_build
    trap - EXIT HUP INT TERM
    return "$status"
}

run_command() {
    source=""
    end_options=0

    while [ $# -gt 0 ]; do
        argument=$1
        if [ "$end_options" -eq 1 ]; then
            source=$argument
            shift
            break
        fi
        case $argument in
            -o|--output)
                run_usage_error "-o is not supported; run compiles to a temporary executable"
                ;;
            -o=*)
                run_usage_error "-o is not supported; run compiles to a temporary executable"
                ;;
            --output=*)
                run_usage_error "-o is not supported; run compiles to a temporary executable"
                ;;
            --)
                end_options=1
                shift
                ;;
            -?*)
                run_usage_error "unknown option \"$argument\""
                ;;
            *)
                source=$argument
                shift
                break
                ;;
        esac
    done

    if [ -z "$source" ]; then
        run_usage_error "missing source file"
    fi
    case $source in
        *.ktn) ;;
        *) run_usage_error "\"$source\" is not a Kitten source file (expected a .ktn file)" ;;
    esac

    source_abs=$(absolute_path "$source")
    prepare_build "$source" "$source_abs" "" "" no
    status=$?
    if [ "$status" -ne 0 ]; then
        cleanup_build
        trap - EXIT HUP INT TERM
        return "$status"
    fi

    "$build_out" "$@"
    status=$?
    cleanup_build
    trap - EXIT HUP INT TERM
    return "$status"
}

if [ $# -eq 0 ]; then
    usage >&2
    exit 2
fi

command=$1
shift
case $command in
    help|-h|--help)
        usage
        exit 0
        ;;
    build)
        build_command "$@"
        exit $?
        ;;
    run)
        run_command "$@"
        exit $?
        ;;
    *)
        printf 'purr: unknown command "%s"\n\n' "$command" >&2
        usage >&2
        exit 2
        ;;
esac
