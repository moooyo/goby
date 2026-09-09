//go:build linux

package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestScanSubtitlesPreservesIndexesAcrossCachedScansAndRetiresDeletedNames(t *testing.T) {
	prober := &libraryFixtureProber{}
	fixture := mediaSourceTestCatalog(t, prober)
	directory := filepath.Dir(fixture.path)
	paths := map[string]string{
		"Feature.srt":                          subtitleTestSRT,
		"Feature.zh-CN.default.forced.sdh.VTT": subtitleTestVTT,
	}
	for name, contents := range paths {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	probeCalls := len(prober.calls())
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	first := subtitleTestTracks(t, fixture)
	if len(first) != 2 || first[0].Index <= highestEmbeddedStreamIndex(fixture.item.Media) || first[1].Index <= first[0].Index {
		t.Fatalf("external subtitle indexes did not follow embedded streams: %+v", first)
	}
	if !first[1].IsDefault || !first[1].IsForced || !first[1].IsHearingImpaired || first[1].Language != "zh-cn" {
		t.Fatalf("subtitle flags were not persisted: %+v", first[1])
	}
	if err := os.WriteFile(filepath.Join(directory, "Feature.en.srt"), []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	added := subtitleTestTracks(t, fixture)
	if len(added) != 3 || !reflect.DeepEqual(added[:2], first) || added[2].Filename != "Feature.en.srt" || added[2].Index <= first[1].Index {
		t.Fatalf("an alphabetically earlier addition renumbered existing subtitles: before=%+v after=%+v", first, added)
	}
	if len(prober.calls()) != probeCalls {
		t.Fatalf("sidecar changes repeated a cached primary media probe: before=%d after=%d", probeCalls, len(prober.calls()))
	}
	removed := added[2]
	if err := os.Remove(filepath.Join(directory, removed.Filename)); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), removed.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a retired subtitle index remained readable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, removed.Filename), []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	reappeared := subtitleTestTracks(t, fixture)
	if len(reappeared) != 3 || reappeared[2].Index <= removed.Index || reappeared[2].Filename != removed.Filename {
		t.Fatalf("a reappearing filename reused a stale URL: old=%+v current=%+v", removed, reappeared)
	}
	var total, active int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*), count(*) FILTER (WHERE active)
		FROM item_subtitles WHERE item_id = $1`, fixture.item.ID).Scan(&total, &active); err != nil {
		t.Fatal(err)
	}
	if total != 4 || active != 3 {
		t.Fatalf("retired subtitle identity was not reserved: total=%d active=%d", total, active)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), removed.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reappearing filename revived an old index: %v", err)
	}
}

func TestScanSubtitlesInvalidExistingCandidateRetainsSnapshotAndStableListingPrunes(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	if err := os.WriteFile(path, []byte("invalid replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	job := libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if job.Error == "" || !reflect.DeepEqual(subtitleTestTracks(t, fixture), []Subtitle{track}) {
		t.Fatalf("invalid existing subtitle did not retain its previous snapshot: job=%+v tracks=%+v", job, subtitleTestTracks(t, fixture))
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("retained metadata made a changed subtitle readable: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if tracks := subtitleTestTracks(t, fixture); len(tracks) != 0 {
		t.Fatalf("complete stable listing did not retire absent subtitle: %+v", tracks)
	}
}

func TestScanSubtitlesChangedDirectoryDoesNotPruneUsingCachedNames(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	state := imageScanTestState(t, fixture.ctx, fixture.pool, fixture.store, fixture.library, "Nested")
	relative := filepath.Join("Nested", "Feature.mkv")
	if err := state.scanSubtitles(fixture.item.ID, relative, fixture.item.Media); err != nil {
		t.Fatal(err)
	}
	index := state.subtitleDirectories["Nested"]
	if index == nil {
		t.Fatal("subtitle scan did not retain its directory index")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := state.scanSubtitles(fixture.item.ID, relative, fixture.item.Media); err != nil {
		t.Fatal(err)
	}
	if state.warnings == 0 || state.subtitleDirectories["Nested"] != index || !reflect.DeepEqual(subtitleTestTracks(t, fixture), []Subtitle{track}) {
		t.Fatal("changed directory names authorized pruning or caused an unbounded reindex")
	}
}

func TestScanSubtitlesReallocatesIndexesAfterEmbeddedStreamCollision(t *testing.T) {
	fixture, track, _ := subtitleTestCatalog(t)
	updated := *fixture.item.Media
	updated.Streams = append(append([]media.Stream(nil), updated.Streams...), media.Stream{Index: track.Index, CodecType: "subtitle", Codec: "subrip"})
	encoded, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media = $2::jsonb WHERE id = $1`, fixture.item.ID, encoded); err != nil {
		t.Fatal(err)
	}
	if tracks := subtitleTestTracks(t, fixture); len(tracks) != 0 {
		t.Fatalf("an external descriptor overlapped the new embedded index: %+v", tracks)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old external URL selected a colliding embedded stream: %v", err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	tracks := subtitleTestTracks(t, fixture)
	if len(tracks) != 1 || tracks[0].Index <= track.Index || tracks[0].Filename != track.Filename {
		t.Fatalf("scan did not reallocate a safe external index: old=%+v current=%+v", track, tracks)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), tracks[0].Index); err != nil {
		t.Fatalf("reallocated external track was not readable: %v", err)
	}
}

