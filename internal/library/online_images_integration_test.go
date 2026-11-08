package library

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/systemevents"
)

type onlineImageTestProvider struct {
	data          []byte
	listCalls     int
	downloadCalls int
	onList        func(providers.Selection) ([]providers.RemoteImage, error)
	onDownload    func(context.Context, providers.RemoteImage) error
}

func (*onlineImageTestProvider) Status() []providers.Status {
	return []providers.Status{{ID: "tmdb", Configured: true}}
}

func (client *onlineImageTestProvider) Images(_ context.Context, selected providers.Selection) ([]providers.RemoteImage, error) {
	client.listCalls++
	if client.onList != nil {
		return client.onList(selected)
	}
	return []providers.RemoteImage{
		{Selection: selected, ImageID: "/poster.png", ImageType: "Primary"},
		{Selection: selected, ImageID: "/backdrop.png", ImageType: "Backdrop"},
		{Selection: selected, ImageID: "/logo.png", ImageType: "Logo"},
	}, nil
}

func (client *onlineImageTestProvider) DownloadImage(ctx context.Context, selected providers.RemoteImage) ([]byte, error) {
	client.downloadCalls++
	if client.onDownload != nil {
		if err := client.onDownload(ctx, selected); err != nil {
			return nil, err
		}
	}
	return client.data, nil
}

func onlineImageIntegrationFixture(t *testing.T) (context.Context, *Store, identity.Principal) {
	t.Helper()
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	providerMetadataTargetFixture(t, ctx, pool)
	actor := metadataEditTestActor(t, ctx, pool, "online-image-editor")
	onlineImageTestMatch(t, ctx, store, actor, "provider-target-item", "42", "Movie")
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		_, err := tx.Exec(`UPDATE libraries SET options=jsonb_set(options,'{EnableOnlineImages}','true'::jsonb),revision=revision+1 WHERE id='provider-target-library'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return systemevents.WithDerived(ctx), store, actor
}

func onlineImageTestMatch(t *testing.T, ctx context.Context, store *Store, actor identity.Principal, itemID, providerID, itemType string) {
	t.Helper()
	detail, err := store.GetItemMetadata(ctx, actor, itemID)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := json.Marshal(map[string]string{"Tmdb": providerID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyOnlineMetadata(ctx, actor, itemID, detail.Revision, providers.Metadata{
		Selection: providers.Selection{Provider: "tmdb", ID: providerID, Type: itemType},
		Fields:    map[string]json.RawMessage{"ProviderIds": ids},
	}); err != nil {
		t.Fatal(err)
	}
}

func onlineImageTestFence(context.Context) AnalysisFence {
	return func(OwnedTx) error { return nil }
}

func onlineImageTestCacheCount(t *testing.T, ctx context.Context, store *Store, itemID string) int {
	t.Helper()
	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM item_provider_images WHERE item_id=$1`, itemID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func onlineImageTestSourceState(t *testing.T, ctx context.Context, store *Store) (string, string, string) {
	t.Helper()
	var item, metadata, events string
	if err := store.pool.QueryRow(ctx, `SELECT to_jsonb(i)::text,to_jsonb(ms)::text FROM items i
		JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.id='provider-target-item'`).Scan(&item, &metadata); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT jsonb_object_agg(name,sequence)::text FROM task_system_events
		WHERE name IN ('LibraryChanged','OnlineImagesRequested')`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	return item, metadata, events
}

func TestOnlineImagesFillsOnlyMissingArtworkWithoutChangingSourceOrRecursing(t *testing.T) {
	ctx, store, actor := onlineImageIntegrationFixture(t)
	detail, err := store.GetItemMetadata(ctx, actor, "provider-target-item")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateItemMetadata(ctx, actor, detail.ItemID, MetadataEdit{Revision: detail.Revision,
		Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Administrator title"`)}, LockedFields: []string{"Overview"}}); err != nil {
		t.Fatal(err)
	}
	beforeItem, beforeMetadata, beforeEvents := onlineImageTestSourceState(t, ctx, store)
	client := &onlineImageTestProvider{data: imageStoreTestPNG(t)}
	var last ProviderProgress
	if err := store.FillMissingOnlineImages(ctx, client, "provider-target-library", onlineImageTestFence(ctx), func(value ProviderProgress) { last = value }); err != nil {
		t.Fatal(err)
	}
	if client.listCalls != 1 || client.downloadCalls != 3 || last.Processed != 1 || last.Added != 3 || onlineImageTestCacheCount(t, ctx, store, "provider-target-item") != 3 {
		t.Fatalf("missing-image work was not committed accurately: lists=%d downloads=%d progress=%+v", client.listCalls, client.downloadCalls, last)
	}
	item, metadata, events := onlineImageTestSourceState(t, ctx, store)
	if item != beforeItem || metadata != beforeMetadata || events != beforeEvents {
		t.Fatal("automatic images changed source/metadata controls or recursively emitted task events")
	}
	if err := store.FillMissingOnlineImages(ctx, client, "provider-target-library", onlineImageTestFence(ctx), nil); err != nil {
		t.Fatal(err)
	}
	if client.listCalls != 1 || client.downloadCalls != 3 {
		t.Fatal("a complete image set generated provider traffic or replaced existing content")
	}
}

