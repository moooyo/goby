package library

import (
	"bytes"
	"errors"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func managedArtworkImage(t *testing.T, collection ArtworkCollection, kind string, index int) Image {
	t.Helper()
	for _, image := range collection.Items {
		if image.ImageType == kind && image.ImageIndex == index {
			return image
		}
	}
	t.Fatalf("artwork collection omitted %s/%d: %+v", kind, index, collection)
	return Image{}
}

func TestStoreManagedArtworkMasksAutomaticSourcesReordersAndPersists(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	actor := metadataEditTestActor(t, ctx, pool, "artwork-editor")
	path := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:managed-artwork")
	posterPath := filepath.Join(filepath.Dir(path), "Film-poster.png")
	poster := imageScanTestWrite(t, posterPath, color.NRGBA{R: 240, A: 255})
	collection := libraryIntegrationCreate(t, ctx, store, "Artwork lifecycle", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	target := ArtworkTarget{ItemID: item.ID}
	before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	baseline := managedArtworkImage(t, before, "Primary", 0)
	deleted, err := store.DeleteArtwork(ctx, actor, identity.AdministratorNative, target, before.Revision, "Primary", 0)
	if err != nil || len(deleted.Items) != 0 {
		t.Fatalf("delete automatic image: %+v, %v", deleted, err)
	}
	if _, _, err := store.OpenImageContentFor(ctx, Subject{UserID: userID}, item.ID, "Primary", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted image fell through to a local source: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if images, err := store.ListImagesFor(ctx, Subject{UserID: userID}, item.ID); err != nil || len(images) != 0 {
		t.Fatalf("rescan revived a deletion mask: %+v, %v", images, err)
	}
	unchanged, err := os.ReadFile(posterPath)
	if err != nil || !bytes.Equal(unchanged, poster) {
		t.Fatalf("managed deletion changed the media root: %v", err)
	}
	restored, err := store.ResetArtwork(ctx, actor, identity.AdministratorNative, target, deleted.Revision, "Primary")
	if err != nil || managedArtworkImage(t, restored, "Primary", 0).Tag != baseline.Tag {
		t.Fatalf("reset did not restore automatic artwork: %v", err)
	}
	first := imageStoreTestPNG(t)
	second := imageScanTestWrite(t, filepath.Join(root, "upload.png"), color.NRGBA{B: 190, A: 255})
	firstSet, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, restored.Revision, "Backdrop", 0, first)
	if err != nil {
		t.Fatal(err)
	}
	secondSet, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, firstSet.Revision, "Backdrop", 1, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReorderArtwork(ctx, actor, identity.AdministratorNative, target, secondSet.Revision, "Backdrop", []int{0, 0}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate permutation was accepted: %v", err)
	}
	reordered, err := store.ReorderArtwork(ctx, actor, identity.AdministratorNative, target, secondSet.Revision, "Backdrop", []int{1, 0})
	if err != nil {
		t.Fatal(err)
	}
	if managedArtworkImage(t, reordered, "Backdrop", 0).Tag != managedArtworkImage(t, secondSet, "Backdrop", 1).Tag ||
		managedArtworkImage(t, reordered, "Backdrop", 1).Tag != managedArtworkImage(t, secondSet, "Backdrop", 0).Tag {
		t.Fatal("reordering did not move actual bytes with their source hashes")
	}
	compacted, err := store.DeleteArtwork(ctx, actor, identity.AdministratorNative, target, reordered.Revision, "Backdrop", 0)
	if err != nil {
		t.Fatal(err)
	}
	if managedArtworkImage(t, compacted, "Backdrop", 0).Tag != managedArtworkImage(t, firstSet, "Backdrop", 0).Tag || len(compacted.Items) != 2 {
		t.Fatalf("deletion did not compact the remaining backdrop: %+v", compacted)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(pool, prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	entitiesStoreCleanup(t, reopened)
	persisted, err := reopened.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil || !reflect.DeepEqual(persisted, compacted) {
		t.Fatalf("restart lost managed images or revision: %+v, %v", persisted, err)
	}
	reader, source, err := reopened.OpenImageContentFor(ctx, Subject{UserID: userID}, item.ID, "Backdrop", 0)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil || !bytes.Equal(content, first) || source.Source != "managed" {
		t.Fatalf("managed bytes changed: %v", readErr)
	}
	if err := reopened.DeleteLibrary(ctx, collection.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM artwork_images)+(SELECT count(*) FROM artwork_state)").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleted item retained managed payload or owner state: count=%d, %v", remaining, err)
	}
}

func TestStoreManagedArtworkConcurrentRevisionAndFreshAdministrator(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "artwork-cas-editor")
	path := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:artwork-cas")
	collection := libraryIntegrationCreate(t, ctx, store, "Artwork CAS", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	target := ArtworkTarget{ItemID: item.ID}
	before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	data := imageStoreTestPNG(t)
	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, before.Revision, "Primary", 0, data)
			errorsSeen <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errorsSeen)
	committed, conflicts := 0, 0
	for err := range errorsSeen {
		if err == nil {
			committed++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent edit failed unexpectedly: %v", err)
		}
	}
	if committed != 1 || conflicts != 1 {
		t.Fatalf("concurrent CAS committed=%d conflicts=%d", committed, conflicts)
	}
	current, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE users SET is_administrator=false WHERE id=$1",
		"UPDATE users SET is_administrator=true,is_disabled=true WHERE id=$1",
	} {
		if _, err := pool.Exec(ctx, statement, actor.User.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, current.Revision, "Primary", 0, []byte("invalid image")); err == nil {
			t.Fatal("stale administrator principal was allowed to edit images")
		}
		if _, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target); err == nil {
			t.Fatal("stale administrator principal read native image management")
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled=false WHERE id=$1", actor.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=now() WHERE id=$1", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteArtwork(ctx, actor, identity.AdministratorNative, target, current.Revision, "Primary", 0); err == nil {
		t.Fatal("revoked administrator session deleted managed bytes")
	}
	var revision, images int
	if err := pool.QueryRow(ctx, "SELECT revision,(SELECT count(*) FROM artwork_images WHERE state_id=state.id) FROM artwork_state state WHERE item_id=$1", item.ID).Scan(&revision, &images); err != nil || revision != 1 || images != 1 {
		t.Fatalf("rejected edits changed durable artwork: revision=%d images=%d, %v", revision, images, err)
	}
}

