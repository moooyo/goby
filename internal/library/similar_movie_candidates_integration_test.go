package library

import (
	"os"
	"strings"
	"testing"
)

func TestStoreSimilarMoviesKeepSeedOutsideCandidateFilters(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	similarInsert(t, ctx, store, "filtered-seed", "Movie", "library-b", "library-b", map[string]any{
		"Genres": []string{"SharedGenre"}, "Tags": []string{"SharedTag"}, "Artists": []string{"SeedOnlyArtist"},
	})
	similarInsert(t, ctx, store, "candidate-folder", "Folder", "library-b", "library-b", nil)
	similarInsert(t, ctx, store, "filtered-match", "Movie", "candidate-folder", "library-b", map[string]any{
		"Genres": []string{"SharedGenre", "CandidateGenre"}, "Tags": []string{"SharedTag"}, "Artists": []string{"CandidateOnlyArtist"},
	})
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "filtered-match", IsFavorite: true, Played: true})
	seedArtist := similarArtistID(t, ctx, store, "MusicArtist", "SeedOnlyArtist")
	candidateArtist := similarArtistID(t, ctx, store, "MusicArtist", "CandidateOnlyArtist")
	yes := true
	query := SimilarQuery{Query: Query{
		UserID: "restricted", ParentID: "candidate-folder", Recursive: true, Ids: []string{"filtered-match"},
		SearchTerm: "filtered-match", Genres: []string{"CandidateGenre"}, IsFavorite: &yes, IsPlayed: &yes,
		ExcludeItemIds: []string{"filtered-seed"}, Limit: 12,
	}, ExcludeArtistIds: []int64{seedArtist}}
	// None of the candidate-only constraints match the seed. Its complete
	// genre/tag population must still qualify the selected candidate.
	similarSet(t, similarQuery(t, ctx, store, "filtered-seed", query), "filtered-match")
	// Exclusions need all candidate credits, including artists the seed lacks.
	query.ExcludeArtistIds = []int64{candidateArtist}
	similarSet(t, similarQuery(t, ctx, store, "filtered-seed", query))
	query.ExcludeArtistIds, query.AlbumIds = nil, []string{"candidate-folder"}
	similarSet(t, similarQuery(t, ctx, store, "filtered-seed", query))
	query.AlbumIds, query.AlbumArtistIds = nil, []int64{candidateArtist}
	similarSet(t, similarQuery(t, ctx, store, "filtered-seed", query))
}

func TestStoreSimilarMoviesRetainDistinctArtistCreditsAndCandidateVisibility(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	similarInsert(t, ctx, store, "artist-seed", "Movie", "library-b", "library-b", map[string]any{
		"Genres": []string{"SharedGenre"}, "Artists": []string{"ArtistOne", "ArtistTwo"},
	})
	similarInsert(t, ctx, store, "hidden-artist-folder", "Folder", "library-b", "library-b", nil)
	for _, fixture := range []struct {
		id, kind, parent, library string
		values                    map[string]any
	}{
		{"two-artists", "Movie", "library-b", "library-b", map[string]any{"Artists": []string{"ArtistOne", "ArtistTwo"}}},
		{"genre-artist", "Movie", "library-b", "library-b", map[string]any{"Genres": []string{"SharedGenre"}, "Artists": []string{"ArtistOne"}}},
		{"duplicate-artist", "Movie", "library-b", "library-b", map[string]any{"Artists": []string{"ArtistOne"}}},
		{"album-artists", "Movie", "library-b", "library-b", map[string]any{"AlbumArtists": []string{"ArtistOne", "ArtistTwo"}}},
		{"invalid-artists", "Movie", "library-b", "library-b", nil},
		{"hidden-artists", "Movie", "library-a", "library-a", map[string]any{"Artists": []string{"ArtistOne", "ArtistTwo"}}},
		{"hidden-ancestor-artists", "Movie", "hidden-artist-folder", "library-b", map[string]any{"Artists": []string{"ArtistOne", "ArtistTwo"}}},
		{"music-video-artists", "MusicVideo", "library-b", "library-b", map[string]any{"Artists": []string{"ArtistOne", "ArtistTwo"}}},
	} {
		similarInsert(t, ctx, store, fixture.id, fixture.kind, fixture.parent, fixture.library, fixture.values)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type)
		SELECT item_id,entity_id,position+100,display_name,credit_group,credit_type
		FROM item_entities WHERE item_id='duplicate-artist';
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type)
		SELECT 'invalid-artists',entity_id,position,display_name,2,'Artist'
		FROM item_entities WHERE item_id='two-artists';
		UPDATE users SET policy=policy||'{"ExcludedSubFolders":["hidden-artist-folder"]}'::jsonb
		WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	query := SimilarQuery{Query: Query{UserID: "restricted", Limit: 20}}
	similarSet(t, similarQuery(t, ctx, store, "artist-seed", query), "two-artists", "genre-artist")
	query.ExcludeArtistIds = []int64{similarArtistID(t, ctx, store, "MusicArtist", "ArtistTwo")}
	similarSet(t, similarQuery(t, ctx, store, "artist-seed", query), "genre-artist")
}