func TestOnlineImagesPreservesSelectedImagesAndManagedDeletionMasks(t *testing.T) {
	ctx, store, actor := onlineImageIntegrationFixture(t)
	data := imageStoreTestPNG(t)
	detail, err := store.GetItemMetadata(ctx, actor, "provider-target-item")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectProviderImage(ctx, actor, detail.ItemID, detail.Revision, providers.RemoteImage{
		Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, ImageID: "/selected.png", ImageType: "Primary",
	}, 0, data); err != nil {
		t.Fatal(err)
	}
	target := ArtworkTarget{ItemID: detail.ItemID}
	collection, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	collection, err = store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, collection.Revision, "Logo", 0, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteArtwork(ctx, actor, identity.AdministratorNative, target, collection.Revision, "Logo", 0); err != nil {
		t.Fatal(err)
	}
	client := &onlineImageTestProvider{data: data}
	if err := store.FillMissingOnlineImages(ctx, client, "provider-target-library", onlineImageTestFence(ctx), nil); err != nil {
		t.Fatal(err)
	}
	if client.downloadCalls != 1 || onlineImageTestCacheCount(t, ctx, store, detail.ItemID) != 2 {
		t.Fatal("automatic downloads replaced an existing poster or revived a deliberate Logo deletion")
	}
	var selectedID string
	if err := store.pool.QueryRow(ctx, `SELECT image_id FROM item_provider_images WHERE item_id=$1 AND image_type='Primary'`, detail.ItemID).Scan(&selectedID); err != nil || selectedID != "/selected.png" {
		t.Fatalf("selected online image was overwritten: %q, %v", selectedID, err)
	}
}

func TestOnlineImagesRejectsConcurrentMetadataLibraryAndArtworkChanges(t *testing.T) {
	for _, change := range []string{"metadata", "library", "policy", "artwork"} {
		t.Run(change, func(t *testing.T) {
			ctx, store, actor := onlineImageIntegrationFixture(t)
			client := &onlineImageTestProvider{data: imageStoreTestPNG(t)}
			client.onDownload = func(ctx context.Context, selected providers.RemoteImage) error {
				switch change {
				case "metadata":
					onlineImageTestMatch(t, ctx, store, actor, "provider-target-item", "99", "Movie")
				case "library", "policy":
					return store.WithOwnedTx(ctx, func(tx OwnedTx) error {
						statement := `UPDATE libraries SET name='Changed library',revision=revision+1 WHERE id='provider-target-library'`
						if change == "policy" {
							statement = `UPDATE libraries SET options=jsonb_set(options,'{EnableOnlineImages}','false'::jsonb),revision=revision+1 WHERE id='provider-target-library'`
						}
						_, err := tx.Exec(statement)
						return err
					})
				case "artwork":
					target := ArtworkTarget{ItemID: "provider-target-item"}
					current, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
					if err != nil {
						return err
					}
					_, err = store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, current.Revision, selected.ImageType, 0, client.data)
					return err
				}
				return nil
			}
			if err := store.FillMissingOnlineImages(ctx, client, "provider-target-library", onlineImageTestFence(ctx), nil); err != nil {
				t.Fatal(err)
			}
			if client.downloadCalls != 1 || onlineImageTestCacheCount(t, ctx, store, "provider-target-item") != 0 {
				t.Fatal("an obsolete download committed after a newer administrator mutation")
			}
		})
	}
}

