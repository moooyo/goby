//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

type audioPIHTTPResult struct {
	PlayID string
	Error  string
	Source map[string]any
}

func audioPIHTTPProfile(container, codec, protocol string) map[string]any {
	profile := map[string]any{"Type": "Audio", "Container": container, "AudioCodec": codec, "Protocol": protocol, "Context": "Streaming"}
	if protocol == "hls" {
		profile["SegmentLength"] = 3
	}
	return profile
}

func audioPIHTTPBody(profiles ...map[string]any) map[string]any {
	return map[string]any{
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowAudioStreamCopy": false,
		"DeviceProfile":        map[string]any{"TranscodingProfiles": profiles},
	}
}

func audioPIHTTPCondition(property, operator, value string) map[string]any {
	return map[string]any{"Property": property, "Condition": operator, "Value": value, "IsRequired": true}
}

func audioPIHTTPConditions(body map[string]any, container, codec string, conditions ...map[string]any) {
	body["DeviceProfile"].(map[string]any)["CodecProfiles"] = []map[string]any{{
		"Type": "Audio", "Container": container, "Codec": codec, "Conditions": conditions,
	}}
}

func audioPIHTTPRequest(t *testing.T, a *audioHTTPFixture, key string, body map[string]any) audioPIHTTPResult {
	t.Helper()
	response := a.request(t, http.MethodPost, "/emby/Items/"+a.sources[key].ID+"/PlaybackInfo", body, a.accounts.viewer.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var result struct {
		PlayID  string           `json:"PlaySessionId"`
		Error   string           `json:"ErrorCode"`
		Sources []map[string]any `json:"MediaSources"`
	}
	if err := json.Unmarshal(response.body, &result); err != nil || len(result.Sources) != 1 || !strings.HasPrefix(result.PlayID, "play_") {
		t.Fatal("audio PlaybackInfo did not return one source and a canonical play identity")
	}
	play, err := a.f.app.library.GetPlaybackSession(a.f.ctx, audioHTTPOwner(a.accounts.viewer), result.PlayID)
	if err != nil || play.ItemID != a.sources[key].ID || play.AuthSessionID != a.accounts.viewer.id ||
		play.State != "Prepared" || play.PositionTicks != 0 || play.StartedAt != nil {
		t.Fatalf("audio PlaybackInfo did not preserve its owned preparation (%T)", err)
	}
	audioPIHTTPOriginalFacts(t, a, key, result.Sources[0])
	return audioPIHTTPResult{PlayID: result.PlayID, Error: result.Error, Source: result.Sources[0]}
}

// The source DTO remains the authoritative original. Output choices belong in
// the playback URL, including when its requested window is shorter than source.
func audioPIHTTPOriginalFacts(t *testing.T, a *audioHTTPFixture, key string, source map[string]any) {
	t.Helper()
	item := a.sources[key]
	if source["Id"] != media.SourceID(item.ID) || source["ItemId"] != item.ID || source["Path"] != item.Path ||
		source["Container"] != media.CanonicalContainer(*item.Media, item.Path) || source["Size"] != float64(item.Media.Size) ||
		source["RunTimeTicks"] != float64(item.Media.DurationTicks) || source["Bitrate"] != float64(item.Media.Bitrate) {
		t.Fatal("audio PlaybackInfo replaced original source facts with projected output metadata")
	}
	streams, ok := source["MediaStreams"].([]any)
	if !ok || len(streams) != len(item.Media.Streams) {
		t.Fatal("audio PlaybackInfo changed the original stream list")
	}
	for _, original := range item.Media.Streams {
		if original.CodecType != "audio" {
			continue
		}
		matched := false
		for _, raw := range streams {
			stream, ok := raw.(map[string]any)
			if !ok || stream["Index"] != float64(original.Index) {
				continue
			}
			matched = stream["Type"] == "Audio" && stream["Codec"] == original.Codec &&
				stream["Channels"] == float64(original.Channels) && stream["SampleRate"] == float64(original.SampleRate)
			if original.BitDepth > 0 {
				matched = matched && stream["BitDepth"] == float64(original.BitDepth)
			}
		}
		if !matched {
			t.Fatal("audio PlaybackInfo substituted an output stream for an original audio track")
		}
	}
}

func audioPIHTTPURL(t *testing.T, a *audioHTTPFixture, key string, result audioPIHTTPResult, field, container string) *url.URL {
	t.Helper()
	raw, ok := result.Source[field].(string)
	if !ok || raw == "" || result.Error != "" {
		t.Fatal("audio PlaybackInfo did not advertise the requested usable delivery URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.Path != "/emby/Audio/"+a.sources[key].ID+"/stream."+container || strings.Contains(raw, a.sources[key].Path) {
		t.Fatal("progressive PlaybackInfo URL is not a standard relative audio route")
	}
	values := parsed.Query()
	for name, want := range map[string]string{
		"MediaSourceId": media.SourceID(a.sources[key].ID), "DeviceId": a.accounts.viewer.deviceID,
		"PlaySessionId": result.PlayID, "api_key": a.accounts.viewer.headers.Get("X-Emby-Token"), "Static": "false",
	} {
		if values.Get(name) != want {
			t.Fatalf("progressive PlaybackInfo URL omitted its %s binding", name)
		}
	}
	if values.Get("GobyHlsId") != "" || values.Get("Path") != "" || values.Get("SourcePath") != "" || values.Get("DurationTicks") != "" ||
		values.Get("StartTimeTicks") == "" || values.Get("AudioCodec") == "" {
		t.Fatal("progressive URL retained a private conversion identity or trusted source facts")
	}
	index, err := strconv.Atoi(values.Get("AudioStreamIndex"))
	if err != nil {
		t.Fatal("progressive URL omitted the source audio stream index")
	}
	for _, stream := range a.sources[key].Media.Streams {
		if stream.Index == index && stream.CodecType == "audio" {
			return parsed
		}
	}
	t.Fatal("progressive URL did not retain a source audio track index")
	return nil
}

type audioPIHTTPOutput struct {
	codec, container      string
	channels, rate, depth int
	bitrate               int64
	seconds               float64
}

func audioPIHTTPVerifyOutput(t *testing.T, a *audioHTTPFixture, data []byte, want audioPIHTTPOutput) {
	t.Helper()
	path := a.receivedFile(t, data, want.container)
	var facts struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Type     string `json:"codec_type"`
			Rate     string `json:"sample_rate"`
			Bitrate  string `json:"bit_rate"`
			Depth    int    `json:"bits_per_sample"`
			RawDepth string `json:"bits_per_raw_sample"`
			Channels int    `json:"channels"`
		} `json:"streams"`
	}
	encoded := hlsHTTPMediaCommand(t, a.ffprobe, "-v", "error", "-show_entries",
		"stream=codec_name,codec_type,sample_rate,bit_rate,channels,bits_per_sample,bits_per_raw_sample", "-of", "json", path)
	if err := json.Unmarshal(encoded, &facts); err != nil || len(facts.Streams) != 1 {
		t.Fatal("negotiated audio output did not contain one real audio stream")
	}
	stream := facts.Streams[0]
	rate, rateErr := strconv.Atoi(stream.Rate)
	if stream.Codec != want.codec || stream.Type != "audio" || stream.Channels != want.channels || rateErr != nil || rate != want.rate {
		t.Fatal("decoded output did not honor the negotiated codec, channels, and sample rate")
	}
	if want.bitrate > 0 {
		bitrate, err := strconv.ParseInt(stream.Bitrate, 10, 64)
		if err != nil || bitrate != want.bitrate {
			t.Fatal("encoded audio did not honor its exact bitrate target")
		}
	}
	if want.depth > 0 {
		depth := stream.Depth
		if rawDepth, err := strconv.Atoi(stream.RawDepth); err == nil && rawDepth > 0 {
			depth = rawDepth
		}
		if depth != want.depth {
			t.Fatal("encoded audio did not honor its exact integer bit depth")
		}
	}
	pcm := a.decodeFile(t, path)
	actual := float64(len(pcm)) / (48000 * 4)
	tolerance := .08
	if want.codec == "flac" || want.codec == "pcm_s16le" {
		tolerance = 1.0 / 48000
	}
	if math.Abs(actual-want.seconds) > tolerance {
		t.Fatalf("negotiated audio decoded %.6f seconds, want %.6f", actual, want.seconds)
	}
}

