package library

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// This opt-in profile compares frozen pre-L02/L03 SQL with current SQL on one
// deterministic catalog and snapshot. Plans are evidence, not pass thresholds:
// retain their actual rows, loops, buffers and temporary I/O when interpreting
// execution time. Alternate sample order to expose cache/order sensitivity.
func TestDiscoveryQueryPlanProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_DISCOVERY_QUERY_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_DISCOVERY_QUERY_PERFORMANCE=1 enables the discovery query profile")
	}
	ctx, _, store, _, _ := libraryIntegrationStoreWithTimeout(t, &libraryFixtureProber{}, 5*time.Minute)
	seedLibraryQueryFixture(t, ctx, store.pool)
	seedDiscoveryQueryProfile(t, ctx, store)
	tx, access, err := store.beginSubjectRead(ctx, Subject{UserID: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `SET LOCAL jit=off`); err != nil {
		t.Fatal(err)
	}
	seed := resolvedMusicMixSeed{kind: "Song", itemID: "profile-track-000001"}
	mixQuery := InstantMixQuery{Query: Query{UserID: "restricted", Recursive: true}}
	originalMix, mixArgs := discoveryProfileOriginalMixSQL(seed, mixQuery, access, "")
	currentMix, currentMixArgs := instantMixRelationSQL(seed, mixQuery, access, "")
	var candidates int
	if err := tx.QueryRow(ctx, currentMix+`SELECT count(*) FROM mix_candidates`, currentMixArgs...).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if candidates != 2400 {
		t.Fatalf("profile mix has %d candidates, want 2400 authorized playable tracks", candidates)
	}
	discoveryProfileCompare(t, ctx, tx, "instant_mix_count", originalMix, mixArgs, currentMix, currentMixArgs,
		`SELECT count(*) AS total FROM mix_candidates`)
	allScores := `SELECT COALESCE(jsonb_agg(to_jsonb(result)),'[]'::jsonb)::text FROM (SELECT id,score FROM mix_scores ORDER BY id) result`
	var originalScores, currentScores string
	if err := tx.QueryRow(ctx, originalMix+allScores, mixArgs...).Scan(&originalScores); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, currentMix+allScores, currentMixArgs...).Scan(&currentScores); err != nil {
		t.Fatal(err)
	}
	if originalScores != currentScores {
		t.Fatal("profile mix changed the complete candidate score relation")
	}
	page := `, mix_page AS MATERIALIZED (
		SELECT i.id,row_number() OVER (ORDER BY ranked.score DESC,lower(i.sort_name),i.id) AS ordinal
		FROM mix_scores ranked JOIN items i ON i.id=ranked.id ORDER BY ordinal LIMIT 25 OFFSET 20
	) `
	discoveryProfileCompare(t, ctx, tx, "instant_mix_page", originalMix+page, mixArgs, currentMix+page, currentMixArgs,
		`SELECT i.id,page.ordinal FROM mix_page page JOIN items i ON i.id=page.id ORDER BY page.ordinal`)
	no := false
	for _, test := range []struct {
		name  string
		query SearchHintsQuery
	}{
		{"selective_mixed", SearchHintsQuery{SearchTerm: "Needle"}},
		{"broad_mixed", SearchHintsQuery{SearchTerm: "Profile"}},
		{"video_entities", SearchHintsQuery{SearchTerm: "Needle", IncludeMedia: &no, MediaTypes: []string{"Video"}}},
	} {
		originalSearch, searchArgs := discoveryProfileOriginalSearchSQL(test.query, access)
		currentSearch, currentSearchArgs := searchHintsSQL(test.query, access)
		discoveryProfileCompare(t, ctx, tx, "search_"+test.name+"_count", originalSearch, searchArgs, currentSearch, currentSearchArgs,
			`SELECT count(*) AS total FROM eligible_hints`)
		discoveryProfileCompare(t, ctx, tx, "search_"+test.name+"_page", originalSearch, searchArgs, currentSearch, currentSearchArgs,
			`SELECT owner_kind,id,name,type,match_rank FROM eligible_hints
			ORDER BY match_rank,lower(name) COLLATE "C",name COLLATE "C",owner_kind COLLATE "C",type COLLATE "C",id COLLATE "C"
			LIMIT 25 OFFSET 2`)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func discoveryProfileCompare(t *testing.T, ctx context.Context, tx pgx.Tx, name, original string, originalArgs []any, current string, currentArgs []any, selection string) {
	t.Helper()
	shapes := []struct {
		name   string
		prefix string
		args   []any
	}{{"original", original, originalArgs}, {"current", current, currentArgs}}
	var expected string
	for index, shape := range shapes {
		var result string
		if err := tx.QueryRow(ctx, shape.prefix+`SELECT COALESCE(jsonb_agg(to_jsonb(result)),'[]'::jsonb)::text FROM (`+selection+`) result`, shape.args...).Scan(&result); err != nil {
			t.Fatalf("%s %s result: %v", name, shape.name, err)
		}
		if index == 0 {
			expected = result
		} else if result != expected {
			t.Fatalf("%s changed results: original=%s current=%s", name, expected, result)
		}
	}
	for sample := range 3 {
		for offset := range shapes {
			shape := shapes[(sample+offset)%len(shapes)]
			var plan json.RawMessage
			if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) `+shape.prefix+selection, shape.args...).Scan(&plan); err != nil {
				t.Fatalf("%s %s plan: %v", name, shape.name, err)
			}
			t.Logf("DISCOVERY_QUERY_PROFILE query=%s shape=%s sample=%d actual_plan=%s", name, shape.name, sample+1, plan)
		}
	}
}

func seedDiscoveryQueryProfile(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	searchHintTestExec(t, ctx, store, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		SELECT 'profile-album-' || lpad(n::text,3,'0'),
		CASE WHEN n%5=0 THEN 'library-a' ELSE 'library-b' END,
		CASE WHEN n%5=0 THEN 'library-a' ELSE 'library-b' END,
		'Profile album ' || lpad(n::text,3,'0'),'Profile album ' || lpad(n::text,3,'0'),'MusicAlbum',true
		FROM generate_series(1,120) n;
		INSERT INTO catalog_entities(kind,name)
		SELECT kind,CASE WHEN n=1 THEN 'Needle ' || lower(kind) ELSE 'Profile ' || lower(kind) || ' ' || lpad(n::text,3,'0') END
		FROM (VALUES ('MusicArtist',200),('Genre',60),('Tag',40),('Studio',25),('Person',300)) kinds(kind,total)
		CROSS JOIN LATERAL generate_series(1,total) n;
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_type,credit_group)
		SELECT album.id,entity.id,1,entity.name,'AlbumArtist',2
		FROM generate_series(1,120) n JOIN items album ON album.id='profile-album-' || lpad(n::text,3,'0')
		JOIN catalog_entities entity ON entity.kind='MusicArtist' AND entity.name=CASE
		WHEN n=1 THEN 'Needle musicartist' ELSE 'Profile musicartist ' || lpad(n::text,3,'0') END`)
	searchHintTestExec(t, ctx, store, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path,file_identity,file_size,modified_at,media)
		SELECT 'profile-track-' || lpad(n::text,6,'0'),album.library_id,root.id,album.id,
		CASE WHEN n%997=0 THEN 'Needle title ' ELSE 'Profile track ' END || lpad(n::text,6,'0'),
		'Profile track ' || lpad(n::text,6,'0'),'Audio',false,
		root.path || '/track-' || n::text || '.flac','track-' || n::text || '.flac','profile-' || n::text,100,
		'2026-09-20T00:00:00Z'::timestamptz,
		jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1,'DurationTicks',120000000,
		'Streams',jsonb_build_array(jsonb_build_object('Index',0,'CodecType','audio','Codec','flac')))
		FROM generate_series(1,3000) n JOIN items album ON album.id='profile-album-' || lpad((((n-1)%120)+1)::text,3,'0')
		JOIN library_roots root ON root.library_id=album.library_id`, media.CurrentProbeVersion)
	searchHintTestExec(t, ctx, store, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		SELECT 'profile-movie-' || lpad(n::text,6,'0'),
		CASE WHEN n%5=0 THEN 'library-a' ELSE 'library-b' END,
		CASE WHEN n%5=0 THEN 'library-a' ELSE 'library-b' END,
		CASE WHEN n%997=0 THEN 'Needle title ' ELSE 'Profile movie ' END || lpad(n::text,6,'0'),
		'Profile movie ' || lpad(n::text,6,'0'),'Movie',false FROM generate_series(1,5000) n;
		WITH source_items AS (
			SELECT 'profile-track-' || lpad(n::text,6,'0') AS id,n,'Audio'::text AS type FROM generate_series(1,3000) n
			UNION ALL SELECT 'profile-movie-' || lpad(n::text,6,'0'),n,'Movie' FROM generate_series(1,5000) n
		)
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_type,credit_group)
		SELECT item.id,entity.id,1,entity.name,CASE WHEN entity.kind='MusicArtist' THEN 'Artist' ELSE '' END,
		CASE WHEN entity.kind='MusicArtist' THEN 1 ELSE 0 END
		FROM source_items item
		CROSS JOIN (VALUES ('MusicArtist',200),('Genre',60),('Tag',40),('Studio',25),('Person',300)) kinds(kind,total)
		JOIN catalog_entities entity ON entity.kind=kinds.kind AND entity.name=CASE
		WHEN (item.n-1)%kinds.total=0 THEN 'Needle ' || lower(kinds.kind)
		ELSE 'Profile ' || lower(kinds.kind) || ' ' || lpad((((item.n-1)%kinds.total)+1)::text,3,'0') END
		WHERE (item.type='Audio' AND kinds.kind<>'Person')
		OR (item.type='Movie' AND kinds.kind IN ('Genre','Person'));
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_type,credit_group)
		SELECT item_id,entity_id,2,display_name,credit_type,credit_group FROM item_entities
		WHERE item_id LIKE 'profile-track-%' AND credit_group=1;
		ANALYZE items; ANALYZE item_entities; ANALYZE catalog_entities; ANALYZE library_roots`)
	var tracks, movies, associations int
	if err := store.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM items WHERE id LIKE 'profile-track-%'),
		(SELECT count(*) FROM items WHERE id LIKE 'profile-movie-%'),
		(SELECT count(*) FROM item_entities)`).Scan(&tracks, &movies, &associations); err != nil {
		t.Fatal(err)
	}
	if tracks != 3000 || movies != 5000 {
		t.Fatalf("incomplete discovery profile: tracks=%d movies=%d", tracks, movies)
	}
	t.Logf("DISCOVERY_QUERY_FIXTURE tracks=%d movies=%d albums=120 associations=%d hidden_fraction=0.2", tracks, movies, associations)
}
