package library

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestMusicAlbumCatalogChangesPublishDerivedMetadataAndKeepCachedScansQuiet(t *testing.T) {
	prober := &metadataMusicScanProber{}
	facts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Track", Album: "Original album", Artist: "Original artist"}
	prober.set("Track.flac", facts, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Physical Album/Track.flac", "audio:derived-notification")
	collection := libraryIntegrationCreate(t, ctx, store, "Derived album notifications", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	track := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	album := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(path))
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: album.ID, IsFavorite: true, PlayCount: 2})
	beforeUserData := forceProbeUserDataSnapshot(t, ctx, pool, album.ID)
	notifications := catalogChangesTestListener(t, store)
	facts.Album, facts.Artist = "Revised album", "Revised artist"
	prober.set("Track.flac", facts, false)
	job := forceProbeTestScan(t, ctx, store, collection.ID, "Completed")
	if job.Error != "" || job.Updated != 1 {
		t.Fatalf("accepted track refresh failed: %+v", job)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{
		{Kind: CatalogUpdated, ItemID: track.ID, LibraryID: collection.ID, ParentID: album.ID},
		{Kind: CatalogUpdated, ItemID: album.ID, LibraryID: collection.ID, ParentID: album.ParentID, IsFolder: true},
		{Kind: CatalogUpdated, ItemID: track.ID, LibraryID: collection.ID, ParentID: album.ID},
	})
	current, err := store.GetItem(ctx, userID, album.ID)
	if err != nil || current.Name != "Revised album" || len(current.Entities.Artists) != 1 || current.Entities.Artists[0].Name != "Revised artist" {
		t.Fatalf("notified album did not expose the committed derived metadata: %+v, %v", current, err)
	}
	currentTrack, err := store.GetItem(ctx, userID, track.ID)
	if err != nil || currentTrack.Album == nil || currentTrack.Album.Name != "Revised album" ||
		len(currentTrack.Album.AlbumArtists) != 1 || currentTrack.Album.AlbumArtists[0].Name != "Revised artist" {
		t.Fatal("inherited track album projection did not match its notification")
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	assertNoCatalogTestNotification(t, notifications)
	if forceProbeUserDataSnapshot(t, ctx, pool, album.ID) != beforeUserData {
		t.Fatal("derived album notification changed user state")
	}
}

