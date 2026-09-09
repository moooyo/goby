package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type forceProbeVersionedFunc func(context.Context, *os.File) (media.Info, error)

func (prober forceProbeVersionedFunc) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	return prober(ctx, file)
}

func (forceProbeVersionedFunc) CacheVersion() int { return media.CurrentProbeVersion }

func forceProbeTestInfo(file *os.File) (media.Info, error) {
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	info := libraryMediaFixture([]byte("video:force-probe"))
	info.Size = stat.Size()
	info.ProbeVersion = media.CurrentProbeVersion
	info.FileChangeTimeNs = media.FileChangeTime(stat)
	return info, nil
}

func forceProbeTestIndex(info media.Info) media.VideoSeekIndex {
	return media.VideoSeekIndex{
		Version: media.VideoSeekIndexVersion, StreamIndex: 0, DurationTicks: info.DurationTicks,
		TimeBaseNumerator: 1, TimeBaseDenominator: 1000,
		SourceIdentity: strings.Repeat("a", 64), ToolIdentity: strings.Repeat("b", 64),
		ParameterSetsSHA256: strings.Repeat("c", 64), PacketSideDataChecked: true, NALScopeChecked: true,
		Width: 1920, Height: 1080, PixelFormat: "yuv420p", DecodedFrameBytes: 1920 * 1080 * 3 / 2,
		Entries: []media.VideoSeekPoint{{PTS: 0, DTS: 0, CodedSHA256: strings.Repeat("d", 64), DecodedSHA256: strings.Repeat("e", 64)}},
	}
}

func forceProbeTestScan(t *testing.T, ctx context.Context, store *Store, libraryID, status string) Job {
	t.Helper()
	job, err := store.StartScanWithOptions(ctx, libraryID, ScanOptions{ForceProbe: true})
	if err != nil {
		t.Fatalf("start forced media probe: %v", err)
	}
	if !job.ForceProbe {
		t.Fatal("queued scan omitted its requested force probe policy")
	}
	finished := libraryIntegrationWaitJob(t, ctx, store, job.ID, status)
	if !finished.ForceProbe {
		t.Fatal("finished scan lost its force probe policy")
	}
	return finished
}

func forceProbeUserDataSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY user_id), '[]'::jsonb)::text
		FROM user_item_data d WHERE item_id = $1`, itemID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot media user state: %v", err)
	}
	return snapshot
}

func TestStoreForceProbeRefreshesSameVersionCacheAndPreservesUserMetadata(t *testing.T) {
	var calls atomic.Int32
	var indexEnabled atomic.Bool
	prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		calls.Add(1)
		info, err := forceProbeTestInfo(file)
		if err == nil && indexEnabled.Load() {
			info.VideoSeekIndexes = []media.VideoSeekIndex{forceProbeTestIndex(info)}
		}
		return info, err
	})
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Refresh.mp4", "video:unchanged-source")
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if media.FileChangeTime(stat) <= 0 {
		t.Skip("same-version probe caching requires filesystem change time support")
	}
	libraryIntegrationFile(t, allowedRoot, "movies/Refresh.nfo", `<movie><title>Automatic title</title><plot>Locked source overview.</plot><genre>Source genre</genre></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Refresh movies", "movies", filepath.Dir(path))
	initial := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if initial.ForceProbe || initial.Added != 1 || calls.Load() != 1 {
		t.Fatalf("ordinary initial scan did not retain its default policy: job=%+v calls=%d", initial, calls.Load())
	}
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if item.Media == nil || len(item.Media.VideoSeekIndexes) != 0 {
		t.Fatal("initial same-version source should retain ordinary media facts without an index")
	}
	actor := metadataEditTestActor(t, ctx, pool, "force-probe-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{
		"Name": json.RawMessage(`"Manual title"`), "Genres": json.RawMessage(`["Manual genre"]`),
	}, []string{"Overview"})
	playedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, PlaybackPositionTicks: 90 * media.TicksPerSecond, PlayCount: 4, IsFavorite: true, LastPlayedDate: &playedAt})
	before := metadataEditTestSnapshot(t, ctx, pool, item.ID)
	beforeUserData := forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	indexEnabled.Store(true)
	cached := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if cached.ForceProbe || cached.Updated != 0 || cached.Added != 0 || calls.Load() != 1 {
		t.Fatalf("ordinary scan did not reuse valid same-version facts: job=%+v calls=%d", cached, calls.Load())
	}
	if after := metadataEditTestSnapshot(t, ctx, pool, item.ID); after != before {
		t.Fatal("ordinary cached scan rewrote the item or manual metadata")
	}
	forced := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
	if forced.Scanned != 1 || forced.Updated != 1 || forced.Added != 0 || forced.Error != "" || calls.Load() != 2 {
		t.Fatalf("forced scan did not refresh the cached source: job=%+v calls=%d", forced, calls.Load())
	}
	updated := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if updated.ID != item.ID || updated.ParentID != item.ParentID || updated.Media == nil ||
		updated.Media.ProbeVersion != item.Media.ProbeVersion || len(updated.Media.VideoSeekIndexes) != 1 ||
		!reflect.DeepEqual(updated.Media.VideoSeekIndexes[0], forceProbeTestIndex(*updated.Media)) {
		t.Fatalf("forced probe did not persist the new private index on the existing item: %+v", updated)
	}
	afterDetail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if !reflect.DeepEqual(afterDetail.Overrides, detail.Overrides) || !reflect.DeepEqual(afterDetail.LockedValues, detail.LockedValues) ||
		!reflect.DeepEqual(afterDetail.LockedFields, detail.LockedFields) || !reflect.DeepEqual(afterDetail.Effective, detail.Effective) ||
		afterDetail.LastEditedBy != detail.LastEditedBy || !reflect.DeepEqual(afterDetail.LastEditedAt, detail.LastEditedAt) {
		t.Fatal("forced media probing changed administrator metadata or its editor history")
	}
	if after := forceProbeUserDataSnapshot(t, ctx, pool, item.ID); after != beforeUserData {
		t.Fatal("forced media probing changed favorite, playback progress, or play history")
	}
	// Successful refreshes are counted even if optional analysis returns the
	// same facts or supplies no index for an unsupported source.
	identical := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
	if identical.Updated != 1 || identical.Error != "" || calls.Load() != 3 {
		t.Fatalf("identical forced facts were not counted as refreshed: %+v", identical)
	}
	indexEnabled.Store(false)
	fallback := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
	if fallback.Updated != 1 || fallback.Error != "" || calls.Load() != 4 {
		t.Fatalf("absence of optional index facts failed a valid media refresh: %+v", fallback)
	}
	withoutIndex := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if withoutIndex.Media == nil || len(withoutIndex.Media.VideoSeekIndexes) != 0 || withoutIndex.ID != item.ID ||
		withoutIndex.Media.DurationTicks != item.Media.DurationTicks || !reflect.DeepEqual(withoutIndex.Media.Streams, item.Media.Streams) {
		t.Fatal("optional-index fallback discarded ordinary media facts or retained stale index facts")
	}
	if after := forceProbeUserDataSnapshot(t, ctx, pool, item.ID); after != beforeUserData {
		t.Fatal("repeated forced probing changed user state")
	}
}

