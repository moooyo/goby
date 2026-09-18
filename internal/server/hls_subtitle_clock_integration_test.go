//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

// The colored source has a green transition at frame 72 (3 seconds) and a
// yellow transition at frame 216 (9 seconds). Compare the returned cue clock
// with actual presentation timestamps for those frames, not predicted muxer
// constants or a second invocation of the production timestamp helper.
func TestHTTPHLSSubtitleRenditionsMatchActualMediaPresentationClock(t *testing.T) {
	h := newHLSHTTPFixture(t)
	// Include reordered input video: make_zero can shift copied PTS when DTS is
	// negative, independently of AAC encoder priming in the encoded candidates.
	reordered := filepath.Join(t.TempDir(), "reordered.mp4")
	hlsHTTPMediaCommand(t, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", h.path,
		"-map", "0:v:0", "-map", "0:a:0", "-c:v", "libx264", "-threads:v", "1", "-bf", "2",
		"-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-pix_fmt", "yuv420p", "-c:a", "copy", reordered)
	encoded, err := os.ReadFile(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	stem := strings.TrimSuffix(h.path, filepath.Ext(h.path))
	tracks := map[string]string{
		"en.srt": "1\n00:00:03,000 --> 00:00:04,000\nGreen English\n\n2\n00:00:09,000 --> 00:00:10,000\nYellow English\n",
		// This is a valid source VTT header belonging to a different transport
		// clock. Generated HLS must not inherit that unrelated MPEGTS mapping.
		"fr.vtt": "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:900000\n\n00:03.000 --> 00:04.000\nGreen French\n\n00:09.000 --> 00:10.000\nYellow French\n",
	}
	for suffix, contents := range tracks {
		if err := os.WriteFile(stem+"."+suffix, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
	item, err := h.f.app.library.GetItem(h.f.ctx, h.accounts.viewer.userID, h.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	indexes := map[string]int{}
	for _, track := range item.Subtitles {
		indexes[track.Language] = track.Index
	}
	if len(indexes) != 2 {
		t.Fatal("both clock comparison subtitle tracks must be indexed")
	}
	for _, container := range []string{"ts", "mp4"} {
		for _, copyVideo := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/copy=%t", container, copyVideo), func(t *testing.T) {
				prepared := h.request(t, http.MethodPost, "/emby/Items/"+item.ID+"/PlaybackInfo", map[string]any{}, h.accounts.viewer.headers)
				expectHLSHTTPStatus(t, prepared, http.StatusOK)
				var negotiation struct {
					PlaySessionID string `json:"PlaySessionId"`
				}
				if json.Unmarshal(prepared.body, &negotiation) != nil || negotiation.PlaySessionID == "" {
					t.Fatal("subtitle clock fixture did not prepare playback")
				}
				defer func() {
					stop := "/emby/Videos/ActiveEncodings?" + url.Values{"PlaySessionId": {negotiation.PlaySessionID}, "DeviceId": {h.accounts.viewer.deviceID}}.Encode()
					expectHLSHTTPStatus(t, h.request(t, http.MethodDelete, stop, nil, h.accounts.viewer.headers), http.StatusNoContent)
				}()
				query := url.Values{"api_key": {h.accounts.viewer.headers.Get("X-Emby-Token")},
					"PlaySessionId": {negotiation.PlaySessionID}, "MediaSourceId": {media.SourceID(item.ID)}, "DeviceId": {h.accounts.viewer.deviceID},
					"SegmentContainer": {container}, "VideoCodec": {"h264"}, "AudioCodec": {"aac"}, "SegmentLength": {"3"},
					"AllowVideoStreamCopy": {strconv.FormatBool(copyVideo)}, "AllowAudioStreamCopy": {strconv.FormatBool(copyVideo)},
					"StartTimeTicks": {"65000000"}, "SubtitleStreamIndex": {strconv.Itoa(indexes["en"])}, "ManifestSubtitles": {"vtt"},
					"SubtitleOffsetTicks": {"2500000"}}
				adaptive := container == "mp4" && !copyVideo
				if adaptive {
					query.Set("EnableAdaptiveBitrate", "true")
				}
				masterPath := "/emby/Videos/" + item.ID + "/master.m3u8?"
				master := h.request(t, http.MethodGet, masterPath+query.Encode(), nil, nil)
				expectHLSHTTPStatus(t, master, http.StatusOK)
				variants := hlsHTTPManifestChildren(master.body)
				if adaptive && len(variants) < 2 || !adaptive && len(variants) != 1 {
					t.Fatal("clock comparison did not receive the requested media rendition count")
				}
				variantFrames := make([][]float64, 0, len(variants))
				seenVariants := make(map[string]bool)
				for _, variant := range variants {
					if seenVariants[variant] {
						t.Fatal("adaptive subtitle clock comparison received aliased variant URLs")
					}
					seenVariants[variant] = true
					var playlist hlsHTTPResponse
					for attempt := 0; attempt < 200; attempt++ {
						playlist = h.request(t, http.MethodGet, variant, nil, nil)
						expectHLSHTTPStatus(t, playlist, http.StatusOK)
						if bytes.Contains(playlist.body, []byte("#EXT-X-ENDLIST")) {
							break
						}
						select {
						case <-h.f.ctx.Done():
							t.Fatal("subtitle media production exceeded the fixture deadline")
						case <-time.After(25 * time.Millisecond):
						}
					}
					if !bytes.Contains(playlist.body, []byte("#EXT-X-ENDLIST")) || !bytes.Contains(playlist.body, []byte("#EXT-X-START:TIME-OFFSET=6.5000000,PRECISE=YES")) {
						t.Fatal("seek must retain the complete source timeline and an explicit start hint")
					}
					variantFrames = append(variantFrames, hlsSubtitleActualFramePTS(t, h, playlist.body, container))
				}
				vttPath, vtt := hlsSubtitleClockDocument(t, h, master.body)
				// One subtitle rendition must map to every independently muxed
				// variant, including after switching resolution or seeking.
				for _, framePTS := range variantFrames {
					hlsAssertSubtitleCueClock(t, vtt, "English", .25, framePTS)
				}
				for _, start := range []string{"0", "95000000", "30000000"} {
					seek, err := url.Parse(variants[0])
					if err != nil {
						t.Fatal(err)
					}
					values := seek.Query()
					values.Set("StartTimeTicks", start)
					seek.RawQuery = values.Encode()
					expectHLSHTTPStatus(t, h.request(t, http.MethodGet, seek.String(), nil, nil), http.StatusOK)
					unchanged := h.request(t, http.MethodGet, vttPath, nil, nil)
					expectHLSHTTPStatus(t, unchanged, http.StatusOK)
					for _, framePTS := range variantFrames {
						hlsAssertSubtitleCueClock(t, unchanged.body, "English", .25, framePTS)
					}
				}
				// A new selected track gets its own immutable rendition, while an
				// attempted in-place track change on an existing URL is rejected.
				mutated, _ := url.Parse(vttPath)
				values := mutated.Query()
				values.Set("SubtitleStreamIndex", strconv.Itoa(indexes["fr"]))
				mutated.RawQuery = values.Encode()
				expectHLSHTTPStatus(t, h.request(t, http.MethodGet, mutated.String(), nil, nil), http.StatusBadRequest)
				query.Set("SubtitleStreamIndex", strconv.Itoa(indexes["fr"]))
				query.Set("SubtitleOffsetTicks", "-2500000")
				switched := h.request(t, http.MethodGet, masterPath+query.Encode(), nil, nil)
				expectHLSHTTPStatus(t, switched, http.StatusOK)
				switchedPath, french := hlsSubtitleClockDocument(t, h, switched.body)
				if switchedPath == vttPath || bytes.Contains(french, []byte("English")) {
					t.Fatal("subtitle switching reused the previous track's resource")
				}
				for _, framePTS := range variantFrames {
					hlsAssertSubtitleCueClock(t, french, "French", -.25, framePTS)
				}
				query.Set("SubtitleStreamIndex", "-1")
				query.Del("ManifestSubtitles")
				query.Del("SubtitleOffsetTicks")
				off := h.request(t, http.MethodGet, masterPath+query.Encode(), nil, nil)
				expectHLSHTTPStatus(t, off, http.StatusOK)
				if bytes.Contains(off.body, []byte("TYPE=SUBTITLES")) || bytes.Contains(off.body, []byte("SUBTITLES=")) {
					t.Fatal("the disabled subtitle selection still advertised a rendition")
				}
			})
		}
	}
}

func hlsSubtitleClockDocument(t *testing.T, h *hlsHTTPFixture, master []byte) (string, []byte) {
	t.Helper()
	child := ""
	for _, line := range strings.Split(string(master), "\n") {
		if strings.HasPrefix(line, "#EXT-X-MEDIA:TYPE=SUBTITLES,") {
			_, value, found := strings.Cut(line, "URI=\"")
			if found {
				child, _, _ = strings.Cut(value, "\"")
			}
		}
	}
	if child == "" {
		t.Fatal("HLS subtitle rendition was not advertised")
	}
	playlist := h.request(t, http.MethodGet, child, nil, nil)
	expectHLSHTTPStatus(t, playlist, http.StatusOK)
	children := hlsHTTPManifestChildren(playlist.body)
	if len(children) != 1 {
		t.Fatal("expected the bounded full-source WebVTT segment")
	}
	vtt := h.request(t, http.MethodGet, children[0], nil, nil)
	expectHLSHTTPStatus(t, vtt, http.StatusOK)
	return children[0], vtt.body
}

func hlsSubtitleActualFramePTS(t *testing.T, h *hlsHTTPFixture, playlist []byte, container string) []float64 {
	t.Helper()
	var combined []byte
	for _, line := range strings.Split(string(playlist), "\n") {
		if strings.HasPrefix(line, "#EXT-X-MAP:URI=\"") {
			path := strings.TrimSuffix(strings.TrimPrefix(line, "#EXT-X-MAP:URI=\""), "\"")
			init := h.request(t, http.MethodGet, path, nil, nil)
			expectHLSHTTPStatus(t, init, http.StatusOK)
			combined = append(combined, init.body...)
		}
	}
	for _, child := range hlsHTTPManifestChildren(playlist) {
		segment := h.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, segment, http.StatusOK)
		combined = append(combined, segment.body...)
		if len(combined) > 32<<20 {
			t.Fatal("clock comparison media exceeded its fixture budget")
		}
	}
	path := filepath.Join(t.TempDir(), "presentation."+container)
	if err := os.WriteFile(path, combined, 0600); err != nil {
		t.Fatal(err)
	}
	output := hlsHTTPMediaCommand(t, h.ffprobe, "-v", "error", "-select_streams", "v:0", "-show_frames", "-show_entries", "frame=best_effort_timestamp_time", "-of", "json", path)
	var facts struct {
		Frames []struct {
			PTS string `json:"best_effort_timestamp_time"`
		} `json:"frames"`
	}
	if json.Unmarshal(output, &facts) != nil || len(facts.Frames) < 223 {
		t.Fatal("actual presentation frames are missing")
	}
	result := make([]float64, len(facts.Frames))
	for index, frame := range facts.Frames {
		value, err := strconv.ParseFloat(frame.PTS, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatal("invalid decoded presentation timestamp")
		}
		result[index] = value
	}
	return result
}

