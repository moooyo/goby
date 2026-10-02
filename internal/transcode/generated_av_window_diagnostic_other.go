//go:build !linux

package transcode

import (
	"context"
	"os"
)

func RunGeneratedAVWindowDiagnostic(context.Context, *os.File, GeneratedAVWindowDiagnosticOptions) (GeneratedAVWindowDiagnostic, error) {
	return GeneratedAVWindowDiagnostic{}, ErrUnsupported
}
