//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

func scanProbeWaitSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func scanProbeFixtureInfo(file *os.File) (media.Info, error) {
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	info := libraryMediaFixture([]byte("video:pipeline"))
	info.Size, info.ProbeVersion = stat.Size(), media.CurrentProbeVersion
	info.FileChangeTimeNs = media.FileChangeTime(stat)
	return info, nil
}

// The production walker preserves File.ReadDir's native order. os.ReadDir
// would sort these names and conceal the ordering contract under test.
func scanProbeFixtureNativeEntries(t *testing.T, path string) []os.DirEntry {
	t.Helper()
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, readErr := directory.ReadDir(-1)
	if err := errors.Join(readErr, directory.Close()); err != nil {
		t.Fatal(err)
	}
	return entries
}

func scanProbeFixtureNativeMediaNames(t *testing.T, path string, count int) []string {
	t.Helper()
	var names []string
	for _, entry := range scanProbeFixtureNativeEntries(t, path) {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && !ignoredName(entry.Name()) && extensionKind(entry.Name()) != "" {
			names = append(names, entry.Name())
		}
	}
	if len(names) != count {
		t.Fatalf("native media order has %d entries, want %d: %v", len(names), count, names)
	}
	t.Logf("native fixture media order=%v", names)
	return names
}

func TestScanProbePipelineIntegrationBoundsReadyFilesAndPublishesInOrder(t *testing.T) {
	firstEntered, secondReturned, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int64
	var firstName, secondName string
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		calls.Add(1)
		switch filepath.Base(file.Name()) {
		case firstName:
			close(firstEntered)
			select {
			case <-release:
			case <-ctx.Done():
				return media.Info{}, ctx.Err()
			}
		case secondName:
			defer close(secondReturned)
		}
		return scanProbeFixtureInfo(file)
	})
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	for _, name := range []string{"A", "B", "C"} {
		libraryIntegrationFile(t, root, "movies/"+name+".mp4", "video:"+name)
	}
	names := scanProbeFixtureNativeMediaNames(t, filepath.Join(root, "movies"), 3)
	firstName, secondName = names[0], names[1]
	var paths, wantOrder []string
	for _, name := range names {
		paths = append(paths, filepath.Join(root, "movies", name))
		wantOrder = append(wantOrder, cleanName(strings.TrimSuffix(name, filepath.Ext(name))))
	}
	library := libraryIntegrationCreate(t, ctx, store, "Pipeline", "movies", filepath.Join(root, "movies"))
	notifications := make(chan CatalogNotification, 16)
	var notificationOverflow atomic.Bool
	store.SetCatalogChangeListener(func(notification CatalogNotification) {
		select {
		case notifications <- notification:
		default:
			notificationOverflow.Store(true)
		}
	})
	t.Cleanup(func() { store.SetCatalogChangeListener(nil) })
	job, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	scanProbeWaitSignal(t, firstEntered, "the blocked first probe")
	scanProbeWaitSignal(t, secondReturned, "the ready second probe")
	if calls.Load() != 2 {
		t.Fatalf("probe calls behind a blocked first result = %d, want 2", calls.Load())
	}
	observed, err := store.GetJob(ctx, job.ID)
	if err != nil || observed.Scanned < 0 || observed.Scanned > 2 || observed.Added != 0 || observed.Updated != 0 {
		t.Fatalf("lookahead checkpoint = %+v, error = %v", observed, err)
	}
	// Scanned visibility follows the bounded checkpoint cadence. The actual
	// probe count, publication state and held descriptors still bound lookahead.
	var published int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND type='Movie'", library.ID).Scan(&published); err != nil || published != 0 {
		t.Fatalf("out-of-order publication count = %d, error = %v", published, err)
	}
	// Both running and ready files retain their one input descriptor; the third
	// file has not even been opened while the oldest ticket is unavailable.
	counts := scanProbeOpenDescriptors(t, paths)
	if !reflect.DeepEqual(counts, []int{1, 1, 0}) {
		t.Fatalf("pipeline descriptor counts = %v, want [1 1 0]", counts)
	}
	releaseOnce.Do(func() { close(release) })
	job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
	if job.Error != "" || job.Scanned != 3 || job.Added != 3 || calls.Load() != 3 {
		t.Fatalf("cold scan = %+v, calls = %d", job, calls.Load())
	}
	var order []string
	for len(notifications) != 0 {
		notification := <-notifications
		for _, change := range notification.Changes {
			if !change.IsFolder && change.Kind == CatalogAdded {
				var name string
				if err := pool.QueryRow(ctx, "SELECT name FROM items WHERE id=$1", change.ItemID).Scan(&name); err != nil {
					t.Fatal(err)
				}
				order = append(order, name)
			}
		}
	}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("publication order = %v, native visit order = %v", order, wantOrder)
	}
	if notificationOverflow.Load() {
		t.Fatal("publication observer overflowed")
	}
	if counts := scanProbeOpenDescriptors(t, paths); !reflect.DeepEqual(counts, []int{0, 0, 0}) {
		t.Fatalf("input descriptors survived terminal scan: %v", counts)
	}
	warm, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	warm = libraryIntegrationWaitJob(t, ctx, store, warm.ID, "Completed")
	if warm.Error != "" || warm.Scanned != 3 || warm.Added != 0 || warm.Updated != 0 || calls.Load() != 3 {
		t.Fatalf("warm scan changed cached facts: %+v, calls = %d", warm, calls.Load())
	}
}

