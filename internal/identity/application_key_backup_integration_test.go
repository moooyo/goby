package identity_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func applicationKeyBackupTestTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool, readOnly bool) pgx.Tx {
	t.Helper()
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead}
	if readOnly {
		options.AccessMode = pgx.ReadOnly
	}
	tx, err := pool.BeginTx(ctx, options)
	if err != nil {
		t.Fatalf("begin application key backup transaction: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	})
	return tx
}

func TestApplicationKeyBackupUsesExportedSnapshotAndRevokedHistory(t *testing.T) {
	ctx, pool, store, actor, path := applicationKeyTestStore(t)
	revoked := issueApplicationKey(t, ctx, store, actor, "Revoked history")
	issueApplicationKey(t, ctx, store, actor, "Active credential")
	if _, err := store.RevokeApplicationKey(ctx, actor, revoked.ID); err != nil {
		t.Fatal(err)
	}
	tx := applicationKeyBackupTestTx(t, ctx, pool, true)
	var snapshot string
	if err := tx.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&snapshot); err != nil || snapshot == "" {
		t.Fatalf("export backup snapshot: %v", err)
	}
	issueApplicationKey(t, ctx, store, actor, "After the snapshot")
	before := applicationKeySnapshot(t, ctx, pool)
	master := bytes.Repeat([]byte{0xff}, 32)
	defer clear(master)
	witness, err := identity.NewApplicationKeyVault(path).WitnessBackup(ctx, tx, master)
	if err != nil || witness.SealedKeyCount != 2 || !witness.HasMasterKey {
		t.Fatalf("backup lost the exported snapshot or revoked key history: %v", err)
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read owned master fixture")
	}
	defer clear(expected)
	if len(expected) != 32 || !bytes.Equal(master, expected) {
		t.Fatal("backup did not return the exact authenticated master")
	}
	if after := applicationKeySnapshot(t, ctx, pool); after != before {
		t.Fatal("backup witness changed persisted identity state")
	}
}

