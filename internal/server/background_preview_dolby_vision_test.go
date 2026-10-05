package server

import (
	"context"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func backgroundDolbyVisionPolicyTestServer() *Server {
	const device = "/dev/dri/renderD128"
	options := acceptedBackgroundDolbyVisionOptions(device)
	execution := transcode.DefaultExecutionOptions(2)
	execution.VulkanToneMapping = true
	return &Server{cfg: config.Config{Transcoding: config.TranscodingConfig{
		Hardware: transcode.Hardware{Device: device}, Execution: execution}},
		backgroundPreviews: &backgroundPreviewRuntime{available: true,
			dolbyVision: backgroundPreviewDolbyVisionState{options: &options}}}
}

func TestBackgroundDolbyVisionPolicyCapturesAcceptedProfiles(t *testing.T) {
	server := backgroundDolbyVisionPolicyTestServer()
	first, err := server.backgroundDolbyVisionOptions(context.Background())
	if err != nil || first == nil || first.Device != "/dev/dri/renderD128" || !first.AllowProfile5 || !first.AllowProfile84 || !first.AllowProfile82 {
		t.Fatalf("server omitted an accepted compatibility profile: %+v, %v", first, err)
	}
	original := *first
	*first = media.BackgroundClipDolbyVisionOptions{Device: "/dev/dri/renderD129"}
	second, err := server.backgroundDolbyVisionOptions(context.Background())
	if err != nil || second == nil || second == first || *second != original {
		t.Fatalf("caller mutated the server's admitted device or compatibility policy: %+v, %v", second, err)
	}
}

func TestBackgroundDolbyVisionPolicyCannotOutliveAdministratorAdmission(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Server)
	}{
		{"runtime absent", func(s *Server) { s.backgroundPreviews = nil }},
		{"runtime unavailable", func(s *Server) { s.backgroundPreviews.available = false }},
		{"hardware unavailable", func(s *Server) { s.cfg.Transcoding.HardwareUnavailable = true }},
		{"tone mapping disabled", func(s *Server) { s.cfg.Transcoding.Execution.VulkanToneMapping = false }},
		{"device removed", func(s *Server) { s.cfg.Transcoding.Hardware.Device = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := backgroundDolbyVisionPolicyTestServer()
			test.mutate(server)
			options, err := server.backgroundDolbyVisionOptions(context.Background())
			if options != nil || !errors.Is(err, media.ErrBackgroundClipDolbyVisionUnavailable) {
				t.Fatalf("cached profile acceptance bypassed current runtime policy: %+v, %v", options, err)
			}
		})
	}
}

func TestBackgroundDolbyVisionPolicyRejectsCancelledAdmission(t *testing.T) {
	server := backgroundDolbyVisionPolicyTestServer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options, err := server.backgroundDolbyVisionOptions(ctx)
	if options != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled generation retained cached profile admission: %+v, %v", options, err)
	}
}
