//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func itemCapabilityTestActor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) identity.Principal {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("item-capability:" + id))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,device_id,expires_at)
		VALUES($1,$2,$3,'emby','capability-device',now()+interval '1 hour')`, id, userID, digest[:]); err != nil {
		t.Fatal(err)
	}
	return identity.Principal{Kind: "emby", SessionID: id, User: identity.User{ID: userID}, PeerIP: "127.0.0.1"}
}

func itemCapabilityTestRead(t *testing.T, fixture mediaSourceFixture, actor identity.Principal, id string) ItemCapabilities {
	t.Helper()
	result, err := fixture.store.ItemCapabilitiesFor(fixture.ctx, actor, []string{id, id, "1", "missing-item", VirtualRootItemID})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"1", "missing-item", VirtualRootItemID} {
		if result[absent] != (ItemCapabilities{}) {
			t.Fatalf("non-item %q received capabilities: %+v", absent, result[absent])
		}
	}
	return result[id]
}

func TestItemCapabilitiesReflectCurrentPermissionsWithoutOpeningMedia(t *testing.T) {
	probe := &libraryFixtureProber{}
	fixture := mediaSourceTestCatalog(t, probe)
	actor := itemCapabilityTestActor(t, fixture.ctx, fixture.pool, fixture.userID)
	before := len(probe.calls())
	got := itemCapabilityTestRead(t, fixture, actor, fixture.item.ID)
	if got.CanDelete || !got.CanDownload {
		t.Fatalf("default capabilities = %+v", got)
	}
	// The response is a catalog capability, not a live-storage availability probe.
	if err := os.Rename(fixture.path, fixture.path+".unavailable"); err != nil {
		t.Fatal(err)
	}
	got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID)
	if !got.CanDownload || len(probe.calls()) != before {
		t.Fatalf("capability projection performed media observation: %+v", got)
	}
	if err := os.Rename(fixture.path+".unavailable", fixture.path); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_administrator=true,
		policy=policy||'{"EnableContentDownloading":false,"EnableContentDeletion":false}'::jsonb WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); got != (ItemCapabilities{}) {
		t.Fatalf("administrator bypassed explicit operation policy: %+v", got)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_administrator=false,
		policy=policy||'{"EnableContentDownloading":true,"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); !got.CanDownload {
		t.Fatal("playback permission was incorrectly required for downloading")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||'{"RestrictedFeatures":["goby_downloads"]}'::jsonb WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); got.CanDownload {
		t.Fatal("download feature restriction was ignored")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||'{"EnableAllFolders":false,
		"EnabledFolders":[],"RestrictedFeatures":[],"EnableContentDeletion":true}'::jsonb WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media='{"ProbeVersion":"invalid","Streams":[{}]}'::jsonb WHERE id=$1`, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); got != (ItemCapabilities{}) {
		t.Fatalf("hidden malformed source acquired capabilities: %+v", got)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ItemCapabilitiesFor(fixture.ctx, actor, []string{fixture.item.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked actor retained capability authority: %v", err)
	}
}

func TestItemCapabilitiesRequireIndependentBoundSourceForPhysicalDeletion(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := itemCapabilityTestActor(t, fixture.ctx, fixture.pool, fixture.userID)
	var bound bool
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT bool_and(storage_binding IS NOT NULL) FROM library_roots WHERE library_id=$1`, fixture.library.ID).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if !bound {
		t.Skip("positive physical deletion capability requires persistent root binding support")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||jsonb_build_object(
		'EnableContentDeletionFromFolders',jsonb_build_array($2::text)) WHERE id=$1`, fixture.userID, fixture.library.ID); err != nil {
		t.Fatal(err)
	}
	got := itemCapabilityTestRead(t, fixture, actor, fixture.item.ID)
	if !got.CanDelete || !got.CanDownload {
		t.Fatalf("bound source with scoped deletion grant = %+v", got)
	}
	if paths, err := fixture.store.MediaDeletionInfo(fixture.ctx, actor, fixture.item.ID); err != nil || len(paths) != 1 {
		t.Fatalf("capability disagreed with deletion target contract: %v, %v", paths, err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES('capability-dependent',$1,$2,'Dependent','dependent','Folder',true)`, fixture.library.ID, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); got.CanDelete || !got.CanDownload {
		t.Fatalf("dependent media received a destructive capability: %+v", got)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `DELETE FROM items WHERE id='capability-dependent'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO scan_jobs(id,library_id,status) VALUES('capability-queued-scan',$1,'Queued')`, fixture.library.ID); err != nil {
		t.Fatal(err)
	}
	if got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); got.CanDelete || !got.CanDownload {
		t.Fatalf("scan conflict did not fence deletion independently: %+v", got)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `DELETE FROM scan_jobs WHERE id='capability-queued-scan'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET storage_binding=NULL,bound_at=NULL,bound_by=NULL WHERE library_id=$1`, fixture.library.ID); err != nil {
		t.Fatal(err)
	}
	if got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); got.CanDelete || !got.CanDownload {
		t.Fatalf("unbound root did not disable deletion independently: %+v", got)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=jsonb_set(media,'{ProbeVersion}','0'::jsonb) WHERE id=$1`, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if got = itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); got != (ItemCapabilities{}) {
		t.Fatalf("stale source received a media capability: %+v", got)
	}
}

func TestItemCapabilitiesSeparateCollectionOwnershipFromEditingShares(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	owner := itemCapabilityTestActor(t, fixture.ctx, fixture.pool, fixture.userID)
	libraryIntegrationUser(t, fixture.ctx, fixture.pool, "capability-editor", false, true, nil)
	editor := itemCapabilityTestActor(t, fixture.ctx, fixture.pool, "capability-editor")
	playlist, err := fixture.store.CreateCollection(WithCollectionActor(fixture.ctx, owner), Subject{UserID: fixture.userID}, PlaylistKind,
		CollectionInput{Name: "Capability playlist", ItemIDs: []string{fixture.item.ID}})
	if err != nil {
		t.Fatal(err)
	}
	shares := []CollectionShare{{UserID: editor.User.ID, CanEdit: true}}
	if _, err := fixture.store.UpdateCollection(WithCollectionActor(fixture.ctx, owner), Subject{UserID: fixture.userID}, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares}); err != nil {
		t.Fatal(err)
	}
	if got := itemCapabilityTestRead(t, fixture, owner, playlist.ID); !got.CanDelete || got.CanDownload {
		t.Fatalf("owner collection capabilities = %+v", got)
	}
	if got := itemCapabilityTestRead(t, fixture, editor, playlist.ID); got != (ItemCapabilities{}) {
		t.Fatalf("editing share granted collection deletion: %+v", got)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_administrator=true WHERE id=$1`, editor.User.ID); err != nil {
		t.Fatal(err)
	}
	if got := itemCapabilityTestRead(t, fixture, editor, playlist.ID); !got.CanDelete || got.CanDownload {
		t.Fatalf("current administrator collection capabilities = %+v", got)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE media_collections SET is_locked=true WHERE item_id=$1`, playlist.ID); err != nil {
		t.Fatal(err)
	}
	if got := itemCapabilityTestRead(t, fixture, editor, playlist.ID); got.CanDelete {
		t.Fatal("administrator bypassed a locked collection")
	}
}

func TestItemCapabilitiesRetainIndependentApplicationCredentialAuthority(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	subject := seedCatalogApplicationKey(t, fixture.ctx, fixture.pool, "capability-application", true)
	var keyID int64
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT id FROM application_keys WHERE credential_id=$1`, subject.ApplicationCredentialID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	client := "capability-application-client"
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO application_key_clients(id,credential_id,client_name,device_id,device_name,client_version)
		VALUES($1,$2,'Capabilities','capability-device','Capability test','1')`, client, subject.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	actor := identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: subject.ApplicationCredentialID, ApplicationKeyID: keyID, ClientSessionID: client}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_disabled=true,policy=policy||'{"EnableContentDownloading":false}'::jsonb WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if got := itemCapabilityTestRead(t, fixture, actor, fixture.item.ID); !got.CanDownload {
		t.Fatal("unrelated account policy removed application download authority")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ItemCapabilitiesFor(fixture.ctx, actor, []string{fixture.item.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked application key retained capabilities: %v", err)
	}
}