func TestApplicationKeyBackupRejectsCorruptRevokedCiphertext(t *testing.T) {
	ctx, pool, store, actor, path := applicationKeyTestStore(t)
	issueApplicationKey(t, ctx, store, actor, "First valid history")
	issueApplicationKey(t, ctx, store, actor, "Second valid history")
	key := issueApplicationKey(t, ctx, store, actor, "Revoked encrypted history")
	if _, err := store.RevokeApplicationKey(ctx, actor, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE application_keys SET secret_ciphertext = set_byte(secret_ciphertext,
		octet_length(secret_ciphertext) - 1, get_byte(secret_ciphertext, octet_length(secret_ciphertext) - 1) # 1)
		WHERE id = $1`, key.ID); err != nil {
		t.Fatal("corrupt owned ciphertext fixture")
	}
	before := applicationKeySnapshot(t, ctx, pool)
	tx := applicationKeyBackupTestTx(t, ctx, pool, true)
	master := bytes.Repeat([]byte{0xff}, 32)
	witness, err := identity.NewApplicationKeyVault(path).WitnessBackup(ctx, tx, master)
	if !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) || witness != (identity.ApplicationKeyBackupWitness{}) {
		t.Fatal("backup accepted corrupted revoked ciphertext")
	}
	if !bytes.Equal(master, make([]byte, 32)) {
		t.Fatal("failed witness exposed a master key")
	}
	if after := applicationKeySnapshot(t, ctx, pool); after != before {
		t.Fatal("failed witness changed persisted identity state")
	}
}

func TestApplicationKeyRecoveryValidatesProtectedMasterWithoutActiveVault(t *testing.T) {
	ctx, pool, store, actor, path := applicationKeyTestStore(t)
	issueApplicationKey(t, ctx, store, actor, "Recovered credential")
	master, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read owned master fixture")
	}
	defer clear(master)
	if err := os.Remove(path); err != nil {
		t.Fatal("remove owned active master fixture")
	}
	tx := applicationKeyBackupTestTx(t, ctx, pool, true)
	witness, err := identity.ValidateApplicationKeyRecovery(ctx, tx, master)
	if err != nil || witness.SealedKeyCount != 1 || !witness.HasMasterKey {
		t.Fatalf("recovery depended on the active vault file: %v", err)
	}
	missing := bytes.Repeat([]byte{0xff}, 32)
	if _, err := identity.NewApplicationKeyVault(path).WitnessBackup(ctx, tx, missing); !errors.Is(err, identity.ErrApplicationKeyVaultMissing) {
		t.Fatal("backup accepted a missing master with sealed history")
	}
	if !bytes.Equal(missing, make([]byte, 32)) {
		t.Fatal("missing master did not clear the output buffer")
	}
	wrong := bytes.Clone(master)
	defer clear(wrong)
	wrong[0] ^= 0xff
	if _, err := identity.ValidateApplicationKeyRecovery(ctx, tx, wrong); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatal("recovery accepted a different master key")
	}
	if _, err := identity.ValidateApplicationKeyRecovery(ctx, tx, nil); !errors.Is(err, identity.ErrApplicationKeyVaultMissing) {
		t.Fatal("recovery accepted sealed history without a master")
	}
}

func TestApplicationKeyBackupEmptyStateNeverCreatesMaster(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("application key backup vault requires Linux")
	}
	ctx, pool, _ := identityTestStore(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "master.key")
	master := bytes.Repeat([]byte{0xff}, 32)
	defer clear(master)
	tx := applicationKeyBackupTestTx(t, ctx, pool, true)
	witness, err := identity.NewApplicationKeyVault(path).WitnessBackup(ctx, tx, master)
	if err != nil || witness != (identity.ApplicationKeyBackupWitness{}) || !bytes.Equal(master, make([]byte, 32)) {
		t.Fatalf("empty backup fabricated a master key: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("empty backup created a master file")
	}
	if recovered, err := identity.ValidateApplicationKeyRecovery(ctx, tx, nil); err != nil || recovered != witness {
		t.Fatalf("empty recovery required a master key: %v", err)
	}
	fixture := bytes.Repeat([]byte{0x41}, 32)
	defer clear(fixture)
	if err := os.WriteFile(path, fixture, 0600); err != nil {
		t.Fatal("write owned empty-database master fixture")
	}
	witness, err = identity.NewApplicationKeyVault(path).WitnessBackup(ctx, tx, master)
	if err != nil || witness.SealedKeyCount != 0 || !witness.HasMasterKey || !bytes.Equal(master, fixture) {
		t.Fatalf("empty backup discarded an existing master key: %v", err)
	}
}

func TestApplicationKeyBackupRejectsUnexpectedRowLinkage(t *testing.T) {
	for _, test := range []struct {
		name     string
		sql      string
		useActor bool
	}{
		{name: "orphan application credential", sql: `DELETE FROM application_keys WHERE credential_id = $1`},
		{name: "key attached to user login", sql: `UPDATE application_keys SET credential_id = $2 WHERE credential_id = $1`, useActor: true},
		{name: "application client attached to user login", sql: `UPDATE application_key_clients SET credential_id = $2 WHERE credential_id = $1`, useActor: true},
		{name: "active key on deleted device", sql: `UPDATE application_key_devices SET deleted_at = clock_timestamp()
			WHERE id = (SELECT reported_device_numeric_id FROM application_keys WHERE credential_id = $1)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool, store, actor, path := applicationKeyTestStore(t)
			key := issueApplicationKey(t, ctx, store, actor, "Linkage fixture")
			arguments := []any{key.CredentialID}
			if test.useActor {
				arguments = append(arguments, actor.SessionID)
			}
			if _, err := pool.Exec(ctx, test.sql, arguments...); err != nil {
				t.Fatal("modify owned identity linkage fixture")
			}
			tx := applicationKeyBackupTestTx(t, ctx, pool, true)
			master := bytes.Repeat([]byte{0xff}, 32)
			witness, err := identity.NewApplicationKeyVault(path).WitnessBackup(ctx, tx, master)
			if !errors.Is(err, identity.ErrApplicationKeyBackupInvalid) || witness != (identity.ApplicationKeyBackupWitness{}) {
				t.Fatal("backup accepted inconsistent application credential linkage")
			}
			if !bytes.Equal(master, make([]byte, 32)) {
				t.Fatal("invalid row linkage exposed a master key")
			}
		})
	}
}

