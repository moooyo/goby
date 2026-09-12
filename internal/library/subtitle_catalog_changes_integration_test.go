//go:build linux

package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestSubtitleCatalogChangesCachedOwnerAddsUpdatesAndRemovesTracks(t *testing.T) {
	prober := &libraryFixtureProber{}
	fixture := mediaSourceTestCatalog(t, prober)
	actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "subtitle-notification-editor")
	detail := metadataEditTestDetail(t, fixture.ctx, fixture.store, actor, fixture.item.ID)
	metadataEditTestUpdate(t, fixture.ctx, fixture.store, actor, detail,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Manual subtitle owner"`)}, []string{"Overview"})
	userDataSeed(t, fixture.ctx, fixture.pool, fixture.userID, UserData{ItemID: fixture.item.ID, IsFavorite: true, PlayCount: 3})
	beforeMetadata := metadataEditTestSnapshot(t, fixture.ctx, fixture.pool, fixture.item.ID)
	beforeUserData := forceProbeUserDataSnapshot(t, fixture.ctx, fixture.pool, fixture.item.ID)
	notifications := catalogChangesTestListener(t, fixture.store)
	path := filepath.Join(filepath.Dir(fixture.path), "Feature.en.default.srt")
	if err := os.WriteFile(path, []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	tracks := subtitleTestTracks(t, fixture)
	if len(tracks) != 1 || tracks[0].Language != "en" || !tracks[0].IsDefault {
		t.Fatalf("notified subtitle addition has no matching public track: %+v", tracks)
	}
	first := tracks[0]
	scanSubtitleCatalogFixture(t, fixture)
	assertNoCatalogTestNotification(t, notifications)
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
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	tracks = subtitleTestTracks(t, fixture)
	if len(tracks) != 1 || tracks[0].Index != first.Index || tracks[0].Tag == first.Tag || tracks[0].Size != first.Size {
		t.Fatalf("same-size content change lost its track identity or hash difference: %+v", tracks)
	}
	content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), first.Index)
	if err != nil || string(content.Data) != changed {
		t.Fatalf("notified subtitle content was not committed: %q, %v", content.Data, err)
	}
	// Refreshing only private stat facts does not change stream metadata or
	// validated content. It must not create a second content notification.
	if err := os.Chtimes(path, stat.ModTime().Add(time.Second), stat.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	assertNoCatalogTestNotification(t, notifications)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	if tracks := subtitleTestTracks(t, fixture); len(tracks) != 0 {
		t.Fatalf("notified removal left a visible track: %+v", tracks)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), first.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired subtitle URL still resolved: %v", err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	assertNoCatalogTestNotification(t, notifications)
	if len(prober.calls()) != 1 || metadataEditTestSnapshot(t, fixture.ctx, fixture.pool, fixture.item.ID) != beforeMetadata ||
		forceProbeUserDataSnapshot(t, fixture.ctx, fixture.pool, fixture.item.ID) != beforeUserData {
		t.Fatal("sidecar notifications reprobed media or changed owner metadata/user data")
	}
}

