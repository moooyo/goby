//go:build linux

package library

import (
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestApplicationKeySourcesIgnoreTargetPlaybackPolicyAndRetainSnapshotChecks(t *testing.T) {
	fixture, track, _ := subtitleTestCatalog(t)
	ctx, store, pool := fixture.ctx, fixture.store, fixture.pool
	subject := seedCatalogApplicationKey(t, ctx, pool, "source-application-key", true)
	libraryIntegrationUser(t, ctx, pool, "source-key-target", false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = policy || '{"EnableMediaPlayback":false}'::jsonb
		WHERE id = 'source-key-target'`); err != nil {
		t.Fatal(err)
	}
	mediaSourceTestOpenError(t, ctx, store, "source-key-target", fixture.item.ID, "", ErrForbidden)
	if _, err := store.ReadSubtitle(ctx, "source-key-target", fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrForbidden) {
		t.Errorf("ordinary subtitle source ignored playback policy: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = 'source-key-target'"); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"", "source-key-target"} {
		subject.UserID = target
		file, source, err := store.OpenMediaFor(ctx, subject, fixture.item.ID, "")
		if err != nil || file == nil || source.Item.ID != fixture.item.ID || !source.Item.CanPlay {
			if file != nil {
				file.Close()
			}
			t.Fatalf("application original source for target %q = %+v, %v", target, source, err)
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil || string(data) != fixture.contents {
			t.Fatalf("application original source bytes = %q, %v", data, err)
		}
		content, err := store.ReadSubtitleFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index)
		if err != nil || string(content.Data) != subtitleTestSRT || content.Info.Index != track.Index {
			t.Fatalf("application subtitle for target %q = %+v, %v", target, content, err)
		}
	}
	if file, _, err := store.OpenMediaFor(ctx, subject, fixture.item.ID, "unrelated-source"); !errors.Is(err, ErrNotFound) || file != nil {
		if file != nil {
			file.Close()
		}
		t.Errorf("application source accepted an unrelated source selector: %v", err)
	}
	if _, err := store.ReadSubtitleFor(ctx, subject, fixture.item.ID, "unrelated-source", track.Index); !errors.Is(err, ErrNotFound) {
		t.Errorf("application subtitle accepted an unrelated source selector: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", subject.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if file, _, err := store.OpenMediaFor(ctx, subject, fixture.item.ID, ""); !errors.Is(err, ErrForbidden) || file != nil {
		if file != nil {
			file.Close()
		}
		t.Errorf("revoked application credential opened a source: %v", err)
	}
	if _, err := store.ReadSubtitleFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrForbidden) {
		t.Errorf("revoked application credential read cached subtitle metadata: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = NULL WHERE id = $1", subject.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET file_identity = 'stale-indexed-identity' WHERE id = $1", fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if file, _, err := store.OpenMediaFor(ctx, subject, fixture.item.ID, ""); !errors.Is(err, ErrUnavailable) || file != nil {
		if file != nil {
			file.Close()
		}
		t.Errorf("application authority bypassed original source snapshot checks: %v", err)
	}
	if _, err := store.ReadSubtitleFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrUnavailable) {
		t.Errorf("application authority bypassed subtitle primary snapshot checks: %v", err)
	}
}

func TestApplicationKeyImageMetadataUsesOptionalTargetCatalogScope(t *testing.T) {
	ctx, pool, store, allowed, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	visible := imageStoreTestRoot(t, ctx, pool, store, allowed, "key-visible")
	hidden := imageStoreTestRoot(t, ctx, pool, store, allowed, "key-hidden")
	data := imageStoreTestPNG(t)
	primary := imageStoreTestInsert(t, ctx, pool, visible, "key-visible-item", "Primary", 0, "primary.png", data)
	secret := imageStoreTestInsert(t, ctx, pool, hidden, "key-hidden-item", "Primary", 0, "secret.png", data)
	libraryIntegrationUser(t, ctx, pool, "key-image-target", false, false, []string{visible.libraryID})
	if _, err := store.ListImages(ctx, "key-image-target", "key-hidden-item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ordinary image metadata escaped library ACL: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = 'key-image-target'"); err != nil {
		t.Fatal(err)
	}
	subject := seedCatalogApplicationKey(t, ctx, pool, "image-application-key", true)
	for _, target := range []string{"", "key-image-target"} {
		subject.UserID = target
		images, err := store.ListImagesFor(ctx, subject, "key-visible-item")
		if err != nil || !reflect.DeepEqual(images, []Image{primary}) {
			t.Fatalf("application image list for target %q = %+v, %v", target, images, err)
		}
		expected := map[string][]Image{"key-visible-item": {primary}}
		if target == "" {
			expected["key-hidden-item"] = []Image{secret}
		} else if _, err := store.ListImagesFor(ctx, subject, "key-hidden-item"); !errors.Is(err, ErrNotFound) {
			t.Errorf("target-scoped image list leaked hidden metadata: %v", err)
		}
		batch, err := store.ImagesForItemsFor(ctx, subject, []string{"key-hidden-item", "key-visible-item", "missing-item"})
		if err != nil || !reflect.DeepEqual(batch, expected) {
			t.Fatalf("application image batch for target %q = %+v, %v", target, batch, err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", subject.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImagesForItemsFor(ctx, subject, []string{"key-hidden-item"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("revoked application credential listed image metadata: %v", err)
	}
}
