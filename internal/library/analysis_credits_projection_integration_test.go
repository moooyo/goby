//go:build linux

package library

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func setStoredCreditsPolicy(t *testing.T, f analysisWorkFixture, enabled bool) {
	t.Helper()
	current, err := f.store.GetLibraryEditing(f.ctx, f.library.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.UpdateLibraryAsAdministrator(f.ctx, f.actor, identity.AdministratorNative, f.library.ID, LibraryUpdate{Revision: current.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableCreditsDetection: &enabled}})
	if err != nil {
		t.Fatal(err)
	}
}

func admittedStoredCredits(t *testing.T, f analysisWorkFixture, ids []string) (AnalysisWork, AnalysisFence) {
	t.Helper()
	run, children := f.admit(t, TaskCreditsAnalysisKey, ids, storedCreditsExecution())
	if len(children) != 1 {
		t.Fatalf("expected one fixture cohort, got %d", len(children))
	}
	f.claim(t, run, children[0])
	fence := f.fence(children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
	if err != nil {
		t.Fatal(err)
	}
	return work, fence
}

func storedCreditsWorkValues(t *testing.T, work AnalysisWork, multiple bool) map[string]AnalysisStoredCreditsResult {
	t.Helper()
	values := map[string]AnalysisStoredCreditsResult{}
	for _, source := range work.Sources {
		if source.Target {
			values[source.ItemID] = storedCreditsValue(t, source, nil, multiple)
		}
	}
	return values
}

func TestCreditsPublicationPreservesIntervalsManualPrecedenceAndPolicy(t *testing.T) {
	f := newAnalysisWorkFixture(t, 2)
	setStoredCreditsPolicy(t, f, true)
	work, fence := admittedStoredCredits(t, f, []string{f.ids[0]})
	values := storedCreditsWorkValues(t, work, true)
	if err := f.store.PublishCreditsAnalysis(f.ctx, work.ChildID, fence, values, nil); err != nil {
		t.Fatal(err)
	}
	source, _ := FindAnalysisSource(work, f.ids[0])
	segments, err := f.store.ResolveDetectedCreditsForRevision(f.ctx, Subject{UserID: f.viewer}, source.ItemID, source.MediaSourceID, source.SourceRevision)
	if err != nil || len(segments) != 2 || !reflect.DeepEqual(segments, values[source.ItemID].Segments) {
		t.Fatalf("multiple segments not projected: %+v %v", segments, err)
	}
	detail, err := f.store.GetItemCredits(f.ctx, f.actor, source.ItemID)
	if err != nil || detail.Effective == nil || detail.Effective.Provenance != "Detected" || detail.DetectedStale || len(detail.Detected) != 2 {
		t.Fatalf("native detected credits missing: %+v %v", detail, err)
	}
	manual, err := f.store.UpdateItemCredits(f.ctx, f.actor, source.ItemID, CreditsEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision, StartTicks: source.DurationTicks - 10*media.TicksPerSecond, Provenance: "Manual"}, false)
	if err != nil || manual.Effective == nil || manual.Effective.Provenance != "Manual" {
		t.Fatal("manual override failed", err)
	}
	segments, err = f.store.ResolveDetectedCreditsForRevision(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "", source.SourceRevision)
	if err != nil || len(segments) != 0 {
		t.Fatal("detection overrode manual credits", err)
	}
	reset, err := f.store.UpdateItemCredits(f.ctx, f.actor, source.ItemID, CreditsEdit{Revision: manual.Revision, SourceRevision: manual.SourceRevision}, true)
	if err != nil || reset.Effective == nil || reset.Effective.Provenance != "Detected" {
		t.Fatal("reset did not reveal valid detection", err)
	}
	setStoredCreditsPolicy(t, f, false)
	segments, err = f.store.ResolveDetectedCreditsForRevision(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "", source.SourceRevision)
	if err != nil || len(segments) != 0 {
		t.Fatal("disabled library exposed detected credits", err)
	}
	var detections, supports, playback int
	var active bool
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM analysis_credits_detections),(SELECT count(*) FROM analysis_credits_detection_sources),(SELECT count(*) FROM play_sessions)+(SELECT count(*) FROM user_item_data),auto_published FROM analysis_credits_detections WHERE item_id=$1`, source.ItemID).Scan(&detections, &supports, &playback, &active); err != nil || detections != 1 || supports != 1 || playback != 0 || active {
		t.Fatalf("policy erased evidence or passive reads created state: %d %d %d %v %v", detections, supports, playback, active, err)
	}
}

func TestCreditsPublicationRejectsManualRevisionAndFinalFenceChanges(t *testing.T) {
	for _, kind := range []string{"manual", "final-fence"} {
		t.Run(kind, func(t *testing.T) {
			f := newAnalysisWorkFixture(t, 2)
			setStoredCreditsPolicy(t, f, true)
			work, fence := admittedStoredCredits(t, f, []string{f.ids[0]})
			values := storedCreditsWorkValues(t, work, false)
			if kind == "manual" {
				detail, err := f.store.GetItemCredits(f.ctx, f.actor, f.ids[0])
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.UpdateItemCredits(f.ctx, f.actor, f.ids[0], CreditsEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision, StartTicks: detail.DurationTicks - 30*media.TicksPerSecond, Provenance: "Import"}, false); err != nil {
					t.Fatal(err)
				}
			} else {
				underlying := fence
				fence = func(tx OwnedTx) error {
					if err := underlying(tx); err != nil {
						return err
					}
					var inserted bool
					if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM analysis_credits_detections)`).Scan(&inserted); err != nil {
						return err
					}
					if inserted {
						return ErrForbidden
					}
					return nil
				}
			}
			err := f.store.PublishCreditsAnalysis(f.ctx, work.ChildID, fence, values, nil)
			if kind == "manual" && !errors.Is(err, ErrAnalysisConflict) || kind == "final-fence" && !errors.Is(err, ErrForbidden) {
				t.Fatal("stale worker publication accepted", err)
			}
			var count int
			if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM analysis_credits_detections)+(SELECT count(*) FROM analysis_credits_detection_sources)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("failed fence left partial publication", err, count)
			}
		})
	}
}