func TestMusicAlbumCatalogChangesKeepHiddenAutomaticChangesQuiet(t *testing.T) {
	prober := &metadataMusicScanProber{}
	facts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Track", Album: "Original album", Artist: "Stable artist"}
	prober.set("Track.flac", facts, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Physical Album/Track.flac", "audio:controlled-album")
	collection := libraryIntegrationCreate(t, ctx, store, "Controlled album notifications", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	track := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	album := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(path))
	actor := metadataEditTestActor(t, ctx, pool, "derived-album-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, album.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{
		"Name": json.RawMessage(`"Manual album"`), "SortName": json.RawMessage(`"Manual album order"`),
	}, []string{"Name"})
	before := metadataMusicScanSource(t, ctx, pool, album.ID)
	notifications := catalogChangesTestListener(t, store)
	facts.Album = "Hidden new album"
	prober.set("Track.flac", facts, false)
	if job := forceProbeTestScan(t, ctx, store, collection.ID, "Completed"); job.Error != "" {
		t.Fatal(job.Error)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: track.ID, LibraryID: collection.ID, ParentID: album.ID}})
	after := metadataEditTestDetail(t, ctx, store, actor, album.ID)
	if before == metadataMusicScanSource(t, ctx, pool, album.ID) || !reflect.DeepEqual(after.Effective, detail.Effective) ||
		!reflect.DeepEqual(after.LockedValues, detail.LockedValues) || !reflect.DeepEqual(after.Overrides, detail.Overrides) {
		t.Fatal("a quiet accepted source update failed to preserve the complete effective album controls")
	}
	prober.set("Track.flac", facts, true)
	if job := forceProbeTestScan(t, ctx, store, collection.ID, "Completed"); job.Error == "" {
		t.Fatal("failed probe did not retain its warning")
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestMusicAlbumCatalogChangesPublishOnlySuccessfulAlbumCommits(t *testing.T) {
	prober := &metadataMusicScanProber{}
	facts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Track", Album: "Original album", Artist: "Artist"}
	prober.set("Track.flac", facts, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Album/Track.flac", "audio:rollback-album")
	collection := libraryIntegrationCreate(t, ctx, store, "Atomic album notifications", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	track := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	album := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(path))
	before := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
	// Seed the already accepted track facts independently of the album commit.
	// The production album transaction still performs every derived write.
	facts.Album = "Rejected album"
	if _, err := pool.Exec(ctx, "UPDATE items SET media = jsonb_set(media, '{EmbeddedMusic}', $2::jsonb) WHERE id = $1",
		track.ID, metadataEditTestRaw(t, facts)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE item_metadata_state SET music_source = $2::jsonb WHERE item_id = $1", track.ID,
		metadataEditTestRaw(t, musicMetadataSource{Version: 1, Name: facts.Title, Album: facts.Album, Artists: []string{facts.Artist}, AlbumArtists: []string{}})); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_notified_album_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.type = 'MusicAlbum' AND NEW.name = 'Rejected album' THEN RAISE EXCEPTION 'album commit fixture rejection'; END IF; RETURN NEW; END; $$;
		CREATE CONSTRAINT TRIGGER reject_notified_album_commit AFTER UPDATE ON items
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_notified_album_commit()`); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	if ready, err := store.refreshOwnedMusicAlbum(ctx, collection.ID, album.ID); err == nil || ready {
		t.Fatal("the deferred album commit rejection was ignored")
	}
	assertNoCatalogTestNotification(t, notifications)
	if metadataMusicScanStateSnapshot(t, ctx, pool, album.ID) != before {
		t.Fatal("a failed album commit changed its accepted source or relationships")
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER reject_notified_album_commit ON items; DROP FUNCTION reject_notified_album_commit()"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if ready, err := store.refreshOwnedMusicAlbum(cancelled, collection.ID, album.ID); err == nil || ready {
		t.Fatal("cancelled album publication was accepted")
	}
	assertNoCatalogTestNotification(t, notifications)
	if ready, err := store.refreshOwnedMusicAlbum(ctx, collection.ID, album.ID); err != nil || !ready {
		t.Fatalf("accepted album did not commit after the fixture rejection was removed: %v", err)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{
		{Kind: CatalogUpdated, ItemID: album.ID, LibraryID: collection.ID, ParentID: album.ParentID, IsFolder: true},
		{Kind: CatalogUpdated, ItemID: track.ID, LibraryID: collection.ID, ParentID: album.ID},
	})
	if current, err := store.GetItem(ctx, userID, album.ID); err != nil || current.Name != "Rejected album" {
		t.Fatal("album notification preceded its independently visible commit")
	}
}

func TestMusicAlbumCatalogChangesIncompleteRootRetainsPublishedAlbum(t *testing.T) {
	prober := &metadataMusicScanProber{}
	facts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Track", Album: "Original album", Artist: "Original artist"}
	prober.set("First.flac", facts, false)
	prober.set("Second.flac", facts, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Album/First.flac", "audio:incomplete-first")
	libraryIntegrationFile(t, root, "music/Album/Second.flac", "audio:incomplete-second")
	collection := libraryIntegrationCreate(t, ctx, store, "Incomplete album notifications", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	track := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	album := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(path))
	before := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
	notifications := catalogChangesTestListener(t, store)
	facts.Album, facts.Artist = "Incomplete new album", "Incomplete new artist"
	prober.set("First.flac", facts, false)
	prober.set("Second.flac", facts, true)
	if job := forceProbeTestScan(t, ctx, store, collection.ID, "Completed"); job.Error == "" || job.Updated != 1 {
		t.Fatalf("incomplete scan did not retain its partial outcome: %+v", job)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: track.ID, LibraryID: collection.ID, ParentID: album.ID}})
	if metadataMusicScanStateSnapshot(t, ctx, pool, album.ID) != before {
		t.Fatal("an incomplete root changed its derived album source")
	}
}

func TestMusicAlbumCatalogChangesBoundInheritedInvalidation(t *testing.T) {
	prober := &metadataMusicScanProber{}
	facts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Track", Album: "Original album", Artist: "Artist"}
	prober.set("Track.flac", facts, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Album/Track.flac", "audio:bounded-album")
	collection := libraryIntegrationCreate(t, ctx, store, "Bounded album notifications", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	track := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	album := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(path))
	facts.Album = "Expanded album"
	encoded := metadataEditTestRaw(t, facts)
	if _, err := pool.Exec(ctx, "UPDATE items SET media = jsonb_set(media, '{EmbeddedMusic}', $2::jsonb) WHERE id = $1", track.ID, encoded); err != nil {
		t.Fatal(err)
	}
	// Accepted catalog rows exercise the fan-out bound without creating or
	// probing a thousand unrelated physical media fixtures.
	if _, err := pool.Exec(ctx, `INSERT INTO items(id, library_id, root_id, parent_id, name, sort_name, type,
		path, relative_path, media)
		SELECT 'album-notification-' || n, $1, owner.root_id, owner.id, 'Accepted track', 'Accepted track', 'Audio',
		owner.path || '/generated-' || n || '.flac', owner.relative_path || '/generated-' || n || '.flac',
		jsonb_build_object('EmbeddedMusic', $3::jsonb)
		FROM items owner CROSS JOIN generate_series(1, $4::integer) n WHERE owner.id = $2`,
		collection.ID, album.ID, encoded, maxCatalogChanges); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	if ready, err := store.refreshOwnedMusicAlbum(ctx, collection.ID, album.ID); err != nil || !ready {
		t.Fatalf("bounded album publication failed: %v", err)
	}
	notification := nextCatalogTestNotification(t, notifications)
	if !notification.Resync || notification.Changes != nil {
		t.Fatal("excess inherited changes produced a partial catalog publication")
	}
	assertNoCatalogTestNotification(t, notifications)
	if current, err := store.GetItem(ctx, userID, album.ID); err != nil || current.Name != "Expanded album" {
		t.Fatal("bounded invalidation changed the successful album commit result")
	}
}

func TestMusicAlbumCatalogChangesDoNotInvalidateNestedAlbumReferences(t *testing.T) {
	prober := &metadataMusicScanProber{}
	outerFacts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Outer track", Album: "Outer album", Artist: "Outer artist"}
	innerFacts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Inner track", Album: "Inner album", Artist: "Inner artist"}
	prober.set("Outer.flac", outerFacts, false)
	prober.set("Inner.flac", innerFacts, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	outerPath := libraryIntegrationFile(t, root, "music/Outer/Outer.flac", "audio:outer-album")
	innerPath := libraryIntegrationFile(t, root, "music/Outer/Inner/Inner.flac", "audio:inner-album")
	collection := libraryIntegrationCreate(t, ctx, store, "Nested album notifications", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	outerTrack := nfoCatalogItem(t, ctx, store, userID, collection.ID, outerPath)
	innerTrack := nfoCatalogItem(t, ctx, store, userID, collection.ID, innerPath)
	outerAlbum := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(outerPath))
	innerAlbum := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(innerPath))
	if innerAlbum.ParentID != outerAlbum.ID || innerTrack.Album == nil || innerTrack.Album.ID != innerAlbum.ID {
		t.Fatal("the fixture did not establish separate nearest album references")
	}
	outerFacts.Album = "Revised outer album"
	if _, err := pool.Exec(ctx, "UPDATE items SET media = jsonb_set(media, '{EmbeddedMusic}', $2::jsonb) WHERE id = $1",
		outerTrack.ID, metadataEditTestRaw(t, outerFacts)); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	if ready, err := store.refreshOwnedMusicAlbum(ctx, collection.ID, outerAlbum.ID); err != nil || !ready {
		t.Fatalf("outer album publication failed: %v", err)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{
		{Kind: CatalogUpdated, ItemID: outerAlbum.ID, LibraryID: collection.ID, ParentID: outerAlbum.ParentID, IsFolder: true},
		{Kind: CatalogUpdated, ItemID: outerTrack.ID, LibraryID: collection.ID, ParentID: outerAlbum.ID},
	})
	if current, err := store.GetItem(ctx, userID, innerTrack.ID); err != nil || !reflect.DeepEqual(current.Album, innerTrack.Album) {
		t.Fatal("outer album publication changed a nested album reference")
	}
}

func TestMusicAlbumCatalogChangesInvalidateDirectThemeAudioReferences(t *testing.T) {
	prober := &metadataMusicScanProber{}
	facts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Track", Album: "Original album", Artist: "Artist"}
	prober.set("Track.flac", facts, false)
	prober.set("theme.mp3", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Theme"}, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Album/Track.flac", "audio:album-track")
	libraryIntegrationFile(t, root, "music/Album/theme.mp3", "audio:album-theme")
	collection := libraryIntegrationCreate(t, ctx, store, "Theme album reference notifications", "music", filepath.Join(root, "music"))
	if job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed"); job.Error != "" {
		t.Fatal(job.Error)
	}
	track := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	album := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(path))
	resources := themeScanTestResources(t, ctx, pool, collection.ID)
	if len(resources) != 1 || !resources[0].active || resources[0].ownerID != album.ID {
		t.Fatal("the fixture did not establish an active album-owned theme")
	}
	theme, err := store.GetItem(ctx, userID, resources[0].id)
	if err != nil || theme.Album == nil || theme.Album.Name != "Original album" {
		t.Fatal("the direct theme did not inherit the album projection")
	}
	facts.Album = "Revised theme album"
	if _, err := pool.Exec(ctx, "UPDATE items SET media = jsonb_set(media, '{EmbeddedMusic}', $2::jsonb) WHERE id = $1",
		track.ID, metadataEditTestRaw(t, facts)); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	if ready, err := store.refreshOwnedMusicAlbum(ctx, collection.ID, album.ID); err != nil || !ready {
		t.Fatalf("album publication failed: %v", err)
	}
	ids := []string{track.ID, theme.ID}
	sort.Strings(ids)
	want := []CatalogChange{{Kind: CatalogUpdated, ItemID: album.ID, LibraryID: collection.ID, ParentID: album.ParentID, IsFolder: true}}
	for _, id := range ids {
		want = append(want, CatalogChange{Kind: CatalogUpdated, ItemID: id, LibraryID: collection.ID, ParentID: album.ID})
	}
	assertScanCatalogChanges(t, notifications, want)
	if current, err := store.GetItem(ctx, userID, theme.ID); err != nil || current.Album == nil || current.Album.Name != "Revised theme album" {
		t.Fatal("direct theme metadata did not match its inherited-change notification")
	}
}
