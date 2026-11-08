package library

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/providers"
)

func seriesOrderConsumerMapping(order string, pairs ...[2]int) *providers.TVEpisodeGroupDetail {
	mapping := &providers.TVEpisodeGroupDetail{
		TVEpisodeGroup: providers.TVEpisodeGroup{ID: "consumer-order", Name: "Explicit provider order", DisplayOrder: order, EpisodeCount: len(pairs), GroupCount: 1},
		SeriesID:       "321", Entries: make([]providers.TVEpisodeGroupEntry, len(pairs)),
	}
	for index, pair := range pairs {
		mapping.Entries[index] = providers.TVEpisodeGroupEntry{
			ProviderEpisodeID: strconv.Itoa(index + 1), SeasonNumber: pair[0], EpisodeNumber: pair[1],
			GroupOrder: 1, EpisodeOrder: index, Ordinal: index, GroupName: "Explicit group",
		}
	}
	return mapping
}

func TestSeriesDisplayOrderConsumersPreserveCanonicalIdentity(t *testing.T) {
	for _, order := range []string{"Dvd", "Absolute"} {
		t.Run(order, func(t *testing.T) {
			ctx, pool, store, root, viewer := libraryIntegrationStore(t, &libraryFixtureProber{})
			collection := nextUpLibrary(t, ctx, store, root, "series-order-consumers")
			series := nextUpSeries(t, ctx, pool, collection, "Ordered", []int{1, 2}, 2)
			actor := metadataEditTestActor(t, ctx, pool, "series-order-consumer-editor")
			metadata := metadataEditTestDetail(t, ctx, store, actor, series.ID)
			metadataEditTestUpdate(t, ctx, store, actor, metadata, map[string]json.RawMessage{
				"ProviderIds": metadataEditTestRaw(t, map[string]string{"Tmdb": "321"}),
			}, nil)
			mapping := seriesOrderConsumerMapping(order, [2]int{2, 1}, [2]int{1, 2}, [2]int{2, 2}, [2]int{1, 1})
			saved, err := store.SetSeriesDisplayOrder(ctx, actor, series.ID, "0", order, mapping)
			if err != nil {
				t.Fatalf("select explicit series order: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE items SET media=media || jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1)
				WHERE library_id=$2 AND media IS NOT NULL`, media.CurrentProbeVersion, collection.ID); err != nil {
				t.Fatal(err)
			}
			want := []string{series.Episodes[2][0], series.Episodes[1][1], series.Episodes[2][1], series.Episodes[1][0]}
			for _, sortBy := range []string{"", "IndexNumber", "ParentIndexNumber,IndexNumber"} {
				result, err := store.QueryItems(ctx, Query{UserID: viewer, ParentID: series.ID, Recursive: true,
					IncludeItemTypes: []string{"Episode"}, SortBy: sortBy})
				if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), want) {
					t.Fatalf("series browse sort %q = %v, %v; want %v", sortBy, queryItemIDs(result.Items), err, want)
				}
				for index, item := range result.Items {
					pair := mapping.Entries[index]
					if item.IndexNumber != pair.EpisodeNumber || item.ParentIndexNumber != pair.SeasonNumber {
						t.Fatalf("display order changed canonical numbering: %+v", item)
					}
				}
			}
			season, err := store.QueryItems(ctx, Query{UserID: viewer, ParentID: series.Seasons[1], IncludeItemTypes: []string{"Episode"}})
			if err != nil || !reflect.DeepEqual(queryItemIDs(season.Items), []string{want[1], want[3]}) {
				t.Fatalf("season browse lost selected global order: %v, %v", queryItemIDs(season.Items), err)
			}
			nextUpPlayed(t, ctx, pool, viewer, want[0], time.Now().UTC())
			next := nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewer, SeriesID: series.ID, Limit: 1})
			nextUpAssertFirstID(t, next, want[1])
			queue, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID)
			if err != nil || !reflect.DeepEqual(queryItemIDs(queue.Items), want) || queue.Items[0].UserData == nil || !queue.Items[0].UserData.Played {
				t.Fatalf("playback queue lost provider order or replay history: %v, %v", queryItemIDs(queue.Items), err)
			}
			if _, err := store.SetSeriesDisplayOrder(ctx, actor, series.ID, saved.Revision, "Aired", nil); err != nil {
				t.Fatalf("restore aired order: %v", err)
			}
			aired, err := store.QueryItems(ctx, Query{UserID: viewer, ParentID: series.ID, Recursive: true,
				IncludeItemTypes: []string{"Episode"}, SortBy: "ParentIndexNumber,IndexNumber"})
			canonical := append(append([]string{}, series.Episodes[1]...), series.Episodes[2]...)
			if err != nil || !reflect.DeepEqual(queryItemIDs(aired.Items), canonical) {
				t.Fatalf("aired order was not restored: %v, %v", queryItemIDs(aired.Items), err)
			}
			next = nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewer, SeriesID: series.ID, Limit: 1})
			nextUpAssertFirstID(t, next, series.Episodes[2][1])
			if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, viewer); err != nil {
				t.Fatal(err)
			}
			if _, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("order mapping bypassed revoked catalog access: %v", err)
			}
		})
	}
}

func TestSeriesDisplayOrderConsumersKeepUnknownMappingsAfterKnownEntries(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, store, root, "series-order-new-episodes")
	series := nextUpSeries(t, ctx, pool, collection, "Unknown", []int{1}, 2)
	actor := metadataEditTestActor(t, ctx, pool, "series-order-unknown-editor")
	metadata := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	metadata = metadataEditTestUpdate(t, ctx, store, actor, metadata, map[string]json.RawMessage{
		"ProviderIds": metadataEditTestRaw(t, map[string]string{"Tmdb": "321"}),
	}, nil)
	if _, err := store.SetSeriesDisplayOrder(ctx, actor, series.ID, "0", "Dvd",
		seriesOrderConsumerMapping("Dvd", [2]int{1, 2}, [2]int{1, 1})); err != nil {
		t.Fatalf("select provider order before the catalog grows: %v", err)
	}
	// Later scans can add episodes not present in the selected provider snapshot.
	// Their IDs and labels deliberately disagree with canonical number order.
	unknown := []string{"a-unmapped-" + series.ID, "z-unmapped-" + series.ID}
	for index, id := range unknown {
		nextUpInsertItem(t, ctx, pool, collection.ID, id, series.Seasons[1], "New episode", "Episode", false,
			nextUpNumber(4-index), nextUpNumber(1), true)
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET media=media || jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1)
		WHERE library_id=$2 AND media IS NOT NULL`, media.CurrentProbeVersion, collection.ID); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		direction string
		want      []string
	}{
		{"ASC", []string{series.Episodes[1][1], series.Episodes[1][0], unknown[0], unknown[1]}},
		{"DESC", []string{series.Episodes[1][0], series.Episodes[1][1], unknown[1], unknown[0]}},
	} {
		result, err := store.QueryItems(ctx, Query{UserID: viewer, ParentID: series.ID, Recursive: true,
			IncludeItemTypes: []string{"Episode"}, SortBy: "IndexNumber", SortOrder: test.direction})
		if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), test.want) {
			t.Fatalf("%s browse applied an aired fallback to unknown entries: %v, %v", test.direction, queryItemIDs(result.Items), err)
		}
	}
	nextUpPlayed(t, ctx, pool, viewer, series.Episodes[1][1], time.Now().UTC())
	nextUpAssertFirstID(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewer, SeriesID: series.ID, Limit: 1}), series.Episodes[1][0])
	queue, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID)
	want := []string{series.Episodes[1][1], series.Episodes[1][0], unknown[0], unknown[1]}
	if err != nil || !reflect.DeepEqual(queryItemIDs(queue.Items), want) {
		t.Fatalf("queue applied an aired fallback to unknown entries: %v, %v", queryItemIDs(queue.Items), err)
	}
	// Reidentifying a Series cannot attach the old show's provider ranks to it.
	metadataEditTestUpdate(t, ctx, store, actor, metadata, map[string]json.RawMessage{
		"ProviderIds": metadataEditTestRaw(t, map[string]string{"Tmdb": "654"}),
	}, nil)
	allUnknown := append(append([]string{}, series.Episodes[1]...), unknown...)
	sort.Strings(allUnknown)
	queue, err = store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID)
	if err != nil || !reflect.DeepEqual(queryItemIDs(queue.Items), allUnknown) {
		t.Fatalf("queue reused a mapping from the previous provider identity: %v, %v", queryItemIDs(queue.Items), err)
	}
	result, err := store.QueryItems(ctx, Query{UserID: viewer, ParentID: series.ID, Recursive: true,
		IncludeItemTypes: []string{"Episode"}, SortBy: "IndexNumber"})
	if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), allUnknown) {
		t.Fatalf("browse reused a mapping from the previous provider identity: %v, %v", queryItemIDs(result.Items), err)
	}
}
