//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const (
	videoSeekHTTPToolName  = "goby-http-video-seek-ffmpeg"
	videoSeekHTTPProbeName = "goby-http-video-seek-ffprobe"
	videoSeekHTTPLogName   = "tool-invocations.jsonl"
)

type videoSeekHTTPInvocation struct {
	Kind string
	Args []string
}

// The copied test binary records each actual tool invocation and then executes
// the configured FFmpeg/ffprobe. Its fixed private directory needs no additional
// environment variables in the production child's sanitized environment.
func init() {
	name := filepath.Base(os.Args[0])
	if name != videoSeekHTTPToolName && name != videoSeekHTTPProbeName {
		return
	}
	tool, kind := "ffmpeg", "ffmpeg"
	args := os.Args[1:]
	if name == videoSeekHTTPProbeName {
		tool, kind = "ffprobe", "probe"
	} else if len(args) == 1 && args[0] == "-version" {
		kind = "identity"
	} else if len(args) > 0 && args[len(args)-1] == "pipe:4" {
		kind = "producer"
	} else {
		for index := 0; index+1 < len(args); index++ {
			if args[index] == "-f" && args[index+1] == "framehash" {
				kind = "proof"
			}
			if args[index] == "-skip_frame" && args[index+1] == "nokey" {
				kind = "index"
				break
			}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(91)
	}
	encoded, err := json.Marshal(videoSeekHTTPInvocation{Kind: kind, Args: args})
	if err != nil {
		os.Exit(92)
	}
	log, err := os.OpenFile(filepath.Join(filepath.Dir(executable), videoSeekHTTPLogName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(93)
	}
	_, writeErr := log.Write(append(encoded, '\n'))
	closeErr := log.Close()
	if writeErr != nil || closeErr != nil {
		os.Exit(94)
	}
	resolved, err := exec.LookPath(tool)
	if err != nil {
		os.Exit(95)
	}
	if err := syscall.Exec(resolved, append([]string{resolved}, args...), os.Environ()); err != nil {
		os.Exit(96)
	}
}

func videoSeekHTTPTools(t *testing.T, ffmpeg, ffprobe string) (string, string, string) {
	t.Helper()
	aliases := t.TempDir()
	for name, configured := range map[string]string{"ffmpeg": ffmpeg, "ffprobe": ffprobe} {
		resolved, err := exec.LookPath(configured)
		if err != nil {
			t.Fatalf("resolve configured video seek tool (%T)", err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			t.Fatal("resolve absolute video seek tool")
		}
		if err := os.Symlink(resolved, filepath.Join(aliases, name)); err != nil {
			t.Fatal("create owned video seek tool alias")
		}
	}
	t.Setenv("PATH", aliases+string(os.PathListSeparator)+os.Getenv("PATH"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate video seek test executable")
	}
	input, err := os.Open(executable)
	if err != nil {
		t.Fatal("open video seek test executable")
	}
	defer input.Close()
	directory := t.TempDir()
	ffmpegRecorder := filepath.Join(directory, videoSeekHTTPToolName)
	output, err := os.OpenFile(ffmpegRecorder, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		t.Fatal("create owned video seek recorder")
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("copy owned video seek recorder")
	}
	ffprobeRecorder := filepath.Join(directory, videoSeekHTTPProbeName)
	if err := os.Symlink(ffmpegRecorder, ffprobeRecorder); err != nil {
		t.Fatal("create owned ffprobe recorder alias")
	}
	return ffmpegRecorder, ffprobeRecorder, filepath.Join(directory, videoSeekHTTPLogName)
}

func videoSeekHTTPInvocations(t *testing.T, path string) []videoSeekHTTPInvocation {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || len(data) > 256<<10 {
		t.Fatal("read bounded video seek tool log")
	}
	var records []videoSeekHTTPInvocation
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var record videoSeekHTTPInvocation
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal("decode video seek tool invocation")
		}
		records = append(records, record)
	}
	return records
}

func videoSeekHTTPKindCount(records []videoSeekHTTPInvocation, kind string) int {
	count := 0
	for _, record := range records {
		if record.Kind == kind {
			count++
		}
	}
	return count
}

func newVideoSeekHTTPFixture(t *testing.T) (*hlsHTTPFixture, string) {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual video seek HTTP verification")
	}
	f, accounts := newClientSessionHTTPAccounts(t)
	root := t.TempDir()
	path := filepath.Join(root, "Trusted.Video.Seek.mp4")
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=160x90:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=12",
		"-vf", "drawbox=color=green:t=fill:enable='gte(t,3)*lt(t,6)',drawbox=color=blue:t=fill:enable='gte(t,6)*lt(t,9)',drawbox=color=yellow:t=fill:enable='gte(t,9)'",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "12", path)
	nfo := `<movie><title>Automatic seek source</title><sorttitle>automatic seek source</sorttitle><plot>Automatic overview.</plot><genre>Automatic Genre</genre><year>2024</year></movie>`
	if err := os.WriteFile(strings.TrimSuffix(path, ".mp4")+".nfo", []byte(nfo), 0600); err != nil {
		t.Fatal("write owned video seek NFO")
	}
	recorder, probeRecorder, logPath := videoSeekHTTPTools(t, ffmpeg, ffprobe)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatalf("close bootstrap application before real scanner setup (%T)", err)
	}
	cfg := f.cfg
	cfg.FFmpegPath, cfg.FFprobePath, cfg.MediaRoots = recorder, probeRecorder, []string{root}
	cfg.Transcoding = config.TranscodingConfig{
		Enabled: true, CacheDirectory: t.TempDir(), Threads: 1,
		MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2,
	}
	app, err := New(f.ctx, cfg, f.pool, f.users, f.log, "video-seek-integration")
	if err != nil {
		t.Fatalf("start actual video seek application (%T)", err)
	}
	f.app, f.cfg, f.handler = app, cfg, app.Handler()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close actual video seek application (%T)", err)
		}
	})
	collection, err := app.library.CreateLibrary(f.ctx, "Trusted seek movies", "movies", []string{root})
	if err != nil {
		t.Fatalf("create owned video seek catalog (%T)", err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	items, err := app.library.QueryItems(f.ctx, library.Query{UserID: accounts.admin.userID, ParentID: collection.ID, Recursive: true, Limit: 20})
	if err != nil {
		t.Fatalf("read scanned video seek source (%T)", err)
	}
	var source library.Item
	for _, item := range items.Items {
		if item.Path == path {
			source = item
		}
	}
	if source.ID == "" || source.Media == nil || source.Media.ProbeVersion != 6 || len(source.Media.VideoSeekIndexes) != 1 ||
		len(source.Media.VideoSeekIndexes[0].Entries) != 4 || media.ValidateVideoSeekIndex(source.Media.VideoSeekIndexes[0]) != nil {
		t.Fatal("actual application scanning did not persist trusted restart evidence")
	}
	fixture := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: ffmpeg, ffprobe: ffprobe, path: path, libraryID: collection.ID, item: source}
	fixture.policy(t, true)
	fixture.server = httptest.NewServer(f.handler)
	t.Cleanup(fixture.server.Close)
	return fixture, logPath
}

