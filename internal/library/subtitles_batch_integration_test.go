//go:build linux

package library

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type subtitlesBatchTraceKey struct{}
type subtitlesBatchPublicationKey struct{}

type subtitlesBatchTrace struct {
	queries, begins, commits, sources, owned, publications atomic.Int64
	beforeSnapshot                                         func(context.Context, int64) error
	afterPublication                                       func(context.Context, int64) error
	mu                                                     sync.Mutex
	hookErr                                                error
}

func (trace *subtitlesBatchTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(subtitlesBatchTraceKey{}) != trace {
		return ctx
	}
	trace.queries.Add(1)
	statement := strings.ToLower(strings.TrimSpace(data.SQL))
	if statement == "begin" || strings.HasPrefix(statement, "begin ") {
		position := trace.begins.Add(1)
		if trace.beforeSnapshot != nil {
			err := trace.beforeSnapshot(ctx, position)
			trace.mu.Lock()
			trace.hookErr = errors.Join(trace.hookErr, err)
			trace.mu.Unlock()
		}
	}
	if statement == "commit" {
		trace.commits.Add(1)
	}
	if strings.Contains(data.SQL, "AND i.type IN ('Movie', 'Episode', 'Video', 'Audio')") {
		trace.sources.Add(1)
	}
	if strings.Contains(data.SQL, "FROM item_owned_subtitles s JOIN items i") {
		trace.owned.Add(1)
	}
	if strings.Contains(data.SQL, "FROM media_operations publication WHERE publication.source_item_id=i.id") {
		return context.WithValue(ctx, subtitlesBatchPublicationKey{}, trace.publications.Add(1))
	}
	return ctx
}

func (trace *subtitlesBatchTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	position, ok := ctx.Value(subtitlesBatchPublicationKey{}).(int64)
	if data.Err != nil || !ok || trace.afterPublication == nil {
		return
	}
	err := trace.afterPublication(ctx, position)
	trace.mu.Lock()
	trace.hookErr = errors.Join(trace.hookErr, err)
	trace.mu.Unlock()
}

func (trace *subtitlesBatchTrace) reset() {
	trace.queries.Store(0)
	trace.begins.Store(0)
	trace.commits.Store(0)
	trace.sources.Store(0)
	trace.owned.Store(0)
	trace.publications.Store(0)
}

func subtitlesBatchTraceFixture(t *testing.T, fixture mediaSourceFixture, trace *subtitlesBatchTrace) context.Context {
	t.Helper()
	configuration := fixture.pool.Config()
	configuration.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, reader)
	fixture.store.pool = reader
	t.Cleanup(func() { fixture.store.pool = fixture.pool })
	return context.WithValue(fixture.ctx, subtitlesBatchTraceKey{}, trace)
}

func subtitlesBatchTestCatalog(t *testing.T, sidecars, owned int, profileTimeout ...time.Duration) (mediaSourceFixture, []Subtitle, []SubtitleExpectation) {
	t.Helper()
	var fixture mediaSourceFixture
	if len(profileTimeout) == 0 {
		fixture = mediaSourceTestCatalog(t, nil)
	} else {
		ctx, pool, store, allowed, user := libraryIntegrationStoreWithTimeout(t,
			mediaSourceTestProber{inner: &libraryFixtureProber{}}, profileTimeout[0])
		contents := "video:subtitle-batch-profile"
		path := libraryIntegrationFile(t, allowed, "movies/Nested/Feature.mkv", contents)
		library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Join(allowed, "movies"))
		libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		items := libraryIntegrationQuery(t, ctx, store, Query{
			UserID: user, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"},
		})
		fixture = mediaSourceFixture{ctx: ctx, pool: pool, store: store, allowedRoot: allowed, userID: user,
			path: path, contents: contents, library: library, item: libraryIntegrationItemByPath(t, items.Items, path)}
	}
	languages := []string{"en", "de", "es", "fr", "it", "ja", "ko", "zh"}
	for position := range sidecars {
		codec, data := "srt", subtitleTestSRT
		if position%2 != 0 {
			codec, data = "vtt", subtitleTestVTT
		}
		path := filepath.Join(filepath.Dir(fixture.path), "Feature."+languages[position]+"."+codec)
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if sidecars != 0 {
		libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	}
	for range owned {
		ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	}
	tracks := subtitleTestTracks(t, fixture)
	if len(tracks) != sidecars+owned {
		t.Fatalf("batch fixture has %d tracks, want %d", len(tracks), sidecars+owned)
	}
	expected := make([]SubtitleExpectation, len(tracks))
	for position, track := range tracks {
		expected[position] = SubtitleExpectation{Index: track.Index, Codec: track.Codec, Tag: track.Tag}
	}
	return fixture, tracks, expected
}

