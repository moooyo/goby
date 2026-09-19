//go:build linux && goby_browser_integration

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const phase2BrowserBundleSHA256 = "04a55387b26d6becff5b87b470b9b19c3fe41d25c0cd3c1e6c3d885a56572fdb"

type phase2BrowserCue struct {
	StreamIndex              int
	Language, CueText        string
	StartSeconds, EndSeconds float64
}

type phase2BrowserFinite struct {
	PlaybackURL, ObservePath, StopPath  string
	Tracks                              []phase2BrowserCue
	ProbeSeconds, TimelineOffsetSeconds float64
	PixelRGB                            [3]int
	OffsetTicks                         int64
}

type phase2BrowserDynamic struct {
	PlaybackURL, ObservePath, StopPath string
	DisconnectPath, ReconnectPath      string
	SubtitleLanguage, CuePrefix        string
	PauseSeconds                       float64
	ExpectedExpiredStatus              int
}

type phase2BrowserContext struct {
	Marker                                                  string
	RunID                                                   string `json:"RunId"`
	BaseURL, Token, HlsBundlePath, ArtifactsDir, ResultPath string
	Finite                                                  phase2BrowserFinite
	Dynamic                                                 phase2BrowserDynamic
}

type phase2BrowserObservation struct {
	ProducerIDs                      []string `json:"ProducerIds"`
	ProducerStates                   []string
	ActiveJobs                       int
	Epoch, MediaSequence             uint64
	WindowStartTicks, WindowEndTicks int64
}

type phase2BrowserUpstream struct {
	d             *dynamicTimeshiftHTTPFixture
	ctx           context.Context
	cancel        context.CancelFunc
	server        *httptest.Server
	resume        chan struct{}
	resumeOnce    sync.Once
	firstExited   chan struct{}
	firstExitOnce sync.Once
	workers       sync.WaitGroup
	active        atomic.Int32
	mu            sync.Mutex
	pids          []int
	closed        sync.Once
}

type phase2BrowserFlushWriter struct{ writer http.ResponseWriter }

func (out phase2BrowserFlushWriter) Write(data []byte) (int, error) {
	n, err := out.writer.Write(data)
	if err == nil {
		err = http.NewResponseController(out.writer).Flush()
	}
	return n, err
}

// A real FFmpeg demuxer paces the owned source, preserving growing media PTS
// across loops. Disconnect kills that exact process and closes its HTTP body;
// a subsequent authorized connection cannot start until the resume gate opens.
func newPhase2BrowserUpstream(t *testing.T, d *dynamicTimeshiftHTTPFixture) *phase2BrowserUpstream {
	t.Helper()
	ctx, cancel := context.WithCancel(d.h.f.ctx)
	upstream := &phase2BrowserUpstream{d: d, ctx: ctx, cancel: cancel, resume: make(chan struct{}), firstExited: make(chan struct{})}
	upstream.server = httptest.NewServer(http.HandlerFunc(upstream.serve))
	t.Cleanup(upstream.close)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := d.h.f.app.closeDynamicSources(closeCtx)
	closeCancel()
	if err != nil {
		t.Fatal("close unused dynamic runtime before the paced browser fixture")
	}
	d.upstream.Close()
	d.upstream = upstream.server
	definition := &d.h.f.app.cfg.DynamicSources[0]
	definition.URL = upstream.server.URL + "/media"
	for index := range definition.Subtitles {
		definition.Subtitles[index].URL = upstream.server.URL + "/subtitle/" + strconv.Itoa(index)
	}
	// Reinitialization restores the production wall clock. The deterministic
	// retention clock used by the ordinary HTTP fixture is not browser evidence.
	if err := d.h.f.app.initializeDynamicSources(d.h.f.ctx); err != nil {
		t.Fatal("initialize the paced browser source")
	}
	d.h.f.cfg = d.h.f.app.cfg
	return upstream
}

