package transcode

import (
	"context"
	"os"
)

// validateSourceInput prevents either source mode from accidentally accepting
// the other's descriptor. Stream plans consume one authorized anonymous pipe;
// regular plans retain the immutable local snapshot requirements.
func validateSourceInput(input *os.File, plan Plan) (os.FileInfo, error) {
	if input == nil {
		return nil, ErrInvalidInput
	}
	info, err := input.Stat()
	if err != nil {
		return nil, ErrInvalidInput
	}
	if plan.SourceMode == "stream" {
		if info.Mode()&os.ModeNamedPipe == 0 {
			return nil, ErrInvalidInput
		}
	} else if !info.Mode().IsRegular() {
		return nil, ErrInvalidInput
	}
	return info, nil
}

// EnsureStream consumes the pipe, including on admission failures and reuse.
// Callers must obtain it from their authorized source lifecycle adapter.
func (manager *Manager) EnsureStream(ctx context.Context, spec Spec, input *os.File) (Record, error) {
	if spec.Plan.SourceMode != "stream" {
		if input != nil {
			_ = input.Close()
		}
		return Record{}, ErrInvalidPlan
	}
	return manager.Ensure(ctx, spec, input)
}