func scanProbeOpenDescriptors(t *testing.T, paths []string) []int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	counts := make([]int, len(paths))
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil {
			continue
		}
		for i, path := range paths {
			if target == path {
				counts[i]++
			}
		}
	}
	return counts
}

func TestScanProbePipelineIntegrationOperationAuthorityAndStalePublication(t *testing.T) {
	const refreshedDurationTicks = 42 * media.TicksPerSecond
	for _, mutation := range []string{"source", "role", "binding_approval", "job", "ctime", "replacement"} {
		t.Run(mutation, func(t *testing.T) {
			entered, release := make(chan struct{}, 1), make(chan struct{})
			var blocked atomic.Bool
			var releaseOnce sync.Once
			prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
				if blocked.Load() {
					entered <- struct{}{}
					select {
					case <-release:
					case <-ctx.Done():
						return media.Info{}, ctx.Err()
					}
				}
				info, err := scanProbeFixtureInfo(file)
				if err == nil && mutation == "binding_approval" && blocked.Load() {
					info.DurationTicks = refreshedDurationTicks
				}
				return info, err
			})
			ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			path := libraryIntegrationFile(t, root, "movies/A.mp4", "video:original")
			library := libraryIntegrationCreate(t, ctx, store, "Pipeline", "movies", filepath.Dir(path))
			initial, err := store.StartScan(ctx, library.ID)
			if err != nil {
				t.Fatal(err)
			}
			initial = libraryIntegrationWaitJob(t, ctx, store, initial.ID, "Completed")
			if initial.Error != "" || initial.Added != 1 {
				t.Fatalf("initial scan = %+v", initial)
			}
			var itemID, rootID string
			if err := pool.QueryRow(ctx, "SELECT id,root_id FROM items WHERE library_id=$1 AND type='Movie'", library.ID).Scan(&itemID, &rootID); err != nil {
				t.Fatal(err)
			}
			blocked.Store(true)
			job, err := store.StartScanWithOptions(ctx, library.ID, ScanOptions{ForceProbe: true})
			if err != nil {
				t.Fatal(err)
			}
			scanProbeWaitSignal(t, entered, "a prepared cold source")
			wantStatus := "Completed"
			switch mutation {
			case "source":
				_, err = pool.Exec(ctx, `UPDATE items SET media=jsonb_set(media,'{DurationTicks}',to_jsonb(123456789::bigint)) WHERE id=$1`, itemID)
			case "role":
				_, err = pool.Exec(ctx, "INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES($1,'A.mp4',false)", rootID)
			case "binding_approval":
				_, err = pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", rootID)
			case "job":
				_, err = pool.Exec(ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", job.ID)
				wantStatus = "Cancelled"
			case "ctime":
				before, statErr := os.Stat(path)
				if statErr != nil {
					t.Fatal(statErr)
				}
				err = os.WriteFile(path, []byte("video:modified"), 0600)
				if err == nil {
					err = os.Chtimes(path, before.ModTime(), before.ModTime())
				}
				after, statErr := os.Stat(path)
				if statErr != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || media.FileChangeTime(before) == media.FileChangeTime(after) {
					t.Fatalf("fixture did not isolate ctime: before=%+v after=%+v error=%v", before, after, statErr)
				}
			case "replacement":
				err = os.Rename(path, path+".retained")
				if err == nil {
					err = os.WriteFile(path, []byte("video:replacement"), 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			var before string
			if err := pool.QueryRow(ctx, "SELECT media::text FROM items WHERE id=$1", itemID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			releaseOnce.Do(func() { close(release) })
			job = libraryIntegrationWaitJob(t, ctx, store, job.ID, wantStatus)
			var after string
			var durationTicks int64
			if err := pool.QueryRow(ctx, "SELECT media::text,(media->>'DurationTicks')::bigint FROM items WHERE id=$1", itemID).Scan(&after, &durationTicks); err != nil {
				t.Fatal(err)
			}
			paths := []string{path}
			if mutation == "replacement" {
				paths = append(paths, path+".retained")
			}
			for _, count := range scanProbeOpenDescriptors(t, paths) {
				if count != 0 {
					t.Fatalf("completed scan retained %d actual source descriptors", count)
				}
			}
			if mutation == "binding_approval" {
				if job.Error != "" || job.Scanned != 1 || job.Added != 0 || job.Updated != 1 || before == after || durationTicks != refreshedDurationTicks {
					t.Fatalf("operation approval did not publish its completed cold probe: job=%+v duration=%d before=%s after=%s", job, durationTicks, before, after)
				}
				return
			}
			if before != after || job.Added != 0 || job.Updated != 0 {
				t.Fatalf("stale probe was published: job=%+v before=%s after=%s", job, before, after)
			}
			if mutation != "job" && job.Error == "" {
				t.Fatalf("rejected %s probe had no retained-record diagnostic: %+v", mutation, job)
			}
		})
	}
}

func TestScanProbePipelineIntegrationCloseRetainsActualWorkerOwnership(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var releaseOnce sync.Once
	prober := scanProberFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		entered <- struct{}{}
		<-release // Deliberately model a filesystem operation that ignores cancel.
		return scanProbeFixtureInfo(file)
	})
	ctx, _, store, root, _ := libraryIntegrationStore(t, prober)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	paths := []string{libraryIntegrationFile(t, root, "movies/A.mp4", "video:A"),
		libraryIntegrationFile(t, root, "movies/B.mp4", "video:B")}
	library := libraryIntegrationCreate(t, ctx, store, "Pipeline", "movies", filepath.Dir(paths[0]))
	if _, err := store.StartScan(ctx, library.ID); err != nil {
		t.Fatal(err)
	}
	scanProbeWaitSignal(t, entered, "the first retained probe")
	scanProbeWaitSignal(t, entered, "the second retained probe")
	closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := store.Close(closeCtx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close with retained workers = %v", err)
	}
	select {
	case <-store.done:
		t.Fatal("Store retired ownership before actual probes returned")
	default:
	}
	if counts := scanProbeOpenDescriptors(t, paths); !reflect.DeepEqual(counts, []int{1, 1}) {
		t.Fatalf("cancellation retired borrowed inputs early: %v", counts)
	}
	releaseOnce.Do(func() { close(release) })
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finishCancel()
	if err := store.Close(finishCtx); err != nil {
		t.Fatal(err)
	}
	if counts := scanProbeOpenDescriptors(t, paths); !reflect.DeepEqual(counts, []int{0, 0}) {
		t.Fatalf("actual worker retirement leaked inputs: %v", counts)
	}
}

func TestScanProbePipelineIntegrationRenameClaimSurvivesReverseCompletion(t *testing.T) {
	first, second, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enabled atomic.Bool
	var releaseOnce sync.Once
	var firstName, secondName string
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if enabled.Load() {
			switch filepath.Base(file.Name()) {
			case firstName:
				close(first)
				select {
				case <-release:
				case <-ctx.Done():
					return media.Info{}, ctx.Err()
				}
			case secondName:
				defer close(second)
			}
		}
		return scanProbeFixtureInfo(file)
	})
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	old := libraryIntegrationFile(t, root, "movies/Old.mp4", "video:claim")
	library := libraryIntegrationCreate(t, ctx, store, "Pipeline claims", "movies", filepath.Dir(old))
	initial := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if initial.Error != "" || initial.Added != 1 {
		t.Fatalf("initial scan = %+v", initial)
	}
	var oldID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND type='Movie'", library.ID).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(filepath.Dir(old), "A.mp4"), filepath.Join(filepath.Dir(old), "B.mp4")
	if err := os.Rename(old, a); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	names := scanProbeFixtureNativeMediaNames(t, filepath.Dir(old), 2)
	firstName, secondName = names[0], names[1]
	enabled.Store(true)
	job, err := store.StartScanWithOptions(ctx, library.ID, ScanOptions{ForceProbe: true})
	if err != nil {
		t.Fatal(err)
	}
	scanProbeWaitSignal(t, first, "the first rename claimant")
	scanProbeWaitSignal(t, second, "the faster competing identity")
	releaseOnce.Do(func() { close(release) })
	job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
	if job.Error != "" || job.Scanned != 2 || job.Added != 1 || job.Updated != 1 {
		t.Fatalf("rename scan = %+v", job)
	}
	var firstID, secondID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path=$2", library.ID, firstName).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path=$2", library.ID, secondName).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	if firstID != oldID || secondID == oldID || firstID == secondID {
		t.Fatalf("probe completion reordered rename claims: old=%s native-first=%s:%s native-second=%s:%s", oldID, firstName, firstID, secondName, secondID)
	}
}

