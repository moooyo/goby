package library

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func musicAlbumCountTestStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder) VALUES
		('album-count-hidden', 'library-a', 'library-a', '0 Hidden Album', '0 Hidden Album', 'MusicAlbum', true),
		('album-count-empty', 'library-b', 'library-b', 'A Empty Album', 'A Empty Album', 'MusicAlbum', true),
		('album-count-single', 'library-b', 'library-b', 'B Single Album', 'B Single Album', 'MusicAlbum', true),
		('album-count-three', 'library-b', 'library-b', 'C Three Children', 'C Three Children', 'MusicAlbum', true),
		('album-count-not-folder', 'library-b', 'library-b', 'D Nonfolder Album', 'D Nonfolder Album', 'MusicAlbum', false),
		('album-count-hidden-track', 'library-a', 'album-count-hidden', 'Hidden Track', 'Hidden Track', 'Audio', false),
		('album-count-single-track', 'library-b', 'album-count-single', 'Single Track', 'Single Track', 'Audio', false),
		('album-count-direct-first', 'library-b', 'album-count-three', 'A Direct Track', 'A Direct Track', 'Audio', false),
		('album-count-direct-second', 'library-b', 'album-count-three', 'B Direct Track', 'B Direct Track', 'Audio', false),
		('album-count-disc', 'library-b', 'album-count-three', 'C Disc Folder', 'C Disc Folder', 'Folder', true),
		('album-count-nested-first', 'library-b', 'album-count-disc', 'Nested First', 'Nested First', 'Audio', false),
		('album-count-nested-second', 'library-b', 'album-count-disc', 'Nested Second', 'Nested Second', 'Audio', false),
		('album-count-cross-library', 'library-c', 'album-count-three', '0 Foreign Child', '0 Foreign Child', 'Audio', false),
		('album-count-nonfolder-child', 'library-b', 'album-count-not-folder', 'Nonfolder Child', 'Nonfolder Child', 'Audio', false)`); err != nil {
		t.Fatalf("seed music album count fixtures: %v", err)
	}
	return ctx, store
}

func assertMusicAlbumChildCount(t *testing.T, item Item, want int) {
	t.Helper()
	if item.Type != "MusicAlbum" || !item.IsFolder || item.ChildCount == nil || *item.ChildCount != want {
		t.Fatalf("music album %s child count = %v, want %d for an album folder", item.ID, item.ChildCount, want)
	}
}

func TestMusicAlbumChildCountsMatchDirectChildrenAcrossQueriesAndPages(t *testing.T) {
	ctx, store := musicAlbumCountTestStore(t)
	ids := []string{"album-count-empty", "album-count-single", "album-count-three", "album-count-hidden"}
	expected := []struct {
		id       string
		children []string
	}{
		{id: "album-count-empty", children: []string{}},
		{id: "album-count-single", children: []string{"album-count-single-track"}},
		{id: "album-count-three", children: []string{"album-count-direct-first", "album-count-direct-second", "album-count-disc"}},
	}
	query := Query{UserID: "restricted", Ids: ids}
	listed, err := store.QueryItems(ctx, query)
	if err != nil {
		t.Fatalf("list music album counts: %v", err)
	}
	if listed.TotalRecordCount != len(expected) || len(listed.Items) != len(expected) {
		t.Fatalf("visible album list = %+v, want %d albums", listed, len(expected))
	}
	for index, want := range expected {
		if listed.Items[index].ID != want.id {
			t.Fatalf("visible album at %d = %s, want %s", index, listed.Items[index].ID, want.id)
		}
		assertMusicAlbumChildCount(t, listed.Items[index], len(want.children))
		item, err := store.GetItem(ctx, "restricted", want.id)
		if err != nil {
			t.Fatalf("get music album count: %v", err)
		}
		assertMusicAlbumChildCount(t, item, len(want.children))
		children, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: want.id})
		if err != nil {
			t.Fatalf("query direct music album children: %v", err)
		}
		if children.TotalRecordCount != *item.ChildCount || !reflect.DeepEqual(queryItemIDs(children.Items), want.children) {
			t.Fatalf("album %s child count does not match its nonrecursive child query: %+v", want.id, children)
		}
		for offset := 0; offset <= len(want.children); offset++ {
			page, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: want.id, StartIndex: offset, Limit: 1})
			if err != nil {
				t.Fatalf("page direct music album children: %v", err)
			}
			if page.TotalRecordCount != *item.ChildCount {
				t.Fatalf("album %s child count changed with page offset %d: %+v", want.id, offset, page)
			}
			if offset == len(want.children) {
				if len(page.Items) != 0 {
					t.Errorf("album %s returned a child beyond its final page", want.id)
				}
			} else if len(page.Items) != 1 || page.Items[0].ID != want.children[offset] {
				t.Errorf("album %s child page at %d = %+v", want.id, offset, page)
			}
		}
	}
	for offset := 0; offset <= len(expected); offset++ {
		query.StartIndex, query.Limit = offset, 1
		page, err := store.QueryItems(ctx, query)
		if err != nil {
			t.Fatalf("page music albums with child counts: %v", err)
		}
		if page.TotalRecordCount != len(expected) {
			t.Fatalf("album pagination lost the full visible album count: %+v", page)
		}
		if offset == len(expected) {
			if len(page.Items) != 0 {
				t.Error("album query returned an item beyond its final page")
			}
			continue
		}
		if len(page.Items) != 1 || page.Items[0].ID != expected[offset].id {
			t.Fatalf("album page at %d = %+v", offset, page)
		}
		assertMusicAlbumChildCount(t, page.Items[0], len(expected[offset].children))
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type)
		VALUES ('album-count-new-child', 'library-b', 'album-count-empty', 'New Track', 'New Track', 'Audio')`); err != nil {
		t.Fatalf("add a current direct child: %v", err)
	}
	current, err := store.GetItem(ctx, "restricted", "album-count-empty")
	if err != nil {
		t.Fatalf("read current album count after a catalog change: %v", err)
	}
	assertMusicAlbumChildCount(t, current, 1)
	updated, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: []string{current.ID}})
	if err != nil || updated.TotalRecordCount != 1 || len(updated.Items) != 1 {
		t.Fatalf("query current album count after a catalog change: %+v, %v", updated, err)
	}
	assertMusicAlbumChildCount(t, updated.Items[0], 1)
}

