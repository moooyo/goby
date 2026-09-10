//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type audioHTTPFixture struct {
	*hlsHTTPFixture
	sources map[string]library.Item
	pidFile string
}

// All database work uses the environment-selected integration database and an
// isolated schema from newServerFixture. Media, cache files, and executable
// wrappers belong exclusively to temporary directories owned by this test.
func newAudioHTTPFixture(t *testing.T, slow bool) *audioHTTPFixture {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real audio HTTP verification")
	}
	f, accounts := newClientSessionHTTPAccounts(t)
	root := t.TempDir()
	paths := map[string]string{
		"flac": filepath.Join(root, "Full.Source.flac"),
		"mp3":  filepath.Join(root, "Original.Source.mp3"),
		"adts": filepath.Join(root, "Packet.Source.aac"),
		"tail": filepath.Join(root, "Short.Tail.flac"),
	}
	for _, source := range []struct{ key, duration string }{{"flac", "6"}, {"tail", "6.001"}} {
		hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
			"-f", "lavfi", "-i", "sine=frequency=733:sample_rate=48000:duration="+source.duration,
			"-ac", "2", "-ar", "48000", "-c:a", "flac", "-sample_fmt", "s16", "-threads:a", "1", paths[source.key])
	}
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", paths["flac"],
		"-map", "0:a:0", "-c:a", "libmp3lame", "-b:a", "192000", "-threads:a", "1", paths["mp3"])
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", paths["flac"],
		"-map", "0:a:0", "-c:a", "aac", "-b:a", "128000", "-threads:a", "1", "-f", "adts", paths["adts"])
	f.app.notifier.Close()
	closeFixtureCatalogForReplacement(t, f)
	catalog, err := library.New(f.pool, media.Prober{FFprobePath: ffprobe, Timeout: 15 * time.Second}, []string{root})
	if err != nil {
		t.Fatalf("create real audio catalog (%T)", err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	t.Cleanup(func() {
		f.app.notifier.Close()
	})
	owned := t.TempDir()
	pidFile := filepath.Join(owned, "encoder-pids")
	wrapper := filepath.Join(owned, "ffmpeg-wrapper")
	readRate := ""
	if slow {
		readRate = " -readrate 0.25"
	}
	// Log only the process ID, never arguments containing paths or credentials.
	// exec preserves that PID and avoids introducing an untracked child shell.
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" >> " + audioHTTPShellQuote(pidFile) +
		"\nexec " + audioHTTPShellQuote(ffmpeg) + readRate + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal("write owned audio encoder wrapper")
	}
	f.app.cfg.FFmpegPath, f.app.cfg.FFprobePath = wrapper, ffprobe
	f.app.cfg.MediaRoots = []string{root}
	f.app.cfg.Transcoding = config.TranscodingConfig{
		Enabled: true, CacheDirectory: t.TempDir(), Threads: 1,
		MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2,
	}
	f.cfg = f.app.cfg
	runtime, err := newHLSRuntime(f.ctx, f.app)
	if err != nil {
		t.Fatalf("create real audio runtime (%T)", err)
	}
	f.app.hls = runtime
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Errorf("close real audio runtime (%T)", err)
		}
	})
	collection, err := catalog.CreateLibrary(f.ctx, "Real HTTP Audio", "music", []string{root})
	if err != nil {
		t.Fatalf("create audio library (%T)", err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	listed, err := catalog.QueryItems(f.ctx, library.Query{UserID: accounts.admin.userID, ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatalf("query indexed audio (%T)", err)
	}
	a := &audioHTTPFixture{hlsHTTPFixture: &hlsHTTPFixture{
		f: f, accounts: accounts, ffmpeg: ffmpeg, ffprobe: ffprobe, libraryID: collection.ID,
	}, sources: make(map[string]library.Item), pidFile: pidFile}
	for _, item := range listed.Items {
		for key, path := range paths {
			if item.Path == path && !item.IsFolder {
				if item.Type != "Audio" || item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion || !item.Media.AudioDurationExact {
					t.Fatalf("audio fixture %s lacks current exact media facts", key)
				}
				a.sources[key] = item
			}
		}
	}
	if len(a.sources) != len(paths) || a.sources["flac"].Media.DurationTicks != 6*media.TicksPerSecond || a.sources["tail"].Media.DurationTicks != 60_010_000 {
		t.Fatal("real audio sources were not indexed with their complete durations")
	}
	a.policy(t, true)
	f.handler = f.app.Handler()
	a.server = httptest.NewServer(f.handler)
	t.Cleanup(a.server.Close)
	return a
}

func audioHTTPShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (a *audioHTTPFixture) universal(key string, values url.Values) string {
	result := "/emby/Audio/" + a.sources[key].ID + "/universal"
	if len(values) != 0 {
		result += "?" + values.Encode()
	}
	return result
}

func audioHTTPMP3Query(reference string, start int64) url.Values {
	return url.Values{"PlaySessionId": {reference}, "Container": {"mp3"}, "MaxStreamingBitrate": {"128000"},
		"StartTimeTicks": {strconv.FormatInt(start, 10)}}
}

func audioHTTPOwner(login clientSessionHTTPLogin) library.PlaybackOwner {
	return library.PlaybackOwner{UserID: login.userID, SessionID: login.id, DeviceID: login.deviceID}
}

func (a *audioHTTPFixture) canonical(t *testing.T, login clientSessionHTTPLogin, reference, key string) string {
	t.Helper()
	id, err := a.f.app.library.ResolvePlaybackReference(a.f.ctx, audioHTTPOwner(login), reference)
	if err != nil || id == reference || !strings.HasPrefix(id, "play_") {
		t.Fatalf("resolve scoped audio playback reference (%T)", err)
	}
	play, err := a.f.app.library.GetPlaybackSession(a.f.ctx, audioHTTPOwner(login), id)
	if err != nil || play.ID != id || play.AuthSessionID != login.id || play.ItemID != a.sources[key].ID ||
		play.MediaSourceID != media.SourceID(a.sources[key].ID) || play.State != "Prepared" || play.PositionTicks != 0 || play.StartedAt != nil {
		t.Fatalf("audio GET did not retain an owned, unplayed preparation (%T)", err)
	}
	return id
}

func (a *audioHTTPFixture) jobs(t *testing.T, login clientSessionHTTPLogin, playID string, active bool) int {
	t.Helper()
	var count int
	if err := a.f.pool.QueryRow(a.f.ctx, `SELECT count(*) FROM encoding_jobs
		WHERE auth_session_id = $1 AND play_session_id = $2
		AND (NOT $3::boolean OR state IN ('queued','running'))`, login.id, playID, active).Scan(&count); err != nil {
		t.Fatal("read scoped audio encoding count")
	}
	return count
}

func (a *audioHTTPFixture) history(t *testing.T) string {
	t.Helper()
	values := make(map[string]library.UserData)
	for key, item := range a.sources {
		data, err := a.f.app.library.GetUserData(a.f.ctx, a.accounts.viewer.userID, item.ID)
		if err != nil {
			t.Fatalf("read audio fixture history (%T)", err)
		}
		values[key] = data
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal("encode audio fixture history")
	}
	return string(encoded)
}

func (a *audioHTTPFixture) encoderPIDs(t *testing.T) []int {
	t.Helper()
	data, err := os.ReadFile(a.pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal("read owned encoder process record")
	}
	var result []int
	for _, value := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(value)
		if err != nil || pid <= 0 {
			t.Fatal("invalid owned encoder process record")
		}
		result = append(result, pid)
	}
	return result
}

func TestHTTPAudioUniversalOriginalBytesRangeHeadAndConditional(t *testing.T) {
	a := newAudioHTTPFixture(t, false)
	before := a.history(t)
	for _, source := range []struct {
		key, contentType string
		values           url.Values
	}{{"flac", "audio/flac", url.Values{"Container": {"mp3,flac"}}}, {"mp3", "audio/mpeg", nil}} {
		t.Run(source.key, func(t *testing.T) {
			original, err := os.ReadFile(a.sources[source.key].Path)
			if err != nil {
				t.Fatal("read owned original audio")
			}
			target := a.universal(source.key, source.values)
			full := a.request(t, http.MethodGet, target, nil, a.accounts.viewer.headers)
			expectHLSHTTPStatus(t, full, http.StatusOK)
			if !bytes.Equal(full.body, original) || full.header.Get("Content-Type") != source.contentType || full.header.Get("ETag") == "" ||
				full.header.Get("Accept-Ranges") != "bytes" || full.header.Get("Content-Length") != strconv.Itoa(len(original)) {
				t.Fatal("Universal did not deliver the complete original representation")
			}
			head := a.request(t, http.MethodHead, target, nil, a.accounts.viewer.headers)
			expectHLSHTTPStatus(t, head, http.StatusOK)
			if len(head.body) != 0 || head.header.Get("ETag") != full.header.Get("ETag") || head.header.Get("Content-Length") != strconv.Itoa(len(original)) {
				t.Fatal("original audio HEAD changed representation metadata")
			}
			rangeHeaders := a.accounts.viewer.headers.Clone()
			rangeHeaders.Set("Range", "bytes=7-38")
			partial := a.request(t, http.MethodGet, target, nil, rangeHeaders)
			expectHLSHTTPStatus(t, partial, http.StatusPartialContent)
			if !bytes.Equal(partial.body, original[7:39]) || partial.header.Get("Content-Range") != fmt.Sprintf("bytes 7-38/%d", len(original)) {
				t.Fatal("original audio byte range did not match the source")
			}
			conditional := a.accounts.viewer.headers.Clone()
			conditional.Set("If-None-Match", full.header.Get("ETag"))
			cached := a.request(t, http.MethodGet, target, nil, conditional)
			expectHLSHTTPStatus(t, cached, http.StatusNotModified)
			if len(cached.body) != 0 {
				t.Fatal("conditional original audio had a body")
			}
			conditional.Set("X-Emby-Token", "invalid-audio-token")
			expectHLSHTTPStatus(t, a.request(t, http.MethodGet, target, nil, conditional), http.StatusUnauthorized)
		})
	}
	if len(a.encoderPIDs(t)) != 0 || a.history(t) != before {
		t.Fatal("original audio GETs started an encoder or changed playback history")
	}
}

func TestHTTPAudioProgressiveFreshNonceHeadRangeSeekAndScopedReuse(t *testing.T) {
	a := newAudioHTTPFixture(t, false)
	invalidCleanup := url.Values{"DeviceId": {a.accounts.viewer.deviceID}, "PlaySessionId": {strings.Repeat("x", 257)}}
	expectHLSHTTPStatus(t, a.request(t, http.MethodDelete, "/emby/Videos/ActiveEncodings?"+invalidCleanup.Encode(), nil, a.accounts.viewer.headers), http.StatusBadRequest)
	before := a.history(t)
	const headReference = "audio-head-only"
	head := a.request(t, http.MethodHead, a.universal("flac", audioHTTPMP3Query(headReference, 0)), nil, a.accounts.viewer.headers)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, head.header, "audio/mpeg")
	headID := a.canonical(t, a.accounts.viewer, headReference, "flac")
	if len(head.body) != 0 || a.jobs(t, a.accounts.viewer, headID, false) != 0 || len(a.encoderPIDs(t)) != 0 {
		t.Fatal("progressive HEAD started production or returned media bytes")
	}
	const reference = "fresh-client-audio-reference"
	if _, err := a.f.app.library.ResolvePlaybackReference(a.f.ctx, audioHTTPOwner(a.accounts.viewer), reference); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("fresh client reference already exists (%T)", err)
	}
	target := a.universal("flac", audioHTTPMP3Query(reference, 0))
	headers := a.accounts.viewer.headers.Clone()
	headers.Set("Range", "bytes=0-31")
	full := a.request(t, http.MethodGet, target, nil, headers)
	expectHLSHTTPStatus(t, full, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, full.header, "audio/mpeg")
	a.verifyMP3(t, full.body, 6)
	canonical := a.canonical(t, a.accounts.viewer, reference, "flac")
	if a.jobs(t, a.accounts.viewer, canonical, false) != 1 || len(a.encoderPIDs(t)) != 1 {
		t.Fatal("fresh Universal request did not produce one scoped audio job")
	}
	a.verifyPreparedPresence(t, canonical)
	retry := a.request(t, http.MethodGet, target, nil, a.accounts.viewer.headers)
	expectHLSHTTPStatus(t, retry, http.StatusOK)
	if !bytes.Equal(retry.body, full.body) || a.canonical(t, a.accounts.viewer, reference, "flac") != canonical ||
		a.jobs(t, a.accounts.viewer, canonical, false) != 1 || len(a.encoderPIDs(t)) != 1 {
		t.Fatal("same-scope audio retry did not reuse its canonical play and completed output")
	}
	if _, err := a.f.app.library.ResolvePlaybackReference(a.f.ctx, audioHTTPOwner(a.accounts.second), reference); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("client reference crossed authentication scope (%T)", err)
	}
	second := a.request(t, http.MethodGet, target, nil, a.accounts.second.headers)
	expectHLSHTTPStatus(t, second, http.StatusOK)
	otherCanonical := a.canonical(t, a.accounts.second, reference, "flac")
	if otherCanonical == canonical || a.jobs(t, a.accounts.second, otherCanonical, false) != 1 || len(a.encoderPIDs(t)) != 2 {
		t.Fatal("another authentication session reused the first session's cache identity")
	}
	expectHLSHTTPStatus(t, a.request(t, http.MethodGet, a.universal("tail", audioHTTPMP3Query(reference, 0)), nil, a.accounts.viewer.headers), http.StatusNotFound)
	if a.canonical(t, a.accounts.viewer, reference, "flac") != canonical {
		t.Fatal("cross-source nonce use rebound the existing play")
	}
	seek := a.request(t, http.MethodGet, a.universal("flac", audioHTTPMP3Query("fresh-seek-reference", 2*media.TicksPerSecond)), nil, a.accounts.viewer.headers)
	expectHLSHTTPStatus(t, seek, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, seek.header, "audio/mpeg")
	a.verifyMP3(t, seek.body, 4)
	if a.history(t) != before {
		t.Fatal("progressive media GETs fabricated playback history or seek progress")
	}
}

