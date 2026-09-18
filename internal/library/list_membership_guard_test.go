package library

import "testing"

func TestListMembershipRequiresVisibleContainersAndVisibleMembersBeforePaging(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('playlist','library-b','library-b','A Playlist','A Playlist','Playlist',true),
		('boxset','library-b','library-b','B Collection','B Collection','BoxSet',true),
		('private-list','library-b','library-b','Private','Private','Playlist',true);
		INSERT INTO media_collections(item_id,owner_id,kind) VALUES
		('playlist','restricted','Playlist'),('boxset','restricted','BoxSet'),('private-list','default','Playlist');
		INSERT INTO media_collection_entries(collection_id,item_id,position) VALUES
		('playlist','movie-b',0),('boxset','movie-b',0),('private-list','movie-b',0),('playlist','movie-a',1)`); err != nil {
		t.Fatal(err)
	}
	query := Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Playlist", "BoxSet"}, ListItemIds: []string{"movie-b"}, Limit: 1}
	result, err := store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 2 || len(result.Items) != 1 || result.Items[0].ID != "playlist" {
		t.Fatalf("container membership result=%+v err=%v", result, err)
	}
	query.StartIndex = 1000
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 2 || len(result.Items) != 0 {
		t.Fatalf("membership count changed with paging: %+v %v", result, err)
	}
	for _, member := range []string{"missing-member", "movie-a"} {
		query.ListItemIds = []string{member}
		query.StartIndex = 0
		result, err = store.QueryItems(ctx, query)
		if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
			t.Fatalf("inaccessible member disclosed containing collections: %+v %v", result, err)
		}
	}
	query.ListItemIds = []string{"movie-b"}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy||'{"ExcludedSubFolders":["movie-b"]}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("subfolder restriction bypassed membership: %+v %v", result, err)
	}
}

func TestListMembershipCannotBeDiscardedByAlternateQueries(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	query := Query{UserID: "restricted", ListItemIds: []string{"missing-member"}}
	if items, err := store.QueryLatest(ctx, query, true); err != nil || len(items) != 0 {
		t.Fatalf("Latest ignored membership: %+v %v", items, err)
	}
	if result, err := store.QueryResume(ctx, query); err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("Resume ignored membership: %+v %v", result, err)
	}
	if result, err := store.ListEntities(ctx, "Genre", query); err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("entity listing ignored membership: %+v %v", result, err)
	}
}
