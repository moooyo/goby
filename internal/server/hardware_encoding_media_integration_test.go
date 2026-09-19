//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestHTTPHardwareAV1PaddingFallsBackToExactSoftwareOutput(t *testing.T) {
	if os.Getenv("GOBY_TEST_VAAPI_EXPECT_AV1_PADDING") != "1" {
		t.Skip("GOBY_TEST_VAAPI_EXPECT_AV1_PADDING=1 selects a worker with independently observed AV1 padding")
	}
	device := os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if device == "" || os.Getenv("GOBY_FFMPEG") == "" || os.Getenv("GOBY_FFPROBE") == "" {
		t.Fatal("the explicit VAAPI device, FFmpeg, and ffprobe paths are required")
	}
	fixture := newHLSHTTPFixture(t)
	item := hardwareEncodingHTTPSource(t, fixture)
	fixture.f.app.cfg.Transcoding.Hardware = transcode.Hardware{Decode: "software", Encode: "vaapi", Device: device}
	if err := fixture.f.app.cfg.Transcoding.Validate(); err != nil {
		t.Fatalf("the configured hardware fixture is invalid: %v", err)
	}
	fixture.f.cfg = fixture.f.app.cfg
	initializeFixtureSettings(t, fixture.f)
	// Keep the production identity, encoder enumeration, and hardware probe
	// functions intact. This test must observe the actual driver rejection.
	runtime := fixture.f.app.hardwareEncodingRuntime()
	t.Cleanup(runtime.cancel)
	runtime.mu.Lock()
	initialEntries := len(runtime.entries)
	runtime.mu.Unlock()
	if initialEntries != 0 {
		t.Fatal("the hardware admission cache must be cold before PlaybackInfo")
	}

	profile := videoHTTPProfile("http", false, false)
	profile["VideoCodec"] = "av1"
	profile["MaxWidth"], profile["MaxHeight"] = 320, 180
	body := videoHTTPBody(false, profile)
	body["DeviceProfile"].(map[string]any)["CodecProfiles"] = []map[string]any{
		{"Type": "Video", "Container": "mp4", "Codec": "av1", "Conditions": []map[string]any{
			audioPIHTTPCondition("Width", "Equals", "320"),
			audioPIHTTPCondition("Height", "Equals", "180"),
			audioPIHTTPCondition("VideoBitDepth", "Equals", "8"),
			audioPIHTTPCondition("VideoProfile", "Equals", "Main"),
			audioPIHTTPCondition("VideoBitrate", "Equals", "300000"),
		}},
	}
	prepared := videoHTTPPrepare(t, fixture, item, body, "http")
	for name, want := range map[string]string{"VideoCodec": "av1", "VideoProfile": "main", "VideoBitDepth": "8", "Width": "320", "Height": "180", "VideoBitrate": "300000"} {
		if prepared.uri.Query().Get(name) != want {
			t.Fatalf("hardware rejection changed the negotiated %s target", name)
		}
	}
	if videoHTTPJobCount(t, fixture, prepared.playID, false) != 0 {
		t.Fatal("hardware admission started the full media producer during PlaybackInfo")
	}
	hardwareEncodingHTTPAssertPaddingRejected(t, runtime, device)

	response := fixture.request(t, http.MethodGet, prepared.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	videoHTTPVerifyMP4(t, fixture, response.body, videoHTTPOutput{codec: "av1", pixelFormat: "yuv420p",
		width: 320, height: 180, frames: 48, seconds: 2, color: [3]int{255, 0, 0}})
	path := filepath.Join(t.TempDir(), "fallback.mp4")
	if err := os.WriteFile(path, response.body, 0600); err != nil {
		t.Fatal(err)
	}
	var probed struct {
		Streams []struct {
			Codec       string `json:"codec_name"`
			Profile     string `json:"profile"`
			PixelFormat string `json:"pix_fmt"`
			Tag         string `json:"codec_tag_string"`
		} `json:"streams"`
	}
	raw := hlsHTTPMediaCommand(t, fixture.ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries",
		"stream=codec_name,profile,pix_fmt,codec_tag_string", "-of", "json", path)
	if json.Unmarshal(raw, &probed) != nil || len(probed.Streams) != 1 || probed.Streams[0].Codec != "av1" ||
		!strings.EqualFold(probed.Streams[0].Profile, "main") || probed.Streams[0].PixelFormat != "yuv420p" || probed.Streams[0].Tag != "av01" {
		t.Fatal("the actual software output did not retain AV1 Main 8-bit MP4 framing")
	}
	session, record := videoHTTPRecord(t, fixture, prepared.playID, "av1", 0)
	plan := record.Spec.Plan
	if record.State != "completed" || plan.Hardware.Encode != "software" || plan.Hardware.Decode != "software" || plan.Hardware.Device != "" ||
		plan.VideoCodec != "av1" || transcode.VideoOutputProfile(plan) != "main" || transcode.VideoOutputBitDepth(plan) != 8 ||
		plan.Width != 320 || plan.Height != 180 || plan.VideoBitrate != 300000 {
		t.Fatalf("the recorded producer violated the exact software fallback contract: %+v", plan)
	}
	if fixture.f.app.cfg.Transcoding.Hardware.Encode != "vaapi" || fixture.f.app.cfg.Transcoding.Hardware.Device != device {
		t.Fatal("one rejected tuple rewrote the server's hardware configuration")
	}
	hardwareEncodingHTTPAssertPaddingRejected(t, runtime, device)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, videoHTTPStopPath(fixture.accounts.viewer, prepared.playID), nil, fixture.accounts.viewer.headers), http.StatusNoContent)
	videoHTTPWaitRetired(t, fixture, prepared.playID, []*hlsSession{session})
}

