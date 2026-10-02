//go:build !linux

package transcode

import (
	"context"
	"errors"
	"os"
)

func MeasureGeneratedAVAssociationProjection(_ context.Context, _, _ *os.File, options GeneratedAVAssociationProjectionOptions) (GeneratedAVAssociationProjection, error) {
	err := ErrUnsupported
	if options.Capture != nil {
		err = errors.Join(err, options.Capture.Close())
	}
	return GeneratedAVAssociationProjection{}, err
}