func TestCreditsProjectionReopensIndependentAudioSupportAndPreservesEvidence(t *testing.T) {
	f := newAnalysisWorkFixture(t, 2)
	setStoredCreditsPolicy(t, f, true)
	work, fence := admittedStoredCredits(t, f, nil)
	identityFacts := analysisFixtureQualifiedResult(t, f, work)
	hashes := map[string]string{}
	for index, source := range work.Sources {
		hashes[source.ItemID] = identityFacts.Episodes[index].ContentIdentity
	}
	audio := storedCreditsNativeMatcherResult(t, work, hashes)
	values := map[string]AnalysisStoredCreditsResult{}
	for index, source := range work.Sources {
		episode := audio.Episodes[index]
		values[source.ItemID] = storedCreditsValue(t, source, &episode, false)
	}
	if err := f.store.PublishCreditsAnalysis(f.ctx, work.ChildID, fence, values, hashes); err != nil {
		t.Fatal(err)
	}
	source := work.Sources[0]
	if intervals, err := f.store.ResolveDetectedCreditsForRevision(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "", source.SourceRevision); err != nil || len(intervals) != 1 {
		t.Fatal("valid independent pair missing", err)
	}
	var peerPath string
	if err := f.pool.QueryRow(f.ctx, `SELECT path FROM items WHERE id=$1`, work.Sources[1].ItemID).Scan(&peerPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(peerPath, []byte("changed-physical-support-before-rescan"), 0600); err != nil {
		t.Fatal(err)
	}
	if intervals, err := f.store.ResolveDetectedCreditsForRevision(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "", source.SourceRevision); err != nil || len(intervals) != 0 {
		t.Fatal("changed physical support retained qualification", err)
	}
	detail, err := f.store.GetItemCredits(f.ctx, f.actor, source.ItemID)
	if err != nil || !detail.DetectedStale || detail.Effective != nil || len(detail.Detected) != 1 {
		t.Fatalf("stale pair evidence was lost or remained effective: %+v %v", detail, err)
	}
	var retained bool
	if err := f.pool.QueryRow(f.ctx, `SELECT auto_published AND status='qualified' FROM analysis_credits_detections WHERE item_id=$1`, source.ItemID).Scan(&retained); err != nil || !retained {
		t.Fatal("passive stale read rewrote history", err)
	}
}

func TestCreditsProjectionCorruptEvidenceFallsBackToManualMarker(t *testing.T) {
	f := newAnalysisWorkFixture(t, 1)
	setStoredCreditsPolicy(t, f, true)
	work, fence := admittedStoredCredits(t, f, nil)
	values := storedCreditsWorkValues(t, work, false)
	if err := f.store.PublishCreditsAnalysis(f.ctx, work.ChildID, fence, values, nil); err != nil {
		t.Fatal(err)
	}
	source := work.Sources[0]
	if _, err := f.pool.Exec(f.ctx, `UPDATE analysis_credits_detections SET result=jsonb_set(result,'{Segments,0,Source}','"Unknown"'::jsonb) WHERE item_id=$1`, source.ItemID); err != nil {
		t.Fatal(err)
	}
	if intervals, err := f.store.ResolveDetectedCreditsForRevision(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "", source.SourceRevision); err != nil || len(intervals) != 0 {
		t.Fatal("corrupt optional evidence caused playback failure", err)
	}
	detail, err := f.store.GetItemCredits(f.ctx, f.actor, source.ItemID)
	if err != nil || !detail.DetectedStale || detail.DetectedReason != "invalid_evidence" {
		t.Fatalf("native stale classification missing: %+v %v", detail, err)
	}
	detail, err = f.store.UpdateItemCredits(f.ctx, f.actor, source.ItemID, CreditsEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision, StartTicks: source.DurationTicks - 20*media.TicksPerSecond, Provenance: "Manual"}, false)
	if err != nil || detail.Effective == nil || detail.Effective.Provenance != "Manual" {
		t.Fatal("corrupt detection blocked manual repair", err)
	}
}
