//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/color"
	"reflect"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
)

type genreCollageQueryCounter struct {
	pgx.Tx
	queries int
}

func (counter *genreCollageQueryCounter) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	counter.queries++
	return counter.Tx.Query(ctx, sql, args...)
}

func (counter *genreCollageQueryCounter) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	counter.queries++
	return counter.Tx.QueryRow(ctx, sql, args...)
}

func TestGenreCollageBatchMatchesSingleManifest(t *testing.T) {
	ctx, pool, store, allowed, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	visible := imageStoreTestRoot(t, ctx, pool, store, allowed, "genre-batch-visible")
	hidden := imageStoreTestRoot(t, ctx, pool, store, allowed, "genre-batch-hidden")
	colors := []color.RGBA{
		{R: 220, A: 255}, {R: 220, A: 255}, {G: 210, A: 255}, {B: 200, A: 255},
		{R: 190, G: 180, A: 255}, {G: 170, B: 160, A: 255}, {R: 150, B: 140, A: 255},
	}
	memberIDs := []string{"a-first", "b-duplicate", "c-provider", "d-managed", "e-tombstone", "f-fourth", "g-fifth"}
	for index, id := range memberIDs {
		imageStoreTestInsert(t, ctx, pool, visible, id, "Primary", 0, id+".png", collageTestPNG(t, colors[index]))
	}
	imageStoreTestInsert(t, ctx, pool, visible, "empty-member", "Thumb", 0, "empty.png", collageTestPNG(t, color.RGBA{R: 90, A: 255}))
	imageStoreTestInsert(t, ctx, pool, hidden, "hidden-member", "Primary", 0, "hidden.png", collageTestPNG(t, color.RGBA{B: 80, A: 255}))
	provider := collageTestPNG(t, color.RGBA{R: 120, G: 110, B: 100, A: 255})
	providerDigest := sha256.Sum256(provider)
	if _, err := pool.Exec(ctx, `INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
		VALUES('c-provider','Primary',0,'tmdb','1','cover',$1,'image/png',2,2,$2)`, provider, hex.EncodeToString(providerDigest[:])); err != nil {
		t.Fatal(err)
	}
	managed := collageTestPNG(t, color.RGBA{R: 70, G: 60, B: 50, A: 255})
	managedDigest := sha256.Sum256(managed)
	if _, err := pool.Exec(ctx, `WITH state AS (
		INSERT INTO artwork_state(item_id,managed_types) VALUES('d-managed',ARRAY['Primary']) RETURNING id
	) INSERT INTO artwork_images(state_id,image_type,image_index,content,mime_type,width,height,source_hash)
		SELECT id,'Primary',0,$1,'image/png',2,2,$2 FROM state`, managed, hex.EncodeToString(managedDigest[:])); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO artwork_state(item_id,managed_types) VALUES('e-tombstone',ARRAY['Primary'])`); err != nil {
		t.Fatal(err)
	}
	shared := collageTestGenre(t, ctx, pool, "Batch shared", append(memberIDs, "hidden-member")...)
	overlap := collageTestGenre(t, ctx, pool, "Batch overlap", "c-provider", "e-tombstone")
	empty := collageTestGenre(t, ctx, pool, "Batch empty", "empty-member")
	suppressed := collageTestGenre(t, ctx, pool, "Batch suppressed", "a-first")
	hiddenOnly := collageTestGenre(t, ctx, pool, "Batch hidden", "hidden-member")
	orphan := collageTestGenre(t, ctx, pool, "Batch orphan")
	collectionOnly := collageTestGenre(t, ctx, pool, "Batch collection", visible.libraryID)
	otherKind := collageTestGenre(t, ctx, pool, "Batch tag", "a-first")
	if _, err := pool.Exec(ctx, `UPDATE catalog_entities SET kind='Tag' WHERE id=$1`, otherKind); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO artwork_state(entity_id,managed_types) VALUES($1,ARRAY['Primary']),($2,ARRAY['Backdrop'])`, suppressed, empty); err != nil {
		t.Fatal(err)
	}
	// Repeated associations must not displace another distinct source.
	if _, err := pool.Exec(ctx, `INSERT INTO item_entities(item_id,entity_id,position,display_name)
		VALUES('a-first',$1,2,'Batch shared')`, shared); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationUser(t, ctx, pool, "genre-batch-reader", false, false, []string{visible.libraryID})
	subject := Subject{UserID: "genre-batch-reader"}
	tx, access, err := store.beginSubjectRead(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	ids := []int64{shared, overlap, empty, suppressed, hiddenOnly, orphan, collectionOnly, otherKind, shared}
	counter := &genreCollageQueryCounter{Tx: tx}
	manifests, err := readGenreCollageManifests(ctx, counter, access, ids)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries != 1 {
		t.Fatalf("genre batch used %d queries, want one", counter.queries)
	}
	if len(manifests) != 3 {
		t.Fatalf("unexpected visible generated genres: %+v", manifests)
	}
	for _, id := range ids {
		manifest, err := readCollageManifest(ctx, tx, access, ArtworkTarget{EntityID: id})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(manifests[id], manifest) {
			t.Fatalf("genre %d batched manifest differs from singleton: got %+v, want %+v", id, manifests[id], manifest)
		}
	}
	var selectedIDs, selectedSources []string
	for _, member := range manifests[shared].Members {
		selectedIDs = append(selectedIDs, member.ItemID)
		selectedSources = append(selectedSources, member.Source)
	}
	if !reflect.DeepEqual(selectedIDs, []string{"a-first", "c-provider", "d-managed", "f-fourth"}) ||
		!reflect.DeepEqual(selectedSources, []string{"sidecar", "provider", "managed", "sidecar"}) {
		t.Fatalf("batch changed source order, precedence, tombstones or limit: %v, %v", selectedIDs, selectedSources)
	}
	if manifests[empty].Members == nil || len(manifests[empty].Members) != 0 || manifests[empty].Tag == "" || !manifests[empty].Changed.IsZero() {
		t.Fatalf("empty genre lost its placeholder identity: %+v", manifests[empty])
	}
	if _, err := readGenreCollageManifests(ctx, counter, access, nil); err != nil || counter.queries != 1 {
		t.Fatalf("empty genre batch queried storage: %d, %v", counter.queries, err)
	}
	listing := make(map[string][]Image)
	var listingIDs []string
	for _, id := range ids {
		listingIDs = append(listingIDs, strconv.FormatInt(id, 10))
	}
	counter.queries = 0
	if err := mergeCollageImageListing(ctx, counter, access, listingIDs, listing); err != nil {
		t.Fatal(err)
	}
	if counter.queries != 2 {
		t.Fatalf("genre listing used %d queries, want target selection plus one batch", counter.queries)
	}
	for id, manifest := range manifests {
		images := listing[strconv.FormatInt(id, 10)]
		if len(images) != 1 || images[0] != manifest.image() {
			t.Fatalf("genre %d image listing differs from its manifest: %+v", id, images)
		}
	}
	if len(listing) != len(manifests) {
		t.Fatalf("genre listing exposed an unavailable generated target: %+v", listing)
	}
}