func TestHTTPAudioPlaybackInfoProgressiveProfilesExecuteExactOutputsAndSeek(t *testing.T) {
	a := newAudioHTTPFixture(t, false)
	history := a.history(t)
	body := audioPIHTTPBody(audioPIHTTPProfile("mp3", "mp3", "http"))
	body["StartTimeTicks"], body["MaxStreamingBitrate"], body["MaxAudioChannels"] = 2*media.TicksPerSecond, 128000, 1
	profile := body["DeviceProfile"].(map[string]any)
	profile["MaxStreamingBitrate"], profile["MusicStreamingTranscodingBitrate"] = 112000, 96000
	audioPIHTTPConditions(body, "mp3", "mp3",
		audioPIHTTPCondition("AudioChannels", "Equals", "1"),
		audioPIHTTPCondition("AudioSampleRate", "Equals", "24000"),
		audioPIHTTPCondition("AudioBitrate", "Equals", "96000"))
	result := audioPIHTTPRequest(t, a, "flac", body)
	if result.Source["SupportsTranscoding"] != true || result.Source["SupportsDirectPlay"] != false || result.Source["SupportsDirectStream"] != false ||
		result.Source["TranscodingContainer"] != "mp3" || result.Source["TranscodingSubProtocol"] != "http" {
		t.Fatal("HTTP audio profile did not advertise the correct delivery mode")
	}
	streamURL := audioPIHTTPURL(t, a, "flac", result, "TranscodingUrl", "mp3")
	for name, want := range map[string]string{"StartTimeTicks": "20000000", "AudioCodec": "mp3", "AudioChannels": "1", "AudioSampleRate": "24000", "AudioBitrate": "96000", "AllowAudioStreamCopy": "false"} {
		if streamURL.Query().Get(name) != want {
			t.Fatalf("progressive URL did not serialize its concrete %s target", name)
		}
	}
	if a.jobs(t, a.accounts.viewer, result.PlayID, false) != 0 || len(a.encoderPIDs(t)) != 0 {
		t.Fatal("PlaybackInfo started an audio encoder")
	}
	head := a.request(t, http.MethodHead, streamURL.String(), nil, nil)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, head.header, "audio/mpeg")
	if len(head.body) != 0 || a.jobs(t, a.accounts.viewer, result.PlayID, false) != 0 || len(a.encoderPIDs(t)) != 0 {
		t.Fatal("negotiated progressive HEAD started a job or invented a media body")
	}
	output := a.request(t, http.MethodGet, streamURL.String(), nil, nil)
	expectHLSHTTPStatus(t, output, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, output.header, "audio/mpeg")
	audioPIHTTPVerifyOutput(t, a, output.body, audioPIHTTPOutput{codec: "mp3", container: "mp3", channels: 1, rate: 24000, bitrate: 96000, seconds: 4})
	// A normal client edits only the standard start hint. There is no private
	// conversion revision to discard or projected source duration to restore.
	query := streamURL.Query()
	query.Set("StartTimeTicks", "0")
	streamURL.RawQuery = query.Encode()
	full := a.request(t, http.MethodGet, streamURL.String(), nil, nil)
	expectHLSHTTPStatus(t, full, http.StatusOK)
	audioPIHTTPVerifyOutput(t, a, full.body, audioPIHTTPOutput{codec: "mp3", container: "mp3", channels: 1, rate: 24000, bitrate: 96000, seconds: 6})
	if a.jobs(t, a.accounts.viewer, result.PlayID, false) != 2 {
		t.Fatal("editing the standard start hint did not construct a separate output window")
	}

	for _, sample := range []struct {
		name    string
		body    map[string]any
		want    audioPIHTTPOutput
		mime    string
		bounded bool
	}{
		{"bounded MP3", audioPIHTTPBoundedMP3(), audioPIHTTPOutput{codec: "mp3", container: "mp3", channels: 1, rate: 32000, bitrate: 64000, seconds: 6}, "audio/mpeg", true},
		{"exact PCM", audioPIHTTPExactPCM(), audioPIHTTPOutput{codec: "pcm_s16le", container: "wav", channels: 1, rate: 22050, bitrate: 352800, depth: 16, seconds: 6}, "audio/wav", false},
		{"24-bit FLAC", audioPIHTTPExactFLAC(), audioPIHTTPOutput{codec: "flac", container: "flac", channels: 1, rate: 32000, depth: 24, seconds: 6}, "audio/flac", false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			result := audioPIHTTPRequest(t, a, "flac", sample.body)
			uri := audioPIHTTPURL(t, a, "flac", result, "TranscodingUrl", sample.want.container)
			values := uri.Query()
			want := sample.want
			if sample.bounded {
				channels, channelsErr := strconv.Atoi(values.Get("AudioChannels"))
				rate, rateErr := strconv.Atoi(values.Get("AudioSampleRate"))
				bitrate, bitrateErr := strconv.ParseInt(values.Get("AudioBitrate"), 10, 64)
				if channelsErr != nil || rateErr != nil || bitrateErr != nil || channels < 1 || channels > want.channels ||
					rate < 1 || rate > want.rate || bitrate < 1 || bitrate > want.bitrate {
					t.Fatal("negotiated output exceeded an intersected channel, rate, or bitrate ceiling")
				}
				want.channels, want.rate, want.bitrate = channels, rate, bitrate
			}
			if values.Get("AudioChannels") != strconv.Itoa(want.channels) || values.Get("AudioSampleRate") != strconv.Itoa(want.rate) || values.Get("AudioCodec") != want.codec {
				t.Fatal("profile constraints did not survive in the generated URL")
			}
			if sample.want.codec == "flac" && values.Get("AudioBitDepth") != "24" {
				t.Fatal("FLAC URL omitted its exact 24-bit encoding request")
			}
			response := a.request(t, http.MethodGet, uri.String(), nil, nil)
			expectHLSHTTPStatus(t, response, http.StatusOK)
			assertAudioHTTPProgressiveHeaders(t, response.header, sample.mime)
			audioPIHTTPVerifyOutput(t, a, response.body, want)
		})
	}
	for _, omitted := range []bool{true, false} {
		profile := audioPIHTTPProfile("mp3", "mp3", "http")
		if omitted {
			delete(profile, "Protocol")
			delete(profile, "Context")
		} else {
			profile["Protocol"], profile["Context"] = "", ""
		}
		request := audioPIHTTPBody(profile)
		request["MaxStreamingBitrate"] = 128000
		result := audioPIHTTPRequest(t, a, "flac", request)
		uri := audioPIHTTPURL(t, a, "flac", result, "TranscodingUrl", "mp3")
		if result.Source["TranscodingSubProtocol"] != "http" {
			t.Fatal("empty audio protocol or context did not retain HTTP streaming semantics")
		}
		started := len(a.encoderPIDs(t))
		jobs := a.jobs(t, a.accounts.viewer, result.PlayID, false)
		head := a.request(t, http.MethodHead, uri.String(), nil, nil)
		expectHLSHTTPStatus(t, head, http.StatusOK)
		assertAudioHTTPProgressiveHeaders(t, head.header, "audio/mpeg")
		if len(a.encoderPIDs(t)) != started || a.jobs(t, a.accounts.viewer, result.PlayID, false) != jobs {
			t.Fatal("default-protocol audio HEAD started an encoder")
		}
	}
	conflict := audioPIHTTPBody(audioPIHTTPProfile("mp3", "mp3", "http"))
	conflict["MaxStreamingBitrate"] = 64000
	audioPIHTTPConditions(conflict, "mp3", "mp3", audioPIHTTPCondition("AudioBitrate", "Equals", "128000"))
	audioPIHTTPUnavailable(t, audioPIHTTPRequest(t, a, "flac", conflict), true)
	conflict = audioPIHTTPBody(audioPIHTTPProfile("mp3", "mp3", "http"))
	conflict["MaxAudioChannels"] = 1
	audioPIHTTPConditions(conflict, "mp3", "mp3", audioPIHTTPCondition("AudioChannels", "Equals", "2"))
	audioPIHTTPUnavailable(t, audioPIHTTPRequest(t, a, "flac", conflict), true)

	httpFirst := audioPIHTTPBody(audioPIHTTPProfile("mp3", "mp3", "http"), audioPIHTTPProfile("ts", "aac", "hls"))
	httpFirst["MaxStreamingBitrate"] = 128000
	ordered := audioPIHTTPRequest(t, a, "flac", httpFirst)
	_ = audioPIHTTPURL(t, a, "flac", ordered, "TranscodingUrl", "mp3")
	if ordered.Source["TranscodingSubProtocol"] != "http" {
		t.Fatal("profile ordering skipped the first compatible HTTP profile")
	}
	hlsFirst := audioPIHTTPBody(audioPIHTTPProfile("ts", "aac", "hls"), audioPIHTTPProfile("mp3", "mp3", "http"))
	hlsFirst["MaxStreamingBitrate"] = 128000
	started := len(a.encoderPIDs(t))
	ordered = audioPIHTTPRequest(t, a, "flac", hlsFirst)
	if len(a.encoderPIDs(t)) != started {
		t.Fatal("audio HLS PlaybackInfo started an encoder")
	}
	audioPIHTTPVerifyHLS(t, a, ordered)
	if a.history(t) != history {
		t.Fatal("audio negotiation, HEAD, GET, or URL seeking changed user data")
	}
}

