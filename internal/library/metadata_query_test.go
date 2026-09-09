package library

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeMetadataItemQuery(t *testing.T) {
	input := MetadataItemQuery{
		SearchTerm: " 100%_Movie ",
		Types:      []string{"movie", " EPISODE ", "Movie", "FoLdEr"},
		StartIndex: math.MaxInt32,
		Limit:      200,
	}
	normalized, err := normalizeMetadataItemQuery("library", input)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Movie", "Episode", "Folder"}; !reflect.DeepEqual(normalized.Types, want) {
		t.Fatalf("types = %v, want %v", normalized.Types, want)
	}
	if normalized.SearchTerm != input.SearchTerm || normalized.StartIndex != input.StartIndex || normalized.Limit != input.Limit {
		t.Fatalf("literal search or pagination changed: %#v", normalized)
	}
	if input.Types[0] != "movie" || len(input.Types) != 4 {
		t.Fatalf("normalization mutated caller types: %v", input.Types)
	}

	normalized, err = normalizeMetadataItemQuery(strings.Repeat("i", 256), MetadataItemQuery{
		SearchTerm: strings.Repeat("\u754c", 341) + "x",
		Limit:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Movie", "Series", "Season", "Episode", "Audio", "Video", "MusicAlbum", "MusicArtist", "Folder"}
	if !reflect.DeepEqual(normalized.Types, want) {
		t.Fatalf("default types = %v, want %v", normalized.Types, want)
	}
}

func TestNormalizeMetadataItemQueryRejectsInvalidInput(t *testing.T) {
	overflowStartIndex := int64(math.MaxInt32) + 1
	tests := []struct {
		name      string
		libraryID string
		query     MetadataItemQuery
		field     string
	}{
		{name: "empty library", query: MetadataItemQuery{Limit: 1}, field: "LibraryId"},
		{name: "blank library", libraryID: " \t", query: MetadataItemQuery{Limit: 1}, field: "LibraryId"},
		{name: "long library", libraryID: strings.Repeat("i", 257), query: MetadataItemQuery{Limit: 1}, field: "LibraryId"},
		{name: "invalid library UTF-8", libraryID: "\xff", query: MetadataItemQuery{Limit: 1}, field: "LibraryId"},
		{name: "library NUL", libraryID: "i\x00d", query: MetadataItemQuery{Limit: 1}, field: "LibraryId"},
		{name: "search bytes", libraryID: "library", query: MetadataItemQuery{Limit: 1, SearchTerm: strings.Repeat("\u754c", 342)}, field: "SearchTerm"},
		{name: "invalid search UTF-8", libraryID: "library", query: MetadataItemQuery{Limit: 1, SearchTerm: "\xff"}, field: "SearchTerm"},
		{name: "search NUL", libraryID: "library", query: MetadataItemQuery{Limit: 1, SearchTerm: "a\x00b"}, field: "SearchTerm"},
		{name: "negative start", libraryID: "library", query: MetadataItemQuery{Limit: 1, StartIndex: -1}, field: "StartIndex"},
		{name: "overflow start", libraryID: "library", query: MetadataItemQuery{Limit: 1, StartIndex: int(overflowStartIndex)}, field: "StartIndex"},
		{name: "missing limit", libraryID: "library", query: MetadataItemQuery{}, field: "Limit"},
		{name: "negative limit", libraryID: "library", query: MetadataItemQuery{Limit: -1}, field: "Limit"},
		{name: "large limit", libraryID: "library", query: MetadataItemQuery{Limit: 201}, field: "Limit"},
		{name: "root type", libraryID: "library", query: MetadataItemQuery{Limit: 1, Types: []string{"CollectionFolder"}}, field: "Types"},
		{name: "unknown type", libraryID: "library", query: MetadataItemQuery{Limit: 1, Types: []string{"Movie", "Unknown"}}, field: "Types"},
		{name: "empty type", libraryID: "library", query: MetadataItemQuery{Limit: 1, Types: []string{""}}, field: "Types"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeMetadataItemQuery(test.libraryID, test.query)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
			var validation *MetadataValidationError
			if !errors.As(err, &validation) || validation.Fields[test.field] == "" {
				t.Fatalf("error = %#v, want validation field %q", err, test.field)
			}
		})
	}
}
