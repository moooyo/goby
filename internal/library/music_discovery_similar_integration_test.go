package library

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
)

func similarArtistID(t *testing.T, ctx context.Context, store *Store, kind, name string) int64 {
	t.Helper()
	var id int64
	if err := store.pool.QueryRow(ctx, "SELECT id FROM catalog_entities WHERE kind=$1 AND name=$2", kind, name).Scan(&id); err != nil {
		t.Fatalf("read similar artist fixture identity %q: %v", name, err)
	}
	return id
}

func similarArtistFixture(t *testing.T) (context.Context, *Store, int64) {
	t.Helper()
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	similarInsert(t, ctx, store, "artist-candidate-scope", "Folder", "library-b", "library-b", nil)
	for _, fixture := range []struct {
		id, artist, kind, parent, library string
		genres, tags, studios             []string
		albumArtist                       bool
	}{
		{id: "artist-seed-source", artist: "Seed Artist", genres: []string{"Shared Genre"}, tags: []string{"Shared Tag"}, studios: []string{"Shared Studio"}},
		{id: "artist-best-source", artist: "Zulu Best", genres: []string{"Shared Genre"}},
		{id: "artist-best-extra", artist: "Zulu Best", tags: []string{"Shared Tag"}, studios: []string{"Shared Studio"}},
		{id: "artist-double-source", artist: "Alpha Double", parent: "artist-candidate-scope", genres: []string{"Shared Genre"}, tags: []string{"Shared Tag"}},
		{id: "artist-duplicate-source", artist: "Beta Duplicate", genres: []string{"Shared Genre"}},
		{id: "artist-duplicate-second", artist: "Beta Duplicate", genres: []string{"Shared Genre"}},
		{id: "artist-genre-source", artist: "Gamma Genre", genres: []string{"Shared Genre"}},
		{id: "artist-album-source", artist: "Album Only", studios: []string{"Shared Studio"}, albumArtist: true},
		{id: "artist-unrelated-source", artist: "Unrelated Artist", genres: []string{"Unrelated Genre"}},
		{id: "artist-empty-source", artist: "Empty Artist"},
		{id: "artist-visible-source", artist: "Visible Hidden Features", genres: []string{"Other Genre"}},
		{id: "artist-hidden-boost", artist: "Beta Duplicate", library: "library-a", genres: []string{"Shared Genre"}, tags: []string{"Shared Tag"}, studios: []string{"Shared Studio"}},
		{id: "artist-hidden-source", artist: "Hidden Artist", library: "library-a", genres: []string{"Shared Genre"}},
		{id: "artist-hidden-seed-feature", artist: "Seed Artist", library: "library-a", genres: []string{"Unrelated Genre"}},
		{id: "artist-hidden-features", artist: "Visible Hidden Features", library: "library-a", genres: []string{"Shared Genre"}, tags: []string{"Shared Tag"}},
		{id: "artist-policy-boost", artist: "Beta Duplicate", genres: []string{"Shared Genre"}, tags: []string{"Shared Tag", "Blocked"}, studios: []string{"Shared Studio"}},
		{id: "artist-policy-hidden", artist: "Policy Hidden Artist", genres: []string{"Shared Genre"}, tags: []string{"Blocked"}},
		{id: "artist-movie-source", artist: "Movie Artist", kind: "Movie", genres: []string{"Shared Genre"}},
	} {
		if fixture.kind == "" {
			fixture.kind = "Audio"
		}
		if fixture.library == "" {
			fixture.library = "library-b"
		}
		if fixture.parent == "" {
			fixture.parent = fixture.library
		}
		metadata := map[string]any{"Genres": fixture.genres, "Tags": fixture.tags, "Studios": fixture.studios}
		if fixture.albumArtist {
			metadata["AlbumArtists"] = []string{fixture.artist}
		} else {
			metadata["Artists"] = []string{fixture.artist}
		}
		similarInsert(t, ctx, store, fixture.id, fixture.kind, fixture.parent, fixture.library, metadata)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy||'{"BlockedTags":["Blocked"]}'::jsonb WHERE id='restricted';
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type)
		SELECT item_id,entity_id,position+100,display_name,credit_group,credit_type FROM item_entities WHERE item_id='artist-duplicate-source';
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type)
		SELECT item_id,entity_id,200,display_name,2,'AlbumArtist' FROM item_entities
		WHERE item_id='artist-duplicate-source' AND credit_group=1 AND position<100;
		INSERT INTO catalog_entities(kind,name) VALUES('MusicArtist','Orphan Artist');
		INSERT INTO entity_user_data(user_id,entity_id,is_favorite,play_count,rating)
		SELECT 'restricted',id,true,3,8.5 FROM catalog_entities WHERE kind='MusicArtist' AND name='Zulu Best'`); err != nil {
		t.Fatalf("seed artist similarity duplicate credits and independent state: %v", err)
	}
	return ctx, store, similarArtistID(t, ctx, store, "MusicArtist", "Seed Artist")
}

func similarArtists(t *testing.T, ctx context.Context, store *Store, seed int64, query SimilarQuery, names ...string) EntityResult {
	t.Helper()
	result, err := store.QuerySimilarArtists(ctx, strconv.FormatInt(seed, 10), query)
	if err != nil {
		t.Fatalf("query similar artist: %v", err)
	}
	actual := make([]string, 0, len(result.Items))
	for _, entity := range result.Items {
		actual = append(actual, entity.Name)
		if entity.Type != "MusicArtist" || entity.ID == seed || entity.Images == nil || entity.UserData == nil {
			t.Fatalf("similar artist lost its entity identity or projection: %+v", entity)
		}
	}
	if result.Items == nil || result.TotalRecordCount != len(result.Items) || len(actual) != len(names) {
		t.Fatalf("similar artists violated its page contract: names=%v count=%d want=%v", actual, result.TotalRecordCount, names)
	}
	for index := range names {
		if actual[index] != names[index] {
			t.Fatalf("similar artist order=%v want=%v", actual, names)
		}
	}
	return result
}

func similarArtistState(t *testing.T, ctx context.Context, store *Store) string {
	t.Helper()
	var state string
	if err := store.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'entities',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM catalog_entities e),
		'entity_state',(SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id,entity_id) FROM entity_user_data u),
		'artwork_state',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM artwork_state s),
		'artwork_images',(SELECT jsonb_agg(to_jsonb(i) ORDER BY state_id,image_type,image_index) FROM artwork_images i))::text`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return similarState(t, ctx, store) + state
}

