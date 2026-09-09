//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func TestOpenMediaEnforcesCurrentUserAndLibraryPermissions(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, pool, store := fixture.ctx, fixture.pool, fixture.store
	secretPath := libraryIntegrationFile(t, fixture.allowedRoot, "private/Secret.mkv", "video:private-source")
	private := libraryIntegrationCreate(t, ctx, store, "Private", "movies", filepath.Dir(secretPath))
	libraryIntegrationScan(t, ctx, store, private.ID, "Completed")
	secret := libraryIntegrationItemByPath(t, libraryIntegrationQuery(t, ctx, store, Query{
		UserID: fixture.userID, ParentID: private.ID, Recursive: true, IncludeItemTypes: []string{"Movie"},
	}).Items, secretPath)
	libraryIntegrationUser(t, ctx, pool, "source-restricted", false, false, []string{fixture.library.ID})
	libraryIntegrationUser(t, ctx, pool, "source-none", false, false, nil)
	libraryIntegrationUser(t, ctx, pool, "source-disabled", true, true, nil)
	libraryIntegrationUser(t, ctx, pool, "source-admin", false, false, nil)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true WHERE id IN ('source-admin', 'source-disabled')"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		userID, itemID string
		want           error
	}{
		{"source-restricted", fixture.item.ID, nil},
		{"source-restricted", secret.ID, ErrNotFound},
		{"source-restricted", "missing-item", ErrNotFound},
		{"source-none", fixture.item.ID, ErrNotFound},
		{"source-disabled", fixture.item.ID, ErrForbidden},
		{"missing-user", fixture.item.ID, ErrForbidden},
		{"source-admin", secret.ID, nil},
	} {
		t.Run(test.userID+"-"+test.itemID, func(t *testing.T) {
			mediaSourceTestOpenError(t, ctx, store, test.userID, test.itemID, "", test.want)
		})
	}
	// Hidden malformed media must be rejected before its JSON is decoded.
	if _, err := pool.Exec(ctx, `UPDATE items SET media = '{"Container":17}'::jsonb WHERE id = $1`, secret.ID); err != nil {
		t.Fatal(err)
	}
	mediaSourceTestOpenError(t, ctx, store, "source-restricted", secret.ID, "", ErrNotFound)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb
		WHERE id = 'source-restricted'`); err != nil {
		t.Fatal(err)
	}
	mediaSourceTestOpenError(t, ctx, store, "source-restricted", fixture.item.ID, "", ErrNotFound)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = $1", fixture.userID); err != nil {
		t.Fatal(err)
	}
	mediaSourceTestOpenError(t, ctx, store, fixture.userID, fixture.item.ID, "", ErrForbidden)
}

func TestOpenMediaAppliesPlaybackPolicyToAdministrators(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	for _, administrator := range []bool{false, true} {
		for _, test := range []struct {
			name, policy string
			want         error
		}{
			{"missing-default", `{"EnableAllFolders":true}`, nil},
			{"explicit-true", `{"EnableAllFolders":true,"EnableMediaPlayback":true}`, nil},
			{"explicit-false", `{"EnableAllFolders":true,"EnableMediaPlayback":false}`, ErrForbidden},
			{"null", `{"EnableAllFolders":true,"EnableMediaPlayback":null}`, ErrForbidden},
			{"string", `{"EnableAllFolders":true,"EnableMediaPlayback":"true"}`, ErrForbidden},
			{"number", `{"EnableAllFolders":true,"EnableMediaPlayback":1}`, ErrForbidden},
			{"array", `{"EnableAllFolders":true,"EnableMediaPlayback":[]}`, ErrForbidden},
			{"object", `{"EnableAllFolders":true,"EnableMediaPlayback":{}}`, ErrForbidden},
		} {
			t.Run(fmt.Sprintf("administrator-%t/%s", administrator, test.name), func(t *testing.T) {
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_administrator = $2, policy = $3::jsonb WHERE id = $1`,
					fixture.userID, administrator, test.policy); err != nil {
					t.Fatal(err)
				}
				mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, "", test.want)
			})
		}
	}
}

