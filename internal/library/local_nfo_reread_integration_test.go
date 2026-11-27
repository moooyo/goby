//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/providers"
)

func rereadNFOSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, targets ...string) string {
	t.Helper()
	if targets == nil {
		targets = []string{}
	}
	var facts string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg((to_jsonb(i)-CASE WHEN id=ANY($1) THEN
			ARRAY['name','sort_name','overview','updated_at','local_metadata','local_metadata_hash','local_metadata_path'] ELSE ARRAY[]::text[] END) ORDER BY id) FROM items i),
		'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m WHERE NOT (item_id=ANY($1))),
		'controls',(SELECT jsonb_agg(jsonb_build_array(item_id, overrides, locked_values, online_source, last_edited_by, last_edited_at) ORDER BY item_id) FROM item_metadata_state),
		'watching',(SELECT jsonb_agg(to_jsonb(w) ORDER BY user_id,item_id) FROM user_item_data w),
		'images',(SELECT jsonb_agg(to_jsonb(i) ORDER BY item_id,image_type,image_index) FROM item_images i),
		'jobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM scan_jobs j),
		'work',(SELECT jsonb_agg(to_jsonb(q) ORDER BY item_id) FROM local_metadata_ingest q),
		'recovery_work',(SELECT jsonb_agg(to_jsonb(q) ORDER BY item_id) FROM local_nfo_recovery_work q),
		'recovery_sources',(SELECT jsonb_agg(to_jsonb(q) ORDER BY item_id) FROM local_nfo_recovery_sources q),
		'tasks',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_runs r),
		'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY name) FROM task_system_events e),
		'activity',(SELECT count(*) FROM activity_entries),
		'provider_images',(SELECT count(*) FROM item_provider_images),
		'provider_sources',(SELECT count(*) FROM item_provider_sources))::text`, targets).Scan(&facts); err != nil {
		t.Fatalf("read side-effect snapshot: %v", err)
	}
	return facts
}

func TestLocalNFORereadUpdatesOnlyRequestedTVItems(t *testing.T) {
	var probes atomic.Int64
	ctx, pool, store, approved, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		probes.Add(1)
		return forceProbeTestInfo(file)
	}))
	actor := metadataEditTestActor(t, ctx, pool, "reread-admin")
	root := filepath.Join(approved, "tv")
	seriesPath := filepath.Join(root, "Physical Series")
	seasonPath := filepath.Join(seriesPath, "Season 01")
	episodePath := libraryIntegrationFile(t, root, "Physical Series/Season 01/Show.S01E01.mp4", "synthetic media")
	libraryIntegrationFile(t, root, "Physical Series/Season 01/Show.S01E02.mp4", "untouched sibling")
	seriesNFO := libraryIntegrationFile(t, root, "Physical Series/tvshow.nfo", `<tvshow><title>Old series</title><mpaa>R</mpaa><tag>Old tag</tag></tvshow>`)
	seasonNFO := libraryIntegrationFile(t, root, "Physical Series/Season 01/season.nfo", `<season><season>1</season><title>Old season</title></season>`)
	episodeNFO := libraryIntegrationFile(t, root, "Physical Series/Season 01/Show.S01E01.nfo", `<episodedetails><season>1</season><episode>1</episode><title>Old episode</title></episodedetails>`)
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative, "Reread TV", "tvshows", []string{root},
		LibraryOptions{EnableLocalMetadata: true, EnableLocalImages: true, EnableOnlineImages: true})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	series := nfoCatalogItem(t, ctx, store, viewer, collection.ID, seriesPath)
	season := nfoCatalogItem(t, ctx, store, viewer, collection.ID, seasonPath)
	episode := nfoCatalogItem(t, ctx, store, viewer, collection.ID, episodePath)
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite) VALUES ($1,$2,12345,2,true)`, viewer, episode.ID); err != nil {
		t.Fatal(err)
	}
	before := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	metadataEditTestUpdate(t, ctx, store, actor, before, map[string]json.RawMessage{"Name": json.RawMessage(`"Pinned display"`)}, []string{"OriginalTitle"})
	episodeDetail := metadataEditTestDetail(t, ctx, store, actor, episode.ID)
	if _, err := store.ApplyOnlineMetadata(ctx, actor, episode.ID, episodeDetail.Revision, providers.Metadata{
		Selection: providers.Selection{Provider: "tmdb", Type: "Episode", ID: "12345:1:1"},
		Fields:    map[string]json.RawMessage{"Overview": json.RawMessage(`"Accepted online overview"`), "IndexNumber": json.RawMessage(`17`)},
	}); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationUser(t, ctx, pool, "reread-restricted", false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5,"BlockedTags":["Old tag"]}'::jsonb WHERE id='reread-restricted'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, "reread-restricted", episode.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old inherited rating/tag allowed the episode: %v", err)
	}

	// Files are staged externally. One invocation must not pick up any sibling
	// change even when multiple sidecars changed in the same show.
	changes := []struct{ id, path, name, doc string }{
		{series.ID, seriesNFO, "New series", `<tvshow><title>New series</title><originaltitle>New original</originaltitle><mpaa>PG</mpaa><tmdbid>812345</tmdbid></tvshow>`},
		{season.ID, seasonNFO, "New season", `<season><season>1</season><title>New season</title><tvdbid>723456</tvdbid></season>`},
		{episode.ID, episodeNFO, "New episode", `<episodedetails><season>1</season><episode>1</episode><title>New episode</title></episodedetails>`},
	}
	for _, change := range changes {
		writeEpisodePairTestNFO(t, change.path, change.doc)
	}
	writes := make(chan CatalogNotification, len(changes))
	store.SetCatalogChangeListener(func(n CatalogNotification) {
		select {
		case writes <- n:
		default:
		}
	})
	mediaProbes := probes.Load()
	for _, change := range changes {
		untouched := rereadNFOSnapshot(t, ctx, pool, change.id)
		expectLocalNFOReread(t, ctx, store, actor, change.id, true)
		if got := rereadNFOSnapshot(t, ctx, pool, change.id); got != untouched {
			t.Fatalf("reread of %s touched a non-target row, the hierarchy, playback, a queue or a task event", change.name)
		}
		if got := metadataEditTestDetail(t, ctx, store, actor, change.id); got.Automatic.Name != change.name {
			t.Fatalf("automatic name = %q, want %q", got.Automatic.Name, change.name)
		}
		if data, err := os.ReadFile(change.path); err != nil || string(data) != change.doc {
			t.Fatalf("reread wrote to the sidecar: %v", err)
		}
		select {
		case n := <-writes:
			if n.Resync || len(n.Changes) != 1 || n.Changes[0].ItemID != change.id || n.Changes[0].Kind != CatalogUpdated {
				t.Fatalf("wrong post-commit notification: %+v", n)
			}
		default:
			t.Fatal("effective NFO change did not notify the catalog listener")
		}
	}
	accepted := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	if accepted.Effective.Name != "Pinned display" || accepted.Effective.OriginalTitle != before.Effective.OriginalTitle || accepted.Effective.ProviderIDs["Tmdb"] != "812345" {
		t.Fatalf("controls or the new local provider declaration did not compose: %+v", accepted.Effective)
	}
	if item, err := store.GetItem(ctx, "reread-restricted", episode.ID); err != nil || item.ParentID != season.ID || item.Overview != "Accepted online overview" || item.IndexNumber != 17 {
		t.Fatalf("reread lost child access, hierarchy or accepted online values: %+v (%v)", item, err)
	}
	unchanged := rereadNFOSnapshot(t, ctx, pool)
	expectLocalNFOReread(t, ctx, store, actor, episode.ID, false)
	if after := rereadNFOSnapshot(t, ctx, pool); after != unchanged || len(writes) != 0 || probes.Load() != mediaProbes {
		t.Fatal("unchanged reread wrote state, notified a client, or reprobed media")
	}
}