func TestStoreSimilarArtistsRanksAuthorizedDistinctMetadataAndPages(t *testing.T) {
	ctx, store, seed := similarArtistFixture(t)
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 20}}
	want := []string{"Zulu Best", "Alpha Double", "Album Only", "Beta Duplicate", "Gamma Genre"}
	before := similarArtistState(t, ctx, store)
	for repeat := 0; repeat < 2; repeat++ {
		result := similarArtists(t, ctx, store, seed, query, want...)
		if result.Items[0].Count != 2 || !result.Items[0].UserData.IsFavorite || result.Items[0].UserData.PlayCount != 3 ||
			result.Items[0].UserData.Rating == nil || *result.Items[0].UserData.Rating != 8.5 || result.Items[3].Count != 2 {
			t.Fatalf("similar artist replaced source counts or independent entity state: %+v", result.Items)
		}
	}
	for offset, name := range want {
		query.Limit, query.StartIndex = 1, offset
		similarArtists(t, ctx, store, seed, query, name)
	}
	query.StartIndex = len(want)
	similarArtists(t, ctx, store, seed, query)
	query.StartIndex = 999
	similarArtists(t, ctx, store, seed, query)
	query.StartIndex, query.Limit = 0, 0
	similarArtists(t, ctx, store, seed, query)
	query.Limit, query.ExplicitSort, query.SortBy, query.SortOrder = 20, true, "SortName", "Descending"
	similarArtists(t, ctx, store, seed, query, "Zulu Best", "Gamma Genre", "Beta Duplicate", "Alpha Double", "Album Only")
	query.SortBy, query.SortOrder = "Name", "Ascending"
	similarArtists(t, ctx, store, seed, query, "Album Only", "Alpha Double", "Beta Duplicate", "Gamma Genre", "Zulu Best")
	query.ExplicitSort = false
	for _, name := range []string{"Empty Artist", "Unrelated Artist"} {
		similarArtists(t, ctx, store, similarArtistID(t, ctx, store, "MusicArtist", name), query)
	}
	if similarArtistState(t, ctx, store) != before {
		t.Fatal("similar artist reads changed catalog, credits, controls, user state, or artwork")
	}
}

func TestStoreSimilarArtistsCandidateFiltersDoNotNarrowSeedAuthority(t *testing.T) {
	ctx, store, seed := similarArtistFixture(t)
	favorite := true
	for _, test := range []struct {
		name  string
		query Query
		want  []string
	}{
		{name: "entity search", query: Query{SearchTerm: "Alpha"}, want: []string{"Alpha Double"}},
		{name: "entity prefix", query: Query{NameStartsWith: "Beta"}, want: []string{"Beta Duplicate"}},
		{name: "entity favorite", query: Query{IsFavorite: &favorite}, want: []string{"Zulu Best"}},
		{name: "source parent", query: Query{ParentID: "artist-candidate-scope"}, want: []string{"Alpha Double"}},
		{name: "source ids", query: Query{Ids: []string{"artist-double-source"}}, want: []string{"Alpha Double"}},
		{name: "profiles retain other visible sources", query: Query{Ids: []string{"artist-best-source", "artist-double-source"}}, want: []string{"Zulu Best", "Alpha Double"}},
		{name: "source exclusions", query: Query{ExcludeItemIds: []string{"artist-double-source"}}, want: []string{"Zulu Best", "Album Only", "Beta Duplicate", "Gamma Genre"}},
		{name: "source metadata", query: Query{Genres: []string{"Shared Genre"}}, want: []string{"Zulu Best", "Alpha Double", "Beta Duplicate", "Gamma Genre"}},
		{name: "empty candidates", query: Query{SearchTerm: "No Such Artist"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.UserID, test.query.Limit = "restricted", 20
			similarArtists(t, ctx, store, seed, SimilarQuery{Query: test.query}, test.want...)
		})
	}
	similarInsert(t, ctx, store, "artist-shared-source", "Audio", "library-b", "library-b", map[string]any{
		"Artists": []string{"Coartist A", "Coartist B"}, "Genres": []string{"Shared Genre"},
	})
	first := similarArtistID(t, ctx, store, "MusicArtist", "Coartist A")
	query := SimilarQuery{Query: Query{UserID: "restricted", SearchTerm: "Coartist", Limit: 20}, ExcludeArtistIds: []int64{first, first, seed}}
	excluded := append([]int64(nil), query.ExcludeArtistIds...)
	similarArtists(t, ctx, store, seed, query, "Coartist B")
	if !reflect.DeepEqual(query.ExcludeArtistIds, excluded) {
		t.Fatal("similar artist query changed the caller's exclusion slice")
	}
}

