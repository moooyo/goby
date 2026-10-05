//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

type bitmapPlaybackHTTPFixture struct {
	*hlsHTTPFixture
	tracks map[string]library.BitmapSubtitle
	paths  map[string]string
}

func newBitmapPlaybackHTTPFixture(t *testing.T) *bitmapPlaybackHTTPFixture {
	t.Helper()
	fixture := &bitmapPlaybackHTTPFixture{hlsHTTPFixture: newHLSHTTPFixture(t),
		tracks: make(map[string]library.BitmapSubtitle), paths: make(map[string]string)}
	base := strings.TrimSuffix(fixture.path, filepath.Ext(fixture.path))
	for _, extension := range []string{"sup", "idx", "sub"} {
		data, err := os.ReadFile(filepath.Join("testdata", "external-bitmap", "captions."+extension))
		if err != nil {
			t.Fatal(err)
		}
		path := base + "." + extension
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		fixture.paths[extension] = path
	}
	(&streamHTTPFixture{f: fixture.f}).rescan(t, fixture.libraryID)
	item, err := fixture.f.app.library.GetItem(fixture.f.ctx, fixture.accounts.viewer.userID, fixture.item.ID)
	if err != nil || len(item.BitmapSubtitles) != 3 || len(item.Subtitles) != 0 {
		t.Fatal("real scan did not index one SUP and two VobSub languages")
	}
	fixture.item = item
	indexes := make(map[int]bool)
	for _, track := range item.BitmapSubtitles {
		key := track.Language
		if track.Format == "sup" {
			key = "sup"
		}
		if indexes[track.Index] || len(track.Tag) != 64 || fixture.tracks[key].Tag != "" {
			t.Fatal("scanned bitmap tracks lost their distinct public identities")
		}
		indexes[track.Index], fixture.tracks[key] = true, track
	}
	if fixture.tracks["sup"].Codec != "hdmv_pgs_subtitle" || fixture.tracks["en"].Codec != "dvd_subtitle" ||
		fixture.tracks["zh"].Codec != "dvd_subtitle" || fixture.tracks["en"].SourceStreamIndex != 0 ||
		fixture.tracks["zh"].SourceStreamIndex != 1 || fixture.tracks["en"].Tag == fixture.tracks["zh"].Tag {
		t.Fatal("VobSub language IDs 3 and 7 were not indexed as distinct demux ordinals 0 and 1")
	}
	fixture.assertNoTimeline(t)
	return fixture
}

func (fixture *bitmapPlaybackHTTPFixture) assertNoTimeline(t *testing.T) {
	t.Helper()
	artifact, err := fixture.f.app.library.GetSubtitleTimelineFor(fixture.f.ctx,
		library.Subject{UserID: fixture.accounts.viewer.userID}, fixture.item.ID)
	if !errors.Is(err, library.ErrNotFound) || artifact.Available {
		t.Fatal("bitmap playback fixture unexpectedly depends on a generated timeline")
	}
	var count int
	if err := fixture.f.pool.QueryRow(fixture.f.ctx, "SELECT count(*) FROM subtitle_timeline_queue").Scan(&count); err != nil || count != 0 {
		t.Fatal("bitmap playback unexpectedly queued subtitle timeline generation")
	}
}

func bitmapPlaybackHTTPBody(protocol string, track library.BitmapSubtitle) map[string]any {
	body := videoHTTPBody(true, videoHTTPProfile(protocol, false, true))
	body["SubtitleStreamIndex"] = track.Index
	container := "mp4"
	if protocol == "hls" {
		container = "ts"
	}
	body["DeviceProfile"].(map[string]any)["SubtitleProfiles"] = []map[string]any{{
		"Format": track.Codec, "Method": "Encode", "Container": container, "Protocol": protocol,
	}}
	return body
}

