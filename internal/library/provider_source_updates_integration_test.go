//go:build linux

package library

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/providers"
)

type providerSourceUpdateCounts struct {
	subtitleInserts, subtitleLookups, returnedBindings int
	albumComparisons, albumUpdates, conditionalUpdates int
	metadataSyncs                                      int
}

type providerSourceUpdateTrace struct {
	mu     sync.Mutex
	counts providerSourceUpdateCounts
}

func (trace *providerSourceUpdateTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.Join(strings.Fields(data.SQL), " ")
	trace.mu.Lock()
	defer trace.mu.Unlock()
	switch {
	case strings.HasPrefix(statement, "INSERT INTO item_subtitle_provider_sources "):
		trace.counts.subtitleInserts++
		if strings.HasSuffix(statement, " RETURNING provider, provider_id") {
			trace.counts.returnedBindings++
		}
	case strings.HasPrefix(statement, "SELECT provider, provider_id FROM item_subtitle_provider_sources "):
		trace.counts.subtitleLookups++
	case strings.HasPrefix(statement, "SELECT music_source IS DISTINCT FROM "):
		trace.counts.albumComparisons++
	case strings.HasPrefix(statement, "UPDATE item_metadata_state SET music_source = "):
		trace.counts.albumUpdates++
		if strings.Contains(statement, "WHERE item_id = $1 AND music_source IS DISTINCT FROM $2::jsonb") {
			trace.counts.conditionalUpdates++
		}
	case strings.HasPrefix(statement, "SELECT i.name, i.sort_name, i.overview, i.type, i.index_number, i.parent_index_number, i.local_metadata, ms.music_source,"):
		trace.counts.metadataSyncs++
	}
	return ctx
}

func (*providerSourceUpdateTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *providerSourceUpdateTrace) reset() {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.counts = providerSourceUpdateCounts{}
}

func (trace *providerSourceUpdateTrace) require(t *testing.T, want providerSourceUpdateCounts) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if trace.counts != want {
		t.Fatalf("source update statements = %+v, want %+v", trace.counts, want)
	}
}

func providerSourceTracedStore(t *testing.T, ctx context.Context, pool *pgxpool.Pool, initial *Store, root string, prober Prober) (*Store, *providerSourceUpdateTrace) {
	t.Helper()
	if err := initial.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &providerSourceUpdateTrace{}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	traced, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, traced)
	store, err := New(traced, prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	trace.reset()
	return store, trace
}