func TestMusicAlbumChildCountsRespectCurrentLibraryAccess(t *testing.T) {
	ctx, store := musicAlbumCountTestStore(t)
	for _, userID := range []string{"restricted", "none"} {
		if _, err := store.GetItem(ctx, userID, "album-count-hidden"); !errors.Is(err, ErrNotFound) {
			t.Errorf("hidden album count was accessible to %s: %v", userID, err)
		}
	}
	for _, userID := range []string{"default", "admin"} {
		album, err := store.GetItem(ctx, userID, "album-count-three")
		if err != nil {
			t.Fatalf("read album for unrestricted user %s: %v", userID, err)
		}
		assertMusicAlbumChildCount(t, album, 3)
		children, err := store.QueryItems(ctx, Query{UserID: userID, ParentID: album.ID})
		if err != nil || children.TotalRecordCount != 3 || len(children.Items) != 3 {
			t.Fatalf("unrestricted direct child query crossed the album library: %+v, %v", children, err)
		}
		for _, child := range children.Items {
			if child.ID == "album-count-cross-library" || child.LibraryID != album.LibraryID {
				t.Errorf("unrestricted album child query included a corrupt foreign child: %+v", child)
			}
		}
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb
		WHERE id = 'restricted'`); err != nil {
		t.Fatalf("revoke current album visibility: %v", err)
	}
	if _, err := store.GetItem(ctx, "restricted", "album-count-three"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked album still exposed its count: %v", err)
	}
	if _, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: "album-count-three", Limit: 1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked album still exposed its child query: %v", err)
	}
	for _, userID := range []string{"restricted", "none"} {
		result, err := store.QueryItems(ctx, Query{UserID: userID, Ids: []string{"album-count-empty", "album-count-single", "album-count-three", "album-count-hidden"}, Limit: 1})
		if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
			t.Errorf("user %s received album counts without current visibility: %+v, %v", userID, result, err)
		}
	}
	other, err := store.GetItem(ctx, "default", "album-count-three")
	if err != nil {
		t.Fatalf("read another user's unchanged album access: %v", err)
	}
	assertMusicAlbumChildCount(t, other, 3)
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":["library-b"]}'::jsonb
		WHERE id = 'none'`); err != nil {
		t.Fatalf("grant another user's current album visibility: %v", err)
	}
	granted, err := store.GetItem(ctx, "none", "album-count-three")
	if err != nil {
		t.Fatalf("read album after current visibility grant: %v", err)
	}
	assertMusicAlbumChildCount(t, granted, 3)
}

func TestMusicAlbumChildCountsStayNilForOtherItemsAndPreserveLatestCounts(t *testing.T) {
	ctx, store := musicAlbumCountTestStore(t)
	ids := []string{"audio-b", "album-count-direct-first", "album-count-disc", "album-count-not-folder", "movie-b", "series-b", "library-b"}
	items, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: ids})
	if err != nil || items.TotalRecordCount != len(ids) || len(items.Items) != len(ids) {
		t.Fatalf("query non-album-folder count projections: %+v, %v", items, err)
	}
	for _, item := range items.Items {
		if item.ChildCount != nil {
			t.Errorf("non-album folder or media %s acquired a child count", item.ID)
		}
		read, err := store.GetItem(ctx, "restricted", item.ID)
		if err != nil {
			t.Fatalf("get non-album-folder count projection: %v", err)
		}
		if read.ChildCount != nil {
			t.Errorf("non-album detail %s acquired a child count", read.ID)
		}
	}
	query := Query{UserID: "restricted", Ids: []string{"album-count-nested-first"}, IncludeItemTypes: []string{"Audio"}}
	grouped, err := store.QueryLatest(ctx, query, true)
	if err != nil || len(grouped) != 1 || grouped[0].Item.ID != "album-count-three" || grouped[0].ChildCount != 1 {
		t.Fatalf("latest matched-source count changed with the album projection: %+v, %v", grouped, err)
	}
	assertMusicAlbumChildCount(t, grouped[0].Item, 3)
	raw, err := store.QueryLatest(ctx, query, false)
	if err != nil || len(raw) != 1 || raw[0].Item.ID != "album-count-nested-first" || raw[0].ChildCount != 1 || raw[0].Item.ChildCount != nil {
		t.Fatalf("raw latest audio count changed with the album projection: %+v, %v", raw, err)
	}
}