func TestHTTPVideoSeekTrustedScanIndexSurvivesNegotiationAndCannotBeInjected(t *testing.T) {
	fixture, logPath := newVideoSeekHTTPFixture(t)
	before := videoSeekHTTPInvocations(t, logPath)
	if videoSeekHTTPKindCount(before, "probe") == 0 || videoSeekHTTPKindCount(before, "index") != 1 ||
		videoSeekHTTPKindCount(before, "proof") != 0 || videoSeekHTTPKindCount(before, "producer") != 0 {
		t.Fatal("scan-time tool observations did not distinguish indexing from runtime proof and production")
	}
	history := videoHTTPHistory(t, fixture, fixture.item)
	const start = int64(62_500_000)
	index := fixture.item.Media.VideoSeekIndexes[0]
	wantCandidate, err := media.SelectVideoSeekCandidate(index, start)
	if err != nil {
		t.Fatal("trusted scanned index has no usable requested candidate")
	}
	body := videoHTTPBody(true, videoHTTPProfile("http", true, true))
	body["StartTimeTicks"] = start
	prepared := videoHTTPPrepare(t, fixture, fixture.item, body, "http")
	encodedSource, err := json.Marshal(prepared.source)
	if err != nil {
		t.Fatal("encode public video source projection")
	}
	for _, private := range []string{"VideoSeekIndexes", "VideoSeekCandidate", "VideoSeekVerification", "InputSeekTicks", "source_identity", "parameter_sets_sha256"} {
		if prepared.uri.Query().Has(private) || bytes.Contains(encodedSource, []byte(private)) {
			t.Fatal("PlaybackInfo exposed private seek evidence or proof")
		}
	}
	head := fixture.request(t, http.MethodHead, prepared.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, head.header, "video/mp4")
	if len(head.body) != 0 || videoHTTPJobCount(t, fixture, prepared.playID, false) != 0 || !reflect.DeepEqual(before, videoSeekHTTPInvocations(t, logPath)) {
		t.Fatal("PlaybackInfo or HEAD performed an extra probe, proof, or conversion")
	}
	forged, err := media.ValidateVideoSeekCandidate(wantCandidate)
	if err != nil {
		t.Fatal("decode expected trusted candidate")
	}
	forged.Index.SourceIdentity = strings.Repeat("0", 64)
	forgedJSON, err := json.Marshal(forged)
	if err != nil {
		t.Fatal("encode query-only forged evidence")
	}
	injected := *prepared.uri
	query := injected.Query()
	query.Set("VideoSeekCandidate", string(forgedJSON))
	query.Set("VideoSeekIndexes", "[]")
	query.Set("VideoSeekVerification", `{"Verified":true,"InputSeekTicks":1}`)
	query.Set("InputSeekTicks", "1")
	injected.RawQuery = query.Encode()
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodHead, injected.String(), nil, nil), http.StatusOK)
	if videoHTTPJobCount(t, fixture, prepared.playID, false) != 0 || !reflect.DeepEqual(before, videoSeekHTTPInvocations(t, logPath)) {
		t.Fatal("query-supplied private evidence triggered work during HEAD")
	}
	output := fixture.request(t, http.MethodGet, injected.String(), nil, nil)
	expectHLSHTTPStatus(t, output, http.StatusOK)
	assertAudioHTTPProgressiveHeaders(t, output.header, "video/mp4")
	videoHTTPVerifyMP4(t, fixture, output.body, videoHTTPOutput{width: 96, height: 54, frames: 138, audio: true, seconds: 5.75, color: [3]int{0, 0, 255}})
	_, record := videoHTTPRecord(t, fixture, prepared.playID, "h264", start)
	if record.State != "completed" || record.Spec.Plan.VideoSeekCandidate != wantCandidate || record.Spec.Plan.VideoStreamIndex != index.StreamIndex {
		t.Fatal("GET did not preserve the scanned candidate in its immutable scoped plan")
	}
	after := videoSeekHTTPInvocations(t, logPath)
	if videoSeekHTTPKindCount(after, "probe") != videoSeekHTTPKindCount(before, "probe") || videoSeekHTTPKindCount(after, "index") != 1 ||
		videoSeekHTTPKindCount(after, "proof") == 0 || videoSeekHTTPKindCount(after, "producer") != 1 {
		t.Fatal("GET did not perform bounded fresh proof before one producer, or unexpectedly reindexed the source")
	}
	var worker []string
	for _, invocation := range after {
		if invocation.Kind == "producer" {
			worker = invocation.Args
		}
	}
	firstInput, inputSeek := -1, -1
	for position, argument := range worker {
		if argument == "-i" && firstInput < 0 {
			firstInput = position
		}
		if argument == "-ss" && inputSeek < 0 {
			inputSeek = position
		}
	}
	if inputSeek < 0 || firstInput < 0 || inputSeek >= firstInput || !videoSeekHTTPArgument(worker, "-seek_timestamp", "1") ||
		!videoSeekHTTPArgument(worker, "-discard:v", "all") || !videoSeekHTTPArgument(worker, "-map", "1:"+strconv.Itoa(record.Spec.Plan.AudioStreamIndex)) {
		t.Fatal("the verified GET did not use fast video input and independently preserved audio history")
	}
	cached := fixture.request(t, http.MethodGet, prepared.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, cached, http.StatusOK)
	if !bytes.Equal(cached.body, output.body) || !reflect.DeepEqual(after, videoSeekHTTPInvocations(t, logPath)) || videoHTTPJobCount(t, fixture, prepared.playID, false) != 1 {
		t.Fatal("irrelevant private query fields changed the canonical output or its cache reuse")
	}
	if videoHTTPHistory(t, fixture, fixture.item) != history {
		t.Fatal("seek preparation, proof, and media reads changed user playback state")
	}
}

