//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

const (
	seriesNFOAuditTestLeafA = "Amber Meridian (2004)"
	seriesNFOAuditTestLeafB = "Beryl Causeway (2016)"
	seriesNFOAuditTestName  = "Beryl Causeway"
	seriesNFOAuditTestID    = "731009803"
	seriesNFOAuditTestDocA  = `<tvshow><title>Beryl Causeway</title><year>2016</year><uniqueid type="tmdb">731009803</uniqueid><tag>Audit blocked</tag></tvshow>`
	seriesNFOAuditTestDocB  = `<tvshow>
	<title>Beryl Causeway</title><year>2016</year><tmdbid>731009803</tmdbid><plot>Synthetic peer</plot>
</tvshow>`
)

func seriesNFOAuditTestFiles(t *testing.T, root, leaf, document string) (sidecar, episode string) {
	t.Helper()
	sidecar = libraryIntegrationFile(t, root, filepath.Join(leaf, "tvshow.nfo"), document)
	episode = libraryIntegrationFile(t, root, filepath.Join(leaf, "Season 01", "Synthetic.S01E01.mp4"), "video:"+leaf)
	return sidecar, episode
}

func seriesNFOAuditTestLibrary(t *testing.T, ctx context.Context, store *Store, actor identity.Principal, name, root string) Library {
	t.Helper()
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
		name, "tvshows", []string{root}, LibraryOptions{EnableLocalMetadata: true})
	if err != nil {
		t.Fatal(err)
	}
	return collection
}

func requireSeriesNFOAudit(t *testing.T, ctx context.Context, store *Store, actor identity.Principal,
	libraryID, status string, series, observed int, warnings ...SeriesNFOAuditWarning) {
	t.Helper()
	report, err := store.AuditSeriesNFO(ctx, actor, libraryID)
	if err != nil || report.LibraryID != libraryID || report.Status != status || report.SeriesCount != series ||
		report.ObservedCount != observed || !slices.Equal(report.Warnings, warnings) {
		t.Fatalf("audit = %+v (%v); want library %q, status %q, series %d, observed %d, warnings %+v",
			report, err, libraryID, status, series, observed, warnings)
	}
}

func seriesNFOAuditTestBytes(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// These tables are in the fixture's isolated schema. Compare complete rows so
// a change to an existing task, backoff, or queue entry cannot hide behind counts.
func seriesNFOAuditTestState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items', (SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM items i),
		'metadata', (SELECT jsonb_agg(to_jsonb(m) ORDER BY m.item_id) FROM item_metadata_state m),
		'playback', (SELECT jsonb_agg(to_jsonb(u) ORDER BY u.user_id,u.item_id) FROM user_item_data u),
		'scans', (SELECT jsonb_agg(to_jsonb(j) ORDER BY j.id) FROM scan_jobs j),
		'import_queue', (SELECT jsonb_agg(to_jsonb(q) ORDER BY q.item_id) FROM local_metadata_ingest q),
		'recovery_work', (SELECT jsonb_agg(to_jsonb(w) ORDER BY w.item_id) FROM local_nfo_recovery_work w),
		'recovery_sources', (SELECT jsonb_agg(to_jsonb(s) ORDER BY s.item_id) FROM local_nfo_recovery_sources s),
		'task_runs', (SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM task_runs r),
		'task_children', (SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM task_run_children c),
		'task_events', (SELECT jsonb_agg(to_jsonb(e) ORDER BY e.name) FROM task_system_events e),
		'notifications', (SELECT jsonb_agg(to_jsonb(n) ORDER BY n.sequence) FROM notification_source_events n)
	)::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot audit fixture: %v", err)
	}
	return snapshot
}

