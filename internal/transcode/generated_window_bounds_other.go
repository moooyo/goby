//go:build !linux

package transcode

import (
	"context"
	"os"
)

// MeasureGeneratedSegmentBounds is available only on the Linux deployment target.
func MeasureGeneratedSegmentBounds(ctx context.Context, ffprobe string, initialization, segment *os.File, video bool) (GeneratedSegmentBounds, error) {
	return GeneratedSegmentBounds{}, ErrUnsupported
}
