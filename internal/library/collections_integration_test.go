package library

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func collectionTestMedia(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO libraries(id,name,collection_type) VALUES ('collection-source-a','Source A','music'),('collection-source-b','Source B','music')`,
		`INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES ('collection-album','collection-source-a','Album','album','MusicAlbum',true)`,
		`INSERT INTO items(id,library_id,parent_id,name,sort_name,type) VALUES ('collection-track-a','collection-source-a','collection-album','Track A','a','Audio'),('collection-track-b','collection-source-a','collection-album','Track B','b','Audio'),('collection-track-c','collection-source-b',NULL,'Track C','c','Audio')`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("insert collection fixture: %v", err)
		}
	}
}

func TestCollectionEntriesPersistAcrossRestartAndKeepDuplicateIdentity(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	subject := Subject{UserID: userID}
	var notifications []CatalogNotification
	store.SetCatalogChangeListener(func(change CatalogNotification) { notifications = append(notifications, change) })
	playlist, err := store.CreateCollection(ctx, subject, PlaylistKind, CollectionInput{Name: "Ordered playlist", MediaType: "Audio", ItemIDs: []string{"collection-track-a", "collection-track-a", "collection-track-c"}})
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	result, err := store.CollectionItems(ctx, subject, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || result.TotalRecordCount != 3 || len(result.Items) != 3 {
		t.Fatalf("initial entries = %#v, %v", result, err)
	}
	first, second, third := result.Items[0].PlaylistItemID, result.Items[1].PlaylistItemID, result.Items[2].PlaylistItemID
	if first == "" || first == second || second == third {
		t.Fatalf("duplicates did not receive distinct entry IDs: %q %q %q", first, second, third)
	}
	preview, err := store.PreviewCollectionItems(ctx, subject, playlist.ID, PlaylistKind, []string{"collection-track-a", "collection-track-b"})
	if err != nil || preview.ItemCount != 2 || !preview.ContainsDuplicates {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	if err := store.MoveCollectionEntry(ctx, subject, playlist.ID, third, 0); err != nil {
		t.Fatalf("move last entry: %v", err)
	}
	if err := store.RemoveCollectionItems(ctx, subject, playlist.ID, PlaylistKind, []string{first}); err != nil {
		t.Fatalf("remove one occurrence: %v", err)
	}
	result, err = store.CollectionItems(ctx, subject, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || len(result.Items) != 2 {
		t.Fatalf("read edited playlist: %#v, %v", result, err)
	}
	if got := []string{result.Items[0].PlaylistItemID, result.Items[1].PlaylistItemID}; !reflect.DeepEqual(got, []string{third, second}) {
		t.Fatalf("entry IDs/order changed: %v", got)
	}
	page, err := store.QueryItems(ctx, Query{UserID: userID, ParentID: playlist.ID, StartIndex: 1, Limit: 1})
	if err != nil || page.TotalRecordCount != 2 || len(page.Items) != 1 || page.Items[0].PlaylistItemID != second {
		t.Fatalf("Items parent paging = %#v, %v", page, err)
	}
	if len(notifications) != 3 {
		t.Fatalf("committed mutation notifications = %d", len(notifications))
	}
	for _, notification := range notifications {
		if !notification.Resync || len(notification.Changes) != 0 {
			t.Fatalf("private identities escaped notification: %#v", notification)
		}
	}
	if err := store.Close(ctx); err != nil {
		t.Fatalf("close first store: %v", err)
	}
	restarted, err := New(pool, &libraryFixtureProber{}, []string{root})
	if err != nil {
		t.Fatalf("restart store: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := restarted.Close(closeCtx); err != nil {
			t.Errorf("close restarted store: %v", err)
		}
	})
	result, err = restarted.CollectionItems(ctx, subject, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || len(result.Items) != 2 || result.Items[0].PlaylistItemID != third || result.Items[1].PlaylistItemID != second {
		t.Fatalf("restart lost entries: %#v, %v", result, err)
	}
	libraries, err := restarted.ListLibraries(ctx)
	if err != nil || len(libraries) != 2 {
		t.Fatalf("internal library leaked: %#v, %v", libraries, err)
	}
	if _, err := restarted.GetLibrary(ctx, CollectionsLibraryID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reserved library detail = %v", err)
	}
	if err := restarted.DeleteLibrary(ctx, CollectionsLibraryID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reserved library deletion = %v", err)
	}
	if _, err := restarted.StartScan(ctx, CollectionsLibraryID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reserved library scan = %v", err)
	}
}

func TestCollectionSharingScopesMembersAndRollsBackUnauthorizedBatches(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "collection-reader", false, false, []string{"collection-source-a"})
	libraryIntegrationUser(t, ctx, pool, "collection-editor", false, false, []string{"collection-source-a"})
	owner, reader, editor := Subject{UserID: ownerID}, Subject{UserID: "collection-reader"}, Subject{UserID: "collection-editor"}
	playlist, err := store.CreateCollection(ctx, owner, PlaylistKind, CollectionInput{Name: "Shared playlist", ItemIDs: []string{"collection-track-a", "collection-track-c"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetCollection(ctx, reader, playlist.ID, PlaylistKind); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private collection read = %v", err)
	}
	private, err := store.QueryItems(ctx, Query{UserID: reader.UserID, Recursive: true, IncludeItemTypes: []string{PlaylistKind}, Limit: 100})
	if err != nil || private.TotalRecordCount != 0 {
		t.Fatalf("private collection list = %#v, %v", private, err)
	}
	shares := []CollectionShare{{UserID: reader.UserID}, {UserID: editor.UserID, CanEdit: true}}
	if _, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares}); err != nil {
		t.Fatal(err)
	}
	info, err := store.GetCollection(ctx, reader, playlist.ID, PlaylistKind)
	if err != nil || info.ItemCount != 1 || len(info.Shares) != 0 {
		t.Fatalf("reader collection = %#v, %v", info, err)
	}
	items, err := store.CollectionItems(ctx, reader, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || items.TotalRecordCount != 1 || items.Items[0].ID != "collection-track-a" {
		t.Fatalf("cross-library members leaked: %#v, %v", items, err)
	}
	if _, err := store.AddCollectionItems(ctx, reader, playlist.ID, PlaylistKind, []string{"collection-track-b"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only share edited entries: %v", err)
	}
	if _, err := store.AddCollectionItems(ctx, editor, playlist.ID, PlaylistKind, []string{"collection-track-b", "collection-track-c"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized batch = %v", err)
	}
	items, err = store.CollectionItems(ctx, owner, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || items.TotalRecordCount != 2 {
		t.Fatalf("failed batch partially committed: %#v, %v", items, err)
	}
	if added, err := store.AddCollectionItems(ctx, editor, playlist.ID, PlaylistKind, []string{"collection-album"}); err != nil || added != 2 {
		t.Fatalf("folder expansion = %d, %v", added, err)
	}
	containing, err := store.QueryItems(ctx, Query{UserID: reader.UserID, Recursive: true, IncludeItemTypes: []string{PlaylistKind}, ListItemIds: []string{"collection-album"}, Limit: 100})
	if err != nil || containing.TotalRecordCount != 1 || containing.Items[0].ID != playlist.ID {
		t.Fatalf("album containing-playlists query = %#v, %v", containing, err)
	}
	containing, err = store.QueryItems(ctx, Query{UserID: reader.UserID, Recursive: true, IncludeItemTypes: []string{PlaylistKind}, ListItemIds: []string{"collection-track-c"}, Limit: 100})
	if err != nil || containing.TotalRecordCount != 0 {
		t.Fatalf("hidden source revealed containing playlist: %#v, %v", containing, err)
	}
	locked := true
	if _, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{IsLocked: &locked}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddCollectionItems(ctx, owner, playlist.ID, PlaylistKind, []string{"collection-track-a"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("locked collection changed: %v", err)
	}
	if err := store.DeleteCollection(ctx, owner, playlist.ID, PlaylistKind); !errors.Is(err, ErrForbidden) {
		t.Fatalf("locked collection deleted: %v", err)
	}
	locked = false
	if _, err := store.UpdateCollection(ctx, editor, playlist.ID, PlaylistKind, CollectionPatch{IsLocked: &locked}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor unlocked collection: %v", err)
	}
	shares = []CollectionShare{}
	if _, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{IsLocked: &locked, Shares: &shares}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CollectionItems(ctx, editor, playlist.ID, PlaylistKind, 0, 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked share still readable: %v", err)
	}
	if _, err := store.CreateCollection(ctx, reader, PlaylistKind, CollectionInput{Name: "Must roll back", ItemIDs: []string{"collection-track-a", "collection-track-c"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized creation = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE name='Must roll back'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed creation left item: %d, %v", count, err)
	}
}

func TestBoxSetsRetainMediaAndCascadeCollectionState(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	owner := Subject{UserID: ownerID}
	box, err := store.CreateCollection(ctx, owner, BoxSetKind, CollectionInput{Name: "Box", ItemIDs: []string{"collection-track-a", "collection-track-a", "collection-track-c"}})
	if err != nil || box.ItemCount != 2 {
		t.Fatalf("create deduplicated box set = %#v, %v", box, err)
	}
	if count, err := store.AddCollectionItems(ctx, owner, box.ID, BoxSetKind, []string{"collection-track-a"}); err != nil || count != 0 {
		t.Fatalf("box set duplicate append = %d, %v", count, err)
	}
	child, err := store.CreateCollection(ctx, owner, BoxSetKind, CollectionInput{Name: "Child", ParentID: box.ID, ItemIDs: []string{"collection-track-b"}})
	if err != nil {
		t.Fatalf("create child collection: %v", err)
	}
	children, err := store.QueryItems(ctx, Query{UserID: ownerID, ParentID: box.ID, Limit: 100})
	if err != nil || children.TotalRecordCount != 3 {
		t.Fatalf("collection hierarchy = %#v, %v", children, err)
	}
	descendants, err := store.QueryItems(ctx, Query{UserID: ownerID, ParentID: box.ID, Recursive: true, Limit: 100})
	if err != nil || descendants.TotalRecordCount != 4 {
		t.Fatalf("recursive collection hierarchy = %#v, %v", descendants, err)
	}
	if err := store.RemoveCollectionItems(ctx, owner, box.ID, BoxSetKind, []string{child.ID, "collection-track-a"}); err != nil {
		t.Fatalf("remove collection members: %v", err)
	}
	child, err = store.GetCollection(ctx, owner, child.ID, BoxSetKind)
	if err != nil || child.ParentID != "" {
		t.Fatalf("detached child = %#v, %v", child, err)
	}
	if _, err := store.GetItem(ctx, ownerID, "collection-track-a"); err != nil {
		t.Fatalf("membership removal deleted media: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM items WHERE id='collection-track-c'`); err != nil {
		t.Fatal(err)
	}
	box, err = store.GetCollection(ctx, owner, box.ID, BoxSetKind)
	if err != nil || box.ItemCount != 0 {
		t.Fatalf("deleted source left dangling membership: %#v, %v", box, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, ownerID); err != nil {
		t.Fatalf("delete collection owner: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE id=ANY($1::text[])`, []string{box.ID, child.ID}).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("owner deletion left collection projections: %d, %v", remaining, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE id='collection-track-a'`).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("owner deletion removed source media: %d, %v", remaining, err)
	}
}

func TestRemovingSourceLibraryInvalidatesReferencedCollections(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	owner := Subject{UserID: ownerID}
	playlist, err := store.CreateCollection(ctx, owner, PlaylistKind, CollectionInput{Name: "Source references", ItemIDs: []string{"collection-track-a", "collection-track-c"}})
	if err != nil {
		t.Fatal(err)
	}
	var notifications []CatalogNotification
	store.SetCatalogChangeListener(func(notification CatalogNotification) { notifications = append(notifications, notification) })
	if err := store.DeleteLibrary(ctx, "collection-source-a"); err != nil {
		t.Fatalf("delete source library: %v", err)
	}
	if len(notifications) != 1 || !notifications[0].Resync || len(notifications[0].Changes) != 0 {
		t.Fatalf("source deletion notification = %#v", notifications)
	}
	items, err := store.CollectionItems(ctx, owner, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || items.TotalRecordCount != 1 || len(items.Items) != 1 || items.Items[0].ID != "collection-track-c" {
		t.Fatalf("source library removal left stale entries: %#v, %v", items, err)
	}
}

func TestCollectionWritesRevalidateTheBoundEmbyCredential(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at)
		VALUES('collection-credential',$1,decode(repeat('af',32),'hex'),'emby','2020-01-01T00:00:00Z','2030-01-01T00:00:00Z')`, ownerID); err != nil {
		t.Fatal(err)
	}
	actor := identity.Principal{User: identity.User{ID: ownerID}, SessionID: "collection-credential", Kind: "emby", PeerIP: "127.0.0.1"}
	bound := WithCollectionActor(ctx, actor)
	subject := Subject{UserID: ownerID}
	playlist, err := store.CreateCollection(bound, subject, PlaylistKind, CollectionInput{Name: "Credential protected"})
	if err != nil {
		t.Fatalf("create with live credential: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id='collection-credential'`); err != nil {
		t.Fatal(err)
	}
	name := "Must not commit"
	if _, err := store.UpdateCollection(bound, subject, playlist.ID, PlaylistKind, CollectionPatch{Name: &name}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked actor edited collection: %v", err)
	}
	if _, err := store.AddCollectionItems(bound, subject, playlist.ID, PlaylistKind, []string{"collection-track-a"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked actor added membership: %v", err)
	}
	if err := store.DeleteCollection(bound, subject, playlist.ID, PlaylistKind); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked actor deleted collection: %v", err)
	}
	retained, err := store.GetCollection(ctx, subject, playlist.ID, PlaylistKind)
	if err != nil || retained.Name != playlist.Name || retained.ItemCount != 0 {
		t.Fatalf("rejected credential mutation changed collection: %#v, %v", retained, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=NULL,expires_at=clock_timestamp()-interval '1 second' WHERE id='collection-credential'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCollection(bound, subject, PlaylistKind, CollectionInput{Name: "Expired actor"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expired actor created collection: %v", err)
	}
}

func TestRestrictedCollectionFeaturesHideOnlyTheirContainerKind(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	subject := Subject{UserID: ownerID}
	playlist, err := store.CreateCollection(ctx, subject, PlaylistKind, CollectionInput{Name: "Restricted playlist", ItemIDs: []string{"collection-track-a"}})
	if err != nil {
		t.Fatal(err)
	}
	box, err := store.CreateCollection(ctx, subject, BoxSetKind, CollectionInput{Name: "Restricted box", ItemIDs: []string{"collection-track-b"}})
	if err != nil {
		t.Fatal(err)
	}
	key := seedCatalogApplicationKey(t, ctx, pool, "collection-feature-key", true)
	key.UserID = ownerID
	for _, test := range []struct{ feature, kind, blockedID, retainedKind, retainedID string }{
		{identity.FeaturePlaylists, PlaylistKind, playlist.ID, BoxSetKind, box.ID},
		{identity.FeatureCollections, BoxSetKind, box.ID, PlaylistKind, playlist.ID},
	} {
		t.Run(test.feature, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `UPDATE users SET policy=jsonb_set(policy,'{RestrictedFeatures}',to_jsonb($2::text[])) WHERE id=$1`, ownerID, []string{test.feature}); err != nil {
				t.Fatal(err)
			}
			blocked, err := store.QueryItems(ctx, Query{UserID: ownerID, Recursive: true, IncludeItemTypes: []string{test.kind}, Limit: 100})
			if err != nil || blocked.TotalRecordCount != 0 || len(blocked.Items) != 0 {
				t.Fatalf("restricted containers leaked into counts/page: %#v, %v", blocked, err)
			}
			if _, err := store.GetCollection(ctx, subject, test.blockedID, test.kind); !errors.Is(err, ErrNotFound) {
				t.Fatalf("restricted detail = %v", err)
			}
			if _, err := store.GetItemFor(ctx, subject, test.blockedID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("restricted item projection = %v", err)
			}
			if _, err := store.CollectionItems(ctx, subject, test.blockedID, test.kind, 0, 100); !errors.Is(err, ErrNotFound) {
				t.Fatalf("restricted members = %v", err)
			}
			if _, err := store.CreateCollection(ctx, subject, test.kind, CollectionInput{Name: "Must reject"}); !errors.Is(err, ErrForbidden) {
				t.Fatalf("restricted creation = %v", err)
			}
			if _, err := store.AddCollectionItems(ctx, subject, test.blockedID, test.kind, []string{"collection-track-c"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("restricted membership edit = %v", err)
			}
			name := "Must reject"
			if _, err := store.UpdateCollection(ctx, subject, test.blockedID, test.kind, CollectionPatch{Name: &name}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("restricted metadata edit = %v", err)
			}
			if _, err := store.GetCollection(ctx, subject, test.retainedID, test.retainedKind); err != nil {
				t.Fatalf("unrelated collection kind was denied: %v", err)
			}
			media, err := store.QueryItems(ctx, Query{UserID: ownerID, Recursive: true, IncludeItemTypes: []string{"Audio"}, Limit: 100})
			if err != nil || media.TotalRecordCount != 3 {
				t.Fatalf("container restriction changed source visibility: %#v, %v", media, err)
			}
			if _, err := store.GetCollection(ctx, key, test.blockedID, test.kind); err != nil {
				t.Fatalf("target user feature restriction weakened application authority: %v", err)
			}
			created, err := store.CreateCollection(ctx, key, test.kind, CollectionInput{Name: "Application-owned creation"})
			if err != nil {
				t.Fatalf("application credential could not create for a restricted target: %v", err)
			}
			if err := store.DeleteCollection(ctx, key, created.ID, test.kind); err != nil {
				t.Fatalf("application credential could not delete its authorized collection: %v", err)
			}
		})
	}
}

func TestCollectionMembershipMutationsUseTheVisiblePlaylistOrder(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type)
		VALUES('collection-hidden-d','collection-source-b','Hidden D','hidden d','Audio');
		SELECT setval(pg_get_serial_sequence('media_collection_entries','id'),9007199254740992,true)`); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationUser(t, ctx, pool, "collection-visible-editor", false, false, []string{"collection-source-a"})
	owner, editor := Subject{UserID: ownerID}, Subject{UserID: "collection-visible-editor"}
	playlist, err := store.CreateCollection(ctx, owner, PlaylistKind, CollectionInput{Name: "Visible order", ItemIDs: []string{"collection-track-c", "collection-track-a", "collection-hidden-d", "collection-track-b"}})
	if err != nil {
		t.Fatal(err)
	}
	shares := []CollectionShare{{UserID: editor.UserID, CanEdit: true}}
	if _, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares}); err != nil {
		t.Fatal(err)
	}
	all, err := store.CollectionItems(ctx, owner, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || len(all.Items) != 4 {
		t.Fatalf("read owner entries: %#v, %v", all, err)
	}
	hiddenEntry, firstEntry, lastEntry := all.Items[0].PlaylistItemID, all.Items[1].PlaylistItemID, all.Items[3].PlaylistItemID
	if hiddenEntry != "9007199254740993" || firstEntry != "9007199254740994" || lastEntry != "9007199254740996" {
		t.Fatalf("fixture lost exact bigint entry IDs: %q %q %q", hiddenEntry, firstEntry, lastEntry)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(id::text,item_id,position) ORDER BY position)::text FROM media_collection_entries WHERE collection_id=$1`, playlist.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	if err := store.MoveCollectionEntry(ctx, editor, playlist.ID, hiddenEntry, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden entry move = %v", err)
	}
	if err := store.MoveCollectionEntry(ctx, editor, playlist.ID, firstEntry, 2); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("index beyond visible count = %v", err)
	}
	if err := store.RemoveCollectionItems(ctx, editor, playlist.ID, PlaylistKind, []string{hiddenEntry}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden entry removal = %v", err)
	}
	if err := store.RemoveCollectionItems(ctx, editor, playlist.ID, PlaylistKind, []string{firstEntry, hiddenEntry}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mixed visible/hidden removal = %v", err)
	}
	if snapshot() != before {
		t.Fatal("rejected visibility probe changed membership or order")
	}
	if _, err := store.PreviewCollectionItems(ctx, editor, playlist.ID, PlaylistKind, []string{"collection-track-c"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("preview disclosed hidden source membership: %v", err)
	}
	preview, err := store.PreviewCollectionItems(ctx, editor, playlist.ID, PlaylistKind, []string{"collection-track-a"})
	if err != nil || preview.ItemCount != 1 || !preview.ContainsDuplicates {
		t.Fatalf("authorized duplicate preview = %#v, %v", preview, err)
	}
	if err := store.MoveCollectionEntry(ctx, editor, playlist.ID, firstEntry, 1); err != nil {
		t.Fatalf("move first visible entry down: %v", err)
	}
	visible, err := store.CollectionItems(ctx, editor, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || visible.TotalRecordCount != 2 || len(visible.Items) != 2 || visible.Items[0].PlaylistItemID != lastEntry || visible.Items[1].PlaylistItemID != firstEntry {
		t.Fatalf("visible down move was not applied: %#v, %v", visible, err)
	}
	all, err = store.CollectionItems(ctx, owner, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || len(all.Items) != 4 {
		t.Fatalf("read full moved entries: %#v, %v", all, err)
	}
	var actual []string
	for _, entry := range all.Items {
		actual = append(actual, entry.ID)
	}
	if !reflect.DeepEqual(actual, []string{"collection-track-c", "collection-hidden-d", "collection-track-b", "collection-track-a"}) {
		t.Fatalf("move changed hidden relative order: %v", actual)
	}
	if err := store.MoveCollectionEntry(ctx, editor, playlist.ID, firstEntry, 0); err != nil {
		t.Fatalf("move visible entry back up: %v", err)
	}
	visible, err = store.CollectionItems(ctx, editor, playlist.ID, PlaylistKind, 0, 1)
	if err != nil || visible.TotalRecordCount != 2 || len(visible.Items) != 1 || visible.Items[0].PlaylistItemID != firstEntry {
		t.Fatalf("paged visible up move = %#v, %v", visible, err)
	}
	if err := store.RemoveCollectionItems(ctx, editor, playlist.ID, PlaylistKind, []string{lastEntry}); err != nil {
		t.Fatalf("remove authorized entry: %v", err)
	}
	visible, err = store.CollectionItems(ctx, editor, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || visible.TotalRecordCount != 1 || len(visible.Items) != 1 || visible.Items[0].PlaylistItemID != firstEntry {
		t.Fatalf("visible removal = %#v, %v", visible, err)
	}
	box, err := store.CreateCollection(ctx, owner, BoxSetKind, CollectionInput{Name: "Scoped removal", ItemIDs: []string{"collection-track-a", "collection-track-c"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateCollection(ctx, owner, box.ID, BoxSetKind, CollectionPatch{Shares: &shares}); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveCollectionItems(ctx, editor, box.ID, BoxSetKind, []string{"collection-track-a", "collection-track-c"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BoxSet hidden member removal = %v", err)
	}
	retained, err := store.GetCollection(ctx, owner, box.ID, BoxSetKind)
	if err != nil || retained.ItemCount != 2 {
		t.Fatalf("BoxSet rejected batch partially removed members: %#v, %v", retained, err)
	}
}

func TestRemovingAReferencedFolderDescendantInvalidatesCollectionAggregates(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type)
		VALUES('collection-unrelated-leaf','collection-source-a','Unrelated','unrelated','Audio')`); err != nil {
		t.Fatal(err)
	}
	owner := Subject{UserID: ownerID}
	box, err := store.CreateCollection(ctx, owner, BoxSetKind, CollectionInput{Name: "Folder reference", ItemIDs: []string{"collection-album"}})
	if err != nil {
		t.Fatal(err)
	}
	var notifications []CatalogNotification
	store.SetCatalogChangeListener(func(notification CatalogNotification) { notifications = append(notifications, notification) })
	remove := func(id, parent string) {
		t.Helper()
		tx, err := store.beginOwnedTx(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		protected := tx.(*ownedTx).ctx
		change := CatalogChange{Kind: CatalogRemoved, ItemID: id, LibraryID: "collection-source-a", ParentID: parent}
		if err := recordCollectionSourceRemovals(tx, []CatalogChange{change}); err != nil {
			t.Fatal(err)
		}
		if err := recordCatalogChanges(tx, change); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(protected, `DELETE FROM items WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(protected); err != nil {
			t.Fatal(err)
		}
	}
	remove("collection-unrelated-leaf", "")
	if len(notifications) != 1 || notifications[0].Resync || len(notifications[0].Changes) != 1 {
		t.Fatalf("unrelated deletion invalidated every collection: %#v", notifications)
	}
	remove("collection-track-b", "collection-album")
	if len(notifications) != 2 || !notifications[1].Resync || len(notifications[1].Changes) != 0 {
		t.Fatalf("referenced ancestor deletion did not safely invalidate aggregates: %#v", notifications)
	}
	retained, err := store.GetCollection(ctx, owner, box.ID, BoxSetKind)
	if err != nil || retained.ItemCount != 1 || retained.UserData == nil || retained.UserData.UnplayedItemCount == nil || *retained.UserData.UnplayedItemCount != 1 {
		t.Fatalf("folder reference aggregate did not follow its remaining media: %#v, %v", retained, err)
	}
	media, err := store.QueryItems(ctx, Query{UserID: ownerID, ParentID: box.ID, Recursive: true, IncludeItemTypes: []string{"Audio"}, Limit: 100})
	if err != nil || media.TotalRecordCount != 1 || len(media.Items) != 1 || media.Items[0].ID != "collection-track-a" {
		t.Fatalf("recursive collection retained deleted descendant: %#v, %v", media, err)
	}
}
