package introdetect

import (
	"fmt"
	"sort"
)

// refineCliqueClocks runs only after the original reference-clock admission.
// Every selected edge has equal weight. For directed offsets d(i,j), the real
// least-squares clock relative to reference r is (sum_j d(j,i)-sum_j d(j,r))/N.
// Accumulating residuals around admitted clocks avoids summing large absolute
// offsets. One integer rounding chooses one mapping, without inspecting media,
// quality scores, band positions, or alternative hypotheses.
func refineCliqueClocks(selected []int, edges map[[2]int]pairMatch, admitted map[int]int64, o Options, budget *workBudget) (map[int]int64, error) {
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	if len(selected) == 0 || len(selected) > o.MaxEpisodes || len(admitted) != len(selected) || o.AudioAlignmentTicks < 0 {
		return nil, fmt.Errorf("%w: clock consensus witness", ErrInvalidInput)
	}
	ordered := append([]int(nil), selected...)
	sort.Ints(ordered)
	reference := ordered[0]
	sums := make(map[int]int64, len(ordered))
	for index, node := range ordered {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		clock, exists := admitted[node]
		if node < 0 || index > 0 && node == ordered[index-1] || !exists {
			return nil, fmt.Errorf("%w: clock consensus member", ErrInvalidInput)
		}
		if node == reference {
			if clock != 0 {
				return nil, fmt.Errorf("%w: clock consensus reference", ErrInvalidInput)
			}
		} else if offset, ok := hypothesisEdgeOffset(edges, reference, node); !ok || clock != offset {
			return nil, fmt.Errorf("%w: clock consensus admission", ErrInvalidInput)
		}
	}
	usable := true
	for index, left := range ordered {
		for _, right := range ordered[index+1:] {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			offset, exists := hypothesisEdgeOffset(edges, left, right)
			expected, valid := hypothesisSubtractTicks(admitted[right], admitted[left])
			if !exists || !valid || !hypothesisTicksWithin(offset, expected, o.AudioAlignmentTicks) {
				return nil, fmt.Errorf("%w: clock consensus requires an admitted clique", ErrInvalidInput)
			}
			residual, valid := hypothesisSubtractTicks(offset, expected)
			if !valid {
				usable = false
				continue
			}
			leftSum, leftOK := hypothesisSubtractTicks(sums[left], residual)
			rightSum, rightOK := addConsensusTicks(sums[right], residual)
			if !leftOK || !rightOK {
				usable = false
				continue
			}
			sums[left], sums[right] = leftSum, rightSum
		}
	}
	refined := make(map[int]int64, len(ordered))
	for _, node := range ordered {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		numerator, valid := hypothesisSubtractTicks(sums[node], sums[reference])
		if !valid {
			usable = false
			continue
		}
		clock, valid := roundConsensusClock(admitted[node], numerator, int64(len(ordered)))
		if !valid {
			usable = false
			continue
		}
		refined[node] = clock
	}
	// Fitting never admits a previously contradictory edge or crosses a frozen
	// phase anchor. The caller remeasures every pair using the chosen mapping.
	for index, left := range ordered {
		for _, right := range ordered[index+1:] {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			offset, _ := hypothesisEdgeOffset(edges, left, right)
			expected, valid := hypothesisSubtractTicks(refined[right], refined[left])
			if !valid || !hypothesisTicksWithin(offset, expected, o.AudioAlignmentTicks) {
				usable = false
			}
			edge := edges[[2]int{left, right}]
			if edge.phaseGrouped {
				anchor := edge.phaseAnchorOffset
				anchorValid := true
				if edge.left != left {
					anchor, anchorValid = hypothesisSubtractTicks(0, anchor)
				}
				if !anchorValid || !hypothesisTicksWithin(anchor, expected, o.AudioAlignmentTicks) {
					usable = false
				}
			}
		}
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	if !usable {
		return admitted, nil
	}
	return refined, nil
}

// The positive divisor is the clique size. Round the complete clock, not its
// correction: half ticks round away from zero even when the correction has the
// opposite sign to a nonzero admitted clock. No large base*divisor is needed.
func roundConsensusClock(base, numerator, divisor int64) (int64, bool) {
	quotient, remainder := numerator/divisor, numerator%divisor
	clock, valid := addConsensusTicks(base, quotient)
	if !valid {
		return 0, false
	}
	threshold := divisor/2 + divisor%2
	if remainder >= threshold {
		if divisor%2 == 0 && remainder == threshold && clock < 0 {
			return clock, true
		}
		return addConsensusTicks(clock, 1)
	}
	if remainder <= -threshold {
		if divisor%2 == 0 && remainder == -threshold && clock > 0 {
			return clock, true
		}
		return addConsensusTicks(clock, -1)
	}
	return clock, true
}

func addConsensusTicks(a, b int64) (int64, bool) {
	if b > 0 && a > (1<<63-1)-b || b < 0 && a < (-1<<63)-b {
		return 0, false
	}
	return a + b, true
}
