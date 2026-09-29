package library

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestLibraryIntroPolicyRequiresExplicitTVOptIn(t *testing.T) {
	if DefaultLibraryOptions().EnableIntroDetection || EffectiveLibraryOptions(Library{}).EnableIntroDetection {
		t.Fatal("historical callers must not acquire automatic intro work")
	}
	options := DefaultLibraryOptions()
	options.EnableIntroDetection = true
	if err := validateLibraryOptions("tvshows", options); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"movies", "mixed", "music", "homevideos", "collections"} {
		if err := validateLibraryOptions(kind, options); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("non-TV opt-in accepted: %s: %v", kind, err)
		}
	}
}

func TestLibraryIntroOptionInputRejectsAmbiguityAndPreservesOmission(t *testing.T) {
	var update LibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"EnableIntroDetection":true,"EnableLocalImages":false}`), &update); err != nil {
		t.Fatal(err)
	}
	options := applyLibraryOptions(DefaultLibraryOptions(), &update)
	if !options.EnableIntroDetection || options.EnableLocalImages || !options.EnableLocalMetadata || !options.EnableEmbeddedArtwork {
		t.Fatalf("independent options changed: %+v", options)
	}
	if err := json.Unmarshal([]byte(`{"EnableLocalMetadata":false}`), &update); err != nil || update.EnableIntroDetection != nil {
		t.Fatalf("omitted intro policy must remain unchanged: %+v %v", update, err)
	}
	if next := applyLibraryOptions(options, &update); !next.EnableIntroDetection || next.EnableLocalMetadata {
		t.Fatalf("another importer edit reset the intro policy: %+v", next)
	}
	for _, raw := range []string{
		`null`, `[]`, `{"EnableIntroDetection":null}`, `{"EnableIntroDetection":"true"}`,
		`{"EnableIntroDetection":1}`, `{"EnableIntroDetection":true,"EnableIntroDetection":false}`,
		`{"EnableIntroDetection":true,"enableintrodetection":false}`, `{"UnknownOption":true}`,
	} {
		if err := json.Unmarshal([]byte(raw), &update); err == nil {
			t.Errorf("ambiguous or unsupported option accepted: %s", raw)
		}
	}
}