func TestScanSubtitlesBoundsLifetimeIdentitiesAndCascadesDeletion(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_subtitles
		(item_id, root_id, stream_index, active, relative_path, file_identity, source_hash, file_size,
		 modified_at, change_time_ns, codec, language, title, is_default, is_forced, is_hearing_impaired, mime_type)
		SELECT item_id, root_id, serial, false, relative_path, file_identity, source_hash, file_size,
		 modified_at, change_time_ns, codec, language, title, is_default, is_forced, is_hearing_impaired, mime_type
		FROM item_subtitles CROSS JOIN generate_series($2::integer + 1, $2::integer + 4095) AS serial
		WHERE item_id = $1 AND stream_index = $2`, fixture.item.ID, track.Index); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "Feature.fr.srt"), []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	job := libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if job.Error == "" || !reflect.DeepEqual(subtitleTestTracks(t, fixture), []Subtitle{track}) {
		t.Fatalf("lifetime identity limit did not preserve existing tracks and report overflow: %+v", job)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, "DELETE FROM items WHERE id = $1", fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := fixture.pool.QueryRow(fixture.ctx, "SELECT count(*) FROM item_subtitles WHERE item_id = $1", fixture.item.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("item deletion retained subtitle identity rows: %d", count)
	}
}

func TestAttachSubtitlesKeepsTheAuthorizedItemSnapshot(t *testing.T) {
	fixture, track, _ := subtitleTestCatalog(t)
	tx, _, err := fixture.store.beginUserRead(fixture.ctx, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(fixture.ctx)
	item, err := scanItem(tx.QueryRow(fixture.ctx, "SELECT "+itemColumns+" FROM items i WHERE i.id = $1", fixture.item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE item_subtitles SET title = 'New title' WHERE item_id = $1", fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	items := []Item{item, item}
	if err := attachSubtitles(fixture.ctx, tx, items); err != nil {
		t.Fatal(err)
	}
	for _, projected := range items {
		if !reflect.DeepEqual(projected.Subtitles, []Subtitle{track}) {
			t.Fatalf("batched subtitles escaped the authorized item snapshot: %+v", projected.Subtitles)
		}
	}
	if err := tx.Commit(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	current := subtitleTestTracks(t, fixture)
	if len(current) != 1 || current[0].Title != "New title" {
		t.Fatalf("a fresh projection did not observe committed subtitle metadata: %+v", current)
	}
}
