//go:build !linux

package transcode

import (
	"context"
	"os"
)

func generatedAVCaptureEffectiveJSON(context.Context, []byte, []*os.File) error {
	return ErrUnsupported
}
