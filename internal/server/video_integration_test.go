//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type videoHTTPNegotiation struct {
	playID string
	uri    *url.URL
	source map[string]any
}

func videoHTTPProfile(protocol string, resize, audio bool) map[string]any {
	container := "mp4"
	if protocol == "hls" {
		container = "ts"
	}
	profile := map[string]any{"Type": "Video", "Container": container, "Protocol": protocol, "Context": "Streaming", "VideoCodec": "h264"}
	if audio {
		profile["AudioCodec"] = "aac"
	}
	if resize {
		profile["MaxWidth"], profile["MaxHeight"] = 96, 54
	}
	if protocol == "hls" {
		profile["SegmentLength"] = 3
	}
	return profile
}

func videoHTTPBody(audio bool, profiles ...map[string]any) map[string]any {
	codecs := []map[string]any{{"Type": "Video", "Container": "mp4", "Codec": "h264",
		"Conditions": []map[string]any{audioPIHTTPCondition("VideoBitrate", "Equals", "300000")}}}
	if audio {
		codecs = append(codecs, map[string]any{"Type": "VideoAudio", "Container": "mp4", "Codec": "aac",
			"Conditions": []map[string]any{audioPIHTTPCondition("AudioBitrate", "Equals", "64000"),
				audioPIHTTPCondition("AudioSampleRate", "Equals", "48000")}})
	}
	return map[string]any{
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false, "MaxStreamingBitrate": 600000,
		"DeviceProfile": map[string]any{"TranscodingProfiles": profiles, "CodecProfiles": codecs},
	}
}

