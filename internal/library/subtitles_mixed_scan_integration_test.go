//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

type subtitleMixedScanTrace struct {
	upserts  atomic.Int64
	armed    atomic.Bool
	onUpsert func()
}

func (trace *subtitleMixedScanTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(strings.TrimSpace(data.SQL), "INSERT INTO item_subtitles") {
		trace.upserts.Add(1)
		if trace.armed.CompareAndSwap(true, false) {
			trace.onUpsert()
		}
	}
	return ctx
}

func (*subtitleMixedScanTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestSubtitleMixedScanWritesOnlyChangedRetainedSources(t *testing.T) {
	trace := &subtitleMixedScanTrace{}
	ctx, pool, store, state, _, root := scanCachedVisitFixture(t, trace)
	path := filepath.Join(root, "combined-visit", "Film.mp4")
	item := nfoCatalogItem(t, ctx, store, "unrestricted-viewer", state.library.ID, path)
	fixture := mediaSourceFixture{ctx: ctx, pool: pool, store: store, userID: "unrestricted-viewer",
		path: path, library: state.library, item: item}
	notifications := catalogChangesTestListener(t, store)
	directory := filepath.Dir(path)
	for index := range maxActiveSubtitles {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("Film.en-%02d.srt", index)), []byte(subtitleTestSRT), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(wantUpserts int64) {
		t.Helper()
		info, err := state.opened.Stat(".")
		if err != nil {
			t.Fatal(err)
		}
		state.directoryIdentities["."] = info
		state.subtitleDirectories = nil
		trace.upserts.Store(0)
		if err := state.scanSubtitles(item.ID, "Film.mp4", item.Media); err != nil {
			t.Fatal(err)
		}
		if state.warnings != 0 || trace.upserts.Load() != wantUpserts {
			t.Fatalf("subtitle scan warnings=%d UPSERTs=%d, want no warnings and %d UPSERTs",
				state.warnings, trace.upserts.Load(), wantUpserts)
		}
	}
	run(maxActiveSubtitles)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	before := subtitleTestTracks(t, fixture)
	if len(before) != maxActiveSubtitles {
		t.Fatalf("subtitle fixture did not fill the active capacity: %+v", before)
	}
	stableVersions := make(map[int]string, len(before)-1)
	for _, track := range before[1:] {
		stableVersions[track.Index] = subtitleScanRowVersion(t, fixture, track.Index)
	}
	assertStable := func(excluded ...int) {
		t.Helper()
		for index, version := range stableVersions {
			skip := false
			for _, omit := range excluded {
				skip = skip || index == omit
			}
			if !skip && subtitleScanRowVersion(t, fixture, index) != version {
				t.Fatalf("mixed scan rewrote stable subtitle index %d", index)
			}
		}
	}
	run(0)
	assertNoCatalogTestNotification(t, notifications)
	changedPath := filepath.Join(directory, before[0].Filename)
	stat, err := os.Stat(changedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changedPath, []byte(strings.Replace(subtitleTestSRT, "Hello", "Other", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(changedPath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	run(1)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	after := subtitleTestTracks(t, fixture)
	if len(after) != len(before) || after[0].Index != before[0].Index || after[0].Tag == before[0].Tag ||
		after[0].Size != before[0].Size || !reflect.DeepEqual(after[1:], before[1:]) {
		t.Fatalf("mixed content update lost source or retained track facts: before=%+v after=%+v", before, after)
	}
	assertStable()

	// Private facts still require one write, but must not publish a new content
	// notification when all public subtitle fields and bytes remain accepted.
	modified := stat.ModTime().Add(time.Second)
	if err := os.Chtimes(changedPath, modified, modified); err != nil {
		t.Fatal(err)
	}
	run(1)
	assertNoCatalogTestNotification(t, notifications)
	if current := subtitleTestTracks(t, fixture); !current[0].ModifiedAt.Equal(modified.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("private source stamp was not refreshed: %+v", current[0])
	}
	assertStable()

	// A full active set can retire one absent identity and allocate a new one
	// without sending conflict checks for the other 31 accepted tracks.
	removed := before[len(before)-1]
	if err := os.Remove(filepath.Join(directory, removed.Filename)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "Film.en-32.srt"), []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	run(1)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	after = subtitleTestTracks(t, fixture)
	if len(after) != maxActiveSubtitles || after[len(after)-1].Index <= removed.Index || after[len(after)-1].Filename != "Film.en-32.srt" {
		t.Fatalf("mixed scan reused a retired index or lost capacity: %+v", after)
	}
	var active bool
	if err := pool.QueryRow(ctx, `SELECT active FROM item_subtitles WHERE item_id=$1 AND stream_index=$2`, item.ID, removed.Index).Scan(&active); err != nil || active {
		t.Fatalf("missing track did not retain an inactive identity: active=%t error=%v", active, err)
	}
	assertStable(removed.Index)

	// An embedded collision is not a retained match even when every source fact
	// still matches the old row. Its external identity must be reallocated.
	colliding := after[0]
	updated := *item.Media
	updated.Streams = append(append([]media.Stream(nil), updated.Streams...), media.Stream{
		Index: colliding.Index, CodecType: "subtitle", Codec: "subrip",
	})
	raw, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET media=$2::jsonb WHERE id=$1", item.ID, raw); err != nil {
		t.Fatal(err)
	}
	item.Media = &updated
	run(1)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	after = subtitleTestTracks(t, fixture)
	if len(after) != maxActiveSubtitles || after[len(after)-1].Filename != colliding.Filename || after[len(after)-1].Index <= removed.Index+1 {
		t.Fatalf("colliding identity was treated as a retained match: %+v", after)
	}
	assertStable(removed.Index)
}

func TestSubtitleMixedScanRechecksSkippedSourcesBeforeCommit(t *testing.T) {
	trace := &subtitleMixedScanTrace{}
	ctx, pool, store, state, _, root := scanCachedVisitFixture(t, trace)
	path := filepath.Join(root, "combined-visit", "Film.mp4")
	item := nfoCatalogItem(t, ctx, store, "unrestricted-viewer", state.library.ID, path)
	fixture := mediaSourceFixture{ctx: ctx, pool: pool, store: store, userID: "unrestricted-viewer",
		path: path, library: state.library, item: item}
	changedPath := libraryIntegrationFile(t, root, "combined-visit/Film.en.srt", subtitleTestSRT)
	stablePath := libraryIntegrationFile(t, root, "combined-visit/Film.fr.srt", subtitleTestSRT)
	info, err := state.opened.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	state.directoryIdentities["."] = info
	if err := state.scanSubtitlesAttempt(item.ID, "Film.mp4", item.Media); err != nil {
		t.Fatal(err)
	}
	before := subtitleTestTracks(t, fixture)
	if len(before) != 2 {
		t.Fatalf("expected two accepted subtitle sources, got %+v", before)
	}
	versions := []string{subtitleScanRowVersion(t, fixture, before[0].Index), subtitleScanRowVersion(t, fixture, before[1].Index)}
	notifications := catalogChangesTestListener(t, store)
	if err := os.WriteFile(changedPath, []byte(strings.Replace(subtitleTestSRT, "Hello", "Other", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	stableInfo, err := os.Stat(stablePath)
	if err != nil {
		t.Fatal(err)
	}
	var mutateErr error
	trace.onUpsert = func() {
		// In-place mutation leaves the directory identity and listing unchanged.
		// The skipped entry still needs its final held-file and ctime proof.
		mutateErr = os.WriteFile(stablePath, []byte(strings.Replace(subtitleTestSRT, "Hello", "Later", 1)), 0600)
		if mutateErr == nil {
			mutateErr = os.Chtimes(stablePath, stableInfo.ModTime(), stableInfo.ModTime())
		}
	}
	trace.upserts.Store(0)
	trace.armed.Store(true)
	err = state.scanSubtitlesAttempt(item.ID, "Film.mp4", item.Media)
	if mutateErr != nil {
		t.Fatal(mutateErr)
	}
	if !errors.Is(err, ErrSourceChanged) || trace.armed.Load() || trace.upserts.Load() != 1 {
		t.Fatalf("mixed scan failed to reject a changed skipped source: error=%v armed=%t UPSERTs=%d", err, trace.armed.Load(), trace.upserts.Load())
	}
	if current := subtitleTestTracks(t, fixture); !reflect.DeepEqual(current, before) {
		t.Fatalf("failed final proof published a partial mixed scan: before=%+v after=%+v", before, current)
	}
	for index, track := range before {
		if subtitleScanRowVersion(t, fixture, track.Index) != versions[index] {
			t.Fatalf("failed final proof rewrote subtitle index %d", track.Index)
		}
	}
	assertNoCatalogTestNotification(t, notifications)
}