func (a *audioHTTPFixture) verifyPreparedPresence(t *testing.T, playID string) {
	t.Helper()
	runtime := a.f.app.hls
	runtime.mu.Lock()
	var selected *hlsSession
	for _, session := range runtime.sessions {
		if session.key.scope.AuthSessionID == a.accounts.viewer.id && session.key.scope.PlaySessionID == playID {
			selected = session
			break
		}
	}
	runtime.mu.Unlock()
	if selected == nil {
		t.Fatal("successful progressive request has no presence revision")
	}
	owner := audioHTTPOwner(a.accounts.viewer)
	before, err := a.f.app.library.GetPlaybackSession(a.f.ctx, owner, playID)
	if err != nil {
		t.Fatalf("read prepared presence baseline (%T)", err)
	}
	history := a.history(t)
	var countedBefore bool
	var shortExpiry time.Time
	if err := a.f.pool.QueryRow(a.f.ctx, `UPDATE play_sessions SET expires_at = clock_timestamp() + interval '1 minute'
		WHERE id = $1 AND user_id = $2 AND auth_session_id = $3 RETURNING counted, expires_at`,
		playID, owner.UserID, owner.SessionID).Scan(&countedBefore, &shortExpiry); err != nil {
		t.Fatal("shorten owned prepared playback lease")
	}
	refreshStarted := time.Now()
	selected.mu.Lock()
	selected.presenceUpdated = refreshStarted.Add(-2 * time.Minute)
	selected.accessed = refreshStarted
	selected.mu.Unlock()
	// Drive the real maintenance path after successful HTTP media delivery,
	// without waiting a minute or pretending the client reported Playing.
	ctx, cancel := context.WithTimeout(a.f.ctx, 5*time.Second)
	runtime.maintainSessions(ctx, []*hlsSession{selected})
	cancel()
	after, err := a.f.app.library.GetPlaybackSession(a.f.ctx, owner, playID)
	if err != nil {
		t.Fatalf("read renewed prepared playback lease (%T)", err)
	}
	var countedAfter, nearThirtyMinutes bool
	if err := a.f.pool.QueryRow(a.f.ctx, `SELECT counted,
		expires_at > clock_timestamp() + interval '29 minutes' AND expires_at < clock_timestamp() + interval '31 minutes'
		FROM play_sessions WHERE id = $1 AND user_id = $2 AND auth_session_id = $3`,
		playID, owner.UserID, owner.SessionID).Scan(&countedAfter, &nearThirtyMinutes); err != nil {
		t.Fatal("read renewed playback presence facts")
	}
	selected.mu.Lock()
	updated, closed := selected.presenceUpdated, selected.closed
	selected.mu.Unlock()
	if !nearThirtyMinutes || after.ExpiresAt.Sub(shortExpiry) < 28*time.Minute || updated.Before(refreshStarted) || closed {
		t.Fatal("recent media traffic did not renew its prepared play for thirty minutes")
	}
	if after.State != before.State || after.PositionTicks != before.PositionTicks || after.DurationTicks != before.DurationTicks ||
		after.StartedAt != nil || after.StoppedAt != nil || countedAfter != countedBefore || a.history(t) != history {
		t.Fatal("media presence renewal fabricated playback state, counts, or user data")
	}
}

