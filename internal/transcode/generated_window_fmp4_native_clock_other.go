//go:build !linux

package transcode

import (
	"context"
	"os"
)

func MeasureGeneratedFMP4NativeClock(context.Context, Plan, *os.File, *os.File) (GeneratedFMP4NativeClock, error) {
	return GeneratedFMP4NativeClock{}, ErrInvalidInput
}
