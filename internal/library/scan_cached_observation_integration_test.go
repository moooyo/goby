//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Observe the actual phase carried by each authority request. Counting SQL alone
// cannot establish that preparation and publication acquired fresh I/O grants.
type cachedObservationGrantTrace struct {
	armed       atomic.Bool
	owner       *pgx.Conn
	walk        *PrimaryRootIO
	grants      atomic.Int64
	invalid     atomic.Int64
	ownedInRead atomic.Int64
}

func (trace *cachedObservationGrantTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.armed.Load() {
		return ctx
	}
	if conn == trace.owner && originalMediaReadGovernor.Stats().Active != 0 {
		trace.ownedInRead.Add(1)
	}
	if data.SQL != primaryScanManualAuthoritySQL && data.SQL != primaryScanTaskAuthoritySQL {
		return ctx
	}
	phase, _ := ctx.Value(primaryRootIOPhaseKey{}).(*primaryRootIOPhase)
	if phase == nil || trace.walk == nil || phase.state != trace.walk.handle.state {
		trace.invalid.Add(1)
		return ctx
	}
	phase.state.mu.Lock()
	active := phase.state.active > 0 && phase.state.phase == phase
	phase.state.mu.Unlock()
	if !active || conn.PgConn().TxStatus() != 'I' {
		trace.invalid.Add(1)
		return ctx
	}
	trace.grants.Add(1)
	return ctx
}

