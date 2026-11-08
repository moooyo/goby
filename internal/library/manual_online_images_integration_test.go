package library

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
)

func manualOnlineImageIntegrationFixture(t *testing.T) (context.Context, *Store, identity.Principal) {
	t.Helper()
	ctx, store, actor := onlineImageIntegrationFixture(t)
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		_, err := tx.Exec(`UPDATE libraries SET options=jsonb_set(options,'{EnableOnlineImages}','false'::jsonb),revision=revision+1
			WHERE id='provider-target-library'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ctx, store, actor
}

func manualUnmatchedOnlineImageIntegrationFixture(t *testing.T) (context.Context, *Store, identity.Principal) {
	t.Helper()
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	providerMetadataTargetFixture(t, ctx, pool)
	actor := metadataEditTestActor(t, ctx, pool, "manual-unmatched-image-editor")
	// This item has never accepted a provider match in any metadata source.
	return ctx, store, actor
}

func TestManualOnlineImagesDownloadOnlyRequestedTypeWithoutOptingIn(t *testing.T) {
	ctx, store, actor := manualOnlineImageIntegrationFixture(t)
	beforeItem, beforeMetadata, beforeEvents := onlineImageTestSourceState(t, ctx, store)
	var beforeOptions string
	if err := store.pool.QueryRow(ctx, `SELECT options::text FROM libraries WHERE id='provider-target-library'`).Scan(&beforeOptions); err != nil {
		t.Fatal(err)
	}
	client := &onlineImageTestProvider{data: imageStoreTestPNG(t)}
	result, err := store.DownloadMissingOnlineImage(ctx, actor, client, "provider-target-library", "provider-target-item", "Backdrop")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "downloaded" || result.ItemID != "provider-target-item" || result.LibraryID != "provider-target-library" || result.ImageType != "Backdrop" ||
		client.listCalls != 1 || client.downloadCalls != 1 || onlineImageTestCacheCount(t, ctx, store, result.ItemID) != 1 {
		t.Fatalf("manual request lost its exact scope or committed count: result=%+v lists=%d downloads=%d", result, client.listCalls, client.downloadCalls)
	}
	current, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, ArtworkTarget{ItemID: result.ItemID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Artwork, current) || len(current.Items) != 1 || current.Items[0].ImageType != "Backdrop" {
		t.Fatalf("manual download did not return the committed artwork: %+v", result.Artwork)
	}
	item, metadata, events := onlineImageTestSourceState(t, ctx, store)
	var options string
	if err := store.pool.QueryRow(ctx, `SELECT options::text FROM libraries WHERE id='provider-target-library'`).Scan(&options); err != nil {
		t.Fatal(err)
	}
	if item != beforeItem || metadata != beforeMetadata || events != beforeEvents || options != beforeOptions {
		t.Fatal("explicit image download changed metadata, source state, task events, or the automatic policy")
	}
	result, err = store.DownloadMissingOnlineImage(ctx, actor, client, result.LibraryID, result.ItemID, "Backdrop")
	if err != nil || result.Status != "already_present" || client.listCalls != 1 || client.downloadCalls != 1 {
		t.Fatalf("repeat missing-only request replaced existing artwork: result=%+v error=%v", result, err)
	}
}

func TestManualOnlineImagesPreserveArtworkMasksAndRequireKnownMatches(t *testing.T) {
	for _, status := range []string{"managed", "unmatched", "unsupported", "no_candidate"} {
		t.Run(status, func(t *testing.T) {
			fixture := manualOnlineImageIntegrationFixture
			if status == "unmatched" {
				fixture = manualUnmatchedOnlineImageIntegrationFixture
			}
			ctx, store, actor := fixture(t)
			itemID := "provider-target-item"
			client := &onlineImageTestProvider{data: imageStoreTestPNG(t)}
			switch status {
			case "managed":
				target := ArtworkTarget{ItemID: itemID}
				before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
				if err != nil {
					t.Fatal(err)
				}
				uploaded, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, before.Revision, "Primary", 0, client.data)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.DeleteArtwork(ctx, actor, identity.AdministratorNative, target, uploaded.Revision, "Primary", 0); err != nil {
					t.Fatal(err)
				}
			case "unsupported":
				itemID = "provider-target-parent"
			case "no_candidate":
				client.onList = func(selected providers.Selection) ([]providers.RemoteImage, error) {
					other := selected
					other.ID = "99"
					return []providers.RemoteImage{
						{Selection: selected, ImageID: "/backdrop.png", ImageType: "Backdrop"},
						{Selection: other, ImageID: "/other.png", ImageType: "Primary"},
					}, nil
				}
			}
			if status == "unmatched" {
				query, err := store.OnlineMetadataQuery(ctx, actor, itemID)
				if err != nil || providers.MetadataID("tmdb", query.Type, query.ProviderIDs) != "" {
					t.Fatalf("unmatched fixture retained a resolved TMDB identity: query=%+v error=%v", query, err)
				}
				var sources int
				if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM item_provider_sources WHERE item_id=$1`, itemID).Scan(&sources); err != nil || sources != 0 {
					t.Fatalf("unmatched fixture retained an accepted provider source: sources=%d error=%v", sources, err)
				}
			}
			before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, ArtworkTarget{ItemID: itemID})
			if err != nil {
				t.Fatal(err)
			}
			result, err := store.DownloadMissingOnlineImage(ctx, actor, client, "provider-target-library", itemID, "Primary")
			if err != nil || result.Status != status || !reflect.DeepEqual(before, result.Artwork) || client.downloadCalls != 0 || onlineImageTestCacheCount(t, ctx, store, itemID) != 0 {
				t.Fatalf("no-op request changed artwork or reported an incorrect reason: result=%+v error=%v", result, err)
			}
			wantLists := 0
			if status == "no_candidate" {
				wantLists = 1
			}
			if client.listCalls != wantLists {
				t.Fatalf("no-op request made unexpected provider calls: %d", client.listCalls)
			}
		})
	}
}

