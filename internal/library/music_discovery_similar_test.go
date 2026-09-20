package library

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQuerySimilarArtistsRejectsInvalidInputsBeforeDatabaseAccess(t *testing.T) {
	for _, seed := range []string{"", "artist", "0", "-1", "+1", "01", " 1", "1 ", "1\n", "1\x00", "9223372036854775808", strings.Repeat("1", 257)} {
		if _, err := (&Store{}).QuerySimilarArtists(context.Background(), seed, SimilarQuery{Query: Query{UserID: "viewer", Limit: 1}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid artist seed %q reached database access: %v", seed, err)
		}
	}
	for _, query := range []SimilarQuery{
		{Query: Query{UserID: "viewer", Limit: -1}},
		{Query: Query{UserID: "viewer", Limit: 1001}},
		{Query: Query{UserID: "viewer", StartIndex: -1}},
		{Query: Query{UserID: "viewer", SortBy: "Random"}, ExplicitSort: true},
		{Query: Query{UserID: "viewer", SortBy: "DateCreated"}, ExplicitSort: true},
		{Query: Query{UserID: "viewer", SortBy: "Name,SortName"}, ExplicitSort: true},
		{Query: Query{UserID: "viewer", SortOrder: "sideways"}, ExplicitSort: true},
		{Query: Query{UserID: "viewer"}, ExcludeArtistIds: []int64{0}},
		{Query: Query{UserID: "viewer"}, ExcludeArtistIds: []int64{-1}},
		{Query: Query{UserID: "viewer"}, ExcludeArtistIds: make([]int64, 1025)},
		{Query: Query{UserID: "viewer", ArtistIds: []int64{-1}}},
		{Query: Query{UserID: "viewer", AlbumIds: []string{" album"}}},
		{Query: Query{Limit: 1}},
	} {
		if _, err := (&Store{}).QuerySimilarArtists(context.Background(), "1", query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid similar artist query reached database access: %v", err)
		}
	}
}
