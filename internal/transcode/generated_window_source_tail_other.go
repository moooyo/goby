//go:build !linux

package transcode

import (
	"context"
	"os"
)

func MeasureGeneratedMP4SourceTail(context.Context, string, *os.File, Plan, GeneratedSourceEndpointCertificate) (GeneratedSourceRange, error) {
	return GeneratedSourceRange{}, ErrUnsupported
}