func TestManualOnlineImagesRejectConcurrentMetadataArtworkLibraryAndAuthorityChanges(t *testing.T) {
	for _, change := range []string{"metadata", "artwork", "library", "authority"} {
		t.Run(change, func(t *testing.T) {
			ctx, store, actor := manualOnlineImageIntegrationFixture(t)
			client := &onlineImageTestProvider{data: imageStoreTestPNG(t)}
			client.onDownload = func(ctx context.Context, selected providers.RemoteImage) error {
				switch change {
				case "metadata":
					onlineImageTestMatch(t, ctx, store, actor, "provider-target-item", "99", "Movie")
				case "artwork":
					target := ArtworkTarget{ItemID: "provider-target-item"}
					current, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
					if err != nil {
						return err
					}
					_, err = store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, current.Revision, "Logo", 0, client.data)
					return err
				case "library":
					return store.WithOwnedTx(ctx, func(tx OwnedTx) error {
						_, err := tx.Exec(`UPDATE libraries SET name='Changed library',revision=revision+1 WHERE id='provider-target-library'`)
						return err
					})
				case "authority":
					_, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID)
					return err
				}
				return nil
			}
			_, err := store.DownloadMissingOnlineImage(ctx, actor, client, "provider-target-library", "provider-target-item", "Primary")
			want := ErrRevisionConflict
			if change == "authority" {
				want = identity.ErrUnauthorized
			}
			if !errors.Is(err, want) || client.downloadCalls != 1 || onlineImageTestCacheCount(t, ctx, store, "provider-target-item") != 0 {
				t.Fatalf("obsolete manual download was not rejected: change=%s error=%v", change, err)
			}
		})
	}
}

func TestManualOnlineImagesRecheckScopeAndAuthorityBeforeEmptySuccess(t *testing.T) {
	for _, change := range []string{"scope", "authority"} {
		t.Run(change, func(t *testing.T) {
			ctx, store, actor := manualOnlineImageIntegrationFixture(t)
			client := &onlineImageTestProvider{onList: func(providers.Selection) ([]providers.RemoteImage, error) {
				if change == "authority" {
					_, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID)
					return nil, err
				}
				err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
					if _, err := tx.Exec(`INSERT INTO libraries(id,name,collection_type) VALUES('manual-image-other-library','Other library','movies')`); err != nil {
						return err
					}
					_, err := tx.Exec(`UPDATE items SET library_id='manual-image-other-library',root_id=NULL,parent_id=NULL WHERE id='provider-target-item'`)
					return err
				})
				return nil, err
			}}
			_, err := store.DownloadMissingOnlineImage(ctx, actor, client, "provider-target-library", "provider-target-item", "Primary")
			want := ErrNotFound
			if change == "authority" {
				want = identity.ErrUnauthorized
			}
			if !errors.Is(err, want) || client.downloadCalls != 0 {
				t.Fatalf("empty response ignored a changed scope or authority: change=%s error=%v", change, err)
			}
		})
	}
}

