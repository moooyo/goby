//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
)

// This bounded scenario exercises production media workers together. Generated
// episodes prove execution and delivery mechanics, not intro detection accuracy.
// GOBY_PHASE3_COMPOUND_CATALOG_ITEMS=100000 adds SQL-only catalog pressure; those
// extra libraries have no physical media and must never be scanned.
func TestHTTPPhase3CompoundFunctionalMedia(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_COMPOUND") != "1" {
		t.Skip("set GOBY_PHASE3_COMPOUND=1 for the real compound media journey")
	}
	for _, name := range []string{"GOBY_TEST_DATABASE_URL", "GOBY_FFMPEG", "GOBY_FFPROBE", "GOBY_INTRO_FINGERPRINT"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required for the requested compound journey", name)
		}
	}
	catalogItems := 0
	switch os.Getenv("GOBY_PHASE3_COMPOUND_CATALOG_ITEMS") {
	case "":
	case "100000":
		catalogItems = 100000
	default:
		t.Fatal("GOBY_PHASE3_COMPOUND_CATALOG_ITEMS must be empty or 100000")
	}
	f, accounts := newClientSessionHTTPAccounts(t, 30*time.Minute)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "goby-phase3-compound-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove owned compound fixture: %v", err)
		}
	})
	mediaRoot := filepath.Join(base, "media")
	moviesRoot, tvRoot := filepath.Join(mediaRoot, "movies"), filepath.Join(mediaRoot, "tv")
	seasonRoot := filepath.Join(tvRoot, "Compound Show", "Season 01")
	for _, path := range []string{moviesRoot, seasonRoot, filepath.Join(base, "evidence")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	movieSeed := filepath.Join(base, "movie.mp4")
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=160x90:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=12",
		"-vf", "drawbox=color=green:t=fill:enable='gte(t,3)*lt(t,6)',drawbox=color=blue:t=fill:enable='gte(t,6)*lt(t,9)',drawbox=color=yellow:t=fill:enable='gte(t,9)'",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "72", "-keyint_min", "72",
		"-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "12", movieSeed)
	episodeSeed := filepath.Join(base, "episode.mp4")
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=45",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=45",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast",
		"-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "45", episodeSeed)
	copyMedia := func(source, target string, number int) {
		t.Helper()
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		// A valid free box makes independent episode content identities while
		// preserving the actual generated audio and video for every decoder.
		box := make([]byte, 16)
		binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
		copy(box[4:8], "free")
		binary.BigEndian.PutUint64(box[8:], uint64(number))
		if err := os.WriteFile(target, append(data, box...), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Keep real probing active long enough to observe both serialized analysis
	// tasks; no production delay or mocked running state creates the overlap.
	const movieCount = 100
	for index := 0; index < movieCount; index++ {
		copyMedia(movieSeed, filepath.Join(moviesRoot, fmt.Sprintf("Movie%02d.mp4", index)), index)
	}
	episodePaths := make([]string, 3)
	for index := range episodePaths {
		episodePaths[index] = filepath.Join(seasonRoot, fmt.Sprintf("Compound.Show.S01E%02d.mp4", index+1))
		copyMedia(episodeSeed, episodePaths[index], index+movieCount)
	}
	f.cfg.MediaRoots, f.cfg.FFmpegPath, f.cfg.FFprobePath = []string{mediaRoot}, ffmpeg, ffprobe
	f.cfg.ScanEvidence = config.ScanEvidenceConfig{Enabled: true, Directory: filepath.Join(base, "evidence"), MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096}
	f.cfg.Transcoding = config.TranscodingConfig{Enabled: true, CacheDirectory: filepath.Join(base, "transcoding"), Threads: 1,
		MaxJobs: 4, MaxUserJobs: 4, MaxSessionJobs: 4, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 128 << 20, MaxJobBytes: 32 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2}
	f.cfg.MediaAnalysis = config.MediaAnalysisConfig{Enabled: true, CacheDirectory: filepath.Join(base, "analysis"),
		CacheMaxBytes: 512 << 20, CacheMaxEntries: 64, MaxEntryBytes: 128 << 20, MaxFileBytes: 32 << 20,
		FingerprintPath: os.Getenv("GOBY_INTRO_FINGERPRINT")}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "phase3-compound-functional")
	if err != nil {
		t.Fatalf("start production compound server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close compound server workers: %v", err)
		}
	})
	f.app, f.handler = app, app.Handler()
	h := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: ffmpeg, ffprobe: ffprobe}
	h.server = httptest.NewServer(f.handler)
	t.Cleanup(h.server.Close)
	adminHeaders := http.Header{"Cookie": {accounts.cookie.String()}, "X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}
	requestJSON := func(method, path string, body any, headers http.Header, status int) map[string]any {
		t.Helper()
		response := h.request(t, method, path, body, headers)
		if response.status != status {
			t.Fatalf("compound HTTP %s %s status=%d expected=%d body=%s", method, path, response.status, status, response.body)
		}
		var value map[string]any
		if err := json.Unmarshal(response.body, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	readScan := func(jobID string) map[string]any {
		t.Helper()
		page := requestJSON(http.MethodGet, "/admin/v1/jobs", nil, adminHeaders, http.StatusOK)
		for _, raw := range page["Items"].([]any) {
			job := raw.(map[string]any)
			if job["Id"] == jobID {
				return job
			}
		}
		t.Fatalf("scan job %s disappeared", jobID)
		return nil
	}
	waitScan := func(jobID string, scanned int) {
		t.Helper()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			job := readScan(jobID)
			if job["Status"] == "completed" {
				if job["Error"] != "" || job["Scanned"] != float64(scanned) {
					t.Fatalf("initial compound scan failed its population check: %#v", job)
				}
				return
			}
			if job["Status"] != "pending" && job["Status"] != "running" {
				t.Fatalf("initial compound scan failed: %#v", job)
			}
			select {
			case <-ticker.C:
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
		}
	}
	libraries := make(map[string]string, 2)
	for _, spec := range []struct {
		name, kind, path string
		count            int
	}{{"Compound Movies", "movies", moviesRoot, movieCount}, {"Compound Television", "tvshows", tvRoot, len(episodePaths)}} {
		created := requestJSON(http.MethodPost, "/admin/v1/libraries", map[string]any{
			"Name": spec.name, "CollectionType": spec.kind, "Paths": []string{spec.path}, "Scan": true,
		}, adminHeaders, http.StatusCreated)
		libraries[spec.kind] = stringValue(t, objectValue(t, created, "Library"), "Id")
		waitScan(stringValue(t, objectValue(t, created, "Job"), "Id"), spec.count)
	}
	h.libraryID, h.path = libraries["movies"], filepath.Join(moviesRoot, "Movie00.mp4")
	var movieID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1 AND type='Movie'`, h.path).Scan(&movieID); err != nil {
		t.Fatal(err)
	}
	h.item, err = app.library.GetItem(f.ctx, accounts.admin.userID, movieID)
	if err != nil {
		t.Fatal(err)
	}
	h.policy(t, true)
	var catalogLibraries []string
	const catalogLeavesPerLibrary = 50000
	if catalogItems != 0 {
		for _, name := range []string{"A", "B"} {
			path := filepath.Join(mediaRoot, "catalog-only-"+name)
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			created := requestJSON(http.MethodPost, "/admin/v1/libraries", map[string]any{
				"Name": "Compound catalog only " + name, "CollectionType": "mixed", "Paths": []string{path}, "Scan": false,
			}, adminHeaders, http.StatusCreated)
			id := stringValue(t, objectValue(t, created, "Library"), "Id")
			catalogCapacitySeed(t, f, id, path, catalogLeavesPerLibrary-catalogCapacityEpisodes-catalogCapacityAudio)
			catalogLibraries = append(catalogLibraries, id)
		}
		// Bulk SQL seeding bypasses the gradual population and auto-analysis of
		// ordinary scans. Prepare statistics before exercising recursive pages.
		analyzeCtx, cancelAnalyze := context.WithTimeout(f.ctx, 30*time.Second)
		_, err := f.pool.Exec(analyzeCtx, "ANALYZE items")
		cancelAnalyze()
		if err != nil {
			t.Fatalf("analyze the isolated bulk catalog fixture: %v", err)
		}
		allowed, err := json.Marshal(append([]string{h.libraryID}, catalogLibraries...))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=jsonb_set(policy,'{EnabledFolders}',$2::jsonb) WHERE id=$1`, accounts.viewer.userID, allowed); err != nil {
			t.Fatal(err)
		}
		t.Logf("compound population real_movies=%d real_episodes=3 catalog_only_leaves=%d catalog_only_libraries=%d", movieCount, catalogItems, len(catalogLibraries))
	}
	assertCatalogPages := func() {
		t.Helper()
		for _, libraryID := range catalogLibraries {
			for _, limit := range []int{catalogCapacityPage, 0} {
				offset := catalogLeavesPerLibrary - catalogCapacityPage
				query := url.Values{"ParentId": {libraryID}, "Recursive": {"true"}, "IncludeItemTypes": {"Movie,Episode,Audio"},
					"SortBy": {"SortName"}, "SortOrder": {"Ascending"}, "StartIndex": {strconv.Itoa(offset)}, "Limit": {strconv.Itoa(limit)}}
				page := requestJSON(http.MethodGet, "/emby/Items?"+query.Encode(), nil, accounts.viewer.headers, http.StatusOK)
				items := page["Items"].([]any)
				if page["TotalRecordCount"] != float64(catalogLeavesPerLibrary) || len(items) != limit {
					t.Fatalf("catalog-only page changed its library count or length: library=%s total=%v length=%d", libraryID, page["TotalRecordCount"], len(items))
				}
				for index, raw := range items {
					item := raw.(map[string]any)
					number := offset + index + 1
					if item["Id"] != catalogCapacityLeafID(libraryID, number) || item["Name"] != fmt.Sprintf("Item %05d", number) || item["Type"] != "Audio" || item["IsFolder"] != false {
						t.Fatalf("catalog-only deep page lost exact order or crossed libraries: library=%s position=%d", libraryID, number)
					}
				}
			}
		}
	}
	assertCatalogPopulation := func() {
		t.Helper()
		for _, libraryID := range catalogLibraries {
			var rows, leaves, scans int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER (WHERE NOT is_folder) FROM items WHERE library_id=$1`, libraryID).Scan(&rows, &leaves); err != nil {
				t.Fatal(err)
			}
			wantRows := catalogLeavesPerLibrary + 1 + catalogCapacitySeries + catalogCapacitySeries*catalogCapacitySeasons + catalogCapacityAlbums
			if rows != wantRows || leaves != catalogLeavesPerLibrary {
				t.Fatalf("catalog-only library population changed: library=%s rows=%d leaves=%d", libraryID, rows, leaves)
			}
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM scan_jobs WHERE library_id=$1`, libraryID).Scan(&scans); err != nil {
				t.Fatal(err)
			}
			if scans != 0 {
				t.Fatalf("catalog-only library was incorrectly scanned: %s", libraryID)
			}
		}
	}
	assertCatalogPages()
	assertCatalogPopulation()
	episodeIDs := make([]string, len(episodePaths))
	for index, path := range episodePaths {
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode'`, path).Scan(&episodeIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	overview := requestJSON(http.MethodGet, "/admin/v1/media-analysis", nil, adminHeaders, http.StatusOK)
	availability := objectValue(t, overview, "Runtime")
	if availability["IntroAvailable"] != true || availability["PreviewAvailable"] != true {
		t.Fatalf("real analysis dependencies are unavailable: %#v", availability)
	}
	configuration := objectValue(t, overview, "Configuration")
	profile := objectValue(t, configuration, "Profile")
	profile["PreviewIntervalSeconds"] = 2
	requestJSON(http.MethodPut, "/admin/v1/media-analysis/configuration", map[string]any{
		"Revision": configuration["Revision"], "Profile": profile,
	}, adminHeaders, http.StatusOK)
	remux := videoHTTPPrepare(t, h, h.item, map[string]any{
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": true, "AllowAudioStreamCopy": true,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{videoHTTPProfile("http", false, true)}},
	}, "http")
	if remux.uri.Query().Get("VideoCodec") != "copy" || remux.uri.Query().Get("AudioCodec") != "copy" {
		t.Fatal("compound remux plan did not preserve both encoded streams")
	}
	encoded := videoHTTPBody(true, videoHTTPProfile("http", true, true))
	encoded["StartTimeTicks"] = int64(42_500_000)
	transcoded := videoHTTPPrepare(t, h, h.item, encoded, "http")
	started := time.Now()
	scan := requestJSON(http.MethodPost, "/admin/v1/libraries/"+h.libraryID+"/scan", map[string]any{"ForceProbe": true}, adminHeaders, http.StatusAccepted)
	scanID := stringValue(t, objectValue(t, scan, "Job"), "Id")
	// Admit analysis after the real scan starts, so fixture scheduling cannot
	// complete a short analysis before any scan-running observation exists.
	for {
		job := readScan(scanID)
		if job["Status"] == "running" {
			break
		}
		if job["Status"] != "pending" {
			t.Fatalf("compound scan ended before analysis admission: %#v", job)
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
	}
	runs := make(map[string]string, 2)
	for _, kind := range []string{"intro", "previews"} {
		admitted := requestJSON(http.MethodPost, "/admin/v1/media-analysis/runs", map[string]any{
			"Kind": kind, "RequestId": "compound-" + kind, "LibraryIds": []string{}, "ItemIds": episodeIDs, "Force": true,
		}, adminHeaders, http.StatusAccepted)
		runs[kind] = stringValue(t, admitted, "RunId")
	}
	completed := make(map[string]bool, 2)
	analysisOverlap := make(map[string]int, 2)
	queryOverlap := 0
	catalogQueryOverlap := 0
	playbackOverlap := false
	var remuxBytes, transcodedBytes []byte
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		job := readScan(scanID)
		scanRunning := job["Status"] == "running"
		if job["Status"] != "pending" && !scanRunning && job["Status"] != "completed" {
			t.Fatalf("compound scan failed: %#v", job)
		}
		for kind, runID := range runs {
			if completed[kind] {
				continue
			}
			value := requestJSON(http.MethodGet, "/admin/v1/task-runs/"+runID+"?StartIndex=0&Limit=25", nil, adminHeaders, http.StatusOK)
			run := objectValue(t, value, "Run")
			children := objectValue(t, value, "Children")
			items := children["Items"].([]any)
			if len(items) == 0 || children["TotalRecordCount"] != float64(len(items)) {
				t.Fatalf("%s did not admit complete real analysis children", kind)
			}
			for _, raw := range items {
				child := raw.(map[string]any)
				if child["State"] == "running" && scanRunning {
					analysisOverlap[kind]++
				}
				if run["State"] == "completed" && (child["State"] != "completed" || child["ErrorCode"] != "") {
					t.Fatalf("%s child did not complete real work: %#v", kind, child)
				}
			}
			switch run["State"] {
			case "completed":
				completed[kind] = true
				t.Logf("compound %s completed elapsed=%s children=%d scan_overlap_samples=%d", kind, time.Since(started), len(items), analysisOverlap[kind])
			case "failed", "cancelled", "interrupted":
				t.Fatalf("%s analysis failed: %#v", kind, value)
			}
		}
		page := requestJSON(http.MethodGet, "/emby/Items?Ids="+url.QueryEscape(movieID), nil, accounts.viewer.headers, http.StatusOK)
		items := page["Items"].([]any)
		if page["TotalRecordCount"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["Id"] != movieID {
			t.Fatal("stable movie query changed during compound work")
		}
		// Observe the short intro worker before issuing synchronous deep pages;
		// those requests can legitimately take longer than intro extraction.
		catalogPagesObserved := len(catalogLibraries) != 0 && analysisOverlap["intro"] != 0
		if catalogPagesObserved {
			assertCatalogPages()
		}
		if scanRunning && readScan(scanID)["Status"] == "running" {
			queryOverlap++
			if catalogPagesObserved {
				catalogQueryOverlap++
			}
		}
		if scanRunning && remuxBytes == nil {
			output := h.request(t, http.MethodGet, remux.uri.String(), nil, nil)
			expectHLSHTTPStatus(t, output, http.StatusOK)
			remuxBytes = output.body
			output = h.request(t, http.MethodGet, transcoded.uri.String(), nil, nil)
			expectHLSHTTPStatus(t, output, http.StatusOK)
			transcodedBytes = output.body
			playbackOverlap = readScan(scanID)["Status"] == "running"
			t.Logf("compound playback received during scan phase remux_bytes=%d transcode_bytes=%d", len(remuxBytes), len(transcodedBytes))
		}
		if job["Status"] == "completed" && completed["intro"] && completed["previews"] {
			if job["Error"] != "" || job["Scanned"] != float64(movieCount) {
				t.Fatalf("compound scan population changed: %#v", job)
			}
			break
		}
		select {
		case <-ticker.C:
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
	}
	if queryOverlap == 0 || analysisOverlap["intro"] == 0 || analysisOverlap["previews"] == 0 || !playbackOverlap || len(remuxBytes) == 0 || len(transcodedBytes) == 0 {
		t.Fatalf("compound work lacked observed overlap or actual playback output: queries=%d analysis=%v playback_overlap=%t remux=%d transcode=%d", queryOverlap, analysisOverlap, playbackOverlap, len(remuxBytes), len(transcodedBytes))
	}
	if catalogItems != 0 && catalogQueryOverlap == 0 {
		t.Fatal("catalog-only deep/count HTTP pages did not overlap the real media scan")
	}
	videoHTTPVerifyMP4(t, h, remuxBytes, videoHTTPOutput{width: 160, height: 90, frames: 288, audio: true, seconds: 12, color: [3]int{255, 0, 0}})
	videoHTTPVerifyMP4(t, h, transcodedBytes, videoHTTPOutput{width: 96, height: 54, frames: 186, audio: true, seconds: 7.75, color: [3]int{0, 128, 0}})
	_, remuxRecord := videoHTTPRecord(t, h, remux.playID, "copy", 0)
	_, transcodeRecord := videoHTTPRecord(t, h, transcoded.playID, "h264", 42_500_000)
	if remuxRecord.State != "completed" || remuxRecord.Spec.Plan.AudioCodec != "copy" || transcodeRecord.State != "completed" {
		t.Fatal("playback output did not come from completed production remux/transcode workers")
	}
	for _, itemID := range episodeIDs {
		var payload []byte
		var duration int64
		if err := f.pool.QueryRow(f.ctx, `SELECT payload,duration_ticks FROM analysis_feature_cache WHERE item_id=$1`, itemID).Scan(&payload, &duration); err != nil {
			t.Fatalf("intro did not persist actual extracted features for %s: %v", itemID, err)
		}
		features, err := library.DecodeAnalysisFeatures(payload, duration)
		if err != nil || len(features.Audio) == 0 || len(features.Visual) == 0 {
			t.Fatalf("intro omitted real audio/visual extraction for %s: %v", itemID, err)
		}
		detail := requestJSON(http.MethodGet, "/admin/v1/media-analysis/items/"+itemID, nil, adminHeaders, http.StatusOK)
		detection := objectValue(t, detail, "Detection")
		if detection["Status"] != "qualified" && detection["Status"] != "review" && detection["Status"] != "no_result" {
			t.Fatalf("intro lacks a completed result: %#v", detection)
		}
		t.Logf("intro item=%s status=%v reasons=%v audio_samples=%d visual_samples=%d", itemID, detection["Status"], detection["Reasons"], len(features.Audio), len(features.Visual))
		response := h.request(t, http.MethodGet, "/emby/Videos/"+itemID+"/index.bif?Width=240", nil, accounts.admin.headers)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		archive, err := bif.Open(bytes.NewReader(response.body), int64(len(response.body)), bif.DefaultLimits())
		if err != nil || archive.Len() < 2 {
			t.Fatalf("actual preview worker did not publish a multi-frame HTTP BIF: %v", err)
		}
		for _, index := range []int{0, archive.Len() / 2, archive.Len() - 1} {
			frame, err := archive.JPEG(f.ctx, index)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := jpeg.Decode(bytes.NewReader(frame))
			if err != nil || decoded.Bounds().Dx() != 240 || decoded.Bounds().Dy() <= 0 {
				t.Fatalf("HTTP BIF contains an invalid generated frame: %v", err)
			}
		}
		t.Logf("preview item=%s HTTP BIF decoded frames=%d bytes=%d", itemID, archive.Len(), len(response.body))
	}
	for _, playID := range []string{remux.playID, transcoded.playID} {
		sessions := videoHTTPSessions(h, playID)
		response := h.request(t, http.MethodDelete, videoHTTPStopPath(accounts.viewer, playID), nil, accounts.viewer.headers)
		expectHLSHTTPStatus(t, response, http.StatusNoContent)
		videoHTTPWaitRetired(t, h, playID, sessions)
	}
	assertCatalogPages()
	assertCatalogPopulation()
	t.Logf("compound accepted real_movies=%d real_episodes=3 catalog_only_leaves=%d catalog_query_overlap=%d query_overlap=%d analysis_overlap=%v playback_overlap=%t elapsed=%s", movieCount, catalogItems, catalogQueryOverlap, queryOverlap, analysisOverlap, playbackOverlap, time.Since(started))
}
