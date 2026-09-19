//go:build linux

package recovery

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
)

// This exercises the encrypted archive and restore finalizer, including the
// extracted master. The PIN ciphertext is created only by the identity owner;
// neither the database archive nor the recovered credential is synthesized.
func TestEngineSelectedPhase1EncryptedProfilePinBackupRestore(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	const ownerName = "Phase one encrypted profile owner"
	const ownerPassword = "phase-one-profile-owner-password"
	const profilePin = "4826"
	const peer = "192.168.30.20"
	owner, err := f.identities.CreateManagedUser(f.ctx, f.actor, ownerName, ownerPassword, false)
	if err != nil {
		t.Fatal("create the encrypted profile fixture through identity management")
	}
	oldLogin, err := f.identities.AuthenticateWithPeer(f.ctx, ownerName, ownerPassword,
		identity.Client{Name: "Profile recovery source", DeviceID: "profile-recovery-source-device"}, "emby", peer)
	if err != nil {
		t.Fatal("issue the source profile owner's login")
	}
	ownerActor, err := f.identities.ResolveWithPeer(f.ctx, oldLogin.Token, "emby", peer)
	if err != nil {
		t.Fatal("resolve the source profile owner")
	}
	pinJSON, err := json.Marshal(profilePin)
	if err != nil {
		t.Fatal("encode the profile fixture input")
	}
	preferences, err := f.identities.UpdateUserPreferences(f.ctx, ownerActor, owner.ID, nil,
		identity.UserConfigurationPatch{"ProfilePin": pinJSON, "SubtitleMode": json.RawMessage(`"Always"`)})
	if err != nil || preferences.Configuration.SubtitleMode != "Always" {
		t.Fatal("persist a real encrypted PIN with its companion preference")
	}
	if pin, err := f.identities.GetOwnProfilePin(f.ctx, ownerActor, owner.ID); err != nil || pin != profilePin {
		t.Fatal("the source owner cannot read the configured profile gate")
	}
	var originalCiphertext []byte
	var plaintextInConfiguration bool
	if err := f.source.QueryRow(f.ctx, `SELECT profile_pin_ciphertext,configuration ? 'ProfilePin'
		FROM users WHERE id=$1`, owner.ID).Scan(&originalCiphertext, &plaintextInConfiguration); err != nil {
		t.Fatal("read the protected source profile state")
	}
	if len(originalCiphertext) != 36 || plaintextInConfiguration {
		t.Fatal("the fixture did not store the profile PIN exclusively as authenticated ciphertext")
	}
	var activeSourceCredentials int64
	if err := f.source.QueryRow(f.ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&activeSourceCredentials); err != nil {
		t.Fatal("count credentials requiring recovery revocation")
	}
	retainedBefore := recoveryEngineRetainedState(t, f.ctx, f.source)
	manifest, metadata := f.create(t)
	masterIncluded := false
	for _, file := range manifest.Files {
		if file.Name == backupformat.MasterKeyName {
			masterIncluded = file.Size == 32
		}
	}
	if !masterIncluded {
		t.Fatal("the encrypted archive omitted the witnessed profile encryption master")
	}
	reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
	if err != nil {
		t.Fatal("open the generated encrypted profile backup")
	}
	defer reader.Close()
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	archive, err := f.engine.OpenArchive(f.ctx, reader, passphrase)
	if err != nil {
		t.Fatal("authenticate and extract the generated profile backup")
	}
	defer archive.Close()
	releaseRecoveryEngineTestMemory()
	lease, err := database.AcquireLease(f.ctx, f.target)
	if err != nil {
		t.Fatal("lease the separately owned empty restore target")
	}
	defer lease.Close()
	result, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
	if err != nil {
		t.Fatalf("restore the real encrypted profile archive through its finalizer: %v", err)
	}
	if result.SourceVersion != manifest.Source.SchemaVersion || result.CurrentVersion != manifest.Source.SchemaVersion ||
		result.RevokedCredentials != activeSourceCredentials {
		t.Fatal("restore did not retain the admitted schema and revoke every imported credential")
	}
	if restored := recoveryEngineRetainedState(t, f.ctx, f.target); restored != retainedBefore {
		t.Fatal("the real restore changed retained credential, profile, or companion preference state")
	}
	var restoredCiphertext []byte
	if err := f.target.QueryRow(f.ctx, `SELECT profile_pin_ciphertext,configuration ? 'ProfilePin'
		FROM users WHERE id=$1`, owner.ID).Scan(&restoredCiphertext, &plaintextInConfiguration); err != nil {
		t.Fatal("read the restored encrypted profile state")
	}
	if !bytes.Equal(restoredCiphertext, originalCiphertext) || plaintextInConfiguration {
		t.Fatal("archive restore replaced the real PIN ciphertext or exposed it as plaintext configuration")
	}
	tx, err := f.target.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin the restored profile master witness")
	}
	witness, witnessErr := identity.ValidateApplicationKeyRecovery(f.ctx, tx, archive.Master)
	rollbackErr := tx.Rollback(f.ctx)
	if witnessErr != nil || rollbackErr != nil || !witness.HasMasterKey || witness.SealedProfilePinCount != 1 || witness.SealedKeyCount != 2 {
		t.Fatal("the extracted archive master did not authenticate the restored PIN and retained key history")
	}
	var unrevoked int64
	if err := f.target.QueryRow(f.ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Fatal("the restore finalizer left an imported credential usable")
	}
	importedIdentities := identity.New(f.target)
	for _, login := range []struct {
		token string
		kind  string
	}{{oldLogin.Token, "emby"}, {f.embyLogin.Token, "emby"}, {f.adminLogin.Token, "admin"}} {
		if _, err := importedIdentities.ResolveWithPeer(f.ctx, login.token, login.kind, peer); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("an original user session authenticated against the restored database")
		}
	}
	for _, key := range []identity.ApplicationKey{f.activeKey, f.revokedKey} {
		if _, err := importedIdentities.ResolveEmby(f.ctx, key.Token); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("an original application key authenticated against the restored database")
		}
	}
	// RestoreInto stages database state. Its deployment owner subsequently
	// installs the already validated extracted master at a target-owned path.
	// Use a new protected file and close the archive before opening a new vault
	// so the fresh login cannot rely on the original vault or archive buffers.
	masterFile, err := os.OpenFile(f.targetConfig.APIKeyMasterKeyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("create the target-owned restored master file")
	}
	written, writeErr := masterFile.Write(archive.Master)
	syncErr := masterFile.Sync()
	closeErr := masterFile.Close()
	if writeErr != nil || written != 32 || syncErr != nil || closeErr != nil {
		t.Fatal("persist the authenticated extracted master for the restored identity owner")
	}
	masterAlias := archive.Master
	if err := archive.Close(); err != nil || !bytes.Equal(masterAlias, make([]byte, 32)) {
		t.Fatal("archive close did not release and clear its extracted master")
	}
	restoredStore := identity.NewWithApplicationKeyVault(f.target, identity.NewApplicationKeyVault(f.targetConfig.APIKeyMasterKeyFile))
	freshLogin, err := restoredStore.AuthenticateWithPeer(f.ctx, ownerName, ownerPassword,
		identity.Client{Name: "Profile recovery target", DeviceID: "profile-recovery-target-device"}, "emby", peer)
	if err != nil {
		t.Fatal("the restored normal password cannot issue a fresh owner login")
	}
	freshOwner, err := restoredStore.ResolveWithPeer(f.ctx, freshLogin.Token, "emby", peer)
	if err != nil || freshOwner.User.ID != owner.ID {
		t.Fatal("the fresh restored login did not resolve the original profile owner")
	}
	if pin, err := restoredStore.GetOwnProfilePin(f.ctx, freshOwner, owner.ID); err != nil || pin != profilePin {
		t.Fatal("the new owner login cannot decrypt the original profile PIN using the restored master")
	}
	restoredPreferences, err := restoredStore.GetUserPreferences(f.ctx, freshOwner, owner.ID)
	if err != nil || restoredPreferences.Revision != preferences.Revision || restoredPreferences.Configuration.SubtitleMode != "Always" {
		t.Fatal("profile recovery lost the atomically stored companion preference or its revision")
	}
	if _, err := restoredStore.GetOwnProfilePin(f.ctx, ownerActor, owner.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("a stale source principal read the restored profile PIN after credential revocation")
	}
	if after := recoveryEngineRetainedState(t, f.ctx, f.source); after != retainedBefore {
		t.Fatal("the profile archive or restore changed the original source state")
	}
}
