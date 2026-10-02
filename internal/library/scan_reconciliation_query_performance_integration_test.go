//go:build linux

package library

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"
)

type reconciliationQueryProfile struct {
	Case         string          `json:"case"`
	Variant      string          `json:"variant"`
	Query        string          `json:"query"`
	TargetItems  int             `json:"target_items"`
	ForeignItems int             `json:"foreign_items"`
	MissingItems int             `json:"missing_items"`
	Rows         int             `json:"rows"`
	OwnerMicros  []int64         `json:"owner_microseconds"`
	MedianMicros int64           `json:"median_microseconds"`
	IndexBytes   int64           `json:"candidate_index_bytes"`
	IndexBuildMS int64           `json:"candidate_index_build_ms"`
	Plan         json.RawMessage `json:"actual_plan"`
}

// This profile runs the actual owner-session statements, including complete
// fact projection, role checks, anti-join, LockRows and result consumption.
// Candidate DDL is restricted to this test's random schema. The profile can be
// copied with scan_reconciliation_query.go into the preceding checkout.
func TestScanReconciliationQueryPerformanceProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_RECONCILIATION_QUERY_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_RECONCILIATION_QUERY_PERFORMANCE=1 enables the query profile")
	}
	for _, scenario := range []struct {
		name                   string
		target, foreign, every int
	}{
		{"small-library-sparse-missing", 2000, 80000, 1000},
		{"large-library-sparse-missing", 20000, 0, 10000},
		{"large-library-dense-missing", 20000, 80000, 20},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newRootBindingScanFixture(t)
			stride := max(1, (scenario.target+scenario.foreign)/scenario.target)
			if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO libraries (id,name,collection_type)
				VALUES ('reconciliation-profile-foreign','Profile foreign library','movies')`); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
				(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
				SELECT lpad((n*$5::integer)::text,9,'0')||'-target',$1,$2,$1,'Profile target','profile target','Movie',false,
				$3||'/Target-'||n::text||'.mkv','Target-'||n::text||'.mkv'
				FROM generate_series(1,$4::integer) n`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path,
				scenario.target, stride); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
				(id,library_id,name,sort_name,type,is_folder,path,relative_path)
				SELECT lpad(n::text,9,'0')||'-foreign','reconciliation-profile-foreign','Profile foreign','profile foreign',
				'Movie',false,'/profile/foreign/Foreign-'||n::text||'.mkv','Foreign-'||n::text||'.mkv'
				FROM generate_series(1,$1::integer) n`, scenario.foreign); err != nil {
				t.Fatal(err)
			}
			scanReconciliationCommitInsertItem(t, fixture, "profile-gone-parent", "Gone", "Folder", fixture.library.ID, true)
			if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
				(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
				SELECT 'profile-child-'||lpad(n::text,4,'0'),$1,$2,'profile-gone-parent','Profile child','profile child',
				'Movie',false,$3||'/Gone/Child-'||n::text||'.mkv','Gone/Child-'||n::text||'.mkv'
				FROM generate_series(1,32) n`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path); err != nil {
				t.Fatal(err)
			}
			accepted := []string{"profile-gone-parent"}
			for index := 1; index <= scenario.target; index++ {
				if index%scenario.every != 0 {
					accepted = append(accepted, fmt.Sprintf("%09d-target", index*stride))
				}
			}
			for index := 1; index <= 32; index++ {
				accepted = append(accepted, fmt.Sprintf("profile-child-%04d", index))
			}
			stage := scanReconciliationStageFixture(t, fixture, accepted)
			if _, err := fixture.pool.Exec(fixture.ctx, `ANALYZE items; ANALYZE item_theme_resources; ANALYZE item_extra_resources`); err != nil {
				t.Fatal(err)
			}
			frontier := []string{"profile-gone-parent"}
			legacyDescendants := `SELECT ` + scanReconciliationItemColumns + `
				FROM items i LEFT JOIN item_theme_resources theme ON theme.resource_item_id=i.id
				LEFT JOIN item_extra_resources extra ON extra.resource_item_id=i.id
				WHERE ` + scanReconciliationDescendantPredicate + ` ORDER BY i.id LIMIT $2 FOR UPDATE OF i`
			var indexBytes, indexBuildMS int64
			for _, indexed := range []bool{false, true} {
				if indexed {
					started := time.Now()
					if _, err := fixture.pool.Exec(fixture.ctx, `CREATE INDEX reconciliation_profile_library_id_idx ON items (library_id,id)`); err != nil {
						t.Fatal(err)
					}
					indexBuildMS = time.Since(started).Milliseconds()
					if err := fixture.pool.QueryRow(fixture.ctx, `SELECT pg_relation_size('reconciliation_profile_library_id_idx')`).Scan(&indexBytes); err != nil {
						t.Fatal(err)
					}
				}
				variant := "existing-indexes"
				if indexed {
					variant = "library-id-index"
				}
				for _, first := range []bool{true, false} {
					after := fmt.Sprintf("%09d-target", scenario.target/2*stride)
					legacySQL, legacyArgs, err := reconciliationQueryLegacyPage(fixture.library.ID, after, first, stage)
					if err != nil {
						t.Fatal(err)
					}
					rangeSQL, rangeArgs, err := scanReconciliationRangePageQuery(fixture.library.ID, after, first, stage)
					if err != nil {
						t.Fatal(err)
					}
					page := "first"
					if !first {
						page = "cursor"
					}
					legacy, baseline := reconciliationQueryMeasure(t, fixture, stage, legacySQL, legacyArgs)
					candidate, result := reconciliationQueryMeasure(t, fixture, stage, rangeSQL, rangeArgs)
					if !reflect.DeepEqual(baseline, result) {
						t.Fatal("range SQL changed complete admitted item facts")
					}
					for label, measurement := range map[string]reconciliationQueryProfile{"legacy-" + page: legacy, "range-" + page: candidate} {
						measurement.Case, measurement.Variant, measurement.Query = scenario.name, variant, label
						measurement.TargetItems, measurement.ForeignItems, measurement.MissingItems = scenario.target, scenario.foreign, scenario.target/scenario.every
						measurement.IndexBytes, measurement.IndexBuildMS = indexBytes, indexBuildMS
						reconciliationQueryLog(t, measurement)
					}
				}
				legacy, baseline := reconciliationQueryMeasure(t, fixture, stage, legacyDescendants, []any{frontier, scanReconciliationMaxItems + 1})
				candidate, result := reconciliationQueryMeasure(t, fixture, stage, scanReconciliationDescendantQuery(), []any{frontier, scanReconciliationMaxItems + 1})
				if len(result) != 32 || !reflect.DeepEqual(baseline, result) {
					t.Fatal("UNION discovery changed complete descendant facts")
				}
				for label, measurement := range map[string]reconciliationQueryProfile{"legacy-descendants": legacy, "union-descendants": candidate} {
					measurement.Case, measurement.Variant, measurement.Query = scenario.name, variant, label
					measurement.TargetItems, measurement.ForeignItems, measurement.MissingItems = scenario.target, scenario.foreign, scenario.target/scenario.every
					measurement.IndexBytes, measurement.IndexBuildMS = indexBytes, indexBuildMS
					reconciliationQueryLog(t, measurement)
				}
				reconciliationQueryMeasureWrites(t, fixture, variant)
			}
		})
	}
}

