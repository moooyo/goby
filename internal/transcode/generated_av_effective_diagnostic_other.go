//go:build !linux

package transcode

import (
	"context"
	"os"
)

func MeasureGeneratedAVEffectiveDecodeDiagnostic(context.Context, string, []*os.File) (GeneratedAVEffectiveDecodeDiagnostic, error) {
	return GeneratedAVEffectiveDecodeDiagnostic{}, ErrUnsupported
}
func MeasureGeneratedAVPCMDiagnostic(context.Context, string, []*os.File, int, int64) (GeneratedAVPCMDiagnostic, error) {
	return GeneratedAVPCMDiagnostic{}, ErrUnsupported
}