func (*cachedObservationGrantTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestCachedScanObservationRunsWithinTwoFreshGrants(t *testing.T) {
	observer := &cachedObservationGrantTrace{}
	ctx, pool, store, state, trace, _ := scanCachedVisitFixture(t, observer)
	walk := primaryScanRoutingRetainWalk(t, state)
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	path := filepath.Join(state.root.path, "Film.mp4")
	beforeDescriptors := scanProbeOpenDescriptors(t, []string{path, state.root.path})
	prober := store.prober.(*libraryFixtureProber)
	beforeProbes := len(prober.calls())
	observer.owner, observer.walk = store.ownership.conn.Conn(), walk
	trace.reset()
	observer.armed.Store(true)
	if err := state.scanFile("Film.mp4", "video", hierarchy{parentID: state.library.ID}); err != nil {
		t.Fatal(err)
	}
	observer.armed.Store(false)
	primaryScanRoutingAssertAuthorityBudget(t, trace, 2)
	if observer.grants.Load() != 2 || observer.invalid.Load() != 0 || observer.ownedInRead.Load() != 0 {
		t.Fatalf("cached observation did not use two fresh phases outside owned SQL: grants=%d invalid=%d owned_in_read=%d",
			observer.grants.Load(), observer.invalid.Load(), observer.ownedInRead.Load())
	}
	if len(prober.calls()) != beforeProbes || state.warnings != 0 || trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 ||
		trace.begins.Load() != 1 || trace.commits.Load() != 1 || trace.cachedCompletionChecks.Load() != 1 {
		t.Fatalf("unchanged observation probed, wrote facts, or changed checkpoints: probes=%d/%d warnings=%d items=%d metadata=%d tx=%d/%d completion=%d",
			len(prober.calls()), beforeProbes, state.warnings, trace.itemRows.Load(), trace.metadataRows.Load(),
			trace.begins.Load(), trace.commits.Load(), trace.cachedCompletionChecks.Load())
	}
	job, err := store.GetJob(ctx, state.task.job.ID)
	if err != nil || job.Scanned != state.task.job.Scanned || job.Added != state.task.job.Added || job.Updated != state.task.job.Updated {
		t.Fatalf("cached observation lost accepted counters: job=%+v error=%v", job, err)
	}
	taskScanAssertChild(t, ctx, pool, state.task.job.TaskChildID, job)
	afterDescriptors := scanProbeOpenDescriptors(t, []string{path, state.root.path})
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners ||
		afterDescriptors[0] != beforeDescriptors[0] || afterDescriptors[1] != beforeDescriptors[1] {
		t.Fatalf("cached observation retained admission or descriptors: before=%+v after=%+v owners=%+v descriptors=%v/%v",
			beforeIO, stats, originalMediaReadOwners.Stats(), beforeDescriptors, afterDescriptors)
	}
}

// Both blockers own real common-governor background leases. Their release
// channels provide the boundary; the timeout only bounds fixture failure.
func cachedObservationBlockBackground(t *testing.T, ctx context.Context, state *scanState) (func(), func()) {
	t.Helper()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	blockers := make([]*primarySidecarRetryBlocker, 0, 2)
	for range 2 {
		operation, err := state.store.preparePrimaryRootIO(ctx,
			[]mediaSourceRootHint{{root: state.walkRow.root, bindingRevision: state.walkRow.revision}})
		if err != nil {
			t.Fatal(err)
		}
		blocker := &primarySidecarRetryBlocker{operation: operation, rootID: state.root.id,
			entered: make(chan struct{}), done: make(chan struct{})}
		blockers = append(blockers, blocker)
		t.Cleanup(func() {
			unblock()
			if blocker.started.Load() {
				mediaSourceAdmissionTestWait(t, blocker.done, "cached observation blocker cleanup")
			}
			if err := operation.Close(); err != nil {
				t.Errorf("close cached observation blocker: %v", err)
			}
		})
		blocker.start(ctx, release)
		select {
		case <-blocker.entered:
		case <-blocker.done:
			t.Fatalf("cached observation blocker failed to acquire its lease: %v", blocker.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Second):
			t.Fatal("cached observation blocker did not enter its lease")
		}
	}
	return unblock, func() {
		t.Helper()
		unblock()
		for _, blocker := range blockers {
			mediaSourceAdmissionTestWait(t, blocker.done, "cached observation blocker retirement")
			if blocker.err != nil {
				t.Fatalf("cached observation blocker failed: %v", blocker.err)
			}
			if err := blocker.operation.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCachedScanObservationRechecksAuthorityAfterQueuedPublication(t *testing.T) {
	for _, scenario := range []struct {
		name, statement string
		job             bool
		want            error
	}{
		{"task_cancel", "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", true, context.Canceled},
		{"binding_revision", "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", false, ErrRootBindingConflict},
		{"binding_document", `UPDATE library_roots SET storage_binding=jsonb_set(storage_binding, '{registered_root,handle}', '"Y2hhbmdlZA=="'::jsonb) WHERE id=$1`, false, ErrRootBindingConflict},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, pool, _, state, trace, _ := scanCachedVisitFixture(t)
			beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			walk := primaryScanRoutingRetainWalk(t, state)
			if scenario.name == "binding_document" && (!state.walkRow.stored || state.walkRow.document == nil) {
				t.Fatal("queued document fixture needs a stored root binding")
			}
			input, err := state.inspectScannedMedia("Film.mp4", "video", scannedRoleOrdinary)
			if err != nil || input == nil || !input.unchanged {
				t.Fatalf("prepare cached publication: input=%v error=%v", input, err)
			}
			t.Cleanup(func() {
				if err := input.close(); err != nil {
					t.Errorf("close queued cached input: %v", err)
				}
			})
			unblock, retire := cachedObservationBlockBackground(t, ctx, state)
			assertDirectoryUnread := primaryScanRoutingWatchSource(t, state.root.path)
			assertMediaUnread := primaryScanRoutingWatchSource(t, filepath.Join(state.root.path, "Film.mp4"))
			beforeJob := state.task.job
			trace.reset()
			done := make(chan struct{})
			var result error
			t.Cleanup(func() {
				state.task.cancel()
				unblock()
				mediaSourceAdmissionTestWait(t, done, "queued cached publication cleanup")
			})
			go func() {
				result = state.publishScannedMedia("Film.mp4", "video", hierarchy{parentID: state.library.ID}, input)
				close(done)
			}()
			wait, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			primaryReadTestWaitQueued(t, wait, beforeIO.Queued+1)
			assertDirectoryUnread()
			assertMediaUnread()
			if trace.begins.Load() != 0 || trace.authorityImplicitAttempts.Load() != 0 {
				t.Fatalf("queued publication started SQL before its actual grant: tx=%d authority=%d", trace.begins.Load(), trace.authorityImplicitAttempts.Load())
			}
			id := state.root.id
			if scenario.job {
				id = state.task.job.ID
			}
			changed, err := pool.Exec(ctx, scenario.statement, id)
			if err != nil || changed.RowsAffected() != 1 {
				t.Fatalf("mutate queued publication authority: rows=%d error=%v", changed.RowsAffected(), err)
			}
			unblock()
			mediaSourceAdmissionTestWait(t, done, "queued cached publication rejection")
			assertDirectoryUnread()
			assertMediaUnread()
			if !errors.Is(result, scenario.want) || state.warnings != 0 || state.task.job.Scanned != beforeJob.Scanned ||
				trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 {
				t.Fatalf("queued publication read or wrote after stale authority: error=%v want=%v warnings=%d scanned=%d/%d items=%d metadata=%d",
					result, scenario.want, state.warnings, state.task.job.Scanned, beforeJob.Scanned, trace.itemRows.Load(), trace.metadataRows.Load())
			}
			retire()
			if err := input.close(); err != nil {
				t.Fatal(err)
			}
			if err := walk.Close(); err != nil {
				t.Fatal(err)
			}
			state.walkIO = nil
			if stats := originalMediaReadGovernor.Stats(); stats != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners ||
				scanProbeOpenDescriptors(t, []string{filepath.Join(state.root.path, "Film.mp4")})[0] != 0 {
				t.Fatalf("rejected cached publication retained I/O resources: before=%+v after=%+v owners=%+v", beforeIO, stats, originalMediaReadOwners.Stats())
			}
		})
	}
}

type cachedObservationCompletionBarrier struct {
	armed   atomic.Bool
	owner   *pgx.Conn
	entered chan struct{}
	release chan struct{}
}

func (barrier *cachedObservationCompletionBarrier) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if conn == barrier.owner && strings.HasPrefix(data.SQL, "SELECT i.file_identity, i.file_size") &&
		strings.Contains(data.SQL, "FROM item_subtitles s WHERE s.item_id = i.id AND s.active") && barrier.armed.CompareAndSwap(true, false) {
		close(barrier.entered)
		select {
		case <-barrier.release:
		case <-ctx.Done():
		}
	}
	return ctx
}

func (*cachedObservationCompletionBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestCachedScanObservationKeepsFinalCancellationCheckpoint(t *testing.T) {
	barrier := &cachedObservationCompletionBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	ctx, pool, store, state, trace, _ := scanCachedVisitFixture(t, barrier)
	primaryScanRoutingRetainWalk(t, state)
	input, err := state.inspectScannedMedia("Film.mp4", "video", scannedRoleOrdinary)
	if err != nil || input == nil || !input.unchanged {
		t.Fatalf("prepare completion fixture: input=%v error=%v", input, err)
	}
	t.Cleanup(func() {
		if err := input.close(); err != nil {
			t.Errorf("close final checkpoint input: %v", err)
		}
	})
	before := state.task.job
	barrier.owner = store.ownership.conn.Conn()
	barrier.armed.Store(true)
	trace.reset()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(barrier.release) }) }
	done := make(chan struct{})
	var result error
	t.Cleanup(func() {
		state.task.cancel()
		unblock()
		mediaSourceAdmissionTestWait(t, done, "cached final checkpoint cleanup")
	})
	go func() {
		result = state.publishScannedMedia("Film.mp4", "video", hierarchy{parentID: state.library.ID}, input)
		close(done)
	}()
	mediaSourceAdmissionTestWait(t, barrier.entered, "cached owner lookup after observation")
	if stats := originalMediaReadGovernor.Stats(); stats.Active != 0 || stats.Background != 0 || stats.Queued != 0 {
		t.Fatalf("cached owner lookup held the observation lease: %+v", stats)
	}
	assertUnread := primaryScanRoutingWatchSource(t, state.root.path)
	if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", before.ID); err != nil {
		t.Fatal(err)
	}
	unblock()
	mediaSourceAdmissionTestWait(t, done, "cached final cancellation checkpoint")
	assertUnread()
	if !errors.Is(result, context.Canceled) || trace.cachedCompletionChecks.Load() != 1 ||
		trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 || state.warnings != 0 {
		t.Fatalf("observation skipped its final cancellation fence: error=%v completion=%d items=%d metadata=%d warnings=%d",
			result, trace.cachedCompletionChecks.Load(), trace.itemRows.Load(), trace.metadataRows.Load(), state.warnings)
	}
	job, err := store.GetJob(ctx, before.ID)
	if err != nil || !job.CancelRequested || job.Scanned != before.Scanned || job.Added != before.Added || job.Updated != before.Updated {
		t.Fatalf("final cancellation lost the already accepted prefix: job=%+v before=%+v error=%v", job, before, err)
	}
	taskScanAssertChild(t, ctx, pool, state.task.job.TaskChildID, job)
}

