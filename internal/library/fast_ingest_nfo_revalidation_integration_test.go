//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

// Inject once, after the scan has observed the candidate and entered its
// publication transaction. Remember how that transaction actually ended.
type fastIngestNFOTrace struct {
	mu       sync.Mutex
	owner    *pgx.Conn
	matches  func(pgx.TraceQueryStartData) bool
	mutate   func() error
	injected bool
	err      error
	outcome  string
}

func (trace *fastIngestNFOTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if conn == trace.owner && !trace.injected && trace.matches(data) {
		trace.injected = true
		if conn.PgConn().TxStatus() != 'T' {
			trace.err = errors.New("sidecar mutation did not run inside the owned transaction")
		} else {
			trace.err = trace.mutate()
		}
	}
	return ctx
}

func (trace *fastIngestNFOTrace) TraceQueryEnd(_ context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if conn != trace.owner || !trace.injected || trace.outcome != "" {
		return
	}
	if tag := data.CommandTag.String(); tag == "ROLLBACK" || tag == "COMMIT" {
		trace.outcome = tag
		if data.Err != nil || conn.PgConn().TxStatus() != 'I' {
			trace.err = errors.Join(trace.err, fmt.Errorf("%s did not close the owned transaction: %v", tag, data.Err))
		}
	}
}

func fastIngestNFOTracedStore(t *testing.T, trace *fastIngestNFOTrace) (context.Context, *pgxpool.Pool, *Store, string, string) {
	t.Helper()
	prober := forceProbeVersionedFunc(versionedEpisodePairTestProbe)
	ctx, pool, previous, approved, viewer := libraryIntegrationStore(t, prober)
	if err := previous.Close(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := pool.Config()
	cfg.ConnConfig.Tracer = trace
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, traced)
	store, err := New(traced, prober, []string{approved}, WithDeferredLocalMetadata())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("close traced library store: %v", err)
		}
	})
	return ctx, traced, store, approved, viewer
}

func createFastIngestNFOLibrary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, collectionType, root string, images bool) Library {
	t.Helper()
	actor := metadataEditTestActor(t, ctx, pool, "warm-nfo-editor")
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
		"Warm NFO revalidation", collectionType, []string{root}, LibraryOptions{EnableLocalMetadata: true, EnableLocalImages: images})
	if err != nil {
		t.Fatal(err)
	}
	return collection
}

func assertFastIngestNFOPhases(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID, jobID string, wantNFO, wantImages bool) {
	t.Helper()
	var generation string
	var nfo, images bool
	err := pool.QueryRow(ctx, `SELECT scan_job_id,import_nfo,import_images FROM local_metadata_ingest WHERE item_id=$1`, itemID).
		Scan(&generation, &nfo, &images)
	if errors.Is(err, pgx.ErrNoRows) && !wantNFO && !wantImages {
		return
	}
	if err != nil || generation != jobID || nfo != wantNFO || images != wantImages {
		t.Fatalf("item %s ingest phases = (job %q, NFO %t, images %t), want (job %q, NFO %t, images %t); error=%v",
			itemID, generation, nfo, images, jobID, wantNFO, wantImages, err)
	}
}

func assertFastIngestNFORetained(t *testing.T, before, after Item) {
	t.Helper()
	if after.ID != before.ID || after.Type != before.Type || after.ParentID != before.ParentID || after.Name != before.Name ||
		after.Overview != before.Overview || !reflect.DeepEqual(after.Metadata, before.Metadata) {
		t.Fatalf("warm scan changed the accepted projection: before=%+v after=%+v", before, after)
	}
}

