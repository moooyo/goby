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

func TestMusicQueryAlbumArtistFiltersSelectOwnGroupBeforePhysicalFallback(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('album-artist-parent','library-b','library-b','Parent Album','parent album','MusicAlbum',true),
		('album-artist-disc','library-b','album-artist-parent','Disc','disc','Folder',true),
		('album-artist-own','library-b','album-artist-disc','Own Track','own track','Audio',false),
		('album-artist-fallback','library-b','album-artist-disc','Fallback Track','fallback track','Audio',false),
		('album-artist-multiple','library-b','album-artist-parent','Multiple Credits','multiple credits','Audio',false),
		('album-artist-standalone-own','library-b','library-b','Standalone Own','standalone own','Audio',false),
		('album-artist-standalone-empty','library-b','library-b','Standalone Empty','standalone empty','Audio',false),
		('album-artist-foreign-own','library-c','album-artist-parent','Foreign Own','foreign own','Audio',false),
		('album-artist-foreign-empty','library-c','album-artist-parent','Foreign Empty','foreign empty','Audio',false),
		('album-artist-video-own','library-b','album-artist-parent','Music Video Own','music video own','MusicVideo',false),
		('album-artist-video-empty','library-b','album-artist-parent','Music Video Empty','music video empty','MusicVideo',false),
		('album-artist-invalid-credits','library-b','album-artist-parent','Invalid Credits','invalid credits','Audio',false),
		('album-artist-movie','library-b','album-artist-parent','Movie With Stray Credits','movie with stray credits','Movie',false);
		SELECT sync_catalog_item_entities('album-artist-parent','{"Artists":["Query Album A"],"AlbumArtists":["Query Album A"]}'::jsonb);
		SELECT sync_catalog_item_entities('album-artist-multiple','{"Artists":["Query Album A"],"AlbumArtists":["Query Album B","Query Album C"]}'::jsonb);
		INSERT INTO catalog_entities(kind,name) VALUES('Person','Query Album B')`); err != nil {
		t.Fatalf("seed independent own and physical album artist groups: %v", err)
	}
	for _, id := range []string{"album-artist-own", "album-artist-standalone-own", "album-artist-foreign-own", "album-artist-video-own", "album-artist-movie"} {
		if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities($1,
			'{"Artists":["Query Album A"],"AlbumArtists":["Query Album B"]}'::jsonb)`, id); err != nil {
			t.Fatalf("persist a separate own album artist credit: %v", err)
		}
	}
	var artistA, artistB, artistC int64
	if err := store.pool.QueryRow(ctx, `SELECT
		(SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Query Album A'),
		(SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Query Album B'),
		(SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Query Album C')`).Scan(&artistA, &artistB, &artistC); err != nil {
		t.Fatalf("read actual album artist filter identities: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type) VALUES
		('album-artist-invalid-credits',$1,1,'Wrong group',0,'AlbumArtist'),
		('album-artist-invalid-credits',$1,1,'Wrong credit type',2,'Artist'),
		('album-artist-invalid-credits',(SELECT id FROM catalog_entities WHERE kind='Person' AND name='Query Album B'),
		1,'Person is not a music identity',2,'AlbumArtist')`, artistB); err != nil {
		t.Fatalf("seed invalid role and generic-person associations: %v", err)
	}
	for _, test := range []struct {
		name, item, user string
		artists          []int64
		want             bool
	}{
		{name: "own_artist_matches", item: "album-artist-own", artists: []int64{artistB}, want: true},
		{name: "parent_artist_cannot_override_own", item: "album-artist-own", artists: []int64{artistA}},
		{name: "any_requested_id_matches_selected_own_group", item: "album-artist-own", artists: []int64{artistA, artistB}, want: true},
		{name: "multiple_own_first", item: "album-artist-multiple", artists: []int64{artistB}, want: true},
		{name: "multiple_own_second", item: "album-artist-multiple", artists: []int64{artistC}, want: true},
		{name: "multiple_own_never_per_artist_parent_fallback", item: "album-artist-multiple", artists: []int64{artistA}},
		{name: "absent_own_inherits_nearest_album", item: "album-artist-fallback", artists: []int64{artistA}, want: true},
		{name: "physical_album_uses_own", item: "album-artist-parent", artists: []int64{artistA}, want: true},
		{name: "standalone_own_matches", item: "album-artist-standalone-own", artists: []int64{artistB}, want: true},
		{name: "standalone_empty_has_no_invented_parent", item: "album-artist-standalone-empty", artists: []int64{artistA}},
		{name: "music_video_own_matches", item: "album-artist-video-own", artists: []int64{artistB}, want: true},
		{name: "music_video_own_blocks_parent", item: "album-artist-video-own", artists: []int64{artistA}},
		{name: "music_video_absent_inherits", item: "album-artist-video-empty", artists: []int64{artistA}, want: true},
		{name: "invalid_own_roles_do_not_block_real_parent", item: "album-artist-invalid-credits", artists: []int64{artistA}, want: true},
		{name: "invalid_own_roles_do_not_match", item: "album-artist-invalid-credits", artists: []int64{artistB}},
		{name: "movie_stray_music_credit_is_not_membership", item: "album-artist-movie", artists: []int64{artistB}},
		{name: "movie_stray_parent_is_not_membership", item: "album-artist-movie", artists: []int64{artistA}},
		{name: "cross_library_parent_is_never_inherited", item: "album-artist-foreign-empty", user: "admin", artists: []int64{artistA}},
		{name: "cross_library_own_still_matches_for_authorized_admin", item: "album-artist-foreign-own", user: "admin", artists: []int64{artistB}, want: true},
		{name: "cross_library_own_does_not_grant_viewer_access", item: "album-artist-foreign-own", artists: []int64{artistB}},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := test.user
			if user == "" {
				user = "restricted"
			}
			result, err := store.QueryItems(ctx, Query{UserID: user, Ids: []string{test.item}, AlbumArtistIds: test.artists})
			want := []string{}
			if test.want {
				want = append(want, test.item)
			}
			if err != nil || result.TotalRecordCount != len(want) || !reflect.DeepEqual(queryItemIDs(result.Items), want) {
				t.Fatalf("album artist filtering did not select the complete effective group: result=%+v want=%v error=%v", result, want, err)
			}
		})
	}
	artistResult, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: []string{"album-artist-own"}, ArtistIds: []int64{artistA}})
	if err != nil || !reflect.DeepEqual(queryItemIDs(artistResult.Items), []string{"album-artist-own"}) {
		t.Fatalf("album artist precedence changed the independent group-one ArtistIds filter: %+v error=%v", artistResult, err)
	}
	physicalResult, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: []string{"album-artist-own"}, AlbumIds: []string{"album-artist-parent"}})
	if err != nil || !reflect.DeepEqual(queryItemIDs(physicalResult.Items), []string{"album-artist-own"}) {
		t.Fatalf("own album artist credits changed physical AlbumIds membership: %+v error=%v", physicalResult, err)
	}
}
