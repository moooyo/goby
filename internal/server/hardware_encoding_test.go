package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func hardwareEncodingTestRuntime(t *testing.T) *hardwareEncodingRuntime {
	t.Helper()
	runtime := newHardwareEncodingRuntime()
	runtime.identify = func(context.Context, string, string, string) (string, error) { return "device-and-tools", nil }
	runtime.toolIdentity = func(context.Context, string) (string, error) { return "encoder-tool", nil }
	runtime.encoders = func(context.Context, string) ([]string, error) {
		return []string{"libx264", "libx265", "libaom-av1"}, nil
	}
	runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		return media.HardwareEncodingResult{Usable: true, Code: "hardware_encoding_usable"}, nil
	}
	t.Cleanup(runtime.cancel)
	return runtime
}

func hardwareEncodingTestPlan(t *testing.T) transcode.Plan {
	t.Helper()
	plan := transcode.Plan{OutputMode: "progressive", Container: "mp4", VideoCodec: "hevc", VideoProfile: "main10", VideoBitDepth: 10,
		VideoStreamIndex: 2, AudioStreamIndex: -1, DurationTicks: 60 * media.TicksPerSecond, Width: 320, Height: 180,
		SourceFormatStartKnown: true, FrameRate: 24, VideoBitrate: 1_000_000, Hardware: transcode.Hardware{Encode: "vaapi", Device: "/dev/dri/renderD128"}}
	plan, err := transcode.CaptureExecution(plan, transcode.DefaultExecutionOptions(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := transcode.ValidatePlan(plan); err != nil {
		t.Fatalf("hardware admission fixture must start with a valid progressive plan: %v", err)
	}
	return plan
}

func TestHardwareEncodingAdmissionPreservesContractAndGPUProcessing(t *testing.T) {
	for _, mode := range []string{"plain", "decode", "vulkan"} {
		t.Run(mode, func(t *testing.T) {
			runtime := hardwareEncodingTestRuntime(t)
			runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
				return media.HardwareEncodingResult{Code: "hardware_encoding_output_mismatch"}, nil
			}
			server := &Server{hardwareEncoding: runtime}
			plan := hardwareEncodingTestPlan(t)
			if mode == "decode" {
				plan.Hardware.Decode = "vaapi"
			}
			if mode == "vulkan" {
				plan.VideoFilters = transcode.VideoFilters{Backend: "vulkan", Deinterlace: "auto", SourceBitDepth: 10}
			}
			before := plan
			decision := playback.ConversionDecision{Plan: &plan, Method: "Transcode", Reasons: []playback.Reason{{Code: "original_reason"}}}
			resolved, err := server.resolveHardwareEncoding(context.Background(), playback.ConversionLimits{AllowVideoTranscode: true}, decision, true)
			if err != nil || resolved.Plan == nil || *resolved.Plan != softwareEncodingPlan(before) || plan != before {
				t.Fatalf("fallback changed the output contract or caller plan: %+v, %v", resolved, err)
			}
			if err := transcode.ValidatePlan(*resolved.Plan); err != nil {
				t.Fatal(err)
			}
			if len(resolved.Reasons) != 2 || resolved.Reasons[1].Code != "hardware_encoding_output_mismatch" || len(decision.Reasons) != 1 {
				t.Fatal("fallback diagnostics were lost or changed the caller decision")
			}
			if mode != "plain" && resolved.Plan.Hardware.Device != before.Hardware.Device {
				t.Fatal("encoder fallback removed the decoder or processing device")
			}
		})
	}
}

func TestHardwareEncodingAdmissionChecksEveryAdaptiveOutput(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	var requests []media.HardwareEncodingRequest
	runtime.probe = func(_ context.Context, _, _ string, request media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		requests = append(requests, request)
		return media.HardwareEncodingResult{Usable: request.Width != 128, Code: "hardware_encoding_output_mismatch"}, nil
	}
	server := &Server{hardwareEncoding: runtime}
	plan := hardwareEncodingTestPlan(t)
	plan.OutputMode, plan.Height, plan.SegmentSeconds = "", 192, 2
	plan.SourceFormatStartKnown = false
	plan.HLS = transcode.HLSPlan{SegmentType: "fmp4", RenditionCount: 3, Renditions: [transcode.MaxHLSRenditions]transcode.HLSRendition{
		{Width: 320, Height: 192, VideoBitrate: 1_000_000}, {Width: 256, Height: 160, VideoBitrate: 500_000}, {Width: 128, Height: 128, VideoBitrate: 100_000}}}
	before := plan
	resolved, err := server.resolveHardwareEncoding(context.Background(), playback.ConversionLimits{AllowVideoTranscode: true}, playback.ConversionDecision{Plan: &plan}, true)
	if err != nil || len(requests) != 3 || resolved.Plan == nil || *resolved.Plan != softwareEncodingPlan(before) || plan != before {
		t.Fatalf("an unchecked adaptive tuple remained in a hardware revision: %+v, %v", requests, err)
	}
	for index, request := range requests {
		rendition := plan.HLS.Renditions[index]
		if request.Width != rendition.Width || request.Height != rendition.Height || request.Bitrate != rendition.VideoBitrate || request.FrameRate != plan.FrameRate || request.Codec != "hevc" || request.Profile != "main10" || request.BitDepth != 10 {
			t.Fatalf("rendition %d admission used a different output tuple: %+v", index, request)
		}
	}
}

func TestHardwareEncodingFallbackReprojectsAndValidatesHEVCSampleEntry(t *testing.T) {
	for _, tagRequired := range []bool{false, true} {
		name := "unconstrained"
		if tagRequired {
			name = "hev1-required"
		}
		t.Run(name, func(t *testing.T) {
			runtime := hardwareEncodingTestRuntime(t)
			runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
				return media.HardwareEncodingResult{Code: "hardware_encoding_output_mismatch"}, nil
			}
			server := &Server{hardwareEncoding: runtime}
			limits := hlsRequestTestLimits()
			limits.Hardware = transcode.Hardware{Encode: "vaapi", Device: "/dev/dri/renderD128"}
			disabled, required := false, true
			request := playback.Request{AllowVideoStreamCopy: &disabled, DeviceProfile: &playback.DeviceProfile{
				TranscodingProfiles: []playback.TranscodingProfile{{Type: playback.DlnaProfileTypeVideo, Protocol: "hls", Container: "mp4", VideoCodec: "hevc", AudioCodec: "aac"}}}}
			if tagRequired {
				request.DeviceProfile.CodecProfiles = []playback.CodecProfile{{Type: playback.CodecTypeVideo, Codec: "hevc", Container: "mp4",
					Conditions: []playback.ProfileCondition{{Property: playback.ProfileConditionValueVideoCodecTag, Condition: playback.ProfileConditionTypeEquals, Value: "hev1", IsRequired: &required}}}}
			}
			decision, err := playback.PlanConversion(hlsRequestTestSource(), request, limits)
			if err != nil || decision.Plan == nil || decision.OutputSource.Info.Streams[0].CodecTag != "hev1" {
				t.Fatalf("fixture did not establish a valid hev1 hardware output: %+v, %v", decision, err)
			}
			resolved, err := server.resolveHardwareEncoding(context.Background(), limits, decision, true)
			if tagRequired {
				if !errors.Is(err, errHLSRequestUnsupported) || resolved.Plan != nil {
					t.Fatal("hardware fallback advertised hvc1 to a client requiring hev1")
				}
				return
			}
			if err != nil || resolved.Plan == nil || resolved.OutputSource.Info.Streams[0].CodecTag != "hvc1" ||
				resolved.OutputSource.Info.Streams[0].CodecTagString != "hvc1" || decision.OutputSource.Info.Streams[0].CodecTag != "hev1" ||
				!resolved.Output.ProfileMatched || !resolved.Output.OriginalCompatible {
				t.Fatal("hardware fallback did not retain an independently accepted software sample entry")
			}
		})
	}
}