func TestAuditSeriesNFOReadsOnlyCurrentAcceptedDeclarations(t *testing.T) {
	for _, mode := range []struct {
		name     string
		deferred bool
	}{{"synchronous", false}, {"deferred", true}} {
		t.Run(mode.name, func(t *testing.T) {
			ctx, pool, store, approved, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
			actor := metadataEditTestActor(t, ctx, pool, "series-audit-admin")
			root := filepath.Join(approved, "tv")
			store.deferLocalMetadata = mode.deferred
			store.nfoRecoveryRoots = map[string]struct{}{root: {}}
			sidecarA, episodePathA := seriesNFOAuditTestFiles(t, root, seriesNFOAuditTestLeafA, seriesNFOAuditTestDocA)
			sidecarB, episodePathB := seriesNFOAuditTestFiles(t, root, seriesNFOAuditTestLeafB, seriesNFOAuditTestDocB)
			collection := seriesNFOAuditTestLibrary(t, ctx, store, actor, "Synthetic series audit", root)
			const restricted = "series-audit-restricted"
			libraryIntegrationUser(t, ctx, pool, restricted, false, true, nil)
			if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"BlockedTags":["Audit blocked"]}'::jsonb WHERE id=$1`, restricted); err != nil {
				t.Fatal(err)
			}
			query := func(userID string) ItemResult {
				t.Helper()
				return libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: collection.ID, Recursive: true,
					IncludeItemTypes: []string{"Series", "Episode"}})
			}
			var checkPlayback func()
			checkAudit := func(phase string, observed int, warnings ...SeriesNFOAuditWarning) {
				t.Helper()
				ordinary, limited := query(viewer), query(restricted)
				before := seriesNFOAuditTestState(t, ctx, pool)
				aBytes, bBytes := seriesNFOAuditTestBytes(t, sidecarA), seriesNFOAuditTestBytes(t, sidecarB)
				requireSeriesNFOAudit(t, ctx, store, actor, collection.ID, "observed", 2, observed, warnings...)
				if !reflect.DeepEqual(ordinary, query(viewer)) || !reflect.DeepEqual(limited, query(restricted)) {
					t.Fatalf("%s: audit changed ordinary or restricted catalog results", phase)
				}
				if after := seriesNFOAuditTestState(t, ctx, pool); after != before {
					t.Fatalf("%s: audit changed persisted catalog, playback, recovery, task, queue or notification state", phase)
				}
				if seriesNFOAuditTestBytes(t, sidecarA) != aBytes || seriesNFOAuditTestBytes(t, sidecarB) != bBytes {
					t.Fatalf("%s: audit rewrote a sidecar", phase)
				}
				if checkPlayback != nil {
					checkPlayback()
				}
			}
			importLocal := func() {
				t.Helper()
				if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
					t.Fatal(err)
				}
			}

			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			first := query(viewer)
			a := libraryIntegrationItemByPath(t, first.Items, filepath.Dir(sidecarA))
			b := libraryIntegrationItemByPath(t, first.Items, filepath.Dir(sidecarB))
			if mode.deferred {
				assertEpisodePairPending(t, ctx, pool, a.ID, job.ID, true)
				assertEpisodePairPending(t, ctx, pool, b.ID, job.ID, true)
				checkAudit("before deferred import", 0)
				importLocal()
			}
			ordinary := query(viewer)
			if ordinary.TotalRecordCount != 4 {
				t.Fatalf("want two series with an episode each; got %d items", ordinary.TotalRecordCount)
			}
			for _, show := range []Item{a, b} {
				accepted := libraryIntegrationItemByPath(t, ordinary.Items, show.Path)
				if accepted.Type != "Series" || accepted.ID != show.ID || accepted.Name != seriesNFOAuditTestName || accepted.Metadata == nil ||
					accepted.Metadata.ProductionYear == nil || *accepted.Metadata.ProductionYear != 2016 || accepted.Metadata.ProviderIDs["Tmdb"] != seriesNFOAuditTestID {
					t.Fatalf("shared declaration was not normally imported for %s: %+v", show.Path, accepted)
				}
			}
			episodeA := libraryIntegrationItemByPath(t, ordinary.Items, episodePathA)
			episodeB := libraryIntegrationItemByPath(t, ordinary.Items, episodePathB)
			if a.ID == b.ID || episodeA.ID == episodeB.ID || episodeA.Series == nil || episodeA.Series.ID != a.ID ||
				episodeB.Series == nil || episodeB.Series.ID != b.ID {
				t.Fatal("import merged distinct physical series or their episodes")
			}
			limited := query(restricted)
			if limited.TotalRecordCount != 2 || libraryIntegrationItemByPath(t, limited.Items, b.Path).ID != b.ID ||
				libraryIntegrationItemByPath(t, limited.Items, episodePathB).ID != episodeB.ID {
				t.Fatal("restricted viewer should see only the peer series and its episode")
			}
			position, playCount := 123*media.TicksPerSecond, 2
			playback, err := store.UpdateUserDataFor(ctx, Subject{UserID: restricted}, episodeB.ID, UserDataPatch{
				PlaybackPositionTicks: &position, PlayCount: &playCount,
			})
			if err != nil || playback.PlaybackPositionTicks != position || playback.PlayCount != playCount {
				t.Fatalf("seed real playback state: %+v (%v)", playback, err)
			}
			aPlayback, err := store.UpdateUserDataFor(ctx, Subject{UserID: viewer}, episodeA.ID, UserDataPatch{
				PlaybackPositionTicks: &position, PlayCount: &playCount,
			})
			if err != nil || aPlayback.PlaybackPositionTicks != position || aPlayback.PlayCount != playCount {
				t.Fatalf("seed playback state for the warned series: %+v (%v)", aPlayback, err)
			}
			checkPlayback = func() {
				t.Helper()
				file, source, err := store.OpenMedia(ctx, viewer, episodeA.ID, "")
				if err != nil {
					t.Fatalf("ordinary viewer lost original playback authorization: %v", err)
				}
				if closeErr := file.Close(); closeErr != nil || source.Item.ID != episodeA.ID {
					t.Fatal("ordinary viewer's media source changed")
				}
				file, _, err = store.OpenMedia(ctx, restricted, episodeA.ID, "")
				if file != nil {
					_ = file.Close()
				}
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("restricted viewer should not receive the warned series' original: %v", err)
				}
				current, err := store.GetUserData(ctx, viewer, episodeA.ID)
				if err != nil || !reflect.DeepEqual(current, aPlayback) {
					t.Fatalf("watch state for the warned series changed: %+v (%v)", current, err)
				}
			}
			checkPlayback()

			strong := SeriesNFOAuditWarning{ItemID: a.ID, Code: "shared_declaration_disagreement", CorroboratingItemID: b.ID}
			weak := SeriesNFOAuditWarning{ItemID: a.ID, Code: "title_year_disagreement"}
			checkAudit("accepted, distinct documents", 2, strong)

			// Change only the peer's XML bytes, keeping all its claims and its file
			// size. Neither this nor the blank case has a new scan/import checkpoint.
			if err := os.WriteFile(sidecarB, []byte(strings.Replace(seriesNFOAuditTestDocB, "\n", " ", 1)), 0600); err != nil {
				t.Fatal(err)
			}
			checkAudit("byte-only peer mutation without scan", 1, weak)
			if err := os.WriteFile(sidecarB, nil, 0600); err != nil {
				t.Fatal(err)
			}
			checkAudit("blank peer without scan", 1, weak)

			// Only the normal scan/worker may confirm damage and request a repair.
			damaged := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			importLocal()
			assertEpisodePairPending(t, ctx, pool, b.ID, damaged.ID, true)
			recoveryTestStatus(t, ctx, pool, b.ID, "pending", "empty", 0)
			checkAudit("confirmed damage", 1, weak)
			progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil)
			if err != nil || progress.Updated != 1 || seriesNFOAuditTestBytes(t, sidecarB) != seriesNFOAuditTestDocB {
				t.Fatalf("normal repair did not restore the accepted peer document: %+v (%v)", progress, err)
			}
			recoveryTestStatus(t, ctx, pool, b.ID, "awaiting_import", "empty", 1)
			if _, err := store.GetItem(ctx, restricted, episodeB.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("repaired but unimported source lifted the pending visibility gate: %v", err)
			}
			checkAudit("repaired bytes still pending import", 1, weak)
			importLocal()
			assertEpisodePairPending(t, ctx, pool, b.ID, "", false)
			checkAudit("reimported repair", 2, strong)
			after, err := store.GetUserData(ctx, restricted, episodeB.ID)
			if err != nil || !reflect.DeepEqual(after, playback) {
				t.Fatalf("playback state after repair and audit = %+v (%v); want %+v", after, err, playback)
			}
		})
	}
}

