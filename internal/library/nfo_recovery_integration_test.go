//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func recoveryTestRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, collection Library,
	guard func() error) (ProviderProgress, error) {
	t.Helper()
	child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskNFORecoveryKey)
	if guard != nil {
		calls := 0
		base := fence
		fence = func(tx OwnedTx) error {
			if err := base(tx); err != nil {
				return err
			}
			calls++
			// Four startup fences and the reservation pass precede admission
			// to the final publication transaction. The temporary is closed.
			if calls == 6 {
				return guard()
			}
			return nil
		}
	}
	var progress ProviderProgress
	result := store.RegenerateLocalNFO(ctx, child, collection.ID, fence, func(value ProviderProgress) error {
		progress = value
		return nil
	})
	if _, err := pool.Exec(ctx, `DELETE FROM task_runs WHERE id=(SELECT run_id FROM task_run_children WHERE id=$1)`, child); err != nil {
		t.Fatal(err)
	}
	return progress, result
}

func recoveryTestStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, status, reason string, attempts int) {
	t.Helper()
	var foundStatus, foundReason string
	var foundAttempts int
	if err := pool.QueryRow(ctx, `SELECT status,reason,attempts FROM local_nfo_recovery_work WHERE item_id=$1`, id).
		Scan(&foundStatus, &foundReason, &foundAttempts); err != nil || foundStatus != status || foundReason != reason || foundAttempts != attempts {
		t.Fatalf("work for %s = %q %q %d (%v), want %q %q %d", id, foundStatus, foundReason, foundAttempts, err, status, reason, attempts)
	}
}