func TestHardwareEncodingAdmissionRejectsBeforeProbeAndRequiresSoftwareEncoder(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	probes, enumerations := 0, 0
	runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		probes++
		return media.HardwareEncodingResult{Code: "hardware_encoding_output_mismatch"}, nil
	}
	runtime.encoders = func(context.Context, string) ([]string, error) { enumerations++; return []string{"libx264"}, nil }
	server := &Server{hardwareEncoding: runtime}
	plan := hardwareEncodingTestPlan(t)
	decision := playback.ConversionDecision{Plan: &plan}
	if _, err := server.resolveHardwareEncoding(context.Background(), playback.ConversionLimits{}, decision, true); !errors.Is(err, library.ErrForbidden) || probes != 0 || enumerations != 0 {
		t.Fatal("video transcoding denial started hardware work")
	}
	allowed := playback.ConversionLimits{AllowVideoTranscode: true}
	if _, err := server.resolveHardwareEncoding(context.Background(), allowed, decision, false); !errors.Is(err, errHLSRequestUnsupported) || probes != 0 || enumerations != 0 {
		t.Fatal("a cold HEAD started work or advertised an unchecked fallback")
	}
	if _, err := server.resolveHardwareEncoding(context.Background(), allowed, decision, true); !errors.Is(err, errHLSRequestUnsupported) || probes != 1 || enumerations != 1 {
		t.Fatal("an absent same-codec software encoder was advertised")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.resolveHardwareEncoding(ctx, allowed, decision, true); !errors.Is(err, context.Canceled) || probes != 1 || enumerations != 1 {
		t.Fatal("a cancelled request started work")
	}
}

