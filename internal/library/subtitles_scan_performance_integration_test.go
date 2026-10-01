//go:build linux

package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func subtitleScanRowVersion(t *testing.T, fixture mediaSourceFixture, index int) string {
	t.Helper()
	var version string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT xmin::text FROM item_subtitles
		WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, index).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func TestSubtitleScanUnchangedSourcesAvoidRowWrites(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	version := subtitleScanRowVersion(t, fixture, track.Index)
	scanSubtitleCatalogFixture(t, fixture)
	if current := subtitleScanRowVersion(t, fixture, track.Index); current != version {
		t.Fatalf("unchanged scan rewrote the accepted subtitle: before=%s after=%s", version, current)
	}
	// Adding another sidecar must not rewrite the already accepted source while
	// the same transaction allocates and publishes the new stream identity.
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "Feature.fr.srt"), []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	tracks := subtitleTestTracks(t, fixture)
	if len(tracks) != 2 || tracks[0].Index != track.Index {
		t.Fatalf("adding a sidecar changed the existing stream identity: %+v", tracks)
	}
	if current := subtitleScanRowVersion(t, fixture, track.Index); current != version {
		t.Fatalf("mixed scan rewrote an unchanged subtitle: before=%s after=%s", version, current)
	}
}

func TestSubtitleScanNoWriteComparisonRetainsContentAndPrivateFacts(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	version := subtitleScanRowVersion(t, fixture, track.Index)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(subtitleTestSRT, "Hello", "Other", 1)
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	tracks := subtitleTestTracks(t, fixture)
	if len(tracks) != 1 || tracks[0].Index != track.Index || tracks[0].Tag == track.Tag || tracks[0].Size != track.Size {
		t.Fatalf("same-size content change with restored mtime was skipped: before=%+v after=%+v", track, tracks)
	}
	current := subtitleScanRowVersion(t, fixture, track.Index)
	if current == version {
		t.Fatal("changed subtitle content did not update its accepted row")
	}
	version = current
	modified := stat.ModTime().Add(time.Second)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	tracks = subtitleTestTracks(t, fixture)
	if len(tracks) != 1 || tracks[0].Index != track.Index || !tracks[0].ModifiedAt.Equal(modified.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("private source stamp refresh was skipped: %+v", tracks)
	}
	if current := subtitleScanRowVersion(t, fixture, track.Index); current == version {
		t.Fatal("changed private source facts did not update their accepted row")
	}
	// Catalog drift still requires publication from the validated sidecar even
	// when its content and filesystem identity already match the stored source.
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_subtitles SET title='Stale title'
		WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, track.Index); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	tracks = subtitleTestTracks(t, fixture)
	if len(tracks) != 1 || tracks[0].Title != track.Title {
		t.Fatalf("unchanged source retained stale subtitle metadata: %+v", tracks)
	}
}
