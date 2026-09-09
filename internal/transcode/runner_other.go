//go:build !linux

package transcode

import (
	"context"
	"os"
)

// Run is available for cross-platform builds, but media conversion is supported
// only on the Linux deployment target.
func Run(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
	return RunResult{ExitCode: -1}, ErrUnsupported
}