func TestStoreSimilarMoviesPrefilterCreditExpansionBySeedEntities(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		matches int
	}{{"sparse", 3}, {"dense", 1200}} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, store := libraryQueryTestStore(t)
			seedLibraryQueryFixture(t, ctx, store.pool)
			similarInsert(t, ctx, store, "scope-seed", "Movie", "library-b", "library-b", map[string]any{
				"Genres": []string{"SharedOne", "SharedTwo"},
			})
			if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,local_metadata)
				SELECT 'scope-movie-'||n,'library-b','library-b','scope-movie-'||n,'scope-movie-'||n,'Movie',
					jsonb_build_object('Genres',CASE WHEN n <= $1 THEN '["SharedOne","SharedTwo"]'::jsonb
						ELSE '["UnrelatedOne","UnrelatedTwo"]'::jsonb END,
						'Tags','["CandidateTag"]'::jsonb,'Studios','["CandidateStudio"]'::jsonb)
				FROM generate_series(1,1200) n`, fixture.matches); err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities(id,local_metadata)
				FROM items WHERE id LIKE 'scope-movie-%'`); err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, `ANALYZE items; ANALYZE item_entities; ANALYZE catalog_entities`); err != nil {
				t.Fatal(err)
			}
			tx, access, err := store.beginSubjectRead(ctx, Subject{UserID: "restricted"})
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			query := SimilarQuery{Query: Query{UserID: "restricted", Recursive: true, Limit: 1}}
			current, args := similarRelationSQL("scope-seed", "Movie", query, access, "")
			var expandedItems, expandedCredits, candidates, score int
			if err := tx.QueryRow(ctx, current+`SELECT
				(SELECT count(*) FROM similar_scope), (SELECT count(*) FROM similar_credits),
				(SELECT count(*) FROM similar_scores), (SELECT sum(score)::bigint FROM similar_scores)`, args...).Scan(
				&expandedItems, &expandedCredits, &candidates, &score); err != nil {
				t.Fatal(err)
			}
			if expandedItems != fixture.matches+1 || expandedCredits != fixture.matches*4+2 ||
				candidates != fixture.matches || score != fixture.matches*2 {
				t.Fatalf("seed candidate scope: items=%d credits=%d candidates=%d score=%d, want %d complete matching movies plus the seed",
					expandedItems, expandedCredits, candidates, score, fixture.matches)
			}
			// Remove only the ID restriction to retain an exhaustive authorized
			// scoring reference. Every qualifying score must survive the reduction;
			// zero-score unrelated movies need not enter the production relation.
			join := " JOIN similar_movie_scope_ids matching ON matching.id = i.id"
			if strings.Count(current, join) != 1 {
				t.Fatal("the Movie query no longer exposes one seed-driven candidate restriction")
			}
			complete := strings.Replace(current, join, "", 1)
			selection := `SELECT id,score FROM similar_scores WHERE score >= 2 ORDER BY id`
			projection := `SELECT COALESCE(jsonb_agg(to_jsonb(result)),'[]'::jsonb)::text FROM (` + selection + `) result`
			var completeScores, currentScores string
			if err := tx.QueryRow(ctx, complete+projection, args...).Scan(&completeScores); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(ctx, current+projection, args...).Scan(&currentScores); err != nil {
				t.Fatal(err)
			}
			if completeScores != currentScores {
				t.Fatal("seed-driven Movie candidates changed exhaustive qualifying scores")
			}
			// Plans are optional observations, never elapsed-time assertions. Dense
			// seeds retain most candidates and may not benefit from the extra join.
			if os.Getenv("GOBY_TEST_DISCOVERY_QUERY_PERFORMANCE") == "1" {
				if _, err := tx.Exec(ctx, `SET LOCAL jit=off`); err != nil {
					t.Fatal(err)
				}
				discoveryProfileCompare(t, ctx, tx, "similar_movie_"+fixture.name, complete, args, current, args, selection)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
