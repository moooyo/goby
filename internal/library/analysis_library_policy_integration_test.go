//go:build linux

package library

import (
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func TestAnalysisDisabledLibraryRetainsManualRunEvidenceWithoutPublishing(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	run, children := f.admit(t, TaskIntroAnalysisKey, nil)
	f.claim(t, run, children[0])
	fence := f.fence(children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, children[0], fence, analysisFixtureQualifiedResult(t, f, work)); err != nil {
		t.Fatal("compatible manual execution failed for a disabled library", err)
	}
	var retained bool
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*)=3 AND bool_and(status='qualified' AND NOT auto_published)
		FROM analysis_detections`).Scan(&retained); err != nil || !retained {
		t.Fatal("disabled library lost evidence or published an automatic intro", err)
	}
	for _, source := range work.Sources {
		value, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "")
		if err != nil || value != nil {
			t.Fatalf("disabled library exposed detected evidence: %+v %v", value, err)
		}
	}
}

func TestAnalysisLibraryPolicyWithdrawsDetectedWithoutErasingExplicitMarkers(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	f.setIntroDetection(t, true)
	run, children := f.admit(t, TaskIntroAnalysisKey, nil)
	f.claim(t, run, children[0])
	fence := f.fence(children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, children[0], fence, analysisFixtureQualifiedResult(t, f, work)); err != nil {
		t.Fatal(err)
	}
	source := work.Sources[0]
	if value, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, ""); err != nil || value == nil || value.Provenance != "Detected" {
		t.Fatalf("enabled library did not publish qualified evidence: %+v %v", value, err)
	}
	// Simulate retained publication history from before the library policy was
	// introduced. Read-time gating must work without rewriting those audit facts.
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=jsonb_set(options,'{EnableIntroDetection}','false'::jsonb) WHERE id=$1`, f.library.ID); err != nil {
		t.Fatal(err)
	}
	if value, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, ""); err != nil || value != nil {
		t.Fatalf("read ignored the current disabled library: %+v %v", value, err)
	}
	var retained bool
	if err := f.pool.QueryRow(f.ctx, `SELECT auto_published FROM analysis_detections WHERE item_id=$1`, source.ItemID).Scan(&retained); err != nil || !retained {
		t.Fatal("read-time policy check rewrote retained publication history", err)
	}
	f.setIntroDetection(t, true)
	if value, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, ""); err != nil || value != nil {
		t.Fatalf("reenabling revived an old publication before fresh analysis: %+v %v", value, err)
	}
	f.setIntroDetection(t, false)
	for _, provenance := range []string{"Manual", "Import"} {
		item, err := f.store.GetAnalysisItem(f.ctx, f.actor, source.ItemID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.store.UpdateItemIntro(f.ctx, f.actor, source.ItemID, IntroEdit{
			Revision: item.Detection.ManualRevision, SourceRevision: item.SourceRevision,
			StartTicks: media.TicksPerSecond, EndTicks: 5 * media.TicksPerSecond, Provenance: provenance}, false)
		if err != nil {
			t.Fatal(err)
		}
		value, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "")
		if err != nil || value == nil || value.Provenance != provenance || value.StartTicks != media.TicksPerSecond || value.EndTicks != 5*media.TicksPerSecond {
			t.Fatalf("disabled library changed the independent %s marker: %+v %v", provenance, value, err)
		}
	}
	var audits int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_intro_audit`).Scan(&audits); err != nil || audits != 3 {
		t.Fatal("library policy changes erased or rewrote detection audit history", err)
	}
}

func TestAnalysisEnabledLibraryDoesNotPublishReviewOrNoResult(t *testing.T) {
	for _, status := range []introdetect.Status{introdetect.Review, introdetect.NoResult} {
		t.Run(string(status), func(t *testing.T) {
			f := newAnalysisWorkFixture(t, 3)
			f.setIntroDetection(t, true)
			run, children := f.admit(t, TaskIntroAnalysisKey, nil)
			f.claim(t, run, children[0])
			fence := f.fence(children[0])
			work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
			if err != nil {
				t.Fatal(err)
			}
			result := analysisFixtureQualifiedResult(t, f, work)
			template := analysisDetectionTestValue(status).Episode
			if status == introdetect.NoResult {
				result.Groups = []introdetect.Group{}
			} else {
				group := &result.Groups[0]
				group.Status, group.Reasons = status, template.Reasons
				group.Metrics = template.Candidates[0].Metrics
				for index := range group.Members {
					group.Members[index].Interval = template.Candidates[0].Support[index].Interval
				}
			}
			for index := range result.Episodes {
				episode := &result.Episodes[index]
				episode.Status, episode.Reasons = status, template.Reasons
				episode.Candidates = []introdetect.Candidate{}
				if status == introdetect.Review {
					group := result.Groups[0]
					episode.Candidates = []introdetect.Candidate{{Interval: group.Members[index].Interval,
						GroupID: group.ID, Status: status, Reasons: group.Reasons, Metrics: group.Metrics, Support: group.Members}}
				}
			}
			if err := f.store.PublishIntroAnalysis(f.ctx, children[0], fence, result); err != nil {
				t.Fatal("retain completed non-qualified analysis", err)
			}
			var retained bool
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*)=3 AND bool_and(status=$1 AND NOT auto_published)
				FROM analysis_detections`, string(status)).Scan(&retained); err != nil || !retained {
				t.Fatal("non-qualified analysis became an automatic intro", err)
			}
			if value, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, work.Sources[0].ItemID, ""); err != nil || value != nil {
				t.Fatalf("non-qualified result reached playback: %+v %v", value, err)
			}
		})
	}
}

func TestAnalysisLibraryReenableRetiresLegacyRejectionBeforeFreshPublication(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	f.setIntroDetection(t, true)
	item, err := f.store.GetAnalysisItem(f.ctx, f.actor, f.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, analysisDecisionForTest(item, "reject")); err != nil {
		t.Fatal("retain a legacy explicit rejection", err)
	}
	f.setIntroDetection(t, false)
	f.setIntroDetection(t, true)
	current, err := f.store.GetAnalysisItem(f.ctx, f.actor, item.ID)
	if err != nil || current.Detection.Suppressed || current.Detection.Effective != nil {
		t.Fatalf("reenabling retained hidden rejection or revived an old intro: %+v %v", current.Detection, err)
	}
	run, children := f.admit(t, TaskIntroAnalysisKey, nil)
	f.claim(t, run, children[0])
	fence := f.fence(children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, children[0], fence, analysisFixtureQualifiedResult(t, f, work)); err != nil {
		t.Fatal(err)
	}
	value, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, item.ID, "")
	if err != nil || value == nil || value.Provenance != "Detected" {
		t.Fatalf("legacy rejection blocked newly enabled qualified publication: %+v %v", value, err)
	}
	var retained bool
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*)=1 FROM analysis_intro_audit
		WHERE item_id=$1 AND action='reject'`, item.ID).Scan(&retained); err != nil || !retained {
		t.Fatal("reenabling erased the historical rejection audit", err)
	}
}
