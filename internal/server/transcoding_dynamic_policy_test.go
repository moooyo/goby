package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

type dynamicTranscodingHDRConnector struct{}

func (dynamicTranscodingHDRConnector) Open(context.Context, dynamicsource.Definition) (*dynamicsource.Connection, error) {
	info := dynamicTestInfo()
	video := &info.Streams[0]
	video.Codec, video.Profile, video.BitDepth, video.PixelFormat = "hevc", "Main 10", 10, "yuv420p10le"
	video.Width, video.Height = 1920, 1080
	video.VideoRange, video.VideoRangeKnown = "HDR10", true
	video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = "smpte2084", "bt2020", "bt2020nc", "tv"
	return &dynamicsource.Connection{Reader: io.NopCloser(strings.NewReader("authorized upstream bytes")), Info: info}, nil
}

func TestTranscodingDynamicRevisionKeepsAdmittedCodecPriorityAcrossPolicyChanges(t *testing.T) {
	for _, hardware := range []bool{false, true} {
		t.Run(map[bool]string{false: "software", true: "hardware"}[hardware], func(t *testing.T) {
			server, principal, lease, request, jobs := dynamicServerFixture(t)
			old := server.currentSettingsSnapshot()
			old.CodecConfigurations = transcode.DefaultCodecConfigurations()
			old.CodecConfigurations[1].Priority = 100
			if hardware {
				server.hardwareEncoding = hardwareEncodingTestRuntime(t)
				old.hardwareFallback = transcode.Hardware{Encode: "vaapi", Device: "/dev/dri/renderD128"}
				request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "hevc,h264"
			} else {
				request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "h264,hevc"
			}
			copyDisabled := false
			request.AllowVideoStreamCopy = &copyDisabled
			initial := managedExecutionRequest(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), old)
			dto, err := server.dynamicPlaybackDTO(initial, principal, lease, request)
			if err != nil {
				t.Fatal(err)
			}
			target, err := url.Parse(dto["TranscodingUrl"].(string))
			if err != nil {
				t.Fatal(err)
			}
			oldID := target.Query().Get("GobyLiveId")
			server.dynamicStreams.mu.Lock()
			bound := server.dynamicStreams.sessions[oldID]
			server.dynamicStreams.mu.Unlock()
			if bound == nil || bound.key.plan.VideoCodec != "hevc" {
				t.Fatal("fixture did not admit the preferred HEVC revision")
			}
			changed := old
			changed.Revision++
			changed.Transcoding.EnableHevcEncoding, changed.Transcoding.EnableHardwareEncoding = false, false
			changed.Transcoding.EnableSubtitleExtraction, changed.Transcoding.EnableOnTheFlyAttachmentExtraction = false, false
			changed.CodecConfigurations[0].Priority = 200
			changed.CodecConfigurations[1].IsEnabled = false
			get := managedExecutionRequest(dynamicTestRequest(principal, http.MethodGet, target.String()), changed)
			get.SetPathValue("LiveStreamId", lease.ID)
			values, _ := hlsValues(get)
			session, _, err := server.findDynamicSession(get, values)
			if err != nil || session != bound {
				t.Fatalf("new policy retired an admitted dynamic revision: %v", err)
			}
			if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil {
				t.Fatal(err)
			}
			jobs.mu.Lock()
			stable := len(jobs.specs) == 1 && jobs.specs[0].Plan.VideoCodec == "hevc" && jobs.specs[0].Plan.Hardware == bound.key.plan.Hardware && jobs.cancelled == 0
			jobs.mu.Unlock()
			if !stable {
				t.Fatal("delayed dynamic production lost admitted encoder policy")
			}
			limits, err := server.dynamicLimits(get.Context(), principal, request, get)
			if err != nil || !limits.DisableHevcEncoding || !limits.DisableHardwareEncoding || limits.CodecConfigurations[1].IsEnabled {
				t.Fatal("new dynamic admission did not receive changed policy")
			}
			fresh, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), request, limits)
			if err != nil || fresh.Plan == nil || fresh.Plan.VideoCodec != "h264" || fresh.Plan.Hardware.Encode != "software" {
				t.Fatalf("new admission did not fall through to its permitted CPU encoder: %+v, %v", fresh, err)
			}
		})
	}
}