func cachedObservationRefreshDirectory(t *testing.T, state *scanState) {
	t.Helper()
	info, err := state.opened.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	state.directoryIdentities = map[string]os.FileInfo{".": info}
	state.imageDirectories, state.subtitleDirectories = nil, nil
}

func cachedObservationSidecarCounts(t *testing.T, state *scanState) (int, int) {
	t.Helper()
	var subtitles, images int
	if err := state.store.pool.QueryRow(state.task.ctx, `SELECT
		(SELECT count(*) FROM item_subtitles s JOIN items i ON i.id=s.item_id WHERE i.root_id=$1 AND s.active),
		(SELECT count(*) FROM item_images m JOIN items i ON i.id=m.item_id WHERE i.root_id=$1)`, state.root.id).Scan(&subtitles, &images); err != nil {
		t.Fatal(err)
	}
	return subtitles, images
}

func TestCachedScanObservationFallsBackForSidecarPayloadAndDeletion(t *testing.T) {
	for _, kind := range []string{"subtitle", "image"} {
		t.Run(kind, func(t *testing.T) {
			_, _, store, state, trace, _ := scanCachedVisitFixture(t)
			primaryScanRoutingRetainWalk(t, state)
			prober := store.prober.(*libraryFixtureProber)
			beforeProbes := len(prober.calls())
			name, contents := "Film.en.srt", []byte(subtitleTestSRT)
			wantSubtitles, wantImages := 1, 0
			if kind == "image" {
				name, contents = "poster.png", imageStoreTestPNG(t)
				wantSubtitles, wantImages = 0, 1
			}
			path := filepath.Join(state.root.path, name)
			if err := os.WriteFile(path, contents, 0600); err != nil {
				t.Fatal(err)
			}
			cachedObservationRefreshDirectory(t, state)
			trace.reset()
			if err := state.scanFile("Film.mp4", "video", hierarchy{parentID: state.library.ID}); err != nil {
				t.Fatal(err)
			}
			if subtitles, images := cachedObservationSidecarCounts(t, state); subtitles != wantSubtitles || images != wantImages {
				t.Fatalf("candidate observation skipped payload publication: subtitles=%d images=%d", subtitles, images)
			}
			if trace.authorityImplicitSingleRows.Load() <= 2 {
				t.Fatal("payload candidate incorrectly completed through the absence-only path")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			cachedObservationRefreshDirectory(t, state)
			trace.reset()
			if err := state.scanFile("Film.mp4", "video", hierarchy{parentID: state.library.ID}); err != nil {
				t.Fatal(err)
			}
			if subtitles, images := cachedObservationSidecarCounts(t, state); subtitles != 0 || images != 0 {
				t.Fatalf("absence optimization retained deleted sidecars: subtitles=%d images=%d", subtitles, images)
			}
			if trace.authorityImplicitSingleRows.Load() <= 2 || len(prober.calls()) != beforeProbes || state.warnings != 0 {
				t.Fatalf("stored sidecar deletion missed complete fallback or repeated probe: authority=%d probes=%d/%d warnings=%d",
					trace.authorityImplicitSingleRows.Load(), len(prober.calls()), beforeProbes, state.warnings)
			}
		})
	}
}

func TestCachedScanObservationRejectsChangedDirectoryEvidence(t *testing.T) {
	for _, change := range []string{"add", "delete", "rename"} {
		t.Run(change, func(t *testing.T) {
			_, _, _, state, trace, _ := scanCachedVisitFixture(t)
			primaryScanRoutingRetainWalk(t, state)
			directory := state.root.path
			if change != "add" {
				if err := os.WriteFile(filepath.Join(directory, "unrelated.txt"), []byte("unrelated"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cachedObservationRefreshDirectory(t, state)
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			expected := state.directoryIdentities["."]
			state.imageDirectories = map[string]*imageDirectoryIndex{".": newImageDirectoryIndex(names, expected)}
			state.subtitleDirectories = map[string]*subtitleDirectoryIndex{".": newSubtitleDirectoryIndex(entries, state.library.CollectionType, expected)}
			switch change {
			case "add":
				err = os.WriteFile(filepath.Join(directory, "Film.en.srt"), []byte(subtitleTestSRT), 0600)
			case "delete":
				err = os.Remove(filepath.Join(directory, "unrelated.txt"))
			case "rename":
				err = os.Rename(filepath.Join(directory, "unrelated.txt"), filepath.Join(directory, "Film.en.srt"))
			}
			if err != nil {
				t.Fatal(err)
			}
			// Advance the directory timestamp explicitly so this assertion does
			// not depend on the filesystem's timestamp granularity or a sleep.
			if err := os.Chtimes(directory, expected.ModTime(), expected.ModTime().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			trace.reset()
			if err := state.scanFile("Film.mp4", "video", hierarchy{parentID: state.library.ID}); err != nil {
				t.Fatal(err)
			}
			if state.warnings == 0 || trace.authorityImplicitSingleRows.Load() <= 2 || trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 {
				t.Fatalf("changed listing authorized the absence-only path: warnings=%d authority=%d items=%d metadata=%d",
					state.warnings, trace.authorityImplicitSingleRows.Load(), trace.itemRows.Load(), trace.metadataRows.Load())
			}
		})
	}
}

func TestCachedScanObservationRejectsMediaReplacementBeforePublication(t *testing.T) {
	_, _, _, state, trace, _ := scanCachedVisitFixture(t)
	primaryScanRoutingRetainWalk(t, state)
	input, err := state.inspectScannedMedia("Film.mp4", "video", scannedRoleOrdinary)
	if err != nil || input == nil || !input.unchanged {
		t.Fatalf("prepare replacement fixture: input=%v error=%v", input, err)
	}
	t.Cleanup(func() {
		if err := input.close(); err != nil {
			t.Errorf("close replaced cached input: %v", err)
		}
	})
	path := filepath.Join(state.root.path, "Film.mp4")
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video:replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	if err := state.publishScannedMedia("Film.mp4", "video", hierarchy{parentID: state.library.ID}, input); err != nil {
		t.Fatal(err)
	}
	if state.warnings == 0 || trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 ||
		state.imageDirectories != nil || state.subtitleDirectories != nil {
		t.Fatalf("replacement media authorized metadata or absence: warnings=%d items=%d metadata=%d image_index=%v subtitle_index=%v",
			state.warnings, trace.itemRows.Load(), trace.metadataRows.Load(), state.imageDirectories, state.subtitleDirectories)
	}
}

func TestCachedScanObservationRejectsQueuedEpisodeBeforeVirtualFolders(t *testing.T) {
	for _, change := range []string{"task_cancel", "binding_revision", "source_replacement"} {
		t.Run(change, func(t *testing.T) {
			ctx, pool, _, state, trace, _ := scanCachedVisitFixture(t)
			state.library.CollectionType = "mixed"
			original := filepath.Join(state.root.path, "Film.mp4")
			initialRelative := "Initial.S01E01.mp4"
			initialPath := filepath.Join(state.root.path, initialRelative)
			if err := os.Rename(original, initialPath); err != nil {
				t.Fatal(err)
			}
			cachedObservationRefreshDirectory(t, state)
			if err := state.scanFile(initialRelative, "video", hierarchy{parentID: state.library.ID}); err != nil {
				t.Fatal(err)
			}
			// A cached Episode moves to another virtual hierarchy. The new
			// Series and Season do not exist before this publication attempt.
			relative := "Other.S02E01.mp4"
			path := filepath.Join(state.root.path, relative)
			if err := os.Rename(initialPath, path); err != nil {
				t.Fatal(err)
			}
			cachedObservationRefreshDirectory(t, state)
			primaryScanRoutingRetainWalk(t, state)
			input, err := state.inspectScannedMedia(relative, "video", scannedRoleOrdinary)
			if err != nil || input == nil || !input.unchanged || input.stored.itemType != "Episode" {
				t.Fatalf("prepare cached Episode transition: input=%v error=%v", input, err)
			}
			t.Cleanup(func() {
				if err := input.close(); err != nil {
					t.Errorf("close rejected Episode input: %v", err)
				}
			})
			snapshot := func() (int, string) {
				t.Helper()
				var folders int
				var item string
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE root_id=$1 AND is_folder`, state.root.id).Scan(&folders); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, `SELECT row_to_json(i)::text FROM items i WHERE id=$1`, input.stored.id).Scan(&item); err != nil {
					t.Fatal(err)
				}
				return folders, item
			}
			beforeFolders, beforeItem := snapshot()
			if beforeFolders != 2 {
				t.Fatalf("Episode fixture must start with one virtual Series and Season: folders=%d", beforeFolders)
			}
			unblock, retire := cachedObservationBlockBackground(t, ctx, state)
			trace.reset()
			done := make(chan struct{})
			var result error
			t.Cleanup(func() {
				state.task.cancel()
				unblock()
				mediaSourceAdmissionTestWait(t, done, "queued Episode publication cleanup")
			})
			go func() {
				result = state.publishScannedMedia(relative, "video", hierarchy{parentID: state.library.ID}, input)
				close(done)
			}()
			wait, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			primaryReadTestWaitQueued(t, wait, 1)
			if folders, item := snapshot(); folders != beforeFolders || item != beforeItem || trace.itemRows.Load() != 0 || trace.begins.Load() != 0 {
				t.Fatalf("queued Episode published its virtual hierarchy before authority: folders=%d/%d items=%d transactions=%d",
					folders, beforeFolders, trace.itemRows.Load(), trace.begins.Load())
			}
			want := error(nil)
			switch change {
			case "task_cancel":
				_, err = pool.Exec(ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", state.task.job.ID)
				want = context.Canceled
			case "binding_revision":
				_, err = pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", state.root.id)
				want = ErrRootBindingConflict
			case "source_replacement":
				err = os.Rename(path, path+".retained")
				if err == nil {
					err = os.WriteFile(path, []byte("video:replacement-episode"), 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			unblock()
			mediaSourceAdmissionTestWait(t, done, "queued Episode rejection")
			retire()
			if !errors.Is(result, want) || change == "source_replacement" && state.warnings == 0 {
				t.Fatalf("queued Episode did not reject changed authority/source: error=%v want=%v warnings=%d", result, want, state.warnings)
			}
			if folders, item := snapshot(); folders != beforeFolders || item != beforeItem || trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 {
				t.Fatalf("rejected cached Episode published virtual folders or primary facts: folders=%d/%d items=%d metadata=%d",
					folders, beforeFolders, trace.itemRows.Load(), trace.metadataRows.Load())
			}
		})
	}
}

func TestCachedScanObservationRetainsLateSourceReplacement(t *testing.T) {
	ctx, pool, _, state, trace, _ := scanCachedVisitFixture(t)
	primaryScanRoutingRetainWalk(t, state)
	input, err := state.inspectScannedMedia("Film.mp4", "video", scannedRoleOrdinary)
	if err != nil || input == nil || !input.unchanged || input.authority != nil {
		t.Fatalf("prepare cached late-replacement fixture: input=%v error=%v", input, err)
	}
	t.Cleanup(func() {
		if err := input.close(); err != nil {
			t.Errorf("close late-replacement input: %v", err)
		}
	})
	var before string
	if err := pool.QueryRow(ctx, `SELECT row_to_json(i)::text FROM items i WHERE id=$1`, input.stored.id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	nfoPath := filepath.Join(state.root.path, "Film.nfo")
	if err := os.WriteFile(nfoPath, []byte(`<movie><title>Must not publish replaced source</title></movie>`), 0600); err != nil {
		t.Fatal(err)
	}
	cachedObservationRefreshDirectory(t, state)
	operation, err := input.primary.preparePublicationIO()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := operation.Close(); err != nil {
			t.Errorf("close late-replacement observation: %v", err)
		}
	})
	path := filepath.Join(state.root.path, "Film.mp4")
	descriptorsBefore := scanProbeOpenDescriptors(t, []string{state.root.path})[0]
	var observedDirectory *os.File
	var observation cachedSidecarObservation
	var nfo localNFOObservation
	var injectErr error
	reads := 0
	err = input.primary.runPublicationMetadata(operation, func(work context.Context) error {
		var err error
		nfo, err = state.observeLocalNFO(work, []string{"Film.nfo"}, "movie")
		if err != nil {
			return err
		}
		primary, err := state.opened.Lstat("Film.mp4")
		if err != nil {
			return err
		}
		observation, err = state.observeCachedSidecarAbsenceWithReader(work, "Film.mp4", "Movie", input.probe, primary,
			func(readContext context.Context, directory *os.File, entriesLimit, bytesLimit int) ([]os.DirEntry, error) {
				reads++
				observedDirectory = directory
				entries, readErr := readScanDirectoryEntriesLimit(readContext, directory, entriesLimit, bytesLimit)
				if readErr != nil {
					return nil, readErr
				}
				// Replace the media after the actual complete ReadDir, while
				// the observation still owns its native directory descriptor.
				injectErr = os.Rename(path, path+".retained")
				if injectErr == nil {
					injectErr = os.WriteFile(path, []byte("video:late-replacement"), 0600)
				}
				return entries, injectErr
			})
		return err
	})
	if err != nil || injectErr != nil || reads != 1 || !observation.sourceChanged || observation.absent || nfo.path != "Film.nfo" || len(nfo.data) == 0 {
		t.Fatalf("late source change became ordinary absence fallback: error=%v injection=%v reads=%d changed=%v absent=%v nfo=%q",
			err, injectErr, reads, observation.sourceChanged, observation.absent, nfo.path)
	}
	if _, err := observedDirectory.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("observation retained its actual directory descriptor: %v", err)
	}
	if descriptors := scanProbeOpenDescriptors(t, []string{state.root.path})[0]; descriptors != descriptorsBefore {
		t.Fatalf("late-replacement observation retained root descriptors: before=%d after=%d", descriptorsBefore, descriptors)
	}
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	// The same cached input still carries old probe and file facts. Even with
	// a valid changed NFO, publication must reject its now-replaced pathname.
	trace.reset()
	if err := state.publishScannedMedia("Film.mp4", "video", hierarchy{parentID: state.library.ID}, input); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT row_to_json(i)::text FROM items i WHERE id=$1`, input.stored.id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before || state.warnings == 0 || trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 {
		t.Fatalf("replaced cached media published changed metadata with stale file facts: same_row=%v warnings=%d items=%d metadata=%d",
			after == before, state.warnings, trace.itemRows.Load(), trace.metadataRows.Load())
	}
}
