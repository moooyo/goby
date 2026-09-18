//go:build !linux

package transcode

import (
	"context"
	"os"
)

// DuplicateInput is available only on the Linux deployment target.
func DuplicateInput(input *os.File) (*os.File, error) {
	return nil, ErrUnsupported
}

// MeasureHLSMuxClock is available only on the Linux deployment target.
func MeasureHLSMuxClock(ctx context.Context, ffprobe string, initialization, segment *os.File, video bool) (int64, error) {
	return 0, ErrUnsupported
}