func expectLocalNFOReread(t *testing.T, ctx context.Context, store *Store, actor identity.Principal, id string, changed bool) {
	t.Helper()
	got, err := store.RereadLocalNFO(ctx, actor, id)
	if err != nil || got != (LocalNFORereadResult{ItemID: id, Changed: changed}) {
		t.Fatalf("reread = %+v, %v; want changed=%t", got, err, changed)
	}
}

func TestLocalNFORereadCandidatePrecedenceAndRejection(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	actor := metadataEditTestActor(t, ctx, pool, "reread-movie-admin")
	mediaPath := libraryIntegrationFile(t, root, "Movie.mp4", "synthetic media")
	primary := libraryIntegrationFile(t, root, "Movie.nfo", `<movie><title>Primary</title></movie>`)
	fallback := libraryIntegrationFile(t, root, "movie.nfo", `<movie><title>Fallback</title></movie>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Reread movies", "movies", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, viewer, collection.ID, mediaPath)
	checkRejected := func(want error) {
		t.Helper()
		before := rereadNFOSnapshot(t, ctx, pool)
		if got, err := store.RereadLocalNFO(ctx, actor, item.ID); !errors.Is(err, want) || got != (LocalNFORereadResult{}) {
			t.Fatalf("reread = %+v, %v; want %v", got, err, want)
		}
		if after := rereadNFOSnapshot(t, ctx, pool); after != before {
			t.Fatal("failed reread changed the previous catalog state")
		}
	}
	for _, invalid := range []string{"", `<movie/>`, `<movie><title>Broken`, `<tvshow><title>Wrong kind</title></tvshow>`, strings.Repeat("x", maxLocalNFOBytes+1)} {
		writeEpisodePairTestNFO(t, primary, invalid)
		checkRejected(ErrLocalNFORejected)
	}
	if err := os.Remove(primary); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("movie.nfo", primary); err != nil {
		t.Fatal(err)
	}
	checkRejected(ErrLocalNFORejected)
	if err := os.Remove(primary); err != nil {
		t.Fatal(err)
	}
	expectLocalNFOReread(t, ctx, store, actor, item.ID, true)
	if got := metadataEditTestDetail(t, ctx, store, actor, item.ID); got.Automatic.Name != "Fallback" {
		t.Fatalf("fallback was not selected on confirmed primary absence: %+v", got.Automatic)
	}
	if err := os.Remove(fallback); err != nil {
		t.Fatal(err)
	}
	checkRejected(ErrLocalNFOMissing)
}

func TestLocalNFORereadPairsAndNumbering(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	actor := metadataEditTestActor(t, ctx, pool, "reread-pair-admin")
	libraryIntegrationFile(t, root, "tvshow.nfo", `<tvshow><title>Root series</title></tvshow>`)
	mediaPath := libraryIntegrationFile(t, root, "Season 01/Show - S01E03E04 - 第 3-4 集.mp4", "synthetic media")
	special := libraryIntegrationFile(t, root, "Specials/Show.S00E01.mp4", "synthetic special")
	specialNFO := libraryIntegrationFile(t, root, "Specials/Show.S00E01.nfo", `<episodedetails><title>Special</title><season>0</season><episode>1</episode></episodedetails>`)
	nfoPath := strings.TrimSuffix(mediaPath, ".mp4") + ".nfo"
	first := pairedTestNFO(3, "First", "PG", "Family")
	second := pairedTestNFO(4, "Second", "PG", "Family")
	writeEpisodePairTestNFO(t, nfoPath, first+second)
	collection := libraryIntegrationCreate(t, ctx, store, "Reread paired TV", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, viewer, collection.ID, mediaPath)
	var seriesID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE library_id=$1 AND relative_path=$2`, collection.ID, tvRootSeriesPath).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	writeEpisodePairTestNFO(t, filepath.Join(root, "tvshow.nfo"), `<tvshow><title>Updated root series</title></tvshow>`)
	expectLocalNFOReread(t, ctx, store, actor, seriesID, true)
	specialItem := nfoCatalogItem(t, ctx, store, viewer, collection.ID, special)
	writeEpisodePairTestNFO(t, specialNFO, `<episodedetails><season>0</season><episode>1</episode><airsbefore_season>1</airsbefore_season><airsbefore_episode>2</airsbefore_episode></episodedetails>`)
	expectLocalNFOReread(t, ctx, store, actor, specialItem.ID, true)
	if got := nfoCatalogItem(t, ctx, store, viewer, collection.ID, special); got.ParentIndexNumber != 0 || got.IndexNumber != 1 || got.Metadata == nil ||
		got.Metadata.AirsBeforeEpisodeNumber == nil || *got.Metadata.AirsBeforeEpisodeNumber != 2 {
		t.Fatalf("special order was not retained without changing the episode number: %+v", got)
	}
	writeEpisodePairTestNFO(t, nfoPath, first+pairedTestNFO(4, "Split rating", "R", "Family"))
	if _, err := store.RereadLocalNFO(ctx, actor, item.ID); !errors.Is(err, ErrLocalNFORejected) {
		t.Fatalf("accepted conflicting pair: %v", err)
	}
	writeEpisodePairTestNFO(t, nfoPath, pairedTestNFO(4, "Wrong starting episode", "PG", "Family"))
	if _, err := store.RereadLocalNFO(ctx, actor, item.ID); !errors.Is(err, ErrLocalNFORejected) {
		t.Fatalf("renumbered episode: %v", err)
	}
	writeEpisodePairTestNFO(t, nfoPath, first+pairedTestNFO(4, "Revised second", "PG", "Family"))
	expectLocalNFOReread(t, ctx, store, actor, item.ID, true)
	got := nfoCatalogItem(t, ctx, store, viewer, collection.ID, mediaPath)
	if end, ok := got.EpisodeEndNumber(); !ok || end != 4 || got.Name != "First,  Revised second" || got.ID != item.ID || got.ParentID != item.ParentID {
		t.Fatalf("bound reread changed the physical pair: %+v", got)
	}
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, collection.ID); err != nil {
		t.Fatal(err)
	}
	writeEpisodePairTestNFO(t, nfoPath, first)
	if _, err := store.RereadLocalNFO(ctx, actor, item.ID); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("reread removed the policy witness for a stale pair: %v", err)
	}
	writeEpisodePairTestNFO(t, nfoPath, first+second)
	expectLocalNFOReread(t, ctx, store, actor, item.ID, true)
	if got := nfoCatalogItem(t, ctx, store, viewer, collection.ID, mediaPath); got.Name != "First,  Second" || got.ParentID != item.ParentID {
		t.Fatalf("new fully bound pair did not replace the stale evidence: %+v", got)
	}
}