func (upstream *phase2BrowserUpstream) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer private-dynamic-fixture" {
		upstream.d.badAuthorization.Store(true)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/subtitle/") {
		index, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/subtitle/"))
		if err != nil || index < 0 || index >= len(upstream.d.subtitleRequests) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		upstream.d.subtitleRequests[index].Add(1)
		w.Header().Set("Content-Type", "text/vtt")
		_, _ = fmt.Fprintf(w, "WEBVTT\n\n00:00:00.000 --> 00:10:00.000\ncaption-track-%d\n\n", index)
		return
	}
	if r.URL.Path != "/media" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	upstream.workers.Add(1)
	defer upstream.workers.Done()
	number := upstream.d.mediaRequests.Add(1)
	if number > 1 {
		select {
		case <-upstream.resume:
		case <-upstream.ctx.Done():
			return
		case <-r.Context().Done():
			return
		}
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(upstream.ctx, cancel)
	defer func() { stop(); cancel() }()
	if number == 1 {
		defer upstream.firstExitOnce.Do(func() { close(upstream.firstExited) })
		watchDone := make(chan struct{})
		defer func() { cancel(); <-watchDone }()
		go func() {
			defer close(watchDone)
			select {
			case <-upstream.d.firstEOF:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	command := exec.CommandContext(ctx, upstream.d.h.ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "error",
		"-threads", "1", "-stream_loop", "-1", "-readrate", "1", "-readrate_initial_burst", "3", "-i", upstream.d.h.path,
		"-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-muxrate", "600000", "-mpegts_flags", "+resend_headers", "-f", "mpegts", "pipe:1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 2 * time.Second
	command.Stdout = phase2BrowserFlushWriter{writer: w}
	command.Stderr = io.Discard
	w.Header().Set("Content-Type", "video/mp2t")
	if err := command.Start(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	upstream.active.Add(1)
	upstream.mu.Lock()
	upstream.pids = append(upstream.pids, command.Process.Pid)
	upstream.mu.Unlock()
	defer upstream.active.Add(-1)
	_ = command.Wait()
}

func (upstream *phase2BrowserUpstream) close() {
	upstream.closed.Do(func() {
		upstream.cancel()
		upstream.server.CloseClientConnections()
		upstream.server.Close()
		upstream.workers.Wait()
	})
}

type phase2BrowserObserver struct {
	mu                         sync.Mutex
	jobs                       map[string]bool
	d                          *dynamicTimeshiftHTTPFixture
	upstream                   *phase2BrowserUpstream
	finiteItem                 library.Item
	finitePlay, finiteRevision string
	dynamic                    dynamicHTTPPresentation
}

func (observer *phase2BrowserObserver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := observer.d.h
	principal, ok := r.Context().Value(principalKey).(identity.Principal)
	if !ok || principal.IsApplicationKey() || principal.User.ID != h.accounts.viewer.userID || principal.SessionID != h.accounts.viewer.id || principal.Client.DeviceID != h.accounts.viewer.deviceID {
		http.Error(w, "unknown owned fixture", http.StatusNotFound)
		return
	}
	kind := r.URL.Query().Get("Kind")
	playID, itemID := observer.finitePlay, observer.finiteItem.ID
	if kind == "Dynamic" {
		playID, itemID = observer.dynamic.playID, h.item.ID
	} else if kind != "Finite" {
		http.Error(w, "unknown fixture", http.StatusNotFound)
		return
	}
	if r.URL.Query().Get("PlaySessionId") != playID {
		http.Error(w, "unknown playback", http.StatusNotFound)
		return
	}
	play, err := h.f.app.library.GetPlaybackSession(r.Context(), playbackOwner(principal), playID)
	if err != nil || play.ItemID != itemID || play.MediaSourceID != media.SourceID(itemID) {
		http.Error(w, "unknown owned playback", http.StatusNotFound)
		return
	}
	result := phase2BrowserObservation{ProducerIDs: []string{}, ProducerStates: []string{}}
	if kind == "Finite" {
		if r.Method != http.MethodGet || r.URL.Path != "/__phase2-observe" {
			http.Error(w, "unknown observation", http.StatusNotFound)
			return
		}
		for _, session := range videoHTTPSessions(h, playID) {
			if session.id != observer.finiteRevision || session.key.scope.ItemID != itemID || session.key.scope.SourceID != play.MediaSourceID {
				continue
			}
			file, _, err := h.f.app.authorizeHLS(r.Context(), principal, session.key.scope, session.key.stamp, session.key.plan)
			if err != nil {
				http.Error(w, "source observation denied", http.StatusNotFound)
				return
			}
			_ = file.Close()
			session.mu.Lock()
			producers := append([]hlsProducer(nil), session.producers...)
			session.mu.Unlock()
			for _, producer := range producers {
				record, err := h.f.app.hls.manager.Snapshot(session.key.scope, producer.id)
				if err != nil {
					http.Error(w, "producer observation unavailable", http.StatusServiceUnavailable)
					return
				}
				result.ProducerIDs = append(result.ProducerIDs, record.ID)
				result.ProducerStates = append(result.ProducerStates, record.State)
				if record.State == "queued" || record.State == "running" {
					result.ActiveJobs++
				}
			}
		}
		result.WindowEndTicks = observer.finiteItem.Media.DurationTicks
	} else {
		fresh, err := h.f.app.freshDynamicPrincipal(r.Context(), principal)
		if err != nil {
			http.Error(w, "current fixture identity unavailable", http.StatusNotFound)
			return
		}
		h.f.app.dynamicStreams.mu.Lock()
		session := h.f.app.dynamicStreams.sessions[observer.dynamic.presentationID]
		h.f.app.dynamicStreams.mu.Unlock()
		if session == nil || session.key.owner != dynamicSourceOwner(fresh).Identity() || session.key.liveID != observer.dynamic.liveID ||
			session.scope.ItemID != itemID || session.scope.PlaySessionID != playID || session.scope.SourceID != play.MediaSourceID {
			http.Error(w, "dynamic observation denied", http.StatusNotFound)
			return
		}
		limits, err := h.f.app.dynamicLimits(r.Context(), fresh, session.request, r)
		if err != nil || !hlsPlanAllowed(session.key.plan, limits) || h.f.app.checkMediaPolicy(fresh, session.scope) != nil {
			http.Error(w, "current dynamic policy denied", http.StatusNotFound)
			return
		}
		file, _, err := h.f.app.library.OpenMediaFor(r.Context(), librarySubject(fresh, fresh.User.ID), itemID, play.MediaSourceID)
		if err != nil {
			http.Error(w, "current source access denied", http.StatusNotFound)
			return
		}
		_ = file.Close()
		session.mu.Lock()
		closed := session.closed
		if !closed {
			session.accessed = time.Now()
		}
		session.mu.Unlock()
		if closed {
			http.Error(w, "dynamic presentation retired", http.StatusNotFound)
			return
		}
		// Do not invoke source Info/Acquire here. A real connection is allowed
		// to be reopening while this authenticated consumer observes retention
		// or releases the fixture's resume gate.
		if r.URL.Path == "/__phase2-disconnect" || r.URL.Path == "/__phase2-reconnect" {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path == "/__phase2-disconnect" {
				observer.d.endFirst.Do(func() { close(observer.d.firstEOF) })
				select {
				case <-observer.upstream.firstExited:
				case <-r.Context().Done():
					return
				case <-time.After(5 * time.Second):
					http.Error(w, "upstream closure timed out", http.StatusServiceUnavailable)
					return
				}
			} else {
				select {
				case <-observer.upstream.firstExited:
				default:
					http.Error(w, "upstream has not disconnected", http.StatusConflict)
					return
				}
				observer.upstream.resumeOnce.Do(func() { close(observer.upstream.resume) })
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/__phase2-observe" {
			http.Error(w, "unknown observation", http.StatusNotFound)
			return
		}
		// This is an authenticated real consumer observation, not publisher work.
		if err := h.f.app.dynamicStreams.store.Touch(r.Context(), timeshiftScope(session.scope), session.windowID); err != nil {
			http.Error(w, "window expired", http.StatusNotFound)
			return
		}
		window, err := h.f.app.dynamicStreams.store.Snapshot(r.Context(), timeshiftScope(session.scope), session.windowID)
		if err != nil {
			http.Error(w, "window observation unavailable", http.StatusServiceUnavailable)
			return
		}
		result.WindowStartTicks, result.WindowEndTicks = window.EarliestTicks, window.LiveEdgeTicks
		result.MediaSequence = window.NextSequence
		if len(window.Segments) > 0 {
			result.MediaSequence = window.Segments[0].Sequence
		}
		if len(window.Epochs) > 0 {
			result.Epoch = window.Epochs[len(window.Epochs)-1].Generation
		}
		session.mu.Lock()
		jobID := session.jobID
		session.mu.Unlock()
		if jobID != "" {
			record, err := h.f.app.hls.manager.Snapshot(session.scope, jobID)
			if err != nil {
				http.Error(w, "dynamic producer observation unavailable", http.StatusServiceUnavailable)
				return
			}
			result.ProducerIDs, result.ProducerStates = append(result.ProducerIDs, record.ID), append(result.ProducerStates, record.State)
			if record.State == "queued" || record.State == "running" {
				result.ActiveJobs++
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	observer.mu.Lock()
	for _, id := range result.ProducerIDs {
		observer.jobs[id] = true
	}
	observer.mu.Unlock()
	_ = json.NewEncoder(w).Encode(result)
}

func phase2BrowserPrivateDirectory(t *testing.T, directory string) {
	t.Helper()
	actual, err := filepath.EvalSymlinks(directory)
	info, statErr := os.Lstat(directory)
	if err != nil || statErr != nil || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || actual != directory || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("phase 2 browser directory must be an existing canonical private directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		t.Fatal("phase 2 browser directory must be owned by the fixture process")
	}
}

func phase2BrowserWriteJSON(t *testing.T, path string, value any) os.FileInfo {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(data) > 512<<10 {
		t.Fatal("phase 2 private artifact is not bounded JSON")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("phase 2 private artifact path must be new")
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		t.Fatal("write phase 2 private artifact")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal("flush phase 2 private artifact")
	}
	info, err := file.Stat()
	if closeErr := file.Close(); err != nil || closeErr != nil {
		t.Fatal("close phase 2 private artifact")
	}
	return info
}

func phase2BrowserReadPrivate(path string, maximum int64, target any) error {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(descriptor), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > maximum {
		return errors.New("invalid private browser artifact")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return errors.New("private browser artifact owner differs")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// Capture only owned job IDs, technical states and byte/count evidence. Never
// serialize credentials, playback URLs, source paths, plans, or stderr here.
func phase2BrowserCleanupDetails(h *hlsHTTPFixture, finitePlay, dynamicPlay string, jobIDs []string) map[string]any {
	result := map[string]any{"CapturedBeforeFixtureClose": true, "CapturedAt": time.Now().UTC(), "CacheDirectories": []map[string]any{}, "DurableJobs": []map[string]any{}}
	var directories []map[string]any
	for _, id := range jobIDs {
		entry := map[string]any{"ID": id, "Exists": false}
		path := filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, id)
		info, err := os.Lstat(path)
		if err == nil {
			entry["Exists"], entry["IsDirectory"], entry["IsSymlink"] = true, info.IsDir(), info.Mode()&os.ModeSymlink != 0
			if info.IsDir() {
				children, readErr := os.ReadDir(path)
				if readErr != nil {
					entry["ReadErrorType"] = fmt.Sprintf("%T", readErr)
				} else {
					var bytes int64
					failures := 0
					for _, child := range children {
						childInfo, infoErr := child.Info()
						if infoErr != nil {
							failures++
							continue
						}
						if childInfo.Mode().IsRegular() {
							bytes += childInfo.Size()
						}
					}
					entry["DirectEntries"], entry["DirectFileBytes"], entry["EntryStatFailures"] = len(children), bytes, failures
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			entry["StatErrorType"] = fmt.Sprintf("%T", err)
		}
		directories = append(directories, entry)
	}
	result["CacheDirectories"] = directories
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := h.f.pool.Query(ctx, `SELECT id,state,error_code,output_bytes,play_session_id FROM encoding_jobs
		WHERE auth_session_id=$1 AND play_session_id IN ($2,$3) ORDER BY id`, h.accounts.viewer.id, finitePlay, dynamicPlay)
	if err != nil {
		result["DurableJobReadErrorType"] = fmt.Sprintf("%T", err)
		return result
	}
	defer rows.Close()
	var jobs []map[string]any
	for rows.Next() {
		var id, state, code, play string
		var bytes int64
		if err := rows.Scan(&id, &state, &code, &bytes, &play); err != nil {
			result["DurableJobReadErrorType"] = fmt.Sprintf("%T", err)
			break
		}
		kind := "Dynamic"
		if play == finitePlay {
			kind = "Finite"
		}
		jobs = append(jobs, map[string]any{"ID": id, "Kind": kind, "State": state, "ErrorCode": code, "OutputBytes": bytes})
	}
	if err := rows.Err(); err != nil {
		result["DurableJobReadErrorType"] = fmt.Sprintf("%T", err)
	}
	result["DurableJobs"] = jobs
	return result
}

func phase2PrepareFinite(t *testing.T, h *hlsHTTPFixture, directory string) (library.Item, string, string, phase2BrowserFinite) {
	t.Helper()
	path := filepath.Join(filepath.Dir(h.path), "Phase2.Finite.Captions.mp4")
	data, err := os.ReadFile(h.path)
	if err != nil || len(data) > 4<<20 {
		t.Fatal("read bounded owned finite source")
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	stem := strings.TrimSuffix(path, filepath.Ext(path))
	for _, language := range []string{"en", "fr", "de", "es", "it", "pt", "ja", "ko"} {
		text := "1\n00:00:03,000 --> 00:00:08,000\nphase2-caption-" + language + "\n"
		if err := os.WriteFile(stem+"."+language+".srt", []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
	items, err := h.f.app.library.QueryItems(h.f.ctx, library.Query{UserID: h.accounts.viewer.userID, ParentID: h.libraryID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var item library.Item
	for _, candidate := range items.Items {
		if candidate.Path == path {
			item = candidate
		}
	}
	if item.ID == "" || item.Media == nil || len(item.Subtitles) != 8 {
		t.Fatal("finite source did not index eight text tracks")
	}
	tracks := make([]phase2BrowserCue, 0, 8)
	for _, track := range item.Subtitles {
		tracks = append(tracks, phase2BrowserCue{StreamIndex: track.Index, Language: track.Language, CueText: "phase2-caption-" + track.Language, StartSeconds: 3, EndSeconds: 8})
	}
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].StreamIndex < tracks[j].StreamIndex })
	response := h.request(t, http.MethodPost, "/emby/Items/"+item.ID+"/PlaybackInfo", map[string]any{
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true, "AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false,
		"SubtitleStreamIndex": tracks[0].StreamIndex, "DeviceProfile": map[string]any{
			"TranscodingProfiles": []map[string]any{{"Type": "Video", "Container": "mp4", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac", "SegmentLength": 3, "ManifestSubtitles": "vtt", "MaxManifestSubtitles": 8}},
			"SubtitleProfiles":    []map[string]any{{"Format": "vtt", "Method": "Hls", "Container": "mp4", "Protocol": "hls"}},
		}}, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var prepared struct {
		PlaySessionID string
		MediaSources  []struct{ TranscodingURL string }
	}
	if json.Unmarshal(response.body, &prepared) != nil || prepared.PlaySessionID == "" || len(prepared.MediaSources) != 1 || prepared.MediaSources[0].TranscodingURL == "" {
		t.Fatal("finite browser source lacks canonical negotiation")
	}
	masterURL := prepared.MediaSources[0].TranscodingURL
	parsed := hlsHTTPURL(t, masterURL, h.accounts.viewer.headers.Get("X-Emby-Token"))
	revision := parsed.Query().Get("GobyHlsId")
	if revision == "" {
		t.Fatal("finite browser source has no immutable HLS revision")
	}
	master := h.request(t, http.MethodGet, masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	variants := hlsHTTPManifestChildren(master.body)
	if len(variants) != 1 || len(hlsSubtitleRenditionURLs(t, master.body)) != 8 {
		t.Fatal("finite browser source lacks its complete actual rendition graph")
	}
	var playlist hlsHTTPResponse
	for attempt := 0; attempt < 200; attempt++ {
		playlist = h.request(t, http.MethodGet, variants[0], nil, nil)
		expectHLSHTTPStatus(t, playlist, http.StatusOK)
		if strings.Contains(string(playlist.body), "#EXT-X-ENDLIST") {
			break
		}
		select {
		case <-time.After(25 * time.Millisecond):
		case <-h.f.ctx.Done():
			t.Fatal("finite fixture production exceeded its deadline")
		}
	}
	if !strings.Contains(string(playlist.body), "#EXT-X-ENDLIST") {
		t.Fatal("finite fixture production did not complete")
	}
	frames := hlsSubtitleActualFramePTS(t, h, playlist.body, "mp4")
	var sourceFrames struct {
		Frames []struct {
			PTS string `json:"best_effort_timestamp_time"`
		}
	}
	probe := hlsHTTPMediaCommand(t, h.ffprobe, "-v", "error", "-select_streams", "v:0", "-read_intervals", "%+#1", "-show_frames", "-show_entries", "frame=best_effort_timestamp_time", "-of", "json", path)
	if json.Unmarshal(probe, &sourceFrames) != nil || len(sourceFrames.Frames) != 1 {
		t.Fatal("source media lacks an independently measured initial frame")
	}
	sourcePTS, err := strconv.ParseFloat(sourceFrames.Frames[0].PTS, 64)
	if err != nil || math.IsNaN(sourcePTS) || math.IsInf(sourcePTS, 0) {
		t.Fatal("source media clock is not finite")
	}
	offset := frames[0] - sourcePTS
	digest := sha256.Sum256(data)
	phase2BrowserWriteJSON(t, filepath.Join(directory, "finite-clock-evidence.json"), map[string]any{
		"SourceSHA256": hex.EncodeToString(digest[:]), "SourceFirstFramePTS": sourcePTS, "OutputFramePTS": frames,
		"TimelineOffsetSeconds": offset, "ProbeSourceSeconds": 4.25, "ExpectedRGB": []int{0, 128, 0}, "SourceFacts": "24 fps; green interval [3,6); captions [3,8)"})
	finite := phase2BrowserFinite{PlaybackURL: masterURL, ObservePath: "/__phase2-observe?Kind=Finite&PlaySessionId=" + url.QueryEscape(prepared.PlaySessionID),
		StopPath: videoHTTPStopPath(h.accounts.viewer, prepared.PlaySessionID), Tracks: tracks, ProbeSeconds: 4.25, TimelineOffsetSeconds: offset, PixelRGB: [3]int{0, 128, 0}, OffsetTicks: 5000000}
	return item, prepared.PlaySessionID, revision, finite
}

func TestPhase2HLSBrowserFixtureServer(t *testing.T) {
	directory := os.Getenv("GOBY_PHASE2_BROWSER_DIRECTORY")
	if directory == "" {
		t.Skip("GOBY_PHASE2_BROWSER_DIRECTORY explicitly admits the phase 2 browser fixture")
	}
	if os.Getenv("GOBY_TEST_DATABASE_URL") == "" || os.Getenv("GOBY_FFMPEG") == "" || os.Getenv("GOBY_FFPROBE") == "" {
		t.Fatal("phase 2 browser fixture requires its owned database and actual media tools")
	}
	phase2BrowserPrivateDirectory(t, directory)
	runID := os.Getenv("GOBY_PHASE2_BROWSER_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("phase 2 browser run ID is required")
	}
	bundlePath := os.Getenv("GOBY_PHASE2_HLS_BUNDLE")
	actual, err := filepath.EvalSymlinks(bundlePath)
	if err != nil || !filepath.IsAbs(bundlePath) || actual != bundlePath {
		t.Fatal("the reviewed HLS engine path must be canonical")
	}
	bundle, err := os.ReadFile(bundlePath)
	digest := sha256.Sum256(bundle)
	if err != nil || len(bundle) > 8<<20 || hex.EncodeToString(digest[:]) != phase2BrowserBundleSHA256 {
		t.Fatal("the retained HLS engine does not match the reviewed bundle")
	}
	contextPath, resultPath, stopPath := filepath.Join(directory, "context.json"), filepath.Join(directory, "browser-result.json"), filepath.Join(directory, "owned-stop.json")
	for _, path := range []string{contextPath, resultPath, stopPath, filepath.Join(directory, "fixture-result.json"), filepath.Join(directory, "finite-clock-evidence.json"), filepath.Join(directory, "fixture-cleanup-before-close.json")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("phase 2 browser run artifacts must be new")
		}
	}
	d := newDynamicTimeshiftHTTPFixture(t, true, 240*time.Second)
	upstream := newPhase2BrowserUpstream(t, d)
	defer upstream.close()
	h := d.h
	finiteItem, finitePlay, finiteRevision, finite := phase2PrepareFinite(t, h, directory)
	presentation := d.open(t)
	observer := &phase2BrowserObserver{d: d, upstream: upstream, jobs: make(map[string]bool), finiteItem: finiteItem, finitePlay: finitePlay, finiteRevision: finiteRevision, dynamic: presentation}
	product := h.f.app.Handler()
	private := h.f.app.requireEmby(observer.ServeHTTP)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__phase2-") {
			private(w, r)
			return
		}
		product.ServeHTTP(w, r)
	}))
	defer func() { server.CloseClientConnections(); server.Close() }()
	h.server.Close()
	h.server = server
	fixture := phase2BrowserContext{Marker: "goby-phase2-hls-browser-fixture-v1", RunID: runID, BaseURL: server.URL,
		Token: h.accounts.viewer.headers.Get("X-Emby-Token"), HlsBundlePath: bundlePath, ArtifactsDir: directory, ResultPath: resultPath, Finite: finite,
		Dynamic: phase2BrowserDynamic{PlaybackURL: presentation.masterURL, ObservePath: "/__phase2-observe?Kind=Dynamic&PlaySessionId=" + url.QueryEscape(presentation.playID),
			StopPath: videoHTTPStopPath(h.accounts.viewer, presentation.playID), DisconnectPath: "/__phase2-disconnect?Kind=Dynamic&PlaySessionId=" + url.QueryEscape(presentation.playID),
			ReconnectPath: "/__phase2-reconnect?Kind=Dynamic&PlaySessionId=" + url.QueryEscape(presentation.playID), SubtitleLanguage: "eng", CuePrefix: "caption-track-", PauseSeconds: 3, ExpectedExpiredStatus: http.StatusNotFound}}
	ownedContext := phase2BrowserWriteJSON(t, contextPath, fixture)
	defer func() {
		if current, err := os.Lstat(contextPath); err == nil && os.SameFile(ownedContext, current) {
			if err := os.Remove(contextPath); err != nil {
				t.Error("remove private phase 2 browser context")
			}
		}
	}()
	stopped, ownedResourcesRetired := false, false
	var cleanupEvidence map[string]any
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := h.f.app.closeDynamicSources(ctx); err != nil {
			t.Error("close phase 2 dynamic sources")
		}
		if err := h.f.app.hls.Close(ctx); err != nil {
			t.Error("close phase 2 media manager")
		}
		upstream.close()
		upstream.mu.Lock()
		pids := append([]int(nil), upstream.pids...)
		upstream.mu.Unlock()
		remaining := 0
		for _, pid := range pids {
			if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); !errors.Is(err, os.ErrNotExist) {
				remaining++
			}
		}
		if upstream.active.Load() != 0 || remaining != 0 {
			t.Error("phase 2 upstream processes did not retire")
		}
		if cleanupEvidence != nil {
			phase2BrowserWriteJSON(t, filepath.Join(directory, "fixture-cleanup-before-close.json"), cleanupEvidence)
		}
		phase2BrowserWriteJSON(t, filepath.Join(directory, "fixture-result.json"), map[string]any{"Marker": "goby-phase2-hls-browser-fixture-result-v1", "RunId": runID,
			"OwnedStopObserved": stopped, "ResourcesRetiredBeforeFixtureClose": ownedResourcesRetired, "GoTestFailed": t.Failed(), "UpstreamProcesses": pids, "RemainingUpstreamProcesses": remaining, "UpstreamConnections": d.mediaRequests.Load(),
			"UpstreamAuthorizationPreserved": !d.badAuthorization.Load(), "HlsBundleSHA256": phase2BrowserBundleSHA256, "CleanupBeforeFixtureClose": cleanupEvidence})
	}()
	t.Log("phase2_browser_fixture_ready=true private_context=context.json stop_marker=owned-stop.json maximum_runtime_seconds=240")
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for !stopped {
		select {
		case <-h.f.ctx.Done():
			t.Fatal("phase 2 browser fixture reached its bounded lifetime before an owned stop")
		case <-ticker.C:
			var stop struct {
				Marker string
				RunID  string `json:"RunId"`
			}
			err := phase2BrowserReadPrivate(stopPath, 4096, &stop)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || stop.Marker != "goby-phase2-hls-browser-stop-v1" || stop.RunID != runID {
				t.Fatal("phase 2 browser stop marker is invalid")
			}
			stopped = true
		}
	}
	var result struct {
		Marker        string
		RunID         string `json:"RunId"`
		Complete      bool
		CleanupFailed bool
	}
	if err := phase2BrowserReadPrivate(resultPath, 512<<10, &result); err != nil || result.Marker != "goby-phase2-hls-browser-result-v1" || result.RunID != runID || !result.Complete || result.CleanupFailed {
		t.Fatal("the phase 2 browser did not complete its actual media acceptance")
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	cleanupStarted := time.Now()
	for !ownedResourcesRetired {
		h.f.app.dynamicStreams.mu.Lock()
		dynamicCount := len(h.f.app.dynamicStreams.sessions)
		h.f.app.dynamicStreams.mu.Unlock()
		usage := h.f.app.dynamicStreams.store.Usage()
		cacheRetired := true
		var knownJobs, residualCaches []string
		cacheStatErrors := make(map[string]string)
		observer.mu.Lock()
		for id := range observer.jobs {
			knownJobs = append(knownJobs, id)
			if _, err := os.Stat(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, id)); !errors.Is(err, os.ErrNotExist) {
				cacheRetired = false
				residualCaches = append(residualCaches, id)
				if err != nil {
					cacheStatErrors[id] = fmt.Sprintf("%T", err)
				}
			}
		}
		observer.mu.Unlock()
		sort.Strings(knownJobs)
		sort.Strings(residualCaches)
		var activeJobs int
		if err := h.f.pool.QueryRow(h.f.ctx, `SELECT count(*) FROM encoding_jobs WHERE auth_session_id=$1 AND play_session_id IN ($2,$3) AND state IN ('queued','running')`,
			h.accounts.viewer.id, finitePlay, presentation.playID).Scan(&activeJobs); err != nil {
			t.Fatal("read owned browser producer cleanup")
		}
		ownedResourcesRetired = cacheRetired && len(videoHTTPSessions(h, finitePlay)) == 0 && dynamicCount == 0 && activeJobs == 0 && usage.Windows == 0 && usage.Bytes == 0 && usage.Readers == 0 && usage.Publishing == 0 && upstream.active.Load() == 0 && len(h.f.app.streamSlots) == 0
		cleanupEvidence = map[string]any{"Marker": "goby-phase2-browser-cleanup-observation-v1", "RunId": runID,
			"ObservedAt": time.Now().UTC(), "ElapsedMilliseconds": time.Since(cleanupStarted).Milliseconds(), "PredicatePassed": ownedResourcesRetired,
			"CacheRetired": cacheRetired, "ObservedProducerIds": knownJobs, "ResidualCacheProducerIds": residualCaches, "CacheStatErrors": cacheStatErrors,
			"FiniteSessions": len(videoHTTPSessions(h, finitePlay)), "DynamicSessions": dynamicCount, "ActiveJobs": activeJobs,
			"Timeshift": usage, "UpstreamActiveProcesses": upstream.active.Load(), "StreamSlots": len(h.f.app.streamSlots), "ManagerHealth": h.f.app.hls.health()}
		if ownedResourcesRetired {
			cleanupEvidence["Details"] = phase2BrowserCleanupDetails(h, finitePlay, presentation.playID, knownJobs)
		}
		if !ownedResourcesRetired {
			select {
			case <-ticker.C:
			case <-deadline.C:
				cleanupEvidence["Details"] = phase2BrowserCleanupDetails(h, finitePlay, presentation.playID, knownJobs)
				if data, err := json.Marshal(cleanupEvidence); err == nil {
					t.Logf("phase2_browser_cleanup_before_close=%s", data)
				}
				t.Fatal("browser stop did not retire its resources before fixture-wide shutdown")
			}
		}
	}
}