func TestScanProbePipelineIntegrationFlushesBeforeFolderHierarchy(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var blockedName string
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == blockedName {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return media.Info{}, ctx.Err()
			}
		}
		return scanProbeFixtureInfo(file)
	})
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	directory := filepath.Join(root, "movies")
	for _, name := range []string{"Entry01", "Entry02", "Entry03", "Entry04"} {
		libraryIntegrationFile(t, root, "movies/"+name+".mp4", "video:"+name)
	}
	// Turn the native last file into a physical folder, then verify the actual
	// post-change order. Directory names may end in .mp4; the walker classifies
	// directories by their type, not by the media extension of their basename.
	names := scanProbeFixtureNativeMediaNames(t, directory, 4)
	folderName := names[len(names)-1]
	folderPath := filepath.Join(directory, folderName)
	if err := os.Remove(folderPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folderPath, 0700); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationFile(t, directory, filepath.Join(folderName, "Nested.mp4"), "video:Nested")
	entries := scanProbeFixtureNativeEntries(t, directory)
	if len(entries) > 0 && entries[0].Name() == folderName {
		// Recreating the folder can move it before the retained media entries.
		// Recreate one media entry through the same insertion path, then prove
		// the final native transition before starting the production scanner.
		mediaName := names[0]
		if err := os.Remove(filepath.Join(directory, mediaName)); err != nil {
			t.Fatal(err)
		}
		libraryIntegrationFile(t, directory, mediaName, "video:"+strings.TrimSuffix(mediaName, filepath.Ext(mediaName)))
		entries = scanProbeFixtureNativeEntries(t, directory)
	}
	for i, entry := range entries {
		if entry.Name() == folderName {
			if !entry.IsDir() || i == 0 || entries[i-1].IsDir() || extensionKind(entries[i-1].Name()) == "" {
				t.Fatalf("fixture has no native media-file transition before its physical folder: %v", entries)
			}
			blockedName = entries[i-1].Name()
			break
		}
	}
	if blockedName == "" {
		t.Fatalf("native fixture enumeration omitted its physical folder: %v", entries)
	}
	t.Logf("native hierarchy barrier file=%s folder=%s", blockedName, folderName)
	library := libraryIntegrationCreate(t, ctx, store, "Pipeline hierarchy", "movies", directory)
	job, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	scanProbeWaitSignal(t, entered, "the file before folder recursion")
	var folders int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND relative_path=$2", library.ID, folderName).Scan(&folders); err != nil || folders != 0 {
		t.Fatalf("folder crossed its earlier publication barrier: count=%d error=%v", folders, err)
	}
	releaseOnce.Do(func() { close(release) })
	job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
	if job.Error != "" || job.Scanned != 4 || job.Added != 4 {
		t.Fatalf("hierarchy scan = %+v", job)
	}
	var correct bool
	if err := pool.QueryRow(ctx, `SELECT child.parent_id=folder.id FROM items child JOIN items folder
		ON folder.library_id=child.library_id AND folder.relative_path=$2
		WHERE child.library_id=$1 AND child.relative_path=$3`, library.ID, folderName, filepath.ToSlash(filepath.Join(folderName, "Nested.mp4"))).Scan(&correct); err != nil || !correct {
		t.Fatalf("child lost its serial physical hierarchy: correct=%t error=%v", correct, err)
	}
}

