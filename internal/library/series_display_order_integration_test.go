package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type seriesDisplayOrderFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	store      *Store
	actor      identity.Principal
	viewer     string
	collection Library
	series     nextUpSeriesFixture
	metadata   ItemMetadataDetail
}

func newSeriesDisplayOrderFixture(t *testing.T) seriesDisplayOrderFixture {
	t.Helper()
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, store, root, "series-order-storage")
	series := nextUpSeries(t, ctx, pool, collection, "Storage", []int{1}, 2)
	actor := metadataEditTestActor(t, ctx, pool, "series-order-storage-editor")
	metadata := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	metadata = metadataEditTestUpdate(t, ctx, store, actor, metadata, map[string]json.RawMessage{
		"ProviderIds": metadataEditTestRaw(t, map[string]string{"Tmdb": "321"}),
	}, nil)
	date := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	userDataSeed(t, ctx, pool, viewer, UserData{ItemID: series.Episodes[1][0], PlaybackPositionTicks: 15000000,
		PlayCount: 2, IsFavorite: true, LastPlayedDate: &date})
	return seriesDisplayOrderFixture{ctx: ctx, pool: pool, store: store, actor: actor, viewer: viewer,
		collection: collection, series: series, metadata: metadata}
}

// Read-only previews and rejected writes must leave all related source,
// projection, identity, roster, and personal-state rows byte-for-byte unchanged.
func (fixture seriesDisplayOrderFixture) snapshot(t *testing.T) string {
	t.Helper()
	var result string
	err := fixture.pool.QueryRow(fixture.ctx, `SELECT jsonb_build_object(
		'items', COALESCE((SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM items i WHERE i.library_id=$1),'[]'::jsonb),
		'metadata', COALESCE((SELECT jsonb_agg(to_jsonb(ms) ORDER BY ms.item_id) FROM item_metadata_state ms JOIN items i ON i.id=ms.item_id WHERE i.library_id=$1),'[]'::jsonb),
		'userdata', COALESCE((SELECT jsonb_agg(to_jsonb(data) ORDER BY data.user_id,data.item_id) FROM user_item_data data JOIN items i ON i.id=data.item_id WHERE i.library_id=$1),'[]'::jsonb),
		'order', (SELECT to_jsonb(saved) FROM series_display_orders saved WHERE saved.series_id=$2),
		'entries', COALESCE((SELECT jsonb_agg(to_jsonb(entry) ORDER BY entry.ordinal) FROM series_display_order_entries entry WHERE entry.series_id=$2),'[]'::jsonb),
		'roster', (SELECT to_jsonb(roster) FROM series_episode_rosters roster WHERE roster.series_id=$2),
		'imports', COALESCE((SELECT jsonb_agg(to_jsonb(imported) ORDER BY imported.revision) FROM episode_roster_imports imported WHERE imported.series_id=$2),'[]'::jsonb),
		'expected', COALESCE((SELECT jsonb_agg(to_jsonb(expected) ORDER BY expected.id) FROM expected_episodes expected WHERE expected.series_id=$2),'[]'::jsonb)
	)::text`, fixture.collection.ID, fixture.series.ID).Scan(&result)
	if err != nil {
		t.Fatalf("snapshot series-order state: %v", err)
	}
	return result
}

func (fixture seriesDisplayOrderFixture) assertSnapshot(t *testing.T, before string) {
	t.Helper()
	if after := fixture.snapshot(t); after != before {
		t.Fatal("a read or rejected ordering mutation changed catalog, metadata, mapping, roster, or user state")
	}
}