func TestLocalNFORereadRechecksBeforeCommit(t *testing.T) {
	for _, reason := range []string{"new candidate", "replaced media", "media ctime only", "completed scan", "revoked session"} {
		t.Run(reason, func(t *testing.T) {
			ctx, pool, store, root, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
			actor := metadataEditTestActor(t, ctx, pool, "reread-race-admin")
			mediaPath := libraryIntegrationFile(t, root, "Movie.mp4", "synthetic media")
			sidecar := libraryIntegrationFile(t, root, "movie.nfo", `<movie><title>Before</title></movie>`)
			collection := libraryIntegrationCreate(t, ctx, store, "Reread race", "movies", root)
			libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			item := nfoCatalogItem(t, ctx, store, viewer, collection.ID, mediaPath)
			before := metadataEditTestDetail(t, ctx, store, actor, item.ID)
			writeEpisodePairTestNFO(t, sidecar, `<movie><title>Proposed but not committed</title></movie>`)
			want := ErrSourceChanged
			if reason == "revoked session" {
				want = ErrForbidden
			}
			observed := false
			store.SetCatalogChangeListener(func(CatalogNotification) { t.Error("rolled-back reread sent a notification") })
			_, err := store.rereadLocalNFO(ctx, actor, item.ID, func(work context.Context, op *PrimaryRootIO,
				plan seriesNFOAuditRootPlan, target localNFORereadTarget) (localNFORereadObservation, error) {
				result, err := observeLocalNFOReread(work, op, plan, target)
				if err != nil {
					return result, err
				}
				observed = true
				switch reason {
				case "new candidate":
					writeEpisodePairTestNFO(t, filepath.Join(root, "Movie.nfo"), `<movie><title>New preferred candidate</title></movie>`)
				case "replaced media":
					if err := os.Remove(mediaPath); err != nil {
						t.Fatal(err)
					}
					writeEpisodePairTestNFO(t, mediaPath, "different media bytes")
				case "media ctime only":
					fileBefore, err := os.Stat(mediaPath)
					if err != nil {
						t.Fatal(err)
					}
					dirBefore, err := os.Stat(root)
					if err != nil {
						t.Fatal(err)
					}
					writeEpisodePairTestNFO(t, mediaPath, "different bytes")
					if err := os.Chtimes(mediaPath, fileBefore.ModTime(), fileBefore.ModTime()); err != nil {
						t.Fatal(err)
					}
					fileAfter, fileErr := os.Stat(mediaPath)
					dirAfter, dirErr := os.Stat(root)
					if fileErr != nil || dirErr != nil || !os.SameFile(fileBefore, fileAfter) || fileBefore.Size() != fileAfter.Size() ||
						!fileBefore.ModTime().Equal(fileAfter.ModTime()) || media.FileChangeTime(fileBefore) == media.FileChangeTime(fileAfter) ||
						!sameSubtitleDirectoryInfo(dirBefore, dirAfter) {
						t.Fatal("fixture did not isolate the media change-time witness")
					}
				case "completed scan":
					if _, err := pool.Exec(work, `INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES ('reread-competing-generation',$1,'Completed',clock_timestamp())`, collection.ID); err != nil {
						t.Fatal(err)
					}
				case "revoked session":
					if _, err := pool.Exec(work, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
						t.Fatal(err)
					}
				}
				return result, nil
			})
			if !observed || !errors.Is(err, want) {
				t.Fatalf("reread after %s = %v, want %v", reason, err, want)
			}
			var name string
			var revision int64
			if err := pool.QueryRow(ctx, `SELECT i.name,ms.revision FROM items i JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.id=$1`, item.ID).Scan(&name, &revision); err != nil ||
				name != "Before" || revision != metadataEditTestRevision(t, before.Revision) {
				t.Fatalf("unsafe reread changed state: %q revision=%d (%v)", name, revision, err)
			}
		})
	}
}

