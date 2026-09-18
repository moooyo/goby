//go:build linux

package library

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestParentalPolicyRevocationClosesDirectMediaAndSubtitleResources(t *testing.T) {
	fixture, track, _ := subtitleTestCatalog(t)
	ctx, store, pool := fixture.ctx, fixture.store, fixture.pool
	mediaSourceTestOpenError(t, ctx, store, fixture.userID, fixture.item.ID, "", nil)
	if _, err := store.ReadSubtitle(ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "parental-resource-editor")
	before := metadataEditTestDetail(t, ctx, store, actor, fixture.item.ID)
	overrides := metadataEditTestCopy(before.Overrides)
	overrides["OfficialRating"] = metadataEditTestRaw(t, "R")
	updated := metadataEditTestUpdate(t, ctx, store, actor, before, overrides, before.LockedFields)
	if updated.Effective.OfficialRating != "R" {
		t.Fatalf("parental fixture did not publish its effective rating: %q", updated.Effective.OfficialRating)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5}' WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	mediaSourceTestOpenError(t, ctx, store, fixture.userID, fixture.item.ID, "", ErrNotFound)
	if _, err := store.ReadSubtitle(ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("denied subtitle remained readable: %v", err)
	}
}

func TestAuthorizedImageSourceRechecksPolicyBeforeOpening(t *testing.T) {
	ctx, pool, store, allowed, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := imageStoreTestRoot(t, ctx, pool, store, allowed, "policy-images")
	imageStoreTestInsert(t, ctx, pool, root, "policy-image-item", "Primary", 0, "poster.png", imageStoreTestPNG(t))
	subject := Subject{UserID: userID}
	file, _, err := store.OpenImageFor(ctx, subject, "policy-image-item", "Primary", 0)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"ExcludedSubFolders":["policy-image-item"]}' WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	file, _, err = store.OpenImageFor(ctx, subject, "policy-image-item", "Primary", 0)
	if file != nil {
		file.Close()
		t.Fatal("denied image returned an open file")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("denied image read error=%v", err)
	}
}