func TestNFORecoveryRegeneratesOnlyBoundOriginalsAndRetainsAccessGate(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferred=%t", deferred), func(t *testing.T) {
			ctx, pool, store, approved, user := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
			root := filepath.Join(approved, "tv")
			store.nfoRecoveryRoots = map[string]struct{}{root: {}}
			store.deferLocalMetadata = deferred
			seasonDirectory := filepath.Join(root, "Show", "Season 01")
			seasonNFO := `<season><seasonnumber>1</seasonnumber><title>Season from NFO</title></season>`
			seasonSidecar := libraryIntegrationFile(t, root, "Show/Season 01/season.nfo", seasonNFO)
			bad := "<episodedetails><season>1</season><episode>9</episode><title>Wrong episode</title></episodedetails>\x00"
			cases := []struct {
				leaf, document, title string
				itemPath, sidecar, id string
			}{
				{leaf: "Show.S01E02", document: pairedTestNFO(2, "Safe", "PG", "Family"), title: "Safe"},
				{leaf: "Show.S01E03", document: pairedTestNFO(3, "Restricted", "R", "Restricted"), title: "Restricted"},
				{leaf: "Show.S01E04"},
				{leaf: "Show.S01E05E06", document: pairedTestNFO(5, "First", "PG", "Family") + pairedTestNFO(6, "Second", "PG", "Family"), title: "First,  Second"},
			}
			for i := range cases {
				c := &cases[i]
				c.itemPath = libraryIntegrationFile(t, root, "Show/Season 01/"+c.leaf+".mp4", "video "+c.leaf)
				c.sidecar = filepath.Join(seasonDirectory, c.leaf+".nfo")
				if c.document != "" {
					writeEpisodePairTestNFO(t, c.sidecar, c.document)
				}
			}
			collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
			libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if deferred {
				if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
					t.Fatal(err)
				}
			}
			season := nfoCatalogItem(t, ctx, store, user, collection.ID, seasonDirectory)
			for i := range cases {
				c := &cases[i]
				initial := nfoCatalogItem(t, ctx, store, user, collection.ID, c.itemPath)
				c.id = initial.ID
				if c.title != "" && initial.Name != c.title {
					t.Fatalf("initial document not accepted: %q", initial.Name)
				}
				writeEpisodePairTestNFO(t, c.sidecar, bad)
			}
			const restricted = "rating-limited"
			libraryIntegrationUser(t, ctx, pool, restricted, false, true, nil)
			if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5,"BlockedTags":["Restricted"]}'::jsonb WHERE id=$1`, restricted); err != nil {
				t.Fatal(err)
			}
			position := int64(123456)
			if _, err := store.UpdateUserDataFor(ctx, Subject{UserID: restricted}, cases[0].id, UserDataPatch{PlaybackPositionTicks: &position}); err != nil {
				t.Fatal(err)
			}
			writeEpisodePairTestNFO(t, seasonSidecar, "")
			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			for _, c := range cases {
				assertEpisodePairPending(t, ctx, pool, c.id, job.ID, true)
			}
			assertEpisodePairPending(t, ctx, pool, season.ID, job.ID, true)
			if _, err := store.GetItem(ctx, restricted, cases[0].id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("pending recovery bypassed rating policy: %v", err)
			}
			for range 2 {
				if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
					t.Fatal(err)
				}
				for _, c := range cases {
					recoveryTestStatus(t, ctx, pool, c.id, "pending", "malformed", 0)
				}
				recoveryTestStatus(t, ctx, pool, season.ID, "pending", "empty", 0)
				var sequence int
				if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='NFORecoveryRequested'`).Scan(&sequence); err != nil || sequence != 5 {
					t.Fatalf("repeat observation created new work: sequence=%d err=%v", sequence, err)
				}
			}
			progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil)
			if err == nil || !strings.Contains(err.Error(), "no trusted recovery document") || progress.Processed != 5 || progress.Updated != 4 {
				t.Fatalf("unexpected regeneration outcome: %+v %v", progress, err)
			}
			for _, c := range cases {
				want, status := c.document, "awaiting_import"
				if want == "" {
					want, status = bad, "no_source"
				}
				body, err := os.ReadFile(c.sidecar)
				if err != nil || string(body) != want {
					t.Fatalf("recovery for %s produced wrong bytes: %v", c.leaf, err)
				}
				recoveryTestStatus(t, ctx, pool, c.id, status, "malformed", 1)
				assertEpisodePairPending(t, ctx, pool, c.id, job.ID, true)
			}
			if body, err := os.ReadFile(seasonSidecar); err != nil || string(body) != seasonNFO {
				t.Fatalf("season recovery did not restore its own document: %v", err)
			}
			if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
				t.Fatal(err)
			}
			assertEpisodePairPending(t, ctx, pool, season.ID, "", false)
			for _, c := range cases {
				assertEpisodePairPending(t, ctx, pool, c.id, "", c.document == "")
				current := nfoCatalogItem(t, ctx, store, user, collection.ID, c.itemPath)
				if current.ID != c.id || current.ParentID != season.ID || c.title != "" && current.Name != c.title || c.title == "" && current.IndexNumber != 4 {
					t.Fatalf("regeneration changed item identity or applied wrong episode: %+v", current)
				}
			}
			if _, err := store.GetItem(ctx, restricted, cases[0].id); err != nil {
				t.Fatalf("verified PG episode remained hidden: %v", err)
			}
			for _, i := range []int{1, 2} {
				if _, err := store.GetItem(ctx, restricted, cases[i].id); !errors.Is(err, ErrNotFound) {
					t.Fatalf("restricted or unverified episode became visible: %v", err)
				}
			}
			var playback int64
			if err := pool.QueryRow(ctx, `SELECT playback_position_ticks FROM user_item_data WHERE user_id=$1 AND item_id=$2`, restricted, cases[0].id).
				Scan(&playback); err != nil || playback != position {
				t.Fatalf("playback position changed: %v %d", err, playback)
			}
			if progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil); err != nil || progress.Processed != 0 {
				t.Fatalf("terminal damage retried: %+v %v", progress, err)
			}
		})
	}
}

func TestNFORecoveryPreservesReplacementAndPersistsRetryBudget(t *testing.T) {
	ctx, pool, store, approved, user := libraryIntegrationStore(t, forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		return forceProbeTestInfo(file)
	}))
	root := filepath.Join(approved, "movies")
	store.nfoRecoveryRoots = map[string]struct{}{root: {}}
	video := libraryIntegrationFile(t, root, "Film.mp4", "video")
	sidecar := libraryIntegrationFile(t, root, "Film.nfo", `<movie><title>Original</title></movie>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Resilient recovery", "movies", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, user, collection.ID, video)
	writeEpisodePairTestNFO(t, sidecar, "")
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			if _, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work SET retry_at=clock_timestamp() WHERE item_id=$1 AND status='pending'`, item.ID); err != nil {
				t.Fatal(err)
			}
		}
		_, err := recoveryTestRun(t, ctx, pool, store, collection, func() error { return errMetadataIngestChanged })
		if err == nil {
			t.Fatal("failed publication was reported successful")
		}
		status := "pending"
		if attempt == 3 {
			status = "verify_outcome"
		}
		recoveryTestStatus(t, ctx, pool, item.ID, status, "empty", attempt)
		if body, err := os.ReadFile(sidecar); err != nil || len(body) != 0 {
			t.Fatalf("failed attempt overwrote original or removed it: %q %v", body, err)
		}
		if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
			t.Fatal(err)
		}
		recoveryTestStatus(t, ctx, pool, item.ID, status, "empty", attempt)
		if progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil); err != nil || progress.Processed != 0 {
			t.Fatalf("retry before due or after exhaustion: %+v %v", progress, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work SET retry_at=clock_timestamp() WHERE item_id=$1 AND status='verify_outcome'`, item.ID); err != nil {
		t.Fatal(err)
	}
	if progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil); err == nil || progress.Processed != 1 || progress.Updated != 0 {
		t.Fatalf("final observation retried the write: %+v %v", progress, err)
	}
	recoveryTestStatus(t, ctx, pool, item.ID, "retry_exhausted", "empty", 3)
	writeEpisodePairTestNFO(t, sidecar, "<movie><title>Incomplete")
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	const replacement = `<movie><title>External repair</title></movie>`
	mutated := false
	if _, err := recoveryTestRun(t, ctx, pool, store, collection, func() error {
		mutated = true
		return os.WriteFile(sidecar, []byte(replacement), 0600)
	}); err == nil || !mutated {
		t.Fatalf("final proof did not notice replacement: changed=%v err=%v", mutated, err)
	}
	if body, err := os.ReadFile(sidecar); err != nil || string(body) != replacement {
		t.Fatalf("regeneration overwrote a new valid file: %q %v", body, err)
	}
	assertEpisodePairPending(t, ctx, pool, item.ID, "", true)
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	if current := nfoCatalogItem(t, ctx, store, user, collection.ID, video); current.ID != item.ID || current.Name != "External repair" {
		t.Fatalf("the external replacement was not normally imported: %+v", current)
	}
	assertEpisodePairPending(t, ctx, pool, item.ID, "", false)
}

