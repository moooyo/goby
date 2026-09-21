package introdetect

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

// These features test interval accounting and alignment mechanics, not media
// accuracy. Complementary halves give each distinct word exactly 16 set bits.
func audioEvidenceWord(index int) uint32 {
	value := uint32(index + 1)
	return value<<16 | (^value & 0xffff)
}

func audioEvidenceFixture(count int, guard int64) (Episode, Episode, []int) {
	step := TicksPerSecond / 4
	a := Episode{DurationTicks: int64(count+4) * step, AudioBoundaryUncertaintyTicks: guard}
	b := a
	matched := make([]int, count+4)
	for index := range matched {
		word := audioEvidenceWord(index)
		start, end := int64(index)*step, int64(index+1)*step
		a.Audio = append(a.Audio, AudioSample{start, end, word})
		matched[index] = index
		if index >= count {
			word, matched[index] = ^word, -1
		}
		b.Audio = append(b.Audio, AudioSample{start, end, word})
	}
	return a, b, matched
}

func audioEvidenceBudget() *workBudget {
	return &workBudget{ctx: context.Background(), limit: DefaultOptions().MaxComparisons}
}

func TestGuardedAudioMeasuresOnlyItsOriginalCompletePairs(t *testing.T) {
	a, b, matched := audioEvidenceFixture(160, 4*TicksPerSecond)
	for _, index := range []int{1, 2, 3, 5, 6, 7, 9, 10, 11, 80, 148, 149, 150, 152, 153, 154, 156, 157, 158} {
		matched[index] = -1
		b.Audio[index].Fingerprint = ^a.Audio[index].Fingerprint
	}
	beforeA, beforeB, beforeMatched := slices.Clone(a.Audio), slices.Clone(b.Audio), slices.Clone(matched)
	unguardedA, unguardedB := a, b
	unguardedA.AudioBoundaryUncertaintyTicks, unguardedB.AudioBoundaryUncertaintyTicks = 0, 0
	raw, reason, err := measureAudioRun(unguardedA, unguardedB, matched, 0, 159, DefaultOptions(), audioEvidenceBudget())
	if err != nil || reason != "" || raw == nil || raw.metrics.AudioAgreementPermille != 881 || !slices.Contains(raw.reasons, WeakAudioEvidence) {
		t.Fatalf("fixture did not expose a weak full run: %#v, %s, %v", raw, reason, err)
	}
	guarded, reason, err := measureAudioRun(a, b, matched, 0, 159, DefaultOptions(), audioEvidenceBudget())
	want := Interval{4 * TicksPerSecond, 36 * TicksPerSecond}
	if err != nil || reason != "" || guarded == nil || guarded.a != want || guarded.b != want ||
		guarded.metrics.AudioAgreementPermille != 992 || guarded.metrics.AudioSamples != 127 || guarded.metrics.AudioDistinct != 127 ||
		guarded.metrics.AudioSimilarityPermille != 1000 || guarded.metrics.BoundaryUncertaintyTicks != 4*TicksPerSecond || len(guarded.reasons) != 0 {
		t.Fatalf("guarded interval inherited raw counts or quality: %#v, %s, %v", guarded, reason, err)
	}
	if !reflect.DeepEqual(a.Audio, beforeA) || !reflect.DeepEqual(b.Audio, beforeB) || !reflect.DeepEqual(matched, beforeMatched) {
		t.Fatal("measuring evidence mutated caller-owned bins or matches")
	}
}

func TestGuardedAudioRecomputesSimilarity(t *testing.T) {
	a, b, matched := audioEvidenceFixture(80, 2*TicksPerSecond)
	for index := 0; index < 80; index++ {
		distance := 6
		if index >= 8 && index < 72 {
			distance = 5
			if index%4 == 0 {
				distance = 4
			}
		}
		b.Audio[index].Fingerprint ^= uint32(1<<distance) - 1
	}
	raw, reason, err := measureMatchedAudioEvidence(a, b, matched, 0, 79, Interval{0, 20 * TicksPerSecond}, Interval{0, 20 * TicksPerSecond}, DefaultOptions(), audioEvidenceBudget())
	if err != nil || reason != "" || raw.AudioSimilarityPermille != 844 {
		t.Fatalf("fixture did not expose weak raw similarity: %#v, %s, %v", raw, reason, err)
	}
	guarded, reason, err := measureAudioRun(a, b, matched, 0, 79, DefaultOptions(), audioEvidenceBudget())
	if err != nil || reason != "" || guarded == nil || guarded.metrics.AudioSimilarityPermille != 852 ||
		guarded.metrics.AudioSamples != 64 || slices.Contains(guarded.reasons, WeakAudioEvidence) {
		t.Fatalf("guarded similarity retained excluded boundary pairs: %#v, %s, %v", guarded, reason, err)
	}
}