func videoSeekHTTPArgument(args []string, key, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}

func TestHTTPVideoSeekProbeFiveUpgradePreservesMetadataControlsAndUserData(t *testing.T) {
	fixture, logPath := newVideoSeekHTTPFixture(t)
	path := "/admin/v1/items/" + fixture.item.ID + "/metadata"
	headers := http.Header{"X-CSRF-Token": {csrfToken(fixture.accounts.cookie.Value)}}
	response := fixture.f.request(t, http.MethodGet, path, nil, nil, fixture.accounts.cookie)
	expectStatus(t, response, http.StatusOK)
	detail := jsonObject(t, response)
	response = fixture.f.request(t, http.MethodPut, path, map[string]any{
		"Revision": detail["Revision"], "Overrides": map[string]any{"Name": "Manual seek title", "Overview": "Manual overview.", "Genres": []string{"Manual Genre"}},
		"LockedFields": []string{"Overview"},
	}, headers, fixture.accounts.cookie)
	expectStatus(t, response, http.StatusOK)
	detail = jsonObject(t, response)
	favorite := fixture.f.request(t, http.MethodPost, "/emby/Users/"+fixture.accounts.viewer.userID+"/FavoriteItems/"+fixture.item.ID, nil, fixture.accounts.viewer.headers)
	expectStatus(t, favorite, http.StatusOK)
	history := videoHTTPHistory(t, fixture, fixture.item)
	snapshot := func() string {
		var value string
		if err := fixture.f.pool.QueryRow(fixture.f.ctx, `SELECT jsonb_build_object(
			'metadata', to_jsonb(ms),
			'entities', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.entity_id) FROM item_entities e WHERE e.item_id = ms.item_id), '[]'::jsonb),
			'userdata', COALESCE((SELECT jsonb_agg(to_jsonb(u) ORDER BY u.user_id) FROM user_item_data u WHERE u.item_id = ms.item_id), '[]'::jsonb))::text
			FROM item_metadata_state ms WHERE ms.item_id = $1`, fixture.item.ID).Scan(&value); err != nil {
			t.Fatalf("read metadata and user-state upgrade snapshot (%T)", err)
		}
		return value
	}
	beforeState := snapshot()
	beforeTools := videoSeekHTTPInvocations(t, logPath)
	if _, err := fixture.f.pool.Exec(fixture.f.ctx, `UPDATE items SET media = (media - 'VideoSeekIndexes') || jsonb_build_object('ProbeVersion', 5) WHERE id = $1`, fixture.item.ID); err != nil {
		t.Fatal("prepare the isolated legacy probe-five snapshot")
	}
	legacy, err := fixture.f.app.library.GetItem(fixture.f.ctx, fixture.accounts.viewer.userID, fixture.item.ID)
	if err != nil || legacy.Media == nil || legacy.Media.ProbeVersion != 5 || len(legacy.Media.VideoSeekIndexes) != 0 {
		t.Fatal("legacy probe fixture retained current restart evidence")
	}
	(&streamHTTPFixture{f: fixture.f}).rescan(t, fixture.libraryID)
	current, err := fixture.f.app.library.GetItem(fixture.f.ctx, fixture.accounts.viewer.userID, fixture.item.ID)
	if err != nil || current.Media == nil || current.Media.ProbeVersion != 6 || len(current.Media.VideoSeekIndexes) != 1 ||
		media.ValidateVideoSeekIndex(current.Media.VideoSeekIndexes[0]) != nil {
		t.Fatal("normal library rescan did not replace legacy probe facts with a trusted seek index")
	}
	afterTools := videoSeekHTTPInvocations(t, logPath)
	if videoSeekHTTPKindCount(afterTools, "probe") <= videoSeekHTTPKindCount(beforeTools, "probe") ||
		videoSeekHTTPKindCount(afterTools, "index") != videoSeekHTTPKindCount(beforeTools, "index")+1 ||
		videoSeekHTTPKindCount(afterTools, "proof") != 0 || videoSeekHTTPKindCount(afterTools, "producer") != 0 {
		t.Fatal("probe upgrade did not run real indexing independently from runtime proof and conversion")
	}
	response = fixture.f.request(t, http.MethodGet, path, nil, nil, fixture.accounts.cookie)
	expectStatus(t, response, http.StatusOK)
	if !reflect.DeepEqual(jsonObject(t, response), detail) || snapshot() != beforeState || videoHTTPHistory(t, fixture, current) != history {
		t.Fatal("technical probe upgrade changed metadata values, locks, revision, attribution, entities, or user data")
	}
	if current.Name != "Manual seek title" || current.Overview != "Manual overview." || current.Path != fixture.item.Path {
		t.Fatal("technical probe upgrade replaced manual catalog values or physical identity")
	}
}
