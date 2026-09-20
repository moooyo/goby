//go:build linux

package identity_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestNotificationSecretsUseSeparatePurposesWitnessAndRestoreFence(t *testing.T) {
	ctx, pool, store, admin, masterPath := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "Notification secret viewer", "notification-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := managedLogin(t, ctx, store, viewer, "notification-password", "emby")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err = identity.LockNotificationMutation(ctx, tx, admin, true); err != nil {
		t.Fatal(err)
	}
	credential, target := "receiver-secret-sentinel-48", "target-secret-sentinel-48"
	configBinding := identity.NotificationSecretBinding("receiver", "1")
	sealedCredential, err := store.SealNotificationSecret(ctx, tx, identity.NotificationReceiverPurpose, configBinding, credential)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE notification_transport SET endpoint='https://receiver.invalid/events',credential_ciphertext=$1 WHERE id=1`, sealedCredential); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	binding := identity.NotificationSecretBinding(id, actor.SessionID, viewer.ID, actor.Client.DeviceID, "1")
	sealedTarget, err := store.SealNotificationSecret(ctx, tx, identity.NotificationTargetPurpose, binding, target)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealedCredential, []byte(credential)) || bytes.Contains(sealedTarget, []byte(target)) {
		t.Fatal("notification secret was stored in plaintext")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,event_ids,token_ciphertext) VALUES($1,$2,$3,$4,'',ARRAY['CatalogInvalidated'],$5)`, id, actor.SessionID, viewer.ID, actor.Client.DeviceID, sealedTarget); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
	if got, err := restarted.OpenNotificationSecret(ctx, identity.NotificationTargetPurpose, binding, sealedTarget); err != nil || got != target {
		t.Fatal("restart lost the sealed registration")
	}
	if _, err := restarted.OpenNotificationSecret(ctx, identity.NotificationReceiverPurpose, binding, sealedTarget); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatal("target ciphertext crossed a vault purpose")
	}
	if _, err := restarted.OpenNotificationSecret(ctx, identity.NotificationTargetPurpose, identity.NotificationSecretBinding(id, actor.SessionID, viewer.ID, actor.Client.DeviceID, "2"), sealedTarget); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatal("target ciphertext crossed a generation")
	}
	witnessTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer witnessTx.Rollback(context.Background())
	master := make([]byte, 32)
	defer clear(master)
	witness, err := identity.NewApplicationKeyVault(masterPath).WitnessBackup(ctx, witnessTx, master)
	if err != nil || witness.SealedNotificationCount != 2 || !witness.HasMasterKey {
		t.Fatal("backup witness omitted notification purposes")
	}
	if err = identity.NormalizeNotificationRestore(ctx, witnessTx); err != nil {
		t.Fatal(err)
	}
	var first, second string
	if witnessTx.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM notification_registrations r WHERE id=$1`, id).Scan(&first) != nil {
		t.Fatal("read restore fence")
	}
	if err = identity.NormalizeNotificationRestore(ctx, witnessTx); err != nil {
		t.Fatal(err)
	}
	if witnessTx.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM notification_registrations r WHERE id=$1`, id).Scan(&second) != nil || first != second {
		t.Fatal("restore normalization is not idempotent")
	}
	if _, err = identity.ValidateApplicationKeyRecovery(ctx, witnessTx, master); err != nil {
		t.Fatal("normalization changed sealed history")
	}
}
