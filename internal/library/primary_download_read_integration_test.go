//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestOriginalDownloadReadUsesDownloadAuthorityAndExpectedSnapshot(t *testing.T) {
	for _, test := range []struct {
		name, policy string
		stale        bool
		want         error
	}{
		{"download-without-playback", `{"EnableAllFolders":true,"EnableContentDownloading":true,"EnableMediaPlayback":false}`, false, nil},
		{"playback-without-download", `{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":true}`, false, ErrForbidden},
		{"stale-download-snapshot", `{"EnableAllFolders":true,"EnableContentDownloading":true,"EnableMediaPlayback":false}`, true, ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			fixture.ctx = ctx
			snapshot := primaryDownloadReadTestSnapshot(t, fixture)
			if _, err := fixture.pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", fixture.userID, test.policy); err != nil {
				t.Fatal(err)
			}
			expectedETag := snapshot.ETag
			if test.stale {
				expectedETag += ".stale"
			}
			beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
			beforeIO := originalMediaReadGovernor.Stats()
			result := primaryReadTestReceiveOpen(t, ctx, primaryDownloadReadTestStartOpen(t, ctx, fixture, expectedETag))
			if test.want != nil {
				primaryReadTestOpenFailure(t, result, test.want)
				if test.stale && !errors.Is(result.err, ErrSourceChanged) {
					t.Fatalf("stale download snapshot lost its source-change error: %v", result.err)
				}
			} else {
				primaryDownloadReadTestRequireOwned(t, result, fixture, snapshot)
				if result.source.Item.CanPlay {
					t.Fatal("download authorization granted disabled playback")
				}
				read := primaryReadTestReceiveRead(t, ctx, primaryReadTestStartRead(result.reader, 8))
				if read.err != nil || string(read.data) != fixture.contents[:8] {
					t.Fatalf("authorized download did not read original bytes: bytes=%q error=%v", read.data, read.err)
				}
				playback := primaryReadTestReceiveOpen(t, ctx, primaryReadTestStartOpen(t, ctx, fixture, snapshot.ETag))
				primaryReadTestOpenFailure(t, playback, ErrForbidden)
				if !primaryReadTestClose(t, result.reader) {
					return
				}
			}
			primaryReadTestWaitOwners(t, ctx, beforeOwners)
			if after := originalMediaReadGovernor.Stats(); after != beforeIO {
				t.Fatalf("download constructor or retired reader retained active admission: before=%+v after=%+v", beforeIO, after)
			}
		})
	}
}