type subtitlesBatchFileEvents struct {
	opens, reads int
}

// Directory events separate opens even while two primary descriptors overlap.
// A file-only watch could coalesce those two adjacent IN_OPEN notifications.
func subtitlesBatchWatchDirectory(t *testing.T, directory string) func() map[string]subtitlesBatchFileEvents {
	t.Helper()
	descriptor, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Close(descriptor); err != nil {
			t.Errorf("close subtitle directory watch: %v", err)
		}
	})
	if _, err := syscall.InotifyAddWatch(descriptor, directory, syscall.IN_OPEN|syscall.IN_CLOSE_NOWRITE|syscall.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	return func() map[string]subtitlesBatchFileEvents {
		t.Helper()
		observed := make(map[string]subtitlesBatchFileEvents)
		var buffer [16384]byte
		for {
			count, err := syscall.Read(descriptor, buffer[:])
			if errors.Is(err, syscall.EAGAIN) {
				return observed
			}
			if err != nil || count == 0 {
				t.Fatalf("read subtitle directory events: bytes=%d error=%v", count, err)
			}
			for offset := 0; offset < count; {
				if count-offset < syscall.SizeofInotifyEvent {
					t.Fatal("subtitle directory watch returned a partial event")
				}
				mask := binary.NativeEndian.Uint32(buffer[offset+4 : offset+8])
				length := int(binary.NativeEndian.Uint32(buffer[offset+12 : offset+16]))
				if mask&(syscall.IN_Q_OVERFLOW|syscall.IN_IGNORED|syscall.IN_UNMOUNT) != 0 || length > count-offset-syscall.SizeofInotifyEvent {
					t.Fatal("subtitle directory watch lost its observation")
				}
				name := strings.TrimRight(string(buffer[offset+syscall.SizeofInotifyEvent:offset+syscall.SizeofInotifyEvent+length]), "\x00")
				if name != "" && mask&syscall.IN_ISDIR == 0 {
					events := observed[name]
					if mask&syscall.IN_OPEN != 0 {
						events.opens++
					}
					if mask&syscall.IN_ACCESS != 0 {
						events.reads++
					}
					observed[name] = events
				}
				offset += syscall.SizeofInotifyEvent + length
			}
		}
	}
}

func TestValidateSubtitlesRejectsUnboundedSelectorsBeforeWork(t *testing.T) {
	var store *Store
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.ValidateSubtitlesFor(ctx, Subject{}, "\x00", "\x00", nil); err != nil {
		t.Fatalf("empty subtitle validation performed work: %v", err)
	}
	valid := SubtitleExpectation{Index: 1, Codec: "srt", Tag: strings.Repeat("0", 64)}
	tooMany := make([]SubtitleExpectation, 9)
	for position := range tooMany {
		tooMany[position] = valid
		tooMany[position].Index = position
	}
	for _, test := range []struct {
		name, itemID, sourceID string
		expected               []SubtitleExpectation
	}{
		{"blank-item", " ", "source", []SubtitleExpectation{valid}},
		{"nul-item", "item\x00", "source", []SubtitleExpectation{valid}},
		{"nul-source", "item", "source\x00", []SubtitleExpectation{valid}},
		{"negative-index", "item", "source", []SubtitleExpectation{{Index: -1, Codec: valid.Codec, Tag: valid.Tag}}},
		{"overflow-index", "item", "source", []SubtitleExpectation{{Index: maxSubtitleStreamIndex + 1, Codec: valid.Codec, Tag: valid.Tag}}},
		{"empty-tag", "item", "source", []SubtitleExpectation{{Index: valid.Index, Codec: valid.Codec}}},
		{"duplicate-index", "item", "source", []SubtitleExpectation{valid, valid}},
		{"too-many-tracks", "item", "source", tooMany},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := store.ValidateSubtitlesFor(context.Background(), Subject{UserID: "reader"}, test.itemID, test.sourceID, test.expected); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid batch reached storage: %v", err)
			}
		})
	}
}