func assertAudioHTTPProgressiveHeaders(t *testing.T, header http.Header, contentType string) {
	t.Helper()
	if header.Get("Content-Type") != contentType || header.Get("Accept-Ranges") != "none" ||
		header.Get("Content-Length") != "" || header.Get("Content-Range") != "" || header.Get("ETag") != "" ||
		!strings.Contains(header.Get("Cache-Control"), "no-store") {
		t.Fatal("progressive audio advertised an unsupported byte range or representation validator")
	}
}

func (a *audioHTTPFixture) decodeFile(t *testing.T, path string) []byte {
	t.Helper()
	return hlsHTTPMediaCommand(t, a.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1",
		"-protocol_whitelist", "file,crypto,data", "-i", path, "-map", "0:a:0", "-vn", "-sn", "-dn",
		"-ac", "2", "-ar", "48000", "-c:a", "pcm_s16le", "-threads:a", "1", "-f", "s16le", "-")
}

func (a *audioHTTPFixture) receivedFile(t *testing.T, data []byte, extension string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "received."+extension)
	if len(data) == 0 || len(data) > 16<<20 {
		t.Fatal("received audio exceeds its fixture bounds")
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal("write owned received audio")
	}
	return path
}

func (a *audioHTTPFixture) verifyMP3(t *testing.T, data []byte, seconds float64) {
	t.Helper()
	path := a.receivedFile(t, data, "mp3")
	var facts struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Type     string `json:"codec_type"`
			Bitrate  string `json:"bit_rate"`
			Channels int    `json:"channels"`
		} `json:"streams"`
	}
	encoded := hlsHTTPMediaCommand(t, a.ffprobe, "-v", "error", "-show_entries", "stream=codec_name,codec_type,bit_rate,channels", "-of", "json", path)
	if err := json.Unmarshal(encoded, &facts); err != nil || len(facts.Streams) != 1 {
		t.Fatal("received progressive audio has invalid stream facts")
	}
	bitrate, err := strconv.ParseInt(facts.Streams[0].Bitrate, 10, 64)
	if err != nil || bitrate <= 0 || bitrate > 128000 || facts.Streams[0].Codec != "mp3" || facts.Streams[0].Type != "audio" || facts.Streams[0].Channels != 2 {
		t.Fatal("progressive output did not honor the MP3 format and bitrate ceiling")
	}
	pcm := a.decodeFile(t, path)
	actual := float64(len(pcm)) / (48000 * 4)
	// Raw progressive MP3 has no rewritten Xing trim header. Permit only a
	// small encoder-frame allowance, never a missing source interval.
	if math.Abs(actual-seconds) > .08 {
		t.Fatalf("decoded progressive duration = %.6f, want %.6f seconds", actual, seconds)
	}
}