func TestSeriesDisplayOrderRevisionIsIndependentAndStaleWritesAreAtomic(t *testing.T) {
	fixture := newSeriesDisplayOrderFixture(t)
	initial, err := fixture.store.GetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID)
	if err != nil || initial.Revision != "0" || initial.DisplayOrder != "Aired" || !initial.SourceMatches || initial.Entries == nil || len(initial.Entries) != 0 {
		t.Fatalf("initial order = %+v, %v", initial, err)
	}
	metadataBefore := metadataEditTestSnapshot(t, fixture.ctx, fixture.pool, fixture.series.ID)
	userBefore, err := fixture.store.GetUserData(fixture.ctx, fixture.viewer, fixture.series.Episodes[1][0])
	if err != nil {
		t.Fatal(err)
	}
	mapping := seriesOrderConsumerMapping("Dvd", [2]int{1, 2}, [2]int{1, 1})
	saved, err := fixture.store.SetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, initial.Revision, "Dvd", mapping)
	if err != nil || saved.Revision != "1" || !saved.SourceMatches || saved.MappingSHA256 == "" || len(saved.Entries) != 2 || saved.Coverage.UnmappedCount != 0 {
		t.Fatalf("save explicit order = %+v, %v", saved, err)
	}
	if after := metadataEditTestSnapshot(t, fixture.ctx, fixture.pool, fixture.series.ID); after != metadataBefore {
		t.Fatal("saving display order changed the independently revisioned metadata record")
	}
	metadata := metadataEditTestDetail(t, fixture.ctx, fixture.store, fixture.actor, fixture.series.ID)
	if metadata.Revision != fixture.metadata.Revision {
		t.Fatal("display-order revision advanced the metadata revision")
	}
	beforeRejected := fixture.snapshot(t)
	for _, stale := range []string{"0", "2"} {
		if _, err := fixture.store.SetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, stale, "Aired", nil); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("stale or future revision %q was accepted: %v", stale, err)
		}
		fixture.assertSnapshot(t, beforeRejected)
	}
	for _, malformed := range []string{"01", "-1", "9223372036854775808"} {
		if _, err := fixture.store.SetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, malformed, "Aired", nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("noncanonical revision %q was accepted: %v", malformed, err)
		}
		fixture.assertSnapshot(t, beforeRejected)
	}
	// An unrelated metadata edit remains allowed under its unchanged revision.
	metadata = metadataEditTestUpdate(t, fixture.ctx, fixture.store, fixture.actor, metadata, map[string]json.RawMessage{
		"ProviderIds": metadataEditTestRaw(t, map[string]string{"Tmdb": "321"}),
		"Overview":    metadataEditTestRaw(t, "Independent descriptive edit"),
	}, nil)
	current, err := fixture.store.GetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID)
	if err != nil || current.Revision != saved.Revision || current.MappingSHA256 != saved.MappingSHA256 {
		t.Fatalf("metadata edit invalidated an unrelated order revision: %+v, %v", current, err)
	}
	userAfter, err := fixture.store.GetUserData(fixture.ctx, fixture.viewer, fixture.series.Episodes[1][0])
	if err != nil || !reflect.DeepEqual(userAfter, userBefore) {
		t.Fatalf("ordering changed personal playback or favorite state: %+v, %v", userAfter, err)
	}
}

