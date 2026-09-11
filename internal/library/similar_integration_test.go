package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func similarInsert(t *testing.T, ctx context.Context, store *Store, id, kind, parent, libraryID string, values map[string]any) {
	t.Helper()
	if values == nil {
		values = map[string]any{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	folder := kind == "MusicAlbum" || kind == "Series" || kind == "Season" || kind == "Folder"
	if _, err := tx.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder, media, local_metadata)
		VALUES ($1,$2,$3,$1,$1,$4,$5,CASE WHEN $5 THEN NULL ELSE '{"DurationTicks":120000000}'::jsonb END,$6::jsonb)`,
		id, libraryID, parent, kind, folder, raw); err != nil {
		t.Fatalf("insert similar catalog fixture: %v", err)
	}
	if err := syncItemEntities(ctx, tx, id, raw); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func similarMovieFixture(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	seedLibraryQueryFixture(t, ctx, store.pool)
	all := map[string]any{"ProductionYear": 2000, "Genres": []string{"Drama", "Adventure"},
		"Tags": []string{"SharedOne", "SharedTwo"}, "Studios": []string{"SharedStudio"},
		"People": []map[string]string{{"Name": "CommonActor", "Type": "Actor"}, {"Name": "CommonDirector", "Type": "Director"}}}
	for _, fixture := range []struct {
		id     string
		values map[string]any
	}{
		{"Seed", all}, {"AllMatches", all},
		{"GenreBoth", map[string]any{"Genres": []string{"Drama", "Adventure"}}},
		{"GenreOne", map[string]any{"Genres": []string{"Drama"}}},
		{"TagBoth", map[string]any{"Tags": []string{"SharedOne", "SharedTwo"}}},
		{"TagOne", map[string]any{"Tags": []string{"SharedOne"}}},
		{"Studio", map[string]any{"Studios": []string{"SharedStudio"}}},
		{"Actor", map[string]any{"People": []map[string]string{{"Name": "CommonActor", "Type": "Actor"}}}},
		{"Director", map[string]any{"People": []map[string]string{{"Name": "CommonDirector", "Type": "Director"}}}},
		{"YearNear", map[string]any{"ProductionYear": 2001}},
		{"YearFar", map[string]any{"ProductionYear": 2025}}, {"NoShared", map[string]any{}},
	} {
		if _, exists := fixture.values["ProductionYear"]; !exists {
			fixture.values["ProductionYear"] = 2000
		}
		similarInsert(t, ctx, store, fixture.id, "Movie", "library-b", "library-b", fixture.values)
	}
	similarInsert(t, ctx, store, "HiddenAllMatches", "Movie", "library-a", "library-a", all)
	similarInsert(t, ctx, store, "DuplicateActorOnly", "Movie", "library-b", "library-b", map[string]any{
		"People": []map[string]string{{"Name": "CommonActor", "Type": "Actor"}, {"Name": "CommonActor", "Type": "Director"}},
	})
	return ctx, store
}

func similarQuery(t *testing.T, ctx context.Context, store *Store, seed string, query SimilarQuery) ItemResult {
	t.Helper()
	result, err := store.QuerySimilar(ctx, seed, query)
	if err != nil {
		t.Fatalf("query similar fixture %q: %v", seed, err)
	}
	if result.Items == nil || result.TotalRecordCount != len(result.Items) {
		t.Fatalf("similar page lost non-null items or returned-page count: %+v", result)
	}
	return result
}

func similarSet(t *testing.T, result ItemResult, expected ...string) {
	t.Helper()
	actual := queryItemIDs(result.Items)
	sort.Strings(actual)
	sort.Strings(expected)
	if len(actual) != len(expected) || strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("similar set = %v, want %v", actual, expected)
	}
}

func similarState(t *testing.T, ctx context.Context, store *Store) string {
	t.Helper()
	var snapshot string
	if err := store.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
		'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m),
		'credits',(SELECT jsonb_agg(to_jsonb(e) ORDER BY item_id,entity_id,credit_group,position) FROM item_entities e),
		'userdata',(SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id,item_id) FROM user_item_data u),
		'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestStoreSimilarMoviesMatchRecordedSetsAndPageContract(t *testing.T) {
	ctx, store := similarMovieFixture(t)
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 12}}
	before := similarState(t, ctx, store)
	// The two equal-score positions changed order in the reference capture;
	// assert their set and the strictly higher first candidate, not an RNG event.
	for index := 0; index < 2; index++ {
		result := similarQuery(t, ctx, store, "Seed", query)
		similarSet(t, result, "AllMatches", "GenreBoth", "TagBoth")
		if result.Items[0].ID != "AllMatches" {
			t.Fatal("default ranking did not retain the strictly highest-scoring candidate")
		}
	}
	query.ExcludeItemIds = []string{"AllMatches"}
	similarSet(t, similarQuery(t, ctx, store, "Seed", query), "GenreBoth", "TagBoth")
	query.ExcludeItemIds = nil
	query.Limit = 0
	similarSet(t, similarQuery(t, ctx, store, "Seed", query), "AllMatches")
	query.Limit, query.StartIndex = 3, 3
	similarSet(t, similarQuery(t, ctx, store, "Seed", query))
	query.StartIndex, query.ExplicitSort, query.SortBy, query.SortOrder = 0, true, "SortName", "Descending"
	result := similarQuery(t, ctx, store, "Seed", query)
	if got := queryItemIDs(result.Items); !reflect.DeepEqual(got, []string{"TagBoth", "GenreBoth", "AllMatches"}) {
		t.Fatalf("explicit ordering did not replace score order: %v", got)
	}
	query.Limit, query.StartIndex = 1, 1
	similarSet(t, similarQuery(t, ctx, store, "Seed", query), "GenreBoth")
	query.Limit, query.StartIndex = 12, 0
	for _, seed := range []string{"NoShared", "YearFar"} {
		similarSet(t, similarQuery(t, ctx, store, seed, query))
	}
	query.ParentID, query.Recursive = "library-b", true
	similarSet(t, similarQuery(t, ctx, store, "Seed", query), "AllMatches", "GenreBoth", "TagBoth")
	if similarState(t, ctx, store) != before {
		t.Fatal("similar reads changed catalog, controls, credits, credentials, or user data")
	}
}

func similarMusicFixture(t *testing.T) (context.Context, *Store, int64, int64) {
	t.Helper()
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	for _, album := range []struct {
		id      string
		artists []string
		album   string
	}{
		{"SeedAlbum", []string{"ArtistA"}, "ArtistA"}, {"SameArtistAlbum", []string{"ArtistA"}, "ArtistA"},
		{"DifferentArtistAlbum", []string{"ArtistB"}, "ArtistB"}, {"MixedAlbum", []string{"ArtistA", "ArtistB"}, "ArtistA"},
	} {
		similarInsert(t, ctx, store, album.id, "MusicAlbum", "library-b", "library-b",
			map[string]any{"Artists": album.artists, "AlbumArtists": []string{album.album}})
		for index, artist := range album.artists {
			similarInsert(t, ctx, store, fmt.Sprintf("%s-Track%d", album.id, index+1), "Audio", album.id, "library-b",
				map[string]any{"Artists": []string{artist}, "AlbumArtists": []string{album.album}})
		}
	}
	var artistA, artistB int64
	if err := store.pool.QueryRow(ctx, "SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='ArtistA'").Scan(&artistA); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, "SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='ArtistB'").Scan(&artistB); err != nil {
		t.Fatal(err)
	}
	return ctx, store, artistA, artistB
}

type similarScoreTracer struct {
	statement string
	arguments []any
}

func (trace *similarScoreTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "JOIN similar_scores ranked") {
		trace.statement, trace.arguments = data.SQL, append([]any(nil), data.Args...)
	}
	return ctx
}

func (*similarScoreTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestStoreSimilarMovieCombinationsExcludePeopleFromQualificationAndOrder(t *testing.T) {
	ctx, store := similarMovieFixture(t)
	for _, fixture := range []struct {
		id     string
		values map[string]any
	}{
		{"GenreTag", map[string]any{"Genres": []string{"Drama"}, "Tags": []string{"SharedOne"}}},
		{"GenreStudio", map[string]any{"Genres": []string{"Drama"}, "Studios": []string{"SharedStudio"}}},
		{"TwoPeople", map[string]any{"People": []map[string]string{{"Name": "CommonActor", "Type": "Actor"}, {"Name": "CommonDirector", "Type": "Director"}}}},
		{"GenreActor", map[string]any{"Genres": []string{"Drama"}, "People": []map[string]string{{"Name": "CommonActor", "Type": "Actor"}}}},
		{"TwoGenresStudio", map[string]any{"Genres": []string{"Drama", "Adventure"}, "Studios": []string{"SharedStudio"}}},
		{"TwoGenresActor", map[string]any{"Genres": []string{"Drama", "Adventure"}, "People": []map[string]string{{"Name": "CommonActor", "Type": "Actor"}}}},
	} {
		fixture.values["ProductionYear"] = 2000
		similarInsert(t, ctx, store, fixture.id, "Movie", "library-b", "library-b", fixture.values)
	}
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 20}}
	trace := &similarScoreTracer{}
	config := store.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	result := similarQuery(t, ctx, &Store{pool: reader}, "Seed", query)
	similarSet(t, result, "AllMatches", "TwoGenresStudio", "GenreBoth", "TagBoth", "GenreTag", "GenreStudio", "TwoGenresActor")
	if result.Items[0].ID != "AllMatches" || result.Items[1].ID != "TwoGenresStudio" {
		t.Fatal("the observed three-point genre/studio candidate did not rank between the five-point seed match and two-point candidates")
	}
	// Inspect the score produced by the actual production query, changing only
	// its final projection. This proves the tied tier without requiring an RNG
	// event or reimplementing the scoring formula in the fixture.
	if strings.Contains(trace.statement, "album_ancestors") {
		t.Fatal("the ordinary Movie query retained an unreachable recursive music subplan")
	}
	scoreQuery := strings.Replace(trace.statement, "SELECT "+similarItemColumns("Movie")+" FROM items i", "SELECT i.id, ranked.score FROM items i", 1)
	if scoreQuery == trace.statement {
		t.Fatal("the actual similar query did not expose its expected result projection")
	}
	rows, err := store.pool.Query(ctx, scoreQuery, trace.arguments...)
	if err != nil {
		t.Fatal(err)
	}
	scores := make(map[string]int64)
	for rows.Next() {
		var id string
		var score int64
		if err := rows.Scan(&id, &score); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		scores[id] = score
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !reflect.DeepEqual(scores, map[string]int64{
		"AllMatches": 5, "TwoGenresStudio": 3, "GenreBoth": 2, "TagBoth": 2,
		"GenreTag": 2, "GenreStudio": 2, "TwoGenresActor": 2,
	}) {
		t.Fatalf("observed-combination score tiers = %v, error = %v", scores, err)
	}
	query.ExplicitSort, query.SortBy = true, "Name"
	result = similarQuery(t, ctx, store, "Seed", query)
	seen := map[string]Item{}
	for _, item := range result.Items {
		seen[item.ID] = item
	}
	if len(seen["TwoGenresActor"].Entities.People) != 1 || len(seen["AllMatches"].Entities.People) != 2 {
		t.Fatal("ignoring Person for similarity changed its ordinary item projection")
	}
}

func TestStoreSimilarMusicUsesDirectedRoleMatchesAndPhysicalAlbum(t *testing.T) {
	ctx, store, artistA, artistB := similarMusicFixture(t)
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 12}}
	for _, test := range []struct {
		seed string
		want []string
	}{
		{"SeedAlbum", []string{"SameArtistAlbum", "MixedAlbum"}},
		{"MixedAlbum", []string{"SeedAlbum", "SameArtistAlbum", "DifferentArtistAlbum"}},
		{"DifferentArtistAlbum", nil},
		{"SeedAlbum-Track1", []string{"SameArtistAlbum-Track1", "MixedAlbum-Track1"}},
		{"DifferentArtistAlbum-Track1", nil},
		{"MixedAlbum-Track1", []string{"SameArtistAlbum-Track1", "SeedAlbum-Track1", "MixedAlbum-Track2"}},
		{"MixedAlbum-Track2", []string{"SameArtistAlbum-Track1", "SeedAlbum-Track1", "MixedAlbum-Track1", "DifferentArtistAlbum-Track1"}},
	} {
		t.Run(test.seed, func(t *testing.T) {
			similarSet(t, similarQuery(t, ctx, store, test.seed, query), test.want...)
		})
	}
	result := similarQuery(t, ctx, store, "MixedAlbum-Track2", query)
	if result.Items[0].ID != "MixedAlbum-Track1" {
		t.Fatal("the shared physical album did not rank the two-role sibling first")
	}
	query.ExcludeArtistIds = []int64{artistB}
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum", query), "SameArtistAlbum")
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum-Track1", query), "SameArtistAlbum-Track1", "MixedAlbum-Track1")
	query.ExcludeArtistIds = []int64{artistA, artistA}
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum", query))
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum-Track1", query))
	similarSet(t, similarQuery(t, ctx, store, "MixedAlbum-Track1", query))
	similarSet(t, similarQuery(t, ctx, store, "MixedAlbum-Track2", query), "DifferentArtistAlbum-Track1")
	similarSet(t, similarQuery(t, ctx, store, "MixedAlbum", query), "DifferentArtistAlbum")
	query.ExcludeArtistIds = []int64{artistB}
	similarSet(t, similarQuery(t, ctx, store, "MixedAlbum-Track2", query), "MixedAlbum-Track1", "SameArtistAlbum-Track1", "SeedAlbum-Track1")
	query.ExcludeArtistIds, query.AlbumIds = nil, []string{"MixedAlbum"}
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum-Track1", query), "MixedAlbum-Track1")
	query.AlbumIds, query.AlbumArtistIds = nil, []int64{artistB}
	similarSet(t, similarQuery(t, ctx, store, "MixedAlbum-Track2", query), "DifferentArtistAlbum-Track1")
	query.AlbumArtistIds = []int64{artistA}
	similarSet(t, similarQuery(t, ctx, store, "MixedAlbum-Track2", query), "MixedAlbum-Track1", "SameArtistAlbum-Track1", "SeedAlbum-Track1")
	query.AlbumArtistIds, query.Limit = nil, 0
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum", query))
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum-Track1", query))
	query.Limit, query.StartIndex, query.SortBy, query.SortOrder, query.ExplicitSort = 1, 1, "Name", "Ascending", true
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum", query), "SameArtistAlbum")
}

func TestStoreSimilarEffectiveCreditsRejectDuplicatesAndParentArtistLeakage(t *testing.T) {
	ctx, store, artistA, artistB := similarMusicFixture(t)
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 100}, ExcludeArtistIds: []int64{artistB}}
	// Model a legacy Audio with no own album-artist group. It inherits only
	// MixedAlbum's AlbumArtist A, never that album's aggregate Artist B.
	if _, err := store.pool.Exec(ctx, "DELETE FROM item_entities WHERE item_id='MixedAlbum-Track1' AND credit_group=2"); err != nil {
		t.Fatal(err)
	}
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum-Track1", query), "SameArtistAlbum-Track1", "MixedAlbum-Track1")
	query.ExcludeArtistIds = []int64{artistA}
	similarSet(t, similarQuery(t, ctx, store, "SeedAlbum-Track1", query))
	// A real own B credit suppresses parent A even when the filter asks for A.
	if _, err := store.pool.Exec(ctx, `UPDATE item_entities SET entity_id=$1, display_name='ArtistB'
		WHERE item_id='MixedAlbum-Track2' AND credit_group=2 AND credit_type='AlbumArtist'`, artistB); err != nil {
		t.Fatal(err)
	}
	similarSet(t, similarQuery(t, ctx, store, "DifferentArtistAlbum-Track1", query), "MixedAlbum-Track2")
	similarInsert(t, ctx, store, "UnparentedA", "Audio", "library-b", "library-b", map[string]any{"Artists": []string{"ArtistA"}})
	similarInsert(t, ctx, store, "DuplicateOnlyA", "Audio", "library-b", "library-b", map[string]any{"Artists": []string{"ArtistA"}})
	if _, err := store.pool.Exec(ctx, `INSERT INTO item_entities (item_id,entity_id,position,display_name,credit_group,credit_type)
		SELECT 'DuplicateOnlyA',$1::bigint,n,'ArtistA',1,'Artist' FROM generate_series(101,105) n
		UNION ALL SELECT 'DuplicateOnlyA',$1::bigint,201,'ArtistA',0,'AlbumArtist'
		UNION ALL SELECT 'DuplicateOnlyA',$1::bigint,202,'ArtistA',2,'Artist'`, artistA); err != nil {
		t.Fatal(err)
	}
	query.ExcludeArtistIds, query.Ids = nil, []string{"DuplicateOnlyA"}
	similarSet(t, similarQuery(t, ctx, store, "UnparentedA", query))
	// Two absent physical album IDs must not create the extra qualifying point.
	query.Ids = []string{"UnparentedA"}
	similarSet(t, similarQuery(t, ctx, store, "DuplicateOnlyA", query))
	query.Ids = nil
	query.ExcludeArtistIds = []int64{artistB}
	before := similarState(t, ctx, store)
	_ = similarQuery(t, ctx, store, "SeedAlbum-Track1", query)
	if similarState(t, ctx, store) != before {
		t.Fatal("effective-role reads rewrote credits or inherited values")
	}
}

func TestStoreSimilarAppliesAccessBeforeRankingAndRejectsEntitySeeds(t *testing.T) {
	ctx, store := similarMovieFixture(t)
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 1, ExcludeItemIds: []string{"AllMatches"}}}
	result := similarQuery(t, ctx, store, "Seed", query)
	if len(result.Items) != 1 || result.Items[0].ID == "HiddenAllMatches" {
		t.Fatal("an inaccessible higher-score candidate consumed the visible page")
	}
	for _, change := range []struct{ seed, user, parent string }{
		{"HiddenAllMatches", "restricted", ""}, {"Seed", "none", ""}, {"Seed", "restricted", "library-a"},
	} {
		q := SimilarQuery{Query: Query{UserID: change.user, ParentID: change.parent, Limit: 0}}
		if _, err := store.QuerySimilar(ctx, change.seed, q); !errors.Is(err, ErrNotFound) {
			t.Fatalf("zero-page seed or parent authorization = %v, want ErrNotFound", err)
		}
	}
	if _, err := store.QuerySimilar(ctx, "Seed", SimilarQuery{Query: Query{UserID: "disabled"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled ordinary user = %v, want ErrForbidden", err)
	}
	var genreID int64
	if err := store.pool.QueryRow(ctx, "SELECT id FROM catalog_entities WHERE kind='Genre' AND name='Drama'").Scan(&genreID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QuerySimilar(ctx, strconv.FormatInt(genreID, 10), SimilarQuery{Query: Query{UserID: "restricted"}}); !errors.Is(err, ErrUnsupportedFilter) {
		t.Fatalf("visible entity seed was represented as a fabricated empty item result: %v", err)
	}
	if _, err := store.QuerySimilar(ctx, strconv.FormatInt(genreID, 10), SimilarQuery{Query: Query{UserID: "none"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden entity seed leaked availability: %v", err)
	}
	key := seedCatalogApplicationKey(t, ctx, store.pool, "similar-key", true)
	keyQuery := SimilarQuery{Query: Query{ApplicationCredentialID: key.ApplicationCredentialID, Limit: 100}}
	result = similarQuery(t, ctx, store, "Seed", keyQuery)
	similarSet(t, result, "AllMatches", "GenreBoth", "TagBoth", "HiddenAllMatches")
	for _, item := range result.Items {
		if item.UserData != nil || !item.CanPlay {
			t.Fatal("userless application authority acquired a user's history or lost its independent playback authority")
		}
	}
	keyQuery.UserID = "restricted"
	similarSet(t, similarQuery(t, ctx, store, "Seed", keyQuery), "AllMatches", "GenreBoth", "TagBoth")
	if _, err := store.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QuerySimilar(ctx, "Seed", keyQuery); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked application key remained authorized: %v", err)
	}
}

func TestStoreSimilarRanksBeyondOrdinaryPageAndUsesEffectiveMetadataControls(t *testing.T) {
	fixtureStarted := time.Now()
	ctx, store := similarMovieFixture(t)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items (id,library_id,parent_id,name,sort_name,type,local_metadata)
		SELECT 'bulk-'||n,'library-b','library-b','bulk-'||n,'bulk-'||n,'Movie','{"Genres":["Drama","Adventure"]}'::jsonb
		FROM generate_series(1,1050) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, "SELECT sync_catalog_item_entities(id,local_metadata) FROM items WHERE id LIKE 'bulk-%'"); err != nil {
		t.Fatal(err)
	}
	similarInsert(t, ctx, store, "zzzzHighest", "Movie", "library-b", "library-b", map[string]any{
		"Genres": []string{"Drama", "Adventure"}, "Tags": []string{"SharedOne", "SharedTwo"}, "Studios": []string{"SharedStudio"},
		"People": []map[string]string{{"Name": "CommonActor", "Type": "Actor"}, {"Name": "CommonDirector", "Type": "Director"}},
	})
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 1, ExcludeItemIds: []string{"AllMatches"}}}
	t.Logf("similar fixture seeded 1050 low-score candidates and a final high-score item in %s; starting full-candidate query", time.Since(fixtureStarted))
	var largeResult ItemResult
	func() {
		queryStarted := time.Now()
		defer func() {
			t.Logf("similar full-candidate query elapsed %s; fixture elapsed %s", time.Since(queryStarted), time.Since(fixtureStarted))
		}()
		largeResult = similarQuery(t, ctx, store, "Seed", query)
	}()
	similarSet(t, largeResult, "zzzzHighest")
	actor := metadataEditTestActor(t, ctx, store.pool, "similar-editor")
	before := metadataEditTestDetail(t, ctx, store, actor, "GenreOne")
	changed := metadataEditTestUpdate(t, ctx, store, actor, before,
		map[string]json.RawMessage{"Genres": json.RawMessage(`["Drama","Adventure"]`)}, []string{"Genres"})
	metadataEditTestUpdate(t, ctx, store, actor, changed, map[string]json.RawMessage{}, []string{"Genres"})
	query.Ids, query.Limit = []string{"GenreOne"}, 12
	result := similarQuery(t, ctx, store, "Seed", query)
	similarSet(t, result, "GenreOne")
	if result.Items[0].Metadata == nil || !reflect.DeepEqual(result.Items[0].Metadata.Genres, []string{"Drama", "Adventure"}) {
		t.Fatal("similar projection did not retain the effective locked genre values")
	}
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "GenreOne", PlayCount: 7, IsFavorite: true})
	state := similarState(t, ctx, store)
	query.SortBy, query.ExplicitSort = "PlayCount", true
	result = similarQuery(t, ctx, store, "Seed", query)
	if result.Items[0].UserData == nil || result.Items[0].UserData.PlayCount != 7 || !result.Items[0].UserData.IsFavorite {
		t.Fatal("similar item omitted the selected user's existing state")
	}
	if similarState(t, ctx, store) != state {
		t.Fatal("similar query changed effective metadata controls or user data")
	}
}

type similarSeedTraceKey struct{}

type similarSnapshotTracer struct {
	writer    *pgxpool.Pool
	once      sync.Once
	err       error
	begins    []string
	writerRan bool
}

func (trace *similarSnapshotTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(strings.ToLower(data.SQL), "begin") {
		trace.begins = append(trace.begins, strings.ToLower(data.SQL))
	}
	return context.WithValue(ctx, similarSeedTraceKey{}, strings.HasPrefix(data.SQL, "SELECT i.type FROM items i"))
}

func (trace *similarSnapshotTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if selected, _ := ctx.Value(similarSeedTraceKey{}).(bool); selected {
		trace.once.Do(func() {
			trace.writerRan = true
			_, trace.err = trace.writer.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id='restricted';
				DELETE FROM item_entities WHERE item_id='AllMatches';
				UPDATE user_item_data SET play_count=99 WHERE user_id='restricted' AND item_id='AllMatches'`)
		})
	}
}

