//go:build !linux

package transcode

import (
	"context"
	"os"
)

// MeasureGeneratedSourceRange is supported only on the Linux deployment target.
func MeasureGeneratedSourceRange(ctx context.Context, ffprobe string, source *os.File, plan Plan, formatOriginTicks int64) (GeneratedSourceRange, error) {
	return GeneratedSourceRange{}, ErrUnsupported
}