func TestHTTPAudioADTSUsesExactSamplesAndHLSTailHasNoPhantomSegment(t *testing.T) {
	a := newAudioHTTPFixture(t, false)
	before := a.history(t)
	adts := a.sources["adts"]
	referencePCM := a.decodeFile(t, adts.Path)
	samples := int64(len(referencePCM) / 4)
	var timing *media.AudioTiming
	for _, stream := range adts.Media.Streams {
		if stream.CodecType == "audio" {
			timing = stream.AudioTiming
		}
	}
	if adts.Media.ProbeVersion < 3 || timing == nil || !timing.Exact || timing.SampleCount != samples || timing.EndTicks != adts.Media.DurationTicks || timing.StartTicks != 0 ||
		math.Abs(float64(adts.Media.DurationTicks)-float64(samples)*float64(media.TicksPerSecond)/48000) > 1 {
		t.Fatal("ADTS catalog duration is not backed by the complete decoded sample count")
	}
	wav := a.request(t, http.MethodGet, a.universal("adts", url.Values{
		"PlaySessionId": {"exact-adts-reference"}, "Container": {"wav"}, "TranscodingContainer": {"wav"}, "AudioCodec": {"pcm_s16le"},
		"AudioSampleRate": {"48000"}, "AudioChannels": {"2"},
	}), nil, a.accounts.viewer.headers)
	expectHLSHTTPStatus(t, wav, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, wav.header, "audio/wav")
	converted := a.decodeFile(t, a.receivedFile(t, wav.body, "wav"))
	if len(converted) != len(referencePCM) {
		t.Fatalf("ADTS conversion truncated its exact tail: decoded bytes = %d, want %d", len(converted), len(referencePCM))
	}
	const reference = "short-tail-hls-reference"
	master := a.request(t, http.MethodGet, a.universal("tail", url.Values{
		"PlaySessionId": {reference}, "Container": {"mp3"}, "TranscodingProtocol": {"hls"}, "TranscodingContainer": {"ts"},
		"AudioCodec": {"aac"}, "AllowAudioStreamCopy": {"false"}, "SegmentLength": {"3"}, "MaxStreamingBitrate": {"128000"},
	}), nil, a.accounts.viewer.headers)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	mainURLs := hlsHTTPManifestChildren(master.body)
	if len(mainURLs) != 1 || bytes.Contains(master.body, []byte("RESOLUTION=")) {
		t.Fatal("audio Universal did not return one audio HLS variant")
	}
	canonical := a.canonical(t, a.accounts.viewer, reference, "tail")
	mainURL := hlsHTTPURL(t, mainURLs[0], a.accounts.viewer.headers.Get("X-Emby-Token"))
	if mainURL.Query().Get("PlaySessionId") != canonical {
		t.Fatal("audio HLS child did not use the scoped canonical playback identity")
	}
	main := a.request(t, http.MethodGet, mainURLs[0], nil, nil)
	expectHLSHTTPStatus(t, main, http.StatusOK)
	children := hlsHTTPManifestChildren(main.body)
	if len(children) != 2 || !bytes.Contains(main.body, []byte("#EXTINF:3.0000000,\n")) || !bytes.Contains(main.body, []byte("#EXTINF:3.0010000,\n")) ||
		!bytes.Contains(main.body, []byte("#EXT-X-MEDIA-SEQUENCE:0\n")) || !bytes.HasSuffix(main.body, []byte("#EXT-X-ENDLIST\n")) {
		t.Fatal("6.001-second audio HLS did not merge its unproducible short tail")
	}
	directory := t.TempDir()
	localManifest := string(main.body)
	for number, child := range children {
		hlsHTTPURL(t, child, a.accounts.viewer.headers.Get("X-Emby-Token"))
		segment := a.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, segment, http.StatusOK)
		if segment.header.Get("Content-Type") != "video/mp2t" || len(segment.body) < 188 {
			t.Fatal("advertised audio HLS segment is not available as MPEG-TS")
		}
		name := fmt.Sprintf("audio-%d.ts", number)
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, segment.body, 0o600); err != nil {
			t.Fatal("write owned audio HLS segment")
		}
		_ = a.decodeFile(t, path)
		localManifest = strings.ReplaceAll(localManifest, child, name)
	}
	phantom := hlsHTTPURL(t, children[len(children)-1], a.accounts.viewer.headers.Get("X-Emby-Token"))
	phantom.Path = phantom.Path[:strings.LastIndex(phantom.Path, "/")+1] + "2.ts"
	expectHLSHTTPStatus(t, a.request(t, http.MethodGet, phantom.String(), nil, nil), http.StatusNotFound)
	playlist := filepath.Join(directory, "complete.m3u8")
	if err := os.WriteFile(playlist, []byte(localManifest), 0o600); err != nil {
		t.Fatal("write owned complete audio HLS playlist")
	}
	pcm := a.decodeFile(t, playlist)
	if actual := float64(len(pcm)) / (48000 * 4); math.Abs(actual-6.001) > .08 {
		t.Fatalf("complete audio HLS decoded %.6f seconds, want 6.001", actual)
	}
	if a.history(t) != before {
		t.Fatal("ADTS and HLS media requests fabricated playback history")
	}
}

