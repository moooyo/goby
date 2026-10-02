//go:build !linux

package transcode

import (
	"context"
	"errors"
	"os"
)

// Run is available for cross-platform builds, but media conversion is supported
// only on the Linux deployment target.
func Run(ctx context.Context, _ string, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
	if lifecycle := resourceLifecycleFromContext(ctx); lifecycle != nil {
		if err := lifecycle.runnerReturned(context.WithoutCancel(ctx)); err != nil {
			return RunResult{ExitCode: -1}, errors.Join(ErrUnsupported, err)
		}
	}
	return RunResult{ExitCode: -1}, ErrUnsupported
}
