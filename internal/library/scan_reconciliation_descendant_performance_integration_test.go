//go:build linux

package library

import (
	"encoding/json"
	"os"
	"testing"
)

// The union intentionally retains every discovered key before the outer row
// limit. Measure PostgreSQL scratch and the existing budget sentinel instead
// of claiming that a client-side item budget also bounds server-side scratch.
func TestScanReconciliationDescendantSQLBudgetProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_RECONCILIATION_QUERY_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_RECONCILIATION_QUERY_PERFORMANCE=1 enables the query profile")
	}
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "profile-parent", "Gone", "Folder", fixture.library.ID, true)
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT 'budget-child-'||lpad(n::text,6,'0'),$1,$2,'profile-parent','Budget child','budget child','Movie',false,
		$3||'/Gone/Child-'||n::text||'.mkv','Gone/Child-'||n::text||'.mkv'
		FROM generate_series(1,$4::integer) n`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path,
		scanReconciliationMaxItems+1); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `ANALYZE items; ANALYZE item_theme_resources; ANALYZE item_extra_resources`); err != nil {
		t.Fatal(err)
	}
	for _, frontier := range [][]string{nil, {"profile-parent"}} {
		for _, shape := range []struct{ name, statement string }{
			{"legacy", reconciliationQueryLegacyDescendants()},
			{"union", scanReconciliationDescendantQuery()},
		} {
			var plan json.RawMessage
			if err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
				return tx.QueryRow(`EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) `+shape.statement,
					frontier, scanReconciliationMaxItems+1).Scan(&plan)
			}); err != nil {
				t.Fatal(err)
			}
			t.Logf("RECONCILIATION_CLOSURE_PROFILE shape=%s frontier=%v matching_children=%d actual_plan=%s",
				shape.name, frontier, scanReconciliationMaxItems+1, plan)
		}
	}
}
