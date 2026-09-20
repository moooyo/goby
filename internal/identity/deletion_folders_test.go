package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestDeletionFolderQueryBoundsAndDefaults(t *testing.T) {
	query, err := normalizeDeletionFolderQuery(DeletionFolderQuery{SearchTerm: "  Example  "})
	if err != nil || query.Limit != 100 || query.SearchTerm != "Example" {
		t.Fatalf("default folder query: %+v %v", query, err)
	}
	for _, query := range []DeletionFolderQuery{
		{StartIndex: -1}, {StartIndex: 2147483648}, {Limit: 201}, {Limit: -1}, {SearchTerm: strings.Repeat("x", 257)},
		{SearchTerm: "unsafe\x00"}, {SearchTerm: string([]byte{255})}, {LibraryID: " folder"},
	} {
		if _, err := normalizeDeletionFolderQuery(query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid folder query accepted: %v", err)
		}
	}
}

func TestSelectedUnratedCategoriesPreserveOnlyExistingInactiveValues(t *testing.T) {
	if err := validateSelectedUnratedCategories([]string{"Movie", "Series", "Music", "Trailer", "Other"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := validateSelectedUnratedCategories([]string{"Movie", "Game", "LiveTvChannel"}, []string{"Game", "LiveTvChannel", "Book"}); err != nil {
		t.Fatal(err)
	}
	if err := validateSelectedUnratedCategories([]string{"Movie"}, []string{"Game"}); err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{"Game", "Book", "LiveTvChannel", "LiveTvProgram", "ChannelContent"} {
		if err := validateSelectedUnratedCategories([]string{category}, nil); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("new inactive category %s accepted", category)
		}
	}
	// Reading historical SDK values stays valid; write admission is a separate
	// rule and cannot turn a restored account into an unreadable policy object.
	if _, err := ParseRuntimePolicy([]byte(`{"BlockUnratedItems":["Game","Book","LiveTvChannel","LiveTvProgram","ChannelContent"]}`)); err != nil {
		t.Fatal(err)
	}
}