func TestScanProbeFactsBudget(t *testing.T) {
	for _, test := range []struct {
		label string
		value any
		fit   bool
	}{
		{"small", media.Info{Container: "matroska", Streams: []media.Stream{{Codec: "h264"}}}, true},
		{"large-string", media.Info{Container: strings.Repeat("x", 4096)}, false},
		{"large-container", media.Info{Streams: make([]media.Stream, 100)}, false},
		{"timestamp", storedFile{modified: scanProbeTestTime(time.Now())}, true},
	} {
		t.Run(test.label, func(t *testing.T) {
			if got := scanProbeFactsFit(test.value, 4096); got != test.fit {
				t.Fatalf("facts fit = %t, want %t", got, test.fit)
			}
		})
	}
}

func scanProbeTestTime(value time.Time) *time.Time { return &value }

func TestScanInputInspectionRejectionPreservesMixedFailures(t *testing.T) {
	rejection := &scanInputInspectionRejection{err: errScanProbeSourceChanged}
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"business-only", rejection, true},
		{"nested-business-only", errors.Join(errors.Join(rejection)), true},
		{"unknown-retirement", errors.Join(rejection, media.ErrProcessRetirementUnknown), false},
		{"close-fault", errors.Join(rejection, scanReadFailure(errors.New("actual resource close failed"))), false},
		{"release-fault", errors.Join(rejection, ErrUnavailable), false},
		{"foreign-rejection", errors.Join(rejection, &scanInputInspectionRejection{err: errScanProbeSourceChanged}), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := scanInputInspectionRejectedOnly(test.err, rejection); got != test.want {
				t.Fatalf("inspection rejection classification = %t, want %t: %v", got, test.want, test.err)
			}
		})
	}
}

