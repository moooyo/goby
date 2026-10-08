package library

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

type policyPlanNode struct {
	NodeType    string           `json:"Node Type"`
	ActualLoops float64          `json:"Actual Loops"`
	Plans       []policyPlanNode `json:"Plans"`
}

func (node policyPlanNode) recursiveWork() (int, float64) {
	count, loops := 0, float64(0)
	if node.NodeType == "Recursive Union" {
		count, loops = 1, node.ActualLoops
	}
	for _, child := range node.Plans {
		childCount, childLoops := child.recursiveWork()
		count, loops = count+childCount, loops+childLoops
	}
	return count, loops
}

func readPolicyCandidateIDs(t *testing.T, ctx context.Context, tx pgx.Tx, predicate string) []string {
	t.Helper()
	rows, err := tx.Query(ctx, "SELECT i.id FROM items i WHERE "+ordinaryItemSQL("i")+" AND "+predicate+" ORDER BY i.id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func readPolicyPredicateStates(t *testing.T, ctx context.Context, tx pgx.Tx, predicate string) map[string]*bool {
	t.Helper()
	rows, err := tx.Query(ctx, "SELECT i.id,"+predicate+" FROM items i ORDER BY i.id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	states := make(map[string]*bool)
	for rows.Next() {
		var id string
		var state *bool
		if err := rows.Scan(&id, &state); err != nil {
			t.Fatal(err)
		}
		states[id] = state
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return states
}

func TestSharedPolicyAncestryPreservesResultsAndReducesRecursivePlans(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, store.pool)
	for index := 1; index <= 8; index++ {
		parent := "library-b"
		if index > 1 {
			parent = fmt.Sprintf("policy-ancestor-%d", index-1)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,path,local_metadata)
			VALUES($1,'library-b',$2,$1,$1,'Folder',true,$3,'{"OfficialRating":"PG","Tags":["Family"]}'::jsonb)`,
			fmt.Sprintf("policy-ancestor-%d", index), parent, fmt.Sprintf("/media/b/level-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,local_metadata)
		SELECT 'policy-leaf-'||n,'library-b','policy-ancestor-8','Leaf '||n,'Leaf '||n,'Movie',
			CASE n%4 WHEN 0 THEN '{"OfficialRating":"UNKNOWN"}'::jsonb WHEN 1 THEN '{"OfficialRating":"UR"}'::jsonb
			WHEN 2 THEN '{"OfficialRating":"R"}'::jsonb ELSE '{}'::jsonb END FROM generate_series(1,160) AS n;
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder,local_metadata) VALUES
			('policy-cycle-a','library-b','Cycle A','Cycle A','Series',true,'{"OfficialRating":"PG"}'::jsonb),
			('policy-cycle-b','library-b','Cycle B','Cycle B','Folder',true,'{}'::jsonb);
		UPDATE items SET parent_id=CASE id WHEN 'policy-cycle-a' THEN 'policy-cycle-b' ELSE 'policy-cycle-a' END
			WHERE id IN ('policy-cycle-a','policy-cycle-b');
		SELECT sync_catalog_item_entities(id,local_metadata) FROM items WHERE id LIKE 'policy-%'`); err != nil {
		t.Fatal(err)
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SET LOCAL jit=off"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		policy string
		shared bool
	}{
		{name: "base ACL", policy: `{}`},
		{name: "single exclusion", policy: `{"ExcludedSubFolders":["missing-folder"]}`},
		{name: "parental and unrated", policy: `{"MaxParentalRating":7,"BlockUnratedItems":["Movie","Series"]}`, shared: true},
		{name: "combined restrictions", policy: `{"ExcludedSubFolders":["missing-folder"],"BlockedTags":["Blocked"],"IncludeTags":["Family"],"MaxParentalRating":7,"BlockUnratedItems":["Movie","Series"]}`, shared: true},
		{name: "tag or rating", policy: `{"IncludeTags":["Family"],"AllowTagOrRating":true,"MaxParentalRating":7,"BlockUnratedItems":["Movie"]}`, shared: true},
		{name: "explicit ancestor denial", policy: `{"ExcludedSubFolders":["policy-ancestor-7"],"BlockedTags":["Family"],"IncludeTags":["Family"],"AllowTagOrRating":true,"MaxParentalRating":7}`, shared: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			access, err := parseLibraryPolicy([]byte(test.policy))
			if err != nil {
				t.Fatal(err)
			}
			access.all, access.folders, access.userID = false, []string{"library-b"}, "restricted"
			legacy, candidate := legacyItemPolicySQL(access, "i"), access.itemPolicySQL("i")
			// Preserve SQL NULL as well as visible IDs: owner checks also use NOT
			// around this predicate, so false and unknown are not interchangeable.
			if !reflect.DeepEqual(readPolicyPredicateStates(t, ctx, tx, legacy), readPolicyPredicateStates(t, ctx, tx, candidate)) {
				t.Fatal("sharing policy ancestry changed a true, false, or unknown authorization result")
			}
			before := readPolicyCandidateIDs(t, ctx, tx, legacy)
			after := readPolicyCandidateIDs(t, ctx, tx, candidate)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("sharing policy ancestry changed visibility: before=%v after=%v", before, after)
			}
			counts := make([]int, 2)
			for index, predicate := range []string{legacy, candidate} {
				var encoded []byte
				if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT i.id FROM items i WHERE "+ordinaryItemSQL("i")+" AND "+predicate+" ORDER BY i.id").Scan(&encoded); err != nil {
					t.Fatal(err)
				}
				var plans []struct {
					Plan          policyPlanNode
					ExecutionTime float64 `json:"Execution Time"`
				}
				if err := json.Unmarshal(encoded, &plans); err != nil || len(plans) != 1 {
					t.Fatalf("decode policy plan: %v", err)
				}
				count, loops := plans[0].Plan.recursiveWork()
				counts[index] = count
				t.Logf("candidate=%t SQL bytes=%d recursive plans=%d recursive loops=%.0f execution_ms=%.3f visible=%d",
					index == 1, len(predicate), count, loops, plans[0].ExecutionTime, len(after))
			}
			if test.shared && counts[1] >= counts[0] {
				t.Fatalf("shared policy retained duplicate recursive plans: before=%d after=%d", counts[0], counts[1])
			}
		})
	}
}