func reconciliationQueryMeasure(t *testing.T, fixture rootBindingScanFixture, stage *scanReconciliationStaging, statement string, arguments []any) (reconciliationQueryProfile, []scanReconciliationItem) {
	t.Helper()
	measurement := reconciliationQueryProfile{}
	var admitted []scanReconciliationItem
	for repetition := 0; repetition < 8; repetition++ {
		started := time.Now()
		if err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
			if err := stage.RequireSealed(tx); err != nil {
				return err
			}
			rows, err := tx.Query(statement, arguments...)
			if err != nil {
				return err
			}
			defer rows.Close()
			var current []scanReconciliationItem
			for rows.Next() {
				item, err := scanReconciliationReadItem(rows)
				if err != nil {
					return err
				}
				current = append(current, item)
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if repetition == 0 {
				admitted = current
			} else if !reflect.DeepEqual(admitted, current) {
				return fmt.Errorf("admitted facts changed during a stable query profile")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if repetition != 0 {
			measurement.OwnerMicros = append(measurement.OwnerMicros, time.Since(started).Microseconds())
		}
	}
	if err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
		return tx.QueryRow(`EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) `+statement, arguments...).Scan(&measurement.Plan)
	}); err != nil {
		t.Fatal(err)
	}
	ordered := append([]int64(nil), measurement.OwnerMicros...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	measurement.MedianMicros, measurement.Rows = ordered[len(ordered)/2], len(admitted)
	return measurement, admitted
}

func reconciliationQueryMeasureWrites(t *testing.T, fixture rootBindingScanFixture, variant string) {
	t.Helper()
	var plan json.RawMessage
	if err := fixture.pool.QueryRow(fixture.ctx, `EXPLAIN (ANALYZE, BUFFERS, WAL, TIMING OFF, FORMAT JSON)
		INSERT INTO items (id,library_id,name,sort_name,type,is_folder)
		SELECT $1||'-write-'||n::text,'reconciliation-profile-foreign','Write sample','write sample','Movie',false
		FROM generate_series(1,1000) n`, variant).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Logf("RECONCILIATION_WRITE_PROFILE %s variant=%s rows=1000 actual_plan=%s", t.Name(), variant, plan)
	if _, err := fixture.pool.Exec(fixture.ctx, `DELETE FROM items WHERE id LIKE $1||'-write-%'`, variant); err != nil {
		t.Fatal(err)
	}
}

func reconciliationQueryLog(t *testing.T, measurement reconciliationQueryProfile) {
	t.Helper()
	raw, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RECONCILIATION_QUERY_PROFILE %s", raw)
}

// Freeze the actual preceding production statement so the paired profile does
// not silently compare the new range helper with itself after it is connected.
func reconciliationQueryLegacyPage(libraryID, after string, first bool, staging *scanReconciliationStaging) (string, []any, error) {
	predicate := `i.library_id=$1 AND ($3::boolean OR i.id>$2)
		AND i.root_id IS NOT NULL AND i.type<>'CollectionFolder'
		AND i.path<>'' AND i.relative_path<>'' AND left(i.path,2)<>'//'
		AND left(i.relative_path,2)<>'//' AND ` + ordinaryItemSQL("i")
	arguments := []any{libraryID, after, first, scanReconciliationPageItems}
	if staging != nil {
		generation, scanID, stagedLibrary := staging.Scope()
		if stagedLibrary != libraryID {
			return "", nil, ErrInvalidInput
		}
		predicate += ` AND NOT EXISTS (SELECT 1 FROM pg_temp.goby_scan_reconciliation_seen seen
			WHERE seen.generation=$5 AND seen.scan_id=$6 AND seen.library_id=$7
			AND seen.item_id=i.id COLLATE "C")`
		arguments = append(arguments, generation, scanID, stagedLibrary)
	}
	return `SELECT ` + scanReconciliationItemColumns + `
		FROM items i LEFT JOIN item_theme_resources theme ON theme.resource_item_id=i.id
		LEFT JOIN item_extra_resources extra ON extra.resource_item_id=i.id
		WHERE ` + predicate + ` ORDER BY i.id LIMIT $4 FOR UPDATE OF i`, arguments, nil
}

// Keep the experimental range shape in the profile only. The observed custom
// plans already simplified the boolean cursor condition, and range-only SQL
// did not improve the production-profile warm samples consistently.
func scanReconciliationRangePageQuery(libraryID, after string, first bool, staging *scanReconciliationStaging) (string, []any, error) {
	predicate := `i.library_id=$1`
	arguments := []any{libraryID}
	if !first {
		predicate += ` AND i.id>$2`
		arguments = append(arguments, after)
	}
	predicate += ` AND i.root_id IS NOT NULL AND i.type<>'CollectionFolder'
		AND i.path<>'' AND i.relative_path<>'' AND left(i.path,2)<>'//'
		AND left(i.relative_path,2)<>'//' AND ` + ordinaryItemSQL("i")
	arguments = append(arguments, scanReconciliationPageItems)
	limit := len(arguments)
	if staging != nil {
		generation, scanID, stagedLibrary := staging.Scope()
		if stagedLibrary != libraryID {
			return "", nil, ErrInvalidInput
		}
		base := len(arguments)
		predicate += fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM pg_temp.goby_scan_reconciliation_seen seen
			WHERE seen.generation=$%d AND seen.scan_id=$%d AND seen.library_id=$%d
			AND seen.item_id=i.id COLLATE "C")`, base+1, base+2, base+3)
		arguments = append(arguments, generation, scanID, stagedLibrary)
	}
	return `SELECT ` + scanReconciliationItemColumns + `
		FROM items i LEFT JOIN item_theme_resources theme ON theme.resource_item_id=i.id
		LEFT JOIN item_extra_resources extra ON extra.resource_item_id=i.id
		WHERE ` + predicate + fmt.Sprintf(` ORDER BY i.id LIMIT $%d FOR UPDATE OF i`, limit), arguments, nil
}
