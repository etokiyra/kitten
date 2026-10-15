package runtime

import (
	"strings"
	"testing"
)

func TestEmbeddedSourcesArePresent(t *testing.T) {
	if strings.TrimSpace(HeaderSource()) == "" {
		t.Errorf("HeaderSource is empty")
	}
	if strings.TrimSpace(Source()) == "" {
		t.Errorf("Source is empty")
	}
}

func TestHeaderDeclaresRuntimeABI(t *testing.T) {
	header := HeaderSource()
	for _, symbol := range []string{
		InitFunction,
		AllocFunction,
		FailFunction,
		StringEqualFunction,
		StringConcatFunction,
		StringAppendFunction,
		ArrayNewFunction,
		ArrayAtFunction,
		ArrayLengthFunction,
		MapNewFunction,
		MapSetFunction,
		MapAtFunction,
		MapHasFunction,
		MapLengthFunction,
		PrintIntFunction,
		PrintFloatFunction,
		PrintBoolFunction,
		PrintStringFunction,
		PrintSpaceFunction,
		PrintNewlineFunction,
	} {
		if !strings.Contains(header, symbol) {
			t.Errorf("header does not declare %q", symbol)
		}
	}
	if !strings.Contains(header, "GC_MALLOC") && !strings.Contains(header, "gc.h") {
		t.Errorf("header does not reference the Boehm GC header")
	}
}

func TestSourceImplementsGarbageCollection(t *testing.T) {
	source := Source()
	for _, fragment := range []string{"GC_INIT", "GC_MALLOC", "strcmp", "memcpy", "ktn_rt_array_new", "ktn_rt_array_at", "snprintf"} {
		if !strings.Contains(source, fragment) {
			t.Errorf("runtime source does not contain %q", fragment)
		}
	}
}

func TestFileNames(t *testing.T) {
	if HeaderFileName != "kitten_runtime.h" {
		t.Errorf("HeaderFileName = %q", HeaderFileName)
	}
	if SourceFileName != "kitten_runtime.c" {
		t.Errorf("SourceFileName = %q", SourceFileName)
	}
}
