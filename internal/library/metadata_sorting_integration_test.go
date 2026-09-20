package library

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/providers"
)

func TestGeneratedSortingFollowsCurrentRulesAcrossScanProviderAndContainerRename(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		_, err := tx.Exec(`UPDATE managed_settings SET sort_remove_words=ARRAY['The']`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	path := libraryIntegrationFile(t, root, "sorting/The Amber.mp4", "video:sorting")
	localPath := libraryIntegrationFile(t, root, "sorting/The Local.mp4", "video:explicit-local")
	libraryIntegrationFile(t, root, "sorting/The Local.nfo", `<movie><title>The Local</title><sorttitle>The explicit nfo key</sorttitle></movie>`)
	collection := libraryIntegrationCreate(t, ctx, store, "The Library", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	local := nfoCatalogItem(t, ctx, store, userID, collection.ID, localPath)
	assertKey := func(id, want string) {
		t.Helper()
		var got string
		if err := pool.QueryRow(ctx, `SELECT sort_name FROM items WHERE id=$1`, id).Scan(&got); err != nil || got != want {
			t.Fatalf("key %s=%q want %q: %v", id, got, want, err)
		}
	}
	assertKey(collection.ID, "library")
	assertKey(item.ID, "amber")
	assertKey(local.ID, "the explicit nfo key")
	libraryIntegrationFile(t, root, "sorting/The Amber.nfo", `<movie><title>The Apricot</title></movie>`)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	assertKey(item.ID, "apricot")
	actor := metadataEditTestActor(t, ctx, pool, "sorting-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	detail, err := store.ApplyOnlineMetadata(ctx, actor, item.ID, detail.Revision, providers.Metadata{Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, Fields: map[string]json.RawMessage{"Name": json.RawMessage(`"The Banana"`)}})
	if err != nil {
		t.Fatal(err)
	}
	assertKey(item.ID, "banana")
	if detail.Automatic.SortName != "banana" || detail.Effective.SortName != "banana" {
		t.Fatal("native metadata projection disagrees with persisted generated key")
	}
	manual := metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{"Name": json.RawMessage(`"Manual Display Name"`)}, nil)
	if manual.Effective.SortName != "banana" {
		t.Fatal("Name-only override changed the independent sort field")
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	assertKey(item.ID, "banana")
	detail = metadataEditTestDetail(t, ctx, store, actor, item.ID)
	_, err = store.ApplyOnlineMetadata(ctx, actor, item.ID, detail.Revision, providers.Metadata{Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, Fields: map[string]json.RawMessage{"Name": json.RawMessage(`"The Cherry"`), "SortName": json.RawMessage(`"The explicit online key"`)}})
	if err != nil {
		t.Fatal(err)
	}
	assertKey(item.ID, "The explicit online key")
	playlist, err := store.CreateCollection(ctx, Subject{UserID: userID}, PlaylistKind, CollectionInput{Name: "The Playlist"})
	if err != nil {
		t.Fatal(err)
	}
	assertKey(playlist.ID, "playlist")
	name := "The Renamed Playlist"
	if _, err := store.UpdateCollection(ctx, Subject{UserID: userID}, playlist.ID, PlaylistKind, CollectionPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	assertKey(playlist.ID, "renamed playlist")
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if _, err := tx.Exec(`UPDATE managed_settings SET sort_remove_words='{}'`); err != nil {
			return err
		}
		return RebuildGeneratedSortNames(tx)
	}); err != nil {
		t.Fatal(err)
	}
	assertKey(playlist.ID, "the renamed playlist")
	assertKey(item.ID, "The explicit online key")
	assertKey(local.ID, "the explicit nfo key")
}
