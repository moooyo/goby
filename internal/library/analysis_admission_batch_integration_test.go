//go:build linux

package library

import (
	"reflect"
	"slices"
	"testing"
)

func TestPreviewAdmissionPersistsAllSnapshotsAcrossBatches(t *testing.T) {
	f := newAnalysisWorkFixture(t, analysisAdmissionBatchSize+1)
	runID, children := f.admit(t, TaskPreviewGenerationKey, nil)
	if len(children) != len(f.ids) {
		t.Fatalf("preview batch lost children: got %d, want %d", len(children), len(f.ids))
	}
	rows, err := f.pool.Query(f.ctx, `SELECT c.id,c.ordinal,c.library_id,c.analysis_scope_key,
		w.scope_key,w.cohort_revision,s.item_id,s.position,s.target
		FROM task_run_children c JOIN analysis_work w ON w.child_id=c.id AND w.run_id=c.run_id
		JOIN analysis_work_sources s ON s.child_id=c.id WHERE c.run_id=$1 ORDER BY c.ordinal`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seen []string
	for rows.Next() {
		var child, libraryID, childScope, workScope, cohort, item string
		var ordinal, position int
		var target bool
		if err := rows.Scan(&child, &ordinal, &libraryID, &childScope, &workScope, &cohort, &item, &position, &target); err != nil {
			t.Fatal(err)
		}
		if ordinal != len(seen) || ordinal >= len(children) || child != children[ordinal] || libraryID != f.library.ID ||
			childScope == "" || childScope != workScope || !analysisSHA(cohort) || !target || position != 0 {
			t.Fatalf("preview snapshot shape changed at ordinal %d", ordinal)
		}
		seen = append(seen, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(seen)
	want := slices.Clone(f.ids)
	slices.Sort(want)
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("preview snapshots tracked %d distinct work inputs, want all %d", len(seen), len(want))
	}
}