func primeFastIngestNFOWarmScan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, collection Library, viewer, path, document string) Item {
	t.Helper()
	images := EffectiveLibraryOptions(collection).EnableLocalImages
	first := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, viewer, collection.ID, path)
	assertFastIngestNFOPhases(t, ctx, pool, item.ID, first.ID, true, images)
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatalf("accept the baseline sidecar state: %v", err)
	}
	accepted := nfoCatalogItem(t, ctx, store, viewer, collection.ID, path)
	if document == "" {
		if accepted.Name != "Film" || accepted.Metadata != nil {
			t.Fatalf("verified absence did not retain the filename projection: %+v", accepted)
		}
	} else {
		nfoAssertMetadata(t, accepted, nfoExpectedMetadata(t, document))
	}
	assertEpisodePairPending(t, ctx, pool, accepted.ID, "", false)

	// Prove that this fixture qualifies for the warm fast path. Without this
	// control, always queueing an NFO could mask a missing final recheck.
	warm := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if warm.Scanned != 1 || warm.Added != 0 || warm.Updated != 0 || warm.Error != "" {
		t.Fatalf("unchanged warm scan did not reuse the existing catalog item: %+v", warm)
	}
	assertFastIngestNFOPhases(t, ctx, pool, accepted.ID, warm.ID, false, images)
	assertFastIngestNFORetained(t, accepted, nfoCatalogItem(t, ctx, store, viewer, collection.ID, path))
	return accepted
}

func changeFastIngestNFOFixture(path, replacement string, present bool) error {
	if !present {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = file.WriteString(replacement)
		return errors.Join(err, file.Close())
	}
	before, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := rewriteIngestFixture(path, []byte(replacement)); err != nil {
		return err
	}
	after, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return errors.New("sidecar rewrite did not preserve inode, size and mtime")
	}
	if media.FileChangeTime(before) == media.FileChangeTime(after) {
		return errors.New("sidecar rewrite did not advance ctime")
	}
	return nil
}

func TestFastIngestNFORechecksWarmProofAtPublication(t *testing.T) {
	const (
		oldMovie = `<movie><title>Old Film</title><mpaa>PG</mpaa></movie>`
		newMovie = `<movie><title>New Film</title><mpaa>PG</mpaa></movie>`
		oldShow  = `<tvshow><title>Old Show</title><mpaa>PG</mpaa></tvshow>`
		newShow  = `<tvshow><title>New Show</title><mpaa>PG</mpaa></tvshow>`
	)
	for _, tc := range []struct {
		name, collectionType, video, selected, mutated, before, after string
		images                                                        bool
	}{
		// With images disabled, the NFO proof itself must open a cached transaction.
		{"cached absence", "movies", "Film.mp4", "", "Film.nfo", "", newMovie, false},
		{"preferred appears over fallback", "movies", "Film.mp4", "movie.nfo", "Film.nfo", oldMovie, oldMovie, true},
		{"cached preferred rewritten with restored mtime", "movies", "Film.mp4", "Film.nfo", "Film.nfo", oldMovie, newMovie, true},
		{"physical series parent", "tvshows", "Show/Season 01/Show.S01E01.mp4", "Show/tvshow.nfo", "Show/tvshow.nfo", oldShow, newShow, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trace := &fastIngestNFOTrace{}
			ctx, pool, store, approved, viewer := fastIngestNFOTracedStore(t, trace)
			root := filepath.Join(approved, "media")
			video := libraryIntegrationFile(t, root, tc.video, "video:warm-nfo")
			if tc.selected != "" {
				libraryIntegrationFile(t, root, tc.selected, tc.before)
			}
			path, kind := video, "Movie"
			if tc.collectionType == "tvshows" {
				path, kind = filepath.Join(root, "Show"), "Series"
			}
			collection := createFastIngestNFOLibrary(t, ctx, pool, store, tc.collectionType, root, tc.images)
			accepted := primeFastIngestNFOWarmScan(t, ctx, pool, store, collection, viewer, path, tc.before)
			if accepted.Type != kind || accepted.IsFolder != (kind == "Series") || accepted.ParentID != collection.ID {
				t.Fatalf("fixture did not use the expected physical hierarchy: %+v", accepted)
			}

			trace.mu.Lock()
			trace.owner = store.ownership.conn.Conn()
			trace.matches = func(data pgx.TraceQueryStartData) bool {
				if accepted.IsFolder {
					return strings.HasPrefix(data.SQL, "INSERT INTO items") && len(data.Args) >= 9 &&
						data.Args[6] == "Series" && data.Args[7] == accepted.Path && data.Args[8] == "Show"
				}
				return strings.HasPrefix(data.SQL, "SELECT file_identity=$5 AND file_size=$6") &&
					len(data.Args) >= 1 && data.Args[0] == accepted.ID
			}
			trace.mutate = func() error {
				return changeFastIngestNFOFixture(filepath.Join(root, tc.mutated), tc.after, tc.selected == tc.mutated)
			}
			trace.mu.Unlock()

			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			trace.mu.Lock()
			injected, mutationErr, outcome := trace.injected, trace.err, trace.outcome
			trace.mu.Unlock()
			if !injected || mutationErr != nil || outcome != "ROLLBACK" {
				t.Fatalf("stale publication did not roll back: injected=%t error=%v transaction=%q", injected, mutationErr, outcome)
			}
			if job.Scanned != 1 || job.Added != 0 || job.Updated != 0 {
				t.Fatalf("rejecting the stale NFO changed the accepted primary visit: %+v", job)
			}
			assertFastIngestNFOPhases(t, ctx, pool, accepted.ID, job.ID, true, tc.images)
			assertFastIngestNFORetained(t, accepted, nfoCatalogItem(t, ctx, store, viewer, collection.ID, path))
		})
	}
}

