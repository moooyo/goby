package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/providers"
)

func TestStoreMusicControlsNumbersComposerAndProviderSurviveScanAndRestart(t *testing.T) {
	prober := &metadataMusicScanProber{}
	facts := media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Track", Album: "Album",
		Artist: "Display A & B", Artists: []string{"One", "Two"}, AlbumArtists: []string{"Ensemble"},
		Composers: []string{"Composer One", "Composer Two"}, Genres: []string{"Classical"}, TrackNumber: 3, DiscNumber: 2}
	prober.set("01 Track.flac", &facts, false)
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, prober)
	actor := metadataEditTestActor(t, ctx, pool, "music-controls-admin")
	path := libraryIntegrationFile(t, root, "music/Album/01 Track.flac", "audio:music-three")
	catalog := libraryIntegrationCreate(t, ctx, store, "Music controls", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, catalog.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, viewer, catalog.ID, path)
	if item.IndexNumber != 3 || item.ParentIndexNumber != 2 || len(item.Entities.Artists) != 2 || len(item.Entities.People) != 2 {
		t.Fatalf("scanned music was not materialized: %+v", item)
	}
	firstID, composerID := item.Entities.Artists[0].ID, item.Entities.People[0].ID
	before := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	manual := map[string]json.RawMessage{"Artists": metadataEditTestRaw(t, []string{"Manual & Solo"}),
		"Album": metadataEditTestRaw(t, "Manual album"), "ParentIndexNumber": json.RawMessage(`4`)}
	locked := metadataEditTestUpdate(t, ctx, store, actor, before, manual, []string{"AlbumArtists", "IndexNumber"})
	provider := providers.Metadata{Selection: providers.Selection{Provider: "musicbrainz", ID: "abcdef01-2345-6789-abcd-0123456789ab", Type: "Audio"},
		SourceURL: "https://musicbrainz.org/recording/abcdef01-2345-6789-abcd-0123456789ab",
		Fields: map[string]json.RawMessage{"Artists": metadataEditTestRaw(t, []string{"Provider artist"}),
			"People": json.RawMessage(`[{"Name":"Provider composer","Type":"Composer"}]`)}}
	if _, err := store.ApplyOnlineMetadata(ctx, actor, item.ID, locked.Revision, provider); err != nil {
		t.Fatal("offline provider composition", err)
	}
	facts.Artists, facts.AlbumArtists, facts.Composers = []string{"Fresh One", "Fresh Two"}, []string{"Fresh Ensemble"}, []string{"Fresh composer"}
	facts.Album, facts.TrackNumber, facts.DiscNumber = "Fresh album", 7, 5
	prober.set("01 Track.flac", &facts, false)
	if err := os.WriteFile(path, []byte("audio:music-new-seven"), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, catalog.ID, "Completed")
	after := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if !reflect.DeepEqual(after.Effective.Artists, []string{"Manual & Solo"}) || after.Effective.Album != "Manual album" ||
		!reflect.DeepEqual(after.Effective.AlbumArtists, []string{"Ensemble"}) || *after.Effective.IndexNumber != 3 || *after.Effective.ParentIndexNumber != 4 ||
		len(after.Effective.People) != 1 || after.Effective.People[0].Name != "Provider composer" {
		t.Fatalf("scan or provider application lost music controls: %+v", after.Effective)
	}
	reset := metadataEditTestUpdate(t, ctx, store, actor, after, map[string]json.RawMessage{}, []string{})
	if !reflect.DeepEqual(reset.Effective.Artists, []string{"Provider artist"}) || reset.Effective.Album != "Fresh album" ||
		*reset.Effective.IndexNumber != 7 || *reset.Effective.ParentIndexNumber != 5 || !reflect.DeepEqual(reset.Effective.AlbumArtists, []string{"Fresh Ensemble"}) {
		t.Fatalf("reset failed to restore current accepted automatic sources: %+v", reset.Effective)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(pool, prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	entitiesStoreCleanup(t, reopened)
	persisted := metadataEditTestDetail(t, ctx, reopened, actor, item.ID)
	if !reflect.DeepEqual(persisted.Effective, reset.Effective) || persisted.Revision != reset.Revision {
		t.Fatal("restart changed saved music controls")
	}
	var retained int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM catalog_entities WHERE id::text=ANY($1::text[])`, []string{strconv.FormatInt(firstID, 10), composerID}).Scan(&retained); err != nil || retained != 2 {
		t.Fatal("credit replacement deleted durable music/person identities", err)
	}
}

func TestStoreMusicUncontrolledAlbumTagNotifiesWithoutChangingPhysicalAlbum(t *testing.T) {
	prober := &metadataMusicScanProber{}
	facts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Track", Album: "Original tag", Artist: "Stable artist"}
	prober.set("Track.flac", facts, false)
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Physical Album/Track.flac", "audio:visible-album-tag")
	catalog := libraryIntegrationCreate(t, ctx, store, "Visible album tags", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, catalog.ID, "Completed")
	track := nfoCatalogItem(t, ctx, store, viewer, catalog.ID, path)
	album := nfoCatalogItem(t, ctx, store, viewer, catalog.ID, filepath.Dir(path))
	actor := metadataEditTestActor(t, ctx, pool, "music-tag-editor")
	before := metadataEditTestDetail(t, ctx, store, actor, album.ID)
	metadataEditTestUpdate(t, ctx, store, actor, before, map[string]json.RawMessage{
		"Name": json.RawMessage(`"Controlled display"`), "SortName": json.RawMessage(`"Controlled sort"`),
	}, []string{"Name"})
	notifications := catalogChangesTestListener(t, store)
	facts.Album = "Changed visible tag"
	prober.set("Track.flac", facts, false)
	if job := forceProbeTestScan(t, ctx, store, catalog.ID, "Completed"); job.Error != "" {
		t.Fatal(job.Error)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{
		{Kind: CatalogUpdated, ItemID: track.ID, LibraryID: catalog.ID, ParentID: album.ID},
		{Kind: CatalogUpdated, ItemID: album.ID, LibraryID: catalog.ID, ParentID: album.ParentID, IsFolder: true},
	})
	after := metadataEditTestDetail(t, ctx, store, actor, album.ID)
	current := nfoCatalogItem(t, ctx, store, viewer, catalog.ID, path)
	if after.Effective.Album != facts.Album || after.Effective.Name != "Controlled display" ||
		current.ParentID != track.ParentID || current.Album == nil || current.Album.ID != album.ID || current.Album.Name != "Controlled display" {
		t.Fatal("album tag edits changed the physical album identity or disappeared from metadata")
	}
}