func TestAuditSeriesNFORegisteredSeriesRootAndUnsettledDeletion(t *testing.T) {
	ctx, pool, store, approved, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	actor := metadataEditTestActor(t, ctx, pool, "series-audit-root-admin")
	seriesNFOAuditTestFiles(t, approved, seriesNFOAuditTestLeafA, seriesNFOAuditTestDocA)
	root := filepath.Join(approved, seriesNFOAuditTestLeafA)
	collection := seriesNFOAuditTestLibrary(t, ctx, store, actor, "Audit a single series root", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	series := libraryIntegrationQuery(t, ctx, store, Query{UserID: viewer, ParentID: collection.ID, Recursive: true,
		IncludeItemTypes: []string{"Series"}})
	if len(series.Items) != 1 || series.Items[0].Path != "" {
		t.Fatalf("want the root-backed representative, got %+v", series)
	}
	itemID := series.Items[0].ID
	requireSeriesNFOAudit(t, ctx, store, actor, collection.ID, "observed", 1, 1,
		SeriesNFOAuditWarning{ItemID: itemID, Code: "title_year_disagreement"})

	for _, state := range []string{"prepared", "catalog_removed"} {
		if _, err := pool.Exec(ctx, `INSERT INTO media_deletion_operations
			(id,item_id,library_id,root_id,actor_id,credential_id,origin_host,kind,state,source_snapshot)
			SELECT 'audit-deletion','media-being-deleted',$1,r.id,$2,$3,repeat('0',64),'media',$4,'{}'
			FROM library_roots r WHERE r.library_id=$1`, collection.ID, actor.User.ID, actor.SessionID, state); err != nil {
			t.Fatal(err)
		}
		requireSeriesNFOAudit(t, ctx, store, actor, collection.ID, "scan_unsettled", 0, 0)
		if _, err := pool.Exec(ctx, `DELETE FROM media_deletion_operations WHERE id='audit-deletion'`); err != nil {
			t.Fatal(err)
		}
	}

	// Overflow is explicit and must not return the warning for whichever item
	// happened to fit into the bounded population.
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,is_folder)
		SELECT 'audit-overflow-'||n,$1,r.id,$1,'Overflow','overflow','Series',r.path||'/Series-'||n,'Series-'||n,true
		FROM library_roots r CROSS JOIN generate_series(1,$2::int) n WHERE r.library_id=$1`, collection.ID, maxSeriesNFOAuditItems); err != nil {
		t.Fatalf("seed the audit population cap: %v", err)
	}
	requireSeriesNFOAudit(t, ctx, store, actor, collection.ID, "limit_exceeded", 0, 0)
}

func TestAuditSeriesNFOIsolatesLibrariesAndRequiresCurrentAdministrator(t *testing.T) {
	ctx, pool, store, approved, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	actor := metadataEditTestActor(t, ctx, pool, "series-audit-scope-admin")
	sourceRoot, peerRoot := filepath.Join(approved, "source"), filepath.Join(approved, "peer")
	sidecar, _ := seriesNFOAuditTestFiles(t, sourceRoot, seriesNFOAuditTestLeafA, seriesNFOAuditTestDocA)
	seriesNFOAuditTestFiles(t, peerRoot, seriesNFOAuditTestLeafB, seriesNFOAuditTestDocB)
	source := seriesNFOAuditTestLibrary(t, ctx, store, actor, "Audit source scope", sourceRoot)
	peer := seriesNFOAuditTestLibrary(t, ctx, store, actor, "Audit other scope", peerRoot)

	requireSeriesNFOAudit(t, ctx, store, actor, source.ID, "scan_unsettled", 0, 0)
	libraryIntegrationScan(t, ctx, store, peer.ID, "Completed")
	libraryIntegrationScan(t, ctx, store, source.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, viewer, source.ID, filepath.Dir(sidecar))
	requireSeriesNFOAudit(t, ctx, store, actor, peer.ID, "observed", 1, 1)
	requireSeriesNFOAudit(t, ctx, store, actor, source.ID, "observed", 1, 1,
		SeriesNFOAuditWarning{ItemID: item.ID, Code: "title_year_disagreement"})

	current, err := store.GetLibrary(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, source.ID, LibraryUpdate{
		Revision: current.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableLocalMetadata: &disabled},
	}); err != nil {
		t.Fatal(err)
	}
	requireSeriesNFOAudit(t, ctx, store, actor, source.ID, "local_metadata_disabled", 0, 0)

	// A principal whose current database role changed must not get an audit.
	if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, actor.User.ID); err != nil {
		t.Fatal(err)
	}
	if report, err := store.AuditSeriesNFO(ctx, actor, peer.ID); !errors.Is(err, ErrForbidden) || report.LibraryID != "" ||
		report.Status != "" || report.SeriesCount != 0 || report.ObservedCount != 0 || len(report.Warnings) != 0 {
		t.Fatalf("audit admitted or disclosed a report to a demoted administrator: %+v (%v)", report, err)
	}
}
