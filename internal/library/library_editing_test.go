package library

import (
	"errors"
	"testing"
)

func TestLibraryEditRevisionRejectsLossyAndNoncanonicalValues(t *testing.T) {
	for _, value := range []string{"", "0", "01", "+1", "-1", "1.0", "1e2", " 1", "1 ", "9223372036854775808", "9007199254740993.0"} {
		if _, err := libraryEditRevision(value); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("accepted revision %q: %v", value, err)
		}
	}
	for _, value := range []string{"1", "9007199254740993", "9223372036854775807"} {
		if _, err := libraryEditRevision(value); err != nil {
			t.Errorf("rejected exact revision %q: %v", value, err)
		}
	}
}

func TestLibraryImportOptionsPreserveHistoricalDefaults(t *testing.T) {
	if got := EffectiveLibraryOptions(Library{}); got != (LibraryOptions{EnableLocalMetadata: true, EnableLocalImages: true}) {
		t.Fatalf("zero-value caller lost historical import defaults: %+v", got)
	}
	disabled := false
	options := applyLibraryOptions(DefaultLibraryOptions(), &LibraryOptionsUpdate{EnableLocalImages: &disabled})
	if !options.EnableLocalMetadata || options.EnableLocalImages {
		t.Fatalf("partial update reset another importer: %+v", options)
	}
	previous := localMetadata{hash: "existing-source", path: "Film.nfo"}
	state := scanState{library: Library{Options: &LibraryOptions{EnableLocalImages: true}}}
	retained := state.localNFO([]string{"not-opened.nfo"}, "Movie", previous)
	if retained.hash != previous.hash || retained.path != previous.path {
		t.Fatal("disabled reader cleared accepted source facts")
	}
}
