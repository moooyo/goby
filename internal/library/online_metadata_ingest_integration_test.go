//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/providers"
)

type onlineMetadataIngestFixtureProvider struct {
	configured bool
	searches   []string
	lookups    []string
	onLookup   func(providers.Selection) error
}

func (f *onlineMetadataIngestFixtureProvider) Status() []providers.Status {
	return []providers.Status{{ID: "tmdb", Configured: f.configured}}
}

func (f *onlineMetadataIngestFixtureProvider) Search(_ context.Context, provider string, query providers.Query) ([]providers.Match, error) {
	f.searches = append(f.searches, query.Type+":"+query.Name)
	match := providers.Match{Selection: providers.Selection{Provider: provider, Type: query.Type, ID: "100", Language: "en"}, Name: query.Name, Year: query.Year}
	if query.Name == "Ambiguous" {
		other := match
		other.ID = "200"
		return []providers.Match{match, other}, nil
	}
	return []providers.Match{match}, nil
}

func (f *onlineMetadataIngestFixtureProvider) Lookup(_ context.Context, selection providers.Selection) (providers.Metadata, error) {
	f.lookups = append(f.lookups, selection.Type+":"+selection.ID)
	if f.onLookup != nil {
		if err := f.onLookup(selection); err != nil {
			return providers.Metadata{}, err
		}
	}
	ids, _ := json.Marshal(map[string]string{"Tmdb": selection.ID})
	return providers.Metadata{Selection: selection, Fields: map[string]json.RawMessage{
		"Name": json.RawMessage(`"Provider title"`), "ProviderIds": ids,
	}}, nil
}

