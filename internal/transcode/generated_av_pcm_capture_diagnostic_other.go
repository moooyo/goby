//go:build !linux

package transcode

import (
	"context"
	"errors"
	"os"
)

func MeasureGeneratedAVPCMCaptureDiagnostic(_ context.Context, source *os.File, files []*os.File, options GeneratedAVPCMCaptureOptions) (GeneratedAVPCMCaptureDiagnostic, error) {
	if generatedAVPCMCaptureAliasesBorrowed(source, files, options.Capture) {
		return GeneratedAVPCMCaptureDiagnostic{Role: options.Role, Stage: "preflight"}, ErrInvalidInput
	}
	err := ErrUnsupported
	if options.Capture != nil {
		err = errors.Join(err, options.Capture.Close())
	}
	return GeneratedAVPCMCaptureDiagnostic{Role: options.Role, Stage: "preflight"}, err
}