func hardwareEncodingHTTPAssertPaddingRejected(t *testing.T, runtime *hardwareEncodingRuntime, device string) {
	t.Helper()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	found := false
	for key, entry := range runtime.entries {
		request := key.request
		if request.Device != device || request.Codec != "av1" || request.Profile != "main" || request.BitDepth != 8 ||
			request.Width != 320 || request.Height != 180 || request.Bitrate != 300000 {
			continue
		}
		found = true
		if key.identity == "" || entry.state != hardwareEncodingRejected || entry.code != "hardware_encoding_output_mismatch" {
			t.Fatalf("the endpoint did not retain the real padding rejection: state=%d code=%s", entry.state, entry.code)
		}
	}
	if !found {
		t.Fatal("the endpoint never ran hardware admission for the exact requested tuple")
	}
}

func hardwareEncodingHTTPSource(t *testing.T, fixture *hlsHTTPFixture) library.Item {
	t.Helper()
	path := filepath.Join(filepath.Dir(fixture.path), "Hardware.AV1.Padding.mp4")
	hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=320x180:rate=24:duration=2", "-an", "-c:v", "libx264", "-threads:v", "1",
		"-preset", "veryfast", "-bf", "0", "-pix_fmt", "yuv420p", path)
	(&streamHTTPFixture{f: fixture.f}).rescan(t, fixture.libraryID)
	items, err := fixture.f.app.library.QueryItems(fixture.f.ctx, library.Query{UserID: fixture.accounts.admin.userID, ParentID: fixture.libraryID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items.Items {
		if item.Path != path || item.Media == nil || item.IsFolder {
			continue
		}
		if len(item.Media.Streams) != 1 || item.Media.Streams[0].CodecType != "video" || item.Media.Streams[0].Width != 320 ||
			item.Media.Streams[0].Height != 180 || item.Media.DurationTicks != 2*media.TicksPerSecond || !item.Media.FormatStartKnown {
			t.Fatal("the padding-rejection source does not have the exact declared original geometry and clock")
		}
		return item
	}
	t.Fatal("the hardware fallback source was not indexed")
	return library.Item{}
}