func TestOnlineImagesFinalFenceRollsBackImageAndCatalogChanges(t *testing.T) {
	ctx, store, _ := onlineImageIntegrationFixture(t)
	client := &onlineImageTestProvider{data: imageStoreTestPNG(t)}
	beforeItem, beforeMetadata, beforeEvents := onlineImageTestSourceState(t, ctx, store)
	fences := 0
	fence := func(OwnedTx) error {
		fences++
		if fences == 2 {
			return context.Canceled
		}
		return nil
	}
	if err := store.FillMissingOnlineImages(ctx, client, "provider-target-library", fence, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("a denied final publication fence did not cancel: %v", err)
	}
	if fences != 2 || onlineImageTestCacheCount(t, ctx, store, "provider-target-item") != 0 {
		t.Fatal("image bytes committed outside the two publication fences")
	}
	item, metadata, events := onlineImageTestSourceState(t, ctx, store)
	if item != beforeItem || metadata != beforeMetadata || events != beforeEvents {
		t.Fatal("a rolled-back image published source, metadata, or task notifications")
	}
}

func TestOnlineImagesProviderFailuresRemainVisible(t *testing.T) {
	ctx, store, _ := onlineImageIntegrationFixture(t)
	client := &onlineImageTestProvider{onList: func(providers.Selection) ([]providers.RemoteImage, error) {
		return nil, providers.ErrQuota
	}}
	if err := store.FillMissingOnlineImages(ctx, client, "provider-target-library", onlineImageTestFence(ctx), nil); !errors.Is(err, providers.ErrQuota) {
		t.Fatalf("provider quota failure became a successful empty run: %v", err)
	}
	if client.listCalls != 1 || client.downloadCalls != 0 || onlineImageTestCacheCount(t, ctx, store, "provider-target-item") != 0 {
		t.Fatal("provider failure triggered an unreported image write")
	}
}

func TestOnlineImagesRejectsParentSeriesRematchWithoutChildRevisionChange(t *testing.T) {
	ctx, store, actor := onlineImageIntegrationFixture(t)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path,index_number,parent_index_number) VALUES
		('image-series','provider-target-library','provider-target-root','provider-target-parent','Series','series','Series',true,'Series',0,0),
		('image-season','provider-target-library','provider-target-root','image-series','Season 1','season 1','Season',true,'Series/Season 1',1,0),
		('image-episode','provider-target-library','provider-target-root','image-season','Episode 2','episode 2','Episode',false,'Series/Season 1/Episode 2.mp4',2,1)`); err != nil {
		t.Fatal(err)
	}
	onlineImageTestMatch(t, ctx, store, actor, "image-series", "42", "Series")
	var childRevision string
	if err := store.pool.QueryRow(ctx, `SELECT revision::text FROM item_metadata_state WHERE item_id='image-episode'`).Scan(&childRevision); err != nil {
		t.Fatal(err)
	}
	client := &onlineImageTestProvider{data: imageStoreTestPNG(t)}
	client.onList = func(selected providers.Selection) ([]providers.RemoteImage, error) {
		if selected.Type != "Episode" {
			return nil, nil
		}
		if selected.ID != "42:1:2" {
			t.Fatalf("episode used the wrong series identity: %+v", selected)
		}
		return []providers.RemoteImage{{Selection: selected, ImageID: "/episode.png", ImageType: "Thumb"}}, nil
	}
	client.onDownload = func(context.Context, providers.RemoteImage) error {
		onlineImageTestMatch(t, ctx, store, actor, "image-series", "99", "Series")
		return nil
	}
	if err := store.FillMissingOnlineImages(ctx, client, "provider-target-library", onlineImageTestFence(ctx), nil); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := store.pool.QueryRow(ctx, `SELECT revision::text FROM item_metadata_state WHERE item_id='image-episode'`).Scan(&after); err != nil || after != childRevision {
		t.Fatalf("fixture did not isolate the parent-only rematch: %q, %q, %v", childRevision, after, err)
	}
	if client.downloadCalls != 1 || onlineImageTestCacheCount(t, ctx, store, "image-episode") != 0 {
		t.Fatal("an episode retained artwork from its former parent-series match")
	}
}