type audioHTTPLive struct {
	response *http.Response
	cancel   context.CancelFunc
	prefix   []byte
}

func (a *audioHTTPFixture) openLive(t *testing.T, reference string) audioHTTPLive {
	t.Helper()
	ctx, cancel := context.WithTimeout(a.f.ctx, 45*time.Second)
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.server.URL+a.universal("flac", audioHTTPMP3Query(reference, 0)), nil)
	if err != nil {
		t.Fatalf("create live audio request (%T)", err)
	}
	request.Header = a.accounts.viewer.headers.Clone()
	response, err := a.server.Client().Do(request)
	if err != nil {
		t.Fatalf("open live audio response (%T)", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("live audio status = %d", response.StatusCode)
	}
	assertAudioHTTPProgressiveHeaders(t, response.Header, "audio/mpeg")
	prefix := make([]byte, 256)
	if _, err := io.ReadFull(response.Body, prefix); err != nil {
		t.Fatalf("live audio did not publish actual output before interruption (%T)", err)
	}
	return audioHTTPLive{response: response, cancel: cancel, prefix: prefix}
}

func (a *audioHTTPFixture) liveSession(t *testing.T, playID string, readers int) (*hlsSession, transcode.Record) {
	t.Helper()
	a.f.app.hls.mu.Lock()
	var selected *hlsSession
	for _, session := range a.f.app.hls.sessions {
		if session.key.scope.AuthSessionID == a.accounts.viewer.id && session.key.scope.PlaySessionID == playID {
			if selected != nil {
				a.f.app.hls.mu.Unlock()
				t.Fatal("one live audio plan acquired multiple runtime revisions")
			}
			selected = session
		}
	}
	a.f.app.hls.mu.Unlock()
	if selected == nil {
		t.Fatal("live audio revision was not registered")
	}
	selected.mu.Lock()
	count, closed := selected.progressiveReaders, selected.closed
	producers := append([]hlsProducer(nil), selected.producers...)
	selected.mu.Unlock()
	if count != readers || closed || len(producers) != 1 {
		t.Fatalf("live audio consumer count = %d, want %d", count, readers)
	}
	record, err := a.f.app.hls.manager.Snapshot(selected.key.scope, producers[0].id)
	if err != nil || record.State != "running" || a.jobs(t, a.accounts.viewer, playID, true) != 1 {
		t.Fatalf("interruption window did not contain a running real encoder (%T)", err)
	}
	return selected, record
}

