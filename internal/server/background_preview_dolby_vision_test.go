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

func TestBackgroundDolbyVisionDeviceCheckBindsGenerationAndCurrentContext(t *testing.T) {
	const device = "/dev/dri/renderD128"
	identity := managedHardwareTestIdentity(20)
	inspections := 0
	inventory := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{Hardware: transcode.Hardware{Device: device}},
		func(string) (managedHardwareIdentity, string) {
			inspections++
			return identity, ""
		})
	server := &Server{managedHardware: inventory}
	check := server.backgroundClipDeviceCheck(device)
	if check == nil || check(context.Background()) != nil {
		t.Fatal("captured device did not pass its current startup identity check")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	before := inspections
	if err := check(cancelled); !errors.Is(err, context.Canceled) || inspections != before {
		t.Fatal("cancelled process context inspected or admitted hardware")
	}
	if err := check(context.Background()); err != nil {
		t.Fatal("a previous caller's cancellation poisoned later device admission", err)
	}
	identity.Node.Inode++
	server.managedHardware = newManagedHardwareInventoryWithInspector(config.TranscodingConfig{Hardware: transcode.Hardware{Device: device}},
		func(string) (managedHardwareIdentity, string) { return identity, "" })
	if err := check(context.Background()); !errors.Is(err, media.ErrBackgroundClipDolbyVisionUnavailable) {
		t.Fatal("device check adopted a newer inventory generation or replacement device", err)
	}
	if err := server.backgroundClipDeviceCheck(device)(context.Background()); err != nil {
		t.Fatal("a new generation did not independently admit its own identity", err)
	}
	if check := (&Server{}).backgroundClipDeviceCheck(device); check != nil {
		t.Fatal("direct fixture without an inventory acquired a hardware callback")
	}
}
