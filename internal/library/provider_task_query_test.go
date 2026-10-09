package library

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/providers"
)

func TestResolvedProviderTaskQueryUsesSnapshotWithoutDatabase(t *testing.T) {
	year, index, parentIndex := 2026, 12, 2
	zero := 0
	cases := []struct {
		name      string
		effective MetadataValues
		want      providers.Query
	}{
		{
			name: "populated fields",
			effective: MetadataValues{
				Name:              "  Snapshot title  ",
				ProductionYear:    &year,
				IndexNumber:       &index,
				ParentIndexNumber: &parentIndex,
				ProviderIDs:       map[string]string{"Tmdb": "42", "imdb": "tt123", "Custom": " value "},
			},
			want: providers.Query{
				Name:        "  Snapshot title  ",
				Year:        2026,
				Season:      2,
				Episode:     12,
				ProviderIDs: map[string]string{"Tmdb": "42", "imdb": "tt123", "Custom": " value "},
			},
		},
		{
			name: "absent fields",
		},
		{
			name: "zero fields and empty provider IDs",
			effective: MetadataValues{
				ProductionYear:    &zero,
				IndexNumber:       &zero,
				ParentIndexNumber: &zero,
				ProviderIDs:       map[string]string{},
			},
			want: providers.Query{ProviderIDs: map[string]string{}},
		},
	}
	for _, itemType := range []string{"Movie", "Video", "Series", "MusicAlbum", "MusicArtist", "Audio"} {
		for _, test := range cases {
			t.Run(itemType+"/"+test.name, func(t *testing.T) {
				store := &Store{}
				detail := ItemMetadataDetail{
					ItemID:     "snapshot-item",
					Type:       itemType,
					Name:       "Stored item title",
					ParentName: "Parent title",
					Automatic:  MetadataValues{Name: "Automatic title", ProviderIDs: map[string]string{"Tmdb": "99"}},
					Effective:  test.effective,
				}
				query, err := store.resolvedProviderTaskQuery(context.Background(), detail)
				if err != nil {
					t.Fatal(err)
				}
				want := test.want
				want.Type = itemType
				if !reflect.DeepEqual(query, want) {
					t.Fatalf("snapshot query = %#v, want %#v", query, want)
				}
				if !reflect.DeepEqual(detail.Effective.ProviderIDs, test.want.ProviderIDs) {
					t.Fatal("query construction changed the snapshot provider IDs")
				}
			})
		}
	}
}

func TestResolvedProviderTaskQueryHonorsCancellationWithoutDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, itemType := range []string{"Movie", "Video", "Series", "MusicAlbum", "MusicArtist", "Audio"} {
		t.Run(itemType, func(t *testing.T) {
			store := &Store{}
			detail := ItemMetadataDetail{Type: itemType, Effective: MetadataValues{Name: "Cancelled snapshot"}}
			query, err := store.resolvedProviderTaskQuery(ctx, detail)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled snapshot query returned %v", err)
			}
			if !reflect.DeepEqual(query, providers.Query{}) {
				t.Fatalf("cancelled snapshot query returned provider facts: %#v", query)
			}
		})
	}
}
