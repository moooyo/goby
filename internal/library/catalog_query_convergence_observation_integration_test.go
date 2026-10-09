package library

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCatalogQueryConvergencePostgreSQL17Observation(t *testing.T) {
	if os.Getenv("GOBY_TEST_CATALOG_CONVERGENCE_OBSERVATION") != "1" {
		t.Skip("GOBY_TEST_CATALOG_CONVERGENCE_OBSERVATION=1 selects the bounded PostgreSQL observation")
	}
	f := newEpisodeRosterFixture(t)
	detail := f.replace(t, f.edit)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		SELECT 'expected-profile-series-'||n,'library-b','library-b','Profile series '||n,'Profile series '||n,'Series',true
		FROM generate_series(1,1024) n;
		INSERT INTO series_episode_rosters(series_id,revision,state,source_key,source_label,source_revision,parser_version,payload_sha256,last_edited_by)
		SELECT 'expected-profile-series-'||n,revision,state,source_key,source_label,source_revision,parser_version,payload_sha256,last_edited_by
		FROM series_episode_rosters CROSS JOIN generate_series(1,1024) n WHERE series_id='series-b';
		INSERT INTO episode_roster_imports(series_id,revision,action,source_key,source_label,source_revision,parser_version,payload,payload_sha256,actor_id)
		SELECT 'expected-profile-series-'||n,revision,action,source_key,source_label,source_revision,parser_version,payload,payload_sha256,actor_id
		FROM episode_roster_imports CROSS JOIN generate_series(1,1024) n WHERE series_id='series-b';
		INSERT INTO expected_episodes(id,series_id,source_key,entry_key,season_number,episode_number,name,premiere_date,active,import_revision)
		SELECT 'missing-'||md5('profile-'||n||'-'||e.id),'expected-profile-series-'||n,e.source_key,e.entry_key,
		e.season_number,e.episode_number,e.name,e.premiere_date,e.active,e.import_revision
		FROM expected_episodes e CROSS JOIN generate_series(1,1024) n WHERE e.series_id='series-b';
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type)
		SELECT 'aggregate-profile-leaf-'||n,'library-b','season-b','Profile leaf '||n,'Profile leaf '||n,'Episode'
		FROM generate_series(1,2000) n;
		INSERT INTO user_item_data(user_id,item_id,played)
		SELECT 'restricted','aggregate-profile-leaf-'||n,true FROM generate_series(1,2000) n WHERE n%2=0;
		ANALYZE items; ANALYZE expected_episodes; ANALYZE series_episode_rosters;
		ANALYZE user_item_data`); err != nil {
		t.Fatal("seed bounded candidate and descendant populations", err)
	}
	t.Log("catalog_convergence_fixture extra_series=1024 extra_expected_rows=3072 extra_playable_leaves=2000 played_leaves=1000")
	tx, access, err := f.store.beginSubjectRead(f.ctx, Subject{UserID: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(f.ctx, "SET LOCAL jit=off"); err != nil {
		t.Fatal(err)
	}
	ids := []string{"series-b", "season-b", "artist-b"}
	root := `SELECT i.id,i.library_id FROM items i WHERE i.id=ANY($1::text[]) AND i.is_folder AND ` + access.ordinarySQL("i")
	currentCounts := folderUserDataCountsSQL(root, 2, access)
	oldCounts := strings.ReplaceAll(currentCounts, "count(leaf.id)", "count(DISTINCT leaf.id)")
	if oldCounts == currentCounts {
		t.Fatal("descendant observation lost its original DISTINCT aggregate")
	}
	if !reflect.DeepEqual(convergenceDescendantCounts(t, f.ctx, tx, oldCounts, ids, "restricted"),
		convergenceDescendantCounts(t, f.ctx, tx, currentCounts, ids, "restricted")) {
		t.Fatal("observed descendant aggregation changed counts")
	}
	id := detail.Entries[0].ID
	for _, mode := range []string{"single", "batch"} {
		outer, inner := "i.id=$1", "e.id=$1::text"
		var argument any = id
		if mode == "batch" {
			outer, inner = "i.id=ANY($1::text[])", "e.id=ANY($1::text[])"
			argument = []string{id, id, ExpectedEpisodeID("absent", "source", "entry")}
		}
		before := convergenceExpectedRows(t, f.ctx, tx, access, expectedEpisodeItemsSQL(access), outer, argument)
		after := convergenceExpectedRows(t, f.ctx, tx, access, expectedEpisodeItemsFilteredSQL(access, inner), outer, argument)
		if !reflect.DeepEqual(before, after) || len(after) != 1 {
			t.Fatal("observed expected-episode identity filtering changed virtual rows")
		}
		for trial := range 3 {
			for _, phase := range []string{"original", "current"} {
				relation := expectedEpisodeItemsSQL(access)
				if phase == "current" {
					relation = expectedEpisodeItemsFilteredSQL(access, inner)
				}
				statement := `SELECT i.id FROM (` + relation + `) i WHERE ` + outer + ` AND ` + access.itemPolicySQL("i")
				var plan json.RawMessage
				if err := tx.QueryRow(f.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement, argument).Scan(&plan); err != nil {
					t.Fatal(err)
				}
				t.Logf("expected_id_observation mode=%s phase=%s trial=%d plan=%s", mode, phase, trial, plan)
			}
		}
	}
	for trial := range 3 {
		for _, shape := range []struct{ name, statement string }{{"original", oldCounts}, {"current", currentCounts}} {
			var plan json.RawMessage
			if err := tx.QueryRow(f.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+shape.statement, ids, "restricted").Scan(&plan); err != nil {
				t.Fatal(err)
			}
			t.Logf("descendant_count_observation phase=%s trial=%d plan=%s", shape.name, trial, plan)
		}
	}
}
