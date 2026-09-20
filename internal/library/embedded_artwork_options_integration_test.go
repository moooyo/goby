//go:build linux

package library

import (
	"image/color"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func TestEmbeddedArtworkOptionControlsNewExtractionAndPreservesAcceptedImages(t *testing.T) {
	picture := embeddedArtworkTestPicture(t, 2, "Front", color.NRGBA{R: 170, A: 255})
	prober := &embeddedArtworkFixtureProber{result: media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture}}}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	firstPath := libraryIntegrationFile(t, root, "embedded-options/First.flac", "audio:first")
	collection := libraryIntegrationCreate(t, ctx, store, "Artwork selectors", "music", filepath.Dir(firstPath))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	first := nfoCatalogItem(t, ctx, store, userID, collection.ID, firstPath)
	embeddedArtworkAssertOpen(t, ctx, store, userID, first.ID, picture)
	actor := metadataEditTestActor(t, ctx, pool, "embedded-options-editor")
	update := func(enabled bool) {
		t.Helper()
		before, err := store.GetLibraryEditing(ctx, collection.ID)
		if err != nil {
			t.Fatal(err)
		}
		after, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID, LibraryUpdate{Revision: before.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableEmbeddedArtwork: &enabled}})
		if err != nil || EffectiveLibraryOptions(after.Library).EnableEmbeddedArtwork != enabled || !EffectiveLibraryOptions(after.Library).EnableLocalImages {
			t.Fatalf("save independent artwork selector: %v", err)
		}
	}
	update(false)
	_, beforeExtractions := prober.counts()
	secondPath := libraryIntegrationFile(t, root, "embedded-options/Second.flac", "audio:second")
	imageScanTestWrite(t, filepath.Join(filepath.Dir(secondPath), "Second-cover.png"), color.NRGBA{B: 190, A: 255})
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	second := nfoCatalogItem(t, ctx, store, userID, collection.ID, secondPath)
	if _, afterExtractions := prober.counts(); afterExtractions != beforeExtractions {
		t.Fatal("disabled dynamic provider extracted new artwork")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM item_embedded_artwork WHERE item_id=$1`, second.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("disabled provider wrote an absence/cache entry: %v", err)
	}
	embeddedArtworkAssertOpen(t, ctx, store, userID, first.ID, picture)
	reader, image, err := store.OpenImageContentFor(ctx, Subject{UserID: userID}, second.ID, "Primary", 0)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	if image.Source == "embedded" {
		t.Fatal("dynamic selector suppressed or replaced directory sidecar")
	}
	update(true)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if _, afterExtractions := prober.counts(); afterExtractions != beforeExtractions+1 {
		t.Fatal("re-enabled provider did not fill missing artwork from cached audio probe")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM item_embedded_artwork WHERE item_id=$1 AND status='ready'`, second.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("re-enabled provider did not persist source-bound artwork: %v", err)
	}
	reader, image, err = store.OpenImageContentFor(ctx, Subject{UserID: userID}, second.ID, "Primary", 0)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	if image.Source == "embedded" {
		t.Fatal("new extraction changed the established sidecar precedence")
	}
}