func TestOpenMediaUsesIndexedSourceMetadataWithoutReadingTheBody(t *testing.T) {
	probe := &libraryFixtureProber{}
	fixture := mediaSourceTestCatalog(t, scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		info, err := probe.ProbeFile(ctx, file)
		info.Container = "mp4"
		return info, err
	}))
	// A watch observes content reads without allocating a large media fixture or
	// assuming a storage speed. Opening a file and reading its metadata do not
	// emit IN_ACCESS.
	watcher, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatalf("create media access watch: %v", err)
	}
	defer syscall.Close(watcher)
	if _, err := syscall.InotifyAddWatch(watcher, fixture.path, syscall.IN_ACCESS); err != nil {
		t.Fatalf("watch indexed media content reads: %v", err)
	}
	beforeProbeCalls := len(probe.calls())
	file, source, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, "")
	if err != nil {
		t.Fatalf("open indexed default media source: %v", err)
	}
	defer file.Close()
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 0 {
		t.Fatalf("media source did not start at offset zero: offset = %d, error = %v", offset, err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if source.Item.ID != fixture.item.ID || source.Item.Path != fixture.path || source.Item.Media == nil ||
		source.SourceID != media.SourceID(fixture.item.ID) || source.Size != info.Size() ||
		!source.ModifiedAt.Equal(catalogModifiedTime(info)) || len(source.ETag) < 3 ||
		!strings.HasPrefix(source.ETag, `"`) || !strings.HasSuffix(source.ETag, `"`) {
		t.Fatalf("media source lost its indexed identity or file metadata: %+v", source)
	}
	if source.Container != "mp4" || source.MIMEType != "video/mp4" {
		t.Errorf("the .mkv filename overrode the probed MP4 container: %+v", source)
	}
	var events [4096]byte
	if count, err := syscall.Read(watcher, events[:]); count > 0 || (err != nil && !errors.Is(err, syscall.EAGAIN)) {
		t.Errorf("OpenMedia read media content or access monitoring failed: bytes = %d, error = %v", count, err)
	}
	if len(probe.calls()) != beforeProbeCalls {
		t.Error("opening an indexed source repeated media probing")
	}
	if data, err := io.ReadAll(file); err != nil || string(data) != fixture.contents {
		t.Errorf("source descriptor did not return its original media bytes: %q, %v", data, err)
	}
	if count, err := syscall.Read(watcher, events[:]); err != nil || count == 0 {
		t.Fatalf("the media access watch did not observe the explicit content read: bytes = %d, error = %v", count, err)
	}
	second, sameSource, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, source.SourceID)
	if err != nil {
		t.Fatalf("open explicit original media source: %v", err)
	}
	defer second.Close()
	if sameSource.ETag != source.ETag || sameSource.SourceID != source.SourceID || sameSource.Size != source.Size ||
		!sameSource.ModifiedAt.Equal(source.ModifiedAt) {
		t.Errorf("unchanged source metadata was unstable: first = %+v, second = %+v", source, sameSource)
	}
	for _, wrong := range []string{media.SourceID("another-item"), fixture.item.ID, source.SourceID + ".mp4", "https://example.invalid/movie.mkv"} {
		mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, wrong, ErrNotFound)
	}

	changed := info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(fixture.path, changed, changed); err != nil {
		t.Fatal(err)
	}
	mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, "", ErrUnavailable)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	updatedFile, updated, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, source.SourceID)
	if err != nil {
		t.Fatalf("open rescanned original media source: %v", err)
	}
	defer updatedFile.Close()
	if updated.SourceID != source.SourceID || updated.Item.ID != source.Item.ID || updated.ETag == source.ETag ||
		!updated.ModifiedAt.Equal(changed.UTC().Truncate(time.Microsecond)) {
		t.Errorf("rescan did not update the snapshot tag while retaining source identity: before = %+v, after = %+v", source, updated)
	}
}

