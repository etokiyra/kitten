package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// discoverCompiler returns the C compiler command to use, as a program followed
// by any fixed arguments.
//
// It honors the CC environment variable when it is set, allowing the user to
// select a specific compiler. CC may contain arguments, for example
// CC="cc -m64". Otherwise it looks for cc, gcc, or clang, in that order. It
// never installs anything and never falls back to anything other than a real
// system C compiler.
func discoverCompiler() ([]string, error) {
	if configured := strings.TrimSpace(os.Getenv("CC")); configured != "" {
		fields := strings.Fields(configured)
		if len(fields) == 0 {
			return nil, fmt.Errorf("CC is set but empty")
		}
		resolved, err := exec.LookPath(fields[0])
		if err != nil {
			return nil, fmt.Errorf("CC=%q: %q not found", configured, fields[0])
		}
		return append([]string{resolved}, fields[1:]...), nil
	}

	for _, candidate := range []string{"cc", "gcc", "clang"} {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return []string{resolved}, nil
		}
	}
	return nil, fmt.Errorf("no C compiler found; set CC or install cc, gcc, or clang")
}

// gcBuildFlags returns the compiler and linker flags needed to build against
// Boehm GC.
//
// When pkg-config knows about bdw-gc, its flags are used so the header and
// library are found wherever they are installed. Otherwise it falls back to
// linking against -lgc with the system default include path.
func gcBuildFlags() (compileFlags, linkFlags []string) {
	if _, err := exec.LookPath("pkg-config"); err == nil {
		if exec.Command("pkg-config", "--exists", "bdw-gc").Run() == nil {
			if output, err := exec.Command("pkg-config", "--cflags", "bdw-gc").Output(); err == nil {
				compileFlags = strings.Fields(string(output))
			}
			if output, err := exec.Command("pkg-config", "--libs", "bdw-gc").Output(); err == nil {
				linkFlags = strings.Fields(string(output))
			}
		}
	}
	if len(linkFlags) == 0 {
		linkFlags = []string{"-lgc"}
	}
	return compileFlags, linkFlags
}
