//go:build !linux

package transcode

import (
	"context"
	"os"
)

func MeasureGeneratedAVTransportDiagnostic(context.Context, *os.File) (GeneratedAVTransportDiagnostic, error) {
	return GeneratedAVTransportDiagnostic{}, ErrUnsupported
}
