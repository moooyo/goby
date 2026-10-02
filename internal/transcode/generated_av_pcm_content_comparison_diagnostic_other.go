//go:build !linux

package transcode

import (
	"context"
	"os"
)

func CompareGeneratedAVPCMContentDiagnostic(context.Context, *os.File, *os.File, GeneratedAVPCMContentOptions) (GeneratedAVPCMContentCandidate, error) {
	return GeneratedAVPCMContentCandidate{}, ErrUnsupported
}
