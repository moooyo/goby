//go:build !linux

package transcode

import (
	"context"
	"os"
)

func MeasureGeneratedMP4SourceEndpoint(context.Context, *os.File, int) (GeneratedSourceEndpointCertificate, error) {
	return GeneratedSourceEndpointCertificate{}, ErrUnsupported
}

func ValidateGeneratedMP4SourceEndpointIdentity(*os.File, GeneratedSourceEndpointCertificate) error {
	return ErrUnsupported
}