func TestScanProbeReadyResultRejectsOversizedFactsBeforeHandoff(t *testing.T) {
	ready := make(chan scanProbeResult, 1)
	var calls atomic.Int64
	prober := scanProberFunc(func(context.Context, *os.File) (media.Info, error) {
		calls.Add(1)
		return media.Info{Container: strings.Repeat("x", scanProbeFactsBytes)}, nil
	})
	fixture := primaryScanReadFixtureAt(t, prober, "")
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	fixture.prepare(t)
	input := &scannedMediaInput{primary: fixture.input, file: fixture.file}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ready <- runScanProbe(fixture.ctx, prober, input)
	}()
	t.Cleanup(func() {
		fixture.state.task.cancel()
		scanProbeWaitSignal(t, finished, "actual over-budget source worker retirement")
	})
	select {
	case result := <-ready:
		if !errors.Is(result.err, errScanProbeFactsBudget) || !reflect.DeepEqual(result.info, media.Info{}) {
			t.Fatalf("over-budget facts survived ready handoff: info bytes=%d error=%v", len(result.info.Container), result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("over-budget result handoff blocked")
	}
	if calls.Load() != 1 {
		t.Fatalf("over-budget fixture did not reach the actual joined backend: %d", calls.Load())
	}
	if err := input.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("over-budget actual input descriptor did not retire: %v", err)
	}
	if originalMediaReadGovernor.Stats() != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
		t.Fatal("over-budget actual input retained phase or owner charges")
	}
}

