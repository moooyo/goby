//go:build linux

package library

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func TestAnalysisVisualPublicationPersistsEvidenceAndHonorsLibraryPolicy(t *testing.T) {
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
	visual := *analysisVisualDetectionTestValue().Episode.Candidates[0].VisualEvidence
	interval := introdetect.Interval{StartTicks: 10 * media.TicksPerSecond, EndTicks: 18 * media.TicksPerSecond}
	result.Groups[0].Metrics = introdetect.Metrics{}
	result.Groups[0].VisualEvidence = &visual
	for index := range result.Groups[0].Members {
		result.Groups[0].Members[index].Interval = interval
	}
	for index := range result.Episodes {
		candidate := &result.Episodes[index].Candidates[0]
		candidate.Interval, candidate.Metrics = interval, introdetect.Metrics{}
		copy := visual
		candidate.VisualEvidence = &copy
	}
	rejecting := func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		return ErrForbidden
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, children[0], rejecting, result); !errors.Is(err, ErrForbidden) {
		t.Fatalf("visual publication bypassed its worker fence: %v", err)
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, children[0], fence, result); err != nil {
		t.Fatal(err)
	}
	for _, source := range work.Sources {
		item, err := f.store.GetAnalysisItem(f.ctx, f.actor, source.ItemID)
		if err != nil || item.Detection.Candidate == nil || !reflect.DeepEqual(item.Detection.Candidate.VisualEvidence, &visual) ||
			item.Detection.Candidate.Metrics != (introdetect.Metrics{}) {
			t.Fatalf("persisted visual evidence changed: %+v %v", item.Detection, err)
		}
		intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "")
		if err != nil || intro == nil || intro.Provenance != "Detected" || intro.StartTicks != interval.StartTicks || intro.EndTicks != interval.EndTicks {
			t.Fatalf("qualified visual interval was not available to playback: %+v %v", intro, err)
		}
	}
	f.setIntroDetection(t, false)
	for _, source := range work.Sources {
		if intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, ""); err != nil || intro != nil {
			t.Fatalf("disabled library retained visual publication authority: %+v %v", intro, err)
		}
	}
}