func TestTranscodingDynamicRevisionRetainsConcreteToneMappedGeometry(t *testing.T) {
	server, principal, lease, request, jobs := dynamicServerFixture(t, dynamicTranscodingHDRConnector{})
	old := server.currentSettingsSnapshot()
	old.Execution.SoftwareToneMapOptions = transcode.SoftwareToneMapOptions{ToneMapSizeLimit: "Limit420", SoftwareToneMapAlgorithm: "Mobius"}
	copyDisabled := false
	request.AllowVideoStreamCopy = &copyDisabled
	initial := managedExecutionRequest(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), old)
	dto, err := server.dynamicPlaybackDTO(initial, principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(dto["TranscodingUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	get := managedExecutionRequest(dynamicTestRequest(principal, http.MethodGet, target.String()), old)
	get.SetPathValue("LiveStreamId", lease.ID)
	values, _ := hlsValues(get)
	session, _, err := server.findDynamicSession(get, values)
	if err != nil || session == nil || session.key.plan.Width != 746 || session.key.plan.Height != 420 || session.key.plan.Execution.SoftwareToneMapOptions.ToneMapSizeLimit != "None" {
		t.Fatalf("first artifact lookup enlarged canonicalized tone mapping geometry: %v", err)
	}
	bound := session.key.plan
	if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil {
		t.Fatal(err)
	}
	jobs.mu.Lock()
	stable := len(jobs.specs) == 1 && jobs.specs[0].Plan.Width == bound.Width && jobs.specs[0].Plan.Height == bound.Height && jobs.cancelled == 0
	jobs.mu.Unlock()
	if !stable {
		t.Fatal("delayed dynamic production enlarged admitted tone mapped output")
	}
	changed := old
	changed.Effective.MaxWidth = 746
	get = managedExecutionRequest(dynamicTestRequest(principal, http.MethodGet, target.String()), changed)
	get.SetPathValue("LiveStreamId", lease.ID)
	if retained, _, err := server.findDynamicSession(get, values); err != nil || retained != session || retained.key.plan.Width != 746 || retained.key.plan.Height != 420 {
		t.Fatalf("an equal width ceiling repeated aspect-ratio rounding to a different retained geometry: %v", err)
	}
	changed.Effective.MaxHeight = 240
	get = managedExecutionRequest(dynamicTestRequest(principal, http.MethodGet, target.String()), changed)
	get.SetPathValue("LiveStreamId", lease.ID)
	if _, _, err := server.findDynamicSession(get, values); err == nil {
		t.Fatal("retained geometry bypassed a stricter current output ceiling")
	}
}

func TestTranscodingDynamicRevisionRetainsActualCodecAfterHardwareFallback(t *testing.T) {
	server, principal, lease, request, jobs := dynamicServerFixture(t)
	runtime := hardwareEncodingTestRuntime(t)
	var probeCount atomic.Int32
	runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		probeCount.Add(1)
		return media.HardwareEncodingResult{Code: "hardware_encoding_output_mismatch"}, nil
	}
	server.hardwareEncoding = runtime
	old := server.currentSettingsSnapshot()
	old.hardwareFallback = transcode.Hardware{Encode: "vaapi", Device: "/dev/dri/renderD128"}
	old.CodecConfigurations = transcode.DefaultCodecConfigurations()
	old.CodecConfigurations[1].Priority = 100
	request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "h264,hevc"
	copyDisabled := false
	request.AllowVideoStreamCopy = &copyDisabled
	initial := managedExecutionRequest(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), old)
	dto, err := server.dynamicPlaybackDTO(initial, principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(dto["TranscodingUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	server.dynamicStreams.mu.Lock()
	initialSession := server.dynamicStreams.sessions[target.Query().Get("GobyLiveId")]
	server.dynamicStreams.mu.Unlock()
	if initialSession == nil || initialSession.key.plan.VideoCodec != "h264" || initialSession.key.plan.Hardware.Encode != "software" || probeCount.Load() != 1 {
		t.Fatal("fixture did not execute a VAAPI H.264 probe and admit its concrete CPU fallback")
	}
	get := managedExecutionRequest(dynamicTestRequest(principal, http.MethodGet, target.String()), old)
	get.SetPathValue("LiveStreamId", lease.ID)
	values, _ := hlsValues(get)
	session, _, err := server.findDynamicSession(get, values)
	if err != nil || session == nil || session.key.plan.VideoCodec != "h264" || session.key.plan.Hardware.Encode != "software" {
		t.Fatalf("first unchanged-policy lookup reselected HEVC after CPU fallback: %v", err)
	}
	get.SetPathValue("Artifact", "master.m3u8")
	response := httptest.NewRecorder()
	server.dynamicHLSArtifact(response, get)
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Body.String(), "#EXTM3U\n") || !strings.Contains(response.Body.String(), "RESOLUTION=1280x720") || probeCount.Load() != 1 {
		t.Fatalf("first master request changed or retired the admitted fallback revision: %d, %s", response.Code, response.Body.String())
	}
	if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil {
		t.Fatal(err)
	}
	jobs.mu.Lock()
	stable := len(jobs.specs) == 1 && jobs.specs[0].Plan.VideoCodec == "h264" && jobs.specs[0].Plan.Hardware.Encode == "software" && jobs.cancelled == 0
	jobs.mu.Unlock()
	if !stable {
		t.Fatal("delayed production reapplied software priority to the retained fallback codec")
	}
	changed := old
	changed.Transcoding.EnableHardwareEncoding = false
	get = managedExecutionRequest(dynamicTestRequest(principal, http.MethodGet, target.String()), changed)
	get.SetPathValue("LiveStreamId", lease.ID)
	if retained, _, err := server.findDynamicSession(get, values); err != nil || retained != session {
		t.Fatal("a later CPU policy reselected an encoder for an admitted fallback revision")
	}
	limits, err := server.dynamicLimits(get.Context(), principal, request, get)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), request, limits)
	if err != nil || fresh.Plan == nil || fresh.Plan.VideoCodec != "hevc" || fresh.Plan.Hardware.Encode != "software" {
		t.Fatalf("new CPU admission ignored native software encoder priority: %+v, %v", fresh, err)
	}
}
