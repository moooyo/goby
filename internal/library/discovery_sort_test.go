package library

import (
	"reflect"
	"testing"
)

func TestDiscoveryMusicSortUsesOrderedCreditsAndPhysicalAlbumBeforePaging(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,index_number) VALUES
		('discovery-album-a','library-b','library-b','Zulu Album','zulu','MusicAlbum',true,0),
		('discovery-album-b','library-b','library-b','Alpha Album','alpha','MusicAlbum',true,0),
		('discovery-track-a','library-b','discovery-album-a','A Track','a','Audio',false,2),
		('discovery-track-b','library-b','discovery-album-b','B Track','b','Audio',false,1),
		('discovery-track-c','library-b','discovery-album-a','C Track','c','Audio',false,1),
		('discovery-track-empty','library-b','library-b','Empty Track','empty','Audio',false,0),
		('discovery-track-hidden','library-a','library-a','Hidden Track','hidden','Audio',false,0);
		SELECT sync_catalog_item_entities('discovery-album-a','{"AlbumArtists":["Album B","Album A"]}'::jsonb);
		SELECT sync_catalog_item_entities('discovery-album-b','{"AlbumArtists":["Album A"]}'::jsonb);
		SELECT sync_catalog_item_entities('discovery-track-a','{"Artists":["Artist B","Artist A"],"AlbumArtists":[]}'::jsonb);
		SELECT sync_catalog_item_entities('discovery-track-b','{"Artists":["Artist A"],"AlbumArtists":["Album Z","Album A"]}'::jsonb);
		SELECT sync_catalog_item_entities('discovery-track-c','{"Artists":["Artist C"],"AlbumArtists":[]}'::jsonb);
		SELECT sync_catalog_item_entities('discovery-track-hidden','{"Artists":["Artist 0"],"AlbumArtists":["Album 0"]}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		by, order string
		want      []string
	}{
		{"Artist", "Ascending", []string{"discovery-track-b", "discovery-track-a", "discovery-track-c", "discovery-track-empty"}},
		{"AlbumArtist", "Ascending", []string{"discovery-track-a", "discovery-track-c", "discovery-track-b", "discovery-track-empty"}},
		{"Album,IndexNumber", "Ascending,Descending", []string{"discovery-track-b", "discovery-track-a", "discovery-track-c", "discovery-track-empty"}},
	} {
		query := Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Audio"},
			Ids: []string{"discovery-track-a", "discovery-track-b", "discovery-track-c", "discovery-track-empty", "discovery-track-hidden"}, SortBy: test.by, SortOrder: test.order}
		result, err := store.QueryItems(ctx, query)
		if err != nil || result.TotalRecordCount != len(test.want) || !reflect.DeepEqual(queryItemIDs(result.Items), test.want) {
			t.Fatalf("music sort %s = %+v, %v; want %v", test.by, result, err, test.want)
		}
		for offset, id := range test.want {
			query.StartIndex, query.Limit = offset, 1
			page, err := store.QueryItems(ctx, query)
			if err != nil || page.TotalRecordCount != len(test.want) || len(page.Items) != 1 || page.Items[0].ID != id {
				t.Fatalf("music sort %s lost ordered paging: %+v, %v", test.by, page, err)
			}
		}
	}
}
