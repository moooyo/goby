//go:build !linux

package transcode

import (
	"context"
	"os"
)

func CompareGeneratedAVPCMOverlapDifferenceDiagnostic(context.Context, *os.File, *os.File, GeneratedAVPCMOverlapDifferenceOptions) (GeneratedAVPCMOverlapDifferenceDiagnostic, error) {
	return GeneratedAVPCMOverlapDifferenceDiagnostic{}, ErrUnsupported
}