func TestNFORecoveryReobservesAfterPublicationCommitFailure(t *testing.T) {
	ctx, pool, store, approved, user := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "tv")
	store.nfoRecoveryRoots = map[string]struct{}{root: {}}
	video := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E02.mp4", "video")
	sidecar := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E02.nfo", pairedTestNFO(2, "Original", "PG", "Family"))
	collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, user, collection.ID, video)
	writeEpisodePairTestNFO(t, sidecar, "")
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work SET attempts=2 WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_nfo_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'test NFO publication commit failure'; END $$;
		CREATE CONSTRAINT TRIGGER reject_nfo_commit AFTER UPDATE ON local_nfo_recovery_work DEFERRABLE INITIALLY DEFERRED
		FOR EACH ROW WHEN (NEW.status='awaiting_import') EXECUTE FUNCTION reject_nfo_commit();`); err != nil {
		t.Fatal(err)
	}
	if progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil); err == nil || progress.Updated != 0 {
		t.Fatalf("failed commit reported completed recovery: %+v %v", progress, err)
	}
	assertEpisodePairPending(t, ctx, pool, item.ID, "", true)
	recoveryTestStatus(t, ctx, pool, item.ID, "verify_outcome", "empty", 3)
	if data, err := os.ReadFile(sidecar); err != nil || string(data) != pairedTestNFO(2, "Original", "PG", "Family") {
		t.Fatalf("expected the actual replacement before failed commit: %v", err)
	}
	visible, err := os.Stat(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER reject_nfo_commit ON local_nfo_recovery_work`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work SET retry_at=clock_timestamp() WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := recoveryTestRun(t, ctx, pool, store, collection, func() error { return errMetadataIngestChanged }); err == nil {
		t.Fatal("interrupted verification reported a completed observation")
	}
	recoveryTestStatus(t, ctx, pool, item.ID, "verify_outcome", "empty", 3)
	if progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil); err != nil || progress.Processed != 0 {
		t.Fatalf("interrupted verification lost its read-only backoff: %+v %v", progress, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work SET retry_at=clock_timestamp() WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil); err != nil || progress.Processed != 1 || progress.Updated != 0 {
		t.Fatalf("re-observation rewrote the accepted bytes: %+v %v", progress, err)
	}
	if current, err := os.Stat(sidecar); err != nil || !sameLocalNFOFile(visible, current) {
		t.Fatalf("retry replaced an already published file: %v", err)
	}
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	assertEpisodePairPending(t, ctx, pool, item.ID, "", false)
}

func TestNFORecoveryDetectsDamageButDoesNotWriteWithoutRootOptIn(t *testing.T) {
	ctx, pool, store, approved, user := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "movies")
	video := libraryIntegrationFile(t, root, "Film.mp4", "video")
	sidecar := libraryIntegrationFile(t, root, "Film.nfo", "")
	collection := libraryIntegrationCreate(t, ctx, store, "Detect without write access", "movies", root)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, user, collection.ID, video)
	assertEpisodePairPending(t, ctx, pool, item.ID, job.ID, true)
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil)
	if err == nil || !strings.Contains(err.Error(), "outside authorized sidecar writes") || progress.Processed != 1 || progress.Updated != 0 {
		t.Fatalf("unconfigured write outcome: %+v %v", progress, err)
	}
	recoveryTestStatus(t, ctx, pool, item.ID, "write_disabled", "empty", 1)
	if data, err := os.ReadFile(sidecar); err != nil || len(data) != 0 {
		t.Fatalf("unconfigured executor touched the sidecar: %q %v", data, err)
	}
	assertEpisodePairPending(t, ctx, pool, item.ID, job.ID, true)
}

