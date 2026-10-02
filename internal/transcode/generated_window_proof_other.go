//go:build !linux

package transcode

import (
	"context"
	"os"
)

func MeasureGeneratedWindowSegment(context.Context, string, Plan, *os.File, *os.File) (GeneratedSegmentBounds, error) {
	return GeneratedSegmentBounds{}, ErrUnsupported
}