func TestOpenMediaReturnsIndexedAudioSource(t *testing.T) {
	ctx, _, store, allowed, userID := libraryIntegrationStore(t, mediaSourceTestProber{inner: &libraryFixtureProber{}})
	const contents = "audio:original-track"
	path := libraryIntegrationFile(t, allowed, "music/Artist/Album/01 Track.flac", contents)
	library := libraryIntegrationCreate(t, ctx, store, "Music", "music", filepath.Join(allowed, "music"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := libraryIntegrationItemByPath(t, libraryIntegrationQuery(t, ctx, store, Query{
		UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Audio"},
	}).Items, path)
	file, source, err := store.OpenMedia(ctx, userID, item.ID, media.SourceID(item.ID))
	if err != nil {
		t.Fatalf("open indexed audio source: %v", err)
	}
	defer file.Close()
	if source.Item.Type != "Audio" || source.Container != "flac" || source.MIMEType != "audio/flac" || source.Size != int64(len(contents)) {
		t.Errorf("audio source metadata = %+v", source)
	}
	if data, err := io.ReadAll(file); err != nil || string(data) != contents {
		t.Errorf("audio source descriptor did not start at its first byte: %q, %v", data, err)
	}
}

func TestOpenMediaRejectsCatalogItemsWithoutPlayableSources(t *testing.T) {
	for _, test := range []struct {
		name, statement string
	}{
		{"folder", "UPDATE items SET is_folder = true WHERE id = $1"},
		{"sql-null-media", "UPDATE items SET media = NULL WHERE id = $1"},
		{"json-null-media", "UPDATE items SET media = 'null'::jsonb WHERE id = $1"},
		{"missing-root", "UPDATE items SET root_id = NULL WHERE id = $1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			if _, err := fixture.pool.Exec(fixture.ctx, test.statement, fixture.item.ID); err != nil {
				t.Fatal(err)
			}
			mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, "", ErrNotFound)
			mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.library.ID, "", ErrNotFound)
		})
	}
	for _, sameLibrary := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrong-root/same-library-%t", sameLibrary), func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			other := imageStoreTestRoot(t, fixture.ctx, fixture.pool, fixture.store, fixture.allowedRoot, "other")
			if sameLibrary {
				if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE library_roots SET library_id = $1 WHERE id = $2", fixture.library.ID, other.id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE items SET root_id = $1 WHERE id = $2", other.id, fixture.item.ID); err != nil {
				t.Fatal(err)
			}
			want := ErrNotFound
			if sameLibrary {
				want = ErrUnavailable
			}
			mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, "", want)
		})
	}
}

func TestOpenMediaRejectsInvalidStoredFileMetadata(t *testing.T) {
	for _, test := range []struct {
		name, statement string
	}{
		{"unrelated-item-path", "UPDATE items SET path = '/unrelated/movie.mkv' WHERE id = $1"},
		{"relative-traversal", "UPDATE items SET relative_path = '../movie.mkv' WHERE id = $1"},
		{"absolute-relative-path", "UPDATE items SET relative_path = '/movie.mkv' WHERE id = $1"},
		{"backslash-relative-path", "UPDATE items SET relative_path = 'Nested' || chr(92) || 'Feature.mkv' WHERE id = $1"},
		{"url-relative-path", "UPDATE items SET relative_path = 'https://example.invalid/movie.mkv' WHERE id = $1"},
		{"empty-identity", "UPDATE items SET file_identity = '' WHERE id = $1"},
		{"empty-file-size", "UPDATE items SET file_size = 0 WHERE id = $1"},
		{"missing-modification-time", "UPDATE items SET modified_at = NULL WHERE id = $1"},
		{"legacy-probe-version", "UPDATE items SET media = jsonb_set(media, '{ProbeVersion}', '0'::jsonb) WHERE id = $1"},
		{"missing-change-time", "UPDATE items SET media = media - 'FileChangeTimeNs' WHERE id = $1"},
		{"mismatched-root-path", "UPDATE library_roots SET path = '/unrelated/root' WHERE id = (SELECT root_id FROM items WHERE id = $1)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			if _, err := fixture.pool.Exec(fixture.ctx, test.statement, fixture.item.ID); err != nil {
				t.Fatal(err)
			}
			mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, "", ErrUnavailable)
		})
	}
}

func TestOpenMediaChangeTimeDetectsWritesWithRestoredModificationTime(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	file, beforeSource, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := file.Stat()
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(fixture.contents)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(fixture.path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fixture.path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(fixture.path)
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() ||
		!after.ModTime().Equal(before.ModTime()) || media.FileChangeTime(before) == media.FileChangeTime(after) {
		t.Fatalf("change-time fixture did not preserve inode, size, and modification time: %v", err)
	}
	mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, "", ErrUnavailable)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	opened, rescanned, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, beforeSource.SourceID)
	if err != nil {
		t.Fatalf("open source after change-time-driven rescan: %v", err)
	}
	defer opened.Close()
	if rescanned.ETag == beforeSource.ETag || rescanned.SourceID != beforeSource.SourceID ||
		rescanned.Item.ID != beforeSource.Item.ID || !rescanned.ModifiedAt.Equal(beforeSource.ModifiedAt) ||
		rescanned.Item.Media == nil || rescanned.Item.Media.FileChangeTimeNs != media.FileChangeTime(after) {
		t.Errorf("change-time rescan did not update the snapshot while retaining its source identity: before = %+v, after = %+v", beforeSource, rescanned)
	}
	if data, err := io.ReadAll(opened); err != nil || !reflect.DeepEqual(data, changed) {
		t.Errorf("rescanned source did not deliver the newly indexed bytes: %q, %v", data, err)
	}
}