func videoHTTPPrepare(t *testing.T, fixture *hlsHTTPFixture, item library.Item, body map[string]any, protocol string) videoHTTPNegotiation {
	t.Helper()
	if item.Media == nil || item.Media.ProbeVersion < 5 || !item.Media.FormatStartKnown {
		t.Fatal("video HTTP fixture lacks a current verified format-clock origin")
	}
	response := fixture.request(t, http.MethodPost, "/emby/Items/"+item.ID+"/PlaybackInfo", body, fixture.accounts.viewer.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var decoded struct {
		PlayID  string           `json:"PlaySessionId"`
		Error   string           `json:"ErrorCode"`
		Sources []map[string]any `json:"MediaSources"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil || decoded.Error != "" || !strings.HasPrefix(decoded.PlayID, "play_") || len(decoded.Sources) != 1 {
		t.Fatal("Movie PlaybackInfo did not return one usable original source and an owned play")
	}
	source := decoded.Sources[0]
	videoHTTPOriginalFacts(t, item, source)
	container := "mp4"
	if protocol == "hls" {
		container = "ts"
	}
	if source["SupportsDirectPlay"] != false || source["SupportsDirectStream"] != false || source["SupportsTranscoding"] != true ||
		source["TranscodingContainer"] != container || source["TranscodingSubProtocol"] != protocol {
		t.Fatal("video profile ordering advertised an incorrect delivery mode")
	}
	raw, ok := source["TranscodingUrl"].(string)
	if !ok {
		t.Fatal("Movie PlaybackInfo omitted its delivery URL")
	}
	uri := hlsHTTPURL(t, raw, fixture.accounts.viewer.headers.Get("X-Emby-Token"))
	file := "stream.mp4"
	if protocol == "hls" {
		file = "master.m3u8"
	}
	if uri.Path != "/emby/Videos/"+item.ID+"/"+file || strings.Contains(raw, item.Path) {
		t.Fatal("video delivery URL is not a standard catalog route")
	}
	query := uri.Query()
	if query.Get("DeviceId") != fixture.accounts.viewer.deviceID || query.Get("MediaSourceId") != media.SourceID(item.ID) || query.Get("PlaySessionId") != decoded.PlayID {
		t.Fatal("video delivery URL lost its source, device, or canonical play scope")
	}
	if protocol == "http" {
		if query.Get("Static") != "false" || query.Get("GobyHlsId") != "" || query.Get("StartTimeTicks") == "" {
			t.Fatal("progressive video URL depends on a private conversion identity")
		}
		for _, private := range []string{"Path", "FormatStartTicks", "SourceFormatStartTicks", "SourceFormatStartKnown", "PresentationOriginTicks", "SourceVideoStartTicks", "SourceAudioStartTicks", "DurationTicks"} {
			if query.Has(private) {
				t.Fatal("video delivery URL exposed private source-clock facts")
			}
		}
		videoIndex, err := strconv.Atoi(query.Get("VideoStreamIndex"))
		if err != nil {
			t.Fatal("video delivery URL omitted the original video stream index")
		}
		foundVideo, foundAudio := false, false
		for _, stream := range item.Media.Streams {
			foundVideo = foundVideo || stream.CodecType == "video" && !stream.IsAttachedPicture && stream.Index == videoIndex
			if stream.CodecType == "audio" && !stream.IsExternal {
				foundAudio = true
				if query.Get("AudioStreamIndex") != strconv.Itoa(stream.Index) {
					t.Fatal("video delivery URL replaced the original audio stream index")
				}
			}
		}
		if !foundVideo || !foundAudio && (query.Has("AudioStreamIndex") || query.Has("AudioCodec")) {
			t.Fatal("video delivery URL invented or omitted an original track")
		}
	}
	play, err := fixture.f.app.library.GetPlaybackSession(fixture.f.ctx, audioHTTPOwner(fixture.accounts.viewer), decoded.PlayID)
	if err != nil || play.ItemID != item.ID || play.State != "Prepared" || play.PositionTicks != 0 || play.StartedAt != nil {
		t.Fatalf("video negotiation fabricated playback activity (%T)", err)
	}
	return videoHTTPNegotiation{playID: decoded.PlayID, uri: uri, source: source}
}

func videoHTTPOriginalFacts(t *testing.T, item library.Item, source map[string]any) {
	t.Helper()
	if source["Id"] != media.SourceID(item.ID) || source["ItemId"] != item.ID || source["Path"] != item.Path ||
		source["Container"] != media.CanonicalContainer(*item.Media, item.Path) || source["Size"] != float64(item.Media.Size) ||
		source["RunTimeTicks"] != float64(item.Media.DurationTicks) || source["Bitrate"] != float64(item.Media.Bitrate) {
		t.Fatal("Movie PlaybackInfo replaced original source facts with projected output facts")
	}
	streams, ok := source["MediaStreams"].([]any)
	if !ok || len(streams) != len(item.Media.Streams) {
		t.Fatal("Movie PlaybackInfo changed the original track list")
	}
	for _, original := range item.Media.Streams {
		matched := false
		for _, raw := range streams {
			stream, ok := raw.(map[string]any)
			if !ok || stream["Index"] != float64(original.Index) || stream["Codec"] != original.Codec {
				continue
			}
			switch original.CodecType {
			case "video":
				matched = stream["Type"] == "Video" && stream["Width"] == float64(original.Width) && stream["Height"] == float64(original.Height)
			case "audio":
				matched = stream["Type"] == "Audio" && stream["Channels"] == float64(original.Channels) && stream["SampleRate"] == float64(original.SampleRate)
			}
		}
		if !matched {
			t.Fatal("Movie PlaybackInfo substituted projected tracks for the original streams")
		}
	}
}

func videoHTTPSilentSource(t *testing.T, fixture *hlsHTTPFixture) library.Item {
	t.Helper()
	path := filepath.Join(filepath.Dir(fixture.path), "Silent.Color.Sequence.mp4")
	hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", fixture.path,
		"-map", "0:v:0", "-an", "-c:v", "copy", "-t", "6", path)
	(&streamHTTPFixture{f: fixture.f}).rescan(t, fixture.libraryID)
	result, err := fixture.f.app.library.QueryItems(fixture.f.ctx, library.Query{
		UserID: fixture.accounts.admin.userID, ParentID: fixture.libraryID, Recursive: true, Limit: 100,
	})
	if err != nil {
		t.Fatalf("query owned silent video (%T)", err)
	}
	for _, item := range result.Items {
		if item.Path == path && !item.IsFolder {
			if item.Media == nil || item.Media.DurationTicks != 6*media.TicksPerSecond || !item.Media.FormatStartKnown || len(item.Media.Streams) != 1 || item.Media.Streams[0].CodecType != "video" {
				t.Fatal("owned silent video does not have verified six-second video-only facts")
			}
			return item
		}
	}
	t.Fatal("owned silent video was not indexed")
	return library.Item{}
}

func videoHTTPHistory(t *testing.T, fixture *hlsHTTPFixture, items ...library.Item) string {
	t.Helper()
	values := make(map[string]library.UserData)
	for _, item := range items {
		data, err := fixture.f.app.library.GetUserData(fixture.f.ctx, fixture.accounts.viewer.userID, item.ID)
		if err != nil {
			t.Fatalf("read owned video history (%T)", err)
		}
		values[item.ID] = data
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal("encode owned video history")
	}
	return string(encoded)
}

func videoHTTPJobCount(t *testing.T, fixture *hlsHTTPFixture, playID string, active bool) int {
	t.Helper()
	var count int
	if err := fixture.f.pool.QueryRow(fixture.f.ctx, `SELECT count(*) FROM encoding_jobs
		WHERE auth_session_id = $1 AND play_session_id = $2 AND (NOT $3::boolean OR state IN ('queued','running'))`,
		fixture.accounts.viewer.id, playID, active).Scan(&count); err != nil {
		t.Fatal("read scoped video encoding count")
	}
	return count
}

type videoHTTPOutput struct {
	width, height, frames int
	audio                 bool
	seconds               float64
	color                 [3]int
}

func videoHTTPVerifyMP4(t *testing.T, fixture *hlsHTTPFixture, data []byte, want videoHTTPOutput) {
	t.Helper()
	if len(data) == 0 || len(data) > 16<<20 || !bytes.Contains(data, []byte("ftyp")) || !bytes.Contains(data, []byte("moof")) {
		t.Fatal("progressive video is not a bounded fragmented MP4 representation")
	}
	path := filepath.Join(t.TempDir(), "received.mp4")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal("write owned received MP4")
	}
	var probe struct {
		Format struct {
			Name     string `json:"format_name"`
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Codec  string `json:"codec_name"`
			Type   string `json:"codec_type"`
			Frames string `json:"nb_read_frames"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
	}
	facts := hlsHTTPMediaCommand(t, fixture.ffprobe, "-v", "error", "-count_frames", "-show_entries",
		"format=format_name,duration:stream=codec_name,codec_type,nb_read_frames,width,height", "-of", "json", path)
	if err := json.Unmarshal(facts, &probe); err != nil || !strings.Contains(probe.Format.Name, "mp4") {
		t.Fatal("received video cannot be probed as MP4")
	}
	videoCount, audioCount := 0, 0
	for _, stream := range probe.Streams {
		switch stream.Type {
		case "video":
			videoCount++
			frames, err := strconv.Atoi(stream.Frames)
			if err != nil || frames != want.frames || stream.Codec != "h264" || stream.Width != want.width || stream.Height != want.height {
				t.Fatalf("received MP4 video does not retain its expected geometry and frame count: %d frames, want %d", frames, want.frames)
			}
		case "audio":
			audioCount++
			if stream.Codec != "aac" {
				t.Fatal("received MP4 audio is not AAC")
			}
		default:
			t.Fatal("received MP4 invented an unrequested stream")
		}
	}
	wantAudio := 0
	if want.audio {
		wantAudio = 1
	}
	seconds, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if videoCount != 1 || audioCount != wantAudio || err != nil || math.Abs(seconds-want.seconds) > .1 {
		t.Fatal("received MP4 changed its track presence or complete source window")
	}
	hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1",
		"-i", path, "-map", "0:v:0", "-map", "0:a?", "-threads", "1", "-f", "null", "-")
	pixel := hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1", "-filter_threads", "1",
		"-i", path, "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=1:1", "-pix_fmt", "rgb24", "-threads", "1", "-f", "rawvideo", "-")
	if len(pixel) != 3 {
		t.Fatal("received MP4 did not decode its initial source-position marker")
	}
	for channel, expected := range want.color {
		if math.Abs(float64(int(pixel[channel])-expected)) > 20 {
			t.Fatal("received MP4 starts in the wrong source color interval")
		}
	}
}

