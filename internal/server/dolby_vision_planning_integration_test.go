//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestHTTPIngestedDolbyVisionOnlyChangesNewConversionPlans(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	root, tools := t.TempDir(), t.TempDir()
	input := []byte("declared Dolby Vision planning fixture; not a decodable movie")
	path := filepath.Join(root, "Declared Dolby Vision.mp4")
	probe, verifier := filepath.Join(tools, "probe"), filepath.Join(tools, "ffmpeg")
	writeFastIngestFile(t, path, input, 0600)
	writeFastIngestFile(t, probe, []byte(`#!/bin/sh
case "$*" in
*-show_streams*) printf '%s\n' '{"format":{"format_name":"mov,mp4","duration":"120","start_time":"0","bit_rate":"4000000"},"streams":[{"index":2,"codec_type":"video","codec_name":"hevc","profile":"Main 10","width":1280,"height":720,"bit_rate":"3800000","pix_fmt":"yuv420p10le","field_order":"progressive","avg_frame_rate":"24/1","time_base":"1/1000","color_space":"bt2020nc","color_transfer":"smpte2084","color_primaries":"bt2020","color_range":"tv","side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":8,"dv_level":6,"rpu_present_flag":1,"el_present_flag":0,"bl_present_flag":1,"dv_bl_signal_compatibility_id":1,"dv_md_compression":"none"}]},{"index":5,"codec_type":"audio","codec_name":"aac","profile":"LC","sample_rate":"48000","channels":2,"bit_rate":"192000"}]}' ;;
*) exit 1 ;;
esac
`), 0700)
	writeFastIngestFile(t, verifier, []byte(`#!/bin/sh
case "$*" in
*-bsf:v*) printf '%s\n' verifier >> "$0.called" ;;
esac
exit 1
`), 0700)
	cfg := f.cfg
	cfg.MediaRoots, cfg.FFprobePath, cfg.FFmpegPath, cfg.FastLibraryIngest = []string{root}, probe, verifier, true
	fastIngestReopen(t, f, cfg)
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	// Authorize a fixed planning-only device; no real GPU or encoder is used.
	const device = "/dev/dri/renderD128"
	f.app.cfg.Transcoding = config.TranscodingConfig{Enabled: true, Threads: 1,
		MaxBitrate: 10_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2,
		Hardware: transcode.Hardware{Decode: "software", Encode: "software", Device: device}}
	f.app.managedHardware = newManagedHardwareInventoryWithInspector(f.app.cfg.Transcoding,
		func(string) (managedHardwareIdentity, string) { return managedHardwareTestIdentity(20), "" })
	initializeFixtureSettings(t, f)
	runtime, _ := hlsRuntimeTestFixture(t)
	jobs := &dolbyVisionPlanningJobs{audioRuntimeJobs: newAudioRuntimeJobs()}
	runtime.server, runtime.verify, runtime.manager = f.app, f.app.authorizeHLS, jobs
	runtime.slots = make(chan struct{}, 4)
	f.app.hls, f.handler = runtime, f.app.Handler()
	collection, err := f.app.library.CreateLibrary(f.ctx, "Dolby Vision planning", "movies", []string{root})
	if err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	item := fastIngestItem(t, f, accounts.viewer.userID, collection.ID, path)
	if item.Media == nil || !reflect.DeepEqual(media.PendingDolbyVisionStreams(*item.Media), []int{2}) {
		t.Fatal("fixture is not an eligible deferred Dolby Vision source")
	}
	stamp := fastIngestSourceStamp(t, f, item.ID)
	headers := accounts.viewer.headers
	playbackPath, videoPath := "/emby/Items/"+item.ID+"/PlaybackInfo", "/emby/Videos/"+item.ID
	base, _ := playbackHTTPSource(t, f.request(t, http.MethodGet, playbackPath, nil, headers))
	manual := videoPath + "/master.m3u8?VideoCodec=h264&PlaySessionId=" + url.QueryEscape(stringValue(t, base, "PlaySessionId"))
	progressive := videoPath + "/stream.mp4?Static=false&VideoCodec=h264"
	// Progressive admission failure retires that play's outputs. Use a
	// different client session so this does not retire the HLS under test.
	progressiveHeaders := accounts.second.headers
	deviceProfile := map[string]any{"TranscodingProfiles": []map[string]any{{
		"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac",
	}}}
	negotiation := map[string]any{"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"SubtitleStreamIndex": -1, "DeviceProfile": deviceProfile}
	original := f.request(t, http.MethodGet, videoPath+"/original.mp4", nil, headers)
	expectStatus(t, original, http.StatusOK)
	if original.Body.String() != string(input) || original.Header().Get("ETag") == "" {
		t.Fatal("pending evidence changed original-file delivery")
	}
	pending, pendingSource := playbackHTTPSource(t, f.request(t, http.MethodPost, playbackPath, negotiation, headers))
	if pending["ErrorCode"] != "NoCompatibleStream" || pendingSource["SupportsTranscoding"] != false {
		t.Fatal("PlaybackInfo advertised a conversion from unverified RPU metadata")
	}
	expectStatus(t, f.request(t, http.MethodGet, manual, nil, headers), http.StatusUnsupportedMediaType)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		expectStatus(t, f.request(t, method, progressive, nil, progressiveHeaders), http.StatusUnsupportedMediaType)
	}
	if jobs.ensureCalls != 0 {
		t.Fatal("unverified source started conversion")
	}

	// Publish a declared private result without running a verifier or touching
	// the accepted source. Media-level tests exercise the verifier itself.
	verified, err := json.Marshal(media.DolbyVisionRPUEvidence{StreamIndex: 2, Verified: true, Profile: 8, ResidualDisabled: true, FrameCount: 2880})
	if err != nil {
		t.Fatal(err)
	}
	if tag, err := f.pool.Exec(f.ctx, `UPDATE dolby_vision_verifications
		SET evidence=$3::jsonb,retry_at='infinity',verified_at=clock_timestamp()
		WHERE item_id=$1 AND stream_index=2 AND source_revision=$2 AND evidence IS NULL`, item.ID, stamp, verified); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("publish fixture RPU evidence: %v, rows=%d", err, tag.RowsAffected())
	}
	ready, readySource := playbackHTTPSource(t, f.request(t, http.MethodPost, playbackPath, negotiation, headers))
	if _, denied := ready["ErrorCode"]; denied || readySource["SupportsTranscoding"] != true ||
		!reflect.DeepEqual(pendingSource["MediaStreams"], readySource["MediaStreams"]) {
		t.Fatal("new PlaybackInfo did not advertise the verified plan or changed the original source DTO")
	}
	master, err := url.Parse(stringValue(t, readySource, "TranscodingUrl"))
	if err != nil {
		t.Fatal("invalid negotiated HLS URL")
	}
	hlsID := master.Query().Get("GobyHlsId")
	registered := runtime.sessions[hlsID]
	if hlsID == "" || registered == nil || registered.key.plan.VideoFilters.ToneMap != "dolbyvision" || registered.key.stamp != original.Header().Get("ETag") {
		t.Fatal("PlaybackInfo did not seal a strict plan against the original source proof")
	}
	sealed := registered.key.plan
	expectStatus(t, f.request(t, http.MethodGet, manual, nil, headers), http.StatusOK)
	manualScoped := false
	for _, session := range runtime.sessions {
		if session.key.scope.PlaySessionID == stringValue(t, base, "PlaySessionId") && session.key.plan.VideoFilters.ToneMap == "dolbyvision" {
			manualScoped = true
		}
	}
	if !manualScoped {
		t.Fatal("manual HLS did not seal a new verified plan")
	}
	head := f.request(t, http.MethodHead, progressive, nil, progressiveHeaders)
	expectStatus(t, head, http.StatusOK)
	if head.Body.Len() != 0 || head.Header().Get("Content-Type") != "video/mp4" || jobs.ensureCalls != 0 {
		t.Fatal("progressive HEAD started an encoder or lost its planned content type")
	}
	expectStatus(t, f.request(t, http.MethodGet, progressive, nil, progressiveHeaders), http.StatusServiceUnavailable)
	if jobs.ensureCalls != 1 {
		t.Fatal("progressive GET did not hand its plan to the controlled encoder")
	}
	spec := jobs.received
	if spec.SourceStamp != original.Header().Get("ETag") || spec.Plan.VideoCodec == "copy" || spec.Plan.VideoFilters.ToneMap != "dolbyvision" {
		t.Fatal("progressive GET did not hand off a strict encode with the original source proof")
	}

	// Losing cached evidence does not renegotiate an existing HLS registration.
	if _, err := f.pool.Exec(f.ctx, `UPDATE dolby_vision_verifications SET evidence=NULL,verified_at=NULL,retry_at='-infinity' WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, master.RequestURI(), nil, headers), http.StatusOK)
	if runtime.sessions[hlsID] != registered || registered.key.plan != sealed {
		t.Fatal("an existing HLS route changed its immutable conversion plan")
	}
	expectStatus(t, f.request(t, http.MethodHead, progressive, nil, progressiveHeaders), http.StatusUnsupportedMediaType)
	current := f.request(t, http.MethodGet, videoPath+"/original.mp4", nil, headers)
	expectStatus(t, current, http.StatusOK)
	if current.Header().Get("ETag") != original.Header().Get("ETag") || current.Body.String() != string(input) || fastIngestSourceStamp(t, f, item.ID) != stamp {
		t.Fatal("publishing or removing RPU evidence changed the source revision, original bytes or ETag")
	}
	if _, err := os.Stat(verifier + ".called"); !os.IsNotExist(err) {
		t.Fatalf("a consumer ran full RPU verification: %v", err)
	}
}

type dolbyVisionPlanningJobs struct {
	*audioRuntimeJobs
	received transcode.Spec
}

func (jobs *dolbyVisionPlanningJobs) Ensure(_ context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	if input != nil {
		_ = input.Close()
	}
	jobs.ensureCalls++
	jobs.received = spec
	// Fail before creating a producer; retiring a fake running job would
	// concurrently cancel the HTTP request and obscure what was admitted.
	return transcode.Record{}, errAudioRuntimeOpen
}
