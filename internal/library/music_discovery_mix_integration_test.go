package library

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func musicDiscoveryFixture(t *testing.T) (context.Context, *Store, int64, int64) {
	t.Helper()
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	seedLibraryQueryFixture(t, ctx, store.pool)
	for _, album := range []struct{ id, library, artist string }{
		{"mix-album-a", "library-b", "Ensemble"}, {"mix-album-b", "library-b", "Other ensemble"},
		{"mix-album-c", "library-b", "Foreign ensemble"}, {"mix-album-hidden", "library-a", "Ensemble"},
	} {
		similarInsert(t, ctx, store, album.id, "MusicAlbum", album.library, album.library, map[string]any{"AlbumArtists": []string{album.artist}, "Genres": []string{"Rock"}})
	}
	for _, track := range []struct{ id, album, library, artist, genre string }{
		{"mix-seed", "mix-album-a", "library-b", "Alpha", "Rock"},
		{"mix-sibling", "mix-album-a", "library-b", "Alpha", "Rock"},
		{"mix-related", "mix-album-b", "library-b", "Alpha", "Rock"},
		{"mix-foreign", "mix-album-c", "library-b", "Other", "Jazz"},
		{"mix-hidden", "mix-album-hidden", "library-a", "Alpha", "Rock"},
		{"mix-unprobed", "mix-album-a", "library-b", "Alpha", "Rock"},
	} {
		similarInsert(t, ctx, store, track.id, "Audio", track.album, track.library, map[string]any{"Artists": []string{track.artist}, "Genres": []string{track.genre}})
		if track.id != "mix-unprobed" {
			if _, err := store.pool.Exec(ctx, `UPDATE items i SET media=media || jsonb_build_object('ProbeVersion',$2::int,'FileChangeTimeNs',1,'Streams',jsonb_build_array(jsonb_build_object('Index',0,'CodecType','audio','Codec','flac'))),root_id=r.id,path=r.path || '/' || i.id || '.flac',relative_path=i.id || '.flac',file_identity='fixture-' || i.id,file_size=100,modified_at='2026-09-20T00:00:00Z' FROM library_roots r WHERE i.id=$1 AND r.library_id=i.library_id`, track.id, media.CurrentProbeVersion); err != nil {
				t.Fatal(err)
			}
		}
	}
	var artist, genre int64
	if err := store.pool.QueryRow(ctx, `SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Alpha'`).Scan(&artist); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT id FROM catalog_entities WHERE kind='Genre' AND name='Rock'`).Scan(&genre); err != nil {
		t.Fatal(err)
	}
	return ctx, store, artist, genre
}

func TestMusicInstantMixSharesTypedSeedsRankingPagingAndPlayableScope(t *testing.T) {
	ctx, store, artist, genre := musicDiscoveryFixture(t)
	query := InstantMixQuery{Query: Query{UserID: "restricted", Limit: 100}}
	before := similarState(t, ctx, store)
	seeds := []MusicMixSeed{
		{Kind: "Song", ID: "mix-seed"}, {Kind: "Item", ID: "mix-seed"}, {Kind: "Album", ID: "mix-album-a"},
		{Kind: "Artist", ID: strconv.FormatInt(artist, 10)}, {Kind: "Genre", ID: strconv.FormatInt(genre, 10)},
		{Kind: "GenreName", Name: "Rock"}, {Kind: "Item", ID: strconv.FormatInt(artist, 10)}, {Kind: "Item", ID: strconv.FormatInt(genre, 10)},
	}
	for _, seed := range seeds {
		result, err := store.QueryInstantMix(ctx, seed, query)
		if err != nil || result.TotalRecordCount != 4 || len(result.Items) != 4 {
			t.Fatalf("seed %#v result=%#v err=%v", seed, result, err)
		}
		if result.Items[len(result.Items)-1].ID != "mix-foreign" {
			t.Fatalf("metadata ranking lost unrelated fallback tail: %v", queryItemIDs(result.Items))
		}
		for _, item := range result.Items {
			if !item.CanPlay || item.Type != "Audio" || item.ID == "mix-hidden" || item.ID == "mix-unprobed" || item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion {
				t.Fatalf("mix admitted unplayable or hidden row: %#v", item)
			}
		}
		second, err := store.QueryInstantMix(ctx, seed, query)
		if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), queryItemIDs(second.Items)) {
			t.Fatal("unchanged mix was randomized")
		}
	}
	seed := MusicMixSeed{Kind: "Song", ID: "mix-seed"}
	result, err := store.QueryInstantMix(ctx, seed, query)
	if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), []string{"mix-seed", "mix-sibling", "mix-related", "mix-foreign"}) {
		t.Fatalf("seed/album/artist/fallback rank = %v, %v", queryItemIDs(result.Items), err)
	}
	query.Limit, query.StartIndex = 1, 2
	result, err = store.QueryInstantMix(ctx, seed, query)
	if err != nil || result.TotalRecordCount != 4 || !reflect.DeepEqual(queryItemIDs(result.Items), []string{"mix-related"}) {
		t.Fatalf("mix paging changed total/order: %#v %v", result, err)
	}
	query.Limit, query.StartIndex = 0, 0
	result, err = store.QueryInstantMix(ctx, seed, query)
	if err != nil || result.TotalRecordCount != 4 || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("zero mix page did not keep count: %#v %v", result, err)
	}
	query.Limit, query.StartIndex = 100, 99
	result, err = store.QueryInstantMix(ctx, seed, query)
	if err != nil || result.TotalRecordCount != 4 || len(result.Items) != 0 {
		t.Fatalf("distant page lost complete count: %#v %v", result, err)
	}
	if after := similarState(t, ctx, store); after != before {
		t.Fatal("mix reads changed catalog, metadata, user state, or sessions")
	}
}

func TestMusicInstantMixFiltersDoNotNarrowSeedAuthorityAndEmptySeedStaysEmpty(t *testing.T) {
	ctx, store, artist, _ := musicDiscoveryFixture(t)
	query := InstantMixQuery{Query: Query{UserID: "restricted", Limit: 100, SearchTerm: "foreign"}}
	seed := MusicMixSeed{Kind: "Song", ID: "mix-seed"}
	result, err := store.QueryInstantMix(ctx, seed, query)
	if err != nil || result.TotalRecordCount != 1 || result.Items[0].ID != "mix-foreign" {
		t.Fatalf("candidate search changed seed authority: %#v %v", result, err)
	}
	query.SearchTerm, query.ExcludeArtistIds = "", []int64{artist}
	result, err = store.QueryInstantMix(ctx, seed, query)
	if err != nil || result.TotalRecordCount != 1 || result.Items[0].ID != "mix-foreign" {
		t.Fatalf("artist exclusion leaked a credited candidate: %#v %v", result, err)
	}
	query.ExcludeArtistIds, query.ExcludeItemIds = nil, []string{"mix-seed", "mix-foreign"}
	result, err = store.QueryInstantMix(ctx, seed, query)
	if err != nil || result.TotalRecordCount != 2 || !reflect.DeepEqual(queryItemIDs(result.Items), []string{"mix-sibling", "mix-related"}) {
		t.Fatalf("item exclusion changed seed profile: %#v %v", result, err)
	}
	query.ExcludeItemIds = nil
	result, err = store.QueryInstantMix(ctx, MusicMixSeed{Kind: "Song", ID: "mix-unprobed"}, query)
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("unplayable seed triggered unrelated fallback: %#v %v", result, err)
	}
	for _, bad := range []MusicMixSeed{{Kind: "Song", ID: "mix-hidden"}, {Kind: "Album", ID: "mix-seed"}, {Kind: "Song", ID: "mix-album-a"}, {Kind: "Item", ID: "movie-b"}} {
		if _, err := store.QueryInstantMix(ctx, bad, query); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid or unauthorized seed %+v returned %v", bad, err)
		}
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueryInstantMix(ctx, seed, query); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled playback produced a playable queue: %v", err)
	}
}

func TestMusicInstantMixPlaylistVisibilityDeduplicationAndIDCollision(t *testing.T) {
	ctx, store, artist, _ := musicDiscoveryFixture(t)
	playlist, err := store.CreateCollection(ctx, Subject{UserID: "admin"}, PlaylistKind, CollectionInput{Name: "Mix seed", MediaType: "Audio", ItemIDs: []string{"mix-seed", "mix-seed", "mix-hidden"}, IsPublic: true})
	if err != nil {
		t.Fatal(err)
	}
	query := InstantMixQuery{Query: Query{UserID: "restricted", Limit: 100}}
	result, err := store.QueryInstantMix(ctx, MusicMixSeed{Kind: "Playlist", ID: playlist.ID}, query)
	if err != nil || result.TotalRecordCount != 4 || !reflect.DeepEqual(queryItemIDs(result.Items), []string{"mix-seed", "mix-sibling", "mix-related", "mix-foreign"}) {
		t.Fatalf("playlist repeats/hidden members escaped mix: %#v %v", result, err)
	}
	for _, item := range result.Items {
		if item.PlaylistItemID != "" {
			t.Fatal("deduplicated mix pretended to retain a playlist entry identity")
		}
	}
	private := false
	if _, err := store.UpdateCollection(ctx, Subject{UserID: "admin"}, playlist.ID, PlaylistKind, CollectionPatch{IsPublic: &private}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueryInstantMix(ctx, MusicMixSeed{Kind: "Item", ID: playlist.ID}, query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private playlist authorized a seed: %v", err)
	}
	id := strconv.FormatInt(artist, 10)
	similarInsert(t, ctx, store, id, "Audio", "library-a", "library-a", nil)
	if _, err := store.QueryInstantMix(ctx, MusicMixSeed{Kind: "Item", ID: id}, query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden opaque item ID was reinterpreted as entity: %v", err)
	}
	result, err = store.QueryInstantMix(ctx, MusicMixSeed{Kind: "Artist", ID: id}, query)
	if err != nil || result.TotalRecordCount != 4 {
		t.Fatalf("explicit typed entity was shadowed by an item ID: %#v %v", result, err)
	}
}

func TestMusicPrefixesCountAuthorizedNamesAndArtistRolesBeforePaging(t *testing.T) {
	ctx, store, _, _ := musicDiscoveryFixture(t)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET name=CASE id WHEN 'mix-seed' THEN $2 WHEN 'mix-sibling' THEN $3 WHEN 'mix-related' THEN $1 ELSE '7 notes' END WHERE id IN ('mix-seed','mix-sibling','mix-related','mix-foreign')`, "\u5468 song", "\u3000alpha", "\u00a0Alpha two"); err != nil {
		t.Fatal(err)
	}
	query := Query{UserID: "restricted", Recursive: true, Ids: []string{"mix-seed", "mix-sibling", "mix-related", "mix-foreign", "mix-hidden"}, Limit: 1, StartIndex: 99}
	got, err := store.QueryNamePrefixes(ctx, "", query)
	want := []NamePrefix{{"7", "1"}, {"A", "2"}, {"\u5468", "1"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("authorized prefix buckets = %#v, %v", got, err)
	}
	query.Ids = nil
	got, err = store.QueryNamePrefixes(ctx, "albumartists", query)
	want = []NamePrefix{{"E", "1"}, {"F", "1"}, {"O", "1"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("album-artist prefixes counted duplicate tracks: %#v %v", got, err)
	}
	query.SearchTerm = "Alpha"
	got, err = store.QueryNamePrefixes(ctx, "artists", query)
	if err != nil || !reflect.DeepEqual(got, []NamePrefix{{"A", "1"}}) {
		t.Fatalf("artist prefix search matched track names: %#v %v", got, err)
	}
}

func TestMusicInstantMixRejectsIncompleteIndexedSourcesWithoutOpeningFiles(t *testing.T) {
	ctx, store, _, _ := musicDiscoveryFixture(t)
	query := InstantMixQuery{Query: Query{UserID: "restricted", Limit: 100}}
	seed := MusicMixSeed{Kind: "Song", ID: "mix-seed"}
	for _, change := range []string{
		"root_id=NULL", "root_id='root-a'", "file_identity=''", "file_size=0", "modified_at=NULL",
		"path=''", "relative_path=''", "media=media || '{\"Size\":999}'::jsonb", "media=media || '{\"Streams\":[]}'::jsonb",
	} {
		if _, err := store.pool.Exec(ctx, `UPDATE items SET `+change+` WHERE id='mix-related'`); err != nil {
			t.Fatal(err)
		}
		result, err := store.QueryInstantMix(ctx, seed, query)
		if err != nil || result.TotalRecordCount != 3 {
			t.Fatalf("incomplete source qualified (%s): %#v %v", change, result, err)
		}
		if _, err := store.pool.Exec(ctx, `UPDATE items target SET root_id=source.root_id,path='/media/b/mix-related.flac',relative_path='mix-related.flac',file_identity='fixture-mix-related',file_size=source.file_size,modified_at=source.modified_at,media=source.media FROM items source WHERE target.id='mix-related' AND source.id='mix-seed'`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMusicAlbumSimilarAliasPreservesExistingAlgorithmAndRejectsTrackSeed(t *testing.T) {
	ctx, store, _, _ := musicDiscoveryFixture(t)
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 100, SortBy: "Name"}, ExplicitSort: true}
	ordinary, err := store.QuerySimilar(ctx, "mix-album-a", query)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := store.QuerySimilarAlbums(ctx, "mix-album-a", query)
	if err != nil || !reflect.DeepEqual(queryItemIDs(ordinary.Items), queryItemIDs(alias.Items)) || ordinary.TotalRecordCount != alias.TotalRecordCount {
		t.Fatalf("album alias changed established similarity: %#v %v", alias, err)
	}
	if _, err := store.QuerySimilarAlbums(ctx, "mix-seed", query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("album alias accepted an Audio seed: %v", err)
	}
}
