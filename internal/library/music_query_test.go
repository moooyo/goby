package library

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMusicQueryFiltersRejectInvalidIdentifiersBeforeDatabaseAccess(t *testing.T) {
	for _, query := range []Query{
		{UserID: "viewer", ArtistIds: []int64{0}}, {UserID: "viewer", AlbumArtistIds: []int64{-1}},
		{UserID: "viewer", AlbumIds: []string{""}}, {UserID: "viewer", AlbumIds: []string{" album"}},
		{UserID: "viewer", ExcludeItemIds: []string{"line\nbreak"}},
		{UserID: "viewer", AlbumIds: []string{strings.Repeat("a", 257)}},
		{UserID: "viewer", AlbumArtistIds: make([]int64, 1025)},
		{UserID: "viewer", ExcludeItemIds: make([]string, 1025)},
	} {
		if _, err := (&Store{}).QueryItems(context.Background(), query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid music filter reached database access: %v", err)
		}
	}
	query := Query{UserID: "viewer", ArtistIds: []int64{1}, AlbumArtistIds: []int64{2}, AlbumIds: []string{"album"}, ExcludeItemIds: []string{"track"}}
	normalized, err := normalizeItemQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	normalized.ArtistIds[0], normalized.AlbumArtistIds[0] = 3, 4
	normalized.AlbumIds[0], normalized.ExcludeItemIds[0] = "changed-album", "changed-track"
	if query.ArtistIds[0] != 1 || query.AlbumArtistIds[0] != 2 || query.AlbumIds[0] != "album" || query.ExcludeItemIds[0] != "track" {
		t.Fatal("normalized music filters retained mutable caller slices")
	}
}

func TestMusicQueryFiltersUsePersistedRolesAndPhysicalAlbumMembership(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('music-query-album-a','library-b','library-b','A Album','a album','MusicAlbum',true),
		('music-query-album-b','library-b','library-b','B Album','b album','MusicAlbum',true),
		('music-query-hidden-album','library-a','library-a','Hidden Album','hidden album','MusicAlbum',true),
		('music-query-disc','library-b','music-query-album-a','Disc','disc','Folder',true),
		('music-query-track-a','library-b','music-query-album-a','A Track','a track','Audio',false),
		('music-query-track-b','library-b','music-query-disc','B Track','b track','Audio',false),
		('music-query-track-c','library-b','music-query-album-b','C Track','c track','Audio',false),
		('music-query-hidden-track','library-a','music-query-hidden-album','Hidden Track','hidden track','Audio',false),
		('music-query-foreign-track','library-c','music-query-album-a','Foreign Track','foreign track','Audio',false)`); err != nil {
		t.Fatal("seed physical music query scopes")
	}
	for _, itemID := range []string{"music-query-album-a", "music-query-hidden-album"} {
		if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities($1,'{"Artists":["Artist A"],"AlbumArtists":["Artist A"]}'::jsonb)`, itemID); err != nil {
			t.Fatal("index album artist scope")
		}
	}
	for _, itemID := range []string{"music-query-track-a", "music-query-track-b", "music-query-hidden-track", "music-query-foreign-track"} {
		if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities($1,'{"Artists":["Artist A"],"AlbumArtists":[]}'::jsonb)`, itemID); err != nil {
			t.Fatal("index track artist scope")
		}
	}
	if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities('music-query-album-b','{"Artists":["Artist B"],"AlbumArtists":["Artist B"]}'::jsonb);
		SELECT sync_catalog_item_entities('music-query-track-c','{"Artists":["Artist B"],"AlbumArtists":[]}'::jsonb)`); err != nil {
		t.Fatal("index second album artist")
	}
	var artistA, artistB int64
	if err := store.pool.QueryRow(ctx, "SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Artist A'").Scan(&artistA); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, "SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Artist B'").Scan(&artistB); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		query Query
		want  []string
	}{
		{name: "artist", query: Query{ArtistIds: []int64{artistA}}, want: []string{"music-query-album-a", "music-query-track-a", "music-query-track-b"}},
		{name: "album_artist", query: Query{AlbumArtistIds: []int64{artistA}}, want: []string{"music-query-album-a", "music-query-track-a", "music-query-track-b"}},
		{name: "album_audio", query: Query{AlbumIds: []string{"music-query-album-a"}, IncludeItemTypes: []string{"Audio"}}, want: []string{"music-query-track-a", "music-query-track-b"}},
		{name: "combined_exclusion", query: Query{ArtistIds: []int64{artistA}, AlbumArtistIds: []int64{artistA}, AlbumIds: []string{"music-query-album-a"}, ExcludeItemIds: []string{"music-query-track-a"}, IncludeItemTypes: []string{"Audio"}}, want: []string{"music-query-track-b"}},
		{name: "other_artist", query: Query{AlbumArtistIds: []int64{artistB}, IncludeItemTypes: []string{"Audio"}}, want: []string{"music-query-track-c"}},
		{name: "foreign_album", query: Query{AlbumIds: []string{"music-query-hidden-album"}}, want: []string{}},
		{name: "no_actual_match", query: Query{ArtistIds: []int64{artistA}, AlbumIds: []string{"music-query-album-b"}}, want: []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			query.UserID = "restricted"
			result, err := store.QueryItems(ctx, query)
			if err != nil || result.TotalRecordCount != len(test.want) || !reflect.DeepEqual(queryItemIDs(result.Items), test.want) {
				t.Fatalf("music filter did not match its real visible relationships: %+v, %v", result, err)
			}
			for offset := 0; offset <= len(test.want); offset++ {
				query.StartIndex, query.Limit = offset, 1
				page, err := store.QueryItems(ctx, query)
				if err != nil || page.TotalRecordCount != len(test.want) {
					t.Fatalf("music filter count changed after paging: %+v, %v", page, err)
				}
				if offset == len(test.want) {
					if len(page.Items) != 0 {
						t.Fatal("music query produced a row beyond its real result set")
					}
				} else if len(page.Items) != 1 || page.Items[0].ID != test.want[offset] {
					t.Fatal("music query paged before applying its relationships")
				}
			}
		})
	}
	admin, err := store.QueryItems(ctx, Query{UserID: "admin", AlbumIds: []string{"music-query-album-a"}, IncludeItemTypes: []string{"Audio"}})
	if err != nil || !reflect.DeepEqual(queryItemIDs(admin.Items), []string{"music-query-track-a", "music-query-track-b"}) {
		t.Fatal("physical album filtering crossed a library boundary for an administrator")
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.QueryItems(ctx, Query{UserID: "restricted", ArtistIds: []int64{artistA}})
	if err != nil || revoked.TotalRecordCount != 0 || len(revoked.Items) != 0 {
		t.Fatal("a music relationship bypassed current library revocation")
	}
}