func TestValidateSubtitlesSharesAuthorityAndPrimaryDescriptors(t *testing.T) {
	for _, owned := range []int{0, 4, 8} {
		t.Run(fmt.Sprintf("owned-%d", owned), func(t *testing.T) {
			fixture, tracks, expected := subtitlesBatchTestCatalog(t, 8-owned, owned)
			trace := &subtitlesBatchTrace{}
			ctx := subtitlesBatchTraceFixture(t, fixture, trace)
			watch := subtitlesBatchWatchDirectory(t, filepath.Dir(fixture.path))
			subject := Subject{UserID: fixture.userID}
			for _, batch := range []bool{false, true} {
				trace.reset()
				if batch {
					if err := fixture.store.ValidateSubtitlesFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), expected); err != nil {
						t.Fatal(err)
					}
				} else {
					for _, track := range tracks {
						content, err := fixture.store.ReadSubtitleFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index)
						if err != nil || len(content.Data) == 0 || content.Info.Tag != track.Tag {
							t.Fatalf("individual subtitle validation failed: index=%d error=%v", track.Index, err)
						}
					}
				}
				wantSnapshots, wantOpens, ownedSnapshots := int64(16), 16, 2
				if batch {
					wantSnapshots, wantOpens, ownedSnapshots = 3, 2, 3
				}
				events := watch()
				primary := events[filepath.Base(fixture.path)]
				if trace.begins.Load() != wantSnapshots || trace.commits.Load() != wantSnapshots || trace.sources.Load() != wantSnapshots || primary.opens != wantOpens || primary.reads != 0 {
					t.Fatalf("batch=%t repeated or skipped shared proofs: begins=%d commits=%d sources=%d primary_opens=%d primary_reads=%d want_snapshots=%d want_opens=%d",
						batch, trace.begins.Load(), trace.commits.Load(), trace.sources.Load(), primary.opens, primary.reads, wantSnapshots, wantOpens)
				}
				if trace.owned.Load() != int64(owned*ownedSnapshots) {
					t.Fatalf("batch=%t did not validate owned bytes in every snapshot: queries=%d want=%d", batch, trace.owned.Load(), owned*ownedSnapshots)
				}
				for _, track := range tracks {
					if track.Owned {
						continue
					}
					if events[track.Filename].opens != 1 || events[track.Filename].reads == 0 {
						t.Fatalf("batch=%t did not read exactly one sidecar payload: track=%s events=%+v", batch, track.Filename, events[track.Filename])
					}
				}
				t.Logf("subtitle_batch_counts batch=%t tracks=%d owned=%d sql=%d snapshots=%d primary_opens=%d", batch, len(tracks), owned, trace.queries.Load(), trace.begins.Load(), primary.opens)
			}
			indices := make([]int, len(tracks))
			for position, track := range tracks {
				indices[position] = track.Index
			}
			_, metadata, err := fixture.store.readSubtitleSnapshotsFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), indices, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, track := range metadata {
				if len(track.ownedData) != 0 {
					t.Fatalf("validation-only snapshot retained owned payload: index=%d bytes=%d", track.Index, len(track.ownedData))
				}
			}
			trace.reset()
			if err := fixture.store.ValidateSubtitlesFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), expected[:1]); err != nil {
				t.Fatal(err)
			}
			if events := watch(); trace.begins.Load() != 2 || trace.sources.Load() != 2 || events[filepath.Base(fixture.path)].opens != 2 {
				t.Fatalf("single-track validation added a group snapshot: begins=%d sources=%d events=%+v", trace.begins.Load(), trace.sources.Load(), events)
			}
		})
	}
}