func TestStoreForceProbeFailurePreservesItemMediaMetadataAndUserState(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		calls.Add(1)
		if fail.Load() {
			return media.Info{}, errors.New("forced probe fixture failure")
		}
		info, err := forceProbeTestInfo(file)
		info.VideoSeekIndexes = []media.VideoSeekIndex{forceProbeTestIndex(info)}
		return info, err
	})
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Retained.mp4", "video:retained-source")
	libraryIntegrationFile(t, allowedRoot, "movies/Retained.nfo", `<movie><title>Valid automatic title</title><plot>Valid overview.</plot></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Retained movies", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	actor := metadataEditTestActor(t, ctx, pool, "force-probe-failure-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{"Name": json.RawMessage(`"Preserved manual title"`)}, []string{"Overview"})
	playedAt := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, PlaybackPositionTicks: 12 * media.TicksPerSecond, PlayCount: 3, IsFavorite: true, Played: true, LastPlayedDate: &playedAt})
	before := metadataEditTestSnapshot(t, ctx, pool, item.ID)
	beforeUserData := forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	libraryIntegrationFile(t, allowedRoot, "movies/Retained.nfo", `<movie><title>Unaccepted source title</title><plot>Unaccepted overview.</plot></movie>`)
	libraryIntegrationFile(t, allowedRoot, "movies/Unknown.mp4", "video:unprobeable-source")
	fail.Store(true)
	job := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
	if job.Error == "" || job.Scanned != 2 || job.Added != 0 || job.Updated != 0 || calls.Load() != 3 {
		t.Fatalf("forced probe failures changed existing scan warning semantics: job=%+v calls=%d", job, calls.Load())
	}
	if after := metadataEditTestSnapshot(t, ctx, pool, item.ID); after != before {
		t.Fatal("failed forced probing changed the item, media, metadata layers, or entity associations")
	}
	if after := forceProbeUserDataSnapshot(t, ctx, pool, item.ID); after != beforeUserData {
		t.Fatal("failed forced probing changed stored user state")
	}
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if len(items.Items) != 1 || items.Items[0].ID != item.ID {
		t.Fatalf("failed forced probing added an unknown file or replaced the original item: %+v", items)
	}
}

func TestStoreForceProbeCancellationPreservesQueuedBusyAndWorkerBounds(t *testing.T) {
	var blocked atomic.Bool
	var calls atomic.Int32
	entered := make(chan struct{}, 3)
	prober := forceProbeVersionedFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		calls.Add(1)
		if blocked.Load() {
			entered <- struct{}{}
			<-ctx.Done()
			return media.Info{}, ctx.Err()
		}
		return forceProbeTestInfo(file)
	})
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	libraries := make([]Library, 0, 3)
	items := make([]Item, 0, 3)
	snapshots := make([]string, 0, 3)
	for _, name := range []string{"first", "second", "queued"} {
		path := libraryIntegrationFile(t, allowedRoot, name+"/Cached.mp4", "video:"+name)
		library := libraryIntegrationCreate(t, ctx, store, name, "movies", filepath.Dir(path))
		libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
		libraries = append(libraries, library)
		items = append(items, item)
		snapshots = append(snapshots, metadataEditTestSnapshot(t, ctx, pool, item.ID))
	}
	blocked.Store(true)
	jobs := make([]Job, 0, 3)
	for index, library := range libraries {
		requestCtx, cancel := context.WithCancel(ctx)
		job, err := store.StartScanWithOptions(requestCtx, library.ID, ScanOptions{ForceProbe: true})
		cancel()
		if err != nil || !job.ForceProbe {
			t.Fatalf("queue detached forced scan: job=%+v error=%v", job, err)
		}
		jobs = append(jobs, job)
		if index < 2 {
			select {
			case <-entered:
			case <-time.After(15 * time.Second):
				t.Fatal("forced scan did not reach a worker independently of the caller context")
			}
		}
		for _, force := range []bool{false, true} {
			if _, err := store.StartScanWithOptions(ctx, library.ID, ScanOptions{ForceProbe: force}); !errors.Is(err, ErrBusy) {
				t.Errorf("duplicate running or queued scan with force=%v: got %v, want ErrBusy", force, err)
			}
		}
	}
	queued, err := store.GetJob(ctx, jobs[2].ID)
	if err != nil || queued.Status != "Queued" || queued.StartedAt != nil || !queued.ForceProbe || calls.Load() != 5 {
		t.Fatalf("forced scan exceeded two workers or lost queued policy: job=%+v calls=%d error=%v", queued, calls.Load(), err)
	}
	// Cancel the queued job before freeing either worker so it cannot probe.
	for _, index := range []int{2, 0, 1} {
		if err := store.CancelJob(ctx, jobs[index].ID); err != nil {
			t.Fatalf("cancel forced scan: %v", err)
		}
		job := libraryIntegrationWaitJob(t, ctx, store, jobs[index].ID, "Cancelled")
		if !job.ForceProbe || job.Updated != 0 || job.Added != 0 {
			t.Fatalf("cancelled forced scan lost policy or published a probe result: %+v", job)
		}
		if index == 2 && (job.StartedAt != nil || job.Scanned != 0) {
			t.Fatalf("cancelled queued scan entered a worker: %+v", job)
		}
	}
	if calls.Load() != 5 {
		t.Fatalf("cancelled queued scan reached media probing: calls=%d, want 5", calls.Load())
	}
	for index, item := range items {
		if after := metadataEditTestSnapshot(t, ctx, pool, item.ID); after != snapshots[index] {
			t.Fatalf("cancelled forced scan changed existing catalog state for item %s", item.ID)
		}
	}
	history, err := store.ListJobs(ctx)
	if err != nil || len(history) != 6 {
		t.Fatalf("duplicate forced scans persisted extra jobs: count=%d error=%v", len(history), err)
	}
	forcedCount := 0
	for _, job := range history {
		if job.ForceProbe {
			forcedCount++
			if job.Status != "Cancelled" {
				t.Errorf("listed forced scan did not retain its terminal status: %+v", job)
			}
		}
	}
	if forcedCount != 3 {
		t.Errorf("job history lost force probe flags: got %d, want 3", forcedCount)
	}
}

func TestStoreForceProbeFlagSurvivesRestartAndInterruptedRecovery(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraries := make([]Library, 0, 2)
	for _, name := range []string{"queued", "running"} {
		path := libraryIntegrationFile(t, allowedRoot, name+"/Media.mp4", "video:"+name)
		libraries = append(libraries, libraryIntegrationCreate(t, ctx, store, name, "movies", filepath.Dir(path)))
	}
	ordinary := libraryIntegrationScan(t, ctx, store, libraries[0].ID, "Completed")
	forced := forceProbeTestScan(t, ctx, store, libraries[0].ID, "Completed")
	if err := store.Close(ctx); err != nil {
		t.Fatalf("close original scan store: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status, force_probe, started_at)
		VALUES ('force-recover-queued', $1, 'Queued', true, NULL), ('force-recover-running', $2, 'Running', true, now())`, libraries[0].ID, libraries[1].ID); err != nil {
		t.Fatalf("seed interrupted forced scans: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status, finished_at)
		VALUES ('force-default-history', $1, 'Completed', now())`, libraries[0].ID); err != nil {
		t.Fatalf("seed default scan history: %v", err)
	}
	reopened, err := New(pool, &libraryFixtureProber{}, []string{allowedRoot})
	if err != nil {
		t.Fatalf("reopen scan store: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := reopened.Close(cleanupCtx); err != nil {
			t.Errorf("close reopened scan store: %v", err)
		}
	})
	wanted := map[string]struct {
		force  bool
		status string
	}{
		ordinary.ID: {false, "Completed"}, forced.ID: {true, "Completed"},
		"force-recover-queued": {true, "Interrupted"}, "force-recover-running": {true, "Interrupted"},
		"force-default-history": {false, "Completed"},
	}
	history, err := reopened.ListJobs(ctx)
	if err != nil || len(history) != len(wanted) {
		t.Fatalf("reopened job history is incomplete: count=%d error=%v", len(history), err)
	}
	for _, job := range history {
		want, ok := wanted[job.ID]
		if !ok || job.ForceProbe != want.force || job.Status != want.status || job.FinishedAt == nil {
			t.Errorf("recovery changed the durable scan policy: %+v", job)
		}
		read, err := reopened.GetJob(ctx, job.ID)
		if err != nil || !reflect.DeepEqual(read, job) {
			t.Errorf("single-job policy differs from restarted history: job=%+v error=%v", read, err)
		}
	}
}

func TestStoreForceProbeRetainsExistingSnapshotWhenSourceChanges(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "ModifiedDescriptor"
		if replace {
			name = "ReplacedPath"
		}
		t.Run(name, func(t *testing.T) {
			var change atomic.Bool
			var changedSuccessfully atomic.Bool
			var calls atomic.Int32
			var path string
			prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
				calls.Add(1)
				info, err := forceProbeTestInfo(file)
				if err != nil || !change.Load() {
					return info, err
				}
				if replace {
					if err := os.Rename(path, path+".backup"); err != nil {
						return media.Info{}, err
					}
					err = os.WriteFile(path, []byte("video:new-source"), 0600)
				} else {
					var stat os.FileInfo
					stat, err = file.Stat()
					if err == nil {
						err = os.Chtimes(path, stat.ModTime().Add(time.Second), stat.ModTime().Add(time.Second))
					}
				}
				changedSuccessfully.Store(err == nil)
				info.DurationTicks += media.TicksPerSecond
				return info, err
			})
			ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
			path = libraryIntegrationFile(t, allowedRoot, "movies/Changing.mp4", "video:old-source")
			library := libraryIntegrationCreate(t, ctx, store, "Changing source", "movies", filepath.Dir(path))
			libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
			before := metadataEditTestSnapshot(t, ctx, pool, item.ID)
			change.Store(true)
			job := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
			if !changedSuccessfully.Load() || job.Error == "" || job.Updated != 0 || job.Added != 0 || calls.Load() != 2 {
				t.Fatalf("forced scan accepted an inconsistent source: job=%+v calls=%d", job, calls.Load())
			}
			if after := metadataEditTestSnapshot(t, ctx, pool, item.ID); after != before {
				t.Fatal("forced probing persisted facts from a modified descriptor or replaced source path")
			}
		})
	}
}
