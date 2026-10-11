//go:build linux

package identity_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestNotificationWitnessUsesFirstSecretAndRecoveryChecksEverySecret(t *testing.T) {
	for _, receiver := range []bool{true, false} {
		name := "target only"
		if receiver {
			name = "receiver first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, admin, masterPath := applicationKeyTestStore(t)
			viewer, err := store.CreateUser(ctx, "Notification Witness Viewer", "viewer-password", false)
			if err != nil {
				t.Fatal(err)
			}
			_, firstActor := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
			_, secondActor := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
			tx := applicationKeyBackupTestTx(t, ctx, pool, false)
			if err := identity.LockNotificationMutation(ctx, tx, admin, true); err != nil {
				t.Fatal(err)
			}
			if receiver {
				sealed, err := store.SealNotificationSecret(ctx, tx, identity.NotificationReceiverPurpose,
					identity.NotificationSecretBinding("receiver", "1"), "receiver-witness-secret")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, `UPDATE notification_transport SET endpoint='https://receiver.invalid/events',
					credential_ciphertext=$1 WHERE id=1`, sealed); err != nil {
					t.Fatal(err)
				}
			}
			firstID, secondID := strings.Repeat("a", 32), strings.Repeat("b", 32)
			// Reverse insertion order makes the first witness depend on the stored
			// purpose and binding order instead of fixture insertion order.
			for _, target := range []struct {
				id    string
				actor identity.Principal
			}{
				{secondID, secondActor},
				{firstID, firstActor},
			} {
				binding := identity.NotificationSecretBinding(target.id, target.actor.SessionID, viewer.ID, target.actor.Client.DeviceID, "1")
				sealed, err := store.SealNotificationSecret(ctx, tx, identity.NotificationTargetPurpose, binding, "target-witness-secret")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, `INSERT INTO notification_registrations
					(id,session_id,user_id,device_id,peer_ip,event_ids,token_ciphertext)
					VALUES($1,$2,$3,$4,'',ARRAY['CatalogInvalidated'],$5)`,
					target.id, target.actor.SessionID, viewer.ID, target.actor.Client.DeviceID, sealed); err != nil {
					t.Fatal(err)
				}
			}
			master, err := os.ReadFile(masterPath)
			if err != nil {
				t.Fatal("read owned notification master fixture")
			}
			defer clear(master)
			wantCount := int64(2)
			if receiver {
				wantCount++
			}
			witness, err := identity.ValidateApplicationKeyRecovery(ctx, tx, master)
			if err != nil || witness.SealedNotificationCount != wantCount || !witness.HasMasterKey {
				t.Fatalf("complete recovery omitted a notification secret: %v", err)
			}
			sealProbe := func() ([]byte, error) {
				return store.SealNotificationSecret(ctx, tx, identity.NotificationReceiverPurpose,
					identity.NotificationSecretBinding("receiver", "9"), "new-receiver-witness-secret")
			}
			corruptLater := func() {
				t.Helper()
				if _, err := tx.Exec(ctx, `UPDATE notification_registrations SET token_ciphertext=set_byte(token_ciphertext,
					octet_length(token_ciphertext)-1,get_byte(token_ciphertext,octet_length(token_ciphertext)-1) # 1)
					WHERE id=$1`, secondID); err != nil {
					t.Fatal(err)
				}
			}
			corruptLater()
			if sealed, err := sealProbe(); err != nil || len(sealed) == 0 {
				t.Fatalf("master witness evaluated a secret after the valid first row: %v", err)
			}
			if _, err := identity.ValidateApplicationKeyRecovery(ctx, tx, master); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
				t.Fatal("complete recovery accepted a corrupt later notification secret")
			}
			corruptLater()
			if receiver {
				_, err = tx.Exec(ctx, "UPDATE notification_transport SET credential_generation=2 WHERE id=1")
			} else {
				_, err = tx.Exec(ctx, "UPDATE notification_registrations SET token_generation=2 WHERE id=$1", firstID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if sealed, err := sealProbe(); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) || sealed != nil {
				t.Fatal("master witness skipped a first row with an invalid generation binding")
			}
			if receiver {
				_, err = tx.Exec(ctx, "UPDATE notification_transport SET credential_generation=1 WHERE id=1")
			} else {
				_, err = tx.Exec(ctx, "UPDATE notification_registrations SET token_generation=1 WHERE id=$1", firstID)
			}
			if err != nil {
				t.Fatal(err)
			}
			wrongMaster := append([]byte(nil), master...)
			defer clear(wrongMaster)
			wrongMaster[0] ^= 0xff
			if err := os.WriteFile(masterPath, wrongMaster, 0o600); err != nil {
				t.Fatal("replace owned notification master fixture")
			}
			if sealed, err := sealProbe(); !errors.Is(err, identity.ErrApplicationKeyVaultUnsafe) || sealed != nil {
				t.Fatalf("an observed master-file change did not retain its unsafe-file error: %v", err)
			}
			reopened := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
			if sealed, err := reopened.SealNotificationSecret(ctx, tx, identity.NotificationReceiverPurpose,
				identity.NotificationSecretBinding("receiver", "9"), "new-receiver-witness-secret"); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) || sealed != nil {
				t.Fatalf("a fresh vault did not reject the changed key against its stored witness: %v", err)
			}
			if err := os.Remove(masterPath); err != nil {
				t.Fatal("remove owned notification master fixture")
			}
			if sealed, err := sealProbe(); !errors.Is(err, identity.ErrApplicationKeyVaultMissing) || sealed != nil {
				t.Fatal("master witness recreated a missing key despite retained notification history")
			}
			if _, err := os.Stat(masterPath); !os.IsNotExist(err) {
				t.Fatal("rejected notification witness created a new master file")
			}
		})
	}
}
