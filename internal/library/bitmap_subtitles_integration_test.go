//go:build linux

package library

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

// These fixtures exercise indexed structure and source identity, not raster
// rendering. Media-layer tests independently prove actual display intervals.
func bitmapCatalogTestSUP() []byte {
	var encoded []byte
	for number := 0; number < 2; number++ {
		header := make([]byte, 13)
		copy(header, "PG")
		binary.BigEndian.PutUint32(header[2:6], uint32(number*90000))
		header[10], header[12] = 0x16, 11
		encoded = append(encoded, header...)
		encoded = append(encoded, make([]byte, 11)...)
		header[10], header[12] = 0x80, 0
		encoded = append(encoded, header...)
	}
	return encoded
}

func bitmapCatalogTestPair() ([]byte, []byte) {
	var sub []byte
	var index strings.Builder
	index.WriteString("# VobSub index file, v7 (do not modify this line!)\nsize: 320x180\n")
	for ordinal, language := range []string{"en", "zh"} {
		fmt.Fprintf(&index, "id: %s, index: %d\ntimestamp: 00:00:01:000, filepos: %09x\n", language, ordinal+3, len(sub))
		// A complete stop-only SPU control sequence is enough for the scanner.
		spu := []byte{0, 10, 0, 4, 0, 0, 0, 4, 2, 0xff}
		payload := append([]byte{0x80, 0, 0, byte(0x23 + ordinal)}, spu...)
		sub = append(sub, 0, 0, 1, 0xba, 0x44, 0, 4, 0, 4, 1, 0, 0, 3, 0xf8)
		sub = append(sub, 0, 0, 1, 0xbd, byte(len(payload)>>8), byte(len(payload)))
		sub = append(sub, payload...)
	}
	return []byte(index.String()), sub
}