func TestGuardedAudioCannotRematchATargetConsumedOutsideItsInterval(t *testing.T) {
	a, b, _ := audioEvidenceFixture(80, 2*TicksPerSecond)
	a.Audio[7].Fingerprint = a.Audio[8].Fingerprint
	b.Audio = append(b.Audio[:7:7], b.Audio[8:]...)
	runs, reasons, err := alignedAudio(a, b, 0, DefaultOptions(), audioEvidenceBudget())
	if err != nil || len(reasons) != 0 || len(runs) != 1 {
		t.Fatalf("fixture did not retain one raw aligned run: %#v, %v, %v", runs, reasons, err)
	}
	want := Interval{2 * TicksPerSecond, 18 * TicksPerSecond}
	if runs[0].a != want || runs[0].b != want || runs[0].metrics.AudioSamples != 63 || runs[0].metrics.AudioAgreementPermille != 984 {
		t.Fatalf("cropping rematched a target consumed by the excluded source bin: %#v", runs[0])
	}
}

func TestAudioEvidenceRejectsPartialBinsOnEitherSide(t *testing.T) {
	a, b, matched := audioEvidenceFixture(80, 0)
	ar := Interval{TicksPerSecond / 10, 199 * TicksPerSecond / 10}
	br := Interval{35 * TicksPerSecond / 100, 1965 * TicksPerSecond / 100}
	metrics, reason, err := measureMatchedAudioEvidence(a, b, matched, 0, 79, ar, br, DefaultOptions(), audioEvidenceBudget())
	if err != nil || reason != "" || metrics.AudioSamples != 76 || metrics.AudioAgreementPermille != 959 {
		t.Fatalf("partial edge bins received invented credit: %#v, %s, %v", metrics, reason, err)
	}
}

func TestGuardedAudioCannotEraseRawHardFailures(t *testing.T) {
	for _, kind := range []string{"coverage", "entropy", "overlong"} {
		t.Run(kind, func(t *testing.T) {
			count, guard := 96, 4*TicksPerSecond
			if kind == "entropy" {
				count, guard = 160, 10*TicksPerSecond
			} else if kind == "overlong" {
				count, guard = 800, 12*TicksPerSecond
			}
			a, b, matched := audioEvidenceFixture(count, guard)
			want := OverlongRepeat
			if kind == "coverage" {
				want = InsufficientAudio
				for index := 0; index < count; index++ {
					if (index < 16 || index >= 80) && index%4 != 0 && index != count-1 {
						matched[index] = -1
					}
				}
			} else if kind == "entropy" {
				want = LowAudioEntropy
				for index := 0; index < count; index++ {
					if index < 40 || index >= 120 {
						a.Audio[index].Fingerprint, b.Audio[index].Fingerprint = 0xaaaa5555, 0xaaaa5555
					}
				}
			}
			interior := Interval{guard, int64(count)*TicksPerSecond/4 - guard}
			metrics, reason, err := measureMatchedAudioEvidence(a, b, matched, 0, count-1, interior, interior, DefaultOptions(), audioEvidenceBudget())
			if err != nil || reason != "" || metrics.AudioAgreementPermille != 1000 {
				t.Fatalf("fixture interior was not strong: %#v, %s, %v", metrics, reason, err)
			}
			match, reason, err := measureAudioRun(a, b, matched, 0, count-1, DefaultOptions(), audioEvidenceBudget())
			if err != nil || match != nil || reason != want {
				t.Fatalf("guard washed away a raw hard failure: %#v, %s, %v", match, reason, err)
			}
		})
	}
}

func TestGuardedAudioCannotBorrowEntropyFromExcludedBins(t *testing.T) {
	a, b, matched := audioEvidenceFixture(96, 4*TicksPerSecond)
	for index := 16; index < 80; index++ {
		word := audioEvidenceWord(500 + index%11)
		a.Audio[index].Fingerprint, b.Audio[index].Fingerprint = word, word
	}
	match, reason, err := measureAudioRun(a, b, matched, 0, 95, DefaultOptions(), audioEvidenceBudget())
	if err != nil || match != nil || reason != LowAudioEntropy {
		t.Fatalf("guarded evidence borrowed the raw run's distinct words: %#v, %s, %v", match, reason, err)
	}
}