func TestStoreSimilarAuthorizationScoringAndUserDataShareOneReadSnapshot(t *testing.T) {
	ctx, store := similarMovieFixture(t)
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "AllMatches", PlayCount: 3})
	trace := &similarSnapshotTracer{writer: store.pool}
	config := store.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	result := similarQuery(t, ctx, &Store{pool: reader}, "Seed", SimilarQuery{Query: Query{UserID: "restricted", Limit: 1}})
	similarSet(t, result, "AllMatches")
	if !trace.writerRan || trace.err != nil || len(trace.begins) != 1 || !strings.Contains(trace.begins[0], "repeatable read") ||
		!strings.Contains(trace.begins[0], "read only") || result.Items[0].UserData == nil || result.Items[0].UserData.PlayCount != 3 {
		t.Fatalf("similar did not retain one authorized read-only snapshot: begins=%v, writer_ran=%v, writer=%v", trace.begins, trace.writerRan, trace.err)
	}
	if _, err := store.QuerySimilar(ctx, "Seed", SimilarQuery{Query: Query{UserID: "restricted", Limit: 1}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the next independent read did not observe committed permission revocation: %v", err)
	}
}

func TestQuerySimilarRejectsInvalidInputsBeforeDatabaseAccess(t *testing.T) {
	for _, test := range []struct {
		seed  string
		query SimilarQuery
	}{
		{"", SimilarQuery{Query: Query{UserID: "user", Limit: 1}}},
		{"bad\x00seed", SimilarQuery{Query: Query{UserID: "user", Limit: 1}}},
		{"seed", SimilarQuery{Query: Query{UserID: "user", Limit: -1}}},
		{"seed", SimilarQuery{Query: Query{UserID: "user", Limit: 1001}}},
		{"seed", SimilarQuery{Query: Query{UserID: "user", StartIndex: -1}}},
		{"seed", SimilarQuery{Query: Query{UserID: "user", SortBy: "Name; DROP TABLE items"}, ExplicitSort: true}},
		{"seed", SimilarQuery{Query: Query{UserID: "user"}, ExcludeArtistIds: []int64{0}}},
		{"seed", SimilarQuery{Query: Query{UserID: "user"}, ExcludeArtistIds: []int64{-1}}},
		{"seed", SimilarQuery{Query: Query{UserID: "user"}, ExcludeArtistIds: make([]int64, 1025)}},
	} {
		if _, err := (&Store{}).QuerySimilar(context.Background(), test.seed, test.query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid similar request reached database work: %v", err)
		}
	}
}
