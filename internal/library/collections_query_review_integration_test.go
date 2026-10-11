package library

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestCollectionItemsReuseAuthorizedParent(t *testing.T) {
	ctx, pool, store, ownerID, trace := catalogBatchTestStore(t)
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "collection-query-reader", false, false, []string{"collection-source-a"})
	owner := Subject{UserID: ownerID}
	reader := Subject{UserID: "collection-query-reader"}
	application := seedCatalogApplicationKey(t, ctx, pool, "collection-query-key", true)
	targeted := application
	targeted.UserID = reader.UserID
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{
		Name: "Parent reuse", ItemIDs: []string{"collection-track-b", "collection-track-a", "collection-track-a", "collection-track-c"},
	})
	box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind, CollectionInput{
		Name: "Folder members", ItemIDs: []string{"collection-album", "collection-track-c"},
	})
	empty := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Empty parent"})
	for _, collection := range []CollectionInfo{playlist, box, empty} {
		collectionUserDataShare(t, ctx, store, owner, collection, reader.UserID)
	}
	repeated := make([]string, 101)
	for index := range repeated {
		repeated[index] = "collection-track-a"
	}
	long := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Default page", ItemIDs: repeated})
	for _, test := range []struct {
		name       string
		subject    Subject
		collection CollectionInfo
		start      int
		limit      int
		total      int
		page       int
	}{
		{name: "playlist owner", subject: owner, collection: playlist, limit: 100, total: 4, page: 4},
		{name: "repeated page", subject: owner, collection: playlist, start: 1, limit: 2, total: 4, page: 2},
		{name: "exhausted page", subject: owner, collection: playlist, start: 100, limit: 2, total: 4},
		{name: "restricted reader", subject: reader, collection: playlist, limit: 100, total: 3, page: 3},
		{name: "userless application", subject: application, collection: playlist, limit: 100, total: 4, page: 4},
		{name: "targeted application", subject: targeted, collection: playlist, limit: 100, total: 3, page: 3},
		{name: "box owner", subject: owner, collection: box, limit: 100, total: 2, page: 2},
		{name: "box reader", subject: reader, collection: box, limit: 100, total: 1, page: 1},
		{name: "empty collection", subject: owner, collection: empty, limit: 100},
		{name: "zero limit defaults to one hundred", subject: owner, collection: long, total: 101, page: 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := Query{UserID: test.subject.UserID, ApplicationCredentialID: test.subject.ApplicationCredentialID,
				ParentID: test.collection.ID, StartIndex: test.start, Limit: test.limit}
			trace.reset()
			generic, err := store.QueryItems(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			if count := catalogBatchStatementCount(trace, "SELECT kind FROM media_collections WHERE item_id=$1"); count != 1 {
				t.Fatalf("generic collection discovery used %d queries, want one", count)
			}
			if count := catalogBatchStatementCount(trace, "WHERE i.id=$1 AND c.kind=$2"); count != 1 {
				t.Fatalf("generic parent authorization used %d queries, want one", count)
			}
			trace.reset()
			actual, err := store.CollectionItems(ctx, test.subject, test.collection.ID, test.collection.Kind, test.start, test.limit)
			if err != nil || !reflect.DeepEqual(actual, generic) || actual.TotalRecordCount != test.total || len(actual.Items) != test.page || actual.Items == nil {
				t.Fatalf("dedicated member page differs: actual=%+v generic=%+v error=%v", actual, generic, err)
			}
			if count := catalogBatchStatementCount(trace, "WHERE i.id=$1 AND c.kind=$2"); count != 1 {
				t.Fatalf("dedicated member page authorized its parent %d times, want once", count)
			}
			if count := catalogBatchStatementCount(trace, "SELECT kind FROM media_collections WHERE item_id=$1"); count != 0 {
				t.Fatalf("dedicated member page rediscovered its parent's kind %d times", count)
			}
			if count := catalogBatchStatementCount(trace, "commit"); count != 1 {
				t.Fatalf("dedicated member page completed %d transactions, want one", count)
			}
			seen := make(map[string]bool)
			for _, item := range actual.Items {
				if test.collection.Kind == PlaylistKind {
					if item.PlaylistItemID == "" || seen[item.PlaylistItemID] {
						t.Fatalf("playlist entry identity was missing or repeated: %q", item.PlaylistItemID)
					}
					seen[item.PlaylistItemID] = true
				}
				if (item.UserData != nil) != (test.subject.UserID != "") {
					t.Fatalf("default user data attachment changed for %q", item.ID)
				}
			}
		})
	}
	for _, test := range []struct{ id, kind string }{
		{id: playlist.ID, kind: BoxSetKind}, {id: box.ID, kind: PlaylistKind}, {id: "missing-collection-parent", kind: PlaylistKind},
	} {
		trace.reset()
		if _, err := store.CollectionItems(ctx, owner, test.id, test.kind, 0, 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("dedicated route accepted missing or wrong-kind parent: %v", err)
		}
		if catalogBatchStatementCount(trace, "WHERE i.id=$1 AND c.kind=$2") != 1 || catalogBatchStatementCount(trace, "SELECT kind FROM media_collections WHERE item_id=$1") != 0 {
			t.Fatal("rejected dedicated parent reached generic collection discovery")
		}
	}
	trace.reset()
	folder, err := store.QueryItems(ctx, Query{UserID: reader.UserID, ParentID: "collection-album", Limit: 100})
	if err != nil || !reflect.DeepEqual(queryItemIDs(folder.Items), []string{"collection-track-a", "collection-track-b"}) || catalogBatchStatementCount(trace, "SELECT kind FROM media_collections WHERE item_id=$1") != 1 {
		t.Fatalf("generic physical-folder fallback changed: %+v, %v", folder, err)
	}
	descendants, err := store.QueryItems(ctx, Query{UserID: ownerID, ParentID: box.ID, Recursive: true, Limit: 100})
	if err != nil || descendants.TotalRecordCount != 4 || len(descendants.Items) != 4 {
		t.Fatalf("generic recursive box members changed: %+v, %v", descendants, err)
	}
	trace.reset()
	count, err := store.CountQueryItems(ctx, Query{UserID: ownerID, ParentID: playlist.ID})
	if err != nil || count.TotalRecordCount != 4 || count.Items == nil || len(count.Items) != 0 {
		t.Fatalf("generic collection count changed: %+v, %v", count, err)
	}
	trace.assertCountWithoutPage(t)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=jsonb_set(policy,'{RestrictedFeatures}',to_jsonb($2::text[])) WHERE id=$1`, reader.UserID, []string{identity.FeaturePlaylists}); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	if _, err := store.CollectionItems(ctx, reader, playlist.ID, PlaylistKind, 0, 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dedicated member core bypassed the parent feature check: %v", err)
	}
	if catalogBatchStatementCount(trace, "WHERE i.id=$1 AND c.kind=$2") != 0 || catalogBatchStatementCount(trace, "SELECT kind FROM media_collections WHERE item_id=$1") != 0 {
		t.Fatal("restricted feature reached a parent query")
	}
}

func TestCollectionItemsUserDataProjectionKeepsMemberFilters(t *testing.T) {
	ctx, pool, store, ownerID, trace := catalogBatchTestStore(t)
	collectionTestMedia(t, ctx, pool)
	owner := Subject{UserID: ownerID}
	userDataSeed(t, ctx, pool, ownerID, UserData{ItemID: "collection-track-a", IsFavorite: true, Played: true, PlayCount: 7})
	userDataSeed(t, ctx, pool, ownerID, UserData{ItemID: "collection-track-b", PlayCount: 2})
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{
		Name: "Projected entries", ItemIDs: []string{"collection-track-b", "collection-track-a", "collection-track-a"},
	})
	box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind, CollectionInput{Name: "Projected folders", ItemIDs: []string{"collection-album"}})
	collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Nested projection", ParentID: box.ID, ItemIDs: []string{"collection-track-a"}})
	for _, collection := range []CollectionInfo{playlist, box} {
		for _, dedicated := range []bool{false, true} {
			var baseline ItemResult
			for _, projection := range []QueryProjection{{}, {UserDataDisabled: true}} {
				trace.reset()
				var result ItemResult
				var err error
				if dedicated {
					result, err = store.CollectionItems(ctx, owner, collection.ID, collection.Kind, 0, 100, projection)
				} else {
					result, err = store.QueryItems(ctx, Query{UserID: ownerID, ParentID: collection.ID, Limit: 100, Projection: projection})
				}
				if err != nil || len(result.Items) == 0 {
					t.Fatalf("collection projection failed: dedicated=%t projection=%+v result=%+v error=%v", dedicated, projection, result, err)
				}
				attachments := catalogBatchStatementCount(trace, "SELECT "+userDataColumns+" FROM user_item_data")
				if projection.UserDataDisabled {
					for index := range baseline.Items {
						baseline.Items[index].UserData = nil
						if baseline.Items[index].Collection != nil {
							baseline.Items[index].Collection.UserData = nil
						}
					}
					if attachments != 0 || !reflect.DeepEqual(result, baseline) {
						t.Fatal("disabling collection user data changed other projections or retained attachment SQL")
					}
				} else {
					baseline = result
					if attachments != 1 {
						t.Fatalf("complete collection page used %d user data attachment queries, want one", attachments)
					}
				}
				for _, item := range result.Items {
					if (item.UserData == nil) != projection.UserDataDisabled || item.Collection != nil && (item.Collection.UserData == nil) != projection.UserDataDisabled {
						t.Fatalf("collection projection lost independent user data control: %+v", item)
					}
				}
			}
		}
	}
	selected := true
	for _, test := range []struct {
		query Query
		want  []string
	}{
		{query: Query{SortBy: "PlayCount", SortOrder: "Descending"}, want: []string{"collection-track-a", "collection-track-a", "collection-track-b"}},
		{query: Query{IsFavorite: &selected}, want: []string{"collection-track-a", "collection-track-a"}},
		{query: Query{IsPlayed: &selected}, want: []string{"collection-track-a", "collection-track-a"}},
	} {
		query := test.query
		query.UserID, query.ParentID, query.Limit = ownerID, playlist.ID, 100
		query.Projection.UserDataDisabled = true
		trace.reset()
		result, err := store.QueryItems(ctx, query)
		if err != nil || result.TotalRecordCount != len(test.want) || !reflect.DeepEqual(queryItemIDs(result.Items), test.want) {
			t.Fatalf("disabled user data changed member filtering or ordering: %+v, %v", result, err)
		}
		if catalogBatchStatementCount(trace, "SELECT "+userDataColumns+" FROM user_item_data") != 0 || catalogBatchStatementCount(trace, "user_item_data") == 0 {
			t.Fatal("state-dependent membership must retain its predicate SQL and omit attachment SQL")
		}
	}
}
