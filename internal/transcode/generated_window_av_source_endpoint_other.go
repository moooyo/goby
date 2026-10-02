//go:build !linux

package transcode

import (
	"context"
	"os"
)

// MeasureGeneratedMP4AVSourceEndpoint requires the Linux descriptor and owned
// process-group contract. It is unsupported on other deployment targets.
func MeasureGeneratedMP4AVSourceEndpoint(context.Context, string, *os.File, int, int) (GeneratedAVSourceCertificate, error) {
	return GeneratedAVSourceCertificate{}, ErrUnsupported
}

func ValidateGeneratedMP4AVSourceEndpointIdentity(*os.File, GeneratedAVSourceCertificate) error {
	return ErrUnsupported
}
