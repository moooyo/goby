//go:build !linux

package transcode

import (
	"context"
	"os"
)

func Keyframes(ctx context.Context, ffprobe string, input *os.File, streamIndex int, durationTicks int64) ([]int64, error) {
	return nil, ErrUnsupported
}
