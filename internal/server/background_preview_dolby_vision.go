package server

import (
	"context"
	"sync"

	"github.com/moooyo/goby/internal/media"
)

type backgroundPreviewDolbyVisionState struct {
	mu      sync.Mutex
	options *media.BackgroundClipDolbyVisionOptions
}

// Device capability is inspected only during admitted generation. Every use
// rechecks the current administrator policy; disabling tone mapping takes
// effect without regenerating or deleting any existing artifact.
func (s *Server) backgroundDolbyVisionOptions(ctx context.Context) (*media.BackgroundClipDolbyVisionOptions, error) {
	r := s.backgroundPreviews
	if r == nil || !r.available {
		return nil, media.ErrBackgroundClipDolbyVisionUnavailable
	}
	settings := s.currentSettingsSnapshot()
	if settings.HardwareUnavailable || !settings.Execution.VulkanToneMapping || settings.Hardware.Device == "" {
		return nil, media.ErrBackgroundClipDolbyVisionUnavailable
	}
	r.dolbyVision.mu.Lock()
	defer r.dolbyVision.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.dolbyVision.options != nil && r.dolbyVision.options.Device == settings.Hardware.Device {
		value := *r.dolbyVision.options
		return &value, nil
	}
	capability, err := r.extractor.BackgroundClipDolbyVisionAvailability(ctx, settings.Hardware.Device)
	if err != nil {
		return nil, err
	}
	if !capability.Available || capability.FFmpegSHA256 != r.extractor.ExpectedFFmpegSHA256 || capability.FFprobeSHA256 != r.extractor.ExpectedFFprobeSHA256 {
		return nil, media.ErrBackgroundClipDolbyVisionUnavailable
	}
	value := acceptedBackgroundDolbyVisionOptions(settings.Hardware.Device)
	stored := value
	r.dolbyVision.options = &stored
	return &value, nil
}

// This policy is applied only after the private renderer and selected device
// pass the runtime dependency check. Source-bound admission and strict per-frame
// RPU validation remain mandatory for every generated clip.
func acceptedBackgroundDolbyVisionOptions(device string) media.BackgroundClipDolbyVisionOptions {
	return media.BackgroundClipDolbyVisionOptions{Device: device, AllowProfile5: true, AllowProfile84: true, AllowProfile82: true}
}