func hlsAssertSubtitleCueClock(t *testing.T, data []byte, language string, delay float64, frames []float64) {
	t.Helper()
	doc, err := subtitle.Parse(data, subtitle.FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	local, transport := float64(0), float64(0)
	mappings := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "X-TIMESTAMP-MAP=") {
			continue
		}
		mappings++
		seenLocal, seenTransport := false, false
		for _, attribute := range strings.Split(strings.TrimPrefix(line, "X-TIMESTAMP-MAP="), ",") {
			if value, ok := strings.CutPrefix(attribute, "LOCAL:"); ok {
				parsed, err := subtitle.Parse([]byte("WEBVTT\n\n"+value+" --> "+value+"\nanchor\n"), subtitle.FormatWebVTT)
				if err != nil || len(parsed.Cues) != 1 {
					t.Fatal("invalid WebVTT local clock mapping")
				}
				local, seenLocal = float64(parsed.Cues[0].StartTicks)/float64(media.TicksPerSecond), true
			}
			if value, ok := strings.CutPrefix(attribute, "MPEGTS:"); ok {
				pts, err := strconv.ParseUint(value, 10, 33)
				if err != nil {
					t.Fatal("invalid WebVTT transport clock mapping")
				}
				transport, seenTransport = float64(pts)/90000, true
			}
		}
		if !seenLocal || !seenTransport {
			t.Fatal("incomplete WebVTT timestamp map")
		}
	}
	if mappings > 1 {
		t.Fatal("the subtitle retained more than one transport clock")
	}
	for _, cue := range []struct {
		text   string
		second float64
	}{{"Green " + language, 3}, {"Yellow " + language, 9}} {
		found := false
		for _, actual := range doc.Cues {
			if actual.Text != cue.text {
				continue
			}
			found = true
			mapped := transport + float64(actual.StartTicks)/float64(media.TicksPerSecond) - local
			frame := int(math.Round((cue.second + delay) * 24))
			if math.Abs(mapped-frames[frame]) > .002 {
				t.Errorf("%s maps to %.6f seconds, actual presentation frame %d is %.6f seconds", cue.text, mapped, frame, frames[frame])
			}
		}
		if !found {
			t.Errorf("selected subtitle cue %q is missing", cue.text)
		}
	}
}