func TestFastIngestNFODirectoryReplacementWithSameVideoLeavesPolicyPending(t *testing.T) {
	trace := &fastIngestNFOTrace{}
	ctx, pool, store, approved, viewer := fastIngestNFOTracedStore(t, trace)
	root := filepath.Join(approved, "movies")
	video := libraryIntegrationFile(t, root, "Film/Film.mp4", "video:hard-linked-fixture")
	const acceptedNFO = `<movie><title>Accepted Film</title><mpaa>PG</mpaa></movie>`
	libraryIntegrationFile(t, root, "Film/Film.nfo", acceptedNFO)
	replacement := filepath.Dir(libraryIntegrationFile(t, approved, "replacement/Film.nfo", `<movie><title>Restricted Film</title><mpaa>R</mpaa></movie>`))
	// Create both names before the cold scan so creating the link does not
	// advance the media inode's ctime after its first accepted observation.
	if err := os.Link(video, filepath.Join(replacement, "Film.mp4")); err != nil {
		t.Fatal(err)
	}
	collection := createFastIngestNFOLibrary(t, ctx, pool, store, "movies", root, false)
	accepted := primeFastIngestNFOWarmScan(t, ctx, pool, store, collection, viewer, video, acceptedNFO)
	const restricted = "directory-replacement-viewer"
	libraryIntegrationUser(t, ctx, pool, restricted, false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5}'::jsonb WHERE id=$1`, restricted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, restricted, accepted.ID); err != nil {
		t.Fatal(err)
	}
	trace.mu.Lock()
	trace.owner = store.ownership.conn.Conn()
	trace.matches = func(data pgx.TraceQueryStartData) bool {
		return strings.HasPrefix(data.SQL, "SELECT file_identity=$5 AND file_size=$6") && len(data.Args) > 0 && data.Args[0] == accepted.ID
	}
	trace.mutate = func() error {
		before, err := os.Stat(video)
		if err != nil {
			return err
		}
		if err := os.Rename(filepath.Dir(video), filepath.Join(approved, "old-directory")); err != nil {
			return err
		}
		if err := os.Rename(replacement, filepath.Dir(video)); err != nil {
			return err
		}
		after, err := os.Stat(video)
		if err != nil || !sameMediaSourceFile(before, after) {
			return errors.Join(errors.New("fixture changed the media identity"), err)
		}
		return nil
	}
	trace.mu.Unlock()
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	trace.mu.Lock()
	injected, mutationErr, outcome := trace.injected, trace.err, trace.outcome
	trace.mu.Unlock()
	if !injected || mutationErr != nil || outcome != "ROLLBACK" {
		t.Fatalf("changed NFO directory did not roll back: injected=%t error=%v transaction=%q", injected, mutationErr, outcome)
	}
	assertFastIngestNFOPhases(t, ctx, pool, accepted.ID, job.ID, true, false)
	assertFastIngestNFORetained(t, accepted, nfoCatalogItem(t, ctx, store, viewer, collection.ID, video))
	if _, err := store.GetItem(ctx, restricted, accepted.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replaced NFO directory left the old rating authorized: %v", err)
	}
}