func audioHTTPWait(t *testing.T, description string, ready func() bool) {
	t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal(description)
		}
	}
}

func (a *audioHTTPFixture) retiredAudio(t *testing.T, playID string, session *hlsSession) {
	t.Helper()
	pids := a.encoderPIDs(t)
	if len(pids) == 0 {
		t.Fatal("no real audio encoder process was recorded")
	}
	session.mu.Lock()
	producers := append([]hlsProducer(nil), session.producers...)
	session.mu.Unlock()
	audioHTTPWait(t, "audio cancellation did not release readers, cache files, HTTP slots, registry, and real workers", func() bool {
		a.f.app.hls.mu.Lock()
		_, exists := a.f.app.hls.sessions[session.id]
		a.f.app.hls.mu.Unlock()
		session.mu.Lock()
		readers := session.progressiveReaders
		session.mu.Unlock()
		if exists || readers != 0 || len(a.f.app.streamSlots) != 0 || a.jobs(t, a.accounts.viewer, playID, true) != 0 {
			return false
		}
		for _, pid := range pids {
			if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); !errors.Is(err, os.ErrNotExist) {
				return false
			}
		}
		// Reclamation requires the manager's file-reader reservations to reach
		// zero after its process has exited; runtime counters alone cannot
		// establish that those lower-level cache pins have been released.
		for _, producer := range producers {
			if _, err := os.Stat(filepath.Join(a.f.app.cfg.Transcoding.CacheDirectory, producer.id)); !errors.Is(err, os.ErrNotExist) {
				return false
			}
		}
		return true
	})
}

