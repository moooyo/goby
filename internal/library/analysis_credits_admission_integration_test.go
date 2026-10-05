//go:build linux

package library

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestCreditsAdmissionKeepsBoundedSeasonSupportAndWholeCohortFence(t *testing.T) {
	f := newAnalysisWorkFixture(t, 40)
	runID, children := f.admit(t, TaskCreditsAnalysisKey, nil, analysisAdmissionTestCreditsExecution())
	if len(children) != 3 {
		t.Fatalf("credits season was not partitioned into 16-target windows: %d", len(children))
	}
	var targets, maximum int
	var duplicate bool
	if err := f.pool.QueryRow(f.ctx, `SELECT
	 (SELECT count(*) FROM analysis_work_sources s JOIN analysis_work w ON w.child_id=s.child_id WHERE w.run_id=$1 AND s.target),
	 (SELECT max(n) FROM (SELECT count(*) n FROM analysis_work_sources s JOIN analysis_work w ON w.child_id=s.child_id WHERE w.run_id=$1 GROUP BY s.child_id) q),
	 EXISTS(SELECT s.item_id FROM analysis_work_sources s JOIN analysis_work w ON w.child_id=s.child_id WHERE w.run_id=$1 AND s.target GROUP BY s.item_id HAVING count(*)<>1)`, runID).Scan(&targets, &maximum, &duplicate); err != nil || targets != 40 || maximum > 32 || duplicate {
		t.Fatalf("credits support/target bounds: %d %d %t %v", targets, maximum, duplicate, err)
	}
	f.claim(t, runID, children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], f.fence(children[0]))
	if err != nil || len(work.Sources) != 32 || work.Reason != "" {
		t.Fatalf("credits work: %+v %v", work, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET index_number=99 WHERE id=$1`, f.ids[39]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetAnalysisWork(f.ctx, children[0], f.fence(children[0])); !errors.Is(err, ErrAnalysisSourceChanged) {
		t.Fatalf("change outside support window escaped whole-season fence: %v", err)
	}
}

func TestCreditsAdmissionAllowsMovieAndIsolatedEpisodeVisualWork(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET type='Movie' WHERE id=$1`, f.ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET parent_id=NULL WHERE id=$1`, f.ids[2]); err != nil {
		t.Fatal(err)
	}
	runID, children := f.admit(t, TaskCreditsAnalysisKey, nil, analysisAdmissionTestCreditsExecution())
	if len(children) != 3 {
		t.Fatalf("movie and isolated episode did not receive independent work: %d", len(children))
	}
	for _, child := range children {
		f.claim(t, runID, child)
		work, err := f.store.GetAnalysisWork(f.ctx, child, f.fence(child))
		if err != nil || len(work.Sources) != 1 || !work.Sources[0].Target || work.Reason != "" {
			t.Fatalf("single-source visual path was suppressed: %+v %v", work, err)
		}
	}
}

func TestCreditsAdmissionManualRevisionIsIndependentFromIntro(t *testing.T) {
	f := newAnalysisWorkFixture(t, 2)
	credits, err := f.store.GetItemCredits(f.ctx, f.actor, f.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	credits, err = f.store.UpdateItemCredits(f.ctx, f.actor, f.ids[0], CreditsEdit{Revision: credits.Revision, SourceRevision: credits.SourceRevision, StartTicks: 5000 * media.TicksPerSecond, Provenance: "Manual"}, false)
	if err != nil {
		t.Fatal(err)
	}
	runID, children := f.admit(t, TaskCreditsAnalysisKey, []string{f.ids[0]}, analysisAdmissionTestCreditsExecution())
	if len(children) != 1 {
		t.Fatalf("leaf credits request lost its season support: %d", len(children))
	}
	f.claim(t, runID, children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], f.fence(children[0]))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.GetAnalysisFeatures(f.ctx, children[0], f.ids[0], f.fence(children[0])); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("credits accessed the prefix-only intro cache: %v", err)
	}
	for _, source := range work.Sources {
		if source.Target && (source.ManualRevision != credits.Revision || source.DecisionRevision != "0") {
			t.Fatalf("credits snapshot inherited intro revision: %+v", source)
		}
	}
	intro, err := f.store.GetItemIntro(f.ctx, f.actor, f.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateItemIntro(f.ctx, f.actor, f.ids[0], IntroEdit{Revision: intro.Revision, SourceRevision: intro.SourceRevision, StartTicks: 0, EndTicks: 20 * media.TicksPerSecond, Provenance: "Manual"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetAnalysisWork(f.ctx, children[0], f.fence(children[0])); err != nil {
		t.Fatalf("intro-only edit cancelled unrelated credits work: %v", err)
	}
	if _, err := f.store.UpdateItemCredits(f.ctx, f.actor, f.ids[0], CreditsEdit{Revision: credits.Revision, SourceRevision: credits.SourceRevision, StartTicks: 5100 * media.TicksPerSecond, Provenance: "Manual"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetAnalysisWork(f.ctx, children[0], f.fence(children[0])); !errors.Is(err, ErrAnalysisConflict) {
		t.Fatalf("credits edit failed to fence stale publication: %v", err)
	}
}
