//go:build linux

package library

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/providers"
)

func TestLocalMetadataIngestImportsCompatibleNFOWithoutGuessingProviderIdentities(t *testing.T) {
	probe := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		return forceProbeTestInfo(file)
	})
	ctx, pool, store, root, userID := libraryIntegrationStore(t, probe)
	actor := metadataEditTestActor(t, ctx, pool, "opaque-series-editor")
	const opaqueID = "https://example.invalid/not-a-provider-lookup-id"
	store.deferLocalMetadata = true
	store.enrichOnlineMetadata = true
	numbered := libraryIntegrationFile(t, root, "tv/Numbered/Season 1/Numbered.S01E01.mp4", "video:opaque-child-id")
	libraryIntegrationFile(t, root, "tv/Numbered/Season 1/Numbered.S01E01.nfo", `<episodedetails><title>Opaque child</title><tmdbid>`+opaqueID+`</tmdbid></episodedetails>`)
	libraryIntegrationFile(t, root, "tv/Numbered/Season 1/Numbered.S01E02.mp4", "video:addressable-child-control")
	libraryIntegrationFile(t, root, "tv/Numbered/Season 1/Numbered.S01E02.nfo", `<episodedetails><tmdbid>201</tmdbid></episodedetails>`)
	libraryIntegrationFile(t, root, "tv/Numbered/tvshow.nfo", `<tvshow><title>NFO series</title><season>-1</season><episode>-1</episode>
		<uniqueid type="TMDB-TV">100</uniqueid><uniqueid type="eidr">10.5240/TEST</uniqueid>
		<uniqueid type="official website">https://example.invalid/series</uniqueid></tvshow>`)
	otherSeries := filepath.Join(root, "tv", "Opaque")
	libraryIntegrationFile(t, root, "tv/Opaque/Season 1/Opaque.S01E01.mp4", "video:opaque-series-id")
	libraryIntegrationFile(t, root, "tv/Opaque/tvshow.nfo", `<tvshow><title>Opaque series</title><uniqueid type="tmdb">`+opaqueID+`</uniqueid></tvshow>`)
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative, "NFO compatibility", "tvshows",
		[]string{filepath.Join(root, "tv")}, LibraryOptions{EnableLocalMetadata: true})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	first := nfoCatalogItem(t, ctx, store, userID, collection.ID, numbered)
	if first.Name == "Opaque child" {
		t.Fatal("NFO must be exercised by deferred import, not the scan")
	}
	child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskLocalMetadataIngestKey)
	var progress ProviderProgress
	importErr := store.ImportLocalScanMetadata(ctx, child, collection.ID, fence, func(value ProviderProgress) error {
		progress = value
		return nil
	})
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM local_metadata_ingest WHERE import_nfo`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if importErr != nil || pending != 0 || progress.Updated != 4 {
		t.Fatalf("deferred NFO import: progress=%+v, pending=%d, error=%v", progress, pending, importErr)
	}
	series := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Join(root, "tv", "Numbered"))
	want := map[string]string{"Tmdb": "100", "eidr": "10.5240/TEST", "official website": "https://example.invalid/series"}
	if series.Name != "NFO series" || series.Metadata == nil || !maps.Equal(series.Metadata.ProviderIDs, want) {
		t.Fatalf("combined series NFO was not imported: name=%q, metadata=%+v", series.Name, series.Metadata)
	}
	accepted := nfoCatalogItem(t, ctx, store, userID, collection.ID, numbered)
	if accepted.ID != first.ID || accepted.ParentID != first.ParentID || accepted.Name != "Opaque child" ||
		accepted.Metadata == nil || accepted.Metadata.ProviderIDs["Tmdb"] != opaqueID {
		t.Fatal("local import lost the opaque ID or changed the physical hierarchy")
	}
	other := nfoCatalogItem(t, ctx, store, userID, collection.ID, otherSeries)
	if other.Metadata == nil || other.Metadata.ProviderIDs["Tmdb"] != opaqueID {
		t.Fatal("opaque series ID was not stored")
	}
	order, err := store.GetSeriesDisplayOrder(ctx, actor, other.ID)
	if err != nil || order.ProviderSeriesID != "" || order.DisplayOrder != "Aired" {
		t.Fatalf("series order exposed a non-addressable ID: %+v, error=%v", order, err)
	}

	onlineChild, onlineFence := metadataIngestTestWorker(t, ctx, pool, collection, TaskOnlineMetadataIngestKey)
	client := &onlineMetadataIngestFixtureProvider{configured: true}
	if err := store.EnrichIngestedOnlineMetadata(ctx, client, onlineChild, collection.ID, onlineFence, func(ProviderProgress) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(client.searches) != 0 || !reflect.DeepEqual(client.lookups, []string{"Series:100", "Season:100:1", "Episode:100:1:2"}) {
		t.Fatalf("opaque IDs triggered requests or interrupted a valid match: searches=%v lookups=%v", client.searches, client.lookups)
	}
	stillLocal := nfoCatalogItem(t, ctx, store, userID, collection.ID, numbered)
	if stillLocal.Name != accepted.Name || stillLocal.Metadata == nil || stillLocal.Metadata.ProviderIDs["Tmdb"] != opaqueID {
		t.Fatal("online enrichment replaced an opaque local selection")
	}
}

func TestStoreNFOOpaqueIDsDoNotStopOnlineArtworkOrAuthorizeMetadataLookup(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "opaque-artwork-editor")
	const opaqueID = "https://example.invalid/opaque/7"
	movie := libraryIntegrationFile(t, root, "movies/Opaque.mp4", "video:opaque-id")
	libraryIntegrationFile(t, root, "movies/Opaque.nfo", `<movie><title>Opaque title</title><tmdbid>`+opaqueID+`</tmdbid></movie>`)
	libraryIntegrationFile(t, root, "movies/Numeric.mp4", "video:valid-id")
	libraryIntegrationFile(t, root, "movies/Numeric.nfo", `<movie><tmdbid>42</tmdbid></movie>`)
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative, "Opaque IDs", "movies",
		[]string{filepath.Dir(movie)}, LibraryOptions{EnableLocalMetadata: true, EnableOnlineImages: true})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, movie)
	query, err := store.OnlineMetadataQuery(ctx, actor, item.ID)
	if err != nil || query.Name != "Opaque title" || query.ProviderIDs["Tmdb"] != opaqueID {
		t.Fatalf("opaque ID was not accepted as local metadata: query=%+v, error=%v", query, err)
	}
	matcher := &onlineMetadataIngestFixtureProvider{configured: true}
	if _, found, err := selectProviderTaskMetadata(ctx, matcher, "tmdb", query); err != nil || found || len(matcher.searches) != 0 {
		t.Fatalf("opaque ID authorized a lookup or title search: found=%v, searches=%v, error=%v", found, matcher.searches, err)
	}

	artwork := &onlineImageTestProvider{onList: func(selection providers.Selection) ([]providers.RemoteImage, error) {
		if selection.ID != "42" {
			t.Fatalf("invalid image lookup ID: %q", selection.ID)
		}
		return nil, nil
	}}
	if err := store.FillMissingOnlineImages(ctx, artwork, collection.ID, onlineImageTestFence(ctx), nil); err != nil || artwork.listCalls != 1 {
		t.Fatalf("opaque ID interrupted eligible artwork work: calls=%d, error=%v", artwork.listCalls, err)
	}
	result, err := store.DownloadMissingOnlineImage(ctx, actor, artwork, collection.ID, item.ID, "Primary")
	if err != nil || result.Status != "unmatched" || artwork.listCalls != 1 {
		t.Fatalf("manual missing-image request did not report unmatched: status=%q, calls=%d, error=%v", result.Status, artwork.listCalls, err)
	}
}

func TestStoreNFOLockFreezesAcceptedOpaqueProviderIDs(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:locked-provider-ids")
	libraryIntegrationFile(t, root, "movies/Film.nfo", `<movie><uniqueid type="official website">https://example.invalid/film</uniqueid>
		<uniqueid type="資料庫">10.5240/test</uniqueid></movie>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Locked NFO IDs", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	actor := metadataEditTestActor(t, ctx, pool, "opaque-lock-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	want := map[string]string{"official website": "https://example.invalid/film", "資料庫": "10.5240/test"}
	if !maps.Equal(detail.Effective.ProviderIDs, want) {
		t.Fatalf("opaque ID readback = %v, want %v", detail.Effective.ProviderIDs, want)
	}
	locked := metadataEditTestUpdate(t, ctx, store, actor, detail, nil, []string{"ProviderIds"})
	var ids map[string]string
	if err := json.Unmarshal(locked.LockedValues["ProviderIds"], &ids); err != nil || !maps.Equal(ids, want) {
		t.Fatalf("captured IDs = %v, error=%v", ids, err)
	}
	libraryIntegrationFile(t, root, "movies/Film.nfo", `<movie><title>Updated title</title><tmdbid>42</tmdbid></movie>`)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	after := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if after.Effective.Name != "Updated title" || !maps.Equal(after.Effective.ProviderIDs, want) ||
		!maps.Equal(after.Automatic.ProviderIDs, map[string]string{"Tmdb": "42"}) {
		t.Fatal("a rescan did not retain the lock separately from the new NFO")
	}
}
