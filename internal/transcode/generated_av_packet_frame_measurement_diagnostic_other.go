//go:build !linux

package transcode

import (
	"context"
	"errors"
	"os"
)

func MeasureGeneratedAVPacketFrameTransportAssociation(_ context.Context, _, _ *os.File, options GeneratedAVAssociationProjectionOptions) (GeneratedAVPacketFrameAssociationMeasurement, error) {
	err := ErrUnsupported
	if options.Capture != nil {
		err = errors.Join(err, options.Capture.Close())
	}
	return GeneratedAVPacketFrameAssociationMeasurement{Stage: "preflight"}, err
}