func TestNFORecoverySurvivesProbeRefreshButWaitsForMediaJournal(t *testing.T) {
	var indexed atomic.Bool
	ctx, pool, store, approved, user := libraryIntegrationStore(t, forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		info, err := forceProbeTestInfo(file)
		if err == nil && indexed.Load() {
			info.VideoSeekIndexes = []media.VideoSeekIndex{forceProbeTestIndex(info)}
		}
		return info, err
	}))
	root := filepath.Join(approved, "movies")
	store.nfoRecoveryRoots = map[string]struct{}{root: {}}
	video := libraryIntegrationFile(t, root, "Folder/Film.mp4", "video")
	const document = `<movie><title>Accepted before new probe evidence</title></movie>`
	sidecar := libraryIntegrationFile(t, root, "Folder/Film.nfo", document)
	collection := libraryIntegrationCreate(t, ctx, store, "Stable physical binding", "movies", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, user, collection.ID, video)
	if item.Media == nil || len(item.Media.VideoSeekIndexes) != 0 {
		t.Fatal("first scan unexpectedly had derived probe results")
	}
	writeEpisodePairTestNFO(t, sidecar, "")
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work SET attempts=1 WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	indexed.Store(true)
	forceProbeTestScan(t, ctx, store, collection.ID, "Completed")
	if after := nfoCatalogItem(t, ctx, store, user, collection.ID, video); after.ID != item.ID || after.Media == nil || len(after.Media.VideoSeekIndexes) != 1 {
		t.Fatal("forced probe did not change only the derived media evidence")
	}
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_deletion_operations
		(id,item_id,library_id,root_id,actor_id,credential_id,origin_host,kind,state,source_snapshot)
		SELECT 'op','possible-sibling',library_id,root_id,'actor','credential',repeat('a',64),'media','prepared','{}'
		FROM items WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil); err != nil || progress.Processed != 0 {
		t.Fatalf("pending media journal did not defer recovery: %+v %v", progress, err)
	}
	recoveryTestStatus(t, ctx, pool, item.ID, "pending", "empty", 1)
	if _, err := pool.Exec(ctx, `DELETE FROM media_deletion_operations WHERE id='op'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE local_metadata_ingest SET import_images=true WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	imagesFinished := false
	progress, err := recoveryTestRun(t, ctx, pool, store, collection, func() error {
		tag, err := pool.Exec(ctx, `UPDATE local_metadata_ingest SET import_images=false WHERE item_id=$1 AND import_nfo`, item.ID)
		imagesFinished = err == nil && tag.RowsAffected() == 1
		return err
	})
	if err != nil || progress.Processed != 1 || progress.Updated != 1 || !imagesFinished {
		t.Fatalf("derived media evidence or independent artwork invalidated recovery: %+v %v", progress, err)
	}
	if body, err := os.ReadFile(sidecar); err != nil || string(body) != document {
		t.Fatalf("restored NFO differs from the accepted original: %v", err)
	}
}

func TestNFORecoveryRechecksAChangedCandidateConflict(t *testing.T) {
	ctx, pool, store, approved, user := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "movies")
	store.nfoRecoveryRoots = map[string]struct{}{root: {}}
	video := libraryIntegrationFile(t, root, "Film.mp4", "video")
	const document = `<movie><title>One movie</title></movie>`
	sidecar := libraryIntegrationFile(t, root, "Film.nfo", document)
	collection := libraryIntegrationCreate(t, ctx, store, "Candidate owner", "movies", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, user, collection.ID, video)
	writeEpisodePairTestNFO(t, sidecar, "")
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
		t.Fatal(err)
	}
	sibling := libraryIntegrationFile(t, root, "Film.mkv", "unindexed contender")
	if _, err := recoveryTestRun(t, ctx, pool, store, collection, func() error { return os.Remove(sibling) }); err == nil {
		t.Fatal("a stale directory conflict was committed as terminal")
	}
	recoveryTestStatus(t, ctx, pool, item.ID, "pending", "empty", 1)
	if body, err := os.ReadFile(sidecar); err != nil || len(body) != 0 {
		t.Fatalf("changed observation modified the sidecar: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work SET retry_at=clock_timestamp() WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil); err != nil || progress.Processed != 1 || progress.Updated != 1 {
		t.Fatalf("recovery did not revisit the expired conflict: %+v %v", progress, err)
	}
}
