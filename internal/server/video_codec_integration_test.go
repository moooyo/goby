//go:build linux

package server

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestHTTPModernVideoEncodingProducesDeclaredMediaAndExactSeek(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	for _, codec := range []string{"hevc", "av1"} {
		for _, depth := range []int{8, 10} {
			t.Run(codec+"/"+strconv.Itoa(depth), func(t *testing.T) {
				profile := videoHTTPProfile("http", true, true)
				profile["VideoCodec"] = codec
				body := videoHTTPBody(true, profile)
				body["StartTimeTicks"] = 42_500_000
				body["DeviceProfile"].(map[string]any)["CodecProfiles"] = []map[string]any{
					{"Type": "Video", "Codec": codec, "Conditions": []map[string]any{
						audioPIHTTPCondition("VideoBitDepth", "Equals", strconv.Itoa(depth)),
						audioPIHTTPCondition("VideoBitrate", "Equals", "300000"),
					}},
				}
				prepared := videoHTTPPrepare(t, fixture, fixture.item, body, "http")
				if prepared.uri.Query().Get("VideoCodec") != codec || prepared.uri.Query().Get("VideoBitDepth") != strconv.Itoa(depth) {
					t.Fatal("negotiated HTTP URL weakened the requested codec or precision")
				}
				response := fixture.request(t, http.MethodGet, prepared.uri.String(), nil, nil)
				expectHLSHTTPStatus(t, response, http.StatusOK)
				pixelFormat := "yuv420p"
				if depth == 10 {
					pixelFormat = "yuv420p10le"
				}
				videoHTTPVerifyMP4(t, fixture, response.body, videoHTTPOutput{codec: codec, pixelFormat: pixelFormat,
					width: 96, height: 54, frames: 186, audio: true, seconds: 7.75, color: [3]int{0, 128, 0}})
				_, record := videoHTTPRecord(t, fixture, prepared.playID, codec, 42_500_000)
				if record.State != "completed" || record.Spec.Plan.VideoBitDepth != depth {
					t.Fatal("actual HTTP producer did not complete the negotiated precision")
				}
				sessions := videoHTTPSessions(fixture, prepared.playID)
				expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, videoHTTPStopPath(fixture.accounts.viewer, prepared.playID), nil, fixture.accounts.viewer.headers), http.StatusNoContent)
				videoHTTPWaitRetired(t, fixture, prepared.playID, sessions)
			})
		}
	}
}

