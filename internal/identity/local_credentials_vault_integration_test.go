package identity_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestStoreProfilePinEncryptionOwnerProjectionAndAtomicPatch(t *testing.T) {
	ctx, pool, store, admin, masterPath := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "Profile PIN owner", "normal-profile-password", false)
	if err != nil {
		t.Fatal(err)
	}
	credentials, actor := managedLogin(t, ctx, store, viewer, "normal-profile-password", "emby")
	initial, err := store.GetUserPreferences(ctx, actor, viewer.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, &initial.Revision, identity.UserConfigurationPatch{
		"ProfilePin": json.RawMessage(`"4826"`), "SubtitleMode": json.RawMessage(`"Always"`),
	})
	if err != nil || updated.Revision != initial.Revision+1 || updated.Configuration.SubtitleMode != "Always" {
		t.Fatalf("atomic PIN/preference patch: %v", err)
	}
	var sealed, raw []byte
	if err := pool.QueryRow(ctx, "SELECT profile_pin_ciphertext,configuration FROM users WHERE id=$1", viewer.ID).Scan(&sealed, &raw); err != nil {
		t.Fatal(err)
	}
	if len(sealed) != 36 || bytes.Contains(sealed, []byte("4826")) || bytes.Contains(raw, []byte("ProfilePin")) || bytes.Contains(raw, []byte("4826")) {
		t.Fatal("profile PIN was not isolated in authenticated ciphertext")
	}
	status, err := store.GetLocalCredentials(ctx, admin, viewer.ID)
	if err != nil || !status.HasProfilePin || status.HasLocalPassword {
		t.Fatal("native credential flags do not describe the independent profile PIN")
	}
	encoded, err := json.Marshal(status)
	if err != nil || bytes.Contains(encoded, []byte("4826")) || bytes.Contains(encoded, []byte("ciphertext")) {
		t.Fatal("native credential DTO contains a secret")
	}
	if pin, err := store.GetOwnProfilePin(ctx, actor, viewer.ID); err != nil || pin != "4826" {
		t.Fatal("owner could not read the compatible profile gate")
	}
	if _, err := store.GetOwnProfilePin(ctx, admin, viewer.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("native administrator read another user's profile PIN")
	}
	if _, err := store.Resolve(ctx, credentials.Token, "emby"); err != nil {
		t.Fatal("PIN-only change revoked the owner's login")
	}
	if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, "4826", identity.Client{}, "emby", "127.0.0.1"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("profile PIN became a login password")
	}
	if _, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, &updated.Revision, identity.UserConfigurationPatch{
		"ProfilePin": json.RawMessage(`"1357"`), "SubtitleMode": json.RawMessage(`"Unsupported"`),
	}); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatal("invalid mixed preference patch was accepted")
	}
	if pin, err := store.GetOwnProfilePin(ctx, actor, viewer.ID); err != nil || pin != "4826" {
		t.Fatal("failed mixed preference patch partially replaced the PIN")
	}
	for _, value := range []string{`"123"`, `"12345"`, `"12a4"`, `"１２３４"`, `4826`, `true`} {
		if _, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, nil, identity.UserConfigurationPatch{"ProfilePin": json.RawMessage(value)}); !errors.Is(err, identity.ErrInvalidInput) {
			t.Fatal("invalid profile PIN shape was accepted")
		}
	}
	store = identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
	if pin, err := store.GetOwnProfilePin(ctx, actor, viewer.ID); err != nil || pin != "4826" {
		t.Fatal("restart lost the encrypted profile PIN")
	}
	key, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	witness, witnessErr := identity.ValidateApplicationKeyRecovery(ctx, tx, key)
	_ = tx.Rollback(ctx)
	if witnessErr != nil || witness.SealedKeyCount != 0 || witness.SealedProfilePinCount != 1 || !witness.HasMasterKey {
		t.Fatal("PIN-only recovery failed to witness the required encryption key")
	}
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := pool.Exec(ctx, "UPDATE users SET profile_pin_ciphertext=$2 WHERE id=$1", viewer.ID, tampered); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOwnProfilePin(ctx, actor, viewer.ID); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatal("tampered PIN ciphertext was accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET profile_pin_ciphertext=$2 WHERE id=$1", viewer.ID, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableUserPreferenceAccess":false}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if pin, err := store.GetOwnProfilePin(ctx, actor, viewer.ID); err != nil || pin != "4826" {
		t.Fatal("disabled preference editing suppressed the owner's existing profile gate")
	}
	if _, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, nil, identity.UserConfigurationPatch{"ProfilePin": json.RawMessage(`null`)}); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatal("preference policy did not protect PIN mutation")
	}
	if _, err := store.ResetManagedUserPassword(ctx, admin, viewer.ID, readManagedUser(t, ctx, store, viewer.ID).Revision, "reset-profile-password"); err != nil {
		t.Fatal(err)
	}
	status, err = store.GetLocalCredentials(ctx, admin, viewer.ID)
	if err != nil || status.HasProfilePin {
		t.Fatal("normal-password reset retained the profile lock")
	}
}

func TestStoreProfilePinMasterLossCannotCreateReplacementOrExposeOtherUser(t *testing.T) {
	ctx, pool, store, admin, masterPath := applicationKeyTestStore(t)
	first, err := store.CreateUser(ctx, "PIN first account", "profile-password", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateUser(ctx, "PIN second account", "profile-password", false)
	if err != nil {
		t.Fatal(err)
	}
	credentials, actor := managedLogin(t, ctx, store, first, "profile-password", "emby")
	_, outsider := managedLogin(t, ctx, store, second, "profile-password", "emby")
	if _, err := store.UpdateUserPreferences(ctx, actor, first.ID, nil, identity.UserConfigurationPatch{"ProfilePin": json.RawMessage(`"2468"`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOwnProfilePin(ctx, outsider, first.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("another account read a profile PIN")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET profile_pin_ciphertext=(SELECT profile_pin_ciphertext FROM users WHERE id=$1) WHERE id=$2", first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOwnProfilePin(ctx, outsider, second.ID); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatal("swapped account ciphertext authenticated")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET profile_pin_ciphertext=NULL WHERE id=$1", second.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(masterPath, masterPath+".retained"); err != nil {
		t.Fatal(err)
	}
	store = identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
	if _, err := store.CreateApplicationKey(ctx, admin, "Missing PIN master", "127.0.0.1", identity.Client{DeviceID: "server"}); !errors.Is(err, identity.ErrApplicationKeyVaultMissing) {
		t.Fatal("application-key creation replaced a lost PIN master")
	}
	if _, err := os.Stat(masterPath); !os.IsNotExist(err) {
		t.Fatal("lost master key was silently recreated")
	}
	if _, err := store.UpdateUserPreferences(ctx, actor, first.ID, nil, identity.UserConfigurationPatch{"ProfilePin": json.RawMessage(`null`)}); err != nil {
		t.Fatal("could not clear a profile PIN after master-key loss")
	}
	if _, err := store.Resolve(ctx, credentials.Token, "emby"); err != nil {
		t.Fatal("clearing the profile PIN revoked the existing login")
	}
}