func TestHTTPAudioStopAfterHeadersAbortsTransportAndCleansWorkers(t *testing.T) {
	a := newAudioHTTPFixture(t, true)
	before := a.history(t)
	const reference = "running-stop-reference"
	live := a.openLive(t, reference)
	playID := a.canonical(t, a.accounts.viewer, reference, "flac")
	session, _ := a.liveSession(t, playID, 1)
	// Exercise the public cleanup API with the original client nonce. The
	// separate scoped lookup above establishes its actual internal identity.
	stop := "/emby/Videos/ActiveEncodings?" + url.Values{"DeviceId": {a.accounts.viewer.deviceID}, "PlaySessionId": {reference}}.Encode()
	expectHLSHTTPStatus(t, a.request(t, http.MethodDelete, stop, nil, a.accounts.viewer.headers), http.StatusNoContent)
	_, err := io.ReadAll(io.LimitReader(live.response.Body, 1<<20))
	if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("server-side cancellation after headers did not abort the HTTP body (%T)", err)
	}
	_ = live.response.Body.Close()
	live.cancel()
	a.retiredAudio(t, playID, session)
	if a.history(t) != before {
		t.Fatal("stopping an audio encoder fabricated playback history")
	}
}

func TestHTTPAudioLastClientCancellationReleasesLiveProducer(t *testing.T) {
	a := newAudioHTTPFixture(t, true)
	before := a.history(t)
	const reference = "last-reader-reference"
	live := a.openLive(t, reference)
	playID := a.canonical(t, a.accounts.viewer, reference, "flac")
	session, _ := a.liveSession(t, playID, 1)
	live.cancel()
	_ = live.response.Body.Close()
	a.retiredAudio(t, playID, session)
	if a.history(t) != before {
		t.Fatal("client audio cancellation fabricated playback history")
	}
}

