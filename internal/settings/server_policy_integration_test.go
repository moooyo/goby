package settings

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestServerPolicyCommitsCASAuthorityAndRuntimeRevocation(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	store, err := NewServerPolicy(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.Get(ctx, actor)
	if err != nil || !reflect.DeepEqual(initial.Values, DefaultServerPolicy()) {
		t.Fatal("migration defaults were not published", err)
	}
	remote, release, allowed := store.Watch(context.Background(), "203.0.113.17")
	if !allowed {
		t.Fatal("default remote access was not retained")
	}
	defer release()
	values := initial.Values
	values.EnableRemoteAccess = false
	values.UICulture = "en-US"
	request := ServerPolicyUpdate{Revision: initial.Revision, Values: values, ClientAddress: "203.0.113.17"}
	if _, err := store.Update(ctx, actor, request); err == nil {
		t.Fatal("self-disconnecting update was accepted without explicit confirmation")
	}
	if remote.Err() != nil || store.Snapshot().Revision != initial.Revision {
		t.Fatal("rejected update altered the runtime boundary")
	}
	request.ConfirmDisconnect = true
	committed, err := store.Update(ctx, actor, request)
	if err != nil || committed.Revision != initial.Revision+1 || committed.Values.EnableRemoteAccess {
		t.Fatal("confirmed policy did not commit", err)
	}
	if !errors.Is(context.Cause(remote), ErrServerPolicyDenied) {
		t.Fatal("commit failed to revoke an existing remote operation")
	}
	reopen := request
	reopen.Revision = committed.Revision
	reopen.Values.EnableRemoteAccess = true
	if _, err := store.Update(ctx, actor, reopen); !errors.Is(err, ErrServerPolicyDenied) {
		t.Fatal("a previously admitted remote writer reopened its denied boundary", err)
	}
	request.ClientAddress = "127.0.0.1"
	if _, err := store.Update(ctx, actor, request); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale revision did not conflict", err)
	}
	reloaded, err := NewServerPolicy(ctx, owner)
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot(), committed) {
		t.Fatal("restart did not recover the committed policy", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled=true WHERE id=$1", actor.Principal.User.ID); err != nil {
		t.Fatal(err)
	}
	request.Revision = committed.Revision
	request.Values.EnableRemoteAccess = true
	if _, err := store.Update(ctx, actor, request); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("stale administrator identity changed the global boundary", err)
	}
	if store.Snapshot().Revision != committed.Revision || store.Snapshot().Values.EnableRemoteAccess {
		t.Fatal("revoked authority changed published settings")
	}
}