func videoHTTPSessions(fixture *hlsHTTPFixture, playID string) []*hlsSession {
	fixture.f.app.hls.mu.Lock()
	defer fixture.f.app.hls.mu.Unlock()
	var sessions []*hlsSession
	for _, session := range fixture.f.app.hls.sessions {
		if session.key.scope.AuthSessionID == fixture.accounts.viewer.id && session.key.scope.PlaySessionID == playID {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

func videoHTTPRecord(t *testing.T, fixture *hlsHTTPFixture, playID, codec string, start int64) (*hlsSession, transcode.Record) {
	t.Helper()
	for _, session := range videoHTTPSessions(fixture, playID) {
		if session.key.plan.OutputMode != "progressive" || session.key.plan.VideoCodec != codec || session.key.plan.StartTicks != start {
			continue
		}
		session.mu.Lock()
		producers := append([]hlsProducer(nil), session.producers...)
		session.mu.Unlock()
		if len(producers) != 1 {
			t.Fatal("progressive video revision does not own exactly one producer")
		}
		record, err := fixture.f.app.hls.manager.Snapshot(session.key.scope, producers[0].id)
		if err != nil {
			t.Fatalf("inspect scoped video producer (%T)", err)
		}
		return session, record
	}
	t.Fatal("scoped progressive video revision was not found")
	return nil, transcode.Record{}
}

func videoHTTPPolicy(t *testing.T, fixture *hlsHTTPFixture, videoEncoding bool) {
	t.Helper()
	policy, err := json.Marshal(map[string]any{
		"EnableAllFolders": false, "EnabledFolders": []string{fixture.libraryID}, "EnableMediaPlayback": true,
		"EnablePlaybackRemuxing": true, "EnableAudioPlaybackTranscoding": true, "EnableVideoPlaybackTranscoding": videoEncoding,
	})
	if err != nil {
		t.Fatal("encode owned video conversion policy")
	}
	if _, err := fixture.f.pool.Exec(fixture.f.ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", fixture.accounts.viewer.userID, policy); err != nil {
		t.Fatal("update owned video conversion policy")
	}
}

func videoHTTPStopPath(login clientSessionHTTPLogin, playID string) string {
	return "/emby/Videos/ActiveEncodings?" + url.Values{"DeviceId": {login.deviceID}, "PlaySessionId": {playID}}.Encode()
}

func videoHTTPWaitRetired(t *testing.T, fixture *hlsHTTPFixture, playID string, sessions []*hlsSession) {
	t.Helper()
	audioHTTPWait(t, "stopped video retained a runtime revision, reader, cache pin, or active job", func() bool {
		if len(videoHTTPSessions(fixture, playID)) != 0 || videoHTTPJobCount(t, fixture, playID, true) != 0 {
			return false
		}
		for _, session := range sessions {
			session.mu.Lock()
			readers, closed := session.progressiveReaders, session.closed
			producers := append([]hlsProducer(nil), session.producers...)
			session.mu.Unlock()
			if readers != 0 || !closed {
				return false
			}
			for _, producer := range producers {
				if _, err := os.Stat(filepath.Join(fixture.f.app.cfg.Transcoding.CacheDirectory, producer.id)); !errors.Is(err, os.ErrNotExist) {
					return false
				}
			}
		}
		return len(fixture.f.app.streamSlots) == 0
	})
}

func TestHTTPVideoProgressiveNegotiationSeekMixedCopyAndSilentSource(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	silent := videoHTTPSilentSource(t, fixture)
	history := videoHTTPHistory(t, fixture, fixture.item, silent)
	httpProfile := videoHTTPProfile("http", true, true)
	hlsProfile := videoHTTPProfile("hls", true, true)
	body := videoHTTPBody(true, httpProfile, hlsProfile)
	prepared := videoHTTPPrepare(t, fixture, fixture.item, body, "http")
	query := prepared.uri.Query()
	for key, expected := range map[string]string{"VideoCodec": "h264", "AudioCodec": "aac", "Width": "96", "Height": "54", "VideoBitrate": "300000", "AudioBitrate": "64000", "AllowVideoStreamCopy": "false", "AllowAudioStreamCopy": "false"} {
		if query.Get(key) != expected {
			t.Fatalf("video URL did not preserve its exact %s encoding choice", key)
		}
	}
	if videoHTTPJobCount(t, fixture, prepared.playID, false) != 0 {
		t.Fatal("Movie PlaybackInfo started an encoder")
	}
	head := fixture.request(t, http.MethodHead, prepared.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, head.header, "video/mp4")
	if len(head.body) != 0 || videoHTTPJobCount(t, fixture, prepared.playID, false) != 0 {
		t.Fatal("video HEAD started production or supplied a media body")
	}
	full := fixture.request(t, http.MethodGet, prepared.uri.String(), nil, http.Header{"Range": {"bytes=0-31"}})
	expectHLSHTTPStatus(t, full, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, full.header, "video/mp4")
	videoHTTPVerifyMP4(t, fixture, full.body, videoHTTPOutput{width: 96, height: 54, frames: 288, audio: true, seconds: 12, color: [3]int{255, 0, 0}})
	seekURL := *prepared.uri
	seekQuery := seekURL.Query()
	seekQuery.Set("StartTimeTicks", "42500000")
	seekURL.RawQuery = seekQuery.Encode()
	// The suffix-free route rebuilds the same explicit MP4 encoding request.
	seekURL.Path = strings.TrimSuffix(seekURL.Path, ".mp4")
	seek := fixture.request(t, http.MethodGet, seekURL.String(), nil, nil)
	expectHLSHTTPStatus(t, seek, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, seek.header, "video/mp4")
	videoHTTPVerifyMP4(t, fixture, seek.body, videoHTTPOutput{width: 96, height: 54, frames: 186, audio: true, seconds: 7.75, color: [3]int{0, 128, 0}})
	_, seekRecord := videoHTTPRecord(t, fixture, prepared.playID, "h264", 42_500_000)
	if !seekRecord.Spec.Plan.SourceFormatStartKnown || seekRecord.Spec.Plan.StartTicks != 42_500_000 || seekRecord.State != "completed" {
		t.Fatal("modified video URL did not preserve its verified source clock and new output window")
	}

	mixedBody := videoHTTPBody(true, videoHTTPProfile("http", false, true))
	mixedBody["AllowVideoStreamCopy"] = true
	mixedBody["DeviceProfile"].(map[string]any)["CodecProfiles"] = []map[string]any{{
		"Type": "VideoAudio", "Container": "mp4", "Codec": "aac",
		"Conditions": []map[string]any{audioPIHTTPCondition("AudioBitrate", "Equals", "64000")},
	}}
	mixed := videoHTTPPrepare(t, fixture, fixture.item, mixedBody, "http")
	if mixed.uri.Query().Get("VideoCodec") != "copy" || mixed.uri.Query().Get("AudioCodec") != "aac" || mixed.uri.Query().Get("StartTimeTicks") != "0" {
		t.Fatal("mixed video negotiation did not preserve video packets and encode audio")
	}
	mixedOutput := fixture.request(t, http.MethodGet, mixed.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, mixedOutput, http.StatusOK)
	videoHTTPVerifyMP4(t, fixture, mixedOutput.body, videoHTTPOutput{width: 160, height: 90, frames: 288, audio: true, seconds: 12, color: [3]int{255, 0, 0}})
	mixedSession, mixedRecord := videoHTTPRecord(t, fixture, mixed.playID, "copy", 0)
	if mixedRecord.Spec.Plan.AudioCodec != "aac" || mixedRecord.State != "completed" {
		t.Fatal("mixed HTTP output did not execute its advertised per-track operations")
	}
	copySeek := *mixed.uri
	copyQuery := copySeek.Query()
	copyQuery.Set("StartTimeTicks", "42500000")
	copySeek.RawQuery = copyQuery.Encode()
	jobsBefore := videoHTTPJobCount(t, fixture, mixed.playID, false)
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		expectHLSHTTPStatus(t, fixture.request(t, method, copySeek.String(), nil, nil), http.StatusUnsupportedMediaType)
	}
	if videoHTTPJobCount(t, fixture, mixed.playID, false) != jobsBefore {
		t.Fatal("unsupported copied-video seeking started a producer")
	}

	silentBody := videoHTTPBody(false, videoHTTPProfile("http", true, false))
	silentPlan := videoHTTPPrepare(t, fixture, silent, silentBody, "http")
	silentOutput := fixture.request(t, http.MethodGet, silentPlan.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, silentOutput, http.StatusOK)
	videoHTTPVerifyMP4(t, fixture, silentOutput.body, videoHTTPOutput{width: 96, height: 54, frames: 144, seconds: 6, color: [3]int{255, 0, 0}})
	_, silentRecord := videoHTTPRecord(t, fixture, silentPlan.playID, "h264", 0)
	if silentRecord.Spec.Plan.AudioStreamIndex != -1 {
		t.Fatal("silent video output invented an input audio track")
	}

	ordered := videoHTTPPrepare(t, fixture, fixture.item, videoHTTPBody(true, hlsProfile, httpProfile), "hls")
	master := fixture.request(t, http.MethodGet, ordered.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	mains := hlsHTTPManifestChildren(master.body)
	if len(mains) != 1 {
		t.Fatal("first compatible HLS profile did not retain its master graph")
	}
	hlsHTTPURL(t, mains[0], fixture.accounts.viewer.headers.Get("X-Emby-Token"))
	main := fixture.request(t, http.MethodGet, mains[0], nil, nil)
	expectHLSHTTPStatus(t, main, http.StatusOK)
	children := hlsHTTPManifestChildren(main.body)
	if len(children) != 4 || !bytes.HasSuffix(main.body, []byte("#EXT-X-ENDLIST\n")) {
		t.Fatal("HLS profile ordering changed the complete source timeline")
	}
	firstSegment := fixture.request(t, http.MethodGet, children[0], nil, nil)
	expectHLSHTTPStatus(t, firstSegment, http.StatusOK)
	fixture.verifySegment(t, firstSegment.body, 0, 3)

	videoHTTPPolicy(t, fixture, false)
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		expectHLSHTTPStatus(t, fixture.request(t, method, prepared.uri.String(), nil, nil), http.StatusUnsupportedMediaType)
	}
	stillAllowed := fixture.request(t, http.MethodGet, mixed.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, stillAllowed, http.StatusOK)
	if !bytes.Equal(stillAllowed.body, mixedOutput.body) {
		t.Fatal("revoking video encoding changed an authorized mixed-copy cached output")
	}
	videoHTTPPolicy(t, fixture, true)
	foreign := *mixed.uri
	foreignQuery := foreign.Query()
	foreignQuery.Del("api_key")
	foreignQuery.Set("DeviceId", fixture.accounts.second.deviceID)
	foreign.RawQuery = foreignQuery.Encode()
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodGet, foreign.String(), nil, fixture.accounts.second.headers), http.StatusNotFound)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, videoHTTPStopPath(fixture.accounts.second, mixed.playID), nil, fixture.accounts.second.headers), http.StatusNoContent)
	state, err := fixture.f.app.hls.manager.Snapshot(mixedSession.key.scope, mixedRecord.ID)
	if err != nil || state.State != "completed" {
		t.Fatalf("foreign stop invalidated an owned video producer (%T)", err)
	}
	sessions := videoHTTPSessions(fixture, mixed.playID)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, videoHTTPStopPath(fixture.accounts.viewer, mixed.playID), nil, fixture.accounts.viewer.headers), http.StatusNoContent)
	videoHTTPWaitRetired(t, fixture, mixed.playID, sessions)
	if videoHTTPHistory(t, fixture, fixture.item, silent) != history {
		t.Fatal("video negotiation, media GETs, seeking, or encoder cleanup fabricated user data")
	}
}

