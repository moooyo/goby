package library

import (
	"math"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestStopPositionUsesOverflowSafeResumeBoundaries(t *testing.T) {
	second := media.TicksPerSecond
	for _, fixture := range []struct {
		name                             string
		position, duration, wantPosition int64
		completed                        bool
	}{
		{"before minimum", 11 * second, 600 * second, 0, false},
		{"at minimum", 12 * second, 600 * second, 12 * second, false},
		{"below completion", 539 * second, 600 * second, 539 * second, false},
		{"at completion", 540 * second, 600 * second, 0, true},
		{"at end", 600 * second, 600 * second, 0, true},
		{"short media", 50 * second, 119 * second, 0, false},
		{"unknown duration", 0, 0, 0, false},
		{"large partial", math.MaxInt64 / 2, math.MaxInt64, math.MaxInt64 / 2, false},
		{"large completed", math.MaxInt64, math.MaxInt64, 0, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			position, completed := stopPosition(fixture.position, fixture.duration)
			if position != fixture.wantPosition || completed != fixture.completed {
				t.Errorf("stopPosition = (%d, %v), want (%d, %v)", position, completed, fixture.wantPosition, fixture.completed)
			}
		})
	}
}