func TestOpenMediaRejectsChangedAndNonregularFiles(t *testing.T) {
	for _, change := range []string{"missing", "size", "mtime", "inode", "directory", "fifo"} {
		t.Run(change, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			before, err := os.Stat(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "size":
				err = os.Truncate(fixture.path, before.Size()+1)
			case "mtime":
				modified := before.ModTime().Add(2 * time.Second)
				err = os.Chtimes(fixture.path, modified, modified)
			case "inode":
				if err = os.Rename(fixture.path, fixture.path+".original"); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(fixture.path, []byte(fixture.contents), 0600); err != nil {
					t.Fatal(err)
				}
				err = os.Chtimes(fixture.path, before.ModTime(), before.ModTime())
				if err == nil {
					after, statErr := os.Stat(fixture.path)
					if statErr != nil || os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
						t.Fatalf("replacement inode fixture did not preserve size and time: %v", statErr)
					}
				}
			default:
				if err = os.Remove(fixture.path); err != nil {
					t.Fatal(err)
				}
				if change == "directory" {
					err = os.Mkdir(fixture.path, 0700)
				} else if change == "fifo" {
					err = syscall.Mkfifo(fixture.path, 0600)
					t.Cleanup(func() {
						if descriptor, err := syscall.Open(fixture.path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
							_ = syscall.Close(descriptor)
						}
					})
				}
			}
			if err != nil {
				t.Fatalf("create %s media replacement: %v", change, err)
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 2*time.Second)
			defer cancel()
			mediaSourceTestOpenError(t, ctx, fixture.store, fixture.userID, fixture.item.ID, "", ErrUnavailable)
		})
	}
}

func TestOpenMediaRejectsSymbolicLinksAtEveryPathLevel(t *testing.T) {
	for _, level := range []string{"leaf", "ancestor", "registered-root"} {
		t.Run(level, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			path := fixture.path
			if level == "ancestor" {
				path = filepath.Dir(path)
			} else if level == "registered-root" {
				path = fixture.library.Paths[0]
			}
			original := path + ".original"
			if err := os.Rename(path, original); err != nil {
				t.Fatal(err)
			}
			// Resolving the link would still reach the indexed inode and bytes;
			// rejection therefore exercises the complete no-symlink path policy.
			if err := os.Symlink(original, path); err != nil {
				t.Fatal(err)
			}
			mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, "", ErrUnavailable)
		})
	}
}

func TestMediaSourceWorkersRetainSlotsAndCloseLateDescriptors(t *testing.T) {
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	slots := make(chan struct{}, 4)
	started, release := make(chan struct{}, 4), make(chan struct{})
	var releaseOnce sync.Once
	var count atomic.Int32
	results := make(chan mediaSourceWorkerTestResult, 4)
	files := make([]*os.File, 0, 4)
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		if !imageStoreWaitWorkerCleanup(slots) {
			t.Error("cancelled media workers did not finish cleanup")
		}
		for _, file := range files {
			_ = file.Close()
		}
	})
	for range 4 {
		file, err := os.CreateTemp(directory, "late-media-*.mkv")
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
		go func() {
			opened, source, err := runMediaSourceWorker(ctx, slots, func() (*os.File, MediaFile, error) {
				count.Add(1)
				started <- struct{}{}
				<-release
				return file, MediaFile{SourceID: "fixture-source"}, nil
			})
			results <- mediaSourceWorkerTestResult{file: opened, source: source, err: err}
		}()
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("four media workers did not enter their blocking operations")
		}
	}
	cancel()
	for range 4 {
		result := mediaSourceReceiveWorkerResult(t, results)
		if result.file != nil {
			_ = result.file.Close()
			t.Error("cancelled media worker delivered a descriptor")
		}
		if !errors.Is(result.err, context.Canceled) {
			t.Errorf("cancelled media worker error = %v", result.err)
		}
	}
	queuedCtx, cancelQueue := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelQueue()
	queued := make(chan mediaSourceWorkerTestResult, 1)
	go func() {
		file, source, err := runMediaSourceWorker(queuedCtx, slots, func() (*os.File, MediaFile, error) {
			count.Add(1)
			return nil, MediaFile{}, nil
		})
		queued <- mediaSourceWorkerTestResult{file: file, source: source, err: err}
	}()
	if result := mediaSourceReceiveWorkerResult(t, queued); !errors.Is(result.err, context.DeadlineExceeded) {
		t.Errorf("queued media request error = %v, want context.DeadlineExceeded", result.err)
	}
	if count.Load() != 4 || len(slots) != 4 {
		t.Fatalf("cancellation released active media work early: started = %d, slots = %d", count.Load(), len(slots))
	}
	releaseOnce.Do(func() { close(release) })
	if !imageStoreWaitWorkerCleanup(slots) {
		t.Fatal("late media workers did not finish cleanup")
	}
	for _, file := range files {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("late media descriptor was not closed before releasing its slot: %v", err)
		}
	}
}

