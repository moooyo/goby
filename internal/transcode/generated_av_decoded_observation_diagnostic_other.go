//go:build !linux

package transcode

import (
	"context"
	"os"
)

func MeasureGeneratedAVDecodedObservation(context.Context, string, []*os.File) (GeneratedAVDecodedObservation, error) {
	return GeneratedAVDecodedObservation{}, ErrUnsupported
}
