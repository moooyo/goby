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
	return manager.EnsureStreamInputs(ctx, spec, StreamInputs{Media: input})
}

// EnsureStreamInputs consumes every supplied descriptor on success, reuse, or
// failure. Additional readers must already belong to the same source lease.
func (manager *Manager) EnsureStreamInputs(ctx context.Context, spec Spec, inputs StreamInputs) (Record, error) {
	if spec.Plan.SourceMode != "stream" {
		inputs.close()
		return Record{}, ErrInvalidPlan
	}
	return manager.ensureInputs(ctx, spec, inputs)
}

func validateStreamInputs(inputs StreamInputs, plan Plan) error {
	if plan.SourceMode != "stream" {
		if inputs.Bitmap != nil {
			return ErrInvalidInput
		}
		return nil
	}
	primary, err := validateSourceInput(inputs.Media, plan)
	if err != nil {
		return err
	}
	if hasBitmapSubtitleInput(plan) {
		if inputs.Bitmap == nil || inputs.Bitmap == inputs.Media {
			return ErrInvalidInput
		}
		bitmap, err := inputs.Bitmap.Stat()
		if err != nil || bitmap.Mode()&os.ModeNamedPipe == 0 || os.SameFile(primary, bitmap) {
			return ErrInvalidInput
		}
	} else if inputs.Bitmap != nil {
		return ErrInvalidInput
	}
	return nil
}

// TouchStream renews only an existing active stream's authorized consumer
// activity. Publication and monitoring must never call it on their own behalf.
// Retained output from a terminal job remains a store concern and is not revived.
func (manager *Manager) TouchStream(scope Scope, id string) error {
	if !validScope(scope) {
		return ErrInvalidScope
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job, err := manager.lookupLocked(scope, id)
	if err != nil {
		return err
	}
	if err := jobError(job); err != nil {
		return err
	}
	if manager.closing {
		return ErrManagerClosed
	}
	if job.record.Spec.Plan.SourceMode != "stream" || job.finished || job.record.State != "queued" && job.record.State != "running" {
		return ErrOutputUnavailable
	}
	manager.touchLocked(job)
	return nil
}