func TestOnlineMetadataIngestFillsSeriesBeforeChildrenAndLeavesAmbiguousMoviesUnmatched(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	store.enrichOnlineMetadata = true
	episodePath := libraryIntegrationFile(t, root, "tv/Show/Season 1/Show.S01E02.mp4", "video:episode")
	libraryIntegrationFile(t, root, "tv/Show/Season 1/Show.S01E02.nfo", `<episodedetails><tmdbid>201</tmdbid></episodedetails>`)
	moviePath := libraryIntegrationFile(t, root, "movies/Ambiguous.mp4", "video:movie")
	tv := libraryIntegrationCreate(t, ctx, store, "Online TV", "tvshows", filepath.Join(root, "tv"))
	movies := libraryIntegrationCreate(t, ctx, store, "Online movies", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, tv.ID, "Completed")
	libraryIntegrationScan(t, ctx, store, movies.ID, "Completed")
	episode := nfoCatalogItem(t, ctx, store, userID, tv.ID, episodePath)
	movie := nfoCatalogItem(t, ctx, store, userID, movies.ID, moviePath)
	actor := metadataEditTestActor(t, ctx, pool, "online-ingest-editor")
	metadataEditTestUpdate(t, ctx, store, actor, metadataEditTestDetail(t, ctx, store, actor, episode.ID),
		map[string]json.RawMessage{"Name": json.RawMessage(`"Manual episode"`)}, []string{"Overview"})
	child, fence := metadataIngestTestWorker(t, ctx, pool, tv, TaskOnlineMetadataIngestKey)
	client := &onlineMetadataIngestFixtureProvider{}
	if err := store.EnrichIngestedOnlineMetadata(ctx, client, child, tv.ID, fence, func(ProviderProgress) error { return nil }); !errors.Is(err, providers.ErrNotConfigured) || len(client.searches)+len(client.lookups) != 0 {
		t.Fatalf("missing configuration started provider work: %v", err)
	}
	client.configured = true
	if err := store.EnrichIngestedOnlineMetadata(ctx, client, child, tv.ID, fence, func(ProviderProgress) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(client.searches, []string{"Series:Show"}) || !reflect.DeepEqual(client.lookups, []string{"Series:100", "Season:100:1", "Episode:100:1:2"}) {
		t.Fatalf("incorrect ancestry or matching order: searches=%v lookups=%v", client.searches, client.lookups)
	}
	after := nfoCatalogItem(t, ctx, store, userID, tv.ID, episodePath)
	if after.ID != episode.ID || after.ParentID != episode.ParentID || after.Name != "Manual episode" || after.Series == nil || after.Series.Name != "Provider title" {
		t.Fatal("online metadata publish changed hierarchy or discarded a manual override")
	}
	// Complete the fixture's first run so the same task can be admitted for a
	// different library. Only the test owns these execution rows.
	if _, err := pool.Exec(ctx, `UPDATE task_runs SET state='completed',finished_at=clock_timestamp() WHERE task_key=$1`, TaskOnlineMetadataIngestKey); err != nil {
		t.Fatal(err)
	}
	child, fence = metadataIngestTestWorker(t, ctx, pool, movies, TaskOnlineMetadataIngestKey)
	if err := store.EnrichIngestedOnlineMetadata(ctx, client, child, movies.ID, fence, func(ProviderProgress) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM local_metadata_ingest`).Scan(&pending); err != nil || pending != 0 || len(client.lookups) != 3 ||
		nfoCatalogItem(t, ctx, store, userID, movies.ID, moviePath).Name != movie.Name {
		t.Fatalf("ambiguous match wrote data or left a completed queue entry: %d %v", pending, err)
	}
}

func TestOnlineMetadataIngestKeepsChildrenPendingWhenParentProjectionIsMissing(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	store.enrichOnlineMetadata = true
	path := libraryIntegrationFile(t, root, "tv/Show/Season 1/Show.S01E02.mp4", "video:episode")
	collection := libraryIntegrationCreate(t, ctx, store, "Incomplete TV projection", "tvshows", filepath.Join(root, "tv"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	episode := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	if episode.Series == nil {
		t.Fatal("missing fixture series")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM item_metadata_state WHERE item_id=$1`, episode.Series.ID); err != nil {
		t.Fatal(err)
	}
	child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskOnlineMetadataIngestKey)
	client := &onlineMetadataIngestFixtureProvider{configured: true}
	if err := store.EnrichIngestedOnlineMetadata(ctx, client, child, collection.ID, fence, func(ProviderProgress) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM local_metadata_ingest WHERE enrich_online`).Scan(&pending); err != nil || pending != 3 || len(client.searches)+len(client.lookups) != 0 {
		t.Fatalf("missing parent projection consumed dependent work: pending=%d provider_calls=%d err=%v", pending, len(client.searches)+len(client.lookups), err)
	}
}

func TestOnlineMetadataIngestRejectsAResultAfterSeriesRematch(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	store.enrichOnlineMetadata = true
	path := libraryIntegrationFile(t, root, "tv/Show/Season 1/Show.S01E02.mp4", "video:episode")
	collection := libraryIntegrationCreate(t, ctx, store, "Rematched TV", "tvshows", filepath.Join(root, "tv"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	episode := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	if episode.Series == nil {
		t.Fatal("missing fixture ancestry")
	}
	actor := metadataEditTestActor(t, ctx, pool, "online-rematch-editor")
	client := &onlineMetadataIngestFixtureProvider{configured: true}
	client.onLookup = func(selection providers.Selection) error {
		if selection.Type == "Episode" {
			metadataEditTestUpdate(t, ctx, store, actor, metadataEditTestDetail(t, ctx, store, actor, episode.Series.ID),
				map[string]json.RawMessage{"ProviderIds": json.RawMessage(`{"Tmdb":"200"}`)}, nil)
		}
		return nil
	}
	child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskOnlineMetadataIngestKey)
	if err := store.EnrichIngestedOnlineMetadata(ctx, client, child, collection.ID, fence, func(ProviderProgress) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var retained bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE item_id=$1 AND enrich_online)
		AND NOT EXISTS(SELECT 1 FROM item_provider_sources WHERE item_id=$1)`, episode.ID).Scan(&retained); err != nil || !retained {
		t.Fatalf("a result for the previous series escaped its publication fence: %v", err)
	}
	if len(client.lookups) != 3 {
		t.Fatal("fixture did not rematch during the dependent provider request")
	}
}