func TestAudioProjectionReevaluatesWeaknessAndRetainsSafetyReasons(t *testing.T) {
	a, b, matched := audioEvidenceFixture(160, 2*TicksPerSecond)
	a.Audio, b.Audio = a.Audio[:160], b.Audio[:160]
	a.DurationTicks, b.DurationTicks = 40*TicksPerSecond, 40*TicksPerSecond
	original, reason, err := measureAudioRun(a, b, matched, 0, 159, DefaultOptions(), audioEvidenceBudget())
	if err != nil || reason != "" || original == nil || !slices.Contains(original.reasons, AnalysisBoundary) {
		t.Fatalf("raw extraction boundary disappeared behind its guard: %#v, %s, %v", original, reason, err)
	}
	original.reasons = []Reason{WeakAudioEvidence, AnalysisBoundary, CandidateSearchLimited, PeriodicVisualEvidence}
	before := slices.Clone(original.reasons)
	interval := Interval{4 * TicksPerSecond, 36 * TicksPerSecond}
	projected, err := projectAudioEvidence(a, b, interval, interval, 0, *original, DefaultOptions(), audioEvidenceBudget())
	if err != nil || projected == nil || projected.a != interval || projected.b != interval || projected.metrics.AudioAgreementPermille != 1000 ||
		slices.Contains(projected.reasons, WeakAudioEvidence) || !slices.Contains(projected.reasons, AnalysisBoundary) ||
		!slices.Contains(projected.reasons, CandidateSearchLimited) || !slices.Contains(projected.reasons, PeriodicVisualEvidence) {
		t.Fatalf("projected local quality retained old weakness or lost independent safety facts: %#v, %v", projected, err)
	}
	if !slices.Equal(original.reasons, before) {
		t.Fatal("projection mutated the original safety facts")
	}
}

func TestAudioProjectionStillRejectsOrMarksActualWeakEvidence(t *testing.T) {
	for _, stride := range []int{8, 4} {
		a, b, matched := audioEvidenceFixture(160, 0)
		original, reason, err := measureAudioRun(a, b, matched, 0, 159, DefaultOptions(), audioEvidenceBudget())
		if err != nil || reason != "" || original == nil {
			t.Fatalf("original fixture failed: %#v, %s, %v", original, reason, err)
		}
		for index := 16; index < 144; index += stride {
			b.Audio[index].Fingerprint = ^a.Audio[index].Fingerprint
		}
		interval := Interval{4 * TicksPerSecond, 36 * TicksPerSecond}
		projected, err := projectAudioEvidence(a, b, interval, interval, 0, *original, DefaultOptions(), audioEvidenceBudget())
		if err != nil {
			t.Fatal(err)
		}
		if stride == 4 {
			if projected != nil {
				t.Fatalf("actual coverage below 800 was admitted: %#v", projected)
			}
		} else if projected == nil || projected.metrics.AudioAgreementPermille != 875 || !slices.Contains(projected.reasons, WeakAudioEvidence) {
			t.Fatalf("actual final weakness was removed: %#v", projected)
		}
	}
}

func TestFinalGroupStillReviewsActualWeakAudio(t *testing.T) {
	episodes, edges := v2ProjectionFixture(t)
	interval := Interval{5 * TicksPerSecond, 45 * TicksPerSecond}
	for index := range episodes[2].Audio {
		sample := &episodes[2].Audio[index]
		if sample.StartTicks >= interval.StartTicks && sample.EndTicks <= interval.EndTicks && index%8 == 4 {
			sample.Fingerprint = ^sample.Fingerprint
		}
	}
	ranges := map[int]Interval{0: interval, 1: interval, 2: interval}
	group, err := projectGroup(episodes, []int{0, 1, 2}, ranges, edges, DefaultOptions(), audioEvidenceBudget())
	if err != nil || group == nil || group.Status != Review || group.Metrics.AudioAgreementPermille != 875 || !slices.Contains(group.Reasons, WeakAudioEvidence) {
		t.Fatalf("actual weak final audio was promoted after removing historical weakness: %#v, %v", group, err)
	}
}

func TestRawAndGuardedAudioMeasurementChargeBudgetAndCancellation(t *testing.T) {
	a, b, matched := audioEvidenceFixture(80, 2*TicksPerSecond)
	for _, limit := range []int64{79, 159} {
		budget := &workBudget{ctx: context.Background(), limit: limit}
		match, reason, err := measureAudioRun(a, b, matched, 0, 79, DefaultOptions(), budget)
		if match != nil || reason != "" || !errors.Is(err, ErrLimit) {
			t.Fatalf("raw or guarded measurement escaped its budget: %#v, %s, %v", match, reason, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	budget := &workBudget{ctx: &cancelDuringContext{Context: ctx, cancel: cancel, after: 2}, limit: DefaultOptions().MaxComparisons}
	match, reason, err := measureAudioRun(a, b, matched, 0, 79, DefaultOptions(), budget)
	if match != nil || reason != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation before guarded measurement returned evidence: %#v, %s, %v", match, reason, err)
	}
}

func TestAlignedAudioDiscardsPartialRunsWhenMeasurementExhaustsBudget(t *testing.T) {
	a, b, _ := audioEvidenceFixture(160, 0)
	for index := 70; index < 90; index++ {
		b.Audio[index].Fingerprint = ^a.Audio[index].Fingerprint
	}
	runs, reasons, err := alignedAudio(a, b, 0, DefaultOptions(), &workBudget{ctx: context.Background(), limit: 500})
	if runs != nil || reasons != nil || !errors.Is(err, ErrLimit) {
		t.Fatalf("an exhausted later run returned partial candidates: %#v, %v, %v", runs, reasons, err)
	}
}