func TestSeriesDisplayOrderRequiresPhysicalAndExpectedCoverageWithoutPartialWrites(t *testing.T) {
	for _, expected := range []bool{false, true} {
		name := "physical episode"
		if expected {
			name = "expected episode"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newSeriesDisplayOrderFixture(t)
			mapping := seriesOrderConsumerMapping("Dvd", [2]int{1, 1})
			if expected {
				if _, err := fixture.store.ReplaceEpisodeRoster(fixture.ctx, fixture.actor, fixture.series.ID, EpisodeRosterEdit{
					Revision: "0", Source: EpisodeRosterSourceInput{Key: "declared-expectations", Label: "Explicit source", Revision: "v1"},
					Entries: []EpisodeRosterEntryInput{{Key: "missing-third", SeasonNumber: 1, EpisodeNumber: 3, Name: "Declared third episode"}},
				}); err != nil {
					t.Fatal(err)
				}
				mapping = seriesOrderConsumerMapping("Dvd", [2]int{1, 2}, [2]int{1, 1})
			}
			before := fixture.snapshot(t)
			preview, err := fixture.store.PreviewSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, *mapping)
			if err != nil || preview.Revision != "0" || preview.MappingSHA256 == "" || preview.Coverage.UnmappedCount != 1 ||
				len(preview.Coverage.Unmapped) != 1 || preview.Coverage.Unmapped[0].Kind != map[bool]string{false: "physical", true: "expected"}[expected] {
				t.Fatalf("incomplete coverage preview = %+v, %v", preview, err)
			}
			fixture.assertSnapshot(t, before)
			_, err = fixture.store.SetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, "0", "Dvd", mapping)
			var coverageError *SeriesOrderMappingError
			if !errors.Is(err, ErrSeriesOrderMappingIncomplete) || !errors.As(err, &coverageError) || coverageError.Coverage.UnmappedCount != 1 {
				t.Fatalf("incomplete order was not rejected with coverage facts: %v", err)
			}
			fixture.assertSnapshot(t, before)
		})
	}
}

func TestSeriesDisplayOrderPreviewReturnsStableFactsWithoutPersistingThem(t *testing.T) {
	fixture := newSeriesDisplayOrderFixture(t)
	mapping := seriesOrderConsumerMapping("Absolute", [2]int{1, 2}, [2]int{1, 1})
	before := fixture.snapshot(t)
	wantDigest, err := SeriesOrderMappingSHA256(*mapping)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		preview, err := fixture.store.PreviewSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, *mapping)
		if err != nil || preview.Revision != "0" || preview.DisplayOrder != "Absolute" || preview.GroupID != mapping.ID ||
			preview.MappingSHA256 != wantDigest || preview.Coverage.PhysicalEpisodeCount != 2 || preview.Coverage.UnmappedCount != 0 ||
			!reflect.DeepEqual(preview.Entries, mapping.Entries) {
			t.Fatalf("read-only order preview = %+v, %v", preview, err)
		}
		fixture.assertSnapshot(t, before)
	}
	foreign := *mapping
	foreign.SeriesID = "654"
	if _, err := fixture.store.PreviewSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, foreign); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("a foreign show's group was previewed as this series: %v", err)
	}
	if _, err := fixture.store.SetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, "0", "Absolute", &foreign); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("a foreign show's group was saved as this series: %v", err)
	}
	fixture.assertSnapshot(t, before)
}

func TestSeriesDisplayOrderRequiresCurrentNativeAdministratorAuthority(t *testing.T) {
	fixture := newSeriesDisplayOrderFixture(t)
	mapping := seriesOrderConsumerMapping("Dvd", [2]int{1, 2}, [2]int{1, 1})
	for _, test := range []struct {
		name   string
		change func(identity.Principal) identity.Principal
	}{
		{"missing actor", func(identity.Principal) identity.Principal { return identity.Principal{} }},
		{"client token", func(actor identity.Principal) identity.Principal { actor.Kind = "emby"; return actor }},
		{"different account", func(actor identity.Principal) identity.Principal { actor.User.ID = fixture.viewer; return actor }},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor := test.change(fixture.actor)
			before := fixture.snapshot(t)
			if _, err := fixture.store.GetSeriesDisplayOrder(fixture.ctx, actor, fixture.series.ID); !errors.Is(err, ErrForbidden) {
				t.Fatalf("unauthorized order read was accepted: %v", err)
			}
			if _, err := fixture.store.PreviewSeriesDisplayOrder(fixture.ctx, actor, fixture.series.ID, *mapping); !errors.Is(err, ErrForbidden) {
				t.Fatalf("unauthorized order preview was accepted: %v", err)
			}
			if _, err := fixture.store.SetSeriesDisplayOrder(fixture.ctx, actor, fixture.series.ID, "0", "Dvd", mapping); !errors.Is(err, ErrForbidden) {
				t.Fatalf("unauthorized order write was accepted: %v", err)
			}
			fixture.assertSnapshot(t, before)
		})
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, fixture.actor.SessionID); err != nil {
		t.Fatal(err)
	}
	before := fixture.snapshot(t)
	if _, err := fixture.store.GetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a revoked stored credential retained read access: %v", err)
	}
	if _, err := fixture.store.SetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, "0", "Dvd", mapping); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a revoked stored credential retained write access: %v", err)
	}
	fixture.assertSnapshot(t, before)
}

