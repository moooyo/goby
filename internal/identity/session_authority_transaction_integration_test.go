package identity_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

func TestRevalidateSessionAuthorityInTransactionUsesCallerUncommittedState(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, previous := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{DeviceID: "transaction-device"}, "emby")
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `UPDATE users SET name='Uncommitted authority name', is_administrator=false,
		policy='{"EnableMediaPlayback":false}'::jsonb, configuration='{"FuturePreference":"uncommitted"}'::jsonb WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	full, err := identity.RevalidateSessionInTransaction(ctx, tx, previous)
	if err != nil || full.User.Name != "Uncommitted authority name" || full.User.IsAdministrator || len(full.User.Configuration) == 0 {
		t.Fatalf("full caller-owned transaction lost current account facts: %v", err)
	}
	narrow, err := identity.RevalidateSessionAuthorityInTransaction(ctx, tx, previous)
	if err != nil || !reflect.DeepEqual(narrow, sessionAuthorityProjection(full)) {
		t.Fatalf("transaction authority did not match the caller's uncommitted account: %v", err)
	}
	outside, err := store.RevalidateSessionAuthority(ctx, previous)
	if err != nil || !outside.User.IsAdministrator {
		t.Fatalf("authority read committed the caller's account changes: %v", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", previous.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.RevalidateSessionAuthorityInTransaction(ctx, tx, previous); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("transaction authority missed its own credential revocation: %v", err)
	}
	if _, err := store.RevalidateSessionAuthority(ctx, previous); err != nil {
		t.Fatalf("authority read committed or leaked an uncommitted revocation: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevalidateSessionAuthority(ctx, previous); err != nil {
		t.Fatalf("caller rollback did not retain the original credential: %v", err)
	}
}

func TestRevalidateSessionAuthorityInTransactionPreservesApplicationBinding(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Transactional authority application")
	previous := bindApplicationClient(t, ctx, store, key, identity.Client{
		Name: "Transactional authority client", DeviceID: "transaction-application",
		Device: "Transactional authority player", Version: "1",
	})
	previous.PeerIP = "192.0.2.45"
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	full, err := identity.RevalidateSessionInTransaction(ctx, tx, previous)
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := identity.RevalidateSessionAuthorityInTransaction(ctx, tx, previous)
	if err != nil || !reflect.DeepEqual(narrow, full) || !narrow.IsApplicationKey() || narrow.PeerIP != previous.PeerIP {
		t.Fatalf("transaction application authority changed its complete binding: %v", err)
	}
	for _, changed := range []identity.Principal{
		func() identity.Principal { p := previous; p.ApplicationKeyID++; return p }(),
		func() identity.Principal { p := previous; p.ClientSessionID = "missing-client"; return p }(),
	} {
		if _, err := identity.RevalidateSessionAuthorityInTransaction(ctx, tx, changed); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("transaction authority accepted an invalid application binding: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", key.CredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.RevalidateSessionAuthorityInTransaction(ctx, tx, previous); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("transaction authority ignored its uncommitted application revocation: %v", err)
	}
	if _, err := store.RevalidateSessionAuthority(ctx, previous); err != nil {
		t.Fatalf("transaction authority completed the caller transaction: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}