func TestValidateSubtitlesRejectsStaleExpectationsBeforeFilesystemWork(t *testing.T) {
	fixture, _, expected := subtitlesBatchTestCatalog(t, 4, 4)
	watch := subtitlesBatchWatchDirectory(t, filepath.Dir(fixture.path))
	for _, kind := range []string{"codec", "unknown-codec", "tag", "missing-index"} {
		t.Run(kind, func(t *testing.T) {
			stale := append([]SubtitleExpectation(nil), expected...)
			last := &stale[len(stale)-1]
			want := ErrSourceChanged
			switch kind {
			case "codec":
				last.Codec = "vtt"
			case "unknown-codec":
				last.Codec = "unsupported"
			case "tag":
				last.Tag = strings.Repeat("0", 64)
			case "missing-index":
				last.Index += 100
				want = ErrNotFound
			}
			if err := fixture.store.ValidateSubtitlesFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, media.SourceID(fixture.item.ID), stale); !errors.Is(err, want) {
				t.Fatalf("stale %s expectation was accepted: %v", kind, err)
			}
			if events := watch(); len(events) != 0 {
				t.Fatalf("stale %s expectation touched payload files: %+v", kind, events)
			}
		})
	}
	for position := range expected {
		if expected[position].Codec == "srt" {
			expected[position].Codec = "subrip"
		}
	}
	if err := fixture.store.ValidateSubtitlesFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, media.SourceID(fixture.item.ID), expected); err != nil {
		t.Fatalf("equivalent subtitle codec aliases were rejected: %v", err)
	}
}

func TestValidateSubtitlesChecksEveryPayloadAndPrimarySnapshot(t *testing.T) {
	for _, kind := range []string{"sidecar-hash", "owned-hash", "sidecar-path", "primary-snapshot"} {
		t.Run(kind, func(t *testing.T) {
			fixture, tracks, expected := subtitlesBatchTestCatalog(t, 4, 4)
			changed := []byte(subtitleTestSRT)
			changed[len(changed)-2] = 'X'
			sidecar := tracks[3]
			path := filepath.Join(filepath.Dir(fixture.path), sidecar.Filename)
			switch kind {
			case "sidecar-hash":
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data[len(data)-2] = 'X'
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, sidecar.ModifiedAt, sidecar.ModifiedAt); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				// Preserve matching metadata so only the persisted hash rejects it.
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_subtitles SET change_time_ns=$3 WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, sidecar.Index, media.FileChangeTime(info)); err != nil {
					t.Fatal(err)
				}
			case "owned-hash":
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_owned_subtitles SET content=$3 WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, tracks[7].Index, changed); err != nil {
					t.Fatal(err)
				}
			case "sidecar-path":
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_subtitles SET relative_path=$3 WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, sidecar.Index, "WrongDirectory/"+sidecar.Filename); err != nil {
					t.Fatal(err)
				}
			case "primary-snapshot":
				if err := os.WriteFile(fixture.path, []byte(fixture.contents+" changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := fixture.store.ValidateSubtitlesFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, media.SourceID(fixture.item.ID), expected)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("invalid %s remained valid in the batch: %v", kind, err)
			}
			if (kind == "sidecar-hash" || kind == "primary-snapshot") && !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("changed %s lost its source-change classification: %v", kind, err)
			}
		})
	}
}