func TestStoreEntityArtworkAndUserStateAreIndependentAndFollowVisibility(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "entity-artwork-editor")
	otherID := "other-entity-viewer"
	libraryIntegrationUser(t, ctx, pool, otherID, false, true, nil)
	path := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:entity-artwork")
	libraryIntegrationFile(t, root, "movies/Film.nfo", `<movie><genre>Independent Genre</genre><studio>Independent Studio</studio><actor><name>Independent Person</name></actor></movie>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Entity artwork", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	genre := entitiesNamed(t, entitiesList(t, ctx, store, "Genre", Query{UserID: userID, Limit: 20}), "Independent Genre")
	target := ArtworkTarget{EntityID: genre.ID}
	before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UploadArtwork(ctx, actor, identity.AdministratorNative, target, before.Revision, "Primary", 0, imageStoreTestPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	favorite, played, likes := true, true, false
	rating, count := 3.5, 4
	lastPlayed := time.Date(2026, 9, 19, 10, 15, 0, 0, time.UTC)
	data, err := store.UpdateEntityUserDataFor(ctx, Subject{UserID: userID}, genre.ID, UserDataPatch{
		IsFavorite: &favorite, Played: &played, Rating: &rating, Likes: &likes, PlayCount: &count, LastPlayedDate: &lastPlayed,
	})
	if err != nil || !data.IsFavorite || !data.Played || data.PlayCount != 4 || data.Rating == nil || *data.Rating != 3.5 || data.Likes == nil || *data.Likes {
		t.Fatalf("entity state did not persist independent fields: %+v, %v", data, err)
	}
	other, err := store.GetEntityUserDataFor(ctx, Subject{UserID: otherID}, genre.ID)
	if err != nil || other.IsFavorite || other.Rating != nil || other.Played {
		t.Fatalf("entity state leaked between users: %+v, %v", other, err)
	}
	var itemStates int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_item_data WHERE user_id=$1", userID).Scan(&itemStates); err != nil || itemStates != 0 {
		t.Fatalf("entity state wrote associated media state: count=%d, %v", itemStates, err)
	}
	result := entitiesList(t, ctx, store, "Genre", Query{UserID: userID, Limit: 20, IsFavorite: &favorite})
	if len(result.Items) != 1 || result.Items[0].ID != genre.ID || result.Items[0].UserData == nil || !result.Items[0].UserData.IsFavorite ||
		len(result.Items[0].Images) != 1 || result.Items[0].Images[0].Tag != managedArtworkImage(t, updated, "Primary", 0).Tag {
		t.Fatalf("entity list did not project its own favorite and images: %+v", result)
	}
	if otherFavorites := entitiesList(t, ctx, store, "Genre", Query{UserID: otherID, Limit: 20, IsFavorite: &favorite}); otherFavorites.TotalRecordCount != 0 {
		t.Fatalf("favorite filter borrowed another user's preferences: %+v", otherFavorites)
	}
	if _, err := store.SetFavoriteFor(ctx, Subject{UserID: otherID}, item.ID, true); err != nil {
		t.Fatal(err)
	}
	if mediaOnly := entitiesList(t, ctx, store, "Genre", Query{UserID: otherID, Limit: 20, IsFavorite: &favorite}); mediaOnly.TotalRecordCount != 0 {
		t.Fatalf("favoriting associated media incorrectly favorited its entity: %+v", mediaOnly)
	}
	notFavorite := false
	if mediaOnly := entitiesList(t, ctx, store, "Genre", Query{UserID: otherID, Limit: 20, IsFavorite: &notFavorite}); mediaOnly.TotalRecordCount != 1 {
		t.Fatalf("unfavorited entity disappeared because its source movie is favorite: %+v", mediaOnly)
	}
	position := int64(1)
	for _, patch := range []UserDataPatch{{PlaybackPositionTicks: &position}, {HideFromResume: &favorite}} {
		if _, err := store.UpdateEntityUserDataFor(ctx, Subject{UserID: userID}, genre.ID, patch); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("nonmedia entity accepted resume state: %v", err)
		}
	}
	cleared, err := store.UpdateEntityUserDataFor(ctx, Subject{UserID: userID}, genre.ID, UserDataPatch{ClearRating: true, ClearLikes: true, ClearLastPlayedDate: true})
	if err != nil || cleared.Rating != nil || cleared.Likes != nil || cleared.LastPlayedDate != nil || !cleared.IsFavorite {
		t.Fatalf("explicit null did not clear only nullable fields: %+v, %v", cleared, err)
	}
	unfavorite, liked := false, true
	if _, err := store.UpdateEntityUserDataFor(ctx, Subject{UserID: userID}, genre.ID, UserDataPatch{IsFavorite: &unfavorite, Likes: &liked}); err != nil {
		t.Fatal(err)
	}
	if result := entitiesList(t, ctx, store, "Genre", Query{UserID: userID, Limit: 20, IsFavoriteOrLikes: &liked}); result.TotalRecordCount != 1 {
		t.Fatalf("favorite-or-likes ignored independent entity likes: %+v", result)
	}
	if result := entitiesList(t, ctx, store, "Genre", Query{UserID: userID, Limit: 20, IsFavorite: &liked}); result.TotalRecordCount != 0 {
		t.Fatalf("favorite-only filter conflated independent likes: %+v", result)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetEntityUserDataFor(ctx, Subject{UserID: userID}, genre.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked entity state read remained visible: %v", err)
	}
	if _, err := store.UpdateEntityUserDataFor(ctx, Subject{UserID: userID}, genre.ID, UserDataPatch{IsFavorite: &favorite}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked entity state write remained visible: %v", err)
	}
	if _, _, err := store.OpenImageContentFor(ctx, Subject{UserID: userID}, strconv.FormatInt(genre.ID, 10), "Primary", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("numeric entity image bypassed current item ACL: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM items WHERE id=$1", item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target); !errors.Is(err, ErrNotFound) {
		t.Fatalf("orphan entity remained manageable: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM catalog_entities WHERE id=$1", genre.ID); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM artwork_state WHERE entity_id=$1)+(SELECT count(*) FROM entity_user_data WHERE entity_id=$1)+(SELECT count(*) FROM artwork_images)", genre.ID).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("deleted entity retained payload or user state: count=%d, %v", retained, err)
	}
}
