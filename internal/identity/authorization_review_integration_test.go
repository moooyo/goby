package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

func TestStoreApplicationKeyMetadataReadsDoNotWaitForManagementLock(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, native, "Metadata lock test")
	application := applicationKeyPrincipal(t, ctx, store, key)
	_, emby := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(4919415424202458192)); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []identity.Principal{native, emby, application} {
		t.Run(actor.Kind, func(t *testing.T) {
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			page, err := store.ListApplicationKeys(readCtx, actor, identity.ApplicationKeyFilter{Limit: 1})
			if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].Token != "" {
				t.Fatalf("metadata list waited for management or exposed a secret: %v", err)
			}
			metadata, err := store.GetApplicationKey(readCtx, actor, key.ID, false)
			if err != nil || metadata.ID != key.ID || metadata.Token != "" {
				t.Fatalf("metadata read waited for management or exposed a secret: %v", err)
			}
		})
	}
	operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := store.ListApplicationKeys(operationCtx, native, identity.ApplicationKeyFilter{RevealTokens: true})
		finished <- err
	}()
	waitManagedBlockedQuery(t, operationCtx, pool, blockerPID, "pg_advisory_xact_lock", done)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("reveal failed after management admission: %v", err)
		}
	case <-operationCtx.Done():
		t.Fatal("reveal did not finish after releasing management admission")
	}
}

func TestStoreApplicationKeyMetadataReadsReauthorizeAfterActorWait(t *testing.T) {
	for _, kind := range []string{"admin", "application_key"} {
		t.Run(kind, func(t *testing.T) {
			ctx, pool, store, actor, _ := applicationKeyTestStore(t)
			key := issueApplicationKey(t, ctx, store, actor, "Metadata revoked actor")
			if kind == "application_key" {
				actor = applicationKeyPrincipal(t, ctx, store, key)
			}
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			var locked string
			statement, id := "SELECT id FROM users WHERE id = $1 FOR UPDATE", actor.User.ID
			if actor.IsApplicationKey() {
				statement, id = "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", actor.SessionID
			}
			if err := blocker.QueryRow(ctx, statement, id).Scan(&locked); err != nil {
				t.Fatal(err)
			}
			operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, err := store.ListApplicationKeys(operationCtx, actor, identity.ApplicationKeyFilter{})
				finished <- err
			}()
			waitManagedBlockedQuery(t, operationCtx, pool, blockerPID, "SELECT id FROM", done)
			if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("metadata reader accepted revoked actor after a wait: %v", err)
				}
			case <-operationCtx.Done():
				t.Fatal("metadata read did not finish after releasing its actor")
			}
		})
	}
}

func TestStoreManagedSessionRevocationAllowsSiblingAuthorizationWhileWaiting(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	_, actorSibling := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	viewer, err := store.CreateUser(ctx, "Revocation Target", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	_, targetSibling := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	var locked string
	if err := blocker.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", target.SessionID).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	results, done := revokeManagedSessionAsync(operationCtx, store, actor, target.SessionID)
	waitManagedBlockedQuery(t, operationCtx, pool, blockerPID, "SELECT id, user_id, kind, revoked_at FROM sessions", done)
	for _, sibling := range []identity.Principal{actorSibling, targetSibling} {
		read, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer read.Rollback(context.Background())
		// Playback authorization takes these same account and credential
		// SHARE locks before refreshing a sibling's current authority.
		if err := read.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR SHARE NOWAIT", sibling.User.ID).Scan(&locked); err != nil {
			t.Fatalf("single-session revocation blocked sibling account authority: %v", err)
		}
		if err := read.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR SHARE NOWAIT", sibling.SessionID).Scan(&locked); err != nil {
			t.Fatalf("single-session revocation blocked sibling credential authority: %v", err)
		}
		if _, err := identity.RevalidateSessionInTransaction(ctx, read, sibling); err != nil {
			t.Fatalf("sibling authorization failed during target revocation wait: %v", err)
		}
		if err := read.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mutation, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mutation.Rollback(context.Background())
	err = mutation.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE NOWAIT", viewer.ID).Scan(&locked)
	var conflict *pgconn.PgError
	if !errors.As(err, &conflict) || conflict.Code != "55P03" {
		t.Fatalf("target account mutation was not excluded while revocation waited: %v", err)
	}
	if err := mutation.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitManagedSessionRevocation(t, operationCtx, results)
	if result.err != nil || result.revocation.SessionID != target.SessionID {
		t.Fatalf("target revocation did not finish after its row was released: %v", result.err)
	}
}