func TestStoreSimilarArtistsRejectsHiddenOrNonmusicSeedsAndCurrentRevocation(t *testing.T) {
	ctx, store, seed := similarArtistFixture(t)
	for _, name := range []string{"Hidden Artist", "Policy Hidden Artist", "Movie Artist", "Orphan Artist"} {
		id := similarArtistID(t, ctx, store, "MusicArtist", name)
		for _, limit := range []int{0, 1} {
			if _, err := store.QuerySimilarArtists(ctx, strconv.FormatInt(id, 10), SimilarQuery{Query: Query{UserID: "restricted", Limit: limit}}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("nonvisible artist %q was authorized with limit %d: %v", name, limit, err)
			}
		}
	}
	genre := similarArtistID(t, ctx, store, "Genre", "Shared Genre")
	if _, err := store.QuerySimilarArtists(ctx, strconv.FormatInt(genre, 10), SimilarQuery{Query: Query{UserID: "restricted", Limit: 1}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a visible nonartist entity became an artist seed: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QuerySimilarArtists(ctx, strconv.FormatInt(seed, 10), SimilarQuery{Query: Query{UserID: "restricted", Limit: 20}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("similar artist seed bypassed current library revocation: %v", err)
	}
}

func TestStoreSimilarArtistsProfilesUseOwnArtistAndEffectiveAlbumArtistGroups(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	similarInsert(t, ctx, store, "artist-role-seed", "Audio", "library-b", "library-b", map[string]any{
		"Artists": []string{"Role Seed"}, "Genres": []string{"Role Feature"},
	})
	similarInsert(t, ctx, store, "artist-inherit-album", "MusicAlbum", "library-b", "library-b", map[string]any{
		"Artists": []string{"Parent Artist"}, "AlbumArtists": []string{"Inherited Artist"},
	})
	similarInsert(t, ctx, store, "artist-inherit-disc", "Folder", "artist-inherit-album", "library-b", nil)
	similarInsert(t, ctx, store, "artist-inherit-track", "Audio", "artist-inherit-disc", "library-b", map[string]any{
		"Genres": []string{"Role Feature"},
	})
	similarInsert(t, ctx, store, "artist-own-album", "MusicAlbum", "library-b", "library-b", map[string]any{
		"Artists": []string{"Parent Only Artist"}, "AlbumArtists": []string{"Overridden Artist"},
	})
	similarInsert(t, ctx, store, "artist-own-track", "MusicVideo", "artist-own-album", "library-b", map[string]any{
		"Artists": []string{"Own Artist"}, "AlbumArtists": []string{"Own Album Artist"}, "Genres": []string{"Role Feature"},
	})
	similarInsert(t, ctx, store, "artist-foreign-album", "MusicAlbum", "library-b", "library-b", map[string]any{
		"AlbumArtists": []string{"Foreign Parent Artist"},
	})
	similarInsert(t, ctx, store, "artist-foreign-track", "Audio", "artist-foreign-album", "library-a", map[string]any{
		"Genres": []string{"Role Feature"},
	})
	similarInsert(t, ctx, store, "artist-invalid-track", "Audio", "library-b", "library-b", map[string]any{
		"Genres": []string{"Role Feature"},
	})
	if _, err := store.pool.Exec(ctx, `INSERT INTO catalog_entities(kind,name) VALUES('MusicArtist','Invalid Role Artist');
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type)
		SELECT 'artist-invalid-track',id,1,name,1,'AlbumArtist' FROM catalog_entities WHERE kind='MusicArtist' AND name='Invalid Role Artist';
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type)
		SELECT 'artist-invalid-track',id,1,name,2,'Artist' FROM catalog_entities WHERE kind='MusicArtist' AND name='Invalid Role Artist'`); err != nil {
		t.Fatalf("seed invalid artist roles: %v", err)
	}
	seed := similarArtistID(t, ctx, store, "MusicArtist", "Role Seed")
	for _, user := range []string{"restricted", "admin"} {
		similarArtists(t, ctx, store, seed, SimilarQuery{Query: Query{UserID: user, Limit: 20}}, "Inherited Artist", "Own Album Artist", "Own Artist")
	}
}
