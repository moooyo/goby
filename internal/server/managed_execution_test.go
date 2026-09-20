package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func managedExecutionRequest(r *http.Request, snapshot requestSettingsSnapshot) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), settingsSnapshotContextKey{}, snapshot))
}

func TestManagedExecutionPlanningUsesOneDetachedRequestSnapshot(t *testing.T) {
	s := &Server{cfg: config.Config{Transcoding: config.TranscodingConfig{Enabled: true, Threads: 2}}}
	execution := transcode.DefaultExecutionOptions(7)
	execution.H264 = transcode.CPUQuality{Preset: "fast", RateControl: "capped_crf", CRF: 19}
	execution.SoftwareToneMapping = false
	snapshot := requestSettingsSnapshot{Revision: 42, Effective: settings.Values{ServerName: "one revision", MaxBitrate: 4_000_000,
		MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 6}, TranscodingMaxWidth: 1280,
		Hardware: transcode.Hardware{Decode: "software", Encode: "vaapi", Device: "/dev/dri/renderD129"}, Execution: execution}
	r := managedExecutionRequest(httptest.NewRequest(http.MethodGet, "/", nil), snapshot)
	snapshot.Execution.Threads = 11
	snapshot.Hardware.Device = "/dev/dri/renderD130"
	planning := s.requestPlanningConfig(r)
	if planning.Threads != 7 || planning.Execution != execution || planning.Hardware.Device != "/dev/dri/renderD129" ||
		planning.MaxWidth != 1280 || planning.MaxBitrate != 4_000_000 || planning.MaxAudioChannels != 6 || !planning.Enabled {
		t.Fatal("planning combined mutable or unrelated settings instead of the captured request")
	}
	limits := hlsServerLimits(planning)
	if limits.Execution != execution || limits.Hardware != planning.Hardware || limits.MaxWidth != 1280 {
		t.Fatal("server conversion limits lost a captured execution choice or ceiling")
	}
}

func TestManagedExecutionFallbackCapturesTheSelectedCPUQuality(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		return media.HardwareEncodingResult{Code: "hardware_encoding_output_mismatch"}, nil
	}
	s := &Server{hardwareEncoding: runtime}
	plan := hardwareEncodingTestPlan(t)
	execution := transcode.DefaultExecutionOptions(9)
	execution.HEVC = transcode.CPUQuality{Preset: "medium", RateControl: "capped_crf", CRF: 31}
	plan, err := transcode.CaptureExecution(plan, execution)
	if err != nil || plan.Execution.HEVC == execution.HEVC {
		t.Fatal("hardware fixture did not keep inactive CPU quality outside its cache identity")
	}
	resolved, err := s.resolveHardwareEncoding(context.Background(), playback.ConversionLimits{AllowVideoTranscode: true, Execution: execution}, playback.ConversionDecision{Plan: &plan}, true)
	if err != nil || resolved.Plan == nil || resolved.Plan.Execution.Threads != 9 || resolved.Plan.Execution.HEVC != execution.HEVC || resolved.Plan.Hardware.Encode != "software" {
		t.Fatal("admitted CPU fallback lost the original complete settings snapshot")
	}
}

func TestManagedExecutionHardwareCacheCannotOutliveAuthorization(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	request := media.HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "h264", Profile: "high", BitDepth: 8, Width: 320, Height: 180}
	var approved atomic.Bool
	approved.Store(true)
	runtime.authorizeDevice = func(string) bool { return approved.Load() }
	if result, err := runtime.check(context.Background(), "ffmpeg", "ffprobe", request, true); err != nil || !result.Usable {
		t.Fatal("fixture did not establish exact cached hardware evidence")
	}
	approved.Store(false)
	if result, err := runtime.check(context.Background(), "ffmpeg", "ffprobe", request, false); err != nil || result.Usable || result.Code != "hardware_device_unavailable" {
		t.Fatal("cached evidence authorized a removed or replaced device")
	}
}

func TestManagedExecutionHardwareWaiterRechecksAuthorization(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	request := media.HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "h264", Profile: "high", BitDepth: 8, Width: 320, Height: 180}
	var approved atomic.Bool
	approved.Store(true)
	runtime.authorizeDevice = func(string) bool { return approved.Load() }
	identified := make(chan struct{}, 1)
	runtime.identify = func(context.Context, string, string, string) (string, error) {
		select {
		case identified <- struct{}{}:
		default:
		}
		return "identity", nil
	}
	runtime.slot <- struct{}{}
	result := make(chan media.HardwareEncodingResult, 1)
	go func() {
		value, _ := runtime.check(context.Background(), "ffmpeg", "ffprobe", request, true)
		result <- value
	}()
	select {
	case <-identified:
	case <-time.After(time.Second):
		t.Fatal("waiter did not reach identity admission")
	}
	approved.Store(false)
	<-runtime.slot
	select {
	case value := <-result:
		if value.Usable || value.Code != "hardware_device_unavailable" {
			t.Fatal("waiter used a revoked device after acquiring capacity")
		}
	case <-time.After(time.Second):
		t.Fatal("hardware waiter did not finish")
	}
}