func TestValidateSubtitlesRefreshesAuthorityAndTracksAfterAdmissionWait(t *testing.T) {
	for _, kind := range []string{"disabled-user", "changed-tag", "owned-hash", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			fixture, tracks, expected := subtitlesBatchTestCatalog(t, 4, 4)
			trace := &subtitlesBatchTrace{}
			traced := subtitlesBatchTraceFixture(t, fixture, trace)
			ctx, cancel := context.WithTimeout(traced, 10*time.Second)
			defer cancel()
			hint, _, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			route, err := fixture.store.primaryReadRoute(hint)
			if err != nil {
				t.Fatal(err)
			}
			releases := primaryReadTestHold(t, ctx, route, mediaSourceRootOwnerLimit)
			unblock := func() {
				for _, release := range releases {
					release()
				}
			}
			baselineIO := originalMediaReadGovernor.Stats()
			baselineOwners := originalMediaReadOwners.Stats().RegisteredOwners
			watch := subtitlesBatchWatchDirectory(t, filepath.Dir(fixture.path))
			call, cancelCall := context.WithCancel(ctx)
			done := make(chan struct{})
			var result error
			t.Cleanup(func() {
				cancelCall()
				unblock()
				mediaSourceAdmissionTestWait(t, done, "subtitle batch caller cleanup")
				if !imageStoreWaitWorkerCleanup(subtitleSourceWorkers) {
					t.Error("subtitle batch worker did not retire during cleanup")
				}
			})
			go func() {
				result = fixture.store.ValidateSubtitlesFor(call, Subject{UserID: fixture.userID}, fixture.item.ID, media.SourceID(fixture.item.ID), expected)
				close(done)
			}()
			primaryReadTestWaitQueued(t, ctx, baselineIO.Queued+1)
			if trace.begins.Load() != 1 || trace.commits.Load() != 1 || trace.sources.Load() != 1 || trace.owned.Load() != 4 {
				t.Fatalf("queued batch did not release one complete shared snapshot: begins=%d commits=%d sources=%d owned=%d",
					trace.begins.Load(), trace.commits.Load(), trace.sources.Load(), trace.owned.Load())
			}
			if events := watch(); len(events) != 0 {
				t.Fatalf("queued batch opened payloads before admission: %+v", events)
			}
			want := ErrSourceChanged
			switch kind {
			case "disabled-user":
				if _, err := fixture.pool.Exec(ctx, "UPDATE users SET is_disabled=true WHERE id=$1", fixture.userID); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(fixture.path); err != nil {
					t.Fatal(err)
				}
				want = ErrForbidden
			case "changed-tag":
				if _, err := fixture.pool.Exec(ctx, `UPDATE item_subtitles SET source_hash=repeat('0',64) WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, tracks[3].Index); err != nil {
					t.Fatal(err)
				}
			case "owned-hash":
				changed := []byte(subtitleTestSRT)
				changed[len(changed)-2] = 'X'
				if _, err := fixture.pool.Exec(ctx, `UPDATE item_owned_subtitles SET content=$3 WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, tracks[7].Index, changed); err != nil {
					t.Fatal(err)
				}
				want = ErrUnavailable
			case "canceled":
				cancelCall()
				want = context.Canceled
			}
			if kind != "canceled" {
				unblock()
			}
			mediaSourceAdmissionTestWait(t, done, "subtitle batch admission result")
			if !errors.Is(result, want) {
				t.Fatalf("queued %s batch returned %v, want %v", kind, result, want)
			}
			if !imageStoreWaitWorkerCleanup(subtitleSourceWorkers) {
				t.Fatal("subtitle batch did not retire its worker after rejection")
			}
			primaryReadTestWaitOwners(t, ctx, baselineOwners)
			if events := watch(); len(events) != 0 {
				t.Fatalf("rejected queued batch touched primary or sidecar payloads: %+v", events)
			}
			if kind == "canceled" && trace.begins.Load() != 1 {
				t.Fatalf("canceled queued batch restarted its snapshot: begins=%d", trace.begins.Load())
			}
			unblock()
		})
	}
}

