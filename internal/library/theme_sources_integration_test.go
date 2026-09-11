//go:build linux

package library

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestThemeVisibilityMediaAndSubtitleDeliveryRequireAnActiveOwnedResource(t *testing.T) {
	fixture, track, _ := subtitleTestCatalog(t)
	ctx, pool, store := fixture.ctx, fixture.pool, fixture.store
	libraryIntegrationUser(t, ctx, pool, "theme-source-reader", false, false, []string{fixture.library.ID})
	libraryIntegrationUser(t, ctx, pool, "theme-source-hidden", false, false, nil)
	reset := func() {
		t.Helper()
		if _, err := pool.Exec(ctx, "UPDATE items SET type='Video',parent_id=library_id WHERE id=$1", fixture.item.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
			SELECT root_id,relative_path,false FROM items WHERE id=$1
			ON CONFLICT(root_id,relative_path) DO NOTHING`, fixture.item.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active)
			SELECT id,library_id,'video',true FROM items WHERE id=$1
			ON CONFLICT(resource_item_id) DO UPDATE SET owner_item_id=EXCLUDED.owner_item_id,kind='video',active=true`, fixture.item.ID); err != nil {
			t.Fatal(err)
		}
	}
	assertDelivery := func(userID string, want error) {
		t.Helper()
		file, source, err := store.OpenMedia(ctx, userID, fixture.item.ID, media.SourceID(fixture.item.ID))
		if want == nil {
			if err != nil || file == nil {
				t.Fatalf("open authorized theme source: %v", err)
			}
			body, readErr := io.ReadAll(file)
			_ = file.Close()
			if readErr != nil || string(body) != fixture.contents || source.Item.ID != fixture.item.ID || source.Item.Type != "Video" {
				t.Fatalf("theme delivery lost its exact indexed source: %v", readErr)
			}
		} else {
			if file != nil {
				_ = file.Close()
			}
			if !errors.Is(err, want) || file != nil || !reflect.DeepEqual(source, MediaFile{}) {
				t.Fatalf("hidden theme returned a source or unexpected error: %v", err)
			}
		}
		content, err := store.ReadSubtitle(ctx, userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index)
		if want == nil {
			if err != nil || string(content.Data) != subtitleTestSRT || !reflect.DeepEqual(content.Info, track) {
				t.Fatalf("active theme lost its indexed subtitle: %v", err)
			}
		} else if !errors.Is(err, want) || len(content.Data) != 0 {
			t.Fatalf("hidden theme returned subtitle contents or unexpected error: %v", err)
		}
	}
	reset()
	assertDelivery("theme-source-reader", nil)
	assertDelivery("theme-source-hidden", ErrNotFound)
	for _, test := range []struct {
		name string
		sql  string
	}{
		{"inactive", "UPDATE item_theme_resources SET active=false WHERE resource_item_id=$1"},
		{"missing_association", "DELETE FROM item_theme_resources WHERE resource_item_id=$1"},
		{"missing_reservation", `DELETE FROM theme_reserved_paths marker USING items item
			WHERE item.id=$1 AND marker.root_id=item.root_id AND marker.relative_path=item.relative_path`},
		{"wrong_parent", "UPDATE items SET parent_id=NULL WHERE id=$1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reset()
			if _, err := pool.Exec(ctx, test.sql, fixture.item.ID); err != nil {
				t.Fatal(err)
			}
			assertDelivery("theme-source-reader", ErrNotFound)
			ordinary, err := store.QueryItems(ctx, Query{UserID: "theme-source-reader", Ids: []string{fixture.item.ID}})
			if err != nil || ordinary.TotalRecordCount != 0 || len(ordinary.Items) != 0 {
				t.Fatalf("an invalid theme became ordinary media: %+v, %v", ordinary, err)
			}
		})
	}
	reset()
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":true,"EnableMediaPlayback":false}'::jsonb
		WHERE id='theme-source-reader'`); err != nil {
		t.Fatal(err)
	}
	assertDelivery("theme-source-reader", ErrForbidden)
}

func TestThemeVisibilityPublicImagesKeepPublicPolicyButRejectInactiveResources(t *testing.T) {
	ctx, pool, store, allowed, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := imageStoreTestRoot(t, ctx, pool, store, allowed, "theme-images")
	data := imageStoreTestPNG(t)
	image := imageStoreTestInsert(t, ctx, pool, root, "theme-image-resource", "Primary", 0, "theme.png", data)
	libraryIntegrationUser(t, ctx, pool, "theme-image-hidden", false, false, nil)
	if _, err := pool.Exec(ctx, `UPDATE items SET type='Video' WHERE id='theme-image-resource';
		INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
		SELECT root_id,relative_path,false FROM items WHERE id='theme-image-resource';
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active)
		SELECT id,library_id,'video',true FROM items WHERE id='theme-image-resource'`); err != nil {
		t.Fatal(err)
	}
	images, err := store.ListImages(ctx, userID, "theme-image-resource")
	if err != nil || !reflect.DeepEqual(images, []Image{image}) {
		t.Fatalf("active theme image metadata was unavailable: %+v, %v", images, err)
	}
	if _, err := store.ListImages(ctx, "theme-image-hidden", "theme-image-resource"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("theme image metadata bypassed current library permissions: %v", err)
	}
	// Public bytes preserve the existing no-user policy while requiring a valid
	// active resource. This is deliberately distinct from image metadata reads.
	file, public, err := store.OpenPublicImage(ctx, "theme-image-resource", "Primary", 0)
	if err != nil || file == nil {
		t.Fatalf("the active theme's public image was made private: %v", err)
	}
	body, readErr := io.ReadAll(file)
	_ = file.Close()
	if readErr != nil || !bytes.Equal(body, data) || !reflect.DeepEqual(public, image) {
		t.Fatalf("public theme image did not preserve exact indexed bytes: %v", readErr)
	}
	if _, err := pool.Exec(ctx, "UPDATE item_theme_resources SET active=false WHERE resource_item_id='theme-image-resource'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListImages(ctx, userID, "theme-image-resource"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive theme image metadata remained visible: %v", err)
	}
	batch, err := store.ImagesForItems(ctx, userID, []string{"theme-image-resource"})
	if err != nil || len(batch) != 0 {
		t.Fatalf("inactive theme escaped the image batch filter: %+v, %v", batch, err)
	}
	for _, retained := range []bool{true, false} {
		if !retained {
			if _, err := pool.Exec(ctx, "DELETE FROM item_theme_resources WHERE resource_item_id='theme-image-resource'"); err != nil {
				t.Fatal(err)
			}
		}
		file, _, err := store.OpenPublicImage(ctx, "theme-image-resource", "Primary", 0)
		if file != nil {
			_ = file.Close()
		}
		if !errors.Is(err, ErrNotFound) || file != nil {
			t.Fatalf("a retained reservation exposed inactive public image bytes: %v", err)
		}
	}
}