func TestSubtitleCatalogChangesRejectedSourcesRetainProjectionWithoutNotification(t *testing.T) {
	for _, kind := range []string{"invalid", "fifo", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			fixture, track, path := subtitleTestCatalog(t)
			notifications := catalogChangesTestListener(t, fixture.store)
			if kind == "invalid" {
				if err := os.WriteFile(path, []byte("not a subtitle document"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if kind == "fifo" {
					if err := syscall.Mkfifo(path, 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink("missing-subtitle.srt", path); err != nil {
					t.Fatal(err)
				}
			}
			job := libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
			if job.Error == "" || !reflect.DeepEqual(subtitleTestTracks(t, fixture), []Subtitle{track}) {
				t.Fatalf("a rejected subtitle changed its retained projection: %+v", job)
			}
			assertNoCatalogTestNotification(t, notifications)
			if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("retained metadata authorized an invalid source: %v", err)
			}
		})
	}
}

func TestSubtitleCatalogChangesUnstableDirectoryRetainsProjectionWithoutNotification(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	notifications := catalogChangesTestListener(t, fixture.store)
	state := imageScanTestState(t, fixture.ctx, fixture.pool, fixture.store, fixture.library, "Nested")
	relative := filepath.Join("Nested", "Feature.mkv")
	if err := state.scanSubtitles(fixture.item.ID, relative, fixture.item.Media); err != nil {
		t.Fatal(err)
	}
	assertNoCatalogTestNotification(t, notifications)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := state.scanSubtitles(fixture.item.ID, relative, fixture.item.Media); err != nil {
		t.Fatal(err)
	}
	if state.warnings == 0 || !reflect.DeepEqual(subtitleTestTracks(t, fixture), []Subtitle{track}) {
		t.Fatal("a stale directory listing authorized subtitle retirement")
	}
	assertNoCatalogTestNotification(t, notifications)
	scanSubtitleCatalogFixture(t, fixture)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	if len(subtitleTestTracks(t, fixture)) != 0 {
		t.Fatal("a fresh stable scan did not retire the absent track")
	}
}

func TestSubtitleCatalogChangesFailedCommitPublishesNothingAndRecovers(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	notifications := catalogChangesTestListener(t, fixture.store)
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE FUNCTION reject_subtitle_notification_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'subtitle notification commit rejected'; END; $$;
		CREATE CONSTRAINT TRIGGER reject_subtitle_notification_commit AFTER INSERT OR UPDATE OR DELETE ON item_subtitles
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_subtitle_notification_commit()`); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(subtitleTestSRT, "Hello", "Later", 1)
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	job := libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Failed")
	if job.Error == "" || !reflect.DeepEqual(subtitleTestTracks(t, fixture), []Subtitle{track}) {
		t.Fatalf("failed subtitle commit changed the indexed projection: %+v", job)
	}
	assertNoCatalogTestNotification(t, notifications)
	if _, err := fixture.pool.Exec(fixture.ctx, `DROP TRIGGER reject_subtitle_notification_commit ON item_subtitles;
		DROP FUNCTION reject_subtitle_notification_commit()`); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	after := subtitleTestTracks(t, fixture)
	if len(after) != 1 || after[0].Index != track.Index || after[0].Tag == track.Tag {
		t.Fatalf("recovery did not publish the corrected source: %+v", after)
	}
}

func TestSubtitleCatalogChangesIgnoreAlreadyHiddenEmbeddedIndexRetirement(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	updated := *fixture.item.Media
	updated.Streams = append(append([]media.Stream(nil), updated.Streams...), media.Stream{Index: track.Index, CodecType: "subtitle", Codec: "subrip"})
	raw, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE items SET media = $2::jsonb WHERE id = $1", fixture.item.ID, raw); err != nil {
		t.Fatal(err)
	}
	if len(subtitleTestTracks(t, fixture)) != 0 {
		t.Fatal("the fixture did not hide the colliding external index")
	}
	notifications := catalogChangesTestListener(t, fixture.store)
	if err := os.WriteFile(path, []byte("invalid retained subtitle"), 0600); err != nil {
		t.Fatal(err)
	}
	job := libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if job.Error == "" || len(subtitleTestTracks(t, fixture)) != 0 {
		t.Fatalf("invalid colliding source became a public track: %+v", job)
	}
	var active bool
	if err := fixture.pool.QueryRow(fixture.ctx, "SELECT active FROM item_subtitles WHERE item_id = $1 AND stream_index = $2",
		fixture.item.ID, track.Index).Scan(&active); err != nil || active {
		t.Fatalf("the hidden old index was not retired: active=%t, error=%v", active, err)
	}
	assertNoCatalogTestNotification(t, notifications)
}

func subtitleCatalogTestUpdate(fixture mediaSourceFixture) []CatalogChange {
	return []CatalogChange{{Kind: CatalogUpdated, ItemID: fixture.item.ID, LibraryID: fixture.library.ID, ParentID: fixture.item.ParentID}}
}

func scanSubtitleCatalogFixture(t *testing.T, fixture mediaSourceFixture) {
	t.Helper()
	if job := libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed"); job.Error != "" {
		t.Fatalf("subtitle scan did not finish cleanly: %+v", job)
	}
}
