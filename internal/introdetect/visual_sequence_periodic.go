package introdetect

import "math/bits"

// Reject only strong fixed-period recurrence seen over at least three cycles.
// Both source clocks must confirm the period; irregular frame gaps cannot
// manufacture a cycle merely by returning to the same state later in a clip.
func sequencePeriodic(a, b Episode, matches [][2]int, budget *workBudget) (bool, error) {
	for lag := 2; lag <= min(16, len(matches)/3); lag++ {
		periodA := a.Visual[matches[lag][0]].Ticks - a.Visual[matches[0][0]].Ticks
		periodB := b.Visual[matches[lag][1]].Ticks - b.Visual[matches[0][1]].Ticks
		repeated := 0
		for i := lag; i < len(matches); i++ {
			if err := budget.spend(); err != nil {
				return false, err
			}
			x, prior := matches[i], matches[i-lag]
			ax, ap := a.Visual[x[0]], a.Visual[prior[0]]
			bx, bp := b.Visual[x[1]], b.Visual[prior[1]]
			if absolute(ax.Ticks-ap.Ticks-periodA) <= visualSequenceResidual && absolute(bx.Ticks-bp.Ticks-periodB) <= visualSequenceResidual &&
				bits.OnesCount64(ax.Hash^ap.Hash) <= 8 && bits.OnesCount64(bx.Hash^bp.Hash) <= 8 && sequenceClose(ax, ap) && sequenceClose(bx, bp) {
				repeated++
			}
			if repeated*1000 >= 900*(len(matches)-lag) {
				return true, nil
			}
			// Even matching every remaining observation cannot satisfy this
			// lag. This bound changes work, not the recurrence threshold.
			remaining := len(matches) - 1 - i
			if (repeated+remaining)*1000 < 900*(len(matches)-lag) {
				break
			}
		}
	}
	return false, nil
}
