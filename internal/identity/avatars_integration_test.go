package identity_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/identity"
)

func avatarTestPNG(t *testing.T, pixel color.NRGBA) []byte {
	t.Helper()
	frame := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			frame.SetNRGBA(x, y, pixel)
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, frame); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestAvatarsPersistExactBytesCASAndDeletionAcrossStoreRecreation(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Avatar viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.GetAvatar(ctx, actor, viewer.ID)
	if err != nil || initial.Revision == "" || initial.Images == nil || len(initial.Images) != 0 {
		t.Fatalf("missing avatar default: %+v, %v", initial, err)
	}
	data := avatarTestPNG(t, color.NRGBA{R: 255, A: 255})
	added, err := store.PutAvatar(ctx, actor, viewer.ID, &initial.Revision, data)
	if err != nil || len(added.Images) != 1 || added.Revision == initial.Revision {
		t.Fatalf("store avatar: %+v, %v", added, err)
	}
	store = identity.New(pool)
	image, err := store.ReadAvatar(ctx, actor, viewer.ID)
	if err != nil || !bytes.Equal(image.Content, data) || image.Tag != added.Images[0].Tag || image.Width != 4 || image.Height != 2 {
		t.Fatalf("reopened avatar does not contain original bytes: %+v, %v", image, err)
	}
	if _, err := store.DeleteAvatar(ctx, actor, viewer.ID, &initial.Revision); !errors.Is(err, artwork.ErrManagedConflict) {
		t.Fatalf("stale deletion: %v", err)
	}
	deleted, err := store.DeleteAvatar(ctx, actor, viewer.ID, &added.Revision)
	if err != nil || len(deleted.Images) != 0 || deleted.Revision == added.Revision || deleted.Revision == initial.Revision {
		t.Fatalf("delete did not retain a new empty revision: %+v, %v", deleted, err)
	}
	if _, err := store.ReadAvatar(ctx, actor, viewer.ID); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("deleted avatar remains readable: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM artwork_images`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted bytes were not reclaimed: %d, %v", count, err)
	}
	if _, err := store.PutAvatar(ctx, actor, viewer.ID, &deleted.Revision, []byte("invalid image")); !errors.Is(err, artwork.ErrInvalidImage) && !errors.Is(err, artwork.ErrUnsupportedFormat) {
		t.Fatalf("invalid bytes accepted: %v", err)
	}
	current, err := store.GetAvatar(ctx, actor, viewer.ID)
	if err != nil || current.Revision != deleted.Revision {
		t.Fatalf("failed upload changed revision: %+v, %v", current, err)
	}
	if _, err := store.PutAvatar(ctx, actor, viewer.ID, &current.Revision, data); err != nil {
		t.Fatal(err)
	}
	managed, err := store.GetManagedUser(ctx, viewer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteManagedUser(ctx, actor, viewer.ID, managed.Revision); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM artwork_images)+(SELECT count(*) FROM artwork_state WHERE user_id=$1)`, viewer.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("account removal left avatar bytes or state: %d, %v", count, err)
	}
}

func TestAvatarsSelfAdministratorApplicationAndCurrentRevocation(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "Avatar self", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, "Avatar other", "other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	credentials, self := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "avatar-device"}, "emby")
	_, embyAdmin := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	key := issueApplicationKey(t, ctx, store, native, "Avatar application")
	application := applicationKeyPrincipal(t, ctx, store, key)
	data := avatarTestPNG(t, color.NRGBA{G: 255, A: 255})
	if _, err := store.PutAvatar(ctx, self, viewer.ID, nil, data); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []identity.Principal{self, native, embyAdmin, application} {
		if image, err := store.ReadAvatar(ctx, actor, viewer.ID); err != nil || !bytes.Equal(image.Content, data) {
			t.Fatalf("authorized actor %s cannot read avatar: %v", actor.Kind, err)
		}
	}
	forged := self
	forged.User.IsAdministrator = true
	if _, err := store.GetAvatar(ctx, forged, other.ID); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("snapshot role granted access to another user: %v", err)
	}
	if _, err := store.PutAvatar(ctx, forged, other.ID, nil, data); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("ordinary user edited another avatar: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableUserPreferenceAccess":false}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteAvatar(ctx, self, viewer.ID, nil); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("preference restriction did not protect mutation: %v", err)
	}
	if _, err := store.ReadAvatar(ctx, self, viewer.ID); err != nil {
		t.Fatalf("preference restriction unexpectedly hid self avatar: %v", err)
	}
	if err := store.Revoke(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAvatar(ctx, self, viewer.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked actor read avatar: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, application.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAvatar(ctx, application, viewer.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked key read avatar: %v", err)
	}
}

func TestPublicAvatarsUseCurrentLoginPickerPolicyAndDeviceHistory(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Public avatar", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	data := avatarTestPNG(t, color.NRGBA{B: 255, A: 255})
	for _, id := range []string{admin.ID, viewer.ID} {
		if _, err := store.PutAvatar(ctx, actor, id, nil, data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ReadPublicAvatar(ctx, admin.ID, false, ""); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("administrator avatar became public: %v", err)
	}
	if _, err := store.ReadPublicAvatar(ctx, viewer.ID, false, ""); err != nil {
		t.Fatalf("visible login avatar unavailable: %v", err)
	}
	for _, patch := range []string{`{"IsHidden":true}`, `{"IsHiddenRemotely":true}`, `{"EnableRemoteAccess":false}`, `{"EnableAllDevices":false,"EnabledDevices":["known-device"]}`, `{"IsHiddenFromUnusedDevices":true}`} {
		if _, err := pool.Exec(ctx, `UPDATE users SET policy=$2::jsonb WHERE id=$1`, viewer.ID, patch); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReadPublicAvatar(ctx, viewer.ID, true, ""); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("public avatar escaped policy %s: %v", patch, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	credentials, _ := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "known-device"}, "emby")
	if err := store.Revoke(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{"IsHiddenFromUnusedDevices":true}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadPublicAvatar(ctx, viewer.ID, false, "known-device"); err != nil {
		t.Fatalf("historical device use was lost after revocation: %v", err)
	}
	if _, err := store.ReadPublicAvatar(ctx, viewer.ID, false, "unknown-device"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("unused device saw hidden avatar: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadPublicAvatar(ctx, viewer.ID, false, "known-device"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("disabled avatar remained public: %v", err)
	}
}

func TestAvatarConcurrentCASAndExpiryAfterWriteRollBack(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	initial, err := store.GetAvatar(ctx, actor, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	data := avatarTestPNG(t, color.NRGBA{R: 150, A: 255})
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := store.PutAvatar(ctx, actor, admin.ID, &initial.Revision, data)
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	succeeded, conflicts := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, artwork.ErrManagedConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicts != 1 {
		t.Fatalf("concurrent avatar CAS: successful=%d conflicts=%d", succeeded, conflicts)
	}
	before, err := store.GetAvatar(ctx, actor, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION expire_avatar_writer() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours', expires_at=clock_timestamp()-interval '1 hour'
		WHERE user_id=(SELECT user_id FROM artwork_state WHERE id=NEW.state_id); RETURN NEW; END; $$;
		CREATE TRIGGER expire_avatar_writer AFTER INSERT ON artwork_images FOR EACH ROW EXECUTE FUNCTION expire_avatar_writer()`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutAvatar(ctx, actor, admin.ID, &before.Revision, avatarTestPNG(t, color.NRGBA{B: 123, A: 255})); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("expiry after byte write did not reject commit: %v", err)
	}
	after, err := store.GetAvatar(ctx, actor, admin.ID)
	if err != nil || before.Revision != after.Revision || before.Images[0].Tag != after.Images[0].Tag {
		t.Fatalf("failed final authority check changed avatar: %+v, %v", after, err)
	}
}