func TestHardwareEncodingCacheSingleFlightAndCancelledWaiter(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	request := media.HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "av1", Profile: "main", BitDepth: 8, Width: 320, Height: 192}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	runtime.probe = func(ctx context.Context, _, _ string, request media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-ctx.Done():
			return media.HardwareEncodingResult{}, ctx.Err()
		case <-release:
			return media.HardwareEncodingResult{Usable: true, Code: "hardware_encoding_usable"}, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 12)
	var workers sync.WaitGroup
	for index := 0; index < 12; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := runtime.check(ctx, "ffmpeg", "ffprobe", request, true)
			if err == nil && !result.Usable {
				err = errors.New("same tuple lost its accepted result")
			}
			results <- err
		}()
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("probe did not start")
	}
	waiter, stop := context.WithCancel(ctx)
	stop()
	if _, err := runtime.check(waiter, "ffmpeg", "ffprobe", request, true); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled waiter was not released")
	}
	close(release)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("one exact tuple started %d concurrent probes", calls.Load())
	}
}

func TestHardwareEncodingCacheExpiresBoundsAndInvalidatesIdentity(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	runtime.now = func() time.Time { return now }
	identity := "first-driver"
	runtime.identify = func(context.Context, string, string, string) (string, error) { return identity, nil }
	calls := 0
	runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		calls++
		return media.HardwareEncodingResult{Code: "hardware_encoding_output_mismatch"}, nil
	}
	request := media.HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "av1", Profile: "main", BitDepth: 8, Width: 320, Height: 180}
	check := func() {
		t.Helper()
		if _, err := runtime.check(context.Background(), "ffmpeg", "ffprobe", request, true); err != nil {
			t.Fatal(err)
		}
	}
	check()
	check()
	if calls != 1 {
		t.Fatal("a rejected tuple was not cached")
	}
	now = now.Add(hardwareEncodingRejectTTL)
	check()
	identity = "second-driver"
	check()
	if calls != 3 {
		t.Fatal("expiration or driver replacement reused stale evidence")
	}
	for index := 0; index <= maxHardwareEncodingEntries; index++ {
		request.Width = 2 + 2*index
		check()
	}
	if len(runtime.entries) != maxHardwareEncodingEntries {
		t.Fatal("the tuple cache exceeded its fixed capacity")
	}
}

func TestHardwareEncodingCancelledProbeAndShutdownDoNotCacheFailure(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	started := make(chan struct{})
	runtime.probe = func(ctx context.Context, _, _ string, _ media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		close(started)
		<-ctx.Done()
		return media.HardwareEncodingResult{}, ctx.Err()
	}
	request := media.HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "h264", Profile: "high", BitDepth: 8, Width: 320, Height: 192}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := runtime.check(ctx, "ffmpeg", "ffprobe", request, true); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || len(runtime.entries) != 0 || len(runtime.slot) != 0 {
		t.Fatal("cancelled admission retained evidence or a process slot")
	}
	runtime.cancel()
	if _, err := runtime.check(context.Background(), "ffmpeg", "ffprobe", request, true); !errors.Is(err, context.Canceled) {
		t.Fatal("closed admission started another probe")
	}
}

func TestHardwareEncodingProbeTimeoutRemainsUnknown(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	calls := 0
	runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		calls++
		return media.HardwareEncodingResult{}, context.DeadlineExceeded
	}
	request := media.HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "hevc", Profile: "main", BitDepth: 8, Width: 320, Height: 192}
	for index := 0; index < 2; index++ {
		result, err := runtime.check(context.Background(), "ffmpeg", "ffprobe", request, true)
		if err != nil || result.Usable || result.Code != "hardware_encoding_probe_timeout" || len(runtime.entries) != 0 {
			t.Fatal("a transient probe timeout became persistent tuple evidence")
		}
	}
	if calls != 2 {
		t.Fatal("a later request could not retry an unknown tuple")
	}
}

func TestHardwareEncodingShutdownDuringIdentityCannotUseCachedEvidence(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	request := media.HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "av1", Profile: "main", BitDepth: 8, Width: 320, Height: 192}
	runtime.remember(hardwareEncodingKey{identity: "device-and-tools", request: request}, media.HardwareEncodingResult{Usable: true, Code: "hardware_encoding_usable"})
	runtime.identify = func(context.Context, string, string, string) (string, error) {
		runtime.cancel()
		return "device-and-tools", nil
	}
	if _, err := runtime.check(context.Background(), "ffmpeg", "ffprobe", request, true); !errors.Is(err, context.Canceled) {
		t.Fatal("a stopped runtime returned a cached admission")
	}
}

func TestHardwareEncodingRevisionComparisonDoesNotIgnorePolicyOutputChanges(t *testing.T) {
	plan := hardwareEncodingTestPlan(t)
	revision := softwareEncodingPlan(plan)
	if !hardwareEncodingRevisionMatches(&plan, revision) {
		t.Fatal("the authorized fallback revision could not be reauthorized")
	}
	for _, change := range []func(*transcode.Plan){
		func(p *transcode.Plan) { p.VideoBitrate++ }, func(p *transcode.Plan) { p.Width += 2 },
		func(p *transcode.Plan) { p.Hardware.Decode = "vaapi" }, func(p *transcode.Plan) { p.VideoProfile = "main" },
	} {
		changed := plan
		change(&changed)
		if hardwareEncodingRevisionMatches(&changed, revision) {
			t.Fatal("fallback matching ignored a material output or execution change")
		}
	}
}
