package main

// Differential testing for the fixtures under tests/ through the Kitten-written
// translation-unit emitter (selfhost/codegen/unit.ktn, driven by selfhost/unit.ktn).
//
// Each fixture is handed to the Kitten backend, which prints a whole C translation
// unit. A fixture the backend can compile is required to match the reference
// generator's unit byte for byte, then to be compiled against the real runtime and
// run, matching its own expected output. A fixture that still needs an expression
// kind the Kitten backend does not implement must report exactly that kind, so a
// program that is not generated can never look like one that is.
//
// The C compiler is the one the environment selects (CC), so running the suite
// under GCC and under Clang checks both, exactly as the program tests do.
//
// The nine top-level fixtures are single file. The modules/ fixture is a
// module-loader case and is not part of this list, because the Kitten backend has
// no module loader yet.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kitten/checker"
	"kitten/codegen"
)

// fixtureNames lists the nine top-level fixtures under tests/, in a stable order.
func fixtureNames() []string {
	return []string{"hello", "fizzbuzz", "math", "room", "strings", "arrays", "variants", "maps", "lexer_proof"}
}

// TestKittenFixturesThroughKittenBackend compiles every fixture with the Kitten
// backend, compares the unit with the reference generator's, and compiles and runs
// the ones it can generate.
func TestKittenFixturesThroughKittenBackend(t *testing.T) {
	requireToolchain(t)

	executable := buildKittenUnit(t)
	probeDirectory := t.TempDir()

	compiler, err := discoverCompiler()
	if err != nil {
		t.Fatalf("discovering the C compiler: %v", err)
	}
	compileFlags, linkFlags := gcBuildFlags()

	generatedFixtures := 0
	blockedFixtures := 0
	for _, name := range fixtureNames() {
		sourcePath := filepath.Join("..", "..", "tests", name+".ktn")
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading %s: %v", sourcePath, err)
		}
		expected, err := os.ReadFile(filepath.Join("..", "..", "tests", name+".expected"))
		if err != nil {
			t.Fatalf("reading the expected output for %s: %v", name, err)
		}

		got, err := runKittenUnit(executable, probeDirectory, string(source), "")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}

		// A fixture the Kitten backend does not generate must be blocked on exactly
		// one thing: the assignment expression, which is the only expression kind it
		// still reports as unimplemented. Any other failure is a real disagreement.
		if strings.HasPrefix(got, "error ") {
			if !strings.Contains(got, "assign expressions are not implemented yet") {
				t.Errorf("%s: the fixture failed for a reason other than the unimplemented assign kind:\n%s", name, got)
				continue
			}
			blockedFixtures++
			continue
		}

		modules, err := referenceModules(string(source), "")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		checked, checkError := checker.CheckModules(modules)
		if checkError != nil {
			t.Errorf("%s: the reference checker rejected the fixture: %v", name, checkError)
			continue
		}
		referenceText, generationError := codegen.Generate(modules, checked)
		if generationError != nil {
			t.Errorf("%s: the reference generator rejected the fixture: %v", name, generationError)
			continue
		}
		if difference := unitDifference(got, referenceText); difference != "" {
			t.Errorf("%s: the Kitten unit differs from the reference: %s", name, difference)
			continue
		}

		// The two units are identical, so compiling and running the Kitten text is a
		// test of the Kitten backend's own output. Compiling the reference text as
		// well would compare a program with itself.
		programDirectory := t.TempDir()
		result, err := buildAndRunProgram(t, compiler, compileFlags, linkFlags, programDirectory, got, 0)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if result.status != 0 || result.stderr != "" {
			t.Errorf("%s: the program exited %d and wrote %q to stderr", name, result.status, result.stderr)
			continue
		}
		if result.stdout != string(expected) {
			t.Errorf("%s: the program printed\n%q\nwant\n%q", name, result.stdout, string(expected))
			continue
		}
		generatedFixtures++
	}

	// This phase's measured outcome: four of the nine fixtures need only the
	// expression kinds it adds. The assertion pins that outcome, and the loop above
	// proves each of the other five is blocked only on assign.
	if generatedFixtures != 4 {
		t.Fatalf("%d of %d fixtures were generated and run, want 4", generatedFixtures, len(fixtureNames()))
	}
	if generatedFixtures+blockedFixtures != len(fixtureNames()) {
		t.Fatalf("%d generated and %d blocked for %d fixtures", generatedFixtures, blockedFixtures, len(fixtureNames()))
	}
	t.Logf("fixtures: %d of %d compile, run, and match their expected output through the Kitten backend under %s; %d report assign as the only unimplemented kind",
		generatedFixtures, len(fixtureNames()), filepath.Base(compiler[0]), blockedFixtures)
}