func TestOriginalDownloadReadCancellationRetainsActualConstructorUntilCleanup(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(fixture.ctx, 20*time.Second)
	defer cancel()
	fixture.ctx = ctx
	if _, err := fixture.pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	snapshot := primaryDownloadReadTestSnapshot(t, fixture)
	_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	rootActive := func() int {
		mediaSourceAdmission.mu.Lock()
		defer mediaSourceAdmission.mu.Unlock()
		return mediaSourceAdmission.roots[root].active
	}
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeIO := originalMediaReadGovernor.Stats()
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	constructed := make(chan *os.File, 1)
	allowReturn := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(allowReturn) }) }
	call := &primaryReadTestOpenCall{results: make(chan primaryReadTestOpenResult, 1)}
	var observed *os.File
	t.Cleanup(func() {
		cancelCaller()
		release()
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		if !call.received {
			select {
			case result := <-call.results:
				call.received = true
				primaryReadTestRetireOpen(t, result)
			case <-cleanup.Done():
				t.Error("download constructor caller did not finish within bounded cleanup")
			}
		}
		drained := true
		if err := fixture.store.Close(cleanup); err != nil {
			drained = false
			t.Errorf("download constructor did not finish actual Store cleanup: %v", err)
		}
		if observed == nil {
			select {
			case observed = <-constructed:
			default:
			}
		}
		if observed != nil && drained {
			if _, err := observed.Stat(); !errors.Is(err, os.ErrClosed) {
				// Failed retirement evidence is recorded before fallback cleanup.
				t.Errorf("undelivered download descriptor survived actual Store drain: %v", err)
				_ = observed.Close()
			}
		}
	})
	go func() {
		file, source, reader, err := fixture.store.openOriginalReadFor(caller, Subject{UserID: fixture.userID},
			fixture.item.ID, snapshot.SourceID, snapshot.ETag, fixture.store.readDownloadSource,
			func(_ context.Context, selected indexedMediaSource) (*os.File, error) {
				if selected.mediaFile.Item.ID != fixture.item.ID || selected.mediaFile.ETag != snapshot.ETag {
					return nil, errors.New("download constructor received an unrelated snapshot")
				}
				opened, err := os.Open(fixture.path)
				if err != nil {
					return nil, err
				}
				constructed <- opened
				// A constructed descriptor remains owned while its real opener ignores
				// cancellation. The gate has an independent bound for failed tests.
				gate := time.NewTimer(30 * time.Second)
				defer gate.Stop()
				select {
				case <-allowReturn:
					return opened, nil
				case <-gate.C:
					return opened, errors.New("download constructor gate timed out")
				}
			})
		call.results <- primaryReadTestOpenResult{file: file, source: source, reader: reader, err: err}
	}()
	select {
	case observed = <-constructed:
	case <-ctx.Done():
		t.Fatalf("authorized download constructor did not create a descriptor: %v", ctx.Err())
	}
	cancelCaller()
	result := primaryReadTestReceiveOpen(t, ctx, call)
	primaryReadTestOpenFailure(t, result, context.Canceled)
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
	if rootActive() != 1 || originalMediaReadGovernor.Stats() != beforeIO {
		t.Fatal("canceled download released its actual source-open owner or charged an undelivered read")
	}
	if _, err := observed.Stat(); err != nil {
		t.Fatalf("actual constructor descriptor retired before its opener returned: %v", err)
	}
	closeCaller, cancelCloseCaller := context.WithCancel(context.Background())
	cancelCloseCaller()
	if err := fixture.store.Close(closeCaller); !errors.Is(err, context.Canceled) {
		t.Fatalf("Store.Close did not preserve its caller cancellation: %v", err)
	}
	select {
	case <-fixture.store.done:
		t.Fatal("Store.Close finished while the actual download constructor retained its descriptor")
	default:
	}
	if fixture.store.catalogChangesClosed.Load() || rootActive() != 1 {
		t.Fatal("Store shutdown retired catalog resources or the blocked download opener")
	}
	release()
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCleanup()
	if err := fixture.store.Close(cleanup); err != nil {
		t.Fatalf("Store.Close did not join actual download constructor cleanup: %v", err)
	}
	if _, err := observed.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("undelivered download descriptor survived Store drain: %v", err)
	}
	if rootActive() != 0 || originalMediaReadGovernor.Stats() != beforeIO {
		t.Fatal("download constructor cleanup retained source-open or actual-read admission")
	}
}