func bitmapCatalogTracks(t *testing.T, fixture mediaSourceFixture) []BitmapSubtitle {
	t.Helper()
	item, err := fixture.store.GetItem(fixture.ctx, fixture.userID, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return item.BitmapSubtitles
}

func writeBitmapCatalogFiles(t *testing.T, fixture mediaSourceFixture) (string, string, string) {
	t.Helper()
	directory := filepath.Dir(fixture.path)
	sup, idx, sub := filepath.Join(directory, "Feature.fr.default.sup"), filepath.Join(directory, "Feature.idx"), filepath.Join(directory, "Feature.sub")
	indexBytes, subBytes := bitmapCatalogTestPair()
	for path, bytes := range map[string][]byte{sup: bitmapCatalogTestSUP(), idx: indexBytes, sub: subBytes} {
		if err := os.WriteFile(path, bytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return sup, idx, sub
}

func TestBitmapSubtitleCatalogScansCachedMediaWithSharedStableIndexes(t *testing.T) {
	prober := &libraryFixtureProber{}
	fixture := mediaSourceTestCatalog(t, prober)
	_, idx, _ := writeBitmapCatalogFiles(t, fixture)
	textPath := filepath.Join(filepath.Dir(fixture.path), "Feature.en.srt")
	if err := os.WriteFile(textPath, []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	probeCalls := len(prober.calls())
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	first := bitmapCatalogTracks(t, fixture)
	text := subtitleTestTracks(t, fixture)
	if len(first) != 3 || len(text) != 1 || first[0].Index <= text[0].Index || first[1].Index != first[0].Index+1 || first[2].Index != first[1].Index+1 {
		t.Fatalf("bitmap and text indexes do not share one stable namespace: bitmap=%+v text=%+v", first, text)
	}
	if first[0].Format != "sup" || first[0].Language != "fr" || !first[0].IsDefault || first[1].Language != "en" || first[2].Language != "zh" ||
		first[1].SourceStreamIndex != 0 || first[2].SourceStreamIndex != 1 || first[1].Tag == first[2].Tag {
		t.Fatalf("bitmap format, language, or component identity was not preserved: %+v", first)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if !reflect.DeepEqual(first, bitmapCatalogTracks(t, fixture)) || len(prober.calls()) != probeCalls {
		t.Fatal("unchanged bitmap scan rewrote identities or reprobed cached media")
	}
	for _, track := range first {
		if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrNotFound) {
			t.Fatalf("bitmap track entered the text delivery path: %v", err)
		}
	}
	index, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	index = []byte(strings.ReplaceAll(string(index), "00:00:01:000", "00:00:02:000"))
	if err := os.WriteFile(idx, index, 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	changed := bitmapCatalogTracks(t, fixture)
	if len(changed) != 3 || changed[1].Index != first[1].Index || changed[2].Index != first[2].Index ||
		changed[1].Tag == first[1].Tag || changed[2].Tag == first[2].Tag {
		t.Fatalf("IDX replacement did not refresh both stable language identities: %+v", changed)
	}
}

func TestBitmapSubtitleCatalogPreservesIncompleteAndCorruptSourcesUntilCompleteRemoval(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	sup, idx, sub := writeBitmapCatalogFiles(t, fixture)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	first := bitmapCatalogTracks(t, fixture)
	if len(first) != 3 {
		t.Fatalf("missing bitmap fixture inventory: %+v", first)
	}
	has := true
	queried, err := fixture.store.QueryItems(fixture.ctx, Query{UserID: fixture.userID, ParentID: fixture.library.ID,
		Recursive: true, IncludeItemTypes: []string{"Movie"}, HasSubtitles: &has})
	if err != nil || len(queried.Items) != 1 || queried.Items[0].ID != fixture.item.ID {
		t.Fatalf("subtitle presence ignored bitmap-only tracks: count=%d error=%v", len(queried.Items), err)
	}
	if err := os.Remove(sub); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sup, []byte("corrupt SUP"), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if !reflect.DeepEqual(first, bitmapCatalogTracks(t, fixture)) {
		t.Fatal("an incomplete pair or corrupt SUP retired an existing snapshot")
	}
	if err := os.Remove(idx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sup); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if tracks := bitmapCatalogTracks(t, fixture); len(tracks) != 0 {
		t.Fatalf("a complete stable removal retained public tracks: %+v", tracks)
	}
	writeBitmapCatalogFiles(t, fixture)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	restored := bitmapCatalogTracks(t, fixture)
	if len(restored) != 3 || restored[0].Index <= first[2].Index {
		t.Fatalf("restored files revived a retired public index: %+v", restored)
	}
	var total, active int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*),count(*) FILTER(WHERE active) FROM item_bitmap_subtitles WHERE item_id=$1`, fixture.item.ID).Scan(&total, &active); err != nil || total != 6 || active != 3 {
		t.Fatalf("retired component identities were not reserved: total=%d active=%d error=%v", total, active, err)
	}
}

func TestBitmapSubtitleCatalogReallocatesAfterEmbeddedCollisionAndReservesTextCapacity(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	writeBitmapCatalogFiles(t, fixture)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	first := bitmapCatalogTracks(t, fixture)
	if len(first) != 3 {
		t.Fatalf("missing bitmap fixture inventory: %+v", first)
	}
	updated := *fixture.item.Media
	updated.Streams = append(append([]media.Stream(nil), updated.Streams...), media.Stream{Index: first[0].Index, CodecType: "subtitle", Codec: "subrip"})
	encoded, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=$2::jsonb WHERE id=$1`, fixture.item.ID, encoded); err != nil {
		t.Fatal(err)
	}
	if tracks := bitmapCatalogTracks(t, fixture); len(tracks) != 2 {
		t.Fatalf("a bitmap index shadowed a newly embedded stream: %+v", tracks)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	reallocated := bitmapCatalogTracks(t, fixture)
	if len(reallocated) != 3 || reallocated[2].Index <= first[2].Index || reallocated[2].Format != "sup" {
		t.Fatalf("colliding external track was not assigned a fresh index: %+v", reallocated)
	}
	// Stay below the text scanner's own candidate overflow threshold so this
	// exercises the shared active capacity after three bitmap tracks exist.
	for number := 0; number < 29; number++ {
		path := filepath.Join(filepath.Dir(fixture.path), fmt.Sprintf("Feature.en-%02d.srt", number))
		if err := os.WriteFile(path, []byte(subtitleTestSRT), 0600); err != nil {
			t.Fatal(err)
		}
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if len(subtitleTestTracks(t, fixture))+len(bitmapCatalogTracks(t, fixture)) != maxActiveSubtitles {
		t.Fatal("text and bitmap tracks did not share the active capacity")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(fixture.path), "Feature.ja.srt"), []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if len(subtitleTestTracks(t, fixture))+len(bitmapCatalogTracks(t, fixture)) != maxActiveSubtitles {
		t.Fatal("a later text scan exceeded the shared active capacity")
	}
}

func TestBitmapSubtitleInspectionRejectsSymlinkAndWrongCompanion(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	sup, _, sub := writeBitmapCatalogFiles(t, fixture)
	root, err := os.OpenRoot(filepath.Dir(fixture.path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Remove(sup); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sub, sup); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectLocalBitmapSubtitle(fixture.ctx, root, bitmapSubtitleCandidate{filename: filepath.Base(sup), format: "sup"}); err == nil {
		t.Fatal("a symlink was admitted as a regular SUP file")
	}
	if _, err := inspectLocalBitmapSubtitle(fixture.ctx, root, bitmapSubtitleCandidate{filename: "Feature.idx", companion: "Feature.idx", format: "vobsub"}); err == nil {
		t.Fatal("the same file was admitted as both IDX and SUB")
	}
}

func TestBitmapSubtitleCatalogChangedDirectoryDoesNotRetireUsingCachedNames(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	_, idx, sub := writeBitmapCatalogFiles(t, fixture)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	first := bitmapCatalogTracks(t, fixture)
	state := imageScanTestState(t, fixture.ctx, fixture.pool, fixture.store, fixture.library, "Nested")
	relative := filepath.Join("Nested", "Feature.mkv")
	if err := state.scanSubtitles(fixture.item.ID, relative, fixture.item.Media); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(idx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sub); err != nil {
		t.Fatal(err)
	}
	if err := state.scanSubtitles(fixture.item.ID, relative, fixture.item.Media); err != nil {
		t.Fatal(err)
	}
	if state.warnings == 0 || !reflect.DeepEqual(first, bitmapCatalogTracks(t, fixture)) {
		t.Fatal("a changed directory authorized retirement using an earlier listing")
	}
}

func TestBitmapSubtitleInspectionRechecksBothHeldPairDescriptors(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	_, _, sub := writeBitmapCatalogFiles(t, fixture)
	root, err := os.OpenRoot(filepath.Dir(fixture.path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	entry, err := inspectLocalBitmapSubtitle(fixture.ctx, root, bitmapSubtitleCandidate{filename: "Feature.idx", companion: "Feature.sub", format: "vobsub"})
	if err != nil {
		t.Fatal(err)
	}
	defer entry.close(fixture.ctx)
	if err := os.Rename(sub, sub+".old"); err != nil {
		t.Fatal(err)
	}
	_, bytes := bitmapCatalogTestPair()
	if err := os.WriteFile(sub, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(verifyScannedBitmapSubtitle(root, entry), ErrSourceChanged) {
		t.Fatal("a replaced SUB pathname retained its earlier authorized descriptor")
	}
}
