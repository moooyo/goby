package library

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/providers"
)

func TestGeneratedSortingFollowsCurrentRulesAcrossScanProviderAndContainerRename(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
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
	beforeGenerated, beforeExplicit := metadataEditTestSnapshot(t, ctx, pool, item.ID), metadataEditTestSnapshot(t, ctx, pool, local.ID)
	probeCalls := len(prober.calls())
	cached := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if cached.Error != "" || cached.Added != 0 || cached.Updated != 0 || len(prober.calls()) != probeCalls || metadataEditTestSnapshot(t, ctx, pool, item.ID) != beforeGenerated || metadataEditTestSnapshot(t, ctx, pool, local.ID) != beforeExplicit {
		t.Fatalf("configured generated/explicit keys caused a cached ordinary write: %+v", cached)
	}
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

func TestCachedOrdinaryScanPreservesExplicitOnlineAndHistoricalSortProvenance(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		_, err := tx.Exec(`UPDATE managed_settings SET sort_remove_words=ARRAY['The']`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	onlinePath := libraryIntegrationFile(t, root, "explicit-cache/The Online.mp4", "video:explicit-online")
	historicalPath := libraryIntegrationFile(t, root, "explicit-cache/The Historical.mp4", "video:explicit-history")
	collection := libraryIntegrationCreate(t, ctx, store, "Cached explicit sorting", "movies", filepath.Dir(onlinePath))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	online := nfoCatalogItem(t, ctx, store, userID, collection.ID, onlinePath)
	historical := nfoCatalogItem(t, ctx, store, userID, collection.ID, historicalPath)
	actor := metadataEditTestActor(t, ctx, pool, "explicit-sort-cache-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, online.ID)
	// The explicit online key intentionally equals the historical unstripped
	// filename key. A generated-key comparison alone would invent a change.
	if _, err := store.ApplyOnlineMetadata(ctx, actor, online.ID, detail.Revision, providers.Metadata{Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, Fields: map[string]json.RawMessage{"Name": json.RawMessage(`"The Online"`), "SortName": json.RawMessage(`"the online"`)}}); err != nil {
		t.Fatal(err)
	}
	// Model a valid conservative migration marker whose original custom source
	// is no longer classified. A cached visit has no new source fact to replace it.
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if _, err := tx.Exec(`UPDATE items SET sort_name='archival order' WHERE id=$1`, historical.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE item_metadata_state SET automatic=jsonb_set(automatic,'{SortName}','"archival order"'::jsonb),automatic_sort_name_explicit=true WHERE item_id=$1`, historical.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, words := range [][]string{{"The"}, {}} {
		if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
			if _, err := tx.Exec(`UPDATE managed_settings SET sort_remove_words=$1`, words); err != nil {
				return err
			}
			return RebuildGeneratedSortNames(tx)
		}); err != nil {
			t.Fatal(err)
		}
		beforeOnline, beforeHistorical := metadataEditTestSnapshot(t, ctx, pool, online.ID), metadataEditTestSnapshot(t, ctx, pool, historical.ID)
		calls := len(prober.calls())
		for visit := 0; visit < 2; visit++ {
			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if job.Error != "" || job.Added != 0 || job.Updated != 0 || len(prober.calls()) != calls || metadataEditTestSnapshot(t, ctx, pool, online.ID) != beforeOnline || metadataEditTestSnapshot(t, ctx, pool, historical.ID) != beforeHistorical {
				t.Fatalf("cached scan rewrote accepted explicit provenance: %+v", job)
			}
		}
		for id, want := range map[string]string{online.ID: "the online", historical.ID: "archival order"} {
			var key string
			var explicit bool
			if err := pool.QueryRow(ctx, `SELECT i.sort_name,ms.automatic_sort_name_explicit FROM items i JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.id=$1`, id).Scan(&key, &explicit); err != nil || key != want || !explicit {
				t.Fatalf("cached provenance or explicit key changed: %v", err)
			}
		}
	}
}