func videoHTTPSlowRuntime(t *testing.T, fixture *hlsHTTPFixture) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(fixture.f.ctx, 10*time.Second)
	if err := fixture.f.app.hls.Close(ctx); err != nil {
		cancel()
		t.Fatalf("close unused video fixture runtime (%T)", err)
	}
	cancel()
	owned := t.TempDir()
	pidFile := filepath.Join(owned, "video-encoder-pid")
	wrapper := filepath.Join(owned, "ffmpeg-video-wrapper")
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" >> " + audioHTTPShellQuote(pidFile) +
		"\nexec " + audioHTTPShellQuote(fixture.ffmpeg) + " -readrate 0.5 \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal("write owned slow video encoder wrapper")
	}
	fixture.f.app.cfg.FFmpegPath = wrapper
	fixture.f.cfg = fixture.f.app.cfg
	runtime, err := newHLSRuntime(fixture.f.ctx, fixture.f.app)
	if err != nil {
		t.Fatalf("create controlled video runtime (%T)", err)
	}
	fixture.f.app.hls = runtime
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Errorf("close controlled video runtime (%T)", err)
		}
	})
	return pidFile
}

func TestHTTPVideoStopAfterMP4HeadersAbortsAndReleasesProducer(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	pidFile := videoHTTPSlowRuntime(t, fixture)
	history := videoHTTPHistory(t, fixture, fixture.item)
	prepared := videoHTTPPrepare(t, fixture, fixture.item, videoHTTPBody(true, videoHTTPProfile("http", true, true)), "http")
	ctx, cancel := context.WithTimeout(fixture.f.ctx, 45*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.server.URL+prepared.uri.String(), nil)
	if err != nil {
		t.Fatal("construct owned live video request")
	}
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatalf("open owned live video output (%T)", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("live video status = %d", response.StatusCode)
	}
	assertAudioHTTPProgressiveHeaders(t, response.Header, "video/mp4")
	prefix := make([]byte, 256)
	if _, err := io.ReadFull(response.Body, prefix); err != nil || !bytes.Contains(prefix, []byte("ftyp")) {
		t.Fatalf("live video did not begin a real MP4 representation (%T)", err)
	}
	session, record := videoHTTPRecord(t, fixture, prepared.playID, "h264", 0)
	session.mu.Lock()
	readers := session.progressiveReaders
	session.mu.Unlock()
	if record.State != "running" || readers != 1 || videoHTTPJobCount(t, fixture, prepared.playID, true) != 1 {
		t.Fatal("video cancellation window did not contain a running producer and reader")
	}
	encodedPIDs, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal("read owned video encoder process record")
	}
	values := strings.Fields(string(encodedPIDs))
	if len(values) != 1 {
		t.Fatal("live video launched more than one real producer")
	}
	pid, err := strconv.Atoi(values[0])
	if err != nil || pid <= 0 {
		t.Fatal("invalid owned video encoder process record")
	}
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, videoHTTPStopPath(fixture.accounts.viewer, prepared.playID), nil, fixture.accounts.viewer.headers), http.StatusNoContent)
	_, err = io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("video stop presented truncated MP4 as a normal HTTP completion (%T)", err)
	}
	_ = response.Body.Close()
	cancel()
	videoHTTPWaitRetired(t, fixture, prepared.playID, []*hlsSession{session})
	audioHTTPWait(t, "stopped real video encoder process was not reaped", func() bool {
		_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))
		return errors.Is(err, os.ErrNotExist)
	})
	if videoHTTPHistory(t, fixture, fixture.item) != history {
		t.Fatal("stopping a live MP4 response fabricated playback history")
	}
}
