package introdetect

import "math/bits"

// audioEvidence counts only observed pairs. Callers decide which complete bins
// belong to the measured intervals; this accumulator never aligns new pairs.
type audioEvidence struct {
	count, distance, adjacent, changesA, changesB  int
	coveredA, coveredB, informativeA, informativeB int64
	uniqueA, uniqueB                               map[uint32]bool
	previousI, previousJ                           int
}

func newAudioEvidence() audioEvidence {
	return audioEvidence{uniqueA: make(map[uint32]bool), uniqueB: make(map[uint32]bool), previousI: -1, previousJ: -1}
}

func (e *audioEvidence) add(a, b Episode, i, j int) {
	left, right := a.Audio[i], b.Audio[j]
	spanA, spanB := left.EndTicks-left.StartTicks, right.EndTicks-right.StartTicks
	e.coveredA, e.coveredB = e.coveredA+spanA, e.coveredB+spanB
	if informativeAudio(a.Audio, i) && informativeAudio(b.Audio, j) {
		e.informativeA, e.informativeB = e.informativeA+spanA, e.informativeB+spanB
	}
	if e.previousI >= 0 && i == e.previousI+1 && j == e.previousJ+1 {
		e.adjacent++
		e.changesA += bits.OnesCount32(left.Fingerprint ^ a.Audio[e.previousI].Fingerprint)
		e.changesB += bits.OnesCount32(right.Fingerprint ^ b.Audio[e.previousJ].Fingerprint)
	}
	e.uniqueA[left.Fingerprint], e.uniqueB[right.Fingerprint] = true, true
	e.count++
	e.distance += bits.OnesCount32(left.Fingerprint ^ right.Fingerprint)
	e.previousI, e.previousJ = i, j
}

func (e *audioEvidence) measure(ar, br Interval, o Options) (Metrics, Reason) {
	if ar.EndTicks <= ar.StartTicks || br.EndTicks <= br.StartTicks {
		return Metrics{}, InsufficientAudio
	}
	distinct := min(len(e.uniqueA), len(e.uniqueB))
	information := min(int(e.informativeA*1000/max(int64(1), e.coveredA)), int(e.informativeB*1000/max(int64(1), e.coveredB)))
	if e.count == 0 || distinct < 12 || e.adjacent == 0 || min(e.changesA, e.changesB) < 2*e.adjacent || information < o.MinAudioInformation {
		return Metrics{}, LowAudioEntropy
	}
	agreement := min(int(e.coveredA*1000/(ar.EndTicks-ar.StartTicks)), int(e.coveredB*1000/(br.EndTicks-br.StartTicks)))
	if agreement < 800 {
		return Metrics{}, InsufficientAudio
	}
	return Metrics{AudioAgreementPermille: min(1000, agreement), AudioInformativePermille: information,
		AudioSimilarityPermille: 1000 - e.distance*1000/(e.count*32), AudioSamples: e.count,
		AudioDistinct: distinct, PairCount: 1}, ""
}

// measureMatchedAudioEvidence retains the original one-to-one alignment. In
// particular, removing a boundary bin cannot free its target for a new match.
func measureMatchedAudioEvidence(a, b Episode, matched []int, first, last int, ar, br Interval, o Options, budget *workBudget) (Metrics, Reason, error) {
	if err := budget.ctx.Err(); err != nil {
		return Metrics{}, "", err
	}
	evidence := newAudioEvidence()
	for i := first; i <= last; i++ {
		if err := budget.spend(); err != nil {
			return Metrics{}, "", err
		}
		j := matched[i]
		if j < 0 || a.Audio[i].StartTicks < ar.StartTicks || a.Audio[i].EndTicks > ar.EndTicks ||
			b.Audio[j].StartTicks < br.StartTicks || b.Audio[j].EndTicks > br.EndTicks {
			continue
		}
		evidence.add(a, b, i, j)
	}
	metrics, reason := evidence.measure(ar, br, o)
	return metrics, reason, nil
}

// WeakAudioEvidence describes one measured interval. Independent safety facts
// remain attached when the same witness is projected onto a smaller interval.
func audioReasonsForInterval(original []Reason, metrics Metrics, o Options) []Reason {
	reasons := []Reason{}
	for _, reason := range original {
		if reason != WeakAudioEvidence {
			reasons = addReason(reasons, reason)
		}
	}
	if metrics.AudioAgreementPermille < o.MinAudioAgreement || metrics.AudioSimilarityPermille < o.MinAudioSimilarity {
		reasons = addReason(reasons, WeakAudioEvidence)
	}
	return reasons
}
