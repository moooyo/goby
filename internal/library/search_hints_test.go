package library

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSearchHintsRejectInvalidInputBeforeDatabaseAccess(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		subject Subject
		query   SearchHintsQuery
	}{
		{"missing authority", Subject{}, SearchHintsQuery{SearchTerm: "Film", Limit: 20}},
		{"negative page", Subject{UserID: "viewer"}, SearchHintsQuery{SearchTerm: "Film", StartIndex: -1}},
		{"excessive page", Subject{UserID: "viewer"}, SearchHintsQuery{SearchTerm: "Film", StartIndex: 1 << 31}},
		{"negative limit", Subject{UserID: "viewer"}, SearchHintsQuery{SearchTerm: "Film", Limit: -1}},
		{"excessive limit", Subject{UserID: "viewer"}, SearchHintsQuery{SearchTerm: "Film", Limit: 1001}},
		{"null term", Subject{UserID: "viewer"}, SearchHintsQuery{SearchTerm: "Film\x00"}},
		{"invalid UTF-8", Subject{UserID: "viewer"}, SearchHintsQuery{SearchTerm: "Film\xff"}},
		{"long term", Subject{UserID: "viewer"}, SearchHintsQuery{SearchTerm: strings.Repeat("a", 1025)}},
		{"unknown include", Subject{UserID: "viewer"}, SearchHintsQuery{IncludeItemTypes: []string{"LiveTvProgram"}}},
		{"unknown exclude", Subject{UserID: "viewer"}, SearchHintsQuery{ExcludeItemTypes: []string{"Tag"}}},
		{"unknown media", Subject{UserID: "viewer"}, SearchHintsQuery{MediaTypes: []string{"Unknown"}}},
		{"excessive types", Subject{UserID: "viewer"}, SearchHintsQuery{IncludeItemTypes: make([]string, 33)}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := (&Store{}).SearchHints(context.Background(), fixture.subject, fixture.query); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid search returned %v", err)
			}
		})
	}
}

func TestSearchHintsEmptyTextStillRequiresAuthorityAndExplicitZeroIsRetained(t *testing.T) {
	query, err := normalizeSearchHintsQuery(Subject{UserID: "viewer"}, SearchHintsQuery{
		SearchTerm: " \t\u7535\u5f71 \n", IncludeItemTypes: []string{"movie", "Movie", " musicartist "},
		ExcludeItemTypes: []string{"Person"}, MediaTypes: []string{"audio", "Audio"},
	})
	if err != nil || query.SearchTerm != "\u7535\u5f71" || query.Limit != 0 ||
		!reflect.DeepEqual(query.IncludeItemTypes, []string{"Movie", "MusicArtist"}) ||
		!reflect.DeepEqual(query.MediaTypes, []string{"Audio"}) {
		t.Fatalf("normalization lost explicit search controls: %+v, %v", query, err)
	}
	if _, err := (&Store{}).SearchHints(context.Background(), Subject{UserID: "viewer"}, SearchHintsQuery{SearchTerm: " \n "}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty search bypassed the authority read: %v", err)
	}
}