func TestValidateSubtitlesReauthorizesAfterStorageBeforeFinalReturn(t *testing.T) {
	for _, kind := range []string{"disabled-user", "revoked-key", "retired-owned"} {
		t.Run(kind, func(t *testing.T) {
			fixture, tracks, expected := subtitlesBatchTestCatalog(t, 4, 4)
			subject := Subject{UserID: fixture.userID}
			if kind == "revoked-key" {
				subject = seedCatalogApplicationKey(t, fixture.ctx, fixture.pool, "subtitle-final-key", true)
			}
			trace := &subtitlesBatchTrace{}
			entered, resume := make(chan struct{}), make(chan struct{})
			var resumeOnce sync.Once
			unblock := func() { resumeOnce.Do(func() { close(resume) }) }
			trace.beforeSnapshot = func(ctx context.Context, position int64) error {
				if position != 3 {
					return nil
				}
				// All real sidecar reads precede this final BEGIN. Pause before
				// the new snapshot exists, then revoke authority from another pool.
				close(entered)
				select {
				case <-resume:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			traced := subtitlesBatchTraceFixture(t, fixture, trace)
			ctx, cancel := context.WithTimeout(traced, 10*time.Second)
			defer cancel()
			watch := subtitlesBatchWatchDirectory(t, filepath.Dir(fixture.path))
			baselineOwners := originalMediaReadOwners.Stats().RegisteredOwners
			done := make(chan struct{})
			var result error
			t.Cleanup(func() {
				cancel()
				unblock()
				mediaSourceAdmissionTestWait(t, done, "final subtitle authority barrier cleanup")
				if !imageStoreWaitWorkerCleanup(subtitleSourceWorkers) {
					t.Error("subtitle batch worker did not retire during cleanup")
				}
			})
			go func() {
				result = fixture.store.ValidateSubtitlesFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), expected)
				close(done)
			}()
			select {
			case <-entered:
			case <-done:
				t.Fatalf("batch returned before its final authority snapshot: %v", result)
			case <-ctx.Done():
				t.Fatalf("batch did not reach its final authority barrier: %v", ctx.Err())
			}
			observed := watch()
			if observed[filepath.Base(fixture.path)].opens != 1 || observed[filepath.Base(fixture.path)].reads != 0 {
				t.Fatalf("final authority barrier preceded the initial primary proof: %+v", observed)
			}
			for _, track := range tracks {
				if !track.Owned && (observed[track.Filename].opens != 1 || observed[track.Filename].reads == 0) {
					t.Fatalf("final authority barrier preceded actual sidecar reading: track=%s events=%+v", track.Filename, observed[track.Filename])
				}
			}
			want := ErrForbidden
			switch kind {
			case "disabled-user":
				if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE users SET is_disabled=true WHERE id=$1", fixture.userID); err != nil {
					t.Fatal(err)
				}
			case "revoked-key":
				if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", subject.ApplicationCredentialID); err != nil {
					t.Fatal(err)
				}
			case "retired-owned":
				if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE item_owned_subtitles SET active=false,retired_at=clock_timestamp() WHERE item_id=$1 AND stream_index=$2", fixture.item.ID, tracks[7].Index); err != nil {
					t.Fatal(err)
				}
				want = ErrNotFound
			}
			unblock()
			mediaSourceAdmissionTestWait(t, done, "final subtitle authority result")
			if !errors.Is(result, want) {
				t.Fatalf("batch ignored %s after storage observation: got=%v want=%v", kind, result, want)
			}
			if !imageStoreWaitWorkerCleanup(subtitleSourceWorkers) {
				t.Fatal("final authority rejection retained a subtitle worker")
			}
			primaryReadTestWaitOwners(t, ctx, baselineOwners)
			if events := watch(); events[filepath.Base(fixture.path)].opens != 0 {
				t.Fatalf("rejected authority reached another primary open: %+v", events)
			}
		})
	}
}