func TestLocalNFORereadLeavesRecoveryAndOnlineTaskOwnershipIntact(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	actor := metadataEditTestActor(t, ctx, pool, "reread-owned-admin")
	root = filepath.Join(root, "movies")
	store.nfoRecoveryRoots = map[string]struct{}{root: {}}
	store.deferLocalMetadata = true
	mediaPath := libraryIntegrationFile(t, root, "Feature.mp4", "synthetic media")
	sidecar := libraryIntegrationFile(t, root, "Feature.nfo", `<movie><title>Recoverable version</title></movie>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Reread managed sources", "movies", root)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, viewer, collection.ID, mediaPath)
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	assertEpisodePairPending(t, ctx, pool, item.ID, job.ID, false)
	var saved string
	if err := pool.QueryRow(ctx, `SELECT document_hash FROM local_nfo_recovery_sources WHERE item_id=$1`, item.ID).Scan(&saved); err != nil || len(saved) != 64 {
		t.Fatalf("fixture has no accepted recovery copy: %v", err)
	}
	writeEpisodePairTestNFO(t, sidecar, `<movie><title>Changed outside the recovery workflow</title></movie>`)
	before := rereadNFOSnapshot(t, ctx, pool)
	if _, err := store.RereadLocalNFO(ctx, actor, item.ID); !errors.Is(err, ErrLocalNFORecoveryOwned) {
		t.Fatalf("reread bypassed an exclusively managed recovery root: %v", err)
	}
	if rereadNFOSnapshot(t, ctx, pool) != before {
		t.Fatal("reread invalidated a recovery copy or changed work")
	}

	store.nfoRecoveryRoots = nil
	metadataIngestTestWorker(t, ctx, pool, collection, TaskOnlineMetadataRefreshKey)
	before = rereadNFOSnapshot(t, ctx, pool)
	if _, err := store.RereadLocalNFO(ctx, actor, item.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("reread overlapped an active provider refresh: %v", err)
	}
	if rereadNFOSnapshot(t, ctx, pool) != before {
		t.Fatal("busy reread changed provider work or the catalog")
	}
}

func TestLocalNFORereadDoesNotConsumePendingGeneration(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	actor := metadataEditTestActor(t, ctx, pool, "reread-pending-admin")
	store.deferLocalMetadata = true
	mediaPath := libraryIntegrationFile(t, root, "Movie.mp4", "synthetic media")
	libraryIntegrationFile(t, root, "Movie.nfo", `<movie><title>Pending</title></movie>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Reread pending", "movies", root)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, viewer, collection.ID, mediaPath)
	assertEpisodePairPending(t, ctx, pool, item.ID, job.ID, true)
	before := rereadNFOSnapshot(t, ctx, pool)
	if _, err := store.RereadLocalNFO(ctx, actor, item.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("reread took ownership of pending NFO work: %v", err)
	}
	if rereadNFOSnapshot(t, ctx, pool) != before {
		t.Fatal("pending reread changed a committed scan or its queue")
	}
}
