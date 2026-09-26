//go:build linux

package library

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestSubtitleScanNoMutationRetainsOwnedAndRejectedExternalTracks(t *testing.T) {
	fixture, sidecar, path := subtitleTestCatalog(t)
	owned := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	before := subtitleTestTracks(t, fixture)
	if !reflect.DeepEqual(before, []Subtitle{sidecar, owned}) {
		t.Fatalf("fixture did not preserve separate external and owned identities: %+v", before)
	}
	notifications := catalogChangesTestListener(t, fixture.store)
	if err := os.WriteFile(path, []byte("invalid replacement retained as an external identity"), 0600); err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		job := libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
		if job.Error == "" || !reflect.DeepEqual(subtitleTestTracks(t, fixture), before) {
			t.Fatalf("rejected external input changed existing subtitle identities: %+v", job)
		}
		assertNoCatalogTestNotification(t, notifications)
		if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID,
			media.SourceID(fixture.item.ID), sidecar.Index); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("invalid external bytes became readable: %v", err)
		}
		content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID,
			media.SourceID(fixture.item.ID), owned.Index)
		if err != nil || !bytes.Equal(content.Data, []byte(subtitleTestSRT)) || !reflect.DeepEqual(content.Info, owned) {
			t.Fatalf("a scan without accepted external input changed owned captions: %v", err)
		}
	}
	// A genuinely absent active sidecar must still retire and notify, even
	// though this scan has no accepted external input and an owned track remains.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	scanSubtitleCatalogFixture(t, fixture)
	assertScanCatalogChanges(t, notifications, subtitleCatalogTestUpdate(fixture))
	if tracks := subtitleTestTracks(t, fixture); !reflect.DeepEqual(tracks, []Subtitle{owned}) {
		t.Fatalf("absent external track was not retired independently: %+v", tracks)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID,
		media.SourceID(fixture.item.ID), sidecar.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired external identity remained readable: %v", err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		scanSubtitleCatalogFixture(t, fixture)
		assertNoCatalogTestNotification(t, notifications)
		if tracks := subtitleTestTracks(t, fixture); !reflect.DeepEqual(tracks, []Subtitle{owned}) {
			t.Fatalf("owned-only scan changed its public projection: %+v", tracks)
		}
		content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID,
			media.SourceID(fixture.item.ID), owned.Index)
		if err != nil || !bytes.Equal(content.Data, []byte(subtitleTestSRT)) || !reflect.DeepEqual(content.Info, owned) {
			t.Fatalf("owned-only scan changed readable content: %v", err)
		}
	}
}