func TestManualOnlineImagesRejectObsoleteEmptyCandidateResults(t *testing.T) {
	for _, change := range []string{"metadata", "artwork"} {
		t.Run(change, func(t *testing.T) {
			ctx, store, actor := manualOnlineImageIntegrationFixture(t)
			data := imageStoreTestPNG(t)
			client := &onlineImageTestProvider{onList: func(providers.Selection) ([]providers.RemoteImage, error) {
				if change == "metadata" {
					onlineImageTestMatch(t, ctx, store, actor, "provider-target-item", "99", "Movie")
					return nil, nil
				}
				target := ArtworkTarget{ItemID: "provider-target-item"}
				current, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
				if err != nil {
					return nil, err
				}
				_, err = store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, current.Revision, "Primary", 0, data)
				return nil, err
			}}
			_, err := store.DownloadMissingOnlineImage(ctx, actor, client, "provider-target-library", "provider-target-item", "Primary")
			if !errors.Is(err, ErrRevisionConflict) || client.listCalls != 1 || client.downloadCalls != 0 || onlineImageTestCacheCount(t, ctx, store, "provider-target-item") != 0 {
				t.Fatalf("obsolete empty candidate result became a successful no-op: change=%s error=%v", change, err)
			}
		})
	}
}

func TestManualOnlineImagesKeepProviderFailuresVisible(t *testing.T) {
	ctx, store, actor := manualOnlineImageIntegrationFixture(t)
	client := &onlineImageTestProvider{onList: func(providers.Selection) ([]providers.RemoteImage, error) {
		return nil, providers.ErrQuota
	}}
	_, err := store.DownloadMissingOnlineImage(ctx, actor, client, "provider-target-library", "provider-target-item", "Primary")
	if !errors.Is(err, providers.ErrQuota) || client.downloadCalls != 0 || onlineImageTestCacheCount(t, ctx, store, "provider-target-item") != 0 {
		t.Fatalf("provider quota became a successful no-op: %v", err)
	}
}

func TestManualOnlineImagesRejectNonOrdinaryTargetsBeforeProviderWork(t *testing.T) {
	for _, target := range []string{"rootless", "wrong_root", "reserved", "container"} {
		t.Run(target, func(t *testing.T) {
			ctx, store, actor := manualOnlineImageIntegrationFixture(t)
			if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
				switch target {
				case "rootless":
					_, err := tx.Exec(`UPDATE items SET root_id=NULL WHERE id='provider-target-item'`)
					return err
				case "wrong_root":
					if _, err := tx.Exec(`INSERT INTO libraries(id,name,collection_type) VALUES('manual-image-other-library','Other library','movies')`); err != nil {
						return err
					}
					if _, err := tx.Exec(`INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
						VALUES('manual-image-other-root','manual-image-other-library','/other-provider-target','/other-provider-target','')`); err != nil {
						return err
					}
					_, err := tx.Exec(`UPDATE items SET root_id='manual-image-other-root' WHERE id='provider-target-item'`)
					return err
				case "reserved":
					_, err := tx.Exec(`INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('provider-target-root','Film',true)`)
					return err
				case "container":
					_, err := tx.Exec(`UPDATE items SET type='CollectionFolder',is_folder=true WHERE id='provider-target-item'`)
					return err
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			client := &onlineImageTestProvider{data: imageStoreTestPNG(t)}
			_, err := store.DownloadMissingOnlineImage(ctx, actor, client, "provider-target-library", "provider-target-item", "Primary")
			if !errors.Is(err, ErrNotFound) || client.listCalls != 0 || client.downloadCalls != 0 {
				t.Fatalf("non-ordinary target reached provider work: target=%s error=%v", target, err)
			}
		})
	}
}