func TestFastIngestNFOWarmProofCanRetainOptedInRecoveryOriginals(t *testing.T) {
	ctx, pool, store, approved, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	store.deferLocalMetadata = true
	root := filepath.Join(approved, "tv")
	series := filepath.Join(root, "Show")
	const show = `<tvshow><title>Accepted Show</title><mpaa>PG</mpaa></tvshow>`
	const episode = `<episodedetails><title>Accepted Episode</title><season>1</season><episode>1</episode></episodedetails>`
	path := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E01.mp4", "video:recovery")
	libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E01.nfo", episode)
	libraryIntegrationFile(t, root, "Show/tvshow.nfo", show)
	collection := createFastIngestNFOLibrary(t, ctx, pool, store, "tvshows", root, false)
	accepted := primeFastIngestNFOWarmScan(t, ctx, pool, store, collection, viewer, path, episode)
	parent := nfoCatalogItem(t, ctx, store, viewer, collection.ID, series)
	for _, id := range []string{accepted.ID, parent.ID} {
		var haveCopy bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_nfo_recovery_sources WHERE item_id=$1)`, id).Scan(&haveCopy); err != nil || haveCopy {
			t.Fatalf("unexpected copy before opting in to recovery for %s: %t %v", id, haveCopy, err)
		}
	}
	store.nfoRecoveryRoots = map[string]struct{}{root: {}}
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	for id, want := range map[string]string{accepted.ID: episode, parent.ID: show} {
		assertFastIngestNFOPhases(t, ctx, pool, id, job.ID, false, false)
		var saved []byte
		if err := pool.QueryRow(ctx, `SELECT document FROM local_nfo_recovery_sources WHERE item_id=$1 AND scan_job_id=$2`, id, job.ID).
			Scan(&saved); err != nil || string(saved) != want {
			t.Fatalf("warm revalidation did not retain the opted-in original for %s: %v", id, err)
		}
	}
}

func TestFastIngestNFOLeavesExistingWorkPending(t *testing.T) {
	ctx, pool, store, approved, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	store.deferLocalMetadata = true
	root := filepath.Join(approved, "movies")
	path := libraryIntegrationFile(t, root, "Film.mp4", "video:pending-nfo")
	const acceptedNFO = `<movie><title>Accepted Film</title><!--A--></movie>`
	const changedNFO = `<movie><title>Accepted Film</title><!--B--></movie>`
	nfo := libraryIntegrationFile(t, root, "Film.nfo", acceptedNFO)
	collection := createFastIngestNFOLibrary(t, ctx, pool, store, "movies", root, true)
	accepted := primeFastIngestNFOWarmScan(t, ctx, pool, store, collection, viewer, path, acceptedNFO)
	checkPending := func() {
		t.Helper()
		job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
		assertFastIngestNFOPhases(t, ctx, pool, accepted.ID, job.ID, true, true)
		assertFastIngestNFORetained(t, accepted, nfoCatalogItem(t, ctx, store, viewer, collection.ID, path))
	}

	// The selected path and parsed facts are identical, but the bytes changed.
	if err := changeFastIngestNFOFixture(nfo, changedNFO, true); err != nil {
		t.Fatal(err)
	}
	checkPending()
	// Restoring the accepted bytes does not let the warm path retire old work.
	if err := changeFastIngestNFOFixture(nfo, acceptedNFO, true); err != nil {
		t.Fatal(err)
	}
	checkPending()
	if _, err := pool.Exec(ctx, `UPDATE libraries SET options=jsonb_set(options,'{EnableLocalMetadata}','false'::jsonb),
		revision=revision+1 WHERE id=$1`, collection.ID); err != nil {
		t.Fatal(err)
	}
	checkPending()
}
