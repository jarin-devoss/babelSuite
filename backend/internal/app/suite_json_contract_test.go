package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/suites"
)

// A definition built from an OCI package leaves the slices it has no data for
// nil, and Go marshals a nil slice as null. Every field below is declared as a
// plain array in the JSON contract, so a client that trusts the declared type
// calls an array method on null -- which is how the suite detail page crashed
// on operations[].exchanges for all fourteen example suites.
func cachedReader(def suites.Definition) *catalogSuiteReader {
	return &catalogSuiteReader{cache: map[string]*suites.Definition{def.ID: &def}}
}

func suiteWithNilSlices() suites.Definition {
	return suites.Definition{
		ID:    "nil-slices",
		Title: "Nil Slices",
		APISurfaces: []suites.APISurface{{
			ID:         "surface",
			Operations: []suites.APIOperation{{ID: "op"}}, // Exchanges left nil
		}},
		// Tags, Modules, Contracts, Profiles, Folders, SourceFiles all nil.
	}
}

func TestGetServesArraysNotNullForDeclaredSliceFields(t *testing.T) {
	reader := cachedReader(suiteWithNilSlices())

	got, err := reader.Get("nil-slices")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, field := range []string{"tags", "modules", "contracts", "profiles", "folders", "sourceFiles", "apiSurfaces", "exchanges"} {
		if strings.Contains(string(encoded), `"`+field+`":null`) {
			t.Errorf("%q serialised as null, want []: %s", field, encoded)
		}
	}
}

func TestListServesArraysNotNullForDeclaredSliceFields(t *testing.T) {
	reader := cachedReader(suiteWithNilSlices())

	listed := reader.List()
	if len(listed) != 1 {
		t.Fatalf("List returned %d suites, want 1", len(listed))
	}

	encoded, err := json.Marshal(listed[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, field := range []string{"tags", "modules", "contracts", "profiles", "folders", "sourceFiles", "apiSurfaces", "exchanges"} {
		if strings.Contains(string(encoded), `"`+field+`":null`) {
			t.Errorf("%q serialised as null, want []: %s", field, encoded)
		}
	}
}

// The reader used to hand out a shallow copy of its cached definition, so a
// caller mutating a slice element wrote straight into the cache.
func TestGetDoesNotShareBackingArraysWithCache(t *testing.T) {
	def := suiteWithNilSlices()
	def.Tags = []string{"v1"}
	reader := cachedReader(def)

	first, err := reader.Get("nil-slices")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	first.Tags[0] = "mutated"

	second, err := reader.Get("nil-slices")
	if err != nil {
		t.Fatalf("Get again: %v", err)
	}
	if second.Tags[0] != "v1" {
		t.Errorf("cached definition was mutated through a returned clone: got %q, want %q", second.Tags[0], "v1")
	}
}