func videoHTTPModernCopySource(t *testing.T, fixture *hlsHTTPFixture, codec string) library.Item {
	t.Helper()
	path := filepath.Join(filepath.Dir(fixture.path), "Copy.Seek."+codec+".mp4")
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-i", fixture.path,
		"-map", "0:v:0", "-map", "0:a:0", "-c:v", transcode.VideoEncoder(codec, "software"), "-threads:v", "1", "-g", "72", "-pix_fmt", "yuv420p", "-c:a", "copy"}
	if codec == "hevc" {
		args = append(args, "-preset", "ultrafast", "-x265-params", "log-level=error:pools=none:frame-threads=1:bframes=0:keyint=72:min-keyint=72:scenecut=0:open-gop=0")
	} else {
		args = append(args, "-usage", "realtime", "-cpu-used", "8", "-row-mt", "1", "-lag-in-frames", "0")
	}
	hlsHTTPMediaCommand(t, fixture.ffmpeg, append(args, path)...)
	(&streamHTTPFixture{f: fixture.f}).rescan(t, fixture.libraryID)
	items, err := fixture.f.app.library.QueryItems(fixture.f.ctx, library.Query{UserID: fixture.accounts.admin.userID, ParentID: fixture.libraryID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items.Items {
		if item.Path == path && item.Media != nil {
			return item
		}
	}
	t.Fatal("the modern-codec copy fixture was not indexed")
	return library.Item{}
}

func TestHTTPModernHLSCopyKeepsSourceTimelineAcrossRandomAccess(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	for _, codec := range []string{"hevc", "av1"} {
		t.Run(codec, func(t *testing.T) {
			item := videoHTTPModernCopySource(t, fixture, codec)
			var sourceVideo *media.Stream
			for index := range item.Media.Streams {
				stream := &item.Media.Streams[index]
				if stream.CodecType == "video" && !stream.IsAttachedPicture {
					sourceVideo = stream
					break
				}
			}
			if sourceVideo == nil || media.EffectiveVideoBitDepth(*sourceVideo) != 8 {
				t.Fatal("the generated 8-bit source did not probe as an 8-bit modern video")
			}
			rawBitDepth := sourceVideo.BitDepth
			t.Logf("source codec=%s pixel_format=%s reported_bit_depth=%d effective_bit_depth=%d", sourceVideo.Codec, sourceVideo.PixelFormat, rawBitDepth, media.EffectiveVideoBitDepth(*sourceVideo))
			body := map[string]any{"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true, "StartTimeTicks": 72_500_000,
				"DeviceProfile": map[string]any{
					"TranscodingProfiles": []map[string]any{{"Type": "Video", "Container": "mp4", "Protocol": "hls", "VideoCodec": codec, "AudioCodec": "aac", "SegmentLength": 3}},
					"CodecProfiles": []map[string]any{{"Type": "Video", "Codec": codec,
						"Conditions": []map[string]any{audioPIHTTPCondition("VideoBitDepth", "Equals", "8")}}},
				}}
			response := fixture.request(t, http.MethodPost, "/emby/Items/"+item.ID+"/PlaybackInfo", body, fixture.accounts.viewer.headers)
			expectHLSHTTPStatus(t, response, http.StatusOK)
			var prepared struct {
				PlayID  string `json:"PlaySessionId"`
				Sources []struct {
					URL     string `json:"TranscodingUrl"`
					Streams []struct {
						Index    int    `json:"Index"`
						Type     string `json:"Type"`
						Codec    string `json:"Codec"`
						BitDepth int    `json:"BitDepth"`
					} `json:"MediaStreams"`
				} `json:"MediaSources"`
			}
			if json.Unmarshal(response.body, &prepared) != nil || len(prepared.Sources) != 1 || prepared.Sources[0].URL == "" {
				t.Fatal("HLS copy negotiation did not return a standard playback URL")
			}
			matchedVideo := false
			for _, stream := range prepared.Sources[0].Streams {
				if stream.Type == "Video" && stream.Index == sourceVideo.Index {
					matchedVideo = stream.Codec == codec && stream.BitDepth == 8
				}
			}
			if !matchedVideo || sourceVideo.BitDepth != rawBitDepth {
				t.Fatal("PlaybackInfo omitted effective precision or modified the original probed depth")
			}
			stored, err := fixture.f.app.library.GetItem(fixture.f.ctx, fixture.accounts.viewer.userID, item.ID)
			if err != nil || stored.Media == nil {
				t.Fatal("the source probe facts could not be read after negotiation")
			}
			retainedDepth := false
			for _, stream := range stored.Media.Streams {
				if stream.CodecType == "video" && stream.Index == sourceVideo.Index {
					retainedDepth = stream.BitDepth == rawBitDepth && stream.PixelFormat == sourceVideo.PixelFormat
				}
			}
			if !retainedDepth {
				t.Fatal("negotiation rewrote the cached probe depth instead of projecting effective precision")
			}
			masterURL := hlsHTTPURL(t, prepared.Sources[0].URL, fixture.accounts.viewer.headers.Get("X-Emby-Token"))
			if masterURL.Query().Get("VideoCodec") != codec || masterURL.Query().Get("VideoBitDepth") != "8" {
				t.Fatal("copied HLS output URL did not preserve the constrained video precision")
			}
			master := fixture.request(t, http.MethodGet, prepared.Sources[0].URL, nil, nil)
			expectHLSHTTPStatus(t, master, http.StatusOK)
			variants := hlsHTTPManifestChildren(master.body)
			if len(variants) != 1 {
				t.Fatal("HLS copy should preserve one source rendition")
			}
			variantURL := hlsHTTPURL(t, variants[0], fixture.accounts.viewer.headers.Get("X-Emby-Token"))
			if variantURL.Query().Get("VideoBitDepth") != "8" {
				t.Fatal("the copied HLS child lost its precision constraint")
			}
			playlist := fixture.request(t, http.MethodGet, variants[0], nil, nil)
			expectHLSHTTPStatus(t, playlist, http.StatusOK)
			if !strings.Contains(string(playlist.body), "#EXT-X-START:TIME-OFFSET=7.2500000,PRECISE=YES") {
				t.Fatal("ordinary HLS seeking lost the requested position within the original timeline")
			}
			segments, mapURI := hlsHTTPManifestChildren(playlist.body), ""
			for _, line := range strings.Split(string(playlist.body), "\n") {
				if strings.HasPrefix(line, "#EXT-X-MAP:URI=\"") {
					mapURI = strings.TrimSuffix(strings.TrimPrefix(line, "#EXT-X-MAP:URI=\""), "\"")
				}
			}
			if len(segments) != 4 || mapURI == "" {
				t.Fatal("HLS copy did not publish the complete source segment window")
			}
			init := fixture.request(t, http.MethodGet, mapURI, nil, nil)
			expectHLSHTTPStatus(t, init, http.StatusOK)
			for _, number := range []int{2, 3, 0, 2} {
				segment := fixture.request(t, http.MethodGet, segments[number], nil, nil)
				expectHLSHTTPStatus(t, segment, http.StatusOK)
				path := filepath.Join(t.TempDir(), "received.mp4")
				if err := os.WriteFile(path, append(append([]byte(nil), init.body...), segment.body...), 0600); err != nil {
					t.Fatal(err)
				}
				var facts struct {
					Streams []struct {
						Codec string `json:"codec_name"`
						Type  string `json:"codec_type"`
						Start string `json:"start_time"`
					} `json:"streams"`
				}
				probe := hlsHTTPMediaCommand(t, fixture.ffprobe, "-v", "error", "-show_entries", "stream=codec_name,codec_type,start_time", "-of", "json", path)
				if json.Unmarshal(probe, &facts) != nil {
					t.Fatal("copied HLS media could not be inspected")
				}
				found := false
				for _, stream := range facts.Streams {
					if stream.Type == "video" {
						start, err := strconv.ParseFloat(stream.Start, 64)
						found = err == nil && stream.Codec == codec && math.Abs(start-float64(number)*3) < .15
					}
				}
				if !found {
					t.Fatal("copied HLS video changed its codec or source-global presentation time")
				}
				hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1", "-i", path, "-map", "0:v:0", "-map", "0:a:0", "-threads", "1", "-f", "null", "-")
				pixel := hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1", "-filter_threads", "1",
					"-i", path, "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=1:1", "-pix_fmt", "rgb24", "-threads", "1", "-f", "rawvideo", "-")
				color := [][3]int{{255, 0, 0}, {0, 128, 0}, {0, 0, 255}, {255, 255, 0}}[number]
				if len(pixel) != 3 {
					t.Fatal("copied HLS segment did not decode its source-position marker")
				}
				for channel, expected := range color {
					if math.Abs(float64(int(pixel[channel])-expected)) > 20 {
						t.Fatal("random HLS access returned a different source interval")
					}
				}
			}
			sessions := videoHTTPSessions(fixture, prepared.PlayID)
			if len(sessions) != 1 || sessions[0].key.plan.VideoCodec != "copy" || sessions[0].key.plan.VideoCopyCodec != codec || sessions[0].key.plan.AudioCodec != "copy" || sessions[0].key.plan.StartTicks != 0 {
				t.Fatal("ordinary HLS seeking silently encoded media or shifted the source origin")
			}
			expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, videoHTTPStopPath(fixture.accounts.viewer, prepared.PlayID), nil, fixture.accounts.viewer.headers), http.StatusNoContent)
			videoHTTPWaitRetired(t, fixture, prepared.PlayID, sessions)
		})
	}
}
