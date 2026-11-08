package library

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
)

func TestReplaceProviderImageArtworkPreservesOtherSlotsAndLegacySelection(t *testing.T) {
	ctx, pool, store, _, _ := catalogBatchTestStore(t)
	providerMetadataTargetFixture(t, ctx, pool)
	actor := metadataEditTestActor(t, ctx, pool, "provider-artwork-editor")
	target := ArtworkTarget{ItemID: "provider-target-item"}
	before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	data := imageStoreTestPNG(t)
	primary, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, before.Revision, "Primary", 0, data)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes := append(bytes.Clone(data), '1')
	first, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, primary.Revision, "Backdrop", 0, firstBytes)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes := append(bytes.Clone(data), '2')
	second, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, first.Revision, "Backdrop", 1, secondBytes)
	if err != nil {
		t.Fatal(err)
	}
	selected := providers.RemoteImage{Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, ImageID: "provider-backdrop", ImageType: "Backdrop"}
	replacement := append(bytes.Clone(data), '3')
	result, updated, err := store.ReplaceProviderImageArtwork(ctx, actor, target.ItemID, "1", second.Revision, selected, 0, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision == second.Revision || len(updated.Items) != len(second.Items) || result.Provider != "tmdb" || result.ProviderID != "42" || result.ImageID != selected.ImageID || result.Tag != managedArtworkImage(t, updated, "Backdrop", 0).Tag {
		t.Fatalf("replacement did not return its committed image: result=%+v collection=%+v", result, updated)
	}
	for _, slot := range []struct {
		imageType string
		index     int
	}{
		{"Primary", 0}, {"Backdrop", 1},
	} {
		if managedArtworkImage(t, updated, slot.imageType, slot.index).Tag != managedArtworkImage(t, second, slot.imageType, slot.index).Tag {
			t.Fatalf("replacement changed another slot: %s/%d", slot.imageType, slot.index)
		}
	}
	reader, image, err := store.OpenArtwork(ctx, actor, identity.AdministratorNative, target, "Backdrop", 0)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(reader)
	if err := errors.Join(readErr, reader.Close()); err != nil || !bytes.Equal(content, replacement) || image.Source != "managed" {
		t.Fatalf("replacement was not immediately effective: image=%+v error=%v", image, err)
	}
	var providerRows int
	var metadataRevision string
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM item_provider_images WHERE item_id=$1`, target.ItemID).Scan(&providerRows); err != nil || providerRows != 0 {
		t.Fatalf("managed selection changed the underlying provider cache: rows=%d error=%v", providerRows, err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision::text FROM item_metadata_state WHERE item_id=$1`, target.ItemID).Scan(&metadataRevision); err != nil || metadataRevision != "1" {
		t.Fatalf("image replacement changed metadata: revision=%s error=%v", metadataRevision, err)
	}
	selected.ImageID = "legacy-backdrop"
	legacy, err := store.SelectProviderImage(ctx, actor, target.ItemID, metadataRevision, selected, 0, firstBytes)
	if err != nil || legacy.Tag == result.Tag {
		t.Fatalf("legacy selection stopped writing its provider cache: result=%+v error=%v", legacy, err)
	}
	visible, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil || !reflect.DeepEqual(visible, updated) {
		t.Fatalf("legacy selection changed a managed override: collection=%+v error=%v", visible, err)
	}
}

func TestReplaceProviderImageArtworkChecksBothRevisionsAndItemType(t *testing.T) {
	ctx, pool, store, _, _ := catalogBatchTestStore(t)
	providerMetadataTargetFixture(t, ctx, pool)
	actor := metadataEditTestActor(t, ctx, pool, "provider-artwork-cas-editor")
	target := ArtworkTarget{ItemID: "provider-target-item"}
	before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	selected := providers.RemoteImage{Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, ImageID: "provider-cover", ImageType: "Primary"}
	data := imageStoreTestPNG(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, _, err := store.ReplaceProviderImageArtwork(ctx, actor, target.ItemID, "1", before.Revision, selected, 0, data)
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	committed, conflicts := 0, 0
	for err := range results {
		if err == nil {
			committed++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent image selection failed unexpectedly: %v", err)
		}
	}
	if committed != 1 || conflicts != 1 {
		t.Fatalf("concurrent selection committed=%d conflicts=%d", committed, conflicts)
	}
	current, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE item_metadata_state SET revision=revision+1 WHERE item_id=$1`, target.ItemID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReplaceProviderImageArtwork(ctx, actor, target.ItemID, "1", current.Revision, selected, 0, append(bytes.Clone(data), '4')); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("current artwork bypassed stale metadata CAS: %v", err)
	}
	wrongType := selected
	wrongType.Type = "Episode"
	if _, _, err := store.ReplaceProviderImageArtwork(ctx, actor, target.ItemID, "2", current.Revision, wrongType, 0, data); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("selection bypassed its target type: %v", err)
	}
	after, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil || !reflect.DeepEqual(current, after) {
		t.Fatalf("rejected selections changed artwork: collection=%+v error=%v", after, err)
	}
}

func TestReplaceProviderImageArtworkRollsBackFinalAuthorityDenial(t *testing.T) {
	ctx, pool, store, _, _ := catalogBatchTestStore(t)
	providerMetadataTargetFixture(t, ctx, pool)
	actor := metadataEditTestActor(t, ctx, pool, "provider-artwork-expiring-editor")
	target := ArtworkTarget{ItemID: "provider-target-item"}
	before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE SEQUENCE provider_artwork_expiry_hits;
		CREATE FUNCTION expire_provider_artwork_actor() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM nextval('provider_artwork_expiry_hits');
			UPDATE sessions SET created_at=clock_timestamp()-interval '2 days',expires_at=clock_timestamp()-interval '1 day'
			WHERE user_id='provider-artwork-expiring-editor';
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER expire_provider_artwork_actor BEFORE INSERT ON artwork_images
		FOR EACH ROW EXECUTE FUNCTION expire_provider_artwork_actor()`); err != nil {
		t.Fatal(err)
	}
	selected := providers.RemoteImage{Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, ImageID: "provider-cover", ImageType: "Primary"}
	if _, _, err := store.ReplaceProviderImageArtwork(ctx, actor, target.ItemID, "1", before.Revision, selected, 0, imageStoreTestPNG(t)); !errors.Is(err, identity.ErrUnauthorized) && !errors.Is(err, ErrForbidden) {
		t.Fatalf("selection committed after final authority denial: %v", err)
	}
	var reached, active bool
	if err := pool.QueryRow(ctx, `SELECT is_called FROM provider_artwork_expiry_hits`).Scan(&reached); err != nil || !reached {
		t.Fatalf("the final-authority fault was not reached: reached=%t error=%v", reached, err)
	}
	if err := pool.QueryRow(ctx, `SELECT expires_at>clock_timestamp() FROM sessions WHERE id=$1`, actor.SessionID).Scan(&active); err != nil || !active {
		t.Fatalf("denied publication retained its expiry mutation: active=%t error=%v", active, err)
	}
	after, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("denied publication retained managed bytes: collection=%+v error=%v", after, err)
	}
}