func audioPIHTTPBoundedMP3() map[string]any {
	body := audioPIHTTPBody(audioPIHTTPProfile("mp3", "mp3", "http"))
	body["MaxStreamingBitrate"], body["MaxAudioChannels"] = 96000, 1
	profile := body["DeviceProfile"].(map[string]any)
	profile["MaxStreamingBitrate"], profile["MusicStreamingTranscodingBitrate"] = 80000, 64000
	audioPIHTTPConditions(body, "mp3", "mp3", audioPIHTTPCondition("AudioSampleRate", "LessThanEqual", "32000"),
		audioPIHTTPCondition("AudioBitrate", "LessThanEqual", "64000"))
	return body
}

func audioPIHTTPExactPCM() map[string]any {
	body := audioPIHTTPBody(audioPIHTTPProfile("wav", "pcm_s16le", "http"))
	body["MaxStreamingBitrate"], body["MaxAudioChannels"] = 400000, 1
	audioPIHTTPConditions(body, "wav", "pcm_s16le", audioPIHTTPCondition("AudioChannels", "Equals", "1"),
		audioPIHTTPCondition("AudioSampleRate", "Equals", "22050"), audioPIHTTPCondition("AudioBitDepth", "Equals", "16"),
		audioPIHTTPCondition("AudioBitrate", "Equals", "352800"))
	return body
}