func TestManagedExecutionDynamicRevisionKeepsExecutionAcrossChangedSettings(t *testing.T) {
	s, principal, lease, request, jobs := dynamicServerFixture(t)
	old := s.currentSettingsSnapshot()
	old.Execution = transcode.DefaultExecutionOptions(4)
	old.Execution.H264 = transcode.CPUQuality{Preset: "fast", RateControl: "capped_crf", CRF: 20}
	initial := managedExecutionRequest(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), old)
	dto, err := s.dynamicPlaybackDTO(initial, principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(dto["TranscodingUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	changed := old
	changed.Revision++
	changed.Effective.ServerName = "unrelated server rename"
	changed.Execution = transcode.DefaultExecutionOptions(12)
	changed.Execution.H264 = transcode.CPUQuality{Preset: "medium", RateControl: "capped_crf", CRF: 33}
	changed.Execution.SoftwareToneMapping, changed.Execution.VulkanToneMapping = false, false
	changed.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD129"}
	changed.HardwareUnavailable = true
	get := managedExecutionRequest(dynamicTestRequest(principal, http.MethodGet, target.String()), changed)
	get.SetPathValue("LiveStreamId", lease.ID)
	values, _ := hlsValues(get)
	session, _, err := s.findDynamicSession(get, values)
	if err != nil {
		t.Fatalf("settings edit retired an accepted dynamic revision: %v", err)
	}
	if _, err := s.ensureDynamicJob(get.Context(), get, session); err != nil {
		t.Fatal(err)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.specs) != 1 || jobs.specs[0].Plan.Execution.Threads != 4 || jobs.specs[0].Plan.Execution.H264 != old.Execution.H264 ||
		jobs.specs[0].Plan.Hardware != session.key.plan.Hardware || jobs.cancelled != 0 {
		t.Fatal("a delayed producer adopted changed execution settings or retired its revision")
	}
	limits, err := s.dynamicLimits(get.Context(), principal, request, get)
	if err != nil || limits.Execution != changed.Execution || limits.Hardware != changed.Hardware || !limits.HardwareUnavailable {
		t.Fatal("new admission did not consume the latest request choices")
	}
}

func TestManagedExecutionDiagnosticCapturesConfiguredProfileAtAdmission(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, true, false)
	profile := media.DiagnosticProfile{Decode: "vaapi", Encode: "software", Device: "/dev/dri/renderD129"}
	var captures atomic.Int32
	f.runtime.captureProfile = func() (media.DiagnosticProfile, int64, bool) { captures.Add(1); return profile, 73, true }
	request := f.request(strings.Repeat("a", 32))
	request.Mode = "configured"
	result, first, err := f.runtime.start(context.Background(), f.actor, request)
	if err != nil || !first || result["SettingsRevision"] != "73" {
		t.Fatal("configured diagnostic did not capture an admission revision")
	}
	profile = media.DiagnosticProfile{Decode: "software", Encode: "software"}
	select {
	case <-f.owner.entered:
	case <-time.After(time.Second):
		t.Fatal("configured diagnostic did not start")
	}
	f.runtime.mu.Lock()
	run := f.runtime.runs[request.RequestId]
	retained := run.report.Selection.ConfiguredProfile
	f.runtime.mu.Unlock()
	if retained.Decode != "vaapi" || retained.Device != "/dev/dri/renderD129" || captures.Load() != 1 {
		t.Fatal("configured diagnostic re-read mutable settings during execution")
	}
	f.runOnce.Do(func() { close(f.runGate) })
	waitDiagnosticRuntime(t, f.runtime)
	request = f.request(strings.Repeat("b", 32))
	request.Mode = "configured"
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); !errors.Is(err, errMediaDiagnosticUnavailable) {
		t.Fatal("a new configured diagnostic ignored the current software-only selection")
	}
}

func TestManagedExecutionDiagnosticRechecksItsCapturedDeviceBeforeCommands(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, true, false)
	profile := media.DiagnosticProfile{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD129"}
	var available atomic.Bool
	available.Store(true)
	f.runtime.captureProfile = func() (media.DiagnosticProfile, int64, bool) { return profile, 74, true }
	f.runtime.validateProfile = func(actual media.DiagnosticProfile) bool {
		if actual != profile {
			t.Error("diagnostic device revalidation adopted another profile")
		}
		return available.Load()
	}
	request := f.request(strings.Repeat("c", 32))
	request.Mode = "configured"
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.owner.entered:
	case <-time.After(time.Second):
		t.Fatal("configured diagnostic did not start")
	}
	f.runtime.mu.Lock()
	run := f.runtime.runs[request.RequestId]
	f.runtime.mu.Unlock()
	available.Store(false)
	if err := f.runtime.authority(context.Background(), run); !errors.Is(err, media.ErrDiagnosticAuthority) {
		t.Fatal("diagnostic command authority accepted a replaced captured device")
	}
	waitDiagnosticRuntime(t, f.runtime)
	result, err := f.runtime.get(f.runtime.instance, request.RequestId, false)
	if err != nil || result["Code"] != "diagnostic_hardware_unavailable" || f.reserved.Load() != 0 {
		t.Fatal("device replacement lost its safe outcome or diagnostic resource closure")
	}
}