func TestHTTPAudioConcurrentConsumerSurvivesAnotherClientClosing(t *testing.T) {
	a := newAudioHTTPFixture(t, true)
	before := a.history(t)
	const reference = "shared-live-consumers"
	first := a.openLive(t, reference)
	playID := a.canonical(t, a.accounts.viewer, reference, "flac")
	_, initial := a.liveSession(t, playID, 1)
	second := a.openLive(t, reference)
	session, concurrent := a.liveSession(t, playID, 2)
	if initial.ID != concurrent.ID || len(a.encoderPIDs(t)) != 1 {
		t.Fatal("concurrent audio consumers launched duplicate producers")
	}
	first.cancel()
	_ = first.response.Body.Close()
	audioHTTPWait(t, "departing audio consumer did not release its request lease", func() bool {
		session.mu.Lock()
		defer session.mu.Unlock()
		return session.progressiveReaders == 1
	})
	_, remaining := a.liveSession(t, playID, 1)
	if remaining.ID != initial.ID {
		t.Fatal("remaining audio reader was silently moved to another producer")
	}
	rest, err := io.ReadAll(io.LimitReader(second.response.Body, 1<<20))
	if err != nil || len(rest) >= 1<<20 {
		t.Fatalf("another consumer closing interrupted the surviving stream (%T)", err)
	}
	_ = second.response.Body.Close()
	second.cancel()
	a.verifyMP3(t, append(append([]byte(nil), second.prefix...), rest...), 6)
	audioHTTPWait(t, "completed audio consumer did not release its reader lease", func() bool {
		session.mu.Lock()
		readers, closed := session.progressiveReaders, session.closed
		session.mu.Unlock()
		record, err := a.f.app.hls.manager.Snapshot(session.key.scope, initial.ID)
		return readers == 0 && !closed && err == nil && record.State == "completed" && len(a.f.app.streamSlots) == 0
	})
	if a.history(t) != before {
		t.Fatal("concurrent audio GETs fabricated playback history")
	}
}