func TestMediaSourceWorkerClosesDescriptorsReturnedWithErrors(t *testing.T) {
	slots := make(chan struct{}, 4)
	file, err := os.CreateTemp(t.TempDir(), "failed-media-*.mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cause := errors.New("media fixture read failed")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results := make(chan mediaSourceWorkerTestResult, 1)
	go func() {
		opened, source, err := runMediaSourceWorker(ctx, slots, func() (*os.File, MediaFile, error) {
			return file, MediaFile{SourceID: "fixture-source"}, cause
		})
		results <- mediaSourceWorkerTestResult{file: opened, source: source, err: err}
	}()
	result := mediaSourceReceiveWorkerResult(t, results)
	if result.file != nil {
		_ = result.file.Close()
		t.Error("failed media worker delivered its descriptor")
	}
	if !errors.Is(result.err, cause) {
		t.Errorf("failed media worker did not preserve its error: %v", result.err)
	}
	if !imageStoreWaitWorkerCleanup(slots) {
		t.Fatal("failed media worker did not finish cleanup")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("failed media worker leaked its descriptor: %v", err)
	}
}

type mediaSourceFixture struct {
	ctx                       context.Context
	pool                      *pgxpool.Pool
	store                     *Store
	allowedRoot, userID, path string
	contents                  string
	library                   Library
	item                      Item
}

func mediaSourceTestCatalog(t *testing.T, prober Prober) mediaSourceFixture {
	t.Helper()
	if prober == nil {
		prober = &libraryFixtureProber{}
	}
	ctx, pool, store, allowed, user := libraryIntegrationStore(t, mediaSourceTestProber{inner: prober})
	contents := "video:media-source-fixture"
	path := libraryIntegrationFile(t, allowed, "movies/Nested/Feature.mkv", contents)
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Join(allowed, "movies"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{
		UserID: user, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"},
	})
	item := libraryIntegrationItemByPath(t, items.Items, path)
	if item.Media == nil || item.Media.Size != int64(len(contents)) ||
		item.Media.ProbeVersion < media.CurrentProbeVersion || item.Media.FileChangeTimeNs <= 0 {
		t.Fatalf("media source fixture was not fully probed and indexed: %+v", item)
	}
	return mediaSourceFixture{ctx: ctx, pool: pool, store: store, allowedRoot: allowed, userID: user,
		path: path, contents: contents, library: library, item: item}
}

// Source tests opt into the current probe cache contract without changing the
// older catalog fixtures used by independent scanner and metadata tests.
type mediaSourceTestProber struct{ inner Prober }

func (prober mediaSourceTestProber) CacheVersion() int { return media.CurrentProbeVersion }

func (prober mediaSourceTestProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := prober.inner.ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	info.ProbeVersion = media.CurrentProbeVersion
	info.FileChangeTimeNs = media.FileChangeTime(stat)
	return info, nil
}

func mediaSourceTestOpenError(t *testing.T, ctx context.Context, store *Store, userID, itemID, sourceID string, want error) {
	t.Helper()
	file, source, err := store.OpenMedia(ctx, userID, itemID, sourceID)
	if file != nil {
		defer file.Close()
	}
	if want == nil {
		if err != nil || file == nil || source.Item.ID != itemID || source.SourceID != media.SourceID(itemID) {
			t.Errorf("OpenMedia(%q, %q, %q) = %v, %+v, %v; want an indexed source", userID, itemID, sourceID, file, source, err)
		}
		return
	}
	if !errors.Is(err, want) || file != nil || !reflect.DeepEqual(source, MediaFile{}) {
		t.Errorf("OpenMedia(%q, %q, %q) = %v, %+v, %v; want no descriptor or source and %v", userID, itemID, sourceID, file, source, err, want)
	}
}

type mediaSourceWorkerTestResult struct {
	file   *os.File
	source MediaFile
	err    error
}

func mediaSourceReceiveWorkerResult(t *testing.T, results <-chan mediaSourceWorkerTestResult) mediaSourceWorkerTestResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("media worker caller did not return within the bounded wait")
		return mediaSourceWorkerTestResult{}
	}
}