type scanProbeBlockedCapture struct {
	rootBindingWriteCapture
	observation *storageObservationLifetime
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (capture *scanProbeBlockedCapture) Revalidate(ctx context.Context) error {
	capture.observation.mu.Lock()
	observing := capture.observation.active != 0
	capture.observation.mu.Unlock()
	if !observing {
		return capture.rootBindingWriteCapture.Revalidate(ctx)
	}
	capture.once.Do(func() { close(capture.entered) })
	<-capture.release // Model a named-storage syscall that cannot observe cancel.
	return capture.rootBindingWriteCapture.Revalidate(ctx)
}

func TestScanProbePipelineIntegrationTimedOutProofCancelsSiblingAndRetainsBorrowedFiles(t *testing.T) {
	siblingEntered, siblingCancelled := make(chan struct{}), make(chan struct{})
	var siblingName string
	ctx, pool, store, root, _ := libraryIntegrationStore(t, scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == siblingName {
			close(siblingEntered)
			<-ctx.Done()
			close(siblingCancelled)
			return media.Info{}, ctx.Err()
		}
		return scanProbeFixtureInfo(file)
	}))
	path := libraryIntegrationFile(t, root, "movies/A.mp4", "video:A")
	siblingPath := libraryIntegrationFile(t, root, "movies/B.mp4", "video:B")
	names := scanProbeFixtureNativeMediaNames(t, filepath.Dir(path), 2)
	path, siblingPath = filepath.Join(filepath.Dir(path), names[0]), filepath.Join(filepath.Dir(path), names[1])
	siblingName = names[1]
	library := libraryIntegrationCreate(t, ctx, store, "Pipeline proof", "movies", filepath.Dir(path))
	task, _ := scanUnchangedProgressTask(t, ctx, pool, library)
	store.mu.Lock()
	store.active[task.job.ID] = task
	store.mu.Unlock()
	var registered libraryRoot
	if err := pool.QueryRow(ctx, "SELECT id,library_id,path,allowed_path,relative_path FROM library_roots WHERE library_id=$1", library.ID).
		Scan(&registered.id, &registered.libraryID, &registered.path, &registered.allowedPath, &registered.relativePath); err != nil {
		t.Fatal(err)
	}
	pass, err := store.prepareScanReconciliation(task, []libraryRoot{registered})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := pass.openRoot(store, registered)
	if err != nil {
		_ = pass.Close()
		t.Fatal(err)
	}
	state := &scanState{store: store, task: task, library: library, root: registered, opened: opened,
		reconciliationPass: pass, reconciliation: pass.collector()}
	if err := state.startThemeScan(); err != nil {
		_ = opened.Close()
		_ = pass.Close()
		t.Fatal(err)
	}
	if err := state.startExtraScan(); err != nil {
		_ = opened.Close()
		_ = pass.Close()
		t.Fatal(err)
	}
	capture := pass.byRoot[registered.id]
	if capture == nil || capture.status != RootBindingVerified {
		_ = opened.Close()
		_ = pass.Close()
		t.Fatal("proof fixture has no independently verified capture")
	}
	gate := &scanProbeBlockedCapture{rootBindingWriteCapture: capture.capture,
		observation: &capture.observation, entered: make(chan struct{}), release: make(chan struct{})}
	window := state.newScanProbeWindow()
	if window == nil {
		_ = opened.Close()
		_ = pass.Close()
		t.Fatal("proof fixture has no bounded primary scan window")
	}
	var releaseOnce sync.Once
	var proveOnce sync.Once
	prove := make(chan struct{})
	ready := make(chan error, 1)
	finished, result := make(chan struct{}), make(chan error, 1)
	go func() {
		defer close(finished)
		defer pass.Close()
		defer opened.Close()
		result <- func() (resultErr error) {
			defer func() { resultErr = errors.Join(resultErr, window.close()) }()
			for _, name := range names {
				if err := window.submit(name, "video", hierarchy{parentID: library.ID}); err != nil {
					ready <- err
					return err
				}
			}
			// Finish the first input's new preparation and post-probe source proofs
			// before installing a gate for its original final publication proof.
			first := <-window.pending[0].result
			window.pending[0].result <- first
			ready <- first.err
			select {
			case <-prove:
			case <-task.ctx.Done():
				return task.ctx.Err()
			}
			return window.flush()
		}()
	}()
	t.Cleanup(func() {
		task.cancel()
		releaseOnce.Do(func() { close(gate.release) })
		proveOnce.Do(func() { close(prove) })
		scanProbeWaitSignal(t, finished, "actual proof retirement")
		store.mu.Lock()
		delete(store.active, task.job.ID)
		store.mu.Unlock()
	})
	scanProbeWaitSignal(t, siblingEntered, "the retained sibling probe")
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("new source preparation failed before the final proof fixture: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("first source did not complete its joined probe and fresh proof")
	}
	capture.capture = gate
	proveOnce.Do(func() { close(prove) })
	scanProbeWaitSignal(t, gate.entered, "the authoritative named-storage proof")
	// Let the proof's own five-second limit expire while the scan's task context
	// remains live. The error path must cancel the sibling before it waits for
	// the first file's uninterruptible observation and retained descriptors.
	scanProbeWaitSignal(t, siblingCancelled, "sibling cancellation after the proof timeout")
	if err := task.ctx.Err(); err != nil {
		t.Fatalf("proof fixture cancelled the whole task instead of its window: %v", err)
	}
	writerCtx, writerCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer writerCancel()
	// The interrupted proof releases the protected database owner even though
	// its actual filesystem worker and the scan's borrowed file still exist.
	if err := store.WithOwnedTx(writerCtx, func(tx OwnedTx) error {
		var value int
		return tx.QueryRow("SELECT 1").Scan(&value)
	}); err != nil {
		t.Fatalf("timed-out proof retained the database writer: %v", err)
	}
	select {
	case <-finished:
		t.Fatal("scan window returned while a proof still borrowed its root and file")
	default:
	}
	if counts := scanProbeOpenDescriptors(t, []string{path, siblingPath}); !reflect.DeepEqual(counts, []int{1, 1}) {
		t.Fatalf("interrupted proof input descriptor = %v", counts)
	}
	if _, err := opened.Lstat("."); err != nil {
		t.Fatalf("proof timeout retired the borrowed root early: %v", err)
	}
	releaseOnce.Do(func() { close(gate.release) })
	scanProbeWaitSignal(t, finished, "joined named-storage proof")
	if err := <-result; !errors.Is(err, errStorageObservationUnavailable) {
		t.Fatalf("interrupted scan window result = %v", err)
	}
	if counts := scanProbeOpenDescriptors(t, []string{path, siblingPath}); !reflect.DeepEqual(counts, []int{0, 0}) {
		t.Fatalf("proof retirement leaked input descriptor: %v", counts)
	}
	var changed int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND type='Movie'", library.ID).Scan(&changed); err != nil || changed != 0 {
		t.Fatalf("interrupted proof committed media: count=%d error=%v", changed, err)
	}
	// Resolve the manually admitted test relation before fixture ownership is
	// retired; this test deliberately did not put it in the production queue.
	task.cancel()
	if err := store.finishTask(task, "Cancelled", "Scan cancelled"); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
}