func TestSeriesDisplayOrderDetectsChangedProviderAndCanRestoreAiredWithoutIt(t *testing.T) {
	fixture := newSeriesDisplayOrderFixture(t)
	mapping := seriesOrderConsumerMapping("Dvd", [2]int{1, 2}, [2]int{1, 1})
	saved, err := fixture.store.SetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, "0", "Dvd", mapping)
	if err != nil {
		t.Fatal(err)
	}
	fixture.metadata = metadataEditTestUpdate(t, fixture.ctx, fixture.store, fixture.actor, fixture.metadata, map[string]json.RawMessage{
		"ProviderIds": metadataEditTestRaw(t, map[string]string{"Tmdb": "654"}),
	}, nil)
	beforeRead := fixture.snapshot(t)
	changed, err := fixture.store.GetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID)
	if err != nil || changed.SourceMatches || changed.Revision != saved.Revision || changed.DisplayOrder != "Dvd" ||
		changed.ProviderSeriesID != "654" || changed.Coverage.UnmappedCount != 2 || len(changed.Entries) != len(mapping.Entries) {
		t.Fatalf("old mapping was treated as current provider evidence: %+v, %v", changed, err)
	}
	fixture.assertSnapshot(t, beforeRead)
	fixture.metadata = metadataEditTestUpdate(t, fixture.ctx, fixture.store, fixture.actor, fixture.metadata, nil, nil)
	metadataBefore := metadataEditTestSnapshot(t, fixture.ctx, fixture.pool, fixture.series.ID)
	userBefore, err := fixture.store.GetUserData(fixture.ctx, fixture.viewer, fixture.series.Episodes[1][0])
	if err != nil {
		t.Fatal(err)
	}
	restored, err := fixture.store.SetSeriesDisplayOrder(fixture.ctx, fixture.actor, fixture.series.ID, saved.Revision, "Aired", nil)
	if err != nil || restored.Revision != "2" || restored.DisplayOrder != "Aired" || !restored.SourceMatches ||
		restored.ProviderSeriesID != "" || restored.GroupID != "" || restored.MappingSHA256 != "" || restored.FetchedAt != nil || len(restored.Entries) != 0 {
		t.Fatalf("aired restoration required unavailable provider evidence or retained its mapping: %+v, %v", restored, err)
	}
	var mappingCount, sourceCount int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT (SELECT count(*) FROM series_display_order_entries WHERE series_id=$1),
		(SELECT count(*) FROM series_display_orders WHERE series_id=$1 AND provider_series_id='' AND provider_group_id='' AND entry_count=0 AND mapping_sha256='' AND fetched_at IS NULL)`, fixture.series.ID).
		Scan(&mappingCount, &sourceCount); err != nil || mappingCount != 0 || sourceCount != 1 {
		t.Fatalf("aired restoration did not clear persisted mapping facts: entries=%d sources=%d error=%v", mappingCount, sourceCount, err)
	}
	if after := metadataEditTestSnapshot(t, fixture.ctx, fixture.pool, fixture.series.ID); after != metadataBefore {
		t.Fatal("restoring aired order changed metadata identity or its revision")
	}
	userAfter, err := fixture.store.GetUserData(fixture.ctx, fixture.viewer, fixture.series.Episodes[1][0])
	if err != nil || !reflect.DeepEqual(userAfter, userBefore) {
		t.Fatalf("aired restoration changed user state: %+v, %v", userAfter, err)
	}
}
