package server

import (
	"context"
	"sync"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
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
	if !settings.Execution.VulkanToneMapping {
		return nil, media.ErrBackgroundClipDolbyVisionUnavailable
	}
	hardware, available, _ := s.resolveSettingsHardware(settings)
	if !available || hardware.Device == "" {
		return nil, media.ErrBackgroundClipDolbyVisionUnavailable
	}
	r.dolbyVision.mu.Lock()
	defer r.dolbyVision.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.dolbyVision.options != nil && r.dolbyVision.options.Device == hardware.Device {
		value := *r.dolbyVision.options
		return &value, nil
	}
	capability, err := r.extractor.BackgroundClipDolbyVisionAvailability(ctx, hardware.Device, s.backgroundClipDeviceCheck(hardware.Device))
	if err != nil {
		return nil, err
	}
	if !capability.Available || capability.FFmpegSHA256 != r.extractor.ExpectedFFmpegSHA256 || capability.FFprobeSHA256 != r.extractor.ExpectedFFprobeSHA256 {
		return nil, media.ErrBackgroundClipDolbyVisionUnavailable
	}
	value := acceptedBackgroundDolbyVisionOptions(hardware.Device)
	stored := value
	r.dolbyVision.options = &stored
	return &value, nil
}

// The callback retains the admitted device and inventory generation, never a
// request context or a later settings selection. The renderer supplies the
// current context after its capacity and source-read waits have completed.
func (s *Server) backgroundClipDeviceCheck(device string) func(context.Context) error {
	inventory := s.managedHardware
	if inventory == nil {
		return nil
	}
	hardware := transcode.Hardware{Decode: "software", Encode: "software", Device: device}
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if available, _ := inventory.checkHardware(hardware); !available {
			return media.ErrBackgroundClipDolbyVisionUnavailable
		}
		return ctx.Err()
	}
}

// This policy is applied only after the private renderer and selected device
// pass the runtime dependency check. Source-bound admission and strict per-frame
// RPU validation remain mandatory for every generated clip.
func acceptedBackgroundDolbyVisionOptions(device string) media.BackgroundClipDolbyVisionOptions {
	return media.BackgroundClipDolbyVisionOptions{Device: device, AllowProfile5: true, AllowProfile84: true, AllowProfile82: true}
}
