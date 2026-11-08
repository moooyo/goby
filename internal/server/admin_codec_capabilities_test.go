package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/settings"
)

func TestCodecInventoryDoesNotPromoteCompiledInterfacesToExecutionProof(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	compiled := compiledCodecSnapshot{state: "current", observed: now, value: media.Capabilities{
		Version: "fixture", HardwareVerified: true, HardwareAccelerators: []string{"vaapi"},
		Decoders: []string{"h264", "hevc", "libdav1d", "vp9"}, Encoders: []string{"libx264", "libx265", "libaom-av1", "h264_vaapi", "hevc_vaapi", "vp9_vaapi"}}}
	result := projectAdminCodecCapabilities(compiled, settings.HardwareSelection{Decode: "vaapi", Encode: "vaapi", DeviceID: "fixture-device"},
		[]managedHardwareDevice{{DeviceID: "fixture-device", Available: true}}, true, nil, now)
	for _, item := range result.Items {
		if len(item.EncodingProofs) != 0 {
			t.Fatalf("compiled interface invented an encoding proof: %+v", item)
		}
		if item.Codec == "vp9" && (item.HardwareEncode.Managed || item.HardwareEncode.Selected || item.SoftwareEncode.Managed) {
			t.Fatalf("FFmpeg availability expanded Goby output policy: %+v", item)
		}
		if item.Codec == "h264" && (item.HardwareDecode.Compiled == nil || !*item.HardwareDecode.Compiled || item.HardwareEncode.Compiled == nil || !*item.HardwareEncode.Compiled) {
			t.Fatalf("known compiled interfaces were omitted: %+v", item)
		}
	}
	compiled.state = "stale"
	stale := projectAdminCodecCapabilities(compiled, settings.HardwareSelection{}, nil, false, nil, now)
	for _, item := range stale.Items {
		if item.SoftwareDecode.Compiled != nil || item.HardwareEncode.Compiled != nil {
			t.Fatalf("stale names were presented as current support: %+v", item)
		}
	}
}

func TestCodecInventoryRefreshBindsToolIdentityAndExpiresWithoutRerunning(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	now := time.Now()
	runtime.now = func() time.Time { return now }
	identity := "first-tool"
	runtime.toolIdentity = func(context.Context, string) (string, error) { return identity, nil }
	probes := 0
	runtime.capabilityProbe = func(context.Context, string) (media.Capabilities, error) {
		probes++
		return media.Capabilities{Version: "fixture", HardwareVerified: true, Encoders: []string{"libx264"}}, nil
	}
	if got := runtime.compiledCodecs(context.Background(), "fixture"); got.state != "unknown" || probes != 0 {
		t.Fatalf("read unexpectedly started a probe: %+v probes=%d", got, probes)
	}
	if err := runtime.refreshCompiledCodecs(context.Background(), "fixture"); err != nil {
		t.Fatal(err)
	}
	got := runtime.compiledCodecs(context.Background(), "fixture")
	if got.state != "current" || got.value.HardwareVerified || probes != 1 {
		t.Fatalf("compiled cache claimed hardware verification: %+v probes=%d", got, probes)
	}
	got.value.Encoders[0] = "caller-mutation"
	if next := runtime.compiledCodecs(context.Background(), "fixture"); next.value.Encoders[0] != "libx264" {
		t.Fatal("capability projection exposed mutable cache storage")
	}
	identity = "replaced-tool"
	if got := runtime.compiledCodecs(context.Background(), "fixture"); got.state != "stale" || probes != 1 {
		t.Fatalf("replaced tool reused current support or triggered a probe: %+v probes=%d", got, probes)
	}
	identity = "first-tool"
	now = now.Add(compiledCodecLifetime)
	if got := runtime.compiledCodecs(context.Background(), "fixture"); got.state != "stale" || probes != 1 {
		t.Fatalf("expired support did not remain unknown: %+v probes=%d", got, probes)
	}
}

func TestCodecInventoryRefreshRejectsChangedToolAndDoesNotBypassFiniteSlot(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	identity := "first-tool"
	runtime.toolIdentity = func(context.Context, string) (string, error) { return identity, nil }
	runtime.capabilityProbe = func(context.Context, string) (media.Capabilities, error) {
		identity = "second-tool"
		return media.Capabilities{Encoders: []string{"libx264"}}, nil
	}
	if err := runtime.refreshCompiledCodecs(context.Background(), "fixture"); err == nil {
		t.Fatal("tool replacement during enumeration was accepted")
	}
	if got := runtime.compiledCodecs(context.Background(), "fixture"); got.state != "unknown" {
		t.Fatalf("rejected observation entered cache: %+v", got)
	}
	runtime.slot <- struct{}{}
	if err := runtime.refreshCompiledCodecs(context.Background(), "fixture"); !errors.Is(err, errCodecInventoryBusy) {
		t.Fatalf("inventory bypassed shared process slot: %v", err)
	}
	<-runtime.slot
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.refreshCompiledCodecs(ctx, "fixture"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inventory started work: %v", err)
	}
}