func TestRevokeRecoveredCredentialsRetainsHistoryAndCallerTransaction(t *testing.T) {
	ctx, pool, store, actor, path := applicationKeyTestStore(t)
	revoked := issueApplicationKey(t, ctx, store, actor, "Already revoked")
	active := issueApplicationKey(t, ctx, store, actor, "Recovered key")
	if _, err := store.RevokeApplicationKey(ctx, actor, revoked.ID); err != nil {
		t.Fatal(err)
	}
	login, _ := managedLogin(t, ctx, store, actor.User, "administrator-password", "emby")
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = created_at - interval '2 days',
		expires_at = created_at - interval '1 day' WHERE id = $1`, login.SessionID); err != nil {
		t.Fatal("expire owned login fixture")
	}
	master, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read owned recovery master fixture")
	}
	defer clear(master)
	var preserved string
	const historyQuery = `SELECT jsonb_build_object(
		'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
		'sessions', (SELECT jsonb_agg(to_jsonb(s) - 'revoked_at' ORDER BY id) FROM sessions s),
		'keys', (SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM application_keys k),
		'clients', (SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM application_key_clients c),
		'devices', (SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM devices d),
		'key_devices', (SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM application_key_devices d),
		'activity', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM activity_entries a))::text`
	if err := pool.QueryRow(ctx, historyQuery).Scan(&preserved); err != nil {
		t.Fatal("snapshot identity history")
	}
	before := applicationKeySnapshot(t, ctx, pool)
	var previousRevocation time.Time
	if err := pool.QueryRow(ctx, "SELECT revoked_at FROM sessions WHERE id = $1", revoked.CredentialID).Scan(&previousRevocation); err != nil {
		t.Fatal("read prior revocation")
	}
	tx := applicationKeyBackupTestTx(t, ctx, pool, false)
	if _, err := identity.ValidateApplicationKeyRecovery(ctx, tx, master); err != nil {
		t.Fatal(err)
	}
	if count, err := identity.RevokeRecoveredCredentials(ctx, tx); err != nil || count != 3 {
		t.Fatalf("recovery did not revoke every credential kind: count %d, error %v", count, err)
	}
	if count, err := identity.RevokeRecoveredCredentials(ctx, tx); err != nil || count != 0 {
		t.Fatalf("repeated recovery revocation was not idempotent: count %d, error %v", count, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if after := applicationKeySnapshot(t, ctx, pool); after != before {
		t.Fatal("recovery revocation escaped the caller transaction")
	}
	tx = applicationKeyBackupTestTx(t, ctx, pool, false)
	if _, err := identity.ValidateApplicationKeyRecovery(ctx, tx, master); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.RevokeRecoveredCredentials(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var afterHistory string
	var afterRevocation time.Time
	var unrevoked int
	if err := pool.QueryRow(ctx, historyQuery).Scan(&afterHistory); err != nil || afterHistory != preserved {
		t.Fatal("recovery revocation changed retained identity history")
	}
	if err := pool.QueryRow(ctx, "SELECT revoked_at FROM sessions WHERE id = $1", revoked.CredentialID).Scan(&afterRevocation); err != nil || !afterRevocation.Equal(previousRevocation) {
		t.Fatal("recovery revocation changed an existing revocation timestamp")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Fatal("recovered credentials remain active")
	}
	if _, err := store.Resolve(ctx, login.Token, "emby"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("recovered user token remains authorized")
	}
	if _, err := store.ResolveEmby(ctx, active.Token); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("recovered application token remains authorized")
	}
	if _, err := store.Authenticate(ctx, actor.User.Name, "administrator-password", identity.Client{}, "admin"); err != nil {
		t.Fatalf("recovery changed the retained account password: %v", err)
	}
}