func audioPIHTTPExactFLAC() map[string]any {
	body := audioPIHTTPBody(audioPIHTTPProfile("flac", "flac", "http"))
	body["MaxStreamingBitrate"], body["MaxAudioChannels"] = 900000, 1
	audioPIHTTPConditions(body, "flac", "flac", audioPIHTTPCondition("AudioChannels", "Equals", "1"),
		audioPIHTTPCondition("AudioSampleRate", "Equals", "32000"), audioPIHTTPCondition("AudioBitDepth", "Equals", "24"))
	return body
}

func audioPIHTTPUnavailable(t *testing.T, result audioPIHTTPResult, wantError bool) {
	t.Helper()
	if result.Source["SupportsDirectPlay"] != false || result.Source["SupportsDirectStream"] != false || result.Source["SupportsTranscoding"] != false ||
		result.Source["DirectStreamUrl"] != nil || result.Source["TranscodingUrl"] != nil {
		t.Fatal("unavailable audio playback advertised a usable delivery flag or URL")
	}
	if wantError && result.Error != "NoCompatibleStream" || !wantError && result.Error != "" {
		t.Fatal("audio playback availability did not match its error classification")
	}
}

func audioPIHTTPVerifyHLS(t *testing.T, a *audioHTTPFixture, result audioPIHTTPResult) {
	t.Helper()
	if result.Source["SupportsTranscoding"] != true || result.Source["TranscodingContainer"] != "ts" || result.Source["TranscodingSubProtocol"] != "hls" {
		t.Fatal("profile ordering skipped the first compatible HLS profile")
	}
	raw, ok := result.Source["TranscodingUrl"].(string)
	if !ok {
		t.Fatal("HLS PlaybackInfo omitted its master URL")
	}
	masterURL := hlsHTTPURL(t, raw, a.accounts.viewer.headers.Get("X-Emby-Token"))
	if masterURL.Path != "/emby/Audio/"+a.sources["flac"].ID+"/master.m3u8" || masterURL.Query().Get("PlaySessionId") != result.PlayID {
		t.Fatal("audio HLS PlaybackInfo URL lost its canonical playback scope")
	}
	started := len(a.encoderPIDs(t))
	head := a.request(t, http.MethodHead, masterURL.String(), nil, nil)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	master := a.request(t, http.MethodGet, masterURL.String(), nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	mainURLs := hlsHTTPManifestChildren(master.body)
	if len(mainURLs) != 1 || len(head.body) != 0 {
		t.Fatal("negotiated audio HLS did not expose one complete variant")
	}
	hlsHTTPURL(t, mainURLs[0], a.accounts.viewer.headers.Get("X-Emby-Token"))
	main := a.request(t, http.MethodGet, mainURLs[0], nil, nil)
	expectHLSHTTPStatus(t, main, http.StatusOK)
	children := hlsHTTPManifestChildren(main.body)
	if len(children) != 2 || len(a.encoderPIDs(t)) != started || !bytes.HasSuffix(main.body, []byte("#EXT-X-ENDLIST\n")) {
		t.Fatal("audio HLS negotiation or playlist reads started production or changed the full VOD table")
	}
	local := string(main.body)
	directory := t.TempDir()
	for index, child := range children {
		hlsHTTPURL(t, child, a.accounts.viewer.headers.Get("X-Emby-Token"))
		response := a.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		name := "negotiated-" + strconv.Itoa(index) + ".ts"
		if err := os.WriteFile(filepath.Join(directory, name), response.body, 0o600); err != nil {
			t.Fatal("write owned negotiated HLS segment")
		}
		if index == 0 {
			audioPIHTTPVerifyOutput(t, a, response.body, audioPIHTTPOutput{codec: "aac", container: "ts", channels: 2, rate: 48000, seconds: 3})
		}
		local = strings.ReplaceAll(local, child, name)
	}
	playlist := filepath.Join(directory, "negotiated.m3u8")
	if err := os.WriteFile(playlist, []byte(local), 0o600); err != nil {
		t.Fatal("write owned negotiated HLS playlist")
	}
	decoded := a.decodeFile(t, playlist)
	if seconds := float64(len(decoded)) / (48000 * 4); math.Abs(seconds-6) > .08 {
		t.Fatalf("negotiated audio HLS decoded %.6f seconds, want 6", seconds)
	}
}

func audioPIHTTPPolicy(t *testing.T, a *audioHTTPFixture, playback, remux, encode bool) {
	t.Helper()
	policy, err := json.Marshal(map[string]any{
		"EnableAllFolders": false, "EnabledFolders": []string{a.libraryID}, "EnableMediaPlayback": playback,
		"EnablePlaybackRemuxing": remux, "EnableAudioPlaybackTranscoding": encode, "EnableVideoPlaybackTranscoding": false,
	})
	if err != nil {
		t.Fatal("encode owned audio playback policy")
	}
	if _, err := a.f.pool.Exec(a.f.ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", a.accounts.viewer.userID, policy); err != nil {
		t.Fatal("update owned audio playback policy")
	}
}

func TestHTTPAudioPlaybackInfoRemuxPermissionsRecheckedAndOriginalFallbackPreserved(t *testing.T) {
	a := newAudioHTTPFixture(t, false)
	history := a.history(t)
	audioPIHTTPPolicy(t, a, true, true, false)
	encodeBody := audioPIHTTPBody(audioPIHTTPProfile("mp3", "mp3", "http"))
	encodeBody["MaxStreamingBitrate"] = 128000
	audioPIHTTPUnavailable(t, audioPIHTTPRequest(t, a, "flac", encodeBody), true)
	copyBody := audioPIHTTPBody(audioPIHTTPProfile("m4a", "aac", "http"))
	copyBody["EnableDirectStream"], copyBody["AllowAudioStreamCopy"] = true, true
	copyBody["DeviceProfile"].(map[string]any)["DirectPlayProfiles"] = []map[string]any{{"Type": "Audio", "Container": "mp3", "AudioCodec": "mp3"}}
	copyResult := audioPIHTTPRequest(t, a, "adts", copyBody)
	copyURL := audioPIHTTPURL(t, a, "adts", copyResult, "TranscodingUrl", "m4a")
	if copyURL.Query().Get("AudioCodec") != "copy" || copyURL.Query().Get("AllowAudioStreamCopy") != "true" || copyResult.Source["SupportsDirectStream"] != false {
		t.Fatal("remux-only negotiation did not explicitly preserve its copy operation")
	}
	copyOutput := a.request(t, http.MethodGet, copyURL.String(), nil, nil)
	expectHLSHTTPStatus(t, copyOutput, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, copyOutput.header, "audio/mp4")
	audioPIHTTPVerifyOutput(t, a, copyOutput.body, audioPIHTTPOutput{codec: "aac", container: "m4a", channels: 2, rate: 48000,
		seconds: float64(a.sources["adts"].Media.DurationTicks) / float64(media.TicksPerSecond)})
	// This exact cached URL must remain a copy request after permissions change;
	// silently re-encoding it would grant a different operation than advertised.
	audioPIHTTPPolicy(t, a, true, false, true)
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		expectHLSHTTPStatus(t, a.request(t, method, copyURL.String(), nil, nil), http.StatusUnsupportedMediaType)
	}
	encodedResult := audioPIHTTPRequest(t, a, "flac", encodeBody)
	encodedURL := audioPIHTTPURL(t, a, "flac", encodedResult, "TranscodingUrl", "mp3")
	if encodedURL.Query().Get("AudioCodec") != "mp3" || encodedURL.Query().Get("AllowAudioStreamCopy") != "false" {
		t.Fatal("transcode-only policy did not advertise a concrete encoding operation")
	}
	encoded := a.request(t, http.MethodGet, encodedURL.String(), nil, nil)
	expectHLSHTTPStatus(t, encoded, http.StatusOK)
	a.verifyMP3(t, encoded.body, 6)
	newAAC := audioPIHTTPRequest(t, a, "adts", copyBody)
	newAACURL := audioPIHTTPURL(t, a, "adts", newAAC, "TranscodingUrl", "m4a")
	if newAACURL.Query().Get("AudioCodec") != "aac" || newAACURL.Query().Get("AllowAudioStreamCopy") != "false" {
		t.Fatal("fresh negotiation retained copy after remux permission was revoked")
	}
	newAACOutput := a.request(t, http.MethodGet, newAACURL.String(), nil, nil)
	expectHLSHTTPStatus(t, newAACOutput, http.StatusOK)
	audioPIHTTPVerifyOutput(t, a, newAACOutput.body, audioPIHTTPOutput{codec: "aac", container: "m4a", channels: 2, rate: 48000,
		seconds: float64(a.sources["adts"].Media.DurationTicks) / float64(media.TicksPerSecond)})
	audioPIHTTPPolicy(t, a, true, false, false)
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		expectHLSHTTPStatus(t, a.request(t, method, encodedURL.String(), nil, nil), http.StatusUnsupportedMediaType)
	}
	audioPIHTTPUnavailable(t, audioPIHTTPRequest(t, a, "flac", encodeBody), true)
	audioPIHTTPPolicy(t, a, true, true, true)
	allDisabled := audioPIHTTPBody(audioPIHTTPProfile("mp3", "mp3", "http"))
	allDisabled["EnableTranscoding"] = false
	started := len(a.encoderPIDs(t))
	audioPIHTTPUnavailable(t, audioPIHTTPRequest(t, a, "flac", allDisabled), false)
	if len(a.encoderPIDs(t)) != started {
		t.Fatal("all-disabled playback negotiation started an encoder")
	}
	// No profile still means original bytes, with compatibility left to the
	// client. Conversion policy cannot rewrite or remove that source fallback.
	audioPIHTTPPolicy(t, a, true, false, false)
	minimal := audioPIHTTPRequest(t, a, "flac", map[string]any{})
	if minimal.Error != "" || minimal.Source["SupportsDirectPlay"] != true || minimal.Source["SupportsDirectStream"] != true ||
		minimal.Source["SupportsTranscoding"] != false || minimal.Source["TranscodingUrl"] != nil || minimal.Source["DirectStreamUrl"] != nil {
		t.Fatal("profile-free PlaybackInfo changed original-file fallback")
	}
	original := audioPIHTTPRequest(t, a, "flac", map[string]any{"EnableTranscoding": false, "EnableDirectPlay": true, "EnableDirectStream": true, "IsPlayback": true})
	raw, ok := original.Source["DirectStreamUrl"].(string)
	if !ok || original.Error != "" || original.Source["SupportsTranscoding"] != false {
		t.Fatal("explicit original fallback omitted its original audio URL")
	}
	uri, err := url.Parse(raw)
	if err != nil || uri.IsAbs() || uri.Host != "" || !strings.HasSuffix(uri.Path, "/original.flac") ||
		uri.Query().Get("PlaySessionId") != original.PlayID || uri.Query().Get("api_key") != a.accounts.viewer.headers.Get("X-Emby-Token") {
		t.Fatal("original fallback URL lost its authorized source identity")
	}
	originalBytes, err := os.ReadFile(a.sources["flac"].Path)
	if err != nil {
		t.Fatal("read owned original fallback audio")
	}
	response := a.request(t, http.MethodGet, uri.String(), nil, nil)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	if !bytes.Equal(response.body, originalBytes) || response.header.Get("ETag") == "" || len(a.encoderPIDs(t)) != started {
		t.Fatal("no-profile fallback did not retain complete original bytes and validators")
	}
	audioPIHTTPPolicy(t, a, false, true, true)
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		expectHLSHTTPStatus(t, a.request(t, method, encodedURL.String(), nil, nil), http.StatusForbidden)
	}
	expectHLSHTTPStatus(t, a.request(t, http.MethodPost, "/emby/Items/"+a.sources["flac"].ID+"/PlaybackInfo", encodeBody, a.accounts.viewer.headers), http.StatusForbidden)
	audioPIHTTPPolicy(t, a, true, true, true)
	if a.history(t) != history {
		t.Fatal("audio profile negotiation, permission checks, or source delivery changed user data")
	}
}