func TestValidateSubtitlesRetainsFinalPublicationFence(t *testing.T) {
	for _, kind := range []string{"publishing", "catalog-revision"} {
		t.Run(kind, func(t *testing.T) {
			fixture, tracks, expected := subtitlesBatchTestCatalog(t, 8, 0)
			operationID := publicationReadTestReservation(t, fixture, "none")
			trace := &subtitlesBatchTrace{}
			trace.afterPublication = func(ctx context.Context, position int64) error {
				// The final group snapshot captures the fourth publication proof.
				// Change it after capture, before the final descriptor is opened.
				if position != 4 {
					return nil
				}
				if kind == "publishing" {
					_, err := fixture.pool.Exec(ctx, `UPDATE media_operations SET state='applying',publication_phase='prepared' WHERE id=$1`, operationID)
					return err
				}
				_, err := fixture.pool.Exec(ctx, `UPDATE items SET media=jsonb_set(media,'{DurationTicks}',to_jsonb((media->>'DurationTicks')::bigint+1)) WHERE id=$1`, fixture.item.ID)
				return err
			}
			ctx := subtitlesBatchTraceFixture(t, fixture, trace)
			watch := subtitlesBatchWatchDirectory(t, filepath.Dir(fixture.path))
			err := fixture.store.ValidateSubtitlesFor(ctx, Subject{UserID: fixture.userID}, fixture.item.ID, media.SourceID(fixture.item.ID), expected)
			want := ErrBusy
			if kind == "catalog-revision" {
				want = ErrSourceChanged
			}
			if !errors.Is(err, want) {
				t.Fatalf("batch crossed the final %s publication fence: %v", kind, err)
			}
			trace.mu.Lock()
			hookErr := trace.hookErr
			trace.mu.Unlock()
			if hookErr != nil {
				t.Fatalf("publication mutation failed: %v", hookErr)
			}
			events := watch()
			if trace.publications.Load() != 5 || events[filepath.Base(fixture.path)].opens != 2 {
				t.Fatalf("batch failed before its final primary fence: publication_checks=%d events=%+v", trace.publications.Load(), events)
			}
			for _, track := range tracks {
				if events[track.Filename].opens != 1 || events[track.Filename].reads == 0 {
					t.Fatalf("final publication rejection skipped the sidecar observation: track=%s events=%+v", track.Filename, events[track.Filename])
				}
			}
		})
	}
}

