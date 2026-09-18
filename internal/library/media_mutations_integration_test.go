//go:build linux

package library

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func mediaDeletionTestActor(t *testing.T, fixture mediaSourceFixture) identity.Principal {
	t.Helper()
	var bound bool
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT bool_and(storage_binding IS NOT NULL) FROM library_roots WHERE library_id=$1`, fixture.library.ID).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if !bound {
		t.Skip("safe deletion requires persistent root binding support")
	}
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,device_id,expires_at)
		VALUES($1,$2,$3,'emby','deletion-device',now()+interval '1 hour')`, id, fixture.userID, token[:]); err != nil {
		t.Fatal(err)
	}
	return identity.Principal{Kind: "emby", SessionID: id, User: identity.User{ID: fixture.userID}, Client: identity.Client{DeviceID: "deletion-device"}, PeerIP: "127.0.0.1"}
}

func TestMediaDeletionRequiresPermissionAndRemovesOnlyItsExactFile(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := mediaDeletionTestActor(t, fixture)
	if err := fixture.store.DeleteMediaFor(fixture.ctx, actor, fixture.item.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled content deletion accepted: %v", err)
	}
	sibling := filepath.Join(filepath.Dir(fixture.path), "unrelated.txt")
	if err := os.WriteFile(sibling, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||jsonb_build_object('EnableContentDeletionFromFolders',jsonb_build_array($2::text)) WHERE id=$1`, fixture.userID, fixture.library.ID); err != nil {
		t.Fatal(err)
	}
	paths, err := fixture.store.MediaDeletionInfo(fixture.ctx, actor, fixture.item.ID)
	if err != nil || len(paths) != 1 || paths[0] != fixture.path {
		t.Fatalf("delete info=%v %v", paths, err)
	}
	if err := fixture.store.DeleteMediaFor(fixture.ctx, actor, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists: %v", err)
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "retained" {
		t.Fatalf("sibling changed: %v", err)
	}
	var present, pending bool
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE id=$1),EXISTS(SELECT 1 FROM media_deletion_operations WHERE item_id=$1)`, fixture.item.ID).Scan(&present, &pending); err != nil || present || pending {
		t.Fatalf("committed deletion left item/journal: %t %t %v", present, pending, err)
	}
}

func TestMediaDeletionRevocationAfterStagingRestoresInsteadOfCommitting(t *testing.T) {
	for _, change := range []string{"policy", "credential"} {
		t.Run(change, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			actor := mediaDeletionTestActor(t, fixture)
			if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||'{"EnableContentDeletion":true}' WHERE id=$1`, fixture.userID); err != nil {
				t.Fatal(err)
			}
			staged, release := make(chan struct{}), make(chan struct{})
			result := make(chan error, 1)
			go func() {
				result <- fixture.store.performManagedFileDeletionWithHook(fixture.ctx, actor, fixture.item.ID, "media", -1, func() { close(staged); <-release })
			}()
			select {
			case <-staged:
			case <-time.After(10 * time.Second):
				t.Fatal("deletion did not reach its staged boundary")
			}
			if _, err := os.Stat(fixture.path); !errors.Is(err, os.ErrNotExist) {
				close(release)
				t.Fatalf("source was not staged: %v", err)
			}
			if change == "policy" {
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||'{"EnableContentDeletion":false}' WHERE id=$1`, fixture.userID); err != nil {
					close(release)
					t.Fatal(err)
				}
			} else if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, actor.SessionID); err != nil {
				close(release)
				t.Fatal(err)
			}
			close(release)
			if err := <-result; !errors.Is(err, ErrForbidden) {
				t.Fatalf("revoked deletion result=%v", err)
			}
			if data, err := os.ReadFile(fixture.path); err != nil || string(data) != fixture.contents {
				t.Fatalf("compensation did not restore original bytes: %v", err)
			}
			var present, pending bool
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE id=$1),EXISTS(SELECT 1 FROM media_deletion_operations WHERE item_id=$1)`, fixture.item.ID).Scan(&present, &pending); err != nil || !present || pending {
				t.Fatalf("revocation committed deletion or stranded recoverable journal: %t %t %v", present, pending, err)
			}
		})
	}
}

func TestPreparedDeletionBlocksScansAndExplicitRetryRestores(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := mediaDeletionTestActor(t, fixture)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||'{"EnableContentDeletion":true}' WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	tx, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	target, err := readFileDeletionTarget(fixture.ctx, tx, actor, fixture.item.ID, "media", -1, false)
	rollback(tx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	host, err := mediaDeletionHostIdentity()
	if err != nil {
		t.Fatal(err)
	}
	target.File.StageName = ".goby-delete-" + id
	op := mediaDeletionOperation{ID: id, State: "prepared", ActorID: actor.User.ID, CredentialID: actor.SessionID, OriginHost: host, Target: target}
	capture, err := fixture.store.prepareFileDeletion(fixture.ctx, target.File)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareDeletionJournal(fixture.ctx, actor, op); err != nil {
		t.Fatal(err)
	}
	if _, _, err := capture.Stage(fixture.ctx); err != nil {
		capture.Close()
		t.Fatal(err)
	}
	capture.Close()
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO scan_jobs(id,library_id,status) VALUES('blocked-by-deletion',$1,'Queued')`, fixture.library.ID); err == nil {
		t.Fatal("scan was admitted while a source was staged")
	}
	if err := fixture.store.DeleteMediaFor(fixture.ctx, actor, fixture.item.ID); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("prepared recovery must restore and require a rescan: %v", err)
	}
	if data, err := os.ReadFile(fixture.path); err != nil || string(data) != fixture.contents {
		t.Fatalf("recovery lost original: %v", err)
	}
}

func TestMediaDeletionRejectsChangedSourceAndDependentItems(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := mediaDeletionTestActor(t, fixture)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||'{"EnableContentDeletion":true}' WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES('dependent',$1,$2,'Dependent','Dependent','Video',false)`, fixture.library.ID, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.DeleteMediaFor(fixture.ctx, actor, fixture.item.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dependent media would cascade: %v", err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `DELETE FROM items WHERE id='dependent'`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, []byte("replacement-media"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.DeleteMediaFor(context.Background(), actor, fixture.item.ID); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("changed source was accepted: %v", err)
	}
	if data, err := os.ReadFile(fixture.path); err != nil || string(data) != "replacement-media" {
		t.Fatal("replacement was removed")
	}
}
