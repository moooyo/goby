//go:build !linux

package transcode

import (
	"context"
	"os"
)

func ValidateLiveVideoRestart(ctx context.Context, ffprobe string, initialization, segment *os.File, codec string) error {
	return ErrUnsupported
}