func providerSubtitleSourceSnapshot(t *testing.T, fixture mediaSourceFixture) string {
	t.Helper()
	var result string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT jsonb_build_object(
		'subtitles', COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.stream_index)
			FROM item_subtitles s WHERE s.item_id=$1), '[]'::jsonb),
		'sources', COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.stream_index)
			FROM item_subtitle_provider_sources p WHERE p.item_id=$1), '[]'::jsonb))::text`,
		fixture.item.ID).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProviderSubtitleSourceReturnsInsertedBindingAndReadsConflicts(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := ownedSubtitleManagementActor(t, fixture, "provider-source-binding")
	store, trace := providerSourceTracedStore(t, fixture.ctx, fixture.pool, fixture.store, fixture.allowedRoot,
		mediaSourceTestProber{inner: &libraryFixtureProber{}})
	fixture.store = store
	notifications := catalogChangesTestListener(t, store)
	download := providers.SubtitleDownload{Provider: "opensubtitles", RemoteID: "source-binding:1", Language: "en", Format: "srt", Data: []byte(subtitleTestSRT)}
	if err := store.RegisterDownloadedSubtitle(fixture.ctx, actor, fixture.item.ID, download); err != nil {
		t.Fatal(err)
	}
	trace.require(t, providerSourceUpdateCounts{subtitleInserts: 1, returnedBindings: 1})
	nextCatalogTestNotification(t, notifications)
	before := providerSubtitleSourceSnapshot(t, fixture)
	var recordedProvider, recordedID string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT provider, provider_id
		FROM item_subtitle_provider_sources WHERE item_id=$1`, fixture.item.ID).Scan(&recordedProvider, &recordedID); err != nil ||
		recordedProvider != download.Provider || recordedID != download.RemoteID {
		t.Fatalf("new binding did not retain the exact provider identity: %q %q %v", recordedProvider, recordedID, err)
	}
	trace.reset()
	if err := store.RegisterDownloadedSubtitle(fixture.ctx, actor, fixture.item.ID, download); err != nil {
		t.Fatalf("matching binding was not idempotent: %v", err)
	}
	trace.require(t, providerSourceUpdateCounts{subtitleInserts: 1, subtitleLookups: 1, returnedBindings: 1})
	if current := providerSubtitleSourceSnapshot(t, fixture); current != before {
		t.Fatal("matching registration changed subtitle provenance or catalog fields")
	}
	assertNoCatalogTestNotification(t, notifications)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_subtitle_provider_sources SET provider_id=$2 WHERE item_id=$1`,
		fixture.item.ID, "conflicting-provider-id"); err != nil {
		t.Fatal(err)
	}
	before = providerSubtitleSourceSnapshot(t, fixture)
	trace.reset()
	if err := store.RegisterDownloadedSubtitle(fixture.ctx, actor, fixture.item.ID, download); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("conflicting provenance was accepted: %v", err)
	}
	trace.require(t, providerSourceUpdateCounts{subtitleInserts: 1, subtitleLookups: 1, returnedBindings: 1})
	if current := providerSubtitleSourceSnapshot(t, fixture); current != before {
		t.Fatal("conflicting registration changed the existing subtitle or binding")
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestProviderSubtitleInsertedSourceRollsBackAfterFinalFailure(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := ownedSubtitleManagementActor(t, fixture, "provider-source-rollback")
	store, trace := providerSourceTracedStore(t, fixture.ctx, fixture.pool, fixture.store, fixture.allowedRoot,
		mediaSourceTestProber{inner: &libraryFixtureProber{}})
	fixture.store = store
	notifications := catalogChangesTestListener(t, store)
	names := providerSubtitlePreflightNames(t, filepath.Dir(fixture.path))
	before := providerSubtitleSourceSnapshot(t, fixture)
	snapshot, err := store.readProviderSubtitleSnapshot(fixture.ctx, &actor, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	download := providers.SubtitleDownload{Provider: "opensubtitles", RemoteID: "source-rollback:1", Language: "en", Format: "srt", Data: []byte(subtitleTestSRT)}
	prepared, err := store.prepareProviderSubtitle(fixture.ctx, &actor, snapshot, primaryio.Foreground,
		download.RemoteID, download.Language, download.Format, false, false, download.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	wantErr := errors.New("final source observation failed")
	reachedFinal := false
	err = store.publishProviderSubtitleBeforeFinal(fixture.ctx, &actor, prepared, download, func() error {
		reachedFinal = true
		trace.require(t, providerSourceUpdateCounts{subtitleInserts: 1, returnedBindings: 1})
		return wantErr
	})
	if !reachedFinal || !errors.Is(err, wantErr) {
		t.Fatalf("publication did not preserve the final failure: reached=%v error=%v", reachedFinal, err)
	}
	if err := prepared.close(); err != nil {
		t.Fatal(err)
	}
	if current := providerSubtitleSourceSnapshot(t, fixture); current != before {
		t.Fatal("final failure retained a newly inserted subtitle or provider binding")
	}
	assertProviderSubtitlePreflightUnchanged(t, fixture, names)
	assertNoCatalogTestNotification(t, notifications)
}

func TestAcceptedMusicAlbumConditionalSourceUpdate(t *testing.T) {
	prober := &metadataMusicScanProber{}
	prober.set("Track.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
		Title: "Track", Album: "Original album", Artist: "Original artist"}, false)
	ctx, pool, initial, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Album/Track.flac", "audio:conditional-album-source")
	collection := libraryIntegrationCreate(t, ctx, initial, "Conditional album source", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, initial, collection.ID, "Completed")
	track := nfoCatalogItem(t, ctx, initial, userID, collection.ID, path)
	album := nfoCatalogItem(t, ctx, initial, userID, collection.ID, filepath.Dir(path))
	store, trace := providerSourceTracedStore(t, ctx, pool, initial, root, prober)
	notifications := catalogChangesTestListener(t, store)
	expectedQueries := providerSourceUpdateCounts{albumUpdates: 1, conditionalUpdates: 1}
	before := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
	if ready, err := store.refreshOwnedMusicAlbum(ctx, collection.ID, album.ID); err != nil || !ready {
		t.Fatalf("unchanged source did not remain ready: ready=%v error=%v", ready, err)
	}
	trace.require(t, expectedQueries)
	if current := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID); current != before {
		t.Fatal("unchanged source rewrote metadata state or entity rows")
	}
	assertNoCatalogTestNotification(t, notifications)
	setAlbum := func(name string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE items SET media=jsonb_set(media, '{EmbeddedMusic,Album}', to_jsonb($2::text)) WHERE id=$1`,
			track.ID, name); err != nil {
			t.Fatal(err)
		}
	}
	setAlbum("Changed album")
	trace.reset()
	if ready, err := store.refreshOwnedMusicAlbum(ctx, collection.ID, album.ID); err != nil || !ready {
		t.Fatalf("changed source was not published: ready=%v error=%v", ready, err)
	}
	changedQueries := expectedQueries
	changedQueries.metadataSyncs = 1
	trace.require(t, changedQueries)
	if current, err := store.GetItem(ctx, userID, album.ID); err != nil || current.Name != "Changed album" {
		t.Fatalf("changed source did not synchronize effective metadata: item=%+v error=%v", current, err)
	}
	metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{Version: musicSourceVersion,
		Name: "Changed album", Album: "Changed album", Artists: []string{"Original artist"}, AlbumArtists: []string{"Original artist"}})
	nextCatalogTestNotification(t, notifications)
	assertNoCatalogTestNotification(t, notifications)
	setAlbum("Rejected album")
	before = metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
	itemBefore := metadataEditTestSnapshot(t, ctx, pool, album.ID)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_source_update_fixture() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'source update fixture failure' USING ERRCODE='P0001'; END; $$`); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []struct{ name, target string }{
		{"source-write", "UPDATE OF music_source ON item_metadata_state"},
		{"metadata-sync", "UPDATE ON items"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "CREATE TRIGGER reject_source_update_fixture BEFORE "+failure.target+
				" FOR EACH ROW EXECUTE FUNCTION reject_source_update_fixture()"); err != nil {
				t.Fatal(err)
			}
			table := "item_metadata_state"
			if failure.name == "metadata-sync" {
				table = "items"
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(ctx, "DROP TRIGGER reject_source_update_fixture ON "+table); err != nil {
					t.Error(err)
				}
			})
			trace.reset()
			ready, err := store.refreshOwnedMusicAlbum(ctx, collection.ID, album.ID)
			var databaseError *pgconn.PgError
			if ready || !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
				t.Fatalf("failed source publication was reported as ready: ready=%v error=%v", ready, err)
			}
			if failure.name == "metadata-sync" {
				trace.require(t, changedQueries)
			} else {
				trace.require(t, expectedQueries)
			}
			if current := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID); current != before ||
				metadataEditTestSnapshot(t, ctx, pool, album.ID) != itemBefore {
				t.Fatal("failed publication did not roll back its accepted source or metadata")
			}
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}