func TestOriginalDownloadReadSharesOriginalGovernorAndDomainRegistry(t *testing.T) {
	t.Run("shared-active-domain", func(t *testing.T) {
		first := mediaSourceTestCatalog(t, nil)
		_, _, domain := mediaSourceRootAdmissionTestLane(t, first, first.item.ID)
		second := primaryReadTestCatalogAt(t, domain, "download-shared-domain")
		ctx, cancel := context.WithTimeout(first.ctx, 20*time.Second)
		defer cancel()
		first.ctx, second.ctx = ctx, ctx
		beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
		beforeIO := originalMediaReadGovernor.Stats()
		original := primaryReadTestOpenOwned(t, ctx, first, primaryReadTestSnapshot(t, first))
		downloadSnapshot := primaryDownloadReadTestSnapshot(t, second)
		download := primaryReadTestReceiveOpen(t, ctx, primaryDownloadReadTestStartOpen(t, ctx, second, downloadSnapshot.ETag))
		primaryDownloadReadTestRequireOwned(t, download, second, downloadSnapshot)
		releases := primaryReadTestHold(t, ctx, primaryReadTestRoute(t, first), mediaSourceRootOwnerLimit)
		read := primaryReadTestStartRead(download.reader, 8)
		primaryReadTestWaitQueued(t, ctx, beforeIO.Queued+1)
		select {
		case result := <-read:
			t.Fatalf("download read bypassed the original reader domain governor: bytes=%q error=%v", result.data, result.err)
		default:
		}
		if after := originalMediaReadGovernor.Stats(); after.Active != beforeIO.Active+mediaSourceRootOwnerLimit {
			t.Fatalf("download queued read changed the active domain budget: %+v", after)
		}
		releases[0]()
		result := primaryReadTestReceiveRead(t, ctx, read)
		if result.err != nil || string(result.data) != second.contents[:8] {
			t.Fatalf("download did not resume after shared domain admission: bytes=%q error=%v", result.data, result.err)
		}
		for _, release := range releases {
			release()
		}
		if !primaryReadTestClose(t, original.reader) || !primaryReadTestClose(t, download.reader) {
			return
		}
		primaryReadTestWaitOwners(t, ctx, beforeOwners)
		if after := originalMediaReadGovernor.Stats(); after != beforeIO {
			t.Fatalf("retired shared download domain retained admission: before=%+v after=%+v", beforeIO, after)
		}
	})
	t.Run("live-overlap-until-close", func(t *testing.T) {
		first := mediaSourceTestCatalog(t, nil)
		_, _, domain := mediaSourceRootAdmissionTestLane(t, first, first.item.ID)
		second := primaryReadTestCatalogAt(t, filepath.Join(domain, "nested-download-domain"), "download-overlap")
		ctx, cancel := context.WithTimeout(first.ctx, 20*time.Second)
		defer cancel()
		first.ctx, second.ctx = ctx, ctx
		beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
		caller, cancelCaller := context.WithCancel(ctx)
		defer cancelCaller()
		original := primaryReadTestOpenOwned(t, caller, first, primaryReadTestSnapshot(t, first))
		downloadSnapshot := primaryDownloadReadTestSnapshot(t, second)
		cancelCaller()
		blocked := primaryReadTestReceiveOpen(t, ctx, primaryDownloadReadTestStartOpen(t, ctx, second, downloadSnapshot.ETag))
		primaryReadTestOpenFailure(t, blocked, ErrUnavailable)
		primaryReadTestWaitOwners(t, ctx, beforeOwners+1)
		if !primaryReadTestClose(t, original.reader) {
			return
		}
		if _, err := original.file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("original domain claim retired before its descriptor closed: %v", err)
		}
		primaryReadTestWaitOwners(t, ctx, beforeOwners)
		retried := primaryReadTestReceiveOpen(t, ctx, primaryDownloadReadTestStartOpen(t, ctx, second, downloadSnapshot.ETag))
		primaryDownloadReadTestRequireOwned(t, retried, second, downloadSnapshot)
		read := primaryReadTestReceiveRead(t, ctx, primaryReadTestStartRead(retried.reader, 8))
		if read.err != nil || string(read.data) != second.contents[:8] {
			t.Fatalf("closed original domain did not allow the download mapping: bytes=%q error=%v", read.data, read.err)
		}
		if !primaryReadTestClose(t, retried.reader) {
			return
		}
		primaryReadTestWaitOwners(t, ctx, beforeOwners)
	})
}