func TestReadSubtitleMissingTrackPrecedesOutdatedPrimarySnapshot(t *testing.T) {
	fixture, track, _ := subtitleTestCatalog(t)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=jsonb_set(media,'{ProbeVersion}','0'::jsonb) WHERE id=$1`, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	content, err := fixture.store.ReadSubtitleFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index+1)
	if !errors.Is(err, ErrNotFound) || len(content.Data) != 0 {
		t.Fatalf("outdated primary snapshot changed the missing-track response: content=%+v error=%v", content, err)
	}
}

type subtitlesBatchProfile struct {
	operations                  int
	elapsed, p95, p99           time.Duration
	allocations, allocatedBytes uint64
	peakHeap, peakHeapGrowth    uint64
}

func subtitlesBatchMeasureProfile(t *testing.T, fixture mediaSourceFixture, ctx context.Context, expected []SubtitleExpectation, subjects []Subject, batch bool, rounds int) subtitlesBatchProfile {
	t.Helper()
	operation := func(subject Subject) error {
		if batch {
			return fixture.store.ValidateSubtitlesFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), expected)
		}
		for _, track := range expected {
			content, err := fixture.store.ReadSubtitleFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index)
			if err != nil {
				return err
			}
			if content.Info.Tag != track.Tag || content.Info.Codec != track.Codec || len(content.Data) == 0 {
				return errors.New("individual validation returned a different subtitle")
			}
		}
		return nil
	}
	for _, subject := range subjects {
		for range 3 {
			if err := operation(subject); err != nil {
				t.Fatal(err)
			}
		}
	}
	operations := len(subjects) * rounds
	durations := make([]time.Duration, operations)
	errorsByWorker := make([]error, len(subjects))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	peak.Store(before.HeapAlloc)
	sample := func() {
		var current runtime.MemStats
		runtime.ReadMemStats(&current)
		for previous := peak.Load(); current.HeapAlloc > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current.HeapAlloc) {
				break
			}
		}
	}
	stop, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sample()
			case <-stop:
				sample()
				return
			}
		}
	}()
	var workers sync.WaitGroup
	start := time.Now()
	for worker, subject := range subjects {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for round := range rounds {
				started := time.Now()
				if err := operation(subject); err != nil {
					errorsByWorker[worker] = err
					return
				}
				durations[worker*rounds+round] = time.Since(started)
			}
		}()
	}
	workers.Wait()
	elapsed := time.Since(start)
	close(stop)
	<-sampled
	runtime.ReadMemStats(&after)
	for _, err := range errorsByWorker {
		if err != nil {
			t.Fatalf("subtitle performance operation failed: %v", err)
		}
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	return subtitlesBatchProfile{operations: operations, elapsed: elapsed,
		p95: durations[(operations*95+99)/100-1], p99: durations[(operations*99+99)/100-1],
		allocations: after.Mallocs - before.Mallocs, allocatedBytes: after.TotalAlloc - before.TotalAlloc,
		peakHeap: peak.Load(), peakHeapGrowth: peak.Load() - before.HeapAlloc}
}

// This opt-in profile reports warm local-fixture costs, not NAS latency. Heap
// peaks are sampled observations and are not an exact retained-memory bound.
// Multi-track owned groups fetch each payload in three authority snapshots;
// individual reads fetch it twice. Include all-owned groups to measure this
// extra database work alongside the reduction in shared primary-source work.
func TestValidateSubtitlesPerformanceProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_SUBTITLE_BATCH_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_SUBTITLE_BATCH_PERFORMANCE=1 enables the subtitle batch performance profile")
	}
	for _, owned := range []int{0, 4, 8} {
		fixture, _, allExpected := subtitlesBatchTestCatalog(t, 8-owned, owned, 5*time.Minute)
		trace := &subtitlesBatchTrace{}
		ctx := subtitlesBatchTraceFixture(t, fixture, trace)
		subjects := []Subject{{UserID: fixture.userID}}
		for worker := 1; worker < 4; worker++ {
			userID := fmt.Sprintf("subtitle-batch-profile-%d", worker)
			libraryIntegrationUser(t, fixture.ctx, fixture.pool, userID, false, false, []string{fixture.library.ID})
			subjects = append(subjects, Subject{UserID: userID})
		}
		for _, count := range []int{1, 8} {
			for _, parallelism := range []int{1, 4} {
				for _, batch := range []bool{false, true} {
					name := fmt.Sprintf("owned-%d/tracks-%d/users-%d/batch-%t", owned, count, parallelism, batch)
					t.Run(name, func(t *testing.T) {
						trace.reset()
						profile := subtitlesBatchMeasureProfile(t, fixture, ctx, allExpected[:count], subjects[:parallelism], batch, 30)
						t.Logf("subtitle_batch_performance batch=%t fixture_owned=%d tracks=%d users=%d operations=%d elapsed=%s ns_per_operation=%d p95=%s p99=%s allocations_per_operation=%.1f bytes_per_operation=%.1f observed_peak_heap_bytes=%d observed_peak_heap_growth_bytes=%d sql_including_warmup=%d snapshots_including_warmup=%d owned_payload_queries_including_warmup=%d",
							batch, owned, count, parallelism, profile.operations, profile.elapsed, profile.elapsed.Nanoseconds()/int64(profile.operations), profile.p95, profile.p99,
							float64(profile.allocations)/float64(profile.operations), float64(profile.allocatedBytes)/float64(profile.operations), profile.peakHeap, profile.peakHeapGrowth,
							trace.queries.Load(), trace.begins.Load(), trace.owned.Load())
					})
				}
			}
		}
	}
}