func (fixture *bitmapPlaybackHTTPFixture) prepare(t *testing.T, protocol string, track library.BitmapSubtitle) videoHTTPNegotiation {
	t.Helper()
	response := fixture.request(t, http.MethodPost, "/emby/Items/"+fixture.item.ID+"/PlaybackInfo",
		bitmapPlaybackHTTPBody(protocol, track), fixture.accounts.viewer.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var decoded struct {
		PlayID  string           `json:"PlaySessionId"`
		Error   string           `json:"ErrorCode"`
		Sources []map[string]any `json:"MediaSources"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil || decoded.Error != "" || len(decoded.Sources) != 1 || decoded.PlayID == "" {
		t.Fatal("bitmap PlaybackInfo did not advertise one usable burn output")
	}
	source := decoded.Sources[0]
	raw, ok := source["TranscodingUrl"].(string)
	if !ok || source["SupportsTranscoding"] != true || source["SupportsDirectPlay"] != false ||
		source["SupportsDirectStream"] != false || source["TranscodingSubProtocol"] != protocol {
		t.Fatal("bitmap selection was silently ignored or advertised as original-file playback")
	}
	uri := hlsHTTPURL(t, raw, fixture.accounts.viewer.headers.Get("X-Emby-Token"))
	if protocol == "http" && (uri.Query().Get("SubtitleStreamIndex") != strconv.Itoa(track.Index) || uri.Query().Get("SubtitleDeliveryMethod") != "Encode") {
		t.Fatal("progressive burn URL omitted the selected public subtitle index")
	}
	streams, ok := source["MediaStreams"].([]any)
	if !ok || len(streams) != len(fixture.item.Media.Streams)+3 {
		t.Fatal("bitmap negotiation omitted original media or scanned subtitle tracks")
	}
	matched := 0
	for _, raw := range streams {
		stream, ok := raw.(map[string]any)
		if !ok || stream["Type"] != "Subtitle" {
			continue
		}
		matched++
		if stream["IsExternal"] != true || stream["IsTextSubtitleStream"] != false ||
			stream["SupportsExternalStream"] != false || stream["DeliveryMethod"] != "Encode" {
			t.Fatal("external bitmap track lacks its real burn-only delivery contract")
		}
		for _, field := range []string{"DeliveryUrl", "GobySubtitleTimelineOnly", "Path", "Components", "SourceStreamIndex"} {
			if _, exists := stream[field]; exists {
				t.Fatalf("bitmap DTO exposes unavailable or private field %s", field)
			}
		}
	}
	if matched != 3 {
		t.Fatal("bitmap negotiation did not preserve all three independently selectable tracks")
	}
	return videoHTTPNegotiation{playID: decoded.PlayID, uri: uri, source: source}
}

func TestHTTPExternalBitmapSubtitleBurnNegotiationAndActualDelivery(t *testing.T) {
	fixture := newBitmapPlaybackHTTPFixture(t)
	history := videoHTTPHistory(t, fixture.hlsHTTPFixture, fixture.item)
	for _, protocol := range []string{"hls", "http"} {
		for _, name := range []string{"sup", "en", "zh"} {
			t.Run(protocol+"/"+name, func(t *testing.T) {
				// PreparePlayback may reuse the same play for this user/item.
				// Retired encoders still have durable rows, so compare the entire
				// authenticated session before negotiation and after HEAD.
				countJobs := func() int {
					var count int
					if err := fixture.f.pool.QueryRow(fixture.f.ctx, "SELECT count(*) FROM encoding_jobs WHERE auth_session_id = $1", fixture.accounts.viewer.id).Scan(&count); err != nil {
						t.Fatal("read bitmap playback encoding count")
					}
					return count
				}
				beforeJobs := countJobs()
				track := fixture.tracks[name]
				prepared := fixture.prepare(t, protocol, track)
				expectHLSHTTPStatus(t, fixture.request(t, http.MethodHead, prepared.uri.String(), nil, nil), http.StatusOK)
				if countJobs() != beforeJobs {
					t.Fatal("bitmap negotiation or HEAD started a media encoder")
				}
				if protocol == "hls" {
					sessions := videoHTTPSessions(fixture.hlsHTTPFixture, prepared.playID)
					if len(sessions) != 1 {
						t.Fatal("HLS burn negotiation did not retain one immutable source plan")
					}
					session := sessions[0]
					selected := session.key.plan.Subtitle
					if selected.Mode != "burn" || selected.StreamIndex != track.Index || selected.ExternalStreamIndex != track.SourceStreamIndex || selected.ExternalTag != track.Tag {
						t.Fatal("negotiated burn plan conflated public index, source ordinal, or component tag")
					}
					spec := transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: session.key.plan}
					fixture.assertHeldBitmapAsset(t, spec, track)
					for _, mutate := range []func(*transcode.Spec){
						func(s *transcode.Spec) { s.Scope.AuthSessionID = fixture.accounts.second.id },
						func(s *transcode.Spec) { s.Plan.Subtitle.ExternalTag = strings.Repeat("0", 64) },
						func(s *transcode.Spec) { s.Plan.Subtitle.ExternalStreamIndex++ },
					} {
						wrong := spec
						mutate(&wrong)
						called := false
						err := fixture.f.app.readBurnBitmapSubtitleAsset(fixture.f.ctx, wrong, func(context.Context, media.ExternalSubtitleTimelineInput) error {
							called = true
							return nil
						})
						if !errors.Is(err, library.ErrNotFound) || called {
							t.Fatal("late bitmap loader borrowed a differently scoped or selected asset")
						}
					}
				}
				// The complete three-track timing/seek matrix lives in the media
				// tests. One real HTTP encode proves the server's late loader is
				// connected to the manager and produces visible, clearing pixels.
				if protocol == "http" && name == "sup" {
					output := fixture.request(t, http.MethodGet, prepared.uri.String(), nil, nil)
					expectHLSHTTPStatus(t, output, http.StatusOK)
					videoHTTPVerifyMP4(t, fixture.hlsHTTPFixture, output.body,
						videoHTTPOutput{width: 160, height: 90, frames: 288, audio: true, seconds: 12, color: [3]int{255, 0, 0}})
					fixture.assertVisibleAndClearedSUP(t, output.body)
					_, record := videoHTTPRecord(t, fixture.hlsHTTPFixture, prepared.playID, "h264", 0)
					if record.State != "completed" || record.Spec.Plan.Subtitle.ExternalTag != track.Tag || record.Spec.Plan.Subtitle.ExternalStreamIndex != 0 {
						t.Fatal("actual server producer lost its source-bound bitmap identity")
					}
				}
				sessions := videoHTTPSessions(fixture.hlsHTTPFixture, prepared.playID)
				expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, videoHTTPStopPath(fixture.accounts.viewer, prepared.playID), nil, fixture.accounts.viewer.headers), http.StatusNoContent)
				videoHTTPWaitRetired(t, fixture.hlsHTTPFixture, prepared.playID, sessions)
			})
		}
	}
	fixture.assertNoTimeline(t)
	if videoHTTPHistory(t, fixture.hlsHTTPFixture, fixture.item) != history {
		t.Fatal("bitmap negotiation or delivery fabricated playback progress")
	}
}

func (fixture *bitmapPlaybackHTTPFixture) assertHeldBitmapAsset(t *testing.T, spec transcode.Spec, track library.BitmapSubtitle) {
	t.Helper()
	var held []*os.File
	err := fixture.f.app.readBurnBitmapSubtitleAsset(fixture.f.ctx, spec, func(ctx context.Context, input media.ExternalSubtitleTimelineInput) error {
		if input.StreamIndex != track.Index || input.SourceStreamIndex != track.SourceStreamIndex || input.Codec != track.Codec || input.Input == nil {
			return errors.New("late loader substituted another selected bitmap source")
		}
		held = append(held, input.Input)
		if input.Companion != nil {
			held = append(held, input.Companion)
		}
		tracks, err := media.InspectExternalBitmapSubtitles(ctx, track.Format, input.Input, input.Companion)
		if err != nil || len(tracks) <= track.SourceStreamIndex || tracks[track.SourceStreamIndex].Codec != track.Codec {
			return errors.New("late loader did not borrow an independently inspectable source")
		}
		return nil
	})
	wantFiles := 1
	if track.Format == "vobsub" {
		wantFiles = 2
	}
	if err != nil || len(held) != wantFiles {
		t.Fatalf("authorized bitmap asset was not borrowed successfully: %v", err)
	}
	for _, file := range held {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("late bitmap source descriptor escaped its synchronous callback")
		}
	}
}

func (fixture *bitmapPlaybackHTTPFixture) assertVisibleAndClearedSUP(t *testing.T, data []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bitmap-burn.mp4")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	const width, height = 160, 90
	mask := bitmapPlaybackAuthoredMask(width, height)
	for _, sample := range []struct {
		seconds string
		visible bool
	}{{"0.5", false}, {"2", true}, {"7", false}} {
		pixels := hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-filter_threads", "1",
			"-i", path, "-ss", sample.seconds, "-map", "0:v:0", "-frames:v", "1", "-pix_fmt", "rgb24", "-threads", "1", "-f", "rawvideo", "-")
		if len(pixels) != width*height*3 {
			t.Fatal("burned HTTP output lacks a complete independently decoded video frame")
		}
		// The authored 2-pixel strokes become antialiased subpixel edges on
		// this smaller source. Match their independently authored shape and
		// normalized position instead of requiring nearly pure white pixels.
		var count, sumMask, sumLight, sumMask2, sumLight2, product float64
		var onLight, offLight float64
		onCount, offCount, outside := 0, 0, 0
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				index := (y*width + x) * 3
				// Both sampled solid backgrounds have a zero RGB channel;
				// a white caption raises all three channels together.
				light := float64(min(pixels[index], pixels[index+1], pixels[index+2]))
				inside := x >= 8 && x < 79 && y >= 16 && y < 29
				if !inside && light > 30 {
					outside++
				}
				if !inside {
					continue
				}
				expected := mask[y*width+x]
				count++
				sumMask += expected
				sumLight += light
				sumMask2 += expected * expected
				sumLight2 += light * light
				product += expected * light
				if expected >= .35 {
					onLight += light
					onCount++
				} else if expected <= .05 {
					offLight += light
					offCount++
				}
			}
		}
		if onCount == 0 || offCount == 0 {
			t.Fatal("authored subtitle mask lacks glyph and blank comparison pixels")
		}
		onLight /= float64(onCount)
		offLight /= float64(offCount)
		correlation := 0.0
		variance := (count*sumMask2 - sumMask*sumMask) * (count*sumLight2 - sumLight*sumLight)
		if variance > 0 {
			correlation = (count*product - sumMask*sumLight) / math.Sqrt(variance)
		}
		if sample.visible && (onLight < 25 || onLight-offLight < 20 || correlation < .6 || outside > 4) ||
			!sample.visible && (onLight > 10 || offLight > 10 || outside > 4) {
			t.Fatalf("SUP shape/position/clear mismatch at %ss: glyph=%g blank=%g correlation=%g unexpected pixels=%d", sample.seconds, onLight, offLight, correlation, outside)
		}
	}
}

// This oracle contains the authored glyphs and PGS presentation coordinates,
// not a decoded or production-rendered subtitle image. Area coverage maps its
// 320x192 canvas into the primary video's pixel grid independently of swscale.
func bitmapPlaybackAuthoredMask(width, height int) []float64 {
	glyphs := map[rune][7]string{
		'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
		'e': {"00000", "00000", "01110", "10001", "11111", "10000", "01111"},
		'l': {"01100", "00100", "00100", "00100", "00100", "00100", "01110"},
		'o': {"00000", "00000", "01110", "10001", "10001", "10001", "01110"},
		'w': {"00000", "00000", "10001", "10001", "10101", "10101", "01010"},
		'r': {"00000", "00000", "10110", "11001", "10000", "10000", "10000"},
		'd': {"00001", "00001", "01101", "10011", "10001", "10001", "01111"},
	}
	canvas := make([]bool, 320*192)
	for position, character := range "Hello world" {
		for row, pixels := range glyphs[character] {
			for column, pixel := range pixels {
				if pixel == '1' {
					for y := 0; y < 2; y++ {
						for x := 0; x < 2; x++ {
							canvas[(40+row*2+y)*320+21+position*12+column*2+x] = true
						}
					}
				}
			}
		}
	}
	mask := make([]float64, width*height)
	for y := 0; y < height; y++ {
		top, bottom := float64(y)*192/float64(height), float64(y+1)*192/float64(height)
		for x := 0; x < width; x++ {
			left, right := float64(x)*320/float64(width), float64(x+1)*320/float64(width)
			for sourceY := int(top); sourceY < int(math.Ceil(bottom)); sourceY++ {
				for sourceX := int(left); sourceX < int(math.Ceil(right)); sourceX++ {
					if canvas[sourceY*320+sourceX] {
						mask[y*width+x] += (min(right, float64(sourceX+1)) - max(left, float64(sourceX))) *
							(min(bottom, float64(sourceY+1)) - max(top, float64(sourceY))) / ((right - left) * (bottom - top))
					}
				}
			}
		}
	}
	return mask
}

func TestHTTPExternalBitmapSubtitleBurnAuthorization(t *testing.T) {
	fixture := newBitmapPlaybackHTTPFixture(t)
	track := fixture.tracks["sup"]
	prepared := []videoHTTPNegotiation{fixture.prepare(t, "http", track), fixture.prepare(t, "hls", track)}
	path := "/emby/Items/" + fixture.item.ID + "/PlaybackInfo"
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodPost, path, bitmapPlaybackHTTPBody("http", track), nil), http.StatusUnauthorized)
	for _, negotiation := range prepared {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			expectHLSHTTPStatus(t, fixture.request(t, method, hlsHTTPWithoutToken(t, negotiation.uri.String()), nil, nil), http.StatusUnauthorized)
			foreign := *negotiation.uri
			query := foreign.Query()
			query.Del("api_key")
			query.Set("DeviceId", fixture.accounts.second.deviceID)
			foreign.RawQuery = query.Encode()
			expectHLSHTTPStatus(t, fixture.request(t, method, foreign.String(), nil, fixture.accounts.second.headers), http.StatusNotFound)
		}
	}
	videoHTTPPolicy(t, fixture.hlsHTTPFixture, false)
	for _, protocol := range []string{"http", "hls"} {
		response := fixture.request(t, http.MethodPost, path, bitmapPlaybackHTTPBody(protocol, track), fixture.accounts.viewer.headers)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if bytes.Contains(response.body, []byte(`"TranscodingUrl"`)) || bytes.Contains(response.body, []byte(`"SupportsTranscoding":true`)) {
			t.Fatal("disabled video conversion still advertises bitmap burn playback")
		}
	}
	for _, negotiation := range prepared {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			if response := fixture.request(t, method, negotiation.uri.String(), nil, nil); response.status < 400 {
				t.Fatal("a previously negotiated bitmap burn bypassed current conversion policy")
			}
		}
	}
	videoHTTPPolicy(t, fixture.hlsHTTPFixture, true)
	fixture.policy(t, false)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodPost, path, bitmapPlaybackHTTPBody("http", track), fixture.accounts.viewer.headers), http.StatusForbidden)
	for _, negotiation := range prepared {
		if videoHTTPJobCount(t, fixture.hlsHTTPFixture, negotiation.playID, false) != 0 {
			t.Fatal("unauthorized bitmap requests started an encoder")
		}
	}
}

func TestHTTPExternalBitmapSubtitleBurnRejectsSamePathChanges(t *testing.T) {
	for _, extension := range []string{"sup", "idx", "sub"} {
		t.Run(extension, func(t *testing.T) {
			fixture := newBitmapPlaybackHTTPFixture(t)
			track := fixture.tracks["sup"]
			if extension != "sup" {
				track = fixture.tracks["zh"]
			}
			prepared := []videoHTTPNegotiation{fixture.prepare(t, "http", track), fixture.prepare(t, "hls", track)}
			path := fixture.paths[extension]
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Retain the path, inode, size, and mtime to ensure these routes
			// cannot authorize replacement bytes using only cheap file facts.
			data[len(data)-1] ^= 1
			if err := os.WriteFile(path, data, before.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			for index, protocol := range []string{"http", "hls"} {
				negotiation := prepared[index]
				for _, method := range []string{http.MethodHead, http.MethodGet} {
					response := fixture.request(t, method, negotiation.uri.String(), nil, nil)
					if response.status < 400 {
						t.Fatal("delivery accepted changed bitmap components under their old fingerprint")
					}
				}
				response := fixture.request(t, http.MethodPost, "/emby/Items/"+fixture.item.ID+"/PlaybackInfo", bitmapPlaybackHTTPBody(protocol, track), fixture.accounts.viewer.headers)
				if response.status < 400 {
					t.Fatal("negotiation advertised changed bitmap components without a new scan")
				}
				if videoHTTPJobCount(t, fixture.hlsHTTPFixture, negotiation.playID, false) != 0 {
					t.Fatal("stale external bitmap components started an encoder")
				}
			}
		})
	}
}