func TestOriginalDownloadReadSharesOriginalRetainedOwnerLimitUntilClose(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(fixture.ctx, 20*time.Second)
	defer cancel()
	fixture.ctx = ctx
	snapshot := primaryDownloadReadTestSnapshot(t, fixture)
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	if originalMediaReadOwners.Stats().MaximumOwners != 64 || beforeOwners > 62 {
		t.Fatalf("original consumer runtime lacks the expected shared 64-owner budget: %+v", originalMediaReadOwners.Stats())
	}
	beforeIO := originalMediaReadGovernor.Stats()
	originalCaller, cancelOriginal := context.WithCancel(ctx)
	defer cancelOriginal()
	original := primaryReadTestOpenOwned(t, originalCaller, fixture, snapshot)
	releases := make([]func(), 0, 62-beforeOwners)
	for range 62 - beforeOwners {
		owner, err := originalMediaReadOwners.Register(ctx)
		if err != nil {
			t.Fatalf("fill the shared retained consumer budget: %v", err)
		}
		var once sync.Once
		release := func() {
			once.Do(func() {
				if err := owner.Complete(); err != nil {
					t.Errorf("retire shared consumer budget holder: %v", err)
				}
			})
		}
		t.Cleanup(release)
		releases = append(releases, release)
	}
	download := primaryReadTestReceiveOpen(t, ctx, primaryDownloadReadTestStartOpen(t, ctx, fixture, snapshot.ETag))
	primaryDownloadReadTestRequireOwned(t, download, fixture, snapshot)
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != 64 {
		t.Fatalf("original and download consumers did not share all retained registrations: %d", owners)
	}
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("idle mixed consumers charged actual-read admission: before=%+v after=%+v", beforeIO, after)
	}
	cancelOriginal()
	blocked := primaryReadTestReceiveOpen(t, ctx, primaryDownloadReadTestStartOpen(t, ctx, fixture, snapshot.ETag))
	primaryReadTestOpenFailure(t, blocked, ErrBusy)
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != 64 {
		t.Fatalf("cancellation or refused download released a live original consumer: %d", owners)
	}
	if !primaryReadTestClose(t, original.reader) {
		return
	}
	if _, err := original.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("original consumer registration retired before its descriptor closed: %v", err)
	}
	primaryReadTestWaitOwners(t, ctx, 63)
	retried := primaryReadTestReceiveOpen(t, ctx, primaryDownloadReadTestStartOpen(t, ctx, fixture, snapshot.ETag))
	primaryDownloadReadTestRequireOwned(t, retried, fixture, snapshot)
	read := primaryReadTestReceiveRead(t, ctx, primaryReadTestStartRead(retried.reader, 8))
	if read.err != nil || string(read.data) != fixture.contents[:8] {
		t.Fatalf("actual original retirement did not allow the next download: bytes=%q error=%v", read.data, read.err)
	}
	if !primaryReadTestClose(t, download.reader) || !primaryReadTestClose(t, retried.reader) {
		return
	}
	for _, release := range releases {
		release()
	}
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("mixed consumer retirement retained admission: before=%+v after=%+v", beforeIO, after)
	}
}

func primaryDownloadReadTestSnapshot(t *testing.T, fixture mediaSourceFixture) MediaFile {
	t.Helper()
	snapshot, err := fixture.store.readDownloadSource(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, media.SourceID(fixture.item.ID))
	if err != nil || snapshot.mediaFile.ETag == "" {
		t.Fatalf("capture original download response snapshot: %v", err)
	}
	return snapshot.mediaFile
}

func primaryDownloadReadTestStartOpen(t *testing.T, ctx context.Context, fixture mediaSourceFixture, expectedETag string) *primaryReadTestOpenCall {
	t.Helper()
	work, cancel := context.WithCancel(ctx)
	call := &primaryReadTestOpenCall{results: make(chan primaryReadTestOpenResult, 1)}
	t.Cleanup(func() {
		cancel()
		if call.received {
			return
		}
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		select {
		case result := <-call.results:
			primaryReadTestRetireOpen(t, result)
		case <-cleanup.Done():
			t.Error("download source caller did not retire within bounded cleanup")
		}
	})
	go func() {
		file, source, reader, err := fixture.store.OpenOriginalDownloadFor(work, Subject{UserID: fixture.userID},
			fixture.item.ID, media.SourceID(fixture.item.ID), expectedETag)
		call.results <- primaryReadTestOpenResult{file: file, source: source, reader: reader, err: err}
	}()
	return call
}

func primaryDownloadReadTestRequireOwned(t *testing.T, result primaryReadTestOpenResult, fixture mediaSourceFixture, snapshot MediaFile) {
	t.Helper()
	if result.err != nil || result.file == nil || result.reader == nil || result.source.Item.ID != fixture.item.ID ||
		result.source.SourceID != snapshot.SourceID || result.source.ETag != snapshot.ETag || result.source.Size != snapshot.Size {
		t.Fatalf("controlled original download did not deliver its authorized snapshot: file=%v reader=%v source=%+v error=%v",
			result.file, result.reader, result.source, result.err)
	}
}
